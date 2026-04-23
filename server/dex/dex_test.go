package dex

import (
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
)

// TestLoadMarketConfAdaptor exercises the adaptor-market parsing
// added to loadMarketConf: explicit "adaptor" swapType requires
// scriptableAsset and lockBlocks; the string scriptableAsset is
// resolved to a BIP-44 ID matching base or quote; defaults stay
// HTLC-shaped.
func TestLoadMarketConfAdaptor(t *testing.T) {
	const validAdaptor = `{
		"markets": [{
			"base": "btc", "quote": "xmr",
			"lotSize": 100000, "rateStep": 100,
			"epochDuration": 20000, "parcelSize": 1,
			"swapType": "adaptor",
			"scriptableAsset": "btc",
			"lockBlocks": 144
		}],
		"assets": {
			"btc": {"bip44symbol": "btc", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1},
			"xmr": {"bip44symbol": "xmr", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1}
		}
	}`

	t.Run("valid-adaptor", func(t *testing.T) {
		mkts, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(validAdaptor))
		if err != nil {
			t.Fatalf("loadMarketConf: %v", err)
		}
		if len(mkts) != 1 {
			t.Fatalf("got %d markets, want 1", len(mkts))
		}
		mkt := mkts[0]
		if mkt.SwapType != dex.SwapTypeAdaptor {
			t.Errorf("SwapType = %d, want SwapTypeAdaptor", mkt.SwapType)
		}
		if mkt.ScriptableAsset != mkt.Base { // BTC bip-44 ID is 0
			t.Errorf("ScriptableAsset = %d, want %d (base)", mkt.ScriptableAsset, mkt.Base)
		}
		if mkt.LockBlocks != 144 {
			t.Errorf("LockBlocks = %d, want 144", mkt.LockBlocks)
		}
	})

	t.Run("htlc-default", func(t *testing.T) {
		const htlc = `{
			"markets": [{
				"base": "btc", "quote": "xmr",
				"lotSize": 100000, "rateStep": 100,
				"epochDuration": 20000, "parcelSize": 1
			}],
			"assets": {
				"btc": {"bip44symbol": "btc", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1},
				"xmr": {"bip44symbol": "xmr", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1}
			}
		}`
		mkts, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(htlc))
		if err != nil {
			t.Fatalf("htlc default: %v", err)
		}
		if mkts[0].SwapType != dex.SwapTypeHTLC {
			t.Errorf("SwapType default = %d, want SwapTypeHTLC", mkts[0].SwapType)
		}
	})

	t.Run("missing-scriptable", func(t *testing.T) {
		bad := strings.Replace(validAdaptor, `"scriptableAsset": "btc",`, "", 1)
		_, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(bad))
		if err == nil {
			t.Fatal("expected error for missing scriptableAsset")
		}
	})

	t.Run("missing-lockblocks", func(t *testing.T) {
		bad := strings.Replace(validAdaptor, `"lockBlocks": 144`, `"lockBlocks": 0`, 1)
		_, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(bad))
		if err == nil {
			t.Fatal("expected error for lockBlocks=0")
		}
	})

	t.Run("scriptable-not-in-pair", func(t *testing.T) {
		bad := strings.Replace(validAdaptor, `"scriptableAsset": "btc"`, `"scriptableAsset": "dcr"`, 1)
		bad = strings.Replace(bad,
			`"xmr": {"symbol": "xmr", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1}`,
			`"xmr": {"symbol": "xmr", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1},
			"dcr": {"symbol": "dcr", "network": "mainnet", "maxFeeRate": 100, "swapConf": 1}`, 1)
		_, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(bad))
		if err == nil {
			t.Fatal("expected error for scriptableAsset not in pair")
		}
	})

	t.Run("unknown-swaptype", func(t *testing.T) {
		bad := strings.Replace(validAdaptor, `"swapType": "adaptor"`, `"swapType": "musig"`, 1)
		_, _, err := loadMarketConf(dex.Mainnet, strings.NewReader(bad))
		if err == nil {
			t.Fatal("expected error for unknown swapType")
		}
	})
}
