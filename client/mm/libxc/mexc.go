// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

// MEXC Spot v3 CEX Integration
//
// API ENDPOINTS:
// REST: https://api.mexc.com
// WebSocket: wss://wbs-api.mexc.com/ws
// Docs: https://mexcdevelop.github.io/apidocs/spot_v3_en/
//
// PROTOBUF:
// Source: https://github.com/mexcdevelop/websocket-proto
// Package: decred.org/dcrdex/client/mm/libxc/mxctypes/pb

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc/mxctypes"
	"decred.org/dcrdex/client/mm/libxc/mxctypes/pb"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/dexnet"
	"google.golang.org/protobuf/proto"
)

// MEXC Spot v3 CEX adaptor.
type mexc struct {
	log       dex.Logger
	notify    func(interface{})
	apiKey    string
	secretKey string
	ctx       context.Context

	mtx sync.RWMutex

	// websocket connections
	marketWS        comms.WsConn
	marketWSWg      *sync.WaitGroup
	marketWSCancel  context.CancelFunc
	marketWSContext context.Context

	// private websocket for user data
	privateWS   comms.WsConn
	privateWSWg *sync.WaitGroup

	// tracking
	activeTopicsMtx sync.RWMutex
	activeTopics    map[string]struct{}

	// ping management
	pingQuit chan struct{}

	// listen key management
	listenKeyMtx           sync.RWMutex
	listenKey              string
	listenKeyExp           time.Time
	listenKeyKeepalive     chan struct{}
	listenKeyKeepaliveQuit chan struct{} // Controls the keepalive goroutine
	listenKeyKeepaliveMtx  sync.Mutex    // Protects goroutine spawn

	// symbols cache
	symbolsMtx sync.RWMutex
	symbols    map[string]struct{}

	// symbol metadata cache (precision, filters, etc.)
	symbolMetaMtx sync.RWMutex
	symbolMeta    map[string]*mxctypes.Symbol

	// in-memory order books per symbol
	subMarketMtx sync.Mutex // Protects subscription operations
	books        map[string]*mexcOrderBook
	booksMtx     sync.RWMutex

	// balances
	balanceMtx sync.RWMutex
	balances   map[uint32]*ExchangeBalance

	// token mapping (MEXC symbol -> asset IDs)
	tokenIDs map[string][]uint32

	// withdrawal info (asset ID -> withdrawal minimum and lot size)
	minWithdraw atomic.Value // map[uint32]*mexcWithdrawInfo

	// API-tradeable symbols (symbol -> empty struct for O(1) lookup)
	apiTradeableSymbols atomic.Value // map[string]struct{}

	// trade tracking
	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*mexcTradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	httpOnce sync.Once
	httpCli  *http.Client
}

type mexcTradeInfo struct {
	updaterID int
	baseID    uint32
	quoteID   uint32
	sell      bool
	rate      uint64
	qty       uint64
	market    bool
}

type mexcWithdrawInfo struct {
	minimum uint64
	lotSize uint64
}

// mexcOrderBook manages an order book for a single market.
// It keeps the order book synced using REST snapshots and WebSocket full snapshot updates.
type mexcOrderBook struct {
	mtx            sync.RWMutex
	synced         atomic.Bool
	numSubscribers uint32
	cm             *dex.ConnectionMaster

	symbol      string
	book        *orderbook
	updateQueue chan *mxctypes.BookUpdate

	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger

	connectedChan chan bool

	getSnapshot func() (*mxctypes.OrderbookSnapshot, error)
}

// newMexcOrderBook creates a new order book for the given symbol.
func newMexcOrderBook(
	baseConversionFactor, quoteConversionFactor uint64,
	symbol string,
	getSnapshot func() (*mxctypes.OrderbookSnapshot, error),
	log dex.Logger,
) *mexcOrderBook {
	return &mexcOrderBook{
		symbol:                symbol,
		book:                  newOrderBook(),
		updateQueue:           make(chan *mxctypes.BookUpdate, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
		getSnapshot:           getSnapshot,
		connectedChan:         make(chan bool, 1),
	}
}

// convertMexcBook converts bids and asks from MEXC format ([][2]json.Number)
// to the DEX orderbook format ([]*obEntry).
func (b *mexcOrderBook) convertMexcBook(mexcBids, mexcAsks [][2]json.Number) (bids, asks []*obEntry, err error) {
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

// convertBookUpdate converts a BookUpdate with float64 arrays to obEntry slices.
func (b *mexcOrderBook) convertBookUpdate(update *mxctypes.BookUpdate) (bids, asks []*obEntry, err error) {
	convert := func(updates [][2]float64) ([]*obEntry, error) {
		convertedUpdates := make([]*obEntry, 0, len(updates))

		for _, update := range updates {
			price := update[0]
			qty := update[1]

			convertedUpdates = append(convertedUpdates, &obEntry{
				rate: calc.MessageRateAlt(price, b.baseConversionFactor, b.quoteConversionFactor),
				qty:  uint64(qty * float64(b.baseConversionFactor)),
			})
		}

		return convertedUpdates, nil
	}

	bids, err = convert(update.Bids)
	if err != nil {
		return nil, nil, err
	}

	asks, err = convert(update.Asks)
	if err != nil {
		return nil, nil, err
	}

	return bids, asks, nil
}

// Connect implements the dex.Connector interface.
// Synchronizes orderbook: fetch REST snapshot, then accept fresh WebSocket updates.
func (b *mexcOrderBook) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Synchronization variables
	var syncMtx sync.Mutex

	resyncChan := make(chan struct{}, 1)

	desync := func(resync bool) {
		// Mark as unsynced and trigger a book refresh.
		syncMtx.Lock()
		defer syncMtx.Unlock()
		wasSync := b.synced.Swap(false)
		if wasSync {
			if resync {
				select {
				case resyncChan <- struct{}{}:
				default:
				}
			}
		}
	}

	acceptUpdate := func(update *mxctypes.BookUpdate) bool {
		if !b.synced.Load() {
			// Book is still syncing. Discard this update since MEXC sends full
			// snapshots and only the latest snapshot matters.
			return true
		}

		// MEXC sends full snapshots; no sequence validation needed.
		bids, asks, err := b.convertBookUpdate(update)
		if err != nil {
			b.log.Errorf("Error parsing MEXC book update: %v", err)
			return false
		}
		// MEXC sends full snapshots; clear stale data before applying.
		b.book.clear()
		b.book.update(bids, asks)
		return true
	}

	syncOrderbook := func() bool {
		snapshot, err := b.getSnapshot()
		if err != nil {
			b.log.Errorf("Error getting orderbook snapshot: %v", err)
			return false
		}

		bids, asks, err := b.convertMexcBook(snapshot.Bids, snapshot.Asks)
		if err != nil {
			b.log.Errorf("Error parsing MEXC book: %v", err)
			return false
		}

		b.log.Debugf("Got %s orderbook snapshot", b.symbol)

		syncMtx.Lock()
		b.book.clear()
		b.book.update(bids, asks)
		b.synced.Store(true)
		syncMtx.Unlock()

		return true
	}

	var wg sync.WaitGroup

	// Goroutine 1: Process update queue
	wg.Add(1)
	go func() {
		processUpdate := func(update *mxctypes.BookUpdate) bool {
			syncMtx.Lock()
			defer syncMtx.Unlock()
			return acceptUpdate(update)
		}

		defer wg.Done()
		for {
			select {
			case update := <-b.updateQueue:
				if !processUpdate(update) {
					b.log.Tracef("Bad %s update", b.symbol)
					desync(true)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Goroutine 2: Manage synchronization
	wg.Add(1)
	go func() {
		defer wg.Done()

		const retryFrequency = time.Second * 30

		retry := time.After(0)

		for {
			select {
			case <-retry:
			case <-resyncChan:
				if retry != nil { // don't hammer
					continue
				}
			case connected := <-b.connectedChan:
				if !connected {
					b.log.Debugf("Unsyncing %s orderbook due to disconnect", b.symbol)
					desync(false)
					retry = nil
					continue
				}
				// On reconnect, trigger immediate sync
				b.log.Debugf("Market WS reconnected, triggering %s orderbook sync", b.symbol)
			case <-ctx.Done():
				return
			}

			if syncOrderbook() {
				b.log.Infof("Synced %s orderbook", b.symbol)
				retry = nil
			} else {
				b.log.Infof("Failed to sync %s orderbook. Trying again in %s", b.symbol, retryFrequency)
				desync(false) // Marks as unsynced
				retry = time.After(retryFrequency)
			}
		}
	}()

	return &wg, nil
}

// vwap returns the volume weighted average price for a certain quantity of the
// base asset. It returns an error if the orderbook is not synced.
func (b *mexcOrderBook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, false, ErrUnsyncedOrderbook
	}

	vwap, extrema, filled = b.book.vwap(bids, qty)
	return
}

// invVWAP returns the inverse volume weighted average price.
func (b *mexcOrderBook) invVWAP(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, false, ErrUnsyncedOrderbook
	}

	vwap, extrema, filled = b.book.invVWAP(bids, qty)
	return
}

// midGap returns the mid-gap price.
func (b *mexcOrderBook) midGap() uint64 {
	return b.book.midGap()
}

func newMEXC(cfg *CEXConfig) (*mexc, error) {
	return &mexc{
		log:           cfg.Logger,
		notify:        cfg.Notify,
		apiKey:        cfg.APIKey,
		secretKey:     cfg.SecretKey,
		activeTopics:  make(map[string]struct{}),
		symbols:       make(map[string]struct{}),
		symbolMeta:    make(map[string]*mxctypes.Symbol),
		books:         make(map[string]*mexcOrderBook),
		balances:      make(map[uint32]*ExchangeBalance),
		tokenIDs:      make(map[string][]uint32),
		tradeInfo:     make(map[string]*mexcTradeInfo),
		tradeUpdaters: make(map[int]chan *Trade),
	}, nil
}

const (
	mexcHTTPURL      = "https://api.mexc.com"
	mexcWSURL        = "wss://wbs-api.mexc.com/ws"
	mexcPrivateWSURL = "wss://wbs-api.mexc.com/ws"
	mexcRecvWindow   = "5000"
)

// getCoinInfo retrieves MEXC configs for coin/network combinations and their withdrawal status.
func (m *mexc) getCoinInfo(ctx context.Context) ([]mxctypes.CoinInfo, error) {
	var coins []mxctypes.CoinInfo
	err := m.getAPI(ctx, "/api/v3/capital/config/getall", nil, true, &coins)
	if err != nil {
		return nil, fmt.Errorf("error getting coin info: %w", err)
	}

	// Store withdrawal info
	m.readCoins(coins)

	return coins, nil
}

// getDefaultSymbols fetches the list of symbols that support API trading.
// MEXC blocks API trading on certain high-volume pairs like BTCUSDT and ETHUSDT.
func (m *mexc) getDefaultSymbols(ctx context.Context) error {
	var resp mxctypes.DefaultSymbolsResponse
	if err := m.getAPI(ctx, "/api/v3/defaultSymbols", nil, false, &resp); err != nil {
		return fmt.Errorf("failed to fetch default symbols: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("default symbols API error: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	// Convert slice to map for O(1) lookup
	tradeableMap := make(map[string]struct{}, len(resp.Data))
	for _, symbol := range resp.Data {
		tradeableMap[strings.ToUpper(symbol)] = struct{}{}
	}

	m.apiTradeableSymbols.Store(tradeableMap)

	// Check which commonly-used major pairs are blocked
	majorPairs := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "ADAUSDT", "XRPUSDT"}
	var blocked []string
	for _, pair := range majorPairs {
		if _, found := tradeableMap[pair]; !found {
			blocked = append(blocked, pair)
		}
	}

	if len(blocked) > 0 {
		m.log.Infof("MEXC API trading blocked for major pairs: %v", blocked)
	}

	m.log.Debugf("Loaded %d API-tradeable symbols from MEXC", len(tradeableMap))
	return nil
}

// isSymbolAPITradeable checks if a symbol supports API trading.
// Returns true if tradeable, false if blocked or unknown (defensive default).
func (m *mexc) isSymbolAPITradeable(symbol string) bool {
	tradeableI := m.apiTradeableSymbols.Load()
	if tradeableI == nil {
		// If we haven't loaded the list yet, assume tradeable (optimistic)
		return true
	}

	tradeable := tradeableI.(map[string]struct{})
	_, found := tradeable[strings.ToUpper(symbol)]
	return found
}

// readCoins stores withdrawal info for assets with deposits/withdrawals enabled.
// Similar to binance.go:733-770.
func (m *mexc) readCoins(coins []mxctypes.CoinInfo) {
	minWithdraw := make(map[uint32]*mexcWithdrawInfo)

	for _, coinInfo := range coins {
		for _, netInfo := range coinInfo.NetworkList {
			// Build DEX symbol from coin + network (e.g., "usdt.polygon")
			symbol := mexcCoinNetworkToDexSymbol(coinInfo.Coin, netInfo.Network)

			assetID, found := dex.BipSymbolID(symbol)
			if !found {
				continue
			}

			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// Not a registered asset
				continue
			}

			// Skip if deposits or withdrawals are disabled
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				m.log.Tracef("Skipping %s network %s because deposits/withdrawals not enabled",
					coinInfo.Coin, netInfo.Network)
				continue
			}

			// Parse withdrawal minimum from string to float, then convert to atomic units
			withdrawMin, err := strconv.ParseFloat(netInfo.WithdrawMin, 64)
			if err != nil {
				m.log.Errorf("Invalid withdraw minimum for %s network %s: %v",
					coinInfo.Coin, netInfo.Network, err)
				continue
			}

			minimum := uint64(math.Round(withdrawMin * float64(ui.Conventional.ConversionFactor)))

			// Parse lot size (precision) if available
			var lotSize uint64 = 1
			if netInfo.WithdrawIntegerMultiple != "" {
				lotSizeFloat, err := strconv.ParseFloat(netInfo.WithdrawIntegerMultiple, 64)
				if err == nil && lotSizeFloat > 0 {
					lotSize = uint64(math.Round(lotSizeFloat * float64(ui.Conventional.ConversionFactor)))
				}
			}

			if minimum == 0 || lotSize == 0 {
				m.log.Errorf("Invalid withdraw minimum or lot size for %s network %s",
					coinInfo.Coin, netInfo.Network)
				continue
			}

			minWithdraw[assetID] = &mexcWithdrawInfo{
				minimum: minimum,
				lotSize: lotSize,
			}
		}
	}

	m.minWithdraw.Store(minWithdraw)
	m.log.Debugf("Stored withdrawal info for %d assets", len(minWithdraw))
}

// mexcCoinNetworkToDexSymbol converts MEXC coin+network to DEX symbol.
// Examples:
//
//	"USDT", "POLYGON" -> "usdt.polygon"
//	"USDT", "ETH" -> "usdt.eth"
//	"BTC", "BTC" -> "btc"
//	"DCR", "DCR" -> "dcr"
func mexcCoinNetworkToDexSymbol(coin, network string) string {
	coin = strings.ToLower(coin)
	network = strings.ToLower(network)

	// For native coins where coin == network, return just the coin
	if coin == network {
		return coin
	}

	// For tokens on different networks, append network as suffix
	// Map MEXC network names to DEX network names
	networkMap := map[string]string{
		"polygon": "polygon",
		"matic":   "polygon", // MEXC uses "MATIC" for Polygon network
		"eth":     "eth",
		"base":    "base",
		// Add more as needed
	}

	dexNetwork, found := networkMap[network]
	if !found {
		// Default: use network as-is
		dexNetwork = network
	}

	return coin + "." + dexNetwork
}

// dex.Connector implementation
func (m *mexc) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	m.ctx = ctx
	var wg sync.WaitGroup

	// Fetch exchange info synchronously before starting background tasks
	if err := m.ensureExchangeInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to fetch exchange info: %w", err)
	}

	// Fetch coin info to populate withdrawal minimums
	if _, err := m.getCoinInfo(ctx); err != nil {
		m.log.Errorf("Failed to fetch coin info: %v", err)
		// Don't fail Connect, but log the error
	}

	// Note: API-tradeable symbols are fetched in ensureExchangeInfo() above
	// to ensure they're loaded before Markets() can be called

	// Fetch balances in background (non-blocking)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := m.refreshBalances(ctx); err != nil {
			m.log.Errorf("initial balance fetch failed: %v", err)
		}
	}()

	// Market WS connection is deferred until first SubscribeMarket call
	// Private WS connect (for user data streams)
	m.log.Infof("Initializing private WebSocket connection...")
	listenKey, err := m.ensureListenKey(ctx)
	if err != nil {
		m.log.Errorf("Failed to get listen key: %v", err)
		return &wg, fmt.Errorf("failed to get listen key: %w", err)
	}

	m.log.Infof("Got listen key, connecting to private WebSocket...")
	privateWSCfg := &comms.WsCfg{
		URL:           mexcPrivateWSURL + "?listenKey=" + listenKey,
		PingWait:      120 * time.Second, // Allow up to 2 minutes between server messages
		Logger:        m.log.SubLogger("WS-PRIV"),
		RawHandler:    m.handlePrivateRaw,
		EchoPingData:  true,
		ReconnectSync: m.onPrivateReconnect,
	}
	privateWS, err := comms.NewWsConn(privateWSCfg)
	if err != nil {
		return &wg, fmt.Errorf("create private ws: %w", err)
	}

	m.privateWS = privateWS
	m.privateWSWg, err = m.privateWS.Connect(m.ctx)
	if err != nil {
		m.log.Errorf("private ws initial connect error: %v (will auto-reconnect)", err)
	} else {
		m.log.Infof("Private WebSocket connected successfully")
	}

	if m.privateWSWg != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.privateWSWg.Wait()
		}()
	}

	// Subscribe to private channels
	// Per MEXC docs, we need to explicitly subscribe to orders, deals, and account updates
	if err := m.subscribePrivateChannels(); err != nil {
		m.log.Errorf("Failed to subscribe to private channels: %v", err)
	}

	// Start ping ticker
	m.startPing()

	return &wg, nil
}

