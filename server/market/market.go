// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"sync"
	"time"

	"github.com/decred/dcrdex/dex"
	"github.com/decred/dcrdex/dex/msgjson"
	"github.com/decred/dcrdex/dex/order"
	"github.com/decred/dcrdex/server/book"
	"github.com/decred/dcrdex/server/db"
	"github.com/decred/dcrdex/server/matcher"
)

// Error is just a basic error.
type Error string

// Error satisfies the error interface.
func (e Error) Error() string {
	return string(e)
}

// ErrClientDisconnected will be returned if Send or Request is called on a
// disconnected link.
const (
	ErrMarketNotRunning       = Error("market not running")
	ErrInvalidOrder           = Error("order failed validation")
	ErrEpochMissed            = Error("order unexpectedly missed its intended epoch")
	ErrDuplicateOrder         = Error("order already in epoch")
	ErrMalformedOrderResponse = Error("malformed order response")
	ErrInternalServer         = Error("internal server error")
)

// Swapper coordinates atomic swaps for one or more matchsets.
type Swapper interface {
	Negotiate(matchSets []*order.MatchSet)
}

// The Market manager should not be overly involved with details of accounts and
// authentication. Via the account package it should request account status with
// new orders, verification of order signatures. The Market should also perform
// various account package callbacks such as order status updates so that the
// account package code can keep various data up-to-date, including order
// status, history, cancellation statistics, etc.
//
// The Market performs the following:
// - Receiving and validating new order data (amounts vs. lot size, check fees,
//   utxos, sufficient market buy buffer, etc.).
// - Putting incoming orders into the current epoch queue.
// - Maintain an order book, which must also implement matcher.Booker.
// - Initiate order matching via matcher.Match(book, currentQueue)
// - During and/or after matching:
//     * update the book (remove orders, add new standing orders, etc.)
//     * retire/archive the epoch queue
//     * publish the matches (and order book changes?)
//     * initiate swaps for each match (possibly groups of related matches)
// - Cycle the epochs.
// - Recording all events with the archivist
type Market struct {
	// ctx is provided to the Market as the first argument of the constructor
	// and subsequently provided to internal (unexported) methods as a field of
	// Market for the sole purpose of providing a cancellation mechanism.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Communications.
	orderRouter chan *orderUpdateSignal  // incoming orders
	orderFeeds  []chan *bookUpdateSignal // outgoing notifications

	// Order handling: book, active epoch queues, order matcher, and swapper.
	running       chan struct{}
	marketInfo    *dex.MarketInfo
	bookMtx       sync.Mutex
	book          *book.Book
	epochDuration int64
	matcher       *matcher.Matcher
	swapper       Swapper
	auth          AuthManager

	// Persistent data storage.
	storage db.DEXArchivist
}

// OrderFeed provides a new order book update channel. This is not thread-safe,
// and should not be called after calling Start.
func (m *Market) OrderFeed() <-chan *bookUpdateSignal {
	bookUpdates := make(chan *bookUpdateSignal, 1)
	m.orderFeeds = append(m.orderFeeds, bookUpdates)
	return bookUpdates
}

type orderUpdateSignal struct {
	rec     *orderRecord
	errChan chan error // should be buffered
}

func newOrderUpdateSignal(ord *orderRecord) *orderUpdateSignal {
	return &orderUpdateSignal{ord, make(chan error, 1)}
}

// SubmitOrder submits a new order for inclusion into the current epoch. This is
// the synchronous version of SubmitOrderAsync.
func (m *Market) SubmitOrder(rec *orderRecord) error {
	return <-m.SubmitOrderAsync(rec)
}

// SubmitOrderAsync submits a new order for inclusion into the current epoch.
// When submission is completed, an error value will be sent on the channel.
// This is the asynchronous version of SubmitOrder.
func (m *Market) SubmitOrderAsync(rec *orderRecord) <-chan error {
	// Validate the order. The order router must do it's own validation, but do
	// a second validation for (1) this Market and (2) epoch status, before
	// putting it on the queue.
	if !m.validateOrder(rec.order) {
		log.Errorf("SubmitOrderAsync: Invalid order received: %v", rec.order)
		errChan := make(chan error, 1)
		errChan <- ErrInvalidOrder
		return errChan
	}

	sig := newOrderUpdateSignal(rec)
	m.orderRouter <- sig
	return sig.errChan
}

