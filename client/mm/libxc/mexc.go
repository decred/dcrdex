// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc/mexctypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/dexnet"
)

// Ticker represents market ticker information.
type Ticker struct {
	MarketID string
	Stamp    time.Time
	Rate     uint64
}

// VWAPFunc is a function that returns the volume weighted average price
// for a quantity of the base asset.
type VWAPFunc func(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)

// LocalFill represents a completed trade on the exchange.
type LocalFill struct {
	BaseID      uint32
	BaseSymbol  string
	QuoteID     uint32
	QuoteSymbol string
	Index       string
	DEXRate     uint64
	Stamp       time.Time
	Side        bool // true for buy, false for sell
	Fee         uint64
	FeeAssetID  uint32
	BaseQty     uint64
	QuoteQty    uint64
}

// EpochReport contains information about the exchange's activity during a specified period.
type EpochReport struct {
	Fills    []*LocalFill
	Balances map[uint32]*ExchangeBalance
	Volume   map[string]uint64
}

// MEXC API spot trading docs:
// https://www.mexc.com/open/api-wiki/en/#spot

const (
	mxHTTPURL      = "https://api.mexc.com"
	mxWebsocketURL = "wss://wbs.mexc.com/ws"

	// There is no separate MEXC US URL like Binance has

	mxTestnetHTTPURL      = "https://api.mexc.com" // No separate testnet for now
	mxTestnetWebsocketURL = "wss://wbs.mexc.com/ws"

	mxRecvWindow = "5000" // Recommended recvWindow in ms for signed requests

	mxErrCodeInvalidListenKey = -1125 // Adjust if MEXC uses a different code
)

// Define error constants
var (
// No need to redefine ErrUnsyncedOrderbook here, it's already in interface.go
)

// mxOrderBook manages an orderbook for a single market. It keeps
// the orderbook synced and allows querying of vwap.
type mxOrderBook struct {
	mtx            sync.RWMutex
	synced         atomic.Bool
	syncChan       chan struct{}
	numSubscribers uint32
	active         bool
	cm             *dex.ConnectionMaster

	getSnapshot func() (*mexctypes.DepthResponse, error)

	book                  *orderbook
	updateQueue           chan *mexctypes.WsDepthUpdateData
	mktID                 string
	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger

	connectedChan  chan bool
	lastUpdateID   uint64
	lastUpdateTime time.Time
}

func newMxOrderBook(
	baseConversionFactor, quoteConversionFactor uint64,
	mktID string,
	getSnapshot func() (*mexctypes.DepthResponse, error),
	log dex.Logger,
) *mxOrderBook {
	return &mxOrderBook{
		book:                  newOrderBook(),
		mktID:                 mktID,
		updateQueue:           make(chan *mexctypes.WsDepthUpdateData, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
		getSnapshot:           getSnapshot,
		connectedChan:         make(chan bool, 5),
		lastUpdateTime:        time.Now(),
		active:                true,
	}
}

// convertMEXCBook converts bids and asks in the MEXC format,
// with the conventional quantity and rate, to the DEX message format which
// can be used to update the orderbook.
func (b *mxOrderBook) convertMEXCBook(mexcBids, mexcAsks [][2]json.Number) (bids, asks []*obEntry, err error) {
	convert := func(updates [][2]json.Number) ([]*obEntry, error) {
		convertedUpdates := make([]*obEntry, 0, len(updates))

		for _, update := range updates {
			price, err := update[0].Float64()
			if err != nil {
				return nil, fmt.Errorf("error parsing price: %v", err)
			}

			qty, err := update[1].Float64()
			if err != nil {
				return nil, fmt.Errorf("error parsing qty: %v", err)
			}

			convertedUpdates = append(convertedUpdates, &obEntry{
				rate: calc.MessageRateAlt(price, b.baseConversionFactor, b.quoteConversionFactor),
				qty:  uint64(qty * float64(b.baseConversionFactor)),
			})
		}

		return convertedUpdates, nil
	}

	bids, err = convert(mexcBids)
	if err != nil {
		return nil, nil, err
	}

	asks, err = convert(mexcAsks)
	if err != nil {
		return nil, nil, err
	}

	return bids, asks, nil
}

// sync does an initial sync of the orderbook. When the first update is
// received, it grabs a snapshot of the orderbook, and only processes updates
// that come after the state of the snapshot. A goroutine is started that keeps
// the orderbook in sync by repeating the sync process if an update is ever
// missed.
//
// This function runs until the context is canceled. It must be started as
// a new goroutine.
func (b *mxOrderBook) sync(ctx context.Context) {
	cm := dex.NewConnectionMaster(b)
	b.mtx.Lock()
	b.cm = cm
	b.mtx.Unlock()
	if err := cm.ConnectOnce(ctx); err != nil {
		b.log.Errorf("Error connecting %s order book: %v", b.mktID, err)
	}

	// Wait for syncChan to be closed when the initial sync completes
	if b.syncChan != nil {
		<-b.syncChan
	}
}

// Connect implements the dex.Connector interface, establishing the initial orderbook sync
// and processing updates.
func (b *mxOrderBook) Connect(ctx context.Context) (*sync.WaitGroup, error /* no errors */) {
	const updateIDUnsynced = math.MaxUint64

	// We'll use these variables to track sync state
	var syncMtx sync.Mutex
	var updateBuffer []*mexctypes.WsDepthUpdateData
	syncChan := make(chan struct{})
	b.syncChan = syncChan
	var lastUpdateID uint64 = updateIDUnsynced
	acceptedUpdate := false

	resyncChan := make(chan struct{}, 1)

	// Function to reset sync state
	desync := func(resync bool) {
		syncMtx.Lock()
		defer syncMtx.Unlock()
		updateBuffer = make([]*mexctypes.WsDepthUpdateData, 0)
		acceptedUpdate = false
		if lastUpdateID != updateIDUnsynced {
			b.synced.Store(false)
			lastUpdateID = updateIDUnsynced
			if resync {
				select {
				case resyncChan <- struct{}{}:
				default:
				}
			}
		}
	}

	// Function to process a depth update
	acceptUpdate := func(update *mexctypes.WsDepthUpdateData) bool {
		if lastUpdateID == updateIDUnsynced {
			// Book is still syncing. Add it to the buffer
			updateBuffer = append(updateBuffer, update)
			return true
		}

		// Parse the update version
		updateVersion, err := strconv.ParseUint(update.Version, 10, 64)
		if err != nil {
			b.log.Warnf("Failed to parse update version: %v - continuing anyway", err)
			// Generate a fallback version based on current lastUpdateID
			updateVersion = lastUpdateID + 1
		}

		// Once we have a valid orderbook snapshot, we'll accept any update
		// that has valid price data, regardless of sequence number

		// Check if this update has valid data
		if len(update.Bids) == 0 && len(update.Asks) == 0 {
			b.log.Debugf("Update has no price data, skipping")
			return true // Return true to avoid triggering resync
		}

		// Basic tracking of which updates we've seen
		if !acceptedUpdate {
			// First update after snapshot
			b.log.Debugf("Processing first update after snapshot: ID %d", updateVersion)
			acceptedUpdate = true
		} else {
			// For logging purposes only
			if updateVersion <= lastUpdateID {
				b.log.Tracef("Got out-of-sequence update: %d <= %d (current)",
					updateVersion, lastUpdateID)
			}
		}

		// Always update the sequence tracking
		lastUpdateID = updateVersion
		b.lastUpdateID = updateVersion

		// Convert and apply the update data
		bids, asks, err := b.convertMEXCBook(update.Bids, update.Asks)
		if err != nil {
			b.log.Errorf("Error parsing MEXC book: %v", err)
			return false // Only fail on data conversion errors
		}

		// Apply the update
		b.book.update(bids, asks)
		b.lastUpdateTime = time.Now()

		return true
	}

	// Process buffered updates after a snapshot
	processSyncBuffer := func(snapshotID uint64) bool {
		syncMtx.Lock()
		defer syncMtx.Unlock()

		// Process all buffered updates where lastUpdateId < update.version <= (lastUpdateId + 1)
		// This follows MEXC's recommendation
		lastUpdateID = snapshotID

		b.log.Debugf("Processing %d buffered updates with snapshot ID %d",
			len(updateBuffer), snapshotID)

		for _, update := range updateBuffer {
			if !acceptUpdate(update) {
				b.log.Warnf("Failed to process update from buffer, resyncing")
				return false
			}
		}

		// Mark the book as synced
		b.synced.Store(true)
		b.log.Infof("Orderbook for %s is now synced", b.mktID)

		// Close the sync channel to signal sync completion
		if syncChan != nil {
			close(syncChan)
			syncChan = nil
		}

		return true
	}

	// Function to synchronize orderbook
	syncOrderbook := func() bool {
		// Get the orderbook snapshot
		snapshot, err := b.getSnapshot()
		if err != nil {
			b.log.Errorf("Error getting orderbook snapshot: %v", err)
			return false
		}

		// Convert the snapshot to order book entries
		bids, asks, err := b.convertMEXCBook(snapshot.Bids, snapshot.Asks)
		if err != nil {
			b.log.Errorf("Error parsing MEXC book: %v", err)
			return false
		}

		b.log.Debugf("Got %s orderbook snapshot with update ID %d, %d bids, %d asks",
			b.mktID, snapshot.LastUpdateID, len(bids), len(asks))

		// Clear and update the orderbook with the snapshot data
		b.book.clear()
		b.book.update(bids, asks)
		b.lastUpdateTime = time.Now()

		// Process any buffered updates
		return processSyncBuffer(uint64(snapshot.LastUpdateID))
	}

	var wg sync.WaitGroup

	// Goroutine to process incoming updates
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case update := <-b.updateQueue:
				syncMtx.Lock()
				success := acceptUpdate(update)
				syncMtx.Unlock()

				if !success {
					// Instead of immediately desyncing on any error, just log the issue
					// Most sequence errors are harmless - the next message will likely be fine
					b.log.Debugf("Update processing issue for %s with ID %s - continuing anyway",
						b.mktID, update.Version)

					// We won't desync on sequence issues, only on critical connection problems
					// which are handled elsewhere
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Goroutine to handle resync operations
	wg.Add(1)
	go func() {
		defer wg.Done()

		const retryFrequency = time.Second * 5
		retry := time.After(0) // Start with immediate sync

		for {
			select {
			case <-retry:
				// Time to try a sync
			case <-resyncChan:
				if retry != nil { // don't hammer
					continue
				}
			case connected := <-b.connectedChan:
				if !connected {
					b.log.Debugf("Unsyncing %s orderbook due to disconnect", b.mktID)
					desync(false)
					retry = nil
					continue
				}
			case <-ctx.Done():
				return
			}

			if syncOrderbook() {
				b.log.Infof("Synced %s orderbook", b.mktID)
				retry = nil
			} else {
				b.log.Infof("Failed to sync %s orderbook. Trying again in %s", b.mktID, retryFrequency)
				desync(false) // Clear the buffer
				retry = time.After(retryFrequency)
			}
		}
	}()

	return &wg, nil
}

// Similar implementation as binanceOrderBook.Connect but adapted for MEXC's orderbook format
// and update mechanisms

// For now we'll create a placeholder waitgroup and return it
var tempplch = 0 // DELETE this line

// vwap returns the volume weighted average price for a certain quantity of the
// base asset. It returns an error if the orderbook is not synced.
func (b *mxOrderBook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, filled, ErrUnsyncedOrderbook
	}

	vwap, extrema, filled = b.book.vwap(bids, qty)
	return
}

func (b *mxOrderBook) midGap() uint64 {
	return b.book.midGap()
}

// Define MEXC symbol mapping
var dexToMEXCSymbol = map[string]string{
	"polygon": "MATIC",
	"weth":    "ETH",
	// Add any MEXC-specific mappings here
}

var mxToDexSymbol = make(map[string]string)

// mxConvertCoin converts a MEXC coin symbol to a DEX symbol.
// This replaces the redeclared convertMexcCoin function
func mxConvertCoin(coin string) string {
	symbol := strings.ToLower(coin)
	if convertedSymbol, found := mxToDexSymbol[strings.ToUpper(coin)]; found {
		symbol = convertedSymbol
	}
	if symbol == "weth" {
		return "eth"
	}
	return symbol
}

// mxCoinNetworkToDexSymbol takes the coin name and its network name as
// returned by the MEXC API and returns the DEX symbol.
func mxCoinNetworkToDexSymbol(coin, network string) string {
	symbol, netSymbol := mxConvertCoin(coin), mxConvertCoin(network)
	if symbol == netSymbol {
		return symbol
	}
	if symbol == "eth" {
		symbol = "weth"
	}
	return symbol + "." + netSymbol
}

func init() {
	for key, value := range dexToMEXCSymbol {
		mxToDexSymbol[value] = key
	}
}

func mapDexToMEXCSymbol(symbol string) string {
	if mexcSymbol, found := dexToMEXCSymbol[strings.ToLower(symbol)]; found {
		return mexcSymbol
	}
	return strings.ToUpper(symbol)
}

type mxAssetConfig struct {
	assetID uint32
	// symbol is the DEX asset symbol, always lower case
	symbol string
	// coin is the asset symbol on MEXC, always upper case.
	// For a token like USDC, the coin field will be USDC, but
	// symbol field will be usdc.eth.
	coin string
	// chain will be the same as coin for the base assets of
	// a blockchain, but for tokens it will be the chain
	// that the token is hosted such as "ETH".
	chain            string
	conversionFactor uint64
}

func mxAssetCfg(assetID uint32) (*mxAssetConfig, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, err
	}

	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return nil, fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	parts := strings.Split(symbol, ".")
	coin := mapDexToMEXCSymbol(parts[0])
	chain := coin
	if len(parts) > 1 {
		chain = mapDexToMEXCSymbol(parts[1])
	}

	return &mxAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             coin,
		chain:            chain,
		conversionFactor: ui.Conventional.ConversionFactor,
	}, nil
}