func (m *mexc) Wait() {}

func (m *mexc) Disconnect() {
	m.stop()
}

// stop cleans up resources and closes connections
func (m *mexc) stop() {
	// Stop pinger
	if m.pingQuit != nil {
		close(m.pingQuit)
		m.pingQuit = nil
	}

	// Stop listen key keepalive
	m.listenKeyKeepaliveMtx.Lock()
	if m.listenKeyKeepaliveQuit != nil {
		close(m.listenKeyKeepaliveQuit)
		m.listenKeyKeepaliveQuit = nil
	}
	m.listenKeyKeepaliveMtx.Unlock()

	// Close listen key to free server resources
	m.listenKeyMtx.Lock()
	listenKey := m.listenKey
	m.listenKey = ""
	m.listenKeyMtx.Unlock()

	if listenKey != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.closeListenKey(ctx, listenKey); err != nil {
			m.log.Warnf("Failed to close listen key on shutdown: %v", err)
		}
	}
}

// initHTTPClient initializes the HTTP client on first use.
func (m *mexc) initHTTPClient() {
	m.httpOnce.Do(func() {
		m.httpCli = &http.Client{
			Timeout: 30 * time.Second,
		}
	})
}

// getAPI performs a signed GET request.
// getOrderbookSnapshot fetches a full order book snapshot from MEXC REST API.
// This is used for initial synchronization of order books.
func (m *mexc) getOrderbookSnapshot(ctx context.Context, symbol string) (*mxctypes.OrderbookSnapshot, error) {
	query := url.Values{}
	query.Set("symbol", symbol)
	query.Set("limit", "5000") // Maximum depth

	var resp mxctypes.OrderbookSnapshot
	if err := m.getAPI(ctx, "/api/v3/depth", query, false, &resp); err != nil {
		return nil, fmt.Errorf("failed to get order book snapshot for %s: %w", symbol, err)
	}

	m.log.Debugf("Got %s order book snapshot", symbol)
	return &resp, nil
}

func (m *mexc) getAPI(ctx context.Context, endpoint string, query url.Values, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodGet, endpoint, query, nil, sign, thing)
}

// postAPI performs a signed POST request.
func (m *mexc) postAPI(ctx context.Context, endpoint string, query url.Values, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodPost, endpoint, query, nil, sign, thing)
}

// putAPI performs a signed PUT request.
func (m *mexc) putAPI(ctx context.Context, endpoint string, query url.Values, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodPut, endpoint, query, nil, sign, thing)
}

// deleteAPI performs a signed DELETE request.
func (m *mexc) deleteAPI(ctx context.Context, endpoint string, query url.Values, sign bool, thing interface{}) error {
	return m.request(ctx, http.MethodDelete, endpoint, query, nil, sign, thing)
}

// request performs an HTTP request with optional HMAC signature.
func (m *mexc) request(ctx context.Context, method, endpoint string, query, form url.Values, sign bool, thing interface{}) error {
	m.initHTTPClient()

	fullURL := mexcHTTPURL + endpoint

	if query == nil {
		query = make(url.Values)
	}
	if sign {
		query.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		query.Add("recvWindow", mexcRecvWindow)
	}
	queryString := query.Encode()
	bodyString := form.Encode()
	header := make(http.Header, 2)
	body := bytes.NewBuffer(nil)
	if bodyString != "" {
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		body = bytes.NewBufferString(bodyString)
	}
	if sign {
		header.Set("X-MEXC-APIKEY", m.apiKey)
		// MEXC signature is HMAC SHA256 of query string
		raw := queryString + bodyString
		mac := hmac.New(sha256.New, []byte(m.secretKey))
		if _, err := mac.Write([]byte(raw)); err != nil {
			return fmt.Errorf("hmac write error: %w", err)
		}
		v := url.Values{}
		v.Set("signature", hex.EncodeToString(mac.Sum(nil)))
		queryString = fmt.Sprintf("%s&%s", queryString, v.Encode())
	}
	if queryString != "" {
		fullURL = fmt.Sprintf("%s?%s", fullURL, queryString)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	// Try to capture error response from MEXC
	var errResp mxctypes.ErrorResponse
	err = dexnet.Do(req, thing, dexnet.WithSizeLimit(1<<24), dexnet.WithErrorParsing(&errResp))
	if err != nil {
		// If we got a MEXC error response, include it in the error message
		if errResp.Code != 0 || errResp.Msg != "" {
			m.log.Errorf("MEXC API error from %s %q: code=%d, msg=%q, query=%q, body=%q",
				method, endpoint, errResp.Code, errResp.Msg, queryString, bodyString)
			return fmt.Errorf("MEXC API error (%s %s): code=%d, msg=%s", method, endpoint, errResp.Code, errResp.Msg)
		}
		// Generic network/parsing error
		m.log.Errorf("request error from endpoint %s %q with query = %q, body = %q: %v",
			method, endpoint, queryString, bodyString, err)
		return fmt.Errorf("MEXC request failed (%s %s): %w", method, endpoint, err)
	}

	return nil
}

// Balance returns the balance of an asset at the CEX.
func (m *mexc) Balance(assetID uint32) (*ExchangeBalance, error) {
	m.balanceMtx.RLock()
	bal, found := m.balances[assetID]
	m.balanceMtx.RUnlock()

	if !found {
		return &ExchangeBalance{Available: 0, Locked: 0}, nil
	}
	return bal, nil
}

// Balances returns all balances.
func (m *mexc) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	m.balanceMtx.RLock()
	defer m.balanceMtx.RUnlock()

	// Return a copy
	balances := make(map[uint32]*ExchangeBalance, len(m.balances))
	for assetID, bal := range m.balances {
		balCopy := *bal
		balances[assetID] = &balCopy
	}
	return balances, nil
}

// refreshBalances fetches account balances from MEXC.
func (m *mexc) refreshBalances(ctx context.Context) error {
	var resp mxctypes.Account
	if err := m.getAPI(ctx, "/api/v3/account", nil, true, &resp); err != nil {
		return fmt.Errorf("error fetching account: %w", err)
	}

	m.log.Debugf("Fetching balances from MEXC")

	// Collect balance updates to notify about AFTER releasing the lock
	var updates []*BalanceUpdate

	m.balanceMtx.Lock()
	for _, bal := range resp.Balances {
		// Skip zero balances
		if bal.Free == 0 && bal.Locked == 0 {
			continue
		}

		// Get all matching asset IDs for this coin (may include tokens on different networks)
		assetIDs := m.getDEXAssetIDs(bal.Asset)
		if len(assetIDs) == 0 {
			m.log.Tracef("Unknown asset symbol %s, skipping", bal.Asset)
			continue
		}

		// Store balance for all matching asset IDs
		for _, assetID := range assetIDs {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				m.log.Errorf("No unit info for asset %s (ID %d): %v", bal.Asset, assetID, err)
				continue
			}

			updatedBalance := &ExchangeBalance{
				Available: uint64(math.Round(bal.Free * float64(ui.Conventional.ConversionFactor))),
				Locked:    uint64(math.Round(bal.Locked * float64(ui.Conventional.ConversionFactor))),
			}

			m.balances[assetID] = updatedBalance

			// Queue notification for after we release the lock
			updates = append(updates, &BalanceUpdate{
				AssetID: assetID,
				Balance: updatedBalance,
			})
		}
	}
	m.balanceMtx.Unlock()

	// Send notifications OUTSIDE the lock to avoid potential deadlocks
	if m.notify != nil {
		for _, update := range updates {
			m.notify(update)
		}
	}
	return nil
}

// supportedMEXCTokens defines which token networks MEXC supports.
// Since MEXC doesn't have an API to query this, we hardcode the networks
// similar to how Coinbase does it. These are the networks we know MEXC supports
// for withdrawals/deposits.
var supportedMEXCTokens = map[uint32]struct{}{
	60001:  {}, // USDC on ETH
	60002:  {}, // USDT on ETH
	61000:  {}, // USDC on BASE
	966001: {}, // USDC on POLYGON
	966004: {}, // USDT on POLYGON
}

