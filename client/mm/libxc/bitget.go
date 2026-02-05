// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

// Bitget Spot v2 CEX Integration
//
// API ENDPOINTS:
// REST: https://api.bitget.com
// WebSocket: wss://ws.bitget.com/v2/ws/public (public)
//            wss://ws.bitget.com/v2/ws/private (private)
// Docs: https://www.bitget.com/api-doc/spot/intro
//
// WEBSOCKET CHANNELS:
// Public: books (orderbook updates)
// Private: orders (trade execution updates), account (balance updates)

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/big"
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
	"decred.org/dcrdex/client/mm/libxc/bgtypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/encode"
	"github.com/huandu/skiplist"
)

const (
	bitgetAPIURL       = "https://api.bitget.com"
	bitgetPublicWSURL  = "wss://ws.bitget.com/v2/ws/public"
	bitgetPrivateWSURL = "wss://ws.bitget.com/v2/ws/private"

	// Order statuses (Bitget Spot Trading API)
	bitgetOrderStatusLive        = "live"             // Pending match
	bitgetOrderStatusPartialFill = "partially_filled" // Partially filled
	bitgetOrderStatusFullFill    = "filled"           // Fully filled
	bitgetOrderStatusCancelled   = "cancelled"        // Cancelled

	// Order sides
	bitgetOrderSideBuy  = "buy"
	bitgetOrderSideSell = "sell"

	// Order types
	bitgetOrderTypeLimit  = "limit"
	bitgetOrderTypeMarket = "market"

	// Time in force
	bitgetForceGTC = "GTC" // Good Till Cancel
	bitgetForceIOC = "IOC" // Immediate Or Cancel
	bitgetForceFOK = "FOK" // Fill Or Kill

	// WebSocket operations
	wsOpSubscribe   = "subscribe"
	wsOpUnsubscribe = "unsubscribe"
	wsOpLogin       = "login"
	wsOpPing        = "ping"
	wsOpPong        = "pong"

	// WebSocket channels
	wsChannelBooks   = "books"
	wsChannelOrders  = "orders"
	wsChannelAccount = "account"

	// Instrument types
	instTypeSpot = "SPOT"
)

// bitgetOrderBook manages an order book for a single market.
// It uses a unified entry type that stores both string (for checksum) and uint64 (for VWAP)
// representations, parsing strings only once on insertion.
type bitgetOrderBook struct {
	mtx            sync.RWMutex
	synced         atomic.Bool
	numSubscribers uint32
	cm             *dex.ConnectionMaster

	symbol      string
	bids        *skiplist.SkipList // *bitgetObEntry, sorted descending by rate
	asks        *skiplist.SkipList // *bitgetObEntry, sorted ascending by rate
	updateQueue chan *bgtypes.BookUpdate

	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger

	connectedChan chan bool

	getSnapshot func() (*bgtypes.OrderbookSnapshot, error)

	// Sequence tracking for data integrity
	lastSeq                atomic.Int64 // Last processed sequence number
	lastUpdateTs           atomic.Int64 // Last update timestamp (ms)
	lastChecksumValidation atomic.Int64 // Last checksum validation time (ms)
}

// bitgetObEntry stores both string (for checksum) and uint64 (for VWAP) representations.
// Strings are parsed once on insertion.
type bitgetObEntry struct {
	priceStr string // Original string for checksum calculation
	qtyStr   string // Original string for checksum calculation
	rate     uint64 // Converted rate for VWAP calculations
	qty      uint64 // Converted quantity for VWAP calculations
}

// bitgetBidsComparable sorts bids descending by rate
type bitgetBidsComparable struct{}

func (bitgetBidsComparable) Compare(lhs, rhs any) int {
	l, r := lhs.(*bitgetObEntry), rhs.(*bitgetObEntry)
	if l.rate > r.rate {
		return -1
	}
	if l.rate < r.rate {
		return 1
	}
	return 0
}

func (bitgetBidsComparable) CalcScore(key any) float64 {
	return -float64(key.(*bitgetObEntry).rate)
}

// bitgetAsksComparable sorts asks ascending by rate
type bitgetAsksComparable struct{}

func (bitgetAsksComparable) Compare(lhs, rhs any) int {
	l, r := lhs.(*bitgetObEntry), rhs.(*bitgetObEntry)
	if l.rate < r.rate {
		return -1
	}
	if l.rate > r.rate {
		return 1
	}
	return 0
}

func (bitgetAsksComparable) CalcScore(key any) float64 {
	return float64(key.(*bitgetObEntry).rate)
}

// newBitgetOrderBook creates a new order book for the given symbol.
func newBitgetOrderBook(
	baseConversionFactor, quoteConversionFactor uint64,
	symbol string,
	getSnapshot func() (*bgtypes.OrderbookSnapshot, error),
	log dex.Logger,
) *bitgetOrderBook {
	return &bitgetOrderBook{
		symbol:                symbol,
		bids:                  skiplist.New(bitgetBidsComparable{}),
		asks:                  skiplist.New(bitgetAsksComparable{}),
		updateQueue:           make(chan *bgtypes.BookUpdate, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
		getSnapshot:           getSnapshot,
		connectedChan:         make(chan bool, 1),
	}
}

// parseAndConvert builds a bitgetObEntry from a pre-computed rate and a
// quantity string. The caller is responsible for parsing the price and
// computing the rate, so that each price string is only parsed once.
func (b *bitgetOrderBook) parseAndConvert(priceStr, qtyStr string, rate uint64) *bitgetObEntry {
	qty, err := strconv.ParseFloat(qtyStr, 64)
	if err != nil {
		b.log.Warnf("%s: failed to parse qty %q: %v", b.symbol, qtyStr, err)
		return nil
	}

	return &bitgetObEntry{
		priceStr: priceStr,
		qtyStr:   qtyStr,
		rate:     rate,
		qty:      uint64(qty * float64(b.baseConversionFactor)),
	}
}

// updateSide applies changes to one side of the orderbook.
func (b *bitgetOrderBook) updateSide(list *skiplist.SkipList, updates [][]string) {
	for _, u := range updates {
		if len(u) < 2 {
			continue
		}
		priceStr, qtyStr := u[0], u[1]

		// Parse price once — used for both lookup and entry construction.
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil {
			continue
		}
		rate := calc.MessageRateAlt(price, b.baseConversionFactor, b.quoteConversionFactor)
		if rate == 0 {
			b.log.Warnf("%s: zero rate from price %s", b.symbol, priceStr)
			continue
		}
		lookupKey := &bitgetObEntry{rate: rate}

		// Always remove existing entry at this rate first.
		list.Remove(lookupKey)

		if qtyStr != "0" {
			if entry := b.parseAndConvert(priceStr, qtyStr, rate); entry != nil {
				list.Set(entry, entry)
			}
		}
	}
}

// update applies changes to the orderbook. Entries are parsed once and stored
// with both string and uint64 representations.
func (b *bitgetOrderBook) update(bids, asks [][]string) {
	b.updateSide(b.bids, bids)
	b.updateSide(b.asks, asks)
}

// clear clears the orderbook
func (b *bitgetOrderBook) clear() {
	b.bids = skiplist.New(bitgetBidsComparable{})
	b.asks = skiplist.New(bitgetAsksComparable{})
}

// snapStrings returns top N levels as strings for checksum calculation.
func (b *bitgetOrderBook) snapStrings(levels int) (bids, asks [][]string) {
	bids = make([][]string, 0, levels)
	count := 0
	for curr := b.bids.Front(); curr != nil && count < levels; curr = curr.Next() {
		entry := curr.Value.(*bitgetObEntry)
		bids = append(bids, []string{entry.priceStr, entry.qtyStr})
		count++
	}

	asks = make([][]string, 0, levels)
	count = 0
	for curr := b.asks.Front(); curr != nil && count < levels; curr = curr.Next() {
		entry := curr.Value.(*bitgetObEntry)
		asks = append(asks, []string{entry.priceStr, entry.qtyStr})
		count++
	}

	return bids, asks
}

// Connect implements the dex.Connector interface.
// Synchronizes orderbook: fetch REST snapshot, then accept fresh WebSocket updates.
func (b *bitgetOrderBook) Connect(ctx context.Context) (*sync.WaitGroup, error) {
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

	acceptUpdate := func(update *bgtypes.BookUpdate) bool {
		if !b.synced.Load() {
			// Book is still syncing. Discard this update.
			return true
		}

		if update.IsSnapshot {
			// Full snapshot: clear and replace entire orderbook
			b.clear()
		}

		// Update orderbook - strings are parsed once and stored with uint64 values
		b.update(update.Bids, update.Asks)

		// Validate checksum (if provided and non-zero)
		// Rate limit to avoid performance issues (100-1000+ updates/sec for BTCUSDT)
		if update.Checksum != 0 {
			now := time.Now().UnixMilli()
			lastValidation := b.lastChecksumValidation.Load()
			shouldValidate := update.IsSnapshot || (now-lastValidation > 5000) // 5 seconds

			if shouldValidate {
				// Get top 25 levels using stored strings
				stringBids, stringAsks := b.snapStrings(25)
				calculated := calculateBookChecksum(stringBids, stringAsks)
				if calculated != update.Checksum {
					b.log.Errorf("%s: Checksum mismatch! Expected %d, calculated %d",
						b.symbol, update.Checksum, calculated)
					// Don't trigger resync - just log for monitoring
					return true
				}
				b.log.Tracef("%s: Checksum validated: %d", b.symbol, update.Checksum)
				b.lastChecksumValidation.Store(now)
			}
		}

		return true
	}

	syncOrderbook := func() bool {
		snapshot, err := b.getSnapshot()
		if err != nil {
			b.log.Errorf("%s: error getting orderbook snapshot: %v", b.symbol, err)
			return false
		}

		b.log.Debugf("Got %s orderbook snapshot", b.symbol)

		syncMtx.Lock()
		b.clear()
		b.update(snapshot.Bids, snapshot.Asks)
		b.synced.Store(true)
		syncMtx.Unlock()

		return true
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		processUpdate := func(update *bgtypes.BookUpdate) bool {
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
					b.log.Debugf("Unsyncing %s orderbook due to disconnect.", b.symbol)
					desync(false)
					retry = nil
					continue
				}
			case <-ctx.Done():
				return
			}

			if syncOrderbook() {
				b.log.Infof("Synced %s orderbook", b.symbol)
				retry = nil
			} else {
				b.log.Infof("Failed to sync %s orderbook. Trying again in %s", b.symbol, retryFrequency)
				desync(false)
				retry = time.After(retryFrequency)
			}
		}
	}()

	// Goroutine 3: Monitor orderbook staleness
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !b.synced.Load() {
					continue // Skip if not synced
				}

				lastUpdate := b.lastUpdateTs.Load()
				if lastUpdate == 0 {
					continue // No updates yet
				}

				age := time.Now().UnixMilli() - lastUpdate
				if age > 30000 { // 30 seconds
					b.log.Warnf("%s: Orderbook stale! No updates for %dms", b.symbol, age)
					// Consider triggering resync if very stale
					if age > 60000 { // 60 seconds - definitely stale
						b.log.Errorf("%s: Orderbook extremely stale (%dms). Triggering resync.", b.symbol, age)
						desync(true)
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

// vwap returns the volume weighted average price for a certain quantity of the
// base asset. It returns an error if the orderbook is not synced.
func (b *bitgetOrderBook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if qty == 0 {
		return 0, 0, false, nil
	}

	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, false, ErrUnsyncedOrderbook
	}

	var list *skiplist.SkipList
	if bids {
		list = b.bids
	} else {
		list = b.asks
	}

	weightedTotal := big.NewInt(0)
	bigQty := big.NewInt(0)
	bigRate := big.NewInt(0)
	addToWeightedTotal := func(rate uint64, qty uint64) {
		bigRate.SetUint64(rate)
		bigQty.SetUint64(qty)
		weightedTotal.Add(weightedTotal, bigRate.Mul(bigRate, bigQty))
	}

	remaining := qty
	for curr := list.Front(); curr != nil; curr = curr.Next() {
		entry := curr.Value.(*bitgetObEntry)
		if entry.qty >= remaining {
			filled = true
			extrema = entry.rate
			addToWeightedTotal(entry.rate, remaining)
			break
		}
		remaining -= entry.qty
		addToWeightedTotal(entry.rate, entry.qty)
	}
	if !filled {
		return 0, 0, false, nil
	}

	return weightedTotal.Div(weightedTotal, big.NewInt(int64(qty))).Uint64(), extrema, filled, nil
}

func (b *bitgetOrderBook) invVWAP(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if qty == 0 {
		return 0, 0, false, nil
	}

	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, false, ErrUnsyncedOrderbook
	}

	var list *skiplist.SkipList
	if bids {
		list = b.bids
	} else {
		list = b.asks
	}

	var totalBaseQty uint64
	remaining := qty
	for curr := list.Front(); curr != nil; curr = curr.Next() {
		entry := curr.Value.(*bitgetObEntry)
		quoteQty := calc.BaseToQuote(entry.rate, entry.qty)

		if quoteQty >= remaining {
			filled = true
			extrema = entry.rate
			totalBaseQty += calc.QuoteToBase(entry.rate, remaining)
			break
		}

		remaining -= quoteQty
		totalBaseQty += entry.qty
	}
	if !filled {
		return 0, 0, false, nil
	}

	return calc.BaseQuoteToRate(totalBaseQty, qty), extrema, filled, nil
}

