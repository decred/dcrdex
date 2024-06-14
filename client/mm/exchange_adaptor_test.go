package mm

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"runtime/debug"
	"sort"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/utils"
	"github.com/davecgh/go-spew/spew"
)

type tEventLogDB struct {
	storedEventsMtx sync.Mutex
	storedEvents    []*MarketMakingEvent
}

var _ eventLogDB = (*tEventLogDB)(nil)

func newTEventLogDB() *tEventLogDB {
	return &tEventLogDB{
		storedEvents: make([]*MarketMakingEvent, 0),
	}
}

func (db *tEventLogDB) storeNewRun(startTime int64, mkt *MarketWithHost, cfg *BotConfig, initialState *BalanceState) error {
	return nil
}
func (db *tEventLogDB) endRun(startTime int64, mkt *MarketWithHost, endTime int64) error { return nil }
func (db *tEventLogDB) storeEvent(startTime int64, mkt *MarketWithHost, e *MarketMakingEvent, fs *BalanceState) {
	db.storedEventsMtx.Lock()
	defer db.storedEventsMtx.Unlock()
	db.storedEvents = append(db.storedEvents, e)
}
func (db *tEventLogDB) storedEventAtIndexEquals(e *MarketMakingEvent, idx int) bool {
	db.storedEventsMtx.Lock()
	defer db.storedEventsMtx.Unlock()
	if idx < 0 || idx >= len(db.storedEvents) {
		return false
	}
	db.storedEvents[idx].TimeStamp = 0 // ignore timestamp
	if !reflect.DeepEqual(db.storedEvents[idx], e) {
		debug.PrintStack()
		fmt.Println("storedEvents: ", spew.Sdump(db.storedEvents))
		fmt.Printf("wanted:\n%v\ngot:\n%v\n", spew.Sdump(e), spew.Sdump(db.storedEvents[idx]))
		return false
	}
	return true
}
func (db *tEventLogDB) latestStoredEventEquals(e *MarketMakingEvent) bool {
	db.storedEventsMtx.Lock()
	if e == nil && len(db.storedEvents) == 0 {
		db.storedEventsMtx.Unlock()
		return true
	}
	if e == nil {
		db.storedEventsMtx.Unlock()
		return false
	}
	db.storedEventsMtx.Unlock()
	return db.storedEventAtIndexEquals(e, len(db.storedEvents)-1)
}
func (db *tEventLogDB) latestStoredEvent() *MarketMakingEvent {
	db.storedEventsMtx.Lock()
	defer db.storedEventsMtx.Unlock()
	if len(db.storedEvents) == 0 {
		return nil
	}
	return db.storedEvents[len(db.storedEvents)-1]
}
func (db *tEventLogDB) runs(n uint64, refStartTime *uint64, refMkt *MarketWithHost) ([]*MarketMakingRun, error) {
	return nil, nil
}
func (db *tEventLogDB) runOverview(startTime int64, mkt *MarketWithHost) (*MarketMakingRunOverview, error) {
	return nil, nil
}
func (db *tEventLogDB) runEvents(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64, pendingOnly bool) ([]*MarketMakingEvent, error) {
	return nil, nil
}

func tFees(swap, redeem, refund, funding uint64) *orderFees {
	lotFees := &LotFees{
		Swap:   swap,
		Redeem: redeem,
		Refund: refund,
	}
	return &orderFees{
		LotFeeRange: &LotFeeRange{
			Max:       lotFees,
			Estimated: lotFees,
		},
		funding: funding,
	}
}

func TestSufficientBalanceForDEXTrade(t *testing.T) {
	lotSize := uint64(1e8)
	sellFees := tFees(1e5, 2e5, 3e5, 0)
	buyFees := tFees(5e5, 6e5, 7e5, 0)

	fundingFees := uint64(8e5)

	type test struct {
		name            string
		baseID, quoteID uint32
		balances        map[uint32]uint64
		isAccountLocker map[uint32]bool
		sell            bool
		rate, qty       uint64
	}

	b2q := calc.BaseToQuote

	tests := []*test{
		{
			name:    "sell, non account locker",
			baseID:  42,
			quoteID: 0,
			sell:    true,
			rate:    1e7,
			qty:     3 * lotSize,
			balances: map[uint32]uint64{
				42: 3*lotSize + 3*sellFees.Max.Swap + fundingFees,
				0:  0,
			},
		},
		{
			name:    "buy, non account locker",
			baseID:  42,
			quoteID: 0,
			rate:    2e7,
			qty:     2 * lotSize,
			sell:    false,
			balances: map[uint32]uint64{
				42: 0,
				0:  b2q(2e7, 2*lotSize) + 2*buyFees.Max.Swap + fundingFees,
			},
		},
		{
			name:    "sell, account locker/token",
			baseID:  966001,
			quoteID: 60,
			sell:    true,
			rate:    2e7,
			qty:     3 * lotSize,
			isAccountLocker: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			balances: map[uint32]uint64{
				966001: 3 * lotSize,
				966:    3*sellFees.Max.Swap + 3*sellFees.Max.Refund + fundingFees,
				60:     3 * sellFees.Max.Redeem,
			},
		},
		{
			name:    "buy, account locker/token",
			baseID:  966001,
			quoteID: 60,
			sell:    false,
			rate:    2e7,
			qty:     3 * lotSize,
			isAccountLocker: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			balances: map[uint32]uint64{
				966: 3 * buyFees.Max.Redeem,
				60:  b2q(2e7, 3*lotSize) + 3*buyFees.Max.Swap + 3*buyFees.Max.Refund + fundingFees,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tCore := newTCore()
			tCore.singleLotSellFees = sellFees
			tCore.singleLotBuyFees = buyFees
			tCore.maxFundingFees = fundingFees

			tCore.market = &core.Market{
				BaseID:  test.baseID,
				QuoteID: test.quoteID,
				LotSize: lotSize,
			}
			mwh := &MarketWithHost{
				BaseID:  test.baseID,
				QuoteID: test.quoteID,
			}

			tCore.isAccountLocker = test.isAccountLocker

			checkBalanceSufficient := func(expSufficient bool) {
				t.Helper()
				adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
					core:            tCore,
					baseDexBalances: test.balances,
					mwh:             mwh,
					eventLogDB:      &tEventLogDB{},
				})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				_, err := adaptor.Connect(ctx)
				if err != nil {
					t.Fatalf("Connect error: %v", err)
				}
				sufficient, err := adaptor.SufficientBalanceForDEXTrade(test.rate, test.qty, test.sell)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if sufficient != expSufficient {
					t.Fatalf("expected sufficient=%v, got %v", expSufficient, sufficient)
				}
			}

			checkBalanceSufficient(true)

			for assetID, bal := range test.balances {
				if bal == 0 {
					continue
				}
				test.balances[assetID]--
				checkBalanceSufficient(false)
				test.balances[assetID]++
			}
		})
	}
}

func TestSufficientBalanceForCEXTrade(t *testing.T) {
	const baseID uint32 = 42
	const quoteID uint32 = 0

	type test struct {
		name        string
		cexBalances map[uint32]uint64
		sell        bool
		rate, qty   uint64
	}

	tests := []*test{
		{
			name: "sell",
			sell: true,
			rate: 5e7,
			qty:  1e8,
			cexBalances: map[uint32]uint64{
				baseID: 1e8,
			},
		},
		{
			name: "buy",
			sell: false,
			rate: 5e7,
			qty:  1e8,
			cexBalances: map[uint32]uint64{
				quoteID: calc.BaseToQuote(5e7, 1e8),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkBalanceSufficient := func(expSufficient bool) {
				tCore := newTCore()
				adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
					core:            tCore,
					baseCexBalances: test.cexBalances,
					mwh: &MarketWithHost{
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				})
				sufficient, err := adaptor.SufficientBalanceForCEXTrade(baseID, quoteID, test.sell, test.rate, test.qty)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if sufficient != expSufficient {
					t.Fatalf("expected sufficient=%v, got %v", expSufficient, sufficient)
				}
			}

			checkBalanceSufficient(true)

			for assetID := range test.cexBalances {
				test.cexBalances[assetID]--
				checkBalanceSufficient(false)
				test.cexBalances[assetID]++
			}
		})
	}
}

func TestCEXBalanceCounterTrade(t *testing.T) {
	// Tests that CEX locked balance is increased and available balance is
	// decreased when CEX funds are required for a counter trade.
	tCore := newTCore()
	tCEX := newTCEX()

	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}

	dexBalances := map[uint32]uint64{
		0:  1e8,
		42: 1e8,
	}
	cexBalances := map[uint32]uint64{
		42: 1e8,
		0:  1e8,
	}

	botID := dexMarketID("host1", 42, 0)
	eventLogDB := newTEventLogDB()
	adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
		botID:           botID,
		core:            tCore,
		cex:             tCEX,
		baseDexBalances: dexBalances,
		baseCexBalances: cexBalances,
		mwh: &MarketWithHost{
			Host:    "host1",
			BaseID:  42,
			QuoteID: 0,
		},
		eventLogDB: eventLogDB,
	})

	adaptor.pendingDEXOrders = map[order.OrderID]*pendingDEXOrder{
		orderIDs[0]: {
			counterTradeRate: 6e7,
		},
		orderIDs[1]: {
			counterTradeRate: 5e7,
		},
	}

	order0 := &core.Order{
		Qty:     5e6,
		Rate:    6.1e7,
		Sell:    true,
		BaseID:  42,
		QuoteID: 0,
	}

	order1 := &core.Order{
		Qty:     5e6,
		Rate:    4.9e7,
		Sell:    false,
		BaseID:  42,
		QuoteID: 0,
	}

	adaptor.pendingDEXOrders[orderIDs[0]].updateState(order0, &balanceEffects{})
	adaptor.pendingDEXOrders[orderIDs[1]].updateState(order1, &balanceEffects{})

	dcrBalance := adaptor.CEXBalance(42)
	expDCR := &BotBalance{
		Available: 1e8 - order1.Qty,
		Reserved:  order1.Qty,
	}
	if !reflect.DeepEqual(dcrBalance, expDCR) {
		t.Fatalf("unexpected DCR balance. wanted %+v, got %+v", expDCR, dcrBalance)
	}

	btcBalance := adaptor.CEXBalance(0)
	expBTCReserved := calc.BaseToQuote(adaptor.pendingDEXOrders[orderIDs[0]].counterTradeRate, order0.Qty)
	expBTC := &BotBalance{
		Available: 1e8 - expBTCReserved,
		Reserved:  expBTCReserved,
	}
	if !reflect.DeepEqual(btcBalance, expBTC) {
		t.Fatalf("unexpected BTC balance. wanted %+v, got %+v", expBTC, btcBalance)
	}
}

func TestFreeUpFunds(t *testing.T) {
	const baseID, quoteID = 42, 0
	const lotSize = 1e9
	const rate = 1e6
	quoteLot := calc.BaseToQuote(rate, lotSize)
	u := mustParseAdaptorFromMarket(&core.Market{
		RateStep:   1e3,
		AtomToConv: 1,
		LotSize:    lotSize,
		BaseID:     baseID,
		QuoteID:    quoteID,
	})
	oid := order.OrderID{1}
	addOrder := func(assetID uint32, lots, epoch uint64) {
		matchable := lots * lotSize
		if assetID == quoteID {
			matchable = calc.BaseToQuote(rate, lots*lotSize)
		}
		po := &pendingDEXOrder{}
		po.state.Store(&dexOrderState{
			balanceEffects: &balanceEffects{
				settled: map[uint32]int64{
					assetID: -int64(matchable),
				},
				locked: map[uint32]uint64{
					assetID: matchable,
				},
				pending: make(map[uint32]uint64),
			},
			order: &core.Order{
				ID:    oid[:],
				Sell:  assetID == baseID,
				Epoch: epoch,
				Rate:  rate,
			},
		})
		u.pendingDEXOrders[oid] = po
	}
	clearOrders := func() {
		u.pendingDEXOrders = make(map[order.OrderID]*pendingDEXOrder)
	}

	const epoch uint64 = 5

	check := func(assetID uint32, expOK bool, qty, pruneTo uint64, expOIDs ...order.OrderID) {
		ords, ok := u.freeUpFunds(assetID, qty, pruneTo, epoch)
		if ok != expOK {
			t.Fatalf("wrong OK. wanted %t, got %t", expOK, ok)
		}
		if len(ords) != len(expOIDs) {
			t.Fatalf("wrong number of orders freed. wanted %d, got %d", len(expOIDs), len(ords))
		}
		m := make(map[order.OrderID]struct{})
		for _, o := range ords {
			var oid order.OrderID
			copy(oid[:], o.order.ID)
			m[oid] = struct{}{}
		}
		for _, oid := range expOIDs {
			if _, found := m[oid]; !found {
				t.Fatalf("didn't find order %s", oid)
			}
		}
	}

	addOrder(baseID, 1, epoch-2)
	check(baseID, true, lotSize, quoteLot, oid)
	check(baseID, false, lotSize+1, quoteLot)
	clearOrders()
	// Uncancellable epoch prevents pruning.
	addOrder(baseID, 1, epoch-1)
	check(baseID, false, lotSize, quoteLot)

	clearOrders()
	addOrder(quoteID, 1, epoch-2)
	check(quoteID, true, quoteLot, lotSize, oid)
	check(quoteID, false, quoteLot+1, lotSize)
}