// getDEXAssetIDs returns all DEX asset IDs that match a MEXC coin symbol.
// For tokens like USDT/USDC, it returns ALL supported network variants to allow
// the user to choose which network they want to trade on (matching Coinbase's behavior).
func (m *mexc) getDEXAssetIDs(coin string) []uint32 {
	dexSymbol := mexcAssetToDexSymbol(coin)

	isRegistered := func(assetID uint32) bool {
		_, err := asset.UnitInfo(assetID)
		return err == nil
	}

	assetIDs := make([]uint32, 0, 3)

	// Try direct symbol match (e.g., BTC, ETH, DCR)
	if assetID, found := dex.BipSymbolID(dexSymbol); found {
		if isRegistered(assetID) {
			assetIDs = append(assetIDs, assetID)
			return assetIDs
		}
	}

	// For tokens, return ALL supported network variants
	// This allows users to choose which network to use
	tokenVariants := []string{"polygon", "eth", "base"}
	for _, network := range tokenVariants {
		tokenSymbol := dexSymbol + "." + network
		if assetID, found := dex.BipSymbolID(tokenSymbol); found {
			// Only include if it's in our supported list AND registered
			if _, supported := supportedMEXCTokens[assetID]; supported && isRegistered(assetID) {
				assetIDs = append(assetIDs, assetID)
			}
		}
	}

	// For wrapped assets, try the wrapped version
	if dexSymbol == "eth" {
		if assetID, found := dex.BipSymbolID("weth.polygon"); found {
			if isRegistered(assetID) {
				assetIDs = append(assetIDs, assetID)
			}
		}
	}

	return assetIDs
}

// dexNetworkToMexcNetwork maps DEX network identifiers to MEXC network names.
// This handles differences in network naming conventions between DEX and MEXC.
// MEXC uses ticker symbols (ETH, MATIC) instead of full network names (ETHEREUM, POLYGON).
var dexNetworkToMexcNetwork = map[string]string{
	"ethereum": "ETH",   // MEXC uses "ETH" instead of "ETHEREUM"
	"polygon":  "MATIC", // MEXC uses "MATIC" instead of "POLYGON"
}

// mexcNetworkToDexNetwork is the reverse mapping for parsing MEXC responses.
var mexcNetworkToDexNetwork = map[string]string{
	"ETH":   "ethereum",
	"MATIC": "polygon",
}

// mexcAssetToDexSymbol maps MEXC asset symbols to DEX symbols.
func mexcAssetToDexSymbol(mexcSymbol string) string {
	// MEXC uses different symbols for some assets
	switch strings.ToUpper(mexcSymbol) {
	case "XBT":
		return "btc"
	case "POL":
		return "polygon" // MEXC uses "POL", DEX uses "polygon"
	default:
		return strings.ToLower(mexcSymbol)
	}
}

// dexSymbolToMexcAsset maps DEX symbols to MEXC asset symbols.
// For token symbols like "usdt.polygon", it extracts the base coin "USDT".
func dexSymbolToMexcAsset(dexSymbol string) string {
	// Strip network suffix for tokens (e.g., "usdt.polygon" -> "usdt")
	if idx := strings.Index(dexSymbol, "."); idx != -1 {
		dexSymbol = dexSymbol[:idx]
	}

	// Reverse mapping
	switch strings.ToLower(dexSymbol) {
	case "btc":
		return "BTC" // MEXC might use BTC or XBT
	default:
		return strings.ToUpper(dexSymbol)
	}
}

// getNetworkForAsset returns the MEXC network name for a given asset ID.
// It handles MEXC-specific network naming conventions (e.g., "MATIC" instead of "POLYGON").
func (m *mexc) getNetworkForAsset(assetID uint32) (string, error) {
	token := asset.TokenInfo(assetID)
	if token != nil {
		// Token asset - determine network from parent chain
		var dexNetwork string
		switch token.ParentID {
		case 60: // Ethereum
			dexNetwork = "ethereum"
		case 966: // Polygon
			dexNetwork = "polygon"
		case 8453: // Base
			dexNetwork = "base"
		default:
			return "", fmt.Errorf("unsupported token network for asset %d (parent: %d)", assetID, token.ParentID)
		}

		// Map to MEXC-specific network name (e.g., "polygon" -> "MATIC")
		if mexcNetwork, found := dexNetworkToMexcNetwork[dexNetwork]; found {
			return mexcNetwork, nil
		}
		return strings.ToUpper(dexNetwork), nil
	}

	// Native asset - use asset symbol as network
	// For most native assets, MEXC uses the coin name as the network
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return "", fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	// For native coins, the network is usually just the uppercase symbol
	// But some have specific naming conventions
	switch assetID {
	case 0: // BTC
		return "BTC", nil
	case 42: // DCR
		return "DCR", nil
	case 60: // ETH
		return "ETH", nil
	case 966: // MATIC/Polygon native
		// Map through dexNetworkToMexcNetwork for consistency
		if mexcNetwork, found := dexNetworkToMexcNetwork["polygon"]; found {
			return mexcNetwork, nil
		}
		return "POLYGON", nil
	default:
		return strings.ToUpper(symbol), nil
	}
}

func (m *mexc) generateTradeID() string {
	return fmt.Sprintf("dcrdex_%d", time.Now().UnixNano())
}

func (m *mexc) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	symbol := m.symbolFor(baseID, quoteID)

	// First, check order status via REST API as a fallback
	// This handles cases where WebSocket updates were missed due to disconnections
	status, err := m.TradeStatus(ctx, tradeID, baseID, quoteID)
	if err != nil {
		m.log.Warnf("Failed to check order status before cancel: %v", err)
		// Continue with cancellation attempt anyway
	} else if status.Complete {
		m.log.Infof("Order %s is already complete (filled or canceled), skipping cancel", tradeID)
		return nil // Not an error - order is already done
	}

	query := url.Values{}
	query.Add("symbol", symbol)
	query.Add("origClientOrderId", tradeID)

	var resp mxctypes.CancelOrderResponse
	if err := m.deleteAPI(ctx, "/api/v3/order", query, true, &resp); err != nil {
		// Check if error is "order filled" - this is not really an error
		if strings.Contains(err.Error(), "Order filled") || strings.Contains(err.Error(), "-2011") {
			m.log.Infof("Order %s was already filled, treating as success", tradeID)
			return nil
		}
		return fmt.Errorf("error canceling order: %w", err)
	}

	m.log.Debugf("Canceled order %s (client ID: %s)", resp.OrderID, resp.ClientOrderID)
	return nil
}

// minimumWithdraws returns the minimum withdrawal amounts for base and quote assets.
func (m *mexc) minimumWithdraws(baseID, quoteID uint32) (base uint64, quote uint64) {
	minsI := m.minWithdraw.Load()
	if minsI == nil {
		return 0, 0
	}
	mins := minsI.(map[uint32]*mexcWithdrawInfo)
	if baseInfo, found := mins[baseID]; found {
		base = baseInfo.minimum
	}
	if quoteInfo, found := mins[quoteID]; found {
		quote = quoteInfo.minimum
	}
	return
}

// withdrawInfo returns withdrawal information for a specific asset.
func (m *mexc) withdrawInfo(assetID uint32) (*mexcWithdrawInfo, error) {
	minsI := m.minWithdraw.Load()
	if minsI == nil {
		return nil, fmt.Errorf("no withdraw info loaded")
	}
	mins := minsI.(map[uint32]*mexcWithdrawInfo)
	if info, found := mins[assetID]; found {
		return info, nil
	}
	return nil, fmt.Errorf("no withdraw info for asset ID %d", assetID)
}

func (m *mexc) Markets(ctx context.Context) (map[string]*Market, error) {
	if err := m.ensureExchangeInfo(ctx); err != nil {
		return nil, err
	}

	m.symbolMetaMtx.RLock()
	defer m.symbolMetaMtx.RUnlock()

	markets := make(map[string]*Market)
	skippedCount := 0
	noBaseCount := 0
	noQuoteCount := 0
	apiBlockedCount := 0

	for symbol, symInfo := range m.symbolMeta {
		// Step 1: Check if symbol is enabled on MEXC
		if symInfo.Status != "1" {
			skippedCount++
			continue
		}

		// Step 2: Match with DEX assets (existing logic)
		baseAssetIDs := m.getDEXAssetIDs(symInfo.BaseAsset)
		quoteAssetIDs := m.getDEXAssetIDs(symInfo.QuoteAsset)

		if len(baseAssetIDs) == 0 {
			noBaseCount++
			continue
		}

		if len(quoteAssetIDs) == 0 {
			noQuoteCount++
			continue
		}

		// Step 3: FINAL VALIDATION - Check if symbol supports API trading
		// This prevents BTCUSDT, ETHUSDT and other API-blocked pairs from being matched
		if !m.isSymbolAPITradeable(symbol) {
			apiBlockedCount++
			m.log.Tracef("Skipping %s: API trading not supported", symbol)
			continue
		}

		// Step 4: Create ALL combinations of base and quote IDs, just like Coinbase does.
		// This allows users to choose which network variant they want to trade on.
		for _, baseID := range baseAssetIDs {
			for _, quoteID := range quoteAssetIDs {
				// Create market ID using the full asset symbols INCLUDING network suffix
				marketID := dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID)

				m.log.Tracef("Adding market %s -> DEX market %s (base:%s quote:%s)",
					symbol, marketID, symInfo.BaseAsset, symInfo.QuoteAsset)

				// Get minimum withdrawals
				baseMinWithdraw, quoteMinWithdraw := m.minimumWithdraws(baseID, quoteID)

				// Store with DEX market ID as key
				markets[marketID] = &Market{
					BaseID:           baseID,
					QuoteID:          quoteID,
					BaseMinWithdraw:  baseMinWithdraw,
					QuoteMinWithdraw: quoteMinWithdraw,
					// Day stats would need a separate API call to /api/v3/ticker/24hr
					// We can add this later if needed
					Day: nil,
				}
			}
		}
	}

	m.log.Infof("MEXC: Loaded %d DEX markets from %d MEXC symbols (skipped:%d disabled, %d API-blocked, %d no base support, %d no quote support)",
		len(markets), len(m.symbolMeta), skippedCount, apiBlockedCount, noBaseCount, noQuoteCount)
	return markets, nil
}

