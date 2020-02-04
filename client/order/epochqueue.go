// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

// EpochQueue represents a client epoch queue.
type EpochQueue struct {
	orders map[order.OrderID]order.Commitment
	mtx    sync.Mutex
}

// NewEpochQueue creates a client epoch queue.
func NewEpochQueue() *EpochQueue {
	return &EpochQueue{
		orders: make(map[order.OrderID]order.Commitment),
	}
}

// Reset clears the epoch queue. This should be called when a new epoch begins.
func (eq *EpochQueue) Reset() {
	eq.mtx.Lock()
	eq.orders = make(map[order.OrderID]order.Commitment)
	eq.mtx.Unlock()
}

// Enqueue appends the provided order note to the epoch queue.
func (eq *EpochQueue) Enqueue(note *msgjson.EpochOrderNote) error {
	if len(note.OrderID) != order.OrderIDSize {
		return fmt.Errorf("expected order id length of %d, got %d",
			order.OrderIDSize, len(note.OrderID))
	}

	var oid order.OrderID
	copy(oid[:], note.OrderID)

	// Ensure the provided epoch order is not already queued.
	_, ok := eq.orders[oid]
	if ok {
		return fmt.Errorf("%s is already queued", oid)
	}

	var commitment order.Commitment
	copy(commitment[:], note.Commitment)

	eq.mtx.Lock()
	eq.orders[oid] = commitment
	eq.mtx.Unlock()

	return nil
}

// Size returns the number of entries in the epoch queue.
func (eq *EpochQueue) Size() int {
	eq.mtx.Lock()
	size := len(eq.orders)
	eq.mtx.Unlock()
	return size
}

// Exists checks if the provided order id is in the queue.
func (eq *EpochQueue) Exists(oid order.OrderID) bool {
	eq.mtx.Lock()
	_, ok := eq.orders[oid]
	eq.mtx.Unlock()
	return ok
}

// GenerateMatchProof calculates the sorting seed used in order matching
// as well as the commitment checksum from the provided epoch queue
// preimages and misses.
//
// The epoch queue needs to be reset if there are preimage mismatches or
// non-existent orders for preimage errors.
func (eq *EpochQueue) GenerateMatchProof(preimages []order.Preimage, misses []order.OrderID) (int64, msgjson.Bytes, error) {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()

	if len(eq.orders) == 0 {
		return 0, nil,
			fmt.Errorf("cannot generate match proof with an empty epoch queue")
	}

	// Remove all misses.
	for _, oid := range misses {
		delete(eq.orders, oid)
	}

	if len(eq.orders) != len(preimages) {
		return 0, nil, fmt.Errorf("expected same length for epoch queue (%v) "+
			"preimages (%v)", len(eq.orders), len(preimages))
	}

	// Map the preimages received with their associated epoch order ids.
	matches := make(map[order.OrderID]order.Preimage, len(eq.orders))
	for i := range preimages {
		var match bool
		for oid, commit := range eq.orders {
			commitment := blake256.Sum256(preimages[i][:])
			if bytes.Equal(commit[:], commitment[:]) {
				matches[oid] = preimages[i]
				match = true
				break
			}
		}

		if !match {
			return 0, nil,
				fmt.Errorf("no order match found for preimage %x", preimages[i])
		}
	}

	// Ensure all remaining epoch orders matched to a preimage.
	if len(matches) != len(eq.orders) {
		return 0, nil, fmt.Errorf("expected all remaining epoch orders (%v) "+
			"matched to a preimage (%v)", len(matches), len(eq.orders))
	}

	oids := make([]order.OrderID, 0, len(eq.orders))
	commitments := make([]order.Commitment, 0, len(eq.orders))
	for oid, commit := range eq.orders {
		oids = append(oids, oid)
		commitments = append(commitments, commit)
	}

	sort.Slice(oids, func(i, j int) bool {
		return bytes.Compare(oids[i][:], oids[j][:]) < 0
	})

	sort.Slice(commitments, func(i, j int) bool {
		return bytes.Compare(commitments[i][:], commitments[j][:]) < 0
	})

	// Concatenate the sorted preimages per and generate the
	// seed.
	sbuff := make([]byte, 0, len(eq.orders)*order.PreimageSize)
	for _, oid := range oids {
		pimg := matches[oid]
		sbuff = append(sbuff, pimg[:]...)
	}

	sum := blake256.Sum256(sbuff)
	seed := int64(binary.LittleEndian.Uint64(sum[:8]))

	// Concatenate the sorted order commitments and generate the
	// commitment checksum.
	cbuff := make([]byte, 0, len(eq.orders)*order.CommitmentSize)
	for _, commit := range commitments {
		cbuff = append(cbuff, commit[:]...)
	}
	csum := blake256.Sum256(cbuff)

	return seed, msgjson.Bytes(csum[:]), nil
}
