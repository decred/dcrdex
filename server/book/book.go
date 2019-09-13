// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package book defines the order book used by each Market.
package book

import (
	"fmt"

	"github.com/decred/dcrdex/server/order"
)

const (
	bookHalfCapacity uint32 = 1 << 21 // 4 * 2 MiB
)

// Book is a market's order book. The Book uses a configurable lot size, of
// which all inserted orders must have a quantity that is a multiple. The buy
// and sell sides of the order book are contained in separate priority queues to
// allow constant time access to the best orders, and log time insertion and
// removal of orders.
type Book struct {
	lotSize uint64
	buys    *OrderPQ
	sells   *OrderPQ
}

// New creates a new order book with the given lot size. Capacity of the order
// book is fixed at 2^21 orders (2 mebiorders = 2,097,152 orders) per book side
// (buy or sell). TODO: allow dynamic order book capacity.
func New(lotSize uint64) *Book {
	return &Book{
		lotSize: lotSize,
		buys:    NewMaxOrderPQ(bookHalfCapacity),
		sells:   NewMinOrderPQ(bookHalfCapacity),
	}
}

// LotSize returns the Book's configured lot size in atoms of the base asset.
func (b *Book) LotSize() uint64 {
	return b.lotSize
}

// BuyCount returns the number of buy orders.
func (b *Book) BuyCount() int {
	return b.buys.Count()
}

// SellCount returns the number of sell orders.
func (b *Book) SellCount() int {
	return b.sells.Count()
}

// BestSell returns a pointer to the best sell order in the order book. The
// order is NOT removed from the book.
func (b *Book) BestSell() *order.LimitOrder {
	best, ok := b.sells.PeekBest().(*order.LimitOrder)
	if !ok {
		panic(fmt.Sprintf("Best sell order is not a *order.LimitOrder: %T",
			b.sells.PeekBest()))
	}
	return best
}

// BestBuy returns a pointer to the best buy order in the order book. The
// order is NOT removed from the book.
func (b *Book) BestBuy() *order.LimitOrder {
	best, ok := b.buys.PeekBest().(*order.LimitOrder)
	if !ok {
		panic(fmt.Sprintf("Best sell order is not a *order.LimitOrder: %T",
			b.sells.PeekBest()))
	}
	return best
}

// Insert attempts to insert the provided order into the order book, returning a
// boolean indicating if the insertion was successful. If the order is not an
// integer multiple of the Book's lot size, the order will not be inserted.
func (b *Book) Insert(o *order.LimitOrder) bool {
	if o.Quantity%b.lotSize != 0 {
		log.Warnf("(*Book).Insert: Refusing to insert an order with a " +
			"quantity that is not a multiple of lot size.")
		return false
	}
	if o.Sell {
		return b.sells.Insert(o)
	}
	return b.buys.Insert(o)
}

// Remove attempts to remove the order with the given OrderID from the book.
func (b *Book) Remove(oid order.OrderID) (*order.LimitOrder, bool) {
	uid := oid.String()
	if removed, ok := b.sells.RemoveOrderUID(uid); ok {
		return removed.(*order.LimitOrder), true
	}
	if removed, ok := b.buys.RemoveOrderUID(uid); ok {
		return removed.(*order.LimitOrder), true
	}
	return nil, false
}
