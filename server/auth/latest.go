// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"sort"
	"sync"
)

type stampedFlag struct {
	time int64
	flag int64
}

func lessByTime(ti, tj *stampedFlag) bool {
	return ti.time < tj.time // ascending (newest last in slice)
}

type latest struct {
	mtx      sync.Mutex
	cap      int16
	stampeds []*stampedFlag
}

func newLatest(cap int16) *latest {
	return &latest{
		cap:      cap,
		stampeds: make([]*stampedFlag, 0, cap+1), // cap+1 since an old item is popped *after* a new one is pushed
	}
}

func (la *latest) add(t *stampedFlag) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	// Use sort.Search and insert it at the right spot.
	n := len(la.stampeds)
	i := sort.Search(n, func(i int) bool {
		return lessByTime(la.stampeds[n-1-i], t)
	})
	if i == int(la.cap) /* i == n && n == int(la.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	}
	// Insert at proper location.
	i = n - i // i-1 is first location that stays
	la.stampeds = append(la.stampeds[:i], append([]*stampedFlag{t}, la.stampeds[i:]...)...)

	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.stampeds) > int(la.cap) {
		// pop front, the oldest stamped
		la.stampeds[0] = nil // avoid memory leak
		la.stampeds = la.stampeds[1:]
	}
}

func (la *latest) bin() map[int64]int64 {
	bins := make(map[int64]int64)
	for _, th := range la.stampeds {
		bins[th.flag]++
	}
	return bins
}
