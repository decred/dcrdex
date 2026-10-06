// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
	"decred.org/dcrdex/server/swap"
)

type TArchivist struct {
	orderWithKnownCommit          order.OrderID
	commitForKnownOrder           order.Commitment
	canceledOrders                []*order.LimitOrder
	archivedCancels               []*order.CancelOrder
	revoked                       order.Order
	mtx                           sync.Mutex
	poisonEpochOrder              order.Order
	bookedOrders                  []*order.LimitOrder
	epochOrders                   []epochOrderWrite
	orderAcceptedUpdates          []*db.OrderAcceptedUpdate
	marketStartedUpdates          []*db.MarketStartedUpdate
	advanceEpochEvents            []*meshevents.AdvanceEpochEvent
	lifecycle                     *db.MarketLifecycle
	marketSuspendScheduledUpdates []*db.MarketSuspendScheduledUpdate
	marketSuspendedUpdates        []*db.MarketSuspendedUpdate
	marketResumeScheduledUpdates  []*db.MarketResumeScheduledUpdate
	marketResumedUpdates          []*db.MarketResumedUpdate
	lifecyclePurgeOrders          []order.OrderID
	poisonEpochProcessed          bool
	epochProcessed                []*db.EpochProcessedUpdate
	epochInserted                 chan struct{}
	suspendedCancels              []*db.SuspendedCancelUpdate
	ordersRevokedUpdates          []*db.OrdersRevokedUpdate
	commitOrders                  []db.OrderWithStatus
	commitOrdersErr               error
}

type tMesh struct {
	err        *msgjson.Error
	publishErr error

	calls int
	req   mesh.CommandRequest
	user  account.AccountID
	msg   *msgjson.Message

	entries []*mesh.Event
	events  map[string]mesh.EventApplier
}

func (t *tMesh) ExecuteCommand(_ context.Context, req mesh.CommandRequest) *msgjson.Error {
	t.calls++
	t.req = req
	t.user = req.User
	t.msg = req.Msg
	return t.err
}

func (t *tMesh) ApplyEvent(ctx context.Context, event *mesh.Event) (any, error) {
	if t.publishErr != nil {
		return nil, t.publishErr
	}
	if event == nil || t.events == nil {
		return nil, nil
	}
	t.entries = append(t.entries, event)
	applier := t.events[event.Kind]
	if applier == nil {
		return nil, fmt.Errorf("unsupported test mesh event %q", event.Kind)
	}
	applyCtx := &mesh.EventApplyContext{Context: ctx}
	_, err := applier(applyCtx, event)
	return applyCtx.Result(), err
}

func newTMesh(mkt *Market, authMgr *TAuth) *tMesh {
	mktName := mkt.name
	bookRouter := NewBookRouter(map[string]BookSource{
		mktName: mkt,
	}, &tFeeSource{}, func(string, comms.MsgHandler) {})
	return &tMesh{
		events: Events(map[string]*Market{
			mktName: mkt,
		}, bookRouter, authMgr.SendIfLocal, nil),
	}
}

func newMarketStartedEvent(marketName string, currentEpochIdx, epochDur int64, runParams meshevents.MarketRunParams,
	revocationTime time.Time, bookedRevokes []*db.StartupOrderRevoke) mesh.EventEncoder {

	return meshevents.NewMarketStartedEvent(marketName, currentEpochIdx, epochDur, runParams,
		revocationTime, encodeStartupOrderRevokes(bookedRevokes), nil)
}

type marketEventRig struct {
	mkt        *Market
	storage    *TArchivist
	auth       *TAuth
	bookRouter *BookRouter
	events     map[string]mesh.EventApplier
	cleanup    func()
}

func newMarketEventRig(t *testing.T, opts ...any) *marketEventRig {
	t.Helper()
	mkt, storage, auth, cleanup, err := newTestMarket(opts...)
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	mktName := mkt.name
	bookRouter := NewBookRouter(map[string]BookSource{
		mktName: mkt,
	}, &tFeeSource{}, func(string, comms.MsgHandler) {})
	return &marketEventRig{
		mkt:        mkt,
		storage:    storage,
		auth:       auth,
		bookRouter: bookRouter,
		events: Events(map[string]*Market{
			mktName: mkt,
		}, bookRouter, auth.SendIfLocal, nil),
		cleanup: cleanup,
	}
}

type epochProcessedTestSwapper struct {
	tracked []*order.MatchSet
	acked   []*order.MatchSet
}

func (s *epochProcessedTestSwapper) TrackMatches(matchSets []*order.MatchSet) {
	s.tracked = append(s.tracked, matchSets...)
}

func (s *epochProcessedTestSwapper) RequestMatchAcks(matchSets []*order.MatchSet) {
	s.acked = append(s.acked, matchSets...)
}

func (s *epochProcessedTestSwapper) CheckUnspent(context.Context, uint32, []byte) error {
	return nil
}

func (s *epochProcessedTestSwapper) ChainsSynced(uint32, uint32) (bool, error) {
	return true, nil
}

func (rig *marketEventRig) apply(t *testing.T, event mesh.EventEncoder) *mesh.Event {
	t.Helper()
	entry, err := rig.applyResult(t, event)
	if err != nil {
		t.Fatalf("ApplyEvent(%q) error: %v", entry.Kind, err)
	}
	return entry
}

func (rig *marketEventRig) applyErr(t *testing.T, event mesh.EventEncoder) error {
	t.Helper()
	_, err := rig.applyResult(t, event)
	return err
}

func (rig *marketEventRig) applyResult(t *testing.T, event mesh.EventEncoder) (*mesh.Event, error) {
	t.Helper()
	entry, err := mesh.NewEvent(event)
	if err != nil {
		t.Fatalf("event error: %v", err)
	}
	applier := rig.events[entry.Kind]
	if applier == nil {
		t.Fatalf("missing event applier for %q", entry.Kind)
	}
	_, err = applier(&mesh.EventApplyContext{Context: context.Background()}, entry)
	return entry, err
}

func (rig *marketEventRig) submitMarketStarted(t *testing.T, currentEpochIdx int64) {
	t.Helper()
	rig.apply(t, newMarketStartedEvent(rig.mkt.name, currentEpochIdx, int64(rig.mkt.EpochDuration()),
		rig.mkt.configuredParams.MarketRunParams, time.UnixMilli(1).UTC(), nil))
}

func (rig *marketEventRig) subscribeBook(t *testing.T) *TLink {
	t.Helper()
	link, sub := newSubscriber(mkt3)
	if err := rig.bookRouter.handleOrderBook(link, sub); err != nil {
		t.Fatalf("handleOrderBook: %v", err)
	}
	_ = link.getSend() // initial order book response
	return link
}

func submitOrderCommand(t *testing.T, mkt *Market, auth *TAuth, rec *orderRecord) *msgjson.Error {
	t.Helper()
	svc, req := prepareOrderCommand(t, mkt, auth, rec)
	return svc.ExecuteCommand(context.Background(), req)
}

func prepareOrderCommand(t *testing.T, mkt *Market, auth *TAuth, rec *orderRecord) (*mesh.Service, mesh.CommandRequest) {
	t.Helper()

	var kind string
	switch rec.order.Type() {
	case order.LimitOrderType:
		kind = commandKindLimit
	case order.MarketOrderType:
		kind = commandKindMarket
	case order.CancelOrderType:
		kind = commandKindCancel
	default:
		t.Fatalf("unknown order type %v", rec.order.Type())
	}

	tm, ok := mkt.mesh.(*tMesh)
	if !ok {
		t.Fatalf("test market mesh has type %T, not *tMesh", mkt.mesh)
	}
	svc, err := mesh.NewService(&mesh.ServiceConfig{
		EventLogReader: emptyEventLogReader{},
		OnHalt:         func(error) {},
		Commands: map[string]mesh.CommandExecutor{
			kind: func(cmd *mesh.CommandContext) *msgjson.Error {
				return mkt.AcceptOrderCommand(cmd.Context, rec, cmd.Completion)
			},
		},
		Events: tm.events,
	})
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}
	msg, err := msgjson.NewRequest(rec.msgID, kind, nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return svc, mesh.CommandRequest{
		Kind: kind,
		User: rec.order.User(),
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			return auth.Send(rec.order.User(), resp)
		},
	}
}

type epochOrderWrite struct {
	ord      order.Order
	epochIdx int64
	epochDur int64
	epochGap int32
}

func (ta *TArchivist) Close() error           { return nil }
func (ta *TArchivist) LastErr() error         { return nil }
func (ta *TArchivist) Fatal() <-chan struct{} { return nil }
func (ta *TArchivist) Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	return nil, order.OrderStatusUnknown, errors.New("boom")
}

func (ta *TArchivist) OrdersWithCommit(_ context.Context, _, _ uint32, commit order.Commitment, _ time.Time) ([]db.OrderWithStatus, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.commitOrdersErr != nil {
		return nil, ta.commitOrdersErr
	}
	var out []db.OrderWithStatus
	for _, stored := range ta.commitOrders {
		if stored.Order.Commitment() == commit {
			out = append(out, stored)
		}
	}
	return out, nil
}
func (ta *TArchivist) BookOrders(base, quote uint32) ([]*order.LimitOrder, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	return ta.bookedOrders, nil
}
func (ta *TArchivist) EpochOrders(base, quote uint32) ([]order.Order, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ords := make([]order.Order, 0, len(ta.epochOrders))
	for _, write := range ta.epochOrders {
		ords = append(ords, write.ord)
	}
	return ords, nil
}
func (ta *TArchivist) MarketMatches(base, quote uint32) ([]*db.MatchDataWithCoins, error) {
	return nil, nil
}
func (ta *TArchivist) UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error) {
	return nil, errors.New("boom")
}
func (ta *TArchivist) ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error) {
	return nil, errors.New("boom")
}
func (ta *TArchivist) OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error) {
	return order.OrderStatusUnknown, order.UnknownOrderType, -1, errors.New("boom")
}
func (ta *TArchivist) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	return new(db.EventLogPosition), nil
}

func (ta *TArchivist) EventLogEntriesAfter(context.Context, uint64, int) ([]*db.EventLogEntry, error) {
	return nil, nil
}
func (ta *TArchivist) ApplyOrderAcceptedEvent(_ context.Context, _ *db.EventLogMeta, update *db.OrderAcceptedUpdate) (*db.EventLogEntry, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.poisonEpochOrder != nil && update.Order.ID() == ta.poisonEpochOrder.ID() {
		return nil, errors.New("barf")
	}
	ta.orderAcceptedUpdates = append(ta.orderAcceptedUpdates, update)
	return new(db.EventLogEntry), nil
}

func (ta *TArchivist) ApplyMarketStartedEvent(_ context.Context, _ *db.EventLogMeta, update *db.MarketStartedUpdate) (*db.MarketStartedApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.marketStartedUpdates = append(ta.marketStartedUpdates, update)
	// Epoch order status is not projected here: tests seed epochOrders for
	// EpochOrders() reads and assert the recorded update.
	next, changed, err := db.ProjectMarketStartedLifecycle(ta.lifecycle, update)
	if err != nil {
		return nil, err
	}
	if changed {
		ta.lifecycle = next
	}
	return &db.MarketStartedApplyResult{Log: new(db.EventLogEntry), Lifecycle: next}, nil
}
func (ta *TArchivist) MarketLifecycle(string) (*db.MarketLifecycle, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.lifecycle == nil {
		return nil, nil
	}
	cpy := *ta.lifecycle
	if ta.lifecycle.PersistBook != nil {
		persist := *ta.lifecycle.PersistBook
		cpy.PersistBook = &persist
	}
	return &cpy, nil
}
func (ta *TArchivist) ApplyMarketSuspendScheduledEvent(_ context.Context, _ *db.EventLogMeta, update *db.MarketSuspendScheduledUpdate) (*db.MarketSuspendScheduledApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.marketSuspendScheduledUpdates = append(ta.marketSuspendScheduledUpdates, update)
	next, err := db.ProjectMarketSuspendScheduled(ta.lifecycle, update)
	if err != nil {
		return nil, err
	}
	ta.lifecycle = next
	return &db.MarketSuspendScheduledApplyResult{Log: new(db.EventLogEntry), Lifecycle: next}, nil
}

func (ta *TArchivist) ApplyMarketSuspendedEvent(_ context.Context, _ *db.EventLogMeta, update *db.MarketSuspendedUpdate) (*db.MarketSuspendedApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.marketSuspendedUpdates = append(ta.marketSuspendedUpdates, update)
	next, err := db.ProjectMarketSuspended(ta.lifecycle, update)
	if err != nil {
		return nil, err
	}
	ta.lifecycle = next
	return &db.MarketSuspendedApplyResult{Log: new(db.EventLogEntry), Lifecycle: next, PurgeOrders: ta.lifecyclePurgeOrders}, nil
}

func (ta *TArchivist) ApplyMarketResumeScheduledEvent(_ context.Context, _ *db.EventLogMeta, update *db.MarketResumeScheduledUpdate) (*db.MarketResumeScheduledApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.marketResumeScheduledUpdates = append(ta.marketResumeScheduledUpdates, update)
	next, err := db.ProjectMarketResumeScheduled(ta.lifecycle, update)
	if err != nil {
		return nil, err
	}
	ta.lifecycle = next
	return &db.MarketResumeScheduledApplyResult{Log: new(db.EventLogEntry), Lifecycle: next}, nil
}

func (ta *TArchivist) ApplyMarketResumedEvent(_ context.Context, _ *db.EventLogMeta, update *db.MarketResumedUpdate) (*db.MarketResumedApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.marketResumedUpdates = append(ta.marketResumedUpdates, update)
	next, err := db.ProjectMarketResumed(ta.lifecycle, update)
	if err != nil {
		return nil, err
	}
	ta.lifecycle = next
	return &db.MarketResumedApplyResult{Log: new(db.EventLogEntry), Lifecycle: next, ResumeRevokes: update.ResumeRevokes}, nil
}
func (ta *TArchivist) ApplyAdvanceEpochEvent(_ context.Context, _ *db.EventLogMeta, event *meshevents.AdvanceEpochEvent) (*db.EventLogEntry, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.advanceEpochEvents = append(ta.advanceEpochEvents, event)
	// Mirror the real applier's lifecycle projection (including the pending
	// suspend final-close transition to MarketStateDraining), but only
	// when a test has seeded a lifecycle row.
	if ta.lifecycle != nil {
		next, err := db.ProjectAdvanceEpochLifecycle(ta.lifecycle, event)
		if err != nil {
			return nil, err
		}
		ta.lifecycle = next
	}
	return new(db.EventLogEntry), nil
}
func (ta *TArchivist) ApplyOrdersRevokedEvent(_ context.Context, _ *db.EventLogMeta, _ *db.ReputationOutcomePolicy, update *db.OrdersRevokedUpdate) (*db.EventLogEntry, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.ordersRevokedUpdates = append(ta.ordersRevokedUpdates, update)
	return new(db.EventLogEntry), nil
}
func (ta *TArchivist) failOnEpochOrder(ord order.Order) {
	ta.mtx.Lock()
	ta.poisonEpochOrder = ord
	ta.mtx.Unlock()
}
func (ta *TArchivist) ApplyEpochProcessedEvent(_ context.Context, _ *db.EventLogMeta, _ *db.ReputationOutcomePolicy, update *db.EpochProcessedUpdate) (*db.EventLogEntry, error) {
	ta.mtx.Lock()
	if ta.poisonEpochProcessed {
		ta.mtx.Unlock()
		return nil, errors.New("epoch processed storage failure")
	}
	if ta.lifecycle != nil {
		next, err := db.ProjectEpochProcessedLifecycle(ta.lifecycle, ta.lifecycle.Market, update.Epoch.Idx, update.Epoch.Dur)
		if err != nil {
			ta.mtx.Unlock()
			return nil, err
		}
		ta.lifecycle = next
	}
	ta.epochProcessed = append(ta.epochProcessed, update)
	epochInserted := ta.epochInserted
	ta.mtx.Unlock()
	if epochInserted != nil {
		epochInserted <- struct{}{}
	}
	return new(db.EventLogEntry), nil
}
func (ta *TArchivist) ApplySuspendedCancelEvent(_ context.Context, _ *db.EventLogMeta, update *db.SuspendedCancelUpdate) (*db.SuspendedCancelApplyResult, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.suspendedCancels = append(ta.suspendedCancels, update)
	return &db.SuspendedCancelApplyResult{
		Log:         new(db.EventLogEntry),
		Cancel:      update.Cancel,
		TargetOrder: update.Match.Maker,
		Match:       update.Match,
	}, nil
}
func (ta *TArchivist) LastEpochRate(base, quote uint32) (rate uint64, err error) {
	return 1, nil
}
func (ta *TArchivist) BookOrder(lo *order.LimitOrder) error {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	ta.bookedOrders = append(ta.bookedOrders, lo)
	return nil
}

// SwapArchiver for Swapper
func (ta *TArchivist) ActiveSwaps() ([]*db.SwapDataFull, error) { return nil, nil }
func (ta *TArchivist) CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*db.MatchOutcome, error) {
	return nil, nil
}
func (ta *TArchivist) AllActiveUserMatches(account.AccountID) ([]*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*db.MatchStatus, error) {
	return nil, nil
}

func (ta *TArchivist) LoadEpochStats(uint32, uint32, []*candles.Cache) error { return nil }

type TCollector struct{}

var collectorSpot = &msgjson.Spot{
	Stamp: rand.Uint64(),
}

func (tc *TCollector) ReportEpoch(base, quote uint32, epochIdx uint64, stats *matcher.MatchCycleStats) (*msgjson.Spot, error) {
	return collectorSpot, nil
}

type tFeeFetcher struct {
	maxFeeRate uint64
}

func (*tFeeFetcher) FeeRate(context.Context) uint64 {
	return 10
}

func (f *tFeeFetcher) MaxFeeRate() uint64 {
	return f.maxFeeRate
}

func (f *tFeeFetcher) LastRate() uint64 {
	return 10
}

func (f *tFeeFetcher) SwapFeeRate(context.Context) uint64 {
	return 10
}

type tBalancer struct {
	reqs          map[string]int
	checkReserved func(string, uint32) bool
}

func newTBalancer() *tBalancer {
	return &tBalancer{reqs: make(map[string]int)}
}

func (b *tBalancer) CheckBalance(acctAddr string, assetID, redeemAssetID uint32, qty, lots uint64, redeems int) bool {
	b.reqs[acctAddr]++
	return true
}

func (b *tBalancer) CheckReserved(acctAddr string, assetID uint32) bool {
	b.reqs[acctAddr]++
	if b.checkReserved != nil {
		return b.checkReserved(acctAddr, assetID)
	}
	return true
}

func randomOrderID() order.OrderID {
	pk := randomBytes(order.OrderIDSize)
	var id order.OrderID
	copy(id[:], pk)
	return id
}

const (
	tUserTier, tUserScore, tMaxScore = int64(1), int32(30), int32(60)
)

var parcelLimit = float64(calcParcelLimit(tUserTier, tUserScore, tMaxScore))

// tMasterLockers is the swap-side lockers behind the market's book lockers.
type tMasterLockers struct {
	base, quote *coinlock.MasterCoinLocker
}

func newTestMarket(opts ...any) (*Market, *TArchivist, *TAuth, func(), error) {
	// The DEX will make MasterCoinLockers for each asset.
	masterLockerBase := coinlock.NewMasterCoinLocker()
	bookLockerBase := masterLockerBase.Book()
	swapLockerBase := masterLockerBase.Swap()

	masterLockerQuote := coinlock.NewMasterCoinLocker()
	bookLockerQuote := masterLockerQuote.Book()
	swapLockerQuote := masterLockerQuote.Swap()

	epochDurationMSec := uint64(500) // 0.5 sec epoch duration
	storage := &TArchivist{}
	var balancer Balancer

	baseAsset, quoteAsset := assetDCR, assetBTC

	for _, opt := range opts {
		switch optT := opt.(type) {
		case *TArchivist:
			storage = optT
		case *tMasterLockers:
			optT.base, optT.quote = masterLockerBase, masterLockerQuote
		case [2]*asset.BackedAsset:
			baseAsset, quoteAsset = optT[0], optT[1]
			if baseAsset.ID == assetETH.ID || baseAsset.ID == assetMATIC.ID {
				bookLockerBase = nil
			}
			if quoteAsset.ID == assetETH.ID || quoteAsset.ID == assetMATIC.ID {
				bookLockerQuote = nil
			}
		case *tBalancer:
			balancer = optT
		}

	}

	authMgr := &TAuth{
		sends:            make([]*msgjson.Message, 0),
		preimagesByMsgID: make(map[uint64]order.Preimage),
		preimagesByOrdID: make(map[string]order.Preimage),
	}

	var swapDone func(ord order.Order, match *order.Match, fail bool)
	swapperCfg := &swap.Config{
		Assets: map[uint32]*swap.SwapperAsset{
			assetDCR.ID:   {BackedAsset: assetDCR, Locker: swapLockerBase},
			assetBTC.ID:   {BackedAsset: assetBTC, Locker: swapLockerQuote},
			assetETH.ID:   {BackedAsset: assetETH},
			assetMATIC.ID: {BackedAsset: assetMATIC},
		},
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: 10 * time.Second,
		TxWaitExpiration: 5 * time.Second,
		LockTimeTaker:    dex.LockTimeTaker(dex.Testnet),
		LockTimeMaker:    dex.LockTimeMaker(dex.Testnet),
		SwapDone: func(ord order.Order, match *order.Match, fail bool) {
			swapDone(ord, match, fail)
		},
	}
	swapper, err := swap.NewSwapper(swapperCfg)
	if err != nil {
		panic(err.Error())
	}

	mbBuffer := 1.1
	mktInfo, err := dex.NewMarketInfo(baseAsset.ID, quoteAsset.ID,
		dcrLotSize, btcRateStep, epochDurationMSec, mbBuffer)
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("dex.NewMarketInfo() failure: %w", err)
	}

	mkt, err := NewMarket(&Config{
		MarketInfo:      mktInfo,
		Storage:         storage,
		Swapper:         swapper,
		AuthManager:     authMgr,
		FeeFetcherBase:  &tFeeFetcher{baseAsset.MaxFeeRate},
		CoinLockerBase:  bookLockerBase,
		FeeFetcherQuote: &tFeeFetcher{quoteAsset.MaxFeeRate},
		CoinLockerQuote: bookLockerQuote,
		DataCollector:   new(TCollector),
		Balancer:        balancer,
		CheckParcelLimit: func(_ account.AccountID, _ time.Time, f MarketParcelCalculator) (bool, error) {
			parcels := f(0)
			return parcels <= parcelLimit, nil
		},
	})
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("Failed to create test market: %w", err)
	}
	if err := mkt.LoadState(); err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("Failed to load test market state: %w", err)
	}
	mkt.SetMeshService(newTMesh(mkt, authMgr))

	swapDone = func(ord order.Order, match *order.Match, fail bool) {
		mkt.SwapDone(ord, match, fail)
	}

	ssw := dex.NewStartStopWaiter(swapper)
	ssw.Start(testCtx)
	cleanup := func() {
		ssw.Stop()
		ssw.WaitForShutdown()
	}

	return mkt, storage, authMgr, cleanup, nil
}

func TestMarket_LoadState_BookOrders(t *testing.T) {
	rnd.Seed(12)
	randCoinDCR := func() []byte {
		coinID := make([]byte, 36)
		rnd.Read(coinID[:])
		return coinID
	}
	loBuy := makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
	loBuy.FillAmt = dcrLotSize
	loBuy.Coins = []order.CoinID{randCoinDCR()}
	loSell := makeLO(seller3, mkRate3(1.0, 1.2), randLots(10)+1, order.StandingTiF)
	loSell.Coins = []order.CoinID{randCoinDCR()}
	storage := &TArchivist{bookedOrders: []*order.LimitOrder{loBuy, loSell}}

	mkt, _, _, cleanup, err := newTestMarket(storage)
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	_, buys, sells := mkt.Book()
	if len(buys) != 1 || len(sells) != 1 {
		t.Fatalf("Fresh market had %d buys and %d sells, expected 1 buy, 1 sell.", len(buys), len(sells))
	}
	if buys[0].ID() != loBuy.ID() {
		t.Errorf("booked buy order has incorrect ID. Expected %v, got %v", loBuy.ID(), buys[0].ID())
	}
	if sells[0].ID() != loSell.ID() {
		t.Errorf("booked sell order has incorrect ID. Expected %v, got %v", loSell.ID(), sells[0].ID())
	}
	// Both unfilled and partially filled booked orders retain their coin locks.
	for _, lo := range []*order.LimitOrder{loBuy, loSell} {
		assetID := mkt.Quote()
		if lo.Sell {
			assetID = mkt.Base()
		}
		for _, coin := range lo.Coins {
			if !mkt.CoinLocked(assetID, coin) {
				t.Errorf("booked order %v coin %x not locked", lo.ID(), coin)
			}
		}
	}
}

func TestLoadStateAdoptsRowParams(t *testing.T) {
	storage := &TArchivist{}
	const epochDur int64 = 1000 // Different from newTestMarket's configuration.
	row := seedLifecycleRow(db.MarketStateSuspended, db.MarketPendingNone, 40, epochDur)
	row.RunParams = meshevents.MarketRunParams{
		LotSize:                dcrLotSize * 2,
		RateStep:               btcRateStep * 2,
		ParcelSize:             2,
		MaxUserCancelsPerEpoch: 3,
		MinimumRate:            btcRateStep,
	}
	storage.lifecycle = row
	compatible := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
	compatible.Coins = []order.CoinID{[]byte{0x82, 0x01}}
	storage.bookedOrders = []*order.LimitOrder{compatible}

	mkt, _, _, cleanup, err := newTestMarket(storage)
	if err != nil {
		t.Fatalf("newTestMarket: %v", err)
	}
	defer cleanup()

	gotParams := meshevents.MarketRunParams{
		LotSize:                mkt.LotSize(),
		RateStep:               mkt.RateStep(),
		ParcelSize:             mkt.ParcelSize(),
		MaxUserCancelsPerEpoch: mkt.maxUserCancelsPerEpoch(),
		MinimumRate:            mkt.minimumRate(),
	}
	if gotParams != row.RunParams {
		t.Fatalf("restored run parameters = %+v, want %+v", gotParams, row.RunParams)
	}
	if got := mkt.EpochDuration(); got != uint64(epochDur) {
		t.Fatalf("restored epoch duration = %d, want %d", got, epochDur)
	}
	if got := mkt.book.LotSize(); got != uint64(dcrLotSize*2) {
		t.Fatalf("book lot size = %d, want %d", got, dcrLotSize*2)
	}
	if mkt.book.Order(compatible.ID()) == nil {
		t.Fatalf("row-compatible order was not rebooked")
	}
}

// TestLoadStateRejectsIncompatibleBookedOrder pins that a booked DB row
// which does not fit the logged lot size is a LoadState error, not a skip.
func TestLoadStateRejectsIncompatibleBookedOrder(t *testing.T) {
	storage := &TArchivist{}
	const epochDur int64 = 500
	row := seedLifecycleRow(db.MarketStateSuspended, db.MarketPendingNone, 40, epochDur)
	row.RunParams.LotSize = dcrLotSize * 2
	storage.lifecycle = row
	incompatible := makeLO(seller3, mkRate3(1.1, 1.3), 1, order.StandingTiF)
	incompatible.Coins = []order.CoinID{[]byte{0x82, 0x02}}
	storage.bookedOrders = []*order.LimitOrder{incompatible}

	_, _, _, _, err := newTestMarket(storage)
	if err == nil || !strings.Contains(err.Error(), "failed to restore booked order") {
		t.Fatalf("LoadState err = %v, want insert failure", err)
	}
}

func newStartupLockTestMarket(storage *TArchivist, baseLocker, quoteLocker coinlock.CoinLocker) (*Market, error) {
	mktInfo, err := dex.NewMarketInfo(assetDCR.ID, assetBTC.ID, dcrLotSize, btcRateStep, 500, 1.1)
	if err != nil {
		return nil, err
	}
	mkt, err := NewMarket(&Config{
		MarketInfo:      mktInfo,
		Storage:         storage,
		CoinLockerBase:  baseLocker,
		CoinLockerQuote: quoteLocker,
		DataCollector:   new(TCollector),
	})
	if err != nil {
		return nil, err
	}
	if err := mkt.LoadState(); err != nil {
		return nil, err
	}
	return mkt, nil
}

