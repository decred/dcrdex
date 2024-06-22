//go:build !harness && !botlive

package mm

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/msgjson"
)

type tBasicMMCalculator struct {
	bp                       uint64
	bpOracleOutsideSafeRange bool
	bpErr                    error

	hs uint64
}

var _ basicMMCalculator = (*tBasicMMCalculator)(nil)

func (r *tBasicMMCalculator) basisPrice() (uint64, bool, error) {
	return r.bp, r.bpOracleOutsideSafeRange, r.bpErr
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
		name            string
		midGap          uint64
		oraclePrice     uint64
		oracleBias      float64
		oracleWeight    float64
		conversions     map[uint32]float64
		fiatRate        uint64
		emptyMarketRate float64

		expBP                     uint64
		expErr                    error
		expOutsideOracleSafeRange bool
	}{
		{
			name:   "just mid-gap is enough",
			midGap: 123e5,
			expBP:  123e5,
		},
		{
			name:         "mid-gap + oracle weight",
			midGap:       1950,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			expBP:        1975,
		},
		{
			name:                      "adjusted mid-gap + oracle weight",
			midGap:                    1000, // adjusted to 1940
			oraclePrice:               2000,
			oracleWeight:              0.5,
			expBP:                     1970,
			expOutsideOracleSafeRange: true,
		},
		{
			name:         "no mid-gap effectively sets oracle weight to 100%",
			midGap:       0,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			expBP:        2000,
		},
		{
			name:         "mid-gap + oracle weight + oracle bias",
			midGap:       1950,
			oraclePrice:  2000,
			oracleBias:   -0.01, // minus 20
			oracleWeight: 0.75,
			expBP:        1972, // 0.25 * 1950 + 0.75 * (2000 - 20) = 1972
		},
		{
			name:         "oracle not available",
			oracleWeight: 0.5,
			oraclePrice:  0,
			expErr:       errNoOracleAvailable,
		},
		{
			name:            "no mid-gap, oracle weight, fiat rate, use empty market rate",
			emptyMarketRate: 1200.0,
			expBP:           calc.MessageRateAlt(1200, 1e8, 1e8),
		},
		{
			name:         "no mid-gap, no oracle weight, no empty market rate",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0,
			expBP:        0,
			expErr:       errNoBasisPrice,
		},
		{
			name:         "no mid-gap and no oracle weight, but fiat rate is set",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0,
			fiatRate:     1200,
			expBP:        1200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oracle := &tOracle{
				marketPrice: mkt.MsgRateToConventional(tt.oraclePrice),
			}
			ob := &tOrderBook{
				midGap: tt.midGap,
			}
			cfg := &BasicMarketMakingConfig{
				OracleWeighting: &tt.oracleWeight,
				OracleBias:      tt.oracleBias,
				EmptyMarketRate: tt.emptyMarketRate,
			}

			tCore := newTCore()
			adaptor := newTBotCoreAdaptor(tCore)
			adaptor.fiatExchangeRate = tt.fiatRate

			calculator := &basicMMCalculatorImpl{
				market: mustParseMarket(mkt),
				book:   ob,
				oracle: oracle,
				cfg:    cfg,
				log:    tLogger,
				core:   adaptor,
			}

			rate, outsideSafeRange, err := calculator.basisPrice()
			if err != nil {
				if tt.expErr != err {
					t.Fatalf("expected error %v, got %v", tt.expErr, err)
				}
				return
			}

			if outsideSafeRange != tt.expOutsideOracleSafeRange {
				t.Fatalf("expected outsideSafeRange %t, got %t", tt.expOutsideOracleSafeRange, outsideSafeRange)
			}

			if rate != tt.expBP {
				t.Fatalf("%s: %d != %d", tt.name, rate, tt.expBP)
			}
		})
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

		expBuyPlacements  []*multiTradePlacement
		expSellPlacements []*multiTradePlacement
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
			expBuyPlacements: []*multiTradePlacement{
				{lots: 1, rate: steppedRate(basisPrice-3*halfSpread, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice-2*halfSpread, rateStep)},
				{lots: 3, rate: steppedRate(basisPrice-1*halfSpread, rateStep)},
			},
			expSellPlacements: []*multiTradePlacement{
				{lots: 3, rate: steppedRate(basisPrice+1*halfSpread, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice+2*halfSpread, rateStep)},
				{lots: 1, rate: steppedRate(basisPrice+3*halfSpread, rateStep)},
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
			expBuyPlacements: []*multiTradePlacement{
				{lots: 1, rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
				{lots: 2, rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{lots: 3, rate: steppedRate(basisPrice-uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
			},
			expSellPlacements: []*multiTradePlacement{
				{lots: 3, rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
				{lots: 2, rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{lots: 1, rate: steppedRate(basisPrice+uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
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
			expBuyPlacements: []*multiTradePlacement{
				{lots: 1, rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
				{lots: 2, rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{lots: 3, rate: steppedRate(basisPrice-halfSpread-uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
			},
			expSellPlacements: []*multiTradePlacement{
				{lots: 3, rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.15))), rateStep)},
				{lots: 2, rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.1))), rateStep)},
				{lots: 1, rate: steppedRate(basisPrice+halfSpread+uint64(math.Round((float64(basisPrice)*0.05))), rateStep)},
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
			expBuyPlacements: []*multiTradePlacement{
				{lots: 1, rate: steppedRate(basisPrice-1e6, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice-3e6, rateStep)},
			},
			expSellPlacements: []*multiTradePlacement{
				{lots: 3, rate: steppedRate(basisPrice+6e6, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice+3e6, rateStep)},
				{lots: 1, rate: steppedRate(basisPrice+1e6, rateStep)},
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
			expBuyPlacements: []*multiTradePlacement{
				{lots: 1, rate: steppedRate(basisPrice-halfSpread-1e6, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice-halfSpread-3e6, rateStep)},
			},
			expSellPlacements: []*multiTradePlacement{
				{lots: 3, rate: steppedRate(basisPrice+halfSpread+6e6, rateStep)},
				{lots: 2, rate: steppedRate(basisPrice+halfSpread+3e6, rateStep)},
				{lots: 1, rate: steppedRate(basisPrice+halfSpread+1e6, rateStep)},
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
			mm.buyFees = &orderFees{
				LotFeeRange: &LotFeeRange{
					Max: &LotFees{
						Redeem: buyRedeemFees,
						Swap:   buySwapFees,
					},
					Estimated: &LotFees{},
				},
				bookingFeesPerLot: buySwapFees,
			}
			mm.sellFees = &orderFees{
				LotFeeRange: &LotFeeRange{
					Max: &LotFees{
						Redeem: sellRedeemFees,
						Swap:   sellSwapFees,
					},
					Estimated: &LotFees{},
				},
				bookingFeesPerLot: sellSwapFees,
			}
			mm.baseDexBalances[baseID] = lotSize * 50
			mm.baseCexBalances[baseID] = lotSize * 50
			mm.baseDexBalances[quoteID] = int64(calc.BaseToQuote(basisPrice, lotSize*50))
			mm.baseCexBalances[quoteID] = int64(calc.BaseToQuote(basisPrice, lotSize*50))
			mm.cfgV.Store(&BasicMarketMakingConfig{
				GapStrategy:    tt.strategy,
				BuyPlacements:  tt.cfgBuyPlacements,
				SellPlacements: tt.cfgSellPlacements,
			})
			mm.rebalance(100, &orderbook.OrderBook{})

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
				if lots, found := buyRateLots[expBuy.rate]; !found {
					t.Fatalf("buy rate %d not found", expBuy.rate)
				} else {
					if expBuy.lots != lots {
						t.Fatalf("wrong lots %d for buy at rate %d", lots, expBuy.rate)
					}
				}
			}
			sellRateLots := make(map[uint64]uint64, len(sells.Placements))
			for _, p := range sells.Placements {
				sellRateLots[p.Rate] = p.Qty / lotSize
			}
			for _, expSell := range tt.expSellPlacements {
				if lots, found := sellRateLots[expSell.rate]; !found {
					t.Fatalf("sell rate %d not found", expSell.rate)
				} else {
					if expSell.lots != lots {
						t.Fatalf("wrong lots %d for sell at rate %d", lots, expSell.rate)
					}
				}
			}
		})
	}
}

func TestBasicMMBotProblems(t *testing.T) {
	const basisPrice uint64 = 5e6
	const halfSpread uint64 = 2e5
	const rateStep uint64 = 1e3
	const atomToConv float64 = 1
	const baseID, quoteID = uint32(42), uint32(0)
	const lotSize = uint64(1e8)

	type test struct {
		name                     string
		bpOracleOutsideSafeRange bool
		bpErr                    error
		multiTradeBuyErr         error
		multiTradeSellErr        error
		noBalance                bool

		expBotProblems *BotProblems
	}

	updateBotProblems := func(f func(*BotProblems)) *BotProblems {
		bp := newBotProblems()
		f(bp)
		return bp
	}

	noIDErr1 := errors.New("no ID")
	noIDErr2 := errors.New("no ID")

	var swapFees, redeemFees, refundFees uint64 = 1e5, 2e5, 3e5

	tests := []*test{
		{
			name:           "no problems",
			expBotProblems: newBotProblems(),
		},
		{
			name:                     "mid gap outside oracle safe range",
			bpOracleOutsideSafeRange: true,
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.MidGapOutsideOracleSafeRange = true
			}),
		},
		{
			name:              "wallet sync errors",
			multiTradeBuyErr:  &core.WalletSyncError{AssetID: baseID},
			multiTradeSellErr: &core.WalletSyncError{AssetID: quoteID},
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.WalletNotSynced[baseID] = true
				bp.WalletNotSynced[quoteID] = true
			}),
		},
		{
			name:              "account suspended",
			multiTradeBuyErr:  core.ErrAccountSuspended,
			multiTradeSellErr: core.ErrAccountSuspended,
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.AccountSuspended = true
			}),
		},
		{
			name:              "buy no peers, sell qty too high",
			multiTradeBuyErr:  &core.WalletNoPeersError{AssetID: baseID},
			multiTradeSellErr: &msgjson.Error{Code: msgjson.OrderQuantityTooHigh},
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.NoWalletPeers[baseID] = true
				bp.UserLimitTooLow = true
			}),
		},
		{
			name:              "unidentified errors",
			multiTradeBuyErr:  noIDErr1,
			multiTradeSellErr: noIDErr2,
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.PlaceBuyOrdersErr = noIDErr1
				bp.PlaceSellOrdersErr = noIDErr2
			}),
		},
		{
			name:      "no balance",
			noBalance: true,
			expBotProblems: updateBotProblems(func(bp *BotProblems) {
				bp.DEXBalanceDeficiencies = map[uint32]uint64{
					baseID:  lotSize + swapFees,
					quoteID: calc.BaseToQuote(basisPrice-basisPrice/100, lotSize) + swapFees,
				}
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calculator := &tBasicMMCalculator{
				bp:                       basisPrice,
				bpOracleOutsideSafeRange: tt.bpOracleOutsideSafeRange,
				bpErr:                    tt.bpErr,
				hs:                       halfSpread,
			}

			adaptor := newTBotCoreAdaptor(newTCore())
			mm := &basicMarketMaker{
				unifiedExchangeAdaptor: mustParseAdaptorFromMarket(&core.Market{
					RateStep:   rateStep,
					AtomToConv: atomToConv,
					BaseID:     baseID,
					QuoteID:    quoteID,
					LotSize:    lotSize,
				}),
				calculator: calculator,
				core:       adaptor,
			}

			mm.buyFees = tFees(swapFees, redeemFees, refundFees, 0)
			mm.sellFees = tFees(swapFees, redeemFees, refundFees, 0)

			mm.unifiedExchangeAdaptor.clientCore.(*tCore).multiTradeBuyErr = tt.multiTradeBuyErr
			mm.unifiedExchangeAdaptor.clientCore.(*tCore).multiTradeSellErr = tt.multiTradeSellErr

			if !tt.noBalance {
				mm.baseDexBalances[baseID] = int64(lotSize * 50)
				mm.baseDexBalances[quoteID] = int64(calc.BaseToQuote(basisPrice, lotSize*50))
			}

			mm.cfgV.Store(&BasicMarketMakingConfig{
				GapStrategy: GapStrategyPercent,
				SellPlacements: []*OrderPlacement{
					{Lots: 1, GapFactor: 0.01},
				},
				BuyPlacements: []*OrderPlacement{
					{Lots: 1, GapFactor: 0.01},
				},
			})

			mm.unifiedExchangeAdaptor.fiatRates.Store(map[uint32]float64{baseID: 1, quoteID: 1})

			mm.rebalance(100, &orderbook.OrderBook{})

			problems := mm.problems()
			if !reflect.DeepEqual(tt.expBotProblems, problems) {
				t.Fatalf("expected bot problems %v, got %v", tt.expBotProblems, problems)
			}
		})
	}
}
