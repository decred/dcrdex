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
	// assetID is the bip id
	assetID uint32
	// symbol is the DEX asset symbol, always lower case
	symbol string
	// coin is the asset symbol on binance, always upper case.
	// For a token like USDC, the coin field will be USDC, but
	// symbol field will be usdc.eth.
	coin string
	// network will be the same as coin for the base assets of
	// a blockchain, but for tokens it will be the network
	// that the token is hosted such as "ETH".
	network          string
	conversionFactor uint64
}

func bncSymbolData(symbol string) (*bncAssetConfig, error) {
	coin := strings.ToUpper(symbol)
	var ok bool
	assetID, ok := dex.BipSymbolID(symbol)
	if !ok {
		return nil, fmt.Errorf("not id found for %q", symbol)
	}
	networkID := assetID
	if token := asset.TokenInfo(assetID); token != nil {
		networkID = token.ParentID
		parts := strings.Split(symbol, ".")
		coin = strings.ToUpper(parts[0])
	}
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, fmt.Errorf("no unit info found for %d", assetID)
	}
	return &bncAssetConfig{
		assetID:          assetID,
		symbol:           symbol,
		coin:             coin,
		network:          strings.ToUpper(dex.BipIDSymbol(networkID)),
		conversionFactor: ui.Conventional.ConversionFactor,
	}, nil
}

// bncBalance is the balance of an asset in conventional units. This must be
// converted before returning.
type bncBalance struct {
	available float64
	locked    float64
}

type binance struct {
	wg          sync.WaitGroup
	log         dex.Logger
	url         string
	wsURL       string
	apiKey      string
	secretKey   string
	knownAssets map[uint32]bool
	net         dex.Network
	ctx         context.Context

	markets atomic.Value // map[string]*symbol

	balanceMtx sync.RWMutex
	balances   map[string]*bncBalance

	marketStreamMtx sync.RWMutex
	marketStream    comms.WsConn

	booksMtx sync.RWMutex
	books    map[string]*bncBook

	tradeUpdaterMtx    sync.RWMutex
	tradeToUpdater     map[string]int
	tradeUpdaters      map[int]chan *TradeUpdate
	tradeUpdateCounter int

	cexUpdatersMtx sync.RWMutex
	cexUpdaters    []chan interface{}
}

var _ CEX = (*binance)(nil)

func newBinance(apiKey, secretKey string, log dex.Logger, net dex.Network, binanceUS bool) CEX {
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
		log:            log,
		url:            url,
		wsURL:          wsURL,
		apiKey:         apiKey,
		secretKey:      secretKey,
		knownAssets:    knownAssets,
		balances:       make(map[string]*bncBalance),
		books:          make(map[string]*bncBook),
		net:            net,
		tradeToUpdater: make(map[string]int),
		tradeUpdaters:  make(map[int]chan *TradeUpdate),
		cexUpdaters:    make([]chan interface{}, 0),
	}

	bnc.markets.Store(make(map[string]symbol))

	return (CEX)(bnc)
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

func (bnc *binance) getCoinInfo() error {
	if bnc.net == dex.Testnet {
		return nil
	}

	coins := make([]*binanceCoinInfo, 0)
	err := bnc.getAPI("/sapi/v1/capital/config/getall", nil, true, true, &coins)
	if err != nil {
		return fmt.Errorf("error getting binance coin info: %v", err)
	}

	bnc.updateBalances(coins)
	return nil
}

func (bnc *binance) getMarkets() error {
	exchangeInfo := &exchangeInfo{}
	err := bnc.getAPI("/api/v3/exchangeInfo", nil, false, false, exchangeInfo)
	if err != nil {
		return fmt.Errorf("error getting markets from Binance: %w", err)
	}

	marketsMap := make(map[string]symbol, len(exchangeInfo.Symbols))
	for _, symbol := range exchangeInfo.Symbols {
		marketsMap[symbol.Symbol] = symbol
	}

	bnc.markets.Store(marketsMap)

	return nil
}

