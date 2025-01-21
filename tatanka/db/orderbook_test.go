package db

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/tatanka/tanka"
)

func testOrders() []*OrderUpdate {
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
	lowbuy := &OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:       peer,
			BaseID:     baseID,
			QuoteID:    quoteID,
			Sell:       false,
			Qty:        1000,
			Settled:    500,
			Rate:       123,
			LotSize:    3,
			MinFeeRate: 10000,
			Nonce:      0,
			Stamp:      mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
			Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		},
	}
	highbuy := &OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:       peer,
			BaseID:     baseID,
			QuoteID:    quoteID,
			Sell:       false,
			Qty:        1000,
			Settled:    500,
			Rate:       1234,
			LotSize:    2,
			MinFeeRate: 10000,
			Nonce:      1,
			Stamp:      mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
			Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		},
	}
	lowsell := &OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:       peer,
			BaseID:     baseID,
			QuoteID:    quoteID,
			Sell:       true,
			Qty:        1000,
			Settled:    500,
			Rate:       12345,
			LotSize:    2,
			MinFeeRate: 10000,
			Nonce:      2,
			Stamp:      mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
			Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		},
	}
	highsell := &OrderUpdate{
		Sig: encode.RandomBytes(32),
		Order: &tanka.Order{
			From:       peer,
			BaseID:     baseID,
			QuoteID:    quoteID,
			Sell:       true,
			Qty:        1000,
			Settled:    500,
			Rate:       123456,
			LotSize:    4,
			MinFeeRate: 10000,
			Nonce:      3,
			Stamp:      mustParse("Sun, 12 Dec 2024 12:23:00 UTC"),
			Expiration: mustParse("Sun, 12 Jan 2025 12:23:00 UTC"),
		},
	}
	return []*OrderUpdate{lowbuy, highbuy, lowsell, highsell}
}

func TestOrderIDs(t *testing.T) {
	db, shutdown := tNewDB()
	defer shutdown()
	ob, err := db.NewOrderBook(0, 42)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range testOrders() {
		if err := ob.Add(o); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name             string
		fromIdx, nOrders uint64
		wantOrderLen     int
		wantAll          bool
	}{{
		name:         "all orders",
		nOrders:      100,
		wantOrderLen: 4,
		wantAll:      true,
	}, {
		name:         "from 2",
		fromIdx:      2,
		nOrders:      100,
		wantOrderLen: 2,
		wantAll:      true,
	}, {
		name:         "3 orders",
		nOrders:      3,
		wantOrderLen: 3,
	}, {
		name:         "from 1 until 2 orders",
		fromIdx:      1,
		nOrders:      2,
		wantOrderLen: 2,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			oids, all, err := ob.OrderIDs(test.fromIdx, test.nOrders)
			if err != nil {
				t.Fatal(err)
			}
			if len(oids) != test.wantOrderLen {
				t.Fatalf("wanted %d but got %d orders", test.wantOrderLen, len(oids))
			}
			if all != test.wantAll {
				t.Fatalf("wanted all to be %v but got %v", test.wantAll, all)
			}
		})
	}
}

func TestOrders(t *testing.T) {
	db, shutdown := tNewDB()
	defer shutdown()
	ob, err := db.NewOrderBook(0, 42)
	if err != nil {
		t.Fatal(err)
	}
	var oids []tanka.ID32
	for _, o := range testOrders() {
		if err := ob.Add(o); err != nil {
			t.Fatal(err)
		}
		oids = append(oids, o.ID())
	}
	ous, err := ob.Orders(oids)
	if err != nil {
		t.Fatal(err)
	}
	if len(ous) != 4 {
		t.Fatalf("wanted 4 but got %d orders", len(oids))
	}
}

func TestFindOrders(t *testing.T) {
	db, shutdown := tNewDB()
	defer shutdown()
	ob, err := db.NewOrderBook(0, 42)
	if err != nil {
		t.Fatal(err)
	}
	tOrds := testOrders()
	for _, o := range tOrds {
		if err := ob.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false

	tests := []struct {
		name         string
		filter       *OrderFilter
		wantOrderLen int
		wantErr      bool
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
			Check: func(ou *OrderUpdate) (ok, done bool) {
				return ou.LotSize > 2, false
			},
		},
		wantOrderLen: 2,
		wantOrderIdx: []int{0, 3},
	}, {
		name: "lot size over 2 and sell",
		filter: &OrderFilter{
			IsSell: &yes,
			Check: func(ou *OrderUpdate) (ok, done bool) {
				return ou.LotSize > 2, false
			},
		},
		wantOrderLen: 1,
		wantOrderIdx: []int{3},
	}, {
		name: "buy done after one",
		filter: &OrderFilter{
			IsSell: &no,
			Check: func() func(*OrderUpdate) (ok, done bool) {
				var i int
				return func(ou *OrderUpdate) (ok, done bool) {
					defer func() { i++ }()
					return true, i == 1
				}
			}(),
		},
		wantOrderLen: 1,
		wantOrderIdx: []int{1},
	}, {
		name:    "nil filter",
		wantErr: true,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ous, err := ob.FindOrders(test.filter)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			} else if err != nil {
				t.Fatal(err)
			}
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
