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
	"maps"
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
	"decred.org/dcrdex/client/mm/libxc/bntypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/utils"
)

// Binance API spot trading docs:
// https://binance-docs.github.io/apidocs/spot/en/#spot-account-trade

const (
	httpURL      = "https://api.binance.com"
	websocketURL = "wss://stream.binance.com:9443"

	usHttpURL      = "https://api.binance.us"
	usWebsocketURL = "wss://stream.binance.us:9443"

	testnetHttpURL      = "https://testnet.binance.vision"
	testnetWebsocketURL = "wss://testnet.binance.vision"

	// sapi endpoints are not implemented by binance's test network. This url
	// connects to the process at client/cmd/testbinance, which responds to the
	// /sapi/v1/capital/config/getall endpoint.
	fakeBinanceURL   = "http://localhost:37346"
	fakeBinanceWsURL = "ws://localhost:37346"

	bnErrCodeInvalidListenKey = -1125
)

// binanceOrderBook manages an orderbook for a single market. It keeps
// the orderbook synced and allows querying of vwap.
type binanceOrderBook struct {
	mtx            sync.RWMutex
	synced         atomic.Bool
	syncChan       chan struct{}
	numSubscribers uint32
	cm             *dex.ConnectionMaster

	getSnapshot func() (*bntypes.OrderbookSnapshot, error)
	getAvgPrice func() (float64, error)

	book                  *orderbook
	updateQueue           chan *bntypes.BookUpdate
	mktID                 string
	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger
	avgPrice              atomic.Uint64

	connectedChan chan bool
}

func newBinanceOrderBook(
	baseConversionFactor, quoteConversionFactor uint64,
	mktID string,
	getSnapshot func() (*bntypes.OrderbookSnapshot, error),
	getAvgPrice func() (float64, error),
	initialAvgPrice float64,
	log dex.Logger,
) *binanceOrderBook {
	msgAvgPrice := calc.MessageRateAlt(initialAvgPrice, baseConversionFactor, quoteConversionFactor)
	book := &binanceOrderBook{
		book:                  newOrderBook(),
		mktID:                 mktID,
		updateQueue:           make(chan *bntypes.BookUpdate, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
		getSnapshot:           getSnapshot,
		getAvgPrice:           getAvgPrice,
		connectedChan:         make(chan bool),
	}
	book.avgPrice.Store(msgAvgPrice)
	return book
}

// convertBinanceBook converts bids and asks in the binance format,
// with the conventional quantity and rate, to the DEX message format which
// can be used to update the orderbook.
func (b *binanceOrderBook) convertBinanceBook(binanceBids, binanceAsks [][2]json.Number) (bids, asks []*obEntry, err error) {
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

	bids, err = convert(binanceBids)
	if err != nil {
		return nil, nil, err
	}

	asks, err = convert(binanceAsks)
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
func (b *binanceOrderBook) sync(ctx context.Context) {
	cm := dex.NewConnectionMaster(b)
	b.mtx.Lock()
	b.cm = cm
	b.mtx.Unlock()
	if err := cm.ConnectOnce(ctx); err != nil {
		b.log.Errorf("Error connecting %s order book: %v", b.mktID, err)
	}
	<-b.syncChan
}

func (b *binanceOrderBook) Connect(ctx context.Context) (*sync.WaitGroup, error /* no errors */) {
	const updateIDUnsynced = math.MaxUint64

	// We'll run two goroutines and synchronize two local vars.
	var syncMtx sync.Mutex
	var syncCache []*bntypes.BookUpdate
	syncChan := make(chan struct{})
	b.syncChan = syncChan
	var updateID uint64 = updateIDUnsynced
	acceptedUpdate := false

	resyncChan := make(chan struct{}, 1)

	desync := func(resync bool) {
		// clear the sync cache, set the special ID, trigger a book refresh.
		syncMtx.Lock()
		defer syncMtx.Unlock()
		syncCache = make([]*bntypes.BookUpdate, 0)
		acceptedUpdate = false
		if updateID != updateIDUnsynced {
			b.synced.Store(false)
			updateID = updateIDUnsynced
			if resync {
				resyncChan <- struct{}{}
			}
		}
	}

	acceptUpdate := func(update *bntypes.BookUpdate) bool {
		if updateID == updateIDUnsynced {
			// Book is still syncing. Add it to the sync cache.
			syncCache = append(syncCache, update)
			return true
		}

		if !acceptedUpdate {
			// On the first update we receive, the update may straddle the last
			// update ID of the snapshot. If the first update ID is greater
			// than the snapshot ID + 1, it means we missed something so we
			// must resync. If the last update ID is less than or equal to the
			// snapshot ID, we can ignore it.
			if update.FirstUpdateID > updateID+1 {
				return false
			} else if update.LastUpdateID <= updateID {
				return true
			}
			// Once we've accepted the first update, the updates must be in
			// sequence.
		} else if update.FirstUpdateID != updateID+1 {
			return false
		}

		acceptedUpdate = true
		updateID = update.LastUpdateID
		bids, asks, err := b.convertBinanceBook(update.Bids, update.Asks)
		if err != nil {
			b.log.Errorf("Error parsing binance book: %v", err)
			// Data is compromised. Trigger a resync.
			return false
		}
		b.book.update(bids, asks)
		return true
	}

	processSyncCache := func(snapshotID uint64) bool {
		syncMtx.Lock()
		defer syncMtx.Unlock()

		updateID = snapshotID
		for _, update := range syncCache {
			if !acceptUpdate(update) {
				return false
			}
		}

		b.synced.Store(true)
		if syncChan != nil {
			close(syncChan)
			syncChan = nil
		}
		return true
	}

	syncOrderbook := func() bool {
		snapshot, err := b.getSnapshot()
		if err != nil {
			b.log.Errorf("Error getting orderbook snapshot: %v", err)
			return false
		}

		bids, asks, err := b.convertBinanceBook(snapshot.Bids, snapshot.Asks)
		if err != nil {
			b.log.Errorf("Error parsing binance book: %v", err)
			return false
		}

		b.log.Debugf("Got %s orderbook snapshot with update ID %d", b.mktID, snapshot.LastUpdateID)

		b.book.clear()
		b.book.update(bids, asks)

		return processSyncCache(snapshot.LastUpdateID)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		processUpdate := func(update *bntypes.BookUpdate) bool {
			syncMtx.Lock()
			defer syncMtx.Unlock()
			return acceptUpdate(update)
		}

		defer wg.Done()
		for {
			select {
			case update := <-b.updateQueue:
				if !processUpdate(update) {
					b.log.Tracef("Bad %s update with ID %d", b.mktID, update.LastUpdateID)
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
					b.log.Debugf("Unsyncing %s orderbook due to disconnect.", b.mktID, retryFrequency)
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
				desync(false) // Clears the syncCache
				retry = time.After(retryFrequency)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		const updateInterval = time.Minute
		timer := time.NewTimer(updateInterval)
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				avgPrice, err := b.getAvgPrice()
				if err != nil {
					b.log.Errorf("Error getting avg price for %s: %v", b.mktID, err)
					timer.Reset(updateInterval)
					continue
				}

				b.log.Debugf("Updating %s avg price to %v", b.mktID, avgPrice)
				msgAvgPrice := calc.MessageRateAlt(avgPrice, b.baseConversionFactor, b.quoteConversionFactor)
				b.avgPrice.Store(msgAvgPrice)
				timer.Reset(updateInterval)
			case <-ctx.Done():
				return
			}
		}
	}()

	return &wg, nil
}

// vwap returns the volume weighted average price for a certain quantity of the
// base asset. It returns an error if the orderbook is not synced.
func (b *binanceOrderBook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, filled, ErrUnsyncedOrderbook
	}

	vwap, extrema, filled = b.book.vwap(bids, qty)
	return
}

func (b *binanceOrderBook) invVWAP(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced.Load() {
		return 0, 0, filled, ErrUnsyncedOrderbook
	}

	vwap, extrema, filled = b.book.invVWAP(bids, qty)
	return
}

func (b *binanceOrderBook) midGap() uint64 {
	return b.book.midGap()
}

// dexToBinanceCoinSymbol maps DEX asset symbols to Binance coin symbols
// Only include mappings that are NOT simple case conversions
var dexToBinanceCoinSymbol = map[string]string{
	"polygon": "POL",
	"weth":    "ETH",
}

// binanceToDexCoinSymbol maps Binance coin symbols to DEX coin symbols.
// These override the reverse mappings from dexToBinanceCoinSymbol.
var binanceToDexCoinSymbol = map[string]string{
	"ETH": "eth",
}

// dexToBinanceNetworkSymbol maps DEX network symbols to Binance network symbols
var dexToBinanceNetworkSymbol = map[string]string{
	"polygon": "MATIC",
}

// dexCoinToWrappedSymbol maps DEX coin symbols to their wrapped version when
// on different networks
var dexCoinToWrappedSymbol = map[string]string{
	"eth": "weth",
}

// binanceToDexSymbol is the complete mapping from Binance symbols to DEX symbols
// Built in init() from all the other mappings
var binanceToDexSymbol = make(map[string]string)

// convertBnCoin converts a binance coin symbol to a dex symbol.
func convertBnCoin(coin string) string {
	if convertedSymbol, found := binanceToDexSymbol[strings.ToUpper(coin)]; found {
		return convertedSymbol
	}
	return strings.ToLower(coin)
}

// convertBnNetwork converts a binance network symbol to a dex symbol.
func convertBnNetwork(network string) string {
	for key, value := range dexToBinanceNetworkSymbol {
		if value == strings.ToUpper(network) {
			return key
		}
	}
	return convertBnCoin(network)
}

// binanceCoinNetworkToDexSymbol takes the coin name and its network name as
// returned by the binance API and returns the DEX symbol.
func binanceCoinNetworkToDexSymbol(coin, network string) string {
	symbol, netSymbol := convertBnCoin(coin), convertBnNetwork(network)
	if symbol == netSymbol {
		return symbol
	}
	// Convert coin to wrapped version if it has a wrapped equivalent
	// Only apply to the coin symbol, not the network symbol
	if wrappedSymbol, found := dexCoinToWrappedSymbol[symbol]; found {
		symbol = wrappedSymbol
	}
	return symbol + "." + netSymbol
}

func mapDexSymbolToBinanceCoin(symbol string) string {
	if binanceSymbol, found := dexToBinanceCoinSymbol[strings.ToLower(symbol)]; found {
		return binanceSymbol
	}
	return strings.ToUpper(symbol)
}

func mapDexSymbolToBinanceNetwork(symbol string) string {
	if binanceSymbol, found := dexToBinanceNetworkSymbol[strings.ToLower(symbol)]; found {
		return binanceSymbol
	}
	return strings.ToUpper(symbol)
}

func init() {
	// Build the binanceToDexSymbol map for coin symbols only
	// Network symbols are handled separately by convertBnNetwork.
	// This is to avoid network symbols affecting coin symbol conversions.
	// The specific reason for this is that the MATIC coin ticker was changed
	// to POL, but the network symbol for Polygon POS is still MATIC. However,
	// MATIC is still returned with a balance of 0 in the balances response.

	// Direct Binance -> DEX coin symbol mappings (highest priority)
	maps.Copy(binanceToDexSymbol, binanceToDexCoinSymbol)

	// From coin symbol mappings (DEX -> Binance, reverse to Binance -> DEX)
	for key, value := range dexToBinanceCoinSymbol {
		// Only add if not already present (lower priority)
		if _, exists := binanceToDexSymbol[value]; !exists {
			binanceToDexSymbol[value] = key
		}
	}
}

type bncAssetConfig struct {
	assetID uint32
	// symbol is the DEX asset symbol, always lower case
	symbol string
	// coin is the asset symbol on binance, always upper case.
	// For a token like USDC, the coin field will be USDC, but
	// symbol field will be usdc.eth.
	coin string
	// chain will be the same as coin for the base assets of
	// a blockchain, but for tokens it will be the chain
	// that the token is hosted such as "ETH".
	chain            string
	conversionFactor uint64
	ui               *dex.UnitInfo
}

func bncAssetCfg(assetID uint32) (*bncAssetConfig, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, err
	}

	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return nil, fmt.Errorf("no symbol found for asset ID %d", assetID)
	}

	parts := strings.Split(symbol, ".")
	coin := mapDexSymbolToBinanceCoin(parts[0])

	var chain string
	if len(parts) > 1 {
		chain = mapDexSymbolToBinanceNetwork(parts[1])
	} else {
		chain = mapDexSymbolToBinanceNetwork(parts[0])
	}

	return &bncAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             coin,
		chain:            chain,
		conversionFactor: ui.Conventional.ConversionFactor,
		ui:               &ui,
	}, nil
}

