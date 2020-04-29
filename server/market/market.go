// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/ws"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
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
	ErrInvalidCommitment      = Error("order commitment invalid")
	ErrEpochMissed            = Error("order unexpectedly missed its intended epoch")
	ErrDuplicateOrder         = Error("order already in epoch") // maybe remove since this is ill defined
	ErrDuplicateCancelOrder   = Error("equivalent cancel order already in epoch")
	ErrInvalidCancelOrder     = Error("cancel order account does not match targeted order account")
	ErrMalformedOrderResponse = Error("malformed order response")
	ErrInternalServer         = Error("internal server error")
)

// Swapper coordinates atomic swaps for one or more matchsets.
type Swapper interface {
	Negotiate(matchSets []*order.MatchSet, offBook map[order.OrderID]bool)
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
	marketInfo *dex.MarketInfo

	// Communications.
	orderRouter  chan *orderUpdateSignal  // incoming orders
	orderFeedMtx sync.RWMutex             // guards orderFeeds and running
	orderFeeds   []chan *bookUpdateSignal // outgoing notifications
	running      chan struct{}

	startEpochIdx int64 // atomic access only

	bookMtx  sync.Mutex // guards book and epochIdx
	book     *book.Book
	epochIdx int64 // next epoch from the point of view of the book

	epochMtx         sync.RWMutex
	epochCommitments map[order.Commitment]order.OrderID
	epochOrders      map[order.OrderID]order.Order

	matcher *matcher.Matcher
	swapper Swapper
	auth    AuthManager

	coinLockerBase  coinlock.CoinLocker
	coinLockerQuote coinlock.CoinLocker

	// Persistent data storage
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
	log.Debugf("Locking %d base asset coins.", len(baseCoins))
	for oid, coins := range baseCoins {
		log.Tracef(" - order %v: %v", oid, coins)
	}
	coinLockerBase.LockCoins(baseCoins)
	log.Debugf("Locking %d quote asset coins.", len(quoteCoins))
	for oid, coins := range quoteCoins {
		log.Tracef(" - order %v: %v", oid, coins)
	}
	coinLockerQuote.LockCoins(quoteCoins)
	// TODO: fail and archive orders that are in orderStatusEpoch, and unlock
	// their coins.

	// Load existing book orders from the DB.
	bookOrders, err := storage.BookOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		return nil, err
	}

	Book := book.New(mktInfo.LotSize)
	for _, lo := range bookOrders {
		if ok := Book.Insert(lo); !ok {
			// This can only happen if one of the loaded orders has an
			// incompatible lot size for the current market config.
			log.Errorf("Failed to insert order %v into %v book.", mktInfo.Name, lo)
		}
	}

	return &Market{
		running:          make(chan struct{}),
		marketInfo:       mktInfo,
		orderRouter:      make(chan *orderUpdateSignal, 16),
		book:             Book,
		matcher:          matcher.New(),
		epochCommitments: make(map[order.Commitment]order.OrderID),
		epochOrders:      make(map[order.OrderID]order.Order),
		swapper:          swapper,
		auth:             authMgr,
		storage:          storage,
		coinLockerBase:   coinLockerBase,
		coinLockerQuote:  coinLockerQuote,
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
	m.orderFeedMtx.RLock()
	c := m.running
	m.orderFeedMtx.RUnlock()
	<-c
}

// EpochDuration returns the Market's epoch duration in milliseconds.
func (m *Market) EpochDuration() uint64 {
	return m.marketInfo.EpochDuration
}

// MarketBuyBuffer returns the Market's market-buy buffer.
func (m *Market) MarketBuyBuffer() float64 {
	return m.marketInfo.MarketBuyBuffer
}

// Base is the base asset ID.
func (m *Market) Base() uint32 {
	return m.marketInfo.Base
}

// Quote is the quote asset ID.
func (m *Market) Quote() uint32 {
	return m.marketInfo.Quote
}

