// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sort"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/lib/pq"
)

// Wrap the CoinID slice to implement custom Scanner and Valuer.
type dbCoins []order.CoinID

// Value implements the sql/driver.Valuer interface. The coin IDs are encoded as
// L0|ID0|L1|ID1|... where | is simple concatenation, Ln is the length of the
// nth coin ID, and IDn is the bytes of the nth coinID.
func (coins dbCoins) Value() (driver.Value, error) {
	if len(coins) == 0 {
		return []byte{}, nil
	}
	// As an initial guess that's likely accurate for most coins, allocate as if
	// each coin ID is the same length.
	lenGuess := len(coins[0])
	b := make([]byte, 0, len(coins)*(lenGuess+1))
	for _, coin := range coins {
		b = append(b, byte(len(coin)))
		b = append(b, coin...)
	}
	return b, nil
}

// Scan implements the sql.Scanner interface.
func (coins *dbCoins) Scan(src interface{}) error {
	b := src.([]byte)
	if len(b) == 0 {
		*coins = dbCoins{}
		return nil
	}
	lenGuess := int(b[0])
	if lenGuess == 0 {
		return fmt.Errorf("zero-length coin ID indicated")
	}
	c := make(dbCoins, 0, len(b)/(lenGuess+1))
	for len(b) > 0 {
		cLen := int(b[0])
		if cLen == 0 {
			return fmt.Errorf("zero-length coin ID indicated")
		}
		if len(b) < cLen+1 {
			return fmt.Errorf("too many bytes indicated")
		}

		// Deep copy the coin ID (a slice) since the backing buffer may be
		// reused.
		bc := make([]byte, cLen)
		copy(bc, b[1:cLen+1])
		c = append(c, bc)

		b = b[cLen+1:]
	}

	*coins = c
	return nil
}

var _ db.OrderArchiver = (*Archiver)(nil)

// Order retrieves an order with the given OrderID, stored for the market
// specified by the given base and quote assets. A non-nil error will be
// returned if the market is not recognized. If the order is not found, the
// error value is ErrUnknownOrder, and the type is order.OrderStatusUnknown. The
// only recognized order types are market, limit, and cancel.
func (a *Archiver) Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, order.OrderStatusUnknown, err
	}

	// Since order type is unknown:
	// - try to load from orders table, which includes market and limit orders
	// - if found, coerce into the correct order type and return
	// - if not found, try loading a cancel order with this oid
	var errA db.ArchiveError
	ord, status, err := loadTrade(a.db, a.dbName, marketSchema, oid)
	if errors.As(err, &errA) {
		if errA.Code != db.ErrUnknownOrder {
			return nil, order.OrderStatusUnknown, err
		}
		// Try the cancel orders.
		var co *order.CancelOrder
		co, status, err = loadCancelOrder(a.db, a.dbName, marketSchema, oid)
		if err != nil {
			return nil, order.OrderStatusUnknown, err // includes ErrUnknownOrder
		}
		co.BaseAsset, co.QuoteAsset = base, quote
		return co, pgToMarketStatus(status), err
		// no other order types to try presently
	}
	if err != nil {
		return nil, order.OrderStatusUnknown, err
	}
	prefix := ord.Prefix()
	prefix.BaseAsset, prefix.QuoteAsset = base, quote
	return ord, pgToMarketStatus(status), nil
}

type pgOrderStatus int16

const (
	orderStatusUnknown pgOrderStatus = iota
	orderStatusEpoch
	orderStatusBooked
	orderStatusExecuted
	orderStatusFailed // failed helps distinguish matched from unmatched executed cancel orders
	orderStatusCanceled
	orderStatusRevoked // indicates a trade order was revoked, or in the cancels table that the cancel is server-generated
)

func marketToPgStatus(status order.OrderStatus) pgOrderStatus {
	switch status {
	case order.OrderStatusEpoch:
		return orderStatusEpoch
	case order.OrderStatusBooked:
		return orderStatusBooked
	case order.OrderStatusExecuted:
		return orderStatusExecuted
	case order.OrderStatusCanceled:
		return orderStatusCanceled
	case order.OrderStatusRevoked:
		return orderStatusRevoked
	}
	return orderStatusUnknown
}

func pgToMarketStatus(status pgOrderStatus) order.OrderStatus {
	switch status {
	case orderStatusEpoch:
		return order.OrderStatusEpoch
	case orderStatusBooked:
		return order.OrderStatusBooked
	case orderStatusExecuted, orderStatusFailed: // failed is executed as far as the market is concerned
		return order.OrderStatusExecuted
	case orderStatusCanceled:
		return order.OrderStatusCanceled
	case orderStatusRevoked, -orderStatusRevoked: // negative revoke status means forgiven preimage miss
		return order.OrderStatusRevoked
	}
	return order.OrderStatusUnknown
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
	case orderStatusCanceled, orderStatusRevoked, -orderStatusRevoked,
		orderStatusExecuted, orderStatusFailed, orderStatusUnknown:
		return false
	default:
		panic("unknown order status!") // programmer error
	}
}

// NewEpochOrder stores the given order with epoch status. This is equivalent to
// StoreOrder with OrderStatusEpoch.
func (a *Archiver) NewEpochOrder(ord order.Order, epochIdx, epochDur int64) error {
	return a.storeOrder(ord, epochIdx, epochDur, orderStatusEpoch)
}

func makePseudoCancel(target order.OrderID, user account.AccountID, base, quote uint32, timeStamp time.Time) *order.CancelOrder {
	// Create a server-generated cancel order to record the server's revoke
	// order action.
	return &order.CancelOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.CancelOrderType,
			ClientTime: timeStamp,
			ServerTime: timeStamp,
			// The zero-value for Commitment is stored as NULL. See
			// (Commitment).Value.
		},
		TargetOrderID: target,
	}
}

