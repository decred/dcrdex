// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	NotificationFeed() *core.NoteFeed
	ExchangeMarket(host string, base, quote uint32) (*core.Market, error)
	SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error)
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

// centralizedExchange is used to manage an exchange API connection.
type centralizedExchange struct {
	libxc.CEX
	*CEXConfig

	mtx        sync.RWMutex
	cm         *dex.ConnectionMaster
	mkts       []*libxc.Market
	connectErr string
}

// MarketMaker handles the market making process. It supports running different
// strategies on different markets.
type MarketMaker struct {
	ctx        context.Context
	die        atomic.Value // context.CancelFunc
	running    atomic.Bool
	log        dex.Logger
	core       clientCore
	cfgPath    string
	eventLogDB eventLogDB

	cfgMtx sync.RWMutex
	cfg    *MarketMakingConfig

	// syncedOracle is only available while the MarketMaker is running. It
	// periodically refreshes the prices for the markets that have bots
	// running on them.
	syncedOracleMtx sync.RWMutex
	syncedOracle    *priceOracle

	// unsyncedOracle is always available and can be used to query prices on
	// all markets. It does not periodically refresh the prices, and queries
	// them on demand.
	unsyncedOracle *priceOracle

	runningBotsMtx sync.RWMutex
	runningBots    map[MarketWithHost]*unifiedExchangeAdaptor

	cexMtx sync.RWMutex
	cexes  map[string]*centralizedExchange
}

// NewMarketMaker creates a new MarketMaker.
func NewMarketMaker(ctx context.Context, c clientCore, eventLogDBPath, cfgPath string, log dex.Logger) (*MarketMaker, error) {
	var cfg MarketMakingConfig
	if b, err := os.ReadFile(cfgPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error reading config file from %q: %w", cfgPath, err)
	} else if len(b) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("error unmarshaling config file: %v", err)
		}
	}

	eventLogDB, err := newBoltEventLogDB(ctx, eventLogDBPath, log.SubLogger("eventlogdb"))
	if err != nil {
		return nil, fmt.Errorf("error creating event log DB: %v", err)
	}

	m := &MarketMaker{
		core:           c,
		log:            log,
		cfgPath:        cfgPath,
		cfg:            &cfg,
		running:        atomic.Bool{},
		runningBots:    make(map[MarketWithHost]*unifiedExchangeAdaptor),
		unsyncedOracle: newUnsyncedPriceOracle(log),
		cexes:          make(map[string]*centralizedExchange),
		eventLogDB:     eventLogDB,
	}
	m.die.Store(context.CancelFunc(func() {}))

	go func() {
		<-ctx.Done()
		m.die.Load().(context.CancelFunc)()
	}()

	return m, nil
}

// Running returns true if the MarketMaker is running.
func (m *MarketMaker) Running() bool {
	return m.running.Load()
}

// runningBotsLookup returns a lookup map for running bots.
func (m *MarketMaker) runningBotsLookup() map[MarketWithHost]*unifiedExchangeAdaptor {
	m.runningBotsMtx.RLock()
	defer m.runningBotsMtx.RUnlock()

	mkts := make(map[MarketWithHost]*unifiedExchangeAdaptor, len(m.runningBots))
	for mkt, adaptor := range m.runningBots {
		mkts[mkt] = adaptor
	}

	return mkts
}

// Status is state information about the MarketMaker.
type Status struct {
	Running bool                  `json:"running"`
	Bots    []*BotStatus          `json:"bots"`
	CEXes   map[string]*CEXStatus `json:"cexes"`
}

// CEXSTatus is state information about a cex.
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