// NewMarket creates a new Market for the provided base and quote assets, with
// an epoch cycling at given duration in seconds.
func NewMarket(ctx context.Context, mktInfo *dex.MarketInfo, storage db.DEXArchivist,
	swapper Swapper, authMgr AuthManager) (*Market, error) {
	// Make sure the DEXArchivist is healthy before taking orders.
	if err := storage.LastErr(); err != nil {
		return nil, err
	}

	// Supplement the provided context with a cancel function. The Stop method
	// may be used to stop the market, or the parent context itself may be
	// canceled.
	ctxInternal, cancel := context.WithCancel(ctx)

	return &Market{
		ctx:           ctxInternal,
		cancel:        cancel,
		running:       make(chan struct{}),
		marketInfo:    mktInfo,
		epochDuration: int64(mktInfo.EpochDuration),
		orderRouter:   make(chan *orderUpdateSignal, 16),
		book:          book.New(mktInfo.LotSize),
		matcher:       matcher.New(),
		swapper:       swapper,
		auth:          authMgr,
		storage:       storage,
	}, nil
}

// Start beings order processing.
func (m *Market) Start(startEpochIdx int64) {
	m.wg.Add(1)
	go m.runEpochs(startEpochIdx)
}

// Stop begins the Market's shutdown. Use WaitForShutdown after Stop to wait for
// shutdown to complete. The Swapper is NOT stopped.
func (m *Market) Stop() {
	log.Infof("Market shutting down...")
	m.cancel()
	for _, s := range m.orderFeeds {
		close(s)
	}
}

// WaitForShutdown waits until the order processing is finished and the Market
// is stopped.
func (m *Market) WaitForShutdown() {
	m.wg.Wait()
}

// WaitForEpochOpen waits until the start of epoch processing.
func (m *Market) WaitForEpochOpen() {
	<-m.running
}

func (m *Market) notifyNewOrder(order order.Order, action bookUpdateAction) {
	// send to each receiver
	for _, s := range m.orderFeeds {
		select {
		case <-m.ctx.Done():
			return
		case s <- &bookUpdateSignal{action, order}:
		}
	}
}

// Cancel the active epoch, signalling to the clients that their orders have
// failed due to server shutdown.
func (m *Market) cancelEpoch() {
	//TODO
}

// Book retrieves the market's current order book.
func (m *Market) Book() (buys, sells []*order.LimitOrder) {
	// NOTE: it may be desirable to cache the response.
	m.bookMtx.Lock()
	buys = m.book.BuyOrders()
	sells = m.book.SellOrders()
	m.bookMtx.Unlock()
	return
}

// runEpochs is the main order processing loop, which takes new orders, notifies
// book subscribers, and cycles the epochs. This is to be run as a goroutine.
func (m *Market) runEpochs(nextEpochIdx int64) {
	nextEpoch := NewEpoch(nextEpochIdx, m.epochDuration)
	epochCycle := time.After(time.Until(nextEpoch.Start))

	//defer m.cancelEpoch() // TODO
	defer m.wg.Done()

	var running bool
	var currentEpoch *EpochQueue
	cycleEpoch := func() {
		if currentEpoch != nil {
			go m.processEpoch(currentEpoch)
		}
		currentEpoch = nextEpoch

		nextEpoch = NewEpoch(currentEpoch.Epoch+1, m.epochDuration)
		epochCycle = time.After(time.Until(nextEpoch.Start))

		if !running {
			close(m.running)
			running = true
		}
	}

	// In case the market is stopped before the first epoch, close the running
	// channel so that WaitForShutdown does not hang.
	defer func() {
		if !running {
			close(m.running)
		}
	}()

	for {
		if m.ctx.Err() != nil {
			return
		}
		if err := m.storage.LastErr(); err != nil {
			log.Criticalf("Archivist failing. Last unexpected error: %v", err)
			m.cancel()
			return
		}

		// Prioritize the epoch cycle.
		select {
		case <-epochCycle:
			cycleEpoch()
		default:
		}

		// Wait for the next signal (cancel, new order, or epoch cycle).
		select {
		case <-m.ctx.Done():
			return

		case s := <-m.orderRouter:
			if currentEpoch == nil {
				// The order is not time-stamped yet, so the ID cannot be computed.
				log.Debugf("Order type %v received prior to market start.", s.rec.order.Type())
				s.errChan <- ErrMarketNotRunning
				continue
			}

			// The order's server time stamp.
			sTime := time.Now()

			// Push the order into the next epoch if receiving and stamping it
			// took just a little too long.
			var orderEpoch *EpochQueue
			switch {
			case currentEpoch.IncludesTime(sTime):
				orderEpoch = currentEpoch
			case nextEpoch.IncludesTime(sTime):
				log.Warnf("Order %v (sTime=%d) fell into the next epoch [%d,%d)",
					s.rec.order, sTime.UnixNano(), nextEpoch.Start.Unix(), nextEpoch.End.Unix())
				orderEpoch = nextEpoch
			default:
				// This should not happen.
				log.Errorf("Time %d does not fit into current or next epoch!",
					sTime.Unix())
				s.errChan <- ErrEpochMissed
				continue
			}

			s.rec.order.SetTime(sTime.Unix())
			m.processOrder(s.rec, orderEpoch, s.errChan)

		case <-epochCycle:
			cycleEpoch()
		}
	}

}

