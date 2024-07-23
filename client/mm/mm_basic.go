// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

// GapStrategy is a specifier for an algorithm to choose the maker bot's target
// spread.
type GapStrategy string

const (
	// GapStrategyMultiplier calculates the spread by multiplying the
	// break-even gap by the specified multiplier, 1 <= r <= 100.
	GapStrategyMultiplier GapStrategy = "multiplier"
	// GapStrategyAbsolute sets the spread to the rate difference.
	GapStrategyAbsolute GapStrategy = "absolute"
	// GapStrategyAbsolutePlus sets the spread to the rate difference plus the
	// break-even gap.
	GapStrategyAbsolutePlus GapStrategy = "absolute-plus"
	// GapStrategyPercent sets the spread as a ratio of the mid-gap rate.
	// 0 <= r <= 0.1
	GapStrategyPercent GapStrategy = "percent"
	// GapStrategyPercentPlus sets the spread as a ratio of the mid-gap rate
	// plus the break-even gap.
	GapStrategyPercentPlus GapStrategy = "percent-plus"
)

// OrderPlacement represents the distance from the mid-gap and the
// amount of lots that should be placed at this distance.
type OrderPlacement struct {
	// Lots is the max number of lots to place at this distance from the
	// mid-gap rate. If there is not enough balance to place this amount
	// of lots, the max that can be afforded will be placed.
	Lots uint64 `json:"lots"`

	// GapFactor controls the gap width in a way determined by the GapStrategy.
	GapFactor float64 `json:"gapFactor"`
}

// BasicMarketMakingConfig is the configuration for a simple market
// maker that places orders on both sides of the order book.
type BasicMarketMakingConfig struct {
	// GapStrategy selects an algorithm for calculating the distance from
	// the basis price to place orders.
	GapStrategy GapStrategy `json:"gapStrategy"`

	// SellPlacements is a list of order placements for sell orders.
	// The orders are prioritized from the first in this list to the
	// last.
	SellPlacements []*OrderPlacement `json:"sellPlacements"`

	// BuyPlacements is a list of order placements for buy orders.
	// The orders are prioritized from the first in this list to the
	// last.
	BuyPlacements []*OrderPlacement `json:"buyPlacements"`

	// DriftTolerance is how far away from an ideal price orders can drift
	// before they are replaced (units: ratio of price). Default: 0.1%.
	// 0 <= x <= 0.01.
	DriftTolerance float64 `json:"driftTolerance"`
}

func needBreakEvenHalfSpread(strat GapStrategy) bool {
	return strat == GapStrategyAbsolutePlus || strat == GapStrategyPercentPlus || strat == GapStrategyMultiplier
}

func (c *BasicMarketMakingConfig) Validate() error {
	if c.DriftTolerance == 0 {
		c.DriftTolerance = 0.001
	}
	if c.DriftTolerance < 0 || c.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", c.DriftTolerance)
	}

	if c.GapStrategy != GapStrategyMultiplier &&
		c.GapStrategy != GapStrategyPercent &&
		c.GapStrategy != GapStrategyPercentPlus &&
		c.GapStrategy != GapStrategyAbsolute &&
		c.GapStrategy != GapStrategyAbsolutePlus {
		return fmt.Errorf("unknown gap strategy %q", c.GapStrategy)
	}

	validatePlacement := func(p *OrderPlacement) error {
		var limits [2]float64
		switch c.GapStrategy {
		case GapStrategyMultiplier:
			limits = [2]float64{1, 100}
		case GapStrategyPercent, GapStrategyPercentPlus:
			limits = [2]float64{0, 0.1}
		case GapStrategyAbsolute, GapStrategyAbsolutePlus:
			limits = [2]float64{0, math.MaxFloat64} // validate at < spot price at creation time
		default:
			return fmt.Errorf("unknown gap strategy %q", c.GapStrategy)
		}

		if p.GapFactor < limits[0] || p.GapFactor > limits[1] {
			return fmt.Errorf("%s gap factor %f is out of bounds %+v", c.GapStrategy, p.GapFactor, limits)
		}

		return nil
	}

	sellPlacements := make(map[float64]bool, len(c.SellPlacements))
	for _, p := range c.SellPlacements {
		if _, duplicate := sellPlacements[p.GapFactor]; duplicate {
			return fmt.Errorf("duplicate sell placement %f", p.GapFactor)
		}
		sellPlacements[p.GapFactor] = true
		if err := validatePlacement(p); err != nil {
			return fmt.Errorf("invalid sell placement: %w", err)
		}
	}

	buyPlacements := make(map[float64]bool, len(c.BuyPlacements))
	for _, p := range c.BuyPlacements {
		if _, duplicate := buyPlacements[p.GapFactor]; duplicate {
			return fmt.Errorf("duplicate buy placement %f", p.GapFactor)
		}
		buyPlacements[p.GapFactor] = true
		if err := validatePlacement(p); err != nil {
			return fmt.Errorf("invalid buy placement: %w", err)
		}
	}

	return nil
}

type basicMMCalculator interface {
	basisPrice() uint64
	halfSpread(uint64) (uint64, error)
	feeGapStats(uint64) (*FeeGapStats, error)
}