// Status generates a Status for the MarketMaker.
func (m *MarketMaker) Status() *Status {
	cfg := m.config()
	status := &Status{
		Running: m.running.Load(),
		CEXes:   make(map[string]*CEXStatus, len(cfg.CexConfigs)),
		Bots:    make([]*BotStatus, 0, len(cfg.BotConfigs)),
	}
	runningBots := m.runningBotsLookup()
	for _, botCfg := range cfg.BotConfigs {
		mkt := MarketWithHost{botCfg.Host, botCfg.BaseID, botCfg.QuoteID}
		adaptor := runningBots[mkt]
		var stats *RunStats
		if adaptor != nil {
			var err error
			stats, err = adaptor.stats()
			if err != nil {
				m.log.Errorf("Error getting stats for %s: %v", mkt, err)
			}
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

func marketsRequiringPriceOracle(cfgs []*BotConfig) []*mkt {
	mkts := make([]*mkt, 0, len(cfgs))

	for _, cfg := range cfgs {
		if cfg.requiresPriceOracle() {
			mkts = append(mkts, &mkt{baseID: cfg.BaseID, quoteID: cfg.QuoteID})
		}
	}

	return mkts
}

func priceOracleFromConfigs(ctx context.Context, cfgs []*BotConfig, log dex.Logger) (*priceOracle, error) {
	var oracle *priceOracle
	var err error
	marketsRequiringOracle := marketsRequiringPriceOracle(cfgs)
	if len(marketsRequiringOracle) > 0 {
		oracle, err = newAutoSyncPriceOracle(ctx, marketsRequiringOracle, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create PriceOracle: %v", err)
		}
	}

	return oracle, nil
}

func (m *MarketMaker) markBotAsRunning(mkt MarketWithHost, adaptor *unifiedExchangeAdaptor) {
	m.runningBotsMtx.Lock()
	defer m.runningBotsMtx.Unlock()
	if adaptor != nil {
		m.runningBots[mkt] = adaptor
	} else {
		delete(m.runningBots, mkt)
	}

	var stats *RunStats
	if adaptor != nil {
		var err error
		stats, err = adaptor.stats()
		if err != nil {
			m.log.Errorf("Error getting stats for %s: %v", mkt, err)
		}
	}
	m.core.Broadcast(newRunStatsNote(mkt.Host, mkt.BaseID, mkt.QuoteID, stats))

	if len(m.runningBots) == 0 {
		m.Stop()
	}
}

func (m *MarketMaker) CEXBalance(cexName string, assetID uint32) (*libxc.ExchangeBalance, error) {
	cex, err := m.connectedCEX(cexName)
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

	m.syncedOracleMtx.RLock()
	if m.syncedOracle != nil {
		price, oracles, err := m.syncedOracle.getOracleInfo(baseID, quoteID)
		if err != nil && !errors.Is(err, errUnsyncedMarket) {
			m.log.Errorf("failed to get oracle info for market %d-%d: %v", baseID, quoteID, err)
		}
		if err == nil {
			m.syncedOracleMtx.RUnlock()
			return &MarketReport{
				Price:         price,
				Oracles:       oracles,
				BaseFiatRate:  baseFiatRate,
				QuoteFiatRate: quoteFiatRate,
			}, nil
		}
	}
	m.syncedOracleMtx.RUnlock()

	price, oracles, err := m.unsyncedOracle.getOracleInfo(baseID, quoteID)
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

func (m *MarketMaker) loginAndUnlockWallets(pw []byte, cfgs []*BotConfig) error {
	err := m.core.Login(pw)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}
	unlocked := make(map[uint32]any)
	for _, cfg := range cfgs {
		if _, done := unlocked[cfg.BaseID]; !done {
			err := m.core.OpenWallet(cfg.BaseID, pw)
			if err != nil {
				return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.BaseID, err)
			}
			unlocked[cfg.BaseID] = true
		}

		if _, done := unlocked[cfg.QuoteID]; !done {
			err := m.core.OpenWallet(cfg.QuoteID, pw)
			if err != nil {
				return fmt.Errorf("failed to unlock wallet for asset %d: %w", cfg.QuoteID, err)
			}
			unlocked[cfg.QuoteID] = true
		}
	}

	return nil
}

// duplicateBotConfig returns an error if there is more than one bot config for
// the same market on the same dex host.
func duplicateBotConfig(cfgs []*BotConfig) error {
	mkts := make(map[string]struct{})

	for _, cfg := range cfgs {
		mkt := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
		if _, found := mkts[mkt]; found {
			return fmt.Errorf("duplicate bot config for market %s", mkt)
		}
		mkts[mkt] = struct{}{}
	}

	return nil
}

func validateAndFilterEnabledConfigs(cfgs []*BotConfig) ([]*BotConfig, error) {
	enabledCfgs := make([]*BotConfig, 0, len(cfgs))
	for _, cfg := range cfgs {
		if cfg.requiresCEX() && cfg.CEXCfg == nil {
			mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
			return nil, fmt.Errorf("bot at %s requires cex config", mktID)
		}
		if !cfg.Disabled {
			enabledCfgs = append(enabledCfgs, cfg)
		}
	}
	if len(enabledCfgs) == 0 {
		return nil, errors.New("no enabled bots")
	}
	if err := duplicateBotConfig(enabledCfgs); err != nil {
		return nil, err
	}
	return enabledCfgs, nil
}

// botInitialBaseBalances returns the initial base balances for each bot.
func botInitialBaseBalances(cfgs []*BotConfig, core clientCore, cexes map[string]*centralizedExchange) (dexBalances, cexBalances map[string]map[uint32]uint64, err error) {
	dexBalances = make(map[string]map[uint32]uint64, len(cfgs))
	cexBalances = make(map[string]map[uint32]uint64, len(cfgs))

	type trackedBalance struct {
		available uint64
		reserved  uint64
	}

	dexBalanceTracker := make(map[uint32]*trackedBalance)
	cexBalanceTracker := make(map[string]map[string]*trackedBalance)

	trackAssetOnDEX := func(assetID uint32) error {
		if _, found := dexBalanceTracker[assetID]; found {
			return nil
		}
		bal, err := core.AssetBalance(assetID)
		if err != nil {
			return fmt.Errorf("failed to get balance for asset %d: %v", assetID, err)
		}
		dexBalanceTracker[assetID] = &trackedBalance{
			available: bal.Available,
		}
		return nil
	}

	trackAssetOnCEX := func(assetSymbol string, assetID uint32, cexName string) error {
		cexBalances, found := cexBalanceTracker[cexName]
		if !found {
			cexBalanceTracker[cexName] = make(map[string]*trackedBalance)
			cexBalances = cexBalanceTracker[cexName]
		}

		if _, found := cexBalances[assetSymbol]; found {
			return nil
		}

		cex, found := cexes[cexName]
		if !found {
			return fmt.Errorf("no cex config for %s", cexName)
		}

		// TODO: what if conversion factors of an asset on different chains
		// are different? currently they are all the same.
		balance, err := cex.Balance(assetID)
		if err != nil {
			return err
		}
		cexBalances[assetSymbol] = &trackedBalance{
			available: balance.Available,
		}
		return nil
	}

	calcBalance := func(balType BalanceType, balAmount float64, availableBal uint64) uint64 {
		if balType == Percentage {
			return uint64(math.Round(float64(availableBal) * balAmount / 100))
		}
		return uint64(math.Round(balAmount))
	}

	for _, cfg := range cfgs {
		err := trackAssetOnDEX(cfg.BaseID)
		if err != nil {
			return nil, nil, err
		}
		err = trackAssetOnDEX(cfg.QuoteID)
		if err != nil {
			return nil, nil, err
		}

		mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)

		// Calculate DEX balances
		baseBalance := dexBalanceTracker[cfg.BaseID]
		quoteBalance := dexBalanceTracker[cfg.QuoteID]
		baseRequired := calcBalance(cfg.BaseBalanceType, cfg.BaseBalance, baseBalance.available)
		quoteRequired := calcBalance(cfg.QuoteBalanceType, cfg.QuoteBalance, quoteBalance.available)

		if baseRequired == 0 && quoteRequired == 0 {
			return nil, nil, fmt.Errorf("both base and quote balance are zero for market %s", mktID)
		}
		if baseRequired > baseBalance.available-baseBalance.reserved {
			return nil, nil, fmt.Errorf("insufficient balance for asset %d", cfg.BaseID)
		}
		if quoteRequired > quoteBalance.available-quoteBalance.reserved {
			return nil, nil, fmt.Errorf("insufficient balance for asset %d", cfg.QuoteID)
		}
		baseBalance.reserved += baseRequired
		quoteBalance.reserved += quoteRequired

		dexBalances[mktID] = map[uint32]uint64{
			cfg.BaseID:  baseRequired,
			cfg.QuoteID: quoteRequired,
		}

		trackTokenFeeAsset := func(base bool) error {
			assetID := cfg.QuoteID
			balType := cfg.QuoteFeeAssetBalanceType
			balAmount := cfg.QuoteFeeAssetBalance
			baseOrQuote := "quote"
			if base {
				assetID = cfg.BaseID
				balType = cfg.BaseFeeAssetBalanceType
				balAmount = cfg.BaseFeeAssetBalance
				baseOrQuote = "base"
			}
			token := asset.TokenInfo(assetID)
			if token == nil {
				return nil
			}
			err := trackAssetOnDEX(token.ParentID)
			if err != nil {
				return err
			}
			tokenFeeAsset := dexBalanceTracker[token.ParentID]
			dexBalances[mktID][token.ParentID] += calcBalance(balType, balAmount, tokenFeeAsset.available)
			tokenFeeAssetRequired := dexBalances[mktID][token.ParentID]
			if tokenFeeAssetRequired == 0 {
				return fmt.Errorf("%s fee asset balance is zero for market %s", baseOrQuote, mktID)
			}
			if tokenFeeAssetRequired > tokenFeeAsset.available-tokenFeeAsset.reserved {
				return fmt.Errorf("insufficient balance for fee asset %d", token.ParentID)
			}
			tokenFeeAsset.reserved += tokenFeeAssetRequired
			return nil
		}
		err = trackTokenFeeAsset(true)
		if err != nil {
			return nil, nil, err
		}
		err = trackTokenFeeAsset(false)
		if err != nil {
			return nil, nil, err
		}

		// Calculate CEX balances
		if cfg.CEXCfg != nil {
			baseSymbol := dex.BipIDSymbol(cfg.BaseID)
			if baseSymbol == "" {
				return nil, nil, fmt.Errorf("unknown asset ID %d", cfg.BaseID)
			}
			baseAssetSymbol := dex.TokenSymbol(baseSymbol)

			quoteSymbol := dex.BipIDSymbol(cfg.QuoteID)
			if quoteSymbol == "" {
				return nil, nil, fmt.Errorf("unknown asset ID %d", cfg.QuoteID)
			}
			quoteAssetSymbol := dex.TokenSymbol(quoteSymbol)

			err = trackAssetOnCEX(baseAssetSymbol, cfg.BaseID, cfg.CEXCfg.Name)
			if err != nil {
				return nil, nil, err
			}
			err = trackAssetOnCEX(quoteAssetSymbol, cfg.QuoteID, cfg.CEXCfg.Name)
			if err != nil {
				return nil, nil, err
			}
			baseCEXBalance := cexBalanceTracker[cfg.CEXCfg.Name][baseAssetSymbol]
			quoteCEXBalance := cexBalanceTracker[cfg.CEXCfg.Name][quoteAssetSymbol]
			cexBaseRequired := calcBalance(cfg.CEXCfg.BaseBalanceType, cfg.CEXCfg.BaseBalance, baseCEXBalance.available)
			cexQuoteRequired := calcBalance(cfg.CEXCfg.QuoteBalanceType, cfg.CEXCfg.QuoteBalance, quoteCEXBalance.available)
			if cexBaseRequired == 0 && cexQuoteRequired == 0 {
				return nil, nil, fmt.Errorf("both base and quote CEX balances are zero for market %s", mktID)
			}
			if cexBaseRequired > baseCEXBalance.available-baseCEXBalance.reserved {
				return nil, nil, fmt.Errorf("insufficient CEX base balance for asset %d", cfg.BaseID)
			}
			if cexQuoteRequired > quoteCEXBalance.available-quoteCEXBalance.reserved {
				return nil, nil, fmt.Errorf("insufficient CEX quote balance for asset %d", cfg.QuoteID)
			}
			baseCEXBalance.reserved += cexBaseRequired
			quoteCEXBalance.reserved += cexQuoteRequired

			cexBalances[mktID] = map[uint32]uint64{
				cfg.BaseID:  cexBaseRequired,
				cfg.QuoteID: cexQuoteRequired,
			}
		}
	}

	return dexBalances, cexBalances, nil
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

// connectedCEX returns the connected centralizedExchange, initializing and
// connecting if required.
func (m *MarketMaker) connectedCEX(cexName string) (*centralizedExchange, error) {
	cfg := m.cexConfig(cexName)
	if cfg == nil {
		return nil, fmt.Errorf("CEX %q not known", cexName)
	}
	cex, err := m.loadAndConnectCEX(m.ctx, cfg)
	return cex, err
}

// initCEXConnections initializes and connects to the specified cexes.
func (m *MarketMaker) initCEXConnections(ctx context.Context, cfgs []*CEXConfig) map[string]*centralizedExchange {
	cexes := make(map[string]*centralizedExchange)

	for _, cfg := range cfgs {
		c, err := m.loadAndConnectCEX(ctx, cfg)
		if err != nil {
			m.log.Errorf("Failed to create %s: %v", cfg.Name, err)
			continue
		}
		cexes[c.Name] = c
	}

	return cexes
}

func (m *MarketMaker) logInitialBotBalances(dexBalances, cexBalances map[string]map[uint32]uint64) {
	var msg strings.Builder
	msg.WriteString("Initial market making balances:\n")
	for mkt, botDexBals := range dexBalances {
		msg.WriteString(fmt.Sprintf("-- %s:\n", mkt))

		i := 0
		msg.WriteString("  DEX: ")
		for assetID, amount := range botDexBals {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), amount))
			if i <= len(botDexBals)-1 {
				msg.WriteString(", ")
			}
			i++
		}

		i = 0
		if botCexBals, found := cexBalances[mkt]; found {
			msg.WriteString("\n  CEX: ")
			for assetID, amount := range botCexBals {
				msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), amount))
				if i <= len(botCexBals)-1 {
					msg.WriteString(", ")
				}
				i++
			}
		}
	}

	m.log.Info(msg.String())
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

