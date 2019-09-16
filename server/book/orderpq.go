// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"container/heap"
	"fmt"
	"sync"
)

// OrderPrice is the type stored in the priority queue.
type OrderPricer interface {
	UID() string
	Price() uint64
	Time() int64
}

type orderEntry struct {
	OrderPricer
	heapIdx int
}

type orderHeap []*orderEntry

// OrderPQ is a priority queue for orders, provided as OrderPricers, based on
// price rate. A max-oriented queue with highest rates on top is constructed via
// NewMaxOrderPQ, while a min-oriented queue is constructed via NewMinOrderPQ.
type OrderPQ struct {
	mtx      sync.Mutex
	oh       orderHeap
	capacity uint32
	lessFn   func(bi, bj OrderPricer) bool
	orders   map[string]*orderEntry
}

// Copy makes a deep copy of the OrderPQ. The OrderPricers are the same; each
// orderEntry is new. The capacity and lessFn are the same.
func (pq *OrderPQ) Copy() *OrderPQ {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.copy(pq.capacity)
}

// Copy makes a deep copy of the OrderPQ with the given capacity. The
// OrderPricers are the same; each orderEntry is new. The lessFn is the same.
func (pq *OrderPQ) Realloc(newCap uint32) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	newPQ := pq.copy(newCap)
	pq.capacity = newCap
	pq.orders = newPQ.orders
	pq.oh = newPQ.oh
}

// copy makes a deep copy of the OrderPQ. The OrderPricers are the same; each
// orderEntry is new. The lessFn is the same. This function is not thread-safe.
func (pq *OrderPQ) copy(newCap uint32) *OrderPQ {
	// Deep copy the order heap.
	orderHeap := make(orderHeap, 0, newCap)
	for _, oe := range pq.oh {
		orderHeap = append(orderHeap, &orderEntry{
			oe.OrderPricer,
			oe.heapIdx,
		})
	}

	// Deep copy the orders map.
	orders := make(map[string]*orderEntry, newCap)
	for _, oe := range orderHeap {
		orders[oe.UID()] = oe
	}

	newPQ := &OrderPQ{
		oh:       orderHeap,
		capacity: newCap,
		lessFn:   pq.lessFn,
		orders:   orders,
	}

	// Since the heap is copied in the same order, and with the same heap
	// indexes, it should not be necessary to reheap. But do it to be safe.
	heap.Init(newPQ)

	return newPQ
}

// Orders copies all orders, sorted with the lessFn. The is heap sort. To avoid
// modifying the OrderPQ or any of the data fields, a deep copy of the OrderPQ
// is made and ExtractBest is called until all entries are extracted.
func (pq *OrderPQ) Orders() []OrderPricer {
	return pq.OrdersN(len(pq.oh))
}

// OrdersN copies the N best orders, sorted with the lessFn. The is heap sort.
// To avoid modifying the OrderPQ or any of the data fields, a deep copy of the
// OrderPQ is made and ExtractBest is called until the entries are extracted.
func (pq *OrderPQ) OrdersN(count int) []OrderPricer {
	// Make a deep copy since extracting all the orders in sorted order (i.e.
	// heap sort) modifies the heap, and the heapIdx of each orderEntry.
	tmp := pq.Copy()
	return tmp.ExtractN(count)
}

// ExtractN extracts the N best orders, sorted with the lessFn. The is heap
// sort. ExtractBest is called until all entries are extracted. Thus, the
// OrderPQ is reduced in length by count, or the length of the heap, whichever
// is shorter. To avoid modifying the queue, use Orders or OrdersN.
func (pq *OrderPQ) ExtractN(count int) []OrderPricer {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	sz := len(pq.oh)
	if sz < count {
		count = sz
	}
	if count < 1 {
		return nil
	}
	orders := make([]OrderPricer, 0, count)
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

func newOrderPQ(cap uint32, lessFn func(bi, bj OrderPricer) bool) *OrderPQ {
	return &OrderPQ{
		oh:       make(orderHeap, 0, cap),
		capacity: cap,
		lessFn:   lessFn,
		orders:   make(map[string]*orderEntry, cap),
	}
}

// Count returns the number of orders in the queue.
func (pq *OrderPQ) Count() int {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return len(pq.orders)
}

// Satisfy heap.Inferface (Len, Less, Swap, Push, Pop). These functions are only
// to be used by the container/heap functions via other thread-safe OrderPQ
// methods. These are not thread safe.

// Len is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Len() int {
	return len(pq.oh)
}