func TestMarket_LoadState_DuplicateBookedCoinLockRollbackAcrossAssets(t *testing.T) {
	baseCoin := order.CoinID([]byte{0x10, 0x20, 0x30})
	sharedQuoteCoin := order.CoinID([]byte{0x40, 0x50, 0x60})

	var sell, buyA, buyB *order.LimitOrder
	var conflict *order.LimitOrder
	// Choose IDs so both a base and a quote coin are locked before the second
	// buy order fails, exercising rollback in both lockers.
	for i := 0; i < 1000; i++ {
		sell = makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		sell.Coins = []order.CoinID{baseCoin}
		buyA = makeLO(buyer3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		buyA.Coins = []order.CoinID{sharedQuoteCoin}
		buyB = makeLO(buyer3, mkRate3(1.1, 1.3), 1, order.StandingTiF)
		buyB.Coins = []order.CoinID{sharedQuoteCoin}

		maxOrder := sell
		for _, lo := range []*order.LimitOrder{buyA, buyB} {
			maxID, oid := maxOrder.ID(), lo.ID()
			if bytes.Compare(maxID[:], oid[:]) < 0 {
				maxOrder = lo
			}
		}
		if maxOrder.ID() == buyA.ID() || maxOrder.ID() == buyB.ID() {
			conflict = maxOrder
			break
		}
	}
	if conflict == nil {
		t.Fatalf("failed to generate deterministic cross-asset rollback order IDs")
	}

	storage := &TArchivist{}
	for _, lo := range []*order.LimitOrder{sell, buyA, buyB} {
		if err := storage.BookOrder(lo); err != nil {
			t.Fatalf("BookOrder error: %v", err)
		}
	}

	baseLocker := coinlock.NewAssetCoinLocker()
	quoteLocker := coinlock.NewAssetCoinLocker()
	_, err := newStartupLockTestMarket(storage, baseLocker, quoteLocker)
	if err == nil {
		t.Fatalf("LoadState succeeded with duplicate quote booked coins")
	}
	if !strings.Contains(err.Error(), conflict.ID().String()) {
		t.Fatalf("LoadState error = %v, want conflicting order %v", err, conflict.ID())
	}
	if baseLocker.CoinLocked(baseCoin) {
		t.Fatalf("base coin remains locked after quote-side restoration failure")
	}
	if quoteLocker.CoinLocked(sharedQuoteCoin) {
		t.Fatalf("quote coin remains locked after quote-side restoration failure")
	}
	if len(storage.marketStartedUpdates) != 0 {
		t.Fatalf("market started update count = %d, want 0", len(storage.marketStartedUpdates))
	}
}

func TestLoadStateEpochs(t *testing.T) {
	const epochDur int64 = 500 // newTestMarket's epoch duration
	const active int64 = 40

	noCursor := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, active, epochDur)
	noCursor.ActiveEpochIdx = 0
	differentDuration := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, active, epochDur)
	differentDuration.StartEpochDur = epochDur * 2
	stray := epochStampedLO(t, active+2, epochDur, 1, order.CoinID{0x60, 0x02})
	leftover := epochStampedLO(t, active, epochDur, 1, order.CoinID{0x60, 0x03})

	pending := epochStampedLO(t, active-1, epochDur, 0, order.CoinID{0x30, 0x01}) // closed, awaiting epoch_processed
	curStart := epochStampedLO(t, active, epochDur, 0, order.CoinID{0x31, 0x01})
	curEnd := epochStampedLO(t, active+1, epochDur, -1, order.CoinID{0x31, 0x02})
	nextStart := epochStampedLO(t, active+1, epochDur, 0, order.CoinID{0x32, 0x01})
	curCancel := epochStampedCO(t, curStart.ID(), active, epochDur, 2)
	finalLO := epochStampedLO(t, active, epochDur, 1, order.CoinID{0x60, 0x04})
	drainA := epochStampedLO(t, active, epochDur, 1, order.CoinID{0x71, 0x01})
	drainB := epochStampedLO(t, active, epochDur, 2, order.CoinID{0x71, 0x02})

	cases := []struct {
		name    string
		row     *db.MarketLifecycle
		orders  []epochOrderWrite
		wantErr string // non-empty: loading must fail with this

		wantCurrent  int64         // expected active epoch, 0 = no epochs seeded
		wantQueued   []order.Order // seeded into the epoch queues and indexes
		wantUnqueued []order.Order // coin-locked but kept out of the queues
	}{{
		name:    "rejects running row without cursor",
		row:     noCursor,
		wantErr: "no active epoch cursor",
	}, {
		name:        "seeds with log duration on config mismatch",
		row:         differentDuration,
		wantCurrent: active,
	}, {
		name:    "rejects order beyond the next epoch window",
		row:     seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, active, epochDur),
		orders:  []epochOrderWrite{{ord: stray, epochIdx: active + 2, epochDur: epochDur}},
		wantErr: "beyond the next epoch window",
	}, {
		name:    "rejects suspended market with epoch orders",
		row:     seedLifecycleRow(db.MarketStateSuspended, db.MarketPendingNone, active, epochDur),
		orders:  []epochOrderWrite{{ord: leftover, epochIdx: active, epochDur: epochDur}},
		wantErr: "storage is inconsistent",
	}, {
		name: "partitions windows around the cursor",
		row:  seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, active, epochDur),
		orders: []epochOrderWrite{
			{ord: pending, epochIdx: active - 1, epochDur: epochDur},
			{ord: curStart, epochIdx: active, epochDur: epochDur},
			{ord: curEnd, epochIdx: active, epochDur: epochDur},
			{ord: curCancel, epochIdx: active, epochDur: epochDur},
			{ord: nextStart, epochIdx: active + 1, epochDur: epochDur},
		},
		wantCurrent:  active,
		wantQueued:   []order.Order{curStart, curEnd, curCancel, nextStart},
		wantUnqueued: []order.Order{pending},
	}, {
		name:        "pending suspend seeds the final epoch with an empty next queue",
		row:         seedLifecycleRow(db.MarketStateRunning, db.MarketPendingSuspend, active, epochDur),
		orders:      []epochOrderWrite{{ord: finalLO, epochIdx: active, epochDur: epochDur}},
		wantCurrent: active,
		wantQueued:  []order.Order{finalLO},
	}, {
		name: "drain seeds no queues but locks the final epoch orders",
		row:  seedLifecycleRow(db.MarketStateDraining, db.MarketPendingNone, active, epochDur),
		orders: []epochOrderWrite{
			{ord: drainA, epochIdx: active, epochDur: epochDur},
			{ord: drainB, epochIdx: active, epochDur: epochDur},
		},
		wantUnqueued: []order.Order{drainA, drainB},
	}, {
		name: "no lifecycle row is never started",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			storage := &TArchivist{
				lifecycle:   tc.row,
				epochOrders: tc.orders,
			}
			mkt, _, _, cleanup, loadErr := newTestMarket(storage)
			defer cleanup()
			if tc.wantErr != "" {
				if loadErr == nil || !strings.Contains(loadErr.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", loadErr, tc.wantErr)
				}
				return
			}
			if loadErr != nil {
				t.Fatalf("loading failed: %v", loadErr)
			}
			if mkt.Running() {
				t.Fatalf("loading opened order admission")
			}
			if tc.row == nil {
				_, buys, sells := mkt.Book()
				if len(buys) > 0 || len(sells) > 0 {
					t.Fatalf("Fresh market had %d buys and %d sells, expected none.", len(buys), len(sells))
				}
			}
			requireSeededState(t, mkt, tc.wantCurrent, tc.wantQueued, tc.wantUnqueued)
		})
	}

	t.Run("rolls back coin locks on an epoch order conflict", func(t *testing.T) {
		mkt, storage, _, cleanup, err := newTestMarket()
		if err != nil {
			t.Fatalf("newTestMarket: %v", err)
		}
		defer cleanup()

		coin := order.CoinID{0x72, 0x01}
		first := epochStampedLO(t, active, epochDur, 1, coin)
		second := epochStampedLO(t, active, epochDur, 2, coin)
		firstID, secondID := first.ID(), second.ID()
		if bytes.Compare(firstID[:], secondID[:]) > 0 {
			first, second = second, first
		}
		// Storage order must not determine which conflicting order fails.
		storage.mtx.Lock()
		storage.epochOrders = []epochOrderWrite{{ord: second}, {ord: first}}
		storage.mtx.Unlock()
		row := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, active, epochDur)
		mkt.epochMtx.Lock()
		mkt.projectMarketLifecycleLocked(row)
		mkt.epochMtx.Unlock()
		err = mkt.restoreEpochState(row)
		wantErr := fmt.Sprintf("failed to lock epoch order %v coins", second.ID())
		if err == nil || err.Error() != wantErr {
			t.Fatalf("restoreEpochState error = %v, want %q", err, wantErr)
		}
		if mkt.coinLockerBase.CoinLocked(coin) {
			t.Fatal("coin remains locked after epoch restoration failed")
		}
		for _, lo := range []*order.LimitOrder{first, second} {
			if len(mkt.coinLockerBase.OrderCoinsLocked(lo.ID())) != 0 {
				t.Fatalf("order %v retains coin locks after epoch restoration failed", lo.ID())
			}
		}
		mkt.epochMtx.RLock()
		defer mkt.epochMtx.RUnlock()
		if mkt.currentEpoch != nil || mkt.nextEpoch != nil || mkt.activeEpochIdx != 0 {
			t.Fatal("epoch queues were published despite failed coin locking")
		}
		if len(mkt.epochOrders) != 0 || len(mkt.epochCommitments) != 0 {
			t.Fatal("epoch indexes were populated despite failed coin locking")
		}
	})
}

// requireSeededState checks the restored epoch queues, indexes, and coin locks.
// Only queued orders belong in the queues and indexes; trade orders in both
// groups must have their funding coins locked under their order IDs.
func requireSeededState(t *testing.T, mkt *Market, wantCurrent int64, queued, unqueued []order.Order) {
	t.Helper()
	epochDur := int64(mkt.EpochDuration())

	requireEpochWindow := func(label string, epoch *EpochQueue, wantIdx int64) {
		t.Helper()
		if epoch == nil || epoch.Epoch != wantIdx || epoch.Duration != epochDur {
			t.Fatalf("%s epoch = %v, want %d/%d", label, epoch, wantIdx, epochDur)
		}
	}
	// An order's stamp picks its queue: wantCurrent's window is the current
	// queue, the following window is next.
	queueFor := func(ord order.Order) *EpochQueue {
		if ord.Time()/epochDur == wantCurrent {
			return mkt.currentEpoch
		}
		return mkt.nextEpoch
	}

	func() {
		t.Helper()
		mkt.epochMtx.RLock()
		defer mkt.epochMtx.RUnlock()

		// The epoch windows and the active cursor.
		if wantCurrent == 0 {
			if mkt.currentEpoch != nil || mkt.nextEpoch != nil || mkt.activeEpochIdx != 0 {
				t.Fatalf("epochs = %v/%v (active %d), want none",
					mkt.currentEpoch, mkt.nextEpoch, mkt.activeEpochIdx)
			}
		} else {
			requireEpochWindow("current", mkt.currentEpoch, wantCurrent)
			requireEpochWindow("next", mkt.nextEpoch, wantCurrent+1)
			if mkt.activeEpochIdx != wantCurrent {
				t.Fatalf("active epoch = %d, want %d", mkt.activeEpochIdx, wantCurrent)
			}
		}

		// Exactly the queued orders are indexed, each in its stamp-selected
		// queue, cancels with their bookkeeping.
		if len(mkt.epochOrders) != len(queued) {
			t.Fatalf("epoch order index has %d orders, want %d", len(mkt.epochOrders), len(queued))
		}
		for _, ord := range queued {
			epoch := queueFor(ord)
			if epoch.Orders[ord.ID()] == nil {
				t.Fatalf("order %v missing from the epoch %d queue", ord.ID(), epoch.Epoch)
			}
			if mkt.epochOrders[ord.ID()] == nil {
				t.Fatalf("order %v missing from the epoch order index", ord.ID())
			}
			if oid := mkt.epochCommitments[ord.Commitment()]; oid != ord.ID() {
				t.Fatalf("commitment index for order %v = %v", ord.ID(), oid)
			}
			if co, ok := ord.(*order.CancelOrder); ok {
				if epoch.CancelTargets[co.TargetOrderID] == nil {
					t.Fatalf("cancel %v missing target bookkeeping", co.ID())
				}
				if epoch.UserCancels[co.AccountID] == 0 {
					t.Fatalf("cancel %v missing user cancel bookkeeping", co.ID())
				}
			}
		}

		// The unqueued orders stay out of the queues and indexes.
		for _, ord := range unqueued {
			if mkt.epochOrders[ord.ID()] != nil {
				t.Fatalf("order %v was seeded into the epoch queues", ord.ID())
			}
			if oid, found := mkt.epochCommitments[ord.Commitment()]; found {
				t.Fatalf("order %v commitment still indexed to %v", ord.ID(), oid)
			}
		}
	}()

	// The book epoch cursor follows the active epoch.
	if wantCurrent != 0 {
		mkt.bookMtx.Lock()
		bookEpochIdx := mkt.bookEpochIdx
		mkt.bookMtx.Unlock()
		if bookEpochIdx != wantCurrent {
			t.Fatalf("bookEpochIdx = %d, want %d", bookEpochIdx, wantCurrent)
		}
	}

	// Every trade's funding coins are locked under its own order ID in the
	// side-appropriate locker, so later unlocks by order ID find them.
	for _, group := range [][]order.Order{queued, unqueued} {
		for _, ord := range group {
			trade := ord.Trade()
			if trade == nil {
				continue // cancels fund nothing
			}
			locker := mkt.coinLockerQuote
			if trade.Sell {
				locker = mkt.coinLockerBase
			}
			locked := make(map[string]bool)
			for _, coin := range locker.OrderCoinsLocked(ord.ID()) {
				locked[string(coin)] = true
			}
			for _, coin := range trade.Coins {
				if !locked[string(coin)] {
					t.Fatalf("coin %v of order %v is not locked under the order's ID", coin, ord.ID())
				}
			}
		}
	}
}

func epochStampedLO(t *testing.T, epochIdx, epochDur, offset int64, coin order.CoinID) *order.LimitOrder {
	t.Helper()
	lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	lo.SetTime(time.UnixMilli(epochIdx*epochDur + offset))
	lo.Coins = []order.CoinID{coin}
	return lo
}

func epochStampedCO(t *testing.T, targetID order.OrderID, epochIdx, epochDur, offset int64) *order.CancelOrder {
	t.Helper()
	co := makeCO(seller3, targetID)
	co.SetTime(time.UnixMilli(epochIdx*epochDur + offset))
	return co
}

func setTestMarketLifecycle(mkt *Market, storage *TArchivist, lc *db.MarketLifecycle) {
	cpy := *lc
	storage.mtx.Lock()
	storage.lifecycle = &cpy
	storage.mtx.Unlock()
	mkt.applyMarketLifecycleRow(&cpy)
}

func seedLifecycleRow(state db.MarketState, pending db.MarketPendingAction, epochIdx, epochDur int64) *db.MarketLifecycle {
	persist := true
	lc := &db.MarketLifecycle{
		Market:        "dcr_btc", // matches newTestMarket
		State:         state,
		StartEpochIdx: epochIdx - 3,
		StartEpochDur: epochDur,
		PendingAction: pending,
		PersistBook:   &persist,
		// Matches newTestMarket's config, so adoption pins the same values
		// the tests' orders are built for.
		RunParams: meshevents.MarketRunParams{
			LotSize:                dcrLotSize,
			RateStep:               btcRateStep,
			ParcelSize:             1,
			MaxUserCancelsPerEpoch: math.MaxUint32,
		},
	}
	switch {
	case state == db.MarketStateSuspended:
		lc.FinalEpochIdx, lc.FinalEpochDur = epochIdx, epochDur
		lc.ProcessedEpochIdx = epochIdx
	case pending == db.MarketPendingSuspend:
		lc.FinalEpochIdx, lc.FinalEpochDur = epochIdx, epochDur
		lc.PendingEpochIdx, lc.PendingEpochDur = epochIdx, epochDur
		lc.ActiveEpochIdx = epochIdx
		lc.ProcessedEpochIdx = epochIdx - 1
	case state == db.MarketStateDraining:
		lc.FinalEpochIdx, lc.FinalEpochDur = epochIdx, epochDur
		lc.ProcessedEpochIdx = epochIdx - 1
	default: // running, nothing pending
		lc.PersistBook = nil
		lc.ActiveEpochIdx = epochIdx
		lc.ProcessedEpochIdx = epochIdx - 1
	}
	return lc
}

func seedEpochOrder(storage *TArchivist, ord order.Order, epochIdx, epochDur int64) {
	storage.mtx.Lock()
	storage.epochOrders = append(storage.epochOrders, epochOrderWrite{
		ord:      ord,
		epochIdx: epochIdx,
		epochDur: epochDur,
	})
	storage.mtx.Unlock()
}

// seedRunningLifecycle restores a running lifecycle row with the given epoch
// cursor and seeds the market's epoch memory from the rig's storage mock,
// mirroring what LoadState does at startup.
func seedRunningLifecycle(t *testing.T, mkt *Market, activeEpochIdx, epochDur int64) {
	t.Helper()
	lc := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, activeEpochIdx, epochDur)
	mkt.epochMtx.Lock()
	mkt.projectMarketLifecycleLocked(lc)
	mkt.epochMtx.Unlock()
	if err := mkt.restoreEpochState(lc); err != nil {
		t.Fatalf("restoreEpochState: %v", err)
	}
}

func TestMarket_Book(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	rnd.Seed(0)

	for i := 0; i < 8; i++ {
		lo := makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
		lo = makeLO(seller3, mkRate3(1.0, 1.2), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
	}

	bestBuy, bestSell := mkt.book.Best()

	marketRate := mkt.MidGap()
	mktRateWant := (bestBuy.Rate + bestSell.Rate) / 2
	if marketRate != mktRateWant {
		t.Errorf("Market rate expected %d, got %d", mktRateWant, marketRate)
	}

	_, buys, sells := mkt.Book()
	if buys[0] != bestBuy {
		t.Errorf("Incorrect best buy order. Got %v, expected %v",
			buys[0], bestBuy)
	}
	if sells[0] != bestSell {
		t.Errorf("Incorrect best sell order. Got %v, expected %v",
			sells[0], bestSell)
	}
}

func TestSwapDone(t *testing.T) {
	matchQty := uint64(dcrLotSize)
	newOrderAndMatch := func(force order.TimeInForce) (*order.LimitOrder, *order.Match) {
		ord := makeLO(seller3, mkRate3(1.0, 1.2), 2, force)
		maker := makeLO(buyer3, ord.Rate, 2, order.StandingTiF)
		return ord, &order.Match{
			Maker:    maker,
			Taker:    ord,
			Quantity: matchQty,
			Rate:     maker.Rate,
		}
	}

	for _, tt := range []struct {
		name              string
		force             order.TimeInForce
		initial           uint64
		hasSettling       bool
		booked            bool
		wantSettling      uint64
		wantSettlingFound bool
	}{
		{
			name:              "remaining swaps keep settling entry",
			force:             order.ImmediateTiF,
			initial:           matchQty * 2,
			hasSettling:       true,
			wantSettling:      matchQty,
			wantSettlingFound: true,
		},
		{
			name:        "last swap clears unbooked order",
			force:       order.ImmediateTiF,
			initial:     matchQty,
			hasSettling: true,
		},
		{
			name:              "booked order keeps zero settling entry",
			force:             order.StandingTiF,
			initial:           matchQty,
			hasSettling:       true,
			booked:            true,
			wantSettlingFound: true,
		},
		{
			name:  "missing settling entry is ignored",
			force: order.ImmediateTiF,
		},
		{
			name:        "insufficient settling quantity does not underflow",
			force:       order.ImmediateTiF,
			initial:     matchQty - 1,
			hasSettling: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ord, match := newOrderAndMatch(tt.force)
			mkt := &Market{
				settling: make(map[order.OrderID]uint64),
				book:     book.New(dcrLotSize, 0),
			}
			if tt.hasSettling {
				mkt.settling[ord.ID()] = tt.initial
			}
			if tt.booked && !mkt.book.Insert(ord) {
				t.Fatal("failed to book order")
			}

			if removed := mkt.SwapDone(ord, match, false); removed != nil {
				t.Fatalf("nonfaulted order was removed: %v", removed.ID())
			}
			got, found := mkt.settling[ord.ID()]
			if found != tt.wantSettlingFound || got != tt.wantSettling {
				t.Fatalf("settling = %d (found %t), want %d (found %t)",
					got, found, tt.wantSettling, tt.wantSettlingFound)
			}
			if onBook := mkt.book.HaveOrder(ord.ID()); onBook != tt.booked {
				t.Fatalf("order on book = %t, want %t", onBook, tt.booked)
			}
		})
	}

	t.Run("faulted booked order is revoked", func(t *testing.T) {
		ord, match := newOrderAndMatch(order.StandingTiF)
		ord.Coins = []order.CoinID{{0x01, 0x02}}
		auth := &TAuth{}
		locker := coinlock.NewAssetCoinLocker()
		mkt := &Market{
			settling:       map[order.OrderID]uint64{ord.ID(): matchQty},
			book:           book.New(dcrLotSize, 0),
			auth:           auth,
			coinLockerBase: locker,
		}
		if !mkt.book.Insert(ord) || !mkt.lockOrderCoins(ord) {
			t.Fatal("failed to book and lock order")
		}
		if !locker.CoinLocked(ord.Coins[0]) {
			t.Fatal("order funding was not locked")
		}

		removed := mkt.SwapDone(ord, match, true)
		if removed == nil || removed.ID() != ord.ID() {
			t.Fatalf("removed order = %v, want %v", removed, ord.ID())
		}
		if _, found := mkt.settling[ord.ID()]; found || mkt.book.HaveOrder(ord.ID()) {
			t.Fatal("faulted order is still tracked or booked")
		}
		if locker.CoinLocked(ord.Coins[0]) {
			t.Fatal("faulted order funding remains locked")
		}
		msg := auth.getSend()
		if msg == nil || msg.Route != msgjson.RevokeOrderRoute {
			t.Fatalf("notification = %v, want revoke_order", msg)
		}
		var note msgjson.RevokeOrder
		if err := msg.Unmarshal(&note); err != nil {
			t.Fatal(err)
		}
		oid := ord.ID()
		if !bytes.Equal(note.OrderID, oid[:]) {
			t.Fatalf("revoked order = %x, want %v", note.OrderID, oid)
		}
		if auth.getSend() != nil {
			t.Fatal("unexpected extra notification")
		}
	})

	t.Run("faulted missing settling entry is ignored", func(t *testing.T) {
		ord, match := newOrderAndMatch(order.ImmediateTiF)
		auth := &TAuth{}
		mkt := &Market{settling: make(map[order.OrderID]uint64), auth: auth}
		if removed := mkt.SwapDone(ord, match, true); removed != nil {
			t.Fatalf("untracked order was removed: %v", removed.ID())
		}
		if len(mkt.settling) != 0 || auth.getSend() != nil {
			t.Fatal("untracked order changed settling or sent a notification")
		}
	})
}

func TestMarketEpochDriverStartupAndAdvance(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	t.Cleanup(cleanup)

	dur := int64(mkt.EpochDuration())
	startEpoch := currentEpochWithHeadroom(t, dur)
	// Startup should replace a previously scheduled start with the current epoch.
	mkt.startEpochIdx = startEpoch + 2

	tm := mkt.mesh.(*tMesh)
	advanceEvents := observeDriverEvents(tm, meshevents.EventKindAdvanceEpoch)

	ctx, cancel := context.WithCancel(context.Background())
	startupDone, runDone := startTestEpochDriver(t, mkt, ctx, cancel)

	select {
	case err := <-startupDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for market_started startup result")
	}

	status := mkt.Status()
	if !status.Running || status.ActiveEpoch != startEpoch || status.StartEpoch != startEpoch {
		t.Fatalf("status = running %v active %d start %d, want true/%d/%d",
			status.Running, status.ActiveEpoch, status.StartEpoch, startEpoch, startEpoch)
	}
	mkt.epochMtx.RLock()
	currentEpoch, nextEpoch := mkt.currentEpoch, mkt.nextEpoch
	mkt.epochMtx.RUnlock()
	if currentEpoch == nil || currentEpoch.Epoch != startEpoch {
		t.Fatalf("current epoch = %v, want %d", currentEpoch, startEpoch)
	}
	if nextEpoch == nil || nextEpoch.Epoch != startEpoch+1 {
		t.Fatalf("next epoch = %v, want %d", nextEpoch, startEpoch+1)
	}
	advanceDeadline := nextEpoch.Start.Add(time.Duration(dur/2) * time.Millisecond)
	select {
	case observed := <-advanceEvents:
		advance, err := meshevents.DecodeAdvanceEpochEvent(observed.event.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if observed.submittedAt.Before(nextEpoch.Start) || observed.submittedAt.After(advanceDeadline) {
			t.Fatalf("advance_epoch submitted at %v, want between %v and %v",
				observed.submittedAt, nextEpoch.Start, advanceDeadline)
		}
		if advance.ClosedEpochIdx != startEpoch || advance.OpenedEpochIdx != startEpoch+1 {
			t.Fatalf("advance_epoch closed/opened = %d/%d, want %d/%d",
				advance.ClosedEpochIdx, advance.OpenedEpochIdx, startEpoch, startEpoch+1)
		}
	case <-time.After(time.Until(advanceDeadline)):
		t.Fatal("timed out waiting for first advance_epoch")
	}
	cancel()
	waitForDriverStop(t, runDone)
	if len(tm.entries) < 2 || tm.entries[0].Kind != meshevents.EventKindMarketStarted ||
		tm.entries[1].Kind != meshevents.EventKindAdvanceEpoch {
		t.Fatal("expected one market_started followed by advance_epoch")
	}
}

func TestMarketEpochDriverSuspendsAfterFinalEpoch(t *testing.T) {
	mkt, storage, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	dur := int64(mkt.EpochDuration())
	finalEpoch := currentEpochWithHeadroom(t, dur)
	setTestMarketLifecycle(mkt, storage, seedLifecycleRow(
		db.MarketStateRunning, db.MarketPendingSuspend, finalEpoch, dur))

	// Hold the final epoch before its processing results are applied.
	tm := mkt.mesh.(*tMesh)
	processingStarted, finishProcessing := make(chan struct{}), make(chan struct{})
	applyProcessed := tm.events[meshevents.EventKindEpochProcessed]
	tm.events[meshevents.EventKindEpochProcessed] = func(ctx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
		close(processingStarted)
		select {
		case <-finishProcessing:
			return applyProcessed(ctx, event)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	suspended := observeDriverEvents(tm, meshevents.EventKindMarketSuspended)
	ctx, cancel := context.WithCancel(context.Background())
	ready, done := startTestEpochDriver(t, mkt, ctx, cancel)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("driver did not finish startup")
	}
	select {
	case <-processingStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("final epoch did not reach processing")
	}

	mkt.epochMtx.RLock()
	state, nextEpoch, processedEpoch := mkt.lifecycleState, mkt.nextEpoch, mkt.processedEpochIdx
	mkt.epochMtx.RUnlock()
	if state != db.MarketStateDraining || nextEpoch != nil || processedEpoch != finalEpoch-1 {
		t.Fatalf("while processing: state %v, next epoch %v, processed %d; want draining, nil, %d",
			state, nextEpoch, processedEpoch, finalEpoch-1)
	}
	select {
	case <-suspended:
		t.Fatal("market suspended before the final epoch was processed")
	default:
	}

	close(finishProcessing)
	select {
	case <-suspended:
	case <-time.After(time.Second):
		t.Fatal("market did not suspend after processing the final epoch")
	}
	cancel()
	waitForDriverStop(t, done)

	wantEvents := []string{meshevents.EventKindMarketStarted, meshevents.EventKindAdvanceEpoch,
		meshevents.EventKindEpochProcessed, meshevents.EventKindMarketSuspended}
	var gotEvents []string
	for _, event := range tm.entries {
		gotEvents = append(gotEvents, event.Kind)
	}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("events = %v, want %v", gotEvents, wantEvents)
	}
	advance := storage.advanceEpochEvents[0]
	if advance.ClosedEpochIdx != finalEpoch || advance.OpenedEpochIdx != 0 {
		t.Fatalf("advance closed/opened = %d/%d, want %d/0",
			advance.ClosedEpochIdx, advance.OpenedEpochIdx, finalEpoch)
	}
	if mkt.lifecycleState != db.MarketStateSuspended || mkt.processedEpochIdx != finalEpoch {
		t.Fatalf("final state = %v, processed epoch = %d; want suspended, %d",
			mkt.lifecycleState, mkt.processedEpochIdx, finalEpoch)
	}
}

func TestMarketEpochDriverCompletesOverdueSuspension(t *testing.T) {
	mkt, storage, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	dur := int64(mkt.EpochDuration())
	finalEpoch := currentMarketEpochIdx(dur) - 5
	setTestMarketLifecycle(mkt, storage, seedLifecycleRow(
		db.MarketStateRunning, db.MarketPendingSuspend, finalEpoch, dur))

	ctx, cancel := context.WithCancel(context.Background())
	ready, done := startTestEpochDriver(t, mkt, ctx, cancel)
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("driver did not complete the overdue suspension during startup")
	}
	cancel()
	waitForDriverStop(t, done)

	events := mkt.mesh.(*tMesh).entries
	if len(events) != 2 || events[0].Kind != meshevents.EventKindMarketStarted ||
		events[1].Kind != meshevents.EventKindMarketSuspended {
		t.Fatal("expected only market_started followed by market_suspended")
	}
	if mkt.lifecycleState != db.MarketStateSuspended || mkt.processedEpochIdx != finalEpoch {
		t.Fatalf("state = %v, processed epoch = %d; want suspended, %d",
			mkt.lifecycleState, mkt.processedEpochIdx, finalEpoch)
	}
	if mkt.currentEpoch != nil || mkt.nextEpoch != nil {
		t.Fatalf("startup opened epoch queues: current %v, next %v", mkt.currentEpoch, mkt.nextEpoch)
	}
	if mkt.suspendEpochIdx != finalEpoch {
		t.Fatalf("final epoch = %d, want %d", mkt.suspendEpochIdx, finalEpoch)
	}
}

