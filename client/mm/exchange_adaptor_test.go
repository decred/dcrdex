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

func TestPrepareRebalance(t *testing.T) {
	baseID := uint32(42)
	quoteID := uint32(0)
	cfg := &AutoRebalanceConfig{
		MinBaseAmt:       120e8,
		MinBaseTransfer:  50e8,
		MinQuoteAmt:      0.5e8,
		MinQuoteTransfer: 0.1e8,
	}
	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}

	type test struct {
		name                  string
		assetID               uint32
		dexBalances           map[uint32]uint64
		cexBalances           map[uint32]uint64
		baseRebalancePending  bool
		quoteRebalancePending bool
		pendingDEXOrders      map[order.OrderID]*pendingDEXOrder
		dexOrdersState        map[order.OrderID]*dexOrderState

		expectedRebalance   int64
		expectedDEXReserves uint64
		expectedCEXReserves uint64
	}

	tests := []*test{
		{
			name:    "no pending orders, no rebalance required",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  120e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  120e8,
				quoteID: 0,
			},
		},
		{
			name:    "no pending orders, base deposit required",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			expectedRebalance: 50e8,
		},
		{
			name:    "no pending orders, quote deposit required",
			assetID: 0,
			dexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			expectedRebalance: 1e8,
		},
		{
			name:    "no pending orders, base withdrawal required",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			cexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			expectedRebalance: -50e8,
		},
		{
			name:    "no pending orders, quote withdrawal required",
			assetID: 0,
			dexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			cexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			expectedRebalance: -1e8,
		},
		{
			name:    "no pending orders, base deposit required, already pending",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			baseRebalancePending: true,
		},
		{
			name:    "no pending orders, quote withdrawal required, already pending",
			assetID: 0,
			dexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			cexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			quoteRebalancePending: true,
		},
		{
			name:    "no pending orders, deposit < min base transfer",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  71e8,
				quoteID: 0,
			},
		},
		{
			name:    "base deposit required, pending orders",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  40e8,
				quoteID: 2e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					counterTradeRate: 6e7,
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							baseID: 130e8,
						},
					},
					order: &core.Order{
						Qty:     130e8,
						Rate:    5e7,
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedRebalance:   0,
			expectedDEXReserves: 50e8,
		},
		{
			name:    "base withdrawal required, pending buy order",
			assetID: 42,
			dexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0,
			},
			cexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 2e8,
			},
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					counterTradeRate: 5e7, // sell with 20e8 remaining
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							quoteID: 130e8,
						},
					},
					order: &core.Order{
						Qty:     150e8,
						Filled:  20e8,
						Rate:    5e7,
						Sell:    false,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedRebalance:   0,
			expectedCEXReserves: 50e8,
		},
		{
			name:    "quote withdrawal required, pending sell order",
			assetID: 0,
			dexBalances: map[uint32]uint64{
				baseID:  70e8,
				quoteID: 0.25e8,
			},
			cexBalances: map[uint32]uint64{
				baseID:  170e8,
				quoteID: 0.75e8,
			},
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					counterTradeRate: 5e5, // 0.6e8 required to counter-trade 120e8 @ 5e5
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							baseID: 100e8,
						},
					},
					order: &core.Order{
						Qty:     140e8,
						Filled:  20e8,
						Rate:    6e5,
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedRebalance:   0,
			expectedCEXReserves: 0.25e8,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tCore := newTCore()
			adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
				core:                tCore,
				baseDexBalances:     test.dexBalances,
				baseCexBalances:     test.cexBalances,
				autoRebalanceConfig: cfg,
				mwh: &MarketWithHost{
					Host:    "dex.com",
					BaseID:  baseID,
					QuoteID: quoteID,
				},
			})
			adaptor.botCfgV.Store(&BotConfig{
				CEXName: "Binance",
			})
			adaptor.pendingBaseRebalance.Store(test.baseRebalancePending)
			adaptor.pendingQuoteRebalance.Store(test.quoteRebalancePending)

			for id, state := range test.dexOrdersState {
				test.pendingDEXOrders[id].updateState(state.order, state.balanceEffects)
			}
			adaptor.pendingDEXOrders = test.pendingDEXOrders

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rebalance, dexReserves, cexReserves := adaptor.PrepareRebalance(ctx, test.assetID)
			if rebalance != test.expectedRebalance {
				t.Fatalf("expected rebalance=%d, got %d", test.expectedRebalance, rebalance)
			}
			if dexReserves != test.expectedDEXReserves {
				t.Fatalf("expected dexReserves=%d, got %d", test.expectedDEXReserves, dexReserves)
			}
			if cexReserves != test.expectedCEXReserves {
				t.Fatalf("expected cexReserves=%d, got %d", test.expectedCEXReserves, cexReserves)
			}
		})
	}
}

