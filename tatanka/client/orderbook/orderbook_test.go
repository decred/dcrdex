package orderbook

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/tatanka/tanka"
)

func testOrders() []*tanka.OrderUpdate {
	mustParse := func(s string) time.Time {
		t, err := time.Parse(time.RFC1123, s)
		if err != nil {
			panic(err)
		}
		return t
	}

	var peer tanka.PeerID
	copy(peer[:], encode.RandomBytes(32))
	baseID, quoteID := uint32(0), uint32(42)
	lowbuy := &tanka.OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:    peer,
			BaseID:  baseID,
			QuoteID: quoteID,
			Sell:    false,
			Qty:     1000,
			Rate:    123,
			LotSize: 3,
			Nonce:   0,
			Stamp:   mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
		},
		Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		Settled:    500,
	}
	highbuy := &tanka.OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:    peer,
			BaseID:  baseID,
			QuoteID: quoteID,
			Sell:    false,
			Qty:     1000,
			Rate:    1234,
			LotSize: 2,
			Nonce:   1,
			Stamp:   mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
		},
		Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		Settled:    500,
	}
	lowsell := &tanka.OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:    peer,
			BaseID:  baseID,
			QuoteID: quoteID,
			Sell:    true,
			Qty:     1000,
			Rate:    12345,
			LotSize: 2,
			Nonce:   2,
			Stamp:   mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
		},
		Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		Settled:    500,
	}
	highsell := &tanka.OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:    peer,
			BaseID:  baseID,
			QuoteID: quoteID,
			Sell:    true,
			Qty:     1000,
			Rate:    123456,
			LotSize: 4,
			Nonce:   3,
			Stamp:   mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
		},
		Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		Settled:    500,
	}
	return []*tanka.OrderUpdate{lowbuy, highbuy, lowsell, highsell}
}

func TestOrderIDs(t *testing.T) {
	ob := NewOrderBook()
	for _, o := range testOrders() {
		if err := ob.AddUpdate(o); err != nil {
			t.Fatal(err)
		}
	}

	ous := ob.OrderIDs()
	if len(ous) != 4 {
		t.Fatalf("wanted 4 but got %d orders", len(ous))
	}
}

func TestOrders(t *testing.T) {
	ob := NewOrderBook()
	var oids [][32]byte
	for _, o := range testOrders() {
		if err := ob.AddUpdate(o); err != nil {
			t.Fatal(err)
		}
		oids = append(oids, o.ID())
	}
	ous := ob.Orders(oids)
	if len(ous) != 4 {
		t.Fatalf("wanted 4 but got %d orders", len(oids))
	}
}

func TestFindOrders(t *testing.T) {
	ob := NewOrderBook()
	tOrds := testOrders()
	for _, o := range tOrds {
		if err := ob.AddUpdate(o); err != nil {
			t.Fatal(err)
		}
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
			Check: func(ou *tanka.OrderUpdate) (ok, done bool) {
				return ou.LotSize > 2, false
			},
		},
		wantOrderLen: 2,
		wantOrderIdx: []int{0, 3},
	}, {
		name: "lot size over 2 and sell",
		filter: &OrderFilter{
			IsSell: &yes,
			Check: func(ou *tanka.OrderUpdate) (ok, done bool) {
				return ou.LotSize > 2, false
			},
		},
		wantOrderLen: 1,
		wantOrderIdx: []int{3},
	}, {
		name: "buy done after one",
		filter: &OrderFilter{
			IsSell: &no,
			Check: func() func(*tanka.OrderUpdate) (ok, done bool) {
				var i int
				return func(ou *tanka.OrderUpdate) (ok, done bool) {
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
			ous := ob.FindOrders(test.filter)
			if len(ous) != test.wantOrderLen {
				t.Fatalf("wanted %d but got %d orders", test.wantOrderLen, len(ous))
			}
			for i, idx := range test.wantOrderIdx {
				if tOrds[idx].ID() != ous[i].ID() {
					t.Fatalf("returned order at idx %d not equal to test order at %d", idx, i)
				}
			}
		})
	}
}

func TestDeleteOrder(t *testing.T) {
	ob := NewOrderBook()
	var oids [][32]byte
	for _, o := range testOrders() {
		if err := ob.AddUpdate(o); err != nil {
			t.Fatal(err)
		}
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