func (b *bitgetOrderBook) midGap() uint64 {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	bestBidI := b.bids.Front()
	if bestBidI == nil {
		return 0
	}
	bestAskI := b.asks.Front()
	if bestAskI == nil {
		return 0
	}
	bestBid := bestBidI.Value.(*bitgetObEntry)
	bestAsk := bestAskI.Value.(*bitgetObEntry)
	return (bestBid.rate + bestAsk.rate) / 2
}

// bitget is the main Bitget exchange adapter
type bitget struct {
	log        dex.Logger
	apiURL     string
	wsPublic   string
	wsPrivate  string
	apiKey     string
	secretKey  string
	passphrase string // Bitget requires a passphrase for API access
	net        dex.Network
	broadcast  func(any)
	ctx        context.Context

	tradeIDNonce       atomic.Uint32
	tradeIDNoncePrefix dex.Bytes

	// Markets and symbols
	markets     atomic.Value // map[string]*bgtypes.Market
	tokenIDs    atomic.Value // map[string][]uint32
	minWithdraw atomic.Value // map[uint32]*bitgetWithdrawInfo

	marketSnapshotMtx sync.Mutex
	marketSnapshot    struct {
		stamp time.Time
		m     map[string]*Market
	}

	// Balances
	balanceMtx sync.RWMutex
	balances   map[uint32]*ExchangeBalance

	// WebSocket connections
	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	userStreamMtx sync.RWMutex
	userStream    comms.WsConn

	// Order books
	booksMtx sync.RWMutex
	books    map[string]*bitgetOrderBook

	// Trade tracking
	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*bitgetTradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	knownAssets map[uint32]bool
}

type bitgetTradeInfo struct {
	updaterID int
	baseID    uint32
	quoteID   uint32
	sell      bool
	rate      uint64
	qty       uint64
	market    bool
}

// bitgetWithdrawInfo stores transfer constraints for deposits and withdrawals.
// The minimum represents max(deposit_min, withdrawal_min) to satisfy both CEX requirements.
type bitgetWithdrawInfo struct {
	minimum uint64 // Minimum transfer amount (satisfies both deposit and withdrawal minimums)
	lotSize uint64 // Step size (integer multiple) for withdrawals
}

var _ CEX = (*bitget)(nil)

// Symbol conversion maps
var dexToBitgetCoinSymbol = map[string]string{
	"polygon": "POL",
	"weth":    "ETH",
}

var bitgetToDexCoinSymbol = map[string]string{
	"ETH": "eth",
}

var dexToBitgetNetworkSymbol = map[string]string{
	"polygon": "Polygon",
	"eth":     "ERC20",
	"base":    "BASE",
}

// dexCoinToWrappedSymbol is already defined in the libxc package

var bitgetToDexSymbol = make(map[string]string)

func init() {
	// Build the bitgetToDexSymbol map
	for key, value := range bitgetToDexCoinSymbol {
		bitgetToDexSymbol[key] = value
	}

	for key, value := range dexToBitgetCoinSymbol {
		if _, exists := bitgetToDexSymbol[value]; !exists {
			bitgetToDexSymbol[value] = key
		}
	}
}

// convertBitgetCoin converts a Bitget coin symbol to a DEX symbol.
func convertBitgetCoin(coin string) string {
	if convertedSymbol, found := bitgetToDexSymbol[strings.ToUpper(coin)]; found {
		return convertedSymbol
	}
	return strings.ToLower(coin)
}

// convertBitgetNetwork converts a Bitget network symbol to a DEX symbol.
func convertBitgetNetwork(network string) string {
	for key, value := range dexToBitgetNetworkSymbol {
		if strings.EqualFold(value, network) {
			return key
		}
	}
	return convertBitgetCoin(network)
}

// bitgetCoinNetworkToDexSymbol takes the coin name and its network name as
// returned by the Bitget API and returns the DEX symbol.
func bitgetCoinNetworkToDexSymbol(coin, network string) string {
	symbol, netSymbol := convertBitgetCoin(coin), convertBitgetNetwork(network)
	if symbol == netSymbol {
		return symbol
	}
	// Convert coin to wrapped version if it has a wrapped equivalent
	if wrappedSymbol, found := dexCoinToWrappedSymbol[symbol]; found {
		symbol = wrappedSymbol
	}
	return symbol + "." + netSymbol
}

func mapDexSymbolToBitgetCoin(symbol string) string {
	if bitgetSymbol, found := dexToBitgetCoinSymbol[strings.ToLower(symbol)]; found {
		return bitgetSymbol
	}
	return strings.ToUpper(symbol)
}

func mapDexSymbolToBitgetNetwork(symbol string) string {
	if bitgetSymbol, found := dexToBitgetNetworkSymbol[strings.ToLower(symbol)]; found {
		return bitgetSymbol
	}
	return strings.ToUpper(symbol)
}

type bitgetAssetConfig struct {
	assetID uint32
	// symbol is the DEX asset symbol, always lower case
	symbol string
	// coin is the asset symbol on Bitget, always upper case.
	coin string
	// chain will be the same as coin for the base assets of
	// a blockchain, but for tokens it will be the chain
	// that the token is hosted such as "ETH".
	chain            string
	conversionFactor uint64
	ui               *dex.UnitInfo
}

func bitgetAssetCfg(assetID uint32) (*bitgetAssetConfig, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, err
	}

	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return nil, fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	parts := strings.Split(symbol, ".")
	coin := mapDexSymbolToBitgetCoin(parts[0])

	var chain string
	if len(parts) > 1 {
		chain = mapDexSymbolToBitgetNetwork(parts[1])
	} else {
		chain = mapDexSymbolToBitgetNetwork(parts[0])
	}

	return &bitgetAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             coin,
		chain:            chain,
		conversionFactor: ui.Conventional.ConversionFactor,
		ui:               &ui,
	}, nil
}

func bitgetAssetCfgs(baseID, quoteID uint32) (*bitgetAssetConfig, *bitgetAssetConfig, error) {
	baseCfg, err := bitgetAssetCfg(baseID)
	if err != nil {
		return nil, nil, err
	}

	quoteCfg, err := bitgetAssetCfg(quoteID)
	if err != nil {
		return nil, nil, err
	}

	return baseCfg, quoteCfg, nil
}

// BitgetCodedErr represents an error response from Bitget API
type BitgetCodedErr struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (e *BitgetCodedErr) Error() string {
	return fmt.Sprintf("code = %s, msg = %q", e.Code, e.Msg)
}

func newBitget(cfg *CEXConfig) *bitget {
	// For testnet/simnet, we'd use different URLs
	apiURL := bitgetAPIURL
	wsPublic := bitgetPublicWSURL
	wsPrivate := bitgetPrivateWSURL

	registeredAssets := asset.Assets()
	knownAssets := make(map[uint32]bool, len(registeredAssets))
	for _, a := range registeredAssets {
		knownAssets[a.ID] = true
	}

	bg := &bitget{
		log:                cfg.Logger,
		broadcast:          cfg.Notify,
		apiURL:             apiURL,
		wsPublic:           wsPublic,
		wsPrivate:          wsPrivate,
		apiKey:             cfg.APIKey,
		secretKey:          cfg.SecretKey,
		passphrase:         cfg.APIPassphrase,
		net:                cfg.Net,
		balances:           make(map[uint32]*ExchangeBalance),
		books:              make(map[string]*bitgetOrderBook),
		tradeInfo:          make(map[string]*bitgetTradeInfo),
		tradeUpdaters:      make(map[int]chan *Trade),
		tradeIDNoncePrefix: encode.RandomBytes(10),
		knownAssets:        knownAssets,
	}

	bg.markets.Store(make(map[string]*bgtypes.Market))

	return bg
}

// generateTradeID generates a unique trade ID
func (bg *bitget) generateTradeID() string {
	nonce := bg.tradeIDNonce.Add(1)
	nonceB := encode.Uint32Bytes(nonce)
	return hex.EncodeToString(append(bg.tradeIDNoncePrefix, nonceB...))
}

// sign creates a signature for Bitget API requests
func (bg *bitget) sign(timestamp, method, requestPath, body string) string {
	message := timestamp + method + requestPath + body
	mac := hmac.New(sha256.New, []byte(bg.secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// request makes an HTTP request to the Bitget API
func (bg *bitget) request(ctx context.Context, method, endpoint string, params map[string]string, body any, needsAuth bool, result any) error {
	fullURL := bg.apiURL + endpoint

	var bodyBytes []byte
	var err error
	bodyStr := ""

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("error marshaling request body: %w", err)
		}
		bodyStr = string(bodyBytes)
	}

	// Add query parameters
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Add(k, v)
		}
		fullURL += "?" + values.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if needsAuth {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		requestPath := endpoint
		if len(params) > 0 {
			values := url.Values{}
			for k, v := range params {
				values.Add(k, v)
			}
			requestPath += "?" + values.Encode()
		}

		sign := bg.sign(timestamp, method, requestPath, bodyStr)

		req.Header.Set("ACCESS-KEY", bg.apiKey)
		req.Header.Set("ACCESS-SIGN", sign)
		req.Header.Set("ACCESS-TIMESTAMP", timestamp)
		req.Header.Set("ACCESS-PASSPHRASE", bg.passphrase)
	}

	var bgErr BitgetCodedErr
	if err := dexnet.Do(req, result, dexnet.WithSizeLimit(1<<24), dexnet.WithErrorParsing(&bgErr)); err != nil {
		bg.log.Errorf("request error from endpoint %s %q: %v, bitget error: %v",
			method, endpoint, err, bgErr.Msg)
		return errors.Join(err, &bgErr)
	}

	return nil
}

// getAPI makes a GET request to the Bitget API
func (bg *bitget) getAPI(ctx context.Context, endpoint string, params map[string]string, needsAuth bool, result any) error {
	return bg.request(ctx, http.MethodGet, endpoint, params, nil, needsAuth, result)
}

// postAPI makes a POST request to the Bitget API
func (bg *bitget) postAPI(ctx context.Context, endpoint string, body any, needsAuth bool, result any) error {
	return bg.request(ctx, http.MethodPost, endpoint, nil, body, needsAuth, result)
}

