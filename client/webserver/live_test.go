// +build live
// Run a test server with
// go test -v -tags live -run Server -timeout 60m
// test server will run for 1 hour and serve randomness.

package webserver

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
)

const (
	firstDEX  = "somedex.com"
	secondDEX = "thisdexwithalongname.com"
)

var (
	tCtx                  context.Context
	maxDelay              = time.Second * 2
	epochDuration         = time.Second * 30 // milliseconds
	feedPeriod            = time.Second * 10
	forceDisconnectWallet bool
	wipeWalletBalance     bool
	gapWidthFactor        = 1.0 // Should be 0 < gapWidthFactor <= 1.0
)

func dummySettings() map[string]string {
	return map[string]string{
		"rpcuser":     "dexuser",
		"rpcpassword": "dexpass",
		"rpcbind":     "127.0.0.1:54321",
		"rpcport":     "",
		"fallbackfee": "20",
		"txsplit":     "0",
		"username":    "dexuser",
		"password":    "dexpass",
		"rpclisten":   "127.0.0.1:54321",
		"rpccert":     "/home/me/dex/rpc.cert",
	}
}

func randomDelay() {
	time.Sleep(time.Duration(rand.Float64() * float64(maxDelay)))
}

// A random number with a random order of magnitude.
func randomMagnitude(low, high int) float64 {
	exponent := rand.Intn(high-low) + low
	mantissa := rand.Float64() * 10
	return mantissa * math.Pow10(exponent)
}

func userOrders() (ords []*core.Order) {
	orderCount := rand.Intn(50)
	for i := 0; i < orderCount; i++ {
		qty := uint64(randomMagnitude(7, 11))
		filled := uint64(rand.Float64() * float64(qty))
		orderType := order.OrderType(rand.Intn(2) + 1)
		status := order.OrderStatusEpoch
		epoch := encode.UnixMilliU(time.Now()) / uint64(epochDuration.Milliseconds())
		isLimit := orderType == order.LimitOrderType
		if rand.Float32() > 0.5 {
			epoch -= 1
			if isLimit {
				status = order.OrderStatusBooked
			} else {
				status = order.OrderStatusExecuted
			}
		}
		var tif order.TimeInForce
		if isLimit {
			if rand.Float32() < 0.25 {
				tif = order.ImmediateTiF
			} else {
				tif = order.StandingTiF
			}
		}
		ords = append(ords, &core.Order{
			ID:     ordertest.RandomOrderID().Bytes(),
			Type:   orderType,
			Stamp:  encode.UnixMilliU(time.Now()) - uint64(rand.Float64()*600_000),
			Status: status,
			Epoch:  epoch,
			Rate:   uint64(randomMagnitude(4, 12)),
			Qty:    qty,
			Sell:   rand.Intn(2) > 0,
			Filled: filled,
			Matches: []*core.Match{
				{
					Qty:    uint64(rand.Float64() * float64(filled)),
					Status: order.MatchComplete,
				},
			},
			TimeInForce: tif,
		})
	}
	return
}

func mkMrkt(base, quote string) *core.Market {
	baseID, _ := dex.BipSymbolID(base)
	quoteID, _ := dex.BipSymbolID(quote)
	return &core.Market{
		Name:            fmt.Sprintf("%s-%s", base, quote),
		BaseID:          baseID,
		BaseSymbol:      base,
		QuoteID:         quoteID,
		QuoteSymbol:     quote,
		MarketBuyBuffer: rand.Float64() + 1,
		EpochLen:        uint64(epochDuration.Milliseconds()),
	}
}

func mkSupportedAsset(symbol string, state *tWalletState, bal *core.WalletBalance) *core.SupportedAsset {
	assetID, _ := dex.BipSymbolID(symbol)
	winfo := winfos[assetID]
	var wallet *core.WalletState
	if state != nil {
		wallet = &core.WalletState{
			Symbol:  unbip(assetID),
			AssetID: assetID,
			Open:    state.open,
			Running: state.running,
			Address: ordertest.RandomAddress(),
			Balance: bal,
			Units:   winfo.Units,
		}
	}
	return &core.SupportedAsset{
		ID:     assetID,
		Symbol: symbol,
		Wallet: wallet,
		Info:   winfo,
	}
}

