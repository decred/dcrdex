package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

// gapEngineInputs are the input functions required a GapEngine to function.
type gapEngineInputs interface {
	basisPrice(oracleBias, oracleWeighting, emptyMarketRate float64) uint64
	halfSpread(basisPrice uint64) (uint64, error)
	sortedOrders() (buys, sells []*Order)
	maxBuy(rate uint64) (*MaxOrderEstimate, error)
	maxSell() (*MaxOrderEstimate, error)
	conventionalRateToMsg(g float64) uint64
	rateStep() uint64
	placeOrder(lots, rate uint64, sell bool) (order.OrderID, error)
	cancelOrder(oid order.OrderID) error
	lotSize() uint64
}

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

// GapEngineCfg is the configuration for a GapEngine.
type GapEngineCfg struct {
	// Lots is the number of lots to allocate to each side of the market. This
	// is an ideal allotment, but at any given time, a side could have up to
	// 2 * Lots on order.
	Lots uint64 `json:"lots"`

	// GapStrategy selects an algorithm for calculating the target spread.
	GapStrategy GapStrategy `json:"gapStrategy"`

	// GapFactor controls the gap width in a way determined by the GapStrategy.
	GapFactor float64 `json:"gapFactor"`

	// DriftTolerance is how far away from an ideal price an order can drift
	// before it will replaced (units: ratio of price). Default: 0.1%.
	// 0 <= x <= 0.01.
	DriftTolerance float64 `json:"driftTolerance"`

	// OracleWeighting affects how the target price is derived based on external
	// market data. OracleWeighting, r, determines the target price with the
	// formula:
	//   target_price = dex_mid_gap_price * (1 - r) + oracle_price * r
	// OracleWeighting is limited to 0 <= x <= 1.0.
	// Fetching of price data is disabled if OracleWeighting = 0.
	OracleWeighting float64 `json:"oracleWeighting"`

	// OracleBias applies a bias in the positive (higher price) or negative
	// (lower price) direction. -0.05 <= x <= 0.05.
	OracleBias float64 `json:"oracleBias"`

	// EmptyMarketRate can be set if there is no market data available, and is
	// ignored if there is market data available.
	EmptyMarketRate float64 `json:"manualRate"`
}

// Validate checks that the configurations are valid.
func (cfg *GapEngineCfg) validate() error {
	if cfg.Lots == 0 {
		return errors.New("cannot run with lots = 0")
	}
	if cfg.OracleBias < -0.05 || cfg.OracleBias > 0.05 {
		return fmt.Errorf("bias %f out of bounds", cfg.OracleBias)
	}
	if cfg.OracleWeighting < 0 || cfg.OracleWeighting > 1 {
		return fmt.Errorf("oracle weighting %f out of bounds", cfg.OracleWeighting)
	}
	if cfg.DriftTolerance == 0 {
		cfg.DriftTolerance = 0.001
	}
	if cfg.DriftTolerance < 0 || cfg.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", cfg.DriftTolerance)
	}

	var limits [2]float64
	switch cfg.GapStrategy {
	case GapStrategyMultiplier:
		limits = [2]float64{1, 100}
	case GapStrategyPercent, GapStrategyPercentPlus:
		limits = [2]float64{0, 0.1}
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		limits = [2]float64{0, math.MaxFloat64} // validate at < spot price at creation time
	default:
		return fmt.Errorf("unknown gap strategy %q", cfg.GapStrategy)
	}

	if cfg.GapFactor < limits[0] || cfg.GapFactor > limits[1] {
		return fmt.Errorf("%s gap factor %f is out of bounds %+v", cfg.GapStrategy, cfg.GapFactor, limits)
	}
	return nil
}

// gapEngine is a botEngine that places orders at a certain distance above and
// below a market's "basis price".
// Given an order for L lots, every epoch the makerBot will...
//  1. Calculate a "basis price", which is based on DEX market data,
//     optionally mixed (OracleWeight) with external market data.
//  2. Calculate a "break-even spread". This is the spread at which tx fee
//     losses exactly match profits.
//  3. The break-even spread serves as a hard minimum, and is used to determine
//     the target spread based on the specified gap strategy, giving the target
//     buy and sell prices.
//  4. Scan existing orders to determine if their prices are still valid,
//     within DriftTolerance of the buy or sell price. If not, schedule them
//     for cancellation.
//  5. Calculate how many lots are needed to be ordered in order to meet the
//     2 x L commitment. If low balance restricts the maintenance of L lots on
//     one side, allow the difference in lots to be added to the opposite side.
//  6. Place orders, cancels first, then buys and sells.
type gapEngine struct {
	inputs gapEngineInputs
	log    dex.Logger
	cfgV   atomic.Value
	ctx    context.Context

	rebalanceRunning uint32 // atomic
}

var _ botEngine = (*gapEngine)(nil)

func newGapEngine(inputs gapEngineInputs, cfg *GapEngineCfg, log dex.Logger) (botEngine, error) {
	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	g := &gapEngine{
		inputs: inputs,
		log:    log,
	}
	g.cfgV.Store(cfg)

	return (botEngine)(g), nil
}

