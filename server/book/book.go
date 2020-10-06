// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package book defines the order book used by each Market.
package book

import (
	"sync"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

const (
	// DefaultBookHalfCapacity is the default capacity of one side (buy or sell)
	// of the order book. It is set to 2^21 orders (2 mebiorders = 2,097,152
	// orders) per book side.
	DefaultBookHalfCapacity uint32 = 1 << 21 // 4 * 2 MiB
)

// Book is a market's order book. The Book uses a configurable lot size, of
// which all inserted orders must have a quantity that is a multiple. The buy
// and sell sides of the order book are contained in separate priority queues to
// allow constant time access to the best orders, and log time insertion and
// removal of orders.
type Book struct {
	mtx     sync.RWMutex
	lotSize uint64
	halfCap uint32
	buys    *OrderPQ
	sells   *OrderPQ
}

// New creates a new order book with the given lot size, and optional order
// capacity of each side of the book. To change capacity of an existing Book,
// use the Realloc method.
func New(lotSize uint64, halfCapacity ...uint32) *Book {
	halfCap := DefaultBookHalfCapacity
	if len(halfCapacity) > 0 {
		halfCap = halfCapacity[0]
	}
	return &Book{
		lotSize: lotSize,
		halfCap: halfCap,
		buys:    NewMaxOrderPQ(halfCap),
		sells:   NewMinOrderPQ(halfCap),
	}
}

// Clear reset the order book with configured capacity.
func (b *Book) Clear() {
	b.mtx.Lock()
	b.buys = NewMaxOrderPQ(b.halfCap)
	b.sells = NewMaxOrderPQ(b.halfCap)
	b.mtx.Unlock()
}

// Realloc changes the capacity of the order book given the specified capacity
// of both buy and sell sides of the book.
func (b *Book) Realloc(newHalfCap uint32) {
	b.mtx.Lock()
	b.buys.Realloc(newHalfCap)
	b.sells.Realloc(newHalfCap)
	b.mtx.Unlock()
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
	return b.sells.PeekBest()
}

// BestBuy returns a pointer to the best buy order in the order book. The
// order is NOT removed from the book.
func (b *Book) BestBuy() *order.LimitOrder {
	return b.buys.PeekBest()
}

// Best returns pointers to the best buy and sell order in the order book. The
// orders are NOT removed from the book.
func (b *Book) Best() (bestBuy, bestSell *order.LimitOrder) {
	b.mtx.RLock()
	bestBuy = b.buys.PeekBest()
	bestSell = b.sells.PeekBest()
	b.mtx.RUnlock()
	return
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
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if o.Sell {
		return b.sells.Insert(o)
	}
	return b.buys.Insert(o)
}

// Remove attempts to remove the order with the given OrderID from the book.
func (b *Book) Remove(oid order.OrderID) (*order.LimitOrder, bool) {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if removed, ok := b.sells.RemoveOrderID(oid); ok {
		return removed, true
	}
	if removed, ok := b.buys.RemoveOrderID(oid); ok {
		return removed, true
	}
	return nil, false
}

// RemoveUserOrders removes all ords from the book that belong to a user. The
// OrderIDs of the removed buy and sell orders are returned.
func (b *Book) RemoveUserOrders(user account.AccountID) (removedBuys, removedSells []*order.LimitOrder) {
	return b.buys.RemoveUserOrders(user), b.sells.RemoveUserOrders(user)
}

// HaveOrder checks if an order is in either the buy or sell side of the book.
func (b *Book) HaveOrder(oid order.OrderID) bool {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	return b.buys.HaveOrder(oid) || b.sells.HaveOrder(oid)
}

// Order attempts to retrieve an order from either the buy or sell side of the
// book. If the order data is not required, consider using HaveOrder.
func (b *Book) Order(oid order.OrderID) *order.LimitOrder {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	if lo := b.buys.Order(oid); lo != nil {
		return lo
	}
	return b.sells.Order(oid)
}

func (b *Book) UserOrderTotals(user account.AccountID) (buyAmt, sellAmt, buyCount, sellCount uint64) {
	b.mtx.RLock()
	buyAmt, buyCount = b.buys.UserOrderTotals(user)
	sellAmt, sellCount = b.sells.UserOrderTotals(user)
	b.mtx.RUnlock()
	return
}

// SellOrders copies out all sell orders in the book, sorted.
func (b *Book) SellOrders() []*order.LimitOrder {
	return b.sells.Orders()
}

// SellOrdersN copies out the N best sell orders in the book, sorted.
func (b *Book) SellOrdersN(N int) []*order.LimitOrder {
	return b.sells.OrdersN(N)
}

// BuyOrders copies out all buy orders in the book, sorted.
func (b *Book) BuyOrders() []*order.LimitOrder {
	return b.buys.Orders()
}

// BuyOrdersN copies out the N best buy orders in the book, sorted.
func (b *Book) BuyOrdersN(N int) []*order.LimitOrder {
	return b.buys.OrdersN(N)
}