// ensureMarketConnection ensures the market WebSocket is connected.
// This is called lazily on first SubscribeMarket call, matching Binance/Coinbase behavior.
func (m *mexc) ensureMarketConnection(ctx context.Context) error {
	// Check if already connected
	if m.marketWS != nil && !m.marketWS.IsDown() {
		return nil
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Double-check after acquiring lock
	if m.marketWS != nil && !m.marketWS.IsDown() {
		return nil
	}

	m.log.Infof("Initializing market WebSocket connection (lazy)...")

	connectEventFunc := func(cs comms.ConnectionStatus) {
		if cs != comms.Disconnected && cs != comms.Connected {
			return
		}
		// If disconnected, set all books to unsynced so bots will not place new orders.
		connected := cs == comms.Connected
		m.booksMtx.RLock()
		defer m.booksMtx.RUnlock()
		for _, book := range m.books {
			select {
			case book.connectedChan <- connected:
			default:
			}
		}
	}

	wsCfg := &comms.WsCfg{
		URL:              mexcWSURL,
		PingWait:         120 * time.Second, // Allow up to 2 minutes between server messages
		Logger:           m.log.SubLogger("WS-MKT"),
		RawHandler:       m.handleMarketRaw,
		EchoPingData:     true,
		ReconnectSync:    m.onMarketReconnect,
		ConnectEventFunc: connectEventFunc,
	}

	marketWS, err := comms.NewWsConn(wsCfg)
	if err != nil {
		return fmt.Errorf("create market ws: %w", err)
	}

	// Create a separate cancellable context for the market WebSocket
	// so we can close it independently when all subscriptions are removed
	marketWSCtx, marketWSCancel := context.WithCancel(m.ctx)

	m.marketWS = marketWS
	m.marketWSContext = marketWSCtx
	m.marketWSCancel = marketWSCancel
	marketWSWg, err := m.marketWS.Connect(marketWSCtx)
	if err != nil {
		marketWSCancel()
		return fmt.Errorf("market ws connect error: %w", err)
	}

	m.marketWSWg = marketWSWg
	m.log.Infof("Market WebSocket connected successfully")

	return nil
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP or MidGap.
func (m *mexc) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	m.subMarketMtx.Lock()
	defer m.subMarketMtx.Unlock()

	symbol := m.symbolFor(baseID, quoteID)
	if err := m.ensureExchangeInfo(ctx); err != nil {
		return err
	}
	if !m.symbolExists(symbol) {
		return fmt.Errorf("symbol not found on MEXC: %s", symbol)
	}

	// Ensure market WebSocket is connected (lazy initialization on first call)
	if err := m.ensureMarketConnection(ctx); err != nil {
		return fmt.Errorf("failed to connect market websocket: %w", err)
	}

	// Check if book already exists
	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if exists {
		// Book already exists, just increment subscriber count
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return nil
	}

	// Get unit info for conversion factors
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("error getting unit info for base asset ID %d: %v", baseID, err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("error getting unit info for quote asset ID %d: %v", quoteID, err)
	}

	// Create snapshot fetcher function
	getSnapshot := func() (*mxctypes.OrderbookSnapshot, error) {
		return m.getOrderbookSnapshot(ctx, symbol)
	}

	// Create new order book
	book = newMexcOrderBook(
		bui.Conventional.ConversionFactor,
		qui.Conventional.ConversionFactor,
		symbol,
		getSnapshot,
		m.log,
	)

	// Add to books map
	m.booksMtx.Lock()
	m.books[symbol] = book
	m.booksMtx.Unlock()

	// Start book sync in a connection master
	cm := dex.NewConnectionMaster(book)
	book.mtx.Lock()
	book.cm = cm
	book.mtx.Unlock()

	if err := cm.ConnectOnce(ctx); err != nil {
		m.log.Errorf("Error connecting %s order book: %v", symbol, err)
		// Don't return error, it will retry
	}

	// Subscribe to WebSocket topics: orderbook snapshots and trade data.
	// Using limit.depth (full snapshots) for orderbook and aggre.deals for trades.
	// Both channels tested and confirmed working on 2025-10-03.
	depthTopic := mxctypes.PublicLimitDepthTopic(symbol, "20")
	tradesTopic := mxctypes.PublicAggreDealsTopic(symbol, "100ms")

	if m.marketWS == nil || m.marketWS.IsDown() {
		return fmt.Errorf("market websocket not connected")
	}

	req := &mxctypes.WsRequest{
		Method: "SUBSCRIPTION",
		Params: []string{depthTopic, tradesTopic},
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}

	if err := m.marketWS.SendRaw(b); err != nil {
		return err
	}

	m.activeTopicsMtx.Lock()
	m.activeTopics[depthTopic] = struct{}{}
	m.activeTopics[tradesTopic] = struct{}{}
	m.activeTopicsMtx.Unlock()

	m.log.Infof("Subscribed to %s orderbook and trade updates", symbol)
	return nil
}

func (m *mexc) handleMarketRaw(b []byte) {
	// Try to parse JSON control/ack; if it fails, treat as protobuf payload.
	var msg struct {
		ID     int    `json:"id"`
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Chan   string `json:"c"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(b, &msg); err == nil {
		// Handle server PING - respond with PONG
		if msg.Method == "PING" {
			pong := map[string]interface{}{
				"id":   0,
				"code": 0,
				"msg":  "PONG",
			}
			if b, err := json.Marshal(pong); err == nil {
				if err := m.marketWS.SendRaw(b); err != nil {
					m.log.Debugf("Failed to send PONG response: %v", err)
				}
			}
			return
		}
		// Handle our PONG responses or other control messages
		if msg.Msg != "" || msg.Chan != "" {
			// Check for subscription confirmation or error
			if msg.Code == 0 && strings.Contains(msg.Msg, "spot@public") {
				m.log.Infof("Market WS subscription confirmed: %s", msg.Msg)
			} else if msg.Code != 0 {
				m.log.Warnf("Market WS error response: code=%d, msg=%s", msg.Code, msg.Msg)
			}
			return
		}
	}

	// Decode protobuf wrapper
	var wrapper pb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(b, &wrapper); err != nil {
		m.log.Debugf("Failed to unmarshal protobuf: %v", err)
		return
	}

	// Route message to appropriate handler based on channel type
	channel := wrapper.GetChannel()
	switch {
	case strings.Contains(channel, "spot@public.limit.depth.v3.api"):
		m.handleLimitDepthUpdate(&wrapper)
	case strings.Contains(channel, "spot@public.aggre.deals.v3.api"):
		m.handleAggreDealsUpdate(&wrapper)
	case strings.Contains(channel, "spot@public.deals.v3.api"):
		m.handleDealsUpdate(&wrapper)
	default:
		m.log.Debugf("Unhandled channel: %s", channel)
	}
}

// handleLimitDepthUpdate processes orderbook snapshot updates from limit.depth channel.
func (m *mexc) handleLimitDepthUpdate(wrapper *pb.PushDataV3ApiWrapper) {
	depth := wrapper.GetPublicLimitDepths()
	if depth == nil {
		return
	}

	// Extract symbol from channel
	channel := wrapper.GetChannel()
	parts := strings.Split(channel, "@")
	if len(parts) < 3 {
		m.log.Errorf("Invalid limit depth channel format: %s", channel)
		return
	}
	symbol := parts[2]

	// Find the order book for this symbol
	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return
	}

	// Build update from snapshot
	update := &mxctypes.BookUpdate{
		Symbol: symbol,
		Asks:   make([][2]float64, 0, len(depth.GetAsks())),
		Bids:   make([][2]float64, 0, len(depth.GetBids())),
	}

	// Process bids
	for _, item := range depth.GetBids() {
		price, err := strconv.ParseFloat(item.GetPrice(), 64)
		if err != nil {
			m.log.Errorf("Invalid bid price: %s", item.GetPrice())
			continue
		}
		qty, err := strconv.ParseFloat(item.GetQuantity(), 64)
		if err != nil {
			m.log.Errorf("Invalid bid quantity: %s", item.GetQuantity())
			continue
		}
		update.Bids = append(update.Bids, [2]float64{price, qty})
	}

	// Process asks
	for _, item := range depth.GetAsks() {
		price, err := strconv.ParseFloat(item.GetPrice(), 64)
		if err != nil {
			m.log.Errorf("Invalid ask price: %s", item.GetPrice())
			continue
		}
		qty, err := strconv.ParseFloat(item.GetQuantity(), 64)
		if err != nil {
			m.log.Errorf("Invalid ask quantity: %s", item.GetQuantity())
			continue
		}
		update.Asks = append(update.Asks, [2]float64{price, qty})
	}

	// Send to book's update queue
	book.updateQueue <- update
}

// handleAggreDealsUpdate processes trade updates from aggre.deals channel.
// Currently logs trade data for monitoring; available for future strategy enhancements.
func (m *mexc) handleAggreDealsUpdate(wrapper *pb.PushDataV3ApiWrapper) {
	deals := wrapper.GetPublicAggreDeals()
	if deals == nil {
		return
	}

	// Extract symbol from channel (format: spot@public.aggre.deals.v3.api.pb@100ms@BTCUSDT)
	channel := wrapper.GetChannel()
	parts := strings.Split(channel, "@")
	if len(parts) < 4 {
		m.log.Errorf("Invalid aggre deals channel format: %s", channel)
		return
	}
	_ = parts[3] // Symbol is at index 3 for aggre.deals
	_ = deals
	// Trade data available for future strategy enhancements
}

func (m *mexc) handleDealsUpdate(wrapper *pb.PushDataV3ApiWrapper) {
	deals := wrapper.GetPublicDeals()
	if deals == nil {
		return
	}

	// Extract symbol from channel (format: spot@public.deals.v3.api@BTCUSDT)
	channel := wrapper.GetChannel()
	parts := strings.Split(channel, "@")
	if len(parts) < 3 {
		m.log.Errorf("Invalid deals channel format: %s", channel)
		return
	}
	_ = parts[2]
	_ = deals
	// Trade data available for future strategy enhancements
}

func (m *mexc) onMarketReconnect() {
	// Re-subscribe active topics after reconnect
	m.activeTopicsMtx.RLock()
	topics := make([]string, 0, len(m.activeTopics))
	for t := range m.activeTopics {
		topics = append(topics, t)
	}
	m.activeTopicsMtx.RUnlock()

	if len(topics) == 0 {
		m.log.Infof("Market WS reconnected but no topics to resubscribe, closing connection")
		// No subscriptions, close the connection instead of keeping it alive
		m.mtx.Lock()
		if m.marketWSCancel != nil {
			m.marketWSCancel()
			m.marketWS = nil
			m.marketWSWg = nil
			m.marketWSCancel = nil
			m.marketWSContext = nil
		}
		m.mtx.Unlock()
		return
	}

	if m.marketWS == nil || m.marketWS.IsDown() {
		m.log.Warnf("Market WS reconnected but connection is down, skipping resubscribe")
		return
	}

	m.log.Infof("Market WS reconnected, resubscribing to %d topics", len(topics))
	req := &mxctypes.WsRequest{Method: "SUBSCRIPTION", Params: topics}
	b, err := json.Marshal(req)
	if err != nil {
		m.log.Errorf("resubscribe marshal: %v", err)
		return
	}
	if err := m.marketWS.SendRaw(b); err != nil {
		m.log.Errorf("resubscribe send error: %v", err)
	} else {
		m.log.Debugf("Successfully resubscribed to %d topics", len(topics))
	}
}

// handlePrivateRaw handles private WebSocket messages
func (m *mexc) handlePrivateRaw(b []byte) {
	// Try to parse JSON control/ack; if it fails, treat as protobuf payload
	var msg struct {
		ID     int    `json:"id"`
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Chan   string `json:"c"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(b, &msg); err == nil {
		m.log.Tracef("Parsed as JSON control message: method=%s, code=%d, msg=%s", msg.Method, msg.Code, msg.Msg)
		// Handle server PING - respond with PONG
		if msg.Method == "PING" {
			pong := map[string]interface{}{
				"id":   0,
				"code": 0,
				"msg":  "PONG",
			}
			if b, err := json.Marshal(pong); err == nil {
				if err := m.privateWS.SendRaw(b); err != nil {
					m.log.Debugf("Failed to send PONG response: %v", err)
				}
			}
			return
		}
		// Handle our PONG responses or other control messages
		if msg.Msg != "" || msg.Chan != "" {
			// Control message acknowledged; nothing else to do for now
			m.log.Tracef("Control message acknowledged: msg=%s, chan=%s", msg.Msg, msg.Chan)
			return
		}
	}

	// Decode protobuf wrapper
	m.log.Tracef("Attempting to parse as protobuf message")
	var wrapper pb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(b, &wrapper); err != nil {
		m.log.Debugf("Failed to unmarshal private protobuf: %v", err)
		return
	}

	// Dispatch based on channel type
	channel := wrapper.GetChannel()
	m.log.Tracef("Private protobuf message on channel: %s", channel)
	switch {
	case strings.Contains(channel, "spot@private.orders.v3.api"):
		m.handlePrivateOrders(&wrapper)
	case strings.Contains(channel, "spot@private.deals.v3.api"):
		m.handlePrivateDeals(&wrapper)
	case strings.Contains(channel, "spot@private.account.v3.api"):
		m.handlePrivateAccount(&wrapper)
	default:
		m.log.Debugf("Unhandled private channel: %s", channel)
	}
}

// handlePrivateOrders handles private order updates
func (m *mexc) handlePrivateOrders(wrapper *pb.PushDataV3ApiWrapper) {
	order := wrapper.GetPrivateOrders()
	if order == nil {
		return
	}

	// Get order details
	clientOrderID := order.GetClientId()
	if clientOrderID == "" {
		m.log.Debugf("Private order update with no client order ID")
		return
	}

	m.log.Debugf("Private order update: ClientOrderID=%s, Status=%d, Price=%s, Qty=%s, CumulativeQty=%s",
		clientOrderID, order.GetStatus(), order.GetPrice(), order.GetQuantity(), order.GetCumulativeQuantity())

	// Find the trade info for this order
	m.tradeUpdaterMtx.RLock()
	info, found := m.tradeInfo[clientOrderID]
	if !found {
		m.tradeUpdaterMtx.RUnlock()
		m.log.Debugf("No trade info found for client order ID %s", clientOrderID)
		return
	}

	updater, found := m.tradeUpdaters[info.updaterID]
	if !found {
		m.tradeUpdaterMtx.RUnlock()
		m.log.Errorf("No trade updater found for client order ID %s", clientOrderID)
		return
	}
	m.tradeUpdaterMtx.RUnlock()

	// Get unit info for conversion
	bui, err := asset.UnitInfo(info.baseID)
	if err != nil {
		m.log.Errorf("Error getting unit info for base asset ID %d: %v", info.baseID, err)
		return
	}

	qui, err := asset.UnitInfo(info.quoteID)
	if err != nil {
		m.log.Errorf("Error getting unit info for quote asset ID %d: %v", info.quoteID, err)
		return
	}

	// Parse filled and total quantities
	filledQty, err := strconv.ParseFloat(order.GetCumulativeQuantity(), 64)
	if err != nil {
		m.log.Errorf("Error parsing cumulative quantity %s: %v", order.GetCumulativeQuantity(), err)
		return
	}

	totalQty, err := strconv.ParseFloat(order.GetQuantity(), 64)
	if err != nil {
		m.log.Errorf("Error parsing order quantity %s: %v", order.GetQuantity(), err)
		return
	}

	// MEXC WebSocket status codes (observed from logs):
	// 1 = NEW
	// 2 = PARTIALLY_FILLED (or FILLED - need to check quantities!)
	// 3 = ??? (possibly not used)
	// 4 = CANCELED
	// 5 = PARTIALLY_CANCELED
	status := order.GetStatus()

	// Determine if order is complete:
	// - Status 4 or 5 = cancelled
	// - Status 2 with filledQty == totalQty = fully filled
	// - Otherwise check if cumulative qty equals total qty (safety check)
	complete := status == 4 || status == 5 || (filledQty >= totalQty && totalQty > 0)

	m.log.Infof("Order status update for %s: status=%d, complete=%t, filled=%s/%s",
		clientOrderID, status, complete, order.GetCumulativeQuantity(), order.GetQuantity())

	baseFilled := uint64(filledQty * float64(bui.Conventional.ConversionFactor))

	// Calculate quote filled (base * avgPrice)
	var quoteFilled uint64
	if filledQty > 0 {
		avgPrice, err := strconv.ParseFloat(order.GetAvgPrice(), 64)
		if err != nil {
			m.log.Errorf("Error parsing avg price %s: %v", order.GetAvgPrice(), err)
			return
		}
		quoteFilled = uint64(filledQty * avgPrice * float64(qui.Conventional.ConversionFactor))
	}

	// Send update to the subscriber
	update := &Trade{
		ID:          clientOrderID,
		Complete:    complete,
		Rate:        info.rate,
		Qty:         info.qty,
		Market:      info.market,
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
		BaseID:      info.baseID,
		QuoteID:     info.quoteID,
		Sell:        info.sell,
	}

	select {
	case updater <- update:
		m.log.Debugf("Sent trade update for %s: complete=%t, baseFilled=%d, quoteFilled=%d",
			clientOrderID, complete, baseFilled, quoteFilled)
	default:
		m.log.Warnf("Trade updater channel full for %s", clientOrderID)
	}

	// Remove from tracking if complete (delayed to allow late-arriving deal updates)
	if complete {
		go func(id string) {
			// Wait 5 seconds to allow any late deal (fill) updates to arrive
			time.Sleep(5 * time.Second)
			m.tradeUpdaterMtx.Lock()
			delete(m.tradeInfo, id)
			m.tradeUpdaterMtx.Unlock()
			m.log.Debugf("Removed trade info for completed order %s", id)
		}(clientOrderID)
	}
}

// handlePrivateDeals handles private deal updates (trade executions/fills)
func (m *mexc) handlePrivateDeals(wrapper *pb.PushDataV3ApiWrapper) {
	deal := wrapper.GetPrivateDeals()
	if deal == nil {
		return
	}

	// Get client order ID
	clientOrderID := deal.GetClientOrderId()
	if clientOrderID == "" {
		m.log.Debugf("Private deal update with no client order ID")
		return
	}

	m.log.Debugf("Private deal (fill) update: ClientOrderID=%s, TradeID=%s, Price=%s, Qty=%s, Amount=%s, Fee=%s %s",
		clientOrderID, deal.GetTradeId(), deal.GetPrice(), deal.GetQuantity(), deal.GetAmount(),
		deal.GetFeeAmount(), deal.GetFeeCurrency())

	// Find the trade info for this order
	m.tradeUpdaterMtx.RLock()
	info, found := m.tradeInfo[clientOrderID]
	if !found {
		m.tradeUpdaterMtx.RUnlock()
		m.log.Debugf("No trade info found for client order ID %s in deals handler", clientOrderID)
		return
	}

	updater, found := m.tradeUpdaters[info.updaterID]
	if !found {
		m.tradeUpdaterMtx.RUnlock()
		m.log.Errorf("No trade updater found for client order ID %s in deals handler", clientOrderID)
		return
	}
	m.tradeUpdaterMtx.RUnlock()

	// Get unit info for conversion
	bui, err := asset.UnitInfo(info.baseID)
	if err != nil {
		m.log.Errorf("Error getting unit info for base asset ID %d: %v", info.baseID, err)
		return
	}

	qui, err := asset.UnitInfo(info.quoteID)
	if err != nil {
		m.log.Errorf("Error getting unit info for quote asset ID %d: %v", info.quoteID, err)
		return
	}

	// Parse filled quantity (this is a PARTIAL fill for this specific trade execution)
	filledQty, err := strconv.ParseFloat(deal.GetQuantity(), 64)
	if err != nil {
		m.log.Errorf("Error parsing deal quantity %s: %v", deal.GetQuantity(), err)
		return
	}

	// Parse filled amount (quote currency)
	filledAmount, err := strconv.ParseFloat(deal.GetAmount(), 64)
	if err != nil {
		m.log.Errorf("Error parsing deal amount %s: %v", deal.GetAmount(), err)
		return
	}

	// Parse fee
	feeAmount, err := strconv.ParseFloat(deal.GetFeeAmount(), 64)
	if err != nil {
		m.log.Errorf("Error parsing fee amount %s: %v", deal.GetFeeAmount(), err)
		return
	}

	// Convert to atomic units
	baseFilled := uint64(filledQty * float64(bui.Conventional.ConversionFactor))
	quoteFilled := uint64(filledAmount * float64(qui.Conventional.ConversionFactor))

	// Handle fee (subtract from the filled amount for the asset being received)
	feeCurrency := deal.GetFeeCurrency()
	baseSymbol := strings.ToUpper(dex.BipIDSymbol(info.baseID))
	quoteSymbol := strings.ToUpper(dex.BipIDSymbol(info.quoteID))

	// Strip network suffixes for comparison
	if i := strings.Index(baseSymbol, "."); i > 0 {
		baseSymbol = baseSymbol[:i]
	}
	if i := strings.Index(quoteSymbol, "."); i > 0 {
		quoteSymbol = quoteSymbol[:i]
	}

	// Subtract fee from the appropriate side
	if feeCurrency == baseSymbol {
		feeAtomic := uint64(feeAmount * float64(bui.Conventional.ConversionFactor))
		if baseFilled > feeAtomic {
			baseFilled -= feeAtomic
		}
	} else if feeCurrency == quoteSymbol {
		feeAtomic := uint64(feeAmount * float64(qui.Conventional.ConversionFactor))
		if quoteFilled > feeAtomic {
			quoteFilled -= feeAtomic
		}
	}

	// Send update - NOTE: This is a PARTIAL fill notification
	// The bot should accumulate these to track total fills
	updater <- &Trade{
		ID:          clientOrderID,
		Complete:    false, // We don't know from a deal if the order is complete
		Rate:        info.rate,
		Qty:         info.qty,
		Market:      info.market,
		BaseFilled:  baseFilled,  // This fill only
		QuoteFilled: quoteFilled, // This fill only
		BaseID:      info.baseID,
		QuoteID:     info.quoteID,
		Sell:        info.sell,
	}

	m.log.Debugf("Sent fill update for %s: baseFilled=%d, quoteFilled=%d", clientOrderID, baseFilled, quoteFilled)
}

// handlePrivateAccount handles private account updates
func (m *mexc) handlePrivateAccount(wrapper *pb.PushDataV3ApiWrapper) {
	account := wrapper.GetPrivateAccount()
	if account == nil {
		return
	}

	// Extract balance information
	coinName := account.GetVcoinName()
	updateType := account.GetType()

	// Log with more context
	m.log.Debugf("Private account update [%s] for %s: balance=%s (Δ%s), frozen=%s (Δ%s)",
		updateType, coinName,
		account.GetBalanceAmount(), account.GetBalanceAmountChange(),
		account.GetFrozenAmount(), account.GetFrozenAmountChange())

	if coinName == "" {
		return
	}

	// Parse balance amounts (absolute values, not deltas)
	balanceStr := account.GetBalanceAmount()
	frozenStr := account.GetFrozenAmount()

	balance, err := strconv.ParseFloat(balanceStr, 64)
	if err != nil {
		m.log.Warnf("Failed to parse balance amount %s: %v", balanceStr, err)
		return
	}

	frozen, err := strconv.ParseFloat(frozenStr, 64)
	if err != nil {
		m.log.Warnf("Failed to parse frozen amount %s: %v", frozenStr, err)
		return
	}

	// Map coin name to asset ID
	assetIDs := m.getDEXAssetIDs(coinName)
	if len(assetIDs) == 0 {
		m.log.Tracef("Unknown asset %s in balance update, skipping", coinName)
		return
	}

	// Get unit info for conversion
	assetID := assetIDs[0] // Use first matching asset ID
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		m.log.Warnf("Failed to get unit info for asset ID %d: %v", assetID, err)
		return
	}

	// Convert to atomic units
	available := uint64(balance * float64(ui.Conventional.ConversionFactor))
	locked := uint64(frozen * float64(ui.Conventional.ConversionFactor))

	// Update internal balance cache
	m.balanceMtx.Lock()
	m.balances[assetID] = &ExchangeBalance{
		Available: available,
		Locked:    locked,
	}
	m.balanceMtx.Unlock()

	m.log.Debugf("Updated balance for %s (asset %d): available=%d, locked=%d", coinName, assetID, available, locked)
}

// subscribePrivateChannels subscribes to all private WebSocket channels
func (m *mexc) subscribePrivateChannels() error {
	if m.privateWS == nil || m.privateWS.IsDown() {
		return fmt.Errorf("private websocket not connected")
	}

	// Subscribe to all private channels as per MEXC documentation
	// https://mexcdevelop.github.io/apidocs/spot_v3_en/#spot-account-orders
	// https://mexcdevelop.github.io/apidocs/spot_v3_en/#spot-account-deals
	channels := []string{
		"spot@private.orders.v3.api.pb",  // Order status updates
		"spot@private.deals.v3.api.pb",   // Trade execution (fills)
		"spot@private.account.v3.api.pb", // Balance updates
	}

	req := &mxctypes.WsRequest{
		Method: "SUBSCRIPTION",
		Params: channels,
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal subscription request: %w", err)
	}

	m.log.Tracef("Sending SUBSCRIPTION request: %s", string(b))
	if err := m.privateWS.SendRaw(b); err != nil {
		return fmt.Errorf("failed to send subscription request: %w", err)
	}

	m.log.Infof("Subscribed to %d private channels: %v", len(channels), channels)
	return nil
}

// onPrivateReconnect handles private WebSocket reconnection
func (m *mexc) onPrivateReconnect() {
	// Re-establish listen key and reconnect
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listenKey, err := m.ensureListenKey(ctx)
	if err != nil {
		m.log.Errorf("Failed to get listen key on reconnect: %v", err)
		return
	}

	// Update WebSocket URL with new listen key
	if m.privateWS != nil {
		newURL := mexcPrivateWSURL + "?listenKey=" + listenKey
		m.privateWS.UpdateURL(newURL)
		m.log.Infof("Updated private WS URL with new listen key: %s", listenKey)
		// Note: The WebSocket connection will be re-established by the comms layer

		// Re-subscribe to private channels after reconnect
		if err := m.subscribePrivateChannels(); err != nil {
			m.log.Errorf("Failed to resubscribe to private channels: %v", err)
		}
	}
}

func (m *mexc) startPing() {
	// MEXC WebSocket keep-alive strategy:
	// We send PING messages every 30 seconds on the private WebSocket to keep it alive.
	// The market WebSocket relies on server-initiated PINGs only.
	if m.pingQuit != nil {
		return
	}
	m.pingQuit = make(chan struct{})

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if m.privateWS == nil {
					continue
				}
				ping := map[string]string{"method": "PING"}
				b, err := json.Marshal(ping)
				if err != nil {
					m.log.Errorf("Failed to marshal PING: %v", err)
					continue
				}
				m.log.Tracef("Sending PING on private WS")
				if err := m.privateWS.SendRaw(b); err != nil {
					m.log.Warnf("Failed to send PING on private WS: %v", err)
				}
			case <-m.pingQuit:
				return
			}
		}
	}()
}

