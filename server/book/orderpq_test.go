package book

import (
	"flag"
	"math/rand"
	"os"
	"sort"
	"testing"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
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

func randomAccount() (user account.AccountID) {
	rand.Read(user[:])
	return
}

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
	seed := int64(-3405439173988651889)
	rand.Seed(seed)

	dupRate := 800
	if listSize < dupRate {
		dupRate = listSize / 10
	}
	if dupRate == 0 {
		dupRate = 2
	}

	bigList = make([]*Order, 0, listSize)
	for i := 0; i < listSize; i++ {
		lo := newLimitOrder(false, uint64(rand.Int63n(90000000)), uint64(rand.Int63n(6))+1, order.StandingTiF, rand.Int63n(240)-120)
		lo.Address = newFakeAddr()
		// duplicate some prices
		if (i+1)%(listSize/dupRate) == 0 {
			lo.Rate = bigList[i/2].Rate
			lo.Quantity = bigList[i/2].Quantity + 1
		}
		_ = lo.ID() // compute and cache the OrderID
		bigList = append(bigList, lo)
	}
}

const (
	shortListLen = 12_000
	longListLen  = 400_000
)

func TestMain(m *testing.M) {
	flag.Parse() // for -short
	if testing.Short() {
		genBigList(shortListLen)
	} else {
		genBigList(longListLen)
	}
	// The first and last of the sells are the same user.
	firstSell, lastSell := bookSellOrders[0], bookSellOrders[len(bookSellOrders)-1]
	firstSell.Coins[0] = lastSell.Coins[0]
	firstSell.Address = lastSell.Address
	os.Exit(m.Run())
}

func TestLargeOrderMaxPriorityQueue(t *testing.T) {
	// Max oriented queue
	pq := NewMaxOrderPQ(uint32(len(bigList) - len(bigList)/16)) // a little smaller to force a realloc
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
	// Min oriented queue
	pq := NewMinOrderPQ(uint32(len(bigList) - len(bigList)/16)) // a little smaller to force a realloc
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

func TestLargeOrderMaxPriorityQueue_realloc(t *testing.T) {
	// Max oriented queue (sell book)
	pq := NewMaxOrderPQ(uint32(len(bigList))) // no realloc for initial inserts
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
	pq.realloc(newCap)

	if pq.capacity != newCap {
		t.Errorf("Reallocated capacity incorrect. Expected %d, got %d",
			newCap, pq.capacity)
	}

	if pq.Len() != len(bigList) {
		t.Errorf("pq length incorrect. expected %d, got %d", len(bigList), pq.Len())
	}

	// Extract all orders, sorted. This should realloc and decrease cap on the way.
	ordersSorted := pq.ExtractN(pq.Count())

	if !testing.Short() && pq.Cap() >= newCap { // only long list is big enough to trigger dealloc
		t.Errorf("expected a capacity decrease")
	}
	t.Logf("OrderPQ capacity decreased from %d to %d after extracting all entries", newCap, pq.Cap())

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

func TestMinOrderPQ(t *testing.T) {
	pq := NewMinOrderPQ(0) // zero cap to force a realloc right away

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

func TestMaxOrderPQ(t *testing.T) {
	pq := NewMaxOrderPQ(0)

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

func TestMaxOrderPQ_TieRate(t *testing.T) {
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

func TestMaxOrderPQ_TieRateAndTime(t *testing.T) {
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

func TestOrderPQCapacity(t *testing.T) {
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
	if !ok {
		t.Errorf("Failed to insert order %v", orders[2])
	}

	if pq.Cap() <= 2 {
		t.Errorf("Failed to grow capacity")
	}

	if pq.Len() != 3 {
		t.Errorf("Failed to insert order with capacity growth")
	}

	best := pq.PeekBest()
	if best.UID() != orders[2].UID() {
		t.Errorf("Incorrect highest rate order returned: rate = %d, UID = %s",
			best.Price(), best.UID())
	}
}

func TestOrderPQ_Insert_negative(t *testing.T) {
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

func TestOrderPQ_Remove(t *testing.T) {
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

func TestOrderPQ_RemoveUserOrders(t *testing.T) {
	pq := NewMaxOrderPQ(6)

	ok := pq.Insert(orders[0])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[0])
	}

	ok = pq.Insert(orders[1])
	if !ok {
		t.Errorf("Failed to insert order %v", orders[1])
	}

	user0 := orders[0].AccountID

	user1 := randomAccount()
	other := newLimitOrder(false, 42000000, 2, order.StandingTiF, 0)
	other.AccountID = user1
	ok = pq.Insert(other)
	if !ok {
		t.Errorf("Failed to insert order %v", other)
	}

	amt, count := pq.UserOrderTotals(user0)
	wantAmt := orders[0].Remaining() + orders[1].Remaining()
	if amt != wantAmt {
		t.Errorf("wanted %d remaining, got %d", wantAmt, amt)
	}
	if count != 2 {
		t.Errorf("wanted %d orders, got %d", 2, count)
	}

	amt, count = pq.UserOrderTotals(user1)
	wantAmt = other.Remaining()
	if amt != wantAmt {
		t.Errorf("wanted %d remaining, got %d", wantAmt, amt)
	}
	if count != 1 {
		t.Errorf("wanted %d orders, got %d", 1, count)
	}

	removed := pq.RemoveUserOrders(user0)
	if pq.Len() != 1 {
		t.Errorf("Queue length expected %d, got %d", 1, pq.Len())
	}
	if len(removed) != 2 {
		t.Fatalf("removed %d orders, expected %d", len(removed), 2)
	}
	for _, oid := range []order.OrderID{orders[0].ID(), orders[1].ID()} {
		var found bool
		for i := range removed {
			if oid == removed[i].ID() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("didn't remove order %v", oid)
		}
	}

	remainingID := pq.PeekBest().ID()
	if remainingID != other.ID() {
		t.Errorf("Remaining element expected %s, got %s", other.ID(),
			remainingID)
	}
	removed = pq.RemoveUserOrders(user1)
	if remain := pq.Len(); remain != 0 {
		t.Errorf("didn't remove all orders, still have %d", remain)
	}
	if len(removed) != 1 {
		t.Fatalf("removed %d orders, expected %d", len(removed), 1)
	}
	if removed[0].ID() != other.ID() {
		t.Errorf("removed order %v, expected %v", removed[0], other.ID())
	}
}

func TestOrderPQMin_Worst(t *testing.T) {
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
	pq := NewMinOrderPQ(uint32(len(bigList) - len(bigList)/16))
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
	// Max oriented queue
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

	sort.Slice(bigList, func(j, i int) bool {
		if bigList[i].Price() == bigList[j].Price() {
			return bigList[i].Time() < bigList[j].Time()
		}
		return bigList[i].Price() < bigList[j].Price()
	})

	// Worst for a min queue is highest rate.
	worst := pq.Worst()
	if worst.UID() != bigList[len(bigList)-1].UID() {
		t.Errorf("Incorrect worst order. Got %s, expected %s", worst.UID(), bigList[len(bigList)-1].UID())
	}
}

func TestOrderPQMax_leafNodes(t *testing.T) {
	// Max oriented queue
	newQ := func(list []*Order) *OrderPQ {
		pq := NewMaxOrderPQ(uint32(len(bigList)))
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
