package auth

import (
	"bytes"
	"math/rand"
	"sort"
	"testing"

	"decred.org/dcrdex/dex/order"
)

func Test_latestMatchOutcomes(t *testing.T) {
	cap := int16(10)
	ordList := newLatestMatchOutcomes(cap)

	randTime := func() int64 {
		return 1600477631 + rand.Int63n(987654)
	}

	N := int(cap + 20)
	times := make([]int64, N)
	for i := 0; i < N; i++ {
		t := randTime()
		times[i] = t
		ordList.add(&matchOutcome{
			time: t,
			// zero OrderID
		})
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j] // ascending
	})

	if len(ordList.outcomes) != int(cap) {
		t.Fatalf("latest list is length %d, wanted %d", len(ordList.outcomes), cap)
	}

	// ensure the ordList contains the N most recent items, with the oldest at
	// the front of the list.
	wantStampedTimes := times[len(times)-int(cap):] // grab the last cap items in the sorted ground truth list
	for i, st := range ordList.outcomes {
		if st.time != wantStampedTimes[i] {
			t.Fatalf("Time #%d is %d, wanted %d", i, st.time, wantStampedTimes[i])
		}
	}

	// Add 3 orders with the same time, different order IDs.
	nextTime := 1 + wantStampedTimes[len(wantStampedTimes)-1] // new
	mids := []order.MatchID{randomMatchID(), randomMatchID(), randomMatchID()}
	for i := range mids {
		ordList.add(&matchOutcome{
			time: nextTime,
			mid:  mids[i],
		})
	}

	sort.Slice(mids, func(i, j int) bool {
		return bytes.Compare(mids[i][:], mids[j][:]) < 0
	})

	if len(ordList.outcomes) != int(cap) {
		t.Fatalf("latest list is length %d, wanted %d", len(ordList.outcomes), cap)
	}

	// Verify that the last three outcomes are the three we just added with the
	// newest time stamp, and that they are storted according to oid.
	for i, oc := range ordList.outcomes[cap-3 : cap] {
		if oc.mid != mids[i] {
			t.Errorf("Wrong mid #%d. got %v, want %v", i, oc.mid, mids[i])
		}
	}
}

func Test_latestPreimageOutcomes(t *testing.T) {
	cap := int16(10)
	ordList := newLatestPreimageOutcomes(cap)

	randTime := func() int64 {
		return 1600477631 + rand.Int63n(987654)
	}

	N := int(cap + 20)
	times := make([]int64, N)
	for i := 0; i < N; i++ {
		t := randTime()
		times[i] = t
		ordList.add(&preimageOutcome{
			time: t,
			// zero OrderID
		})
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j] // ascending
	})

	if len(ordList.outcomes) != int(cap) {
		t.Fatalf("latest list is length %d, wanted %d", len(ordList.outcomes), cap)
	}

	// ensure the ordList contains the N most recent items, with the oldest at
	// the front of the list.
	wantStampedTimes := times[len(times)-int(cap):] // grab the last cap items in the sorted ground truth list
	for i, st := range ordList.outcomes {
		if st.time != wantStampedTimes[i] {
			t.Fatalf("Time #%d is %d, wanted %d", i, st.time, wantStampedTimes[i])
		}
	}

	// Add 3 orders with the same time, different order IDs.
	nextTime := 1 + wantStampedTimes[len(wantStampedTimes)-1] // new
	oids := []order.OrderID{randomOrderID(), randomOrderID(), randomOrderID()}
	for i := range oids {
		ordList.add(&preimageOutcome{
			time: nextTime,
			oid:  oids[i],
		})
	}

	sort.Slice(oids, func(i, j int) bool {
		return bytes.Compare(oids[i][:], oids[j][:]) < 0
	})

	if len(ordList.outcomes) != int(cap) {
		t.Fatalf("latest list is length %d, wanted %d", len(ordList.outcomes), cap)
	}

	// Verify that the last three outcomes are the three we just added with the
	// newest time stamp, and that they are storted according to oid.
	for i, oc := range ordList.outcomes[cap-3 : cap] {
		if oc.oid != oids[i] {
			t.Errorf("Wrong oid #%d. got %v, want %v", i, oc.oid, oids[i])
		}
	}
}
