// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package market manages order books and processes trading epochs.
package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
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
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// Error is just a basic error.
type Error string

// Error satisfies the error interface.
func (e Error) Error() string {
	return string(e)
}

const (
	ErrMarketNotRunning     = Error("market not running")
	ErrInvalidOrder         = Error("order failed validation")
	ErrInvalidRate          = Error("limit order rate too low")
	ErrInvalidCommitment    = Error("order commitment invalid")
	ErrEpochMissed          = Error("order unexpectedly missed its intended epoch")
	ErrDuplicateOrder       = Error("order already in epoch") // maybe remove since this is ill defined
	ErrQuantityTooHigh      = Error("order quantity exceeds user limit")
	ErrDuplicateCancelOrder = Error("equivalent cancel order already in epoch")
	ErrTooManyCancelOrders  = Error("too many cancel orders in current epoch")
	ErrCancelNotPermitted   = Error("cancel order account does not match targeted order account")
	ErrTargetNotActive      = Error("target order not active on this market")
	ErrTargetNotCancelable  = Error("targeted order is not a limit order with standing time-in-force")
	ErrSuspendedAccount     = Error("suspended account")
	ErrInternalServer       = Error("internal server error")
)

var errEpochOrderStorage = errors.New("epoch order storage failure")

// Swapper coordinates atomic swaps for one or more matchsets.
type Swapper interface {
	TrackMatches(matchSets []*order.MatchSet)
	RequestMatchAcks(matchSets []*order.MatchSet)
	CheckUnspent(ctx context.Context, asset uint32, coinID []byte) error
	ChainsSynced(base, quote uint32) (bool, error)
}

type DataCollector interface {
	ReportEpoch(base, quote uint32, epochIdx uint64, stats *matcher.MatchCycleStats) (*msgjson.Spot, error)
}

// FeeFetcher is a fee fetcher for fetching fees. Fees are fickle, so fetch fees
// with FeeFetcher fairly frequently.
type FeeFetcher interface {
	FeeRate(context.Context) uint64
	SwapFeeRate(context.Context) uint64
	LastRate() uint64
	MaxFeeRate() uint64
}

// Balancer provides a method to check that an account on an account-based
// asset has sufficient balance.
type Balancer interface {
	// CheckBalance checks that the address's account has sufficient balance to
	// trade the outgoing number of lots (totaling qty) and incoming number of
	// redeems.
	CheckBalance(acctAddr string, assetID, redeemAssetID uint32, qty, lots uint64, redeems int) bool
	// CheckReserved reports whether the account can still fund its existing
	// DEX commitments for this asset. No new order is being placed.
	CheckReserved(acctAddr string, assetID uint32) bool
}

// Config is the Market configuration.
type Config struct {
	MarketInfo       *dex.MarketInfo
	Storage          Storage
	Swapper          Swapper
	AuthManager      AuthManager
	FeeFetcherBase   FeeFetcher
	CoinLockerBase   coinlock.CoinLocker
	FeeFetcherQuote  FeeFetcher
	CoinLockerQuote  coinlock.CoinLocker
	DataCollector    DataCollector
	Balancer         Balancer
	CheckParcelLimit func(user account.AccountID, asOf time.Time, calcParcels MarketParcelCalculator) (bool, error)
	MinimumRate      uint64
}

// marketRun is MarketRunParams plus epoch duration. Duration is not in
// MarketRunParams: a resume may replace the params but must keep the
// duration the resume epoch was scheduled in.
type marketRun struct {
	meshevents.MarketRunParams
	epochDur int64
}

// Market manages the order book, epoch queues, and trading lifecycle for a
// base/quote asset pair. It validates orders, matches them at epoch close,
// and coordinates settlement with the swapper.
type Market struct {
	name            string
	base, quote     uint32
	marketBuyBuffer float64
	// configuredParams contains the locally configured trading parameters
	// used when starting or resuming a market.
	configuredParams marketRun
	// liveParams contains the current trading parameters, restored from storage
	// or applied by lifecycle events. It initially contains configuredParams.
	liveParams atomic.Pointer[marketRun]

	tasks sync.WaitGroup // for lazy asynchronous tasks e.g. revoke ntfns

	running atomic.Bool // true when accepting new orders
	up      uint32      // Run is called, either waiting for first epoch or running

	bookMtx      sync.Mutex // guards book and bookEpochIdx
	book         *book.Book
	acctTracking book.AccountTracking
	bookEpochIdx int64 // next epoch from the point of view of the book
	settling     map[order.OrderID]uint64

	epochMtx                 sync.RWMutex
	startEpochIdx            int64
	activeEpochIdx           int64
	processedEpochIdx        int64
	suspendEpochIdx          int64
	pendingLifecycleAction   db.MarketPendingAction
	pendingLifecycleEpochIdx int64
	pendingLifecycleEpochDur int64
	persistBook              bool
	persistBookSet           bool
	lifecycleState           db.MarketState
	lifecycleWake            chan struct{}
	closureWake              chan struct{}
	epochCommitments         map[order.Commitment]order.OrderID
	epochOrders              map[order.OrderID]order.Order
	currentEpoch             *EpochQueue
	nextEpoch                *EpochQueue

	// resumeSubmitMtx prevents suspended cancellations while resume checks
	// booked orders and applies its event.
	resumeSubmitMtx sync.RWMutex

	matcher  *matcher.Matcher
	swapper  Swapper
	auth     AuthManager
	balancer Balancer

	feeScalesMtx sync.RWMutex
	feeScales    struct {
		base  float64
		quote float64
	}

	coinLockerBase  coinlock.CoinLocker
	coinLockerQuote coinlock.CoinLocker

	baseFeeFetcher  FeeFetcher
	quoteFeeFetcher FeeFetcher

	// Persistent data storage
	storage Storage

	// Data API
	dataCollector DataCollector
	lastRate      uint64

	checkParcelLimit func(user account.AccountID, asOf time.Time, calcParcels MarketParcelCalculator) (bool, error)

	mmSnapshotMtx  sync.RWMutex
	mmSnapshotSubs map[account.AccountID]struct{}

	mesh MeshService
}

// Storage is the DB interface required by Market.
type Storage interface {
	db.OrderArchiver
	LastErr() error
	Fatal() <-chan struct{}
	Close() error
	LastEpochRate(base, quote uint32) (uint64, error)
	MarketMatches(base, quote uint32) ([]*db.MatchDataWithCoins, error)
}

// NewMarket initializes a market for the configured base and quote assets.
// Call LoadState to restore its stored state before processing orders or events.
func NewMarket(cfg *Config) (*Market, error) {
	// Make sure the DEXArchivist is healthy before taking orders.
	storage, mktInfo, swapper := cfg.Storage, cfg.MarketInfo, cfg.Swapper
	if err := storage.LastErr(); err != nil {
		return nil, err
	}

	baseIsAcctBased := cfg.CoinLockerBase == nil
	quoteIsAcctBased := cfg.CoinLockerQuote == nil
	var acctTracking book.AccountTracking

	if baseIsAcctBased {
		acctTracking |= book.AccountTrackingBase
	}
	if quoteIsAcctBased {
		acctTracking |= book.AccountTrackingQuote
	}

	cfgRun := marketRun{
		MarketRunParams: meshevents.MarketRunParams{
			LotSize:                mktInfo.LotSize,
			RateStep:               mktInfo.RateStep,
			ParcelSize:             mktInfo.ParcelSize,
			MaxUserCancelsPerEpoch: mktInfo.MaxUserCancelsPerEpoch,
			MinimumRate:            cfg.MinimumRate,
		},
		epochDur: int64(mktInfo.EpochDuration),
	}
	m := &Market{
		name:             mktInfo.Name,
		base:             mktInfo.Base,
		quote:            mktInfo.Quote,
		marketBuyBuffer:  mktInfo.MarketBuyBuffer,
		configuredParams: cfgRun,
		acctTracking:     acctTracking,
		book:             book.New(mktInfo.LotSize, acctTracking),
		settling:         make(map[order.OrderID]uint64),
		matcher:          matcher.New(),
		persistBook:      true,
		persistBookSet:   true,
		lifecycleWake:    make(chan struct{}, 1),
		closureWake:      make(chan struct{}, 1),
		epochCommitments: make(map[order.Commitment]order.OrderID),
		epochOrders:      make(map[order.OrderID]order.Order),
		swapper:          swapper,
		auth:             cfg.AuthManager,
		balancer:         cfg.Balancer,
		storage:          storage,
		coinLockerBase:   cfg.CoinLockerBase,
		coinLockerQuote:  cfg.CoinLockerQuote,
		baseFeeFetcher:   cfg.FeeFetcherBase,
		quoteFeeFetcher:  cfg.FeeFetcherQuote,
		dataCollector:    cfg.DataCollector,
		checkParcelLimit: cfg.CheckParcelLimit,
		mmSnapshotSubs:   make(map[account.AccountID]struct{}),
	}
	live := cfgRun
	m.liveParams.Store(&live)

	return m, nil
}

// LoadState restores the market's book, lifecycle, epoch queues, and coin
// locks from storage. Call it once on a newly created market, before
// processing orders or events.
func (m *Market) LoadState() error {
	storage := m.storage
	base, quote := m.base, m.quote

	// Restore the stored market parameters before creating the book.
	lifecycle, err := storage.MarketLifecycle(m.name)
	if err != nil {
		return fmt.Errorf("load market lifecycle for %s: %w", m.name, err)
	}
	if lifecycle != nil {
		m.epochMtx.Lock()
		m.projectMarketLifecycleLocked(lifecycle)
		m.epochMtx.Unlock()
	}

	bookOrders, err := storage.BookOrders(base, quote)
	if err != nil {
		return fmt.Errorf("load book orders for %s: %w", m.name, err)
	}
	log.Infof("Loaded %d stored book orders.", len(bookOrders))

	restoredBook := book.New(m.LotSize(), m.acctTracking)
	for _, ord := range bookOrders {
		if !restoredBook.Insert(ord) {
			return fmt.Errorf("failed to restore booked order %v for %s", ord.ID(), m.name)
		}
	}
	m.bookMtx.Lock()
	m.book = restoredBook
	m.bookMtx.Unlock()

	activeMatches, err := storage.MarketMatches(base, quote)
	if err != nil {
		return fmt.Errorf("failed to load active matches for market %v: %w", m.name, err)
	}
	// Restore each order's quantity in active matches. Include canceled orders
	// and orders with previous match failures, allowing their remaining matches
	// to count toward successful completion.
	for _, match := range activeMatches {
		m.settling[match.Taker] += match.Quantity
		m.settling[match.Maker] += match.Quantity
	}
	log.Infof("Tracking %d orders with %d active matches.", len(m.settling), len(activeMatches))

	lastRate, err := storage.LastEpochRate(base, quote)
	if err != nil {
		return fmt.Errorf("failed to load last epoch end rate: %w", err)
	}
	m.lastRate = lastRate

	// Restore coin locks for booked orders before restoring epoch orders.
	if err := restoreStartupBookCoinLocks(m.name, bookOrders, m.coinLockerBase, m.coinLockerQuote); err != nil {
		return err
	}

	return m.restoreEpochState(lifecycle)
}

// projectMarketLifecycleLocked copies durable lifecycle fields into the market.
// Caller must hold epochMtx.
func (m *Market) projectMarketLifecycleLocked(lc *db.MarketLifecycle) {
	m.liveParams.Store(&marketRun{MarketRunParams: lc.RunParams, epochDur: lc.StartEpochDur})
	m.lifecycleState = lc.State
	m.startEpochIdx = lc.StartEpochIdx
	m.suspendEpochIdx = lc.FinalEpochIdx
	m.pendingLifecycleAction = lc.PendingAction
	m.pendingLifecycleEpochIdx = lc.PendingEpochIdx
	m.pendingLifecycleEpochDur = lc.PendingEpochDur
	m.processedEpochIdx = lc.ProcessedEpochIdx
	m.persistBookSet = lc.PersistBook != nil
	if lc.PersistBook != nil {
		m.persistBook = *lc.PersistBook
	}
	if lc.State == db.MarketStateSuspended || lc.State == db.MarketStateDraining {
		m.activeEpochIdx = 0
		m.currentEpoch = nil
		m.nextEpoch = nil
	}
	m.wakeClosureWaiter()
}

// wakeClosureWaiter wakes the epoch advancer after the processed-epoch cursor
// or market lifecycle changes.
func (m *Market) wakeClosureWaiter() {
	select {
	case m.closureWake <- struct{}{}:
	default:
	}
}

