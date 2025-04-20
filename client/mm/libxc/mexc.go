package libxc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
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
)

const (
	mexcHTTPURL = "https://api.mexc.com"
	mexcWsURL   = "wss://wbs.mexc.com/ws"
	// Add testnet URLs if needed and known
	// mexcTestnetHTTPURL = "..."
	// mexcTestnetWsURL   = "..."
	mexcRecvWindow = "5000"           // Recommended recvWindow for signed requests
	cacheDuration  = 10 * time.Minute // How long to cache exchange/coin info
	// Add local constant, value copied from comms/wsconn.go
	reconnectInterval = 5 * time.Second
)

// Ensure mexc implements the CEX interface.
var _ CEX = (*mexc)(nil)

// tradeInfo stores details needed to update a trade via websocket/polling.
// Ensure only one definition exists.
type mexcTradeInfo struct {
	updaterID   int
	baseID      uint32
	quoteID     uint32
	sell        bool
	rate        uint64
	qty         uint64
	baseFilled  uint64 // Add cumulative base filled
	quoteFilled uint64 // Add cumulative quote filled
}

type mexc struct {
	// Configuration and Dependencies
	cfg       *CEXConfig
	log       dex.Logger
	client    *http.Client // Use a shared client potentially configured in cfg?
	net       dex.Network
	apiKey    string
	secretKey string
	broadcast func(interface{}) // For notifications

	// State & Caches
	balances    map[uint32]*ExchangeBalance
	balancesMtx sync.RWMutex

	// Exchange and Coin Info Caches
	exchangeInfo      *mexctypes.ExchangeInfo
	exchangeInfoMtx   sync.RWMutex
	exchangeInfoStamp time.Time

	coinInfo      map[string]*mexctypes.CoinInfo // map MEXC coin name (uppercase) -> info
	coinInfoMtx   sync.RWMutex
	coinInfoStamp time.Time

	// Asset ID to MEXC Coin mapping (and vice versa)
	assetSymbolToCoin map[string]string   // e.g. "btc" -> "BTC", "usdt.erc20" -> "USDT"
	assetIDToCoin     map[uint32]string   // e.g. btcID -> "BTC"
	coinToAssetIDs    map[string][]uint32 // e.g. "USDT" -> [usdtErc20ID, usdtTrc20ID]
	symbolToAssetID   map[string]uint32   // NEW: Map from DEX symbol to ID
	mapMtx            sync.RWMutex

	// Client Order ID generation
	tradeIDNonce       uint32
	tradeIDNonceMtx    sync.Mutex
	tradeIDNoncePrefix dex.Bytes // Prefix based on API key

	// Websocket Management
	marketStream        comms.WsConn
	marketStreamMtx     sync.RWMutex
	marketSubs          map[string]uint32 // key: MEXC market symbol (e.g. BTCUSDT), value: subscriber count
	marketSubResyncChan chan string       // Channel to signal book resync needed for a market symbol

	userDataStream     comms.WsConn
	userDataStreamMtx  sync.RWMutex
	listenKey          atomic.Value  // Stores the current listen key (string)
	listenKeyMtx       sync.Mutex    // For coordinating listen key refresh
	listenKeyRefresh   *time.Timer   // Timer for next keepalive
	listenKeyRequested atomic.Bool   // Prevent concurrent getListenKey calls
	reconnectChan      chan struct{} // Signals need to reconnect user data stream

	// Order Book Management
	books    map[string]*mexcOrderBook // key: MEXC market symbol (e.g. BTCUSDT)
	booksMtx sync.RWMutex

	// Trade Update Management
	tradeUpdaters      map[int]chan *Trade // subscriptionID -> channel
	tradeUpdaterMtx    sync.RWMutex
	tradeUpdateCounter int
	// Need map to track active trades for user data stream updates
	activeTrades    map[string]*mexcTradeInfo // key: MEXC order ID
	activeTradesMtx sync.RWMutex

	// Shutdown signalling
	wg   sync.WaitGroup
	quit chan struct{}
}

// mexcOrderBook struct definition
type mexcOrderBook struct {
	mtx            sync.RWMutex
	synced         atomic.Bool
	syncChan       chan struct{} // Closed when initial sync completes
	stopChan       chan struct{} // Closed to signal run loop exit
	numSubscribers uint32
	getSnapshot    func(ctx context.Context, symbol string) (*mexctypes.DepthResponse, error)
	book           *orderbook // Use local type
	updateQueue    chan *mexctypes.WsDepthUpdateData
	mktSymbol      string
	baseFactor     uint64
	quoteFactor    uint64
	log            dex.Logger
	lastUpdateID   uint64 // Use uint64 for comparison
}

// newMEXC constructor (Configure RawHandler)
func newMEXC(cfg *CEXConfig) (*mexc, error) {
	if cfg.APIKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("API key and secret key are required for MEXC")
	}

	client := &http.Client{
		Timeout: time.Second * 20,
	}

	prefixBytes := sha256.Sum256([]byte(cfg.APIKey))
	prefix := hex.EncodeToString(prefixBytes[:4])

	m := &mexc{
		cfg:       cfg,
		log:       cfg.Logger, // Logger assigned here
		client:    client,
		net:       cfg.Net,
		apiKey:    cfg.APIKey,
		secretKey: cfg.SecretKey,
		broadcast: cfg.Notify,
		quit:      make(chan struct{}),

		balances:          make(map[uint32]*ExchangeBalance),
		coinInfo:          make(map[string]*mexctypes.CoinInfo),
		assetSymbolToCoin: make(map[string]string),
		assetIDToCoin:     make(map[uint32]string),
		coinToAssetIDs:    make(map[string][]uint32),
		symbolToAssetID:   make(map[string]uint32), // Initialize new map

		tradeIDNoncePrefix: dex.Bytes(prefix),

		marketSubs:          make(map[string]uint32),
		marketSubResyncChan: make(chan string, 10),
		books:               make(map[string]*mexcOrderBook),
		tradeUpdaters:       make(map[int]chan *Trade),
		activeTrades:        make(map[string]*mexcTradeInfo),
		reconnectChan:       make(chan struct{}, 1),
	}
	// Add log check right after logger assignment
	// m.log.Debugf("Logger check inside newMEXC - This should appear if debug is set.")

	// Pre-build the symbol -> assetID map
	// m.log.Debugf("Building DEX symbol to Asset ID map...")
	assetsInfo := asset.Assets()
	for id, assetData := range assetsInfo {
		lowerSymbol := strings.ToLower(assetData.Symbol)
		m.symbolToAssetID[lowerSymbol] = id
		// m.log.Tracef("Mapped symbol \"%s\" to ID %d", lowerSymbol, id)
		if assetData.Tokens != nil {
			for tokenID := range assetData.Tokens { // Iterate over keys (token IDs)
				// Get the registered symbol string for the token ID
				tokenSymbol := dex.BipIDSymbol(tokenID)
				if tokenSymbol != "" {
					lowerTokenSymbol := strings.ToLower(tokenSymbol)
					m.symbolToAssetID[lowerTokenSymbol] = tokenID
					// m.log.Tracef("Mapped token symbol \"%s\" to ID %d", lowerTokenSymbol, tokenID)
				} else {
					m.log.Warnf("Could not get symbol for known token ID %d", tokenID)
				}
			}
		}
	}
	m.log.Infof("Built DEX symbol map with %d entries.", len(m.symbolToAssetID))

	// --- Add this block to log map keys ---
	// m.log.Debugf("Symbols found in asset map:")
	mapKeys := make([]string, 0, len(m.symbolToAssetID))
	for k := range m.symbolToAssetID {
		mapKeys = append(mapKeys, k)
	}
	sort.Strings(mapKeys) // Sort for easier reading
	for _, k := range mapKeys {
		_ = k // Avoid declared and not used error when debug log is commented
		// m.log.Debugf("  - \"%s\" -> ID %d", k, m.symbolToAssetID[k])
	}
	// --- End block ---

	// Setup WS Config
	// Create distinct loggers for each stream
	marketLogger := cfg.Logger.SubLogger("MarketWS")
	userLogger := cfg.Logger.SubLogger("UserWS")

	wsCfgBase := &comms.WsCfg{
		Logger:               cfg.Logger,       // Base logger, will be replaced
		PingWait:             60 * time.Second, // Default, will be overridden
		DisableAutoReconnect: false,
		// ReconnectSync and ConnectEventFunc will be set per stream
	}

	// Market Stream uses handleMarketRawMessage
	marketWsCfg := *wsCfgBase
	marketWsCfg.Logger = marketLogger // Assign market logger
	marketWsCfg.URL = mexcWsURL
	marketWsCfg.RawHandler = m.handleMarketRawMessage // Assign market raw handler
	marketWsCfg.ReconnectSync = m.resubscribeMarkets
	marketWsCfg.ConnectEventFunc = m.handleMarketConnectEvent
	marketWsCfg.PingWait = 75 * time.Second // Increased PingWait for market stream
	marketStream, err := comms.NewWsConn(&marketWsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create market WebSocket connection: %w", err)
	}
	m.marketStream = marketStream

	// User Stream uses handleUserDataRawMessage
	userWsCfg := *wsCfgBase
	userWsCfg.Logger = userLogger                         // Assign user logger
	userWsCfg.URL = mexcWsURL                             // Actual URL set later with listen key
	userWsCfg.RawHandler = m.handleUserDataRawMessage     // Assign specific raw handler
	userWsCfg.ReconnectSync = nil                         // No automatic resubscribe for user stream (needs new key)
	userWsCfg.ConnectEventFunc = m.handleUserConnectEvent // Separate event handler
	userWsCfg.PingWait = 24 * time.Hour                   // Effectively disable WsConn auto ping/pong
	userWsCfg.EchoPingData = true                         // Required by MEXC for some interactions
	userDataStream, err := comms.NewWsConn(&userWsCfg)
	if err != nil {
		// Clean up market stream if user stream fails? Consider this if needed.
		return nil, fmt.Errorf("failed to create user data WebSocket connection: %w", err)
	}
	m.userDataStream = userDataStream

	// Initial ping to check connectivity and API key validity (for signed endpoints)
	err = m.ping(context.Background())
	if err != nil {
		// Don't wrap specific error here, request func already formats it
		return nil, fmt.Errorf("initial MEXC ping failed: %w", err)
	}
	m.log.Infof("MEXC ping successful.")

	// Fetch initial exchange info and coin info required for operation
	// These functions will need to be implemented
	/*
	   _, err = m.getCachedExchangeInfo(context.Background())
	   if err != nil {
	       return nil, fmt.Errorf("failed to fetch initial MEXC exchange info: %w", err)
	   }
	   _, err = m.getCachedCoinInfo(context.Background())
	   if err != nil {
	       return nil, fmt.Errorf("failed to fetch initial MEXC coin info: %w", err)
	   }
	   m.log.Infof("Fetched initial MEXC exchange and coin info.")
	*/

	m.log.Infof("Initialized MEXC exchange client")

	// Initialize listenKey value to empty string
	m.listenKey.Store("")

	return m, nil
}

// request makes an HTTP request to the MEXC API.
// It handles adding headers, signing, sending the request, and decoding the response.
// `thing` should be a pointer to the struct where the JSON response will be decoded.
// For endpoints that don't return data (like DELETE, or successful POSTs without data), `thing` can be nil.
func (m *mexc) request(ctx context.Context, method, path string, params url.Values, form url.Values, needsSigning bool, thing interface{}) error {
	if params == nil {
		params = url.Values{}
	}

	bodyStr := ""
	if form != nil {
		bodyStr = form.Encode() // Use form encoding for body
	}

	// Signature payload generation
	signaturePayload := ""
	if needsSigning {
		// Add timestamp and recvWindow to query params for ALL signed requests
		timestampStr := strconv.FormatInt(time.Now().UnixMilli(), 10)
		params.Set("timestamp", timestampStr)
		params.Set("recvWindow", mexcRecvWindow)

		// Determine payload based on method
		switch method {
		case http.MethodGet, http.MethodDelete:
			// For GET/DELETE, sign the query string parameters
			// Ensure params are encoded alphabetically (url.Values.Encode() does this)
			signaturePayload = params.Encode() // Payload = Query String
		case http.MethodPost, http.MethodPut:
			// For POST/PUT: Sign the body IF it exists.
			// If body is empty, sign the query parameters instead.
			if bodyStr != "" {
				signaturePayload = bodyStr
			} else {
				signaturePayload = params.Encode() // Sign query params if body is empty
			}
		default:
			return fmt.Errorf("unsupported HTTP method for MEXC signing: %s", method)
		} // Corrected closing brace for switch

		// Calculate signature
		mac := hmac.New(sha256.New, []byte(m.secretKey))
		_, err := mac.Write([]byte(signaturePayload))
		if err != nil {
			return fmt.Errorf("HMAC write failed: %w", err)
		}
		signature := hex.EncodeToString(mac.Sum(nil))
		// Signature MUST be added to query parameters for MEXC
		params.Set("signature", signature)
	}

	// Construct final URL with all params (including signature if added)
	finalQueryString := params.Encode()
	fullURL := mexcHTTPURL + path
	if finalQueryString != "" {
		fullURL += "?" + finalQueryString
	}

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create MEXC request: %w", err)
	}

	// Add headers
	req.Header.Set("X-MEXC-APIKEY", m.apiKey)
	if bodyStr != "" {
		// Set content type only if there is a body
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "dcrdex-client")

	// m.log.Tracef("MEXC Request: %s %s", method, fullURL)

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("MEXC request failed (%s %s): %w", method, path, err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read MEXC response body (%s %s): %w", method, path, err)
	}

	// m.log.Tracef("MEXC Response Status: %d Body: %s", resp.StatusCode, string(respBodyBytes))

	// Check for non-2xx status codes first.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse as MEXC standard error
		var apiErr mexctypes.ErrorResponse
		errDecode := json.Unmarshal(respBodyBytes, &apiErr)
		// Check if decoding worked AND code is non-zero (0 usually means success in their system)
		if errDecode == nil && apiErr.Code != 0 {
			// Return the structured error
			return &apiErr // Return as error type
		}
		// If decoding failed or code was 0, return a generic HTTP error
		return fmt.Errorf("MEXC request failed (%s %s): status %d, body: %s", method, path, resp.StatusCode, string(respBodyBytes))
	}

	// Success (2xx response). Decode if a target struct is provided.
	if thing != nil {
		err = json.Unmarshal(respBodyBytes, thing)
		if err != nil {
			return fmt.Errorf("failed to decode MEXC success response (%s %s) into %T: %w, body: %s", method, path, thing, err, string(respBodyBytes))
		}
	}

	return nil // Success
}

// ping checks connectivity to the MEXC API (unauthenticated endpoint).
func (m *mexc) ping(ctx context.Context) error {
	path := "/api/v3/ping"
	return m.request(ctx, http.MethodGet, path, nil, nil, false, nil)
}

// checkAPIKeys checks if the provided API keys are valid by making a signed request.
func (m *mexc) checkAPIKeys(ctx context.Context) error {
	path := "/api/v3/account" // Simple signed endpoint
	return m.request(ctx, http.MethodGet, path, nil, nil, true, nil)
}

// --- dex.Connector Implementation ---

// Connect performs initial setup and starts background processes.
func (m *mexc) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if err := m.checkAPIKeys(ctx); err != nil {
		return nil, fmt.Errorf("MEXC API key check failed: %w", err)
	}
	m.log.Infof("MEXC API keys appear valid.")

	if _, err := m.getCachedExchangeInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch initial MEXC exchange info: %w", err)
	}
	if _, err := m.getCachedCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch initial MEXC coin info: %w", err)
	}
	m.log.Infof("Fetched initial MEXC exchange and coin info, updated mappings.")

	// Launch User Data Stream connection manager
	m.log.Infof("Launching connectUserStream goroutine...")
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.connectUserStream(ctx)
	}()
	m.log.Infof("Launched connectUserStream goroutine.")

	// NOTE: Account polling is removed as we rely on User Data Stream primarily now.
	// If User Data Stream proves unreliable, polling could be re-added.
	// m.startAccountDataPolling(ctx)

	// Periodic background refresh tasks
	m.wg.Add(2)
	go m.periodicExchangeInfoRefresh(ctx)
	go m.periodicCoinInfoRefresh(ctx)

	m.log.Infof("MEXC Connect sequence completed (Market stream connects on first subscription).")
	m.log.Infof("Connect method attempting to return...")
	return &m.wg, nil
}