func TestMarketEpochDriverResumeRescheduled(t *testing.T) {
	mkt, storage, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	t.Cleanup(cleanup)

	dur := int64(mkt.EpochDuration())
	nowEpoch := currentEpochWithHeadroom(t, dur)
	firstResumeEpoch := nowEpoch + 2
	rescheduledResumeEpoch := firstResumeEpoch + 2
	persist := true
	setTestMarketLifecycle(mkt, storage, &db.MarketLifecycle{
		Market:          mkt.name,
		State:           db.MarketStateSuspended,
		StartEpochIdx:   firstResumeEpoch,
		StartEpochDur:   dur,
		PendingAction:   db.MarketPendingResume,
		PendingEpochIdx: firstResumeEpoch,
		PendingEpochDur: dur,
		PersistBook:     &persist,
		RunParams:       mkt.configuredParams.MarketRunParams,
	})

	resumeEvents := observeDriverEvents(mkt.mesh.(*tMesh), meshevents.EventKindMarketResumed)
	ctx, cancel := context.WithCancel(context.Background())
	startupDone, runDone := startTestEpochDriver(t, mkt, ctx, cancel)

	select {
	case err := <-startupDone:
		if err != nil {
			t.Fatalf("startup error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for suspended startup")
	}

	reschedule := &meshevents.MarketResumeScheduledEvent{Market: mkt.name, StartEpochIdx: rescheduledResumeEpoch, EpochDur: dur}
	rescheduleEvent, err := mesh.NewEvent(reschedule)
	if err != nil {
		t.Fatalf("build schedule_resume event: %v", err)
	}
	if _, err := mkt.mesh.ApplyEvent(context.Background(), rescheduleEvent); err != nil {
		t.Fatalf("apply schedule_resume event: %v", err)
	}

	// The first resume must wait for the new schedule, even after the old time passes.
	resumeTime := time.UnixMilli(rescheduledResumeEpoch * dur)
	resumeDeadline := resumeTime.Add(time.Duration(dur/2) * time.Millisecond)
	select {
	case observed := <-resumeEvents:
		resumed, err := meshevents.DecodeMarketResumedEvent(observed.event.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if observed.submittedAt.Before(resumeTime) || observed.submittedAt.After(resumeDeadline) {
			t.Fatalf("market resumed at %v, want between %v and %v",
				observed.submittedAt, resumeTime, resumeDeadline)
		}
		if resumed.StartEpochIdx != rescheduledResumeEpoch || resumed.EpochDur != dur {
			t.Fatalf("resume epoch = %d:%d, want %d:%d", resumed.StartEpochIdx,
				resumed.EpochDur, rescheduledResumeEpoch, dur)
		}
	case <-time.After(time.Until(resumeDeadline)):
		t.Fatal("market did not resume at the new scheduled time")
	}
	cancel()
	waitForDriverStop(t, runDone)
	if len(storage.marketResumedUpdates) != 1 {
		t.Fatalf("resume applications = %d, want 1", len(storage.marketResumedUpdates))
	}
}

func TestMarketEpochDriverWaitsForChainSync(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	t.Cleanup(cleanup)

	// Even existing epoch queues must wait for chain synchronization.
	dur := int64(mkt.EpochDuration())
	mkt.startEpochIdx = 1
	mkt.currentEpoch = NewEpoch(10, dur)
	mkt.nextEpoch = NewEpoch(11, dur)
	swapper := &unsyncedDriverSwapper{checked: make(chan struct{}, 1)}
	mkt.swapper = swapper

	ctx, cancel := context.WithCancel(context.Background())
	startupDone, runDone := startTestEpochDriver(t, mkt, ctx, cancel)
	select {
	case <-swapper.checked:
	case <-time.After(time.Second):
		t.Fatal("driver did not check chain synchronization")
	}

	select {
	case err := <-startupDone:
		t.Fatalf("startup result while unsynced: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-runDone:
		t.Fatalf("market run returned before cancellation")
	default:
	}
	cancel()
	select {
	case err := <-startupDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("startup error after cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for canceled startup result")
	}
	waitForDriverStop(t, runDone)
	if events := mkt.mesh.(*tMesh).entries; len(events) != 0 {
		t.Fatalf("events while unsynced = %d, want 0", len(events))
	}
	status := mkt.Status()
	if status.Running || status.ActiveEpoch != 0 {
		t.Fatalf("market status after canceled startup = running %v active %d, want false/0",
			status.Running, status.ActiveEpoch)
	}
}

func TestMarketEpochDriverProcessingLimit(t *testing.T) {
	const processedEpoch = int64(10)
	lastAllowedClose := processedEpoch + db.MaxUnprocessedClosedEpochs

	t.Run("within limit", func(t *testing.T) {
		driver := &marketEpochDriver{m: &Market{processedEpochIdx: processedEpoch}}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := driver.waitForEpochProcessing(ctx, lastAllowedClose); err != nil {
			t.Fatalf("closing at the limit: %v", err)
		}
	})

	t.Run("waits for processing", func(t *testing.T) {
		mkt := &Market{processedEpochIdx: processedEpoch, closureWake: make(chan struct{}, 1)}
		driver := &marketEpochDriver{m: mkt}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			defer close(done)
			done <- driver.waitForEpochProcessing(ctx, lastAllowedClose+1)
		}()
		defer func() {
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("processing waiter did not stop")
			}
		}()
		select {
		case err := <-done:
			t.Fatalf("advanced before processing caught up: %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		mkt.epochMtx.Lock()
		mkt.processedEpochIdx++
		mkt.epochMtx.Unlock()
		mkt.wakeClosureWaiter()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("processing caught up: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("still waiting after processing caught up")
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		driver := &marketEpochDriver{m: &Market{processedEpochIdx: processedEpoch}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			defer close(done)
			done <- driver.waitForEpochProcessing(ctx, lastAllowedClose+1)
		}()
		defer func() {
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("processing waiter did not stop")
			}
		}()
		select {
		case err := <-done:
			t.Fatalf("wait returned before cancellation: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("wait error = %v, want context.Canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("wait ignored cancellation")
		}
	})
}

func TestMarketEpochDriverStopsOnAdvanceError(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	failed := false
	mkt.mesh.(*tMesh).events[meshevents.EventKindAdvanceEpoch] = func(*mesh.EventApplyContext, *mesh.Event) (*db.EventLogEntry, error) {
		failed = true
		return nil, errors.New("advance failed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready, done := startTestEpochDriver(t, mkt, ctx, cancel)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("driver did not stop after advancement failed")
	}
	if !failed || ctx.Err() != nil {
		t.Fatal("driver must stop on advancement failure without canceling its parent")
	}
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("startup: %v", err)
		}
	default:
		t.Fatal("driver did not report startup")
	}
	if len(ready) != 0 {
		t.Fatal("driver reported startup more than once")
	}
	status := mkt.Status()
	if status.Running || status.ActiveEpoch != 0 {
		t.Fatalf("market not stopped: running=%v, active epoch=%d", status.Running, status.ActiveEpoch)
	}
}

func TestMarketEpochDriverDrainsOnProcessingError(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	processed := 0
	mkt.mesh.(*tMesh).events[meshevents.EventKindEpochProcessed] = func(*mesh.EventApplyContext, *mesh.Event) (*db.EventLogEntry, error) {
		processed++
		return nil, errors.New("processing failed")
	}
	driver := &marketEpochDriver{m: mkt, epochPump: newEpochPump()}
	// Queue more completed epochs than fit in the pump's output buffer.
	for i := int64(1); i <= 4; i++ {
		epoch := driver.epochPump.Insert(NewEpoch(i, int64(mkt.EpochDuration())))
		epoch.complete(nil, nil, nil, time.Time{})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pumpDone, processingDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(pumpDone)
		driver.epochPump.Run(ctx)
	}()
	go func() {
		defer close(processingDone)
		driver.runEpochProcessing(ctx, cancel)
	}()

	for _, worker := range []struct {
		name string
		done <-chan struct{}
	}{{"pump", pumpDone}, {"processing", processingDone}} {
		select {
		case <-worker.done:
		case <-time.After(time.Second):
			t.Fatalf("%s did not stop after processing failed", worker.name)
		}
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("processing failure did not cancel the workers")
	}
	if processed != 1 {
		t.Fatalf("processed %d epochs, want only the failed epoch", processed)
	}
	if len(driver.epochPump.ready) != 0 {
		t.Fatal("pump output was not drained")
	}
}

// startTestEpochDriver leaves readiness checks to the test and joins the driver
// before the market fixture is cleaned up.
func startTestEpochDriver(t *testing.T, mkt *Market, ctx context.Context, cancel context.CancelFunc) (<-chan error, <-chan struct{}) {
	t.Helper()
	ready := make(chan error, 2) // Leave room to detect a duplicate startup result.
	done := make(chan struct{})
	go func() {
		defer close(done)
		newMarketEpochDriver(mkt).run(ctx, ready)
	}()
	t.Cleanup(func() {
		cancel()
		waitForDriverStop(t, done)
	})
	return ready, done
}

func waitForDriverStop(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("epoch driver did not stop")
	}
}

type observedDriverEvent struct {
	event       *mesh.Event
	submittedAt time.Time
}

// observeDriverEvents reports successful applications with their submission times.
func observeDriverEvents(tm *tMesh, kind string) <-chan observedDriverEvent {
	events := make(chan observedDriverEvent, 1)
	apply := tm.events[kind]
	tm.events[kind] = func(ctx *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
		submittedAt := time.Now()
		entry, err := apply(ctx, event)
		if err == nil {
			select {
			case events <- observedDriverEvent{event, submittedAt}:
			case <-ctx.Done():
			}
		}
		return entry, err
	}
	return events
}

type unsyncedDriverSwapper struct {
	epochProcessedTestSwapper
	checked chan struct{}
}

func (s *unsyncedDriverSwapper) ChainsSynced(uint32, uint32) (bool, error) {
	select {
	case s.checked <- struct{}{}:
	default:
	}
	return false, nil
}

func currentEpochWithHeadroom(t *testing.T, dur int64) int64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Duration(dur) * time.Millisecond)
	for time.Now().Before(deadline) {
		nowMS := time.Now().UnixMilli()
		if nowMS%dur < dur/4 {
			return nowMS / dur
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for epoch headroom")
	return 0
}

func waitForOrderAdmission(t *testing.T, mkt *Market) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if mkt.Running() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for order admission")
}

func TestMarket_Run(t *testing.T) {
	// This test exercises the Market's main loop, which cycles the epochs and
	// queues (or not) incoming orders.

	// Create the market.
	mkt, storage, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		cleanup()
		return
	}
	epochDurationMSec := int64(mkt.EpochDuration())
	// This test wants to know when epoch order matching booking is done.
	storage.epochInserted = make(chan struct{}, 1)
	// and when handlePreimage is done.
	auth.handlePreimageDone = make(chan struct{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check that start is delayed by an unsynced backend. Tell the Market to
	// start
	atomic.StoreUint32(&oRig.dcr.synced, 0)
	nowEpochIdx := time.Now().UnixMilli()/epochDurationMSec + 1

	unsyncedEpochIdx := nowEpochIdx + 1
	unsyncedEpochTime := time.UnixMilli(unsyncedEpochIdx * epochDurationMSec)

	startEpochIdx := unsyncedEpochIdx + 1
	startEpochTime := time.UnixMilli(startEpochIdx * epochDurationMSec)

	var wg sync.WaitGroup
	wg.Add(1)
	mkt.startEpochIdx = unsyncedEpochIdx
	go func() {
		defer wg.Done()
		mkt.Run(ctx, nil)
	}()

	// Make an order for the first epoch.
	clientTimeMSec := startEpochIdx*epochDurationMSec + 10 // 10 ms after epoch start
	lots := 1
	qty := uint64(dcrLotSize * lots)
	rate := uint64(1000) * dcrRateStep
	aid := test.NextAccount()
	pi := test.RandomPreimage()
	commit := pi.Commit()
	limit := &msgjson.LimitOrder{
		Prefix: msgjson.Prefix{
			AccountID:  aid[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.LimitOrderNum,
			ClientTime: uint64(clientTimeMSec),
			Commit:     commit[:],
		},
		Trade: msgjson.Trade{
			Side:     msgjson.SellOrderNum,
			Quantity: qty,
			Coins:    []*msgjson.Coin{},
			Address:  btcAddr,
		},
		Rate: rate,
		TiF:  msgjson.StandingOrderNum,
	}

	newLimit := func() *order.LimitOrder {
		return &order.LimitOrder{
			P: order.Prefix{
				AccountID:  aid,
				BaseAsset:  limit.Base,
				QuoteAsset: limit.Quote,
				OrderType:  order.LimitOrderType,
				ClientTime: time.UnixMilli(clientTimeMSec),
				Commit:     commit,
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     true,
				Quantity: limit.Quantity,
				Address:  limit.Address,
			},
			Rate:  limit.Rate,
			Force: order.StandingTiF,
		}
	}

	parcelQty := uint64(dcrLotSize)
	maxMakerQty := parcelQty * uint64(parcelLimit)
	maxTakerQty := maxMakerQty / 2

	var msgID uint64
	nextMsgID := func() uint64 { msgID++; return msgID }
	newOR := func() *orderRecord {
		return &orderRecord{
			msgID: nextMsgID(),
			req:   limit,
			order: newLimit(),
		}
	}

	storMsgPI := func(id uint64, pi order.Preimage) {
		auth.piMtx.Lock()
		auth.preimagesByMsgID[id] = pi
		auth.piMtx.Unlock()
	}

	oRecord := newOR()
	storMsgPI(oRecord.msgID, pi)
	//auth.Send will update preimagesByOrderID

	// Submit order before market starts running
	rpcErr := submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr == nil || rpcErr.Code != msgjson.MarketNotRunningError {
		t.Fatalf(`expected ErrMarketNotRunning ("%v"), got "%v"`, ErrMarketNotRunning, rpcErr)
	}

	mktStatus := mkt.Status()
	if mktStatus.Running {
		t.Fatalf("Market should not be running yet")
	}

	halfEpoch := time.Duration(epochDurationMSec/2) * time.Millisecond

	<-time.After(time.Until(unsyncedEpochTime.Add(halfEpoch)))

	if mkt.Running() {
		t.Errorf("market running with an unsynced backend")
	}

	atomic.StoreUint32(&oRig.dcr.synced, 1)

	<-time.After(time.Until(startEpochTime.Add(halfEpoch)))
	<-storage.epochInserted

	if !mkt.Running() {
		t.Errorf("market not running after backend sync finished")
	}

	// Submit again.
	limit.Quantity = dcrLotSize

	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)...
	<-auth.handlePreimageDone
	<-storage.epochInserted

	// The parcel-limit checks require the first order to be booked.
	oid := oRecord.order.ID()
	deadline := time.Now().Add(5 * time.Second)
	for !mkt.book.HaveOrder(oid) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the first order to be booked")
		}
		time.Sleep(time.Millisecond)
	}

	// Submit an immediate taker sell (taker) over user taker limit

	piSell := test.RandomPreimage()
	commitSell := piSell.Commit()
	oRecordSell := newOR()
	limit.Commit = commitSell[:]
	loSell := oRecordSell.order.(*order.LimitOrder)
	loSell.P.Commit = commitSell
	loSell.Force = order.ImmediateTiF // likely taker
	loSell.Quantity = maxTakerQty     // one lot already booked

	storMsgPI(oRecordSell.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecordSell)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderQuantityTooHigh {
		t.Fatalf("RPC error = %v, want code %d", rpcErr, msgjson.OrderQuantityTooHigh)
	}

	// Submit a taker buy that is over user taker limit
	// loSell := oRecord.order.(*order.LimitOrder)
	piBuy := test.RandomPreimage()
	commitBuy := piBuy.Commit()
	oRecordBuy := newOR()
	limit.Commit = commitBuy[:]
	loBuy := oRecordBuy.order.(*order.LimitOrder)
	loBuy.P.Commit = commitBuy
	loBuy.Sell = false
	loBuy.Quantity = maxTakerQty // One lot already booked
	// rate matches with the booked sell = likely taker

	storMsgPI(oRecordBuy.msgID, piBuy)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecordBuy)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderQuantityTooHigh {
		t.Fatalf("RPC error = %v, want code %d", rpcErr, msgjson.OrderQuantityTooHigh)
	}

	// Submit a likely taker with an acceptable limit
	loSell.Quantity = maxTakerQty - dcrLotSize // the limit

	storMsgPI(oRecordSell.msgID, piSell)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecordSell)
	if rpcErr != nil {
		t.Fatalf("should have allowed that likely-taker: %v", rpcErr)
	}

	// Another in the same epoch will push over the limit
	loBuy.Quantity = dcrLotSize // just one lot
	storMsgPI(oRecordBuy.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecordBuy)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderQuantityTooHigh {
		t.Fatalf("RPC error = %v, want code %d", rpcErr, msgjson.OrderQuantityTooHigh)
	}

	// Submit a valid cancel order.
	loID := oRecord.order.ID()
	piCo := test.RandomPreimage()
	commit = piCo.Commit()
	cancelTime := time.Now().UnixMilli()
	cancelMsg := &msgjson.CancelOrder{
		Prefix: msgjson.Prefix{
			AccountID:  aid[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.CancelOrderNum,
			ClientTime: uint64(cancelTime),
			Commit:     commit[:],
		},
		TargetID: loID[:],
	}

	newCancel := func() *order.CancelOrder {
		return &order.CancelOrder{
			P: order.Prefix{
				AccountID:  aid,
				BaseAsset:  limit.Base,
				QuoteAsset: limit.Quote,
				OrderType:  order.CancelOrderType,
				ClientTime: time.UnixMilli(cancelTime),
				Commit:     commit,
			},
			TargetOrderID: loID,
		}
	}
	co := newCancel()

	coRecord := orderRecord{
		msgID: nextMsgID(),
		req:   cancelMsg,
		order: co,
	}

	// Cancel order w/o permission to cancel target order (the limit order from
	// above that is now booked)
	cancelTime++
	otherAccount := test.NextAccount()
	cancelMsg.ClientTime = uint64(cancelTime)
	cancelMsg.AccountID = otherAccount[:]
	coWrongAccount := newCancel()
	piBadCo := test.RandomPreimage()
	commitBadCo := piBadCo.Commit()
	coWrongAccount.Commit = commitBadCo
	coWrongAccount.AccountID = otherAccount
	coWrongAccount.ClientTime = time.UnixMilli(cancelTime)
	cancelMsg.Commit = commitBadCo[:]
	coRecordWrongAccount := orderRecord{
		msgID: nextMsgID(),
		req:   cancelMsg,
		order: coWrongAccount,
	}

	// Submit the invalid cancel order first because it would be caught by the
	// duplicate check if we do it after the valid one is submitted.
	storMsgPI(coRecordWrongAccount.msgID, piBadCo)
	rpcErr = submitOrderCommand(t, mkt, auth, &coRecordWrongAccount)
	if rpcErr == nil || rpcErr.Code != msgjson.UnknownMarketError || rpcErr.Message != ErrCancelNotPermitted.Error() {
		t.Errorf(`expected ErrCancelNotPermitted ("%v"), got "%v"`, ErrCancelNotPermitted, rpcErr)
	}

	// Valid cancel order
	storMsgPI(coRecord.msgID, piCo)
	rpcErr = submitOrderCommand(t, mkt, auth, &coRecord)
	if rpcErr != nil {
		t.Fatalf("Failed to submit order: %v", rpcErr)
	}

	// Duplicate cancel order
	piCoDup := test.RandomPreimage()
	commit = piCoDup.Commit()
	cancelTime++
	cancelMsg.ClientTime = uint64(cancelTime)
	cancelMsg.Commit = commit[:]
	coDup := newCancel()
	coDup.Commit = commit
	coDup.ClientTime = time.UnixMilli(cancelTime)
	coRecordDup := orderRecord{
		msgID: nextMsgID(),
		req:   cancelMsg,
		order: coDup,
	}
	storMsgPI(coRecordDup.msgID, piCoDup)
	rpcErr = submitOrderCommand(t, mkt, auth, &coRecordDup)
	if rpcErr == nil || rpcErr.Code != msgjson.UnknownMarketError || rpcErr.Message != ErrDuplicateCancelOrder.Error() {
		t.Errorf(`expected ErrDuplicateCancelOrder ("%v"), got "%v"`, ErrDuplicateCancelOrder, rpcErr)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)..
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted

	cancel()
	wg.Wait()
	cleanup()

	// Test duplicate order (commitment) with a new Market.
	mkt, storage, auth, cleanup, err = newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	storage.epochInserted = make(chan struct{}, 1)
	auth.handlePreimageDone = make(chan struct{}, 1)

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Run(ctx, nil)
	}()
	waitForOrderAdmission(t, mkt)

	// fresh oRecord
	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr != nil {
		t.Fatalf("first order: %v", rpcErr)
	}

	oRecord = newOR()
	oRecord.order.(*order.LimitOrder).Quantity *= 2
	storMsgPI(oRecord.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderParameterError || rpcErr.Message != ErrInvalidCommitment.Error() {
		t.Errorf(`expected ErrInvalidCommitment ("%v"), got "%v"`, ErrInvalidCommitment, rpcErr)
	}

	// Send an order with a bad lot size.
	oRecord = newOR()
	oRecord.order.(*order.LimitOrder).Quantity += mkt.configuredParams.LotSize / 2
	storMsgPI(oRecord.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderParameterError || rpcErr.Message != ErrInvalidOrder.Error() {
		t.Errorf(`expected ErrInvalidOrder ("%v"), got "%v"`, ErrInvalidOrder, rpcErr)
	}

	// Rate too low. The live floor is the run's adopted parameter set, so
	// pin a floor above the order's rate there.
	oRecord = newOR()
	setTestRunMinimumRate(t, mkt, oRecord.order.(*order.LimitOrder).Rate+1)
	storMsgPI(oRecord.msgID, pi)
	if rpcErr = submitOrderCommand(t, mkt, auth, oRecord); rpcErr == nil ||
		rpcErr.Code != msgjson.OrderParameterError || rpcErr.Message != ErrInvalidRate.Error() {
		t.Errorf("RPC error = %v, want invalid-rate error", rpcErr)
	}
	setTestRunMinimumRate(t, mkt, 0)

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)..
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted

	// Submit an order with a zero commit.
	oRecord = newOR()
	oRecord.order.(*order.LimitOrder).Commit = order.Commitment{}
	storMsgPI(oRecord.msgID, pi)
	rpcErr = submitOrderCommand(t, mkt, auth, oRecord)
	if rpcErr == nil || rpcErr.Code != msgjson.OrderParameterError || rpcErr.Message != ErrInvalidCommitment.Error() {
		t.Errorf(`expected ErrInvalidCommitment ("%v"), got "%v"`, ErrInvalidCommitment, rpcErr)
	}

	// Submit an order that breaks storage somehow.
	// tweak the order's commitment+preimage so it's not a dup.
	oRecord = newOR()
	pi = test.RandomPreimage()
	commit = pi.Commit()
	lo := oRecord.order.(*order.LimitOrder)
	lo.Commit = commit
	limit.Commit = commit[:] // oRecord.req
	storMsgPI(oRecord.msgID, pi)
	storage.failOnEpochOrder(lo) // force storage to fail on this order
	if rpcErr = submitOrderCommand(t, mkt, auth, oRecord); rpcErr == nil || rpcErr.Code != msgjson.RPCInternalError {
		t.Errorf("RPC error = %v, want code %d", rpcErr, msgjson.RPCInternalError)
	}

	cancel()
	wg.Wait()
	cleanup()
}

func TestMarket_enqueueEpoch(t *testing.T) {
	trade, tradePI := makeLORevealed(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	cancelOrder, cancelPI := makeCORevealed(buyer3, randomOrderID())
	missed, _ := makeLORevealed(seller3, mkRate3(1.0, 1.2), 1, order.ImmediateTiF)

	for _, tt := range []struct {
		name     string
		revealed []*matcher.OrderRevealed
		missed   []order.Order
	}{
		{
			name: "all preimages revealed",
			revealed: []*matcher.OrderRevealed{
				{Order: trade, Preimage: tradePI},
				{Order: cancelOrder, Preimage: cancelPI},
			},
		},
		{
			name:     "missing preimage",
			revealed: []*matcher.OrderRevealed{{Order: cancelOrder, Preimage: cancelPI}},
			missed:   []order.Order{missed},
		},
		{name: "empty epoch"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mkt, storage, auth, cleanup, err := newTestMarket()
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()

			const epochIdx int64 = 1234
			epochDur := int64(mkt.EpochDuration())
			epoch := NewEpoch(epochIdx, epochDur)
			for _, revealed := range tt.revealed {
				revealed.Order.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
				epoch.Insert(revealed.Order)
				auth.preimagesByOrdID[revealed.Order.UID()] = revealed.Preimage
			}
			for _, ord := range tt.missed {
				ord.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
				epoch.Insert(ord)
			}
			wantCSum := matcher.CSum(epoch.OrderSlice())
			// The epoch is closed and is next in line for processing.
			storage.lifecycle = seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, epochIdx+1, epochDur)
			storage.lifecycle.ProcessedEpochIdx = epochIdx - 1

			ctx, cancel := context.WithCancel(context.Background())
			pump := newEpochPump()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				pump.Run(ctx)
			}()
			defer wg.Wait()
			defer cancel()

			if !mkt.enqueueEpoch(pump, epoch) {
				t.Fatal("enqueueEpoch returned false")
			}
			var ready *readyEpoch
			select {
			case ready = <-pump.ready:
				if ready == nil {
					t.Fatal("epoch pump closed before emitting the epoch")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("epoch pump blocked waiting for preimage collection")
			}
			select {
			case <-ready.ready:
			default:
				t.Fatal("ready epoch channel was not closed")
			}
			if ready.missRevokeTime.IsZero() {
				t.Fatal("ready epoch has no miss revoke time")
			}
			meshSvc := mkt.mesh.(*tMesh)
			if len(meshSvc.entries) != 0 {
				t.Fatal("preimage collection published a mesh event")
			}

			if err := mkt.processReadyEpoch(ctx, ready); err != nil {
				t.Fatalf("processReadyEpoch: %v", err)
			}
			if len(meshSvc.entries) != 1 || meshSvc.entries[0].Kind != meshevents.EventKindEpochProcessed {
				t.Fatalf("published events = %v, want one epoch_processed event", meshSvc.entries)
			}
			processed, err := meshevents.DecodeEpochProcessedEvent(meshSvc.entries[0].Payload)
			if err != nil {
				t.Fatal(err)
			}
			if processed.Market != mkt.name || processed.EpochIdx != epochIdx || processed.EpochDur != epochDur {
				t.Fatalf("processed market/epoch = %s/%d:%d, want %s/%d:%d",
					processed.Market, processed.EpochIdx, processed.EpochDur, mkt.name, epochIdx, epochDur)
			}
			if !bytes.Equal(processed.CSum, wantCSum) {
				t.Fatalf("processed checksum = %x, want %x", processed.CSum, wantCSum)
			}
			if processed.MissRevokeTime != ready.missRevokeTime.UnixMilli() {
				t.Fatal("event did not preserve the collected miss revocation time")
			}
			revealed, err := processed.OrdersRevealed()
			if err != nil {
				t.Fatal(err)
			}
			if len(revealed) != len(tt.revealed) {
				t.Fatalf("revealed orders = %d, want %d", len(revealed), len(tt.revealed))
			}
			preimages := make(map[order.OrderID]order.Preimage, len(revealed))
			for _, reveal := range revealed {
				preimages[reveal.Order.ID()] = reveal.Preimage
			}
			for _, want := range tt.revealed {
				if pi, found := preimages[want.Order.ID()]; !found || pi != want.Preimage {
					t.Fatalf("incorrect or missing preimage for %v", want.Order.ID())
				}
			}
			missed, err := processed.MissedOrders()
			if err != nil {
				t.Fatal(err)
			}
			if len(missed) != len(tt.missed) {
				t.Fatalf("missed orders = %d, want %d", len(missed), len(tt.missed))
			}
			for i, want := range tt.missed {
				if missed[i].ID() != want.ID() {
					t.Fatalf("missed order %d = %v, want %v", i, missed[i].ID(), want.ID())
				}
			}
		})
	}
}

