// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"bytes"
	"container/heap"
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

type orderEntry struct {
	order   *order.LimitOrder
	heapIdx int
}

type orderHeap []*orderEntry

// OrderPQ is a priority queue for orders, provided as orders, based on
// price rate. A max-oriented queue with highest rates on top is constructed via
// NewMaxOrderPQ, while a min-oriented queue is constructed via NewMinOrderPQ.
type OrderPQ struct {
	mtx        sync.RWMutex
	oh         orderHeap
	capacity   uint32
	lessFn     func(bi, bj *order.LimitOrder) bool
	orders     map[order.OrderID]*orderEntry
	userOrders map[account.AccountID]map[order.OrderID]*order.LimitOrder
}

// Copy makes a deep copy of the OrderPQ. The orders are the same; each
// orderEntry is new. The capacity and lessFn are the same.
func (pq *OrderPQ) Copy() *OrderPQ {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.copy(pq.capacity)
}

// realloc changes the capacity of the OrderPQ by making a deep copy with the
// specified capacity. The specified capacity must not be less the current queue
// length. Truncation is not supported; the consumer should first extract
// entries before attempting to reallocate to a smaller capacity.
func (pq *OrderPQ) realloc(newCap uint32) {
	if len(pq.oh) > int(newCap) {
		panic(fmt.Sprintf("(*OrderPQ).Realloc: new cap %d < current utilization %d",
			newCap, len(pq.oh)))
	}
	newPQ := pq.copy(newCap)
	pq.capacity = newCap
	pq.orders = newPQ.orders
	pq.oh = newPQ.oh
	pq.userOrders = newPQ.userOrders
}

// Cap returns the current capacity of the OrderPQ.
func (pq *OrderPQ) Cap() uint32 {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()
	return pq.capacity
}

func (pq *OrderPQ) push(oe *orderEntry) {
	// Append the entry to the heap slice.
	pq.oh = append(pq.oh, oe)
	// Store it in the orders and userOrders maps.
	lo := oe.order
	oid := lo.ID()
	pq.orders[oid] = oe
	if uos, found := pq.userOrders[lo.AccountID]; found {
		uos[oid] = lo // alloc_space hot spot
	} else {
		pq.userOrders[lo.AccountID] = map[order.OrderID]*order.LimitOrder{oid: lo}
	}
}

// copy makes a deep copy of the OrderPQ. The orders are the same; each
// orderEntry is new. The lessFn is the same. This function is not thread-safe.
// The CALLER (e.g. realloc) must be sure the new capacity is sufficient.
func (pq *OrderPQ) copy(newCap uint32) *OrderPQ {
	if len(pq.oh) > int(newCap) {
		panic(fmt.Sprintf("len %d > newCap %d", len(pq.oh), int(newCap)))
	}
	// Initialize the new OrderPQ.
	newPQ := newOrderPQ(newCap, pq.lessFn)
	newPQ.userOrders = make(map[account.AccountID]map[order.OrderID]*order.LimitOrder, len(pq.userOrders))
	for aid, uos := range pq.userOrders {
		newPQ.userOrders[aid] = make(map[order.OrderID]*order.LimitOrder, len(uos)) // actual *LimitOrders copied in push
	}

	// Deep copy the order heap, and recreate the maps.
	for _, oe := range pq.oh {
		entry := &orderEntry{ // alloc_objects hot spot
			oe.order,
			oe.heapIdx,
		}
		newPQ.push(entry)
	}

	// Since the heap is copied in the same order, and with the same heap
	// indexes, it should not be necessary to reheap. But do it to be safe.
	heap.Init(newPQ)

	return newPQ
}

// UnfilledForUser retrieves all completely unfilled orders for a given user.
func (pq *OrderPQ) UnfilledForUser(user account.AccountID) []*order.LimitOrder {
	pq.mtx.RLock()
	var orders []*order.LimitOrder
	for _, oe := range pq.oh {
		if oe.order.AccountID == user && oe.order.Filled() == 0 {
			orders = append(orders, oe.order)
		}
	}
	pq.mtx.RUnlock()
	return orders
}

// Orders copies all orders, sorted with the lessFn. The OrderPQ is unmodified.
func (pq *OrderPQ) Orders() []*order.LimitOrder {
	// Deep copy the orders.
	pq.mtx.RLock()
	orders := make([]*order.LimitOrder, len(pq.oh))
	for i, oe := range pq.oh {
		orders[i] = oe.order
	}
	pq.mtx.RUnlock()

	// Sort the orders with pq.lessFn.
	sort.Slice(orders, func(i, j int) bool {
		return pq.lessFn(orders[i], orders[j])
	})

	return orders
}

