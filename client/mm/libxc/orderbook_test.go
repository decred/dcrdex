// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import "testing"

func TestOrderbook(t *testing.T) {
	ob := NewOrderBook()

	// Test vwap on empty books
	_, _, filled := ob.VWAP(true, 1)
	if filled {
		t.Fatalf("empty book should not be filled")
	}
	_, _, filled = ob.VWAP(false, 1)
	if filled {
		t.Fatalf("empty book should not be filled")
	}

	// Populate the book with some bids and asks. They both
	// have the same values, but VWAP for asks should be
	// calculate from the lower values first.
	ob.Update([]*PriceBin{
		{Qty: 30, Rate: 4000},
		{Qty: 30, Rate: 5000},
		{Qty: 80, Rate: 400},
		{Qty: 10, Rate: 3000},
	}, []*PriceBin{
		{Qty: 30, Rate: 4000},
		{Qty: 30, Rate: 5000},
		{Qty: 80, Rate: 400},
		{Qty: 10, Rate: 3000},
	})
	vwap, extrema, filled := ob.VWAP(true, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP := uint64((30*5000 + 30*4000 + 5*3000) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 3000 {
		t.Fatalf("wrong extrema")
	}

	vwap, extrema, filled = ob.VWAP(false, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64(400)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 400 {
		t.Fatalf("wrong extrema")
	}

	// Tests querying more quantity than on books
	_, _, filled = ob.VWAP(true, 161)
	if filled {
		t.Fatalf("should not be filled")
	}
	_, _, filled = ob.VWAP(false, 161)
	if filled {
		t.Fatalf("should not be filled")
	}

	// Update quantities. Setting qty to 0 should delete.
	ob.Update([]*PriceBin{
		{Qty: 0, Rate: 5000},
		{Qty: 50, Rate: 4000},
	}, []*PriceBin{
		{Qty: 0, Rate: 400},
		{Qty: 35, Rate: 4000},
	})

	vwap, extrema, filled = ob.VWAP(true, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64((50*4000 + 10*3000 + 5*400) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 400 {
		t.Fatalf("wrong extrema")
	}

	vwap, extrema, filled = ob.VWAP(false, 65)
	if !filled {
		t.Fatalf("should be filled")
	}
	expectedVWAP = uint64((20*5000 + 35*4000 + 10*3000) / 65)
	if vwap != expectedVWAP {
		t.Fatalf("wrong vwap. expected %d got %d", expectedVWAP, vwap)
	}
	if extrema != 5000 {
		t.Fatalf("wrong extrema")
	}
}
