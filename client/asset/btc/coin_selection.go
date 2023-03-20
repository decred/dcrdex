// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"math/rand"
	"sort"
	"time"

	"decred.org/dcrdex/dex/calc"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

// sendEnough generates a function that can be used as the enough argument to
// the fund method when creating transactions to send funds. If fees are to be
// subtracted from the inputs, set subtract so that the required amount excludes
// the transaction fee. If change from the transaction should be considered
// immediately available, set reportChange to indicate this and the returned
// enough func will return a non-zero excess value. Otherwise, the enough func
// will always return 0, leaving only unselected UTXOs to cover any required
// reserves.
func sendEnough(amt, feeRate uint64, subtract bool, baseTxSize uint64, segwit, reportChange bool) func(inputSize, sum uint64) (bool, uint64) {
	return func(inputSize, sum uint64) (bool, uint64) {
		txFee := (baseTxSize + inputSize) * feeRate
		req := amt
		if !subtract { // add the fee to required
			req += txFee
		}
		if sum < req {
			return false, 0
		}
		excess := sum - req
		if !reportChange || dexbtc.IsDustVal(dexbtc.P2PKHOutputSize, excess, feeRate, segwit) {
			excess = 0
		}
		return true, excess
	}
}

// orderEnough generates a function that can be used as the enough argument to
// the fund method. If change from a split transaction will be created AND
// immediately available, set reportChange to indicate this and the returned
// enough func will return a non-zero excess value reflecting this potential
// spit tx change. Otherwise, the enough func will always return 0, leaving
// only unselected UTXOs to cover any required reserves.
func orderEnough(val, lots, feeRate, initTxSizeBase, initTxSize uint64, segwit, reportChange bool) func(inputsSize, sum uint64) (bool, uint64) {
	return func(inputsSize, sum uint64) (bool, uint64) {
		reqFunds := calc.RequiredOrderFundsAlt(val, inputsSize, lots, initTxSizeBase, initTxSize, feeRate)
		if sum >= reqFunds {
			excess := sum - reqFunds
			if !reportChange || dexbtc.IsDustVal(dexbtc.P2PKHOutputSize, excess, feeRate, segwit) {
				excess = 0
			}
			return true, excess
		}
		return false, 0
	}
}

func sumUTXOs(set []*compositeUTXO) (tot uint64) {
	for _, utxo := range set {
		tot += utxo.amount
	}
	return tot
}

// subsetWithLeastSumGreaterThan attempts to select the subset of UTXOs with
// the smallest total value greater than amt. It does this by making
// 1000 random selections and returning the best one. Each selection
// involves two passes over the UTXOs. The first pass randomly selects
// each UTXO with 50% probability. Then, the second pass selects any
// unused UTXOs until the total value is greater than or equal to amt.
func subsetWithLeastSumGreaterThan(amt uint64, utxos []*compositeUTXO) []*compositeUTXO {
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

searchLoop:
	for nRep := 0; nRep < iterations; nRep++ {
		var nTotal uint64
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
					nTotal += shuffledUTXOs[i].amount
					if nTotal >= amt {
						if nTotal < best || (nTotal == best && numIncluded < bestNumIncluded) {
							best = nTotal
							if bestIncluded == nil {
								bestIncluded = make([]bool, len(shuffledUTXOs))
							}
							copy(bestIncluded, included)
							bestNumIncluded = numIncluded
						}
						if nTotal == amt {
							break searchLoop
						}
						included[i] = false
						nTotal -= shuffledUTXOs[i].amount
						numIncluded--
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
func leastOverFund(amt uint64, utxos []*compositeUTXO) []*compositeUTXO {
	if amt == 0 || sumUTXOs(utxos) < amt {
		return nil
	}

	// Partition - smallest UTXO that is large enough to fully fund, and the set
	// of smaller ones.
	idx := sort.Search(len(utxos), func(i int) bool {
		return utxos[i].amount >= amt
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
		set = subsetWithLeastSumGreaterThan(amt, small)
	} else if single != nil {
		return []*compositeUTXO{single}
	}

	// Return the small UTXO subset if it is less than the single big UTXO.
	if single != nil && single.amount < sumUTXOs(set) {
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