// FlushBook revokes all booked orders for a market.
func (a *Archiver) FlushBook(base, quote uint32) (sellsRemoved, buysRemoved []order.OrderID, err error) {
	var marketSchema string
	marketSchema, err = a.marketSchema(base, quote)
	if err != nil {
		return
	}

	// Booked orders (active) are made revoked (archived).
	srcTableName := fullOrderTableName(a.dbName, marketSchema, orderStatusBooked.active())
	dstTableName := fullOrderTableName(a.dbName, marketSchema, orderStatusRevoked.active())

	timeStamp := time.Now().Truncate(time.Millisecond).UTC()

	var dbTx *sql.Tx
	dbTx, err = a.db.Begin()
	if err != nil {
		err = fmt.Errorf("failed to begin database transaction: %w", err)
		return
	}

	fail := func() {
		sellsRemoved, buysRemoved = nil, nil
		a.fatalBackendErr(err)
		_ = dbTx.Rollback()
	}

	// Changed all booked orders to revoked.
	stmt := fmt.Sprintf(internal.PurgeBook, srcTableName, orderStatusRevoked, dstTableName)
	var rows *sql.Rows
	rows, err = dbTx.Query(stmt, orderStatusBooked)
	if err != nil {
		fail()
		return
	}
	defer rows.Close()

	var cos []*order.CancelOrder
	for rows.Next() {
		var oid order.OrderID
		var sell bool
		var aid account.AccountID
		if err = rows.Scan(&oid, &sell, &aid); err != nil {
			fail()
			return
		}
		cos = append(cos, makePseudoCancel(oid, aid, base, quote, timeStamp))
		if sell {
			sellsRemoved = append(sellsRemoved, oid)
		} else {
			buysRemoved = append(buysRemoved, oid)
		}
	}

	if err = rows.Err(); err != nil {
		fail()
		return
	}

	// Insert the pseudo-cancel orders.
	cancelTable := fullCancelOrderTableName(a.dbName, marketSchema, orderStatusRevoked.active())
	stmt = fmt.Sprintf(internal.InsertCancelOrder, cancelTable)
	for _, co := range cos {
		// Special values for this server-generate cancel order:
		//  - Pass nil instead of the zero value Commitment to save a comparison
		//    in (Commitment).Value with the zero value.
		//  - Set epoch idx to exemptEpochIdx (-1) and dur to dummyEpochDur (1),
		//    consistent with revokeOrder(..., exempt=true).
		_, err = dbTx.Exec(stmt, co.ID(), co.AccountID, co.ClientTime,
			co.ServerTime, nil, co.TargetOrderID, orderStatusRevoked, exemptEpochIdx, dummyEpochDur)
		if err != nil {
			fail()
			err = fmt.Errorf("failed to store pseudo-cancel order: %w", err)
			return
		}
	}

	if err = dbTx.Commit(); err != nil {
		fail()
		err = fmt.Errorf("failed to commit transaction: %w", err)
		return
	}

	return
}

// BookOrders retrieves all booked orders (with order status booked) for the
// specified market. This will be used to repopulate a market's book on
// construction of the market.
func (a *Archiver) BookOrders(base, quote uint32) ([]*order.LimitOrder, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	// All booked orders are active.
	tableName := fullOrderTableName(a.dbName, marketSchema, true) // active (true)

	// no query timeout here, only explicit cancellation
	ords, err := ordersByStatusFromTable(a.ctx, a.db, tableName, base, quote, orderStatusBooked)
	if err != nil {
		return nil, err
	}

	// Verify loaded orders are limits, and cast to *LimitOrder.
	limits := make([]*order.LimitOrder, 0, len(ords))
	for _, ord := range ords {
		lo, ok := ord.(*order.LimitOrder)
		if !ok {
			log.Errorf("loaded book order %v that was not a limit order", ord.ID())
			continue
		}

		limits = append(limits, lo)
	}

	return limits, nil
}

// EpochOrders retrieves all epoch orders for the specified market returns them
// as a slice of order.Order.
func (a *Archiver) EpochOrders(base, quote uint32) ([]order.Order, error) {
	los, mos, cos, err := a.epochOrders(base, quote)
	if err != nil {
		return nil, err
	}
	orders := make([]order.Order, 0, len(los)+len(mos)+len(cos))
	for _, o := range los {
		orders = append(orders, o)
	}
	for _, o := range mos {
		orders = append(orders, o)
	}
	for _, o := range cos {
		orders = append(orders, o)
	}
	return orders, nil
}

// epochOrders retrieves all epoch orders for the specified market.
func (a *Archiver) epochOrders(base, quote uint32) ([]*order.LimitOrder, []*order.MarketOrder, []*order.CancelOrder, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, nil, nil, err
	}

	tableName := fullOrderTableName(a.dbName, marketSchema, true) // active (true)

	// no query timeout here, only explicit cancellation
	ords, err := ordersByStatusFromTable(a.ctx, a.db, tableName, base, quote, orderStatusEpoch)
	if err != nil {
		return nil, nil, nil, err
	}

	// Verify loaded order type and add to correct slice.
	var limits []*order.LimitOrder
	var markets []*order.MarketOrder
	for _, ord := range ords {
		switch o := ord.(type) {
		case *order.LimitOrder:
			limits = append(limits, o)
		case *order.MarketOrder:
			markets = append(markets, o)
		default:
			log.Errorf("loaded epoch order %v that was not a limit or market order: %T", ord.ID(), ord)
		}
	}

	tableName = fullCancelOrderTableName(a.dbName, marketSchema, true) // active(true)
	cancels, err := cancelOrdersByStatusFromTable(a.ctx, a.db, tableName, base, quote, orderStatusEpoch)
	if err != nil {
		return nil, nil, nil, err
	}

	return limits, markets, cancels, nil
}

// ActiveOrderCoins retrieves a CoinID slice for each active order.
func (a *Archiver) ActiveOrderCoins(base, quote uint32) (baseCoins, quoteCoins map[order.OrderID][]order.CoinID, err error) {
	var marketSchema string
	marketSchema, err = a.marketSchema(base, quote)
	if err != nil {
		return
	}

	tableName := fullOrderTableName(a.dbName, marketSchema, true) // active (true)
	stmt := fmt.Sprintf(internal.SelectOrderCoinIDs, tableName)

	var rows *sql.Rows
	rows, err = a.db.Query(stmt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = nil
		fallthrough
	case err == nil:
		baseCoins = make(map[order.OrderID][]order.CoinID)
		quoteCoins = make(map[order.OrderID][]order.CoinID)
	default:
		return
	}
	defer rows.Close()

	for rows.Next() {
		var oid order.OrderID
		var coins dbCoins
		var sell bool
		err = rows.Scan(&oid, &sell, &coins)
		if err != nil {
			return nil, nil, err
		}

		// Sell orders lock base asset coins.
		if sell {
			baseCoins[oid] = coins
		} else {
			// Buy orders lock quote asset coins.
			quoteCoins[oid] = coins
		}
	}

	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return
}

// BookOrder updates the given LimitOrder with booked status.
func (a *Archiver) BookOrder(lo *order.LimitOrder) error {
	return a.updateOrderStatus(lo, orderStatusBooked)
}

// ExecuteOrder updates the given Order with executed status.
func (a *Archiver) ExecuteOrder(ord order.Order) error {
	return a.updateOrderStatus(ord, orderStatusExecuted)
}

