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

	book                  *orderbook
	updateQueue           chan *bntypes.BookUpdate
	mktID                 string
	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger
}

func newBinanceOrderBook(
	baseConversionFactor, quoteConversionFactor uint64,
	mktID string,
	getSnapshot func() (*bntypes.OrderbookSnapshot, error),
	log dex.Logger,
) *binanceOrderBook {
	return &binanceOrderBook{
		book:                  newOrderBook(),
		mktID:                 mktID,
		updateQueue:           make(chan *bntypes.BookUpdate, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
		getSnapshot:           getSnapshot,
	}
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

	desync := func() {
		// clear the sync cache, set the special ID, trigger a book refresh.
		syncMtx.Lock()
		defer syncMtx.Unlock()
		syncCache = make([]*bntypes.BookUpdate, 0)
		acceptedUpdate = false
		if updateID != updateIDUnsynced {
			b.synced.Store(false)
			updateID = updateIDUnsynced
			resyncChan <- struct{}{}
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
					desync()
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
			case <-ctx.Done():
				return
			}

			if syncOrderbook() {
				b.log.Infof("Synced %s orderbook", b.mktID)
				retry = nil
			} else {
				b.log.Infof("Failed  to sync %s orderbook. Trying again in %s", b.mktID, retryFrequency)
				desync() // Clears the syncCache
				retry = time.After(retryFrequency)
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
		return 0, 0, filled, errors.New("orderbook not synced")
	}

	vwap, extrema, filled = b.book.vwap(bids, qty)
	return
}

func (b *binanceOrderBook) midGap() uint64 {
	return b.book.midGap()
}

// TODO: check all symbols
var dexToBinanceSymbol = map[string]string{
	"POLYGON": "MATIC",
	"WETH":    "ETH",
}

var binanceToDexSymbol = make(map[string]string)

func convertBnCoin(coin string) string {
	symbol := strings.ToLower(coin)
	if convertedSymbol, found := binanceToDexSymbol[strings.ToUpper(coin)]; found {
		symbol = strings.ToLower(convertedSymbol)
	}
	return symbol
}

func convertBnNetwork(network string) string {
	symbol := convertBnCoin(network)
	if symbol == "weth" {
		return "eth"
	}
	return symbol
}

// binanceCoinNetworkToDexSymbol takes the coin name and its network name as
// returned by the binance API and returns the DEX symbol.
func binanceCoinNetworkToDexSymbol(coin, network string) string {
	symbol, netSymbol := convertBnCoin(coin), convertBnNetwork(network)
	if symbol == "weth" && netSymbol == "eth" {
		return "eth"
	}
	if symbol == netSymbol {
		return symbol
	}
	return symbol + "." + netSymbol
}

func init() {
	for key, value := range dexToBinanceSymbol {
		binanceToDexSymbol[value] = key
	}
}

func mapDexToBinanceSymbol(symbol string) string {
	if binanceSymbol, found := dexToBinanceSymbol[symbol]; found {
		return binanceSymbol
	}
	return symbol
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
}

func bncAssetCfg(assetID uint32) (*bncAssetConfig, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, err
	}
	coin := mapDexToBinanceSymbol(ui.Conventional.Unit)
	chain := coin
	if tkn := asset.TokenInfo(assetID); tkn != nil {
		pui, err := asset.UnitInfo(tkn.ParentID)
		if err != nil {
			return nil, err
		}
		chain = pui.Conventional.Unit
	}

	return &bncAssetConfig{
		assetID:          assetID,
		symbol:           dex.BipIDSymbol(assetID),
		coin:             coin,
		chain:            mapDexToBinanceSymbol(chain),
		conversionFactor: ui.Conventional.ConversionFactor,
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
	broadcast          func(interface{})
	isUS               bool

	markets atomic.Value // map[string]*binanceMarket
	// tokenIDs maps the token's symbol to the list of bip ids of the token
	// for each chain for which deposits and withdrawals are enabled on
	// binance.
	tokenIDs    atomic.Value // map[string][]uint32, binance coin ID string -> assset IDs
	minWithdraw atomic.Value // map[uint32]map[uint32]uint64

	marketSnapshotMtx sync.Mutex
	marketSnapshot    struct {
		stamp time.Time
		m     map[string]*Market
	}

	balanceMtx sync.RWMutex
	balances   map[uint32]*ExchangeBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	booksMtx sync.RWMutex
	books    map[string]*binanceOrderBook

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*tradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int
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
	}

	bnc.markets.Store(make(map[string]*bntypes.Market))

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
		return fmt.Errorf("error getting balances: %w", err)
	}

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
	minWithdraw := make(map[uint32]uint64)
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
			minWithdraw[assetID] = uint64(math.Round(float64(ui.Conventional.ConversionFactor) * netInfo.WithdrawMin))
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
		return fmt.Errorf("error getting binance coin info: %w", err)
	}

	bnc.readCoins(coins)
	return nil
}

