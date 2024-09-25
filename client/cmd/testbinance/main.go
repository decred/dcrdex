package main

/*
 * Starts an http server that responds to some of the binance api's endpoints.
 * The "runserver" command starts the server, and other commands are used to
 * update the server's state.
 */

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/mm/binance/bntypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/utils"
	"decred.org/dcrdex/dex/ws"
	"decred.org/dcrdex/server/comms"
	"github.com/go-chi/chi/v5"
)

const (
	pongWait     = 60 * time.Second
	pingPeriod   = (pongWait * 9) / 10
	depositConfs = 3

	// maxWalkingSpeed is that maximum amount the mid-gap can change per shuffle.
	// Default about 3% of the basis price, but can be scaled by walkingspeed
	// flag. The actual mid-gap shift during a shuffle is randomized in the
	// range [0, defaultWalkingSpeed*walkingSpeedAdj].
	defaultWalkingSpeed = 0.03
)

var (
	log dex.Logger

	walkingSpeedAdj float64
	gapRange        float64

	xcInfo = &bntypes.ExchangeInfo{
		Timezone:   "UTC",
		ServerTime: time.Now().Unix(),
		RateLimits: []*bntypes.RateLimit{},
		Symbols: []*bntypes.Market{
			makeMarket("dcr", "btc"),
			makeMarket("eth", "btc"),
			makeMarket("dcr", "usdc"),
			makeMarket("zec", "btc"),
		},
	}

	coinInfos = []*bntypes.CoinInfo{
		makeCoinInfo("BTC", "BTC", true, true, 0.00000610, 0.0007),
		makeCoinInfo("ETH", "ETH", true, true, 0.00035, 0.008),
		makeCoinInfo("DCR", "DCR", true, true, 0.00001000, 0.05),
		makeCoinInfo("USDC", "MATIC", true, true, 0.01000, 10),
		makeCoinInfo("ZEC", "ZEC", true, true, 0.00500000, 0.01000000),
	}

	coinpapAssets = []*fiatrates.CoinpaprikaAsset{
		makeCoinpapAsset(0, "btc", "Bitcoin"),
		makeCoinpapAsset(42, "dcr", "Decred"),
		makeCoinpapAsset(60, "eth", "Ethereum"),
		makeCoinpapAsset(966001, "usdc.polygon", "USDC"),
		makeCoinpapAsset(133, "zec", "Zcash"),
	}

	initialBalances = []*bntypes.Balance{
		makeBalance("btc", 1.5),
		makeBalance("dcr", 10000),
		makeBalance("eth", 5),
		makeBalance("usdc", 1152),
		makeBalance("zec", 10000),
	}
)

func parseAssetID(asset string) uint32 {
	symbol := strings.ToLower(asset)
	switch symbol {
	case "usdc":
		symbol = "usdc.polygon"
	}
	assetID, _ := dex.BipSymbolID(symbol)
	return assetID
}

func makeMarket(baseSymbol, quoteSymbol string) *bntypes.Market {
	baseSymbol, quoteSymbol = strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol)
	return &bntypes.Market{
		Symbol:              baseSymbol + quoteSymbol,
		Status:              "TRADING",
		BaseAsset:           baseSymbol,
		BaseAssetPrecision:  8,
		QuoteAsset:          quoteSymbol,
		QuoteAssetPrecision: 8,
		OrderTypes: []string{
			"LIMIT",
			"LIMIT_MAKER",
			"MARKET",
			"STOP_LOSS",
			"STOP_LOSS_LIMIT",
			"TAKE_PROFIT",
			"TAKE_PROFIT_LIMIT",
		},
	}
}

func makeBalance(symbol string, bal float64) *bntypes.Balance {
	return &bntypes.Balance{
		Asset: strings.ToUpper(symbol),
		Free:  bal,
	}
}

func makeCoinInfo(coin, network string, withdrawsEnabled, depositsEnabled bool, withdrawFee, withdrawMin float64) *bntypes.CoinInfo {
	return &bntypes.CoinInfo{
		Coin: coin,
		NetworkList: []*bntypes.NetworkInfo{{
			Coin:           coin,
			Network:        network,
			WithdrawEnable: withdrawsEnabled,
			DepositEnable:  depositsEnabled,
			WithdrawFee:    withdrawFee,
			WithdrawMin:    withdrawMin,
		}},
	}
}

func makeCoinpapAsset(assetID uint32, symbol, name string) *fiatrates.CoinpaprikaAsset {
	return &fiatrates.CoinpaprikaAsset{
		AssetID: assetID,
		Symbol:  symbol,
		Name:    name,
	}
}

