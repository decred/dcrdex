package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	dcrasset "decred.org/dcrdex/server/asset/dcr"
	"decred.org/dcrdex/server/auth"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg"
	"decred.org/dcrdex/server/market"
	"decred.org/dcrdex/server/swap"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/slog"
)

// AssetConf is like dex.Asset except it lacks the BIP44 integer ID, has Network
// and ConfigPath strings, and has JSON tags.
type AssetConf struct {
	Symbol     string `json:"bip44symbol"`
	Network    string `json:"network"`
	LotSize    uint64 `json:"lotSize"`
	RateStep   uint64 `json:"rateStep"`
	FeeRate    uint64 `json:"feeRate"`
	SwapConf   uint32 `json:"swapConf"`
	ConfigPath string `json:"configPath"`
}

// DBConf groups the database configuration parameters.
type DBConf struct {
	DBName       string
	User         string
	Pass         string
	Host         string
	Port         uint16
	ShowPGConfig bool
}

// RPCConfig is an alias for the comms Server's RPC config struct.
type RPCConfig = comms.RPCConfig

// LoggerMaker allows creation of new log subsystems with predefined levels.
type LoggerMaker struct {
	*slog.Backend
	DefaultLevel slog.Level
	Levels       map[string]slog.Level
}

// SubLogger creates a Logger with a subsystem name "parent[name]", using any
// known log level for the parent subsystem, defaulting to the DefaultLevel if
// the parent does not have an explicitly set level.
func (lm *LoggerMaker) SubLogger(parent, name string) dex.Logger {
	// Use the parent logger's log level, if set.
	level, ok := lm.Levels[parent]
	if !ok {
		level = lm.DefaultLevel
	}
	logger := lm.Backend.Logger(fmt.Sprintf("%s[%s]", parent, name))
	logger.SetLevel(level)
	return logger
}

// NewLogger creates a new Logger for the subsystem with the given name. If a
// log level is specified, it is used for the Logger. Otherwise the DefaultLevel
// is used.
func (lm *LoggerMaker) NewLogger(name string, level ...slog.Level) dex.Logger {
	lvl := lm.DefaultLevel
	if len(level) > 0 {
		lvl = level[0]
	}
	logger := lm.Backend.Logger(name)
	logger.SetLevel(lvl)
	return logger
}

// DexConf is the configuration data required to create a new DEX.
type DexConf struct {
	SwapState        *swap.State
	DataDir          string
	LogBackend       *dex.LoggerMaker
	Markets          []*dex.MarketInfo
	Assets           []*AssetConf
	Network          dex.Network
	DBConf           *DBConf
	RegFeeXPub       string
	RegFeeConfirms   int64
	RegFeeAmount     uint64
	BroadcastTimeout time.Duration
	CancelThreshold  float64
	Anarchy          bool
	DEXPrivKey       *secp256k1.PrivateKey
	CommsCfg         *RPCConfig
}

type subsystem struct {
	*dex.StartStopWaiter
	name string
}

// DEX is the DEX manager, which creates and controls the lifetime of all
// components of the DEX.
type DEX struct {
	network     dex.Network
	markets     map[string]*market.Market
	assets      map[uint32]*swap.LockableAsset
	storage     db.DEXArchivist
	swapper     *swap.Swapper
	orderRouter *market.OrderRouter
	bookRouter  *market.BookRouter
	stopWaiters []subsystem
	server      *comms.Server

	configRespMtx sync.RWMutex
	configResp    *configResponse
}

// configResponse is defined here to leave open the possibility for hot
// adjustable parameters while storing a pre-encoded config response message. An
// update method will need to be defined in the future for this purpose.
type configResponse struct {
	configMsg *msgjson.ConfigResult // constant for now
	configEnc json.RawMessage
}

func newConfigResponse(cfg *DexConf, cfgAssets []*msgjson.Asset, cfgMarkets []*msgjson.Market) (*configResponse, error) {
	configMsg := &msgjson.ConfigResult{
		BroadcastTimeout: uint64(cfg.BroadcastTimeout.Milliseconds()),
		CancelMax:        cfg.CancelThreshold,
		RegFeeConfirms:   uint16(cfg.RegFeeConfirms),
		Assets:           cfgAssets,
		Markets:          cfgMarkets,
		Fee:              cfg.RegFeeAmount,
	}

	// NOTE/TODO: To include active epoch in the market status objects, we need
	// a channel from Market to push status changes back to DEX manager.
	// Presently just include start epoch that we set when launching the
	// Markets, and suspend info that DEX obtained when calling the Market's
	// Suspend method.

	encResult, err := json.Marshal(configMsg)
	if err != nil {
		return nil, err
	}
	payload := &msgjson.ResponsePayload{
		Result: encResult,
	}
	encPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &configResponse{
		configMsg: configMsg,
		configEnc: encPayload,
	}, nil
}

