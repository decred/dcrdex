// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/swap"
)

type TArchivist struct {
	mtx                  sync.Mutex
	poisonEpochOrder     order.Order
	orderWithKnownCommit order.OrderID
	commitForKnownOrder  order.Commitment
	bookedOrders         []*order.LimitOrder
	epochInserted        chan struct{}
}

func (ta *TArchivist) LastErr() error { return nil }
func (ta *TArchivist) Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	return nil, order.OrderStatusUnknown, errors.New("boom")
}
func (ta *TArchivist) BookOrders(base, quote uint32) ([]*order.LimitOrder, error) {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	return ta.bookedOrders, nil
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
func (ta *TArchivist) failOnCommitWithOrder(ord order.Order) {
	ta.mtx.Lock()
	ta.commitForKnownOrder = ord.Commitment()
	ta.orderWithKnownCommit = ord.ID()
	ta.mtx.Unlock()
}
func (ta *TArchivist) CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error) {
	return nil, nil, nil
}
func (ta *TArchivist) ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error) {
	return nil, nil, nil, nil
}
func (ta *TArchivist) OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error) {
	return order.OrderStatusUnknown, order.UnknownOrderType, -1, errors.New("boom")
}
func (ta *TArchivist) NewEpochOrder(ord order.Order, epochIdx, epochDur int64) error {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.poisonEpochOrder != nil && ord.ID() == ta.poisonEpochOrder.ID() {
		return errors.New("barf")
	}
	return nil
}
func (ta *TArchivist) StorePreimage(ord order.Order, pi order.Preimage) error { return nil }
func (ta *TArchivist) failOnEpochOrder(ord order.Order) {
	ta.mtx.Lock()
	ta.poisonEpochOrder = ord
	ta.mtx.Unlock()
}
func (ta *TArchivist) InsertEpoch(ed *db.EpochResults) error {
	if ta.epochInserted != nil { // the test wants to know
		ta.epochInserted <- struct{}{}
	}
	return nil
}
func (ta *TArchivist) BookOrder(lo *order.LimitOrder) error {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	// Note that the other storage functions like ExecuteOrder and CancelOrder
	// do not change this order slice.
	ta.bookedOrders = append(ta.bookedOrders, lo)
	return nil
}
func (ta *TArchivist) ExecuteOrder(ord order.Order) error  { return nil }
func (ta *TArchivist) CancelOrder(*order.LimitOrder) error { return nil }
func (ta *TArchivist) RevokeOrder(order.Order) (order.OrderID, time.Time, error) {
	return order.OrderID{}, time.Now(), nil
}
func (ta *TArchivist) SetOrderCompleteTime(ord order.Order, compTime int64) error { return nil }
func (ta *TArchivist) FailCancelOrder(*order.CancelOrder) error                   { return nil }
func (ta *TArchivist) UpdateOrderFilled(*order.LimitOrder) error                  { return nil }
func (ta *TArchivist) UpdateOrderStatus(order.Order, order.OrderStatus) error     { return nil }
func (ta *TArchivist) InsertMatch(match *order.Match) error                       { return nil }
func (ta *TArchivist) MatchByID(mid order.MatchID, base, quote uint32) (*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) UserMatches(aid account.AccountID, base, quote uint32) ([]*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) ActiveMatches(account.AccountID) ([]*order.UserMatch, error) {
	return nil, nil
}
func (ta *TArchivist) SwapData(mid db.MarketMatchID) (order.MatchStatus, *db.SwapData, error) {
	return 0, nil, nil
}
func (ta *TArchivist) SaveMatchAckSigA(mid db.MarketMatchID, sig []byte) error { return nil }
func (ta *TArchivist) SaveMatchAckSigB(mid db.MarketMatchID, sig []byte) error { return nil }

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
func (ta *TArchivist) SaveRedeemAckSigA(mid db.MarketMatchID, sig []byte) error {
	return nil
}
func (ta *TArchivist) SetMatchInactive(mid db.MarketMatchID) error  { return nil }
func (ta *TArchivist) CloseAccount(account.AccountID, account.Rule) {}
func (ta *TArchivist) Account(account.AccountID) (acct *account.Account, paid, open bool) {
	return nil, false, false
}
func (ta *TArchivist) CreateAccount(*account.Account) (string, error)   { return "", nil }
func (ta *TArchivist) AccountRegAddr(account.AccountID) (string, error) { return "", nil }
func (ta *TArchivist) PayAccount(account.AccountID, []byte) error       { return nil }
func (ta *TArchivist) Close() error                                     { return nil }

func randomOrderID() order.OrderID {
	pk := randomBytes(order.OrderIDSize)
	var id order.OrderID
	copy(id[:], pk)
	return id
}

