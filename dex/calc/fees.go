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
// coin.LotSize, so it must be a separate parameter. The chained swap txns will
// be the standard size as they will spend a previous swap's change output.
func RequiredOrderFunds(swapVal, inputsSize, maxSwaps uint64, coin *dex.Asset) uint64 {
	baseBytes := maxSwaps * coin.SwapSize
	// SwapSize already includes one input, replace the size of the first swap
	// in the chain with the given size of the actual inputs + SwapSizeBase.
	firstSwapSize := inputsSize + coin.SwapSizeBase
	totalBytes := baseBytes + firstSwapSize - coin.SwapSize // in this order because we are using unsigned integers
	fee := totalBytes * coin.MaxFeeRate
	return swapVal + fee
}
