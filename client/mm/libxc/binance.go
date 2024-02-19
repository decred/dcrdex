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
	"io"
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
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
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
	fakeBinanceURL = "http://localhost:37346"
)

// binanceOrderBook manages an orderbook for a single market. It keeps
// the orderbook synced and allows querying of vwap.
type binanceOrderBook struct {
	mtx            sync.RWMutex
	synced         bool
	numSubscribers uint32

	book                  *orderbook
	updateQueue           chan *binanceBookUpdate
	mktID                 string
	baseConversionFactor  uint64
	quoteConversionFactor uint64
	log                   dex.Logger
}

func newBinanceOrderBook(baseConversionFactor, quoteConversionFactor uint64, mktID string, log dex.Logger) *binanceOrderBook {
	return &binanceOrderBook{
		book:                  newOrderBook(),
		mktID:                 mktID,
		updateQueue:           make(chan *binanceBookUpdate, 1024),
		numSubscribers:        1,
		baseConversionFactor:  baseConversionFactor,
		quoteConversionFactor: quoteConversionFactor,
		log:                   log,
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
func (b *binanceOrderBook) sync(ctx context.Context, getSnapshot func() (*binanceOrderbookSnapshot, error)) {
	syncOrderbook := func(minUpdateID uint64) (uint64, bool) {
		b.log.Debugf("Syncing %s orderbook. First update ID: %d", b.mktID, minUpdateID)

		const maxTries = 5
		for i := 0; i < maxTries; i++ {
			if i > 0 {
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
					return 0, false
				}
			}

			snapshot, err := getSnapshot()
			if err != nil {
				b.log.Errorf("Error getting orderbook snapshot: %v", err)
				continue
			}
			if snapshot.LastUpdateID < minUpdateID {
				b.log.Infof("Snapshot last update ID %d is less than first update ID %d. Getting new snapshot...", snapshot.LastUpdateID, minUpdateID)
				continue
			}

			bids, asks, err := b.convertBinanceBook(snapshot.Bids, snapshot.Asks)
			if err != nil {
				b.log.Errorf("Error parsing binance book: %v", err)
				return 0, false
			}

			b.log.Debugf("Got %s orderbook snapshot with update ID %d", b.mktID, snapshot.LastUpdateID)

			b.book.clear()
			b.book.update(bids, asks)
			return snapshot.LastUpdateID, true
		}

		return 0, false
	}

	setSynced := func(synced bool) {
		b.mtx.Lock()
		b.synced = synced
		b.mtx.Unlock()
	}

	var firstUpdate *binanceBookUpdate
	select {
	case <-ctx.Done():
		return
	case firstUpdate = <-b.updateQueue:
	}

	latestUpdate, success := syncOrderbook(firstUpdate.FirstUpdateID)
	if !success {
		b.log.Errorf("Failed to sync %s orderbook", b.mktID)
		return
	}

	setSynced(true)

	b.log.Infof("Successfully synced %s orderbook", b.mktID)

	for {
		select {
		case update := <-b.updateQueue:
			if update.LastUpdateID <= latestUpdate {
				continue
			}

			if update.FirstUpdateID > latestUpdate+1 {
				b.log.Warnf("Missed %d updates for %s orderbook. Re-syncing...", update.FirstUpdateID-latestUpdate, b.mktID)

				setSynced(false)

				latestUpdate, success = syncOrderbook(update.LastUpdateID)
				if !success {
					b.log.Errorf("Failed to re-sync %s orderbook.", b.mktID)
					return
				}

				setSynced(true)
				continue
			}

			bids, asks, err := b.convertBinanceBook(update.Bids, update.Asks)
			if err != nil {
				b.log.Errorf("Error parsing binance book: %v", err)
				continue
			}

			b.book.update(bids, asks)
			latestUpdate = update.LastUpdateID
		case <-ctx.Done():
			return
		}
	}
}

// vwap returns the volume weighted average price for a certain quantity of the
// base asset. It returns an error if the orderbook is not synced.
func (b *binanceOrderBook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()

	if !b.synced {
		return 0, 0, filled, errors.New("orderbook not synced")
	}

	vwap, extrema, filled = b.book.vwap(bids, qty)
	return
}

// TODO: check all symbols
var dexToBinanceSymbol = map[string]string{
	"POLYGON": "MATIC",
	"WETH":    "ETH",
}

var binanceToDexSymbol = make(map[string]string)

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

// binanceCoinNetworkToDexSymbol takes the coin name and its network name as
// returned by the binance API and returns the DEX symbol.
func binanceCoinNetworkToDexSymbol(coin, network string) string {
	if coin == "ETH" && network == "ETH" {
		return "eth"
	}

	var dexSymbol, dexNetwork string
	if symbol, found := binanceToDexSymbol[coin]; found {
		dexSymbol = strings.ToLower(symbol)
	} else {
		dexSymbol = strings.ToLower(coin)
	}

	if symbol, found := binanceToDexSymbol[network]; found && network != "ETH" {
		dexNetwork = strings.ToLower(symbol)
	} else {
		dexNetwork = strings.ToLower(network)
	}

	if dexSymbol == dexNetwork {
		return dexSymbol
	}

	return fmt.Sprintf("%s.%s", dexSymbol, dexNetwork)
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
	symbol := dex.BipIDSymbol(assetID)
	if symbol == "" {
		return nil, fmt.Errorf("no symbol found for %d", assetID)
	}

	coin := strings.ToUpper(symbol)
	chain := strings.ToUpper(symbol)
	if parts := strings.Split(symbol, "."); len(parts) > 1 {
		coin = strings.ToUpper(parts[0])
		chain = strings.ToUpper(parts[1])
	}

	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, fmt.Errorf("no unit info found for %d", assetID)
	}

	return &bncAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             mapDexToBinanceSymbol(coin),
		chain:            mapDexToBinanceSymbol(chain),
		conversionFactor: ui.Conventional.ConversionFactor,
	}, nil
}

