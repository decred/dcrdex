// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.
package libxc

import (
	"fmt"
	"math"
	"sync"

	"github.com/huandu/skiplist"
)

type PriceBin struct {
	Qty  uint64
	Rate uint64
}

// obEntryComparable is a skiplist.Comparable implementation for
// obEntry. It is used to sort the orderbook entries by rate.
type obEntryComparable int

const bidsComparable = obEntryComparable(0)
const asksComparable = obEntryComparable(1)

var _ skiplist.Comparable = (*obEntryComparable)(nil)

func (o obEntryComparable) Compare(lhs, rhs interface{}) int {
	lhsEntry := lhs.(*PriceBin)
	rhsEntry := rhs.(*PriceBin)

	var toReturn int
	if lhsEntry.Rate < rhsEntry.Rate {
		toReturn = -1
	}
	if lhsEntry.Rate > rhsEntry.Rate {
		toReturn = 1
	}

	if o == bidsComparable {
		toReturn = -toReturn
	}

	return toReturn
}

func (o obEntryComparable) CalcScore(key interface{}) float64 {
	if o == bidsComparable {
		return math.MaxFloat64 - float64(key.(*PriceBin).Rate)
	} else {
		return float64(key.(*PriceBin).Rate)
	}
}

// Orderbook is an implementation of a limit order book that allows for quick
// updates and calculation of the volume weighted average price (VWAP).
type Orderbook struct {
	mtx  sync.RWMutex
	bids skiplist.SkipList
	asks skiplist.SkipList
}

func NewOrderBook() *Orderbook {
	return &Orderbook{
		bids: *skiplist.New(bidsComparable),
		asks: *skiplist.New(asksComparable),
	}
}

func (ob *Orderbook) String() string {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bids := make([]PriceBin, 0, ob.bids.Len())
	for curr := ob.bids.Front(); curr != nil; curr = curr.Next() {
		bids = append(bids, *curr.Value.(*PriceBin))
	}

	asks := make([]PriceBin, 0, ob.asks.Len())
	for curr := ob.asks.Front(); curr != nil; curr = curr.Next() {
		asks = append(asks, *curr.Value.(*PriceBin))
	}

	return fmt.Sprintf("bids: %v, asks: %v", bids, asks)
}

// Update updates the orderbook new quantities at the given rates.
// If the quantity is 0, the entry is removed from the orderbook.
func (ob *Orderbook) Update(bids []*PriceBin, asks []*PriceBin) {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()

	for _, entry := range bids {
		if entry.Qty == 0 {
			ob.bids.Remove(entry)
			continue
		}
		ob.bids.Set(entry, entry)
	}

	for _, entry := range asks {
		if entry.Qty == 0 {
			ob.asks.Remove(entry)
			continue
		}
		ob.asks.Set(entry, entry)
	}
}

func (ob *Orderbook) Clear() {
	ob.mtx.Lock()
	defer ob.mtx.Unlock()

	ob.bids = *skiplist.New(bidsComparable)
	ob.asks = *skiplist.New(asksComparable)
}

func (ob *Orderbook) VWAP(bids bool, qty uint64) (vwap, extrema uint64, filled bool) {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	var list *skiplist.SkipList
	if bids {
		list = &ob.bids
	} else {
		list = &ob.asks
	}

	remaining := qty
	var weightedSum uint64
	for curr := list.Front(); curr != nil; curr = curr.Next() {
		if curr == nil {
			break
		}
		entry := curr.Value.(*PriceBin)
		extrema = entry.Rate
		if entry.Qty >= remaining {
			filled = true
			weightedSum += remaining * extrema
			break
		}
		remaining -= entry.Qty
		weightedSum += entry.Qty * extrema
	}
	if !filled {
		return 0, 0, false
	}

	return weightedSum / qty, extrema, filled
}

func (ob *Orderbook) MidGap() uint64 {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bestBuyI := ob.bids.Front()
	if bestBuyI == nil {
		return 0
	}
	bestSellI := ob.asks.Front()
	if bestSellI == nil {
		return 0
	}
	bestBuy, bestSell := bestBuyI.Value.(*PriceBin), bestSellI.Value.(*PriceBin)
	return (bestBuy.Rate + bestSell.Rate) / 2
}

// Snap generates a snapshot of the book.
func (ob *Orderbook) Snap() (bids, asks []*PriceBin) {
	ob.mtx.RLock()
	defer ob.mtx.RUnlock()

	bids = make([]*PriceBin, 0, ob.bids.Len())
	for curr := ob.bids.Front(); curr != nil; curr = curr.Next() {
		bids = append(bids, curr.Value.(*PriceBin))
	}

	asks = make([]*PriceBin, 0, ob.asks.Len())
	for curr := ob.asks.Front(); curr != nil; curr = curr.Next() {
		asks = append(asks, curr.Value.(*PriceBin))
	}

	return bids, asks
}
