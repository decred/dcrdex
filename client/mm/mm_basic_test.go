//go:build !harness && !botlive

package mm

import (
	"math"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/calc"
	"github.com/davecgh/go-spew/spew"
)

type tBasicMMCalculator struct {
	bp uint64
	hs uint64
}

var _ basicMMCalculator = (*tBasicMMCalculator)(nil)

func (r *tBasicMMCalculator) basisPrice() uint64 {
	return r.bp
}
func (r *tBasicMMCalculator) halfSpread(basisPrice uint64) (uint64, error) {
	return r.hs, nil
}

func TestBasisPrice(t *testing.T) {
	mkt := &core.Market{
		RateStep:   1,
		BaseID:     42,
		QuoteID:    0,
		AtomToConv: 1,
	}

	tests := []*struct {
		name         string
		midGap       uint64
		oraclePrice  uint64
		oracleBias   float64
		oracleWeight float64
		conversions  map[uint32]float64
		fiatRate     uint64
		exp          uint64
	}{
		{
			name:   "just mid-gap is enough",
			midGap: 123e5,
			exp:    123e5,
		},
		{
			name:         "mid-gap + oracle weight",
			midGap:       1950,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          1975,
		},
		{
			name:         "adjusted mid-gap + oracle weight",
			midGap:       1000, // adjusted to 1940
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          1970,
		},
		{
			name:         "no mid-gap effectively sets oracle weight to 100%",
			midGap:       0,
			oraclePrice:  2000,
			oracleWeight: 0.5,
			exp:          2000,
		},
		{
			name:         "mid-gap + oracle weight + oracle bias",
			midGap:       1950,
			oraclePrice:  2000,
			oracleBias:   -0.01, // minus 20
			oracleWeight: 0.75,
			exp:          1972, // 0.25 * 1950 + 0.75 * (2000 - 20) = 1972
		},
		{
			name:         "no mid-gap and no oracle weight fails to produce result",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			exp:          0,
		},
		{
			name:         "no mid-gap and no oracle weight, but fiat rate is set",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			fiatRate:     1200,
			exp:          1200,
		},
	}

	for _, tt := range tests {
		oracle := &tOracle{
			marketPrice: mkt.MsgRateToConventional(tt.oraclePrice),
		}
		ob := &tOrderBook{
			midGap: tt.midGap,
		}
		cfg := &BasicMarketMakingConfig{
			OracleWeighting: &tt.oracleWeight,
			OracleBias:      tt.oracleBias,
		}

		tCore := newTCore()
		adaptor := newTBotCoreAdaptor(tCore)
		adaptor.fiatExchangeRate = tt.fiatRate

		calculator := &basicMMCalculatorImpl{
			book:   ob,
			oracle: oracle,
			mkt:    mkt,
			cfg:    cfg,
			log:    tLogger,
			core:   adaptor,
		}

		rate := calculator.basisPrice()
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
			core: coreAdaptor,
			mkt:  tt.mkt,
			log:  tLogger,
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
				{lots: 0, rate: 0}, // 5e6 - 6e6 < 0
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
				{lots: 0, rate: 0},
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
			adaptor := newTBotCoreAdaptor(newTCore())
			cfg := &BasicMarketMakingConfig{
				GapStrategy:    tt.strategy,
				BuyPlacements:  tt.cfgBuyPlacements,
				SellPlacements: tt.cfgSellPlacements,
			}
			mm := &basicMarketMaker{
				cfg:        cfg,
				calculator: calculator,
				core:       adaptor,
				log:        tLogger,
				mkt: &core.Market{
					RateStep:   rateStep,
					AtomToConv: atomToConv,
				},
			}

			mm.rebalance(100)

			if !reflect.DeepEqual(tt.expBuyPlacements, adaptor.lastMultiTradeBuys) {
				t.Fatal(spew.Sprintf("expected buy placements:\n%#+v\ngot:\n%#+v", tt.expBuyPlacements, adaptor.lastMultiTradeBuys))
			}
			if !reflect.DeepEqual(tt.expSellPlacements, adaptor.lastMultiTradeSells) {
				t.Fatal(spew.Sprintf("expected sell placements:\n%#+v\ngot:\n%#+v", tt.expSellPlacements, adaptor.lastMultiTradeSells))
			}
		})
	}
}