func mkDexAsset(symbol string) *dex.Asset {
	assetID, _ := dex.BipSymbolID(symbol)
	assetOrder := rand.Intn(5) + 6
	lotSize := uint64(math.Pow10(assetOrder)) * uint64(rand.Intn(9)+1)
	a := &dex.Asset{
		ID:           assetID,
		Symbol:       symbol,
		LotSize:      lotSize,
		RateStep:     lotSize / 1e3,
		MaxFeeRate:   uint64(rand.Intn(10) + 1),
		SwapSize:     uint64(rand.Intn(150) + 150),
		SwapSizeBase: uint64(rand.Intn(150) + 15),
		SwapConf:     uint32(rand.Intn(5) + 2),
	}
	return a
}

func mkid(b, q uint32) string {
	return unbip(b) + "_" + unbip(q)
}

func getEpoch() uint64 {
	return encode.UnixMilliU(time.Now()) / uint64(epochDuration.Milliseconds())
}

func randomOrder(sell bool, maxQty, midGap, marketWidth float64, epoch bool) *core.MiniOrder {
	var epochIdx uint64
	var rate float64
	var limitRate = midGap - rand.Float64()*marketWidth
	if sell {
		limitRate = midGap + rand.Float64()*marketWidth
	}
	if epoch {
		epochIdx = getEpoch()
		// Epoch orders might be market orders.
		if rand.Float32() < 0.5 {
			rate = limitRate
		}
	} else {
		rate = limitRate
	}

	return &core.MiniOrder{
		Qty:   math.Exp(-rand.Float64()*5) * maxQty,
		Rate:  rate,
		Sell:  sell,
		Token: nextToken(),
		Epoch: &epochIdx,
	}
}

var tExchanges = map[string]*core.Exchange{
	firstDEX: {
		Host: "somedex.com",
		Assets: map[uint32]*dex.Asset{
			0:   mkDexAsset("btc"),
			2:   mkDexAsset("ltc"),
			42:  mkDexAsset("dcr"),
			22:  mkDexAsset("mona"),
			28:  mkDexAsset("vtc"),
			141: mkDexAsset("kmd"),
			3:   mkDexAsset("doge"),
		},
		Markets: map[string]*core.Market{
			mkid(42, 0): mkMrkt("dcr", "btc"),
			mkid(42, 2): mkMrkt("dcr", "ltc"),
			mkid(3, 22): mkMrkt("doge", "mona"),
		},
		Connected: true,
	},
	secondDEX: {
		Host: "thisdexwithalongname.com",
		Assets: map[uint32]*dex.Asset{
			0:   mkDexAsset("btc"),
			2:   mkDexAsset("ltc"),
			42:  mkDexAsset("dcr"),
			22:  mkDexAsset("mona"),
			28:  mkDexAsset("vtc"),
			141: mkDexAsset("kmd"),
			3:   mkDexAsset("doge"),
		},
		Markets: map[string]*core.Market{
			mkid(42, 28):  mkMrkt("dcr", "vtc"),
			mkid(0, 2):    mkMrkt("btc", "ltc"),
			mkid(22, 141): mkMrkt("mona", "kmd"),
		},
		Connected: true,
	},
}

type tCoin struct {
	id       []byte
	confs    uint32
	confsErr error
}

func (c *tCoin) ID() dex.Bytes {
	return c.id
}

func (c *tCoin) String() string {
	return hex.EncodeToString(c.id)
}

func (c *tCoin) Value() uint64 {
	return 0
}

func (c *tCoin) Confirmations() (uint32, error) {
	return c.confs, c.confsErr
}

type tWalletState struct {
	open     bool
	running  bool
	settings map[string]string
}

