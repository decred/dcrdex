// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/meshevents"
)

// ApplyMarketStartedEvent updates the market lifecycle, revokes the listed
// booked orders and all abandoned epoch trades, and marks abandoned cancel
// orders as failed. It returns the lifecycle and event log entry committed
// with these changes.
func (a *Archiver) ApplyMarketStartedEvent(ctx context.Context, meta *db.EventLogMeta, update *db.MarketStartedUpdate) (*db.MarketStartedApplyResult, error) {
	txData, err := update.EventTxData()
	if err != nil {
		return nil, err
	}

	result := new(db.MarketStartedApplyResult)
	logEntry, err := a.applyEventTx(ctx, meta, meshevents.EventKindMarketStarted, txData, func(tx *sql.Tx) error {
		lifecycle, err := a.applyMarketStartedLifecycleTx(tx, update)
		if err != nil {
			return err
		}
		result.Lifecycle = lifecycle
		for _, revoke := range update.BookedRevokes {
			ord := revoke.Order
			oid := ord.ID()
			if _, err := a.revokeBookedOrderByID(tx, oid, ord.User(), update.Base, update.Quote, true, update.RevocationTime); err != nil {
				return fmt.Errorf("startup booked revoke %v: %w", oid, err)
			}
		}
		return a.applyMarketStartedEpochRevokesTx(tx, update)
	})
	if err != nil {
		return nil, err
	}
	result.Log = logEntry
	return result, nil
}

// applyMarketStartedEpochRevokesTx revokes the epoch-status orders listed in
// a market_started event. The revoke set must exactly match the market's
// active epoch orders.
func (a *Archiver) applyMarketStartedEpochRevokesTx(tx *sql.Tx, update *db.MarketStartedUpdate) error {
	activeIDs, err := a.activeEpochOrderIDsTx(tx, update.Base, update.Quote)
	if err != nil {
		return err
	}
	revokeIDs := make([]order.OrderID, 0, len(update.EpochRevokes))
	for _, revoke := range update.EpochRevokes {
		revokeIDs = append(revokeIDs, revoke.Order.ID())
	}
	sortOrderIDs(activeIDs)
	sortOrderIDs(revokeIDs)
	if !slices.Equal(activeIDs, revokeIDs) {
		return fmt.Errorf("market_started epoch revoke set does not match the %d active epoch orders for market %s",
			len(activeIDs), update.Market)
	}
	for _, revoke := range update.EpochRevokes {
		ord := revoke.Order
		oid := ord.ID()
		if ord.Type() == order.CancelOrderType {
			if err := a.updateOrderStatus(tx, ord, orderStatusFailed); err != nil {
				return fmt.Errorf("startup epoch cancel failure %v: %w", oid, err)
			}
			continue
		}
		if _, err := a.revokeOrder(tx, ord, true, update.RevocationTime); err != nil {
			return fmt.Errorf("startup epoch revoke %v: %w", oid, err)
		}
	}
	return nil
}

// ApplyOrdersRevokedEvent revokes booked orders, records their generated
// cancel orders, and records non-penalizing reputation outcomes.
func (a *Archiver) ApplyOrdersRevokedEvent(ctx context.Context, meta *db.EventLogMeta, policy *db.ReputationOutcomePolicy, update *db.OrdersRevokedUpdate) (*db.EventLogEntry, error) {
	if err := validateOrdersRevokedUpdate(update); err != nil {
		return nil, err
	}
	txData, err := update.EventTxData()
	if err != nil {
		return nil, err
	}

	return a.applyRepEventTx(ctx, meta, meshevents.EventKindOrdersRevoked, txData, policy, func(tx *sql.Tx, repUpdates *reputationOutcomeBatch) error {
		for _, lo := range update.Orders {
			oid, user := lo.ID(), lo.User()
			cancelID, err := a.revokeBookedOrderByID(tx, oid, user, lo.Base(), lo.Quote(), false, update.RevokeTime)
			if err != nil {
				return fmt.Errorf("orders_revoked revoke %v: %w", oid, err)
			}
			// Server revocations do not count as user cancellations.
			repUpdates.orders = append(repUpdates.orders, &reputationOrderOutcome{
				user: user,
				oid:  cancelID,
			})
		}
		return nil
	})
}

func validateOrdersRevokedUpdate(update *db.OrdersRevokedUpdate) error {
	if update == nil {
		return fmt.Errorf("nil orders revoked update")
	}
	if !meshevents.ValidOrderRevokeReason(update.Reason) {
		return fmt.Errorf("invalid order revoke reason %d", update.Reason)
	}
	if update.RevokeTime.IsZero() {
		return fmt.Errorf("empty orders_revoked revoke time")
	}
	if len(update.Orders) == 0 {
		return fmt.Errorf("empty orders_revoked order list")
	}
	return nil
}

