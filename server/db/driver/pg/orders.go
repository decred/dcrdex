// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/db"
	"github.com/decred/dcrdex/server/db/driver/pg/internal"
	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
	"github.com/lib/pq"
)

type Error string

func (err Error) Error() string { return string(err) }

const (
	ErrUnknownOrder = Error("unknown order")
)

// utxo implements order.Outpoint
type utxo struct {
	txHash []byte
	vout   uint32
}

func (u *utxo) TxHash() []byte { return u.txHash }
func (u *utxo) Vout() uint32   { return u.vout }

func newUtxo(txid string, vout uint32) *utxo {
	hash, err := hex.DecodeString(txid)
	if err != nil {
		panic(err)
	}
	return &utxo{hash, vout}
}

func newUtxoFromOutpoint(outpoint string) (*utxo, error) {
	outParts := strings.Split(outpoint, ":")
	if len(outParts) != 2 {
		return nil, fmt.Errorf("invalid outpoint %s", outpoint)
	}
	hash, err := hex.DecodeString(outParts[0])
	if err != nil {
		panic(err)
	}
	vout, err := strconv.ParseUint(outParts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid vout %s: %v", outParts[1], err)
	}
	return &utxo{hash, uint32(vout)}, nil
}

var _ db.OrderArchiver = (*Archiver)(nil)

// Order retrieves an order with the given OrderID, stored for the market
// specified by the given base and quote assets. A non-nil error will be
// returned if the market is not recognized. If the order is not found, the
// error value is ErrUnknownOrder, and the type is
// market/types.OrderStatusUnknown. The only recognized order types are market,
// limit, and cancel.
func (a *Archiver) Order(oid order.OrderID, base, quote uint32) (order.Order, types.OrderStatus, error) {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return nil, types.OrderStatusUnknown, err
	}

	// Since order type is unknown:
	// - try to load from orders table, which includes market and limit orders
	// - if found, coerce into the correct order type and return
	// - if not found, try loading a cancel order with this oid
	lo, status, err := loadLimitOrder(a.db, a.dbName, marketSchema, oid)
	if err == ErrUnknownOrder {
		// Try the cancel orders.
		var co *order.CancelOrder
		co, status, err = loadCancelOrder(a.db, a.dbName, marketSchema, oid)
		if err != nil {
			return nil, types.OrderStatusUnknown, err // includes ErrUnknownOrder
		}
		co.BaseAsset, co.QuoteAsset = base, quote
		return co, pgToMarketStatus(status), err
		// no other order types to try presently
	}
	if err != nil {
		return nil, types.OrderStatusUnknown, err
	}

	lo.BaseAsset, lo.QuoteAsset = base, quote

	// Since loadLimitOrder returns both market and limit orders in a
	// *LimitOrder, identify the real type via the OrderType field. Extract the
	// MarketOrder if necessary.
	switch lo.OrderType {
	case order.MarketOrderType:
		return &lo.MarketOrder, pgToMarketStatus(status), nil
	case order.LimitOrderType:
		return lo, pgToMarketStatus(status), nil
	}

	return nil, types.OrderStatusUnknown,
		fmt.Errorf("retrieved unsupported order type %d (%s)", lo.OrderType, lo.OrderType)
}

type pgOrderStatus uint16

const (
	orderStatusUnknown pgOrderStatus = iota
	orderStatusEpoch
	orderStatusBooked
	orderStatusExecuted
	orderStatusFailed // failed helps distinguish matched from unmatched executed cancel orders
	orderStatusCanceled
	orderStatusRevoked
)

func marketToPgStatus(status types.OrderStatus) pgOrderStatus {
	switch status {
	case types.OrderStatusEpoch:
		return orderStatusEpoch
	case types.OrderStatusBooked:
		return orderStatusBooked
	case types.OrderStatusExecuted:
		return orderStatusExecuted
	case types.OrderStatusCanceled:
		return orderStatusCanceled
	case types.OrderStatusRevoked:
		return orderStatusRevoked
	}
	return orderStatusUnknown
}