func bncAssetCfgs(baseID, quoteID uint32) (*bncAssetConfig, *bncAssetConfig, error) {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return nil, nil, err
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return nil, nil, err
	}

	return baseCfg, quoteCfg, nil
}

type tradeInfo struct {
	updaterID int
	baseID    uint32
	quoteID   uint32
	sell      bool
	rate      uint64
	qty       uint64
	market    bool
}

type withdrawInfo struct {
	minimum uint64
	lotSize uint64
}

type BinanceCodedErr struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *BinanceCodedErr) Error() string {
	return fmt.Sprintf("code = %d, msg = %q", e.Code, e.Msg)
}

func errHasBnCode(err error, code int) bool {
	var bnErr *BinanceCodedErr
	if errors.As(err, &bnErr) && bnErr.Code == code {
		return true
	}
	return false
}

type binance struct {
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
	broadcast          func(any)
	isUS               bool

	markets atomic.Value // map[string]*binanceMarket
	// tokenIDs maps the token's symbol to the list of bip ids of the token
	// for each chain for which deposits and withdrawals are enabled on
	// binance.
	tokenIDs    atomic.Value // map[string][]uint32, binance coin ID string -> asset IDs
	minWithdraw atomic.Value // map[uint32]map[uint32]*withdrawInfo

	marketSnapshotMtx sync.Mutex
	marketSnapshot    struct {
		stamp time.Time
		m     map[string]*Market
	}

	balanceMtx sync.RWMutex
	balances   map[uint32]*ExchangeBalance

	commissionRates atomic.Value // *bntypes.CommissionRates

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	marketStreamRespsMtx sync.Mutex
	marketStreamResps    map[uint64]chan<- []string

	booksMtx sync.RWMutex
	books    map[string]*binanceOrderBook

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*tradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	listenKey     atomic.Value // string
	reconnectChan chan struct{}
}

var _ CEX = (*binance)(nil)

// TODO: Investigate stablecoin auto-conversion.
// https://developers.binance.com/docs/wallet/endpoints/switch-busd-stable-coins-convertion

func newBinance(cfg *CEXConfig, binanceUS bool) *binance {
	var marketsURL, accountsURL, wsURL string

	switch cfg.Net {
	case dex.Testnet:
		marketsURL, accountsURL, wsURL = testnetHttpURL, fakeBinanceURL, testnetWebsocketURL
	case dex.Simnet:
		marketsURL, accountsURL, wsURL = fakeBinanceURL, fakeBinanceURL, fakeBinanceWsURL
	default: //mainnet
		if binanceUS {
			marketsURL, accountsURL, wsURL = usHttpURL, usHttpURL, usWebsocketURL
		} else {
			marketsURL, accountsURL, wsURL = httpURL, httpURL, websocketURL
		}
	}

	registeredAssets := asset.Assets()
	knownAssets := make(map[uint32]bool, len(registeredAssets))
	for _, a := range registeredAssets {
		knownAssets[a.ID] = true
	}

	bnc := &binance{
		log:                cfg.Logger,
		broadcast:          cfg.Notify,
		isUS:               binanceUS,
		marketsURL:         marketsURL,
		accountsURL:        accountsURL,
		wsURL:              wsURL,
		apiKey:             cfg.APIKey,
		secretKey:          cfg.SecretKey,
		knownAssets:        knownAssets,
		balances:           make(map[uint32]*ExchangeBalance),
		books:              make(map[string]*binanceOrderBook),
		net:                cfg.Net,
		tradeInfo:          make(map[string]*tradeInfo),
		tradeUpdaters:      make(map[int]chan *Trade),
		tradeIDNoncePrefix: encode.RandomBytes(10),
		reconnectChan:      make(chan struct{}),
		marketStreamResps:  make(map[uint64]chan<- []string),
	}

	bnc.markets.Store(make(map[string]*bntypes.Market))
	bnc.listenKey.Store("")

	return bnc
}

// setBalances queries binance for the user's balances and stores them in the
// balances map.
func (bnc *binance) setBalances(ctx context.Context) error {
	bnc.balanceMtx.Lock()
	defer bnc.balanceMtx.Unlock()
	return bnc.refreshBalances(ctx)
}

func (bnc *binance) refreshBalances(ctx context.Context) error {
	var resp bntypes.Account
	err := bnc.getAPI(ctx, "/api/v3/account", nil, true, true, &resp)
	if err != nil {
		return err
	}

	bnc.commissionRates.Store(resp.CommissionRates)

	tokenIDsI := bnc.tokenIDs.Load()
	if tokenIDsI == nil {
		return errors.New("cannot set balances before coin info is fetched")
	}
	tokenIDs := tokenIDsI.(map[string][]uint32)

	for _, bal := range resp.Balances {
		for _, assetID := range getDEXAssetIDs(bal.Asset, tokenIDs) {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				bnc.log.Errorf("no unit info for known asset ID %d?", assetID)
				continue
			}
			updatedBalance := &ExchangeBalance{
				Available: uint64(math.Round(bal.Free * float64(ui.Conventional.ConversionFactor))),
				Locked:    uint64(math.Round(bal.Locked * float64(ui.Conventional.ConversionFactor))),
			}
			currBalance, found := bnc.balances[assetID]
			if found && *currBalance != *updatedBalance {
				// This function is only called when the CEX is started up, and
				// once every 10 minutes. The balance should be updated by the user
				// data stream, so if it is updated here, it could mean there is an
				// issue.
				bnc.log.Warnf("%v balance was out of sync. Updating. %+v -> %+v", bal.Asset, currBalance, updatedBalance)
			}

			bnc.balances[assetID] = updatedBalance
		}
	}

	return nil
}

// readCoins stores the token IDs for which deposits and withdrawals are
// enabled on binance and sets the minWithdraw map.
func (bnc *binance) readCoins(coins []*bntypes.CoinInfo) {
	tokenIDs := make(map[string][]uint32)
	minWithdraw := make(map[uint32]*withdrawInfo)
	for _, nfo := range coins {
		for _, netInfo := range nfo.NetworkList {
			symbol := binanceCoinNetworkToDexSymbol(nfo.Coin, netInfo.Network)

			assetID, found := dex.BipSymbolID(symbol)
			if !found {
				continue
			}
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				// not a registered asset
				continue
			}
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				bnc.log.Tracef("Skipping %s network %s because deposits and/or withdraws are not enabled.", netInfo.Coin, netInfo.Network)
				continue
			}
			if tkn := asset.TokenInfo(assetID); tkn != nil {
				tokenIDs[nfo.Coin] = append(tokenIDs[nfo.Coin], assetID)
			}
			minimum := uint64(math.Round(float64(ui.Conventional.ConversionFactor) * netInfo.WithdrawMin))
			lotSize := uint64(math.Round(netInfo.WithdrawIntegerMultiple * float64(ui.Conventional.ConversionFactor)))
			if minimum == 0 || lotSize == 0 {
				bnc.log.Errorf("invalid withdraw minimum or lot size for %s network %s", netInfo.Coin, netInfo.Network)
				continue
			}
			minWithdraw[assetID] = &withdrawInfo{
				minimum: minimum,
				lotSize: lotSize,
			}
		}
	}
	bnc.tokenIDs.Store(tokenIDs)
	bnc.minWithdraw.Store(minWithdraw)
}

