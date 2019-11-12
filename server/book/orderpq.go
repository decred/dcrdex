// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"container/heap"
	"fmt"
	"sort"
	"sync"

	"github.com/decred/dcrdex/dex/order"
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
	mtx      sync.RWMutex
	oh       orderHeap
	capacity uint32
	lessFn   func(bi, bj *order.LimitOrder) bool
	orders   map[string]*orderEntry
}

// Copy makes a deep copy of the OrderPQ. The orders are the same; each
// orderEntry is new. The capacity and lessFn are the same.
func (pq *OrderPQ) Copy() *OrderPQ {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.copy(pq.capacity)
}

// Realloc changes the capacity of the OrderPQ by making a deep copy with the
// specified capacity.
func (pq *OrderPQ) Realloc(newCap uint32) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	newPQ := pq.copy(newCap)
	pq.capacity = newCap
	pq.orders = newPQ.orders
	pq.oh = newPQ.oh
}

// copy makes a deep copy of the OrderPQ. The orders are the same; each
// orderEntry is new. The lessFn is the same. This function is not thread-safe.
func (pq *OrderPQ) copy(newCap uint32) *OrderPQ {
	// Deep copy the order heap.
	orderHeap := make(orderHeap, 0, newCap)
	for _, oe := range pq.oh {
		orderHeap = append(orderHeap, &orderEntry{
			oe.order,
			oe.heapIdx,
		})
	}

	// Deep copy the orders map.
	orders := make(map[string]*orderEntry, newCap)
	for _, oe := range orderHeap {
		orders[oe.order.UID()] = oe
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

// Orders copies all orders, sorted with the lessFn. To avoid modifying the
// OrderPQ or any of the data fields, a deep copy of the OrderPQ is made and
// ExtractBest is called until all entries are extracted.
func (pq *OrderPQ) Orders() []*order.LimitOrder {
	pq.mtx.RLock()

	// To use the configured lessFn, make a temporary OrderPQ with just the
	// lessFn and orderHeap set. Do not use the constructor since we do not care
	// about the orders map or capacity.
	pqTmp := OrderPQ{
		lessFn: pq.lessFn,
		oh:     make(orderHeap, len(pq.oh)),
	}
	copy(pqTmp.oh, pq.oh)
	pq.mtx.RUnlock()

	// Sort the orderHeap, which implements sort.Interface with the configured
	// lessFn.
	sort.Sort(&pqTmp)

	// Extract the LimitOrder from each orderEntry in heap.
	orders := make([]*order.LimitOrder, len(pqTmp.oh))
	for i := range pqTmp.oh {
		orders[i] = pqTmp.oh[i].order
	}
	return orders

	// Copy heap and extract N.
	//return pq.OrdersN(len(pq.oh))
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
	return pq.lessFn(pq.oh[i].order, pq.oh[j].order)
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
	lo, ok := ord.(*order.LimitOrder)
	if !ok || lo == nil {
		fmt.Printf("Failed to push an order: %v", ord)
		return
	}

	entry := &orderEntry{
		order:   lo,
		heapIdx: len(pq.oh),
	}

	uid := entry.order.UID()
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
	ord := old[n-1] // heap.Pop put the best value at the end and reheaped without it
	ord.heapIdx = -1
	pq.oh = old[0 : n-1]
	delete(pq.orders, ord.order.UID())
	return ord.order
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
	return bi.Price() < bj.Price()
}

// GreaterByPrice defines a higher priority as having a higher price rate.
func GreaterByPrice(bi, bj *order.LimitOrder) bool {
	return bi.Price() > bj.Price()
}

// LessByPriceThenTime defines a higher priority as having a lower price rate,
// with older orders breaking any tie.
func LessByPriceThenTime(bi, bj *order.LimitOrder) bool {
	if bi.Price() == bj.Price() {
		return bi.Time() < bj.Time()
	}
	return LessByPrice(bi, bj)
}

// GreaterByPriceThenTime defines a higher priority as having a higher price
// rate, with older orders breaking any tie.
func GreaterByPriceThenTime(bi, bj *order.LimitOrder) bool {
	if bi.Price() == bj.Price() {
		return bi.Time() < bj.Time()
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

// Reset creates a fresh queue given the input []OrderPricer. For every element
// in the queue, Reset resets the heap index. The heap is then heapified. The
// input slice is not modifed.
func (pq *OrderPQ) Reset(orders []*order.LimitOrder) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	pq.oh = make([]*orderEntry, 0, len(orders))
	pq.orders = make(map[string]*orderEntry, len(pq.oh))
	for i, o := range orders {
		entry := &orderEntry{
			order:   o,
			heapIdx: i,
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
		pq.orders[oe.order.UID()] = oe
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
func (pq *OrderPQ) Insert(ord *order.LimitOrder) bool {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if pq.capacity == 0 {
		return false
	}

	if ord == nil || ord.UID() == "" {
		return false
	}

	uid := ord.UID()
	if oe, ok := pq.orders[uid]; ok {
		fmt.Println(oe.order, uid)
		return false
	}

	// At capacity
	if int(pq.capacity) <= pq.Len() {
		return false
	}

	// With room to grow, append at bottom and bubble up. Note that
	// (*OrderPQ).Push will update the OrderPQ.orders map.
	heap.Push(pq, ord)
	return true
}

// ReplaceOrder will update the specified OrderPricer, which must be in the
// queue, and then restores heapiness.
func (pq *OrderPQ) ReplaceOrder(old *order.LimitOrder, new *order.LimitOrder) bool {
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
	entry.order = new
	pq.orders[newUID] = entry

	heap.Fix(pq, entry.heapIdx)
	return true
}

// RemoveOrder attempts to remove the provided order from the priority queue
// based on it's UID.
func (pq *OrderPQ) RemoveOrder(r *order.LimitOrder) (*order.LimitOrder, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[r.UID()])
}

// RemoveOrderUID attempts to remove the order with the given UID from the
// priority queue.
func (pq *OrderPQ) RemoveOrderUID(uid string) (*order.LimitOrder, bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	return pq.removeOrder(pq.orders[uid])
}

// removeOrder removes the specified orderEntry from the queue. This function is
// NOT thread-safe.
func (pq *OrderPQ) removeOrder(o *orderEntry) (*order.LimitOrder, bool) {
	if o != nil && o.heapIdx >= 0 && o.heapIdx < pq.Len() {
		// Only remove the order if it is really in the queue.
		uid := o.order.UID()
		removed := pq.oh[o.heapIdx].order
		if removed.UID() == uid {
			delete(pq.orders, uid)
			pq.removeIndex(o.heapIdx)
			return removed, true
		}
		log.Warnf("Tried to remove an order that was NOT in the PQ. ID: %s",
			o.order.UID())
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
