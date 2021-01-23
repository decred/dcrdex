// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// orderPreference represents ordering preference for a sort.
type orderPreference int

const (
	ascending orderPreference = iota
	descending
)

// Fill represents an order fill.
type Fill struct {
	Rate     uint64
	Quantity uint64
}

// bookSide represents a side of the order book.
type bookSide struct {
	bins      map[uint64][]*Order
	rateIndex *rateIndex
	orderPref orderPreference
	mtx       sync.RWMutex
}

// newBookSide creates a new book side depth.
func newBookSide(pref orderPreference) *bookSide {
	return &bookSide{
		bins:      make(map[uint64][]*Order),
		rateIndex: newRateIndex(),
		orderPref: pref,
	}
}

// Add puts an order in its associated bin.
func (d *bookSide) Add(order *Order) {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	bin, exists := d.bins[order.Rate]
	if !exists {
		bin = make([]*Order, 0, 1)
	}

	i := sort.Search(len(bin), func(i int) bool { return bin[i].Time >= order.Time })
	for ; i < len(bin); i++ {
		if bin[i].Time != order.Time {
			break
		}
		// Differentiate via order ids if timestamps are identical.
		if bytes.Compare(bin[i].OrderID[:], order.OrderID[:]) > 0 {
			break
		}
	}

	bin = append(bin, nil)
	copy(bin[i+1:], bin[i:])
	bin[i] = order
	d.bins[order.Rate] = bin

	// Update the sort order if a new order group is created.
	if !exists {
		d.rateIndex.Add(order.Rate)
		return
	}
}

// Remove deletes an order from its associated bin.
func (d *bookSide) Remove(order *Order) error {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	bin, exists := d.bins[order.Rate]
	if !exists {
		return fmt.Errorf("no bin found for rate %d", order.Rate)
	}

	for i := 0; i < len(bin); i++ {
		if bytes.Equal(order.OrderID[:], bin[i].OrderID[:]) {
			// Remove the entry and preserve the sort order.
			if i < len(bin)-1 {
				copy(bin[i:], bin[i+1:])
			}
			bin[len(bin)-1] = nil
			bin = bin[:len(bin)-1]

			// Delete the bin if there are no orders left in it.
			if len(bin) == 0 {
				delete(d.bins, order.Rate)
				return d.rateIndex.Remove(order.Rate)
			}

			d.bins[order.Rate] = bin
			return nil
		}
	}

	return fmt.Errorf("order %s not found", order.OrderID)
}

// UpdateRemaining updates the remaining quantity for an order. If the order is
// found it will be returned, else nil.
func (d *bookSide) UpdateRemaining(oid order.OrderID, remaining uint64) *Order {
	d.mtx.Lock()
	defer d.mtx.Unlock()

	for _, bin := range d.bins {
		for _, ord := range bin {
			if ord.OrderID == oid {
				ord.Quantity = remaining
				return ord
			}
		}
	}
	return nil
}

// orders is all orders for the side, sorted.
func (d *bookSide) orders() []*Order {
	orders, _ := d.BestNOrders(int(^uint(0) >> 1)) // Max int value
	return orders
}

// BestNOrders returns the best N orders of the book side.
func (d *bookSide) BestNOrders(n int) ([]*Order, bool) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	count := n
	best := make([]*Order, 0)

	d.iterateOrders(func(ord *Order) bool {
		if count == 0 {
			return false
		}
		best = append(best, ord)
		count--
		return true
	})

	return best, len(best) == n
}

// BestFill returns the best fill for the provided quantity.
func (d *bookSide) BestFill(qty uint64) ([]*Fill, bool) {
	return d.bestFill(qty, false, 0)
}

func (d *bookSide) bestFill(quantity uint64, convert bool, lotSize uint64) ([]*Fill, bool) {
	remainingQty := quantity
	best := make([]*Fill, 0)

	d.iterateOrders(func(ord *Order) bool {
		if remainingQty == 0 {
			return false
		}

		qty := ord.Quantity
		if convert {
			if calc.QuoteToBase(ord.Rate, remainingQty) < lotSize {
				return false
			}
			qty = calc.BaseToQuote(ord.Rate, ord.Quantity)
		}

		var entry *Fill
		if remainingQty < qty {
			fillQty := remainingQty
			if convert {
				r := calc.QuoteToBase(ord.Rate, remainingQty)
				fillQty = r - (r % lotSize)
				// remainingQty -= calc.BaseToQuote(ord.Rate, ord.Quantity-fillQty)
			}

			// remainingQty almost certainly not zero for market buy orders, but
			// set to zero to return filled=true to indicate that the order was
			// exhausted before the book.
			remainingQty = 0

			entry = &Fill{
				Rate:     ord.Rate,
				Quantity: fillQty,
			}
		} else {
			entry = &Fill{
				Rate:     ord.Rate,
				Quantity: ord.Quantity,
			}
			remainingQty -= qty
		}

		best = append(best, entry)
		return true
	})

	// Or maybe should return calc.QuoteToBase(ord.Rate, remainingQty) < lotSize
	// when convert = true?
	return best, remainingQty == 0
}

func (d *bookSide) idxCalculator() func(i int) int {
	if d.orderPref == ascending {
		return func(i int) int { return i }
	}
	binCount := len(d.rateIndex.Rates)
	return func(i int) int { return binCount - 1 - i }
}

func (d *bookSide) iterateOrders(check func(*Order) bool) {
	calcIdx := d.idxCalculator()

	for i := 0; i < len(d.rateIndex.Rates); i++ {
		bin := d.bins[d.rateIndex.Rates[calcIdx(i)]]

		for idx := 0; idx < len(bin); idx++ {
			if !check(bin[idx]) {
				break
			}
		}
	}
}
