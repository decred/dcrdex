// +build pgonline

package pg

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/davecgh/go-spew/spew"
)

const cancelThreshWindow = 100 // spec

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

	limitA := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	marketSellA := newMarketSellOrder(2, 1)
	marketSellB := newMarketSellOrder(2, 0)
	cancelA := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)

	// Order with the same commitment as limitA, but different order id.
	limitAx := new(order.LimitOrder)
	*limitAx = *limitA
	limitAx.SetTime(time.Now())

	var epochIdx, epochDur int64 = 13245678, 6000

	type args struct {
		ord    order.Order
		status order.OrderStatus
	}
	tests := []struct {
		name        string
		args        args
		wantErr     bool
		wantErrType error
	}{
		{
			name: "ok limit booked (active)",
			args: args{
				ord:    newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
				status: order.OrderStatusBooked,
			},
			wantErr: false,
		},
		{
			name: "ok limit epoch (active)",
			args: args{
				ord:    newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: order.OrderStatusEpoch,
			},
			wantErr: false,
		},
		{
			name: "ok limit canceled (archived)",
			args: args{
				ord:    newLimitOrder(false, 4700000, 1, order.StandingTiF, 0),
				status: order.OrderStatusCanceled,
			},
			wantErr: false,
		},
		{
			name: "ok limit executed (archived)",
			args: args{
				ord:    limitA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "limit duplicate",
			args: args{
				ord:    limitA,
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
		{
			name: "limit duplicate by commit only",
			args: args{
				ord:    limitAx,
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
		{
			name: "limit bad quantity (lot size)",
			args: args{
				ord:    orderBadLotSize,
				status: order.OrderStatusEpoch,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "limit bad trading pair",
			args: args{
				ord:    orderBadMarket,
				status: order.OrderStatusEpoch,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrUnsupportedMarket},
		},
		{
			name: "market sell - bad status (booked)",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusBooked,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "market sell - bad status (canceled)",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusCanceled,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "market sell - active",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusEpoch,
			},
			wantErr: false,
		},
		{
			name: "market sell - archived",
			args: args{
				ord:    marketSellA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "market sell - already in other table",
			args: args{
				ord:    marketSellA,
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
		{
			name: "market sell - duplicate archived order",
			args: args{
				ord:    marketSellB, // dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
		{
			name: "cancel order",
			args: args{
				ord:    cancelA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "cancel order - duplicate archived order",
			args: args{
				ord:    cancelA,
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := archie.StoreOrder(tt.args.ord, epochIdx, epochDur, tt.args.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("StoreOrder() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				t.Logf("%s: %v", tt.name, err)
				if !db.SameErrorTypes(err, tt.wantErrType) {
					t.Errorf("Wrong error. Got %v, expected %v", err, tt.wantErrType)
				}
			}
		})
	}
}

func TestBookOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Store order (epoch) for new order
	// BookOrder for existing order

	var epochIdx, epochDur int64 = 13245678, 6000

	// Store standing limit order in epoch status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusEpoch)
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

	// Store order (executed) for new order
	// ExecuteOrder for existing order

	var epochIdx, epochDur int64 = 13245678, 6000

	// Store standing limit order in executed status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusExecuted)
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
	var epochIdx, epochDur int64 = 13245678, 6000
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusBooked)
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
	if !db.IsErrOrderUnknown(err) {
		t.Fatalf("CancelOrder should have failed for unknown order.")
	}
}

func TestRevokeOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Standing limit == OK
	var epochIdx, epochDur int64 = 13245678, 6000
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Revoke the same limit order.
	cancelID, timeStamp, err := archie.RevokeOrder(lo)
	if err != nil {
		t.Fatalf("RevokeOrder failed: %v", err)
	}

	// Check for the server-generated cancel order.
	co, coStatus, err := archie.Order(cancelID, lo.BaseAsset, lo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate cancel order: %v", err)
	}
	if co.ID() != cancelID {
		t.Errorf("incorrect cancel ID retrieved")
	}
	coT, ok := co.(*order.CancelOrder)
	if !ok {
		t.Fatalf("not a cancel order")
	}
	if coT.ClientTime != timeStamp {
		t.Errorf("got ClientTime %v, expected %v", coT.ClientTime, timeStamp)
	}
	if coT.ServerTime != timeStamp {
		t.Errorf("got ServerTime %v, expected %v", coT.ServerTime, timeStamp)
	}
	if coStatus != order.OrderStatusRevoked {
		t.Errorf("got order status %v, expected %v", coStatus, order.OrderStatusRevoked)
	}
	if !coT.Commit.IsZero() {
		t.Errorf("generated cancel order did not have NULL/zero-value commitment")
	}

	// Market orders may be revoked too, while swap is in progress.
	// NOTE: executed -> revoked status change may be odd.
	mo := newMarketSellOrder(1, 0)
	err = archie.StoreOrder(mo, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	cancelID, timeStamp, err = archie.RevokeOrderUncounted(mo)
	if err != nil {
		t.Fatalf("RevokeOrder failed: %v", err)
	}

	co, coStatus, err = archie.Order(cancelID, mo.BaseAsset, mo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate cancel order: %v", err)
	}
	if co.ID() != cancelID {
		t.Errorf("incorrect cancel ID retrieved")
	}
	coT, ok = co.(*order.CancelOrder)
	if !ok {
		t.Fatalf("not a cancel order")
	}
	if coT.ClientTime != timeStamp {
		t.Errorf("got ClientTime %v, expected %v", coT.ClientTime, timeStamp)
	}
	if coT.ServerTime != timeStamp {
		t.Errorf("got ServerTime %v, expected %v", coT.ServerTime, timeStamp)
	}
	if coStatus != order.OrderStatusRevoked {
		t.Errorf("got order status %v, expected %v", coStatus, order.OrderStatusRevoked)
	}
	if !coT.Commit.IsZero() {
		t.Errorf("generated cancel order did not have NULL/zero-value commitment")
	}

	// Revoke an order not in the tables yet
	lo2 := newLimitOrder(true, 4600000, 1, order.StandingTiF, 0)
	_, _, err = archie.RevokeOrder(lo2)
	if !db.IsErrOrderUnknown(err) {
		t.Fatalf("RevokeOrder should have failed for unknown order.")
	}
}

func TestFlushBook(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Standing limit == OK as booked
	var epochIdx, epochDur int64 = 13245678, 6000
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// A not booked order.
	mo := newMarketSellOrder(1, 0)
	mo.AccountID = lo.AccountID
	err = archie.StoreOrder(mo, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	sellsRemoved, buysRemoved, err := archie.FlushBook(lo.BaseAsset, lo.QuoteAsset)
	if err != nil {
		t.Fatalf("FlushBook failed: %v", err)
	}
	if len(sellsRemoved) != 0 {
		t.Fatalf("flushed %d book sell orders, expected 0", len(sellsRemoved))
	}
	if len(buysRemoved) != 1 {
		t.Fatalf("flushed %d book buy orders, expected 1", len(buysRemoved))
	}
	if buysRemoved[0] != lo.ID() {
		t.Errorf("flushed sell order has ID %v, expected %v", buysRemoved[0], lo.ID())
	}

	// Check for new status of the order.
	loNow, loStatus, err := archie.Order(lo.ID(), lo.BaseAsset, lo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate order: %v", err)
	}
	if loNow.ID() != lo.ID() {
		t.Errorf("incorrect order ID retrieved")
	}
	_, ok := loNow.(*order.LimitOrder)
	if !ok {
		t.Fatalf("not a limit order")
	}
	if loStatus != order.OrderStatusRevoked {
		t.Errorf("got order status %v, expected %v", loStatus, order.OrderStatusRevoked)
	}

	ordersOut, _, err := archie.UserOrders(context.Background(), lo.User(), lo.BaseAsset, lo.QuoteAsset)
	if err != nil {
		t.Fatalf("UserOrders failed: %v", err)
	}

	wantNumOrders := 2 // market and limit
	if len(ordersOut) != wantNumOrders {
		t.Fatalf("got %d user orders, expected %d", len(ordersOut), wantNumOrders)
	}

	coids, targets, _, err := archie.ExecutedCancelsForUser(lo.User(), cancelThreshWindow)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	// ExecutedCancelsForUser should not find the (exempt) cancels created by
	// FlushBook.
	if len(coids) != 0 {
		t.Fatalf("got %d cancels, expected 0", len(coids))
	}
	if len(targets) != 0 {
		t.Fatalf("got %d cancel targets, expected 0", len(targets))
	}

	// Query for the revoke associated cancels without the exemption filter.
	cancelTableName := fullCancelOrderTableName(archie.dbName, mktInfo.Name, false)
	stmt := fmt.Sprintf(internal.SelectRevokeCancels, cancelTableName)
	rows, err := archie.db.QueryContext(context.Background(), stmt, lo.User(), orderStatusRevoked, cancelThreshWindow)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}

	var ords []cancelExecStamped
	for rows.Next() {
		var oid, target order.OrderID
		var revokeTime time.Time
		var epochIdx int64
		err = rows.Scan(&oid, &target, &revokeTime, &epochIdx)
		if err != nil {
			rows.Close()
			t.Fatalf("rows Scan failed")
		}

		if epochIdx != exemptEpochIdx {
			t.Errorf("got epoch index %d, expected %d", epochIdx, exemptEpochIdx)
		}

		ords = append(ords, cancelExecStamped{oid, target, encode.UnixMilli(revokeTime)})
	}

	if err = rows.Err(); err != nil {
		t.Fatalf("rows Scan failed")
	}

	if len(ords) != 1 {
		t.Fatalf("found %d cancels, wanted 1", len(ords))
	}

	if ords[0].target != lo.ID() {
		t.Fatalf("cancel order is targeting %v, expected %v", ords[0].target, lo.ID())
	}

	// Ensure market order is still there.
	moNow, moStatus, err := archie.Order(mo.ID(), mo.BaseAsset, mo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate order: %v", err)
	}
	if moNow.ID() != mo.ID() {
		t.Errorf("incorrect order ID retrieved")
	}
	_, ok = moNow.(*order.MarketOrder)
	if !ok {
		t.Fatalf("not a market order")
	}
	if moStatus != order.OrderStatusExecuted {
		t.Errorf("got order status %v, expected %v", loStatus, order.OrderStatusExecuted)
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
	if statusOut != order.OrderStatusUnknown {
		t.Errorf("status of non-existent order should be OrderStatusUnknown, got %s", statusOut)
	}
}

func TestStoreLoadLimitOrderActive(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// Limit: buy, standing, booked
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := order.OrderStatusBooked

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

	var epochIdx, epochDur int64 = 13245678, 6000

	// Limit: buy, standing, executed
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := order.OrderStatusExecuted

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

	var epochIdx, epochDur int64 = 13245678, 6000

	// Market: sell, epoch (active)
	ordIn := newMarketSellOrder(1, 0)
	statusIn := order.OrderStatusEpoch

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: epoch (active)
	ordIn := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)
	statusIn := order.OrderStatusEpoch

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

func TestOrderStatusUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ord := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0) // not stored
	_, _, _, err := archie.OrderStatus(ord)
	if err == nil {
		t.Fatalf("OrderStatus succeeded to find nonexistent order!")
	}
	if !db.SameErrorTypes(err, db.ArchiveError{Code: db.ErrUnknownOrder}) {
		if errA, ok := err.(db.ArchiveError); ok {
			t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %d", errA.Code)
		}
		t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %v", err)
	}
}

// Test ActiveOrderCoins, BookOrders, and EpochOrders.
func TestActiveOrderCoins(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	multiCoinLO := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	multiCoinLO.Coins = append(multiCoinLO.Coins, order.CoinID{0x22, 0x23})

	epochLO := newLimitOrder(true, 1, 1, order.StandingTiF, 0)
	epochCO := newCancelOrder(multiCoinLO.ID(), AssetDCR, AssetBTC, 0)

	orderStatuses := []struct {
		ord         order.Order
		status      order.OrderStatus
		activeCoins int
	}{
		{
			multiCoinLO,
			order.OrderStatusBooked, // active, buy, booked
			-1,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived, buy
			0,
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active, sell, epoch
			1,
		},
		{
			epochLO,
			order.OrderStatusEpoch, // active, buy, epoch
			1,
		},
		{
			epochCO,
			order.OrderStatusEpoch, // cancel, epoch
			0,
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived, sell
			0,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active, buy
			-1,
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived, buy
			0,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}
	}

	baseCoins, quoteCoins, err := archie.ActiveOrderCoins(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("ActiveOrderCoins failed: %v", err)
	}

	for _, os := range orderStatuses {
		var coins, wantCoins []order.CoinID
		switch os.activeCoins {
		case 0: // no active
		case 1: // active base coins (sell order)
			coins = baseCoins[os.ord.ID()]
			wantCoins = os.ord.Trade().Coins
		case -1: // active quote coins (buy order)
			coins = quoteCoins[os.ord.ID()]
			wantCoins = os.ord.Trade().Coins
		}

		if len(coins) != len(wantCoins) {
			t.Errorf("Order %v has %d coins, expected %d", os.ord.ID(),
				len(coins), len(wantCoins))
			continue
		}
		for i := range coins {
			if !bytes.Equal(coins[i], wantCoins[i]) {
				t.Errorf("Order %v coin %d mismatch:\n\tgot %v\n\texpected %v",
					os.ord.ID(), i, coins[i], wantCoins[i])
			}
		}
	}

	bookOrders, err := archie.BookOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("BookOrders failed: %v", err)
	}

	if len(bookOrders) != 1 {
		t.Fatalf("got %d book orders, expected 1", len(bookOrders))
	}

	// Verify the order ID of the loaded order is correct. This ensures the
	// order is being loaded with all the fields to provide and identical
	// serialization.
	if multiCoinLO.ID() != bookOrders[0].ID() {
		t.Errorf("loaded book order has an incorrect order ID. Got %v, expected %v",
			bookOrders[0].ID(), multiCoinLO.ID())
	}

	los, mos, cos, err := archie.epochOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("epochOrders failed: %v", err)
	}

	if len(los) != 1 || len(mos) != 2 || len(cos) != 1 {
		t.Fatalf("got %d epoch limit orders, %d epoch market orders, and %d epoch cancel orders, expected 1, 2, and 1",
			len(los), len(mos), len(cos))
	}

	// Verify the order ID of the loaded order is correct. This ensures the
	// order is being loaded with all the fields to provide and identical
	// serialization.
	if epochLO.ID() != los[0].ID() {
		t.Errorf("epoch limit order has an incorrect order ID. Got %v, expected %v",
			los[0].ID(), epochLO.ID())
	}
	if epochCO.ID() != cos[0].ID() {
		t.Errorf("epoch cancel order has an incorrect order ID. Got %v, expected %v",
			cos[0].ID(), epochCO.ID())
	}

	// The exported version should return the same orders.
	orders, err := archie.EpochOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("EpochOrders failed: %v", err)
	}

	if len(orders) != 4 {
		t.Fatalf("got %d epoch orders, expected 4", len(orders))
	}
	for _, o := range orders {
		if o.ID() == los[0].ID() ||
			o.ID() == mos[0].ID() ||
			o.ID() == mos[1].ID() ||
			o.ID() == cos[0].ID() {
			continue
		}
		t.Fatalf("order %v in EpochOrders but not epochOrders", o.ID())
	}
}

func TestOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderStatuses := []struct {
		ord    order.Order
		status order.OrderStatus
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		trade := ordIn.Trade()
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

		if filledOut != int64(trade.Filled()) {
			t.Errorf("Incorrect FillAmt for retrieved order. Got %v, expected %v.",
				filledOut, trade.Filled())
		}
	}
}

func TestCancelOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: executed (archived)
	ordIn := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 0)
	statusIn := order.OrderStatusExecuted

	//oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

func TestUpdateOrderUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ord := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0) // not stored

	err := archie.UpdateOrderStatus(ord, order.OrderStatusExecuted)
	if err == nil {
		t.Fatalf("UpdateOrder succeeded to update nonexistent order!")
	}
	if !db.SameErrorTypes(err, db.ArchiveError{Code: db.ErrUnknownOrder}) {
		if errA, ok := err.(db.ArchiveError); ok {
			t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %d", errA.Code)
		}
		t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %v", err)
	}
}

func TestUpdateOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	orderStatuses := []struct {
		ord       order.Order
		status    order.OrderStatus
		newStatus order.OrderStatus
		newFilled uint64
		wantErr   bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusEpoch,  // active
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked,   // active
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
			order.OrderStatusBooked,   // active, should err
			0,
			true,
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch,  // active
			order.OrderStatusBooked, // active, invalid for market
			0,
			false,
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
			order.OrderStatusExecuted, // archived, no change
			0,
			false,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch,    // active
			order.OrderStatusExecuted, // archived
			2000000000,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			order.OrderStatusEpoch,    // active
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 2),
			order.OrderStatusExecuted, // archived
			order.OrderStatusCanceled, // archived
			0,
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		switch ot := ordIn.(type) {
		case *order.LimitOrder:
			ot.FillAmt = orderStatuses[i].newFilled
		case *order.MarketOrder:
			ot.FillAmt = orderStatuses[i].newFilled
		}

		newStatus := orderStatuses[i].newStatus
		err = archie.UpdateOrderStatus(ordIn, newStatus)
		if (err != nil) != orderStatuses[i].wantErr {
			t.Fatalf("UpdateOrderStatus(%d:%v, %s) failed: %v", i, ordIn, newStatus, err)
		}
	}
}