// restoreStartupBookCoinLocks locks the funding coins for booked orders.
// If an order conflicts, it releases the locks acquired for preceding orders.
func restoreStartupBookCoinLocks(marketName string, bookOrders []*order.LimitOrder, baseLocker, quoteLocker coinlock.CoinLocker) error {
	candidates := make([]*order.LimitOrder, 0, len(bookOrders))
	for _, ord := range bookOrders {
		locker := quoteLocker
		if ord.Sell {
			locker = baseLocker
		}
		if locker != nil {
			candidates = append(candidates, ord)
		}
	}
	// Lock in order ID order so conflicting orders fail consistently.
	sort.Slice(candidates, func(i, j int) bool {
		idi := candidates[i].ID()
		idj := candidates[j].ID()
		return bytes.Compare(idi[:], idj[:]) < 0
	})

	var lockedBase, lockedQuote []order.OrderID
	rollback := func() {
		if baseLocker != nil {
			baseLocker.UnlockOrdersCoins(lockedBase)
		}
		if quoteLocker != nil {
			quoteLocker.UnlockOrdersCoins(lockedQuote)
		}
	}

	for _, ord := range candidates {
		oid := ord.ID()
		locker := quoteLocker
		if ord.Sell {
			locker = baseLocker
		}
		if failed := locker.LockCoins(map[order.OrderID][]order.CoinID{
			oid: ord.Coins,
		}); len(failed) > 0 {
			rollback()
			return fmt.Errorf("failed to restore startup book coin locks for market %s order %v", marketName, oid)
		}
		if ord.Sell {
			lockedBase = append(lockedBase, oid)
		} else {
			lockedQuote = append(lockedQuote, oid)
		}
	}
	return nil
}

// restoreEpochState restores epoch queues and coin locks for a running market.
// Draining markets only restore coin locks; suspended markets must have no
// stored epoch orders.
func (m *Market) restoreEpochState(lifecycle *db.MarketLifecycle) error {
	if lifecycle == nil {
		return nil
	}
	if lifecycle.State == db.MarketStateSuspended {
		return m.verifyNoStoredEpochOrders()
	}

	// Market is either running or draining.

	epochOrders, err := m.storage.EpochOrders(m.base, m.quote)
	if err != nil {
		return fmt.Errorf("load epoch orders for %s: %w", m.name, err)
	}
	// Restore orders in a consistent order, including which coin conflict fails.
	sortOrdersByID(epochOrders)

	if lifecycle.State == db.MarketStateDraining {
		// Draining markets have stopped accepting orders, but their remaining
		// epoch orders still need coin locks until processing finishes.
		return m.lockEpochOrderCoins(epochOrders)
	}

	if err := m.validateEpochSeed(lifecycle, epochOrders); err != nil {
		return err
	}
	if err := m.lockEpochOrderCoins(epochOrders); err != nil {
		return err
	}
	m.seedEpochQueues(lifecycle.ActiveEpochIdx, lifecycle.StartEpochDur, epochOrders)
	return nil
}

// verifyNoStoredEpochOrders checks that the database has no epoch-status orders
// for this market. This is a sanity check when restoring a suspended market.
func (m *Market) verifyNoStoredEpochOrders() error {
	ords, err := m.storage.EpochOrders(m.base, m.quote)
	if err != nil {
		return fmt.Errorf("load epoch orders for %s: %w", m.name, err)
	}
	if len(ords) > 0 {
		return fmt.Errorf("suspended market %s has %d epoch-status orders; storage is inconsistent",
			m.name, len(ords))
	}
	return nil
}

// validateEpochSeed sanity-checks a running market's stored epoch state.
// It requires a positive active epoch index and rejects orders timestamped
// at or beyond the end of the next epoch.
func (m *Market) validateEpochSeed(lc *db.MarketLifecycle, epochOrders []order.Order) error {
	name := m.name
	active, epochDur := lc.ActiveEpochIdx, lc.StartEpochDur
	if epochDur != m.configuredParams.epochDur {
		log.Warnf("Market %s runs with log-pinned epoch duration %d; configured duration %d "+
			"takes effect at the next market start.", name, epochDur, m.configuredParams.epochDur)
	}
	if active <= 0 {
		return fmt.Errorf("running market %s has no active epoch cursor", name)
	}
	nextEnd := (active + 2) * epochDur
	for _, ord := range epochOrders {
		if ord.Time() >= nextEnd {
			return fmt.Errorf("market %s epoch order %v stamped %d beyond the next epoch window of cursor %d",
				name, ord.ID(), ord.Time(), active)
		}
	}
	return nil
}

// seedEpochQueues projects the current and next epoch queues from the stored
// epoch orders, which must be sorted by order ID.
// Orders from earlier epochs remain in storage with their coins locked,
// but are not inserted into the current or next epoch queue.
func (m *Market) seedEpochQueues(active, epochDur int64, epochOrders []order.Order) {
	currentOrders := ordersInEpoch(epochOrders, active, epochDur)
	nextOrders := ordersInEpoch(epochOrders, active+1, epochDur)
	m.epochMtx.Lock()
	m.currentEpoch = NewEpoch(active, epochDur)
	m.nextEpoch = NewEpoch(active+1, epochDur)
	m.activeEpochIdx = active
	for _, ord := range currentOrders {
		m.insertEpochOrderLocked(m.currentEpoch, ord)
	}
	for _, ord := range nextOrders {
		m.insertEpochOrderLocked(m.nextEpoch, ord)
	}
	m.epochMtx.Unlock()

	m.bookMtx.Lock()
	if active > m.bookEpochIdx {
		m.bookEpochIdx = active
	}
	m.bookMtx.Unlock()
	log.Infof("Seeded market %s epoch memory at epoch %d: %d current, %d next, %d awaiting processing.",
		m.name, active, len(currentOrders), len(nextOrders),
		len(epochOrders)-len(currentOrders)-len(nextOrders))
}

// ordersInEpoch returns the orders in the given epoch, preserving their order.
func ordersInEpoch(ords []order.Order, epochIdx, epochDur int64) []order.Order {
	epoch := NewEpoch(epochIdx, epochDur)
	filtered := make([]order.Order, 0, len(ords))
	for _, ord := range ords {
		if ord == nil {
			continue
		}
		if epoch.IncludesTime(time.UnixMilli(ord.Time())) {
			filtered = append(filtered, ord)
		}
	}
	return filtered
}

func sortOrdersByID(ords []order.Order) {
	sort.Slice(ords, func(i, j int) bool {
		idi, idj := ords[i].ID(), ords[j].ID()
		return bytes.Compare(idi[:], idj[:]) < 0
	})
}

// insertEpochOrderLocked inserts an order into the given epoch queue and the
// market's epoch index maps. m.epochMtx must be held for writing.
func (m *Market) insertEpochOrderLocked(epoch *EpochQueue, ord order.Order) {
	epoch.Insert(ord)
	m.epochOrders[ord.ID()] = ord
	m.epochCommitments[ord.Commitment()] = ord.ID()
}

// lockEpochOrderCoins locks the funding coins of orders reconstructed into
// epoch memory from durable storage, rolling back every lock if any fails.
func (m *Market) lockEpochOrderCoins(ords []order.Order) error {
	var locked []order.Order
	for _, ord := range ords {
		if !m.lockOrderCoins(ord) {
			for _, lockedOrd := range locked {
				m.unlockOrderCoins(lockedOrd)
			}
			return fmt.Errorf("failed to lock epoch order %v coins", ord.ID())
		}
		locked = append(locked, ord)
	}
	return nil
}

// SetMeshService sets the mesh service used to submit commands and events.
// Call it before starting market workers or serving requests.
func (m *Market) SetMeshService(mesh MeshService) {
	m.mesh = mesh
}

func (m *Market) wakeLifecycleDriver() {
	select {
	case m.lifecycleWake <- struct{}{}:
	default:
	}
}

// applyMarketLifecycleRow updates the market's in-memory state from the stored
// lifecycle. It initializes epoch queues when needed, updates whether orders
// are accepted, and wakes the lifecycle driver.
func (m *Market) applyMarketLifecycleRow(lifecycle *db.MarketLifecycle) {
	if lifecycle == nil {
		return
	}
	m.book.SetLotSize(lifecycle.RunParams.LotSize)

	m.epochMtx.Lock()
	m.projectMarketLifecycleLocked(lifecycle)

	isRunning := lifecycle.State == db.MarketStateRunning
	if isRunning && lifecycle.PendingAction == db.MarketPendingNone && m.currentEpoch == nil {
		m.currentEpoch = NewEpoch(lifecycle.ActiveEpochIdx, lifecycle.StartEpochDur)
		m.nextEpoch = NewEpoch(lifecycle.ActiveEpochIdx+1, lifecycle.StartEpochDur)
		m.activeEpochIdx = lifecycle.ActiveEpochIdx
	}

	acceptOrders := isRunning && m.currentEpoch != nil
	m.epochMtx.Unlock()

	m.running.Store(acceptOrders)
	m.wakeLifecycleDriver()
}

func (m *Market) drainingFinalEpoch(epochIdx, epochDur int64) bool {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	return m.lifecycleState == db.MarketStateDraining &&
		m.suspendEpochIdx == epochIdx && m.liveParams.Load().epochDur == epochDur
}

func (m *Market) isDraining() bool {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	return m.lifecycleState == db.MarketStateDraining
}

func (m *Market) hasPendingResume(epochIdx, epochDur int64) bool {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	return m.lifecycleState == db.MarketStateSuspended &&
		m.pendingLifecycleAction == db.MarketPendingResume &&
		m.pendingLifecycleEpochIdx == epochIdx &&
		m.pendingLifecycleEpochDur == epochDur
}

func (m *Market) applyMarketSuspendPurge(purged []order.OrderID) {
	if len(purged) == 0 {
		return
	}
	m.bookMtx.Lock()
	var removed []*order.LimitOrder
	for _, oid := range purged {
		lo, ok := m.book.Remove(oid)
		if !ok {
			continue
		}
		delete(m.settling, oid)
		removed = append(removed, lo)
	}
	m.bookMtx.Unlock()
	for _, lo := range removed {
		m.unlockOrderCoins(lo)
		m.sendRevokeOrderNote(lo.ID(), lo.User())
	}
}

func (m *Market) applyMarketResumeCleanup(revokes []*db.StartupOrderRevoke) []*order.LimitOrder {
	if len(revokes) == 0 {
		return nil
	}
	m.bookMtx.Lock()
	var revokedOrders []order.Order
	var unbookedOrders []*order.LimitOrder
	for _, revoke := range revokes {
		if revoke == nil || revoke.Order == nil {
			continue
		}
		lo, ok := m.book.Remove(revoke.Order.ID())
		if ok {
			delete(m.settling, lo.ID())
			unbookedOrders = append(unbookedOrders, lo)
		}
		revokedOrders = append(revokedOrders, revoke.Order)
	}
	m.bookMtx.Unlock()
	for _, ord := range revokedOrders {
		m.unlockOrderCoins(ord)
		m.sendRevokeOrderNote(ord.ID(), ord.User())
	}
	return unbookedOrders
}

// buildScheduleSuspendEvent builds an event scheduling suspension and returns
// the final trading epoch. That epoch ends at or after asSoonAs.
func (m *Market) buildScheduleSuspendEvent(asSoonAs time.Time, persistBook bool) (*mesh.Event, *SuspendEpoch, error) {
	m.epochMtx.RLock()
	activeEpochIdx := m.activeEpochIdx
	epochDur := m.liveParams.Load().epochDur
	m.epochMtx.RUnlock()
	if activeEpochIdx == 0 {
		return nil, nil, fmt.Errorf("unable to schedule suspend for market %s without an active epoch", m.name)
	}

	ms := asSoonAs.UnixMilli()
	finalEpochIdx := ms / epochDur
	// At an exact boundary, the preceding epoch ends at the requested time.
	if ms%epochDur == 0 {
		finalEpochIdx--
	}
	// The next epoch may already contain accepted orders.
	finalEpochIdx = max(finalEpochIdx, activeEpochIdx+1)
	finalEpochEnd := time.UnixMilli((finalEpochIdx + 1) * epochDur)

	event := &meshevents.MarketSuspendScheduledEvent{
		Market:        m.name,
		FinalEpochIdx: finalEpochIdx,
		EpochDur:      epochDur,
	}
	event.PersistBook = persistBook
	meshEvent, err := mesh.NewEvent(event)
	if err != nil {
		return nil, nil, err
	}
	return meshEvent, &SuspendEpoch{Idx: finalEpochIdx, End: finalEpochEnd}, nil
}

// submitMarketSuspend submits the event that completes the scheduled suspension.
func (m *Market) submitMarketSuspend(ctx context.Context) error {
	m.epochMtx.RLock()
	finalEpochIdx := m.suspendEpochIdx
	finalEpochDur := m.liveParams.Load().epochDur
	m.epochMtx.RUnlock()
	if finalEpochIdx == 0 || finalEpochDur == 0 {
		return fmt.Errorf("market %s has no final epoch to suspend", m.name)
	}
	event := &meshevents.MarketSuspendedEvent{
		Market:        m.name,
		FinalEpochIdx: finalEpochIdx,
		EpochDur:      finalEpochDur,
	}
	event.Timestamp = time.Now().UnixMilli()
	meshEvent, err := mesh.NewEvent(event)
	if err != nil {
		return err
	}
	_, err = m.mesh.ApplyEvent(ctx, meshEvent)
	return err
}