func bncAssetCfgs(baseAsset, quoteAsset uint32) (*bncAssetConfig, *bncAssetConfig, error) {
	baseCfg, err := bncAssetCfg(baseAsset)
	if err != nil {
		return nil, nil, err
	}

	quoteCfg, err := bncAssetCfg(quoteAsset)
	if err != nil {
		return nil, nil, err
	}

	return baseCfg, quoteCfg, nil
}

// bncBalance is the balance of an asset in conventional units. This must be
// converted before returning.
type bncBalance struct {
	available float64
	locked    float64
}

type tradeInfo struct {
	updaterID int
	baseID    uint32
	quoteID   uint32
	sell      bool
}

type binance struct {
	log                dex.Logger
	url                string
	wsURL              string
	apiKey             string
	secretKey          string
	knownAssets        map[uint32]bool
	net                dex.Network
	tradeIDNonce       atomic.Uint32
	tradeIDNoncePrefix dex.Bytes

	markets atomic.Value // map[string]*binanceMarket
	// tokenIDs maps the token's symbol to the list of bip ids of the token
	// for each chain for which deposits and withdrawals are enabled on
	// binance.
	tokenIDs atomic.Value // map[string][]uint32

	balanceMtx sync.RWMutex
	balances   map[uint32]*bncBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	booksMtx sync.RWMutex
	books    map[string]*binanceOrderBook

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*tradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	cexUpdatersMtx sync.RWMutex
	cexUpdaters    map[chan interface{}]struct{}
}

var _ CEX = (*binance)(nil)

func newBinance(apiKey, secretKey string, log dex.Logger, net dex.Network, binanceUS bool) *binance {
	url, wsURL := httpURL, websocketURL
	if binanceUS {
		url, wsURL = usHttpURL, usWebsocketURL
	}
	if net == dex.Testnet || net == dex.Simnet {
		url, wsURL = testnetHttpURL, testnetWebsocketURL
	}

	registeredAssets := asset.Assets()
	knownAssets := make(map[uint32]bool, len(registeredAssets))
	for _, a := range registeredAssets {
		knownAssets[a.ID] = true
	}

	bnc := &binance{
		log:                log,
		url:                url,
		wsURL:              wsURL,
		apiKey:             apiKey,
		secretKey:          secretKey,
		knownAssets:        knownAssets,
		balances:           make(map[uint32]*bncBalance),
		books:              make(map[string]*binanceOrderBook),
		net:                net,
		tradeInfo:          make(map[string]*tradeInfo),
		tradeUpdaters:      make(map[int]chan *Trade),
		cexUpdaters:        make(map[chan interface{}]struct{}, 0),
		tradeIDNoncePrefix: encode.RandomBytes(10),
	}

	bnc.markets.Store(make(map[string]*binanceMarket))
	bnc.tokenIDs.Store(make(map[string][]uint32))

	return bnc
}