func (m *Market) processOrder(rec *orderRecord, epoch *EpochQueue, errChan chan<- error) {
	// Verify that the order is not already in the epoch queue
	ord := rec.order
	if epoch.Orders[ord.ID()] != nil {
		// TODO: remove this check when we are sure the order router is behaving
		log.Errorf("Received duplicate order %v!", ord)
		errChan <- ErrDuplicateOrder
		return
	}

	// Sign the order and prepare the client response. Only after the archiver
	// has successfully stored the new epoch order should the order be committed
	// for processing.
	respMsg, err := m.orderResponse(rec, uint64(epoch.Epoch), uint64(epoch.Duration))
	if err != nil {
		log.Errorf("failed to create msgjson.Message for order %v, msgID %v response: %v",
			rec.order, rec.msgID, err)
		errChan <- ErrMalformedOrderResponse
		return
	}

	// Archive the new epoch order BEFORE inserting it into the epoch queue,
	// initiating the swap, and notifying book subscribers.
	if err := m.storage.NewEpochOrder(ord); err != nil {
		log.Errorf("(*Market).runEpochs: Failed to store new epoch order %v: %v",
			ord, err)
		errChan <- ErrInternalServer
		m.cancel()
		return
	}

	// Insert the order into the epoch queue.
	epoch.Insert(ord)
	errChan <- nil

	// Inform the client that the order has been received, stamped, signed, and
	// inserted into the current epoch queue.
	m.auth.Send(ord.User(), respMsg)

	// Send epoch update to order book subscribers.
	go m.notifyNewOrder(ord, epochAction)
}

// processEpoch performs the following operations for a closed epoch. The
// EpochQueue's Orders map must not be modified by another goroutine:
//  1. Perform matching with the order book.
//  2. Send book and unbook notifications to the book subscribers.
//  3. Initiate the swap negotiation via the Market's Swapper.
func (m *Market) processEpoch(epoch *EpochQueue) {
	// Perform the matching. The matcher updates the order book.
	orders := epoch.OrderSlice()
	m.bookMtx.Lock()
	matches, _, failed, partial, booked, unbooked := m.matcher.Match(m.book, orders)
	m.bookMtx.Unlock()
	log.Debugf("Matching complete for epoch %d:"+
		" %d matches (%d partial fills), %d booked, %d unbooked, %d failed",
		epoch.Epoch, len(matches), len(partial), len(booked), len(unbooked), len(failed),
	)

	// Set the EpochID for each MatchSet before initiating the swaps.
	for i := range matches {
		matches[i].Epoch.Idx = uint64(epoch.Epoch)
		matches[i].Epoch.Dur = uint64(epoch.Duration)
	}

	// Send "book" notifications to order book subscribers.
	for _, ord := range booked {
		m.notifyNewOrder(ord, bookAction)
	}

	// Send "unbook" notifications to order book subscribers.
	for _, ord := range unbooked {
		m.notifyNewOrder(ord, unbookAction)
	}

	// Initiate the swap.
	if len(matches) > 0 {
		log.Debugf("Negotiating %d matches for epoch %d:%d", len(matches),
			epoch.Epoch, epoch.Duration)
		go m.swapper.Negotiate(matches)
	}
}

// validateOrder used db.ValidateOrder to ensure that the provided order is
// valid for the current market with epoch order status.
func (m *Market) validateOrder(ord order.Order) bool {
	return db.ValidateOrder(ord, order.OrderStatusEpoch, m.marketInfo)
}

// orderResponse signs the order data and prepares the OrderResult to be sent to
// the client.
func (m *Market) orderResponse(oRecord *orderRecord, epochIndex, epochDuration uint64) (*msgjson.Message, error) {
	// Add the server timestamp.
	stamp := uint64(oRecord.order.Time())
	oRecord.req.Stamp(stamp, epochIndex, epochDuration)

	// Sign the serialized order request.
	m.auth.Sign(oRecord.req)

	// Prepare the OrderResult, including the server signature and time stamp.
	oid := oRecord.order.ID()
	res := &msgjson.OrderResult{
		Sig:        oRecord.req.SigBytes(),
		ServerTime: stamp,
		OrderID:    oid[:],
		EpochIdx:   epochIndex,
		EpochDur:   epochDuration,
	}

	// Encode the order response as a message for the client.
	return msgjson.NewResponse(oRecord.msgID, res, nil)
}