func main() {
	var logDebug, logTrace bool
	flag.Float64Var(&walkingSpeedAdj, "walkspeed", 1.0, "scale the maximum walking speed. default scale of 1.0 is about 3%")
	flag.Float64Var(&gapRange, "gaprange", 0.04, "a ratio of how much the gap can vary. default is 0.04 => 4%")
	flag.BoolVar(&logDebug, "debug", false, "use debug logging")
	flag.BoolVar(&logTrace, "trace", false, "use trace logging")
	flag.Parse()

	switch {
	case logTrace:
		log = dex.StdOutLogger("TB", dex.LevelTrace)
		comms.UseLogger(dex.StdOutLogger("C", dex.LevelTrace))
	case logDebug:
		log = dex.StdOutLogger("TB", dex.LevelDebug)
		comms.UseLogger(dex.StdOutLogger("C", dex.LevelDebug))
	default:
		log = dex.StdOutLogger("TB", dex.LevelInfo)
		comms.UseLogger(dex.StdOutLogger("C", dex.LevelInfo))
	}

	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	if walkingSpeedAdj > 10 {
		return fmt.Errorf("invalid walkspeed must be in < 10")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		log.Info("Shutting down...")
		cancel()
	}()

	bnc, err := newFakeBinanceServer(ctx)
	if err != nil {
		return err
	}

	bnc.run(ctx)

	return nil
}

type withdrawal struct {
	amt     float64
	txID    atomic.Value // string
	coin    string
	network string
	address string
	apiKey  string
}

type marketSubscriber struct {
	*ws.WSLink

	// markets is protected by the fakeBinance.marketsMtx.
	markets map[string]struct{}
}

type userOrder struct {
	slug   string
	sell   bool
	rate   float64
	qty    float64
	apiKey string
	stamp  time.Time
	status string
}

type fakeBinance struct {
	ctx       context.Context
	srv       *comms.Server
	fiatRates map[uint32]float64

	withdrawalHistoryMtx sync.RWMutex
	withdrawalHistory    map[string]*withdrawal

	balancesMtx sync.RWMutex
	balances    map[string]*bntypes.Balance

	accountSubscribersMtx sync.RWMutex
	accountSubscribers    map[string]*ws.WSLink

	marketsMtx        sync.RWMutex
	markets           map[string]*market
	marketSubscribers map[string]*marketSubscriber

	walletMtx sync.RWMutex
	wallets   map[string]Wallet

	bookedOrdersMtx sync.RWMutex
	bookedOrders    map[string]*userOrder
}

func newFakeBinanceServer(ctx context.Context) (*fakeBinance, error) {
	log.Trace("Fetching coinpaprika prices")
	fiatRates := fiatrates.FetchCoinpaprikaRates(ctx, coinpapAssets, dex.StdOutLogger("CP", dex.LevelDebug))
	if len(fiatRates) < len(coinpapAssets) {
		return nil, fmt.Errorf("not enough coinpap assets. wanted %d, got %d", len(coinpapAssets), len(fiatRates))
	}

	srv, err := comms.NewServer(&comms.RPCConfig{
		ListenAddrs: []string{":37346"},
		NoTLS:       true,
	})
	if err != nil {
		return nil, fmt.Errorf("Error creating server: %w", err)
	}

	balances := make(map[string]*bntypes.Balance, len(initialBalances))
	for _, bal := range initialBalances {
		balances[bal.Asset] = bal
	}

	f := &fakeBinance{
		ctx:                ctx,
		srv:                srv,
		withdrawalHistory:  make(map[string]*withdrawal, 0),
		balances:           balances,
		accountSubscribers: make(map[string]*ws.WSLink),
		wallets:            make(map[string]Wallet),
		fiatRates:          fiatRates,
		markets:            make(map[string]*market),
		marketSubscribers:  make(map[string]*marketSubscriber),
		bookedOrders:       make(map[string]*userOrder),
	}

	mux := srv.Mux()

	mux.Route("/sapi/v1/capital", func(r chi.Router) {
		r.Get("/config/getall", f.handleWalletCoinsReq)
		r.Get("/deposit/hisrec", f.handleConfirmDeposit)
		r.Get("/deposit/address", f.handleGetDepositAddress)
		r.Post("/withdraw/apply", f.handleWithdrawal)
		r.Get("/withdraw/history", f.handleWithdrawalHistory)

	})
	mux.Route("/api/v3", func(r chi.Router) {
		r.Get("/exchangeInfo", f.handleExchangeInfo)
		r.Get("/account", f.handleAccount)
		r.Get("/depth", f.handleDepth)
		r.Get("/order", f.handleGetOrder)
		r.Post("/order", f.handlePostOrder)
		r.Post("/userDataStream", f.handleListenKeyRequest)
		r.Put("/userDataStream", f.streamExtend)
		r.Delete("/order", f.handleDeleteOrder)
		r.Get("/ticker/24hr", f.handleMarketTicker24)
	})

	mux.Get("/ws/{listenKey}", f.handleAccountSubscription)
	mux.Get("/stream", f.handleMarketStream)

	return f, nil
}

