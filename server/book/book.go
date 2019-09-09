// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"sync"

	"github.com/decred/dcrdex/server/order"
)

const (
	bookHalfCapacity uint32 = 1 << 21 // 4 * 2 MiB
)

type Book struct {
	mtx                      sync.RWMutex
	lotSize                  uint64
	buys                     *OrderPQ
	minBuyRate, maxBuyRate   uint64
	sells                    *OrderPQ
	minSellRate, maxSellRate uint64
}

func New(lotSize uint64) *Book {
	return &Book{
		lotSize: lotSize,
		buys:    NewMaxOrderPQ(bookHalfCapacity),
		sells:   NewMinOrderPQ(bookHalfCapacity),
	}
}

func (b *Book) LotSize() uint64 {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.lotSize
}

func (b *Book) BuyCount() int {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.buys.Count()
}

func (b *Book) SellCount() int {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.sells.Count()
}

func (b *Book) BestSell() *order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	best, ok := b.sells.PeekBest().(*order.LimitOrder)
	if !ok {
		return nil
	}
	return best
}

func (b *Book) BestBuy() *order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	best, ok := b.buys.PeekBest().(*order.LimitOrder)
	if !ok {
		return nil
	}
	return best
}

func (b *Book) Insert(o *order.LimitOrder) bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	rate := o.Rate

	if o.Sell {
		if b.sells.Count() == 0 {
			b.minSellRate = rate
			b.maxSellRate = rate
		} else {
			if rate < b.minSellRate {
				b.minSellRate = rate
			} else if rate > b.maxSellRate {
				b.maxSellRate = rate
			}
		}
		return b.sells.Insert(o)
	}

	if b.buys.Count() == 0 {
		b.minBuyRate = rate
		b.maxBuyRate = rate
	} else {
		if rate < b.minBuyRate {
			b.minBuyRate = rate
		} else if rate > b.maxBuyRate {
			b.maxBuyRate = rate
		}
	}
	return b.buys.Insert(o)
}

func (b *Book) Remove(oid order.OrderID) (*order.LimitOrder, bool) {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	uid := oid.String()
	if removed, ok := b.sells.RemoveOrderUID(uid); ok {
		//		if removed.Price() == b.
		return removed.(*order.LimitOrder), true
	}
	if removed, ok := b.buys.RemoveOrderUID(uid); ok {
		return removed.(*order.LimitOrder), true
	}
	return nil, false
}