// OrdersN copies the N best orders, sorted with the lessFn. To avoid modifying
// the OrderPQ or any of the data fields, a deep copy of the OrderPQ is made and
// ExtractBest is called until the requested number of entries are extracted.
func (pq *OrderPQ) OrdersN(count int) []*order.LimitOrder {
	// Make a deep copy since extracting all the orders in sorted order (i.e.
	// heap sort) modifies the heap, and the heapIdx of each orderEntry.
	tmp := pq.Copy()
	return tmp.ExtractN(count)
}

// ExtractN extracts the N best orders, sorted with the lessFn. ExtractBest is
// called until the requested number of entries are extracted. Thus, the OrderPQ
// is reduced in length by count, or the length of the heap, whichever is
// shorter. To avoid modifying the queue, use Orders or OrdersN.
func (pq *OrderPQ) ExtractN(count int) []*order.LimitOrder {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	sz := len(pq.oh)
	if sz < count {
		count = sz
	}
	if count < 1 {
		return nil
	}
	orders := make([]*order.LimitOrder, 0, count)
	for len(orders) < count {
		orders = append(orders, pq.ExtractBest())
	}
	return orders
}

// NewMinOrderPQ is the constructor for OrderPQ that initializes an empty heap
// with the given capacity, and sets the default LessFn for a min heap. Use
// OrderPQ.SetLessFn to redefine the comparator.
func NewMinOrderPQ(capacity uint32) *OrderPQ {
	return newOrderPQ(capacity, LessByPriceThenTime)
}

// NewMaxOrderPQ is the constructor for OrderPQ that initializes an empty heap
// with the given capacity, and sets the default LessFn for a max heap. Use
// OrderPQ.SetLessFn to redefine the comparator.
func NewMaxOrderPQ(capacity uint32) *OrderPQ {
	return newOrderPQ(capacity, GreaterByPriceThenTime)
}

func newOrderPQ(cap uint32, lessFn func(bi, bj *order.LimitOrder) bool) *OrderPQ {
	return &OrderPQ{
		oh:         make(orderHeap, 0, cap),
		capacity:   cap,
		lessFn:     lessFn,
		orders:     make(map[order.OrderID]*orderEntry, cap),
		userOrders: make(map[account.AccountID]map[order.OrderID]*order.LimitOrder),
	}
}

const (
	minCapIncrement = 4096
	deallocThresh   = 10 * minCapIncrement
)

// capForUtilization suggests a capacity for a certain utilization. It is
// computed as sz plus the larger of sz/8 and minCapIncrement.
func capForUtilization(sz int) uint32 {
	inc := sz / 8
	if inc < minCapIncrement {
		inc = minCapIncrement
	}
	return uint32(sz + inc)
}

// Count returns the number of orders in the queue.
func (pq *OrderPQ) Count() int {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return len(pq.orders)
}

// Satisfy heap.Inferface (Len, Less, Swap, Push, Pop). These functions are only
// to be used by the container/heap functions via other thread-safe OrderPQ
// methods. These are not safe for concurrent use.

// Len is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Len() int {
	return len(pq.oh)
}

// Less performs the comparison priority(i) vs. priority(j). Use
// OrderPQ.SetLessFn to define the desired behavior for the orderEntry heap[i]
// and heap[j]. Less is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Less(i, j int) bool {
	return pq.lessFn(pq.oh[i].order, pq.oh[j].order)
}

// Swap swaps the orderEntry at i and j. This is used by container/heap. Swap is
// required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Swap(i, j int) {
	pq.oh[i], pq.oh[j] = pq.oh[j], pq.oh[i]
	pq.oh[i].heapIdx = i
	pq.oh[j].heapIdx = j
}

// Push an order, which must be a *LimitOrder. Use heap.Push, not this directly.
// Push is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Push(ord interface{}) {
	lo, ok := ord.(*order.LimitOrder)
	if !ok || lo == nil {
		fmt.Printf("Failed to push an order: %v", ord)
		return
	}

	if pq.orders[lo.ID()] != nil {
		fmt.Printf("Attempted to push existing order: %v", ord)
		return
	}

	entry := &orderEntry{
		order:   lo,
		heapIdx: len(pq.oh),
	}
	pq.push(entry)
}