// periodicExchangeInfoRefresh periodically refreshes the Exchange Info cache.
func (m *mexc) periodicExchangeInfoRefresh(ctx context.Context) {
	defer m.wg.Done()

	const defaultInterval = 1 * time.Hour
	const errorInterval = 1 * time.Minute
	interval := defaultInterval
	timer := time.NewTimer(interval)
	defer timer.Stop()

	m.log.Infof("Starting periodic Exchange Info refresh (Interval: %v)", interval)
	defer m.log.Infof("Stopping periodic Exchange Info refresh.")

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := m.getCachedExchangeInfo(refreshCtx)
			cancel()
			if err != nil {
				m.log.Errorf("Periodic Exchange Info refresh failed: %v. Retrying in %v", err, errorInterval)
				interval = errorInterval
			} else {
				if interval != defaultInterval {
					m.log.Infof("Periodic Exchange Info refresh successful. Resetting interval to %v", defaultInterval)
					interval = defaultInterval
				} else {
					m.log.Debugf("Periodic Exchange Info refresh successful.")
				}
			}
			timer.Reset(interval)
		}
	}
}

// periodicCoinInfoRefresh periodically refreshes the Coin Info cache.
func (m *mexc) periodicCoinInfoRefresh(ctx context.Context) {
	defer m.wg.Done()

	const defaultInterval = 1 * time.Hour
	const errorInterval = 1 * time.Minute
	interval := defaultInterval
	timer := time.NewTimer(interval)
	defer timer.Stop()

	m.log.Infof("Starting periodic Coin Info refresh (Interval: %v)", interval)
	defer m.log.Infof("Stopping periodic Coin Info refresh.")

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_, err := m.getCachedCoinInfo(refreshCtx)
			cancel()
			if err != nil {
				m.log.Errorf("Periodic Coin Info refresh failed: %v. Retrying in %v", err, errorInterval)
				interval = errorInterval
			} else {
				if interval != defaultInterval {
					m.log.Infof("Periodic Coin Info refresh successful. Resetting interval to %v", defaultInterval)
					interval = defaultInterval
				} else {
					m.log.Debugf("Periodic Coin Info refresh successful.")
				}
			}
			timer.Reset(interval)
		}
	}
}

// --- CEX Interface Method Placeholders ---

func (m *mexc) Balance(assetID uint32) (*ExchangeBalance, error) {
	m.balancesMtx.RLock()
	defer m.balancesMtx.RUnlock()
	// TODO: Implement actual fetching if not found or stale
	bal, ok := m.balances[assetID]
	if !ok {
		return nil, fmt.Errorf("balance for asset ID %d not available (MEXC)", assetID)
	}
	// Return a copy to prevent modification
	ret := *bal
	return &ret, nil
}

// Balances retrieves the account balances from MEXC.
func (m *mexc) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	// m.log.Debugf("Fetching MEXC balances...")
	path := "/api/v3/account"
	var accountInfo mexctypes.AccountInfo
	err := m.request(ctx, http.MethodGet, path, nil, nil, true, &accountInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get MEXC account info: %w", err)
	}

	// --- Add this logging ---
	// Log the raw structure to see exactly what assets are returned
	rawBalancesJSON, _ := json.MarshalIndent(accountInfo.Balances, "", "  ") // Pretty print
	_ = rawBalancesJSON                                                      // Avoid declared and not used error when debug log is commented
	// m.log.Debugf("Received raw balances from /api/v3/account:\n%s", string(rawBalancesJSON))
	// --- End logging block ---

	_, err = m.getCachedCoinInfo(ctx) // Ensure coin info is loaded for precision
	if err != nil {
		return nil, fmt.Errorf("cannot process balances without coin info: %w", err)
	}

	newBalances := make(map[uint32]*ExchangeBalance)
	m.mapMtx.RLock()
	m.coinInfoMtx.RLock()
	// m.log.Debugf("Processing %d balances from MEXC API", len(accountInfo.Balances)) // Log count
	for _, bal := range accountInfo.Balances {
		mexcCoinUpper := strings.ToUpper(bal.Asset)
		// m.log.Debugf("Processing balance for MEXC Coin: %s (Free: %s, Locked: %s)", mexcCoinUpper, bal.Free, bal.Locked) // Log raw balance

		assetIDs, coinKnown := m.coinToAssetIDs[mexcCoinUpper]
		if !coinKnown || len(assetIDs) == 0 {
			// m.log.Debugf(" -> Skipping %s: Not mapped to any known DEX asset ID", mexcCoinUpper) // Log skip reason
			continue
		}
		// m.log.Debugf(" -> Mapped to DEX IDs: %v", assetIDs) // Log mapped IDs

		// Use first ID for precision lookup, assume precision is per-coin not per-network variant for balance
		precision, pErr := m.getCoinPrecision(ctx, assetIDs[0])
		if pErr != nil {
			m.log.Errorf(" -> Skipping %s: Failed to get precision: %v", mexcCoinUpper, pErr)
			continue
		}
		convFactor := math.Pow10(precision)

		avail, err := m.stringToSatoshis(bal.Free, convFactor)
		if err != nil {
			m.log.Errorf(" -> Skipping %s: Error parsing free balance %s: %v", mexcCoinUpper, bal.Free, err)
			continue
		}
		locked, err := m.stringToSatoshis(bal.Locked, convFactor)
		if err != nil {
			m.log.Errorf(" -> Skipping %s: Error parsing locked balance %s: %v", mexcCoinUpper, bal.Locked, err)
			continue
		}

		for _, assetID := range assetIDs {
			newBalances[assetID] = &ExchangeBalance{Available: avail, Locked: locked}
			// m.log.Debugf("   -> Set balance for DEX ID %d", assetID) // Log successful set
		}
	}
	m.coinInfoMtx.RUnlock()
	m.mapMtx.RUnlock()

	// Update internal cache
	m.balancesMtx.Lock()
	m.balances = newBalances // Replace the whole map
	m.balancesMtx.Unlock()

	// Return a copy
	retBalances := make(map[uint32]*ExchangeBalance, len(newBalances))
	for k, v := range newBalances {
		retBalances[k] = v
	}

	m.log.Infof("Successfully updated MEXC balances for %d assets.", len(retBalances))
	return retBalances, nil
}

// Markets retrieves information for all DEX-supported markets on MEXC using asset.UnitInfo validation.
func (m *mexc) Markets(ctx context.Context) (map[string]*Market, error) {
	info, err := m.getCachedExchangeInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange info for Markets: %w", err)
	}

	// Fetch all tickers
	tickers, tickerErr := m.getTickers24hr(ctx, nil)
	if tickerErr != nil {
		m.log.Warnf("Could not fetch 24hr ticker data for all markets: %v. MarketDay data will be missing.", tickerErr)
		tickers = make(map[string]*mexctypes.Ticker24hr) // Ensure map is not nil
	}

	m.mapMtx.RLock() // Lock for reading mapping info
	defer m.mapMtx.RUnlock()

	markets := make(map[string]*Market)

	for _, symInfo := range info.Symbols {
		// Check status (NOTE: Intentionally ignoring isSpotTradingAllowed as per user request)
		if symInfo.Status != "1" /*|| !symInfo.IsSpotTradingAllowed*/ {
			continue
		}

		// Map MEXC base/quote to potential DEX asset IDs
		mexcBaseUpper := strings.ToUpper(symInfo.BaseAsset)
		mexcQuoteUpper := strings.ToUpper(symInfo.QuoteAsset)

		baseIDs, baseOK := m.coinToAssetIDs[mexcBaseUpper]
		quoteIDs, quoteOK := m.coinToAssetIDs[mexcQuoteUpper]

		if !baseOK || !quoteOK {
			continue // Skip if either base or quote coin isn't mapped by us at all
		}

		// Iterate through all valid DEX asset ID combinations for this MEXC symbol
		for _, baseID := range baseIDs {
			// Validate base asset ID using UnitInfo
			baseUnitInfo, baseErr := asset.UnitInfo(baseID)
			if baseErr != nil {
				// m.log.Tracef("Skipping base ID %d for MEXC base %s: %v", baseID, mexcBaseUpper, baseErr)
				continue // Skip if this specific base ID is not known by asset system
			}

			for _, quoteID := range quoteIDs {
				// Validate quote asset ID using UnitInfo
				quoteUnitInfo, quoteErr := asset.UnitInfo(quoteID)
				if quoteErr != nil {
					// m.log.Tracef("Skipping quote ID %d for MEXC quote %s: %v", quoteID, mexcQuoteUpper, quoteErr)
					continue // Skip if this specific quote ID is not known by asset system
				}

				// Generate DEX market name (e.g., "dcr_btc")
				dexMarketName, nameErr := dex.MarketName(baseID, quoteID)
				if nameErr != nil {
					m.log.Warnf("Could not generate DEX market name for base %d / quote %d: %v", baseID, quoteID, nameErr)
					continue
				}

				// Check if we already added this DEX pair (e.g., from USDT/USDC mapping to same ID)
				if _, exists := markets[dexMarketName]; exists {
					continue
				}

				market := &Market{
					BaseID:  baseID,
					QuoteID: quoteID,
					// Use the validated UnitInfo to get factors
					// BaseMinWithdraw/QuoteMinWithdraw might require info not available here easily,
					// would need to cross-reference with CoinInfo/NetworkList - skip for now.
				}

				// Add MarketDay data if available
				if ticker, ok := tickers[symInfo.Symbol]; ok {
					day, parseErr := m.parseMarketDay(ticker)
					if parseErr != nil {
						m.log.Warnf("Error parsing MarketDay for %s (DEX: %s): %v", symInfo.Symbol, dexMarketName, parseErr)
					} else {
						market.Day = day
					}
				}
				markets[dexMarketName] = market
				_ = baseUnitInfo // Avoid unused variable error if logging is off
				_ = quoteUnitInfo
				// m.log.Tracef("Added market %s (MEXC: %s, Base: %d, Quote: %d)", dexMarketName, symInfo.Symbol, baseID, quoteID)
			}
		}
	}

	// Log the found markets for confirmation
	if len(markets) > 0 {
		mktNames := make([]string, 0, len(markets))
		for name := range markets {
			mktNames = append(mktNames, name)
		}
		sort.Strings(mktNames) // Sort for consistent logging
		m.log.Infof("Found %d supported markets on MEXC: %s", len(markets), strings.Join(mktNames, ", "))
	} else {
		m.log.Warnf("Found 0 supported markets on MEXC after filtering.")
	}
	return markets, nil
}

// MatchedMarkets retrieves all supported markets on MEXC using asset.UnitInfo validation.
func (m *mexc) MatchedMarkets(ctx context.Context) ([]*MarketMatch, error) {
	info, err := m.getCachedExchangeInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange info for MatchedMarkets: %w", err)
	}

	m.mapMtx.RLock() // Lock for reading mapping info
	defer m.mapMtx.RUnlock()

	matches := []*MarketMatch{}
	seenDexPairs := make(map[string]bool) // Track DEX pairs to avoid duplicates

	for _, symInfo := range info.Symbols {
		// Check status (NOTE: Intentionally ignoring isSpotTradingAllowed as per user request)
		if symInfo.Status != "1" /*|| !symInfo.IsSpotTradingAllowed*/ {
			continue
		}

		// Map MEXC base/quote to potential DEX asset IDs
		mexcBaseUpper := strings.ToUpper(symInfo.BaseAsset)
		mexcQuoteUpper := strings.ToUpper(symInfo.QuoteAsset)

		baseIDs, baseOK := m.coinToAssetIDs[mexcBaseUpper]
		quoteIDs, quoteOK := m.coinToAssetIDs[mexcQuoteUpper]

		if !baseOK || !quoteOK {
			continue // Skip if either base or quote coin isn't mapped by us at all
		}

		// Iterate through all valid DEX asset ID combinations
		for _, baseID := range baseIDs {
			// Validate base asset ID using UnitInfo
			_, baseErr := asset.UnitInfo(baseID)
			if baseErr != nil {
				continue // Skip if this specific base ID is not known by asset system
			}

			for _, quoteID := range quoteIDs {
				// Validate quote asset ID using UnitInfo
				_, quoteErr := asset.UnitInfo(quoteID)
				if quoteErr != nil {
					continue // Skip if this specific quote ID is not known by asset system
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
					Slug:     symInfo.Symbol, // Use the specific MEXC symbol as the Slug
				})
			}
		}
	}

	m.log.Infof("Found %d matched markets on MEXC.", len(matches))
	return matches, nil
}

// getTickers24hr fetches 24hr ticker data for specific symbols or all symbols.
func (m *mexc) getTickers24hr(ctx context.Context, symbols []string) (map[string]*mexctypes.Ticker24hr, error) {
	path := "/api/v3/ticker/24hr"
	params := url.Values{}
	if len(symbols) > 0 {
		// Need to format symbols list as JSON array string: ["BTCUSDT","ETHUSDT"]
		symJSON, err := json.Marshal(symbols)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal symbols for ticker request: %w", err)
		}
		params.Set("symbols", string(symJSON))
	}

	var tickers []*mexctypes.Ticker24hr // API returns a list
	err := m.request(ctx, http.MethodGet, path, params, nil, false, &tickers)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MEXC 24hr ticker data: %w", err)
	}

	// Convert list to map keyed by symbol
	tickerMap := make(map[string]*mexctypes.Ticker24hr, len(tickers))
	for _, ticker := range tickers {
		tickerMap[ticker.Symbol] = ticker
	}
	return tickerMap, nil
}

// parseFloat converts a string to float64, returning 0 on error.
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0 // Or handle error more gracefully?
	}
	return f
}

// parseMarketDay converts a MEXC Ticker24hr to a libxc MarketDay.
func (m *mexc) parseMarketDay(ticker *mexctypes.Ticker24hr) (*MarketDay, error) {
	if ticker == nil {
		return nil, fmt.Errorf("nil ticker provided")
	}
	return &MarketDay{
		Vol:            parseFloat(ticker.Volume),
		QuoteVol:       parseFloat(ticker.QuoteVolume),
		PriceChange:    parseFloat(ticker.PriceChange),
		PriceChangePct: parseFloat(ticker.PriceChangePercent),
		AvgPrice:       0, // MEXC ticker doesn't seem to have VWAP/AvgPrice directly
		LastPrice:      parseFloat(ticker.LastPrice),
		OpenPrice:      parseFloat(ticker.OpenPrice),
		HighPrice:      parseFloat(ticker.HighPrice),
		LowPrice:       parseFloat(ticker.LowPrice),
	}, nil
}

