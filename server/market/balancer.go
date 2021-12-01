// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/server/asset"
)

// BackedBalancer is an asset manager that is capable of querying the entire DEX
// for the balance required to fulfill new + existing orders and outstanding
// redemptions.
type DEXBalancer struct {
	tunnels         map[string]MarketTunnel
	assets          map[uint32]*backedBalancer
	matchNegotiator MatchNegotiator
}

// NewDEXBalancer is a constructor for a DEXBalancer. Provided assets will
// be filtered for those that are account-based. The matchNegotitator is
// satisfied by the *Swapper.
func NewDEXBalancer(tunnels map[string]MarketTunnel, assets map[uint32]*asset.BackedAsset, matchNegotiator MatchNegotiator) *DEXBalancer {
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
		log.Errorf("asset ID %d not found in accountBalancer assets map", assetID)
		return false
	}

	log.Tracef("balance check for %s - %s: new qty = %d, new lots = %d, new redeems = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems)

	bal, err := backedAsset.balancer.AccountBalance(acctAddr)
	if err != nil {
		log.Error("error getting account balance for %q: %v", acctAddr, err)
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
	redeemCosts := uint64(redeems) * assetInfo.RedeemSize * assetInfo.MaxFeeRate
	reqFunds := calc.RequiredOrderFunds(qty, 0, lots, assetInfo) + redeemCosts

	log.Tracef("balance check for %s - %s: total qty = %d, total lots = %d, "+
		"total redeems = %d, redeemCosts = %d, required = %d, bal = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems, redeemCosts, reqFunds, bal)

	return bal >= reqFunds
}

// backedBalancer is similar to a BackedAsset, but with the Backends already
// cast to AccountBalancer.
type backedBalancer struct {
	balancer  asset.AccountBalancer
	assetInfo *dex.Asset
}