// CancelOrder updates a LimitOrder with canceled status. If the order does not
// exist in the Archiver, CancelOrder returns ErrUnknownOrder. To store a new
// limit order with canceled status, use StoreOrder.
func (a *Archiver) CancelOrder(lo *order.LimitOrder) error {
	return a.updateOrderStatus(lo, orderStatusCanceled)
}

// RevokeOrder updates an Order with revoked status, which is used for
// DEX-revoked orders rather than orders matched with a user's CancelOrder. If
// the order does not exist in the Archiver, RevokeOrder returns
// ErrUnknownOrder. This may change orders with status executed to revoked,
// which may be unexpected.
func (a *Archiver) RevokeOrder(ord order.Order) (cancelID order.OrderID, timeStamp time.Time, err error) {
	return a.revokeOrder(ord, false)
}

// RevokeOrderUncounted is like RevokeOrder except that the generated cancel
// order will not be counted against the user. i.e. ExecutedCancelsForUser
// should not return the cancel orders created this way.
func (a *Archiver) RevokeOrderUncounted(ord order.Order) (cancelID order.OrderID, timeStamp time.Time, err error) {
	return a.revokeOrder(ord, true)
}

const (
	exemptEpochIdx  int64 = -1
	countedEpochIdx int64 = 0
	dummyEpochDur   int64 = 1 // for idx*duration math
)

func (a *Archiver) revokeOrder(ord order.Order, exempt bool) (cancelID order.OrderID, timeStamp time.Time, err error) {
	// Revoke the targeted order.
	err = a.updateOrderStatus(ord, orderStatusRevoked)
	if err != nil {
		return
	}

	// Store the pseudo-cancel order with 0 epoch idx and duration and status
	// orderStatusRevoked as indicators that this is a revocation.
	timeStamp = time.Now().Truncate(time.Millisecond).UTC()
	co := makePseudoCancel(ord.ID(), ord.User(), ord.Base(), ord.Quote(), timeStamp)
	cancelID = co.ID()
	epochIdx := countedEpochIdx
	if exempt {
		epochIdx = exemptEpochIdx
	}
	err = a.storeOrder(co, epochIdx, dummyEpochDur, orderStatusRevoked)
	return
}

// FailCancelOrder updates or inserts the given CancelOrder with failed status.
// To update a CancelOrder with executed status, use ExecuteOrder.
func (a *Archiver) FailCancelOrder(co *order.CancelOrder) error {
	return a.updateOrderStatus(co, orderStatusFailed)
}

func validateOrder(ord order.Order, status pgOrderStatus, mkt *dex.MarketInfo) bool {
	if status == orderStatusFailed && ord.Type() != order.CancelOrderType {
		return false
	}
	return db.ValidateOrder(ord, pgToMarketStatus(status), mkt)
}

// StoreOrder stores an order for the specified epoch ID (idx:dur) with the
// provided status. The market is determined from the Order. A non-nil error
// will be returned if the market is not recognized. All orders are validated
// via server/db.ValidateOrder to ensure only sensible orders reach persistent
// storage. Updating orders should be done via one of the update functions such
// as UpdateOrderStatus.
func (a *Archiver) StoreOrder(ord order.Order, epochIdx, epochDur int64, status order.OrderStatus) error {
	return a.storeOrder(ord, epochIdx, epochDur, marketToPgStatus(status))
}

func (a *Archiver) storeOrder(ord order.Order, epochIdx, epochDur int64, status pgOrderStatus) error {
	marketSchema, err := a.marketSchema(ord.Base(), ord.Quote())
	if err != nil {
		return err
	}

	if !validateOrder(ord, status, a.markets[marketSchema]) {
		return db.ArchiveError{
			Code: db.ErrInvalidOrder,
			Detail: fmt.Sprintf("invalid order %v for status %v and market %v",
				ord.UID(), status, a.markets[marketSchema]),
		}
	}

	// If enabled, search all tables for the order to ensure it is not already
	// stored somewhere.
	// if a.checkedStores {
	// 	var foundStatus pgOrderStatus
	// 	switch ord.Type() {
	// 	case order.MarketOrderType, order.LimitOrderType:
	// 		foundStatus, _, _, err = orderStatus(a.db, ord.ID(), a.dbName, marketSchema)
	// 	case order.CancelOrderType:
	// 		foundStatus, err = cancelOrderStatus(a.db, ord.ID(), a.dbName, marketSchema)
	// 	}
	//
	// 	if err == nil {
	// 		return fmt.Errorf("attempted to store a %s order while it exists "+
	// 			"in another table as %s", pgToMarketStatus(status), pgToMarketStatus(foundStatus))
	// 	}
	// 	if !db.IsErrOrderUnknown(err) {
	// 		a.fatalBackendErr(err)
	// 		return fmt.Errorf("findOrder failed: %v", err)
	// 	}
	// }

	// Check for order commitment duplicates. This also covers order ID since
	// commitment is part of order serialization. Note that it checks ALL
	// markets, so this may be excessive. This check may be more appropriate in
	// the caller, or may be removed in favor of a different check depending on
	// where preimages are stored. If we allow reused commitments if the
	// preimages are only revealed once, then the unique constraint on the
	// commit column in the orders tables would need to be removed.

	// IDEA: Do not apply this constraint to server-generated cancel orders,
	// which we may wish to have a zero value commitment and status revoked.
	// if _, isCancel := ord.(*order.CancelOrder); !isCancel || status != orderStatusRevoked {
	commit := ord.Commitment()
	found, prevOid, err := a.OrderWithCommit(a.ctx, commit) // no query timeouts in storeOrder, only explicit cancellation
	if err != nil {
		return err
	}
	if found {
		return db.ArchiveError{
			Code: db.ErrReusedCommit,
			Detail: fmt.Sprintf("order %v reuses commit %v from previous order %v",
				ord.UID(), commit, prevOid),
		}
	}

	var N int64
	switch ot := ord.(type) {
	case *order.CancelOrder:
		tableName := fullCancelOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeCancelOrder(a.db, tableName, ot, status, epochIdx, epochDur)
		if err != nil {
			a.fatalBackendErr(err)
			return fmt.Errorf("storeCancelOrder failed: %w", err)
		}
	case *order.MarketOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeMarketOrder(a.db, tableName, ot, status, epochIdx, epochDur)
		if err != nil {
			a.fatalBackendErr(err)
			return fmt.Errorf("storeMarketOrder failed: %w", err)
		}
	case *order.LimitOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeLimitOrder(a.db, tableName, ot, status, epochIdx, epochDur)
		if err != nil {
			a.fatalBackendErr(err)
			return fmt.Errorf("storeLimitOrder failed: %w", err)
		}
	default:
		panic("ValidateOrder should have caught this")
	}

	if N != 1 {
		err = fmt.Errorf("failed to store order %v: %d rows affected, expected 1",
			ord.UID(), N)
		a.fatalBackendErr(err)
		return err
	}

	return nil
}

