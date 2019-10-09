// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"
	"strings"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/dcrdex/server/order"
)

// var (
// 	driverMtx  sync.Mutex
// 	driverName string
// 	driver     Driver
// )

// type Driver interface {
// 	Open(ctx context.Context, cfg interface{}) (DEXArchivist, error)
// }

// func Register(name string, d Driver) {
// 	driverMtx.Lock()
// 	defer driverMtx.Unlock()
// 	if d == nil {
// 		panic("db: Register driver is nil")
// 	}
// 	if driver != nil {
// 		panic("db: Register already called. driver: " + driverName)
// 	}
// 	driverName = name
// 	driver = d
// }

// MarketInfo specified a market that the Archiver must support.
type MarketInfo struct {
	Name    string
	Base    uint32
	Quote   uint32
	LotSize uint64
}

func marketName(base, quote string) string {
	return base + "_" + quote
}

func MarketName(base, quote uint32) (string, error) {
	baseSymbol := asset.BipIDSymbol(base)
	if baseSymbol == "" {
		return "", fmt.Errorf("base asset %d not found", base)
	}
	baseSymbol = strings.ToLower(baseSymbol)

	quoteSymbol := asset.BipIDSymbol(quote)
	if quoteSymbol == "" {
		return "", fmt.Errorf("quote asset %d not found", quote)
	}
	quoteSymbol = strings.ToLower(quoteSymbol)

	return marketName(baseSymbol, quoteSymbol), nil
}

func NewMarketInfo(base, quote uint32, lotSize uint64) (*MarketInfo, error) {
	name, err := MarketName(base, quote)
	if err != nil {
		return nil, err
	}
	return &MarketInfo{
		Name:    name,
		Base:    base,
		Quote:   quote,
		LotSize: lotSize,
	}, nil
}

func NewMarketInfoFromSymbols(base, quote string, lotSize uint64) (*MarketInfo, error) {
	baseID, found := asset.BipSymbolID(strings.ToLower(base))
	if !found {
		return nil, fmt.Errorf(`base asset symbol "%s" unrecognized`, base)
	}

	quoteID, found := asset.BipSymbolID(strings.ToLower(quote))
	if !found {
		return nil, fmt.Errorf(`quote asset symbol "%s" unrecognized`, quote)
	}

	return &MarketInfo{
		Name:    marketName(base, quote),
		Base:    baseID,
		Quote:   quoteID,
		LotSize: lotSize,
	}, nil
}

type DEXArchivist interface {
	OrderArchiver
}

type OrderArchiver interface {
	// Order retrieves an order with the given OrderID, stored for the market
	// specified by the given base and quote assets.
	Order(oid order.OrderID, base, quote uint32) (order.Order, OrderStatus, error)
	// StoreOrder stores an order with the provided status.
	StoreOrder(ord order.Order, status OrderStatus) error

	// OrderStatus gets the status and filled amount of the given order.
	OrderStatus(order.Order) (OrderStatus, int64, error)
	// OrderStatusByID gets the status and filled amount of the order with the
	// given OrderID in the market specified by a base and quote asset.
	OrderStatusByID(oid order.OrderID, base, quote uint32) (OrderStatus, int64, error)

	// UpdateOrder updates the filled amount of the given order.
	UpdateOrder(order.Order) error
	// UpdateOrderByID updates the filled amount of the order with the given
	// OrderID in the market specified by a base and quote asset.
	UpdateOrderByID(oid order.OrderID, base, quote uint32, filled int64) error

	// UpdateOrderStatus updates the status and filled amount of the given
	// order.
	UpdateOrderStatus(order.Order, OrderStatus) error
	// UpdateOrderStatusByID updates the status and filled amount of the order
	// with the given OrderID in the market specified by a base and quote asset.
	UpdateOrderStatusByID(oid order.OrderID, base, quote uint32, status OrderStatus, filled int64) error

	// UserOrders retrieves all orders for the given account in the market
	// specified by a base and quote asset.
	UserOrders(aid account.AccountID, base, quote uint32) ([]order.Order, error)
}

// OrderStatus indicates how an order is presently being processed, or the final
// state of the order if processing is completed.
type OrderStatus uint16