func TestStorePreimage(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	lo, pi := newLimitOrderRevealed(false, 4900000, 1, order.StandingTiF, 0)
	err := archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	err = archie.StorePreimage(lo, pi)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	piOut, err := archie.OrderPreimage(lo)
	if err != nil {
		t.Fatalf("OrderPreimage failed: %v", err)
	}

	if pi != piOut {
		t.Errorf("got preimage %v, expected %v", piOut, pi)
	}

	// Now test OrderPreimage when preimage is NULL.
	lo2, _ := newLimitOrderRevealed(false, 4900000, 1, order.StandingTiF, 0)
	err = archie.StoreOrder(lo2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	piOut2, err := archie.OrderPreimage(lo2)
	if err != nil {
		t.Fatalf("OrderPreimage failed: %v", err)
	}
	if !piOut2.IsZero() {
		t.Errorf("Preimage should have been the zero value, got %v", piOut2)
	}
}

func TestFailCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	co := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	err := archie.StoreOrder(co, epochIdx, epochDur, order.OrderStatusEpoch)
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

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	orderStatuses := []struct {
		ord           *order.LimitOrder
		status        order.OrderStatus
		newFilled     uint64
		wantUpdateErr bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		ordIn.FillAmt = orderStatuses[i].newFilled

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

	var epochIdx, epochDur int64 = 13245678, 6000

	limitSell := newLimitOrder(true, 4900000, 1, order.StandingTiF, 0)
	limitBuy := newLimitOrder(false, 4100000, 1, order.StandingTiF, 0)
	marketSell := newMarketSellOrder(2, 0)
	marketBuy := newMarketBuyOrder(2000000000, 0)

	// Make all of the above orders belong to the same user.
	aid := limitSell.AccountID
	limitBuy.AccountID = aid
	limitBuy.AccountID = aid
	marketSell.AccountID = aid
	marketBuy.AccountID = aid

	marketSellOtherGuy := newMarketSellOrder(2, 0)
	marketSellOtherGuy.Address = "1MUz4VMYui5qY1mxUiG8BQ1Luv6tqkvaiL"

	orderStatuses := []struct {
		ord     order.Order
		status  order.OrderStatus
		ordType order.OrderType
		wantErr bool
	}{
		{
			limitSell,
			order.OrderStatusBooked, // active
			order.LimitOrderType,
			false,
		},
		{
			limitBuy,
			order.OrderStatusCanceled, // archived
			order.LimitOrderType,
			false,
		},
		{
			marketSell,
			order.OrderStatusEpoch, // active
			order.MarketOrderType,
			false,
		},
		{
			marketBuy,
			order.OrderStatusExecuted, // archived
			order.MarketOrderType,
			false,
		},
		{
			marketSellOtherGuy,
			order.OrderStatusExecuted, // archived
			order.MarketOrderType,
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
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

	findExpected := func(ord order.Order) int {
		for i := range orderStatuses {
			if orderStatuses[i].ord.ID() == ord.ID() {
				return i
			}
		}
		return -1
	}

	for i := range ordersOut {
		j := findExpected(ordersOut[i])
		if j == -1 {
			t.Errorf("failed to find order %v", ordersOut[i])
			continue
		}
		if ordersOut[i].Type() != orderStatuses[j].ordType {
			t.Errorf("wrong type %v, wanted %v", ordersOut[i].Type(), orderStatuses[j].ordType)
		}
		if statusesOut[i] != orderStatuses[j].status {
			t.Errorf("wrong status %v, wanted %v", statusesOut[i], orderStatuses[j].status)
		}
	}
}
func TestUserOrderStatuses(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderStatuses := []struct {
		ord    order.Order
		status order.OrderStatus
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived
		},
	}

	unsavedOrder1 := newMarketBuyOrder(3000000000, 0)
	unsavedOrder2 := newMarketSellOrder(4, 0)

	orders := make([]order.Order, 0, len(orderStatuses)+2)
	orderIDs := make([]order.OrderID, 0, len(orderStatuses)+2)

	// Add unsaved orders
	orders = append(orders, unsavedOrder1, unsavedOrder2)
	orderIDs = append(orderIDs, unsavedOrder1.ID(), unsavedOrder2.ID())

	accountID := randomAccountID()
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		if lo, ok := ordIn.(*order.LimitOrder); ok {
			lo.BaseAsset, lo.QuoteAsset = AssetBTC, AssetLTC // swap the assets to test across different mkts
		}
		ordIn.Prefix().AccountID = accountID
		err := archie.StoreOrder(ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}
		orders = append(orders, ordIn)
		orderIDs = append(orderIDs, ordIn.ID())
	}

	// All orders except the 2 limit orders are DCR-BTC.
	orderStatusesOut, err := archie.UserOrderStatuses(accountID, AssetDCR, AssetBTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != len(orderStatuses)-2 /*the 2 limits*/ {
		t.Fatalf("OrderStatuses returned %d orders instead of %d", len(orderStatusesOut), len(orderStatuses)-2)
	}
	outMap := make(map[order.OrderID]*db.OrderStatus, len(orderStatusesOut))
	for _, orderStatus := range orderStatusesOut {
		outMap[orderStatus.ID] = orderStatus
	}
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		orderOut, found := outMap[ordIn.ID()]
		if !found {
			continue
		}
		statusIn := orderStatuses[i].status
		if orderOut.Status != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				orderOut.Status, statusIn)
		}
	}

	// Check statuses for the 2 limit orders that are BTC-LTC.
	orderStatusesOut, err = archie.UserOrderStatuses(accountID, AssetBTC, AssetLTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != 2 /*the 2 limits*/ {
		t.Fatalf("OrderStatuses returned %d orders instead of %d", len(orderStatusesOut), 2)
	}
	outMap = make(map[order.OrderID]*db.OrderStatus, len(orderStatusesOut))
	for _, orderStatus := range orderStatusesOut {
		outMap[orderStatus.ID] = orderStatus
	}
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		orderOut, found := outMap[ordIn.ID()]
		if !found {
			continue
		}
		statusIn := orderStatuses[i].status
		if orderOut.Status != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				orderOut.Status, statusIn)
		}
	}

	// Expect nothing for wrong user ID.
	orderStatusesOut, err = archie.UserOrderStatuses(randomAccountID(), AssetDCR, AssetBTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != 0 {
		t.Fatalf("OrderStatuses returned %d orders for wrong account ID", len(orderStatusesOut))
	}
}
func TestActiveUserOrderStatuses(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Two orders, different accounts, DCR-BTC.
	maker := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	taker := newLimitOrder(true, 4490000, 1, order.StandingTiF, 10)

	var epochIdx, epochDur int64 = 13245678, 6000
	err := archie.StoreOrder(maker, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = archie.StoreOrder(taker, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Second order from the same maker account.
	maker2 := newLimitOrder(false, 4500000, 1, order.ImmediateTiF, 20)
	maker2.AccountID = maker.AccountID

	// Store it.
	err = archie.StoreOrder(maker2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Same taker account, different market (BTC-LTC).
	taker2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	taker2.BaseAsset = AssetBTC
	taker2.QuoteAsset = AssetLTC
	taker2.AccountID = taker.AccountID

	// Store it.
	err = archie.StoreOrder(taker2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Store cancel order for taker account.
	taker2Incomplete := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	taker2Incomplete.AccountID = taker.AccountID
	err = archie.StoreOrder(taker2Incomplete, epochIdx, epochDur, order.OrderStatusCanceled)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Maker should have 2 active orders in 1 market.
	// Taker should have 2 active orders in 2 markets and 1 inactive (canceled) order.

	tests := []struct {
		name              string
		acctID            account.AccountID
		numExpected       int
		wantOrderIDs      []order.OrderID
		wantOrderStatuses []order.OrderStatus
		wantedErr         error
	}{
		{
			"ok maker",
			maker.User(),
			2,
			[]order.OrderID{maker.ID(), maker2.ID()},
			[]order.OrderStatus{order.OrderStatusBooked, order.OrderStatusEpoch},
			nil,
		},
		{
			"ok taker",
			taker.User(),
			2,
			[]order.OrderID{taker.ID(), taker2.ID()},
			[]order.OrderStatus{order.OrderStatusBooked, order.OrderStatusEpoch},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
			nil,
			nil,
		},
	}

	idInSlice := func(oid order.OrderID, oids []order.OrderID) int {
		for i := range oids {
			if oids[i] == oid {
				return i
			}
		}
		return -1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderStatuses, err := archie.ActiveUserOrderStatuses(tt.acctID)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(orderStatuses) != tt.numExpected {
				t.Errorf("Retrieved %d active orders for user %v, expected %d.", len(orderStatuses), tt.acctID, tt.numExpected)
			}
			for _, ord := range orderStatuses {
				wantId := idInSlice(ord.ID, tt.wantOrderIDs)
				if wantId == -1 {
					t.Errorf("Unexpected order ID %v retrieved.", ord.ID)
					continue
				}
				if ord.Status != tt.wantOrderStatuses[wantId] {
					t.Errorf("Incorrect order status for order %v. Got %d, want %d.",
						ord.ID, ord.Status, tt.wantOrderStatuses[wantId])
				}
			}
		})
	}
}

func TestCompletedUserOrders(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	nowMs := func() int64 {
		return encode.UnixMilli(time.Now().Truncate(time.Millisecond))
	}

	// Two orders, different accounts, DCR-BTC.
	maker := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	taker := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	var epochIdx, epochDur int64 = 13245678, 6000
	err := archie.StoreOrder(maker, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = archie.StoreOrder(taker, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set the orders' swap completion times.
	tSwapDoneMaker := nowMs()
	if err = archie.SetOrderCompleteTime(maker, tSwapDoneMaker); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	tSwapDoneTaker := tSwapDoneMaker + 10
	if err = archie.SetOrderCompleteTime(taker, tSwapDoneTaker); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Second order from the same maker account.
	maker2 := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	maker2.AccountID = maker.AccountID

	// Store it.
	err = archie.StoreOrder(maker2, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	// Set swap complete time.
	tSwapDoneMaker2 := nowMs()
	if err = archie.SetOrderCompleteTime(maker2, tSwapDoneMaker2); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Same taker account, different market (BTC-LTC).
	taker2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	taker2.BaseAsset = AssetBTC
	taker2.QuoteAsset = AssetLTC
	taker2.AccountID = taker.AccountID

	// Store it.
	err = archie.StoreOrder(taker2, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set swap complete time.
	tSwapDoneTaker2 := nowMs()
	if err = archie.SetOrderCompleteTime(taker2, tSwapDoneTaker2); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Order without completion time set.
	taker2Incomplete := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	taker2Incomplete.AccountID = taker.AccountID
	err = archie.StoreOrder(taker2Incomplete, epochIdx, epochDur, order.OrderStatusCanceled) // archived, but not complete
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	// NO SetOrderCompleteTime, BUT in an orders_archived table.

	// Try and fail to set completion time for an order not in executed status.
	taker3 := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	err = archie.StoreOrder(taker3, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set swap complete time.
	tSwapDoneTaker3 := nowMs()
	if err = archie.SetOrderCompleteTime(taker3, tSwapDoneTaker3); !db.IsErrOrderNotExecuted(err) {
		t.Fatalf("SetOrderCompleteTime should have returned a ErrOrderNotExecuted error for booked (not executed) order")
	}

	// Maker should have 2 completed orders in 1 market.
	// Taker should have 2 completed orders in 2 markets.

	tests := []struct {
		name          string
		acctID        account.AccountID
		numExpected   int
		wantOrderIDs  []order.OrderID
		wantCompTimes []int64
		wantedErr     error
	}{
		{
			"ok maker",
			maker.User(),
			2,
			[]order.OrderID{maker.ID(), maker2.ID()},
			[]int64{tSwapDoneMaker, tSwapDoneMaker2},
			nil,
		},
		{
			"ok taker",
			taker.User(),
			2,
			[]order.OrderID{taker.ID(), taker2.ID()},
			[]int64{tSwapDoneTaker, tSwapDoneTaker2},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
			nil,
			nil,
		},
	}

	idInSlice := func(mid order.OrderID, mids []order.OrderID) int {
		for i := range mids {
			if mids[i] == mid {
				return i
			}
		}
		return -1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oids, compTimes, err := archie.CompletedUserOrders(tt.acctID, cancelThreshWindow)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(oids) != tt.numExpected {
				t.Errorf("Retrieved %d completed orders for user %v, expected %d.", len(oids), tt.acctID, tt.numExpected)
			}
			for i := range oids {
				loc := idInSlice(oids[i], tt.wantOrderIDs)
				if loc == -1 {
					t.Errorf("Unexpected order ID %v retrieved.", oids[i])
					continue
				}
				if compTimes[i] != tt.wantCompTimes[loc] {
					t.Errorf("Incorrect order completion time. Got %d, want %d.",
						compTimes[loc], tt.wantCompTimes[i])
				}
			}
		})
	}
}

func TestExecutedCancelsForUser(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	co := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	err := archie.StoreOrder(co, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Mark the cancel order executed.
	err = archie.ExecuteOrder(co)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	_, status, err := loadCancelOrder(archie.db, archie.dbName, mktInfo.Name, co.ID())
	if err != nil {
		t.Errorf("loadCancelOrder failed: %v", err)
	}
	if status != orderStatusExecuted {
		t.Fatalf("cancel order should have been %s, got %s", orderStatusFailed, status)
	}

	// order ID for a revoked order
	lo := newLimitOrder(true, 4900000, 1, order.StandingTiF, 0)
	lo.AccountID = co.AccountID // same user
	lo.BaseAsset, lo.QuoteAsset = mktInfo.Base, mktInfo.Quote
	err = archie.StoreOrder(lo, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Revoke the order.
	time.Sleep(time.Millisecond * 10) // ensure the resulting cancel order is newer than the other cancel order above.
	coID, coTime, err := archie.RevokeOrder(lo)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	coOut, coStatusOut, err := loadCancelOrder(archie.db, archie.dbName, mktInfo.Name, coID)
	// loadCancelOrder does not set base and quote
	coOut.BaseAsset, coOut.QuoteAsset = mktInfo.Base, mktInfo.Quote
	if err != nil {
		t.Errorf("loadCancelOrder failed: %v", err)
	}
	if coStatusOut != orderStatusRevoked {
		t.Fatalf("cancel order should have been %s, got %s", orderStatusRevoked, status)
	}
	if coOut.ID() != coID {
		t.Errorf("incorrect cancel order ID. got %v, expected %v", coOut.ID(), coID)
	}
	if coOut.Time() != encode.UnixMilli(coTime) {
		t.Errorf("incorrect cancel time. got %x, expected %x", coOut.Time(), encode.UnixMilli(coTime))
	}

	// Store the epoch.
	matchTime := encode.UnixMilli(time.Now().Truncate(time.Millisecond))
	err = archie.InsertEpoch(&db.EpochResults{
		MktBase:        mktInfo.Base,
		MktQuote:       mktInfo.Quote,
		Idx:            epochIdx,
		Dur:            epochDur,
		MatchTime:      matchTime,
		OrdersRevealed: []order.OrderID{co.ID()}, // not needed, but would be the case if it were executed in this epoch
	})
	if err != nil {
		t.Errorf("InsertEpoch failed: %v", err)
	}

	// A revoked order (exempt cancel), which should NOT be found with
	// ExecutedCancelsForUser.
	lo2 := newLimitOrder(true, 4900000, 1, order.StandingTiF, 1)
	lo2.AccountID = co.AccountID // same user
	lo2.BaseAsset, lo2.QuoteAsset = mktInfo.Base, mktInfo.Quote
	err = archie.StoreOrder(lo2, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Revoke the order.
	time.Sleep(time.Millisecond * 10)
	_, _, err = archie.RevokeOrderUncounted(lo2)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	user := co.User()
	oids, targets, compTimes, err := archie.ExecutedCancelsForUser(user, cancelThreshWindow)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	if len(oids) != 2 {
		t.Fatalf("found %d orders, expected 1", len(oids))
	}
	if oids[0] != co.ID() {
		t.Errorf("incorrect executed cancel %v, expected %v", oids[0], co.ID())
	}
	if targets[0] != targetOrderID {
		t.Errorf("incorrect target for executed cancel %v, expected %v", targets[0], targetOrderID)
	}
	if compTimes[0] != matchTime {
		t.Errorf("incorrect exec time for executed cancel %v, expected %v", compTimes[0], matchTime)
	}
	if oids[1] != coID {
		t.Errorf("incorrect executed cancel %v, expected %v", oids[1], coID)
	}
	if targets[1] != lo.ID() {
		t.Errorf("incorrect target for executed cancel %v, expected %v", targets[1], lo.ID())
	}
	if compTimes[1] != encode.UnixMilli(coTime) {
		t.Errorf("incorrect exec time for executed cancel %v, expected %v", compTimes[1], encode.UnixMilli(coTime))
	}

	// test the limit
	oids, targets, compTimes, err = archie.ExecutedCancelsForUser(user, 0)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	if len(oids) > 0 || len(targets) > 0 || len(compTimes) > 0 {
		t.Errorf("found executed orders for user")
	}

	// Cancel order in epoch status, and with no epochs table entry.
	co2 := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	co2.AccountID = randomAccountID() // different user
	epochIdx++
	err = archie.StoreOrder(co2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = archie.FailCancelOrder(co2)
	if err != nil {
		t.Fatalf("FailCancelOrder failed: %v", err)
	}

	user2 := co2.User()
	oids, targets, compTimes, err = archie.ExecutedCancelsForUser(user2, cancelThreshWindow)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	if len(oids) > 0 || len(targets) > 0 || len(compTimes) > 0 {
		t.Errorf("found executed orders for user")
	}

	// Cancel order in failed status, with an epochs table entry.
	co3 := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	co3.AccountID = randomAccountID() // different user
	epochIdx++
	err = archie.StoreOrder(co3, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = archie.FailCancelOrder(co3)
	if err != nil {
		t.Fatalf("ExecuteOrder failed: %v", err)
	}

	// Store the epoch.
	matchTime3 := encode.UnixMilli(time.Now().Truncate(time.Millisecond))
	err = archie.InsertEpoch(&db.EpochResults{
		MktBase:        mktInfo.Base,
		MktQuote:       mktInfo.Quote,
		Idx:            epochIdx,
		Dur:            epochDur,
		MatchTime:      matchTime3,
		OrdersRevealed: []order.OrderID{co3.ID()}, // not needed, but would be the case if it were executed in this epoch
	})
	if err != nil {
		t.Errorf("InsertEpoch failed: %v", err)
	}

	user3 := co3.User()
	oids, targets, compTimes, err = archie.ExecutedCancelsForUser(user3, cancelThreshWindow)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	if len(oids) > 0 || len(targets) > 0 || len(compTimes) > 0 {
		t.Errorf("found executed orders for user")
	}

	// Cancel order in executed status, but with no epochs table entry.
	co4 := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	co4.AccountID = randomAccountID() // different user
	epochIdx++
	err = archie.StoreOrder(co4, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = archie.ExecuteOrder(co4)
	if err != nil {
		t.Fatalf("ExecuteOrder failed: %v", err)
	}

	user4 := co4.User()
	oids, targets, compTimes, err = archie.ExecutedCancelsForUser(user4, cancelThreshWindow)
	if err != nil {
		t.Errorf("ExecutedCancelsForUser failed: %v", err)
	}
	if len(oids) > 0 || len(targets) > 0 || len(compTimes) > 0 {
		t.Errorf("found executed orders for user")
	}
}