func (m *Market) validateScheduleSuspendEvent(finalEpochIdx, finalEpochDur int64) error {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	if finalEpochDur != int64(m.EpochDuration()) {
		return fmt.Errorf("schedule_suspend epoch duration %d mismatches market duration %d",
			finalEpochDur, m.EpochDuration())
	}
	if m.lifecycleState != db.MarketStateRunning {
		return fmt.Errorf("schedule_suspend rejected for non-running market %s", m.name)
	}
	if m.currentEpoch == nil {
		return fmt.Errorf("schedule_suspend rejected: running market %s has no current epoch", m.name)
	}
	// A pending suspension cannot be moved once its final epoch is active.
	if m.pendingLifecycleAction == db.MarketPendingSuspend && m.pendingLifecycleEpochIdx == m.currentEpoch.Epoch {
		return fmt.Errorf("schedule_suspend rejected: market %s final epoch %d is already closing",
			m.name, m.currentEpoch.Epoch)
	}
	if finalEpochIdx <= m.currentEpoch.Epoch {
		return fmt.Errorf("schedule_suspend final epoch %d is not after current epoch %d",
			finalEpochIdx, m.currentEpoch.Epoch)
	}
	return nil
}

// buildScheduleResumeEvent builds an event scheduling resumption and returns
// the scheduled starting epoch and its start time.
// It fails if the market is already running.
func (m *Market) buildScheduleResumeEvent(asSoonAs time.Time) (*mesh.Event, int64, time.Time, error) {
	if m.Running() {
		return nil, 0, time.Time{}, fmt.Errorf("unable to resume market %s at time %v", m.name, asSoonAs)
	}
	epochDur := m.liveParams.Load().epochDur
	// Resume at the first epoch starting after both the requested time and now.
	startEpochIdx := 1 + max(asSoonAs.UnixMilli(), time.Now().UnixMilli())/epochDur
	startTime := time.UnixMilli(epochDur * startEpochIdx)
	event := &meshevents.MarketResumeScheduledEvent{
		Market:        m.name,
		StartEpochIdx: startEpochIdx,
		EpochDur:      epochDur,
	}
	meshEvent, err := mesh.NewEvent(event)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	return meshEvent, startEpochIdx, startTime, nil
}

// submitMarketResume rechecks booked orders and submits a resume event with
// the required revocations and configured trading parameters.
func (m *Market) submitMarketResume(ctx context.Context, pendingEpochIdx, pendingEpochDur int64) error {
	m.resumeSubmitMtx.Lock()
	defer m.resumeSubmitMtx.Unlock()

	if !m.hasPendingResume(pendingEpochIdx, pendingEpochDur) {
		return fmt.Errorf("market %s has no matching pending resume epoch %d:%d",
			m.name, pendingEpochIdx, pendingEpochDur)
	}

	// Changing the duration would change the time identified by the scheduled epoch.
	if pendingEpochDur != m.configuredParams.epochDur {
		return fmt.Errorf("market %s epoch duration changed from %d to %d; revert the configured duration, "+
			"resume, and change it at the next market start",
			m.name, pendingEpochDur, m.configuredParams.epochDur)
	}

	runParams := m.configuredParams.MarketRunParams
	bookedRevokes, err := m.startupBookedRevokes(ctx, runParams.LotSize)
	if err != nil {
		return err
	}

	// Keep the scheduled epoch so application can verify the pending resume.
	// If funding checks finish late, the timestamp determines the opening epoch.
	event := &meshevents.MarketResumedEvent{
		Market:        m.name,
		StartEpochIdx: pendingEpochIdx,
		EpochDur:      pendingEpochDur,
	}
	event.Timestamp = time.Now().UnixMilli()
	event.ResumeRevokes = encodeStartupOrderRevokes(bookedRevokes)
	event.RunParams = runParams
	meshEvent, err := mesh.NewEvent(event)
	if err != nil {
		return err
	}
	_, err = m.mesh.ApplyEvent(ctx, meshEvent)
	return err
}

// Status describes the operation state of the Market.
type Status struct {
	Running       bool
	EpochDuration uint64 // to compute times from epoch inds
	ActiveEpoch   int64
	StartEpoch    int64
	SuspendEpoch  int64
	// PersistBook reports whether booked orders are retained on suspension.
	// It is non-nil when a suspension is scheduled or the market is suspended.
	PersistBook *bool
	Base, Quote uint32
	LotSize     uint64
	RateStep    uint64
	ParcelSize  uint32
}

// persistBookForStatusLocked reports whether PersistBook should appear in
// Status: only once a suspend/resume decision exists. Caller holds epochMtx.
func (m *Market) persistBookForStatusLocked() *bool {
	if !m.persistBookSet {
		return nil
	}
	if m.suspendEpochIdx == 0 &&
		m.pendingLifecycleAction != db.MarketPendingResume &&
		m.lifecycleState != db.MarketStateSuspended {
		return nil
	}
	persist := m.persistBook
	return &persist
}

// Status returns the current operating state of the Market.
func (m *Market) Status() *Status {
	m.epochMtx.Lock()
	defer m.epochMtx.Unlock()
	return &Status{
		Running:       m.Running(),
		EpochDuration: m.EpochDuration(),
		LotSize:       m.LotSize(),
		RateStep:      m.RateStep(),
		ParcelSize:    m.ParcelSize(),
		ActiveEpoch:   m.activeEpochIdx,
		StartEpoch:    m.startEpochIdx,
		SuspendEpoch:  m.suspendEpochIdx,
		PersistBook:   m.persistBookForStatusLocked(),
		Base:          m.base,
		Quote:         m.quote,
	}
}

// LifecycleState returns the market's stored lifecycle state.
func (m *Market) LifecycleState() db.MarketState {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	return m.lifecycleState
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
	return m.running.Load()
}

// EpochDuration returns the Market's epoch duration in milliseconds.
func (m *Market) EpochDuration() uint64 {
	return uint64(m.liveParams.Load().epochDur)
}

// MarketBuyBuffer returns the lot-size multiplier used to determine the minimum
// quantity for a market buy order.
func (m *Market) MarketBuyBuffer() float64 {
	return m.marketBuyBuffer
}

// LotSize returns the market's lot size in units of the base asset.
func (m *Market) LotSize() uint64 {
	return m.liveParams.Load().LotSize
}

// RateStep returns the market's rate step in units of the quote asset.
func (m *Market) RateStep() uint64 {
	return m.liveParams.Load().RateStep
}

func (m *Market) minimumRate() uint64 {
	return m.liveParams.Load().MinimumRate
}

func (m *Market) maxUserCancelsPerEpoch() uint32 {
	return m.liveParams.Load().MaxUserCancelsPerEpoch
}

// Base is the base asset ID.
func (m *Market) Base() uint32 {
	return m.base
}

// Quote is the quote asset ID.
func (m *Market) Quote() uint32 {
	return m.quote
}

// marketOrderError maps an order submission error to a client RPC error,
// preserving the underlying cause.
func marketOrderError(err error) *msgjson.Error {
	code := msgjson.UnknownMarketError
	switch {
	case errors.Is(err, ErrInternalServer), errors.Is(err, errEpochOrderStorage):
		code = msgjson.RPCInternalError
		log.Errorf("Market order submission failed: %v", err)
	case errors.Is(err, ErrMarketNotRunning):
		code = msgjson.MarketNotRunningError
	case errors.Is(err, ErrQuantityTooHigh):
		code = msgjson.OrderQuantityTooHigh
	case errors.Is(err, ErrInvalidRate), errors.Is(err, ErrInvalidCommitment), errors.Is(err, ErrInvalidOrder):
		code = msgjson.OrderParameterError
	default:
		log.Debugf("Market order submission failed: %v", err)
	}
	return mesh.ClientError(err, code, "%v", err)
}

// resendResultWindow bounds archived-order lookups for resubmitted requests.
// It must exceed the client's maximum order retry duration.
const resendResultWindow = 30 * time.Minute

// matchOrderAndSetTime compares incoming with stored using stored's server time.
// If they match, incoming keeps that time; otherwise its server time is cleared.
func matchOrderAndSetTime(incoming, stored order.Order) bool {
	incoming.SetTime(time.UnixMilli(stored.Time()))
	if incoming.ID() == stored.ID() {
		return true
	}
	// Restore the unstamped state on a miss.
	incoming.SetTime(time.Time{})
	return false
}

// HandleOrderResubmission checks whether a request matches a previously accepted
// order. For active orders, it completes the command with the original order ID
// and server time. For archived orders accepted within resendResultWindow, it
// returns an error. A database lookup failure returns a retryable error.
// It returns handled=false if no matching order is found.
//
// Call it before validating a new order, since an already accepted order may
// no longer pass those checks. Its timestamp may be too old, market parameters
// may have changed, or the original order's funding locks would cause the retry
// to be rejected.
func (m *Market) HandleOrderResubmission(ctx context.Context, rec *orderRecord, completion *mesh.CommandCompletion) (handled bool, rpcErr *msgjson.Error) {
	commit := rec.order.Commitment()

	m.epochMtx.RLock()
	oid, found := m.epochCommitments[commit]
	epochOrd := m.epochOrders[oid]
	m.epochMtx.RUnlock()
	if found && epochOrd != nil && matchOrderAndSetTime(rec.order, epochOrd) {
		m.completeResubmittedOrder(ctx, rec, completion)
		return true, nil
	}

	// Commitments can be reused after an order is archived, so a matching
	// commitment does not necessarily identify the same order request.
	candidates, err := m.storage.OrdersWithCommit(ctx, m.base, m.quote, commit,
		time.Now().Add(-resendResultWindow))
	if err != nil {
		log.Errorf("Resend lookup for commitment %v failed: %v", commit, err)
		return true, msgjson.NewError(msgjson.TryAgainLaterError,
			"order resend lookup unavailable; retry the request")
	}
	for _, candidate := range candidates {
		if !matchOrderAndSetTime(rec.order, candidate.Order) {
			continue
		}
		switch candidate.Status {
		case order.OrderStatusEpoch, order.OrderStatusBooked:
			m.completeResubmittedOrder(ctx, rec, completion)
			return true, nil
		default:
			// A success response would make the client track an archived order.
			return true, msgjson.NewError(msgjson.UnknownOrderError,
				"order %v with this commitment was already accepted and retired", candidate.Order.ID())
		}
	}
	return false, nil
}

// completeResubmittedOrder signs and sends an acceptance response using the
// original server time set by matchOrderAndSetTime. It does not emit an event.
func (m *Market) completeResubmittedOrder(ctx context.Context, rec *orderRecord, completion *mesh.CommandCompletion) {
	result := m.orderResult(rec)
	log.Debugf("Answering resubmission of accepted order %v.", rec.order.ID())
	if err := completion.Complete(ctx, result); err != nil {
		// The order is already accepted; the client can retry delivery.
		log.Errorf("failed to deliver resubmitted order result for %v: %v", rec.order.ID(), err)
	}
}

// stampedOrderAcceptedEvent sets the order's server time, checks its eligibility,
// and builds the order_accepted event and client result.
func (m *Market) stampedOrderAcceptedEvent(rec *orderRecord) (*mesh.Event, *msgjson.OrderResult, *msgjson.Error) {
	sTime := time.Now().Truncate(time.Millisecond).UTC()
	rec.order.SetTime(sTime)
	log.Tracef("Received order %v at %v", rec.order, sTime)

	if err := m.checkOrderEligibility(rec.order); err != nil {
		return nil, nil, marketOrderError(err)
	}

	result := m.orderResult(rec)
	event, err := mesh.NewEvent(meshevents.NewOrderAcceptedEvent(rec.order))
	if err != nil {
		return nil, nil, msgjson.NewError(msgjson.RPCInternalError, "failed to build accepted order event: %v", err)
	}
	return event, result, nil
}

// checkOrderEligibility checks the suspension boundary and the account
// tier and parcel limit for a new order. Cancel orders bypass the account checks.
func (m *Market) checkOrderEligibility(ord order.Order) error {
	if m.orderAtOrAfterSuspendBoundary(ord) {
		return ErrMarketNotRunning
	}
	if ord.Type() == order.CancelOrderType {
		return nil
	}
	oid := ord.ID()
	if _, tier := m.auth.AcctStatus(ord.User()); tier < 1 {
		log.Debugf("Account %v with tier %d not allowed to submit order %v", ord.User(), tier, oid)
		return ErrSuspendedAccount
	}
	return m.validateOrderAcceptedParcelLimit(ord)
}

