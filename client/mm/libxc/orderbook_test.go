// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
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

	sortAndConvertToQuote := func(bids bool, entries []*obEntry) []*obEntry {
		convertedEntries := make([]*obEntry, len(entries))
		for i, entry := range entries {
			convertedEntries[i] = &obEntry{
				rate: entry.rate,
				qty:  calc.BaseToQuote(entry.rate, entry.qty),
			}
		}
		sort.Slice(convertedEntries, func(i, j int) bool {
			lower := convertedEntries[i].rate < convertedEntries[j].rate
			if bids {
				return !lower
			}
			return lower
		})
		return convertedEntries
	}

	// Populate the book with some bids and asks. They both
	// have the same values, but VWAP for asks should be
	// calculate from the lower values first.
	bids := []*obEntry{
		{qty: 30, rate: 4000e8},
		{qty: 30, rate: 5000e8},
		{qty: 80, rate: 400e8},
		{qty: 10, rate: 3000e8},
	}
	asks := []*obEntry{
		{qty: 30, rate: 4000e8},
		{qty: 30, rate: 5000e8},
		{qty: 80, rate: 400e8},
		{qty: 10, rate: 3000e8},
	}
	ob.update(bids, asks)
	quoteBids := sortAndConvertToQuote(true, bids)
	quoteAsks := sortAndConvertToQuote(false, asks)

	vwap, extrema, filled := ob.vwap(true, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP := uint64((30*bids[0].rate + 30*bids[1].rate + 5*bids[3].rate) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 3000e8 {
		t.Fatalf("wrong extrema")
	}

	vwap, extrema, filled = ob.vwap(false, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64(400e8)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 400e8 {
		t.Fatalf("wrong extrema")
	}

	quoteBidsQty := quoteBids[0].qty + quoteBids[1].qty
	expVwap := uint64((quoteBids[0].qty*quoteBids[0].rate + quoteBids[1].qty*quoteBids[1].rate) / quoteBidsQty)
	vwap, extrema, filled = ob.invVWAP(true, quoteBidsQty)
	if !filled {
		t.Fatalf("should be filled")
	}
	if vwap != expVwap {
		t.Fatalf("wrong vwap. expected %d got %d", expVwap, vwap)
	}
	if quoteBids[1].rate != extrema {
		t.Fatalf("wrong extrema")
	}

	quoteAsksQty := quoteAsks[0].qty + quoteAsks[1].qty
	expVwap = uint64((quoteAsks[0].qty*quoteAsks[0].rate + quoteAsks[1].qty*quoteAsks[1].rate) / quoteAsksQty)
	vwap, extrema, filled = ob.invVWAP(false, quoteAsksQty)
	if !filled {
		t.Fatalf("should be filled")
	}
	if vwap != expVwap {
		t.Fatalf("wrong vwap. expected %d got %d", expVwap, vwap)
	}
	if quoteAsks[1].rate != extrema {
		t.Fatalf("wrong extrema")
	}

	// Tests querying more quantity than on books
	_, _, filled = ob.vwap(true, 161)
	if filled {
		t.Fatalf("should not be filled")
	}
	_, _, filled = ob.vwap(false, 161)
	if filled {
		t.Fatalf("should not be filled")
	}

	// Update quantities. Setting qty to 0 should delete.
	bids = []*obEntry{
		{qty: 0, rate: 5000e8},
		{qty: 50, rate: 4000e8},
	}
	asks = []*obEntry{
		{qty: 0, rate: 400e8},
		{qty: 35, rate: 4000e8},
	}
	ob.update(bids, asks)

	vwap, extrema, filled = ob.vwap(true, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64((50*uint64(4000e8) + 10*uint64(3000e8) + 5*uint64(400e8)) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != uint64(400e8) {
		t.Fatalf("wrong extrema")
	}

	vwap, extrema, filled = ob.vwap(false, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64((20*uint64(5000e8) + 35*uint64(4000e8) + 10*uint64(3000e8)) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != uint64(5000e8) {
		t.Fatalf("wrong extrema")
	}
}