// OrderFeed provides a new order book update channel. Channels provided before
// the market starts and while a market is running are both valid. When the
// market stops, channels are closed (invalidated), and new channels should be
// requested if the market starts again.
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
	if err := m.validateOrder(rec.order); err != nil {
		// Order ID may not be computed since ServerTime has not been set.
		log.Debugf("SubmitOrderAsync: Invalid order received: %x: %v", rec.order.Serialize(), err)
		errChan := make(chan error, 1)
		errChan <- err // i.e. ErrInvalidOrder, ErrInvalidCommitment
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
			return 0
		}
		return bestSell.Rate
	} else if bestSell == nil {
		return bestBuy.Rate
	}
	return (bestBuy.Rate + bestSell.Rate) / 2 // note downward bias on truncate
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

// CancelableBy determines if an order is cancelable by a certain account. This
// means: (1) an order in the book or epoch queue, (2) type limit with
// time-in-force standing (implied for book orders), and (3) AccountID field
// matching the provided account ID.
func (m *Market) CancelableBy(oid order.OrderID, aid account.AccountID) bool {
	// All book orders are standing limit orders.
	if lo := m.book.Order(oid); lo != nil {
		return lo.AccountID == aid
	}

	// Check the active epochs (includes current and next).
	m.epochMtx.RLock()
	ord := m.epochOrders[oid]
	m.epochMtx.RUnlock()

	switch o := ord.(type) {
	case *order.LimitOrder:
		return o.Force == order.StandingTiF && o.AccountID == aid
	}
	return false
}

func (m *Market) notify(ctx context.Context, sig *bookUpdateSignal) {
	// send to each receiver
	m.orderFeedMtx.RLock()
	defer m.orderFeedMtx.RUnlock()
	// The market may have shut down while waiting for the lock.
	if ctx.Err() != nil {
		return
	}
	for _, s := range m.orderFeeds {
		select {
		case <-ctx.Done():
			return
		case s <- sig:
		}
	}
}

// Book retrieves the market's current order book and the current epoch index.
// If the Market is not yet running or the start epoch has not yet begun, the
// epoch index will be zero.
func (m *Market) Book() (epoch int64, buys, sells []*order.LimitOrder) {
	// NOTE: it may be desirable to cache the response.
	m.bookMtx.Lock()
	buys = m.book.BuyOrders()
	sells = m.book.SellOrders()
	epoch = m.epochIdx
	m.bookMtx.Unlock()
	return
}

