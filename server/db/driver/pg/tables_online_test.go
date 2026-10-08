//go:build pgonline

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
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e9, RateStep, EpochDuration, 0, MarketBuyBuffer)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*dex.MarketInfo{mktConfig}
	if err := prepareTables(context.Background(), archie.db, markets); err != nil {
		t.Error(err)
	}

	// Cover the cases where the tables already exist (OK). This hits the
	// upgradeDB path, which returns early with current == dbVersion.
	if err := prepareTables(context.Background(), archie.db, markets); err != nil {
		t.Error(err)
	}

	// Mutated existing market. The lot size metadata update happens here, but
	// book cleanup is handled by market startup cleanup.
	mktConfig, err = dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e8, RateStep, EpochDuration, 0, MarketBuyBuffer) // lot size change
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig}); err != nil {
		t.Error(err)
	}

	// Add a new market.
	mktConfig, _ = dex.NewMarketInfoFromSymbols("dcr", "ltc", 1e9, RateStep, EpochDuration, 0, MarketBuyBuffer)
	if err := prepareTables(context.Background(), archie.db, []*dex.MarketInfo{mktConfig}); err != nil {
		t.Error(err)
	}
}

func TestUpdateLotSize(t *testing.T) {
	if err := nukeAll(archie.db); err != nil {
		t.Fatal(err)
	}

	// valid market
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", 1e9, RateStep, EpochDuration, 0, MarketBuyBuffer)
	if err != nil {
		t.Fatal(err)
	}

	// Create new tables and schemas.
	markets := []*dex.MarketInfo{mktConfig}
	if err := prepareTables(context.Background(), archie.db, markets); err != nil {
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
