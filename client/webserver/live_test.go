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
	"github.com/decred/slog"
)

var tMarkets = []*core.MarketInfo{
	{
		DEX:     "https://somedex.com",
		Markets: []string{"DCR-BTC", "DCR-LTC", "DOGE-MONA"},
	},
	{
		DEX:     "https://thisdexwithalongname.com",
		Markets: []string{"DCR-VTC", "BTC-LTC", "MONA-LTC"},
	},
}

type TCore struct {
	reg *core.Registration
}

func (c *TCore) ListMarkets() []*core.MarketInfo { return tMarkets }
func (c *TCore) Register(r *core.Registration) error {
	c.reg = r
	return nil
}
func (c *TCore) Login(dex, pw string) error { return nil }
func (c *TCore) Sync(dex, mkt string) (*core.OrderBook, chan *core.BookUpdate, error) {
	// Pick an order of magnitude for the midGap price between -2 and 3
	rateMagnitude := rand.Intn(6)         // Don't subtract yet
	qtyMagnitude := 5 - rateMagnitude - 2 // larger rate -> smaller qty
	rateMagnitude -= 2

	mantissa := rand.Float64() * 10
	midGap := mantissa * math.Pow10(rateMagnitude)
	mantissa = rand.Float64() * 10
	maxQty := mantissa * math.Pow10(qtyMagnitude) * 10
	// Set the market width to about 5% of midGap
	marketWidth := 0.05 * midGap
	numPerSide := 80
	sells := make([]*core.MiniOrder, 0, numPerSide)
	buys := make([]*core.MiniOrder, 0, numPerSide)
	for i := 0; i < numPerSide; i++ {
		// For sells the rate must be larger than midGap.
		rate := midGap + rand.Float64()*marketWidth
		sells = append(sells, &core.MiniOrder{
			Rate:  rate,
			Qty:   math.Exp(-rand.Float64()*10) * maxQty,
			Epoch: rand.Float32() > 0.8, // 1 in 5 are epoch orders.
		})
	}
	for i := 0; i < numPerSide; i++ {
		// For buys the rate must be smaller than midGap.
		rate := midGap - rand.Float64()*marketWidth
		buys = append(buys, &core.MiniOrder{
			Rate:  rate,
			Qty:   math.Exp(-rand.Float64()*10) * maxQty,
			Epoch: rand.Float32() > 0.8, // 1 in 5 are epoch orders.
		})
	}
	sort.Slice(buys, func(i, j int) bool { return buys[i].Rate > buys[j].Rate })
	sort.Slice(sells, func(i, j int) bool { return sells[i].Rate < sells[j].Rate })
	return &core.OrderBook{
		Buys:  buys,
		Sells: sells,
	}, make(chan *core.BookUpdate), nil
}

func (c *TCore) Unsync(dex, mkt string) {}

func (c *TCore) Balance(string) (float64, error) {
	return rand.Float64() * 10 * math.Pow10(rand.Intn(6)-2), nil
}

func TestServer(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())
	time.AfterFunc(time.Minute*59, func() { shutdown() })
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	logger.SetLevel(slog.LevelTrace)
	time.AfterFunc(time.Minute*60, func() { shutdown() })
	go func() {
		Run(ctx, &TCore{}, ":54321", logger, true)
	}()
	<-ctx.Done()
}