func (a *Archiver) orderTableName(ord order.Order) (string, pgOrderStatus, error) {
	status, orderType, _, err := a.orderStatus(ord)
	if err != nil {
		return "", status, err
	}

	marketSchema, err := a.marketSchema(ord.Base(), ord.Quote())
	if err != nil {
		return "", status, err
	}

	var tableName string
	switch orderType {
	case order.MarketOrderType, order.LimitOrderType:
		tableName = fullOrderTableName(a.dbName, marketSchema, status.active())
	case order.CancelOrderType:
		tableName = fullCancelOrderTableName(a.dbName, marketSchema, status.active())
	default:
		return "", status, fmt.Errorf("unrecognized order type %v", orderType)
	}
	return tableName, status, nil
}

func (a *Archiver) OrderPreimage(ord order.Order) (order.Preimage, error) {
	var pi order.Preimage

	tableName, _, err := a.orderTableName(ord)
	if err != nil {
		return pi, err
	}

	stmt := fmt.Sprintf(internal.SelectOrderPreimage, tableName)
	err = a.db.QueryRow(stmt, ord.ID()).Scan(&pi)
	return pi, err
}

// StorePreimage stores the preimage associated with an existing order.
func (a *Archiver) StorePreimage(ord order.Order, pi order.Preimage) error {
	tableName, status, err := a.orderTableName(ord)
	if err != nil {
		return err
	}

	// Preimages are stored during epoch processing, specifically after users
	// have responded with their preimages but before swap negotiation begins.
	// Thus, this order should be "active" i.e. not in an archived orders table.
	if !status.active() {
		log.Warnf("Attempting to set preimage for archived order %v", ord.UID())
	}

	stmt := fmt.Sprintf(internal.SetOrderPreimage, tableName)
	N, err := sqlExec(a.db, stmt, pi, ord.ID())
	if err != nil {
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("failed to update 1 order's preimage, updated %d", N)
	}
	return nil
}

// SetOrderCompleteTime sets the swap completion time for an existing order.
func (a *Archiver) SetOrderCompleteTime(ord order.Order, compTimeMs int64) error {
	status, orderType, _, err := a.orderStatus(ord)
	if err != nil {
		return err
	}

	if status != orderStatusExecuted /*status.active()*/ {
		log.Warnf("Attempting to set swap completion time for active order %v", ord.UID())
		return db.ArchiveError{
			Code:   db.ErrOrderNotExecuted,
			Detail: fmt.Sprintf("unable to set completed time for order %v not in executed status ", ord.UID()),
		}
	}

	marketSchema, err := a.marketSchema(ord.Base(), ord.Quote())
	if err != nil {
		return db.ArchiveError{
			Code: db.ErrInvalidOrder,
			Detail: fmt.Sprintf("unknown market (%d, %d) for order %v",
				ord.Base(), ord.Quote(), ord.UID()),
		}
	}

	var tableName string
	switch orderType {
	case order.MarketOrderType, order.LimitOrderType:
		tableName = fullOrderTableName(a.dbName, marketSchema, status.active())
	case order.CancelOrderType:
		tableName = fullCancelOrderTableName(a.dbName, marketSchema, status.active())
	default:
		return db.ArchiveError{
			Code:   db.ErrInvalidOrder,
			Detail: fmt.Sprintf("unknown type for order %v: %v", ord.UID(), orderType),
		}
	}

	stmt := fmt.Sprintf(internal.SetOrderCompleteTime, tableName)
	N, err := sqlExec(a.db, stmt, compTimeMs, ord.ID())
	if err != nil {
		a.fatalBackendErr(err)
		return db.ArchiveError{
			Code:   db.ErrGeneralFailure,
			Detail: "SetOrderCompleteTime failed:" + err.Error(),
		}
	}
	if N != 1 {
		return db.ArchiveError{
			Code:   db.ErrUpdateCount,
			Detail: fmt.Sprintf("failed to update 1 order's completion time, updated %d", N),
		}
	}
	return nil
}

type orderCompStamped struct {
	oid order.OrderID
	t   int64
}

// CompletedUserOrders retrieves the N most recently completed orders for a user
// across all markets.
func (a *Archiver) CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error) {
	var ords []orderCompStamped

	for m := range a.markets {
		tableName := fullOrderTableName(a.dbName, m, false) // NOT active table
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		mktOids, err := completedUserOrders(ctx, a.db, tableName, aid, N)
		cancel()
		if err != nil {
			return nil, nil, err
		}
		ords = append(ords, mktOids...)
	}

	sort.Slice(ords, func(i, j int) bool {
		return ords[i].t > ords[j].t // descending, latest completed order first
	})

	if N > len(ords) {
		N = len(ords)
	}

	for i := range ords[:N] {
		oids = append(oids, ords[i].oid)
		compTimes = append(compTimes, ords[i].t)
	}

	return
}

func completedUserOrders(ctx context.Context, dbe *sql.DB, tableName string, aid account.AccountID, N int) (oids []orderCompStamped, err error) {
	stmt := fmt.Sprintf(internal.RetrieveCompletedOrdersForAccount, tableName)
	var rows *sql.Rows
	rows, err = dbe.QueryContext(ctx, stmt, aid, N)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var oid order.OrderID
		var acct account.AccountID
		var completeTime sql.NullInt64
		err = rows.Scan(&oid, &acct, &completeTime)
		if err != nil {
			return nil, err
		}

		oids = append(oids, orderCompStamped{oid, completeTime.Int64})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return
}