// Run is the main order processing loop, which takes new orders, notifies book
// subscribers, and cycles the epochs. The caller should cancel the provided
// Context to stop the market. When Run returns, all book order feeds obtained
// via OrderFeed are closed and invalidated. Clients must request a new feed to
// receive updates when and if the Market restarts.
func (m *Market) Run(ctx context.Context) {
	ctxRun, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start the closed epoch pump, which drives preimage collection and orderly
	// epoch processing.
	eq := newEpochPump()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		eq.Run(ctxRun)
	}()

	// Start the closed epoch processing pipeline.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case ep, ok := <-eq.ready:
				if !ok {
					return
				}

				// epochStart has completed preimage collection.
				m.processReadyEpoch(ctxRun, ep)
			case <-ctxRun.Done():
				return
			}
		}
	}()

	nextEpochIdx := atomic.LoadInt64(&m.startEpochIdx)
	if nextEpochIdx == 0 {
		log.Warnf("Run: startEpochIdx not set. Starting at the next epoch.")
		now := encode.UnixMilli(time.Now())
		nextEpochIdx = 1 + now/int64(m.EpochDuration())
	}
	epochDuration := int64(m.marketInfo.EpochDuration)
	nextEpoch := NewEpoch(nextEpochIdx, epochDuration)
	epochCycle := time.After(time.Until(nextEpoch.Start))

	var running bool
	var currentEpoch *EpochQueue
	cycleEpoch := func() {
		if currentEpoch != nil {
			// Process the epoch asynchronously since there is a delay while the
			// preimages are requested and clients respond with their preimages.
			m.enqueueEpoch(eq, currentEpoch)

			// The epoch is closed, long live the epoch.
			sig := &bookUpdateSignal{
				action:   newEpochAction,
				epochIdx: nextEpoch.Epoch,
			}
			m.notify(ctxRun, sig)
		}
		currentEpoch = nextEpoch

		nextEpoch = NewEpoch(currentEpoch.Epoch+1, epochDuration)
		epochCycle = time.After(time.Until(nextEpoch.Start))

		if !running {
			close(m.running) // no lock, this field is not set in another goroutine
			running = true
		}
	}

	defer func() {
		m.orderFeedMtx.Lock()
		for _, s := range m.orderFeeds {
			close(s)
		}
		m.orderFeeds = nil
		// In case the market is stopped before the first epoch, close the
		// running channel so that waitForEpochOpen does not hang.
		if !running {
			close(m.running)
		}
		// Make a new running channel for any future Run.
		m.running = make(chan struct{}) // also guarded in OrderFeed and waitForEpochOpen
		m.orderFeedMtx.Unlock()

		wg.Wait()

		log.Debugf("Market %q stopped.", m.marketInfo.Name)
	}()

	for {
		if ctxRun.Err() != nil {
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
		case <-ctxRun.Done():
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
			err := m.processOrder(ctxRun, s.rec, orderEpoch, s.errChan)
			if err != nil {
				if ctxRun.Err() == nil {
					// This was not context cancellation.
					log.Errorf("Failed to process order %v: %v", s.rec.order, err)
					// Signal to the other Run goroutines to return.
					cancel()
				}
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
	// Verify that an order with the same commitment is not already in the epoch
	// queue. Since commitment is part of the order serialization and thus order
	// ID, this also prevents orders with the same ID.
	// TODO: Prevent commitment reuse in general, without expensive DB queries.
	ord := rec.order
	commit := ord.Commitment()
	m.epochMtx.RLock()
	otherOid, found := m.epochCommitments[commit]
	m.epochMtx.RUnlock()
	if found {
		log.Debugf("Received order %v with commitment %x also used in previous order %v!",
			ord, commit, otherOid)
		errChan <- ErrInvalidCommitment
	}

	// Verify that another cancel order targeting the same order is not already
	// in the epoch queue. Market and limit orders using the same coin IDs as
	// other orders is prevented by the coinlocker.
	if co, ok := ord.(*order.CancelOrder); ok {
		if eco := epoch.CancelTargets[co.TargetOrderID]; eco != nil {
			log.Debugf("Received cancel order %v targeting %v, but already have %v.",
				co, co.TargetOrderID, eco)
			errChan <- ErrDuplicateCancelOrder
			return nil
		}

		// Verify that the target order is on the books or in the epoch queue,
		// and that the account of the CancelOrder is the same as the account of
		// the target order.
		if !m.CancelableBy(co.TargetOrderID, co.AccountID) {
			log.Debugf("Cancel order %v (account=%v) does not own target order %v.",
				co, co.AccountID, co.TargetOrderID)
			errChan <- ErrInvalidCancelOrder
			return nil
		}
	}

	// Sign the order and prepare the client response. Only after the archiver
	// has successfully stored the new epoch order should the order be committed
	// for processing.
	respMsg, err := m.orderResponse(rec)
	if err != nil {
		log.Errorf("failed to create msgjson.Message for order %v, msgID %v response: %v",
			rec.order, rec.msgID, err)
		errChan <- ErrMalformedOrderResponse
		return nil
	}

	// Ensure that the received order does not use locked coins.
	lockedCoins := m.coinsLocked(ord)
	if len(lockedCoins) > 0 {
		log.Debugf("processOrder: Order %v submitted with already-locked coins: %v",
			ord, lockedCoins)
		errChan <- ErrInvalidOrder
		return nil
	}

	// For market and limit orders, lock the backing coins NOW so orders using
	// locked coins cannot get into the epoch queue. Later, in processEpoch or
	// the Swapper, release these coins when the swap is completed.
	m.lockOrderCoins(ord)

	// Check for known orders in the DB with the same Commitment.
	//
	// NOTE: This is disabled since (1) it may not scale as order history grows,
	// and (2) it is hard to see how this can be done by new servers in a mesh.
	// NOTE 2: Perhaps a better check would be commits with revealed preimages,
	// since a dedicated commit->preimage map or DB is conceivable.
	//
	// commitFound, prevOrderID, err := m.storage.OrderWithCommit(ctx, commit)
	// if err != nil {
	// 	errChan <- ErrInternalServer
	// 	return fmt.Errorf("processOrder: Failed to query for orders by commitment: %v", err)
	// }
	// if commitFound {
	// 	log.Debugf("processOrder: Order %v submitted with reused commitment %v "+
	// 		"from previous order %v", ord, commit, prevOrderID)
	// 	errChan <- ErrInvalidCommitment
	// 	return nil
	// }

	// Store the new epoch order BEFORE inserting it into the epoch queue,
	// initiating the swap, and notifying book subscribers.
	if err := m.storage.NewEpochOrder(ord, epoch.Epoch, epoch.Duration); err != nil {
		errChan <- ErrInternalServer
		return fmt.Errorf("processOrder: Failed to store new epoch order %v: %v",
			ord, err)
	}

	// Insert the order into the epoch queue.
	epoch.Insert(ord)

	oid := ord.ID()
	m.epochMtx.Lock()
	m.epochOrders[oid] = ord
	m.epochCommitments[commit] = oid
	m.epochMtx.Unlock()

	// Respond to the order router only after updating epochOrders so that
	// Cancelable will reflect that the order is now in the epoch queue.
	errChan <- nil

	// Inform the client that the order has been received, stamped, signed, and
	// inserted into the current epoch queue.
	m.auth.Send(ord.User(), respMsg)

	// Send epoch update to epoch queue subscribers.
	sig := &bookUpdateSignal{
		action:   epochAction,
		order:    ord,
		epochIdx: epoch.Epoch,
	}
	go m.notify(ctx, sig)
	return nil
}

func idToBytes(id [order.OrderIDSize]byte) []byte {
	return id[:]
}

// respondError sends an rpcError to a user.
func (m *Market) respondError(id uint64, user account.AccountID, code int, errMsg string) {
	log.Debugf("error going to user %x, code: %d, msg: %s", user, code, errMsg)
	msg, err := msgjson.NewResponse(id, nil, &msgjson.Error{
		Code:    code,
		Message: errMsg,
	})
	if err != nil {
		log.Errorf("error creating error response with message '%s': %v", msg, err)
	}
	m.auth.Send(user, msg)
}

// preimage request-response handling data
type piData struct {
	ord      order.Order
	preimage chan *order.Preimage
}

// handlePreimageResp is to be used in the response callback function provided
// to AuthManager.Request for the preimage route.
func (m *Market) handlePreimageResp(msg *msgjson.Message, reqData *piData) {
	sendPI := func(pi *order.Preimage) {
		reqData.preimage <- pi
	}

	var piResp msgjson.PreimageResponse
	resp, err := msg.Response()
	if err != nil {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.RPCParseError,
			fmt.Sprintf("error parsing preimage notification response: %v", err))
		return
	}
	err = json.Unmarshal(resp.Result, &piResp)
	if err != nil {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.RPCParseError,
			fmt.Sprintf("error parsing preimage notification response payload result: %v", err))
		return
	}

	// Validate preimage length.
	if len(piResp.Preimage) != order.PreimageSize {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.InvalidPreimage,
			fmt.Sprintf("invalid preimage length (%d byes)", len(piResp.Preimage)))
		return
	}

	// Check that the preimage is the hash of the order commitment.
	var pi order.Preimage
	copy(pi[:], piResp.Preimage)
	piCommit := pi.Commit()
	if reqData.ord.Commitment() != piCommit {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.PreimageCommitmentMismatch,
			fmt.Sprintf("preimage hash %x does not match order commitment %x",
				piCommit, reqData.ord.Commitment()))
		return
	}

	// The preimage is good.
	log.Tracef("Good preimage received for order %v: %x", reqData.ord.UID(), pi)
	err = m.storage.StorePreimage(reqData.ord, pi)
	if err != nil {
		log.Errorf("StorePreimage: %v", err)
		// Fatal backend error. New swaps will not begin, but pass the preimage
		// along so it does not appear as a miss to collectPreimages.
		m.respondError(msg.ID, reqData.ord.User(), msgjson.UnknownMarketError, "internal server error")
	}

	sendPI(&pi)
}

