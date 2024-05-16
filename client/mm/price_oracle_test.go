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
	"github.com/davecgh/go-spew/spew"

	"decred.org/dcrdex/dex"
)

func TestPriceOracle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := dex.StdOutLogger("TEST", dex.LevelTrace)

	oracle := newPriceOracle(ctx, logger)

	markets := []*marketPair{
		{baseID: 42, quoteID: 0},
		{baseID: 60, quoteID: 0},
	}
	for _, mkt := range markets {
		price, oracleReport, err := oracle.getOracleInfo(mkt.baseID, mkt.quoteID)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("~~~~~ Get oracle info result:", mkt)
		fmt.Printf("Price: %f, OracleReport: %s \n", price, spew.Sdump(oracleReport))

		// Should be the same because cached
		priceOnly := oracle.getMarketPrice(mkt.baseID, mkt.quoteID)
		if priceOnly != price {
			t.Fatalf("Expected price %f, got %f", price, priceOnly)
		}
	}
}

func TestAutoSyncPriceOracle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := dex.StdOutLogger("TEST", dex.LevelTrace)

	oracle := newPriceOracle(ctx, logger)

	markets := []*marketPair{
		{baseID: 42, quoteID: 0},
		{baseID: 60, quoteID: 0},
	}
	for _, mkt := range markets {
		err := oracle.startAutoSyncingMarket(mkt.baseID, mkt.quoteID)
		if err != nil {
			t.Fatal(err)
		}

		err = oracle.startAutoSyncingMarket(mkt.baseID, mkt.quoteID)
		if err != nil {
			t.Fatal(err)
		}

		price, oracleReport, err := oracle.getOracleInfo(mkt.baseID, mkt.quoteID)
		if err != nil {
			t.Fatal(err)
		}

		fmt.Println("~~~~~ Get oracle info result:", mkt)
		fmt.Printf("Price: %f, OracleReport: %s \n", price, spew.Sdump(oracleReport))

		oracle.stopAutoSyncingMarket(mkt.baseID, mkt.quoteID)
		if !oracle.marketIsAutoSyncing(mkt.baseID, mkt.quoteID) {
			t.Fatalf("Expected market to be auto-syncing")
		}
		oracle.stopAutoSyncingMarket(mkt.baseID, mkt.quoteID)
		if oracle.marketIsAutoSyncing(mkt.baseID, mkt.quoteID) {
			t.Fatalf("Expected market to not be auto-syncing")
		}
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
