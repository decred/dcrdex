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
	"decred.org/dcrdex/client/core/botengine"
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

	ErrNoMarkets = dex.ErrorKind("no markets")

	// Our mid-gap rate derived from the local DEX order book is converted to an
	// effective mid-gap that can only vary by up to 3% from the oracle rate.
	// This is to prevent someone from taking advantage of a sparse market to
	// force a bot into giving a favorable price. In reality a market maker on
	// an empty market should use a high oracle bias anyway, but this should
	// prevent catastrophe.
	maxOracleMismatch = 0.03
)

type MakerProgram struct {
	Host     string `json:"host"`
	BaseID   uint32 `json:"baseID"`
	QuoteID  uint32 `json:"quoteID"`
	EngineID string `json:"engineID"`

	// The config of each engine type will be here, but only one will be non-nil
	GapEngineCfg *botengine.GapEngineCfg `json:"gapEngineCfg"`
}

func (pgm *MakerProgram) validate() error {
	if pgm.Host == "" {
		return errors.New("no host specified")
	}
	if dex.BipIDSymbol(pgm.BaseID) == "" {
		return fmt.Errorf("base asset %d unknown", pgm.BaseID)
	}
	if dex.BipIDSymbol(pgm.QuoteID) == "" {
		return fmt.Errorf("quote asset %d unknown", pgm.QuoteID)
	}

	switch pgm.EngineID {
	case string(botengine.GapEngineType):
		if pgm.GapEngineCfg == nil {
			return errors.New("gap engine cfg is nil")
		}
		if err := pgm.GapEngineCfg.Validate(); err != nil {
			return fmt.Errorf("error validating gap engine cfg: %w", err)
		}
	default:
		return fmt.Errorf("unknown bot engine type: %s", pgm.EngineID)
	}

	return nil
}

// makerAsset combines a *dex.Asset with a WalletState.
type makerAsset struct {
	*SupportedAsset
	// walletV  atomic.Value // *WalletState
	balanceV atomic.Value // *WalletBalance
}

// makerBot is a *Core extension that enables operation of a market-maker bot.
// The strategy the makerBot follows depends on the engine.
type makerBot struct {
	*Core
	pgmID uint64
	base  *makerAsset
	quote *makerAsset
	// TODO: enable updating of market, or just grab it live when needed.
	market *Market
	log    dex.Logger
	book   *orderbook.OrderBook
	engine botengine.BotEngine

	running uint32 // atomic
	wg      sync.WaitGroup
	die     context.CancelFunc

	programV atomic.Value // *MakerProgram

	ordMtx sync.RWMutex
	ords   map[order.OrderID]*Order

	oracleRunning uint32 // atomic
}

var _ botengine.GapEngineInputs = (*makerBot)(nil)

// checkInitialFunding ensures that the wallets have enough funds to start the bot.
//
// The makerBot's engine must be set before calling this function.
func (m *makerBot) checkInitialFunding(ctx context.Context) error {
	// Get a rate now, because max buy won't have an oracle fallback.
	var rate uint64
	if m.market.SpotPrice != nil {
		rate = m.market.SpotPrice.Rate
	}
	if rate == 0 {
		price, err := m.syncOraclePrice(ctx)
		if err != nil {
			return fmt.Errorf("failed to establish a starting price: %v", err)
		}
		rate = m.market.ConventionalRateToMsg(price)
	}

	pgm := m.program()

	var maxBuyLots, maxSellLots uint64
	maxBuy, err := m.MaxBuy(rate)
	if err == nil {
		maxBuyLots = maxBuy.Swap.Lots
	} else {
		m.log.Errorf("Bot MaxBuy error: %v", err)
	}
	maxSell, err := m.MaxSell()
	if err == nil {
		maxSellLots = maxSell.Swap.Lots
	} else {
		m.log.Errorf("Bot MaxSell error: %v", err)
	}

	lotsRequired := m.engine.InitialLotsRequired()
	if maxBuyLots+maxSellLots < lotsRequired {
		return fmt.Errorf("cannot create bot. %d lots total balance required to start, "+
			"and only %d %s lots and %d %s lots = %d total lots are available", lotsRequired,
			maxBuyLots, unbip(pgm.QuoteID), maxSellLots, unbip(pgm.BaseID), maxSellLots+maxBuyLots)
	}

	return nil
}

