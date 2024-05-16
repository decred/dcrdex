// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	NotificationFeed() *core.NoteFeed
	ExchangeMarket(host string, baseID, quoteID uint32) (*core.Market, error)
	MarketConfig(host string, baseID, quoteID uint32) (*msgjson.Market, error)
	SyncBook(host string, baseID, quoteID uint32) (*orderbook.OrderBook, core.BookFeed, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, uint64, error)
	Cancel(oidB dex.Bytes) error
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	WalletState(assetID uint32) *core.WalletState
	MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error)
	MaxFundingFees(fromAsset uint32, host string, numTrades uint32, fromSettings map[string]string) (uint64, error)
	User() *core.User
	Login(pw []byte) error
	OpenWallet(assetID uint32, appPW []byte) error
	Broadcast(core.Notification)
	FiatConversionRates() map[uint32]float64
	Send(pw []byte, assetID uint32, value uint64, address string, subtract bool) (asset.Coin, error)
	NewDepositAddress(assetID uint32) (string, error)
	Network() dex.Network
	Order(oidB dex.Bytes) (*core.Order, error)
	WalletTransaction(uint32, string) (*asset.WalletTransaction, error)
}

var _ clientCore = (*core.Core)(nil)

// dexOrderBook is satisfied by orderbook.OrderBook.
// Avoids having to mock the entire orderbook in tests.
type dexOrderBook interface {
	MidGap() (uint64, error)
	VWAP(lots, lotSize uint64, sell bool) (avg, extrema uint64, filled bool, err error)
}

var _ dexOrderBook = (*orderbook.OrderBook)(nil)

// MarketWithHost represents a market on a specific dex server.
type MarketWithHost struct {
	Host    string `json:"host"`
	BaseID  uint32 `json:"base"`
	QuoteID uint32 `json:"quote"`
}

func (m MarketWithHost) String() string {
	return fmt.Sprintf("%s-%d-%d", m.Host, m.BaseID, m.QuoteID)
}

func (m MarketWithHost) ID() string {
	n, _ := dex.MarketName(m.BaseID, m.QuoteID)
	return n
}

// centralizedExchange is used to manage an exchange API connection.
type centralizedExchange struct {
	libxc.CEX
	*CEXConfig

	mtx        sync.RWMutex
	cm         *dex.ConnectionMaster
	mkts       []*libxc.Market
	connectErr string
}

type bot interface {
	dex.Connector
	updateConfig(cfg *BotConfig) error
}

// botConnectionMaster is an extension of dex.ConnectionMaster that allows
// pausing and resuming of the bot in order to update the configuration.
type botConnectionMaster struct {
	ctx         context.Context
	done        chan struct{}
	closeDone   func()
	donePausing chan struct{}

	// mtx is locked for the entire duration of each operation.
	mtx    sync.Mutex
	bot    bot
	botCM  *dex.ConnectionMaster
	paused bool
}

// newBotConnectionMaster creates a new botConnectionMaster, and starts the
// bot. Unlike the dex.ConnectionMaster, the bot can only be started once.
func newBotConnectionMaster(ctx context.Context, bot bot) (*botConnectionMaster, error) {
	cm := dex.NewConnectionMaster(bot)
	err := cm.ConnectOnce(ctx)
	if err != nil {
		return nil, err
	}

	done := make(chan struct{})

	closeDoneCtx, closeDone := context.WithCancel(context.Background())
	go func() {
		<-closeDoneCtx.Done()
		close(done)
	}()

	bcm := &botConnectionMaster{
		bot:   bot,
		botCM: cm,
		ctx:   ctx,
		done:  done,
		closeDone: func() {
			closeDone()
		},
		donePausing: make(chan struct{}, 16),
	}

	bcm.closeDoneWhenBotDone()

	return bcm, nil
}

func (b *botConnectionMaster) closeDoneWhenBotDone() {
	go func() {
		<-b.botCM.Done()
		b.mtx.Lock()
		defer b.mtx.Unlock()

		if !b.paused {
			b.closeDone()
		} else {
			select {
			case b.donePausing <- struct{}{}:
			default:
			}
		}
	}()
}

func (b *botConnectionMaster) disconnect() {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	b.botCM.Disconnect()
	b.closeDone()
}

func (b *botConnectionMaster) pause() error {
	pause := func() error {
		b.mtx.Lock()
		defer b.mtx.Unlock()

		if b.paused {
			return errors.New("already paused")
		}
		if !b.botCM.On() {
			return errors.New("bot not running")
		}
		b.paused = true
		b.botCM.Disconnect()
		return nil
	}

	if err := pause(); err != nil {
		return err
	}

	<-b.donePausing
	return nil
}

func (b *botConnectionMaster) resume() error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if !b.paused {
		return errors.New("bot not paused")
	}
	if b.botCM.On() {
		return errors.New("bot running")
	}

	err := b.botCM.ConnectOnce(b.ctx)
	if err != nil {
		b.paused = false
		b.closeDone()
		return err
	}

	b.paused = false
	b.closeDoneWhenBotDone()
	return nil
}

func (b *botConnectionMaster) updateConfig(cfg *BotConfig) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.botCM.On() || !b.paused {
		return errors.New("bot must be paused to update config")
	}

	return b.bot.updateConfig(cfg)
}