func (m *MarketMaker) config() *MarketMakingConfig {
	m.cfgMtx.RLock()
	defer m.cfgMtx.RUnlock()
	return m.cfg.Copy()
}

func (m *MarketMaker) cexConfig(cexName string) *CEXConfig {
	m.cfgMtx.RLock()
	defer m.cfgMtx.RUnlock()
	for _, cfg := range m.cfg.CexConfigs {
		if cfg.Name == cexName {
			return cfg
		}
	}
	return nil
}

func (m *MarketMaker) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	m.ctx = ctx
	cfg := m.config()
	for _, cexCfg := range cfg.CexConfigs {
		if _, err := m.loadCEX(ctx, cexCfg); err != nil {
			m.log.Errorf("Error adding %s: %v", cexCfg.Name, err)
		}
	}
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

// Start the MarketMaker. There can only be one BotConfig per dex market.
func (m *MarketMaker) Start(pw []byte, alternateConfigPath *string) (err error) {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("market making is already running")
	}

	var startedMarketMaking bool
	defer func() {
		if !startedMarketMaking {
			m.running.Store(false)
		}
	}()

	cfg := m.config()
	if alternateConfigPath != nil {
		cfg, err = getMarketMakingConfig(*alternateConfigPath)
		if err != nil {
			return fmt.Errorf("error loading custom market making config: %v", err)
		}
	}

	ctx, die := context.WithCancel(m.ctx)
	m.die.Store(die)

	enabledBots, err := validateAndFilterEnabledConfigs(cfg.BotConfigs)
	if err != nil {
		return err
	}

	if err := m.loginAndUnlockWallets(pw, enabledBots); err != nil {
		return err
	}

	oracle, err := priceOracleFromConfigs(ctx, enabledBots, m.log.SubLogger("PriceOracle"))
	if err != nil {
		return err
	}
	m.syncedOracleMtx.Lock()
	m.syncedOracle = oracle
	m.syncedOracleMtx.Unlock()
	defer func() {
		m.syncedOracleMtx.Lock()
		m.syncedOracle = nil
		m.syncedOracleMtx.Unlock()
	}()

	cexes := m.initCEXConnections(ctx, cfg.CexConfigs)

	dexBaseBalances, cexBaseBalances, err := botInitialBaseBalances(enabledBots, m.core, cexes)
	if err != nil {
		return err
	}
	m.logInitialBotBalances(dexBaseBalances, cexBaseBalances)

	startedMarketMaking = true
	m.core.Broadcast(newMMStartStopNote(true))

	wg := new(sync.WaitGroup)

	var cexCfgMap map[string]*CEXConfig
	if len(cfg.CexConfigs) > 0 {
		cexCfgMap = make(map[string]*CEXConfig, len(cfg.CexConfigs))
		for _, cexCfg := range cfg.CexConfigs {
			cexCfgMap[cexCfg.Name] = cexCfg
		}
	}

	for _, cfg := range enabledBots {
		switch {
		case cfg.BasicMMConfig != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				defer wg.Done()
				defer m.log.Infof("Basic market maker for %s-%d-%d stopped", cfg.Host, cfg.BaseID, cfg.QuoteID)

				mkt := MarketWithHost{cfg.Host, cfg.BaseID, cfg.QuoteID}
				mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
				logger := m.log.SubLogger(fmt.Sprintf("MarketMaker-%s", mktID))
				exchangeAdaptor := unifiedExchangeAdaptorForBot(&exchangeAdaptorCfg{
					botID:              mktID,
					market:             &mkt,
					baseDexBalances:    dexBaseBalances[mktID],
					baseCexBalances:    cexBaseBalances[mktID],
					core:               m.core,
					cex:                nil,
					maxBuyPlacements:   uint32(len(cfg.BasicMMConfig.BuyPlacements)),
					maxSellPlacements:  uint32(len(cfg.BasicMMConfig.SellPlacements)),
					baseWalletOptions:  cfg.BaseWalletOptions,
					quoteWalletOptions: cfg.QuoteWalletOptions,
					log:                logger,
					botCfg:             cfg,
					eventLogDB:         m.eventLogDB,
				})
				if err := exchangeAdaptor.run(ctx); err != nil {
					logger.Errorf("Failed to start market maker: %v", err)
					return
				}

				m.markBotAsRunning(mkt, exchangeAdaptor)
				defer func() {
					m.markBotAsRunning(mkt, nil)
				}()

				RunBasicMarketMaker(ctx, cfg, exchangeAdaptor, oracle, logger)
			}(cfg)
		case cfg.SimpleArbConfig != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				defer wg.Done()
				defer m.log.Infof("Simple arbitrage bot for %s-%d-%d stopped", cfg.Host, cfg.BaseID, cfg.QuoteID)

				mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
				logger := m.log.SubLogger(fmt.Sprintf("SimpleArbitrage-%s", mktID))

				cex, found := cexes[cfg.CEXCfg.Name]
				if !found {
					logger.Errorf("Cannot start %s bot due to CEX not starting", mktID)
					return
				}
				mkt := MarketWithHost{cfg.Host, cfg.BaseID, cfg.QuoteID}

				exchangeAdaptor := unifiedExchangeAdaptorForBot(&exchangeAdaptorCfg{
					botID:              mktID,
					market:             &mkt,
					baseDexBalances:    dexBaseBalances[mktID],
					baseCexBalances:    cexBaseBalances[mktID],
					core:               m.core,
					cex:                cex,
					maxBuyPlacements:   1,
					maxSellPlacements:  1,
					baseWalletOptions:  cfg.BaseWalletOptions,
					quoteWalletOptions: cfg.QuoteWalletOptions,
					rebalanceCfg:       cfg.CEXCfg.AutoRebalance,
					log:                logger,
					botCfg:             cfg,
					eventLogDB:         m.eventLogDB,
				})
				if err := exchangeAdaptor.run(ctx); err != nil {
					logger.Errorf("Failed to start market maker: %v", err)
					return
				}
				m.markBotAsRunning(mkt, exchangeAdaptor)
				defer func() {
					m.markBotAsRunning(mkt, nil)
				}()

				RunSimpleArbBot(ctx, cfg, exchangeAdaptor, exchangeAdaptor, logger)
			}(cfg)
		case cfg.ArbMarketMakerConfig != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				defer wg.Done()
				defer m.log.Infof("ArbMarketMaker for %s-%d-%d stopped", cfg.Host, cfg.BaseID, cfg.QuoteID)

				mktID := dexMarketID(cfg.Host, cfg.BaseID, cfg.QuoteID)
				logger := m.log.SubLogger(fmt.Sprintf("ArbMarketMaker-%s", mktID))

				cex, found := cexes[cfg.CEXCfg.Name]
				if !found {
					logger.Errorf("Cannot stat %s bot due to CEX not starting", mktID)
					return
				}

				mkt := MarketWithHost{cfg.Host, cfg.BaseID, cfg.QuoteID}
				exchangeAdaptor := unifiedExchangeAdaptorForBot(&exchangeAdaptorCfg{
					botID:              mktID,
					market:             &mkt,
					baseDexBalances:    dexBaseBalances[mktID],
					baseCexBalances:    cexBaseBalances[mktID],
					core:               m.core,
					cex:                cex,
					maxBuyPlacements:   uint32(len(cfg.ArbMarketMakerConfig.BuyPlacements)),
					maxSellPlacements:  uint32(len(cfg.ArbMarketMakerConfig.SellPlacements)),
					baseWalletOptions:  cfg.BaseWalletOptions,
					quoteWalletOptions: cfg.QuoteWalletOptions,
					rebalanceCfg:       cfg.CEXCfg.AutoRebalance,
					log:                logger,
					botCfg:             cfg,
					eventLogDB:         m.eventLogDB,
				})
				if err := exchangeAdaptor.run(ctx); err != nil {
					logger.Errorf("Failed to start market maker: %v", err)
					return
				}

				m.markBotAsRunning(mkt, exchangeAdaptor)
				defer func() {
					m.markBotAsRunning(mkt, nil)
				}()

				RunArbMarketMaker(ctx, cfg, exchangeAdaptor, exchangeAdaptor, logger)
			}(cfg)
		default:
			m.log.Errorf("No bot config provided. Skipping %s-%d-%d", cfg.Host, cfg.BaseID, cfg.QuoteID)
		}
	}

	go func() {
		wg.Wait()
		m.running.Store(false)
		m.core.Broadcast(newMMStartStopNote(false))
	}()

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

	err = os.WriteFile(m.cfgPath, data, 0644)
	if err != nil {
		return fmt.Errorf("error writing market making config: %v", err)
	}
	m.cfgMtx.Lock()
	m.cfg = cfg
	m.cfgMtx.Unlock()
	return nil
}

