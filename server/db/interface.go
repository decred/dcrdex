// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

// TODO
//
// Epochs:
//  1. PK: epoch ID
//  2. list of order IDs and order hashes (ref orders table)
//  3. resulting shuffle order
//  4. resulting matches (ref matches table)
//  5. resulting order mods (change filled amount)
//  6. resulting book mods (insert, remove)
//  refs: orders, matches
//
// Books are in-memory, but on shutdown or other maintenance events, the books
// can be stored to facilitate restart without clearing the books.
//  1. buy and sell orders (two differnet lists)
//
// For dex/market activity, consider a noSQL DB for structured logging. Market
// activity with *timestamped* events, possibly including:
//  1. order receipt
//  2. order validation
//  3. order entry into an epoch
//  4. matches made
//  5. book mods (insert, update, remove)
//  6. swap events (announce, init, etc.)
//
// NOTE: all other events can go to the logger

import (
	"context"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// DEXArchivist will be composed of several different interfaces. Starting with
// OrderArchiver.
type DEXArchivist interface {
	// LastErr should returns any fatal or unexpected error encountered by the
	// archivist backend. This may be used to check if the database had an
	// unrecoverable error (disconnect, etc.).
	LastErr() error

	OrderArchiver
	AccountArchiver
	MatchArchiver
}

// OrderArchiver is the interface required for storage and retrieval of all
// order data.
type OrderArchiver interface {
	// Order retrieves an order with the given OrderID, stored for the market
	// specified by the given base and quote assets.
	Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error)

	ActiveOrderCoins(base, quote uint32) (baseCoins, quoteCoins map[order.OrderID][]order.CoinID, err error)

	// UserOrders retrieves all orders for the given account in the market
	// specified by a base and quote asset.
	UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []order.OrderStatus, error)

	// OrderStatus gets the status, ID, and filled amount of the given order.
	OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error)

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
	UpdateOrderStatus(order.Order, order.OrderStatus) error
}

// AccountArchiver is the interface required for storage and retrieval of all
// account data.
type AccountArchiver interface {
	// CloseAccount closes an account for violating a rule of community conduct.
	CloseAccount(account.AccountID, account.Rule)

	// Account retrieves the account information for the specified account ID.
	// The registration fee payment status is returned as well. A nil pointer
	// will be returned for unknown or closed accounts.
	Account(account.AccountID) (acct *account.Account, paid bool)

	// ActiveMatches will be needed, but does not belong in this archiver.
	// // ActiveMatches retrieves the current active matches for an account.
	// ActiveMatches(account.AccountID) []*order.UserMatch

	// CreateAccount stores a new account. The account is considered unpaid until
	// PayAccount is used to set the payment details.
	CreateAccount(*account.Account) (string, error)

	// AccountRegAddr gets the registration fee address assigned to the account.
	AccountRegAddr(account.AccountID) (string, error)

	// PayAccount sets the registration fee payment transaction details for the
	// account, completing the registration process.
	PayAccount(account.AccountID, string, uint32) error
}

// MatchData represents an order pair match, but with just the order IDs instead
// of the full orders. The actual orders may be retrieved by ID.
type MatchData struct {
	ID        order.MatchID
	Taker     order.OrderID
	TakerAcct account.AccountID
	TakerAddr string
	Maker     order.OrderID
	MakerAcct account.AccountID
	MakerAddr string
	Epoch     order.EpochID
	Quantity  uint64
	Rate      uint64
	Status    order.MatchStatus
	Sigs      order.Signatures
	// cachedHash order.MatchID // TODO
}

// MatchArchiver is the interface required for storage and retrieval of all
// match data.
type MatchArchiver interface {
	UpdateMatch(match *order.Match) error
	MatchByID(mid order.MatchID, base, quote uint32) (*MatchData, error)
	UserMatches(aid account.AccountID, base, quote uint32) ([]*MatchData, error)
}

// ValidateOrder ensures that the order with the given status for the specified
// market is sensible. This function is in the database package because the
// concept of a valid order-status-market state is dependent on the semantics of
// order archival.
func ValidateOrder(ord order.Order, status order.OrderStatus, mkt *dex.MarketInfo) bool {
	// Orders with status OrderStatusUnknown should never reach the database.
	if status == order.OrderStatusUnknown {
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
		case order.OrderStatusEpoch, order.OrderStatusExecuted:
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
		case order.OrderStatusEpoch, order.OrderStatusExecuted: // orderStatusFailed if we decide to export that
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
		case order.OrderStatusEpoch, order.OrderStatusExecuted, order.OrderStatusRevoked:
		case order.OrderStatusBooked, order.OrderStatusCanceled:
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