// getCoinInfo retrieves Bitget coin configurations and updates tokenIDs and minWithdraw
func (bg *bitget) getCoinInfo(ctx context.Context) error {
	var resp bgtypes.APIResponse
	err := bg.getAPI(ctx, "/api/v2/spot/public/coins", nil, false, &resp)
	if err != nil {
		return err
	}

	if resp.Code != "00000" {
		return fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var coins []*bgtypes.CoinInfo
	if err := json.Unmarshal(resp.Data, &coins); err != nil {
		return fmt.Errorf("error unmarshaling coin info: %w", err)
	}

	bg.readCoins(coins)
	return nil
}

// readCoins processes coin info and stores token IDs and withdrawal info
func (bg *bitget) readCoins(coins []*bgtypes.CoinInfo) {
	tokenIDs := make(map[string][]uint32)
	minWithdraw := make(map[uint32]*bitgetWithdrawInfo)

	for _, coin := range coins {
		for _, chain := range coin.Chains {
			if !strings.EqualFold(chain.Withdrawable, "true") || !strings.EqualFold(chain.Rechargeable, "true") {
				continue
			}

			symbol := bitgetCoinNetworkToDexSymbol(coin.Coin, chain.Chain)
			assetID, found := dex.BipSymbolID(symbol)
			if !found {
				continue
			}

			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// Asset ID not found in DEX - skip this coin
				continue
			}

			if tkn := asset.TokenInfo(assetID); tkn != nil {
				tokenIDs[coin.Coin] = append(tokenIDs[coin.Coin], assetID)
			}

			// Parse both deposit and withdrawal minimums
			minDepositAmt, err := strconv.ParseFloat(chain.MinDepositAmount, 64)
			if err != nil {
				bg.log.Warnf("Failed to parse min deposit amount for %s: %v", coin.Coin, err)
				minDepositAmt = 0
			}
			minWithdrawAmt, err := strconv.ParseFloat(chain.MinWithdrawAmount, 64)
			if err != nil {
				bg.log.Warnf("Failed to parse min withdraw amount for %s: %v", coin.Coin, err)
				minWithdrawAmt = 0
			}

			// Use the MAXIMUM of deposit and withdrawal minimums
			// This ensures we never transfer less than either minimum:
			// - Deposits below minDepositAmount won't be credited by Bitget
			// - Withdrawals below minWithdrawAmount will be rejected by Bitget
			minTransferAmt := math.Max(minDepositAmt, minWithdrawAmt)
			minimum := uint64(math.Round(float64(ui.Conventional.ConversionFactor) * minTransferAmt))

			// Parse lot size (precision/step) if available, default to 1
			var lotSize uint64 = 1
			if chain.WithdrawIntegerMultiple != "" {
				withdrawStep, err := strconv.ParseFloat(chain.WithdrawIntegerMultiple, 64)
				if err != nil {
					bg.log.Warnf("Failed to parse withdraw step for %s: %v", coin.Coin, err)
				} else if withdrawStep > 0 {
					lotSize = uint64(math.Round(withdrawStep * float64(ui.Conventional.ConversionFactor)))
				}
			}

			if minimum == 0 {
				bg.log.Tracef("Invalid transfer minimum for %s network %s (deposit min=%s, withdraw min=%s)",
					coin.Coin, chain.Chain, chain.MinDepositAmount, chain.MinWithdrawAmount)
				continue
			}

			// Ensure lot size is at least 1
			if lotSize == 0 {
				lotSize = 1
			}

			minWithdraw[assetID] = &bitgetWithdrawInfo{
				minimum: minimum, // max(deposit_min, withdrawal_min) to satisfy both
				lotSize: lotSize,
			}
		}
	}

	bg.log.Debugf("Stored deposit/withdrawal info for %d assets", len(minWithdraw))
	bg.tokenIDs.Store(tokenIDs)
	bg.minWithdraw.Store(minWithdraw)
}

// getMarkets retrieves and processes market/symbol information
func (bg *bitget) getMarkets(ctx context.Context) (map[string]*bgtypes.Market, error) {
	var resp bgtypes.APIResponse
	err := bg.getAPI(ctx, "/api/v2/spot/public/symbols", nil, false, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var symbols []*bgtypes.SymbolInfo
	if err := json.Unmarshal(resp.Data, &symbols); err != nil {
		return nil, fmt.Errorf("error unmarshaling symbols: %w", err)
	}

	marketsMap := make(map[string]*bgtypes.Market, len(symbols))
	tokenIDs := bg.tokenIDs.Load().(map[string][]uint32)

	skippedNotLive := 0
	skippedNoMatch := 0

	for _, sym := range symbols {
		// Bitget uses "online" for active markets, not "live"
		if sym.Status != "online" {
			skippedNotLive++
			continue
		}

		// Check if we support both assets
		dexMarkets := bitgetMarketToDexMarkets(sym.BaseCoin, sym.QuoteCoin, tokenIDs)
		if len(dexMarkets) == 0 {
			skippedNoMatch++
			continue
		}

		dexMkt := dexMarkets[0]
		bui, err := asset.UnitInfo(dexMkt.BaseID)
		if err != nil {
			bg.log.Warnf("Failed to get unit info for base asset %d: %v", dexMkt.BaseID, err)
			continue
		}
		qui, err := asset.UnitInfo(dexMkt.QuoteID)
		if err != nil {
			bg.log.Warnf("Failed to get unit info for quote asset %d: %v", dexMkt.QuoteID, err)
			continue
		}

		minTradeAmount, err := strconv.ParseFloat(sym.MinTradeAmount, 64)
		if err != nil {
			bg.log.Warnf("Failed to parse min trade amount for %s: %v", sym.Symbol, err)
			minTradeAmount = 0
		}
		maxTradeAmount, err := strconv.ParseFloat(sym.MaxTradeAmount, 64)
		if err != nil {
			bg.log.Warnf("Failed to parse max trade amount for %s: %v", sym.Symbol, err)
			maxTradeAmount = 0
		}
		minTradeUSDT, err := strconv.ParseFloat(sym.MinTradeUSDT, 64)
		if err != nil {
			bg.log.Warnf("Failed to parse min trade USDT for %s: %v", sym.Symbol, err)
			minTradeUSDT = 0
		}
		pricePrecision, err := strconv.Atoi(sym.PricePrecision)
		if err != nil {
			bg.log.Warnf("Failed to parse price precision for %s: %v", sym.Symbol, err)
			pricePrecision = 8 // Default to 8 decimal places
		}
		quantityPrecision, err := strconv.Atoi(sym.QuantityPrecision)
		if err != nil {
			bg.log.Warnf("Failed to parse quantity precision for %s: %v", sym.Symbol, err)
			quantityPrecision = 8 // Default to 8 decimal places
		}

		// Calculate step sizes
		priceStep := calc.MessageRateAlt(math.Pow10(-pricePrecision), bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
		lotSize := uint64(math.Pow10(-quantityPrecision) * float64(bui.Conventional.ConversionFactor))

		marketsMap[sym.Symbol] = &bgtypes.Market{
			Symbol:            sym.Symbol,
			BaseCoin:          sym.BaseCoin,
			QuoteCoin:         sym.QuoteCoin,
			MinTradeAmount:    minTradeAmount,
			MaxTradeAmount:    maxTradeAmount,
			PricePrecision:    pricePrecision,
			QuantityPrecision: quantityPrecision,
			MinTradeUSDT:      minTradeUSDT,
			PriceStep:         priceStep,
			LotSize:           lotSize,
			MinQty:            uint64(minTradeAmount * float64(bui.Conventional.ConversionFactor)),
			MaxQty:            uint64(maxTradeAmount * float64(bui.Conventional.ConversionFactor)),
			MinNotional:       uint64(minTradeUSDT * float64(qui.Conventional.ConversionFactor)),
		}
	}

	bg.log.Debugf("Loaded %d API-tradeable symbols from Bitget (skipped: %d not active, %d no DEX match)",
		len(marketsMap), skippedNotLive, skippedNoMatch)

	bg.markets.Store(marketsMap)
	return marketsMap, nil
}

// bitgetMarketToDexMarkets converts Bitget market identifiers to DEX market matches
func bitgetMarketToDexMarkets(baseSymbol, quoteSymbol string, tokenIDs map[string][]uint32) []*MarketMatch {
	baseAssetIDs := bitgetGetDEXAssetIDs(baseSymbol, tokenIDs)
	if len(baseAssetIDs) == 0 {
		return nil
	}

	quoteAssetIDs := bitgetGetDEXAssetIDs(quoteSymbol, tokenIDs)
	if len(quoteAssetIDs) == 0 {
		return nil
	}

	markets := make([]*MarketMatch, 0, len(baseAssetIDs)*len(quoteAssetIDs))
	for _, baseID := range baseAssetIDs {
		for _, quoteID := range quoteAssetIDs {
			markets = append(markets, &MarketMatch{
				Slug:     baseSymbol + quoteSymbol,
				MarketID: dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID),
				BaseID:   baseID,
				QuoteID:  quoteID,
			})
		}
	}

	return markets
}

// bitgetGetDEXAssetIDs returns all DEX asset IDs for a Bitget coin symbol.
// For tokens like USDT/USDC, it returns ALL network variants that Bitget supports,
// allowing the user to choose which network they want to trade on.
func bitgetGetDEXAssetIDs(coin string, tokenIDs map[string][]uint32) []uint32 {
	dexSymbol := convertBitgetCoin(coin)

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

	// For tokens, return ALL variants that Bitget actually supports
	// (as discovered from the API and stored in tokenIDs map)
	if tokens, found := tokenIDs[coin]; found {
		for _, tokenID := range tokens {
			if isRegistered(tokenID) {
				assetIDs = append(assetIDs, tokenID)
			}
		}
	}

	return assetIDs
}

// bitgetMktID creates a market identifier from asset configs
func bitgetMktID(baseCfg, quoteCfg *bitgetAssetConfig) string {
	return baseCfg.coin + quoteCfg.coin
}

// Connect implements the dex.Connector interface
func (bg *bitget) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	bg.ctx = ctx
	wg := new(sync.WaitGroup)

	// Fetch coin info to populate token mappings and withdrawal minimums
	if err := bg.getCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("error getting coin info: %w", err)
	}

	// Fetch markets and log statistics
	if _, err := bg.MatchedMarkets(ctx); err != nil {
		return nil, fmt.Errorf("error getting markets: %w", err)
	}

	// Fetch balances in background (non-blocking)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bg.refreshBalances(ctx); err != nil {
			bg.log.Errorf("initial balance fetch failed: %v", err)
		}
	}()

	// Private WS connect (for user data streams)
	bg.log.Infof("Initializing private WebSocket connection...")
	privateWSCfg := &comms.WsCfg{
		URL:           bg.wsPrivate,
		PingWait:      120 * time.Second,
		Logger:        bg.log.SubLogger("WS-PRIV"),
		RawHandler:    bg.handlePrivateWsMessage,
		ReconnectSync: bg.onPrivateReconnect,
	}
	privateWS, err := comms.NewWsConn(privateWSCfg)
	if err != nil {
		return wg, fmt.Errorf("create private ws: %w", err)
	}

	bg.userStreamMtx.Lock()
	bg.userStream = privateWS
	bg.userStreamMtx.Unlock()

	privateWSWg, err := privateWS.Connect(bg.ctx)
	if err != nil {
		bg.log.Errorf("private ws initial connect error: %v (will auto-reconnect)", err)
	} else {
		bg.log.Infof("Private WebSocket connected successfully")
	}

	if privateWSWg != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			privateWSWg.Wait()
		}()
	}

	// Authenticate and subscribe to private channels
	if err := bg.loginPrivateWs(); err != nil {
		bg.log.Errorf("Failed to login to private WebSocket: %v", err)
	} else {
		if err := bg.subscribePrivateChannels(); err != nil {
			bg.log.Errorf("Failed to subscribe to private channels: %v", err)
		}
	}

	bg.log.Infof("Bitget connected successfully")

	// Keep-alive ping for private WebSocket
	// Bitget requires clients to send string "ping" every 30 seconds
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Send raw string "ping" as per Bitget API docs
				bg.userStreamMtx.RLock()
				if bg.userStream != nil && !bg.userStream.IsDown() {
					if err := bg.userStream.SendRaw([]byte("ping")); err != nil {
						bg.log.Errorf("Failed to send private WebSocket ping: %v", err)
					}
				}
				bg.userStreamMtx.RUnlock()
			case <-bg.ctx.Done():
				return
			}
		}
	}()

	// Refresh balances periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				err := bg.refreshBalances(ctx)
				if err != nil {
					bg.log.Errorf("Error fetching balances: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Refresh markets periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				_, err := bg.getMarkets(ctx)
				if err != nil {
					bg.log.Errorf("Error fetching markets: %v", err)
					nextTick = time.After(time.Minute)
				} else {
					nextTick = time.After(time.Hour)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Refresh coin info periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				err := bg.getCoinInfo(ctx)
				if err != nil {
					bg.log.Errorf("Error fetching coin info: %v", err)
					nextTick = time.After(time.Minute)
				} else {
					nextTick = time.After(time.Hour)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return wg, nil
}

// loginPrivateWs authenticates to the private WebSocket
func (bg *bitget) loginPrivateWs() error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Sign: base64(hmac_sha256(timestamp + 'GET' + '/user/verify', secretKey))
	message := timestamp + "GET" + "/user/verify"
	mac := hmac.New(sha256.New, []byte(bg.secretKey))
	mac.Write([]byte(message))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	loginReq := &bgtypes.WsLoginRequest{
		Op: wsOpLogin,
		Args: []bgtypes.WsLoginArg{
			{
				ApiKey:     bg.apiKey,
				Passphrase: bg.passphrase,
				Timestamp:  timestamp,
				Sign:       sign,
			},
		},
	}

	bg.userStreamMtx.RLock()
	defer bg.userStreamMtx.RUnlock()

	if bg.userStream == nil {
		return fmt.Errorf("user stream not connected")
	}

	loginBytes, err := json.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login request: %w", err)
	}

	return bg.userStream.SendRaw(loginBytes)
}

// subscribePrivateChannels subscribes to private WebSocket channels
func (bg *bitget) subscribePrivateChannels() error {
	// Subscribe to orders and account updates
	subscribeReq := &bgtypes.WsRequest{
		Op: wsOpSubscribe,
		Args: []bgtypes.WsArg{
			{
				InstType: instTypeSpot,
				Channel:  wsChannelOrders,
				InstId:   "default", // Subscribe to all trading pairs
			},
			{
				InstType: instTypeSpot,
				Channel:  wsChannelAccount,
				Coin:     "default", // Subscribe to all coins
			},
		},
	}

	bg.userStreamMtx.RLock()
	defer bg.userStreamMtx.RUnlock()

	if bg.userStream == nil {
		return fmt.Errorf("user stream not connected")
	}

	subBytes, err := json.Marshal(subscribeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal subscribe request: %w", err)
	}

	if err := bg.userStream.SendRaw(subBytes); err != nil {
		return fmt.Errorf("failed to send subscribe request: %w", err)
	}

	// Count channels for logging
	channels := []string{wsChannelOrders, wsChannelAccount}
	bg.log.Infof("Subscribed to %d private channels: %v", len(channels), channels)

	return nil
}

// handlePrivateWsMessage handles incoming private WebSocket messages
func (bg *bitget) handlePrivateWsMessage(b []byte) {
	// Check for raw string "ping" from server
	if string(b) == "ping" {
		// Respond with raw string "pong"
		bg.userStreamMtx.RLock()
		if bg.userStream != nil {
			bg.userStream.SendRaw([]byte("pong"))
		}
		bg.userStreamMtx.RUnlock()
		return
	}

	// Check for JSON ping (legacy, might not be used by Bitget anymore)
	var pingMsg bgtypes.WsPingMessage
	if err := json.Unmarshal(b, &pingMsg); err == nil && pingMsg.Op == wsOpPing {
		// Respond with pong
		pongMsg := &bgtypes.WsPongMessage{Op: wsOpPong}
		pongBytes, err := json.Marshal(pongMsg)
		if err == nil {
			bg.userStreamMtx.RLock()
			if bg.userStream != nil {
				bg.userStream.SendRaw(pongBytes)
			}
			bg.userStreamMtx.RUnlock()
		}
		return
	}

	// Check for response (login, subscribe, etc.)
	var wsResp bgtypes.WsResponse
	if err := json.Unmarshal(b, &wsResp); err == nil {
		if wsResp.Event != "" {
			if wsResp.Code != "0" && wsResp.Code != "" {
				bg.log.Errorf("WebSocket error: event=%s, code=%s, msg=%s", wsResp.Event, wsResp.Code, wsResp.Msg)
			} else {
				bg.log.Tracef("WebSocket event: %s", wsResp.Event)
			}
			return
		}
	}

	// Check for data message
	var dataMsg bgtypes.WsDataMessage
	if err := json.Unmarshal(b, &dataMsg); err == nil && dataMsg.Arg != nil {
		switch dataMsg.Arg.Channel {
		case wsChannelAccount:
			bg.handleAccountUpdate(&dataMsg)
		case wsChannelOrders:
			bg.handleOrderUpdate(&dataMsg)
		default:
			bg.log.Tracef("Unhandled WebSocket channel: %s", dataMsg.Arg.Channel)
		}
		return
	}

	bg.log.Tracef("Unhandled WebSocket message: %s", string(b))
}

// handleAccountUpdate processes account balance updates from WebSocket
func (bg *bitget) handleAccountUpdate(msg *bgtypes.WsDataMessage) {
	for _, dataItem := range msg.Data {
		dataBytes, err := json.Marshal(dataItem)
		if err != nil {
			bg.log.Errorf("Error marshaling account data: %v", err)
			continue
		}

		var accountData bgtypes.WsAccountData
		if err := json.Unmarshal(dataBytes, &accountData); err != nil {
			bg.log.Errorf("Error unmarshaling account data: %v", err)
			continue
		}

		// Update balance cache
		// Empty strings should not occur for balances, but handle gracefully
		available, err := strconv.ParseFloat(accountData.Available, 64)
		if err != nil {
			if accountData.Available != "" {
				bg.log.Warnf("Failed to parse available balance for %s: %v", accountData.Coin, err)
			}
			available = 0
		}
		frozen, err := strconv.ParseFloat(accountData.Frozen, 64)
		if err != nil {
			if accountData.Frozen != "" {
				bg.log.Warnf("Failed to parse frozen balance for %s: %v", accountData.Coin, err)
			}
			frozen = 0
		}

		// Note: Frozen = funds locked in open orders
		// Locked = funds locked for other purposes (fiat merchant, etc.)
		// For trading, we care about Frozen

		// Map coin to asset IDs
		tokenIDs := bg.tokenIDs.Load().(map[string][]uint32)
		assetIDs := bitgetGetDEXAssetIDs(accountData.Coin, tokenIDs)

		if len(assetIDs) == 0 {
			continue
		}

		for _, assetID := range assetIDs {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// Asset ID not found in DEX - skip this asset
				bg.log.Tracef("Asset ID %d not found in DEX unit info: %v", assetID, err)
				continue
			}

			bg.balanceMtx.Lock()
			bg.balances[assetID] = &ExchangeBalance{
				Available: uint64(available * float64(ui.Conventional.ConversionFactor)),
				Locked:    uint64(frozen * float64(ui.Conventional.ConversionFactor)),
			}
			bg.balanceMtx.Unlock()
		}
	}
}

