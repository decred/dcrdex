// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package matcher

import "decred.org/dcrdex/dex/order"

// Booker should be implemented by the order book.
type Booker interface {
	LotSize() uint64
	BuyCount() int
	SellCount() int
	BestSell() *order.LimitOrder
	BestBuy() *order.LimitOrder
	Insert(*order.LimitOrder) bool
	Remove(order.OrderID) (*order.LimitOrder, bool)
	BuyOrders() []*order.LimitOrder
	SellOrders() []*order.LimitOrder
}

// MatchCycleStats is data about the results of a match cycle.
type MatchCycleStats struct {
	MatchVolume uint64
	QuoteVolume uint64
	BookVolume  uint64
	OrderVolume uint64
	HighRate    uint64
	LowRate     uint64
	StartRate   uint64
	EndRate     uint64
}
