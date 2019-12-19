// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
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
	LockCoins(asset uint32, coins map[order.OrderID][]order.CoinID)
	LockOrdersCoins(orders []order.Order)
}

// Market is the market manager. It should not be overly involved with details
// of accounts and authentication. Via the account package it should request
// account status with new orders, verification of order signatures. The Market
// should also perform various account package callbacks such as order status
// updates so that the account package code can keep various data up-to-date,
// including order status, history, cancellation statistics, etc.
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
	epochMtx      sync.RWMutex
	epochOrders   map[order.OrderID]order.Order
	matcher       *matcher.Matcher
	swapper       Swapper
	auth          AuthManager

	coinLockerBase  coinlock.CoinLocker
	coinLockerQuote coinlock.CoinLocker

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

// MidGap returns the mid-gap market rate, which is ths rate halfway between the
// best buy order and the best sell order in the order book. If one side has no
// orders, the best order rate on other side is returned. If both sides have no
// orders, 0 is returned.
func (m *Market) MidGap() uint64 {
	bestBuy, bestSell := m.book.Best()
	if bestBuy == nil {
		if bestSell == nil {
			return 0 // ?
		}
		return bestSell.Rate
	} else if bestSell == nil {
		return bestBuy.Rate
	}
	return (bestBuy.Rate + bestSell.Rate) / 2
}

// CoinLocked checks if a coin is locked. The asset is specified since we should
// not assume that a CoinID for one asset cannot be made to match another
// asset's CoinID.
func (m *Market) CoinLocked(asset uint32, coin coinlock.CoinID) bool {
	switch asset {
	case m.marketInfo.Base:
		return m.coinLockerBase.CoinLocked(coin)
	case m.marketInfo.Quote:
		return m.coinLockerQuote.CoinLocked(coin)
	default:
		panic(fmt.Sprintf("invalid asset %d for market %s", asset, m.marketInfo.Name))
	}
}

// Cancelable determines if an order is a limit order with time-in-force
// standing that is in either the epoch queue or in the order book.
func (m *Market) Cancelable(oid order.OrderID) bool {
	// All book orders are standing limit orders.
	if m.book.HaveOrder(oid) {
		return true
	}

	// Check the active epochs (includes current and next).
	m.epochMtx.RLock()
	ord := m.epochOrders[oid]
	m.epochMtx.RUnlock()

	switch o := ord.(type) {
	case *order.LimitOrder:
		return o.Force == order.StandingTiF
	}
	return false
}

// NewMarket creates a new Market for the provided base and quote assets, with
// an epoch cycling at given duration in milliseconds.
func NewMarket(ctx context.Context, mktInfo *dex.MarketInfo, storage db.DEXArchivist,
	swapper Swapper, authMgr AuthManager, coinLockerBase, coinLockerQuote coinlock.CoinLocker) (*Market, error) {
	// Make sure the DEXArchivist is healthy before taking orders.
	if err := storage.LastErr(); err != nil {
		return nil, err
	}

	// Lock coins backing active orders.
	baseCoins, quoteCoins, err := storage.ActiveOrderCoins(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		return nil, err
	}
	// TODO: first check that the coins aren't already locked by e.g. another market.
	coinLockerBase.LockCoins(baseCoins)
	coinLockerQuote.LockCoins(quoteCoins)

	// Supplement the provided context with a cancel function. The Stop method
	// may be used to stop the market, or the parent context itself may be
	// canceled.
	ctxInternal, cancel := context.WithCancel(ctx)

	return &Market{
		ctx:             ctxInternal,
		cancel:          cancel,
		running:         make(chan struct{}),
		marketInfo:      mktInfo,
		epochDuration:   int64(mktInfo.EpochDuration),
		orderRouter:     make(chan *orderUpdateSignal, 16),
		book:            book.New(mktInfo.LotSize),
		matcher:         matcher.New(),
		epochOrders:     make(map[order.OrderID]order.Order),
		swapper:         swapper,
		auth:            authMgr,
		storage:         storage,
		coinLockerBase:  coinLockerBase,
		coinLockerQuote: coinLockerQuote,
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
			// Process the epoch synchronously so the coin locks are up-to-date
			// when the next order arrives.
			m.processEpoch(currentEpoch)
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
			sTime := time.Now().Truncate(time.Millisecond).UTC()

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
					sTime.UnixNano())
				s.errChan <- ErrEpochMissed
				continue
			}

			// Stamp and process the order in the target epoch queue.
			s.rec.order.SetTime(sTime)
			m.processOrder(s.rec, orderEpoch, s.errChan)

		case <-epochCycle:
			cycleEpoch()
		}
	}

}

func (m *Market) coinsLocked(o order.Order) []order.CoinID {
	if o.Type() == order.CancelOrderType {
		return nil
	}

	locker := m.coinLockerQuote
	if o.IsSell() {
		locker = m.coinLockerBase
	}

	// Check if this order is known by the locker.
	lockedCoins := locker.OrderCoinsLocked(o.ID())
	if len(lockedCoins) > 0 {
		return lockedCoins
	}

	// Check the individual coins.
	for _, coin := range o.CoinIDs() {
		if locker.CoinLocked(coin) {
			lockedCoins = append(lockedCoins, coin)
		}
	}
	return lockedCoins
}