// handleOrderUpdate processes order updates from WebSocket
func (bg *bitget) handleOrderUpdate(msg *bgtypes.WsDataMessage) {
	bg.log.Debugf("Order update action=%s, %d items", msg.Action, len(msg.Data))

	for _, dataItem := range msg.Data {
		dataBytes, err := json.Marshal(dataItem)
		if err != nil {
			bg.log.Errorf("Error marshaling order data: %v", err)
			continue
		}

		var orderData bgtypes.WsOrderData
		if err := json.Unmarshal(dataBytes, &orderData); err != nil {
			bg.log.Errorf("Error unmarshaling order data: %v", err)
			continue
		}

		// Log order data for debugging
		if bg.log.Level() <= dex.LevelTrace {
			dataJson, _ := json.MarshalIndent(orderData, "", "  ")
			bg.log.Tracef("Order data: %s", string(dataJson))
		}

		// Process order update
		bg.processOrderUpdate(&orderData)
	}
}

// processOrderUpdate processes a single order update
func (bg *bitget) processOrderUpdate(order *bgtypes.WsOrderData) {
	bg.tradeUpdaterMtx.RLock()
	defer bg.tradeUpdaterMtx.RUnlock()

	tradeInfo, found := bg.tradeInfo[order.ClientOid]
	if !found {
		return
	}

	baseUI, err := asset.UnitInfo(tradeInfo.baseID)
	if err != nil {
		return
	}

	quoteUI, err := asset.UnitInfo(tradeInfo.quoteID)
	if err != nil {
		return
	}

	complete := order.Status == bitgetOrderStatusFullFill || order.Status == bitgetOrderStatusCancelled

	// Use AccBaseVolume (cumulative total) instead of BaseVolume (latest fill only)
	accBaseQty, err := strconv.ParseFloat(order.AccBaseVolume, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse accumulated base volume for order %s: %v", order.OrderId, err)
		accBaseQty = 0
	}
	avgPrice, err := strconv.ParseFloat(order.PriceAvg, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse average price for order %s: %v", order.OrderId, err)
		avgPrice = 0
	}

	// Calculate quote volume from cumulative base * average price
	accQuoteQty := accBaseQty * avgPrice

	trade := &Trade{
		ID:          order.OrderId,
		Complete:    complete,
		Rate:        0, // Will be calculated below
		Qty:         uint64(accBaseQty * float64(baseUI.Conventional.ConversionFactor)),
		BaseFilled:  uint64(accBaseQty * float64(baseUI.Conventional.ConversionFactor)),
		QuoteFilled: uint64(accQuoteQty * float64(quoteUI.Conventional.ConversionFactor)),
	}

	// Calculate rate from average price
	if avgPrice > 0 {
		trade.Rate = uint64(avgPrice * float64(quoteUI.Conventional.ConversionFactor) / float64(baseUI.Conventional.ConversionFactor) * calc.RateEncodingFactor)
	}

	// Optional: Log maker/taker status for debugging
	if order.TradeScope != "" {
		makerTaker := "unknown"
		if order.TradeScope == "T" {
			makerTaker = "taker"
		} else if order.TradeScope == "M" {
			makerTaker = "maker"
		}
		bg.log.Tracef("Order %s: %s, filled: %s/%s (avg price: %s)",
			order.OrderId, makerTaker, order.AccBaseVolume, order.NewSize, order.PriceAvg)
	}

	for _, updater := range bg.tradeUpdaters {
		select {
		case updater <- trade:
		default:
		}
	}
}

// onPrivateReconnect is called when the private WebSocket reconnects
func (bg *bitget) onPrivateReconnect() {
	bg.log.Infof("Private WebSocket reconnected, re-authenticating...")

	if err := bg.loginPrivateWs(); err != nil {
		bg.log.Errorf("Failed to re-authenticate after reconnect: %v", err)
		return
	}

	if err := bg.subscribePrivateChannels(); err != nil {
		bg.log.Errorf("Failed to re-subscribe after reconnect: %v", err)
		return
	}

	// Refresh balances after reconnection
	if err := bg.refreshBalances(bg.ctx); err != nil {
		bg.log.Errorf("Failed to refresh balances after reconnect: %v", err)
	}
}

// SubscribeMarket subscribes to orderbook updates for a market
func (bg *bitget) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	baseCfg, quoteCfg, err := bitgetAssetCfgs(baseID, quoteID)
	if err != nil {
		return err
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)

	// Check if book already exists
	bg.booksMtx.RLock()
	book, exists := bg.books[symbol]
	bg.booksMtx.RUnlock()

	if exists {
		// Book already exists, just increment subscriber count
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return nil
	}

	// Create snapshot fetcher function
	getSnapshot := func() (*bgtypes.OrderbookSnapshot, error) {
		return bg.getOrderbookSnapshot(ctx, symbol)
	}

	// Create new order book
	book = newBitgetOrderBook(
		baseCfg.conversionFactor,
		quoteCfg.conversionFactor,
		symbol,
		getSnapshot,
		bg.log,
	)

	// Add to books map
	bg.booksMtx.Lock()
	bg.books[symbol] = book
	bg.booksMtx.Unlock()

	// Start book sync in a connection master
	cm := dex.NewConnectionMaster(book)
	book.mtx.Lock()
	book.cm = cm
	book.mtx.Unlock()

	if err := cm.ConnectOnce(ctx); err != nil {
		bg.log.Errorf("Error connecting %s order book: %v", symbol, err)
		// Don't return error, it will retry
	}

	// Ensure market WebSocket is connected
	if err := bg.ensureMarketConnection(ctx); err != nil {
		return fmt.Errorf("failed to connect market websocket: %w", err)
	}

	// Subscribe to WebSocket orderbook channel
	subscribeReq := &bgtypes.WsRequest{
		Op: wsOpSubscribe,
		Args: []bgtypes.WsArg{
			{
				InstType: instTypeSpot,
				Channel:  wsChannelBooks,
				InstId:   symbol,
			},
		},
	}

	subBytes, err := json.Marshal(subscribeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal subscribe request: %w", err)
	}

	bg.marketStreamMtx.RLock()
	defer bg.marketStreamMtx.RUnlock()

	if bg.marketStream == nil || bg.marketStream.IsDown() {
		return fmt.Errorf("market websocket not connected")
	}

	if err := bg.marketStream.SendRaw(subBytes); err != nil {
		return fmt.Errorf("failed to send subscribe request: %w", err)
	}

	bg.log.Infof("Subscribed to %s orderbook updates", symbol)
	return nil
}

