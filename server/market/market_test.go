// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/order/test"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/swap"
)

const (
	AssetDCR = 42
	AssetBTC = 0
)

// This stub satisfies asset.DEXAsset.
type TAsset struct{}

func (a *TAsset) Coin(coinID []byte, redeemScript []byte) (asset.Coin, error) {
	return nil, nil
}
func (a *TAsset) BlockChannel(size int) chan uint32 { return nil }
func (a *TAsset) InitTxSize() uint32                { return 100 }
func (a *TAsset) CheckAddress(string) bool          { return true }

func newAsset(id uint32, lotSize uint64) *asset.BackedAsset {
	return &asset.BackedAsset{
		Backend: &TAsset{},
		Asset: dex.Asset{
			ID:      id,
			LotSize: lotSize,
			Symbol:  dex.BipIDSymbol(id),
		},
	}
}

type TArchivist struct {
	poisonEpochOrder order.Order
}

func (ta *TArchivist) LastErr() error { return nil }
func (ta *TArchivist) Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	return nil, order.OrderStatusUnknown, errors.New("boom")
}
func (ta *TArchivist) ActiveOrderCoins(base, quote uint32) (baseCoins, quoteCoins map[order.OrderID][]order.CoinID, err error) {
	return make(map[order.OrderID][]order.CoinID), make(map[order.OrderID][]order.CoinID), nil
}
func (ta *TArchivist) UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []order.OrderStatus, error) {
	return nil, nil, errors.New("boom")
}
func (ta *TArchivist) OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error) {
	return order.OrderStatusUnknown, order.UnknownOrderType, -1, errors.New("boom")
}
func (ta *TArchivist) NewEpochOrder(ord order.Order) error {
	if ta.poisonEpochOrder != nil && ord.ID() == ta.poisonEpochOrder.ID() {
		return errors.New("barf")
	}
	return nil
}
func (ta *TArchivist) failOnEpochOrder(ord order.Order)                       { ta.poisonEpochOrder = ord }
func (ta *TArchivist) BookOrder(*order.LimitOrder) error                      { return nil }
func (ta *TArchivist) ExecuteOrder(ord order.Order) error                     { return nil }
func (ta *TArchivist) CancelOrder(*order.LimitOrder) error                    { return nil }
func (ta *TArchivist) RevokeOrder(*order.LimitOrder) error                    { return nil }
func (ta *TArchivist) FailCancelOrder(*order.CancelOrder) error               { return nil }
func (ta *TArchivist) UpdateOrderFilled(order.Order) error                    { return nil }
func (ta *TArchivist) UpdateOrderStatus(order.Order, order.OrderStatus) error { return nil }
func (ta *TArchivist) UpdateMatch(match *order.Match) error                   { return nil }
func (ta *TArchivist) MatchByID(mid order.MatchID, base, quote uint32) (*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) UserMatches(aid account.AccountID, base, quote uint32) ([]*db.MatchData, error) {
	return nil, nil
}
func (ta *TArchivist) CloseAccount(account.AccountID, account.Rule)                 {}
func (ta *TArchivist) Account(account.AccountID) (acct *account.Account, paid bool) { return nil, false }
func (ta *TArchivist) CreateAccount(*account.Account) (string, error)               { return "", nil }
func (ta *TArchivist) AccountRegAddr(account.AccountID) (string, error)             { return "", nil }
func (ta *TArchivist) PayAccount(account.AccountID, string, uint32) error           { return nil }

func randomOrderID() order.OrderID {
	pk := randomBytes(order.OrderIDSize)
	var id order.OrderID
	copy(id[:], pk)
	return id
}

func newTestMarket() (*Market, *TArchivist, error) {
	// The DEX will make MasterCoinLockers for each asset.
	masterLockerBase := coinlock.NewMasterCoinLocker()
	bookLockerBase := masterLockerBase.Book()
	swapLockerBase := masterLockerBase.Swap()

	masterLockerQuote := coinlock.NewMasterCoinLocker()
	bookLockerQuote := masterLockerQuote.Book()
	swapLockerQuote := masterLockerQuote.Swap()

	ctx := context.Background()
	epochDurationMSec := uint64(500) // 0.5 sec epoch duration
	storage := &TArchivist{}
	authMgr := &TAuth{}
	swapperCfg := &swap.Config{
		Ctx: ctx,
		Assets: map[uint32]*swap.LockableAsset{
			assetDCR.ID: {BackedAsset: assetDCR, CoinLocker: swapLockerBase},
			assetBTC.ID: {BackedAsset: assetBTC, CoinLocker: swapLockerQuote},
		},
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: 10 * time.Second,
	}
	swapper := swap.NewSwapper(swapperCfg)

	mktInfo, err := dex.NewMarketInfo(assetDCR.ID, assetBTC.ID,
		assetDCR.LotSize, epochDurationMSec)
	if err != nil {
		return nil, nil, fmt.Errorf("dex.NewMarketInfo() failure: %v", err)
	}

	mkt, err := NewMarket(ctx, mktInfo, storage, swapper, authMgr, bookLockerBase, bookLockerQuote)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to create test market: %v", err)
	}
	return mkt, storage, nil
}