func (f *fakeBinance) run(ctx context.Context) {
	// Start a ticker to do book shuffles.

	go func() {
		runMarketTick := func() {
			f.marketsMtx.RLock()
			defer f.marketsMtx.RUnlock()
			updates := make(map[string]json.RawMessage)
			for mktID, mkt := range f.markets {
				mkt.bookMtx.Lock()
				buys, sells := mkt.shuffle()
				firstUpdateID := mkt.updateID + 1
				mkt.updateID += uint64(len(buys) + len(sells))
				update, _ := json.Marshal(&bntypes.BookNote{
					StreamName: mktID + "@depth",
					Data: &bntypes.BookUpdate{
						Bids:          buys,
						Asks:          sells,
						FirstUpdateID: firstUpdateID,
						LastUpdateID:  mkt.updateID,
					},
				})
				updates[mktID] = update
				mkt.bookMtx.Unlock()
			}

			if len(f.marketSubscribers) > 0 {
				log.Tracef("Sending %d market updates to %d subscribers", len(updates), len(f.marketSubscribers))
			}
			for _, sub := range f.marketSubscribers {
				for symbol := range updates {
					if _, found := sub.markets[symbol]; found {
						sub.SendRaw(updates[symbol])
					}
				}
			}
		}
		const marketMinTick, marketTickRange = time.Second * 5, time.Second * 25
		for {
			delay := marketMinTick + time.Duration(rand.Float64()*float64(marketTickRange))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
			runMarketTick()
		}
	}()

	// Start a ticker to fill booked orders
	go func() {
		// 50% chance of filling all booked orders every 5 to 30 seconds.
		const minFillTick, fillTickRange = 5 * time.Second, 25 * time.Second
		for {
			select {
			case <-time.After(minFillTick + time.Duration(rand.Float64()*float64(fillTickRange))):
			case <-ctx.Done():
				return
			}
			if rand.Float32() < 0.5 {
				continue
			}
			type filledOrder struct {
				bntypes.StreamUpdate
				apiKey string
			}

			f.bookedOrdersMtx.Lock()
			fills := make([]*filledOrder, 0)
			for tradeID, ord := range f.bookedOrders {
				if ord.status != "NEW" {
					if time.Since(ord.stamp) > time.Hour {
						delete(f.bookedOrders, tradeID)
					}
					continue
				}

				ord.status = "FILLED"
				fills = append(fills, &filledOrder{
					StreamUpdate: bntypes.StreamUpdate{
						EventType:          "executionReport",
						CurrentOrderStatus: "FILLED",
						// CancelledOrderID
						ClientOrderID: tradeID,
						Filled:        ord.qty,
						QuoteFilled:   ord.qty * ord.rate,
					},
					apiKey: ord.apiKey,
				})
				f.updateOrderBalances(ord.slug, ord.sell, ord.qty, ord.rate, updateTypeFillBooked)
			}
			f.bookedOrdersMtx.Unlock()
			if len(fills) > 0 {
				log.Tracef("Filling %d booked user orders", len(fills))
			}
			for _, ord := range fills {
				f.accountSubscribersMtx.RLock()
				sub, found := f.accountSubscribers[ord.apiKey]
				f.accountSubscribersMtx.RUnlock()
				if !found {
					continue
				}
				respB, _ := json.Marshal(ord)
				sub.SendRaw(respB)
			}
		}
	}()

	// Start a ticker to complete withdrawals.
	go func() {
		for {
			tick := time.After(time.Second * 30)
			select {
			case <-tick:
			case <-ctx.Done():
				return
			}
			f.withdrawalHistoryMtx.Lock()
			for transferID, withdraw := range f.withdrawalHistory {
				if withdraw.txID.Load() != nil {
					continue
				}
				wallet, err := f.getWallet(withdraw.network)
				if err != nil {
					log.Errorf("No wallet for withdraw coin %s", withdraw.coin)
					delete(f.withdrawalHistory, transferID)
					continue
				}
				txID, err := wallet.Send(ctx, withdraw.address, withdraw.coin, withdraw.amt)
				if err != nil {
					log.Errorf("Error sending %s: %v", withdraw.coin, err)
					delete(f.withdrawalHistory, transferID)
					continue
				}
				log.Debug("Sent withdraw of %.8f to user %s, coin = %s, txid = %s", withdraw.amt, withdraw.apiKey, withdraw.coin, txID)
				withdraw.txID.Store(txID)
			}
			f.withdrawalHistoryMtx.Unlock()
		}
	}()

	f.srv.Run(ctx)
}

func (f *fakeBinance) newWSLink(w http.ResponseWriter, r *http.Request, handler func([]byte)) (_ *ws.WSLink, _ *dex.ConnectionMaster) {
	wsConn, err := ws.NewConnection(w, r, pongWait)
	if err != nil {
		log.Errorf("ws.NewConnection error: %v", err)
		http.Error(w, "error initializing connection", http.StatusInternalServerError)
		return
	}

	ip := dex.NewIPKey(r.RemoteAddr)

	conn := ws.NewWSLink(ip.String(), wsConn, pingPeriod, func(msg *msgjson.Message) *msgjson.Error {
		return nil
	}, dex.StdOutLogger(fmt.Sprintf("CL[%s]", ip), dex.LevelDebug))
	conn.RawHandler = handler

	cm := dex.NewConnectionMaster(conn)
	if err = cm.ConnectOnce(f.ctx); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	return conn, cm
}

func (f *fakeBinance) handleAccountSubscription(w http.ResponseWriter, r *http.Request) {
	apiKey := extractAPIKey(r)

	conn, cm := f.newWSLink(w, r, func(b []byte) {
		log.Errorf("Message received from api key %s over account update channel: %s", apiKey, string(b))
	})
	if conn == nil { // Already logged.
		return
	}

	log.Tracef("User subscribed to account stream with API key %s", apiKey)

	f.accountSubscribersMtx.Lock()
	f.accountSubscribers[apiKey] = conn
	f.accountSubscribersMtx.Unlock()

	go func() {
		cm.Wait()
		f.accountSubscribersMtx.Lock()
		delete(f.accountSubscribers, apiKey)
		f.accountSubscribersMtx.Unlock()
		log.Tracef("Account stream connection ended for API key %s", apiKey)
	}()
}