// collectPreimages solicits preimages from the owners of each of the orders in
// the provided queue with a 'preimage' ntfn/request via AuthManager.Request,
// and returns the preimages contained in the client responses. This function
// can block for up to 5 seconds (piTimeout) to allow clients time to respond.
// Clients that fail to respond, or respond with invalid data (see
// handlePreimageResp), are counted as misses.
func (m *Market) collectPreimages(orders []order.Order) (cSum []byte, ordersRevealed []*matcher.OrderRevealed, misses []order.Order) {
	// Compute the commitment checksum for the order queue.
	cSum = matcher.CSum(orders)

	// Request preimages from the clients.
	piTimeout := 5 * time.Second
	preimages := make(map[order.Order]chan *order.Preimage, len(orders))
	for _, ord := range orders {
		// Make the 'preimage' request.
		piReqParams := &msgjson.PreimageRequest{
			OrderID:        idToBytes(ord.ID()),
			CommitChecksum: cSum,
		}
		req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, piReqParams)
		if err != nil {
			// This is likely an impossible condition, but it's not the client's
			// fault.
			log.Errorf("error creating preimage request: %v", err)
			// TODO: respond to client with server error.
			continue
		}

		// The clients preimage response comes back via a channel, where nil
		// indicates client failure to respond, either due to disconnection or
		// no action.
		piChan := make(chan *order.Preimage)

		reqData := &piData{
			ord:      ord,
			preimage: piChan,
		}

		// Failure to respond in time is a miss, signalled by a nil pointer.
		miss := func() {
			piChan <- nil
		}

		// Send the preimage request to the order's owner.
		err = m.auth.RequestWithTimeout(ord.User(), req, func(_ comms.Link, msg *msgjson.Message) {
			m.handlePreimageResp(msg, reqData)
		}, piTimeout, miss)
		if err != nil {
			if errors.Is(err, ws.ErrPeerDisconnected) {
				misses = append(misses, ord)
				log.Debug("Preimage request failed: client gone.")
			} else {
				// Server error should not count as a miss. We may need a way to
				// identify server connectivity problems to clients are not
				// penalized when it is not their fault.
				log.Warnf("Preimage request failed: %v", err) // maybe Debugf if there is nothing unexpected
			}
			continue
		}

		log.Tracef("Preimage request sent for order %v", ord)
		preimages[ord] = piChan
	}

	// Receive preimages from response channels.
	for ord, pic := range preimages {
		pi := <-pic
		if pi == nil {
			misses = append(misses, ord)
		} else {
			ordersRevealed = append(ordersRevealed, &matcher.OrderRevealed{
				Order:    ord,
				Preimage: *pi,
			})
		}
	}

	return
}

