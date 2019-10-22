// +build pgonline

package pg

import (
	"context"
	"encoding/hex"
	"github.com/davecgh/go-spew/spew"
	"testing"

	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
)

func TestStoreOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
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
			name: "ok limit epoch (active)",
			args: args{
				ord:    newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: types.OrderStatusEpoch,
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
			name: "limit duplicate",
			args: args{
				ord:    newLimitOrder(false, 4800000, 1, order.StandingTiF, 0),
				status: types.OrderStatusExecuted,
			},
			wantErr: true,
		},
		{
			name: "limit bad quantity (lot size)",
			args: args{
				ord:    orderBadLotSize,
				status: types.OrderStatusEpoch,
			},
			wantErr: true,
		},
		{
			name: "limit bad trading pair",
			args: args{
				ord:    orderBadMarket,
				status: types.OrderStatusEpoch,
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
				status: types.OrderStatusEpoch,
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

func TestBookOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// BookOrder for new order
	// Store order (epoch) for new order
	// BookOrder for existing order

	// Standing limit == OK
	err := archie.BookOrder(newLimitOrder(false, 4800000, 1, order.StandingTiF, 0))
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}

	// Immediate limit == bad
	err = archie.BookOrder(newLimitOrder(false, 4800000, 1, order.ImmediateTiF, 0))
	if err == nil {
		t.Fatalf("BookOrder should have failed for immediate TiF limit order")
	}

	// Store standing limit order in epoch status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err = archie.StoreOrder(lo, types.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Book the same limit order.
	err = archie.BookOrder(lo)
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}
}

func TestExecuteOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// ExecuteOrder for new order
	// Store order (executed) for new order
	// ExecuteOrder for existing order

	// Standing limit == OK
	err := archie.ExecuteOrder(newLimitOrder(false, 4800000, 1, order.StandingTiF, 0))
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}

	// Store standing limit order in executed status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err = archie.StoreOrder(lo, types.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Execute the same limit order.
	err = archie.ExecuteOrder(lo)
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}
}

func TestCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Standing limit == OK
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := archie.BookOrder(lo)
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}

	// Execute the same limit order.
	err = archie.CancelOrder(lo)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}

	// Cancel an order not in the tables yet
	lo2 := newLimitOrder(true, 4600000, 1, order.StandingTiF, 0)
	err = archie.CancelOrder(lo2)
	if err != ErrUnknownOrder {
		t.Fatalf("CancelOrder should have failed for unknown order.")
	}
}

func TestLoadOrderUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
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
		t.Fatalf("cleanTables: %v", err)
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
		t.Fatalf("cleanTables: %v", err)
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
		t.Fatalf("cleanTables: %v", err)
	}

	// Market: sell, epoch (active)
	ordIn := newMarketSellOrder(1, 0)
	statusIn := types.OrderStatusEpoch

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
		t.Fatalf("cleanTables: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: epoch (active)
	ordIn := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)
	statusIn := types.OrderStatusEpoch

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
		t.Fatalf("cleanTables: %v", err)
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
			types.OrderStatusEpoch, // active
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusExecuted, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusEpoch, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			types.OrderStatusExecuted, // archived
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
		t.Fatalf("cleanTables: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: executed (archived)
	ordIn := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 0)
	statusIn := types.OrderStatusExecuted

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
		t.Fatalf("cleanTables: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	orderStatuses := []struct {
		ord       order.Order
		status    types.OrderStatus
		newStatus types.OrderStatus
		newFilled uint64
		wantErr   bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			types.OrderStatusEpoch,  // active
			types.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			types.OrderStatusBooked,   // active
			types.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			types.OrderStatusExecuted, // archived
			types.OrderStatusBooked,   // active, should err
			0,
			true,
		},
		{
			newMarketSellOrder(2, 0),
			types.OrderStatusEpoch,  // active
			types.OrderStatusBooked, // active, invalid for market
			0,
			false,
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusExecuted, // archived
			types.OrderStatusExecuted, // archived, no change
			0,
			false,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusEpoch,    // active
			types.OrderStatusExecuted, // archived
			2000000000,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			types.OrderStatusEpoch,    // active
			types.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 2),
			types.OrderStatusExecuted, // archived
			types.OrderStatusCanceled, // archived
			0,
			false,
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
		if (err != nil) != orderStatuses[i].wantErr {
			t.Fatalf("UpdateOrderStatus(%d:%v, %s) failed: %v", i, ordIn, newStatus, err)
		}
	}
}

func TestFailCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	co := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	err := archie.StoreOrder(co, types.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	err = archie.FailCancelOrder(co)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	_, status, err := loadCancelOrder(archie.db, archie.dbName, mktInfo.Name, co.ID())
	if err != nil {
		t.Errorf("loadCancelOrder failed: %v", err)
	}

	if status != orderStatusFailed {
		t.Errorf("cancel order should have been %s, got %s", orderStatusFailed, status)
	}
}

func TestUpdateOrderFilled(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

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
			types.OrderStatusBooked, // active
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
			types.OrderStatusEpoch, // active
			0,
			false,
		},
		{
			newMarketSellOrder(1, 0),
			types.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			types.OrderStatusEpoch, // active
			2000000000,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			types.OrderStatusEpoch, // active
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

func TestUserOrders(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	limitSell := newLimitOrder(true, 4900000, 1, order.StandingTiF, 0)
	limitBuy := newLimitOrder(false, 4100000, 1, order.StandingTiF, 0)
	marketSell := newMarketSellOrder(2, 0)
	marketBuy := newMarketBuyOrder(2000000000, 0)

	aid := limitSell.AccountID

	marketSellOtherGuy := newMarketSellOrder(2, 0)
	marketSellOtherGuy.AccountID = randomAccountID()
	marketSellOtherGuy.Address = "1MUz4VMYui5qY1mxUiG8BQ1Luv6tqkvaiL"

	orderStatuses := []struct {
		ord     order.Order
		status  types.OrderStatus
		wantErr bool
	}{
		{
			limitSell,
			types.OrderStatusBooked, // active
			false,
		},
		{
			limitBuy,
			types.OrderStatusCanceled, // archived
			false,
		},
		{
			marketSell,
			types.OrderStatusEpoch, // active
			false,
		},
		{
			marketBuy,
			types.OrderStatusExecuted, // archived
			false,
		},
		{
			marketSellOtherGuy,
			types.OrderStatusExecuted, // archived
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}
	}

	ordersOut, statusesOut, err := archie.UserOrders(context.Background(), aid, mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Error(err)
	}

	if len(ordersOut) != len(statusesOut) {
		t.Errorf("UserOrders returned %d orders, but %d order status. Should be equal.",
			len(ordersOut), len(statusesOut))
	}

	numOrdersForGuy0 := len(orderStatuses) - 1
	if len(ordersOut) != numOrdersForGuy0 {
		t.Errorf("incorrect number of orders for user %d retrieved. "+
			"got %d, expected %d", aid, len(ordersOut), numOrdersForGuy0)
	}
}
