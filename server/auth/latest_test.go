package auth

import (
	"math/rand"
	"sort"
	"testing"
)

func Test_latest(t *testing.T) {
	cap := int16(10)
	ordList := newLatest(cap)

	randTime := func() int64 {
		return 1600477631 + rand.Int63n(987654)
	}

	N := int(cap + 20)
	times := make([]int64, N)
	for i := 0; i < N; i++ {
		t := randTime()
		times[i] = t
		ordList.add(&stampedFlag{
			time: t,
		})
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j] // ascending
	})

	if len(ordList.stampeds) != int(cap) {
		t.Fatalf("latest list is length %d, wanted %d", len(ordList.stampeds), cap)
	}

	// ensure the ordList contains the N most recent items, with the oldest at
	// the front of the list.
	wantStampedTimes := times[len(times)-int(cap):] // grab the last cap items in the sorted ground truth list
	for i, st := range ordList.stampeds {
		if st.time != wantStampedTimes[i] {
			t.Fatalf("Time #%d is %d, wanted %d", i, st.time, wantStampedTimes[i])
		}
	}
}
