// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"fmt"

	"github.com/decred/dcrdex/server/order"
)

const (
	bookHalfCapacity uint32 = 1 << 21 // 4 * 2 MiB
)

type Book struct {
	lotSize uint64
	buys    *OrderPQ
	sells   *OrderPQ
}

func New(lotSize uint64) *Book {
	return &Book{
		lotSize: lotSize,
		buys:    NewMaxOrderPQ(bookHalfCapacity),
		sells:   NewMinOrderPQ(bookHalfCapacity),
	}
}

func (b *Book) LotSize() uint64 {
	return b.lotSize
}

func (b *Book) BuyCount() int {
	return b.buys.Count()
}

func (b *Book) SellCount() int {
	return b.sells.Count()
}

func (b *Book) BestSell() *order.LimitOrder {
	best, ok := b.sells.PeekBest().(*order.LimitOrder)
	if !ok {
		panic(fmt.Sprintf("Best sell order is not a *order.LimitOrder: %T",
			b.sells.PeekBest()))
	}
	return best
}

func (b *Book) BestBuy() *order.LimitOrder {
	best, ok := b.buys.PeekBest().(*order.LimitOrder)
	if !ok {
		panic(fmt.Sprintf("Best sell order is not a *order.LimitOrder: %T",
			b.sells.PeekBest()))
	}
	return best
}

func (b *Book) Insert(o *order.LimitOrder) bool {
	if o.Sell {
		return b.sells.Insert(o)
	}
	return b.buys.Insert(o)
}

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
