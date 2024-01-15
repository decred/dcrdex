//go:build !harness && !botlive

package mm

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

var (
	dcrBipID uint32 = 42
	btcBipID uint32 = 0
)

type tRebalancer struct {
	basis        uint64
	breakEven    uint64
	breakEvenErr error
	sortedBuys   map[int][]*groupedOrder
	sortedSells  map[int][]*groupedOrder
}

var _ rebalancer = (*tRebalancer)(nil)

func (r *tRebalancer) basisPrice() uint64 {
	return r.basis
}

func (r *tRebalancer) halfSpread(basisPrice uint64) (uint64, error) {
	return r.breakEven, r.breakEvenErr
}

func (r *tRebalancer) groupedOrders() (buys, sells map[int][]*groupedOrder) {
	return r.sortedBuys, r.sortedSells
}

func TestRebalance(t *testing.T) {
	const rateStep uint64 = 1e3
	const midGap uint64 = 5e8
	const lotSize uint64 = 50e8
	const breakEven uint64 = 200 * rateStep
	const newEpoch = 123_456_789
	const driftTolerance = 0.001
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

	tCore := newTCore()

	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	mkt := &core.Market{
		RateStep:   rateStep,
		AtomToConv: 1,
		LotSize:    lotSize,
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
	}

	newBalancer := func(existingBuys, existingSells map[int][]*groupedOrder) *tRebalancer {
		return &tRebalancer{
			basis:       midGap,
			breakEven:   breakEven,
			sortedBuys:  existingBuys,
			sortedSells: existingSells,
		}
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	type test struct {
		name       string
		cfg        *BasicMarketMakingConfig
		epoch      uint64
		rebalancer *tRebalancer

		isAccountLocker map[uint32]bool
		balances        map[uint32]uint64

		expectedBuys    []rateLots
		expectedSells   []rateLots
		expectedCancels []order.OrderID
	}

	newgroupedOrder := func(id order.OrderID, lots, rate uint64, sell bool, freeCancel bool) *groupedOrder {
		var epoch uint64 = newEpoch
		if freeCancel {
			epoch = newEpoch - 2
		}
		return &groupedOrder{
			id:    id,
			epoch: epoch,
			rate:  rate,
			lots:  lots,
		}
	}

	driftToleranceEdge := func(rate uint64, within bool) uint64 {
		edge := rate + uint64(float64(rate)*driftTolerance)
		if within {
			return edge - rateStep
		}
		return edge + rateStep
	}

	requiredForOrder := func(sell bool, placements []*OrderPlacement, strategy GapStrategy) (req uint64) {
		for _, placement := range placements {
			if sell {
				req += placement.Lots * mkt.LotSize
			} else {
				rate := orderPrice(midGap, breakEven, strategy, placement.GapFactor, sell, mkt)
				req += calc.BaseToQuote(rate, placement.Lots*mkt.LotSize)
			}
		}
		return
	}

	tests := []*test{
		// "no existing orders, one order per side"
		{
			name: "no existing orders, one order per side",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 3),
					lots: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 3),
					lots: 1,
				},
			},
		},
		// "no existing orders, no sell placements"
		{
			name: "no existing orders, no sell placements",
			cfg: &BasicMarketMakingConfig{
				GapStrategy:    GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},

			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 3),
					lots: 1,
				},
			},
			expectedSells: []rateLots{},
		},
		// "no existing orders, no buy placements"
		{
			name: "no existing orders, no buy placements",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},

			expectedBuys: []rateLots{},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 3),
					lots: 1,
				},
			},
		},
		//  "no existing orders, multiple placements per side"
		{
			name: "no existing orders, multiple placements per side",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},

			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "test balances edge, enough for orders"
		{
			name: "test balances edge, enough for orders",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: requiredForOrder(true, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 2*sellFees.swap + sellFees.funding,
				btcBipID: requiredForOrder(false, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 2*buyFees.swap + buyFees.funding,
			},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "test balances edge, not enough for orders"
		{
			name: "test balances edge, not enough for orders",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: requiredForOrder(true, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 2*sellFees.swap + sellFees.funding - 1,
				btcBipID: requiredForOrder(false, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 2*buyFees.swap + buyFees.funding - 1,
			},
			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 2),
					lots: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 2),
					lots: 1,
				},
			},
		},
		// "test balances edge, enough for 2 lot orders"
		{
			name: "test balances edge, enough for 2 lot orders",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: requiredForOrder(true, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 3*sellFees.swap + sellFees.funding,
				btcBipID: requiredForOrder(false, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 3*buyFees.swap + buyFees.funding,
			},

			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           2,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           2,
					placementIndex: 1,
				},
			},
		},
		// "test balances edge, not enough for 2 lot orders, place 1 lot"
		{
			name: "test balances edge, not enough for 2 lot orders, place 1 lot",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
			},
			rebalancer: newBalancer(nil, nil),
			balances: map[uint32]uint64{
				dcrBipID: requiredForOrder(true, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 3*sellFees.swap + sellFees.funding - 1,
				btcBipID: requiredForOrder(false, []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				}, GapStrategyMultiplier) + 3*buyFees.swap + buyFees.funding - 1,
			},
			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		//  "existing orders outside edge of drift tolerance"
		{
			name: "existing orders outside edge of drift tolerance",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[0], 1, driftToleranceEdge(midGap-(breakEven*2), false), false, true),
					},
				},
				map[int][]*groupedOrder{
					1: {
						newgroupedOrder(orderIDs[1], 1, driftToleranceEdge(midGap+(breakEven*3), false), true, true),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedCancels: []order.OrderID{
				orderIDs[0],
				orderIDs[1],
			},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 2),
					lots:           1,
					placementIndex: 0,
				},
			},
		},
		//  "existing orders within edge of drift tolerance"
		{
			name: "existing orders within edge of drift tolerance",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[0], 1, driftToleranceEdge(midGap-(breakEven*2), true), false, true),
					},
				},
				map[int][]*groupedOrder{
					1: {
						newgroupedOrder(orderIDs[1], 1, driftToleranceEdge(midGap+(breakEven*3), true), true, true),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 2),
					lots: 1,
				},
			},
		},
		//  "existing partially filled orders within drift tolerance"
		{
			name: "existing partially filled orders within drift tolerance",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      2,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[0], 1, driftToleranceEdge(midGap-(breakEven*2), true), false, true),
					},
				},
				map[int][]*groupedOrder{
					1: {
						newgroupedOrder(orderIDs[1], 1, driftToleranceEdge(midGap+(breakEven*3), true), true, true),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		//  "existing partially filled orders outside drift tolerance"
		{
			name: "existing partially filled orders outside drift tolerance",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      2,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      2,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[0], 1, driftToleranceEdge(midGap-(breakEven*2), false), false, true),
					},
				},
				map[int][]*groupedOrder{
					1: {
						newgroupedOrder(orderIDs[1], 1, driftToleranceEdge(midGap+(breakEven*3), false), true, true),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedBuys: []rateLots{
				{
					rate: midGap - (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate: midGap + (breakEven * 2),
					lots: 1,
				},
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedCancels: []order.OrderID{
				orderIDs[0],
				orderIDs[1],
			},
		},
		// "cannot place buy order due to self matching"
		{
			name: "cannot place buy order due to self matching",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				nil,
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[1], 1, midGap-(breakEven*2)-1, true, true),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedCancels: []order.OrderID{
				orderIDs[1],
			},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "cannot place sell order due to self matching"
		{
			name: "cannot place sell order due to self matching",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[1], 1, midGap+(breakEven*2), true, true),
					},
				},
				nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedCancels: []order.OrderID{
				orderIDs[1],
			},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "cannot place buy order due to self matching, can't cancel because too soon"
		{
			name: "cannot place buy order due to self matching, can't cancel because too soon",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				nil,
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[1], 1, midGap-(breakEven*2)-1, true, false),
					},
				}),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedCancels: []order.OrderID{},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
		// "cannot place sell order due to self matching, can't cancel because too soon"
		{
			name: "cannot place sell order due to self matching, can't cancel because too soon",
			cfg: &BasicMarketMakingConfig{
				GapStrategy: GapStrategyMultiplier,
				SellPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				BuyPlacements: []*OrderPlacement{
					{
						Lots:      1,
						GapFactor: 2,
					},
					{
						Lots:      1,
						GapFactor: 3,
					},
				},
				DriftTolerance: driftTolerance,
			},
			rebalancer: newBalancer(
				map[int][]*groupedOrder{
					0: {
						newgroupedOrder(orderIDs[1], 1, midGap+(breakEven*2), true, false),
					},
				},
				nil),
			balances: map[uint32]uint64{
				dcrBipID: 1e13,
				btcBipID: 1e13,
			},
			expectedCancels: []order.OrderID{},
			expectedBuys: []rateLots{
				{
					rate:           midGap - (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
			expectedSells: []rateLots{
				{
					rate:           midGap + (breakEven * 3),
					lots:           1,
					placementIndex: 1,
				},
			},
		},
	}

	for _, tt := range tests {
		if tt.isAccountLocker != nil {
			tCore.isAccountLocker = tt.isAccountLocker
		} else {
			tCore.isAccountLocker = map[uint32]bool{}
		}

		tCore.setAssetBalances(tt.balances)

		epoch := tt.epoch
		if epoch == 0 {
			epoch = newEpoch
		}

		cancels, buys, sells := basicMMRebalance(epoch, tt.rebalancer, newTBotCoreAdaptor(tCore), tt.cfg, mkt, buyFees, sellFees, log)

		if len(cancels) != len(tt.expectedCancels) {
			t.Fatalf("%s: cancel count mismatch. expected %d, got %d", tt.name, len(tt.expectedCancels), len(cancels))
		}

		expectedCancels := make(map[order.OrderID]bool)
		for _, id := range tt.expectedCancels {
			expectedCancels[id] = true
		}
		for _, cancel := range cancels {
			var id order.OrderID
			copy(id[:], cancel)
			if !expectedCancels[id] {
				t.Fatalf("%s: unexpected cancel order ID %s", tt.name, id)
			}
		}

		if len(buys) != len(tt.expectedBuys) {
			t.Fatalf("%s: buy count mismatch. expected %d, got %d", tt.name, len(tt.expectedBuys), len(buys))
		}
		if len(sells) != len(tt.expectedSells) {
			t.Fatalf("%s: sell count mismatch. expected %d, got %d", tt.name, len(tt.expectedSells), len(sells))
		}

		for i, buy := range buys {
			if buy.rate != tt.expectedBuys[i].rate {
				t.Fatalf("%s: buy rate mismatch. expected %d, got %d", tt.name, tt.expectedBuys[i].rate, buy.rate)
			}
			if buy.lots != tt.expectedBuys[i].lots {
				t.Fatalf("%s: buy lots mismatch. expected %d, got %d", tt.name, tt.expectedBuys[i].lots, buy.lots)
			}
			if buy.placementIndex != tt.expectedBuys[i].placementIndex {
				t.Fatalf("%s: buy placement index mismatch. expected %d, got %d", tt.name, tt.expectedBuys[i].placementIndex, buy.placementIndex)
			}
		}

		for i, sell := range sells {
			if sell.rate != tt.expectedSells[i].rate {
				t.Fatalf("%s: sell rate mismatch. expected %d, got %d", tt.name, tt.expectedSells[i].rate, sell.rate)
			}
			if sell.lots != tt.expectedSells[i].lots {
				t.Fatalf("%s: sell lots mismatch. expected %d, got %d", tt.name, tt.expectedSells[i].lots, sell.lots)
			}
			if sell.placementIndex != tt.expectedSells[i].placementIndex {
				t.Fatalf("%s: sell placement index mismatch. expected %d, got %d", tt.name, tt.expectedSells[i].placementIndex, sell.placementIndex)
			}
		}
	}
}

func TestBasisPrice(t *testing.T) {
	mkt := &core.Market{
		RateStep:   1,
		BaseID:     42,
		QuoteID:    0,
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
		rate := basisPrice(ob, oracle, cfg, mkt, tt.fiatRate, log)
		if rate != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, rate, tt.exp)
		}
	}
}