// mapMEXCSymbolToDEXIDs attempts to map a MEXC symbol (e.g., "BTCUSDT") to DEX base and quote IDs.
func (m *mexc) mapMEXCSymbolToDEXIDs(mexcSymbol string) (baseIDs, quoteIDs []uint32, err error) {
	info, err := m.getCachedExchangeInfo(context.TODO()) // Use background context for internal lookup
	if err != nil {
		return nil, nil, fmt.Errorf("exchange info not available for symbol mapping")
	}

	var baseAsset, quoteAsset string
	found := false
	for _, si := range info.Symbols {
		if si.Symbol == mexcSymbol {
			baseAsset = strings.ToUpper(si.BaseAsset)
			quoteAsset = strings.ToUpper(si.QuoteAsset)
			found = true
			break
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("MEXC symbol %q not found in exchange info", mexcSymbol)
	}

	m.mapMtx.RLock()
	defer m.mapMtx.RUnlock()

	baseIDs, baseOK := m.coinToAssetIDs[baseAsset]
	quoteIDs, quoteOK := m.coinToAssetIDs[quoteAsset]

	if !baseOK || !quoteOK || len(baseIDs) == 0 || len(quoteIDs) == 0 {
		return nil, nil, fmt.Errorf("could not map base (%s) or quote (%s) for MEXC symbol %q to DEX asset ID", baseAsset, quoteAsset, mexcSymbol)
	}

	return baseIDs, quoteIDs, nil
}

// mapDEXIDsToMEXCSymbol attempts to find the MEXC symbol for a given DEX base/quote ID pair.
func (m *mexc) mapDEXIDsToMEXCSymbol(baseID, quoteID uint32) (string, error) {
	info, err := m.getCachedExchangeInfo(context.TODO())
	if err != nil {
		return "", fmt.Errorf("exchange info not available for symbol lookup")
	}

	m.mapMtx.RLock()
	mexcBaseCoin, baseOK := m.assetIDToCoin[baseID]
	mexcQuoteCoin, quoteOK := m.assetIDToCoin[quoteID]
	m.mapMtx.RUnlock()

	if !baseOK || !quoteOK {
		return "", fmt.Errorf("could not map base ID %d or quote ID %d to MEXC coin", baseID, quoteID)
	}

	// Construct the potential symbol (e.g., BTCUSDT) and check if it exists.
	// Note: MEXC symbols are uppercase.
	potentialSymbol := mexcBaseCoin + mexcQuoteCoin
	found := false
	for _, si := range info.Symbols {
		if si.Symbol == potentialSymbol {
			found = true
			break
		}
	}

	if !found {
		// Try reverse order? (e.g., USDTBTC) - Unlikely for MEXC, but possible
		potentialSymbol = mexcQuoteCoin + mexcBaseCoin
		for _, si := range info.Symbols {
			if si.Symbol == potentialSymbol {
				found = true
				break
			}
		}
	}

	if !found {
		return "", fmt.Errorf("could not find MEXC symbol for base %s (ID %d) / quote %s (ID %d)", mexcBaseCoin, baseID, mexcQuoteCoin, quoteID)
	}

	return potentialSymbol, nil
}

// --- Caching Helpers ---

// getCachedExchangeInfo fetches exchange info if the cache is stale.
func (m *mexc) getCachedExchangeInfo(ctx context.Context) (*mexctypes.ExchangeInfo, error) {
	m.exchangeInfoMtx.RLock()
	if m.exchangeInfo != nil && time.Since(m.exchangeInfoStamp) < cacheDuration {
		info := m.exchangeInfo
		m.exchangeInfoMtx.RUnlock()
		return info, nil
	}
	m.exchangeInfoMtx.RUnlock()

	// Cache miss or stale, need to fetch (with write lock)
	m.exchangeInfoMtx.Lock()
	defer m.exchangeInfoMtx.Unlock()

	// Double check after acquiring write lock
	if m.exchangeInfo != nil && time.Since(m.exchangeInfoStamp) < cacheDuration {
		return m.exchangeInfo, nil
	}

	m.log.Infof("Fetching MEXC exchange info...")
	path := "/api/v3/exchangeInfo"
	var info mexctypes.ExchangeInfo
	err := m.request(ctx, http.MethodGet, path, nil, nil, false, &info)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MEXC exchange info: %w", err)
	}

	m.exchangeInfo = &info
	m.exchangeInfoStamp = time.Now()
	m.log.Infof("Successfully fetched and cached MEXC exchange info (%d symbols).", len(info.Symbols))

	// Remove premature call - mapping requires coinInfo as well.
	// m.updateSymbolMappings(false)

	return m.exchangeInfo, nil
}

// getCachedCoinInfo fetches coin info if the cache is stale.
func (m *mexc) getCachedCoinInfo(ctx context.Context) (map[string]*mexctypes.CoinInfo, error) {
	m.coinInfoMtx.RLock()
	if m.coinInfo != nil && len(m.coinInfo) > 0 && time.Since(m.coinInfoStamp) < cacheDuration {
		infoMap := m.coinInfo
		m.coinInfoMtx.RUnlock()
		return infoMap, nil
	}
	m.coinInfoMtx.RUnlock()

	// Cache miss or stale, need to fetch (with write lock)
	m.coinInfoMtx.Lock()
	defer m.coinInfoMtx.Unlock()

	// Double check after acquiring write lock
	if m.coinInfo != nil && len(m.coinInfo) > 0 && time.Since(m.coinInfoStamp) < cacheDuration {
		return m.coinInfo, nil
	}

	m.log.Infof("Fetching MEXC coin info...")
	path := "/api/v3/capital/config/getall"
	var infoList []*mexctypes.CoinInfo                                     // API returns a list
	err := m.request(ctx, http.MethodGet, path, nil, nil, true, &infoList) // Requires signing
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MEXC coin info: %w", err)
	}

	// Convert list to map keyed by uppercase coin name
	newCoinInfo := make(map[string]*mexctypes.CoinInfo, len(infoList))
	for _, coin := range infoList {
		newCoinInfo[strings.ToUpper(coin.Coin)] = coin
	}

	m.coinInfo = newCoinInfo
	m.coinInfoStamp = time.Now()
	m.log.Infof("Successfully fetched and cached MEXC coin info (%d coins).", len(newCoinInfo))

	// Update mappings - indicate we hold the coin lock
	m.updateSymbolMappings(true)

	return m.coinInfo, nil
}

// updateSymbolMappings rebuilds the internal assetID <-> coin name mappings,
func (m *mexc) updateSymbolMappings(callerHoldsCoinLock bool) {
	m.mapMtx.Lock()
	if !callerHoldsCoinLock {
		m.coinInfoMtx.RLock()
		defer m.coinInfoMtx.RUnlock()
	}
	defer m.mapMtx.Unlock()

	if m.coinInfo == nil {
		m.log.Warnf("Cannot update symbol mappings: coinInfo not loaded")
		return
	}
	// fmt.Println("PRINTF_DEBUG: Starting updateSymbolMappings...") // Remove Printf

	m.assetSymbolToCoin = make(map[string]string)
	m.assetIDToCoin = make(map[uint32]string)
	m.coinToAssetIDs = make(map[string][]uint32)
	mappedCount := 0

	// Target DEX symbols we care about (e.g., DCR, BTC, USDT variants)
	targetSymbols := map[string]bool{
		"dcr":          true,
		"btc":          true, // Add BTC
		"usdt.polygon": true, // Keep specific USDT variant
		"usdt.erc20":   true, // Add another USDT variant if needed
		"usdt.trc20":   true, // Add another USDT variant if needed
		// Add other desired symbols here
	}

	for mexcCoinUpper, coinData := range m.coinInfo {
		// Optimization: Skip coins not potentially part of targetSymbols
		// Note: This is a rough check, as we don't know the network suffix yet.
		if !(strings.Contains(strings.ToLower(mexcCoinUpper), "dcr") ||
			strings.Contains(strings.ToLower(mexcCoinUpper), "btc") ||
			strings.Contains(strings.ToLower(mexcCoinUpper), "usdt")) { // Adjust check based on targetSymbols
			// continue // Keep commented to map all enabled coins, uncomment for optimization
		}

		for _, netInfo := range coinData.NetworkList {
			if !netInfo.DepositEnable || !netInfo.WithdrawEnable {
				continue
			}
			dexSymbol := m.mapMEXCCoinNetworkToDEXSymbol(mexcCoinUpper, netInfo.Network)
			if dexSymbol == "" {
				continue
			}

			// Check if the generated DEX symbol is one we are interested in mapping
			// OR if we want to map all known symbols (remove this check)
			if !targetSymbols[dexSymbol] {
				// continue // Keep commented to map all enabled coins, uncomment for optimization
			}

			assetID, found := m.symbolToAssetID[dexSymbol]
			if !found {
				// fmt.Printf("PRINTF_DEBUG:  -> Failed: ...\n") // Remove Printf
				continue
			}
			// fmt.Printf("PRINTF_DEBUG:  -> Success: ...\n") // Remove Printf
			m.assetSymbolToCoin[dexSymbol] = mexcCoinUpper
			m.assetIDToCoin[assetID] = mexcCoinUpper
			foundSlice := false
			for _, existingID := range m.coinToAssetIDs[mexcCoinUpper] {
				if existingID == assetID {
					foundSlice = true
					break
				}
			}
			if !foundSlice {
				m.coinToAssetIDs[mexcCoinUpper] = append(m.coinToAssetIDs[mexcCoinUpper], assetID)
			}
			mappedCount++
		}
	}
	m.log.Infof("Updated MEXC symbol mappings: %d targeted asset IDs mapped.", mappedCount)
	if mappedCount < 2 {
		m.log.Warnf("Expected at least DCR and USDT.polygon mappings, but only found %d.", mappedCount)
	}

	// --- Add Temp Debug Logging ---
	logMsg := strings.Builder{}
	logMsg.WriteString("Final coinToAssetIDs map contents:")
	mapKeys := make([]string, 0, len(m.coinToAssetIDs))
	for k := range m.coinToAssetIDs {
		mapKeys = append(mapKeys, k)
	}
	sort.Strings(mapKeys)
	for _, k := range mapKeys {
		logMsg.WriteString(fmt.Sprintf("\n  %s: %v", k, m.coinToAssetIDs[k]))
	}
	m.log.Debugf(logMsg.String())
	// --- End Temp Debug Logging ---
}

// mapMEXCCoinNetworkToDEXSymbol attempts to construct a DEX symbol (lowercase)
func (m *mexc) mapMEXCCoinNetworkToDEXSymbol(mexcCoinUpper, mexcNetwork string) string {
	if mexcCoinUpper == "" || mexcNetwork == "" {
		return ""
	}

	dexBase := strings.ToLower(mexcCoinUpper)
	upNetwork := strings.ToUpper(mexcNetwork)

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

// --- Client Order ID Generation ---

// generateClientOrderID creates a unique client order ID for MEXC.
// Format: dcrdex-<prefix>-<timestamp>-<nonce>
func (m *mexc) generateClientOrderID() string {
	m.tradeIDNonceMtx.Lock()
	m.tradeIDNonce++
	nonce := m.tradeIDNonce
	m.tradeIDNonceMtx.Unlock()

	// Prefix is stored as hex bytes, convert back to string
	prefixStr := string(m.tradeIDNoncePrefix)

	// Timestamp in milliseconds
	timestamp := time.Now().UnixMilli()

	// Max length for clientOrderID is often limited (e.g., 32 chars for Binance). Check MEXC docs.
	// Let's assume 32 for now.
	// dcrdex- (7) + prefix (8) + - (1) + ts (13) + - (1) + nonce (?) = 30 + nonce
	// Keep nonce short.
	return fmt.Sprintf("dcrdex-%s-%d-%d", prefixStr, timestamp, nonce)
}

// stringToSatoshis converts a decimal string amount to uint64 satoshis using a conversion factor.
func (m *mexc) stringToSatoshis(decimalAmount string, conversionFactor float64) (uint64, error) {
	if decimalAmount == "" {
		return 0, nil
	}
	// Use big.Float for precision, explicitly setting precision
	const floatPrec = 128 // Use sufficient precision
	fltAmount, ok := new(big.Float).SetPrec(floatPrec).SetString(decimalAmount)
	if !ok {
		return 0, fmt.Errorf("invalid amount string: %q", decimalAmount)
	}
	fltFactor := big.NewFloat(conversionFactor).SetPrec(floatPrec)
	satAmountFlt := new(big.Float).SetPrec(floatPrec).Mul(fltAmount, fltFactor)

	// --- Keep the rest of the checks added previously ---
	if satAmountFlt == nil {
		return 0, fmt.Errorf("internal error: satAmountFlt is nil")
	}
	satAmountInt, accuracy := satAmountFlt.Int(nil)
	if satAmountInt == nil {
		return 0, fmt.Errorf("internal error: satAmountInt is nil after Int conversion")
	}
	if satAmountInt.Sign() < 0 {
		return 0, fmt.Errorf("amount %q results in negative satoshis", decimalAmount)
	}
	maxUint64Int := new(big.Int).SetUint64(math.MaxUint64)
	if satAmountInt.Cmp(maxUint64Int) > 0 {
		return 0, fmt.Errorf("amount %q exceeds uint64 range after conversion", decimalAmount)
	}
	if !satAmountInt.IsUint64() {
		return 0, fmt.Errorf("amount %q cannot be represented as uint64 (IsUint64 failed)", decimalAmount)
	}
	if accuracy != big.Exact && accuracy != big.Below {
		return 0, fmt.Errorf("cannot exactly convert %s to satoshis (Accuracy: %v)", decimalAmount, accuracy)
	}

	return satAmountInt.Uint64(), nil
}

// --- Trading Methods ---

// Trade executes a trade order on MEXC.
func (m *mexc) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (*Trade, error) {
	baseSymbol := dex.BipIDSymbol(baseID)   // Correct usage
	quoteSymbol := dex.BipIDSymbol(quoteID) // Correct usage
	m.log.Infof("Attempting MEXC trade: %s -> %s, Sell: %v, Rate: %d, Qty: %d, SubID: %d",
		baseSymbol, quoteSymbol, sell, rate, qty, subscriptionID)

	mexcSymbol, err := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if err != nil {
		return nil, fmt.Errorf("cannot map DEX IDs to MEXC symbol for trade: %w", err)
	}
	symInfo, err := m.getSymbolInfo(ctx, mexcSymbol)
	if err != nil {
		return nil, fmt.Errorf("cannot get MEXC symbol info for trade (%s): %w", mexcSymbol, err)
	}
	baseAssetInfo, baseAssetErr := asset.Info(baseID)
	quoteAssetInfo, quoteAssetErr := asset.Info(quoteID)
	if baseAssetErr != nil || quoteAssetErr != nil {
		return nil, fmt.Errorf("failed to get asset info for base %d or quote %d", baseID, quoteID)
	}
	baseFactor := float64(baseAssetInfo.UnitInfo.Conventional.ConversionFactor)
	quoteFactor := float64(quoteAssetInfo.UnitInfo.Conventional.ConversionFactor)

	// Use correct calc function and manual amount conversion
	conventionalRate := calc.ConventionalRateAlt(rate, uint64(baseFactor), uint64(quoteFactor)) // Correct usage
	conventionalQty := float64(qty) / baseFactor                                                // Manual conversion

	priceStr := strconv.FormatFloat(conventionalRate, 'f', symInfo.QuotePrecision, 64)
	qtyStr := strconv.FormatFloat(conventionalQty, 'f', symInfo.BaseAssetPrecision, 64)

	params := url.Values{}
	params.Set("symbol", mexcSymbol)
	if sell {
		params.Set("side", "SELL")
	} else {
		params.Set("side", "BUY")
	}
	params.Set("type", "LIMIT")
	params.Set("quantity", qtyStr)
	params.Set("price", priceStr)
	clientOrderID := m.generateClientOrderID()
	params.Set("newClientOrderId", clientOrderID)

	path := "/api/v3/order"
	var resp mexctypes.NewOrderResponse
	err = m.request(ctx, http.MethodPost, path, params, nil, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("MEXC new order request failed: %w", err)
	}

	// Create the initial libxc Trade struct to return
	trade := &Trade{
		ID:          resp.OrderID, // Use MEXC's OrderID
		Sell:        sell,
		Qty:         qty,
		Rate:        rate,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  0,     // Initially zero
		QuoteFilled: 0,     // Initially zero
		Complete:    false, // Initially false
	}

	// If there's a subscription, store info for WebSocket updates
	if subscriptionID != 0 {
		m.activeTradesMtx.Lock()
		// Store info needed to process WS updates and push to subscriber
		m.activeTrades[resp.OrderID] = &mexcTradeInfo{
			updaterID:   subscriptionID,
			baseID:      baseID,
			quoteID:     quoteID,
			sell:        sell,
			rate:        rate,
			qty:         qty,
			baseFilled:  0, // Initialize filled amounts
			quoteFilled: 0,
		}
		m.activeTradesMtx.Unlock()
		m.log.Debugf("Stored active trade info for MEXC OrderID %s (SubID %d)", resp.OrderID, subscriptionID)
	} else {
		m.log.Warnf("Trade placed without subscription ID for MEXC OrderID %s", resp.OrderID)
	}

	m.log.Infof("Successfully placed MEXC order %s (%s) for %s -> %s. Qty: %s, Price: %s",
		trade.ID, clientOrderID, baseSymbol, quoteSymbol, qtyStr, priceStr)
	return trade, nil
}

// getSymbolInfo retrieves SymbolInfo from the cached exchange info.
func (m *mexc) getSymbolInfo(ctx context.Context, mexcSymbol string) (*mexctypes.SymbolInfo, error) {
	info, err := m.getCachedExchangeInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("exchange info not available for symbol lookup")
	}

	for i := range info.Symbols {
		if info.Symbols[i].Symbol == mexcSymbol {
			return &info.Symbols[i], nil
		}
	}
	return nil, fmt.Errorf("symbol %q not found in MEXC exchange info", mexcSymbol)
}

