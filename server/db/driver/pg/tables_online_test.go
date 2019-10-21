// +build pgonline

package pg

import (
	"testing"

	"github.com/decred/dcrdex/server/market/types"
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
	mktConfig, err := types.NewMarketInfoFromSymbols("DCR", "BTC", 1e9)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*types.MarketInfo{mktConfig}
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
	mktConfig, _ = types.NewMarketInfoFromSymbols("DCR", "BTC", 1e8) // lot size change
	func() {
		defer func() {
			if recover() == nil {
				t.Error("PrepareTables should have paniced with lot size change.")
			}
		}()
		_ = PrepareTables(archie.db, []*types.MarketInfo{mktConfig})
	}()

	// Add a new market.
	mktConfig, _ = types.NewMarketInfoFromSymbols("dcr", "ltc", 1e9)
	err = PrepareTables(archie.db, []*types.MarketInfo{mktConfig})
	if err != nil {
		t.Error(err)
	}
}