type runningBot struct {
	adaptor exchangeAdaptor
	botCm   *botConnectionMaster
	cexCfg  *CEXConfig

	botCfgMtx sync.RWMutex
	botCfg    *BotConfig
}

func (rb *runningBot) assets() map[uint32]interface{} {
	assets := make(map[uint32]interface{})

	rb.botCfgMtx.RLock()
	defer rb.botCfgMtx.RUnlock()

	assets[rb.botCfg.BaseID] = struct{}{}
	assets[rb.botCfg.QuoteID] = struct{}{}
	assets[feeAsset(rb.botCfg.BaseID)] = struct{}{}
	assets[feeAsset(rb.botCfg.QuoteID)] = struct{}{}

	return assets
}

func (rb *runningBot) cexName() string {
	if rb.botCfg.CEXCfg != nil {
		return rb.botCfg.CEXCfg.Name
	}
	return ""
}

// MarketMaker handles the market making process. It supports running different
// strategies on different markets.
type MarketMaker struct {
	ctx            context.Context
	log            dex.Logger
	core           clientCore
	defaultCfgPath string
	eventLogDBPath string
	eventLogDB     eventLogDB
	oracle         *priceOracle

	defaultCfgMtx sync.RWMutex
	// defaultCfg is the configuration specified by the file at the path passed
	// to NewMarketMaker as an argument. An alternateCfgPath can be passed to
	// some functions to use a different config file (this is how the
	// MarketMaker is used from the CLI).
	defaultCfg *MarketMakingConfig

	runningBotsMtx sync.RWMutex
	runningBots    map[MarketWithHost]*runningBot

	// startUpdateMtx is used to prevent starting or updating bots concurrently.
	startUpdateMtx sync.Mutex

	cexMtx sync.RWMutex
	cexes  map[string]*centralizedExchange
}

// NewMarketMaker creates a new MarketMaker.
func NewMarketMaker(c clientCore, eventLogDBPath, cfgPath string, log dex.Logger) (*MarketMaker, error) {
	var cfg MarketMakingConfig
	if b, err := os.ReadFile(cfgPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error reading config file from %q: %w", cfgPath, err)
	} else if len(b) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("error unmarshaling config file: %v", err)
		}
	}

	return &MarketMaker{
		core:           c,
		log:            log,
		defaultCfgPath: cfgPath,
		defaultCfg:     &cfg,
		eventLogDBPath: eventLogDBPath,
		runningBots:    make(map[MarketWithHost]*runningBot),
		cexes:          make(map[string]*centralizedExchange),
	}, nil
}

// runningBotsLookup returns a lookup map for running bots.
func (m *MarketMaker) runningBotsLookup() map[MarketWithHost]*runningBot {
	m.runningBotsMtx.RLock()
	defer m.runningBotsMtx.RUnlock()

	mkts := make(map[MarketWithHost]*runningBot, len(m.runningBots))
	for mkt, rb := range m.runningBots {
		mkts[mkt] = rb
	}

	return mkts
}

// Status is state information about the MarketMaker.
type Status struct {
	Bots  []*BotStatus          `json:"bots"`
	CEXes map[string]*CEXStatus `json:"cexes"`
}

// CEXStatus is state information about a cex.
type CEXStatus struct {
	Config          *CEXConfig      `json:"config"`
	Connected       bool            `json:"connected"`
	ConnectionError string          `json:"connectErr"`
	Markets         []*libxc.Market `json:"markets"`
}

// BotStatus is state information about a configured bot.
type BotStatus struct {
	Config *BotConfig `json:"config"`
	// RunStats being non-nil means the bot is running.
	RunStats *RunStats `json:"runStats"`
}

// Status generates a Status for the MarketMaker. This returns the status of
// all bots specified in the default config file.
func (m *MarketMaker) Status() *Status {
	cfg := m.defaultConfig()
	status := &Status{
		CEXes: make(map[string]*CEXStatus, len(cfg.CexConfigs)),
		Bots:  make([]*BotStatus, 0, len(cfg.BotConfigs)),
	}
	runningBots := m.runningBotsLookup()
	for _, botCfg := range cfg.BotConfigs {
		mkt := MarketWithHost{botCfg.Host, botCfg.BaseID, botCfg.QuoteID}
		rb := runningBots[mkt]
		var stats *RunStats
		if rb != nil {
			stats = rb.adaptor.stats()
		}
		status.Bots = append(status.Bots, &BotStatus{
			Config:   botCfg,
			RunStats: stats,
		})
	}
	for _, cex := range m.cexList() {
		s := &CEXStatus{Config: cex.CEXConfig}
		if cex != nil {
			cex.mtx.RLock()
			s.Connected = cex.cm != nil && cex.cm.On()
			s.Markets = cex.mkts
			s.ConnectionError = cex.connectErr
			cex.mtx.RUnlock()
		}
		status.CEXes[cex.Name] = s
	}
	return status
}

