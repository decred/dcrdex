// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/ws"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/auth"
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
	ErrQuantityTooHigh        = Error("order quantity exceeds user limit")
	ErrDuplicateCancelOrder   = Error("equivalent cancel order already in epoch")
	ErrTooManyCancelOrders    = Error("too many cancel orders in current epoch")
	ErrCancelNotPermitted     = Error("cancel order account does not match targeted order account")
	ErrTargetNotActive        = Error("target order not active on this market")
	ErrTargetNotCancelable    = Error("targeted order is not a limit order with standing time-in-force")
	ErrSuspendedAccount       = Error("suspended account")
	ErrMalformedOrderResponse = Error("malformed order response")
	ErrInternalServer         = Error("internal server error")
)

// Swapper coordinates atomic swaps for one or more matchsets.
type Swapper interface {
	Negotiate(matchSets []*order.MatchSet)
	CheckUnspent(ctx context.Context, asset uint32, coinID []byte) error
	UserSwappingAmt(user account.AccountID, base, quote uint32) (amt, count uint64)
	ChainsSynced(base, quote uint32) (bool, error)
}

type DataCollector interface {
	ReportEpoch(base, quote uint32, epochIdx uint64, stats *matcher.MatchCycleStats) (*msgjson.Spot, error)
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

	tasks sync.WaitGroup // for lazy asynchronous tasks e.g. revoke ntfns

	// Communications.
	orderRouter chan *orderUpdateSignal // incoming orders, via SubmitOrderAsync

	orderFeedMtx sync.RWMutex         // guards orderFeeds and running
	orderFeeds   []chan *updateSignal // all outgoing notification consumers

	runMtx  sync.RWMutex
	running chan struct{} // closed when running (accepting new orders)
	up      uint32        // Run is called, either waiting for first epoch or running

	bookMtx      sync.Mutex // guards book and bookEpochIdx
	book         *book.Book
	bookEpochIdx int64 // next epoch from the point of view of the book
	settling     map[order.OrderID]uint64

	epochMtx         sync.RWMutex
	startEpochIdx    int64
	activeEpochIdx   int64
	suspendEpochIdx  int64
	persistBook      bool
	epochCommitments map[order.Commitment]order.OrderID
	epochOrders      map[order.OrderID]order.Order

	matcher *matcher.Matcher
	swapper Swapper
	auth    AuthManager

	coinLockerBase  coinlock.CoinLocker
	coinLockerQuote coinlock.CoinLocker

	// Persistent data storage
	storage Storage

	// Data API
	dataCollector DataCollector
}

// Storage is the DB interface required by Market.
type Storage interface {
	db.OrderArchiver
	LastErr() error
	Fatal() <-chan struct{}
	Close() error
	InsertEpoch(ed *db.EpochResults) error
	MarketMatches(base, quote uint32, includeInactive bool) ([]*db.MatchData, error)
}

// NewMarket creates a new Market for the provided base and quote assets, with
// an epoch cycling at given duration in milliseconds.
func NewMarket(mktInfo *dex.MarketInfo, storage Storage, swapper Swapper, authMgr AuthManager,
	coinLockerBase, coinLockerQuote coinlock.CoinLocker, dataCollector DataCollector) (*Market, error) {
	// Make sure the DEXArchivist is healthy before taking orders.
	if err := storage.LastErr(); err != nil {
		return nil, err
	}

	// Load existing book orders from the DB.
	base, quote := mktInfo.Base, mktInfo.Quote
	bookOrders, err := storage.BookOrders(base, quote)
	if err != nil {
		return nil, err
	}
	log.Infof("Allowing %d lots on the book per user.", mktInfo.BookedLotLimit)

	log.Infof("Loaded %d stored book orders.", len(bookOrders))
	// Put the book orders in a map so orders that no longer have funding coins
	// can be removed easily.
	bookOrdersByID := make(map[order.OrderID]*order.LimitOrder, len(bookOrders))
	for _, ord := range bookOrders {
		bookOrdersByID[ord.ID()] = ord
	}

	baseCoins := make(map[order.OrderID][]order.CoinID)
	quoteCoins := make(map[order.OrderID][]order.CoinID)

ordersLoop:
	for id, lo := range bookOrdersByID {
		if lo.FillAmt > 0 {
			// Order already matched with another trade, so it is expected that
			// the funding coins are spent in a swap.
			//
			// In general, our position is that the server is not ultimately
			// responsible for verifying that all orders have locked coins since
			// the client will be penalized if they cannot complete the swap.
			// The least the server can do is ensure funding coins for NEW
			// orders are unspent and owned by the user.

			// On to the next order. Do not lock coins that are spent or should
			// be spent in a swap contract.
			continue
		}

		// Verify all funding coins for this order.
		assetID := quote
		if lo.Sell {
			assetID = base
		}
		for i := range lo.Coins {
			err = swapper.CheckUnspent(context.Background(), assetID, lo.Coins[i]) // no timeout
			if err == nil {
				continue
			}

			if errors.Is(err, asset.CoinNotFoundError) {
				// spent, exclude this order
				coin, _ := asset.DecodeCoinID(dex.BipIDSymbol(assetID), lo.Coins[i]) // coin decoding succeeded in CheckUnspent
				log.Warnf("Coin %s not unspent for unfilled order %v. "+
					"Revoking the order.", coin, lo)
			} else {
				// other failure (coinID decode, RPC, etc.)
				return nil, fmt.Errorf("unexpected error checking coinID %v for order %v: %w",
					lo.Coins[i], lo, err)
				// NOTE: This does not revoke orders from storage since this is
				// likely to be a configuration or node issue.
			}

			delete(bookOrdersByID, id)
			// Revoke the order, but do not count this against the user.
			if _, _, err = storage.RevokeOrderUncounted(lo); err != nil {
				log.Errorf("Failed to revoke order %v: %v", lo, err)
			}
			// No penalization here presently since the market was down, but if
			// a suspend message with persist=true was sent, the users should
			// have kept their coins locked. (TODO)
			continue ordersLoop
		}

		// All coins are unspent. Lock them.
		if lo.Sell {
			baseCoins[id] = lo.Coins
		} else {
			quoteCoins[id] = lo.Coins
		}
	}

	log.Debugf("Locking %d base asset (%d) coins.", len(baseCoins), base)
	if log.Level() <= dex.LevelTrace {
		for oid, coins := range baseCoins {
			log.Tracef(" - order %v: %v", oid, coins)
		}
	}

	log.Debugf("Locking %d quote asset (%d) coins.", len(quoteCoins), quote)
	if log.Level() <= dex.LevelTrace {
		for oid, coins := range quoteCoins {
			log.Tracef(" - order %v: %v", oid, coins)
		}
	}

	// Lock the coins, catching and removing orders where coins were already
	// locked by another market.
	failedOrderCoins := coinLockerBase.LockCoins(baseCoins)
	// Merge base and quote asset coins.
	for id, coins := range coinLockerQuote.LockCoins(quoteCoins) {
		failedOrderCoins[id] = coins
	}

	for oid := range failedOrderCoins {
		log.Warnf("Revoking book order %v with already locked coins.", oid)
		bad := bookOrdersByID[oid]
		delete(bookOrdersByID, oid)
		// Revoke the order, but do not count this against the user.
		if _, _, err = storage.RevokeOrderUncounted(bad); err != nil {
			log.Errorf("Failed to revoke order %v: %v", bad, err)
			// But still not added back on the book.
		}
	}

	Book := book.New(mktInfo.LotSize)
	for _, lo := range bookOrdersByID {
		// Limit order amount requirements are simple unlike market buys.
		if lo.Quantity%mktInfo.LotSize != 0 || lo.FillAmt%mktInfo.LotSize != 0 {
			// To change market configuration, the operator should suspended the
			// market with persist=false, but that may not have happened, or
			// maybe a revoke failed.
			log.Errorf("Not rebooking order %v with amount (%v/%v) incompatible with current lot size (%v)",
				lo.FillAmt, lo.Quantity, mktInfo.LotSize)
			// Revoke the order, but do not count this against the user.
			if _, _, err = storage.RevokeOrderUncounted(lo); err != nil {
				log.Errorf("Failed to revoke order %v: %v", lo, err)
				// But still not added back on the book.
			}
			continue
		}

		if ok := Book.Insert(lo); !ok {
			// This can only happen if one of the loaded orders has an
			// incompatible lot size for the current market config.
			log.Errorf("Failed to insert order %v into %v book.", mktInfo.Name, lo)
		}
	}

	// Populate the order settling amount map from the active matches in DB.
	activeMatches, err := storage.MarketMatches(base, quote, false)
	if err != nil {
		return nil, fmt.Errorf("failed to load active matches for market %v: %w", mktInfo.Name, err)
	}
	settling := make(map[order.OrderID]uint64)
	for _, match := range activeMatches {
		settling[match.Taker] += match.Quantity
		settling[match.Maker] += match.Quantity
		// Note: we actually don't want to bother with matches for orders that
		// were canceled or had at-fault match failures, since including them
		// give that user another shot to get a successfully "completed" order
		// if they complete these remaining matches, but it's OK. We'd have to
		// query these order statuses, and look for at-fault match failures
		// involving them, so just give the user the benefit of the doubt.
	}
	log.Infof("Tracking %d orders with %d active matches.", len(settling), len(activeMatches))

	return &Market{
		running:          make(chan struct{}), // closed on market start
		marketInfo:       mktInfo,
		book:             Book,
		settling:         settling,
		matcher:          matcher.New(),
		persistBook:      true,
		epochCommitments: make(map[order.Commitment]order.OrderID),
		epochOrders:      make(map[order.OrderID]order.Order),
		swapper:          swapper,
		auth:             authMgr,
		storage:          storage,
		coinLockerBase:   coinLockerBase,
		coinLockerQuote:  coinLockerQuote,
		dataCollector:    dataCollector,
	}, nil
}