func pgToMarketStatus(status pgOrderStatus) types.OrderStatus {
	switch status {
	case orderStatusEpoch:
		return types.OrderStatusEpoch
	case orderStatusBooked:
		return types.OrderStatusBooked
	case orderStatusExecuted, orderStatusFailed: // failed is executed as far as the market is concerned
		return types.OrderStatusExecuted
	case orderStatusCanceled:
		return types.OrderStatusCanceled
	case orderStatusRevoked:
		return types.OrderStatusRevoked
	}
	return types.OrderStatusUnknown
}

func (status pgOrderStatus) String() string {
	switch status {
	case orderStatusFailed:
		return "failed"
	default:
		return pgToMarketStatus(status).String()
	}
}

func (status pgOrderStatus) active() bool {
	switch status {
	case orderStatusEpoch, orderStatusBooked:
		return true
	case orderStatusCanceled, orderStatusRevoked, orderStatusExecuted,
		orderStatusFailed, orderStatusUnknown:
		return false
	default:
		panic("unknown order status!") // programmer error
	}
}

// NewEpochOrder stores the given order with epoch status.
func (a *Archiver) NewEpochOrder(ord order.Order) error {
	return a.storeOrder(ord, orderStatusEpoch)
}

func (a *Archiver) insertOrUpdate(ord order.Order, status pgOrderStatus) error {
	_, _, _, err := a.orderStatus(ord)
	switch err {
	case nil:
		return a.updateOrderStatus(ord, status)
	case ErrUnknownOrder:
		return a.storeOrder(ord, status)
	}
	return err
}

// BookOrder updates or inserts the given LimitOrder with booked status.
func (a *Archiver) BookOrder(lo *order.LimitOrder) error {
	return a.insertOrUpdate(lo, orderStatusBooked)
}

// ExecuteOrder updates or inserts the given Order with executed status.
func (a *Archiver) ExecuteOrder(ord order.Order) error {
	return a.insertOrUpdate(ord, orderStatusExecuted)
}

// CancelOrder updates a LimitOrder with canceled status. If the order does not
// exit in the Archiver, CancelOrder returns ErrUnknownOrder. To store a new
// limit order with canceled status, use StoreOrder.
func (a *Archiver) CancelOrder(lo *order.LimitOrder) error {
	return a.updateOrderStatus(lo, orderStatusCanceled)
}

// RevokeOrder updates a LimitOrder with revoked status, which is used for
// DEX-revoked orders rather than orders matched with a user's CancelOrder. If
// the order does not exit in the Archiver, RevokeOrder returns ErrUnknownOrder.
// To store a new limit order with revoked status, use StoreOrder.
func (a *Archiver) RevokeOrder(lo *order.LimitOrder) error {
	return a.updateOrderStatus(lo, orderStatusRevoked)
}

// FailCancelOrder updates or inserts the given CancelOrder with failed status.
// To update a CancelOrder with executed status, use ExecuteOrder.
func (a *Archiver) FailCancelOrder(co *order.CancelOrder) error {
	return a.updateOrderStatus(co, orderStatusFailed)
}

func validateOrder(ord order.Order, status pgOrderStatus, mkt *types.MarketInfo) bool {
	if status == orderStatusFailed && ord.Type() != order.CancelOrderType {
		return false
	}
	return db.ValidateOrder(ord, pgToMarketStatus(status), mkt)
}

// StoreOrder stores an order with the provided status. The market is determined
// from the Order. A non-nil error will be returned if the market is not
// recognized. All orders are validated via server/db.ValidateOrder to ensure
// only sensible orders reach persistent storage. Updating orders should be done
// via one of the update functions such as UpdateOrderStatus.
func (a *Archiver) StoreOrder(ord order.Order, status types.OrderStatus) error {
	return a.storeOrder(ord, marketToPgStatus(status))
}

