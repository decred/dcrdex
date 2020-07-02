package calc

import (
	"math"

	"decred.org/dcrdex/dex"
)

// RequiredOrderFunds calculates the required amount needed to fulfill the swap
// amount and pay transaction fees. swapVal is the total quantity needed to
// fulfill an order. inputsSize is the size of the serialized inputs associated
// with a set of UTXOs to be spent in the *first* swap txn. The chained swap
// txns will be the standard size as they will spend a previous swap's change
// output.
func RequiredOrderFunds(swapVal, inputsSize uint64, coin *dex.Asset) uint64 {
	maxSwaps := swapVal / coin.LotSize
	if maxSwaps == 0 {
		return math.MaxUint64 // caller messed up
	}

	baseBytes := maxSwaps * coin.SwapSize
	// SwapSize already includes one input, replace the size of the first swap
	// in the chain with the given size of the actual inputs + SwapSizeBase.
	firstSwapSize := inputsSize + coin.SwapSizeBase
	totalBytes := baseBytes + firstSwapSize - coin.SwapSize // in this order because we are using unsigned integers
	fee := totalBytes * coin.MaxFeeRate
	return swapVal + fee
}
