package core

import (
	"errors"
	"math"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

type tOrder struct {
	lots uint64
	rate uint64
	sell bool
}

type tGapEngineInputs struct {
	basisPriceV   uint64
	halfSpreadV   uint64
	halfSpreadErr error
	sortedBuys    []*Order
	sortedSells   []*Order
	maxBuyV       *MaxOrderEstimate
	maxBuyErr     error
	maxSellV      *MaxOrderEstimate
	maxSellErr    error
	atomToConv    float64
	rateStepV     uint64
	placedOrders  []tOrder
	cancels       []order.OrderID
	lotSizeV      uint64
}

func (t *tGapEngineInputs) basisPrice(oracleBias, oracleWeighting, emptyMarketRate float64) uint64 {
	return t.basisPriceV
}
func (t *tGapEngineInputs) halfSpread(basisPrice uint64) (uint64, error) {
	return t.halfSpreadV, t.halfSpreadErr
}
func (t *tGapEngineInputs) sortedOrders() (buys, sells []*Order) {
	return t.sortedBuys, t.sortedSells
}
func (t *tGapEngineInputs) maxBuy(rate uint64) (*MaxOrderEstimate, error) {
	return t.maxBuyV, t.maxBuyErr
}
func (t *tGapEngineInputs) maxSell() (*MaxOrderEstimate, error) {
	return t.maxSellV, t.maxSellErr

}
func (t *tGapEngineInputs) conventionalRateToMsg(p float64) uint64 {
	return uint64(math.Round(p / t.atomToConv * calc.RateEncodingFactor))
}
func (t *tGapEngineInputs) rateStep() uint64 {
	return t.rateStepV
}
func (t *tGapEngineInputs) placeOrder(lots, rate uint64, sell bool) (order.OrderID, error) {
	t.placedOrders = append(t.placedOrders, tOrder{lots, rate, sell})
	return order.OrderID{}, nil
}
func (t *tGapEngineInputs) cancelOrder(oid order.OrderID) error {
	t.cancels = append(t.cancels, oid)
	return nil
}
func (t *tGapEngineInputs) lotSize() uint64 {
	return t.lotSizeV
}
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

func TestGapRebalance(t *testing.T) {
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

		inputs tGapEngineInputs
		cfg    *GapEngineCfg

		expBuyLots  int
		expSellLots int
		expCancels  int
	}

	newInputs := func(maxBuyLots, maxSellLots uint64, buyErr, sellErr error, existingBuys, existingSells []*Order) tGapEngineInputs {
		return tGapEngineInputs{
			basisPriceV:  midGap,
			halfSpreadV:  breakEven,
			maxBuyV:      maxBuy(maxBuyLots),
			maxBuyErr:    buyErr,
			maxSellV:     maxSell(maxSellLots),
			maxSellErr:   sellErr,
			sortedBuys:   existingBuys,
			sortedSells:  existingSells,
			rateStepV:    rateStep,
			atomToConv:   1,
			placedOrders: make([]tOrder, 0, 2),
			cancels:      make([]order.OrderID, 0, 2),
			lotSizeV:     lotSize,
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
			Qty:    lots * lotSize,
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
		engine, err := newGapEngine(&tt.inputs, tt.cfg, log)
		if err != nil {
			t.Fatalf("%s: error creating gap engine: %v", tt.name, err)
		}
		gapEngine := engine.(*gapEngine)

		gapEngine.notify(newEpochEngineNote(newEpoch))

		var buyLots, sellLots uint64
		for _, order := range tt.inputs.placedOrders {
			if order.sell {
				sellLots = order.lots
			} else {
				buyLots = order.lots
			}
		}

		if buyLots != uint64(tt.expBuyLots) {
			t.Fatalf("%s: expected %d buy lots but got %d", tt.name, tt.expBuyLots, buyLots)
		}
		if sellLots != uint64(tt.expSellLots) {
			t.Fatalf("%s: expected %d buy lots but got %d", tt.name, tt.expSellLots, sellLots)
		}
		if len(tt.inputs.cancels) != tt.expCancels {
			t.Fatalf("%s: expected %d cancels but got %d", tt.name, tt.expCancels, len(tt.inputs.cancels))
		}
	}
}