type TCore struct {
	reg         *core.RegisterForm
	inited      bool
	mtx         sync.RWMutex
	wallets     map[uint32]*tWalletState
	balances    map[uint32]*core.WalletBalance
	dexAddr     string
	marketID    string
	base        uint32
	quote       uint32
	midGap      float64
	maxQty      float64
	feed        *core.BookFeed
	killFeed    context.CancelFunc
	buys        map[string]*core.MiniOrder
	sells       map[string]*core.MiniOrder
	noteFeed    chan core.Notification
	orderMtx    sync.Mutex
	epochOrders []*core.BookUpdate
}

// TDriver implements the interface required of all exchange wallets.
type TDriver struct{}

func (*TDriver) Setup(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error) {
	return nil, nil
}

func (*TDriver) DecodeCoinID(coinID []byte) (string, error) {
	return asset.DecodeCoinID(0, coinID) // btc decoder
}

func (*TDriver) Info() *asset.WalletInfo {
	return nil
}

func newTCore() *TCore {
	return &TCore{
		wallets: make(map[uint32]*tWalletState),
		balances: map[uint32]*core.WalletBalance{
			0:  randomBalance(),
			2:  randomBalance(),
			42: randomBalance(),
			22: randomBalance(),
			3:  randomBalance(),
			28: randomBalance(),
		},
		noteFeed: make(chan core.Notification, 1),
	}
}

func (c *TCore) trySend(u *core.BookUpdate) {
	select {
	case c.feed.C <- u:
	default:
	}
}

func (c *TCore) Network() dex.Network { return dex.Mainnet }

func (c *TCore) Exchanges() map[string]*core.Exchange { return tExchanges }

func (c *TCore) InitializeClient(pw []byte) error {
	randomDelay()
	c.inited = true
	return nil
}
func (c *TCore) GetFee(host, cert string) (uint64, error) {
	return 1e8, nil
}

func (c *TCore) Register(r *core.RegisterForm) (*core.RegisterResult, error) {
	randomDelay()
	c.reg = r
	return nil, nil
}
func (c *TCore) Login([]byte) (*core.LoginResult, error) { return &core.LoginResult{}, nil }
func (c *TCore) Logout() error                           { return nil }

var orderAssets = []string{"dcr", "btc", "ltc", "doge", "mona", "vtc"}

func (c *TCore) Orders(filter *core.OrderFilter) ([]*core.Order, error) {
	var spacing uint64 = 60 * 60 * 1000 / 2 // half an hour
	t := encode.UnixMilliU(time.Now())

	cords := make([]*core.Order, 0, filter.N)
	for i := 0; i < int(filter.N); i++ {
		cord := makeCoreOrder()

		cord.Stamp = t
		// Make it a little older.
		t -= spacing

		cords = append(cords, cord)
	}
	return cords, nil
}

func utxoID() []byte {
	vout := uint32(rand.Intn(15))
	txHash := encode.RandomBytes(32)
	b := make([]byte, 36)
	copy(b[:], txHash)
	binary.BigEndian.PutUint32(b[32:], vout)
	return b
}

