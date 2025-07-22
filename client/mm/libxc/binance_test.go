// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"testing"

	_ "decred.org/dcrdex/client/asset/importall"
)

func TestSubscribeTradeUpdates(t *testing.T) {
	bn := &binance{
		tradeUpdaters: make(map[int]chan *Trade),
	}
	_, unsub0, _ := bn.SubscribeTradeUpdates()
	_, _, id1 := bn.SubscribeTradeUpdates()
	unsub0()
	_, _, id2 := bn.SubscribeTradeUpdates()
	if len(bn.tradeUpdaters) != 2 {
		t.Fatalf("wrong number of updaters. wanted 2, got %d", len(bn.tradeUpdaters))
	}
	if id1 == id2 {
		t.Fatalf("ids should be unique. got %d twice", id1)
	}
	if _, found := bn.tradeUpdaters[id1]; !found {
		t.Fatalf("id1 not found")
	}
	if _, found := bn.tradeUpdaters[id2]; !found {
		t.Fatalf("id2 not found")
	}
}

func TestBinanceToDexSymbol(t *testing.T) {
	tests := map[[2]string]string{
		{"ETH", "ETH"}:    "eth",
		{"ETH", "MATIC"}:  "weth.polygon",
		{"USDC", "ETH"}:   "usdc.eth",
		{"USDC", "MATIC"}: "usdc.polygon",
		{"BTC", "BTC"}:    "btc",
		{"WBTC", "ETH"}:   "wbtc.eth",
		{"POL", "MATIC"}:  "polygon",
	}

	for test, expected := range tests {
		dexSymbol := binanceCoinNetworkToDexSymbol(test[0], test[1])
		if expected != dexSymbol {
			t.Fatalf("expected %s but got %v", expected, dexSymbol)
		}
	}
}

func TestBncAssetCfg(t *testing.T) {
	tests := map[uint32]*bncAssetConfig{
		0: {
			assetID:          0,
			symbol:           "btc",
			coin:             "BTC",
			chain:            "BTC",
			conversionFactor: 1e8,
		},
		2: {
			assetID:          2,
			symbol:           "ltc",
			coin:             "LTC",
			chain:            "LTC",
			conversionFactor: 1e8,
		},
		3: {
			assetID:          3,
			symbol:           "doge",
			coin:             "DOGE",
			chain:            "DOGE",
			conversionFactor: 1e8,
		},
		5: {
			assetID:          5,
			symbol:           "dash",
			coin:             "DASH",
			chain:            "DASH",
			conversionFactor: 1e8,
		},
		60: {
			assetID:          60,
			symbol:           "eth",
			coin:             "ETH",
			chain:            "ETH",
			conversionFactor: 1e9,
		},
		42: {
			assetID:          42,
			symbol:           "dcr",
			coin:             "DCR",
			chain:            "DCR",
			conversionFactor: 1e8,
		},
		966001: {
			assetID:          966001,
			symbol:           "usdc.polygon",
			coin:             "USDC",
			chain:            "MATIC",
			conversionFactor: 1e6,
		},
		966002: {
			assetID:          966002,
			symbol:           "weth.polygon",
			coin:             "ETH",
			chain:            "MATIC",
			conversionFactor: 1e9,
		},
		966: {
			assetID:          966,
			symbol:           "polygon",
			coin:             "POL",
			chain:            "MATIC",
			conversionFactor: 1e9,
		},
	}

	for test, expected := range tests {
		cfg, err := bncAssetCfg(test)
		if err != nil {
			t.Fatalf("error getting asset config: %v", err)
		}
		if *expected != *cfg {
			t.Fatalf("expected %v but got %v", expected, cfg)
		}
	}
}