// getCoinInfo retrieves binance configs then updates the user balances and
// the tokenIDs.
func (bnc *binance) getCoinInfo(ctx context.Context) error {
	coins := make([]*bntypes.CoinInfo, 0)
	err := bnc.getAPI(ctx, "/sapi/v1/capital/config/getall", nil, true, true, &coins)
	if err != nil {
		return err
	}

	bnc.readCoins(coins)
	return nil
}

func parseMarketFilters(market *bntypes.Market, bui, qui dex.UnitInfo) (*bntypes.Market, error) {
	var rateStepFound, lotSizeFound bool

	market.MaxNotional = math.MaxUint64

	for _, filter := range market.Filters {
		switch filter.Type {
		case "PRICE_FILTER":
			rateStepFound = true
			conv := float64(qui.Conventional.ConversionFactor) / float64(bui.Conventional.ConversionFactor) * calc.RateEncodingFactor
			if filter.TickSize == 0 {
				market.RateStep = 1
			} else {
				market.RateStep = uint64(math.Round(filter.TickSize * conv))
			}
			market.MinPrice = uint64(math.Round(filter.MinPrice * conv))
			if filter.MaxPrice == 0 {
				market.MaxPrice = math.MaxUint64
			} else {
				market.MaxPrice = uint64(math.Round(filter.MaxPrice * conv))
			}
		case "LOT_SIZE":
			lotSizeFound = true
			if filter.StepSize == 0 {
				market.LotSize = 1
			} else {
				market.LotSize = uint64(math.Round(filter.StepSize * float64(bui.Conventional.ConversionFactor)))
			}
			market.MinQty = uint64(math.Round(filter.MinQty * float64(bui.Conventional.ConversionFactor)))
			market.MaxQty = uint64(math.Round(filter.MaxQty * float64(bui.Conventional.ConversionFactor)))
		case "NOTIONAL":
			market.MinNotional = uint64(math.Round(filter.MinNotional * float64(qui.Conventional.ConversionFactor)))
			market.MaxNotional = uint64(math.Round(filter.MaxNotional * float64(qui.Conventional.ConversionFactor)))
			market.ApplyMinNotionalToMarket = filter.ApplyMinToMarket
			market.ApplyMaxNotionalToMarket = filter.ApplyMaxToMarket
		case "MIN_NOTIONAL":
			market.MinNotional = uint64(math.Round(filter.MinNotional * float64(qui.Conventional.ConversionFactor)))
			market.ApplyMinNotionalToMarket = filter.ApplyToMarket
		}
	}

	if !rateStepFound || !lotSizeFound {
		return nil, errors.New("missing rate step or lot size filter")
	}

	return market, nil
}

func (bnc *binance) getMarkets(ctx context.Context) (map[string]*bntypes.Market, error) {
	var exchangeInfo bntypes.ExchangeInfo
	err := bnc.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, false, &exchangeInfo)
	if err != nil {
		return nil, err
	}

	marketsMap := make(map[string]*bntypes.Market, len(exchangeInfo.Symbols))
	tokenIDs := bnc.tokenIDs.Load().(map[string][]uint32)

	for _, market := range exchangeInfo.Symbols {
		// Skip markets that are not trading or do not allow you to specify a
		// quote order qty. All markets other than a few margin related markets
		// allow quote order qty for market orders.
		if market.Status != "TRADING" || !market.QuoteOrderQtyMarketAllowed {
			continue
		}

		// Check to see if the assets on this market are supported by the DEX.
		// If not, skip the market.
		//
		// TODO: In the future, we should be able to support markets that the
		// DEX does not support in order to allow more flexibility in multi-hop
		// arbitrage.
		dexMarkets := binanceMarketToDexMarkets(market.BaseAsset, market.QuoteAsset, tokenIDs, bnc.isUS)
		if len(dexMarkets) == 0 {
			continue
		}
		dexMkt := dexMarkets[0]
		bui, _ := asset.UnitInfo(dexMkt.BaseID)
		qui, _ := asset.UnitInfo(dexMkt.QuoteID)

		market, err := parseMarketFilters(market, bui, qui)
		if err != nil {
			bnc.log.Errorf("error parsing market filters for %s: %v", market.Symbol, err)
			continue
		}

		marketsMap[market.Symbol] = market
	}

	bnc.markets.Store(marketsMap)
	return marketsMap, nil
}