func TestMarket_handlePreimageResp(t *testing.T) {
	randomCommit := func() (com order.Commitment) {
		rnd.Read(com[:])
		return
	}

	newOrder := func() (*order.LimitOrder, order.Preimage) {
		qty := uint64(dcrLotSize * 10)
		rate := uint64(1000) * dcrRateStep
		return makeLORevealed(seller3, rate, qty, order.StandingTiF)
	}

	authMgr := &TAuth{}
	mkt := &Market{
		auth:    authMgr,
		storage: &TArchivist{},
	}

	piMsg := &msgjson.PreimageResponse{
		Preimage: msgjson.Bytes{},
	}
	msg, _ := msgjson.NewResponse(5, piMsg, nil)

	runAndReceive := func(msg *msgjson.Message, dat *piData) *order.Preimage {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			mkt.handlePreimageResp(msg, dat)
			wg.Done()
		}()
		piRes := <-dat.preimage
		wg.Wait()
		return piRes
	}

	// 1. bad Message.Type: RPCParseError
	msg.Type = msgjson.Request // should be Response
	lo, pi := newOrder()
	dat := &piData{lo, make(chan *order.Preimage)}
	piRes := runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	// Inspect the servers rpc error response message.
	respMsg := authMgr.getSend()
	if respMsg == nil {
		t.Fatalf("no error response")
	}
	resp, _ := respMsg.Response()
	msgErr := resp.Error
	// Code 1, Message about parsing response and invalid type (1 is not response)
	if msgErr.Code != msgjson.RPCParseError {
		t.Errorf("Expected error code %d, got %d", msgjson.RPCParseError, msgErr.Code)
	}
	wantMsgPrefix := "error parsing preimage notification response"
	if !strings.Contains(msgErr.Message, wantMsgPrefix) {
		t.Errorf("Expected error message %q, got %q", wantMsgPrefix, msgErr.Message)
	}

	// 2. empty preimage from client: InvalidPreimage
	msg, _ = msgjson.NewResponse(5, piMsg, nil)
	//lo, pi := newOrder()
	dat = &piData{lo, make(chan *order.Preimage)}
	piRes = runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	respMsg = authMgr.getSend()
	if respMsg == nil {
		t.Fatalf("no error response")
	}
	resp, _ = respMsg.Response()
	msgErr = resp.Error
	// 30 invalid preimage length (0 byes)
	if msgErr.Code != msgjson.InvalidPreimage {
		t.Errorf("Expected error code %d, got %d", msgjson.InvalidPreimage, msgErr.Code)
	}
	if !strings.Contains(msgErr.Message, "invalid preimage length") {
		t.Errorf("Expected error message %q, got %q",
			"invalid preimage length (0 bytes)",
			msgErr.Message)
	}

	// 3. correct preimage length, commitment mismatch
	//lo, pi := newOrder()
	lo.Commit = randomCommit() // break the commitment
	dat = &piData{
		ord:      lo,
		preimage: make(chan *order.Preimage),
	}
	piMsg = &msgjson.PreimageResponse{
		Preimage: pi[:],
	}

	msg, _ = msgjson.NewResponse(5, piMsg, nil)
	piRes = runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	respMsg = authMgr.getSend()
	if respMsg == nil {
		t.Fatalf("no error response")
	}
	resp, _ = respMsg.Response()
	msgErr = resp.Error
	// 30 invalid preimage length (0 byes)
	if msgErr.Code != msgjson.PreimageCommitmentMismatch {
		t.Errorf("Expected error code %d, got %d",
			msgjson.PreimageCommitmentMismatch, msgErr.Code)
	}
	if !strings.Contains(msgErr.Message, "does not match order commitment") {
		t.Errorf("Expected error message of the form %q, got %q",
			"preimage hash {hash} does not match order commitment {commit}",
			msgErr.Message)
	}

	// 4. correct preimage and commit
	lo.Commit = pi.Commit() // fix the commitment
	dat = &piData{
		ord:      lo,
		preimage: make(chan *order.Preimage),
	}
	piMsg = &msgjson.PreimageResponse{
		Preimage: pi[:],
	}

	piRes = runAndReceive(msg, dat)
	if piRes == nil {
		t.Errorf("Expected preimage %x, got <nil>", pi)
	} else if *piRes != pi {
		t.Errorf("Expected preimage %x, got %x", pi, *piRes)
	}

	// no response this time (no error)
	respMsg = authMgr.getSend()
	if respMsg != nil {
		t.Fatalf("got error response: %d %q", respMsg.Type, string(respMsg.Payload))
	}

	// 5. client classified server request as invalid: InvalidRequestError
	msg, _ = msgjson.NewResponse(5, nil, msgjson.NewError(msgjson.InvalidRequestError, "invalid request"))
	lo, pi = newOrder()
	dat = &piData{lo, make(chan *order.Preimage)}
	piRes = runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	// Inspect the servers rpc error response message.
	respMsg = authMgr.getSend()
	if respMsg != nil {
		t.Fatalf("server is not expected to respond with anything")
	}

	// 6. payload is not msgjson.PreimageResponse, unmarshal still succeeds, but PI is nil
	notaPiMsg := new(msgjson.OrderBookSubscription)
	msg, _ = msgjson.NewResponse(5, notaPiMsg, nil)
	dat = &piData{lo, make(chan *order.Preimage)}
	piRes = runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	respMsg = authMgr.getSend()
	if respMsg == nil {
		t.Fatalf("no error response")
	}
	resp, _ = respMsg.Response()
	msgErr = resp.Error
	// 30 invalid preimage length (0 byes)
	if msgErr.Code != msgjson.InvalidPreimage {
		t.Errorf("Expected error code %d, got %d", msgjson.InvalidPreimage, msgErr.Code)
	}
	if !strings.Contains(msgErr.Message, "invalid preimage length") {
		t.Errorf("Expected error message %q, got %q",
			"invalid preimage length (0 bytes)",
			msgErr.Message)
	}

	// 7. payload unmarshal error
	msg, _ = msgjson.NewResponse(5, piMsg, nil)
	msg.Payload = json.RawMessage(`{"result":1}`) // ResponsePayload with invalid Result
	dat = &piData{lo, make(chan *order.Preimage)}
	piRes = runAndReceive(msg, dat)
	if piRes != nil {
		t.Errorf("Expected <nil> preimage, got %v", piRes)
	}

	respMsg = authMgr.getSend()
	if respMsg == nil {
		t.Fatalf("no error response")
	}
	resp, _ = respMsg.Response()
	msgErr = resp.Error
	// Code 1, Message about parsing response payload and invalid type (1 is not response)
	if msgErr.Code != msgjson.RPCParseError {
		t.Errorf("Expected error code %d, got %d", msgjson.RPCParseError, msgErr.Code)
	}
	// wrapped json.UnmarshalFieldError
	wantMsgPrefix = "error parsing preimage response payload result"
	if !strings.Contains(msgErr.Message, wantMsgPrefix) {
		t.Errorf("Expected error message %q, got %q", wantMsgPrefix, msgErr.Message)
	}
}

func TestMarket_MarketStartup_AccountBased(t *testing.T) {
	t.Run("account-based base", func(t *testing.T) { testAccountAssets(t, true, false) })
	t.Run("account-based quote", func(t *testing.T) { testAccountAssets(t, false, true) })
	t.Run("both account-based", func(t *testing.T) { testAccountAssets(t, true, true) })
}

func testAccountAssets(t *testing.T, base, quote bool) {
	t.Helper()
	storage := &TArchivist{}
	balancer := newTBalancer()
	var ords []*order.LimitOrder
	var partialSell *order.LimitOrder

	baseAsset, quoteAsset := assetDCR, assetBTC
	if base {
		baseAsset = assetETH
	}
	if quote {
		quoteAsset = assetMATIC
	}

	for _, spec := range []struct {
		sell    bool
		partial bool
	}{
		{sell: true},
		{sell: false},
		{sell: true, partial: true},
		{sell: false, partial: true},
	} {
		writer := test.RandomWriter()
		writer.Market = &test.Market{
			Base:    baseAsset.ID,
			Quote:   quoteAsset.ID,
			LotSize: dcrLotSize,
		}
		writer.Sell = spec.sell
		ord := makeLO(writer, mkRate3(0.8, 1.0), 2, order.StandingTiF)
		if (ord.Sell && base) || (!ord.Sell && quote) { // Account-funded orders use an account address as their coin.
			ord.Coins = []order.CoinID{[]byte(test.RandomAddress())}
		}
		if spec.partial {
			ord.FillAmt = dcrLotSize
			if spec.sell {
				partialSell = ord
			}
		}
		ords = append(ords, ord)
		storage.BookOrder(ord)
	}

	mkt, storage, _, cleanup, err := newTestMarket(storage, balancer, [2]*asset.BackedAsset{baseAsset, quoteAsset})
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	for _, lo := range ords {
		if base && balancer.reqs[lo.BaseAccount()] != 0 {
			t.Fatalf("constructor requested base balance for order")
		}
		if quote && balancer.reqs[lo.QuoteAccount()] != 0 {
			t.Fatalf("constructor requested quote balance for order")
		}
	}

	if _, err := mkt.submitMarketStarted(context.Background()); err != nil {
		t.Fatalf("submitMarketStarted error: %v", err)
	}
	if len(storage.marketStartedUpdates) != 1 {
		t.Fatalf("market started update count = %d, want 1", len(storage.marketStartedUpdates))
	}
	if len(storage.marketStartedUpdates[0].BookedRevokes) != 0 {
		t.Fatalf("startup cleanup unexpectedly revoked %d orders", len(storage.marketStartedUpdates[0].BookedRevokes))
	}
	for _, lo := range ords {
		if base && balancer.reqs[lo.BaseAccount()] == 0 {
			t.Fatalf("base balance not requested for order")
		}
		if quote && balancer.reqs[lo.QuoteAccount()] == 0 {
			t.Fatalf("quote balance not requested for order")
		}
	}

	// A partially filled order still needs an account balance check.
	failedAddr, failedAsset := partialSell.QuoteAccount(), quoteAsset.ID
	if base {
		failedAddr, failedAsset = partialSell.BaseAccount(), baseAsset.ID
	}
	balancer.checkReserved = func(addr string, assetID uint32) bool {
		return addr != failedAddr || assetID != failedAsset
	}
	if _, err := mkt.submitMarketStarted(context.Background()); err != nil {
		t.Fatalf("submitMarketStarted with low balance: %v", err)
	}
	if len(storage.marketStartedUpdates) != 2 {
		t.Fatalf("market started updates = %d, want 2", len(storage.marketStartedUpdates))
	}
	revokes := storage.marketStartedUpdates[1].BookedRevokes
	if len(revokes) != 1 || revokes[0].Order.ID() != partialSell.ID() ||
		revokes[0].Reason != meshevents.StartupOrderRevokeReasonAccountLowBalance {
		t.Fatalf("expected the partially filled order to be revoked for low balance, got %v", revokes)
	}
}

func TestMarket_AccountPending(t *testing.T) {
	storage := &TArchivist{}
	writer := test.RandomWriter()
	writer.Market = &test.Market{
		Base:    assetETH.ID,
		Quote:   assetMATIC.ID,
		LotSize: dcrLotSize,
	}

	const rate = btcRateStep * 100
	const sellLots = 10
	const buyLots = 20
	ethAddr := test.RandomAddress()
	maticAddr := test.RandomAddress()

	writer.Sell = true
	lo := makeLO(writer, rate, sellLots, order.StandingTiF)
	lo.Coins = []order.CoinID{[]byte(ethAddr)}
	lo.Address = maticAddr
	storage.BookOrder(lo)

	writer.Sell = false
	lo = makeLO(writer, rate, buyLots, order.StandingTiF)
	lo.Coins = []order.CoinID{[]byte(maticAddr)}
	lo.Address = ethAddr
	storage.BookOrder(lo)

	mkt, _, _, cleanup, err := newTestMarket(storage, newTBalancer(), [2]*asset.BackedAsset{assetETH, assetMATIC})
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	checkPending := func(tag string, addr string, assetID uint32, expQty, expLots uint64, expRedeems int) {
		t.Helper()
		qty, lots, redeems := mkt.AccountPending(addr, assetID)
		if qty != expQty {
			t.Fatalf("%s: wrong quantity: wanted %d, got %d", tag, expQty, qty)
		}
		if lots != expLots {
			t.Fatalf("%s: wrong lots: wanted %d, got %d", tag, expLots, lots)
		}
		if redeems != expRedeems {
			t.Fatalf("%s: wrong redeems: wanted %d, got %d", tag, expRedeems, redeems)
		}
	}

	checkPending("booked-only-eth", ethAddr, assetETH.ID, sellLots*dcrLotSize, sellLots, buyLots)

	quoteQty := calc.BaseToQuote(rate, buyLots*dcrLotSize)
	checkPending("booked-only-matic", maticAddr, assetMATIC.ID, quoteQty, buyLots, sellLots)

	const epochSellLots = 5
	writer.Sell = true
	lo = makeLO(writer, rate, epochSellLots, order.StandingTiF)
	lo.Coins = []order.CoinID{[]byte(ethAddr)}
	lo.Address = maticAddr
	mkt.epochOrders[lo.ID()] = lo
	const totalSellLots = sellLots + epochSellLots
	checkPending("with-epoch-sell-eth", ethAddr, assetETH.ID, totalSellLots*dcrLotSize, totalSellLots, buyLots)
	checkPending("with-epoch-sell-matic", maticAddr, assetMATIC.ID, quoteQty, buyLots, totalSellLots)

	// Market buy order.
	midGap := mkt.MidGap()
	mktBuyQty := quoteQty + calc.BaseToQuote(midGap, dcrLotSize/2)
	writer.Sell = false
	mo := makeMO(writer, 0)
	mo.Quantity = mktBuyQty
	mo.Coins = []order.CoinID{[]byte(maticAddr)}
	mo.Address = ethAddr
	mkt.epochOrders[mo.ID()] = mo
	redeems := int(totalSellLots)
	totalBuyLots := buyLots + calc.QuoteToBase(midGap, mktBuyQty)/dcrLotSize
	totalQty := quoteQty + mktBuyQty
	checkPending("with-epoch-market-buy-matic", maticAddr, assetMATIC.ID, totalQty, totalBuyLots, redeems)
	checkPending("with-epoch-market-buy-eth", ethAddr, assetETH.ID, totalSellLots*dcrLotSize, totalSellLots, int(totalBuyLots))
}

func TestSubscribeMMSnapshots(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	user := test.NextAccount()

	// Subscribe.
	mkt.SubscribeMMSnapshots(user, false)
	mkt.mmSnapshotMtx.RLock()
	_, ok := mkt.mmSnapshotSubs[user]
	mkt.mmSnapshotMtx.RUnlock()
	if !ok {
		t.Fatal("user not in mmSnapshotSubs after subscribe")
	}

	// Unsubscribe.
	mkt.SubscribeMMSnapshots(user, true)
	mkt.mmSnapshotMtx.RLock()
	_, ok = mkt.mmSnapshotSubs[user]
	mkt.mmSnapshotMtx.RUnlock()
	if ok {
		t.Fatal("user still in mmSnapshotSubs after unsubscribe")
	}
}

func TestSendMMSnapshots(t *testing.T) {
	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	// No subscribers — no messages should be sent.
	epoch := &readyEpoch{
		EpochQueue: NewEpoch(100, 500),
	}
	auth.sendsMtx.Lock()
	auth.sends = auth.sends[:0]
	auth.sendsMtx.Unlock()

	mkt.sendMMSnapshots(epoch.Epoch, epoch.Duration)

	auth.sendsMtx.Lock()
	nSends := len(auth.sends)
	auth.sendsMtx.Unlock()
	if nSends != 0 {
		t.Fatalf("expected 0 sends with no subscribers, got %d", nSends)
	}

	// Subscribe two users.
	user1 := buyer3.Acct
	user2 := seller3.Acct

	mkt.SubscribeMMSnapshots(user1, false)
	mkt.SubscribeMMSnapshots(user2, false)

	// Insert buy orders for user1 and sell orders for user2.
	buyRate1 := uint64(2_500_000_000)
	buyRate2 := uint64(2_600_000_000)
	sellRate1 := uint64(2_700_000_000)
	sellRate2 := uint64(2_800_000_000)

	loBuy1 := makeLO(buyer3, buyRate1, 1, order.StandingTiF)
	loBuy1.AccountID = user1
	loBuy2 := makeLO(buyer3, buyRate2, 2, order.StandingTiF)
	loBuy2.AccountID = user1

	loSell1 := makeLO(seller3, sellRate1, 1, order.StandingTiF)
	loSell1.AccountID = user2
	loSell2 := makeLO(seller3, sellRate2, 2, order.StandingTiF)
	loSell2.AccountID = user2

	mkt.bookMtx.Lock()
	for _, lo := range []*order.LimitOrder{loBuy1, loBuy2, loSell1, loSell2} {
		if !mkt.book.Insert(lo) {
			t.Fatalf("failed to insert order into book")
		}
	}
	mkt.bookMtx.Unlock()

	auth.sendsMtx.Lock()
	auth.sends = auth.sends[:0]
	auth.sendsMtx.Unlock()

	mkt.sendMMSnapshots(epoch.Epoch, epoch.Duration)

	auth.sendsMtx.Lock()
	sends := make([]*msgjson.Message, len(auth.sends))
	copy(sends, auth.sends)
	auth.sendsMtx.Unlock()

	if len(sends) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(sends))
	}

	// Each send should be an MMEpochSnapshotRoute notification.
	for _, msg := range sends {
		if msg.Route != msgjson.MMEpochSnapshotRoute {
			t.Fatalf("expected route %s, got %s", msgjson.MMEpochSnapshotRoute, msg.Route)
		}
		var snap msgjson.MMEpochSnapshot
		if err := msg.Unmarshal(&snap); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if snap.EpochIdx != uint64(epoch.Epoch) {
			t.Fatalf("expected epochIdx %d, got %d", epoch.Epoch, snap.EpochIdx)
		}
		if snap.EpochDur != uint64(epoch.Duration) {
			t.Fatalf("expected epochDur %d, got %d", epoch.Duration, snap.EpochDur)
		}
		acct := account.AccountID{}
		copy(acct[:], snap.AccountID)
		if acct == user1 {
			// user1 has buy orders only.
			if len(snap.BuyOrders) != 2 {
				t.Fatalf("user1: expected 2 buy orders, got %d", len(snap.BuyOrders))
			}
			if len(snap.SellOrders) != 0 {
				t.Fatalf("user1: expected 0 sell orders, got %d", len(snap.SellOrders))
			}
			// Verify orders are sorted by rate ascending.
			if snap.BuyOrders[0].Rate >= snap.BuyOrders[1].Rate {
				t.Fatalf("user1: buy orders not sorted by rate ascending: %d >= %d",
					snap.BuyOrders[0].Rate, snap.BuyOrders[1].Rate)
			}
		} else if acct == user2 {
			// user2 has sell orders only.
			if len(snap.BuyOrders) != 0 {
				t.Fatalf("user2: expected 0 buy orders, got %d", len(snap.BuyOrders))
			}
			if len(snap.SellOrders) != 2 {
				t.Fatalf("user2: expected 2 sell orders, got %d", len(snap.SellOrders))
			}
			// Verify orders are sorted by rate ascending.
			if snap.SellOrders[0].Rate >= snap.SellOrders[1].Rate {
				t.Fatalf("user2: sell orders not sorted by rate ascending: %d >= %d",
					snap.SellOrders[0].Rate, snap.SellOrders[1].Rate)
			}
		} else {
			t.Fatalf("unexpected accountID: %x", snap.AccountID)
		}

		// BestBuy and BestSell should reflect the book.
		if snap.BestBuy == 0 {
			t.Fatal("BestBuy should be non-zero")
		}
		if snap.BestSell == 0 {
			t.Fatal("BestSell should be non-zero")
		}
	}

	// Subscriber with no orders should get empty order lists.
	user3 := test.NextAccount()
	mkt.SubscribeMMSnapshots(user3, false)

	auth.sendsMtx.Lock()
	auth.sends = auth.sends[:0]
	auth.sendsMtx.Unlock()

	mkt.sendMMSnapshots(epoch.Epoch, epoch.Duration)

	auth.sendsMtx.Lock()
	sends = make([]*msgjson.Message, len(auth.sends))
	copy(sends, auth.sends)
	auth.sendsMtx.Unlock()

	// 3 subscribers now.
	if len(sends) != 3 {
		t.Fatalf("expected 3 sends, got %d", len(sends))
	}
	// Find user3's snapshot.
	for _, msg := range sends {
		var snap msgjson.MMEpochSnapshot
		if err := msg.Unmarshal(&snap); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		acct := account.AccountID{}
		copy(acct[:], snap.AccountID)
		if acct == user3 {
			if len(snap.BuyOrders) != 0 {
				t.Fatalf("user3: expected 0 buy orders, got %d", len(snap.BuyOrders))
			}
			if len(snap.SellOrders) != 0 {
				t.Fatalf("user3: expected 0 sell orders, got %d", len(snap.SellOrders))
			}
		}
	}
}

func TestSendMMSnapshotsAutoUnsub(t *testing.T) {
	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	user1 := buyer3.Acct
	user2 := seller3.Acct

	mkt.SubscribeMMSnapshots(user1, false)
	mkt.SubscribeMMSnapshots(user2, false)

	// Make Send fail for user1.
	auth.sendErrForMtx.Lock()
	if auth.sendErrFor == nil {
		auth.sendErrFor = make(map[account.AccountID]error)
	}
	auth.sendErrFor[user1] = errors.New("disconnected")
	auth.sendErrForMtx.Unlock()

	epoch := &readyEpoch{
		EpochQueue: NewEpoch(100, 500),
	}
	mkt.sendMMSnapshots(epoch.Epoch, epoch.Duration)

	// user1 should have been auto-unsubscribed.
	mkt.mmSnapshotMtx.RLock()
	_, user1Found := mkt.mmSnapshotSubs[user1]
	_, user2Found := mkt.mmSnapshotSubs[user2]
	mkt.mmSnapshotMtx.RUnlock()
	if user1Found {
		t.Fatal("user1 should have been auto-unsubscribed after Send failure")
	}
	if !user2Found {
		t.Fatal("user2 should still be subscribed")
	}

	// user2 should have received a message.
	auth.sendsMtx.Lock()
	nSends := len(auth.sends)
	auth.sendsMtx.Unlock()
	if nSends != 1 {
		t.Fatalf("expected 1 send (user2 only), got %d", nSends)
	}
}

func TestSendMMSnapshotsBothSides(t *testing.T) {
	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	// Single user with orders on both sides of the book.
	user := buyer3.Acct
	mkt.SubscribeMMSnapshots(user, false)

	buyRate := uint64(2_500_000_000)
	sellRate := uint64(2_700_000_000)

	loBuy := makeLO(buyer3, buyRate, 1, order.StandingTiF)
	loBuy.AccountID = user
	loSell := makeLO(seller3, sellRate, 1, order.StandingTiF)
	loSell.AccountID = user

	mkt.bookMtx.Lock()
	if !mkt.book.Insert(loBuy) {
		t.Fatal("failed to insert buy order")
	}
	if !mkt.book.Insert(loSell) {
		t.Fatal("failed to insert sell order")
	}
	mkt.bookMtx.Unlock()

	auth.sendsMtx.Lock()
	auth.sends = auth.sends[:0]
	auth.sendsMtx.Unlock()

	epoch := &readyEpoch{
		EpochQueue: NewEpoch(100, 500),
	}
	mkt.sendMMSnapshots(epoch.Epoch, epoch.Duration)

	auth.sendsMtx.Lock()
	sends := make([]*msgjson.Message, len(auth.sends))
	copy(sends, auth.sends)
	auth.sendsMtx.Unlock()

	if len(sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(sends))
	}

	var snap msgjson.MMEpochSnapshot
	if err := sends[0].Unmarshal(&snap); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(snap.BuyOrders) != 1 {
		t.Fatalf("expected 1 buy order, got %d", len(snap.BuyOrders))
	}
	if len(snap.SellOrders) != 1 {
		t.Fatalf("expected 1 sell order, got %d", len(snap.SellOrders))
	}
	if snap.BuyOrders[0].Rate != buyRate {
		t.Fatalf("expected buy rate %d, got %d", buyRate, snap.BuyOrders[0].Rate)
	}
	if snap.SellOrders[0].Rate != sellRate {
		t.Fatalf("expected sell rate %d, got %d", sellRate, snap.SellOrders[0].Rate)
	}
}

func TestMarket_lockOrderCoins(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	coin1 := order.CoinID([]byte{0x01, 0x02, 0x03})
	coin2 := order.CoinID([]byte{0x04, 0x05, 0x06})

	// Sell order uses base asset locker.
	sellOrder := makeLO(seller3, mkRate3(0.8, 1.0), 1, order.StandingTiF)
	sellOrder.Coins = []order.CoinID{coin1}

	// Buy order uses quote asset locker.
	buyOrder := makeLO(buyer3, mkRate3(0.8, 1.0), 1, order.StandingTiF)
	buyOrder.Coins = []order.CoinID{coin2}

	// Locking fresh coins should succeed.
	if !mkt.lockOrderCoins(sellOrder) {
		t.Fatal("lockOrderCoins failed for sell order with unlocked coins")
	}
	if !mkt.lockOrderCoins(buyOrder) {
		t.Fatal("lockOrderCoins failed for buy order with unlocked coins")
	}

	// Unlock so we can reuse the coins.
	mkt.unlockOrderCoins(sellOrder)
	mkt.unlockOrderCoins(buyOrder)

	// Pre-lock coin1 in the base book locker under a different order.
	mkt.coinLockerBase.LockCoins(map[order.OrderID][]order.CoinID{
		randomOrderID(): {coin1},
	})

	// Pre-lock coin2 in the quote book locker under a different order.
	mkt.coinLockerQuote.LockCoins(map[order.OrderID][]order.CoinID{
		randomOrderID(): {coin2},
	})

	// lockOrderCoins should now fail for both since their coins are
	// already locked.
	if mkt.lockOrderCoins(sellOrder) {
		t.Fatal("lockOrderCoins should have failed for sell order with locked coins")
	}
	if mkt.lockOrderCoins(buyOrder) {
		t.Fatal("lockOrderCoins should have failed for buy order with locked coins")
	}

	// Cancel orders always succeed regardless of coin lock state.
	co := makeCO(seller3, randomOrderID())
	if !mkt.lockOrderCoins(co) {
		t.Fatal("lockOrderCoins should always succeed for cancel orders")
	}
}

func orderFromAcceptedEvent(t *testing.T, event *mesh.Event) order.Order {
	t.Helper()
	if event.Kind != meshevents.EventKindOrderAccepted {
		t.Fatalf("wrong event kind. got %q, want %q", event.Kind, meshevents.EventKindOrderAccepted)
	}
	accepted, err := meshevents.DecodeOrderAcceptedEvent(event.Payload)
	if err != nil {
		t.Fatalf("DecodeOrderAcceptedEvent error: %v", err)
	}
	ord, err := accepted.Order()
	if err != nil {
		t.Fatalf("accepted order error: %v", err)
	}
	return ord
}

