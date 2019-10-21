// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/market/types"
	"github.com/decred/dcrdex/server/order"
)

// DEXArchivist will be composed of several different interfaces. Starting with
// OrderArchiver.
type DEXArchivist interface {
	OrderArchiver
}

// OrderArchiver is the interface required for storage and retrieval of all
// order data.
type OrderArchiver interface {
	// Order retrieves an order with the given OrderID, stored for the market
	// specified by the given base and quote assets.
	Order(oid order.OrderID, base, quote uint32) (order.Order, types.OrderStatus, error)

	// UserOrders retrieves all orders for the given account in the market
	// specified by a base and quote asset.
	UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []types.OrderStatus, error)

	// OrderStatus gets the status, ID, and filled amount of the given order.
	OrderStatus(order.Order) (types.OrderStatus, order.OrderType, int64, error)

	// NewEpochOrder stores a new order with epoch status. Such orders are
	// pending execution or insertion on a book (standing limit orders with a
	// remaining unfilled amount).
	NewEpochOrder(ord order.Order) error

	// BookOrder books the given order. If the order was already stored (i.e.
	// NewEpochOrder), it's status and filled amount are updated, otherwise it
	// is inserted. See also UpdateOrderFilled.
	BookOrder(*order.LimitOrder) error

	// ExecuteOrder puts the order into the executed state, and sets the filled
	// amount for market and limit orders. For unmatched cancel orders, use
	// FailCancelOrder instead.
	ExecuteOrder(ord order.Order) error

	// CancelOrder puts a limit order into the canceled state. Market orders
	// must use ExecuteOrder since they may not be canceled. Similarly, cancel
	// orders must use ExecuteOrder or FailCancelOrder. Orders that are
	// terminated by the DEX rather than via a cancel order are considered
	// "revoked", and RevokeOrder should be used to set this status.
	CancelOrder(*order.LimitOrder) error

	// RevokeOrder puts a limit order into the revoked state. Orders should be
	// revoked by the DEX according to policy on failed orders. For canceling an
	// order that was matched with a cancel order, use CancelOrder.
	RevokeOrder(*order.LimitOrder) error

	// FailCancelOrder puts an unmatched cancel order into the executed state.
	// For matched cancel orders, use ExecuteOrder.
	FailCancelOrder(*order.CancelOrder) error

	// UpdateOrderFilled updates the filled amount of the given order. This
	// function applies only to market and limit orders, not cancel orders.
	UpdateOrderFilled(order.Order) error

	// UpdateOrderStatus updates the status and filled amount of the given
	// order.
	UpdateOrderStatus(order.Order, types.OrderStatus) error
}

// ValidateOrder ensures that the order with the given status for the specified
// market is sensible. This function is in the database package because the
// concept of a valid order-status-market state is dependent on the semantics of
// order archival.
func ValidateOrder(ord order.Order, status types.OrderStatus, mkt *types.MarketInfo) bool {
	// Orders with status OrderStatusUnknown should never reach the database.
	if status == types.OrderStatusUnknown {
		return false
	}

	// Bad MarketInfo!
	if mkt.Base == mkt.Quote {
		panic("MarketInfo specifies market with same base and quote assets")
	}

	// Verify the order is for the intended types.
	if ord.Base() != mkt.Base || ord.Quote() != mkt.Quote {
		return false
	}

	// Each order type has different rules about status and lot size.
	switch ot := ord.(type) {
	case *order.MarketOrder:
		// Market orders OK statuses: epoch and executed (NOT booked or
		// canceled).
		switch status {
		case types.OrderStatusEpoch, types.OrderStatusExecuted:
		default:
			return false
		}

		if ot.OrderType != order.MarketOrderType {
			return false
		}

		// Market sell orders must respect lot size.
		if ot.Sell && (ot.Quantity%mkt.LotSize != 0 || ord.Remaining()%mkt.LotSize != 0) {
			return false
		}

	case *order.CancelOrder:
		// Cancel order OK statuses: epoch, and executed (NOT booked or
		// canceled).
		switch status {
		case types.OrderStatusEpoch, types.OrderStatusExecuted: // orderStatusFailed if we decide to export that
		default:
			return false
		}

		if ot.OrderType != order.CancelOrderType {
			return false
		}

		// All cancel orders must have zero and filled remaining amounts.
		if ord.Remaining() != 0 || ord.FilledAmt() != 0 {
			panic("a CancelOrder should always return 0 Remaining and FilledAmt")
		}

	case *order.LimitOrder:
		// Limit order OK statuses: epoch, booked, executed, and canceled (same
		// as market plus booked).
		switch status {
		case types.OrderStatusEpoch, types.OrderStatusExecuted, types.OrderStatusRevoked:
		case types.OrderStatusBooked, types.OrderStatusCanceled:
			// Immediate time in force limit orders may not be canceled, and may
			// not be in the order book.
			if ot.Force == order.ImmediateTiF {
				return false
			}
		default:
			return false
		}

		if ot.OrderType != order.LimitOrderType {
			return false
		}

		// All limit orders must respect lot size.
		if ot.Quantity%mkt.LotSize != 0 || ord.Remaining()%mkt.LotSize != 0 {
			return false
		}
	default:
		// cannot validate an unknown order type
		return false
	}

	return true
}

// Orders (only validated ones):
//  1. PK: Computed public order UID
//  2. All other essential and immutable order.Order data
//  3. Status (epoch, booked, executed, canceled, revoked)
//  4. Remaining/filled amount

// Epochs:
//  1. PK: epoch ID
//  2. list of order IDs and order hashes (ref orders table)
//  3. resulting shuffle order
//  4. resulting matches (ref matches table)
//  5. resulting order mods (change filled amount)
//  6. resulting book mods (insert, remove)
//  refs: orders, matches

// Matches (merged with Swaps):
//  1. The order.Match data, one row per taker/maker pair
//  2. Epoch ID
//  3. Swap status (in-progress, failed, executed executed/swapped)
// PK is ??? a unique ID used by the Market?
//  refs: orders

// Users:
//  1. The account.{User,Account} data
//  2. PK: Unique user ID (server generated)
//  3. Order IDs (or maybe just join with Orders table by UserID to get these)

// Books are in-memory, but on shutdown or other maintenance events, the books can be stored.
//  1. buy and sell orders (two differnet lists)

// Market history with *timestamped* events including:
//  1. order receipt (ref to: users table)
//  2. order validation (ref to: orders table)
//  3. order entry into an epoch (ref to: orders and epochs tables)
//  4. epoch matches made (ref to: epochs table)
//  5. book mods (insert, update, remove) (ref to: orders and epochs tables)
//  6. swap events (announce, init, etc.) (ref to: swaps table)
//
// NOTE: all other events can go to the logger
