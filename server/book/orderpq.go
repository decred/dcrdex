// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package book

import (
	"container/heap"
	"fmt"
	"sync"
)

// OrderRater is the type stored in the priority queue.
type OrderRater interface {
	UID() string
	Rate() float64
	Time() int64
}

type orderEntry struct {
	OrderRater
	heapIdx int
}

type orderHeap []*orderEntry

// OrderPQ is a priority queue for orders, provided as OrderRaters, based on
// rate. A max-oriented queue with highest rates on top is constructed via
// NewMaxOrderPQ, while a min-oriented queue is constructed via NewMinOrderPQ.
type OrderPQ struct {
	mtx      sync.Mutex
	oh       orderHeap
	capacity uint32
	lessFn   func(bi, bj OrderRater) bool
	orders   map[string]*orderEntry
}

// NewMinOrderPQ is the constructor for OrderPQ that initializes an empty heap
// with the given capacity, and sets the default LessFn for a min heap. Use
// OrderPQ.SetLessFn to redefine the comparator.
func NewMinOrderPQ(capacity uint32) *OrderPQ {
	return newOrderPQ(capacity, LessByRateThenTime)
}

// NewMaxOrderPQ is the constructor for OrderPQ that initializes an empty heap
// with the given capacity, and sets the default LessFn for a max heap. Use
// OrderPQ.SetLessFn to redefine the comparator.
func NewMaxOrderPQ(capacity uint32) *OrderPQ {
	return newOrderPQ(capacity, GreaterByRateThenTime)
}

func newOrderPQ(cap uint32, lessFn func(bi, bj OrderRater) bool) *OrderPQ {
	return &OrderPQ{
		oh:       orderHeap{},
		capacity: cap,
		lessFn:   lessFn,
		orders:   make(map[string]*orderEntry, cap),
	}
}

// Satisfy heap.Inferface

// Len is require for heap.Interface
func (pq *OrderPQ) Len() int {
	return len(pq.oh)
}

// Less performs the comparison priority(i) vs. priority(j). Use
// OrderPQ.SetLessFn to define the desired behavior for the orderEntry heap[i]
// and heap[j].
func (pq *OrderPQ) Less(i, j int) bool {
	return pq.lessFn(pq.oh[i], pq.oh[j])
}

// Swap swaps the orderEntry at i and j. This is used by container/heap.
func (pq *OrderPQ) Swap(i, j int) {
	pq.oh[i], pq.oh[j] = pq.oh[j], pq.oh[i]
	pq.oh[i].heapIdx = i
	pq.oh[j].heapIdx = j
}

// SetLessFn sets the function called by Less. The input lessFn must accept two
// *orderEntry and return a bool, unlike Less, which accepts heap indexes i, j.
// This allows to define a comparator without requiring a heap.
func (pq *OrderPQ) SetLessFn(lessFn func(bi, bj OrderRater) bool) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.lessFn = lessFn
}

// LessByRate defines a higher priority as having a lower rate.
func LessByRate(bi, bj OrderRater) bool {
	return bi.Rate() < bj.Rate()
}

// GreaterByRate defines a higher priority as having a higher rate.
func GreaterByRate(bi, bj OrderRater) bool {
	return bi.Rate() > bj.Rate()
}

// LessByRateThenTime defines a higher priority as having a lower rate, with
// older orders breaking any tie.
func LessByRateThenTime(bi, bj OrderRater) bool {
	if bi.Rate() == bj.Rate() {
		return bi.Time() < bj.Time()
	}
	return LessByRate(bi, bj)
}

// GreaterByRateThenTime defines a higher priority as having a higher rate, with
// older orders breaking any tie.
func GreaterByRateThenTime(bi, bj OrderRater) bool {
	if bi.Rate() == bj.Rate() {
		return bi.Time() < bj.Time()
	}
	return GreaterByRate(bi, bj)
}