func TestHandleOrderResubmission(t *testing.T) {
	for _, test := range []struct {
		name           string
		inMemory       bool
		differentOrder bool
		storedStatus   order.OrderStatus
		lookupErr      error
		wantHandled    bool
		wantCode       int
	}{
		{name: "unknown"},
		{name: "epoch memory", inMemory: true, wantHandled: true},
		{name: "stored epoch", storedStatus: order.OrderStatusEpoch, wantHandled: true},
		{name: "booked", storedStatus: order.OrderStatusBooked, wantHandled: true},
		{name: "archived", storedStatus: order.OrderStatusRevoked, wantHandled: true, wantCode: msgjson.UnknownOrderError},
		{name: "different order with same commitment", inMemory: true, differentOrder: true},
		{name: "archived match after memory mismatch", inMemory: true, differentOrder: true,
			storedStatus: order.OrderStatusRevoked, wantHandled: true, wantCode: msgjson.UnknownOrderError},
		{name: "lookup failure", lookupErr: errors.New("db down"), wantHandled: true, wantCode: msgjson.TryAgainLaterError},
	} {
		t.Run(test.name, func(t *testing.T) {
			mkt, storage, _, cleanup, err := newTestMarket()
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			stored := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
			stored.SetTime(time.Now().Truncate(time.Millisecond))
			incoming := order.LimitOrder{P: stored.P, T: *stored.T.Copy(), Rate: stored.Rate, Force: stored.Force}
			incoming.SetTime(time.Time{})
			rec := &orderRecord{order: &incoming, req: &msgjson.LimitOrder{}, msgID: 42}
			if test.inMemory {
				epochOrder := order.LimitOrder{P: stored.P, T: *stored.T.Copy(), Rate: stored.Rate, Force: stored.Force}
				if test.differentOrder {
					epochOrder.Quantity *= 2
				}
				epochID := epochOrder.ID()
				mkt.epochCommitments[stored.Commitment()] = epochID
				mkt.epochOrders[epochID] = &epochOrder
			}
			if test.storedStatus != order.OrderStatusUnknown {
				storage.commitOrders = []db.OrderWithStatus{{Order: stored, Status: test.storedStatus}}
			}
			storage.commitOrdersErr = test.lookupErr
			var response *msgjson.Message
			var executed, handled bool
			svc, err := mesh.NewService(&mesh.ServiceConfig{
				EventLogReader: emptyEventLogReader{},
				OnHalt:         func(error) {},
				Commands: map[string]mesh.CommandExecutor{
					commandKindLimit: func(cmd *mesh.CommandContext) *msgjson.Error {
						executed = true
						var rpcErr *msgjson.Error
						handled, rpcErr = mkt.HandleOrderResubmission(cmd.Context, rec, cmd.Completion)
						return rpcErr
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			msg, err := msgjson.NewRequest(rec.msgID, msgjson.LimitRoute, nil)
			if err != nil {
				t.Fatal(err)
			}
			rpcErr := svc.ExecuteCommand(context.Background(), mesh.CommandRequest{
				Kind: commandKindLimit, User: stored.User(), Msg: msg,
				Respond: func(resp *msgjson.Message) error { response = resp; return nil },
			})
			if !executed {
				t.Fatal("command executor was not called")
			}
			if handled != test.wantHandled {
				t.Errorf("handled = %v, want %v", handled, test.wantHandled)
			}
			code := 0
			if rpcErr != nil {
				code = rpcErr.Code
			}
			if code != test.wantCode {
				t.Fatalf("RPC error = %v, want code %d", rpcErr, test.wantCode)
			}
			if !test.wantHandled {
				if !incoming.ServerTime.IsZero() {
					t.Error("unrecognized order kept a candidate's server time")
				}
				if response != nil {
					t.Fatal("unrecognized order received a response")
				}
				return
			}
			if test.wantCode != 0 {
				return
			}
			if response == nil {
				t.Fatal("missing acceptance response")
			}
			var result msgjson.OrderResult
			if err := response.UnmarshalResult(&result); err != nil {
				t.Fatal(err)
			}
			id := stored.ID()
			if response.ID != rec.msgID || !bytes.Equal(result.OrderID, id[:]) || result.ServerTime != uint64(stored.Time()) {
				t.Fatalf("resubmission did not return the original order ID and server time: %+v", result)
			}
		})
	}
}

func TestAcceptOrderCommandResubmission(t *testing.T) {
	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	mkt.running.Store(true)

	accepted := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	accepted.SetTime(time.Now().Truncate(time.Millisecond))
	oid := accepted.ID()
	mkt.epochOrders[oid] = accepted
	mkt.epochCommitments[accepted.Commitment()] = oid

	incoming := order.LimitOrder{P: accepted.P, T: *accepted.T.Copy(), Rate: accepted.Rate, Force: accepted.Force}
	incoming.SetTime(time.Time{})
	rec := &orderRecord{order: &incoming, req: &msgjson.LimitOrder{}, msgID: 42}
	svc, req := prepareOrderCommand(t, mkt, auth, rec)
	if rpcErr := svc.ExecuteCommand(context.Background(), req); rpcErr != nil {
		t.Fatalf("resubmission: %v", rpcErr)
	}
	response := auth.getSend()
	if response == nil {
		t.Fatal("resubmission delivered no result")
	}
	var result msgjson.OrderResult
	if err := response.UnmarshalResult(&result); err != nil {
		t.Fatal(err)
	}
	if response.ID != rec.msgID || !bytes.Equal(result.OrderID, oid[:]) || result.ServerTime != uint64(accepted.Time()) {
		t.Fatalf("resubmission did not return the original order ID and server time: %+v", result)
	}
}

func TestAcceptOrderCommandRestampsAfterMissedEpoch(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	// AcceptOrderCommand only accepts new orders once the market is running.
	mkt.running.Store(true)

	const requestID = uint64(42)
	lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	user := lo.User()
	commit := lo.Commitment()
	rec := &orderRecord{
		order: lo,
		req: &msgjson.LimitOrder{
			Prefix: msgjson.Prefix{
				AccountID:  user[:],
				Base:       lo.Base(),
				Quote:      lo.Quote(),
				OrderType:  msgjson.LimitOrderNum,
				ClientTime: uint64(lo.ClientTime.UnixMilli()),
				Commit:     commit[:],
			},
		},
		msgID: requestID,
	}

	var events []*mesh.Event
	applied := false
	meshSvc, err := mesh.NewService(&mesh.ServiceConfig{
		EventLogReader: emptyEventLogReader{},
		OnHalt:         func(error) {},
		Commands: map[string]mesh.CommandExecutor{
			commandKindLimit: func(cmd *mesh.CommandContext) *msgjson.Error {
				return mkt.AcceptOrderCommand(cmd.Context, rec, cmd.Completion)
			},
		},
		Events: map[string]mesh.EventApplier{
			meshevents.EventKindOrderAccepted: func(_ *mesh.EventApplyContext, event *mesh.Event) (*db.EventLogEntry, error) {
				events = append(events, event)
				if len(events) == 1 {
					time.Sleep(2 * time.Millisecond)
					return nil, ErrEpochMissed
				}
				defer func() { applied = true }()
				return &db.EventLogEntry{
					Seq:     1,
					Kind:    event.Kind,
					Event:   event.Payload,
					TipHash: []byte{1},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewService error: %v", err)
	}

	msg, err := msgjson.NewRequest(requestID, msgjson.LimitRoute, nil)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	var response *msgjson.Message
	rpcErr := meshSvc.ExecuteCommand(context.Background(), mesh.CommandRequest{
		Kind: commandKindLimit,
		User: user,
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			if !applied {
				t.Error("response delivered before successful event apply returned")
			}
			if response != nil {
				t.Fatalf("multiple command responses")
			}
			response = resp
			return nil
		},
	})
	if rpcErr != nil {
		t.Fatalf("ExecuteCommand error: %v", rpcErr)
	}
	if response == nil {
		t.Fatalf("missing command response")
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	firstOrder := orderFromAcceptedEvent(t, events[0])
	secondOrder := orderFromAcceptedEvent(t, events[1])
	if firstOrder.Time() == secondOrder.Time() {
		t.Fatalf("retry did not restamp order time")
	}
	if firstOrder.ID() == secondOrder.ID() {
		t.Fatalf("retry did not create a new order ID")
	}

	var result msgjson.OrderResult
	if err := response.UnmarshalResult(&result); err != nil {
		t.Fatalf("response result: %v", err)
	}
	resultOrderID, err := order.IDFromBytes(result.OrderID)
	if err != nil {
		t.Fatalf("response order id: %v", err)
	}
	if resultOrderID != secondOrder.ID() {
		t.Fatalf("response order id = %v, want retried order id %v", resultOrderID, secondOrder.ID())
	}
	if result.ServerTime != uint64(secondOrder.Time()) {
		t.Fatalf("response server time = %d, want %d", result.ServerTime, secondOrder.Time())
	}
}

func seedPendingResumeState(mkt *Market) {
	mkt.epochMtx.Lock()
	mkt.lifecycleState = db.MarketStateSuspended
	mkt.pendingLifecycleAction = db.MarketPendingResume
	mkt.pendingLifecycleEpochIdx = 123
	mkt.pendingLifecycleEpochDur = int64(mkt.EpochDuration())
	mkt.persistBook = true
	mkt.persistBookSet = true
	mkt.epochMtx.Unlock()
}

func suspendedCancelRecord(t *testing.T, mkt *Market, storage *TArchivist, msgID uint64) (*order.LimitOrder, order.CoinID, *orderRecord) {
	t.Helper()
	coin := order.CoinID([]byte{0x61, 0x62, 0x63})
	target := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	target.Coins = []order.CoinID{coin}
	mkt.bookMtx.Lock()
	inserted := mkt.book.Insert(target)
	mkt.bookMtx.Unlock()
	if !inserted {
		t.Fatalf("failed to insert target order")
	}
	if !mkt.lockOrderCoins(target) {
		t.Fatalf("failed to lock target order coins")
	}
	if err := storage.BookOrder(target); err != nil {
		t.Fatalf("BookOrder error: %v", err)
	}

	targetID := target.ID()
	aid := target.User()
	cancelTime := time.Now().UnixMilli()
	pi := test.RandomPreimage()
	commit := pi.Commit()
	cancelMsg := &msgjson.CancelOrder{
		Prefix: msgjson.Prefix{
			AccountID:  aid[:],
			Base:       target.Base(),
			Quote:      target.Quote(),
			OrderType:  msgjson.CancelOrderNum,
			ClientTime: uint64(cancelTime),
			Commit:     commit[:],
		},
		TargetID: targetID[:],
	}
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  aid,
			BaseAsset:  target.Base(),
			QuoteAsset: target.Quote(),
			OrderType:  order.CancelOrderType,
			ClientTime: time.UnixMilli(cancelTime),
			Commit:     commit,
		},
		TargetOrderID: targetID,
	}
	rec := &orderRecord{
		order: co,
		req:   cancelMsg,
		msgID: msgID,
	}
	return target, coin, rec
}

func TestMarketAcceptOrderCommandSuspendedCancel(t *testing.T) {
	tests := []struct {
		name          string
		state         db.MarketState
		pendingResume bool
		wantErr       bool
	}{
		{name: "suspended", state: db.MarketStateSuspended},
		{name: "pending resume", state: db.MarketStateSuspended, pendingResume: true},
		{name: "draining", state: db.MarketStateDraining, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt, storage, auth, cleanup, err := newTestMarket()
			if err != nil {
				t.Fatalf("newTestMarket: %v", err)
			}
			defer cleanup()
			mkt.lifecycleState = tt.state
			if tt.pendingResume {
				seedPendingResumeState(mkt)
			}
			target, coin, rec := suspendedCancelRecord(t, mkt, storage, 42)
			targetID := target.ID()
			auth.handleMatchDone = make(chan *msgjson.Message, 1)
			svc, req := prepareOrderCommand(t, mkt, auth, rec)
			respond := req.Respond
			req.Respond = func(response *msgjson.Message) error {
				if len(auth.handleMatchDone) != 0 {
					t.Error("match request sent before acceptance response")
				}
				return respond(response)
			}
			rpcErr := svc.ExecuteCommand(context.Background(), req)
			if tt.wantErr {
				if rpcErr == nil || rpcErr.Code != msgjson.MarketNotRunningError {
					t.Fatalf("cancel error = %v, want market not running", rpcErr)
				}
				if len(storage.suspendedCancels) != 0 || !mkt.book.HaveOrder(targetID) || !mkt.CoinLocked(mkt.Base(), coin) {
					t.Fatal("rejected cancellation changed storage, book, or funding locks")
				}
				if auth.getSend() != nil || len(auth.handleMatchDone) != 0 {
					t.Fatal("rejected cancellation sent an acceptance response or match request")
				}
				return
			}
			if rpcErr != nil {
				t.Fatalf("cancel: %v", rpcErr)
			}
			if len(storage.suspendedCancels) != 1 {
				t.Fatalf("stored cancellations = %d, want 1", len(storage.suspendedCancels))
			}
			update := storage.suspendedCancels[0]
			if update.TargetOrderID != targetID {
				t.Fatalf("stored target = %v, want %v", update.TargetOrderID, targetID)
			}
			if mkt.book.HaveOrder(targetID) || mkt.CoinLocked(mkt.Base(), coin) {
				t.Fatal("canceled target remains booked or has locked funding")
			}

			response := auth.getSend()
			if response == nil || response.ID != rec.msgID || auth.getSend() != nil {
				t.Fatal("expected exactly one acceptance response with the request ID")
			}
			var result msgjson.OrderResult
			if err := response.UnmarshalResult(&result); err != nil {
				t.Fatalf("decode acceptance response: %v", err)
			}
			cancelID := rec.order.ID()
			if !bytes.Equal(result.OrderID, cancelID[:]) || result.ServerTime != uint64(rec.order.Time()) {
				t.Fatalf("acceptance response = %+v, want cancel %v at %d", result, cancelID, rec.order.Time())
			}
			if update.MatchServerTime.UnixMilli() != rec.order.Time() || update.EpochIdx != rec.order.Time()/update.EpochDur {
				t.Fatal("cancel, match, and epoch do not use the same server time")
			}

			var matchRequest *msgjson.Message
			select {
			case matchRequest = <-auth.handleMatchDone:
			default:
				t.Fatal("missing cancellation match request")
			}
			var matches []msgjson.Match
			if err := json.Unmarshal(matchRequest.Payload, &matches); err != nil {
				t.Fatalf("decode match request: %v", err)
			}
			if len(matches) != 2 {
				t.Fatalf("match notifications = %d, want maker and taker", len(matches))
			}
			matchID := update.Match.ID()
			for i, oid := range []order.OrderID{targetID, cancelID} {
				note := matches[i]
				if !bytes.Equal(note.OrderID, oid[:]) || !bytes.Equal(note.MatchID, matchID[:]) ||
					note.Side != uint8(i) || note.Quantity != target.Remaining() || note.ServerTime != result.ServerTime {
					t.Fatalf("match notification %d = %+v, want order %v, match %v at %d", i, note, oid, matchID, result.ServerTime)
				}
			}
		})
	}
}

func TestMarketAcceptOrderCommandSuspendedCancelBlocksDuringResumePreparation(t *testing.T) {
	mkt, storage, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket: %v", err)
	}
	defer cleanup()
	seedPendingResumeState(mkt)
	_, _, rec := suspendedCancelRecord(t, mkt, storage, 43)
	svc, req := prepareOrderCommand(t, mkt, auth, rec)
	result := make(chan *msgjson.Error, 1)

	func() {
		mkt.resumeSubmitMtx.Lock()
		defer mkt.resumeSubmitMtx.Unlock()
		go func() {
			result <- svc.ExecuteCommand(context.Background(), req)
		}()
		select {
		case rpcErr := <-result:
			t.Fatalf("cancel completed during resume preparation: %v", rpcErr)
		case <-time.After(100 * time.Millisecond):
		}
		storage.mtx.Lock()
		n := len(storage.suspendedCancels)
		storage.mtx.Unlock()
		if n != 0 {
			t.Fatalf("stored cancellations during resume preparation = %d, want 0", n)
		}
	}()

	select {
	case rpcErr := <-result:
		if rpcErr != nil {
			t.Fatalf("cancel after resume preparation: %v", rpcErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not complete after resume preparation")
	}
	if len(storage.suspendedCancels) != 1 {
		t.Fatalf("stored cancellations = %d, want 1", len(storage.suspendedCancels))
	}
}

func TestApplyOrderAcceptedEvent(t *testing.T) {
	type applyCase struct {
		name           string
		ord            order.Order
		epochGap       int32
		lockedCoin     order.CoinID
		wantCancelable bool
		wantOrderType  uint8
	}

	coinAssetID := func(mkt *Market, ord order.Order) uint32 {
		if ord.Trade().Sell {
			return mkt.Base()
		}
		return mkt.Quote()
	}

	requireOrderApplied := func(t *testing.T, mkt *Market, ord order.Order, lockedCoin order.CoinID, wantCancelable bool) {
		t.Helper()
		oid := ord.ID()
		if got := mkt.epochOrders[oid]; got == nil || got.ID() != oid {
			t.Fatalf("epochOrders entry mismatch. got %v, want %v", got, oid)
		}
		if got := mkt.epochCommitments[ord.Commitment()]; got != oid {
			t.Fatalf("epochCommitments entry mismatch. got %v, want %v", got, oid)
		}
		if lockedCoin != nil && !mkt.CoinLocked(coinAssetID(mkt, ord), lockedCoin) {
			t.Fatalf("accepted order coin was not locked")
		}
		if got := mkt.Cancelable(oid); got != wantCancelable {
			t.Fatalf("cancelable = %t, want %t", got, wantCancelable)
		}
		if mkt.book.HaveOrder(oid) {
			t.Fatalf("accepted order %v should not be in the booked order book", oid)
		}
	}

	requireOrderAcceptedUpdate := func(t *testing.T, update *db.OrderAcceptedUpdate, tt applyCase, epochIdx, epochDur int64) {
		t.Helper()
		oid := tt.ord.ID()
		if update.Order == nil || update.Order.ID() != oid {
			t.Fatalf("order accepted update order mismatch. got %v, want %v", update.Order, oid)
		}
		if update.EpochIdx != epochIdx {
			t.Fatalf("order accepted update epoch idx = %d, want %d", update.EpochIdx, epochIdx)
		}
		if update.EpochDur != epochDur {
			t.Fatalf("order accepted update epoch dur = %d, want %d", update.EpochDur, epochDur)
		}
		if update.EpochGap != tt.epochGap {
			t.Fatalf("order accepted update epoch gap = %d, want %d", update.EpochGap, tt.epochGap)
		}
	}

	requireOrderRejected := func(t *testing.T, mkt *Market, storage *TArchivist, ord order.Order, wantStorageWrites int) {
		t.Helper()
		if got := len(storage.orderAcceptedUpdates); got != wantStorageWrites {
			t.Fatalf("order accepted updates = %d, want %d", got, wantStorageWrites)
		}
		mkt.epochMtx.RLock()
		_, inserted := mkt.epochOrders[ord.ID()]
		mkt.epochMtx.RUnlock()
		if inserted {
			t.Fatalf("rejected order %v was inserted into epoch memory", ord.ID())
		}
	}

	t.Run("rejects order with no active epoch", func(t *testing.T) {
		// Startup seeding rebuilds epoch memory for every running market
		// before any event applies, so an order_accepted reaching a market
		// with no current epoch is an invariant violation and must fail
		// without any durable or memory effect.
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		link := rig.subscribeBook(t)
		mkt, storage := rig.mkt, rig.storage
		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(1234)
		lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		lo.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		lo.Coins = []order.CoinID{[]byte{0x01, 0x02, 0x03}}

		err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(lo))
		if err == nil || !strings.Contains(err.Error(), "no active epoch") {
			t.Fatalf("apply error = %v, want no active epoch", err)
		}
		requireOrderRejected(t, mkt, storage, lo, 0)
		requireNoBookNoteFromLink(t, link)
	})

	t.Run("applies order types", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		link := rig.subscribeBook(t)
		mkt, storage := rig.mkt, rig.storage
		limitCoin := order.CoinID([]byte{0x01, 0x02, 0x03})
		marketCoin := order.CoinID([]byte{0x41, 0x42, 0x43})
		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(1234)
		rig.submitMarketStarted(t, epochIdx)
		lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		lo.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		lo.Coins = []order.CoinID{limitCoin}
		mo := makeMO(seller3, 1)
		mo.SetTime(time.UnixMilli(epochIdx*epochDur + 2))
		mo.Coins = []order.CoinID{marketCoin}
		co := makeCO(seller3, lo.ID())
		co.SetTime(time.UnixMilli(epochIdx*epochDur + 3))

		tests := []applyCase{
			{
				name:           "limit",
				ord:            lo,
				epochGap:       db.EpochGapNA,
				lockedCoin:     limitCoin,
				wantCancelable: true,
				wantOrderType:  msgjson.LimitOrderNum,
			},
			{
				name:          "market",
				ord:           mo,
				epochGap:      db.EpochGapNA,
				lockedCoin:    marketCoin,
				wantOrderType: msgjson.MarketOrderNum,
			},
			{
				name:          "cancel",
				ord:           co,
				epochGap:      0,
				wantOrderType: msgjson.CancelOrderNum,
			},
		}

		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rig.apply(t, meshevents.NewOrderAcceptedEvent(tt.ord))
				if got := len(storage.orderAcceptedUpdates); got != i+1 {
					t.Fatalf("order accepted updates = %d, want %d", got, i+1)
				}
				requireOrderAcceptedUpdate(t, storage.orderAcceptedUpdates[i], tt, epochIdx, epochDur)
				requireOrderApplied(t, mkt, tt.ord, tt.lockedCoin, tt.wantCancelable)
				requireEpochNoteFromLink(t, link, mkt, tt.ord, epochIdx, tt.wantOrderType)
			})
		}
	})

	t.Run("uses active run parameters", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		mkt := rig.mkt

		// Use a lot size and cancel limit that differ from the configured values.
		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(2345)
		runParams := mkt.configuredParams.MarketRunParams
		runParams.LotSize *= 2
		runParams.MaxUserCancelsPerEpoch = 1
		rig.apply(t, newMarketStartedEvent(mkt.name, epochIdx, epochDur, runParams,
			time.UnixMilli(1).UTC(), nil))

		// One configured lot is too small for the active run.
		badLO := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		badLO.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		badLO.Coins = []order.CoinID{[]byte{0x71, 0x01}}
		if err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(badLO)); !errors.Is(err, ErrInvalidOrder) {
			t.Fatalf("config-lot order apply error = %v, want %v", err, ErrInvalidOrder)
		}

		// Two configured lots make one lot for the active run.
		goodLO := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		goodLO.SetTime(time.UnixMilli(epochIdx*epochDur + 2))
		goodLO.Coins = []order.CoinID{[]byte{0x71, 0x02}}
		rig.apply(t, meshevents.NewOrderAcceptedEvent(goodLO))
		goodLO2 := makeLO(seller3, mkRate3(1.1, 1.3), 2, order.StandingTiF)
		goodLO2.SetTime(time.UnixMilli(epochIdx*epochDur + 3))
		goodLO2.Coins = []order.CoinID{[]byte{0x71, 0x03}}
		rig.apply(t, meshevents.NewOrderAcceptedEvent(goodLO2))

		// The adopted cancel cap of one: the first cancel applies, a second
		// cancel against a different target is capped.
		co := makeCO(seller3, goodLO.ID())
		co.SetTime(time.UnixMilli(epochIdx*epochDur + 4))
		rig.apply(t, meshevents.NewOrderAcceptedEvent(co))
		co2 := makeCO(seller3, goodLO2.ID())
		co2.SetTime(time.UnixMilli(epochIdx*epochDur + 5))
		if err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(co2)); !errors.Is(err, ErrTooManyCancelOrders) {
			t.Fatalf("capped cancel apply error = %v, want %v", err, ErrTooManyCancelOrders)
		}
	})

	t.Run("duplicate cancel event appends storage without reprojecting memory", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		link := rig.subscribeBook(t)
		mkt, storage := rig.mkt, rig.storage
		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(1234)
		rig.submitMarketStarted(t, epochIdx)
		lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		lo.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		co := makeCO(seller3, lo.ID())
		co.SetTime(time.UnixMilli(epochIdx*epochDur + 2))

		rig.apply(t, meshevents.NewOrderAcceptedEvent(lo))
		requireEpochNoteFromLink(t, link, mkt, lo, epochIdx, msgjson.LimitOrderNum)
		rig.apply(t, meshevents.NewOrderAcceptedEvent(co))
		requireEpochNoteFromLink(t, link, mkt, co, epochIdx, msgjson.CancelOrderNum)

		rig.apply(t, meshevents.NewOrderAcceptedEvent(co))
		if got, want := len(storage.orderAcceptedUpdates), 3; got != want {
			t.Fatalf("storage writes = %d, want %d", got, want)
		}
		if got := storage.orderAcceptedUpdates[len(storage.orderAcceptedUpdates)-1].EpochGap; got != 0 {
			t.Fatalf("duplicate cancel stored epoch gap = %d, want 0", got)
		}
		if got := len(mkt.currentEpoch.Orders); got != 2 {
			t.Fatalf("epoch order count after duplicate cancel = %d, want 2", got)
		}
		if got := mkt.currentEpoch.UserCancels[co.AccountID]; got != 1 {
			t.Fatalf("cancel count after duplicate cancel = %d, want 1", got)
		}
		requireNoBookNoteFromLink(t, link)
	})

	t.Run("locked coin rejected before db apply", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		mkt, storage := rig.mkt, rig.storage
		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(1234)
		rig.submitMarketStarted(t, epochIdx)
		coin := order.CoinID([]byte{0x0a, 0x0b, 0x0c})
		lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		lo.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		lo.Coins = []order.CoinID{coin}
		if !mkt.lockOrderCoins(lo) {
			t.Fatalf("test setup failed to lock order coin")
		}

		err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(lo))
		if err == nil || !strings.Contains(err.Error(), "already-locked") {
			t.Fatalf("apply error = %v, want already-locked coin error", err)
		}
		requireOrderRejected(t, mkt, storage, lo, 0)
	})

	t.Run("rejects parcel limit before mutation", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		mkt, storage := rig.mkt, rig.storage
		var wantAsOf time.Time
		mkt.checkParcelLimit = func(_ account.AccountID, asOf time.Time, calcParcels MarketParcelCalculator) (bool, error) {
			if !asOf.Equal(wantAsOf) {
				t.Fatalf("parcel check time = %v, want order server time %v", asOf, wantAsOf)
			}
			return calcParcels(0) <= 1, nil
		}

		epochDur := int64(mkt.EpochDuration())
		epochIdx := int64(2345)
		rig.submitMarketStarted(t, epochIdx)
		first := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		first.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
		first.Coins = []order.CoinID{[]byte{0x01, 0x11, 0x21}}
		second := makeLO(seller3, mkRate3(1.1, 1.3), 1, order.StandingTiF)
		second.SetTime(time.UnixMilli(epochIdx*epochDur + 2))
		secondCoin := order.CoinID([]byte{0x02, 0x12, 0x22})
		second.Coins = []order.CoinID{secondCoin}

		wantAsOf = first.ServerTime
		rig.apply(t, meshevents.NewOrderAcceptedEvent(first))

		wantAsOf = second.ServerTime
		if err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(second)); !errors.Is(err, ErrQuantityTooHigh) {
			t.Fatalf("second apply error = %v, want %v", err, ErrQuantityTooHigh)
		}
		requireOrderRejected(t, mkt, storage, second, 1)
		if mkt.CoinLocked(mkt.Base(), secondCoin) {
			t.Fatalf("rejected order coin was locked")
		}
	})
}

func TestMarket_SwapLockedCoinsRejectOrders(t *testing.T) {
	lockers := &tMasterLockers{}
	rig := newMarketEventRig(t, lockers)
	defer rig.cleanup()
	mkt := rig.mkt
	epochDur := int64(mkt.EpochDuration())
	epochIdx := int64(1234)
	rig.submitMarketStarted(t, epochIdx)

	coin := order.CoinID([]byte{0x0a, 0x0b, 0x0c})
	inSwap := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
	inSwap.Coins = []order.CoinID{coin}
	if failed := lockers.base.Swap().LockOrdersCoins([]order.Order{inSwap}); len(failed) > 0 {
		t.Fatalf("test setup failed to lock the in-swap coin")
	}

	lo := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
	lo.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
	lo.Coins = []order.CoinID{coin}
	err := rig.applyErr(t, meshevents.NewOrderAcceptedEvent(lo))
	if err == nil || !strings.Contains(err.Error(), "already-locked") {
		t.Fatalf("apply error = %v, want already-locked coin error", err)
	}
}

// collectOwnerNotes drains the auth feed and collects revoke_order and
// nomatch notes by order ID, failing on any penalty note.
func collectOwnerNotes(t *testing.T, auth *TAuth) (revokes, nomatches map[order.OrderID]bool) {
	t.Helper()
	revokes = make(map[order.OrderID]bool)
	nomatches = make(map[order.OrderID]bool)
	for msg := auth.getSend(); msg != nil; msg = auth.getSend() {
		switch msg.Route {
		case msgjson.RevokeOrderRoute:
			var note msgjson.RevokeOrder
			if err := json.Unmarshal(msg.Payload, &note); err != nil {
				t.Fatalf("revoke note unmarshal: %v", err)
			}
			var oid order.OrderID
			copy(oid[:], note.OrderID)
			revokes[oid] = true
		case msgjson.NoMatchRoute:
			var note msgjson.NoMatch
			if err := json.Unmarshal(msg.Payload, &note); err != nil {
				t.Fatalf("nomatch note unmarshal: %v", err)
			}
			var oid order.OrderID
			copy(oid[:], note.OrderID)
			nomatches[oid] = true
		case msgjson.PenaltyRoute:
			t.Fatalf("unexpected penalty note")
		}
	}
	return revokes, nomatches
}

