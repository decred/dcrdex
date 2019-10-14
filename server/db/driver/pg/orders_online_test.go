// +build pgonline

package pg

import (
	"encoding/hex"
	"github.com/davecgh/go-spew/spew"
	"testing"

	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
)

func TestStoreOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	orderBadLotSize := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadLotSize.Quantity /= 2

	orderBadMarket := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadMarket.BaseAsset = AssetDCR
	orderBadMarket.QuoteAsset = AssetDCR // same as base

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	type args struct {
		ord    order.Order
		status types.OrderStatus
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "ok limit booked (active)",
			args: args{
				ord:    newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
				status: types.OrderStatusBooked,
			},
			wantErr: false,
		},
		{
			name: "ok limit matched (active)",
			args: args{
				ord:    newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: types.OrderStatusMatched,
			},
			wantErr: false,
		},
		{
			name: "ok limit canceled (archived)",
			args: args{
				ord:    newLimitOrder(false, 4700000, 1, order.StandingTiF, 0),
				status: types.OrderStatusCanceled,
			},
			wantErr: false,
		},
		{
			name: "ok limit executed (archived)",
			args: args{
				ord:    newLimitOrder(false, 4800000, 1, order.StandingTiF, 0),
				status: types.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "ok limit failed (archived)",
			args: args{
				ord:    newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
				status: types.OrderStatusFailed,
			},
			wantErr: false,
		},
		{
			name: "limit duplicate",
			args: args{
				ord:    newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
				status: types.OrderStatusFailed,
			},
			wantErr: true,
		},
		{
			name: "limit bad quantity (lot size)",
			args: args{
				ord:    orderBadLotSize,
				status: types.OrderStatusPending,
			},
			wantErr: true,
		},
		{
			name: "limit bad trading pair",
			args: args{
				ord:    orderBadMarket,
				status: types.OrderStatusPending,
			},
			wantErr: true,
		},
		{
			name: "market sell - bad status (booked)",
			args: args{
				ord:    newMarketSellOrder(2, 0),
				status: types.OrderStatusBooked,
			},
			wantErr: true,
		},
		{
			name: "market sell - bad status (canceled)",
			args: args{
				ord:    newMarketSellOrder(2, 0),
				status: types.OrderStatusCanceled,
			},
			wantErr: true,
		},
		{
			name: "market sell - active",
			args: args{
				ord:    newMarketSellOrder(2, 0),
				status: types.OrderStatusSwapping,
			},
			wantErr: false,
		},
		{
			name: "market sell - archived",
			args: args{
				ord:    newMarketSellOrder(2, 1),
				status: types.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "market sell - already in other table",
			args: args{
				ord:    newMarketSellOrder(2, 1),
				status: types.OrderStatusExecuted,
			},
			wantErr: true,
		},
		{
			name: "market sell - duplicate archived order",
			args: args{
				ord:    newMarketSellOrder(2, 0), // dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d
				status: types.OrderStatusExecuted,
			},
			wantErr: true,
		},
		{
			name: "cancel order",
			args: args{
				ord:    newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0),
				status: types.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "cancel order - duplicate archived order",
			args: args{
				ord:    newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0),
				status: types.OrderStatusExecuted,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := archie.StoreOrder(tt.args.ord, tt.args.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("StoreOrder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadOrderUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var oid order.OrderID
	copy(oid[:], orderID0)

	ordOut, statusOut, err := archie.Order(oid, mktInfo.Base, mktInfo.Quote)
	if err == nil || ordOut != nil {
		t.Errorf("Order should have failed to load non-existent order")
	}
	if statusOut != types.OrderStatusUnknown {
		t.Errorf("status of non-existent order should be OrderStatusUnknown, got %s", statusOut)
	}
}

func TestStoreLoadLimitOrderActive(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// Limit: buy, standing, booked
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := types.OrderStatusBooked

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadLimitOrderArchived(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// Limit: buy, standing, executed
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := types.OrderStatusExecuted

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadMarketOrderActive(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// Limit: buy, standing, booked
	ordIn := newMarketSellOrder(1, 0)
	statusIn := types.OrderStatusSwapping

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: pending (active)
	ordIn := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)
	statusIn := types.OrderStatusPending

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	orderStatuses := []struct {
		ord    order.Order
		status types.OrderStatus
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			types.OrderStatusBooked, // active
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			types.OrderStatusExecuted, // archived
		},
		{
			newMarketSellOrder(2, 0),
			types.OrderStatusMatched, // active
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusFailed, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusMatched, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			types.OrderStatusFailed, // archived
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		statusOut, typeOut, filledOut, err := archie.OrderStatus(ordIn)
		if err != nil {
			t.Fatalf("OrderStatus(%d:%v) failed: %v", i, ordIn, err)
		}

		if statusOut != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				statusOut, statusIn)
		}

		if typeOut != ordIn.Type() {
			t.Errorf("Incorrect OrderType for retrieved order. Got %v, expected %v.",
				typeOut, ordIn.Type())
		}

		if filledOut != int64(ordIn.FilledAmt()) {
			t.Errorf("Incorrect FilledAmt for retrieved order. Got %v, expected %v.",
				filledOut, ordIn.FilledAmt())
		}
	}
}

func TestCancelOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: failed (archived)
	ordIn := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 0)
	statusIn := types.OrderStatusFailed

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	//oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	statusOut, typeOut, filledOut, err := archie.OrderStatus(ordIn)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}

	if typeOut != ordIn.Type() {
		t.Errorf("Incorrect OrderType for retrieved order. Got %v, expected %v.",
			typeOut, ordIn.Type())
	}

	if filledOut != -1 {
		t.Errorf("Incorrect FilledAmt for retrieved order. Got %v, expected %v.",
			filledOut, -1)
	}
}

func TestUpdateOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	orderStatuses := []struct {
		ord       order.Order
		status    types.OrderStatus
		newStatus types.OrderStatus
		newFilled uint64
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			types.OrderStatusBooked,   // active
			types.OrderStatusSwapping, // active
			0,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			types.OrderStatusSwapping, // active
			types.OrderStatusBooked,   // active
			0,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			types.OrderStatusExecuted, // archived
			types.OrderStatusBooked,   // active, should warn
			0,
		},
		{
			newMarketSellOrder(2, 0),
			types.OrderStatusMatched, // active
			types.OrderStatusBooked,  // active, invalid for market
			0,
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusFailed, // archived
			types.OrderStatusFailed, // archived, no change
			0,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusMatched,  // active
			types.OrderStatusExecuted, // archived
			2000000000,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			types.OrderStatusPending, // active
			types.OrderStatusMatched, // active
			0,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 0),
			types.OrderStatusPending, // active
			types.OrderStatusFailed,  // archived
			0,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		switch ot := ordIn.(type) {
		case *order.LimitOrder:
			ot.Filled = orderStatuses[i].newFilled
		case *order.MarketOrder:
			ot.Filled = orderStatuses[i].newFilled
		}

		newStatus := orderStatuses[i].newStatus
		err = archie.UpdateOrderStatus(ordIn, newStatus)
		if err != nil {
			t.Fatalf("UpdateOrderStatus(%d:%v, %s) failed: %v", i, ordIn, newStatus, err)
		}
	}

}

func TestUpdateOrderFilled(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("nukeAll: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	orderStatuses := []struct {
		ord           order.Order
		status        types.OrderStatus
		newFilled     uint64
		wantUpdateErr bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			types.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			types.OrderStatusSwapping, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			types.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newMarketSellOrder(2, 0),
			types.OrderStatusMatched, // active
			0,
			false,
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusFailed, // archived
			0,
			false,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusMatched, // active
			2000000000,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			types.OrderStatusPending, // active
			0,
			true, // cannot set filled amount for order type cancel
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		switch ot := ordIn.(type) {
		case *order.LimitOrder:
			ot.Filled = orderStatuses[i].newFilled
		case *order.MarketOrder:
			ot.Filled = orderStatuses[i].newFilled
		}

		err = archie.UpdateOrderFilled(ordIn)
		if (err != nil) != orderStatuses[i].wantUpdateErr {
			t.Fatalf("UpdateOrderFilled(%d:%v) failed: %v", i, ordIn, err)
		}
	}

}