// SuspendASAP suspends requests the market to gracefully suspend epoch cycling
// as soon as possible, always allowing an active epoch to close. See also
// Suspend.
func (m *Market) SuspendASAP(persistBook bool) (finalEpochIdx int64, finalEpochEnd time.Time) {
	return m.Suspend(time.Now(), persistBook)
}

// Suspend requests the market to gracefully suspend epoch cycling as soon as
// the given time, always allowing the epoch including that time to complete. If
// the time is before the current epoch, the current epoch will be the last.
func (m *Market) Suspend(asSoonAs time.Time, persistBook bool) (finalEpochIdx int64, finalEpochEnd time.Time) {
	// epochMtx guards activeEpochIdx, startEpochIdx, suspendEpochIdx, and
	// persistBook.
	m.epochMtx.Lock()
	defer m.epochMtx.Unlock()

	dur := int64(m.EpochDuration())

	epochEnd := func(idx int64) time.Time {
		start := encode.UnixTimeMilli(idx * dur)
		return start.Add(time.Duration(dur) * time.Millisecond)
	}

	// Determine which epoch includes asSoonAs, and compute its end time. If
	// asSoonAs is in a past epoch, suspend at the end of the active epoch.

	soonestFinalIdx := m.activeEpochIdx
	if soonestFinalIdx == 0 {
		// Cannot schedule a suspend if Run isn't running.
		if m.startEpochIdx == 0 {
			return -1, time.Time{}
		}
		// Not yet started. Soonest suspend idx is the start epoch idx - 1.
		soonestFinalIdx = m.startEpochIdx - 1
	}

	if soonestEnd := epochEnd(soonestFinalIdx); asSoonAs.Before(soonestEnd) {
		// Suspend at the end of the active epoch or the one prior to start.
		finalEpochIdx = soonestFinalIdx
		finalEpochEnd = soonestEnd
	} else {
		// Suspend at the end of the epoch that includes the target time.
		ms := encode.UnixMilli(asSoonAs)
		finalEpochIdx = ms / dur
		// Allow stopping at boundary, prior to the epoch starting at this time.
		if ms%dur == 0 {
			finalEpochIdx--
		}
		finalEpochEnd = epochEnd(finalEpochIdx)
	}

	m.suspendEpochIdx = finalEpochIdx
	m.persistBook = persistBook

	return
}

// ResumeEpoch gets the next available resume epoch index for the currently
// configured epoch duration for the market and the provided earliest allowable
// start time. The market must be running, otherwise the zero index is returned.
func (m *Market) ResumeEpoch(asSoonAs time.Time) (startEpochIdx int64) {
	// Only allow scheduling a resume if the market is not running.
	if m.Running() {
		return
	}

	dur := int64(m.EpochDuration())

	now := encode.UnixMilli(time.Now())
	nextEpochIdx := 1 + now/dur

	ms := encode.UnixMilli(asSoonAs)
	startEpochIdx = 1 + ms/dur

	if startEpochIdx < nextEpochIdx {
		startEpochIdx = nextEpochIdx
	}
	return
}

// SetStartEpochIdx sets the starting epoch index. This should generally be
// called before Run, or Start used to specify the index at the same time.
func (m *Market) SetStartEpochIdx(startEpochIdx int64) {
	m.epochMtx.Lock()
	m.startEpochIdx = startEpochIdx
	m.epochMtx.Unlock()
}

// Start begins order processing with a starting epoch index. See also
// SetStartEpochIdx and Run. Stop the Market by cancelling the context.
func (m *Market) Start(ctx context.Context, startEpochIdx int64) {
	m.SetStartEpochIdx(startEpochIdx)
	m.Run(ctx)
}

// waitForEpochOpen waits until the start of epoch processing.
func (m *Market) waitForEpochOpen() {
	m.runMtx.RLock()
	c := m.running // the field may be rewritten, but only after close
	m.runMtx.RUnlock()
	<-c
}

// Status describes the operation state of the Market.
type Status struct {
	Running       bool
	EpochDuration uint64 // to compute times from epoch inds
	ActiveEpoch   int64
	StartEpoch    int64
	SuspendEpoch  int64
	PersistBook   bool
	Base, Quote   uint32
}

// Status returns the current operating state of the Market.
func (m *Market) Status() *Status {
	m.epochMtx.Lock()
	defer m.epochMtx.Unlock()
	return &Status{
		Running:       m.Running(),
		EpochDuration: m.marketInfo.EpochDuration,
		ActiveEpoch:   m.activeEpochIdx,
		StartEpoch:    m.startEpochIdx,
		SuspendEpoch:  m.suspendEpochIdx,
		PersistBook:   m.persistBook,
		Base:          m.marketInfo.Base,
		Quote:         m.marketInfo.Quote,
	}
}