func TestMarket_Book(t *testing.T) {
	mkt, _, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}

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

	buys, sells := mkt.Book()
	if buys[0] != bestBuy {
		t.Errorf("Incorrect best buy order. Got %v, expected %v",
			buys[0], bestBuy)
	}
	if sells[0] != bestSell {
		t.Errorf("Incorrect best sell order. Got %v, expected %v",
			sells[0], bestSell)
	}
}

func TestMarket_runEpochs(t *testing.T) {
	// This test exercises the Market's main loop, which cycles the epochs and
	// queues (or not) incoming orders.
	lots := 10
	qty := uint64(dcrLotSize * lots)
	rate := uint64(1000) * dcrRateStep
	aid := test.NextAccount()
	now := time.Now().Round(time.Millisecond).UTC()
	limit := &msgjson.Limit{
		Prefix: msgjson.Prefix{
			AccountID:  aid[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.LimitOrderNum,
			ClientTime: uint64(order.UnixMilli(now)),
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
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  aid,
					BaseAsset:  limit.Base,
					QuoteAsset: limit.Quote,
					OrderType:  order.LimitOrderType,
					ClientTime: now,
				},
				Coins:    []order.CoinID{},
				Sell:     true,
				Quantity: limit.Quantity,
				Address:  limit.Address,
			},
			Rate:  limit.Rate,
			Force: order.StandingTiF,
		}
	}
	lo := newLimit()

	// w := &test.Writer{
	// 	Addr: limit.Address,
	// 	Acct: aid,
	// 	Sell: true,
	// 	Market: &test.Market{
	// 		Base:    assetDCR.ID,
	// 		Quote:   assetBTC.ID,
	// 		LotSize: assetDCR.LotSize,
	// 	},
	// }
	// test.WriteLimitOrder(w, limit.Rate, lots, order.StandingTiF, 0 /* ! */)

	oRecord := orderRecord{
		msgID: 1,
		req:   limit,
		order: lo,
	}

	mkt, _, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		return
	}
	t.Log(mkt.marketInfo.Name)
	epochDurationMSec := mkt.epochDuration

	startEpochIdx := 1 + order.UnixMilli(time.Now())/epochDurationMSec
	mkt.Start(startEpochIdx)

	// Submit order before market starts running
	err = mkt.SubmitOrder(&oRecord)
	if err == nil {
		t.Error("order submitted to stopped market")
	}
	if err != ErrMarketNotRunning {
		t.Errorf(`expected ErrMarketNotRunning ("%v"), got "%v"`, ErrMarketNotRunning, err)
	}

	mkt.WaitForEpochOpen()

	// Submit again
	err = mkt.SubmitOrder(&oRecord)
	if err != nil {
		t.Error(err)
	}

	//let the epoch cycle
	time.Sleep(time.Duration(epochDurationMSec)*time.Millisecond + time.Duration(epochDurationMSec/20))

	mkt.Stop()
	mkt.WaitForShutdown()

	// Test duplicate order with a new Market.
	mkt, storage, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	startEpochIdx = 1 + order.UnixMilli(time.Now())/epochDurationMSec
	mkt.Start(startEpochIdx)
	mkt.WaitForEpochOpen()

	err = mkt.SubmitOrder(&oRecord)
	if err != nil {
		t.Error(err)
	}

	err = mkt.SubmitOrder(&oRecord)
	if err == nil {
		t.Errorf("A duplicate order was processed, but it should not have been.")
	} else if err != ErrDuplicateOrder {
		t.Errorf(`expected ErrDuplicateOrder ("%v"), got "%v"`, ErrDuplicateOrder, err)
	}

	// Send an order with a bad lot size.
	lo = newLimit()
	lo.Quantity += mkt.marketInfo.LotSize / 2
	oRecord.order = lo
	err = mkt.SubmitOrder(&oRecord)
	if err == nil {
		t.Errorf("An invalid order was processed, but it should not have been.")
	} else if err != ErrInvalidOrder {
		t.Errorf(`expected ErrInvalidOrder ("%v"), got "%v"`, ErrInvalidOrder, err)
	}

	// Submit an order that breaks storage somehow.
	// tweak the order so it's not a dup.
	lo = newLimit()
	lo.Quantity *= 2
	oRecord.order = lo
	storage.failOnEpochOrder(lo)
	if err = mkt.SubmitOrder(&oRecord); err != ErrInternalServer {
		t.Errorf(`expected ErrInternalServer ("%v"), got "%v"`, ErrInternalServer, err)
	}
}