func createMakerBot(ctx context.Context, c *Core, pgm *MakerProgram) (*makerBot, error) {
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

	switch pgm.EngineID {
	case string(botengine.GapEngineType):
		if pgm.GapEngineCfg == nil {
			return nil, errors.New("cannot create gap engine without gap engine cfg")
		}
		engine, err := botengine.NewGapEngine(m, pgm.GapEngineCfg, m.log.SubLogger("GAP-ENGINE"))
		if err != nil {
			return nil, fmt.Errorf("failed to create gap engine: %w", err)
		}
		m.engine = engine

	default:
		return nil, fmt.Errorf("unknown bot engine: %v", pgm.EngineID)
	}

	err = m.checkInitialFunding(ctx)
	if err != nil {
		return nil, err
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

// recreateMakerBot recreates a maker bot using the object saved in the db.
func recreateMakerBot(ctx context.Context, c *Core, id uint64, botPgm *db.BotProgram) (*makerBot, error) {
	var makerPgm *MakerProgram
	if err := json.Unmarshal(botPgm.Program, &makerPgm); err != nil {
		return nil, fmt.Errorf("Error decoding maker program: %v", err)
	}
	m, err := createMakerBot(ctx, c, makerPgm)
	if err != nil {
		return nil, err
	}
	m.pgmID = id

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

	m.ordMtx.Lock()
	m.ords = make(map[order.OrderID]*Order)
	m.ordMtx.Unlock()

	// TODO: Should really wait until the cancels match. If we're cancelling
	// orders placed in the same epoch, they might miss and need to be replaced.

	// TODO: If the bot is stopped before the trades become booked, and the cancels
	// do not match, then the bot will be stopped but the orders still on the books.
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

	m.engine.Run(ctx)

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

	tradeFeed := m.engine.TradeFeed()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case n := <-tradeFeed:
				m.handleTradeFeedNote(n)
			case <-ctx.Done():
				return
			}
		}
	}()

	m.notify(newBotNote(TopicBotStarted, "", "", db.Data, m.report()))

	m.wg.Wait()
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
		m.engine.Notify(botengine.EpochNote(n.Epoch))
	}
}

