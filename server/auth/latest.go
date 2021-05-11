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

	// Do a dumb search for the match ID, without regard to time, so we can't
	// insert a match twice.
	for _, oc := range la.outcomes {
		if oc.mid == mo.mid {
			log.Warnf("(*latestMatchOutcomes).add: Rejecting duplicate match ID: %v", mo.mid)
			return
		}
	}

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
	la.mtx.Lock()
	defer la.mtx.Unlock()

	bins := make(map[Violation]int64)
	for _, mo := range la.outcomes {
		bins[mo.outcome]++
	}
	return bins
}

// SwapAmounts breaks down the quantities of completed swaps in four rough
// categories: successfully swapped (Swapped), failed with counterparty funds
// locked for the long/maker lock time (StuckLong), failed with counterparty
// funds locked for the short/taker lock time (StuckShort), and failed to
// initiate swap following match with no funds locked in contracts (Spoofed).
type SwapAmounts struct {
	Swapped    int64
	StuckLong  int64
	StuckShort int64
	Spoofed    int64
}

func (sa *SwapAmounts) addAmt(v Violation, value int64) {
	switch v {
	case ViolationSwapSuccess:
		sa.Swapped += value
	case ViolationNoSwapAsTaker:
		sa.StuckLong += value
	case ViolationNoRedeemAsMaker:
		sa.StuckShort += value
	case ViolationNoSwapAsMaker, ViolationPreimageMiss: // ! preimage misses are presently in preimageOutcome
		sa.Spoofed += value
	}
}

// func (la *latestMatchOutcomes) swapAmounts() *SwapAmounts {
// 	sa := new(SwapAmounts)
// 	for _, mo := range la.outcomes {
// 		sa.addAmt(mo.outcome, int64(mo.value)) // must be same units (e.g. lots)!
// 	}
// 	return sa
// }

func (la *latestMatchOutcomes) mktSwapAmounts(base, quote uint32) *SwapAmounts {
	sa := new(SwapAmounts)
	for _, mo := range la.outcomes {
		if mo.base == base && mo.quote == quote {
			sa.addAmt(mo.outcome, int64(mo.value))
		}
	}
	return sa
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

	// Do a dumb search for the order ID, without regard to time, so we can't
	// insert an order twice.
	for _, oc := range la.outcomes {
		if oc.oid == po.oid {
			log.Warnf("(*latestPreimageOutcomes).add: Rejecting duplicate order ID: %v", po.oid)
			return
		}
	}

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
	la.mtx.Lock()
	defer la.mtx.Unlock()

	for _, th := range la.outcomes {
		if th.miss {
			misses++
		}
	}
	return
}
