// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package matcher

import "github.com/decred/dcrdex/dex/order"

// Booker should be implemented by the order book.
type Booker interface {
	LotSize() uint64
	BuyCount() int
	SellCount() int
	BestSell() *order.LimitOrder
	BestBuy() *order.LimitOrder
	Insert(*order.LimitOrder) bool
	Remove(order.OrderID) (*order.LimitOrder, bool)
}