func (m *Market) orderAtOrAfterSuspendBoundary(ord order.Order) bool {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()
	return m.orderAtOrAfterSuspendBoundaryLocked(ord)
}

func (m *Market) orderAtOrAfterSuspendBoundaryLocked(ord order.Order) bool {
	finalIdx, finalDur := m.pendingLifecycleEpochIdx, m.pendingLifecycleEpochDur
	if m.lifecycleState == db.MarketStateDraining {
		finalIdx, finalDur = m.suspendEpochIdx, m.liveParams.Load().epochDur
	} else if m.pendingLifecycleAction != db.MarketPendingSuspend {
		return false
	}
	if finalIdx == 0 || finalDur == 0 {
		return false
	}
	return ord.Time() >= (finalIdx+1)*finalDur
}

// AcceptOrderCommand validates an order and emits order_accepted, or
// suspended_cancel for a cancel on a suspended market. The router checks for
// resubmissions first; duplicate commitments trigger another lookup here to
// handle requests accepted concurrently.
func (m *Market) AcceptOrderCommand(ctx context.Context, rec *orderRecord, completion *mesh.CommandCompletion) *msgjson.Error {
	if err := m.validateOrder(rec.order); err != nil {
		log.Debugf("AcceptOrderCommand: Invalid order received from user %v with commitment %v: %v",
			rec.order.User(), rec.order.Commitment(), err)
		return marketOrderError(err)
	}

	if !m.Running() {
		if rec.order.Type() != order.CancelOrderType {
			log.Infof("AcceptOrderCommand: Market stopped with an order in submission (commitment %v).",
				rec.order.Commitment())
			return msgjson.NewError(msgjson.MarketNotRunningError, "%v", ErrMarketNotRunning)
		}
		if handled, rpcErr := m.acceptSuspendedCancel(ctx, rec, completion); handled {
			return rpcErr
		}
		// The market resumed while acquiring resumeSubmitMtx; take the normal
		// running-market path below.
	}

	commit := rec.order.Commitment()
	m.epochMtx.RLock()
	otherOID, found := m.epochCommitments[commit]
	m.epochMtx.RUnlock()
	if found {
		// An identical request may have been accepted since the router's lookup.
		// Return its result before rejecting a reused commitment.
		if handled, rpcErr := m.HandleOrderResubmission(ctx, rec, completion); handled {
			return rpcErr
		}
		log.Debugf("Received order with commitment %x also used in previous order %v!",
			commit, otherOID)
		return marketOrderError(ErrInvalidCommitment)
	}

	// If the epoch closes before the event is applied, restamp and retry once.
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		event, result, rpcErr := m.stampedOrderAcceptedEvent(rec)
		if rpcErr != nil {
			return rpcErr
		}
		err = completion.Emit(ctx, event, func() any { return result })
		if err == nil {
			return nil
		}
		if attempt != 0 || !errors.Is(err, ErrEpochMissed) {
			break
		}
		log.Debugf("Restamping order %v after missed epoch during event apply", rec.order.ID())
	}

	if db.IsErrReusedCommit(err) {
		// Check whether a concurrent identical request was accepted.
		if handled, rpcErr := m.HandleOrderResubmission(ctx, rec, completion); handled {
			return rpcErr
		}
		return marketOrderError(ErrInvalidCommitment)
	}
	return marketOrderError(err)
}

// acceptSuspendedCancel submits a cancellation for a booked order in a suspended
// market. It returns handled=false if the market resumed while waiting for
// resumeSubmitMtx, so the caller can accept the cancel into an epoch instead.
func (m *Market) acceptSuspendedCancel(ctx context.Context, rec *orderRecord, completion *mesh.CommandCompletion) (handled bool, rpcErr *msgjson.Error) {
	m.resumeSubmitMtx.RLock()
	defer m.resumeSubmitMtx.RUnlock()

	if m.Running() {
		return false, nil
	}
	m.epochMtx.RLock()
	suspended := m.lifecycleState == db.MarketStateSuspended &&
		(m.pendingLifecycleAction == db.MarketPendingNone || m.pendingLifecycleAction == db.MarketPendingResume)
	m.epochMtx.RUnlock()
	if !suspended {
		return true, msgjson.NewError(msgjson.MarketNotRunningError, "%v", ErrMarketNotRunning)
	}
	event, result, rpcErr := m.stampedSuspendedCancelEvent(rec)
	if rpcErr != nil {
		// Once an identical duplicate applied, CancelableBy no longer sees
		// the target it unbooked.
		if handled, resendErr := m.HandleOrderResubmission(ctx, rec, completion); handled {
			return true, resendErr
		}
		return true, rpcErr
	}
	if err := completion.Emit(ctx, event, func() any { return result }); err != nil {
		// Check whether a concurrent identical request was accepted.
		if handled, resendErr := m.HandleOrderResubmission(ctx, rec, completion); handled {
			return true, resendErr
		}
		return true, marketOrderError(err)
	}
	return true, nil
}

// stampedSuspendedCancelEvent checks the cancellation target, sets the
// cancel's server time, and builds the event and acceptance response.
func (m *Market) stampedSuspendedCancelEvent(rec *orderRecord) (*mesh.Event, *msgjson.OrderResult, *msgjson.Error) {
	co, ok := rec.order.(*order.CancelOrder)
	if !ok {
		return nil, nil, marketOrderError(ErrInvalidOrder)
	}
	if cancelable, _, err := m.CancelableBy(co.TargetOrderID, co.AccountID); !cancelable {
		return nil, nil, marketOrderError(err)
	}
	m.bookMtx.Lock()
	target := m.book.Order(co.TargetOrderID)
	m.bookMtx.Unlock()
	if target == nil {
		return nil, nil, marketOrderError(ErrTargetNotCancelable)
	}

	sTime := time.Now().Truncate(time.Millisecond).UTC()
	co.SetTime(sTime)
	result := m.orderResult(rec)
	dur := int64(m.EpochDuration())
	epochIdx := sTime.UnixMilli() / dur
	event, err := mesh.NewEvent(meshevents.NewSuspendedCancelEvent(m.name, m.base,
		m.quote, co, target, epochIdx, dur, m.getFeeRate(m.Base(), m.baseFeeFetcher),
		m.getFeeRate(m.Quote(), m.quoteFeeFetcher), sTime))
	if err != nil {
		return nil, nil, msgjson.NewError(msgjson.RPCInternalError, "failed to build suspended cancel event: %v", err)
	}
	return event, result, nil
}

// sendSuspendedCancelMatchRequest signs and sends the maker and taker match
// notifications for a cancellation to the locally connected order owner.
func (m *Market) sendSuspendedCancelMatchRequest(user account.AccountID, match *order.Match, serverTime time.Time) {
	if match == nil {
		return
	}
	makerMsg, takerMsg := matchNotifications(match, serverTime)
	m.auth.Sign(makerMsg)
	m.auth.Sign(takerMsg)
	msgs := []msgjson.Signable{makerMsg, takerMsg}
	req, err := msgjson.NewRequest(comms.NextID(), msgjson.MatchRoute, msgs)
	if err != nil {
		log.Errorf("Failed to create suspended cancel match request: %v", err)
		return
	}
	if err = m.auth.RequestIfLocal(user, req, func(_ comms.Link, resp *msgjson.Message) {
		m.processMatchAcksForCancel(user, resp)
	}); err != nil {
		log.Errorf("Failed to send suspended cancel match request: %v", err)
	}
}

// matchNotifications creates a pair of msgjson.Match from a match, stamped
// with the given server time.
func matchNotifications(match *order.Match, serverTime time.Time) (makerMsg *msgjson.Match, takerMsg *msgjson.Match) {
	stamp := uint64(serverTime.UnixMilli())
	return &msgjson.Match{
			OrderID:      idToBytes(match.Maker.ID()),
			MatchID:      idToBytes(match.ID()),
			Quantity:     match.Quantity,
			Rate:         match.Rate,
			Address:      order.ExtractAddress(match.Taker),
			ServerTime:   stamp,
			FeeRateBase:  match.FeeRateBase,
			FeeRateQuote: match.FeeRateQuote,
			Side:         uint8(order.Maker),
		}, &msgjson.Match{
			OrderID:      idToBytes(match.Taker.ID()),
			MatchID:      idToBytes(match.ID()),
			Quantity:     match.Quantity,
			Rate:         match.Rate,
			Address:      order.ExtractAddress(match.Maker),
			ServerTime:   stamp,
			FeeRateBase:  match.FeeRateBase,
			FeeRateQuote: match.FeeRateQuote,
			Side:         uint8(order.Taker),
		}
}

