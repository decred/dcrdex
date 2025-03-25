// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
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
	"decred.org/dcrdex/client/mm/libxc/cbtypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/utils"

	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"
)

// https://docs.cdp.coinbase.com/advanced-trade/reference/
// https://docs.cdp.coinbase.com/advanced-trade/docs/ws-overview

// supportedCoinbaseTokens is the set of supported Coinbase tokens.
// There is no API to query the networks that coinbase supports withdrawals on.
var supportedCoinbaseTokens = map[uint32]struct{}{
	60001:  {}, // USDC on ETH
	60002:  {}, // USDT on ETH
	966001: {}, // USDC on POLYGON
}

// cbWsConn manages a websocket connection to the Coinbase API. As per
// Coinbase's recommendation, a separate connection is created for the
// subscription to each market. cbWSConn subscribes to a single channel
// and also the heartbeats channel. If a message arrives out of order,
// the channel is unsubscribed, then resubscribed.
type cbWSConn struct {
	wsConn        comms.WsConn
	seq           atomic.Uint64
	wsPath        string
	log           dex.Logger
	productID     string
	channel       string
	channelID     string
	msgHandler    func([]byte)
	setSynced     func(bool)
	apiName       string
	apiPrivateKey string
}

func newCBWSConn(apiName, apiPrivateKey, wsPath, productID, channel, channelID string, msgHandler func([]byte), setSynced func(bool), log dex.Logger) *cbWSConn {
	return &cbWSConn{
		apiName:       apiName,
		apiPrivateKey: apiPrivateKey,
		wsPath:        wsPath,
		log:           log,
		productID:     productID,
		channel:       channel,
		channelID:     channelID,
		msgHandler:    msgHandler,
		setSynced:     setSynced,
	}
}