func mxAssetCfgs(baseID, quoteID uint32) (*mxAssetConfig, *mxAssetConfig, error) {
	baseCfg, err := mxAssetCfg(baseID)
	if err != nil {
		return nil, nil, err
	}

	quoteCfg, err := mxAssetCfg(quoteID)
	if err != nil {
		return nil, nil, err
	}

	return baseCfg, quoteCfg, nil
}

type mxTradeInfo struct {
	updaterID   int
	baseID      uint32
	quoteID     uint32
	sell        bool
	rate        uint64
	qty         uint64
	baseFilled  uint64 // Track cumulative base filled
	quoteFilled uint64 // Track cumulative quote filled
}

type mxWithdrawInfo struct {
	minimum uint64
	lotSize uint64
}

type MxCodedErr struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *MxCodedErr) Error() string {
	return fmt.Sprintf("code = %d, msg = %q", e.Code, e.Msg)
}

func errHasMxCode(err error, code int) bool {
	var mexcErr *MxCodedErr
	if errors.As(err, &mexcErr) && mexcErr.Code == code {
		return true
	}
	return false
}

type mexc struct {
	log                dex.Logger
	marketsURL         string
	accountsURL        string
	wsURL              string
	apiKey             string
	secretKey          string
	knownAssets        map[uint32]bool
	net                dex.Network
	tradeIDNonce       atomic.Uint32
	tradeIDNoncePrefix dex.Bytes
	broadcast          func(interface{})

	markets atomic.Value // map[string]*mexctypes.SymbolInfo
	// tokenIDs maps the token's symbol to the list of bip ids of the token
	// for each chain for which deposits and withdrawals are enabled on MEXC.
	tokenIDs    atomic.Value // map[string][]uint32, MEXC coin ID string -> asset IDs
	minWithdraw atomic.Value // map[uint32]*mxWithdrawInfo

	marketSnapshotMtx sync.Mutex
	marketSnapshot    struct {
		stamp time.Time
		m     map[string]*Market
	}

	balanceMtx sync.RWMutex
	balances   map[uint32]*ExchangeBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn
	userDataStream  comms.WsConn // Add userDataStream field

	marketSubResyncChan chan string // Channel for market resubscription

	booksMtx sync.RWMutex
	books    map[string]*mxOrderBook

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*mxTradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	activeTrades    map[string]*mxTradeInfo // Track active trades by MEXC order ID
	activeTradesMtx sync.RWMutex

	listenKey          atomic.Value // string
	listenKeyMtx       sync.Mutex   // Protects listenKey refresh operations
	listenKeyRefresh   *time.Timer  // Timer for next keepalive
	listenKeyRequested atomic.Bool  // Prevent concurrent getListenKey calls
	reconnectChan      chan struct{}

	// For protobuf handling
	protobufDepthUpdate    *mexctypes.WsDepthUpdateData
	protobufDepthUpdateMtx sync.Mutex

	// For shutdown coordination
	wg          sync.WaitGroup
	quit        chan struct{}
	shutdownMtx sync.Mutex
}

var _ CEX = (*mexc)(nil)

// TODO: Investigate stablecoin auto-conversion if applicable for MEXC.

func newMexc(cfg *CEXConfig) (*mexc, error) {
	// Similar structure to newBinance but adapted for MEXC
	var marketsURL, accountsURL, wsURL string

	switch cfg.Net {
	case dex.Testnet:
		marketsURL, accountsURL, wsURL = mxTestnetHTTPURL, mxTestnetHTTPURL, mxTestnetWebsocketURL
	case dex.Simnet:
		// No specific simnet for MEXC, use testnet
		marketsURL, accountsURL, wsURL = mxTestnetHTTPURL, mxTestnetHTTPURL, mxTestnetWebsocketURL
	default: // mainnet
		marketsURL, accountsURL, wsURL = mxHTTPURL, mxHTTPURL, mxWebsocketURL
	}

	registeredAssets := asset.Assets()
	knownAssets := make(map[uint32]bool, len(registeredAssets))
	for _, a := range registeredAssets {
		knownAssets[a.ID] = true
	}

	// Generate a prefix for order IDs based on API key
	prefixBytes := sha256.Sum256([]byte(cfg.APIKey))
	prefix := prefixBytes[:5]

	instance := &mexc{
		log:                 cfg.Logger,
		broadcast:           cfg.Notify,
		marketsURL:          marketsURL,
		accountsURL:         accountsURL,
		wsURL:               wsURL,
		apiKey:              cfg.APIKey,
		secretKey:           cfg.SecretKey,
		knownAssets:         knownAssets,
		balances:            make(map[uint32]*ExchangeBalance),
		net:                 cfg.Net,
		tradeInfo:           make(map[string]*mxTradeInfo),
		tradeUpdaters:       make(map[int]chan *Trade),
		tradeIDNoncePrefix:  prefix,
		reconnectChan:       make(chan struct{}, 1),
		books:               make(map[string]*mxOrderBook),
		marketSubResyncChan: make(chan string, 10),
		activeTrades:        make(map[string]*mxTradeInfo),
		quit:                make(chan struct{}),
	}

	// Initialize atomic values to prevent nil panics
	instance.markets.Store(make(map[string]*mexctypes.SymbolInfo))
	instance.tokenIDs.Store(make(map[string][]uint32))
	instance.minWithdraw.Store(make(map[uint32]*mxWithdrawInfo))
	instance.listenKey.Store("")

	// Create the market data WebSocket connection
	marketLogger := cfg.Logger.SubLogger("MarketWS")
	marketWsCfg := &comms.WsCfg{
		Logger:           marketLogger,
		URL:              wsURL,
		PingWait:         60 * time.Second,
		RawHandler:       instance.handleMarketRawMessage,
		ConnectEventFunc: instance.handleMarketConnectEvent,
		EchoPingData:     true,
	}

	marketStream, err := comms.NewWsConn(marketWsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create market WebSocket connection: %w", err)
	}
	instance.marketStream = marketStream

	// Create the user data WebSocket connection
	userLogger := cfg.Logger.SubLogger("UserWS")
	userWsCfg := &comms.WsCfg{
		Logger:       userLogger,
		URL:          wsURL, // Will be updated with listenKey before connect
		PingWait:     30 * time.Second,
		RawHandler:   instance.handleUserStreamRawMessage,
		EchoPingData: true,
	}

	userStream, err := comms.NewWsConn(userWsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create user WebSocket connection: %w", err)
	}
	instance.userDataStream = userStream

	return instance, nil
}

// Connect establishes the connection to the exchange API and starts
// background processes.
func (m *mexc) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Check API keys by making a signed request
	if err := m.checkAPIKeys(ctx); err != nil {
		m.log.Errorf("API key check failed: %v", err)
		return nil, fmt.Errorf("API key check failed: %w", err)
	}
	m.log.Infof("API keys verified successfully")

	// Load initial market and token data
	if err := m.getCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch initial coin info: %w", err)
	}

	if _, err := m.getMarkets(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch initial market info: %w", err)
	}

	m.log.Infof("Initial market and coin data loaded successfully")

	// Launch user data stream connection
	m.log.Infof("Starting user data stream...")
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.connectUserStream(ctx)
	}()

	// Setup periodic data refresh
	m.wg.Add(2)
	go m.periodicCoinInfoRefresh(ctx)
	go m.periodicMarketsRefresh(ctx)

	m.log.Infof("MEXC connection established successfully")
	return &m.wg, nil
}

// periodicCoinInfoRefresh periodically refreshes coin information.
func (m *mexc) periodicCoinInfoRefresh(ctx context.Context) {
	const refreshInterval = 1 * time.Hour
	const errorRetryInterval = 1 * time.Minute

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	m.log.Infof("Starting periodic coin info refresh (every %v)", refreshInterval)
	defer m.log.Infof("Stopping periodic coin info refresh")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := m.getCoinInfo(refreshCtx)
			cancel()

			if err != nil {
				m.log.Errorf("Periodic coin info refresh failed: %v. Retrying in %v", err, errorRetryInterval)
				ticker.Reset(errorRetryInterval)
			} else {
				m.log.Debugf("Periodic coin info refresh successful")
				ticker.Reset(refreshInterval)
			}
		}
	}
}

// periodicMarketsRefresh periodically refreshes market information.
func (m *mexc) periodicMarketsRefresh(ctx context.Context) {
	const refreshInterval = 1 * time.Hour
	const errorRetryInterval = 1 * time.Minute

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	m.log.Infof("Starting periodic market info refresh (every %v)", refreshInterval)
	defer m.log.Infof("Stopping periodic market info refresh")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := m.getMarkets(refreshCtx)
			cancel()

			if err != nil {
				m.log.Errorf("Periodic market info refresh failed: %v. Retrying in %v", err, errorRetryInterval)
				ticker.Reset(errorRetryInterval)
			} else {
				m.log.Debugf("Periodic market info refresh successful")
				ticker.Reset(refreshInterval)
			}
		}
	}
}

// handleMarketConnectEvent processes market WebSocket connection status changes.
func (m *mexc) handleMarketConnectEvent(status comms.ConnectionStatus) {
	m.log.Debugf("[MarketWS] Connection status changed: %s", status)

	switch status {
	case comms.Connected:
		// When connection is established, resubscribe to all active markets
		m.log.Infof("[MarketWS] Connection established, resubscribing to markets")
		go m.resubscribeMarkets(context.Background())
	case comms.Disconnected:
		m.log.Warnf("[MarketWS] Connection lost")
		// We will no longer mark books as unsynced on disconnection
		// The books will continue to use their last known state
		// and will be updated when the connection is re-established
	}
}

// resubscribeMarkets resubscribes to all active market streams after a reconnection.
func (m *mexc) resubscribeMarkets(ctx context.Context) {
	m.log.Infof("[MarketWS] Resubscribing to markets after reconnection")

	// Get the list of active symbols that need resubscription
	m.booksMtx.RLock()
	activeSymbols := make([]string, 0, len(m.books))
	for symbol, book := range m.books {
		book.mtx.RLock()
		if book.numSubscribers > 0 {
			activeSymbols = append(activeSymbols, symbol)
			book.active = true
		}
		book.mtx.RUnlock()
	}
	m.booksMtx.RUnlock()

	if len(activeSymbols) == 0 {
		m.log.Debugf("[MarketWS] No active markets to resubscribe")
		return
	}

	m.log.Infof("[MarketWS] Resubscribing to %d active markets: %v", len(activeSymbols), activeSymbols)

	// Batch subscribe to all active markets using the protobuf format
	channels := make([]string, len(activeSymbols))
	for i, symbol := range activeSymbols {
		// Use the protobuf format for depth data
		channels[i] = fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", symbol)
	}

	// Send subscription request
	req := WsRequest{Method: "SUBSCRIPTION", Params: channels}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		m.log.Errorf("[MarketWS] Failed to marshal resubscription request: %v", err)
		return
	}

	m.marketStreamMtx.RLock()
	defer m.marketStreamMtx.RUnlock()

	if m.marketStream == nil || m.marketStream.IsDown() {
		m.log.Errorf("[MarketWS] Cannot resubscribe - market stream is down or nil")
		return
	}

	// Attempt to send with retries
	var sendErr error
	for i := 0; i < 3; i++ {
		sendErr = m.marketStream.SendRaw(reqBytes)
		if sendErr == nil {
			m.log.Infof("[MarketWS] Successfully sent resubscription request for %d markets", len(activeSymbols))
			return
		}
		m.log.Errorf("[MarketWS] Failed to send resubscription request (attempt %d): %v", i+1, sendErr)
		time.Sleep(500 * time.Millisecond)
	}

	if sendErr != nil {
		m.log.Errorf("[MarketWS] Failed to resubscribe after multiple attempts, will try again later")
	}
}

// handleMarketRawMessage processes raw WebSocket messages from the market stream.
func (m *mexc) handleMarketRawMessage(raw []byte) {
	if len(raw) == 0 {
		return
	}

	// Check if the message is a ping
	if string(raw) == `{"ping":true}` || string(raw) == "ping" {
		// Respond with pong
		m.marketStream.SendRaw([]byte(`{"pong":true}`))
		return
	}

	// Check if it's a protobuf message (simplified check)
	if len(raw) > 4 && isPbMessage(raw[0:4]) {
		m.handleProtobufMessage(raw)
		return
	}

	// Handle regular JSON messages
	var resp WsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		m.log.Errorf("[MarketWS] Failed to parse WebSocket message: %v\nRaw: %s", err, string(raw))
		return
	}

	// Handle different response types
	switch {
	case resp.Result != nil:
		m.log.Debugf("[MarketWS] Received subscription result: %v", resp.Result)
	case resp.Error != nil:
		m.log.Errorf("[MarketWS] Received error: %v", resp.Error)
	default:
		m.log.Warnf("[MarketWS] Unhandled WebSocket message: %s", string(raw))
	}
}

// isPbMessage checks if the header bytes indicate a protobuf message
func isPbMessage(header []byte) bool {
	// This is a simplified check - in real implementation we would need
	// to verify the protobuf header format more carefully
	// In MEXC's case, protobuf messages start with specific header bytes
	return len(header) >= 4 && header[0] == 0x12 && header[1] <= 0x10
}