func TestDistribution(t *testing.T) {
	// utxo/utxo
	testDistribution(t, 42, 0)
	// utxo/account-locker
	testDistribution(t, 42, 60)
	testDistribution(t, 60, 42)
	// token/parent
	testDistribution(t, 60001, 60)
	testDistribution(t, 60, 60001)
	// token/token
	testDistribution(t, 60001, 966003)
	testDistribution(t, 966003, 60001)
	// token/utxo
	testDistribution(t, 42, 966003)
	testDistribution(t, 966003, 42)
}

func testDistribution(t *testing.T, baseID, quoteID uint32) {
	const lotSize = 5e7
	const sellSwapFees, sellRedeemFees = 3e5, 1e5
	const buySwapFees, buyRedeemFees = 2e4, 1e4
	const sellRefundFees, buyRefundFees = 8e3, 9e4
	const buyVWAP, sellVWAP = 1e7, 1.1e7
	const extra = 80
	const profit = 0.01

	u := mustParseAdaptorFromMarket(&core.Market{
		LotSize:  lotSize,
		BaseID:   baseID,
		QuoteID:  quoteID,
		RateStep: 1e2,
	})
	cex := newTCEX()
	tCore := newTCore()
	u.CEX = cex
	u.clientCore = tCore
	u.autoRebalanceCfg = &AutoRebalanceConfig{}
	a := &arbMarketMaker{unifiedExchangeAdaptor: u}
	a.cfgV.Store(&ArbMarketMakerConfig{Profit: profit})
	fiatRates := map[uint32]float64{baseID: 1, quoteID: 1}
	u.fiatRates.Store(fiatRates)

	isAccountLocker := func(assetID uint32) bool {
		if tkn := asset.TokenInfo(assetID); tkn != nil {
			fiatRates[tkn.ParentID] = 1
			return true
		}
		return len(asset.Asset(assetID).Tokens) > 0
	}
	var sellFundingFees, buyFundingFees uint64
	if !isAccountLocker(baseID) {
		sellFundingFees = 5e3
	}
	if !isAccountLocker(quoteID) {
		buyFundingFees = 6e3
	}

	a.buyFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Redeem: buyRedeemFees,
				Refund: buyRefundFees,
			},
			Estimated: &LotFees{
				Swap:   buySwapFees,
				Redeem: buyRedeemFees,
				Refund: buyRefundFees,
			},
		},
		funding:           buyFundingFees,
		bookingFeesPerLot: buySwapFees,
	}
	a.sellFees = &orderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Redeem: sellRedeemFees,
				Refund: sellRefundFees,
			},
			Estimated: &LotFees{
				Swap:   sellSwapFees,
				Redeem: sellRedeemFees,
			},
		},
		funding:           sellFundingFees,
		bookingFeesPerLot: sellSwapFees,
	}

	buyRate, _ := a.dexPlacementRate(buyVWAP, false)
	sellRate, _ := a.dexPlacementRate(sellVWAP, true)

	var buyLots, sellLots, minDexBase, minCexBase, totalBase, minDexQuote, minCexQuote, totalQuote uint64
	var addBaseFees, addQuoteFees uint64
	var perLot *lotCosts

	setBals := func(dexBase, cexBase, dexQuote, cexQuote uint64) {
		a.baseDexBalances[baseID] = int64(dexBase)
		a.baseCexBalances[baseID] = int64(cexBase)
		a.baseDexBalances[quoteID] = int64(dexQuote)
		a.baseCexBalances[quoteID] = int64(cexQuote)
	}

	setLots := func(b, s uint64) {
		buyLots, sellLots = b, s
		a.placementLotsV.Store(&placementLots{
			baseLots:  sellLots,
			quoteLots: buyLots,
		})
		addBaseFees, addQuoteFees = sellFundingFees, buyFundingFees
		cex.asksVWAP[lotSize*buyLots] = vwapResult{avg: buyVWAP}
		cex.bidsVWAP[lotSize*sellLots] = vwapResult{avg: sellVWAP}
		minDexBase = sellLots*(lotSize+sellSwapFees) + sellFundingFees
		if baseID == u.quoteFeeID {
			addBaseFees += buyRedeemFees * buyLots
			minDexBase += buyRedeemFees * buyLots
		}
		minCexBase = buyLots * lotSize
		minDexQuote = calc.BaseToQuote(buyRate, buyLots*lotSize) + a.buyFees.bookingFeesPerLot*buyLots + buyFundingFees
		if quoteID == u.baseFeeID {
			addQuoteFees += sellRedeemFees * sellLots
			minDexQuote += sellRedeemFees * sellLots
		}
		minCexQuote = calc.BaseToQuote(sellRate, sellLots*lotSize)
		totalBase = minCexBase + minDexBase
		totalQuote = minCexQuote + minDexQuote
		var err error
		perLot, err = a.lotCosts(buyRate, sellRate)
		if err != nil {
			t.Fatalf("Error getting lot costs: %v", err)
		}
		a.autoRebalanceCfg.MinBaseTransfer = lotSize
		a.autoRebalanceCfg.MinQuoteTransfer = utils.Min(perLot.cexQuote, perLot.dexQuote)
	}

	checkDistribution := func(baseDeposit, baseWithdraw, quoteDeposit, quoteWithdraw uint64) {
		t.Helper()
		dist, err := a.distribution()
		if err != nil {
			t.Fatalf("distribution error: %v", err)
		}
		if dist.baseInv.toDeposit != baseDeposit {
			t.Fatalf("wrong base deposit size. wanted %d, got %d", baseDeposit, dist.baseInv.toDeposit)
		}
		if dist.baseInv.toWithdraw != baseWithdraw {
			t.Fatalf("wrong base withrawal size. wanted %d, got %d", baseWithdraw, dist.baseInv.toWithdraw)
		}
		if dist.quoteInv.toDeposit != quoteDeposit {
			t.Fatalf("wrong quote deposit size. wanted %d, got %d", quoteDeposit, dist.quoteInv.toDeposit)
		}
		if dist.quoteInv.toWithdraw != quoteWithdraw {
			t.Fatalf("wrong quote withrawal size. wanted %d, got %d", quoteWithdraw, dist.quoteInv.toWithdraw)
		}
	}

	setLots(1, 1)
	// Base asset - perfect distribution - no action
	setBals(minDexBase, minCexBase, minDexQuote, minCexQuote)
	checkDistribution(0, 0, 0, 0)
	// Quote asset - perfect distribution - no action
	checkDistribution(0, 0, 0, 0)

	// Move all of the base balance to cex and max sure we get a withdraw.
	setBals(0, totalBase, minDexQuote, minCexQuote)
	checkDistribution(0, minDexBase, 0, 0)
	// Raise the transfer theshold by one atom and it should zero the withdraw.
	a.autoRebalanceCfg.MinBaseTransfer = minDexBase + 1
	checkDistribution(0, 0, 0, 0)
	a.autoRebalanceCfg.MinBaseTransfer = 0

	// Same for quote
	setBals(minDexBase, minCexBase, 0, totalQuote)
	checkDistribution(0, 0, 0, minDexQuote)
	a.autoRebalanceCfg.MinQuoteTransfer = minDexQuote + 1
	checkDistribution(0, 0, 0, 0)
	a.autoRebalanceCfg.MinQuoteTransfer = 0
	// Base deposit
	setBals(totalBase, 0, minDexQuote, minCexQuote)

	checkDistribution(minCexBase, 0, 0, 0)
	// Quote deposit
	setBals(minDexBase, minCexBase, totalQuote, 0)
	checkDistribution(0, 0, minCexQuote, 0)
	// Doesn't have to be symmetric.
	setLots(1, 3)
	setBals(totalBase, 0, minDexQuote, minCexQuote)
	checkDistribution(minCexBase, 0, 0, 0)
	setBals(minDexBase, minCexBase, 0, totalQuote)
	checkDistribution(0, 0, 0, minDexQuote)

	// Even if there's extra, if neither side has too low of balance, nothing
	// will happen. The extra will be split evenly between dex and cex.
	// But if a side is one atom short, a full reblance will be done.
	setLots(5, 3)
	// Base OK
	setBals(minDexBase, minCexBase*10, minDexQuote, minCexQuote)
	checkDistribution(0, 0, 0, 0)
	// Base withdraw. Extra goes to dex for base asset.
	setBals(0, minDexBase+minCexBase+extra, minDexQuote, minCexQuote)
	checkDistribution(0, minDexBase+extra, 0, 0)
	// Base deposit.
	setBals(minDexBase+minCexBase, extra, minDexQuote, minCexQuote)
	checkDistribution(minCexBase-extra, 0, 0, 0)
	// Quote OK
	setBals(minDexBase, minCexBase, minDexQuote*100, minCexQuote*100)
	checkDistribution(0, 0, 0, 0)
	// Quote withdraw. Extra is split for the quote asset. Gotta lower the min
	// transfer a little bit to make this one happen.
	setBals(minDexBase, minCexBase, minDexQuote-perLot.dexQuote+extra, minCexQuote+perLot.dexQuote)
	a.autoRebalanceCfg.MinQuoteTransfer = perLot.dexQuote - extra/2
	checkDistribution(0, 0, 0, perLot.dexQuote-extra/2)
	// Quote deposit
	setBals(minDexBase, minCexBase, minDexQuote+perLot.cexQuote+extra, minCexQuote-perLot.cexQuote)
	checkDistribution(0, 0, perLot.cexQuote+extra/2, 0)

	// Deficit math.
	// Since cex lot is smaller, dex can't use this extra.
	setBals(addBaseFees+perLot.dexBase*2+perLot.cexBase, 0, addQuoteFees+minDexQuote, minCexQuote)
	checkDistribution(perLot.cexBase, 0, 0, 0)
	// Same thing, but with enough for fees, and there's no reason to transfer
	// because it doesn't improve our matchability.
	setBals(perLot.dexBase*3, extra, minDexQuote, minCexQuote)
	checkDistribution(0, 0, 0, 0)
	setBals(addBaseFees+minDexBase, minCexBase, addQuoteFees+perLot.dexQuote*5+perLot.cexQuote*2+extra, 0)
	checkDistribution(0, 0, perLot.cexQuote*2+extra/2, 0)
	setBals(addBaseFees+perLot.dexBase, perLot.cexBase*5+perLot.dexBase+extra, addQuoteFees+minDexQuote, minCexQuote)
	checkDistribution(0, perLot.dexBase+extra, 0, 0)
	setBals(addBaseFees+perLot.dexBase*2, perLot.cexBase*2, addQuoteFees+perLot.dexQuote, perLot.cexQuote*2+perLot.dexQuote+extra)
	checkDistribution(0, 0, 0, perLot.dexQuote+extra/2)

	var epok uint64
	epoch := func() uint64 {
		epok++
		return epok
	}

	checkTransfers := func(expActionTaken bool, expBaseDeposit, expBaseWithdraw, expQuoteDeposit, expQuoteWithdraw uint64) {
		t.Helper()
		defer func() {
			u.wg.Wait()
			cex.withdrawals = nil
			tCore.sends = nil
			u.pendingDeposits = make(map[string]*pendingDeposit)
			u.pendingWithdrawals = make(map[string]*pendingWithdrawal)
			u.pendingDEXOrders = make(map[order.OrderID]*pendingDEXOrder)
		}()

		actionTaken, err := a.tryTransfers(epoch())
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if actionTaken != expActionTaken {
			t.Fatalf("wrong actionTaken result. wanted %t, got %t", expActionTaken, actionTaken)
		}
		var baseDeposit, quoteDeposit *sendArgs
		for _, s := range tCore.sends {
			if s.assetID == baseID {
				baseDeposit = s
			} else {
				quoteDeposit = s
			}
		}
		var baseWithdrawal, quoteWithdrawal *withdrawArgs
		for _, w := range cex.withdrawals {
			if w.assetID == baseID {
				baseWithdrawal = w
			} else {
				quoteWithdrawal = w
			}
		}
		if expBaseDeposit > 0 {
			if baseDeposit == nil {
				t.Fatalf("Missing base deposit")
			}
			if baseDeposit.value != expBaseDeposit {
				t.Fatalf("Wrong value for base deposit. wanted %d, got %d", expBaseDeposit, baseDeposit.value)
			}
		} else if baseDeposit != nil {
			t.Fatalf("Unexpected base deposit")
		}
		if expQuoteDeposit > 0 {
			if quoteDeposit == nil {
				t.Fatalf("Missing quote deposit")
			}
			if quoteDeposit.value != expQuoteDeposit {
				t.Fatalf("Wrong value for quote deposit. wanted %d, got %d", expQuoteDeposit, quoteDeposit.value)
			}
		} else if quoteDeposit != nil {
			t.Fatalf("Unexpected quote deposit")
		}
		if expBaseWithdraw > 0 {
			if baseWithdrawal == nil {
				t.Fatalf("Missing base withdrawal")
			}
			if baseWithdrawal.amt != expBaseWithdraw {
				t.Fatalf("Wrong value for base withdrawal. wanted %d, got %d", expBaseWithdraw, baseWithdrawal.amt)
			}
		} else if baseWithdrawal != nil {
			t.Fatalf("Unexpected base withdrawal")
		}
		if expQuoteWithdraw > 0 {
			if quoteWithdrawal == nil {
				t.Fatalf("Missing quote withdrawal")
			}
			if quoteWithdrawal.amt != expQuoteWithdraw {
				t.Fatalf("Wrong value for quote withdrawal. wanted %d, got %d", expQuoteWithdraw, quoteWithdrawal.amt)
			}
		} else if quoteWithdrawal != nil {
			t.Fatalf("Unexpected quote withdrawal")
		}
	}

	setLots(1, 1)
	setBals(minDexBase, minCexBase, minDexQuote, minCexQuote)
	checkTransfers(false, 0, 0, 0, 0)

	coinID := []byte{0xa0}
	coin := &tCoin{coinID: coinID, value: 1}
	txID := coin.TxID()
	tCore.sendCoin = coin
	tCore.walletTxs[txID] = &asset.WalletTransaction{Confirmed: true}
	cex.confirmedDeposit = &coin.value

	// Base deposit.
	setBals(totalBase, 0, minDexQuote, minCexQuote)
	checkTransfers(true, minCexBase, 0, 0, 0)

	// Base withdrawal
	cex.confirmWithdrawal = &withdrawArgs{txID: txID}
	setBals(0, totalBase, minDexQuote, minCexQuote)
	checkTransfers(true, 0, minDexBase, 0, 0)

	// Quote deposit
	setBals(minDexBase, minCexBase, totalQuote, 0)
	checkTransfers(true, 0, 0, minCexQuote, 0)

	// Quote withdrawal
	setBals(minDexBase, minCexBase, 0, totalQuote)
	checkTransfers(true, 0, 0, 0, minDexQuote)

	// Base deposit, but we need to cancel an order to free up the funds.
	setBals(totalBase, 0, minDexQuote, minCexQuote)
	oid := order.OrderID{0x1b}
	addLocked := func(assetID uint32, val uint64) {
		po := &pendingDEXOrder{}
		po.state.Store(&dexOrderState{
			balanceEffects: &balanceEffects{
				settled: map[uint32]int64{
					assetID: -int64(val),
				},
				locked: map[uint32]uint64{
					assetID: val,
				},
				pending: make(map[uint32]uint64),
			},
			order: &core.Order{
				ID:   oid[:],
				Sell: assetID == baseID,
			},
		})
		u.pendingDEXOrders[oid] = po
	}
	checkCancel := func() {
		if len(tCore.cancelsPlaced) != 1 || tCore.cancelsPlaced[0] != oid {
			t.Fatalf("No cancels placed")
		}
		tCore.cancelsPlaced = nil
	}
	addLocked(baseID, totalBase)
	checkTransfers(true, 0, 0, 0, 0)
	checkCancel()

	setBals(minDexBase, minCexBase, totalQuote, 0)
	addLocked(quoteID, totalQuote)
	checkTransfers(true, 0, 0, 0, 0)
	checkCancel()

	setBals(0, totalBase /* being withdrawn */, minDexQuote, minCexQuote)
	u.pendingWithdrawals["a"] = &pendingWithdrawal{
		assetID:      baseID,
		amtWithdrawn: totalBase,
	}
	// Distribution should indicate a deposit.
	checkDistribution(minCexBase, 0, 0, 0)
	// But freeUpFunds will come up short. No action taken.
	checkTransfers(false, 0, 0, 0, 0)

	setBals(minDexBase, minCexBase, 0, totalQuote)
	u.pendingWithdrawals["a"] = &pendingWithdrawal{
		assetID:      quoteID,
		amtWithdrawn: totalQuote,
	}
	checkDistribution(0, 0, minCexQuote, 0)
	checkTransfers(false, 0, 0, 0, 0)

	u.market = mustParseMarket(&core.Market{})
}