func newTestMarket(stor ...*TArchivist) (*Market, *TArchivist, *TAuth, func(), error) {
	// The DEX will make MasterCoinLockers for each asset.
	masterLockerBase := coinlock.NewMasterCoinLocker()
	bookLockerBase := masterLockerBase.Book()
	swapLockerBase := masterLockerBase.Swap()

	masterLockerQuote := coinlock.NewMasterCoinLocker()
	bookLockerQuote := masterLockerQuote.Book()
	swapLockerQuote := masterLockerQuote.Swap()

	epochDurationMSec := uint64(500) // 0.5 sec epoch duration
	storage := &TArchivist{}
	if len(stor) > 0 {
		storage = stor[0]
	}
	authMgr := &TAuth{
		sends:            make([]*msgjson.Message, 0),
		preimagesByMsgID: make(map[uint64]order.Preimage),
		preimagesByOrdID: make(map[string]order.Preimage),
	}
	swapperCfg := &swap.Config{
		Assets: map[uint32]*swap.LockableAsset{
			assetDCR.ID: {BackedAsset: assetDCR, CoinLocker: swapLockerBase},
			assetBTC.ID: {BackedAsset: assetBTC, CoinLocker: swapLockerQuote},
		},
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: 10 * time.Second,
	}
	swapper := swap.NewSwapper(swapperCfg)
	ssw := dex.NewStartStopWaiter(swapper)
	ssw.Start(testCtx)
	cleanup := func() {
		ssw.Stop()
		ssw.WaitForShutdown()
	}

	mbBuffer := 1.1
	mktInfo, err := dex.NewMarketInfo(assetDCR.ID, assetBTC.ID,
		assetDCR.LotSize, epochDurationMSec, mbBuffer)
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("dex.NewMarketInfo() failure: %v", err)
	}

	mkt, err := NewMarket(mktInfo, storage, swapper, authMgr,
		bookLockerBase, bookLockerQuote)
	if err != nil {
		return nil, nil, nil, func() {}, fmt.Errorf("Failed to create test market: %v", err)
	}
	return mkt, storage, authMgr, cleanup, nil
}

func TestMarket_NewMarket_BookOrders(t *testing.T) {
	mkt, storage, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}

	// With no book orders in the DB, the market should have an empty book after
	// construction.
	_, buys, sells := mkt.Book()
	if len(buys) > 0 || len(sells) > 0 {
		cleanup()
		t.Fatalf("Fresh market had %d buys and %d sells, expected none.",
			len(buys), len(sells))
	}
	cleanup()

	// Now store some book orders to verify NewMarket sees them.
	loBuy := makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
	loSell := makeLO(seller3, mkRate3(1.0, 1.2), randLots(10), order.StandingTiF)
	_ = storage.BookOrder(loBuy)  // the stub does not error
	_ = storage.BookOrder(loSell) // the stub does not error

	mkt, storage, _, cleanup, err = newTestMarket(storage)
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	_, buys, sells = mkt.Book()
	if len(buys) != 1 || len(sells) != 1 {
		t.Fatalf("Fresh market had %d buys and %d sells, expected 1 buy, 1 sell.",
			len(buys), len(sells))
	}
	if buys[0].ID() != loBuy.ID() {
		t.Errorf("booked buy order has incorrect ID. Expected %v, got %v",
			loBuy.ID(), buys[0].ID())
	}
	if sells[0].ID() != loSell.ID() {
		t.Errorf("booked sell order has incorrect ID. Expected %v, got %v",
			loSell.ID(), sells[0].ID())
	}

	// PurgeBook should clear the in memory book and those in storage.
	mkt.PurgeBook()
	_, buys, sells = mkt.Book()
	if len(buys) > 0 || len(sells) > 0 {
		t.Fatalf("purged market had %d buys and %d sells, expected none.",
			len(buys), len(sells))
	}

	los, _ := storage.BookOrders(mkt.marketInfo.Base, mkt.marketInfo.Quote)
	if len(los) != 0 {
		t.Errorf("stored book orders were not flushed")
	}

}