func (f *fakeBinance) handleMarketStream(w http.ResponseWriter, r *http.Request) {
	streamsStr := r.URL.Query().Get("streams")
	if streamsStr == "" {
		log.Error("Client connected to market stream without providing a 'streams' query parameter")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	rawStreams := strings.Split(streamsStr, "/")
	marketIDs := make(map[string]struct{}, len(rawStreams))
	for _, raw := range rawStreams {
		parts := strings.Split(raw, "@")
		if len(parts) < 2 {
			http.Error(w, fmt.Sprintf("stream encoding incorrect %q", raw), http.StatusBadRequest)
			return
		}
		marketIDs[strings.ToUpper(parts[0])] = struct{}{}
	}

	cl := &marketSubscriber{
		markets: marketIDs,
	}

	subscribe := func(streamIDs []string) {
		f.marketsMtx.Lock()
		defer f.marketsMtx.Unlock()
		for _, streamID := range streamIDs {
			parts := strings.Split(streamID, "@")
			if len(parts) < 2 {
				log.Errorf("SUBSCRIBE stream encoding incorrect: %q", streamID)
				return
			}
			cl.markets[strings.ToUpper(parts[0])] = struct{}{}
		}
	}

	unsubscribe := func(streamIDs []string) {
		f.marketsMtx.Lock()
		defer f.marketsMtx.Unlock()
		for _, streamID := range streamIDs {
			parts := strings.Split(streamID, "@")
			if len(parts) < 2 {
				log.Errorf("UNSUBSCRIBE stream encoding incorrect: %q", streamID)
				return
			}
			delete(cl.markets, strings.ToUpper(parts[0]))
		}
		f.cleanMarkets()
	}

	conn, cm := f.newWSLink(w, r, func(b []byte) {
		var req bntypes.StreamSubscription
		if err := json.Unmarshal(b, &req); err != nil {
			log.Errorf("Error unmarshalling markets stream message: %v", err)
			return
		}
		switch req.Method {
		case "SUBSCRIBE":
			subscribe(req.Params)
		case "UNSUBSCRIBE":
			unsubscribe(req.Params)
		}
	})
	if conn == nil {
		return
	}

	cl.WSLink = conn

	addr := conn.Addr()
	log.Tracef("Websocket client %s connected to market stream for markets %+v", addr, marketIDs)

	f.marketsMtx.Lock()
	f.marketSubscribers[addr] = cl
	f.marketsMtx.Unlock()

	go func() {
		cm.Wait()
		log.Tracef("Market stream client %s disconnected", addr)
		f.marketsMtx.Lock()
		delete(f.marketSubscribers, addr)
		f.cleanMarkets()
		f.marketsMtx.Unlock()
	}()
}

// Call with f.marketsMtx locked
func (f *fakeBinance) cleanMarkets() {
	marketSubCount := make(map[string]int)
	for _, cl := range f.marketSubscribers {
		for mktID := range cl.markets {
			marketSubCount[mktID]++
		}
	}
	for mktID := range f.markets {
		if marketSubCount[mktID] == 0 {
			delete(f.markets, mktID)
		}
	}
}

func (f *fakeBinance) handleWalletCoinsReq(w http.ResponseWriter, r *http.Request) {
	respB, _ := json.Marshal(coinInfos)
	writeBytesWithStatus(w, respB, http.StatusOK)
}

func (f *fakeBinance) sendBalanceUpdates(bals []*bntypes.WSBalance) {
	update := &bntypes.StreamUpdate{
		EventType: "outboundAccountPosition",
		Balances:  bals,
	}
	updateB, _ := json.Marshal(update)
	f.accountSubscribersMtx.Lock()
	if len(f.accountSubscribers) > 0 {
		log.Tracef("Sending balance updates to %d subscribers", len(f.accountSubscribers))
	}
	for _, sub := range f.accountSubscribers {
		sub.SendRaw(updateB)
	}
	f.accountSubscribersMtx.Unlock()
}

func (f *fakeBinance) handleConfirmDeposit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	txID := q.Get("txid")
	amtStr := q.Get("amt")
	amt, err := strconv.ParseFloat(amtStr, 64)
	if err != nil {
		log.Errorf("Error parsing deposit amount string %q: %v", amtStr, err)
		http.Error(w, "error parsing amount", http.StatusBadRequest)
		return
	}
	coin := q.Get("coin")
	network := q.Get("network")
	wallet, err := f.getWallet(network)
	if err != nil {
		log.Errorf("Error creating deposit wallet for %s: %v", coin, err)
		http.Error(w, "error creating wallet", http.StatusBadRequest)
		return
	}
	confs, err := wallet.Confirmations(f.ctx, txID)
	if err != nil {
		log.Errorf("Error getting deposit confirmations for %s -> %s: %v", coin, txID, err)
		http.Error(w, "error getting confirmations", http.StatusInternalServerError)
		return
	}
	apiKey := extractAPIKey(r)
	status := bntypes.DepositStatusPending
	if confs >= depositConfs {
		status = bntypes.DepositStatusCredited
		log.Debugf("Confirmed deposit for %s of %.8f %s", apiKey, amt, coin)
		f.balancesMtx.Lock()
		var bal *bntypes.WSBalance
		for _, b := range f.balances {
			if b.Asset == coin {
				bal = (*bntypes.WSBalance)(b)
				b.Free += amt
				break
			}
		}
		f.balancesMtx.Unlock()
		if bal != nil {
			f.sendBalanceUpdates([]*bntypes.WSBalance{bal})
		}
	} else {
		log.Tracef("Updating user %s on deposit status for %.8f %s. Confs = %d", apiKey, amt, coin, confs)
	}
	resp := []*bntypes.PendingDeposit{{
		Amount:  amt,
		Status:  status,
		TxID:    txID,
		Coin:    coin,
		Network: network,
	}}
	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) getWallet(network string) (Wallet, error) {
	symbol := strings.ToLower(network)
	f.walletMtx.Lock()
	defer f.walletMtx.Unlock()
	wallet, exists := f.wallets[symbol]
	if exists {
		return wallet, nil
	}
	wallet, err := newWallet(f.ctx, symbol)
	if err != nil {
		return nil, err
	}
	f.wallets[symbol] = wallet
	return wallet, nil
}

