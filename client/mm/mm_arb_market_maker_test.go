// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"github.com/davecgh/go-spew/spew"
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
		orderArbRates:          make(map[order.OrderID][2]uint64),
		orderIndex:             make(map[bool]map[order.OrderID]int),
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
		u.botCfgV.Store(&BotConfig{
			ArbMarketMakerConfig: &ArbMarketMakerConfig{
				Profit: 0,
				BuyPlacements: []*ArbMarketMakingPlacement{
					{Lots: buyLots, Multiplier: 1},
				},
				SellPlacements: []*ArbMarketMakingPlacement{
					{Lots: sellLots, Multiplier: 1},
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
		pendingOrders     map[order.OrderID][2]uint64
		orderUpdates      []*core.Order
		expectedCEXTrades []*libxc.Trade
	}

	tests := []*test{
		{
			name: "one buy and one sell match, repeated",
			pendingOrders: map[order.OrderID][2]uint64{
				orderIDs[0]: {7.9e5, 0},
				orderIDs[1]: {6.1e5, 0},
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
			cexTrades:              make(map[string]*cexTradeInfo),
			orderArbRates:          test.pendingOrders,
			orderIndex:             make(map[bool]map[order.OrderID]int),
		}
		arbMM.CEX = newTCEX()
		arbMM.ctx = ctx
		arbMM.setBotLoop(arbMM.botLoop)
		arbMM.unifiedExchangeAdaptor.botCfgV.Store(&BotConfig{
			ArbMarketMakerConfig: &ArbMarketMakerConfig{
				Profit: profit,
			},
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

func TestArbMarketMakerMultiHopDexUpdates(t *testing.T) {
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
		pendingOrders     map[order.OrderID][2]uint64
		marketOrders      bool
		orderUpdates      []*core.Order
		expectedCEXTrades []*libxc.Trade
	}

	tests := []*test{
		{
			name: "one buy and one sell match, repeated, market",
			pendingOrders: map[order.OrderID][2]uint64{
				orderIDs[0]: {7.9e5, 0},
				orderIDs[1]: {6.1e5, 0},
			},
			marketOrders: true,
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
					BaseID:  0,
					QuoteID: 60002,
					Rate:    7.9e5,
					Qty:     calc.BaseToQuote(8e5, lotSize),
					Sell:    true,
					Market:  false,
				},
				{
					BaseID:  42,
					QuoteID: 60002,
					Rate:    6.1e5,
					Qty:     lotSize,
					Sell:    true,
					Market:  false,
				},
				nil,
				nil,
			},
		},
		{
			name: "one buy and one sell match, repeated, limit",
			pendingOrders: map[order.OrderID][2]uint64{
				orderIDs[0]: {7.9e5, 1e5},
				orderIDs[1]: {6.1e5, 1e5},
			},
			marketOrders: false,
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
					BaseID:  0,
					QuoteID: 60002,
					Rate:    7.9e5,
					Qty:     calc.BaseToQuote(8e5, lotSize),
					Sell:    true,
					Market:  false,
				},
				{
					BaseID:  42,
					QuoteID: 60002,
					Rate:    6.1e5,
					Qty:     lotSize,
					Sell:    true,
					Market:  false,
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
			cexTrades:              make(map[string]*cexTradeInfo),
			orderArbRates:          test.pendingOrders,
			orderIndex:             make(map[bool]map[order.OrderID]int),
		}
		arbMM.CEX = newTCEX()
		arbMM.ctx = ctx
		arbMM.setBotLoop(arbMM.botLoop)
		arbMM.botCfgV.Store(&BotConfig{
			ArbMarketMakerConfig: &ArbMarketMakerConfig{
				Profit: profit,
				MultiHop: &MultiHopCfg{
					BaseAssetMarket:  [2]uint32{42, 60002},
					QuoteAssetMarket: [2]uint32{0, 60002},
					MarketOrders:     test.marketOrders,
				},
			},
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
				t.Fatalf("%s: expected cex order after update #%d %v but got %v", test.name, i, (expectedCEXTrade != nil), (cex.lastTrade != nil))
			}

			if cex.lastTrade != nil &&
				*cex.lastTrade != *expectedCEXTrade {
				t.Fatalf("%s: update #%d cex order %+v != expected %+v", test.name, i, cex.lastTrade, expectedCEXTrade)
			}
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestMultiHopRate(t *testing.T) {
	const lotSize uint64 = 50e8
	const baseID, quoteID uint32 = 42, 0
	const intermediateID uint32 = 60002

	mkt := &market{
		baseID:  baseID,
		quoteID: quoteID,
	}
	mkt.lotSize.Store(lotSize)

	cfg := &ArbMarketMakerConfig{
		MultiHop: &MultiHopCfg{
			BaseAssetMarket:  [2]uint32{baseID, intermediateID},
			QuoteAssetMarket: [2]uint32{quoteID, intermediateID},
		},
	}

	inverseCfg := &ArbMarketMakerConfig{
		MultiHop: &MultiHopCfg{
			BaseAssetMarket:  [2]uint32{intermediateID, baseID},
			QuoteAssetMarket: [2]uint32{intermediateID, quoteID},
		},
	}

	type vwapResult struct {
		rate   uint64
		filled bool
		err    error
	}

	type test struct {
		name                      string
		depth                     uint64
		cfg                       *ArbMarketMakerConfig
		marketOrders              bool
		limitOrdersBuffer         float64
		vwapResults               map[[2]uint32]map[bool]map[uint64]vwapResult
		invVwapResults            map[[2]uint32]map[bool]map[uint64]vwapResult
		expectedSellRate          uint64
		expectedBuyRate           uint64
		expectedBuyArbs           []*arbTradeArgs
		expectedSellArbs          []*arbTradeArgs
		expectedSellMultiHopRates [2]uint64
		expectedBuyMultiHopRates  [2]uint64
		expectedFilled            bool
		expectedError             string
	}

	inverseRate := func(rate uint64, baseConvFactor, quoteConvFactor float64) uint64 {
		convRate := float64(rate) / calc.RateEncodingFactor * baseConvFactor / quoteConvFactor
		return uint64(math.Round((1 / convRate) * quoteConvFactor / baseConvFactor * calc.RateEncodingFactor))
	}

	dcrUSDTMidGap := uint64(20000000)
	dcrUSDTBuyRate := dcrUSDTMidGap * 101 / 100 // 20200000
	dcrUSDTSellRate := dcrUSDTMidGap * 99 / 100 // 19800000
	usdtDCRBuyRate := inverseRate(dcrUSDTSellRate, 1e6, 1e8)
	usdtDCRSellRate := inverseRate(dcrUSDTBuyRate, 1e6, 1e8)
	btcUSDTMidGap := uint64(98000000000)
	btcUSDTBuyRate := btcUSDTMidGap * 101 / 100 // 98980000000
	btcUSDTSellRate := btcUSDTMidGap * 99 / 100 // 97020000000
	usdtBTCBuyRate := inverseRate(btcUSDTSellRate, 1e6, 1e8)
	usdtBTCSellRate := inverseRate(btcUSDTBuyRate, 1e6, 1e8)

	const numLots uint64 = 3

	tests := []*test{
		{
			name:              "dcr/usdt,btc/usdt",
			depth:             lotSize,
			cfg:               cfg,
			marketOrders:      false,
			limitOrdersBuffer: 0,
			vwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				{baseID, intermediateID}: {
					true: { // sell
						lotSize: {
							rate:   dcrUSDTSellRate,
							filled: true,
						},
					},
					false: { // buy
						lotSize: {
							rate:   dcrUSDTBuyRate,
							filled: true,
						},
					},
				},
			},
			invVwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				{quoteID, intermediateID}: {
					true: { // sell
						calc.BaseToQuote(dcrUSDTBuyRate, lotSize): {
							rate:   btcUSDTSellRate,
							filled: true,
						},
					},
					false: { // buy
						calc.BaseToQuote(dcrUSDTSellRate, lotSize): {
							rate:   btcUSDTBuyRate,
							filled: true,
						},
					},
				},
			},
			expectedBuyRate:  aggregateRates(dcrUSDTBuyRate, btcUSDTSellRate, mkt, cfg.MultiHop.BaseAssetMarket, cfg.MultiHop.QuoteAssetMarket),
			expectedSellRate: aggregateRates(dcrUSDTSellRate, btcUSDTBuyRate, mkt, cfg.MultiHop.BaseAssetMarket, cfg.MultiHop.QuoteAssetMarket),
			expectedBuyArbs: []*arbTradeArgs{
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      dcrUSDTBuyRate,
					qty:       lotSize,
					sell:      true,
				},
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      dcrUSDTBuyRate,
					qty:       lotSize * numLots,
					sell:      true,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimit,
					rate:      btcUSDTSellRate,
					quoteQty:  calc.BaseToQuote(dcrUSDTBuyRate, lotSize),
					sell:      false,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimit,
					rate:      btcUSDTSellRate,
					quoteQty:  calc.BaseToQuote(dcrUSDTBuyRate, lotSize*numLots),
					sell:      false,
				},
			},
			expectedSellArbs: []*arbTradeArgs{
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimit,
					rate:      dcrUSDTSellRate,
					quoteQty:  calc.BaseToQuote(dcrUSDTSellRate, lotSize),
					sell:      false,
				},
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimit,
					rate:      dcrUSDTSellRate,
					quoteQty:  calc.BaseToQuote(dcrUSDTSellRate, lotSize*numLots),
					sell:      false,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      btcUSDTBuyRate,
					qty:       calc.QuoteToBase(btcUSDTBuyRate, calc.BaseToQuote(dcrUSDTSellRate, lotSize)),
					sell:      true,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      btcUSDTBuyRate,
					qty:       calc.QuoteToBase(btcUSDTBuyRate, calc.BaseToQuote(dcrUSDTSellRate, lotSize*numLots)),
					sell:      true,
				},
			},
			expectedSellMultiHopRates: [2]uint64{btcUSDTBuyRate, dcrUSDTSellRate},
			expectedBuyMultiHopRates:  [2]uint64{dcrUSDTBuyRate, btcUSDTSellRate},
			expectedFilled:            true,
		},
		{
			name:              "usdt/dcr,usdt/btc",
			depth:             lotSize,
			cfg:               inverseCfg,
			marketOrders:      false,
			limitOrdersBuffer: 0,
			vwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				inverseCfg.MultiHop.QuoteAssetMarket: {
					true: { // sell
						calc.QuoteToBase(usdtDCRBuyRate, lotSize): {
							rate:   usdtBTCSellRate,
							filled: true,
						},
					},
					false: { // buy
						calc.QuoteToBase(usdtDCRSellRate, lotSize): {
							rate:   usdtBTCBuyRate,
							filled: true,
						},
					},
				},
			},
			invVwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				inverseCfg.MultiHop.BaseAssetMarket: {
					true: { // sell
						lotSize: {
							rate:   usdtDCRSellRate,
							filled: true,
						},
					},
					false: { // buy
						lotSize: {
							rate:   usdtDCRBuyRate,
							filled: true,
						},
					},
				},
			},
			expectedBuyRate:  aggregateRates(usdtDCRSellRate, usdtBTCBuyRate, mkt, inverseCfg.MultiHop.BaseAssetMarket, inverseCfg.MultiHop.QuoteAssetMarket),
			expectedSellRate: aggregateRates(usdtDCRBuyRate, usdtBTCSellRate, mkt, inverseCfg.MultiHop.BaseAssetMarket, inverseCfg.MultiHop.QuoteAssetMarket),
			expectedBuyArbs: []*arbTradeArgs{
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtDCRSellRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtDCRSellRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize*numLots),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimit,
					rate:      usdtBTCBuyRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimit,
					rate:      usdtBTCBuyRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize*numLots),
					sell:      true,
				},
			},
			expectedSellArbs: []*arbTradeArgs{
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimit,
					rate:      usdtDCRBuyRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimit,
					rate:      usdtDCRBuyRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize*numLots),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtBTCSellRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtBTCSellRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize*numLots),
					sell:      false,
				},
			},
			expectedSellMultiHopRates: [2]uint64{usdtBTCSellRate, usdtDCRBuyRate},
			expectedBuyMultiHopRates:  [2]uint64{usdtDCRSellRate, usdtBTCBuyRate},
			expectedFilled:            true,
		},
		{
			name:              "dcr/usdt,btc/usdt,market",
			depth:             lotSize,
			cfg:               cfg,
			marketOrders:      true,
			limitOrdersBuffer: 0,
			vwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				{baseID, intermediateID}: {
					true: { // sell
						lotSize: {
							rate:   dcrUSDTSellRate,
							filled: true,
						},
					},
					false: { // buy
						lotSize: {
							rate:   dcrUSDTBuyRate,
							filled: true,
						},
					},
				},
			},
			invVwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				{quoteID, intermediateID}: {
					true: { // sell
						calc.BaseToQuote(dcrUSDTBuyRate, lotSize): {
							rate:   btcUSDTSellRate,
							filled: true,
						},
					},
					false: { // buy
						calc.BaseToQuote(dcrUSDTSellRate, lotSize): {
							rate:   btcUSDTBuyRate,
							filled: true,
						},
					},
				},
			},
			expectedBuyRate:  aggregateRates(dcrUSDTBuyRate, btcUSDTSellRate, mkt, cfg.MultiHop.BaseAssetMarket, cfg.MultiHop.QuoteAssetMarket),
			expectedSellRate: aggregateRates(dcrUSDTSellRate, btcUSDTBuyRate, mkt, cfg.MultiHop.BaseAssetMarket, cfg.MultiHop.QuoteAssetMarket),
			expectedBuyArbs: []*arbTradeArgs{
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      dcrUSDTBuyRate,
					qty:       lotSize,
					sell:      true,
				},
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      dcrUSDTBuyRate,
					qty:       lotSize * numLots,
					sell:      true,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					quoteQty:  calc.BaseToQuote(dcrUSDTBuyRate, lotSize),
					sell:      false,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					quoteQty:  calc.BaseToQuote(dcrUSDTBuyRate, lotSize*numLots),
					sell:      false,
				},
			},
			expectedSellArbs: []*arbTradeArgs{
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					quoteQty:  calc.BaseToQuote(dcrUSDTSellRate, lotSize),
					sell:      false,
				},
				{
					baseID:    baseID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					quoteQty:  calc.BaseToQuote(dcrUSDTSellRate, lotSize*numLots),
					sell:      false,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      btcUSDTBuyRate,
					qty:       calc.QuoteToBase(btcUSDTBuyRate, calc.BaseToQuote(dcrUSDTSellRate, lotSize)),
					sell:      true,
				},
				{
					baseID:    quoteID,
					quoteID:   intermediateID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      btcUSDTBuyRate,
					qty:       calc.QuoteToBase(btcUSDTBuyRate, calc.BaseToQuote(dcrUSDTSellRate, lotSize*numLots)),
					sell:      true,
				},
			},
			expectedSellMultiHopRates: [2]uint64{btcUSDTBuyRate, dcrUSDTSellRate},
			expectedBuyMultiHopRates:  [2]uint64{dcrUSDTBuyRate, btcUSDTSellRate},
			expectedFilled:            true,
		},
		{
			name:              "usdt/dcr,usdt/btc,market",
			depth:             lotSize,
			cfg:               inverseCfg,
			marketOrders:      true,
			limitOrdersBuffer: 0,
			vwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				inverseCfg.MultiHop.QuoteAssetMarket: {
					true: { // sell
						calc.QuoteToBase(usdtDCRBuyRate, lotSize): {
							rate:   usdtBTCSellRate,
							filled: true,
						},
					},
					false: { // buy
						calc.QuoteToBase(usdtDCRSellRate, lotSize): {
							rate:   usdtBTCBuyRate,
							filled: true,
						},
					},
				},
			},
			invVwapResults: map[[2]uint32]map[bool]map[uint64]vwapResult{
				inverseCfg.MultiHop.BaseAssetMarket: {
					true: { // sell
						lotSize: {
							rate:   usdtDCRSellRate,
							filled: true,
						},
					},
					false: { // buy
						lotSize: {
							rate:   usdtDCRBuyRate,
							filled: true,
						},
					},
				},
			},
			expectedBuyRate:  aggregateRates(usdtDCRSellRate, usdtBTCBuyRate, mkt, inverseCfg.MultiHop.BaseAssetMarket, inverseCfg.MultiHop.QuoteAssetMarket),
			expectedSellRate: aggregateRates(usdtDCRBuyRate, usdtBTCSellRate, mkt, inverseCfg.MultiHop.BaseAssetMarket, inverseCfg.MultiHop.QuoteAssetMarket),
			expectedBuyArbs: []*arbTradeArgs{
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtDCRSellRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtDCRSellRate,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize*numLots),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					qty:       calc.QuoteToBase(usdtDCRSellRate, lotSize*numLots),
					sell:      true,
				},
			},
			expectedSellArbs: []*arbTradeArgs{
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   baseID,
					orderType: libxc.OrderTypeMarket,
					rate:      0,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize*numLots),
					sell:      true,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtBTCSellRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize),
					sell:      false,
				},
				{
					baseID:    intermediateID,
					quoteID:   quoteID,
					orderType: libxc.OrderTypeLimitIOC,
					rate:      usdtBTCSellRate,
					qty:       calc.QuoteToBase(usdtDCRBuyRate, lotSize*numLots),
					sell:      false,
				},
			},
			expectedSellMultiHopRates: [2]uint64{usdtBTCSellRate, usdtDCRBuyRate},
			expectedBuyMultiHopRates:  [2]uint64{usdtDCRSellRate, usdtBTCBuyRate},
			expectedFilled:            true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.cfg.MultiHop.MarketOrders = test.marketOrders
			test.cfg.MultiHop.LimitOrdersBuffer = test.limitOrdersBuffer

			mockVWAP := func(baseID, quoteID uint32, sell bool, qty uint64) (uint64, uint64, bool, error) {
				market := [2]uint32{baseID, quoteID}
				res := test.vwapResults[market][sell][qty]
				return 0, res.rate, res.filled, res.err
			}

			mockInvVWAP := func(baseID, quoteID uint32, sell bool, qty uint64) (uint64, uint64, bool, error) {
				market := [2]uint32{baseID, quoteID}
				res := test.invVwapResults[market][sell][qty]
				return 0, res.rate, res.filled, res.err
			}

			testRate := func(sell bool, expectedRate uint64, expectedArbs []*arbTradeArgs, expectedMultiHopRates [2]uint64) {
				side := map[bool]string{true: "sell", false: "buy"}[sell]
				filled, rate, multiHopRates, arbs, err := multiHopRateAndTrades(sell, test.depth, numLots, test.cfg.MultiHop, mkt, mockVWAP, mockInvVWAP)
				if test.expectedError != "" {
					if err == nil || err.Error() != test.expectedError {
						t.Fatalf("expected error %q, got %v", test.expectedError, err)
					}
					return
				}
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if filled != test.expectedFilled {
					t.Errorf("%s expected filled = %v, got %v", side, test.expectedFilled, filled)
					return
				}
				if filled && rate != expectedRate {
					t.Errorf("%s expected rate %d, got %d", side, expectedRate, rate)
					return
				}
				if filled && !reflect.DeepEqual(arbs, expectedArbs) {
					t.Errorf("%s expected arbs %s, got %s", side, spew.Sdump(expectedArbs), spew.Sdump(arbs))
				}
				if !reflect.DeepEqual(multiHopRates, expectedMultiHopRates) {
					t.Errorf("%s expected multiHopRates %s, got %s", side, spew.Sdump(expectedMultiHopRates), spew.Sdump(multiHopRates))
				}
			}

			t.Run("sell", func(t *testing.T) {
				testRate(true, test.expectedSellRate, test.expectedSellArbs, test.expectedSellMultiHopRates)
			})
			t.Run("buy", func(t *testing.T) {
				testRate(false, test.expectedBuyRate, test.expectedBuyArbs, test.expectedBuyMultiHopRates)
			})
		})
	}
}