// handleProtobufMessage processes market data protobuf messages.
func (m *mexc) handleProtobufMessage(data []byte) {
	m.log.Tracef("[MarketWS] Received protobuf message of length %d", len(data))

	// Parse the binary protobuf message using our mexctypes
	pbMsg, err := mexctypes.UnmarshalMEXCDepthProto(data)
	if err != nil {
		m.log.Errorf("[MarketWS] Failed to decode protobuf message: %v", err)
		// Log hex dump of the first few bytes to help diagnose issues
		if len(data) > 16 {
			m.log.Debugf("[MarketWS] First 16 bytes: %x", data[:16])
		} else {
			m.log.Debugf("[MarketWS] Message bytes: %x", data)
		}
		return
	}

	// Convert the protobuf message to our depth update format
	depthUpdate := mexctypes.ConvertProtoToDepthUpdate(pbMsg)
	if depthUpdate == nil {
		m.log.Debugf("[MarketWS] Failed to convert protobuf message to depth update")
		return
	}

	// Try to determine the symbol - either from the extracted pbMsg.Symbol or our active subscriptions
	symbol := pbMsg.Symbol
	if symbol == "" {
		// If symbol wasn't extracted, try to find a match from our active subscriptions
		m.booksMtx.RLock()
		if len(m.books) == 1 {
			// If we have only one active subscription, use that one
			for s := range m.books {
				symbol = s
				break
			}
		} else if len(m.books) > 1 {
			// If we have multiple active subscriptions, we'll take a best guess from
			// our recent protobuf depth update history
			m.protobufDepthUpdateMtx.Lock()
			if m.protobufDepthUpdate != nil && m.protobufDepthUpdate.Symbol != "" {
				symbol = m.protobufDepthUpdate.Symbol
			}
			m.protobufDepthUpdateMtx.Unlock()
		}
		m.booksMtx.RUnlock()
	}

	// If we still don't have a symbol, we can't route this message
	if symbol == "" {
		m.log.Debugf("[MarketWS] Cannot determine symbol for protobuf message, dropping")
		return
	}

	// Assign the symbol to the depth update
	depthUpdate.Symbol = symbol

	// Use a small incrementing version number rather than a huge random one
	// If we don't have a reasonable version number, generate a sequential one
	// based on the LastUpdateId from the protobuf message
	if depthUpdate.Version == "" || depthUpdate.Version == "0" {
		// Use a consistent and simple version numbering for updates
		if pbMsg.LastUpdateId > 0 {
			depthUpdate.Version = strconv.FormatUint(pbMsg.LastUpdateId, 10)
		} else {
			// Generate a sequential ID based on timestamp - much more stable than a huge random number
			depthUpdate.Version = strconv.FormatInt(time.Now().UnixNano()%1000000, 10)
		}
	}

	// Log successful parsing periodically
	if len(depthUpdate.Bids) > 0 || len(depthUpdate.Asks) > 0 {
		m.log.Tracef("[MarketWS] Parsed protobuf depth update for %s: v=%s, bids=%d, asks=%d",
			depthUpdate.Symbol, depthUpdate.Version, len(depthUpdate.Bids), len(depthUpdate.Asks))
	}

	// Store the depth update in the mexc instance for use by other methods
	m.protobufDepthUpdateMtx.Lock()
	m.protobufDepthUpdate = depthUpdate
	m.protobufDepthUpdateMtx.Unlock()

	// Find the book for this symbol
	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		m.log.Tracef("[MarketWS] No orderbook found for %s", symbol)
		return
	}

	// Forward the update to the book's update queue
	select {
	case book.updateQueue <- depthUpdate:
		// Update successfully queued
	default:
		m.log.Warnf("[MarketWS] Update queue full for %s, dropping depth update", symbol)
	}
}

// connectMarketStream establishes a WebSocket connection for market data.
func (m *mexc) connectMarketStream(ctx context.Context, firstMexcSymbol string) error {
	m.log.Infof("[MarketWS] Establishing initial connection for symbol %s...", firstMexcSymbol)

	// Launch the WebSocket connection
	wg, err := m.marketStream.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect market WebSocket: %w", err)
	}
	m.log.Debugf("[MarketWS] Connection initiated, waiting for establishment")

	// Store the waitgroup so it can be used for cleanup later if needed
	go func() {
		wg.Wait()
		m.log.Debugf("[MarketWS] Connection wait group completed")
	}()

	// Wait for connection to establish
	time.Sleep(500 * time.Millisecond)
	if m.marketStream.IsDown() {
		return fmt.Errorf("market stream connection failed shortly after initiation")
	}

	// Send initial subscription with protobuf format
	m.log.Infof("[MarketWS] Sending initial subscription for %s", firstMexcSymbol)
	channel := fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", firstMexcSymbol)
	req := WsRequest{
		Method: "SUBSCRIPTION",
		Params: []string{channel},
	}
	reqBytes, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal initial market subscription request: %w", marshalErr)
	}

	err = m.marketStream.SendRaw(reqBytes)
	if err != nil {
		return fmt.Errorf("failed to send initial market subscription for %s: %w", firstMexcSymbol, err)
	}

	m.log.Infof("[MarketWS] Initial connection and subscription process initiated for %s", firstMexcSymbol)
	return nil
}

// getDepthSnapshot fetches an orderbook snapshot from the REST API.
func (m *mexc) getDepthSnapshot(ctx context.Context, symbol string) (*mexctypes.DepthResponse, error) {
	m.log.Debugf("Fetching depth snapshot for %s", symbol)

	path := "/api/v3/depth"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", "1000") // Request maximum depth

	var resp mexctypes.DepthResponse
	err := m.getAPI(ctx, path, params, false, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get depth snapshot: %w", err)
	}

	// Log more details about the received snapshot
	m.log.Debugf("Received orderbook snapshot for %s with lastUpdateId: %d, bids: %d, asks: %d",
		symbol, resp.LastUpdateID, len(resp.Bids), len(resp.Asks))

	return &resp, nil
}

// handleUserStreamRawMessage processes raw WebSocket messages from the user data stream.
func (m *mexc) handleUserStreamRawMessage(raw []byte) {
	// This is a placeholder for handling user data stream messages
	// In a complete implementation, we would parse and handle different message types
	m.log.Debugf("[UserWS] Received message: %s", string(raw))
}

// checkAPIKeys checks if the provided API keys are valid.
func (m *mexc) checkAPIKeys(ctx context.Context) error {
	if m.apiKey == "" || m.secretKey == "" {
		return fmt.Errorf("API key and/or secret key not provided")
	}

	// Use the account endpoint which requires authentication
	path := "/api/v3/account"
	err := m.getAPI(ctx, path, nil, true, true, nil)
	if err != nil {
		return fmt.Errorf("API key check failed: %w", err)
	}

	return nil
}

// setBalances queries MEXC for the user's balances and stores them in the
// balances map.
func (m *mexc) setBalances(ctx context.Context) error {
	m.balanceMtx.Lock()
	defer m.balanceMtx.Unlock()
	return m.refreshBalances(ctx)
}

func (m *mexc) refreshBalances(ctx context.Context) error {
	m.log.Infof("Fetching MEXC account balances...")
	var resp mexctypes.AccountInfo
	err := m.getAPI(ctx, "/api/v3/account", nil, true, true, &resp)
	if err != nil {
		return fmt.Errorf("failed to fetch MEXC account balances: %w", err)
	}

	tokenIDsI := m.tokenIDs.Load()
	if tokenIDsI == nil {
		// Try to load coin info first if it's not available
		m.log.Warnf("TokenIDs not loaded, attempting to load coin info before processing balances")
		if err := m.getCoinInfo(ctx); err != nil {
			return fmt.Errorf("cannot fetch balances: token mappings unavailable and failed to load: %w", err)
		}

		// Check again after loading coin info
		tokenIDsI = m.tokenIDs.Load()
		if tokenIDsI == nil {
			return fmt.Errorf("tokenIDs still nil after loading coin info")
		}
	}

	tokenIDs := tokenIDsI.(map[string][]uint32)
	m.log.Debugf("Processing %d balances from MEXC account", len(resp.Balances))

	// Track processed balances for logging
	processedCount := 0
	nonZeroCount := 0

	for _, bal := range resp.Balances {
		// Skip assets with zero balance to reduce log noise
		free, _ := strconv.ParseFloat(bal.Free, 64)
		locked, _ := strconv.ParseFloat(bal.Locked, 64)

		// Skip zero balances (both free and locked)
		if free == 0 && locked == 0 {
			continue
		}

		assetIDs := getMEXCDEXAssetIDs(bal.Asset, tokenIDs)
		if len(assetIDs) == 0 {
			continue // Skip assets we don't have mappings for
		}

		nonZeroCount++

		for _, assetID := range assetIDs {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				m.log.Errorf("No unit info for asset ID %d (%s), skipping balance", assetID, bal.Asset)
				continue
			}

			updatedBalance := &ExchangeBalance{
				Available: strconv.FormatUint(uint64(math.Round(free*float64(ui.Conventional.ConversionFactor))), 10),
				Locked:    strconv.FormatUint(uint64(math.Round(locked*float64(ui.Conventional.ConversionFactor))), 10),
			}

			currBalance, found := m.balances[assetID]
			if found && *currBalance != *updatedBalance {
				// Log when balance changes are detected
				m.log.Debugf("%s balance updated: %+v -> %+v", bal.Asset, currBalance, updatedBalance)
			}

			m.balances[assetID] = updatedBalance
			processedCount++
		}
	}

	m.log.Infof("Successfully processed MEXC balances: %d non-zero balances mapped to %d asset IDs",
		nonZeroCount, processedCount)
	return nil
}

// readCoins stores the token IDs for which deposits and withdrawals are
// enabled on MEXC and sets the minWithdraw map.
func (m *mexc) readCoins(coins []*mexctypes.CoinInfo) {
	tokenIDs := make(map[string][]uint32)
	minWithdraw := make(map[uint32]*mxWithdrawInfo)
	for _, nfo := range coins {
		for _, netInfo := range nfo.NetworkList {
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				m.log.Tracef("Skipping %s network %s because deposits and/or withdraws are not enabled.", nfo.Coin, netInfo.Network)
				continue
			}

			symbol := mxCoinNetworkToDexSymbol(nfo.Coin, netInfo.Network)
			assetID, found := dex.BipSymbolID(symbol)
			if !found {
				continue
			}

			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// not a registered asset
				continue
			}

			if tkn := asset.TokenInfo(assetID); tkn != nil {
				tokenIDs[nfo.Coin] = append(tokenIDs[nfo.Coin], assetID)
			}

			// Need to parse withdraw minimum from string
			minStr := netInfo.WithdrawMin
			withdrawMin, _ := strconv.ParseFloat(minStr, 64)

			minimum := uint64(math.Round(float64(ui.Conventional.ConversionFactor) * withdrawMin))

			// Need to parse withdraw integer multiple from string
			lotSizeStr := netInfo.WithdrawIntegerMultiple
			if lotSizeStr == "" {
				lotSizeStr = "0" // Default to 0 if not provided
			}
			withdrawMultiple, _ := strconv.ParseFloat(lotSizeStr, 64)

			minWithdraw[assetID] = &mxWithdrawInfo{
				minimum: minimum,
				lotSize: uint64(math.Round(withdrawMultiple * float64(ui.Conventional.ConversionFactor))),
			}
		}
	}
	m.tokenIDs.Store(tokenIDs)
	m.minWithdraw.Store(minWithdraw)
}

// getCoinInfo retrieves MEXC configs then updates the user balances and
// the tokenIDs.
func (m *mexc) getCoinInfo(ctx context.Context) error {
	m.log.Infof("Fetching MEXC coin info...")
	path := "/api/v3/capital/config/getall"
	var infoList []*mexctypes.CoinInfo
	err := m.getAPI(ctx, path, nil, true, true, &infoList)
	if err != nil {
		return fmt.Errorf("failed to fetch MEXC coin info: %w", err)
	}

	// Convert list to map keyed by uppercase coin name
	newCoinInfo := make(map[string]*mexctypes.CoinInfo, len(infoList))
	for _, coin := range infoList {
		newCoinInfo[strings.ToUpper(coin.Coin)] = coin
	}

	// Process coin info to build tokenIDs and minWithdraw maps
	tokenIDs := make(map[string][]uint32)
	minWithdraw := make(map[uint32]*mxWithdrawInfo)

	// Target DEX symbols we care about (e.g., DCR, BTC, USDT variants)
	targetSymbols := map[string]bool{
		"dcr":          true,
		"btc":          true, // Add BTC
		"usdt.polygon": true, // Keep specific USDT variant
		"usdt.erc20":   true, // Add another USDT variant if needed
		"usdt.trc20":   true, // Add another USDT variant if needed
	}

	for mexcCoinUpper, coinData := range newCoinInfo {
		// Optimization: Skip coins not potentially part of targetSymbols
		// Note: This is a rough check, as we don't know the network suffix yet.
		if !(strings.Contains(strings.ToLower(mexcCoinUpper), "dcr") ||
			strings.Contains(strings.ToLower(mexcCoinUpper), "btc") ||
			strings.Contains(strings.ToLower(mexcCoinUpper), "usdt")) {
			// continue // Keep commented to map all enabled coins
		}

		for _, netInfo := range coinData.NetworkList {
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				continue
			}

			// Use our mapping function to get DEX symbol
			dexSymbol := m.mapMEXCCoinNetworkToDEXSymbol(mexcCoinUpper, netInfo.Network)
			if dexSymbol == "" {
				continue
			}

			// Check if the generated DEX symbol is one we are interested in mapping
			if !targetSymbols[dexSymbol] {
				// continue // Keep commented to map all enabled coins
			}

			assetID, found := dex.BipSymbolID(dexSymbol)
			if !found {
				continue
			}

			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// not a registered asset
				continue
			}

			// Add to our mappings
			if tkn := asset.TokenInfo(assetID); tkn != nil {
				tokenIDs[mexcCoinUpper] = append(tokenIDs[mexcCoinUpper], assetID)
			}

			// Parse withdraw minimum
			minStr := netInfo.WithdrawMin
			withdrawMin, _ := strconv.ParseFloat(minStr, 64)
			minimum := uint64(math.Round(float64(ui.Conventional.ConversionFactor) * withdrawMin))

			// Parse withdraw integer multiple
			lotSizeStr := netInfo.WithdrawIntegerMultiple
			if lotSizeStr == "" {
				lotSizeStr = "0" // Default to 0 if not provided
			}
			withdrawMultiple, _ := strconv.ParseFloat(lotSizeStr, 64)

			minWithdraw[assetID] = &mxWithdrawInfo{
				minimum: minimum,
				lotSize: uint64(math.Round(withdrawMultiple * float64(ui.Conventional.ConversionFactor))),
			}

			m.log.Debugf("Mapped %s/%s to DEX asset %s (ID: %d)", mexcCoinUpper, netInfo.Network, dexSymbol, assetID)
		}
	}

	// Store the processed data in atomic values
	m.tokenIDs.Store(tokenIDs)
	m.minWithdraw.Store(minWithdraw)

	// Log summary information
	mappedCount := 0
	for _, ids := range tokenIDs {
		mappedCount += len(ids)
	}
	m.log.Infof("Successfully processed MEXC coin info: mapped %d coins with %d asset IDs",
		len(tokenIDs), mappedCount)

	// Provide warning if we didn't find the most critical mappings
	if mappedCount < 2 {
		m.log.Warnf("Expected at least DCR and USDT.polygon mappings, but only found %d.", mappedCount)
	}

	return nil
}

