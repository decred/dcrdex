package book

import (
	"encoding/hex"
	"math/rand"
	"sort"
	"testing"

	"github.com/decred/dcrd/crypto/blake256"
)

type Order struct {
	uid  string
	rate uint64
	time int64
}

var _ OrderPricer = (*Order)(nil)

func (o *Order) UID() string {
	return o.uid
}

func (o *Order) Price() uint64 {
	return o.rate
}

func (o *Order) Time() int64 {
	return o.time
}

func (o *Order) String() string {
	return o.UID()
}

var (
	bigList []*Order
	orders  = []*Order{
		{
			uid:  "fakefakefake1324",
			rate: 42000000,
			time: 56789,
		},
		{
			uid:  "1324fakefakefake",
			rate: 10000,
			time: 56789,
		},
		{
			uid:  "fakefakefake1324OLDER",
			rate: 42000000,
			time: 45678,
		},
		{
			uid:  "topDog",
			rate: 123000000,
			time: 56789,
		},
	}
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomHash() [32]byte {
	return blake256.Sum256(randomBytes(32))
}

func genBigList() {
	if bigList != nil {
		return
	}
	// var b [8]byte
	// crand.Read(b[:])
	// seed := int64(binary.LittleEndian.Uint64(b[:]))
	seed := int64(-3405439173988651889)
	rand.Seed(seed)

	refTime := int64(1567100226)

	listSize := 1000000
	bigList = make([]*Order, 0, listSize)
	for i := 0; i < listSize; i++ {
		uid := randomHash()
		order := &Order{
			uid:  hex.EncodeToString(uid[:]),
			rate: uint64(rand.Int63n(90000000)),
			time: rand.Int63n(240) + refTime,
		}
		if (i+1)%(listSize/400) == 0 {
			order.rate = bigList[i/2].rate
		}
		bigList = append(bigList, order)
	}
}

func TestLargeOrderMaxPriorityQueue(t *testing.T) {
	genBigList()

	// Max oriented queue
	pq := NewMaxOrderPQ(uint32(len(bigList) * 3 / 2))
	for _, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %v", o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	initLen := pq.Len()
	best := pq.ExtractBest()
	allOrders := make([]OrderPricer, 0, initLen)
	allOrders = append(allOrders, best)

	lastTime := best.Time()
	lastRate := best.Price()
	rates := make([]uint64, initLen)
	rates[0] = lastRate

	lastLen := pq.Len()
	i := int(1) // already popped 0
	for pq.Len() > 0 {
		best = pq.ExtractBest()
		allOrders = append(allOrders, best)
		rate := best.Price()
		if rate > lastRate {
			t.Fatalf("Current rate %d > last rate %d. Should be less.",
				rate, lastRate)
		}
		thisTime := best.Time()
		if rate == lastRate && thisTime < lastTime {
			t.Fatalf("Orders with the same rate; current time %d < last time %d. Should be greater.",
				thisTime, lastTime)
		}
		lastRate = rate
		lastTime = thisTime

		rates[i] = rate
		i++

		if pq.Len() != lastLen-1 {
			t.Fatalf("Queue length failed to shrink by 1.")
		}
		lastLen = pq.Len()
	}

	// Ensure sorted in a different way.
	sorted := sort.SliceIsSorted(rates, func(i, j int) bool {
		return rates[j] < rates[i]
	})
	if !sorted {
		t.Errorf("Rates should have been sorted.")
	}

	pq.Reset(allOrders)
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}

	pq.Reheap()
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}
}

func TestLargeOrderMinPriorityQueue(t *testing.T) {
	genBigList()

	// Min oriented queue
	pq := NewMinOrderPQ(uint32(len(bigList) * 3 / 2))
	for _, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %v", o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	initLen := pq.Len()
	best := pq.ExtractBest()
	allOrders := make([]OrderPricer, 0, initLen)
	allOrders = append(allOrders, best)

	lastTime := best.Time()
	lastRate := best.Price()
	rates := make([]uint64, initLen)
	rates[0] = lastRate

	lastLen := pq.Len()
	i := int(1) // already popped 0
	for pq.Len() > 0 {
		best = pq.ExtractBest()
		allOrders = append(allOrders, best)
		rate := best.Price()
		if rate < lastRate {
			t.Fatalf("Current (%d) rate %d < last rate %d. Should be greater.",
				i, rate, lastRate)
		}
		thisTime := best.Time()
		if rate == lastRate && thisTime < lastTime {
			t.Fatalf("Orders with the same rate; current time %d < last time %d. Should be greater.",
				thisTime, lastTime)
		}
		lastRate = rate
		lastTime = thisTime

		rates[i] = rate
		i++

		if pq.Len() != lastLen-1 {
			t.Fatalf("Queue length failed to shrink by 1.")
		}
		lastLen = pq.Len()
	}

	// Ensure sorted in a different way.
	sorted := sort.SliceIsSorted(rates, func(i, j int) bool {
		return rates[i] < rates[j]
	})
	if !sorted {
		t.Errorf("Rates should have been sorted.")
	}

	pq.Reset(allOrders)
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}

	pq.Reheap()
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}
}