type basicMMCalculatorImpl struct {
	*market
	oracle oracle
	core   botCoreAdaptor
	cfg    *BasicMarketMakingConfig
	log    dex.Logger
}

// basisPrice calculates the basis price for the market maker.
// The mid-gap of the dex order book is used, and if oracles are
// available, and the oracle weighting is > 0, the oracle price
// is used to adjust the basis price.
// If the dex market is empty, but there are oracles available and
// oracle weighting is > 0, the oracle rate is used.
// If the dex market is empty and there are either no oracles available
// or oracle weighting is 0, the fiat rate is used.
// If there is no fiat rate available, the empty market rate in the
// configuration is used.
func (b *basicMMCalculatorImpl) basisPrice() uint64 {
	oracleRate := b.msgRate(b.oracle.getMarketPrice(b.baseID, b.quoteID))
	b.log.Tracef("oracle rate = %s", b.fmtRate(oracleRate))

	if oracleRate == 0 {
		oracleRate = b.core.ExchangeRateFromFiatSources()
		if oracleRate == 0 {
			return 0
		}

		b.log.Tracef("using fiat rate = %s", b.fmtRate(oracleRate))
	}

	return steppedRate(oracleRate, b.rateStep)
}

// halfSpread calculates the distance from the mid-gap where if you sell a lot
// at the basis price plus half-gap, then buy a lot at the basis price minus
// half-gap, you will have one lot of the base asset plus the total fees in
// base units. Since the fees are in base units, basis price can be used to
// convert the quote fees to base units. In the case of tokens, the fees are
// converted using fiat rates.
func (b *basicMMCalculatorImpl) halfSpread(basisPrice uint64) (uint64, error) {
	feeStats, err := b.feeGapStats(basisPrice)
	if err != nil {
		return 0, err
	}
	return feeStats.FeeGap / 2, nil
}

// FeeGapStats is info about market and fee state. The intepretation of the
// various statistics may vary slightly with bot type.
type FeeGapStats struct {
	BasisPrice    uint64 `json:"basisPrice"`
	RemoteGap     uint64 `json:"remoteGap"`
	FeeGap        uint64 `json:"feeGap"`
	RoundTripFees uint64 `json:"roundTripFees"` // base units
}

func (b *basicMMCalculatorImpl) feeGapStats(basisPrice uint64) (*FeeGapStats, error) {
	if basisPrice == 0 { // prevent divide by zero later
		return nil, fmt.Errorf("basis price cannot be zero")
	}

	sellFeesInBaseUnits, err := b.core.OrderFeesInUnits(true, true, basisPrice)
	if err != nil {
		return nil, fmt.Errorf("error getting sell fees in base units: %w", err)
	}

	buyFeesInBaseUnits, err := b.core.OrderFeesInUnits(false, true, basisPrice)
	if err != nil {
		return nil, fmt.Errorf("error getting buy fees in base units: %w", err)
	}

	/*
	 * g = half-gap
	 * r = basis price (atomic ratio)
	 * l = lot size
	 * f = total fees in base units
	 *
	 * We must choose a half-gap such that:
	 * (r + g) * l / (r - g) = l + f
	 *
	 * This means that when you sell a lot at the basis price plus half-gap,
	 * then buy a lot at the basis price minus half-gap, you will have one
	 * lot of the base asset plus the total fees in base units.
	 *
	 * Solving for g, you get:
	 * g = f * r / (f + 2l)
	 */

	f := sellFeesInBaseUnits + buyFeesInBaseUnits
	l := b.lotSize

	r := float64(basisPrice) / calc.RateEncodingFactor
	g := float64(f) * r / float64(f+2*l)

	halfGap := uint64(math.Round(g * calc.RateEncodingFactor))

	if b.log.Level() == dex.LevelTrace {
		b.log.Tracef("halfSpread: basis price = %s, lot size = %s, aggregate fees = %s, half-gap = %s, sell fees = %s, buy fees = %s",
			b.fmtRate(basisPrice), b.fmtBase(l), b.fmtBaseFees(f), b.fmtRate(halfGap),
			b.fmtBaseFees(sellFeesInBaseUnits), b.fmtBaseFees(buyFeesInBaseUnits))
	}

	return &FeeGapStats{
		BasisPrice:    basisPrice,
		FeeGap:        halfGap * 2,
		RoundTripFees: f,
	}, nil
}

type basicMarketMaker struct {
	*unifiedExchangeAdaptor
	cfgV             atomic.Value // *BasicMarketMakingConfig
	core             botCoreAdaptor
	oracle           oracle
	rebalanceRunning atomic.Bool
	calculator       basicMMCalculator
}

var _ bot = (*basicMarketMaker)(nil)

func (m *basicMarketMaker) cfg() *BasicMarketMakingConfig {
	return m.cfgV.Load().(*BasicMarketMakingConfig)
}