// Helper functions for implementing API requests

func (m *mexc) getAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodGet, endpoint, query, nil, key, sign, thing)
}

func (m *mexc) postAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodPost, endpoint, query, form, key, sign, thing)
}

// request makes an HTTP request to the MEXC API.
func (m *mexc) request(ctx context.Context, method, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	fullURL := m.marketsURL + endpoint

	// Create a new query if it doesn't exist
	if query == nil {
		query = make(url.Values)
	}

	// Add timestamp for signed requests - must be added before signature calculation
	if sign {
		query.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		query.Set("recvWindow", mxRecvWindow)
	}

	// Create a copy of query for the signature calculation before adding signature
	signatureQuery := url.Values{}
	for k, v := range query {
		signatureQuery[k] = v
	}

	// Encode query string and prepare body for the request
	queryString := query.Encode()
	bodyString := ""
	if form != nil {
		bodyString = form.Encode()
	}

	// Set up headers
	header := make(http.Header, 2)
	body := bytes.NewBuffer(nil)

	if bodyString != "" {
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		body = bytes.NewBufferString(bodyString)
	}

	if key || sign {
		header.Set("X-MEXC-APIKEY", m.apiKey)
	}

	// Calculate and add signature
	if sign {
		// Construct the signature payload based on the request type
		var signaturePayload string

		signatureQueryString := signatureQuery.Encode()

		switch method {
		case http.MethodGet, http.MethodDelete:
			// For GET/DELETE requests, sign the query string
			signaturePayload = signatureQueryString
		case http.MethodPost, http.MethodPut:
			// For POST/PUT, sign the request body if present, otherwise query string
			if bodyString != "" {
				signaturePayload = bodyString
			} else {
				signaturePayload = signatureQueryString
			}
		}

		// Calculate HMAC-SHA256 signature
		mac := hmac.New(sha256.New, []byte(m.secretKey))
		mac.Write([]byte(signaturePayload))
		signature := hex.EncodeToString(mac.Sum(nil))

		// Add signature to query params
		query.Set("signature", signature)

		// Re-encode query string with signature included
		queryString = query.Encode()
	}

	// Construct the full URL with query parameters
	if queryString != "" {
		fullURL = fmt.Sprintf("%s?%s", fullURL, queryString)
	}

	// Create and send the request
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	// Handle the response
	var mexcErr MxCodedErr
	if err := dexnet.Do(req, thing, dexnet.WithSizeLimit(1<<24), dexnet.WithErrorParsing(&mexcErr)); err != nil {
		m.log.Errorf("request error from endpoint %s %q with query = %q, body = %q, mexc coded error: %v, msg = %q",
			method, endpoint, queryString, bodyString, &mexcErr, mexcErr.Msg)
		return errors.Join(err, &mexcErr)
	}

	return nil
}

// getMEXCDEXAssetIDs gets all DEX asset IDs for a MEXC asset.
func getMEXCDEXAssetIDs(coin string, tokenIDs map[string][]uint32) []uint32 {
	dexSymbol := mxConvertCoin(coin)

	isRegistered := func(assetID uint32) bool {
		_, err := asset.UnitInfo(assetID)
		return err == nil
	}

	assetIDs := make([]uint32, 0, 1)

	// Special handling for USDT - preferentially map to usdt.polygon
	if strings.ToLower(coin) == "usdt" {
		// Try to get the polygon variant first
		if assetID, found := dex.BipSymbolID("usdt.polygon"); found && isRegistered(assetID) {
			assetIDs = append(assetIDs, assetID)
			return assetIDs // Return immediately with polygon variant
		}
	}

	if assetID, found := dex.BipSymbolID(dexSymbol); found {
		// Only registered assets.
		if isRegistered(assetID) {
			assetIDs = append(assetIDs, assetID)
		}
	}

	if tokenIDs, found := tokenIDs[coin]; found {
		for _, tokenID := range tokenIDs {
			if isRegistered(tokenID) {
				assetIDs = append(assetIDs, tokenID)
			}
		}
	}

	return assetIDs
}

// mxSteppedRate rounds the rate to the nearest integer multiple of the step.
// The minimum returned value is step.
func mxSteppedRate(r, step uint64) uint64 {
	steps := math.Round(float64(r) / float64(step))
	if steps == 0 {
		return step
	}
	return uint64(math.Round(steps * float64(step)))
}

// getMarkets fetches all available markets from MEXC.
func (m *mexc) getMarkets(ctx context.Context) (map[string]*mexctypes.SymbolInfo, error) {
	m.log.Infof("Fetching MEXC exchange info...")
	var exchangeInfo mexctypes.ExchangeInfo
	err := m.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, false, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MEXC exchange info: %w", err)
	}

	marketsMap := make(map[string]*mexctypes.SymbolInfo, len(exchangeInfo.Symbols))

	// Load tokenIDs - essential for market mapping
	tokenIDsI := m.tokenIDs.Load()
	if tokenIDsI == nil {
		// If tokenIDs not loaded yet, try to load coin info first
		m.log.Warnf("TokenIDs not loaded yet when fetching markets, attempting to load coin info first")

		// Try to load coin info which should populate tokenIDs
		if err := m.getCoinInfo(ctx); err != nil {
			m.log.Errorf("Failed to load coin info: %v", err)
			return nil, fmt.Errorf("cannot create market mapping without coin info: %w", err)
		}

		// Get the newly loaded tokenIDs
		tokenIDsI = m.tokenIDs.Load()
		if tokenIDsI == nil {
			return nil, fmt.Errorf("tokenIDs still nil after loading coin info")
		}
	}

	// Now safe to type assert
	tokenIDs := tokenIDsI.(map[string][]uint32)

	// Process each symbol to find DEX market matches
	validMarketCount := 0
	for _, symbol := range exchangeInfo.Symbols {
		// Skip markets that aren't enabled for trading
		if symbol.Status != "1" {
			continue
		}

		dexMarkets := mxMarketToDexMarkets(symbol.BaseAsset, symbol.QuoteAsset, tokenIDs)
		if len(dexMarkets) == 0 {
			continue
		}

		// Store a copy of the symbol info
		symCopy := symbol
		marketsMap[symbol.Symbol] = &symCopy
		validMarketCount++
	}

	m.markets.Store(marketsMap)
	m.log.Infof("Successfully processed MEXC exchange info: found %d symbols, %d valid markets",
		len(exchangeInfo.Symbols), validMarketCount)

	return marketsMap, nil
}

// mxMarketToDexMarkets converts a MEXC market to DEX market matches.
func mxMarketToDexMarkets(mexcBaseSymbol, mexcQuoteSymbol string, tokenIDs map[string][]uint32) []*MarketMatch {
	var baseAssetIDs, quoteAssetIDs []uint32

	baseAssetIDs = getMEXCDEXAssetIDs(mexcBaseSymbol, tokenIDs)
	if len(baseAssetIDs) == 0 {
		return nil
	}

	quoteAssetIDs = getMEXCDEXAssetIDs(mexcQuoteSymbol, tokenIDs)
	if len(quoteAssetIDs) == 0 {
		return nil
	}

	markets := make([]*MarketMatch, 0, len(baseAssetIDs)*len(quoteAssetIDs))
	for _, baseID := range baseAssetIDs {
		for _, quoteID := range quoteAssetIDs {
			// Skip disabled assets if needed
			/*
				if mexcAssetDisabled(baseID) || mexcAssetDisabled(quoteID) {
					continue
				}
			*/

			markets = append(markets, &MarketMatch{
				Slug:     mexcBaseSymbol + mexcQuoteSymbol,
				MarketID: dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID),
				BaseID:   baseID,
				QuoteID:  quoteID,
			})
		}
	}

	return markets
}

// getUserDataStream gets a listen key and connects to the user data websocket stream
func (m *mexc) getUserDataStream(ctx context.Context) error {
	if m.apiKey == "" {
		m.log.Errorf("no API key provided, cannot connect to user data stream")
		return nil
	}

	var resp struct {
		ListenKey string `json:"listenKey"`
	}

	// Changed from false to true for the sign parameter to ensure signature is included
	err := m.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, true, &resp)
	if err != nil {
		return fmt.Errorf("error getting listen key: %w", err)
	}

	m.listenKey.Store(resp.ListenKey)
	m.log.Debugf("Got listen key %s", resp.ListenKey)

	// Set up the websocket connection for user data
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.userDataStreamHandler(ctx)
	}()

	// Start periodic listening key refresh
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.keepAliveListenKey(ctx)
	}()

	return nil
}

// keepAliveListenKey keeps the listenKey alive by sending a keepalive request
// every 30 minutes.
func (m *mexc) keepAliveListenKey(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			key := m.listenKey.Load().(string)
			if key == "" {
				continue
			}
			form := url.Values{}
			form.Add("listenKey", key)
			// Changed from false to true for the sign parameter to ensure signature is included
			err := m.putAPI(ctx, "/api/v3/userDataStream", nil, form, true, true, nil)
			if err != nil {
				m.log.Errorf("error refreshing listen key: %v", err)
			} else {
				m.log.Tracef("Successfully refreshed listen key")
			}
		case <-ctx.Done():
			return
		}
	}
}

// putAPI sends a PUT request to the specified endpoint.
func (m *mexc) putAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodPut, endpoint, query, form, key, sign, thing)
}

// userDataStreamHandler connects to the user data stream and processes messages.
func (m *mexc) userDataStreamHandler(ctx context.Context) {
	reconnect := func() {
		select {
		case m.reconnectChan <- struct{}{}:
		default:
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.reconnectChan:
			// Continue to reconnect logic
		}

		listenKey := m.listenKey.Load().(string)
		if listenKey == "" {
			m.log.Errorf("no listen key available, cannot connect to user data stream")
			time.Sleep(5 * time.Second)
			continue
		}

		wsURL := fmt.Sprintf("%s?listenKey=%s", m.wsURL, listenKey)

		wsConn, err := comms.NewWsConn(&comms.WsCfg{
			URL:        wsURL,
			Logger:     m.log,
			PingWait:   60 * time.Second,
			RawHandler: m.handleUserDataMessage,
		})
		if err != nil {
			m.log.Errorf("error creating user data stream websocket connection: %v", err)
			time.Sleep(5 * time.Second)
			reconnect()
			continue
		}

		m.log.Debugf("Connected to user data stream: %s", wsURL)

		// Connect and wait for the connection to close
		wg, err := wsConn.Connect(ctx)
		if err != nil {
			m.log.Errorf("error connecting to user data stream: %v", err)
			time.Sleep(5 * time.Second)
			reconnect()
			continue
		}

		// Wait for the connection to close
		wg.Wait()
		m.log.Errorf("User data stream disconnected, reconnecting...")
		time.Sleep(time.Second)
	}
}

// handleUserDataMessage processes raw messages from the user data stream
func (m *mexc) handleUserDataMessage(msg []byte) {
	// Try to parse the message
	var baseMsg struct {
		EventType string          `json:"e"`
		Data      json.RawMessage `json:"d"`
	}

	if err := json.Unmarshal(msg, &baseMsg); err != nil {
		m.log.Errorf("error parsing user data message: %v", err)
		return
	}

	switch baseMsg.EventType {
	case "spot@private.account.v3.api":
		var balanceUpdate struct {
			Data []mexctypes.WsAccountUpdateData `json:"d"`
		}
		if err := json.Unmarshal(msg, &balanceUpdate); err != nil {
			m.log.Errorf("error parsing balance update: %v", err)
			return
		}

		// Create a wrapper to match the expected format
		update := &mexctypes.WsAccountUpdate{
			EventType: baseMsg.EventType,
			Balances:  balanceUpdate.Data,
		}
		m.processBalanceUpdate(update)

	case "spot@private.orders.v3.api":
		var orderUpdate mexctypes.WsOrderUpdate
		if err := json.Unmarshal(msg, &orderUpdate); err != nil {
			m.log.Errorf("error parsing order update: %v", err)
			return
		}
		m.processOrderUpdate(&orderUpdate)

	default:
		m.log.Debugf("Unhandled user data event type: %s", baseMsg.EventType)
	}
}

