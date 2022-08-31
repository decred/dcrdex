//go:build !harness && !botlive

package core

import (
	"context"
	"errors"
	"math"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

type tRebalancer struct {
	basis        uint64
	breakEven    uint64
	breakEvenErr error
	sortedBuys   []*sortedOrder
	sortedSells  []*sortedOrder
	cancels      int
	maxBuy       *MaxOrderEstimate
	maxBuyErr    error
	maxSell      *MaxOrderEstimate
	maxSellErr   error
}

var _ rebalancer = (*tRebalancer)(nil)

func (r *tRebalancer) basisPrice() uint64 {
	return r.basis
}

func (r *tRebalancer) halfSpread(basisPrice uint64) (uint64, error) {
	return r.breakEven, r.breakEvenErr
}

func (r *tRebalancer) sortedOrders() (buys, sells []*sortedOrder) {
	return r.sortedBuys, r.sortedSells
}

func (r *tRebalancer) cancelOrder(oid order.OrderID) error {
	r.cancels++
	return nil
}

func (r *tRebalancer) MaxBuy(host string, base, quote uint32, rate uint64) (*MaxOrderEstimate, error) {
	return r.maxBuy, r.maxBuyErr
}

func (r *tRebalancer) MaxSell(host string, base, quote uint32) (*MaxOrderEstimate, error) {
	return r.maxSell, r.maxSellErr
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

func TestRebalance(t *testing.T) {
	const rateStep uint64 = 7e14
	const midGap uint64 = 1234 * rateStep
	const lotSize uint64 = 50e8
	const breakEven uint64 = 8 * rateStep
	const newEpoch = 123_456_789
	const spreadMultiplier = 2
	const driftTolerance = 0.001

	mkt := &Market{
		RateStep:   rateStep,
		AtomToConv: 1,
	}

	inverseLot := calc.BaseToQuote(midGap, lotSize)

	maxBuy := func(lots uint64) *MaxOrderEstimate {
		return tMaxOrderEstimate(lots, inverseLot/100, lotSize/200)
	}
	maxSell := func(lots uint64) *MaxOrderEstimate {
		return tMaxOrderEstimate(lots, lotSize/100, inverseLot/200)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := dex.StdOutLogger("T", dex.LevelTrace)

	log.Info("TestRebalance: rateStep =", rateStep)
	log.Info("TestRebalance: midGap =", midGap)
	log.Info("TestRebalance: lotSize =", lotSize)
	log.Info("TestRebalance: breakEven =", breakEven)

	type test struct {
		name        string
		rebalancer  *tRebalancer
		program     *MakerProgram
		epoch       uint64
		expBuyLots  int
		expSellLots int
		expCancels  int
	}

	newBalancer := func(maxBuyLots, maxSellLots uint64, buyErr, sellErr error, existingBuys, existingSells []*sortedOrder) *tRebalancer {
		return &tRebalancer{
			basis:       midGap,
			breakEven:   breakEven,
			maxBuy:      maxBuy(maxBuyLots),
			maxBuyErr:   buyErr,
			maxSell:     maxSell(maxSellLots),
			maxSellErr:  sellErr,
			sortedBuys:  existingBuys,
			sortedSells: existingSells,
		}
	}
	newProgram := func(lots uint64) *MakerProgram {
		return &MakerProgram{
			Lots:           lots,
			DriftTolerance: driftTolerance,
			GapStrategy:    GapStrategyMultiplier,
			GapFactor:      spreadMultiplier,
		}
	}

	newSortedOrder := func(lots, rate uint64, sell bool, freeCancel bool) *sortedOrder {
		var epoch uint64 = newEpoch
		if freeCancel {
			epoch = newEpoch - 2
		}
		return &sortedOrder{
			Order: &Order{
				Epoch:  epoch,
				Sell:   sell,
				Status: order.OrderStatusBooked,
			},
			rate: rate,
			lots: lots,
		}
	}

	buyPrice := midGap - (breakEven * (1 + spreadMultiplier))
	sellPrice := midGap + (breakEven * (1 + spreadMultiplier))

	sellTolerance := uint64(math.Round(float64(sellPrice) * driftTolerance))

	tests := []*test{
		{
			name:        "1 lot per side",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, nil),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 1,
		},
		{
			name:        "1 sell, buy already exists",
			rebalancer:  newBalancer(1, 1, nil, nil, []*sortedOrder{newSortedOrder(1, buyPrice, false, true)}, nil),
			program:     newProgram(1),
			expBuyLots:  0,
			expSellLots: 1,
		},
		{
			name:        "1 buy, sell already exists",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, []*sortedOrder{newSortedOrder(1, sellPrice, true, true)}),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 0,
		},
		{
			name:        "1 buy, sell already exists, just within tolerance",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, []*sortedOrder{newSortedOrder(1, sellPrice+sellTolerance, true, true)}),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 0,
		},
		{
			name:        "1 lot each, sell just out of tolerance, but doesn't interfere",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, []*sortedOrder{newSortedOrder(1, sellPrice+sellTolerance+1, true, true)}),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "no buy, because an existing order (cancellation) interferes",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, []*sortedOrder{newSortedOrder(1, buyPrice, true, true)}),
			program:     newProgram(1),
			expBuyLots:  0, // cuz interference
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "no sell, because an existing order (cancellation) interferes",
			rebalancer:  newBalancer(1, 1, nil, nil, []*sortedOrder{newSortedOrder(1, sellPrice, true, true)}, nil),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 0,
			expCancels:  1,
		},
		{
			name:        "1 lot each, existing order barely escapes interference",
			rebalancer:  newBalancer(1, 1, nil, nil, nil, []*sortedOrder{newSortedOrder(1, buyPrice+1, true, true)}),
			program:     newProgram(1),
			expBuyLots:  1,
			expSellLots: 1,
			expCancels:  1,
		},
		{
			name:        "1 sell and 3 buy lots on an unbalanced 2 lot program",
			rebalancer:  newBalancer(10, 1, nil, nil, nil, nil),
			program:     newProgram(2),
			expBuyLots:  3,
			expSellLots: 1,
		},
		{
			name:        "1 sell and 3 buy lots on an unbalanced 3 lot program with balance limitations",
			rebalancer:  newBalancer(3, 1, nil, nil, nil, nil),
			program:     newProgram(3),
			expBuyLots:  3,
			expSellLots: 1,
		},
		{
			name:        "2 sell and 1 buy lots on an unbalanced 2 lot program with balance limitations",
			rebalancer:  newBalancer(1, 2, nil, nil, nil, nil),
			program:     newProgram(2),
			expBuyLots:  1,
			expSellLots: 2,
		},
		{
			name:        "4 buy lots on an unbalanced 2 lot program with balance but MaxSell error",
			rebalancer:  newBalancer(10, 1, nil, errors.New("test error"), nil, nil),
			program:     newProgram(2),
			expBuyLots:  4,
			expSellLots: 0,
		},
		{
			name:        "1 lot sell on an unbalanced 2 lot program with limited balance but MaxBuy error",
			rebalancer:  newBalancer(10, 1, errors.New("test error"), nil, nil, nil),
			program:     newProgram(2),
			expBuyLots:  0,
			expSellLots: 1,
		},
	}

	for _, tt := range tests {
		epoch := tt.epoch
		if epoch == 0 {
			epoch = newEpoch
		}
		newBuyLots, newSellLots, buyRate, sellRate := rebalance(ctx, tt.rebalancer, mkt, tt.program, log, epoch)
		if newBuyLots != tt.expBuyLots {
			t.Fatalf("%s: buy lots mismatch. expected %d, got %d", tt.name, tt.expBuyLots, newBuyLots)
		}
		if newSellLots != tt.expSellLots {
			t.Fatalf("%s: sell lots mismatch. expected %d, got %d", tt.name, tt.expSellLots, newSellLots)
		}
		if sellPrice != sellRate {
			t.Fatalf("%s: sell price mismatch. expected %d, got %d", tt.name, sellPrice, sellRate)
		}
		if buyPrice != buyRate {
			t.Fatalf("%s: buy price mismatch. expected %d, got %d", tt.name, buyPrice, buyRate)
		}
		if tt.rebalancer.cancels != tt.expCancels {
			t.Fatalf("%s: cancel count mismatch. expected %d, got %d", tt.name, tt.expCancels, tt.rebalancer.cancels)
		}
	}
}