func (f *fakeBinance) handleGetDepositAddress(w http.ResponseWriter, r *http.Request) {
	coin := r.URL.Query().Get("coin")
	network := r.URL.Query().Get("network")

	wallet, err := f.getWallet(network)
	if err != nil {
		log.Errorf("Error creating wallet for %s: %v", coin, err)
		http.Error(w, "error creating wallet", http.StatusBadRequest)
		return
	}

	resp := struct {
		Address string `json:"address"`
	}{
		Address: wallet.DepositAddress(),
	}

	log.Tracef("User %s requested deposit address %s", extractAPIKey(r), resp.Address)

	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) handleWithdrawal(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	apiKey := extractAPIKey(r)
	err := r.ParseForm()
	if err != nil {
		log.Errorf("Error parsing form for user %s: ", apiKey, err)
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	amountStr := r.Form.Get("amount")
	amt, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		log.Errorf("Error parsing amount for user %s: ", apiKey, err)
		http.Error(w, "Error parsing amount", http.StatusBadRequest)
		return
	}

	coin := r.Form.Get("coin")
	network := r.Form.Get("network")
	address := r.Form.Get("address")

	withdrawalID := hex.EncodeToString(encode.RandomBytes(32))
	log.Debugf("Withdraw of %.8f %s initiated for user %s", amt, coin, apiKey)

	f.withdrawalHistoryMtx.Lock()
	f.withdrawalHistory[withdrawalID] = &withdrawal{
		amt:     amt * 0.99,
		coin:    coin,
		network: network,
		address: address,
		apiKey:  apiKey,
	}
	f.withdrawalHistoryMtx.Unlock()

	var balUpdate *bntypes.WSBalance
	debitBalance := func(coin string, amt float64) {
		for _, b := range f.balances {
			if b.Asset == coin {
				if amt > b.Free {
					b.Free = 0
				} else {
					b.Free -= amt
				}
			}
			balUpdate = (*bntypes.WSBalance)(b)
		}
	}

	f.balancesMtx.Lock()
	debitBalance(coin, amt)
	f.balancesMtx.Unlock()

	f.sendBalanceUpdates([]*bntypes.WSBalance{balUpdate})

	resp := struct {
		ID string `json:"id"`
	}{withdrawalID}
	writeJSONWithStatus(w, resp, http.StatusOK)
}

type withdrawalHistoryStatus struct {
	ID     string  `json:"id"`
	Amount float64 `json:"amount,string"`
	Status int     `json:"status"`
	TxID   string  `json:"txId"`
}

func (f *fakeBinance) handleWithdrawalHistory(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	const withdrawalCompleteStatus = 6
	withdrawalHistory := make([]*withdrawalHistoryStatus, 0)

	f.withdrawalHistoryMtx.RLock()
	for transferID, w := range f.withdrawalHistory {
		var status int
		txIDPtr := w.txID.Load()
		var txID string
		if txIDPtr == nil {
			status = 2
		} else {
			txID = txIDPtr.(string)
			status = withdrawalCompleteStatus
		}
		withdrawalHistory = append(withdrawalHistory, &withdrawalHistoryStatus{
			ID:     transferID,
			Amount: w.amt,
			Status: status,
			TxID:   txID,
		})
	}
	f.withdrawalHistoryMtx.RUnlock()

	log.Tracef("Sending %d withdraws to user %s", len(withdrawalHistory), extractAPIKey(r))
	writeJSONWithStatus(w, withdrawalHistory, http.StatusOK)
}

func (f *fakeBinance) handleExchangeInfo(w http.ResponseWriter, r *http.Request) {
	writeJSONWithStatus(w, xcInfo, http.StatusOK)
}

func (f *fakeBinance) handleAccount(w http.ResponseWriter, r *http.Request) {
	f.balancesMtx.RLock()
	defer f.balancesMtx.RUnlock()
	writeJSONWithStatus(w, &bntypes.Account{Balances: utils.MapItems(f.balances)}, http.StatusOK)
}