type readyEpoch struct {
	*EpochQueue
	ready          chan struct{}
	cSum           []byte
	ordersRevealed []*matcher.OrderRevealed
	misses         []order.Order
}

type epochPump struct {
	ready chan *readyEpoch // consumer receives from this

	mtx  sync.RWMutex
	q    []*readyEpoch
	head chan *readyEpoch // internal
}

func newEpochPump() *epochPump {
	return &epochPump{
		ready: make(chan *readyEpoch, 1),
		head:  make(chan *readyEpoch, 1),
	}
}

func (ep *epochPump) Run(ctx context.Context) {
	defer close(ep.ready)
	for {
		rq, ok := <-ep.next(ctx)
		if !ok {
			return
		}
		select {
		case ep.ready <- rq: // consumer should receive this
		case <-ctx.Done():
			return
		}
	}
}

// Insert enqueues an EpochQueue and start's preimage collection immediately.
// Access epoch queues in order and when they have completed preimage collection
// by receiving from the epochPump.ready channel.
func (ep *epochPump) Insert(epoch *EpochQueue) *readyEpoch {
	rq := &readyEpoch{
		EpochQueue: epoch,
		ready:      make(chan struct{}),
	}
	ep.mtx.Lock()
	select {
	case ep.head <- rq: // buffered, so non-blocking when empty and no receiver
	default:
		ep.push(rq)
	}
	ep.mtx.Unlock()
	return rq
}