// Connect connects to the binance API.
func (bnc *binance) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg := new(sync.WaitGroup)

	if err := bnc.getCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("error getting coin info: %w", err)
	}

	if _, err := bnc.getMarkets(ctx); err != nil {
		return nil, fmt.Errorf("error getting markets: %w", err)
	}

	if err := bnc.setBalances(ctx); err != nil {
		return nil, fmt.Errorf("error getting balances")
	}

	if err := bnc.getUserDataStream(ctx); err != nil {
		return nil, fmt.Errorf("error getting user data stream")
	}

	// Refresh balances periodically. This is just for safety as they should
	// be updated based on the user data stream.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				err := bnc.setBalances(ctx)
				if err != nil {
					bnc.log.Errorf("Error fetching balances: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Refresh the markets periodically.
	wg.Add(1)
	go func() {
		defer wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				_, err := bnc.getMarkets(ctx)
				if err != nil {
					bnc.log.Errorf("Error fetching markets: %v", err)
					nextTick = time.After(time.Minute)
				} else {
					nextTick = time.After(time.Hour)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Refresh the coin info periodically.
	wg.Add(1)
	go func() {
		defer wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				err := bnc.getCoinInfo(ctx)
				if err != nil {
					bnc.log.Errorf("Error fetching markets: %v", err)
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

// Balance returns the balance of an asset at the CEX.
func (bnc *binance) Balance(assetID uint32) (*ExchangeBalance, error) {
	assetConfig, err := bncAssetCfg(assetID)
	if err != nil {
		return nil, err
	}

	bnc.balanceMtx.RLock()
	defer bnc.balanceMtx.RUnlock()

	bal, found := bnc.balances[assetConfig.assetID]
	if !found {
		return nil, fmt.Errorf("no %q balance found", assetConfig.coin)
	}

	return bal, nil
}

func (bnc *binance) generateTradeID() string {
	nonce := bnc.tradeIDNonce.Add(1)
	nonceB := encode.Uint32Bytes(nonce)
	return hex.EncodeToString(append(bnc.tradeIDNoncePrefix, nonceB...))
}

// steppedRate rounds the rate to the nearest integer multiple of the step.
// The minimum returned value is step.
func steppedRate(r, step uint64) uint64 {
	steps := uint64(math.Round(float64(r) / float64(step)))
	if steps == 0 {
		return step
	}
	return steps * step
}

// steppedQty rounds the quantity to the nearest integer multiple of the step
// that is less than or equal to the quantity. This is in order to avoid
// attempting to place a higher quantity order than the user has available.
func steppedQty(qty, step uint64) uint64 {
	steps := uint64(math.Floor(float64(qty) / float64(step)))
	if steps == 0 {
		return step
	}
	return steps * step
}

func qtyToString(qty uint64, assetCfg *bncAssetConfig, lotSize uint64) string {
	steppedQty := steppedQty(qty, lotSize)
	convQty := float64(steppedQty) / float64(assetCfg.conversionFactor)
	qtyPrec := int(math.Round(math.Log10(float64(assetCfg.conversionFactor) / float64(lotSize))))
	return strconv.FormatFloat(convQty, 'f', qtyPrec, 64)
}

func rateToString(rate uint64, baseCfg, quoteCfg *bncAssetConfig, rateStep uint64) string {
	rate = steppedRate(rate, rateStep)
	convRate := calc.ConventionalRateAlt(rate, baseCfg.conversionFactor, quoteCfg.conversionFactor)
	convRateStep := calc.ConventionalRate(rateStep, *baseCfg.ui, *quoteCfg.ui)
	ratePrec := -int(math.Round(math.Log10(convRateStep)))
	return strconv.FormatFloat(convRate, 'f', ratePrec, 64)
}

func buildTradeRequest(baseCfg, quoteCfg *bncAssetConfig, market *bntypes.Market, avgPrice uint64, sell bool, orderType OrderType, rate, qty, quoteQty uint64, tradeID string) (url.Values, uint64, error) {
	if qty > 0 && quoteQty > 0 {
		return nil, 0, fmt.Errorf("cannot specify both quantity and quote quantity")
	}
	if sell && quoteQty > 0 {
		return nil, 0, fmt.Errorf("quote quantity cannot be used for sell orders")
	}
	if !sell && orderType == OrderTypeMarket && qty > 0 {
		return nil, 0, fmt.Errorf("quoteQty MUST be used for market buys")
	}

	side := "BUY"
	if sell {
		side = "SELL"
	}
	orderTypeStr := "LIMIT"
	if orderType == OrderTypeMarket {
		orderTypeStr = "MARKET"
	}

	var rateStr, qtyStr, quoteQtyStr string
	var qtyToReturn uint64

	if orderType == OrderTypeLimit || orderType == OrderTypeLimitIOC {
		if rate < market.MinPrice || rate > market.MaxPrice {
			return nil, 0, fmt.Errorf("rate %v is out of bounds. min: %d, max: %d", rate, market.MinPrice, market.MaxPrice)
		}

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
		if notional > market.MaxNotional {
			return nil, 0, fmt.Errorf("notional %s > max %s",
				quoteCfg.ui.FormatConventional(notional),
				quoteCfg.ui.FormatConventional(market.MaxNotional))
		}

		rateStr = rateToString(rate, baseCfg, quoteCfg, market.RateStep)
		qtyStr = qtyToString(qtyToReturn, baseCfg, market.LotSize)
	} else { // market
		var notional uint64
		if quoteQty > 0 {
			notional = quoteQty
			quoteQtyStr = qtyToString(quoteQty, quoteCfg, 1)
			qtyToReturn = quoteQty
		} else {
			if qty < market.MinQty || qty > market.MaxQty {
				return nil, 0, fmt.Errorf("quantity %s is out of bounds. min: %s, max: %s",
					baseCfg.ui.FormatConventional(qty),
					baseCfg.ui.FormatConventional(market.MinQty),
					baseCfg.ui.FormatConventional(market.MaxQty))
			}
			notional = calc.BaseToQuote(avgPrice, qty)
			qtyStr = qtyToString(qty, baseCfg, market.LotSize)
			qtyToReturn = qty
		}
		if market.ApplyMinNotionalToMarket && notional < market.MinNotional {
			return nil, 0, fmt.Errorf("notional %s < min %s",
				quoteCfg.ui.FormatConventional(notional),
				quoteCfg.ui.FormatConventional(market.MinNotional))
		}
		if market.ApplyMaxNotionalToMarket && notional > market.MaxNotional {
			return nil, 0, fmt.Errorf("notional %s > max %s",
				quoteCfg.ui.FormatConventional(notional),
				quoteCfg.ui.FormatConventional(market.MaxNotional))
		}
	}

	var timeInForceStr string
	if orderType == OrderTypeLimit {
		timeInForceStr = "GTC"
	} else if orderType == OrderTypeLimitIOC {
		timeInForceStr = "IOC"
	}

	// Build the request body
	v := url.Values{}
	v.Add("symbol", market.Symbol)
	v.Add("side", side)
	v.Add("type", orderTypeStr)
	v.Add("newClientOrderId", tradeID)
	if qtyStr != "" {
		v.Add("quantity", qtyStr)
	}
	if rateStr != "" {
		v.Add("price", rateStr)
	}
	if quoteQtyStr != "" {
		v.Add("quoteOrderQty", quoteQtyStr)
	}
	if timeInForceStr != "" {
		v.Add("timeInForce", timeInForceStr)
	}

	return v, qtyToReturn, nil
}

// calcFees returns the total base and quote fees for a trade based on the
// fills. Only one of the two fees, the one the user is receiving in the trade,
// will be non-zero.
func (bnc *binance) calcFees(fills []*bntypes.Fill, baseCfg, quoteCfg *bncAssetConfig) (feeBase, feeQuote uint64) {
	for _, fill := range fills {
		if fill.CommissionAsset == baseCfg.coin {
			feeBase += uint64(fill.Commission * float64(baseCfg.conversionFactor))
		} else if fill.CommissionAsset == quoteCfg.coin {
			feeQuote += uint64(fill.Commission * float64(quoteCfg.conversionFactor))
		}
	}

	if feeBase > 0 && feeQuote > 0 {
		bnc.log.Errorf("calcFees: both base and quote fees are non-zero: %d %d", feeBase, feeQuote)
	}

	return feeBase, feeQuote
}

func (bnc *binance) cachedAvgPrice(mktID string) (uint64, error) {
	bnc.booksMtx.RLock()
	defer bnc.booksMtx.RUnlock()

	book, found := bnc.books[mktID]
	if !found {
		return 0, fmt.Errorf("book not found for %s", mktID)
	}

	avgPrice := book.avgPrice.Load()
	if avgPrice == 0 {
		return 0, fmt.Errorf("avg price not found for %s", mktID)
	}

	return avgPrice, nil
}

// ValidateTrade returns an error if the parameters for this trade are not
// allowed.
func (bnc *binance) ValidateTrade(baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType) error {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}
	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	slug := baseCfg.coin + quoteCfg.coin
	marketsMap := bnc.markets.Load().(map[string]*bntypes.Market)
	market, found := marketsMap[slug]
	if !found {
		return fmt.Errorf("market not found: %v", slug)
	}

	avgPrice, err := bnc.cachedAvgPrice(slug)
	if err != nil {
		return fmt.Errorf("error getting avg price for %s: %w", slug, err)
	}

	_, _, err = buildTradeRequest(baseCfg, quoteCfg, market, avgPrice, sell, orderType, rate, qty, quoteQty, "")
	if err != nil {
		return err
	}

	return nil
}

// Trade executes a trade on the CEX.
//   - subscriptionID takes an ID returned from SubscribeTradeUpdates.
//   - Rate is ignored for market orders.
//   - Qty is in units of base asset, quoteQty is in units of quote asset.
//     Only one of qty or quoteQty should be non-zero.
//   - QuoteQty is only allowed for BUY orders, and it is required for market
//     buy orders.
func (bnc *binance) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty, quoteQty uint64, orderType OrderType, subscriptionID int) (*Trade, error) {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}
	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	slug := baseCfg.coin + quoteCfg.coin
	marketsMap := bnc.markets.Load().(map[string]*bntypes.Market)
	market, found := marketsMap[slug]
	if !found {
		return nil, fmt.Errorf("market not found: %v", slug)
	}

	tradeID := bnc.generateTradeID()

	avgPrice, err := bnc.cachedAvgPrice(slug)
	if err != nil {
		return nil, fmt.Errorf("error getting avg price for %s: %w", slug, err)
	}

	v, qtyInRequest, err := buildTradeRequest(baseCfg, quoteCfg, market, avgPrice, sell, orderType, rate, qty, quoteQty, tradeID)
	if err != nil {
		return nil, fmt.Errorf("error building trade request: %w", err)
	}

	bnc.tradeUpdaterMtx.Lock()
	_, found = bnc.tradeUpdaters[subscriptionID]
	if !found {
		bnc.tradeUpdaterMtx.Unlock()
		return nil, fmt.Errorf("no trade updater with ID %v", subscriptionID)
	}
	bnc.tradeInfo[tradeID] = &tradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
		sell:      sell,
		rate:      rate,
		qty:       qtyInRequest,
		market:    orderType == OrderTypeMarket,
	}
	bnc.tradeUpdaterMtx.Unlock()

	var success bool
	defer func() {
		if !success {
			bnc.tradeUpdaterMtx.Lock()
			delete(bnc.tradeInfo, tradeID)
			bnc.tradeUpdaterMtx.Unlock()
		}
	}()

	var orderResponse bntypes.OrderResponse
	err = bnc.postAPI(ctx, "/api/v3/order", v, nil, true, true, &orderResponse)
	if err != nil {
		return nil, err
	}

	success = true

	baseFees, quoteFees := bnc.calcFees(orderResponse.Fills, baseCfg, quoteCfg)
	baseFilled := uint64(orderResponse.ExecutedQty * float64(baseCfg.conversionFactor))
	quoteFilled := uint64(orderResponse.CumulativeQuoteQty * float64(quoteCfg.conversionFactor))

	return &Trade{
		ID:          tradeID,
		Sell:        sell,
		Rate:        rate,
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		Market:      orderType == OrderTypeMarket,
		BaseFilled:  utils.SafeSub(baseFilled, baseFees),
		QuoteFilled: utils.SafeSub(quoteFilled, quoteFees),
		Complete:    orderResponse.Status != "NEW" && orderResponse.Status != "PARTIALLY_FILLED",
	}, err
}

// ConfirmWithdrawal checks whether a withdrawal has been completed. If the
// withdrawal has not yet been sent, ErrWithdrawalPending is returned.
func (bnc *binance) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	assetCfg, err := bncAssetCfg(assetID)
	if err != nil {
		return 0, "", fmt.Errorf("error getting symbol data for %d: %w", assetID, err)
	}

	type withdrawalHistoryStatus struct {
		ID     string  `json:"id"`
		Amount float64 `json:"amount,string"`
		Status int     `json:"status"`
		TxID   string  `json:"txId"`
	}

	withdrawHistoryResponse := []*withdrawalHistoryStatus{}
	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	err = bnc.getAPI(ctx, "/sapi/v1/capital/withdraw/history", v, true, true, &withdrawHistoryResponse)
	if err != nil {
		return 0, "", err
	}

	var status *withdrawalHistoryStatus
	for _, s := range withdrawHistoryResponse {
		if s.ID == withdrawalID {
			status = s
			break
		}
	}
	if status == nil {
		return 0, "", fmt.Errorf("withdrawal status not found for %s", withdrawalID)
	}

	bnc.log.Tracef("Withdrawal status: %+v", status)

	if status.TxID == "" {
		return 0, "", ErrWithdrawalPending
	}

	amt := status.Amount * float64(assetCfg.conversionFactor)
	return uint64(amt), status.TxID, nil
}

// Withdraw withdraws funds from the CEX to a certain address. onComplete
// is called with the actual amount withdrawn (amt - fees) and the
// transaction ID of the withdrawal.
func (bnc *binance) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string) (string, uint64, error) {
	assetCfg, err := bncAssetCfg(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting symbol data for %d: %w", assetID, err)
	}

	lotSize, err := bnc.withdrawLotSize(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting withdraw lot size for %d: %w", assetID, err)
	}

	steppedQty := steppedQty(qty, lotSize)
	convQty := float64(steppedQty) / float64(assetCfg.conversionFactor)
	prec := int(math.Round(math.Log10(float64(assetCfg.conversionFactor) / float64(lotSize))))
	qtyStr := strconv.FormatFloat(convQty, 'f', prec, 64)

	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	v.Add("network", assetCfg.chain)
	v.Add("address", address)
	v.Add("amount", qtyStr)

	withdrawResp := struct {
		ID string `json:"id"`
	}{}
	err = bnc.postAPI(ctx, "/sapi/v1/capital/withdraw/apply", nil, v, true, true, &withdrawResp)
	if err != nil {
		return "", 0, err
	}

	return withdrawResp.ID, qty, nil
}

