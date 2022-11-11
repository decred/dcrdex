package botengine

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

type tGapEngineInputs struct {
	basisPrice    uint64
	halfSpread    uint64
	halfSpreadErr error
	sortedBuys    []*Order
	sortedSells   []*Order
	maxBuy        *MaxOrderEstimate
	maxBuyErr     error
	maxSell       *MaxOrderEstimate
	maxSellErr    error
	atomToConv    float64
	rateStep      uint64
}

func (t *tGapEngineInputs) BasisPrice(oracleBias, oracleWeighting, emptyMarketRate float64) uint64 {
	return t.basisPrice
}
func (t *tGapEngineInputs) HalfSpread(basisPrice uint64) (uint64, error) {
	return t.halfSpread, t.halfSpreadErr
}
func (t *tGapEngineInputs) SortedOrders() (buys, sells []*Order) {
	return t.sortedBuys, t.sortedSells
}
func (t *tGapEngineInputs) MaxBuy(rate uint64) (*MaxOrderEstimate, error) {
	return t.maxBuy, t.maxBuyErr
}
func (t *tGapEngineInputs) MaxSell() (*MaxOrderEstimate, error) {
	return t.maxSell, t.maxSellErr

}
func (t *tGapEngineInputs) ConventionalRateToMsg(p float64) uint64 {
	return uint64(math.Round(p / t.atomToConv * calc.RateEncodingFactor))
}
func (t *tGapEngineInputs) RateStep() uint64 {
	return t.rateStep
}
func (t *tGapEngineInputs) StartOracleSync(context.Context) {}

func tMaxOrderEstimate(lots uint64, swapFees, redeemFees uint64) *MaxOrderEstimate {
	return &MaxOrderEstimate{
		Swap: &asset.SwapEstimate{
			RealisticWorstCase: swapFees,
			Lots:               lots,
		},
		Redeem: &asset.RedeemEstimate{
			RealisticWorstCase: redeemFees,
		},
	}
}

