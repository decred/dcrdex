package core

import (
	"testing"

	"decred.org/dcrdex/dex/order"
)

func TestOrderReader_StatusString(t *testing.T) {
	tests := []struct {
		name       string
		status     order.OrderStatus
		matches    []*Match
		qty        uint64
		orderType  order.OrderType
		sell       bool
		cancelling bool
		want       string
	}{
		{
			name:      "fully cancelled order - no fills",
			status:    order.OrderStatusCanceled,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: true, Qty: 1000, Rate: 1e8, Active: false},
			},
			want: "canceled",
		},
		{
			name:      "partially filled then cancelled",
			status:    order.OrderStatusCanceled,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 500, Rate: 1e8, Active: false}, // Actual fill
				{IsCancel: true, Qty: 500, Rate: 1e8, Active: false},  // Cancel match
			},
			want: "canceled/partially filled",
		},
		{
			name:      "cancelled with active matches",
			status:    order.OrderStatusCanceled,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 500, Rate: 1e8, Active: true}, // Active fill
			},
			want: "canceled/settling",
		},
		{
			name:      "executed - fully filled",
			status:    order.OrderStatusExecuted,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 1000, Rate: 1e8, Active: false},
			},
			want: "executed",
		},
		{
			name:      "executed - no match",
			status:    order.OrderStatusExecuted,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches:   []*Match{},
			want:      "no match",
		},
		{
			name:      "executed - partially filled",
			status:    order.OrderStatusExecuted,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 500, Rate: 1e8, Active: false},
			},
			want: "executed",
		},
		{
			name:      "booked",
			status:    order.OrderStatusBooked,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches:   []*Match{},
			want:      "booked",
		},
		{
			name:       "booked - cancelling",
			status:     order.OrderStatusBooked,
			qty:        1000,
			orderType:  order.LimitOrderType,
			sell:       true,
			cancelling: true,
			matches:    []*Match{},
			want:       "cancelling",
		},
		{
			name:      "revoked - no fills",
			status:    order.OrderStatusRevoked,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches:   []*Match{},
			want:      "revoked",
		},
		{
			name:      "revoked - partially filled",
			status:    order.OrderStatusRevoked,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 500, Rate: 1e8, Active: false},
			},
			want: "revoked/partially filled",
		},
		{
			name:      "revoked - with active matches",
			status:    order.OrderStatusRevoked,
			qty:       1000,
			orderType: order.LimitOrderType,
			sell:      true,
			matches: []*Match{
				{IsCancel: false, Qty: 500, Rate: 1e8, Active: true},
			},
			want: "revoked/settling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ord := &OrderReader{
				Order: &Order{
					Status:     tt.status,
					Qty:        tt.qty,
					Type:       tt.orderType,
					Sell:       tt.sell,
					Cancelling: tt.cancelling,
					Matches:    tt.matches,
				},
			}

			got := ord.StatusString()
			if got != tt.want {
				t.Errorf("OrderReader.StatusString() = %v, want %v", got, tt.want)
			}
		})
	}
}
