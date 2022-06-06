// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"sync/atomic"

	"decred.org/dcrdex/client/asset"
)

type reservesCounter struct {
	done uint32 // mutable atomic

	num  uint32 // max lot/swap count
	mult uint64 // amount reserved per swap
	rem  uint64 // ideally zero, but handled if not
}

func newReservesCounter(num uint32, total uint64) *reservesCounter {
	return &reservesCounter{
		num:  num,
		mult: total / uint64(num),
		rem:  total % uint64(num),
	}
}

func (rc *reservesCounter) releaseN(count uint32) uint64 {
	if rc == nil {
		return 0
	}
	newDone := atomic.AddUint32(&rc.done, count)
	if newDone > rc.num { // caller bug, but work with it
		prevDone := newDone - count
		if prevDone >= rc.num {
			return 0
		}
		count = rc.num - prevDone
	}

	unlock := uint64(count) * rc.mult
	if newDone >= rc.num { // final
		unlock += rc.rem
	}
	return unlock
}

func (rc *reservesCounter) releaseAll() uint64 {
	if rc == nil {
		return 0
	}
	prevDone := atomic.SwapUint32(&rc.done, rc.num)
	if prevDone >= rc.num { // releaseAll will be routinely called when an order is retired
		return 0
	}
	count := rc.num - prevDone
	return uint64(count)*rc.mult + rc.rem
}

// accountRedeemer is equivalent to calling
// xcWallet.Wallet.(asset.RedeemReserver) on the to-wallet.
func (t *trackedTrade) accountRedeemer() (asset.RedeemReserver, bool) {
	ar, is := t.wallets.toWallet.Wallet.(asset.RedeemReserver)
	return ar, is
}

func (t *trackedTrade) reserveRedeems(count uint32) { // once
	if count == 0 {
		return
	}
	redeemer, is := t.accountRedeemer()
	if !is {
		return
	}
	total, err := redeemer.ReserveNRedemptions(uint64(count), t.wallets.toAsset)
	if err != nil {
		t.dc.log.Errorf("error reserving for %d redemptions: %v", count, err)
		return // that's alright?
	}
	t.redeemRes = newReservesCounter(count, total)
}
func (t *trackedTrade) unreserveRedeems(count uint32) {
	redeemer, is := t.accountRedeemer()
	if !is {
		return
	}
	unlock := t.redeemRes.releaseN(count)
	redeemer.UnlockRedemptionReserves(unlock)
}
func (t *trackedTrade) unreserveRemainingRedeems() {
	redeemer, is := t.accountRedeemer()
	if !is {
		return
	}
	unlock := t.redeemRes.releaseAll()
	redeemer.UnlockRedemptionReserves(unlock)
}

// accountRefunder is equivalent to calling
// xcWallet.Wallet.(asset.RefundReserver) on the from-wallet.
func (t *trackedTrade) accountRefunder() (asset.RefundReserver, bool) {
	ar, is := t.wallets.fromWallet.Wallet.(asset.RefundReserver)
	return ar, is
}

func (t *trackedTrade) reserveRefunds(count uint32) { // once
	if count == 0 {
		return
	}
	refunder, is := t.accountRefunder()
	if !is {
		return
	}
	total, err := refunder.ReserveNRefunds(uint64(count), t.wallets.fromAsset)
	if err != nil {
		t.dc.log.Errorf("error reserving for %d refunds: %v", count, err)
		return // that's alright?
	}
	t.refundRes = newReservesCounter(count, total)
}
func (t *trackedTrade) unreserveRefunds(count uint32) {
	refunder, is := t.accountRefunder()
	if !is {
		return
	}
	unlock := t.refundRes.releaseN(count)
	refunder.UnlockRefundReserves(unlock)
}
func (t *trackedTrade) unreserveMatchRefund(matchQty uint64) {
	count := uint32(matchQty / t.metaData.LotSize)
	t.unreserveRefunds(count)
}
func (t *trackedTrade) unreserveRemainingRefunds() {
	refunder, is := t.accountRefunder()
	if !is {
		return
	}
	unlock := t.refundRes.releaseAll()
	refunder.UnlockRefundReserves(unlock)
}

func (t *trackedTrade) unreserveMatches(count uint32) {
	t.unreserveRedeems(count)
	t.unreserveRefunds(count)
}
func (t *trackedTrade) unreserveMatch(matchQty uint64) {
	count := uint32(matchQty / t.metaData.LotSize)
	t.unreserveMatches(count)
}
func (t *trackedTrade) unreserveAll() {
	t.unreserveRemainingRedeems()
	t.unreserveRemainingRefunds()
}
func (t *trackedTrade) unreserveUnfilled() {
	filled, _ := t.filled() // correct before or after cancel
	unfilled := t.Trade().Quantity - filled
	if unfilled == 0 {
		return
	}

	if len(t.matches) == 0 {
		t.unreserveAll()
		return
	} else if t.isMarketBuy() {
		// Market buy with at least one match: keep reserves as a buffer until
		// order retire since the initial reserve amount is a wild estimate.
		return
	}

	t.unreserveMatch(unfilled)
}
