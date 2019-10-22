package types

import (
	"os"
	"testing"

	"github.com/decred/dcrdex/server/asset"
)

const LotSize = uint64(10_000_000_000)

var (
	AssetDCR uint32
	AssetBTC uint32
)

func TestOrderStatus_String(t *testing.T) {
	// spot check a valid status
	bookedStatusStr := OrderStatusBooked.String()
	if bookedStatusStr != "booked" {
		t.Errorf(`OrderStatusBooked.String() = %s (!= "booked")`, bookedStatusStr)
	}

	unknownStatusStr := OrderStatusUnknown.String()
	if unknownStatusStr != "unknown" {
		t.Errorf(`OrderStatusBooked.String() = %s (!= "unknown")`, unknownStatusStr)
	}

	// Check a fake and invalid status.
	os := OrderStatus(12345)
	defer func() {
		if recover() == nil {
			t.Fatalf("OrderStatus.String() should panic for invalid status")
		}
	}()
	_ = os.String()
}

func TestMain(m *testing.M) {
	var ok bool
	AssetDCR, ok = asset.BipSymbolID("dcr")
	if !ok {
		os.Exit(1)
	}
	AssetBTC, ok = asset.BipSymbolID("btc")
	if !ok {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestNewMarketName(t *testing.T) {
	mkt, err := MarketName(AssetDCR, AssetBTC)
	if err != nil {
		t.Fatalf("MarketName(%d,%d) failed: %v", AssetDCR, AssetBTC, err)
	}
	if mkt != "dcr_btc" {
		t.Errorf("Incorrect market name. Got %s, expected %s", mkt, "dcr_btc")
	}
}

func TestNewMarketInfo(t *testing.T) {
	// ok
	mktInfo, err := NewMarketInfo(AssetDCR, AssetBTC, LotSize)
	if err != nil {
		t.Errorf("NewMarketInfoFromSymbols failed: %v", err)
	}
	if mktInfo.Name != "dcr_btc" {
		t.Errorf("market Name incorrect. got %s, expected %s", mktInfo.Name, "dcr_btc")
	}

	// bad base
	_, err = NewMarketInfo(8765678, AssetBTC, LotSize)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent base asset")
	}

	// bad quote
	_, err = NewMarketInfo(AssetDCR, 8765678, LotSize)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent quote asset")
	}
}

func TestNewMarketInfoFromSymbols(t *testing.T) {
	// ok
	mktInfo, err := NewMarketInfoFromSymbols("dcr", "btc", LotSize)
	if err != nil {
		t.Errorf("NewMarketInfoFromSymbols failed: %v", err)
	}
	if mktInfo.Name != "dcr_btc" {
		t.Errorf("market Name incorrect. got %s, expected %s", mktInfo.Name, "dcr_btc")
	}

	// bad base
	_, err = NewMarketInfoFromSymbols("super fake asset", "btc", LotSize)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent base asset")
	}

	// bad quote
	_, err = NewMarketInfoFromSymbols("dcr", "btc not BTC or bTC", LotSize)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent quote asset")
	}
}
