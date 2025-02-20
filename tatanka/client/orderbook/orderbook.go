// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"errors"
	"sort"
	"sync"

	"decred.org/dcrdex/tatanka/tanka"
)

// OrderBooker specifies the orderbook interface.
type OrderBooker interface {
	OrderIDs() [][32]byte
	Order(id [32]byte) *tanka.OrderUpdate
	Orders(ids [][32]byte) []*tanka.OrderUpdate
	FindOrders(filter *OrderFilter) []*tanka.OrderUpdate
	AddUpdate(*tanka.OrderUpdate) error
	Delete(id [32]byte)
}

// OrderFilter is used when searching for orders.
type OrderFilter struct {
	IsSell *bool
	Check  func(*tanka.OrderUpdate) (ok, done bool)
}

var _ OrderBooker = (*OrderBook)(nil)

// OrderBook holds the orderbook.
type OrderBook struct {
	mtx  sync.RWMutex
	book map[[32]byte]*tanka.OrderUpdate
	// buys and sells use the same pointers as the book and are sorted
	// with the best trade first.
	buys, sells []*tanka.OrderUpdate
}

// NewOrderBook created a new orderbook.
func NewOrderBook() *OrderBook {
	return &OrderBook{
		book: make(map[[32]byte]*tanka.OrderUpdate),
	}
}

// OrderIDs returns all order ids.
func (ob *OrderBook) OrderIDs() [][32]byte {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	ids := make([][32]byte, len(ob.book))
	i := 0
	for id := range ob.book {
		ids[i] = id
		i++
	}
	return ids
}

// Order returns one order by id.
func (ob *OrderBook) Order(id [32]byte) *tanka.OrderUpdate {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	return ob.book[id]
}

// Orders returns all orders for the supplied ids.
func (ob *OrderBook) Orders(ids [][32]byte) []*tanka.OrderUpdate {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()
	ords := make([]*tanka.OrderUpdate, 0, len(ids))
	for _, id := range ids {
		if ou, has := ob.book[id]; has {
			ords = append(ords, ou)
		}
	}
	return ords
}

// FindOrders returns all orders the filter returns true until it specifies
// done. Orders are returned with the best buys/sells first.
func (ob *OrderBook) FindOrders(filter *OrderFilter) (ous []*tanka.OrderUpdate) {
	if filter == nil {
		return nil
	}
	find := func(isSell bool) {
		ords := ob.buys
		if isSell {
			ords = ob.sells
		}
		for _, ou := range ords {
			if filter.Check != nil {
				ok, done := filter.Check(ou)
				if !ok {
					continue
				}
				if done {
					break
				}
			}
			ous = append(ous, ou)
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
	return ous
}

// addOrderAndSort must be called with the mtx held for writes.
func (ob *OrderBook) addOrderAndSort(ou *tanka.OrderUpdate) {
	ords := &ob.buys
	sortFn := func(i, j int) bool { return (*ords)[i].Rate > (*ords)[j].Rate }
	if ou.Sell {
		ords = &ob.sells
		sortFn = func(i, j int) bool { return (*ords)[i].Rate < (*ords)[j].Rate }
	}
	*ords = append(*ords, ou)
	sort.Slice(*ords, sortFn)
}

// addOrderAndSort must be called with the mtx held for writes.
func (ob *OrderBook) deleteSortedOrder(ou *tanka.OrderUpdate) {
	ords := &ob.buys
	if ou.Sell {
		ords = &ob.sells
	}
	for i, o := range *ords {
		// Comparing pointers.
		if o == ou {
			*ords = append((*ords)[:i], (*ords)[i+1:]...)
			break
		}
	}
}

// AddUpdate adds or updates an order.
func (ob *OrderBook) AddUpdate(ou *tanka.OrderUpdate) error {
	id := ou.ID()
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	oldOU, has := ob.book[id]
	if has && oldOU.Expiration.Before(ou.Expiration) {
		return errors.New("expiration is before already recorded order")
	}
	ob.book[id] = ou
	if has {
		ob.deleteSortedOrder(oldOU)
	}
	ob.addOrderAndSort(ou)
	return nil
}

// Delete deletes an order from the books.
func (ob *OrderBook) Delete(id [32]byte) {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()
	ou, has := ob.book[id]
	if has {
		delete(ob.book, id)
		ob.deleteSortedOrder(ou)
	}
}
