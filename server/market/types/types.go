package types

import (
	"fmt"
	"strings"

	"github.com/decred/dcrdex/server/asset"
)

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

// OrderStatus indicates how an order is presently being processed, or the final
// state of the order if processing is completed.
type OrderStatus uint16

const (
	// OrderStatusUnknown is a sentinel value to be used when the status of an
	// order cannot be determined.
	OrderStatusUnknown OrderStatus = iota

	// There are two general classes of orders: ACTIVE and ARCHIVED. Orders with
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

	// Below are the ARCHIVED order statuses. These orders are unlikely to be
	// updated. As such, they are suitable for archival.

	// OrderStatusFailed is for archived unmatched orders that do not go on the
	// order book, either because the time in force is immediate or the order is
	// not a limit order.
	OrderStatusFailed
	// OrderStatusCanceled is for archived orders that have been explicitly
	// canceled by a cancel order or administrative action such as conduct
	// enforcement.
	OrderStatusCanceled
	// OrderStatus Executed is for archived orders that have been successfully
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
// live/active (true), or if the order is in a archived state (false).
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
// archived state and is no longer being processed. Archived == !Active.
func (s OrderStatus) Archived() bool {
	return !s.Active()
}
