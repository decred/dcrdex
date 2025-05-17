// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/tatanka/tanka"
)

// OrderBooker specifies the orderbook interface.
type OrderBooker interface {
	Order(id tanka.ID40) *tanka.Order
	Orders(ids []tanka.ID40) []*tanka.Order
	FindOrders(filter *OrderFilter) []*tanka.Order
	Add(*tanka.Order)
	Update(ou *tanka.OrderUpdate) error
	Delete(id tanka.ID40)
}

// OrderFilter is used when searching for orders.
type OrderFilter struct {
	IsSell *bool
	Check  func(*tanka.Order) (ok, done bool)
}

var _ OrderBooker = (*OrderBook)(nil)

// OrderBook holds the orderbook.
type OrderBook struct {
	mtx  sync.RWMutex
	book map[tanka.ID40]*tanka.Order
	// buys and sells use the same pointers as the book and are sorted
	// with the best trade first.
	buys, sells []*tanka.Order
}

// NewOrderBook created a new orderbook.
func NewOrderBook() *OrderBook {
	return &OrderBook{
		book: make(map[tanka.ID40]*tanka.Order),
	}
}

// Order returns one order by id.
func (ob *OrderBook) Order(id tanka.ID40) *tanka.Order {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	return ob.book[id]
}

// Orders returns all orders for the supplied ids.
func (ob *OrderBook) Orders(ids []tanka.ID40) []*tanka.Order {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	ords := make([]*tanka.Order, 0, len(ids))
	for _, id := range ids {
		if o, has := ob.book[id]; has {
			ords = append(ords, o)
		}
	}
	return ords
}

// FindOrders returns all orders the filter returns true until it specifies
// done. Orders are returned with the best buys/sells first.
func (ob *OrderBook) FindOrders(filter *OrderFilter) (orders []*tanka.Order) {
	if filter == nil {
		return nil
	}
	find := func(isSell bool) {
		ords := ob.buys
		if isSell {
			ords = ob.sells
		}
		for _, o := range ords {
			if filter.Check != nil {
				ok, done := filter.Check(o)
				if !ok {
					continue
				}
				if done {
					break
				}
			}
			orders = append(orders, o)
		}
	}
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	if filter.IsSell != nil {
		find(*filter.IsSell)
	} else {
		find(false)
		find(true)
	}
	return orders
}

// addOrderAndSort must be called with the mtx held for writes.
func (ob *OrderBook) addOrderAndSort(o *tanka.Order) {
	ords := &ob.buys
	sortFn := func(i, j int) bool { return (*ords)[i].Rate > (*ords)[j].Rate }
	if o.Sell {
		ords = &ob.sells
		sortFn = func(i, j int) bool { return (*ords)[i].Rate < (*ords)[j].Rate }
	}
	*ords = append(*ords, o)
	sort.Slice(*ords, sortFn)
}

// deleteSortedOrder must be called with the mtx held for writes.
func (ob *OrderBook) deleteSortedOrder(to *tanka.Order) {
	ords := &ob.buys
	if to.Sell {
		ords = &ob.sells
	}
	for i, o := range *ords {
		// Comparing pointers.
		if o == to {
			*ords = append((*ords)[:i], (*ords)[i+1:]...)
			break
		}
	}
}

// Add adds an order.
func (ob *OrderBook) Add(o *tanka.Order) {
	id := o.ID()
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	_, has := ob.book[id]
	if has {
		return
	}
	ob.book[id] = o
	ob.addOrderAndSort(o)
}

// Update updates an order.
func (ob *OrderBook) Update(ou *tanka.OrderUpdate) error {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	id := ou.ID()
	o, has := ob.book[id]
	if !has {
		return fmt.Errorf("order %x not found", id)
	}
	o.Qty = ou.Qty
	o.Stamp = ou.Stamp
	return nil
}

// Delete deletes an order from the books.
func (ob *OrderBook) Delete(id tanka.ID40) {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	o, has := ob.book[id]
	if has {
		delete(ob.book, id)
		ob.deleteSortedOrder(o)
	}
}