// processMatchAcksForCancel is called when receiving a response to a match
// request for a cancel order. Nothing is done other than logging and verifying
// that the response is in the correct format.
//
// This is currently only used for cancel orders that happen while the market is
// suspended, but may be later used for all cancel orders.
func (m *Market) processMatchAcksForCancel(user account.AccountID, msg *msgjson.Message) {
	var acks []msgjson.Acknowledgement
	err := msg.UnmarshalResult(&acks)
	if err != nil {
		m.respondError(msg.ID, user, msgjson.RPCParseError,
			fmt.Sprintf("error parsing match request acknowledgment: %v", err))
		return
	}
	// The acknowledgment for both the taker and maker should come from the same user.
	expectedNumAcks := 2
	if len(acks) != expectedNumAcks {
		m.respondError(msg.ID, user, msgjson.AckCountError,
			fmt.Sprintf("expected %d acknowledgements, got %d", expectedNumAcks, len(acks)))
		return
	}
	log.Debugf("processMatchAcksForCancel: 'match' ack received from %v", user)
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
	switch {
	case asset == m.base && m.coinLockerBase != nil:
		return m.coinLockerBase.CoinLocked(coin)
	case asset == m.quote && m.coinLockerQuote != nil:
		return m.coinLockerQuote.CoinLocked(coin)
	default:
		panic(fmt.Sprintf("invalid utxo-based asset %d for market %s", asset, m.name))
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
func (m *Market) CancelableBy(oid order.OrderID, aid account.AccountID) (bool, time.Time, error) {
	// All book orders are standing limit orders.
	if lo := m.book.Order(oid); lo != nil {
		if lo.AccountID == aid {
			return true, lo.ServerTime, nil
		}
		return false, time.Time{}, ErrCancelNotPermitted
	}

	// Check the active epochs (includes current and next).
	m.epochMtx.RLock()
	ord := m.epochOrders[oid]
	m.epochMtx.RUnlock()

	if ord == nil {
		return false, time.Time{}, ErrTargetNotActive
	}

	lo, ok := ord.(*order.LimitOrder)
	if !ok {
		return false, time.Time{}, ErrTargetNotCancelable
	}
	if lo.Force != order.StandingTiF {
		return false, time.Time{}, ErrTargetNotCancelable
	}
	if lo.AccountID != aid {
		return false, time.Time{}, ErrCancelNotPermitted
	}
	return true, lo.ServerTime, nil
}

// SwapDone updates an order's outstanding swap quantity. If faulted is true,
// it stops tracking the order and releases any limit-order funding coins. It
// also removes any booked remainder and notifies the owner of that removal.
// It returns the removed order, or nil. Orders no longer tracked for settlement
// are ignored.
func (m *Market) SwapDone(ord order.Order, match *order.Match, faulted bool) *order.LimitOrder {
	if !faulted {
		m.reduceSettling(ord, match)
		return nil
	}

	oid := ord.ID()
	m.bookMtx.Lock()
	settling, found := m.settling[oid]
	if !found {
		m.bookMtx.Unlock()
		return nil
	}
	if settling < match.Quantity {
		log.Errorf("Finished swap %v (qty %d) for order %v larger than current settling (%d) amount.",
			match.ID(), match.Quantity, oid, settling)
	}

	lo, limit := ord.(*order.LimitOrder)
	delete(m.settling, oid)
	var removed bool
	if limit {
		_, removed = m.book.Remove(oid)
	}
	m.bookMtx.Unlock()
	if !limit {
		return nil
	}

	m.unlockOrderCoins(lo)
	if removed {
		m.sendRevokeOrderNote(oid, lo.User())
		return lo
	}
	return nil
}

// reduceSettling subtracts a finished match from the order's outstanding swap
// quantity, retaining the entry while swaps remain or the order is still booked.
func (m *Market) reduceSettling(ord order.Order, match *order.Match) {
	oid := ord.ID()
	m.bookMtx.Lock()
	defer m.bookMtx.Unlock()

	settling, found := m.settling[oid]
	if !found {
		return
	}
	if settling < match.Quantity {
		log.Errorf("Finished swap %v (qty %d) for order %v larger than current settling (%d) amount.",
			match.ID(), match.Quantity, oid, settling)
		settling = 0
	} else {
		settling -= match.Quantity
	}

	// Check the market's book because the supplied order may have an outdated
	// filled amount. A booked order can still make more matches.
	lo, limit := ord.(*order.LimitOrder)
	if settling > 0 || (limit && lo.Force == order.StandingTiF && m.book.HaveOrder(oid)) {
		m.settling[oid] = settling
		return
	}
	delete(m.settling, oid)
}

// CheckUnfilled revokes a user's unfilled booked orders whose funding
// coins are spent and returns the revoked orders.
func (m *Market) CheckUnfilled(assetID uint32, user account.AccountID) []*order.LimitOrder {
	var unfilled []*order.LimitOrder
	switch assetID {
	case m.base:
		// Sell orders are funded by the base asset.
		unfilled = m.book.UnfilledUserSells(user)
	case m.quote:
		// Buy orders are funded by the quote asset.
		unfilled = m.book.UnfilledUserBuys(user)
	default:
		return nil
	}

	spent := m.spentFundingOrders(assetID, unfilled)
	if len(spent) == 0 {
		return nil
	}

	oids := make([]order.OrderID, 0, len(spent))
	for _, lo := range spent {
		oids = append(oids, lo.ID())
	}
	event, err := mesh.NewEvent(meshevents.NewOrdersRevokedForOrdersEvent(m.name, oids,
		meshevents.OrderRevokeReasonFundingSpent, time.Now().UTC()))
	if err != nil {
		log.Errorf("Failed to build orders_revoked event for %d spent-funding orders on market %s: %v",
			len(spent), m.name, err)
		return nil
	}
	result, err := m.mesh.ApplyEvent(context.Background(), event)
	if err != nil {
		log.Errorf("Failed to apply orders_revoked event for %d spent-funding orders on market %s: %v",
			len(spent), m.name, err)
		return nil
	}
	return result.([]*order.LimitOrder)
}

// spentFundingOrders returns the unfilled booked orders with spent funding coins.
func (m *Market) spentFundingOrders(assetID uint32, unfilled []*order.LimitOrder) (spent []*order.LimitOrder) {
	checkUnspent := func(coinID []byte) error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return m.swapper.CheckUnspent(ctx, assetID, coinID)
	}

orders:
	for _, lo := range unfilled {
		log.Tracef("Checking %d funding coins for order %v", len(lo.Coins), lo.ID())
		for _, coinID := range lo.Coins {
			err := checkUnspent(coinID)
			if err == nil {
				continue // unspent, check next coin
			}

			if !errors.Is(err, asset.CoinNotFoundError) {
				// Backend errors do not establish that funding was spent.
				log.Errorf("Unexpected error checking coinID %v for order %v: %v",
					coinID, lo.ID(), err)
				continue orders
			}

			// An order matched during the funding checks may have spent its coins
			// legitimately to fund a swap.
			if lo.Filled() == 0 {
				log.Warnf("Funding coin %s is spent for unfilled order %v", fmtCoinID(assetID, coinID), lo.ID())
				spent = append(spent, lo)
			}
			continue orders
		}
	}
	return
}

// AccountPending sums the orders quantities that pay to or from the specified
// account address.
func (m *Market) AccountPending(acctAddr string, assetID uint32) (qty, lots uint64, redeems int) {
	base, quote := m.base, m.quote
	if (assetID != base && assetID != quote) ||
		(assetID == m.base && m.coinLockerBase != nil) ||
		(assetID == m.quote && m.coinLockerQuote != nil) {

		return
	}

	midGap := m.MidGap()
	if midGap == 0 {
		midGap = m.RateStep()
	}

	lotSize := m.LotSize()
	switch assetID {
	case base:
		m.iterateBaseAccount(acctAddr, func(trade *order.Trade, rate uint64) {
			r := trade.Remaining()
			if trade.Sell {
				qty += r
				lots += r / lotSize
			} else {
				if rate == 0 { // market buy
					redeems += int(calc.QuoteToBase(midGap, r) / lotSize)
				} else {
					redeems += int(r / lotSize)
				}
			}
		})
	case quote:
		m.iterateQuoteAccount(acctAddr, func(trade *order.Trade, rate uint64) {
			r := trade.Remaining()
			if trade.Sell {
				redeems += int(r / lotSize)
			} else {
				if rate == 0 { // market buy
					qty += r
					lots += calc.QuoteToBase(midGap, r) / lotSize
				} else {
					qty += calc.BaseToQuote(midGap, r)
					lots += r / lotSize
				}
			}
		})
	}
	return
}

func (m *Market) iterateBaseAccount(acctAddr string, f func(*order.Trade, uint64)) {
	m.epochMtx.RLock()
	for _, epOrd := range m.epochOrders {
		if epOrd.Type() == order.CancelOrderType || epOrd.Trade().BaseAccount() != acctAddr {
			continue
		}
		var rate uint64
		if lo, is := epOrd.(*order.LimitOrder); is {
			rate = lo.Rate
		}
		f(epOrd.Trade(), rate)

	}
	m.epochMtx.RUnlock()
	m.book.IterateBaseAccount(acctAddr, func(lo *order.LimitOrder) {
		f(lo.Trade(), lo.Rate)
	})
}

func (m *Market) iterateQuoteAccount(acctAddr string, f func(*order.Trade, uint64)) {
	m.epochMtx.RLock()
	for _, epOrd := range m.epochOrders {
		if epOrd.Type() == order.CancelOrderType || epOrd.Trade().QuoteAccount() != acctAddr {
			continue
		}
		var rate uint64
		if lo, is := epOrd.(*order.LimitOrder); is {
			rate = lo.Rate
		}
		f(epOrd.Trade(), rate)
	}
	m.epochMtx.RUnlock()
	m.book.IterateQuoteAccount(acctAddr, func(lo *order.LimitOrder) {
		f(lo.Trade(), lo.Rate)
	})
}

// Book returns the market's current order book and last applied book epoch
// index. A nonzero epoch does not mean the market is accepting orders;
// use Running or Status to check that.
func (m *Market) Book() (epoch int64, buys, sells []*order.LimitOrder) {
	// NOTE: it may be desirable to cache the response.
	m.bookMtx.Lock()
	buys = m.book.BuyOrders()
	sells = m.book.SellOrders()
	epoch = m.bookEpochIdx
	m.bookMtx.Unlock()
	return
}

// Run starts the epoch driver and reports its startup result through
// reportReady, if provided. Successful startup may leave the market suspended.
// Call SetMeshService before Run.
func (m *Market) Run(ctx context.Context, reportReady func(error)) {
	if reportReady == nil {
		reportReady = func(error) {}
	}

	// Prevent concurrent calls to Run.
	if !atomic.CompareAndSwapUint32(&m.up, 0, 1) {
		log.Errorf("Run: Market not stopped!")
		reportReady(Error("market already running"))
		return
	}
	defer atomic.StoreUint32(&m.up, 0)

	driver := newMarketEpochDriver(m)
	ready := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		driver.run(ctx, ready)
	}()

	reportReady(<-ready)
	<-done
}

func (m *Market) coinsLocked(o order.Order) ([]order.CoinID, uint32) {
	if o.Type() == order.CancelOrderType {
		return nil, 0
	}

	locker := m.coinLockerQuote
	assetID := m.quote
	if o.Trade().Trade().Sell {
		locker = m.coinLockerBase
		assetID = m.base
	}

	if locker == nil { // Not utxo-based
		return nil, 0
	}

	// Check if this order is known by the locker.
	lockedCoins := locker.OrderCoinsLocked(o.ID())
	if len(lockedCoins) > 0 {
		return lockedCoins, assetID
	}

	// Check the individual coins.
	for _, coin := range o.Trade().Coins {
		if locker.CoinLocked(coin) {
			lockedCoins = append(lockedCoins, coin)
		}
	}
	return lockedCoins, assetID
}

func (m *Market) lockOrderCoins(o order.Order) bool {
	if o.Type() == order.CancelOrderType {
		return true
	}

	if o.Trade().Sell {
		if m.coinLockerBase != nil {
			return len(m.coinLockerBase.LockOrdersCoins([]order.Order{o})) == 0
		}
	} else if m.coinLockerQuote != nil {
		return len(m.coinLockerQuote.LockOrdersCoins([]order.Order{o})) == 0
	}
	return true
}

func (m *Market) unlockOrderCoins(o order.Order) {
	if o.Type() == order.CancelOrderType {
		return
	}

	if o.Trade().Sell {
		if m.coinLockerBase != nil {
			m.coinLockerBase.UnlockOrderCoins(o.ID())
		}
	} else if m.coinLockerQuote != nil {
		m.coinLockerQuote.UnlockOrderCoins(o.ID())
	}
}

func (m *Market) analysisHelpers() (
	likelyTaker func(ord order.Order) bool,
	baseQty func(ord order.Order) uint64,
) {
	bestBuy, midGap, bestSell := m.rates()
	likelyTaker = func(ord order.Order) bool {
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
	baseQty = func(ord order.Order) uint64 {
		if ord.Type() == order.CancelOrderType {
			return 0
		}
		qty := ord.Trade().Quantity
		if ord.Type() == order.MarketOrderType && !ord.Trade().Sell {
			// Market buy qty is in quote asset. Convert to base.
			if midGap == 0 {
				qty = m.LotSize() // no orders on the book; call it 1 lot
			} else {
				qty = calc.QuoteToBase(midGap, qty)
			}
		}
		return qty
	}
	return
}

// ParcelSize returns the market's current parcel size.
func (m *Market) ParcelSize() uint32 {
	return m.liveParams.Load().ParcelSize
}

// Parcels calculates the total parcels for the market with the specified
// settling quantity. Parcels is used as part of order validation for global
// parcel limits. Parcels is not called for the market for which the order is
// for, which will use m.checkParcelLimit to validate in processOrder.
func (m *Market) Parcels(user account.AccountID, settlingQty uint64) float64 {
	return m.parcels(user, settlingQty)
}

func (m *Market) parcels(user account.AccountID, addParcelWeight uint64) float64 {
	likelyTaker, baseQty := m.analysisHelpers()
	var takerQty, makerQty uint64
	m.epochMtx.RLock()
	for _, epOrd := range m.epochOrders {
		if epOrd.User() != user || epOrd.Type() == order.CancelOrderType {
			continue
		}

		// Even if standing, may count as taker for purposes of taker qty limit.
		if likelyTaker(epOrd) {
			takerQty += baseQty(epOrd)
		} else {
			makerQty += baseQty(epOrd)
		}
	}
	m.epochMtx.RUnlock()

	bookedBuyAmt, bookedSellAmt, _, _ := m.book.UserOrderTotals(user)
	makerQty += bookedBuyAmt + bookedSellAmt
	return calc.Parcels(makerQty+addParcelWeight, takerQty, m.LotSize(), m.ParcelSize())
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
			fmt.Sprintf("error parsing preimage notification response: %v", err))
		return
	}
	if resp.Error != nil {
		log.Warnf("Client failed to handle preimage request: %v", resp.Error)
		sendPI(nil)
		return
	}
	err = json.Unmarshal(resp.Result, &piResp)
	if err != nil {
		sendPI(nil)
		m.respondError(msg.ID, reqData.ord.User(), msgjson.RPCParseError,
			fmt.Sprintf("error parsing preimage response payload result: %v", err))
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
		oc := reqData.ord.Commitment()
		m.respondError(msg.ID, reqData.ord.User(), msgjson.PreimageCommitmentMismatch,
			fmt.Sprintf("preimage hash %x does not match order commitment %x",
				piCommit[:], oc[:]))
		return
	}

	// The preimage is good.
	log.Tracef("Good preimage received for order %v: %x", reqData.ord, pi)

	sendPI(&pi)
}