func (cr *configResponse) setMktSuspend(name string, finalEpoch uint64, persist bool) {
	for _, mkt := range cr.configMsg.Markets {
		if mkt.Name == name {
			mkt.MarketStatus.FinalEpoch = finalEpoch
			mkt.MarketStatus.Persist = &persist
			cr.remarshall()
			return
		}
	}
	log.Errorf("Failed to set MarketStatus for market %q", name)
}

func (cr *configResponse) remarshall() {
	encResult, err := json.Marshal(cr.configMsg)
	if err != nil {
		log.Errorf("failed to marshal config message: %v", err)
		return
	}
	payload := &msgjson.ResponsePayload{
		Result: encResult,
	}
	encPayload, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("failed to marshal config message payload: %v", err)
		return
	}
	cr.configEnc = encPayload
}

// Stop shuts down the DEX. Stop returns only after all components have
// completed their shutdown.
func (dm *DEX) Stop() {
	log.Infof("Stopping subsystems...")
	for _, ssw := range dm.stopWaiters {
		ssw.Stop()
		ssw.WaitForShutdown()
		log.Infof("%s shutdown.", ssw.name)
	}
	if err := dm.storage.Close(); err != nil {
		log.Errorf("DEXArchivist.Close: %v", err)
	}
}

func (dm *DEX) handleDEXConfig(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	dm.configRespMtx.RLock()
	defer dm.configRespMtx.RUnlock()

	ack := &msgjson.Message{
		Type:    msgjson.Response,
		ID:      msg.ID,
		Payload: dm.configResp.configEnc,
	}

	if err := conn.Send(ack); err != nil {
		log.Debugf("error sending config response: %v", err)
	}

	return nil
}

