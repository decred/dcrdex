// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/tatanka/tanka"
)

// Booker specifies the orderbook interface.
type Booker interface {
	Order(id tanka.ID40) *tanka.Order
	Orders(ids []tanka.ID40) []*tanka.Order
	Find(filter *Filter)
	Add(*tanka.Order)
	Update(ou *tanka.OrderUpdate) error
	Delete(id tanka.ID40)
}

// Filter is used when searching for orders.
type Filter struct {
	IsSell *bool
	Check  func(*tanka.Order) (done bool)
}

var _ Booker = (*Book)(nil)

// Book holds the orderbook.
type Book struct {
	mtx  sync.RWMutex
	book map[tanka.ID40]*tanka.Order
	// buys and sells use the same pointers as the book and are sorted
	// with the best trade first.
	buys, sells []*tanka.Order
}

// NewBook created a new orderbook.
func New() *Book {
	return &Book{
		book: make(map[tanka.ID40]*tanka.Order),
	}
}

// Order returns one order by id.
func (ob *Book) Order(id tanka.ID40) *tanka.Order {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	return ob.book[id]
}

// Orders returns all orders for the supplied ids.
func (ob *Book) Orders(ids []tanka.ID40) []*tanka.Order {
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

// Find filters through all orders with the filter.Check function until done is
// true. They are processed best orders first.
func (ob *Book) Find(filter *Filter) {
	if filter == nil || filter.Check == nil {
		return
	}
	find := func(isSell bool) {
		ords := ob.buys
		if isSell {
			ords = ob.sells
		}
		for _, o := range ords {
			if filter.Check(o) {
				break
			}
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
}

// addOrderAndSort must be called with the mtx held for writes.
func (ob *Book) addOrderAndSort(o *tanka.Order) {
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
func (ob *Book) deleteSortedOrder(to *tanka.Order) {
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
func (ob *Book) Add(o *tanka.Order) {
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
func (ob *Book) Update(ou *tanka.OrderUpdate) error {
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
func (ob *Book) Delete(id tanka.ID40) {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	o, has := ob.book[id]
	if has {
		delete(ob.book, id)
		ob.deleteSortedOrder(o)
	}
}
