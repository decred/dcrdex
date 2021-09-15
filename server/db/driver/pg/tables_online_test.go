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
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e9, RateStep, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*dex.MarketInfo{mktConfig}
	purgeMkts, err := prepareTables(context.Background(), archie.db, markets)
	if err != nil {
		t.Error(err)
	}
	if purgeMkts != nil {
		t.Error("expected no purged markets for new table")
	}

	// Cover the cases where the tables already exist (OK). This hits the
	// upgradeDB path, which returns early with current == dbVersion.
	purgeMkts, err = prepareTables(context.Background(), archie.db, markets)
	if err != nil {
		t.Error(err)
	}
	if purgeMkts != nil {
		t.Error("expected no purged markets for no config changes")
	}

	// Mutated existing market. Should return mutated markets in purge
	// returns.
	mktConfig, err = dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e8, RateStep, EpochDuration, MarketBuyBuffer) // lot size change
	if err != nil {
		t.Fatal(err)
	}
	purgeMkts, err = prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig})
	if err != nil {
		t.Error(err)
	}
	if len(purgeMkts) != 1 || purgeMkts[0] != "dcr_btc" {
		t.Error("expected a dcr_btc purge markets return for changed lot size")
	}

	// Add a new market.
	mktConfig, _ = dex.NewMarketInfoFromSymbols("dcr", "ltc", 1e9, RateStep, EpochDuration, MarketBuyBuffer)
	purgeMkts, err = prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig})
	if err != nil {
		t.Error(err)
	}
	if purgeMkts != nil {
		t.Error("expected no purged markets for new market")
	}
}

func TestUpdateLotSize(t *testing.T) {
	if err := nukeAll(archie.db); err != nil {
		t.Fatal(err)
	}

	// valid market
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e9, RateStep, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*dex.MarketInfo{mktConfig}
	_, err = prepareTables(context.Background(), archie.db, markets)
	if err != nil {
		t.Error(err)
	}

	mkts, err := loadMarkets(archie.db, marketsTableName)
	if err != nil {
		t.Error(err)
	}
	if mkts[0].LotSize != 1e9 {
		t.Error("unexpected lot size before updating")
	}

	err = updateLotSize(archie.db, publicSchema, "dcr_btc", 1337)
	if err != nil {
		t.Error(err)
	}

	mkts, err = loadMarkets(archie.db, marketsTableName)
	if err != nil {
		t.Error(err)
	}
	if mkts[0].LotSize != 1337 {
		t.Error("lot size is not 1337 after updating")
	}
}