// NewDEX creates the dex manager and starts all subsystems. Use Stop to
// shutdown cleanly.
//  1. Validate each specified asset.
//  2. Create CoinLockers for each asset.
//  3. Create and start asset backends.
//  4. Create the archivist and connect to the storage backend.
//  5. Create the authentication manager.
//  6. Create and start the Swapper.
//  7. Create and start the markets.
//  8. Create and start the book router, and create the order router.
//  9. Create and start the comms server.
func NewDEX(cfg *DexConf) (*DEX, error) {
	// Disallow running without user penalization in a mainnet config.
	if cfg.Anarchy && cfg.Network == dex.Mainnet {
		return nil, fmt.Errorf("User penalties may not be disabled on mainnet.")
	}

	var stopWaiters []subsystem
	ctx, cancel := context.WithCancel(context.Background())

	startSubSys := func(name string, r dex.Runner) {
		ssw := dex.NewStartStopWaiter(r)
		ssw.Start(ctx)
		stopWaiters = append([]subsystem{{ssw, name}}, stopWaiters...)
	}

	abort := func() {
		for _, ssw := range stopWaiters {
			ssw.Stop()
			ssw.WaitForShutdown()
		}
		// If the DB is running, kill it too.
		cancel()
	}

	// Check each configured asset.
	assetIDs := make([]uint32, len(cfg.Assets))
	for i, assetConf := range cfg.Assets {
		symbol := strings.ToLower(assetConf.Symbol)

		// Ensure the symbol is a recognized BIP44 symbol, and retrieve its ID.
		ID, found := dex.BipSymbolID(symbol)
		if !found {
			return nil, fmt.Errorf("asset symbol %q unrecognized", assetConf.Symbol)
		}

		// Double check the asset's network.
		net, err := dex.NetFromString(assetConf.Network)
		if err != nil {
			return nil, fmt.Errorf("unrecognized network %s for asset %s",
				assetConf.Network, symbol)
		}
		if cfg.Network != net {
			return nil, fmt.Errorf("asset %q is configured for network %q, expected %q",
				symbol, assetConf.Network, cfg.Network.String())
		}

		assetIDs[i] = ID
	}

	// Create a MasterCoinLocker for each asset.
	dexCoinLocker := coinlock.NewDEXCoinLocker(assetIDs)

	// Start asset backends.
	var dcrBackend *dcrasset.Backend
	lockableAssets := make(map[uint32]*swap.LockableAsset, len(cfg.Assets))
	backedAssets := make(map[uint32]*asset.BackedAsset, len(cfg.Assets))
	cfgAssets := make([]*msgjson.Asset, 0, len(cfg.Assets))
	for i, assetConf := range cfg.Assets {
		symbol := strings.ToLower(assetConf.Symbol)
		ID := assetIDs[i]

		// Create a new asset backend. An asset driver with a name matching the
		// asset symbol must be available.
		log.Infof("Starting asset backend %q...", symbol)
		logger := cfg.LogBackend.SubLogger("ASSET", symbol)
		be, err := asset.Setup(symbol, assetConf.ConfigPath, logger, cfg.Network)
		if err != nil {
			abort()
			return nil, fmt.Errorf("failed to setup asset %q: %v", symbol, err)
		}

		if symbol == "dcr" {
			var ok bool
			dcrBackend, ok = be.(*dcrasset.Backend)
			if !ok {
				abort()
				return nil, fmt.Errorf("dcr backend is invalid")
			}
		}

		startSubSys(fmt.Sprintf("Asset[%s]", symbol), be)

		ba := &asset.BackedAsset{
			Asset: dex.Asset{
				ID:       ID,
				Symbol:   symbol,
				LotSize:  assetConf.LotSize,
				RateStep: assetConf.RateStep,
				FeeRate:  assetConf.FeeRate,
				SwapConf: assetConf.SwapConf,
			},
			Backend: be,
		}

		backedAssets[ID] = ba
		lockableAssets[ID] = &swap.LockableAsset{
			BackedAsset: ba,
			CoinLocker:  dexCoinLocker.AssetLocker(ID).Swap(),
		}

		cfgAssets = append(cfgAssets, &msgjson.Asset{
			Symbol:   assetConf.Symbol,
			ID:       ID,
			LotSize:  assetConf.LotSize,
			RateStep: assetConf.RateStep,
			FeeRate:  assetConf.FeeRate,
			SwapSize: uint64(be.InitTxSize()),
			SwapConf: uint16(assetConf.SwapConf),
		})
	}

	for _, mkt := range cfg.Markets {
		mkt.Name = strings.ToLower(mkt.Name)
	}

	// Create DEXArchivist with the pg DB driver.
	pgCfg := &pg.Config{
		Host:         cfg.DBConf.Host,
		Port:         strconv.Itoa(int(cfg.DBConf.Port)),
		User:         cfg.DBConf.User,
		Pass:         cfg.DBConf.Pass,
		DBName:       cfg.DBConf.DBName,
		ShowPGConfig: cfg.DBConf.ShowPGConfig,
		QueryTimeout: 20 * time.Minute,
		MarketCfg:    cfg.Markets,
		//CheckedStores: true,
		Net:    cfg.Network,
		FeeKey: cfg.RegFeeXPub,
	}
	storage, err := db.Open(ctx, "pg", pgCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("db.Open: %v", err)
	}

	authCfg := auth.Config{
		Storage:         storage,
		Signer:          cfg.DEXPrivKey,
		RegistrationFee: cfg.RegFeeAmount,
		FeeConfs:        cfg.RegFeeConfirms,
		FeeChecker:      dcrBackend.FeeCoin,
		CancelThreshold: cfg.CancelThreshold,
		Anarchy:         cfg.Anarchy,
	}

	authMgr := auth.NewAuthManager(&authCfg)
	startSubSys("Auth manager", authMgr)

	markets := make(map[string]*market.Market, len(cfg.Markets))
	marketUnbookHook := func(lo *order.LimitOrder) bool {
		name, err := dex.MarketName(lo.BaseAsset, lo.QuoteAsset)
		if err != nil {
			log.Error(err)
			return false
		}

		return markets[name].Unbook(lo)
	}

	// Create the swapper.
	swapperCfg := &swap.Config{
		State:            cfg.SwapState,
		DataDir:          cfg.DataDir,
		Assets:           lockableAssets,
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: cfg.BroadcastTimeout,
		LockTimeTaker:    dex.LockTimeTaker(cfg.Network),
		LockTimeMaker:    dex.LockTimeMaker(cfg.Network),
		UnbookHook:       marketUnbookHook,
	}

	swapper, err := swap.NewSwapper(swapperCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("NewSwapper: %v", err)
	}

	// Markets
	for _, mktInf := range cfg.Markets {
		baseCoinLocker := dexCoinLocker.AssetLocker(mktInf.Base).Book()
		quoteCoinLocker := dexCoinLocker.AssetLocker(mktInf.Quote).Book()
		mkt, err := market.NewMarket(mktInf, storage, swapper, authMgr, baseCoinLocker, quoteCoinLocker)
		if err != nil {
			abort()
			return nil, fmt.Errorf("NewMarket failed: %v", err)
		}
		markets[mktInf.Name] = mkt
	}

	startSubSys("Swapper", swapper) // after markets map set

	// Set start epoch index for each market. Also create BookSources for the
	// BookRouter, and MarketTunnels for the OrderRouter
	now := encode.UnixMilli(time.Now())
	bookSources := make(map[string]market.BookSource, len(cfg.Markets))
	marketTunnels := make(map[string]market.MarketTunnel, len(cfg.Markets))
	cfgMarkets := make([]*msgjson.Market, 0, len(cfg.Markets))
	for name, mkt := range markets {
		startEpochIdx := 1 + now/int64(mkt.EpochDuration())
		mkt.SetStartEpochIdx(startEpochIdx)
		startSubSys(fmt.Sprintf("Market[%s]", name), mkt)
		bookSources[name] = mkt
		marketTunnels[name] = mkt
		cfgMarkets = append(cfgMarkets, &msgjson.Market{
			Name:            name,
			Base:            mkt.Base(),
			Quote:           mkt.Quote(),
			EpochLen:        mkt.EpochDuration(),
			MarketBuyBuffer: mkt.MarketBuyBuffer(),
			MarketStatus: msgjson.MarketStatus{
				StartEpoch: uint64(startEpochIdx),
			},
		})
	}

	// Book router
	bookRouter := market.NewBookRouter(bookSources)
	startSubSys("BookRouter", bookRouter)

	// Order router
	orderRouter := market.NewOrderRouter(&market.OrderRouterConfig{
		Assets:      backedAssets,
		AuthManager: authMgr,
		Markets:     marketTunnels,
	})

	// Client comms RPC server.
	server, err := comms.NewServer(cfg.CommsCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("NewServer failed: %v", err)
	}
	startSubSys("Comms Server", server)

	cfgResp, err := newConfigResponse(cfg, cfgAssets, cfgMarkets)
	if err != nil {
		return nil, err
	}

	dexMgr := &DEX{
		network:     cfg.Network,
		markets:     markets,
		assets:      lockableAssets,
		swapper:     swapper,
		storage:     storage,
		orderRouter: orderRouter,
		bookRouter:  bookRouter,
		stopWaiters: stopWaiters,
		server:      server,
		configResp:  cfgResp,
	}

	comms.Route(msgjson.ConfigRoute, dexMgr.handleDEXConfig)

	return dexMgr, nil
}

