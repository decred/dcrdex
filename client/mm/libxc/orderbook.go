// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.
package libxc

import (
	"fmt"
	"math"
	"sync"

	"github.com/huandu/skiplist"
)

type obEntry struct {
	qty  uint64
	rate uint64
}

// obEntryComparable is a skiplist.Comparable implementation for
// obEntry. It is used to sort the orderbook entries by rate.
type obEntryComparable int

const bidsComparable = obEntryComparable(0)
const asksComparable = obEntryComparable(1)

var _ skiplist.Comparable = (*obEntryComparable)(nil)

func (o obEntryComparable) Compare(lhs, rhs interface{}) int {
	lhsEntry := lhs.(*obEntry)
	rhsEntry := rhs.(*obEntry)

	var toReturn int
	if lhsEntry.rate < rhsEntry.rate {
		toReturn = -1
	}
	if lhsEntry.rate > rhsEntry.rate {
		toReturn = 1
	}

	if o == bidsComparable {
		toReturn = -toReturn
	}

	return toReturn
}

func (o obEntryComparable) CalcScore(key interface{}) float64 {
	if o == bidsComparable {
		return math.MaxFloat64 - float64(key.(*obEntry).rate)
	} else {
		return float64(key.(*obEntry).rate)
	}
}

// orderbook is an implementation of a limit order book that allows for quick
// updates and calculation of the volume weighted average price (VWAP).
type orderbook struct {
	mtx  sync.RWMutex
	bids skiplist.SkipList
	asks skiplist.SkipList
}

func newOrderBook() *orderbook {
	return &orderbook{
		bids: *skiplist.New(bidsComparable),
		asks: *skiplist.New(asksComparable),
	}
}

func (ob *orderbook) String() string {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bids := make([]obEntry, 0, ob.bids.Len())
	for curr := ob.bids.Front(); curr != nil; curr = curr.Next() {
		bids = append(bids, *curr.Value.(*obEntry))
	}

	asks := make([]obEntry, 0, ob.asks.Len())
	for curr := ob.asks.Front(); curr != nil; curr = curr.Next() {
		asks = append(asks, *curr.Value.(*obEntry))
	}

	return fmt.Sprintf("bids: %v, asks: %v", bids, asks)
}

// update updates the orderbook new quantities at the given rates.
// If the quantity is 0, the entry is removed from the orderbook.
func (ob *orderbook) update(bids []*obEntry, asks []*obEntry) {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()

	var bidsRemoved, bidsAdded, asksRemoved, asksAdded int

	for _, entry := range bids {
		if entry.qty == 0 {
			ob.bids.Remove(entry)
			bidsRemoved++
			continue
		}
		ob.bids.Set(entry, entry)
		bidsAdded++
	}

	for _, entry := range asks {
		if entry.qty == 0 {
			ob.asks.Remove(entry)
			asksRemoved++
			continue
		}
		ob.asks.Set(entry, entry)
		asksAdded++
	}

	// Log summary of changes made to orderbook
	if bidsRemoved > 0 || bidsAdded > 0 || asksRemoved > 0 || asksAdded > 0 {
		fmt.Printf("[DEBUG-ORDERBOOK-UPDATE] Updated orderbook: bids +%d/-%d, asks +%d/-%d, new sizes: bids=%d, asks=%d\n",
			bidsAdded, bidsRemoved, asksAdded, asksRemoved, ob.bids.Len(), ob.asks.Len())
	}
}

func (ob *orderbook) clear() {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()

	fmt.Printf("[DEBUG-ORDERBOOK-UPDATE] Clearing orderbook (previous sizes: bids=%d, asks=%d)\n",
		ob.bids.Len(), ob.asks.Len())

	ob.bids = *skiplist.New(bidsComparable)
	ob.asks = *skiplist.New(asksComparable)
}

func (ob *orderbook) vwap(bids bool, qty uint64) (vwap, extrema uint64, filled bool) {
	if qty == 0 { // avoid division by zero
		return 0, 0, false
	}

	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	var list *skiplist.SkipList
	if bids {
		list = &ob.bids
	} else {
		list = &ob.asks
	}

	// Log the VWAP request
	side := "asks"
	if bids {
		side = "bids"
	}
	fmt.Printf("[DEBUG-VWAP] Calculating VWAP for %s, qty=%d, book size=%d\n",
		side, qty, list.Len())

	if list.Len() == 0 {
		fmt.Printf("[DEBUG-VWAP] Empty orderbook for %s, cannot calculate VWAP\n", side)
		return 0, 0, false
	}

	remaining := qty
	var weightedSum uint64
	for curr := list.Front(); curr != nil; curr = curr.Next() {
		if curr == nil {
			break
		}
		entry := curr.Value.(*obEntry)
		extrema = entry.rate

		// Log the entry being processed
		fmt.Printf("[DEBUG-VWAP] Processing %s entry: rate=%d, qty=%d, remaining=%d\n",
			side, entry.rate, entry.qty, remaining)

		if entry.qty >= remaining {
			filled = true
			weightedSum += remaining * extrema

			// Log that we've filled the order
			fmt.Printf("[DEBUG-VWAP] Order filled with this entry, final extrema=%d\n", extrema)
			break
		}
		remaining -= entry.qty
		weightedSum += entry.qty * extrema
	}
	if !filled {
		fmt.Printf("[DEBUG-VWAP] Could not fill order completely, remaining=%d\n", remaining)
		return 0, 0, false
	}

	vwap = weightedSum / qty
	fmt.Printf("[DEBUG-VWAP] VWAP calculation complete: vwap=%d, extrema=%d\n", vwap, extrema)
	return weightedSum / qty, extrema, filled
}

func (ob *orderbook) midGap() uint64 {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bestBuyI := ob.bids.Front()
	if bestBuyI == nil {
		fmt.Printf("[DEBUG-MIDGAP] No bids in orderbook\n")
		return 0
	}
	bestSellI := ob.asks.Front()
	if bestSellI == nil {
		fmt.Printf("[DEBUG-MIDGAP] No asks in orderbook\n")
		return 0
	}
	bestBuy, bestSell := bestBuyI.Value.(*obEntry), bestSellI.Value.(*obEntry)
	midGap := (bestBuy.rate + bestSell.rate) / 2
	fmt.Printf("[DEBUG-MIDGAP] Calculated midgap: bestBuy=%d, bestSell=%d, midGap=%d\n",
		bestBuy.rate, bestSell.rate, midGap)
	return midGap
}

// snap generates a snapshot of the book.
func (ob *orderbook) snap() (bids, asks []*obEntry) {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bids = make([]*obEntry, 0, ob.bids.Len())
	for curr := ob.bids.Front(); curr != nil; curr = curr.Next() {
		bids = append(bids, curr.Value.(*obEntry))
	}

	asks = make([]*obEntry, 0, ob.asks.Len())
	for curr := ob.asks.Front(); curr != nil; curr = curr.Next() {
		asks = append(asks, curr.Value.(*obEntry))
	}

	return bids, asks
}