// handleTradeFeedNote handles a note from the engine.
func (m *makerBot) handleTradeFeedNote(note interface{}) {
	switch n := note.(type) {
	case *botengine.CancelNote:
		err := m.cancelOrder(n.OID)
		if err != nil {
			m.log.Errorf("failed to place cancel order: %v", err)
		}
	case *botengine.OrderNote:
		m.placeOrder(n.Lots, n.Rate, n.Sell)
	default:
		m.log.Warnf("unexpected trade feed note type: %v", n)
	}
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

// marshalEngineCfg marshals the engine's configuration.
func (m *makerBot) marshalEngineCfg() ([]byte, error) {
	pgm := m.program()
	switch pgm.EngineID {
	case string(botengine.GapEngineType):
		if pgm.GapEngineCfg == nil {
			return nil, errors.New("gap engine cfg is nil")
		}
		cfgB, err := json.Marshal(pgm.GapEngineCfg)
		if err != nil {
			return nil, err
		}
		return cfgB, nil
	default:
		return nil, fmt.Errorf("unknown bot engine type: %s", pgm.EngineID)
	}
}

// updateProgram updates the current bot program.
func (m *makerBot) updateProgram(pgm *MakerProgram) error {
	err := pgm.validate()
	if err != nil {
		return err
	}

	currentPgm := m.program()
	if pgm.Host != currentPgm.Host ||
		pgm.BaseID != currentPgm.BaseID ||
		pgm.QuoteID != currentPgm.QuoteID ||
		pgm.EngineID != currentPgm.EngineID {
		return errors.New("only engine config can be updated")
	}

	dbRecord, err := makerProgramDBRecord(pgm)
	if err != nil {
		return err
	}

	cfgB, err := m.marshalEngineCfg()
	if err != nil {
		return fmt.Errorf("failed to marshal engine cfg: %w", err)
	}

	err = m.engine.Update(cfgB)
	if err != nil {
		return fmt.Errorf("failed to update engine: %w", err)
	}

	m.programV.Store(pgm)

	if err := m.db.UpdateBotProgram(m.pgmID, dbRecord); err != nil {
		m.log.Errorf("faied to store updated bot program in DB: %v", err)
	}

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

// StartOracleSync starts the syncOracle loop.
func (m *makerBot) StartOracleSync(ctx context.Context) {
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

// BasisPrice is the basis price for the makerBot's market.
func (m *makerBot) BasisPrice(oracleBias, oracleWeighting, emptyMarketRate float64) uint64 {
	pgm := m.program()
	midGap, err := m.book.MidGap()
	if err != nil && !errors.Is(err, orderbook.ErrEmptyOrderbook) {
		m.log.Errorf("error calculating mid-gap: %w", err)
		return 0
	}
	if p := basisPrice(pgm.Host, m.market, oracleBias, oracleWeighting, midGap, m, m.log); p > 0 {
		return p
	}
	return m.market.ConventionalRateToMsg(emptyMarketRate)
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
	if err := getInto(ctx, url, &rawMarkets); err != nil {
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

// placeOrder places a single order on the market.
func (m *makerBot) placeOrder(lots, rate uint64, sell bool) {
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
	} else {
		var oid order.OrderID
		copy(oid[:], ord.ID)
		m.ordMtx.Lock()
		m.ords[oid] = ord
		m.ordMtx.Unlock()
	}
}

func (m *makerBot) MaxBuy(rate uint64) (*botengine.MaxOrderEstimate, error) {
	pgm := m.program()
	estimate, err := m.Core.MaxBuy(pgm.Host, pgm.BaseID, pgm.QuoteID, rate)
	if err != nil {
		return nil, err
	}
	return &botengine.MaxOrderEstimate{
		Swap:   estimate.Swap,
		Redeem: estimate.Redeem,
	}, nil
}

func (m *makerBot) MaxSell() (*botengine.MaxOrderEstimate, error) {
	pgm := m.program()
	estimate, err := m.Core.MaxSell(pgm.Host, pgm.BaseID, pgm.QuoteID)
	if err != nil {
		return nil, err
	}
	return &botengine.MaxOrderEstimate{
		Swap:   estimate.Swap,
		Redeem: estimate.Redeem,
	}, nil
}

func (m *makerBot) ConventionalRateToMsg(r float64) uint64 {
	return m.market.ConventionalRateToMsg(r)
}
func (m *makerBot) RateStep() uint64 {
	return m.market.RateStep
}

// sortedOrders returns lists of buy and sell orders, with buys sorted
// high to low by rate, and sells low to high.
func (m *makerBot) SortedOrders() (buys, sells []*botengine.Order) {
	makeBotEngineOrder := func(o *Order) *botengine.Order {
		var oid order.OrderID
		copy(oid[:], o.ID)
		return &botengine.Order{
			Sell:   o.Sell,
			ID:     oid,
			Status: o.Status,
			Rate:   o.Rate,
			Lots:   (o.Qty - o.Filled) / m.market.LotSize,
			Epoch:  o.Epoch,
		}
	}

	buys, sells = make([]*botengine.Order, 0), make([]*botengine.Order, 0)
	m.ordMtx.RLock()
	for _, ord := range m.ords {
		if ord.Sell {
			sells = append(sells, makeBotEngineOrder(ord))
		} else {
			buys = append(buys, makeBotEngineOrder(ord))
		}
	}
	m.ordMtx.RUnlock()

	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })

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
		Immediate:       (form.IsLimit && form.TifNow),
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

// HalfSpread calculates the half-gap at which a buy->sell or sell->buy
// sequence breaks even in terms of profit and fee losses.
func (m *makerBot) HalfSpread(basisPrice uint64) (uint64, error) {
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
		bot, err := recreateMakerBot(c.ctx, c, pgmID, botPgm)
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
	return resp.AskPrice, resp.BidPrice, getInto(ctx, url, &resp)
}

func fetchCoinbaseSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/ticker", slug)

	var resp struct {
		Ask float64 `json:"ask,string"`
		Bid float64 `json:"bid,string"`
	}

	return resp.Ask, resp.Bid, getInto(ctx, url, &resp)
}

func fetchBittrexSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.bittrex.com/v3/markets/%s/ticker", slug)
	var resp struct {
		AskRate float64 `json:"askRate,string"`
		BidRate float64 `json:"bidRate,string"`
	}
	return resp.AskRate, resp.BidRate, getInto(ctx, url, &resp)
}

func fetchHitBTCSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.hitbtc.com/api/3/public/orderbook/%s?depth=1", slug)

	var resp struct {
		Ask [][2]json.Number `json:"ask"`
		Bid [][2]json.Number `json:"bid"`
	}
	if err := getInto(ctx, url, &resp); err != nil {
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

	if err := getInto(ctx, url, &resp); err != nil {
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
