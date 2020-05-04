// +build live
// Run a test server with
// go test -v -tags live -run Server -timeout 60m
// test server will run for 1 hour and serve randomness.

package webserver

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"github.com/decred/slog"
)

var (
	tCtx          context.Context
	maxDelay      = time.Second * 2
	epochDuration = time.Second * 120 // milliseconds
	feedPeriod    = time.Second * 10
)

func randomDelay() {
	time.Sleep(time.Duration(rand.Float64() * float64(maxDelay)))
}

// A random number with a random order of magnitude.
func randomMagnitude(low, high int) float64 {
	exponent := rand.Intn(high-low) + low
	mantissa := rand.Float64() * 10
	return mantissa * math.Pow10(exponent)
}

func mkMrkt(base, quote string) *core.Market {
	baseID, _ := dex.BipSymbolID(base)
	quoteID, _ := dex.BipSymbolID(quote)
	market := &core.Market{
		Name:            fmt.Sprintf("%s-%s", base, quote),
		BaseID:          baseID,
		BaseSymbol:      base,
		QuoteID:         quoteID,
		QuoteSymbol:     quote,
		MarketBuyBuffer: rand.Float64() + 1,
	}
	orderCount := rand.Intn(5)
	qty := uint64(randomMagnitude(7, 11))
	for i := 0; i < orderCount; i++ {
		market.Orders = append(market.Orders, &core.Order{
			ID:      ordertest.RandomOrderID().String(),
			Type:    order.OrderType(rand.Intn(2) + 1),
			Stamp:   encode.UnixMilliU(time.Now()) - uint64(rand.Float64()*86_400_000),
			Rate:    uint64(randomMagnitude(-2, 4)),
			Qty:     qty,
			Sell:    rand.Intn(2) > 0,
			Filled:  uint64(rand.Float64() * float64(qty)),
			Matches: nil,
		})
	}

	return market
}

func mkSupportedAsset(symbol string, state *tWalletState, bal uint64) *core.SupportedAsset {
	assetID, _ := dex.BipSymbolID(symbol)
	var wallet *core.WalletState
	if state != nil {
		wallet = &core.WalletState{
			Symbol:  unbip(assetID),
			AssetID: assetID,
			Open:    state.open,
			Running: state.running,
			Address: ordertest.RandomAddress(),
			Balance: bal,
			FeeRate: winfos[assetID].DefaultFeeRate,
			Units:   winfos[assetID].Units,
		}
	}
	name := winfos[assetID].Name
	lower := strings.ToLower(name)
	return &core.SupportedAsset{
		ID:     assetID,
		Symbol: symbol,
		Wallet: wallet,
		Info: &asset.WalletInfo{
			Name:              name,
			DefaultConfigPath: "/home/you/." + lower + "/" + lower + ".conf",
		},
	}
}

func mkDexAsset(symbol string) *dex.Asset {
	assetID, _ := dex.BipSymbolID(symbol)
	assetOrder := rand.Intn(5) + 6
	return &dex.Asset{
		ID:       assetID,
		Symbol:   symbol,
		LotSize:  uint64(math.Pow10(assetOrder)) * uint64(rand.Intn(10)),
		RateStep: uint64(math.Pow10(assetOrder-2)) * uint64(rand.Intn(10)),
		FeeRate:  uint64(rand.Intn(10) + 1),
		SwapSize: uint64(rand.Intn(150) + 150),
		SwapConf: uint32(rand.Intn(5) + 2),
		FundConf: uint32(rand.Intn(5) + 2),
	}
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
		Epoch: epochIdx,
	}
}

