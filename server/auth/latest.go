// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"sync"

	"decred.org/dcrdex/server/db"
)

type latestOutcomes[T db.Outcomer] struct {
	mtx      sync.Mutex
	cap      int16
	outcomes []T
}

func newLatestOutcomes[T db.Outcomer](os []T, cap int16) *latestOutcomes[T] {
	return &latestOutcomes[T]{
		cap:      cap,
		outcomes: os,
	}
}

func (la *latestOutcomes[T]) add(o T) (popped int64) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	dbID := o.ID()
	for _, oo := range la.outcomes {
		if dbID == oo.ID() {
			log.Warnf("Attempted to add a latest outcome with a duplicate DB ID")
			return
		}
	}

	la.outcomes = append(la.outcomes, o)
	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.outcomes) > int(la.cap) {
		// pop front, the oldest stamped
		popped = la.outcomes[0].ID()
		// la.outcomes[0] = nil // avoid memory leak
		la.outcomes = la.outcomes[1:]
	}
	return
}

func (la *latestOutcomes[T]) binViolations() map[Outcome]int64 {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	bins := make(map[Outcome]int64)
	for _, o := range la.outcomes {
		bins[o.Outcome()]++
	}
	return bins
}
