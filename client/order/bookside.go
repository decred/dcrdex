package order

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
)

// OrderPreference reprsents ordering preference for a sort.
type OrderPreference int

const (
	// Ascending denotes ascending order.
	ascending OrderPreference = iota
	// Descending denotes descending order.
	descending
)

// fill represents an order fill.
type fill struct {
	match    *Order
	quantity uint64
}

// bookSide represents a side of the order book.
type bookSide struct {
	bins      map[uint64][]*Order
	rateIndex *rateIndex
	orderPref OrderPreference
	mtx       sync.RWMutex
}

// NewBookSide creates a new book side depth.
func NewBookSide(pref OrderPreference) *bookSide {
	return &bookSide{
		bins:      make(map[uint64][]*Order),
		rateIndex: NewRateIndex(),
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

// BestNOrders returns the best N orders of the book side.
func (d *bookSide) BestNOrders(n uint64) ([]*Order, error) {
	count := n
	best := make([]*Order, 0)

	// Fetch the best N orders per order preference.
	switch d.orderPref {
	case ascending:
		for i := 0; i < len(d.rateIndex.Rates); i++ {
			bin := d.bins[d.rateIndex.Rates[i]]

			for idx := 0; idx < len(bin); idx++ {
				if count == 0 {
					break
				}

				best = append(best, bin[idx])
				count--
			}

			if count == 0 {
				break
			}
		}

	case descending:
		for i := len(d.rateIndex.Rates) - 1; i >= 0; i-- {
			group := d.bins[d.rateIndex.Rates[i]]

			for idx := 0; idx < len(group); idx++ {
				if count == 0 {
					break
				}

				best = append(best, group[idx])
				count--
			}

			if count == 0 {
				break
			}
		}

	default:
		return nil, fmt.Errorf("unknown order preference %v", d.orderPref)
	}

	return best, nil
}

// BestFill returns the best fill for the provided quantity.
func (d *bookSide) BestFill(quantity uint64) ([]*fill, error) {
	remainingQty := quantity
	best := make([]*fill, 0)

	// Fetch the best fill for the provided quantity.
	switch d.orderPref {
	case ascending:
		for i := 0; i < len(d.rateIndex.Rates); i++ {
			bin := d.bins[d.rateIndex.Rates[i]]

			for idx := 0; idx < len(bin); idx++ {
				if remainingQty == 0 {
					break
				}

				var entry *fill
				if remainingQty < bin[idx].Quantity {
					entry = &fill{
						match:    bin[idx],
						quantity: remainingQty,
					}
					remainingQty = 0
				} else {
					entry = &fill{
						match:    bin[idx],
						quantity: bin[idx].Quantity,
					}
					remainingQty -= bin[idx].Quantity
				}

				best = append(best, entry)
			}

			if remainingQty == 0 {
				break
			}
		}

	case descending:
		for i := len(d.rateIndex.Rates) - 1; i >= 0; i-- {
			bin := d.bins[d.rateIndex.Rates[i]]

			for idx := 0; idx < len(bin); idx++ {
				if remainingQty == 0 {
					break
				}

				var entry *fill
				if remainingQty < bin[idx].Quantity {
					entry = &fill{
						match:    bin[idx],
						quantity: remainingQty,
					}
					remainingQty = 0
				} else {
					entry = &fill{
						match:    bin[idx],
						quantity: bin[idx].Quantity,
					}
					remainingQty -= bin[idx].Quantity
				}

				best = append(best, entry)
			}

			if remainingQty == 0 {
				break
			}
		}

	default:
		return nil, fmt.Errorf("unknown order preference %v", d.orderPref)
	}

	return best, nil
}