// collectPreimages solicits preimages from the owners of each of the orders in
// the provided queue with a 'preimage' ntfn/request via AuthManager.Request,
// and returns the preimages contained in the client responses. This function
// can block for up to the preimage deadline (piTimeout below) to allow clients
// time to respond. Clients that fail to respond, or respond with invalid data
// (see handlePreimageResp), are counted as misses.
func (m *Market) collectPreimages(orders []order.Order) (cSum []byte, ordersRevealed []*matcher.OrderRevealed, misses []order.Order) {
	// Compute the commitment checksum for the order queue.
	cSum = matcher.CSum(orders)

	// Two-thirds of an epoch, capped at 20s, so one unresponsive owner cannot
	// delay the close by a full epoch.
	piTimeout := min(20*time.Second, 2*time.Duration(m.EpochDuration())*time.Millisecond/3)
	preimages := make(map[order.Order]chan *order.Preimage, len(orders))
	for _, ord := range orders {
		// Make the 'preimage' request.
		commit := ord.Commitment()
		piReqParams := &msgjson.PreimageRequest{
			OrderID:        idToBytes(ord.ID()),
			Commitment:     commit[:],
			CommitChecksum: cSum,
		}
		req, err := msgjson.NewRequest(comms.NextID(), msgjson.PreimageRoute, piReqParams)
		if err != nil {
			// This is likely an impossible condition, and it is not the
			// client's fault, but the close must give every order a
			// disposition: an order that is neither revealed nor missed
			// would strand in epoch status. Count it as a miss.
			log.Errorf("error creating preimage request: %v", err)
			misses = append(misses, ord)
			continue
		}

		// The client's preimage response comes back via a channel, where nil
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

	orders := epoch.OrderSlice()

	// Start preimage collection.
	go func() {
		cSum, ordersRevealed, misses := m.collectPreimages(orders)
		if len(orders) > 0 {
			log.Infof("Collected %d valid order preimages, missed %d. Commit checksum: %x",
				len(ordersRevealed), len(misses), cSum)
		}
		missRevokeTime := time.Now().Truncate(time.Millisecond).UTC()
		rq.complete(cSum, ordersRevealed, misses, missRevokeTime)
	}()

	return true
}

// validateAdvanceEpochState checks the transition against the current
// epoch queues and returns the orders in the epoch being closed.
func (m *Market) validateAdvanceEpochState(event *meshevents.AdvanceEpochEvent) ([]order.Order, error) {
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()

	if m.currentEpoch == nil {
		return nil, fmt.Errorf("advance_epoch with no active epoch on market %s", m.name)
	}
	if m.currentEpoch.Epoch != event.ClosedEpochIdx {
		return nil, fmt.Errorf("advance_epoch closed epoch %d does not match current epoch %d",
			event.ClosedEpochIdx, m.currentEpoch.Epoch)
	}
	if event.OpenedEpochIdx == 0 {
		if m.pendingLifecycleAction != db.MarketPendingSuspend ||
			m.pendingLifecycleEpochIdx != event.ClosedEpochIdx ||
			m.pendingLifecycleEpochDur != event.EpochDur {
			return nil, fmt.Errorf("advance_epoch final close without matching pending suspend")
		}
	} else if m.nextEpoch != nil && m.nextEpoch.Epoch != event.OpenedEpochIdx {
		return nil, fmt.Errorf("advance_epoch opened epoch %d does not match next epoch %d",
			event.OpenedEpochIdx, m.nextEpoch.Epoch)
	}
	closedOrders := m.currentEpoch.OrderSlice()
	for _, ord := range closedOrders {
		oid := ord.ID()
		if _, found := m.epochOrders[oid]; !found {
			return nil, fmt.Errorf("advance_epoch closed order %v is not in epoch memory", oid)
		}
	}
	return closedOrders, nil
}

// applyAdvanceEpochEvent advances epoch memory after an advance_epoch event.
func (m *Market) applyAdvanceEpochEvent(event *meshevents.AdvanceEpochEvent, closedOrders []order.Order) {
	if event.OpenedEpochIdx > 0 {
		m.bookMtx.Lock()
		if event.OpenedEpochIdx <= m.bookEpochIdx {
			log.Errorf("market %s advance_epoch opened %d but book epoch is already %d",
				m.name, event.OpenedEpochIdx, m.bookEpochIdx)
		} else {
			m.bookEpochIdx = event.OpenedEpochIdx
		}
		m.bookMtx.Unlock()
	}

	m.epochMtx.Lock()
	// Remove closed orders from the epoch maps, retaining their funding locks
	// until epoch processing or startup recovery releases them.
	for _, ord := range closedOrders {
		oid := ord.ID()
		delete(m.epochOrders, oid)
		delete(m.epochCommitments, ord.Commitment())
	}

	if event.OpenedEpochIdx == 0 {
		m.currentEpoch = nil
		m.nextEpoch = nil
		m.activeEpochIdx = 0
		m.lifecycleState = db.MarketStateDraining
		m.pendingLifecycleAction = db.MarketPendingNone
		m.pendingLifecycleEpochIdx = 0
		m.pendingLifecycleEpochDur = 0
		m.epochMtx.Unlock()
		m.running.Store(false)
		m.wakeLifecycleDriver()
		return
	}
	if m.nextEpoch != nil {
		m.currentEpoch = m.nextEpoch
	} else {
		m.currentEpoch = NewEpoch(event.OpenedEpochIdx, event.EpochDur)
	}
	m.nextEpoch = NewEpoch(event.OpenedEpochIdx+1, event.EpochDur)
	m.activeEpochIdx = event.OpenedEpochIdx
	acceptOrders := m.lifecycleState == db.MarketStateRunning
	m.epochMtx.Unlock()
	if acceptOrders {
		m.running.Store(true)
	}
}

func (m *Market) sendRevokeOrderNote(oid order.OrderID, user account.AccountID) {
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
		if err = m.auth.SendIfLocal(user, revNtfn); err != nil {
			log.Debugf("Failed to send %s notification to user %v: %v", route, user, err)
		}
	}
}

// noMatchMessage creates a nomatch notification for the specified order.
func noMatchMessage(oid order.OrderID) (*msgjson.Message, error) {
	return msgjson.NewNotification(msgjson.NoMatchRoute, &msgjson.NoMatch{
		OrderID: oid[:],
	})
}

// sendNoMatchNote sends a nomatch notification to the order owner if they
// are connected to this node.
func (m *Market) sendNoMatchNote(oid order.OrderID, user account.AccountID) {
	msg, err := noMatchMessage(oid)
	if err != nil {
		log.Errorf("Failed to create nomatch notification for order %v: %v", oid, err)
		return
	}

	if err := m.auth.SendIfLocal(user, msg); err != nil {
		log.Debugf("Failed to send nomatch notification to user %v: %v", user, err)
	}
}

// sendPenaltyNote notifies a locally connected user that their trading tier
// is too low.
func (m *Market) sendPenaltyNote(user account.AccountID, penaltyTime time.Time) {
	penaltyNote := &msgjson.PenaltyNote{
		Penalty: &msgjson.Penalty{
			Rule: account.NoRule,
			Time: uint64(penaltyTime.UnixMilli()),
			Details: "Ordering has been suspended for this account. " +
				"Post additional bond to offset violations.",
		},
	}
	m.auth.Sign(penaltyNote)
	note, err := msgjson.NewNotification(msgjson.PenaltyRoute, penaltyNote)
	if err != nil {
		log.Errorf("Failed to create penalty notification for user %v: %v", user, err)
		return
	}
	if err := m.auth.SendIfLocal(user, note); err != nil {
		log.Debugf("Failed to send penalty notification to user %v: %v", user, err)
	}
}

// applyOrderRevokedMemory removes a revoked order from the book and settling
// map, unlocks its funding coins, and notifies its owner. It reports whether
// the order was removed from the book.
func (m *Market) applyOrderRevokedMemory(lo *order.LimitOrder) bool {
	oid := lo.ID()
	m.bookMtx.Lock()
	_, removed := m.book.Remove(oid)
	delete(m.settling, oid)
	m.bookMtx.Unlock()

	m.unlockOrderCoins(lo)

	if !removed {
		log.Errorf("orders_revoked target %v was not on the %s book", oid, m.name)
		return false
	}

	m.sendRevokeOrderNote(oid, lo.User())
	return true
}

// getFeeRate gets the fee rate for an asset.
func (m *Market) getFeeRate(assetID uint32, f FeeFetcher) uint64 {
	// Do not block indefinitely waiting for fetcher.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rate := f.SwapFeeRate(ctx)
	if ctx.Err() != nil { // timeout, try last known rate
		rate = f.LastRate()
		log.Warnf("Failed to get latest fee rate for %v. Using last known rate %d.",
			dex.BipIDSymbol(assetID), rate)
	}
	rate = m.ScaleFeeRate(assetID, rate)
	if rate > f.MaxFeeRate() || rate == 0 {
		rate = f.MaxFeeRate()
	}
	return rate
}

// epochProcessedResult contains the match results and database changes for a
// processed epoch.
type epochProcessedResult struct {
	seed        []byte
	matches     []*order.MatchSet
	revealed    []*matcher.OrderRevealed
	misses      []order.Order
	failed      []*matcher.OrderRevealed
	doneOK      []*matcher.OrderRevealed
	partial     []*matcher.OrderRevealed
	booked      []*matcher.OrderRevealed
	nomatched   []*matcher.OrderRevealed
	unbooked    []*order.LimitOrder
	updates     *matcher.OrdersUpdated
	stats       *matcher.MatchCycleStats
	spot        *msgjson.Spot
	matchReport [][2]int64

	dbUpdate       *db.EpochProcessedUpdate
	dbLog          *db.EventLogEntry
	updateLastRate bool
}

// processReadyEpoch publishes the preimage collection results in an
// epoch_processed event, then requests acknowledgements for new matches.
// An error stops processing for the current market run.
func (m *Market) processReadyEpoch(ctx context.Context, epoch *readyEpoch) error {
	// Ensure the epoch has actually completed preimage collection. This can
	// only fail if the epochPump malfunctioned.
	select {
	case <-epoch.ready:
	default:
		return fmt.Errorf("preimage collection not yet complete for epoch %d", epoch.Epoch)
	}

	// Abort epoch processing if there was a fatal DB backend error.
	if err := m.storage.LastErr(); err != nil {
		return fmt.Errorf("aborting epoch processing on account of failing DB: %w", err)
	}

	// Get the base and quote fee rates.
	// NOTE: We might consider moving this before the match cycle and abandoning
	// the match cycle when no fee rate can be found (on mainnet). The only
	// hesitation there is that it makes certain maintenance tasks longer but
	// that's not unexpected in the world of cryptocurrency exchanges. It also
	// makes it harder to fire up a private DEX server to conduct a private
	// trade, but even in that case, I wouldn't want to be matching at the
	// fallback MaxFeeRate when the justifiable network rate is much lower. I do
	// remember some minor discussion of this at some point in the past, but I'd
	// like to bring it back.
	feeRateBase := m.getFeeRate(m.Base(), m.baseFeeFetcher)
	feeRateQuote := m.getFeeRate(m.Quote(), m.quoteFeeFetcher)
	matchTime := time.Now().Truncate(time.Millisecond).UTC()

	event, err := mesh.NewEvent(meshevents.NewEpochProcessedEvent(m.name, epoch.Epoch, epoch.Duration,
		matchTime, feeRateBase, feeRateQuote, m.lastRate, epoch.cSum, epoch.ordersRevealed, epoch.misses,
		epoch.missRevokeTime))
	if err != nil {
		return fmt.Errorf("failed to build epoch processed event for epoch %d: %w", epoch.Epoch, err)
	}
	res, err := m.mesh.ApplyEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to apply epoch processed event for epoch %d: %w", epoch.Epoch, err)
	}

	// The applier tracks matches on every node. Only the master requests
	// acknowledgements after applying the event.
	result, _ := res.(*epochProcessedResult)
	if result == nil || len(result.matches) == 0 {
		return nil
	}

	log.Debugf("Negotiating %d matches for epoch %d:%d", len(result.matches),
		epoch.Epoch, epoch.Duration)
	m.swapper.RequestMatchAcks(result.matches)
	return nil
}

func (m *Market) applyEpochProcessedEvent(applyCtx *mesh.EventApplyContext, event *meshevents.EpochProcessedEvent, raw *mesh.Event) (*epochProcessedResult, error) {
	m.bookMtx.Lock()
	result, err := m.buildEpochProcessedUpdate(event)
	if err != nil {
		m.bookMtx.Unlock()
		return nil, err
	}
	dbLog, err := m.storage.ApplyEpochProcessedEvent(
		applyCtx, dbEventLogMeta(applyCtx.Position, raw), m.auth.ReputationOutcomePolicy(), result.dbUpdate)
	if err != nil {
		m.bookMtx.Unlock()
		return nil, err
	}
	result.dbLog = dbLog
	m.applyEpochProcessedBookLocked(event, result)
	m.bookMtx.Unlock()
	m.epochMtx.Lock()
	if event.EpochIdx > m.processedEpochIdx {
		m.processedEpochIdx = event.EpochIdx
	}
	m.epochMtx.Unlock()
	m.wakeClosureWaiter()
	m.finalizePreimageMisses(result.misses)
	m.finalizeEpochProcessed(uint64(event.EpochIdx), result)
	return result, nil
}

// finalizePreimageMisses releases funding coins for orders revoked for missing
// preimages and notifies their owners.
func (m *Market) finalizePreimageMisses(misses []order.Order) {
	for _, ord := range misses {
		oid, user := ord.ID(), ord.User()
		log.Infof("Revoked order %v from user %v for missing its preimage.",
			oid, user)
		m.unlockOrderCoins(ord)
		go m.sendRevokeOrderNote(oid, user)
	}
}