func TestApplyMarketStartedEvent(t *testing.T) {
	t.Run("revokes orders and resets epoch queues", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		mkt, storage, auth := rig.mkt, rig.storage, rig.auth
		mktName := mkt.name
		epochDur := int64(mkt.EpochDuration())
		const seededEpochIdx int64 = 42
		startedEpochIdx := seededEpochIdx + 100 // the restart jumps the epoch

		// Warm state: a queued trade and cancel seeded from storage, plus a
		// booked order in the market and router books.
		epochCoin := order.CoinID{0xa4, 0xb5, 0xc6}
		epochLO := epochStampedLO(t, seededEpochIdx, epochDur, 1, epochCoin)
		epochCO := epochStampedCO(t, epochLO.ID(), seededEpochIdx, epochDur, 2)
		seedEpochOrder(storage, epochLO, seededEpochIdx, epochDur)
		seedEpochOrder(storage, epochCO, seededEpochIdx, epochDur)
		seedRunningLifecycle(t, mkt, seededEpochIdx, epochDur)
		mkt.epochMtx.RLock()
		oldCurrent, oldNext := mkt.currentEpoch, mkt.nextEpoch
		mkt.epochMtx.RUnlock()

		bookedCoin := order.CoinID{0xa1, 0xb2, 0xc3}
		booked := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		booked.Coins = []order.CoinID{bookedCoin}
		bookStandingOrder(t, rig, booked)
		book := rig.bookRouter.books[mktName]
		rig.bookRouter.SeedBooks()
		link := rig.subscribeBook(t)

		revocationTime := time.UnixMilli(123456789).UTC()
		startedEvent := meshevents.NewMarketStartedEvent(mktName, startedEpochIdx, epochDur,
			mkt.configuredParams.MarketRunParams, revocationTime,
			[]meshevents.StartupOrderRevokeRecord{
				meshevents.NewStartupOrderRevokeRecord(booked, meshevents.StartupOrderRevokeReasonLotSizeIncompatible),
			}, []meshevents.StartupOrderRevokeRecord{
				meshevents.NewStartupOrderRevokeRecord(epochLO, meshevents.StartupOrderRevokeReasonEpochAbandoned),
				meshevents.NewStartupOrderRevokeRecord(epochCO, meshevents.StartupOrderRevokeReasonEpochAbandoned),
			})
		rig.apply(t, startedEvent)

		// The stored update mirrors the event.
		if len(storage.marketStartedUpdates) != 1 {
			t.Fatalf("market started updates = %d, want 1", len(storage.marketStartedUpdates))
		}
		update := storage.marketStartedUpdates[0]
		if update.Market != mktName || update.RevocationTime != revocationTime {
			t.Fatalf("update market/time = %q/%v, want %q/%v",
				update.Market, update.RevocationTime, mktName, revocationTime)
		}
		if update.CurrentEpochIdx != startedEpochIdx || update.EpochDur != epochDur {
			t.Fatalf("market started epoch = %d:%d, want %d:%d",
				update.CurrentEpochIdx, update.EpochDur, startedEpochIdx, epochDur)
		}
		if len(update.BookedRevokes) != 1 || update.BookedRevokes[0].Order.ID() != booked.ID() ||
			update.BookedRevokes[0].Reason != meshevents.StartupOrderRevokeReasonLotSizeIncompatible {
			t.Fatalf("booked revokes mismatch: %+v", update.BookedRevokes)
		}

		// Every revoked order left the book with its coins unlocked. Storage
		// epochOrders is a seed for EpochOrders() reads, not a projected table.
		requireRevokedOrderGone(t, mkt, booked)
		requireRevokedOrderGone(t, mkt, epochLO)

		// Epoch memory replaced wholesale: fresh empty queues at the started
		// epoch, old pointers not reused, indexes emptied, cursors moved.
		mkt.epochMtx.RLock()
		if mkt.currentEpoch == nil || mkt.currentEpoch.Epoch != startedEpochIdx ||
			mkt.currentEpoch.Duration != epochDur || len(mkt.currentEpoch.Orders) != 0 {
			t.Fatalf("current epoch = %v, want empty %d:%d", mkt.currentEpoch, startedEpochIdx, epochDur)
		}
		if mkt.nextEpoch == nil || mkt.nextEpoch.Epoch != startedEpochIdx+1 ||
			mkt.nextEpoch.Duration != epochDur || len(mkt.nextEpoch.Orders) != 0 {
			t.Fatalf("next epoch = %v, want empty %d:%d", mkt.nextEpoch, startedEpochIdx+1, epochDur)
		}
		if mkt.currentEpoch == oldCurrent || mkt.nextEpoch == oldNext {
			t.Fatalf("market_started reused seeded epoch queues")
		}
		if len(mkt.epochOrders) != 0 || len(mkt.epochCommitments) != 0 {
			t.Fatalf("queued orders survived market_started: orders=%d commitments=%d",
				len(mkt.epochOrders), len(mkt.epochCommitments))
		}
		if mkt.startEpochIdx != startedEpochIdx || mkt.activeEpochIdx != startedEpochIdx {
			t.Fatalf("epoch cursors = %d/%d, want %d", mkt.startEpochIdx, mkt.activeEpochIdx, startedEpochIdx)
		}
		mkt.epochMtx.RUnlock()
		if bookEpoch, _, _ := mkt.Book(); bookEpoch != startedEpochIdx {
			t.Fatalf("market book epoch = %d, want %d", bookEpoch, startedEpochIdx)
		}
		if routerEpoch := book.epoch(); routerEpoch != startedEpochIdx {
			t.Fatalf("router book epoch = %d, want %d", routerEpoch, startedEpochIdx)
		}

		// Notes: an unbook for the booked order; revoke notes for the booked
		// order and the epoch trade; a nomatch for the epoch cancel; no penalty.
		unbookMsg := link.getSend()
		if unbookMsg == nil || unbookMsg.Route != msgjson.UnbookOrderRoute {
			t.Fatalf("unbook route = %v, want %q", unbookMsg, msgjson.UnbookOrderRoute)
		}
		var unbookNote msgjson.UnbookOrderNote
		if err := json.Unmarshal(unbookMsg.Payload, &unbookNote); err != nil {
			t.Fatalf("unbook note: %v", err)
		}
		bookedID := booked.ID()
		if !bytes.Equal(unbookNote.OrderID, bookedID[:]) {
			t.Fatalf("unbook order id = %x, want %x", unbookNote.OrderID, bookedID)
		}
		revokeIDs, nomatchIDs := collectOwnerNotes(t, auth)
		if len(revokeIDs) != 2 || !revokeIDs[booked.ID()] || !revokeIDs[epochLO.ID()] {
			t.Fatalf("revoke notes = %v, want %v and %v", revokeIDs, booked.ID(), epochLO.ID())
		}
		if len(nomatchIDs) != 1 || !nomatchIDs[epochCO.ID()] {
			t.Fatalf("nomatch notes = %v, want %v", nomatchIDs, epochCO.ID())
		}
	})

	t.Run("pending suspend start keeps the suspend and opens the final epoch", func(t *testing.T) {
		rig := newMarketEventRig(t)
		defer rig.cleanup()
		mkt, storage := rig.mkt, rig.storage
		epochDur := int64(mkt.EpochDuration())
		finalEpochIdx := int64(42)
		row := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingSuspend, finalEpochIdx, epochDur)
		setTestMarketLifecycle(mkt, storage, row)
		lo := epochStampedLO(t, finalEpochIdx, epochDur, 1, order.CoinID{0xa7, 0xb8})
		seedEpochOrder(storage, lo, finalEpochIdx, epochDur)
		if err := mkt.restoreEpochState(row); err != nil {
			t.Fatalf("restoreEpochState: %v", err)
		}
		startedEvent := meshevents.NewMarketStartedEvent(mkt.name, finalEpochIdx, epochDur,
			mkt.configuredParams.MarketRunParams, time.UnixMilli(123456789).UTC(), nil,
			[]meshevents.StartupOrderRevokeRecord{
				meshevents.NewStartupOrderRevokeRecord(lo, meshevents.StartupOrderRevokeReasonEpochAbandoned),
			})
		rig.apply(t, startedEvent)
		mkt.epochMtx.RLock()
		defer mkt.epochMtx.RUnlock()
		if mkt.pendingLifecycleAction != db.MarketPendingSuspend {
			t.Fatalf("pending action = %v, want %v", mkt.pendingLifecycleAction, db.MarketPendingSuspend)
		}
		if mkt.currentEpoch == nil || mkt.currentEpoch.Epoch != finalEpochIdx || len(mkt.currentEpoch.Orders) != 0 {
			t.Fatalf("current epoch = %v, want empty %d", mkt.currentEpoch, finalEpochIdx)
		}
	})
}

func TestApplyMarketResumedEvent(t *testing.T) {
	const scheduledEpochIdx int64 = 88
	newPendingResumeRig := func(t *testing.T) *marketEventRig {
		t.Helper()
		rig := newMarketEventRig(t)
		t.Cleanup(rig.cleanup)
		mkt := rig.mkt
		epochDur := int64(mkt.EpochDuration())
		persistBook := true
		setTestMarketLifecycle(mkt, rig.storage, &db.MarketLifecycle{
			Market:          mkt.name,
			State:           db.MarketStateSuspended,
			StartEpochIdx:   scheduledEpochIdx,
			StartEpochDur:   epochDur,
			PendingAction:   db.MarketPendingResume,
			PendingEpochIdx: scheduledEpochIdx,
			PendingEpochDur: epochDur,
			PersistBook:     &persistBook,
			RunParams:       mkt.configuredParams.MarketRunParams,
		})
		return rig
	}

	t.Run("late resume revokes listed orders", func(t *testing.T) {
		rig := newPendingResumeRig(t)
		mkt := rig.mkt
		revokedOrder := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		revokedOrder.Coins = []order.CoinID{{0xd1, 0xe2, 0xf3}}
		bookStandingOrder(t, rig, revokedOrder)
		book := rig.bookRouter.books[mkt.name]
		rig.bookRouter.SeedBooks()
		link := rig.subscribeBook(t)

		epochDur := int64(mkt.EpochDuration())
		resumedEpochIdx := scheduledEpochIdx + 2
		event := &meshevents.MarketResumedEvent{
			Market:        mkt.name,
			StartEpochIdx: scheduledEpochIdx,
			EpochDur:      epochDur,
			Timestamp:     resumedEpochIdx * epochDur,
			RunParams:     mkt.configuredParams.MarketRunParams,
			ResumeRevokes: []meshevents.StartupOrderRevokeRecord{
				meshevents.NewStartupOrderRevokeRecord(revokedOrder, meshevents.StartupOrderRevokeReasonFundingCoinSpent),
			},
		}
		rig.apply(t, event)

		requireRevokedOrderGone(t, mkt, revokedOrder)
		snapshot := rig.bookRouter.msgOrderBook(book)
		if snapshot == nil || snapshot.Epoch != uint64(resumedEpochIdx) {
			t.Fatalf("book snapshot = %+v, want epoch %d", snapshot, resumedEpochIdx)
		}
		if len(snapshot.Orders) != 0 {
			t.Fatalf("book snapshot has %d orders, want none", len(snapshot.Orders))
		}

		unbookMsg := link.getSend()
		if unbookMsg == nil || unbookMsg.Route != msgjson.UnbookOrderRoute {
			t.Fatalf("first route = %v, want %q", unbookMsg, msgjson.UnbookOrderRoute)
		}
		var unbookNote msgjson.UnbookOrderNote
		if err := json.Unmarshal(unbookMsg.Payload, &unbookNote); err != nil {
			t.Fatalf("unbook note: %v", err)
		}
		oid := revokedOrder.ID()
		if !bytes.Equal(unbookNote.OrderID, oid[:]) {
			t.Fatalf("unbook order id = %x, want %x", unbookNote.OrderID, oid)
		}
		resumeMsg := link.getSend()
		if resumeMsg == nil || resumeMsg.Route != msgjson.ResumptionRoute {
			t.Fatalf("second route = %v, want %q", resumeMsg, msgjson.ResumptionRoute)
		}
		var resumeNote msgjson.TradeResumption
		if err := json.Unmarshal(resumeMsg.Payload, &resumeNote); err != nil {
			t.Fatalf("resume note: %v", err)
		}
		if resumeNote.MarketID != mkt.name || resumeNote.StartEpoch != uint64(resumedEpochIdx) {
			t.Fatalf("resume note = %+v, want market %s at epoch %d", resumeNote, mkt.name, resumedEpochIdx)
		}
	})

	t.Run("lot size change requires revoking incompatible orders", func(t *testing.T) {
		rig := newPendingResumeRig(t)
		mkt := rig.mkt
		oldParams := mkt.configuredParams.MarketRunParams
		incompatibleOrder := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		incompatibleOrder.Coins = []order.CoinID{{0x81, 0x01}}
		bookStandingOrder(t, rig, incompatibleOrder)
		compatibleOrder := makeLO(seller3, mkRate3(1.1, 1.3), 2, order.StandingTiF)
		compatibleOrder.Coins = []order.CoinID{{0x81, 0x02}}
		bookStandingOrder(t, rig, compatibleOrder)

		newParams := oldParams
		newParams.LotSize = oldParams.LotSize * 2
		epochDur := int64(mkt.EpochDuration())
		event := &meshevents.MarketResumedEvent{
			Market:        mkt.name,
			StartEpochIdx: scheduledEpochIdx,
			EpochDur:      epochDur,
			Timestamp:     scheduledEpochIdx * epochDur,
			RunParams:     newParams,
		}
		if err := rig.applyErr(t, event); err == nil || !strings.Contains(err.Error(), "incompatible with lot size") {
			t.Fatalf("resume error = %v, want rejection because the incompatible order was not listed for revocation", err)
		}
		if mkt.LotSize() != oldParams.LotSize {
			t.Fatalf("rejected resume changed the adopted lot size")
		}

		event.ResumeRevokes = []meshevents.StartupOrderRevokeRecord{
			meshevents.NewStartupOrderRevokeRecord(incompatibleOrder, meshevents.StartupOrderRevokeReasonLotSizeIncompatible),
		}
		rig.apply(t, event)
		if mkt.LotSize() != newParams.LotSize {
			t.Fatalf("adopted lot size = %d, want %d", mkt.LotSize(), newParams.LotSize)
		}
		if mkt.book.HaveOrder(incompatibleOrder.ID()) {
			t.Fatalf("incompatible order remained booked")
		}
		if mkt.book.Order(compatibleOrder.ID()) == nil {
			t.Fatalf("compatible order left the book")
		}
	})
}

// revokeBeforeApplyMesh changes book state after funding checks, before applying
// their event, without relying on concurrent goroutine timing.
type revokeBeforeApplyMesh struct {
	*tMesh
	beforeApply func()
}

func (m *revokeBeforeApplyMesh) ApplyEvent(ctx context.Context, event *mesh.Event) (any, error) {
	m.beforeApply()
	return m.tMesh.ApplyEvent(ctx, event)
}

type spentFundingSwapper struct{ epochProcessedTestSwapper }

func (*spentFundingSwapper) CheckUnspent(context.Context, uint32, []byte) error {
	return asset.CoinNotFoundError
}

func TestCheckUnfilledReturnsAppliedOrders(t *testing.T) {
	rig := newMarketEventRig(t)
	defer rig.cleanup()
	mkt := rig.mkt
	spent := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
	matchedBeforeApply := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
	for i, lo := range []*order.LimitOrder{spent, matchedBeforeApply} {
		lo.Coins = []order.CoinID{{byte(i + 1)}}
		bookStandingOrder(t, rig, lo)
	}
	rig.bookRouter.SeedBooks()
	mkt.swapper = &spentFundingSwapper{}
	meshStub := &tMesh{events: rig.events}
	mkt.mesh = &revokeBeforeApplyMesh{
		tMesh: meshStub,
		beforeApply: func() {
			// Matching occurs after funding checks but before revocation is applied.
			matchedBeforeApply.AddFill(mkt.LotSize())
		},
	}
	got := mkt.CheckUnfilled(mkt.base, spent.User())
	if len(meshStub.entries) != 1 {
		t.Fatalf("got %d events, want 1", len(meshStub.entries))
	}
	payload, err := meshevents.DecodeOrdersRevokedEvent(meshStub.entries[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	var eventIDs []order.OrderID
	for _, id := range payload.OrderIDs {
		eventIDs = append(eventIDs, order.OrderID(id))
	}
	if len(eventIDs) != 2 || !slices.Contains(eventIDs, spent.ID()) || !slices.Contains(eventIDs, matchedBeforeApply.ID()) {
		t.Fatalf("event targets = %v, want both orders", eventIDs)
	}
	if len(got) != 1 || got[0].ID() != spent.ID() {
		t.Fatalf("CheckUnfilled returned %v, want only %v", got, spent.ID())
	}
	requireRevokedOrderGone(t, mkt, spent)
	if !mkt.book.HaveOrder(matchedBeforeApply.ID()) || !mkt.CoinLocked(mkt.base, matchedBeforeApply.Coins[0]) {
		t.Fatal("partially filled order lost its book entry or funding lock")
	}
}

func TestApplyOrdersRevokedEvent(t *testing.T) {
	type fixture struct {
		*marketEventRig
		second  *Market
		markets map[string]*Market
		links   map[string]*TLink
	}
	newFixture := func(t *testing.T) *fixture {
		t.Helper()
		rig := newMarketEventRig(t)
		t.Cleanup(rig.cleanup)
		second, _, _, cleanup, err := newTestMarket(rig.storage, [2]*asset.BackedAsset{assetBTC, assetDCR})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		second.auth = rig.auth
		markets := map[string]*Market{rig.mkt.name: rig.mkt, second.name: second}
		rig.bookRouter = NewBookRouter(map[string]BookSource{rig.mkt.name: rig.mkt, second.name: second},
			&tFeeSource{}, func(string, comms.MsgHandler) {})
		rig.events = Events(markets, rig.bookRouter, rig.auth.SendIfLocal, nil)
		return &fixture{marketEventRig: rig, second: second, markets: markets, links: make(map[string]*TLink)}
	}
	newOrder := func(mkt *Market, user account.AccountID, sell bool, coin byte) *order.LimitOrder {
		lo := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		lo.AccountID, lo.BaseAsset, lo.QuoteAsset = user, mkt.base, mkt.quote
		lo.Sell = sell
		lo.Coins = []order.CoinID{{coin}}
		return lo
	}
	bookOrders := func(t *testing.T, mkt *Market, orders ...*order.LimitOrder) {
		t.Helper()
		for _, lo := range orders {
			if !mkt.book.Insert(lo) || !mkt.lockOrderCoins(lo) {
				t.Fatalf("failed to book and lock order %v", lo.ID())
			}
			mkt.settling[lo.ID()] = mkt.LotSize()
		}
	}
	subscribeBooks := func(t *testing.T, f *fixture) {
		t.Helper()
		f.bookRouter.SeedBooks()
		for name, mkt := range f.markets {
			link, sub := newSubscriber(&test.Market{Base: mkt.base, Quote: mkt.quote})
			if err := f.bookRouter.handleOrderBook(link, sub); err != nil {
				t.Fatal(err)
			}
			link.getSend() // initial book response
			f.links[name] = link
		}
	}
	requireUpdate := func(t *testing.T, f *fixture, event *meshevents.OrdersRevokedEvent, result any, want ...*order.LimitOrder) {
		t.Helper()
		if len(f.storage.ordersRevokedUpdates) != 1 {
			t.Fatalf("got %d DB updates, want 1", len(f.storage.ordersRevokedUpdates))
		}
		update := f.storage.ordersRevokedUpdates[0]
		if update.Reason != event.Reason || !update.RevokeTime.Equal(time.UnixMilli(event.RevokeTime)) {
			t.Fatalf("unexpected revocation metadata: %+v", update)
		}
		var wantIDs []order.OrderID
		for _, lo := range want {
			wantIDs = append(wantIDs, lo.ID())
		}
		slices.SortFunc(wantIDs, func(a, b order.OrderID) int { return bytes.Compare(a[:], b[:]) })
		for name, orders := range map[string][]*order.LimitOrder{"DB targets": update.Orders, "result": result.([]*order.LimitOrder)} {
			var ids []order.OrderID
			for _, lo := range orders {
				ids = append(ids, lo.ID())
			}
			if !slices.Equal(ids, wantIDs) {
				t.Fatalf("%s = %v, want %v", name, ids, wantIDs)
			}
		}
	}
	requireRemoved := func(t *testing.T, f *fixture, lo *order.LimitOrder) {
		t.Helper()
		name, _ := dex.MarketName(lo.Base(), lo.Quote())
		mkt := f.markets[name]
		requireRevokedOrderGone(t, mkt, lo)
		if _, found := mkt.settling[lo.ID()]; found {
			t.Fatal("revoked order remains in settling map")
		}
		if _, found := f.bookRouter.books[name].orders[lo.ID()]; found {
			t.Fatal("revoked order remains in router book")
		}
		msg := f.auth.getSend()
		if msg == nil || msg.Route != msgjson.RevokeOrderRoute {
			t.Fatalf("expected revoke notification, got %v", msg)
		}
		var note msgjson.RevokeOrder
		if err := json.Unmarshal(msg.Payload, &note); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(note.OrderID, lo.ID().Bytes()) {
			t.Fatalf("owner notification ID = %x, want %v", note.OrderID, lo.ID())
		}
		unbook := getUnbookNoteFromLink(t, f.links[name])
		if !bytes.Equal(unbook.OrderID, lo.ID().Bytes()) || unbook.MarketID != name {
			t.Fatalf("unexpected unbook notification: %+v", unbook)
		}
	}
	requireRetained := func(t *testing.T, f *fixture, mkt *Market, orders ...*order.LimitOrder) {
		t.Helper()
		for _, lo := range orders {
			_, inRouter := f.bookRouter.books[mkt.name].orders[lo.ID()]
			if !inRouter || !mkt.book.HaveOrder(lo.ID()) {
				t.Fatalf("retained order %v missing from market or router book", lo.ID())
			}
			assetID := mkt.quote
			if lo.Sell {
				assetID = mkt.base
			}
			if !mkt.CoinLocked(assetID, lo.Coins[0]) || mkt.settling[lo.ID()] != mkt.LotSize() {
				t.Fatalf("retained order %v lost funding lock or settling entry", lo.ID())
			}
		}
	}
	requireNoMoreNotifications := func(t *testing.T, f *fixture) {
		t.Helper()
		if extra := f.auth.getSend(); extra != nil {
			t.Fatalf("unexpected owner notification: %v", extra)
		}
		for _, link := range f.links {
			requireNoBookNoteFromLink(t, link)
		}
	}
	revokeTime := time.UnixMilli(123456789).UTC()

	t.Run("explicit order IDs", func(t *testing.T) {
		f := newFixture(t)
		buy := newOrder(f.mkt, seller3.Acct, false, 1)
		sell := newOrder(f.mkt, seller3.Acct, true, 2)
		partial := newOrder(f.mkt, seller3.Acct, true, 3)
		partial.AddFill(f.mkt.LotSize())
		unlisted := newOrder(f.mkt, seller3.Acct, true, 4)
		otherMarket := newOrder(f.second, seller3.Acct, true, 5)
		bookOrders(t, f.mkt, buy, sell, partial, unlisted)
		bookOrders(t, f.second, otherMarket)
		subscribeBooks(t, f)

		// The missing order, partial fill, and duplicate must not add revocations.
		ids := []order.OrderID{sell.ID(), buy.ID(), {}, partial.ID(), sell.ID()}
		payload := meshevents.NewOrdersRevokedForOrdersEvent(f.mkt.name, ids, meshevents.OrderRevokeReasonFundingSpent, revokeTime)
		event, err := mesh.NewEvent(payload)
		if err != nil {
			t.Fatal(err)
		}
		result, err := (&tMesh{events: f.events}).ApplyEvent(context.Background(), event)
		if err != nil {
			t.Fatal(err)
		}

		requireUpdate(t, f, payload, result, buy, sell)
		// Notifications follow the sorted database targets.
		for _, lo := range f.storage.ordersRevokedUpdates[0].Orders {
			requireRemoved(t, f, lo)
		}
		requireRetained(t, f, f.mkt, partial, unlisted)
		requireRetained(t, f, f.second, otherMarket)
		requireNoMoreNotifications(t, f)
	})

	t.Run("account orders across markets", func(t *testing.T) {
		f := newFixture(t)
		buy := newOrder(f.mkt, seller3.Acct, false, 1)
		sell := newOrder(f.mkt, seller3.Acct, true, 2)
		partial := newOrder(f.mkt, seller3.Acct, true, 3)
		partial.AddFill(f.mkt.LotSize())
		otherUser := newOrder(f.mkt, buyer3.Acct, true, 4)
		otherMarket := newOrder(f.second, seller3.Acct, true, 5)
		bookOrders(t, f.mkt, buy, sell, partial, otherUser)
		bookOrders(t, f.second, otherMarket)
		subscribeBooks(t, f)

		payload := meshevents.NewOrdersRevokedForUserEvent(seller3.Acct, meshevents.OrderRevokeReasonPenalty, revokeTime)
		event, err := mesh.NewEvent(payload)
		if err != nil {
			t.Fatal(err)
		}
		result, err := (&tMesh{events: f.events}).ApplyEvent(context.Background(), event)
		if err != nil {
			t.Fatal(err)
		}

		requireUpdate(t, f, payload, result, buy, sell, partial, otherMarket)
		for _, lo := range f.storage.ordersRevokedUpdates[0].Orders {
			requireRemoved(t, f, lo)
		}
		requireRetained(t, f, f.mkt, otherUser)

		// One account penalty is sent, regardless of the number of revoked orders.
		msg := f.auth.getSend()
		if msg == nil || msg.Route != msgjson.PenaltyRoute {
			t.Fatalf("expected one penalty notification, got %v", msg)
		}
		var note msgjson.PenaltyNote
		if err := json.Unmarshal(msg.Payload, &note); err != nil {
			t.Fatal(err)
		}
		if note.Penalty.Time != uint64(revokeTime.UnixMilli()) {
			t.Fatalf("penalty time = %d, want %d", note.Penalty.Time, revokeTime.UnixMilli())
		}
		requireNoMoreNotifications(t, f)
	})
}

func TestApplyAdvanceEpochEvent(t *testing.T) {
	const closedEpochIdx int64 = 10
	const nextEpochIdx = closedEpochIdx + 1

	type fixture struct {
		*marketEventRig
		book   *msgBook
		closed []order.Order
		next   []order.Order
		event  *meshevents.AdvanceEpochEvent
	}
	newFixture := func(t *testing.T, pending db.MarketPendingAction) *fixture {
		t.Helper()
		rig := newMarketEventRig(t)
		t.Cleanup(rig.cleanup)
		mkt := rig.mkt
		epochDur := int64(mkt.EpochDuration())
		f := &fixture{
			marketEventRig: rig,
			book:           rig.bookRouter.books[mkt.name],
			closed: []order.Order{
				epochStampedLO(t, closedEpochIdx, epochDur, 1, order.CoinID{0x11}),
				epochStampedLO(t, closedEpochIdx, epochDur, 2, order.CoinID{0x12}),
			},
			event: meshevents.NewAdvanceEpochEvent(mkt.name, closedEpochIdx, nextEpochIdx, epochDur),
		}
		if pending == db.MarketPendingSuspend {
			f.event.OpenedEpochIdx = 0
		} else {
			f.next = []order.Order{epochStampedLO(t, nextEpochIdx, epochDur, 1, order.CoinID{0x21})}
		}
		for _, ord := range f.closed {
			seedEpochOrder(rig.storage, ord, closedEpochIdx, epochDur)
		}
		for _, ord := range f.next {
			seedEpochOrder(rig.storage, ord, nextEpochIdx, epochDur)
		}
		lc := seedLifecycleRow(db.MarketStateRunning, pending, closedEpochIdx, epochDur)
		rig.storage.lifecycle = lc
		mkt.epochMtx.Lock()
		mkt.projectMarketLifecycleLocked(lc)
		mkt.epochMtx.Unlock()
		if err := mkt.restoreEpochState(lc); err != nil {
			t.Fatalf("restoreEpochState: %v", err)
		}
		mkt.running.Store(true)
		f.book.setEpoch(closedEpochIdx)
		return f
	}

	// Check the queues, funding locks, book epochs, and cancelability.
	requireEpochState := func(t *testing.T, f *fixture, activeEpoch, bookEpoch int64, queued, dequeued []order.Order) {
		t.Helper()
		requireSeededState(t, f.mkt, activeEpoch, queued, dequeued)
		if got := f.book.epoch(); got != bookEpoch {
			t.Fatalf("bookrouter epoch = %d, want %d", got, bookEpoch)
		}
		if got := f.mkt.bookEpochIdx; got != bookEpoch {
			t.Fatalf("market book epoch = %d, want %d", got, bookEpoch)
		}
		for _, ord := range queued {
			if !f.mkt.Cancelable(ord.ID()) {
				t.Fatalf("queued order %v should be cancelable", ord.ID())
			}
		}
		for _, ord := range dequeued {
			if f.mkt.Cancelable(ord.ID()) {
				t.Fatalf("dequeued order %v unexpectedly cancelable", ord.ID())
			}
		}
	}
	requireStoredEvent := func(t *testing.T, f *fixture) {
		t.Helper()
		if got := len(f.storage.advanceEpochEvents); got != 1 {
			t.Fatalf("advance epoch events = %d, want 1", got)
		}
		if got := f.storage.advanceEpochEvents[0]; *got != *f.event {
			t.Fatalf("stored advance epoch event = %+v, want %+v", got, f.event)
		}
	}

	t.Run("opens next epoch", func(t *testing.T) {
		f := newFixture(t, db.MarketPendingNone)
		f.apply(t, f.event)

		requireStoredEvent(t, f)
		requireEpochState(t, f, nextEpochIdx, nextEpochIdx, f.next, f.closed)
		if !f.mkt.running.Load() {
			t.Fatal("advancement stopped order intake")
		}
	})

	t.Run("closes final epoch", func(t *testing.T) {
		f := newFixture(t, db.MarketPendingSuspend)
		f.apply(t, f.event)

		requireStoredEvent(t, f)
		requireEpochState(t, f, 0, closedEpochIdx, nil, f.closed)
		mkt := f.mkt
		if mkt.lifecycleState != db.MarketStateDraining || mkt.running.Load() ||
			mkt.pendingLifecycleAction != db.MarketPendingNone ||
			mkt.pendingLifecycleEpochIdx != 0 || mkt.pendingLifecycleEpochDur != 0 {
			t.Fatal("final close did not stop intake and clear the pending suspension")
		}
		if mkt.currentEpoch != nil || mkt.nextEpoch != nil || mkt.LifecycleState() != db.MarketStateDraining {
			t.Fatal("draining market still has epoch queues or the wrong state")
		}
		epochDur := f.event.EpochDur
		if err := mkt.validateScheduleSuspendEvent(closedEpochIdx+1, epochDur); err == nil {
			t.Fatal("draining market accepted another suspend schedule")
		}
		lo := epochStampedLO(t, closedEpochIdx, epochDur, 1, order.CoinID{0x45})
		if _, _, _, err := mkt.acceptedOrderEpoch(lo); err == nil {
			t.Fatal("draining market accepted an order")
		}
		if !mkt.drainingFinalEpoch(closedEpochIdx, epochDur) || mkt.drainingFinalEpoch(closedEpochIdx-1, epochDur) ||
			mkt.drainingFinalEpoch(closedEpochIdx, epochDur*2) {
			t.Fatal("incorrect final draining epoch")
		}
		lo.SetTime(time.UnixMilli((closedEpochIdx+1)*epochDur - 1))
		if mkt.orderAtOrAfterSuspendBoundary(lo) {
			t.Fatal("order before final boundary rejected")
		}
		lo.SetTime(time.UnixMilli((closedEpochIdx + 1) * epochDur))
		if !mkt.orderAtOrAfterSuspendBoundary(lo) {
			t.Fatal("order at final boundary allowed")
		}
	})

	for _, tt := range []struct {
		name   string
		mutate func(*fixture)
	}{
		{
			name: "wrong current epoch",
			mutate: func(f *fixture) {
				f.event.ClosedEpochIdx++
				f.event.OpenedEpochIdx++
			},
		},
		{
			name: "final close without scheduled suspension",
			mutate: func(f *fixture) {
				f.event.OpenedEpochIdx = 0
			},
		},
		{
			name: "missing book",
			mutate: func(f *fixture) {
				delete(f.bookRouter.books, f.event.Market)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, db.MarketPendingNone)
			tt.mutate(f)
			if err := f.applyErr(t, f.event); err == nil {
				t.Fatal("expected validation error")
			}
			if len(f.storage.advanceEpochEvents) != 0 {
				t.Fatal("invalid event reached storage")
			}
			requireEpochState(t, f, closedEpochIdx, closedEpochIdx, append(f.closed, f.next...), nil)
		})
	}
}

func newLifecycleCommandRig(t *testing.T) (*marketEventRig, *mesh.Service) {
	t.Helper()
	rig := newMarketEventRig(t)
	t.Cleanup(rig.cleanup)
	svc, err := mesh.NewService(&mesh.ServiceConfig{
		EventLogReader: emptyEventLogReader{},
		OnHalt:         func(error) {},
		Commands:       LifecycleCommands(map[string]*Market{rig.mkt.name: rig.mkt}),
		Events:         rig.events,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rig, svc
}

func TestScheduleSuspendCommand(t *testing.T) {
	const current, epochDur int64 = 40, 500
	for _, tc := range []struct {
		name          string
		activeEpoch   int64
		pendingEpoch  int64
		retainQueue   bool
		unknownMarket bool
		asSoonAs      time.Time
		persistBook   bool
		wantEpoch     int64
		wantErr       string
	}{
		{
			name: "past epoch", activeEpoch: current,
			asSoonAs: time.UnixMilli((current - 1) * epochDur), persistBook: true,
			wantEpoch: current + 1,
		},
		{
			name: "future epoch boundary", activeEpoch: current,
			asSoonAs: time.UnixMilli((current + 4) * epochDur), persistBook: true,
			wantEpoch: current + 3,
		},
		{
			name: "inside future epoch", activeEpoch: current,
			asSoonAs: time.UnixMilli((current+4)*epochDur + 1), persistBook: true,
			wantEpoch: current + 4,
		},
		{
			name: "reschedule earlier without keeping the book", activeEpoch: current, pendingEpoch: current + 3,
			wantEpoch: current + 1,
		},
		{
			name: "final epoch already open", activeEpoch: current, pendingEpoch: current,
			wantErr: "is already closing",
		},
		{
			name:    "no epoch",
			wantErr: "without an active epoch",
		},
		{
			name: "stopped worker with retained epoch", retainQueue: true,
			asSoonAs: time.UnixMilli((current + 4) * epochDur),
			wantErr:  "without an active epoch",
		},
		{
			name: "unknown market", activeEpoch: current, unknownMarket: true,
			wantErr: "unknown market",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig, svc := newLifecycleCommandRig(t)
			mkt := rig.mkt
			row := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone, current, epochDur)
			// Create the live epoch queues before adding a pending suspension.
			setTestMarketLifecycle(mkt, rig.storage, row)
			if tc.pendingEpoch != 0 {
				persist := true
				row.PendingAction = db.MarketPendingSuspend
				row.PendingEpochIdx, row.FinalEpochIdx = tc.pendingEpoch, tc.pendingEpoch
				row.PendingEpochDur, row.FinalEpochDur = epochDur, epochDur
				row.PersistBook = &persist
				setTestMarketLifecycle(mkt, rig.storage, row)
			}
			mkt.activeEpochIdx = tc.activeEpoch
			if tc.activeEpoch == 0 {
				mkt.running.Store(false)
				if !tc.retainQueue {
					mkt.currentEpoch, mkt.nextEpoch = nil, nil
				}
			}
			marketName := mkt.name
			if tc.unknownMarket {
				marketName = "nope_btc"
			}

			susp, err := ExecuteScheduleSuspend(context.Background(), svc, marketName, tc.asSoonAs, tc.persistBook)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("suspend error = %v, want %q", err, tc.wantErr)
				}
				if len(rig.storage.marketSuspendScheduledUpdates) != 0 {
					t.Fatal("rejected schedule reached storage")
				}
				if !reflect.DeepEqual(rig.storage.lifecycle, row) {
					t.Fatalf("rejected schedule changed stored lifecycle: got %+v, want %+v", rig.storage.lifecycle, row)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantEnd := time.UnixMilli((tc.wantEpoch + 1) * epochDur)
			if susp.Idx != tc.wantEpoch || !susp.End.Equal(wantEnd) {
				t.Fatalf("suspend = %+v, want epoch %d ending %v", susp, tc.wantEpoch, wantEnd)
			}
			if len(rig.storage.marketSuspendScheduledUpdates) != 1 {
				t.Fatal("expected one scheduling update")
			}
			update := rig.storage.marketSuspendScheduledUpdates[0]
			if update.Market != mkt.name ||
				update.FinalEpochIdx != tc.wantEpoch || update.EpochDur != epochDur || update.PersistBook != tc.persistBook {
				t.Fatalf("unexpected scheduling update: %+v", update)
			}
			stored := rig.storage.lifecycle
			if stored.PendingEpochIdx != tc.wantEpoch || stored.FinalEpochIdx != tc.wantEpoch ||
				stored.PersistBook == nil || *stored.PersistBook != tc.persistBook {
				t.Fatalf("unexpected stored suspension: %+v", stored)
			}
		})
	}
}

func TestScheduleResumeCommand(t *testing.T) {
	const epochDur int64 = 500
	futureEpoch := time.Now().Add(time.Hour).UnixMilli() / epochDur
	for _, tc := range []struct {
		name      string
		state     db.MarketState
		asSoonAs  time.Time
		wantEpoch int64
		wantErr   string
	}{
		{
			name: "suspended market", state: db.MarketStateSuspended,
			asSoonAs: time.UnixMilli(futureEpoch * epochDur), wantEpoch: futureEpoch + 1,
		},
		{
			name: "already running", state: db.MarketStateRunning,
			wantErr: "unable to resume market",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig, svc := newLifecycleCommandRig(t)
			mkt := rig.mkt
			setTestMarketLifecycle(mkt, rig.storage, seedLifecycleRow(tc.state, db.MarketPendingNone, 40, epochDur))
			startEpoch, startTime, err := ExecuteScheduleResume(context.Background(), svc, mkt.name, tc.asSoonAs)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resume error = %v, want %q", err, tc.wantErr)
				}
				if len(rig.storage.marketResumeScheduledUpdates) != 0 {
					t.Fatal("rejected resume reached storage")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantTime := time.UnixMilli(tc.wantEpoch * epochDur)
			if startEpoch != tc.wantEpoch || !startTime.Equal(wantTime) {
				t.Fatalf("resume = %d %v, want %d %v", startEpoch, startTime, tc.wantEpoch, wantTime)
			}
			if len(rig.storage.marketResumeScheduledUpdates) != 1 {
				t.Fatal("expected one scheduling update")
			}
			update := rig.storage.marketResumeScheduledUpdates[0]
			if update.Market != mkt.name ||
				update.StartEpochIdx != tc.wantEpoch || update.EpochDur != epochDur {
				t.Fatalf("unexpected scheduling update: %+v", update)
			}
		})
	}
}