// RunningBotsStatus returns the status of all currently running bots. This
// should be used by the CLI which may have passed in an alternate config
// file when starting bots.
func (m *MarketMaker) RunningBotsStatus() *Status {
	status := &Status{
		CEXes: make(map[string]*CEXStatus, 0),
		Bots:  make([]*BotStatus, 0),
	}
	runningBots := m.runningBotsLookup()
	for _, rb := range runningBots {
		stats := rb.adaptor.stats()

		rb.botCfgMtx.RLock()
		cfg := rb.botCfg
		rb.botCfgMtx.RUnlock()

		status.Bots = append(status.Bots, &BotStatus{
			Config:   cfg,
			RunStats: stats,
		})
	}
	return status
}

func (m *MarketMaker) CEXBalance(cexName string, assetID uint32) (*libxc.ExchangeBalance, error) {
	cfg := m.defaultConfig()

	var cexCfg *CEXConfig
	for _, cex := range cfg.CexConfigs {
		if cexCfg.Name == cexName {
			cexCfg = cex
			break
		}
	}
	if cexCfg == nil {
		return nil, fmt.Errorf("no CEX config found for %s", cexName)
	}

	cex, err := m.loadAndConnectCEX(m.ctx, cexCfg)
	if err != nil {
		return nil, fmt.Errorf("error getting connected CEX: %w", err)
	}

	return cex.Balance(assetID)
}

// MarketReport returns information about the oracle rates on a market
// pair and the fiat rates of the base and quote assets.
func (m *MarketMaker) MarketReport(host string, baseID, quoteID uint32) (*MarketReport, error) {
	fiatRates := m.core.FiatConversionRates()
	baseFiatRate := fiatRates[baseID]
	quoteFiatRate := fiatRates[quoteID]

	price, oracles, err := m.oracle.getOracleInfo(baseID, quoteID)
	if err != nil {
		return nil, err
	}

	baseFeesEst, quoteFeesEst, err := marketFees(m.core, host, baseID, quoteID, false)
	if err != nil {
		return nil, err
	}

	baseFeesMax, quoteFeesMax, err := marketFees(m.core, host, baseID, quoteID, true)
	if err != nil {
		return nil, err
	}

	return &MarketReport{
		Price:         price,
		Oracles:       oracles,
		BaseFiatRate:  baseFiatRate,
		QuoteFiatRate: quoteFiatRate,
		BaseFees: &LotFeeRange{
			Max:       baseFeesMax,
			Estimated: baseFeesEst,
		},
		QuoteFees: &LotFeeRange{
			Max:       quoteFeesMax,
			Estimated: quoteFeesEst,
		},
	}, nil
}

func (m *MarketMaker) loginAndUnlockWallets(pw []byte, cfg *BotConfig) error {
	err := m.core.Login(pw)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	err = m.core.OpenWallet(cfg.BaseID, pw)
	if err != nil {
		return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.BaseID, err)
	}

	err = m.core.OpenWallet(cfg.QuoteID, pw)
	if err != nil {
		return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.QuoteID, err)
	}

	return nil
}

// loadAndConnectCEX initializes the centralizedExchange if required, and
// connects if not already connected.
func (m *MarketMaker) loadAndConnectCEX(ctx context.Context, cfg *CEXConfig) (*centralizedExchange, error) {
	c, err := m.loadCEX(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("error loading CEX: %w", err)
	}

	var cm *dex.ConnectionMaster
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.cm == nil || !c.cm.On() {
		cm = dex.NewConnectionMaster(c)
		c.cm = cm
	} else {
		cm = c.cm
	}

	if !cm.On() {
		c.connectErr = ""
		if err = cm.ConnectOnce(ctx); err != nil {
			c.connectErr = err.Error()
			return nil, fmt.Errorf("failed to connect to CEX: %v", err)
		}
		if c.mkts, err = c.Markets(ctx); err != nil {
			// Probably can't get here if we didn't error on connect, but
			// checking anyway.
			c.connectErr = err.Error()
			return nil, fmt.Errorf("error refreshing markets: %v", err)
		}
	}

	return c, nil
}

// loadCEX initializes the cex if required and returns the centralizedExchange.
func (m *MarketMaker) loadCEX(ctx context.Context, cfg *CEXConfig) (*centralizedExchange, error) {
	m.cexMtx.Lock()
	defer m.cexMtx.Unlock()
	var success bool
	if cex := m.cexes[cfg.Name]; cex != nil {
		if cex.APIKey == cfg.APIKey && cex.APISecret == cfg.APISecret {
			return cex, nil
		}
		if m.cexInUse(cfg.Name) {
			return nil, fmt.Errorf("CEX %s already in use with different API key", cfg.Name)
		}
		// New credentials. Delete the old cex.
		defer func() {
			if success {
				cex.mtx.Lock()
				cex.cm.Disconnect()
				cex.cm = nil
				cex.mtx.Unlock()
			}
		}()
	}
	logger := m.log.SubLogger(fmt.Sprintf("CEX-%s", cfg.Name))
	cex, err := libxc.NewCEX(cfg.Name, cfg.APIKey, cfg.APISecret, logger, m.core.Network())
	if err != nil {
		return nil, fmt.Errorf("failed to create CEX: %v", err)
	}
	c := &centralizedExchange{
		CEX:       cex,
		CEXConfig: cfg,
	}
	c.mkts, err = cex.Markets(ctx)
	if err != nil {
		m.log.Errorf("Failed to get markets for %s: %w", cfg.Name, err)
		c.mkts = make([]*libxc.Market, 0)
		c.connectErr = err.Error()
	}
	m.cexes[cfg.Name] = c
	success = true
	return c, nil
}