// symbolFor builds a MEXC symbol from asset IDs, using exchange info when available.
func (m *mexc) symbolFor(baseID, quoteID uint32) string {
	base := strings.ToUpper(dex.BipIDSymbol(baseID))
	quote := strings.ToUpper(dex.BipIDSymbol(quoteID))

	// Strip network suffixes
	if i := strings.Index(base, "."); i > 0 {
		base = base[:i]
	}
	if i := strings.Index(quote, "."); i > 0 {
		quote = quote[:i]
	}

	// Try to find exact match in exchange info
	candidate := base + quote
	if m.symbolExists(candidate) {
		return candidate
	}

	// Try common variations
	variations := []string{
		base + quote,
		base + "_" + quote,
		base + "-" + quote,
		base + "/" + quote,
	}

	for _, variation := range variations {
		if m.symbolExists(variation) {
			return variation
		}
	}

	// Fallback to naive concatenation
	return base + quote
}

func (m *mexc) ensureExchangeInfo(ctx context.Context) error {
	m.symbolsMtx.RLock()
	loaded := len(m.symbols) > 0
	m.symbolsMtx.RUnlock()

	// Check if API-tradeable symbols are also loaded
	tradeableLoaded := m.apiTradeableSymbols.Load() != nil

	if loaded && tradeableLoaded {
		return nil
	}

	// If symbols are loaded but tradeable list isn't, just fetch tradeable list
	if loaded && !tradeableLoaded {
		if err := m.getDefaultSymbols(ctx); err != nil {
			m.log.Errorf("Failed to fetch API-tradeable symbols: %v", err)
		}
		return nil
	}

	var info mxctypes.ExchangeInfo
	if err := m.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, &info); err != nil {
		return fmt.Errorf("failed to get exchange info: %w", err)
	}

	// Populate symbols cache
	syms := make(map[string]struct{}, len(info.Symbols))
	symbolMeta := make(map[string]*mxctypes.Symbol, len(info.Symbols))

	for _, s := range info.Symbols {
		symbol := strings.ToUpper(s.Symbol)
		syms[symbol] = struct{}{}
		symbolMeta[symbol] = &s
	}

	m.symbolsMtx.Lock()
	m.symbols = syms
	m.symbolsMtx.Unlock()

	m.symbolMetaMtx.Lock()
	m.symbolMeta = symbolMeta
	m.symbolMetaMtx.Unlock()

	// Fetch API-tradeable symbols immediately after exchange info
	// This ensures Markets() will have the tradeable list when called
	if err := m.getDefaultSymbols(ctx); err != nil {
		m.log.Errorf("Failed to fetch API-tradeable symbols: %v", err)
		// Don't fail - Markets() will use optimistic default (assume all tradeable)
	}

	return nil
}