// UnsubscribeMarket unsubscribes from orderbook updates
func (bg *bitget) UnsubscribeMarket(baseID, quoteID uint32) error {
	baseCfg, quoteCfg, err := bitgetAssetCfgs(baseID, quoteID)
	if err != nil {
		return err
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)

	bg.booksMtx.RLock()
	book, exists := bg.books[symbol]
	bg.booksMtx.RUnlock()

	if !exists {
		return nil
	}

	book.mtx.Lock()
	book.numSubscribers--
	numSubs := book.numSubscribers
	book.mtx.Unlock()

	if numSubs > 0 {
		return nil
	}

	// Unsubscribe from WebSocket
	unsubscribeReq := &bgtypes.WsRequest{
		Op: wsOpUnsubscribe,
		Args: []bgtypes.WsArg{
			{
				InstType: instTypeSpot,
				Channel:  wsChannelBooks,
				InstId:   symbol,
			},
		},
	}

	unsubBytes, err := json.Marshal(unsubscribeReq)
	if err != nil {
		bg.log.Errorf("failed to marshal unsubscribe request: %v", err)
	} else {
		bg.marketStreamMtx.RLock()
		if bg.marketStream != nil && !bg.marketStream.IsDown() {
			if err := bg.marketStream.SendRaw(unsubBytes); err != nil {
				bg.log.Errorf("failed to send unsubscribe request: %v", err)
			}
		}
		bg.marketStreamMtx.RUnlock()
	}

	// Disconnect the book
	if book.cm != nil {
		book.cm.Disconnect()
	}

	// Remove from books map
	bg.booksMtx.Lock()
	delete(bg.books, symbol)
	bg.booksMtx.Unlock()

	bg.log.Infof("Unsubscribed from %s orderbook updates", symbol)
	return nil
}

// book returns the bitgetOrderBook for a market
func (bg *bitget) book(baseID, quoteID uint32) (*bitgetOrderBook, error) {
	baseCfg, quoteCfg, err := bitgetAssetCfgs(baseID, quoteID)
	if err != nil {
		return nil, err
	}
	symbol := bitgetMktID(baseCfg, quoteCfg)

	bg.booksMtx.RLock()
	book, found := bg.books[symbol]
	bg.booksMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no book for market %s", symbol)
	}
	return book, nil
}

// VWAP returns volume weighted average price for a certain quantity of the base asset on a market.
// SubscribeMarket must be called, and the market must be synced before results can be expected.
func (bg *bitget) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	book, err := bg.book(baseID, quoteID)
	if err != nil {
		return 0, 0, false, err
	}
	return book.vwap(!sell, qty)
}

// InvVWAP returns the inverse volume weighted average price for a certain quantity of the quote asset on a market.
// SubscribeMarket must be called, and the market must be synced before results can be expected.
func (bg *bitget) InvVWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	book, err := bg.book(baseID, quoteID)
	if err != nil {
		return 0, 0, false, err
	}
	return book.invVWAP(!sell, qty)
}

// MidGap returns the mid-gap price for a market.
func (bg *bitget) MidGap(baseID, quoteID uint32) uint64 {
	book, err := bg.book(baseID, quoteID)
	if err != nil {
		return 0
	}
	return book.midGap()
}

// Book returns the orderbook as MiniOrders
func (bg *bitget) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	book, err := bg.book(baseID, quoteID)
	if err != nil {
		return nil, nil, err
	}

	book.mtx.RLock()
	defer book.mtx.RUnlock()

	bFactor := book.baseConversionFactor
	qFactor := book.quoteConversionFactor

	// Convert bids
	buys = make([]*core.MiniOrder, 0, book.bids.Len())
	for curr := book.bids.Front(); curr != nil; curr = curr.Next() {
		e := curr.Value.(*bitgetObEntry)
		buys = append(buys, &core.MiniOrder{
			Qty:       float64(e.qty) / float64(bFactor),
			QtyAtomic: e.qty,
			Rate:      calc.ConventionalRateAlt(e.rate, bFactor, qFactor),
			MsgRate:   e.rate,
			Sell:      false,
		})
	}

	// Convert asks
	sells = make([]*core.MiniOrder, 0, book.asks.Len())
	for curr := book.asks.Front(); curr != nil; curr = curr.Next() {
		e := curr.Value.(*bitgetObEntry)
		sells = append(sells, &core.MiniOrder{
			Qty:       float64(e.qty) / float64(bFactor),
			QtyAtomic: e.qty,
			Rate:      calc.ConventionalRateAlt(e.rate, bFactor, qFactor),
			MsgRate:   e.rate,
			Sell:      true,
		})
	}

	return buys, sells, nil
}