// cexList generates a slice of configured centralizedExchange.
func (m *MarketMaker) cexList() []*centralizedExchange {
	m.cexMtx.RLock()
	defer m.cexMtx.RUnlock()
	cexes := make([]*centralizedExchange, 0, len(m.cexes))
	for _, cex := range m.cexes {
		cexes = append(cexes, cex)
	}
	return cexes
}

func (m *MarketMaker) defaultConfig() *MarketMakingConfig {
	m.defaultCfgMtx.RLock()
	defer m.defaultCfgMtx.RUnlock()
	return m.defaultCfg.Copy()
}

func (m *MarketMaker) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	m.ctx = ctx
	cfg := m.defaultConfig()
	for _, cexCfg := range cfg.CexConfigs {
		if _, err := m.loadCEX(ctx, cexCfg); err != nil {
			m.log.Errorf("Error adding %s: %v", cexCfg.Name, err)
		}
	}

	eventLogDB, err := newBoltEventLogDB(ctx, m.eventLogDBPath, m.log.SubLogger("eventlogdb"))
	if err != nil {
		return nil, fmt.Errorf("error creating event log DB: %v", err)
	}
	m.eventLogDB = eventLogDB

	m.oracle = newPriceOracle(m.ctx, m.log.SubLogger("oracle"))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		for _, cex := range m.cexList() {
			cex.mtx.RLock()
			cm := cex.cm
			cex.mtx.RUnlock()
			if cm != nil {
				cm.Disconnect()
			}
			delete(m.cexes, cex.Name)
		}
	}()

	return &wg, nil
}

func (m *MarketMaker) balancesSufficient(balances *BotBalanceAllocation, mkt *MarketWithHost, cexCfg *CEXConfig) error {
	availableDEXBalances, availableCEXBalances, err := m.availableBalances(mkt, cexCfg)
	if err != nil {
		return fmt.Errorf("error getting available balances: %v", err)
	}

	for assetID, amount := range balances.DEX {
		availableBalance := availableDEXBalances[assetID]
		if amount > availableBalance {
			return fmt.Errorf("insufficient DEX balance for %s: %d < %d", dex.BipIDSymbol(assetID), availableBalance, amount)
		}
	}

	for assetID, amount := range balances.CEX {
		availableBalance := availableCEXBalances[assetID]
		if amount > availableBalance {
			return fmt.Errorf("insufficient CEX balance for %s: %d < %d", dex.BipIDSymbol(assetID), availableBalance, amount)
		}
	}

	return nil
}

// botCfgForMarket returns the configuration for a bot on a specific market.
// If alternateConfigPath is not nil, the configuration will be loaded from the
// file at that path.
func (m *MarketMaker) configsForMarket(mkt *MarketWithHost, alternateConfigPath *string) (botConfig *BotConfig, cexConfig *CEXConfig, err error) {
	fullCfg := m.defaultConfig()
	if alternateConfigPath != nil {
		fullCfg, err = getMarketMakingConfig(*alternateConfigPath)
		if err != nil {
			return nil, nil, fmt.Errorf("error loading custom market making config: %v", err)
		}
	}

	for _, c := range fullCfg.BotConfigs {
		if c.Host == mkt.Host && c.BaseID == mkt.BaseID && c.QuoteID == mkt.QuoteID {
			botConfig = c
		}
	}
	if botConfig == nil {
		return nil, nil, fmt.Errorf("no bot config found for %s", mkt)
	}

	if botConfig.CEXCfg != nil {
		for _, c := range fullCfg.CexConfigs {
			if c.Name == botConfig.CEXCfg.Name {
				cexConfig = c
			}
		}
		if cexConfig == nil {
			return nil, nil, fmt.Errorf("no CEX config found for %s", botConfig.CEXCfg.Name)
		}
	}

	return
}

func (m *MarketMaker) botSubLogger(cfg *BotConfig) dex.Logger {
	mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
	switch {
	case cfg.BasicMMConfig != nil:
		return m.log.SubLogger(fmt.Sprintf("MM-%s", mktID))
	case cfg.SimpleArbConfig != nil:
		return m.log.SubLogger(fmt.Sprintf("ARB-%s", mktID))
	case cfg.ArbMarketMakerConfig != nil:
		return m.log.SubLogger(fmt.Sprintf("AMM-%s", mktID))
	}
	// This will error in the caller.
	return m.log.SubLogger(fmt.Sprintf("Bot-%s", mktID))
}

func (m *MarketMaker) cexInUse(cexName string) bool {
	runningBots := m.runningBotsLookup()
	for _, bot := range runningBots {
		if bot.cexName() == cexName {
			return true
		}
	}
	return false
}