func (a *Archiver) storeOrder(ord order.Order, status pgOrderStatus) error {
	marketSchema, err := types.MarketName(ord.Base(), ord.Quote())
	if err != nil {
		return err
	}
	mkt, found := a.markets[marketSchema]
	if !found {
		return fmt.Errorf(`archiver does not support the market "%s" for order %v`,
			marketSchema, ord.UID())
	}

	if !validateOrder(ord, status, mkt) {
		return fmt.Errorf("invalid order %v for status %v and market %v",
			ord.UID(), status, mkt.Name)
	}

	// If enabled, search all tables for the order to ensure it is not already
	// stored somewhere.
	if a.checkedStores {
		var foundStatus pgOrderStatus
		switch ord.Type() {
		case order.MarketOrderType, order.LimitOrderType:
			foundStatus, _, _, err = orderStatus(a.db, ord.ID(), a.dbName, marketSchema)
		case order.CancelOrderType:
			foundStatus, err = cancelOrderStatus(a.db, ord.ID(), a.dbName, marketSchema)
		}

		switch err {
		case ErrUnknownOrder: // good
		case nil: // found, bad
			return fmt.Errorf("attempted to store a %s order while it exists "+
				"in another table as %s", pgToMarketStatus(status), pgToMarketStatus(foundStatus))
		default: // err != nil, bad
			return fmt.Errorf("findOrder failed: %v", err)
		}
	}

	var N int64
	switch ot := ord.(type) {
	case *order.CancelOrder:
		tableName := fullCancelOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeCancelOrder(a.db, tableName, ot, status)
		if err != nil {
			return fmt.Errorf("storeLimitOrder failed: %v", err)
		}
	case *order.MarketOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeMarketOrder(a.db, tableName, ot, status)
		if err != nil {
			return fmt.Errorf("storeLimitOrder failed: %v", err)
		}
	case *order.LimitOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeLimitOrder(a.db, tableName, ot, status)
		if err != nil {
			return fmt.Errorf("storeLimitOrder failed: %v", err)
		}
	default:
		panic("db.ValidateOrder should have caught this")
	}

	if N != 1 {
		return fmt.Errorf("failed to store order %v: %d rows affected, expected 1",
			ord.UID(), N)
	}

	return nil
}

// OrderStatusByID gets the status, ID, and filled amount of the order with the
// given OrderID in the market specified by a base and quote asset. See also
// OrderStatus. If the order is not found, the error value is ErrUnknownOrder,
// and the type is market/types.OrderStatusUnknown.
func (a *Archiver) OrderStatusByID(oid order.OrderID, base, quote uint32) (types.OrderStatus, order.OrderType, int64, error) {
	pgStatus, orderType, filled, err := a.orderStatusByID(oid, base, quote)
	return pgToMarketStatus(pgStatus), orderType, filled, err
}

func (a *Archiver) orderStatusByID(oid order.OrderID, base, quote uint32) (pgOrderStatus, order.OrderType, int64, error) {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return orderStatusUnknown, order.UnknownOrderType, -1, err
	}
	status, orderType, filled, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if err == ErrUnknownOrder {
		status, err = cancelOrderStatus(a.db, oid, a.dbName, marketSchema)
		if err != nil {
			return orderStatusUnknown, order.UnknownOrderType, -1, err // includes ErrUnknownOrder
		}
		filled = -1
		orderType = order.CancelOrderType
	}
	return status, orderType, filled, err
}