func TestRebalance(t *testing.T) {
	const rateStep uint64 = 7e14
	const midGap uint64 = 1234 * rateStep
	const lotSize uint64 = 50e8
	const breakEven uint64 = 8 * rateStep
	const newEpoch = 123_456_789
	const spreadMultiplier = 3
	const driftTolerance = 0.001

	inverseLot := calc.BaseToQuote(midGap, lotSize)

	maxBuy := func(lots uint64) *MaxOrderEstimate {
		return tMaxOrderEstimate(lots, inverseLot/100, lotSize/200)
	}
	maxSell := func(lots uint64) *MaxOrderEstimate {
		return tMaxOrderEstimate(lots, lotSize/100, inverseLot/200)
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	type test struct {
		name string

		inputs GapEngineInputs
		cfg    *GapEngineCfg

		expBuyLots  int
		expSellLots int
		expCancels  int
	}

	newInputs := func(maxBuyLots, maxSellLots uint64, buyErr, sellErr error, existingBuys, existingSells []*Order) *tGapEngineInputs {
		return &tGapEngineInputs{
			basisPrice:  midGap,
			halfSpread:  breakEven,
			maxBuy:      maxBuy(maxBuyLots),
			maxBuyErr:   buyErr,
			maxSell:     maxSell(maxSellLots),
			maxSellErr:  sellErr,
			sortedBuys:  existingBuys,
			sortedSells: existingSells,
			rateStep:    rateStep,
			atomToConv:  1,
		}
	}

	newCfg := func(lots uint64) *GapEngineCfg {
		return &GapEngineCfg{
			Lots:           lots,
			DriftTolerance: driftTolerance,
			GapStrategy:    GapStrategyMultiplier,
			GapFactor:      spreadMultiplier,
		}
	}

	newOrder := func(lots, rate uint64, sell bool, freeCancel bool) *Order {
		var epoch uint64 = newEpoch
		if freeCancel {
			epoch = newEpoch - 2
		}
		return &Order{
			Sell:   sell,
			Status: order.OrderStatusBooked,
			Epoch:  epoch,
			Rate:   rate,
			Lots:   lots,
		}
	}

	buyPrice := midGap - (breakEven * spreadMultiplier)
	sellPrice := midGap + (breakEven * spreadMultiplier)

	sellTolerance := uint64(math.Round(float64(sellPrice) * driftTolerance))

	tests := []*test{
		{
			name:        "1 lot per side",
			inputs:      newInputs(1, 1, nil, nil, nil, nil),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 1,
		},
		{
			name:        "1 sell, buy already exists",
			inputs:      newInputs(1, 1, nil, nil, []*Order{newOrder(1, buyPrice, false, true)}, nil),
			cfg:         newCfg(1),
			expBuyLots:  0,
			expSellLots: 1,
		},
		{
			name:        "1 buy, sell already exists",
			inputs:      newInputs(1, 1, nil, nil, nil, []*Order{newOrder(1, sellPrice, true, true)}),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 0,
		},
		{
			name:        "1 buy, sell already exists, just within tolerance",
			inputs:      newInputs(1, 1, nil, nil, nil, []*Order{newOrder(1, sellPrice+sellTolerance, true, true)}),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 0,
		},
		{
			name:        "1 lot each, sell just out of tolerance, but doesn't interfere",
			inputs:      newInputs(1, 1, nil, nil, nil, []*Order{newOrder(1, sellPrice+sellTolerance+1, true, true)}),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "no buy, because an existing order (cancellation) interferes",
			inputs:      newInputs(1, 1, nil, nil, nil, []*Order{newOrder(1, buyPrice, true, true)}),
			cfg:         newCfg(1),
			expBuyLots:  0, // cuz interference
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "no sell, because an existing order (cancellation) interferes",
			inputs:      newInputs(1, 1, nil, nil, []*Order{newOrder(1, sellPrice, true, true)}, nil),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 0,
			expCancels:  1,
		},
		{
			name:        "1 lot each, existing order barely escapes interference",
			inputs:      newInputs(1, 1, nil, nil, nil, []*Order{newOrder(1, buyPrice+1, true, true)}),
			cfg:         newCfg(1),
			expBuyLots:  1,
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "1 sell and 3 buy lots on an unbalanced 2 lot program",
			inputs:      newInputs(10, 1, nil, nil, nil, nil),
			cfg:         newCfg(2),
			expBuyLots:  3,
			expSellLots: 1,
		},
		{
			name:        "1 sell and 3 buy lots on an unbalanced 3 lot program with balance limitations",
			inputs:      newInputs(3, 1, nil, nil, nil, nil),
			cfg:         newCfg(3),
			expBuyLots:  3,
			expSellLots: 1,
		},
		{
			name:        "2 sell and 1 buy lots on an unbalanced 2 lot program with balance limitations",
			inputs:      newInputs(1, 2, nil, nil, nil, nil),
			cfg:         newCfg(2),
			expBuyLots:  1,
			expSellLots: 2,
		},
		{
			name:        "4 buy lots on an unbalanced 2 lot program with balance but MaxSell error",
			inputs:      newInputs(10, 1, nil, errors.New("test error"), nil, nil),
			cfg:         newCfg(2),
			expBuyLots:  4,
			expSellLots: 0,
		},
		{
			name:        "1 lot sell on an unbalanced 2 lot program with limited balance but MaxBuy error",
			inputs:      newInputs(10, 1, errors.New("test error"), nil, nil, nil),
			cfg:         newCfg(2),
			expBuyLots:  0,
			expSellLots: 1,
		},
	}

	for _, tt := range tests {
		engine, err := NewGapEngine(tt.inputs, tt.cfg, log)
		if err != nil {
			t.Fatalf("%s: error creating gap engine: %v", tt.name, err)
		}
		gapEngine := engine.(*GapEngine)

		gapEngine.rebalance(newEpoch)

		tradeFeed := gapEngine.TradeFeed()

		expNotes := tt.expCancels
		if tt.expBuyLots > 0 {
			expNotes++
		}
		if tt.expSellLots > 0 {
			expNotes++
		}

		var numCancels int

		handleTradeFeedNote := func(note interface{}) {
			switch n := note.(type) {
			case *CancelNote:
				numCancels++
			case *OrderNote:
				if n.Sell {
					if tt.expSellLots != int(n.Lots) {
						t.Fatalf("%s: expected sell lots %d != actual %d", tt.name, tt.expSellLots, n.Lots)
					}
					if sellPrice != n.Rate {
						t.Fatalf("%s: expected sell rate %d != actual %d", tt.name, sellPrice, n.Rate)
					}
				} else {
					if tt.expBuyLots != int(n.Lots) {
						t.Fatalf("%s: expected sell lots %d != actual %d", tt.name, tt.expBuyLots, n.Lots)
					}
					if buyPrice != n.Rate {
						t.Fatalf("%s: expected sell rate %d != actual %d", tt.name, buyPrice, n.Rate)
					}
				}
			default:
				t.Fatalf("unexpected note type: %t", n)
			}
		}

		for i := 0; i < expNotes; i++ {
			select {
			case n := <-tradeFeed:
				handleTradeFeedNote(n)
			case <-time.After(20 * time.Second):
				t.Fatalf("%s: not enough notifications recieved on trade feed", tt.name)
			}
		}

		if numCancels != tt.expCancels {
			t.Fatalf("%s: expected cancels %d != actual %d", tt.name, tt.expCancels, numCancels)
		}
	}
}