func (m *Market) lockOrderCoins(o order.Order) {
	if o.Type() == order.CancelOrderType {
		return
	}

	if o.IsSell() {
		m.coinLockerBase.LockOrdersCoins([]order.Order{o})
	} else {
		m.coinLockerQuote.LockOrdersCoins([]order.Order{o})
	}
}

func (m *Market) unlockOrderCoins(o order.Order) {
	if o.Type() == order.CancelOrderType {
		return
	}

	if o.IsSell() {
		m.coinLockerBase.UnlockOrderCoins(o.ID())
	} else {
		m.coinLockerQuote.UnlockOrderCoins(o.ID())
	}
}

// processOrder performs the following actions:
// 1. Verify the order is new and that none of the backing coins are locked.
// 2. Lock the order's coins.
// 3. Store the order in the DB.
// 4. Insert the order into the EpochQueue.
// 5. Respond to the client that placed the order.
// 6. Notify epoch queue event subscribers.
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

	// Ensure that the received order does not use locked coins.
	lockedCoins := m.coinsLocked(ord)
	if len(lockedCoins) > 0 {
		log.Errorf("(*Market).runEpochs: Order %v submitted with already-locked coins: %v",
			ord, lockedCoins)
		errChan <- ErrInvalidOrder
		return
	}

	// For market and limit orders, lock the backing coins NOW so orders using
	// locked coins cannot get into the epoch queue. Later, in processEpoch or
	// the Swapper, release these coins when the swap is completed.
	m.lockOrderCoins(ord)

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

	m.epochMtx.Lock()
	m.epochOrders[ord.ID()] = ord
	m.epochMtx.Unlock()

	// Inform the client that the order has been received, stamped, signed, and
	// inserted into the current epoch queue.
	m.auth.Send(ord.User(), respMsg)

	// Send epoch update to epoch queue subscribers.
	go m.notifyNewOrder(ord, epochAction)
}

// processEpoch performs the following operations for a closed epoch:
//  1. Perform matching with the order book.
//  2. Send book and unbook notifications to the book subscribers.
//  3. Unlock coins with the book lock for unbooked and failed orders.
//  4. Lock coins with the swap lock.
//  5. Initiate the swap negotiation via the Market's Swapper.
// The EpochQueue's Orders map must not be modified by another goroutine.
func (m *Market) processEpoch(epoch *EpochQueue) {
	// Perform the matching. The matcher updates the order book.
	orders := epoch.OrderSlice()
	m.epochMtx.Lock()
	for _, ord := range orders {
		delete(m.epochOrders, ord.ID())
	}
	m.epochMtx.Unlock()

	m.bookMtx.Lock()
	matches, _, failed, doneOK, partial, booked, unbooked := m.matcher.Match(m.book, orders)
	m.bookMtx.Unlock()
	log.Debugf("Matching complete for epoch %d:"+
		" %d matches (%d partial fills), %d completed OK (not booked),"+
		" %d booked, %d unbooked, %d failed",
		epoch.Epoch,
		len(matches), len(partial), len(doneOK),
		len(booked), len(unbooked), len(failed),
	)

	// Pre-process the matches:
	// - Set the EpochID for each MatchSet.
	// - Identify taker orders with backing coins that the swapper will need to
	//   lock and unlock.
	swapOrders := make([]order.Order, 0, 2*len(matches)) // size guess
	for _, match := range matches {
		// Set the epoch ID.
		match.Epoch.Idx = uint64(epoch.Epoch)
		match.Epoch.Dur = uint64(epoch.Duration)

		// The order targeted by a matched cancel order will be in the unbooked
		// slice. These coins are unlocked next in the book locker.
		if match.Taker.Type() == order.CancelOrderType {
			continue
		}
		swapOrders = append(swapOrders, match.Taker)

		for _, maker := range match.Makers {
			swapOrders = append(swapOrders, maker)
		}
	}

	// Unlock passed but not booked order (e.g. matched market and immediate
	// orders) coins were locked upon order receipt in processOrder and must be
	// unlocked now since they do not go on the book.
	for _, k := range doneOK {
		m.unlockOrderCoins(k)
	}

	// Unlock unmatched (failed) order coins.
	for _, fo := range failed {
		m.unlockOrderCoins(fo)
	}

	// Booked order coins were locked upon receipt by processOrder, and remain
	// locked until they are either: unbooked by a future match that completely
	// fills the order, unbooked by a matched cancel order, or (unimplemented)
	// unbooked by another Market mechanism such as client disconnect or ban.

	// Unlock unbooked order coins.
	for _, ubo := range unbooked {
		m.unlockOrderCoins(ubo)
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
		// Since Swapper.Negotiate is called asynchronously, we must lock coins
		// with the Swapper first. The swapper will unlock the coins.
		m.swapper.LockOrdersCoins(swapOrders)
		go m.swapper.Negotiate(matches)
	}
}

// validateOrder uses db.ValidateOrder to ensure that the provided order is
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