// push appends a new readyEpoch to the closed epoch queue, q. It is not
// thread-safe. push is only used in Insert.
func (ep *epochPump) push(rq *readyEpoch) {
	ep.q = append(ep.q, rq)
}

// popFront removes the next readyEpoch from the closed epoch queue, q. It is
// not thread-safe. pop is only used in next to advance the head of the pump.
func (ep *epochPump) popFront() *readyEpoch {
	if len(ep.q) == 0 {
		return nil
	}
	x := ep.q[0]
	ep.q = ep.q[1:]
	return x
}

// next provides a channel for receiving the next readyEpoch when it completes
// preimage collection. next blocks until there is an epoch to send.
func (ep *epochPump) next(ctx context.Context) <-chan *readyEpoch {
	var next *readyEpoch
	ready := make(chan *readyEpoch)
	select {
	case <-ctx.Done():
		close(ready)
		return ready
	case next = <-ep.head: // block if their is no next yet
	}

	ep.mtx.Lock()
	defer ep.mtx.Unlock()

	// If the queue is not empty, set new head.
	x := ep.popFront()
	if x != nil {
		ep.head <- x // non-blocking
	}

	// Send next on the returned channel when it becomes ready. If the process
	// dies before goroutine completion, the Market is down anyway.
	go func() {
		<-next.ready // block until preimage collection is complete
		ready <- next
	}()
	return ready
}

func (m *Market) enqueueEpoch(eq *epochPump, epoch *EpochQueue) {
	// Enqueue the epoch for matching when preimage collection is completed and
	// it is this epoch's turn.
	rq := eq.Insert(epoch)

	orders := epoch.OrderSlice()
	m.epochMtx.Lock()
	for _, ord := range orders {
		delete(m.epochOrders, ord.ID())
		delete(m.epochCommitments, ord.Commitment())
	}
	m.epochMtx.Unlock()

	// Start preimage collection.
	go func() {
		rq.cSum, rq.ordersRevealed, rq.misses = m.epochStart(orders)
		close(rq.ready)
	}()
}

// epochStart collects order preimages, and penalizes users who fail to respond.
func (m *Market) epochStart(orders []order.Order) (cSum []byte, ordersRevealed []*matcher.OrderRevealed, misses []order.Order) {
	// Solicit the preimages for each order.
	cSum, ordersRevealed, misses = m.collectPreimages(orders)
	if len(orders) > 0 {
		log.Infof("Collected %d valid order preimages, missed %d. Commit checksum: %x",
			len(ordersRevealed), len(misses), cSum)
	}

	// Penalize accounts with misses. TODO: consider if Penalize can be an async
	// function call.
	for _, ord := range misses {
		m.auth.Penalize(ord.User(), account.PreimageReveal)
	}

	return
}