func (m *mexc) symbolExists(sym string) bool {
	m.symbolsMtx.RLock()
	_, ok := m.symbols[strings.ToUpper(sym)]
	m.symbolsMtx.RUnlock()
	return ok
}

// getSymbolMeta returns the symbol metadata for a given symbol.
func (m *mexc) getSymbolMeta(symbol string) *mxctypes.Symbol {
	m.symbolMetaMtx.RLock()
	defer m.symbolMetaMtx.RUnlock()
	return m.symbolMeta[strings.ToUpper(symbol)]
}

// getFilterValue returns the value of a specific filter type for a symbol.
func (m *mexc) getFilterValue(symbol, filterType string) string {
	meta := m.getSymbolMeta(symbol)
	if meta == nil {
		return ""
	}
	for _, filter := range meta.Filters {
		if filter.FilterType == filterType {
			switch filterType {
			case "PRICE_FILTER":
				return filter.TickSize
			case "LOT_SIZE":
				return filter.StepSize
			case "MIN_NOTIONAL":
				return filter.MinNotional
			case "MIN_QTY":
				return filter.MinQty
			case "MAX_QTY":
				return filter.MaxQty
			}
		}
	}
	return ""
}

// snapPrice snaps a price to the tick size for a symbol.
func (m *mexc) snapPrice(symbol string, price float64) float64 {
	tickSizeStr := m.getFilterValue(symbol, "PRICE_FILTER")
	if tickSizeStr == "" {
		return price
	}
	tickSize, err := strconv.ParseFloat(tickSizeStr, 64)
	if err != nil {
		m.log.Errorf("Failed to parse tickSize '%s' for symbol %s: %v", tickSizeStr, symbol, err)
		return price
	}
	return float64(int64(price/tickSize+0.5)) * tickSize
}

// snapQuantity snaps a quantity to the step size for a symbol.
func (m *mexc) snapQuantity(symbol string, qty float64) float64 {
	stepSizeStr := m.getFilterValue(symbol, "LOT_SIZE")
	if stepSizeStr == "" {
		return qty
	}
	stepSize, err := strconv.ParseFloat(stepSizeStr, 64)
	if err != nil {
		m.log.Errorf("Failed to parse stepSize '%s' for symbol %s: %v", stepSizeStr, symbol, err)
		return qty
	}
	return float64(int64(qty/stepSize)) * stepSize
}

// validateMinNotional checks if the order meets the minimum notional value.
func (m *mexc) validateMinNotional(symbol string, price, qty float64) bool {
	minNotionalStr := m.getFilterValue(symbol, "MIN_NOTIONAL")
	if minNotionalStr == "" {
		return true
	}
	minNotional, err := strconv.ParseFloat(minNotionalStr, 64)
	if err != nil {
		m.log.Errorf("Failed to parse minNotional '%s' for symbol %s: %v", minNotionalStr, symbol, err)
		return true
	}
	return price*qty >= minNotional
}

// getListenKey requests a new listen key from MEXC API
func (m *mexc) getListenKey(ctx context.Context) (string, error) {
	var result struct {
		ListenKey string `json:"listenKey"`
	}
	if err := m.postAPI(ctx, "/api/v3/userDataStream", nil, true, &result); err != nil {
		return "", fmt.Errorf("failed to get listen key: %w", err)
	}
	return result.ListenKey, nil
}

// keepAliveListenKey extends the listen key validity
func (m *mexc) keepAliveListenKey(ctx context.Context, listenKey string) error {
	query := url.Values{}
	query.Set("listenKey", listenKey)
	var result struct{}
	if err := m.putAPI(ctx, "/api/v3/userDataStream", query, true, &result); err != nil {
		return fmt.Errorf("failed to keep alive listen key: %w", err)
	}
	return nil
}

// closeListenKey closes/deletes a listen key
func (m *mexc) closeListenKey(ctx context.Context, listenKey string) error {
	if listenKey == "" {
		return nil
	}
	query := url.Values{}
	query.Set("listenKey", listenKey)
	var result struct{}
	if err := m.deleteAPI(ctx, "/api/v3/userDataStream", query, true, &result); err != nil {
		return fmt.Errorf("failed to close listen key: %w", err)
	}
	m.log.Debugf("Closed listen key: %s", listenKey)
	return nil
}

// ensureListenKey ensures we have a valid listen key
func (m *mexc) ensureListenKey(ctx context.Context) (string, error) {
	m.listenKeyMtx.Lock()
	defer m.listenKeyMtx.Unlock()

	// Check if current key is still valid (with 5 minute buffer)
	if m.listenKey != "" && time.Now().Before(m.listenKeyExp.Add(-5*time.Minute)) {
		return m.listenKey, nil
	}

	// Close old listen key before creating a new one
	if m.listenKey != "" {
		oldKey := m.listenKey
		m.listenKey = "" // Clear it first in case closeListenKey fails
		if err := m.closeListenKey(ctx, oldKey); err != nil {
			m.log.Warnf("Failed to close old listen key: %v", err)
			// Continue anyway - we need a new key
		}
	}

	// Get new listen key
	listenKey, err := m.getListenKey(ctx)
	if err != nil {
		return "", err
	}

	m.listenKey = listenKey
	m.listenKeyExp = time.Now().Add(60 * time.Minute) // MEXC listen keys expire in 60 minutes

	// Start keepalive goroutine
	go m.startListenKeyKeepalive()

	return listenKey, nil
}

// startListenKeyKeepalive keeps the listen key alive
func (m *mexc) startListenKeyKeepalive() {
	m.listenKeyKeepaliveMtx.Lock()
	defer m.listenKeyKeepaliveMtx.Unlock()

	// Guard: Don't start if already running
	if m.listenKeyKeepaliveQuit != nil {
		m.log.Tracef("Listen key keepalive already running")
		return
	}

	m.listenKeyKeepaliveQuit = make(chan struct{})
	m.log.Debugf("Starting listen key keepalive goroutine")

	go func() {
		defer func() {
			m.listenKeyKeepaliveMtx.Lock()
			m.listenKeyKeepaliveQuit = nil
			m.listenKeyKeepaliveMtx.Unlock()
			m.log.Debugf("Listen key keepalive goroutine stopped")
		}()

		ticker := time.NewTicker(30 * time.Minute) // Keep alive every 30 minutes
		defer ticker.Stop()

		for {
			select {
			case <-m.listenKeyKeepaliveQuit:
				return
			case <-ticker.C:
				m.listenKeyMtx.RLock()
				key := m.listenKey
				m.listenKeyMtx.RUnlock()

				if key == "" {
					continue
				}

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := m.keepAliveListenKey(ctx, key); err != nil {
					m.log.Errorf("Failed to keep alive listen key: %v", err)
				} else {
					m.log.Debugf("Listen key keep-alive sent successfully")
				}
				cancel()
			}
		}
	}()
}

func (m *mexc) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	m.tradeUpdaterMtx.Lock()
	defer m.tradeUpdaterMtx.Unlock()

	id := m.tradeUpdateCounter
	m.tradeUpdateCounter++

	ch := make(chan *Trade, 256)
	m.tradeUpdaters[id] = ch

	unsub := func() {
		m.tradeUpdaterMtx.Lock()
		delete(m.tradeUpdaters, id)
		m.tradeUpdaterMtx.Unlock()
		close(ch)
	}

	return ch, unsub, id
}

func (m *mexc) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType, subscriptionID int) (*Trade, error) {
	symbol := m.symbolFor(baseID, quoteID)

	// Validate trade parameters FIRST to catch minimum notional violations early
	if err := m.ValidateTrade(baseID, quoteID, sell, rate, qty, quoteQty, orderType); err != nil {
		return nil, fmt.Errorf("trade validation failed: %w", err)
	}

	// Ensure exchange info is loaded
	if err := m.ensureExchangeInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to load exchange info: %w", err)
	}

	// Check if symbol exists
	if !m.symbolExists(symbol) {
		return nil, fmt.Errorf("symbol %s not found", symbol)
	}

	// Verify subscription ID
	m.tradeUpdaterMtx.RLock()
	_, found := m.tradeUpdaters[subscriptionID]
	m.tradeUpdaterMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no trade updater with ID %v", subscriptionID)
	}

	// Get unit info for proper conversion
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting base unit info: %v", err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting quote unit info: %v", err)
	}

	// Generate trade ID
	tradeID := m.generateTradeID()

	// Build order request
	query := url.Values{}
	query.Add("symbol", symbol)
	query.Add("newClientOrderId", tradeID)

	if sell {
		query.Add("side", "SELL")
	} else {
		query.Add("side", "BUY")
	}

	// Order type
	var market bool
	switch orderType {
	case OrderTypeMarket:
		query.Add("type", "MARKET")
		market = true
	case OrderTypeLimit:
		query.Add("type", "LIMIT")
		// Convert rate from message-rate encoding to conventional rate
		price := calc.ConventionalRateAlt(rate, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
		price = m.snapPrice(symbol, price)
		query.Add("price", strconv.FormatFloat(price, 'f', -1, 64))
	case OrderTypeLimitIOC:
		query.Add("type", "IMMEDIATE_OR_CANCEL")
		price := calc.ConventionalRateAlt(rate, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
		price = m.snapPrice(symbol, price)
		query.Add("price", strconv.FormatFloat(price, 'f', -1, 64))
	default:
		return nil, fmt.Errorf("unsupported order type: %v", orderType)
	}

	// Quantity
	var qtyToReturn uint64
	if quoteQty > 0 && !sell {
		// Market buy with quote quantity - convert using quote conversion factor
		if orderType != OrderTypeMarket {
			return nil, fmt.Errorf("quoteQty only allowed for market buy orders")
		}
		quoteQtyFloat := float64(quoteQty) / float64(qui.Conventional.ConversionFactor)
		query.Add("quoteOrderQty", strconv.FormatFloat(quoteQtyFloat, 'f', -1, 64))
		qtyToReturn = quoteQty
	} else if qty > 0 {
		// Base quantity - convert using base conversion factor
		qtyFloat := float64(qty) / float64(bui.Conventional.ConversionFactor)
		qtyFloat = m.snapQuantity(symbol, qtyFloat)
		query.Add("quantity", strconv.FormatFloat(qtyFloat, 'f', -1, 64))
		qtyToReturn = uint64(qtyFloat * float64(bui.Conventional.ConversionFactor))
	} else {
		return nil, fmt.Errorf("either qty or quoteQty must be specified")
	}

	// Validate the trade
	if err := m.ValidateTrade(baseID, quoteID, sell, rate, qtyToReturn, quoteQty, orderType); err != nil {
		return nil, fmt.Errorf("trade validation failed: %w", err)
	}

	// Store trade info for tracking
	m.tradeUpdaterMtx.Lock()
	m.tradeInfo[tradeID] = &mexcTradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
		sell:      sell,
		rate:      rate,
		qty:       qtyToReturn,
		market:    market,
	}
	m.tradeUpdaterMtx.Unlock()

	var success bool
	defer func() {
		if !success {
			m.tradeUpdaterMtx.Lock()
			delete(m.tradeInfo, tradeID)
			m.tradeUpdaterMtx.Unlock()
		}
	}()

	// Place the order
	var orderResp mxctypes.OrderResponse
	if err := m.postAPI(ctx, "/api/v3/order", query, true, &orderResp); err != nil {
		return nil, fmt.Errorf("error placing order: %w", err)
	}

	success = true

	m.log.Infof("Order placed: %s %s %s @ %s (ID: %s, ClientID: %s)",
		orderResp.Side, orderResp.OrigQty, orderResp.Symbol, orderResp.Price,
		orderResp.OrderID, orderResp.ClientOrderID)

	// Return Trade
	return &Trade{
		ID:      orderResp.ClientOrderID,
		Sell:    sell,
		Rate:    rate,
		Qty:     qtyToReturn,
		BaseID:  baseID,
		QuoteID: quoteID,
		Market:  market,
	}, nil
}

