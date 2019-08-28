package book

import (
	"testing"
)

type Order struct {
	uid  string
	rate float64
}

var _ OrderRater = (*Order)(nil)

func (o *Order) UID() string {
	return o.uid
}

func (o *Order) Rate() float64 {
	return o.rate
}

func (o *Order) String() string {
	return o.UID()
}

var (
	orders = []*Order{
		{
			uid:  "fakefakefake1324",
			rate: 42,
		},
		{
			uid:  "1324fakefakefake",
			rate: 0.0001,
		},
		{
			uid:  "topDog",
			rate: 123,
		},
	}
)

func TestMinOrderPriorityQueue(t *testing.T) {
	pq := NewMinOrderPQ(2)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[1])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[1])
	}

	best := pq.ExtractBest().(*Order)
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect lowest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}
}

func TestMaxOrderPriorityQueue(t *testing.T) {
	pq := NewMaxOrderPQ(3)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[1])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[1])
	}

	ok = pq.Insert(orders[2])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[2])
	}

	best := pq.ExtractBest().(*Order)
	if best.UID() != orders[2].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}
}

func TestOrderPriorityQueueCapacity(t *testing.T) {
	pq := NewMaxOrderPQ(2)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[1])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[1])
	}

	ok = pq.Insert(orders[2])
	if ok {
		t.Errorf("Should have failed to insert order %v, but succeeded", orders[2])
	}

	best := pq.PeekBest().(*Order)
	if best.UID() != orders[0].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}
}

func TestOrderPriorityQueueNegative_Insert(t *testing.T) {
	pq := NewMinOrderPQ(2)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[0])
	if ok {
		t.Errorf("Inserted duplicate order %v", orders[1])
	}

	ok = pq.Insert(nil)
	if ok {
		t.Errorf("Inserted nil order %v", orders[1])
	}
}

func TestOrderPriorityQueue_Replace(t *testing.T) {
	pq := NewMinOrderPQ(2)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	pq.ReplaceOrder(orders[0], orders[1])
	if pq.Len() != 1 {
		t.Fatalf("expected queue length 1, got %d", pq.Len())
	}
	best := pq.ExtractBest()
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}
}

func TestResetHeap(t *testing.T) {
	pq := NewMaxOrderPQ(2)

	orderEntries := []*orderEntry{
		{
			OrderRater: orders[0],
			heapIdx:    -1,
		},
		{
			OrderRater: orders[1],
			heapIdx:    -1,
		},
	}

	pq.ResetHeap(orderEntries)

	best := pq.ExtractBest()
	if best.UID() != orders[0].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}

	best = pq.ExtractBest()
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %g, UID = %s",
			best.Rate(), best.UID())
	}

	best = pq.ExtractBest()
	if best != nil {
		t.Errorf("Order returned, but queue should be empty.")
	}
}

func TestOrderPriorityQueue_Remove(t *testing.T) {
	pq := NewMaxOrderPQ(2)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[1])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[1])
	}

	pq.RemoveOrder(orders[1])
	if pq.Len() != 1 {
		t.Errorf("Queue length expected %d, got %d", 1, pq.Len())
	}
	remainingUID := pq.PeekBest().UID()
	if remainingUID != orders[0].UID() {
		t.Errorf("Remaining element expected %s, got %s", orders[0].UID(),
			remainingUID)
	}
	pq.RemoveOrderUID(remainingUID)
	if pq.Len() != 0 {
		t.Errorf("Expected empty queue, got %d", pq.Len())
	}
}