func (f *fakeBinance) handleDepth(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("symbol")
	var mkt *bntypes.Market
	for _, m := range xcInfo.Symbols {
		if m.Symbol == slug {
			mkt = m
			break
		}
	}
	if mkt == nil {
		log.Errorf("No market definition found for market %q", slug)
		http.Error(w, "no market "+slug, http.StatusBadRequest)
		return
	}
	f.marketsMtx.Lock()
	m, found := f.markets[slug]
	if !found {
		baseFiatRate := f.fiatRates[parseAssetID(mkt.BaseAsset)]
		quoteFiatRate := f.fiatRates[parseAssetID(mkt.QuoteAsset)]
		m = newMarket(slug, mkt.BaseAsset, mkt.QuoteAsset, baseFiatRate, quoteFiatRate)
		f.markets[slug] = m
	}
	f.marketsMtx.Unlock()

	var resp bntypes.OrderbookSnapshot
	m.bookMtx.RLock()
	for _, ord := range m.buys {
		resp.Bids = append(resp.Bids, [2]json.Number{json.Number(floatString(ord.rate)), json.Number(floatString(ord.qty))})
	}
	for _, ord := range m.sells {
		resp.Asks = append(resp.Asks, [2]json.Number{json.Number(floatString(ord.rate)), json.Number(floatString(ord.qty))})
	}
	resp.LastUpdateID = m.updateID
	m.bookMtx.RUnlock()
	writeJSONWithStatus(w, &resp, http.StatusOK)
}

func (f *fakeBinance) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	tradeID := r.URL.Query().Get("origClientOrderId")
	var status string
	f.bookedOrdersMtx.RLock()
	ord, found := f.bookedOrders[tradeID]
	if found {
		status = ord.status
	}
	f.bookedOrdersMtx.RUnlock()
	if !found {
		log.Errorf("User %s requested unknown order %s", extractAPIKey(r), tradeID)
		http.Error(w, "order not found", http.StatusBadRequest)
		return
	}
	resp := &bntypes.BookedOrder{
		Symbol: ord.slug,
		// OrderID:            ,
		ClientOrderID:      tradeID,
		Price:              ord.rate,
		OrigQty:            ord.qty,
		ExecutedQty:        0,
		CumulativeQuoteQty: 0,
		Status:             status,
		TimeInForce:        "GTC",
	}
	writeJSONWithStatus(w, &resp, http.StatusOK)
}

type orderBalanceUpdate uint16

const (
	updateTypeInstantFill orderBalanceUpdate = iota
	updateTypeBook
	updateTypeFillBooked
	updateTypeCancelBooked
)

func (f *fakeBinance) updateOrderBalances(symbol string, sell bool, qty, rate float64, updateType orderBalanceUpdate) {
	f.marketsMtx.RLock()
	mkt := f.markets[symbol]
	f.marketsMtx.RUnlock()

	fromSlug, toSlug, fromQty, toQty := mkt.quoteSlug, mkt.baseSlug, qty*rate, qty
	if sell {
		fromSlug, toSlug, fromQty, toQty = toSlug, fromSlug, toQty, fromQty
	}

	balUpdates := make([]*bntypes.WSBalance, 0, 2)

	f.balancesMtx.Lock()
	fromBal, toBal := f.balances[fromSlug], f.balances[toSlug]
	switch updateType {
	case updateTypeInstantFill:
		fromBal.Free -= fromQty
		toBal.Free += toQty
		balUpdates = append(balUpdates, (*bntypes.WSBalance)(toBal))
	case updateTypeBook:
		fromBal.Free -= fromQty
		fromBal.Locked += fromQty
	case updateTypeFillBooked:
		fromBal.Locked -= fromQty
		toBal.Free += toQty
		balUpdates = append(balUpdates, (*bntypes.WSBalance)(toBal))
	case updateTypeCancelBooked:
		fromBal.Locked -= fromQty
		fromBal.Free += fromQty
	}
	balUpdates = append(balUpdates, (*bntypes.WSBalance)(fromBal))
	f.balancesMtx.Unlock()

	f.sendBalanceUpdates(balUpdates)
}

func (f *fakeBinance) handlePostOrder(w http.ResponseWriter, r *http.Request) {
	apiKey := extractAPIKey(r)
	q := r.URL.Query()
	slug := q.Get("symbol")
	side := q.Get("side")
	tradeID := q.Get("newClientOrderId")
	qty, err := strconv.ParseFloat(q.Get("quantity"), 64)
	if err != nil {
		log.Errorf("Error parsing quantity %q for order from user %s: %v", q.Get("quantity"), apiKey, err)
		http.Error(w, "Bad quantity formatting", http.StatusBadRequest)
		return
	}
	price, err := strconv.ParseFloat(q.Get("price"), 64)
	if err != nil {
		log.Errorf("Error parsing price %q for order from user %s: %v", q.Get("price"), apiKey, err)
		http.Error(w, "Missing price formatting", http.StatusBadRequest)
		return
	}

	resp := &bntypes.OrderResponse{
		Symbol:       slug,
		Price:        price,
		OrigQty:      qty,
		OrigQuoteQty: qty * price,
	}

	bookIt := rand.Float32() < 0.2
	if bookIt {
		resp.Status = "NEW"
		log.Tracef("Booking %s order on %s for %.8f for user %s", side, slug, qty, apiKey)
		f.updateOrderBalances(slug, side == "SELL", qty, price, updateTypeBook)
	} else {
		log.Tracef("Filled %s order on %s for %.8f for user %s", side, slug, qty, apiKey)
		resp.Status = "FILLED"
		resp.ExecutedQty = qty
		resp.CumulativeQuoteQty = qty * price
		f.updateOrderBalances(slug, side == "SELL", qty, price, updateTypeInstantFill)
	}

	f.bookedOrdersMtx.Lock()
	f.bookedOrders[tradeID] = &userOrder{
		slug:   slug,
		sell:   side == "SELL",
		rate:   price,
		qty:    qty,
		apiKey: apiKey,
		stamp:  time.Now(),
		status: resp.Status,
	}
	f.bookedOrdersMtx.Unlock()

	writeJSONWithStatus(w, &resp, http.StatusOK)
}

