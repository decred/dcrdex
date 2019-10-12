// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
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

// utxo implements order.UTXO
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
		return co, status, err
		// no other order types to try presently
	}

	if err != nil {
		return nil, types.OrderStatusUnknown, fmt.Errorf("loadLimitOrder failed: %v", err)
	}

	lo.BaseAsset, lo.QuoteAsset = base, quote

	switch lo.OrderType {
	case order.MarketOrderType:
		return &lo.MarketOrder, status, nil
	case order.LimitOrderType:
		return lo, status, nil
	}

	return nil, types.OrderStatusUnknown,
		fmt.Errorf("retrieved unsupported order type %d (%s)", lo.OrderType, lo.OrderType)
}

func (a *Archiver) StoreOrder(ord order.Order, status types.OrderStatus) error {
	marketSchema, err := types.MarketName(ord.Base(), ord.Quote())
	if err != nil {
		return err
	}

	mkt, found := a.markets[marketSchema]
	if !found {
		return fmt.Errorf(`archiver does not support the market "%s" for order %v`,
			marketSchema, ord.UID())
	}
	if !db.ValidateOrder(ord, status, mkt) {
		return fmt.Errorf("invalid order %v for status %v and market %v",
			ord.UID(), status, mkt.Name)
	}

	if a.checkedStores {
		var foundStatus types.OrderStatus
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
				"in another table as %s", status, foundStatus)
		default: // err != nil, bad
			return fmt.Errorf("findOrder failed: %v", err)
		}
	}

	var N int64
	switch ot := ord.(type) {
	case *order.CancelOrder:
		tableName := fullCancelOrderTableName(a.dbName, marketSchema, status.Active())
		N, err = storeCancelOrder(a.db, tableName, ot, status)
		if err != nil {
			return fmt.Errorf("storeLimitOrder failed: %v", err)
		}
	case *order.MarketOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
		N, err = storeMarketOrder(a.db, tableName, ot, status)
		if err != nil {
			return fmt.Errorf("storeLimitOrder failed: %v", err)
		}
	case *order.LimitOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
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

func (a *Archiver) OrderStatusByID(oid order.OrderID, base, quote uint32) (types.OrderStatus, order.OrderType, int64, error) {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return types.OrderStatusUnknown, order.UnknownOrderType, -1, err
	}
	status, orderType, filled, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if err == ErrUnknownOrder {
		status, err = cancelOrderStatus(a.db, oid, a.dbName, marketSchema)
		if err != nil {
			return types.OrderStatusUnknown, order.UnknownOrderType, -1, err // includes ErrUnknownOrder
		}
		filled = -1
		orderType = order.CancelOrderType
	}
	return status, orderType, filled, err
}

func (a *Archiver) OrderStatus(ord order.Order) (types.OrderStatus, order.OrderType, int64, error) {
	return a.OrderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

func (a *Archiver) UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status types.OrderStatus, filled int64) error {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err
	}

	initStatus, orderType, initFilled, err := a.OrderStatusByID(oid, base, quote)
	if err != nil {
		return err
	}
	if initStatus == types.OrderStatusUnknown {
		return fmt.Errorf("unknown order")
	}
	if initStatus == status && filled == initFilled {
		log.Debugf("Not updating order with no status or filled amount change.")
		return nil
	}
	if filled == -1 {
		filled = initFilled
	}

	tableChange := status.Active() != initStatus.Active()

	if initStatus.Archived() {
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
		srcTableName := fullOrderTableName(a.dbName, marketSchema, initStatus.Active())
		if tableChange {
			dstTableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
			return a.moveOrder(oid, srcTableName, dstTableName, status, filled)
		}

		// No table move, just update the order.
		return updateOrderStatusAndFilledAmt(a.db, srcTableName, oid, status, uint64(filled))

	case order.CancelOrderType:
		srcTableName := fullCancelOrderTableName(a.dbName, marketSchema, initStatus.Active())
		if tableChange {
			dstTableName := fullCancelOrderTableName(a.dbName, marketSchema, status.Active())
			return a.moveCancelOrder(oid, srcTableName, dstTableName, status)
		}

		// No table move, just update the order.
		return updateCancelOrderStatus(a.db, srcTableName, oid, status)
	default:
		return fmt.Errorf("unsupported order type: %v", orderType)
	}
}

func (a *Archiver) UpdateOrderStatus(ord order.Order, status types.OrderStatus) error {
	filled := int64(ord.FilledAmt())
	return a.UpdateOrderStatusByID(ord.ID(), ord.Base(), ord.Quote(), status, filled)
}

func (a *Archiver) moveOrder(oid order.OrderID, srcTableName, dstTableName string, status types.OrderStatus, filled int64) error {
	// Move the order, updating status and filled amount.
	moved, err := moveOrder(a.db, srcTableName, dstTableName, oid, status, uint64(filled))
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("order %s not moved from %s to %s", oid, srcTableName, dstTableName)
	}
	return nil
}

