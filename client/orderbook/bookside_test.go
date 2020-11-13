package orderbook

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// makeBookSideDepth creates a new book side depth from the provided
// group and sort order.
func makeBookSide(groups map[uint64][]*Order, rateIndex *rateIndex, orderPref orderPreference) *bookSide {
	return &bookSide{
		bins:      groups,
		rateIndex: rateIndex,
		orderPref: orderPref,
	}
}

func makeOrder(id order.OrderID, side uint8, quantity uint64, rate uint64, time uint64) *Order {
	return &Order{
		OrderID:  id,
		Side:     side,
		Quantity: quantity,
		Rate:     rate,
		Time:     time,
	}
}

func TestBookSideAdd(t *testing.T) {
	tests := []struct {
		label    string
		side     *bookSide
		entry    *Order
		expected *bookSide
	}{
		{
			label: "Add order to buy book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 3, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			entry: makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 1, 2, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 3, 2, 10),
						makeOrder([32]byte{'e'}, msgjson.BuyOrderNum, 1, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
		},
		{
			label: "Add order to new bin of a buy book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			entry: makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
		},
		{
			label: "Add order to existing bin of a buy book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			entry: makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
		},
		{
			label: "Add order to existing buy book side bin sorted by order id",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			entry: makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
		},
		{
			label: "Add order to existing buy book side bin sorted by time",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			entry: makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 5),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 5),
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
		},
		{
			label: "Add order to sell book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
			entry: makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
		},
		{
			label: "Add order to sell book side bin sorted by time",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
			entry: makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {

						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
		},
	}

	for idx, tc := range tests {
		tc.side.Add(tc.entry)
		if len(tc.side.bins) != len(tc.expected.bins) {
			t.Fatalf("[BookSide.Add] #%d: expected bin size of %d,"+
				" got %d", idx+1, len(tc.expected.bins), len(tc.side.bins))
		}

		for price, bin := range tc.side.bins {
			expected := tc.expected.bins[price]
			for i := 0; i < len(bin); i++ {
				if !bytes.Equal(bin[i].OrderID[:], expected[i].OrderID[:]) {
					t.Fatalf("[BookSide.Add] #%d: expected price bin %d "+
						"entry at index %d to have id %x, got %x", idx+1,
						price, idx, expected[i].OrderID[:], bin[i].OrderID[:])
				}

				if bin[i].Time != expected[i].Time {
					t.Fatalf("[BookSide.Add] #%d: expected price bin %d "+
						"entry at index %d to have timestamp %d, got %d", idx+1,
						price, idx, expected[i].Time, bin[i].Time)
				}

				if bin[i].Quantity != expected[i].Quantity {
					t.Fatalf("[BookSide.Add] #%d: expected price bin %d "+
						"entry at index %d to have quantity %d, got %d", idx+1,
						price, idx, expected[i].Quantity, bin[i].Quantity)
				}
			}
		}

		entryBin := tc.side.bins[tc.entry.Rate]
		expectedBin := tc.expected.bins[tc.entry.Rate]

		if len(entryBin) != len(expectedBin) {
			t.Fatalf("[BookSide.Add] #%d: expected bin size of %d,"+
				" got %d", idx+1, len(expectedBin), len(entryBin))
		}

		if len(tc.side.rateIndex.Rates) != len(tc.expected.rateIndex.Rates) {
			t.Fatalf("[BookSide.Add] #%d: expected index size of %d,"+
				" got %d", idx+1, len(tc.expected.rateIndex.Rates),
				len(tc.side.rateIndex.Rates))
		}

		for i := 0; i < len(tc.side.rateIndex.Rates); i++ {
			if tc.side.rateIndex.Rates[i] !=
				tc.expected.rateIndex.Rates[i] {
				t.Fatalf("[BookSide.Add] #%d: expected index "+
					"rate value of %d at index %d, got %d", idx+1,
					tc.expected.rateIndex.Rates[i], i,
					tc.side.rateIndex.Rates[i])
			}
		}
	}
}

