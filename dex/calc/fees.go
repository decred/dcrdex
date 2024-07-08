// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package calc

// RequiredOrderFunds calculates the funds required for an order.
func RequiredOrderFunds(swapVal, inputsSize, maxSwaps, swapSizeBase, swapSize, feeRate uint64) uint64 {
	baseBytes := maxSwaps * swapSize
	// SwapSize already includes one input, replace the size of the first swap
	// in the chain with the given size of the actual inputs + SwapSizeBase.
	firstSwapSize := inputsSize + swapSizeBase
	totalBytes := baseBytes + firstSwapSize - swapSize // in this order because we are using unsigned integers
	fee := totalBytes * feeRate
	return swapVal + fee
}