func TestMultiTrade(t *testing.T) {
	const lotSize uint64 = 50e8
	const rateStep uint64 = 1e3
	const currEpoch = 100
	const driftTolerance = 0.001
	sellFees := tFees(1e5, 2e5, 3e5, 4e5)
	buyFees := tFees(5e5, 6e5, 7e5, 8e5)
	orderIDs := make([]order.OrderID, 10)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}

	driftToleranceEdge := func(rate uint64, within bool) uint64 {
		edge := rate + uint64(float64(rate)*driftTolerance)
		if within {
			return edge - rateStep
		}
		return edge + rateStep
	}

	sellPlacements := []*multiTradePlacement{
		{lots: 1, rate: 1e7, counterTradeRate: 0.9e7},
		{lots: 2, rate: 2e7, counterTradeRate: 1.9e7},
		{lots: 3, rate: 3e7, counterTradeRate: 2.9e7},
		{lots: 2, rate: 4e7, counterTradeRate: 3.9e7},
	}

	buyPlacements := []*multiTradePlacement{
		{lots: 1, rate: 4e7, counterTradeRate: 4.1e7},
		{lots: 2, rate: 3e7, counterTradeRate: 3.1e7},
		{lots: 3, rate: 2e7, counterTradeRate: 2.1e7},
		{lots: 2, rate: 1e7, counterTradeRate: 1.1e7},
	}

	// cancelLastPlacement is the same as placements, but with the rate
	// and lots of the last order set to zero, which should cause pending
	// orders at that placementIndex to be cancelled.
	cancelLastPlacement := func(sell bool) []*multiTradePlacement {
		placements := make([]*multiTradePlacement, len(sellPlacements))
		if sell {
			copy(placements, sellPlacements)
		} else {
			copy(placements, buyPlacements)
		}
		placements[len(placements)-1] = &multiTradePlacement{}
		return placements
	}

	// removeLastPlacement simulates a reconfiguration is which the
	// last placement is removed.
	removeLastPlacement := func(sell bool) []*multiTradePlacement {
		placements := make([]*multiTradePlacement, len(sellPlacements))
		if sell {
			copy(placements, sellPlacements)
		} else {
			copy(placements, buyPlacements)
		}
		return placements[:len(placements)-1]
	}

	// reconfigToMorePlacements simulates a reconfiguration in which
	// the lots allocated to the placement at index 1 is reduced by 1.
	reconfigToLessPlacements := func(sell bool) []*multiTradePlacement {
		placements := make([]*multiTradePlacement, len(sellPlacements))
		if sell {
			copy(placements, sellPlacements)
		} else {
			copy(placements, buyPlacements)
		}
		placements[1] = &multiTradePlacement{
			lots:             placements[1].lots - 1,
			rate:             placements[1].rate,
			counterTradeRate: placements[1].counterTradeRate,
		}
		return placements
	}

	pendingOrders := func(sell bool, baseID, quoteID uint32) map[order.OrderID]*pendingDEXOrder {
		var placements []*multiTradePlacement
		if sell {
			placements = sellPlacements
		} else {
			placements = buyPlacements
		}

		orders := map[order.OrderID]*core.Order{
			orderIDs[0]: { // Should cancel, but cannot due to epoch > currEpoch - 2
				Qty:     1 * lotSize,
				Sell:    sell,
				ID:      orderIDs[0][:],
				Rate:    driftToleranceEdge(placements[0].rate, true),
				Epoch:   currEpoch - 1,
				BaseID:  baseID,
				QuoteID: quoteID,
			},
			orderIDs[1]: { // Within tolerance, don't cancel
				Qty:     2 * lotSize,
				Filled:  lotSize,
				Sell:    sell,
				ID:      orderIDs[1][:],
				Rate:    driftToleranceEdge(placements[1].rate, true),
				Epoch:   currEpoch - 2,
				BaseID:  baseID,
				QuoteID: quoteID,
			},
			orderIDs[2]: { // Cancel
				Qty:     lotSize,
				Sell:    sell,
				ID:      orderIDs[2][:],
				Rate:    driftToleranceEdge(placements[2].rate, false),
				Epoch:   currEpoch - 2,
				BaseID:  baseID,
				QuoteID: quoteID,
			},
			orderIDs[3]: { // Within tolerance, don't cancel
				Qty:     lotSize,
				Sell:    sell,
				ID:      orderIDs[3][:],
				Rate:    driftToleranceEdge(placements[3].rate, true),
				Epoch:   currEpoch - 2,
				BaseID:  baseID,
				QuoteID: quoteID,
			},
		}

		toReturn := map[order.OrderID]*pendingDEXOrder{
			orderIDs[0]: { // Should cancel, but cannot due to epoch > currEpoch - 2
				placementIndex:   0,
				counterTradeRate: placements[0].counterTradeRate,
			},
			orderIDs[1]: {
				placementIndex:   1,
				counterTradeRate: placements[1].counterTradeRate,
			},
			orderIDs[2]: {
				placementIndex:   2,
				counterTradeRate: placements[2].counterTradeRate,
			},
			orderIDs[3]: {
				placementIndex:   3,
				counterTradeRate: placements[3].counterTradeRate,
			},
		}

		for id, order := range orders {
			toReturn[id].updateState(order, &balanceEffects{})
		}
		return toReturn
	}

	// secondPendingOrderNotFilled returns the same pending orders as
	// pendingOrders, but with the second order not filled.
	secondPendingOrderNotFilled := func(sell bool, baseID, quoteID uint32) map[order.OrderID]*pendingDEXOrder {
		orders := pendingOrders(sell, baseID, quoteID)
		orders[orderIDs[1]].currentState().order.Filled = 0
		return orders
	}

	// pendingWithSelfMatch returns the same pending orders as pendingOrders,
	// but with an additional order on the other side of the market that
	// would cause a self-match.
	pendingOrdersSelfMatch := func(sell bool, baseID, quoteID uint32) map[order.OrderID]*pendingDEXOrder {
		orders := pendingOrders(sell, baseID, quoteID)
		var rate uint64
		if sell {
			rate = driftToleranceEdge(2e7, true) // 2e7 is the rate of the lowest sell placement
		} else {
			rate = 3e7 // 3e7 is the rate of the highest buy placement
		}
		pendingOrder := &pendingDEXOrder{
			placementIndex: 0,
		}
		pendingOrder.updateState(&core.Order{ // Within tolerance, don't cancel
			Qty:   lotSize,
			Sell:  !sell,
			ID:    orderIDs[4][:],
			Rate:  rate,
			Epoch: currEpoch - 2,
		}, &balanceEffects{})
		orders[orderIDs[4]] = pendingOrder
		return orders
	}

	b2q := calc.BaseToQuote

	/*
	 * The dexBalance and cexBalances fields of this test are set so that they
	 * are at an edge. If any non-zero balance is decreased by 1, the behavior
	 * of the function should change. Each of the "WithDecrement" fields are
	 * the expected result if any of the non-zero balances are decreased by 1.
	 */
	type test struct {
		name    string
		baseID  uint32
		quoteID uint32

		sellDexBalances   map[uint32]uint64
		sellCexBalances   map[uint32]uint64
		sellPlacements    []*multiTradePlacement
		sellPendingOrders map[order.OrderID]*pendingDEXOrder

		buyCexBalances   map[uint32]uint64
		buyDexBalances   map[uint32]uint64
		buyPlacements    []*multiTradePlacement
		buyPendingOrders map[order.OrderID]*pendingDEXOrder

		isAccountLocker               map[uint32]bool
		multiTradeResult              []*core.Order
		multiTradeResultWithDecrement []*core.Order

		expectedOrderIDs              []order.OrderID
		expectedOrderIDsWithDecrement []order.OrderID

		expectedSellPlacements              []*core.QtyRate
		expectedSellPlacementsWithDecrement []*core.QtyRate

		expectedBuyPlacements              []*core.QtyRate
		expectedBuyPlacementsWithDecrement []*core.QtyRate

		expectedCancels              []order.OrderID
		expectedCancelsWithDecrement []order.OrderID
	}

	tests := []*test{
		{
			name:    "non account locker",
			baseID:  42,
			quoteID: 0,

			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 4*lotSize + 4*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, 2*lotSize),
			},
			sellPlacements:    sellPlacements,
			sellPendingOrders: pendingOrders(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
				{Qty: lotSize, Rate: sellPlacements[3].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[1].rate, lotSize) +
					b2q(buyPlacements[2].rate, 2*lotSize) +
					b2q(buyPlacements[3].rate, lotSize) +
					4*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 8 * lotSize,
				0:  0,
			},
			buyPlacements:    buyPlacements,
			buyPendingOrders: pendingOrders(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
				{Qty: lotSize, Rate: buyPlacements[3].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
				{ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[4], orderIDs[5], orderIDs[6],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
		},
		{
			name:    "non account locker, reconfig to less placements",
			baseID:  42,
			quoteID: 0,

			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 3*lotSize + 3*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, 2*lotSize),
			},
			sellPlacements:    reconfigToLessPlacements(true),
			sellPendingOrders: secondPendingOrderNotFilled(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				// {Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
				{Qty: lotSize, Rate: sellPlacements[3].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				// {Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[2].rate, 2*lotSize) +
					b2q(buyPlacements[3].rate, lotSize) +
					3*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 8 * lotSize,
				0:  0,
			},
			buyPlacements:    reconfigToLessPlacements(false),
			buyPendingOrders: secondPendingOrderNotFilled(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				// {Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
				{Qty: lotSize, Rate: buyPlacements[3].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				// {Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[1], orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[1], orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
				// {ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				// {ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[4],
			},
		},
		{
			name:    "non account locker, self-match",
			baseID:  42,
			quoteID: 0,

			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 3*lotSize + 3*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, 2*lotSize),
			},
			sellPlacements:    sellPlacements,
			sellPendingOrders: pendingOrdersSelfMatch(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
				{Qty: lotSize, Rate: sellPlacements[3].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[2].rate, 2*lotSize) +
					b2q(buyPlacements[3].rate, lotSize) +
					3*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 7 * lotSize,
				0:  0,
			},
			buyPlacements:    buyPlacements,
			buyPendingOrders: pendingOrdersSelfMatch(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
				{Qty: lotSize, Rate: buyPlacements[3].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[5][:]},
				{ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[5], orderIDs[6],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[5],
			},
		},
		{
			name:    "non account locker, cancel last placement",
			baseID:  42,
			quoteID: 0,
			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 3*lotSize + 3*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, lotSize),
			},
			sellPlacements:    cancelLastPlacement(true),
			sellPendingOrders: pendingOrders(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[1].rate, lotSize) +
					b2q(buyPlacements[2].rate, 2*lotSize) +
					3*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 7 * lotSize,
				0:  0,
			},
			buyPlacements:    cancelLastPlacement(false),
			buyPendingOrders: pendingOrders(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[3], orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[3], orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
		},
		{
			name:    "non account locker, remove last placement",
			baseID:  42,
			quoteID: 0,
			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 3*lotSize + 3*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, lotSize),
			},
			sellPlacements:    removeLastPlacement(true),
			sellPendingOrders: pendingOrders(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[1].rate, lotSize) +
					b2q(buyPlacements[2].rate, 2*lotSize) +
					3*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 7 * lotSize,
				0:  0,
			},
			buyPlacements:    removeLastPlacement(false),
			buyPendingOrders: pendingOrders(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[3], orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[3], orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[4], orderIDs[5],
			},
		},
		{
			name:    "account locker token",
			baseID:  966001,
			quoteID: 60,
			isAccountLocker: map[uint32]bool{
				966001: true,
				60:     true,
			},

			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				966001: 4 * lotSize,
				966:    4*(sellFees.Max.Swap+sellFees.Max.Refund) + sellFees.funding,
				60:     4 * sellFees.Max.Redeem,
			},
			sellCexBalances: map[uint32]uint64{
				96601: 0,
				60: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 3*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, 2*lotSize),
			},
			sellPlacements:    sellPlacements,
			sellPendingOrders: pendingOrders(true, 966001, 60),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
				{Qty: lotSize, Rate: sellPlacements[3].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: sellPlacements[2].rate},
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				966: 4 * buyFees.Max.Redeem,
				60: b2q(buyPlacements[1].rate, lotSize) +
					b2q(buyPlacements[2].rate, 2*lotSize) +
					b2q(buyPlacements[3].rate, lotSize) +
					4*buyFees.Max.Swap + 4*buyFees.Max.Refund + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				966001: 8 * lotSize,
				0:      0,
			},
			buyPlacements:    buyPlacements,
			buyPendingOrders: pendingOrders(false, 966001, 60),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
				{Qty: lotSize, Rate: buyPlacements[3].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: 2 * lotSize, Rate: buyPlacements[2].rate},
			},

			expectedCancels:              []order.OrderID{orderIDs[2]},
			expectedCancelsWithDecrement: []order.OrderID{orderIDs[2]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
			},
			expectedOrderIDs: []order.OrderID{
				orderIDs[3], orderIDs[4], orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []order.OrderID{
				orderIDs[3], orderIDs[4],
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testWithDecrement := func(sell, decrement, cex bool, assetID uint32) {
				t.Run(fmt.Sprintf("sell=%v, decrement=%v, cex=%v, assetID=%d", sell, decrement, cex, assetID), func(t *testing.T) {
					tCore := newTCore()
					tCore.isAccountLocker = test.isAccountLocker
					tCore.market = &core.Market{
						BaseID:  test.baseID,
						QuoteID: test.quoteID,
						LotSize: lotSize,
					}
					tCore.multiTradeResult = test.multiTradeResult
					if decrement {
						tCore.multiTradeResult = test.multiTradeResultWithDecrement
					}

					var dexBalances, cexBalances map[uint32]uint64
					if sell {
						dexBalances = test.sellDexBalances
						cexBalances = test.sellCexBalances
					} else {
						dexBalances = test.buyDexBalances
						cexBalances = test.buyCexBalances
					}

					adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
						core:            tCore,
						baseDexBalances: dexBalances,
						baseCexBalances: cexBalances,
						mwh: &MarketWithHost{
							Host:    "dex.com",
							BaseID:  test.baseID,
							QuoteID: test.quoteID,
						},
						eventLogDB: &tEventLogDB{},
					})

					var pendingOrders map[order.OrderID]*pendingDEXOrder
					if sell {
						pendingOrders = test.sellPendingOrders
					} else {
						pendingOrders = test.buyPendingOrders
					}

					pendingOrdersCopy := make(map[order.OrderID]*pendingDEXOrder)
					for id, order := range pendingOrders {
						pendingOrdersCopy[id] = order
					}
					adaptor.pendingDEXOrders = pendingOrdersCopy
					adaptor.buyFees = buyFees
					adaptor.sellFees = sellFees

					var placements []*multiTradePlacement
					if sell {
						placements = test.sellPlacements
					} else {
						placements = test.buyPlacements
					}
					res := adaptor.multiTrade(placements, sell, driftTolerance, currEpoch)

					expectedOrderIDs := test.expectedOrderIDs
					if decrement {
						expectedOrderIDs = test.expectedOrderIDsWithDecrement
					}
					if len(res) != len(expectedOrderIDs) {
						t.Fatalf("expected %d orders, got %d", len(expectedOrderIDs), len(res))
					}
					for oid := range res {
						if _, found := res[oid]; !found {
							t.Fatalf("order id %s not in results", oid)
						}
					}

					var expectedPlacements []*core.QtyRate
					if sell {
						expectedPlacements = test.expectedSellPlacements
						if decrement {
							expectedPlacements = test.expectedSellPlacementsWithDecrement
						}
					} else {
						expectedPlacements = test.expectedBuyPlacements
						if decrement {
							expectedPlacements = test.expectedBuyPlacementsWithDecrement
						}
					}
					if len(expectedPlacements) > 0 != (len(tCore.multiTradesPlaced) > 0) {
						t.Fatalf("%s: expected placements %v, got %v", test.name, len(expectedPlacements) > 0, len(tCore.multiTradesPlaced) > 0)
					}
					if len(expectedPlacements) > 0 {
						placements := tCore.multiTradesPlaced[0].Placements
						if !reflect.DeepEqual(placements, expectedPlacements) {
							t.Fatal(spew.Sprintf("%s: expected placements:\n%#+v\ngot:\n%+#v", test.name, expectedPlacements, placements))
						}
					}

					expectedCancels := test.expectedCancels
					if decrement {
						expectedCancels = test.expectedCancelsWithDecrement
					}
					sort.Slice(tCore.cancelsPlaced, func(i, j int) bool {
						return bytes.Compare(tCore.cancelsPlaced[i][:], tCore.cancelsPlaced[j][:]) < 0
					})
					sort.Slice(expectedCancels, func(i, j int) bool {
						return bytes.Compare(expectedCancels[i][:], expectedCancels[j][:]) < 0
					})
					if !reflect.DeepEqual(tCore.cancelsPlaced, expectedCancels) {
						t.Fatalf("expected cancels %v, got %v", expectedCancels, tCore.cancelsPlaced)
					}
				})
			}

			for _, sell := range []bool{true, false} {
				var dexBalances, cexBalances map[uint32]uint64
				if sell {
					dexBalances = test.sellDexBalances
					cexBalances = test.sellCexBalances
				} else {
					dexBalances = test.buyDexBalances
					cexBalances = test.buyCexBalances
				}

				testWithDecrement(sell, false, false, 0)
				for assetID, bal := range dexBalances {
					if bal == 0 {
						continue
					}
					dexBalances[assetID]--
					testWithDecrement(sell, true, false, assetID)
					dexBalances[assetID]++
				}
				for assetID, bal := range cexBalances {
					if bal == 0 {
						continue
					}
					cexBalances[assetID]--
					testWithDecrement(sell, true, true, assetID)
					cexBalances[assetID]++
				}
			}
		})
	}
}