// GetDepositAddress returns a deposit address for an asset.
func (bnc *binance) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	assetCfg, err := bncAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting asset cfg for %d: %w", assetID, err)
	}

	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	v.Add("network", assetCfg.chain)

	resp := struct {
		Address string `json:"address"`
	}{}
	err = bnc.getAPI(ctx, "/sapi/v1/capital/deposit/address", v, true, true, &resp)
	if err != nil {
		return "", err
	}

	return resp.Address, nil
}

// ConfirmDeposit is an async function that calls onConfirm when the status of
// a deposit has been confirmed.
func (bnc *binance) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	var resp []*bntypes.PendingDeposit
	// We'll add info for the fake server.
	var query url.Values
	if bnc.accountsURL == fakeBinanceURL {
		bncAsset, err := bncAssetCfg(deposit.AssetID)
		if err != nil {
			bnc.log.Errorf("Error getting asset cfg for %d: %v", deposit.AssetID, err)
			return false, 0
		}

		query = url.Values{
			"txid":    []string{deposit.TxID},
			"amt":     []string{strconv.FormatFloat(deposit.AmountConventional, 'f', 9, 64)},
			"coin":    []string{bncAsset.coin},
			"network": []string{bncAsset.chain},
		}
	}
	// TODO: Use the "startTime" parameter to apply a reasonable limit to
	// this request.
	err := bnc.getAPI(ctx, "/sapi/v1/capital/deposit/hisrec", query, true, true, &resp)
	if err != nil {
		bnc.log.Errorf("error getting deposit status: %v", err)
		return false, 0
	}

	for _, status := range resp {
		if status.TxID == deposit.TxID {
			switch status.Status {
			case bntypes.DepositStatusSuccess, bntypes.DepositStatusCredited:
				symbol := binanceCoinNetworkToDexSymbol(status.Coin, status.Network)
				assetID, found := dex.BipSymbolID(symbol)
				if !found {
					bnc.log.Errorf("Failed to find DEX asset ID for Coin: %s, Network: %s", status.Coin, status.Network)
					return true, 0
				}
				ui, err := asset.UnitInfo(assetID)
				if err != nil {
					bnc.log.Errorf("Failed to find unit info for asset ID %d", assetID)
					return true, 0
				}
				amount := uint64(status.Amount * float64(ui.Conventional.ConversionFactor))
				return true, amount
			case bntypes.DepositStatusPending:
				return false, 0
			case bntypes.DepositStatusWaitingUserConfirm:
				// This shouldn't ever happen.
				bnc.log.Errorf("Deposit %s to binance requires user confirmation!")
				return true, 0
			case bntypes.DepositStatusWrongDeposit:
				return true, 0
			default:
				bnc.log.Errorf("Deposit %s to binance has an unknown status %d", status.Status)
			}
		}
	}

	return false, 0
}

// SubscribeTradeUpdates returns a channel that the caller can use to
// listen for updates to a trade's status. When the subscription ID
// returned from this function is passed as the updaterID argument to
// Trade, then updates to the trade will be sent on the updated channel
// returned from this function.
func (bnc *binance) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	bnc.tradeUpdaterMtx.Lock()
	defer bnc.tradeUpdaterMtx.Unlock()
	updaterID := bnc.tradeUpdateCounter
	bnc.tradeUpdateCounter++
	updater := make(chan *Trade, 256)
	bnc.tradeUpdaters[updaterID] = updater

	unsubscribe := func() {
		bnc.tradeUpdaterMtx.Lock()
		delete(bnc.tradeUpdaters, updaterID)
		bnc.tradeUpdaterMtx.Unlock()
	}

	return updater, unsubscribe, updaterID
}

// CancelTrade cancels a trade.
func (bnc *binance) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", quoteID, err)
	}

	slug := baseCfg.coin + quoteCfg.coin

	v := make(url.Values)
	v.Add("symbol", slug)
	v.Add("origClientOrderId", tradeID)

	return bnc.request(ctx, "DELETE", "/api/v3/order", v, nil, true, true, nil)
}

func (bnc *binance) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	bnc.balanceMtx.RLock()
	defer bnc.balanceMtx.RUnlock()

	if len(bnc.balances) == 0 {
		if err := bnc.refreshBalances(ctx); err != nil {
			return nil, err
		}
	}

	balances := make(map[uint32]*ExchangeBalance)

	for assetID, bal := range bnc.balances {
		assetConfig, err := bncAssetCfg(assetID)
		if err != nil {
			continue
		}

		balances[assetConfig.assetID] = bal
	}

	return balances, nil
}

func (bnc *binance) minimumWithdraws(baseID, quoteID uint32) (base uint64, quote uint64) {
	minsI := bnc.minWithdraw.Load()
	if minsI == nil {
		return 0, 0
	}
	mins := minsI.(map[uint32]*withdrawInfo)
	if baseInfo, found := mins[baseID]; found {
		base = baseInfo.minimum
	}
	if quoteInfo, found := mins[quoteID]; found {
		quote = quoteInfo.minimum
	}
	return
}

func (bnc *binance) withdrawLotSize(assetID uint32) (uint64, error) {
	minsI := bnc.minWithdraw.Load()
	if minsI == nil {
		return 0, fmt.Errorf("no withdraw info")
	}
	mins := minsI.(map[uint32]*withdrawInfo)
	if info, found := mins[assetID]; found {
		return info.lotSize, nil
	}
	return 0, fmt.Errorf("no withdraw info for asset ID %d", assetID)
}

func (bnc *binance) Markets(ctx context.Context) (map[string]*Market, error) {
	bnc.marketSnapshotMtx.Lock()
	defer bnc.marketSnapshotMtx.Unlock()

	const snapshotTimeout = time.Minute * 30
	if bnc.marketSnapshot.m != nil && time.Since(bnc.marketSnapshot.stamp) < snapshotTimeout {
		return bnc.marketSnapshot.m, nil
	}

	matches, err := bnc.MatchedMarkets(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting market list for market data request: %w", err)
	}

	mkts := make(map[string][]*MarketMatch, len(matches))
	for _, m := range matches {
		mkts[m.Slug] = append(mkts[m.Slug], m)
	}
	encSymbols, err := json.Marshal(utils.MapKeys(mkts))
	if err != nil {
		return nil, fmt.Errorf("error encoding symbold for market data request: %w", err)
	}

	q := make(url.Values)
	q.Set("symbols", string(encSymbols))

	var ds []*bntypes.MarketTicker24
	if err = bnc.getAPI(ctx, "/api/v3/ticker/24hr", q, false, false, &ds); err != nil {
		return nil, err
	}

	m := make(map[string]*Market, len(ds))
	for _, d := range ds {
		ms, found := mkts[d.Symbol]
		if !found {
			bnc.log.Errorf("Market %s not returned in market data request", d.Symbol)
			continue
		}
		for _, mkt := range ms {
			baseMinWithdraw, quoteMinWithdraw := bnc.minimumWithdraws(mkt.BaseID, mkt.QuoteID)
			m[mkt.MarketID] = &Market{
				BaseID:           mkt.BaseID,
				QuoteID:          mkt.QuoteID,
				BaseMinWithdraw:  baseMinWithdraw,
				QuoteMinWithdraw: quoteMinWithdraw,
				Day: &MarketDay{
					Vol:            d.Volume,
					QuoteVol:       d.QuoteVolume,
					PriceChange:    d.PriceChange,
					PriceChangePct: d.PriceChangePercent,
					AvgPrice:       d.WeightedAvgPrice,
					LastPrice:      d.LastPrice,
					OpenPrice:      d.OpenPrice,
					HighPrice:      d.HighPrice,
					LowPrice:       d.LowPrice,
				},
			}
		}
	}
	bnc.marketSnapshot.m = m
	bnc.marketSnapshot.stamp = time.Now()

	return m, nil
}

func (bnc *binance) MatchedMarkets(ctx context.Context) (_ []*MarketMatch, err error) {
	if tokenIDsI := bnc.tokenIDs.Load(); tokenIDsI == nil {
		if err := bnc.getCoinInfo(ctx); err != nil {
			return nil, fmt.Errorf("error getting coin info for token IDs: %v", err)
		}
	}
	tokenIDs := bnc.tokenIDs.Load().(map[string][]uint32)

	bnMarkets := bnc.markets.Load().(map[string]*bntypes.Market)
	if len(bnMarkets) == 0 {
		bnMarkets, err = bnc.getMarkets(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting markets: %v", err)
		}
	}
	markets := make([]*MarketMatch, 0, len(bnMarkets))

	for _, mkt := range bnMarkets {
		dexMarkets := binanceMarketToDexMarkets(mkt.BaseAsset, mkt.QuoteAsset, tokenIDs, bnc.isUS)
		markets = append(markets, dexMarkets...)
	}

	return markets, nil
}

func (bnc *binance) getAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing any) error {
	return bnc.request(ctx, http.MethodGet, endpoint, query, nil, key, sign, thing)
}

func (bnc *binance) postAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing any) error {
	return bnc.request(ctx, http.MethodPost, endpoint, query, form, key, sign, thing)
}

