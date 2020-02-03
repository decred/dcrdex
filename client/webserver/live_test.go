// +build live
// Run a test server with
// go test -v -tags live -run Server -timeout 60m
// test server will run for 1 hour and serve randomness.

package webserver

import (
	"context"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

var maxDelay = time.Second * 2

func randomDelay() {
	time.Sleep(time.Duration(rand.Float64() * float64(maxDelay)))
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

var tMarkets = map[string][]*core.Market{
	"https://somedex.com": []*core.Market{
		mkMrkt("dcr", "btc"), mkMrkt("dcr", "ltc"), mkMrkt("doge", "mona"),
	},
	"https://thisdexwithalongname.com": []*core.Market{
		mkMrkt("dcr", "vtc"), mkMrkt("btc", "ltc"), mkMrkt("mona", "ltc"),
	},
}

type TCore struct {
	reg     *core.Registration
	inited  bool
	has     bool
	running bool
	open    bool
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
	// Pick an order of magnitude for the midGap price between -2 and 3
	rateMagnitude := rand.Intn(6)     // Don't subtract yet
	qtyMagnitude := 3 - rateMagnitude // larger rate -> smaller qty
	rateMagnitude -= 2

	// Randomize mid-gap.
	mantissa := rand.Float64() * 10
	midGap := mantissa * math.Pow10(rateMagnitude)
	// Randomize a depth factor.
	mantissa = rand.Float64() * 10
	maxQty := mantissa * math.Pow10(qtyMagnitude) * 10
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

func (c *TCore) WalletStatus(assetID uint32) (has, running, open bool) {
	return c.has, c.running, c.open
}

func (c *TCore) CreateWallet(form *core.WalletForm) error {
	randomDelay()
	c.has = true
	c.running = true
	c.open = true
	return nil
}

func (c *TCore) OpenWallet(assetID uint32, pw string) error {
	return nil
}

func (c *TCore) Wallets() []*core.WalletStatus {
	if c.has {
		return []*core.WalletStatus{
			{
				Symbol:  "dcr",
				AssetID: 42,
				Open:    true,
				Running: true,
			},
		}
	}
	return nil
}

func (c *TCore) User() *core.User {
	user := &core.User{
		Markets:     tMarkets,
		Initialized: c.inited,
	}
	if c.has {
		user.Wallets = []*core.WalletStatus{
			{
				Symbol:  "dcr",
				AssetID: 42,
				Open:    true,
				Running: true,
			},
		}
	}
	return user
}

func TestServer(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	time.AfterFunc(time.Minute*60, func() { shutdown() })
	s, err := New(&TCore{}, ":54321", logger, true)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	go s.Run(ctx)
	<-ctx.Done()
}
