//go:build botlive

package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

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
}

func TestFetchBittrexSpread(t *testing.T) {
	testSpreader(t, fetchBittrexSpread, "dcr", "btc")
}

func TestFetchHitBTCSpread(t *testing.T) {
	testSpreader(t, fetchHitBTCSpread, "dcr", "btc")
}

func TestFetchEXMOSpread(t *testing.T) {
	testSpreader(t, fetchEXMOSpread, "dcr", "btc")
}

func TestMarketAveragedPrice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &makerBot{
		Core: &Core{},
		base: &makerAsset{
			Asset: &dex.Asset{Symbol: "dcr"},
			Name:  "Decred",
		},
		quote: &makerAsset{
			Asset: &dex.Asset{Symbol: "btc"},
			Name:  "Bitcoin",
		},
		log: dex.StdOutLogger("T", dex.LevelTrace),
		market: &Market{
			Name: "dcr_btc",
		},
	}

	p, err := m.marketAveragedPrice(ctx)
	if err != nil {
		t.Fatal(err)
	} else {
		fmt.Printf("Success: price = %f \n", p)
	}
}
