// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"reflect"
	"sort"
	"testing"

	"decred.org/dcrdex/dex/calc"
)

func TestOrderbook(t *testing.T) {
	ob := newOrderBook()

	// Test vwap on empty books
	_, _, filled := ob.vwap(true, 1)
	if filled {
		t.Fatalf("empty book should not be filled")
	}
	_, _, filled = ob.vwap(false, 1)
	if filled {
		t.Fatalf("empty book should not be filled")
	}

	sortedEntries := func(bids bool, entries []*obEntry) []*obEntry {
		sorted := make([]*obEntry, len(entries))
		copy(sorted, entries)
		sort.Slice(sorted, func(i, j int) bool {
			if bids {
				return sorted[i].rate > sorted[j].rate
			}
			return sorted[i].rate < sorted[j].rate
		})
		return sorted
	}
	convertToQuote := func(entries []*obEntry) []*obEntry {
		convertedEntries := make([]*obEntry, len(entries))
		for i, entry := range entries {
			convertedEntries[i] = &obEntry{
				rate: entry.rate,
				qty:  calc.BaseToQuote(entry.rate, entry.qty),
			}
		}
		return convertedEntries
	}

	// Populate the book with some bids and asks. They both
	// have the same values, but VWAP for asks should be
	// calculated from the lower values first.
	bids := []*obEntry{
		{qty: 30e8, rate: 4000},
		{qty: 30e8, rate: 5000},
		{qty: 80e8, rate: 400},
		{qty: 10e8, rate: 3000},
	}
	asks := []*obEntry{
		{qty: 30e8, rate: 4000},
		{qty: 30e8, rate: 5000},
		{qty: 80e8, rate: 400},
		{qty: 10e8, rate: 3000},
	}
	ob.update(bids, asks)

	sortedBids := sortedEntries(true, bids)
	sortedAsks := sortedEntries(false, asks)
	quoteBids := convertToQuote(sortedBids)
	quoteAsks := convertToQuote(sortedAsks)
	snapBids, snapAsks := ob.snap()
	if !reflect.DeepEqual(snapBids, sortedBids) {
		t.Fatalf("wrong snap bids. expected %v got %v", sortedBids, snapBids)
	}
	if !reflect.DeepEqual(snapAsks, sortedAsks) {
		t.Fatalf("wrong snap asks. expected %v got %v", sortedAsks, snapAsks)
	}

	type vwapFn func(bids bool, qty uint64) (vwap, extrema uint64, filled bool)
	checkVWAPFn := func(fn vwapFn, bids bool, qty uint64, expVWAP, expExtrema uint64, expFilled bool) {
		t.Helper()
		vwap, extrema, filled := fn(bids, qty)
		if filled != expFilled {
			t.Fatalf("wrong filled. expected %v got %v", expFilled, filled)
		}
		if vwap != expVWAP {
			t.Fatalf("wrong vwap. expected %d got %d", expVWAP, vwap)
		}
		if extrema != expExtrema {
			t.Fatalf("wrong extrema. expected %d got %d", expExtrema, extrema)
		}
	}
	checkVWAP := func(bids bool, qty uint64, expVWAP, expExtrema uint64, expFilled bool) {
		t.Helper()
		checkVWAPFn(ob.vwap, bids, qty, expVWAP, expExtrema, expFilled)
	}
	checkInvVWAP := func(bids bool, qty uint64, expVWAP, expExtrema uint64, expFilled bool) {
		t.Helper()
		checkVWAPFn(ob.invVWAP, bids, qty, expVWAP, expExtrema, expFilled)
	}

	// Test vwap for bids and asks
	expVWAP := (sortedBids[0].rate*30e8 + sortedBids[1].rate*30e8 + sortedBids[2].rate*5e8) / 65e8
	checkVWAP(true, 65e8, expVWAP, 3000, true)
	checkVWAP(false, 65e8, 400, 400, true)

	// Test inv vwap for bids and asks
	quoteBidsQty := quoteBids[0].qty + quoteBids[1].qty
	expVWAP = calc.BaseQuoteToRate(sortedBids[0].qty+sortedBids[1].qty, quoteBidsQty)
	checkInvVWAP(true, quoteBidsQty, expVWAP, quoteBids[1].rate, true)
	quoteAsksQty := quoteAsks[0].qty + quoteAsks[1].qty
	expVWAP = calc.BaseQuoteToRate(sortedAsks[0].qty+sortedAsks[1].qty, quoteAsksQty)
	checkInvVWAP(false, quoteAsksQty, expVWAP, quoteAsks[1].rate, true)

	// Test vwap for qty > total qty
	checkVWAP(true, 161e8, 0, 0, false)
	checkVWAP(false, 161e8, 0, 0, false)

	// Update quantities. Setting qty to 0 should delete.
	bids = []*obEntry{
		{qty: 0, rate: 5000},
		{qty: 50e8, rate: 4000},
	}
	asks = []*obEntry{
		{qty: 0, rate: 400},
		{qty: 35e8, rate: 4000},
	}
	ob.update(bids, asks)

	// Make sure snap returns the correct entries
	expSnapBids := []*obEntry{
		{qty: 50e8, rate: 4000},
		{qty: 10e8, rate: 3000},
		{qty: 80e8, rate: 400},
	}
	expSnapAsks := []*obEntry{
		{qty: 10e8, rate: 3000},
		{qty: 35e8, rate: 4000},
		{qty: 30e8, rate: 5000},
	}
	snapBids, snapAsks = ob.snap()
	if !reflect.DeepEqual(snapBids, expSnapBids) {
		t.Fatalf("wrong snap bids. expected %v got %v", expSnapBids, snapBids)
	}
	if !reflect.DeepEqual(snapAsks, expSnapAsks) {
		t.Fatalf("wrong snap asks. expected %v got %v", expSnapAsks, snapAsks)
	}

	// Test vwap with updated quantities
	expVWAP = (50e8*uint64(4000) + 10e8*uint64(3000) + 5e8*uint64(400)) / 65e8
	checkVWAP(true, 65e8, expVWAP, 400, true)
	expVWAP = (10e8*uint64(3000) + 35e8*uint64(4000) + 20e8*uint64(5000)) / 65e8
	checkVWAP(false, 65e8, expVWAP, 5000, true)
}

// Test vwap and inv vwap with values that would overflow uint64.
func TestVWAPOverflow(t *testing.T) {
	bids := []*obEntry{
		{qty: 1e15, rate: 12e9},
		{qty: 1e15, rate: 10e9},
	}
	ob := newOrderBook()
	ob.update(bids, nil)

	vwap, extrema, filled := ob.vwap(true, 2e15)
	if !filled {
		t.Fatalf("should be filled")
	}
	if vwap != uint64(11e9) {
		t.Fatalf("wrong vwap. expected %d got %d", uint64(11e9), vwap)
	}
	if extrema != uint64(10e9) {
		t.Fatalf("wrong extrema")
	}

	var totalQuoteQty, totalBaseQty uint64
	for _, bid := range bids {
		totalQuoteQty += calc.BaseToQuote(bid.rate, bid.qty)
		totalBaseQty += bid.qty
	}
	vwap, extrema, filled = ob.invVWAP(true, totalQuoteQty)
	if !filled {
		t.Fatalf("should be filled")
	}
	if calc.QuoteToBase(vwap, totalQuoteQty) != totalBaseQty {
		t.Fatal("wrong inv vwap")
	}
	if extrema != uint64(10e9) {
		t.Fatalf("wrong extrema")
	}
}