// Running indicates is the market is accepting new orders. This will return
// false when suspended, but false does not necessarily mean Run has stopped
// since a start epoch may be set. Note that this method is of limited use and
// communicating subsystems shouldn't rely on the result for correct operation
// since a market could start or stop. Rather, they should infer or be informed
// of market status rather than rely on this.
//
// TODO: Instead of using Running in OrderRouter and DEX, these types should
// track statuses (known suspend times).
func (m *Market) Running() bool {
	m.runMtx.RLock()
	defer m.runMtx.RUnlock()
	select {
	case <-m.running:
		return true
	default:
		return false
	}
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
func (m *Market) OrderFeed() <-chan *updateSignal {
	bookUpdates := make(chan *updateSignal, 1)
	m.orderFeedMtx.Lock()
	m.orderFeeds = append(m.orderFeeds, bookUpdates)
	m.orderFeedMtx.Unlock()
	return bookUpdates
}

// FeedDone informs the market that the caller is finished receiving from the
// given channel, which should have been obtained from OrderFeed. If the channel
// was a registered order feed channel from OrderFeed, it is closed and removed
// so that no further signals will be send on the channel.
func (m *Market) FeedDone(feed <-chan *updateSignal) bool {
	m.orderFeedMtx.Lock()
	defer m.orderFeedMtx.Unlock()
	for i := range m.orderFeeds {
		if m.orderFeeds[i] == feed {
			close(m.orderFeeds[i])
			// Order is not important to delete the channel without allocation.
			m.orderFeeds[i] = m.orderFeeds[len(m.orderFeeds)-1]
			m.orderFeeds[len(m.orderFeeds)-1] = nil // chan is a pointer
			m.orderFeeds = m.orderFeeds[:len(m.orderFeeds)-1]
			return true
		}
	}
	return false
}

// sendToFeeds sends an *updateSignal to all order feed channels created with
// OrderFeed().
func (m *Market) sendToFeeds(sig *updateSignal) {
	m.orderFeedMtx.RLock()
	for _, s := range m.orderFeeds {
		s <- sig
	}
	m.orderFeedMtx.RUnlock()
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
	sendErr := func(err error) <-chan error {
		errChan := make(chan error, 1)
		errChan <- err // i.e. ErrInvalidOrder, ErrInvalidCommitment
		return errChan
	}

	// Validate the order. The order router must do it's own validation, but do
	// a second validation for (1) this Market and (2) epoch status, before
	// putting it on the queue.
	if err := m.validateOrder(rec.order); err != nil {
		// Order ID cannot be computed since ServerTime has not been set.
		log.Debugf("SubmitOrderAsync: Invalid order received from user %v with commitment %v: %v",
			rec.order.User(), rec.order.Commitment(), err)
		return sendErr(err)
	}

	// Only submit orders while market is running.
	m.runMtx.RLock()
	defer m.runMtx.RUnlock()

	select {
	case <-m.running:
	default:
		// m.orderRouter is closed
		log.Infof("SubmitOrderAsync: Market stopped with an order in submission (commitment %v).",
			rec.order.Commitment()) // The order is not time stamped, so no OrderID.
		return sendErr(ErrMarketNotRunning)
	}

	sig := newOrderUpdateSignal(rec)
	// The lock is still held, so there is a receiver: either Run's main loop or
	// the drain in Run's defer that runs until m.running starts blocking.
	m.orderRouter <- sig
	return sig.errChan
}

// MidGap returns the mid-gap market rate, which is ths rate halfway between the
// best buy order and the best sell order in the order book. If one side has no
// orders, the best order rate on other side is returned. If both sides have no
// orders, 0 is returned.
func (m *Market) MidGap() uint64 {
	_, mid, _ := m.rates()
	return mid
}

func (m *Market) rates() (bestBuyRate, mid, bestSellRate uint64) {
	bestBuy, bestSell := m.book.Best()
	if bestBuy == nil {
		if bestSell == nil {
			return
		}
		return 0, bestSell.Rate, bestSell.Rate
	} else if bestSell == nil {
		return bestBuy.Rate, bestBuy.Rate, math.MaxUint64
	}
	mid = (bestBuy.Rate + bestSell.Rate) / 2 // note downward bias on truncate
	return bestBuy.Rate, mid, bestSell.Rate
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

	if lo, ok := ord.(*order.LimitOrder); ok {
		return lo.Force == order.StandingTiF
	}
	return false
}

// CancelableBy determines if an order is cancelable by a certain account. This
// means: (1) an order in the book or epoch queue, (2) type limit with
// time-in-force standing (implied for book orders), and (3) AccountID field
// matching the provided account ID.
func (m *Market) CancelableBy(oid order.OrderID, aid account.AccountID) (bool, error) {
	// All book orders are standing limit orders.
	if lo := m.book.Order(oid); lo != nil {
		if lo.AccountID == aid {
			return true, nil
		}
		return false, ErrCancelNotPermitted
	}

	// Check the active epochs (includes current and next).
	m.epochMtx.RLock()
	ord := m.epochOrders[oid]
	m.epochMtx.RUnlock()

	if ord == nil {
		return false, ErrTargetNotActive
	}

	lo, ok := ord.(*order.LimitOrder)
	if !ok {
		return false, ErrTargetNotCancelable
	}
	if lo.Force != order.StandingTiF {
		return false, ErrTargetNotCancelable
	}
	if lo.AccountID != aid {
		return false, ErrCancelNotPermitted
	}
	return true, nil
}

func (m *Market) checkUnfilledOrders(assetID uint32, unfilled []*order.LimitOrder) (unbooked []*order.LimitOrder) {
	checkUnspent := func(assetID uint32, coinID []byte) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return m.swapper.CheckUnspent(ctx, assetID, coinID)
	}

orders:
	for _, lo := range unfilled {
		log.Tracef("Checking %d funding coins for order %v", len(lo.Coins), lo.ID())
		for i := range lo.Coins {
			err := checkUnspent(assetID, lo.Coins[i])
			if err == nil {
				continue // unspent, check next coin
			}

			if !errors.Is(err, asset.CoinNotFoundError) {
				// other failure (timeout, coinID decode, RPC, etc.)
				log.Errorf("Unexpected error checking coinID %v for order %v: %v",
					lo.Coins[i], lo, err)
				continue orders
				// NOTE: This does not revoke orders from storage since this is
				// likely to be a configuration or node issue.
			}

			// Final fill amount check in case it was matched after we pulled
			// the list of unfilled orders from the book.
			if lo.Filled() == 0 {
				coin, _ := asset.DecodeCoinID(dex.BipIDSymbol(assetID), lo.Coins[i]) // coin decoding succeeded in CheckUnspent
				log.Warnf("Coin %s not unspent for unfilled order %v. "+
					"Revoking the order.", coin, lo)
				m.Unbook(lo)
				unbooked = append(unbooked, lo)
			}
			continue orders
		}
	}
	return
}