// setBalances queries binance for the user's balances and stores them in the
// balances map.
func (bnc *binance) setBalances(ctx context.Context) error {
	var resp struct {
		Balances []struct {
			Asset  string  `json:"asset"`
			Free   float64 `json:"free,string"`
			Locked float64 `json:"locked,string"`
		} `json:"balances"`
	}
	err := bnc.getAPI(ctx, "/api/v3/account", nil, true, true, &resp)
	if err != nil {
		return err
	}

	tokenIDs := bnc.tokenIDs.Load().(map[string][]uint32)

	bnc.balanceMtx.Lock()
	defer bnc.balanceMtx.Unlock()

	for _, bal := range resp.Balances {
		for _, assetID := range getDEXAssetIDs(bal.Asset, tokenIDs) {
			updatedBalance := &bncBalance{
				available: bal.Free,
				locked:    bal.Locked,
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

// setTokenIDs stores the token IDs for which deposits and withdrawals are
// enabled on binance.
func (bnc *binance) setTokenIDs(coins []*binanceCoinInfo) {
	tokenIDs := make(map[string][]uint32)
	for _, nfo := range coins {
		tokenSymbol := strings.ToLower(nfo.Coin)
		if convertedSymbol, found := binanceToDexSymbol[nfo.Coin]; found {
			tokenSymbol = strings.ToLower(convertedSymbol)
		}
		for _, netInfo := range nfo.NetworkList {
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				bnc.log.Infof("Skipping %s network %s because deposits and/or withdraws are not enabled.", netInfo.Coin, netInfo.Network)
				continue
			}
			dexSymbol := binanceCoinNetworkToDexSymbol(nfo.Coin, netInfo.Network)
			if !strings.Contains(dexSymbol, ".") {
				continue
			}
			if bipID, supported := dex.BipSymbolID(dexSymbol); supported {
				tokenIDs[tokenSymbol] = append(tokenIDs[tokenSymbol], bipID)
			}
		}
	}
	bnc.tokenIDs.Store(tokenIDs)
}

// getCoinInfo retrieves binance configs then updates the user balances and
// the tokenIDs.
func (bnc *binance) getCoinInfo(ctx context.Context) error {
	coins := make([]*binanceCoinInfo, 0)
	err := bnc.getAPI(ctx, "/sapi/v1/capital/config/getall", nil, true, true, &coins)
	if err != nil {
		return fmt.Errorf("error getting binance coin info: %w", err)
	}

	bnc.setTokenIDs(coins)
	return nil
}

func (bnc *binance) getMarkets(ctx context.Context) (map[string]*binanceMarket, error) {
	var exchangeInfo struct {
		Timezone   string `json:"timezone"`
		ServerTime int64  `json:"serverTime"`
		RateLimits []struct {
			RateLimitType string `json:"rateLimitType"`
			Interval      string `json:"interval"`
			IntervalNum   int64  `json:"intervalNum"`
			Limit         int64  `json:"limit"`
		} `json:"rateLimits"`
		Symbols []*binanceMarket `json:"symbols"`
	}
	err := bnc.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, false, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("error getting markets from Binance: %w", err)
	}

	marketsMap := make(map[string]*binanceMarket, len(exchangeInfo.Symbols))
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
					bnc.sendCexUpdateNotes()
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
					bnc.sendCexUpdateNotes()
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return wg, nil
}

// SubscribeCEXUpdates returns a channel which sends an empty struct when
// the balance of an asset on the CEX has been updated.
func (bnc *binance) SubscribeCEXUpdates() (<-chan interface{}, func()) {
	updater := make(chan interface{}, 128)
	bnc.cexUpdatersMtx.Lock()
	bnc.cexUpdaters[updater] = struct{}{}
	bnc.cexUpdatersMtx.Unlock()

	unsubscribe := func() {
		bnc.cexUpdatersMtx.Lock()
		delete(bnc.cexUpdaters, updater)
		bnc.cexUpdatersMtx.Unlock()
	}

	return updater, unsubscribe
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

	return &ExchangeBalance{
		Available: uint64(math.Floor(bal.available * float64(assetConfig.conversionFactor))),
		Locked:    uint64(math.Floor(bal.locked * float64(assetConfig.conversionFactor))),
	}, nil
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

	marketsMap := bnc.markets.Load().(map[string]*binanceMarket)
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

	var orderResponse struct {
		Symbol             string  `json:"symbol"`
		Price              float64 `json:"price,string"`
		OrigQty            float64 `json:"origQty,string"`
		OrigQuoteQty       float64 `json:"origQuoteOrderQty,string"`
		ExecutedQty        float64 `json:"executedQty,string"`
		CumulativeQuoteQty float64 `json:"cummulativeQuoteQty,string"`
		Status             string  `json:"status"`
	}
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
	for _, market := range bnc.markets.Load().(map[string]*binanceMarket) {
		if market.BaseAsset == coin {
			return market.BaseAssetPrecision, nil
		}
		if market.QuoteAsset == coin {
			return market.QuoteAssetPrecision, nil
		}
	}
	return 0, fmt.Errorf("asset %s not found", coin)
}

// Withdraw withdraws funds from the CEX to a certain address. onComplete
// is called with the actual amount withdrawn (amt - fees) and the
// transaction ID of the withdrawal.
func (bnc *binance) Withdraw(ctx context.Context, assetID uint32, qty uint64, address string, onComplete func(uint64, string)) error {
	assetCfg, err := bncAssetCfg(assetID)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %d: %w", assetID, err)
	}

	precision, err := bnc.assetPrecision(assetCfg.coin)
	if err != nil {
		return fmt.Errorf("error getting precision for %s: %w", assetCfg.coin, err)
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
		return err
	}

	go func() {
		getWithdrawalStatus := func() (complete bool, amount uint64, txID string) {
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
				bnc.log.Errorf("Error getting withdrawal status: %v", err)
				return false, 0, ""
			}

			var status *withdrawalHistoryStatus
			for _, s := range withdrawHistoryResponse {
				if s.ID == withdrawResp.ID {
					status = s
					break
				}
			}
			if status == nil {
				bnc.log.Errorf("Withdrawal status not found for %s", withdrawResp.ID)
				return false, 0, ""
			}

			bnc.log.Tracef("Withdrawal status: %+v", status)

			amt := status.Amount * float64(assetCfg.conversionFactor)
			return status.Status == 6, uint64(amt), status.TxID
		}

		for {
			ticker := time.NewTicker(time.Second * 20)
			defer ticker.Stop()

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if complete, amt, txID := getWithdrawalStatus(); complete {
					onComplete(amt, txID)
					return
				}
			}
		}
	}()

	return nil
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
func (bnc *binance) ConfirmDeposit(ctx context.Context, txID string, onConfirm func(bool, uint64)) {
	const (
		pendingStatus            = 0
		successStatus            = 1
		creditedStatus           = 6
		wrongDepositStatus       = 7
		waitingUserConfirmStatus = 8
	)

	checkDepositStatus := func() (success, done bool, amt uint64) {
		var resp []struct {
			Amount  float64 `json:"amount,string"`
			Coin    string  `json:"coin"`
			Network string  `json:"network"`
			Status  int     `json:"status"`
			TxID    string  `json:"txId"`
		}
		err := bnc.getAPI(ctx, "/sapi/v1/capital/deposit/hisrec", nil, true, true, &resp)
		if err != nil {
			bnc.log.Errorf("error getting deposit status: %v", err)
			return false, false, 0
		}

		for _, status := range resp {
			if status.TxID == txID {
				switch status.Status {
				case successStatus, creditedStatus:
					dexSymbol := binanceCoinNetworkToDexSymbol(status.Coin, status.Network)
					assetID, found := dex.BipSymbolID(dexSymbol)
					if !found {
						bnc.log.Errorf("Failed to find DEX asset ID for Coin: %s, Network: %s", status.Coin, status.Network)
						return false, true, 0
					}
					ui, err := asset.UnitInfo(assetID)
					if err != nil {
						bnc.log.Errorf("Failed to find unit info for asset ID %d", assetID)
						return false, true, 0
					}
					amount := uint64(status.Amount * float64(ui.Conventional.ConversionFactor))
					return true, true, amount
				case pendingStatus:
					return false, false, 0
				case waitingUserConfirmStatus:
					// This shouldn't ever happen.
					bnc.log.Errorf("Deposit %s to binance requires user confirmation!")
					return false, true, 0
				case wrongDepositStatus:
					return false, true, 0
				default:
					bnc.log.Errorf("Deposit %s to binance has an unknown status %d", status.Status)
				}
			}
		}

		return false, false, 0
	}

	go func() {
		for {
			ticker := time.NewTicker(time.Second * 20)
			defer ticker.Stop()

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				success, done, amt := checkDepositStatus()
				if done {
					onConfirm(success, amt)
					return
				}
			}
		}
	}()
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

	return bnc.requestInto(req, &struct{}{})
}

