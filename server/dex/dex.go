// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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
	"decred.org/dcrdex/server/apidata"
	"decred.org/dcrdex/server/asset"
	dcrasset "decred.org/dcrdex/server/asset/dcr"
	"decred.org/dcrdex/server/auth"
	"decred.org/dcrdex/server/coinlock"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg"
	"decred.org/dcrdex/server/market"
	"decred.org/dcrdex/server/swap"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
)

const (
	// PreAPIVersion covers all API iterations before versioning started.
	PreAPIVersion = iota

	// APIVersion is the current API version.
	APIVersion = PreAPIVersion
)

// AssetConf is like dex.Asset except it lacks the BIP44 integer ID and
// implementation version, has Network and ConfigPath strings, and has JSON
// tags.
type AssetConf struct {
	Symbol     string `json:"bip44symbol"`
	Network    string `json:"network"`
	LotSize    uint64 `json:"lotSize"`
	RateStep   uint64 `json:"rateStep"`
	MaxFeeRate uint64 `json:"maxFeeRate"`
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

// DexConf is the configuration data required to create a new DEX.
type DexConf struct {
	DataDir           string
	LogBackend        *dex.LoggerMaker
	Markets           []*dex.MarketInfo
	Assets            []*AssetConf
	Network           dex.Network
	DBConf            *DBConf
	RegFeeXPub        string
	RegFeeConfirms    int64
	RegFeeAmount      uint64
	BroadcastTimeout  time.Duration
	CancelThreshold   float64
	Anarchy           bool
	FreeCancels       bool
	BanScore          uint32
	InitTakerLotLimit uint32
	AbsTakerLotLimit  uint32
	DEXPrivKey        *secp256k1.PrivateKey
	CommsCfg          *RPCConfig
	NoResumeSwaps     bool
}

type signer struct {
	*secp256k1.PrivateKey
}

func (s signer) Sign(hash []byte) *ecdsa.Signature {
	return ecdsa.Sign(s.PrivateKey, hash)
}

type subsystem struct {
	name string
	// either a ssw or cm
	ssw *dex.StartStopWaiter
	cm  *dex.ConnectionMaster
}

func (ss *subsystem) stop() {
	if ss.ssw != nil {
		ss.ssw.Stop()
		ss.ssw.WaitForShutdown()
	} else {
		ss.cm.Disconnect()
		ss.cm.Wait()
	}
}

// DEX is the DEX manager, which creates and controls the lifetime of all
// components of the DEX.
type DEX struct {
	network     dex.Network
	markets     map[string]*market.Market
	assets      map[uint32]*swap.LockableAsset
	storage     db.DEXArchivist
	authMgr     *auth.AuthManager
	swapper     *swap.Swapper
	orderRouter *market.OrderRouter
	bookRouter  *market.BookRouter
	subsystems  []subsystem
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
		APIVersion:       uint16(APIVersion),
		BinSizes:         apidata.BinSizes,
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

	return &configResponse{
		configMsg: configMsg,
		configEnc: encResult,
	}, nil
}

func (cr *configResponse) setMktSuspend(name string, finalEpoch uint64, persist bool) {
	for _, mkt := range cr.configMsg.Markets {
		if mkt.Name == name {
			mkt.MarketStatus.FinalEpoch = finalEpoch
			mkt.MarketStatus.Persist = &persist
			cr.remarshal()
			return
		}
	}
	log.Errorf("Failed to update MarketStatus for market %q", name)
}

func (cr *configResponse) setMktResume(name string, startEpoch uint64) (epochLen uint64) {
	for _, mkt := range cr.configMsg.Markets {
		if mkt.Name == name {
			mkt.MarketStatus.StartEpoch = startEpoch
			mkt.MarketStatus.FinalEpoch = 0
			cr.remarshal()
			return mkt.EpochLen
		}
	}
	log.Errorf("Failed to update MarketStatus for market %q", name)
	return 0
}

func (cr *configResponse) remarshal() {
	encResult, err := json.Marshal(cr.configMsg)
	if err != nil {
		log.Errorf("failed to marshal config message: %v", err)
		return
	}
	cr.configEnc = encResult
}

// Stop shuts down the DEX. Stop returns only after all components have
// completed their shutdown.
func (dm *DEX) Stop() {
	log.Infof("Stopping all DEX subsystems.")
	for _, ss := range dm.subsystems {
		log.Infof("Stopping %s...", ss.name)
		ss.stop()
		log.Infof("%s is now shut down.", ss.name)
	}
	log.Infof("Stopping storage...")
	if err := dm.storage.Close(); err != nil {
		log.Errorf("DEXArchivist.Close: %v", err)
	}
}

func marketSubSysName(name string) string {
	return fmt.Sprintf("Market[%s]", name)
}

func (dm *DEX) handleDEXConfig(interface{}) (interface{}, error) {
	dm.configRespMtx.RLock()
	defer dm.configRespMtx.RUnlock()
	return dm.configResp.configEnc, nil
}

// NewDEX creates the dex manager and starts all subsystems. Use Stop to
// shutdown cleanly. The Context is used to abort setup.
//  1. Validate each specified asset.
//  2. Create CoinLockers for each asset.
//  3. Create and start asset backends.
//  4. Create the archivist and connect to the storage backend.
//  5. Create the authentication manager.
//  6. Create and start the Swapper.
//  7. Create and start the markets.
//  8. Create and start the book router, and create the order router.
//  9. Create and start the comms server.
func NewDEX(ctx context.Context, cfg *DexConf) (*DEX, error) {
	// Disallow running without user penalization in a mainnet config.
	if cfg.Anarchy && cfg.Network == dex.Mainnet {
		return nil, fmt.Errorf("user penalties may not be disabled on mainnet")
	}

	var subsystems []subsystem
	startSubSys := func(name string, rc interface{}) (err error) {
		subsys := subsystem{name: name}
		switch st := rc.(type) {
		case dex.Runner:
			subsys.ssw = dex.NewStartStopWaiter(st)
			subsys.ssw.Start(context.Background()) // stopped with Stop
		case dex.Connector:
			subsys.cm = dex.NewConnectionMaster(st)
			err = subsys.cm.Connect(context.Background()) // stopped with Disconnect
			if err != nil {
				return
			}
		default:
			panic(fmt.Sprintf("Invalid subsystem type %T", rc))
		}

		subsystems = append([]subsystem{subsys}, subsystems...) // top of stack
		return
	}

	// Do not wrap the caller's context for the DB since we must coordinate it's
	// shutdown in sequence with the other subsystems.
	ctxDB, cancelDB := context.WithCancel(context.Background())
	abort := func() {
		for _, ss := range subsystems {
			ss.stop()
		}
		// If the DB is running, kill it too.
		cancelDB()
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

		if assetConf.MaxFeeRate == 0 {
			return nil, fmt.Errorf("max fee rate of 0 is invalid for asset %q", symbol)
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
	assetLogger := cfg.LogBackend.Logger("ASSET")
	txDataSources := make(map[uint32]auth.TxDataSource)
	feeMgr := NewFeeManager()
	for i, assetConf := range cfg.Assets {
		symbol := strings.ToLower(assetConf.Symbol)
		assetID := assetIDs[i]

		assetVer, err := asset.Version(symbol)
		if err != nil {
			abort()
			return nil, fmt.Errorf("failed to retrieve asset %q version: %w", symbol, err)
		}

		// Create a new asset backend. An asset driver with a name matching the
		// asset symbol must be available.
		log.Infof("Starting asset backend %q...", symbol)
		logger := assetLogger.SubLogger(symbol)
		be, err := asset.Setup(symbol, assetConf.ConfigPath, logger, cfg.Network)
		if err != nil {
			abort()
			return nil, fmt.Errorf("failed to setup asset %q: %w", symbol, err)
		}

		if symbol == "dcr" {
			var ok bool
			dcrBackend, ok = be.(*dcrasset.Backend)
			if !ok {
				abort()
				return nil, fmt.Errorf("dcr backend is invalid")
			}
		}

		err = startSubSys(fmt.Sprintf("Asset[%s]", symbol), be)
		if err != nil {
			abort()
			return nil, fmt.Errorf("failed to start asset %q: %w", symbol, err)
		}

		initTxSize := uint64(be.InitTxSize())
		initTxSizeBase := uint64(be.InitTxSizeBase())
		ba := &asset.BackedAsset{
			Asset: dex.Asset{
				ID:           assetID,
				Symbol:       symbol,
				Version:      assetVer,
				LotSize:      assetConf.LotSize,
				RateStep:     assetConf.RateStep,
				MaxFeeRate:   assetConf.MaxFeeRate,
				SwapSize:     initTxSize,
				SwapSizeBase: initTxSizeBase,
				SwapConf:     assetConf.SwapConf,
			},
			Backend: be,
		}

		backedAssets[assetID] = ba
		lockableAssets[assetID] = &swap.LockableAsset{
			BackedAsset: ba,
			CoinLocker:  dexCoinLocker.AssetLocker(assetID).Swap(),
		}
		feeMgr.AddFetcher(ba)

		// Prepare assets portion of config response.
		cfgAssets = append(cfgAssets, &msgjson.Asset{
			Symbol:       assetConf.Symbol,
			ID:           assetID,
			Version:      assetVer,
			LotSize:      assetConf.LotSize,
			RateStep:     assetConf.RateStep,
			MaxFeeRate:   assetConf.MaxFeeRate,
			SwapSize:     initTxSize,
			SwapSizeBase: initTxSizeBase,
			SwapConf:     uint16(assetConf.SwapConf),
		})

		txDataSources[assetID] = be.TxData
	}

	// Ensure their is a DCR asset backend.
	if dcrBackend == nil {
		abort()
		return nil, fmt.Errorf("no DCR backend configured")
	}
	// Validate the registration fee extended public key.
	if err := dcrBackend.ValidateXPub(cfg.RegFeeXPub); err != nil {
		return nil, fmt.Errorf("invalid regfeexpub: %w", err)
	}

	for _, mkt := range cfg.Markets {
		mkt.Name = strings.ToLower(mkt.Name)
	}

	if err := ctx.Err(); err != nil {
		abort()
		return nil, err
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
	// After DEX construction, the storage subsystem should be stopped
	// gracefully with its Close method, and in coordination with other
	// subsystems via Stop. To abort its setup, rig a temporary link to the
	// caller's Context.
	running := make(chan struct{})
	defer close(running) // break the link
	go func() {
		select {
		case <-ctx.Done(): // cancelled construction
			cancelDB()
		case <-running: // DB shutdown now only via dex.Stop=>db.Close
		}
	}()
	storage, err := db.Open(ctxDB, "pg", pgCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("db.Open: %w", err)
	}

	dataAPI := apidata.NewDataAPI(storage)

	// Create the user order unbook dispatcher for the AuthManager.
	markets := make(map[string]*market.Market, len(cfg.Markets))
	userUnbookFun := func(user account.AccountID) {
		for _, mkt := range markets {
			mkt.UnbookUserOrders(user)
		}
	}

	authCfg := auth.Config{
		Storage:           storage,
		Signer:            signer{cfg.DEXPrivKey},
		RegistrationFee:   cfg.RegFeeAmount,
		FeeConfs:          cfg.RegFeeConfirms,
		FeeChecker:        dcrBackend.FeeCoin,
		UserUnbooker:      userUnbookFun,
		MiaUserTimeout:    cfg.BroadcastTimeout,
		CancelThreshold:   cfg.CancelThreshold,
		Anarchy:           cfg.Anarchy,
		FreeCancels:       cfg.FreeCancels,
		BanScore:          cfg.BanScore,
		InitTakerLotLimit: cfg.InitTakerLotLimit,
		AbsTakerLotLimit:  cfg.AbsTakerLotLimit,
		TxDataSources:     txDataSources,
	}

	authMgr := auth.NewAuthManager(&authCfg)
	log.Infof("Cancellation rate threshold %f, new user grace period %d cancels",
		cfg.CancelThreshold, authMgr.GraceLimit())
	log.Infof("MIA user order unbook timeout %v", cfg.BroadcastTimeout)
	if authCfg.FreeCancels {
		log.Infof("Cancellations are NOT COUNTED (the cancellation rate threshold is ignored).")
	}
	log.Infof("Ban score threshold is %v", cfg.BanScore)

	// Create a swapDone dispatcher for the Swapper.
	swapDone := func(ord order.Order, match *order.Match, fail bool) {
		name, err := dex.MarketName(ord.Base(), ord.Quote())
		if err != nil {
			log.Errorf("bad market for order %v: %v", ord.ID(), err)
			return
		}
		markets[name].SwapDone(ord, match, fail)
	}

	// Create the swapper.
	swapperCfg := &swap.Config{
		Assets:           lockableAssets,
		Storage:          storage,
		AuthManager:      authMgr,
		BroadcastTimeout: cfg.BroadcastTimeout,
		LockTimeTaker:    dex.LockTimeTaker(cfg.Network),
		LockTimeMaker:    dex.LockTimeMaker(cfg.Network),
		SwapDone:         swapDone,
		NoResume:         cfg.NoResumeSwaps,
		// TODO: set the AllowPartialRestore bool to allow startup with a
		// missing asset backend if necessary in an emergency.
	}

	swapper, err := swap.NewSwapper(swapperCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("NewSwapper: %w", err)
	}

	if err := ctx.Err(); err != nil {
		abort()
		return nil, err
	}

	// Markets
	usersWithOrders := make(map[account.AccountID]struct{})
	for _, mktInf := range cfg.Markets {
		baseCoinLocker := dexCoinLocker.AssetLocker(mktInf.Base).Book()
		quoteCoinLocker := dexCoinLocker.AssetLocker(mktInf.Quote).Book()
		mkt, err := market.NewMarket(&market.Config{
			MarketInfo:      mktInf,
			Storage:         storage,
			Swapper:         swapper,
			AuthManager:     authMgr,
			FeeFetcherBase:  feeMgr.FeeFetcher(mktInf.Base),
			CoinLockerBase:  baseCoinLocker,
			FeeFetcherQuote: feeMgr.FeeFetcher(mktInf.Quote),
			CoinLockerQuote: quoteCoinLocker,
			DataCollector:   dataAPI,
		})
		if err != nil {
			abort()
			return nil, fmt.Errorf("NewMarket failed: %w", err)
		}
		markets[mktInf.Name] = mkt
		log.Infof("Preparing historical market data API for market %v...", mktInf.Name)
		err = dataAPI.AddMarketSource(mkt)
		if err != nil {
			abort()
			return nil, fmt.Errorf("DataSource.AddMarket: %w", err)
		}

		// Having loaded the book, get the accounts owning the orders.
		_, buys, sells := mkt.Book()
		for _, lo := range buys {
			usersWithOrders[lo.AccountID] = struct{}{}
		}
		for _, lo := range sells {
			usersWithOrders[lo.AccountID] = struct{}{}
		}
	}

	// Having enumerated all users with booked orders, configure the AuthManager
	// to expect them to connect in a certain time period.
	authMgr.ExpectUsers(usersWithOrders, cfg.BroadcastTimeout)

	// Start the AuthManager and Swapper subsystems after populating the markets
	// map used by the unbook callbacks, and setting the AuthManager's unbook
	// timers for the users with currently booked orders.
	startSubSys("Auth manager", authMgr)
	startSubSys("Swapper", swapper)

	// Set start epoch index for each market. Also create BookSources for the
	// BookRouter, and MarketTunnels for the OrderRouter.
	now := encode.UnixMilli(time.Now())
	bookSources := make(map[string]market.BookSource, len(cfg.Markets))
	marketTunnels := make(map[string]market.MarketTunnel, len(cfg.Markets))
	cfgMarkets := make([]*msgjson.Market, 0, len(cfg.Markets))
	for name, mkt := range markets {
		startEpochIdx := 1 + now/int64(mkt.EpochDuration())
		mkt.SetStartEpochIdx(startEpochIdx)
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
	bookRouter := market.NewBookRouter(bookSources, feeMgr)
	startSubSys("BookRouter", bookRouter)

	// The data API gets the order book from the book router.
	dataAPI.SetBookSource(bookRouter)

	// Market, now that book router is running.
	for name, mkt := range markets {
		startSubSys(marketSubSysName(name), mkt)
	}

	// Order router
	orderRouter := market.NewOrderRouter(&market.OrderRouterConfig{
		Assets:      backedAssets,
		AuthManager: authMgr,
		Markets:     marketTunnels,
		FeeSource:   feeMgr,
	})
	startSubSys("OrderRouter", orderRouter)

	if err := ctx.Err(); err != nil {
		abort()
		return nil, err
	}

	// Client comms RPC server.
	server, err := comms.NewServer(cfg.CommsCfg)
	if err != nil {
		abort()
		return nil, fmt.Errorf("NewServer failed: %w", err)
	}

	cfgResp, err := newConfigResponse(cfg, cfgAssets, cfgMarkets)
	if err != nil {
		abort()
		return nil, err
	}

	dexMgr := &DEX{
		network:     cfg.Network,
		markets:     markets,
		assets:      lockableAssets,
		swapper:     swapper,
		authMgr:     authMgr,
		storage:     storage,
		orderRouter: orderRouter,
		bookRouter:  bookRouter,
		subsystems:  subsystems,
		server:      server,
		configResp:  cfgResp,
	}

	comms.RegisterHTTP(msgjson.ConfigRoute, dexMgr.handleDEXConfig)

	startSubSys("Comms Server", server)

	return dexMgr, nil
}

// Asset retrieves an asset backend by its ID.
func (dm *DEX) Asset(id uint32) (*asset.BackedAsset, error) {
	asset, found := dm.assets[id]
	if !found {
		return nil, fmt.Errorf("no backend for asset %d", id)
	}
	return asset.BackedAsset, nil
}

// SetFeeRateScale specifies a scale factor that the Swapper should use to scale
// the optimal fee rates for new swaps for for the specified asset. That is,
// values above 1 increase the fee rate, while values below 1 decrease it.
func (dm *DEX) SetFeeRateScale(assetID uint32, scale float64) {
	for _, mkt := range dm.markets {
		if mkt.Base() == assetID || mkt.Quote() == assetID {
			mkt.SetFeeRateScale(assetID, scale)
		}
	}
}

// ScaleFeeRate scales the provided fee rate with the given asset's swap fee
// rate scale factor, which is 1.0 by default.
func (dm *DEX) ScaleFeeRate(assetID uint32, rate uint64) uint64 {
	// Any market will have the rate. Just find the first one.
	for _, mkt := range dm.markets {
		if mkt.Base() == assetID || mkt.Quote() == assetID {
			return mkt.ScaleFeeRate(assetID, rate)
		}
	}
	return rate
}

// Config returns the current dex configuration.
func (dm *DEX) ConfigMsg() json.RawMessage {
	dm.configRespMtx.RLock()
	defer dm.configRespMtx.RUnlock()
	return dm.configResp.configEnc
}

// TODO: for just market running status, the DEX manager should use its
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
func (dm *DEX) SuspendMarket(name string, tSusp time.Time, persistBooks bool) (suspEpoch *market.SuspendEpoch, err error) {
	name = strings.ToLower(name)

	// Locate the (running) subsystem for this market.
	i := dm.findSubsys(marketSubSysName(name))
	if i == -1 {
		err = fmt.Errorf("market subsystem %s not found", name)
		return
	}
	if !dm.subsystems[i].ssw.On() {
		err = fmt.Errorf("market subsystem %s is not running", name)
		return
	}

	// Go through the order router since OrderRouter is likely to have market
	// status tracking built into it to facilitate resume.
	suspEpoch = dm.orderRouter.SuspendMarket(name, tSusp, persistBooks)
	if suspEpoch == nil {
		err = fmt.Errorf("unable to locate market %s", name)
		return
	}

	// Update config message with suspend schedule.
	dm.configRespMtx.Lock()
	dm.configResp.setMktSuspend(name, uint64(suspEpoch.Idx), persistBooks)
	dm.configRespMtx.Unlock()

	// Broadcast a TradeSuspension notification to all connected clients.
	note, errMsg := msgjson.NewNotification(msgjson.SuspensionRoute, msgjson.TradeSuspension{
		MarketID:    name,
		FinalEpoch:  uint64(suspEpoch.Idx),
		SuspendTime: encode.UnixMilliU(suspEpoch.End),
		Persist:     persistBooks,
	})
	if errMsg != nil {
		log.Errorf("Failed to create suspend notification: %v", errMsg)
		// Notification or not, the market is resuming, so do not return error.
	} else {
		dm.server.Broadcast(note)
	}
	return
}

func (dm *DEX) findSubsys(name string) int {
	for i := range dm.subsystems {
		if dm.subsystems[i].name == name {
			return i
		}
	}
	return -1
}

// ResumeMarket launches a stopped market subsystem as early as the given time.
// The actual time the market will resume depends on the configure epoch
// duration, as the market only starts at the beginning of an epoch.
func (dm *DEX) ResumeMarket(name string, asSoonAs time.Time) (startEpoch int64, startTime time.Time, err error) {
	name = strings.ToLower(name)
	mkt := dm.markets[name]
	if mkt == nil {
		err = fmt.Errorf("unknown market %s", name)
		return
	}

	// Get the next available start epoch given the earliest allowed time.
	// Requires the market to be stopped already.
	startEpoch = mkt.ResumeEpoch(asSoonAs)
	if startEpoch == 0 {
		err = fmt.Errorf("unable to resume market %s at time %v", name, asSoonAs)
		return
	}

	// Locate the (stopped) subsystem for this market.
	i := dm.findSubsys(marketSubSysName(name))
	if i == -1 {
		err = fmt.Errorf("market subsystem %s not found", name)
		return
	}
	if dm.subsystems[i].ssw.On() {
		err = fmt.Errorf("market subsystem %s not stopped", name)
		return
	}

	// Update config message with resume schedule.
	dm.configRespMtx.Lock()
	epochLen := dm.configResp.setMktResume(name, uint64(startEpoch))
	dm.configRespMtx.Unlock()
	if epochLen == 0 {
		return // couldn't set the new start epoch
	}

	// Configure the start epoch with the Market.
	startTimeMS := int64(epochLen) * startEpoch
	startTime = encode.UnixTimeMilli(startTimeMS)
	mkt.SetStartEpochIdx(startEpoch)

	// Relaunch the market.
	ssw := dex.NewStartStopWaiter(mkt)
	dm.subsystems[i].ssw = ssw
	ssw.Start(context.Background())

	// Broadcast a TradeResumption notification to all connected clients.
	note, errMsg := msgjson.NewNotification(msgjson.ResumptionRoute, msgjson.TradeResumption{
		MarketID:   name,
		ResumeTime: uint64(startTimeMS),
		StartEpoch: uint64(startEpoch),
	})
	if errMsg != nil {
		log.Errorf("Failed to create resume notification: %v", errMsg)
		// Notification or not, the market is resuming, so do not return error.
	} else {
		dm.server.Broadcast(note)
	}

	return
}

// Accounts returns data for all accounts.
func (dm *DEX) Accounts() ([]*db.Account, error) {
	return dm.storage.Accounts()
}

// AccountInfo returns data for an account.
func (dm *DEX) AccountInfo(aid account.AccountID) (*db.Account, error) {
	return dm.storage.AccountInfo(aid)
}

// Penalize bans an account by canceling the client's orders and setting their rule
// status to rule.
func (dm *DEX) Penalize(aid account.AccountID, rule account.Rule, details string) error {
	return dm.authMgr.Penalize(aid, rule, details)
}

// Unban reverses a ban and allows a client to resume trading.
func (dm *DEX) Unban(aid account.AccountID) error {
	return dm.authMgr.Unban(aid)
}

// ForgiveMatchFail forgives a user for a specific match failure, potentially
// allowing them to resume trading if their score becomes passing.
func (dm *DEX) ForgiveMatchFail(aid account.AccountID, mid order.MatchID) (forgiven, unbanned bool, err error) {
	return dm.authMgr.ForgiveMatchFail(aid, mid)
}

// Notify sends a text notification to a connected client.
func (dm *DEX) Notify(acctID account.AccountID, msg *msgjson.Message) {
	dm.authMgr.Notify(acctID, msg)
}

// NotifyAll sends a text notification to all connected clients.
func (dm *DEX) NotifyAll(msg *msgjson.Message) {
	dm.server.Broadcast(msg)
}

// BookOrders returns booked orders for market with base and quote.
func (dm *DEX) BookOrders(base, quote uint32) ([]*order.LimitOrder, error) {
	return dm.storage.BookOrders(base, quote)
}

// EpochOrders returns epoch orders for market with base and quote.
func (dm *DEX) EpochOrders(base, quote uint32) ([]order.Order, error) {
	return dm.storage.EpochOrders(base, quote)
}

// MatchData embeds db.MatchData with decoded swap transaction coin IDs.
type MatchData struct {
	db.MatchData
	MakerSwap   string
	TakerSwap   string
	MakerRedeem string
	TakerRedeem string
}

// MarketMatches returns matches for market with base and quote.
func (dm *DEX) MarketMatches(base, quote uint32, includeInactive bool) ([]*MatchData, error) {
	baseAsset := dm.assets[base]
	if baseAsset == nil {
		return nil, fmt.Errorf("asset %d not found", base)
	}
	quoteAsset := dm.assets[quote]
	if quoteAsset == nil {
		return nil, fmt.Errorf("asset %d not found", quote)
	}
	mds, err := dm.storage.MarketMatches(base, quote, includeInactive)
	if err != nil {
		return nil, err
	}

	matchDatas := make([]*MatchData, 0, len(mds))
	for _, md := range mds {
		matchData := MatchData{
			MatchData: md.MatchData,
		}
		// asset0 is the maker swap / taker redeem asset.
		// asset1 is the taker swap / maker redeem asset.
		// Maker selling means asset 0 is base; asset 1 is quote.
		asset0, asset1 := baseAsset, quoteAsset
		if md.TakerSell {
			asset0, asset1 = quoteAsset, baseAsset
		}
		if len(md.MakerSwapCoin) > 0 {
			coinStr, err := asset0.Backend.ValidateCoinID(md.MakerSwapCoin)
			if err != nil {
				log.Errorf("Unable to decode coin %x: %v", md.MakerSwapCoin, err)
			}
			matchData.MakerSwap = coinStr
		}
		if len(md.TakerSwapCoin) > 0 {
			coinStr, err := asset1.Backend.ValidateCoinID(md.TakerSwapCoin)
			if err != nil {
				log.Errorf("Unable to decode coin %x: %v", md.TakerSwapCoin, err)
			}
			matchData.TakerSwap = coinStr
		}
		if len(md.MakerRedeemCoin) > 0 {
			coinStr, err := asset0.Backend.ValidateCoinID(md.MakerRedeemCoin)
			if err != nil {
				log.Errorf("Unable to decode coin %x: %v", md.MakerRedeemCoin, err)
			}
			matchData.MakerRedeem = coinStr
		}
		if len(md.TakerRedeemCoin) > 0 {
			coinStr, err := asset1.Backend.ValidateCoinID(md.TakerRedeemCoin)
			if err != nil {
				log.Errorf("Unable to decode coin %x: %v", md.TakerRedeemCoin, err)
			}
			matchData.TakerRedeem = coinStr
		}

		matchDatas = append(matchDatas, &matchData)
	}

	return matchDatas, nil
}

// EnableDataAPI can be called via admin API to enable or disable the HTTP data
// API endpoints.
func (dm *DEX) EnableDataAPI(yes bool) {
	dm.server.EnableDataAPI(yes)
}