// PreimageStats retrieves results of the N most recent preimage requests for
// the user across all markets.
func (a *Archiver) PreimageStats(user account.AccountID, lastN int) ([]*db.PreimageResult, error) {
	var outcomes []*db.PreimageResult

	queryOutcomes := func(stmt string) error {
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		defer cancel()

		rows, err := a.db.QueryContext(ctx, stmt, user, lastN, orderStatusRevoked)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var miss bool
			var time int64
			var oid order.OrderID
			err = rows.Scan(&oid, &miss, &time)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, &db.PreimageResult{
				Miss: miss,
				Time: time,
				ID:   oid,
			})
		}

		return rows.Err()
	}

	for m := range a.markets {
		// archived trade orders
		stmt := fmt.Sprintf(internal.PreimageResultsLastN, fullOrderTableName(a.dbName, m, false))
		if err := queryOutcomes(stmt); err != nil {
			return nil, err
		}

		// archived cancel orders
		stmt = fmt.Sprintf(internal.CancelPreimageResultsLastN, fullCancelOrderTableName(a.dbName, m, false))
		if err := queryOutcomes(stmt); err != nil {
			return nil, err
		}
	}

	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[j].Time < outcomes[i].Time // descending
	})
	if len(outcomes) > lastN {
		outcomes = outcomes[:lastN]
	}

	return outcomes, nil
}

// OrderStatusByID gets the status, type, and filled amount of the order with
// the given OrderID in the market specified by a base and quote asset. See also
// OrderStatus. If the order is not found, the error value is ErrUnknownOrder,
// and the type is order.OrderStatusUnknown.
func (a *Archiver) OrderStatusByID(oid order.OrderID, base, quote uint32) (order.OrderStatus, order.OrderType, int64, error) {
	pgStatus, orderType, filled, err := a.orderStatusByID(oid, base, quote)
	return pgToMarketStatus(pgStatus), orderType, filled, err
}

func (a *Archiver) orderStatusByID(oid order.OrderID, base, quote uint32) (pgOrderStatus, order.OrderType, int64, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return orderStatusUnknown, order.UnknownOrderType, -1, err
	}
	status, orderType, filled, err := orderStatus(a.db, oid, a.dbName, marketSchema)
	if db.IsErrOrderUnknown(err) {
		status, err = cancelOrderStatus(a.db, oid, a.dbName, marketSchema)
		if err != nil {
			// The severity of an unknown order is up to the caller.
			if !db.IsErrOrderUnknown(err) {
				a.fatalBackendErr(err)
			}
			return orderStatusUnknown, order.UnknownOrderType, -1, err // includes ErrUnknownOrder
		}
		filled = -1
		orderType = order.CancelOrderType
	}
	return status, orderType, filled, err
}

// OrderStatus gets the status, ID, and filled amount of the given order. See
// also OrderStatusByID.
func (a *Archiver) OrderStatus(ord order.Order) (order.OrderStatus, order.OrderType, int64, error) {
	return a.OrderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

func (a *Archiver) orderStatus(ord order.Order) (pgOrderStatus, order.OrderType, int64, error) {
	return a.orderStatusByID(ord.ID(), ord.Base(), ord.Quote())
}

// UpdateOrderStatusByID updates the status and filled amount of the order with
// the given OrderID in the market specified by a base and quote asset. If
// filled is -1, the filled amount is unchanged. For cancel orders, the filled
// amount is ignored. OrderStatusByID is used to locate the existing order. If
// the order is not found, the error value is ErrUnknownOrder, and the type is
// market/order.OrderStatusUnknown. See also UpdateOrderStatus.
func (a *Archiver) UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status order.OrderStatus, filled int64) error {
	return a.updateOrderStatusByID(oid, base, quote, marketToPgStatus(status), filled)
}

func (a *Archiver) updateOrderStatusByID(oid order.OrderID, base, quote uint32, status pgOrderStatus, filled int64) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}

	initStatus, orderType, initFilled, err := a.orderStatusByID(oid, base, quote)
	if err != nil {
		return err
	}

	if initStatus == status && filled == initFilled {
		log.Tracef("Not updating order with no status or filled amount change: %v.", oid)
		return nil
	}
	if filled == -1 {
		filled = initFilled
	}

	tableChange := status.active() != initStatus.active()

	if !initStatus.active() {
		if tableChange {
			return fmt.Errorf("Moving an order from an archived to active status: "+
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
func (a *Archiver) UpdateOrderStatus(ord order.Order, status order.OrderStatus) error {
	return a.updateOrderStatus(ord, marketToPgStatus(status))
}

func (a *Archiver) updateOrderStatus(ord order.Order, status pgOrderStatus) error {
	var filled int64
	if ord.Type() != order.CancelOrderType {
		filled = int64(ord.Trade().Filled())
	}
	return a.updateOrderStatusByID(ord.ID(), ord.Base(), ord.Quote(), status, filled)
}

func (a *Archiver) moveOrder(oid order.OrderID, srcTableName, dstTableName string, status pgOrderStatus, filled int64) error {
	// Move the order, updating status and filled amount.
	moved, err := moveOrder(a.db, srcTableName, dstTableName, oid,
		status, uint64(filled))
	if err != nil {
		a.fatalBackendErr(err)
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
		a.fatalBackendErr(err)
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
// value is ErrUnknownOrder, and the type is order.OrderStatusUnknown. See also
// UpdateOrderFilled. To also update the order status, use UpdateOrderStatusByID
// or UpdateOrderStatus.
func (a *Archiver) UpdateOrderFilledByID(oid order.OrderID, base, quote uint32, filled int64) error {
	// Locate the order.
	status, orderType, initFilled, err := a.orderStatusByID(oid, base, quote)
	if err != nil {
		return err
	}

	switch orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}

	if filled == initFilled {
		return nil // nothing to do
	}

	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err // should be caught already by a.OrderStatusByID
	}
	tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
	err = updateOrderFilledAmt(a.db, tableName, oid, uint64(filled))
	if err != nil {
		a.fatalBackendErr(err) // TODO: it could have changed tables since this function is not atomic
	}
	return err
}

// UpdateOrderFilled updates the filled amount of the given order. Both the
// market and new filled amount are determined from the Order. OrderStatusByID
// is used to locate the existing order. This function applies only to limit
// orders, not market or cancel orders. Market orders may only be updated by
// ExecuteOrder since their filled amount only changes when their status
// changes. See also UpdateOrderFilledByID.
func (a *Archiver) UpdateOrderFilled(ord *order.LimitOrder) error {
	switch orderType := ord.Type(); orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}
	return a.UpdateOrderFilledByID(ord.ID(), ord.Base(), ord.Quote(), int64(ord.Trade().Filled()))
}

// UserOrders retrieves all orders for the given account in the market specified
// by a base and quote asset.
func (a *Archiver) UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []order.OrderStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, nil, err
	}

	orders, pgStatuses, err := a.userOrders(ctx, base, quote, aid)
	if err != nil {
		a.fatalBackendErr(err)
		log.Errorf("Failed to query for orders by user for market %v and account %v",
			marketSchema, aid)
		return nil, nil, err
	}
	statuses := make([]order.OrderStatus, len(pgStatuses))
	for i := range pgStatuses {
		statuses[i] = pgToMarketStatus(pgStatuses[i])
	}
	return orders, statuses, err
}

