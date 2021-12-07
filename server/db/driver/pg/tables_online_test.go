//go:build pgonline
// +build pgonline

package pg

import (
	"context"
	"testing"

	"decred.org/dcrdex/dex"
)

func TestCheckCurrentTimeZone(t *testing.T) {
	currentTZ, err := checkCurrentTimeZone(archie.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Set time zone: %v", currentTZ)
}

func TestPrepareTables(t *testing.T) {
	if err := nukeAll(archie.db); err != nil {
		t.Fatal(err)
	}

	// valid market
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e9, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*dex.MarketInfo{mktConfig}
	err = prepareTables(context.Background(), archie.db, markets)
	if err != nil {
		t.Error(err)
	}

	// Cover the cases where the tables already exist (OK). This hits the
	// upgradeDB path, which returns early with current == dbVersion.
	err = prepareTables(context.Background(), archie.db, markets)
	if err != nil {
		t.Error(err)
	}

	// Mutated existing market (not supported, will panic).
	mktConfig, _ = dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e8, EpochDuration, MarketBuyBuffer) // lot size change
	func() {
		defer func() {
			if recover() == nil {
				t.Error("prepareTables should have paniced with lot size change.")
			}
		}()
		_ = prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig})
	}()

	// Add a new market.
	mktConfig, _ = dex.NewMarketInfoFromSymbols("dcr", "ltc", 1e9, EpochDuration, MarketBuyBuffer)
	err = prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig})
	if err != nil {
		t.Error(err)
	}
}