type tBasisPricer struct {
	price       *stampedPrice
	conversions map[uint32]float64
}

func (bp *tBasisPricer) cachedOraclePrice(mktName string) *stampedPrice {
	return bp.price
}

func (bp *tBasisPricer) fiatConversions() map[uint32]float64 {
	return bp.conversions
}

func TestBasisPrice(t *testing.T) {
	mkt := &Market{
		RateStep:   1,
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
		AtomToConv: 1,
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	tests := []*struct {
		name         string
		midGap       uint64
		oraclePrice  uint64
		oracleBias   float64
		oracleWeight float64
		conversions  map[uint32]float64
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
			name:         "no mid-gap and no oracle weight falls back to fiat ratio",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			conversions: map[uint32]float64{
				btcBipID: 80,
				dcrBipID: 2,
			},
			exp: 2500000, // (2 / 80) * 1e8
		},
		{
			name:         "no mid-gap and no oracle weight and a missing fiat conversion fails to produce result",
			midGap:       0,
			oraclePrice:  0,
			oracleWeight: 0.75,
			conversions: map[uint32]float64{
				btcBipID: 80,
			},
			exp: 0,
		},
	}

	for _, tt := range tests {
		bp := &tBasisPricer{
			price:       &stampedPrice{price: mkt.MsgRateToConventional(tt.oraclePrice)},
			conversions: tt.conversions,
		}
		rate := basisPrice("", mkt, tt.oracleBias, tt.oracleWeight, tt.midGap, bp, log)
		if rate != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, rate, tt.exp)
		}
	}
}

