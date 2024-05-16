// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"github.com/davecgh/go-spew/spew"
)

func TestArbMMRebalance(t *testing.T) {
	const lotSize uint64 = 50e8
	const currEpoch uint64 = 100
	const baseID = 42
	const quoteID = 0

	cfg := &ArbMarketMakerConfig{
		Profit:             0.01,
		NumEpochsLeaveOpen: 5,
		BuyPlacements: []*ArbMarketMakingPlacement{
			{
				Lots:       2,
				Multiplier: 1.5,
			},
			{
				Lots:       1,
				Multiplier: 2,
			},
		},
		SellPlacements: []*ArbMarketMakingPlacement{
			{
				Lots:       2,
				Multiplier: 2.0,
			},
			{
				Lots:       1,
				Multiplier: 1.5,
			},
			{
				Lots:       1,
				Multiplier: 1.5,
			},
		},
	}

	bidsVWAP := map[uint64]*vwapResult{
		4 * lotSize: {
			avg:     5e6,
			extrema: 4.5e6,
		},
		11 * lotSize / 2 /* 5.5 lots */ : {
			avg:     4e6,
			extrema: 3e6,
		},
		// no result for 7 lots. CEX order doesn't have enough liquidity.
	}

	asksVWAP := map[uint64]*vwapResult{
		3 * lotSize: {
			avg:     6e6,
			extrema: 6.5e6,
		},
		5 * lotSize: {
			avg:     7e6,
			extrema: 7.5e6,
		},
	}

	mkt := &core.Market{
		RateStep:   1e3,
		AtomToConv: 1,
		LotSize:    lotSize,
		BaseID:     baseID,
		QuoteID:    quoteID,
	}
	buyFeesInQuoteUnits := uint64(2e5)
	sellFeesInQuoteUnits := uint64(3e5)

	coreAdaptor := newTBotCoreAdaptor(newTCore())
	coreAdaptor.buyFeesInQuote = buyFeesInQuoteUnits
	coreAdaptor.sellFeesInQuote = sellFeesInQuoteUnits

	cexAdaptor := newTBotCEXAdaptor()
	cexAdaptor.asksVWAP = asksVWAP
	cexAdaptor.bidsVWAP = bidsVWAP

	// In the first call, these values will be returned as reserves,
	// then in the second call they should returned as the rebalance
	// amount prompting a withdrawal/deposit.
	const baseCexReserves uint64 = 2e5
	const quoteDexReserves uint64 = 3e5
	cexAdaptor.prepareRebalanceResults[baseID] = &prepareRebalanceResult{
		cexReserves: baseCexReserves,
	}
	cexAdaptor.prepareRebalanceResults[quoteID] = &prepareRebalanceResult{
		dexReserves: quoteDexReserves,
	}

	arbMM := &arbMarketMaker{
		baseID:      baseID,
		quoteID:     quoteID,
		cex:         cexAdaptor,
		core:        coreAdaptor,
		log:         tLogger,
		mkt:         mkt,
		dexReserves: make(map[uint32]uint64),
		cexReserves: make(map[uint32]uint64),
	}
	arbMM.cfgV.Store(cfg)

	arbMM.rebalance(currEpoch)

	dexRate := func(counterTradeRate uint64, sell bool) uint64 {
		fees := buyFeesInQuoteUnits
		if sell {
			fees = sellFeesInQuoteUnits
		}
		rate, err := dexPlacementRate(counterTradeRate, sell, 0.01, mkt, fees, tLogger)
		if err != nil {
			panic(err)
		}
		return rate
	}
	expBuyPlacements := []*multiTradePlacement{
		{
			lots:             2,
			rate:             dexRate(6.5e6, false),
			counterTradeRate: 6.5e6,
		},
		{
			lots:             1,
			rate:             dexRate(7.5e6, false),
			counterTradeRate: 7.5e6,
		},
	}

	expSellPlacements := []*multiTradePlacement{
		{
			lots:             2,
			rate:             dexRate(4.5e6, true),
			counterTradeRate: 4.5e6,
		},
		{
			lots:             1,
			rate:             dexRate(3e6, true),
			counterTradeRate: 3e6,
		},
		{
			lots: 0,
			rate: 0,
		},
	}
	if !reflect.DeepEqual(expBuyPlacements, coreAdaptor.lastMultiTradeBuys) {
		t.Fatal(spew.Sprintf("expected buy placements:\n%#+v\ngot:\n%#+v", expBuyPlacements, coreAdaptor.lastMultiTradeBuys))
	}
	if !reflect.DeepEqual(expSellPlacements, coreAdaptor.lastMultiTradeSells) {
		t.Fatal(spew.Sprintf("expected sell placements:\n%#+v\ngot:\n%#+v", expSellPlacements, coreAdaptor.lastMultiTradeSells))
	}
	delete(arbMM.cexTrades, "1")            // this normally will get deleted when an update arrives from the CEX
	cexAdaptor.cancelledTrades = []string{} // reset

	cexAdaptor.prepareRebalanceResults[baseID] = &prepareRebalanceResult{
		rebalance: -int64(baseCexReserves),
	}
	cexAdaptor.prepareRebalanceResults[quoteID] = &prepareRebalanceResult{
		rebalance: int64(quoteDexReserves),
	}

	arbMM.rebalance(currEpoch + 1)

	// The CEX orderbook hasn't changed, so the rates are the same.
	if !reflect.DeepEqual(expBuyPlacements, coreAdaptor.lastMultiTradeBuys) {
		t.Fatal(spew.Sprintf("expected buy placements:\n%#+v\ngot:\n%#+v", expBuyPlacements, coreAdaptor.lastMultiTradeBuys))
	}
	if !reflect.DeepEqual(expSellPlacements, coreAdaptor.lastMultiTradeSells) {
		t.Fatal(spew.Sprintf("expected sell placements:\n%#+v\ngot:\n%#+v", expSellPlacements, coreAdaptor.lastMultiTradeSells))
	}

	// Make sure MultiTrade was called with the correct reserve arguments.
	expectedDEXReserves := map[uint32]uint64{baseID: 0, quoteID: quoteDexReserves}
	expectedCEXReserves := map[uint32]uint64{baseID: baseCexReserves, quoteID: 0}
	if !reflect.DeepEqual(coreAdaptor.buysCEXReserves, expectedCEXReserves) {
		t.Fatalf("expected cex reserves:\n%+v\ngot:\n%+v", expectedCEXReserves, coreAdaptor.buysCEXReserves)
	}
	if !reflect.DeepEqual(coreAdaptor.sellsCEXReserves, expectedCEXReserves) {
		t.Fatalf("expected cex reserves:\n%+v\ngot:\n%+v", expectedCEXReserves, coreAdaptor.sellsCEXReserves)
	}
	if !reflect.DeepEqual(coreAdaptor.buysDEXReserves, expectedDEXReserves) {
		t.Fatalf("expected dex reserves:\n%+v\ngot:\n%+v", expectedDEXReserves, coreAdaptor.buysDEXReserves)
	}
	if !reflect.DeepEqual(coreAdaptor.sellsDEXReserves, expectedDEXReserves) {
		t.Fatalf("expected dex reserves:\n%+v\ngot:\n%+v", expectedDEXReserves, coreAdaptor.sellsDEXReserves)
	}

	expectedWithdrawal := &withdrawArgs{
		assetID: baseID,
		amt:     baseCexReserves,
	}
	if !reflect.DeepEqual(expectedWithdrawal, cexAdaptor.lastWithdrawArgs) {
		t.Fatalf("expected withdrawal:\n%+v\ngot:\n%+v", expectedWithdrawal, cexAdaptor.lastWithdrawArgs)
	}

	expectedDeposit := &withdrawArgs{
		assetID: quoteID,
		amt:     quoteDexReserves,
	}
	if !reflect.DeepEqual(expectedDeposit, cexAdaptor.lastDepositArgs) {
		t.Fatalf("expected deposit:\n%+v\ngot:\n%+v", expectedDeposit, cexAdaptor.lastDepositArgs)
	}

	arbMM.cexTrades = make(map[string]uint64)
	arbMM.cexTrades["1"] = currEpoch + 2 - cfg.NumEpochsLeaveOpen
	arbMM.cexTrades["2"] = currEpoch + 2 - cfg.NumEpochsLeaveOpen + 1
	arbMM.rebalance(currEpoch + 2)

	expectedCancels := []string{"1"}
	if !reflect.DeepEqual(expectedCancels, cexAdaptor.cancelledTrades) {
		t.Fatalf("expected cancels:\n%+v\ngot:\n%+v", expectedCancels, cexAdaptor.cancelledTrades)
	}
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
			cex:           cex,
			core:          coreAdaptor,
			ctx:           ctx,
			baseID:        42,
			quoteID:       0,
			matchesSeen:   make(map[order.MatchID]bool),
			cexTrades:     make(map[string]uint64),
			mkt:           mkt,
			pendingOrders: test.pendingOrders,
		}
		arbMM.cfgV.Store(&ArbMarketMakerConfig{
			Profit: profit,
		})
		arbMM.currEpoch.Store(123)
		_, err := arbMM.Connect(ctx)
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
		mkt              *core.Market
	}

	tests := []*test{
		{
			name:             "dcr/btc",
			counterTradeRate: 5e6,
			profit:           0.03,
			base:             42,
			quote:            0,
			fees:             4e5,
			mkt: &core.Market{
				BaseID:   42,
				QuoteID:  0,
				LotSize:  40e8,
				RateStep: 1e2,
			},
		},
		{
			name:             "btc/usdc.eth",
			counterTradeRate: calc.MessageRateAlt(43000, 1e8, 1e6),
			profit:           0.01,
			base:             0,
			quote:            60001,
			fees:             5e5,
			mkt: &core.Market{
				BaseID:   0,
				QuoteID:  60001,
				LotSize:  5e6,
				RateStep: 1e4,
			},
		},
		{
			name:             "wbtc.polygon/usdc.eth",
			counterTradeRate: calc.MessageRateAlt(43000, 1e8, 1e6),
			profit:           0.02,
			base:             966003,
			quote:            60001,
			fees:             3e5,
			mkt: &core.Market{
				BaseID:   966003,
				QuoteID:  60001,
				LotSize:  5e6,
				RateStep: 1e4,
			},
		},
	}

	runTest := func(tt *test) {
		sellRate, err := dexPlacementRate(tt.counterTradeRate, true, tt.profit, tt.mkt, tt.fees, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}

		expectedProfitableSellRate := uint64(float64(tt.counterTradeRate) * (1 + tt.profit))
		additional := calc.BaseToQuote(sellRate, tt.mkt.LotSize) - calc.BaseToQuote(expectedProfitableSellRate, tt.mkt.LotSize)
		if additional > tt.fees*101/100 || additional < tt.fees*99/100 {
			t.Fatalf("%s: expected additional %d but got %d", tt.name, tt.fees, additional)
		}

		buyRate, err := dexPlacementRate(tt.counterTradeRate, false, tt.profit, tt.mkt, tt.fees, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		expectedProfitableBuyRate := uint64(float64(tt.counterTradeRate) / (1 + tt.profit))
		savings := calc.BaseToQuote(expectedProfitableBuyRate, tt.mkt.LotSize) - calc.BaseToQuote(buyRate, tt.mkt.LotSize)
		if savings > tt.fees*101/100 || savings < tt.fees*99/100 {
			t.Fatalf("%s: expected savings %d but got %d", tt.name, tt.fees, savings)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}
