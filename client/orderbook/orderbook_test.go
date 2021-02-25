package orderbook

import (
	"bytes"
	"encoding/hex"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

func makeOrderBookMsg(seq uint64, mid string, orders []*msgjson.BookOrderNote) *msgjson.OrderBook {
	return &msgjson.OrderBook{
		Seq:      seq,
		MarketID: mid,
		Orders:   orders,
	}
}

func makeBookOrderNote(seq uint64, mid string, oid order.OrderID, side uint8,
	qty uint64, rate uint64, time uint64) *msgjson.BookOrderNote {
	return &msgjson.BookOrderNote{
		TradeNote: msgjson.TradeNote{
			Side:     side,
			Quantity: qty,
			Rate:     rate,
			TiF:      0,
			Time:     time,
		},
		OrderNote: msgjson.OrderNote{
			Seq:      seq,
			MarketID: mid,
			OrderID:  oid[:],
		},
	}
}

func makeUnbookOrderNote(seq uint64, mid string, oid order.OrderID) *msgjson.UnbookOrderNote {
	return &msgjson.UnbookOrderNote{
		Seq:      seq,
		MarketID: mid,
		OrderID:  oid[:],
	}
}

func makeCachedBookOrderNote(orderNote *msgjson.BookOrderNote) *cachedOrderNote {
	return &cachedOrderNote{
		Route:     msgjson.BookOrderRoute,
		OrderNote: orderNote,
	}
}

func makeCachedUnbookOrderNote(orderNote *msgjson.UnbookOrderNote) *cachedOrderNote {
	return &cachedOrderNote{
		Route:     msgjson.UnbookOrderRoute,
		OrderNote: orderNote,
	}
}

func makeOrderBook(seq uint64, marketID string, orders []*Order, cachedOrders []*cachedOrderNote, synced bool) *OrderBook {
	ob := NewOrderBook(tLogger)
	ob.noteQueue = cachedOrders
	ob.marketID = marketID
	ob.seq = seq

	for _, order := range orders {
		ob.orders[order.OrderID] = order

		switch order.Side {
		case msgjson.BuyOrderNum:
			ob.buys.Add(order)

		case msgjson.SellOrderNum:
			ob.sells.Add(order)
		}
	}

	ob.setSynced(synced)

	return ob
}

func TestOrderBookSync(t *testing.T) {
	tests := []struct {
		label             string
		snapshot          *msgjson.OrderBook
		orderBook         *OrderBook
		expected          *OrderBook
		initialQueueState []*cachedOrderNote
		initialSyncState  bool
		wantErr           bool
	}{
		{
			label: "Sync blank unsynced order book",
			snapshot: makeOrderBookMsg(
				2,
				"ob",
				[]*msgjson.BookOrderNote{
					makeBookOrderNote(1, "ob", [32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeBookOrderNote(2, "ob", [32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
			),
			orderBook: NewOrderBook(tLogger),
			expected: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			initialQueueState: make([]*cachedOrderNote, 0),
			initialSyncState:  false,
			wantErr:           false,
		},
		{
			label: "Sync blank order book with no order notes",
			snapshot: makeOrderBookMsg(
				2,
				"ob",
				[]*msgjson.BookOrderNote{},
			),
			orderBook: NewOrderBook(tLogger),
			expected: makeOrderBook(
				2,
				"ob",
				[]*Order{},
				make([]*cachedOrderNote, 0),
				true,
			),
			initialQueueState: make([]*cachedOrderNote, 0),
			initialSyncState:  false,
			wantErr:           false,
		},
		{
			label: "Sync already synced order book",
			snapshot: makeOrderBookMsg(
				2,
				"ob",
				[]*msgjson.BookOrderNote{
					makeBookOrderNote(1, "ob", [32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeBookOrderNote(2, "ob", [32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
			),
			orderBook:         NewOrderBook(tLogger),
			expected:          nil,
			initialQueueState: make([]*cachedOrderNote, 0),
			initialSyncState:  true,
			wantErr:           true,
		},
		{
			label: "Sync order book with cached unbook order note",
			snapshot: makeOrderBookMsg(
				2,
				"ob",
				[]*msgjson.BookOrderNote{
					makeBookOrderNote(1, "ob", [32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeBookOrderNote(2, "ob", [32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
			),
			orderBook: NewOrderBook(tLogger),
			expected: makeOrderBook(
				3,
				"ob",
				[]*Order{
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 1, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			initialQueueState: []*cachedOrderNote{
				makeCachedUnbookOrderNote(makeUnbookOrderNote(3, "ob", [32]byte{'b'})),
			},
			initialSyncState: false,
			wantErr:          false,
		},
		{
			label: "Sync order book with cached book order note",
			snapshot: makeOrderBookMsg(
				3,
				"ob",
				[]*msgjson.BookOrderNote{
					makeBookOrderNote(1, "ob", [32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeBookOrderNote(2, "ob", [32]byte{'c'}, msgjson.BuyOrderNum, 5, 2, 5),
					makeBookOrderNote(3, "ob", [32]byte{'d'}, msgjson.SellOrderNum, 6, 3, 10),
				},
			),
			orderBook: NewOrderBook(tLogger),
			expected: makeOrderBook(
				4,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 5, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 6, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 4, 2, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			initialQueueState: []*cachedOrderNote{
				makeCachedBookOrderNote(
					makeBookOrderNote(4, "ob", [32]byte{'e'}, msgjson.SellOrderNum, 4, 2, 12)),
			},
			initialSyncState: false,
			wantErr:          false,
		},
	}

	for idx, tc := range tests {
		tc.orderBook.noteQueue = tc.initialQueueState
		tc.orderBook.setSynced(tc.initialSyncState)
		err := tc.orderBook.Sync(tc.snapshot)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.Sync] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if tc.orderBook.seq != tc.expected.seq {
				t.Fatalf("[OrderBook.Sync] #%d: expected sequence of %d,"+
					" got %d", idx+1, tc.expected.seq, tc.orderBook.seq)
			}

			if tc.orderBook.marketID != tc.expected.marketID {
				t.Fatalf("[OrderBook.Sync] #%d: expected market id of %s,"+
					" got %s", idx+1, tc.expected.marketID, tc.orderBook.marketID)
			}

			if len(tc.orderBook.orders) != len(tc.expected.orders) {
				t.Fatalf("[OrderBook.Sync] #%d: expected orders size of %d,"+
					" got %d", idx+1, len(tc.expected.orders), len(tc.orderBook.orders))
			}

			if len(tc.orderBook.buys.bins) != len(tc.expected.buys.bins) {
				t.Fatalf("[OrderBook.Sync] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.buys.bins),
					len(tc.orderBook.buys.bins))
			}

			if len(tc.orderBook.sells.bins) != len(tc.expected.sells.bins) {
				t.Fatalf("[OrderBook.Sync] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.sells.bins),
					len(tc.orderBook.sells.bins))
			}

			if len(tc.orderBook.noteQueue) != len(tc.expected.noteQueue) {
				t.Fatalf("[OrderBook.Sync] #%d: expected note queue "+
					"size of %d, got %d", idx+1, len(tc.expected.noteQueue),
					len(tc.orderBook.noteQueue))
			}
		}
	}
}

func TestOrderBookBook(t *testing.T) {
	tests := []struct {
		label     string
		orderBook *OrderBook
		note      *msgjson.BookOrderNote
		expected  *OrderBook
		wantErr   bool
	}{
		{
			label: "Book a buy order",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			note: makeBookOrderNote(3, "ob", [32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
			expected: makeOrderBook(
				3,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			wantErr: false,
		},
		// May want to re-implement strict sequence checking. Might use these tests
		// again.
		// {
		// 	label: "Book buy order with outdated sequence value",
		// 	orderBook: makeOrderBook(
		// 		2,
		// 		"ob",
		// 		[]*Order{
		// 			makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
		// 			makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
		// 		},
		// 		make([]*cachedOrderNote, 0),
		// 		true,
		// 	),
		// 	note: makeBookOrderNote(0, "ob", [32]byte{'a'}, msgjson.BuyOrderNum, 2, 3, 1),
		// 	expected: makeOrderBook(
		// 		2,
		// 		"ob",
		// 		[]*Order{
		// 			makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
		// 			makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
		// 		},
		// 		make([]*cachedOrderNote, 0),
		// 		true,
		// 	),
		// 	wantErr: false,
		// },
		{
			label: "Book buy order to unsynced order book",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				false,
			),
			note: makeBookOrderNote(3, "ob", [32]byte{'a'}, msgjson.BuyOrderNum, 2, 3, 10),
			expected: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
				[]*cachedOrderNote{
					makeCachedBookOrderNote(
						makeBookOrderNote(3, "ob", [32]byte{'a'}, msgjson.BuyOrderNum, 2, 3, 10)),
				},
				false,
			),
			wantErr: false,
		},
		{
			label: "Book sell order to order book with different market id",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			note:     makeBookOrderNote(3, "oc", [32]byte{'a'}, msgjson.SellOrderNum, 2, 3, 10),
			expected: nil,
			wantErr:  true,
		},
		// May want to re-implement strict sequence checking. Might use these tests
		// again.
		// {
		// 	label: "Book sell order to synced order book with future sequence value",
		// 	orderBook: makeOrderBook(
		// 		2,
		// 		"ob",
		// 		[]*Order{
		// 			makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
		// 			makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
		// 		},
		// 		make([]*cachedOrderNote, 0),
		// 		true,
		// 	),
		// 	note:     makeBookOrderNote(5, "ob", [32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
		// 	expected: nil,
		// 	wantErr:  true,
		// },
	}

	for idx, tc := range tests {
		err := tc.orderBook.Book(tc.note)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.Book:%s]: error: %v, wantErr: %v",
				tc.label, err, tc.wantErr)
		}

		if !tc.wantErr {
			if tc.orderBook.seq != tc.expected.seq {
				t.Fatalf("[OrderBook.Book] #%d: expected sequence of %d,"+
					" got %d", idx+1, tc.expected.seq, tc.orderBook.seq)
			}

			if tc.orderBook.marketID != tc.expected.marketID {
				t.Fatalf("[OrderBook.Book] #%d: expected market id of %s,"+
					" got %s", idx+1, tc.expected.marketID, tc.orderBook.marketID)
			}

			if len(tc.orderBook.orders) != len(tc.expected.orders) {
				t.Fatalf("[OrderBook.Book] #%d: expected orders size of %d,"+
					" got %d", idx+1, len(tc.expected.orders), len(tc.orderBook.orders))
			}

			if len(tc.orderBook.buys.bins) != len(tc.expected.buys.bins) {
				t.Fatalf("[OrderBook.Book] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.buys.bins),
					len(tc.orderBook.buys.bins))
			}

			if len(tc.orderBook.sells.bins) != len(tc.expected.sells.bins) {
				t.Fatalf("[OrderBook.Book] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.sells.bins),
					len(tc.orderBook.sells.bins))
			}

			if len(tc.orderBook.noteQueue) != len(tc.expected.noteQueue) {
				t.Fatalf("[OrderBook.Book] #%d: expected note queue "+
					"size of %d, got %d", idx+1, len(tc.expected.noteQueue),
					len(tc.orderBook.noteQueue))
			}
		}
	}
}

func TestOrderBookUpdateRemaining(t *testing.T) {
	var qty uint64 = 10
	var remaining uint64 = 5
	mid := "abc_xyz"
	oid := order.OrderID{0x01}

	book := makeOrderBook(
		1,
		mid,
		[]*Order{
			makeOrder(oid, msgjson.SellOrderNum, qty, 1, 2),
		},
		make([]*cachedOrderNote, 0),
		true,
	)

	urNote := &msgjson.UpdateRemainingNote{
		OrderNote: msgjson.OrderNote{
			OrderID:  oid[:],
			MarketID: mid,
		},
		Remaining: remaining,
	}

	err := book.UpdateRemaining(urNote)
	if err != nil {
		t.Fatalf("error updating remaining qty: %v", err)
	}
	_, sells, _ := book.Orders()
	if sells[0].Quantity != remaining {
		t.Fatalf("failed to update remaining,. Wanted %d, got %d", remaining, sells[0].Quantity)
	}

	// Unknown order
	wrongID := order.OrderID{0x02}
	urNote.OrderID = wrongID[:]
	err = book.UpdateRemaining(urNote)
	if err == nil {
		t.Fatalf("no error updating remaining qty for unknown order")
	}

	// Bad order ID
	urNote.OrderID = []byte{0x03}
	err = book.UpdateRemaining(urNote)
	if err == nil {
		t.Fatalf("no error updating remaining qty for invalid order ID")
	}
}

func TestOrderBookUnbook(t *testing.T) {
	tests := []struct {
		label     string
		orderBook *OrderBook
		note      *msgjson.UnbookOrderNote
		expected  *OrderBook
		wantErr   bool
	}{
		{
			label: "Unbook sell order with synced order book",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			note: makeUnbookOrderNote(3, "ob", [32]byte{'c'}),
			expected: makeOrderBook(
				3,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			wantErr: false,
		},
		// May want to re-implement strict sequence checking. Might use these tests
		// again.
		// {
		// 	label: "Unbook sell order with outdated sequence value",
		// 	orderBook: makeOrderBook(
		// 		2,
		// 		"ob",
		// 		[]*Order{
		// 			makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
		// 			makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
		// 		},
		// 		make([]*cachedOrderNote, 0),
		// 		true,
		// 	),
		// 	note: makeUnbookOrderNote(0, "ob", [32]byte{'a'}),
		// 	expected: makeOrderBook(
		// 		2,
		// 		"ob",
		// 		[]*Order{
		// 			makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
		// 			makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
		// 		},
		// 		make([]*cachedOrderNote, 0),
		// 		true,
		// 	),
		// 	wantErr: false,
		// },
		{
			label: "Unbook sell order with unsynced order book",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				false,
			),
			note: makeUnbookOrderNote(3, "ob", [32]byte{'a'}),
			expected: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				[]*cachedOrderNote{
					makeCachedUnbookOrderNote(
						makeUnbookOrderNote(3, "ob", [32]byte{'a'})),
				},
				false,
			),
			wantErr: false,
		},
		{
			label: "Unbook sell order with different market id",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			note:     makeUnbookOrderNote(3, "oc", [32]byte{'a'}),
			expected: nil,
			wantErr:  true,
		},
		{
			label: "Unbook sell order with future sequence value",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			note:     makeUnbookOrderNote(5, "ob", [32]byte{'d'}),
			expected: nil,
			wantErr:  true,
		},
	}

	for idx, tc := range tests {
		err := tc.orderBook.Unbook(tc.note)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.Book:%s]: error: %v, wantErr: %v",
				tc.label, err, tc.wantErr)
		}

		if !tc.wantErr {
			if tc.orderBook.seq != tc.expected.seq {
				t.Fatalf("[OrderBook.Book] #%d: expected sequence of %d,"+
					" got %d", idx+1, tc.expected.seq, tc.orderBook.seq)
			}

			if tc.orderBook.marketID != tc.expected.marketID {
				t.Fatalf("[OrderBook.Book] #%d: expected market id of %s,"+
					" got %s", idx+1, tc.expected.marketID, tc.orderBook.marketID)
			}

			if len(tc.orderBook.orders) != len(tc.expected.orders) {
				t.Fatalf("[OrderBook.Book] #%d: expected orders size of %d,"+
					" got %d", idx+1, len(tc.expected.orders), len(tc.orderBook.orders))
			}

			if len(tc.orderBook.buys.bins) != len(tc.expected.buys.bins) {
				t.Fatalf("[OrderBook.Book] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.buys.bins),
					len(tc.orderBook.buys.bins))
			}

			if len(tc.orderBook.sells.bins) != len(tc.expected.sells.bins) {
				t.Fatalf("[OrderBook.Book] #%d: expected buys book side "+
					"size of %d, got %d", idx+1, len(tc.expected.sells.bins),
					len(tc.orderBook.sells.bins))
			}

			if len(tc.orderBook.noteQueue) != len(tc.expected.noteQueue) {
				t.Fatalf("[OrderBook.Book] #%d: expected note queue "+
					"size of %d, got %d", idx+1, len(tc.expected.noteQueue),
					len(tc.orderBook.noteQueue))
			}
		}
	}
}

func TestOrderBookBestNOrders(t *testing.T) {
	tests := []struct {
		label     string
		orderBook *OrderBook
		n         int
		side      uint8
		expected  []*Order
		wantErr   bool
	}{
		{
			label: "Fetch best N orders from buy book side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			n:    3,
			side: msgjson.BuyOrderNum,
			expected: []*Order{
				makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 8, 4, 12),
				makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
				makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
			},
			wantErr: false,
		},
		{
			label: "Fetch best N orders from empty sell book side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			n:        3,
			side:     msgjson.SellOrderNum,
			expected: []*Order{},
			wantErr:  false,
		},
		{
			label: "Fetch best N orders (all orders) from sell book side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			n:    5,
			side: msgjson.SellOrderNum,
			expected: []*Order{
				makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
				makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
				makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 8, 4, 12),
			},
			wantErr: false,
		},
		{
			label: "Fetch best N orders from unsynced order book",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				false,
			),
			n:        5,
			side:     msgjson.SellOrderNum,
			expected: nil,
			wantErr:  true,
		},
		{
			label: "Fetch best N orders (some orders) from sell book side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			n:    3,
			side: msgjson.SellOrderNum,
			expected: []*Order{
				makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
				makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
			},
			wantErr: false,
		},
	}

	for idx, tc := range tests {
		best, _, err := tc.orderBook.BestNOrders(tc.n, tc.side)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.BestNOrders] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if len(best) != len(tc.expected) {
				t.Fatalf("[OrderBook.BestNOrders] #%d: expected best N orders "+
					"size of %d, got %d", idx+1, len(tc.expected),
					len(best))
			}

			for i := 0; i < len(best); i++ {
				if !bytes.Equal(best[i].OrderID[:], tc.expected[i].OrderID[:]) {
					t.Fatalf("[OrderBook.BestNOrders] #%d: expected order at "+
						"index %d to be %x, got %x", idx+1, i,
						tc.expected[i].OrderID[:], best[i].OrderID[:])
				}

				if best[i].Quantity != tc.expected[i].Quantity {
					t.Fatalf("[OrderBook.BestNOrders] #%d: expected order "+
						"quantity at index %d to be %x, got %x", idx+1, i,
						tc.expected[i].Quantity, best[i].Quantity)
				}

				if best[i].Time != tc.expected[i].Time {
					t.Fatalf("[OrderBook.BestNOrders] #%d: expected order "+
						"timestamp at index %d to be %x, got %x", idx+1, i,
						tc.expected[i].Time, best[i].Time)
				}
			}
		}
	}
}

func TestOrderBookBestFill(t *testing.T) {
	tests := []struct {
		label     string
		orderBook *OrderBook
		qty       uint64
		side      uint8
		expected  []*Fill
	}{
		{
			label: "Fetch best fill from buy side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			qty:  24,
			side: msgjson.BuyOrderNum,
			expected: []*Fill{
				{
					Rate:     4,
					Quantity: 8,
				},
				{
					Rate:     3,
					Quantity: 5,
				},
				{
					Rate:     2,
					Quantity: 10,
				},
				{
					Rate:     1,
					Quantity: 1,
				},
			},
		},
		{
			label: "Fetch best fill from empty buy side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
					makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
					makeOrder([32]byte{'e'}, msgjson.SellOrderNum, 8, 4, 12),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			qty:      24,
			side:     msgjson.BuyOrderNum,
			expected: []*Fill{},
		},
		{
			label: "Fetch best fill (order book total less than fill quantity) from buy side",
			orderBook: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			qty:  40,
			side: msgjson.BuyOrderNum,
			expected: []*Fill{
				{
					Rate:     2,
					Quantity: 10,
				},
				{
					Rate:     1,
					Quantity: 10,
				},
			},
		},
	}

	// bestFill returns the best fill for a quantity from the provided side.
	bestFill := func(ob *OrderBook, qty uint64, side uint8) ([]*Fill, bool) {
		switch side {
		case msgjson.BuyOrderNum:
			return ob.buys.BestFill(qty)
		case msgjson.SellOrderNum:
			return ob.sells.BestFill(qty)
		}
		return nil, false
	}

	for idx, tc := range tests {
		best, _ := bestFill(tc.orderBook, tc.qty, tc.side)

		if len(best) != len(tc.expected) {
			t.Fatalf("[OrderBook.BestFill] #%d: expected best fill "+
				"size of %d, got %d", idx+1, len(tc.expected), len(best))
		}

		for i := 0; i < len(best); i++ {
			if best[i].Rate != tc.expected[i].Rate {
				t.Fatalf("[OrderBook.BestFill] #%d: expected fill at "+
					"index %d to have rate %d, got %d", idx+1, i,
					tc.expected[i].Rate, best[i].Rate)
			}

			if best[i].Quantity != tc.expected[i].Quantity {
				t.Fatalf("[OrderBook.BestFill] #%d: expected fill at "+
					"index %d to have quantity %d, got %d", idx+1, i,
					tc.expected[i].Quantity, best[i].Quantity)
			}
		}
	}
}

func TestValidateMatchProof(t *testing.T) {
	mid := "mkt"
	ob := NewOrderBook(tLogger)
	epoch := uint64(10)
	n1Pimg := [32]byte{'1'}
	n1Commitment := makeCommitment(n1Pimg)
	n1OrderID := [32]byte{'a'}
	n1 := makeEpochOrderNote(mid, n1OrderID, msgjson.BuyOrderNum, 1, 2, n1Commitment, epoch)

	n2Pimg := [32]byte{'2'}
	n2Commitment := makeCommitment(n2Pimg)
	n2OrderID := [32]byte{'b'}
	n2 := makeEpochOrderNote(mid, n2OrderID, msgjson.BuyOrderNum, 1, 2, n2Commitment, epoch)

	n3Pimg := [32]byte{'3'}
	n3Commitment := makeCommitment(n3Pimg)
	n3OrderID := [32]byte{'c'}
	n3 := makeEpochOrderNote(mid, n3OrderID, msgjson.BuyOrderNum, 1, 2, n3Commitment, epoch)

	err := ob.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	expectedCSum, _ := hex.DecodeString("9db8c0547f3b80574df730c3b7005ccef" +
		"4310e93f766442110fc2c9353230985")
	expectedSeed, _ := hex.DecodeString("e2b770f60baab7ac877edfa55bd1443b59" +
		"1c1cdd461667c6eb737ae0c65daf2d")

	matchProofNote := msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch,
		Preimages: []msgjson.Bytes{n1Pimg[:], n2Pimg[:], n3Pimg[:]},
		Misses:    []msgjson.Bytes{},
		CSum:      expectedCSum,
		Seed:      expectedSeed,
	}

	// Ensure a valid match proof message gets validated as expected.
	if err := ob.ValidateMatchProof(matchProofNote); err != nil {
		t.Fatalf("[ValidateMatchProof]: unexpected error: %v", err)
	}

	ob = NewOrderBook(tLogger)

	err = ob.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	expectedSeedWithMisses, _ := hex.DecodeString("01a161289f06be16ea9b5a5a" +
		"5492f5664f3e92750dc5ce3fa5775eb9be225730")
	expectedCSumWithMisses, _ := hex.DecodeString("0433a2dec5f3b9f530fba28a" +
		"d1b4c15c454b4b41ab3bd0ba8f30a6d1de2a1128")

	matchProofNoteWithMisses := msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch,
		Preimages: []msgjson.Bytes{n1Pimg[:], n3Pimg[:]},
		Misses:    []msgjson.Bytes{n2.OrderID},
		CSum:      expectedCSumWithMisses,
		Seed:      expectedSeedWithMisses,
	}

	// Ensure a valid match proof message gets validated as expected.
	if err := ob.ValidateMatchProof(matchProofNoteWithMisses); err != nil {
		t.Fatalf("[ValidateMatchProof (with misses)]: unexpected error: %v", err)
	}

	ob = NewOrderBook(tLogger)

	// firstProof for idx-1, length mismatch ignored
	emptyProofNote := msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch - 1,                  // previous
		Preimages: []msgjson.Bytes{n1Pimg[:]}, // ob will be missing this, but it's the firstProof, so no error
	}
	if err := ob.ValidateMatchProof(emptyProofNote); err != nil {
		t.Fatalf("[ValidateMatchProof (empty)]: unexpected error: %v", err)
	}

	// next proof will not permit length mismatches
	err = ob.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	expectedCSum, _ = hex.DecodeString("9db8c0547f3b80574df730c3b7005ccef" +
		"4310e93f766442110fc2c9353230985")
	expectedSeed, _ = hex.DecodeString("e2b770f60baab7ac877edfa55bd1443b59" +
		"1c1cdd461667c6eb737ae0c65daf2d")

	matchProofNote = msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch,
		Preimages: []msgjson.Bytes{n1Pimg[:], n2Pimg[:]},
		Misses:    []msgjson.Bytes{},
		CSum:      expectedCSum,
		Seed:      expectedSeed,
	}

	// Ensure a invalid match proof message (missing a preimage) gets
	// detected as expected.
	if err := ob.ValidateMatchProof(matchProofNote); err == nil {
		t.Fatalf("[ValidateMatchProof (missing a preimage)]: unexpected an error")
	}

	ob = NewOrderBook(tLogger)

	err = ob.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	expectedCSum, _ = hex.DecodeString("0433a2dec5f3b9f530fba28a" +
		"d1b4c15c454b4b41ab3bd0ba8f30a6d1de2a1128")
	expectedSeed, _ = hex.DecodeString("e2b770f60baab7ac877edfa55bd1443b59" +
		"1c1cdd461667c6eb737ae0c65daf2d")

	matchProofNote = msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch,
		Preimages: []msgjson.Bytes{n1Pimg[:], n3Pimg[:]},
		Misses:    []msgjson.Bytes{n2.OrderID},
		CSum:      expectedCSum,
		Seed:      expectedSeed,
	}

	// Ensure a invalid match proof message (invalid seed) gets detected
	// as expected.
	if err := ob.ValidateMatchProof(matchProofNote); err == nil {
		t.Fatalf("[ValidateMatchProof (inavlid seed)]: unexpected error: %v", err)
	}

	ob = NewOrderBook(tLogger)

	err = ob.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = ob.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	expectedCSum, _ = hex.DecodeString("9db8c0547f3b80574df730c3b7005ccef" +
		"4310e93f766442110fc2c9353230985")
	expectedSeed, _ = hex.DecodeString("01a161289f06be16ea9b5a5a" +
		"5492f5664f3e92750dc5ce3fa5775eb9be225730")

	matchProofNote = msgjson.MatchProofNote{
		MarketID:  mid,
		Epoch:     epoch,
		Preimages: []msgjson.Bytes{n1Pimg[:], n3Pimg[:]},
		Misses:    []msgjson.Bytes{n2.OrderID},
		CSum:      expectedCSum,
		Seed:      expectedSeed,
	}

	// Ensure a invalid match proof message (invalid csum) gets detected
	// as expected.
	if err := ob.ValidateMatchProof(matchProofNote); err == nil {
		t.Fatalf("[ValidateMatchProof (inavlid csum)]: unexpected error: %v", err)
	}
}