func makeCoreOrder() *core.Order {
	// sell := rand.Float32() < 0.5
	// center := randomMagnitude(-2, 4)
	// ord := randomOrder(sell, randomMagnitude(-2, 4), center, gapWidthFactor*center, true)
	host := firstDEX
	if rand.Float32() > 0.5 {
		host = secondDEX
	}
	baseIdx := rand.Intn(len(orderAssets))
	baseSymbol := orderAssets[baseIdx]
	quoteSymbol := orderAssets[(baseIdx+1)%len(orderAssets)]
	baseID, _ := dex.BipSymbolID(baseSymbol)
	quoteID, _ := dex.BipSymbolID(quoteSymbol)
	lotSize := tExchanges[host].Assets[baseID].LotSize
	rateStep := tExchanges[host].Assets[quoteID].RateStep
	rate := uint64(rand.Intn(1e3)) * rateStep
	baseQty := uint64(rand.Intn(1e3)) * lotSize
	isMarket := rand.Float32() > 0.5
	sell := rand.Float32() > 0.5
	numMatches := rand.Intn(13)
	orderQty := baseQty
	orderRate := rate
	matchQ := baseQty / 13
	matchQ -= matchQ % lotSize
	tif := order.TimeInForce(rand.Intn(int(order.StandingTiF)))

	if isMarket {
		orderRate = 0
		if !sell {
			orderQty = calc.BaseToQuote(rate, baseQty)
		}
	}

	numCoins := rand.Intn(5) + 1
	fundingCoins := make([]dex.Bytes, 0, numCoins)
	for i := 0; i < numCoins; i++ {
		coinID := make([]byte, 36)
		copy(coinID[:], encode.RandomBytes(32))
		coinID[35] = byte(rand.Intn(8))
		fundingCoins = append(fundingCoins, coinID)
	}

	status := order.OrderStatus(rand.Intn(int(order.OrderStatusRevoked-1))) + 1

	stamp := func() uint64 {
		return encode.UnixMilliU(time.Now().Add(-time.Second * time.Duration(rand.Intn(60*60))))
	}

	cord := &core.Order{
		Host:        host,
		BaseID:      baseID,
		BaseSymbol:  baseSymbol,
		QuoteID:     quoteID,
		QuoteSymbol: quoteSymbol,
		MarketID:    baseSymbol + "_" + quoteSymbol,
		Type:        order.OrderType(rand.Intn(int(order.MarketOrderType))) + 1,
		Stamp:       stamp(),
		ID:          ordertest.RandomOrderID().Bytes(),
		Status:      status,
		Qty:         orderQty,
		Sell:        sell,
		Filled:      uint64(rand.Float64() * float64(orderQty)),
		Canceled:    status == order.OrderStatusCanceled,
		Rate:        orderRate,
		TimeInForce: tif,
		FeesPaid: &core.FeeBreakdown{
			Swap:       orderQty / 100,
			Redemption: rateStep * 100,
		},
		FundingCoins: fundingCoins,
	}

	for i := 0; i < numMatches; i++ {
		userMatch := ordertest.RandomUserMatch()
		matchQty := matchQ
		if i == numMatches-1 {
			matchQty = baseQty - (matchQ * (uint64(numMatches) - 1))
		}
		match := &core.Match{
			MatchID: userMatch.MatchID[:],
			Status:  userMatch.Status,
			Rate:    rate,
			Qty:     matchQty,
			Side:    userMatch.Side,
			Stamp:   stamp(),
		}

		if rand.Float32() < 0.75 {
			match.Swap = utxoID()
			if rand.Float32() < 0.75 {
				match.CounterSwap = utxoID()
				if rand.Float32() < 0.75 {
					match.Redeem = utxoID()
					match.CounterRedeem = utxoID()
				} else if rand.Float32() < 0.2 {
					match.Refund = utxoID()
				}
			}
		}

		cord.Matches = append(cord.Matches, match)
	}
	return cord
}

func (c *TCore) Order(dex.Bytes) (*core.Order, error) {
	return makeCoreOrder(), nil
}