func (m *mexc) ValidateTrade(baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType) error {
	symbol := m.symbolFor(baseID, quoteID)

	// Ensure exchange info is loaded
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.ensureExchangeInfo(ctx); err != nil {
		return fmt.Errorf("failed to load exchange info: %v", err)
	}

	// Check if symbol exists
	if !m.symbolExists(symbol) {
		return fmt.Errorf("symbol %s not found", symbol)
	}

	// Get unit info for proper conversion
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("error getting base unit info: %v", err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("error getting quote unit info: %v", err)
	}

	// Convert rate from message-rate encoding to conventional rate
	price := calc.ConventionalRateAlt(rate, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)

	var quantity float64
	if qty > 0 {
		// Base quantity: convert using base conversion factor
		quantity = float64(qty) / float64(bui.Conventional.ConversionFactor)
	} else if quoteQty > 0 {
		// Quote quantity: convert using quote conversion factor
		quantity = float64(quoteQty) / float64(qui.Conventional.ConversionFactor)
	} else {
		return fmt.Errorf("either qty or quoteQty must be specified")
	}

	// Snap values to exchange requirements
	price = m.snapPrice(symbol, price)
	quantity = m.snapQuantity(symbol, quantity)

	// Validate minimum notional value
	if !m.validateMinNotional(symbol, price, quantity) {
		minNotionalStr := m.getFilterValue(symbol, "MIN_NOTIONAL")
		return fmt.Errorf("order value %.8f below minimum notional %s", price*quantity, minNotionalStr)
	}

	// Validate minimum quantity
	minQtyStr := m.getFilterValue(symbol, "MIN_QTY")
	if minQtyStr != "" {
		if minQty, err := strconv.ParseFloat(minQtyStr, 64); err == nil {
			if quantity < minQty {
				return fmt.Errorf("quantity %.8f below minimum %s", quantity, minQtyStr)
			}
		}
	}

	// Validate maximum quantity
	maxQtyStr := m.getFilterValue(symbol, "MAX_QTY")
	if maxQtyStr != "" {
		if maxQty, err := strconv.ParseFloat(maxQtyStr, 64); err == nil {
			if quantity > maxQty {
				return fmt.Errorf("quantity %.8f above maximum %s", quantity, maxQtyStr)
			}
		}
	}

	return nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (m *mexc) UnsubscribeMarket(baseID, quoteID uint32) error {
	m.subMarketMtx.Lock()
	defer m.subMarketMtx.Unlock()

	symbol := m.symbolFor(baseID, quoteID)

	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return fmt.Errorf("no book found for symbol: %v", symbol)
	}

	book.mtx.Lock()
	book.numSubscribers--
	numSubs := book.numSubscribers
	book.mtx.Unlock()

	if numSubs == 0 {
		// No more subscribers, clean up
		m.booksMtx.Lock()
		delete(m.books, symbol)
		numBooks := len(m.books)
		m.booksMtx.Unlock()

		// Disconnect the book's connection master
		if book.cm != nil {
			go book.cm.Disconnect()
		}

		// Unsubscribe from both WebSocket topics
		depthTopic := mxctypes.PublicLimitDepthTopic(symbol, "20")
		tradesTopic := mxctypes.PublicAggreDealsTopic(symbol, "100ms")
		if m.marketWS != nil && !m.marketWS.IsDown() {
			req := &mxctypes.WsRequest{
				Method: "UNSUBSCRIPTION",
				Params: []string{depthTopic, tradesTopic},
			}
			b, err := json.Marshal(req)
			if err == nil {
				m.marketWS.SendRaw(b)
			}
		}

		m.activeTopicsMtx.Lock()
		delete(m.activeTopics, depthTopic)
		delete(m.activeTopics, tradesTopic)
		numTopics := len(m.activeTopics)
		m.activeTopicsMtx.Unlock()

		m.log.Infof("Unsubscribed from %s order book", symbol)

		// If no more books/topics, close the market WebSocket connection
		if numBooks == 0 && numTopics == 0 {
			m.log.Infof("No more market subscriptions, closing market WebSocket")
			m.mtx.Lock()
			if m.marketWSCancel != nil {
				m.marketWSCancel()
				m.marketWS = nil
				m.marketWSWg = nil
				m.marketWSCancel = nil
				m.marketWSContext = nil
			}
			m.mtx.Unlock()
		}
	}

	return nil
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market. SubscribeMarket must be called first.
func (m *mexc) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (uint64, uint64, bool, error) {
	symbol := m.symbolFor(baseID, quoteID)

	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return 0, 0, false, fmt.Errorf("market not subscribed: %s", symbol)
	}

	return book.vwap(!sell, qty)
}

// InvVWAP returns the inverse volume weighted average price for a certain
// quantity of the quote asset on a market. SubscribeMarket must be called first.
func (m *mexc) InvVWAP(baseID, quoteID uint32, sell bool, qty uint64) (uint64, uint64, bool, error) {
	symbol := m.symbolFor(baseID, quoteID)

	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return 0, 0, false, fmt.Errorf("market not subscribed: %s", symbol)
	}

	return book.invVWAP(!sell, qty)
}

// MidGap returns the mid-gap market rate. SubscribeMarket must be called first.
func (m *mexc) MidGap(baseID, quoteID uint32) uint64 {
	symbol := m.symbolFor(baseID, quoteID)

	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return 0
	}

	return book.midGap()
}

func (m *mexc) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	// Get asset symbol
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return "", fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	// dexSymbolToMexcAsset strips network suffix: "usdt.polygon" -> "USDT"
	coin := dexSymbolToMexcAsset(symbol)
	network, err := m.getNetworkForAsset(assetID)
	if err != nil {
		return "", err
	}

	m.log.Debugf("GetDepositAddress: assetID=%d, symbol=%s, coin=%s, network=%s",
		assetID, symbol, coin, network)

	// MEXC API quirk: Querying with network parameter sometimes returns empty results,
	// but querying without network returns all addresses. So we query without network
	// and filter the results to find the matching network.
	query := url.Values{}
	query.Add("coin", coin)
	// Note: NOT adding network parameter - MEXC returns empty when network is specified

	// MEXC returns an array of deposit addresses
	var respArray []mxctypes.DepositAddressResponse
	if err := m.getAPI(ctx, "/api/v3/capital/deposit/address", query, true, &respArray); err != nil {
		return "", fmt.Errorf("error getting deposit address: %w", err)
	}

	m.log.Debugf("GetDepositAddress API response: %d addresses returned for coin=%s",
		len(respArray), coin)

	if len(respArray) == 0 {
		return "", fmt.Errorf("no deposit address returned for %s", coin)
	}

	// Filter to find the address for the desired network
	for _, addr := range respArray {
		// Case-insensitive network match
		if strings.EqualFold(addr.Network, network) {
			m.log.Debugf("  Found address for network %s: %s (tag: %s)", addr.Network, addr.Address, addr.Tag)
			// Some assets require a memo/tag (e.g., XRP, XLM)
			address := addr.Address
			if addr.Tag != "" {
				address = fmt.Sprintf("%s?memoId=%s", addr.Address, addr.Tag)
			}
			return address, nil
		}
	}

	// Network not found in the list - log available networks for debugging
	availableNetworks := make([]string, 0, len(respArray))
	for _, addr := range respArray {
		availableNetworks = append(availableNetworks, addr.Network)
	}
	m.log.Debugf("Network %s not found for %s. Available networks: %v", network, coin, availableNetworks)

	return "", fmt.Errorf("no deposit address returned for %s on network %s (available: %v)", coin, network, availableNetworks)
}

func (m *mexc) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	// Get asset symbol
	symbol := dex.BipIDSymbol(deposit.AssetID)
	if symbol == "" {
		m.log.Errorf("no symbol found for asset ID %d", deposit.AssetID)
		return false, 0
	}

	coin := dexSymbolToMexcAsset(symbol)

	query := url.Values{}
	query.Add("coin", coin)
	// Optionally add txId filter
	if deposit.TxID != "" {
		query.Add("txId", deposit.TxID)
	}

	var resp []mxctypes.DepositHistoryItem
	if err := m.getAPI(ctx, "/api/v3/capital/deposit/hisrec", query, true, &resp); err != nil {
		m.log.Errorf("error getting deposit history: %v", err)
		return false, 0
	}

	m.log.Debugf("ConfirmDeposit for txID %s: got %d deposit records", deposit.TxID, len(resp))

	// Find the matching deposit
	for _, item := range resp {

		// Match exact txID or txID with output index (MEXC format: "txhash:index")
		isMatch := item.TxID == deposit.TxID || strings.HasPrefix(item.TxID, deposit.TxID+":")

		if isMatch {
			m.log.Infof("Found matching deposit for txID %s (MEXC txID: %s): status=%d", deposit.TxID, item.TxID, item.Status)

			// MEXC Deposit Status Codes:
			// 1: SMALL (small deposit not credited)
			// 2: TIME_DELAY (delayed credit)
			// 3: LARGE_DELAY (large deposit delay)
			// 4: PENDING (awaiting confirmations)
			// 5: SUCCESS (credited successfully)
			// 6: AUDITING (under review)
			// 7: REJECTED (rejected)
			// 8: REFUND (forced deposit return)
			// 9: PRE_SUCCESS (pre-credited)
			// 10: INVALID (invalid deposit)
			// 11: RESTRICTED (restricted)
			// 12: COMPLETED (completed successfully)
			switch item.Status {
			case 5, 12: // SUCCESS or COMPLETED - deposit credited successfully
				// Parse amount
				amount, err := strconv.ParseFloat(item.Amount, 64)
				if err != nil {
					m.log.Errorf("error parsing deposit amount: %v", err)
					return true, 0
				}

				// Get unit info to convert to atomic units
				ui, err := asset.UnitInfo(deposit.AssetID)
				if err != nil {
					m.log.Errorf("error getting unit info for asset %d: %v", deposit.AssetID, err)
					return true, 0
				}

				atomicAmount := uint64(amount * float64(ui.Conventional.ConversionFactor))
				m.log.Infof("Deposit confirmed: %s %f credited (status=%d)", item.Coin, amount, item.Status)
				return true, atomicAmount
			case 9: // PRE_SUCCESS - pre-credited (also considered successful)
				// Parse amount
				amount, err := strconv.ParseFloat(item.Amount, 64)
				if err != nil {
					m.log.Errorf("error parsing deposit amount: %v", err)
					return true, 0
				}
				ui, err := asset.UnitInfo(deposit.AssetID)
				if err != nil {
					m.log.Errorf("error getting unit info for asset %d: %v", deposit.AssetID, err)
					return true, 0
				}
				atomicAmount := uint64(amount * float64(ui.Conventional.ConversionFactor))
				m.log.Infof("Deposit pre-credited: %s %f (status=9)", item.Coin, amount)
				return true, atomicAmount
			case 1, 2, 3, 4, 6: // Pending states (small, delayed, pending, auditing)
				m.log.Debugf("Deposit pending (status=%d): txID %s", item.Status, deposit.TxID)
				return false, 0
			case 7, 8, 10, 11: // Failed states (rejected, refund, invalid, restricted)
				m.log.Warnf("Deposit failed/rejected (status=%d): txID %s", item.Status, deposit.TxID)
				return true, 0
			default:
				m.log.Errorf("unknown deposit status %d for txID %s", item.Status, deposit.TxID)
				return false, 0
			}
		}
	}

	// Deposit not found yet
	m.log.Debugf("Deposit not found in history: txID %s", deposit.TxID)
	return false, 0
}

// getWhitelistedNetwork returns the network name that MEXC has on file for a whitelisted withdrawal address.
// Returns the exact network string that should be used in the withdrawal API call.
func (m *mexc) getWhitelistedNetwork(ctx context.Context, coin, address string) (string, error) {
	query := url.Values{}
	query.Add("coin", coin)

	type WithdrawAddressItem struct {
		Coin       string `json:"coin"`
		Network    string `json:"network"`
		Address    string `json:"address"`
		AddressTag string `json:"addressTag,omitempty"`
	}

	var resp struct {
		Data []WithdrawAddressItem `json:"data"`
	}

	if err := m.getAPI(ctx, "/api/v3/capital/withdraw/address", query, true, &resp); err != nil {
		return "", fmt.Errorf("error fetching withdrawal addresses: %w", err)
	}

	// Normalize the target address for comparison (lowercase for EVM addresses)
	normalizedTarget := strings.ToLower(strings.TrimSpace(address))

	// Find the address and return its network
	for _, item := range resp.Data {
		normalizedItem := strings.ToLower(strings.TrimSpace(item.Address))
		if normalizedItem == normalizedTarget {
			return item.Network, nil
		}
	}

	if len(resp.Data) == 0 {
		return "", fmt.Errorf("no withdrawal addresses configured for %s. Please add withdrawal address %s via MEXC website/app", coin, address)
	}

	return "", fmt.Errorf("withdrawal address %s not whitelisted for %s. Please add it via MEXC website/app", address, coin)
}

