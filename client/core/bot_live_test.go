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

func TestOracleMarketReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := &SupportedAsset{Symbol: "dcr", Name: "Decred"}
	q := &SupportedAsset{Symbol: "btc", Name: "Bitcoin"}

	oracles, err := oracleMarketReport(ctx, b, q, dex.StdOutLogger("T", dex.LevelTrace))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range oracles {
		fmt.Printf("Success: %s price = %f \n", o.Host, (o.BestBuy+o.BestSell)/2)
	}

}