func (c *TCore) SyncBook(dexAddr string, base, quote uint32) (*core.BookFeed, error) {
	quoteAsset := tExchanges[dexAddr].Assets[quote]
	baseAsset := tExchanges[dexAddr].Assets[base]
	c.midGap = float64(quoteAsset.RateStep) / 1e8 * float64(rand.Intn(1e6))
	c.maxQty = float64(baseAsset.LotSize) / 1e8 * float64(rand.Intn(1e3))
	mktID, _ := dex.MarketName(base, quote)
	c.mtx.Lock()
	c.dexAddr = dexAddr
	c.marketID = mktID
	c.base = base
	c.quote = quote
	c.mtx.Unlock()

	if c.feed != nil {
		c.killFeed()
	}

	c.feed = core.NewBookFeed(func(*core.BookFeed) {})
	var ctx context.Context
	ctx, c.killFeed = context.WithCancel(tCtx)
	go func() {
	out:
		for {
			select {
			case <-time.NewTicker(feedPeriod).C:
				// Send a random order to the order feed. Slighly biased away from
				// unbook_order and towards book_order.
				r := rand.Float32()
				switch {
				case r < 0.80:
					// Book order
					sell := rand.Float32() < 0.5
					ord := randomOrder(sell, c.maxQty, c.midGap, gapWidthFactor*c.midGap, true)
					c.orderMtx.Lock()
					side := c.buys
					if sell {
						side = c.sells
					}
					side[ord.Token] = ord
					epochOrder := &core.BookUpdate{
						Action:   msgjson.EpochOrderRoute,
						Host:     c.dexAddr,
						MarketID: mktID,
						Payload:  ord,
					}
					c.trySend(epochOrder)
					c.epochOrders = append(c.epochOrders, epochOrder)
					c.orderMtx.Unlock()
				default:
					// Unbook order
					sell := rand.Float32() < 0.5
					c.orderMtx.Lock()
					side := c.buys
					if sell {
						side = c.sells
					}
					var tkn string
					for tkn = range side {
						break
					}
					if tkn == "" {
						c.orderMtx.Unlock()
						continue
					}
					delete(side, tkn)
					c.orderMtx.Unlock()

					c.trySend(&core.BookUpdate{
						Action:   msgjson.UnbookOrderRoute,
						Host:     c.dexAddr,
						MarketID: mktID,
						Payload:  &core.MiniOrder{Token: tkn},
					})
				}
			case <-ctx.Done():
				break out
			}
		}
	}()

	c.feed.C <- &core.BookUpdate{
		Action:   core.FreshBookAction,
		Host:     dexAddr,
		MarketID: mktID,
		Payload: &core.MarketOrderBook{
			Base:  base,
			Quote: quote,
			Book:  c.book(),
		},
	}

	return c.feed, nil
}

var numBuys = 80
var numSells = 80
var tokenCounter uint32

func nextToken() string {
	return strconv.Itoa(int(atomic.AddUint32(&tokenCounter, 1)))
}

// Book randomizes an order book.
func (c *TCore) book() *core.OrderBook {
	midGap := c.midGap
	maxQty := c.maxQty
	// Set the market width to about 5% of midGap.
	var buys, sells []*core.MiniOrder
	c.orderMtx.Lock()
	c.buys = make(map[string]*core.MiniOrder, numBuys)
	c.sells = make(map[string]*core.MiniOrder, numSells)
	c.epochOrders = nil
	for i := 0; i < numSells; i++ {
		ord := randomOrder(true, maxQty, midGap, gapWidthFactor*c.midGap, false)
		sells = append(sells, ord)
		c.sells[ord.Token] = ord
	}
	for i := 0; i < numBuys; i++ {
		// For buys the rate must be smaller than midGap.
		ord := randomOrder(false, maxQty, midGap, gapWidthFactor*c.midGap, false)
		buys = append(buys, ord)
		c.buys[ord.Token] = ord
	}
	c.orderMtx.Unlock()
	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })
	return &core.OrderBook{
		Buys:  buys,
		Sells: sells,
	}
}

func (c *TCore) Unsync(dex string, base, quote uint32) {
	if c.feed != nil {
		c.killFeed()
	}
}

func randomBalance() *core.WalletBalance {
	randVal := func() uint64 {
		return uint64(rand.Float64() * math.Pow10(rand.Intn(6)+6))
	}

	return &core.WalletBalance{
		Balance: &db.Balance{
			Balance: asset.Balance{
				Available: randVal(),
				Immature:  randVal(),
				Locked:    randVal(),
			},
			Stamp: time.Now().Add(-time.Duration(int64(2 * float64(time.Hour) * rand.Float64()))),
		},
		ContractLocked: randVal(),
	}
}

func randomBalanceNote(assetID uint32) *core.BalanceNote {
	return &core.BalanceNote{
		Notification: db.NewNotification(core.NoteTypeBalance, "", "", db.Data),
		AssetID:      assetID,
		Balance:      randomBalance(),
	}
}

func (c *TCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	balNote := randomBalanceNote(assetID)
	balNote.Balance.Stamp = time.Now()
	c.noteFeed <- balNote
	// c.mtx.Lock()
	// c.balances[assetID] = balNote.Balances
	// c.mtx.Unlock()
	return balNote.Balance, nil
}

