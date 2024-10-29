// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"sync"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

func TestArbMMRebalance(t *testing.T) {
	const baseID, quoteID = 42, 0
	const lotSize uint64 = 5e9
	const sellSwapFees, sellRedeemFees = 3e6, 1e6
	const buySwapFees, buyRedeemFees = 2e5, 1e5
	const buyRate, sellRate = 1e7, 1.1e7

	var epok uint64
	epoch := func() uint64 {
		epok++
		return epok
	}

	mkt := &core.Market{
		RateStep:   1e3,
		AtomToConv: 1,
		LotSize:    lotSize,
		BaseID:     baseID,
		QuoteID:    quoteID,
	}

	cex := newTCEX()
	u := mustParseAdaptorFromMarket(mkt)
	u.CEX = cex
	u.botCfgV.Store(&BotConfig{})
	c := newTCore()
	c.setWalletsAndExchange(mkt)
	u.clientCore = c
	u.fiatRates.Store(map[uint32]float64{baseID: 1, quoteID: 1})
	a := &arbMarketMaker{
		unifiedExchangeAdaptor: u,
		cex:                    newTBotCEXAdaptor(),
		core:                   newTBotCoreAdaptor(c),
		pendingOrders:          make(map[order.OrderID]uint64),
	}
	a.buyFees = &OrderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Redeem: buyRedeemFees,
				Swap:   buySwapFees,
			},
			Estimated: &LotFees{},
		},
		BookingFeesPerLot: buySwapFees,
	}
	a.sellFees = &OrderFees{
		LotFeeRange: &LotFeeRange{
			Max: &LotFees{
				Redeem: sellRedeemFees,
				Swap:   sellSwapFees,
			},
			Estimated: &LotFees{},
		},
		BookingFeesPerLot: sellSwapFees,
	}

	var buyLots, sellLots, minDexBase, minCexBase /* totalBase, */, minDexQuote, minCexQuote /*, totalQuote */ uint64
	setLots := func(buy, sell uint64) {
		buyLots, sellLots = buy, sell
		a.placementLotsV.Store(&placementLots{
			baseLots:  sellLots,
			quoteLots: buyLots,
		})
		a.cfgV.Store(&ArbMarketMakerConfig{
			Profit: 0,
			BuyPlacements: []*ArbMarketMakingPlacement{
				{
					Lots:       buyLots,
					Multiplier: 1,
				},
			},
			SellPlacements: []*ArbMarketMakingPlacement{
				{
					Lots:       sellLots,
					Multiplier: 1,
				},
			},
		})
		cex.bidsVWAP[lotSize*buyLots] = vwapResult{
			avg:     buyRate,
			extrema: buyRate,
		}
		cex.asksVWAP[lotSize*sellLots] = vwapResult{
			avg:     sellRate,
			extrema: sellRate,
		}
		minDexBase = sellLots * (lotSize + sellSwapFees)
		minCexBase = buyLots * lotSize
		minDexQuote = calc.BaseToQuote(buyRate, buyLots*lotSize) + a.buyFees.BookingFeesPerLot*buyLots
		minCexQuote = calc.BaseToQuote(sellRate, sellLots*lotSize)
	}

	setBals := func(assetID uint32, dexBal, cexBal uint64) {
		a.baseDexBalances[assetID] = int64(dexBal)
		a.baseCexBalances[assetID] = int64(cexBal)
	}

	type expectedPlacement struct {
		sell bool
		rate uint64
		lots uint64
	}

	ep := func(sell bool, rate, lots uint64) *expectedPlacement {
		return &expectedPlacement{sell: sell, rate: rate, lots: lots}
	}

	checkPlacements := func(ps ...*expectedPlacement) {
		t.Helper()

		if len(ps) != len(c.multiTradesPlaced) {
			t.Fatalf("expected %d placements, got %d", len(ps), len(c.multiTradesPlaced))
		}

		var n int
		for _, ord := range c.multiTradesPlaced {
			for _, pl := range ord.Placements {
				n++
				if len(ps) < n {
					t.Fatalf("too many placements")
				}
				p := ps[n-1]
				if p.sell != ord.Sell {
					t.Fatalf("expected placement %d to be sell = %t, got sell = %t", n-1, p.sell, ord.Sell)
				}
				if p.rate != pl.Rate {
					t.Fatalf("placement %d: expected rate %d, but got %d", n-1, p.rate, pl.Rate)
				}
				if p.lots != pl.Qty/lotSize {
					t.Fatalf("placement %d: expected %d lots, but got %d", n-1, p.lots, pl.Qty/lotSize)
				}
			}
		}
		c.multiTradesPlaced = nil
		a.pendingDEXOrders = make(map[order.OrderID]*pendingDEXOrder)
	}

	setLots(1, 1)
	setBals(baseID, minDexBase, minCexBase)
	setBals(quoteID, minDexQuote, minCexQuote)

	a.rebalance(epoch(), &orderbook.OrderBook{})
	checkPlacements(ep(false, buyRate, 1), ep(true, sellRate, 1))

	// base balance too low
	setBals(baseID, minDexBase-1, minCexBase)
	a.rebalance(epoch(), &orderbook.OrderBook{})
	checkPlacements(ep(false, buyRate, 1))

	// quote balance too low
	setBals(baseID, minDexBase, minCexBase)
	setBals(quoteID, minDexQuote-1, minCexQuote)
	a.rebalance(epoch(), &orderbook.OrderBook{})
	checkPlacements(ep(true, sellRate, 1))

	// cex quote balance too low. Can't place sell.
	setBals(quoteID, minDexQuote, minCexQuote-1)
	a.rebalance(epoch(), &orderbook.OrderBook{})
	checkPlacements(ep(false, buyRate, 1))

	// cex base balance too low. Can't place buy.
	setBals(baseID, minDexBase, minCexBase-1)
	setBals(quoteID, minDexQuote, minCexQuote)
	a.rebalance(epoch(), &orderbook.OrderBook{})
	checkPlacements(ep(true, sellRate, 1))
}