// ensureMarketConnection ensures the market WebSocket is connected
func (bg *bitget) ensureMarketConnection(ctx context.Context) error {
	// Check if already connected
	bg.marketStreamMtx.RLock()
	isConnected := bg.marketStream != nil && !bg.marketStream.IsDown()
	bg.marketStreamMtx.RUnlock()

	if isConnected {
		return nil
	}

	bg.marketStreamMtx.Lock()
	defer bg.marketStreamMtx.Unlock()

	// Double-check after acquiring lock
	if bg.marketStream != nil && !bg.marketStream.IsDown() {
		return nil
	}

	bg.log.Infof("Initializing market WebSocket connection (lazy)...")

	connectEventFunc := func(cs comms.ConnectionStatus) {
		if cs != comms.Disconnected && cs != comms.Connected {
			return
		}
		// Notify all orderbooks of connection status change
		connected := cs == comms.Connected
		bg.booksMtx.RLock()
		defer bg.booksMtx.RUnlock()
		for _, book := range bg.books {
			select {
			case book.connectedChan <- connected:
			default:
			}
		}
	}

	marketWSCfg := &comms.WsCfg{
		URL:              bg.wsPublic,
		PingWait:         120 * time.Second,
		Logger:           bg.log.SubLogger("WS-MKT"),
		RawHandler:       bg.handleMarketWsMessage,
		ReconnectSync:    bg.onMarketReconnect,
		ConnectEventFunc: connectEventFunc,
	}

	marketWS, err := comms.NewWsConn(marketWSCfg)
	if err != nil {
		return fmt.Errorf("create market ws: %w", err)
	}

	bg.marketStream = marketWS

	_, err = marketWS.Connect(ctx)
	if err != nil {
		return fmt.Errorf("market ws connect error: %w", err)
	}

	bg.log.Infof("Market WebSocket connected successfully")

	// Keep-alive ping for market WebSocket
	// Bitget requires clients to send string "ping" every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Send raw string "ping" as per Bitget API docs
				bg.marketStreamMtx.RLock()
				if bg.marketStream != nil && !bg.marketStream.IsDown() {
					if err := bg.marketStream.SendRaw([]byte("ping")); err != nil {
						bg.log.Errorf("Failed to send market WebSocket ping: %v", err)
					}
				}
				bg.marketStreamMtx.RUnlock()
			case <-bg.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// getOrderbookSnapshot fetches an orderbook snapshot via REST API
func (bg *bitget) getOrderbookSnapshot(ctx context.Context, symbol string) (*bgtypes.OrderbookSnapshot, error) {
	params := map[string]string{
		"symbol": symbol,
		"limit":  "100",
	}

	var resp bgtypes.APIResponse
	err := bg.getAPI(ctx, "/api/v2/spot/market/orderbook", params, false, &resp)
	if err != nil {
		bg.log.Errorf("Failed to get %s orderbook snapshot: %v", symbol, err)
		return nil, err
	}

	if resp.Code != "00000" {
		bg.log.Errorf("Bitget API error for %s orderbook: code=%s, msg=%s", symbol, resp.Code, resp.Msg)
		return nil, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var snapshot bgtypes.OrderbookSnapshot
	if err := json.Unmarshal(resp.Data, &snapshot); err != nil {
		bg.log.Errorf("Failed to unmarshal %s orderbook snapshot: %v", symbol, err)
		return nil, fmt.Errorf("error unmarshaling orderbook snapshot: %w", err)
	}

	return &snapshot, nil
}

// handleMarketWsMessage handles incoming market WebSocket messages
func (bg *bitget) handleMarketWsMessage(b []byte) {
	// Check for raw string "ping" from server
	if string(b) == "ping" {
		// Respond with raw string "pong"
		bg.marketStreamMtx.RLock()
		if bg.marketStream != nil {
			bg.marketStream.SendRaw([]byte("pong"))
		}
		bg.marketStreamMtx.RUnlock()
		return
	}

	// Check for JSON ping (legacy, might not be used by Bitget anymore)
	var pingMsg bgtypes.WsPingMessage
	if err := json.Unmarshal(b, &pingMsg); err == nil && pingMsg.Op == wsOpPing {
		// Respond with pong
		pongMsg := &bgtypes.WsPongMessage{Op: wsOpPong}
		pongBytes, err := json.Marshal(pongMsg)
		if err == nil {
			bg.marketStreamMtx.RLock()
			if bg.marketStream != nil {
				bg.marketStream.SendRaw(pongBytes)
			}
			bg.marketStreamMtx.RUnlock()
		}
		return
	}

	// Check for response (subscribe, unsubscribe, etc.)
	var wsResp bgtypes.WsResponse
	if err := json.Unmarshal(b, &wsResp); err == nil {
		if wsResp.Event != "" {
			if wsResp.Code != "0" && wsResp.Code != "" {
				bg.log.Errorf("Market WebSocket error: event=%s, code=%s, msg=%s", wsResp.Event, wsResp.Code, wsResp.Msg)
			} else {
				bg.log.Tracef("Market WebSocket event: %s", wsResp.Event)
			}
			return
		}
	}

	// Check for data message (orderbook updates)
	var dataMsg bgtypes.WsDataMessage
	if err := json.Unmarshal(b, &dataMsg); err == nil && dataMsg.Arg != nil {
		if dataMsg.Arg.Channel == wsChannelBooks {
			bg.handleOrderbookUpdate(&dataMsg)
		}
		return
	}

	bg.log.Tracef("Unhandled market WebSocket message: %s", string(b))
}

// handleOrderbookUpdate processes orderbook updates from WebSocket
func (bg *bitget) handleOrderbookUpdate(msg *bgtypes.WsDataMessage) {
	if msg.Arg == nil || msg.Arg.InstId == "" {
		return
	}

	symbol := msg.Arg.InstId
	isSnapshot := (msg.Action == "snapshot")

	bg.booksMtx.RLock()
	book, exists := bg.books[symbol]
	bg.booksMtx.RUnlock()

	if !exists {
		return
	}

	for _, dataItem := range msg.Data {
		dataBytes, err := json.Marshal(dataItem)
		if err != nil {
			bg.log.Errorf("%s: error marshaling orderbook data: %v", symbol, err)
			continue
		}

		var bookData bgtypes.WsBookData
		if err := json.Unmarshal(dataBytes, &bookData); err != nil {
			bg.log.Errorf("%s: error unmarshaling orderbook data: %v", symbol, err)
			continue
		}

		// Validate sequence number (if provided)
		// Note: Bitget sequences are NOT consecutive per symbol. They appear to be
		// global timestamps/IDs. We only check for monotonic increase (no backwards movement).
		if bookData.Seq > 0 {
			if isSnapshot {
				// Snapshots are the source of truth - always reset sequence
				book.lastSeq.Store(bookData.Seq)
				bg.log.Tracef("%s: Snapshot received, sequence reset to %d", symbol, bookData.Seq)
			} else {
				// For updates, only check that new seq > last seq (not consecutive)
				lastSeq := book.lastSeq.Load()
				if lastSeq > 0 && bookData.Seq <= lastSeq {
					// This is duplicate or old data - skip it
					bg.log.Warnf("%s: Stale sequence: got %d, last %d (skipping)",
						symbol, bookData.Seq, lastSeq)
					continue
				}
				// Accept update and store new sequence
				book.lastSeq.Store(bookData.Seq)
				if lastSeq > 0 {
					bg.log.Tracef("%s: Sequence update: %d -> %d (gap: %d)",
						symbol, lastSeq, bookData.Seq, bookData.Seq-lastSeq)
				}
			}
		}

		// Update timestamp tracking
		if bookData.Ts != "" {
			ts, err := strconv.ParseInt(bookData.Ts, 10, 64)
			if err != nil {
				bg.log.Warnf("%s: Failed to parse timestamp %s: %v", symbol, bookData.Ts, err)
			} else {
				book.lastUpdateTs.Store(ts)
			}
		}

		// Create update with string data - conversion happens once in orderbook.update()
		update := &bgtypes.BookUpdate{
			Bids:       bookData.Bids,
			Asks:       bookData.Asks,
			IsSnapshot: isSnapshot,
			Checksum:   int32(bookData.Checksum),
		}

		select {
		case book.updateQueue <- update:
		default:
			bg.log.Warnf("%s: orderbook update queue full, dropping update", symbol)
		}
	}
}

// parseFloatOrZero parses a string to float64, returning 0 for empty strings
// and logging warnings only for actual parsing errors (non-empty strings that fail to parse)
func parseFloatOrZero(s string, fieldName, symbol string, log dex.Logger) float64 {
	if s == "" {
		// Empty strings are expected for some ticker fields (e.g., new markets, no recent activity)
		return 0
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		log.Warnf("Failed to parse %s for %s: %v", fieldName, symbol, err)
		return 0
	}
	return val
}

// calculateBookChecksum calculates CRC32 checksum per Bitget specification.
// Uses the original string format from WebSocket messages (preserving exact precision).
// Per Bitget docs: https://www.bitget.com/api-doc/spot/websocket/public/Depth-Channel
// Algorithm:
//   - Alternate bid:ask pairs up to 25 levels: bid1:ask1:bid2:ask2:...
//   - If one side has fewer than 25 levels, continue with remaining levels from other side
//   - Example: 1 bid, 3 asks → "bid1:ask1:ask2:ask3"
//   - CRITICAL: Must use exact original strings (e.g., "0.5000" not "0.5")
func calculateBookChecksum(bidsOrig, asksOrig [][]string) int32 {
	var parts []string
	maxLevels := 25

	// Alternate between bids and asks up to 25 levels each
	// Use original string format to preserve exact precision
	for i := 0; i < maxLevels; i++ {
		if i < len(bidsOrig) && len(bidsOrig[i]) >= 2 {
			// Use original string format directly (price:quantity)
			parts = append(parts, fmt.Sprintf("%s:%s", bidsOrig[i][0], bidsOrig[i][1]))
		}
		if i < len(asksOrig) && len(asksOrig[i]) >= 2 {
			// Use original string format directly (price:quantity)
			parts = append(parts, fmt.Sprintf("%s:%s", asksOrig[i][0], asksOrig[i][1]))
		}
	}

	checksumStr := strings.Join(parts, ":")
	return int32(crc32.ChecksumIEEE([]byte(checksumStr)))
}

// onMarketReconnect is called when the market WebSocket reconnects.
// Note: The ConnectEventFunc callback handles notifying orderbooks to trigger
// REST snapshots. This function only handles re-subscribing to WebSocket channels.
func (bg *bitget) onMarketReconnect() {
	bg.log.Infof("Market WebSocket reconnected, re-subscribing to orderbooks...")

	bg.booksMtx.RLock()
	symbols := make([]string, 0, len(bg.books))
	for symbol := range bg.books {
		symbols = append(symbols, symbol)
	}
	bg.booksMtx.RUnlock()

	// Re-subscribe to all active orderbooks
	for _, symbol := range symbols {
		subscribeReq := &bgtypes.WsRequest{
			Op: wsOpSubscribe,
			Args: []bgtypes.WsArg{
				{
					InstType: instTypeSpot,
					Channel:  wsChannelBooks,
					InstId:   symbol,
				},
			},
		}

		subBytes, err := json.Marshal(subscribeReq)
		if err != nil {
			bg.log.Errorf("failed to marshal subscribe request for %s: %v", symbol, err)
			continue
		}

		bg.marketStreamMtx.RLock()
		if bg.marketStream != nil && !bg.marketStream.IsDown() {
			if err := bg.marketStream.SendRaw(subBytes); err != nil {
				bg.log.Errorf("failed to re-subscribe to %s: %v", symbol, err)
			}
		}
		bg.marketStreamMtx.RUnlock()
	}
}

// Balance returns the balance of an asset
func (bg *bitget) Balance(assetID uint32) (*ExchangeBalance, error) {
	assetConfig, err := bitgetAssetCfg(assetID)
	if err != nil {
		return nil, err
	}

	bg.balanceMtx.RLock()
	defer bg.balanceMtx.RUnlock()

	bal, found := bg.balances[assetConfig.assetID]
	if !found {
		return nil, fmt.Errorf("no %q balance found", assetConfig.coin)
	}

	return bal, nil
}

// Balances returns balances for all known assets
func (bg *bitget) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	// Check if we need to refresh without holding the lock
	bg.balanceMtx.RLock()
	needsRefresh := len(bg.balances) == 0
	bg.balanceMtx.RUnlock()

	// Refresh if needed (without holding any lock)
	if needsRefresh {
		if err := bg.refreshBalances(ctx); err != nil {
			return nil, err
		}
	}

	// Now read the balances
	bg.balanceMtx.RLock()
	defer bg.balanceMtx.RUnlock()

	balances := make(map[uint32]*ExchangeBalance)
	for assetID, bal := range bg.balances {
		assetConfig, err := bitgetAssetCfg(assetID)
		if err != nil {
			// Asset ID not found in Bitget config - skip this balance
			bg.log.Tracef("Asset ID %d not found in Bitget config: %v", assetID, err)
			continue
		}
		balances[assetConfig.assetID] = bal
	}

	return balances, nil
}

// refreshBalances queries Bitget for the user's balances and updates the cache
func (bg *bitget) refreshBalances(ctx context.Context) error {
	var resp bgtypes.APIResponse
	err := bg.getAPI(ctx, "/api/v2/spot/account/assets", nil, true, &resp)
	if err != nil {
		return err
	}

	if resp.Code != "00000" {
		return fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var balances []*bgtypes.AssetBalance
	if err := json.Unmarshal(resp.Data, &balances); err != nil {
		return fmt.Errorf("error unmarshaling balances: %w", err)
	}

	tokenIDsI := bg.tokenIDs.Load()
	if tokenIDsI == nil {
		return errors.New("cannot set balances before coin info is fetched")
	}
	tokenIDs := tokenIDsI.(map[string][]uint32)

	bg.balanceMtx.Lock()
	defer bg.balanceMtx.Unlock()

	for _, bal := range balances {
		assetIDs := bitgetGetDEXAssetIDs(bal.Coin, tokenIDs)
		if len(assetIDs) == 0 {
			continue
		}

		for _, assetID := range assetIDs {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				bg.log.Errorf("no unit info for known asset ID %d?", assetID)
				continue
			}

			// Parse balances - empty strings should not occur for balances, but handle gracefully
			available, err := strconv.ParseFloat(bal.Available, 64)
			if err != nil {
				if bal.Available != "" {
					bg.log.Warnf("Failed to parse available balance for %s: %v", bal.Coin, err)
				}
				available = 0
			}
			locked, err := strconv.ParseFloat(bal.Locked, 64)
			if err != nil {
				if bal.Locked != "" {
					bg.log.Warnf("Failed to parse locked balance for %s: %v", bal.Coin, err)
				}
				locked = 0
			}

			updatedBalance := &ExchangeBalance{
				Available: uint64(math.Round(available * float64(ui.Conventional.ConversionFactor))),
				Locked:    uint64(math.Round(locked * float64(ui.Conventional.ConversionFactor))),
			}

			currBalance, found := bg.balances[assetID]
			if found && *currBalance != *updatedBalance {
				oldAvail := float64(currBalance.Available) / float64(ui.Conventional.ConversionFactor)
				oldLocked := float64(currBalance.Locked) / float64(ui.Conventional.ConversionFactor)
				newAvail := float64(updatedBalance.Available) / float64(ui.Conventional.ConversionFactor)
				newLocked := float64(updatedBalance.Locked) / float64(ui.Conventional.ConversionFactor)
				bg.log.Warnf("%s balance was out of sync. Updating: Available %.8f → %.8f, Locked %.8f → %.8f",
					bal.Coin, oldAvail, newAvail, oldLocked, newLocked)
			}

			bg.balances[assetID] = updatedBalance
		}
	}

	return nil
}

// Markets returns the list of markets at Bitget
func (bg *bitget) Markets(ctx context.Context) (map[string]*Market, error) {
	bg.marketSnapshotMtx.Lock()
	defer bg.marketSnapshotMtx.Unlock()

	const snapshotTimeout = time.Minute * 30
	if bg.marketSnapshot.m != nil && time.Since(bg.marketSnapshot.stamp) < snapshotTimeout {
		return bg.marketSnapshot.m, nil
	}

	matches, err := bg.MatchedMarkets(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting market list: %w", err)
	}

	// Group MarketMatches by Slug (like Binance)
	// Use uppercase for case-insensitive matching with ticker symbols
	mkts := make(map[string][]*MarketMatch, len(matches))
	for _, m := range matches {
		slugUpper := strings.ToUpper(m.Slug)
		mkts[slugUpper] = append(mkts[slugUpper], m)
	}

	// Get 24hr ticker data
	var resp bgtypes.APIResponse
	err = bg.getAPI(ctx, "/api/v2/spot/market/tickers", nil, false, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var tickers []*bgtypes.TickerData
	if err := json.Unmarshal(resp.Data, &tickers); err != nil {
		return nil, fmt.Errorf("error unmarshaling tickers: %w", err)
	}

	// Iterate through ticker responses, not MarketMatches (like Binance)
	m := make(map[string]*Market, len(tickers))
	for _, ticker := range tickers {
		// Normalize to uppercase for case-insensitive matching
		tickerSymbolUpper := strings.ToUpper(ticker.Symbol)
		ms, found := mkts[tickerSymbolUpper]
		if !found {
			// Ticker returned but doesn't match any MarketMatch Slug - skip it
			continue
		}
		// Create Market entry for EACH MarketMatch in this Slug group
		for _, mkt := range ms {
			baseMinWithdraw, quoteMinWithdraw := bg.minimumWithdraws(mkt.BaseID, mkt.QuoteID)

			// Parse ticker data - empty strings are expected for some fields and handled gracefully
			high24h := parseFloatOrZero(ticker.High24h, "high24h", ticker.Symbol, bg.log)
			low24h := parseFloatOrZero(ticker.Low24h, "low24h", ticker.Symbol, bg.log)
			close := parseFloatOrZero(ticker.Close, "close", ticker.Symbol, bg.log)
			quoteVol := parseFloatOrZero(ticker.QuoteVol, "quoteVol", ticker.Symbol, bg.log)
			baseVol := parseFloatOrZero(ticker.BaseVol, "baseVol", ticker.Symbol, bg.log)
			openUtc := parseFloatOrZero(ticker.OpenUtc, "openUtc", ticker.Symbol, bg.log)
			changeUtc24h := parseFloatOrZero(ticker.ChangeUtc24h, "changeUtc24h", ticker.Symbol, bg.log)

			// Calculate PriceChangePct with division by zero safety
			var priceChangePct float64
			if openUtc != 0 {
				priceChangePct = (close - openUtc) / openUtc * 100
			} else {
				priceChangePct = 0
			}

			m[mkt.MarketID] = &Market{
				BaseID:           mkt.BaseID,
				QuoteID:          mkt.QuoteID,
				BaseMinWithdraw:  baseMinWithdraw,
				QuoteMinWithdraw: quoteMinWithdraw,
				Day: &MarketDay{
					Vol:            baseVol,
					QuoteVol:       quoteVol,
					PriceChange:    changeUtc24h,
					PriceChangePct: priceChangePct,
					AvgPrice:       (high24h + low24h) / 2,
					LastPrice:      close,
					OpenPrice:      openUtc,
					HighPrice:      high24h,
					LowPrice:       low24h,
				},
			}
		}
	}

	bg.marketSnapshot.m = m
	bg.marketSnapshot.stamp = time.Now()

	return m, nil
}

// MatchedMarkets returns all markets that are supported by both Bitget and DEX
func (bg *bitget) MatchedMarkets(ctx context.Context) ([]*MarketMatch, error) {
	if tokenIDsI := bg.tokenIDs.Load(); tokenIDsI == nil {
		if err := bg.getCoinInfo(ctx); err != nil {
			return nil, fmt.Errorf("error getting coin info for token IDs: %v", err)
		}
	}
	tokenIDs := bg.tokenIDs.Load().(map[string][]uint32)

	bgMarkets := bg.markets.Load().(map[string]*bgtypes.Market)
	if len(bgMarkets) == 0 {
		var err error
		bgMarkets, err = bg.getMarkets(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting markets: %v", err)
		}
		bg.markets.Store(bgMarkets)
	}

	markets := make([]*MarketMatch, 0, len(bgMarkets))
	noBaseCount := 0
	noQuoteCount := 0

	for _, mkt := range bgMarkets {
		baseAssetIDs := bitgetGetDEXAssetIDs(mkt.BaseCoin, tokenIDs)
		if len(baseAssetIDs) == 0 {
			noBaseCount++
			continue
		}

		quoteAssetIDs := bitgetGetDEXAssetIDs(mkt.QuoteCoin, tokenIDs)
		if len(quoteAssetIDs) == 0 {
			noQuoteCount++
			continue
		}

		dexMarkets := bitgetMarketToDexMarkets(mkt.BaseCoin, mkt.QuoteCoin, tokenIDs)
		markets = append(markets, dexMarkets...)
	}

	bg.log.Infof("Bitget: Loaded %d DEX markets from %d Bitget symbols (skipped: %d no base support, %d no quote support)",
		len(markets), len(bgMarkets), noBaseCount, noQuoteCount)
	return markets, nil
}

// minimumWithdraws returns the minimum transfer amounts for base and quote assets.
// These minimums satisfy both deposit and withdrawal requirements (uses the maximum of both).
func (bg *bitget) minimumWithdraws(baseID, quoteID uint32) (base uint64, quote uint64) {
	minsI := bg.minWithdraw.Load()
	if minsI == nil {
		return 0, 0
	}
	mins := minsI.(map[uint32]*bitgetWithdrawInfo)
	if baseInfo, found := mins[baseID]; found {
		base = baseInfo.minimum
	}
	if quoteInfo, found := mins[quoteID]; found {
		quote = quoteInfo.minimum
	}
	return
}

// withdrawLotSize returns the lot size for withdrawals
func (bg *bitget) withdrawLotSize(assetID uint32) (uint64, error) {
	minsI := bg.minWithdraw.Load()
	if minsI == nil {
		return 0, fmt.Errorf("no withdraw info")
	}
	mins := minsI.(map[uint32]*bitgetWithdrawInfo)
	if info, found := mins[assetID]; found {
		return info.lotSize, nil
	}
	return 0, fmt.Errorf("no withdraw info for asset ID %d", assetID)
}

// withdrawInfo returns withdrawal information for an asset
func (bg *bitget) withdrawInfo(assetID uint32) (*bitgetWithdrawInfo, error) {
	minsI := bg.minWithdraw.Load()
	if minsI == nil {
		return nil, fmt.Errorf("no withdraw info")
	}
	mins := minsI.(map[uint32]*bitgetWithdrawInfo)
	if info, found := mins[assetID]; found {
		return info, nil
	}
	return nil, fmt.Errorf("no withdraw info for asset ID %d", assetID)
}

// Helper functions for trade execution

// bitgetQtyToString converts a quantity in atoms to a Bitget-compatible string
func bitgetQtyToString(qty uint64, assetCfg *bitgetAssetConfig, lotSize uint64) string {
	steppedQty := steppedQty(qty, lotSize)
	convQty := float64(steppedQty) / float64(assetCfg.conversionFactor)
	qtyPrec := int(math.Round(math.Log10(float64(assetCfg.conversionFactor) / float64(lotSize))))
	return strconv.FormatFloat(convQty, 'f', qtyPrec, 64)
}

// bitgetRateToString converts a rate in message rate encoding to a Bitget-compatible string
func bitgetRateToString(rate uint64, baseCfg, quoteCfg *bitgetAssetConfig, rateStep uint64) string {
	rate = steppedRate(rate, rateStep)
	convRate := calc.ConventionalRateAlt(rate, baseCfg.conversionFactor, quoteCfg.conversionFactor)
	convRateStep := calc.ConventionalRate(rateStep, *baseCfg.ui, *quoteCfg.ui)
	ratePrec := -int(math.Round(math.Log10(convRateStep)))
	return strconv.FormatFloat(convRate, 'f', ratePrec, 64)
}

// buildBitgetTradeRequest constructs a Bitget order request from trade parameters
func buildBitgetTradeRequest(baseCfg, quoteCfg *bitgetAssetConfig, market *bgtypes.Market, sell bool, orderType OrderType, rate, qty, quoteQty uint64, tradeID string) (*bgtypes.OrderRequest, uint64, error) {
	if qty > 0 && quoteQty > 0 {
		return nil, 0, fmt.Errorf("cannot specify both quantity and quote quantity")
	}
	if sell && quoteQty > 0 {
		return nil, 0, fmt.Errorf("quote quantity cannot be used for sell orders")
	}
	if !sell && orderType == OrderTypeMarket && qty > 0 {
		return nil, 0, fmt.Errorf("quoteQty MUST be used for market buys")
	}

	side := "buy"
	if sell {
		side = "sell"
	}
	orderTypeStr := "limit"
	if orderType == OrderTypeMarket {
		orderTypeStr = "market"
	}

	var priceStr, sizeStr, quoteSizeStr string
	var qtyToReturn uint64

	if orderType == OrderTypeLimit || orderType == OrderTypeLimitIOC {
		qtyToReturn = qty
		if quoteQty > 0 {
			qtyToReturn = steppedQty(calc.QuoteToBase(rate, quoteQty), market.LotSize)
		}

		if qtyToReturn < market.MinQty || qtyToReturn > market.MaxQty {
			return nil, 0, fmt.Errorf("quantity %s is out of bounds. min: %s, max: %s",
				baseCfg.ui.FormatConventional(qtyToReturn),
				baseCfg.ui.FormatConventional(market.MinQty),
				baseCfg.ui.FormatConventional(market.MaxQty))
		}

		notional := calc.BaseToQuote(rate, qtyToReturn)
		if notional < market.MinNotional {
			return nil, 0, fmt.Errorf("notional %s < min %s",
				quoteCfg.ui.FormatConventional(notional),
				quoteCfg.ui.FormatConventional(market.MinNotional))
		}

		priceStr = bitgetRateToString(rate, baseCfg, quoteCfg, market.PriceStep)
		sizeStr = bitgetQtyToString(qtyToReturn, baseCfg, market.LotSize)
	} else { // market order
		if quoteQty > 0 {
			quoteSizeStr = bitgetQtyToString(quoteQty, quoteCfg, 1)
			qtyToReturn = quoteQty
		} else {
			if qty < market.MinQty || qty > market.MaxQty {
				return nil, 0, fmt.Errorf("quantity %s is out of bounds. min: %s, max: %s",
					baseCfg.ui.FormatConventional(qty),
					baseCfg.ui.FormatConventional(market.MinQty),
					baseCfg.ui.FormatConventional(market.MaxQty))
			}
			sizeStr = bitgetQtyToString(qty, baseCfg, market.LotSize)
			qtyToReturn = qty
		}
	}

	force := "GTC"
	if orderType == OrderTypeLimitIOC {
		force = "IOC"
	}

	req := &bgtypes.OrderRequest{
		Symbol:    market.Symbol,
		Side:      side,
		OrderType: orderTypeStr,
		Price:     priceStr,
		Size:      sizeStr,
		QuoteSize: quoteSizeStr,
		Force:     force,
		ClientOid: tradeID,
	}

	return req, qtyToReturn, nil
}

// Trading Operations

// SubscribeTradeUpdates returns a channel for trade updates
func (bg *bitget) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	bg.tradeUpdaterMtx.Lock()
	defer bg.tradeUpdaterMtx.Unlock()
	updaterID := bg.tradeUpdateCounter
	bg.tradeUpdateCounter++
	updater := make(chan *Trade, 256)
	bg.tradeUpdaters[updaterID] = updater

	unsubscribe := func() {
		bg.tradeUpdaterMtx.Lock()
		delete(bg.tradeUpdaters, updaterID)
		bg.tradeUpdaterMtx.Unlock()
	}

	return updater, unsubscribe, updaterID
}

// ValidateTrade validates trade parameters
func (bg *bitget) ValidateTrade(baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType) error {
	baseCfg, err := bitgetAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}
	quoteCfg, err := bitgetAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)
	marketsMap := bg.markets.Load().(map[string]*bgtypes.Market)
	market, found := marketsMap[symbol]
	if !found {
		return fmt.Errorf("market not found: %v", symbol)
	}

	_, _, err = buildBitgetTradeRequest(baseCfg, quoteCfg, market, sell, orderType, rate, qty, quoteQty, "")
	return err
}

