package core

import (
	"context"
	"os"
	"strconv"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
)

var tCtx context.Context

// type TWebsocket struct {
// 	id      uint64
// 	sendErr error
// 	reqErr  error
// 	msgs    <-chan *msgjson.Message
// }
//
// func newTWebsocket() *TWebsocket {
// 	return &TWebsocket{msgs: make(<-chan *msgjson.Message)}
// }
//
// func (conn *TWebsocket) NextID() uint64 {
// 	conn.id++
// 	return conn.id
// }
//
// func (conn *TWebsocket) WaitForShutdown()                {}
// func (conn *TWebsocket) Send(msg *msgjson.Message) error { return conn.sendErr }
// func (conn *TWebsocket) Request(msg *msgjson.Message, f func(*msgjson.Message)) error {
// 	return conn.reqErr
// }
// func (conn *TWebsocket) MessageSource() <-chan *msgjson.Message { return conn.msgs }

var tAssetID uint32

func randomAsset() *msgjson.Asset {
	tAssetID++
	return &msgjson.Asset{
		Symbol: "BT" + strconv.Itoa(int(tAssetID)),
		ID:     tAssetID,
	}
}

func randomMsgMarket() (baseAsset, quoteAsset *msgjson.Asset) {
	return randomAsset(), randomAsset()
}

func testCore() *Core {
	return &Core{
		ctx:   tCtx,
		conns: make(map[string]*dexConnection),
	}
}

func tMarketID(bq [2]uint32) string {
	return strconv.Itoa(int(bq[0])) + "-" + strconv.Itoa(int(bq[1]))
}

func TestMain(m *testing.M) {
	var shutdown context.CancelFunc
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestListMarkets(t *testing.T) {
	tCore := testCore()
	// Simulate 10 markets.
	marketIDs := make(map[string]struct{})
	var markets [][2]uint32
	assets := make(map[uint32]*msgjson.Asset)
	for i := 0; i < 10; i++ {
		base, quote := randomMsgMarket()
		mkt := [2]uint32{base.ID, quote.ID}
		marketIDs[tMarketID(mkt)] = struct{}{}
		markets = append(markets, mkt)
		assets[base.ID] = base
		assets[quote.ID] = quote
	}
	tCore.conns["testdex"] = &dexConnection{
		assets: assets,
		cfg: &msgjson.ConfigResult{
			Markets: markets,
		},
	}

	// Just check that the information is coming through correctly.
	dexes := tCore.ListMarkets()
	if len(dexes) != 1 {
		t.Fatalf("expected 1 MarketConfig, got %d", len(dexes))
	}
	mktCfg := dexes[0]
	if len(mktCfg.Markets) != 10 {
		t.Fatalf("expected 10 Markets, got %d", len(mktCfg.Markets))
	}
	for _, market := range mktCfg.Markets {
		mkt := tMarketID([2]uint32{market.BaseID, market.QuoteID})
		_, found := marketIDs[mkt]
		if !found {
			t.Fatalf("market %s not found", mkt)
		}
		if assets[market.BaseID].Symbol != market.BaseSymbol {
			t.Fatalf("base symbol mismatch. %s != %s", assets[market.BaseID].Symbol, market.BaseSymbol)
		}
		if assets[market.QuoteID].Symbol != market.QuoteSymbol {
			t.Fatalf("quote symbol mismatch. %s != %s", assets[market.QuoteID].Symbol, market.QuoteSymbol)
		}
	}
}