// UserOrderStatuses retrieves the statuses and filled amounts of the orders
// with the provided order IDs for the given account in the market specified
// by a base and quote asset.
// The number and ordering of the returned statuses is not necessarily the same
// as the number and ordering of the provided order IDs. It is not an error if
// any or all of the provided order IDs cannot be found for the given account
// in the specified market.
func (a *Archiver) UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*db.OrderStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}

	// Active orders.
	fullTable := fullOrderTableName(a.dbName, marketSchema, true)
	activeOrderStatuses, err := a.userOrderStatusesFromTable(fullTable, aid, oids)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.fatalBackendErr(err)
		log.Errorf("Failed to query for active order statuses by user for market %v and account %v",
			marketSchema, aid)
		return nil, err
	}

	if len(oids) == len(activeOrderStatuses) {
		return activeOrderStatuses, nil
	}

	foundOrders := make(map[order.OrderID]bool, len(activeOrderStatuses))
	for _, status := range activeOrderStatuses {
		foundOrders[status.ID] = true
	}
	var remainingOids []order.OrderID
	for _, oid := range oids {
		if !foundOrders[oid] {
			remainingOids = append(remainingOids, oid)
		}
	}

	// Archived Orders.
	fullTable = fullOrderTableName(a.dbName, marketSchema, false)
	archivedOrderStatuses, err := a.userOrderStatusesFromTable(fullTable, aid, remainingOids)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.fatalBackendErr(err)
		log.Errorf("Failed to query for archived order statuses by user for market %v and account %v",
			marketSchema, aid)
		return nil, err
	}

	return append(activeOrderStatuses, archivedOrderStatuses...), nil
}

// ActiveUserOrderStatuses retrieves the statuses and filled amounts of all
// active orders for a user across all markets.
func (a *Archiver) ActiveUserOrderStatuses(aid account.AccountID) ([]*db.OrderStatus, error) {
	var orders []*db.OrderStatus
	for m := range a.markets {
		tableName := fullOrderTableName(a.dbName, m, true) // active table
		mktOrders, err := a.userOrderStatusesFromTable(tableName, aid, nil)
		if err != nil {
			return nil, err
		}
		orders = append(orders, mktOrders...)
	}
	return orders, nil
}