func (bnc *binance) request(ctx context.Context, method, endpoint string, query, form url.Values, key, sign bool, thing any) error {
	var fullURL string
	if strings.Contains(endpoint, "sapi") {
		fullURL = bnc.accountsURL + endpoint
	} else {
		fullURL = bnc.marketsURL + endpoint
	}

	if query == nil {
		query = make(url.Values)
	}
	if sign {
		query.Add("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	}
	queryString := query.Encode()
	bodyString := form.Encode()
	header := make(http.Header, 2)
	body := bytes.NewBuffer(nil)
	if bodyString != "" {
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		body = bytes.NewBufferString(bodyString)
	}
	if key || sign {
		header.Set("X-MBX-APIKEY", bnc.apiKey)
	}

	if sign {
		raw := queryString + bodyString
		mac := hmac.New(sha256.New, []byte(bnc.secretKey))
		if _, err := mac.Write([]byte(raw)); err != nil {
			return fmt.Errorf("hmax Write error: %w", err)
		}
		v := url.Values{}
		v.Set("signature", hex.EncodeToString(mac.Sum(nil)))
		if queryString == "" {
			queryString = v.Encode()
		} else {
			queryString = fmt.Sprintf("%s&%s", queryString, v.Encode())
		}
	}
	if queryString != "" {
		fullURL = fmt.Sprintf("%s?%s", fullURL, queryString)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	var bnErr BinanceCodedErr
	if err := dexnet.Do(req, thing, dexnet.WithSizeLimit(1<<24), dexnet.WithErrorParsing(&bnErr)); err != nil {
		bnc.log.Errorf("request error from endpoint %s %q with query = %q, body = %q, bn coded error: %v, msg = %q",
			method, endpoint, queryString, bodyString, &bnErr, bnErr.Msg)
		return errors.Join(err, &bnErr)
	}

	return nil
}

func (bnc *binance) handleOutboundAccountPosition(update *bntypes.StreamUpdate) {
	bnc.log.Debugf("Received outboundAccountPosition: %+v", update)
	for _, bal := range update.Balances {
		bnc.log.Debugf("outboundAccountPosition balance: %+v", bal)
	}

	supportedTokens := bnc.tokenIDs.Load().(map[string][]uint32)
	updates := make([]*BalanceUpdate, 0, len(update.Balances))

	processSymbol := func(symbol string, bal *bntypes.WSBalance) {
		for _, assetID := range getDEXAssetIDs(symbol, supportedTokens) {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				bnc.log.Errorf("no unit info for known asset ID %d?", assetID)
				return
			}
			oldBal := bnc.balances[assetID]
			newBal := &ExchangeBalance{
				Available: uint64(math.Round(bal.Free * float64(ui.Conventional.ConversionFactor))),
				Locked:    uint64(math.Round(bal.Locked * float64(ui.Conventional.ConversionFactor))),
			}
			bnc.balances[assetID] = newBal
			if oldBal != nil && *oldBal != *newBal {
				updates = append(updates, &BalanceUpdate{
					AssetID: assetID,
					Balance: newBal,
				})
			}
		}
	}

	bnc.balanceMtx.Lock()
	for _, bal := range update.Balances {
		processSymbol(bal.Asset, bal)
		if bal.Asset == "ETH" {
			processSymbol("WETH", bal)
		}
	}
	bnc.balanceMtx.Unlock()

	for _, u := range updates {
		bnc.broadcast(u)
	}
}

func (bnc *binance) getTradeUpdater(tradeID string) (chan *Trade, *tradeInfo, error) {
	bnc.tradeUpdaterMtx.RLock()
	defer bnc.tradeUpdaterMtx.RUnlock()

	tradeInfo, found := bnc.tradeInfo[tradeID]
	if !found {
		return nil, nil, fmt.Errorf("info not found for trade ID %v", tradeID)
	}
	updater, found := bnc.tradeUpdaters[tradeInfo.updaterID]
	if !found {
		return nil, nil, fmt.Errorf("no updater with ID %v", tradeID)
	}

	return updater, tradeInfo, nil
}

func (bnc *binance) removeTradeUpdater(tradeID string) {
	bnc.tradeUpdaterMtx.RLock()
	defer bnc.tradeUpdaterMtx.RUnlock()
	delete(bnc.tradeInfo, tradeID)
}

func (bnc *binance) handleExecutionReport(update *bntypes.StreamUpdate) {
	bnc.log.Debugf("Received executionReport: %+v", update)

	status := update.CurrentOrderStatus
	var id string
	if status == "CANCELED" {
		id = update.CancelledOrderID
	} else {
		id = update.ClientOrderID
	}

	updater, tradeInfo, err := bnc.getTradeUpdater(id)
	if err != nil {
		bnc.log.Errorf("Error getting trade updater: %v", err)
		return
	}

	complete := status != "NEW" && status != "PARTIALLY_FILLED"

	baseCfg, err := bncAssetCfg(tradeInfo.baseID)
	if err != nil {
		bnc.log.Errorf("Error getting asset cfg for %d: %v", tradeInfo.baseID, err)
		return
	}

	quoteCfg, err := bncAssetCfg(tradeInfo.quoteID)
	if err != nil {
		bnc.log.Errorf("Error getting asset cfg for %d: %v", tradeInfo.quoteID, err)
		return
	}

	baseFilled := uint64(update.Filled * float64(baseCfg.conversionFactor))
	quoteFilled := uint64(update.QuoteFilled * float64(quoteCfg.conversionFactor))
	var baseFee, quoteFee uint64
	if update.Commission > 0 {
		if update.CommissionAsset == baseCfg.coin {
			baseFee = uint64(update.Commission * float64(baseCfg.conversionFactor))
		} else if update.CommissionAsset == quoteCfg.coin {
			quoteFee = uint64(update.Commission * float64(quoteCfg.conversionFactor))
		} else {
			bnc.log.Errorf("unknown commission asset %q", update.CommissionAsset)
		}
	}

	updater <- &Trade{
		ID:          id,
		Complete:    complete,
		Rate:        tradeInfo.rate,
		Qty:         tradeInfo.qty,
		Market:      tradeInfo.market,
		BaseFilled:  utils.SafeSub(baseFilled, baseFee),
		QuoteFilled: utils.SafeSub(quoteFilled, quoteFee),
		BaseID:      tradeInfo.baseID,
		QuoteID:     tradeInfo.quoteID,
		Sell:        tradeInfo.sell,
	}

	if complete {
		bnc.removeTradeUpdater(id)
	}
}

func (bnc *binance) handleListenKeyExpired(update *bntypes.StreamUpdate) {
	bnc.log.Debugf("Received listenKeyExpired: %+v", update)
	expireTime := time.Unix(update.E/1000, 0)
	bnc.log.Errorf("Listen key %v expired at %v. Attempting to reconnect and get a new one.", update.ListenKey, expireTime)
	bnc.reconnectChan <- struct{}{}
}

func (bnc *binance) handleUserDataStreamUpdate(b []byte) {
	bnc.log.Tracef("Received user data stream update: %s", string(b))

	var msg *bntypes.StreamUpdate
	if err := json.Unmarshal(b, &msg); err != nil {
		bnc.log.Errorf("Error unmarshaling user data stream update: %v\nRaw message: %s", err, string(b))
		return
	}

	switch msg.EventType {
	case "outboundAccountPosition":
		bnc.handleOutboundAccountPosition(msg)
	case "executionReport":
		bnc.handleExecutionReport(msg)
	case "listenKeyExpired":
		bnc.handleListenKeyExpired(msg)
	}
}

func (bnc *binance) getListenID(ctx context.Context) (string, error) {
	var resp *bntypes.DataStreamKey
	if err := bnc.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, false, &resp); err != nil {
		return "", err
	}
	bnc.listenKey.Store(resp.ListenKey)
	return resp.ListenKey, nil
}

func (bnc *binance) getUserDataStream(ctx context.Context) (err error) {
	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err := bnc.getListenID(ctx)
		if err != nil {
			return nil, err
		}

		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL:          bnc.wsURL + "/ws/" + listenKey,
			PingWait:     time.Minute * 4,
			EchoPingData: true,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			Logger:         bnc.log.SubLogger("BNCWS"),
			RawHandler:     bnc.handleUserDataStreamUpdate,
			ConnectHeaders: http.Header{"X-MBX-APIKEY": []string{bnc.apiKey}},
		})
		if err != nil {
			return nil, fmt.Errorf("NewWsConn error: %w", err)
		}

		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, err
		}

		return cm, nil
	}

	cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}

	go func() {
		// A single connection to stream.binance.com is only valid for 24 hours;
		// expect to be disconnected at the 24 hour mark.
		reconnect := time.After(time.Hour * 12)
		// Keepalive a user data stream to prevent a time out. User data streams
		// will close after 60 minutes. It's recommended to send a ping about
		// every 30 minutes.
		keepAlive := time.NewTicker(time.Minute * 30)
		defer keepAlive.Stop()

		retryKeepAlive := make(<-chan time.Time)

		connected := true // do not keep alive on a failed connection

		doReconnect := func() {
			if cm != nil {
				cm.Disconnect()
			}
			cm, err = newConn()
			if err != nil {
				connected = false
				bnc.log.Errorf("Error reconnecting: %v", err)
				reconnect = time.After(time.Second * 30)
			} else {
				connected = true
				reconnect = time.After(time.Hour * 12)
			}
		}

		doKeepAlive := func() {
			if !connected {
				bnc.log.Warn("Cannot keep binance connection alive because we are disconnected. Trying again in 10 seconds.")
				retryKeepAlive = time.After(time.Second * 10)
				return
			}
			q := make(url.Values)
			q.Add("listenKey", bnc.listenKey.Load().(string))
			// Doing a PUT on a listenKey will extend its validity for 60 minutes.
			if err := bnc.request(ctx, http.MethodPut, "/api/v3/userDataStream", q, nil, true, false, nil); err != nil {
				if errHasBnCode(err, bnErrCodeInvalidListenKey) {
					bnc.log.Warnf("Invalid listen key. Reconnecting...")
					doReconnect()
					return
				}
				bnc.log.Errorf("Error sending keep-alive request: %v. Trying again in 10 seconds", err)
				retryKeepAlive = time.After(time.Second * 10)
				return
			}
			bnc.log.Debug("Binance connection keep alive sent successfully.")
		}

		for {
			select {
			case <-bnc.reconnectChan:
				doReconnect()
			case <-reconnect:
				doReconnect()
			case <-retryKeepAlive:
				doKeepAlive()
			case <-keepAlive.C:
				doKeepAlive()
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

var subscribeID uint64

func binanceMktID(baseCfg, quoteCfg *bncAssetConfig) string {
	return baseCfg.coin + quoteCfg.coin
}

func marketDataStreams(mktID string) []string {
	mktID = strings.ToLower(mktID)
	return []string{
		mktID + "@depth",
	}
}

// subUnsubDepth sends a subscription or unsubscription request to the market
// data stream.
// The marketStreamMtx MUST be held when calling this function.
func (bnc *binance) subUnsubMktStreams(subscribe bool, streamIDs []string) error {
	method := "SUBSCRIBE"
	if !subscribe {
		method = "UNSUBSCRIBE"
	}

	req := &bntypes.StreamSubscription{
		Method: method,
		Params: streamIDs,
		ID:     atomic.AddUint64(&subscribeID, 1),
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("error marshaling subscription stream request: %w", err)
	}

	bnc.log.Debugf("Sending %v for %v", method, streamIDs)
	if err := bnc.marketStream.SendRaw(b); err != nil {
		return fmt.Errorf("error sending subscription stream request: %w", err)
	}

	return nil
}

func (bnc *binance) handleMarketDataNote(b []byte) {
	var note *bntypes.BookNote
	if err := json.Unmarshal(b, &note); err != nil {
		bnc.log.Errorf("Error unmarshaling book note: %v", err)
		return
	}
	if note == nil {
		bnc.log.Debugf("Market data update does not parse to a note: %s", string(b))
		return
	}

	if note.Data == nil {
		var waitingResp bool
		bnc.marketStreamRespsMtx.Lock()
		if ch, exists := bnc.marketStreamResps[note.ID]; exists {
			waitingResp = true
			timeout := time.After(time.Second * 5)
			select {
			case ch <- note.Result:
			case <-timeout:
				bnc.log.Errorf("Noone waiting for market stream result id %d", note.ID)
			}
		}
		bnc.marketStreamRespsMtx.Unlock()
		if !waitingResp {
			bnc.log.Debugf("No data in market data update: %s", string(b))
		}
		return
	}

	parts := strings.Split(note.StreamName, "@")
	if len(parts) != 2 || parts[1] != "depth" {
		bnc.log.Errorf("Unknown stream name %q", note.StreamName)
		return
	}

	slug := parts[0] // will be lower-case
	mktID := strings.ToUpper(slug)

	bnc.booksMtx.Lock()
	defer bnc.booksMtx.Unlock()

	book := bnc.books[mktID]
	if book == nil {
		bnc.log.Errorf("No book for stream %q", note.StreamName)
		return
	}

	book.updateQueue <- note.Data
}

func (bnc *binance) getOrderbookSnapshot(ctx context.Context, mktSymbol string) (*bntypes.OrderbookSnapshot, error) {
	v := make(url.Values)
	v.Add("symbol", strings.ToUpper(mktSymbol))
	v.Add("limit", "1000")
	var resp bntypes.OrderbookSnapshot
	return &resp, bnc.getAPI(ctx, "/api/v3/depth", v, false, false, &resp)
}

// subscribeToAdditionalMarketDataStream is called when a new market is
// subscribed to after the market data stream connection has already been
// established.
func (bnc *binance) subscribeToAdditionalMarketDataStream(ctx context.Context, baseID, quoteID uint32) (err error) {
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	mktID := binanceMktID(baseCfg, quoteCfg)

	defer func() {
		bnc.marketStream.UpdateURL(bnc.streamURL())
	}()

	bnc.booksMtx.Lock()
	defer bnc.booksMtx.Unlock()

	book, found := bnc.books[mktID]
	if found {
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return nil
	}

	if err := bnc.subUnsubMktStreams(true, marketDataStreams(mktID)); err != nil {
		return fmt.Errorf("error subscribing to %s: %v", mktID, err)
	}

	getSnapshot := func() (*bntypes.OrderbookSnapshot, error) {
		return bnc.getOrderbookSnapshot(ctx, mktID)
	}
	getAvgPrice := func() (float64, error) {
		return bnc.getAvgPrice(ctx, mktID)
	}
	initialAvgPrice, err := bnc.getAvgPrice(ctx, mktID)
	if err != nil {
		return fmt.Errorf("error getting avg price for %s: %v", mktID, err)
	}
	book = newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, getSnapshot, getAvgPrice, initialAvgPrice, bnc.log)
	bnc.books[mktID] = book
	book.sync(ctx)

	return nil
}

func (bnc *binance) streams() []string {
	bnc.booksMtx.RLock()
	defer bnc.booksMtx.RUnlock()
	streamNames := make([]string, 0, len(bnc.books))
	for mktID := range bnc.books {
		streamNames = append(streamNames, marketDataStreams(mktID)...)
	}
	return streamNames
}

func (bnc *binance) streamURL() string {
	return fmt.Sprintf("%s/stream?streams=%s", bnc.wsURL, strings.Join(bnc.streams(), "/"))
}

// checkSubs will query binance for current market subscriptions and compare
// that to what subscriptions we should have. If there is a discrepancy a
// warning is logged and the market subbed or unsubbed.
func (bnc *binance) checkSubs(ctx context.Context) error {
	bnc.marketStreamMtx.Lock()
	defer bnc.marketStreamMtx.Unlock()
	streams := bnc.streams()
	if len(streams) == 0 {
		return nil
	}

	method := "LIST_SUBSCRIPTIONS"
	id := atomic.AddUint64(&subscribeID, 1)

	resp := make(chan []string, 1)
	bnc.marketStreamRespsMtx.Lock()
	bnc.marketStreamResps[id] = resp
	bnc.marketStreamRespsMtx.Unlock()

	defer func() {
		bnc.marketStreamRespsMtx.Lock()
		delete(bnc.marketStreamResps, id)
		bnc.marketStreamRespsMtx.Unlock()
	}()

	req := &bntypes.StreamSubscription{
		Method: method,
		ID:     id,
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("error marshaling subscription stream request: %w", err)
	}

	bnc.log.Debugf("Sending %v", method)
	if err := bnc.marketStream.SendRaw(b); err != nil {
		return fmt.Errorf("error sending subscription stream request: %w", err)
	}

	timeout := time.After(time.Second * 5)
	var subs []string
	select {
	case subs = <-resp:
	case <-timeout:
		return fmt.Errorf("market stream result id %d did not come.", id)
	case <-ctx.Done():
		return nil
	}

	var sub []string
	unsub := make([]string, len(subs))
	copy(unsub, subs)

out:
	for _, us := range streams {
		for i, them := range unsub {
			if us == them {
				unsub[i] = unsub[len(unsub)-1]
				unsub = unsub[:len(unsub)-1]
				continue out
			}
		}
		sub = append(sub, us)
	}

	if len(sub) > 0 {
		bnc.log.Warnf("Subbing to previously unsubbed streams %v", subs)
		if err := bnc.subUnsubMktStreams(true, sub); err != nil {
			bnc.log.Errorf("Error subscribing to %v: %v", sub, err)
		}
	}

	if len(unsub) > 0 {
		bnc.log.Warnf("Unsubbing from previously subbed streams %v", unsub)
		if err := bnc.subUnsubMktStreams(false, unsub); err != nil {
			bnc.log.Errorf("Error unsubscribing from %v: %v", unsub, err)
		}
	}

	return nil
}

// connectToMarketDataStream is called when the first market is subscribed to.
// It creates a connection to the market data stream and starts a goroutine
// to reconnect every 12 hours, as Binance will close the stream every 24
// hours. Additional markets are subscribed to by calling
// subscribeToAdditionalMarketDataStream.
func (bnc *binance) connectToMarketDataStream(ctx context.Context, baseID, quoteID uint32) error {
	reconnectC := make(chan struct{})
	checkSubsC := make(chan struct{})

	newConnection := func() (*dex.ConnectionMaster, error) {
		// Need to send key but not signature
		connectEventFunc := func(cs comms.ConnectionStatus) {
			if cs != comms.Disconnected && cs != comms.Connected {
				return
			}
			// If disconnected, set all books to unsynced so bots
			// will not place new orders.
			connected := cs == comms.Connected
			bnc.booksMtx.RLock()
			defer bnc.booksMtx.RUnlock()
			for _, b := range bnc.books {
				select {
				case b.connectedChan <- connected:
				default:
				}
			}
		}
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: bnc.streamURL(),
			// Binance Docs: The websocket server will send a ping frame every 3
			// minutes. If the websocket server does not receive a pong frame
			// back from the connection within a 10 minute period, the connection
			// will be disconnected. Unsolicited pong frames are allowed.
			PingWait:     time.Minute * 4,
			EchoPingData: true,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
				select {
				case checkSubsC <- struct{}{}:
				default:
				}
			},
			ConnectEventFunc: connectEventFunc,
			Logger:           bnc.log.SubLogger("BNCBOOK"),
			RawHandler:       bnc.handleMarketDataNote,
		})
		if err != nil {
			return nil, err
		}

		bnc.marketStream = conn
		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return cm, nil
	}

	// Add the initial book to the books map
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return err
	}
	mktID := binanceMktID(baseCfg, quoteCfg)
	bnc.booksMtx.Lock()
	getSnapshot := func() (*bntypes.OrderbookSnapshot, error) {
		return bnc.getOrderbookSnapshot(ctx, mktID)
	}
	getAvgPrice := func() (float64, error) {
		return bnc.getAvgPrice(ctx, mktID)
	}
	initialAvgPrice, err := bnc.getAvgPrice(ctx, mktID)
	if err != nil {
		return fmt.Errorf("error getting avg price for %s: %v", mktID, err)
	}
	book := newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, getSnapshot, getAvgPrice, initialAvgPrice, bnc.log)
	bnc.books[mktID] = book
	bnc.booksMtx.Unlock()

	// Create initial connection to the market data stream
	cm, err := newConnection()
	if err != nil {
		return fmt.Errorf("error connecting to market data stream : %v", err)
	}

	book.sync(ctx)

	// Start a goroutine to reconnect every 12 hours
	go func() {
		reconnect := func() error {
			bnc.marketStreamMtx.Lock()
			defer bnc.marketStreamMtx.Unlock()
			oldCm := cm
			cm, err = newConnection()
			if err != nil {
				return err
			}

			if oldCm != nil {
				oldCm.Disconnect()
			}
			return nil
		}

		checkSubsInterval := time.Minute
		checkSubs := time.After(checkSubsInterval)
		reconnectTimer := time.After(time.Hour * 12)
		for {
			select {
			case <-reconnectC:
				if err := reconnect(); err != nil {
					bnc.log.Errorf("Error reconnecting: %v", err)
					reconnectTimer = time.After(time.Second * 30)
					checkSubs = make(<-chan time.Time)
					continue
				}
				checkSubs = time.After(checkSubsInterval)
			case <-reconnectTimer:
				if err := reconnect(); err != nil {
					bnc.log.Errorf("Error refreshing connection: %v", err)
					reconnectTimer = time.After(time.Second * 30)
					checkSubs = make(<-chan time.Time)
					continue
				}
				reconnectTimer = time.After(time.Hour * 12)
				checkSubs = time.After(checkSubsInterval)
			case <-checkSubs:
				if err := bnc.checkSubs(ctx); err != nil {
					bnc.log.Errorf("Error checking subscriptions: %v", err)
				}
				checkSubs = time.After(checkSubsInterval)
			case <-checkSubsC:
				if err := bnc.checkSubs(ctx); err != nil {
					bnc.log.Errorf("Error checking subscriptions: %v", err)
				}
				checkSubs = time.After(checkSubsInterval)
			case <-ctx.Done():
				bnc.marketStreamMtx.Lock()
				bnc.marketStream = nil
				bnc.marketStreamMtx.Unlock()
				if cm != nil {
					cm.Disconnect()
				}
				return
			}
		}
	}()

	return nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (bnc *binance) UnsubscribeMarket(baseID, quoteID uint32) (err error) {
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return err
	}
	mktID := binanceMktID(baseCfg, quoteCfg)

	bnc.marketStreamMtx.Lock()
	defer bnc.marketStreamMtx.Unlock()

	conn := bnc.marketStream
	if conn == nil {
		return fmt.Errorf("can't unsubscribe. no stream - %p", bnc)
	}

	var unsubscribe bool
	var closer *dex.ConnectionMaster

	bnc.booksMtx.Lock()
	defer func() {
		bnc.booksMtx.Unlock()

		conn.UpdateURL(bnc.streamURL())

		if closer != nil {
			closer.Disconnect()
		}

		if unsubscribe {
			if err := bnc.subUnsubMktStreams(false, marketDataStreams(mktID)); err != nil {
				bnc.log.Errorf("error unsubscribing from market data stream", err)
			}
		}
	}()

	book, found := bnc.books[mktID]
	if !found {
		unsubscribe = true
		return nil
	}

	book.mtx.Lock()
	book.numSubscribers--
	if book.numSubscribers == 0 {
		unsubscribe = true
		delete(bnc.books, mktID)
		closer = book.cm
	}
	book.mtx.Unlock()

	return nil
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP.
func (bnc *binance) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	bnc.marketStreamMtx.Lock()
	defer bnc.marketStreamMtx.Unlock()

	if bnc.marketStream == nil {
		bnc.connectToMarketDataStream(ctx, baseID, quoteID)
	}

	return bnc.subscribeToAdditionalMarketDataStream(ctx, baseID, quoteID)
}