func (m *MarketMaker) newBot(cfg *BotConfig, adaptor *unifiedExchangeAdaptor, oracle oracle, log dex.Logger) (bot, error) {
	mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
	switch {
	case cfg.ArbMarketMakerConfig != nil:
		return newArbMarketMaker(cfg, adaptor, adaptor, m.log.SubLogger(fmt.Sprintf("AMM-%s", mktID)))
	case cfg.BasicMMConfig != nil:
		return newBasicMarketMaker(cfg, adaptor, m.oracle, m.log.SubLogger(fmt.Sprintf("MM-%s", mktID)))
	case cfg.SimpleArbConfig != nil:
		return newSimpleArbMarketMaker(cfg, adaptor, adaptor, m.log.SubLogger(fmt.Sprintf("ARB-%s", mktID)))
	default:
		return nil, fmt.Errorf("not bot config found")
	}
}

// StartBot starts a market making bot.
func (m *MarketMaker) StartBot(mkt *MarketWithHost, allocation *BotBalanceAllocation, alternateConfigPath *string, pw []byte) (err error) {
	m.startUpdateMtx.Lock()
	defer m.startUpdateMtx.Unlock()

	m.runningBotsMtx.RLock()
	_, found := m.runningBots[*mkt]
	m.runningBotsMtx.RUnlock()
	if found {
		return fmt.Errorf("bot for %s already running", mkt)
	}

	botCfg, cexCfg, err := m.configsForMarket(mkt, alternateConfigPath)
	if err != nil {
		return err
	}

	if err := m.balancesSufficient(allocation, mkt, cexCfg); err != nil {
		return err
	}

	if err := m.loginAndUnlockWallets(pw, botCfg); err != nil {
		return err
	}

	var cex *centralizedExchange
	if cexCfg != nil {
		cex, err = m.loadAndConnectCEX(m.ctx, cexCfg)
		if err != nil {
			return fmt.Errorf("error loading %s: %w", cexCfg.Name, err)
		}
	}

	var startedBot bool

	requiresOracle := botCfg.requiresPriceOracle()
	if requiresOracle {
		err := m.oracle.startAutoSyncingMarket(botCfg.BaseID, botCfg.QuoteID)
		if err != nil {
			return err
		}
		defer func() {
			if !startedBot {
				m.oracle.stopAutoSyncingMarket(botCfg.BaseID, botCfg.QuoteID)
			}
		}()
	}

	mktID := dexMarketID(botCfg.Host, botCfg.BaseID, botCfg.QuoteID)
	logger := m.botSubLogger(botCfg)
	exchangeAdaptor := unifiedExchangeAdaptorForBot(&exchangeAdaptorCfg{
		botID:           mktID,
		market:          mkt,
		baseDexBalances: allocation.DEX,
		baseCexBalances: allocation.CEX,
		core:            m.core,
		cex:             cex,
		log:             logger,
		botCfg:          botCfg,
		eventLogDB:      m.eventLogDB,
	})

	adaptorCm := dex.NewConnectionMaster(exchangeAdaptor)
	err = adaptorCm.ConnectOnce(m.ctx)
	if err != nil {
		return err
	}

	bot, err := m.newBot(botCfg, exchangeAdaptor, m.oracle, logger)
	if err != nil {
		return err
	}

	botCm, err := newBotConnectionMaster(m.ctx, bot)
	if err != nil {
		return err
	}

	go func() {
		<-botCm.done
		adaptorCm.Disconnect()
		m.runningBotsMtx.Lock()
		if bot, found := m.runningBots[*mkt]; found {
			bot.botCfgMtx.RLock()
			if bot.botCfg.requiresPriceOracle() {
				m.oracle.stopAutoSyncingMarket(mkt.BaseID, mkt.QuoteID)
			}
			bot.botCfgMtx.RUnlock()
			delete(m.runningBots, *mkt)
		}
		m.runningBotsMtx.Unlock()
		m.core.Broadcast(newRunStatsNote(mkt.Host, mkt.BaseID, mkt.QuoteID, nil))
	}()

	startedBot = true

	m.runningBotsMtx.Lock()
	m.runningBots[*mkt] = &runningBot{
		adaptor: exchangeAdaptor,
		botCm:   botCm,
		botCfg:  botCfg,
		cexCfg:  cexCfg,
	}
	m.runningBotsMtx.Unlock()
	exchangeAdaptor.sendStatsUpdate()

	return nil
}

// StopBot stops a running bot.
func (m *MarketMaker) StopBot(mkt *MarketWithHost) error {
	runningBots := m.runningBotsLookup()
	bot, found := runningBots[*mkt]
	if !found {
		return fmt.Errorf("no bot running on market: %s", mkt)
	}
	bot.botCm.disconnect()
	return nil
}

func getMarketMakingConfig(path string) (*MarketMakingConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("no config file provided")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &MarketMakingConfig{}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (m *MarketMaker) writeConfigFile(cfg *MarketMakingConfig) error {
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("error marshalling market making config: %v", err)
	}

	err = os.WriteFile(m.defaultCfgPath, data, 0644)
	if err != nil {
		return fmt.Errorf("error writing market making config: %v", err)
	}
	m.defaultCfgMtx.Lock()
	m.defaultCfg = cfg
	m.defaultCfgMtx.Unlock()
	return nil
}