func (c *TCore) AckNotes(ids []dex.Bytes) {}

var configOpts = []*asset.ConfigOption{
	{
		DisplayName: "RPC Server",
		Description: "RPC Server",
		Key:         "rpc_server",
	},
}
var winfos = map[uint32]*asset.WalletInfo{
	0:  btc.WalletInfo,
	2:  ltc.WalletInfo,
	42: dcr.WalletInfo,
	22: {
		Units:      "atoms",
		Name:       "Monacoin",
		ConfigOpts: configOpts,
	},
	3: {
		Units:      "atoms",
		Name:       "Dogecoin",
		ConfigOpts: configOpts,
	},
	28: {
		Units:      "Satoshis",
		Name:       "Vertcoin",
		ConfigOpts: configOpts,
	},
}

func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	w := c.wallets[assetID]
	if w == nil {
		return nil
	}
	return &core.WalletState{
		Symbol:  unbip(assetID),
		AssetID: assetID,
		Open:    w.open,
		Running: w.running,
		Address: ordertest.RandomAddress(),
		Balance: c.balances[assetID],
		Units:   winfos[assetID].Units,
	}
}

func (c *TCore) CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error {
	randomDelay()
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.wallets[form.AssetID] = &tWalletState{
		running:  true,
		open:     true,
		settings: form.Config,
	}
	return nil
}

func (c *TCore) OpenWallet(assetID uint32, pw []byte) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to open non-existent test wallet for asset ID %d", assetID)
	}
	wallet.running = true
	wallet.open = true
	return nil
}

func (c *TCore) ConnectWallet(assetID uint32) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to connect to non-existent test wallet for asset ID %d", assetID)
	}
	wallet.running = true
	return nil
}

func (c *TCore) CloseWallet(assetID uint32) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to close non-existent test wallet")
	}

	if forceDisconnectWallet {
		wallet.running = false
	}

	wallet.open = false
	return nil
}

func (c *TCore) Wallets() []*core.WalletState {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	stats := make([]*core.WalletState, 0, len(c.wallets))
	for assetID, wallet := range c.wallets {
		stats = append(stats, &core.WalletState{
			Symbol:  unbip(assetID),
			AssetID: assetID,
			Open:    wallet.open,
			Running: wallet.running,
			Address: ordertest.RandomAddress(),
			Balance: c.balances[assetID],
			Units:   winfos[assetID].Units,
		})
	}
	return stats
}

func (c *TCore) WalletSettings(assetID uint32) (map[string]string, error) {
	return c.wallets[assetID].settings, nil
}
func (c *TCore) ReconfigureWallet(pw []byte, assetID uint32, cfg map[string]string) error {
	c.wallets[assetID].settings = cfg
	return nil
}

func (c *TCore) SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error { return nil }

func (c *TCore) User() *core.User {
	exchanges := map[string]*core.Exchange{}
	if c.reg != nil {
		exchanges = tExchanges
	}
	for _, xc := range tExchanges {
		for _, mkt := range xc.Markets {
			mkt.Orders = userOrders()
		}
	}
	user := &core.User{
		Exchanges:   exchanges,
		Initialized: c.inited,
		Assets:      c.SupportedAssets(),
	}
	return user
}

func (c *TCore) AutoWalletConfig(assetID uint32) (map[string]string, error) {
	return map[string]string{
		"username": "tacotime",
		"password": "abc123",
	}, nil
}

func (c *TCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return map[uint32]*core.SupportedAsset{
		0:  mkSupportedAsset("btc", c.wallets[0], c.balances[0]),
		42: mkSupportedAsset("dcr", c.wallets[42], c.balances[42]),
		2:  mkSupportedAsset("ltc", c.wallets[2], c.balances[2]),
		22: mkSupportedAsset("mona", c.wallets[22], c.balances[22]),
		3:  mkSupportedAsset("doge", c.wallets[3], c.balances[3]),
		28: mkSupportedAsset("vtc", c.wallets[28], c.balances[28]),
	}
}

