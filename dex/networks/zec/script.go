// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org

package zec

import (
	"math"

	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/wire"
)

// https://zips.z.cash/zip-0317

// TransparentTxFeesZIP317 calculates the ZIP-0317 fees for a fully transparent
// Zcash transaction, which only depends on the size of the tx_in and tx_out
// fields.
func TransparentTxFeesZIP317(txInSize, txOutSize uint64) uint64 {
	return TxFeesZIP317(txInSize, txOutSize, 0, 0, 0, 0)
}

// TxFeexZIP317 calculates fees for a transaction. The caller must sum up the
// txin and txout, which is the entire serialization size associated with the
// respective field, including the size of the count varint.
func TxFeesZIP317(transparentTxInsSize, transparentTxOutsSize uint64, nSpendsSapling, nOutputsSapling, nJoinSplit, nActionsOrchard uint64) uint64 {
	const (
		marginalFee           = 5000
		graceActions          = 2
		pkhStandardInputSize  = 150
		pkhStandardOutputSize = 34
	)

	nIn := math.Ceil(float64(transparentTxInsSize) / pkhStandardInputSize)
	nOut := math.Ceil(float64(transparentTxOutsSize) / pkhStandardOutputSize)

	nSapling := uint64(math.Max(float64(nSpendsSapling), float64(nOutputsSapling)))
	logicalActions := uint64(math.Max(nIn, nOut)) + 2*nJoinSplit + nSapling + nActionsOrchard

	return marginalFee * uint64(math.Max(graceActions, float64(logicalActions)))
}

// RequiredOrderFunds is the ZIP-0317 compliant version of
// calc.RequiredOrderFunds.
func RequiredOrderFunds(swapVal, inputCount, inputsSize, maxSwaps uint64) uint64 {
	// One p2sh output for the contract, 1 change output.
	const txOutsSize = dexbtc.P2PKHOutputSize + dexbtc.P2SHOutputSize + 1 /* wire.VarIntSerializeSize(2) */
	txInsSize := inputsSize + uint64(wire.VarIntSerializeSize(inputCount))
	firstTxFees := TransparentTxFeesZIP317(txInsSize, txOutsSize)
	if maxSwaps == 1 {
		return swapVal + firstTxFees
	}

	otherTxsFees := TransparentTxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, txOutsSize)
	fees := firstTxFees + (maxSwaps-1)*otherTxsFees
	return swapVal + fees
}

// MinHTLCSize calculates the minimum value for the output of a chained
// P2SH -> P2WPKH transaction pair where the spending tx size is known.
func MinHTLCSize(redeemTxFees uint64) uint64 {
	var outputSize uint64 = dexbtc.P2PKHOutputSize
	sz := outputSize + 148                        // 148 accounts for an input on spending tx
	const oneThirdDustThresholdRate = 100         // zats / kB
	nFee := oneThirdDustThresholdRate * sz / 1000 // This is different from BTC
	if nFee == 0 {
		nFee = oneThirdDustThresholdRate
	}
	htlcOutputDustMin := 3 * nFee

	return htlcOutputDustMin + redeemTxFees
}

// MinLotSize is the minimum lot size that avoids dust for a given max network
// fee rate.
func MinLotSize(maxFeeRate uint64) uint64 {
	var inputsSize uint64 = dexbtc.TxInOverhead + dexbtc.RefundSigScriptSize + 1
	var outputsSize uint64 = dexbtc.P2PKHOutputSize + 1
	refundBondTxFees := TxFeesZIP317(inputsSize, outputsSize, 0, 0, 0, 0)
	return MinHTLCSize(refundBondTxFees)
}
