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
	return loadLimitOrder(a.db, a.dbName, marketSchema, oid)
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

	var res sql.Result
	tableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
	switch ot := ord.(type) {
	case *order.MarketOrder:

	case *order.CancelOrder:

	case *order.LimitOrder:
		res, err = storeLimitOrder(a.db, tableName, ot, status)
		if err != nil {
			return err
		} else if res == nil {
			return fmt.Errorf("unknown error storing limit order %v", ot)
		}
	default:
		panic("db.ValidateOrder should have caught this")
	}

	if n, err0 := res.RowsAffected(); err0 != nil {
		return err0
	} else if n != 1 {
		return fmt.Errorf("failed to store order %v: %d rows affected, expected 1",
			ord.UID(), n)
	}

	return nil
}

func (a *Archiver) OrderStatusByID(oid order.OrderID, base, quote uint32) (types.OrderStatus, int64, error) {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return types.OrderStatusUnknown, -1, err
	}
	return orderStatus(a.db, oid, a.dbName, marketSchema)
}

func (a *Archiver) OrderStatus(ord order.Order) (types.OrderStatus, int64, error) {
	return a.OrderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

func (a *Archiver) UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status types.OrderStatus, filled int64) error {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err
	}
	initStatus, initFilled, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if err != nil {
		return err
	}
	if initStatus == types.OrderStatusUnknown {
		return fmt.Errorf("unknown order")
	}
	if filled == -1 {
		filled = initFilled
	}

	srcTableName := fullOrderTableName(a.dbName, marketSchema, initStatus.Active())
	if status.Active() == initStatus.Active() {
		if initStatus.Archived() {
			log.Warnf("archived order is changing status. "+
				"Order %s (%s -> %s)", oid, initStatus, status)
		}
		// No table move, just update.
		return updateOrderStatus(a.db, srcTableName, oid, status)
	}

	if initStatus.Archived() {
		log.Warnf("moving an order from an archived to active status. "+
			"Order %s (%s -> %s)", oid, initStatus, status)
	}

	// Move the order, updating status and filled amount
	dstTableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
	moved, err := moveOrder(a.db, srcTableName, dstTableName, oid, status, uint64(filled))
	if err != nil {
		return err
	}
	if !moved {
		return fmt.Errorf("order %s not moved from %s to %s", oid, srcTableName, dstTableName)
	}
	return nil
}

func (a *Archiver) UpdateOrderStatus(ord order.Order, status types.OrderStatus) error {
	filled := int64(ord.FilledAmt())
	return a.UpdateOrderStatusByID(ord.ID(), ord.Base(), ord.Quote(), status, filled)
}

func (a *Archiver) UpdateOrderByID(oid order.OrderID, base, quote uint32, filled int64) error {
	marketSchema, err := types.MarketName(base, quote)
	if err != nil {
		return err
	}
	// Locate the order.
	status, _, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if err != nil {
		return err
	}
	if status == types.OrderStatusUnknown {
		return fmt.Errorf("unknown order")
	}

	tableName := fullOrderTableName(a.dbName, marketSchema, status.Active())
	err = updateOrderFilledAmt(a.db, tableName, oid, uint64(filled))
	return err
}

func (a *Archiver) UpdateOrder(ord order.Order) error {
	filled := int64(ord.FilledAmt())
	return a.UpdateOrderByID(ord.ID(), ord.Base(), ord.Quote(), filled)
}

func (a *Archiver) UserOrders(aid account.AccountID, base, quote uint32) ([]order.Order, error) {
	return nil, nil // TODO
}

func fullOrderTableName(dbName, marketSchema string, active bool) string {
	var orderTable string
	if active {
		orderTable = "orders_active"
	} else {
		orderTable = "orders_archived"
	}

	return dbName + "." + marketSchema + "." + orderTable
}

func orderStatus(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string) (types.OrderStatus, int64, error) {
	// Search active orders first.
	found, status, filled, err := findOrder(dbe, oid, dbName, marketSchema, false)
	if err != nil {
		return types.OrderStatusUnknown, -1, err
	}
	if found {
		return status, filled, nil
	}

	// Search archived orders.
	found, status, filled, err = findOrder(dbe, oid, dbName, marketSchema, true)
	if err != nil {
		return types.OrderStatusUnknown, -1, err
	}
	if found {
		return status, filled, nil
	}

	// Order not found in either orders table.
	return types.OrderStatusUnknown, -1, nil
}

func findOrder(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string, archived bool) (bool, types.OrderStatus, int64, error) {
	fullTable := fullOrderTableName(dbName, marketSchema, archived)
	stmt := fmt.Sprintf(internal.OrderStatus, fullTable)
	var status uint16
	var filled int64
	err := dbe.QueryRow(stmt, oid).Scan(&status, &filled)
	switch err {
	case sql.ErrNoRows:
		return false, types.OrderStatusUnknown, -1, nil
	case nil:
		return true, types.OrderStatus(status), filled, nil
	default:
		return false, types.OrderStatusUnknown, -1, err
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
		// not found
		return nil, types.OrderStatusUnknown, nil
	case nil:
		// found
		return lo, status, nil
	default:
		// query error
		return lo, types.OrderStatusUnknown, err
	}
}

func loadLimitOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.LimitOrder, types.OrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var lo order.LimitOrder
	var id order.OrderID
	var utxos pq.StringArray
	var status types.OrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &lo.OrderType, &lo.Sell,
		&lo.AccountID, &lo.Address, &lo.ClientTime, &lo.ServerTime, &utxos,
		&lo.Quantity, &status, &lo.Filled)
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

func storeLimitOrder(dbe sqlExecutor, tableName string, lo *order.LimitOrder, status types.OrderStatus) (sql.Result, error) {
	// UTXOs are stored as an array of strings like ["txid0:vout0", ...] despite
	// this being less space efficient than a BYTEA because it significantly
	// simplifies debugging.
	var utxos []string
	for _, u := range lo.UTXOs {
		utxos = append(utxos, order.UTXOString(u))
	}
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	result, err := dbe.Exec(stmt, lo.ID(), lo.Type(), lo.Sell, lo.AccountID,
		lo.Address, lo.ClientTime.Unix(), lo.ServerTime.Unix(), pq.StringArray(utxos),
		lo.Quantity, status, lo.Filled)
	return result, err
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

func moveOrder(dbe sqlExecutor, oldTableName, newTableName string, oid order.OrderID, newStatus types.OrderStatus, newFilled uint64) (bool, error) {
	stmt := fmt.Sprintf(internal.MoveOrder, oldTableName, newTableName)
	res, err := dbe.Exec(stmt, oid, newStatus, newFilled)
	if err != nil {
		return false, err
	}
	moved, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if moved != 1 {
		panic(fmt.Sprintf("moved %d orders instead of 1", moved))
	}
	return true, nil
}