func TestMarket_processEpoch(t *testing.T) {
	// This tests that processEpoch sends the expected book and unbook messages
	// to book subscribers registered via OrderFeed.

	mkt, _, err := newTestMarket()
	if err != nil {
		t.Fatalf("Failed to create test market: %v", err)
		return
	}

	rand.Seed(0)

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
	bestBuyRate := bestBuy.Rate
	bestBuyQuant := bestBuy.Quantity
	bestSellID := bestSell.ID()

	eq := NewEpoch(123413513, 4)
	lo := makeLO(seller3, bestBuyRate-dcrRateStep, bestBuyQuant, order.StandingTiF)
	co := makeCO(buyer3, bestSellID)
	eq.Insert(lo)
	eq.Insert(co)

	eq2 := NewEpoch(123413513, 4)
	coMiss := makeCO(buyer3, randomOrderID())
	eq2.Insert(coMiss)

	// These little helper functions are not thread safe.
	var bookSignals []*bookUpdateSignal
	var mtx sync.Mutex
	bookChan := mkt.OrderFeed()
	go func() {
		for up := range bookChan {
			mtx.Lock()
			bookSignals = append(bookSignals, up)
			mtx.Unlock()
		}
	}()

	tests := []struct {
		name                string
		epoch               *EpochQueue
		expectedBookSignals []*bookUpdateSignal
	}{
		{
			"ok book unbook",
			eq,
			[]*bookUpdateSignal{
				{bookAction, lo},
				{unbookAction, bestBuy},
				{unbookAction, bestSell},
			},
		},
		{
			"ok no matches, on book updates",
			eq2,
			[]*bookUpdateSignal{},
		},
		{
			"ok empty queue",
			NewEpoch(123413513, 4),
			[]*bookUpdateSignal{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt.processEpoch(tt.epoch)
			time.Sleep(50 * time.Millisecond) // let the test goroutine receive the signals
			mtx.Lock()
			if len(bookSignals) != len(tt.expectedBookSignals) {
				t.Errorf("expected %d book update signals, got %d",
					len(tt.expectedBookSignals), len(bookSignals))
			}
			for i, s := range bookSignals {
				if tt.expectedBookSignals[i].action != s.action {
					t.Errorf("Book signal %d has action %d, expected %d",
						i, s.action, tt.expectedBookSignals[i].action)
				}
				if tt.expectedBookSignals[i].order.ID() != s.order.ID() {
					t.Errorf("Book signal %d has order %v, expected %v",
						i, s.order.ID(), tt.expectedBookSignals[i].order.ID())
				}
			}
			bookSignals = []*bookUpdateSignal{}
			mtx.Unlock()
		})
	}

	mkt.Stop()
	mkt.WaitForShutdown()
}

func TestMarket_Cancelable(t *testing.T) {
	lots := 10
	qty := uint64(dcrLotSize * lots)
	rate := uint64(1000) * dcrRateStep
	aid := test.NextAccount()
	now := time.Now().Round(time.Millisecond).UTC()
	limitMsg := &msgjson.Limit{
		Prefix: msgjson.Prefix{
			AccountID:  aid[:],
			Base:       dcrID,
			Quote:      btcID,
			OrderType:  msgjson.LimitOrderNum,
			ClientTime: uint64(order.UnixMilli(now)),
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
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  aid,
					BaseAsset:  limitMsg.Base,
					QuoteAsset: limitMsg.Quote,
					OrderType:  order.LimitOrderType,
					ClientTime: now,
				},
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

	mkt, _, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		return
	}

	epochDurationMSec := mkt.epochDuration
	startEpochIdx := 1 + order.UnixMilli(time.Now())/epochDurationMSec
	mkt.Start(startEpochIdx)

	mkt.WaitForEpochOpen()

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

	// Let the epoch cycle.
	time.Sleep(time.Duration(epochDurationMSec)*time.Millisecond + time.Duration(epochDurationMSec/20))
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
}