// Less performs the comparison priority(i) vs. priority(j). Use
// OrderPQ.SetLessFn to define the desired behavior for the orderEntry heap[i]
// and heap[j]. Less is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Less(i, j int) bool {
	return pq.lessFn(pq.oh[i], pq.oh[j])
}

// Swap swaps the orderEntry at i and j. This is used by container/heap. Swap is
// required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Swap(i, j int) {
	pq.oh[i], pq.oh[j] = pq.oh[j], pq.oh[i]
	pq.oh[i].heapIdx = i
	pq.oh[j].heapIdx = j
}

// Push an order, which must be an OrderPricer. Use heap.Push, not this directly.
// Push is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Push(ord interface{}) {
	pricer, ok := ord.(OrderPricer)
	if !ok || pricer == nil {
		fmt.Printf("Failed to push an order: %v", ord)
		return
	}

	entry := &orderEntry{
		OrderPricer: pricer,
		heapIdx:     len(pq.oh),
	}

	uid := entry.UID()
	if pq.orders[uid] != nil {
		fmt.Printf("Attempted to push existing order: %v", ord)
		return
	}

	pq.orders[uid] = entry

	pq.oh = append(pq.oh, entry)
}

// Pop will return an interface{} that may be cast to OrderPricer (or the
// underlying concrete type). Use heap.Pop or OrderPQ.ExtractBest, not this. Pop
// is required for heap.Interface. It is not thread-safe.
func (pq *OrderPQ) Pop() interface{} {
	n := pq.Len()
	old := pq.oh
	order := old[n-1] // heap.Pop put the best value at the end and reheaped without it
	order.heapIdx = -1
	pq.oh = old[0 : n-1]
	delete(pq.orders, order.UID())
	return order.OrderPricer
}

// End heap.Inferface.

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *orderEntry and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *OrderPQ) SetLessFn(lessFn func(bi, bj OrderPricer) bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.lessFn = lessFn
}

// LessByPrice defines a higher priority as having a lower price rate.
func LessByPrice(bi, bj OrderPricer) bool {
	return bi.Price() < bj.Price()
}

// GreaterByPrice defines a higher priority as having a higher price rate.
func GreaterByPrice(bi, bj OrderPricer) bool {
	return bi.Price() > bj.Price()
}

// LessByPriceThenTime defines a higher priority as having a lower price rate,
// with older orders breaking any tie.
func LessByPriceThenTime(bi, bj OrderPricer) bool {
	if bi.Price() == bj.Price() {
		return bi.Time() < bj.Time()
	}
	return LessByPrice(bi, bj)
}

// GreaterByPriceThenTime defines a higher priority as having a higher price
// rate, with older orders breaking any tie.
func GreaterByPriceThenTime(bi, bj OrderPricer) bool {
	if bi.Price() == bj.Price() {
		return bi.Time() < bj.Time()
	}
	return GreaterByPrice(bi, bj)
}

// ExtractBest a.k.a. pop removes the highest priority order from the queue, and
// returns it.
func (pq *OrderPQ) ExtractBest() OrderPricer {
	return pq.extractBest()
}

// extractBest is the not thread-safe version of ExtractBest
func (pq *OrderPQ) extractBest() OrderPricer {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(OrderPricer)
}

// PeekBest returns the highest priority order without removing it from the
// queue.
func (pq *OrderPQ) PeekBest() OrderPricer {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	if pq.Len() == 0 {
		return nil
	}
	return pq.oh[0].OrderPricer
}

// Reset creates a fresh queue given the input []OrderPricer. For every element
// in the queue, Reset resets the heap index. The heap is then heapified. The
// input slice is not modifed.
func (pq *OrderPQ) Reset(orders []OrderPricer) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	pq.oh = make([]*orderEntry, 0, len(orders))
	pq.orders = make(map[string]*orderEntry, len(pq.oh))
	for i, o := range orders {
		entry := &orderEntry{
			OrderPricer: o,
			heapIdx:     i,
		}
		pq.oh = append(pq.oh, entry)
		pq.orders[o.UID()] = entry
	}

	heap.Init(pq)
}

