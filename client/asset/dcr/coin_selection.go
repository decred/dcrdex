// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"sort"

	"decred.org/dcrdex/dex/calc"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
)

// sendEnough generates a function that can be used as the enough argument to
// the fund method when creating transactions to send funds. If fees are to be
// subtracted from the inputs, set subtract so that the required amount excludes
// the transaction fee. If change from the transaction should be considered
// immediately available (not mixing), set reportChange to indicate this and the
// returned enough func will return a non-zero excess value. Otherwise, the
// enough func will always return 0, leaving only unselected UTXOs to cover any
// required reserves.
func sendEnough(amt, feeRate uint64, subtract bool, baseTxSize uint32, reportChange bool) func(sum uint64, inputSize uint32, unspent *compositeUTXO) (bool, uint64) {
	return func(sum uint64, inputSize uint32, unspent *compositeUTXO) (bool, uint64) {
		total := sum + toAtoms(unspent.rpc.Amount)
		txFee := uint64(baseTxSize+inputSize+unspent.input.Size()) * feeRate
		req := amt
		if !subtract { // add the fee to required
			req += txFee
		}
		if total < req {
			return false, 0
		}
		excess := total - req
		if !reportChange || dexdcr.IsDustVal(dexdcr.P2PKHOutputSize, excess, feeRate) {
			excess = 0
		}
		return true, excess
	}
}

// orderEnough generates a function that can be used as the enough argument to
// the fund method. If change from a split transaction will be created AND
// immediately available (not mixing), set reportChange to indicate this and the
// returned enough func will return a non-zero excess value reflecting this
// potential spit tx change. Otherwise, the enough func will always return 0,
// leaving only unselected UTXOs to cover any required reserves.
func orderEnough(val, lots, feeRate uint64, reportChange bool) func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64) {
	return func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64) {
		reqFunds := calc.RequiredOrderFundsAlt(val, uint64(size+unspent.input.Size()), lots,
			dexdcr.InitTxSizeBase, dexdcr.InitTxSize, feeRate)
		total := sum + toAtoms(unspent.rpc.Amount) // all selected utxos

		if total >= reqFunds { // that'll do it
			// change = total - (val + swapTxnsFee)
			excess := total - reqFunds // reqFunds = val + swapTxnsFee
			if !reportChange || dexdcr.IsDustVal(dexdcr.P2PKHOutputSize, excess, feeRate) {
				excess = 0
			}
			return true, excess
		}
		return false, 0
	}
}

func sumUTXOs(set []*compositeUTXO) (tot uint64) {
	for _, utxo := range set {
		tot += toAtoms(utxo.rpc.Amount)
	}
	return tot
}

// In the following utxo selection functions, the compositeUTXO slice MUST be
// sorted in ascending order (smallest first, largest last).

// subsetLargeBias begins by summing from the largest UTXO down until the sum is
// just below the requested amount. Then it picks the (one) final UTXO that
// reaches the amount with the least excess. Each utxo in the input must be
// smaller than amt - use via leastOverFund only!
func subsetLargeBias(amt uint64, utxos []*compositeUTXO) []*compositeUTXO {
	// Add from largest down until sum is *just under*. Resulting sum will be
	// less, and index will be the next (smaller) element that would hit amt.
	var sum uint64
	var i int
	for i = len(utxos) - 1; i >= 0; i-- {
		this := toAtoms(utxos[i].rpc.Amount) // must not be >= amt
		if sum+this >= amt {
			break
		}
		sum += this
	}

	if i == -1 { // full set used
		// if sum >= amt { return utxos } // would have been i>=0 after break above
		return nil
	}
	// if i == len(utxos)-1 { return utxos[i:] } // shouldn't happen if each in set are < amt

	// Find the last one to meet amt, as small as possible.
	rem, set := utxos[:i+1], utxos[i+1:]
	idx := sort.Search(len(rem), func(i int) bool {
		return sum+toAtoms(rem[i].rpc.Amount) >= amt
	})

	return append([]*compositeUTXO{rem[idx]}, set...)
}

// subsetSmallBias begins by summing from the smallest UTXO up until the sum is
// at least the requested amount. Then it drops the smallest ones it had
// selected if they are not required to reach the amount.
func subsetSmallBias(amt uint64, utxos []*compositeUTXO) []*compositeUTXO {
	// Add from smallest up until sum is enough.
	var sum uint64
	var idx int
	for i, utxo := range utxos {
		sum += toAtoms(utxo.rpc.Amount)
		if sum >= amt {
			idx = i
			break
		}
	}
	if sum < amt {
		return nil
	}
	set := utxos[:idx+1]

	// Now drop excess small ones.
	for i, utxo := range set {
		sum -= toAtoms(utxo.rpc.Amount)
		if sum < amt {
			idx = i // needed this one
			break
		}
	}
	return set[idx:]
}

// leastOverFund attempts to pick a subset of the provided UTXOs to reach the
// required amount with the objective of minimizing the total amount of the
// selected UTXOs. This is different from the objective used when funding
// orders, which is to minimize the number of UTXOs (to minimize fees).
//
// The UTXOs MUST be sorted in ascending order (smallest first, largest last)!
//
// This begins by partitioning the slice before the smallest single UTXO that is
// large enough to fully fund the requested amount, if it exists. If the smaller
// set is insufficient, the single largest UTXO is returned. If instead the set
// of smaller UTXOs has enough total value, it will search for a subset that
// reaches the amount with least over-funding (see subsetSmallBias and
// subsetLargeBias). If that subset has less combined value than the single
// sufficiently-large UTXO (if it exists), the subset will be returned,
// otherwise the single UTXO will be returned.
//
// If the provided UTXO set has less combined value than the requested amount a
// nil slice is returned.
func leastOverFund(amt uint64, utxos []*compositeUTXO) []*compositeUTXO {
	if amt == 0 || sumUTXOs(utxos) < amt {
		return nil
	}

	// Partition - smallest UTXO that is large enough to fully fund, and the set
	// of smaller ones.
	idx := sort.Search(len(utxos), func(i int) bool {
		return toAtoms(utxos[i].rpc.Amount) >= amt
	})
	var small []*compositeUTXO
	var single *compositeUTXO // only return this if smaller ones would use more
	if idx == len(utxos) {    // no one is enough
		small = utxos
	} else {
		small = utxos[:idx]
		single = utxos[idx]
	}

	// Find a subset of the small UTXO set with smallest combined amount.
	var set []*compositeUTXO
	if sumUTXOs(small) >= amt {
		set = subsetLargeBias(amt, small)
		setX := subsetSmallBias(amt, small)
		if sumUTXOs(setX) < sumUTXOs(set) {
			set = setX
		}
	} else if single != nil {
		return []*compositeUTXO{single}
	}

	// Return the small UTXO subset if it is less than the single big UTXO.
	if single != nil && toAtoms(single.rpc.Amount) < sumUTXOs(set) {
		return []*compositeUTXO{single}
	}
	return set
}

// utxoSetDiff performs the setdiff(set,sub) of two UTXO sets. That is, any
// UTXOs that are both sets are removed from the first. The comparison is done
// *by pointer*, with no regard to the values of the compositeUTXO elements.
func utxoSetDiff(set, sub []*compositeUTXO) []*compositeUTXO {
	var availUTXOs []*compositeUTXO
avail:
	for _, utxo := range set {
		for _, kept := range sub {
			if utxo == kept { // by pointer
				continue avail
			}
		}
		availUTXOs = append(availUTXOs, utxo)
	}
	return availUTXOs
}
