// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/order"
)

// ord is a time-stamped order ID with a flag indicating if it is a cancel.
type ord struct {
	order.OrderID
	time   int64
	target *order.OrderID
}

// ordsByTimeThenID is used to sort an ord slice in ascending order by time and
// then order ID. This puts the oldest order at the front and latest at the back
// of the slice.
type ordsByTimeThenID []*ord

func (o ordsByTimeThenID) Len() int {
	return len(o)
}

func (o ordsByTimeThenID) Swap(i, j int) {
	o[j], o[i] = o[i], o[j]
}

func (o ordsByTimeThenID) Less(i, j int) bool {
	return less(o[i], o[j])
}

func less(oi, oj *ord) bool {
	if oi.time == oj.time {
		cmp := bytes.Compare(oi.OrderID[:], oj.OrderID[:])
		if cmp == 0 {
			panic("slice contains the more than one instance of a given order")
		}
		return cmp < 0 // ascending (smaller order ID first)
	}
	return oi.time < oj.time // ascending (newest last in slice)
}

// latestOrders manages a list of the latest orders for a user. Its purpose is
// to track cancelation frequency.
type latestOrders struct {
	mtx    sync.Mutex
	cap    int16
	orders []*ord
}

func newLatestOrders(cap int16) *latestOrders {
	return &latestOrders{
		cap:    cap,
		orders: make([]*ord, 0, cap+1), // cap+1 since an old order is always popped after a new one is pushed
	}
}

/*func (lo *latestOrders) addSimple(o *ord) {
	lo.mtx.Lock()
	defer lo.mtx.Unlock()

	// push back, where the latest order goes
	lo.orders = append(lo.orders, o)

	// Should be few if any swaps. This is only to deal with the possibility of
	// adding an order that is not the latest, and equal time stamps.
	sort.Sort(ordsByTimeThenID(lo.orders))

	// Pop one order if the slice was at capacity prior to pushing the new one.
	for len(lo.orders) > int(lo.cap) {
		// pop front, the oldest order
		lo.orders[0] = nil // avoid memory leak
		lo.orders = lo.orders[1:]
	}
}*/

func (lo *latestOrders) add(o *ord) {
	lo.mtx.Lock()
	defer lo.mtx.Unlock()

	// Use sort.Search and insert it as the right spot.
	n := len(lo.orders)
	i := sort.Search(n, func(i int) bool {
		return less(lo.orders[n-1-i], o)
	})
	if i == int(lo.cap) /* i == n && n == int(lo.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	} else {
		// Insert at proper location.
		i = n - i // i-1 is first location that stays
		lo.orders = append(lo.orders[:i], append([]*ord{o}, lo.orders[i:]...)...)
	}

	// if !sort.IsSorted(ordsByTimeThenID(lo.orders)) {
	// 	panic("it's not sorted")
	// }

	// Pop one order if the slice was at capacity prior to pushing the new one.
	if len(lo.orders) > int(lo.cap) {
		// pop front, the oldest order
		lo.orders[0] = nil // avoid memory leak
		lo.orders = lo.orders[1:]
	}

	// if len(lo.orders) > int(lo.cap) {
	// 	panic("still too long")
	// }
}

func (lo *latestOrders) counts() (total, cancels int) {
	lo.mtx.Lock()
	defer lo.mtx.Unlock()

	total = len(lo.orders)
	for _, o := range lo.orders {
		if o.target != nil {
			cancels++
		}
	}

	return
}
