package matcher

import "github.com/decred/dcrdex/server/market/order"

// TODO. PLACEHOLDER. There needs to be an interface satisfied by the order book
// type provided to the matcher. This Booker is just a hint.

type Booker interface {
	BestSell() *order.Order
	BestBuy() *order.Order
	Insert(*order.Order)
	Remove(*order.Order)
}

// type EpochQueuer interface {
// 	Booker
// }
