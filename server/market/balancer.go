// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/server/asset"
)

// PendingAccounter can view order-reserved funds for an account-based asset's
// address. PendingAccounter is satisfied by *Market.
type PendingAccounter interface {
	// AccountPending retreives the total pending order-reserved quantity for
	// the asset, as well as the number of possible pending redemptions
	// (a.k.a. ordered lots).
	AccountPending(acctAddr string, assetID uint32) (qty, lots uint64, redeems int)
}

// MatchNegotiator can view match-reserved funds for an account-based asset's
// address. MatchNegotiator is satisfied by *Swapper.
type MatchNegotiator interface {
	// AccountStats collects stats about pending matches for account's address
	// on an account-based asset. qty is the total pending outgoing quantity,
	// swaps is the number matches with oustanding swaps funded by the account,
	// and redeem is the number of matches with outstanding redemptions that pay
	// to the account.
	AccountStats(acctAddr string, assetID uint32) (qty, swaps uint64, redeems int)
}

// BackedBalancer is an asset manager that is capable of querying the entire DEX
// for the balance required to fulfill new + existing orders and outstanding
// redemptions.
type DEXBalancer struct {
	tunnels         map[string]PendingAccounter
	assets          map[uint32]*backedBalancer
	matchNegotiator MatchNegotiator
}

// NewDEXBalancer is a constructor for a DEXBalancer. Provided assets will
// be filtered for those that are account-based. The matchNegotiator is
// satisfied by the *Swapper.
func NewDEXBalancer(tunnels map[string]PendingAccounter, assets map[uint32]*asset.BackedAsset, matchNegotiator MatchNegotiator) *DEXBalancer {
	balancers := make(map[uint32]*backedBalancer)
	for assetID, ba := range assets {
		balancer, is := ba.Backend.(asset.AccountBalancer)
		if !is {
			continue
		}
		balancers[assetID] = &backedBalancer{
			balancer:  balancer,
			assetInfo: &ba.Asset,
		}
	}

	return &DEXBalancer{
		tunnels:         tunnels,
		assets:          balancers,
		matchNegotiator: matchNegotiator,
	}
}

// CheckBalance checks if there is sufficient balance to support the specified
// new funding and redemptions, given the existing orders throughout DEX that
// fund from or redeem to the specified account address for the account-based
// asset. It is an internally logged error to call CheckBalance for a
// non-account-based asset or an asset that is was not provided to the
// constructor.
func (b *DEXBalancer) CheckBalance(acctAddr string, assetID uint32, qty, lots uint64, redeems int) bool {
	backedAsset, found := b.assets[assetID]
	if !found {
		log.Errorf("(*DEXBalancer).CheckBalance: asset ID %d not a configured backedBalancer", assetID)
		return false
	}

	log.Tracef("balance check for %s - %s: new qty = %d, new lots = %d, new redeems = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems)

	bal, err := backedAsset.balancer.AccountBalance(acctAddr)
	if err != nil {
		log.Error("(*DEXBalancer).CheckBalance: error getting account balance for %q: %v", acctAddr, err)
		return false
	}

	// Add quantity for unfilled orders.
	for _, mt := range b.tunnels {
		newQty, newLots, newRedeems := mt.AccountPending(acctAddr, assetID)
		lots += newLots
		qty += newQty
		redeems += newRedeems
	}

	// Add in-process swaps.
	newQty, newLots, newRedeems := b.matchNegotiator.AccountStats(acctAddr, assetID)
	lots += newLots
	qty += newQty
	redeems += newRedeems

	assetInfo := backedAsset.assetInfo
	// The fee rate assigned to redemptions is at the discretion of the user.
	// MaxFeeRate is used as a conservatively high estimate. This is then a
	// server policy that clients must satisfy.
	redeemCosts := uint64(redeems) * assetInfo.RedeemSize * assetInfo.MaxFeeRate
	reqFunds := calc.RequiredOrderFunds(qty, 0, lots, assetInfo) + redeemCosts

	log.Tracef("(*DEXBalancer).CheckBalance: balance check for %s - %s: total qty = %d, "+
		"total lots = %d, total redeems = %d, redeemCosts = %d, required = %d, bal = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems, redeemCosts, reqFunds, bal)

	return bal >= reqFunds
}

// backedBalancer is similar to a BackedAsset, but with the Backends already
// cast to AccountBalancer.
type backedBalancer struct {
	balancer  asset.AccountBalancer
	assetInfo *dex.Asset
}
