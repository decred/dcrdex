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

type bookBin struct {
	Price float64
	Qty   float64
}

type bncBook struct {
	bids           []*bookBin
	asks           []*bookBin
	latestUpdate   int64
	numSubscribers uint32
}

func newBNCBook() *bncBook {
	return &bncBook{
		bids:           make([]*bookBin, 0),
		asks:           make([]*bookBin, 0),
		numSubscribers: 1,
	}
}

type bncAssetConfig struct {
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
		symbol:           symbol,
		coin:             mapDexToBinanceSymbol(coin),
		chain:            mapDexToBinanceSymbol(chain),
		conversionFactor: ui.Conventional.ConversionFactor,
	}, nil
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

	markets atomic.Value // map[string]*bnMarket
	// tokenIDs maps the token's symbol to the list of bip ids of the token
	// for each chain for which deposits and withdrawals are enabled on
	// binance.
	tokenIDs atomic.Value // map[string][]uint32

	balanceMtx sync.RWMutex
	balances   map[string]*bncBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	booksMtx sync.RWMutex
	books    map[string]*bncBook

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*tradeInfo
	tradeUpdaters      map[int]chan *TradeUpdate
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
		balances:           make(map[string]*bncBalance),
		books:              make(map[string]*bncBook),
		net:                net,
		tradeInfo:          make(map[string]*tradeInfo),
		tradeUpdaters:      make(map[int]chan *TradeUpdate),
		cexUpdaters:        make(map[chan interface{}]struct{}, 0),
		tradeIDNoncePrefix: encode.RandomBytes(10),
	}

	bnc.markets.Store(make(map[string]*bnMarket))
	bnc.tokenIDs.Store(make(map[string][]uint32))

	return bnc
}

func (bnc *binance) updateBalances(coinsData []*binanceCoinInfo) {
	bnc.balanceMtx.Lock()
	defer bnc.balanceMtx.Unlock()

	for _, nfo := range coinsData {
		bnc.balances[nfo.Coin] = &bncBalance{
			available: nfo.Free,
			locked:    nfo.Locked,
		}
	}
}

func (bnc *binance) getCoinInfo(ctx context.Context) error {
	coins := make([]*binanceCoinInfo, 0)
	err := bnc.getAPI(ctx, "/sapi/v1/capital/config/getall", nil, true, true, &coins)
	if err != nil {
		return fmt.Errorf("error getting binance coin info: %v", err)
	}

	bnc.updateBalances(coins)

	tokenIDs := make(map[string][]uint32)
	for _, nfo := range coins {
		if nfo.Coin == "WBTC" {
			bnc.log.Infof("WBTC INFO: %+v", nfo)
			for _, netInfo := range nfo.NetworkList {
				bnc.log.Infof("%+v", netInfo)
			}
		}
		tokenSymbol := strings.ToLower(nfo.Coin)
		chainIDs, isToken := dex.TokenChains[tokenSymbol]
		if !isToken {
			continue
		}
		isSupportedChain := func(assetID uint32) (uint32, bool) {
			for _, chainID := range chainIDs {
				if chainID[1] == assetID {
					return chainID[0], true
				}
			}
			return 0, false
		}
		for _, netInfo := range nfo.NetworkList {
			chainSymbol := strings.ToLower(netInfo.Network)
			chainID, found := dex.BipSymbolID(chainSymbol)
			if !found {
				continue
			}
			if !netInfo.WithdrawEnable || !netInfo.DepositEnable {
				bnc.log.Tracef("Skipping %s network %s because deposits and/or withdraws are not enabled.", tokenSymbol, chainSymbol)
				continue
			}
			if tokenBipId, supported := isSupportedChain(chainID); supported {
				tokenIDs[tokenSymbol] = append(tokenIDs[tokenSymbol], tokenBipId)
			}
		}
	}
	bnc.tokenIDs.Store(tokenIDs)

	return nil
}