func (bnc *binance) Balances() (map[uint32]*ExchangeBalance, error) {
	bnc.balanceMtx.RLock()
	defer bnc.balanceMtx.RUnlock()

	balances := make(map[uint32]*ExchangeBalance)

	for assetID, bal := range bnc.balances {
		assetConfig, err := bncAssetCfg(assetID)
		if err != nil {
			continue
		}

		balances[assetConfig.assetID] = &ExchangeBalance{
			Available: uint64(bal.available * float64(assetConfig.conversionFactor)),
			Locked:    uint64(bal.locked * float64(assetConfig.conversionFactor)),
		}
	}

	return balances, nil
}

func (bnc *binance) Markets(ctx context.Context) (_ []*Market, err error) {
	bnMarkets := bnc.markets.Load().(map[string]*binanceMarket)
	if len(bnMarkets) == 0 {
		bnMarkets, err = bnc.getMarkets(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting markets: %v", err)
		}
	}
	markets := make([]*Market, 0, len(bnMarkets))
	tokenIDs := bnc.tokenIDs.Load().(map[string][]uint32)
	for _, mkt := range bnMarkets {
		dexMarkets := binanceMarketToDexMarkets(mkt.BaseAsset, mkt.QuoteAsset, tokenIDs)
		markets = append(markets, dexMarkets...)
	}
	return markets, nil
}

