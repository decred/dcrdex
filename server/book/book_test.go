// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"os"
	"testing"
	"time"

	"github.com/decred/dcrdex/dex/order"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/slog"
)

// An arbitrary account ID for test orders.
var acct0 = account.AccountID{
	0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
	0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
}

const (
	AssetDCR uint32 = iota
	AssetBTC

	LotSize = uint64(10 * 1e8)
)

func startLogger() {
	logger := slog.NewBackend(os.Stdout).Logger("BOOKTEST")
	logger.SetLevel(slog.LevelDebug)
	UseLogger(logger)
}

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	return &order.LimitOrder{
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
			},
			UTXOs:    []order.Outpoint{},
			Sell:     sell,
			Quantity: quantityLots * LotSize,
			Address:  addr,
		},
		Rate:  rate,
		Force: force,
	}
}

var (
	// Create a coherent order book of standing orders and sorted rates.
	bookBuyOrders = []*order.LimitOrder{
		newLimitOrder(false, 2500000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 2700000, 2, order.StandingTiF, 0),
		//newLimitOrder(false, 3200000, 2, order.StandingTiF, 0), // Commented in these tests so buy and sell books are different lengths.
		newLimitOrder(false, 3300000, 1, order.StandingTiF, 2), // newer
		newLimitOrder(false, 3300000, 2, order.StandingTiF, 0), // older
		newLimitOrder(false, 3600000, 4, order.StandingTiF, 0),
		newLimitOrder(false, 3900000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 4000000, 10, order.StandingTiF, 0),
		newLimitOrder(false, 4300000, 4, order.StandingTiF, 1), // newer
		newLimitOrder(false, 4300000, 2, order.StandingTiF, 0), // older
		newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
	}
	bestBuyOrder   = bookBuyOrders[len(bookBuyOrders)-1]
	bookSellOrders = []*order.LimitOrder{
		newLimitOrder(true, 6200000, 2, order.StandingTiF, 1), // newer
		newLimitOrder(true, 6200000, 2, order.StandingTiF, 0), // older
		newLimitOrder(true, 6100000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 6000000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 5500000, 1, order.StandingTiF, 0),
		newLimitOrder(true, 5400000, 4, order.StandingTiF, 0),
		newLimitOrder(true, 5000000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 4700000, 4, order.StandingTiF, 1),  // newer
		newLimitOrder(true, 4700000, 10, order.StandingTiF, 0), //older
		newLimitOrder(true, 4600000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 4550000, 1, order.StandingTiF, 0),
	}
	bestSellOrder = bookSellOrders[len(bookSellOrders)-1]
)

func newBook(t *testing.T) *Book {
	resetMakers()

	b := New(LotSize, DefaultBookHalfCapacity)

	for _, o := range bookBuyOrders {
		if ok := b.Insert(o); !ok {
			t.Fatalf("Failed to insert buy order %v", o)
		}
	}
	for _, o := range bookSellOrders {
		if ok := b.Insert(o); !ok {
			t.Fatalf("Failed to insert sell order %v", o)
		}
	}
	return b
}

func resetMakers() {
	for _, o := range bookBuyOrders {
		o.Filled = 0
	}
	for _, o := range bookSellOrders {
		o.Filled = 0
	}
}

