package order

import (
	"bytes"
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
	ob := &OrderBook{
		seq:       seq,
		marketID:  marketID,
		noteQueue: cachedOrders,
		orders:    make(map[order.OrderID]*Order),
		buys:      NewBookSide(descending),
		sells:     NewBookSide(ascending),
	}

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
			orderBook: NewOrderBook(),
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
			orderBook: NewOrderBook(),
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
			orderBook:         NewOrderBook(),
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
			orderBook: NewOrderBook(),
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
			orderBook: NewOrderBook(),
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
		{
			label: "Book buy order with outdated sequence value",
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
			note: makeBookOrderNote(0, "ob", [32]byte{'a'}, msgjson.BuyOrderNum, 2, 3, 1),
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
			wantErr: false,
		},
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
		{
			label: "Book sell order to synced order book with future sequence value",
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
			note:     makeBookOrderNote(5, "ob", [32]byte{'d'}, msgjson.SellOrderNum, 5, 3, 10),
			expected: nil,
			wantErr:  true,
		},
	}

	for idx, tc := range tests {
		err := tc.orderBook.Book(tc.note)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.Book] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
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
		{
			label: "Unbook sell order with outdated sequence value",
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
			note: makeUnbookOrderNote(0, "ob", [32]byte{'a'}),
			expected: makeOrderBook(
				2,
				"ob",
				[]*Order{
					makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 10, 1, 2),
					makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 10, 2, 5),
				},
				make([]*cachedOrderNote, 0),
				true,
			),
			wantErr: false,
		},
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
			t.Fatalf("[OrderBook.Book] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
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
		expected  []*fill
		wantErr   bool
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
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 8, 4, 12),
					quantity: 8,
				},
				{
					match:    makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 3, 10),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					quantity: 10,
				},
				{
					match:    makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					quantity: 1,
				},
			},
			wantErr: false,
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
			expected: []*fill{},
			wantErr:  false,
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
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 10, 2, 5),
					quantity: 10,
				},
				{
					match:    makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 10, 1, 2),
					quantity: 10,
				},
			},
			wantErr: false,
		},
		{
			label: "Fetch best fill from unsynced order book",
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
			qty:      10,
			side:     msgjson.SellOrderNum,
			expected: nil,
			wantErr:  true,
		},
	}

	for idx, tc := range tests {
		best, err := tc.orderBook.BestFill(tc.qty, tc.side)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[OrderBook.BestFill] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if len(best) != len(tc.expected) {
				t.Fatalf("[OrderBook.BestFill] #%d: expected best fill "+
					"size of %d, got %d", idx+1, len(tc.expected), len(best))
			}

			for i := 0; i < len(best); i++ {
				if !bytes.Equal(best[i].match.OrderID[:], tc.expected[i].match.OrderID[:]) {
					t.Fatalf("[OrderBook.BestFill] #%d: expected fill at "+
						"index %d to be %x, got %x", idx+1, i,
						tc.expected[i].match.OrderID[:], best[i].match.OrderID[:])
				}

				if best[i].quantity != tc.expected[i].quantity {
					t.Fatalf("[OrderBook.BestFill] #%d: expected fill at "+
						"index %d to have quantity %d, got %d", idx+1, i,
						tc.expected[i].quantity, best[i].quantity)
				}

				if best[i].match.Time != tc.expected[i].match.Time {
					t.Fatalf("[OrderBook.BestFill] #%d: expected fill at "+
						"index %d to have match timestamp %d, got %d", idx+1, i,
						tc.expected[i].match.Time, best[i].match.Time)
				}
			}
		}
	}
}
