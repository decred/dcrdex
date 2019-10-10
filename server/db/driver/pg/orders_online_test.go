// +build pgonline

package pg

import (
	"testing"

	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
)

func TestStoreLoadOrders(t *testing.T) {
	startLogger()
	type args struct {
		lo     *order.LimitOrder
		status types.OrderStatus
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
				status: types.OrderStatusBooked,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4700000, 1, order.StandingTiF, 0),
				status: types.OrderStatusCanceled,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4800000, 1, order.StandingTiF, 0),
				status: types.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
				status: types.OrderStatusFailed,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: types.OrderStatusMatched,
			},
			wantErr: false,
		},
		{
			name: "bad",
			args: args{
				lo:     newLimitOrder(false, 5000000, 1, order.ImmediateTiF, 0),
				status: types.OrderStatusMatched,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := archie.StoreOrder(tt.args.lo, tt.args.status)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