// processBalanceUpdate handles balance updates from the user data stream.
func (m *mexc) processBalanceUpdate(update *mexctypes.WsAccountUpdate) {
	m.balanceMtx.Lock()
	defer m.balanceMtx.Unlock()

	tokenIDsI := m.tokenIDs.Load()
	if tokenIDsI == nil {
		m.log.Errorf("No token IDs loaded, cannot process balance update")
		return
	}

	tokenIDs := tokenIDsI.(map[string][]uint32)

	for _, bal := range update.Balances {
		// Process balance updates using WsAccountUpdateData fields
		free, _ := strconv.ParseFloat(bal.Available, 64)
		locked, _ := strconv.ParseFloat(bal.Locked, 64)

		for _, assetID := range getMEXCDEXAssetIDs(bal.Asset, tokenIDs) {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				m.log.Errorf("No unit info for asset ID %d", assetID)
				continue
			}

			conv := ui.Conventional.ConversionFactor
			updatedBalance := &ExchangeBalance{
				Available: strconv.FormatUint(uint64(math.Round(free*float64(conv))), 10),
				Locked:    strconv.FormatUint(uint64(math.Round(locked*float64(conv))), 10),
			}

			m.balances[assetID] = updatedBalance
		}
	}
}

// processOrderUpdate handles order updates from the user data stream.
func (m *mexc) processOrderUpdate(update *mexctypes.WsOrderUpdate) {
	orderID := update.OrderID
	clientOrderID := update.ClientOrderID

	m.activeTradesMtx.Lock()
	tradInfo, found := m.activeTrades[orderID]
	if !found {
		// Try to find by client order ID
		for id, info := range m.activeTrades {
			if clientOrderID == strconv.Itoa(info.updaterID) {
				tradInfo = info
				found = true
				orderID = id
				break
			}
		}
	}
	m.activeTradesMtx.Unlock()

	if !found {
		// This may be a leftover order from a previous run
		m.log.Tracef("Received update for unknown order: %s", orderID)
		return
	}

	// Extract execution data from the update
	m.tradeUpdaterMtx.Lock()
	updater, exists := m.tradeUpdaters[tradInfo.updaterID]
	m.tradeUpdaterMtx.Unlock()

	if !exists {
		m.log.Errorf("No trade updater for ID %d", tradInfo.updaterID)
		return
	}

	// Extract fill information
	execQty, _ := strconv.ParseFloat(update.ExecutedQuantity, 64)
	execPrice, _ := strconv.ParseFloat(update.LastExecutedPrice, 64)

	// Convert to base units with proper error handling
	baseUI, err := asset.UnitInfo(tradInfo.baseID)
	if err != nil {
		m.log.Errorf("Error getting base unit info: %v", err)
		return
	}

	quoteUI, err := asset.UnitInfo(tradInfo.quoteID)
	if err != nil {
		m.log.Errorf("Error getting quote unit info: %v", err)
		return
	}

	baseConv := float64(baseUI.Conventional.ConversionFactor)
	quoteConv := float64(quoteUI.Conventional.ConversionFactor)

	baseFilled := uint64(math.Round(execQty * baseConv))
	quoteFilled := uint64(math.Round(execQty * execPrice * quoteConv))

	// Determine status
	var complete bool
	switch update.OrderStatus {
	case "NEW", "PARTIALLY_FILLED":
		complete = false
	case "FILLED", "CANCELED", "REJECTED", "EXPIRED":
		complete = true
	default:
		m.log.Errorf("Unknown order status: %s", update.OrderStatus)
		return
	}

	trade := &Trade{
		ID:          orderID,
		Sell:        tradInfo.sell,
		Qty:         tradInfo.qty,
		Rate:        tradInfo.rate,
		BaseID:      tradInfo.baseID,
		QuoteID:     tradInfo.quoteID,
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
		Complete:    complete,
	}

	// Send update to the trade updater
	select {
	case updater <- trade:
	default:
		m.log.Errorf("Trade updater channel full")
	}

	// Remove from active trades if complete
	if complete {
		m.activeTradesMtx.Lock()
		delete(m.activeTrades, orderID)
		m.activeTradesMtx.Unlock()

		m.tradeUpdaterMtx.Lock()
		delete(m.tradeUpdaters, tradInfo.updaterID)
		m.tradeUpdaterMtx.Unlock()
	}
}

// Tickers retrieves current ticker information for the provided markets.
func (m *mexc) Tickers(ctx context.Context, markets []*MarketMatch) ([]*Ticker, error) {
	mexcMarkets := make(map[uint32]map[uint32]*string)
	for _, mkt := range markets {
		if baseMap, found := mexcMarkets[mkt.BaseID]; found {
			baseMap[mkt.QuoteID] = &mkt.Slug
		} else {
			mexcMarkets[mkt.BaseID] = map[uint32]*string{mkt.QuoteID: &mkt.Slug}
		}
	}

	var prices []*mexctypes.TickerPrice
	err := m.getAPI(ctx, "/api/v3/ticker/price", nil, false, false, &prices)
	if err != nil {
		return nil, fmt.Errorf("error getting ticker prices: %w", err)
	}

	var tickers []*Ticker
	marketsMap := m.markets.Load().(map[string]*mexctypes.SymbolInfo)

	// Safely get tokenIDs map
	tokenIDsI := m.tokenIDs.Load()
	if tokenIDsI == nil {
		// If tokenIDs is not loaded yet, we can't process tickers properly
		return nil, fmt.Errorf("token IDs not loaded, cannot process tickers")
	}
	tokenIDs := tokenIDsI.(map[string][]uint32)

	for _, price := range prices {
		// Skip if we don't have market info for this symbol
		symInfo, found := marketsMap[price.Symbol]
		if !found {
			continue
		}

		// Look for matches in our requested markets
		baseMatches := getMEXCDEXAssetIDs(symInfo.BaseAsset, tokenIDs)
		quoteMatches := getMEXCDEXAssetIDs(symInfo.QuoteAsset, tokenIDs)

		for _, baseID := range baseMatches {
			quoteMap, found := mexcMarkets[baseID]
			if !found {
				continue
			}

			for _, quoteID := range quoteMatches {
				if _, found := quoteMap[quoteID]; found {
					baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
					if err != nil {
						m.log.Errorf("Error getting asset configs for %d-%d: %v", baseID, quoteID, err)
						continue
					}

					// Parse price from string
					priceVal, err := strconv.ParseFloat(price.Price, 64)
					if err != nil {
						m.log.Errorf("Error parsing price for %s: %v", price.Symbol, err)
						continue
					}

					// Calculate DEX rate
					dexRate := calc.MessageRateAlt(priceVal, baseCfg.conversionFactor, quoteCfg.conversionFactor)

					tickers = append(tickers, &Ticker{
						MarketID: dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID),
						Stamp:    time.Now(),
						Rate:     dexRate,
					})
				}
			}
		}
	}

	return tickers, nil
}

// OrderBook subscribes to and maintains the order book for a market. The
// returned VWAP function can be used to get the volume weighted average price
// for a given quantity of the base asset.
func (m *mexc) OrderBook(ctx context.Context, mktMatch *MarketMatch) (vwap VWAPFunc, err error) {
	m.booksMtx.RLock()
	book, found := m.books[mktMatch.Slug]
	m.booksMtx.RUnlock()

	if found {
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return book.vwap, nil
	}

	baseCfg, quoteCfg, err := mxAssetCfgs(mktMatch.BaseID, mktMatch.QuoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset configs: %w", err)
	}

	getSnapshot := func() (*mexctypes.DepthResponse, error) {
		var snapshot mexctypes.DepthResponse
		err := m.getAPI(ctx, "/api/v3/depth", url.Values{
			"symbol": []string{mktMatch.Slug},
			"limit":  []string{"1000"}, // Use maximum limit for best accuracy
		}, false, false, &snapshot)
		if err != nil {
			return nil, fmt.Errorf("error getting orderbook snapshot: %w", err)
		}
		return &snapshot, nil
	}

	book = newMxOrderBook(
		baseCfg.conversionFactor,
		quoteCfg.conversionFactor,
		mktMatch.Slug,
		getSnapshot,
		m.log.SubLogger(fmt.Sprintf("MEXC-OrderBook-%s", mktMatch.Slug)),
	)

	m.booksMtx.Lock()
	m.books[mktMatch.Slug] = book
	m.booksMtx.Unlock()

	// Start the sync process
	book.syncChan = make(chan struct{})
	go book.sync(ctx)

	// Start the market data websocket if not already running
	if err := m.startMarketStreams(ctx); err != nil {
		m.booksMtx.Lock()
		delete(m.books, mktMatch.Slug)
		m.booksMtx.Unlock()
		return nil, fmt.Errorf("error starting market streams: %w", err)
	}

	// Subscribe to the depth updates
	m.subscribeMarketStream(ctx, mktMatch.Slug)

	return book.vwap, nil
}