func (a *Archiver) moveCancelOrder(oid order.OrderID, srcTableName, dstTableName string, status types.OrderStatus) error {
	// Move the order, updating status and filled amount.
	moved, err := moveCancelOrder(a.db, srcTableName, dstTableName, oid, status)
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("cancel order %s not moved from %s to %s", oid, srcTableName, dstTableName)
	}
	return nil
}

func (a *Archiver) UpdateOrderFilledByID(oid order.OrderID, base, quote uint32, filled int64) error {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err
	}
	// Locate the order.
	status, orderType, initFilled, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if err != nil {
		return err
	}
	if status == types.OrderStatusUnknown {
		return fmt.Errorf("unknown order")
	}

	switch orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}

	if filled == initFilled {
		return nil // nothing to do
	}

	tableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
	err = updateOrderFilledAmt(a.db, tableName, oid, uint64(filled))
	return err
}

func (a *Archiver) UpdateOrderFilled(ord order.Order) error {
	filled := int64(ord.FilledAmt())
	return a.UpdateOrderFilledByID(ord.ID(), ord.Base(), ord.Quote(), filled)
}

func (a *Archiver) UserOrders(aid account.AccountID, base, quote uint32) ([]order.Order, error) {
	return nil, nil // TODO
}

// BEGIN regular order functions

func orderStatus(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string) (types.OrderStatus, order.OrderType, int64, error) {
	// Search active orders first.
	fullTable := fullOrderTableName(dbName, marketSchema, true)
	found, status, orderType, filled, err := findOrder(dbe, oid, fullTable)
	if err != nil {
		return types.OrderStatusUnknown, order.UnknownOrderType, -1, err
	}
	if found {
		return status, orderType, filled, nil
	}

	// Search archived orders.
	fullTable = fullOrderTableName(dbName, marketSchema, false)
	found, status, orderType, filled, err = findOrder(dbe, oid, fullTable)
	if err != nil {
		return types.OrderStatusUnknown, order.UnknownOrderType, -1, err
	}
	if found {
		return status, orderType, filled, nil
	}

	// Order not found in either orders table.
	return types.OrderStatusUnknown, order.UnknownOrderType, -1, ErrUnknownOrder
}

func findOrder(dbe *sql.DB, oid order.OrderID, fullTable string) (bool, types.OrderStatus, order.OrderType, int64, error) {
	stmt := fmt.Sprintf(internal.OrderStatus, fullTable)
	var status uint16
	var filled int64
	var orderType order.OrderType
	err := dbe.QueryRow(stmt, oid).Scan(&orderType, &status, &filled)
	switch err {
	case sql.ErrNoRows:
		return false, types.OrderStatusUnknown, order.UnknownOrderType, -1, nil
	case nil:
		return true, types.OrderStatus(status), orderType, filled, nil
	default:
		return false, types.OrderStatusUnknown, order.UnknownOrderType, -1, err
	}
}

func loadLimitOrder(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.LimitOrder, types.OrderStatus, error) {
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
		return lo, types.OrderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullOrderTableName(dbName, marketSchema, true)
	lo, status, err = loadLimitOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		return nil, types.OrderStatusUnknown, ErrUnknownOrder
	case nil:
		// found
		return lo, status, nil
	default:
		// query error
		return nil, types.OrderStatusUnknown, err
	}
	return lo, status, nil
}

func loadLimitOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.LimitOrder, types.OrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var lo order.LimitOrder
	var id order.OrderID
	var utxos pq.StringArray
	var status types.OrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &lo.OrderType, &lo.Sell,
		&lo.AccountID, &lo.Address, &lo.ClientTime, &lo.ServerTime, &utxos,
		&lo.Quantity, &lo.Rate, &lo.Force, &status, &lo.Filled)
	if err != nil {
		return nil, types.OrderStatusUnknown, err
	}

	lo.UTXOs = make([]order.UTXO, 0, len(utxos))
	for i := range utxos {
		utxo, err := newUtxoFromOutpoint(utxos[i])
		if err != nil {
			return nil, types.OrderStatusUnknown, fmt.Errorf("bad utxo %s: %v", utxos[i], err)
		}
		lo.UTXOs = append(lo.UTXOs, utxo)
	}

	return &lo, status, nil
}

func marshalUTXOs(UTXOs []order.UTXO) (utxos []string) {
	// UTXOs are stored as an array of strings like ["txid0:vout0", ...] despite
	// this being less space efficient than a BYTEA because it significantly
	// simplifies debugging.
	for _, u := range UTXOs {
		utxos = append(utxos, order.UTXOString(u))
	}
	return
}