func TestDEXTrade(t *testing.T) {
	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}
	matchIDs := make([]order.MatchID, 5)
	for i := range matchIDs {
		var id order.MatchID
		copy(id[:], encode.RandomBytes(order.MatchIDSize))
		matchIDs[i] = id
	}
	coinIDs := make([]string, 6)
	for i := range coinIDs {
		coinIDs[i] = hex.EncodeToString(encode.RandomBytes(32))
	}

	type matchUpdate struct {
		swapCoin   *dex.Bytes
		redeemCoin *dex.Bytes
		refundCoin *dex.Bytes
		qty, rate  uint64
	}
	newMatchUpdate := func(swapCoin, redeemCoin, refundCoin *string, qty, rate uint64) *matchUpdate {
		stringToBytes := func(s *string) *dex.Bytes {
			if s == nil {
				return nil
			}
			b, _ := hex.DecodeString(*s)
			d := dex.Bytes(b)
			return &d
		}

		return &matchUpdate{
			swapCoin:   stringToBytes(swapCoin),
			redeemCoin: stringToBytes(redeemCoin),
			refundCoin: stringToBytes(refundCoin),
			qty:        qty,
			rate:       rate,
		}
	}

	type orderUpdate struct {
		id                   order.OrderID
		lockedAmt            uint64
		parentAssetLockedAmt uint64
		redeemLockedAmt      uint64
		refundLockedAmt      uint64
		status               order.OrderStatus
		matches              []*matchUpdate
		allFeesConfirmed     bool
	}
	newOrderUpdate := func(id order.OrderID, lockedAmt, parentAssetLockedAmt, redeemLockedAmt, refundLockedAmt uint64, status order.OrderStatus, allFeesConfirmed bool, matches ...*matchUpdate) *orderUpdate {
		return &orderUpdate{
			id:                   id,
			lockedAmt:            lockedAmt,
			parentAssetLockedAmt: parentAssetLockedAmt,
			redeemLockedAmt:      redeemLockedAmt,
			refundLockedAmt:      refundLockedAmt,
			status:               status,
			matches:              matches,
			allFeesConfirmed:     allFeesConfirmed,
		}
	}

	type orderLockedFunds struct {
		id                   order.OrderID
		lockedAmt            uint64
		parentAssetLockedAmt uint64
		redeemLockedAmt      uint64
		refundLockedAmt      uint64
	}
	newOrderLockedFunds := func(id order.OrderID, lockedAmt, parentAssetLockedAmt, redeemLockedAmt, refundLockedAmt uint64) *orderLockedFunds {
		return &orderLockedFunds{
			id:                   id,
			lockedAmt:            lockedAmt,
			parentAssetLockedAmt: parentAssetLockedAmt,
			redeemLockedAmt:      redeemLockedAmt,
			refundLockedAmt:      refundLockedAmt,
		}
	}

	newWalletTx := func(id string, txType asset.TransactionType, amount, fees uint64, confirmed bool) *asset.WalletTransaction {
		return &asset.WalletTransaction{
			ID:        id,
			Amount:    amount,
			Fees:      fees,
			Confirmed: confirmed,
			Type:      txType,
		}
	}

	b2q := calc.BaseToQuote

	type updatesAndBalances struct {
		orderUpdate      *orderUpdate
		txUpdates        []*asset.WalletTransaction
		stats            *RunStats
		numPendingTrades int
	}

	type test struct {
		name               string
		isDynamicSwapper   map[uint32]bool
		initialBalances    map[uint32]uint64
		baseID             uint32
		quoteID            uint32
		sell               bool
		placements         []*multiTradePlacement
		initialLockedFunds []*orderLockedFunds

		postTradeBalances  map[uint32]*BotBalance
		updatesAndBalances []*updatesAndBalances
	}

	const host = "dex.com"
	const lotSize = 1e6
	const rate1, rate2 = 5e7, 6e7
	const swapFees, redeemFees, refundFees = 1000, 1100, 1200
	const sellFees, buyFees = 2000, 50 // booking fees per lot
	const basePerLot = lotSize + sellFees
	quoteLot1, quoteLot2 := b2q(rate1, lotSize), b2q(rate2, lotSize)
	quotePerLot1, quotePerLot2 := quoteLot1+buyFees, quoteLot2+buyFees

	tests := []*test{
		{
			name: "non dynamic swapper, sell",
			initialBalances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			sell:    true,
			baseID:  42,
			quoteID: 0,
			placements: []*multiTradePlacement{
				{lots: 5, rate: rate1},
				{lots: 5, rate: rate2},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], basePerLot*5, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], basePerLot*5, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {1e8 - 10*basePerLot, 10 * basePerLot, 0, 0},
				0:  {1e8, 0, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 8*basePerLot - 2*lotSize - swapFees, 8 * basePerLot, 0, 0},
							0:  {1e8, 0, 2 * quoteLot1, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2*basePerLot, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 5*basePerLot - 5*lotSize - 2*swapFees, 5 * basePerLot, 0, 0},
							0:  {1e8, 0, 2*quoteLot1 + 3*quoteLot2, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*lotSize, swapFees, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2*quoteLot1, redeemFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 5*basePerLot - 5*lotSize - 2*swapFees, 5 * basePerLot, 0, 0},
							0:  {1e8 - redeemFees, 0, 2*quoteLot1 + 3*quoteLot2, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 5*basePerLot - 5*lotSize - 2*swapFees, 5 * basePerLot, 0, 0},
							0:  {1e8 + 2*quoteLot1 - redeemFees, 0, 3 * quoteLot2, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 2*basePerLot - 5*lotSize - 2*swapFees, 2 * basePerLot, 0, 0},
							0:  {1e8 + 2*quoteLot1 - redeemFees, 0, 3 * quoteLot2, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*lotSize, swapFees, true),
						newWalletTx(coinIDs[3], asset.Refund, 3*lotSize, refundFees, false),
						newWalletTx(coinIDs[4], asset.Swap, 2*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], nil, nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 7*lotSize - 3*swapFees - refundFees, 0, 3 * lotSize /* refund */, 0},
							0:  {1e8 + 2*quoteLot1 - redeemFees, 0, 2 * quoteLot2 /* new swap */, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3*lotSize, refundFees, true),
						newWalletTx(coinIDs[4], asset.Swap, 2*lotSize, swapFees, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2*quoteLot2, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - 4*lotSize - 3*swapFees - refundFees, 0, 0, 0},
							0:  {1e8 + 2*quoteLot1 + 2*quoteLot2 - 2*redeemFees, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "non dynamic swapper, buy",
			initialBalances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			baseID:  42,
			quoteID: 0,
			placements: []*multiTradePlacement{
				{lots: 5, rate: rate1},
				{lots: 5, rate: rate2},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5*quotePerLot1, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], 5*quotePerLot2, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {1e8, 0, 0, 0},
				0:  {1e8 - 5*quotePerLot1 - 5*quotePerLot2, 5*quotePerLot1 + 5*quotePerLot2, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*quoteLot1, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quotePerLot1, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8, 0, 2 * lotSize, 0},
							0:  {1e8 - 3*quotePerLot1 - 5*quotePerLot2 - 2*quoteLot1 - swapFees, 3*quotePerLot1 + 5*quotePerLot2, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*quoteLot2, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2*quotePerLot2, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8, 0, 5 * lotSize, 0},
							0:  {1e8 - 3*quotePerLot1 - 2*quotePerLot2 - 2*quoteLot1 - 3*quoteLot2 - 2*swapFees, 3*quotePerLot1 + 2*quotePerLot2, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*quoteLot1, swapFees, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2*lotSize, redeemFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quotePerLot1, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*quoteLot1, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - redeemFees, 0, 5 * lotSize, 0},
							0:  {1e8 - 3*quotePerLot1 - 2*quotePerLot2 - 2*quoteLot1 - 3*quoteLot2 - 2*swapFees, 3*quotePerLot1 + 2*quotePerLot2, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2*lotSize, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quotePerLot1, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2*lotSize - redeemFees, 0, 3 * lotSize, 0},
							0:  {1e8 - 3*quotePerLot1 - 2*quotePerLot2 - 2*quoteLot1 - 3*quoteLot2 - 2*swapFees, 3*quotePerLot1 + 2*quotePerLot2, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2*lotSize - redeemFees, 0, 3 * lotSize, 0},
							0:  {1e8 - 2*quotePerLot2 - 2*quoteLot1 - 3*quoteLot2 - 2*swapFees, 2 * quotePerLot2, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*quoteLot2, swapFees, true),
						newWalletTx(coinIDs[3], asset.Refund, 3*quoteLot2, refundFees, false),
						newWalletTx(coinIDs[4], asset.Swap, 2*quoteLot2, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], nil, nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2*lotSize - redeemFees, 0, 2 * lotSize, 0},
							0:  {1e8 - 2*quoteLot1 - 5*quoteLot2 - 3*swapFees - refundFees, 0, 3 * quoteLot2, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3*quoteLot2, refundFees, true),
						newWalletTx(coinIDs[4], asset.Swap, 2*quoteLot2, swapFees, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2*lotSize, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 4*lotSize - 2*redeemFees, 0, 0, 0},
							0:  {1e8 - 2*quoteLot1 - 2*quoteLot2 - 3*swapFees - refundFees, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, sell",
			initialBalances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			sell:    true,
			baseID:  60,
			quoteID: 966001,
			placements: []*multiTradePlacement{
				{lots: 5, rate: rate1},
				{lots: 5, rate: rate2},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[1], 5*basePerLot, 0, 5*redeemFees, 5*refundFees),
				newOrderLockedFunds(orderIDs[0], 5*basePerLot, 0, 5*redeemFees, 5*refundFees),
			},
			postTradeBalances: map[uint32]*BotBalance{
				966001: {1e8, 0, 0, 0},
				966:    {1e8 - 10*redeemFees, 10 * redeemFees, 0, 0},
				60:     {1e8 - 10*(basePerLot+refundFees), 10 * (basePerLot + refundFees), 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 5*redeemFees, 5*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, 2 * quoteLot1, 0},
							966:    {1e8 - 10*redeemFees, 10 * redeemFees, 0, 0},
							60:     {1e8 - 8*(basePerLot+refundFees) - 2*lotSize - 2*refundFees - swapFees, 8*(basePerLot+refundFees) + 2*refundFees, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2*basePerLot, 0, 5*redeemFees, 5*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, 2*quoteLot1 + 3*quoteLot2, 0},
							966:    {1e8 - 10*redeemFees, 10 * redeemFees, 0, 0},
							60:     {1e8 - 5*(basePerLot+refundFees) - 5*lotSize - 5*refundFees - 2*swapFees, 5*(basePerLot+refundFees) + 5*refundFees, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*lotSize, swapFees, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2*quoteLot1, redeemFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 3*redeemFees, 3*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, 2*quoteLot1 + 3*quoteLot2, 0},
							966:    {1e8 - 9*redeemFees, 8 * redeemFees, 0, 0},
							60:     {1e8 - 5*(basePerLot+refundFees) - 5*lotSize - 3*refundFees - 2*swapFees, 5*(basePerLot+refundFees) + 3*refundFees, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2*quoteLot1, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*basePerLot, 0, 3*redeemFees, 3*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + 2*quoteLot1, 0, 3 * quoteLot2, 0},
							966:    {1e8 - 9*redeemFees, 8 * redeemFees, 0, 0},
							60:     {1e8 - 5*(basePerLot+refundFees) - 5*lotSize - 3*refundFees - 2*swapFees, 5*(basePerLot+refundFees) + 3*refundFees, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + 2*quoteLot1, 0, 3 * quoteLot2, 0},
							966:    {1e8 - 6*redeemFees, 5 * redeemFees, 0, 0},
							60:     {1e8 - 2*(basePerLot+refundFees) - 5*lotSize - 3*refundFees - 2*swapFees, 2*(basePerLot+refundFees) + 3*refundFees, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*lotSize, swapFees, true),
						newWalletTx(coinIDs[3], asset.Refund, 3*lotSize, refundFees, false),
						newWalletTx(coinIDs[4], asset.Swap, 2*lotSize, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 2*redeemFees, 2*refundFees, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], nil, nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + 2*quoteLot1, 0, 2 * quoteLot2, 0},
							966:    {1e8 - 3*redeemFees, 2 * redeemFees, 0, 0},
							60:     {1e8 - 7*lotSize - 2*refundFees - 3*swapFees - refundFees, 2 * refundFees, 3 * lotSize, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3*lotSize, refundFees, true),
						newWalletTx(coinIDs[4], asset.Swap, 2*lotSize, swapFees, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2*quoteLot2, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 0, 0), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 0, 0)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + 2*quoteLot1 + 2*quoteLot2, 0, 0, 0},
							966:    {1e8 - 2*redeemFees, 0, 0, 0},
							60:     {1e8 - 4*lotSize - 3*swapFees - refundFees, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, buy",
			initialBalances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			baseID:  60,
			quoteID: 966001,
			placements: []*multiTradePlacement{
				{lots: 5, rate: rate1},
				{lots: 5, rate: rate2},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5*quoteLot1, 5*buyFees, 5*redeemFees, 5*refundFees),
				newOrderLockedFunds(orderIDs[1], 5*quoteLot2, 5*buyFees, 5*redeemFees, 5*refundFees),
			},
			postTradeBalances: map[uint32]*BotBalance{
				966001: {1e8 - 5*quoteLot1 - 5*quoteLot2, 5*quoteLot1 + 5*quoteLot2, 0, 0},
				966:    {1e8 - 10*(buyFees+refundFees), 10 * (buyFees + refundFees), 0, 0},
				60:     {1e8 - 10*redeemFees, 10 * redeemFees, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*quoteLot1, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quoteLot1, 3*buyFees, 5*redeemFees, 5*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 5*quoteLot1 - 5*quoteLot2, 3*quoteLot1 + 5*quoteLot2, 0, 0},
							966:    {1e8 - 8*(buyFees+refundFees) - swapFees - 2*refundFees, 8*(buyFees+refundFees) + 2*refundFees, 0, 0},
							60:     {1e8 - 10*redeemFees, 10 * redeemFees, 2 * lotSize, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*quoteLot2, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2*quoteLot2, 2*buyFees, 5*redeemFees, 5*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 5*quoteLot1 - 5*quoteLot2, 3*quoteLot1 + 2*quoteLot2, 0, 0},
							966:    {1e8 - 5*(buyFees+refundFees) - 2*swapFees - 5*refundFees, 5*(buyFees+refundFees) + 5*refundFees, 0, 0},
							60:     {1e8 - 10*redeemFees, 10 * redeemFees, 5 * lotSize, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2*quoteLot1, swapFees, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2*lotSize, redeemFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quoteLot1, 3*buyFees, 3*redeemFees, 3*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 5*quoteLot1 - 5*quoteLot2, 3*quoteLot1 + 2*quoteLot2, 0, 0},
							966:    {1e8 - 5*(buyFees+refundFees) - 2*swapFees - 3*refundFees, 5*(buyFees+refundFees) + 3*refundFees, 0, 0},
							60:     {1e8 - 8*redeemFees - redeemFees, 8 * redeemFees, 5 * lotSize, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2*lotSize, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3*quoteLot1, 3*buyFees, 3*redeemFees, 3*refundFees, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 5*quoteLot1 - 5*quoteLot2, 3*quoteLot1 + 2*quoteLot2, 0, 0},
							966:    {1e8 - 5*(buyFees+refundFees) - 2*swapFees - 3*refundFees, 5*(buyFees+refundFees) + 3*refundFees, 0, 0},
							60:     {1e8 + 2*lotSize - 8*redeemFees - redeemFees, 8 * redeemFees, 3 * lotSize, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2*lotSize, rate1)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 2*quoteLot1 - 5*quoteLot2, 2 * quoteLot2, 0, 0},
							966:    {1e8 - 2*(buyFees+refundFees) - 2*swapFees - 3*refundFees, 2*(buyFees+refundFees) + 3*refundFees, 0, 0},
							60:     {1e8 + 2*lotSize - 5*redeemFees - redeemFees, 5 * redeemFees, 3 * lotSize, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3*quoteLot2, swapFees, true),
						newWalletTx(coinIDs[3], asset.Refund, 3*quoteLot2, refundFees, false),
						newWalletTx(coinIDs[4], asset.Swap, 2*quoteLot2, swapFees, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 2*redeemFees, 2*refundFees, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], nil, nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 2*quoteLot1 - 5*quoteLot2, 0, 3 * quoteLot2, 0},
							966:    {1e8 - 3*swapFees - 3*refundFees, 2 * refundFees, 0, 0},
							60:     {1e8 + 2*lotSize - 2*redeemFees - redeemFees, 2 * redeemFees, 2 * lotSize, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3*quoteLot2, refundFees, true),
						newWalletTx(coinIDs[4], asset.Swap, 2*quoteLot2, swapFees, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2*lotSize, redeemFees, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3*lotSize, rate2),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2*lotSize, rate2)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - 2*quoteLot1 - 2*quoteLot2, 0, 0, 0},
							966:    {1e8 - 3*swapFees - 1*refundFees, 0, 0, 0},
							60:     {1e8 + 4*lotSize - 2*redeemFees, 0, 0, 0},
						},
					},
				},
			},
		},
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCore.market = &core.Market{
			BaseID:  test.baseID,
			QuoteID: test.quoteID,
			LotSize: lotSize,
		}
		tCore.isDynamicSwapper = test.isDynamicSwapper

		multiTradeResult := make([]*core.Order, 0, len(test.initialLockedFunds))
		for i, o := range test.initialLockedFunds {
			multiTradeResult = append(multiTradeResult, &core.Order{
				Host:                 host,
				BaseID:               test.baseID,
				QuoteID:              test.quoteID,
				Sell:                 test.sell,
				LockedAmt:            o.lockedAmt,
				ID:                   o.id[:],
				ParentAssetLockedAmt: o.parentAssetLockedAmt,
				RedeemLockedAmt:      o.redeemLockedAmt,
				RefundLockedAmt:      o.refundLockedAmt,
				Rate:                 test.placements[i].rate,
				Qty:                  test.placements[i].lots * lotSize,
			})
		}
		tCore.multiTradeResult = multiTradeResult

		// These don't effect the test, but need to be non-nil.
		tCore.singleLotBuyFees = tFees(0, 0, 0, 0)
		tCore.singleLotSellFees = tFees(0, 0, 0, 0)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID(host, test.baseID, test.quoteID)
		eventLogDB := newTEventLogDB()
		adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
			botID:           botID,
			core:            tCore,
			baseDexBalances: test.initialBalances,
			mwh: &MarketWithHost{
				Host:    host,
				BaseID:  test.baseID,
				QuoteID: test.quoteID,
			},
			eventLogDB: eventLogDB,
		})
		_, err := adaptor.Connect(ctx)
		if err != nil {
			t.Fatalf("%s: Connect error: %v", test.name, err)
		}

		orders := adaptor.multiTrade(test.placements, test.sell, 0.01, 100)
		if len(orders) == 0 {
			t.Fatalf("%s: multi trade did not place orders", test.name)
		}

		checkBalances := func(expected map[uint32]*BotBalance, updateNum int) {
			t.Helper()
			stats := adaptor.stats()
			for assetID, expectedBal := range expected {
				bal := adaptor.DEXBalance(assetID)
				statsBal := stats.DEXBalances[assetID]
				if *statsBal != *bal {
					t.Fatalf("%s: stats bal != bal for asset %d. stats bal: %+v, bal: %+v", test.name, assetID, statsBal, bal)
				}
				if *bal != *expectedBal {
					var updateStr string
					if updateNum <= 0 {
						updateStr = "post trade"
					} else {
						updateStr = fmt.Sprintf("after update #%d", updateNum)
					}
					t.Fatalf("%s: unexpected asset %d balance %s. want %+v, got %+v",
						test.name, assetID, updateStr, expectedBal, bal)
				}
			}
		}

		// Check that the correct initial events are logged
		oidToEventID := make(map[order.OrderID]uint64)
		for i, trade := range test.placements {
			o := test.initialLockedFunds[i]
			oidToEventID[o.id] = uint64(i + 1)
			e := &MarketMakingEvent{
				ID: uint64(i + 1),
				DEXOrderEvent: &DEXOrderEvent{
					ID:           o.id.String(),
					Rate:         trade.rate,
					Qty:          trade.lots * lotSize,
					Sell:         test.sell,
					Transactions: []*asset.WalletTransaction{},
				},
				Pending: true,
			}

			if !eventLogDB.storedEventAtIndexEquals(e, i) {
				t.Fatalf("%s: unexpected event logged. want:\n%+v,\ngot:\n%+v", test.name, e, eventLogDB.latestStoredEvent())
			}
		}

		checkBalances(test.postTradeBalances, 0)

		for i, update := range test.updatesAndBalances {
			tCore.walletTxsMtx.Lock()
			for _, txUpdate := range update.txUpdates {
				tCore.walletTxs[txUpdate.ID] = txUpdate
			}
			tCore.walletTxsMtx.Unlock()

			o := &core.Order{
				Host:                 host,
				BaseID:               test.baseID,
				QuoteID:              test.quoteID,
				Sell:                 test.sell,
				LockedAmt:            update.orderUpdate.lockedAmt,
				ID:                   update.orderUpdate.id[:],
				ParentAssetLockedAmt: update.orderUpdate.parentAssetLockedAmt,
				RedeemLockedAmt:      update.orderUpdate.redeemLockedAmt,
				RefundLockedAmt:      update.orderUpdate.refundLockedAmt,
				Status:               update.orderUpdate.status,
				Matches:              make([]*core.Match, len(update.orderUpdate.matches)),
				AllFeesConfirmed:     update.orderUpdate.allFeesConfirmed,
			}

			for i, matchUpdate := range update.orderUpdate.matches {
				o.Matches[i] = &core.Match{
					Rate: matchUpdate.rate,
					Qty:  matchUpdate.qty,
				}
				if matchUpdate.swapCoin != nil {
					o.Matches[i].Swap = &core.Coin{
						ID: *matchUpdate.swapCoin,
					}
				}
				if matchUpdate.redeemCoin != nil {
					o.Matches[i].Redeem = &core.Coin{
						ID: *matchUpdate.redeemCoin,
					}
				}
				if matchUpdate.refundCoin != nil {
					o.Matches[i].Refund = &core.Coin{
						ID: *matchUpdate.refundCoin,
					}
				}
			}

			note := core.OrderNote{
				Order: o,
			}
			tCore.noteFeed <- &note
			tCore.noteFeed <- &core.BondPostNote{} // dummy note

			checkBalances(update.stats.DEXBalances, i+1)

			stats := adaptor.stats()
			stats.CEXBalances = nil
			stats.StartTime = 0

			if !reflect.DeepEqual(stats.DEXBalances, update.stats.DEXBalances) {
				t.Fatalf("%s: stats mismatch after update %d.\nwant: %+v\n\ngot: %+v", test.name, i+1, update.stats, stats)
			}

			if len(adaptor.pendingDEXOrders) != update.numPendingTrades {
				t.Fatalf("%s: update #%d, expected %d pending trades, got %d", test.name, i+1, update.numPendingTrades, len(adaptor.pendingDEXOrders))
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestDeposit(t *testing.T) {
	type test struct {
		name              string
		isWithdrawer      bool
		isDynamicSwapper  bool
		depositAmt        uint64
		sendCoin          *tCoin
		unconfirmedTx     *asset.WalletTransaction
		confirmedTx       *asset.WalletTransaction
		receivedAmt       uint64
		initialDEXBalance uint64
		initialCEXBalance uint64
		assetID           uint32
		initialEvent      *MarketMakingEvent
		postConfirmEvent  *MarketMakingEvent

		preConfirmDEXBalance  *BotBalance
		preConfirmCEXBalance  *BotBalance
		postConfirmDEXBalance *BotBalance
		postConfirmCEXBalance *BotBalance
	}

	coinID := encode.RandomBytes(32)
	txID := hex.EncodeToString(coinID)

	tests := []test{
		{
			name:         "withdrawer, not dynamic swapper",
			assetID:      42,
			isWithdrawer: true,
			depositAmt:   1e6,
			sendCoin: &tCoin{
				coinID: coinID,
				value:  1e6 - 2000,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:     txID,
				Amount: 1e6 - 2000,
				Fees:   2000,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:     txID,
				Amount: 1e6 - 2000,
				Fees:   2000,
			},
			receivedAmt:       1e6 - 2000,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &BotBalance{
				Available: 2e6,
			},
			preConfirmCEXBalance: &BotBalance{
				Available: 1e6,
				Pending:   1e6 - 2000,
			},
			postConfirmDEXBalance: &BotBalance{
				Available: 2e6,
			},
			postConfirmCEXBalance: &BotBalance{
				Available: 2e6 - 2000,
			},
			initialEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  true,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6 - 2000,
						Fees:   2000,
					},
				},
			},
			postConfirmEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  false,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6 - 2000,
						Fees:   2000,
					},
					CEXCredit: 1e6 - 2000,
				},
			},
		},
		{
			name:       "not withdrawer, not dynamic swapper",
			assetID:    42,
			depositAmt: 1e6,
			sendCoin: &tCoin{
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:     txID,
				Amount: 1e6,
				Fees:   2000,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:     txID,
				Amount: 1e6,
				Fees:   2000,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &BotBalance{
				Available: 2e6 - 2000,
			},
			preConfirmCEXBalance: &BotBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &BotBalance{
				Available: 2e6 - 2000,
			},
			postConfirmCEXBalance: &BotBalance{
				Available: 2e6,
			},
			initialEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  true,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6,
						Fees:   2000,
					},
				},
			},
			postConfirmEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  false,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6,
						Fees:   2000,
					},
					CEXCredit: 1e6,
				},
			},
		},
		{
			name:             "not withdrawer, dynamic swapper",
			assetID:          42,
			isDynamicSwapper: true,
			depositAmt:       1e6,
			sendCoin: &tCoin{
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      4000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: true,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &BotBalance{
				Available: 2e6 - 4000,
			},
			preConfirmCEXBalance: &BotBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &BotBalance{
				Available: 2e6 - 2000,
			},
			postConfirmCEXBalance: &BotBalance{
				Available: 2e6,
			},
			initialEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 4000,
				Pending:  true,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6,
						Fees:   4000,
					},
				},
			},
			postConfirmEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  false,
				DepositEvent: &DepositEvent{
					AssetID: 42,
					Transaction: &asset.WalletTransaction{
						ID:        txID,
						Amount:    1e6,
						Fees:      2000,
						Confirmed: true,
					},
					CEXCredit: 1e6,
				},
			},
		},
		{
			name:             "not withdrawer, dynamic swapper, token",
			assetID:          966001,
			isDynamicSwapper: true,
			depositAmt:       1e6,
			sendCoin: &tCoin{
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      4000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: true,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &BotBalance{
				Available: 2e6,
			},
			preConfirmCEXBalance: &BotBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &BotBalance{
				Available: 2e6,
			},
			postConfirmCEXBalance: &BotBalance{
				Available: 2e6,
			},
			initialEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 4000,
				Pending:  true,
				DepositEvent: &DepositEvent{
					AssetID: 966001,
					Transaction: &asset.WalletTransaction{
						ID:     txID,
						Amount: 1e6,
						Fees:   4000,
					},
				},
			},
			postConfirmEvent: &MarketMakingEvent{
				ID:       1,
				BaseFees: 2000,
				Pending:  false,
				DepositEvent: &DepositEvent{
					AssetID: 966001,
					Transaction: &asset.WalletTransaction{
						ID:        txID,
						Amount:    1e6,
						Fees:      2000,
						Confirmed: true,
					},
					CEXCredit: 1e6,
				},
			},
		},
	}

	runTest := func(test *test) {
		t.Run(test.name, func(t *testing.T) {
			tCore := newTCore()
			tCore.isWithdrawer[test.assetID] = test.isWithdrawer
			tCore.isDynamicSwapper[test.assetID] = test.isDynamicSwapper
			tCore.setAssetBalances(map[uint32]uint64{test.assetID: test.initialDEXBalance, 0: 2e6, 966: 2e6})
			tCore.walletTxsMtx.Lock()
			tCore.walletTxs[test.unconfirmedTx.ID] = test.unconfirmedTx
			tCore.walletTxsMtx.Unlock()
			tCore.sendCoin = test.sendCoin

			tCEX := newTCEX()
			tCEX.balances[test.assetID] = &libxc.ExchangeBalance{
				Available: test.initialCEXBalance,
			}
			tCEX.balances[0] = &libxc.ExchangeBalance{
				Available: 2e6,
			}
			tCEX.balances[966] = &libxc.ExchangeBalance{
				Available: 1e8,
			}

			dexBalances := map[uint32]uint64{
				test.assetID: test.initialDEXBalance,
				0:            2e6,
				966:          2e6,
			}
			cexBalances := map[uint32]uint64{
				0:   2e6,
				966: 1e8,
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			botID := dexMarketID("host1", test.assetID, 0)
			eventLogDB := newTEventLogDB()
			adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
				botID:           botID,
				core:            tCore,
				cex:             tCEX,
				baseDexBalances: dexBalances,
				baseCexBalances: cexBalances,
				mwh: &MarketWithHost{
					Host:    "host1",
					BaseID:  test.assetID,
					QuoteID: 0,
				},
				eventLogDB: eventLogDB,
			})
			_, err := adaptor.Connect(ctx)
			if err != nil {
				t.Fatalf("%s: Connect error: %v", test.name, err)
			}

			err = adaptor.deposit(ctx, test.assetID, test.depositAmt)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}

			preConfirmBal := adaptor.DEXBalance(test.assetID)
			if *preConfirmBal != *test.preConfirmDEXBalance {
				t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
			}

			if test.assetID == 966001 {
				preConfirmParentBal := adaptor.DEXBalance(966)
				if preConfirmParentBal.Available != 2e6-test.unconfirmedTx.Fees {
					t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
				}
			}

			if !eventLogDB.latestStoredEventEquals(test.initialEvent) {
				t.Fatalf("%s: unexpected event logged. want:\n%+v,\ngot:\n%+v", test.name, test.initialEvent, eventLogDB.latestStoredEvent())
			}

			tCore.walletTxsMtx.Lock()
			tCore.walletTxs[test.unconfirmedTx.ID] = test.confirmedTx
			tCore.walletTxsMtx.Unlock()

			tCEX.confirmDepositMtx.Lock()
			tCEX.confirmedDeposit = &test.receivedAmt
			tCEX.confirmDepositMtx.Unlock()

			adaptor.confirmDeposit(ctx, txID)

			checkPostConfirmBalance := func() error {
				postConfirmBal := adaptor.DEXBalance(test.assetID)
				if *postConfirmBal != *test.postConfirmDEXBalance {
					return fmt.Errorf("%s: unexpected post confirm dex balance. want %d, got %d", test.name, test.postConfirmDEXBalance, postConfirmBal.Available)
				}

				if test.assetID == 966001 {
					postConfirmParentBal := adaptor.DEXBalance(966)
					if postConfirmParentBal.Available != 2e6-test.confirmedTx.Fees {
						return fmt.Errorf("%s: unexpected post confirm fee balance. want %d, got %d", test.name, 2e6-test.confirmedTx.Fees, postConfirmParentBal.Available)
					}
				}
				return nil
			}

			tryWithTimeout := func(f func() error) {
				t.Helper()
				var err error
				for i := 0; i < 20; i++ {
					time.Sleep(100 * time.Millisecond)
					err = f()
					if err == nil {
						return
					}
				}
				t.Fatal(err)
			}

			// Synchronizing because the event may not yet be when confirmDeposit
			// returns if two calls to confirmDeposit happen in parallel.
			tryWithTimeout(func() error {
				err = checkPostConfirmBalance()
				if err != nil {
					return err
				}

				if !eventLogDB.latestStoredEventEquals(test.postConfirmEvent) {
					return fmt.Errorf("%s: unexpected event logged. want:\n%+v,\ngot:\n%+v", test.name, test.postConfirmEvent, eventLogDB.latestStoredEvent())
				}
				return nil
			})
		})
	}

	for _, test := range tests {
		runTest(&test)
	}
}