func TestBook(t *testing.T) {
	startLogger()

	b := newBook(t)
	if b.BuyCount() != len(bookBuyOrders) {
		t.Errorf("Incorrect number of buy orders. Got %d, expected %d",
			b.BuyCount(), len(bookBuyOrders))
	}
	if b.SellCount() != len(bookSellOrders) {
		t.Errorf("Incorrect number of sell orders. Got %d, expected %d",
			b.SellCount(), len(bookSellOrders))
	}

	if b.BestBuy().Sell {
		t.Error("Best buy order was a sell order.")
	}
	if !b.BestSell().Sell {
		t.Error("Best sell order was a buy order.")
	}

	if b.BestBuy().ID() != bestBuyOrder.ID() {
		t.Errorf("The book returned the wrong best buy order. Got %v, expected %v",
			b.BestBuy().ID(), bestBuyOrder.ID())
	}
	if b.BestSell().ID() != bestSellOrder.ID() {
		t.Errorf("The book returned the wrong best sell order. Got %v, expected %v",
			b.BestSell().ID(), bestSellOrder.ID())
	}

	sells := b.SellOrders()
	if len(sells) != b.SellCount() {
		t.Errorf("Incorrect number of sell orders. Got %d, expected %d",
			len(sells), b.SellCount())
	}

	buys := b.BuyOrders()
	if len(buys) != b.BuyCount() {
		t.Errorf("Incorrect number of buy orders. Got %d, expected %d",
			len(buys), b.BuyCount())
	}

	b.Realloc(DefaultBookHalfCapacity * 2)

	buys2 := b.BuyOrders()
	if len(buys) != len(buys2) {
		t.Errorf("Incorrect number of buy orders after realloc. Got %d, expected %d",
			len(buys), len(buys2))
	}
	for i := range buys2 {
		if buys2[i] != buys[i] {
			t.Errorf("Buy order %d mismatch after realloc. Got %s, expected %s",
				i, buys2[i].UID(), buys[i].UID())
		}
	}

	sells2 := b.SellOrders()
	if len(sells) != len(sells2) {
		t.Errorf("Incorrect number of sell orders after realloc. Got %d, expected %d",
			len(sells), len(sells2))
	}
	for i := range sells2 {
		if sells2[i] != sells[i] {
			t.Errorf("Sell order %d mismatch after realloc. Got %s, expected %s",
				i, sells2[i].UID(), sells[i].UID())
		}
	}

	badOrder := newLimitOrder(false, 2500000, 1, order.StandingTiF, 0)
	badOrder.Quantity /= 3
	if b.Insert(badOrder) {
		t.Errorf("Inserted order with non-integer multiple of lot size!")
	}

	removed, ok := b.Remove(order.OrderID{})
	if ok {
		t.Fatalf("Somehow removed order for fake ID. Removed %v", removed.ID())
	}

	// Remove not the best buy order.
	removed, ok = b.Remove(bookBuyOrders[3].ID())
	if !ok {
		t.Fatalf("Failed to remove existing buy order %v", bestBuyOrder.ID())
	}
	if removed.ID() != bookBuyOrders[3].ID() {
		t.Errorf("Failed to remove existing buy order. Got %v, wanted %v",
			removed.ID(), bookBuyOrders[3].ID())
	}

	if b.BuyCount() != len(bookBuyOrders)-1 {
		t.Errorf("Expected %d book orders, got %d", len(bookBuyOrders)-1, b.BuyCount())
	}

	if b.SellCount() != len(bookSellOrders) {
		t.Errorf("Expected %d book orders, got %d", len(bookSellOrders), b.BuyCount())
	}

	// Remove not the best sell order.
	removed, ok = b.Remove(bookSellOrders[2].ID())
	if !ok {
		t.Fatalf("Failed to remove existing buy order %v", bestBuyOrder.ID())
	}
	if removed.ID() != bookSellOrders[2].ID() {
		t.Errorf("Failed to remove existing buy order. Got %v, wanted %v",
			removed.ID(), bookSellOrders[2].ID())
	}

	if b.BuyCount() != len(bookBuyOrders)-1 {
		t.Errorf("Expected %d book orders, got %d", len(bookBuyOrders)-1, b.BuyCount())
	}

	if b.SellCount() != len(bookSellOrders)-1 {
		t.Errorf("Expected %d book orders, got %d", len(bookSellOrders)-1, b.BuyCount())
	}

	removed, ok = b.Remove(bestBuyOrder.ID())
	if !ok {
		t.Fatalf("Failed to remove best buy order %v", bestBuyOrder.ID())
	}
	if removed.ID() != bestBuyOrder.ID() {
		t.Errorf("Failed to remove best buy order. Got %v, wanted %v",
			removed.ID(), bestBuyOrder.ID())
	}

	removed, ok = b.Remove(bestSellOrder.ID())
	if !ok {
		t.Fatalf("Failed to remove best sell order %v", bestSellOrder.ID())
	}
	if removed.ID() != bestSellOrder.ID() {
		t.Errorf("Failed to remove best sell order. Got %v, wanted %v",
			removed.ID(), bestSellOrder.ID())
	}
}
