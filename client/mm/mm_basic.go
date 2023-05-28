// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

const (
	// Our mid-gap rate derived from the local DEX order book is converted to an
	// effective mid-gap that can only vary by up to 3% from the oracle rate.
	// This is to prevent someone from taking advantage of a sparse market to
	// force a bot into giving a favorable price. In reality a market maker on
	// an empty market should use a high oracle bias anyway, but this should
	// prevent catastrophe.
	maxOracleMismatch = 0.03
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

// MarketMakingConfig is the configuration for a simple market
// maker that places orders on both sides of the order book.
type MarketMakingConfig struct {
	// Lots is the number of lots to allocate to each side of the market. This
	// is an ideal allotment, but at any given time, a side could have up to
	// 2 * Lots on order.
	Lots uint64 `json:"lots"`

	// GapStrategy selects an algorithm for calculating the target spread.
	GapStrategy GapStrategy `json:"gapStrategy"`

	// GapFactor controls the gap width in a way determined by the GapStrategy.
	GapFactor float64 `json:"gapFactor"`

	// DriftTolerance is how far away from an ideal price orders can drift
	// before they are replaced (units: ratio of price). Default: 0.1%.
	// 0 <= x <= 0.01.
	DriftTolerance float64 `json:"driftTolerance"`

	// OracleWeighting affects how the target price is derived based on external
	// market data. OracleWeighting, r, determines the target price with the
	// formula:
	//   target_price = dex_mid_gap_price * (1 - r) + oracle_price * r
	// OracleWeighting is limited to 0 <= x <= 1.0.
	// Fetching of price data is disabled if OracleWeighting = 0.
	OracleWeighting *float64 `json:"oracleWeighting"`

	// OracleBias applies a bias in the positive (higher price) or negative
	// (lower price) direction. -0.05 <= x <= 0.05.
	OracleBias float64 `json:"oracleBias"`

	// EmptyMarketRate can be set if there is no market data available, and is
	// ignored if there is market data available.
	EmptyMarketRate float64 `json:"manualRate"`
}

func (c *MarketMakingConfig) Validate() error {
	if c.OracleBias < -0.05 || c.OracleBias > 0.05 {
		return fmt.Errorf("bias %f out of bounds", c.OracleBias)
	}
	if c.OracleWeighting != nil {
		w := *c.OracleWeighting
		if w < 0 || w > 1 {
			return fmt.Errorf("oracle weighting %f out of bounds", w)
		}
	}

	if c.DriftTolerance == 0 {
		c.DriftTolerance = 0.001
	}
	if c.DriftTolerance < 0 || c.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", c.DriftTolerance)
	}

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

	if c.GapFactor < limits[0] || c.GapFactor > limits[1] {
		return fmt.Errorf("%s gap factor %f is out of bounds %+v", c.GapStrategy, c.GapFactor, limits)
	}

	return nil
}

// steppedRate rounds the rate to the nearest integer multiple of the step.
// The minimum returned value is step.
func steppedRate(r, step uint64) uint64 {
	steps := math.Round(float64(r) / float64(step))
	if steps == 0 {
		return step
	}
	return uint64(math.Round(steps * float64(step)))
}

type basicMarketMaker struct {
	ctx    context.Context
	host   string
	base   uint32
	quote  uint32
	cfg    *MarketMakingConfig
	book   *orderbook.OrderBook
	log    dex.Logger
	core   clientCore
	oracle *priceOracle
	mkt    *core.Market
	pw     []byte

	rebalanceRunning atomic.Bool

	ordMtx sync.RWMutex
	ords   map[order.OrderID]*core.Order
}

// sortedOrder is a subset of an *core.Order used internally for sorting.
type sortedOrder struct {
	*core.Order
	id   order.OrderID
	rate uint64
	lots uint64
}

