package auth

import (
	"math/rand"
	"sort"
	"testing"

	"decred.org/dcrdex/dex/order"
)

func randomOrderID() (oid order.OrderID) {
	rand.Read(oid[:])
	return
}

func Test_latestOrders(t *testing.T) {
	cap := int16(cancelThreshWindow)
	ordList := newLatestOrders(cap)

	maybeCancel := func() *order.OrderID {
		if rand.Intn(6) > 4 {
			oid := randomOrderID()
			return &oid
		}
		return nil
	}

	checkSort := func() {
		if !sort.IsSorted(ordsByTimeThenID(ordList.orders)) {
			t.Fatal("list wasn't sorted")
		}
		if len(ordList.orders) > int(ordList.cap) {
			t.Fatalf("list is above capacity somehow")
		}
	}

	// empty list
	total, cancels := ordList.counts()
	if total != 0 {
		t.Errorf("expected 0 orders, got %d", total)
	}
	if cancels != 0 {
		t.Errorf("expected 0 cancels, got %d", total)
	}

	// add one cancel
	ts := int64(1234)
	coid := randomOrderID()
	ordList.add(&oidStamped{order.OrderID{0x1}, ts, &coid})
	checkSort()
	total, cancels = ordList.counts()
	if total != 1 {
		t.Errorf("expected 1 orders, got %d", total)
	}
	if cancels != 1 {
		t.Errorf("expected 1 cancels, got %d", total)
	}

	// add one non-cancel
	ts++
	ordList.add(&oidStamped{order.OrderID{0x2}, ts, nil})
	checkSort()
	total, cancels = ordList.counts()
	if total != 2 {
		t.Errorf("expected 2 orders, got %d", total)
	}
	if cancels != 1 {
		t.Errorf("expected 1 cancels, got %d", total)
	}

	// add one that is the smallest
	ordList.add(&oidStamped{order.OrderID{0x3}, ts - 10, nil})
	checkSort()
	total, cancels = ordList.counts()
	if total != 3 {
		t.Errorf("expected 3 orders, got %d", total)
	}
	if cancels != 1 {
		t.Errorf("expected 1 cancels, got %d", total)
	}

	rand.Seed(1324)

	for i := total; i < int(cap); i++ {
		ts++
		ordList.add(&oidStamped{
			OrderID: randomOrderID(),
			time:    ts,
			target:  maybeCancel(),
		})
		checkSort()
	}

	total, _ = ordList.counts()
	if total != int(cap) {
		t.Errorf("expected %d orders, got %d", int(cap), total)
	}
	//t.Logf("got %d cancels", cancels)

	// Now that the list is at capacity, add another to test pop of the oldest order.
	expectedOldest := ordList.orders[1] // the second oldest order
	ts += 2                             // still in order, leave space for an out of order add
	ordList.add(&oidStamped{
		OrderID: order.OrderID{0x4},
		time:    ts,
		target:  maybeCancel(),
	})
	checkSort()

	// should still be at capacity
	total, _ = ordList.counts()
	if total != int(cap) {
		t.Errorf("expected %d orders, got %d", int(cap), total)
	}

	// verify the oldest order is the previously second oldest
	if expectedOldest != ordList.orders[0] {
		t.Errorf("expected oldest order to be %x, got %x", expectedOldest, ordList.orders[0])
	}

	// Now add one with the same time as the last, but larger order ID, thus not
	// requiring a swap.
	oid4 := order.OrderID{0x5}
	ordList.add(&oidStamped{
		OrderID: oid4,
		time:    ts,
		target:  maybeCancel(),
	})
	checkSort()

	// verify the latest order is the one just stored
	latestOid := ordList.orders[len(ordList.orders)-1].OrderID
	if oid4 != latestOid {
		t.Errorf("expected latest order ID to be %x, got %x", oid4, latestOid)
	}

	// Add another with the same time as last, but this time with a smaller ID,
	// thus requiring a swap.
	oid0 := order.OrderID{0x0}
	ordList.add(&oidStamped{
		OrderID: oid0,
		time:    ts,
		target:  maybeCancel(),
	})
	checkSort()

	// verify the latest order has not changed
	latestOid = ordList.orders[len(ordList.orders)-1].OrderID
	if oid4 != latestOid {
		t.Errorf("expected latest order ID to be %x, got %x", oid4, latestOid)
	}

	// Add one with an older time, thus necessitating a few swaps.
	ts--
	ordList.add(&oidStamped{
		OrderID: randomOrderID(),
		time:    ts,
		target:  maybeCancel(),
	})
	checkSort()

	// verify the latest order has not changed
	latestOid = ordList.orders[len(ordList.orders)-1].OrderID
	if oid4 != latestOid {
		t.Errorf("expected latest order ID to be %x, got %x", oid4, latestOid)
	}

	// Now exercise it and ensure it is always sorted.
	for i := 0; i < 100000; i++ {
		ordList.add(&oidStamped{
			OrderID: randomOrderID(),
			time:    rand.Int63n(44444),
			target:  maybeCancel(),
		})
		checkSort()
	}
}

func Test_ordsByTimeThenID_Sort(t *testing.T) {
	tests := []struct {
		name     string
		ords     []*oidStamped
		wantOrds []*oidStamped
	}{
		{
			name: "unique, no swap",
			ords: []*oidStamped{
				{order.OrderID{0x1}, 1234, nil},
				{order.OrderID{0x2}, 1235, nil},
			},
			wantOrds: []*oidStamped{
				{order.OrderID{0x1}, 1234, nil},
				{order.OrderID{0x2}, 1235, nil},
			},
		},
		{
			name: "unique, one swap",
			ords: []*oidStamped{
				{order.OrderID{0x2}, 1235, nil},
				{order.OrderID{0x1}, 1234, nil},
			},
			wantOrds: []*oidStamped{
				{order.OrderID{0x1}, 1234, nil},
				{order.OrderID{0x2}, 1235, nil},
			},
		},
		{
			name: "time tie, swap by order ID",
			ords: []*oidStamped{
				{order.OrderID{0x2}, 1234, nil},
				{order.OrderID{0x1}, 1234, nil},
			},
			wantOrds: []*oidStamped{
				{order.OrderID{0x1}, 1234, nil},
				{order.OrderID{0x2}, 1234, nil},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(ordsByTimeThenID(tt.ords))
			for i, o := range tt.ords {
				if o.OrderID != tt.wantOrds[i].OrderID {
					t.Errorf("element %d has order ID %x, wanted %x", i,
						o.OrderID, tt.wantOrds[i].OrderID)
				}
			}
		})
	}

	// Identical orders in the slice now just log and error, but the case can be
	// tested with the panic instead:
	//
	// t.Run("dup order should panic", func(t *testing.T) {
	//  defer func() {
	//      if recover() == nil {
	//          t.Error("sort should have paniced with identical orders.")
	//      }
	//  }()
	//  dups := []*oidStamped{
	//      {order.OrderID{0x1}, 1234, nil},
	//      {order.OrderID{0x1}, 1234, nil},
	//  }
	//  sort.Sort(ordsByTimeThenID(dups))
	// })
}