// SwapDone registers a match for a given order as being finished. Whether the
// match was a successful or failed swap is indicated by fail. This is used to
// (1) register completed orders for cancellation rate purposes, and (2) to
// unbook at-fault limit orders.
//
// Implementation note: Orders that have failed a swap or were canceled (see
// processReadyEpoch) are removed from the settling map regardless of any amount
// still setting for such orders.
func (m *Market) SwapDone(ord order.Order, match *order.Match, fail bool) {
	oid := ord.ID()
	m.bookMtx.Lock()
	defer m.bookMtx.Unlock()
	settling, found := m.settling[oid]
	if !found {
		// Order was canceled or already had failed swap, and was removed from
		// the map. No more settling amount tracking needed.
		return
	}
	if settling < match.Quantity {
		log.Errorf("Finished swap %v (qty %d) for order %v larger than current settling (%d) amount.",
			match.ID(), match.Quantity, oid, settling)
		settling = 0
	} else {
		settling -= match.Quantity
	}

	// Limit orders may need to be unbooked, or considered for further matches.
	lo, limit := ord.(*order.LimitOrder)

	// For a failed swap, remove the map entry, and unbook/revoke the order.
	if fail {
		delete(m.settling, oid)
		if limit {
			// Try to unbook and revoke failed limit orders.
			_, removed := m.book.Remove(oid)
			m.unlockOrderCoins(lo)
			if removed {
				// Lazily update DB and auth, and notify orderbook subscribers.
				m.lazy(func() { m.unbookedOrder(lo) })
			}
		}
		return
	}

	// Continue tracking if there are swaps settling or it is booked (more
	// matches can be made). We check Book.HaveOrder instead of Remaining since
	// the provided Order instance may not belong to Market and may thus be out
	// of sync with respect to filled amount.
	if settling > 0 || (limit && lo.Force == order.StandingTiF && m.book.HaveOrder(oid)) {
		m.settling[oid] = settling
		return
	}

	// The order can no longer be matched and nothing is settling.
	delete(m.settling, oid)

	// Register the order as successfully completed in the auth manager.
	compTime := time.Now().UTC()
	m.auth.RecordCompletedOrder(ord.User(), oid, compTime)
	// Record the successful completion time.
	if err := m.storage.SetOrderCompleteTime(ord, encode.UnixMilli(compTime)); err != nil {
		if db.IsErrGeneralFailure(err) {
			log.Errorf("fatal error with SetOrderCompleteTime for order %v: %v", ord, err)
			return
		}
		log.Errorf("SetOrderCompleteTime for %v: %v", ord, err)
	}
}

// CheckUnfilled checks unfilled book orders belonging to a user and funded by
// coins for a given asset to ensure that their funding coins are not spent. If
// any of an order's funding coins are spent, the order is unbooked (removed
// from the in-memory book, revoked in the DB, a cancellation marked against the
// user, coins unlocked, and orderbook subscribers notified). See Unbook for
// details.
func (m *Market) CheckUnfilled(assetID uint32, user account.AccountID) (unbooked []*order.LimitOrder) {
	base, quote := m.marketInfo.Base, m.marketInfo.Quote
	if assetID != base && assetID != quote {
		return
	}
	var unfilled []*order.LimitOrder
	switch assetID {
	case base:
		// Sell orders are funded by the base asset.
		unfilled = m.book.UnfilledUserSells(user)
	case quote:
		// Buy orders are funded by the quote asset.
		unfilled = m.book.UnfilledUserBuys(user)
	default:
		return
	}

	return m.checkUnfilledOrders(assetID, unfilled)
}

// Book retrieves the market's current order book and the current epoch index.
// If the Market is not yet running or the start epoch has not yet begun, the
// epoch index will be zero.
func (m *Market) Book() (epoch int64, buys, sells []*order.LimitOrder) {
	// NOTE: it may be desirable to cache the response.
	m.bookMtx.Lock()
	buys = m.book.BuyOrders()
	sells = m.book.SellOrders()
	epoch = m.bookEpochIdx
	m.bookMtx.Unlock()
	return
}

// PurgeBook flushes all booked orders from the in-memory book and persistent
// storage. In terms of storage, this means changing orders with status booked
// to status revoked.
func (m *Market) PurgeBook() {
	m.bookMtx.Lock()
	defer m.bookMtx.Unlock()

	// Revoke all booked orders in the DB.
	sellsRemoved, buysRemoved, err := m.storage.FlushBook(m.marketInfo.Base, m.marketInfo.Quote)
	if err != nil {
		log.Errorf("Failed to flush book for market %s: %v", m.marketInfo.Name, err)
	} else {
		log.Infof("Flushed %d sell orders and %d buy orders from market %q book",
			len(sellsRemoved), len(buysRemoved), m.marketInfo.Name)
		// Clear the in-memory order book to match the DB.
		m.book.Clear()
		// Unlock coins for removed orders.

		// TODO: only unlock previously booked order coins, do not include coins
		// that might belong to orders still in epoch status. This won't matter
		// if the market is suspended, but it does if PurgeBook is used while
		// the market is still accepting new orders and processing epochs.

		// Unlock base asset coins locked by sell orders.
		for i := range sellsRemoved {
			m.coinLockerBase.UnlockOrderCoins(sellsRemoved[i])
		}
		// Unlock quote asset coins locked by buy orders.
		for i := range buysRemoved {
			m.coinLockerQuote.UnlockOrderCoins(buysRemoved[i])
		}
	}
}

func (m *Market) lazy(do func()) {
	m.tasks.Add(1)
	go func() {
		defer m.tasks.Done()
		do()
	}()
}

