// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package trade

import (
	"testing"

	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/tatanka/tanka"
)

func TestMatchBook(t *testing.T) {
	// These can be varied before resetting.
	weSell := false
	var weWantLots uint64 = 1

	// These will be initialized by reset.
	var lotSize, atomicRate, msgRate, baseQty uint64
	var desire *DesiredTrade
	var ords []*tanka.Order
	var p *FeeParameters

	// A function to reset everything
	reset := func() {
		atomicRate = 1
		lotSize = 1024
		msgRate = atomicRate * calc.RateEncodingFactor
		baseQty = weWantLots * lotSize
		p = &FeeParameters{
			MaxFeeExposure:    0.01,
			BaseFeesPerMatch:  5,
			QuoteFeesPerMatch: 5,
		}
		desire = &DesiredTrade{
			Qty:  baseQty,
			Rate: msgRate,
			Sell: weSell,
		}
		ords = []*tanka.Order{
			{
				Rate:    msgRate,
				Qty:     baseQty,
				LotSize: lotSize,
			},
		}
	}

	// Our testing function
	testMatches := func(expRemain uint64, expQtys []uint64) {
		t.Helper()
		matches, remain := MatchBook(desire, p, ords)
		if remain != expRemain {
			t.Fatal("wrong remain", remain, expRemain)
		}
		if len(matches) != len(expQtys) {
			t.Fatal("wrong number of matches", matches, len(expQtys))
		}
		for i, m := range matches {
			if m.Qty != expQtys[i] {
				t.Fatal("wrong qty", i, m.Qty, expQtys[i])
			}
		}
	}

	// Basic 1-lot buy order that perfectly matches the 1 order on the book,
	// leaving no remainder.
	reset()
	testMatches(0, []uint64{baseQty})
	// Double the first order's qty. Shouldn't change anything.
	ords[0].Qty *= 2
	testMatches(0, []uint64{baseQty})
	// Triple our ask. We'll get the whole (now-doubled) order, with some
	// remainder.
	desire.Qty *= 3
	testMatches(baseQty, []uint64{2 * baseQty})
	// Add another order to satisfy our needs.
	ords = append(ords, &tanka.Order{
		Rate:    msgRate,
		Qty:     baseQty,
		LotSize: lotSize,
	})
	testMatches(0, []uint64{2 * baseQty, baseQty})
	// Double the rate of the new order though, and we're back to only getting
	// the first order.
	ords[1].Rate *= 2
	testMatches(baseQty, []uint64{2 * baseQty})

	// Make sure it works with multiple lots too.
	weWantLots = 4
	reset()
	testMatches(0, []uint64{baseQty})

	// Basic 1-lot sell order now.
	weSell = true
	reset()
	testMatches(0, []uint64{baseQty})
	// Doubling our ask should leave a remainder.
	desire.Qty *= 2
	testMatches(baseQty, []uint64{baseQty})
	// Add another order to satisfy our needs.
	ords = append(ords, &tanka.Order{
		Rate:    msgRate,
		Qty:     baseQty,
		LotSize: lotSize,
	})
	testMatches(0, []uint64{baseQty, baseQty})
	// But if the second order isn't offering enough, we can't match it.
	ords[1].Rate -= 1
	testMatches(baseQty, []uint64{baseQty})

	// Back to buying 1 lot
	weSell, weWantLots = false, 1
	reset()
	testMatches(0, []uint64{baseQty})

	// Make the order incompatible because our maximum fee exposure is too low.
	p.MaxFeeExposure /= 2
	testMatches(baseQty, nil)

	// Sanity check
	reset()
	testMatches(0, []uint64{baseQty})
}