// Pop will return an interface{} that may be cast to *LimitOrder. Use heap.Pop,
// Extract*, or Remove*, not this method. Pop is required for heap.Interface. It
// is not thread-safe.
func (pq *OrderPQ) Pop() interface{} {
	// heap.Pop put the best value at the end and reheaped without it. Now
	// actually pop it off the heap's slice.
	n := pq.Len()
	oe := pq.oh[n-1]
	oe.heapIdx = -1
	pq.oh[n-1] = nil
	pq.oh = pq.oh[:n-1]

	// Remove the order from the orders and userOrders maps.
	lo := oe.order
	oid := lo.ID()
	delete(pq.orders, oid)
	user := lo.AccountID
	if uos, found := pq.userOrders[user]; found {
		if len(uos) == 1 {
			delete(pq.userOrders, user)
		} else {
			delete(uos, oid)
		}
	} else {
		fmt.Printf("(*OrderPQ).Pop: no userOrders for %v found when popping order %v!", user, oid)
	}

	// If the heap has shrunk well below capacity, realloc smaller.
	if pq.capacity > deallocThresh {
		capTarget := capForUtilization(len(pq.oh)) // new cap if we realloc for this utilization
		if pq.capacity > capTarget {               // don't increase
			savings := pq.capacity - capTarget
			if savings > deallocThresh && savings > pq.capacity/3 { // only reduce cap for significant savings
				pq.realloc(capTarget)
			}
		}
	}

	return lo
}

// End heap.Inferface.

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *orderEntry and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *OrderPQ) SetLessFn(lessFn func(bi, bj *order.LimitOrder) bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.lessFn = lessFn
}

// LessByPrice defines a higher priority as having a lower price rate.
func LessByPrice(bi, bj *order.LimitOrder) bool {
	return bi.Rate < bj.Rate
}

// GreaterByPrice defines a higher priority as having a higher price rate.
func GreaterByPrice(bi, bj *order.LimitOrder) bool {
	return bi.Rate > bj.Rate
}

// LessByPriceThenTime defines a higher priority as having a lower price rate,
// with older orders breaking any tie, then OrderID as a last tie breaker.
func LessByPriceThenTime(bi, bj *order.LimitOrder) bool {
	if bi.Rate == bj.Rate {
		ti, tj := bi.Time(), bj.Time()
		if ti == tj {
			// Lexicographical comparison of the OrderIDs requires a slice. This
			// comparison should be exceedingly rare, so the required memory
			// allocations are acceptable.
			idi, idj := bi.ID(), bj.ID()
			return bytes.Compare(idi[:], idj[:]) < 0 // idi < idj
		}
		return ti < tj
	}
	return LessByPrice(bi, bj)
}

// GreaterByPriceThenTime defines a higher priority as having a higher price
// rate, with older orders breaking any tie, then OrderID as a last tie breaker.
func GreaterByPriceThenTime(bi, bj *order.LimitOrder) bool {
	if bi.Rate == bj.Rate {
		ti, tj := bi.Time(), bj.Time()
		if ti == tj {
			// Lexicographical comparison of the OrderIDs requires a slice. This
			// comparison should be exceedingly rare, so the required memory
			// allocations are acceptable.
			idi, idj := bi.ID(), bj.ID()
			return bytes.Compare(idi[:], idj[:]) < 0 // idi < idj
		}
		return ti < tj
	}
	return GreaterByPrice(bi, bj)
}

// ExtractBest a.k.a. pop removes the highest priority order from the queue, and
// returns it.
func (pq *OrderPQ) ExtractBest() *order.LimitOrder {
	return pq.extractBest()
}

// extractBest is the not thread-safe version of ExtractBest
func (pq *OrderPQ) extractBest() *order.LimitOrder {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(*order.LimitOrder)
}

// PeekBest returns the highest priority order without removing it from the
// queue.
func (pq *OrderPQ) PeekBest() *order.LimitOrder {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	if pq.Len() == 0 {
		return nil
	}
	return pq.oh[0].order
}

// Reset creates a fresh queue given the input LimitOrder slice. For every
// element in the queue, this resets the heap index. The heap is then heapified.
// The input slice is not modifed.
func (pq *OrderPQ) Reset(orders []*order.LimitOrder) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	pq.oh = make([]*orderEntry, 0, len(orders))
	pq.orders = make(map[order.OrderID]*orderEntry, len(pq.oh))
	pq.userOrders = make(map[account.AccountID]map[order.OrderID]*order.LimitOrder)
	for i, lo := range orders {
		entry := &orderEntry{
			order:   lo,
			heapIdx: i,
		}
		pq.push(entry)
	}

	heap.Init(pq)
}