// ApplyEpochProcessedEvent records an epoch's preimage results, order changes,
// matches, and reputation outcomes, and advances the last processed epoch.
func (a *Archiver) ApplyEpochProcessedEvent(ctx context.Context, meta *db.EventLogMeta, policy *db.ReputationOutcomePolicy, update *db.EpochProcessedUpdate) (*db.EventLogEntry, error) {
	txData, err := update.EventTxData()
	if err != nil {
		return nil, err
	}

	marketName, err := dex.MarketName(update.Epoch.MktBase, update.Epoch.MktQuote)
	if err != nil {
		return nil, err
	}
	return a.applyRepEventTx(ctx, meta, meshevents.EventKindEpochProcessed, txData, policy, func(tx *sql.Tx, repUpdates *reputationOutcomeBatch) error {
		if err := a.applyEpochProcessedLifecycleTx(tx, marketName, update.Epoch.Idx, update.Epoch.Dur); err != nil {
			return err
		}
		if err := a.applyEpochPreimagesTx(tx, update, repUpdates); err != nil {
			return err
		}
		if err := a.insertEpoch(tx, update.Epoch); err != nil {
			return err
		}
		if err := a.applyEpochOrderUpdatesTx(tx, update, policy, repUpdates); err != nil {
			return err
		}

		for _, match := range update.Matches {
			matchesTableName, err := a.matchTableName(match)
			if err != nil {
				return err
			}
			n, err := upsertMatch(tx, matchesTableName, match)
			if err != nil {
				a.fatalBackendErr(err)
				return err
			}
			if n != 1 {
				return fmt.Errorf("upsertMatch: updated %d rows, expected 1", n)
			}
		}

		// Make sure no orders from the epoch were left in epoch status.
		stranded, err := a.unprocessedEpochOrderIDsTx(tx, update.Epoch.MktBase, update.Epoch.MktQuote,
			update.Epoch.Idx, update.Epoch.Dur)
		if err != nil {
			return err
		}
		if n := len(stranded); n > 0 {
			return fmt.Errorf("epoch_processed for epoch %d leaves %d orders in epoch status",
				update.Epoch.Idx, n)
		}

		return nil
	})
}

// applyEpochPreimagesTx stores revealed preimages and revokes orders with missing
// preimages, collecting the corresponding reputation outcomes.
func (a *Archiver) applyEpochPreimagesTx(tx *sql.Tx, update *db.EpochProcessedUpdate, repUpdates *reputationOutcomeBatch) error {
	for _, miss := range update.Misses {
		cancelID, err := a.revokeOrder(tx, miss.Order, false, miss.RevokeTime)
		if err != nil {
			return err
		}
		repUpdates.orders = append(repUpdates.orders, &reputationOrderOutcome{
			user: miss.Order.User(),
			oid:  cancelID,
		})
		repUpdates.preimages = append(repUpdates.preimages, &reputationPreimageOutcome{
			user: miss.Order.User(),
			oid:  miss.Order.ID(),
			miss: true,
		})
	}
	for _, reveal := range update.Reveals {
		if err := a.storePreimage(tx, reveal.Order, reveal.Preimage); err != nil {
			return err
		}
		repUpdates.preimages = append(repUpdates.preimages, &reputationPreimageOutcome{
			user: reveal.Order.User(),
			oid:  reveal.Order.ID(),
		})
	}
	return nil
}

// applyEpochOrderUpdatesTx records order statuses and fills after matching,
// collecting reputation outcomes for successful cancellations.
func (a *Archiver) applyEpochOrderUpdatesTx(tx *sql.Tx, update *db.EpochProcessedUpdate, policy *db.ReputationOutcomePolicy, repUpdates *reputationOutcomeBatch) error {
	for _, lo := range update.TradesBooked {
		if err := a.updateOrderStatus(tx, lo, orderStatusBooked); err != nil {
			return err
		}
	}
	for _, lo := range update.TradesPartial {
		if err := a.updateOrderFilledByID(tx, lo.ID(), lo.Base(), lo.Quote(), int64(lo.Trade().Filled())); err != nil {
			return err
		}
	}
	for _, ord := range update.TradesCompleted {
		if err := a.updateOrderStatus(tx, ord, orderStatusExecuted); err != nil {
			return err
		}
	}
	for _, lo := range update.TradesCanceled {
		if err := a.updateOrderStatus(tx, lo, orderStatusCanceled); err != nil {
			return err
		}
	}
	for _, ord := range update.TradesFailed {
		if err := a.updateOrderStatus(tx, ord, orderStatusExecuted); err != nil {
			return err
		}
	}
	for _, co := range update.CancelsFailed {
		if err := a.updateOrderStatus(tx, co, orderStatusFailed); err != nil {
			return err
		}
	}
	for _, co := range update.CancelsExecuted {
		epochGap, err := a.cancelOrderEpochGap(tx, co.ID(), co.Base(), co.Quote())
		if err != nil {
			return err
		}
		if err := a.updateOrderStatus(tx, co, orderStatusExecuted); err != nil {
			return err
		}
		penalizedCancel := policy != nil && epochGap >= 0 && epochGap < policy.FreeCancelThreshold
		repUpdates.orders = append(repUpdates.orders, &reputationOrderOutcome{
			user:            co.User(),
			oid:             co.ID(),
			penalizedCancel: penalizedCancel,
		})
	}
	return nil
}
