// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

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
	epoch      uint64
}

// EpochQueue represents a client epoch queue.
type EpochQueue struct {
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
	eq.orders = make(map[order.OrderID]*epochOrder)
	eq.mtx.Unlock()
}

// Enqueue appends the provided order note to the epoch queue.
func (eq *EpochQueue) Enqueue(note *msgjson.EpochOrderNote) error {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()

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
		epoch:      note.Epoch,
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

// pimgMatch pairs matched preimage and epochOrder while generating the match
// proof.
type pimgMatch struct {
	id   order.OrderID
	ord  *epochOrder
	pimg order.Preimage
}

// GenerateMatchProof calculates the sorting seed used in order matching
// as well as the commitment checksum from the provided epoch queue
// preimages and misses.
//
// The epoch queue needs to be reset if there are preimage mismatches or
// non-existent orders for preimage errors.
func (eq *EpochQueue) GenerateMatchProof(epoch uint64, preimages []order.Preimage, misses []order.OrderID) (msgjson.Bytes, msgjson.Bytes, error) {
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

	// Map the preimages received with their associated epoch order ids.
	matches := make([]*pimgMatch, 0, len(preimages))
outer:
	for i := range preimages {
		pimg := preimages[i]
		commitment := blake256.Sum256(pimg[:])
		for oid, ord := range eq.orders {
			if bytes.Equal(ord.Commitment[:], commitment[:]) {
				matches = append(matches, &pimgMatch{
					id:   oid,
					ord:  ord,
					pimg: pimg,
				})
				delete(eq.orders, oid)
				continue outer
			}
		}
		return nil, nil, fmt.Errorf("no order match found for preimage %x", pimg)
	}

	sort.Slice(matches, func(i, j int) bool {
		return bytes.Compare(matches[i].id[:], matches[j].id[:]) < 0
	})

	// Concatenate the sorted preimages per and generate the
	// seed.
	sbuff := make([]byte, 0, len(eq.orders)*order.PreimageSize)
	for _, match := range matches {
		sbuff = append(sbuff, match.pimg[:]...)
	}
	seed := blake256.Sum256(sbuff)

	sort.Slice(matches, func(i, j int) bool {
		return bytes.Compare(matches[i].ord.Commitment[:], matches[j].ord.Commitment[:]) < 0
	})
	// Concatenate the sorted order commitments and generate the
	// commitment checksum.
	cbuff := make([]byte, 0, len(eq.orders)*order.CommitmentSize)
	for _, match := range matches {
		cbuff = append(cbuff, match.ord.Commitment[:]...)
	}
	csum := blake256.Sum256(cbuff)

	// Check for old orders and log any found.
	for oid, ord := range eq.orders {
		if ord.epoch <= epoch {
			log.Errorf("removing stale order from epoch queue %s", oid)
			delete(eq.orders, oid)
		}
	}

	return seed[:], msgjson.Bytes(csum[:]), nil
}

// Orders returns the eqoch queue as a []*Order.
func (eq *EpochQueue) Orders() (orders []*Order) {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()
	for oid, ord := range eq.orders {
		orders = append(orders, &Order{
			OrderID:  oid,
			Side:     ord.Side,
			Quantity: ord.Quantity,
			Rate:     ord.Rate,
		})
	}
	return
}