// Reheap is a thread-safe shortcut for heap.Init(pq).
func (pq *OrderPQ) Reheap() {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	heap.Init(pq)
}

// Insert will add an element, while respecting the queue's capacity.
//
//	if (have order already), fail
//	else (not at capacity), push the order onto the heap
//
// If the queue is at capacity, it will automatically reallocate with an
// increased capacity. See the Cap method.
func (pq *OrderPQ) Insert(ord *order.LimitOrder) bool {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if ord == nil {
		fmt.Println("(*OrderPQ).Insert: attempting to insert nil *LimitOrder!")
		return false
	}

	// Have order
	if _, found := pq.orders[ord.ID()]; found {
		return false
	}

	// At capacity, reallocate larger.
	if int(pq.capacity) <= len(pq.oh) {
		pq.realloc(capForUtilization(len(pq.oh)))
	}

	// With room to grow, append at bottom and bubble up. Note that
	// (*OrderPQ).Push will update the OrderPQ.orders map.
	heap.Push(pq, ord)
	return true
}

// RemoveOrder attempts to remove the provided order from the priority queue
// based on it's ID.
func (pq *OrderPQ) RemoveOrder(lo *order.LimitOrder) (*order.LimitOrder, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[lo.ID()])
}

// RemoveOrderID attempts to remove the order with the given ID from the
// priority queue.
func (pq *OrderPQ) RemoveOrderID(oid order.OrderID) (*order.LimitOrder, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[oid])
}

// RemoveUserOrders removes all orders from the queue that belong to a user.
func (pq *OrderPQ) RemoveUserOrders(user account.AccountID) (removed []*order.LimitOrder) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	uos, found := pq.userOrders[user]
	if !found {
		return
	}

	removed = make([]*order.LimitOrder, 0, len(uos))
	for oid, lo := range uos {
		pq.removeOrder(pq.orders[oid])
		removed = append(removed, lo)
	}
	return
}

// HaveOrder indicates if an order is in the queue.
func (pq *OrderPQ) HaveOrder(oid order.OrderID) bool {
	return pq.Order(oid) != nil
}

// Order retrieves any existing order in the queue with the given ID.
func (pq *OrderPQ) Order(oid order.OrderID) *order.LimitOrder {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	if oe := pq.orders[oid]; oe != nil {
		return oe.order
	}
	return nil
}

// UserOrderTotals returns the total value and number of booked orders.
func (pq *OrderPQ) UserOrderTotals(user account.AccountID) (amt, count uint64) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	uos, found := pq.userOrders[user]
	if !found {
		return
	}
	for _, lo := range uos {
		amt += lo.Remaining()
		count++
	}
	return
}

// removeOrder removes the specified orderEntry from the queue. This function is
// NOT thread-safe.
func (pq *OrderPQ) removeOrder(o *orderEntry) (*order.LimitOrder, bool) {
	if o != nil && o.heapIdx >= 0 && o.heapIdx < pq.Len() {
		// Only remove the order if it is really in the queue.
		oid := o.order.ID()
		removed := pq.oh[o.heapIdx].order
		if removed.ID() == oid {
			heap.Remove(pq, o.heapIdx) // heap.Pop => (*OrderPQ).Pop removes map entries
			return removed, true
		}
		fmt.Printf("Tried to remove an order that was NOT in the PQ. ID: %s", oid)
	}
	return nil, false
}

func (pq *OrderPQ) leafNodes() []*orderEntry {
	n := len(pq.oh)
	if n == 0 {
		return nil
	}
	numLeaves := n/2 + n%2
	return pq.oh[n-numLeaves:]
}

// Worst returns the worst order (depending on the queue's lessFn) in the queue.
// This is done by scanning the binary heap's leaf nodes since only the best
// order's position (first element) is known, while the only guarantee regarding
// the worst element is that it will not be another node's parent (i.e. it is a
// leaf node).
func (pq *OrderPQ) Worst() *order.LimitOrder {
	pq.mtx.RLock()
	defer pq.mtx.RUnlock()
	// Check the leaf nodes for the worst order according to lessFn.
	leaves := pq.leafNodes()
	switch len(leaves) {
	case 0:
		return nil
	case 1:
		return leaves[0].order
	}
	worst := leaves[0].order
	for i := 0; i < len(leaves)-1; i++ {
		if pq.lessFn(worst, leaves[i+1].order) {
			worst = leaves[i+1].order
		}
	}
	return worst
}