func (m *basicMarketMaker) orderPrice(basisPrice, feeAdj uint64, sell bool, gapFactor float64) uint64 {
	var adj uint64

	// Apply the base strategy.
	switch m.cfg().GapStrategy {
	case GapStrategyMultiplier:
		adj = uint64(math.Round(float64(feeAdj) * gapFactor))
	case GapStrategyPercent, GapStrategyPercentPlus:
		adj = uint64(math.Round(gapFactor * float64(basisPrice)))
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		adj = m.msgRate(gapFactor)
	}

	// Add the break-even to the "-plus" strategies
	switch m.cfg().GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus:
		adj += feeAdj
	}

	adj = steppedRate(adj, m.rateStep)

	if sell {
		return basisPrice + adj
	}

	if basisPrice < adj {
		return 0
	}

	return basisPrice - adj
}

func (m *basicMarketMaker) ordersToPlace(basisPrice, feeAdj uint64) (buyOrders, sellOrders []*multiTradePlacement) {
	orders := func(orderPlacements []*OrderPlacement, sell bool) []*multiTradePlacement {
		placements := make([]*multiTradePlacement, 0, len(orderPlacements))
		for i, p := range orderPlacements {
			rate := m.orderPrice(basisPrice, feeAdj, sell, p.GapFactor)

			if m.log.Level() == dex.LevelTrace {
				m.log.Tracef("ordersToPlace.orders: %s placement # %d, gap factor = %f, rate = %s, %+v",
					sellStr(sell), i, p.GapFactor, m.fmtRate(rate), rate)
			}

			lots := p.Lots
			if rate == 0 {
				lots = 0
			}
			placements = append(placements, &multiTradePlacement{
				rate: rate,
				lots: lots,
			})
		}
		return placements
	}

	buyOrders = orders(m.cfg().BuyPlacements, false)
	sellOrders = orders(m.cfg().SellPlacements, true)
	return buyOrders, sellOrders
}

func (m *basicMarketMaker) rebalance(newEpoch uint64) {
	if !m.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer m.rebalanceRunning.Store(false)

	m.log.Tracef("rebalance: epoch %d", newEpoch)
	basisPrice := m.calculator.basisPrice()
	if basisPrice == 0 {
		m.log.Errorf("No basis price available")
		return
	}

	feeGap, err := m.calculator.feeGapStats(basisPrice)
	if err != nil {
		m.log.Errorf("Could not calculate fee-gap stats: %v", err)
		return
	}

	m.registerFeeGap(feeGap)
	var feeAdj uint64
	if needBreakEvenHalfSpread(m.cfg().GapStrategy) {
		feeAdj = feeGap.FeeGap / 2
	}

	if m.log.Level() == dex.LevelTrace {
		m.log.Tracef("ordersToPlace %s, basis price = %s, break-even fee adjustment = %s",
			m.name, m.fmtRate(basisPrice), m.fmtRate(feeAdj))
	}

	buyOrders, sellOrders := m.ordersToPlace(basisPrice, feeAdj)
	m.multiTrade(buyOrders, false, m.cfg().DriftTolerance, newEpoch)
	m.multiTrade(sellOrders, true, m.cfg().DriftTolerance, newEpoch)
}

func (m *basicMarketMaker) botLoop(ctx context.Context) (*sync.WaitGroup, error) {
	_, bookFeed, err := m.core.SyncBook(m.host, m.baseID, m.quoteID)
	if err != nil {
		return nil, fmt.Errorf("failed to sync book: %v", err)
	}

	m.calculator = &basicMMCalculatorImpl{
		market: m.market,
		oracle: m.oracle,
		core:   m.core,
		cfg:    m.cfg(),
		log:    m.log,
	}

	// Process book updates
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case ni := <-bookFeed.Next():
				switch epoch := ni.Payload.(type) {
				case *core.ResolvedEpoch:
					m.rebalance(epoch.Current)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

func (m *basicMarketMaker) updateConfig(cfg *BotConfig) error {
	if cfg.BasicMMConfig == nil {
		// implies bug in caller
		return errors.New("no market making config provided")
	}

	err := cfg.BasicMMConfig.Validate()
	if err != nil {
		return fmt.Errorf("invalid market making config: %v", err)
	}

	m.cfgV.Store(cfg.BasicMMConfig)
	return nil
}

// RunBasicMarketMaker starts a basic market maker bot.
func newBasicMarketMaker(cfg *BotConfig, adaptorCfg *exchangeAdaptorCfg, oracle oracle, log dex.Logger) (*basicMarketMaker, error) {
	if cfg.BasicMMConfig == nil {
		// implies bug in caller
		return nil, errors.New("no market making config provided")
	}

	adaptor, err := newUnifiedExchangeAdaptor(adaptorCfg)
	if err != nil {
		return nil, fmt.Errorf("error constructing exchange adaptor: %w", err)
	}

	err = cfg.BasicMMConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid market making config: %v", err)
	}

	basicMM := &basicMarketMaker{
		unifiedExchangeAdaptor: adaptor,
		core:                   adaptor,
		oracle:                 oracle,
	}
	basicMM.cfgV.Store(cfg.BasicMMConfig)
	adaptor.setBotLoop(basicMM.botLoop)
	return basicMM, nil
}
