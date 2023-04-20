//go:build live

package mm

import (
	"context"
	"fmt"
	"testing"
	"time"

	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
	_ "decred.org/dcrdex/client/asset/eth"

	"decred.org/dcrdex/dex"
)

func TestPriceOracle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := dex.StdOutLogger("TEST", dex.LevelTrace)

	markets := []*mkt{
		&mkt{base: 42, quote: 0},
		&mkt{base: 60, quote: 0},
	}

	oracle, err := newPriceOracle(ctx, markets, logger)
	if err != nil {
		t.Fatalf("error creating price oracle: %v", err)
	}

	for _, mkt := range markets {
		price := oracle.getMarketPrice(mkt.base, mkt.quote)
		fmt.Printf("market %d - %d, price: %v\n", mkt.base, mkt.quote, price)
	}
}

func testSpreader(t *testing.T, spreader Spreader, baseSymbol, quoteSymbol string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	sell, buy, err := spreader(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Fetched: sell = %f, buy = %f \n", sell, buy)
}

func TestFetchBinanceSpread(t *testing.T) {
	testSpreader(t, fetchBinanceSpread, "dcr", "btc")
}

func TestFetchCoinbaseSpread(t *testing.T) {
	testSpreader(t, fetchCoinbaseSpread, "btc", "usd")
}x

func TestFetchBittrexSpread(t *testing.T) {
	testSpreader(t, fetchBittrexSpread, "dcr", "btc")
}

func TestFetchHitBTCSpread(t *testing.T) {
	testSpreader(t, fetchHitBTCSpread, "dcr", "btc")
}

func TestFetchEXMOSpread(t *testing.T) {
	testSpreader(t, fetchEXMOSpread, "dcr", "btc")
}
