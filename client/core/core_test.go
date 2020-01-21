package core

import (
	"context"
	"encoding/hex"
	"os"
	"strconv"
	"testing"

	"decred.org/dcrdex/client/order"
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
	var markets []msgjson.Market
	assets := make(map[uint32]*msgjson.Asset)
	for i := 0; i < 10; i++ {
		base, quote := randomMsgMarket()
		mkt := [2]uint32{base.ID, quote.ID}
		marketIDs[tMarketID(mkt)] = struct{}{}
		markets = append(markets, msgjson.Market{
			Name:            base.Symbol + "_" + quote.Symbol,
			Base:            base.ID,
			Quote:           quote.ID,
			EpochLen:        5000,
			StartEpoch:      1234,
			MarketBuyBuffer: 1.4,
		})
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
		t.Fatalf("expected 1 MarketInfo, got %d", len(dexes))
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

func TestDexConnectionOrderBook(t *testing.T) {
	tCore := testCore()
	mid := "ob"
	dc := &dexConnection{
		books: make(map[string]*order.OrderBook),
	}

	// Ensure handleOrderBookMsg creates an order book as expected.
	oid, err := hex.DecodeString("1d6c8998e93898f872fa43f35ede17c3196c6a1a2054cb8d91f2e184e8ca0316")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	msg, err := msgjson.NewResponse(1, &msgjson.OrderBook{
		Seq:      1,
		MarketID: "ob",
		Orders: []*msgjson.BookOrderNote{
			{
				TradeNote: msgjson.TradeNote{
					Side:     msgjson.BuyOrderNum,
					Quantity: 10,
					Rate:     2,
				},
				OrderNote: msgjson.OrderNote{
					Seq:      1,
					MarketID: mid,
					OrderID:  oid,
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("[NewResponse]: unexpected err: %v", err)
	}

	err = tCore.handleOrderBookMsg(dc, msg)
	if err != nil {
		t.Fatalf("[handleOrderBookMsg]: unexpected err: %v", err)
	}
	if len(dc.books) != 1 {
		t.Fatalf("expected %v order book created, got %v", 1, len(dc.books))
	}
	_, ok := dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}

	// Ensure handleBookOrderMsg creates a book order for the associated
	// order book as expected.
	oid, err = hex.DecodeString("d445ab685f5cf54dfebdaa05232892b4cfa453a566b5e85f62627dd6834c5c02")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	bookSeq := uint64(2)
	msg, err = msgjson.NewNotification(msgjson.BookOrderRoute, &msgjson.BookOrderNote{
		TradeNote: msgjson.TradeNote{
			Side:     msgjson.BuyOrderNum,
			Quantity: 10,
			Rate:     2,
		},
		OrderNote: msgjson.OrderNote{
			Seq:      bookSeq,
			MarketID: mid,
			OrderID:  oid,
		},
	})
	if err != nil {
		t.Fatalf("[NewNotification]: unexpected err: %v", err)
	}

	err = tCore.handleBookOrderMsg(dc, msg)
	if err != nil {
		t.Fatalf("[handleBookOrderMsg]: unexpected err: %v", err)
	}
	_, ok = dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}

	// Ensure handleUnbookOrderMsg removes a book order from an associated
	// order book as expected.
	oid, err = hex.DecodeString("d445ab685f5cf54dfebdaa05232892b4cfa453a566b5e85f62627dd6834c5c02")
	if err != nil {
		t.Fatalf("[DecodeString]: unexpected err: %v", err)
	}
	unbookSeq := uint64(3)
	msg, err = msgjson.NewNotification(msgjson.UnbookOrderRoute, &msgjson.UnbookOrderNote{
		Seq:      unbookSeq,
		MarketID: mid,
		OrderID:  oid,
	})
	if err != nil {
		t.Fatalf("[NewNotification]: unexpected err: %v", err)
	}

	err = tCore.handleUnbookOrderMsg(dc, msg)
	if err != nil {
		t.Fatalf("[handleUnbookOrderMsg]: unexpected err: %v", err)
	}
	_, ok = dc.books[mid]
	if !ok {
		t.Fatalf("expected order book with market id %s", mid)
	}
}