// --- CEX Interface Method Placeholders ---

// CancelTrade placeholder (re-added)
func (m *mexc) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	m.log.Infof("Attempting to cancel MEXC order ID: %s (Market: %d/%d)", tradeID, baseID, quoteID)

	// 1. Map Base/Quote IDs to MEXC Symbol
	mexcSymbol, mapErr := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if mapErr != nil {
		// Log the error but attempt cancellation anyway if possible, as symbol is required.
		// If mapping fails, the cancellation request will likely fail at the API level.
		m.log.Errorf("Cannot map DEX IDs to MEXC symbol for cancel, cancellation likely to fail: %v", mapErr)
		// For robustness, we could potentially try to find the symbol associated
		// with the tradeID from an internal cache if we stored it, but let's proceed.
		return fmt.Errorf("cannot map DEX IDs to MEXC symbol for cancel: %w", mapErr)
	}

	// 2. Prepare API Request
	// Use DELETE /api/v3/order, requires orderId or origClientOrderId
	path := "/api/v3/order"
	params := url.Values{}
	params.Set("symbol", mexcSymbol) // Required by API
	params.Set("orderId", tradeID)
	// TODO: Consider allowing cancellation by clientOrderID if we store it

	// 3. Send Request
	// Response should be the details of the canceled order (mexctypes.Order)
	var cancelResp mexctypes.Order
	err := m.request(ctx, http.MethodDelete, path, params, nil, true, &cancelResp)
	if err != nil {
		// Handle errors, e.g., already filled/canceled (-2011: Unknown order sent.)
		var mexcErr *mexctypes.ErrorResponse
		if errors.As(err, &mexcErr) && mexcErr.Code == -2011 {
			m.log.Warnf("Attempted to cancel already filled/canceled/unknown MEXC order %s: %v", tradeID, err)
			// Order is already in a terminal state (or never existed). Treat as success.
			return nil
		}
		return fmt.Errorf("MEXC cancel order request failed for ID %s: %w", tradeID, err)
	}

	m.log.Infof("Successfully requested cancellation for MEXC order %s (Final status in response: %s)", tradeID, cancelResp.Status)
	// TODO: Should we update internal trade status based on cancelResp?
	return nil
}

// SubscribeMarket subscribes to order book updates for a given market.
func (m *mexc) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	m.log.Debugf("SubscribeMarket called for %d/%d", baseID, quoteID)

	// 1. Map IDs to MEXC symbol
	mexcSymbol, err := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("map error: %w", err)
	}

	// 2. Handle order book instance management
	m.booksMtx.Lock()
	book, exists := m.books[mexcSymbol]
	if exists {
		book.numSubscribers++
		m.log.Debugf("Incremented subscriber count for %s to %d", mexcSymbol, book.numSubscribers)
		m.booksMtx.Unlock()
		return nil // Already subscribed (by another caller), just increment count
	}

	// First subscriber for this specific market, create the book instance.
	m.log.Infof("First subscriber for %s, creating order book.", mexcSymbol)
	baseInfo, baseErr := asset.Info(baseID)
	quoteInfo, quoteErr := asset.Info(quoteID)
	if baseErr != nil || quoteErr != nil {
		m.booksMtx.Unlock()
		return fmt.Errorf("failed to get asset info for factors (base %d, quote %d)", baseID, quoteID)
	}

	newBook := newMEXCOrderBook(
		mexcSymbol,
		baseInfo.UnitInfo.Conventional.ConversionFactor,
		quoteInfo.UnitInfo.Conventional.ConversionFactor,
		m.log.SubLogger(fmt.Sprintf("Book-%s", mexcSymbol)),
		m.getDepthSnapshot,
	)
	newBook.numSubscribers = 1
	m.books[mexcSymbol] = newBook
	m.booksMtx.Unlock() // Unlock BEFORE potentially blocking network calls or starting goroutine

	// Start the book's run goroutine (manages sync state)
	go newBook.run(ctx, m.marketSubResyncChan)

	// 3. Handle WebSocket connection and subscription
	m.marketStreamMtx.Lock()
	defer m.marketStreamMtx.Unlock()

	if m.marketStream == nil {
		// First market subscription overall, establish connection
		m.marketStreamMtx.Unlock() // Unlock before calling connect which locks its own mutexes
		err = m.connectMarketStream(ctx, mexcSymbol)
		m.marketStreamMtx.Lock() // Re-lock after call returns
		if err != nil {
			// Need to clean up the book we added if connection failed
			m.booksMtx.Lock()
			delete(m.books, mexcSymbol)
			m.booksMtx.Unlock()
			newBook.stop() // Stop the run goroutine we started
			return fmt.Errorf("failed to establish initial market stream connection: %w", err)
		}
		// connectMarketStream already sent the subscription for this first symbol.
		m.log.Infof("Initial market stream connected and subscribed to %s", mexcSymbol)
	} else {
		// Connection exists, subscribe to this additional market
		err = m.subscribeToAdditionalMarket(ctx, mexcSymbol)
		if err != nil {
			// Need to clean up the book we added if subscription failed
			m.booksMtx.Lock()
			delete(m.books, mexcSymbol)
			m.booksMtx.Unlock()
			newBook.stop() // Stop the run goroutine we started
			return fmt.Errorf("failed to subscribe to additional market %s: %w", mexcSymbol, err)
		}
	}

	m.log.Infof("Successfully processed subscription request for MEXC market %s", mexcSymbol)
	return nil
}

// GetDepositAddress returns a deposit address for the given asset.
func (m *mexc) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	m.log.Debugf("Getting MEXC deposit address for asset ID %d", assetID)

	// 1. Map asset ID to MEXC Coin and Network
	m.mapMtx.RLock()
	mexcCoin, okCoin := m.assetIDToCoin[assetID]
	m.mapMtx.RUnlock()
	if !okCoin {
		return "", fmt.Errorf("asset ID %d not mapped to a MEXC coin", assetID)
	}

	dexSymbol := dex.BipIDSymbol(assetID)
	if dexSymbol == "" {
		return "", fmt.Errorf("could not determine DEX symbol for asset ID %d", assetID)
	}

	// Use the mapping logic (requires coinInfo loaded)
	m.coinInfoMtx.RLock()
	if m.coinInfo == nil {
		m.coinInfoMtx.RUnlock()
		_, err := m.getCachedCoinInfo(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get coin info to determine network for %s: %w", dexSymbol, err)
		}
		m.coinInfoMtx.RLock() // Re-acquire lock
	}
	_, mexcNetwork := m.findMEXCCoinAndNetwork(dexSymbol)
	m.coinInfoMtx.RUnlock()

	if mexcNetwork == "" {
		return "", fmt.Errorf("could not determine MEXC network for DEX symbol %s (Coin: %s)", dexSymbol, mexcCoin)
	}

	m.log.Debugf("Mapped asset %d (%s) to MEXC Coin %s, Network %s", assetID, dexSymbol, mexcCoin, mexcNetwork)

	// 2. Prepare API Request
	path := "/api/v3/capital/deposit/address"
	params := url.Values{}
	params.Set("coin", mexcCoin)
	params.Set("network", mexcNetwork)

	// 3. Send Request
	var resp mexctypes.DepositAddress
	err := m.request(ctx, http.MethodGet, path, params, nil, true, &resp)
	if err != nil {
		return "", fmt.Errorf("MEXC get deposit address request failed for %s (%s): %w", mexcCoin, mexcNetwork, err)
	}

	// 4. Return address (and handle tag)
	if resp.Address == "" {
		return "", fmt.Errorf("MEXC returned empty deposit address for %s (%s)", mexcCoin, mexcNetwork)
	}

	if resp.Tag != "" {
		m.log.Warnf("MEXC deposit address for %s (%s) requires a tag/memo: %s. Ensure user includes this.", mexcCoin, mexcNetwork, resp.Tag)
		// NOTE: Current libxc interface only returns address string.
	}

	m.log.Infof("Retrieved MEXC deposit address for %s (%s)", mexcCoin, mexcNetwork)
	return resp.Address, nil
}

// TradeStatus retrieves the current status of an order.
func (m *mexc) TradeStatus(ctx context.Context, tradeID string, baseID, quoteID uint32) (*Trade, error) {
	m.log.Debugf("Checking MEXC trade status for Order ID: %s (Market: %d/%d)", tradeID, baseID, quoteID)

	// 1. Map Base/Quote IDs to MEXC Symbol (needed for context/logging mainly)
	mexcSymbol, mapErr := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if mapErr != nil {
		m.log.Warnf("Could not map base/quote IDs to MEXC symbol for TradeStatus check: %v", mapErr)
		mexcSymbol = "UNKNOWN" // Proceed with caution if symbol is unknown
	}

	// 2. Prepare API Request
	path := "/api/v3/order"
	params := url.Values{}
	// MEXC requires symbol even when querying by orderId
	if mexcSymbol == "UNKNOWN" {
		return nil, fmt.Errorf("cannot check trade status without a valid market symbol (mapping failed for %d/%d)", baseID, quoteID)
	}
	params.Set("symbol", mexcSymbol)
	params.Set("orderId", tradeID)

	// 3. Send Request
	var orderStatus mexctypes.Order
	err := m.request(ctx, http.MethodGet, path, params, nil, true, &orderStatus)
	if err != nil {
		var mexcErr *mexctypes.ErrorResponse
		if errors.As(err, &mexcErr) && mexcErr.Code == -2013 { // -2013: Order does not exist.
			return nil, fmt.Errorf("MEXC order %s not found: %w", tradeID, err)
		}
		return nil, fmt.Errorf("MEXC get order status request failed for ID %s: %w", tradeID, err)
	}

	// 4. Parse Response into libxc.Trade format
	baseAssetInfo, baseAssetErr := asset.Info(baseID)
	quoteAssetInfo, quoteAssetErr := asset.Info(quoteID)
	if baseAssetErr != nil || quoteAssetErr != nil {
		return nil, fmt.Errorf("failed to get asset info for parsing trade status (base %d or quote %d)", baseID, quoteID)
	}

	baseDecimals := inferDecimals(baseAssetInfo.UnitInfo.Conventional.ConversionFactor)
	quoteDecimals := inferDecimals(quoteAssetInfo.UnitInfo.Conventional.ConversionFactor)
	baseFactor := math.Pow10(baseDecimals)
	quoteFactor := math.Pow10(quoteDecimals)

	baseFilled, bfErr := m.stringToSatoshis(orderStatus.ExecutedQuantity, baseFactor)
	quoteFilled, qfErr := m.stringToSatoshis(orderStatus.CummulativeQuoteQty, quoteFactor)
	if bfErr != nil || qfErr != nil {
		return nil, fmt.Errorf("error parsing filled amounts for order %s: baseErr=%v, quoteErr=%v", tradeID, bfErr, qfErr)
	}

	complete := false
	switch orderStatus.Status {
	case "FILLED", "CANCELED", "PARTIALLY_CANCELED":
		complete = true
	case "NEW", "PARTIALLY_FILLED":
		complete = false
	default:
		m.log.Warnf("Unrecognized MEXC order status '%s' for order %s", orderStatus.Status, tradeID)
		complete = false
	}

	// Retrieve original Rate/Qty from activeTrades if available
	var origRate, origQty uint64
	m.activeTradesMtx.RLock()
	if tradeInfo, exists := m.activeTrades[tradeID]; exists {
		origRate = tradeInfo.rate
		origQty = tradeInfo.qty
	}
	m.activeTradesMtx.RUnlock()
	if origQty == 0 { // If not found in active trades, try parsing from response
		var parseErr error
		origQty, parseErr = m.stringToSatoshis(orderStatus.OrigQuantity, baseFactor)
		if parseErr != nil {
			m.log.Warnf("Could not determine original quantity for trade %s: %v", tradeID, parseErr)
		}
		// Attempting to parse rate requires factors, might be inaccurate if factors changed
		conventionalRate, _ := strconv.ParseFloat(orderStatus.Price, 64)
		origRate = calc.MessageRateAlt(conventionalRate, uint64(baseFactor), uint64(quoteFactor))
	}

	trade := &Trade{
		ID:          tradeID,
		Sell:        orderStatus.Side == "SELL",
		Qty:         origQty,
		Rate:        origRate,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
		Complete:    complete,
	}

	m.log.Debugf("Retrieved MEXC trade status for %s: Status=%s, BaseFilled=%d, QuoteFilled=%d, Complete=%v",
		tradeID, orderStatus.Status, baseFilled, quoteFilled, complete)

	return trade, nil
}

// Withdraw initiates a withdrawal from MEXC.
func (m *mexc) Withdraw(ctx context.Context, assetID uint32, atoms uint64, address string) (withdrawalID string, err error) {
	m.log.Infof("Attempting MEXC withdrawal: Asset %d, Amount: %d atoms, Address: %s", assetID, atoms, address)

	// 1. Map asset ID to MEXC Coin and Network
	m.mapMtx.RLock()
	mexcCoin, okCoin := m.assetIDToCoin[assetID]
	m.mapMtx.RUnlock()
	if !okCoin {
		return "", fmt.Errorf("asset ID %d not mapped to a MEXC coin for withdrawal", assetID)
	}
	dexSymbol := dex.BipIDSymbol(assetID)
	if dexSymbol == "" {
		return "", fmt.Errorf("could not determine DEX symbol for asset ID %d", assetID)
	}
	m.coinInfoMtx.RLock()
	// Ensure coinInfo is loaded before calling findMEXCCoinAndNetwork
	if m.coinInfo == nil {
		m.coinInfoMtx.RUnlock()
		_, fetchErr := m.getCachedCoinInfo(ctx)
		if fetchErr != nil {
			return "", fmt.Errorf("failed to get coin info for withdrawal: %w", fetchErr)
		}
		m.coinInfoMtx.RLock() // Re-acquire lock
	}
	_, mexcNetwork := m.findMEXCCoinAndNetwork(dexSymbol)
	m.coinInfoMtx.RUnlock()
	if mexcNetwork == "" {
		return "", fmt.Errorf("could not determine MEXC network for withdrawal of %s (Coin: %s)", dexSymbol, mexcCoin)
	}

	// 2. Convert atomic amount to conventional string with correct precision
	precision, pErr := m.getCoinPrecision(ctx, assetID)
	if pErr != nil {
		return "", fmt.Errorf("failed to get precision for withdrawal amount (%s): %w", mexcCoin, pErr)
	}
	convFactor := math.Pow10(precision)
	conventionalAmount := float64(atoms) / convFactor
	amountStr := strconv.FormatFloat(conventionalAmount, 'f', precision, 64)

	// 3. Prepare API Request
	path := "/api/v3/capital/withdraw/apply"
	params := url.Values{}
	params.Set("coin", mexcCoin)
	params.Set("network", mexcNetwork)
	params.Set("address", address)
	params.Set("amount", amountStr)
	// TODO: Add memo/tag handling if needed.
	// Example: if tag != "" { params.Set("memo", tag) }

	// 4. Send Request
	var resp mexctypes.WithdrawApplyResponse
	err = m.request(ctx, http.MethodPost, path, params, nil, true, &resp)
	if err != nil {
		return "", fmt.Errorf("MEXC withdraw request failed for %s (%s): %w", mexcCoin, mexcNetwork, err)
	}

	// 5. Return withdrawal ID
	if resp.WithdrawID == "" {
		return "", fmt.Errorf("MEXC withdraw request succeeded but did not return a withdrawal ID")
	}

	m.log.Infof("Successfully initiated MEXC withdrawal for %s (%s), Amount: %s. Withdrawal ID: %s",
		mexcCoin, mexcNetwork, amountStr, resp.WithdrawID)
	return resp.WithdrawID, nil
}