func (g *gapEngine) cfg() *GapEngineCfg {
	return g.cfgV.Load().(*GapEngineCfg)
}

func (g *gapEngine) run(ctx context.Context) (*sync.WaitGroup, error) {
	g.ctx = ctx
	return new(sync.WaitGroup), nil
}

func (g *gapEngine) stop() {}

// update updates the gapEngine's configuration.
func (g *gapEngine) update(cfgB []byte) error {
	cfg := new(GapEngineCfg)

	err := json.Unmarshal(cfgB, &cfg)
	if err != nil {
		return fmt.Errorf("failed to unmarshal gap engine config: %w", err)
	}

	err = cfg.validate()
	if err != nil {
		return fmt.Errorf("failed to validate gap engine config: %w", err)
	}

	g.cfgV.Store(cfg)

	return nil
}

// notify is called to let the engine know that the state of the market has
// changed.
func (g *gapEngine) notify(note interface{}) {
	newEpochNote, is := note.(newEpochEngineNote)
	if !is {
		return
	}
	g.rebalance(uint64(newEpochNote))
}

// initialLotsRequired returns the total amount of lots of balance that is
// required to create a bot with this GapEngine.
func (g *gapEngine) initialLotsRequired() uint64 {
	return 2 * g.cfg().Lots
}

