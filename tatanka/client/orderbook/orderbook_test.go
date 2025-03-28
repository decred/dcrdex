package orderbook

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/tatanka/tanka"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC1123, s)
	if err != nil {
		panic(err)
	}
	return t
}

func testOrders() []*tanka.Order {
	var peer tanka.PeerID
	copy(peer[:], encode.RandomBytes(32))
	baseID, quoteID := uint32(0), uint32(42)
	lowbuy := &tanka.Order{
		From:    peer,
		BaseID:  baseID,
		QuoteID: quoteID,
		Sell:    false,
		Qty:     1000,
		Rate:    123,
		LotSize: 3,
		Nonce:   0,
		Stamp:   mustParseTime("Sun, 12 Dec 2024 12:23:00 UTC"),
	}
	highbuy := &tanka.Order{
		From:    peer,
		BaseID:  baseID,
		QuoteID: quoteID,
		Sell:    false,
		Qty:     1000,
		Rate:    1234,
		LotSize: 2,
		Nonce:   1,
		Stamp:   mustParseTime("Sun, 12 Dec 2024 12:23:00 UTC"),
	}
	lowsell := &tanka.Order{
		From:    peer,
		BaseID:  baseID,
		QuoteID: quoteID,
		Sell:    true,
		Qty:     1000,
		Rate:    12345,
		LotSize: 2,
		Nonce:   2,
		Stamp:   mustParseTime("Sun, 12 Dec 2024 12:23:00 UTC"),
	}
	highsell := &tanka.Order{
		From:    peer,
		BaseID:  baseID,
		QuoteID: quoteID,
		Sell:    true,
		Qty:     1000,
		Rate:    123456,
		LotSize: 4,
		Nonce:   3,
		Stamp:   mustParseTime("Sun, 12 Dec 2024 12:23:00 UTC"),
	}
	return []*tanka.Order{lowbuy, highbuy, lowsell, highsell}
}

func TestOrders(t *testing.T) {
	ob := NewOrderBook()
	var oids []tanka.ID40
	for _, o := range testOrders() {
		ob.Add(o)
		oids = append(oids, o.ID())
	}
	ords := ob.Orders(oids)
	if len(ords) != 4 {
		t.Fatalf("wanted 4 but got %d orders", len(ords))
	}
}

func TestFindOrders(t *testing.T) {
	ob := NewOrderBook()
	tOrds := testOrders()
	for _, o := range tOrds {
		ob.Add(o)
	}
	yes, no := true, false

	tests := []struct {
		name         string
		filter       *OrderFilter
		wantOrderLen int
		wantOrderIdx []int
	}{{
		name:         "all orders",
		filter:       new(OrderFilter),
		wantOrderLen: 4,
		wantOrderIdx: []int{1, 0, 2, 3},
	}, {
		name: "sells",
		filter: &OrderFilter{
			IsSell: &yes,
		},
		wantOrderLen: 2,
		wantOrderIdx: []int{2, 3},
	}, {
		name: "buys",
		filter: &OrderFilter{
			IsSell: &no,
		},
		wantOrderLen: 2,
		wantOrderIdx: []int{1, 0},
	}, {
		name: "lot size over 2",
		filter: &OrderFilter{
			Check: func(o *tanka.Order) (ok, done bool) {
				return o.LotSize > 2, false
			},
		},
		wantOrderLen: 2,
		wantOrderIdx: []int{0, 3},
	}, {
		name: "lot size over 2 and sell",
		filter: &OrderFilter{
			IsSell: &yes,
			Check: func(o *tanka.Order) (ok, done bool) {
				return o.LotSize > 2, false
			},
		},
		wantOrderLen: 1,
		wantOrderIdx: []int{3},
	}, {
		name: "buy done after one",
		filter: &OrderFilter{
			IsSell: &no,
			Check: func() func(*tanka.Order) (ok, done bool) {
				var i int
				return func(*tanka.Order) (ok, done bool) {
					defer func() { i++ }()
					return true, i == 1
				}
			}(),
		},
		wantOrderLen: 1,
		wantOrderIdx: []int{1},
	}, {
		name: "nil filter",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ords := ob.FindOrders(test.filter)
			if len(ords) != test.wantOrderLen {
				t.Fatalf("wanted %d but got %d orders", test.wantOrderLen, len(ords))
			}
			for i, idx := range test.wantOrderIdx {
				if tOrds[idx].ID() != ords[i].ID() {
					t.Fatalf("returned order at idx %d not equal to test order at %d", idx, i)
				}
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	ob := NewOrderBook()
	ords := testOrders()
	for _, o := range ords {
		ob.Add(o)
	}
	o := ords[0]
	updateTime := mustParseTime("Sun, 28 Dec 2025 12:00:00 UTC")
	tests := []struct {
		name    string
		update  *tanka.OrderUpdate
		wantErr bool
	}{{
		name: "ok",
		update: &tanka.OrderUpdate{
			From:  o.From,
			Nonce: o.Nonce,
			Qty:   7,
			Stamp: updateTime,
		},
	}, {
		name: "order does not exist",
		update: &tanka.OrderUpdate{
			From:  o.From,
			Nonce: 100,
			Qty:   7,
			Stamp: updateTime,
		},
		wantErr: true,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ob.Update(test.update)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			ord := ob.Order(test.update.ID())
			if ord.Qty != test.update.Qty {
				t.Fatalf("expected qty %d but got %d", test.update.Qty, ord.Qty)
			}
			if ord.Stamp != test.update.Stamp {
				t.Fatalf("expected stamp %s but got %s", test.update.Stamp, ord.Stamp)
			}
		})
	}
}

func TestDeleteOrder(t *testing.T) {
	ob := NewOrderBook()
	var oids []tanka.ID40
	for _, o := range testOrders() {
		ob.Add(o)
		oids = append(oids, o.ID())
	}
	ob.Delete(oids[0])
	ob.Delete(oids[3])
	ous := ob.Orders(oids)
	if len(ous) != 2 {
		t.Fatalf("wanted 2 but got %d orders", len(oids))
	}
	if len(ob.sells) != 1 {
		t.Fatalf("wanted 1 but got %d orders", len(oids))
	}
	if len(ob.buys) != 1 {
		t.Fatalf("wanted 1 but got %d orders", len(oids))
	}
}
