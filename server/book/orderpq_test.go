package book

import (
	"math/rand"
	"sort"
	"testing"

	"decred.org/dcrdex/dex/order"
)

type Order = order.LimitOrder

var (
	bigList []*Order
	orders  = []*Order{
		newLimitOrder(false, 42000000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 10000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 42000000, 2, order.StandingTiF, -1000), // rate dup, different time
		newLimitOrder(false, 123000000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 42000000, 1, order.StandingTiF, 0), // rate and time dup, different OrderID
	}
)

// func randomBytes(len int) []byte {
// 	bytes := make([]byte, len)
// 	rand.Read(bytes)
// 	return bytes
// }

// func randomHash() [32]byte {
// 	return blake256.Sum256(randomBytes(32))
// }

func newFakeAddr() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 35)
	for i := range b {
		b[i] = letters[rand.Int63()%int64(len(letters))]
	}
	b[0], b[1] = 'D', 's' // at least have it resemble an address
	return string(b)
}

func genBigList(listSize int) {
	if bigList != nil {
		return
	}
	// var b [8]byte
	// crand.Read(b[:])
	// seed := int64(binary.LittleEndian.Uint64(b[:]))
	seed := int64(-3405439173988651889)
	rand.Seed(seed)

	dupRate := 400
	if listSize < dupRate {
		dupRate = listSize / 10
	}
	if dupRate == 0 {
		dupRate = 2
	}

	bigList = make([]*Order, 0, listSize)
	for i := 0; i < listSize; i++ {
		order := newLimitOrder(false, uint64(rand.Int63n(90000000)), uint64(rand.Int63n(6))+1, order.StandingTiF, rand.Int63n(240)-120)
		order.Address = newFakeAddr()
		// duplicate some prices
		if (i+1)%(listSize/dupRate) == 0 {
			order.Rate = bigList[i/2].Rate
			order.Quantity = bigList[i/2].Quantity + 1
		}
		_ = order.ID()
		bigList = append(bigList, order)
	}
}