// OrderStatus gets the status, ID, and filled amount of the given order. See
// also OrderStatusByID.
func (a *Archiver) OrderStatus(ord order.Order) (types.OrderStatus, order.OrderType, int64, error) {
	return a.OrderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

func (a *Archiver) orderStatus(ord order.Order) (pgOrderStatus, order.OrderType, int64, error) {
	return a.orderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

// UpdateOrderStatusByID updates the status and filled amount of the order with
// the given OrderID in the market specified by a base and quote asset. For
// cancel orders, the filled amount is ignored. OrderStatusByID is used to
// locate the existing order. If the order is not found, the error value is
// ErrUnknownOrder, and the type is market/types.OrderStatusUnknown. See also
// UpdateOrderStatus.
func (a *Archiver) UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status types.OrderStatus, filled int64) error {
	return a.updateOrderStatusByID(oid, base, quote, marketToPgStatus(status), filled)
}

func (a *Archiver) updateOrderStatusByID(oid order.OrderID, base, quote uint32, status pgOrderStatus, filled int64) error {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err
	}

	initStatus, orderType, initFilled, err := a.orderStatusByID(oid, base, quote)
	if err != nil {
		return err
	}
	if initStatus == orderStatusUnknown {
		return ErrUnknownOrder
	}
	if initStatus == status && filled == initFilled {
		log.Debugf("Not updating order with no status or filled amount change.")
		return nil
	}
	if filled == -1 {
		filled = initFilled
	}

	tableChange := status.active() != initStatus.active()

	if !initStatus.active() {
		if tableChange {
			log.Warnf("Moving an order from an archived to active status: "+
				"Order %s (%s -> %s)", oid, initStatus, status)
		} else {
			log.Infof("Archived order is changing status: "+
				"Order %s (%s -> %s)", oid, initStatus, status)
		}
	}

	switch orderType {
	case order.LimitOrderType, order.MarketOrderType:
		srcTableName := fullOrderTableName(a.dbName, marketSchema, initStatus.active())
		if tableChange {
			dstTableName := fullOrderTableName(a.dbName, marketSchema, status.active())
			return a.moveOrder(oid, srcTableName, dstTableName, status, filled)
		}

		// No table move, just update the order.
		return updateOrderStatusAndFilledAmt(a.db, srcTableName, oid, status, uint64(filled))

	case order.CancelOrderType:
		srcTableName := fullCancelOrderTableName(a.dbName, marketSchema, initStatus.active())
		if tableChange {
			dstTableName := fullCancelOrderTableName(a.dbName, marketSchema, status.active())
			return a.moveCancelOrder(oid, srcTableName, dstTableName, status)
		}

		// No table move, just update the order.
		return updateCancelOrderStatus(a.db, srcTableName, oid, status)
	default:
		return fmt.Errorf("unsupported order type: %v", orderType)
	}
}

// UpdateOrderStatus updates the status and filled amount of the given order.
// Both the market and new filled amount are determined from the Order.
// OrderStatusByID is used to locate the existing order. See also
// UpdateOrderStatusByID.
func (a *Archiver) UpdateOrderStatus(ord order.Order, status types.OrderStatus) error {
	return a.updateOrderStatus(ord, marketToPgStatus(status))
}

func (a *Archiver) updateOrderStatus(ord order.Order, status pgOrderStatus) error {
	filled := int64(ord.FilledAmt())
	return a.updateOrderStatusByID(ord.ID(), ord.Base(), ord.Quote(), status, filled)
}

func (a *Archiver) moveOrder(oid order.OrderID, srcTableName, dstTableName string, status pgOrderStatus, filled int64) error {
	// Move the order, updating status and filled amount.
	moved, err := moveOrder(a.db, srcTableName, dstTableName, oid,
		status, uint64(filled))
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("order %s not moved from %s to %s", oid, srcTableName, dstTableName)
	}
	return nil
}

func (a *Archiver) moveCancelOrder(oid order.OrderID, srcTableName, dstTableName string, status pgOrderStatus) error {
	// Move the order, updating status and filled amount.
	moved, err := moveCancelOrder(a.db, srcTableName, dstTableName, oid,
		status)
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("cancel order %s not moved from %s to %s", oid, srcTableName, dstTableName)
	}
	return nil
}

// UpdateOrderFilledByID updates the filled amount of the order with the given
// OrderID in the market specified by a base and quote asset. This function
// applies only to market and limit orders, not cancel orders. OrderStatusByID
// is used to locate the existing order. If the order is not found, the error
// value is ErrUnknownOrder, and the type is market/types.OrderStatusUnknown.
// See also UpdateOrderFilled. To also update the order status, use
// UpdateOrderStatusByID or UpdateOrderStatus.
func (a *Archiver) UpdateOrderFilledByID(oid order.OrderID, base, quote uint32, filled int64) error {
	// Locate the order.
	status, orderType, initFilled, err := a.orderStatusByID(oid, base, quote)
	//status, orderType, initFilled, err := orderStatus(a.db, oid, a.dbName, marketSchema) // only checks market and limit orders
	if err != nil {
		return err
	}
	if status == orderStatusUnknown {
		return ErrUnknownOrder
	}

	switch orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}

	if filled == initFilled {
		return nil // nothing to do
	}

	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err // should be caught already by a.OrderStatusByID
	}
	tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
	err = updateOrderFilledAmt(a.db, tableName, oid, uint64(filled))
	return err
}