func (bnc *binance) getMarkets(ctx context.Context) (map[string]*bntypes.Market, error) {
	var exchangeInfo bntypes.ExchangeInfo
	err := bnc.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, false, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("error getting markets from Binance: %w", err)
	}

	marketsMap := make(map[string]*bntypes.Market, len(exchangeInfo.Symbols))
	for _, market := range exchangeInfo.Symbols {
		marketsMap[market.Symbol] = market
	}

	bnc.markets.Store(marketsMap)

	return marketsMap, nil
}

// Connect connects to the binance API.
func (bnc *binance) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg := new(sync.WaitGroup)

	if err := bnc.getCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("error getting coin info: %v", err)
	}

	if _, err := bnc.getMarkets(ctx); err != nil {
		return nil, fmt.Errorf("error getting markets: %v", err)
	}

	if err := bnc.setBalances(ctx); err != nil {
		return nil, err
	}

	if err := bnc.getUserDataStream(ctx); err != nil {
		return nil, err
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

// Trade executes a trade on the CEX. subscriptionID takes an ID returned from
// SubscribeTradeUpdates.
func (bnc *binance) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (*Trade, error) {
	side := "BUY"
	if sell {
		side = "SELL"
	}

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

	price := calc.ConventionalRateAlt(rate, baseCfg.conversionFactor, quoteCfg.conversionFactor)
	amt := float64(qty) / float64(baseCfg.conversionFactor)
	tradeID := bnc.generateTradeID()

	v := make(url.Values)
	v.Add("symbol", slug)
	v.Add("side", side)
	v.Add("type", "LIMIT")
	v.Add("timeInForce", "GTC")
	v.Add("newClientOrderId", tradeID)
	v.Add("quantity", strconv.FormatFloat(amt, 'f', market.BaseAssetPrecision, 64))
	v.Add("price", strconv.FormatFloat(price, 'f', market.QuoteAssetPrecision, 64))

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
		qty:       qty,
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

	return &Trade{
		ID:          tradeID,
		Sell:        sell,
		Rate:        rate,
		Qty:         qty,
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  uint64(orderResponse.ExecutedQty * float64(baseCfg.conversionFactor)),
		QuoteFilled: uint64(orderResponse.CumulativeQuoteQty * float64(quoteCfg.conversionFactor)),
		Complete:    orderResponse.Status != "NEW" && orderResponse.Status != "PARTIALLY_FILLED",
	}, err
}

func (bnc *binance) assetPrecision(coin string) (int, error) {
	for _, market := range bnc.markets.Load().(map[string]*bntypes.Market) {
		if market.BaseAsset == coin {
			return market.BaseAssetPrecision, nil
		}
		if market.QuoteAsset == coin {
			return market.QuoteAssetPrecision, nil
		}
	}
	return 0, fmt.Errorf("asset %s not found", coin)
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
func (bnc *binance) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string) (string, error) {
	assetCfg, err := bncAssetCfg(assetID)
	if err != nil {
		return "", fmt.Errorf("error getting symbol data for %d: %w", assetID, err)
	}

	precision, err := bnc.assetPrecision(assetCfg.coin)
	if err != nil {
		return "", fmt.Errorf("error getting precision for %s: %w", assetCfg.coin, err)
	}

	amt := float64(qty) / float64(assetCfg.conversionFactor)
	v := make(url.Values)
	v.Add("coin", assetCfg.coin)
	v.Add("network", assetCfg.chain)
	v.Add("address", address)
	v.Add("amount", strconv.FormatFloat(amt, 'f', precision, 64))

	withdrawResp := struct {
		ID string `json:"id"`
	}{}
	err = bnc.postAPI(ctx, "/sapi/v1/capital/withdraw/apply", nil, v, true, true, &withdrawResp)
	if err != nil {
		return "", err
	}

	return withdrawResp.ID, nil
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

	req, err := bnc.generateRequest(ctx, "DELETE", "/api/v3/order", v, nil, true, true)
	if err != nil {
		return err
	}

	return requestInto(req, &struct{}{})
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

func (bnc *binance) minimumWithdraws(baseID, quoteID uint32) (uint64, uint64) {
	minsI := bnc.minWithdraw.Load()
	if minsI == nil {
		return 0, 0
	}
	mins := minsI.(map[uint32]uint64)
	return mins[baseID], mins[quoteID]
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

func (bnc *binance) getAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodGet, endpoint, query, nil, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return requestInto(req, thing)
}

func (bnc *binance) postAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodPost, endpoint, query, form, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return requestInto(req, thing)
}

func (bnc *binance) generateRequest(ctx context.Context, method, endpoint string, query, form url.Values, key, sign bool) (*http.Request, error) {
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
			return nil, fmt.Errorf("hmax Write error: %w", err)
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
		return nil, fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	return req, nil
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

	updater <- &Trade{
		ID:          id,
		Complete:    complete,
		Rate:        tradeInfo.rate,
		Qty:         tradeInfo.qty,
		BaseFilled:  uint64(update.Filled * float64(baseCfg.conversionFactor)),
		QuoteFilled: uint64(update.QuoteFilled * float64(quoteCfg.conversionFactor)),
		BaseID:      tradeInfo.baseID,
		QuoteID:     tradeInfo.quoteID,
		Sell:        tradeInfo.sell,
	}

	if complete {
		bnc.removeTradeUpdater(id)
	}
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
	}
}