// sortedOrders returns lists of buy and sell orders, with buys sorted
// high to low by rate, and sells low to high.
func (m *basicMarketMaker) sortedOrders() (buys, sells []*sortedOrder) {
	makeSortedOrder := func(o *core.Order) *sortedOrder {
		var oid order.OrderID
		copy(oid[:], o.ID)
		return &sortedOrder{
			Order: o,
			id:    oid,
			rate:  o.Rate,
			lots:  (o.Qty - o.Filled) / m.mkt.LotSize,
		}
	}

	buys, sells = make([]*sortedOrder, 0), make([]*sortedOrder, 0)
	m.ordMtx.RLock()
	for _, ord := range m.ords {
		if ord.Sell {
			sells = append(sells, makeSortedOrder(ord))
		} else {
			buys = append(buys, makeSortedOrder(ord))
		}
	}
	m.ordMtx.RUnlock()

	sort.Slice(buys, func(i, j int) bool { return buys[i].rate > buys[j].rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].rate < sells[j].rate })

	return buys, sells
}

func (m *basicMarketMaker) basisPrice() uint64 {
	midGap, err := m.book.MidGap()
	if err != nil && !errors.Is(err, orderbook.ErrEmptyOrderbook) {
		m.log.Errorf("MidGap error: %v", err)
		return 0
	}

	basisPrice := float64(midGap) // float64 message-rate units

	oraclePrice := m.oracle.getMarketPrice(m.base, m.quote)
	var oracleWeighting float64
	if m.cfg.OracleWeighting != nil {
		oracleWeighting = *m.cfg.OracleWeighting
	}

	if oraclePrice > 0 {
		msgOracleRate := float64(m.mkt.ConventionalRateToMsg(oraclePrice))

		// Apply the oracle mismatch filter.
		if basisPrice > 0 {
			low, high := msgOracleRate*(1-maxOracleMismatch), msgOracleRate*(1+maxOracleMismatch)
			if basisPrice < low {
				m.log.Debug("local mid-gap is below safe range. Using effective mid-gap of %d%% below the oracle rate.", maxOracleMismatch*100)
				basisPrice = low
			} else if basisPrice > high {
				m.log.Debug("local mid-gap is above safe range. Using effective mid-gap of %d%% above the oracle rate.", maxOracleMismatch*100)
				basisPrice = high
			}
		}

		if m.cfg.OracleBias != 0 {
			msgOracleRate *= 1 + m.cfg.OracleBias
		}

		if basisPrice == 0 { // no mid-gap available. Use the oracle price.
			basisPrice = msgOracleRate
			m.log.Tracef("basisPrice: using basis price %.0f from oracle because no mid-gap was found in order book", basisPrice)
		} else if oracleWeighting > 0 {
			basisPrice = msgOracleRate*oracleWeighting + basisPrice*(1-oracleWeighting)
			m.log.Tracef("basisPrice: oracle-weighted basis price = %f", basisPrice)
		}
	} else {
		m.log.Warnf("no oracle price available for %s bot", m.mkt.Name)
	}

	if basisPrice > 0 {
		return steppedRate(uint64(basisPrice), m.mkt.RateStep)
	}

	// TODO: infer basis price from fiat rates?
	return 0
}

func (m *basicMarketMaker) halfSpread(basisPrice uint64) (uint64, error) {
	form := &core.SingleLotFeesForm{
		Host:    m.host,
		Base:    m.base,
		Quote:   m.quote,
		Sell:    true,
		Options: nil, // Probably will need to support split tx
	}

	if basisPrice == 0 { // prevent divide by zero later
		return 0, fmt.Errorf("basis price cannot be zero")
	}

	baseFees, quoteFees, err := m.core.SingleLotFees(form)
	if err != nil {
		return 0, fmt.Errorf("SingleLotFees error: %v", err)
	}

	form.Sell = false
	newQuoteFees, newBaseFees, err := m.core.SingleLotFees(form)
	if err != nil {
		return 0, fmt.Errorf("SingleLotFees error: %v", err)
	}

	baseFees += newBaseFees
	quoteFees += newQuoteFees

	g := float64(calc.BaseToQuote(basisPrice, baseFees)+quoteFees) /
		float64(baseFees+2*m.mkt.LotSize)

	halfGap := uint64(math.Round(g * calc.RateEncodingFactor))

	m.log.Tracef("halfSpread: base basis price = %d, lot size = %d, base fees = %d, quote fees = %d, half-gap = %d",
		basisPrice, m.mkt.LotSize, baseFees, quoteFees, halfGap)

	return halfGap, nil
}