var tExchanges = map[string]*core.Exchange{
	"https://somedex.com": {
		URL: "https://somedex.com",
		Assets: map[uint32]*dex.Asset{
			0:  mkDexAsset("btc"),
			2:  mkDexAsset("ltc"),
			42: mkDexAsset("dcr"),
			22: mkDexAsset("mona"),
			3:  mkDexAsset("doge"),
		},
		Markets: map[string]*core.Market{
			mkid(42, 0): mkMrkt("dcr", "btc"),
			mkid(42, 2): mkMrkt("dcr", "ltc"),
			mkid(3, 22): mkMrkt("doge", "mona"),
		},
	},
	"https://thisdexwithalongname.com": {
		URL: "https://thisdexwithalongname.com",
		Assets: map[uint32]*dex.Asset{
			0:  mkDexAsset("btc"),
			2:  mkDexAsset("ltc"),
			42: mkDexAsset("dcr"),
			22: mkDexAsset("mona"),
			28: mkDexAsset("vtc"),
		},
		Markets: map[string]*core.Market{
			mkid(42, 28): mkMrkt("dcr", "vtc"),
			mkid(0, 2):   mkMrkt("btc", "ltc"),
			mkid(22, 2):  mkMrkt("mona", "ltc"),
		},
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

func (c *tCoin) Redeem() dex.Bytes {
	return nil
}

type tWalletState struct {
	open    bool
	running bool
}

type TCore struct {
	reg      *core.RegisterForm
	inited   bool
	mtx      sync.RWMutex
	wallets  map[uint32]*tWalletState
	balances map[uint32]uint64
	midGap   float64
	maxQty   float64
	feed     *core.BookFeed
	killFeed context.CancelFunc
	buys     map[string]*core.MiniOrder
	sells    map[string]*core.MiniOrder
	noteFeed chan core.Notification
}

func newTCore() *TCore {
	return &TCore{
		wallets: make(map[uint32]*tWalletState),
		balances: map[uint32]uint64{
			0:  uint64(randomMagnitude(7, 11)),
			2:  uint64(randomMagnitude(7, 11)),
			42: uint64(randomMagnitude(7, 11)),
			22: uint64(randomMagnitude(7, 11)),
			3:  uint64(randomMagnitude(7, 11)),
			28: uint64(randomMagnitude(7, 11)),
		},
		noteFeed: make(chan core.Notification, 1),
	}
}

func (c *TCore) Exchanges() map[string]*core.Exchange { return tExchanges }

func (c *TCore) InitializeClient(pw []byte) error {
	randomDelay()
	c.inited = true
	return nil
}
func (c *TCore) PreRegister(form *core.PreRegisterForm) (uint64, error) {
	return 1e8, nil
}

func (c *TCore) Register(r *core.RegisterForm) error {
	randomDelay()
	c.reg = r
	return nil
}
func (c *TCore) Login([]byte) ([]*db.Notification, error) { return nil, nil }

func (c *TCore) Sync(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error) {
	c.midGap = randomMagnitude(-2, 4)
	c.maxQty = randomMagnitude(-2, 4)

	if c.feed != nil {
		c.killFeed()
	}

	c.feed = core.NewBookFeed(func(*core.BookFeed) {})
	var ctx context.Context
	ctx, c.killFeed = context.WithCancel(tCtx)
	trySend := func(u *core.BookUpdate) {
		select {
		case c.feed.C <- u:
		default:
		}
	}
	go func() {
	out:
		for {
			select {
			case <-time.NewTicker(feedPeriod).C:
				// Send a random order to the order feed. Slighly biased away from
				// unbook_order and towards book_order.
				r := rand.Float32()
				switch {
				case r < 0.33:
					// Epoch order
					trySend(&core.BookUpdate{
						Action: msgjson.EpochOrderRoute,
						Order:  randomOrder(rand.Float32() < 0.5, c.maxQty, c.midGap, 0.05*c.midGap, true),
					})
				case r < 0.76:
					// Book order
					sell := rand.Float32() < 0.5
					ord := randomOrder(sell, c.maxQty, c.midGap, 0.05*c.midGap, false)
					side := c.buys
					if sell {
						side = c.sells
					}
					side[ord.Token] = ord
					trySend(&core.BookUpdate{
						Action: msgjson.BookOrderRoute,
						Order:  ord,
					})
				default:
					// Unbook order
					sell := rand.Float32() < 0.5
					side := c.buys
					if sell {
						side = c.sells
					}
					var tkn string
					for tkn = range side {
						break
					}
					if tkn == "" {
						continue
					}
					delete(side, tkn)

					trySend(&core.BookUpdate{
						Action: msgjson.UnbookOrderRoute,
						Order:  &core.MiniOrder{Token: tkn},
					})
				}
			case <-ctx.Done():
				break out
			}
		}
	}()
	return c.book(), c.feed, nil
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
	marketWidth := 0.05 * midGap
	var buys, sells []*core.MiniOrder
	c.buys = make(map[string]*core.MiniOrder, numBuys)
	c.sells = make(map[string]*core.MiniOrder, numSells)
	for i := 0; i < numSells; i++ {
		ord := randomOrder(true, maxQty, midGap, marketWidth, false)
		sells = append(sells, ord)
		c.sells[ord.Token] = ord
	}
	for i := 0; i < numBuys; i++ {
		// For buys the rate must be smaller than midGap.
		ord := randomOrder(false, maxQty, midGap, marketWidth, false)
		buys = append(buys, ord)
		c.buys[ord.Token] = ord
	}
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

func (c *TCore) Balance(uint32) (uint64, error) {
	return uint64(rand.Float64() * math.Pow10(rand.Intn(6)+6)), nil
}

func (c *TCore) AckNotes(ids []dex.Bytes) {}

var winfos = map[uint32]*asset.WalletInfo{
	0: {
		DefaultFeeRate: 2,
		Units:          "Satoshis",
		Name:           "Bitcoin",
	},
	2: {
		DefaultFeeRate: 100,
		Units:          "litoshi", // Plural seemingly has no 's'.
		Name:           "Litecoin",
	},
	42: {
		DefaultFeeRate: 10,
		Units:          "atoms",
		Name:           "Decred",
	},
	22: {
		DefaultFeeRate: 50,
		Units:          "atoms",
		Name:           "Monacoin",
	},
	3: {
		DefaultFeeRate: 1000,
		Units:          "atoms",
		Name:           "Dogecoin",
	},
	28: {
		DefaultFeeRate: 20,
		Units:          "Satoshis",
		Name:           "Vertcoin",
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
		FeeRate: winfos[assetID].DefaultFeeRate,
		Units:   winfos[assetID].Units,
	}
}

func (c *TCore) CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error {
	randomDelay()
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.wallets[form.AssetID] = &tWalletState{
		running: true,
		open:    true,
	}
	return nil
}

func (c *TCore) OpenWallet(assetID uint32, pw []byte) error {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	wallet := c.wallets[assetID]
	if wallet == nil {
		return fmt.Errorf("attempting to open non-existent test wallet")
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
		return fmt.Errorf("attempting to open non-existent test wallet")
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
			FeeRate: winfos[assetID].DefaultFeeRate,
			Units:   winfos[assetID].Units,
		})
	}
	return stats
}

func (c *TCore) User() *core.User {
	user := &core.User{
		Exchanges:   tExchanges,
		Initialized: c.inited,
		Assets:      c.SupportedAssets(),
	}
	return user
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

func (c *TCore) Withdraw(pw []byte, assetID uint32, value uint64) (asset.Coin, error) {
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
		ID:    ordertest.RandomOrderID().String(),
		Type:  oType,
		Stamp: encode.UnixMilliU(time.Now()),
		Rate:  form.Rate,
		Qty:   form.Qty,
		Sell:  form.Sell,
	}, nil
}

func (c *TCore) Cancel(pw []byte, sid string) error {
	for _, xc := range tExchanges {
		for _, mkt := range xc.Markets {
			for _, ord := range mkt.Orders {
				if ord.ID == sid {
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
			c.noteFeed <- &core.EpochNotification{
				Notification: db.NewNotification("epoch", "", "", db.Data),
				Epoch:        getEpoch(),
			}
		case <-tCtx.Done():
			break out
		}
	}
}

func TestServer(t *testing.T) {
	numBuys = 0
	numSells = 0
	feedPeriod = 5000 * time.Millisecond
	register := true

	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	time.AfterFunc(time.Minute*60, func() { shutdown() })
	tCore := newTCore()

	if register {
		tCore.InitializeClient([]byte(""))
		tCore.Register(new(core.RegisterForm))
	}

	s, err := New(tCore, ":54321", logger, true)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	go s.Run(tCtx)
	go tCore.runEpochs()
	<-tCtx.Done()
}
