// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"os"
	"testing"
)

const (
	LotSize         = uint64(10_000_000_000)
	EpochDuration   = uint64(10_000)
	MarketBuyBuffer = 1.1
)

var (
	AssetDCR uint32
	AssetBTC uint32
)

func TestMain(m *testing.M) {
	var ok bool
	AssetDCR, ok = BipSymbolID("dcr")
	if !ok {
		os.Exit(1)
	}
	AssetBTC, ok = BipSymbolID("btc")
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
	mktInfo, err := NewMarketInfo(AssetDCR, AssetBTC, LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Errorf("NewMarketInfoFromSymbols failed: %v", err)
	}
	if mktInfo.Name != "dcr_btc" {
		t.Errorf("market Name incorrect. got %s, expected %s", mktInfo.Name, "dcr_btc")
	}

	// bad base
	_, err = NewMarketInfo(8765678, AssetBTC, LotSize, EpochDuration, MarketBuyBuffer)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent base asset")
	}

	// bad quote
	_, err = NewMarketInfo(AssetDCR, 8765678, LotSize, EpochDuration, MarketBuyBuffer)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent quote asset")
	}
}

func TestNewMarketInfoFromSymbols(t *testing.T) {
	// ok
	mktInfo, err := NewMarketInfoFromSymbols("dcr", "btc", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Errorf("NewMarketInfoFromSymbols failed: %v", err)
	}
	if mktInfo.Name != "dcr_btc" {
		t.Errorf("market Name incorrect. got %s, expected %s", mktInfo.Name, "dcr_btc")
	}

	// bad base
	_, err = NewMarketInfoFromSymbols("super fake asset", "btc", LotSize, EpochDuration, MarketBuyBuffer)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent base asset")
	}

	// bad quote
	_, err = NewMarketInfoFromSymbols("dcr", "btc not BTC or bTC", LotSize, EpochDuration, MarketBuyBuffer)
	if err == nil {
		t.Errorf("NewMarketInfoFromSymbols succeeded for non-existent quote asset")
	}
}