// processReadyEpoch performs the following operations for a closed epoch that
// has finished preimage collection via collectPreimages:
//  1. Perform matching with the order book.
//  2. Send book and unbook notifications to the book subscribers.
//  3. Unlock coins with the book lock for unbooked and failed orders.
//  4. Lock coins with the swap lock.
//  5. Initiate the swap negotiation via the Market's Swapper.
// The EpochQueue's Orders map must not be modified by another goroutine.
func (m *Market) processReadyEpoch(ctx context.Context, epoch *readyEpoch) {
	// Ensure the epoch has actually completed preimage collection. This can
	// only fail if the epochPump malfunctioned. Remove this check eventually.
	select {
	case <-epoch.ready:
	default:
		log.Criticalf("preimages not yet collected for epoch %d!", epoch.Epoch)
		return // maybe panic
	}

	// Abort epoch processing if there was a fatal DB backend error during
	// preimage collection.
	if err := m.storage.LastErr(); err != nil {
		log.Criticalf("aborting epoch processing on account of failing DB: %v", err)
		return
	}

	// Data from preimage collection
	ordersRevealed := epoch.ordersRevealed
	cSum := epoch.cSum
	misses := epoch.misses

	// Perform order matching using the preimages to shuffle the queue.
	m.bookMtx.Lock()
	matchTime := time.Now() // considered as the time at which matched cancel orders are executed
	seed, matches, _, failed, doneOK, partial, booked, unbooked, updates := m.matcher.Match(m.book, ordersRevealed)
	m.epochIdx = epoch.Epoch + 1
	m.bookMtx.Unlock()
	if len(ordersRevealed) > 0 {
		log.Infof("Matching complete for market %v epoch %d:"+
			" %d matches (%d partial fills), %d completed OK (not booked),"+
			" %d booked, %d unbooked, %d failed",
			m.marketInfo.Name, epoch.Epoch,
			len(matches), len(partial), len(doneOK),
			len(booked), len(unbooked), len(failed),
		)
	}

	// Store data in epochs table, including matchTime so that cancel execution
	// times can be obtained from the DB for cancellation ratio computation.
	oidsRevealed := make([]order.OrderID, 0, len(ordersRevealed))
	for _, or := range ordersRevealed {
		oidsRevealed = append(oidsRevealed, or.Order.ID())
	}
	oidsMissed := make([]order.OrderID, 0, len(misses))
	for _, om := range misses {
		oidsMissed = append(oidsMissed, om.ID())
	}

	err := m.storage.InsertEpoch(&db.EpochResults{
		MktBase:        m.marketInfo.Base,
		MktQuote:       m.marketInfo.Quote,
		Idx:            epoch.Epoch,
		Dur:            epoch.Duration,
		MatchTime:      encode.UnixMilli(matchTime),
		CSum:           cSum,
		Seed:           seed,
		OrdersRevealed: oidsRevealed,
		OrdersMissed:   oidsMissed,
	})
	if err != nil {
		// fatal backend error, do not begin new swaps.
		return // TODO: notify clients
	}

	// Note: validated preimages are stored in the orders/cancels tables on
	// receipt from the user by handlePreimageResp.

	// Update orders in persistent storage. Trade orders may appear in multiple
	// trade order slices, so update in the sequence: booked, partial, completed
	// or canceled. However, an order in the failed slice will not be in another
	// slice since failed indicates unmatched&unbooked or bad lot size.
	//
	// TODO: Only execute the net effect. Each status update also updates the
	// filled amount of the trade order.
	//
	// Cancel order status updates are from epoch to executed or failed status.

	// Newly-booked orders.
	for _, lo := range updates.TradesBooked {
		if err = m.storage.BookOrder(lo); err != nil {
			return
		}
	}

	// Book orders that were partially filled and remain on the books.
	for _, lo := range updates.TradesPartial {
		if err = m.storage.UpdateOrderFilled(lo); err != nil {
			return
		}
	}

	// Completed orders (includes epoch and formerly booked orders).
	for _, ord := range updates.TradesCompleted {
		if err = m.storage.ExecuteOrder(ord); err != nil {
			return
		}
	}
	// Canceled orders.
	for _, lo := range updates.TradesCanceled {
		if err = m.storage.CancelOrder(lo); err != nil {
			return
		}
	}
	// Failed orders refer to epoch queue orders that are unmatched&unbooked, or
	// had a bad lot size.
	for _, ord := range updates.TradesFailed {
		if err = m.storage.ExecuteOrder(ord); err != nil {
			return
		}
	}

	// Change cancel orders from epoch status to executed or failed status.
	for _, co := range updates.CancelsFailed {
		if err = m.storage.FailCancelOrder(co); err != nil {
			return
		}
	}
	for _, co := range updates.CancelsExecuted {
		if err = m.storage.ExecuteOrder(co); err != nil {
			return
		}
	}

	// The Swapper needs to know which orders it is processing are off the book
	// so that they may be marked as complete when/if all swaps complete.
	offBookOrders := make(map[order.OrderID]bool)
	offBook := func(ord order.Order) bool {
		lo, limit := ord.(*order.LimitOrder)
		// Non-limit orders are not on the book (!limit).
		// Immediate force limits are not on the book.
		// Standing limits with no remaining are not on the book.
		return !limit || lo.Force == order.ImmediateTiF || lo.Remaining() == 0
		// Don't forget to check canceled orders too.
	}

	// Set the EpochID for each MatchSet, and record executed cancels.
	for _, match := range matches {
		offBookOrders[match.Taker.ID()] = offBook(match.Taker)
		for _, lo := range match.Makers {
			offBookOrders[lo.ID()] = offBook(lo)
		}

		// Set the epoch ID.
		match.Epoch.Idx = uint64(epoch.Epoch)
		match.Epoch.Dur = uint64(epoch.Duration)

		// Record the cancel in the auth manager.
		if co, ok := match.Taker.(*order.CancelOrder); ok {
			m.auth.RecordCancel(co.User(), co.ID(), co.TargetOrderID, matchTime) // cancel execution time, not order's server time
			// The order could be involved in trade match from up the epoch, but
			// it is now off the book regardless of order type and status.
			offBookOrders[co.TargetOrderID] = true
		}
	}

	// Signal the match_proof to the orderbook subscribers.
	preimages := make([]order.Preimage, len(ordersRevealed))
	for i := range ordersRevealed {
		preimages[i] = ordersRevealed[i].Preimage
	}
	sig := &bookUpdateSignal{
		action: matchProofAction,
		matchProof: &order.MatchProof{
			Epoch: order.EpochID{
				Idx: uint64(epoch.Epoch),
				Dur: m.EpochDuration(),
			},
			Preimages: preimages,
			Misses:    misses,
			CSum:      cSum,
			Seed:      seed,
		},
		epochIdx: epoch.Epoch,
	}
	m.notify(ctx, sig)

	// Unlock passed but not booked order (e.g. matched market and immediate
	// orders) coins were locked upon order receipt in processOrder and must be
	// unlocked now since they do not go on the book.
	for _, k := range doneOK {
		m.unlockOrderCoins(k.Order)
	}

	// Unlock unmatched (failed) order coins.
	for _, fo := range failed {
		m.unlockOrderCoins(fo.Order)
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
		sig := &bookUpdateSignal{
			action:   bookAction,
			order:    ord.Order,
			epochIdx: epoch.Epoch,
		}
		m.notify(ctx, sig)
	}

	// Send "unbook" notifications to order book subscribers.
	for _, ord := range unbooked {
		sig := &bookUpdateSignal{
			action:   unbookAction,
			order:    ord,
			epochIdx: epoch.Epoch,
		}
		m.notify(ctx, sig)
	}

	// Initiate the swaps.
	if len(matches) > 0 {
		log.Debugf("Negotiating %d matches for epoch %d:%d", len(matches),
			epoch.Epoch, epoch.Duration)
		m.swapper.Negotiate(matches, offBookOrders)
	}
}