func (m *basicMarketMaker) placeOrder(lots, rate uint64, sell bool) *core.Order {
	ord, err := m.core.Trade(m.pw, &core.TradeForm{
		Host:    m.host,
		IsLimit: true,
		Sell:    sell,
		Base:    m.base,
		Quote:   m.quote,
		Qty:     lots * m.mkt.LotSize,
		Rate:    rate,
	})
	if err != nil {
		m.log.Errorf("Error placing rebalancing order: %v", err)
		return nil
	}
	return ord
}

func (m *basicMarketMaker) processTrade(o *core.Order) {
	if len(o.ID) == 0 {
		return
	}

	var oid order.OrderID
	copy(oid[:], o.ID)

	m.log.Tracef("processTrade: oid = %s, status = %s", oid, o.Status)

	m.ordMtx.Lock()
	defer m.ordMtx.Unlock()
	_, found := m.ords[oid]
	if !found {
		return
	}

	convRate := m.mkt.MsgRateToConventional(o.Rate)
	m.log.Tracef("processTrade: oid = %s, status = %s, qty = %d, filled = %d, rate = %f", oid, o.Status, o.Qty, o.Filled, convRate)

	if o.Status > order.OrderStatusBooked {
		// We stop caring when the order is taken off the book.
		delete(m.ords, oid)

		switch {
		case o.Filled == o.Qty:
			m.log.Tracef("processTrade: order filled")
		case o.Status == order.OrderStatusCanceled:
			if len(o.Matches) == 0 {
				m.log.Tracef("processTrade: order canceled WITHOUT matches")
			} else {
				m.log.Tracef("processTrade: order canceled WITH matches")
			}
		}
		return
	} else {
		// Update our reference.
		m.ords[oid] = o
	}
}