// rebalance rebalances the bot's orders.
//  1. Generate a basis price, p, adjusted for oracle weighting and bias.
//  2. Apply the gap strategy to get a target spread, s.
//  3. Check existing orders, if out of bounds
//     [p +/- (s/2) - drift_tolerance, p +/- (s/2) + drift_tolerance],
//     cancel the order
//  4. Compare remaining order counts to configured, lots, and place new
//     orders.
func (g *gapEngine) rebalance(newEpoch uint64) {
	if !atomic.CompareAndSwapUint32(&g.rebalanceRunning, 0, 1) {
		return
	}
	defer atomic.StoreUint32(&g.rebalanceRunning, 0)

	cfg := g.cfg()

	basisPrice := g.inputs.basisPrice(cfg.OracleBias, cfg.OracleWeighting, cfg.EmptyMarketRate)
	if basisPrice == 0 {
		g.log.Errorf("No basis price available and no empty-market rate set")
		return
	}

	g.log.Tracef("rebalance: basis price = %d", basisPrice)

	// Three of the strategies will use a break-even half-gap.
	var breakEven uint64
	switch cfg.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus, GapStrategyMultiplier:
		var err error
		breakEven, err = g.inputs.halfSpread(basisPrice)
		if err != nil {
			g.log.Errorf("Could not calculate break-even spread: %v", err)
			return
		}
	}

	// Apply the base strategy.
	var halfSpread uint64
	switch cfg.GapStrategy {
	case GapStrategyMultiplier:
		halfSpread = uint64(math.Round(float64(breakEven) * cfg.GapFactor))
	case GapStrategyPercent, GapStrategyPercentPlus:
		halfSpread = uint64(math.Round(cfg.GapFactor * float64(basisPrice)))
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		halfSpread = g.inputs.conventionalRateToMsg(cfg.GapFactor)
	}

	// Add the break-even to the "-plus" strategies.
	switch cfg.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus:
		halfSpread += breakEven
	}

	g.log.Tracef("rebalance: strategized half-spread = %d, strategy = %s", halfSpread, cfg.GapStrategy)

	halfSpread = steppedRate(halfSpread, g.inputs.rateStep())

	g.log.Tracef("rebalance: step-resolved half-spread = %d", halfSpread)

	buyPrice := basisPrice - halfSpread
	sellPrice := basisPrice + halfSpread

	g.log.Tracef("rebalance: buy price = %d, sell price = %d", buyPrice, sellPrice)

	buys, sells := g.inputs.sortedOrders()

	// Figure out the best existing sell and buy of existing monitored orders.
	// These values are used to cancel order placement if there is a chance
	// of self-matching, especially against a scheduled cancel order.
	highestBuy, lowestSell := buyPrice, sellPrice
	if len(sells) > 0 {
		ord := sells[0]
		if ord.Rate < lowestSell {
			lowestSell = ord.Rate
		}
	}
	if len(buys) > 0 {
		ord := buys[0]
		if ord.Rate > highestBuy {
			highestBuy = ord.Rate
		}
	}

	// Check if order-placement might self-match.
	var cantBuy, cantSell bool
	if buyPrice >= lowestSell {
		g.log.Tracef("rebalance: can't buy because delayed cancel sell order interferes. booked rate = %d, buy price = %d",
			lowestSell, buyPrice)
		cantBuy = true
	}
	if sellPrice <= highestBuy {
		g.log.Tracef("rebalance: can't sell because delayed cancel sell order interferes. booked rate = %d, sell price = %d",
			highestBuy, sellPrice)
		cantSell = true
	}

	var canceledBuyLots, canceledSellLots uint64 // for stats reporting
	cancels := make([]*Order, 0)
	addCancel := func(ord *Order) {
		if newEpoch-ord.Epoch < 2 {
			g.log.Tracef("rebalance: skipping cancel not past free cancel threshold")
		}

		lotsRemaining := (ord.Qty - ord.Filled) / g.inputs.lotSize()
		if ord.Sell {
			canceledSellLots += lotsRemaining
		} else {
			canceledBuyLots += lotsRemaining
		}
		if ord.Status <= order.OrderStatusBooked {
			cancels = append(cancels, ord)
		}
	}

	processSide := func(ords []*Order, price uint64, sell bool) (keptLots int) {
		tol := uint64(math.Round(float64(price) * cfg.DriftTolerance))
		low, high := price-tol, price+tol

		// Limit large drift tolerances to their respective sides, i.e. mid-gap
		// is a hard cutoff.
		if !sell && high > basisPrice {
			high = basisPrice - 1
		}
		if sell && low < basisPrice {
			low = basisPrice + 1
		}

		for _, ord := range ords {
			lotsRemaining := (ord.Qty - ord.Filled) / g.inputs.lotSize()
			g.log.Tracef("rebalance: processSide: sell = %t, order rate = %d, low = %d, high = %d",
				sell, ord.Rate, low, high)
			if ord.Rate < low || ord.Rate > high {
				if newEpoch < ord.Epoch+2 { // https://github.com/decred/dcrdex/pull/1682
					g.log.Tracef("rebalance: postponing cancellation for order < 2 epochs old")
					keptLots += int(lotsRemaining)
				} else {
					g.log.Tracef("rebalance: cancelling out-of-bounds order (%d lots remaining). rate %d is not in range %d < r < %d",
						lotsRemaining, ord.Rate, low, high)
					addCancel(ord)
				}
			} else {
				keptLots += int(lotsRemaining)
			}
		}
		return
	}

	newBuyLots, newSellLots := int(cfg.Lots), int(cfg.Lots)
	keptBuys := processSide(buys, buyPrice, false)
	keptSells := processSide(sells, sellPrice, true)
	newBuyLots -= keptBuys
	newSellLots -= keptSells

	// Cancel out of bounds or over-stacked orders.
	if len(cancels) > 0 {
		g.log.Tracef("rebalance: cancelling %d orders", len(cancels))
		for _, cancel := range cancels {
			var cancelID order.OrderID
			copy(cancelID[:], cancel.ID)
			err := g.inputs.cancelOrder(cancelID)
			if err != nil {
				g.log.Errorf("failed to cancel order: %v", err)
			}
		}
	}

	if cantBuy {
		newBuyLots = 0
	}
	if cantSell {
		newSellLots = 0
	}

	g.log.Tracef("rebalance: %d buy lots and %d sell lots scheduled after existing valid %d buy and %d sell lots accounted",
		newBuyLots, newSellLots, keptBuys, keptSells)

	// Resolve requested lots against the current balance. If we come up short,
	// we may be able to place extra orders on the other side to satisfy our
	// lot commitment and shift our balance back.
	var maxBuyLots int
	if newBuyLots > 0 {
		// TODO: MaxBuy and MaxSell shouldn't error for insufficient funds, but
		// they do. Maybe consider a constant error asset.InsufficientBalance.
		maxOrder, err := g.inputs.maxBuy(buyPrice)
		if err != nil {
			g.log.Errorf("MaxBuy error: %v", err)
		} else {
			maxBuyLots = int(maxOrder.Swap.Lots)
		}
		if maxBuyLots < newBuyLots {
			// We don't have the balance. Add our shortcoming to the other side.
			shortLots := newBuyLots - maxBuyLots
			newSellLots += shortLots
			newBuyLots = maxBuyLots
			g.log.Tracef("rebalance: reduced buy lots to %d because of low balance", newBuyLots)
		}
	}

	if newSellLots > 0 {
		var maxLots int
		maxOrder, err := g.inputs.maxSell()
		if err != nil {
			g.log.Errorf("MaxSell error: %v", err)
		} else {
			maxLots = int(maxOrder.Swap.Lots)
		}
		if maxLots < newSellLots {
			shortLots := newSellLots - maxLots
			newBuyLots += shortLots
			if newBuyLots > maxBuyLots {
				g.log.Tracef("rebalance: increased buy lot order to %d lots because sell balance is low", newBuyLots)
				newBuyLots = maxBuyLots
			}
			newSellLots = maxLots
			g.log.Tracef("rebalance: reduced sell lots to %d because of low balance", newSellLots)
		}
	}

	// Place buy orders.
	if newBuyLots > 0 {
		_, err := g.inputs.placeOrder(uint64(newBuyLots), buyPrice, false)
		if err != nil {
			g.log.Errorf("failed to place order: %v", err)
		}
	}

	// Place sell orders.
	if newSellLots > 0 {
		_, err := g.inputs.placeOrder(uint64(newSellLots), sellPrice, true)
		if err != nil {
			g.log.Errorf("failed to place order: %v", err)
		}
	}
}
