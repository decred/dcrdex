package dex

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
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
	FundConf   uint32 `json:"fundConf"`
	ConfigPath string `json:"configPath"`
}

// DBConf groups the database configuration parameters.
type DBConf struct {
	DBName string
	User   string
	Pass   string
	Host   string
	Port   uint16
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
	LogBackend       *dex.LoggerMaker
	Markets          []*dex.MarketInfo
	Assets           []*AssetConf
	Network          dex.Network
	DBConf           *DBConf
	RegFeeXPub       string
	RegFeeConfirms   int64
	RegFeeAmount     uint64
	BroadcastTimeout time.Duration
	CancelThreshold  float32
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
	config      *configResponse
}

// configResponse is defined here to leave open the possibility for hot
// adjustable parameters while storing a pre-encoded config response message. An
// update method will need to be defined in the future for this purpose.
type configResponse struct {
	configMsg *msgjson.ConfigResult // constant for now
	configEnc json.RawMessage
}

func newConfigResponse(cfg *DexConf, cfgAssets []msgjson.Asset, cfgMarkets []msgjson.Market) (*configResponse, error) {
	configMsg := &msgjson.ConfigResult{
		BroadcastTimeout: uint64(cfg.BroadcastTimeout.Seconds()),
		CancelMax:        cfg.CancelThreshold,
		RegFeeConfirms:   uint16(cfg.RegFeeConfirms),
		Assets:           cfgAssets,
		Markets:          cfgMarkets,
		Fee:              cfg.RegFeeAmount,
	}

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
	ack := &msgjson.Message{
		Type:    msgjson.Response,
		ID:      msg.ID,
		Payload: dm.config.configEnc,
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
	cfgAssets := make([]msgjson.Asset, 0, len(cfg.Assets))
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
				FundConf: assetConf.FundConf,
			},
			Backend: be,
		}

		backedAssets[ID] = ba
		lockableAssets[ID] = &swap.LockableAsset{
			BackedAsset: ba,
			CoinLocker:  dexCoinLocker.AssetLocker(ID).Swap(),
		}

		cfgAssets = append(cfgAssets, msgjson.Asset{
			Symbol:   assetConf.Symbol,
			ID:       ID,
			LotSize:  assetConf.LotSize,
			RateStep: assetConf.RateStep,
			FeeRate:  assetConf.FeeRate,
			SwapSize: uint64(be.InitTxSize()),
			SwapConf: uint16(assetConf.SwapConf),
			FundConf: uint16(assetConf.FundConf),
		})
	}

	// Create DEXArchivist with the pg DB driver.
	pgCfg := &pg.Config{
		Host:         cfg.DBConf.Host,
		Port:         strconv.Itoa(int(cfg.DBConf.Port)),
		User:         cfg.DBConf.User,
		Pass:         cfg.DBConf.Pass,
		DBName:       cfg.DBConf.DBName,
		HidePGConfig: false,
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
		FeeChecker:      dcrBackend.UnspentCoinDetails,
	}

	authMgr := auth.NewAuthManager(&authCfg)
	go authMgr.Run(ctx)

	// Create the swapper.
	swapperCfg := &swap.Config{
		Assets:           lockableAssets,
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: cfg.BroadcastTimeout,
	}

	swapper := swap.NewSwapper(swapperCfg)
	startSubSys("Swapper", swapper)

	// Markets
	markets := make(map[string]*market.Market, len(cfg.Markets))
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

	// Set start epoch index for each market. Also create BookSources for the
	// BookRouter, and MarketTunnels for the OrderRouter
	now := encode.UnixMilli(time.Now())
	bookSources := make(map[string]market.BookSource, len(cfg.Markets))
	marketTunnels := make(map[string]market.MarketTunnel, len(cfg.Markets))
	cfgMarkets := make([]msgjson.Market, 0, len(cfg.Markets))
	for name, mkt := range markets {
		startEpochIdx := 1 + now/int64(mkt.EpochDuration())
		mkt.SetStartEpochIdx(startEpochIdx)
		startSubSys(fmt.Sprintf("Market[%s]", name), mkt)
		bookSources[name] = mkt
		marketTunnels[name] = mkt
		cfgMarkets = append(cfgMarkets, msgjson.Market{
			Name:            name,
			Base:            mkt.Base(),
			Quote:           mkt.Quote(),
			EpochLen:        mkt.EpochDuration(),
			StartEpoch:      uint64(startEpochIdx),
			MarketBuyBuffer: mkt.MarketBuyBuffer(),
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
		config:      cfgResp,
	}

	comms.Route(msgjson.ConfigRoute, dexMgr.handleDEXConfig)

	return dexMgr, nil
}
