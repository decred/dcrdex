//go:build !harness && !botlive

package mm

import (
	"math"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/calc"
)

type tBasicMMCalculator struct {
	bp    uint64
	bpErr error

	hs uint64
}

var _ basicMMCalculator = (*tBasicMMCalculator)(nil)

func (r *tBasicMMCalculator) basisPrice() (uint64, error) {
	return r.bp, r.bpErr
}
func (r *tBasicMMCalculator) halfSpread(basisPrice uint64) (uint64, error) {
	return r.hs, nil
}

func (r *tBasicMMCalculator) feeGapStats(basisPrice uint64) (*FeeGapStats, error) {
	return &FeeGapStats{FeeGap: r.hs * 2}, nil
}
func TestBasisPrice(t *testing.T) {
	mkt := &core.Market{
		RateStep:   1,
		BaseID:     42,
		QuoteID:    0,
		AtomToConv: 1,
	}

	tests := []*struct {
		name        string
		oraclePrice uint64
		fiatRate    uint64
		exp         uint64
	}{
		{
			name:        "oracle price",
			oraclePrice: 2000,
			fiatRate:    1900,
			exp:         2000,
		},
		{
			name:        "failed sanity check",
			oraclePrice: 2000,
			fiatRate:    1850, // mismatch > 5%
			exp:         0,
		},
		{
			name:        "no oracle price",
			oraclePrice: 0,
			fiatRate:    1000,
			exp:         1000,
		},
		{
			name:        "no oracle price or fiat rate",
			oraclePrice: 0,
			fiatRate:    0,
			exp:         0,
		},
	}

	for _, tt := range tests {
		oracle := &tOracle{
			marketPrice: mkt.MsgRateToConventional(tt.oraclePrice),
		}

		tCore := newTCore()
		adaptor := newTBotCoreAdaptor(tCore)
		adaptor.fiatExchangeRate = tt.fiatRate

		calculator := &basicMMCalculatorImpl{
			market: mustParseMarket(mkt),
			oracle: oracle,
			cfg:    &BasicMarketMakingConfig{},
			log:    tLogger,
			core:   adaptor,
		}

		rate, _ := calculator.basisPrice()
		if rate != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, rate, tt.exp)
		}
	}
}

func TestBreakEvenHalfSpread(t *testing.T) {
	tests := []*struct {
		name                 string
		basisPrice           uint64
		mkt                  *core.Market
		buyFeesInBaseUnits   uint64
		sellFeesInBaseUnits  uint64
		buyFeesInQuoteUnits  uint64
		sellFeesInQuoteUnits uint64
		singleLotFeesErr     error
		expErr               bool
	}{
		{
			name:   "basis price = 0 not allowed",
			expErr: true,
			mkt: &core.Market{
				LotSize: 20e8,
				BaseID:  42,
				QuoteID: 0,
			},
		},
		{
			name:       "dcr/btc",
			basisPrice: 5e7, // 0.4 BTC/DCR, quote lot = 8 BTC
			mkt: &core.Market{
				LotSize: 20e8,
				BaseID:  42,
				QuoteID: 0,
			},
			buyFeesInBaseUnits:   2.2e6,
			sellFeesInBaseUnits:  2e6,
			buyFeesInQuoteUnits:  calc.BaseToQuote(2.2e6, 5e7),
			sellFeesInQuoteUnits: calc.BaseToQuote(2e6, 5e7),
		},
		{
			name:       "btc/usdc.eth",
			basisPrice: calc.MessageRateAlt(43000, 1e8, 1e6),
			mkt: &core.Market{
				BaseID:  0,
				QuoteID: 60001,
				LotSize: 1e7,
			},
			buyFeesInBaseUnits:   1e6,
			sellFeesInBaseUnits:  2e6,
			buyFeesInQuoteUnits:  calc.BaseToQuote(calc.MessageRateAlt(43000, 1e8, 1e6), 1e6),
			sellFeesInQuoteUnits: calc.BaseToQuote(calc.MessageRateAlt(43000, 1e8, 1e6), 2e6),
		},
	}

	for _, tt := range tests {
		tCore := newTCore()
		coreAdaptor := newTBotCoreAdaptor(tCore)
		coreAdaptor.buyFeesInBase = tt.buyFeesInBaseUnits
		coreAdaptor.sellFeesInBase = tt.sellFeesInBaseUnits
		coreAdaptor.buyFeesInQuote = tt.buyFeesInQuoteUnits
		coreAdaptor.sellFeesInQuote = tt.sellFeesInQuoteUnits

		calculator := &basicMMCalculatorImpl{
			market: mustParseMarket(tt.mkt),
			core:   coreAdaptor,
			log:    tLogger,
		}

		halfSpread, err := calculator.halfSpread(tt.basisPrice)
		if (err != nil) != tt.expErr {
			t.Fatalf("expErr = %t, err = %v", tt.expErr, err)
		}
		if tt.expErr {
			continue
		}

		afterSell := calc.BaseToQuote(tt.basisPrice+halfSpread, tt.mkt.LotSize)
		afterBuy := calc.QuoteToBase(tt.basisPrice-halfSpread, afterSell)
		fees := afterBuy - tt.mkt.LotSize
		expectedFees := tt.buyFeesInBaseUnits + tt.sellFeesInBaseUnits

		if expectedFees > fees*10001/10000 || expectedFees < fees*9999/10000 {
			t.Fatalf("%s: expected fees %d, got %d", tt.name, expectedFees, fees)
		}

	}
}

