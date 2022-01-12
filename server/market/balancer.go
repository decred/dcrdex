// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"fmt"

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
	// Base is the base asset ID.
	Base() uint32
	// Quote is the quote asset ID.
	Quote() uint32
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
	assets          map[uint32]*backedBalancer
	matchNegotiator MatchNegotiator
}

// NewDEXBalancer is a constructor for a DEXBalancer. Provided assets will
// be filtered for those that are account-based. The matchNegotiator is
// satisfied by the *Swapper.
func NewDEXBalancer(tunnels map[string]PendingAccounter, assets map[uint32]*asset.BackedAsset, matchNegotiator MatchNegotiator) (*DEXBalancer, error) {
	balancers := make(map[uint32]*backedBalancer)

	addAsset := func(ba *asset.BackedAsset) error {
		assetID := ba.ID
		balancer, is := ba.Backend.(asset.AccountBalancer)
		if !is {
			return nil
		}

		var markets []PendingAccounter
		for _, mkt := range tunnels {
			if mkt.Base() == assetID || mkt.Quote() == assetID {
				markets = append(markets, mkt)
			}
		}

		bb := &backedBalancer{
			balancer:  balancer,
			assetInfo: &ba.Asset,
			feeFamily: make(map[uint32]*dex.Asset),
			markets:   markets,
		}
		balancers[assetID] = bb

		isToken, parentID := asset.IsToken(assetID)
		if isToken {
			parent, found := balancers[parentID]
			if !found {
				return fmt.Errorf("%s parent asset %d(%s)", ba.Symbol, parentID, dex.BipIDSymbol(parentID))
			}
			bb.feeFamily[parentID] = parent.assetInfo
			for tokenID := range asset.Tokens(parentID) {
				if tokenID == assetID { // Don't double count
					continue
				}
				bb.feeFamily[tokenID] = &assets[tokenID].Asset
			}
			bb.feeBalancer = parent
		} else {
			for tokenID := range asset.Tokens(assetID) {
				if familyAsset, found := assets[tokenID]; found {
					if _, is := familyAsset.Backend.(asset.AccountBalancer); !is {
						return fmt.Errorf("fee-family asset %s is not an AccountBalancer", familyAsset.Symbol)
					}
					bb.feeFamily[tokenID] = &familyAsset.Asset
				}
			}
		}
		return nil
	}

	// Add base chain assets first, then tokens.
	tokens := make([]*asset.BackedAsset, 0)

	for assetID, ba := range assets {
		if isToken, _ := asset.IsToken(assetID); isToken {
			tokens = append(tokens, ba)
			continue
		}
		if err := addAsset(ba); err != nil {
			return nil, err
		}
	}

	for _, ba := range tokens {
		if err := addAsset(ba); err != nil {
			return nil, err
		}
	}

	return &DEXBalancer{
		assets:          balancers,
		matchNegotiator: matchNegotiator,
	}, nil
}