// validateOrder uses db.ValidateOrder to ensure that the provided order is
// valid for the current market with epoch order status.
func (m *Market) validateOrder(ord order.Order) error {
	// First check the order commitment before bothering the Market's run loop.
	c0 := order.Commitment{}
	if ord.Commitment() == c0 {
		log.Debugf("Received order %v with zero-value Commitment. Rejecting.", ord)
		return ErrInvalidCommitment
	}

	if !db.ValidateOrder(ord, order.OrderStatusEpoch, m.marketInfo) {
		return ErrInvalidOrder // non-specific
	}
	return nil
}

// orderResponse signs the order data and prepares the OrderResult to be sent to
// the client.
func (m *Market) orderResponse(oRecord *orderRecord) (*msgjson.Message, error) {
	// Add the server timestamp.
	stamp := uint64(oRecord.order.Time())
	oRecord.req.Stamp(stamp)

	// Sign the serialized order request.
	err := m.auth.Sign(oRecord.req)
	if err != nil {
		return nil, err
	}

	// Prepare the OrderResult, including the server signature and time stamp.
	oid := oRecord.order.ID()
	res := &msgjson.OrderResult{
		Sig:        oRecord.req.SigBytes(),
		OrderID:    oid[:],
		ServerTime: stamp,
	}

	// Encode the order response as a message for the client.
	return msgjson.NewResponse(oRecord.msgID, res, nil)
}