// Config returns the current dex configuration.
func (dm *DEX) ConfigMsg() json.RawMessage {
	dm.configRespMtx.RLock()
	defer dm.configRespMtx.RUnlock()
	return dm.configResp.configEnc
}

// TODO: for just market running status, the DEX manager should use it's
// knowledge of Market subsystem state.
func (dm *DEX) MarketRunning(mktName string) (found, running bool) {
	mkt := dm.markets[mktName]
	if mkt == nil {
		return
	}
	return true, mkt.Running()
}

// MarketStatus returns the market.Status for the named market. If the market is
// unknown to the DEX, nil is returned.
func (dm *DEX) MarketStatus(mktName string) *market.Status {
	mkt := dm.markets[mktName]
	if mkt == nil {
		return nil
	}
	return mkt.Status()
}

// MarketStatuses returns a map of market names to market.Status for all known
// markets.
func (dm *DEX) MarketStatuses() map[string]*market.Status {
	statuses := make(map[string]*market.Status, len(dm.markets))
	for name, mkt := range dm.markets {
		statuses[name] = mkt.Status()
	}
	return statuses
}

// SuspendMarket schedules a suspension of a given market, with the option to
// persist the orders on the book (or purge the book automatically on market
// shutdown). The scheduled final epoch and suspend time are returned. This is a
// passthrough to the OrderRouter. A TradeSuspension notification is broadcasted
// to all connected clients.
func (dm *DEX) SuspendMarket(name string, tSusp time.Time, persistBooks bool) *market.SuspendEpoch {
	name = strings.ToLower(name)
	// Go through the order router since OrderRouter is likely to have market
	// status tracking built into it to facilitate resume.
	suspEpoch := dm.orderRouter.SuspendMarket(name, tSusp, persistBooks)

	// Update config message with suspend schedule.
	dm.configRespMtx.Lock()
	dm.configResp.setMktSuspend(name, uint64(suspEpoch.Idx), persistBooks)
	dm.configRespMtx.Unlock()

	// Broadcast a TradeSuspension notification to all connected clients.
	note, err := msgjson.NewNotification(msgjson.SuspensionRoute, msgjson.TradeSuspension{
		MarketID:    name,
		FinalEpoch:  uint64(suspEpoch.Idx),
		SuspendTime: encode.UnixMilliU(suspEpoch.End),
		Persist:     persistBooks,
	})
	if err != nil {
		log.Errorf("Failed to create suspend notification: %v", err)
	} else {
		dm.server.Broadcast(note)
	}
	return suspEpoch
}

// TODO: resume by relaunching the market subsystems (Run)
// Resume / ResumeMarket

// Accounts returns data for all accounts.
func (dm *DEX) Accounts() ([]*db.Account, error) {
	return dm.storage.Accounts()
}

// AccountInfo returns data for an account.
func (dm *DEX) AccountInfo(aid account.AccountID) (*db.Account, error) {
	return dm.storage.AccountInfo(aid)
}
