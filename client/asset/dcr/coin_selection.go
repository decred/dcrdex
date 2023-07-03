// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"math"
	"math/rand"
	"sort"
	"time"

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

// reserveEnough generates a function that can be used as the enough argument
// to the fund method. The function returns true if sum is greater than equal
// to amt.
func reserveEnough(amt uint64) func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64) {
	return func(sum uint64, _ uint32, unspent *compositeUTXO) (bool, uint64) {
		return sum+toAtoms(unspent.rpc.Amount) >= amt, 0
	}
}

func sumUTXOSize(set []*compositeUTXO) (tot uint32) {
	for _, utxo := range set {
		tot += utxo.input.Size()
	}
	return tot
}

func sumUTXOs(set []*compositeUTXO) (tot uint64) {
	for _, utxo := range set {
		tot += toAtoms(utxo.rpc.Amount)
	}
	return tot
}

// subsetWithLeastSumGreaterThan attempts to select the subset of UTXOs with
// the smallest total value greater than amt. It does this by making
// 1000 random selections and returning the best one. Each selection
// involves two passes over the UTXOs. The first pass randomly selects
// each UTXO with 50% probability. Then, the second pass selects any
// unused UTXOs until the total value is greater than or equal to amt.
func subsetWithLeastOverFund(enough func(uint64, uint32, *compositeUTXO) (bool, uint64), maxFund uint64, utxos []*compositeUTXO) []*compositeUTXO {
	best := uint64(1 << 62)
	var bestIncluded []bool
	bestNumIncluded := 0

	rnd := rand.New(rand.NewSource(time.Now().Unix()))

	shuffledUTXOs := make([]*compositeUTXO, len(utxos))
	copy(shuffledUTXOs, utxos)
	rnd.Shuffle(len(shuffledUTXOs), func(i, j int) {
		shuffledUTXOs[i], shuffledUTXOs[j] = shuffledUTXOs[j], shuffledUTXOs[i]
	})

	included := make([]bool, len(utxos))
	const iterations = 1000

	for nRep := 0; nRep < iterations; nRep++ {
		var nTotal uint64
		var totalSize uint32
		var numIncluded int

		for nPass := 0; nPass < 2; nPass++ {
			for i := 0; i < len(shuffledUTXOs); i++ {
				var use bool
				if nPass == 0 {
					use = rnd.Int63()&1 == 1
				} else {
					use = !included[i]
				}
				if use {
					included[i] = true
					numIncluded++
					totalBefore := nTotal
					sizeBefore := totalSize
					nTotal += toAtoms(shuffledUTXOs[i].rpc.Amount)
					totalSize += shuffledUTXOs[i].input.Size()

					if e, _ := enough(totalBefore, sizeBefore, shuffledUTXOs[i]); e {
						if nTotal < best || (nTotal == best && numIncluded < bestNumIncluded) && nTotal <= maxFund {
							best = nTotal
							if bestIncluded == nil {
								bestIncluded = make([]bool, len(shuffledUTXOs))
							}
							copy(bestIncluded, included)
							bestNumIncluded = numIncluded
						}

						included[i] = false
						numIncluded--
						nTotal -= toAtoms(shuffledUTXOs[i].rpc.Amount)
						totalSize -= shuffledUTXOs[i].input.Size()
					}
				}
			}
		}
		for i := 0; i < len(included); i++ {
			included[i] = false
		}
	}

	if bestIncluded == nil {
		return nil
	}

	set := make([]*compositeUTXO, 0, len(shuffledUTXOs))
	for i, inc := range bestIncluded {
		if inc {
			set = append(set, shuffledUTXOs[i])
		}
	}

	return set
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
// reaches the amount with least over-funding (see subsetWithLeastSumGreaterThan).
// If that subset has less combined value than the single
// sufficiently-large UTXO (if it exists), the subset will be returned,
// otherwise the single UTXO will be returned.
//
// If the provided UTXO set has less combined value than the requested amount a
// nil slice is returned.
func leastOverFund(enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64), utxos []*compositeUTXO) []*compositeUTXO {
	return leastOverFundWithLimit(enough, math.MaxUint64, utxos)
}

// enoughWithoutAdditional is used to utilize an "enough" function with a set
// of UTXOs when we are not looking to add another UTXO to the set, just
// check if the current set if UTXOs is enough.
func enoughWithoutAdditional(enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64), utxos []*compositeUTXO) bool {
	if len(utxos) == 0 {
		return false
	}

	if len(utxos) == 1 {
		e, _ := enough(0, 0, utxos[0])
		return e
	}

	valueWithoutLast := sumUTXOs(utxos[:len(utxos)-1])
	sizeWithoutLast := sumUTXOSize(utxos[:len(utxos)-1])

	e, _ := enough(valueWithoutLast, sizeWithoutLast, utxos[len(utxos)-1])
	return e
}

// leastOverFundWithLimit is the same as leastOverFund, but with an additional
// maxFund parameter. The total value of the returned UTXOs will not exceed
// maxFund.
func leastOverFundWithLimit(enough func(sum uint64, size uint32, unspent *compositeUTXO) (bool, uint64), maxFund uint64, utxos []*compositeUTXO) []*compositeUTXO {
	// Remove the UTXOs that are larger than maxFund
	var smallEnoughUTXOs []*compositeUTXO
	idx := sort.Search(len(utxos), func(i int) bool {
		utxo := utxos[i]
		return toAtoms(utxo.rpc.Amount) > maxFund
	})
	if idx == len(utxos) {
		smallEnoughUTXOs = utxos
	} else {
		smallEnoughUTXOs = utxos[:idx]
	}

	// Partition - smallest UTXO that is large enough to fully fund, and the set
	// of smaller ones.
	idx = sort.Search(len(smallEnoughUTXOs), func(i int) bool {
		utxo := smallEnoughUTXOs[i]
		e, _ := enough(0, 0, utxo)
		return e
	})
	var small []*compositeUTXO
	var single *compositeUTXO         // only return this if smaller ones would use more
	if idx == len(smallEnoughUTXOs) { // no one is enough
		small = smallEnoughUTXOs
	} else {
		small = smallEnoughUTXOs[:idx]
		single = smallEnoughUTXOs[idx]
	}

	var set []*compositeUTXO
	if !enoughWithoutAdditional(enough, small) {
		if single != nil {
			return []*compositeUTXO{single}
		} else {
			return nil
		}
	} else {
		set = subsetWithLeastOverFund(enough, maxFund, small)
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
