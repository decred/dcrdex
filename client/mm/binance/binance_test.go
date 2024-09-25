// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package binance

import (
	"testing"

	"decred.org/dcrdex/client/mm/libxc"
)

func TestSubscribeTradeUpdates(t *testing.T) {
	bn := &binance{
		tradeUpdaters: make(map[int]chan *libxc.Trade),
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
		{"ETH", "ETH"}:     "eth",
		{"ETH", "MATIC"}:   "weth.polygon",
		{"MATIC", "MATIC"}: "polygon",
		{"USDC", "ETH"}:    "usdc.eth",
		{"USDC", "MATIC"}:  "usdc.polygon",
		{"BTC", "BTC"}:     "btc",
		{"WBTC", "ETH"}:    "wbtc.eth",
	}

	for test, expected := range tests {
		dexSymbol := binanceCoinNetworkToDexSymbol(test[0], test[1])
		if expected != dexSymbol {
			t.Fatalf("expected %s but got %v", expected, dexSymbol)
		}
	}
}