// Run is the main order processing loop, which takes new orders, notifies book
// subscribers, and cycles the epochs. The caller should cancel the provided
// Context to stop the market. The outgoing order feed channels persist after
// Run returns for possible Market resume, and for Swapper's unbook callback to
// function using sendToFeeds.
func (m *Market) Run(ctx context.Context) {
	// Prevent multiple incantations of Run.
	if !atomic.CompareAndSwapUint32(&m.up, 0, 1) {
		log.Errorf("Run: Market not stopped!")
		return
	}
	defer atomic.StoreUint32(&m.up, 0)

	var running bool
	ctxRun, cancel := context.WithCancel(ctx)
	var wgFeeds, wgEpochs sync.WaitGroup
	notifyChan := make(chan *updateSignal, 32)

	// For clarity, define the shutdown sequence in a single closure rather than
	// the defer stack.
	defer func() {
		// Drain the order router of incoming orders that made it in after the
		// main loop broke and before flagging the market stopped. Do this in a
		// goroutine because the market is flagged as stopped under runMtx lock
		// in this defer and there is a risk of deadlock in SubmitOrderAsync
		// that sends under runMtx lock as well.
		wgFeeds.Add(1)
		go func() {
			defer wgFeeds.Done()
			for sig := range m.orderRouter {
				sig.errChan <- ErrMarketNotRunning
			}
		}()

		// Under lock, flag as not running.
		m.runMtx.Lock() // block while SubmitOrderAsync is sending to the drain
		if !running {
			// In case the market is stopped before the first epoch, close the
			// running channel so that waitForEpochOpen does not hang.
			close(m.running)
		}
		m.running = make(chan struct{})
		running = false
		close(m.orderRouter) // stop the order router drain
		m.runMtx.Unlock()

		// Stop and wait for epoch pump and processing pipeline goroutines.
		cancel() // may already be done by suspend
		wgEpochs.Wait()
		// Book mod goroutines done, may purge if requested.

		// persistBook is set under epochMtx lock.
		m.epochMtx.Lock()
		if !m.persistBook {
			m.PurgeBook()
		}
		// Signal to the book router of the suspend now that the closed epoch
		// processing pipeline is finished (wgEpochs).
		notifyChan <- &updateSignal{
			action: suspendAction,
			data: sigDataSuspend{
				finalEpoch:  m.activeEpochIdx,
				persistBook: m.persistBook,
			},
		}
		m.persistBook = true // future resume default
		m.activeEpochIdx = 0

		// Revoke any unmatched epoch orders (if context was canceled, not a
		// clean suspend stopped the market).
		for oid, ord := range m.epochOrders {
			log.Infof("Dropping epoch order %v", oid)
			if co, ok := ord.(*order.CancelOrder); ok {
				if err := m.storage.FailCancelOrder(co); err != nil {
					log.Errorf("Failed to set orphaned epoch cancel order %v as executed: %v", oid, err)
				}
				continue
			}
			if err := m.storage.ExecuteOrder(ord); err != nil {
				log.Errorf("Failed to set orphaned epoch trade order %v as executed: %v", oid, err)
			}
		}
		m.epochMtx.Unlock()

		// Stop and wait for the order feed goroutine.
		close(notifyChan)
		wgFeeds.Wait()

		m.tasks.Wait()

		log.Infof("Market %q stopped.", m.marketInfo.Name)
	}()

	// Start outgoing order feed notification goroutine.
	wgFeeds.Add(1)
	go func() {
		defer wgFeeds.Done()
		for sig := range notifyChan {
			m.sendToFeeds(sig)
		}
	}()

	// Start the closed epoch pump, which drives preimage collection and orderly
	// epoch processing.
	eq := newEpochPump()
	wgEpochs.Add(1)
	go func() {
		defer wgEpochs.Done()
		eq.Run(ctxRun)
	}()

	// Start the closed epoch processing pipeline.
	wgEpochs.Add(1)
	go func() {
		defer wgEpochs.Done()
		for ep := range eq.ready {
			// prepEpoch has completed preimage collection.
			m.processReadyEpoch(ep, notifyChan)
		}
		log.Debugf("epoch pump drained for market %s", m.marketInfo.Name)
		// There must be no more notify calls.
	}()

	m.epochMtx.Lock()
	nextEpochIdx := m.startEpochIdx
	if nextEpochIdx == 0 {
		log.Warnf("Run: startEpochIdx not set. Starting at the next epoch.")
		now := encode.UnixMilli(time.Now())
		nextEpochIdx = 1 + now/int64(m.EpochDuration())
		m.startEpochIdx = nextEpochIdx
	}
	m.epochMtx.Unlock()

	epochDuration := int64(m.marketInfo.EpochDuration)
	nextEpoch := NewEpoch(nextEpochIdx, epochDuration)
	epochCycle := time.After(time.Until(nextEpoch.Start))

	var currentEpoch *EpochQueue
	cycleEpoch := func() {
		if currentEpoch != nil {
			// Process the epoch asynchronously since there is a delay while the
			// preimages are requested and clients respond with their preimages.
			if !m.enqueueEpoch(eq, currentEpoch) {
				return
			}

			// The epoch is closed, long live the epoch.
			sig := &updateSignal{
				action: newEpochAction,
				data:   sigDataNewEpoch{idx: nextEpoch.Epoch},
			}
			notifyChan <- sig
		}

		// Guard activeEpochIdx and suspendEpochIdx.
		m.epochMtx.Lock()
		defer m.epochMtx.Unlock()

		// Check suspendEpochIdx and suspend if the just-closed epoch idx is the
		// suspend epoch.
		if m.suspendEpochIdx == nextEpoch.Epoch-1 {
			// Reject incoming orders.
			currentEpoch = nil
			cancel() // graceful market shutdown
			return
		}

		currentEpoch = nextEpoch
		nextEpochIdx = currentEpoch.Epoch + 1
		m.activeEpochIdx = currentEpoch.Epoch

		if !running {
			// Check that both blockchains are synced before actually starting.
			synced, err := m.swapper.ChainsSynced(m.marketInfo.Base, m.marketInfo.Quote)
			if err != nil {
				log.Errorf("Not starting %s market because of ChainsSynced error: %v", m.marketInfo.Name, err)
			} else if !synced {
				log.Debugf("Delaying start of %s market because chains aren't synced", m.marketInfo.Name)
			} else {
				// Open up SubmitOrderAsync.
				close(m.running)
				running = true
				log.Infof("Market %s now accepting orders, epoch %d:%d", m.marketInfo.Name,
					currentEpoch.Epoch, epochDuration)
				// Signal to the book router if this is a resume.
				if m.suspendEpochIdx != 0 {
					notifyChan <- &updateSignal{
						action: resumeAction,
						data: sigDataResume{
							epochIdx: currentEpoch.Epoch,
							// TODO: signal config or new config
						},
					}
				}
			}
		}

		// Replace the next epoch and set the cycle Timer.
		nextEpoch = NewEpoch(nextEpochIdx, epochDuration)
		epochCycle = time.After(time.Until(nextEpoch.Start))
	}

	// Set the orderRouter field now since the main loop below receives on it,
	// even though SubmitOrderAsync disallows sends on orderRouter when the
	// market is not running.
	m.orderRouter = make(chan *orderUpdateSignal, 32) // implicitly guarded by m.runMtx since Market is not running yet

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

		// cycleEpoch can cancel ctxRun if suspend initiated.
		if ctxRun.Err() != nil {
			return
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

			// Set the order's server time stamp, giving the order a valid ID.
			sTime := time.Now().Truncate(time.Millisecond).UTC()
			s.rec.order.SetTime(sTime) // Order.ID()/UID()/String() is OK now.
			log.Tracef("Received order %v at %v", s.rec.order, sTime)

			// Push the order into the next epoch if receiving and stamping it
			// took just a little too long.
			var orderEpoch *EpochQueue
			switch {
			case currentEpoch.IncludesTime(sTime):
				orderEpoch = currentEpoch
			case nextEpoch.IncludesTime(sTime):
				log.Infof("Order %v (sTime=%d) fell into the next epoch [%d,%d)",
					s.rec.order, sTime.UnixNano(), nextEpoch.Start.Unix(), nextEpoch.End.Unix())
				orderEpoch = nextEpoch
			default:
				// This should not happen.
				log.Errorf("Time %d does not fit into current or next epoch!",
					sTime.UnixNano())
				s.errChan <- ErrEpochMissed
				continue
			}

			// Process the order in the target epoch queue.
			err := m.processOrder(s.rec, orderEpoch, notifyChan, s.errChan)
			if err != nil {
				log.Errorf("Failed to process order %v: %v", s.rec.order, err)
				// Signal to the other Run goroutines to return.
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
func (m *Market) processOrder(rec *orderRecord, epoch *EpochQueue, notifyChan chan<- *updateSignal, errChan chan<- error) error {
	// Disallow trade orders from suspended accounts. Cancel orders are allowed.
	if rec.order.Type() != order.CancelOrderType {
		// Do not bother the auth manager for cancel orders.
		if _, suspended := m.auth.Suspended(rec.order.User()); suspended {
			log.Debugf("Account %v not allowed to submit order %v", rec.order.User(), rec.order.ID())
			errChan <- ErrSuspendedAccount
			return nil
		}
	}

	// Verify that an order with the same commitment is not already in the epoch
	// queue. Since commitment is part of the order serialization and thus order
	// ID, this also prevents orders with the same ID.
	// TODO: Prevent commitment reuse in general, without expensive DB queries.
	ord := rec.order
	oid := ord.ID()
	user := ord.User()
	commit := ord.Commitment()
	m.epochMtx.RLock()
	otherOid, found := m.epochCommitments[commit]
	m.epochMtx.RUnlock()
	if found {
		log.Debugf("Received order %v with commitment %x also used in previous order %v!",
			oid, commit, otherOid)
		errChan <- ErrInvalidCommitment
		return nil
	}

	// Whether an order is a taker depends on type, and for limit orders it
	// depends on force and rates.
	bestBuy, midGap, bestSell := m.rates()
	likelyTaker := func(ord order.Order) bool {
		lo, ok := ord.(*order.LimitOrder)
		if !ok || lo.Force == order.ImmediateTiF {
			return true
		}
		// Must cross the spread to be a taker (not so conservative).
		switch {
		case midGap == 0:
			return false // empty market: could be taker, but assume not
		case lo.Sell:
			return lo.Rate <= bestBuy
		default:
			return lo.Rate >= bestSell
		}
	}
	// Note: bestSell and bestBuy do not include other epoch orders with
	// standing force that might become booked. Doing so would only make this
	// order less likely to be assumed a taker by moving bestSell down or
	// bestBuy up, so be conservative and only consider current book.

	// helper to compute an order's quantity in base asset units, using current
	// midGap rate for market buys.
	baseQty := func(ord order.Order) uint64 {
		if ord.Type() == order.CancelOrderType {
			return 0
		}
		qty := ord.Trade().Quantity
		if ord.Type() == order.MarketOrderType && !ord.Trade().Sell {
			// Market buy qty is in quote asset. Convert to base.
			if midGap == 0 {
				qty = m.marketInfo.LotSize // no orders on the book; call it 1 lot
			} else {
				qty = calc.QuoteToBase(midGap, qty)
			}
		}
		return qty
	}

	// Include user's own epoch orders when enforcing both booked order and
	// taker settling amount limits.
	var userStandingEpochQty, userTakerEpochQty uint64
	m.epochMtx.RLock()
	for _, epOrd := range m.epochOrders {
		if epOrd.User() != user || epOrd.Type() == order.CancelOrderType {
			continue
		}

		// For purposes of the user's book qty limit, assumed standing limits
		// will be booked.
		if lo, ok := epOrd.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
			userStandingEpochQty += lo.Quantity
		}

		// Even if standing, may count as taker for purposes of taker qty limit.
		if likelyTaker(epOrd) {
			userTakerEpochQty += baseQty(epOrd)
		}
	}
	m.epochMtx.RUnlock()

	// Now that epoch orders are considered, check this candidate order.
	if lo, ok := ord.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
		// Check the user's current booked amount.
		bookedBuyAmt, bookedSellAmt, _, _ := m.book.UserOrderTotals(user)
		bookedAmt := bookedBuyAmt + bookedSellAmt + userStandingEpochQty
		qty := lo.Quantity
		if (qty+bookedAmt)/m.marketInfo.LotSize > uint64(m.marketInfo.BookedLotLimit) {
			log.Debugf("Rejecting user %v order %v: too much in booked orders", user, oid)
			errChan <- dex.NewError(ErrQuantityTooHigh,
				fmt.Sprintf("Order quantity %d (%d lots) too large. User book limit is %d lots, and you have %d lots booked already).",
					qty, qty/m.marketInfo.LotSize, uint64(m.marketInfo.BookedLotLimit), bookedAmt/m.marketInfo.LotSize))
			return nil
		}
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

		if nc := epoch.UserCancels[co.AccountID]; nc >= m.marketInfo.MaxUserCancelsPerEpoch {
			log.Debugf("Received cancel order %v targeting %v, but user already has %d cancel orders in this epoch.",
				co, co.TargetOrderID, nc)
			errChan <- ErrTooManyCancelOrders
			return nil
		}

		// Verify that the target order is on the books or in the epoch queue,
		// and that the account of the CancelOrder is the same as the account of
		// the target order.
		cancelable, err := m.CancelableBy(co.TargetOrderID, co.AccountID)
		if !cancelable {
			log.Debugf("Cancel order %v (account=%v) target order %v: %v",
				co, co.AccountID, co.TargetOrderID, err)
			errChan <- err
			return nil
		}
	} else if likelyTaker(ord) { // Likely-taker trade order. Check the quantity against user's limit.
		// NOTE: We can entirely change this so that the taker limit is not
		// based on just this market. Also so that it's based on lots rather
		// than amount, but we risk comparing apples to oranges. Further, I'm
		// open to going even simpler and scaling an absolute max by the user's
		// current score, but that takes swapped value entirely out of the
		// picture; adding swapped value into the users score is another
		// possibility, but which has the same units selection challenge.

		// Swapper knows how much is in active swaps for this asset pair.
		amtInSwaps, activeSwaps := m.swapper.UserSwappingAmt(user, ord.Base(), ord.Quote()) // swapper knows nothing of lots, and we know nothing of other markets

		// Get the settling amount limit in units of the base asset from the
		// AuthManager, which tracks the user's swap outcome amount history.
		userLimit := m.auth.UserSettlingLimit(user, m.marketInfo) // hard to make this lots across all markets, partly because db stores base value
		// Subtract the user's total active amount from their limit.
		orderQtyAllowed := userLimit - int64(amtInSwaps+userTakerEpochQty)

		qty := baseQty(ord)
		symb := dex.BipIDSymbol(m.marketInfo.Base)
		log.Debugf("User placing likely-taker order on market %s worth %d (%s units) of %d allowed. "+
			"User has %d (%s units) in %d active swaps, %d in epoch taker orders.",
			m.marketInfo.Name, qty, symb, orderQtyAllowed,
			amtInSwaps, symb, activeSwaps, userTakerEpochQty)

		if int64(qty) > orderQtyAllowed {
			log.Infof("Rejecting user %v likely-taker order %v: qty %d > %d allowed "+
				"(already have %d swapping and %d epoch takers with %d limit)",
				user, oid, qty, orderQtyAllowed, amtInSwaps, userTakerEpochQty, userLimit)
			errChan <- dex.NewError(ErrQuantityTooHigh,
				fmt.Sprintf("Order quantity %d too large. Current likely-taker order limit: %d "+
					"(you have %d settling already and %d in epoch taker orders)",
					qty, orderQtyAllowed, amtInSwaps, userTakerEpochQty))
			return nil
		}
	}

	// Sign the order and prepare the client response. Only after the archiver
	// has successfully stored the new epoch order should the order be committed
	// for processing.
	respMsg, err := m.orderResponse(rec)
	if err != nil {
		log.Errorf("failed to create msgjson.Message for order %v, msgID %v response: %v",
			ord, rec.msgID, err)
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
	// locked coins cannot get into the epoch queue. Later, in processReadyEpoch
	// or the Swapper, release these coins when the swap is completed.
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
		return fmt.Errorf("processOrder: Failed to store new epoch order %v: %w",
			ord, err)
	}

	// Insert the order into the epoch queue.
	epoch.Insert(ord)

	m.epochMtx.Lock()
	m.epochOrders[oid] = ord
	m.epochCommitments[commit] = oid
	m.epochMtx.Unlock()

	// Respond to the order router only after updating epochOrders so that
	// Cancelable will reflect that the order is now in the epoch queue.
	errChan <- nil

	// Inform the client that the order has been received, stamped, signed, and
	// inserted into the current epoch queue.
	m.lazy(func() {
		if err := m.auth.Send(user, respMsg); err != nil {
			log.Infof("Failed to send signed new order response to user %v, order %v: %v",
				user, oid, err)
		}
	})

	// Send epoch update to epoch queue subscribers.
	notifyChan <- &updateSignal{
		action: epochAction,
		data: sigDataEpochOrder{
			order:    ord,
			epochIdx: epoch.Epoch,
		},
	}
	// With the notification sent to subscribers, this order must be included in
	// the processing of this epoch.
	return nil
}

func idToBytes(id [order.OrderIDSize]byte) []byte {
	return id[:]
}

// respondError sends an rpcError to a user.
func (m *Market) respondError(id uint64, user account.AccountID, code int, errMsg string) {
	log.Debugf("sending error to user %v, code: %d, msg: %s", user, code, errMsg)
	msg, err := msgjson.NewResponse(id, nil, &msgjson.Error{
		Code:    code,
		Message: errMsg,
	})
	if err != nil {
		log.Errorf("error creating error response with message '%s': %v", msg, err)
	}
	if err := m.auth.Send(user, msg); err != nil {
		log.Infof("Failed to send %s error response (code = %d, msg = %s) to user %v: %v",
			msg.Route, code, errMsg, user, err)
	}
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
			"error parsing preimage notification response")
		return
	}
	err = json.Unmarshal(resp.Result, &piResp)
	if err != nil {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.RPCParseError,
			"error parsing preimage response payload result")
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
	log.Tracef("Good preimage received for order %v: %x", reqData.ord, pi)
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
// can block for up to 20 seconds (piTimeout) to allow clients time to respond.
// Clients that fail to respond, or respond with invalid data (see
// handlePreimageResp), are counted as misses.
func (m *Market) collectPreimages(orders []order.Order) (cSum []byte, ordersRevealed []*matcher.OrderRevealed, misses []order.Order) {
	// Compute the commitment checksum for the order queue.
	cSum = matcher.CSum(orders)

	// Request preimages from the clients.
	piTimeout := 20 * time.Second
	preimages := make(map[order.Order]chan *order.Preimage, len(orders))
	for _, ord := range orders {
		// Make the 'preimage' request.
		piReqParams := &msgjson.PreimageRequest{
			OrderID:        idToBytes(ord.ID()),
			CommitChecksum: cSum,
			// TODO: include this order's commitment so client can recognize it prior to submission response
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
		piChan := make(chan *order.Preimage, 1) // buffer so the link's in handler does not block

		reqData := &piData{
			ord:      ord,
			preimage: piChan,
		}

		// Failure to respond in time or an async link write error is a miss,
		// signalled by a nil pointer. Request errors returned by
		// RequestWithTimeout instead register a miss immediately.
		miss := func() { piChan <- nil }

		// Send the preimage request to the order's owner.
		err = m.auth.RequestWithTimeout(ord.User(), req, func(_ comms.Link, msg *msgjson.Message) {
			m.handlePreimageResp(msg, reqData) // sends on piChan
		}, piTimeout, miss)
		if err != nil {
			if errors.Is(err, ws.ErrPeerDisconnected) || errors.Is(err, auth.ErrUserNotConnected) {
				log.Debugf("Preimage request failed, client gone: %v", err)
			} else {
				// We may need a way to identify server connectivity problems so
				// clients are not penalized when it is not their fault. For
				// now, log this at warning level since the error is not novel.
				log.Warnf("Preimage request failed: %v", err)
			}

			// Register the miss now, no channel receive for this order.
			misses = append(misses, ord)
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

func (m *Market) enqueueEpoch(eq *epochPump, epoch *EpochQueue) bool {
	// Enqueue the epoch for matching when preimage collection is completed and
	// it is this epoch's turn.
	rq := eq.Insert(epoch)
	if rq == nil {
		// should not happen if cycleEpoch considers when the halt began.
		log.Errorf("failed to enqueue an epoch into a halted epoch pump")
		return false
	}

	// With this epoch closed, these orders are no longer cancelable, if and
	// until they are booked in processReadyEpoch (after preimage collection).
	orders := epoch.OrderSlice()
	m.epochMtx.Lock()
	for _, ord := range orders {
		delete(m.epochOrders, ord.ID())
		delete(m.epochCommitments, ord.Commitment())
		// Would be nice to remove orders from users that got suspended, but the
		// epoch order notifications were sent to subscribers when the order was
		// received, thus setting expectations for auditing the queue.
		//
		// Preimage collection for suspended users could be skipped, forcing
		// them into the misses slice perhaps by passing user IDs to skip into
		// prepEpoch, with a SPEC UPDATE noting that preimage requests are not
		// sent to suspended accounts.
	}
	m.epochMtx.Unlock()

	// Start preimage collection.
	go func() {
		rq.cSum, rq.ordersRevealed, rq.misses = m.prepEpoch(orders, epoch.End)
		close(rq.ready)
	}()

	return true
}

// prepEpoch collects order preimages, and penalizes users who fail to respond.
func (m *Market) prepEpoch(orders []order.Order, epochEnd time.Time) (cSum []byte, ordersRevealed []*matcher.OrderRevealed, misses []order.Order) {
	// Solicit the preimages for each order.
	cSum, ordersRevealed, misses = m.collectPreimages(orders)
	if len(orders) > 0 {
		log.Infof("Collected %d valid order preimages, missed %d. Commit checksum: %x",
			len(ordersRevealed), len(misses), cSum)
	}

	for _, ord := range misses {
		log.Infof("No preimage received for order %v from user %v. Recording violation and revoking order.",
			ord.ID(), ord.User())
		// Register the preimage miss violation, adjusting the user's score.
		m.auth.MissedPreimage(ord.User(), epochEnd, ord.ID())
		// Unlock the order's coins locked in processOrder.
		m.unlockOrderCoins(ord) // could also be done in processReadyEpoch
		// Change the order status from orderStatusEpoch to orderStatusRevoked.
		coid, revTime, err := m.storage.RevokeOrder(ord)
		if err == nil {
			m.auth.RecordCancel(ord.User(), coid, ord.ID(), revTime)
		} else {
			log.Errorf("Failed to revoke order %v with a new cancel order: %v",
				ord.UID(), err)
		}
	}

	// Register the preimage collection successes, potentially evicting preimage
	// miss violations for purposes of user scoring.
	for _, ord := range ordersRevealed {
		m.auth.PreimageSuccess(ord.Order.User(), epochEnd, ord.Order.ID())
	}

	return
}

// UnbookUserOrders unbooks all orders belonging to a user, unlocks the coins
// that were used to fund the unbooked orders, changes the orders' statuses to
// revoked in the DB, and notifies orderbook subscribers.
func (m *Market) UnbookUserOrders(user account.AccountID) {
	m.bookMtx.Lock()
	removedBuys, removedSells := m.book.RemoveUserOrders(user)
	m.bookMtx.Unlock()

	total := len(removedBuys) + len(removedSells)
	if total == 0 {
		return
	}

	log.Infof("Unbooked %d orders (%d buys, %d sells) from market %v from user %v.",
		total, len(removedBuys), len(removedSells), m.marketInfo.Name, user)

	// Unlock the order funding coins, update order statuses in DB, and notify
	// orderbook subscribers.
	sellIDs := make([]order.OrderID, 0, len(removedSells))
	for _, lo := range removedSells {
		sellIDs = append(sellIDs, lo.ID())
		m.unbookedOrder(lo)
	}
	m.coinLockerBase.UnlockOrdersCoins(sellIDs)

	buyIDs := make([]order.OrderID, 0, len(removedBuys))
	for _, lo := range removedBuys {
		buyIDs = append(buyIDs, lo.ID())
		m.unbookedOrder(lo)
	}
	m.coinLockerQuote.UnlockOrdersCoins(buyIDs)
}

// Unbook allows the DEX manager to remove a booked order. This does: (1) remove
// the order from the in-memory book, (2) unlock funding order coins, (3) set
// the order's status in the DB to "revoked", (4) inform the auth manager of the
// action for cancellation ratio accounting, and (5) send an 'unbook'
// notification to subscribers of this market's order book. Note that this
// presently treats the user as at-fault by counting the revocation in the
// user's cancellation statistics.
func (m *Market) Unbook(lo *order.LimitOrder) bool {
	// Ensure we do not unbook during matching.
	m.bookMtx.Lock()
	_, removed := m.book.Remove(lo.ID())
	m.bookMtx.Unlock()

	m.unlockOrderCoins(lo)

	if removed {
		// Update the order status in DB, and notify orderbook subscribers.
		m.unbookedOrder(lo)
	}
	return removed
}

func (m *Market) unbookedOrder(lo *order.LimitOrder) {
	// Create the server-generated cancel order, and register it with the
	// AuthManager for cancellation rate computation if still connected.
	oid, user := lo.ID(), lo.User()
	coid, revTime, err := m.storage.RevokeOrder(lo)
	if err == nil {
		m.auth.RecordCancel(user, coid, oid, revTime)
	} else {
		log.Errorf("Failed to revoke order %v with a new cancel order: %v",
			lo.UID(), err)
	}

	// Send revoke_order notification to order owner.
	route := msgjson.RevokeOrderRoute
	log.Infof("Sending a '%s' notification to %v for order %v", route, user, oid)
	revMsg := &msgjson.RevokeOrder{
		OrderID: oid.Bytes(),
	}
	m.auth.Sign(revMsg)
	revNtfn, err := msgjson.NewNotification(route, revMsg)
	if err != nil {
		log.Errorf("Failed to create %s notification for order %v: %v", route, oid, err)
	} else {
		err = m.auth.Send(user, revNtfn)
		if err != nil {
			log.Debugf("Failed to send %s notification to user %v: %v", route, user, err)
		}
	}

	// Send "unbook" notification to order book subscribers.
	m.sendToFeeds(&updateSignal{
		action: unbookAction,
		data: sigDataUnbookedOrder{
			order:    lo,
			epochIdx: -1, // NOTE: no epoch
		},
	})
}

// processReadyEpoch performs the following operations for a closed epoch that
// has finished preimage collection via collectPreimages:
//  1. Perform matching with the order book.
//  2. Send book and unbook notifications to the book subscribers.
//  3. Unlock coins with the book lock for unbooked and failed orders.
//  4. Lock coins with the swap lock.
//  5. Initiate the swap negotiation via the Market's Swapper.
// The EpochQueue's Orders map must not be modified by another goroutine.
func (m *Market) processReadyEpoch(epoch *readyEpoch, notifyChan chan<- *updateSignal) {
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
	m.bookMtx.Lock()        // allow a coherent view of book orders with (*Market).Book
	matchTime := time.Now() // considered as the time at which matched cancel orders are executed
	seed, matches, _, failed, doneOK, partial, booked, nomatched, unbooked, updates, stats := m.matcher.Match(m.book, ordersRevealed)
	m.bookEpochIdx = epoch.Epoch + 1
	var canceled []order.OrderID
	for _, ms := range matches {
		// Set the epoch ID.
		ms.Epoch.Idx = uint64(epoch.Epoch)
		ms.Epoch.Dur = uint64(epoch.Duration)

		// Update order settling amounts.
		for _, match := range ms.Matches() {
			if co, ok := match.Taker.(*order.CancelOrder); ok {
				m.auth.RecordCancel(co.User(), co.ID(), co.TargetOrderID, matchTime)
				canceled = append(canceled, co.TargetOrderID)
				continue
			}
			m.settling[match.Taker.ID()] += match.Quantity
			m.settling[match.Maker.ID()] += match.Quantity
		}
	}
	for _, oid := range canceled {
		delete(m.settling, oid) // may still be settling, but we don't care anymore
	}
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
	// times can be obtained from the DB for cancellation rate computation.
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
		MatchVolume:    stats.MatchVolume,
		QuoteVolume:    stats.QuoteVolume,
		BookBuys:       stats.BookBuys,
		BookBuys5:      stats.BookBuys5,
		BookBuys25:     stats.BookBuys25,
		BookSells:      stats.BookSells,
		BookSells5:     stats.BookSells5,
		BookSells25:    stats.BookSells25,
		HighRate:       stats.HighRate,
		LowRate:        stats.LowRate,
		StartRate:      stats.StartRate,
		EndRate:        stats.EndRate,
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

	// Signal the match_proof to the orderbook subscribers.
	preimages := make([]order.Preimage, len(ordersRevealed))
	for i := range ordersRevealed {
		preimages[i] = ordersRevealed[i].Preimage
	}
	sig := &updateSignal{
		action: matchProofAction,
		data: sigDataMatchProof{
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
		},
	}
	notifyChan <- sig

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
		sig := &updateSignal{
			action: bookAction,
			data: sigDataBookedOrder{
				order:    ord.Order,
				epochIdx: epoch.Epoch,
			},
		}
		notifyChan <- sig
	}

	// Send "update_remaining" notifications to order book subscribers.
	for _, lo := range updates.TradesPartial {
		notifyChan <- &updateSignal{
			action: updateRemainingAction,
			data: sigDataUpdateRemaining{
				order:    lo,
				epochIdx: epoch.Epoch,
			},
		}
	}

	// Send "unbook" notifications to order book subscribers. This must be after
	// update_remaining.
	for _, ord := range unbooked {
		sig := &updateSignal{
			action: unbookAction,
			data: sigDataUnbookedOrder{
				order:    ord,
				epochIdx: epoch.Epoch,
			},
		}
		notifyChan <- sig
	}

	// Send "nomatch" notifications.
	for _, ord := range nomatched {
		oid := ord.Order.ID()
		msg, err := msgjson.NewNotification(msgjson.NoMatchRoute, &msgjson.NoMatch{
			OrderID: oid[:],
		})
		if err != nil {
			// This is probably impossible in practice, but we'll log it anyway.
			log.Errorf("Failed to encode 'nomatch' notification.")
			continue
		}
		if err := m.auth.Send(ord.Order.User(), msg); err != nil {
			log.Infof("Failed to send nomatch to user %s: %v", ord.Order.User(), err)
		}
	}

	// Send "epoch_report" notifications.
	notifyChan <- &updateSignal{
		action: epochReportAction,
		data: sigDataEpochReport{
			epochIdx: epoch.Epoch,
			epochDur: epoch.Duration,
			stats:    stats,
		},
	}

	// Initiate the swaps.
	if len(matches) > 0 {
		log.Debugf("Negotiating %d matches for epoch %d:%d", len(matches),
			epoch.Epoch, epoch.Duration)
		m.swapper.Negotiate(matches)
	}

	// Update the API data collector.
	_, err = m.dataCollector.ReportEpoch(m.Base(), m.Quote(), uint64(epoch.Epoch), stats)
	if err != nil {
		log.Errorf("Error updating API data collector: %v", err)
	}
}

// validateOrder uses db.ValidateOrder to ensure that the provided order is
// valid for the current market with epoch order status.
func (m *Market) validateOrder(ord order.Order) error {
	// First check the order commitment before bothering the Market's run loop.
	c0 := order.Commitment{}
	if ord.Commitment() == c0 {
		// Note that OrderID may not be valid if ServerTime has not been set.
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
	m.auth.Sign(oRecord.req)

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