func (bnc *binance) getAvgPrice(ctx context.Context, mktID string) (float64, error) {
	v := make(url.Values)
	v.Add("symbol", mktID)

	var resp bntypes.AvgPriceResponse
	err := bnc.getAPI(ctx, "/api/v3/avgPrice", v, false, false, &resp)
	if err != nil {
		return 0, err
	}

	return resp.Price, nil
}

func (bnc *binance) book(baseID, quoteID uint32) (*binanceOrderBook, error) {
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return nil, err
	}
	mktID := binanceMktID(baseCfg, quoteCfg)

	bnc.booksMtx.RLock()
	book, found := bnc.books[mktID]
	bnc.booksMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no book for market %s", mktID)
	}
	return book, nil
}

func convertSide(side []*obEntry, sell bool, baseFactor, quoteFactor uint64) []*core.MiniOrder {
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

func (bnc *binance) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	book, err := bnc.book(baseID, quoteID)
	if err != nil {
		return nil, nil, err
	}
	bids, asks := book.book.snap()
	bFactor := book.baseConversionFactor
	qFactor := book.quoteConversionFactor
	buys = convertSide(bids, false, bFactor, qFactor)
	sells = convertSide(asks, true, bFactor, qFactor)
	return
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market. SubscribeMarket must be called, and the
// market must be synced before results can be expected.
func (bnc *binance) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (avgPrice, extrema uint64, filled bool, err error) {
	book, err := bnc.book(baseID, quoteID)
	if err != nil {
		return 0, 0, false, err
	}
	return book.vwap(!sell, qty)
}

// InvVWAP returns the inverse volume weighted average price for a certain
// quantity of the quote asset on a market. SubscribeMarket must be called,
// and the market must be synced before results can be expected.
func (bnc *binance) InvVWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	book, err := bnc.book(baseID, quoteID)
	if err != nil {
		return 0, 0, false, err
	}
	return book.invVWAP(!sell, qty)
}