func TestBreakEvenHalfSpread(t *testing.T) {
	mkt := &core.Market{
		LotSize:    20e8, // 20 DCR
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
		AtomToConv: 1,
	}

	tCore := newTCore()
	log := dex.StdOutLogger("T", dex.LevelTrace)

	tests := []*struct {
		name             string
		basisPrice       uint64
		sellSwapFees     uint64
		sellRedeemFees   uint64
		buySwapFees      uint64
		buyRedeemFees    uint64
		exp              uint64
		singleLotFeesErr error
		expErr           bool
	}{
		{
			name:   "basis price = 0 not allowed",
			expErr: true,
		},
		{
			name:             "swap fees error propagates",
			singleLotFeesErr: errors.New("t"),
			expErr:           true,
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
		tCore.sellSwapFees = tt.sellSwapFees
		tCore.sellRedeemFees = tt.sellRedeemFees
		tCore.buySwapFees = tt.buySwapFees
		tCore.buyRedeemFees = tt.buyRedeemFees
		tCore.singleLotFeesErr = tt.singleLotFeesErr

		basicMM := &basicMarketMaker{
			core: newTBotCoreAdaptor(tCore),
			mkt:  mkt,
			log:  log,
		}

		halfSpread, err := basicMM.halfSpread(tt.basisPrice)
		if (err != nil) != tt.expErr {
			t.Fatalf("expErr = %t, err = %v", tt.expErr, err)
		}
		if halfSpread != tt.exp {
			t.Fatalf("%s: %d != %d", tt.name, halfSpread, tt.exp)
		}
	}
}

func TestGroupedOrders(t *testing.T) {
	const rateStep uint64 = 1e3
	const lotSize uint64 = 50e8
	mkt := &core.Market{
		RateStep:   rateStep,
		AtomToConv: 1,
		LotSize:    lotSize,
		BaseID:     dcrBipID,
		QuoteID:    btcBipID,
	}

	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		copy(orderIDs[i][:], encode.RandomBytes(32))
	}

	orders := map[order.OrderID]*core.Order{
		orderIDs[0]: {
			ID:     orderIDs[0][:],
			Sell:   true,
			Rate:   100e8,
			Qty:    2 * lotSize,
			Filled: lotSize,
		},
		orderIDs[1]: {
			ID:     orderIDs[1][:],
			Sell:   false,
			Rate:   200e8,
			Qty:    2 * lotSize,
			Filled: lotSize,
		},
		orderIDs[2]: {
			ID:   orderIDs[2][:],
			Sell: true,
			Rate: 300e8,
			Qty:  2 * lotSize,
		},
		orderIDs[3]: {
			ID:   orderIDs[3][:],
			Sell: false,
			Rate: 400e8,
			Qty:  1 * lotSize,
		},
		orderIDs[4]: {
			ID:   orderIDs[4][:],
			Sell: false,
			Rate: 402e8,
			Qty:  1 * lotSize,
		},
	}

	ordToPlacementIndex := map[order.OrderID]int{
		orderIDs[0]: 0,
		orderIDs[1]: 0,
		orderIDs[2]: 1,
		orderIDs[3]: 1,
		orderIDs[4]: 1,
	}

	mm := &basicMarketMaker{
		mkt:            mkt,
		ords:           orders,
		oidToPlacement: ordToPlacementIndex,
	}

	expectedBuys := map[int][]*groupedOrder{
		0: {{
			id:   orderIDs[1],
			rate: 200e8,
			lots: 1,
		}},
		1: {{
			id:   orderIDs[3],
			rate: 400e8,
			lots: 1,
		}, {
			id:   orderIDs[4],
			rate: 402e8,
			lots: 1,
		}},
	}

	expectedSells := map[int][]*groupedOrder{
		0: {{
			id:   orderIDs[0],
			rate: 100e8,
			lots: 1,
		}},
		1: {{
			id:   orderIDs[2],
			rate: 300e8,
			lots: 2,
		}},
	}

	buys, sells := mm.groupedOrders()

	for i, buy := range buys {
		sort.Slice(buy, func(i, j int) bool {
			return buy[i].rate < buy[j].rate
		})
		reflect.DeepEqual(buy, expectedBuys[i])
	}

	for i, sell := range sells {
		sort.Slice(sell, func(i, j int) bool {
			return sell[i].rate < sell[j].rate
		})
		reflect.DeepEqual(sell, expectedSells[i])
	}
}