func TestWithdraw(t *testing.T) {
	assetID := uint32(42)
	coinID := encode.RandomBytes(32)
	txID := hex.EncodeToString(coinID)
	withdrawalID := hex.EncodeToString(encode.RandomBytes(32))

	type test struct {
		name              string
		withdrawAmt       uint64
		tx                *asset.WalletTransaction
		initialDEXBalance uint64
		initialCEXBalance uint64

		preConfirmDEXBalance  *BotBalance
		preConfirmCEXBalance  *BotBalance
		postConfirmDEXBalance *BotBalance
		postConfirmCEXBalance *BotBalance

		initialEvent     *MarketMakingEvent
		postConfirmEvent *MarketMakingEvent
	}

	tests := []test{
		{
			name:        "ok",
			withdrawAmt: 1e6,
			tx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    0.9e6 - 2000,
				Fees:      2000,
				Confirmed: true,
			},
			initialCEXBalance: 3e6,
			initialDEXBalance: 1e6,
			preConfirmDEXBalance: &BotBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			preConfirmCEXBalance: &BotBalance{
				Available: 1.9e6,
			},
			postConfirmDEXBalance: &BotBalance{
				Available: 1.9e6 - 2000,
			},
			postConfirmCEXBalance: &BotBalance{
				Available: 2e6,
			},
			initialEvent: &MarketMakingEvent{
				ID:      1,
				Pending: true,
				WithdrawalEvent: &WithdrawalEvent{
					AssetID:  42,
					CEXDebit: 1e6,
					ID:       withdrawalID,
				},
			},
			postConfirmEvent: &MarketMakingEvent{
				ID:       1,
				Pending:  false,
				BaseFees: 0.1e6 + 2000,
				WithdrawalEvent: &WithdrawalEvent{
					AssetID:  42,
					CEXDebit: 1e6,
					ID:       withdrawalID,
					Transaction: &asset.WalletTransaction{
						ID:        txID,
						Amount:    0.9e6 - 2000,
						Fees:      2000,
						Confirmed: true,
					},
				},
			},
		},
	}

	runTest := func(test *test) {
		tCore := newTCore()

		tCore.walletTxsMtx.Lock()
		tCore.walletTxs[test.tx.ID] = test.tx
		tCore.walletTxsMtx.Unlock()

		tCEX := newTCEX()

		dexBalances := map[uint32]uint64{
			assetID: test.initialDEXBalance,
			0:       2e6,
		}
		cexBalances := map[uint32]uint64{
			assetID: test.initialCEXBalance,
			966:     1e8,
		}

		tCEX.withdrawalID = withdrawalID

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID("host1", assetID, 0)
		eventLogDB := newTEventLogDB()
		adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
			botID:           botID,
			core:            tCore,
			cex:             tCEX,
			baseDexBalances: dexBalances,
			baseCexBalances: cexBalances,
			mwh: &MarketWithHost{
				Host:    "host1",
				BaseID:  assetID,
				QuoteID: 0,
			},
			eventLogDB: eventLogDB,
		})
		_, err := adaptor.Connect(ctx)
		if err != nil {
			t.Fatalf("%s: Connect error: %v", test.name, err)
		}

		err = adaptor.withdraw(ctx, assetID, test.withdrawAmt)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if !eventLogDB.latestStoredEventEquals(test.initialEvent) {
			t.Fatalf("%s: unexpected event logged. want:\n%+v,\ngot:\n%+v", test.name, test.initialEvent, eventLogDB.latestStoredEvent())
		}
		preConfirmBal := adaptor.DEXBalance(assetID)
		if *preConfirmBal != *test.preConfirmDEXBalance {
			t.Fatalf("%s: unexpected pre confirm dex balance. want %+v, got %+v", test.name, test.preConfirmDEXBalance, preConfirmBal)
		}

		tCEX.confirmWithdrawalMtx.Lock()
		tCEX.confirmWithdrawal = &withdrawArgs{
			assetID: assetID,
			amt:     test.withdrawAmt,
			txID:    test.tx.ID,
		}
		tCEX.confirmWithdrawalMtx.Unlock()

		adaptor.confirmWithdrawal(ctx, withdrawalID)

		tryWithTimeout := func(f func() error) {
			t.Helper()
			var err error
			for i := 0; i < 20; i++ {
				time.Sleep(100 * time.Millisecond)
				err = f()
				if err == nil {
					return
				}
			}
			t.Fatal(err)
		}

		// Synchronizing because the event may not yet be when confirmWithdrawal
		// returns if two calls to confirmWithdrawal happen in parallel.
		tryWithTimeout(func() error {
			postConfirmBal := adaptor.DEXBalance(assetID)
			if *postConfirmBal != *test.postConfirmDEXBalance {
				return fmt.Errorf("%s: unexpected post confirm dex balance. want %+v, got %+v", test.name, test.postConfirmDEXBalance, postConfirmBal)
			}
			if !eventLogDB.latestStoredEventEquals(test.postConfirmEvent) {
				return fmt.Errorf("%s: unexpected event logged. want:\n%s,\ngot:\n%s", test.name, spew.Sdump(test.postConfirmEvent), spew.Sdump(eventLogDB.latestStoredEvent()))
			}
			return nil
		})
	}

	for _, test := range tests {
		runTest(&test)
	}
}