func (m *basicMarketMaker) rebalance(newEpoch uint64) {
	if !m.rebalanceRunning.CompareAndSwap(false, true) {
		return
	}
	defer m.rebalanceRunning.Store(false)

	basisPrice := m.basisPrice()
	if basisPrice == 0 && m.cfg.EmptyMarketRate == 0 {
		m.log.Errorf("No basis price available and no empty-market rate set")
	} else if basisPrice == 0 {
		basisPrice = m.mkt.ConventionalRateToMsg(m.cfg.EmptyMarketRate)
	}

	m.log.Tracef("rebalance (%s): basis price = %d", m.mkt.Name, basisPrice)

	// Three of the strategies will use a break-even half-gap.
	var breakEven uint64
	switch m.cfg.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus, GapStrategyMultiplier:
		var err error
		breakEven, err = m.halfSpread(basisPrice)
		if err != nil {
			m.log.Errorf("Could not calculate break-even spread: %v", err)
			return
		}
	}

	// Apply the base strategy.
	var halfSpread uint64
	switch m.cfg.GapStrategy {
	case GapStrategyMultiplier:
		halfSpread = uint64(math.Round(float64(breakEven) * m.cfg.GapFactor))
	case GapStrategyPercent, GapStrategyPercentPlus:
		halfSpread = uint64(math.Round(m.cfg.GapFactor * float64(basisPrice)))
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		halfSpread = m.mkt.ConventionalRateToMsg(m.cfg.GapFactor)
	}

	// Add the break-even to the "-plus" strategies.
	switch m.cfg.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus:
		halfSpread += breakEven
	}

	m.log.Tracef("rebalance: strategized half-spread = %d, strategy = %s", halfSpread, m.cfg.GapStrategy)

	halfSpread = steppedRate(halfSpread, m.mkt.RateStep)

	m.log.Tracef("rebalance: stepped half-spread = %d", halfSpread)

	buyPrice := basisPrice - halfSpread
	sellPrice := basisPrice + halfSpread

	m.log.Tracef("rebalance: buy price = %d, sell price = %d", buyPrice, sellPrice)

	buys, sells := m.sortedOrders()

	// Figure out the best existing sell and buy of existing monitored orders.
	// These values are used to cancel order placement if there is a chance
	// of self-matching, especially against a scheduled cancel order.
	highestBuy, lowestSell := buyPrice, sellPrice
	if len(sells) > 0 {
		ord := sells[0]
		if ord.rate < lowestSell {
			lowestSell = ord.rate
		}
	}
	if len(buys) > 0 {
		ord := buys[0]
		if ord.rate > highestBuy {
			highestBuy = ord.rate
		}
	}

	// Check if order-placement might self-match.
	var cantBuy, cantSell bool
	if buyPrice >= lowestSell {
		m.log.Tracef("rebalance: can't buy because delayed cancel sell order interferes. booked rate = %d, buy price = %d",
			lowestSell, buyPrice)
		cantBuy = true
	}
	if sellPrice <= highestBuy {
		m.log.Tracef("rebalance: can't sell because delayed cancel sell order interferes. booked rate = %d, sell price = %d",
			highestBuy, sellPrice)
		cantSell = true
	}

	var canceledBuyLots, canceledSellLots uint64 // for stats reporting
	cancels := make([]*sortedOrder, 0)
	addCancel := func(ord *sortedOrder) {
		if newEpoch-ord.Epoch < 2 {
			m.log.Debugf("rebalance: skipping cancel not past free cancel threshold")
		}

		if ord.Sell {
			canceledSellLots += ord.lots
		} else {
			canceledBuyLots += ord.lots
		}
		if ord.Status <= order.OrderStatusBooked {
			cancels = append(cancels, ord)
		}
	}

	processSide := func(ords []*sortedOrder, price uint64, sell bool) (keptLots int) {
		tol := uint64(math.Round(float64(price) * m.cfg.DriftTolerance))
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
			m.log.Tracef("rebalance: processSide: sell = %t, order rate = %d, low = %d, high = %d",
				sell, ord.rate, low, high)
			if ord.rate < low || ord.rate > high {
				if newEpoch < ord.Epoch+2 { // https://github.com/decred/dcrdex/pull/1682
					m.log.Tracef("rebalance: postponing cancellation for order < 2 epochs old")
					keptLots += int(ord.lots)
				} else {
					m.log.Tracef("rebalance: cancelling out-of-bounds order (%d lots remaining). rate %d is not in range %d < r < %d",
						ord.lots, ord.rate, low, high)
					addCancel(ord)
				}
			} else {
				keptLots += int(ord.lots)
			}
		}
		return
	}

	newBuyLots, newSellLots := int(m.cfg.Lots), int(m.cfg.Lots)
	keptBuys := processSide(buys, buyPrice, false)
	keptSells := processSide(sells, sellPrice, true)
	newBuyLots -= keptBuys
	newSellLots -= keptSells

	// Cancel out of bounds or over-stacked orders.
	if len(cancels) > 0 {
		// Only cancel orders that are > 1 epoch old.
		m.log.Tracef("rebalance: cancelling %d orders", len(cancels))
		for _, cancel := range cancels {
			if err := m.core.Cancel(cancel.id[:]); err != nil {
				m.log.Errorf("error cancelling order: %v", err)
				return
			}
		}
	}

	if cantBuy {
		newBuyLots = 0
	}
	if cantSell {
		newSellLots = 0
	}

	m.log.Tracef("rebalance: %d buy lots and %d sell lots scheduled after existing valid %d buy and %d sell lots accounted",
		newBuyLots, newSellLots, keptBuys, keptSells)

	// Resolve requested lots against the current balance. If we come up short,
	// we may be able to place extra orders on the other side to satisfy our
	// lot commitment and shift our balance back.
	var maxBuyLots int
	if newBuyLots > 0 {
		// TODO: MaxBuy and MaxSell shouldn't error for insufficient funds, but
		// they do. Maybe consider a constant error asset.InsufficientBalance.
		maxOrder, err := m.core.MaxBuy(m.host, m.base, m.quote, buyPrice)
		if err != nil {
			m.log.Tracef("MaxBuy error: %v", err)
		} else {
			maxBuyLots = int(maxOrder.Swap.Lots)
		}
		if maxBuyLots < newBuyLots {
			// We don't have the balance. Add our shortcoming to the other side.
			shortLots := newBuyLots - maxBuyLots
			newSellLots += shortLots
			newBuyLots = maxBuyLots
			m.log.Tracef("rebalance: reduced buy lots to %d because of low balance", newBuyLots)
		}
	}

	if newSellLots > 0 {
		var maxLots int
		maxOrder, err := m.core.MaxSell(m.host, m.base, m.quote)
		if err != nil {
			m.log.Tracef("MaxSell error: %v", err)
		} else {
			maxLots = int(maxOrder.Swap.Lots)
		}
		if maxLots < newSellLots {
			shortLots := newSellLots - maxLots
			newBuyLots += shortLots
			if newBuyLots > maxBuyLots {
				m.log.Tracef("rebalance: increased buy lot order to %d lots because sell balance is low", newBuyLots)
				newBuyLots = maxBuyLots
			}
			newSellLots = maxLots
			m.log.Tracef("rebalance: reduced sell lots to %d because of low balance", newSellLots)
		}
	}

	// Place buy orders.
	if newBuyLots > 0 {
		ord := m.placeOrder(uint64(newBuyLots), buyPrice, false)
		if ord != nil {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			m.ordMtx.Lock()
			m.ords[oid] = ord
			m.ordMtx.Unlock()
		}
	}

	// Place sell orders.
	if newSellLots > 0 {
		ord := m.placeOrder(uint64(newSellLots), sellPrice, true)
		if ord != nil {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			m.ordMtx.Lock()
			m.ords[oid] = ord
			m.ordMtx.Unlock()
		}
	}

}

