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
	rate float64
	time int64
}

var _ OrderRater = (*Order)(nil)

func (o *Order) UID() string {
	return o.uid
}

func (o *Order) Rate() float64 {
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
			rate: 42,
			time: 56789,
		},
		{
			uid:  "1324fakefakefake",
			rate: 0.0001,
			time: 56789,
		},
		{
			uid:  "topDog",
			rate: 123,
			time: 56789,
		},
		{
			uid:  "fakefakefake1324OLDER",
			rate: 42,
			time: 45678,
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
			rate: rand.Float64() * 4,
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

	// f, err := os.Create("insert.pprof")
	// if err != nil {
	// 	t.Fatal("poo")
	// }
	// pprof.StartCPUProfile(f)
	// defer pprof.StopCPUProfile()

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
	allOrders := make([]OrderRater, 0, initLen)
	allOrders = append(allOrders, best)

	lastTime := best.Time()
	lastRate := best.Rate()
	rates := make([]float64, initLen)
	rates[0] = lastRate

	lastLen := pq.Len()
	i := int(1) // already popped 0
	for pq.Len() > 0 {
		best = pq.ExtractBest()
		allOrders = append(allOrders, best)
		rate := best.Rate()
		if rate > lastRate {
			t.Fatalf("Current rate %g > last rate %g. Should be less.",
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
	if pq.PeekBest().Rate() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}

	pq.Reheap()
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Rate() != rates[0] {
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
	allOrders := make([]OrderRater, 0, initLen)
	allOrders = append(allOrders, best)

	lastTime := best.Time()
	lastRate := best.Rate()
	rates := make([]float64, initLen)
	rates[0] = lastRate

	lastLen := pq.Len()
	i := int(1) // already popped 0
	for pq.Len() > 0 {
		best = pq.ExtractBest()
		allOrders = append(allOrders, best)
		rate := best.Rate()
		if rate < lastRate {
			t.Fatalf("Current (%d) rate %g < last rate %g. Should be greater.",
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
	if pq.PeekBest().Rate() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}

	pq.Reheap()
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Rate() != rates[0] {
		t.Errorf("Heap Reset failed.")
	}
}

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

	pq.resetHeap(orderEntries)

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
