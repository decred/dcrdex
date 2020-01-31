// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

// EpochQueue represents a client epoch queue.
type EpochQueue struct {
	orders             map[order.OrderID]*msgjson.EpochOrderNote
	ordersMtx          sync.Mutex
	seedIndex          []order.OrderID
	seedIndexMtx       sync.Mutex
	commitmentIndex    []order.OrderID
	commitmentIndexMtx sync.Mutex
}

// NewEpochQueue creates a client epoch queue.
func NewEpochQueue() *EpochQueue {
	return &EpochQueue{
		orders:          make(map[order.OrderID]*msgjson.EpochOrderNote),
		seedIndex:       make([]order.OrderID, 0),
		commitmentIndex: make([]order.OrderID, 0),
	}
}

// Reset clears the epoch queue. This should be called when a new epoch begins.
func (eq *EpochQueue) Reset() {
	eq.ordersMtx.Lock()
	eq.orders = make(map[order.OrderID]*msgjson.EpochOrderNote)
	eq.ordersMtx.Unlock()

	eq.seedIndexMtx.Lock()
	eq.seedIndex = make([]order.OrderID, 0)
	eq.seedIndexMtx.Unlock()

	eq.commitmentIndexMtx.Lock()
	eq.commitmentIndex = make([]order.OrderID, 0)
	eq.commitmentIndexMtx.Unlock()
}

// Queue appends the provided note to the epoch queue.
func (eq *EpochQueue) Queue(note *msgjson.EpochOrderNote) {
	var oid order.OrderID
	copy(oid[:], note.OrderID[:])

	eq.ordersMtx.Lock()
	defer eq.ordersMtx.Unlock()

	eq.orders[oid] = note

	// Update the seed sort order per order ids.
	eq.seedIndexMtx.Lock()
	si := sort.Search(len(eq.seedIndex), func(i int) bool {
		order := eq.orders[eq.seedIndex[i]]
		return bytes.Compare(order.OrderID[:], oid[:]) > 0
	})
	eq.seedIndex = append(eq.seedIndex, order.OrderID{})
	copy(eq.seedIndex[si+1:], eq.seedIndex[si:])
	eq.seedIndex[si] = oid
	eq.seedIndexMtx.Unlock()

	// Update the commitment sort order per order commitments.
	eq.commitmentIndexMtx.Lock()
	ci := sort.Search(len(eq.commitmentIndex), func(i int) bool {
		order := eq.orders[eq.commitmentIndex[i]]
		return bytes.Compare(order.Commitment[:], note.Commitment[:]) > 0
	})
	eq.commitmentIndex = append(eq.commitmentIndex, order.OrderID{})
	copy(eq.commitmentIndex[ci+1:], eq.commitmentIndex[ci:])
	eq.commitmentIndex[ci] = oid
	eq.commitmentIndexMtx.Unlock()
}

// Exists checks if the provided order id is in the queue.
func (eq *EpochQueue) Size() int {
	eq.ordersMtx.Lock()
	size := len(eq.orders)
	eq.ordersMtx.Unlock()
	return size
}

// Exists checks if the provided order id is in the queue.
func (eq *EpochQueue) Exists(oid order.OrderID) bool {
	eq.ordersMtx.Lock()
	_, ok := eq.orders[oid]
	eq.ordersMtx.Unlock()
	return ok
}

// GenerateMatchProof calculates the sorting seed used in order matching as well as the
// commitment checksum from the provided epoch queue preimages and misses.
func (eq *EpochQueue) GenerateMatchProof(preimages []order.Preimage, misses []string) (int64, msgjson.Bytes, error) {
	eq.ordersMtx.Lock()
	defer eq.ordersMtx.Unlock()

	// Remove all misses.
	for _, entry := range misses {
		b, err := hex.DecodeString(entry[:])
		if err != nil {
			return 0, nil, fmt.Errorf("decoding error: %v", err)
		}
		var oid order.OrderID
		copy(oid[:], b[:])

		delete(eq.orders, oid)

		// Remove the seed index associated with the miss, preserving
		// the sort order in the process.
		eq.seedIndexMtx.Lock()
		si := sort.Search(len(eq.seedIndex), func(i int) bool { return bytes.Compare(eq.seedIndex[i][:], oid[:]) >= 0 })
		if si == len(eq.seedIndex) {
			return 0, nil, fmt.Errorf("expected to find id %s in seed sort index", oid)
		}
		if si < len(eq.seedIndex)-1 {
			copy(eq.seedIndex[si:], eq.seedIndex[si+1:])
		}
		eq.seedIndex[len(eq.seedIndex)-1] = order.OrderID{}
		eq.seedIndex = eq.seedIndex[:len(eq.seedIndex)-1]
		eq.seedIndexMtx.Unlock()

		// Remove the commitment index associated with the miss, preserving
		// the sort order in the process.
		eq.commitmentIndexMtx.Lock()
		ci := sort.Search(len(eq.commitmentIndex), func(i int) bool { return bytes.Compare(eq.commitmentIndex[i][:], oid[:]) >= 0 })
		if ci == len(eq.commitmentIndex) {
			return 0, nil, fmt.Errorf("expected to find id %s in commitment sort index", oid)
		}
		if ci < len(eq.commitmentIndex)-1 {
			copy(eq.commitmentIndex[ci:], eq.commitmentIndex[ci+1:])
		}
		eq.commitmentIndex[len(eq.commitmentIndex)-1] = order.OrderID{}
		eq.commitmentIndex = eq.commitmentIndex[:len(eq.commitmentIndex)-1]
		eq.commitmentIndexMtx.Unlock()
	}

	// Map the preimages received with their associated epoch order ids.
	matches := make(map[order.OrderID]msgjson.Bytes)
	for i := 0; i < len(preimages); i++ {
		for oid, note := range eq.orders {
			commitment := blake256.Sum256(preimages[i][:])
			if bytes.Equal(note.Commitment, commitment[:]) {
				matches[oid] = preimages[i][:]
				break
			}
		}
	}

	// Ensure all remaining epoch orders matched to a preimage.
	if len(matches) != len(eq.orders) {
		return 0, nil, fmt.Errorf("expected all remaining epoch orders (%v) "+
			"matched to a preimage (%v)", len(matches), len(eq.orders))
	}

	// Concatenate all preimages per the seed sort index and generate the
	// seed.
	eq.seedIndexMtx.Lock()
	sbuff := make([]byte, 0, len(eq.seedIndex)*order.PreimageSize)
	for _, oid := range eq.seedIndex {
		pimg := matches[oid]
		sbuff = append(sbuff, pimg...)
	}
	eq.seedIndexMtx.Unlock()

	seed := int64(binary.LittleEndian.Uint64(sbuff[:64]))

	// Concatenate all order commitments per the commitment sort index and
	// generate the commitment checksum.
	eq.commitmentIndexMtx.Lock()
	cbuff := make([]byte, 0, len(eq.orders)*order.CommitmentSize)
	for _, oid := range eq.commitmentIndex {
		cbuff = append(cbuff, eq.orders[oid].Commitment...)
	}
	eq.commitmentIndexMtx.Unlock()

	csum := blake256.Sum256(cbuff)

	return seed, msgjson.Bytes(csum[:]), nil
}