func cloneOrderForMatching(ord order.Order) (order.Order, error) {
	switch o := ord.(type) {
	case *order.LimitOrder:
		return cloneLimitOrderForMatching(o), nil
	case *order.MarketOrder:
		return &order.MarketOrder{
			P: o.P,
			T: *o.T.Copy(),
		}, nil
	case *order.CancelOrder:
		return &order.CancelOrder{
			P:             o.P,
			TargetOrderID: o.TargetOrderID,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported order type %T", ord)
	}
}

func cloneLimitOrderForMatching(ord *order.LimitOrder) *order.LimitOrder {
	return &order.LimitOrder{
		P:     ord.P,
		T:     *ord.T.Copy(),
		Rate:  ord.Rate,
		Force: ord.Force,
	}
}

func cloneRevealedOrdersForMatching(revealed []*matcher.OrderRevealed) ([]*matcher.OrderRevealed, error) {
	copies := make([]*matcher.OrderRevealed, 0, len(revealed))
	for _, revealedOrder := range revealed {
		if revealedOrder == nil {
			return nil, fmt.Errorf("nil revealed order")
		}
		ord, err := cloneOrderForMatching(revealedOrder.Order)
		if err != nil {
			return nil, err
		}
		copies = append(copies, &matcher.OrderRevealed{
			Order:    ord,
			Preimage: revealedOrder.Preimage,
		})
	}
	return copies, nil
}

// matchingBookSnapshotLocked copies the book for matching without changing live
// state. The orders must also be copied because matching changes their filled
// quantities. The caller must hold bookMtx.
func (m *Market) matchingBookSnapshotLocked() (*book.Book, error) {
	bookCopy := book.New(m.LotSize(), 0)
	for _, side := range [][]*order.LimitOrder{m.book.BuyOrders(), m.book.SellOrders()} {
		for _, ord := range side {
			orderCopy := cloneLimitOrderForMatching(ord)
			if !bookCopy.Insert(orderCopy) {
				return nil, fmt.Errorf("failed to insert cloned book order %v", ord.ID())
			}
		}
	}
	return bookCopy, nil
}

func stampEpochMatchSets(event *meshevents.EpochProcessedEvent, matches []*order.MatchSet) {
	for _, ms := range matches {
		ms.Epoch.Idx = uint64(event.EpochIdx)
		ms.Epoch.Dur = uint64(event.EpochDur)
		ms.FeeRateBase = event.FeeRateBase
		ms.FeeRateQuote = event.FeeRateQuote
	}
}

// applyEpochStatsLastRate carries the previous trade rate forward when the epoch
// contains no trade matches, including epochs with only cancellations.
func applyEpochStatsLastRate(stats *matcher.MatchCycleStats, lastRate uint64) {
	if stats.EndRate != 0 {
		return
	}
	stats.EndRate = lastRate
	stats.StartRate = lastRate
	stats.HighRate = lastRate
	stats.LowRate = lastRate
}

// epochMatchReport groups consecutive trade matches with the same rate and taker
// side into [rate, signed quantity] pairs. Sell quantities are positive and buy
// quantities are negative. Cancel matches are excluded.
func epochMatchReport(matches []*order.MatchSet) [][2]int64 {
	matchReport := make([][2]int64, 0, len(matches))
	var lastRate uint64
	var lastSell bool
	for _, matchSet := range matches {
		for _, match := range matchSet.Matches() {
			trade := match.Taker.Trade()
			if trade == nil {
				continue
			}
			if len(matchReport) == 0 || match.Rate != lastRate || trade.Sell != lastSell {
				matchReport = append(matchReport, [2]int64{int64(match.Rate), 0})
				lastRate, lastSell = match.Rate, trade.Sell
			}
			if trade.Sell {
				matchReport[len(matchReport)-1][1] += int64(match.Quantity)
			} else {
				matchReport[len(matchReport)-1][1] -= int64(match.Quantity)
			}
		}
	}
	return matchReport
}

// buildEpochProcessedUpdate runs the matcher on copies of the book and revealed
// orders and builds the database update without changing market state.
// The caller must hold bookMtx.
func (m *Market) buildEpochProcessedUpdate(event *meshevents.EpochProcessedEvent) (*epochProcessedResult, error) {
	ordersRevealed, err := event.OrdersRevealed()
	if err != nil {
		return nil, err
	}
	misses, err := event.MissedOrders()
	if err != nil {
		return nil, err
	}

	// Matching changes fills in both the book orders and the revealed orders.
	// Run it on copies first so a failed database update leaves live state
	// unchanged. After the update commits, matching runs again on the live book
	// with the original revealed orders and their initial fills.
	matchingOrders, err := cloneRevealedOrdersForMatching(ordersRevealed)
	if err != nil {
		return nil, err
	}
	bookCopy, err := m.matchingBookSnapshotLocked()
	if err != nil {
		return nil, err
	}
	seed, matches, _, failed, doneOK, partial, booked, nomatched, unbooked, updates, stats := m.matcher.Match(bookCopy, matchingOrders)
	stampEpochMatchSets(event, matches)

	if len(ordersRevealed) > 0 {
		log.Infof("Matching complete for market %v epoch %d:"+
			" %d matches (%d partial fills), %d completed OK (not booked),"+
			" %d booked, %d unbooked, %d failed",
			m.name, event.EpochIdx,
			len(matches), len(partial), len(doneOK),
			len(booked), len(unbooked), len(failed),
		)
	}

	// Build the epoch results and order updates to be stored together.
	oidsRevealed := make([]order.OrderID, 0, len(matchingOrders))
	for _, revealedOrder := range matchingOrders {
		oidsRevealed = append(oidsRevealed, revealedOrder.Order.ID())
	}
	oidsMissed := make([]order.OrderID, 0, len(misses))
	for _, missedOrder := range misses {
		oidsMissed = append(oidsMissed, missedOrder.ID())
	}

	applyEpochStatsLastRate(stats, event.LastRate)

	epochResults := &db.EpochResults{
		MktBase:        m.base,
		MktQuote:       m.quote,
		Idx:            event.EpochIdx,
		Dur:            event.EpochDur,
		MatchTime:      event.MatchTime,
		CSum:           event.CSum,
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
	}

	dbMatches := make([]*order.Match, 0)
	for _, matchSet := range matches {
		dbMatches = append(dbMatches, matchSet.Matches()...)
	}

	missRevokeTime := time.UnixMilli(event.MissRevokeTime)
	missUpdates := make([]*db.PreimageMissUpdate, 0, len(misses))
	for _, ord := range misses {
		missUpdates = append(missUpdates, &db.PreimageMissUpdate{
			Order:      ord,
			RevokeTime: missRevokeTime,
		})
	}
	revealUpdates := make([]*db.PreimageRevealUpdate, 0, len(ordersRevealed))
	for _, revealedOrder := range ordersRevealed {
		revealUpdates = append(revealUpdates, &db.PreimageRevealUpdate{
			Order:    revealedOrder.Order,
			Preimage: revealedOrder.Preimage,
		})
	}

	dbUpdate := &db.EpochProcessedUpdate{
		Epoch:           epochResults,
		Misses:          missUpdates,
		Reveals:         revealUpdates,
		TradesBooked:    updates.TradesBooked,
		TradesPartial:   updates.TradesPartial,
		TradesCompleted: updates.TradesCompleted,
		TradesCanceled:  updates.TradesCanceled,
		TradesFailed:    updates.TradesFailed,
		CancelsFailed:   updates.CancelsFailed,
		CancelsExecuted: updates.CancelsExecuted,
		Matches:         dbMatches,
	}

	return &epochProcessedResult{
		seed:           seed,
		matches:        matches,
		revealed:       ordersRevealed,
		misses:         misses,
		failed:         failed,
		doneOK:         doneOK,
		partial:        partial,
		booked:         booked,
		nomatched:      nomatched,
		unbooked:       unbooked,
		updates:        updates,
		stats:          stats,
		matchReport:    epochMatchReport(matches),
		dbUpdate:       dbUpdate,
		updateLastRate: stats.EndRate != event.LastRate || len(matches) > 0,
	}, nil
}

// matchSetsEqual compares the planned matches with those produced on the live book.
func matchSetsEqual(a, b []*order.MatchSet) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aSet, bSet := a[i], b[i]
		if aSet.Taker.ID() != bSet.Taker.ID() ||
			aSet.Epoch != bSet.Epoch ||
			aSet.FeeRateBase != bSet.FeeRateBase ||
			aSet.FeeRateQuote != bSet.FeeRateQuote ||
			aSet.Total != bSet.Total ||
			len(aSet.Makers) != len(bSet.Makers) ||
			!slices.Equal(aSet.Amounts, bSet.Amounts) ||
			!slices.Equal(aSet.Rates, bSet.Rates) {
			return false
		}
		for j := range aSet.Makers {
			if aSet.Makers[j].ID() != bSet.Makers[j].ID() {
				return false
			}
		}
	}
	return true
}

// applyEpochProcessedBookLocked reruns matching on the live book after the
// database update commits, then updates the quantities awaiting settlement.
// The caller must hold bookMtx.
func (m *Market) applyEpochProcessedBookLocked(event *meshevents.EpochProcessedEvent, result *epochProcessedResult) {
	seed, matches, _, failed, doneOK, partial, booked, nomatched, unbooked, updates, stats := m.matcher.Match(m.book, result.revealed)
	// The database has committed the planned results. Continuing with different
	// in-memory results would leave the market inconsistent.
	if !bytes.Equal(seed, result.seed) {
		panic("live epoch match seed mismatch")
	}
	stampEpochMatchSets(event, matches)
	if !matchSetsEqual(matches, result.matches) {
		panic("live epoch match result mismatch")
	}
	applyEpochStatsLastRate(stats, event.LastRate)

	result.matches = matches
	result.failed = failed
	result.doneOK = doneOK
	result.partial = partial
	result.booked = booked
	result.nomatched = nomatched
	result.unbooked = unbooked
	result.updates = updates
	result.stats = stats
	result.matchReport = epochMatchReport(matches)

	canceled := make([]order.OrderID, 0)
	for _, ms := range matches {
		for _, match := range ms.Matches() {
			if co, ok := match.Taker.(*order.CancelOrder); ok {
				canceled = append(canceled, co.TargetOrderID)
				continue
			}
			m.settling[match.Taker.ID()] += match.Quantity
			m.settling[match.Maker.ID()] += match.Quantity
		}
	}
	// An order may trade and then be canceled in the same epoch. Remove canceled
	// orders after adding all match quantities; they receive no completion credit.
	for _, oid := range canceled {
		delete(m.settling, oid)
	}
}

// finalizeEpochProcessed releases funding locks, updates market statistics,
// and registers matches with the swapper.
func (m *Market) finalizeEpochProcessed(epochID uint64, result *epochProcessedResult) {
	if result.updateLastRate {
		m.lastRate = result.stats.EndRate
	}

	// Completed, failed, and unbooked orders release their funding locks.
	// Orders remaining on the book keep their locks.
	for _, ord := range result.doneOK {
		m.unlockOrderCoins(ord.Order)
	}
	for _, ord := range result.failed {
		m.unlockOrderCoins(ord.Order)
	}
	for _, ord := range result.unbooked {
		m.unlockOrderCoins(ord)
	}

	// Update the API data collector.
	spot, err := m.dataCollector.ReportEpoch(m.Base(), m.Quote(), epochID, result.stats)
	if err != nil {
		log.Errorf("Error updating API data collector: %v", err)
	}
	result.spot = spot

	if len(result.matches) > 0 {
		m.swapper.TrackMatches(result.matches)
	}
}

func (m *Market) validateOrderAcceptedEvent(ord order.Order, book *msgBook) (*validatedOrderAcceptedEvent, error) {
	oid := ord.ID()

	// Accepted orders must be valid and not already booked.
	if err := m.validateOrder(ord); err != nil {
		return nil, err
	}
	if m.book.HaveOrder(oid) {
		return nil, fmt.Errorf("replicated accepted order %v is already booked", oid)
	}

	// Resolve the epoch selected by the order's server time.
	epochIdx, epochDur, epoch, err := m.acceptedOrderEpoch(ord)
	if err != nil {
		return nil, err
	}

	// An order already in epoch memory must not count against its own
	// limits or coin locks.
	alreadyApplied, err := m.checkAcceptedOrderEpochState(ord, epoch)
	if err != nil {
		return nil, err
	}

	// Compute the persisted epoch gap for cancels.
	epochGap := db.EpochGapNA
	if co, ok := ord.(*order.CancelOrder); ok {
		epochGap, err = m.cancelOrderEpochGap(co, epochIdx, epochDur)
		if err != nil {
			log.Debugf("Cancel order %v (account=%v) target order %v: %v",
				co, co.AccountID, co.TargetOrderID, err)
			return nil, err
		}
	}

	if !alreadyApplied {
		// Re-check parcel limits against the local epoch/book projection.
		if err := m.validateOrderAcceptedParcelLimit(ord); err != nil {
			return nil, err
		}

		// Check for locked coins; lock them later during memory apply.
		if lockedCoins, assetID := m.coinsLocked(ord); len(lockedCoins) > 0 {
			return nil, fmt.Errorf("order %v submitted with already-locked %s coins: %v",
				ord.ID(), dex.BipIDSymbol(assetID), fmtCoinIDs(assetID, lockedCoins))
		}
	}

	return &validatedOrderAcceptedEvent{
		mkt:  m,
		book: book,
		update: &db.OrderAcceptedUpdate{
			Order:    ord,
			EpochIdx: epochIdx,
			EpochDur: epochDur,
			EpochGap: epochGap,
		},
		alreadyApplied: alreadyApplied,
	}, nil
}