func (bnc *binance) getMarkets(ctx context.Context) error {
	var exchangeInfo struct {
		Timezone   string `json:"timezone"`
		ServerTime int64  `json:"serverTime"`
		RateLimits []struct {
			RateLimitType string `json:"rateLimitType"`
			Interval      string `json:"interval"`
			IntervalNum   int64  `json:"intervalNum"`
			Limit         int64  `json:"limit"`
		} `json:"rateLimits"`
		Symbols []*bnMarket `json:"symbols"`
	}
	err := bnc.getAPI(ctx, "/api/v3/exchangeInfo", nil, false, false, &exchangeInfo)
	if err != nil {
		return fmt.Errorf("error getting markets from Binance: %w", err)
	}

	marketsMap := make(map[string]*bnMarket, len(exchangeInfo.Symbols))
	for _, market := range exchangeInfo.Symbols {
		marketsMap[market.Symbol] = market
	}

	bnc.markets.Store(marketsMap)

	return nil
}

// Connect connects to the binance API.
func (bnc *binance) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg := new(sync.WaitGroup)

	if err := bnc.getCoinInfo(ctx); err != nil {
		return nil, fmt.Errorf("error getting coin info: %v", err)
	}

	if err := bnc.getMarkets(ctx); err != nil {
		return nil, fmt.Errorf("error getting markets: %v", err)
	}

	if err := bnc.getUserDataStream(ctx); err != nil {
		return nil, err
	}

	// Refresh the markets periodically.
	wg.Add(1)
	go func() {
		defer wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				err := bnc.getMarkets(ctx)
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

	bal, found := bnc.balances[assetConfig.coin]
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
func (bnc *binance) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (string, error) {
	side := "BUY"
	if sell {
		side = "SELL"
	}

	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return "", fmt.Errorf("error getting asset cfg for %d", baseID)
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return "", fmt.Errorf("error getting asset cfg for %d", quoteID)
	}

	slug := baseCfg.coin + quoteCfg.coin

	marketsMap := bnc.markets.Load().(map[string]*bnMarket)
	market, found := marketsMap[slug]
	if !found {
		return "", fmt.Errorf("market not found: %v", slug)
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

	bnc.tradeUpdaterMtx.RLock()
	_, found = bnc.tradeUpdaters[subscriptionID]
	if !found {
		bnc.tradeUpdaterMtx.RUnlock()
		return "", fmt.Errorf("no trade updater with ID %v", subscriptionID)
	}
	bnc.tradeUpdaterMtx.RUnlock()

	err = bnc.postAPI(ctx, "/api/v3/order", v, nil, true, true, &struct{}{})
	if err != nil {
		return "", err
	}

	bnc.tradeUpdaterMtx.Lock()
	defer bnc.tradeUpdaterMtx.Unlock()
	bnc.tradeInfo[tradeID] = &tradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
	}

	return tradeID, err
}

func (bnc *binance) assetPrecision(coin string) (int, error) {
	for _, market := range bnc.markets.Load().(map[string]*bnMarket) {
		if market.BaseAsset == coin {
			return market.BaseAssetPrecision, nil
		}
		if market.QuoteAsset == coin {
			return market.QuoteAssetPrecision, nil
		}
	}
	return 0, fmt.Errorf("asset %s not found", coin)
}

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
						bnc.log.Errorf("Failed to find DEX asset ID for Coin: %s, Network %s", status.Coin, status.Network)
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
func (bnc *binance) SubscribeTradeUpdates() (<-chan *TradeUpdate, func(), int) {
	bnc.tradeUpdaterMtx.Lock()
	defer bnc.tradeUpdaterMtx.Unlock()
	updaterID := bnc.tradeUpdateCounter
	bnc.tradeUpdateCounter++
	updater := make(chan *TradeUpdate, 256)
	bnc.tradeUpdaters[updaterID] = updater

	unsubscribe := func() {
		bnc.tradeUpdaterMtx.Lock()
		delete(bnc.tradeUpdaters, updaterID)
		bnc.tradeUpdaterMtx.Unlock()
	}

	return updater, unsubscribe, updaterID
}

// CancelTrade cancels a trade on the CEX.
func (bnc *binance) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", baseID)
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", quoteID)
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

