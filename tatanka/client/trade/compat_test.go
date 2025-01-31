// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package trade

import (
	"testing"

	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/tatanka/tanka"
)

func TestMinimumLotSize(t *testing.T) {
	// Exchange rate of 1. 10 atoms lost to fees in the match. To stay under 1%,
	// the lot size needs to be >= 1000, so 1024 is the closest power of 2.
	p := &FeeParameters{
		MaxFeeExposure:    0.01,
		BaseFeesPerMatch:  5,
		QuoteFeesPerMatch: 5,
	}
	var atomicRate uint64 = 1
	msgRate := atomicRate * calc.RateEncodingFactor
	if l := MinimumLotSize(msgRate, p); l != 1024 {
		t.Fatal(p, l)
	}
	// Doubling the fees should double the lot size.
	p.QuoteFeesPerMatch *= 2
	p.BaseFeesPerMatch *= 2
	if l := MinimumLotSize(msgRate, p); l != 2048 {
		t.Fatal(p, l)
	}
	// Double the quote fees, but also double the rate (to halve the quote qty).
	// The effects should offset.
	p.QuoteFeesPerMatch *= 2
	msgRate *= 2
	if l := MinimumLotSize(msgRate, p); l != 2048 {
		t.Fatal(p, l)
	}
}

func TestOrderIsMatchable(t *testing.T) {
	// Set up fee parameters with a min lot size of 1024.
	p := &FeeParameters{
		MaxFeeExposure:    0.01,
		BaseFeesPerMatch:  5,
		QuoteFeesPerMatch: 5,
	}
	var desiredQty uint64 = 1024
	// Create a standing order that matches our lot size and has 2 lots
	// available.
	var atomicRate uint64 = 1
	msgRate := atomicRate * calc.RateEncodingFactor
	var lotSize uint64 = 1024
	theirOrd := &tanka.Order{
		LotSize: lotSize,
		Rate:    msgRate,
		Qty:     lotSize * 2,
	}
	// Should be matchable.
	if matchable, reason := OrderIsMatchable(desiredQty, theirOrd, p); !matchable {
		t.Fatal(reason)
	}
	// Quadrupling our min lot size should cause unmatchability.
	p.MaxFeeExposure /= 4
	matchable, reason := OrderIsMatchable(desiredQty, theirOrd, p)
	if matchable {
		t.Fatal("Their remaining qty should be too small")
	}
	if reason != ReasonTheirQtyTooSmall {
		t.Fatal(reason)
	}
	p.MaxFeeExposure *= 4 // undoing
	// Make our desired qty too small.
	desiredQty /= 2
	matchable, reason = OrderIsMatchable(desiredQty, theirOrd, p)
	if matchable {
		t.Fatal("Our desired qty should be too small")
	}
	if reason != ReasonOurQtyTooSmall {
		t.Fatal(reason)
	}
}
