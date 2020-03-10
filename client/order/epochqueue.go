// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

// epochOrder represents a compact EpochOrderNote.
type epochOrder struct {
	Side       uint8
	Quantity   uint64
	Rate       uint64
	Commitment order.Commitment
}

// EpochQueue represents a client epoch queue.
type EpochQueue struct {
	epoch  uint64
	orders map[order.OrderID]*epochOrder
	mtx    sync.RWMutex
}

// NewEpochQueue creates a client epoch queue.
func NewEpochQueue() *EpochQueue {
	return &EpochQueue{
		orders: make(map[order.OrderID]*epochOrder),
	}
}

// Reset clears the epoch queue. This should be called when a new epoch begins.
func (eq *EpochQueue) Reset() {
	eq.mtx.Lock()
	eq.epoch = 0
	eq.orders = make(map[order.OrderID]*epochOrder)
	eq.mtx.Unlock()
}

// Enqueue appends the provided order note to the epoch queue.
func (eq *EpochQueue) Enqueue(note *msgjson.EpochOrderNote) error {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()

	// Ensure the order received is of the current epoch.
	if eq.epoch != 0 && note.Epoch != eq.epoch {
		return fmt.Errorf("epoch mismatch: expected %d, got %d",
			eq.epoch, note.Epoch)
	}

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
	copy(commitment[:], note.Commit)

	order := &epochOrder{
		Commitment: commitment,
		Quantity:   note.Quantity,
		Rate:       note.Rate,
		Side:       note.Side,
	}

	// Set the epoch if the order note is the first to be queued.
	if eq.epoch == 0 {
		atomic.StoreUint64(&eq.epoch, note.Epoch)
	}

	eq.orders[oid] = order

	return nil
}

// Size returns the number of entries in the epoch queue.
func (eq *EpochQueue) Size() int {
	eq.mtx.RLock()
	size := len(eq.orders)
	eq.mtx.RUnlock()
	return size
}

// Exists checks if the provided order id is in the queue.
func (eq *EpochQueue) Exists(oid order.OrderID) bool {
	eq.mtx.RLock()
	_, ok := eq.orders[oid]
	eq.mtx.RUnlock()
	return ok
}

// Epoch returns the current epoch being tracked by the queue.
func (eq *EpochQueue) Epoch() uint64 {
	eq.mtx.RLock()
	epoch := eq.epoch
	eq.mtx.RUnlock()
	return epoch
}

// GenerateMatchProof calculates the sorting seed used in order matching
// as well as the commitment checksum from the provided epoch queue
// preimages and misses.
//
// The epoch queue needs to be reset if there are preimage mismatches or
// non-existent orders for preimage errors.
func (eq *EpochQueue) GenerateMatchProof(preimages []order.Preimage, misses []order.OrderID) (msgjson.Bytes, msgjson.Bytes, error) {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()

	if len(eq.orders) == 0 {
		return nil, nil,
			fmt.Errorf("cannot generate match proof with an empty epoch queue")
	}

	// Remove all misses.
	for _, oid := range misses {
		delete(eq.orders, oid)
	}

	if len(eq.orders) != len(preimages) {
		return nil, nil, fmt.Errorf("expected same length for epoch queue (%v) "+
			"preimages (%v)", len(eq.orders), len(preimages))
	}

	// Map the preimages received with their associated epoch order ids.
	matches := make(map[order.OrderID]order.Preimage, len(eq.orders))
	for i := range preimages {
		var match bool
		for oid, order := range eq.orders {
			commitment := blake256.Sum256(preimages[i][:])
			if bytes.Equal(order.Commitment[:], commitment[:]) {
				matches[oid] = preimages[i]
				match = true
				break
			}
		}

		if !match {
			return nil, nil,
				fmt.Errorf("no order match found for preimage %x", preimages[i])
		}
	}

	// Ensure all remaining epoch orders matched to a preimage.
	if len(matches) != len(eq.orders) {
		return nil, nil, fmt.Errorf("expected all remaining epoch orders (%v) "+
			"matched to a preimage (%v)", len(matches), len(eq.orders))
	}

	oids := make([]order.OrderID, 0, len(eq.orders))
	commitments := make([]order.Commitment, 0, len(eq.orders))
	for oid, order := range eq.orders {
		oids = append(oids, oid)
		commitments = append(commitments, order.Commitment)
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
	seed := blake256.Sum256(sbuff)

	// Concatenate the sorted order commitments and generate the
	// commitment checksum.
	cbuff := make([]byte, 0, len(eq.orders)*order.CommitmentSize)
	for _, commit := range commitments {
		cbuff = append(cbuff, commit[:]...)
	}
	csum := blake256.Sum256(cbuff)

	return seed[:], msgjson.Bytes(csum[:]), nil
}
