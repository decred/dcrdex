// +build pgonline

package pg

import (
	"testing"

	"github.com/decred/dcrdex/server/db"
	"github.com/decred/dcrdex/server/order"
)

func TestStoreLoadOrders(t *testing.T) {
	startLogger()
	type args struct {
		lo     *order.LimitOrder
		status db.OrderStatus
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
				status: db.OrderStatusBooked,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4700000, 1, order.StandingTiF, 0),
				status: db.OrderStatusCanceled,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4800000, 1, order.StandingTiF, 0),
				status: db.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
				status: db.OrderStatusFailed,
			},
			wantErr: false,
		},
		{
			name: "ok",
			args: args{
				lo:     newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: db.OrderStatusMatched,
			},
			wantErr: false,
		},
		{
			name: "bad",
			args: args{
				lo:     newLimitOrder(false, 5000000, 1, order.ImmediateTiF, 0),
				status: db.OrderStatusMatched,
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