// Connect connects to the binance API.
func (bnc *binance) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	bnc.ctx = ctx

	if err := bnc.getCoinInfo(); err != nil {
		return nil, fmt.Errorf("error getting coin info: %v", err)
	}

	if err := bnc.getMarkets(); err != nil {
		return nil, fmt.Errorf("error getting markets: %v", err)
	}

	if err := bnc.getUserDataStream(); err != nil {
		return nil, err
	}

	// Refresh the markets periodically.
	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				err := bnc.getMarkets()
				if err != nil {
					bnc.log.Errorf("error fetching markets: %v", err)
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
	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
		nextTick := time.After(time.Hour)
		for {
			select {
			case <-nextTick:
				err := bnc.getCoinInfo()
				if err != nil {
					bnc.log.Errorf("error fetching markets: %v", err)
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

	return &bnc.wg, nil
}

// SubscribeCEXUpdates returns a channel which sends an empty struct when
// the balance of an asset on the CEX has been updated.
func (bnc *binance) SubscribeCEXUpdates() <-chan interface{} {
	updater := make(chan interface{}, 128)
	bnc.cexUpdatersMtx.Lock()
	bnc.cexUpdaters = append(bnc.cexUpdaters, updater)
	bnc.cexUpdatersMtx.Unlock()
	return updater
}

// Balance returns the balance of an asset at the CEX.
func (bnc *binance) Balance(symbol string) (*ExchangeBalance, error) {
	assetConfig, err := bncSymbolData(symbol)
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
		Available: uint64(bal.available * float64(assetConfig.conversionFactor)),
		Locked:    uint64(bal.locked * float64(assetConfig.conversionFactor)),
	}, nil
}

// GenerateTradeID returns a trade ID that must be passed as an argument
// when calling Trade. This ID will be used to identify updates to the
// trade. It is necessary to pre-generate this because updates to the
// trade may arrive before the Trade function returns.
func (bnc *binance) GenerateTradeID() string {
	// TODO: make it unrepeatable
	return fmt.Sprintf("%x", encode.RandomBytes(10))
}

// Trade executes a trade on the CEX. updaterID takes an ID returned from
// SubscribeTradeUpdates, and tradeID takes an ID returned from
// GenerateTradeID.
func (bnc *binance) Trade(baseSymbol, quoteSymbol string, sell bool, rate, qty uint64, updaterID int, orderID string) error {
	side := "BUY"
	if sell {
		side = "SELL"
	}

	baseCfg, err := bncSymbolData(baseSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", baseSymbol, err)
	}

	quoteCfg, err := bncSymbolData(quoteSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", quoteSymbol, err)
	}

	slug := baseCfg.coin + quoteCfg.coin

	marketsMap := bnc.markets.Load().(map[string]symbol)
	market, found := marketsMap[slug]
	if !found {
		return fmt.Errorf("market not found: %v", slug)
	}

	price := calc.ConventionalRateAlt(rate, baseCfg.conversionFactor, quoteCfg.conversionFactor)
	amt := float64(qty) / float64(baseCfg.conversionFactor)

	v := make(url.Values)
	v.Add("symbol", slug)
	v.Add("side", side)
	v.Add("type", "LIMIT")
	v.Add("timeInForce", "GTC")
	v.Add("newClientOrderId", orderID)
	v.Add("quantity", strconv.FormatFloat(amt, 'f', market.BaseAssetPrecision, 64))
	v.Add("price", strconv.FormatFloat(price, 'f', market.QuoteAssetPrecision, 64))

	bnc.tradeUpdaterMtx.Lock()
	_, found = bnc.tradeUpdaters[updaterID]
	if !found {
		bnc.tradeUpdaterMtx.Unlock()
		return fmt.Errorf("no trade updater with ID %v", updaterID)
	}
	bnc.tradeToUpdater[orderID] = updaterID
	bnc.tradeUpdaterMtx.Unlock()

	return bnc.postAPI("/api/v3/order", v, nil, true, true, &struct{}{})
}

// SubscribeTradeUpdates returns a channel that the caller can use to
// listen for updates to a trade's status. If integer returned from
// this function must be passed as the updaterID argument to Trade,
// updates to the trade will be sent on the channel.
func (bnc *binance) SubscribeTradeUpdates() (<-chan *TradeUpdate, int) {
	bnc.tradeUpdaterMtx.Lock()
	defer bnc.tradeUpdaterMtx.Unlock()

	bnc.tradeUpdateCounter++
	updater := make(chan *TradeUpdate, 256)
	bnc.tradeUpdaters[bnc.tradeUpdateCounter] = updater
	return updater, bnc.tradeUpdateCounter
}

// CancelTrade cancels a trade on the CEX.
func (bnc *binance) CancelTrade(baseSymbol, quoteSymbol string, tradeID string) error {
	baseCfg, err := bncSymbolData(baseSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", baseSymbol, err)
	}

	quoteCfg, err := bncSymbolData(quoteSymbol)
	if err != nil {
		return fmt.Errorf("error getting symbol data for %s: %w", quoteSymbol, err)
	}

	slug := baseCfg.coin + quoteCfg.coin

	v := make(url.Values)
	v.Add("symbol", slug)
	v.Add("origClientOrderId", tradeID)

	req, err := bnc.generateRequest("DELETE", "/api/v3/order", v, nil, true, true)
	if err != nil {
		return err
	}

	return bnc.requestInto(req, &struct{}{})
}

func (bnc *binance) Balances() (map[uint32]*ExchangeBalance, error) {
	bnc.balanceMtx.RLock()
	defer bnc.balanceMtx.RUnlock()

	balances := make(map[uint32]*ExchangeBalance)

	for coin, bal := range bnc.balances {
		assetConfig, err := bncSymbolData(strings.ToLower(coin))
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

func (bnc *binance) Markets() ([]*Market, error) {
	symbols := bnc.markets.Load().(map[string]symbol)

	markets := make([]*Market, 0, 16)
	for _, symbol := range symbols {
		markets = append(markets, symbol.markets()...)
	}

	return markets, nil
}

func (bnc *binance) getAPI(endpoint string, query url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(http.MethodGet, endpoint, query, nil, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *binance) postAPI(endpoint string, query, form url.Values, key, sign bool, thing interface{}) error {
	req, err := bnc.generateRequest(http.MethodPost, endpoint, query, form, key, sign)
	if err != nil {
		return fmt.Errorf("generateRequest error: %w", err)
	}
	return bnc.requestInto(req, thing)
}

func (bnc *binance) requestInto(req *http.Request, thing interface{}) error {
	bnc.log.Tracef("sending request: %+v", req)

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
	reader := io.LimitReader(resp.Body, 1<<20)
	r, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(r, thing); err != nil {
		return fmt.Errorf("json Decode error: %w", err)
	}
	return nil
}

func (bnc *binance) generateRequest(method, endpoint string, query, form url.Values, key, sign bool) (*http.Request, error) {
	var fullURL string
	if bnc.net == dex.Simnet && strings.Contains(endpoint, "sapi") {
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

	req, err := http.NewRequestWithContext(bnc.ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("NewRequestWithContext error: %w", err)
	}

	req.Header = header

	return req, nil
}

func (bnc *binance) getListenID() (string, error) {
	var resp struct {
		ListenKey string `json:"listenKey"`
	}
	return resp.ListenKey, bnc.postAPI("/api/v3/userDataStream", nil, nil, true, false, &resp)
}

type wsBalance struct {
	Asset  string  `json:"a"`
	Free   float64 `json:"f,string"`
	Locked float64 `json:"l,string"`
}

type bncStreamUpdate struct {
	Asset              string       `json:"a"`
	EventType          string       `json:"e"`
	ClientOrderID      string       `json:"c"`
	CurrentOrderStatus string       `json:"X"`
	Balances           []*wsBalance `json:"B"`
	BalanceDelta       float64      `json:"d,string"`
	Filled             float64      `json:"z,string"`
	OrderQty           float64      `json:"q,string"`
}

func decodeStreamUpdate(b []byte) (*bncStreamUpdate, error) {
	var msg *bncStreamUpdate
	// Go's json doesn't handle case-sensitivity all that well.
	b = bytes.ReplaceAll(b, []byte(`"E"`), []byte(`"E0"`))
	b = bytes.ReplaceAll(b, []byte(`"C"`), []byte(`"C0"`))
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (bnc *binance) sendCexUpdateNotes() {
	bnc.cexUpdatersMtx.RLock()
	defer bnc.cexUpdatersMtx.RUnlock()
	for _, updater := range bnc.cexUpdaters {
		updater <- struct{}{}
	}
}

func (bnc *binance) handleOutboundAccountPosition(update *bncStreamUpdate) {
	bnc.log.Tracef("received outboundAccountPosition: %+v", update)

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

func (bnc *binance) handleExecutionReport(update *bncStreamUpdate) {
	bnc.log.Tracef("received executionReport: %+v", update)

	bnc.tradeUpdaterMtx.RLock()
	updaterID, found := bnc.tradeToUpdater[update.ClientOrderID]
	if !found {
		bnc.log.Errorf("updater not found for trade ID %v", update.ClientOrderID)
		bnc.tradeUpdaterMtx.RUnlock()
		return
	}
	updater, found := bnc.tradeUpdaters[updaterID]
	if !found {
		bnc.log.Errorf("no updater with ID %v", updaterID)
		bnc.tradeUpdaterMtx.RUnlock()
		return
	}
	status := update.CurrentOrderStatus

	updater <- &TradeUpdate{
		TradeID:  update.ClientOrderID,
		Complete: status == "FILLED" || status == "CANCELED" || status == "REJECTED" || status == "EXPIRED",
	}
	bnc.tradeUpdaterMtx.RUnlock()

}

func (bnc *binance) getUserDataStream() (err error) {
	streamHandler := func(b []byte) {
		u, err := decodeStreamUpdate(b)
		if err != nil {
			bnc.log.Errorf("error unmarshaling user stream update: %v", err)
			bnc.log.Errorf("raw message: %s", string(b))
			return
		}
		switch u.EventType {
		case "outboundAccountPosition":
			bnc.handleOutboundAccountPosition(u)
		case "executionReport":
			bnc.handleExecutionReport(u)
		case "balanceUpdate":
			// TODO: check if we need this.. is outbound account position enough?
			bnc.log.Tracef("received balanceUpdate: %s", string(b))
		}
	}

	var listenKey string
	newConn := func() (*dex.ConnectionMaster, error) {
		listenKey, err = bnc.getListenID()
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
		if err = cm.ConnectOnce(bnc.ctx); err != nil {
			return nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return cm, nil
	}

	cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}

	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
		// A single connection to stream.binance.com is only valid for 24 hours;
		// expect to be disconnected at the 24 hour mark.
		reconnect := time.After(time.Hour * 12)
		// Keepalive a user data stream to prevent a time out. User data streams
		// will close after 60 minutes. It's recommended to send a ping about
		// every 30 minutes.
		keepAlive := time.NewTicker(time.Minute * 30)
		defer keepAlive.Stop()
		for {
			select {
			case <-reconnect:
				if cm != nil {
					cm.Disconnect()
				}
				cm, err = newConn()
				if err != nil {
					bnc.log.Errorf("error reconnecting: %v", err)
					reconnect = time.After(time.Second * 30)
				} else {
					reconnect = time.After(time.Hour * 12)
				}
			case <-keepAlive.C:
				q := make(url.Values)
				q.Add("listenKey", listenKey)
				// Doing a PUT on a listenKey will extend its validity for 60 minutes.
				req, err := bnc.generateRequest(http.MethodPut, "/api/v3/userDataStream", q, nil, true, false)
				if err != nil {
					bnc.log.Errorf("error generating keep-alive request: %v", err)
					continue
				}
				if err := bnc.requestInto(req, nil); err != nil {
					bnc.log.Errorf("error sending keep-alive request: %v", err)
				}
			case <-bnc.ctx.Done():
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

	bnc.log.Debugf("sending %v for market %v", method, slug)
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

	bnc.booksMtx.Lock()
	defer bnc.booksMtx.Unlock()

	var unsubscribe bool
	book, found := bnc.books[slug]
	if found {
		book.numSubscribers--
		if book.numSubscribers == 0 {
			unsubscribe = true
			delete(bnc.books, slug)
		}
	} else {
		bnc.log.Debugf("no book")
		unsubscribe = true
	}
	if !unsubscribe {
		return nil
	}

	if err := bnc.subUnsubDepth(conn, "UNSUBSCRIBE", slug); err != nil {
		return fmt.Errorf("subUnsubDepth(UNSUBSCRIBE): %v", err)
	}

	return nil
}

func (bnc *binance) startMarketDataStream(baseSymbol, quoteSymbol string) (err error) {
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
			bnc.log.Errorf("error unmarshaling book note: %v", err)
			return
		}

		if note == nil || note.Data == nil {
			bnc.log.Debugf("no data in %q update: %+v", slug, note)
			return
		}

		parts := strings.Split(note.StreamName, "@")
		if len(parts) != 2 || parts[1] != "depth20" {
			bnc.log.Errorf("unknown stream name %q", note.StreamName)
			return
		}
		slug = parts[0] // will be lower-case

		bnc.log.Tracef("received %d bids and %d asks in a book update for %s", len(note.Data.Bids), len(note.Data.Asks), slug)

		bids, err := bncParseBookUpdates(note.Data.Bids)
		if err != nil {
			bnc.log.Errorf("error parsing bid updates: %v", err)
			return
		}

		asks, err := bncParseBookUpdates(note.Data.Asks)
		if err != nil {
			bnc.log.Errorf("error parsing ask updates: %v", err)
			return
		}

		bnc.booksMtx.Lock()
		defer bnc.booksMtx.Unlock()
		book := bnc.books[slug]
		if book == nil {
			bnc.log.Errorf("no book for stream %q", slug)
			return
		}

		book.asks = asks
		book.bids = bids
	}

	newConn := func() (comms.WsConn, *dex.ConnectionMaster, error) {
		if bnc.ctx.Err() != nil {
			return nil, nil, bnc.ctx.Err()
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
		if err = cm.ConnectOnce(bnc.ctx); err != nil {
			return nil, nil, fmt.Errorf("websocketHandler remote connect: %v", err)
		}

		return conn, cm, nil
	}

	conn, cm, err := newConn()
	if err != nil {
		return fmt.Errorf("error initializing connection: %v", err)
	}
	bnc.marketStream = conn

	bnc.wg.Add(1)
	go func() {
		defer bnc.wg.Done()
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
					bnc.log.Errorf("error reconnecting: %v", err)
					reconnect = time.After(time.Second * 30)
				} else {
					bnc.storeMarketStream(conn)
					reconnect = time.After(time.Hour * 12)
				}
			case <-bnc.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (bnc *binance) UnsubscribeMarket(baseSymbol, quoteSymbol string) error {
	return bnc.stopMarketDataStream(strings.ToLower(baseSymbol + quoteSymbol))
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP.
func (bnc *binance) SubscribeMarket(baseSymbol, quoteSymbol string) error {
	return bnc.startMarketDataStream(baseSymbol, quoteSymbol)
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market.
func (bnc *binance) VWAP(baseSymbol, quoteSymbol string, sell bool, qty uint64) (avgPrice, extrema uint64, filled bool, err error) {
	fail := func(err error) (uint64, uint64, bool, error) {
		return 0, 0, false, err
	}

	slug := strings.ToLower(baseSymbol + quoteSymbol)
	var side []*bookBin
	bnc.booksMtx.RLock()
	book, found := bnc.books[slug]
	if found {
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

	baseCfg, err := bncSymbolData(baseSymbol)
	if err != nil {
		return fail(fmt.Errorf("error getting symbol data for %s: %w", baseSymbol, err))
	}

	quoteCfg, err := bncSymbolData(quoteSymbol)
	if err != nil {
		return fail(fmt.Errorf("error getting symbol data for %s: %w", quoteSymbol, err))
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

type symbol struct {
	Symbol              string   `json:"symbol"`
	Status              string   `json:"status"`
	BaseAsset           string   `json:"baseAsset"`
	BaseAssetPrecision  int      `json:"baseAssetPrecision"`
	QuoteAsset          string   `json:"quoteAsset"`
	QuoteAssetPrecision int      `json:"quoteAssetPrecision"`
	OrderTypes          []string `json:"orderTypes"`
}

// markets returns all the possible markets for this symbol. A symbol
// represents a single market on the CEX, but tokens on the DEX have a
// different assetID for each network they are on, therefore they will
// match multiple markets as defined using assetID.
func (s *symbol) markets() []*Market {
	var baseAssetIDs, quoteAssetIDs []uint32

	getAssetIDs := func(coin string) []uint32 {
		symbol := strings.ToLower(coin)
		if assetID, found := dex.BipSymbolID(symbol); found {
			return []uint32{assetID}
		}

		if networks, found := dex.BipTokenSymbolNetworks(symbol); found {
			ids := make([]uint32, 0, len(networks))
			for _, assetID := range networks {
				ids = append(ids, assetID)
			}
			return ids
		}

		return nil
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
	for _, base := range baseAssetIDs {
		for _, quote := range quoteAssetIDs {
			markets = append(markets, &Market{
				Base:  base,
				Quote: quote,
			})
		}
	}
	return markets
}

type rateLimit struct {
	RateLimitType string `json:"rateLimitType"`
	Interval      string `json:"interval"`
	IntervalNum   int64  `json:"intervalNum"`
	Limit         int64  `json:"limit"`
}

type exchangeInfo struct {
	Timezone   string      `json:"timezone"`
	ServerTime int64       `json:"serverTime"`
	RateLimits []rateLimit `json:"rateLimits"`
	Symbols    []symbol    `json:"symbols"`
}