// UnsubscribeMarket unsubscribes from order book updates.
func (m *mexc) UnsubscribeMarket(baseID, quoteID uint32) error {
	m.log.Debugf("Unsubscribing from MEXC market %d/%d", baseID, quoteID)

	// 1. Map IDs to MEXC symbol
	mexcSymbol, err := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("cannot map DEX IDs to MEXC symbol for unsubscribe: %w", err)
	}

	// 2. Decrement subscriber count / remove book
	m.booksMtx.Lock()
	book, exists := m.books[mexcSymbol]
	if !exists {
		m.booksMtx.Unlock()
		m.log.Warnf("Attempted to unsubscribe from non-existent book %s", mexcSymbol)
		return nil // Not subscribed, nothing to do
	}

	if book.numSubscribers > 1 {
		book.numSubscribers--
		m.log.Debugf("Decremented subscriber count for %s to %d", mexcSymbol, book.numSubscribers)
		m.booksMtx.Unlock()
		return nil // Still other subscribers
	}

	// Last subscriber, remove book and potentially send unsubscribe message
	delete(m.books, mexcSymbol)
	numRemainingBooks := len(m.books)
	m.booksMtx.Unlock()

	m.log.Infof("Last subscriber for %s, removing book (remaining books: %d).", mexcSymbol, numRemainingBooks)

	// Stop the book's sync goroutine
	book.stop()

	// 3. Send unsubscribe message if WebSocket is connected
	m.marketStreamMtx.Lock()
	defer m.marketStreamMtx.Unlock()

	if m.marketStream == nil || m.marketStream.IsDown() {
		m.log.Warnf("Cannot send unsubscribe for %s, market stream not connected or nil.", mexcSymbol)
		return nil // Can't send if not connected
	}

	// Marshal and send
	channel := fmt.Sprintf("spot@public.increase.depth.v3.api@%s", mexcSymbol)
	req := mexctypes.WsRequest{
		Method: "UNSUBSCRIPTION",
		Params: []string{channel},
	}
	reqBytes, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		m.log.Errorf("Failed to marshal market unsubscription request for %s: %v", mexcSymbol, marshalErr)
		return nil // Return nil to indicate local cleanup succeeded despite marshal failure.
	}
	m.log.Debugf("[MarketWS] Sending UNSUBSCRIPTION for %s", mexcSymbol)
	err = m.marketStream.SendRaw(reqBytes)
	if err != nil {
		m.log.Errorf("Failed to send MEXC market unsubscription for %s: %v", mexcSymbol, err)
		return fmt.Errorf("failed to send market unsubscription: %w", err)
	}

	m.log.Infof("Sent unsubscription request for %s", mexcSymbol)
	return nil
}

// connectMarketStream establishes the initial market data WebSocket connection,
// sends the first subscription, and starts the proactive PING goroutine.
// It should only be called by SubscribeMarket when m.marketStream is nil.
func (m *mexc) connectMarketStream(ctx context.Context, firstMexcSymbol string) error {
	m.log.Infof("[MarketWS] Establishing initial connection for symbol %s...", firstMexcSymbol)

	// Create WsConn instance
	marketLogger := m.log.SubLogger("MarketWS")
	marketWsCfg := &comms.WsCfg{
		Logger: marketLogger, URL: mexcWsURL, PingWait: 75 * time.Second,
		RawHandler: m.handleMarketRawMessage, ReconnectSync: m.resubscribeMarkets,
		ConnectEventFunc: m.handleMarketConnectEvent, EchoPingData: true,
	}
	conn, err := comms.NewWsConn(marketWsCfg)
	if err != nil {
		return fmt.Errorf("failed to create market WebSocket connection: %w", err)
	}
	m.marketStream = conn

	// Launch WsConn manager goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		connCtx := ctx
		wgConn, err := m.marketStream.Connect(connCtx)
		if err != nil {
			if connCtx.Err() == nil {
				m.log.Errorf("[MarketWS] Connection manager exited with error: %v", err)
			} else {
				m.log.Infof("[MarketWS] Connection manager exited: %v", connCtx.Err())
			}
		} else {
			m.log.Infof("[MarketWS] Connection manager exited cleanly.")
		}
		if wgConn != nil {
			wgConn.Wait()
			m.log.Infof("[MarketWS] Connect waitgroup finished.")
		}
	}()

	// Launch PING goroutine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()
		m.log.Infof("[MarketWS] Starting proactive PING ticker.")
		defer m.log.Infof("[MarketWS] Stopping proactive PING ticker.")
		pingCtx := ctx
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-pingTicker.C:
				m.marketStreamMtx.RLock()
				stream := m.marketStream
				m.marketStreamMtx.RUnlock()
				if stream != nil && !stream.IsDown() {
					m.log.Debugf("Sending proactive WebSocket PING (Market Stream)")
					pingJSON := `{"method":"PING"}`
					if err := stream.SendRaw([]byte(pingJSON)); err != nil {
						m.log.Errorf("Failed to send proactive WebSocket PING (Market Stream): %v", err)
					}
				}
			}
		}
	}()

	time.Sleep(2 * time.Second)
	if m.marketStream.IsDown() {
		return fmt.Errorf("[MarketWS] connection failed shortly after initiation")
	}

	// Send initial subscription
	m.log.Infof("[MarketWS] Sending initial subscription for %s", firstMexcSymbol)
	channel := fmt.Sprintf("spot@public.increase.depth.v3.api@%s", firstMexcSymbol)
	req := mexctypes.WsRequest{Method: "SUBSCRIPTION", Params: []string{channel}}
	reqBytes, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal initial market subscription request: %w", marshalErr)
	}
	err = m.marketStream.SendRaw(reqBytes)
	if err != nil {
		return fmt.Errorf("failed to send initial market subscription for %s: %w", firstMexcSymbol, err)
	}

	m.log.Infof("[MarketWS] Initial connection and subscription process initiated for %s.", firstMexcSymbol)
	return nil
}

// handleMarketConnectEvent handles connection status changes.
func (m *mexc) handleMarketConnectEvent(status comms.ConnectionStatus) {
	// Commented out: Underlying comms.WsConn may log similar reconnect events.
	// m.log.Infof("Market WS Connection Status Change: %v", status)
	if status != comms.Connected {
		m.booksMtx.RLock()
		for _, book := range m.books {
			book.synced.Store(false)
		}
		m.booksMtx.RUnlock()
	}
	// ReconnectSync (resubscribeMarkets) is called automatically by WsConn after Connected status
}

// subscribeToAdditionalMarket sends a subscription message for a new market
func (m *mexc) subscribeToAdditionalMarket(ctx context.Context, mexcSymbol string) error {
	m.log.Infof("[MarketWS] Subscribing to additional market: %s", mexcSymbol)

	if m.marketStream == nil || m.marketStream.IsDown() {
		// This check is slightly redundant if caller holds lock and checks,
		// but provides safety if called incorrectly.
		return fmt.Errorf("market stream not connected when trying to subscribe to %s", mexcSymbol)
	}

	channel := fmt.Sprintf("spot@public.increase.depth.v3.api@%s", mexcSymbol)
	req := mexctypes.WsRequest{
		Method: "SUBSCRIPTION",
		Params: []string{channel},
	}
	reqBytes, marshalErr := json.Marshal(req)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal market subscription request for %s: %w", mexcSymbol, marshalErr)
	}

	m.log.Debugf("[MarketWS] Sending SUBSCRIPTION for %s", mexcSymbol)
	err := m.marketStream.SendRaw(reqBytes)
	if err != nil {
		// Log the error, WsConn might handle reconnect, but the sub likely failed for now.
		m.log.Errorf("Failed to send market subscription for %s: %v", mexcSymbol, err)
		return fmt.Errorf("failed to send market subscription for %s: %w", mexcSymbol, err)
	}

	m.log.Infof("[MarketWS] Sent subscription request for additional market %s", mexcSymbol)
	return nil
}

// handleMarketRawMessage parses raw byte messages from the WebSocket.
func (m *mexc) handleMarketRawMessage(msgBytes []byte) {
	// Try to unmarshal as the base message type first (for Channel, Symbol, Ping, Data)
	var baseMsg mexctypes.WsMessage
	if err := json.Unmarshal(msgBytes, &baseMsg); err != nil {
		m.log.Errorf("[MarketWS] Failed to unmarshal base market WS message: %v, msg: %s", err, string(msgBytes))
		return
	}

	// Handle server-sent PING { "ping": timestamp }
	if baseMsg.Ping != 0 {
		m.log.Tracef("[MarketWS] Received MEXC Server Ping object: %d", baseMsg.Ping)
		pong := mexctypes.WsPong{Pong: baseMsg.Ping}
		pongBytes, _ := json.Marshal(pong)
		if errPong := m.marketStream.SendRaw(pongBytes); errPong != nil {
			m.log.Errorf("[MarketWS] Failed to send MEXC Pong response: %v", errPong)
		}
		return
	}

	// Dispatch based on known channels
	switch baseMsg.Channel {
	case "spot@public.increase.depth.v3.api":
		if len(baseMsg.Data) == 0 || string(baseMsg.Data) == "null" {
			m.log.Warnf("[MarketWS] Received depth update message with missing/null data field: %s", string(msgBytes))
			return
		}
		m.handleDepthUpdate(&baseMsg)
		return // Handled depth update
	}

	// If Channel is empty or not recognized, check for other known message types
	var genericResp struct {
		ID   interface{} `json:"id"`
		Code int         `json:"code"`
		Msg  string      `json:"msg"`
	}
	if err := json.Unmarshal(msgBytes, &genericResp); err == nil {
		if genericResp.Code == 0 && genericResp.Msg == "PONG" {
			m.log.Tracef("[MarketWS] Received server PONG acknowledgement")
			return
		}
		if genericResp.Code == 0 && strings.Contains(strings.ToLower(genericResp.Msg), "success") {
			m.log.Debugf("[MarketWS] Received MEXC WS Success Response/Confirmation: %s", string(msgBytes))
			return
		}
		m.log.Warnf("[MarketWS] Received unknown structured WS message: %s", string(msgBytes))
		return
	}

	m.log.Warnf("[MarketWS] Received unhandled/unparseable raw market message: %s", string(msgBytes))
}

// handleDepthUpdate parses the data part of the depth update and forwards it.
func (m *mexc) handleDepthUpdate(msg *mexctypes.WsMessage) {
	// ... function body ...
}

// resubscribeMarkets sends subscription messages for all currently tracked markets.
func (m *mexc) resubscribeMarkets() {
	m.booksMtx.RLock()
	defer m.booksMtx.RUnlock()

	if len(m.books) == 0 {
		return
	}
	m.log.Infof("Resubscribing to %d markets after reconnect...", len(m.books))
	for symbol := range m.books {
		// Reuse SubscribeMarket logic without locking/book creation
		channel := fmt.Sprintf("spot@public.increase.depth.v3.api@%s", symbol)
		req := mexctypes.WsRequest{
			Method: "SUBSCRIPTION",
			Params: []string{channel},
		}
		reqBytes, err := json.Marshal(req)
		if err != nil {
			m.log.Errorf("[Resubscribe] Failed to marshal subscription for %s: %v", symbol, err)
			continue
		}
		// Use SendRaw
		if err := m.marketStream.SendRaw(reqBytes); err != nil {
			m.log.Errorf("[Resubscribe] Failed to send subscription for %s: %v", symbol, err)
		}
	}
}

// --- CEX Interface Method Placeholders ---

// Book method implementation (Retrieves full book from local cache)
func (m *mexc) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	mexcSymbol, mapErr := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if mapErr != nil {
		return nil, nil, fmt.Errorf("failed to map market for Book: %w", mapErr)
	}

	m.booksMtx.RLock()
	bookInstance, exists := m.books[mexcSymbol]
	m.booksMtx.RUnlock()
	if !exists {
		return nil, nil, fmt.Errorf("order book for %s not managed", mexcSymbol)
	}

	bookInstance.mtx.RLock()
	defer bookInstance.mtx.RUnlock()

	if !bookInstance.synced.Load() {
		return nil, nil, ErrUnsyncedOrderbook
	}

	// Get asset info for conversion factors
	baseInfo, baseErr := asset.Info(baseID)
	quoteInfo, quoteErr := asset.Info(quoteID)
	if baseErr != nil || quoteErr != nil {
		return nil, nil, fmt.Errorf("failed to get asset info for book conversion (base %d, quote %d): %w, %w", baseID, quoteID, baseErr, quoteErr)
	}
	baseFactor := float64(baseInfo.UnitInfo.Conventional.ConversionFactor)
	quoteFactor := float64(quoteInfo.UnitInfo.Conventional.ConversionFactor)
	if baseFactor == 0 || quoteFactor == 0 {
		return nil, nil, fmt.Errorf("invalid conversion factor (base: %.0f, quote: %.0f) for market %d/%d", baseFactor, quoteFactor, baseID, quoteID)
	}

	// Use the snap() method from orderbook.go
	rawBids, rawAsks := bookInstance.book.snap() // Get bids/asks snapshot

	// Convert rawBids ([]*obEntry uint64) to buys ([]*core.MiniOrder float64)
	buys = make([]*core.MiniOrder, len(rawBids))
	for i, bid := range rawBids {
		conventionalRate := (float64(bid.rate) / quoteFactor) * baseFactor // Convert atomic rate (q/b) to conventional rate
		conventionalQty := float64(bid.qty) / baseFactor                   // Convert atomic base qty to conventional qty
		buys[i] = &core.MiniOrder{
			Rate:      conventionalRate, // Assign float64
			Qty:       conventionalQty,  // Assign float64
			QtyAtomic: bid.qty,          // Add atomic qty
			MsgRate:   bid.rate,         // Add atomic rate
		}
	}

	// Convert rawAsks ([]*obEntry uint64) to sells ([]*core.MiniOrder float64)
	sells = make([]*core.MiniOrder, len(rawAsks))
	for i, ask := range rawAsks {
		conventionalRate := (float64(ask.rate) / quoteFactor) * baseFactor // Convert atomic rate (q/b) to conventional rate
		conventionalQty := float64(ask.qty) / baseFactor                   // Convert atomic base qty to conventional qty
		sells[i] = &core.MiniOrder{
			Rate:      conventionalRate, // Assign float64
			Qty:       conventionalQty,  // Assign float64
			QtyAtomic: ask.qty,          // Add atomic qty
			MsgRate:   ask.rate,         // Add atomic rate
		}
	}

	// Books are typically sorted best rate first. Ensure MiniOrder slices are sorted.
	// The local orderbook implementation likely keeps them sorted.

	return buys, sells, nil
}

// VWAP method implementation (using local orderbook)
func (m *mexc) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	mexcSymbol, mapErr := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if mapErr != nil {
		return 0, 0, false, fmt.Errorf("failed to map market: %w", mapErr)
	}
	m.booksMtx.RLock()
	bookInstance, exists := m.books[mexcSymbol]
	m.booksMtx.RUnlock()
	if !exists {
		return 0, 0, false, fmt.Errorf("order book for %s not managed", mexcSymbol)
	}

	bookInstance.mtx.RLock()
	defer bookInstance.mtx.RUnlock()
	if !bookInstance.synced.Load() {
		return 0, 0, false, ErrUnsyncedOrderbook
	}

	// Use the local book's vwap method
	vwap, extrema, filled = bookInstance.book.vwap(!sell, qty) // Pass !sell for bids flag
	// Local vwap doesn't return error according to its definition in libxc/orderbook.go
	return vwap, extrema, filled, nil // Added return
}