func TestUpdateLotSize(t *testing.T) {
	tests := []struct {
		name           string
		placements     []*OrderPlacement
		originalSize   uint64
		newSize        uint64
		wantPlacements []*OrderPlacement
	}{
		{
			name: "simple halving",
			placements: []*OrderPlacement{
				{Lots: 2, GapFactor: 1.0},
				{Lots: 4, GapFactor: 2.0},
			},
			originalSize: 100,
			newSize:      200,
			wantPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: 1.0},
				{Lots: 2, GapFactor: 2.0},
			},
		},
		{
			name: "rounding up",
			placements: []*OrderPlacement{
				{Lots: 3, GapFactor: 1.0},
				{Lots: 1, GapFactor: 1.0},
			},
			originalSize: 100,
			newSize:      160,
			wantPlacements: []*OrderPlacement{
				{Lots: 2, GapFactor: 1.0},
			},
		},
		{
			name: "minimum 1 lot",
			placements: []*OrderPlacement{
				{Lots: 1, GapFactor: 1.0},
				{Lots: 1, GapFactor: 1.0},
				{Lots: 1, GapFactor: 1.0},
			},
			originalSize: 100,
			newSize:      250,
			wantPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: 1.0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateLotSize(tt.placements, tt.originalSize, tt.newSize)
			if len(got) != len(tt.wantPlacements) {
				t.Fatalf("got %d placements, want %d", len(got), len(tt.wantPlacements))
			}
			for i := range got {
				if got[i].Lots != tt.wantPlacements[i].Lots {
					t.Errorf("placement %d: got %d lots, want %d", i, got[i].Lots, tt.wantPlacements[i].Lots)
				}
				if got[i].GapFactor != tt.wantPlacements[i].GapFactor {
					t.Errorf("placement %d: got %f gap factor, want %f", i, got[i].GapFactor, tt.wantPlacements[i].GapFactor)
				}
			}
		})
	}
}