func TestAggregateRates(t *testing.T) {
	dcrBtcMkt := mustParseMarket(&core.Market{
		BaseID:   42, // DCR
		QuoteID:  0,  // BTC
		LotSize:  5e6,
		RateStep: 1e4,
	})
	expDcrBtcRate := calc.MessageRateAlt(float64(20)/float64(98000), 1e8, 1e8)

	dcrUsdtMkt := mustParseMarket(&core.Market{
		BaseID:   42,    // DCR
		QuoteID:  60002, // USDT
		LotSize:  5e6,
		RateStep: 1e4,
	})
	expDcrUsdtRate := calc.MessageRateAlt(float64(20), 1e8, 1e6)

	tests := []struct {
		name             string
		intMarketRate    uint64
		targetMarketRate uint64
		intMarket        [2]uint32
		targetMarket     [2]uint32
		mkt              *market
		expRate          uint64
	}{
		{
			name:             "dcr/usdt -> btc/usdt",
			mkt:              dcrBtcMkt,
			intMarketRate:    calc.MessageRateAlt(20, 1e8, 1e6),    // DCR/USDT rate
			targetMarketRate: calc.MessageRateAlt(98000, 1e8, 1e6), // BTC/USDT rate
			intMarket:        [2]uint32{42, 60002},
			targetMarket:     [2]uint32{0, 60002},
			expRate:          expDcrBtcRate,
		},
		{
			name:             "usdt/dcr -> btc/usdt",
			mkt:              dcrBtcMkt,
			intMarketRate:    calc.MessageRateAlt(float64(1)/float64(20), 1e6, 1e8),
			targetMarketRate: calc.MessageRateAlt(98000, 1e8, 1e6),
			intMarket:        [2]uint32{60002, 42},
			targetMarket:     [2]uint32{0, 60002},
			expRate:          expDcrBtcRate,
		},
		{
			name:             "dcr/usdt -> usdt/btc",
			mkt:              dcrBtcMkt,
			intMarketRate:    calc.MessageRateAlt(20, 1e8, 1e6),
			targetMarketRate: calc.MessageRateAlt(float64(1)/float64(98000), 1e6, 1e8),
			intMarket:        [2]uint32{42, 60002},
			targetMarket:     [2]uint32{60002, 0},
			expRate:          expDcrBtcRate,
		},
		{
			name:             "usdt/dcr -> usdt/btc",
			mkt:              dcrBtcMkt,
			intMarketRate:    calc.MessageRateAlt(float64(1)/float64(20), 1e6, 1e8),
			targetMarketRate: calc.MessageRateAlt(float64(1)/float64(98000), 1e6, 1e8),
			intMarket:        [2]uint32{60002, 42},
			targetMarket:     [2]uint32{60002, 0},
			expRate:          expDcrBtcRate,
		},
		{
			name:             "dcr/btc -> btc/usdt",
			mkt:              dcrUsdtMkt,
			intMarketRate:    calc.MessageRateAlt(float64(20)/float64(98000), 1e8, 1e8),
			targetMarketRate: calc.MessageRateAlt(98000, 1e8, 1e6),
			intMarket:        [2]uint32{42, 0},
			targetMarket:     [2]uint32{0, 60002},
			expRate:          expDcrUsdtRate,
		},
		{
			name:             "btc/dcr -> btc/usdt",
			mkt:              dcrUsdtMkt,
			intMarketRate:    calc.MessageRateAlt(float64(98000)/float64(20), 1e8, 1e8),
			targetMarketRate: calc.MessageRateAlt(float64(98000), 1e8, 1e6),
			intMarket:        [2]uint32{0, 42},
			targetMarket:     [2]uint32{0, 60002},
			expRate:          expDcrUsdtRate,
		},
		{
			name:             "dcr/btc -> usdt/btc",
			mkt:              dcrUsdtMkt,
			intMarketRate:    calc.MessageRateAlt(float64(20)/float64(98000), 1e8, 1e8),
			targetMarketRate: calc.MessageRateAlt(float64(1)/float64(98000), 1e6, 1e8),
			intMarket:        [2]uint32{42, 0},
			targetMarket:     [2]uint32{60002, 0},
			expRate:          expDcrUsdtRate,
		},
		{
			name:             "btc/dcr -> usdt/btc",
			mkt:              dcrUsdtMkt,
			intMarketRate:    calc.MessageRateAlt(float64(98000)/float64(20), 1e8, 1e8),
			targetMarketRate: calc.MessageRateAlt(float64(1)/float64(98000), 1e6, 1e8),
			intMarket:        [2]uint32{0, 42},
			targetMarket:     [2]uint32{60002, 0},
			expRate:          expDcrUsdtRate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := aggregateRates(tt.intMarketRate, tt.targetMarketRate, tt.mkt, tt.intMarket, tt.targetMarket)
			if rate < (tt.expRate*99999/100000) || rate > (tt.expRate*100001/100000) {
				t.Fatalf("expected rate %d but got %d", tt.expRate, rate)
			}
		})
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
		additional := calc.BaseToQuote(sellRate, tt.mkt.lotSize.Load()) - calc.BaseToQuote(expectedProfitableSellRate, tt.mkt.lotSize.Load())
		if additional > tt.fees*101/100 || additional < tt.fees*99/100 {
			t.Fatalf("%s: expected additional %d but got %d", tt.name, tt.fees, additional)
		}

		buyRate, err := dexPlacementRate(tt.counterTradeRate, false, tt.profit, tt.mkt, tt.fees, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		expectedProfitableBuyRate := uint64(float64(tt.counterTradeRate) / (1 + tt.profit))
		savings := calc.BaseToQuote(expectedProfitableBuyRate, tt.mkt.lotSize.Load()) - calc.BaseToQuote(buyRate, tt.mkt.lotSize.Load())
		if savings > tt.fees*101/100 || savings < tt.fees*99/100 {
			t.Fatalf("%s: expected savings %d but got %d", tt.name, tt.fees, savings)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestConvRate(t *testing.T) {
	msgRate := uint64(94207230000)
	fmt.Println(calc.ConventionalRateAlt(msgRate, 1e8, 1e6))
}

func TestMultiHopArbCompletionParams(t *testing.T) {
	const baseID, quoteID = 42, 0 // DCR/BTC

	mkt := &core.Market{
		BaseID:  baseID,
		QuoteID: quoteID,
		LotSize: 5e9,
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
		orderArbRates:          make(map[order.OrderID][2]uint64),
		orderIndex:             make(map[bool]map[order.OrderID]int),
	}

	tests := []struct {
		name             string
		trade            *libxc.Trade
		baseAssetMarket  [2]uint32
		quoteAssetMarket [2]uint32
		marketOrders     bool
		buffer           float64
		followUpRate     uint64
		wantMakeTrade    bool
		wantOrderType    libxc.OrderType
		wantBaseID       uint32
		wantQuoteID      uint32
		wantSell         bool
		wantQty          uint64
		wantQuoteQty     uint64
		wantRate         uint64
	}{
		{
			name: "dcr/usdt, btc/usdt, sell dcr, market",
			trade: &libxc.Trade{
				BaseID:      42,
				QuoteID:     60002,
				Qty:         1e8,
				Sell:        true,
				BaseFilled:  1e8,
				QuoteFilled: 100e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeMarket,
			wantBaseID:       0,
			wantQuoteID:      60002,
			wantSell:         false,
			wantQuoteQty:     100e6,
			wantRate:         0,
		},
		{
			name: "dcr/usdt, btc/usdt, sell dcr, limit",
			trade: &libxc.Trade{
				BaseID:      42,
				QuoteID:     60002,
				Qty:         1e8,
				Sell:        true,
				BaseFilled:  1e8,
				QuoteFilled: 100e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     false,
			buffer:           0.01,
			followUpRate:     100000000,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       0,
			wantQuoteID:      60002,
			wantSell:         false,
			wantQty:          0,
			wantQuoteQty:     100e6,
			wantRate:         101000000,
		},
		{
			name: "dcr/usdt, btc/usdt, buy dcr",
			trade: &libxc.Trade{
				BaseID:      42,
				QuoteID:     60002,
				Sell:        false,
				Qty:         100e6,
				BaseFilled:  1e8,
				QuoteFilled: 100e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    false,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       0,
			wantQuoteID:      0,
			wantSell:         false,
			wantQty:          0,
			wantRate:         0,
		},
		{
			name: "dcr/usdt, btc/usdt, buy btc",
			trade: &libxc.Trade{
				BaseID:      0,
				QuoteID:     60002,
				Sell:        false,
				Qty:         1000e6,
				BaseFilled:  1e7,
				QuoteFilled: 1000e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    false,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       0,
			wantQuoteID:      0,
			wantSell:         false,
			wantQty:          0,
			wantRate:         0,
		},
		{
			name: "dcr/usdt, btc/usdt, sell btc, market",
			trade: &libxc.Trade{
				BaseID:      0,
				QuoteID:     60002,
				Qty:         1e7,
				Sell:        true,
				BaseFilled:  1e7,
				QuoteFilled: 1000e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeMarket,
			wantBaseID:       42,
			wantQuoteID:      60002,
			wantSell:         false,
			wantQty:          0,
			wantQuoteQty:     1000e6,
			wantRate:         0,
		},
		{
			name: "dcr/usdt, btc/usdt, sell btc, limit",
			trade: &libxc.Trade{
				BaseID:      0,
				QuoteID:     60002,
				Qty:         1e7,
				Sell:        true,
				BaseFilled:  1e7,
				QuoteFilled: 1000e6,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{42, 60002},
			quoteAssetMarket: [2]uint32{0, 60002},
			marketOrders:     false,
			buffer:           0.01,
			followUpRate:     100000000,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       42,
			wantQuoteID:      60002,
			wantSell:         false,
			wantQty:          0,
			wantQuoteQty:     1000e6,
			wantRate:         101000000,
		},
		{
			name: "usdt/dcr, usdt/btc, sell usdt for dcr",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     42,
				Qty:         100e6,
				Sell:        true,
				BaseFilled:  100e6,
				QuoteFilled: 1e8,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    false,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       0,
			wantQuoteID:      0,
			wantSell:         false,
			wantQty:          0,
			wantRate:         0,
		},
		{
			name: "usdt/dcr, usdt/btc, buy usdt with dcr, market",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     42,
				Sell:        false,
				Qty:         1e8,
				BaseFilled:  100e6,
				QuoteFilled: 1e8,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeMarket,
			wantBaseID:       60002,
			wantQuoteID:      0,
			wantSell:         true,
			wantQty:          100e6,
			wantRate:         0,
		},
		{
			name: "usdt/dcr, usdt/btc, buy usdt with dcr, limit",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     42,
				Sell:        false,
				Qty:         1e8,
				BaseFilled:  100e6,
				QuoteFilled: 1e8,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     false,
			buffer:           0.01,
			followUpRate:     100000000,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       60002,
			wantQuoteID:      0,
			wantSell:         true,
			wantQty:          100e6,    // since sell=true, no qty adjust
			wantRate:         99000000, // floor(100000000 * 0.99)
		},
		{
			name: "usdt/dcr, usdt/btc, sell usdt for btc",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     0,
				Qty:         1000e6,
				Sell:        true,
				BaseFilled:  1000e6,
				QuoteFilled: 1e7,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    false,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       0,
			wantQuoteID:      0,
			wantSell:         false,
			wantQty:          0,
			wantRate:         0,
		},
		{
			name: "usdt/dcr, usdt/btc, buy usdt with btc, market",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     0,
				Sell:        false,
				Qty:         1e7,
				BaseFilled:  1000e6,
				QuoteFilled: 1e7,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     true,
			buffer:           0,
			followUpRate:     0,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeMarket,
			wantBaseID:       60002,
			wantQuoteID:      42,
			wantSell:         true,
			wantQty:          1000e6,
			wantRate:         0,
		},
		{
			name: "usdt/dcr, usdt/btc, buy usdt with btc, limit",
			trade: &libxc.Trade{
				BaseID:      60002,
				QuoteID:     0,
				Sell:        false,
				Qty:         1e7,
				BaseFilled:  1000e6,
				QuoteFilled: 1e7,
				Complete:    true,
			},
			baseAssetMarket:  [2]uint32{60002, 42},
			quoteAssetMarket: [2]uint32{60002, 0},
			marketOrders:     false,
			buffer:           0.01,
			followUpRate:     100000000,
			wantMakeTrade:    true,
			wantOrderType:    libxc.OrderTypeLimit,
			wantBaseID:       60002,
			wantQuoteID:      42,
			wantSell:         true,
			wantQty:          1000e6, // sell=true, no adjust
			wantRate:         99000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ArbMarketMakerConfig{
				MultiHop: &MultiHopCfg{
					BaseAssetMarket:   tt.baseAssetMarket,
					QuoteAssetMarket:  tt.quoteAssetMarket,
					MarketOrders:      tt.marketOrders,
					LimitOrdersBuffer: tt.buffer,
				},
			}
			a.botCfgV.Store(&BotConfig{
				ArbMarketMakerConfig: cfg,
			})
			makeTrade, orderType, baseID, quoteID, sell, qty, quoteQty, rate := a.multiHopArbCompletionParams(tt.trade, tt.followUpRate)
			if makeTrade != tt.wantMakeTrade {
				t.Errorf("makeTrade = %v, want %v", makeTrade, tt.wantMakeTrade)
			}
			if orderType != tt.wantOrderType {
				t.Errorf("orderType = %v, want %v", orderType, tt.wantOrderType)
			}
			if baseID != tt.wantBaseID {
				t.Errorf("baseID = %v, want %v", baseID, tt.wantBaseID)
			}
			if quoteID != tt.wantQuoteID {
				t.Errorf("quoteID = %v, want %v", quoteID, tt.wantQuoteID)
			}
			if sell != tt.wantSell {
				t.Errorf("sell = %v, want %v", sell, tt.wantSell)
			}
			if qty != tt.wantQty {
				t.Errorf("qty = %v, want %v", qty, tt.wantQty)
			}
			if quoteQty != tt.wantQuoteQty {
				t.Errorf("quoteQty = %v, want %v", quoteQty, tt.wantQuoteQty)
			}
			if rate != tt.wantRate {
				t.Errorf("rate = %v, want %v", rate, tt.wantRate)
			}
		})
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
		internalTransfer: func(mwh *MarketWithHost, fn doInternalTransferFunc) error {
			return fn(map[uint32]uint64{}, map[uint32]uint64{})
		},
	}

	u.botCfgV.Store(&BotConfig{
		Host:    u.host,
		BaseID:  u.baseID,
		QuoteID: u.quoteID,
	})

	return u
}

func updateInternalTransferBalances(u *unifiedExchangeAdaptor, baseBal, quoteBal map[uint32]uint64) {
	u.internalTransfer = func(mwh *MarketWithHost, fn doInternalTransferFunc) error {
		return fn(baseBal, quoteBal)
	}
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