const (
	// OrderStatusUnknown is a sentinel value to be used when the status of an
	// order cannot be determined.
	OrderStatusUnknown OrderStatus = iota

	// There are two general classes of orders: ACTIVE and TERMINAL. Orders with
	// one of the ACTIVE order statuses that follow are likely to be updated.

	// OrderStatusPending is for active orders that have been received and
	// validated, but not processed by the epoch order matcher.
	OrderStatusPending
	// OrderStatusMatched is for active orders that have been matched with other
	// orders, but for which an atomic swap has not been initiated.
	OrderStatusMatched
	// OrderStatusSwapping is for active orders that have begun the atomic swap
	// process. Specifically, this is when the first swap initialization
	// transaction has been broadcast by a client.
	OrderStatusSwapping
	// OrderStatusBooked is for active unmatched orders that have been put on
	// the order book (standing time in force).
	OrderStatusBooked

	// Below are the TERMINAL order statuses. These orders are unlikely to be
	// updated. As such, they are suitable for archival.

	// OrderStatusFailed is for terminal unmatched orders that do not go on the
	// order book, either because the time in force is immediate or the order is
	// not a limit order.
	OrderStatusFailed
	// OrderStatusCanceled is for terminal orders that have been explicitly
	// canceled by a cancel order or administrative action such as conduct
	// enforcement.
	OrderStatusCanceled
	// OrderStatus Executed is for terminal orders that have been successfully
	// processed.
	OrderStatusExecuted
)

var orderStatusNames = map[OrderStatus]string{
	OrderStatusUnknown:  "unknown",
	OrderStatusPending:  "pending",
	OrderStatusMatched:  "matched",
	OrderStatusSwapping: "swapping",
	OrderStatusBooked:   "booked",
	OrderStatusFailed:   "failed",
	OrderStatusCanceled: "canceled",
	OrderStatusExecuted: "executed",
}

// String implements Stringer.
func (s OrderStatus) String() string {
	name, ok := orderStatusNames[s]
	if !ok {
		panic("unknown order status!") // programmer error
	}
	return name
}

// Active indicates if the OrderStatus reflects an order that is still
// live/active (true), or if the order is in a terminal state (false).
func (s OrderStatus) Active() bool {
	switch s {
	case OrderStatusPending, OrderStatusMatched, OrderStatusSwapping,
		OrderStatusBooked:
		return true
	case OrderStatusFailed, OrderStatusCanceled, OrderStatusExecuted,
		OrderStatusUnknown:
		return false
	default:
		panic("unknown order status!") // programmer error
	}
}

// Archived indicates if the OrderStatus reflects an order that has reached a
// terminal state and is no longer being processed. Archived == !Active.
func (s OrderStatus) Archived() bool {
	return !s.Active()
}

func ValidateOrder(ord order.Order, status OrderStatus, mkt *MarketInfo) bool {
	//  NO OrderStatusUnknown
	if status == OrderStatusUnknown {
		return false
	}

	// Verify the order is for the given market.
	if ord.Base() != mkt.Base || ord.Quote() != mkt.Quote {
		return false
	}

	switch ot := ord.(type) {
	case *order.MarketOrder:
		// Market orders OK statuses: pending, matched, swapping, failed,
		// executed, canceled (NOT booked).
		switch status {
		case OrderStatusPending, OrderStatusMatched, OrderStatusSwapping,
			OrderStatusFailed, OrderStatusExecuted, OrderStatusCanceled:
		default:
			return false
		}

		if ot.OrderType != order.MarketOrderType {
			return false
		}

		// Market sell orders must respect lot size.
		if ot.Sell && ot.Quantity%mkt.LotSize != 0 || ord.Remaining()%mkt.LotSize != 0 {
			return false
		}

	case *order.CancelOrder:
		// Cancel order OK statuses: pending, matched, failed, executed
		// (NOT booked, swapping, or canceled).
		switch status {
		case OrderStatusPending, OrderStatusMatched, OrderStatusFailed,
			OrderStatusExecuted:
		default:
			return false
		}

		if ot.OrderType != order.CancelOrderType {
			return false
		}

		// All cancel orders must have zero and filled remaining amounts.
		if ord.Remaining() != 0 || ord.FilledAmt() != 0 {
			return false
		}

	case *order.LimitOrder:
		// Limit order OK statuses: pending, matched, swapping, booked, failed,
		// executed, canceled (same as market plus booked).
		switch status {
		case OrderStatusPending, OrderStatusMatched, OrderStatusSwapping,
			OrderStatusBooked, OrderStatusFailed, OrderStatusExecuted,
			OrderStatusCanceled:
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
//  1. The essential and immutable order.Order data
//  2. PK: Computed public order UID
//  3. Status (canceled, failed/not matched, matched, swap in progress, swap completed)
//  4. Remaining/filled amount (or is this a book/market item?)
//  5. Owner account ID

// Epochs:
//  1. PK: epoch ID
//  2. list of order IDs and order hashes (ref orders table)
//  3. resulting shuffle order
//  4. resulting matches (ref matches table)
//  5. resulting order mods (change filled amount)
//  6. resulting book mods (insert, remove)
//  refs: orders, matches

// Matches:
//  1. The order.Match data
//  2. Status for each maker matched with the taker (failed, executed/swapped)
// PK is ??? a unique ID used by the Market?
//  refs: orders

// Swaps:
//  1. The swap.Swap data
//  2. PK: Computed public swap ID
//  3. Status (pending, in progress, failed, executed)
//  refs: matches

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