func (bnc *binance) MidGap(baseID, quoteID uint32) uint64 {
	book, err := bnc.book(baseID, quoteID)
	if err != nil {
		bnc.log.Errorf("Error getting order book for (%d, %d): %v", baseID, quoteID, err)
		return 0
	}
	return book.midGap()
}

// TradeStatus returns the current status of a trade.
func (bnc *binance) TradeStatus(ctx context.Context, tradeID string, baseID, quoteID uint32) (*Trade, error) {
	baseAsset, err := bncAssetCfg(baseID)
	if err != nil {
		return nil, err
	}

	quoteAsset, err := bncAssetCfg(quoteID)
	if err != nil {
		return nil, err
	}

	v := make(url.Values)
	v.Add("symbol", baseAsset.coin+quoteAsset.coin)
	v.Add("origClientOrderId", tradeID)

	var resp bntypes.BookedOrder
	err = bnc.getAPI(ctx, "/api/v3/order", v, true, true, &resp)
	if err != nil {
		return nil, err
	}

	market := resp.Type == "MARKET"
	sell := resp.Side == "SELL"

	var qty uint64
	if market && !sell {
		qty = uint64(resp.OrigQuoteQty * float64(quoteAsset.conversionFactor))
	} else {
		qty = uint64(resp.OrigQty * float64(baseAsset.conversionFactor))
	}

	baseFilled := uint64(resp.ExecutedQty * float64(baseAsset.conversionFactor))
	quoteFilled := uint64(resp.CumulativeQuoteQty * float64(quoteAsset.conversionFactor))

	// The GET /api/v3/order response does not include how much commission was paid.
	// We need to calculate it based on the commission rates.
	commissionRates := bnc.commissionRates.Load().(*bntypes.CommissionRates)
	if commissionRates != nil {
		maxCommission := math.Max(commissionRates.Maker, commissionRates.Taker)
		if sell {
			quoteFilled = uint64(float64(quoteFilled) * (1 - maxCommission))
		} else {
			baseFilled = uint64(float64(baseFilled) * (1 - maxCommission))
		}
	} else {
		bnc.log.Errorf("commission rates not set")
	}

	return &Trade{
		ID:          tradeID,
		Sell:        sell,
		Rate:        calc.MessageRateAlt(resp.Price, baseAsset.conversionFactor, quoteAsset.conversionFactor),
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  baseFilled,
		QuoteFilled: quoteFilled,
		Complete:    resp.Status != "NEW" && resp.Status != "PARTIALLY_FILLED",
		Market:      market,
	}, nil
}

func getDEXAssetIDs(coin string, tokenIDs map[string][]uint32) []uint32 {
	dexSymbol := convertBnCoin(coin)

	isRegistered := func(assetID uint32) bool {
		_, err := asset.UnitInfo(assetID)
		return err == nil
	}

	assetIDs := make([]uint32, 0, 1)
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

func assetDisabled(isUS bool, assetID uint32) bool {
	switch dex.BipIDSymbol(assetID) {
	case "zec":
		return !isUS // exchange addresses not yet implemented
	}
	return false
}

// dexMarkets returns all the possible dex markets for this binance market.
// A symbol represents a single market on the CEX, but tokens on the DEX
// have a different assetID for each network they are on, therefore they will
// match multiple markets as defined using assetID.
func binanceMarketToDexMarkets(binanceBaseSymbol, binanceQuoteSymbol string, tokenIDs map[string][]uint32, isUS bool) []*MarketMatch {
	var baseAssetIDs, quoteAssetIDs []uint32

	baseAssetIDs = getDEXAssetIDs(binanceBaseSymbol, tokenIDs)
	if len(baseAssetIDs) == 0 {
		return nil
	}

	quoteAssetIDs = getDEXAssetIDs(binanceQuoteSymbol, tokenIDs)
	if len(quoteAssetIDs) == 0 {
		return nil
	}

	markets := make([]*MarketMatch, 0, len(baseAssetIDs)*len(quoteAssetIDs))
	for _, baseID := range baseAssetIDs {
		for _, quoteID := range quoteAssetIDs {
			if assetDisabled(isUS, baseID) || assetDisabled(isUS, quoteID) {
				continue
			}
			markets = append(markets, &MarketMatch{
				Slug:     binanceBaseSymbol + binanceQuoteSymbol,
				MarketID: dex.BipIDSymbol(baseID) + "_" + dex.BipIDSymbol(quoteID),
				BaseID:   baseID,
				QuoteID:  quoteID,
			})
		}
	}

	return markets
}