func (f *fakeBinance) streamExtend(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (f *fakeBinance) handleListenKeyRequest(w http.ResponseWriter, r *http.Request) {
	resp := &bntypes.DataStreamKey{
		ListenKey: extractAPIKey(r),
	}
	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) handleDeleteOrder(w http.ResponseWriter, r *http.Request) {
	tradeID := r.URL.Query().Get("origClientOrderId")
	apiKey := extractAPIKey(r)

	f.bookedOrdersMtx.Lock()
	ord, found := f.bookedOrders[tradeID]
	updateBals := false
	if found {
		if ord.status == "CANCELED" {
			log.Errorf("Detected cancellation of an already cancelled order %s", tradeID)
		} else if ord.status == "FILLED" {
			log.Errorf("Detected cancellation of an already filled order %s", tradeID)
		} else {
			updateBals = true
		}
		ord.status = "CANCELED"
	}
	f.bookedOrdersMtx.Unlock()

	if updateBals {
		f.updateOrderBalances(ord.slug, ord.sell, ord.qty, ord.rate, updateTypeCancelBooked)
	}

	writeJSONWithStatus(w, &struct{}{}, http.StatusOK)
	if !found {
		log.Errorf("DELETE request received from user %s for unknown order %s", apiKey, tradeID)
		return
	}

	log.Tracef("Deleting order %s on %s for user %s", tradeID, ord.slug, apiKey)
	f.accountSubscribersMtx.RLock()
	sub, found := f.accountSubscribers[ord.apiKey]
	f.accountSubscribersMtx.RUnlock()
	if !found {
		return
	}
	update := &bntypes.StreamUpdate{
		EventType:          "executionReport",
		CurrentOrderStatus: "CANCELED",
		ClientOrderID:      hex.EncodeToString(encode.RandomBytes(20)),
		CancelledOrderID:   tradeID,
		Filled:             0,
		QuoteFilled:        0,
	}
	updateB, _ := json.Marshal(update)
	sub.SendRaw(updateB)
}

func (f *fakeBinance) handleMarketTicker24(w http.ResponseWriter, r *http.Request) {
	resp := make([]*bntypes.MarketTicker24, 0, len(xcInfo.Symbols))
	for _, mkt := range xcInfo.Symbols {
		baseFiatRate := f.fiatRates[parseAssetID(mkt.BaseAsset)]
		quoteFiatRate := f.fiatRates[parseAssetID(mkt.QuoteAsset)]
		m := newMarket(mkt.Symbol, mkt.BaseAsset, mkt.QuoteAsset, baseFiatRate, quoteFiatRate)
		var buyPrice, sellPrice float64
		if len(m.buys) > 0 {
			buyPrice = m.buys[0].rate
		}
		if len(m.sells) > 0 {
			sellPrice = m.sells[0].rate
		}
		vol24USD := math.Pow(10, float64(rand.Intn(4)+2))
		vol24Base := vol24USD / baseFiatRate
		vol24Quote := vol24USD / quoteFiatRate
		lastPrice := m.basisRate
		highPrice := lastPrice * (1 + rand.Float64()*0.15)
		lowPrice := lastPrice / (1 + rand.Float64()*0.15)
		openPrice := lowPrice + ((highPrice - lowPrice) * rand.Float64())
		priceChange := lastPrice - openPrice
		priceChangePct := priceChange / openPrice * 100

		avgPrice := (openPrice + lastPrice + highPrice + lowPrice) / 4

		resp = append(resp, &bntypes.MarketTicker24{
			Symbol:             mkt.Symbol,
			PriceChange:        priceChange,
			PriceChangePercent: priceChangePct,
			BidPrice:           buyPrice,
			AskPrice:           sellPrice,
			Volume:             vol24Base,
			QuoteVolume:        vol24Quote,
			WeightedAvgPrice:   avgPrice,
			LastPrice:          lastPrice,
			OpenPrice:          openPrice,
			HighPrice:          highPrice,
			LowPrice:           lowPrice,
		})
	}
	writeJSONWithStatus(w, &resp, http.StatusOK)
}

type rateQty struct {
	rate float64
	qty  float64
}

type market struct {
	symbol, baseSlug, quoteSlug            string
	baseFiatRate, quoteFiatRate, basisRate float64
	minRate, maxRate                       float64

	rate atomic.Uint64

	bookMtx     sync.RWMutex
	updateID    uint64
	buys, sells []*rateQty
}

func newMarket(symbol, baseSlug, quoteSlug string, baseFiatRate, quoteFiatRate float64) *market {
	const maxVariation = 0.1
	basisRate := baseFiatRate / quoteFiatRate
	minRate, maxRate := basisRate*(1/(1+maxVariation)), basisRate*(1+maxVariation)
	m := &market{
		symbol:        symbol,
		baseSlug:      baseSlug,
		quoteSlug:     quoteSlug,
		baseFiatRate:  baseFiatRate,
		quoteFiatRate: quoteFiatRate,
		basisRate:     basisRate,
		minRate:       minRate,
		maxRate:       maxRate,
		buys:          make([]*rateQty, 0),
		sells:         make([]*rateQty, 0),
	}
	m.rate.Store(math.Float64bits(basisRate))
	log.Tracef("Market %s intitialized with base fiat rate = %.4f, quote fiat rate = %.4f "+
		"basis rate = %.8f. Mid-gap rate will randomly walk between %.8f and %.8f",
		symbol, baseFiatRate, quoteFiatRate, basisRate, minRate, maxRate)
	m.shuffle()
	return m
}

// Randomize the order book. booksMtx must be locked.
func (m *market) shuffle() (buys, sells [][2]json.Number) {
	maxChangeRatio := defaultWalkingSpeed * walkingSpeedAdj
	maxShift := m.basisRate * maxChangeRatio
	oldRate := math.Float64frombits(m.rate.Load())
	if rand.Float64() < 0.5 {
		maxShift *= -1
	}
	shiftRoll := rand.Float64()
	shift := maxShift * shiftRoll
	newRate := oldRate + shift

	if newRate < m.minRate {
		newRate = m.minRate
	}
	if newRate > m.maxRate {
		newRate = m.maxRate
	}

	m.rate.Store(math.Float64bits(newRate))
	log.Tracef("%s: A randomized (max %.1f%%) shift of %.8f (%.3f%%) was applied to the old rate of %.8f, "+
		"resulting in a new mid-gap of %.8f",
		m.symbol, maxChangeRatio*100, shift, shiftRoll*maxChangeRatio*100, oldRate, newRate,
	)

	halfGapRoll := rand.Float64()
	const minHalfGap = 0.002 // 0.2%
	halfGapRange := gapRange / 2
	halfGapFactor := minHalfGap + halfGapRoll*halfGapRange
	bestBuy, bestSell := newRate/(1+halfGapFactor), newRate*(1+halfGapFactor)

	levelSpacingRoll := rand.Float64()
	const minLevelSpacing, levelSpacingRange = 0.002, 0.01
	levelSpacing := (minLevelSpacing + levelSpacingRoll*levelSpacingRange) * newRate

	log.Tracef("%s: Half-gap roll of %.4f%% resulted in a half-gap factor of %.4f%%, range %.8f to %0.8f. "+
		"Level-spacing roll of %.4f%% resulted in a level spacing of %.8f",
		m.symbol, halfGapRoll*100, halfGapFactor*100, bestBuy, bestSell, levelSpacingRoll*100, levelSpacing,
	)

	zeroBookSide := func(ords []*rateQty) map[string]string {
		bin := make(map[string]string, len(ords))
		for _, ord := range ords {
			bin[floatString(ord.rate)] = "0"
		}
		return bin
	}
	jsBuys, jsSells := zeroBookSide(m.buys), zeroBookSide(m.sells)

	makeOrders := func(bestRate, direction float64, jsSide map[string]string) []*rateQty {
		nLevels := rand.Intn(20) + 5
		ords := make([]*rateQty, nLevels)
		for i := 0; i < nLevels; i++ {
			rate := bestRate + levelSpacing*direction*float64(i)
			// Each level has between 1 and 10,001 USD equivalent.
			const minQtyUSD, qtyUSDRange = 1, 10_000
			qtyUSD := minQtyUSD + qtyUSDRange*rand.Float64()
			qty := qtyUSD / m.baseFiatRate
			jsSide[floatString(rate)] = floatString(qty)
			ords[i] = &rateQty{
				rate: rate,
				qty:  qty,
			}
		}
		return ords
	}
	m.buys = makeOrders(bestBuy, -1, jsBuys)
	m.sells = makeOrders(bestSell, 1, jsSells)

	log.Tracef("%s: Shuffle resulted in %d buy orders and %d sell orders being placed", m.symbol, len(m.buys), len(m.sells))

	convertSide := func(side map[string]string) [][2]json.Number {
		updates := make([][2]json.Number, 0, len(side))
		for r, q := range side {
			updates = append(updates, [2]json.Number{json.Number(r), json.Number(q)})
		}
		return updates
	}

	return convertSide(jsBuys), convertSide(jsSells)
}

// writeJSON marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(thing)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("JSON encode error: %v", err)
		return
	}
	writeBytesWithStatus(w, b, code)
}

func writeBytesWithStatus(w http.ResponseWriter, b []byte, code int) {
	w.WriteHeader(code)
	_, err := w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}

func extractAPIKey(r *http.Request) string {
	return r.Header.Get("X-MBX-APIKEY")
}

func floatString(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}