func TestLargeOrderMaxPriorityQueue(t *testing.T) {
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

	// Max oriented queue
	pq := NewMaxOrderPQ(uint32(len(bigList) * 3 / 2))
	for i, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %d: %v", i, o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	initLen := pq.Len()
	best := pq.ExtractBest()
	allOrders := make([]*Order, 0, initLen)
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
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
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

	initLen := pq.Len()
	best := pq.ExtractBest()
	allOrders := make([]*Order, 0, initLen)
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

func TestLargeOrderMaxPriorityQueue_Orders(t *testing.T) {
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

	// Max oriented queue (sell book)
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

	// Copy out all orders, sorted.
	ordersSorted := pq.Orders()

	// Ensure sorted in a different way.
	sorted := sort.SliceIsSorted(ordersSorted, func(i, j int) bool {
		if ordersSorted[i].Price() == ordersSorted[j].Price() {
			return ordersSorted[i].Time() < ordersSorted[j].Time()
		}
		return ordersSorted[i].Price() > ordersSorted[j].Price() // max pq
	})
	if !sorted {
		t.Errorf("Rates should have been sorted")
		// for _, op := range ordersSorted {
		// 	t.Log(op.Price(), op.Time())
		// }
	}

	ordersSorted2 := pq.OrdersN(pq.Count())
	if len(ordersSorted2) != len(ordersSorted) {
		t.Fatalf("Orders() and OrdersN(Count()) returned different slices.")
	}
	for i, o := range ordersSorted {
		if o.ID() != ordersSorted2[i].ID() {
			t.Errorf("Mismatched orders: %v != %v", o, ordersSorted2[i])
		}
	}

	// Copy out just the six best orders.
	sixOrders := pq.OrdersN(6)
	for i := range sixOrders {
		if sixOrders[i].UID() != ordersSorted[i].UID() {
			t.Errorf("Order %d incorrect. Got %s, expected %s",
				i, sixOrders[i].UID(), ordersSorted[i].UID())
		}
	}

	// Do it again to ensure the queue is not changed by copying.
	sixOrders2 := pq.OrdersN(6)
	for i := range sixOrders2 {
		if sixOrders[i].UID() != sixOrders2[i].UID() {
			t.Errorf("Order %d incorrect. Got %s, expected %s",
				i, sixOrders2[i].UID(), sixOrders[i].UID())
		}
	}

	pq.Reset(ordersSorted)
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != ordersSorted[0].Price() {
		t.Errorf("Heap Reset failed.")
	}
}

func TestLargeOrderMaxPriorityQueue_Realloc(t *testing.T) {
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

	// Max oriented queue (sell book)
	pq := NewMaxOrderPQ(uint32(len(bigList)))
	for _, o := range bigList {
		ok := pq.Insert(o)
		if !ok {
			t.Fatalf("Failed to insert order %v", o)
		}
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	newCap := pq.capacity * 3 / 2
	pq.Realloc(newCap)

	if pq.capacity != newCap {
		t.Errorf("Reallocated capacity incorrect. Expected %d, got %d",
			newCap, pq.capacity)
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	// Extract all orders, sorted.
	ordersSorted := pq.ExtractN(pq.Count())

	// Ensure sorted in a different way.
	sorted := sort.SliceIsSorted(ordersSorted, func(i, j int) bool {
		if ordersSorted[i].Price() == ordersSorted[j].Price() {
			return ordersSorted[i].Time() < ordersSorted[j].Time()
		}
		return ordersSorted[i].Price() > ordersSorted[j].Price() // max pq
	})
	if !sorted {
		t.Errorf("Rates should have been sorted")
		// for _, op := range ordersSorted {
		// 	t.Log(op.Price(), op.Time())
		// }
	}

	// Remake the queue with the extracted orders.
	pq.Reset(ordersSorted)
	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}
	if pq.PeekBest().Price() != ordersSorted[0].Price() {
		t.Errorf("Heap Reset failed.")
	}
}

func TestMinOrderPriorityQueue(t *testing.T) {
	startLogger()

	pq := NewMinOrderPQ(5)

	for _, o := range orders {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest()
	if best.UID() != orders[1].UID() {
		t.Errorf("Incorrect lowest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestMaxOrderPriorityQueue(t *testing.T) {
	startLogger()

	pq := NewMaxOrderPQ(5)

	for _, o := range orders {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest()
	if best.UID() != orders[3].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestMaxOrderPriorityQueue_TieRate(t *testing.T) {
	startLogger()

	pq := NewMaxOrderPQ(4)

	for _, o := range orders[:3] {
		ok := pq.Insert(o)
		if !ok {
			t.Errorf("Failed to insert order %v", o)
		}
	}

	best := pq.ExtractBest()
	if best.UID() != orders[2].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestMaxOrderPriorityQueue_TieRateAndTime(t *testing.T) {
	startLogger()

	pq := NewMaxOrderPQ(4)

	// 7f9200eedcf2fa868173cdfc2101ee4d71ec024c1c052589b3371442aaa26c2d
	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	// 2eb563f255b0a9484bbbee718b2cdce3a31bd5ea8649b579b3184a4bd60d1703 ** higher priority
	ok = pq.Insert(orders[4])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[4])
	}

	best := pq.ExtractBest()
	if best.UID() != orders[4].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestOrderPriorityQueueCapacity(t *testing.T) {
	startLogger()

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

	best := pq.PeekBest()
	if best.UID() != orders[0].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestOrderPriorityQueueNegative_Insert(t *testing.T) {
	startLogger()

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
	startLogger()

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
	if best.UID() != orders[1].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestResetHeap(t *testing.T) {
	startLogger()

	pq := NewMaxOrderPQ(2)

	orderEntries := []*orderEntry{
		{
			order:   orders[0],
			heapIdx: -1,
		},
		{
			order:   orders[1],
			heapIdx: -1,
		},
	}

	pq.resetHeap(orderEntries)

	best := pq.ExtractBest()
	if best.UID() != orders[0].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}

	best = pq.ExtractBest()
	if best.UID() != orders[1].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}

	best = pq.ExtractBest()
	if best != nil {
		t.Errorf("Order returned, but queue should be empty.")
	}
}

func TestOrderPriorityQueue_Remove(t *testing.T) {
	startLogger()

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
	remainingID := pq.PeekBest().ID()
	if remainingID != orders[0].ID() {
		t.Errorf("Remaining element expected %s, got %s", orders[0].ID(),
			remainingID)
	}
	pq.RemoveOrderID(remainingID)
	if pq.Len() != 0 {
		t.Errorf("Expected empty queue, got %d", pq.Len())
	}
}

func TestOrderPQMin_Worst(t *testing.T) {
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

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
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

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

	//t.Log(bigList[0].Price(), bigList[len(bigList)-1].Price(), pq.PeekBest().Price())

	// Worst for a min queue is highest rate.
	worst := pq.Worst()
	if worst.UID() != bigList[len(bigList)-1].UID() {
		t.Errorf("Incorrect worst order. Got %s, expected %s", worst.UID(), bigList[len(bigList)-1].UID())
	}
}

func TestOrderPQMax_leafNodes(t *testing.T) {
	startLogger()

	if testing.Short() {
		genBigList(10000)
	} else {
		genBigList(1000000)
	}

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