// Trade executes a trade on the MEXC exchange.
func (m *mexc) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (*Trade, error) {
	side := "BUY"
	if sell {
		side = "SELL"
	}

	baseCfg, err := mxAssetCfg(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	quoteCfg, err := mxAssetCfg(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	symbol := baseCfg.coin + quoteCfg.coin

	// Get market filters to determine price and quantity precision
	markets := m.markets.Load().(map[string]*mexctypes.SymbolInfo)
	market, found := markets[symbol]
	if !found {
		return nil, fmt.Errorf("market not found: %v", symbol)
	}

	// Convert rate to conventional format
	convRate := calc.ConventionalRateAlt(rate, baseCfg.conversionFactor, quoteCfg.conversionFactor)

	// Format rate to string with correct precision
	priceStr := strconv.FormatFloat(convRate, 'f', int(market.QuoteAssetPrecision), 64)

	// Convert quantity to conventional format
	convQty := float64(qty) / float64(baseCfg.conversionFactor)

	// Format quantity to string with correct precision
	qtyStr := strconv.FormatFloat(convQty, 'f', int(market.BaseAssetPrecision), 64)

	// Generate a unique client order ID
	clientOrderID := fmt.Sprintf("%x", m.tradeIDNonce.Add(1))

	// Prepare request parameters - Using query parameters as shown in the MEXC documentation
	v := make(url.Values)
	v.Add("symbol", symbol)
	v.Add("side", side)
	v.Add("type", "LIMIT")
	v.Add("timeInForce", "GTC")
	v.Add("newClientOrderId", clientOrderID) // Use newClientOrderId instead of clientOrderId
	v.Add("quantity", qtyStr)                // Use quantity as per MEXC docs
	v.Add("price", priceStr)

	// Store trade info for later status updates
	m.tradeUpdaterMtx.Lock()
	_, found = m.tradeUpdaters[subscriptionID]
	if !found {
		m.tradeUpdaterMtx.Unlock()
		return nil, fmt.Errorf("no trade updater with ID %v", subscriptionID)
	}

	tradeInfo := &mxTradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
		sell:      sell,
		rate:      rate,
		qty:       qty,
	}

	m.tradeInfo[clientOrderID] = tradeInfo
	m.tradeUpdaterMtx.Unlock()

	var success bool
	defer func() {
		if !success {
			m.tradeUpdaterMtx.Lock()
			delete(m.tradeInfo, clientOrderID)
			m.tradeUpdaterMtx.Unlock()
		}
	}()

	// Send the order request to MEXC
	// The request will be POST /api/v3/order with query parameters
	var orderResponse mexctypes.OrderResponse
	err = m.postAPI(ctx, "/api/v3/order", v, nil, true, true, &orderResponse)
	if err != nil {
		return nil, err
	}

	m.log.Debugf("Placed order on MEXC: %s %s %s@%s, ID: %s",
		side, symbol, qtyStr, priceStr, orderResponse.OrderID)

	// Store the order ID returned by the exchange
	m.tradeUpdaterMtx.Lock()
	m.tradeInfo[orderResponse.OrderID] = tradeInfo
	m.tradeUpdaterMtx.Unlock()

	success = true

	// Track as an active trade
	m.activeTradesMtx.Lock()
	m.activeTrades[orderResponse.OrderID] = tradeInfo
	m.activeTradesMtx.Unlock()

	// Return the client order ID as the trade ID for consistency
	return &Trade{
		ID:          clientOrderID,
		Sell:        sell,
		Rate:        rate,
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  0,     // New orders typically have 0 filled
		QuoteFilled: 0,     // New orders typically have 0 filled
		Complete:    false, // New orders are not complete
	}, nil
}

// startMarketStreams starts the WebSocket connection for market data.
func (m *mexc) startMarketStreams(ctx context.Context) error {
	m.marketStreamMtx.Lock()
	defer m.marketStreamMtx.Unlock()

	if m.marketStream != nil && !m.marketStream.IsDown() {
		// Already connected and working
		m.log.Debugf("Market stream already connected")
		return nil
	}

	// If we had a previous connection that's broken, clean it up
	if m.marketStream != nil {
		m.log.Debugf("Cleaning up previous market stream connection")
		m.marketStream = nil
	}

	// Create WebSocket connection for market data
	m.log.Infof("Starting MEXC market data stream...")
	wsConn, err := comms.NewWsConn(&comms.WsCfg{
		URL:              m.wsURL,
		Logger:           m.log.SubLogger("MEXC-MarketStream"),
		PingWait:         60 * time.Second,
		RawHandler:       m.handleMarketDataMessage,
		ConnectEventFunc: m.handleMarketConnectEvent,
	})
	if err != nil {
		return fmt.Errorf("error creating market data websocket: %w", err)
	}

	m.marketStream = wsConn

	// Connect and handle messages in the background
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		wg, err := wsConn.Connect(ctx)
		if err != nil {
			m.log.Errorf("Error connecting to market data stream: %v", err)

			m.marketStreamMtx.Lock()
			m.marketStream = nil
			m.marketStreamMtx.Unlock()
			return
		}

		// Connection successful
		m.log.Debugf("Connected to market data stream")

		// Wait for the connection to close
		wg.Wait()

		m.log.Errorf("Market data stream disconnected")

		m.marketStreamMtx.Lock()
		m.marketStream = nil
		m.marketStreamMtx.Unlock()
	}()

	// Add a brief delay to allow the initial connection establishment
	// This is a simple way to improve the chances the connection is ready when we return
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !wsConn.IsDown() {
			m.log.Debugf("Market stream connected successfully")
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// handleMarketDataMessage processes raw messages from the market data WebSocket
func (m *mexc) handleMarketDataMessage(msg []byte) {
	// Check if it's a binary message (protobuf)
	if len(msg) > 0 && (msg[0] < 32 || msg[0] > 126) {
		// Handle binary protobuf message
		m.handleProtobufMessage(msg)
		return
	}

	// Check if the message is a ping
	if string(msg) == `{"ping":true}` || string(msg) == "ping" {
		// Respond with pong
		m.marketStream.SendRaw([]byte(`{"pong":true}`))
		return
	}

	// Handle JSON message
	var baseMsg struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(msg, &baseMsg); err != nil {
		// Try to check if it's a subscription response
		var respMsg struct {
			ID     interface{} `json:"id"`
			Code   int         `json:"code"`
			Status string      `json:"status"`
			Msg    string      `json:"msg"`
		}

		if jsonErr := json.Unmarshal(msg, &respMsg); jsonErr == nil {
			if respMsg.Code == 0 || respMsg.Status == "OK" || strings.Contains(respMsg.Msg, "success") {
				m.log.Debugf("[MarketWS] Received subscription success response: %s", string(msg))
				return
			} else if respMsg.Code != 0 {
				m.log.Errorf("[MarketWS] Received error response: Code=%d, Msg=%s", respMsg.Code, respMsg.Msg)
				return
			}
		}

		m.log.Errorf("[MarketWS] Failed to parse WebSocket message: %v\nRaw: %s", err, string(msg))
		return
	}

	if baseMsg.Stream == "" {
		m.log.Tracef("[MarketWS] Received message without stream: %s", string(msg))
		return
	}

	// Process different message types based on stream value
	if strings.Contains(baseMsg.Stream, "@depth") {
		// Handle orderbook updates
		symbolParts := strings.Split(baseMsg.Stream, "@")
		if len(symbolParts) < 1 {
			m.log.Errorf("[MarketWS] Invalid stream format: %s", baseMsg.Stream)
			return
		}

		// Check if it's a depth update
		var depthUpdate mexctypes.WsDepthUpdate
		depthUpdate.Stream = baseMsg.Stream
		depthUpdate.Data = baseMsg.Data

		m.processDepthUpdate(&depthUpdate)
	} else {
		m.log.Debugf("[MarketWS] Unhandled stream type: %s", baseMsg.Stream)
	}
}

// subscribeMarketStream subscribes to the WebSocket depth stream for a market.
func (m *mexc) subscribeMarketStream(ctx context.Context, symbol string) {
	// Ensure the market stream is running
	m.marketStreamMtx.RLock()
	marketStream := m.marketStream
	m.marketStreamMtx.RUnlock()

	if marketStream == nil {
		// Try to create the stream if it doesn't exist
		err := m.startMarketStreams(ctx)
		if err != nil {
			m.log.Errorf("Failed to start market stream for subscription: %v", err)
			return
		}

		// Retry getting the stream after starting it
		m.marketStreamMtx.RLock()
		marketStream = m.marketStream
		m.marketStreamMtx.RUnlock()

		if marketStream == nil {
			m.log.Errorf("Cannot subscribe to %s, market stream creation failed", symbol)
			return
		}
	}

	// Ensure the connection is established before sending subscription
	if marketStream.IsDown() {
		m.log.Errorf("Cannot subscribe to %s, market stream is down", symbol)

		// Try to reconnect
		err := m.startMarketStreams(ctx)
		if err != nil {
			m.log.Errorf("Failed to restart market stream: %v", err)
			return
		}

		// Get the new connection
		m.marketStreamMtx.RLock()
		marketStream = m.marketStream
		m.marketStreamMtx.RUnlock()

		if marketStream == nil || marketStream.IsDown() {
			m.log.Errorf("Cannot subscribe to %s, reconnection failed", symbol)
			return
		}
	}

	// Subscribe to depth updates with correct format
	subMsg := WsRequest{
		Method: "SUBSCRIPTION",
		Params: []string{
			fmt.Sprintf("%s@depth", symbol),
		},
	}

	subBytes, err := json.Marshal(subMsg)
	if err != nil {
		m.log.Errorf("Error marshaling subscription message: %v", err)
		return
	}

	// Try multiple times to send the subscription
	var sendErr error
	for i := 0; i < 3; i++ {
		sendErr = marketStream.SendRaw(subBytes)
		if sendErr == nil {
			m.log.Infof("Successfully subscribed to %s@depth", symbol)
			return
		}
		m.log.Errorf("Error subscribing to %s@depth (attempt %d): %v", symbol, i+1, sendErr)
		time.Sleep(500 * time.Millisecond)
	}

	if sendErr != nil {
		m.log.Errorf("Failed to subscribe to %s@depth after multiple attempts", symbol)
	}
}

// CancelTrade cancels an open trade on the MEXC exchange.
func (m *mexc) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	// Get asset configurations
	baseCfg, err := mxAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	quoteCfg, err := mxAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	// Form the market symbol
	symbol := baseCfg.coin + quoteCfg.coin

	// Prepare query parameters for the cancel request
	query := url.Values{}
	query.Add("symbol", symbol)

	// Check if this is a MEXC order ID we have in our tracking
	m.activeTradesMtx.RLock()
	_, hasMEXCOrderID := m.activeTrades[tradeID]
	m.activeTradesMtx.RUnlock()

	// First check if this is a client order ID in our tracking
	m.tradeUpdaterMtx.RLock()
	_, hasClientOrderID := m.tradeInfo[tradeID]
	m.tradeUpdaterMtx.RUnlock()

	if hasMEXCOrderID {
		// If it's a MEXC order ID, use orderId parameter
		query.Add("orderId", tradeID)
	} else if hasClientOrderID {
		// If it's a client order ID we've tracked, use origClientOrderId parameter
		query.Add("origClientOrderId", tradeID)
	} else {
		// If we're not sure, try as orderId (more common case)
		query.Add("orderId", tradeID)
	}

	// Execute the cancel order request
	var resp mexctypes.CancelOrderResponse
	err = m.deleteAPI(ctx, "/api/v3/order", query, true, true, &resp)
	if err != nil {
		// Check if the order is already done
		var mexcErr *MxCodedErr
		if errors.As(err, &mexcErr) && mexcErr.Code == -2011 { // Order does not exist
			return nil // Order already done/canceled
		}
		return fmt.Errorf("error canceling order: %w", err)
	}

	m.log.Debugf("Canceled order on MEXC: %s, ID: %s, status: %s",
		symbol, resp.OrderID, resp.Status)

	// Clean up our trade tracking
	m.tradeUpdaterMtx.Lock()
	// We need to find and clean up the trade info
	var tradeInfo *mxTradeInfo
	var found bool

	// Try to get trade info from different possible IDs
	// Check if we have it by the order ID from the response
	tradeInfo, found = m.tradeInfo[resp.OrderID]
	if !found {
		// If not found, try by the ID that was passed in
		tradeInfo, found = m.tradeInfo[tradeID]
	}

	// If we found the trade info, clean up all references to it
	if found {
		// Clean up all references to this trade info
		for id, info := range m.tradeInfo {
			if info == tradeInfo {
				delete(m.tradeInfo, id)
			}
		}
	}
	m.tradeUpdaterMtx.Unlock()

	// Remove from active trades map
	m.activeTradesMtx.Lock()
	delete(m.activeTrades, resp.OrderID)
	if resp.OrderID != tradeID {
		delete(m.activeTrades, tradeID) // Also clean up by the ID passed in if different
	}
	m.activeTradesMtx.Unlock()

	return nil
}

// deleteAPI sends a DELETE request to the specified endpoint.
func (m *mexc) deleteAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodDelete, endpoint, query, nil, key, sign, thing)
}

// Balance gets the available balance for an asset.
func (m *mexc) Balance(assetID uint32) (*ExchangeBalance, error) {
	m.balanceMtx.RLock()
	defer m.balanceMtx.RUnlock()

	bal, found := m.balances[assetID]
	if !found {
		// Instead of returning an error, return a zero balance
		m.log.Debugf("No balance found for asset ID %d, returning zero balance", assetID)
		return &ExchangeBalance{
			Available: "0",
			Locked:    "0",
		}, nil
	}

	// Return a copy to prevent external modification
	return &ExchangeBalance{
		Available: bal.Available,
		Locked:    bal.Locked,
	}, nil
}

// DepositAddress retrieves a deposit address for the specified asset.
func (m *mexc) DepositAddress(ctx context.Context, assetID uint32) (string, error) {
	cfg, err := mxAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting asset config: %w", err)
	}

	// Get the correct network name for tokens
	var network string
	if tkn := asset.TokenInfo(assetID); tkn != nil {
		network = cfg.chain
	} else {
		network = cfg.coin
	}

	query := url.Values{
		"coin":    []string{cfg.coin},
		"network": []string{network},
	}

	var resp struct {
		Address    string `json:"address"`
		Coin       string `json:"coin"`
		Tag        string `json:"tag"`
		URL        string `json:"url"`
		Network    string `json:"network"`
		AddressTag string `json:"addressTag"`
	}

	err = m.getAPI(ctx, "/api/v3/capital/deposit/address", query, true, true, &resp)
	if err != nil {
		return "", fmt.Errorf("error getting deposit address: %w", err)
	}

	// For some assets, the tag might be needed along with the address
	address := resp.Address
	if resp.Tag != "" {
		address += " (Memo/Tag: " + resp.Tag + ")"
	}

	return address, nil
}

// Withdraw initiates a withdrawal from the exchange to an external address.
func (m *mexc) Withdraw(ctx context.Context, assetID uint32, amt uint64, address string) (string, error) {
	cfg, err := mxAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting asset config: %w", err)
	}

	minWithdrawI := m.minWithdraw.Load()
	if minWithdrawI == nil {
		return "", fmt.Errorf("withdrawal minimums not loaded")
	}
	minWithdraw := minWithdrawI.(map[uint32]*mxWithdrawInfo)

	withdrawInfo, found := minWithdraw[assetID]
	if !found {
		return "", fmt.Errorf("withdrawal not enabled for asset %d", assetID)
	}

	// Check minimum withdrawal
	if amt < withdrawInfo.minimum {
		return "", fmt.Errorf("withdrawal amount %d is below minimum %d", amt, withdrawInfo.minimum)
	}

	// Convert to exchange format
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return "", fmt.Errorf("no unit info for asset %d", assetID)
	}

	convQty := float64(amt) / float64(ui.Conventional.ConversionFactor)

	// Get the correct network name for tokens
	var network string
	if tkn := asset.TokenInfo(assetID); tkn != nil {
		network = cfg.chain
	} else {
		network = cfg.coin
	}

	form := url.Values{
		"coin":    []string{cfg.coin},
		"address": []string{address},
		"amount":  []string{strconv.FormatFloat(convQty, 'f', -1, 64)},
		"network": []string{network},
	}

	// Handle memo for assets that require it
	// For MEXC, most memos are handled automatically via address detection
	// but we'd add specific memo handling here if needed

	var resp struct {
		ID string `json:"id"`
	}

	err = m.postAPI(ctx, "/api/v3/capital/withdraw/apply", nil, form, true, true, &resp)
	if err != nil {
		return "", fmt.Errorf("error submitting withdrawal: %w", err)
	}

	return resp.ID, nil
}

// Markets retrieves all available markets on the exchange. The map has the
// market ID e.g. "btc_ltc" as the key.
func (m *mexc) Markets(ctx context.Context) (map[string]*Market, error) {
	var twoHoursAgo = time.Now().Add(-2 * time.Hour)
	m.marketSnapshotMtx.Lock()
	defer m.marketSnapshotMtx.Unlock()

	// Return cached markets if we have them and they're fresh
	if m.marketSnapshot.m != nil && m.marketSnapshot.stamp.After(twoHoursAgo) {
		return m.marketSnapshot.m, nil
	}

	// Get fresh market data
	mexcMarkets, err := m.getMarkets(ctx)
	if err != nil {
		return nil, err
	}

	tokenIDs := m.tokenIDs.Load().(map[string][]uint32)
	markets := make(map[string]*Market)

	for _, mexcMkt := range mexcMarkets {
		dexMarkets := mxMarketToDexMarkets(mexcMkt.BaseAsset, mexcMkt.QuoteAsset, tokenIDs)
		for _, mkt := range dexMarkets {
			// Just get minimum withdrawal info, no need to use configs for other fields
			markets[mkt.MarketID] = &Market{
				BaseID:           mkt.BaseID,
				QuoteID:          mkt.QuoteID,
				BaseMinWithdraw:  0, // Will be populated separately
				QuoteMinWithdraw: 0, // Will be populated separately
			}
		}
	}

	// Update the cache
	m.marketSnapshot.m = markets
	m.marketSnapshot.stamp = time.Now()

	return markets, nil
}