func TestFreeUpFunds(t *testing.T) {
	var currEpoch uint64 = 100
	baseID := uint32(42)
	quoteID := uint32(0)
	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}

	type test struct {
		name             string
		dexBalances      map[uint32]uint64
		cexBalances      map[uint32]uint64
		assetID          uint32
		cex              bool
		amt              uint64
		pendingDEXOrders map[order.OrderID]*pendingDEXOrder
		dexOrdersState   map[order.OrderID]*dexOrderState

		expectedCancels []*order.OrderID
	}

	tests := []*test{
		{
			name: "base, dex",
			dexBalances: map[uint32]uint64{
				baseID: 10e8,
			},
			assetID: baseID,
			amt:     50e8,
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					placementIndex: 1,
				},
				orderIDs[1]: {
					placementIndex: 0,
				},
				orderIDs[2]: {
					placementIndex: 2,
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							baseID: 20e8,
						},
					},
					order: &core.Order{
						ID:      orderIDs[0][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[1]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							baseID: 20e8,
						},
					},
					order: &core.Order{
						ID:      orderIDs[1][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[2]: {
					balanceEffects: &balanceEffects{
						locked: map[uint32]uint64{
							baseID: 20e8,
						},
					},
					order: &core.Order{
						ID:      orderIDs[2][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedCancels: []*order.OrderID{
				&orderIDs[2], // placementIndex 2
				&orderIDs[0], // placementIndex 1
			},
		},
		{
			name: "base, cex",
			cexBalances: map[uint32]uint64{
				baseID: 70e8,
			},
			assetID: baseID,
			cex:     true,
			amt:     50e8,
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					placementIndex:   1,
					counterTradeRate: 5e5,
				},
				orderIDs[1]: {
					placementIndex:   0,
					counterTradeRate: 5e5,
				},
				orderIDs[2]: {
					placementIndex:   2,
					counterTradeRate: 5e5,
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[0][:],
						Sell:    false,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[1]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[1][:],
						Sell:    false,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[2]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[2][:],
						Sell:    false,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedCancels: []*order.OrderID{
				&orderIDs[2], // placementIndex 2
				&orderIDs[0], // placementIndex 1
			},
		},
		{
			name: "quote, cex",
			cexBalances: map[uint32]uint64{
				quoteID: 0.6e8,
			},
			assetID: quoteID,
			cex:     true,
			amt:     0.5e8,
			pendingDEXOrders: map[order.OrderID]*pendingDEXOrder{
				orderIDs[0]: {
					placementIndex:   1,
					counterTradeRate: 5e5, // 0.1e8 required to counter-trade 20e8 @ 5e5
				},
				orderIDs[1]: {
					placementIndex:   0,
					counterTradeRate: 5e5,
				},
				orderIDs[2]: {
					placementIndex:   2,
					counterTradeRate: 5e5,
				},
			},
			dexOrdersState: map[order.OrderID]*dexOrderState{
				orderIDs[0]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[0][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[1]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[1][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
				orderIDs[2]: {
					balanceEffects: &balanceEffects{},
					order: &core.Order{
						Qty:     20e8,
						ID:      orderIDs[2][:],
						Sell:    true,
						BaseID:  baseID,
						QuoteID: quoteID,
					},
				},
			},
			expectedCancels: []*order.OrderID{
				&orderIDs[2], // placementIndex 2
				&orderIDs[0], // placementIndex 1
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tCore := newTCore()
			adaptor := mustParseAdaptor(&exchangeAdaptorCfg{
				core:                tCore,
				baseDexBalances:     test.dexBalances,
				baseCexBalances:     test.cexBalances,
				autoRebalanceConfig: &AutoRebalanceConfig{},
				mwh: &MarketWithHost{
					Host:    "dex.com",
					BaseID:  baseID,
					QuoteID: quoteID,
				},
			})
			adaptor.botCfgV.Store(&BotConfig{
				CEXName: "Binance",
			})
			for id, state := range test.dexOrdersState {
				test.pendingDEXOrders[id].updateState(state.order, state.balanceEffects)
			}
			adaptor.pendingDEXOrders = test.pendingDEXOrders
			adaptor.FreeUpFunds(test.assetID, test.cex, test.amt, currEpoch)

			if len(tCore.cancelsPlaced) != len(test.expectedCancels) {
				t.Fatalf("%s: expected %d cancels, got %d", test.name, len(test.expectedCancels), len(tCore.cancelsPlaced))
			}

			for i, cancel := range tCore.cancelsPlaced {
				if !bytes.Equal(cancel, test.expectedCancels[i][:]) {
					t.Fatalf("%s: expected cancel %d to be %v, got %v", test.name, i, test.expectedCancels[i], cancel)
				}
			}
		})
	}
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
		sellDexReserves   map[uint32]uint64
		sellCexReserves   map[uint32]uint64

		buyCexBalances   map[uint32]uint64
		buyDexBalances   map[uint32]uint64
		buyPlacements    []*multiTradePlacement
		buyPendingOrders map[order.OrderID]*pendingDEXOrder
		buyDexReserves   map[uint32]uint64
		buyCexReserves   map[uint32]uint64

		isAccountLocker               map[uint32]bool
		multiTradeResult              []*core.Order
		multiTradeResultWithDecrement []*core.Order

		expectedOrderIDs              []*order.OrderID
		expectedOrderIDsWithDecrement []*order.OrderID

		expectedSellPlacements              []*core.QtyRate
		expectedSellPlacementsWithDecrement []*core.QtyRate

		expectedBuyPlacements              []*core.QtyRate
		expectedBuyPlacementsWithDecrement []*core.QtyRate

		expectedCancels              []dex.Bytes
		expectedCancelsWithDecrement []dex.Bytes
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

			expectedCancels:              []dex.Bytes{orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
				{ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5], &orderIDs[6],
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5], nil,
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

			expectedCancels:              []dex.Bytes{orderIDs[1][:], orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[1][:], orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
				// {ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				// {ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, nil, &orderIDs[4], &orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, nil, &orderIDs[4], nil,
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

			expectedCancels:              []dex.Bytes{orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[5][:]},
				{ID: orderIDs[6][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, nil, &orderIDs[5], &orderIDs[6],
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, nil, &orderIDs[5], nil,
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

			expectedCancels:              []dex.Bytes{orderIDs[3][:], orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[3][:], orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5], nil,
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5], nil,
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

			expectedCancels:              []dex.Bytes{orderIDs[3][:], orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[3][:], orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[4], &orderIDs[5],
			},
		},
		{
			name:    "non account locker, cex reserves",
			baseID:  42,
			quoteID: 0,
			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 2*lotSize + 2*sellFees.Max.Swap + sellFees.funding,
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
				{Qty: lotSize, Rate: sellPlacements[2].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
			},
			sellCexReserves: map[uint32]uint64{
				0: b2q(3.9e7, lotSize) + b2q(2.9e7, lotSize),
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[1].rate, lotSize) +
					b2q(buyPlacements[2].rate, lotSize) +
					2*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 8 * lotSize,
				0:  0,
			},
			buyPlacements:    buyPlacements,
			buyPendingOrders: pendingOrders(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: lotSize, Rate: buyPlacements[2].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
			},
			buyCexReserves: map[uint32]uint64{
				42: 2 * lotSize,
			},

			expectedCancels:              []dex.Bytes{orderIDs[2][:], orderIDs[3][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[2][:], orderIDs[3][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[3][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[3], &orderIDs[4], nil,
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[3], nil, nil,
			},
		},
		{
			name:    "non account locker, dex reserves",
			baseID:  42,
			quoteID: 0,
			// ---- Sell ----
			sellDexBalances: map[uint32]uint64{
				42: 4*lotSize + 2*sellFees.Max.Swap + sellFees.funding,
				0:  0,
			},
			sellCexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(sellPlacements[0].counterTradeRate, lotSize) +
					b2q(sellPlacements[1].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[2].counterTradeRate, 2*lotSize) +
					b2q(sellPlacements[3].counterTradeRate, lotSize),
			},
			sellPlacements:    sellPlacements,
			sellPendingOrders: pendingOrders(true, 42, 0),
			expectedSellPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
				{Qty: lotSize, Rate: sellPlacements[2].rate},
			},
			expectedSellPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: sellPlacements[1].rate},
			},
			sellDexReserves: map[uint32]uint64{
				42: 2 * lotSize,
			},

			// ---- Buy ----
			buyDexBalances: map[uint32]uint64{
				42: 0,
				0: b2q(buyPlacements[1].rate, 2*lotSize) +
					b2q(buyPlacements[2].rate, 2*lotSize) +
					2*buyFees.Max.Swap + buyFees.funding,
			},
			buyCexBalances: map[uint32]uint64{
				42: 6 * lotSize,
				0:  0,
			},
			buyPlacements:    buyPlacements,
			buyPendingOrders: pendingOrders(false, 42, 0),
			expectedBuyPlacements: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
				{Qty: lotSize, Rate: buyPlacements[2].rate},
			},
			expectedBuyPlacementsWithDecrement: []*core.QtyRate{
				{Qty: lotSize, Rate: buyPlacements[1].rate},
			},
			buyDexReserves: map[uint32]uint64{
				0: b2q(buyPlacements[1].rate, lotSize) + b2q(buyPlacements[2].rate, lotSize),
			},

			expectedCancels:              []dex.Bytes{orderIDs[2][:], orderIDs[3][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[2][:], orderIDs[3][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[3][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[3], &orderIDs[4], nil,
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[3], nil, nil,
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

			expectedCancels:              []dex.Bytes{orderIDs[2][:]},
			expectedCancelsWithDecrement: []dex.Bytes{orderIDs[2][:]},
			multiTradeResult: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
				{ID: orderIDs[5][:]},
			},
			multiTradeResultWithDecrement: []*core.Order{
				{ID: orderIDs[3][:]},
				{ID: orderIDs[4][:]},
			},
			expectedOrderIDs: []*order.OrderID{
				nil, &orderIDs[3], &orderIDs[4], &orderIDs[5],
			},
			expectedOrderIDsWithDecrement: []*order.OrderID{
				nil, &orderIDs[3], &orderIDs[4], nil,
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
					var dexReserves, cexReserves map[uint32]uint64
					if sell {
						placements = test.sellPlacements
						dexReserves = test.sellDexReserves
						cexReserves = test.sellCexReserves
					} else {
						placements = test.buyPlacements
						dexReserves = test.buyDexReserves
						cexReserves = test.buyCexReserves
					}
					res := adaptor.MultiTrade(placements, sell, driftTolerance, currEpoch, dexReserves, cexReserves)

					expectedOrderIDs := test.expectedOrderIDs
					if decrement {
						expectedOrderIDs = test.expectedOrderIDsWithDecrement
					}
					if !reflect.DeepEqual(res, expectedOrderIDs) {
						t.Fatalf("expected orderIDs %v, got %v", expectedOrderIDs, res)
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
						return bytes.Compare(tCore.cancelsPlaced[i], tCore.cancelsPlaced[j]) < 0
					})
					sort.Slice(expectedCancels, func(i, j int) bool {
						return bytes.Compare(expectedCancels[i], expectedCancels[j]) < 0
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
	host := "dex.com"
	lotSize := uint64(1e6)

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
				{lots: 5, rate: 5e7},
				{lots: 5, rate: 6e7},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5e6+2000, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], 5e6+2000, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {1e8 - (5e6+2000)*2, (5e6 + 2000) * 2, 0, 0},
				0:  {1e8, 0, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (5e6+2000)*2, 5e6 + 2000 + 3e6 + 1000, 0, 0},
							0:  {1e8, 0, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2e6+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0, 0},
							0:  {1e8, 0, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, true),
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0, 0},
							0:  {1e8, 0, b2q(5e7, 2e6) - 1000, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0, 0},
							0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (7e6 + 2000 + 1000), 2e6 + 1000, 0, 0},
							0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], nil, nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (7e6 + 1800 + 1000), 0, 3e6 - 1200, 0},
							0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, b2q(6e7, 2e6), 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 - (4e6 + 1800 + 1000 + 1200), 0, 0, 0},
							0:  {1e8 + b2q(5e7, 2e6) + b2q(6e7, 2e6) - 1700, 0, 0, 0},
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
				{lots: 5, rate: 5e7},
				{lots: 5, rate: 6e7},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], b2q(5e7, 5e6)+2000, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], b2q(6e7, 5e6)+2000, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*BotBalance{
				42: {1e8, 0, 0, 0},
				0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8, 0, 0, 0},
							0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 5e6) + 3000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, b2q(6e7, 3e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], b2q(6e7, 2e6)+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8, 0, 0, 0},
							0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, b2q(5e7, 2e6), 1000, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8, 0, 2e6 - 1000, 0},
							0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2e6 - 1000, 0, 0, 0},
							0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2e6 - 1000, 0, 0, 0},
							0:  {1e8 - (b2q(5e7, 2e6) + b2q(6e7, 5e6) + 3000), b2q(6e7, 2e6) + 1000, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, b2q(6e7, 3e6), 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, b2q(6e7, 3e6), 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, b2q(6e7, 2e6), 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], nil, nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 2e6 - 1000, 0, 0, 0},
							0:  {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6) + 2800), 0, calc.BaseToQuote(6e7, 3e6) - 1200, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, b2q(6e7, 3e6), 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, b2q(6e7, 2e6), 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2e6, 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							42: {1e8 + 4e6 - 1700, 0, 0, 0},
							0:  {1e8 - (b2q(5e7, 2e6) + b2q(6e7, 2e6) + 2800 + 1200), 0, 0, 0},
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
				{lots: 5, rate: 5e7},
				{lots: 5, rate: 6e7},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5e6+2000, 0, 4000, 3000),
				newOrderLockedFunds(orderIDs[1], 5e6+2000, 0, 4000, 3000),
			},
			postTradeBalances: map[uint32]*BotBalance{
				966001: {1e8, 0, 0, 0},
				966:    {1e8 - 8000, 8000, 0, 0},
				60:     {1e8 - (5e6+2000+3000)*2, (5e6 + 2000 + 3000) * 2, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 4000, 3000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, 0, 0},
							966:    {1e8 - 8000, 8000, 0, 0},
							60:     {1e8 - (5e6+2000+3000)*2, 3e6 + 1000 + 5e6 + 2000 + 3000 + 3000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2e6+1000, 0, 4000, 3000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, 0, 0},
							966:    {1e8 - 8000, 8000, 0, 0},
							60:     {1e8 - (5e6+2000+3000)*2, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 900, true),
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 3000, 3000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8, 0, b2q(5e7, 2e6), 0},
							966:    {1e8 - 8000, 7000, 0, 0},
							60:     {1e8 - (5e6+2000+3000)*2 + 100, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 800, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 3000, 3000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + b2q(5e7, 2e6), 0, 0, 0},
							966:    {1e8 - 7000 - 800, 7000, 0, 0},
							60:     {1e8 - (5e6+2000+3000)*2 + 100, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + b2q(5e7, 2e6), 0, 0, 0},
							966:    {1e8 - 4000 - 800, 4000, 0, 0},
							60:     {1e8 - (7e6 + 900 + 2000 + 3000), 2e6 + 1000 + 3000, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 4000, 1800, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], nil, nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + b2q(5e7, 2e6), 0, 0, 0},
							966:    {1e8 - 4000 - 800, 4000, 0, 0},
							60:     {1e8 - (7e6 + 900 + 2000 + 3000) + 200, 1800, 3e6, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1100, true),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, calc.BaseToQuote(6e7, 2e6), 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 0, 0), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 0, 0)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 + calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6), 0, 0, 0},
							966:    {1e8 - 1500, 0, 0, 0},
							60:     {1e8 - (4e6 + 3800), 0, 0, 0},
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
				{lots: 5, rate: 5e7},
				{lots: 5, rate: 6e7},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], b2q(5e7, 5e6), 2000, 3000, 4000),
				newOrderLockedFunds(orderIDs[1], b2q(6e7, 5e6), 2000, 3000, 4000),
			},
			postTradeBalances: map[uint32]*BotBalance{
				966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6), 0, 0},
				966:    {1e8 - 12000, 12000, 0, 0},
				60:     {1e8 - 6000, 6000, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, calc.BaseToQuote(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 3000, 4000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], nil, nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 5e6), 0, 0},
							966:    {1e8 - 12000, 11000, 0, 0},
							60:     {1e8 - 6000, 6000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, calc.BaseToQuote(6e7, 3e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], calc.BaseToQuote(6e7, 2e6), 1000, 3000, 4000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[1], nil, nil, 3e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0, 0},
							966:    {1e8 - 12000, 10000, 0, 0},
							60:     {1e8 - 6000, 6000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, calc.BaseToQuote(5e7, 2e6), 900, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 2000, 4000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0, 0},
							966:    {1e8 - 12000 + 100, 10000, 0, 0},
							60:     {1e8 - 6000, 5000, 2e6, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 800, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 2000, 4000, order.OrderStatusBooked, false,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0, 0},
							966:    {1e8 - 12000 + 100, 10000, 0, 0},
							60:     {1e8 - 5800 + 2e6, 5000, 0, 0},
						},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true,
						newMatchUpdate(&coinIDs[0], &coinIDs[2], nil, 2e6, 5e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(6e7, 2e6), 0, 0},
							966:    {1e8 - 7000 + 100, 5000, 0, 0},
							60:     {1e8 + 2e6 - 3800, 3000, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, calc.BaseToQuote(6e7, 3e6), 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, calc.BaseToQuote(6e7, 3e6), 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, calc.BaseToQuote(6e7, 2e6), 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 3000, 2000, order.OrderStatusExecuted, false,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], nil, nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)), 0, calc.BaseToQuote(6e7, 3e6), 0},
							966:    {1e8 - 6000 + 100, 2000, 0, 0},
							60:     {1e8 + 2e6 - 3800, 3000, 0, 0},
						},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, calc.BaseToQuote(6e7, 3e6), 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, calc.BaseToQuote(6e7, 2e6), 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2e6, 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true,
						newMatchUpdate(&coinIDs[1], nil, &coinIDs[3], 3e6, 6e7),
						newMatchUpdate(&coinIDs[4], &coinIDs[5], nil, 2e6, 6e7)),
					stats: &RunStats{
						DEXBalances: map[uint32]*BotBalance{
							966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6)), 0, 0, 0},
							966:    {1e8 - 3900, 0, 0, 0},
							60:     {1e8 + 4e6 - 1500, 0, 0, 0},
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

		orders := adaptor.MultiTrade(test.placements, test.sell, 0.01, 100, nil, nil)
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

			err = adaptor.Deposit(ctx, test.assetID, test.depositAmt)
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

		err = adaptor.Withdraw(ctx, assetID, test.withdrawAmt)
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
			rate:              calc.MessageRateAlt(43000, 1e8, 1e6),
			expectedSellBase:  108839, // 5e4 sats + (1.1e7 gwei / 1e9 * 2300 / 42999 * 1e8) = 108838.57
			expectedBuyBase:   93490,
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
			rate:              calc.MessageRateAlt(43000, 1e8, 1e6),
			expectedSellBase:  60470,
			expectedBuyBase:   54494,
			expectedSellQuote: 25959596,
			expectedBuyQuote:  23393939,
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
	profitLoss, profitRatio := calcRunProfitLoss(initialBalances, finalBalances, nil, fiatRates)
	expProfitLoss := (9-10)*23 + (0.011-0.01)*65000
	if math.Abs(profitLoss-expProfitLoss) > 1e-6 {
		t.Fatalf("unexpected profit loss. want %f, got %f", expProfitLoss, profitLoss)
	}
	initialFiatValue := 10*23 + 0.01*65000
	expProfitRatio := expProfitLoss / initialFiatValue
	if math.Abs(profitRatio-expProfitRatio) > 1e-6 {
		t.Fatalf("unexpected profit ratio. want %f, got %f", expProfitRatio, profitRatio)
	}

	// Add mods and decrease initial balances by the same amount. P/L should be the same.
	mods := map[uint32]int64{
		42: 1e6,
		0:  2e6,
	}
	initialBalances[42] -= 1e6
	initialBalances[0] -= 2e6
	profitLoss, profitRatio = calcRunProfitLoss(initialBalances, finalBalances, mods, fiatRates)
	if math.Abs(profitLoss-expProfitLoss) > 1e-6 {
		t.Fatalf("unexpected profit loss. want %f, got %f", expProfitLoss, profitLoss)
	}
	if math.Abs(profitRatio-expProfitRatio) > 1e-6 {
		t.Fatalf("unexpected profit ratio. want %f, got %f", expProfitRatio, profitRatio)
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