func TestBookSideRemove(t *testing.T) {
	tests := []struct {
		label    string
		side     *bookSide
		entry    *Order
		expected *bookSide
		wantErr  bool
	}{
		{
			label: "Remove order from buy book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 3, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			entry: makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 3, 2, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			wantErr: false,
		},
		{
			label: "Remove last order from sell book side bin",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 2, 2, 10),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				descending,
			),
			entry: makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 2, 2, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
			wantErr: false,
		},
		{
			label: "Remove non-existing order from buy book side",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			entry: makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 2, 2, 10),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 10, 1, 10),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 5, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				ascending,
			),
			wantErr: true,
		},
		{
			label: "Remove order from sell book side bin",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
			entry: makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 5, 1, 5),
			expected: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 10, 1, 2),
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 2, 1, 10),
					},
				},
				makeRateIndex([]uint64{1}),
				descending,
			),
			wantErr: false,
		},
	}

	for idx, tc := range tests {
		err := tc.side.Remove(tc.entry)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[BookSide.Remove] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if len(tc.side.bins) != len(tc.expected.bins) {
			t.Fatalf("[BookSide.Remove] #%d: expected bin size of %d,"+
				" got %d", idx+1, len(tc.expected.bins),
				len(tc.side.bins))
		}

		for price, bin := range tc.side.bins {
			expected := tc.expected.bins[price]
			for i := 0; i < len(bin); i++ {
				if !bytes.Equal(bin[i].OrderID[:], expected[i].OrderID[:]) {
					t.Fatalf("[BookSide.Remove] #%d: expected price bin %d "+
						"entry at index %d to have id %x, got %x", idx+1,
						price, idx, expected[i].OrderID[:], bin[i].OrderID[:])
				}

				if bin[i].Time != expected[i].Time {
					t.Fatalf("[BookSide.Remove] #%d: expected price bin %d "+
						"entry at index %d to have timestamp %d, got %d", idx+1,
						price, idx, expected[i].Time, bin[i].Time)
				}

				if bin[i].Quantity != expected[i].Quantity {
					t.Fatalf("[BookSide.Remove] #%d: expected price bin %d "+
						"entry at index %d to have quantity %d, got %d", idx+1,
						price, idx, expected[i].Quantity, bin[i].Quantity)
				}
			}
		}

		entryBin := tc.side.bins[tc.entry.Rate]
		expectedBin := tc.expected.bins[tc.entry.Rate]

		if len(entryBin) != len(expectedBin) {
			t.Fatalf("[BookSide.Remove] #%d: expected bin size of %d,"+
				" got %d", idx+1, len(expectedBin), len(entryBin))
		}

		if len(tc.side.rateIndex.Rates) != len(tc.expected.rateIndex.Rates) {
			t.Fatalf("[BookSide.Remove] #%d: expected index size of %d,"+
				" got %d", idx+1, len(tc.expected.rateIndex.Rates),
				len(tc.side.rateIndex.Rates))
		}

		for i := 0; i < len(tc.side.rateIndex.Rates); i++ {
			if tc.side.rateIndex.Rates[i] !=
				tc.expected.rateIndex.Rates[i] {
				t.Fatalf("[BookSide.Remove] #%d: expected "+
					" index value of %d at index %d, got %d", idx+1,
					tc.expected.rateIndex.Rates[i], i,
					tc.side.rateIndex.Rates[i])
			}
		}
	}
}

func TestBookSideBestNOrders(t *testing.T) {
	tests := []struct {
		label    string
		side     *bookSide
		n        int
		expected []*Order
	}{
		{
			label: "Fetch best N orders from buy book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			n: 3,
			expected: []*Order{
				makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
				makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
				makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
			},
		},
		{
			label: "Fetch best N orders from buy book side sorted in descending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				descending,
			),
			n: 3,
			expected: []*Order{
				makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
				makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
				makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
			},
		},
		{
			label: "Fetch best N orders from sell book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			n: 10,
			expected: []*Order{
				makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
				makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
				makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
			},
		},
		{
			label: "Fetch best N orders from sell book side sorted in descending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				descending,
			),
			n: 10,
			expected: []*Order{
				makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
				makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
				makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
				makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
			},
		},
		{
			label: "Fetch best N orders from empty book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{},
				makeRateIndex([]uint64{}),
				ascending,
			),
			n:        3,
			expected: []*Order{},
		},
	}

	for idx, tc := range tests {
		best, _ := tc.side.BestNOrders(tc.n)
		if len(best) != len(tc.expected) {
			t.Fatalf("[BookSide.BestNOrders] #%d: expected best "+
				"order size of %d, got %d", idx+1, len(tc.expected),
				len(best))
		}

		for i := 0; i < len(best); i++ {
			if best[i].OrderID != tc.expected[i].OrderID {
				t.Fatalf("[BookSide.BestNOrders] #%d: expected "+
					"order id %x at index of %d, got %x", idx+1,
					tc.expected[i].OrderID[:], idx, best[i].OrderID[:])
			}

			if best[i].Quantity != tc.expected[i].Quantity {
				t.Fatalf("[BookSide.BestNOrders] #%d: expected "+
					"quantity %d at index of %d, got %d", idx+1,
					tc.expected[i].Quantity, idx, best[i].Quantity)
			}

			if best[i].Time != tc.expected[i].Time {
				t.Fatalf("[BookSide.BestNOrders] #%d: expected "+
					"timestamp %d at index of %d, got %d", idx+1,
					tc.expected[i].Time, idx, best[i].Time)
			}
		}
	}
}

