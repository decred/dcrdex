// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package orderbook

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
	mtx    sync.RWMutex
	orders map[order.OrderID]*epochOrder
}

// NewEpochQueue creates a client epoch queue.
func NewEpochQueue() *EpochQueue {
	return &EpochQueue{
		orders: make(map[order.OrderID]*epochOrder),
	}
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
func (eq *EpochQueue) GenerateMatchProof(preimages []order.Preimage, misses []order.OrderID) (msgjson.Bytes, msgjson.Bytes, error) {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()

	if len(eq.orders) == 0 {
		return nil, nil, fmt.Errorf("cannot generate match proof with an empty epoch queue")
	}

	// Get the commitments for all orders in the epoch queue.
	commits := make([]*order.Commitment, 0, len(eq.orders))
	for _, ord := range eq.orders {
		commits = append(commits, &ord.Commitment)
	}

	// Sort the commitments slice.
	sort.Slice(commits, func(i, j int) bool {
		return bytes.Compare(commits[i][:], commits[j][:]) < 0
	})

	// Generate the commitment checksum.
	comH := blake256.New()
	for _, commit := range commits {
		comH.Write(commit[:])
	}
	csum := comH.Sum(nil)

	// Remove all misses.
	for _, oid := range misses {
		delete(eq.orders, oid)
	}

	// Map the preimages received with their associated epoch order ids.
	matches := make([]*pimgMatch, 0, len(preimages))
outer:
	for _, pimg := range preimages {
		commitment := blake256.Sum256(pimg[:])
		for oid, ord := range eq.orders {
			if ord.Commitment == commitment {
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

	for oid := range eq.orders {
		fmt.Printf("WARNING! Order not accounted for in match proof: %v\n", oid)
		// csum will not match, so just note what was omitted for debugging.
	}

	// Compute the hash of the concatenated preimages, sorted by order ID.
	piH := blake256.New()
	for _, match := range matches {
		piH.Write(match.pimg[:])
	}
	seed := piH.Sum(nil)

	return seed, csum, nil
}

// Orders returns the epoch queue as a []*Order.
func (eq *EpochQueue) Orders() (orders []*Order) {
	eq.mtx.Lock()
	defer eq.mtx.Unlock()
	for oid, ord := range eq.orders {
		orders = append(orders, &Order{
			OrderID:  oid,
			Side:     ord.Side,
			Quantity: ord.Quantity,
			Rate:     ord.Rate,
			Epoch:    ord.epoch,
		})
	}
	return
}