func TestArbMarketMakerDEXUpdates(t *testing.T) {
	const lotSize uint64 = 50e8
	const profit float64 = 0.01

	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	matchIDs := make([]order.MatchID, 5)
	for i := 0; i < 5; i++ {
		copy(matchIDs[i][:], encode.RandomBytes(32))
	}

	mkt := &core.Market{
		RateStep:    1e3,
		AtomToConv:  1,
		LotSize:     lotSize,
		BaseID:      42,
		QuoteID:     0,
		BaseSymbol:  "dcr",
		QuoteSymbol: "btc",
	}

	type test struct {
		name              string
		pendingOrders     map[order.OrderID]uint64
		orderUpdates      []*core.Order
		expectedCEXTrades []*libxc.Trade
	}

	tests := []*test{
		{
			name: "one buy and one sell match, repeated",
			pendingOrders: map[order.OrderID]uint64{
				orderIDs[0]: 7.9e5,
				orderIDs[1]: 6.1e5,
			},
			orderUpdates: []*core.Order{
				{
					ID:   orderIDs[0][:],
					Sell: true,
					Qty:  lotSize,
					Rate: 8e5,
					Matches: []*core.Match{
						{
							MatchID: matchIDs[0][:],
							Qty:     lotSize,
							Rate:    8e5,
						},
					},
				},
				{
					ID:   orderIDs[1][:],
					Sell: false,
					Qty:  lotSize,
					Rate: 6e5,
					Matches: []*core.Match{
						{
							MatchID: matchIDs[1][:],
							Qty:     lotSize,
							Rate:    6e5,
						},
					},
				},
				{
					ID:   orderIDs[0][:],
					Sell: true,
					Qty:  lotSize,
					Rate: 8e5,
					Matches: []*core.Match{
						{
							MatchID: matchIDs[0][:],
							Qty:     lotSize,
							Rate:    8e5,
						},
					},
				},
				{
					ID:   orderIDs[1][:],
					Sell: false,
					Qty:  lotSize,
					Rate: 6e5,
					Matches: []*core.Match{
						{
							MatchID: matchIDs[1][:],
							Qty:     lotSize,
							Rate:    6e5,
						},
					},
				},
			},
			expectedCEXTrades: []*libxc.Trade{
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    7.9e5,
					Sell:    false,
				},
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    6.1e5,
					Sell:    true,
				},
				nil,
				nil,
			},
		},
	}

	runTest := func(test *test) {
		cex := newTBotCEXAdaptor()
		tCore := newTCore()
		coreAdaptor := newTBotCoreAdaptor(tCore)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		arbMM := &arbMarketMaker{
			unifiedExchangeAdaptor: mustParseAdaptorFromMarket(mkt),
			cex:                    cex,
			core:                   coreAdaptor,
			matchesSeen:            make(map[order.MatchID]bool),
			cexTrades:              make(map[string]uint64),
			pendingOrders:          test.pendingOrders,
		}
		arbMM.CEX = newTCEX()
		arbMM.ctx = ctx
		arbMM.setBotLoop(arbMM.botLoop)
		arbMM.cfgV.Store(&ArbMarketMakerConfig{
			Profit: profit,
		})
		arbMM.currEpoch.Store(123)
		err := arbMM.runBotLoop(ctx)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		for i, note := range test.orderUpdates {
			cex.lastTrade = nil

			coreAdaptor.orderUpdates <- note
			coreAdaptor.orderUpdates <- &core.Order{} // Dummy update should have no effect

			expectedCEXTrade := test.expectedCEXTrades[i]
			if (expectedCEXTrade == nil) != (cex.lastTrade == nil) {
				t.Fatalf("%s: expected cex order after update %d %v but got %v", test.name, i, (expectedCEXTrade != nil), (cex.lastTrade != nil))
			}

			if cex.lastTrade != nil &&
				*cex.lastTrade != *expectedCEXTrade {
				t.Fatalf("%s: cex order %+v != expected %+v", test.name, cex.lastTrade, expectedCEXTrade)
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestDEXPlacementRate(t *testing.T) {
	type test struct {
		name             string
		counterTradeRate uint64
		profit           float64
		base             uint32
		quote            uint32
		fees             uint64
		mkt              *market
	}

	tests := []*test{
		{
			name:             "dcr/btc",
			counterTradeRate: 5e6,
			profit:           0.03,
			base:             42,
			quote:            0,
			fees:             4e5,
			mkt: mustParseMarket(&core.Market{
				BaseID:   42,
				QuoteID:  0,
				LotSize:  40e8,
				RateStep: 1e2,
			}),
		},
		{
			name:             "btc/usdc.eth",
			counterTradeRate: calc.MessageRateAlt(43000, 1e8, 1e6),
			profit:           0.01,
			base:             0,
			quote:            60001,
			fees:             5e5,
			mkt: mustParseMarket(&core.Market{
				BaseID:   0,
				QuoteID:  60001,
				LotSize:  5e6,
				RateStep: 1e4,
			}),
		},
		{
			name:             "wbtc.polygon/usdc.eth",
			counterTradeRate: calc.MessageRateAlt(43000, 1e8, 1e6),
			profit:           0.02,
			base:             966003,
			quote:            60001,
			fees:             3e5,
			mkt: mustParseMarket(&core.Market{
				BaseID:   966003,
				QuoteID:  60001,
				LotSize:  5e6,
				RateStep: 1e4,
			}),
		},
	}

	runTest := func(tt *test) {
		sellRate, err := dexPlacementRate(tt.counterTradeRate, true, tt.profit, tt.mkt, tt.fees, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}

		expectedProfitableSellRate := uint64(float64(tt.counterTradeRate) * (1 + tt.profit))
		additional := calc.BaseToQuote(sellRate, tt.mkt.lotSize) - calc.BaseToQuote(expectedProfitableSellRate, tt.mkt.lotSize)
		if additional > tt.fees*101/100 || additional < tt.fees*99/100 {
			t.Fatalf("%s: expected additional %d but got %d", tt.name, tt.fees, additional)
		}

		buyRate, err := dexPlacementRate(tt.counterTradeRate, false, tt.profit, tt.mkt, tt.fees, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		expectedProfitableBuyRate := uint64(float64(tt.counterTradeRate) / (1 + tt.profit))
		savings := calc.BaseToQuote(expectedProfitableBuyRate, tt.mkt.lotSize) - calc.BaseToQuote(buyRate, tt.mkt.lotSize)
		if savings > tt.fees*101/100 || savings < tt.fees*99/100 {
			t.Fatalf("%s: expected savings %d but got %d", tt.name, tt.fees, savings)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func mustParseMarket(m *core.Market) *market {
	mkt, err := parseMarket("host.com", m)
	if err != nil {
		panic(err.Error())
	}
	return mkt
}

func mustParseAdaptorFromMarket(m *core.Market) *unifiedExchangeAdaptor {
	tCore := newTCore()
	tCore.setWalletsAndExchange(m)

	u := &unifiedExchangeAdaptor{
		ctx:                context.Background(),
		market:             mustParseMarket(m),
		log:                tLogger,
		botLooper:          botLooper(dummyLooper),
		baseDexBalances:    make(map[uint32]int64),
		baseCexBalances:    make(map[uint32]int64),
		pendingDEXOrders:   make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:   make(map[string]*pendingCEXOrder),
		eventLogDB:         newTEventLogDB(),
		pendingDeposits:    make(map[string]*pendingDeposit),
		pendingWithdrawals: make(map[string]*pendingWithdrawal),
		clientCore:         tCore,
		cexProblems:        newCEXProblems(),
	}

	u.botCfgV.Store(&BotConfig{
		Host:    u.host,
		BaseID:  u.baseID,
		QuoteID: u.quoteID,
	})

	return u
}

func mustParseAdaptor(cfg *exchangeAdaptorCfg) *unifiedExchangeAdaptor {
	if cfg.core.(*tCore).market == nil {
		cfg.core.(*tCore).market = &core.Market{
			BaseID:  cfg.mwh.BaseID,
			QuoteID: cfg.mwh.QuoteID,
			LotSize: 1e8,
		}
	}
	cfg.log = tLogger
	adaptor, err := newUnifiedExchangeAdaptor(cfg)
	if err != nil {
		panic(err.Error())
	}
	adaptor.ctx = context.Background()
	adaptor.botLooper = botLooper(dummyLooper)
	adaptor.botCfgV.Store(&BotConfig{})
	return adaptor
}

func dummyLooper(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-ctx.Done()
		wg.Done()
	}()
	return &wg, nil
}
