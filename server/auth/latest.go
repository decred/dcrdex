// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/order"
)

type matchOutcome struct {
	// sorting is done by time and match ID
	time int64
	mid  order.MatchID

	// match outcome and value
	outcome     Violation
	base, quote uint32 // market
	value       uint64
}

func lessByTimeThenMID(ti, tj *matchOutcome) bool {
	if ti.time == tj.time {
		return bytes.Compare(ti.mid[:], tj.mid[:]) < 0 // ascending (smaller ID first)
	}
	return ti.time < tj.time // ascending (newest last in slice)
}

type latestMatchOutcomes struct {
	mtx      sync.Mutex
	cap      int16
	outcomes []*matchOutcome
}

func newLatestMatchOutcomes(cap int16) *latestMatchOutcomes {
	return &latestMatchOutcomes{
		cap:      cap,
		outcomes: make([]*matchOutcome, 0, cap+1), // cap+1 since an old item is popped *after* a new one is pushed
	}
}

func (la *latestMatchOutcomes) add(mo *matchOutcome) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	// Use sort.Search and insert it at the right spot.
	n := len(la.outcomes)
	i := sort.Search(n, func(i int) bool {
		return lessByTimeThenMID(la.outcomes[n-1-i], mo)
	})
	if i == int(la.cap) /* i == n && n == int(la.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	}
	// Insert at proper location.
	i = n - i // i-1 is first location that stays
	la.outcomes = append(la.outcomes[:i], append([]*matchOutcome{mo}, la.outcomes[i:]...)...)

	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.outcomes) > int(la.cap) {
		// pop front, the oldest stamped
		la.outcomes[0] = nil // avoid memory leak
		la.outcomes = la.outcomes[1:]
	}
}

func (la *latestMatchOutcomes) binViolations() map[Violation]int64 {
	bins := make(map[Violation]int64)
	for _, mo := range la.outcomes {
		bins[mo.outcome]++
	}
	return bins
}

type preimageOutcome struct {
	time int64
	oid  order.OrderID
	miss bool
}

func lessByTimeThenOID(ti, tj *preimageOutcome) bool {
	if ti.time == tj.time {
		return bytes.Compare(ti.oid[:], tj.oid[:]) < 0 // ascending (smaller ID first)
	}
	return ti.time < tj.time // ascending (newest last in slice)
}

type latestPreimageOutcomes struct {
	mtx      sync.Mutex
	cap      int16
	outcomes []*preimageOutcome
}

func newLatestPreimageOutcomes(cap int16) *latestPreimageOutcomes {
	return &latestPreimageOutcomes{
		cap:      cap,
		outcomes: make([]*preimageOutcome, 0, cap+1), // cap+1 since an old item is popped *after* a new one is pushed
	}
}

func (la *latestPreimageOutcomes) add(po *preimageOutcome) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	// Use sort.Search and insert it at the right spot.
	n := len(la.outcomes)
	i := sort.Search(n, func(i int) bool {
		return lessByTimeThenOID(la.outcomes[n-1-i], po)
	})
	if i == int(la.cap) /* i == n && n == int(la.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	}
	// Insert at proper location.
	i = n - i // i-1 is first location that stays
	la.outcomes = append(la.outcomes[:i], append([]*preimageOutcome{po}, la.outcomes[i:]...)...)

	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.outcomes) > int(la.cap) {
		// pop front, the oldest stamped
		la.outcomes[0] = nil // avoid memory leak
		la.outcomes = la.outcomes[1:]
	}
}

func (la *latestPreimageOutcomes) misses() (misses int32) {
	for _, th := range la.outcomes {
		if th.miss {
			misses++
		}
	}
	return
}