// UpdateOrderFilled updates the filled amount of the given order. Both the
// market and new filled amount are determined from the Order. OrderStatusByID
// is used to locate the existing order. This function applies only to market
// and limit orders, not cancel orders. See also UpdateOrderFilledByID.
func (a *Archiver) UpdateOrderFilled(ord order.Order) error {
	switch orderType := ord.Type(); orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}
	filled := int64(ord.FilledAmt())
	return a.UpdateOrderFilledByID(ord.ID(), ord.Base(), ord.Quote(), filled)
}

// UserOrders retrieves all orders for the given account in the market specified
// by a base and quote asset.
func (a *Archiver) UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []types.OrderStatus, error) {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return nil, nil, err
	}

	orders, pgStatuses, err := userOrders(ctx, a.db, a.dbName, marketSchema, aid)
	if err != nil {
		return nil, nil, err
	}
	statuses := make([]types.OrderStatus, len(pgStatuses))
	for i := range pgStatuses {
		statuses[i] = pgToMarketStatus(pgStatuses[i])
	}
	return orders, statuses, err
}

// BEGIN regular order functions

func orderStatus(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string) (pgOrderStatus, order.OrderType, int64, error) {
	// Search active orders first.
	fullTable := fullOrderTableName(dbName, marketSchema, true)
	found, status, orderType, filled, err := findOrder(dbe, oid, fullTable)
	if err != nil {
		return orderStatusUnknown, order.UnknownOrderType, -1, err
	}
	if found {
		return status, orderType, filled, nil
	}

	// Search archived orders.
	fullTable = fullOrderTableName(dbName, marketSchema, false)
	found, status, orderType, filled, err = findOrder(dbe, oid, fullTable)
	if err != nil {
		return orderStatusUnknown, order.UnknownOrderType, -1, err
	}
	if found {
		return status, orderType, filled, nil
	}

	// Order not found in either orders table.
	return orderStatusUnknown, order.UnknownOrderType, -1, ErrUnknownOrder
}

func findOrder(dbe *sql.DB, oid order.OrderID, fullTable string) (bool, pgOrderStatus, order.OrderType, int64, error) {
	stmt := fmt.Sprintf(internal.OrderStatus, fullTable)
	var status pgOrderStatus
	var filled int64
	var orderType order.OrderType
	err := dbe.QueryRow(stmt, oid).Scan(&orderType, &status, &filled)
	switch err {
	case sql.ErrNoRows:
		return false, orderStatusUnknown, order.UnknownOrderType, -1, nil
	case nil:
		return true, status, orderType, filled, nil
	default:
		return false, orderStatusUnknown, order.UnknownOrderType, -1, err
	}
}

func loadLimitOrder(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.LimitOrder, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullOrderTableName(dbName, marketSchema, false)
	lo, status, err := loadLimitOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		// try archived orders next
	case nil:
		// found
		return lo, status, nil
	default:
		// query error
		return lo, orderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullOrderTableName(dbName, marketSchema, true)
	lo, status, err = loadLimitOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		return nil, orderStatusUnknown, ErrUnknownOrder
	case nil:
		// found
		return lo, status, nil
	default:
		// query error
		return nil, orderStatusUnknown, err
	}
}

func loadLimitOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.LimitOrder, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var lo order.LimitOrder
	var id order.OrderID
	var utxos pq.StringArray
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &lo.OrderType, &lo.Sell,
		&lo.AccountID, &lo.Address, &lo.ClientTime, &lo.ServerTime, &utxos,
		&lo.Quantity, &lo.Rate, &lo.Force, &status, &lo.Filled)
	if err != nil {
		return nil, orderStatusUnknown, err
	}

	lo.UTXOs = make([]order.Outpoint, 0, len(utxos))
	for i := range utxos {
		utxo, err := newUtxoFromOutpoint(utxos[i])
		if err != nil {
			return nil, orderStatusUnknown, fmt.Errorf("bad utxo %s: %v", utxos[i], err)
		}
		lo.UTXOs = append(lo.UTXOs, utxo)
	}

	return &lo, status, nil
}

func userOrders(ctx context.Context, dbe *sql.DB, dbName, marketSchema string, aid account.AccountID) ([]order.Order, []pgOrderStatus, error) {
	// Active orders.
	fullTable := fullOrderTableName(dbName, marketSchema, false)
	orders, statuses, err := userOrdersFromTable(ctx, dbe, fullTable, aid)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}

	// Archived Orders.
	fullTable = fullOrderTableName(dbName, marketSchema, true)
	ordersArchived, statusesArchived, err := userOrdersFromTable(ctx, dbe, fullTable, aid)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}

	orders = append(orders, ordersArchived...)
	statuses = append(statuses, statusesArchived...)
	return orders, statuses, nil
}

func userOrdersFromTable(ctx context.Context, dbe *sql.DB, fullTable string, aid account.AccountID) ([]order.Order, []pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectUserOrders, fullTable)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, nil, err
	}

	var orders []order.Order
	var statuses []pgOrderStatus

	for rows.Next() {
		var lo order.LimitOrder
		var id order.OrderID
		var utxos pq.StringArray
		var status pgOrderStatus
		err = rows.Scan(&id, &lo.OrderType, &lo.Sell,
			&lo.AccountID, &lo.Address, &lo.ClientTime, &lo.ServerTime, &utxos,
			&lo.Quantity, &lo.Rate, &lo.Force, &status, &lo.Filled)
		if err != nil {
			return nil, nil, err
		}

		lo.UTXOs = make([]order.Outpoint, 0, len(utxos))
		for i := range utxos {
			utxo, err := newUtxoFromOutpoint(utxos[i])
			if err != nil {
				return nil, nil, fmt.Errorf("bad utxo %s: %v", utxos[i], err)
			}
			lo.UTXOs = append(lo.UTXOs, utxo)
		}

		orders = append(orders, &lo)
		statuses = append(statuses, status)
	}

	return orders, statuses, nil
}

func marshalUTXOs(UTXOs []order.Outpoint) (utxos []string) {
	// UTXOs are stored as an array of strings like ["txid0:vout0", ...] despite
	// this being less space efficient than a BYTEA because it significantly
	// simplifies debugging.
	for _, u := range UTXOs {
		utxos = append(utxos, order.OutpointString(u))
	}
	return
}

func storeLimitOrder(dbe sqlExecutor, tableName string, lo *order.LimitOrder, status pgOrderStatus) (int64, error) {
	utxos := marshalUTXOs(lo.UTXOs)
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, lo.ID(), lo.Type(), lo.Sell, lo.AccountID,
		lo.Address, lo.ClientTime, lo.ServerTime, pq.StringArray(utxos),
		lo.Quantity, lo.Rate, lo.Force, status, lo.Filled)
}

func storeMarketOrder(dbe sqlExecutor, tableName string, mo *order.MarketOrder, status pgOrderStatus) (int64, error) {
	utxos := marshalUTXOs(mo.UTXOs)
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, mo.ID(), mo.Type(), mo.Sell, mo.AccountID,
		mo.Address, mo.ClientTime, mo.ServerTime, pq.StringArray(utxos),
		mo.Quantity, 0, order.ImmediateTiF, status, mo.Filled)
}

func updateOrderStatus(dbe sqlExecutor, tableName string, oid order.OrderID, status pgOrderStatus) error {
	stmt := fmt.Sprintf(internal.UpdateOrderStatus, tableName)
	_, err := dbe.Exec(stmt, status, oid)
	return err
}