// verifyWithdrawalAddress checks if the given address is whitelisted in MEXC for withdrawals.
// MEXC requires addresses to be pre-registered by the user for security purposes when whitelisting is enabled.
func (m *mexc) verifyWithdrawalAddress(ctx context.Context, coin, network, address string) error {
	query := url.Values{}
	query.Add("coin", coin)

	type WithdrawAddressItem struct {
		Coin       string `json:"coin"`
		Network    string `json:"network"`
		Address    string `json:"address"`
		AddressTag string `json:"addressTag,omitempty"`
	}

	var resp struct {
		Data []WithdrawAddressItem `json:"data"`
	}

	if err := m.getAPI(ctx, "/api/v3/capital/withdraw/address", query, true, &resp); err != nil {
		return fmt.Errorf("error fetching withdrawal addresses: %w", err)
	}

	// Normalize the target address for comparison (lowercase for EVM addresses)
	normalizedTarget := strings.ToLower(strings.TrimSpace(address))

	// Check if the address exists for the specified network
	for _, item := range resp.Data {
		if !strings.EqualFold(item.Network, network) {
			continue
		}

		normalizedItem := strings.ToLower(strings.TrimSpace(item.Address))
		if normalizedItem == normalizedTarget {
			m.log.Debugf("Withdrawal address %s verified for %s on network %s", address, coin, network)
			return nil
		}
	}

	// Address not found - provide helpful error message
	availableNetworks := make([]string, 0)
	for _, item := range resp.Data {
		availableNetworks = append(availableNetworks, item.Network)
	}

	if len(resp.Data) == 0 {
		return fmt.Errorf("no withdrawal addresses configured for %s. Please add withdrawal address %s for network %s via MEXC website/app", coin, address, network)
	}

	return fmt.Errorf("withdrawal address %s not whitelisted for %s on network %s. Please add it via MEXC website/app. Available networks: %v", address, coin, network, availableNetworks)
}

func (m *mexc) Withdraw(ctx context.Context, assetID uint32, amt uint64, address string) (string, uint64, error) {
	// Get asset symbol
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return "", 0, fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	coin := dexSymbolToMexcAsset(symbol)
	network, err := m.getNetworkForAsset(assetID)
	if err != nil {
		return "", 0, err
	}

	// Check if withdrawals are enabled for this coin/network combination
	coinInfos, err := m.getCoinInfo(ctx)
	if err != nil {
		m.log.Warnf("Could not verify withdrawal availability: %v", err)
	} else {
		withdrawEnabled := false
		var availableNetworks []string
		var networkInfo *mxctypes.NetworkInfo
		for _, coinInfo := range coinInfos {
			if coinInfo.Coin == coin {
				for _, net := range coinInfo.NetworkList {
					if net.WithdrawEnable {
						availableNetworks = append(availableNetworks, net.Network)
						if net.Network == network {
							withdrawEnabled = true
							networkInfo = &net
							break
						}
					}
				}
				break
			}
		}
		if !withdrawEnabled {
			if len(availableNetworks) > 0 {
				return "", 0, fmt.Errorf("withdrawals not enabled for %s on network %s. Available withdrawal networks: %v", coin, network, availableNetworks)
			}
			return "", 0, fmt.Errorf("withdrawals not enabled for %s on network %s", coin, network)
		}
		if networkInfo != nil {
			m.log.Debugf("Withdrawal network info for %s on %s: min=%s, max=%s, fee=%s, integerMultiple=%s",
				coin, network, networkInfo.WithdrawMin, networkInfo.WithdrawMax, networkInfo.WithdrawFee, networkInfo.WithdrawIntegerMultiple)
		}
	}

	// Check if withdrawal address is whitelisted in MEXC and get the exact network name MEXC expects.
	actualNetwork, err := m.getWhitelistedNetwork(ctx, coin, address)
	if err != nil {
		m.log.Warnf("Could not verify withdrawal address network (will attempt with %s): %v", network, err)
	} else if actualNetwork != network {
		m.log.Infof("Withdrawal address whitelisted with network %q, adjusting from %q", actualNetwork, network)
		network = actualNetwork
	} else {
		m.log.Debugf("Withdrawal address %s verified for %s on network %s", address, coin, network)
	}

	// Get unit info for conversion
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting unit info for asset %d: %w", assetID, err)
	}

	// Validate and step withdrawal amount
	info, err := m.withdrawInfo(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("cannot verify withdrawal minimum for asset %d (%s): %w. Check if deposits/withdrawals are enabled on MEXC for this network", assetID, symbol, err)
	}

	if amt < info.minimum {
		convAmt := float64(amt) / float64(ui.Conventional.ConversionFactor)
		convMin := float64(info.minimum) / float64(ui.Conventional.ConversionFactor)
		return "", 0, fmt.Errorf("withdrawal amount %.8f %s is below MEXC minimum %.8f %s. Configure AutoRebalanceConfig.MinBaseTransfer or MinQuoteTransfer >= %d atomic units",
			convAmt, symbol, convMin, symbol, info.minimum)
	}

	// Step to lot size like binance does
	amt = steppedQty(amt, info.lotSize)

	// Convert from atomic units to conventional
	amountConv := float64(amt) / float64(ui.Conventional.ConversionFactor)

	// Format with appropriate precision
	// Use 8 decimal places as default, but this could be adjusted per asset
	amountStr := strconv.FormatFloat(amountConv, 'f', 8, 64)

	m.log.Debugf("Withdraw: assetID=%d, symbol=%s, coin=%s, network=%s, amount=%s, address=%s",
		assetID, symbol, coin, network, amountStr, address)

	query := url.Values{}
	query.Add("coin", coin)
	query.Add("netWork", network) // Note: MEXC uses "netWork" (capital W) as of 2024-06-09
	query.Add("address", address)
	query.Add("amount", amountStr)

	var resp mxctypes.WithdrawResponse
	if err := m.postAPI(ctx, "/api/v3/capital/withdraw", query, true, &resp); err != nil {
		return "", 0, fmt.Errorf("error submitting withdrawal: %w", err)
	}

	m.log.Infof("Withdrawal submitted: %s %f %s to %s (ID: %s)",
		coin, amountConv, network, address, resp.ID)

	return resp.ID, amt, nil
}

func (m *mexc) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	// Get asset symbol
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return 0, "", fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	coin := dexSymbolToMexcAsset(symbol)

	query := url.Values{}
	query.Add("coin", coin)

	var resp []mxctypes.WithdrawHistoryItem
	if err := m.getAPI(ctx, "/api/v3/capital/withdraw/history", query, true, &resp); err != nil {
		return 0, "", fmt.Errorf("error getting withdrawal history: %w", err)
	}

	m.log.Debugf("ConfirmWithdrawal for ID %s: got %d withdrawal records", withdrawalID, len(resp))

	// Find the matching withdrawal
	for _, item := range resp {
		m.log.Debugf("Checking withdrawal: id=%s, coin=%s, status=%d, txID=%s",
			item.ID, item.Coin, item.Status, item.TxID)

		if item.ID == withdrawalID {
			m.log.Infof("Found matching withdrawal for ID %s: status=%d, txID=%s", withdrawalID, item.Status, item.TxID)

			// MEXC Withdrawal Status Codes:
			// 1: APPLY (applied, awaiting audit)
			// 2: AUDITING (under audit)
			// 3: WAIT (approved, waiting to be processed)
			// 4: PROCESSING (being processed)
			// 5: WAIT_PACKAGING (waiting to be packaged into block)
			// 6: WAIT_CONFIRM (on blockchain, waiting for confirmations)
			// 7: SUCCESS (completed successfully)
			// 8: FAILED (failed)
			// 9: CANCEL (cancelled)
			// 10: MANUAL (manual processing)
			switch item.Status {
			case 7: // SUCCESS - completed successfully
				if item.TxID == "" {
					return 0, "", ErrWithdrawalPending
				}

				// Parse amount
				amount, err := strconv.ParseFloat(item.Amount, 64)
				if err != nil {
					return 0, "", fmt.Errorf("error parsing withdrawal amount: %w", err)
				}

				// Get unit info to convert to atomic units
				ui, err := asset.UnitInfo(assetID)
				if err != nil {
					return 0, "", fmt.Errorf("error getting unit info for asset %d: %w", assetID, err)
				}

				atomicAmount := uint64(amount * float64(ui.Conventional.ConversionFactor))
				m.log.Infof("Withdrawal confirmed: %s %f, txID=%s", item.Coin, amount, item.TxID)
				return atomicAmount, item.TxID, nil
			case 1, 2, 3, 4, 5, 6, 10: // Pending states (awaiting completion)
				m.log.Debugf("Withdrawal pending (status=%d): ID %s", item.Status, withdrawalID)
				return 0, "", ErrWithdrawalPending
			case 8: // FAILED
				m.log.Warnf("Withdrawal failed: ID %s, reason: %s", withdrawalID, item.Info)
				return 0, "", fmt.Errorf("withdrawal failed: %s", item.Info)
			case 9: // CANCEL
				m.log.Warnf("Withdrawal cancelled: ID %s", withdrawalID)
				return 0, "", fmt.Errorf("withdrawal cancelled")
			default:
				m.log.Errorf("Unknown withdrawal status %d for ID %s", item.Status, withdrawalID)
				return 0, "", fmt.Errorf("unknown withdrawal status %d", item.Status)
			}
		}
	}

	m.log.Debugf("Withdrawal not found in history: ID %s", withdrawalID)
	return 0, "", fmt.Errorf("withdrawal %s not found", withdrawalID)
}

func (m *mexc) TradeStatus(ctx context.Context, id string, baseID, quoteID uint32) (*Trade, error) {
	symbol := m.symbolFor(baseID, quoteID)

	query := url.Values{}
	query.Add("symbol", symbol)
	query.Add("origClientOrderId", id)

	var orderStatus mxctypes.OrderStatusResponse
	if err := m.getAPI(ctx, "/api/v3/order", query, true, &orderStatus); err != nil {
		return nil, fmt.Errorf("error getting order status: %w", err)
	}

	// Get trade info if available
	m.tradeUpdaterMtx.RLock()
	info, hasInfo := m.tradeInfo[id]
	m.tradeUpdaterMtx.RUnlock()

	// Parse quantities and prices
	executedQty, _ := strconv.ParseFloat(orderStatus.ExecutedQty, 64)
	cumulativeQuoteQty, _ := strconv.ParseFloat(orderStatus.CummulativeQuoteQty, 64)
	price, _ := strconv.ParseFloat(orderStatus.Price, 64)

	var avgPrice float64
	if executedQty > 0 && cumulativeQuoteQty > 0 {
		avgPrice = cumulativeQuoteQty / executedQty
	} else if price > 0 {
		avgPrice = price
	}

	// Get unit info for proper conversion
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for base asset ID %d: %w", baseID, err)
	}

	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for quote asset ID %d: %w", quoteID, err)
	}

	// Convert to atomic units using correct conversion factors
	baseFilled := uint64(executedQty * float64(bui.Conventional.ConversionFactor))
	quoteFilled := uint64(cumulativeQuoteQty * float64(qui.Conventional.ConversionFactor))

	// Calculate rate in atomic units (quote per base)
	var rate uint64
	if executedQty > 0 && avgPrice > 0 {
		rate = calc.MessageRateAlt(avgPrice, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
	}

	// Determine sell flag
	sell := orderStatus.Side == "SELL"
	if hasInfo {
		sell = info.sell
		if rate == 0 && info.rate > 0 {
			rate = info.rate
		}
	}

	// Parse status
	complete := false
	switch orderStatus.Status {
	case "FILLED":
		complete = true
	case "CANCELED", "REJECTED", "EXPIRED":
		complete = true
	case "NEW", "PARTIALLY_FILLED":
		complete = false
	}

	return &Trade{
		ID:          id,
		Sell:        sell,
		Rate:        rate,
		Qty:         baseFilled,
		BaseID:      baseID,
		QuoteID:     quoteID,
		Complete:    complete,
		Market:      orderStatus.Type == "MARKET",
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
	}, nil
}

// Book returns the current order book. SubscribeMarket must be called first.
func (m *mexc) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	symbol := m.symbolFor(baseID, quoteID)

	m.booksMtx.RLock()
	book, exists := m.books[symbol]
	m.booksMtx.RUnlock()

	if !exists {
		return nil, nil, fmt.Errorf("market not subscribed: %s", symbol)
	}

	if !book.synced.Load() {
		return nil, nil, fmt.Errorf("order book not synced for %s", symbol)
	}

	bids, asks := book.book.snap()
	bFactor := book.baseConversionFactor
	qFactor := book.quoteConversionFactor

	buys = convertBookSide(bids, false, bFactor, qFactor)
	sells = convertBookSide(asks, true, bFactor, qFactor)
	return buys, sells, nil
}

// convertBookSide converts order book entries to core.MiniOrder format
func convertBookSide(side []*obEntry, sell bool, baseFactor, quoteFactor uint64) []*core.MiniOrder {
	ords := make([]*core.MiniOrder, len(side))
	for i, e := range side {
		ords[i] = &core.MiniOrder{
			Qty:       float64(e.qty) / float64(baseFactor),
			QtyAtomic: e.qty,
			Rate:      calc.ConventionalRateAlt(e.rate, baseFactor, quoteFactor),
			MsgRate:   e.rate,
			Sell:      sell,
		}
	}
	return ords
}