func (m *MarketMaker) updateDefaultBotConfig(updatedCfg *BotConfig) {
	cfg := m.defaultConfig()

	var updated bool
	for i, c := range cfg.BotConfigs {
		if c.Host == updatedCfg.Host && c.QuoteID == updatedCfg.QuoteID && c.BaseID == updatedCfg.BaseID {
			cfg.BotConfigs[i] = updatedCfg
			updated = true
			break
		}
	}
	if !updated {
		cfg.BotConfigs = append(cfg.BotConfigs, updatedCfg)
	}

	if err := m.writeConfigFile(cfg); err != nil {
		m.log.Errorf("Error saving configuration file: %v", err)
	}
}

// UpdateBotConfig updates the configuration for one of the bots.
func (m *MarketMaker) UpdateBotConfig(updatedCfg *BotConfig) error {
	m.runningBotsMtx.RLock()
	_, running := m.runningBots[MarketWithHost{updatedCfg.Host, updatedCfg.BaseID, updatedCfg.QuoteID}]
	m.runningBotsMtx.RUnlock()
	if running {
		return fmt.Errorf("call UpdateRunningBotCfg to update the config of a running bot")
	}

	m.updateDefaultBotConfig(updatedCfg)
	return nil
}

func (m *MarketMaker) UpdateCEXConfig(updatedCfg *CEXConfig) error {
	_, err := m.loadAndConnectCEX(m.ctx, updatedCfg)
	if err != nil {
		return fmt.Errorf("error loading %s with updated config: %w", updatedCfg.Name, err)
	}

	var updated bool
	m.defaultCfgMtx.Lock()
	for i, c := range m.defaultCfg.CexConfigs {
		if c.Name == updatedCfg.Name {
			m.defaultCfg.CexConfigs[i] = updatedCfg
			updated = true
			break
		}
	}
	if !updated {
		m.defaultCfg.CexConfigs = append(m.defaultCfg.CexConfigs, updatedCfg)
	}
	m.defaultCfgMtx.Unlock()
	if err := m.writeConfigFile(m.defaultCfg); err != nil {
		m.log.Errorf("Error saving new bot configuration: %w", err)
	}

	return nil
}

// RemoveConfig removes a bot config from the market making config.
func (m *MarketMaker) RemoveBotConfig(host string, baseID, quoteID uint32) error {
	cfg := m.defaultConfig()

	var updated bool
	for i, c := range cfg.BotConfigs {
		if c.Host == host && c.QuoteID == quoteID && c.BaseID == baseID {
			cfg.BotConfigs = append(cfg.BotConfigs[:i], cfg.BotConfigs[i+1:]...)
			updated = true
			break
		}
	}
	if !updated {
		return fmt.Errorf("config not found")
	}

	if err := m.writeConfigFile(cfg); err != nil {
		m.log.Errorf("Error saving updated config file: %v", err)
	}

	return nil
}

func validRunningBotCfgUpdate(oldCfg, newCfg *BotConfig) error {
	if oldCfg.CEXCfg != nil && newCfg.CEXCfg == nil {
		return fmt.Errorf("cannot remove CEX config from running bot")
	}

	if oldCfg.CEXCfg != nil && (oldCfg.CEXCfg.Name != newCfg.CEXCfg.Name) {
		return fmt.Errorf("cannot change CEX config for running bot")
	}

	if oldCfg.BasicMMConfig == nil != (newCfg.BasicMMConfig == nil) {
		return fmt.Errorf("cannot change bot type for running bot")
	}

	if oldCfg.SimpleArbConfig == nil != (newCfg.SimpleArbConfig == nil) {
		return fmt.Errorf("cannot change bot type for running bot")
	}

	if oldCfg.ArbMarketMakerConfig == nil != (newCfg.ArbMarketMakerConfig == nil) {
		return fmt.Errorf("cannot change bot type for running bot")
	}

	return nil
}

// UpdateRunningBotInventory updates the inventory of a running bot.
func (m *MarketMaker) UpdateRunningBotInventory(mkt *MarketWithHost, balanceDiffs *BotInventoryDiffs) error {
	m.startUpdateMtx.Lock()
	defer m.startUpdateMtx.Unlock()

	m.runningBotsMtx.RLock()
	runningBot := m.runningBots[*mkt]
	m.runningBotsMtx.RUnlock()
	if runningBot == nil {
		return fmt.Errorf("no bot running on market: %s", mkt)
	}

	if err := m.balancesSufficient(balanceDiffsToAllocation(balanceDiffs), mkt, runningBot.cexCfg); err != nil {
		return err
	}

	if err := runningBot.botCm.pause(); err != nil {
		return err
	}

	runningBot.adaptor.updateInventory(balanceDiffs)

	if err := runningBot.botCm.resume(); err != nil {
		return err
	}

	return nil
}

