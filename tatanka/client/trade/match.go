// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package trade

import (
	"decred.org/dcrdex/tatanka/tanka"
)

// DesiredTrade is the parameters of a trade that the user wants to make.
type DesiredTrade struct {
	Qty  uint64
	Rate uint64
	Sell bool
}

// MatchProposal is a potential match based on our desired trade and the
// current standing orders.
type MatchProposal struct {
	Order *tanka.Order
	Qty   uint64
}

// MatchBook matches our desired trade with the order book side. It is assumed
// that the order book side is correct for our choice of buy/sell, and that
// the orders are ordered by rate, with low-to-high for sell orders, and
// high-to-low for buy orders.
func MatchBook(desire *DesiredTrade, p *FeeParameters, ords []*tanka.Order) (matches []*MatchProposal, remain uint64) {
	remain = desire.Qty
	for _, ord := range ords {
		// Check rate compatibility.
		if desire.Sell {
			if ord.Rate < desire.Rate {
				break
			}
		} else if ord.Rate > desire.Rate {
			break
		}
		// Check lot size compatibility.
		if compat, _ := OrderIsMatchable(desire.Qty, ord, p); !compat {
			continue
		}
		// How much can we match?
		maxQty := ord.Qty
		if ord.Qty > remain {
			maxQty = remain
		}
		lots := maxQty / ord.LotSize
		qty := lots * ord.LotSize
		matches = append(matches, &MatchProposal{Order: ord, Qty: qty})
		remain -= qty
		if remain == 0 {
			break
		}
	}
	return matches, remain
}