func TestExecuteLifecycleCommand(t *testing.T) {
	response, err := msgjson.NewResponse(1, scheduleSuspendResult{EpochIdx: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("response through callback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		requests := make(chan mesh.CommandRequest, 1)
		execute := func(_ context.Context, req mesh.CommandRequest) *msgjson.Error {
			requests <- req
			return nil
		}
		var result scheduleSuspendResult
		done := make(chan error, 1)
		go func() {
			done <- executeLifecycleCommand(ctx, execute, commandKindScheduleSuspend, scheduleSuspendRequest{}, &result)
		}()
		select {
		case req := <-requests:
			if err := req.Respond(response); err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("command was not executed")
		}
		if err := <-done; err != nil || result.EpochIdx != 42 {
			t.Fatalf("result = %+v, error = %v, want epoch 42", result, err)
		}
	})

	t.Run("execution error", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		execErr := msgjson.NewError(msgjson.RPCInternalError, "execution failed")
		execute := func(context.Context, mesh.CommandRequest) *msgjson.Error { return execErr }
		var result scheduleSuspendResult
		err := executeLifecycleCommand(ctx, execute, commandKindScheduleSuspend, scheduleSuspendRequest{}, &result)
		if !errors.Is(err, execErr) || result.EpochIdx != 0 {
			t.Fatalf("result = %+v, error = %v, want execution error", result, err)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		execute := func(context.Context, mesh.CommandRequest) *msgjson.Error {
			cancel()
			return nil
		}
		var result scheduleSuspendResult
		err := executeLifecycleCommand(ctx, execute, commandKindScheduleSuspend, scheduleSuspendRequest{}, &result)
		if !errors.Is(err, context.Canceled) || result.EpochIdx != 0 {
			t.Fatalf("result = %+v, error = %v, want cancellation", result, err)
		}
	})

	t.Run("response before cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		execute := func(_ context.Context, req mesh.CommandRequest) *msgjson.Error {
			_ = req.Respond(response)
			cancel()
			return nil
		}
		var result scheduleSuspendResult
		err := executeLifecycleCommand(ctx, execute, commandKindScheduleSuspend, scheduleSuspendRequest{}, &result)
		if err != nil || result.EpochIdx != 42 {
			t.Fatalf("result = %+v, error = %v, want epoch 42", result, err)
		}
	})
}

func TestSubmitMarketSuspend(t *testing.T) {
	const finalEpoch int64 = 41
	for _, tc := range []struct {
		name              string
		processedEpochIdx int64
		wantErr           string
	}{
		{
			name: "final epoch still processing", processedEpochIdx: finalEpoch - 1,
			wantErr: "is not processed",
		},
		{
			name: "final epoch processed", processedEpochIdx: finalEpoch,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMarketEventRig(t)
			defer rig.cleanup()
			mkt := rig.mkt
			epochDur := int64(mkt.EpochDuration())
			row := seedLifecycleRow(db.MarketStateDraining, db.MarketPendingNone, finalEpoch, epochDur)
			row.ProcessedEpochIdx = tc.processedEpochIdx
			setTestMarketLifecycle(mkt, rig.storage, row)
			rig.useApplierMesh()

			err := mkt.submitMarketSuspend(context.Background())
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("suspend error = %v, want %q", err, tc.wantErr)
				}
				if mkt.lifecycleState != db.MarketStateDraining {
					t.Fatalf("market state = %v, want draining", mkt.lifecycleState)
				}
				if !reflect.DeepEqual(rig.storage.lifecycle, row) {
					t.Fatalf("rejected suspension changed stored lifecycle: got %+v, want %+v", rig.storage.lifecycle, row)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mkt.lifecycleState != db.MarketStateSuspended {
				t.Fatalf("market state = %v, want suspended", mkt.lifecycleState)
			}
			if mkt.suspendEpochIdx != finalEpoch {
				t.Fatalf("final epoch = %d, want %d", mkt.suspendEpochIdx, finalEpoch)
			}
			if len(rig.storage.marketSuspendedUpdates) != 1 {
				t.Fatalf("suspension updates = %d, want 1", len(rig.storage.marketSuspendedUpdates))
			}
			update := rig.storage.marketSuspendedUpdates[0]
			if update.FinalEpochIdx != finalEpoch || update.EpochDur != epochDur {
				t.Fatalf("suspension update = %+v, want final epoch %d and duration %d", update, finalEpoch, epochDur)
			}
		})
	}
}

func TestSubmitMarketResume(t *testing.T) {
	const scheduledEpochIdx, epochDur int64 = 500, 1000
	type bookedOrder struct {
		lots       uint64
		wantRevoke bool
	}
	for _, tc := range []struct {
		name               string
		requestedEpoch     int64
		configuredEpochDur int64
		configuredLotSize  uint64
		orders             []bookedOrder
		wantErr            string
	}{
		{
			name:               "resume with updated parameters",
			requestedEpoch:     scheduledEpochIdx,
			configuredEpochDur: epochDur,
			configuredLotSize:  2 * dcrLotSize,
			// Doubling the lot size invalidates the one-lot order but retains the two-lot order.
			orders: []bookedOrder{{lots: 1, wantRevoke: true}, {lots: 2}},
		},
		{
			name:               "mismatched schedule",
			requestedEpoch:     scheduledEpochIdx + 1,
			configuredEpochDur: epochDur,
			configuredLotSize:  dcrLotSize,
			wantErr:            "no matching pending resume",
		},
		{
			name:               "changed epoch duration",
			requestedEpoch:     scheduledEpochIdx,
			configuredEpochDur: 2000,
			configuredLotSize:  dcrLotSize,
			wantErr:            "revert the configured duration",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMarketEventRig(t)
			defer rig.cleanup()
			mkt := rig.mkt
			mkt.swapper = new(epochProcessedTestSwapper) // Booked orders have unspent funding.
			resumeScheduled := seedLifecycleRow(db.MarketStateSuspended, db.MarketPendingNone, 40, epochDur)
			resumeScheduled.StartEpochIdx = scheduledEpochIdx
			resumeScheduled.PendingEpochIdx = scheduledEpochIdx
			resumeScheduled.PendingEpochDur = epochDur
			resumeScheduled.PendingAction = db.MarketPendingResume
			resumeScheduled.FinalEpochIdx = 0
			resumeScheduled.FinalEpochDur = 0
			setTestMarketLifecycle(mkt, rig.storage, resumeScheduled)
			mkt.configuredParams.epochDur = tc.configuredEpochDur
			mkt.configuredParams.LotSize = tc.configuredLotSize
			meshSvc := &tMesh{events: rig.events}
			mkt.SetMeshService(meshSvc)

			var wantRevoked []order.OrderID
			for i, spec := range tc.orders {
				lo := makeLO(seller3, mkRate3(1.0, 1.2), spec.lots, order.StandingTiF)
				lo.Coins = []order.CoinID{{byte(i + 1)}}
				bookStandingOrder(t, rig, lo)
				rig.storage.bookedOrders = append(rig.storage.bookedOrders, lo)
				if spec.wantRevoke {
					wantRevoked = append(wantRevoked, lo.ID())
				}
			}

			err := mkt.submitMarketResume(context.Background(), tc.requestedEpoch, epochDur)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resume error = %v, want %q", err, tc.wantErr)
				}
				if len(meshSvc.entries) != 0 || len(rig.storage.marketResumedUpdates) != 0 {
					t.Fatalf("rejected resume produced %d event entries and %d resume updates, want none", len(meshSvc.entries), len(rig.storage.marketResumedUpdates))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(rig.storage.marketResumedUpdates) != 1 {
				t.Fatalf("resume updates = %d, want 1", len(rig.storage.marketResumedUpdates))
			}
			update := rig.storage.marketResumedUpdates[0]
			if update.StartEpochIdx != scheduledEpochIdx || update.EpochDur != epochDur {
				t.Fatalf("resume epoch = %d with duration %d, want %d with duration %d", update.StartEpochIdx, update.EpochDur, scheduledEpochIdx, epochDur)
			}
			if update.RunParams != mkt.configuredParams.MarketRunParams {
				t.Fatalf("resume parameters = %+v, want %+v", update.RunParams, mkt.configuredParams.MarketRunParams)
			}
			if len(update.ResumeRevokes) != len(wantRevoked) {
				t.Fatalf("resume revoked %d orders, want %d", len(update.ResumeRevokes), len(wantRevoked))
			}
			for _, revoke := range update.ResumeRevokes {
				if !slices.Contains(wantRevoked, revoke.Order.ID()) || revoke.Reason != meshevents.StartupOrderRevokeReasonLotSizeIncompatible {
					t.Fatalf("unexpected resume revocation: %+v", revoke)
				}
			}
		})
	}
}

func TestApplyMarketSuspendedEvent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		persistBook bool
	}{
		{name: "retain booked orders", persistBook: true},
		{name: "purge booked orders", persistBook: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMarketEventRig(t)
			defer rig.cleanup()
			mkt := rig.mkt
			const finalEpoch int64 = 40
			epochDur := int64(mkt.EpochDuration())
			row := seedLifecycleRow(db.MarketStateDraining, db.MarketPendingNone, finalEpoch, epochDur)
			row.ProcessedEpochIdx = finalEpoch
			row.PersistBook = &tc.persistBook
			setTestMarketLifecycle(mkt, rig.storage, row)

			bookedOrder := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
			bookedOrder.Coins = []order.CoinID{{0x71, 0x72}}
			bookStandingOrder(t, rig, bookedOrder)
			mkt.settling[bookedOrder.ID()] = mkt.LotSize()
			rig.bookRouter.SeedBooks()
			link := rig.subscribeBook(t)
			if !tc.persistBook {
				rig.storage.lifecyclePurgeOrders = []order.OrderID{bookedOrder.ID()}
			}

			event := &meshevents.MarketSuspendedEvent{Market: mkt.name, FinalEpochIdx: finalEpoch, EpochDur: epochDur}
			event.Timestamp = (finalEpoch + 1) * epochDur
			rig.apply(t, event)
			if mkt.lifecycleState != db.MarketStateSuspended {
				t.Fatalf("market state = %v, want suspended", mkt.lifecycleState)
			}
			if retained := mkt.book.HaveOrder(bookedOrder.ID()); retained != tc.persistBook {
				t.Fatalf("book order retained = %t, want %t", retained, tc.persistBook)
			}
			if locked := mkt.CoinLocked(bookedOrder.Base(), bookedOrder.Coins[0]); locked != tc.persistBook {
				t.Fatalf("funding locked = %t, want %t", locked, tc.persistBook)
			}
			if _, settling := mkt.settling[bookedOrder.ID()]; settling != tc.persistBook {
				t.Fatalf("settling entry retained = %t, want %t", settling, tc.persistBook)
			}
			snapshot := rig.bookRouter.msgOrderBook(rig.bookRouter.books[mkt.name])
			wantOrders := 0
			if tc.persistBook {
				wantOrders = 1
			}
			if snapshot == nil || len(snapshot.Orders) != wantOrders {
				t.Fatalf("book snapshot = %+v, want %d orders", snapshot, wantOrders)
			}
			revokes, _ := collectOwnerNotes(t, rig.auth)
			if tc.persistBook {
				if len(revokes) != 0 {
					t.Fatalf("owner revocations = %v, want none", revokes)
				}
			} else if len(revokes) != 1 || !revokes[bookedOrder.ID()] {
				t.Fatalf("owner revocations = %v, want only %v", revokes, bookedOrder.ID())
			}
			msg := link.getSend()
			if msg == nil || msg.Route != msgjson.SuspensionRoute {
				t.Fatalf("notification = %v, want suspension", msg)
			}
			var note msgjson.TradeSuspension
			if err := json.Unmarshal(msg.Payload, &note); err != nil {
				t.Fatal(err)
			}
			if note.MarketID != mkt.name || note.FinalEpoch != uint64(finalEpoch) || note.Persist != tc.persistBook {
				t.Fatalf("unexpected suspension note: %+v", note)
			}
			requireNoBookNoteFromLink(t, link)
		})
	}
}

func requireEpochNoteFromLink(t *testing.T, link *TLink, mkt *Market, ord order.Order, epochIdx int64, wantOrderType uint8) {
	t.Helper()
	note := getEpochNoteFromLink(t, link)
	if note.MarketID != mkt.name {
		t.Fatalf("epoch note market = %q, want %q", note.MarketID, mkt.name)
	}
	if note.Epoch != uint64(epochIdx) {
		t.Fatalf("epoch note epoch = %d, want %d", note.Epoch, epochIdx)
	}
	if note.Seq == 0 {
		t.Fatalf("expected non-zero epoch note sequence")
	}
	if note.OrderType != wantOrderType {
		t.Fatalf("epoch note order type = %d, want %d", note.OrderType, wantOrderType)
	}
	oid := ord.ID()
	if !bytes.Equal(note.OrderID, oid[:]) {
		t.Fatalf("epoch note order id = %x, want %x", note.OrderID, oid)
	}

	switch ord := ord.(type) {
	case *order.LimitOrder:
		if note.Rate != ord.Rate {
			t.Fatalf("limit note rate = %d, want %d", note.Rate, ord.Rate)
		}
		tif := uint8(msgjson.StandingOrderNum)
		if ord.Force == order.ImmediateTiF {
			tif = msgjson.ImmediateOrderNum
		}
		if note.TiF != tif {
			t.Fatalf("limit note tif = %d, want %d", note.TiF, tif)
		}
	case *order.CancelOrder:
		if !bytes.Equal(note.TargetID, ord.TargetOrderID[:]) {
			t.Fatalf("cancel note target = %x, want %x", note.TargetID, ord.TargetOrderID)
		}
	}
}

func requireNoBookNoteFromLink(t *testing.T, link *TLink) {
	t.Helper()
	link.mtx.Lock()
	defer link.mtx.Unlock()
	if len(link.sends) != 0 {
		t.Fatalf("unexpected book notification")
	}
}

// bookStandingOrder locks a standing order's funding coins and inserts it
// into the market book and the router's book projection, as the router's
// startup snapshot or booked-order apply would in production.
func bookStandingOrder(t *testing.T, rig *marketEventRig, lo *order.LimitOrder) {
	t.Helper()
	mkt := rig.mkt
	if !mkt.lockOrderCoins(lo) {
		t.Fatalf("failed to lock book order coins")
	}
	mkt.bookMtx.Lock()
	inserted := mkt.book.Insert(lo)
	mkt.bookMtx.Unlock()
	if !inserted {
		t.Fatalf("failed to insert book order %v", lo.ID())
	}
	rig.bookRouter.books[mkt.name].insert(lo)
}

// requireRevokedOrderGone checks that a revoked order has left the market
// book and that its funding coins are unlocked in the side-appropriate locker.
func requireRevokedOrderGone(t *testing.T, mkt *Market, lo *order.LimitOrder) {
	t.Helper()
	if mkt.book.HaveOrder(lo.ID()) {
		t.Fatalf("revoked order %v remains in market book", lo.ID())
	}
	assetID := mkt.Quote()
	if lo.Sell {
		assetID = mkt.Base()
	}
	for _, coin := range lo.Coins {
		if mkt.CoinLocked(assetID, []byte(coin)) {
			t.Fatalf("revoked order %v coin %x remains locked", lo.ID(), coin)
		}
	}
}

// setTestRunMinimumRate pins a rate floor in the market's adopted run
// parameters, the only floor live admission reads.
func setTestRunMinimumRate(t *testing.T, mkt *Market, rate uint64) {
	t.Helper()
	run := mkt.liveParams.Load()
	if run == nil {
		t.Fatalf("market has no adopted run parameters")
	}
	cpy := *run
	cpy.MinimumRate = rate
	mkt.liveParams.Store(&cpy)
}

