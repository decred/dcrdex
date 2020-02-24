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
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	ordertest "decred.org/dcrdex/dex/order/test"
	"github.com/decred/slog"
)

var maxDelay = time.Second * 2

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
	return &core.Market{
		BaseID:      baseID,
		BaseSymbol:  base,
		QuoteID:     quoteID,
		QuoteSymbol: quote,
	}
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
			FeeRate: winfos[assetID].FeeRate,
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
			Name:       name,
			ConfigPath: "/home/you/." + lower + "/" + lower + ".conf",
		},
	}
}

var tMarkets = map[string][]*core.Market{
	"https://somedex.com": []*core.Market{
		mkMrkt("dcr", "btc"), mkMrkt("dcr", "ltc"), mkMrkt("doge", "mona"),
	},
	"https://thisdexwithalongname.com": []*core.Market{
		mkMrkt("dcr", "vtc"), mkMrkt("btc", "ltc"), mkMrkt("mona", "ltc"),
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
	reg      *core.Registration
	inited   bool
	mtx      sync.RWMutex
	wallets  map[uint32]*tWalletState
	balances map[uint32]uint64
}

func newTCore() *TCore {
	return &TCore{
		wallets: make(map[uint32]*tWalletState),
		balances: map[uint32]uint64{
			0:  uint64(randomMagnitude(7, 11)),
			2:  uint64(randomMagnitude(7, 11)),
			42: uint64(randomMagnitude(7, 11)),
		},
	}
}

func (c *TCore) Markets() map[string][]*core.Market { return tMarkets }

func (c *TCore) InitializeClient(pw string) error {
	randomDelay()
	c.inited = true
	return nil
}
func (c *TCore) PreRegister(dex string) (uint64, error) { return 1e8, nil }

func (c *TCore) Register(r *core.Registration) (error, <-chan error) {
	randomDelay()
	c.reg = r
	errChan := make(chan error, 1)
	errChan <- nil
	return nil, errChan
}
func (c *TCore) Login(string) ([]core.Negotiation, error) { return nil, nil }

func (c *TCore) Sync(dex string, base, quote uint32) (chan *core.BookUpdate, error) {
	return make(chan *core.BookUpdate), nil
}

// Book randomizes an order book.
func (c *TCore) Book(dex string, base, quote uint32) *core.OrderBook {
	midGap := randomMagnitude(0, 6)
	maxQty := randomMagnitude(-2, 4)
	// Set the market width to about 5% of midGap.
	marketWidth := 0.05 * midGap
	numPerSide := 80
	sells := make([]*core.MiniOrder, 0, numPerSide)
	buys := make([]*core.MiniOrder, 0, numPerSide)
	for i := 0; i < numPerSide; i++ {
		// For sells the rate must be larger than midGap.
		rate := midGap + rand.Float64()*marketWidth
		sells = append(sells, &core.MiniOrder{
			Rate: rate,
			// Find a random quantity on an exponential curve with a minimum of
			// e^-5 * maxQty ~= .0067 * maxQty
			Qty:   math.Exp(-rand.Float64()*5) * maxQty,
			Epoch: rand.Float32() > 0.8, // 1 in 5 are epoch orders.
		})
	}
	for i := 0; i < numPerSide; i++ {
		// For buys the rate must be smaller than midGap.
		rate := midGap - rand.Float64()*marketWidth
		buys = append(buys, &core.MiniOrder{
			Rate:  rate,
			Qty:   math.Exp(-rand.Float64()*5) * maxQty,
			Epoch: rand.Float32() > 0.8, // 1 in 5 are epoch orders.
		})
	}
	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })
	return &core.OrderBook{
		Buys:  buys,
		Sells: sells,
	}
}

func (c *TCore) Unsync(dex string, base, quote uint32) {}

func (c *TCore) Balance(uint32) (uint64, error) {
	return uint64(rand.Float64() * math.Pow10(rand.Intn(6)+6)), nil
}

var winfos = map[uint32]*asset.WalletInfo{
	0: &asset.WalletInfo{
		FeeRate: 2,
		Units:   "Satoshis",
		Name:    "Bitcoin",
	},
	2: &asset.WalletInfo{
		FeeRate: 100,
		Units:   "litoshi", // Plural seemingly has no 's'.
		Name:    "Litecoin",
	},
	42: &asset.WalletInfo{
		FeeRate: 10,
		Units:   "atoms",
		Name:    "Decred",
	},
}

func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	if c.wallets[assetID] == nil {
		return nil
	}
	return &core.WalletState{
		Symbol:  unbip(assetID),
		AssetID: assetID,

		Open:    true,
		Running: true,
		Address: ordertest.RandomAddress(),
		Balance: c.balances[assetID],
		FeeRate: winfos[assetID].FeeRate,
		Units:   winfos[assetID].Units,
	}
}

func (c *TCore) CreateWallet(appPW, walletPW string, form *core.WalletForm) error {
	randomDelay()
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.wallets[form.AssetID] = &tWalletState{
		running: true,
	}
	return nil
}

func (c *TCore) OpenWallet(assetID uint32, pw string) error {
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
			FeeRate: winfos[assetID].FeeRate,
			Units:   winfos[assetID].Units,
		})
	}
	return stats
}

func (c *TCore) User() *core.User {
	user := &core.User{
		Markets:     tMarkets,
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
	}
}

func (c *TCore) Withdraw(pw string, assetID uint32, value uint64) (asset.Coin, error) {
	return &tCoin{id: []byte{0xde, 0xc7, 0xed}}, nil
}

func TestServer(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	time.AfterFunc(time.Minute*60, func() { shutdown() })
	s, err := New(newTCore(), ":54321", logger, true)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	go s.Run(ctx)
	<-ctx.Done()
}