func (bnc *binance) getAPI(ctx context.Context, endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodGet, endpoint, query, nil, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *binance) postAPI(ctx context.Context, endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(ctx, http.MethodPost, endpoint, query, form, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *binance) requestInto(req *http.Request, thing interface{}) error {
	bnc.log.Tracef("Sending request: %+v", req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("httpClient.Do error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error (%d) %s", resp.StatusCode, resp.Status)
	}

	if thing == nil {
		return nil
	}
	// TODO: use buffered reader
	reader := io.LimitReader(resp.Body, 1<<22)
	r, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(r, thing); err != nil {
		return fmt.Errorf("json Decode error: %w", err)
	}
	return nil
}

func (bnc *binance) generateRequest(ctx context.Context, method, endpoint string, query, form url.Values, key, sign bool) (*http.Request, error) {
	var fullURL string
	if (bnc.net == dex.Simnet || bnc.net == dex.Testnet) && strings.Contains(endpoint, "sapi") {
		fullURL = fakeBinanceURL + endpoint
	} else {
		fullURL = bnc.url + endpoint
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

func (bnc *binance) sendCexUpdateNotes() {
	bnc.cexUpdatersMtx.RLock()
	defer bnc.cexUpdatersMtx.RUnlock()
	for updater := range bnc.cexUpdaters {
		updater <- struct{}{}
	}
}

func (bnc *binance) handleOutboundAccountPosition(update *binanceStreamUpdate) {
	bnc.log.Debugf("Received outboundAccountPosition: %+v", update)
	for _, bal := range update.Balances {
		bnc.log.Debugf("outboundAccountPosition balance: %+v", bal)
	}

	supportedTokens := bnc.tokenIDs.Load().(map[string][]uint32)

	processSymbol := func(symbol string, bal *binanceWSBalance) {
		for _, assetID := range getDEXAssetIDs(symbol, supportedTokens) {
			bnc.balances[assetID] = &bncBalance{
				available: bal.Free,
				locked:    bal.Locked,
			}
		}
	}

	bnc.balanceMtx.Lock()
	for _, bal := range update.Balances {
		symbol := strings.ToLower(bal.Asset)
		processSymbol(symbol, bal)
		if symbol == "eth" {
			processSymbol("weth", bal)
		}
	}
	bnc.balanceMtx.Unlock()

	bnc.sendCexUpdateNotes()
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

func (bnc *binance) handleExecutionReport(update *binanceStreamUpdate) {
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

	var msg *binanceStreamUpdate
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
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	return resp.ListenKey, bnc.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, false, &resp)
}

func (bnc *binance) getUserDataStream(ctx context.Context) (err error) {
	var listenKey string

	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err = bnc.getListenID(ctx)
		if err != nil {
			return nil, err
		}

		hdr := make(http.Header)
		hdr.Set("X-MBX-APIKEY", bnc.apiKey)
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL:      bnc.wsURL + "/ws/" + listenKey,
			PingWait: time.Minute * 4,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			Logger:         bnc.log.SubLogger("BNCWS"),
			RawHandler:     bnc.handleUserDataStreamUpdate,
			ConnectHeaders: hdr,
		})
		if err != nil {
			return nil, err
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
				if err := bnc.requestInto(req, nil); err != nil {
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

	req := &struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
		ID     uint64   `json:"id"`
	}{
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
	var note *binanceBookNote
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

	bnc.log.Infof("Received book update for %q", mktID)

	bnc.booksMtx.Lock()
	defer bnc.booksMtx.Unlock()

	book := bnc.books[mktID]
	if book == nil {
		bnc.log.Errorf("No book for stream %q", note.StreamName)
		return
	}
	book.updateQueue <- note.Data
}

func (bnc *binance) getOrderbookSnapshot(ctx context.Context, mktSymbol string) (*binanceOrderbookSnapshot, error) {
	v := make(url.Values)
	v.Add("symbol", strings.ToUpper(mktSymbol))
	v.Add("limit", "1000")
	resp := &binanceOrderbookSnapshot{}
	return resp, bnc.getAPI(ctx, "/api/v3/depth", v, false, false, resp)
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

	bnc.books[mktID] = newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, bnc.log)
	book = bnc.books[mktID]

	if err := bnc.subUnsubDepth(true, streamID); err != nil {
		return fmt.Errorf("error subscribing to %s: %v", streamID, err)
	}

	go book.sync(ctx, func() (*binanceOrderbookSnapshot, error) {
		return bnc.getOrderbookSnapshot(ctx, mktID)
	})

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
			PingWait: time.Minute * 4,
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
	book := newBinanceOrderBook(baseCfg.conversionFactor, quoteCfg.conversionFactor, mktID, bnc.log)
	bnc.books[mktID] = book
	bnc.booksMtx.Unlock()

	// Create initial connection to the market data stream
	conn, cm, err := newConnection()
	if err != nil {
		return fmt.Errorf("error connecting to market data stream : %v", err)
	}

	bnc.marketStream = conn

	go book.sync(ctx, func() (*binanceOrderbookSnapshot, error) {
		return bnc.getOrderbookSnapshot(ctx, mktID)
	})

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

	bnc.booksMtx.Lock()
	defer func() {
		bnc.booksMtx.Unlock()

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
	defer book.mtx.Unlock()
	book.numSubscribers--
	if book.numSubscribers == 0 {
		unsubscribe = true
		delete(bnc.books, mktID)
	}

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

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market. SubscribeMarket must be called, and the
// market must be synced before results can be expected.
func (bnc *binance) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (avgPrice, extrema uint64, filled bool, err error) {
	baseCfg, quoteCfg, err := bncAssetCfgs(baseID, quoteID)
	if err != nil {
		return 0, 0, false, err
	}
	mktID := binanceMktID(baseCfg, quoteCfg)

	bnc.booksMtx.RLock()
	book, found := bnc.books[mktID]
	bnc.booksMtx.RUnlock()
	if !found {
		return 0, 0, false, fmt.Errorf("no book for market %s", mktID)
	}

	return book.vwap(!sell, qty)
}

func (bnc *binance) TradeStatus(ctx context.Context, id string, baseID, quoteID uint32) {
	var resp struct {
		Symbol             string `json:"symbol"`
		OrderID            int64  `json:"orderId"`
		ClientOrderID      string `json:"clientOrderId"`
		Price              string `json:"price"`
		OrigQty            string `json:"origQty"`
		ExecutedQty        string `json:"executedQty"`
		CumulativeQuoteQty string `json:"cumulativeQuoteQty"`
		Status             string `json:"status"`
		TimeInForce        string `json:"timeInForce"`
	}

	baseAsset, err := bncAssetCfg(baseID)
	if err != nil {
		bnc.log.Errorf("Error getting asset cfg for %d: %v", baseID, err)
		return
	}

	quoteAsset, err := bncAssetCfg(quoteID)
	if err != nil {
		bnc.log.Errorf("Error getting asset cfg for %d: %v", quoteID, err)
		return
	}

	v := make(url.Values)
	v.Add("symbol", baseAsset.coin+quoteAsset.coin)
	v.Add("origClientOrderId", id)

	err = bnc.getAPI(ctx, "/api/v3/order", v, true, true, &resp)
	if err != nil {
		bnc.log.Errorf("Error getting trade status: %v", err)
		return
	}

	bnc.log.Infof("Trade status: %+v", resp)
}

func getDEXAssetIDs(binanceSymbol string, tokenIDs map[string][]uint32) []uint32 {
	dexSymbol := strings.ToLower(binanceSymbol)
	if convertedDEXSymbol, found := binanceToDexSymbol[binanceSymbol]; found {
		dexSymbol = strings.ToLower(convertedDEXSymbol)
	}

	// Binance does not differentiate between eth and weth like we do.
	dexNonTokenSymbol := dexSymbol
	if dexNonTokenSymbol == "weth" {
		dexNonTokenSymbol = "eth"
	}

	assetIDs := make([]uint32, 0, 1)
	if assetID, found := dex.BipSymbolID(dexNonTokenSymbol); found {
		assetIDs = append(assetIDs, assetID)
	}

	if tokenIDs, found := tokenIDs[dexSymbol]; found {
		assetIDs = append(assetIDs, tokenIDs...)
	}

	return assetIDs
}

// dexMarkets returns all the possible dex markets for this binance market.
// A symbol represents a single market on the CEX, but tokens on the DEX
// have a different assetID for each network they are on, therefore they will
// match multiple markets as defined using assetID.
func binanceMarketToDexMarkets(binanceBaseSymbol, binanceQuoteSymbol string, tokenIDs map[string][]uint32) []*Market {
	var baseAssetIDs, quoteAssetIDs []uint32

	baseAssetIDs = getDEXAssetIDs(binanceBaseSymbol, tokenIDs)
	if len(baseAssetIDs) == 0 {
		return nil
	}

	quoteAssetIDs = getDEXAssetIDs(binanceQuoteSymbol, tokenIDs)
	if len(quoteAssetIDs) == 0 {
		return nil
	}

	markets := make([]*Market, 0, len(baseAssetIDs)*len(quoteAssetIDs))
	for _, baseID := range baseAssetIDs {
		for _, quoteID := range quoteAssetIDs {
			markets = append(markets, &Market{
				BaseID:  baseID,
				QuoteID: quoteID,
			})
		}
	}

	return markets
}
