// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

import (
	"fmt"
	"sort"
)

var (
	// defaultCapacity represents the default rate index capacity.
	defaultCapacity = 10
)

// rateIndex represents a sorted group of rate.
type rateIndex struct {
	Rates []uint64
}

// newRateIndex creates a new rate index.
func newRateIndex() *rateIndex {
	return &rateIndex{
		Rates: make([]uint64, 0, defaultCapacity),
	}
}

// Add adds a new entry to the rate index with the order preserved.
func (ri *rateIndex) Add(entry uint64) {
	i := sort.Search(len(ri.Rates),
		func(i int) bool { return ri.Rates[i] >= entry })

	// Only add a new entry to the rate index if it does not
	// already exist.
	if i < len(ri.Rates) {
		if ri.Rates[i] == entry {
			return
		}
	}

	ri.Rates = append(ri.Rates, 0)
	copy(ri.Rates[i+1:], ri.Rates[i:])
	ri.Rates[i] = entry
}

// Remove removes an entry from the rate index with the order preserved.
func (ri *rateIndex) Remove(entry uint64) error {
	i := sort.Search(len(ri.Rates),
		func(i int) bool { return ri.Rates[i] >= entry })

	if i == len(ri.Rates) {
		return fmt.Errorf("no entry found for value %d", entry)
	}

	if i < len(ri.Rates)-1 {
		copy(ri.Rates[i:], ri.Rates[i+1:])
	}
	ri.Rates[len(ri.Rates)-1] = uint64(0)
	ri.Rates = ri.Rates[:len(ri.Rates)-1]

	return nil
}