func updateOrderFilledAmt(dbe sqlExecutor, tableName string, oid order.OrderID, filled uint64) error {
	stmt := fmt.Sprintf(internal.UpdateOrderFilledAmt, tableName)
	_, err := dbe.Exec(stmt, filled, oid)
	return err
}

func updateOrderStatusAndFilledAmt(dbe sqlExecutor, tableName string, oid order.OrderID, status pgOrderStatus, filled uint64) error {
	stmt := fmt.Sprintf(internal.UpdateOrderStatusAndFilledAmt, tableName)
	_, err := dbe.Exec(stmt, status, filled, oid)
	return err
}

func moveOrder(dbe sqlExecutor, oldTableName, newTableName string, oid order.OrderID, newStatus pgOrderStatus, newFilled uint64) (bool, error) {
	stmt := fmt.Sprintf(internal.MoveOrder, oldTableName, newStatus, newFilled, newTableName)
	moved, err := sqlExec(dbe, stmt, oid)
	if err != nil {
		return false, err
	}
	if moved != 1 {
		panic(fmt.Sprintf("moved %d orders instead of 1", moved))
	}
	return true, nil
}

// END regular order functions

// BEGIN cancel order functions

func storeCancelOrder(dbe sqlExecutor, tableName string, co *order.CancelOrder, status pgOrderStatus) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertCancelOrder, tableName)
	return sqlExec(dbe, stmt, co.ID(), co.AccountID, co.ClientTime,
		co.ServerTime, co.TargetOrderID, status)
}

func loadCancelOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var co order.CancelOrder
	var id order.OrderID
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &co.AccountID, &co.ClientTime,
		&co.ServerTime, &co.TargetOrderID, &status)
	if err != nil {
		return nil, orderStatusUnknown, err
	}

	co.OrderType = order.CancelOrderType

	return &co, status, nil
}

func loadCancelOrder(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullCancelOrderTableName(dbName, marketSchema, true)
	co, status, err := loadCancelOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		// try archived orders next
	case nil:
		// found
		return co, status, nil
	default:
		// query error
		return co, orderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullCancelOrderTableName(dbName, marketSchema, false)
	co, status, err = loadCancelOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		return nil, orderStatusUnknown, ErrUnknownOrder
	case nil:
		// found
		return co, status, nil
	default:
		// query error
		return nil, orderStatusUnknown, err
	}
}

func cancelOrderStatus(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string) (pgOrderStatus, error) {
	// Search active orders first.
	found, status, err := findCancelOrder(dbe, oid, dbName, marketSchema, true)
	if err != nil {
		return orderStatusUnknown, err
	}
	if found {
		return status, nil
	}

	// Search archived orders.
	found, status, err = findCancelOrder(dbe, oid, dbName, marketSchema, false)
	if err != nil {
		return orderStatusUnknown, err
	}
	if found {
		return status, nil
	}

	// Order not found in either orders table.
	return orderStatusUnknown, ErrUnknownOrder
}

func findCancelOrder(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string, active bool) (bool, pgOrderStatus, error) {
	fullTable := fullCancelOrderTableName(dbName, marketSchema, active)
	stmt := fmt.Sprintf(internal.CancelOrderStatus, fullTable)
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&status)
	switch err {
	case sql.ErrNoRows:
		return false, orderStatusUnknown, nil
	case nil:
		return true, status, nil
	default:
		return false, orderStatusUnknown, err
	}
}

func updateCancelOrderStatus(dbe sqlExecutor, tableName string, oid order.OrderID, status pgOrderStatus) error {
	return updateOrderStatus(dbe, tableName, oid, status)
}

func moveCancelOrder(dbe sqlExecutor, oldTableName, newTableName string, oid order.OrderID, newStatus pgOrderStatus) (bool, error) {
	stmt := fmt.Sprintf(internal.MoveCancelOrder, oldTableName, newStatus, newTableName)
	moved, err := sqlExec(dbe, stmt, oid)
	if err != nil {
		return false, err
	}
	if moved != 1 {
		panic(fmt.Sprintf("moved %d cancel orders instead of 1", moved))
	}
	return true, nil
}

// END cancel order functions
