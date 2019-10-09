// +build pgonline

package pg

import (
	"testing"

	"github.com/decred/dcrdex/server/db"
)

func TestCheckCurrentTimeZone(t *testing.T) {
	currentTZ, err := checkCurrentTimeZone(archie.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Set time zone: %v", currentTZ)
}

func TestPrepareTables(t *testing.T) {
	mktConfig, err := db.NewMarketInfoFromSymbols("DCR", "BTC", 1e9)
	if err != nil {
		t.Fatal(err)
	}

	markets := []*db.MarketInfo{mktConfig}
	err = PrepareTables(archie.db, markets)
	if err != nil {
		t.Error(err)
	}
}