func (bnc *binance) getListenID(ctx context.Context) (string, error) {
	var resp *bntypes.DataStreamKey
	return resp.ListenKey, bnc.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, false, &resp)
}

func (bnc *binance) getUserDataStream(ctx context.Context) (err error) {
	var listenKey string

	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err = bnc.getListenID(ctx)
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
			return nil, fmt.Errorf("user data stream connection error: %v", err)
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

		connected := true // do not keep alive on a failed connection
		for {
			select {
			case <-reconnect:
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
			case <-keepAlive.C:
				if !connected {
					continue
				}
				q := make(url.Values)
				q.Add("listenKey", listenKey)
				// Doing a PUT on a listenKey will extend its validity for 60 minutes.
				req, err := bnc.generateRequest(ctx, http.MethodPut, "/api/v3/userDataStream", q, nil, true, false)
				if err != nil {
					bnc.log.Errorf("Error generating keep-alive request: %v", err)
					continue
				}
				if err := requestInto(req, nil); err != nil {
					bnc.log.Errorf("Error sending keep-alive request: %v", err)
				}
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

func marketDataStreamID(mktID string) string {
	return strings.ToLower(mktID) + "@depth"
}

// subUnsubDepth sends a subscription or unsubscription request to the market
// data stream.
// The marketStreamMtx MUST be held when calling this function.
func (bnc *binance) subUnsubDepth(subscribe bool, mktStreamID string) error {
	method := "SUBSCRIBE"
	if !subscribe {
		method = "UNSUBSCRIBE"
	}

	req := &bntypes.StreamSubscription{
		Method: method,
		Params: []string{mktStreamID},
		ID:     atomic.AddUint64(&subscribeID, 1),
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("error marshaling subscription stream request: %w", err)
	}

	bnc.log.Debugf("Sending %v for market %v", method, mktStreamID)
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
	if note == nil || note.Data == nil {
		bnc.log.Debugf("No data in market data update: %s", string(b))
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
func (bnc *binance) subscribeToAdditionalMarketDataStream(ctx context.Context, baseID, quoteID uint32) error {
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d: %w", baseID, err)
	}

	mktID := binanceMktID(baseCfg, quoteCfg)
	streamID := marketDataStreamID(mktID)

	bnc.booksMtx.Lock()
	defer bnc.booksMtx.Unlock()

	book, found := bnc.books[mktID]
	if found {
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return nil
	}

	if err := bnc.subUnsubDepth(true, streamID); err != nil {
		return fmt.Errorf("error subscribing to %s: %v", streamID, err)
	}

	getSnapshot := func() (*bntypes.OrderbookSnapshot, error) {
		return bnc.getOrderbookSnapshot(ctx, mktID)
	}
	book = newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, getSnapshot, bnc.log)
	bnc.books[mktID] = book
	book.sync(ctx)

	return nil
}

// connectToMarketDataStream is called when the first market is subscribed to.
// It creates a connection to the market data stream and starts a goroutine
// to reconnect every 12 hours, as Binance will close the stream every 24
// hours. Additional markets are subscribed to by calling
// subscribeToAdditionalMarketDataStream.
func (bnc *binance) connectToMarketDataStream(ctx context.Context, baseID, quoteID uint32) error {
	newConnection := func() (comms.WsConn, *dex.ConnectionMaster, error) {
		bnc.booksMtx.Lock()
		streamNames := make([]string, 0, len(bnc.books))
		for mktID := range bnc.books {
			streamNames = append(streamNames, marketDataStreamID(mktID))
		}
		bnc.booksMtx.Unlock()

		addr := fmt.Sprintf("%s/stream?streams=%s", bnc.wsURL, strings.Join(streamNames, "/"))
		// Need to send key but not signature
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: addr,
			// Binance Docs: The websocket server will send a ping frame every 3
			// minutes. If the websocket server does not receive a pong frame
			// back from the connection within a 10 minute period, the connection
			// will be disconnected. Unsolicited pong frames are allowed.
			PingWait:     time.Minute * 4,
			EchoPingData: true,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			ConnectEventFunc: func(cs comms.ConnectionStatus) {},
			Logger:           bnc.log.SubLogger("BNCBOOK"),
			RawHandler:       bnc.handleMarketDataNote,
		})
		if err != nil {
			return nil, nil, err
		}

		bnc.marketStream = conn
		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return conn, cm, nil
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
	book := newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, getSnapshot, bnc.log)
	bnc.books[mktID] = book
	bnc.booksMtx.Unlock()

	// Create initial connection to the market data stream
	conn, cm, err := newConnection()
	if err != nil {
		return fmt.Errorf("error connecting to market data stream : %v", err)
	}

	bnc.marketStream = conn

	book.sync(ctx)

	// Start a goroutine to reconnect every 12 hours
	go func() {
		reconnect := func() error {
			bnc.marketStreamMtx.Lock()
			defer bnc.marketStreamMtx.Unlock()

			oldCm := cm
			conn, cm, err = newConnection()
			if err != nil {
				return err
			}

			if oldCm != nil {
				oldCm.Disconnect()
			}

			bnc.marketStream = conn
			return nil
		}

		reconnectTimer := time.After(time.Hour * 12)
		for {
			select {
			case <-reconnectTimer:
				err = reconnect()
				if err != nil {
					bnc.log.Errorf("Error reconnecting: %v", err)
					reconnectTimer = time.After(time.Second * 30)
					continue
				}
				reconnectTimer = time.After(time.Hour * 12)
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
	streamID := marketDataStreamID(mktID)

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

		if closer != nil {
			closer.Disconnect()
		}

		if unsubscribe {
			if err := bnc.subUnsubDepth(false, streamID); err != nil {
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

func (bnc *binance) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	book, err := bnc.book(baseID, quoteID)
	if err != nil {
		return nil, nil, err
	}
	bids, asks := book.book.snap()
	bFactor := float64(book.baseConversionFactor)
	convertSide := func(side []*obEntry, sell bool) []*core.MiniOrder {
		ords := make([]*core.MiniOrder, len(side))
		for i, e := range side {
			ords[i] = &core.MiniOrder{
				Qty:       float64(e.qty) / bFactor,
				QtyAtomic: e.qty,
				Rate:      calc.ConventionalRateAlt(e.rate, book.baseConversionFactor, book.quoteConversionFactor),
				MsgRate:   e.rate,
				Sell:      sell,
			}
		}
		return ords
	}
	buys = convertSide(bids, false)
	sells = convertSide(asks, true)
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

	return &Trade{
		ID:          tradeID,
		Sell:        resp.Side == "SELL",
		Rate:        calc.MessageRateAlt(resp.Price, baseAsset.conversionFactor, quoteAsset.conversionFactor),
		Qty:         uint64(resp.OrigQty * float64(baseAsset.conversionFactor)),
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  uint64(resp.ExecutedQty * float64(baseAsset.conversionFactor)),
		QuoteFilled: uint64(resp.CumulativeQuoteQty * float64(quoteAsset.conversionFactor)),
		Complete:    resp.Status != "NEW" && resp.Status != "PARTIALLY_FILLED",
	}, nil
}

func getDEXAssetIDs(coin string, tokenIDs map[string][]uint32) []uint32 {
	dexSymbol := convertBnCoin(coin)

	// Binance does not differentiate between eth and weth like we do.
	dexNonTokenSymbol := dexSymbol
	if dexNonTokenSymbol == "weth" {
		dexNonTokenSymbol = "eth"
	}

	isRegistered := func(assetID uint32) bool {
		_, err := asset.UnitInfo(assetID)
		return err == nil
	}

	assetIDs := make([]uint32, 0, 1)
	if assetID, found := dex.BipSymbolID(dexNonTokenSymbol); found {
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
	case "firo", "zec":
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

func requestInto(req *http.Request, thing interface{}) error {
	// bnc.log.Tracef("Sending request: %+v", req)
	return dexnet.Do(req, thing, dexnet.WithSizeLimit(1<<24))
}