func TestMinOrderPriorityQueue(t *testing.T) {
	pq := NewMinOrderPQ(4)

	for _, o := range orders {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest().(*Order)
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect lowest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestMaxOrderPriorityQueue(t *testing.T) {
	pq := NewMaxOrderPQ(4)

	for _, o := range orders {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest().(*Order)
	if best.UID() != orders[3].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestMaxOrderPriorityQueue_TieRate(t *testing.T) {
	pq := NewMaxOrderPQ(4)

	for _, o := range orders[:3] {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest().(*Order)
	//t.Log(best.String()) // the older order
	if best.UID() != orders[2].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
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
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
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

	ok = pq.ReplaceOrder(orders[0], orders[1])
	if !ok {
		t.Fatalf("failed to ReplaceOrder for %v <- %v", orders[0], orders[1])
	}
	if pq.Len() != 1 {
		t.Fatalf("expected queue length 1, got %d", pq.Len())
	}
	best := pq.ExtractBest()
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestResetHeap(t *testing.T) {
	pq := NewMaxOrderPQ(2)

	orderEntries := []*orderEntry{
		{
			OrderPricer: orders[0],
			heapIdx:     -1,
		},
		{
			OrderPricer: orders[1],
			heapIdx:     -1,
		},
	}

	pq.resetHeap(orderEntries)

	best := pq.ExtractBest()
	if best.UID() != orders[0].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}

	best = pq.ExtractBest()
	if best.UID() != orders[1].uid {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
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

func TestOrderPQMin_Worst(t *testing.T) {
	genBigList()

	pq0 := NewMinOrderPQ(4)
	worst := pq0.Worst()
	if worst != nil {
		t.Errorf("Worst for an empty queue should be nil, got %v", worst)
	}

	pq1 := NewMinOrderPQ(4)
	if !pq1.Insert(bigList[0]) {
		t.Fatalf("Failed to insert order %v", bigList[0])
	}
	worst = pq1.Worst()
	if worst.UID() != bigList[0].UID() {
		t.Errorf("Worst failed to return the only order in the queue, got %v", worst)
	}

	// Min oriented queue
	pq := NewMinOrderPQ(uint32(len(bigList) * 3 / 2))
	for _, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %v", o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	sort.Slice(bigList, func(i, j int) bool {
		if bigList[i].Price() == bigList[j].Price() {
			return bigList[i].Time() < bigList[j].Time()
		}
		return bigList[i].Price() < bigList[j].Price()
	})

	// Worst for a min queue is highest rate.
	worst = pq.Worst()
	if worst.UID() != bigList[len(bigList)-1].UID() {
		t.Errorf("Incorrect worst order. Got %s, expected %s", worst.UID(), bigList[len(bigList)-1].UID())
	}
}

func TestOrderPQMax_Worst(t *testing.T) {
	genBigList()

	// Max oriented queue
	pq := NewMaxOrderPQ(uint32(len(bigList) * 3 / 2))
	for _, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %v", o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	sort.Slice(bigList, func(j, i int) bool {
		if bigList[i].Price() == bigList[j].Price() {
			return bigList[i].Time() < bigList[j].Time()
		}
		return bigList[i].Price() < bigList[j].Price()
	})

	t.Log(bigList[0].Price(), bigList[len(bigList)-1].Price(), pq.PeekBest().Price())

	// Worst for a min queue is highest rate.
	worst := pq.Worst()
	if worst.UID() != bigList[len(bigList)-1].UID() {
		t.Errorf("Incorrect worst order. Got %s, expected %s", worst.UID(), bigList[len(bigList)-1].UID())
	}
}

func TestOrderPQMax_leafNodes(t *testing.T) {
	genBigList()

	// Max oriented queue
	newQ := func(list []*Order) *OrderPQ {
		pq := NewMaxOrderPQ(uint32(len(list) * 3 / 2))
		for _, o := range list {
			ok := pq.Insert(o)
			if !ok {
				t.Fatalf("Failed to insert order %v", o)
			}
		}
		return pq
	}

	for sz := 0; sz < 131; sz++ {
		list := bigList[:sz]
		pq := newQ(list)
		leaves := pq.leafNodes()
		total := pq.Count()
		expectedNum := total / 2
		if total%2 != 0 {
			expectedNum++
		}
		if len(leaves) != expectedNum {
			t.Errorf("Incorrect number of leaf nodes. Got %d, expected %d",
				len(leaves), expectedNum)
		}
	}
}
