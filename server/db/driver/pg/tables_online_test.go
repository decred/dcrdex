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
	mktConfig, err := types.NewMarketInfoFromSymbols("DCR", "BTC", 1e9)
	if err != nil {
		t.Fatal(err)
	}

	markets := []*types.MarketInfo{mktConfig}
	err = PrepareTables(archie.db, markets)
	if err != nil {
		t.Error(err)
	}
}