// UpdateBotConfig updates the configuration for one of the bots.
func (m *MarketMaker) UpdateBotConfig(updatedCfg *BotConfig) error {
	cfg := m.config()

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

	return nil
}

func (m *MarketMaker) UpdateCEXConfig(updatedCfg *CEXConfig) error {
	_, err := m.loadAndConnectCEX(m.ctx, updatedCfg)
	if err != nil {
		return fmt.Errorf("error loading %s with updated config: %w", updatedCfg.Name, err)
	}

	var updated bool
	m.cfgMtx.Lock()
	for i, c := range m.cfg.CexConfigs {
		if c.Name == updatedCfg.Name {
			m.cfg.CexConfigs[i] = updatedCfg
			updated = true
			break
		}
	}
	if !updated {
		m.cfg.CexConfigs = append(m.cfg.CexConfigs, updatedCfg)
	}
	m.cfgMtx.Unlock()
	if err := m.writeConfigFile(m.cfg); err != nil {
		m.log.Errorf("Error saving new bot configuration: %w", err)
	}

	return nil
}

// RemoveConfig removes a bot config from the market making config.
func (m *MarketMaker) RemoveBotConfig(host string, baseID, quoteID uint32) error {
	cfg := m.config()

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

// Stop stops the MarketMaker.
func (m *MarketMaker) Stop() {
	m.die.Load().(context.CancelFunc)()
}

// ArchivedRuns returns all archived market making runs.
func (m *MarketMaker) ArchivedRuns() ([]*MarketMakingRun, error) {
	allRuns, err := m.eventLogDB.runs(0, nil, nil)
	if err != nil {
		return nil, err
	}

	if !m.running.Load() {
		return allRuns, nil
	}

	runningBots := m.runningBotsLookup()
	archivedRuns := make([]*MarketMakingRun, 0, len(allRuns))
	for _, run := range allRuns {
		runningBot := runningBots[*run.Market]
		if runningBot == nil || runningBot.startTime.Load() != run.StartTime {
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