func TestMarket_Book(t *testing.T) {
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	defer cleanup()

	rand.Seed(0)

	// Fill the book.
	for i := 0; i < 8; i++ {
		// Buys
		lo := makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
		//t.Logf("Inserted buy order  (rate=%10d, quantity=%d) onto book.", lo.Rate, lo.Quantity)

		// Sells
		lo = makeLO(seller3, mkRate3(1.0, 1.2), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
		//t.Logf("Inserted sell order (rate=%10d, quantity=%d) onto book.", lo.Rate, lo.Quantity)
	}

	bestBuy, bestSell := mkt.book.Best()

	marketRate := mkt.MidGap()
	mktRateWant := (bestBuy.Rate + bestSell.Rate) / 2
	if marketRate != mktRateWant {
		t.Errorf("Market rate expected %d, got %d", mktRateWant, mktRateWant)
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

func TestMarket_Suspend(t *testing.T) {
	// Create the market.
	mkt, _, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		cleanup()
		return
	}
	defer cleanup()
	epochDurationMSec := int64(mkt.EpochDuration())

	// Suspend before market start.
	finalIdx, _ := mkt.Suspend(time.Now(), false)
	if finalIdx != -1 {
		t.Fatalf("not running market should not allow suspend")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startEpochIdx := 2 + encode.UnixMilli(time.Now())/epochDurationMSec
	startEpochTime := encode.UnixTimeMilli(startEpochIdx * epochDurationMSec)
	midPrevEpochTime := startEpochTime.Add(time.Duration(-epochDurationMSec/2) * time.Millisecond)

	// ~----|-------|-------|-------|
	// ^now ^prev   ^start  ^next

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	var wantClosedFeed bool
	feed := mkt.OrderFeed()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range feed {
		}
		if !wantClosedFeed {
			t.Errorf("order feed should not be closed")
		}
	}()

	// Wait until half way through the epoch prior to start, when we know Run is
	// running but the market hasn't started yet.
	<-time.After(time.Until(midPrevEpochTime))

	// This tests the case where m.activeEpochIdx == 0 but start is scheduled.
	// The suspend (final) epoch should be the one just prior to startEpochIdx.
	persist := true
	finalIdx, finalTime := mkt.Suspend(time.Now(), persist)
	if finalIdx != startEpochIdx-1 {
		t.Fatalf("finalIdx = %d, wanted %d", finalIdx, startEpochIdx-1)
	}
	if !startEpochTime.Equal(finalTime) {
		t.Errorf("got finalTime = %v, wanted %v", finalTime, startEpochTime)
	}

	if mkt.suspendEpochIdx != finalIdx {
		t.Errorf("got suspendEpochIdx = %d, wanted = %d", mkt.suspendEpochIdx, finalIdx)
	}

	// Set a new suspend time, in the future this time.
	nextEpochIdx := startEpochIdx + 1
	nextEpochTime := encode.UnixTimeMilli(nextEpochIdx * epochDurationMSec)

	// Just before second epoch start.
	finalIdx, finalTime = mkt.Suspend(nextEpochTime.Add(-1*time.Millisecond), persist)
	if finalIdx != nextEpochIdx-1 {
		t.Fatalf("finalIdx = %d, wanted %d", finalIdx, nextEpochIdx-1)
	}
	if !nextEpochTime.Equal(finalTime) {
		t.Errorf("got finalTime = %v, wanted %v", finalTime, nextEpochTime)
	}

	if mkt.suspendEpochIdx != finalIdx {
		t.Errorf("got suspendEpochIdx = %d, wanted = %d", mkt.suspendEpochIdx, finalIdx)
	}

	// Exactly at second epoch start, with same result.
	wantClosedFeed = true // we intend to have this suspend happen
	finalIdx, finalTime = mkt.Suspend(nextEpochTime, persist)
	if finalIdx != nextEpochIdx-1 {
		t.Fatalf("finalIdx = %d, wanted %d", finalIdx, nextEpochIdx-1)
	}
	if !nextEpochTime.Equal(finalTime) {
		t.Errorf("got finalTime = %v, wanted %v", finalTime, nextEpochTime)
	}

	if mkt.suspendEpochIdx != finalIdx {
		t.Errorf("got suspendEpochIdx = %d, wanted = %d", mkt.suspendEpochIdx, finalIdx)
	}

	mkt.waitForEpochOpen()

	// should be running
	if !mkt.Running() {
		t.Fatal("the market should have be running")
	}

	// Wait until after suspend time.
	<-time.After(time.Until(finalTime.Add(20 * time.Millisecond)))

	// should be stopped
	if mkt.Running() {
		t.Fatal("the market should have been suspended")
	}

	wg.Wait()

	// Start up again (consumer resumes the Market manually)
	startEpochIdx = 1 + encode.UnixMilli(time.Now())/epochDurationMSec
	startEpochTime = encode.UnixTimeMilli(startEpochIdx * epochDurationMSec)

	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	feed = mkt.OrderFeed()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range feed {
		}
		if !wantClosedFeed {
			t.Errorf("order feed should not be closed")
		}
	}()

	mkt.waitForEpochOpen()

	// should be running
	if !mkt.Running() {
		t.Fatal("the market should have be running")
	}

	// Suspend asap.
	wantClosedFeed = true // allow the feed receiver goroutine to return w/o error
	_, finalTime = mkt.SuspendASAP(persist)
	<-time.After(time.Until(finalTime.Add(40 * time.Millisecond)))

	// Should be stopped
	if mkt.Running() {
		t.Fatal("the market should have been suspended")
	}

	// Ensure the feed is closed (Run returned).
	select {
	case <-feed:
	default:
		t.Errorf("order feed should be closed")
	}

	cancel()
	wg.Wait()
}

func TestMarket_Suspend_Persist(t *testing.T) {
	// Create the market.
	mkt, storage, _, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		cleanup()
		return
	}
	defer cleanup()
	epochDurationMSec := int64(mkt.EpochDuration())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startEpochIdx := 2 + encode.UnixMilli(time.Now())/epochDurationMSec
	//startEpochTime := encode.UnixTimeMilli(startEpochIdx * epochDurationMSec)

	// ~----|-------|-------|-------|
	// ^now ^prev   ^start  ^next

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	var wantClosedFeed bool

	startFeedRecv := func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			feed := mkt.OrderFeed()
			for range feed {
			}
			if !wantClosedFeed {
				t.Errorf("order feed should not be closed")
			}
		}()
	}

	// Wait until after original start time.
	mkt.waitForEpochOpen()

	if !mkt.Running() {
		t.Fatal("the market should be running")
	}

	lo := makeLO(seller3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
	ok := mkt.book.Insert(lo)
	if !ok {
		t.Fatalf("Failed to insert an order into Market's Book")
	}
	_ = storage.BookOrder(lo)

	// Suspend asap with no resume.  The epoch with the limit order will be
	// processed and then the market will suspend.
	wantClosedFeed = true // allow the feed receiver goroutine to return w/o error
	persist := true
	_, finalTime := mkt.SuspendASAP(persist)
	<-time.After(time.Until(finalTime.Add(40 * time.Millisecond)))

	// Wait for Run to return.
	wg.Wait()

	// Should be stopped
	if mkt.Running() {
		t.Fatal("the market should have been suspended")
	}

	// Verify the order is still there.
	los, _ := storage.BookOrders(mkt.marketInfo.Base, mkt.marketInfo.Quote)
	if len(los) == 0 {
		t.Errorf("stored book orders were flushed")
	}

	_, buys, sells := mkt.Book()
	if len(buys) != 0 {
		t.Errorf("buy side of book not empty")
	}
	if len(sells) != 1 {
		t.Errorf("sell side of book not equal to 1")
	}

	// Start it up again.
	startEpochIdx = 1 + encode.UnixMilli(time.Now())/epochDurationMSec
	//startEpochTime = encode.UnixTimeMilli(startEpochIdx * epochDurationMSec)
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	startFeedRecv()

	mkt.waitForEpochOpen()

	if !mkt.Running() {
		t.Fatal("the market should be running")
	}

	persist = false
	_, finalTime = mkt.SuspendASAP(persist)
	<-time.After(time.Until(finalTime.Add(40 * time.Millisecond)))

	// Wait for Run to return.
	wg.Wait()

	// Should be stopped
	if mkt.Running() {
		t.Fatal("the market should have been suspended")
	}

	// Verify the order is gone.
	los, _ = storage.BookOrders(mkt.marketInfo.Base, mkt.marketInfo.Quote)
	if len(los) != 0 {
		t.Errorf("stored book orders were not flushed")
	}

	_, buys, sells = mkt.Book()
	if len(buys) != 0 {
		t.Errorf("buy side of book not empty")
	}
	if len(sells) != 0 {
		t.Errorf("sell side of book not empty")
	}

	cancel()
	wg.Wait()
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
	startEpochIdx := 1 + encode.UnixMilli(time.Now())/epochDurationMSec
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	// Make an order for the first epoch.
	clientTimeMSec := startEpochIdx*epochDurationMSec + 10 // 10 ms after epoch start
	lots := 10
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
				ClientTime: encode.UnixTimeMilli(clientTimeMSec),
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
	err = mkt.SubmitOrder(oRecord)
	if err == nil {
		t.Error("order submitted to stopped market")
	}
	if !errors.Is(err, ErrMarketNotRunning) {
		t.Fatalf(`expected ErrMarketNotRunning ("%v"), got "%v"`, ErrMarketNotRunning, err)
	}

	mktStatus := mkt.Status()
	if mktStatus.Running {
		t.Errorf("Market should not be running yet")
	}

	mkt.waitForEpochOpen()

	mktStatus = mkt.Status()
	if !mktStatus.Running {
		t.Errorf("Market should be running now")
	}

	// Submit again
	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err != nil {
		t.Error(err)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)...
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted

	// Submit a valid cancel order.
	loID := oRecord.order.ID()
	piCo := test.RandomPreimage()
	commit = piCo.Commit()
	cancelTime := encode.UnixMilli(time.Now())
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
				ClientTime: encode.UnixTimeMilli(cancelTime),
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
	coWrongAccount.ClientTime = encode.UnixTimeMilli(cancelTime)
	cancelMsg.Commit = commitBadCo[:]
	coRecordWrongAccount := orderRecord{
		msgID: nextMsgID(),
		req:   cancelMsg,
		order: coWrongAccount,
	}

	// Submit the invalid cancel order first because it would be caught by the
	// duplicate check if we do it after the valid one is submitted.
	storMsgPI(coRecordWrongAccount.msgID, piBadCo)
	err = mkt.SubmitOrder(&coRecordWrongAccount)
	if err == nil {
		t.Errorf("An invalid order was processed, but it should not have been.")
	} else if !errors.Is(err, ErrInvalidCancelOrder) {
		t.Errorf(`expected ErrInvalidCancelOrder ("%v"), got "%v"`, ErrInvalidCancelOrder, err)
	}

	// Valid cancel order
	storMsgPI(coRecord.msgID, piCo)
	err = mkt.SubmitOrder(&coRecord)
	if err != nil {
		t.Errorf("Failed to submit order: %v", err)
	}

	// Duplicate cancel order
	piCoDup := test.RandomPreimage()
	commit = piCoDup.Commit()
	cancelTime++
	cancelMsg.ClientTime = uint64(cancelTime)
	cancelMsg.Commit = commit[:]
	coDup := newCancel()
	coDup.Commit = commit
	coDup.ClientTime = encode.UnixTimeMilli(cancelTime)
	coRecordDup := orderRecord{
		msgID: nextMsgID(),
		req:   cancelMsg,
		order: coDup,
	}
	storMsgPI(coRecordDup.msgID, piCoDup)
	err = mkt.SubmitOrder(&coRecordDup)
	if err == nil {
		t.Errorf("An duplicate cancel order was processed, but it should not have been.")
	} else if !errors.Is(err, ErrDuplicateCancelOrder) {
		t.Errorf(`expected ErrDuplicateCancelOrder ("%v"), got "%v"`, ErrDuplicateCancelOrder, err)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)..
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted
	storage.epochInserted = nil

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
		mkt.Run(ctx) // begin on next epoch start
		// /startEpochIdx = 1 + encode.UnixMilli(time.Now())/epochDurationMSec
		//mkt.Start(ctx, startEpochIdx)
	}()
	mkt.waitForEpochOpen()

	// fresh oRecord
	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err != nil {
		t.Error(err)
	}

	// Submit another order with the same Commitment in the same Epoch.
	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err == nil {
		t.Errorf("A duplicate order was processed, but it should not have been.")
	} else if !errors.Is(err, ErrInvalidCommitment) {
		t.Errorf(`expected ErrInvalidCommitment ("%v"), got "%v"`, ErrInvalidCommitment, err)
	}

	// Send an order with a bad lot size.
	oRecord = newOR()
	oRecord.order.(*order.LimitOrder).Quantity += mkt.marketInfo.LotSize / 2
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err == nil {
		t.Errorf("An invalid order was processed, but it should not have been.")
	} else if !errors.Is(err, ErrInvalidOrder) {
		t.Errorf(`expected ErrInvalidOrder ("%v"), got "%v"`, ErrInvalidOrder, err)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)..
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted
	storage.epochInserted = nil

	// Submit an order with a Commitment known to the DB.
	// NOTE: disabled since the OrderWithCommit check in Market.processOrder is disabled too.
	// oRecord = newOR()
	// oRecord.order.SetTime(time.Now()) // This will register a different order ID with the DB in the next statement.
	// storage.failOnCommitWithOrder(oRecord.order)
	// storMsgPI(oRecord.msgID, pi)
	// err = mkt.SubmitOrder(oRecord) // Will re-stamp the order, but the commit will be the same.
	// if err == nil {
	// 	t.Errorf("A duplicate order was processed, but it should not have been.")
	// } else if !errors.Is(err, ErrInvalidCommitment) {
	// 	t.Errorf(`expected ErrInvalidCommitment ("%v"), got "%v"`, ErrInvalidCommitment, err)
	// }

	// Submit an order with a zero commit.
	oRecord = newOR()
	oRecord.order.(*order.LimitOrder).Commit = order.Commitment{}
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err == nil {
		t.Errorf("An order with a zero Commitment was processed, but it should not have been.")
	} else if !errors.Is(err, ErrInvalidCommitment) {
		t.Errorf(`expected ErrInvalidCommitment ("%v"), got "%v"`, ErrInvalidCommitment, err)
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
	if err = mkt.SubmitOrder(oRecord); !errors.Is(err, ErrInternalServer) {
		t.Errorf(`expected ErrInternalServer ("%v"), got "%v"`, ErrInternalServer, err)
	}

	// NOTE: The Market is now stopping on its own because of the storage failure.

	wg.Wait()
	cleanup()
}

