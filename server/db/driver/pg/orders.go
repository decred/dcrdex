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
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"decred.org/dcrdex/server/meshevents"
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
func (coins *dbCoins) Scan(src any) error {
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
	return a.order(context.Background(), oid, base, quote)
}

func (a *Archiver) order(ctx context.Context, oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, order.OrderStatusUnknown, err
	}

	// Since order type is unknown:
	// - try to load from orders table, which includes market and limit orders
	// - if found, coerce into the correct order type and return
	// - if not found, try loading a cancel order with this oid
	var errA db.ArchiveError
	ord, status, err := loadTrade(ctx, a.db, a.dbName, marketSchema, oid)
	if errors.As(err, &errA) {
		if errA.Code != db.ErrUnknownOrder {
			return nil, order.OrderStatusUnknown, err
		}
		// Try the cancel orders.
		var co *order.CancelOrder
		co, status, err = loadCancelOrder(ctx, a.db, a.dbName, marketSchema, oid)
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

// ApplyOrderAcceptedEvent stores an accepted order with epoch status and
// appends its event log entry in the same transaction.
func (a *Archiver) ApplyOrderAcceptedEvent(ctx context.Context, meta *db.EventLogMeta, update *db.OrderAcceptedUpdate) (*db.EventLogEntry, error) {
	txData, err := update.EventTxData()
	if err != nil {
		return nil, err
	}
	ord := update.Order
	marketSchema, err := a.marketSchema(ord.Base(), ord.Quote())
	if err != nil {
		return nil, err
	}

	return a.applyEventTx(ctx, meta, meshevents.EventKindOrderAccepted, txData, func(tx *sql.Tx) error {
		commit := ord.Commitment()
		for schema := range a.markets {
			found, previousID, err := orderForCommit(ctx, tx, a.dbName, schema, commit)
			if err != nil {
				return err
			}
			if !found {
				continue
			}
			if previousID == ord.ID() {
				// Skip inserting the existing order, but still append the event log entry.
				return nil
			}
			return db.ArchiveError{
				Code: db.ErrReusedCommit,
				Detail: fmt.Sprintf("order %v reuses commit %v from previous order %v",
					ord.UID(), commit, previousID),
			}
		}

		mkt := a.markets[marketSchema]
		if mkt == nil {
			return fmt.Errorf("unknown market schema %s", marketSchema)
		}
		if err := a.checkOrderAcceptanceTx(tx, mkt.Name, update.EpochIdx, update.EpochDur, ord.Time()); err != nil {
			return err
		}

		var rowsAffected int64
		var storeErr error
		switch ord := ord.(type) {
		case *order.CancelOrder:
			tableName := fullCancelOrderTableName(a.dbName, marketSchema, true)
			rowsAffected, storeErr = storeCancelOrder(tx, tableName, ord, orderStatusEpoch, update.EpochIdx, update.EpochDur, update.EpochGap)
		case *order.MarketOrder:
			tableName := fullOrderTableName(a.dbName, marketSchema, true)
			rowsAffected, storeErr = storeMarketOrder(tx, tableName, ord, orderStatusEpoch, update.EpochIdx, update.EpochDur)
		case *order.LimitOrder:
			tableName := fullOrderTableName(a.dbName, marketSchema, true)
			rowsAffected, storeErr = storeLimitOrder(tx, tableName, ord, orderStatusEpoch, update.EpochIdx, update.EpochDur)
		default:
			return fmt.Errorf("unsupported accepted order type %T", ord)
		}
		if storeErr != nil {
			if ctx.Err() == nil {
				a.fatalBackendErr(storeErr)
			}
			return fmt.Errorf("failed to store accepted order %v: %w", ord.UID(), storeErr)
		}
		if rowsAffected != 1 {
			return fmt.Errorf("failed to store order %v: %d rows affected, expected 1", ord.UID(), rowsAffected)
		}
		return nil
	})
}

// ApplySuspendedCancelEvent records an executed cancel, marks its booked
// target as canceled, and stores the cancellation match.
func (a *Archiver) ApplySuspendedCancelEvent(ctx context.Context, meta *db.EventLogMeta, update *db.SuspendedCancelUpdate) (*db.SuspendedCancelApplyResult, error) {
	txData, err := update.EventTxData()
	if err != nil {
		return nil, err
	}
	if err := a.validateLifecycleMarket(update.Market, update.Base, update.Quote); err != nil {
		return nil, err
	}
	result := &db.SuspendedCancelApplyResult{Cancel: update.Cancel}
	logEntry, err := a.applyEventTx(ctx, meta, meshevents.EventKindSuspendedCancel, txData, func(tx *sql.Tx) error {
		target, match, err := a.applySuspendedCancelTx(ctx, tx, update)
		if err != nil {
			return err
		}
		result.TargetOrder = target
		result.Match = match
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.Log = logEntry
	return result, nil
}

// applySuspendedCancelTx cancels a booked order in a suspended market and records
// the executed cancel and match. It returns the stored target and derived match.
func (a *Archiver) applySuspendedCancelTx(ctx context.Context, tx *sql.Tx, update *db.SuspendedCancelUpdate) (*order.LimitOrder, *order.Match, error) {
	lifecycle, err := a.marketLifecycleForUpdate(tx, update.Market)
	if err != nil {
		return nil, nil, err
	}
	if lifecycle == nil || lifecycle.State != db.MarketStateSuspended {
		return nil, nil, fmt.Errorf("suspended_cancel requires suspended lifecycle for market %s", update.Market)
	}
	if update.Cancel.Base() != update.Base || update.Cancel.Quote() != update.Quote {
		return nil, nil, fmt.Errorf("suspended_cancel market mismatch")
	}
	if update.Cancel.TargetOrderID != update.TargetOrderID {
		return nil, nil, fmt.Errorf("suspended_cancel target mismatch")
	}
	if update.Cancel.AccountID != update.TargetAccount {
		return nil, nil, fmt.Errorf("suspended_cancel account mismatch")
	}
	marketSchema, err := a.marketSchema(update.Base, update.Quote)
	if err != nil {
		return nil, nil, err
	}
	tableName := fullOrderTableName(a.dbName, marketSchema, orderStatusBooked.active())
	ord, status, err := loadTradeFromTable(ctx, tx, tableName, update.TargetOrderID)
	if err != nil {
		return nil, nil, err
	}
	if status != orderStatusBooked {
		return nil, nil, fmt.Errorf("order %v has status %v, want booked", update.TargetOrderID, status)
	}
	target, ok := ord.(*order.LimitOrder)
	if !ok || target.Force != order.StandingTiF {
		return nil, nil, fmt.Errorf("order %v is not a booked standing limit order", update.TargetOrderID)
	}
	target.BaseAsset = update.Base
	target.QuoteAsset = update.Quote
	if target.AccountID != update.TargetAccount || target.Sell != update.TargetSell {
		return nil, nil, fmt.Errorf("suspended_cancel target order mismatch")
	}
	// Derive the match from the stored target to check the supplied match
	// against its current remaining quantity.
	expectedMatch := newSuspendedCancelMatch(update.Cancel, target, update.EpochIdx, update.EpochDur,
		update.FeeRateBase, update.FeeRateQuote)
	if err := validateSuspendedCancelMatch(update.Match, expectedMatch); err != nil {
		return nil, nil, err
	}
	if found, oid, err := a.orderWithCommit(ctx, tx, update.Cancel.Commitment()); err != nil {
		return nil, nil, err
	} else if found {
		return nil, nil, fmt.Errorf("suspended_cancel order %v reuses commitment from %v", update.Cancel.ID(), oid)
	}
	cancelTable := fullCancelOrderTableName(a.dbName, marketSchema, orderStatusExecuted.active())
	n, err := storeCancelOrder(tx, cancelTable, update.Cancel, orderStatusExecuted, update.EpochIdx, update.EpochDur, db.EpochGapNA)
	if err != nil {
		return nil, nil, err
	}
	if n != 1 {
		return nil, nil, fmt.Errorf("stored suspended cancel rows = %d, want 1", n)
	}
	if err := a.updateOrderStatus(tx, target, orderStatusCanceled); err != nil {
		return nil, nil, err
	}
	matchesTableName := fullMatchesTableName(a.dbName, marketSchema)
	n, err = upsertMatch(tx, matchesTableName, expectedMatch)
	if err != nil {
		return nil, nil, err
	}
	if n != 1 {
		return nil, nil, fmt.Errorf("upsertMatch: updated %d rows, expected 1", n)
	}
	return target, expectedMatch, nil
}

func newSuspendedCancelMatch(cancel *order.CancelOrder, target *order.LimitOrder, epochIdx, epochDur int64, feeRateBase, feeRateQuote uint64) *order.Match {
	return &order.Match{
		Taker:        cancel,
		Maker:        target,
		Quantity:     target.Remaining(),
		Rate:         target.Rate,
		Epoch:        order.EpochID{Idx: uint64(epochIdx), Dur: uint64(epochDur)},
		FeeRateBase:  feeRateBase,
		FeeRateQuote: feeRateQuote,
		Status:       order.MatchComplete,
	}
}

func validateSuspendedCancelMatch(supplied, expected *order.Match) error {
	if supplied == nil {
		return fmt.Errorf("nil suspended cancel match")
	}
	if supplied.Taker == nil || supplied.Maker == nil {
		return fmt.Errorf("nil suspended cancel match order")
	}
	if supplied.ID() != expected.ID() {
		return fmt.Errorf("suspended_cancel match ID mismatch")
	}
	if supplied.Taker.ID() != expected.Taker.ID() || supplied.Maker.ID() != expected.Maker.ID() {
		return fmt.Errorf("suspended_cancel match order mismatch")
	}
	if supplied.Quantity != expected.Quantity || supplied.Rate != expected.Rate {
		return fmt.Errorf("suspended_cancel match quantity/rate mismatch")
	}
	if supplied.Epoch != expected.Epoch {
		return fmt.Errorf("suspended_cancel match epoch mismatch")
	}
	if supplied.FeeRateBase != expected.FeeRateBase || supplied.FeeRateQuote != expected.FeeRateQuote {
		return fmt.Errorf("suspended_cancel match fee-rate mismatch")
	}
	if supplied.Status != order.MatchComplete {
		return fmt.Errorf("suspended_cancel match status %d, want %d", supplied.Status, order.MatchComplete)
	}
	return nil
}

// ApplyAdvanceEpochEvent advances the market's active epoch, or enters
// the draining state when its final epoch closes.
func (a *Archiver) ApplyAdvanceEpochEvent(ctx context.Context, meta *db.EventLogMeta, event *meshevents.AdvanceEpochEvent) (*db.EventLogEntry, error) {
	txData, err := event.EventTxData()
	if err != nil {
		return nil, err
	}
	return a.applyEventTx(ctx, meta, meshevents.EventKindAdvanceEpoch, txData, func(tx *sql.Tx) error {
		return a.applyAdvanceEpochLifecycleTx(tx, event)
	})
}

// NewEpochOrder stores the given order with epoch status.
func (a *Archiver) NewEpochOrder(ord order.Order, epochIdx, epochDur int64, epochGap int32) error {
	return a.storeOrder(a.db, ord, epochIdx, epochDur, epochGap, orderStatusEpoch)
}

// NewArchivedCancel stores a cancel order directly in the executed state. This
// is used for orders that are canceled when the market is suspended, and therefore
// do not need to be matched.
func (a *Archiver) NewArchivedCancel(ord *order.CancelOrder, epochID, epochDur int64) error {
	marketSchema, err := a.marketSchema(ord.Base(), ord.Quote())
	if err != nil {
		return err
	}
	status := orderStatusExecuted
	tableName := fullCancelOrderTableName(a.dbName, marketSchema, status.active())
	N, err := storeCancelOrder(a.db, tableName, ord, status, epochID, epochDur, db.EpochGapNA)
	if err != nil {
		a.fatalBackendErr(err)
		return fmt.Errorf("storeCancelOrder failed: %w", err)
	}
	if N != 1 {
		err = fmt.Errorf("failed to store order %v: %d rows affected, expected 1",
			ord.UID(), N)
		return err
	}

	return nil
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

// CancelOrder is a temporary stub for the legacy swapper. Remove it when
// the swapper no longer calls it; market events now persist cancellations.
func (a *Archiver) CancelOrder(*order.LimitOrder) error {
	return nil
}

// RevokeOrder updates an Order with revoked status, which is used for
// DEX-revoked orders rather than orders matched with a user's CancelOrder. If
// the order does not exist in the Archiver, RevokeOrder returns
// ErrUnknownOrder. This may change orders with status executed to revoked,
// which may be unexpected.
func (a *Archiver) RevokeOrder(ord order.Order) (cancelID order.OrderID, timeStamp time.Time, err error) {
	timeStamp = time.Now().Truncate(time.Millisecond).UTC()
	cancelID, err = a.revokeOrder(a.db, ord, false, timeStamp)
	return
}

// RevokeOrderUncounted is like RevokeOrder except that the generated cancel
// order will not be counted against the user. i.e. ExecutedCancelsForUser
// should not return the cancel orders created this way.
func (a *Archiver) RevokeOrderUncounted(ord order.Order) (cancelID order.OrderID, timeStamp time.Time, err error) {
	timeStamp = time.Now().Truncate(time.Millisecond).UTC()
	cancelID, err = a.revokeOrder(a.db, ord, true, timeStamp)
	return
}

const (
	exemptEpochIdx  int64 = -1
	countedEpochIdx int64 = 0
	dummyEpochDur   int64 = 1 // for idx*duration math
)

func (a *Archiver) revokeOrder(dbe sqlQueryExecutor, ord order.Order, exempt bool, timeStamp time.Time) (order.OrderID, error) {
	if err := a.updateOrderStatus(dbe, ord, orderStatusRevoked); err != nil {
		return order.OrderID{}, err
	}

	return a.storeRevocationCancel(dbe, ord.ID(), ord.User(), ord.Base(), ord.Quote(), exempt, timeStamp)
}

func (a *Archiver) revokeBookedOrderByID(dbe sqlQueryExecutor, oid order.OrderID, user account.AccountID, base, quote uint32, exempt bool, timeStamp time.Time) (order.OrderID, error) {
	status, ordType, _, err := a.orderStatusByID(dbe, oid, base, quote)
	if err != nil {
		return order.OrderID{}, err
	}
	if status != orderStatusBooked || ordType != order.LimitOrderType {
		return order.OrderID{}, fmt.Errorf("cannot revoke non-booked order %v in status %v with type %v", oid, status, ordType)
	}

	if err := a.updateOrderStatusByID(dbe, oid, base, quote, orderStatusRevoked, -1); err != nil {
		return order.OrderID{}, err
	}

	return a.storeRevocationCancel(dbe, oid, user, base, quote, exempt, timeStamp)
}

func (a *Archiver) storeRevocationCancel(dbe sqlQueryExecutor, oid order.OrderID, user account.AccountID, base, quote uint32, exempt bool, timeStamp time.Time) (order.OrderID, error) {
	timeStamp = timeStamp.Truncate(time.Millisecond).UTC()

	// Record the revocation with a server-generated cancel order.
	co := makePseudoCancel(oid, user, base, quote, timeStamp)
	epochIdx := countedEpochIdx
	if exempt {
		epochIdx = exemptEpochIdx
	}
	err := a.storeOrder(dbe, co, epochIdx, dummyEpochDur, db.EpochGapNA, orderStatusRevoked)
	return co.ID(), err
}

func validateOrder(ord order.Order, status pgOrderStatus, mkt *dex.MarketInfo) bool {
	if status == orderStatusFailed && ord.Type() != order.CancelOrderType {
		return false
	}
	return db.ValidateOrder(ord, pgToMarketStatus(status), mkt)
}

func (a *Archiver) storeOrder(dbe sqlQueryExecutor, ord order.Order, epochIdx, epochDur int64, epochGap int32, status pgOrderStatus) error {
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

	// Reject commitments already present in active orders.
	// Commitments from archived orders may be reused.
	commit := ord.Commitment()
	found, prevOid, err := a.orderWithCommit(a.ctx, dbe, commit) // no query timeouts in storeOrder, only explicit cancellation
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
		N, err = storeCancelOrder(dbe, tableName, ot, status, epochIdx, epochDur, epochGap)
		if err != nil {
			a.fatalBackendErr(err)
			return fmt.Errorf("storeCancelOrder failed: %w", err)
		}
	case *order.MarketOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeMarketOrder(dbe, tableName, ot, status, epochIdx, epochDur)
		if err != nil {
			a.fatalBackendErr(err)
			return fmt.Errorf("storeMarketOrder failed: %w", err)
		}
	case *order.LimitOrder:
		tableName := fullOrderTableName(a.dbName, marketSchema, status.active())
		N, err = storeLimitOrder(dbe, tableName, ot, status, epochIdx, epochDur)
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
	return a.orderTableNameWithExecutor(a.db, ord)
}

func (a *Archiver) orderTableNameWithExecutor(dbe sqlQueryer, ord order.Order) (string, pgOrderStatus, error) {
	status, orderType, _, err := a.orderStatus(dbe, ord)
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

func (a *Archiver) storePreimage(dbe sqlQueryExecutor, ord order.Order, pi order.Preimage) error {
	tableName, status, err := a.orderTableNameWithExecutor(dbe, ord)
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
	N, err := sqlExec(dbe, stmt, pi, ord.ID())
	if err != nil {
		a.fatalBackendErr(err)
		return err
	}
	if N != 1 {
		return fmt.Errorf("failed to update 1 order's preimage, updated %d", N)
	}
	return nil
}

// StorePreimage stores the preimage associated with an existing order.
func (a *Archiver) StorePreimage(ord order.Order, pi order.Preimage) error {
	return a.storePreimage(a.db, ord, pi)
}

// SetOrderCompleteTime sets the successful swap completion time for an existing
// order. It is an error if the order is not in executed status.
func (a *Archiver) SetOrderCompleteTime(ord order.Order, compTimeMs int64) error {
	status, orderType, _, err := a.orderStatus(a.db, ord)
	if err != nil {
		return err
	}

	if status != orderStatusExecuted { // complete_time is only set for executed orders, not canceled or revoked
		log.Warnf("Attempting to set swap completion time for order %v in status %v, not executed",
			ord.UID(), status)
		return db.ArchiveError{
			Code: db.ErrOrderNotExecuted,
			Detail: fmt.Sprintf("unable to set completed time for order %v in status %v, not executed",
				ord.UID(), status),
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

	for schema := range a.markets {
		tableName := fullOrderTableName(a.dbName, schema, false) // NOT active table
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

func completedUserOrders(ctx context.Context, dbe sqlQueryer, tableName string, aid account.AccountID, N int) (oids []orderCompStamped, err error) {
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

		results, err := preimageStats(ctx, a.db, stmt, user, lastN)
		outcomes = append(outcomes, results...)
		return err
	}

	for schema := range a.markets {
		// archived trade orders
		stmt := fmt.Sprintf(internal.PreimageResultsLastN, fullOrderTableName(a.dbName, schema, false))
		if err := queryOutcomes(stmt); err != nil {
			return nil, err
		}

		// archived cancel orders
		stmt = fmt.Sprintf(internal.CancelPreimageResultsLastN, fullCancelOrderTableName(a.dbName, schema, false))
		if err := queryOutcomes(stmt); err != nil {
			return nil, err
		}
	}

	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[i].Time < outcomes[j].Time // ascending
	})
	if len(outcomes) > lastN {
		outcomes = outcomes[len(outcomes)-lastN:]
	}

	return outcomes, nil
}

// preimageStats reads preimage results from one order table.
func preimageStats(ctx context.Context, dbe sqlQueryer, stmt string, user account.AccountID, lastN int) ([]*db.PreimageResult, error) {
	rows, err := dbe.QueryContext(ctx, stmt, user, lastN, orderStatusRevoked)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var outcomes []*db.PreimageResult
	for rows.Next() {
		var result db.PreimageResult
		if err := rows.Scan(&result.ID, &result.Miss, &result.Time); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, &result)
	}
	return outcomes, rows.Err()
}

// OrderStatusByID gets the status, type, and filled amount of the order with
// the given OrderID in the market specified by a base and quote asset. See also
// OrderStatus. If the order is not found, the error value is ErrUnknownOrder,
// and the type is order.OrderStatusUnknown.
func (a *Archiver) OrderStatusByID(oid order.OrderID, base, quote uint32) (order.OrderStatus, order.OrderType, int64, error) {
	pgStatus, orderType, filled, err := a.orderStatusByID(a.db, oid, base, quote)
	return pgToMarketStatus(pgStatus), orderType, filled, err
}

func (a *Archiver) orderStatusByID(dbe sqlQueryer, oid order.OrderID, base, quote uint32) (pgOrderStatus, order.OrderType, int64, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return orderStatusUnknown, order.UnknownOrderType, -1, err
	}
	status, orderType, filled, err := orderStatus(dbe, oid, a.dbName, marketSchema)
	if db.IsErrOrderUnknown(err) {
		status, err = cancelOrderStatus(dbe, oid, a.dbName, marketSchema)
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

func (a *Archiver) orderStatus(dbe sqlQueryer, ord order.Order) (pgOrderStatus, order.OrderType, int64, error) {
	return a.orderStatusByID(dbe, ord.ID(), ord.Base(), ord.Quote())
}

// UpdateOrderStatusByID updates the status and filled amount of the order with
// the given OrderID in the market specified by a base and quote asset. If
// filled is -1, the filled amount is unchanged. For cancel orders, the filled
// amount is ignored. OrderStatusByID is used to locate the existing order. If
// the order is not found, the error value is ErrUnknownOrder, and the type is
// market/order.OrderStatusUnknown. See also UpdateOrderStatus.
func (a *Archiver) UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status order.OrderStatus, filled int64) error {
	return a.updateOrderStatusByID(a.db, oid, base, quote, marketToPgStatus(status), filled)
}

func (a *Archiver) updateOrderStatusByID(dbe sqlQueryExecutor, oid order.OrderID, base, quote uint32, status pgOrderStatus, filled int64) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}

	currentStatus, orderType, currentFilled, err := a.orderStatusByID(dbe, oid, base, quote)
	if err != nil {
		return err
	}

	// A filled amount of -1 preserves the stored amount.
	if filled == -1 {
		filled = currentFilled
	}
	if currentStatus == status && filled == currentFilled {
		log.Tracef("Not updating order with no status or filled amount change: %v.", oid)
		return nil
	}

	movesTable := status.active() != currentStatus.active()

	if !currentStatus.active() && status.active() {
		return fmt.Errorf("Moving an order from an archived to active status: "+
			"Order %s (%s -> %s)", oid, currentStatus, status)
	}
	if !currentStatus.active() {
		log.Infof("Archived order is changing status: Order %s (%s -> %s)",
			oid, currentStatus, status)
	}

	switch orderType {
	case order.LimitOrderType, order.MarketOrderType:
		srcTableName := fullOrderTableName(a.dbName, marketSchema, currentStatus.active())
		if movesTable {
			dstTableName := fullOrderTableName(a.dbName, marketSchema, status.active())
			return a.moveOrder(dbe, oid, srcTableName, dstTableName, status, filled)
		}

		// No table move, just update the order.
		return updateOrderStatusAndFilledAmt(dbe, srcTableName, oid, status, uint64(filled))

	case order.CancelOrderType:
		srcTableName := fullCancelOrderTableName(a.dbName, marketSchema, currentStatus.active())
		if movesTable {
			dstTableName := fullCancelOrderTableName(a.dbName, marketSchema, status.active())
			return a.moveCancelOrder(dbe, oid, srcTableName, dstTableName, status)
		}

		// No table move, just update the order.
		return updateCancelOrderStatus(dbe, srcTableName, oid, status)
	default:
		return fmt.Errorf("unsupported order type: %v", orderType)
	}
}

// UpdateOrderStatus updates the status and filled amount of the given order.
// Both the market and new filled amount are determined from the Order.
// OrderStatusByID is used to locate the existing order. See also
// UpdateOrderStatusByID.
func (a *Archiver) UpdateOrderStatus(ord order.Order, status order.OrderStatus) error {
	return a.updateOrderStatus(a.db, ord, marketToPgStatus(status))
}

// updateOrderStatus sets the order's status and records its filled amount.
func (a *Archiver) updateOrderStatus(dbe sqlQueryExecutor, ord order.Order, status pgOrderStatus) error {
	var filled int64
	if ord.Type() != order.CancelOrderType {
		filled = int64(ord.Trade().Filled())
	}
	return a.updateOrderStatusByID(dbe, ord.ID(), ord.Base(), ord.Quote(), status, filled)
}

func (a *Archiver) moveOrder(dbe sqlExecutor, oid order.OrderID, srcTableName, dstTableName string, status pgOrderStatus, filled int64) error {
	// Move the order, updating status and filled amount.
	moved, err := moveOrder(dbe, srcTableName, dstTableName, oid,
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

func (a *Archiver) moveCancelOrder(dbe sqlExecutor, oid order.OrderID, srcTableName, dstTableName string, status pgOrderStatus) error {
	moved, err := moveCancelOrder(dbe, srcTableName, dstTableName, oid,
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

// updateOrderFilledByID updates the filled amount of the order
// with the given OrderID in the market specified by a base and quote asset.
// This function applies only to market and limit orders, not cancel orders.
// The order's status is used to locate the existing order. If the order is not
// found, the error value is ErrUnknownOrder.
func (a *Archiver) updateOrderFilledByID(dbe sqlQueryExecutor, oid order.OrderID, base, quote uint32, filled int64) error {
	// Locate the order.
	status, orderType, initFilled, err := a.orderStatusByID(dbe, oid, base, quote)
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
	err = updateOrderFilledAmt(dbe, tableName, oid, uint64(filled))
	if err != nil {
		a.fatalBackendErr(err) // TODO: it could have changed tables since this function is not atomic
	}
	return err
}

// UpdateOrderFilledByID updates the filled amount of the order with the given
// OrderID in the market specified by a base and quote asset. This function
// applies only to market and limit orders, not cancel orders. OrderStatusByID
// is used to locate the existing order. If the order is not found, the error
// value is ErrUnknownOrder, and the type is order.OrderStatusUnknown. See also
// UpdateOrderFilled. To also update the order status, use UpdateOrderStatusByID
// or UpdateOrderStatus.
func (a *Archiver) UpdateOrderFilledByID(oid order.OrderID, base, quote uint32, filled int64) error {
	return a.updateOrderFilledByID(a.db, oid, base, quote, filled)
}

// UpdateOrderFilled updates the filled amount of the given order. Both the
// market and new filled amount are determined from the Order. OrderStatusByID
// is used to locate the existing order. This function applies only to limit
// orders, not market or cancel orders. See also UpdateOrderFilledByID.
func (a *Archiver) UpdateOrderFilled(ord *order.LimitOrder) error {
	switch orderType := ord.Type(); orderType {
	case order.MarketOrderType, order.LimitOrderType:
	default:
		return fmt.Errorf("cannot set filled amount for order type %v", orderType)
	}
	return a.UpdateOrderFilledByID(ord.ID(), ord.Base(), ord.Quote(), int64(ord.Trade().Filled()))
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
	for schema := range a.markets {
		tableName := fullOrderTableName(a.dbName, schema, true) // active table
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

// OrdersWithCommit returns orders with the given commitment in one market.
// It includes all active orders and archived orders accepted at or after
// archivedCutoff.
func (a *Archiver) OrdersWithCommit(ctx context.Context, base, quote uint32,
	commit order.Commitment, archivedCutoff time.Time) ([]db.OrderWithStatus, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return nil, err
	}
	found, oid, err := orderForCommit(ctx, a.db, a.dbName, marketSchema, commit)
	if err != nil {
		if ctx.Err() == nil {
			a.fatalBackendErr(err)
		}
		return nil, err
	}
	var oids []order.OrderID
	if found {
		oids = append(oids, oid)
	}
	archived, err := archivedOrderIDsForCommitSince(ctx, a.db, a.dbName, marketSchema, commit, archivedCutoff)
	if err != nil {
		if ctx.Err() == nil {
			a.fatalBackendErr(err)
		}
		return nil, err
	}
	oids = append(oids, archived...)

	orders := make([]db.OrderWithStatus, 0, len(oids))
	for _, oid := range oids {
		ord, status, err := a.order(ctx, oid, base, quote)
		if err != nil {
			return nil, err
		}
		orders = append(orders, db.OrderWithStatus{Order: ord, Status: status})
	}
	return orders, nil
}

func archivedOrderIDsForCommitSince(ctx context.Context, dbe sqlQueryer, dbName, marketSchema string,
	commit order.Commitment, cutoff time.Time) ([]order.OrderID, error) {
	var oids []order.OrderID
	for _, tableName := range []string{
		fullOrderTableName(dbName, marketSchema, false),
		fullCancelOrderTableName(dbName, marketSchema, false),
	} {
		stmt := fmt.Sprintf(internal.SelectOrderByCommitSince, tableName)
		ids, err := scanOrderIDRows(dbe.QueryContext(ctx, stmt, commit, cutoff))
		if err != nil {
			return nil, err
		}
		oids = append(oids, ids...)
	}
	return oids, nil
}

// OrderWithCommit searches all markets' active trade and cancel orders for
// the given commitment.
func (a *Archiver) OrderWithCommit(ctx context.Context, commit order.Commitment) (found bool, oid order.OrderID, err error) {
	return a.orderWithCommit(ctx, a.db, commit)
}

// orderWithCommit searches all markets' active trade and cancel orders for
// the given Commitment.
func (a *Archiver) orderWithCommit(ctx context.Context, dbe sqlQueryer, commit order.Commitment) (found bool, oid order.OrderID, err error) {
	// Check all markets.
	for marketSchema := range a.markets {
		found, oid, err = orderForCommit(ctx, dbe, a.dbName, marketSchema, commit)
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

func executedCancelsForUser(ctx context.Context, dbe sqlQueryer, stmt string,
	aid account.AccountID, N int) (ords []*db.CancelRecord, err error) {

	var rows *sql.Rows
	rows, err = dbe.QueryContext(ctx, stmt, aid, orderStatusExecuted, N) // excludes orderStatusFailed
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var oid, target order.OrderID
		var execTime int64
		var epochGap int32
		err = rows.Scan(&oid, &target, &epochGap, &execTime)
		if err != nil {
			return
		}

		ords = append(ords, &db.CancelRecord{
			ID:        oid,
			TargetID:  target,
			MatchTime: execTime,
			EpochGap:  epochGap,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return
}

// revokeGeneratedCancelsForUser excludes exempt/uncounted cancels created with
// RevokeOrderUncounted or revokeOrder(..., exempt=true).
func revokeGeneratedCancelsForUser(ctx context.Context, dbe sqlQueryer, stmt string,
	aid account.AccountID, N int) (ords []*db.CancelRecord, err error) {

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

		ords = append(ords, &db.CancelRecord{
			ID:        oid,
			TargetID:  target,
			MatchTime: revokeTime.UnixMilli(),
			EpochGap:  db.EpochGapNA,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return
}

func orderStatus(dbe sqlQueryer, oid order.OrderID, dbName, marketSchema string) (pgOrderStatus, order.OrderType, int64, error) {
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

func findOrder(dbe sqlQueryer, oid order.OrderID, fullTable string) (bool, pgOrderStatus, order.OrderType, int64, error) {
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
func loadTrade(ctx context.Context, dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (order.Order, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullOrderTableName(dbName, marketSchema, true)
	ord, status, err := loadTradeFromTable(ctx, dbe, fullTable, oid)
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
	ord, status, err = loadTradeFromTable(ctx, dbe, fullTable, oid)
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
func loadTradeFromTable(ctx context.Context, dbe sqlQueryer, fullTable string, oid order.OrderID) (order.Order, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectOrder, fullTable)

	var prefix order.Prefix
	var trade order.Trade
	var id order.OrderID
	var tif order.TimeInForce
	var rate uint64
	var status pgOrderStatus
	err := dbe.QueryRowContext(ctx, stmt, oid).Scan(&id, &prefix.OrderType, &trade.Sell,
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

func orderForCommit(ctx context.Context, dbe sqlQueryer, dbName, marketSchema string, commit order.Commitment) (bool, order.OrderID, error) {
	for _, tableName := range []string{
		fullOrderTableName(dbName, marketSchema, true),
		fullCancelOrderTableName(dbName, marketSchema, true),
	} {
		stmt := fmt.Sprintf(internal.SelectOrderByCommit, tableName)
		var oid order.OrderID
		err := dbe.QueryRowContext(ctx, stmt, commit).Scan(&oid)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, order.OrderID{}, err
		}
		return true, oid, nil
	}
	return false, order.OrderID{}, nil
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

func storeCancelOrder(dbe sqlExecutor, tableName string, co *order.CancelOrder, status pgOrderStatus, epochIdx, epochDur int64, epochGap int32) (int64, error) {
	stmt := fmt.Sprintf(internal.InsertCancelOrder, tableName)
	return sqlExec(dbe, stmt, co.ID(), co.AccountID, co.ClientTime,
		co.ServerTime, co.Commit, co.TargetOrderID, status, epochIdx, epochDur, epochGap)
}

// loadCancelOrderFromTable does NOT set BaseAsset and QuoteAsset!
func loadCancelOrderFromTable(ctx context.Context, dbe *sql.DB, fullTable string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	stmt := fmt.Sprintf(internal.SelectCancelOrder, fullTable)

	var co order.CancelOrder
	var id order.OrderID
	var status pgOrderStatus
	err := dbe.QueryRowContext(ctx, stmt, oid).Scan(&id, &co.AccountID, &co.ClientTime,
		&co.ServerTime, &co.Commit, &co.TargetOrderID, &status)
	if err != nil {
		return nil, orderStatusUnknown, err
	}

	co.OrderType = order.CancelOrderType

	return &co, status, nil
}

// loadCancelOrder does NOT set BaseAsset and QuoteAsset!
func loadCancelOrder(ctx context.Context, dbe *sql.DB, dbName, marketSchema string, oid order.OrderID) (*order.CancelOrder, pgOrderStatus, error) {
	// Search active orders first.
	fullTable := fullCancelOrderTableName(dbName, marketSchema, true)
	co, status, err := loadCancelOrderFromTable(ctx, dbe, fullTable, oid)
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
	co, status, err = loadCancelOrderFromTable(ctx, dbe, fullTable, oid)
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

func cancelOrderStatus(dbe sqlQueryer, oid order.OrderID, dbName, marketSchema string) (pgOrderStatus, error) {
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

// cancelOrderEpochGap returns the number of epochs between a cancel order and
// its target order. Server-generated revocations use db.EpochGapNA.
func (a *Archiver) cancelOrderEpochGap(dbe sqlQueryer, oid order.OrderID, base, quote uint32) (int32, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return db.EpochGapNA, err
	}
	for _, active := range []bool{true, false} {
		table := fullCancelOrderTableName(a.dbName, marketSchema, active)
		stmt := fmt.Sprintf(internal.SelectCancelOrderEpochGap, table)
		var gap int32
		err := dbe.QueryRow(stmt, oid).Scan(&gap)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return db.EpochGapNA, err
		}
		return gap, nil
	}
	return db.EpochGapNA, db.ArchiveError{Code: db.ErrUnknownOrder}
}

func findCancelOrder(dbe sqlQueryer, oid order.OrderID, dbName, marketSchema string, active bool) (bool, pgOrderStatus, error) {
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