type tBotFeeEstimator struct {
	buySwap, buyRedeem, sellSwap, sellRedeem uint64
	buyErr, sellErr                          error
}

func (fe *tBotFeeEstimator) feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error) {
	if form.Sell {
		return fe.sellSwap, fe.sellRedeem, fe.sellErr
	}
	return fe.buySwap, fe.buyRedeem, fe.buyErr
}

func TestBreakEvenHalfSpread(t *testing.T) {
	mkt := &Market{
		LotSize:    20e8, // 20 DCR
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
		AtomToConv: 1,
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	tests := []*struct {
		name           string
		basisPrice     uint64
		sellSwapFees   uint64
		sellRedeemFees uint64
		buySwapFees    uint64
		buyRedeemFees  uint64
		exp            uint64
		buyErr         error
		sellErr        error
		expErr         bool
	}{
		{
			name:   "basis price = 0 not allowed",
			expErr: true,
		},
		{
			name:   "estimator buy error propagates",
			buyErr: errors.New("t"),
			expErr: true,
		},
		{
			name:    "estimator sell error propagates",
			sellErr: errors.New("t"),
			expErr:  true,
		},
		{
			name:           "simple",
			basisPrice:     4e7, // 0.4 BTC/DCR, quote lot = 8 BTC
			buySwapFees:    200, // BTC
			buyRedeemFees:  100, // DCR
			sellSwapFees:   300, // DCR
			sellRedeemFees: 50,  // BTC
			// total btc (quote) fees, Q = 250
			// total dcr (base) fees, B = 400
			// g = (pB + Q) / (B + 2L)
			// g = (0.4*400 + 250) / (400 + 40e8) = 1.02e-7 // atomic units
			// g = 10 // msg-rate units
			exp: 10,
		},
	}

	for _, tt := range tests {
		fe := &tBotFeeEstimator{
			sellSwap:   tt.sellSwapFees,
			sellRedeem: tt.sellRedeemFees,
			buySwap:    tt.buySwapFees,
			buyRedeem:  tt.buyRedeemFees,
			buyErr:     tt.buyErr,
			sellErr:    tt.sellErr,
		}

		halfSpread, err := breakEvenHalfSpread("", mkt, tt.basisPrice, fe, log)
		if (err != nil) != tt.expErr {
			t.Fatalf("expErr = %t, err = %v", tt.expErr, err)
		}
		if halfSpread != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, halfSpread, tt.exp)
		}
	}
}