func (m *basicMarketMaker) handleNotification(note core.Notification) {
	switch n := note.(type) {
	case *core.OrderNote:
		ord := n.Order
		if ord == nil {
			return
		}
		m.processTrade(ord)
	case *core.EpochNotification:
		go m.rebalance(n.Epoch)
	}
}

func (m *basicMarketMaker) cancelAllOrders() {
	m.ordMtx.Lock()
	defer m.ordMtx.Unlock()
	for oid := range m.ords {
		if err := m.core.Cancel(oid[:]); err != nil {
			m.log.Errorf("error cancelling order: %v", err)
		}
	}
	m.ords = make(map[order.OrderID]*core.Order)
}

func (m *basicMarketMaker) run() {
	book, bookFeed, err := m.core.SyncBook(m.host, m.base, m.quote)
	if err != nil {
		m.log.Errorf("Failed to sync book: %v", err)
		return
	}
	m.book = book

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-bookFeed.Next():
				// Really nothing to do with the updates. We just need to keep
				// the subscription live in order to get a mid-gap rate when
				// needed. We could use this to trigger rebalances mid-epoch
				// though, which I think would provide some advantage.
			case <-m.ctx.Done():
				return
			}
		}
	}()

	noteFeed := m.core.NotificationFeed()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer noteFeed.ReturnFeed()
		for {
			select {
			case n := <-noteFeed.C:
				m.handleNotification(n)
			case <-m.ctx.Done():
				return
			}
		}
	}()

	wg.Wait()

	m.cancelAllOrders()
}

// RunBasicMarketMaker starts a basic market maker bot.
func RunBasicMarketMaker(ctx context.Context, cfg *BotConfig, c clientCore, oracle *priceOracle, pw []byte, log dex.Logger) {
	if cfg.MMCfg == nil {
		// implies bug in caller
		log.Errorf("No market making config provided. Exiting.")
		return
	}

	mkt, err := c.ExchangeMarket(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
	if err != nil {
		log.Errorf("Failed to get market: %v", err)
		return
	}

	(&basicMarketMaker{
		ctx:    ctx,
		core:   c,
		log:    log,
		cfg:    cfg.MMCfg,
		host:   cfg.Host,
		base:   cfg.BaseAsset,
		quote:  cfg.QuoteAsset,
		oracle: oracle,
		pw:     pw,
		mkt:    mkt,
		ords:   make(map[order.OrderID]*core.Order),
	}).run()
}
