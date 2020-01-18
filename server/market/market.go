// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
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
	TxMonitored(user account.AccountID, asset uint32, txid string) bool
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
	// Communications.
	orderRouter  chan *orderUpdateSignal  // incoming orders
	orderFeeds   []chan *bookUpdateSignal // outgoing notifications
	orderFeedMtx sync.RWMutex

	// Order handling: book, active epoch queues, order matcher, and swapper.
	running       chan struct{}
	startEpochIdx int64
	marketInfo    *dex.MarketInfo
	bookMtx       sync.Mutex
	book          *book.Book
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

// NewMarket creates a new Market for the provided base and quote assets, with
// an epoch cycling at given duration in milliseconds.
func NewMarket(mktInfo *dex.MarketInfo, storage db.DEXArchivist, swapper Swapper, authMgr AuthManager,
	coinLockerBase, coinLockerQuote coinlock.CoinLocker) (*Market, error) {
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

	return &Market{
		running:         make(chan struct{}),
		marketInfo:      mktInfo,
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

// SetStartEpochIdx sets the starting epoch index. This should generally be
// called before Run, or Start used to specify the index at the same time.
func (m *Market) SetStartEpochIdx(startEpochIdx int64) {
	atomic.StoreInt64(&m.startEpochIdx, startEpochIdx)
}

// StartEpochIdx gets the starting epoch index.
func (m *Market) StartEpochIdx() int64 {
	return atomic.LoadInt64(&m.startEpochIdx)
}

// Start begins order processing with a starting epoch index. See also
// SetStartEpochIdx and Run. Stop the Market by cancelling the context.
func (m *Market) Start(ctx context.Context, startEpochIdx int64) {
	m.SetStartEpochIdx(startEpochIdx)
	m.Run(ctx)
}

// waitForEpochOpen waits until the start of epoch processing.
func (m *Market) waitForEpochOpen() {
	<-m.running
}

// EpochDuration returns the Market's epoch duration in milliseconds.
func (m *Market) EpochDuration() uint64 {
	return m.marketInfo.EpochDuration
}

// MarketBuyBuffer returns the Market's market-buy buffer.
func (m *Market) MarketBuyBuffer() float64 {
	return m.marketInfo.MarketBuyBuffer
}

// OrderFeed provides a new order book update channel. This is not thread-safe,
// and should not be called after calling Start.
func (m *Market) OrderFeed() <-chan *bookUpdateSignal {
	bookUpdates := make(chan *bookUpdateSignal, 1)
	m.orderFeedMtx.Lock()
	m.orderFeeds = append(m.orderFeeds, bookUpdates)
	m.orderFeedMtx.Unlock()
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

// TxMonitored checks if a user's transaction for a certain asset is being
// monitored by the Swapper.
func (m *Market) TxMonitored(user account.AccountID, asset uint32, txid string) bool {
	return m.swapper.TxMonitored(user, asset, txid)
}

// SubmitOrderAsync submits a new order for inclusion into the current epoch.
// When submission is completed, an error value will be sent on the channel.
// This is the asynchronous version of SubmitOrder.
func (m *Market) SubmitOrderAsync(rec *orderRecord) <-chan error {
	// Validate the order. The order router must do it's own validation, but do
	// a second validation for (1) this Market and (2) epoch status, before
	// putting it on the queue.
	if !m.validateOrder(rec.order) {
		// Order ID may not be computed since ServerTime has not been set.
		log.Debugf("SubmitOrderAsync: Invalid order received: %x", rec.order.Serialize())
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

func (m *Market) notifyNewOrder(ctx context.Context, order order.Order, action bookUpdateAction, eidx int64) {
	// send to each receiver
	m.orderFeedMtx.RLock()
	defer m.orderFeedMtx.RUnlock()
	if ctx.Err() != nil {
		return
	}
	for _, s := range m.orderFeeds {
		select {
		case <-ctx.Done():
			return
		case s <- &bookUpdateSignal{action, order, eidx}:
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

// Run is the main order processing loop, which takes new orders, notifies book
// subscribers, and cycles the epochs. The caller should cancel the provided
// Context to stop the market. When Run returns, all book order feeds obtained
// via OrderFeed are closed and invalidated. Clients must request a new feed to
// receive updates when and if the Market restarts.
func (m *Market) Run(ctx context.Context) {
	nextEpochIdx := atomic.LoadInt64(&m.startEpochIdx)
	if nextEpochIdx == 0 {
		log.Warnf("Run: startEpochIdx not set. Starting at the next epoch.")
		now := encode.UnixMilli(time.Now())
		nextEpochIdx = 1 + now/int64(m.EpochDuration())
	}
	epochDuration := int64(m.marketInfo.EpochDuration)
	nextEpoch := NewEpoch(nextEpochIdx, epochDuration)
	epochCycle := time.After(time.Until(nextEpoch.Start))

	defer func() {
		m.orderFeedMtx.Lock()
		for _, s := range m.orderFeeds {
			close(s)
		}
		m.orderFeeds = nil
		m.orderFeedMtx.Unlock()
		log.Debugf("Market %q stopped.", m.marketInfo.Name)
	}()

	var running bool
	var currentEpoch *EpochQueue
	cycleEpoch := func() {
		if currentEpoch != nil {
			// Process the epoch synchronously so the coin locks are up-to-date
			// when the next order arrives.
			m.processEpoch(ctx, currentEpoch)
		}
		currentEpoch = nextEpoch

		nextEpoch = NewEpoch(currentEpoch.Epoch+1, epochDuration)
		epochCycle = time.After(time.Until(nextEpoch.Start))

		if !running {
			close(m.running)
			running = true
		}
	}

	// In case the market is stopped before the first epoch, close the running
	// channel so that waitForEpochOpen does not hang.
	defer func() {
		if !running {
			close(m.running)
		}
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		if err := m.storage.LastErr(); err != nil {
			log.Criticalf("Archivist failing. Last unexpected error: %v", err)
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
		case <-ctx.Done():
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
			err := m.processOrder(ctx, s.rec, orderEpoch, s.errChan)
			if err != nil {
				log.Errorf("Failed to process order %v: %v", s.rec.order, err)
				return
			}

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
	if o.Trade().Trade().Sell {
		locker = m.coinLockerBase
	}

	// Check if this order is known by the locker.
	lockedCoins := locker.OrderCoinsLocked(o.ID())
	if len(lockedCoins) > 0 {
		return lockedCoins
	}

	// Check the individual coins.
	for _, coin := range o.Trade().Coins {
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

	if o.Trade().Sell {
		m.coinLockerBase.LockOrdersCoins([]order.Order{o})
	} else {
		m.coinLockerQuote.LockOrdersCoins([]order.Order{o})
	}
}

func (m *Market) unlockOrderCoins(o order.Order) {
	if o.Type() == order.CancelOrderType {
		return
	}

	if o.Trade().Sell {
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
func (m *Market) processOrder(ctx context.Context, rec *orderRecord, epoch *EpochQueue, errChan chan<- error) error {
	// Verify that the order is not already in the epoch queue
	ord := rec.order
	if epoch.Orders[ord.ID()] != nil {
		// TODO: remove this check when we are sure the order router is behaving
		log.Errorf("Received duplicate order %v!", ord)
		errChan <- ErrDuplicateOrder
		return nil
	}

	// Sign the order and prepare the client response. Only after the archiver
	// has successfully stored the new epoch order should the order be committed
	// for processing.
	respMsg, err := m.orderResponse(rec, uint64(epoch.Epoch), uint64(epoch.Duration))
	if err != nil {
		log.Errorf("failed to create msgjson.Message for order %v, msgID %v response: %v",
			rec.order, rec.msgID, err)
		errChan <- ErrMalformedOrderResponse
		return nil
	}

	// Ensure that the received order does not use locked coins.
	lockedCoins := m.coinsLocked(ord)
	if len(lockedCoins) > 0 {
		log.Errorf("processOrder: Order %v submitted with already-locked coins: %v",
			ord, lockedCoins)
		errChan <- ErrInvalidOrder
		return nil
	}

	// For market and limit orders, lock the backing coins NOW so orders using
	// locked coins cannot get into the epoch queue. Later, in processEpoch or
	// the Swapper, release these coins when the swap is completed.
	m.lockOrderCoins(ord)

	// Archive the new epoch order BEFORE inserting it into the epoch queue,
	// initiating the swap, and notifying book subscribers.
	if err := m.storage.NewEpochOrder(ord); err != nil {
		errChan <- ErrInternalServer
		return fmt.Errorf("processOrder: Failed to store new epoch order %v: %v",
			ord, err)
	}

	// Insert the order into the epoch queue.
	epoch.Insert(ord)

	m.epochMtx.Lock()
	m.epochOrders[ord.ID()] = ord
	m.epochMtx.Unlock()

	// Respond to the order router only after updating epochOrders so that
	// Cancelable will reflect that the order is now in the epoch queue.
	errChan <- nil

	// Inform the client that the order has been received, stamped, signed, and
	// inserted into the current epoch queue.
	m.auth.Send(ord.User(), respMsg)

	// Send epoch update to epoch queue subscribers.
	go m.notifyNewOrder(ctx, ord, epochAction, epoch.Epoch)
	return nil
}

// processEpoch performs the following operations for a closed epoch:
//  1. Perform matching with the order book.
//  2. Send book and unbook notifications to the book subscribers.
//  3. Unlock coins with the book lock for unbooked and failed orders.
//  4. Lock coins with the swap lock.
//  5. Initiate the swap negotiation via the Market's Swapper.
// The EpochQueue's Orders map must not be modified by another goroutine.
func (m *Market) processEpoch(ctx context.Context, epoch *EpochQueue) {
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
			//m.auth.RecordCancel(match.Taker.User())
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
		m.notifyNewOrder(ctx, ord, bookAction, epoch.Epoch)
	}

	// Send "unbook" notifications to order book subscribers.
	for _, ord := range unbooked {
		m.notifyNewOrder(ctx, ord, unbookAction, epoch.Epoch)
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
	err := m.auth.Sign(oRecord.req)
	if err != nil {
		return nil, err
	}

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
