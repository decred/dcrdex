package matcher

import (
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/market/order"
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
)

var (
	marketOrders = []*order.MarketOrder{
		{
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
			UTXOs:    []order.UTXO{},
			Side:     order.BuySide,
			Quantity: 132413241324,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
		{
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497654, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
			UTXOs:    []order.UTXO{},
			Side:     order.SellSide,
			Quantity: 32413241324,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
	}

	limitOrders = []*order.LimitOrder{
		{
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
				},
				UTXOs:    []order.UTXO{},
				Side:     order.BuySide,
				Quantity: 102413241324,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			Rate:  0.13241324,
			Force: order.StandingTiF,
		},
	}
)

func Test_shuffleQueue(t *testing.T) {
	// order queues to be shuffled
	q3_1 := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []order.Order{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Shuffled is the expected result of sorting q3_1 and q3_2
	q3Shuffled := []order.Order{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// shuffleQueue should work with nil slice
	var qNil []order.Order

	// shuffleQueue should work with empty slice
	qEmpty := []order.Order{}

	// shuffleQueue should work with single element slice
	q1 := []order.Order{
		marketOrders[0],
	}

	// shuffleQueue should work with two element slice
	q2_a := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	// repeated orders should be handled
	qDup := []order.Order{
		marketOrders[0],
		marketOrders[0],
	}

	q2Shuffled := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	tests := []struct {
		name  string
		inOut []order.Order
		want  []order.Order
	}{
		{
			"q3_1 iter 1",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_1 iter 2",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_1 iter 3",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_2",
			q3_2,
			q3Shuffled,
		},
		{
			"qEmpty",
			qEmpty,
			[]order.Order{},
		},
		{
			"qNil",
			qNil,
			[]order.Order(nil),
		},
		{
			"q1",
			q1,
			[]order.Order{q2Shuffled[0]},
		},
		{
			"q2_a",
			q2_a,
			q2Shuffled,
		},
		{
			"q2_b",
			q2_b,
			q2Shuffled,
		},
		{
			"qDup",
			qDup,
			[]order.Order{q2Shuffled[0], q2Shuffled[0]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shuffleQueue(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("shuffleQueue(q): q = %#v, want %#v", tt.inOut, tt.want)
			}
		})
	}
}

func Test_sortQueue(t *testing.T) {
	// order queues to be sorted
	q3_1 := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []order.Order{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Sorted is the expected result of sorting q3_1 and q3_2
	q3Sorted := []order.Order{
		marketOrders[0],
		marketOrders[1],
		limitOrders[0],
	}

	// sortQueue should work with nil slice
	var qNil []order.Order

	// sortQueue should work with empty slice
	qEmpty := []order.Order{}

	// sortQueue should work with single element slice
	q1 := []order.Order{
		marketOrders[0],
	}

	// sortQueue should work with two element slice
	q2_a := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	// repeated orders should be handled
	qDup := []order.Order{
		marketOrders[0],
		marketOrders[0],
	}

	q2Sorted := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	tests := []struct {
		name  string
		inOut []order.Order
		want  []order.Order
	}{
		{
			"q3_1 iter 1",
			q3_1,
			q3Sorted,
		},
		{
			"q3_1 iter 2",
			q3_1,
			q3Sorted,
		},
		{
			"q3_1 iter 3",
			q3_1,
			q3Sorted,
		},
		{
			"q3_2",
			q3_2,
			q3Sorted,
		},
		{
			"qEmpty",
			qEmpty,
			[]order.Order{},
		},
		{
			"qNil",
			qNil,
			[]order.Order(nil),
		},
		{
			"q1",
			q1,
			[]order.Order{q2Sorted[0]},
		},
		{
			"q2_a",
			q2_a,
			q2Sorted,
		},
		{
			"q2_b",
			q2_b,
			q2Sorted,
		},
		{
			"qDup",
			qDup,
			[]order.Order{q2Sorted[0], q2Sorted[0]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortQueue(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("sortQueue(q): q = %#v, want %#v", tt.inOut, tt.want)
			}
		})
	}
}
