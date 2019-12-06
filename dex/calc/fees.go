package calc

import "decred.org/dcrdex/dex"

// RequiredFunds calculates the minimum amount needed to fulfill the swap amount
// and pay transaction fees. The backingSize is the sum size of the serialized
// inputs associated with a set of UTXOs to be spent. The swapVal is the total
// quantity needed to fulfill an order.
func RequiredFunds(swapVal uint64, backingSize uint32, coin *dex.Asset) uint64 {
	R := float64(coin.SwapSize) * float64(coin.FeeRate) / float64(coin.LotSize)
	fBase := uint64(float64(swapVal) * R)
	fUtxo := uint64(backingSize) * coin.FeeRate
	return swapVal + fBase + fUtxo
}