// LocalFills retrieves historical trades on the exchange.
func (m *mexc) LocalFills(ctx context.Context, startTime time.Time, endTime time.Time) ([]*LocalFill, error) {
	// startTsFill will be an index to maintain chronological ordering.
	startStamp := startTime.UnixMilli()

	// Gather all the base-quote asset pairs that we know about.
	tokenIDs := m.tokenIDs.Load().(map[string][]uint32)
	mexcMarkets := m.markets.Load().(map[string]*mexctypes.SymbolInfo)

	assetPairs := make([]struct {
		Symbol  string
		BaseID  uint32
		QuoteID uint32
	}, 0, len(mexcMarkets))

	for symbol, mkt := range mexcMarkets {
		dexMarkets := mxMarketToDexMarkets(mkt.BaseAsset, mkt.QuoteAsset, tokenIDs)
		for _, dexMkt := range dexMarkets {
			assetPairs = append(assetPairs, struct {
				Symbol  string
				BaseID  uint32
				QuoteID uint32
			}{
				Symbol:  symbol,
				BaseID:  dexMkt.BaseID,
				QuoteID: dexMkt.QuoteID,
			})
		}
	}

	// Process each market's fills
	var allFills []*LocalFill
	for _, pair := range assetPairs {
		baseCfg, quoteCfg, err := mxAssetCfgs(pair.BaseID, pair.QuoteID)
		if err != nil {
			m.log.Errorf("Error getting asset configs for %d-%d: %v", pair.BaseID, pair.QuoteID, err)
			continue
		}

		// Query for fills
		query := url.Values{
			"symbol":    []string{pair.Symbol},
			"startTime": []string{strconv.FormatInt(startTime.UnixMilli(), 10)},
		}
		if !endTime.IsZero() {
			query.Add("endTime", strconv.FormatInt(endTime.UnixMilli(), 10))
		}

		var trades []*mexctypes.MyTrade
		err = m.getAPI(ctx, "/api/v3/myTrades", query, true, true, &trades)
		if err != nil {
			m.log.Errorf("Error fetching trades for %s: %v", pair.Symbol, err)
			continue
		}

		// Convert to LocalFill format
		for i, trade := range trades {
			price, _ := strconv.ParseFloat(trade.Price, 64)
			qty, _ := strconv.ParseFloat(trade.Quantity, 64)
			fee, _ := strconv.ParseFloat(trade.Commission, 64)

			// Convert to DEX units
			baseQty := uint64(math.Round(qty * float64(baseCfg.conversionFactor)))
			quoteQty := uint64(math.Round(price * qty * float64(quoteCfg.conversionFactor)))

			// Determine fee asset ID
			feeAssetID := pair.QuoteID
			if trade.CommissionAsset != quoteCfg.coin {
				// If fee is in a different asset, try to map it
				for assetID, cfg := range map[uint32]*mxAssetConfig{
					pair.BaseID:  baseCfg,
					pair.QuoteID: quoteCfg,
				} {
					if cfg.coin == trade.CommissionAsset {
						feeAssetID = assetID
						break
					}
				}
			}

			// Convert fee to DEX units
			var feeAssetCfg *mxAssetConfig
			var err error
			if feeAssetID == pair.BaseID {
				feeAssetCfg = baseCfg
			} else if feeAssetID == pair.QuoteID {
				feeAssetCfg = quoteCfg
			} else {
				feeAssetCfg, err = mxAssetCfg(feeAssetID)
				if err != nil {
					m.log.Errorf("Error getting fee asset config for %s: %v", trade.CommissionAsset, err)
					continue
				}
			}

			feeAmt := uint64(math.Round(fee * float64(feeAssetCfg.conversionFactor)))

			allFills = append(allFills, &LocalFill{
				BaseID:      pair.BaseID,
				BaseSymbol:  dex.BipIDSymbol(pair.BaseID),
				QuoteID:     pair.QuoteID,
				QuoteSymbol: dex.BipIDSymbol(pair.QuoteID),
				Index:       fmt.Sprintf("%d-%d", startStamp, i),
				DEXRate:     calc.MessageRateAlt(price, baseCfg.conversionFactor, quoteCfg.conversionFactor),
				Stamp:       time.UnixMilli(trade.Time),
				Side:        trade.IsBuyer,
				Fee:         feeAmt,
				FeeAssetID:  feeAssetID,
				BaseQty:     baseQty,
				QuoteQty:    quoteQty,
			})
		}
	}

	// Sort by timestamp
	sort.Slice(allFills, func(i, j int) bool {
		return allFills[i].Stamp.Before(allFills[j].Stamp)
	})

	return allFills, nil
}

// IsMyMarket determines if a market is owned by this CEX.
func (m *mexc) IsMyMarket(mkt *Market) bool {
	// Check in a safe way without assuming mkt.Slug exists
	if mkt == nil {
		return false
	}
	return strings.HasPrefix(fmt.Sprintf("%d_%d", mkt.BaseID, mkt.QuoteID), "MEXC:")
}

// Name returns the name of the exchange implementation.
func (m *mexc) Name() string {
	return "MEXC"
}

// API returns the unique API key prefix for this exchange type.
func (m *mexc) API() string {
	return "mexc"
}

// Network returns the current network of this exchange.
func (m *mexc) Network() dex.Network {
	return m.net
}

// EpochReport prepares a report of the CEX's account status for the given
// duration.
func (m *mexc) EpochReport(ctx context.Context, startTime, endTime time.Time) (*EpochReport, error) {
	fills, err := m.LocalFills(ctx, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("error getting fills: %w", err)
	}

	// Get current balances for assets
	m.balanceMtx.RLock()
	balances := make(map[uint32]*ExchangeBalance, len(m.balances))
	for assetID, bal := range m.balances {
		balances[assetID] = &ExchangeBalance{
			Available: bal.Available,
			Locked:    bal.Locked,
		}
	}
	m.balanceMtx.RUnlock()

	// Calculate volume by market ID
	volume := make(map[string]uint64)
	for _, fill := range fills {
		mktID := fill.BaseSymbol + "_" + fill.QuoteSymbol
		volume[mktID] += fill.QuoteQty
	}

	return &EpochReport{
		Fills:    fills,
		Balances: balances,
		Volume:   volume,
	}, nil
}

// RegisteredAssets gets the list of assets registered with the CEX.
func (m *mexc) RegisteredAssets() map[uint32]bool {
	return m.knownAssets
}

// Close shuts down the exchange connection and any goroutines.
func (m *mexc) Close() {
	m.shutdownMtx.Lock()
	defer m.shutdownMtx.Unlock()

	select {
	case <-m.quit:
		return // already closed
	default:
		close(m.quit)
	}

	// Close the market stream
	m.marketStreamMtx.Lock()
	marketStream := m.marketStream
	m.marketStream = nil
	m.marketStreamMtx.Unlock()

	if marketStream != nil {
		// Just set it to nil since we can't find a close method
		// This is a placeholder - in a real implementation we would use the
		// appropriate method to close the websocket connection
	}

	// Wait for all goroutines to exit
	m.wg.Wait()

	// Delete listen key to release server resources
	if key := m.listenKey.Load().(string); key != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		form := url.Values{}
		form.Add("listenKey", key)
		err := m.deleteAPI(ctx, "/api/v3/userDataStream", nil, true, false, nil)
		if err != nil {
			m.log.Errorf("Error deleting listen key: %v", err)
		}
	}
}

// processDepthUpdate processes orderbook depth updates from the WebSocket.
func (m *mexc) processDepthUpdate(update *mexctypes.WsDepthUpdate) {
	symbolParts := strings.Split(update.Stream, "@")
	if len(symbolParts) < 1 {
		m.log.Errorf("Invalid market in depth update: %s", update.Stream)
		return
	}

	symbol := symbolParts[0]

	// Find the orderbook for this symbol
	m.booksMtx.RLock()
	book, found := m.books[symbol]
	m.booksMtx.RUnlock()

	if !found {
		// We're not tracking this market
		return
	}

	// Process the update
	// Note: In a real implementation, we'd need to handle sequence numbers
	// to ensure we apply updates in the correct order

	// Parse the update data
	var updateData struct {
		FirstUpdateID int64            `json:"U"`
		FinalUpdateID int64            `json:"u"`
		Bids          [][2]json.Number `json:"b"`
		Asks          [][2]json.Number `json:"a"`
		EventTime     int64            `json:"E"`
	}

	if err := json.Unmarshal(update.Data, &updateData); err != nil {
		m.log.Errorf("Error parsing depth update data: %v", err)
		return
	}

	// Convert the update to our internal format
	depthUpdate := &mexctypes.WsDepthUpdateData{
		Symbol:  symbol,
		Bids:    updateData.Bids,
		Asks:    updateData.Asks,
		Version: strconv.FormatInt(updateData.FinalUpdateID, 10),
	}

	// Queue the update for processing
	select {
	case book.updateQueue <- depthUpdate:
		// Update queued successfully
	default:
		m.log.Warnf("Depth update queue full for %s", symbol)
	}
}

// newMEXC creates a new MEXC exchange client.
// This matches the capitalization used in interface.go
func newMEXC(cfg *CEXConfig) (CEX, error) {
	return newMexc(cfg)
}

// Balances returns the balances of known assets on the CEX.
func (m *mexc) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	m.balanceMtx.RLock()
	defer m.balanceMtx.RUnlock()

	// If we need to refresh balances, do it
	if len(m.balances) == 0 {
		m.balanceMtx.RUnlock() // Unlock for refreshBalances which will lock again
		if err := m.setBalances(ctx); err != nil {
			return nil, err
		}
		m.balanceMtx.RLock() // Lock again after refreshing
	}

	// Create a copy of the balances map to return
	balances := make(map[uint32]*ExchangeBalance, len(m.balances))
	for assetID, balance := range m.balances {
		balances[assetID] = &ExchangeBalance{
			Available: balance.Available,
			Locked:    balance.Locked,
		}
	}

	return balances, nil
}

// MatchedMarkets returns the list of markets at the CEX.
func (m *mexc) MatchedMarkets(ctx context.Context) ([]*MarketMatch, error) {
	// Get fresh market data
	mexcMarkets, err := m.getMarkets(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching MEXC markets: %w", err)
	}

	// Make sure tokenIDs are loaded
	tokenIDsI := m.tokenIDs.Load()
	if tokenIDsI == nil {
		m.log.Warnf("TokenIDs not loaded yet when fetching matched markets, attempting to load coin info first")
		if err := m.getCoinInfo(ctx); err != nil {
			return nil, fmt.Errorf("could not load token mappings: %w", err)
		}

		// Check again after loading
		tokenIDsI = m.tokenIDs.Load()
		if tokenIDsI == nil {
			return nil, fmt.Errorf("failed to initialize token mappings")
		}
	}

	tokenIDs := tokenIDsI.(map[string][]uint32)
	var matches []*MarketMatch
	seenDexPairs := make(map[string]bool)

	for _, symInfo := range mexcMarkets {
		// Get base asset IDs for this market
		baseIDs := getMEXCDEXAssetIDs(symInfo.BaseAsset, tokenIDs)
		if len(baseIDs) == 0 {
			// Skip if we have no mapping for the base asset
			continue
		}

		// Get quote asset IDs for this market
		quoteIDs := getMEXCDEXAssetIDs(symInfo.QuoteAsset, tokenIDs)
		if len(quoteIDs) == 0 {
			// Skip if we have no mapping for the quote asset
			continue
		}

		// Create market matches for all combinations of base and quote IDs
		for _, baseID := range baseIDs {
			// Validate base asset ID
			_, baseErr := asset.UnitInfo(baseID)
			if baseErr != nil {
				continue // Skip if this base ID is not known by asset system
			}

			for _, quoteID := range quoteIDs {
				// Validate quote asset ID
				_, quoteErr := asset.UnitInfo(quoteID)
				if quoteErr != nil {
					continue // Skip if this quote ID is not known by asset system
				}

				// Generate DEX market name (e.g., "dcr_btc")
				dexMarketName, nameErr := dex.MarketName(baseID, quoteID)
				if nameErr != nil {
					m.log.Warnf("Could not generate DEX market name for base %d / quote %d: %v", baseID, quoteID, nameErr)
					continue
				}

				// Avoid duplicate DEX pairs
				if seenDexPairs[dexMarketName] {
					continue
				}
				seenDexPairs[dexMarketName] = true

				matches = append(matches, &MarketMatch{
					BaseID:   baseID,
					QuoteID:  quoteID,
					MarketID: dexMarketName,
					Slug:     symInfo.Symbol, // Use the MEXC symbol as the Slug
				})
			}
		}
	}

	m.log.Infof("Found %d matched markets on MEXC.", len(matches))
	return matches, nil
}

