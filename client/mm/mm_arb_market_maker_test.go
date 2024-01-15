// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/hex"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

type tArbMMRebalancer struct {
	buyVWAP      map[uint64]*vwapResult
	sellVWAP     map[uint64]*vwapResult
	groupedBuys  map[int][]*groupedOrder
	groupedSells map[int][]*groupedOrder
}

var _ arbMMRebalancer = (*tArbMMRebalancer)(nil)

func (r *tArbMMRebalancer) vwap(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if sell {
		if res, found := r.sellVWAP[qty]; found {
			return res.avg, res.extrema, true, nil
		}
		return 0, 0, false, nil
	}
	if res, found := r.buyVWAP[qty]; found {
		return res.avg, res.extrema, true, nil
	}
	return 0, 0, false, nil
}

func (r *tArbMMRebalancer) groupedOrders() (buys, sells map[int][]*groupedOrder) {
	return r.groupedBuys, r.groupedSells
}

func TestArbMarketMakerRebalance(t *testing.T) {
	const rateStep uint64 = 1e3
	const lotSize uint64 = 50e8
	const newEpoch = 123_456_789
	const driftTolerance = 0.001
	const profit = 0.01

	buyFees := &orderFees{
		swap:       1e4,
		redemption: 2e4,
		funding:    3e4,
	}
	sellFees := &orderFees{
		swap:       2e4,
		redemption: 1e4,
		funding:    4e4,
	}

	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	mkt := &core.Market{
		RateStep:    rateStep,
		AtomToConv:  1,
		LotSize:     lotSize,
		BaseID:      42,
		QuoteID:     0,
		BaseSymbol:  "dcr",
		QuoteSymbol: "btc",
	}

	cfg1 := &ArbMarketMakerConfig{
		DriftTolerance: driftTolerance,
		Profit:         profit,
		BuyPlacements: []*ArbMarketMakingPlacement{{
			Lots:       1,
			Multiplier: 1.5,
		}},
		SellPlacements: []*ArbMarketMakingPlacement{{
			Lots:       1,
			Multiplier: 1.5,
		}},
	}

	cfg2 := &ArbMarketMakerConfig{
		DriftTolerance: driftTolerance,
		Profit:         profit,
		BuyPlacements: []*ArbMarketMakingPlacement{
			{
				Lots:       1,
				Multiplier: 2,
			},
			{
				Lots:       1,
				Multiplier: 1.5,
			},
		},
		SellPlacements: []*ArbMarketMakingPlacement{
			{
				Lots:       1,
				Multiplier: 2,
			},
			{
				Lots:       1,
				Multiplier: 1.5,
			},
		},
	}

	type test struct {
		name string

		rebalancer  *tArbMMRebalancer
		cfg         *ArbMarketMakerConfig
		dexBalances map[uint32]uint64
		cexBalances map[uint32]*botBalance
		reserves    autoRebalanceReserves

		expectedCancels []dex.Bytes
		expectedBuys    []*rateLots
		expectedSells   []*rateLots
	}

	multiplyRate := func(u uint64, m float64) uint64 {
		return steppedRate(uint64(float64(u)*m), mkt.RateStep)
	}
	divideRate := func(u uint64, d float64) uint64 {
		return steppedRate(uint64(float64(u)/d), mkt.RateStep)
	}
	lotSizeMultiplier := func(m float64) uint64 {
		return uint64(float64(mkt.LotSize) * m)
	}

	tests := []*test{
		//  "no existing orders"
		{
			name: "no existing orders",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2e6,
						extrema: 1.9e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
				},
			},
			cfg: cfg1,
			dexBalances: map[uint32]uint64{
				42: lotSize * 3,
				0:  calc.BaseToQuote(1e6, 3*lotSize),
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedBuys: []*rateLots{{
				rate: 1.881e6,
				lots: 1,
			}},
			expectedSells: []*rateLots{{
				rate: 2.222e6,
				lots: 1,
			}},
		},
		// "existing orders within drift tolerance"
		{
			name: "existing orders within drift tolerance",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2e6,
						extrema: 1.9e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
				},
				groupedBuys: map[int][]*groupedOrder{
					0: {{
						rate: 1.882e6,
						lots: 1,
					}},
				},
				groupedSells: map[int][]*groupedOrder{
					0: {{
						rate: 2.223e6,
						lots: 1,
					}},
				},
			},
			cfg: cfg1,
			dexBalances: map[uint32]uint64{
				42: lotSize * 3,
				0:  calc.BaseToQuote(1e6, 3*lotSize),
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
		},
		// "existing orders outside drift tolerance"
		{
			name: "existing orders outside drift tolerance",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2e6,
						extrema: 1.9e6,
					},
				},
				groupedBuys: map[int][]*groupedOrder{
					0: {{
						id:   orderIDs[0],
						rate: 1.883e6,
						lots: 1,
					}},
				},
				groupedSells: map[int][]*groupedOrder{
					0: {{
						id:   orderIDs[1],
						rate: 2.225e6,
						lots: 1,
					}},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
				},
			},
			cfg: cfg1,
			dexBalances: map[uint32]uint64{
				42: lotSize * 3,
				0:  calc.BaseToQuote(1e6, 3*lotSize),
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedCancels: []dex.Bytes{
				orderIDs[0][:],
				orderIDs[1][:],
			},
		},
		// "don't cancel before free cancel"
		{
			name: "don't cancel before free cancel",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2e6,
						extrema: 1.9e6,
					},
				},
				groupedBuys: map[int][]*groupedOrder{
					0: {{
						id:    orderIDs[0],
						rate:  1.883e6,
						lots:  1,
						epoch: newEpoch - 1,
					}},
				},
				groupedSells: map[int][]*groupedOrder{
					0: {{
						id:    orderIDs[1],
						rate:  2.225e6,
						lots:  1,
						epoch: newEpoch - 2,
					}},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(1.5): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
				},
			},
			cfg: cfg1,
			dexBalances: map[uint32]uint64{
				42: lotSize * 3,
				0:  calc.BaseToQuote(1e6, 3*lotSize),
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedCancels: []dex.Bytes{
				orderIDs[1][:],
			},
		},
		// "no existing orders, two orders each, dex balance edge, enough"
		{
			name: "no existing orders, two orders each, dex balance edge, enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 2*(lotSize+sellFees.swap) + sellFees.funding,
				0:  calc.BaseToQuote(divideRate(1.9e6, 1+profit), lotSize) + calc.BaseToQuote(divideRate(1.7e6, 1+profit), lotSize) + 2*buyFees.swap + buyFees.funding,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "no existing orders, two orders each, dex balance edge, not enough"
		{
			name: "no existing orders, two orders each, dex balance edge, not enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 2*(lotSize+sellFees.swap) + sellFees.funding - 1,
				0:  calc.BaseToQuote(divideRate(1.9e6, 1+profit), lotSize) + calc.BaseToQuote(divideRate(1.7e6, 1+profit), lotSize) + 2*buyFees.swap + buyFees.funding - 1,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
			},
		},
		// "no existing orders, two orders each, cex balance edge, enough"
		{
			name: "no existing orders, two orders each, cex balance edge, enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize)},
				42: {Available: 2 * mkt.LotSize},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "no existing orders, two orders each, cex balance edge, not enough"
		{
			name: "no existing orders, two orders each, cex balance edge, not enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize) - 1},
				42: {Available: 2*mkt.LotSize - 1},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
			},
		},
		// "one existing order, enough cex balance for second"
		{
			name: "one existing order, enough cex balance for second",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
				groupedBuys: map[int][]*groupedOrder{
					0: {{
						rate: divideRate(1.9e6, 1+profit),
						lots: 1,
					}},
				},
				groupedSells: map[int][]*groupedOrder{
					0: {{
						rate: multiplyRate(2.2e6, 1+profit),
						lots: 1,
					}},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize)},
				42: {Available: 2 * mkt.LotSize},
			},
			expectedBuys: []*rateLots{
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "one existing order, not enough cex balance for second"
		{
			name: "one existing order, not enough cex balance for second",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
				groupedBuys: map[int][]*groupedOrder{
					0: {{
						rate: divideRate(1.9e6, 1+profit),
						lots: 1,
					}},
				},
				groupedSells: map[int][]*groupedOrder{
					0: {{
						rate: multiplyRate(2.2e6, 1+profit),
						lots: 1,
					}},
				},
			},
			cfg: cfg2,
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize) - 1},
				42: {Available: 2*mkt.LotSize - 1},
			},
		},
		// "no existing orders, two orders each, dex balance edge with reserves, enough"
		{
			name: "no existing orders, two orders each, dex balance edge with reserves, enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			reserves: autoRebalanceReserves{
				baseDexReserves: 2 * lotSize,
			},
			dexBalances: map[uint32]uint64{
				42: 2*(lotSize+sellFees.swap) + sellFees.funding + 2*lotSize,
				0:  calc.BaseToQuote(divideRate(1.9e6, 1+profit), lotSize) + calc.BaseToQuote(divideRate(1.7e6, 1+profit), lotSize) + 2*buyFees.swap + buyFees.funding,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "no existing orders, two orders each, dex balance edge with reserves, not enough"
		{
			name: "no existing orders, two orders each, dex balance edge with reserves, not enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			reserves: autoRebalanceReserves{
				baseDexReserves: 2 * lotSize,
			},
			dexBalances: map[uint32]uint64{
				42: 2*(lotSize+sellFees.swap) + sellFees.funding + 2*lotSize - 1,
				0:  calc.BaseToQuote(divideRate(1.9e6, 1+profit), lotSize) + calc.BaseToQuote(divideRate(1.7e6, 1+profit), lotSize) + 2*buyFees.swap + buyFees.funding,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: 1e19},
				42: {Available: 1e19},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
			},
		},
		// "no existing orders, two orders each, cex balance edge with reserves, enough"
		{
			name: "no existing orders, two orders each, cex balance edge with reserves, enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			reserves: autoRebalanceReserves{
				quoteCexReserves: lotSize,
			},
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize) + lotSize},
				42: {Available: 2 * mkt.LotSize},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
				{
					rate:           divideRate(1.7e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "no existing orders, two orders each, cex balance edge with reserves, enough"
		{
			name: "no existing orders, two orders each, cex balance edge with reserves, not enough",
			rebalancer: &tArbMMRebalancer{
				buyVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2e6,
						extrema: 1.9e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     1.8e6,
						extrema: 1.7e6,
					},
				},
				sellVWAP: map[uint64]*vwapResult{
					lotSizeMultiplier(2): {
						avg:     2.1e6,
						extrema: 2.2e6,
					},
					lotSizeMultiplier(3.5): {
						avg:     2.3e6,
						extrema: 2.4e6,
					},
				},
			},
			cfg: cfg2,
			reserves: autoRebalanceReserves{
				baseCexReserves: lotSize,
			},
			dexBalances: map[uint32]uint64{
				42: 1e19,
				0:  1e19,
			},
			cexBalances: map[uint32]*botBalance{
				0:  {Available: calc.BaseToQuote(2.2e6, mkt.LotSize) + calc.BaseToQuote(2.4e6, mkt.LotSize) + lotSize - 1},
				42: {Available: 2 * mkt.LotSize},
			},
			expectedBuys: []*rateLots{
				{
					rate: divideRate(1.9e6, 1+profit),
					lots: 1,
				},
			},
			expectedSells: []*rateLots{
				{
					rate: multiplyRate(2.2e6, 1+profit),
					lots: 1,
				},
				{
					rate:           multiplyRate(2.4e6, 1+profit),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
	}

	for _, test := range tests {
		tCore := newTCore()
		tCore.setAssetBalances(test.dexBalances)
		cex := newTBotCEXAdaptor()
		cex.balances = test.cexBalances

		cancels, buys, sells := arbMarketMakerRebalance(newEpoch, test.rebalancer,
			newTBotCoreAdaptor(tCore), cex, test.cfg, mkt, buyFees, sellFees, &test.reserves, tLogger)

		if len(cancels) != len(test.expectedCancels) {
			t.Fatalf("%s: expected %d cancels, got %d", test.name, len(test.expectedCancels), len(cancels))
		}
		for i := range cancels {
			if !cancels[i].Equal(test.expectedCancels[i]) {
				t.Fatalf("%s: cancel %d expected %x, got %x", test.name, i, test.expectedCancels[i], cancels[i])
			}
		}

		if len(buys) != len(test.expectedBuys) {
			t.Fatalf("%s: expected %d buys, got %d", test.name, len(test.expectedBuys), len(buys))
		}
		for i := range buys {
			if buys[i].rate != test.expectedBuys[i].rate {
				t.Fatalf("%s: buy %d expected rate %d, got %d", test.name, i, test.expectedBuys[i].rate, buys[i].rate)
			}
			if buys[i].lots != test.expectedBuys[i].lots {
				t.Fatalf("%s: buy %d expected lots %d, got %d", test.name, i, test.expectedBuys[i].lots, buys[i].lots)
			}
			if buys[i].placementIndex != test.expectedBuys[i].placementIndex {
				t.Fatalf("%s: buy %d expected placement index %d, got %d", test.name, i, test.expectedBuys[i].placementIndex, buys[i].placementIndex)
			}
		}

		if len(sells) != len(test.expectedSells) {
			t.Fatalf("%s: expected %d sells, got %d", test.name, len(test.expectedSells), len(sells))
		}
		for i := range sells {
			if sells[i].rate != test.expectedSells[i].rate {
				t.Fatalf("%s: sell %d expected rate %d, got %d", test.name, i, test.expectedSells[i].rate, sells[i].rate)
			}
			if sells[i].lots != test.expectedSells[i].lots {
				t.Fatalf("%s: sell %d expected lots %d, got %d", test.name, i, test.expectedSells[i].lots, sells[i].lots)
			}
			if sells[i].placementIndex != test.expectedSells[i].placementIndex {
				t.Fatalf("%s: sell %d expected placement index %d, got %d", test.name, i, test.expectedSells[i].placementIndex, sells[i].placementIndex)
			}
		}
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

	multiplyRate := func(u uint64, m float64) uint64 {
		return steppedRate(uint64(float64(u)*m), mkt.RateStep)
	}
	divideRate := func(u uint64, d float64) uint64 {
		return steppedRate(uint64(float64(u)/d), mkt.RateStep)
	}

	type test struct {
		name              string
		orders            []*core.Order
		notes             []core.Notification
		expectedCEXTrades []*libxc.Trade
	}

	tests := []*test{
		{
			name: "one buy and one sell match notifications",
			orders: []*core.Order{
				{
					ID:   orderIDs[0][:],
					Sell: true,
					Qty:  lotSize,
					Rate: 8e5,
				},
				{
					ID:   orderIDs[1][:],
					Sell: false,
					Qty:  lotSize,
					Rate: 6e5,
				},
			},
			notes: []core.Notification{
				&core.MatchNote{
					OrderID: orderIDs[0][:],
					Match: &core.Match{
						MatchID: matchIDs[0][:],
						Qty:     lotSize,
						Rate:    8e5,
					},
				},
				&core.MatchNote{
					OrderID: orderIDs[1][:],
					Match: &core.Match{
						MatchID: matchIDs[1][:],
						Qty:     lotSize,
						Rate:    6e5,
					},
				},
				&core.OrderNote{
					Order: &core.Order{
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
				},
				&core.OrderNote{
					Order: &core.Order{
						ID:   orderIDs[1][:],
						Sell: false,
						Qty:  lotSize,
						Rate: 8e5,
						Matches: []*core.Match{
							{
								MatchID: matchIDs[1][:],
								Qty:     lotSize,
								Rate:    6e5,
							},
						},
					},
				},
			},
			expectedCEXTrades: []*libxc.Trade{
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    divideRate(8e5, 1+profit),
					Sell:    false,
				},
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    multiplyRate(6e5, 1+profit),
					Sell:    true,
				},
				nil,
				nil,
			},
		},
		{
			name: "place cex trades due to order note",
			orders: []*core.Order{
				{
					ID:   orderIDs[0][:],
					Sell: true,
					Qty:  lotSize,
					Rate: 8e5,
				},
				{
					ID:   orderIDs[1][:],
					Sell: false,
					Qty:  lotSize,
					Rate: 6e5,
				},
			},
			notes: []core.Notification{
				&core.OrderNote{
					Order: &core.Order{
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
				},
				&core.OrderNote{
					Order: &core.Order{
						ID:   orderIDs[1][:],
						Sell: false,
						Qty:  lotSize,
						Rate: 8e5,
						Matches: []*core.Match{
							{
								MatchID: matchIDs[1][:],
								Qty:     lotSize,
								Rate:    6e5,
							},
						},
					},
				},
				&core.MatchNote{
					OrderID: orderIDs[0][:],
					Match: &core.Match{
						MatchID: matchIDs[0][:],
						Qty:     lotSize,
						Rate:    8e5,
					},
				},
				&core.MatchNote{
					OrderID: orderIDs[1][:],
					Match: &core.Match{
						MatchID: matchIDs[1][:],
						Qty:     lotSize,
						Rate:    6e5,
					},
				},
			},
			expectedCEXTrades: []*libxc.Trade{
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    divideRate(8e5, 1+profit),
					Sell:    false,
				},
				{
					BaseID:  42,
					QuoteID: 0,
					Qty:     lotSize,
					Rate:    multiplyRate(6e5, 1+profit),
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
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ords := make(map[order.OrderID]*core.Order)

		for _, o := range test.orders {
			var oid order.OrderID
			copy(oid[:], o.ID)
			ords[oid] = o
		}

		arbMM := &arbMarketMaker{
			cex:            cex,
			core:           newTBotCoreAdaptor(tCore),
			ctx:            ctx,
			ords:           ords,
			baseID:         42,
			quoteID:        0,
			oidToPlacement: make(map[order.OrderID]int),
			matchesSeen:    make(map[order.MatchID]bool),
			cexTrades:      make(map[string]uint64),
			mkt:            mkt,
			cfg: &ArbMarketMakerConfig{
				Profit: profit,
			},
		}
		arbMM.currEpoch.Store(123)
		go arbMM.run()

		dummyNote := &core.BondRefundNote{}

		for i, note := range test.notes {
			cex.lastTrade = nil

			tCore.noteFeed <- note
			tCore.noteFeed <- dummyNote

			expectedCEXTrade := test.expectedCEXTrades[i]
			if (expectedCEXTrade == nil) != (cex.lastTrade == nil) {
				t.Fatalf("%s: expected cex order %v but got %v", test.name, (expectedCEXTrade != nil), (cex.lastTrade != nil))
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

func TestArbMarketMakerAutoRebalance(t *testing.T) {
	orderIDs := make([]order.OrderID, 5)
	for i := 0; i < 5; i++ {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	matchIDs := make([]order.MatchID, 5)
	for i := 0; i < 5; i++ {
		copy(matchIDs[i][:], encode.RandomBytes(32))
	}

	mkt := &core.Market{
		LotSize: 4e8,
	}

	baseID, quoteID := uint32(42), uint32(0)

	profitRate := float64(0.01)

	type test struct {
		name            string
		cfg             *AutoRebalanceConfig
		orders          map[order.OrderID]*core.Order
		oidToPlacement  map[order.OrderID]int
		cexBaseBalance  uint64
		cexQuoteBalance uint64
		dexBaseBalance  uint64
		dexQuoteBalance uint64
		activeCEXOrders bool

		expectedDeposit      *withdrawArgs
		expectedWithdraw     *withdrawArgs
		expectedCancels      []dex.Bytes
		expectedReserves     autoRebalanceReserves
		expectedBasePending  bool
		expectedQuotePending bool
	}

	currEpoch := uint64(123)

	tests := []*test{
		// "no orders, no need to rebalance"
		{
			name: "no orders, no need to rebalance",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  1e16,
				MinQuoteAmt: 1e12,
			},
			orders:          map[order.OrderID]*core.Order{},
			oidToPlacement:  map[order.OrderID]int{},
			cexBaseBalance:  1e16,
			cexQuoteBalance: 1e12,
			dexBaseBalance:  1e16,
			dexQuoteBalance: 1e12,
		},
		//  "no action with active cex orders"
		{
			name: "no action with active cex orders",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       4 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       4 * mkt.LotSize,
					Rate:      6e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
			},
			cexBaseBalance:  3 * mkt.LotSize,
			dexBaseBalance:  5 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			activeCEXOrders: true,
		},
		// "no orders, need to withdraw base"
		{
			name: "no orders, need to withdraw base",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  1e16,
				MinQuoteAmt: 1e12,
			},
			orders:          map[order.OrderID]*core.Order{},
			oidToPlacement:  map[order.OrderID]int{},
			cexBaseBalance:  8e16,
			cexQuoteBalance: 1e12,
			dexBaseBalance:  9e15,
			dexQuoteBalance: 1e12,
			expectedWithdraw: &withdrawArgs{
				assetID: 42,
				amt:     (9e15+8e16)/2 - 9e15,
			},
			expectedBasePending: true,
		},
		//  "need to deposit base, no need to cancel order"
		{
			name: "need to deposit base, no need to cancel order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       4 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       4 * mkt.LotSize,
					Rate:      6e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
			},
			cexBaseBalance:  3 * mkt.LotSize,
			dexBaseBalance:  5 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			expectedDeposit: &withdrawArgs{
				assetID: 42,
				amt:     5 * mkt.LotSize,
			},
			expectedBasePending: true,
		},
		//  "need to deposit base, need to cancel 1 order"
		{
			name: "need to deposit base, need to cancel 1 order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       4 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       4 * mkt.LotSize,
					Rate:      6e7,
					Sell:      true,
					LockedAmt: 4 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
			},
			cexBaseBalance:  3 * mkt.LotSize,
			dexBaseBalance:  5*mkt.LotSize - 2,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			expectedCancels: []dex.Bytes{
				orderIDs[1][:],
			},
			expectedReserves: autoRebalanceReserves{
				baseDexReserves: (16*mkt.LotSize-2)/2 - 3*mkt.LotSize,
			},
		},
		//  "need to deposit base, need to cancel 2 orders"
		{
			name: "need to deposit base, need to cancel 2 orders",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  3 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 2 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 2 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[2]: {
					ID:        orderIDs[2][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 2 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
				orderIDs[2]: 2,
			},
			cexBaseBalance:  0,
			dexBaseBalance:  1000,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			expectedCancels: []dex.Bytes{
				orderIDs[2][:],
				orderIDs[1][:],
			},
			expectedReserves: autoRebalanceReserves{
				baseDexReserves: (6*mkt.LotSize + 1000) / 2,
			},
		},
		//  "need to withdraw base, no need to cancel order"
		{
			name: "need to withdraw base, no need to cancel order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  3 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
				orderIDs[2]: 2,
			},
			cexBaseBalance:  6 * mkt.LotSize,
			dexBaseBalance:  0,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			expectedWithdraw: &withdrawArgs{
				assetID: baseID,
				amt:     3 * mkt.LotSize,
			},
			expectedBasePending: true,
		},
		//  "need to withdraw base, need to cancel 1 order"
		{
			name: "need to withdraw base, need to cancel 1 order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  3 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      false,
					LockedAmt: 2*mkt.LotSize + 1500,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       2 * mkt.LotSize,
					Rate:      6e7,
					Sell:      false,
					LockedAmt: 2*mkt.LotSize + 1500,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
			},
			cexBaseBalance:  8*mkt.LotSize - 2,
			dexBaseBalance:  0,
			cexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			expectedCancels: []dex.Bytes{
				orderIDs[1][:],
			},
			expectedReserves: autoRebalanceReserves{
				baseCexReserves: 4*mkt.LotSize - 1,
			},
		},
		//  "need to deposit quote, no need to cancel order"
		{
			name: "need to deposit quote, no need to cancel order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(5e7, 2*mkt.LotSize),
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       2 * mkt.LotSize,
					Rate:      5e7,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(5e7, 2*mkt.LotSize),
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
			},
			cexBaseBalance:  10 * mkt.LotSize,
			dexBaseBalance:  10 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 4*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 8*mkt.LotSize),
			expectedDeposit: &withdrawArgs{
				assetID: 0,
				amt:     calc.BaseToQuote(5e7, 4*mkt.LotSize),
			},
			expectedQuotePending: true,
		},
		//  "need to deposit quote, need to cancel 1 order"
		{
			name: "need to deposit quote, no need to cancel order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       4 * mkt.LotSize,
					Rate:      5e7,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(5e7, 4*mkt.LotSize),
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       4 * mkt.LotSize,
					Rate:      5e7,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(5e7, 4*mkt.LotSize),
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
			},
			cexBaseBalance:  10 * mkt.LotSize,
			dexBaseBalance:  10 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 4*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 2*mkt.LotSize),
			expectedCancels: []dex.Bytes{
				orderIDs[1][:],
			},
			expectedReserves: autoRebalanceReserves{
				quoteDexReserves: calc.BaseToQuote(5e7, 3*mkt.LotSize),
			},
		},
		//  "need to withdraw quote, no need to cancel order"
		{
			name: "need to withdraw quote, no need to cancel order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       3 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 3 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       3 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 3 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
			},
			cexBaseBalance:  10 * mkt.LotSize,
			dexBaseBalance:  10 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 4*mkt.LotSize) + calc.BaseToQuote(uint64(float64(5e7)*(1+profitRate)), 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 2*mkt.LotSize) + 12e6,
			expectedWithdraw: &withdrawArgs{
				assetID: quoteID,
				amt:     calc.BaseToQuote(5e7, 4*mkt.LotSize),
			},
			expectedQuotePending: true,
		},
		//  "need to withdraw quote, no need to cancel 1 order"
		{
			name: "need to withdraw quote, no need to cancel 1 order",
			cfg: &AutoRebalanceConfig{
				MinBaseAmt:  6 * mkt.LotSize,
				MinQuoteAmt: calc.BaseToQuote(5e7, 6*mkt.LotSize),
			},
			orders: map[order.OrderID]*core.Order{
				orderIDs[0]: {
					ID:        orderIDs[0][:],
					Qty:       3 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 3 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
				orderIDs[1]: {
					ID:        orderIDs[1][:],
					Qty:       3 * mkt.LotSize,
					Rate:      5e7,
					Sell:      true,
					LockedAmt: 3 * mkt.LotSize,
					Epoch:     currEpoch - 2,
				},
			},
			oidToPlacement: map[order.OrderID]int{
				orderIDs[0]: 0,
				orderIDs[1]: 1,
			},
			cexBaseBalance:  10 * mkt.LotSize,
			dexBaseBalance:  10 * mkt.LotSize,
			cexQuoteBalance: calc.BaseToQuote(5e7, 4*mkt.LotSize) + calc.BaseToQuote(uint64(float64(5e7)*(1+profitRate)), 6*mkt.LotSize),
			dexQuoteBalance: calc.BaseToQuote(5e7, 2*mkt.LotSize) + 12e6 - 2,
			expectedCancels: []dex.Bytes{
				orderIDs[1][:],
			},
			expectedReserves: autoRebalanceReserves{
				quoteCexReserves: calc.BaseToQuote(5e7, 4*mkt.LotSize) + 1,
			},
		},
	}

	runTest := func(test *test) {
		cex := newTBotCEXAdaptor()
		cex.balances = map[uint32]*botBalance{
			baseID:  {Available: test.cexBaseBalance},
			quoteID: {Available: test.cexQuoteBalance},
		}
		tCore := newTCore()
		tCore.setAssetBalances(map[uint32]uint64{
			baseID:  test.dexBaseBalance,
			quoteID: test.dexQuoteBalance,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mm := &arbMarketMaker{
			ctx:            ctx,
			cex:            cex,
			core:           newTBotCoreAdaptor(tCore),
			baseID:         baseID,
			quoteID:        quoteID,
			oidToPlacement: test.oidToPlacement,
			ords:           test.orders,
			log:            tLogger,
			cfg: &ArbMarketMakerConfig{
				AutoRebalance: test.cfg,
				Profit:        profitRate,
			},
			mkt: mkt,
		}

		if test.activeCEXOrders {
			mm.cexTrades = map[string]uint64{"abc": 1234}
		}

		mm.rebalanceAssets()

		if (test.expectedDeposit == nil) != (cex.lastDepositArgs == nil) {
			t.Fatalf("%s: expected deposit %v but got %v", test.name, (test.expectedDeposit != nil), (cex.lastDepositArgs != nil))
		}
		if test.expectedDeposit != nil {
			if *cex.lastDepositArgs != *test.expectedDeposit {
				t.Fatalf("%s: expected deposit %+v but got %+v", test.name, test.expectedDeposit, cex.lastDepositArgs)
			}
		}

		if (test.expectedWithdraw == nil) != (cex.lastWithdrawArgs == nil) {
			t.Fatalf("%s: expected withdraw %v but got %v", test.name, (test.expectedWithdraw != nil), (cex.lastWithdrawArgs != nil))
		}
		if test.expectedWithdraw != nil {
			if *cex.lastWithdrawArgs != *test.expectedWithdraw {
				t.Fatalf("%s: expected withdraw %+v but got %+v", test.name, test.expectedWithdraw, cex.lastWithdrawArgs)
			}
		}

		if len(tCore.cancelsPlaced) != len(test.expectedCancels) {
			t.Fatalf("%s: expected %d cancels but got %d", test.name, len(test.expectedCancels), len(tCore.cancelsPlaced))
		}
		for i := range test.expectedCancels {
			if !tCore.cancelsPlaced[i].Equal(test.expectedCancels[i]) {
				t.Fatalf("%s: expected cancel %d %s but got %s", test.name, i, hex.EncodeToString(test.expectedCancels[i]), hex.EncodeToString(tCore.cancelsPlaced[i]))
			}
		}

		if test.expectedReserves != mm.reserves {
			t.Fatalf("%s: expected reserves %+v but got %+v", test.name, test.expectedReserves, mm.reserves)
		}

		if test.expectedBasePending != mm.pendingBaseRebalance.Load() {
			t.Fatalf("%s: expected pending base rebalance %v but got %v", test.name, test.expectedBasePending, mm.pendingBaseRebalance.Load())
		}
		if test.expectedQuotePending != mm.pendingQuoteRebalance.Load() {
			t.Fatalf("%s: expected pending quote rebalance %v but got %v", test.name, test.expectedQuotePending, mm.pendingQuoteRebalance.Load())
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}