// resetHeap creates a fresh queue given the input []*orderEntry. For every
// element in the queue, resetHeap resets the heap index. The heap is then
// heapified. NOTE: the input slice is modifed, but not reordered. A fresh slice
// is created for PQ internal use.
func (pq *OrderPQ) resetHeap(oh []*orderEntry) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	for i := range oh {
		oh[i].heapIdx = i
	}

	pq.oh = make([]*orderEntry, len(oh))
	copy(pq.oh, oh)

	pq.orders = make(map[string]*orderEntry, len(oh))
	for _, oe := range oh {
		pq.orders[oe.UID()] = oe
	}

	// Do not call Reheap unless you want a deadlock.
	heap.Init(pq)
}

// Reheap is a thread-safe shortcut for heap.Init(pq).
func (pq *OrderPQ) Reheap() {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	heap.Init(pq)
}

// Insert will add an element, while respecting the queue's capacity.
// if at capacity, fail
// else (not at capacity)
// 		- heap.Push, which is pq.Push (append at bottom) then heapup
func (pq *OrderPQ) Insert(pricer OrderPricer) bool {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if pq.capacity == 0 {
		return false
	}

	if pricer == nil || pricer.UID() == "" {
		return false
	}

	if pq.orders[pricer.UID()] != nil {
		return false
	}

	// At capacity
	if int(pq.capacity) <= pq.Len() {
		return false
	}

	// With room to grow, append at bottom and bubble up. Note that
	// (*OrderPQ).Push will update the OrderPQ.orders map.
	heap.Push(pq, pricer)
	return true
}

// ReplaceOrder will update the specified OrderPricer, which must be in the
// queue, and then restores heapiness.
func (pq *OrderPQ) ReplaceOrder(old OrderPricer, new OrderPricer) bool {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if old == nil || new == nil {
		return false
	}

	oldUID := old.UID()
	entry := pq.orders[oldUID]
	if entry == nil {
		return false
	}

	newUID := new.UID()
	// if oldUID == newUID {
	// 	return false
	// }
	// Above is commented to update regardless of UID.

	delete(pq.orders, oldUID)
	entry.OrderPricer = new
	pq.orders[newUID] = entry

	heap.Fix(pq, entry.heapIdx)
	return true
}

// RemoveOrder attempts to remove the provided order from the priority queue
// based on it's UID.
func (pq *OrderPQ) RemoveOrder(r OrderPricer) (OrderPricer, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[r.UID()])
}

// RemoveOrderUID attempts to remove the order with the given UID from the
// priority queue.
func (pq *OrderPQ) RemoveOrderUID(uid string) (OrderPricer, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[uid])
}

// removeOrder removes the specified orderEntry from the queue. This function is
// NOT thread-safe.
func (pq *OrderPQ) removeOrder(o *orderEntry) (OrderPricer, bool) {
	if o != nil && o.heapIdx >= 0 && o.heapIdx < pq.Len() {
		// Only remove the order if it is really in the queue.
		uid := o.UID()
		removed := pq.oh[o.heapIdx].OrderPricer
		if removed.UID() == uid {
			delete(pq.orders, uid)
			pq.removeIndex(o.heapIdx)
			return removed, true
		}
		log.Warnf("Tried to remove an order that was NOT in the PQ. ID: %s",
			o.UID())
	}
	return nil, false
}

// removeIndex removes the orderEntry at the specified position in the heap.
// This function is NOT thread-safe.
func (pq *OrderPQ) removeIndex(idx int) {
	heap.Remove(pq, idx)
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
func (pq *OrderPQ) Worst() OrderPricer {
	// Check the leaf nodes for the worst order according to lessFn.
	leaves := pq.leafNodes()
	switch len(leaves) {
	case 0:
		return nil
	case 1:
		return leaves[0].OrderPricer
	}
	worst := leaves[0]
	for i := 0; i < len(leaves)-1; i++ {
		if pq.lessFn(worst, leaves[i+1]) {
			worst = leaves[i+1]
		}
	}
	return worst.OrderPricer
}