func (m *Market) acceptedOrderEpoch(ord order.Order) (epochIdx, epochDur int64, epoch *EpochQueue, err error) {
	sTime := time.UnixMilli(ord.Time())
	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()

	if m.currentEpoch == nil {
		return 0, 0, nil, fmt.Errorf("order_accepted with no active epoch on market %s", m.name)
	}
	if m.orderAtOrAfterSuspendBoundaryLocked(ord) {
		return 0, 0, nil, ErrMarketNotRunning
	}
	if m.currentEpoch.IncludesTime(sTime) {
		return m.currentEpoch.Epoch, m.currentEpoch.Duration, m.currentEpoch, nil
	}
	if m.nextEpoch != nil && m.nextEpoch.IncludesTime(sTime) {
		return m.nextEpoch.Epoch, m.nextEpoch.Duration, m.nextEpoch, nil
	}
	return 0, 0, nil, ErrEpochMissed
}

// checkAcceptedOrderEpochState reports whether the order is already in
// epoch memory and checks for commitment conflicts and cancel limits.
func (m *Market) checkAcceptedOrderEpochState(ord order.Order, epoch *EpochQueue) (alreadyApplied bool, err error) {
	oid := ord.ID()
	commit := ord.Commitment()

	m.epochMtx.RLock()
	defer m.epochMtx.RUnlock()

	if existing, found := m.epochOrders[oid]; found {
		if existing.Commitment() == commit {
			return true, nil
		}
		return false, fmt.Errorf("replicated accepted order %v conflicts with existing epoch order", oid)
	}
	if otherOID, commitFound := m.epochCommitments[commit]; commitFound && otherOID != oid {
		return false, fmt.Errorf("replicated accepted order %v conflicts with commitment already used by %v", oid, otherOID)
	}

	if co, ok := ord.(*order.CancelOrder); ok {
		if eco := epoch.CancelTargets[co.TargetOrderID]; eco != nil {
			log.Debugf("Received cancel order %v targeting %v, but already have %v.",
				co, co.TargetOrderID, eco)
			return false, ErrDuplicateCancelOrder
		}
		if nc := epoch.UserCancels[co.AccountID]; nc >= m.maxUserCancelsPerEpoch() {
			log.Debugf("Received cancel order %v targeting %v, but user already has %d cancel orders in this epoch.",
				co, co.TargetOrderID, nc)
			return false, ErrTooManyCancelOrders
		}
	}
	return false, nil
}

func (m *Market) cancelOrderEpochGap(co *order.CancelOrder, epochIdx, epochDur int64) (int32, error) {
	cancelable, loTime, err := m.CancelableBy(co.TargetOrderID, co.AccountID)
	if !cancelable {
		return 0, err
	}
	return int32(epochIdx - loTime.UnixMilli()/epochDur), nil
}

func (m *Market) validateOrderAcceptedParcelLimit(ord order.Order) error {
	if ord.Type() == order.CancelOrderType {
		return nil
	}
	likelyTaker, baseQty := m.analysisHelpers()
	orderWeight := baseQty(ord)
	if likelyTaker(ord) {
		orderWeight *= 2
	}
	user := ord.User()
	calcParcels := func(settlingWeight uint64) float64 {
		return m.parcels(user, settlingWeight+orderWeight)
	}
	ok, err := m.checkParcelLimit(user, time.UnixMilli(ord.Time()).UTC(), calcParcels)
	if err != nil {
		// Reputation load failed: propagate, do not treat as over-limit.
		return fmt.Errorf("parcel limit reputation load for order %v: %w: %w", ord.ID(), ErrInternalServer, err)
	}
	if !ok {
		oid := ord.ID()
		log.Debugf("Received order %s that pushed user over the parcel limit", oid)
		return ErrQuantityTooHigh
	}
	return nil
}

// applyOrderAcceptedMemory projects an accepted order into the in-memory
// market state. Validation and DB persistence have already succeeded.
func (m *Market) applyOrderAcceptedMemory(update *db.OrderAcceptedUpdate) {
	ord := update.Order
	oid := ord.ID()

	if !m.lockOrderCoins(ord) {
		// TODO(mesh): Decide how to handle coin-lock failure after the event is committed.
		// Validation already checked these coins. Continuing would leave the
		// committed order missing from epoch memory.
		panic(fmt.Sprintf("failed to lock accepted order %v coins during memory apply", oid))
	}

	m.epochMtx.Lock()
	epoch := m.acceptedOrderEpochLocked(update.EpochIdx)
	m.insertEpochOrderLocked(epoch, ord)
	m.epochMtx.Unlock()
}

// acceptedOrderEpochLocked returns the previously validated epoch queue.
// The caller holds epochMtx. Mesh serializes event application, so the epoch
// cannot change between validation and this lookup.
func (m *Market) acceptedOrderEpochLocked(epochIdx int64) *EpochQueue {
	if m.currentEpoch != nil && m.currentEpoch.Epoch == epochIdx {
		return m.currentEpoch
	}
	if m.nextEpoch != nil && m.nextEpoch.Epoch == epochIdx {
		return m.nextEpoch
	}

	panic(fmt.Sprintf("accepted order epoch %d is not current or next", epochIdx))
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

	if !db.ValidateOrder(ord, order.OrderStatusEpoch, &dex.MarketInfo{
		Base:    m.base,
		Quote:   m.quote,
		LotSize: m.LotSize(),
	}) {
		return ErrInvalidOrder // non-specific
	}

	if lo, is := ord.(*order.LimitOrder); is && lo.Rate < m.minimumRate() {
		return ErrInvalidRate
	}

	return nil
}

// orderResult signs the stamped order request and returns its signature,
// order ID, and server time.
func (m *Market) orderResult(rec *orderRecord) *msgjson.OrderResult {
	stamp := uint64(rec.order.Time())
	rec.req.Stamp(stamp)
	m.auth.Sign(rec.req)

	oid := rec.order.ID()
	return &msgjson.OrderResult{
		Sig:        rec.req.SigBytes(),
		OrderID:    oid[:],
		ServerTime: stamp,
	}
}

// SetFeeRateScale sets a swap fee scale factor for the given asset.
// SetFeeRateScale should be called regardless of whether the Market is
// suspended.
func (m *Market) SetFeeRateScale(assetID uint32, scale float64) {
	m.feeScalesMtx.Lock()
	switch assetID {
	case m.base:
		m.feeScales.base = scale
	case m.quote:
		m.feeScales.quote = scale
	default:
		log.Errorf("Unknown asset ID %d for market %d-%d",
			assetID, m.base, m.quote)
	}
	m.feeScalesMtx.Unlock()
}

// ScaleFeeRate scales the provided fee rate with the given asset's swap fee
// rate scale factor, which is 1.0 by default.
func (m *Market) ScaleFeeRate(assetID uint32, feeRate uint64) uint64 {
	if feeRate == 0 {
		return feeRate // no idea if this is sensible for any asset, but ok
	}
	var feeScale float64
	m.feeScalesMtx.RLock()
	switch assetID {
	case m.base:
		feeScale = m.feeScales.base
	default:
		feeScale = m.feeScales.quote
	}
	m.feeScalesMtx.RUnlock()
	if feeScale == 0 {
		return feeRate
	}
	if feeScale < 1 {
		log.Warnf("Using fee rate scale of %f < 1.0 for asset %d", feeScale, assetID)
	}
	// It started non-zero, so don't allow it to go to zero.
	return uint64(math.Max(1.0, math.Round(float64(feeRate)*feeScale)))
}

// SubscribeMMSnapshots subscribes or unsubscribes a user from per-epoch market
// making snapshots for this market.
func (m *Market) SubscribeMMSnapshots(user account.AccountID, unsub bool) {
	m.mmSnapshotMtx.Lock()
	if unsub {
		delete(m.mmSnapshotSubs, user)
		log.Debugf("User %v unsubscribed from MM snapshots for %s", user, m.name)
	} else {
		m.mmSnapshotSubs[user] = struct{}{}
		log.Debugf("User %v subscribed to MM snapshots for %s", user, m.name)
	}
	m.mmSnapshotMtx.Unlock()
}

// sendMMSnapshots builds and sends signed epoch snapshots to all MM snapshot
// subscribers after the book has been updated for the given epoch.
func (m *Market) sendMMSnapshots(epochIdx, epochDur int64) {
	m.mmSnapshotMtx.RLock()
	if len(m.mmSnapshotSubs) == 0 {
		m.mmSnapshotMtx.RUnlock()
		return
	}
	subs := make(map[account.AccountID]struct{}, len(m.mmSnapshotSubs))
	for acctID := range m.mmSnapshotSubs {
		subs[acctID] = struct{}{}
	}
	m.mmSnapshotMtx.RUnlock()

	// Hold bookMtx to get an atomic snapshot of the book state.
	m.bookMtx.Lock()
	bestBuy, bestSell := m.book.Best()
	var bestBuyRate, bestSellRate uint64
	if bestBuy != nil {
		bestBuyRate = bestBuy.Rate
	}
	if bestSell != nil {
		bestSellRate = bestSell.Rate
	}

	buyOrders := m.book.BuyOrders()
	sellOrders := m.book.SellOrders()
	m.bookMtx.Unlock()

	base, quote := m.Base(), m.Quote()
	mktID := m.name
	msgEpochIdx := uint64(epochIdx)
	msgEpochDur := uint64(epochDur)

	// Build per-account order lists in a single pass over the book.
	type acctOrders struct {
		buys, sells []msgjson.SnapOrder
	}
	acctMap := make(map[account.AccountID]*acctOrders, len(subs))
	for _, lo := range buyOrders {
		if _, ok := subs[lo.AccountID]; ok {
			ao := acctMap[lo.AccountID]
			if ao == nil {
				ao = &acctOrders{}
				acctMap[lo.AccountID] = ao
			}
			ao.buys = append(ao.buys, msgjson.SnapOrder{
				Rate: lo.Rate,
				Qty:  lo.Remaining(),
			})
		}
	}
	for _, lo := range sellOrders {
		if _, ok := subs[lo.AccountID]; ok {
			ao := acctMap[lo.AccountID]
			if ao == nil {
				ao = &acctOrders{}
				acctMap[lo.AccountID] = ao
			}
			ao.sells = append(ao.sells, msgjson.SnapOrder{
				Rate: lo.Rate,
				Qty:  lo.Remaining(),
			})
		}
	}

	for acctID := range subs {
		ao := acctMap[acctID]
		var buys, sells []msgjson.SnapOrder
		if ao != nil {
			buys, sells = ao.buys, ao.sells
		}

		// Sort by rate ascending for deterministic serialization.
		sort.Slice(buys, func(i, j int) bool { return buys[i].Rate < buys[j].Rate })
		sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })

		const maxSnapOrders = 1<<16 - 1 // uint16 max
		if len(buys) > maxSnapOrders {
			log.Warnf("Truncating %d buy orders to %d for snapshot", len(buys), maxSnapOrders)
			buys = buys[:maxSnapOrders]
		}
		if len(sells) > maxSnapOrders {
			log.Warnf("Truncating %d sell orders to %d for snapshot", len(sells), maxSnapOrders)
			sells = sells[:maxSnapOrders]
		}

		snap := &msgjson.MMEpochSnapshot{
			MarketID:   mktID,
			Base:       base,
			Quote:      quote,
			EpochIdx:   msgEpochIdx,
			EpochDur:   msgEpochDur,
			AccountID:  acctID[:],
			BuyOrders:  buys,
			SellOrders: sells,
			BestBuy:    bestBuyRate,
			BestSell:   bestSellRate,
		}
		m.auth.Sign(snap)

		msg, err := msgjson.NewNotification(msgjson.MMEpochSnapshotRoute, snap)
		if err != nil {
			log.Errorf("Failed to create mm_epoch_snapshot notification: %v", err)
			continue
		}
		if err := m.auth.Send(acctID, msg); err != nil {
			log.Debugf("Failed to send mm_epoch_snapshot to %v (removing subscription): %v", acctID, err)
			m.mmSnapshotMtx.Lock()
			delete(m.mmSnapshotSubs, acctID)
			m.mmSnapshotMtx.Unlock()
		}
	}
}