// Push an order (a OrderRater). Use heap.Push, not this directly.
func (pq *OrderPQ) Push(ord interface{}) {
	rater, ok := ord.(OrderRater)
	if !ok || rater == nil {
		fmt.Printf("Failed to push an order: %v", ord)
		return
	}

	entry := &orderEntry{
		OrderRater: rater,
		heapIdx:    len(pq.oh),
	}

	uid := entry.UID()
	if pq.orders[uid] != nil {
		fmt.Printf("Attempted to push existing order: %v", ord)
		return
	}

	pq.orders[uid] = entry

	pq.oh = append(pq.oh, entry)
}

// Pop will return an interface{} that may be cast to OrderRater (or the
// underlying concrete type). Use heap.Pop or OrderPQ.PopBest, not this.
func (pq *OrderPQ) Pop() interface{} {
	n := pq.Len()
	old := pq.oh
	order := old[n-1] // heap.Pop put the best value at the end and reheaped without it
	order.heapIdx = -1
	pq.oh = old[0 : n-1]
	delete(pq.orders, order.UID())
	return order.OrderRater
}

// ExtractBest a.k.a. pop removes the highest priority order from the queue, and
// returns it.
func (pq *OrderPQ) ExtractBest() OrderRater {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(OrderRater)
}

// PeekBest returns the highest priority order without removing it from the
// queue.
func (pq *OrderPQ) PeekBest() OrderRater {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	if pq.Len() == 0 {
		return nil
	}
	return pq.oh[0].OrderRater
}

// Reset creates a fresh queue given the input []OrderRater. For every
// element in the queue, Reset resets the heap index. The heap is then
// heapified. The input slice is note modifed.
func (pq *OrderPQ) Reset(orders []OrderRater) {
	pq.oh = make([]*orderEntry, 0, len(orders))
	for i, o := range orders {
		pq.oh = append(pq.oh, &orderEntry{
			OrderRater: o,
			heapIdx:    i,
		})
	}

	pq.orders = make(map[string]*orderEntry, len(pq.oh))

	// Do not call Reheap unless you want a deadlock.
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
func (pq *OrderPQ) Insert(rater OrderRater) bool {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()

	if pq.capacity == 0 {
		return false
	}

	if rater == nil || rater.UID() == "" {
		return false
	}

	if pq.orders[rater.UID()] != nil {
		return false
	}

	// At capacity
	if int(pq.capacity) <= pq.Len() {
		return false
	}

	// With room to grow, append at bottom and bubble up. Note that
	// (*OrderPQ).Push will update the OrderPQ.orders map.
	heap.Push(pq, rater)
	return true
}

// ReplaceOrder will update the specified OrderRater, which must be in the
// queue. This function is NOT thread-safe.
func (pq *OrderPQ) ReplaceOrder(old OrderRater, new OrderRater) {
	if old == nil || new == nil {
		return
	}

	oldUID := old.UID()
	entry := pq.orders[oldUID]

	newUID := new.UID()

	if oldUID == newUID {
		return
	}

	delete(pq.orders, oldUID)
	entry.OrderRater = new
	pq.orders[newUID] = entry

	heap.Fix(pq, entry.heapIdx)
}

// RemoveOrder attempts to remove the provided order from the priority queue
// based on it's UID.
func (pq *OrderPQ) RemoveOrder(r OrderRater) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.removeOrder(pq.orders[r.UID()])
}

// RemoveOrderUID attempts to remove the order with the given UID from the
// priority queue.
func (pq *OrderPQ) RemoveOrderUID(uid string) {
	pq.mtx.Lock()
	defer pq.mtx.Unlock()
	pq.removeOrder(pq.orders[uid])
}

// removeOrder removes the specified orderEntry from the queue.
func (pq *OrderPQ) removeOrder(o *orderEntry) {
	if o != nil && o.heapIdx >= 0 && o.heapIdx < pq.Len() {
		// Only remove the order if it is really in the queue.
		uid := o.UID()
		if pq.oh[o.heapIdx].UID() == uid {
			delete(pq.orders, uid)
			pq.removeIndex(o.heapIdx)
			return
		}
		fmt.Printf("Tried to remove an order that was NOT in the PQ. ID: %s",
			o.UID())
	}
}

// removeIndex removes the orderEntry at the specified position in the heap.
// This function is NOT thread-safe.
func (pq *OrderPQ) removeIndex(idx int) {
	heap.Remove(pq, idx)
}