func TestMarket_enqueueEpoch(t *testing.T) {
	// This tests processing of a closed epoch by epochStart (for preimage
	// collection) and processReadyEpoch (for sending the expected book and
	// unbook messages to book subscribers registered via OrderFeed) via
	// enqueueEpoch and the epochPump.

	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("Failed to create test market: %v", err)
		return
	}
	defer cleanup()

	rand.Seed(0)

	// Fill the book. Preimages not needed for these.
	for i := 0; i < 8; i++ {
		// Buys
		lo := makeLO(buyer3, mkRate3(0.8, 1.0), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
		//t.Logf("Inserted buy order (rate=%10d, quantity=%d) onto book.", lo.Rate, lo.Quantity)

		// Sells
		lo = makeLO(seller3, mkRate3(1.0, 1.2), randLots(10), order.StandingTiF)
		if !mkt.book.Insert(lo) {
			t.Fatalf("Failed to Insert order into book.")
		}
		//t.Logf("Inserted sell order (rate=%10d, quantity=%d) onto book.", lo.Rate, lo.Quantity)
	}

	bestBuy, bestSell := mkt.book.Best()
	bestBuyRate := bestBuy.Rate
	bestBuyQuant := bestBuy.Quantity * 3 // tweak for new shuffle seed without changing csum
	bestSellID := bestSell.ID()

	var epochIdx, epochDur int64 = 123413513, int64(mkt.marketInfo.EpochDuration)
	eq := NewEpoch(epochIdx, epochDur)
	eID := order.EpochID{Idx: uint64(epochIdx), Dur: uint64(epochDur)}
	lo, loPI := makeLORevealed(seller3, bestBuyRate-dcrRateStep, bestBuyQuant, order.StandingTiF)
	co, coPI := makeCORevealed(buyer3, bestSellID)
	eq.Insert(lo)
	eq.Insert(co)

	cSum, _ := hex.DecodeString("4859aa186630c2b135074037a8db42f240bbbe81c1361d8783aa605ed3f0cf90")
	seed, _ := hex.DecodeString("e061777b09170c80ce7049439bef0d69649f361ed16b500b5e53b80920813c54")
	mp := &order.MatchProof{
		Epoch:     eID,
		Preimages: []order.Preimage{loPI, coPI},
		Misses:    nil,
		CSum:      cSum,
		Seed:      seed,
	}

	eq2 := NewEpoch(epochIdx, epochDur)
	coMiss, coMissPI := makeCORevealed(buyer3, randomOrderID())
	eq2.Insert(coMiss)

	cSum2, _ := hex.DecodeString("a958ef5d180cb9dabf4d6aa120489955e6ad04bcba3414d1f4cf1725ed7e0634")
	seed2, _ := hex.DecodeString("aba75140b1f6edf26955a97e1b09d7b17abdc9c0b099fc73d9729501652fbf66")
	mp2 := &order.MatchProof{
		Epoch:     eID,
		Preimages: []order.Preimage{coMissPI},
		Misses:    nil,
		CSum:      cSum2,
		Seed:      seed2,
	}

	auth.piMtx.Lock()
	auth.preimagesByOrdID[lo.UID()] = loPI
	auth.preimagesByOrdID[co.UID()] = coPI
	auth.preimagesByOrdID[coMiss.UID()] = coMissPI
	auth.piMtx.Unlock()

	var bookSignals []*updateSignal
	var mtx sync.Mutex
	// intercept what would go to an OrderFeed() chan of Run were running.
	notifyChan := make(chan *updateSignal, 32)
	defer close(notifyChan) // quit bookSignals receiver, but not necessary
	go func() {
		for up := range notifyChan {
			//fmt.Println("received signal", up.action)
			mtx.Lock()
			bookSignals = append(bookSignals, up)
			mtx.Unlock()
		}
	}()

	var wg sync.WaitGroup
	defer wg.Wait() // wait for the following epoch pipeline goroutines

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // stop the following epoch pipeline goroutines

	// This test does not start the entire market, so manually start the epoch
	// queue pump, and a goroutine to receive ready (preimage collection
	// completed) epochs and start matching, etc.
	ePump := newEpochPump()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ePump.Run(ctx)
	}()

	goForIt := make(chan struct{}, 1)

	wg.Add(1)
	go func() {
		defer close(goForIt)
		defer wg.Done()
		for ep := range ePump.ready {
			t.Logf("processReadyEpoch: %d orders revealed\n", len(ep.ordersRevealed))

			// epochStart has completed preimage collection.
			mkt.processReadyEpoch(ep, notifyChan) // notify is async!
			goForIt <- struct{}{}
		}
	}()

	// MatchProof for empty epoch queue.
	mp0 := &order.MatchProof{
		Epoch: eID,
		// everything else is nil
	}

	tests := []struct {
		name                string
		epoch               *EpochQueue
		expectedBookSignals []*updateSignal
	}{
		{
			"ok book unbook",
			eq,
			[]*updateSignal{
				{matchProofAction, sigDataMatchProof{mp}},
				{bookAction, sigDataBookedOrder{lo, epochIdx}},
				{unbookAction, sigDataUnbookedOrder{bestBuy, epochIdx}},
				{unbookAction, sigDataUnbookedOrder{bestSell, epochIdx}},
			},
		},
		{
			"ok no matches, on book updates",
			eq2,
			[]*updateSignal{{matchProofAction, sigDataMatchProof{mp2}}},
		},
		{
			"ok empty queue",
			NewEpoch(epochIdx, epochDur),
			[]*updateSignal{{matchProofAction, sigDataMatchProof{mp0}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt.enqueueEpoch(ePump, tt.epoch)
			// Wait for processReadyEpoch, which sends on buffered (async) book
			// order feed channels.
			<-goForIt
			// Preimage collection has completed, but notifications are asynchronous.
			runtime.Gosched()                  // defer to the notify goroutine in (*Market).Run, somewhat redundant with the following sleep
			time.Sleep(250 * time.Millisecond) // let the test goroutine receive the signals on notifyChan, updating bookSignals
			// TODO: if this sleep becomes a problem, a receive(expectedNotes int) function might be needed
			mtx.Lock()
			defer mtx.Unlock() // inside this closure
			defer func() { bookSignals = []*updateSignal{} }()
			if len(bookSignals) != len(tt.expectedBookSignals) {
				t.Fatalf("expected %d book update signals, got %d",
					len(tt.expectedBookSignals), len(bookSignals))
			}
			for i, s := range bookSignals {
				exp := tt.expectedBookSignals[i]
				if exp.action != s.action {
					t.Errorf("Book signal #%d has action %d, expected %d",
						i, s.action, exp.action)
				}

				switch sigData := s.data.(type) {
				case sigDataMatchProof:
					mp := sigData.matchProof
					wantMp := exp.data.(sigDataMatchProof).matchProof
					if !bytes.Equal(wantMp.CSum, mp.CSum) {
						t.Errorf("Book signal #%d (action %v), has CSum %x, expected %x",
							i, s.action, mp.CSum, wantMp.CSum)
					}
					if !bytes.Equal(wantMp.Seed, mp.Seed) {
						t.Errorf("Book signal #%d (action %v), has Seed %x, expected %x",
							i, s.action, mp.Seed, wantMp.Seed)
					}
					if wantMp.Epoch.Idx != mp.Epoch.Idx {
						t.Errorf("Book signal #%d (action %v), has Epoch Idx %d, expected %d",
							i, s.action, mp.Epoch.Idx, wantMp.Epoch.Idx)
					}
					if wantMp.Epoch.Dur != mp.Epoch.Dur {
						t.Errorf("Book signal #%d (action %v), has Epoch Dur %d, expected %d",
							i, s.action, mp.Epoch.Dur, wantMp.Epoch.Dur)
					}
					if len(wantMp.Preimages) != len(mp.Preimages) {
						t.Errorf("Book signal #%d (action %v), has %d Preimages, expected %d",
							i, s.action, len(mp.Preimages), len(wantMp.Preimages))
						continue
					}
					for ii := range wantMp.Preimages {
						if wantMp.Preimages[ii] != mp.Preimages[ii] {
							t.Errorf("Book signal #%d (action %v), has #%d Preimage %x, expected %x",
								i, s.action, ii, mp.Preimages[ii], wantMp.Preimages[ii])
						}
					}
					if len(wantMp.Misses) != len(mp.Misses) {
						t.Errorf("Book signal #%d (action %v), has %d Misses, expected %d",
							i, s.action, len(mp.Misses), len(wantMp.Misses))
						continue
					}
					for ii := range wantMp.Misses {
						if wantMp.Misses[ii].ID() != mp.Misses[ii].ID() {
							t.Errorf("Book signal #%d (action %v), has #%d missed Order %v, expected %v",
								i, s.action, ii, mp.Misses[ii].ID(), wantMp.Misses[ii].ID())
						}
					}

				case sigDataBookedOrder:
					wantOrd := exp.data.(sigDataBookedOrder).order
					if wantOrd.ID() != sigData.order.ID() {
						t.Errorf("Book signal #%d (action %v) has order %v, expected %v",
							i, s.action, sigData.order.ID(), wantOrd.ID())
					}

				case sigDataUnbookedOrder:
					wantOrd := exp.data.(sigDataUnbookedOrder).order
					if wantOrd.ID() != sigData.order.ID() {
						t.Errorf("Unbook signal #%d (action %v) has order %v, expected %v",
							i, s.action, sigData.order.ID(), wantOrd.ID())
					}

				case sigDataNewEpoch:
					wantIdx := exp.data.(sigDataNewEpoch).idx
					if wantIdx != sigData.idx {
						t.Errorf("new epoch signal #%d (action %v) has epoch index %d, expected %d",
							i, s.action, sigData.idx, wantIdx)
					}
				}

			}
		})
	}

	cancel()
}

