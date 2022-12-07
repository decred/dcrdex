// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
)

const (
	// MakerBotV0 is the bot specifier associated with makerBot. The bot
	// specifier is used to determine how to decode stored program data.
	MakerBotV0 = "MakerV0"

	oraclePriceExpiration = time.Minute * 10
	oracleRecheckInterval = time.Minute * 3

	ErrNoMarkets           = dex.ErrorKind("no markets")
	defaultOracleWeighting = 0.2

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

// MakerProgram is the program for a makerBot.
type MakerProgram struct {
	Host    string `json:"host"`
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`

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
	OracleWeighting *float64 `json:"oracleWeighting"`

	// OracleBias applies a bias in the positive (higher price) or negative
	// (lower price) direction. -0.05 <= x <= 0.05.
	OracleBias float64 `json:"oracleBias"`

	// EmptyMarketRate can be set if there is no market data available, and is
	// ignored if there is market data available.
	EmptyMarketRate float64 `json:"manualRate"`
}

// validateProgram checks the sensibility of a *MakerProgram's values and sets
// some defaults.
func validateProgram(pgm *MakerProgram) error {
	if pgm.Host == "" {
		return errors.New("no host specified")
	}
	if dex.BipIDSymbol(pgm.BaseID) == "" {
		return fmt.Errorf("base asset %d unknown", pgm.BaseID)
	}
	if dex.BipIDSymbol(pgm.QuoteID) == "" {
		return fmt.Errorf("quote asset %d unknown", pgm.QuoteID)
	}
	if pgm.Lots == 0 {
		return errors.New("cannot run with lots = 0")
	}
	if pgm.OracleBias < -0.05 || pgm.OracleBias > 0.05 {
		return fmt.Errorf("bias %f out of bounds", pgm.OracleBias)
	}
	if pgm.OracleWeighting != nil {
		w := *pgm.OracleWeighting
		if w < 0 || w > 1 {
			return fmt.Errorf("oracle weighting %f out of bounds", w)
		}
	}

	if pgm.DriftTolerance == 0 {
		pgm.DriftTolerance = 0.001
	}
	if pgm.DriftTolerance < 0 || pgm.DriftTolerance > 0.01 {
		return fmt.Errorf("drift tolerance %f out of bounds", pgm.DriftTolerance)
	}

	var limits [2]float64
	switch pgm.GapStrategy {
	case GapStrategyMultiplier:
		limits = [2]float64{1, 100}
	case GapStrategyPercent, GapStrategyPercentPlus:
		limits = [2]float64{0, 0.1}
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		limits = [2]float64{0, math.MaxFloat64} // validate at < spot price at creation time
	default:
		return fmt.Errorf("unknown gap strategy %q", pgm.GapStrategy)
	}

	if pgm.GapFactor < limits[0] || pgm.GapFactor > limits[1] {
		return fmt.Errorf("%s gap factor %f is out of bounds %+v", pgm.GapStrategy, pgm.GapFactor, limits)
	}
	return nil
}

// oracleWeighting returns the specified OracleWeighting, or the default if
// not set.
func (pgm *MakerProgram) oracleWeighting() float64 {
	if pgm.OracleWeighting == nil {
		return defaultOracleWeighting
	}
	return *pgm.OracleWeighting
}

// makerAsset combines a *dex.Asset with a WalletState.
type makerAsset struct {
	*SupportedAsset
	// walletV  atomic.Value // *WalletState
	balanceV atomic.Value // *WalletBalance

}

// makerBot is a *Core extension that enables operation of a market-maker bot.
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
type makerBot struct {
	*Core
	pgmID uint64
	base  *makerAsset
	quote *makerAsset
	// TODO: enable updating of market, or just grab it live when needed.
	market *Market
	log    dex.Logger
	book   *orderbook.OrderBook

	running uint32 // atomic
	wg      sync.WaitGroup
	die     context.CancelFunc

	rebalanceRunning uint32 // atomic

	programV atomic.Value // *MakerProgram

	ordMtx sync.RWMutex
	ords   map[order.OrderID]*Order

	oracleRunning uint32 // atomic
}

func createMakerBot(ctx context.Context, c *Core, pgm *MakerProgram) (*makerBot, error) {
	if err := validateProgram(pgm); err != nil {
		return nil, err
	}
	dc, err := c.connectedDEX(pgm.Host)
	if err != nil {
		return nil, err
	}
	xcInfo := dc.exchangeInfo()
	supportedAssets := c.assetMap()

	if _, ok := supportedAssets[pgm.BaseID]; !ok {
		return nil, fmt.Errorf("base asset %d (%s) not supported", pgm.BaseID, unbip(pgm.BaseID))
	}
	if _, ok := supportedAssets[pgm.QuoteID]; !ok {
		return nil, fmt.Errorf("quote asset %d (%s) not supported", pgm.QuoteID, unbip(pgm.QuoteID))
	}

	baseWallet := c.WalletState(pgm.BaseID)
	if baseWallet == nil {
		return nil, fmt.Errorf("no wallet found for base asset %d -> %s", pgm.BaseID, unbip(pgm.BaseID))
	}
	quoteWallet := c.WalletState(pgm.QuoteID)
	if quoteWallet == nil {
		return nil, fmt.Errorf("no wallet found for quote asset %d -> %s", pgm.QuoteID, unbip(pgm.QuoteID))
	}

	mktName := marketName(pgm.BaseID, pgm.QuoteID)
	mkt := xcInfo.Markets[mktName]
	if mkt == nil {
		return nil, fmt.Errorf("market %s not known at %s", mktName, pgm.Host)
	}

	base := &makerAsset{
		SupportedAsset: supportedAssets[pgm.BaseID],
	}
	base.balanceV.Store(baseWallet.Balance)

	quote := &makerAsset{
		SupportedAsset: supportedAssets[pgm.QuoteID],
	}
	quote.balanceV.Store(quoteWallet.Balance)

	m := &makerBot{
		Core:   c,
		base:   base,
		quote:  quote,
		market: mkt,
		// oracle:    oracle,
		log:  c.log.SubLogger("BOT").SubLogger(marketName(pgm.BaseID, pgm.QuoteID)),
		ords: make(map[order.OrderID]*Order),
	}
	m.programV.Store(pgm)

	// Get a rate now, because max buy won't have an oracle fallback.
	var rate uint64
	if mkt.SpotPrice != nil {
		rate = mkt.SpotPrice.Rate
	}
	if rate == 0 {
		price, err := m.syncOraclePrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to establish a starting price: %v", err)
		}
		rate = mkt.ConventionalRateToMsg(price)
	}

	var maxBuyLots, maxSellLots uint64
	maxBuy, err := c.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, rate)
	if err == nil {
		maxBuyLots = maxBuy.Swap.Lots
	} else {
		m.log.Errorf("Bot MaxBuy error: %v", err)
	}
	maxSell, err := c.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
	if err == nil {
		maxSellLots = maxSell.Swap.Lots
	} else {
		m.log.Errorf("Bot MaxSell error: %v", err)
	}

	if maxBuyLots+maxSellLots < pgm.Lots*2 {
		return nil, fmt.Errorf("cannot create bot with %d lots. 2 x %d = %d lots total balance required to start, "+
			"and only %d %s lots and %d %s lots = %d total lots are available", pgm.Lots, pgm.Lots, pgm.Lots*2,
			maxBuyLots, unbip(pgm.QuoteID), maxSellLots, unbip(pgm.BaseID), maxSellLots+maxBuyLots)
	}

	// We don't use the dynamic data from *Market, just the configuration.
	mkt.Orders = nil
	mkt.SpotPrice = nil

	return m, nil
}

// newMakerBot constructs a new makerBot with a new program ID. To resurrect an
// existing bot, use recreateMakerBot.
func newMakerBot(ctx context.Context, c *Core, pgm *MakerProgram) (*makerBot, error) {
	m, err := createMakerBot(ctx, c, pgm)
	if err != nil {
		return nil, err
	}
	m.pgmID, err = m.saveProgram()
	if err != nil {
		return nil, fmt.Errorf("Error saving bot program: %v", err)
	}

	c.notify(newBotNote(TopicBotCreated, "", "", db.Data, m.report()))

	return m, nil
}

// recreateMakerBot creates a new makerBot and assigns the specified program ID.
func recreateMakerBot(ctx context.Context, c *Core, pgmID uint64, pgm *MakerProgram) (*makerBot, error) {
	m, err := createMakerBot(ctx, c, pgm)
	if err != nil {
		return nil, err
	}
	m.pgmID = pgmID

	return m, nil
}

func (m *makerBot) program() *MakerProgram {
	return m.programV.Load().(*MakerProgram)
}

// liverOrderIDs returns a list of order IDs of all orders created and
// currently monitored by the makerBot.
func (m *makerBot) liveOrderIDs() []order.OrderID {
	m.ordMtx.RLock()
	oids := make([]order.OrderID, 0, len(m.ords))
	for oid, ord := range m.ords {
		if ord.Status <= order.OrderStatusBooked {
			oids = append(oids, oid)
		}
	}
	m.ordMtx.RUnlock()
	return oids
}

// retire retires the makerBot, cancelling all orders and marking retired in
// the database.
func (m *makerBot) retire() {
	m.stop()
	if err := m.db.RetireBotProgram(m.pgmID); err != nil {
		m.log.Errorf("error retiring bot program")
		return
	}
	m.notify(newBotNote(TopicBotRetired, "", "", db.Data, m.report()))
}

// stop stops the bot and cancels all live orders. Note that stop is not called
// on *Core shutdown, so existing bot orders remain live if shutdown is forced.
func (m *makerBot) stop() {
	if m.die != nil {
		m.die()
		m.wg.Wait()
	}
	for _, oid := range m.liveOrderIDs() {
		if err := m.cancelOrder(oid); err != nil {
			m.log.Errorf("error cancelling order %s while stopping bot %d: %v", oid, m.pgmID, err)
		}
	}

	// TODO: Should really wait until the cancels match. If we're cancelling
	// orders placed in the same epoch, they might miss and need to be replaced.
}

func (m *makerBot) run(ctx context.Context) {
	if !atomic.CompareAndSwapUint32(&m.running, 0, 1) {
		m.log.Errorf("run called while makerBot already running")
		return
	}
	defer func() {
		atomic.StoreUint32(&m.running, 0)
		m.notify(newBotNote(TopicBotStopped, "", "", db.Data, m.report()))
	}()

	ctx, m.die = context.WithCancel(ctx)
	defer m.die()

	pgm := m.program()

	book, bookFeed, err := m.syncBook(pgm.Host, pgm.BaseID, pgm.QuoteID)
	if err != nil {
		m.log.Errorf("Error establishing book feed: %v", err)
		return
	}
	m.book = book

	if pgm.OracleWeighting != nil {
		m.startOracleSync(ctx)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer bookFeed.Close()
		for {
			select {
			case <-bookFeed.Next():
				// Really nothing to do with the updates. We just need to keep
				// the subscription live in order to get a mid-gap rate when
				// needed. We could use this to trigger rebalances mid-epoch
				// though, which I think would provide some advantage.
			case <-ctx.Done():
				return
			}
		}
	}()

	cid, notes := m.notificationFeed()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.returnFeed(cid)
		for {
			select {
			case n := <-notes:
				m.handleNote(ctx, n)
			case <-ctx.Done():
				return
			}
		}
	}()

	m.notify(newBotNote(TopicBotStarted, "", "", db.Data, m.report()))

	m.wg.Wait()
}

// botOrders is a list of orders created and monitored by the makerBot.
func (m *makerBot) botOrders() []*BotOrder {
	m.ordMtx.RLock()
	defer m.ordMtx.RUnlock()
	ords := make([]*BotOrder, 0, len(m.ords))
	for _, ord := range m.ords {
		ords = append(ords, &BotOrder{
			Host:     ord.Host,
			MarketID: ord.MarketID,
			OrderID:  ord.ID,
			Status:   ord.Status,
		})
	}
	return ords
}

// report generates a BotReport for the makerBot.
func (m *makerBot) report() *BotReport {
	return &BotReport{
		ProgramID: m.pgmID,
		Program:   m.program(),
		Running:   atomic.LoadUint32(&m.running) == 1,
		Orders:    m.botOrders(),
	}
}

// makerProgramDBRecord converts a *MakerProgram to a *db.BotProgram for a
// makerBot.
func makerProgramDBRecord(pgm *MakerProgram) (*db.BotProgram, error) {
	pgmB, err := json.Marshal(pgm)
	if err != nil {
		return nil, err
	}
	return &db.BotProgram{
		Type:    MakerBotV0,
		Program: pgmB,
	}, nil
}

// saveProgram saves a new bot program, returning the program ID.
func (m *makerBot) saveProgram() (uint64, error) {
	dbRecord, err := makerProgramDBRecord(m.program())
	if err != nil {
		return 0, err
	}
	return m.db.SaveBotProgram(dbRecord)
}

// updateProgram updates the current bot program. The applied changes will
// be in effect for the next rebalance.
func (m *makerBot) updateProgram(pgm *MakerProgram) error {
	dbRecord, err := makerProgramDBRecord(pgm)
	if err != nil {
		return err
	}

	if err := m.db.UpdateBotProgram(m.pgmID, dbRecord); err != nil {
		return err
	}

	if pgm.oracleWeighting() > 0 {
		m.startOracleSync(m.ctx) // no-op if sync is already running
	}

	m.programV.Store(pgm)
	m.notify(newBotNote(TopicBotUpdated, "", "", db.Data, m.report()))
	return nil
}

// syncOraclePrice retrieves price data for the makerBot's market and returns
// the volume-weighted average price.
func (m *makerBot) syncOraclePrice(ctx context.Context) (float64, error) {
	_, price, err := m.Core.marketReport(ctx, m.base.SupportedAsset, m.quote.SupportedAsset)
	return price, err
}

// cachedOraclePrice gets the cached price data for the specified market.
// Expired data will not be returned.
func (c *Core) cachedOraclePrice(mktName string) *stampedPrice {
	c.mm.cache.RLock()
	defer c.mm.cache.RUnlock()
	p := c.mm.cache.prices[mktName]
	if p == nil {
		return nil
	}
	if time.Since(p.stamp) > oraclePriceExpiration {
		return nil
	}
	return p
}

// startOracleSync starts the syncOracle loop.
func (m *makerBot) startOracleSync(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		m.syncOracle(ctx)
		m.wg.Done()
	}()
}

// syncOracle keeps the oracle price data updated by querying the data on
// a regular interval.
// TODO: We should only run one syncOracle per market pair, not per bot or per
// market.
func (m *makerBot) syncOracle(ctx context.Context) {
	if !atomic.CompareAndSwapUint32(&m.oracleRunning, 0, 1) {
		m.log.Errorf("syncOracle called while already running")
		return
	}
	defer atomic.StoreUint32(&m.oracleRunning, 0)

	var lastCheck time.Time
	m.mm.cache.RLock()
	if p := m.mm.cache.prices[m.market.Name]; p != nil {
		lastCheck = p.stamp
	}
	m.mm.cache.RUnlock()

	// If we don't already have a price, sync now.
	if time.Since(lastCheck) > time.Minute {
		if _, err := m.syncOraclePrice(ctx); err != nil {
			m.log.Errorf("failed to establish a market-averaged price: %v", err)
		}
	}

	ticker := time.NewTicker(oracleRecheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
		if _, err := m.syncOraclePrice(ctx); err != nil {
			m.log.Errorf("failed to get market-averaged price: %v", err)
		}
	}
}

// basisPrice is the basis price for the makerBot's market.
func (m *makerBot) basisPrice() uint64 {
	pgm := m.program()
	midGap, err := m.book.MidGap()
	if err != nil && !errors.Is(err, orderbook.ErrEmptyOrderbook) {
		m.log.Errorf("error calculating mid-gap: %w", err)
		return 0
	}
	if p := basisPrice(pgm.Host, m.market, pgm.OracleBias, pgm.oracleWeighting(), midGap, m, m.log); p > 0 {
		return p
	}
	return m.market.ConventionalRateToMsg(pgm.EmptyMarketRate)
}

type basisPricer interface {
	cachedOraclePrice(mktName string) *stampedPrice
	fiatConversions() map[uint32]float64
}

// basisPrice is the oracle-weighted price on which to base the target price and
// spread. The oracle weighting and bias is applied, but no other adjustments.
func basisPrice(host string, mkt *Market, oracleBias, oracleWeighting float64, midGap uint64, bp basisPricer, log dex.Logger) uint64 {
	basisPrice := float64(midGap) // float64 message-rate units

	log.Tracef("basisPrice: mid-gap price = %d", midGap)

	// Our sync loop should be running. Only check the cache.
	p := bp.cachedOraclePrice(mkt.Name)

	if p != nil && p.price != 0 {
		log.Tracef("basisPrice: raw oracle price = %.8f", p.price)

		msgOracleRate := float64(mkt.ConventionalRateToMsg(p.price))

		// Apply the oracle mismatch filter.
		if basisPrice > 0 {
			low, high := msgOracleRate*(1-maxOracleMismatch), msgOracleRate*(1+maxOracleMismatch)
			if basisPrice < low {
				log.Debug("local mid-gap is below safe range. Using effective mid-gap of %d%% below the oracle rate.", maxOracleMismatch*100)
				basisPrice = low
			} else if basisPrice > high {
				log.Debug("local mid-gap is above safe range. Using effective mid-gap of %d%% above the oracle rate.", maxOracleMismatch*100)
				basisPrice = high
			}
		}

		if oracleBias != 0 {
			msgOracleRate *= 1 + oracleBias

			log.Tracef("basisPrice: biased oracle price = %.0f", msgOracleRate)
		}

		if basisPrice == 0 { // no mid-gap available. Just use the oracle value straight.
			basisPrice = msgOracleRate
			log.Tracef("basisPrice: using basis price %.0f from oracle because no mid-gap was found in order book", basisPrice)
		} else if oracleWeighting > 0 {
			basisPrice = msgOracleRate*oracleWeighting + basisPrice*(1-oracleWeighting)
			log.Tracef("basisPrice: oracle-weighted basis price = %f", basisPrice)
		}
	} else {
		log.Warnf("no oracle price available for %s bot", mkt.Name)
	}

	if basisPrice > 0 {
		return steppedRate(uint64(basisPrice), mkt.RateStep)
	}

	// If we're still unable to resolve a mid-gap price, infer it from the
	// fiat rates.
	log.Infof("Inferring exchange rate from fiat data.")
	fiatRates := bp.fiatConversions()
	baseRate, found := fiatRates[mkt.BaseID]
	if !found || baseRate == 0 {
		return 0
	}
	quoteRate, found := fiatRates[mkt.QuoteID]
	if !found || quoteRate == 0 {
		return 0
	}
	convRate := baseRate / quoteRate // ($ / DCR) / ($ / BTC) => BTC / DCR
	basisPrice = float64(mkt.ConventionalRateToMsg(convRate))

	return steppedRate(uint64(basisPrice), mkt.RateStep)
}

// marketReport fetches current oracle market data and a volume-weighted average
// mid-gap rate for a market. The data is cached, so will be available via
// (*Core).cachedOraclePrice until expiration.
func (c *Core) marketReport(ctx context.Context, b, q *SupportedAsset) ([]*OracleReport, float64, error) {
	oracles, err := oracleMarketReport(ctx, b, q, c.log)
	if err != nil {
		return nil, 0, err
	}
	price, err := oracleAverage(oracles, c.log)
	if err != nil && !errors.Is(err, ErrNoMarkets) {
		return nil, 0, err
	}
	// If ErrNoMarkets, cache the zero price and empty oracles anyway, to prevent
	// hitting coinpaprika too often.
	c.mm.cache.Lock()
	c.mm.cache.prices[marketName(b.ID, q.ID)] = &stampedPrice{
		stamp:   time.Now(),
		price:   price,
		oracles: oracles,
	}
	c.mm.cache.Unlock()
	return oracles, price, nil
}

// oracleAverage averages the oracle market data into a volume-weighted average
// price.
func oracleAverage(mkts []*OracleReport, log dex.Logger) (float64, error) {
	var weightedSum, usdVolume float64
	var n int
	for _, mkt := range mkts {
		n++
		weightedSum += mkt.USDVol * (mkt.BestBuy + mkt.BestSell) / 2
		usdVolume += mkt.USDVol
	}
	if usdVolume == 0 {
		log.Tracef("marketAveragedPrice: no markets")
		return 0, ErrNoMarkets
	}

	rate := weightedSum / usdVolume
	// TODO: Require a minimum USD volume?
	log.Tracef("marketAveragedPrice: price calculated from %d markets: rate = %f, USD volume = %f", n, rate, usdVolume)
	return rate, nil
}

// oracleMarketReport fetches oracle price, spread, and volume data for known
// exchanges for a market. This is done by fetching the market data from
// coinpaprika, looking for known exchanges in the results, then pulling the
// data directly from the exchange's public data API.
func oracleMarketReport(ctx context.Context, b, q *SupportedAsset, log dex.Logger) (oracles []*OracleReport, err error) {
	// They're going to return the quote prices in terms of USD, which is
	// sort of nonsense for a non-USD market like DCR-BTC.
	baseSlug := coinpapSlug(b.Symbol, b.Name)
	quoteSlug := coinpapSlug(q.Symbol, q.Name)

	type coinpapQuote struct {
		Price  float64 `json:"price"`
		Volume float64 `json:"volume_24h"`
	}

	type coinpapMarket struct {
		BaseCurrencyID  string                   `json:"base_currency_id"`
		QuoteCurrencyID string                   `json:"quote_currency_id"`
		MarketURL       string                   `json:"market_url"`
		LastUpdated     time.Time                `json:"last_updated"`
		TrustScore      string                   `json:"trust_score"`
		Quotes          map[string]*coinpapQuote `json:"quotes"`
	}

	// We use a cache for the market data in case there is more than one bot
	// running on the same market.
	var rawMarkets []*coinpapMarket
	url := fmt.Sprintf("https://api.coinpaprika.com/v1/coins/%s/markets", baseSlug)
	if err := getRates(ctx, url, &rawMarkets); err != nil {
		return nil, err
	}

	// Create filter for desireable matches.
	marketMatches := func(mkt *coinpapMarket) bool {
		if mkt.TrustScore != "high" {
			return false
		}
		if time.Since(mkt.LastUpdated) > time.Minute*30 {
			return false
		}
		return (mkt.BaseCurrencyID == baseSlug && mkt.QuoteCurrencyID == quoteSlug) ||
			(mkt.BaseCurrencyID == quoteSlug && mkt.QuoteCurrencyID == baseSlug)
	}

	var filteredResults []*coinpapMarket
	for _, mkt := range rawMarkets {
		if marketMatches(mkt) {
			filteredResults = append(filteredResults, mkt)
		}
	}

	addMarket := func(mkt *coinpapMarket, buy, sell float64) {
		host, err := shortHost(mkt.MarketURL)
		if err != nil {
			log.Error(err)
			return
		}
		oracle := &OracleReport{
			Host:     host,
			BestBuy:  buy,
			BestSell: sell,
		}
		oracles = append(oracles, oracle)
		usdQuote, found := mkt.Quotes["USD"]
		if found {
			oracle.USDVol = usdQuote.Volume
		}
	}

	for _, mkt := range filteredResults {
		if mkt.BaseCurrencyID == baseSlug {
			buy, sell := spread(ctx, mkt.MarketURL, b.Symbol, q.Symbol, log)
			if buy > 0 && sell > 0 {
				// buy = 0, sell = 0 for any unknown markets
				addMarket(mkt, buy, sell)
			}
		} else {
			buy, sell := spread(ctx, mkt.MarketURL, q.Symbol, b.Symbol, log) // base and quote switched
			if buy > 0 && sell > 0 {
				addMarket(mkt, 1/sell, 1/buy) // inverted
			}
		}
	}

	return
}

// handleNote handles the makerBot's Core notifications.
func (m *makerBot) handleNote(ctx context.Context, note Notification) {
	switch n := note.(type) {
	case *OrderNote:
		ord := n.Order
		if ord == nil {
			return
		}
		m.processTrade(ord)
	case *EpochNotification:
		go m.rebalance(ctx, n.Epoch)
	}
}

// processTrade processes an order update.
func (m *makerBot) processTrade(o *Order) {
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

	convRate := m.market.MsgRateToConventional(o.Rate)
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

// rebalance rebalances the makerBot's orders.
//  1. Generate a basis price, p, adjusted for oracle weighting and bias.
//  2. Apply the gap strategy to get a target spread, s.
//  3. Check existing orders, if out of bounds
//     [p +/- (s/2) - drift_tolerance, p +/- (s/2) + drift_tolerance],
//     cancel the order
//  4. Compare remaining order counts to configured, lots, and place new
//     orders.
func (m *makerBot) rebalance(ctx context.Context, newEpoch uint64) {
	if !atomic.CompareAndSwapUint32(&m.rebalanceRunning, 0, 1) {
		return
	}
	defer atomic.StoreUint32(&m.rebalanceRunning, 0)

	newBuyLots, newSellLots, buyPrice, sellPrice := rebalance(ctx, m, m.market, m.program(), m.log, newEpoch)

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

// rebalancer is a stub to enabling testing of the rebalance calculations.
type rebalancer interface {
	basisPrice() uint64
	halfSpread(basisPrice uint64) (uint64, error)
	sortedOrders() (buys, sells []*sortedOrder)
	cancelOrder(oid order.OrderID) error
	MaxBuy(host string, base, quote uint32, rate uint64) (*MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*MaxOrderEstimate, error)
}

func rebalance(ctx context.Context, m rebalancer, mkt *Market, pgm *MakerProgram, log dex.Logger, newEpoch uint64) (newBuyLots, newSellLots int, buyPrice, sellPrice uint64) {
	basisPrice := m.basisPrice()
	if basisPrice == 0 {
		log.Errorf("No basis price available and no empty-market rate set")
		return
	}

	log.Tracef("rebalance: basis price = %d", basisPrice)

	// Three of the strategies will use a break-even half-gap.
	var breakEven uint64
	switch pgm.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus, GapStrategyMultiplier:
		var err error
		breakEven, err = m.halfSpread(basisPrice)
		if err != nil {
			log.Errorf("Could not calculate break-even spread: %v", err)
			return
		}
	}

	// Apply the base strategy.
	var halfSpread uint64
	switch pgm.GapStrategy {
	case GapStrategyMultiplier:
		halfSpread = uint64(math.Round(float64(breakEven) * pgm.GapFactor))
	case GapStrategyPercent, GapStrategyPercentPlus:
		halfSpread = uint64(math.Round(pgm.GapFactor * float64(basisPrice)))
	case GapStrategyAbsolute, GapStrategyAbsolutePlus:
		halfSpread = mkt.ConventionalRateToMsg(pgm.GapFactor)
	}

	// Add the break-even to the "-plus" strategies.
	switch pgm.GapStrategy {
	case GapStrategyAbsolutePlus, GapStrategyPercentPlus:
		halfSpread += breakEven
	}

	log.Tracef("rebalance: strategized half-spread = %d, strategy = %s", halfSpread, pgm.GapStrategy)

	halfSpread = steppedRate(halfSpread, mkt.RateStep)

	log.Tracef("rebalance: step-resolved half-spread = %d", halfSpread)

	buyPrice = basisPrice - halfSpread
	sellPrice = basisPrice + halfSpread

	log.Tracef("rebalance: buy price = %d, sell price = %d", buyPrice, sellPrice)

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
		log.Tracef("rebalance: can't buy because delayed cancel sell order interferes. booked rate = %d, buy price = %d",
			lowestSell, buyPrice)
		cantBuy = true
	}
	if sellPrice <= highestBuy {
		log.Tracef("rebalance: can't sell because delayed cancel sell order interferes. booked rate = %d, sell price = %d",
			highestBuy, sellPrice)
		cantSell = true
	}

	var canceledBuyLots, canceledSellLots uint64 // for stats reporting
	cancels := make([]*sortedOrder, 0)
	addCancel := func(ord *sortedOrder) {
		if newEpoch-ord.Epoch < 2 {
			log.Debugf("rebalance: skipping cancel not past free cancel threshold")
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
		tol := uint64(math.Round(float64(price) * pgm.DriftTolerance))
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
			log.Tracef("rebalance: processSide: sell = %t, order rate = %d, low = %d, high = %d",
				sell, ord.rate, low, high)
			if ord.rate < low || ord.rate > high {
				if newEpoch < ord.Epoch+2 { // https://github.com/decred/dcrdex/pull/1682
					log.Tracef("rebalance: postponing cancellation for order < 2 epochs old")
					keptLots += int(ord.lots)
				} else {
					log.Tracef("rebalance: cancelling out-of-bounds order (%d lots remaining). rate %d is not in range %d < r < %d",
						ord.lots, ord.rate, low, high)
					addCancel(ord)
				}
			} else {
				keptLots += int(ord.lots)
			}
		}
		return
	}

	newBuyLots, newSellLots = int(pgm.Lots), int(pgm.Lots)
	keptBuys := processSide(buys, buyPrice, false)
	keptSells := processSide(sells, sellPrice, true)
	newBuyLots -= keptBuys
	newSellLots -= keptSells

	// Cancel out of bounds or over-stacked orders.
	if len(cancels) > 0 {
		// Only cancel orders that are > 1 epoch old.
		log.Tracef("rebalance: cancelling %d orders", len(cancels))
		for _, cancel := range cancels {
			if err := m.cancelOrder(cancel.id); err != nil {
				log.Errorf("error cancelling order: %v", err)
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

	log.Tracef("rebalance: %d buy lots and %d sell lots scheduled after existing valid %d buy and %d sell lots accounted",
		newBuyLots, newSellLots, keptBuys, keptSells)

	// Resolve requested lots against the current balance. If we come up short,
	// we may be able to place extra orders on the other side to satisfy our
	// lot commitment and shift our balance back.
	var maxBuyLots int
	if newBuyLots > 0 {
		// TODO: MaxBuy and MaxSell shouldn't error for insufficient funds, but
		// they do. Maybe consider a constant error asset.InsufficientBalance.
		maxOrder, err := m.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, buyPrice)
		if err != nil {
			log.Tracef("MaxBuy error: %v", err)
		} else {
			maxBuyLots = int(maxOrder.Swap.Lots)
		}
		if maxBuyLots < newBuyLots {
			// We don't have the balance. Add our shortcoming to the other side.
			shortLots := newBuyLots - maxBuyLots
			newSellLots += shortLots
			newBuyLots = maxBuyLots
			log.Tracef("rebalance: reduced buy lots to %d because of low balance", newBuyLots)
		}
	}

	if newSellLots > 0 {
		var maxLots int
		maxOrder, err := m.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
		if err != nil {
			log.Tracef("MaxSell error: %v", err)
		} else {
			maxLots = int(maxOrder.Swap.Lots)
		}
		if maxLots < newSellLots {
			shortLots := newSellLots - maxLots
			newBuyLots += shortLots
			if newBuyLots > maxBuyLots {
				log.Tracef("rebalance: increased buy lot order to %d lots because sell balance is low", newBuyLots)
				newBuyLots = maxBuyLots
			}
			newSellLots = maxLots
			log.Tracef("rebalance: reduced sell lots to %d because of low balance", newSellLots)
		}
	}
	return
}

// placeOrder places a single order on the market.
func (m *makerBot) placeOrder(lots, rate uint64, sell bool) *Order {
	pgm := m.program()
	ord, err := m.Trade(nil, &TradeForm{
		Host:    pgm.Host,
		IsLimit: true,
		Sell:    sell,
		Base:    pgm.BaseID,
		Quote:   pgm.QuoteID,
		Qty:     lots * m.market.LotSize,
		Rate:    rate,
		Program: m.pgmID,
	})
	if err != nil {
		m.log.Errorf("Error placing rebalancing order: %v", err)
		return nil
	}
	return ord
}

// sortedOrder is a subset of an *Order used internally for sorting.
type sortedOrder struct {
	*Order
	id   order.OrderID
	rate uint64
	lots uint64
}

// sortedOrders returns lists of buy and sell orders, with buys sorted
// high to low by rate, and sells low to high.
func (m *makerBot) sortedOrders() (buys, sells []*sortedOrder) {
	makeSortedOrder := func(o *Order) *sortedOrder {
		var oid order.OrderID
		copy(oid[:], o.ID)
		return &sortedOrder{
			Order: o,
			id:    oid,
			rate:  o.Rate,
			lots:  (o.Qty - o.Filled) / m.market.LotSize,
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

// feeEstimates calculates the swap and redeem fees on an order. If the wallet's
// PreSwap/PreRedeem method cannot provide a value (because no balance, likely),
// and the Wallet implements BotWallet, then the estimate from
// SingleLotSwapFees/SingleLotRedeemFees will be used.
func (c *Core) feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error) {
	dc, connected, err := c.dex(form.Host)
	if err != nil {
		return 0, 0, err
	}
	if !connected {
		return 0, 0, errors.New("dex not connected")
	}

	baseWallet, found := c.wallet(form.Base)
	if !found {
		return 0, 0, fmt.Errorf("no base wallet found")
	}

	quoteWallet, found := c.wallet(form.Quote)
	if !found {
		return 0, 0, fmt.Errorf("no quote wallet found")
	}

	fromWallet, toWallet := quoteWallet, baseWallet
	if form.Sell {
		fromWallet, toWallet = baseWallet, quoteWallet
	}

	swapFeeSuggestion := c.feeSuggestion(dc, fromWallet.AssetID)
	if swapFeeSuggestion == 0 {
		return 0, 0, fmt.Errorf("failed to get swap fee suggestion for %s at %s", unbip(fromWallet.AssetID), form.Host)
	}

	redeemFeeSuggestion := c.feeSuggestionAny(toWallet.AssetID)
	if redeemFeeSuggestion == 0 {
		return 0, 0, fmt.Errorf("failed to get redeem fee suggestion for %s at %s", unbip(toWallet.AssetID), form.Host)
	}

	mkt := dc.coreMarket(marketName(form.Base, form.Quote))
	if mkt == nil {
		return 0, 0, fmt.Errorf("failed to get market %q at %q", marketName(form.Base, form.Quote), form.Host)
	}

	// Get the MaxFeeRate for the "from" asset.
	fromAsset := dc.exchangeInfo().Assets[fromWallet.AssetID]
	if fromAsset == nil { // unlikely given it has a mkt with this asset, but be defensive
		return 0, 0, fmt.Errorf("asset %d not supported by host %v", fromWallet.AssetID, form.Host)
	}

	lots := form.Qty / mkt.LotSize
	swapLotSize := mkt.LotSize
	if !form.Sell {
		swapLotSize = calc.BaseToQuote(form.Rate, mkt.LotSize)
	}

	preSwapForm := &asset.PreSwapForm{
		Version:         fromWallet.version,
		LotSize:         swapLotSize,
		Lots:            lots,
		MaxFeeRate:      fromAsset.MaxFeeRate,
		Immediate:       (form.IsLimit && form.TifNow) || !form.IsLimit,
		FeeSuggestion:   swapFeeSuggestion,
		SelectedOptions: form.Options,
		RedeemVersion:   toWallet.version,
		RedeemAssetID:   toWallet.AssetID,
	}

	swapEstimate, err := fromWallet.PreSwap(preSwapForm)
	if err == nil {
		swapFees = swapEstimate.Estimate.RealisticWorstCase
	} else {
		// Maybe they offer a single-lot estimate.
		if bw, is := fromWallet.Wallet.(asset.BotWallet); is {
			var err2 error
			swapFees, err2 = bw.SingleLotSwapFees(preSwapForm)
			if err2 != nil {
				return 0, 0, fmt.Errorf("error getting swap estimate (%v) and single-lot estimate (%v)", err, err2)
			}
			err = nil
		} else {
			return 0, 0, fmt.Errorf("error getting swap estimate: %w", err)
		}
	}

	preRedeemForm := &asset.PreRedeemForm{
		Version:         toWallet.version,
		Lots:            lots,
		FeeSuggestion:   redeemFeeSuggestion,
		SelectedOptions: form.Options,
	}
	redeemEstimate, err := toWallet.PreRedeem(preRedeemForm)
	if err == nil {
		redeemFees = redeemEstimate.Estimate.RealisticWorstCase
	} else {
		if bw, is := toWallet.Wallet.(asset.BotWallet); is {
			var err2 error
			redeemFees, err2 = bw.SingleLotRedeemFees(preRedeemForm)
			if err2 != nil {
				return 0, 0, fmt.Errorf("error getting redemption estimate (%v) and single-lot estimate (%v)", err, err2)
			}
			err = nil
		} else {
			return 0, 0, fmt.Errorf("error getting redemption estimate: %v", err)
		}
	}
	return
}

func (m *makerBot) halfSpread(basisPrice uint64) (uint64, error) {
	pgm := m.program()
	return breakEvenHalfSpread(pgm.Host, m.market, basisPrice, m.Core, m.log)
}

// breakEvenHalfSpread is the minimum spread that should be maintained to
// theoretically break even on a single-lot round-trip order pair. The returned
// spread half-width will be rounded up to the nearest integer, but not to a
// a rate step.
func (c *Core) breakEvenHalfSpread(host string, mkt *Market, basisPrice uint64) (uint64, error) {
	return breakEvenHalfSpread(host, mkt, basisPrice, c, c.log)
}

type botFeeEstimator interface {
	feeEstimates(form *TradeForm) (swapFees, redeemFees uint64, err error)
}

// breakEvenHalfSpread calculates the half-gap at which a buy->sell or sell->buy
// sequence breaks even in terms of profit and fee losses. See
// docs/images/break-even-half-gap.png.
func breakEvenHalfSpread(host string, mkt *Market, basisPrice uint64, fe botFeeEstimator, log dex.Logger) (uint64, error) {
	form := &TradeForm{
		Host:    host,
		IsLimit: true,
		Sell:    true,
		Base:    mkt.BaseID,
		Quote:   mkt.QuoteID,
		Qty:     mkt.LotSize,
		Rate:    basisPrice,
		TifNow:  false,
		// Options: , // Maybe can enable someday.
	}

	if basisPrice == 0 { // prevent divide by zero later
		return 0, errors.New("basis price cannot be zero")
	}

	baseFees, quoteFees, err := fe.feeEstimates(form)
	if err != nil {
		return 0, fmt.Errorf("error getting sell order estimate: %w", err)
	}
	log.Tracef("breakEvenHalfSpread: sell = true fees: base = %d, quote = %d", baseFees, quoteFees)

	form.Sell = false
	newQuoteFees, newBaseFees, err := fe.feeEstimates(form)
	log.Tracef("breakEvenHalfSpread: sell = false fees: base = %d, quote = %d", newBaseFees, newQuoteFees)
	if err != nil {
		return 0, fmt.Errorf("error getting buy order estimate: %w", err)
	}
	baseFees += newBaseFees
	quoteFees += newQuoteFees

	g := float64(calc.BaseToQuote(basisPrice, baseFees)+quoteFees) /
		float64(baseFees+2*mkt.LotSize)

	halfGap := uint64(math.Round(g * calc.RateEncodingFactor))

	log.Tracef("breakEvenHalfSpread: base basis price = %d, lot size = %d, base fees = %d, quote fees = %d, half-gap = %d",
		basisPrice, mkt.LotSize, baseFees, quoteFees, halfGap)

	return halfGap, nil
}

// Truncates the URL to the domain name and TLD.
func shortHost(addr string) (string, error) {
	u, err := url.Parse(addr)
	if u == nil {
		return "", fmt.Errorf("Error parsing URL %q: %v", addr, err)
	}
	// remove subdomains
	parts := strings.Split(u.Host, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("Not enought URL parts: %q", addr)
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1], nil
}

// spread fetches market data and returns the best buy and sell prices.
// TODO: We may be able to do better. We could pull a small amount of market
// book data and do a VWAP-like integration of, say, 1 DEX lot's worth.
func spread(ctx context.Context, addr string, baseSymbol, quoteSymbol string, log dex.Logger) (sell, buy float64) {
	host, err := shortHost(addr)
	if err != nil {
		log.Error(err)
		return
	}
	s := spreaders[host]
	if s == nil {
		return 0, 0
	}
	sell, buy, err = s(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		log.Errorf("Error getting spread from %q: %v", addr, err)
		return 0, 0
	}
	return sell, buy
}

// prepareBotWallets unlocks the wallets that marketBot uses.
func (c *Core) prepareBotWallets(crypter encrypt.Crypter, baseID, quoteID uint32) error {
	baseWallet, found := c.wallet(baseID)
	if !found {
		return fmt.Errorf("no base %s wallet", unbip(baseID))
	}

	if !baseWallet.unlocked() {
		if err := c.connectAndUnlock(crypter, baseWallet); err != nil {
			return fmt.Errorf("failed to unlock base %s wallet: %v", unbip(baseID), err)
		}
	}

	quoteWallet, found := c.wallet(quoteID)
	if !found {
		return fmt.Errorf("no quote %s wallet", unbip(quoteID))
	}

	if !quoteWallet.unlocked() {
		if err := c.connectAndUnlock(crypter, quoteWallet); err != nil {
			return fmt.Errorf("failed to unlock quote %s wallet: %v", unbip(quoteID), err)
		}
	}

	return nil
}

// CreateBot creates and starts a market-maker bot.
func (c *Core) CreateBot(pw []byte, botType string, pgm *MakerProgram) (uint64, error) {
	if botType != MakerBotV0 {
		return 0, fmt.Errorf("unknown bot type %q", botType)
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return 0, codedError(passwordErr, err)
	}
	defer crypter.Close()

	if err := c.prepareBotWallets(crypter, pgm.BaseID, pgm.QuoteID); err != nil {
		return 0, err
	}

	bot, err := newMakerBot(c.ctx, c, pgm)
	if err != nil {
		return 0, err
	}
	c.mm.Lock()
	c.mm.bots[bot.pgmID] = bot
	c.mm.Unlock()

	c.wg.Add(1)
	go func() {
		bot.run(c.ctx)
		c.wg.Done()
	}()
	return bot.pgmID, nil
}

// StartBot starts an existing market-maker bot.
func (c *Core) StartBot(pw []byte, pgmID uint64) error {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return codedError(passwordErr, err)
	}
	defer crypter.Close()

	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}

	if atomic.LoadUint32(&bot.running) == 1 {
		c.log.Warnf("Ignoring attempt to start an already-running bot for %s", bot.market.Name)
		return nil
	}

	if err := c.prepareBotWallets(crypter, bot.base.ID, bot.quote.ID); err != nil {
		return err
	}

	c.wg.Add(1)
	go func() {
		bot.run(c.ctx)
		c.wg.Done()
	}()
	return nil
}

// StopBot stops a running market-maker bot. Stopping the bot cancels all
// existing orders.
func (c *Core) StopBot(pgmID uint64) error {
	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	bot.stop()
	return nil
}

// UpdateBotProgram updates the program of an existing market-maker bot.
func (c *Core) UpdateBotProgram(pgmID uint64, pgm *MakerProgram) error {
	c.mm.RLock()
	bot := c.mm.bots[pgmID]
	c.mm.RUnlock()
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	return bot.updateProgram(pgm)
}

// RetireBot stops a bot and deletes its program from the database.
func (c *Core) RetireBot(pgmID uint64) error {
	c.mm.Lock()
	defer c.mm.Unlock()
	bot := c.mm.bots[pgmID]
	if bot == nil {
		return fmt.Errorf("no bot with program ID %d", pgmID)
	}
	bot.retire()
	delete(c.mm.bots, pgmID)
	return nil
}

// loadBotPrograms is used as part of the startup routine run during Login.
// Loads all existing programs from the database and matches bots with
// existing orders.
func (c *Core) loadBotPrograms() error {
	botPrograms, err := c.db.ActiveBotPrograms()
	if err != nil {
		return err
	}
	if len(botPrograms) == 0 {
		return nil
	}
	botTrades := make(map[uint64][]*Order)
	for _, dc := range c.dexConnections() {
		for _, t := range dc.trackedTrades() {
			if t.metaData.ProgramID > 0 {
				botTrades[t.metaData.ProgramID] = append(botTrades[t.metaData.ProgramID], t.coreOrder())
			}
		}
	}
	c.mm.Lock()
	defer c.mm.Unlock()
	for pgmID, botPgm := range botPrograms {
		if c.mm.bots[pgmID] != nil {
			continue
		}
		var makerPgm *MakerProgram
		if err := json.Unmarshal(botPgm.Program, &makerPgm); err != nil {
			c.log.Errorf("Error decoding maker program: %v", err)
			continue
		}
		bot, err := recreateMakerBot(c.ctx, c, pgmID, makerPgm)
		if err != nil {
			c.log.Errorf("Error recreating maker bot: %v", err)
			continue
		}
		for _, ord := range botTrades[pgmID] {
			var oid order.OrderID
			copy(oid[:], ord.ID)
			bot.ords[oid] = ord
		}
		c.mm.bots[pgmID] = bot
	}
	return nil
}

// bots returns a list of BotReport for all existing bots.
func (c *Core) bots() []*BotReport {
	c.mm.RLock()
	defer c.mm.RUnlock()
	bots := make([]*BotReport, 0, len(c.mm.bots))
	for _, bot := range c.mm.bots {
		bots = append(bots, bot.report())
	}
	// Sort them newest first.
	sort.Slice(bots, func(i, j int) bool { return bots[i].ProgramID > bots[j].ProgramID })
	return bots
}

// MarketReport generates a summary of oracle price data for a market.
func (c *Core) MarketReport(host string, baseID, quoteID uint32) (*MarketReport, error) {
	dc, _, err := c.dex(host)
	if err != nil {
		return nil, fmt.Errorf("host %q not known", host)
	}

	mkt := dc.coreMarket(marketName(baseID, quoteID))
	if mkt == nil {
		return nil, fmt.Errorf("no market %q at %q", marketName(baseID, quoteID), host)
	}

	r := &MarketReport{}

	// The break-even spread depends on the basis price. So we need to
	// exhaust all possible options to get a basis price before performing
	// this last step.
	setBreakEven := func() error {
		// If the basis price is still zero, use the oracle price. This mirrors
		// the handling in basisPrice for oracle weight > 0.
		if r.BasisPrice == 0 {
			r.BasisPrice = r.Price
		}
		breakEven, err := c.breakEvenHalfSpread(host, mkt, mkt.ConventionalRateToMsg(r.BasisPrice))
		if err != nil {
			return fmt.Errorf("error calculating break-even spread: %v", err)
		}
		r.BreakEvenSpread = mkt.MsgRateToConventional(breakEven) * 2
		return nil
	}

	dc.booksMtx.Lock()
	book := dc.books[mkt.Name]
	dc.booksMtx.Unlock()
	var midGap uint64
	if book != nil {
		midGap, err = book.MidGap()
		if err != nil && !errors.Is(err, orderbook.ErrEmptyOrderbook) {
			return nil, fmt.Errorf("error calculating mid-gap: %w", err)
		}
	}
	// oracle weighting = 0 bypasses any oracle checks and uses mid-gap with a
	// fiat rate ratio backup.
	var zeroOracleBias, zeroOracleWeight float64
	r.BasisPrice = mkt.MsgRateToConventional(basisPrice(host, mkt, zeroOracleBias, zeroOracleWeight, midGap, c, c.log))

	// See if we have a valid cached report.
	p := c.cachedOraclePrice(marketName(baseID, quoteID))
	if p != nil {
		r.Price = p.price
		r.Oracles = p.oracles
		return r, setBreakEven()
	}

	// No cached oracle data. Try to get it new.
	b := c.asset(baseID)
	if b == nil {
		return nil, fmt.Errorf("base asset %d not known", baseID)
	}

	q := c.asset(quoteID)
	if q == nil {
		return nil, fmt.Errorf("quote asset %d not known", quoteID)
	}

	oracles, price, err := c.marketReport(c.ctx, b, q)
	if err != nil {
		return nil, err
	}
	c.log.Debugf("oracle rate fetched for market %s: %f", marketName(baseID, quoteID), price)
	r.Oracles = oracles
	r.Price = price
	r.BasisPrice = mkt.MsgRateToConventional(basisPrice(host, mkt, zeroOracleBias, zeroOracleWeight, midGap, c, c.log))
	return r, setBreakEven()
}

// Spreader is a function that can generate market spread data for a known
// exchange.
type Spreader func(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error)

var spreaders = map[string]Spreader{
	"binance.com":  fetchBinanceSpread,
	"coinbase.com": fetchCoinbaseSpread,
	"bittrex.com":  fetchBittrexSpread,
	"hitbtc.com":   fetchHitBTCSpread,
	"exmo.com":     fetchEXMOSpread,
}

func fetchBinanceSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/bookTicker?symbol=%s", slug)

	var resp struct {
		BidPrice float64 `json:"bidPrice,string"`
		AskPrice float64 `json:"askPrice,string"`
	}
	return resp.AskPrice, resp.BidPrice, getRates(ctx, url, &resp)
}

func fetchCoinbaseSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/ticker", slug)

	var resp struct {
		Ask float64 `json:"ask,string"`
		Bid float64 `json:"bid,string"`
	}

	return resp.Ask, resp.Bid, getRates(ctx, url, &resp)
}

func fetchBittrexSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.bittrex.com/v3/markets/%s/ticker", slug)
	var resp struct {
		AskRate float64 `json:"askRate,string"`
		BidRate float64 `json:"bidRate,string"`
	}
	return resp.AskRate, resp.BidRate, getRates(ctx, url, &resp)
}

func fetchHitBTCSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.hitbtc.com/api/3/public/orderbook/%s?depth=1", slug)

	var resp struct {
		Ask [][2]json.Number `json:"ask"`
		Bid [][2]json.Number `json:"bid"`
	}
	if err := getRates(ctx, url, &resp); err != nil {
		return 0, 0, err
	}
	if len(resp.Ask) < 1 || len(resp.Bid) < 1 {
		return 0, 0, fmt.Errorf("not enough orders")
	}

	ask, err := resp.Ask[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode ask price %q", resp.Ask[0][0])
	}

	bid, err := resp.Bid[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode bid price %q", resp.Bid[0][0])
	}

	return ask, bid, nil
}

func fetchEXMOSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s_%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exmo.com/v1.1/order_book?pair=%s&limit=1", slug)

	var resp map[string]*struct {
		AskTop float64 `json:"ask_top,string"`
		BidTop float64 `json:"bid_top,string"`
	}

	if err := getRates(ctx, url, &resp); err != nil {
		return 0, 0, err
	}

	mkt := resp[slug]
	if mkt == nil {
		return 0, 0, errors.New("slug not in response")
	}

	return mkt.AskTop, mkt.BidTop, nil
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

// stampedPrice is used for caching price data that can expire.
type stampedPrice struct {
	stamp   time.Time
	price   float64
	oracles []*OracleReport
}
