// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package trade

import (
	"math"

	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/tatanka/tanka"
)

// FeeParameters combines the user's fee exposure settings with the fees per
// lot, which are based on current on-chain fee rates and other asset-specific
// parameters e.g. swap tx size for utxo-based assets.
type FeeParameters struct {
	// MaxFeeExposure is the maximum fee losses we are willing to incur from a
	// trade, as a ratio of the trade size.
	MaxFeeExposure    float64
	BaseFeesPerMatch  uint64
	QuoteFeesPerMatch uint64
}

const (
	ReasonOurQtyTooSmall   = "our order size is less than their lot size"
	ReasonTheirQtyTooSmall = "their order size is less than our lot size"
)

// OrderIsMatchable determines whether a given standing limit order is
// matchable for our desired quantity and fee parameters.
func OrderIsMatchable(desiredQty uint64, theirOrd *tanka.Order, p *FeeParameters) (_ bool, reason string) {
	// Can we satisfy their lot size?
	if desiredQty < theirOrd.LotSize {
		return false, ReasonOurQtyTooSmall
	}
	// Can they satisfy our lot size?
	minLotSize := MinimumLotSize(theirOrd.Rate, p)
	if theirOrd.Qty < minLotSize {
		return false, ReasonTheirQtyTooSmall
	}
	return true, ""
}

// MinimumLotSize calculates the smallest lot size that satisfies our
// desired maximum fee exposure.
func MinimumLotSize(msgRate uint64, p *FeeParameters) uint64 {
	// fee_exposure = (lots * base_fees_per_lot / qty) + (lots * quote_fees_per_lot / quote_qty)
	// quote_qty = qty * rate
	// ## We want fee_exposure < max_fee_exposure
	// (lots * base_fees_per_lot / qty) + (lots * quote_fees_per_lot / (qty * rate)) < max_fee_exposure
	// ## multiplying both sides by qty
	// (lots * base_fees_per_lot) + (lots * quote_fees_per_lot / rate) < max_fee_exposure * qty
	// ## Factoring out lots
	// lots * (base_fees_per_lot + (quote_fees_per_lot / rate)) < max_fee_exposure * qty
	// ## isolating lots
	// lots < (max_fee_exposure * qty) / (base_fees_per_lot + (quote_fees_per_lot / rate))
	// ## Noting that lots = qty / lot_size
	// qty / lot_size < (max_fee_exposure * qty) / (base_fees_per_lot + (quote_fees_per_lot / rate))
	// ## Dividing both size by qty
	// 1 / lot_size < max_fee_exposure / (base_fees_per_lot + (quote_fees_per_lot / rate))
	// ## Fliparoo
	// lot_size > (base_fees_per_lot + (quote_fees_per_lot / rate)) / max_fee_exposure
	atomicRate := float64(msgRate) / calc.RateEncodingFactor
	perfectLotSize := math.Ceil((float64(p.BaseFeesPerMatch) + float64(p.QuoteFeesPerMatch)/atomicRate) / float64(p.MaxFeeExposure))
	// How many powers of 2?
	n := math.Ceil(math.Log2(perfectLotSize))
	return uint64(math.Round(math.Pow(2, n)))
}