// MidGap method implementation (using local orderbook)
func (m *mexc) MidGap(baseID, quoteID uint32) uint64 {
	mexcSymbol, mapErr := m.mapDEXIDsToMEXCSymbol(baseID, quoteID)
	if mapErr != nil {
		m.log.Errorf("Failed to map market for MidGap: %v", mapErr)
		return 0
	}
	m.booksMtx.RLock()
	bookInstance, exists := m.books[mexcSymbol]
	m.booksMtx.RUnlock()
	if !exists {
		m.log.Warnf("MidGap requested for unmanaged book %s", mexcSymbol)
		return 0
	}

	bookInstance.mtx.RLock()
	defer bookInstance.mtx.RUnlock()
	if !bookInstance.synced.Load() {
		return 0
	}

	// Use the local book's midGap method
	return bookInstance.book.midGap() // Added return
}

// ConfirmDeposit checks the status of a deposit by querying deposit history.
func (m *mexc) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	// NOTE: Matching primarily by TxID as Address might not be unique or consistently available.
	// m.log.Debugf("Confirming MEXC deposit for Asset: %d, TxID: %s", deposit.AssetID, deposit.TxID)

	// 1. Map asset ID to MEXC Coin
	m.mapMtx.RLock()
	mexcCoin, okCoin := m.assetIDToCoin[deposit.AssetID]
	m.mapMtx.RUnlock()
	if !okCoin {
		m.log.Warnf("Cannot confirm deposit for asset %d: not mapped to a MEXC coin.", deposit.AssetID)
		return false, 0
	}

	// 2. Prepare API Request for Deposit History
	// Fetch recent deposits (e.g., last 10-50?) as filtering by address/txid isn't directly supported.
	// Fetch deposits within a reasonable time window (e.g., last 24 hours)
	path := "/api/v3/capital/deposit/hisrec"
	params := url.Values{}
	params.Set("coin", mexcCoin)
	// params.Set("status", "6") // Filter for success? API docs unclear if this works reliably for confirmation. Let's fetch all recent.
	// Set a time window, e.g., last 24 hours. startTime/endTime are in milliseconds.
	endTime := time.Now().UnixMilli()
	startTime := time.Now().Add(-24 * time.Hour).UnixMilli() // Check last 24 hours
	params.Set("startTime", strconv.FormatInt(startTime, 10))
	params.Set("endTime", strconv.FormatInt(endTime, 10))
	params.Set("limit", "50") // Fetch up to 50 recent deposits

	var history []*mexctypes.DepositHistoryRecord
	err := m.request(ctx, http.MethodGet, path, params, nil, true, &history)
	if err != nil {
		m.log.Errorf("Failed to fetch MEXC deposit history for %s: %v", mexcCoin, err)
		return false, 0
	}

	// 3. Iterate and Match Deposit
	for _, record := range history {
		// m.log.Tracef("Checking deposit record: Coin=%s, Amount=%s, Address=%s, TxID=%s, Status=%d",
		// 	record.Coin, record.Amount, record.Address, record.TxID, record.Status)

		// Primary match criteria: TxID if available.
		// Address matching is removed as DepositData doesn't have it and TxID is more reliable.
		// addressMatch := record.Address == deposit.Address // Removed
		txidMatch := deposit.TxID != "" && record.TxID == deposit.TxID

		if txidMatch {
			// m.log.Debugf("Found potential deposit match via TxID: %s, Status=%d", record.TxID, record.Status)
			// Check status: 6 = Credited/Success
			if record.Status == 6 {
				// 4. Parse Amount and Return Success
				precision, pErr := m.getCoinPrecision(ctx, deposit.AssetID)
				if pErr != nil {
					m.log.Errorf("Failed to get precision for confirmed deposit amount (%s): %v", mexcCoin, pErr)
					continue // Skip this record if precision fails
				}
				convFactor := math.Pow10(precision)
				confirmedAmount, amountErr := m.stringToSatoshis(record.Amount, convFactor)
				if amountErr != nil {
					m.log.Errorf("Failed to parse confirmed deposit amount %q for %s: %v", record.Amount, mexcCoin, amountErr)
					continue // Skip this record if amount parsing fails
				}
				m.log.Infof("Confirmed MEXC deposit: Asset=%d, Amount=%d atoms, Address=%s, TxID=%s", deposit.AssetID, confirmedAmount, record.Address, record.TxID)
				return true, confirmedAmount
			}
			// If status is not success, keep checking other records in case of duplicates/re-deposits?
			// For now, assume first match dictates the status for that address/txid combo.
			m.log.Debugf("Deposit match found but status is not success (%d).", record.Status)
			// Potentially return false earlier if a non-success match is found?
			// Let's continue checking in case a later record shows success.
		}
	}

	// 5. No Confirmed Match Found
	// Updated log message to reflect matching by TxID
	m.log.Debugf("No confirmed deposit found matching Asset %d, TxID %s", deposit.AssetID, deposit.TxID)
	return false, 0
}

// ConfirmWithdrawal checks the status of a withdrawal.
// NOTE: This requires polling.
func (m *mexc) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (amount uint64, txid string, err error) {
	m.log.Debugf("Checking MEXC withdrawal status for ID: %s (Asset: %d)", withdrawalID, assetID)

	m.mapMtx.RLock()
	mexcCoin, okCoin := m.assetIDToCoin[assetID]
	m.mapMtx.RUnlock() // Unlock after reading coin mapping
	if !okCoin {
		// It's better to return an error if the asset isn't mapped.
		return 0, "", fmt.Errorf("asset ID %d not mapped to a MEXC coin", assetID)
	}

	path := "/api/v3/capital/withdraw/history"
	params := url.Values{}
	params.Set("withdrawId", withdrawalID)
	// Potentially filter by coin? API docs are ambiguous if withdrawId is globally unique.
	// params.Set("coin", mexcCoin)

	var history []*mexctypes.WithdrawHistoryRecord
	err = m.request(ctx, http.MethodGet, path, params, nil, true, &history)
	if err != nil {
		// Don't return specific error types directly here, let the caller handle ErrWithdrawalPending
		return 0, "", fmt.Errorf("MEXC get withdrawal history request failed for ID %s: %w", withdrawalID, err)
	}

	if len(history) == 0 {
		m.log.Debugf("Withdrawal ID %s for %s not found in recent history. Treating as pending.", withdrawalID, mexcCoin)
		return 0, "", ErrWithdrawalPending // Treat as pending if not found yet
	}
	if len(history) > 1 {
		// This case is unlikely if withdrawId is unique, but log just in case.
		m.log.Warnf("MEXC withdrawal history returned multiple records for single ID %s. Using first record.", withdrawalID)
	}
	record := history[0]

	// Ensure the record found actually matches the requested coin, in case withdrawId isn't globally unique.
	if record.Coin != mexcCoin {
		m.log.Warnf("Withdrawal ID %s found, but associated coin %s does not match requested asset %d (%s). Treating as pending/not found.", withdrawalID, record.Coin, assetID, mexcCoin)
		return 0, "", ErrWithdrawalPending
	}

	// Status mapping based on general CEX patterns and previous assumptions:
	// Need to confirm exact meaning of MEXC statuses if possible.
	// 0:"Applying", 1:"Applied", 2:"Processing", 3:"Waiting", 4:"Processing", 5:"Awaiting Confirmation",
	// 6:"Success", 7:"Failed", 8:"Rejected", 9:"Cancelled", 10:"Awaiting Transfer"
	switch record.Status {
	case 6: // SUCCESS
		m.log.Infof("MEXC withdrawal %s for %s confirmed. TxID: %s", withdrawalID, mexcCoin, record.TxID)
		precision, pErr := m.getCoinPrecision(ctx, assetID)
		if pErr != nil {
			// Return error if precision lookup fails for confirmed withdrawal
			return 0, "", fmt.Errorf("failed to get precision for confirmed withdrawal amount (%s): %w", mexcCoin, pErr)
		}
		convFactor := math.Pow10(precision)
		confirmedAmount, amountErr := m.stringToSatoshis(record.Amount, convFactor)
		if amountErr != nil {
			return 0, "", fmt.Errorf("failed to parse confirmed withdrawal amount %q for %s: %w", record.Amount, mexcCoin, amountErr)
		}
		// Return amount, txid, and nil error for success
		return confirmedAmount, record.TxID, nil
	case 0, 1, 2, 3, 4, 5, 10: // Various pending/processing states
		m.log.Tracef("MEXC withdrawal %s for %s is still pending/processing (Status: %d)", withdrawalID, mexcCoin, record.Status)
		return 0, "", ErrWithdrawalPending
	case 7, 8, 9: // FAILED / REJECTED / CANCELLED
		m.log.Warnf("MEXC withdrawal %s for %s failed/rejected/canceled (Status: %d)", withdrawalID, mexcCoin, record.Status)
		// Return a specific error indicating the withdrawal failed permanently.
		return 0, "", fmt.Errorf("withdrawal %s failed/rejected/canceled (status %d)", withdrawalID, record.Status)
	default:
		m.log.Warnf("MEXC withdrawal %s for %s has unknown status: %d", withdrawalID, mexcCoin, record.Status)
		// Return an error for unknown status
		return 0, "", fmt.Errorf("withdrawal %s has unknown status %d", withdrawalID, record.Status)
	}
}

// ... Other existing placeholders/implementations ...

// inferDecimals helper function (Add back)
func inferDecimals(factor uint64) int {
	if factor == 0 {
		return 0
	}
	decimals := 0
	for factor > 1 {
		if factor%10 != 0 {
			break
		}
		factor /= 10
		decimals++
	}
	return decimals
}

// --- User Data Stream (Listen Key) ---

const listenKeyRefreshInterval = 45 * time.Minute // Keep alive slightly less than 60 min expiry