func storeLimitOrder(dbe sqlExecutor, tableName string, lo *order.LimitOrder, status types.OrderStatus) (int64, error) {
	utxos := marshalUTXOs(lo.UTXOs)
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, lo.ID(), lo.Type(), lo.Sell, lo.AccountID,
		lo.Address, lo.ClientTime, lo.ServerTime, pq.StringArray(utxos),
		lo.Quantity, lo.Rate, lo.Force, status, lo.Filled)
}

func storeMarketOrder(dbe sqlExecutor, tableName string, mo *order.MarketOrder, status types.OrderStatus) (int64, error) {
	utxos := marshalUTXOs(mo.UTXOs)
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, mo.ID(), mo.Type(), mo.Sell, mo.AccountID,
		mo.Address, mo.ClientTime, mo.ServerTime, pq.StringArray(utxos),
		mo.Quantity, 0, order.ImmediateTiF, status, mo.Filled)
}

func updateOrderStatus(dbe sqlExecutor, tableName string, oid order.OrderID, status types.OrderStatus) error {
	stmt := fmt.Sprintf(internal.UpdateOrderStatus, tableName)
	_, err := dbe.Exec(stmt, status, oid)
	return err
}

func updateOrderFilledAmt(dbe sqlExecutor, tableName string, oid order.OrderID, filled uint64) error {
	stmt := fmt.Sprintf(internal.UpdateOrderFilledAmt, tableName)
	_, err := dbe.Exec(stmt, filled, oid)
	return err
}

func updateOrderStatusAndFilledAmt(dbe sqlExecutor, tableName string, oid order.OrderID, status types.OrderStatus, filled uint64) error {
	stmt := fmt.Sprintf(internal.UpdateOrderStatusAndFilledAmt, tableName)
	_, err := dbe.Exec(stmt, status, filled, oid)
	return err
}

func moveOrder(dbe sqlExecutor, oldTableName, newTableName string, oid order.OrderID, newStatus types.OrderStatus, newFilled uint64) (bool, error) {
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

func storeCancelOrder(dbe sqlExecutor, tableName string, co *order.CancelOrder, status types.OrderStatus) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertCancelOrder, tableName)
	return sqlExec(dbe, stmt, co.ID(), co.AccountID, co.ClientTime,
		co.ServerTime, co.TargetOrderID, status)
}

func loadCancelOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.CancelOrder, types.OrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var co order.CancelOrder
	var id order.OrderID
	var status types.OrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &co.AccountID, &co.ClientTime,
		&co.ServerTime, &co.TargetOrderID, &status)
	if err != nil {
		return nil, types.OrderStatusUnknown, err
	}

	co.OrderType = order.CancelOrderType

	return &co, status, nil
}

func loadCancelOrder(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.CancelOrder, types.OrderStatus, error) {
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
		return co, types.OrderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullCancelOrderTableName(dbName, marketSchema, false)
	co, status, err = loadCancelOrderFromTable(dbe, fullTable, oid)
	switch err {
	case sql.ErrNoRows:
		return nil, types.OrderStatusUnknown, ErrUnknownOrder
	case nil:
		// found
		return co, status, nil
	default:
		// query error
		return nil, types.OrderStatusUnknown, err
	}
}

func cancelOrderStatus(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string) (types.OrderStatus, error) {
	// Search active orders first.
	found, status, err := findCancelOrder(dbe, oid, dbName, marketSchema, true)
	if err != nil {
		return types.OrderStatusUnknown, err
	}
	if found {
		return status, nil
	}

	// Search archived orders.
	found, status, err = findCancelOrder(dbe, oid, dbName, marketSchema, false)
	if err != nil {
		return types.OrderStatusUnknown, err
	}
	if found {
		return status, nil
	}

	// Order not found in either orders table.
	return types.OrderStatusUnknown, ErrUnknownOrder
}

func findCancelOrder(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string, active bool) (bool, types.OrderStatus, error) {
	fullTable := fullCancelOrderTableName(dbName, marketSchema, active)
	stmt := fmt.Sprintf(internal.CancelOrderStatus, fullTable)
	var status uint16
	err := dbe.QueryRow(stmt, oid).Scan(&status)
	switch err {
	case sql.ErrNoRows:
		return false, types.OrderStatusUnknown, nil
	case nil:
		return true, types.OrderStatus(status), nil
	default:
		return false, types.OrderStatusUnknown, err
	}
}

func updateCancelOrderStatus(dbe sqlExecutor, tableName string, oid order.OrderID, status types.OrderStatus) error {
	return updateOrderStatus(dbe, tableName, oid, status)
}

func moveCancelOrder(dbe sqlExecutor, oldTableName, newTableName string, oid order.OrderID, newStatus types.OrderStatus) (bool, error) {
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