// Trade executes a trade on Bitget
func (bg *bitget) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType, subscriptionID int) (*Trade, error) {
	baseCfg, err := bitgetAssetCfg(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}
	quoteCfg, err := bitgetAssetCfg(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)
	marketsMap := bg.markets.Load().(map[string]*bgtypes.Market)
	market, found := marketsMap[symbol]
	if !found {
		return nil, fmt.Errorf("market not found: %v", symbol)
	}

	tradeID := bg.generateTradeID()

	orderReq, qtyInRequest, err := buildBitgetTradeRequest(baseCfg, quoteCfg, market, sell, orderType, rate, qty, quoteQty, tradeID)
	if err != nil {
		return nil, fmt.Errorf("error building trade request: %w", err)
	}

	bg.tradeUpdaterMtx.Lock()
	_, found = bg.tradeUpdaters[subscriptionID]
	if !found {
		bg.tradeUpdaterMtx.Unlock()
		return nil, fmt.Errorf("no trade updater with ID %v", subscriptionID)
	}
	bg.tradeInfo[tradeID] = &bitgetTradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
		sell:      sell,
		rate:      rate,
		qty:       qtyInRequest,
		market:    orderType == OrderTypeMarket,
	}
	bg.tradeUpdaterMtx.Unlock()

	var success bool
	defer func() {
		if !success {
			bg.tradeUpdaterMtx.Lock()
			delete(bg.tradeInfo, tradeID)
			bg.tradeUpdaterMtx.Unlock()
		}
	}()

	var resp bgtypes.APIResponse
	err = bg.postAPI(ctx, "/api/v2/spot/trade/place-order", orderReq, true, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var orderResponse bgtypes.OrderResponse
	if err := json.Unmarshal(resp.Data, &orderResponse); err != nil {
		return nil, fmt.Errorf("error unmarshaling order response: %w", err)
	}

	success = true

	// For immediate response, we don't have fill information yet
	// WebSocket updates will provide actual fill details
	return &Trade{
		ID:          tradeID,
		Sell:        sell,
		Rate:        rate,
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		Market:      orderType == OrderTypeMarket,
		BaseFilled:  0,
		QuoteFilled: 0,
		Complete:    false,
	}, nil
}

// CancelTrade cancels a trade on Bitget
func (bg *bitget) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	baseCfg, err := bitgetAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	quoteCfg, err := bitgetAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	// First, check order status via REST API as a fallback
	status, err := bg.TradeStatus(ctx, tradeID, baseID, quoteID)
	if err != nil {
		bg.log.Warnf("Failed to check order status before cancel: %v", err)
	} else if status.Complete {
		return nil // Order is already complete, no need to cancel
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)

	cancelReq := &bgtypes.CancelOrderRequest{
		Symbol:    symbol,
		ClientOid: tradeID,
	}

	var resp bgtypes.APIResponse
	err = bg.postAPI(ctx, "/api/v2/spot/trade/cancel-order", cancelReq, true, &resp)
	if err != nil {
		return err
	}

	if resp.Code != "00000" {
		return fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	return nil
}