// getListenKey fetches a new listen key from MEXC.
func (m *mexc) getListenKey(ctx context.Context) (string, error) {
	// Prevent multiple concurrent requests
	if !m.listenKeyRequested.CompareAndSwap(false, true) {
		// Another request is in progress, wait a short time and check the stored key
		// This avoids hammering the API if multiple goroutines need the key at once.
		select {
		case <-time.After(2 * time.Second):
			key := m.listenKey.Load().(string)
			if key != "" {
				return key, nil
			}
			return "", fmt.Errorf("listen key request already in progress, timed out waiting")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	defer m.listenKeyRequested.Store(false)

	m.log.Infof("Requesting new MEXC listen key...")
	path := "/api/v3/userDataStream"
	var resp mexctypes.ListenKeyResponse

	// Use POST request
	err := m.request(ctx, http.MethodPost, path, nil, nil, true, &resp)
	if err != nil {
		return "", fmt.Errorf("MEXC get listen key request failed: %w", err)
	}

	if resp.ListenKey == "" {
		return "", fmt.Errorf("MEXC returned empty listen key")
	}

	m.log.Infof("Obtained new MEXC listen key.") // Don't log the key itself
	m.listenKey.Store(resp.ListenKey)

	// Reset the keepalive timer
	m.resetListenKeyTimer()

	return resp.ListenKey, nil
}

// keepAliveListenKey sends a keepalive ping for the current listen key.
func (m *mexc) keepAliveListenKey(ctx context.Context) error {
	m.listenKeyMtx.Lock() // Ensure only one keepalive runs at a time
	defer m.listenKeyMtx.Unlock()

	key := m.listenKey.Load().(string)
	if key == "" {
		return fmt.Errorf("no listen key available to keep alive")
	}

	m.log.Debugf("Sending MEXC listen key keepalive...")
	path := "/api/v3/userDataStream"
	params := url.Values{}
	params.Set("listenKey", key)

	// Use PUT request - expecting empty success response {}
	err := m.request(ctx, http.MethodPut, path, params, nil, true, nil)
	if err != nil {
		m.log.Errorf("MEXC listen key keepalive failed: %v", err)
		// If keepalive fails (e.g., key expired), clear stored key and signal reconnect
		m.listenKey.Store("")
		m.signalReconnect() // Signal the user stream connector to get a new key
		return fmt.Errorf("MEXC listen key keepalive failed: %w", err)
	}

	m.log.Debugf("MEXC listen key keepalive successful.")
	// Reset the timer after successful keepalive
	m.resetListenKeyTimer()
	return nil
}

// deleteListenKey informs the server that the listen key is no longer needed.
func (m *mexc) deleteListenKey(ctx context.Context) error {
	key := m.listenKey.Load().(string)
	if key == "" {
		return nil // No key to delete
	}
	m.log.Infof("Deleting MEXC listen key...")
	path := "/api/v3/userDataStream"
	params := url.Values{}
	params.Set("listenKey", key)

	// Use DELETE request - expecting empty success response {}
	err := m.request(ctx, http.MethodDelete, path, params, nil, true, nil)
	m.listenKey.Store("") // Clear local key regardless of API call success
	if m.listenKeyRefresh != nil {
		m.listenKeyRefresh.Stop() // Stop the timer
	}
	if err != nil {
		// Log error but don't necessarily fail shutdown
		m.log.Errorf("MEXC delete listen key request failed: %v", err)
		return fmt.Errorf("MEXC delete listen key request failed: %w", err)
	}
	m.log.Infof("Deleted MEXC listen key.")
	return nil
}

// resetListenKeyTimer stops the current timer (if any) and starts a new one.
func (m *mexc) resetListenKeyTimer() {
	m.listenKeyMtx.Lock()
	defer m.listenKeyMtx.Unlock()

	if m.listenKeyRefresh != nil {
		m.listenKeyRefresh.Stop()
	}
	// Create a new timer that will call keepAliveListenKey after the interval
	m.listenKeyRefresh = time.AfterFunc(listenKeyRefreshInterval, func() {
		// Use a background context for the keepalive initiated by the timer
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.keepAliveListenKey(ctx)
		if err != nil {
			m.log.Errorf("Listen key keepalive timer failed: %v", err)
			// Reconnect should be signalled by keepAliveListenKey itself on failure
		}
	})
}

// signalReconnect sends a non-blocking signal to the reconnect channel.
func (m *mexc) signalReconnect() {
	select {
	case m.reconnectChan <- struct{}{}: // Signal if possible
	default: // If channel is full, a reconnect is already pending
		m.log.Debugf("Reconnect signal already pending for user data stream.")
	}
}

// --- User Data Stream Connection & Handling ---

// connectUserStream manages the user data websocket connection lifecycle.
func (m *mexc) connectUserStream(ctx context.Context) {
	m.log.Infof("ENTER connectUserStream: Managing user data stream connection...")
	defer m.log.Infof("EXIT connectUserStream")

	var keyRefreshWg sync.WaitGroup
	keyRefreshCtx, cancelKeyRefresh := context.WithCancel(ctx)
	defer cancelKeyRefresh()

connectLoop:
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		key, err := m.getListenKey(ctx)
		if err != nil {
			m.log.Errorf("Failed to get initial listen key: %v. Retrying in %v...", err, reconnectInterval)
			// Check context before sleeping
			select {
			case <-time.After(reconnectInterval):
				continue connectLoop
			case <-ctx.Done():
				return
			}
		}

		wsURL := fmt.Sprintf("%s?listenKey=%s", mexcWsURL, key)
		m.userDataStream.UpdateURL(wsURL) // Assumes WsConn has UpdateURL method
		m.log.Infof("User data stream URL set.")

		keyRefreshWg.Add(1)
		go func() {
			defer keyRefreshWg.Done()
			m.listenKeyMaintainer(keyRefreshCtx)
		}()

		m.log.Infof("Connecting user data stream...")
		connWg, connErr := m.userDataStream.Connect(keyRefreshCtx)
		if connErr != nil {
			m.log.Errorf("User data stream initial Connect failed: %v. Retrying...", connErr)
			cancelKeyRefresh()
			keyRefreshWg.Wait()
			keyRefreshCtx, cancelKeyRefresh = context.WithCancel(ctx)
			// Check context before sleeping
			select {
			case <-time.After(reconnectInterval):
				continue connectLoop
			case <-ctx.Done():
				return
			}
		}
		m.log.Infof("User data stream connected.")

		// Add a small delay before sending subscriptions
		time.Sleep(1 * time.Second)

		// Add explicit subscriptions for the user data channels (if needed by MEXC)
		// MEXC user stream might not require explicit subscriptions like Binance
		// If they are needed, re-add the subscription loop here.
		m.log.Infof("MEXC User Stream does not require explicit channel subscriptions.")

		// Start a ticker for proactive WebSocket PINGs
		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		// RawHandler handles messages, listen key keepalive handled elsewhere
	wsLoop: // Label for the select loop
		for {
			select {
			case <-ctx.Done():
				m.log.Infof("Main context canceled, shutting down user stream.")
				cancelKeyRefresh()
				break wsLoop // Exit inner loop
			case <-m.reconnectChan:
				m.log.Warnf("Received reconnect signal for user data stream. Reconnecting...")
				cancelKeyRefresh()
				break wsLoop // Exit inner loop
			// case <-proactiveReconnect.C: // This was removed as potentially problematic
			// 	m.log.Infof("Proactively reconnecting WebSocket after %v to prevent server timeout", proactiveReconnectInterval)
			// 	cancelKeyRefresh()
			// 	break wsLoop // Exit inner loop
			case <-pingTicker.C: // Send proactive WebSocket PING
				// Only send ping if the stream is actually connected
				if m.userDataStream != nil && !m.userDataStream.IsDown() {
					m.log.Debugf("Sending proactive WebSocket PING (User Stream)")
					pingJSON := `{"method":"PING"}`
					if err := m.userDataStream.SendRaw([]byte(pingJSON)); err != nil {
						m.log.Errorf("Failed to send proactive WebSocket PING (User Stream): %v", err)
						// Consider triggering reconnect if ping fails?
					}
				}
			}
		}

		keyRefreshWg.Wait()
		if connWg != nil {
			connWg.Wait()
		}
		keyRefreshCtx, cancelKeyRefresh = context.WithCancel(ctx)
		// Loop continues after cleanup
	}
}

// listenKeyMaintainer waits for the context to cancel and cleans up the listen key.
func (m *mexc) listenKeyMaintainer(ctx context.Context) {
	m.log.Infof("[UserWS] Starting listen key maintainer (HTTP keepalive timer managed separately).")
	defer m.log.Infof("[UserWS] Stopping listen key maintainer.")

	// The keepalive logic is handled by the time.AfterFunc timer in resetListenKeyTimer
	// which calls keepAliveListenKey. This goroutine just needs to wait for shutdown.
	<-ctx.Done()

	// Stop the timer on shutdown
	m.listenKeyMtx.Lock()
	if m.listenKeyRefresh != nil {
		m.listenKeyRefresh.Stop()
	}
	m.listenKeyMtx.Unlock()

	// Attempt to delete the key on clean shutdown
	delCtx, cancelDel := context.WithTimeout(context.Background(), 10*time.Second)
	m.deleteListenKey(delCtx)
	cancelDel()
}

// handleUserConnectEvent handles connection status changes for the user stream.
func (m *mexc) handleUserConnectEvent(status comms.ConnectionStatus) {
	// Commented out: Underlying comms.WsConn may log similar reconnect events.
	// m.log.Infof("User WS Connection Status Change: %v", status)
	// DO NOT signal reconnect here. WsConn handles temporary disconnects.
	// Reconnect should only be triggered by listen key expiry (via keepAlive failure)
	// or main context cancellation.
}

// handleUserDataRawMessage parses raw byte messages from the User Data Stream.
func (m *mexc) handleUserDataRawMessage(msgBytes []byte) {
	// m.log.Tracef("[UserWS] Raw message: %s", string(msgBytes))

	// First check for server PING {"ping": ts}
	var pingCheck struct {
		Ping int64 `json:"ping"`
	}
	if err := json.Unmarshal(msgBytes, &pingCheck); err == nil && pingCheck.Ping > 0 {
		m.log.Debugf("[UserWS] Received server PING: %d", pingCheck.Ping)
		pong := mexctypes.WsPong{Pong: pingCheck.Ping}
		pongBytes, err := json.Marshal(pong)
		if err == nil {
			if err := m.userDataStream.SendRaw(pongBytes); err != nil {
				m.log.Errorf("[UserWS] Failed to send PONG response: %v", err)
			} else {
				m.log.Debugf("[UserWS] Sent PONG response: %d", pingCheck.Ping)
			}
		}
		return
	}

	// Try to unmarshal into the base event type to get the event string 'e'
	var baseEvent mexctypes.WsMessage // Reusing this struct for EventType ('e') field
	err := json.Unmarshal(msgBytes, &baseEvent)
	if err != nil {
		m.log.Errorf("[UserWS] Failed to unmarshal base user event: %v, payload: %s", err, string(msgBytes))
		return
	}

	if baseEvent.EventType == "" {
		// Could be keepalive response {"msg":"PONG", ...} or other non-event message
		m.log.Tracef("[UserWS] Received user data message without event type: %s", string(msgBytes))
		// Should we check for PONG msg here too?
		// If we send PING {"method":"PING"}, MEXC might reply with {"msg":"PONG"}?
		// Let's check for that explicitly.
		var pongAckCheck struct {
			Msg string `json:"msg"`
		}
		if json.Unmarshal(msgBytes, &pongAckCheck) == nil && pongAckCheck.Msg == "PONG" {
			m.log.Tracef("[UserWS] Received PONG acknowledgement message.")
			return
		}
		return
	}

	// Dispatch based on event type ('e')
	switch baseEvent.EventType {
	case "spot@private.orders.v3.api": // Order updates (e.g., new, filled, canceled)
		m.handleOrderUpdate(msgBytes) // Pass raw bytes for specific unmarshalling
	case "spot@private.deals.v3.api": // Trade execution updates
		m.handleDealUpdate(msgBytes) // Pass raw bytes for specific unmarshalling
	case "spot@private.account.v3.api": // Balance updates
		m.handleBalanceUpdate(msgBytes) // Call new handler
	default:
		m.log.Warnf("[UserWS] Received unhandled user data event type '%s': %s", baseEvent.EventType, string(msgBytes))
	}
}

// handleOrderUpdate processes order update messages.
func (m *mexc) handleOrderUpdate(payload json.RawMessage) {
	var baseMsg mexctypes.WsMessage // Need outer message for Symbol
	if err := json.Unmarshal(payload, &baseMsg); err != nil {
		m.log.Errorf("Failed to unmarshal outer WsMessage for order update: %v, data: %s", err, string(payload))
		return
	}

	var orderUpdate mexctypes.WsOrderUpdateData
	err := json.Unmarshal(baseMsg.Data, &orderUpdate) // Unmarshal the inner 'd' field
	if err != nil {
		m.log.Errorf("Failed to unmarshal WsOrderUpdateData ('d' field): %v, data: %s", err, string(baseMsg.Data))
		return
	}

	// Log using available fields from WsOrderUpdateData (p, q, a, f, fc, S, o, s, i, c, m, O, T)
	// and Symbol (s) from the outer WsMessage.
	m.log.Infof("Received MEXC Order Update: ID=%s, ClientID=%s, Symbol=%s, Side=%s, Type=%s, Status=%s, Price=%s, Qty=%s, Amount=%s, Fee=%s %s, Maker=%v, OrderTime=%d, TxTime=%d",
		orderUpdate.OrderID,         // i
		orderUpdate.ClientOrderID,   // c
		baseMsg.Symbol,              // s (from outer message)
		orderUpdate.Side,            // S
		orderUpdate.Type,            // o
		orderUpdate.Status,          // s
		orderUpdate.Price,           // p
		orderUpdate.Quantity,        // q
		orderUpdate.Amount,          // a
		orderUpdate.Fee,             // f
		orderUpdate.FeeCurrency,     // fc
		orderUpdate.IsMaker,         // m
		orderUpdate.OrderTime,       // O
		orderUpdate.TransactionTime, // T
	)

	// Find the tracked trade info using MEXC Order ID
	m.activeTradesMtx.RLock()
	tradeInfo, exists := m.activeTrades[orderUpdate.OrderID]
	if !exists {
		m.activeTradesMtx.RUnlock()
		// If not found by OrderID, maybe it's the clientOrderID? Unlikely for updates, but possible.
		// Let's assume OrderID is the primary key for updates.
		// This could happen if the update arrives after the trade was considered complete locally or before it was stored.
		// m.log.Warnf("Received order update for untracked MEXC OrderID: %s", orderUpdate.OrderID) // Can be noisy
		return
	}
	// Copy needed info under read lock
	subID := tradeInfo.updaterID
	baseID := tradeInfo.baseID
	quoteID := tradeInfo.quoteID
	originalRate := tradeInfo.rate
	originalQty := tradeInfo.qty
	m.activeTradesMtx.RUnlock()

	// Cannot determine filled amounts from WsOrderUpdateData struct definition.
	// Fill info comes from deal messages or REST polling.
	// Remove the placeholder baseFilled/quoteFilled assignment.
	// baseFilled := uint64(0) // Removed
	// quoteFilled := uint64(0) // Removed

	// Determine completion status based SOLELY on the status field of this message
	complete := false
	switch orderUpdate.Status { // Use Status field 's'
	case "FILLED", "CANCELED", "PARTIALLY_CANCELED": // Note: Binance uses EXPIRED, REJECTED. Check MEXC docs for final states. Assume these are final.
		complete = true
	case "NEW", "PARTIALLY_FILLED":
		complete = false
	default:
		m.log.Warnf("Unrecognized MEXC order status '%s' in update for order %s", orderUpdate.Status, orderUpdate.OrderID)
		complete = false // Treat unrecognized status as not complete
	}

	// If the status indicates completion, we notify and remove from tracking.
	// We CANNOT provide accurate fill info from this message alone.
	if complete {
		// Fetch potentially updated fill amounts before notifying.
		m.activeTradesMtx.Lock() // Lock for potential delete
		finalTradeInfo, stillExists := m.activeTrades[orderUpdate.OrderID]
		var finalBaseFilled, finalQuoteFilled uint64
		if stillExists {
			// Use the latest known fill amounts stored internally (updated by deals)
			finalBaseFilled = finalTradeInfo.baseFilled
			finalQuoteFilled = finalTradeInfo.quoteFilled
			delete(m.activeTrades, orderUpdate.OrderID)
			m.log.Debugf("Removed completed/canceled trade %s from active tracking (via order update).", orderUpdate.OrderID)
		}
		m.activeTradesMtx.Unlock()

		if stillExists { // Only notify if we actually removed it
			tradeUpdate := &Trade{
				ID:          orderUpdate.OrderID,
				Sell:        orderUpdate.Side == "SELL",
				Qty:         originalQty,  // Return original requested qty
				Rate:        originalRate, // Return original requested rate
				BaseID:      baseID,
				QuoteID:     quoteID,
				BaseFilled:  finalBaseFilled,  // Use latest known fill
				QuoteFilled: finalQuoteFilled, // Use latest known fill
				Complete:    true,
			}
			m.notifySubscriber(subID, tradeUpdate)
		}
	} else {
		// If not complete based on status, we don't send an update from here.
		// Updates with fill info should come from handleDealUpdate.
		m.log.Tracef("Non-terminal order status '%s' received for %s, no update sent from handleOrderUpdate.", orderUpdate.Status, orderUpdate.OrderID)
	}

	// -- Old notification logic removed as it lacked fill info --
}

// handleDealUpdate processes trade execution messages and updates trade state.
func (m *mexc) handleDealUpdate(payload json.RawMessage) {
	var baseMsg mexctypes.WsMessage // Need outer message for Symbol
	if err := json.Unmarshal(payload, &baseMsg); err != nil {
		m.log.Errorf("Failed to unmarshal outer WsMessage for deal update: %v, data: %s", err, string(payload))
		return
	}

	var dealUpdate mexctypes.WsDealUpdateData
	err := json.Unmarshal(baseMsg.Data, &dealUpdate) // Unmarshal the inner 'd' field
	if err != nil {
		m.log.Errorf("Failed to unmarshal WsDealUpdateData ('d' field): %v, data: %s", err, string(baseMsg.Data))
		return
	}

	// Log the deal details using corrected field names (S, T, f, fc, q, p, a, m, d, i)
	// Symbol comes from outer message.
	m.log.Infof("Received MEXC Deal Update: OrderID=%s, DealID=%s, Symbol=%s, Side=%s, Price=%s, Qty=%s, Amount=%s, Fee=%s %s, Maker=%v, Time=%d",
		dealUpdate.OrderID,         // i
		dealUpdate.DealID,          // d
		baseMsg.Symbol,             // s (from outer message)
		dealUpdate.Side,            // S
		dealUpdate.Price,           // p
		dealUpdate.Quantity,        // q
		dealUpdate.Amount,          // a
		dealUpdate.Fee,             // f
		dealUpdate.FeeCurrency,     // fc
		dealUpdate.IsMaker,         // m
		dealUpdate.TransactionTime, // T
	)

	// 1. Find associated tradeInfo in m.activeTrades using dealUpdate.OrderID.
	m.activeTradesMtx.Lock() // Lock for read and potential update/delete
	defer m.activeTradesMtx.Unlock()

	tradeInfo, exists := m.activeTrades[dealUpdate.OrderID]
	if !exists {
		// Deal message for an order we are no longer tracking (already completed/canceled?)
		// m.log.Warnf("Received deal update for untracked MEXC OrderID: %s", dealUpdate.OrderID)
		return
	}

	// 2. Get asset info for conversion
	baseID := tradeInfo.baseID
	quoteID := tradeInfo.quoteID
	baseAssetInfo, baseAssetErr := asset.Info(baseID)
	quoteAssetInfo, quoteAssetErr := asset.Info(quoteID)
	if baseAssetErr != nil || quoteAssetErr != nil {
		m.log.Errorf("Failed to get asset info for processing deal update %s (DealID %s): baseErr=%v, quoteErr=%v",
			dealUpdate.OrderID, dealUpdate.DealID, baseAssetErr, quoteAssetErr)
		return
	}
	baseDecimals := inferDecimals(baseAssetInfo.UnitInfo.Conventional.ConversionFactor)
	quoteDecimals := inferDecimals(quoteAssetInfo.UnitInfo.Conventional.ConversionFactor)
	baseFactor := math.Pow10(baseDecimals)
	quoteFactor := math.Pow10(quoteDecimals)

	// 3. Calculate base and quote amounts for this specific deal.
	baseDealt, bdErr := m.stringToSatoshis(dealUpdate.Quantity, baseFactor) // q is base qty
	quoteDealt, qdErr := m.stringToSatoshis(dealUpdate.Amount, quoteFactor) // a is quote qty
	if bdErr != nil || qdErr != nil {
		m.log.Errorf("Error parsing dealt amounts from deal update %s (DealID %s): baseErr=%v, quoteErr=%v",
			dealUpdate.OrderID, dealUpdate.DealID, bdErr, qdErr)
		return // Cannot process update without amounts
	}

	// TODO: Handle fees (dealUpdate.Fee, dealUpdate.FeeCurrency)
	// Need to convert fee amount, potentially requires fetching fee asset precision.
	// For now, fee processing is skipped.

	// 4. Atomically update tradeInfo.baseFilled/quoteFilled
	tradeInfo.baseFilled += baseDealt
	tradeInfo.quoteFilled += quoteDealt

	// 5. Check if trade is complete
	// Use >= for robustness in case fill slightly exceeds requested qty
	complete := tradeInfo.baseFilled >= tradeInfo.qty

	// 6. Create notification Trade struct with CUMULATIVE filled amounts
	tradeUpdate := &Trade{
		ID:          dealUpdate.OrderID,
		Sell:        tradeInfo.sell, // Use stored side
		Qty:         tradeInfo.qty,  // Original requested qty
		Rate:        tradeInfo.rate, // Original requested rate
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  tradeInfo.baseFilled,  // Use updated cumulative fill
		QuoteFilled: tradeInfo.quoteFilled, // Use updated cumulative fill
		Complete:    complete,
		// TODO: Add timestamp from deal? (dealUpdate.TransactionTime)
		// TODO: Add fee info?
	}

	// 7. Remove from activeTrades if complete
	if complete {
		delete(m.activeTrades, dealUpdate.OrderID)
		m.log.Debugf("Trade %s completed via deal update (DealID %s), removed from active tracking.", dealUpdate.OrderID, dealUpdate.DealID)
	}

	// 8. Notify subscriber (needs to be done outside the lock? No, map read is safe)
	m.notifySubscriber(tradeInfo.updaterID, tradeUpdate)
}

// handleBalanceUpdate processes account balance update messages from WebSocket.
func (m *mexc) handleBalanceUpdate(payload json.RawMessage) {
	var baseMsg mexctypes.WsMessage // Need outer message for timestamp etc if needed later
	if err := json.Unmarshal(payload, &baseMsg); err != nil {
		m.log.Errorf("Failed to unmarshal outer WsMessage for balance update: %v, data: %s", err, string(payload))
		return
	}

	var balanceUpdate mexctypes.WsAccountUpdateData
	err := json.Unmarshal(baseMsg.Data, &balanceUpdate) // Unmarshal the inner 'd' field
	if err != nil {
		m.log.Errorf("Failed to unmarshal WsAccountUpdateData ('d' field): %v, data: %s", err, string(baseMsg.Data))
		return
	}

	m.log.Debugf("Received MEXC Balance Update: Asset=%s, Available=%s, Locked=%s",
		balanceUpdate.Asset, balanceUpdate.Available, balanceUpdate.Locked)

	// Map MEXC asset to DEX asset ID(s)
	mexcCoinUpper := strings.ToUpper(balanceUpdate.Asset)
	m.mapMtx.RLock() // Lock for reading coinToAssetIDs
	assetIDs, coinKnown := m.coinToAssetIDs[mexcCoinUpper]
	m.mapMtx.RUnlock()

	if !coinKnown || len(assetIDs) == 0 {
		// m.log.Tracef("Balance update for unmapped/unknown asset: %s", mexcCoinUpper)
		return // Ignore updates for assets we don't track
	}

	// Get precision for conversion (use first mapped ID, assuming same precision)
	// Use background context as this is processing a background event.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	precision, pErr := m.getCoinPrecision(ctx, assetIDs[0])
	if pErr != nil {
		m.log.Errorf("Failed to get precision for balance update (%s): %v", mexcCoinUpper, pErr)
		return
	}
	convFactor := math.Pow10(precision)

	// Parse available and locked amounts
	avail, availErr := m.stringToSatoshis(balanceUpdate.Available, convFactor)
	locked, lockedErr := m.stringToSatoshis(balanceUpdate.Locked, convFactor)
	if availErr != nil || lockedErr != nil {
		m.log.Errorf("Error parsing balance amounts from update (%s): availErr=%v, lockedErr=%v", mexcCoinUpper, availErr, lockedErr)
		return
	}

	newBalance := &ExchangeBalance{
		Available: avail,
		Locked:    locked,
	}

	// Update internal cache and broadcast for each mapped DEX asset ID
	m.balancesMtx.Lock()
	defer m.balancesMtx.Unlock()

	for _, assetID := range assetIDs {
		// Check if the balance actually changed before broadcasting
		currentBalance, exists := m.balances[assetID]
		if !exists || *currentBalance != *newBalance {
			m.balances[assetID] = newBalance
			m.log.Debugf("Updated balance via WS for AssetID %d: Avail=%d, Locked=%d", assetID, newBalance.Available, newBalance.Locked)

			// Broadcast the update
			bncUpdate := &BalanceUpdate{
				AssetID: assetID,
				Balance: newBalance, // Send pointer to the new balance stored in the map? Or a copy? Copy is safer.
			}
			bncUpdateCopy := *bncUpdate
			bncUpdateCopy.Balance = &ExchangeBalance{Available: newBalance.Available, Locked: newBalance.Locked} // Make a copy of balance too
			m.broadcast(&bncUpdateCopy)

		} else {
			// m.log.Tracef("Balance update for AssetID %d received, but no change detected.", assetID)
		}
	}
}

// notifySubscriber sends the trade update to the correct channel.
func (m *mexc) notifySubscriber(subID int, tradeUpdate *Trade) {
	m.tradeUpdaterMtx.RLock()
	subscriberChan, chanExists := m.tradeUpdaters[subID]
	m.tradeUpdaterMtx.RUnlock()

	if chanExists {
		select {
		case subscriberChan <- tradeUpdate:
			m.log.Tracef("Sent trade update notification for OrderID %s to SubID %d", tradeUpdate.ID, subID)
		default:
			m.log.Warnf("Trade update channel full for SubID %d, dropping update for OrderID %s", subID, tradeUpdate.ID)
		}
	} else {
		m.log.Warnf("No subscriber channel found for SubID %d (OrderID %s update)", subID, tradeUpdate.ID)
	}
}

// --- SubscribeTradeUpdates Implementation ---
func (m *mexc) SubscribeTradeUpdates() (updates <-chan *Trade, unsubscribe func(), subscriptionID int) {
	m.tradeUpdaterMtx.Lock()
	defer m.tradeUpdaterMtx.Unlock()

	m.tradeUpdateCounter++
	subscriptionID = m.tradeUpdateCounter
	updateChan := make(chan *Trade, 10) // Buffered channel
	m.tradeUpdaters[subscriptionID] = updateChan

	unsubscribe = func() {
		m.tradeUpdaterMtx.Lock()
		defer m.tradeUpdaterMtx.Unlock()
		delete(m.tradeUpdaters, subscriptionID)
		m.log.Debugf("Unsubscribed trade updates for subscription ID %d", subscriptionID)
	}

	m.log.Debugf("Created trade update subscription ID %d", subscriptionID)
	// TODO: Ensure user data stream is active

	return updateChan, unsubscribe, subscriptionID
}

// getCoinPrecision helper function (Add back)
// findMEXCCoinAndNetwork attempts to find the corresponding MEXC coin symbol (uppercase)
// and network string based on a DEX asset symbol (lowercase, e.g., "btc", "usdt.erc20").
// Assumes necessary read locks (coinInfoMtx) are held by the caller.
func (m *mexc) findMEXCCoinAndNetwork(dexSymbol string) (coin, network string) {
	parts := strings.Split(dexSymbol, ".")
	dexBase := parts[0]
	dexNet := ""
	if len(parts) > 1 {
		dexNet = parts[1]
	}

	mexcCoinGuess := strings.ToUpper(dexBase)
	// NOTE: This function assumes the m.coinInfoMtx read lock is held by the caller!
	coinData, exists := m.coinInfo[mexcCoinGuess]
	if !exists {
		return "", ""
	}

	if dexNet == "" { // Native asset case
		for _, netInfo := range coinData.NetworkList {
			if netInfo.Network == mexcCoinGuess { // e.g., Network "BTC" for Coin "BTC"
				return mexcCoinGuess, mexcCoinGuess
			}
		}
		return "", "" // Native network not found for this coin
	}

	// Token case
	mexcNetworkGuess := ""
	switch strings.ToLower(dexNet) {
	case "erc20":
		mexcNetworkGuess = "ERC20"
	case "trc20":
		mexcNetworkGuess = "TRC20"
	case "bep20", "bsc":
		mexcNetworkGuess = "BEP20(BSC)"
	case "sol":
		mexcNetworkGuess = "SOLANA"
	case "matic", "polygon":
		mexcNetworkGuess = "MATIC"
	default:
		mexcNetworkGuess = strings.ToUpper(dexNet)
	}

	// Verify the guessed network exists for the coin
	for _, netInfo := range coinData.NetworkList {
		if netInfo.Network == mexcNetworkGuess {
			return mexcCoinGuess, mexcNetworkGuess
		}
	}

	return "", "" // Network not found for this coin
}
func (m *mexc) getCoinPrecision(ctx context.Context, assetID uint32) (int, error) {
	m.mapMtx.RLock()
	mexcCoin, okCoin := m.assetIDToCoin[assetID]
	m.mapMtx.RUnlock()
	if !okCoin {
		return -1, fmt.Errorf("asset ID %d not mapped to a MEXC coin", assetID)
	}

	m.coinInfoMtx.RLock()
	coinDetails, hasCoinDetails := m.coinInfo[mexcCoin]
	var precision int = -1
	if hasCoinDetails {
		dexSymbol := dex.BipIDSymbol(assetID)
		_, mexcNetwork := m.findMEXCCoinAndNetwork(dexSymbol)
		if mexcNetwork != "" {
			for _, netInfo := range coinDetails.NetworkList {
				if netInfo.Network == mexcNetwork {
					parts := strings.Split(netInfo.WithdrawMin, ".")
					if len(parts) == 2 {
						precision = len(parts[1])
					} else {
						precision = 0
					}
					break
				}
			}
		}
	}
	m.coinInfoMtx.RUnlock()

	if precision == -1 {
		info, infoErr := m.getCachedExchangeInfo(ctx)
		if infoErr == nil {
			for _, sym := range info.Symbols {
				if strings.ToUpper(sym.BaseAsset) == mexcCoin {
					precision = sym.BaseAssetPrecision
					break
				} else if strings.ToUpper(sym.QuoteAsset) == mexcCoin {
					precision = sym.QuoteAssetPrecision
					break
				}
			}
		}
	}

	if precision == -1 {
		precision = 8 // Final fallback
		m.log.Warnf("Could not reliably determine precision for MEXC coin %q, using default %d.", mexcCoin, precision)
		return precision, fmt.Errorf("precision not found for %s, using default", mexcCoin)
	}
	return precision, nil
}

// ... findMEXCCoinAndNetwork ...

// --- Order Book Management ---

// getDepthSnapshot fetches the order book snapshot via REST API.
func (m *mexc) getDepthSnapshot(ctx context.Context, symbol string) (*mexctypes.DepthResponse, error) {
	m.log.Debugf("Fetching order book snapshot for %s", symbol)
	path := "/api/v3/depth"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", "1000")

	var resp mexctypes.DepthResponse
	err := m.request(ctx, http.MethodGet, path, params, nil, false, &resp)
	if err != nil {
		return nil, fmt.Errorf("failed to get depth snapshot for %s: %w", symbol, err)
	}
	return &resp, nil
}

// newMEXCOrderBook creates a new order book manager for a market.
func newMEXCOrderBook(
	mktSymbol string,
	baseFactor, quoteFactor uint64,
	log dex.Logger,
	getSnapshotFunc func(ctx context.Context, symbol string) (*mexctypes.DepthResponse, error),
) *mexcOrderBook {
	return &mexcOrderBook{
		mktSymbol:   mktSymbol,
		baseFactor:  baseFactor,
		quoteFactor: quoteFactor,
		log:         log,
		getSnapshot: getSnapshotFunc,
		book:        newOrderBook(),
		updateQueue: make(chan *mexctypes.WsDepthUpdateData, 256), // Buffered queue
		syncChan:    make(chan struct{}),                          // Unclosed initially
		stopChan:    make(chan struct{}),                          // For stopping the run goroutine
	}
}

// run starts the synchronization process for the order book.
func (ob *mexcOrderBook) run(ctx context.Context, resyncChan chan<- string) {
	ob.log.Infof("Starting order book run loop for %s", ob.mktSymbol)
	defer ob.log.Infof("Exiting order book run loop for %s", ob.mktSymbol)

	// Initial sync attempt
	if !ob.syncOrderbook(ctx) {
		ob.log.Errorf("Initial order book sync failed for %s", ob.mktSymbol)
		// If initial sync fails, we might rely on reconnects/resync signals
	}

	for {
		select {
		case <-ctx.Done(): // Main context cancelled
			return
		case <-ob.stopChan: // Explicit stop signal for this book
			return
		case update, ok := <-ob.updateQueue:
			if !ok {
				ob.log.Infof("Update queue closed for %s, exiting run loop.", ob.mktSymbol)
				return
			}
			ob.processUpdate(update, resyncChan)
		}
	}
}

// stop signals the run goroutine to exit.
func (ob *mexcOrderBook) stop() {
	close(ob.stopChan)
}

// syncOrderbook fetches a snapshot and processes the initial state.
func (ob *mexcOrderBook) syncOrderbook(ctx context.Context) bool {
	ob.log.Infof("Attempting to sync order book for %s...", ob.mktSymbol)
	ob.synced.Store(false)

	// Drain queue of any updates received before snapshot fetch started
drainLoop:
	for {
		select {
		case <-ob.updateQueue:
		default:
			break drainLoop
		}
	}

	snapshot, err := ob.getSnapshot(ctx, ob.mktSymbol)
	if err != nil {
		ob.log.Errorf("Error getting orderbook snapshot for %s: %v", ob.mktSymbol, err)
		return false
	}

	bids, asks, err := ob.convertDepthEntries(snapshot.Bids, snapshot.Asks)
	if err != nil {
		ob.log.Errorf("Error converting snapshot entries for %s: %v", ob.mktSymbol, err)
		return false
	}

	snapshotVersion := uint64(snapshot.LastUpdateID)

	ob.mtx.Lock()
	ob.book.clear()
	ob.book.update(bids, asks)
	ob.lastUpdateID = snapshotVersion
	ob.mtx.Unlock()

	ob.log.Infof("Successfully processed snapshot for %s (Version: %d). Book Synced.", ob.mktSymbol, snapshotVersion)
	ob.synced.Store(true)

	// Signal completion
	select {
	case <-ob.syncChan:
		// Already closed, maybe reopen if re-syncing?
		ob.syncChan = make(chan struct{})
	default:
		// Still open, close it now.
	}
	close(ob.syncChan)

	return true
}

// processUpdate handles an incoming WebSocket depth update.
func (ob *mexcOrderBook) processUpdate(update *mexctypes.WsDepthUpdateData, resyncChan chan<- string) {
	updateVersion, err := strconv.ParseUint(update.Version, 10, 64)
	if err != nil {
		ob.log.Errorf("Failed to parse update version '%s' for %s: %v. Requesting resync.", update.Version, ob.mktSymbol, err)
		ob.requestResync(resyncChan)
		return
	}

	ob.mtx.Lock()
	defer ob.mtx.Unlock()

	if !ob.synced.Load() {
		return // Drop if not synced
	}

	if updateVersion <= ob.lastUpdateID {
		return // Stale or duplicate
	}

	if updateVersion > ob.lastUpdateID+1 {
		ob.log.Warnf("Gap detected for %s: last update ID %d, received %d. Requesting resync.", ob.mktSymbol, ob.lastUpdateID, updateVersion)
		ob.requestResync(resyncChan)
		return
	}

	bids, asks, err := ob.convertDepthEntries(update.Bids, update.Asks)
	if err != nil {
		ob.log.Errorf("Error converting update entries for %s (v%d): %v. Requesting resync.", ob.mktSymbol, updateVersion, err)
		ob.requestResync(resyncChan)
		return
	}
	ob.book.update(bids, asks)
	ob.lastUpdateID = updateVersion
}

// requestResync marks the book as unsynced and signals for a resync.
func (ob *mexcOrderBook) requestResync(resyncChan chan<- string) {
	ob.synced.Store(false)
	select {
	case resyncChan <- ob.mktSymbol:
		ob.log.Infof("Resync requested for %s", ob.mktSymbol)
	default:
		ob.log.Warnf("Resync channel full for market %s, resync likely already pending.", ob.mktSymbol)
	}
}

// convertDepthEntries converts MEXC depth entries to local obEntry.
func (ob *mexcOrderBook) convertDepthEntries(mexcBids, mexcAsks [][2]json.Number) (bids, asks []*obEntry, err error) {
	convert := func(updates [][2]json.Number) ([]*obEntry, error) {
		convertedUpdates := make([]*obEntry, 0, len(updates))
		for _, update := range updates {
			priceFlt, pErr := update[0].Float64()
			qtyFlt, qErr := update[1].Float64()
			if pErr != nil || qErr != nil {
				return nil, fmt.Errorf("error parsing price (%v) or qty (%v) from json.Number", pErr, qErr)
			}
			rate := calc.MessageRateAlt(priceFlt, ob.baseFactor, ob.quoteFactor)
			qty := uint64(qtyFlt * float64(ob.baseFactor))
			convertedUpdates = append(convertedUpdates, &obEntry{
				rate: rate,
				qty:  qty,
			})
		}
		return convertedUpdates, nil
	}
	bids, err = convert(mexcBids)
	if err != nil {
		return nil, nil, fmt.Errorf("bids: %w", err)
	}
	asks, err = convert(mexcAsks)
	if err != nil {
		return nil, nil, fmt.Errorf("asks: %w", err)
	}
	return bids, asks, nil
}
