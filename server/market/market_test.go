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
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/swap"
)

const (
	AssetDCR = 42
	AssetBTC = 0
)

// This stub satisfies asset.Backend.
type TAsset struct{}

func (a *TAsset) Contract(coinID []byte, redeemScript []byte) (asset.Contract, error) {
	return nil, nil
}
func (a *TAsset) FundingCoin(coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	return nil, nil
}
func (a *TAsset) Redemption(redemptionID, contractID []byte) (asset.Coin, error) {
	return nil, nil
}
func (a *TAsset) BlockChannel(size int) chan uint32 { return nil }
func (a *TAsset) InitTxSize() uint32                { return 100 }
func (a *TAsset) CheckAddress(string) bool          { return true }
func (a *TAsset) Run(context.Context)               {}
func (a *TAsset) ValidateCoinID(coinID []byte) error {
	return nil
}
func (a *TAsset) ValidateContract(contract []byte) error {
	return nil
}

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
	mtx                  sync.Mutex
	poisonEpochOrder     order.Order
	orderWithKnownCommit order.OrderID
	commitForKnownOrder  order.Commitment
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
func (ta *TArchivist) OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error) {
	return order.OrderStatusUnknown, order.UnknownOrderType, -1, errors.New("boom")
}
func (ta *TArchivist) NewEpochOrder(ord order.Order) error {
	ta.mtx.Lock()
	defer ta.mtx.Unlock()
	if ta.poisonEpochOrder != nil && ord.ID() == ta.poisonEpochOrder.ID() {
		return errors.New("barf")
	}
	return nil
}
func (ta *TArchivist) failOnEpochOrder(ord order.Order) {
	ta.mtx.Lock()
	ta.poisonEpochOrder = ord
	ta.mtx.Unlock()
}
func (ta *TArchivist) BookOrder(*order.LimitOrder) error                      { return nil }
func (ta *TArchivist) ExecuteOrder(ord order.Order) error                     { return nil }
func (ta *TArchivist) CancelOrder(*order.LimitOrder) error                    { return nil }
func (ta *TArchivist) RevokeOrder(*order.LimitOrder) error                    { return nil }
func (ta *TArchivist) FailCancelOrder(*order.CancelOrder) error               { return nil }
func (ta *TArchivist) UpdateOrderFilled(order.Order) error                    { return nil }
func (ta *TArchivist) UpdateOrderStatus(order.Order, order.OrderStatus) error { return nil }
func (ta *TArchivist) InsertMatch(match *order.Match) error                   { return nil }
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
func (ta *TArchivist) SaveRedeemA(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SaveRedeemAckSigB(mid db.MarketMatchID, sig []byte) error { return nil }
func (ta *TArchivist) SaveRedeemB(mid db.MarketMatchID, coinID []byte, timestamp int64) error {
	return nil
}
func (ta *TArchivist) SaveRedeemAckSigA(mid db.MarketMatchID, sig []byte) error { return nil }
func (ta *TArchivist) SetMatchInactive(mid db.MarketMatchID) error              { return nil }

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

func newTestMarket() (*Market, *TArchivist, *TAuth, func(), error) {
	// The DEX will make MasterCoinLockers for each asset.
	masterLockerBase := coinlock.NewMasterCoinLocker()
	bookLockerBase := masterLockerBase.Book()
	swapLockerBase := masterLockerBase.Swap()

	masterLockerQuote := coinlock.NewMasterCoinLocker()
	bookLockerQuote := masterLockerQuote.Book()
	swapLockerQuote := masterLockerQuote.Swap()

	epochDurationMSec := uint64(500) // 0.5 sec epoch duration
	storage := &TArchivist{}
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

	mkt.waitForEpochOpen()

	// Submit again
	oRecord = newOR()
	storMsgPI(oRecord.msgID, pi)
	err = mkt.SubmitOrder(oRecord)
	if err != nil {
		t.Error(err)
	}

	// Let the epoch cycle.
	time.Sleep(time.Duration(epochDurationMSec)*time.Millisecond + time.Duration(epochDurationMSec/20))
	// Let the fake client respond with its preimage, and for matching to complete.
	time.Sleep(clientPreimageDelay + 20*time.Millisecond)

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

	// Let the epoch cycle.
	time.Sleep(time.Duration(epochDurationMSec)*time.Millisecond + time.Duration(epochDurationMSec/20))
	// Let the fake client respond with its preimage, and for matching to complete.
	time.Sleep(clientPreimageDelay + 20*time.Millisecond)

	cancel()
	wg.Wait()
	cleanup()

	// Test duplicate order (commitment) with a new Market.
	mkt, storage, auth, cleanup, err = newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
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

	// Let the epoch cycle.
	time.Sleep(time.Duration(epochDurationMSec)*time.Millisecond + time.Duration(epochDurationMSec/20))
	// Let the fake client respond with its preimage, and for matching to complete.
	time.Sleep(clientPreimageDelay + 20*time.Millisecond)

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

	// NOTE: The Market is now stopping!

	cancel()
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

	var bookSignals []*bookUpdateSignal
	var mtx sync.Mutex
	bookChan := mkt.OrderFeed()
	go func() {
		for up := range bookChan {
			//fmt.Println("received signal", up.action)
			mtx.Lock()
			bookSignals = append(bookSignals, up)
			mtx.Unlock()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// This test does not start the entire market, so manually start the epoch
	// queue pump, and a goroutine to receive ready (preimage collection
	// completed) epochs and start matching, etc.
	ePump := newEpochPump()
	var wg sync.WaitGroup
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
		for {
			select {
			case ep, ok := <-ePump.ready:
				if !ok {
					return
				}

				t.Logf("processReadyEpoch: %d orders revealed\n", len(ep.ordersRevealed))

				// epochStart has completed preimage collection.
				mkt.processReadyEpoch(ctx, ep)
				goForIt <- struct{}{}
			case <-ctx.Done():
				return
			}
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
		expectedBookSignals []*bookUpdateSignal
	}{
		{
			"ok book unbook",
			eq,
			[]*bookUpdateSignal{
				{matchProofAction, nil, mp, epochIdx},
				{bookAction, lo, nil, epochIdx},
				{unbookAction, bestBuy, nil, epochIdx},
				{unbookAction, bestSell, nil, epochIdx},
			},
		},
		{
			"ok no matches, on book updates",
			eq2,
			[]*bookUpdateSignal{{matchProofAction, nil, mp2, epochIdx}},
		},
		{
			"ok empty queue",
			NewEpoch(epochIdx, epochDur),
			[]*bookUpdateSignal{{matchProofAction, nil, mp0, epochIdx}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt.enqueueEpoch(ePump, tt.epoch)
			<-goForIt
			time.Sleep(50 * time.Millisecond) // let the test goroutine receive the signals, and update bookSignals
			mtx.Lock()
			defer mtx.Unlock() // inside this closure
			defer func() { bookSignals = []*bookUpdateSignal{} }()
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

				switch s.action {
				case matchProofAction:
					mp := exp.matchProof
					if !bytes.Equal(mp.CSum, s.matchProof.CSum) {
						t.Errorf("Book signal #%d (action %v), has CSum %x, expected %x",
							i, s.action, s.matchProof.CSum, mp.CSum)
					}
					if !bytes.Equal(mp.Seed, s.matchProof.Seed) {
						t.Errorf("Book signal #%d (action %v), has Seed %x, expected %x",
							i, s.action, s.matchProof.Seed, mp.Seed)
					}
					if mp.Epoch.Idx != s.matchProof.Epoch.Idx {
						t.Errorf("Book signal #%d (action %v), has Epoch Idx %d, expected %d",
							i, s.action, s.matchProof.Epoch.Idx, mp.Epoch.Idx)
					}
					if mp.Epoch.Dur != s.matchProof.Epoch.Dur {
						t.Errorf("Book signal #%d (action %v), has Epoch Dur %d, expected %d",
							i, s.action, s.matchProof.Epoch.Dur, mp.Epoch.Dur)
					}
					if len(mp.Preimages) != len(s.matchProof.Preimages) {
						t.Errorf("Book signal #%d (action %v), has %d Preimages, expected %d",
							i, s.action, len(s.matchProof.Preimages), len(mp.Preimages))
						continue
					}
					for ii := range mp.Preimages {
						if mp.Preimages[ii] != s.matchProof.Preimages[ii] {
							t.Errorf("Book signal #%d (action %v), has #%d Preimage %x, expected %x",
								i, s.action, ii, s.matchProof.Preimages[ii], mp.Preimages[ii])
						}
					}
					if len(mp.Misses) != len(s.matchProof.Misses) {
						t.Errorf("Book signal #%d (action %v), has %d Misses, expected %d",
							i, s.action, len(s.matchProof.Misses), len(mp.Misses))
						continue
					}
					for ii := range mp.Misses {
						if mp.Misses[ii].ID() != s.matchProof.Misses[ii].ID() {
							t.Errorf("Book signal #%d (action %v), has #%d missed Order %v, expected %v",
								i, s.action, ii, s.matchProof.Misses[ii].ID(), mp.Misses[ii].ID())
						}
					}
				case bookAction, unbookAction:
					if exp.order.ID() != s.order.ID() {
						t.Errorf("Book signal #%d (action %v) has order %v, expected %v",
							i, s.action, s.order.ID(), exp.order.ID())
					}
				}

				if exp.epochIdx != s.epochIdx {
					t.Errorf("Book signal #%d (action %v) has epoch index %d, expected %d",
						i, s.action, s.epochIdx, exp.epochIdx)
				}
			}
		})
	}

	cancel()
	wg.Wait()
}

func TestMarket_Cancelable(t *testing.T) {
	// Create the market.
	mkt, _, auth, cleanup, err := newTestMarket()
	if err != nil {
		t.Fatalf("newTestMarket failure: %v", err)
		return
	}
	defer cleanup()

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

	// Let the epoch cycle.
	time.Sleep(time.Duration(epochDurationMSec+epochDurationMSec/20) * time.Millisecond)
	// Let the fake client respond with its preimage, and for matching to complete.
	time.Sleep(clientPreimageDelay + 20*time.Millisecond)
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

	randomPreimage := func() (pi order.Preimage) {
		rand.Read(pi[:])
		return
	}

	randomCommit := func() (com order.Commitment) {
		rand.Read(com[:])
		return
	}

	authMgr := &TAuth{}
	mkt := &Market{
		auth: authMgr,
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
	dat := &piData{preimage: make(chan *order.Preimage)}
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
	dat = &piData{preimage: make(chan *order.Preimage)}
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
	pi := randomPreimage()
	dat = &piData{
		commit:   randomCommit(), // instead of pi.Commit()
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
	dat = &piData{
		commit:   pi.Commit(),
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
	dat = &piData{preimage: make(chan *order.Preimage)}
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
	dat = &piData{preimage: make(chan *order.Preimage)}
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

func Test_epochPump_next(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ep := newEpochPump()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ep.Run(ctx)
		wg.Done()
	}()

	waitTime := int64(5 * time.Second)
	if testing.Short() {
		waitTime = int64(1 * time.Second)
	}

	var epochStart, epochDur, numEpochs int64 = 123413513, 1_000, 100
	epochs := make([]*readyEpoch, numEpochs)
	for i := int64(0); i < numEpochs; i++ {
		rq := ep.Insert(NewEpoch(i+epochStart, epochDur))
		epochs[i] = rq

		// Simulate preimage collection, randomly making the queues ready.
		go func(epochIdx int64) {
			wait := time.Duration(rand.Int63n(waitTime))
			time.Sleep(wait)
			close(rq.ready)
		}(i + epochStart)
	}

	// Receive all the ready epochs, verifying they come in order.
	for i := epochStart; i < numEpochs+epochStart; i++ {
		rq := <-ep.ready
		if rq.Epoch != i {
			t.Errorf("Received epoch %d, expected %d", rq.Epoch, i)
		}
	}
	// All fake preimage collection goroutines are done now.

	cancel()
	wg.Wait()
}
