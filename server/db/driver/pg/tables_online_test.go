// +build pgonline

package pg

import (
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
	err = PrepareTables(archie.db, markets)
	if err != nil {
		t.Error(err)
	}

	// Cover the cases where the tables already exist (OK).
	err = PrepareTables(archie.db, markets)
	if err != nil {
		t.Error(err)
	}

	// Mutated existing market (not supported, will panic).
	mktConfig, _ = dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e8, EpochDuration, MarketBuyBuffer) // lot size change
	func() {
		defer func() {
			if recover() == nil {
				t.Error("PrepareTables should have paniced with lot size change.")
			}
		}()
		_ = PrepareTables(archie.db, []*dex.MarketInfo{mktConfig})
	}()

	// Add a new market.
	mktConfig, _ = dex.NewMarketInfoFromSymbols("dcr", "ltc", 1e9, EpochDuration, MarketBuyBuffer)
	err = PrepareTables(archie.db, []*dex.MarketInfo{mktConfig})
	if err != nil {
		t.Error(err)
	}
}
