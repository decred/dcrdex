// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package matcher

import "github.com/decred/dcrdex/server/market/order"

// TODO. PLACEHOLDER. There needs to be an interface satisfied by the order book
// type provided to the matcher. This Booker is just a hint.

type Booker interface {
	LotSize() uint64
	BuyCount() int
	SellCount() int
	BestSell() *order.LimitOrder
	BestBuy() *order.LimitOrder
	//Best(sell bool) *order.LimitOrder
	Insert(*order.LimitOrder)
	Remove(order.OrderID) (*order.LimitOrder, bool)
}

// type EpochQueuer interface {
// 	Booker
// }