func TestBookSideBestFill(t *testing.T) {
	tests := []struct {
		label     string
		side      *bookSide
		quantity  uint64
		orderPref orderPreference
		expected  []*fill
		wantErr   bool
	}{
		{
			label: "Fetch best fill from buy book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			quantity: 9,
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
					quantity: 3,
				},
				{
					match:    makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
					quantity: 1,
				},
			},
			wantErr: false,
		},
		{
			label: "Fetch best fill from buy book side sorted in descending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.BuyOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				descending,
			),
			quantity: 7,
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'c'}, msgjson.BuyOrderNum, 1, 2, 2),
					quantity: 1,
				},
				{
					match:    makeOrder([32]byte{'d'}, msgjson.BuyOrderNum, 5, 2, 5),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'a'}, msgjson.BuyOrderNum, 5, 1, 2),
					quantity: 1,
				},
			},
			wantErr: false,
		},
		{
			label: "Fetch best fill from sell book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			quantity: 0,
			expected: []*fill{},
			wantErr:  false,
		},
		{
			label: "Fetch best fill from sell book side sorted in ascending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				ascending,
			),
			quantity: 9,
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					quantity: 3,
				},
				{
					match:    makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
					quantity: 1,
				},
			},
			wantErr: false,
		},
		{
			label: "Fetch best fill from sell book side sorted in descending order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				descending,
			),
			quantity: 50,
			expected: []*fill{
				{
					match:    makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
					quantity: 1,
				},
				{
					match:    makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
					quantity: 5,
				},
				{
					match:    makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					quantity: 3,
				},
			},
			wantErr: false,
		},
		{
			label: "Fetch best fill from sell book side sorted in unknown order",
			side: makeBookSide(
				map[uint64][]*Order{
					1: {
						makeOrder([32]byte{'a'}, msgjson.SellOrderNum, 5, 1, 2),
						makeOrder([32]byte{'b'}, msgjson.SellOrderNum, 3, 1, 5),
					},
					2: {
						makeOrder([32]byte{'c'}, msgjson.SellOrderNum, 1, 2, 2),
						makeOrder([32]byte{'d'}, msgjson.SellOrderNum, 5, 2, 5),
					},
				},
				makeRateIndex([]uint64{1, 2}),
				50,
			),
			quantity: 3,
			expected: nil,
			wantErr:  true,
		},
	}

	for idx, tc := range tests {
		best, err := tc.side.BestFill(tc.quantity)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[BookSide.BestFill] #%d: error: %v, "+
				"wantErr: %v", idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if len(best) != len(tc.expected) {
				t.Fatalf("[BookSide.BestFill] #%d: expected best "+
					"order size of %d, got %d", idx+1, len(tc.expected),
					len(best))
			}

			for i := 0; i < len(best); i++ {
				if best[i].match.OrderID != tc.expected[i].match.OrderID {
					t.Fatalf("[BookSide.BestFill] #%d: expected "+
						"order id %x at index of %d, got %x", idx+1,
						tc.expected[i].match.OrderID[:], idx,
						best[i].match.OrderID[:])
				}

				if best[i].quantity != tc.expected[i].quantity {
					t.Fatalf("[BookSide.BestFill] #%d: expected fill at "+
						"index %d to have quantity %d, got %d", idx+1, i,
						tc.expected[i].quantity, best[i].quantity)
				}

				if best[i].match.Time != tc.expected[i].match.Time {
					t.Fatalf("[BookSide.BestFill] #%d: expected "+
						"timestamp %d at index of %d, got %d", idx+1,
						tc.expected[i].match.Time, idx, best[i].match.Time)
				}
			}
		}
	}
}