// CheckBalance checks if there is sufficient balance to support the specified
// new funding and redemptions, given the existing orders throughout DEX that
// fund from or redeem to the specified account address for the account-based
// asset. It is an internally logged error to call CheckBalance for a
// non-account-based asset or an asset that is was not provided to the
// constructor.
func (b *DEXBalancer) CheckBalance(acctAddr string, assetID, redeemAssetID uint32, qty, lots uint64, redeems int) bool {
	backedAsset, found := b.assets[assetID]
	if !found {
		log.Errorf("(*DEXBalancer).CheckBalance: asset ID %d not a configured backedBalancer", assetID)
		return false
	}

	log.Tracef("balance check for %s - %s: new qty = %d, new lots = %d, new redeems = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems)

	// Make sure we can get the primary balance first.
	bal, err := backedAsset.balancer.AccountBalance(acctAddr)
	if err != nil {
		log.Error("(*DEXBalancer).CheckBalance: error getting account balance for %q: %v", acctAddr, err)
		return false
	}
	if bal == 0 {
		log.Tracef("(*DEXBalancer).CheckBalance(%q, %d, %d, %d, %d) false for zero balance",
			acctAddr, assetID, qty, lots, redeems)
		return false
	}

	// Set up fee tracking for tokens.
	var feeID uint32 // Asset ID of the base chain asset.
	// feeQty will track the fee asset order-locked quantity, but is only used
	// for tokens. When not a token (when assetID = base chain), the
	// order-locked quantity is summed in reqFunds as for any other asset. We'll
	// fetch feeBal to catch any zero bals or other errors early before
	// iterating markets and querying the Swapper.
	var feeQty, feeBal uint64 // Current balance.
	feeBalancer := backedAsset.feeBalancer
	isToken := feeBalancer != nil
	if isToken {
		feeID = feeBalancer.assetInfo.ID
		feeBal, err = feeBalancer.balancer.AccountBalance(acctAddr)
		if err != nil {
			log.Error("(*DEXBalancer).CheckBalance: error getting fee asset balance for %q: %v", acctAddr, err)
			return false
		}
		if feeBal == 0 {
			log.Tracef("(*DEXBalancer).CheckBalance(%q, %d, %d, %d, %d) false for zero fee asset (%s) balance",
				acctAddr, assetID, qty, lots, redeems, feeBalancer.assetInfo.Symbol)
			return false
		}
	}

	var swapFees, redeemFees uint64
	addFees := func(assetInfo *dex.Asset, l uint64, r int) {
		// The fee rate assigned to redemptions is at the discretion of the
		// user. MaxFeeRate is used as a conservatively high estimate. This is
		// then a server policy that clients must satisfy.
		redeemFees += uint64(r) * assetInfo.RedeemSize * assetInfo.MaxFeeRate
		swapFees += calc.RequiredOrderFunds(0, 0, l, assetInfo) // Alt: assetInfo.SwapSize * l
	}

	// Add the fees for the requested lots.
	addFees(backedAsset.assetInfo, lots, redeems)

	// Prepare a function to add fees pending orders for the asset and each
	// asset in the fee-family.
	addPending := func(assetID uint32) (q uint64) {
		ba, found := b.assets[assetID]
		if !found {
			log.Errorf("(*DEXBalancer).CheckBalance: asset ID %d not a configured backedBalancer", assetID)
			return 0
		}

		var l uint64
		var r int
		for _, mt := range ba.markets {
			newQty, newLots, newRedeems := mt.AccountPending(acctAddr, assetID)
			l += newLots
			q += newQty
			r += newRedeems
		}

		// Add in-process swaps.
		newQty, newLots, newRedeems := b.matchNegotiator.AccountStats(acctAddr, assetID)
		l += newLots
		q += newQty
		r += newRedeems

		addFees(ba.assetInfo, l, r)
		return
	}

	qty += addPending(assetID)

	// Add fee-family asset pending fees.
	for famID := range backedAsset.feeFamily {
		if q := addPending(famID); isToken && famID == feeID {
			// Add the quantity of the base chain order reserves to our fee
			// asset balance check.
			feeQty = q
		}
	}

	// Add redeems if we're redeeming this to a fee-family asset.
	if redeemAsset, found := backedAsset.feeFamily[redeemAssetID]; found {
		redeemFees += redeemAsset.RedeemSize * redeemAsset.MaxFeeRate * lots
	}

	reqFunds := qty
	if isToken { // Check sufficient fee asset balance for tokens.
		reqFeeAssetQty := swapFees + redeemFees + feeQty
		if feeBal < reqFeeAssetQty {
			log.Tracef("(*DEXBalancer).CheckBalance(%q, %d, %d, %d, %d) false for low fee asset balance. %d < %d",
				acctAddr, assetID, qty, lots, redeems, feeBal, reqFeeAssetQty)
			return false
		}
	} else { // Add fees to main qty for base chain assets.
		reqFunds += redeemFees + swapFees
	}

	log.Tracef("(*DEXBalancer).CheckBalance: balance check for %s - %s: total qty = %d, "+
		"total lots = %d, total redeems = %d, redeemCosts = %d, required = %d, bal = %d",
		backedAsset.assetInfo.Symbol, acctAddr, qty, lots, redeems, redeemFees, reqFunds, bal)

	return bal >= reqFunds
}

// backedBalancer is similar to a BackedAsset, but with the Backends already
// cast to AccountBalancer.
type backedBalancer struct {
	balancer    asset.AccountBalancer
	assetInfo   *dex.Asset
	feeBalancer *backedBalancer       // feeBalancer != nil implies that this is a token
	feeFamily   map[uint32]*dex.Asset // Excluding self
	markets     []PendingAccounter
}