func (c *TCore) Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error) {
	return &tCoin{id: []byte{0xde, 0xc7, 0xed}}, nil
}

func (c *TCore) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	c.OpenWallet(form.Quote, []byte(""))
	c.OpenWallet(form.Base, []byte(""))
	oType := order.LimitOrderType
	if !form.IsLimit {
		oType = order.MarketOrderType
	}
	return &core.Order{
		ID:    ordertest.RandomOrderID().Bytes(),
		Type:  oType,
		Stamp: encode.UnixMilliU(time.Now()),
		Rate:  form.Rate,
		Qty:   form.Qty,
		Sell:  form.Sell,
	}, nil
}

func (c *TCore) Cancel(pw []byte, oid dex.Bytes) error {
	for _, xc := range tExchanges {
		for _, mkt := range xc.Markets {
			for _, ord := range mkt.Orders {
				if ord.ID.String() == oid.String() {
					ord.Cancelling = true
				}
			}
		}
	}
	return nil
}

func (c *TCore) NotificationFeed() <-chan core.Notification { return c.noteFeed }

func (c *TCore) runEpochs() {
	epochTick := time.NewTimer(time.Second).C
out:
	for {
		select {
		case <-epochTick:
			epochTick = time.NewTimer(epochDuration - time.Since(time.Now().Truncate(epochDuration))).C
			c.mtx.RLock()
			dexAddr := c.dexAddr
			mktID := c.marketID
			baseID := c.base
			quoteID := c.quote
			baseConnected := false
			if w := c.wallets[baseID]; w != nil && w.running {
				baseConnected = true
			}
			quoteConnected := false
			if w := c.wallets[quoteID]; w != nil && w.running {
				quoteConnected = true
			}
			c.mtx.RUnlock()
			c.noteFeed <- &core.EpochNotification{
				Host:         dexAddr,
				MarketID:     mktID,
				Notification: db.NewNotification(core.NoteTypeEpoch, "", "", db.Data),
				Epoch:        getEpoch(),
			}

			// randomize the balance
			if baseID != 141 && baseConnected { // komodo unsupported
				c.noteFeed <- randomBalanceNote(baseID)
			}
			if quoteID != 141 && quoteConnected { // komodo unsupported
				c.noteFeed <- randomBalanceNote(quoteID)
			}

			c.orderMtx.Lock()
			// Send limit orders as newly booked.
			for _, o := range c.epochOrders {
				miniOrder := o.Payload.(*core.MiniOrder)
				if miniOrder.Rate > 0 {
					miniOrder.Epoch = new(uint64)
					o.Action = msgjson.BookOrderRoute
					c.trySend(o)
					if miniOrder.Sell {
						c.sells[miniOrder.Token] = miniOrder
					} else {
						c.buys[miniOrder.Token] = miniOrder
					}
				}
			}
			c.epochOrders = nil
			c.orderMtx.Unlock()
		case <-tCtx.Done():
			break out
		}
	}
}

func TestServer(t *testing.T) {
	// Register dummy drivers for unimplemented assets.
	asset.Register(22, &TDriver{})  // mona
	asset.Register(28, &TDriver{})  // vtc
	asset.Register(141, &TDriver{}) // kmd
	asset.Register(3, &TDriver{})   // doge

	numBuys = 0
	numSells = 0
	feedPeriod = 2000 * time.Millisecond
	initialize := false
	register := true
	forceDisconnectWallet = true
	gapWidthFactor = 0.2

	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := dex.StdOutLogger("TEST", dex.LevelTrace)
	tCore := newTCore()

	if initialize {
		tCore.InitializeClient([]byte(""))
	}

	if register {
		// initialize is implied and forced if register = true.
		if !initialize {
			tCore.InitializeClient([]byte(""))
		}
		tCore.Register(new(core.RegisterForm))
	}

	s, err := New(tCore, ":54321", logger, true)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	cm := dex.NewConnectionMaster(s)
	err = cm.Connect(tCtx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	go tCore.runEpochs()
	cm.Wait()
}