func TestMarket_Cancelable(t *testing.T) {
	// Create the market.
	mkt, storage, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		return
	}
	defer cleanup()
	// This test wants to know when epoch order matching booking is done.
	storage.epochInserted = make(chan struct{}, 1)
	// and when handlePreimage is done.
	auth.handlePreimageDone = make(chan struct{}, 1)

	epochDurationMSec := int64(mkt.EpochDuration())
	startEpochIdx := 1 + encode.UnixMilli(time.Now().Truncate(time.Millisecond))/epochDurationMSec
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mkt.Start(ctx, startEpochIdx)
	}()

	// Make an order for the first epoch.
	clientTimeMSec := startEpochIdx*epochDurationMSec + 10 // 10 ms after epoch start
	lots := 10
	qty := uint64(dcrLotSize * lots)
	rate := uint64(1000) * dcrRateStep
	aid := test.NextAccount()
	pi := test.RandomPreimage()
	commit := pi.Commit()
	limitMsg := &msgjson.LimitOrder{
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
				BaseAsset:  limitMsg.Base,
				QuoteAsset: limitMsg.Quote,
				OrderType:  order.LimitOrderType,
				ClientTime: encode.UnixTimeMilli(clientTimeMSec),
				Commit:     commit,
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     true,
				Quantity: limitMsg.Quantity,
				Address:  limitMsg.Address,
			},
			Rate:  limitMsg.Rate,
			Force: order.StandingTiF,
		}
	}
	lo := newLimit()

	oRecord := orderRecord{
		msgID: 1,
		req:   limitMsg,
		order: lo,
	}

	auth.piMtx.Lock()
	auth.preimagesByMsgID[oRecord.msgID] = pi
	auth.piMtx.Unlock()

	// Wait for the start of the epoch to submit the order.
	mkt.waitForEpochOpen()

	if mkt.Cancelable(order.OrderID{}) {
		t.Errorf("Cancelable reported bogus order as is cancelable, " +
			"but it wasn't even submitted.")
	}

	// Submit the standing limit order into the current epoch.
	err = mkt.SubmitOrder(&oRecord)
	if err != nil {
		t.Error(err)
	}

	if !mkt.Cancelable(lo.ID()) {
		t.Errorf("Cancelable failed to report order %v as cancelable, "+
			"but it was in the epoch queue", lo)
	}

	// Let the epoch cycle and the fake client respond with its preimage
	// (handlePreimageResp done)..
	<-auth.handlePreimageDone
	// and for matching to complete (in processReadyEpoch).
	<-storage.epochInserted
	storage.epochInserted = nil

	if !mkt.Cancelable(lo.ID()) {
		t.Errorf("Cancelable failed to report order %v as cancelable, "+
			"but it should have been booked.", lo)
	}

	mkt.bookMtx.Lock()
	_, ok := mkt.book.Remove(lo.ID())
	mkt.bookMtx.Unlock()
	if !ok {
		t.Errorf("Failed to remove order %v from the book.", lo)
	}

	if mkt.Cancelable(lo.ID()) {
		t.Errorf("Cancelable reported order %v as is cancelable, "+
			"but it was removed from the Book.", lo)
	}

	cancel()
	wg.Wait()
}

func TestMarket_handlePreimageResp(t *testing.T) {
	randomCommit := func() (com order.Commitment) {
		rand.Read(com[:])
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
	if !strings.Contains(msgErr.Message, "error parsing preimage notification response") ||
		!strings.Contains(msgErr.Message, "invalid type") {
		t.Errorf("Expected error message %q, got %q",
			"error parsing preimage notification response: invalid type 1 for ResponsePayload",
			msgErr.Message)
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

	// 5. payload is not msgjson.PreimageResponse, unmarshal still succeeds, but PI is nil
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

	// 6. payload unmarshal error
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
	if !strings.Contains(msgErr.Message, "error parsing preimage notification response payload result") ||
		!strings.Contains(msgErr.Message, "json: cannot unmarshal") {
		t.Errorf("Expected error message %q, got %q",
			"error parsing preimage notification response payload result: json: cannot unmarshal",
			msgErr.Message)
	}
}