// TradeStatus returns the status of a trade on Bitget
func (bg *bitget) TradeStatus(ctx context.Context, tradeID string, baseID, quoteID uint32) (*Trade, error) {
	baseCfg, err := bitgetAssetCfg(baseID)
	if err != nil {
		return nil, err
	}

	quoteCfg, err := bitgetAssetCfg(quoteID)
	if err != nil {
		return nil, err
	}

	symbol := bitgetMktID(baseCfg, quoteCfg)

	params := map[string]string{
		"symbol":    symbol,
		"clientOid": tradeID,
	}

	var resp bgtypes.APIResponse
	err = bg.getAPI(ctx, "/api/v2/spot/trade/orderInfo", params, true, &resp)
	if err != nil {
		return nil, err
	}

	if resp.Code != "00000" {
		return nil, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	// Bitget API returns an array of orders, even when querying by clientOid
	var orderDetails []bgtypes.OrderDetail
	if err := json.Unmarshal(resp.Data, &orderDetails); err != nil {
		return nil, fmt.Errorf("error unmarshaling order detail: %w", err)
	}

	if len(orderDetails) == 0 {
		return nil, fmt.Errorf("order not found: %s", tradeID)
	}

	orderDetail := orderDetails[0]

	sell := orderDetail.Side == "sell"
	market := orderDetail.OrderType == "market"

	// Parse quantities
	size, err := strconv.ParseFloat(orderDetail.Size, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse order size for %s: %v", tradeID, err)
		size = 0
	}
	baseFilled, err := strconv.ParseFloat(orderDetail.BaseVolume, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse base volume for order %s: %v", tradeID, err)
		baseFilled = 0
	}
	quoteFilled, err := strconv.ParseFloat(orderDetail.QuoteVolume, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse quote volume for order %s: %v", tradeID, err)
		quoteFilled = 0
	}
	priceAvg, err := strconv.ParseFloat(orderDetail.PriceAvg, 64)
	if err != nil {
		bg.log.Warnf("Failed to parse average price for order %s: %v", tradeID, err)
		priceAvg = 0
	}

	// Convert to internal units
	qty := uint64(size * float64(baseCfg.conversionFactor))
	baseFilledAmt := uint64(baseFilled * float64(baseCfg.conversionFactor))
	quoteFilledAmt := uint64(quoteFilled * float64(quoteCfg.conversionFactor))

	// Calculate rate from average price
	rateAmt := calc.MessageRateAlt(priceAvg, baseCfg.conversionFactor, quoteCfg.conversionFactor)

	// Parse fees from feeDetail if available
	// Bitget includes fees in the filled amounts already, so we don't subtract them separately

	return &Trade{
		ID:          tradeID,
		Sell:        sell,
		Rate:        rateAmt,
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  baseFilledAmt,
		QuoteFilled: quoteFilledAmt,
		Complete:    orderDetail.Status == bitgetOrderStatusFullFill || orderDetail.Status == bitgetOrderStatusCancelled,
		Market:      market,
	}, nil
}

// Deposit and Withdrawal Operations

// GetDepositAddress returns a deposit address for an asset
func (bg *bitget) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	assetCfg, err := bitgetAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting asset cfg for %d: %w", assetID, err)
	}

	// CRITICAL: Set deposit account to SPOT before getting address
	// This ensures all deposits go directly to SPOT account instead of FUTURES
	modifyReq := &bgtypes.ModifyDepositAccountRequest{
		Coin:        assetCfg.coin,
		AccountType: "spot",
	}

	var modifyResp bgtypes.APIResponse
	err = bg.postAPI(ctx, "/api/v2/spot/wallet/modify-deposit-account", modifyReq, true, &modifyResp)
	if err != nil {
		bg.log.Warnf("Failed to set deposit account to SPOT for %s: %v (continuing anyway)", assetCfg.coin, err)
		// Don't fail completely - try to get deposit address anyway
	} else if modifyResp.Code != "00000" {
		bg.log.Warnf("Bitget API error setting deposit account to SPOT for %s: %s - %s (continuing anyway)",
			assetCfg.coin, modifyResp.Code, modifyResp.Msg)
	} else {
		bg.log.Infof("Set deposit account to SPOT for %s", assetCfg.coin)
	}

	// Now get the deposit address
	params := map[string]string{
		"coin":  assetCfg.coin,
		"chain": assetCfg.chain,
	}

	var resp bgtypes.APIResponse
	err = bg.getAPI(ctx, "/api/v2/spot/wallet/deposit-address", params, true, &resp)
	if err != nil {
		return "", err
	}

	if resp.Code != "00000" {
		return "", fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var depositAddr bgtypes.DepositAddress
	if err := json.Unmarshal(resp.Data, &depositAddr); err != nil {
		return "", fmt.Errorf("error unmarshaling deposit address: %w", err)
	}

	// Bitget may require a tag/memo for some assets
	if depositAddr.Tag != "" {
		return depositAddr.Address + ":" + depositAddr.Tag, nil
	}

	return depositAddr.Address, nil
}

// ConfirmDeposit checks the status of a deposit transaction
func (bg *bitget) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	assetCfg, err := bitgetAssetCfg(deposit.AssetID)
	if err != nil {
		bg.log.Errorf("error getting asset cfg for %d: %v", deposit.AssetID, err)
		return false, 0
	}

	params := map[string]string{
		"coin": assetCfg.coin,
	}

	// Add chain parameter for tokens (required by Bitget API)
	if assetCfg.chain != "" {
		params["chain"] = assetCfg.chain
	}

	// Add startTime to limit results (24 hours ago in milliseconds)
	startTime := time.Now().Add(-24 * time.Hour).UnixMilli()
	params["startTime"] = strconv.FormatInt(startTime, 10)

	// Add endTime (now in milliseconds)
	endTime := time.Now().UnixMilli()
	params["endTime"] = strconv.FormatInt(endTime, 10)

	var resp bgtypes.APIResponse
	err = bg.getAPI(ctx, "/api/v2/spot/wallet/deposit-records", params, true, &resp)
	if err != nil {
		bg.log.Errorf("error getting deposit records: %v", err)
		return false, 0
	}

	if resp.Code != "00000" {
		bg.log.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
		return false, 0
	}

	var deposits []*bgtypes.DepositRecord
	if err := json.Unmarshal(resp.Data, &deposits); err != nil {
		bg.log.Errorf("error unmarshaling deposit records: %v", err)
		return false, 0
	}

	bg.log.Debugf("ConfirmDeposit for txID %s: got %d deposit records", deposit.TxID, len(deposits))

	// Get unit info for amount comparison
	ui, err := asset.UnitInfo(deposit.AssetID)
	if err != nil {
		bg.log.Errorf("failed to find unit info for asset ID %d", deposit.AssetID)
		return false, 0
	}

	// Bitget uses TradeId field for on-chain transaction hash
	for _, status := range deposits {
		// Match by TradeId (case-insensitive for Ethereum-style addresses)
		if status.TradeId != "" && deposit.TxID != "" {
			statusTxID := strings.ToLower(status.TradeId)
			expectedTxID := strings.ToLower(deposit.TxID)

			if statusTxID == expectedTxID {
				bg.log.Infof("Found matching deposit for txID %s (TradeId: %s): status=%s", deposit.TxID, status.TradeId, status.Status)

				switch status.Status {
				case "success":
					// Deposit confirmed
					sizeFloat, err := strconv.ParseFloat(status.Size, 64)
					if err != nil {
						bg.log.Errorf("Failed to parse deposit size for %s: %v", assetCfg.coin, err)
						return false, 0
					}
					amount := uint64(sizeFloat * float64(ui.Conventional.ConversionFactor))
					bg.log.Infof("Deposit confirmed: %s %f credited", assetCfg.coin, sizeFloat)
					return true, amount
				case "pending":
					// Still pending
					bg.log.Debugf("Deposit pending: txID %s", deposit.TxID)
					return false, 0
				case "failed", "rejected":
					// Deposit failed
					bg.log.Warnf("Deposit failed/rejected: txID %s", deposit.TxID)
					return true, 0
				default:
					bg.log.Errorf("unknown deposit status %s for txID %s", status.Status, deposit.TxID)
					return false, 0
				}
			}
		}
	}

	// Deposit not found yet
	bg.log.Debugf("Deposit not found in history: txID %s", deposit.TxID)
	return false, 0
}

// Withdraw initiates a withdrawal to an external address
func (bg *bitget) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string) (string, uint64, error) {
	assetCfg, err := bitgetAssetCfg(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting asset cfg for %d: %w", assetID, err)
	}

	// Get unit info for conversion
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting unit info for asset %d: %w", assetID, err)
	}

	// Get DEX symbol for error messages
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		symbol = fmt.Sprintf("asset_%d", assetID)
	}

	// Validate and step withdrawal amount
	info, err := bg.withdrawInfo(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("cannot verify withdrawal minimum for asset %d (%s): %w. Check if deposits/withdrawals are enabled on Bitget for this network", assetID, symbol, err)
	}

	// Check minimum withdrawal amount
	if qty < info.minimum {
		convQty := float64(qty) / float64(ui.Conventional.ConversionFactor)
		convMin := float64(info.minimum) / float64(ui.Conventional.ConversionFactor)
		return "", 0, fmt.Errorf("withdrawal amount %.8f %s is below Bitget minimum %.8f %s. Configure AutoRebalanceConfig.MinBaseTransfer or MinQuoteTransfer >= %d atomic units",
			convQty, symbol, convMin, symbol, info.minimum)
	}

	// Step to lot size (like binance and mexc do)
	steppedQty := steppedQty(qty, info.lotSize)
	convQty := float64(steppedQty) / float64(assetCfg.conversionFactor)
	prec := int(math.Round(math.Log10(float64(assetCfg.conversionFactor) / float64(info.lotSize))))
	qtyStr := strconv.FormatFloat(convQty, 'f', prec, 64)

	bg.log.Debugf("Withdraw: assetID=%d, symbol=%s, coin=%s, chain=%s, amount=%s, address=%s",
		assetID, symbol, assetCfg.coin, assetCfg.chain, qtyStr, address)

	// Parse address and tag if needed (format: "address:tag")
	var addr, tag string
	if idx := strings.IndexRune(address, ':'); idx > 0 {
		addr = address[:idx]
		tag = address[idx+1:]
	} else {
		addr = address
	}

	withdrawReq := &bgtypes.WithdrawalRequest{
		Coin:         assetCfg.coin,
		TransferType: "on_chain", // Blockchain withdrawal (vs "internal_transfer")
		Chain:        assetCfg.chain,
		Address:      addr,
		Tag:          tag,
		Size:         qtyStr,
	}

	var resp bgtypes.APIResponse
	err = bg.postAPI(ctx, "/api/v2/spot/wallet/withdrawal", withdrawReq, true, &resp)
	if err != nil {
		return "", 0, err
	}

	if resp.Code != "00000" {
		return "", 0, fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var withdrawResp bgtypes.WithdrawalResponse
	if err := json.Unmarshal(resp.Data, &withdrawResp); err != nil {
		return "", 0, fmt.Errorf("error unmarshaling withdrawal response: %w", err)
	}

	return withdrawResp.OrderId, qty, nil
}

// ConfirmWithdrawal checks the status of a withdrawal
func (bg *bitget) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	assetCfg, err := bitgetAssetCfg(assetID)
	if err != nil {
		return 0, "", fmt.Errorf("error getting asset cfg for %d: %w", assetID, err)
	}

	params := map[string]string{
		"coin": assetCfg.coin,
	}

	// Add chain parameter for tokens (required by Bitget API)
	if assetCfg.chain != "" {
		params["chain"] = assetCfg.chain
	}

	// Add startTime to limit results (24 hours ago in milliseconds)
	startTime := time.Now().Add(-24 * time.Hour).UnixMilli()
	params["startTime"] = strconv.FormatInt(startTime, 10)

	// Add endTime (now in milliseconds)
	endTime := time.Now().UnixMilli()
	params["endTime"] = strconv.FormatInt(endTime, 10)

	var resp bgtypes.APIResponse
	err = bg.getAPI(ctx, "/api/v2/spot/wallet/withdrawal-records", params, true, &resp)
	if err != nil {
		return 0, "", err
	}

	if resp.Code != "00000" {
		return 0, "", fmt.Errorf("bitget API error: %s - %s", resp.Code, resp.Msg)
	}

	var withdrawals []*bgtypes.WithdrawalRecord
	if err := json.Unmarshal(resp.Data, &withdrawals); err != nil {
		return 0, "", fmt.Errorf("error unmarshaling withdrawal records: %w", err)
	}

	bg.log.Debugf("ConfirmWithdrawal for ID %s: got %d withdrawal records", withdrawalID, len(withdrawals))

	for _, status := range withdrawals {
		if status.OrderId == withdrawalID {
			bg.log.Infof("Found matching withdrawal for ID %s: status=%s, txID=%s", withdrawalID, status.Status, status.TxId)

			// Bitget withdrawal statuses: success, pending, failed, etc.
			switch status.Status {
			case "success":
				if status.TxId == "" {
					bg.log.Debugf("Withdrawal pending (no txID yet): ID %s", withdrawalID)
					return 0, "", ErrWithdrawalPending
				}

				sizeFloat, err := strconv.ParseFloat(status.Size, 64)
				if err != nil {
					bg.log.Errorf("Failed to parse withdrawal size for %s: %v", assetCfg.coin, err)
					return 0, "", fmt.Errorf("failed to parse withdrawal size: %w", err)
				}
				amt := sizeFloat * float64(assetCfg.conversionFactor)
				bg.log.Infof("Withdrawal confirmed: %s %f, txID=%s", assetCfg.coin, sizeFloat, status.TxId)
				return uint64(amt), status.TxId, nil
			case "pending":
				bg.log.Debugf("Withdrawal pending: ID %s", withdrawalID)
				return 0, "", ErrWithdrawalPending
			case "failed", "rejected":
				bg.log.Warnf("Withdrawal failed/rejected: ID %s", withdrawalID)
				return 0, "", fmt.Errorf("withdrawal failed: %s", status.Status)
			default:
				bg.log.Errorf("Unknown withdrawal status %s for ID %s", status.Status, withdrawalID)
				return 0, "", fmt.Errorf("unknown withdrawal status: %s", status.Status)
			}
		}
	}

	bg.log.Debugf("Withdrawal not found in history: ID %s", withdrawalID)
	return 0, "", fmt.Errorf("withdrawal %s not found", withdrawalID)
}