func (bnc *binance) Markets() ([]*Market, error) {
	bnMarkets := bnc.markets.Load().(map[string]*bnMarket)
	markets := make([]*Market, 0, 16)
	tokenIDs := bnc.tokenIDs.Load().(map[string][]uint32)
	for _, mkt := range bnMarkets {
		markets = append(markets, mkt.dexMarkets(tokenIDs)...)
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

func (bnc *binance) getListenID(ctx context.Context) (string, error) {
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	return resp.ListenKey, bnc.postAPI(ctx, "/api/v3/userDataStream", nil, nil, true, false, &resp)
}

type wsBalance struct {
	Asset  string  `json:"a"`
	Free   float64 `json:"f,string"`
	Locked float64 `json:"l,string"`
}

type bncStreamUpdate struct {
	Asset              string          `json:"a"`
	EventType          string          `json:"e"`
	ClientOrderID      string          `json:"c"`
	CurrentOrderStatus string          `json:"X"`
	Balances           []*wsBalance    `json:"B"`
	BalanceDelta       float64         `json:"d,string"`
	Filled             float64         `json:"z,string"`
	QuoteFilled        float64         `json:"Z,string"`
	OrderQty           float64         `json:"q,string"`
	QuoteOrderQty      float64         `json:"Q,string"`
	CancelledOrderID   string          `json:"C"`
	E                  json.RawMessage `json:"E"`
}

func decodeStreamUpdate(b []byte) (*bncStreamUpdate, error) {
	var msg *bncStreamUpdate
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (bnc *binance) sendCexUpdateNotes() {
	bnc.cexUpdatersMtx.RLock()
	defer bnc.cexUpdatersMtx.RUnlock()
	for updater := range bnc.cexUpdaters {
		updater <- struct{}{}
	}
}

func (bnc *binance) handleOutboundAccountPosition(update *bncStreamUpdate) {
	bnc.log.Tracef("Received outboundAccountPosition: %+v", update)
	for _, bal := range update.Balances {
		bnc.log.Tracef("balance: %+v", bal)
	}

	bnc.balanceMtx.Lock()
	for _, bal := range update.Balances {
		symbol := strings.ToLower(bal.Asset)
		bnc.balances[symbol] = &bncBalance{
			available: bal.Free,
			locked:    bal.Locked,
		}
	}
	bnc.balanceMtx.Unlock()
	bnc.sendCexUpdateNotes()
}

func (bnc *binance) getTradeUpdater(tradeID string) (chan *TradeUpdate, *tradeInfo, error) {
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

func (bnc *binance) handleExecutionReport(update *bncStreamUpdate) {
	bnc.log.Tracef("Received executionReport: %+v", update)

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

	complete := status == "FILLED" || status == "CANCELED" || status == "REJECTED" || status == "EXPIRED"

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

	updater <- &TradeUpdate{
		TradeID:     id,
		Complete:    complete,
		BaseFilled:  uint64(update.Filled * float64(baseCfg.conversionFactor)),
		QuoteFilled: uint64(update.QuoteFilled * float64(quoteCfg.conversionFactor)),
	}

	if complete {
		bnc.removeTradeUpdater(id)
	}
}

func (bnc *binance) getUserDataStream(ctx context.Context) (err error) {
	streamHandler := func(b []byte) {
		u, err := decodeStreamUpdate(b)
		if err != nil {
			bnc.log.Errorf("Error unmarshaling user stream update: %v", err)
			bnc.log.Errorf("Raw message: %s", string(b))
			return
		}
		switch u.EventType {
		case "outboundAccountPosition":
			bnc.handleOutboundAccountPosition(u)
		case "executionReport":
			bnc.handleExecutionReport(u)
		case "balanceUpdate":
			// TODO: check if we need this.. is outbound account position enough?
			bnc.log.Tracef("Received balanceUpdate: %s", string(b))
		}
	}

	var listenKey string
	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err = bnc.getListenID(ctx)
		if err != nil {
			return nil, err
		}

		hdr := make(http.Header)
		hdr.Set("X-MBX-APIKEY", bnc.apiKey)
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: bnc.wsURL + "/ws/" + listenKey,

			// Is this necessary for user data stream??
			PingWait: time.Minute * 4,

			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			ConnectEventFunc: func(cs comms.ConnectionStatus) {},
			Logger:           bnc.log.SubLogger("BNCWS"),
			RawHandler:       streamHandler,
			ConnectHeaders:   hdr,
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

type bncBookBinUpdate struct {
	LastUpdateID uint64           `json:"lastUpdateId"`
	Bids         [][2]json.Number `json:"bids"`
	Asks         [][2]json.Number `json:"asks"`
}

type bncBookNote struct {
	StreamName string            `json:"stream"`
	Data       *bncBookBinUpdate `json:"data"`
}

func bncParseBookUpdates(pts [][2]json.Number) ([]*bookBin, error) {
	bins := make([]*bookBin, 0, len(pts))
	for _, nums := range pts {
		price, err := nums[0].Float64()
		if err != nil {
			return nil, fmt.Errorf("error parsing price: %v", err)
		}
		qty, err := nums[1].Float64()
		if err != nil {
			return nil, fmt.Errorf("error quantity qty: %v", err)
		}
		bins = append(bins, &bookBin{
			Price: price,
			Qty:   qty,
		})
	}
	return bins, nil
}

func (bnc *binance) storeMarketStream(conn comms.WsConn) {
	bnc.marketStreamMtx.Lock()
	bnc.marketStream = conn
	bnc.marketStreamMtx.Unlock()
}

func (bnc *binance) subUnsubDepth(conn comms.WsConn, method, slug string) error {
	req := &struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
		ID     uint64   `json:"id"`
	}{
		Method: method,
		Params: []string{
			slug + "@depth20",
		},
		ID: atomic.AddUint64(&subscribeID, 1),
	}

	b, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("error marshaling subscription stream request: %w", err)
	}

	bnc.log.Debugf("Sending %v for market %v", method, slug)
	if err := conn.SendRaw(b); err != nil {
		return fmt.Errorf("error sending subscription stream request: %w", err)
	}

	return nil
}

func (bnc *binance) stopMarketDataStream(slug string) (err error) {
	bnc.marketStreamMtx.RLock()
	conn := bnc.marketStream
	bnc.marketStreamMtx.RUnlock()
	if conn == nil {
		return fmt.Errorf("can't unsubscribe. no stream - %p", bnc)
	}

	var unsubscribe bool

	bnc.booksMtx.Lock()
	defer func() {
		bnc.booksMtx.Unlock()
		if unsubscribe {
			if err := bnc.subUnsubDepth(conn, "UNSUBSCRIBE", slug); err != nil {
				bnc.log.Errorf("subUnsubDepth(UNSUBSCRIBE): %v", err)
			}
		}
	}()

	book, found := bnc.books[slug]
	if !found {
		unsubscribe = true
		return nil
	}

	book.numSubscribers--
	if book.numSubscribers == 0 {
		unsubscribe = true
		delete(bnc.books, slug)
	}

	return nil
}

func (bnc *binance) startMarketDataStream(ctx context.Context, baseSymbol, quoteSymbol string) (err error) {
	slug := strings.ToLower(baseSymbol + quoteSymbol)

	bnc.marketStreamMtx.Lock()
	defer bnc.marketStreamMtx.Unlock()

	// If a market stream already exists, just subscribe to this market.
	if bnc.marketStream != nil {
		bnc.booksMtx.Lock()
		_, exists := bnc.books[slug]
		if !exists {
			bnc.books[slug] = newBNCBook()
		} else {
			bnc.books[slug].numSubscribers++
		}
		bnc.booksMtx.Unlock()

		if err := bnc.subUnsubDepth(bnc.marketStream, "SUBSCRIBE", slug); err != nil {
			return fmt.Errorf("subUnsubDepth(SUBSCRIBE): %v", err)
		}
		return nil
	}

	streamHandler := func(b []byte) {
		var note *bncBookNote
		if err := json.Unmarshal(b, &note); err != nil {
			bnc.log.Errorf("Error unmarshaling book note: %v", err)
			return
		}

		if note == nil || note.Data == nil {
			bnc.log.Debugf("No data in %q update: %+v", slug, note)
			return
		}

		parts := strings.Split(note.StreamName, "@")
		if len(parts) != 2 || parts[1] != "depth20" {
			bnc.log.Errorf("Unknown stream name %q", note.StreamName)
			return
		}
		slug = parts[0] // will be lower-case

		bnc.log.Tracef("Received %d bids and %d asks in a book update for %s", len(note.Data.Bids), len(note.Data.Asks), slug)

		bids, err := bncParseBookUpdates(note.Data.Bids)
		if err != nil {
			bnc.log.Errorf("Error parsing bid updates: %v", err)
			return
		}

		asks, err := bncParseBookUpdates(note.Data.Asks)
		if err != nil {
			bnc.log.Errorf("Error parsing ask updates: %v", err)
			return
		}

		bnc.booksMtx.Lock()
		defer bnc.booksMtx.Unlock()
		book := bnc.books[slug]
		if book == nil {
			bnc.log.Errorf("No book for stream %q", slug)
			return
		}

		book.latestUpdate = time.Now().Unix()
		book.asks = asks
		book.bids = bids
	}

	newConn := func() (comms.WsConn, *dex.ConnectionMaster, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		bnc.log.Debugf("Creating a new Binance market stream handler")
		// Get a list of current subscriptions so we can restart them all.
		bnc.booksMtx.Lock()
		bnc.books[slug] = newBNCBook()
		bnc.booksMtx.Unlock()

		streamName := slug + "@depth20"
		addr := fmt.Sprintf("%s/stream?streams=%s", bnc.wsURL, streamName)

		// Need to send key but not signature
		conn, err := comms.NewWsConn(&comms.WsCfg{
			URL: addr,
			// The websocket server will send a ping frame every 3 minutes. If the
			// websocket server does not receive a pong frame back from the
			// connection within a 10 minute period, the connection will be
			// disconnected. Unsolicited pong frames are allowed.
			PingWait: time.Minute * 4,
			ReconnectSync: func() {
				bnc.log.Debugf("Binance reconnected")
			},
			ConnectEventFunc: func(cs comms.ConnectionStatus) {},
			Logger:           bnc.log.SubLogger("BNCBOOK"),
			RawHandler:       streamHandler,
		})
		if err != nil {
			return nil, nil, err
		}

		cm := dex.NewConnectionMaster(conn)
		if err = cm.ConnectOnce(ctx); err != nil {
			return nil, nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return conn, cm, nil
	}

	conn, cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}
	bnc.marketStream = conn

	go func() {
		defer func() {
			bnc.storeMarketStream(nil)
			if cm != nil {
				cm.Disconnect()
			}
		}()

		reconnect := time.After(time.Hour * 12)
		for {
			select {
			case <-reconnect:
				if cm != nil {
					cm.Disconnect()
				}
				conn, cm, err = newConn()
				if err != nil {
					bnc.log.Errorf("Error reconnecting: %v", err)
					reconnect = time.After(time.Second * 30)
				} else {
					bnc.storeMarketStream(conn)
					reconnect = time.After(time.Hour * 12)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (bnc *binance) UnsubscribeMarket(baseID, quoteID uint32) (err error) {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", baseID)
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", quoteID)
	}

	bnc.stopMarketDataStream(strings.ToLower(baseCfg.coin + quoteCfg.coin))
	return nil
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP.
func (bnc *binance) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", baseID)
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fmt.Errorf("error getting asset cfg for %d", quoteID)
	}

	return bnc.startMarketDataStream(ctx, baseCfg.coin, quoteCfg.coin)
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market.
func (bnc *binance) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (avgPrice, extrema uint64, filled bool, err error) {
	fail := func(err error) (uint64, uint64, bool, error) {
		return 0, 0, false, err
	}

	baseCfg, err := bncAssetCfg(baseID)
	if err != nil {
		return fail(fmt.Errorf("error getting asset cfg for %d", baseID))
	}

	quoteCfg, err := bncAssetCfg(quoteID)
	if err != nil {
		return fail(fmt.Errorf("error getting asset cfg for %d", quoteID))
	}

	slug := strings.ToLower(baseCfg.coin + quoteCfg.coin)
	var side []*bookBin
	var latestUpdate int64
	bnc.booksMtx.RLock()
	book, found := bnc.books[slug]
	if found {
		latestUpdate = book.latestUpdate
		if sell {
			side = book.asks
		} else {
			side = book.bids
		}
	}
	bnc.booksMtx.RUnlock()
	if side == nil {
		return fail(fmt.Errorf("no book found for %s", slug))
	}

	if latestUpdate < time.Now().Unix()-60 {
		return fail(fmt.Errorf("book for %s is stale", slug))
	}

	remaining := qty
	var weightedSum uint64
	for _, bin := range side {
		extrema = calc.MessageRateAlt(bin.Price, baseCfg.conversionFactor, quoteCfg.conversionFactor)
		binQty := uint64(bin.Qty * float64(baseCfg.conversionFactor))
		if binQty >= remaining {
			filled = true
			weightedSum += remaining * extrema
			break
		}
		remaining -= binQty
		weightedSum += binQty * extrema
	}

	if !filled {
		return 0, 0, false, nil
	}

	return weightedSum / qty, extrema, true, nil
}

type binanceNetworkInfo struct {
	AddressRegex            string  `json:"addressRegex"`
	Coin                    string  `json:"coin"`
	DepositEnable           bool    `json:"depositEnable"`
	IsDefault               bool    `json:"isDefault"`
	MemoRegex               string  `json:"memoRegex"`
	MinConfirm              int     `json:"minConfirm"`
	Name                    string  `json:"name"`
	Network                 string  `json:"network"`
	ResetAddressStatus      bool    `json:"resetAddressStatus"`
	SpecialTips             string  `json:"specialTips"`
	UnLockConfirm           int     `json:"unLockConfirm"`
	WithdrawEnable          bool    `json:"withdrawEnable"`
	WithdrawFee             float64 `json:"withdrawFee,string"`
	WithdrawIntegerMultiple float64 `json:"withdrawIntegerMultiple,string"`
	WithdrawMax             float64 `json:"withdrawMax,string"`
	WithdrawMin             float64 `json:"withdrawMin,string"`
	SameAddress             bool    `json:"sameAddress"`
	EstimatedArrivalTime    int     `json:"estimatedArrivalTime"`
	Busy                    bool    `json:"busy"`
}

type binanceCoinInfo struct {
	Coin              string                `json:"coin"`
	DepositAllEnable  bool                  `json:"depositAllEnable"`
	Free              float64               `json:"free,string"`
	Freeze            float64               `json:"freeze,string"`
	Ipoable           float64               `json:"ipoable,string"`
	Ipoing            float64               `json:"ipoing,string"`
	IsLegalMoney      bool                  `json:"isLegalMoney"`
	Locked            float64               `json:"locked,string"`
	Name              string                `json:"name"`
	Storage           float64               `json:"storage,string"`
	Trading           bool                  `json:"trading"`
	WithdrawAllEnable bool                  `json:"withdrawAllEnable"`
	Withdrawing       float64               `json:"withdrawing,string"`
	NetworkList       []*binanceNetworkInfo `json:"networkList"`
}

type bnMarket struct {
	Symbol              string   `json:"symbol"`
	Status              string   `json:"status"`
	BaseAsset           string   `json:"baseAsset"`
	BaseAssetPrecision  int      `json:"baseAssetPrecision"`
	QuoteAsset          string   `json:"quoteAsset"`
	QuoteAssetPrecision int      `json:"quoteAssetPrecision"`
	OrderTypes          []string `json:"orderTypes"`
}

// dexMarkets returns all the possible dex markets for this binance market.
// A symbol represents a single market on the CEX, but tokens on the DEX
// have a different assetID for each network they are on, therefore they will
// match multiple markets as defined using assetID.
func (s *bnMarket) dexMarkets(tokenIDs map[string][]uint32) []*Market {
	var baseAssetIDs, quoteAssetIDs []uint32

	getAssetIDs := func(coin string) []uint32 {
		symbol := strings.ToLower(coin)
		assetIDs := make([]uint32, 0, 1)

		// In some cases a token may also be a base asset. For example btc
		// should return the btc ID as well as the ID of all wbtc tokens.
		if assetID, found := dex.BipSymbolID(symbol); found {
			assetIDs = append(assetIDs, assetID)
		}
		if tokenIDs, found := tokenIDs[symbol]; found {
			assetIDs = append(assetIDs, tokenIDs...)
		}

		return assetIDs
	}

	baseAssetIDs = getAssetIDs(s.BaseAsset)
	if len(baseAssetIDs) == 0 {
		return nil
	}

	quoteAssetIDs = getAssetIDs(s.QuoteAsset)
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