func TestBuildEpochProcessedUpdate(t *testing.T) {
	const epochIdx, epochDur int64 = 4321, 6000
	const lastRate uint64 = 2 * dcrRateStep
	matchTime := time.UnixMilli(epochIdx*epochDur + 100)
	missRevokeTime := matchTime.Add(time.Second)

	type updateCounts struct {
		booked, partial, completed, canceled int
		failed, cancelsFailed, cancelsDone   int
		matches                              int
	}
	tests := []struct {
		name string
		kind string
		want updateCounts
	}{
		{"unmatched standing order and missed preimage", "standing", updateCounts{booked: 1}},
		{"partial maker fill and completed taker", "trade", updateCounts{partial: 1, completed: 1, matches: 1}},
		{"canceled maker and executed cancel", "cancel", updateCounts{canceled: 1, cancelsDone: 1, matches: 1}},
		{"empty epoch retains the previous rate", "empty", updateCounts{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt := &Market{name: mktName3, base: mkt3.Base, quote: mkt3.Quote,
				book: book.New(dcrLotSize, 0), matcher: matcher.New()}
			mkt.liveParams.Store(&marketRun{MarketRunParams: meshevents.MarketRunParams{LotSize: dcrLotSize}, epochDur: epochDur})

			var maker *order.LimitOrder
			if tt.kind == "trade" || tt.kind == "cancel" {
				maker = makeLO(buyer3, mkRate3(1.0, 1.2), 3, order.StandingTiF)
				maker.SetTime(time.UnixMilli(epochIdx*epochDur - 1))
				maker.AddFill(dcrLotSize)
				if !mkt.book.Insert(maker) {
					t.Fatal("insert maker")
				}
			}
			var epochOrder order.Order
			var preimage order.Preimage
			switch tt.kind {
			case "standing":
				epochOrder, preimage = makeLORevealed(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
			case "trade":
				epochOrder, preimage = makeLORevealed(seller3, maker.Rate-dcrRateStep, 1, order.ImmediateTiF)
			case "cancel":
				epochOrder, preimage = makeCORevealed(buyer3, maker.ID())
			}
			var revealed []*matcher.OrderRevealed
			var epochOrders, misses []order.Order
			var revealedIDs, missedIDs []order.OrderID
			if epochOrder != nil {
				epochOrder.SetTime(time.UnixMilli(epochIdx*epochDur + 1))
				revealed = []*matcher.OrderRevealed{{Order: epochOrder, Preimage: preimage}}
				epochOrders = append(epochOrders, epochOrder)
				revealedIDs = append(revealedIDs, epochOrder.ID())
			}
			if tt.kind == "standing" {
				missed := makeLO(seller3, mkRate3(1.2, 1.4), 1, order.StandingTiF)
				misses = []order.Order{missed}
				epochOrders = append(epochOrders, missed)
				missedIDs = []order.OrderID{missed.ID()}
			}
			cSum := matcher.CSum(epochOrders)
			event := meshevents.NewEpochProcessedEvent(mkt.name, epochIdx, epochDur,
				matchTime, 11, 22, lastRate, cSum, revealed, misses, missRevokeTime)
			mkt.bookMtx.Lock()
			result, err := mkt.buildEpochProcessedUpdate(event)
			mkt.bookMtx.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			update := result.dbUpdate
			got := updateCounts{
				booked: len(update.TradesBooked), partial: len(update.TradesPartial),
				completed: len(update.TradesCompleted), canceled: len(update.TradesCanceled),
				failed: len(update.TradesFailed), cancelsFailed: len(update.CancelsFailed),
				cancelsDone: len(update.CancelsExecuted), matches: len(update.Matches),
			}
			if got != tt.want {
				t.Fatalf("updates = %+v, want %+v", got, tt.want)
			}
			epoch := update.Epoch
			if epoch == nil || epoch.MktBase != mkt.base || epoch.MktQuote != mkt.quote ||
				epoch.Idx != epochIdx || epoch.Dur != epochDur || epoch.MatchTime != matchTime.UnixMilli() || !bytes.Equal(epoch.CSum, cSum) {
				t.Fatalf("unexpected epoch results: %+v", epoch)
			}
			if !slices.Equal(epoch.OrdersRevealed, revealedIDs) || !slices.Equal(epoch.OrdersMissed, missedIDs) {
				t.Fatalf("revealed/missed IDs = %v/%v, want %v/%v", epoch.OrdersRevealed, epoch.OrdersMissed, revealedIDs, missedIDs)
			}
			if len(update.Reveals) != len(revealed) || len(update.Misses) != len(misses) {
				t.Fatalf("reveals/misses = %d/%d, want %d/%d", len(update.Reveals), len(update.Misses), len(revealed), len(misses))
			}
			if len(revealed) != 0 && (update.Reveals[0].Order.ID() != epochOrder.ID() || update.Reveals[0].Preimage != preimage) {
				t.Fatalf("unexpected reveal: %+v", update.Reveals[0])
			}
			if len(misses) != 0 && (update.Misses[0].Order.ID() != misses[0].ID() || !update.Misses[0].RevokeTime.Equal(missRevokeTime)) {
				t.Fatalf("unexpected miss: %+v", update.Misses[0])
			}

			if tt.want.booked != 0 && update.TradesBooked[0].ID() != epochOrder.ID() {
				t.Fatal("wrong booked order")
			}
			if tt.want.partial != 0 && (update.TradesPartial[0].ID() != maker.ID() || update.TradesPartial[0].Filled() != 2*dcrLotSize) {
				t.Fatal("wrong partially filled maker or fill amount")
			}
			if tt.want.completed != 0 && (update.TradesCompleted[0].ID() != epochOrder.ID() || update.TradesCompleted[0].Trade().Filled() != dcrLotSize) {
				t.Fatal("wrong completed taker or fill amount")
			}
			if tt.want.canceled != 0 && (update.TradesCanceled[0].ID() != maker.ID() || update.CancelsExecuted[0].ID() != epochOrder.ID()) {
				t.Fatal("wrong canceled maker or executed cancel")
			}
			if tt.want.matches != 0 {
				match := update.Matches[0]
				wantQty := uint64(dcrLotSize)
				if tt.kind == "cancel" {
					wantQty = 2 * dcrLotSize
				}
				if match.Maker.ID() != maker.ID() || match.Taker.ID() != epochOrder.ID() || match.Quantity != wantQty || match.Rate != maker.Rate ||
					match.Epoch.Idx != uint64(epochIdx) || match.Epoch.Dur != uint64(epochDur) || match.FeeRateBase != 11 || match.FeeRateQuote != 22 {
					t.Fatalf("unexpected match: %+v", match)
				}
			}
			wantRate := lastRate
			var wantReport [][2]int64
			if tt.kind == "trade" {
				wantRate = maker.Rate
				wantReport = [][2]int64{{int64(maker.Rate), int64(dcrLotSize)}}
			}
			if epoch.StartRate != wantRate || epoch.EndRate != wantRate || epoch.HighRate != wantRate || epoch.LowRate != wantRate {
				t.Fatalf("epoch rates = %d/%d/%d/%d, want %d", epoch.StartRate, epoch.EndRate, epoch.HighRate, epoch.LowRate, wantRate)
			}
			if !slices.Equal(result.matchReport, wantReport) {
				t.Fatalf("match report = %v, want %v", result.matchReport, wantReport)
			}

			// Planning must neither alter the live book nor consume the revealed
			// orders' fills before they are matched against it after commit.
			wantBookSize := 0
			if maker != nil {
				wantBookSize = 1
				if !mkt.book.HaveOrder(maker.ID()) || maker.Filled() != dcrLotSize {
					t.Fatal("builder removed or changed the live maker")
				}
			}
			if mkt.book.BuyCount()+mkt.book.SellCount() != wantBookSize {
				t.Fatal("builder changed live book membership")
			}
			if len(result.revealed) != len(revealed) {
				t.Fatalf("preserved reveals = %d, want %d", len(result.revealed), len(revealed))
			}
			for _, preserved := range result.revealed {
				if trade := preserved.Order.Trade(); trade != nil && trade.Filled() != 0 {
					t.Fatal("builder changed the preserved revealed order's fill")
				}
			}
		})
	}
}

func TestApplyEpochProcessedEvent(t *testing.T) {
	const epochIdx int64 = 4321

	type fixture struct {
		*marketEventRig
		swapper *epochProcessedTestSwapper
		link    *TLink
	}
	newFixture := func(t *testing.T) *fixture {
		t.Helper()
		rig := newMarketEventRig(t)
		t.Cleanup(rig.cleanup)
		swapper := new(epochProcessedTestSwapper)
		rig.mkt.swapper = swapper
		lifecycle := seedLifecycleRow(db.MarketStateRunning, db.MarketPendingNone,
			epochIdx+1, int64(rig.mkt.EpochDuration()))
		lifecycle.ProcessedEpochIdx = epochIdx - 1
		rig.storage.lifecycle = lifecycle
		rig.mkt.epochMtx.Lock()
		rig.mkt.projectMarketLifecycleLocked(lifecycle)
		rig.mkt.epochMtx.Unlock()
		rig.bookRouter.books[rig.mkt.name].setEpoch(epochIdx + 1)
		return &fixture{marketEventRig: rig, swapper: swapper}
	}
	requireFundingLocked := func(t *testing.T, f *fixture, ord order.Order, locked bool) {
		t.Helper()
		asset := f.mkt.Quote()
		if ord.Trade().Sell {
			asset = f.mkt.Base()
		}
		for _, coin := range ord.Trade().Coins {
			if got := f.mkt.CoinLocked(asset, coin); got != locked {
				t.Fatalf("order %v coin %x locked = %v, want %v", ord.ID(), coin, got, locked)
			}
		}
	}
	bookOrder := func(t *testing.T, f *fixture, ord *order.LimitOrder) {
		t.Helper()
		ord.SetTime(time.UnixMilli(epochIdx*int64(f.mkt.EpochDuration()) - 1))
		bookStandingOrder(t, f.marketEventRig, ord)
	}
	apply := func(t *testing.T, f *fixture, revealed []*matcher.OrderRevealed, missed []order.Order) error {
		t.Helper()
		mkt := f.mkt
		epochDur := int64(mkt.EpochDuration())
		orders := make([]order.Order, 0, len(revealed)+len(missed))
		for _, ord := range revealed {
			orders = append(orders, ord.Order)
		}
		orders = append(orders, missed...)
		for i, ord := range orders {
			ord.SetTime(time.UnixMilli(epochIdx*epochDur + int64(i) + 1))
			seedEpochOrder(f.storage, ord, epochIdx, epochDur)
		}
		// Closed-epoch orders retain funding locks but are no longer in the queues.
		if err := mkt.restoreEpochState(f.storage.lifecycle); err != nil {
			t.Fatalf("restore epoch state: %v", err)
		}
		for _, ord := range orders {
			if ord.Trade() != nil {
				requireFundingLocked(t, f, ord, true)
			}
		}
		f.link = f.subscribeBook(t)
		matchTime := time.UnixMilli((epochIdx + 1) * epochDur)
		event := meshevents.NewEpochProcessedEvent(mkt.name, epochIdx, epochDur,
			matchTime, 10, 10, mkt.lastRate, matcher.CSum(orders), revealed, missed, matchTime)
		return f.applyErr(t, event)
	}
	requireApplied := func(t *testing.T, f *fixture, err error, matchSets int) {
		t.Helper()
		if err != nil {
			t.Fatalf("apply epoch_processed: %v", err)
		}
		if len(f.storage.epochProcessed) != 1 {
			t.Fatalf("epoch processed updates = %d, want 1", len(f.storage.epochProcessed))
		}
		update := f.storage.epochProcessed[0]
		if update.Epoch == nil || update.Epoch.Idx != epochIdx {
			t.Fatalf("epoch update = %+v, want idx %d", update.Epoch, epochIdx)
		}
		if f.mkt.processedEpochIdx != epochIdx || f.storage.lifecycle.ProcessedEpochIdx != epochIdx {
			t.Fatal("successful apply did not advance the processed epoch")
		}
		if len(f.swapper.tracked) != matchSets {
			t.Fatalf("tracked match sets = %d, want %d", len(f.swapper.tracked), matchSets)
		}
	}
	requireRoute := func(t *testing.T, msg *msgjson.Message, route string) {
		t.Helper()
		if msg == nil {
			t.Fatalf("missing %q notification", route)
		}
		if msg.Route != route {
			t.Fatalf("notification route = %q, want %q", msg.Route, route)
		}
	}
	requireMatchProof := func(t *testing.T, f *fixture, preimages []order.Preimage, misses []order.OrderID) {
		t.Helper()
		msg := f.link.getSend()
		requireRoute(t, msg, msgjson.MatchProofRoute)
		var note msgjson.MatchProofNote
		if err := json.Unmarshal(msg.Payload, &note); err != nil {
			t.Fatalf("match proof note: %v", err)
		}
		if note.MarketID != f.mkt.name || note.Epoch != uint64(epochIdx) {
			t.Fatalf("match proof market/epoch = %q/%d, want %q/%d",
				note.MarketID, note.Epoch, f.mkt.name, epochIdx)
		}
		if len(note.Preimages) != len(preimages) || len(note.Misses) != len(misses) {
			t.Fatalf("match proof reveals/misses = %d/%d, want %d/%d",
				len(note.Preimages), len(note.Misses), len(preimages), len(misses))
		}
		for i, pi := range preimages {
			if !bytes.Equal(note.Preimages[i], pi[:]) {
				t.Fatalf("match proof preimage = %x, want %x", note.Preimages[i], pi)
			}
		}
		for i, oid := range misses {
			if !bytes.Equal(note.Misses[i], oid[:]) {
				t.Fatalf("match proof miss = %x, want %x", note.Misses[i], oid)
			}
		}
	}
	requireNoteOrder := func(t *testing.T, f *fixture, marketID string, orderID msgjson.Bytes, ord order.Order) {
		t.Helper()
		oid := ord.ID()
		if marketID != f.mkt.name || !bytes.Equal(orderID, oid[:]) {
			t.Fatalf("book note market/order = %q/%x, want %q/%x", marketID, orderID, f.mkt.name, oid)
		}
	}
	requireNoBookSends := func(t *testing.T, f *fixture) {
		t.Helper()
		f.link.mtx.Lock()
		defer f.link.mtx.Unlock()
		if len(f.link.sends) != 0 {
			t.Fatalf("unexpected book notifications = %d", len(f.link.sends))
		}
	}
	requireNoAuthSends := func(t *testing.T, f *fixture) {
		t.Helper()
		f.auth.sendsMtx.Lock()
		defer f.auth.sendsMtx.Unlock()
		if len(f.auth.sends) != 0 {
			t.Fatalf("unexpected auth notifications = %d", len(f.auth.sends))
		}
	}
	requireEpochReport := func(t *testing.T, f *fixture) {
		t.Helper()
		requireRoute(t, f.link.getSend(), msgjson.EpochReportRoute)
		requireNoBookSends(t, f)
	}

	t.Run("books unmatched standing order", func(t *testing.T) {
		f := newFixture(t)
		ord, pi := makeLORevealed(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		ord.Coins = []order.CoinID{{0x31}}
		err := apply(t, f, []*matcher.OrderRevealed{{Order: ord, Preimage: pi}}, nil)
		requireApplied(t, f, err, 0)

		if !f.mkt.book.HaveOrder(ord.ID()) || !f.mkt.Cancelable(ord.ID()) {
			t.Fatal("standing order was not booked and made cancelable")
		}
		requireFundingLocked(t, f, ord, true)
		requireMatchProof(t, f, []order.Preimage{pi}, nil)
		note := getBookNoteFromLink(t, f.link)
		requireNoteOrder(t, f, note.MarketID, note.OrderID, ord)
		requireEpochReport(t, f)

		msg := f.auth.getSend()
		requireRoute(t, msg, msgjson.NoMatchRoute)
		var noMatch msgjson.NoMatch
		if err := json.Unmarshal(msg.Payload, &noMatch); err != nil {
			t.Fatalf("nomatch note: %v", err)
		}
		oid := ord.ID()
		if !bytes.Equal(noMatch.OrderID, oid[:]) {
			t.Fatalf("nomatch order = %x, want %x", noMatch.OrderID, oid)
		}
		requireNoAuthSends(t, f)
	})

	t.Run("partial trade updates remaining and tracks match", func(t *testing.T) {
		f := newFixture(t)
		maker := makeLO(buyer3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		maker.Coins = []order.CoinID{{0x41}}
		bookOrder(t, f, maker)
		taker, pi := makeLORevealed(seller3, maker.Rate-dcrRateStep, 1, order.ImmediateTiF)
		taker.Coins = []order.CoinID{{0x51}}
		err := apply(t, f, []*matcher.OrderRevealed{{Order: taker, Preimage: pi}}, nil)
		requireApplied(t, f, err, 1)

		if !f.mkt.book.HaveOrder(maker.ID()) || maker.Filled() != taker.Quantity {
			t.Fatal("maker was not partially filled and retained on the book")
		}
		if f.mkt.book.HaveOrder(taker.ID()) {
			t.Fatal("immediate taker was booked")
		}
		if f.mkt.settling[maker.ID()] != taker.Quantity || f.mkt.settling[taker.ID()] != taker.Quantity {
			t.Fatal("match quantities were not added to settling orders")
		}
		requireFundingLocked(t, f, maker, true)
		requireFundingLocked(t, f, taker, false)
		requireMatchProof(t, f, []order.Preimage{pi}, nil)
		note := getUpdateRemainingNoteFromLink(t, f.link)
		requireNoteOrder(t, f, note.MarketID, note.OrderID, maker)
		if note.Remaining != maker.Remaining() {
			t.Fatalf("update remaining = %d, want %d", note.Remaining, maker.Remaining())
		}
		requireEpochReport(t, f)
		requireNoAuthSends(t, f)
	})

	t.Run("cancel unbooks target with an unsettled fill", func(t *testing.T) {
		f := newFixture(t)
		target := makeLO(seller3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		target.Coins = []order.CoinID{{0x61}}
		target.AddFill(f.mkt.LotSize())
		bookOrder(t, f, target)
		f.mkt.settling[target.ID()] = f.mkt.LotSize()
		cancel, pi := makeCORevealed(seller3, target.ID())
		err := apply(t, f, []*matcher.OrderRevealed{{Order: cancel, Preimage: pi}}, nil)
		requireApplied(t, f, err, 1)

		if f.mkt.book.HaveOrder(target.ID()) || f.mkt.Cancelable(target.ID()) {
			t.Fatal("canceled target is still on the book or cancelable")
		}
		if _, found := f.mkt.settling[target.ID()]; found {
			t.Fatal("canceled target is still settling")
		}
		requireFundingLocked(t, f, target, false)
		requireMatchProof(t, f, []order.Preimage{pi}, nil)
		note := getUnbookNoteFromLink(t, f.link)
		requireNoteOrder(t, f, note.MarketID, note.OrderID, target)
		requireEpochReport(t, f)
		requireNoAuthSends(t, f)
	})

	t.Run("missing preimage", func(t *testing.T) {
		f := newFixture(t)
		missed := makeLO(seller3, mkRate3(1.0, 1.2), 1, order.StandingTiF)
		missed.Coins = []order.CoinID{{0x71}}
		err := apply(t, f, nil, []order.Order{missed})
		requireApplied(t, f, err, 0)

		if f.mkt.book.HaveOrder(missed.ID()) {
			t.Fatal("order with a missing preimage was booked")
		}
		requireFundingLocked(t, f, missed, false)
		requireMatchProof(t, f, nil, []order.OrderID{missed.ID()})
		requireEpochReport(t, f)
	})

	t.Run("storage failure leaves state unchanged", func(t *testing.T) {
		f := newFixture(t)
		maker := makeLO(buyer3, mkRate3(1.0, 1.2), 2, order.StandingTiF)
		maker.Coins = []order.CoinID{{0x41}}
		bookOrder(t, f, maker)
		taker, pi := makeLORevealed(seller3, maker.Rate-dcrRateStep, 1, order.ImmediateTiF)
		taker.Coins = []order.CoinID{{0x51}}
		missed := makeLO(seller3, maker.Rate, 1, order.StandingTiF)
		missed.Coins = []order.CoinID{{0x71}}
		f.storage.poisonEpochProcessed = true
		err := apply(t, f, []*matcher.OrderRevealed{{Order: taker, Preimage: pi}}, []order.Order{missed})
		if err == nil || !strings.Contains(err.Error(), "epoch processed storage failure") {
			t.Fatalf("apply error = %v, want storage failure", err)
		}

		if len(f.storage.epochProcessed) != 0 || len(f.swapper.tracked) != 0 {
			t.Fatal("storage failure recorded an update or tracked matches")
		}
		if f.mkt.processedEpochIdx != epochIdx-1 || f.storage.lifecycle.ProcessedEpochIdx != epochIdx-1 {
			t.Fatal("storage failure advanced the processed epoch")
		}
		if !f.mkt.book.HaveOrder(maker.ID()) || maker.Filled() != 0 {
			t.Fatal("storage failure changed the maker's book state")
		}
		if f.mkt.book.HaveOrder(taker.ID()) {
			t.Fatal("storage failure booked the taker")
		}
		if len(f.mkt.settling) != 0 {
			t.Fatalf("storage failure added settling orders: %v", f.mkt.settling)
		}
		requireFundingLocked(t, f, maker, true)
		requireFundingLocked(t, f, taker, true)
		requireFundingLocked(t, f, missed, true)
		requireNoBookSends(t, f)
		requireNoAuthSends(t, f)
	})
}

// useApplierMesh routes the market's own event submissions through the rig's
// appliers, so they run the same projection every node applies.
func (rig *marketEventRig) useApplierMesh() {
	rig.mkt.SetMeshService(&tMesh{events: rig.events})
}

func TestSubmitMarketStarted(t *testing.T) {
	type leftoverEpochOrderSpec struct {
		offset int64 // epochs relative to the current clock epoch
		cancel bool  // targets the previously seeded trade
		coin   order.CoinID
	}
	cases := []struct {
		name           string
		state          db.MarketState // zero means no stored lifecycle
		pendingAction  db.MarketPendingAction
		suspendOffset  int64
		leftoverOrders []leftoverEpochOrderSpec
		wantOpenEpoch  bool
	}{
		{
			name: "first start revokes all leftover epoch orders and opens an epoch",
			leftoverOrders: []leftoverEpochOrderSpec{
				{offset: -900, coin: order.CoinID{0x11, 0x12}},
				{offset: -900, coin: order.CoinID{0x13, 0x14}},
				{offset: -900, cancel: true},
				{offset: 0, coin: order.CoinID{0x31, 0x32}},
			},
			wantOpenEpoch: true,
		},
		{
			name:           "pending suspend past the final epoch finalizes without opening an epoch",
			state:          db.MarketStateRunning,
			pendingAction:  db.MarketPendingSuspend,
			suspendOffset:  -5,
			leftoverOrders: []leftoverEpochOrderSpec{{offset: -5, coin: order.CoinID{0x51, 0x52}}},
		},
		{
			name:           "draining restart repairs leftover orders without opening an epoch",
			state:          db.MarketStateDraining,
			suspendOffset:  -5,
			leftoverOrders: []leftoverEpochOrderSpec{{offset: -5, coin: order.CoinID{0x61, 0x62}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMarketEventRig(t)
			defer rig.cleanup()
			rig.useApplierMesh()
			mkt, storage := rig.mkt, rig.storage
			epochDur := int64(mkt.EpochDuration())
			referenceEpoch := currentMarketEpochIdx(epochDur)

			if tc.state != 0 {
				finalIdx := referenceEpoch + tc.suspendOffset
				setTestMarketLifecycle(mkt, storage, seedLifecycleRow(tc.state, tc.pendingAction, finalIdx, epochDur))
			}

			var leftover []order.Order
			var lastTrade *order.LimitOrder
			for i, spec := range tc.leftoverOrders {
				idx := referenceEpoch + spec.offset
				var ord order.Order
				if spec.cancel {
					ord = epochStampedCO(t, lastTrade.ID(), idx, epochDur, int64(i+1))
				} else {
					lastTrade = epochStampedLO(t, idx, epochDur, int64(i+1), spec.coin)
					ord = lastTrade
				}
				seedEpochOrder(storage, ord, idx, epochDur)
				leftover = append(leftover, ord)
			}

			startupEpoch, err := mkt.submitMarketStarted(context.Background())
			if err != nil {
				t.Fatalf("submitMarketStarted error: %v", err)
			}
			if tc.wantOpenEpoch && startupEpoch == 0 {
				t.Fatalf("startup epoch = 0, want nonzero")
			}
			if len(storage.marketStartedUpdates) != 1 {
				t.Fatalf("market started updates = %d, want 1", len(storage.marketStartedUpdates))
			}
			requireEpochRevokeSet(t, storage.marketStartedUpdates[0], leftover)
			if len(storage.epochProcessed) != 0 {
				t.Fatalf("startup processed %d epochs, want none", len(storage.epochProcessed))
			}

			mkt.epochMtx.RLock()
			currentEpoch := mkt.currentEpoch
			mkt.epochMtx.RUnlock()
			if (currentEpoch != nil) != tc.wantOpenEpoch {
				t.Fatalf("current epoch = %v, want opened %t", currentEpoch, tc.wantOpenEpoch)
			}
			if got := mkt.isDraining(); got != !tc.wantOpenEpoch {
				t.Fatalf("finalizing suspend = %t, want %t", got, !tc.wantOpenEpoch)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		state   db.MarketState
		pending db.MarketPendingAction
	}{
		{"due suspension keeps stored parameters", db.MarketStateRunning, db.MarketPendingSuspend},
		{"draining keeps stored parameters", db.MarketStateDraining, db.MarketPendingNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &TArchivist{}
			balancer := newTBalancer()
			writer := test.RandomWriter()
			writer.Market = &test.Market{Base: assetETH.ID, Quote: assetBTC.ID, LotSize: dcrLotSize}
			writer.Sell = true
			addr := test.RandomAddress()
			for lots := uint64(1); lots <= 2; lots++ {
				lo := makeLO(writer, btcRateStep, lots, order.StandingTiF)
				lo.Coins = []order.CoinID{[]byte(addr)}
				storage.BookOrder(lo)
			}
			rig := newMarketEventRig(t, storage, balancer, [2]*asset.BackedAsset{assetETH, assetBTC})
			defer rig.cleanup()
			rig.useApplierMesh()
			mkt := rig.mkt
			live := *mkt.liveParams.Load()
			finalEpoch := currentMarketEpochIdx(live.epochDur) - 5
			row := seedLifecycleRow(tc.state, tc.pending, finalEpoch, live.epochDur)
			row.Market = mkt.name
			row.RunParams = live.MarketRunParams
			setTestMarketLifecycle(mkt, storage, row)
			mkt.configuredParams.LotSize *= 2
			if tc.state == db.MarketStateDraining {
				mkt.configuredParams.epochDur *= 2
			}
			if got := mkt.startParams(time.Now()); got != live {
				t.Fatal("startup did not select the stored trading parameters")
			}

			if _, err := mkt.submitMarketStarted(context.Background()); err != nil {
				t.Fatalf("submitMarketStarted: %v", err)
			}
			if len(storage.marketStartedUpdates) != 1 {
				t.Fatalf("startup updates = %d, want 1", len(storage.marketStartedUpdates))
			}
			update := storage.marketStartedUpdates[0]
			if update.RunParams != live.MarketRunParams || update.EpochDur != live.epochDur {
				t.Fatal("startup did not keep the stored trading parameters")
			}
			_, buys, sells := mkt.Book()
			if len(update.BookedRevokes) != 0 || len(buys)+len(sells) != 2 {
				t.Fatal("startup revoked orders using the configured lot size")
			}
			if mkt.lifecycleState != db.MarketStateDraining || mkt.currentEpoch != nil {
				t.Fatal("startup reopened trading while completing suspension")
			}
		})
	}
}

// requireEpochRevokeSet checks that the event's epoch revokes cover exactly
// the given orders, sorted by ID, all with the penalty-free reason.
func requireEpochRevokeSet(t *testing.T, update *db.MarketStartedUpdate, want []order.Order) {
	t.Helper()
	if len(update.EpochRevokes) != len(want) {
		t.Fatalf("epoch revokes = %d, want %d", len(update.EpochRevokes), len(want))
	}
	wantIDs := make([]order.OrderID, len(want))
	for i, ord := range want {
		wantIDs[i] = ord.ID()
	}
	sort.Slice(wantIDs, func(i, j int) bool {
		return bytes.Compare(wantIDs[i][:], wantIDs[j][:]) < 0
	})
	for i, revoke := range update.EpochRevokes {
		if revoke.Order.ID() != wantIDs[i] {
			t.Fatalf("epoch revoke %d ID = %v, want %v", i, revoke.Order.ID(), wantIDs[i])
		}
		if revoke.Reason != meshevents.StartupOrderRevokeReasonEpochAbandoned {
			t.Fatalf("epoch revoke %d reason = %d, want %d", i,
				revoke.Reason, meshevents.StartupOrderRevokeReasonEpochAbandoned)
		}
	}
}

func (ta *TArchivist) FlushBook(base, quote uint32) (sells, buys []order.OrderID, err error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	for _, lo := range ta.bookedOrders {
		if lo.Sell {
			sells = append(sells, lo.ID())
		} else {
			buys = append(buys, lo.ID())
		}
	}
	ta.bookedOrders = nil
	return
}
func (ta *TArchivist) NewArchivedCancel(ord *order.CancelOrder, epochID, epochDur int64) error {
	if ta.archivedCancels != nil {
		ta.archivedCancels = append(ta.archivedCancels, ord)
	}
	return nil
}
func (ta *TArchivist) ActiveOrderCoins(base, quote uint32) (baseCoins, quoteCoins map[order.OrderID][]order.CoinID, err error) {
	return make(map[order.OrderID][]order.CoinID), make(map[order.OrderID][]order.CoinID), nil
}
func (ta *TArchivist) UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []order.OrderStatus, error) {
	return nil, nil, errors.New("boom")
}

func (ta *TArchivist) OrderWithCommit(ctx context.Context, commit order.Commitment) (found bool, oid order.OrderID, err error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if commit == ta.commitForKnownOrder {
		return true, ta.orderWithKnownCommit, nil
	}
	return
}
func (ta *TArchivist) CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error) {
	return nil, nil, nil
}
func (ta *TArchivist) ExecutedCancelsForUser(aid account.AccountID, N int) ([]*db.CancelRecord, error) {
	return nil, nil
}

func (ta *TArchivist) NewEpochOrder(ord order.Order, epochIdx, epochDur int64, epochGap int32) error {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.poisonEpochOrder != nil && ord.ID() == ta.poisonEpochOrder.ID() {
		return errors.New("barf")
	}
	return nil
}
func (ta *TArchivist) StorePreimage(ord order.Order, pi order.Preimage) error { return nil }

func (ta *TArchivist) InsertEpoch(ed *db.EpochResults) error {
	if ta.epochInserted != nil { // the test wants to know
		ta.epochInserted <- struct{}{}
	}
	return nil
}

func (ta *TArchivist) ExecuteOrder(ord order.Order) error { return nil }
func (ta *TArchivist) CancelOrder(lo *order.LimitOrder) error {
	if ta.canceledOrders != nil {
		ta.canceledOrders = append(ta.canceledOrders, lo)
	}
	return nil
}
func (ta *TArchivist) RevokeOrder(ord order.Order) (order.OrderID, time.Time, error) {
	ta.revoked = ord
	return ord.ID(), time.Now(), nil
}
func (ta *TArchivist) RevokeOrderUncounted(order.Order) (order.OrderID, time.Time, error) {
	return order.OrderID{}, time.Now(), nil
}
func (ta *TArchivist) SetOrderCompleteTime(ord order.Order, compTime int64) error { return nil }
func (ta *TArchivist) FailCancelOrder(*order.CancelOrder) error                   { return nil }
func (ta *TArchivist) UpdateOrderFilled(*order.LimitOrder) error                  { return nil }
func (ta *TArchivist) UpdateOrderStatus(order.Order, order.OrderStatus) error     { return nil }

func (ta *TArchivist) InsertMatch(match *order.Match) error { return nil }
func (ta *TArchivist) MatchByID(mid order.MatchID, base, quote uint32) (*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) UserMatches(aid account.AccountID, base, quote uint32) ([]*db.MatchData, error) {
	return nil, nil
}

func (ta *TArchivist) PreimageStats(user account.AccountID, lastN int) ([]*db.PreimageResult, error) {
	return nil, nil
}
func (ta *TArchivist) ForgiveMatchFail(order.MatchID) (bool, error) { return false, nil }

func (ta *TArchivist) SwapData(mid db.MarketMatchID) (order.MatchStatus, *db.SwapData, error) {
	return 0, nil, nil
}
func (ta *TArchivist) SaveMatchAckSigA(mid db.MarketMatchID, sig []byte) error   { return nil }
func (ta *TArchivist) SaveMatchAckSigB(mid db.MarketMatchID, sig []byte) error   { return nil }
func (ta *TArchivist) SaveMatchAckAddrA(mid db.MarketMatchID, addr string) error { return nil }
func (ta *TArchivist) SaveMatchAckAddrB(mid db.MarketMatchID, addr string) error { return nil }

// Contract data.
func (ta *TArchivist) SaveContractA(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SaveAuditAckSigB(mid db.MarketMatchID, sig []byte) error { return nil }
func (ta *TArchivist) SaveContractB(mid db.MarketMatchID, contract []byte, coinID []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SaveAuditAckSigA(mid db.MarketMatchID, sig []byte) error { return nil }

// Redeem data.
func (ta *TArchivist) SaveRedeemA(mid db.MarketMatchID, coinID, secret []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SaveRedeemAckSigB(mid db.MarketMatchID, sig []byte) error {
	return nil
}
func (ta *TArchivist) SaveRedeemB(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SetMatchInactive(mid db.MarketMatchID, forgive bool) error { return nil }
