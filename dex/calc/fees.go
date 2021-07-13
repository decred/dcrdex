// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package calc

import (
	"decred.org/dcrdex/dex"
)

// RequiredOrderFunds calculates the required amount needed to fulfill the swap
// amount and pay transaction fees. swapVal is the total quantity needed to
// fulfill an order. inputsSize is the size of the serialized inputs associated
// with a set of UTXOs to be spent in the *first* swap txn. maxSwaps is the
// number of lots in the order. For the quote asset, maxSwaps is not swapVal /
// lotSize, so it must be a separate parameter. The chained swap txns will be
// the standard size as they will spend a previous swap's change output.
func RequiredOrderFunds(swapVal, inputsSize, maxSwaps uint64, nfo *dex.Asset) uint64 {
	return RequiredOrderFundsAlt(swapVal, inputsSize, maxSwaps, nfo.SwapSizeBase, nfo.SwapSize, nfo.MaxFeeRate)
}

// RequiredOrderFundsAlt is the same as RequiredOrderFunds, but built-in type
// parameters.
func RequiredOrderFundsAlt(swapVal, inputsSize, maxSwaps uint64, swapSizeBase, swapSize, feeRate uint64) uint64 {
	baseBytes := maxSwaps * swapSize
	// SwapSize already includes one input, replace the size of the first swap
	// in the chain with the given size of the actual inputs + SwapSizeBase.
	firstSwapSize := inputsSize + swapSizeBase
	totalBytes := baseBytes + firstSwapSize - swapSize // in this order because we are using unsigned integers
	fee := totalBytes * feeRate
	return swapVal + fee
}
