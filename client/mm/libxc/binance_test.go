package libxc

import (
	"testing"
)

func TestSubscribeCEXUpdates(t *testing.T) {
	bn := &binance{
		cexUpdaters: make(map[chan interface{}]struct{}),
	}
	_, unsub0 := bn.SubscribeCEXUpdates()
	bn.SubscribeCEXUpdates()
	unsub0()
	bn.SubscribeCEXUpdates()
	if len(bn.cexUpdaters) != 2 {
		t.Fatalf("wrong number of updaters. wanted 2, got %d", len(bn.cexUpdaters))
	}
}

func TestSubscribeTradeUpdates(t *testing.T) {
	bn := &binance{
		tradeUpdaters: make(map[int]chan *TradeUpdate),
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