// UpdateRunningBotCfg updates the configuration and balance allocation for a
// running bot. If saveUpdate is true, the update configuration will be saved
// to the default config file.
func (m *MarketMaker) UpdateRunningBotCfg(cfg *BotConfig, balanceDiffs *BotInventoryDiffs, saveUpdate bool) error {
	m.startUpdateMtx.Lock()
	defer m.startUpdateMtx.Unlock()

	if cfg == nil {
		return fmt.Errorf("nil config")
	}

	mkt := MarketWithHost{cfg.Host, cfg.BaseID, cfg.QuoteID}
	m.runningBotsMtx.RLock()
	runningBot := m.runningBots[mkt]
	m.runningBotsMtx.RUnlock()
	if runningBot == nil {
		return fmt.Errorf("no bot running on market: %s", mkt)
	}

	runningBot.botCfgMtx.RLock()
	oldCfg := runningBot.botCfg
	runningBot.botCfgMtx.RUnlock()
	if err := validRunningBotCfgUpdate(oldCfg, cfg); err != nil {
		return err
	}

	if balanceDiffs != nil {
		if err := m.balancesSufficient(balanceDiffsToAllocation(balanceDiffs), &mkt, runningBot.cexCfg); err != nil {
			return err
		}
	}

	var stoppedOracle, startedOracle, updateSuccess bool
	defer func() {
		if updateSuccess {
			return
		}
		if startedOracle {
			m.oracle.stopAutoSyncingMarket(cfg.BaseID, cfg.QuoteID)
		} else if stoppedOracle {
			err := m.oracle.startAutoSyncingMarket(oldCfg.BaseID, oldCfg.QuoteID)
			if err != nil {
				m.log.Errorf("Error restarting oracle for %s: %v", mkt, err)
			}
		}
	}()

	if !oldCfg.requiresPriceOracle() && cfg.requiresPriceOracle() {
		err := m.oracle.startAutoSyncingMarket(cfg.BaseID, cfg.QuoteID)
		if err != nil {
			return err
		}
		startedOracle = true
	} else if oldCfg.requiresPriceOracle() && !cfg.requiresPriceOracle() {
		m.oracle.stopAutoSyncingMarket(cfg.BaseID, cfg.QuoteID)
		stoppedOracle = true
	}

	if err := runningBot.botCm.pause(); err != nil {
		return err
	}

	if updateErr := runningBot.botCm.updateConfig(cfg); updateErr != nil {
		if resumeErr := runningBot.botCm.resume(); resumeErr != nil {
			m.log.Errorf("Error resuming bot after failed config update: %v", resumeErr)
		}
		return updateErr
	}

	runningBot.adaptor.updateConfig(cfg)
	if balanceDiffs != nil {
		runningBot.adaptor.updateInventory(balanceDiffs)
	}

	if err := runningBot.botCm.resume(); err != nil {
		return err
	}

	runningBot.botCfgMtx.Lock()
	runningBot.botCfg = cfg
	runningBot.botCfgMtx.Unlock()

	updateSuccess = true

	return nil
}

// ArchivedRuns returns all archived market making runs.
func (m *MarketMaker) ArchivedRuns() ([]*MarketMakingRun, error) {
	allRuns, err := m.eventLogDB.runs(0, nil, nil)
	if err != nil {
		return nil, err
	}

	runningBots := m.runningBotsLookup()
	archivedRuns := make([]*MarketMakingRun, 0, len(allRuns))
	for _, run := range allRuns {
		runningBot := runningBots[*run.Market]
		if runningBot == nil || runningBot.adaptor.timeStart() != run.StartTime {
			archivedRuns = append(archivedRuns, run)
		}
	}

	return archivedRuns, nil
}

// RunOverview returns the overview of a market making run.
func (m *MarketMaker) RunOverview(startTime int64, mkt *MarketWithHost) (*MarketMakingRunOverview, error) {
	return m.eventLogDB.runOverview(startTime, mkt)
}

// RunLogs returns the event logs of a market making run. At most n events are
// returned, if n == 0 then all events are returned. If refID is not nil, then
// the events including and after refID are returned.
func (m *MarketMaker) RunLogs(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64) ([]*MarketMakingEvent, error) {
	return m.eventLogDB.runEvents(startTime, mkt, n, refID, false)
}

// LotFees are the fees for trading one lot.
type LotFees struct {
	Swap   uint64 `json:"swap"`
	Redeem uint64 `json:"redeem"`
	Refund uint64 `json:"refund"`
}

// LotFeeRange combine the estimated and maximum LotFees.
type LotFeeRange struct {
	Max       *LotFees `json:"max"`
	Estimated *LotFees `json:"estimated"`
}

// marketFees calculates the LotFees for the base and quote assets.
func marketFees(c clientCore, host string, baseID, quoteID uint32, useMaxFeeRate bool) (baseFees, quoteFees *LotFees, _ error) {
	buySwapFees, buyRedeemFees, buyRefundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          baseID,
		Quote:         quoteID,
		UseMaxFeeRate: useMaxFeeRate,
		UseSafeTxSize: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get buy single lot fees: %v", err)
	}

	sellSwapFees, sellRedeemFees, sellRefundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          baseID,
		Quote:         quoteID,
		UseMaxFeeRate: useMaxFeeRate,
		UseSafeTxSize: true,
		Sell:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get sell single lot fees: %v", err)
	}

	return &LotFees{
			Swap:   sellSwapFees,
			Redeem: buyRedeemFees,
			Refund: sellRefundFees,
		}, &LotFees{
			Swap:   buySwapFees,
			Redeem: sellRedeemFees,
			Refund: buyRefundFees,
		}, nil
}

