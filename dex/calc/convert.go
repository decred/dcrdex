// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package calc

import (
	"math/big"

	"decred.org/dcrdex/dex"
)

// RateEncodingFactor is used when encoding an exchange rate as an integer.
// https://github.com/decred/dcrdex/blob/master/spec/comm.mediawiki#Rate_Encoding
const RateEncodingFactor = 1e8

var (
	bigRateConversionFactor = big.NewInt(RateEncodingFactor)
)

// BaseToQuote computes a quote asset amount based on a base asset amount
// and an integer representation of the price rate. That is,
//    quoteAmt = rate * baseAmt / atomsPerCoin
func BaseToQuote(rate uint64, base uint64) (quote uint64) {
	bigRate := big.NewInt(int64(rate))
	bigBase := big.NewInt(int64(base))
	bigBase.Mul(bigBase, bigRate)
	bigBase.Div(bigBase, bigRateConversionFactor)
	return bigBase.Uint64()
}

// QuoteToBase computes a base asset amount based on a quote asset amount
// and an integer representation of the price rate. That is,
//    baseAmt = quoteAmt * atomsPerCoin / rate
func QuoteToBase(rate uint64, quote uint64) (base uint64) {
	if rate == 0 {
		return 0 // caller handle rate==0, but don't panic
	}
	bigRate := big.NewInt(int64(rate))
	bigQuote := big.NewInt(int64(quote))
	bigQuote.Mul(bigQuote, bigRateConversionFactor)
	bigQuote.Div(bigQuote, bigRate)
	return bigQuote.Uint64()
}

// ConventionalRate converts an exchange rate in message-rate encoding to a
// conventional exchange rate, using the base and quote assets' UnitInfo.
func ConventionalRate(msgRate uint64, baseInfo, quoteInfo dex.UnitInfo) float64 {
	return ConventionalRateAlt(msgRate, baseInfo.Conventional.ConversionFactor, quoteInfo.Conventional.ConversionFactor)
}

// ConventionalRateAlt converts an exchange rate in message-rate encoding to a
// conventional exchange rate using the base and quote assets' conventional
// conversion factors.
func ConventionalRateAlt(msgRate uint64, baseFactor, quoteFactor uint64) float64 {
	return float64(msgRate) / RateEncodingFactor * float64(baseFactor) / float64(quoteFactor)
}