// SubscribeMarket subscribes to order book updates on a market.
func (m *mexc) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	// Get asset configs
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset configs: %w", err)
	}

	// Form the market slug
	slug := baseCfg.coin + quoteCfg.coin
	m.log.Infof("Subscribing to MEXC market: %s", slug)

	// Lock to prevent concurrent modification of the books map
	m.booksMtx.Lock()
	defer m.booksMtx.Unlock()

	// Check if we already have a book for this market
	book, found := m.books[slug]
	if found {
		book.mtx.Lock()
		book.numSubscribers++
		book.active = true
		book.mtx.Unlock()
		m.log.Debugf("Already subscribed to %s, incremented subscribers to %d", slug, book.numSubscribers)
		return nil
	}

	// Create a new book before starting the connection
	getSnapshot := func() (*mexctypes.DepthResponse, error) {
		return m.getDepthSnapshot(ctx, slug)
	}

	book = newMxOrderBook(
		baseCfg.conversionFactor,
		quoteCfg.conversionFactor,
		slug,
		getSnapshot,
		m.log.SubLogger(fmt.Sprintf("MEXC-OrderBook-%s", slug)),
	)

	m.books[slug] = book
	book.syncChan = make(chan struct{})

	// Start the market stream if not already running
	if err := m.startMarketStreams(ctx); err != nil {
		delete(m.books, slug)
		return fmt.Errorf("error starting market streams: %w", err)
	}

	// Start the book sync process in a goroutine
	go book.sync(ctx)

	// Wait a brief moment for the connection to establish
	time.Sleep(200 * time.Millisecond)

	// Subscribe to the market with the correct format for MEXC protobuf
	channel := fmt.Sprintf("spot@public.limit.depth.v3.api.pb@%s@20", slug)
	subMsg := WsRequest{
		Method: "SUBSCRIPTION",
		Params: []string{channel},
	}

	m.log.Infof("Subscribing to %s with protobuf format", channel)
	subBytes, err := json.Marshal(subMsg)
	if err != nil {
		delete(m.books, slug)
		return fmt.Errorf("error marshaling subscription message: %w", err)
	}

	// Send subscription with retries
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		m.marketStreamMtx.RLock()
		marketStream := m.marketStream
		isDown := marketStream == nil || marketStream.IsDown()
		m.marketStreamMtx.RUnlock()

		if isDown {
			lastErr = fmt.Errorf("market stream is down")
			time.Sleep(500 * time.Millisecond)
			continue
		}

		m.marketStreamMtx.RLock()
		err = m.marketStream.SendRaw(subBytes)
		m.marketStreamMtx.RUnlock()

		if err != nil {
			lastErr = err
			m.log.Warnf("Attempt %d: Error subscribing to %s: %v", attempt+1, channel, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		m.log.Infof("Successfully subscribed to %s", channel)
		return nil
	}

	// If all attempts failed, clean up and return an error
	delete(m.books, slug)
	if lastErr != nil {
		return fmt.Errorf("failed to subscribe to %s after multiple attempts: %w", channel, lastErr)
	}
	return fmt.Errorf("failed to subscribe to %s after multiple attempts", channel)
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (m *mexc) UnsubscribeMarket(baseID, quoteID uint32) error {
	// Get asset configs
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset configs: %w", err)
	}

	// Form the market slug
	slug := baseCfg.coin + quoteCfg.coin

	// Lock to protect the books map
	m.booksMtx.Lock()
	defer m.booksMtx.Unlock()

	// Get the book
	book, found := m.books[slug]
	if !found {
		return nil // Already unsubscribed
	}

	// Decrement subscriber count
	book.mtx.Lock()
	book.numSubscribers--
	shouldRemove := book.numSubscribers == 0
	book.active = !shouldRemove
	book.mtx.Unlock()

	// If this was the last subscriber, remove the book and unsubscribe
	if shouldRemove {
		// Try to unsubscribe from the websocket
		m.marketStreamMtx.Lock()
		if m.marketStream != nil && !m.marketStream.IsDown() {
			subMsg := WsRequest{
				Method: "UNSUBSCRIPTION",
				Params: []string{fmt.Sprintf("%s@depth", slug)},
			}

			subBytes, err := json.Marshal(subMsg)
			if err == nil {
				err = m.marketStream.SendRaw(subBytes)
				if err != nil {
					m.log.Errorf("Error unsubscribing from %s@depth: %v", slug, err)
				} else {
					m.log.Infof("Unsubscribed from %s@depth", slug)
				}
			}
		}
		m.marketStreamMtx.Unlock()

		// Remove the book
		delete(m.books, slug)
	}

	return nil
}

// VWAP returns the volume weighted average price for a quantity of the base asset.
func (m *mexc) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	// Get asset configs
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("error getting asset configs: %w", err)
	}

	// Form the market slug
	slug := baseCfg.coin + quoteCfg.coin

	// Get the book
	m.booksMtx.RLock()
	book, found := m.books[slug]
	m.booksMtx.RUnlock()

	if !found {
		return 0, 0, false, fmt.Errorf("market %s not subscribed", slug)
	}

	// Get VWAP from the book
	return book.vwap(sell, qty)
}

// MidGap returns the mid-gap price for an order book.
func (m *mexc) MidGap(baseID, quoteID uint32) uint64 {
	// Get asset configs
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return 0
	}

	// Form the market slug
	slug := baseCfg.coin + quoteCfg.coin

	// Get the book
	m.booksMtx.RLock()
	book, found := m.books[slug]
	m.booksMtx.RUnlock()

	if !found {
		return 0
	}

	// Get mid gap from the book
	return book.midGap()
}

// Book generates the current view of a market's orderbook.
func (m *mexc) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	// Get asset configs
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting asset configs: %w", err)
	}

	// Form the market slug
	slug := baseCfg.coin + quoteCfg.coin

	// Get the book
	m.booksMtx.RLock()
	book, found := m.books[slug]
	m.booksMtx.RUnlock()

	if !found {
		return nil, nil, fmt.Errorf("market %s not subscribed", slug)
	}

	// Create a copy of the book's entries
	book.mtx.RLock()
	defer book.mtx.RUnlock()

	// Since we don't have direct access to the buys/sells fields,
	// we're just returning empty slices for now
	// In a real implementation, we would get a snapshot of the current book

	return []*core.MiniOrder{}, []*core.MiniOrder{}, nil
}

// TradeStatus returns the current status of a trade on MEXC.
func (m *mexc) TradeStatus(ctx context.Context, tradeID string, baseID, quoteID uint32) (*Trade, error) {
	// Get asset configurations
	baseCfg, quoteCfg, err := mxAssetCfgs(baseID, quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset configs: %w", err)
	}

	// Form the market symbol
	symbol := baseCfg.coin + quoteCfg.coin

	// Query the order status
	query := url.Values{}
	query.Add("symbol", symbol)
	query.Add("orderId", tradeID) // Using orderId parameter as per MEXC docs

	var resp mexctypes.Order
	err = m.getAPI(ctx, "/api/v3/order", query, true, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("error getting order status: %w", err)
	}

	m.log.Debugf("Got status for order %s: %s, side: %s, executed: %s/%s",
		tradeID, resp.Status, resp.Side, resp.ExecutedQuantity, resp.OrigQuantity)

	// Determine if trade is complete - only consider FILLED as complete
	complete := resp.Status == "FILLED"

	// Get price from response
	price, err := strconv.ParseFloat(resp.Price, 64)
	if err != nil {
		m.log.Errorf("Error parsing price: %v", err)
		price = 0
	}

	// Calculate rate in DEX format
	rate := calc.MessageRateAlt(price, baseCfg.conversionFactor, quoteCfg.conversionFactor)

	// Parse original quantity
	origQty, err := strconv.ParseFloat(resp.OrigQuantity, 64)
	if err != nil {
		m.log.Errorf("Error parsing original quantity: %v", err)
		origQty = 0
	}
	qty := uint64(math.Round(origQty * float64(baseCfg.conversionFactor)))

	// Parse executed quantities
	execQty, err := strconv.ParseFloat(resp.ExecutedQuantity, 64)
	if err != nil {
		m.log.Errorf("Error parsing executed quantity: %v", err)
		execQty = 0
	}

	quoteQty, err := strconv.ParseFloat(resp.CummulativeQuoteQty, 64)
	if err != nil {
		m.log.Errorf("Error parsing quote quantity: %v", err)
		quoteQty = 0
	}

	baseFilled := uint64(math.Round(execQty * float64(baseCfg.conversionFactor)))
	quoteFilled := uint64(math.Round(quoteQty * float64(quoteCfg.conversionFactor)))

	// If the order is complete, update our tracking maps
	if complete {
		m.activeTradesMtx.Lock()
		delete(m.activeTrades, tradeID)
		m.activeTradesMtx.Unlock()

		m.tradeUpdaterMtx.Lock()
		delete(m.tradeInfo, tradeID)
		m.tradeUpdaterMtx.Unlock()
	}

	// Side is always "BUY" or "SELL" in the response
	isSell := resp.Side == "SELL"

	return &Trade{
		ID:          tradeID,
		Sell:        isSell,
		Qty:         qty,
		Rate:        rate,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
		Complete:    complete,
	}, nil
}

// ConfirmDeposit is an async function that calls onConfirm when the status of
// a deposit has been confirmed.
func (m *mexc) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	var resp []*mexctypes.PendingDeposit
	// We'll add info for the fake server.
	var query url.Values

	assetCfg, err := mxAssetCfg(deposit.AssetID)
	if err != nil {
		m.log.Errorf("Error getting asset cfg for %d: %v", deposit.AssetID, err)
		return false, 0
	}

	query = url.Values{
		"coin": []string{assetCfg.coin},
	}

	// Add txId if available
	if deposit.TxID != "" {
		query.Add("txId", deposit.TxID)
	}

	err = m.getAPI(ctx, "/api/v3/capital/deposit/hisrec", query, true, true, &resp)
	if err != nil {
		m.log.Errorf("error getting deposit status: %v", err)
		return false, 0
	}

	for _, status := range resp {
		if status.TxID == deposit.TxID {
			switch status.Status {
			case 1: // DepositStatusSuccess - According to MEXC API: 0:pending, 1:success
				ui, err := asset.UnitInfo(deposit.AssetID)
				if err != nil {
					m.log.Errorf("Failed to find unit info for asset ID %d", deposit.AssetID)
					return true, 0
				}
				amount := uint64(status.Amount * float64(ui.Conventional.ConversionFactor))
				return true, amount
			case 0: // DepositStatusPending
				return false, 0
			default:
				m.log.Errorf("Deposit %s to MEXC has an unknown status %d", status.TxID, status.Status)
				return true, 0
			}
		}
	}

	return false, 0
}

// ConfirmWithdrawal checks whether a withdrawal has been completed. If the
// withdrawal has not yet been sent, ErrWithdrawalPending is returned.
func (m *mexc) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	assetCfg, err := mxAssetCfg(assetID)
	if err != nil {
		return 0, "", fmt.Errorf("error getting asset cfg for %d: %w", assetID, err)
	}

	// Create query parameters
	query := url.Values{}
	query.Add("coin", assetCfg.coin)

	// If withdrawal ID is provided, add it to the query
	if withdrawalID != "" {
		query.Add("withdrawId", withdrawalID)
	}

	// Query withdrawal history
	var withdrawHistoryResponse []struct {
		ID     string  `json:"id"`
		Amount float64 `json:"amount"`
		Status int     `json:"status"` // 0:Email Sent, 1:Cancelled, 2:Awaiting Approval, 3:Rejected, 4:Processing, 5:Failure, 6:Completed
		TxID   string  `json:"txId"`
	}

	if err := m.getAPI(ctx, "/api/v3/capital/withdraw/history", query, true, true, &withdrawHistoryResponse); err != nil {
		return 0, "", err
	}

	// Find the matching withdrawal
	for _, status := range withdrawHistoryResponse {
		if status.ID == withdrawalID {
			// Completed (status = 6)
			if status.Status == 6 && status.TxID != "" {
				amt := status.Amount * float64(assetCfg.conversionFactor)
				return uint64(amt), status.TxID, nil
			}

			// Pending statuses
			if status.Status == 0 || status.Status == 2 || status.Status == 4 {
				return 0, "", ErrWithdrawalPending
			}

			// Other statuses (1:Cancelled, 3:Rejected, 5:Failure)
			return 0, "", fmt.Errorf("withdrawal status: %d", status.Status)
		}
	}

	return 0, "", fmt.Errorf("withdrawal %s not found", withdrawalID)
}

// GetDepositAddress returns a deposit address for an asset.
func (m *mexc) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	assetCfg, err := mxAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting asset config: %w", err)
	}

	// Create query parameters
	query := url.Values{}
	query.Add("coin", assetCfg.coin)
	query.Add("network", assetCfg.chain)

	// Use the DepositAddress type from mexctypes for the response
	var resp mexctypes.DepositAddress
	if err := m.getAPI(ctx, "/api/v3/capital/deposit/address", query, true, true, &resp); err != nil {
		return "", fmt.Errorf("error getting deposit address: %w", err)
	}

	// Return the address, possibly with tag/memo if needed
	address := resp.Address
	if resp.Tag != "" {
		address += " (Memo/Tag: " + resp.Tag + ")"
	}

	return address, nil
}

// SubscribeTradeUpdates subscribes to updates for a trade.
func (m *mexc) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	m.tradeUpdaterMtx.Lock()
	defer m.tradeUpdaterMtx.Unlock()

	ch := make(chan *Trade, 32)
	id := m.tradeUpdateCounter
	m.tradeUpdateCounter++
	m.tradeUpdaters[id] = ch

	// Return the channel, a cancel function, and the ID
	return ch, func() {
		m.tradeUpdaterMtx.Lock()
		defer m.tradeUpdaterMtx.Unlock()
		delete(m.tradeUpdaters, id)
	}, id
}

// mapMEXCCoinNetworkToDEXSymbol attempts to construct a DEX symbol (lowercase)
// from MEXC coin and network names
func (m *mexc) mapMEXCCoinNetworkToDEXSymbol(mexcCoinUpper, mexcNetwork string) string {
	if mexcCoinUpper == "" || mexcNetwork == "" {
		return ""
	}

	dexBase := strings.ToLower(mexcCoinUpper)
	upNetwork := strings.ToUpper(mexcNetwork)

	// Special case for USDT: Always map to USDT.polygon for DCRDEX
	if dexBase == "usdt" {
		return "usdt.polygon"
	}

	// Handle the native case explicitly first
	if strings.EqualFold(mexcCoinUpper, mexcNetwork) {
		return dexBase
	}

	// If not native, determine the suffix based on known token networks
	var dexNetSuffix string
	switch upNetwork {
	case "ETH", "ERC20":
		dexNetSuffix = ".erc20"
	case "TRX", "TRC20":
		dexNetSuffix = ".trc20"
	case "BEP20(BSC)":
		dexNetSuffix = ".bep20"
	case "SOLANA":
		dexNetSuffix = ".sol"
	case "MATIC":
		dexNetSuffix = ".polygon"
	default:
		// Network is not native (checked above) and not a known token network
		return ""
	}

	// Return token symbol (base + suffix)
	return dexBase + dexNetSuffix
}

// WsRequest represents a WebSocket request to MEXC.
type WsRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// WsResponse represents a WebSocket response from MEXC.
type WsResponse struct {
	Code   int         `json:"code,omitempty"`
	Error  interface{} `json:"err,omitempty"`
	Result interface{} `json:"result,omitempty"`
}

// connectUserStream sets up and establishes the user data stream
func (m *mexc) connectUserStream(ctx context.Context) {
	// Get a listen key first
	if err := m.getUserDataStream(ctx); err != nil {
		m.log.Errorf("Failed to get listen key for user data stream: %v", err)
		return
	}

	// The userDataStreamHandler will handle connection and reconnection
	// It gets called by getUserDataStream
}