func TestCEXTrade(t *testing.T) {
	baseID := uint32(42)
	quoteID := uint32(0)
	tradeID := "123"

	type updateAndStats struct {
		update *libxc.Trade
		stats  *RunStats
		event  *MarketMakingEvent
	}

	type test struct {
		name     string
		sell     bool
		rate     uint64
		qty      uint64
		balances map[uint32]uint64

		wantErr           bool
		postTradeBalances map[uint32]*BotBalance
		postTradeEvent    *MarketMakingEvent
		updates           []*updateAndStats
	}

	b2q := calc.BaseToQuote

	tests := []*test{
		{
			name: "fully filled sell",
			sell: true,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {
					Available: 5e6,
					Locked:    5e6,
				},
				0: {
					Available: 1e7,
				},
			},
			postTradeEvent: &MarketMakingEvent{
				ID:      1,
				Pending: true,
				CEXOrderEvent: &CEXOrderEvent{
					ID:   tradeID,
					Rate: 5e7,
					Qty:  5e6,
					Sell: true,
				},
			},
			updates: []*updateAndStats{
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    true,
						BaseDelta:  -3e6,
						QuoteDelta: 1.6e6,
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        true,
							BaseFilled:  3e6,
							QuoteFilled: 1.6e6,
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {5e6, 5e6 - 3e6, 0, 0},
							0:  {1e7 + 1.6e6, 0, 0, 0},
						},
					},
				},
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5e6,
						QuoteFilled: 2.8e6,
						Complete:    true,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    false,
						BaseDelta:  -5e6,
						QuoteDelta: 2.8e6,
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        true,
							BaseFilled:  5e6,
							QuoteFilled: 2.8e6,
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {5e6, 0, 0, 0},
							0:  {1e7 + 2.8e6, 0, 0, 0},
						},
					},
				},
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5e6,
						QuoteFilled: 2.8e6,
						Complete:    true,
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {5e6, 0, 0, 0},
							0:  {1e7 + 2.8e6, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "partially filled sell",
			sell: true,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {
					Available: 5e6,
					Locked:    5e6,
				},
				0: {
					Available: 1e7,
				},
			},
			postTradeEvent: &MarketMakingEvent{
				ID:      1,
				Pending: true,
				CEXOrderEvent: &CEXOrderEvent{
					ID:   tradeID,
					Rate: 5e7,
					Qty:  5e6,
					Sell: true,
				},
			},
			updates: []*updateAndStats{
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
						Complete:    true,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    false,
						BaseDelta:  -3e6,
						QuoteDelta: 1.6e6,
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        true,
							BaseFilled:  3e6,
							QuoteFilled: 1.6e6,
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {7e6, 0, 0, 0},
							0:  {1e7 + 1.6e6, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "fully filled buy",
			sell: false,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {
					Available: 1e7,
				},
				0: {
					Available: 1e7 - b2q(5e7, 5e6),
					Locked:    b2q(5e7, 5e6),
				},
			},
			postTradeEvent: &MarketMakingEvent{
				ID:      1,
				Pending: true,
				CEXOrderEvent: &CEXOrderEvent{
					ID:   tradeID,
					Rate: 5e7,
					Qty:  5e6,
					Sell: false,
				},
			},
			updates: []*updateAndStats{
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    true,
						BaseDelta:  3e6,
						QuoteDelta: -1.6e6,
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        false,
							BaseFilled:  3e6,
							QuoteFilled: 1.6e6,
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {1e7 + 3e6, 0, 0, 0},
							0:  {1e7 - b2q(5e7, 5e6), b2q(5e7, 5e6) - 1.6e6, 0, 0},
						},
					},
				},
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5.1e6,
						QuoteFilled: calc.BaseToQuote(5e7, 5e6),
						Complete:    true,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    false,
						BaseDelta:  5.1e6,
						QuoteDelta: -int64(b2q(5e7, 5e6)),
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        false,
							BaseFilled:  5.1e6,
							QuoteFilled: calc.BaseToQuote(5e7, 5e6),
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {1e7 + 5.1e6, 0, 0, 0},
							0:  {1e7 - b2q(5e7, 5e6), 0, 0, 0},
						},
					},
				},
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5.1e6,
						QuoteFilled: b2q(5e7, 5e6),
						Complete:    true,
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {1e7 + 5.1e6, 0, 0, 0},
							0:  {1e7 - b2q(5e7, 5e6), 0, 0, 0},
						},
					},
				},
			},
		},
		{
			name: "partially filled buy",
			sell: false,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {
					Available: 1e7,
				},
				0: {
					Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
					Locked:    calc.BaseToQuote(5e7, 5e6),
				},
			},
			postTradeEvent: &MarketMakingEvent{
				ID:      1,
				Pending: true,
				CEXOrderEvent: &CEXOrderEvent{
					ID:   tradeID,
					Rate: 5e7,
					Qty:  5e6,
					Sell: false,
				},
			},
			updates: []*updateAndStats{
				{
					update: &libxc.Trade{
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
						Complete:    true,
					},
					event: &MarketMakingEvent{
						ID:         1,
						Pending:    false,
						BaseDelta:  3e6,
						QuoteDelta: -1.6e6,
						CEXOrderEvent: &CEXOrderEvent{
							ID:          tradeID,
							Rate:        5e7,
							Qty:         5e6,
							Sell:        false,
							BaseFilled:  3e6,
							QuoteFilled: 1.6e6,
						},
					},
					stats: &RunStats{
						CEXBalances: map[uint32]*BotBalance{
							42: {1e7 + 3e6, 0, 0, 0},
							0:  {1e7 - 1.6e6, 0, 0, 0},
						},
					},
				},
			},
		},
	}

	botCfg := &BotConfig{
		Host:    "host1",
		BaseID:  baseID,
		QuoteID: quoteID,
		CEXName: "Binance",
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCEX := newTCEX()
		tCEX.tradeID = tradeID

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID(botCfg.Host, botCfg.BaseID, botCfg.QuoteID)
		eventLogDB := newTEventLogDB()
		adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
			botID:           botID,
			core:            tCore,
			cex:             tCEX,
			baseDexBalances: test.balances,
			baseCexBalances: test.balances,
			mwh: &MarketWithHost{
				Host:    "host1",
				BaseID:  botCfg.BaseID,
				QuoteID: botCfg.QuoteID,
			},
			eventLogDB: eventLogDB,
		})
		_, err := adaptor.Connect(ctx)
		if err != nil {
			t.Fatalf("%s: Connect error: %v", test.name, err)
		}

		adaptor.SubscribeTradeUpdates()

		_, err = adaptor.CEXTrade(ctx, baseID, quoteID, test.sell, test.rate, test.qty)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		checkBalances := func(expected map[uint32]*BotBalance, i int) {
			t.Helper()
			for assetID, expectedBal := range expected {
				bal := adaptor.CEXBalance(assetID)
				if *bal != *expectedBal {
					step := "post trade"
					if i > 0 {
						step = fmt.Sprintf("after update #%d", i)
					}
					t.Fatalf("%s: unexpected cex balance %s for asset %d. want %+v, got %+v",
						test.name, step, assetID, expectedBal, bal)
				}
			}
		}

		checkBalances(test.postTradeBalances, 0)

		checkLatestEvent := func(expected *MarketMakingEvent, i int) {
			t.Helper()
			step := "post trade"
			if i > 0 {
				step = fmt.Sprintf("after update #%d", i)
			}
			if !eventLogDB.latestStoredEventEquals(expected) {
				t.Fatalf("%s: unexpected event %s. want:\n%+v,\ngot:\n%+v", test.name, step, expected, eventLogDB.latestStoredEvent())
			}
		}

		checkLatestEvent(test.postTradeEvent, 0)

		for i, updateAndStats := range test.updates {
			update := updateAndStats.update
			update.ID = tradeID
			update.BaseID = baseID
			update.QuoteID = quoteID
			update.Sell = test.sell
			eventLogDB.storedEventsMtx.Lock()
			eventLogDB.storedEvents = []*MarketMakingEvent{}
			eventLogDB.storedEventsMtx.Unlock()
			tCEX.tradeUpdates <- updateAndStats.update
			tCEX.tradeUpdates <- &libxc.Trade{} // dummy update
			checkBalances(updateAndStats.stats.CEXBalances, i+1)
			checkLatestEvent(updateAndStats.event, i+1)

			stats := adaptor.stats()
			stats.DEXBalances = nil
			stats.StartTime = 0
			if !reflect.DeepEqual(stats.CEXBalances, updateAndStats.stats.CEXBalances) {
				t.Fatalf("%s: stats mismatch after update %d.\nwant: %+v\n\ngot: %+v", test.name, i+1, updateAndStats.stats, stats)
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestOrderFeesInUnits(t *testing.T) {
	type test struct {
		name      string
		buyFees   *orderFees
		sellFees  *orderFees
		rate      uint64
		market    *MarketWithHost
		fiatRates map[uint32]float64

		expectedSellBase  uint64
		expectedSellQuote uint64
		expectedBuyBase   uint64
		expectedBuyQuote  uint64
	}

	tests := []*test{
		{
			name: "dcr/btc",
			market: &MarketWithHost{
				BaseID:  42,
				QuoteID: 0,
			},
			buyFees:           tFees(5e5, 1.1e4, 0, 0),
			sellFees:          tFees(1.085e4, 4e5, 0, 0),
			rate:              5e7,
			expectedSellBase:  810850,
			expectedBuyBase:   1011000,
			expectedSellQuote: 405425,
			expectedBuyQuote:  505500,
		},
		{
			name: "btc/usdc.eth",
			market: &MarketWithHost{
				BaseID:  0,
				QuoteID: 60001,
			},
			buyFees:  tFees(1e7, 4e4, 0, 0),
			sellFees: tFees(5e4, 1.1e7, 0, 0),
			fiatRates: map[uint32]float64{
				60001: 0.99,
				60:    2300,
				0:     42999,
			},
			rate: calc.MessageRateAlt(43000, 1e8, 1e6),
			// We first convert from the parent asset to the child.
			// 5e4 sats + (1.1e7 gwei / 1e9 * 2300 / 0.99 * 1e6) = 25555555 microUSDC
			// Then we use QuoteToBase with the message-rate.
			// r = 43000 * 1e8 / 1e8 * 1e6 = 43_000_000_000
			// 25555555 * 1e8 / 43_000_000_000 = 59431 Sats
			// 5e4 + 59431 = 109431
			expectedSellBase: 109431,
			// 1e7 gwei * / 1e9 * 2300 / 0.99 * 1e6 = 23232323 microUSDC
			// 23232323 * 1e8 / 43_000_000_000 = 54028
			// 4e4 + 54028 = 94028
			expectedBuyBase:   94028,
			expectedSellQuote: 47055556,
			expectedBuyQuote:  40432323,
		},
		{
			name: "wbtc.polygon/usdc.eth",
			market: &MarketWithHost{
				BaseID:  966003,
				QuoteID: 60001,
			},
			buyFees:  tFees(1e7, 2e8, 0, 0),
			sellFees: tFees(5e8, 1.1e7, 0, 0),
			fiatRates: map[uint32]float64{
				60001:  0.99,
				60:     2300,
				966003: 42500,
				966:    0.8,
			},
			rate: calc.MessageRateAlt(43000, 1e8, 1e6),
			// 1.1e7 gwei / 1e9 * 2300 / 0.99 * 1e6 = 25555556 micoUSDC
			// 25555556 * 1e8 / 43_000_000_000 = 59431 Sats
			// 5e8 gwei / 1e9 * 0.8 / 42500 * 1e8 = 941 wSats
			// 59431 + 941 = 60372
			expectedSellBase: 60372,
			// 1e7 gwei / 1e9 * 2300 / 0.99 = 23232323 microUSDC
			// 23232323 * 1e8 / 43_000_000_000 = 54028 wSats
			// 2e8 / 1e9 * 0.8 / 42500 * 1e8 = 376 wSats
			// 54028 + 376 = 54404
			expectedBuyBase: 54404,
			// 5e8 gwei / 1e9 * 0.8 / 42500 * 1e8 = 941 wSats
			// 941 * 43_000_000_000 / 1e8 = 404630 microUSDC
			// 1.1e7 gwei / 1e9 * 2300 / 0.99 * 1e6 = 25555556 microUSDC
			// 404630 + 25555556 = 25960186
			expectedSellQuote: 25960186,
			// 1e7 / 1e9 * 2300 / 0.99 * 1e6 = 23232323 microUSDC
			// 2e8 / 1e9 * 0.8 / 42500 * 1e8 = 376 wSats
			// 376 * 43_000_000_000 / 1e8 = 161680 microUSDC
			// 23232323 + 161680 = 23394003
			expectedBuyQuote: 23394003,
		},
	}

	runTest := func(tt *test) {
		tCore := newTCore()
		tCore.fiatRates = tt.fiatRates
		tCore.singleLotBuyFees = tt.buyFees
		tCore.singleLotSellFees = tt.sellFees
		adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
			core:       tCore,
			mwh:        tt.market,
			eventLogDB: &tEventLogDB{},
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := adaptor.Connect(ctx)
		if err != nil {
			t.Fatalf("%s: Connect error: %v", tt.name, err)
		}

		sellBase, err := adaptor.OrderFeesInUnits(true, true, tt.rate)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if sellBase != tt.expectedSellBase {
			t.Fatalf("%s: unexpected sell base fee. want %d, got %d", tt.name, tt.expectedSellBase, sellBase)
		}

		sellQuote, err := adaptor.OrderFeesInUnits(true, false, tt.rate)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if sellQuote != tt.expectedSellQuote {
			t.Fatalf("%s: unexpected sell quote fee. want %d, got %d", tt.name, tt.expectedSellQuote, sellQuote)
		}

		buyBase, err := adaptor.OrderFeesInUnits(false, true, tt.rate)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if buyBase != tt.expectedBuyBase {
			t.Fatalf("%s: unexpected buy base fee. want %d, got %d", tt.name, tt.expectedBuyBase, buyBase)
		}

		buyQuote, err := adaptor.OrderFeesInUnits(false, false, tt.rate)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if buyQuote != tt.expectedBuyQuote {
			t.Fatalf("%s: unexpected buy quote fee. want %d, got %d", tt.name, tt.expectedBuyQuote, buyQuote)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestCalcProfitLoss(t *testing.T) {
	initialBalances := map[uint32]uint64{
		42: 1e9,
		0:  1e6,
	}
	finalBalances := map[uint32]uint64{
		42: 0.9e9,
		0:  1.1e6,
	}
	fiatRates := map[uint32]float64{
		42: 23,
		0:  65000,
	}
	pl := newProfitLoss(initialBalances, finalBalances, nil, fiatRates)
	expProfitLoss := (9-10)*23 + (0.011-0.01)*65000
	if math.Abs(pl.Profit-expProfitLoss) > 1e-6 {
		t.Fatalf("unexpected profit loss. want %f, got %f", expProfitLoss, pl.Profit)
	}
	initialFiatValue := 10*23 + 0.01*65000
	expProfitRatio := expProfitLoss / initialFiatValue
	if math.Abs(pl.ProfitRatio-expProfitRatio) > 1e-6 {
		t.Fatalf("unexpected profit ratio. want %f, got %f", expProfitRatio, pl.ProfitRatio)
	}

	// Add mods and decrease initial balances by the same amount. P/L should be the same.
	mods := map[uint32]int64{
		42: 1e6,
		0:  2e6,
	}
	initialBalances[42] -= 1e6
	initialBalances[0] -= 2e6
	pl = newProfitLoss(initialBalances, finalBalances, mods, fiatRates)
	if math.Abs(pl.Profit-expProfitLoss) > 1e-6 {
		t.Fatalf("unexpected profit loss. want %f, got %f", expProfitLoss, pl.Profit)
	}
	if math.Abs(pl.ProfitRatio-expProfitRatio) > 1e-6 {
		t.Fatalf("unexpected profit ratio. want %f, got %f", expProfitRatio, pl.ProfitRatio)
	}
}

func TestRefreshPendingEvents(t *testing.T) {
	tCore := newTCore()
	tCEX := newTCEX()

	dexBalances := map[uint32]uint64{
		42: 1e9,
		0:  1e9,
	}
	cexBalances := map[uint32]uint64{
		42: 1e9,
		0:  1e9,
	}

	adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
		core: tCore,
		cex:  tCEX,
		mwh: &MarketWithHost{
			Host:    "host1",
			BaseID:  42,
			QuoteID: 0,
		},
		baseDexBalances: dexBalances,
		baseCexBalances: cexBalances,
		eventLogDB:      &tEventLogDB{},
	})

	// These will be updated throughout the test
	expectedDEXAvailableBalance := map[uint32]uint64{
		42: 1e9,
		0:  1e9,
	}
	expectedCEXAvailableBalance := map[uint32]uint64{
		42: 1e9,
		0:  1e9,
	}
	checkAvailableBalances := func() {
		t.Helper()
		for assetID, expectedBal := range expectedDEXAvailableBalance {
			bal := adaptor.DEXBalance(assetID)
			if bal.Available != expectedBal {
				t.Fatalf("unexpected dex balance for asset %d. want %d, got %d", assetID, expectedBal, bal.Available)
			}
		}

		for assetID, expectedBal := range expectedCEXAvailableBalance {
			bal := adaptor.CEXBalance(assetID)
			if bal.Available != expectedBal {
				t.Fatalf("unexpected cex balance for asset %d. want %d, got %d", assetID, expectedBal, bal.Available)
			}
		}
	}

	// Add a pending dex order, then refresh pending events
	var dexOrderID order.OrderID
	copy(dexOrderID[:], encode.RandomBytes(32))
	swapCoinID := encode.RandomBytes(32)
	redeemCoinID := encode.RandomBytes(32)
	tCore.walletTxs = map[string]*asset.WalletTransaction{
		hex.EncodeToString(swapCoinID): {
			Confirmed: true,
			Fees:      2000,
			Amount:    5e6,
		},
		hex.EncodeToString(redeemCoinID): {
			Confirmed: true,
			Fees:      1000,
			Amount:    calc.BaseToQuote(5e6, 5e7),
		},
	}
	adaptor.pendingDEXOrders[dexOrderID] = &pendingDEXOrder{
		swaps:   map[string]*asset.WalletTransaction{},
		redeems: map[string]*asset.WalletTransaction{},
		refunds: map[string]*asset.WalletTransaction{},
	}
	adaptor.pendingDEXOrders[dexOrderID].updateState(&core.Order{
		ID:      dexOrderID[:],
		Sell:    true,
		Rate:    5e6,
		Qty:     5e7,
		BaseID:  42,
		QuoteID: 0,
		Matches: []*core.Match{
			{
				Rate: 5e6,
				Qty:  5e7,
				Swap: &core.Coin{
					ID: swapCoinID,
				},
				Redeem: &core.Coin{
					ID: redeemCoinID,
				},
			},
		},
	}, &balanceEffects{})
	ctx := context.Background()
	adaptor.refreshAllPendingEvents(ctx)
	expectedDEXAvailableBalance[42] -= 5e6 + 2000
	expectedDEXAvailableBalance[0] += calc.BaseToQuote(5e6, 5e7) - 1000
	checkAvailableBalances()

	// Add a pending unfilled CEX order, then refresh pending events
	cexOrderID := "123"
	adaptor.pendingCEXOrders = map[string]*pendingCEXOrder{
		cexOrderID: {
			trade: &libxc.Trade{
				ID:      cexOrderID,
				Sell:    true,
				Rate:    5e6,
				Qty:     5e7,
				BaseID:  42,
				QuoteID: 0,
			},
		},
	}
	tCEX.tradeStatus = &libxc.Trade{
		ID:          cexOrderID,
		Sell:        true,
		Rate:        5e6,
		Qty:         5e7,
		BaseID:      42,
		QuoteID:     0,
		BaseFilled:  5e7,
		QuoteFilled: calc.BaseToQuote(5e6, 5e7),
		Complete:    true,
	}
	adaptor.refreshAllPendingEvents(ctx)
	expectedCEXAvailableBalance[42] -= 5e7
	expectedCEXAvailableBalance[0] += calc.BaseToQuote(5e6, 5e7)
	checkAvailableBalances()

	// Add a pending deposit, then refresh pending events
	depositTxID := hex.EncodeToString(encode.RandomBytes(32))
	adaptor.pendingDeposits[depositTxID] = &pendingDeposit{
		assetID: 42,
		tx: &asset.WalletTransaction{
			ID:        depositTxID,
			Fees:      1000,
			Amount:    1e7,
			Confirmed: true,
		},
		feeConfirmed: true,
	}
	amtReceived := uint64(1e7 - 1000)
	tCEX.confirmDepositMtx.Lock()
	tCEX.confirmedDeposit = &amtReceived
	tCEX.confirmDepositMtx.Unlock()
	adaptor.refreshAllPendingEvents(ctx)
	expectedDEXAvailableBalance[42] -= 1e7 + 1000
	expectedCEXAvailableBalance[42] += amtReceived
	checkAvailableBalances()

	// Add a pending withdrawal, then refresh pending events
	withdrawalID := "456"
	adaptor.pendingWithdrawals[withdrawalID] = &pendingWithdrawal{
		withdrawalID: withdrawalID,
		assetID:      42,
		amtWithdrawn: 2e7,
	}

	withdrawalTxID := hex.EncodeToString(encode.RandomBytes(32))
	tCore.walletTxs[withdrawalTxID] = &asset.WalletTransaction{
		ID:        withdrawalTxID,
		Amount:    2e7 - 3000,
		Confirmed: true,
	}

	tCEX.confirmWithdrawalMtx.Lock()
	tCEX.confirmWithdrawal = &withdrawArgs{
		assetID: 42,
		amt:     2e7,
		txID:    withdrawalTxID,
	}
	tCEX.confirmWithdrawalMtx.Unlock()

	adaptor.refreshAllPendingEvents(ctx)
	expectedDEXAvailableBalance[42] += 2e7 - 3000
	expectedCEXAvailableBalance[42] -= 2e7
	checkAvailableBalances()
}