func (c *cbWSConn) handleWebsocketMessage(b []byte) {
	var msg struct {
		Type        string `json:"type"`
		Message     string `json:"message"`
		Channel     string `json:"channel"`
		SequenceNum uint64 `json:"sequence_num"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling websocket message: %v", err)
		c.log.Errorf("Raw Message: %s", string(b))
		return
	}
	if msg.Type == "error" {
		c.log.Errorf("Websocket error: %s", msg.Message)
		return
	}

	lastSeq := c.seq.Swap(msg.SequenceNum)
	if lastSeq != 0 && lastSeq != msg.SequenceNum-1 {
		c.log.Errorf("Websocket message out of sequence. %d -> %d", lastSeq, msg.SequenceNum)
		c.setSynced(false)

		// Will resubscribe in handleSubscriptionMessage.
		c.subUnsub(false)
		return
	}

	switch msg.Channel {
	case c.channelID:
		c.msgHandler(b)
	case "subscriptions":
		c.handleSubscriptionMessage(b)
	case "heartbeats":
	default:
		c.log.Errorf("Websocket message for unknown channel %q", msg.Channel)
	}
}
func (c *cbWSConn) subUnsub(sub bool) error {
	typ := "subscribe"
	if !sub {
		typ = "unsubscribe"
	}

	productIDs := []string{}
	if c.productID != "" {
		productIDs = []string{c.productID}
	}

	req := &coinbaseWebsocketSubscription{
		Type:       typ,
		ProductIDs: productIDs,
		Channel:    c.channel,
	}

	if c.apiName != "" {
		uri := fmt.Sprintf("%s %s%s", http.MethodGet, c.wsPath, "")
		jwt, err := buildJWT(c.apiName, c.apiPrivateKey, uri)
		if err != nil {
			return err
		}
		req.JWT = jwt
	}

	reqB, err := json.Marshal(req)
	if err != nil {
		return err
	}

	return c.wsConn.SendRaw(reqB)
}

func (c *cbWSConn) subHeartbeats() error {
	reqB, err := json.Marshal(&coinbaseWebsocketSubscription{
		Type:       "subscribe",
		ProductIDs: []string{},
		Channel:    "heartbeats",
	})
	if err != nil {
		return err
	}

	return c.wsConn.SendRaw(reqB)
}

func (c *cbWSConn) handleSubscriptionMessage(b []byte) {
	msg := new(cbtypes.SubscriptionMessage)
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling subscription message: %v", err)
		return
	}

	if len(msg.Events) != 1 {
		c.log.Errorf("subscriptions message received with %d events", len(msg.Events))
		return
	}

	subscribed := func(productID string) bool {
		if c.productID == "" {
			return true
		}
		parts := strings.Split(productID, "-")
		if len(parts) != 2 {
			return false
		}
		targetParts := strings.Split(c.productID, "-")
		if len(targetParts) != 2 {
			return false
		}
		eq := func(a, b string) bool {
			return a == b || (a == "USDC" && b == "USD") || (a == "USD" && b == "USDC")
		}
		return eq(parts[0], targetParts[0]) && eq(parts[1], targetParts[1])
	}

	// If there is no subcription to the channel, it means we have gone out of
	// sync, unsubscribed, and now need to resubscribe.
	var subbed bool
	for channel, productIDs := range msg.Events[0].Subscriptions {
		if channel == c.channel {
			for _, productID := range productIDs {
				if subscribed(productID) {
					subbed = true
					break
				}
			}
		}
	}

	if !subbed {
		c.log.Infof("Re-subscribing to %s - %s", c.channel, c.productID)
		c.subUnsub(true)
	}
}

func (c *cbWSConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	subLoggerName := fmt.Sprintf("WS-%s", c.channel)
	if c.productID != "" {
		subLoggerName += "-" + c.productID
	}

	conn, err := comms.NewWsConn(&comms.WsCfg{
		URL: "wss://" + c.wsPath,
		// The websocket server will send a ping frame every 3 minutes. If the
		// websocket server does not receive a pong frame back from the
		// connection within a 10 minute period, the connection will be
		// disconnected. Unsolicited pong frames are allowed.
		PingWait: time.Minute * 4,
		ReconnectSync: func() {
			fmt.Println("--reconnected")
		},

		ConnectEventFunc: func(cs comms.ConnectionStatus) {},
		Logger:           c.log.SubLogger(subLoggerName),
		RawHandler:       c.handleWebsocketMessage,
	})
	if err != nil {
		return nil, fmt.Errorf("error creating WsConn: %w", err)
	}

	cm := dex.NewConnectionMaster(conn)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to websocket feed: %w", err)
	}

	c.wsConn = conn

	if err := c.subUnsub(true); err != nil {
		cm.Disconnect()
		return nil, fmt.Errorf("error subscribing to level2: %w", err)
	}

	if err := c.subHeartbeats(); err != nil {
		cm.Disconnect()
		return nil, fmt.Errorf("error subscribing to heartbeat: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		cm.Disconnect()
	}()

	return &wg, nil
}

type cbBook struct {
	mtx            sync.RWMutex
	numSubscribers uint32

	cm        *dex.ConnectionMaster
	synced    atomic.Bool
	productID string
	wsPath    string
	book      *orderbook
	bui       *dex.UnitInfo
	qui       *dex.UnitInfo
	log       dex.Logger
}

func newCBBook(wsPath, productID string, bui, qui *dex.UnitInfo, log dex.Logger) *cbBook {
	return &cbBook{
		wsPath:    wsPath,
		productID: productID,
		book:      newOrderBook(),
		bui:       bui,
		qui:       qui,
		log:       log,
	}
}
func (c *cbBook) convertOBUpdates(updates []*cbtypes.OrderbookUpdate) (bids, asks []*obEntry) {
	bids = make([]*obEntry, 0, len(updates))
	asks = make([]*obEntry, 0, len(updates))
	for _, update := range updates {
		entry := &obEntry{
			qty:  toAtomic(update.NewQuantity, c.bui),
			rate: messageRate(update.PriceLevel, c.bui, c.qui),
		}
		if update.Side == "bid" {
			bids = append(bids, entry)
		} else {
			asks = append(asks, entry)
		}
	}
	return
}

func (c *cbBook) handleLevel2Message(b []byte) {
	msg := new(cbtypes.Level2Message)
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling level2 message: %v", err)
		return
	}

	for _, event := range msg.Events {
		if event.Type == "snapshot" {
			c.book.clear()
		}

		bids, asks := c.convertOBUpdates(event.Updates)
		c.book.update(bids, asks)

		if event.Type == "snapshot" {
			c.log.Infof("Book synced")
			c.synced.Store(true)
		}
	}
}

func (c *cbBook) midGap() (uint64, error) {
	if !c.synced.Load() {
		return 0, fmt.Errorf("book not synced")
	}

	return c.book.midGap(), nil
}

func (c *cbBook) vwap(sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	if !c.synced.Load() {
		return 0, 0, false, fmt.Errorf("book not synced")
	}

	vwap, extrema, filled = c.book.vwap(sell, qty)
	return
}

func (c *cbBook) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	setSynced := func(synced bool) {
		c.synced.Store(synced)
	}

	// apiKey and apiName not provided because these subscriptions do not need
	// authentication.
	conn := newCBWSConn("", "", c.wsPath, c.productID, "level2", "l2_data", c.handleLevel2Message, setSynced, c.log)
	wsCM := dex.NewConnectionMaster(conn)
	if err := wsCM.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to websocket feed: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		wsCM.Disconnect()
	}()

	return &wg, nil
}

func (c *cbBook) sync(ctx context.Context) error {
	cm := dex.NewConnectionMaster(c)
	c.mtx.Lock()
	c.cm = cm
	c.numSubscribers++
	c.mtx.Unlock()
	return cm.ConnectOnce(ctx)
}

type coinbase struct {
	log           dex.Logger
	basePath      string
	wsPath        string
	apiName       string
	apiPrivateKey string
	tickerIDs     map[string][]uint32
	idTicker      map[uint32]string
	ctx           context.Context
	net           dex.Network
	accounts      atomic.Value // map[string]*coinbaseAccount
	assets        atomic.Value // map[string]*coinbaseAsset
	markets       atomic.Value // map[string]*coinbaseMarket

	marketSnapshotMtx sync.RWMutex
	marketSnapshot    struct {
		stamp time.Time
		m     map[string]*Market
	}

	// subMarketMtx must be held while subscribing or unsubscribing to a
	// market.
	subMarketMtx sync.Mutex

	booksMtx sync.RWMutex
	books    map[string]*cbBook

	cexUpdatersMtx sync.RWMutex
	cexUpdaters    map[chan interface{}]struct{}

	tradeUpdaterMtx    sync.RWMutex
	tradeInfo          map[string]*tradeInfo
	tradeUpdaters      map[int]chan *Trade
	tradeUpdateCounter int

	tradeIDNonce       atomic.Uint32
	tradeIDNoncePrefix dex.Bytes

	broadcast func(interface{})
}

var _ CEX = (*coinbase)(nil)

func newCoinbase(cfg *CEXConfig) (*coinbase, error) {
	var basePath, wsPath string
	switch cfg.Net {
	case dex.Mainnet:
		basePath = "api.coinbase.com/"
		wsPath = "advanced-trade-ws.coinbase.com/"
	default:
		return nil, fmt.Errorf("coinbase is only supported on mainnet")
	}

	tickerIDs := make(map[string][]uint32)
	idTicker := make(map[uint32]string)

	addTicker := func(assetID uint32, ticker string) {
		if _, found := tickerIDs[ticker]; !found {
			tickerIDs[ticker] = []uint32{assetID}
		} else {
			tickerIDs[ticker] = append(tickerIDs[ticker], assetID)
		}
		idTicker[assetID] = ticker
	}

	for _, a := range asset.Assets() {
		if a.ID != 966 {
			addTicker(a.ID, a.Info.UnitInfo.Conventional.Unit)
		}
		for tokenID, tkn := range a.Tokens {
			if _, supported := supportedCoinbaseTokens[tokenID]; supported {
				addTicker(tokenID, tkn.UnitInfo.Conventional.Unit)
			}
		}
	}

	// Unescape any line breaks in the secret key.
	secretKey := strings.ReplaceAll(cfg.SecretKey, `\n`, "\n")

	return &coinbase{
		net:                cfg.Net,
		log:                cfg.Logger,
		basePath:           basePath,
		wsPath:             wsPath,
		apiName:            cfg.APIKey,
		apiPrivateKey:      secretKey,
		tickerIDs:          tickerIDs,
		idTicker:           idTicker,
		tradeIDNoncePrefix: encode.RandomBytes(10),
		books:              make(map[string]*cbBook),
		cexUpdaters:        make(map[chan interface{}]struct{}),
		tradeInfo:          make(map[string]*tradeInfo),
		tradeUpdaters:      make(map[int]chan *Trade),
		broadcast:          cfg.Notify,
	}, nil
}

func (c *coinbase) handleUserMessage(b []byte) {
	msg := new(cbtypes.UserMessage)
	if err := json.Unmarshal(b, &msg); err != nil {
		c.log.Errorf("Error unmarshaling user message: %v", err)
		return
	}

	updateOrder := func(order *cbtypes.UserMessageOrder) {
		c.tradeUpdaterMtx.RLock()
		defer c.tradeUpdaterMtx.RUnlock()

		info, found := c.tradeInfo[order.OrderID]
		if !found {
			c.log.Errorf("no trade info found for order ID %s", order.OrderID)
			return
		}

		updater, found := c.tradeUpdaters[info.updaterID]
		if !found {
			c.log.Errorf("no trade updater found for trade ID %s", order.OrderID)
			return
		}

		bui, err := asset.UnitInfo(info.baseID)
		if err != nil {
			c.log.Errorf("error getting unit info for asset ID %d: %v", info.baseID, err)
			return
		}

		qui, err := asset.UnitInfo(info.quoteID)
		if err != nil {
			c.log.Errorf("error getting unit info for asset ID %d: %v", info.quoteID, err)
			return
		}

		filledValue := toAtomic(order.FilledValue, &qui)
		totalFees := toAtomic(order.TotalFees, &qui)

		update := &Trade{
			ID:          order.OrderID,
			Qty:         info.qty,
			Sell:        info.sell,
			Rate:        info.rate,
			BaseID:      info.baseID,
			QuoteID:     info.quoteID,
			BaseFilled:  toAtomic(order.CumulativeQty, &bui),
			QuoteFilled: utils.SafeSub(filledValue, totalFees),
			Complete:    order.Status != "OPEN" && order.Status != "PENDING",
		}
		updater <- update
	}

	for _, event := range msg.Events {
		for _, order := range event.Orders {
			updateOrder(order)
		}
	}
}

func (c *coinbase) subscribeUserChannel(ctx context.Context) (*sync.WaitGroup, error) {
	conn := newCBWSConn(c.apiName, c.apiPrivateKey, c.wsPath, "", "user", "user", c.handleUserMessage, func(bool) {}, c.log)
	cm := dex.NewConnectionMaster(conn)
	if err := cm.ConnectOnce(ctx); err != nil {
		return nil, fmt.Errorf("error connecting to websocket feed: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		cm.Disconnect()
	}()

	return &wg, nil
}

func (c *coinbase) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if err := c.updateAccounts(ctx); err != nil {
		return nil, fmt.Errorf("error fetching accounts: %w", err)
	}

	if err := c.updateAssets(ctx); err != nil {
		return nil, fmt.Errorf("error fetching assets: %w", err)
	}

	if _, err := c.updateMarkets(ctx); err != nil {
		return nil, fmt.Errorf("error fetching markets: %w", err)
	}

	c.ctx = ctx

	wg, err := c.subscribeUserChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("error subscribing to user channel: %w", err)
	}

	// Update accounts frequently. This is the only way to know if a balance
	// has been updated. The user channel does not provide balance updates.
	wg.Add(1)
	go func() {
		defer wg.Done()

		timer := time.NewTimer(time.Second * 30)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := c.updateAccounts(ctx); err != nil {
					c.log.Errorf("error fetching accounts: %v", err)
				}
			}
		}
	}()

	// Update assets / markets every 10 minutes. These shouldn't change often.
	wg.Add(1)
	go func() {
		defer wg.Done()

		timer := time.NewTimer(time.Minute * 10)
		defer timer.Stop()

		updateStuff := func() {
			if err := c.updateAssets(ctx); err != nil {
				c.log.Errorf("error fetching assets: %v", err)
			}
			if _, err := c.updateMarkets(ctx); err != nil {
				c.log.Errorf("error fetching markets: %v", err)
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				updateStuff()
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		c.booksMtx.RLock()
		defer c.booksMtx.RUnlock()
		for _, book := range c.books {
			book.cm.Disconnect()
		}
	}()

	return wg, nil
}

// Balance returns the balance of an asset at the CEX.
func (c *coinbase) Balance(assetID uint32) (*ExchangeBalance, error) {
	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for asset ID %d", assetID)
	}
	symbol := dex.BipIDSymbol(assetID)

	ticker, found := c.idTicker[assetID]
	if !found {
		return nil, fmt.Errorf("no known tickers for %s", symbol)
	}

	err = c.updateAccounts(c.ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching accounts: %w", err)
	}

	accounts := c.accounts.Load().(map[string]*cbtypes.Account)
	account, found := accounts[ticker]
	if !found {
		return nil, fmt.Errorf("no account found for %s", symbol)
	}

	return &ExchangeBalance{
		Available: toAtomic(account.AvailableBalance.Value, &ui),
		Locked:    toAtomic(account.Hold.Value, &ui),
	}, nil
}

// Balances returns the balances of all assets at the CEX.
func (c *coinbase) Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error) {
	err := c.updateAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("error fetching accounts: %w", err)
	}

	balances := make(map[uint32]*ExchangeBalance)
	accounts := c.accounts.Load().(map[string]*cbtypes.Account)
	for ticker, account := range accounts {
		assetIDs, found := c.tickerIDs[ticker]
		if !found {
			continue
		}
		for _, assetID := range assetIDs {
			ui, err := asset.UnitInfo(assetID)
			if err != nil {
				return nil, fmt.Errorf("error getting unit info for asset ID %d: %w", assetID, err)
			}
			balances[assetID] = &ExchangeBalance{
				Available: toAtomic(account.AvailableBalance.Value, &ui),
				Locked:    toAtomic(account.Hold.Value, &ui),
			}
		}
	}
	return balances, nil
}

// CancelTrade cancels a trade on the CEX.
func (c *coinbase) CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error {
	req := &cbtypes.CancelMessage{
		OrderIDs: []string{tradeID},
	}
	var res cbtypes.CancelResponse
	if err := c.request(ctx, http.MethodPost, "api/v3/brokerage/orders/batch_cancel", nil, req, &res); err != nil {
		return fmt.Errorf("error cancelling order: %w", err)
	}
	if len(res.Results) != 1 {
		return fmt.Errorf("expected 1 cancellation result, got %d", len(res.Results))
	}
	if !res.Results[0].Success {
		return fmt.Errorf("cancellation failed: %s", res.Results[0].FailureReason)
	}

	return nil
}

// Markets returns the list of markets at the CEX.
func (c *coinbase) Markets(ctx context.Context) (map[string]*Market, error) {
	c.marketSnapshotMtx.RLock()
	const snapshotTimeout = time.Minute * 30
	if c.marketSnapshot.m != nil && time.Since(c.marketSnapshot.stamp) < snapshotTimeout {
		defer c.marketSnapshotMtx.RUnlock()
		return c.marketSnapshot.m, nil
	}
	c.marketSnapshotMtx.RUnlock()

	return c.updateMarkets(ctx)
}

// SubscribeCEXUpdates returns a channel which sends an empty struct when
// the balance of an asset on the CEX has been updated.
func (c *coinbase) SubscribeCEXUpdates() (<-chan interface{}, func()) {
	updater := make(chan interface{}, 128)
	c.cexUpdatersMtx.Lock()
	c.cexUpdaters[updater] = struct{}{}
	c.cexUpdatersMtx.Unlock()

	unsubscribe := func() {
		c.cexUpdatersMtx.Lock()
		delete(c.cexUpdaters, updater)
		c.cexUpdatersMtx.Unlock()
	}

	return updater, unsubscribe
}

func (c *coinbase) marketExists(baseID, quoteID uint32) bool {
	c.marketSnapshotMtx.RLock()
	defer c.marketSnapshotMtx.RUnlock()
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return false
	}
	mkts := c.markets.Load().(map[string]*cbtypes.Market)
	_, found := mkts[productID]
	return found
}

// SubscribeMarket subscribes to order book updates on a market. This must
// be called before calling VWAP or MidGap.
func (c *coinbase) SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error {
	c.subMarketMtx.Lock()
	defer c.subMarketMtx.Unlock()

	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return err
	}

	if !c.marketExists(baseID, quoteID) {
		return fmt.Errorf("no market found for product ID %s", productID)
	}
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("error getting unit info for base asset ID %d: %v", baseID, err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("error getting unit info for quote asset ID %d: %v", quoteID, err)
	}

	c.booksMtx.RLock()
	book, exists := c.books[productID]
	c.booksMtx.RUnlock()
	if exists {
		book.mtx.Lock()
		book.numSubscribers++
		book.mtx.Unlock()
		return nil
	}

	book = newCBBook(c.wsPath, productID, &bui, &qui, c.log)
	err = book.sync(c.ctx)
	if err != nil {
		return fmt.Errorf("error syncing book: %v", err)
	}

	c.booksMtx.Lock()
	c.books[productID] = book
	c.booksMtx.Unlock()

	return nil
}

// SubscribeTradeUpdates returns a channel that the caller can use to
// listen for updates to a trade's status. When the subscription ID
// returned from this function is passed as the updaterID argument to
// Trade, then updates to the trade will be sent on the updated channel
// returned from this function.
func (c *coinbase) SubscribeTradeUpdates() (<-chan *Trade, func(), int) {
	c.tradeUpdaterMtx.Lock()
	defer c.tradeUpdaterMtx.Unlock()

	updaterID := c.tradeUpdateCounter
	c.tradeUpdateCounter++
	updater := make(chan *Trade, 256)
	c.tradeUpdaters[updaterID] = updater

	unsubscribe := func() {
		c.tradeUpdaterMtx.Lock()
		delete(c.tradeUpdaters, updaterID)
		c.tradeUpdaterMtx.Unlock()
	}

	return updater, unsubscribe, updaterID
}

func (bnc *coinbase) generateTradeID() string {
	nonce := bnc.tradeIDNonce.Add(1)
	nonceB := encode.Uint32Bytes(nonce)
	return hex.EncodeToString(append(bnc.tradeIDNoncePrefix, nonceB...))
}

// Trade executes a trade on the CEX. updaterID takes a subscriptionID
// returned from SubscribeTradeUpdates.
func (c *coinbase) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (*Trade, error) {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return nil, fmt.Errorf("error generating product ID: %w", err)
	}

	markets := c.markets.Load().(map[string]*cbtypes.Market)
	mkt, found := markets[productID]
	if !found {
		return nil, fmt.Errorf("no book found for %s", productID)
	}
	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for base asset ID %d: %v", baseID, err)
	}
	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for quote asset ID %d: %v", quoteID, err)
	}
	bFactor := bui.Conventional.ConversionFactor
	qFactor := qui.Conventional.ConversionFactor

	rate = steppedRate(rate, mkt.RateStep)
	convRate := calc.ConventionalRateAlt(rate, bFactor, qFactor)
	ratePrec := int(math.Round(math.Log10(calc.RateEncodingFactor * float64(bFactor) / float64(qFactor) / float64(mkt.RateStep))))
	rateStr := strconv.FormatFloat(convRate, 'f', ratePrec, 64)

	if qty < mkt.MinQty || qty > mkt.MaxQty {
		return nil, fmt.Errorf("quantity %v is out of bounds for market %v", qty, productID)
	}
	steppedQty := steppedRate(qty, mkt.LotSize)
	convQty := float64(steppedQty) / float64(bFactor)
	qtyPrec := int(math.Round(math.Log10(float64(bFactor) / float64(mkt.LotSize))))
	qtyStr := strconv.FormatFloat(convQty, 'f', qtyPrec, 64)

	side := "BUY"
	if sell {
		side = "SELL"
	}

	c.tradeUpdaterMtx.Lock()
	defer c.tradeUpdaterMtx.Unlock()

	_, found = c.tradeUpdaters[subscriptionID]
	if !found {
		return nil, fmt.Errorf("no trade updater found for subscription ID %d", subscriptionID)
	}

	tradeID := c.generateTradeID()

	ord := &cbtypes.OrderRequest{
		ProductID:     productID,
		Side:          side,
		ClientOrderID: tradeID,
	}
	limitConfig := &ord.OrderConfiguration.Limit
	limitConfig.BaseSize = qtyStr
	limitConfig.LimitPrice = rateStr

	var res cbtypes.OrderResponse
	if err := c.request(ctx, http.MethodPost, "api/v3/brokerage/orders", nil, ord, &res); err != nil {
		return nil, fmt.Errorf("error posting order: %w", err)
	}

	if !res.Success {
		e := &res.ErrorResponse
		return nil, fmt.Errorf("error placing order: %s", e.Message)
	}

	c.tradeInfo[res.SuccessResponse.OrderID] = &tradeInfo{
		updaterID: subscriptionID,
		baseID:    baseID,
		quoteID:   quoteID,
		sell:      sell,
		rate:      rate,
		qty:       qty,
	}

	return &Trade{
		ID:      res.SuccessResponse.OrderID,
		Sell:    sell,
		Rate:    rate,
		Qty:     qty,
		BaseID:  baseID,
		QuoteID: quoteID,
	}, nil
}

// UnsubscribeMarket unsubscribes from order book updates on a market.
func (c *coinbase) UnsubscribeMarket(baseID, quoteID uint32) error {
	c.subMarketMtx.Lock()
	defer c.subMarketMtx.Unlock()

	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return fmt.Errorf("new product ID error: %v", err)
	}

	c.booksMtx.RLock()
	book, found := c.books[productID]
	c.booksMtx.RUnlock()
	if !found {
		return fmt.Errorf("no book found for product ID: %v", productID)
	}

	book.mtx.Lock()
	book.numSubscribers--
	book.mtx.Unlock()

	if book.numSubscribers == 0 {
		c.booksMtx.Lock()
		delete(c.books, productID)
		c.booksMtx.Unlock()
		go book.cm.Disconnect()
	}

	return nil
}

func (c *coinbase) book(baseID, quoteID uint32) (*cbBook, error) {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return nil, fmt.Errorf("error generating product ID: %v", err)
	}

	c.booksMtx.RLock()
	book, found := c.books[productID]
	c.booksMtx.RUnlock()
	if !found {
		return nil, fmt.Errorf("no book for market %s", productID)
	}

	return book, nil
}

func (c *coinbase) Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error) {
	book, err := c.book(baseID, quoteID)
	if err != nil {
		return nil, nil, err
	}
	bids, asks := book.book.snap()
	baseFactor := book.bui.Conventional.ConversionFactor
	quoteFactor := book.qui.Conventional.ConversionFactor
	buys = convertSide(bids, false, baseFactor, quoteFactor)
	sells = convertSide(asks, true, baseFactor, quoteFactor)
	return
}

// VWAP returns the volume weighted average price for a certain quantity
// of the base asset on a market.
func (c *coinbase) VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error) {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		return 0, 0, false, fmt.Errorf("error generating product ID: %v", err)
	}

	c.booksMtx.RLock()
	book, found := c.books[productID]
	c.booksMtx.RUnlock()
	if !found {
		return 0, 0, false, fmt.Errorf("no book found for %s", productID)
	}

	return book.vwap(sell, qty)
}

// MidGap returns the mid-gap price for a market.
func (c *coinbase) MidGap(baseID, quoteID uint32) uint64 {
	productID, err := c.newProductID(baseID, quoteID)
	if err != nil {
		c.log.Errorf("error generating product ID: %v", err)
		return 0
	}

	c.booksMtx.RLock()
	book, found := c.books[productID]
	c.booksMtx.RUnlock()
	if !found {
		c.log.Errorf("no book found for %s", productID)
		return 0
	}

	midGap, err := book.midGap()
	if err != nil {
		c.log.Errorf("error getting mid gap: %v", err)
		return 0
	}

	return midGap
}

// GetDepositAddress returns a deposit address for the specified asset.
func (c *coinbase) GetDepositAddress(ctx context.Context, assetID uint32) (string, error) {
	ticker, found := c.idTicker[assetID]
	if !found {
		return "", fmt.Errorf("no ticker found for asset ID %d", assetID)
	}

	acctID, err := c.getAccountID(ctx, ticker)
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("v2/accounts/%s/addresses", acctID)
	var res cbtypes.DepositAddressResponse
	if err := c.request(ctx, http.MethodPost, endpoint, nil, nil, &res); err != nil {
		return "", fmt.Errorf("error fetching deposit address: %w", err)
	}

	return res.Data.Address, nil
}

func (c *coinbase) getTransaction(ctx context.Context, assetID uint32, txID string) (*cbtypes.Transaction, error) {
	ticker, found := c.idTicker[assetID]
	if !found {
		return nil, fmt.Errorf("no ticker found for asset ID %d", assetID)
	}

	acctID, err := c.getAccountID(ctx, ticker)
	if err != nil {
		return nil, err
	}

	endpoint := fmt.Sprintf("v2/accounts/%s/transactions/%s", acctID, txID)
	var res cbtypes.TransactionResponse
	if err := c.request(ctx, http.MethodGet, endpoint, nil, nil, &res); err != nil {
		return nil, fmt.Errorf("error fetching transaction: %w", err)
	}

	return res.Data, nil
}

// getTransactionByTxID returns a transaction by transaction ID, rather than
// the coinbase ID. It will not return transactions older than 3 days.
func (c *coinbase) getTransactionByTxID(ctx context.Context, assetID uint32, txID string) (*cbtypes.Transaction, error) {
	ticker, found := c.idTicker[assetID]
	if !found {
		return nil, fmt.Errorf("no ticker found for asset ID %d", assetID)
	}

	acctID, err := c.getAccountID(ctx, ticker)
	if err != nil {
		return nil, err
	}

	nextURI := fmt.Sprintf("v2/accounts/%s/transactions", acctID)
	for i := 0; i < 100 && nextURI != ""; i++ {
		var res cbtypes.ListTransactionsResponse
		if err := c.request(ctx, http.MethodGet, nextURI, nil, nil, &res); err != nil {
			return nil, fmt.Errorf("error fetching transactions: %w", err)
		}

		for _, tx := range res.Data {
			if tx.Network.Hash == txID {
				return tx, nil
			}
		}

		if len(res.Data) > 0 {
			date := res.Data[len(res.Data)-1].CreatedAt
			if date.Before(time.Now().Add(-time.Hour * 24 * 3)) {
				return nil, fmt.Errorf("transaction %s not found", txID)
			}
		}

		nextURI = res.Pagination.NextURI
		nextURI = strings.TrimPrefix(nextURI, "/")
	}

	return nil, fmt.Errorf("transaction %s not found", txID)
}

// ConfirmDeposit checks if a deposit has been confirmed and returns the
// amount deposited. This will only work for deposits that have been
// in the past 3 days.
func (c *coinbase) ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64) {
	tx, err := c.getTransactionByTxID(ctx, deposit.AssetID, deposit.TxID)
	if err != nil {
		c.log.Errorf("error fetching transaction: %v", err)
		return false, 0
	}

	if tx.Status != "completed" {
		return false, 0
	}

	ui, err := asset.UnitInfo(deposit.AssetID)
	if err != nil {
		c.log.Errorf("error getting unit info for asset ID %d: %v", deposit.AssetID, err)
		return false, 0
	}

	return true, toAtomic(tx.Amount.Amount, &ui)
}

// Withdraw sends funds to the specified address. Coinbase adds the fee on top
// of the amount, so more than the amount specified will be deducted from the
// balance.
func (c *coinbase) Withdraw(ctx context.Context, assetID uint32, amt uint64, address string) (string, uint64, error) {
	ticker, found := c.idTicker[assetID]
	if !found {
		return "", 0, fmt.Errorf("no ticker found for asset ID %d", assetID)
	}

	acctID, err := c.getAccountID(ctx, ticker)
	if err != nil {
		return "", 0, err
	}

	assets := c.assets.Load().(map[string]*coinbaseAsset)
	cbAsset, found := assets[ticker]
	if !found {
		return "", 0, fmt.Errorf("asset %s not found", ticker)
	}

	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return "", 0, fmt.Errorf("error getting unit info for asset ID %d: %v", assetID, err)
	}

	req := cbtypes.SendTransactionRequest{
		Type:     "send",
		To:       address,
		Amount:   strconv.FormatFloat(toConv(amt, &ui), 'f', cbAsset.Exponent, 64),
		Currency: cbAsset.Code,
	}

	if token := asset.TokenInfo(assetID); token != nil {
		if token.ParentID == 60 {
			req.Network = "ethereum"
		} else if token.ParentID == 966 {
			req.Network = "polygon"
		} else {
			return "", 0, fmt.Errorf("unsupported network token: %s", token.Name)
		}
	}
	endpoint := fmt.Sprintf("v2/accounts/%s/transactions", acctID)
	var res cbtypes.TransactionResponse
	if err := c.request(ctx, http.MethodPost, endpoint, nil, req, &res); err != nil {
		return "", 0, fmt.Errorf("error withdrawing: %w", err)
	}

	amount := res.Data.Amount.Amount
	if amount < 0 {
		amount = -amount
	}

	return res.Data.ID, toAtomic(amount, &ui), nil
}

// ConfirmWithdrawal checks if a withdrawal has been confirmed and returns the
// on chain transaction ID and the amount withdrawn.
func (c *coinbase) ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error) {
	tx, err := c.getTransaction(ctx, assetID, withdrawalID)
	if err != nil {
		return 0, "", fmt.Errorf("error fetching transaction: %w", err)
	}

	if tx.Network.Hash == "" {
		return 0, "", ErrWithdrawalPending
	}

	ui, err := asset.UnitInfo(assetID)
	if err != nil {
		return 0, "", fmt.Errorf("error getting unit info for asset ID %d: %v", assetID, err)
	}

	amount := tx.Amount.Amount
	if amount < 0 {
		amount = -amount
	}

	return toAtomic(amount, &ui), tx.Network.Hash, nil
}

// TradeStatus returns the status of a trade.
func (c *coinbase) TradeStatus(ctx context.Context, id string, baseID, quoteID uint32) (*Trade, error) {
	endpoint := fmt.Sprintf("api/v3/brokerage/orders/historical/%s", id)
	var res cbtypes.TradeStatusResponse
	if err := c.request(ctx, http.MethodGet, endpoint, nil, nil, &res); err != nil {
		return nil, fmt.Errorf("error fetching order status: %w", err)
	}

	bui, err := asset.UnitInfo(baseID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for base asset ID %d: %v", baseID, err)
	}

	qui, err := asset.UnitInfo(quoteID)
	if err != nil {
		return nil, fmt.Errorf("error getting unit info for quote asset ID %d: %v", quoteID, err)
	}

	return &Trade{
		ID:          id,
		Sell:        res.Order.Side == "SELL",
		Qty:         toAtomic(res.Order.Config.LimitGTC.BaseSize, &bui),
		Rate:        messageRate(res.Order.Config.LimitGTC.LimitPrice, &bui, &qui),
		BaseID:      baseID,
		QuoteID:     quoteID,
		BaseFilled:  toAtomic(res.Order.FilledSize, &bui),
		QuoteFilled: toAtomic(res.Order.TotalValueAfterFees, &qui),
		Complete:    res.Order.Status != "OPEN" && res.Order.Status != "PENDING",
	}, nil
}

type coinbaseWebsocketSubscription struct {
	Type       string   `json:"type"`
	ProductIDs []string `json:"product_ids"`
	Channel    string   `json:"channel"`
	JWT        string   `json:"jwt"`
}

func (c *coinbase) newProductID(baseID, quoteID uint32) (string, error) {
	baseTicker, found := c.idTicker[baseID]
	if !found {
		return "", fmt.Errorf("ticker not found for base asset ID %d", baseID)
	}
	quoteTicker, found := c.idTicker[quoteID]
	if !found {
		return "", fmt.Errorf("ticker not found for quote asset ID %d", baseID)
	}

	return baseTicker + "-" + quoteTicker, nil
}

type cbErrorResp struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func isSuccessfulStatusCode(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed
}

func (c *coinbase) request(ctx context.Context, method, endpoint string, queryParams url.Values, bodyParams, res interface{}) error {
	req, cancel, err := c.prepareRequest(ctx, method, endpoint, queryParams, bodyParams)
	if err != nil {
		return fmt.Errorf("prepareRequest error: %v", err)
	}
	defer cancel()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error requesting %q: %v", req.URL, err)
	}
	defer resp.Body.Close()

	if !isSuccessfulStatusCode(resp) {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading response body: %v", err)
		}
		if len(b) < 14 && strings.TrimSpace(string(b)) == "Unauthorized" {
			return fmt.Errorf("unauthorized")
		}
		var e cbErrorResp
		if err := json.Unmarshal(b, &e); err != nil {
			return fmt.Errorf("error unmarshaling error response: %s", string(b))
		}
		if len(e.Errors) > 0 {
			return fmt.Errorf("error response (%d): %s", resp.StatusCode, e.Errors[0].Message)
		}
		return fmt.Errorf("error response (%d): %s", resp.StatusCode, string(b))
	}

	if res != nil {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading response body: %v", err)
		}

		if err := json.Unmarshal(b, res); err != nil {
			return fmt.Errorf("error unmarshaling response: %v", err)
		}
	}

	return nil
}

var max = big.NewInt(math.MaxInt64)

type nonceSource struct{}

func (n nonceSource) Nonce() (string, error) {
	r, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return r.String(), nil
}

type APIKeyClaims struct {
	*jwt.Claims
	URI string `json:"uri,omitempty"`
}

func buildJWT(apiName, apiPrivateKey, uri string) (string, error) {
	block, _ := pem.Decode([]byte(apiPrivateKey))
	if block == nil {
		return "", fmt.Errorf("jwt: Could not decode private key")
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("jwt: %w", err)
	}

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{NonceSource: nonceSource{}}).WithType("JWT").WithHeader("kid", apiName),
	)
	if err != nil {
		return "", fmt.Errorf("jwt: %w", err)
	}

	cl := &APIKeyClaims{
		Claims: &jwt.Claims{
			Subject:   apiName,
			Issuer:    "coinbase-cloud",
			NotBefore: jwt.NewNumericDate(time.Now()),
			Expiry:    jwt.NewNumericDate(time.Now().Add(time.Minute * 2)),
		},
		URI: uri,
	}
	jwtString, err := jwt.Signed(sig).Claims(cl).CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("jwt: %w", err)
	}
	return jwtString, nil
}

func (c *coinbase) sign(preimage string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(c.apiPrivateKey)
	if err != nil {
		return "", err
	}

	signature := hmac.New(sha256.New, key)
	_, err = signature.Write([]byte(preimage))
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (c *coinbase) prepareRequest(ctx context.Context, method, endpoint string, queryParams url.Values, bodyParams interface{}) (_ *http.Request, _ context.CancelFunc, err error) {
	var body []byte
	if bodyParams != nil {
		body, err = json.Marshal(bodyParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error marshaling request: %w", err)
		}
	}

	fullUrl := fmt.Sprintf("https://%s%s", c.basePath, endpoint)
	if queryParams != nil {
		fullUrl += "?" + queryParams.Encode()
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	req, err := http.NewRequestWithContext(ctx, method, fullUrl, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("error generating http request: %w", err)
	}

	uri := fmt.Sprintf("%s %s%s", method, c.basePath, endpoint)
	jwt, err := buildJWT(c.apiName, c.apiPrivateKey, uri)
	if err != nil {
		return nil, nil, fmt.Errorf("error building JWT: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("CB-VERSION", "2024-05-27")

	return req, cancel, nil
}

func (c *coinbase) updateAccounts(ctx context.Context) error {
	accounts := make(map[string]*cbtypes.Account)

	var cursor string
	hasNext := true
	// The accounts are paginated. If there are more than 250 accounts, we need to
	// fetch additional.
	for i := 0; i < 20 && hasNext; i++ {
		v := make(url.Values)
		if cursor != "" {
			v.Add("cursor", cursor)
		}
		v.Add("limit", "250")

		var res cbtypes.AccountsResult
		if err := c.request(ctx, http.MethodGet, "api/v3/brokerage/accounts", v, nil, &res); err != nil {
			return err
		}

		for _, a := range res.Accounts {
			if _, supported := c.tickerIDs[a.Currency]; supported {
				accounts[a.Currency] = a
			}
		}

		cursor = res.Cursor
		hasNext = res.HasNext
	}
	if hasNext {
		return fmt.Errorf("failed to fetch all accounts, cursor: %s", cursor)
	}

	if oldAccounts := c.accounts.Load(); oldAccounts != nil {
		oldAccounts := oldAccounts.(map[string]*cbtypes.Account)
		for k, v := range accounts {
			old, exists := oldAccounts[k]
			if exists && old.AvailableBalance.Value == v.AvailableBalance.Value && old.Hold.Value == v.Hold.Value {
				// No change.
				continue
			}

			ids := c.tickerIDs[k]
			if len(ids) == 0 {
				continue
			}

			ui, err := asset.UnitInfo(ids[0])
			if err != nil {
				c.log.Errorf("error getting unit info for asset ID %d: %v", ids[0], err)
				continue
			}

			for _, id := range ids {
				c.broadcast(&BalanceUpdate{
					AssetID: id,
					Balance: &ExchangeBalance{
						Available: toAtomic(v.AvailableBalance.Value, &ui),
						Locked:    toAtomic(v.Hold.Value, &ui),
					},
				})
			}
		}
	}

	c.accounts.Store(accounts)

	return nil
}

type coinbaseAsset struct {
	Code     string `json:"code"`
	Exponent int    `json:"exponent"`
}

type coinbaseAssetResult struct {
	Data []*coinbaseAsset `json:"data"`
}

func (c *coinbase) updateAssets(ctx context.Context) error {
	var res coinbaseAssetResult
	if err := c.request(ctx, http.MethodGet, "v2/currencies/crypto", nil, nil, &res); err != nil {
		return err
	}

	assets := make(map[string]*coinbaseAsset, len(res.Data))

	for _, a := range res.Data {
		if _, supported := c.tickerIDs[a.Code]; supported {
			assets[a.Code] = a
		}
	}

	c.assets.Store(assets)

	return nil
}

func (c *coinbase) getAccountID(ctx context.Context, ticker string) (string, error) {
	accounts := c.accounts.Load().(map[string]*cbtypes.Account)
	a, found := accounts[ticker]
	if found {
		return a.UUID, nil
	}

	// The account may have been added after the last time the accounts
	// were loaded.
	err := c.updateAccounts(ctx)
	if err != nil {
		return "", fmt.Errorf("error updating accounts: %w", err)
	}

	accounts = c.accounts.Load().(map[string]*cbtypes.Account)
	a, found = accounts[ticker]
	if !found {
		return "", fmt.Errorf("no account found for ticker %s", ticker)
	}

	return a.UUID, nil
}

func parseFloat(v string) float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

func parsePercent(v string) float64 {
	return parseFloat(strings.TrimSuffix(v, "%"))
}

func (c *coinbase) coinbaseMarketToDexMarkets(baseTicker, quoteTicker string) [][2]uint32 {
	baseIDs := c.tickerIDs[baseTicker]
	quoteIDs := c.tickerIDs[quoteTicker]

	if len(baseIDs) == 0 || len(quoteIDs) == 0 {
		return nil
	}

	markets := make([][2]uint32, 0, len(baseIDs)*len(quoteIDs))
	for _, baseID := range baseIDs {
		for _, quoteID := range quoteIDs {
			markets = append(markets, [2]uint32{baseID, quoteID})
		}
	}

	return markets
}

func (c *coinbase) updateMarkets(ctx context.Context) (map[string]*Market, error) {
	var res cbtypes.ProductsResult
	const endpoint = "api/v3/brokerage/products"
	queryParams := make(url.Values)
	queryParams.Add("product_type", "SPOT")
	queryParams.Add("get_tradability_status", "true")
	if err := c.request(ctx, http.MethodGet, endpoint, queryParams, nil, &res); err != nil {
		return nil, err
	}

	markets := make(map[string]*Market, len(res.Products))
	cbMarkets := make(map[string]*cbtypes.Market, len(res.Products))

	for _, mkt := range res.Products {
		if mkt.ViewOnly || mkt.IsDisabled {
			// TODO: disabled should be shown?
			continue
		}

		dexMarkets := c.coinbaseMarketToDexMarkets(mkt.BaseCurrencyID, mkt.QuoteCurrencyID)
		if len(dexMarkets) == 0 {
			continue
		}

		baseAssetID := dexMarkets[0][0]
		quoteAssetID := dexMarkets[0][1]
		bui, _ := asset.UnitInfo(baseAssetID)
		qui, _ := asset.UnitInfo(quoteAssetID)

		mkt.LotSize = uint64(math.Round(parseFloat(mkt.BaseIncrement) * float64(bui.Conventional.ConversionFactor)))
		mkt.MaxQty = uint64(math.Round(parseFloat(mkt.BaseMaxSize) * float64(bui.Conventional.ConversionFactor)))
		mkt.MinQty = uint64(math.Round(parseFloat(mkt.BaseMinSize) * float64(bui.Conventional.ConversionFactor)))
		conv := float64(qui.Conventional.ConversionFactor) / float64(bui.Conventional.ConversionFactor) * calc.RateEncodingFactor
		mkt.RateStep = uint64(math.Round(parseFloat(mkt.PriceIncrement) * conv))
		cbMarkets[mkt.ProductID] = mkt

		pctPriceChange := parsePercent(mkt.DayPriceChangePctStr)
		var priceChange float64
		if pctPriceChange == -100 {
			// Just in case to avoid division by zero.
			priceChange = 0
		} else {
			price := parseFloat(mkt.Price)
			priceChange = price - price/(1+pctPriceChange/100)
		}
		day := &MarketDay{
			Vol:            parseFloat(mkt.Volume),
			QuoteVol:       parseFloat(mkt.Price) * parseFloat(mkt.Volume),
			PriceChange:    priceChange,
			PriceChangePct: pctPriceChange,
			LastPrice:      parseFloat(mkt.Price),
			// Coinbase does not provide average and high/low prices.
		}

		for _, m := range dexMarkets {
			dexMarketSlug := dex.BipIDSymbol(m[0]) + "_" + dex.BipIDSymbol(m[1])
			markets[dexMarketSlug] = &Market{
				BaseID:  m[0],
				QuoteID: m[1],
				Day:     day,
				// TODO: add min withdraw amounts. Looks like there are none, but need at least
				// enough to cover fees.
			}
		}
	}

	c.markets.Store(cbMarkets)

	c.marketSnapshotMtx.Lock()
	defer c.marketSnapshotMtx.Unlock()
	c.marketSnapshot.m = markets
	c.marketSnapshot.stamp = time.Now()
	return markets, nil
}

func toAtomicFromString(v string, ui *dex.UnitInfo) uint64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return toAtomic(f, ui)
}

func toAtomic(v float64, ui *dex.UnitInfo) uint64 {
	return uint64(math.Round(v * float64(ui.Conventional.ConversionFactor)))
}

func toConv(v uint64, ui *dex.UnitInfo) float64 {
	return float64(v) / float64(ui.Conventional.ConversionFactor)
}

func messageRate(conventionalRate float64, bui, qui *dex.UnitInfo) uint64 {
	return calc.MessageRateAlt(conventionalRate, bui.Conventional.ConversionFactor, qui.Conventional.ConversionFactor)
}