// Pass nil or empty oids to return statuses for all user orders in the
// specified table.
func (a *Archiver) userOrderStatusesFromTable(fullTable string, aid account.AccountID, oids []order.OrderID) ([]*db.OrderStatus, error) {
	execQuery := func(ctx context.Context) (*sql.Rows, error) {
		if len(oids) == 0 {
			stmt := fmt.Sprintf(internal.SelectUserOrderStatuses, fullTable)
			return a.db.QueryContext(ctx, stmt, aid)
		}
		oidArr := make(pq.ByteaArray, 0, len(oids))
		for i := range oids {
			oidArr = append(oidArr, oids[i][:])
		}
		stmt := fmt.Sprintf(internal.SelectUserOrderStatusesByID, fullTable)
		return a.db.QueryContext(ctx, stmt, aid, oidArr)
	}

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	rows, err := execQuery(ctx)
	defer cancel()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := make([]*db.OrderStatus, 0, len(oids))
	for rows.Next() {
		var oid order.OrderID
		var status pgOrderStatus
		err = rows.Scan(&oid, &status)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, &db.OrderStatus{
			ID:     oid,
			Status: pgToMarketStatus(status),
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return statuses, nil
}

// OrderWithCommit searches all markets' trade and cancel orders, both active
// and archived, for an order with the given Commitment.
func (a *Archiver) OrderWithCommit(ctx context.Context, commit order.Commitment) (found bool, oid order.OrderID, err error) {
	// Check all markets.
	for marketSchema := range a.markets {
		found, oid, err = orderForCommit(ctx, a.db, a.dbName, marketSchema, commit)
		if err != nil {
			a.fatalBackendErr(err)
			log.Errorf("Failed to query for orders by commit for market %v and commit %v",
				marketSchema, commit)
			return
		}
		if found {
			return
		}
	}
	return // false, zero, nil
}

type cancelExecStamped struct {
	oid, target order.OrderID
	t           int64
}

// ExecutedCancelsForUser retrieves up to N executed cancel orders for a given
// user. These may be user-initiated cancels, or cancels created by the server
// (revokes). Executed cancel orders from all markets are returned.
func (a *Archiver) ExecutedCancelsForUser(aid account.AccountID, N int) (oids, targets []order.OrderID, execTimes []int64, err error) {
	var ords []cancelExecStamped

	// Check all markets.
	for marketSchema := range a.markets {
		// Query for executed cancels (user-initiated).
		cancelTableName := fullCancelOrderTableName(a.dbName, marketSchema, false) // executed cancel orders are inactive
		epochsTableName := fullEpochsTableName(a.dbName, marketSchema)
		stmt := fmt.Sprintf(internal.RetrieveCancelTimesForUserByStatus, cancelTableName, epochsTableName)
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		mktOids, err := a.executedCancelsForUser(ctx, a.db, stmt, aid, N)
		cancel()
		if err != nil {
			return nil, nil, nil, err
		}
		ords = append(ords, mktOids...)

		// Query for revoked orders (server-initiated cancels).
		stmt = fmt.Sprintf(internal.SelectRevokeCancels, cancelTableName)
		ctx, cancel = context.WithTimeout(a.ctx, a.queryTimeout)
		mktOids, err = a.revokeGeneratedCancelsForUser(ctx, a.db, stmt, aid, N)
		cancel()
		if err != nil {
			return nil, nil, nil, err
		}
		ords = append(ords, mktOids...)
	}

	sort.Slice(ords, func(i, j int) bool {
		return ords[i].t > ords[j].t // descending, latest completed order first
	})

	if N > len(ords) {
		N = len(ords)
	}

	for i := range ords[:N] {
		oids = append(oids, ords[i].oid)
		targets = append(targets, ords[i].target)
		execTimes = append(execTimes, ords[i].t)
	}

	return
}

func (a *Archiver) executedCancelsForUser(ctx context.Context, dbe *sql.DB, stmt string, aid account.AccountID, N int) (ords []cancelExecStamped, err error) {
	var rows *sql.Rows
	rows, err = dbe.QueryContext(ctx, stmt, aid, orderStatusExecuted, N) // excludes orderStatusFailed
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var oid, target order.OrderID
		var execTime int64
		err = rows.Scan(&oid, &target, &execTime)
		if err != nil {
			return
		}

		ords = append(ords, cancelExecStamped{oid, target, execTime})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return
}

// revokeGeneratedCancelsForUser excludes exempt/uncounted cancels created with
// RevokeOrderUncounted or revokeOrder(..., exempt=true).
func (a *Archiver) revokeGeneratedCancelsForUser(ctx context.Context, dbe *sql.DB, stmt string, aid account.AccountID, N int) (ords []cancelExecStamped, err error) {
	var rows *sql.Rows
	rows, err = dbe.QueryContext(ctx, stmt, aid, orderStatusRevoked, N)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var oid, target order.OrderID
		var revokeTime time.Time
		var epochIdx int64
		err = rows.Scan(&oid, &target, &revokeTime, &epochIdx)
		if err != nil {
			return
		}

		// only include non-exempt/counted cancels
		if epochIdx == exemptEpochIdx {
			continue
		}

		ords = append(ords, cancelExecStamped{oid, target, encode.UnixMilli(revokeTime)})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return
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
	return orderStatusUnknown, order.UnknownOrderType, -1, db.ArchiveError{Code: db.ErrUnknownOrder}
}

func findOrder(dbe *sql.DB, oid order.OrderID, fullTable string) (bool, pgOrderStatus, order.OrderType, int64, error) {
	stmt := fmt.Sprintf(internal.OrderStatus, fullTable)
	var status pgOrderStatus
	var filled int64
	var orderType order.OrderType
	err := dbe.QueryRow(stmt, oid).Scan(&orderType, &status, &filled)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, orderStatusUnknown, order.UnknownOrderType, -1, nil
	case err == nil:
		return true, status, orderType, filled, nil
	default:
		return false, orderStatusUnknown, order.UnknownOrderType, -1, err
	}
}

// loadTrade does NOT set BaseAsset and QuoteAsset!
func loadTrade(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (order.Order, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullOrderTableName(dbName, marketSchema, true)
	ord, status, err := loadTradeFromTable(dbe, fullTable, oid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// try archived orders next
	case err == nil:
		// found
		return ord, status, nil
	default:
		// query error
		return ord, orderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullOrderTableName(dbName, marketSchema, false)
	ord, status, err = loadTradeFromTable(dbe, fullTable, oid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, orderStatusUnknown, db.ArchiveError{Code: db.ErrUnknownOrder}
	case err == nil:
		// found
		return ord, status, nil
	default:
		// query error
		return nil, orderStatusUnknown, err
	}
}

// loadTradeFromTable does NOT set BaseAsset and QuoteAsset!
func loadTradeFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (order.Order, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var prefix order.Prefix
	var trade order.Trade
	var id order.OrderID
	var tif order.TimeInForce
	var rate uint64
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &prefix.OrderType, &trade.Sell,
		&prefix.AccountID, &trade.Address, &prefix.ClientTime, &prefix.ServerTime,
		&prefix.Commit, (*dbCoins)(&trade.Coins),
		&trade.Quantity, &rate, &tif, &status, &trade.FillAmt)
	if err != nil {
		return nil, orderStatusUnknown, err
	}
	switch prefix.OrderType {
	case order.LimitOrderType:
		return &order.LimitOrder{
			T:     *trade.Copy(), // govet would complain because Trade has a Mutex
			P:     prefix,
			Rate:  rate,
			Force: tif,
		}, status, nil
	case order.MarketOrderType:
		return &order.MarketOrder{
			T: *trade.Copy(),
			P: prefix,
		}, status, nil

	}
	return nil, 0, fmt.Errorf("unknown order type %d retrieved", prefix.OrderType)
}

func (a *Archiver) userOrders(ctx context.Context, base, quote uint32, aid account.AccountID) ([]order.Order, []pgOrderStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, nil, err
	}

	// Active orders.
	fullTable := fullOrderTableName(a.dbName, marketSchema, true)
	orders, statuses, err := userOrdersFromTable(ctx, a.db, fullTable, base, quote, aid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	// Archived Orders.
	fullTable = fullOrderTableName(a.dbName, marketSchema, false)
	ordersArchived, statusesArchived, err := userOrdersFromTable(ctx, a.db, fullTable, base, quote, aid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	orders = append(orders, ordersArchived...)
	statuses = append(statuses, statusesArchived...)

	return orders, statuses, nil
}

func cancelOrdersByStatusFromTable(ctx context.Context, dbe *sql.DB, fullTable string, base, quote uint32, status pgOrderStatus) ([]*order.CancelOrder, error) {
	stmt := fmt.Sprintf(internal.SelectCancelOrdersByStatus, fullTable)
	rows, err := dbe.QueryContext(ctx, stmt, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cos []*order.CancelOrder

	for rows.Next() {
		var co order.CancelOrder
		co.OrderType = order.CancelOrderType
		err := rows.Scan(&co.AccountID, &co.ClientTime,
			&co.ServerTime, &co.Commit, &co.TargetOrderID)
		if err != nil {
			return nil, err
		}
		co.BaseAsset, co.QuoteAsset = base, quote
		cos = append(cos, &co)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return cos, nil
}

// base and quote are used to set the prefix, not specify which table to search.
// NOTE: There is considerable overlap with userOrdersFromTable, but a
// generalized function is likely to hurt readability and simplicity.
func ordersByStatusFromTable(ctx context.Context, dbe *sql.DB, fullTable string, base, quote uint32, status pgOrderStatus) ([]order.Order, error) {
	stmt := fmt.Sprintf(internal.SelectOrdersByStatus, fullTable)
	rows, err := dbe.QueryContext(ctx, stmt, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []order.Order

	for rows.Next() {
		var prefix order.Prefix
		var trade order.Trade
		var id order.OrderID
		var tif order.TimeInForce
		var rate uint64
		err = rows.Scan(&id, &prefix.OrderType, &trade.Sell,
			&prefix.AccountID, &trade.Address, &prefix.ClientTime, &prefix.ServerTime,
			&prefix.Commit, (*dbCoins)(&trade.Coins),
			&trade.Quantity, &rate, &tif, &trade.FillAmt)
		if err != nil {
			return nil, err
		}
		prefix.BaseAsset, prefix.QuoteAsset = base, quote

		var ord order.Order
		switch prefix.OrderType {
		case order.LimitOrderType:
			ord = &order.LimitOrder{
				P:     prefix,
				T:     *trade.Copy(),
				Rate:  rate,
				Force: tif,
			}
		case order.MarketOrderType:
			ord = &order.MarketOrder{
				P: prefix,
				T: *trade.Copy(),
			}
		default:
			log.Errorf("ordersByStatusFromTable: encountered unexpected order type %v",
				prefix.OrderType)
			continue
		}

		orders = append(orders, ord)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return orders, nil
}

// base and quote are used to set the prefix, not specify which table to search.
func userOrdersFromTable(ctx context.Context, dbe *sql.DB, fullTable string, base, quote uint32, aid account.AccountID) ([]order.Order, []pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectUserOrders, fullTable)
	rows, err := dbe.QueryContext(ctx, stmt, aid)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var orders []order.Order
	var statuses []pgOrderStatus

	for rows.Next() {
		var prefix order.Prefix
		var trade order.Trade
		var id order.OrderID
		var tif order.TimeInForce
		var rate uint64
		var status pgOrderStatus
		err = rows.Scan(&id, &prefix.OrderType, &trade.Sell,
			&prefix.AccountID, &trade.Address, &prefix.ClientTime, &prefix.ServerTime,
			&prefix.Commit, (*dbCoins)(&trade.Coins),
			&trade.Quantity, &rate, &tif, &status, &trade.FillAmt)
		if err != nil {
			return nil, nil, err
		}
		prefix.BaseAsset, prefix.QuoteAsset = base, quote

		var ord order.Order
		switch prefix.OrderType {
		case order.LimitOrderType:
			ord = &order.LimitOrder{
				P:     prefix,
				T:     *trade.Copy(),
				Rate:  rate,
				Force: tif,
			}
		case order.MarketOrderType:
			ord = &order.MarketOrder{
				P: prefix,
				T: *trade.Copy(),
			}
		default:
			log.Errorf("userOrdersFromTable: encountered unexpected order type %v",
				prefix.OrderType)
			continue
		}

		orders = append(orders, ord)
		statuses = append(statuses, status)
	}

	if err = rows.Err(); err != nil {
		return nil, nil, err
	}

	return orders, statuses, nil
}

func orderForCommit(ctx context.Context, dbe *sql.DB, dbName, marketSchema string, commit order.Commitment) (bool, order.OrderID, error) {
	var zeroOrderID order.OrderID

	execCheckOrderStmt := func(stmt string) (bool, order.OrderID, error) {
		var oid order.OrderID
		err := dbe.QueryRowContext(ctx, stmt, commit).Scan(&oid)
		if err == nil {
			return true, oid, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, zeroOrderID, err
		}
		// sql.ErrNoRows
		return false, zeroOrderID, nil
	}

	checkTradeOrders := func(active bool) (bool, order.OrderID, error) {
		fullTable := fullOrderTableName(dbName, marketSchema, active)
		stmt := fmt.Sprintf(internal.SelectOrderByCommit, fullTable)
		return execCheckOrderStmt(stmt)
	}

	checkCancelOrders := func(active bool) (bool, order.OrderID, error) {
		fullTable := fullCancelOrderTableName(dbName, marketSchema, active)
		stmt := fmt.Sprintf(internal.SelectOrderByCommit, fullTable)
		return execCheckOrderStmt(stmt)
	}

	// Check active then archived cancel and trade orders.
	for _, active := range []bool{true, false} {
		// Trade orders.
		found, oid, err := checkTradeOrders(active)
		if found || err != nil {
			return found, oid, err
		}

		// Cancel orders.
		found, oid, err = checkCancelOrders(active)
		if found || err != nil {
			return found, oid, err
		}
	}
	return false, zeroOrderID, nil
}

func storeLimitOrder(dbe sqlExecutor, tableName string, lo *order.LimitOrder, status pgOrderStatus, epochIdx, epochDur int64) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, lo.ID(), lo.Type(), lo.Sell, lo.AccountID,
		lo.Address, lo.ClientTime, lo.ServerTime, lo.Commit, dbCoins(lo.Coins),
		lo.Quantity, lo.Rate, lo.Force, status, lo.Filled(), epochIdx, epochDur)
}

func storeMarketOrder(dbe sqlExecutor, tableName string, mo *order.MarketOrder, status pgOrderStatus, epochIdx, epochDur int64) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertOrder, tableName)
	return sqlExec(dbe, stmt, mo.ID(), mo.Type(), mo.Sell, mo.AccountID,
		mo.Address, mo.ClientTime, mo.ServerTime, mo.Commit, dbCoins(mo.Coins),
		mo.Quantity, 0, order.ImmediateTiF, status, mo.Filled(), epochIdx, epochDur)
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

func storeCancelOrder(dbe sqlExecutor, tableName string, co *order.CancelOrder, status pgOrderStatus, epochIdx, epochDur int64) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertCancelOrder, tableName)
	return sqlExec(dbe, stmt, co.ID(), co.AccountID, co.ClientTime,
		co.ServerTime, co.Commit, co.TargetOrderID, status, epochIdx, epochDur)
}

// loadCancelOrderFromTable does NOT set BaseAsset and QuoteAsset!
func loadCancelOrderFromTable(dbe *sql.DB, fullTable string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectCancelOrder, fullTable)

	var co order.CancelOrder
	var id order.OrderID
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&id, &co.AccountID, &co.ClientTime,
		&co.ServerTime, &co.Commit, &co.TargetOrderID, &status)
	if err != nil {
		return nil, orderStatusUnknown, err
	}

	co.OrderType = order.CancelOrderType

	return &co, status, nil
}

// loadCancelOrder does NOT set BaseAsset and QuoteAsset!
func loadCancelOrder(dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullCancelOrderTableName(dbName, marketSchema, true)
	co, status, err := loadCancelOrderFromTable(dbe, fullTable, oid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	// try archived orders next
	case err == nil:
		// found
		return co, status, nil
	default:
		// query error
		return co, orderStatusUnknown, err
	}

	// Search archived orders.
	fullTable = fullCancelOrderTableName(dbName, marketSchema, false)
	co, status, err = loadCancelOrderFromTable(dbe, fullTable, oid)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, orderStatusUnknown, db.ArchiveError{Code: db.ErrUnknownOrder}
	case err == nil:
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
	return orderStatusUnknown, db.ArchiveError{Code: db.ErrUnknownOrder}
}

func findCancelOrder(dbe *sql.DB, oid order.OrderID, dbName, marketSchema string, active bool) (bool, pgOrderStatus, error) {
	fullTable := fullCancelOrderTableName(dbName, marketSchema, active)
	stmt := fmt.Sprintf(internal.CancelOrderStatus, fullTable)
	var status pgOrderStatus
	err := dbe.QueryRow(stmt, oid).Scan(&status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, orderStatusUnknown, nil
	case err == nil:
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