func (m *MarketMaker) availableBalances(mkt *MarketWithHost, cexCfg *CEXConfig) (dexBalances, cexBalances map[uint32]uint64, _ error) {
	dexAssets := make(map[uint32]interface{})
	cexAssets := make(map[uint32]interface{})

	dexAssets[mkt.BaseID] = struct{}{}
	dexAssets[mkt.QuoteID] = struct{}{}
	dexAssets[feeAsset(mkt.BaseID)] = struct{}{}
	dexAssets[feeAsset(mkt.QuoteID)] = struct{}{}

	if cexCfg != nil {
		cexAssets[mkt.BaseID] = struct{}{}
		cexAssets[mkt.QuoteID] = struct{}{}
	}

	checkTotalBalances := func() (dexBals, cexBals map[uint32]uint64, err error) {
		dexBals = make(map[uint32]uint64, len(dexAssets))
		cexBals = make(map[uint32]uint64, len(cexAssets))

		for assetID := range dexAssets {
			bal, err := m.core.AssetBalance(assetID)
			if err != nil {
				return nil, nil, err
			}
			dexBals[assetID] = bal.Available
		}

		if cexCfg != nil {
			cex, err := m.loadAndConnectCEX(m.ctx, cexCfg)
			if err != nil {
				return nil, nil, err
			}

			for assetID := range cexAssets {
				balance, err := cex.Balance(assetID)
				if err != nil {
					return nil, nil, err
				}

				cexBals[assetID] = balance.Available
			}
		}

		return dexBals, cexBals, nil
	}

	checkBot := func(bot *runningBot) bool {
		botAssets := bot.assets()
		for assetID := range dexAssets {
			if _, found := botAssets[assetID]; found {
				return true
			}
		}
		return false
	}

	balancesEqual := func(bal1, bal2 map[uint32]uint64) bool {
		if len(bal1) != len(bal2) {
			return false
		}
		for assetID, bal := range bal1 {
			if bal2[assetID] != bal {
				return false
			}
		}
		return true
	}

	// We first check the available balances in the DEX wallets and on
	// the CEX, then check the amounts reserved by the running bots,
	// and then recheck the amounts available on the DEX and CEX. If
	// the available balances in the first and last checks are equal,
	// then we know that nothing has changed. If not, we try again.
	totalDEXBalances, totalCEXBalances, err := checkTotalBalances()
	if err != nil {
		return nil, nil, err
	}

	const maxTries = 5
	for i := 0; i < maxTries; i++ {
		reservedDEXBalances := make(map[uint32]uint64, len(dexAssets))
		reservedCEXBalances := make(map[uint32]uint64, len(cexAssets))

		runningBots := m.runningBotsLookup()
		for _, bot := range runningBots {
			if !checkBot(bot) {
				continue
			}

			bot.adaptor.refreshAllPendingEvents(m.ctx)

			for assetID := range dexAssets {
				botBalance := bot.adaptor.DEXBalance(assetID)
				reservedDEXBalances[assetID] += botBalance.Available
			}

			if cexCfg != nil && bot.cexName() == cexCfg.Name {
				for assetID := range cexAssets {
					botBalance := bot.adaptor.CEXBalance(assetID)
					reservedCEXBalances[assetID] += botBalance.Available + botBalance.Reserved
				}
			}
		}

		updatedDEXBalances, updatedCEXBalances, err := checkTotalBalances()
		if err != nil {
			return nil, nil, err
		}

		if balancesEqual(updatedDEXBalances, totalDEXBalances) && balancesEqual(updatedCEXBalances, totalCEXBalances) {
			for assetID, bal := range reservedDEXBalances {
				if bal > totalDEXBalances[assetID] {
					m.log.Warnf("reserved DEX balance for %s exceeds available balance: %d > %d", dex.BipIDSymbol(assetID), bal, totalDEXBalances[assetID])
					totalDEXBalances[assetID] = 0
				} else {
					totalDEXBalances[assetID] -= bal
				}
			}
			for assetID, bal := range reservedCEXBalances {
				if bal > totalCEXBalances[assetID] {
					m.log.Warnf("reserved CEX balance for %s exceeds available balance: %d > %d", dex.BipIDSymbol(assetID), bal, totalCEXBalances[assetID])
					totalCEXBalances[assetID] = 0
				} else {
					totalCEXBalances[assetID] -= bal
				}
			}
			return totalDEXBalances, totalCEXBalances, nil
		}

		totalDEXBalances = updatedDEXBalances
		totalCEXBalances = updatedCEXBalances
	}

	return nil, nil, fmt.Errorf("failed to get available balances after %d tries", maxTries)
}

// AvailableBalances returns the available balances of assets relevant to
// market making on the specified market on the DEX (including fee assets),
// and optionally a CEX depending on the configured strategy.
func (m *MarketMaker) AvailableBalances(mkt *MarketWithHost, alternateConfigPath *string) (dexBalances, cexBalances map[uint32]uint64, _ error) {
	_, cexCfg, err := m.configsForMarket(mkt, alternateConfigPath)
	if err != nil {
		return nil, nil, err
	}

	return m.availableBalances(mkt, cexCfg)
}