func TestBasicMMRebalance(t *testing.T) {
	const basisPrice uint64 = 5e6
	const halfSpread uint64 = 2e5
	const rateStep uint64 = 1e3
	const atomToConv float64 = 1

	calculator := &tBasicMMCalculator{
		bp: basisPrice,
		hs: halfSpread,
	}

	type test struct {
		name              string
		strategy          GapStrategy
		cfgBuyPlacements  []*OrderPlacement
		cfgSellPlacements []*OrderPlacement

		expBuyPlacements  []*TradePlacement
		expSellPlacements []*TradePlacement
	}
	tests := []*test{
		{
			name:     "multiplier",
			strategy: GapStrategyMultiplier,
			cfgBuyPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: 3},
				{Lots: 2, GapFactor: 2},
				{Lots: 3, GapFactor: 1},
			},
			cfgSellPlacements: []*OrderPlacement{
				{Lots: 3, GapFactor: 1},
				{Lots: 2, GapFactor: 2},
				{Lots: 1, GapFactor: 3},
			},
			expBuyPlacements: []*TradePlacement{
				{Lots: 1, Rate: steppedRate(basisPrice-3*halfSpread, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice-2*halfSpread, rateStep)},
				{Lots: 3, Rate: steppedRate(basisPrice-1*halfSpread, rateStep)},
			},
			expSellPlacements: []*TradePlacement{
				{Lots: 3, Rate: steppedRate(basisPrice+1*halfSpread, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice+2*halfSpread, rateStep)},
				{Lots: 1, Rate: steppedRate(basisPrice+3*halfSpread, rateStep)},
			},
		},
		{
			name:     "percent",
			strategy: GapStrategyPercent,
			cfgBuyPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: 0.05},
				{Lots: 2, GapFactor: 0.1},
				{Lots: 3, GapFactor: 0.15},
			},
			cfgSellPlacements: []*OrderPlacement{
				{Lots: 3, GapFactor: 0.15},
				{Lots: 2, GapFactor: 0.1},
				{Lots: 1, GapFactor: 0.05},
			},
			expBuyPlacements: []*TradePlacement{
				{Lots: 1, Rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{Lots: 3, Rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
			},
			expSellPlacements: []*TradePlacement{
				{Lots: 3, Rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{Lots: 1, Rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
			},
		},
		{
			name:     "percent-plus",
			strategy: GapStrategyPercentPlus,
			cfgBuyPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: 0.05},
				{Lots: 2, GapFactor: 0.1},
				{Lots: 3, GapFactor: 0.15},
			},
			cfgSellPlacements: []*OrderPlacement{
				{Lots: 3, GapFactor: 0.15},
				{Lots: 2, GapFactor: 0.1},
				{Lots: 1, GapFactor: 0.05},
			},
			expBuyPlacements: []*TradePlacement{
				{Lots: 1, Rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{Lots: 3, Rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
			},
			expSellPlacements: []*TradePlacement{
				{Lots: 3, Rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{Lots: 1, Rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
			},
		},
		{
			name:     "absolute",
			strategy: GapStrategyAbsolute,
			cfgBuyPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: .01},
				{Lots: 2, GapFactor: .03},
				{Lots: 3, GapFactor: .06},
			},
			cfgSellPlacements: []*OrderPlacement{
				{Lots: 3, GapFactor: .06},
				{Lots: 2, GapFactor: .03},
				{Lots: 1, GapFactor: .01},
			},
			expBuyPlacements: []*TradePlacement{
				{Lots: 1, Rate: steppedRate(basisPrice-1e6, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice-3e6, rateStep)},
			},
			expSellPlacements: []*TradePlacement{
				{Lots: 3, Rate: steppedRate(basisPrice+6e6, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice+3e6, rateStep)},
				{Lots: 1, Rate: steppedRate(basisPrice+1e6, rateStep)},
			},
		},
		{
			name:     "absolute-plus",
			strategy: GapStrategyAbsolutePlus,
			cfgBuyPlacements: []*OrderPlacement{
				{Lots: 1, GapFactor: .01},
				{Lots: 2, GapFactor: .03},
				{Lots: 3, GapFactor: .06},
			},
			cfgSellPlacements: []*OrderPlacement{
				{Lots: 3, GapFactor: .06},
				{Lots: 2, GapFactor: .03},
				{Lots: 1, GapFactor: .01},
			},
			expBuyPlacements: []*TradePlacement{
				{Lots: 1, Rate: steppedRate(basisPrice-halfSpread-1e6, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice-halfSpread-3e6, rateStep)},
			},
			expSellPlacements: []*TradePlacement{
				{Lots: 3, Rate: steppedRate(basisPrice+halfSpread+6e6, rateStep)},
				{Lots: 2, Rate: steppedRate(basisPrice+halfSpread+3e6, rateStep)},
				{Lots: 1, Rate: steppedRate(basisPrice+halfSpread+1e6, rateStep)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const lotSize = 5e9
			const baseID, quoteID = 42, 0
			mm := &basicMarketMaker{
				unifiedExchangeAdaptor: mustParseAdaptorFromMarket(&core.Market{
					RateStep:   rateStep,
					AtomToConv: atomToConv,
					LotSize:    lotSize,
					BaseID:     baseID,
					QuoteID:    quoteID,
				}),
				calculator: calculator,
			}
			tcore := newTCore()
			tcore.setWalletsAndExchange(&core.Market{
				BaseID:  baseID,
				QuoteID: quoteID,
			})
			mm.clientCore = tcore
			mm.botCfgV.Store(&BotConfig{})
			mm.fiatRates.Store(map[uint32]float64{baseID: 1, quoteID: 1})
			const sellSwapFees, sellRedeemFees = 3e6, 1e6
			const buySwapFees, buyRedeemFees = 2e5, 1e5
			mm.buyFees = &OrderFees{
				LotFeeRange: &LotFeeRange{
					Max: &LotFees{
						Redeem: buyRedeemFees,
						Swap:   buySwapFees,
					},
					Estimated: &LotFees{},
				},
				BookingFeesPerLot: buySwapFees,
			}
			mm.sellFees = &OrderFees{
				LotFeeRange: &LotFeeRange{
					Max: &LotFees{
						Redeem: sellRedeemFees,
						Swap:   sellSwapFees,
					},
					Estimated: &LotFees{},
				},
				BookingFeesPerLot: sellSwapFees,
			}
			mm.baseDexBalances[baseID] = lotSize * 50
			mm.baseCexBalances[baseID] = lotSize * 50
			mm.baseDexBalances[quoteID] = int64(calc.BaseToQuote(basisPrice, lotSize*50))
			mm.baseCexBalances[quoteID] = int64(calc.BaseToQuote(basisPrice, lotSize*50))
			mm.unifiedExchangeAdaptor.botCfgV.Store(&BotConfig{
				BasicMMConfig: &BasicMarketMakingConfig{
					GapStrategy:    tt.strategy,
					BuyPlacements:  tt.cfgBuyPlacements,
					SellPlacements: tt.cfgSellPlacements,
				}})

			mm.rebalance(100)

			if len(tcore.multiTradesPlaced) != 2 {
				t.Fatal("expected both buy and sell orders placed")
			}
			buys, sells := tcore.multiTradesPlaced[0], tcore.multiTradesPlaced[1]

			expOrdersN := len(tt.expBuyPlacements) + len(tt.expSellPlacements)
			if len(buys.Placements)+len(sells.Placements) != expOrdersN {
				t.Fatalf("expected %d orders, got %d", expOrdersN, len(buys.Placements)+len(sells.Placements))
			}

			buyRateLots := make(map[uint64]uint64, len(buys.Placements))
			for _, p := range buys.Placements {
				buyRateLots[p.Rate] = p.Qty / lotSize
			}
			for _, expBuy := range tt.expBuyPlacements {
				if lots, found := buyRateLots[expBuy.Rate]; !found {
					t.Fatalf("buy rate %d not found", expBuy.Rate)
				} else {
					if expBuy.Lots != lots {
						t.Fatalf("wrong lots %d for buy at rate %d", lots, expBuy.Rate)
					}
				}
			}
			sellRateLots := make(map[uint64]uint64, len(sells.Placements))
			for _, p := range sells.Placements {
				sellRateLots[p.Rate] = p.Qty / lotSize
			}
			for _, expSell := range tt.expSellPlacements {
				if lots, found := sellRateLots[expSell.Rate]; !found {
					t.Fatalf("sell rate %d not found", expSell.Rate)
				} else {
					if expSell.Lots != lots {
						t.Fatalf("wrong lots %d for sell at rate %d", lots, expSell.Rate)
					}
				}
			}
		})
	}
}
