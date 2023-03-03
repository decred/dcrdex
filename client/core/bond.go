package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

const (
	// lockTimeLimit is an upper limit on the allowable bond lockTime.
	lockTimeLimit = 120 * 24 * time.Hour

	defaultBondAsset = 42 // DCR

	maxBondedMult = 4
	bondOverlap   = 2
)

func cutBond(bonds []*db.Bond, i int) []*db.Bond { // input slice modified
	bonds[i] = bonds[len(bonds)-1]
	bonds[len(bonds)-1] = nil
	bonds = bonds[:len(bonds)-1]
	return bonds
}

func (c *Core) watchBonds(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.rotateBonds(ctx)
		}
	}
}

// Bond lifetime
//
//   t0  t1                      t2'    t2                   t3  t4
//   |~~~|-----------------------^------|====================|+++|
//     ~                 -                        =
//  pending (p)       live (l)                expired (E)   maturing (m)
//
//    t0 = authoring/broadcast
//    t1 = activation (confirmed and accepted)
//    t2 = expiry (tier accounting)
//    t3 = lockTime
//    t4 = spendable (block medianTime > lockTime)
//
//    E = t3 - t2, *constant* duration, dex.BondExpiry()
//    p = t1 - t0, *variable*, a random process
//    m = t4 - t3, *variable*, depends on consensus rules and blocks
//        e.g. several blocks after lockTime passes
//
//  - bonds may be spent at t4
//  - bonds must be replaced by t2 i.e. broadcast by some t2'
//  - perfectly aligning t4 for bond A with t2' for bond B is impossible on
//    account of the variable durations
//  - t2-t2' should be greater than a large percent of expected pending
//    durations (t1-t0), see pendingBuffer
//
// Here a replacement bond B had a long pending period, and it became active
// after bond A expired (too late):
//
//             t0  t1                         t2' t2                   t3
//   bond A:   |~~~|--------------------------^---|====================|
//                                                x
//   bond B:                                  |~~~~~~|------------------...
//
// Here the replacement bond was confirmed quickly, but l was too short,
// causing it to expire before bond A became spendable:
//                                                                        > renew as bond C
//   bond A:   |~~~|----------------------^-------|====================|++‖~~~~~|---...
//                                          ✓                             x     x
//   bond B:                              |~|------------------------|=====...
//
// Similarly, l could have been long enough to broadcast a replacement in time,
// but the pending period could be too long (the second "x").
//
// Here the replacement bond was broadcast with enough time to confirm before
// the previous bond expired, and the previous bond became spendable in time to
// broadcast and confirm another replacement (sustainable):
//                                                                        > renew as bond C
//   bond A:   |~~~|----------------------^-------|====================|++‖~~~~~|---...
//                                               ✓                        ✓     ✓
//   bond B:                              |~~~~~~|-------------------------------|====...
//
// Thus, bond rotation without tier drops requires l>E+m+p. For
// t3-t0 = p+l+E, this means t3-t0 >= 2(E+p)+m. We will assume the time
// from authoring to broadcast is negligible, absorbed into the estimate of the
// max pending duration.
//
// tldr:
//  - when creating a bond, set lockTime = now + minBondLifetime, where
//    minBondLifetime = 2*(BondExpiry+pendingBuffer)+spendableDelay
//  - create a replacement bond at lockTime-BondExpiry-pendingBuffer

// pendingBuffer gives the duration in seconds prior to reaching bond expiry
// (account, not lockTime) after which a new bond should be posted to avoid
// account tier falling below target while the replacement bond is pending. The
// network is a parameter because expected block times are network-dependent,
// and we require short bond lifetimes on simnet.
func pendingBuffer(net dex.Network) int64 {
	switch net {
	case dex.Mainnet: // unpredictable, so extra large to prevent falling tier
		return 90 * 60
	case dex.Testnet: // testnet generally has shorter block times, min diff rules, and vacant blocks
		return 20 * 60
	default: // Regtest and Simnet have on-demand blocks
		return 35
	}
}

// spendableDelay gives a high estimate in seconds of the duration required for
// a bond to become spendable after reaching lockTime. This depends on consensus
// rules and block times for an asset. For some assets this could be zero, while
// for others like Bitcoin, a time-locked output becomes spendable when the
// median of the last 11 blocks is greater than the lockTime. This function
// returns a high value to avoid all but extremely rare (but temporary) drops in
// tier. NOTE: to minimize bond overlap, an asset.Bonder method could provide
// this estimate, but it is still very short relative to the entire bond
// lifetime, which is on the order of months.
func spendableDelay(net dex.Network) int64 {
	// We use 3*pendingBuffer as a well-padded estimate of this duration. e.g.
	// with mainnet, we would use a 90 minute pendingBuffer and a 270 minute
	// spendableDelay to be robust to periods of slow blocks.
	return 3 * pendingBuffer(net)
}

// minBondLifetime gives the minimum bond lifetime (a duration from now until
// lockTime) that a new bond should use to prevent a tier drop. bondExpiry is in
// seconds.
func minBondLifetime(net dex.Network, bondExpiry int64) time.Duration {
	lifeTimeSec := 2*(pendingBuffer(net)+bondExpiry) + spendableDelay(net) // 2*(p+E)+m
	return time.Second * time.Duration(lifeTimeSec)
}

// rotateBonds should only be run sequentially i.e. in the watchBonds loop.
func (c *Core) rotateBonds(ctx context.Context) {
	// 1. Refund bonds with passed lockTime.
	// 2. Move bonds that are expired according to DEX bond expiry into
	//    expiredBonds (lockTime<lockTimeThresh).
	// 3. Add bonds to keep N bonds active, according to target tier and max
	//    bonded amount, posting before expiry of the bond being replaced.

	if !c.bondKeysReady() { // not logged in, and nextBondKey requires login to decrypt bond xpriv
		return // nothing to do until wallets are connected on login
	}

	now := time.Now().Unix()

	type bondID struct {
		assetID uint32
		coinID  []byte
	}

	type acctUpdate struct {
		acct         *dexAccount
		tierChange   int64
		reserveDelta int64
	}

	netReserveDeltas := make(map[uint32]int64) // for all DEX conns, per asset, compare to lastReserves
	actuatedTierChanges := make(map[uint32][]acctUpdate)

	for _, dc := range c.dexConnections() {
		initialized, unlocked := dc.acct.status()
		if !initialized {
			continue // view-only or temporary connection
		}
		// Account unlocked is generally implied by bondKeysReady, but we will
		// check per-account before post since accounts can be individually
		// locked. However, we must refund bonds regardless.

		lockTimeThresh := now // in case dex is down, expire (to refund when lock time is passed)
		var bondExpiry int64
		bondAssets := make(map[uint32]*msgjson.BondAsset)
		var haveConnected bool
		if cfg := dc.config(); cfg != nil {
			haveConnected = true
			bondExpiry = int64(cfg.BondExpiry)
			for symb, ba := range cfg.BondAssets {
				id, _ := dex.BipSymbolID(symb)
				bondAssets[id] = ba
			}
			lockTimeThresh += bondExpiry // when dex is up, expire sooner according to bondExpiry
		}
		replaceThresh := lockTimeThresh + pendingBuffer(c.net) // replace before expiry to avoid tier drop

		var weak []*db.Bond // may re-post ahead of expiry to maintain tier, if connected and authenticated
		filterExpiredBonds := func(bonds []*db.Bond) (liveBonds []*db.Bond) {
			for _, bond := range bonds {
				if int64(bond.LockTime) <= lockTimeThresh {
					// Often auth, reconnect, or a bondexpired notification will
					// do this first, but we must also here for refunds when the
					// DEX host is down or gone.
					dc.acct.expiredBonds = append(dc.acct.expiredBonds, bond)
					c.log.Infof("Newly expired bond found: %v (%s)", coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
				} else {
					if int64(bond.LockTime) <= replaceThresh {
						weak = append(weak, bond) // but not yet expired (still live or pending)
					}
					liveBonds = append(liveBonds, bond)
				}
			}
			return liveBonds
		}

		sumBondStrengths := func(bonds []*db.Bond) (total int64) {
			for _, bond := range bonds {
				if ba := bondAssets[bond.AssetID]; ba != nil && ba.Amt > 0 {
					strength := bond.Amount / ba.Amt
					total += int64(strength)
				}
			}
			return
		}

		dc.acct.authMtx.Lock()
		tierChange := dc.acct.tierChange // since last reserves update
		tier, targetTier := dc.acct.tier, dc.acct.targetTier
		bondAssetID, maxBondedAmt := dc.acct.bondAsset, dc.acct.maxBondedAmt
		// Screen the unexpired bonds slices.
		dc.acct.bonds = filterExpiredBonds(dc.acct.bonds)
		dc.acct.pendingBonds = filterExpiredBonds(dc.acct.pendingBonds) // possibly expired before confirmed
		pendingStrength := sumBondStrengths(dc.acct.pendingBonds)
		weakStrength := sumBondStrengths(weak)
		liveStrength := sumBondStrengths(dc.acct.bonds) // for max bonded check
		// Extract the expired bonds.
		expiredBonds := make([]*db.Bond, len(dc.acct.expiredBonds))
		copy(expiredBonds, dc.acct.expiredBonds)
		// Retry postbond for pending bonds that may have failed during
		// submission after their block waiters triggered.
		var repost []*asset.Bond
		for _, bond := range dc.acct.pendingBonds {
			if haveConnected && !c.waiting(bond.CoinID, bond.AssetID) {
				c.log.Warnf("Found a pending bond that is not waiting for confirmations. Re-posting: %s (%s)",
					coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
				repost = append(repost, assetBond(bond))
			}
		}
		dc.acct.authMtx.Unlock()

		for _, bond := range repost { // outside of authMtx lock
			if !unlocked { // can't sign the postbond msg
				c.log.Warnf("Cannot post pending bond for %v until account is unlocked.", dc.acct.host)
				continue
			}
			// Not dependent on authed - this may be the first bond
			// (registering) where bondConfirmed does authDEX if needed.
			if bondAsset, ok := bondAssets[bond.AssetID]; ok {
				c.monitorBondConfs(dc, bond, bondAsset.Confs, true) // rebroadcast
			} else {
				c.log.Errorf("Asset %v no longer supported by %v for bonds! "+
					"Pending bond to refund: %s",
					unbip(bond.AssetID), dc.acct.host,
					coinIDString(bond.AssetID, bond.CoinID))
				// Or maybe the server config will update again? Hard to know
				// how to handle this. This really shouldn't happen though.
			}
		}

		tierDeficit := int64(targetTier) - tier
		mustPost := tierDeficit + weakStrength - pendingStrength

		spentBonds := make([]*bondID, 0, len(expiredBonds))
		transactedAssets := make(map[uint32]struct{}) // wallets that will need balance update
		for _, bond := range expiredBonds {
			bondIDStr := fmt.Sprintf("%v (%s)", coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
			if now < int64(bond.LockTime) {
				c.log.Debugf("Expired bond %v refundable in about %v.",
					bondIDStr, time.Duration(int64(bond.LockTime)-now)*time.Second)
				continue
			}

			assetID := bond.AssetID
			wallet, err := c.connectedWallet(assetID)
			if err != nil {
				c.log.Errorf("%v wallet not available to refund bond %v: %v",
					unbip(bond.AssetID), bondIDStr, err)
				continue
			}
			if _, ok := wallet.Wallet.(asset.Bonder); !ok { // will fail in RefundBond, but assert here anyway
				c.log.Errorf("Wallet %v is not an asset.Bonder", unbip(bond.AssetID))
				return
			}

			expired, err := wallet.LockTimeExpired(ctx, time.Unix(int64(bond.LockTime), 0))
			if err != nil {
				c.log.Errorf("Unable to check if bond %v has expired: %v", bondIDStr, err)
				continue
			}
			if !expired {
				c.log.Debugf("Expired bond %v with lock time %v not yet refundable according to wallet.",
					bondIDStr, time.Unix(int64(bond.LockTime), 0))
				continue
			}

			// Here we may either refund or renew the bond depending on target
			// tier and timing. Direct renewal (refund and post in one) is only
			// useful if there is insufficient reserves or the client had been
			// stopped for a while. Normally, a bond becoming spendable will not
			// coincide with the need to post bond.
			//
			// TODO: if mustPost > 0 { wallet.RenewBond(...) }

			// Generate a refund tx paying to an address from the currently
			// connected wallet, using bond.KeyIndex to create the signed
			// transaction. The RefundTx is really a backup.
			var refundCoinStr string
			var refundVal uint64
			var bondAlreadySpent bool
			if bond.KeyIndex == math.MaxUint32 { // invalid/unknown key index fallback (v0 db.Bond, which was never released), also will skirt reserves :/
				if len(bond.RefundTx) > 0 {
					refundCoinID, err := wallet.SendTransaction(bond.RefundTx)
					if err != nil {
						c.log.Errorf("Failed to broadcast bond refund txn %x: %v", bond.RefundTx, err)
						continue
					}
					refundCoinStr, _ = asset.DecodeCoinID(bond.AssetID, refundCoinID)
				} else { // else "Unknown bond reported by server", see result.ActiveBonds in authDEX
					bondAlreadySpent = true
				}
			} else { // expected case -- TODO: remove the math.MaxUint32 sometime after 0.6 release
				priv, err := c.bondKeyIdx(bond.AssetID, bond.KeyIndex)
				if err != nil {
					c.log.Errorf("Failed to derive bond private key: %v", err)
					continue
				}
				refundCoin, err := wallet.RefundBond(ctx, bond.Version, bond.CoinID, bond.Data, bond.Amount, priv)
				priv.Zero()
				bondAlreadySpent = errors.Is(err, asset.CoinNotFoundError) // or never mined!
				if err != nil {
					if errors.Is(err, asset.ErrIncorrectBondKey) { // imported account and app seed is different
						c.log.Warnf("Private key to spend bond %v is not available. Broadcasting backup refund tx.", bondIDStr)
						refundCoinID, err := wallet.SendTransaction(bond.RefundTx)
						if err != nil {
							c.log.Errorf("Failed to broadcast bond refund txn %x: %v", bond.RefundTx, err)
							continue
						}
						refundCoinStr, _ = asset.DecodeCoinID(bond.AssetID, refundCoinID)
					} else if !bondAlreadySpent {
						c.log.Errorf("Failed to generate bond refund tx: %v", err)
						continue
					}
				} else {
					refundCoinStr, refundVal = refundCoin.String(), refundCoin.Value()
				}
			}
			// RefundBond increases reserves when it spends the bond, adding to
			// the wallet's balance (available or immature).

			// If the user hasn't already manually refunded the bond, broadcast
			// the refund txn. Mark it refunded and stop tracking regardless.
			if bondAlreadySpent {
				c.log.Warnf("Bond output not found, possibly already spent or never mined! "+
					"Marking refunded. Backup refund transaction: %x", bond.RefundTx)
			} else {
				// TODO: subject, detail := c.formatDetails(...)
				details := fmt.Sprintf("Bond %v for %v refunded in %v, reclaiming %v of %v after tx fees",
					bondIDStr, dc.acct.host, refundCoinStr, wallet.amtString(refundVal),
					wallet.amtString(bond.Amount))
				c.notify(newBondRefundNote(TopicBondRefunded, string(TopicBondRefunded),
					details, db.Success))
			}

			err = c.db.BondRefunded(dc.acct.host, assetID, bond.CoinID)
			if err != nil { // next DB load we'll retry, hit bondAlreadySpent, and store here again
				c.log.Errorf("Failed to mark bond as refunded: %v", err)
			}

			spentBonds = append(spentBonds, &bondID{assetID, bond.CoinID})
			transactedAssets[assetID] = struct{}{}
		}

		// Delete the now-spent bonds from the expiredBonds slice.
		dc.acct.authMtx.Lock()
		for _, spentBond := range spentBonds {
			for i, bond := range dc.acct.expiredBonds {
				if bond.AssetID == spentBond.assetID && bytes.Equal(bond.CoinID, spentBond.coinID) {
					dc.acct.expiredBonds = cutBond(dc.acct.expiredBonds, i)
					break // next spentBond
				}
			}
		}
		expiredStrength := sumBondStrengths(dc.acct.expiredBonds)
		dc.acct.authMtx.Unlock()

		for assetID := range transactedAssets {
			c.updateAssetBalance(assetID)
		}

		bondAsset := bondAssets[bondAssetID]
		if bondAsset == nil {
			c.log.Warnf("Bond asset %d not supported by DEX %v", bondAssetID, dc.acct.host)
			continue
		}

		wallet, err := c.connectedWallet(bondAssetID)
		if err != nil {
			c.log.Errorf("%v wallet not available for bonds: %v", unbip(bondAssetID), err)
			continue
		}

		// Compute required reserves adjustments for any unactuated tierChanges.
		if tierChange != 0 {
			reserveDelta := bondOverlap * -tierChange * int64(bondAsset.Amt)
			netReserveDeltas[bondAssetID] += reserveDelta // negative means free reserves
			// After applying the net reserveDelta, we would normally *zero* the
			// tierChange field, but it could change, so we remember it.
			actuatedTierChanges[bondAssetID] = append(actuatedTierChanges[bondAssetID],
				acctUpdate{dc.acct, tierChange, reserveDelta},
			)
		}

		if targetTier > 0 && mustPost > 0 && bondExpiry > 0 {
			c.log.Infof("Gotta post %d bond increments now. Target tier %d, current tier %d (%d weak, %d pending)",
				mustPost, targetTier, tier, weakStrength, pendingStrength)
			if !unlocked || dc.status() != comms.Connected {
				c.log.Warnf("Unable to post the required bond while disconnected or account is locked.")
				continue
			}
			_, err = wallet.refreshUnlock()
			if err != nil {
				c.log.Errorf("failed to unlock bond asset wallet %v: %v", unbip(bondAssetID), err)
				continue
			}
			if !wallet.synchronized() {
				c.log.Warnf("Wallet %v is not yet synchronized with the network. Cannot post new bonds yet.",
					unbip(bondAssetID))
				continue // otherwise we might double spend if the wallet keys were used elsewhere
			}

			// For the max bonded limit, we'll normalize all bonds to the
			// currently selected bond asset.
			toPost := mustPost
			amt := bondAsset.Amt * uint64(mustPost)
			currentlyBondedAmt := uint64(pendingStrength+liveStrength+expiredStrength) * bondAsset.Amt
			for maxBondedAmt > 0 && amt+currentlyBondedAmt > maxBondedAmt && toPost > 0 {
				toPost-- // dumber, but reads easier
				amt = bondAsset.Amt * uint64(toPost)
			}
			if toPost == 0 {
				c.log.Warnf("Unable to post new bond with equivalent of %s currently bonded (limit of %s)",
					wallet.amtString(currentlyBondedAmt), wallet.amtString(maxBondedAmt))
				continue
			}
			if toPost < mustPost {
				c.log.Warnf("Only posting %d bond increments instead of %d because of current bonding limit of %s",
					toPost, mustPost, wallet.amtString(maxBondedAmt))
			}

			bondLifetime := minBondLifetime(c.net, bondExpiry)
			lockTime := time.Now().Add(bondLifetime).Truncate(time.Second)
			if lockDur := time.Until(lockTime); lockDur > lockTimeLimit {
				c.log.Errorf("excessive lock time (%v>%v) - not posting!", lockDur, lockTimeLimit)
			} else {
				c.log.Tracef("Bond lifetime = %v (lockTime = %v)", bondLifetime, lockTime)
				_, err = c.makeAndPostBond(dc, true, wallet, amt, lockTime, bondAsset)
				if err != nil {
					c.log.Errorf("Unable to post bond: %v", err)
				} // else it's now in pendingBonds
			}
		} // mustPost
	} // range c.dexConnections

	for assetID, reserveDelta := range netReserveDeltas {
		if reserveDelta != 0 { // net adjustment across all dexConnections
			wallet, err := c.connectedWallet(assetID)
			if err != nil { // we grabbed it above so shouldn't happen
				c.log.Errorf("%v wallet not available for bond: %v", unbip(assetID), err)
				continue
			}
			c.log.Infof("Updating bond reserves by %s (automatic tier change adjustments)",
				wallet.amtStringSigned(reserveDelta))
			wallet.ReserveBondFunds(reserveDelta, false)
		}
		// Update the unactuated tier change counter.
		for _, s := range actuatedTierChanges[assetID] {
			s.acct.authMtx.Lock()
			s.acct.tierChange -= s.tierChange
			s.acct.totalReserved += s.reserveDelta
			s.acct.authMtx.Unlock()
		}
	}
}

func (c *Core) preValidateBond(dc *dexConnection, bond *asset.Bond) error {
	if len(dc.acct.encKey) == 0 {
		return fmt.Errorf("uninitialized account")
	}

	pkBytes := dc.acct.pubKey()
	if len(pkBytes) == 0 {
		return fmt.Errorf("account keys not decrypted")
	}

	assetID, bondCoin := bond.AssetID, bond.CoinID
	bondCoinStr := coinIDString(assetID, bondCoin)

	// Pre-validate with the raw bytes of the unsigned tx and our account
	// pubkey.
	preBond := &msgjson.PreValidateBond{
		AcctPubKey: pkBytes,
		AssetID:    assetID,
		Version:    bond.Version,
		RawTx:      bond.UnsignedTx,
	}

	preBondRes := new(msgjson.PreValidateBondResult)
	err := dc.signAndRequest(preBond, msgjson.PreValidateBondRoute, preBondRes, DefaultResponseTimeout)
	if err != nil {
		return codedError(registerErr, err)
	}

	// Check the response signature.
	err = dc.acct.checkSig(preBondRes.Serialize(), preBondRes.Sig)
	if err != nil {
		c.log.Warnf("prevalidatebond: DEX signature validation error: %v", err)
	}
	if !bytes.Equal(preBondRes.BondID, bondCoin) {
		return fmt.Errorf("server reported bond coin ID %v, expected %v", bondCoinStr,
			coinIDString(assetID, preBondRes.BondID))
	}

	if preBondRes.Amount != bond.Amount {
		return newError(bondTimeErr, "pre-validated bond amount is not the desired amount: %d != %d",
			preBondRes.Amount, bond.Amount)
	}

	return nil
}

func (c *Core) postBond(dc *dexConnection, bond *asset.Bond) (*msgjson.PostBondResult, error) {
	if len(dc.acct.encKey) == 0 {
		return nil, fmt.Errorf("uninitialized account")
	}

	pkBytes := dc.acct.pubKey()
	if len(pkBytes) == 0 {
		return nil, fmt.Errorf("account keys not decrypted")
	}

	assetID, bondCoin := bond.AssetID, bond.CoinID
	bondCoinStr := coinIDString(assetID, bondCoin)

	// Do a postbond request with the raw bytes of the unsigned tx, the bond
	// script, and our account pubkey.
	postBond := &msgjson.PostBond{
		AcctPubKey: pkBytes,
		AssetID:    assetID,
		Version:    bond.Version,
		CoinID:     bondCoin,
	}

	postBondRes := new(msgjson.PostBondResult)
	err := dc.signAndRequest(postBond, msgjson.PostBondRoute, postBondRes, DefaultResponseTimeout)
	if err != nil {
		return nil, codedError(registerErr, err)
	}

	// Check the response signature.
	err = dc.acct.checkSig(postBondRes.Serialize(), postBondRes.Sig)
	if err != nil {
		c.log.Warnf("postbond: DEX signature validation error: %v", err)
	}
	if !bytes.Equal(postBondRes.BondID, bondCoin) {
		return nil, fmt.Errorf("server reported bond coin ID %v, expected %v", bondCoinStr,
			coinIDString(assetID, postBondRes.BondID))
	}

	return postBondRes, nil
}

// postAndConfirmBond submits a postbond request for the given bond.
func (c *Core) postAndConfirmBond(dc *dexConnection, bond *asset.Bond) {
	assetID, coinID := bond.AssetID, bond.CoinID
	coinIDStr := coinIDString(assetID, coinID)

	// Inform the server, which will attempt to locate the bond and check
	// confirmations. If server sees the required number of confirmations, the
	// bond will be active (and account created if new) and we should confirm
	// the bond (in DB and dc.acct.{bond,pendingBonds}).
	pbr, err := c.postBond(dc, bond) // can be long while server searches
	if err != nil {
		// TODO: subject, detail := c.formatDetails(...)
		details := fmt.Sprintf("postbond request error (will retry): %v (%T)", err, err)
		c.notify(newBondPostNote(TopicBondPostError, string(TopicBondPostError),
			details, db.ErrorLevel, dc.acct.host))
		return
	}

	c.log.Infof("Bond confirmed %v (%s) with expire time of %v", coinIDStr,
		unbip(assetID), time.Unix(int64(pbr.Expiry), 0))
	err = c.bondConfirmed(dc, assetID, coinID, pbr.Tier)
	if err != nil {
		c.log.Errorf("Unable to confirm bond: %v", err)
	}
}

// monitorBondConfs launches a block waiter for the bond txns to reach the
// required amount of confirmations. Once the requirement is met the server is
// notified.
func (c *Core) monitorBondConfs(dc *dexConnection, bond *asset.Bond, reqConfs uint32, rebroadcast ...bool) {
	assetID, coinID := bond.AssetID, bond.CoinID
	coinIDStr := coinIDString(assetID, coinID)
	host := dc.acct.host

	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		c.log.Errorf("No connected wallet for asset %v: %v", unbip(assetID), err)
		return
	}
	lastConfs, err := wallet.RegFeeConfirmations(c.ctx, coinID)
	coinNotFound := errors.Is(err, asset.CoinNotFoundError)
	if err != nil && !coinNotFound {
		c.log.Errorf("Error getting confirmations for %s: %w", coinIDStr, err)
		return
	}

	if lastConfs >= reqConfs { // don't bother waiting for a block
		go c.postAndConfirmBond(dc, bond)
		return
	}

	if coinNotFound || (len(rebroadcast) > 0 && rebroadcast[0]) {
		// Broadcast the bond and start waiting for confs.
		c.log.Infof("Rebroadcasting bond %v (%s), data = %x.\n\n"+
			"BACKUP refund tx paying to current wallet: %x\n\n",
			coinIDStr, unbip(bond.AssetID), bond.Data, bond.RedeemTx)
		c.log.Tracef("Raw bond transaction: %x", bond.SignedTx)
		if _, err = wallet.SendTransaction(bond.SignedTx); err != nil {
			c.log.Warnf("Failed to broadcast bond txn (%v): Tx bytes %x", err, bond.SignedTx)
			// TODO: screen inputs if the tx is trying to spend spent outputs
			// (invalid bond transaction that should be abandoned).
		}
		c.updateAssetBalance(bond.AssetID)
	}

	c.updatePendingBondConfs(dc, bond.AssetID, bond.CoinID, lastConfs)

	trigger := func() (bool, error) {
		// Retrieve the current wallet in case it was reconfigured.
		wallet, _ := c.wallet(assetID) // We already know the wallet is there by now.
		confs, err := wallet.RegFeeConfirmations(c.ctx, coinID)
		if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
			return false, fmt.Errorf("Error getting confirmations for %s: %w", coinIDStr, err)
		}

		if confs != lastConfs {
			c.updateAssetBalance(assetID)
			lastConfs = confs
			c.updatePendingBondConfs(dc, bond.AssetID, bond.CoinID, confs)
		}

		if confs < reqConfs {
			details := fmt.Sprintf("Bond confirmations %v/%v", confs, reqConfs)
			c.notify(newBondPostNoteWithConfirmations(TopicRegUpdate, string(TopicRegUpdate),
				details, db.Data, assetID, coinIDStr, int32(confs), host))
		}

		return confs >= reqConfs, nil
	}

	c.wait(coinID, assetID, trigger, func(err error) {
		if err != nil { // TODO: subject, detail := c.formatDetails(...)
			details := fmt.Sprintf("Error encountered while waiting for bond confirms for %s: %v", host, err)
			c.notify(newBondPostNote(TopicBondPostError, string(TopicBondPostError),
				details, db.ErrorLevel, host))
			return
		}

		c.log.Infof("DEX %v bond txn %s now has %d confirmations. Submitting postbond request...",
			host, coinIDStr, reqConfs)

		c.postAndConfirmBond(dc, bond) // if it fails (e.g. timeout), retry in rotateBonds
	})
}

func deriveBondKey(bondXPriv *hdkeychain.ExtendedKey, assetID, bondIndex uint32) (*secp256k1.PrivateKey, error) {
	kids := []uint32{
		assetID + hdkeychain.HardenedKeyStart,
		bondIndex,
	}
	extKey, err := keygen.GenDeepChildFromXPriv(bondXPriv, kids)
	if err != nil {
		return nil, fmt.Errorf("GenDeepChild error: %w", err)
	}
	privB, err := extKey.SerializedPrivKey()
	if err != nil {
		return nil, fmt.Errorf("SerializedPrivKey error: %w", err)
	}
	priv := secp256k1.PrivKeyFromBytes(privB)
	return priv, nil
}

func deriveBondXPriv(seed []byte) (*hdkeychain.ExtendedKey, error) {
	return keygen.GenDeepChild(seed, []uint32{hdKeyPurposeBonds})
}

func (c *Core) bondKeyIdx(assetID, idx uint32) (*secp256k1.PrivateKey, error) {
	c.loginMtx.Lock()
	defer c.loginMtx.Unlock()

	if c.bondXPriv == nil {
		return nil, errors.New("not logged in")
	}

	return deriveBondKey(c.bondXPriv, assetID, idx)
}

// nextBondKey generates the private key for the next bond, incrementing a
// persistent bond index counter. This method requires login to decrypt and set
// the bond xpriv, so use the bondKeysReady method to ensure it is ready first.
// The bond key index is returned so the same key may be regenerated.
func (c *Core) nextBondKey(assetID uint32) (*secp256k1.PrivateKey, uint32, error) {
	nextBondKeyIndex, err := c.db.NextBondKeyIndex(assetID)
	if err != nil {
		return nil, 0, fmt.Errorf("NextBondIndex: %v", err)
	}

	priv, err := c.bondKeyIdx(assetID, nextBondKeyIndex)
	if err != nil {
		return nil, 0, fmt.Errorf("bondKeyIdx: %v", err)
	}
	return priv, nextBondKeyIndex, nil
}

// UpdateBondOptions sets the bond rotation options for a DEX host, including
// the target trading tier, the preferred asset to use for bonds, and the
// maximum amount allowable to be locked in bonds.
func (c *Core) UpdateBondOptions(form *BondOptionsForm) error {
	dc, _, err := c.dex(form.Addr)
	if err != nil {
		return err
	}
	// TODO: exclude unregistered and/or watch-only
	acct, err := c.db.Account(form.Addr)
	if err != nil {
		return err
	}

	bondAssets, _ := dc.bondAssets()
	if bondAssets == nil {
		c.log.Warnf("DEX host %v is offline. Bond reconfiguration options are limited to disabling.",
			dc.acct.host)
	}

	// For certain changes, update one or more wallet balances when done.
	var tierChanged, assetChanged bool
	var wallet *xcWallet    // new wallet
	var bondAssetID0 uint32 // old wallet's asset ID
	var targetTier0, maxBondedAmt0 uint64
	defer func() {
		if (tierChanged || assetChanged) && (wallet != nil) {
			if _, err := c.updateWalletBalance(wallet); err != nil {
				c.log.Errorf("Unable to set balance for wallet %v", wallet.Symbol)
			}
			if wallet.AssetID != bondAssetID0 && targetTier0 > 0 {
				c.updateAssetBalance(bondAssetID0)
			}
		}
	}()

	dc.acct.authMtx.Lock()
	defer dc.acct.authMtx.Unlock()

	if !dc.acct.isAuthed {
		return errors.New("login or register first")
	}

	// Revert to initial values if we encounter any error below.
	bondAssetID0 = dc.acct.bondAsset
	targetTier0, maxBondedAmt0 = dc.acct.targetTier, dc.acct.maxBondedAmt
	totalReserved0 := dc.acct.totalReserved
	var success bool
	defer func() { // still under authMtx lock on defer stack
		if !success {
			dc.acct.bondAsset = bondAssetID0
			dc.acct.maxBondedAmt = maxBondedAmt0
			if dc.acct.targetTier > 0 || assetChanged {
				dc.acct.targetTier = targetTier0
				dc.acct.totalReserved = totalReserved0
			} // else the user was trying to clear target tier and the wallet was gone too
		}
	}()

	// Verify the new bond asset wallet first.
	bondAssetID := bondAssetID0
	if form.BondAsset != nil {
		bondAssetID = *form.BondAsset
	}
	assetChanged = bondAssetID != bondAssetID0

	targetTier := targetTier0
	if form.TargetTier != nil {
		targetTier = *form.TargetTier
	}
	tierChanged = targetTier != targetTier0
	if tierChanged {
		dc.acct.targetTier = targetTier
		acct.TargetTier = targetTier
	}

	var bondAssetAmt uint64 // because to disable we must proceed even with no config
	if bondAsset := bondAssets[bondAssetID]; bondAsset == nil {
		if targetTier > 0 || assetChanged {
			return fmt.Errorf("dex %v is does not support %v as a bond asset (or we lack their config)",
				acct.Host, unbip(bondAssetID))
		} // else disable, attempting to unreserve funds if wallet is available
	} else {
		bondAssetAmt = bondAsset.Amt
	}

	var found bool
	wallet, found = c.wallet(bondAssetID)
	if !found || !wallet.connected() {
		return fmt.Errorf("bond asset wallet %v does not exist or is not connected", unbip(bondAssetID))
	}
	if _, ok := wallet.Wallet.(asset.Bonder); !ok {
		return fmt.Errorf("wallet %v is not an asset.Bonder", unbip(bondAssetID))
	}

	_, err = wallet.refreshUnlock()
	if err != nil {
		return fmt.Errorf("bond asset wallet %v is locked", unbip(bondAssetID))
	}

	bondedTier := func() uint64 {
		var tiers uint64
		for _, bond := range dc.acct.bonds { // only the active ones, all assets
			if ba := bondAssets[bond.AssetID]; ba != nil {
				tiers += bond.Amount / ba.Amt
			}
		}
		return tiers
	}

	if assetChanged {
		// NOTE: This assetChanged branch has not been used in practice yet
		// since at this moment only DCR is a Bonder.
		dc.acct.bondAsset = bondAssetID
		acct.BondAsset = bondAssetID

		// First the new asset wallet.
		if targetTier > 0 { // enabling maintenance with the new asset
			inBonds, live := dc.bondTotalInternal(bondAssetID)
			mod := int64(bondOverlap * targetTier * bondAssetAmt)
			// If we appear to be penalized, add increments to mod.
			if tierOffset := int64(bondedTier()) - dc.acct.tier; tierOffset > 0 {
				mod += tierOffset * int64(bondAssetAmt)
			}
			dc.acct.totalReserved = mod
			mod -= -int64(inBonds)

			modStr := wallet.amtStringSigned(mod)
			c.log.Infof("Attempting to set bond reserves to %s "+
				"(%v currently locked in bonds with %v in-force) "+
				"for bond asset change from %d to %d, with bond increment %v",
				modStr, wallet.amtString(inBonds), wallet.amtString(live),
				bondAssetID0, bondAssetID0, wallet.amtString(bondAssetAmt))
			if !wallet.ReserveBondFunds(mod, true) {
				return fmt.Errorf("insufficient balance to reserve %v", modStr)
			}
		} else { // maintenance stays disabled on the new asset
			dc.acct.totalReserved = 0
		}

		if targetTier0 > 0 { // old asset had bond maintenance, remove reserves
			wallet0, found := c.wallet(bondAssetID0)
			if !found || !wallet0.connected() {
				c.log.Warnf("old bond asset wallet not found, cannot release any reserves")
			} else if _, ok := wallet0.Wallet.(asset.Bonder); !ok {
				c.log.Warnf("old bond wallet %v is not an asset.Bonder", unbip(bondAssetID0))
			} else {
				wallet0.ReserveBondFunds(-totalReserved0, false)
			}
		}
	} else if tierChanged { // && asset not changed
		oldTarget, newTarget := targetTier0, targetTier // for readability
		tierDelta := int64(newTarget) - int64(oldTarget)

		inBonds, live := dc.bondTotalInternal(bondAssetID)

		var mod int64
		if newTarget == 0 { // disable, not just a delta
			mod = -dc.acct.totalReserved
			dc.acct.totalReserved = 0 // += mod
		} else {
			mod = bondOverlap * tierDelta * int64(bondAssetAmt)
			if oldTarget == 0 { // enabling maintenance, not just a delta
				// If we appear to be penalized, add increments to mod.
				if tierOffset := int64(bondedTier()) - dc.acct.tier; tierOffset > 0 {
					mod += tierOffset * int64(bondAssetAmt)
				}
				// Wallet already has live unspent bonds (inBonds) registered.
				mod -= int64(inBonds)                  // negative OK, combined is still positive
				dc.acct.totalReserved = int64(inBonds) // enabling starts with inBonds as base nominal reserves
			}
			dc.acct.totalReserved += mod
		}

		modStr := wallet.amtStringSigned(mod)
		c.log.Infof("Attempting to update bond reserves by %s "+
			"(%v currently locked in bonds with %v in-force) "+
			"for target tier change from %v to %v, with bond increment %v",
			modStr, wallet.amtString(inBonds), wallet.amtString(live),
			oldTarget, newTarget, wallet.amtString(bondAssetAmt))
		if !wallet.ReserveBondFunds(mod, true) { // will always allow negative mod, but may warn about balance
			return fmt.Errorf("insufficient balance to reserve an extra %v", modStr)
		}
		dc.log.Infof("Total reserved for %v is now %v (%v more in future bonds)", dc.acct.host,
			wallet.amtStringSigned(dc.acct.totalReserved), wallet.amtStringSigned(mod))
	}

	maxBonded := maxBondedMult * bondAssetAmt * targetTier // the min if none specified
	if form.MaxBondedAmt != nil && *form.MaxBondedAmt > maxBonded {
		maxBonded = *form.MaxBondedAmt
	}
	if assetChanged || tierChanged || form.MaxBondedAmt != nil || maxBonded < dc.acct.maxBondedAmt {
		dc.acct.maxBondedAmt = maxBonded
		acct.MaxBondedAmt = maxBonded
	}

	c.log.Debugf("Bond options for %v: target tier %d, bond asset %d, maxBonded %v",
		acct.Host, dc.acct.targetTier, dc.acct.bondAsset, acct.MaxBondedAmt)

	if err = c.db.UpdateAccountInfo(acct); err == nil {
		success = true
	} // else we might have already done ReserveBondFunds...
	return err
}

// BondsFeeBuffer suggests how much extra may be required for the transaction
// fees part of bond reserves when bond rotation is enabled. This may be used to
// inform the consumer how much extra (beyond double the bond amount) is
// required to facilitate uninterrupted maintenance of a target trading tier.
func (c *Core) BondsFeeBuffer(assetID uint32) (uint64, error) {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return 0, err
	}
	bonder, ok := wallet.Wallet.(asset.Bonder)
	if !ok {
		return 0, errors.New("wallet does not support bonds")
	}
	return bonder.BondsFeeBuffer(), nil
}

// PostBond begins the process of posting a new bond for a new or existing DEX
// account. On return, the bond transaction will have been broadcast, and when
// the required number of confirmations is reached, Core will submit the bond
// for acceptance to the server. A TopicBondConfirmed is emitted when the
// fully-confirmed bond is accepted. Before the transaction is broadcasted, a
// prevalidatebond request is sent to ensure the transaction is compliant and
// (and that the intended server is actually online!). PostBond may be used to
// create a new account with a bond, or to top-up bond on an existing account.
// If the account is not yet configured in Core, account discovery will be
// performed prior to posting a new bond. If account discovery finds an existing
// account, the connection is established but no additional bond is posted. If
// no account is discovered on the server, the account is created locally and
// bond is posted to create the account.
func (c *Core) PostBond(form *PostBondForm) (*PostBondResult, error) {
	// Make sure the app has been initialized.
	if !c.IsInitialized() {
		return nil, fmt.Errorf("app not initialized")
	}

	// Check that the bond amount is non-zero before we touch wallets and make
	// connections to the DEX host.
	if form.Bond == 0 {
		return nil, newError(bondAmtErr, "zero registration fees not allowed")
	}

	// Get the wallet to author the transaction. Default to DCR.
	bondAssetID := uint32(42)
	if form.Asset != nil {
		bondAssetID = *form.Asset
	}
	bondAssetSymbol := dex.BipIDSymbol(bondAssetID)
	wallet, err := c.connectedWallet(bondAssetID)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s wallet to pay fee: %w", bondAssetSymbol, err)
	}
	if _, ok := wallet.Wallet.(asset.Bonder); !ok { // will fail in MakeBondTx, but assert early
		return nil, fmt.Errorf("wallet %v is not an asset.Bonder", bondAssetSymbol)
	}
	if !wallet.synchronized() { // otherwise we might double spend if the wallet keys were used elsewhere
		return nil, fmt.Errorf("wallet %v is not synchronized", unbip(bondAssetID))
	}

	// Check the app password.
	crypter, err := c.encryptionKey(form.AppPass)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	defer crypter.Close()
	if form.Addr == "" {
		return nil, newError(emptyHostErr, "no dex address specified")
	}
	host, err := addrHost(form.Addr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}

	// Get ready to generate the bond txn.
	if !wallet.unlocked() {
		err = wallet.Unlock(crypter)
		if err != nil {
			return nil, newError(walletAuthErr, "failed to unlock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}

	var success, acctExists bool

	// When creating an account or registering a view-only account, the default
	// is to maintain tier.
	maintain := true
	if form.MaintainTier != nil {
		maintain = *form.MaintainTier
	}

	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if found {
		acctExists = !dc.acct.isViewOnly()
		if acctExists {
			if dc.acct.locked() { // require authDEX first to reconcile any existing bond statuses
				return nil, newError(acctKeyErr, "acct locked %s (login first)", form.Addr)
			}
			if form.MaintainTier != nil || form.MaxBondedAmt != nil {
				return nil, fmt.Errorf("maintain tier and max bonded amount may only be set when registering " +
					"(use UpdateBondOptions to change bond maintenance settings)")
			}
		}
	} else {
		// Before connecting to the DEX host, do a quick balance check to ensure
		// we at least have the nominal bond amount available.
		if bal, err := wallet.Balance(); err != nil {
			return nil, newError(bondAssetErr, "unable to check wallet balance: %w", err)
		} else if bal.Available < form.Bond {
			return nil, newError(bondAssetErr, "insufficient available balance")
		}

		// New DEX connection.
		cert, err := parseCert(host, form.Cert, c.net)
		if err != nil {
			return nil, newError(fileReadErr, "failed to read certificate file from %s: %v", cert, err)
		}
		dc, err = c.connectDEX(&db.AccountInfo{
			Host: host,
			Cert: cert,
			// bond maintenance options set below.
		})
		if err != nil {
			if dc != nil {
				// Stop (re)connect loop, which may be running even if err != nil.
				dc.connMaster.Disconnect()
			}
			return nil, codedError(connectionErr, err)
		}

		// Close the connection to the dex server if the registration fails.
		defer func() {
			if !success {
				dc.connMaster.Disconnect()
			}
		}()
	}

	if !acctExists { // new dex connection or pre-existing view-only connection
		paid, err := c.discoverAccount(dc, crypter)
		if err != nil {
			return nil, err
		}
		// dc.acct is now configured with encKey, privKey, and id for a new
		// (unregistered) account.

		if paid {
			success = true
			// The listen goroutine is already running, now track the conn.
			c.addDexConnection(dc)
			return &PostBondResult{ /* no new bond */ }, nil
		}
	}

	// Ensure this DEX supports this asset for bond, and get the required
	// confirmations and bond amount.
	bondAsset, bondExpiry := dc.bondAsset(bondAssetID)
	if bondAsset == nil {
		return nil, newError(assetSupportErr, "dex host has not connected or does not support fidelity bonds in asset %q", bondAssetSymbol)
	}

	lockDur := minBondLifetime(c.net, int64(bondExpiry))
	lockTime := time.Now().Add(lockDur).Truncate(time.Second)
	if form.LockTime > 0 {
		lockTime = time.Unix(int64(form.LockTime), 0)
	}
	expireTime := lockTime.Add(time.Second * time.Duration(-bondExpiry)) // when the server would expire the bond
	if time.Until(expireTime) < time.Minute {
		return nil, newError(bondTimeErr, "bond would expire in less than one minute")
	}
	if lockDur := time.Until(lockTime); lockDur > lockTimeLimit {
		return nil, newError(bondTimeErr, "excessive lock time (%v>%v)", lockDur, lockTimeLimit)
	} else if lockDur <= 0 { // should be redundant, but be sure
		return nil, newError(bondTimeErr, "lock time of %d in the past", form.LockTime)
	}

	// Check that the bond amount matches the caller's expectations.
	if form.Bond < bondAsset.Amt {
		return nil, newError(bondAmtErr, "specified bond amount is less than the DEX-provided amount. %d < %d",
			form.Bond, bondAsset.Amt)
	}
	if rem := form.Bond % bondAsset.Amt; rem != 0 {
		return nil, newError(bondAmtErr, "specified bond amount is not a multiple of the DEX-provided amount. %d %% %d = %d",
			form.Bond, bondAsset.Amt, rem)
	}
	if acctExists { // if account exists, advise using UpdateBondOptions
		autoBondAsset, targetTier, maxBondedAmt := dc.bondOpts()
		c.log.Warnf("Manually posting bond for existing account "+
			"(target tier %d, bond asset %d, maxBonded %v). "+
			"Consider using UpdateBondOptions instead.",
			targetTier, autoBondAsset, wallet.amtString(maxBondedAmt))
	} else if maintain { // new account (or registering a view-only acct) with tier maintenance enabled
		// Fully pre-reserve funding with the wallet before making and
		// transactions. bondConfirmed will call authDEX, which will recognize
		// that it is the first authorization of the account with the DEX via
		// the totalReserves and isAuthed fields of dexAccount.
		mod := bondOverlap * form.Bond
		if !wallet.ReserveBondFunds(int64(mod), true) {
			return nil, newError(bondAssetErr, "insufficient available balance to reserve %v for bonds plus fees",
				wallet.amtString(mod))
		}
		maxBondedAmt := maxBondedMult * form.Bond // default
		if form.MaxBondedAmt != nil {
			maxBondedAmt = *form.MaxBondedAmt
		}
		dc.acct.authMtx.Lock()
		dc.acct.totalReserved = int64(mod) // should have been zero already (new account == no existing bonds)
		dc.acct.bondAsset = bondAssetID
		dc.acct.targetTier = form.Bond / bondAsset.Amt
		dc.acct.maxBondedAmt = maxBondedAmt
		dc.acct.authMtx.Unlock()
	}

	// Make a bond transaction for the account ID generated from our public key.
	bondCoin, err := c.makeAndPostBond(dc, acctExists, wallet, form.Bond, lockTime, bondAsset)
	if err != nil {
		return nil, err
	}
	success = true
	bondCoinStr := coinIDString(bondAssetID, bondCoin)
	return &PostBondResult{BondID: bondCoinStr, ReqConfirms: uint16(bondAsset.Confs)}, nil
}

func (c *Core) makeAndPostBond(dc *dexConnection, acctExists bool, wallet *xcWallet, amt uint64,
	lockTime time.Time, bondAsset *msgjson.BondAsset) ([]byte, error) {
	bondKey, keyIndex, err := c.nextBondKey(bondAsset.ID)
	if err != nil {
		return nil, fmt.Errorf("bond key derivation failed: %v", err)
	}
	defer bondKey.Zero()

	acctID := dc.acct.ID()
	feeRate := c.feeSuggestionAny(bondAsset.ID)
	bond, abandon, err := wallet.MakeBondTx(bondAsset.Version, amt, feeRate, lockTime, bondKey, acctID[:])
	if err != nil {
		return nil, codedError(bondPostErr, err)
	}
	// MakeBondTx lock coins and reduces reserves in proportion

	var success bool
	defer func() {
		if !success {
			abandon() // unlock coins and increase reserves
		}
	}()

	// Do prevalidatebond with the *unsigned* txn.
	if err = c.preValidateBond(dc, bond); err != nil {
		return nil, err
	}

	reqConfs := bondAsset.Confs
	bondCoinStr := coinIDString(bond.AssetID, bond.CoinID)
	c.log.Infof("DEX %v has validated our bond %v (%s) with strength %d. %d confirmations required to trade.",
		dc.acct.host, bondCoinStr, unbip(bond.AssetID), amt/bondAsset.Amt, reqConfs)

	// Store the account and bond info.
	dbBond := &db.Bond{
		Version:    bond.Version,
		AssetID:    bond.AssetID,
		CoinID:     bond.CoinID,
		UnsignedTx: bond.UnsignedTx,
		SignedTx:   bond.SignedTx,
		Data:       bond.Data,
		Amount:     amt,
		LockTime:   uint64(lockTime.Unix()),
		KeyIndex:   keyIndex,
		RefundTx:   bond.RedeemTx,
		// Confirmed and Refunded are false (new bond tx)
	}

	if acctExists {
		err = c.db.AddBond(dc.acct.host, dbBond)
		if err != nil {
			return nil, fmt.Errorf("failed to store bond %v (%s) for dex %v: %w",
				bondCoinStr, unbip(bond.AssetID), dc.acct.host, err)
		}
	} else {
		bondAsset, targetTier, maxBondedAmt := dc.bondOpts()
		ai := &db.AccountInfo{
			Host:         dc.acct.host,
			Cert:         dc.acct.cert,
			DEXPubKey:    dc.acct.dexPubKey,
			EncKeyV2:     dc.acct.encKey,
			Bonds:        []*db.Bond{dbBond},
			TargetTier:   targetTier,
			MaxBondedAmt: maxBondedAmt,
			BondAsset:    bondAsset,
		}
		err = c.dbCreateOrUpdateAccount(dc, ai)
		if err != nil {
			return nil, fmt.Errorf("failed to store account %v for dex %v: %w",
				dc.acct.id, dc.acct.host, err)
		}
	}

	success = true // we're doing this

	dc.acct.authMtx.Lock()
	dc.acct.pendingBonds = append(dc.acct.pendingBonds, dbBond)
	dc.acct.authMtx.Unlock()

	if !acctExists { // *after* setting pendingBonds for rotateBonds accounting if targetTier>0
		c.addDexConnection(dc)
		// NOTE: it's still not authed if this was the first bond
	}

	// Broadcast the bond and start waiting for confs.
	c.log.Infof("Broadcasting bond %v (%s) with lock time %v, data = %x.\n\n"+
		"BACKUP refund tx paying to current wallet: %x\n\n",
		bondCoinStr, unbip(bond.AssetID), lockTime, bond.Data, bond.RedeemTx)
	if bondCoinCast, err := wallet.SendTransaction(bond.SignedTx); err != nil {
		c.log.Warnf("Failed to broadcast bond txn (%v). Tx bytes: %x", err, bond.SignedTx)
		// There is a good possibility it actually made it to the network. We
		// should start monitoring, perhaps even rebroadcast. It's tempting to
		// abort and remove the pending bond, but that's bad if it's sent.
	} else if !bytes.Equal(bond.CoinID, bondCoinCast) {
		c.log.Warnf("Broadcasted bond %v; was expecting %v!",
			coinIDString(bond.AssetID, bondCoinCast), bondCoinStr)
	}

	c.updateAssetBalance(bond.AssetID)

	// Start waiting for reqConfs.
	details := fmt.Sprintf("Waiting for %d confirmations to post bond %v (%s) to %s",
		reqConfs, bondCoinStr, unbip(bond.AssetID), dc.acct.host) // TODO: subject, detail := c.formatDetails(...)
	c.notify(newBondPostNoteWithConfirmations(TopicBondConfirming, string(TopicBondConfirming),
		details, db.Success, bond.AssetID, bondCoinStr, 0, dc.acct.host))
	// Set up the coin waiter, which watches confirmations so the user knows
	// when to expect their account to be marked paid by the server.
	c.monitorBondConfs(dc, bond, reqConfs)

	return bond.CoinID, nil
}

func (c *Core) updatePendingBondConfs(dc *dexConnection, assetID uint32, coinID []byte, confs uint32) {
	dc.acct.authMtx.Lock()
	defer dc.acct.authMtx.Unlock()
	bondIDStr := coinIDString(assetID, coinID)
	dc.acct.pendingBondsConfs[bondIDStr] = confs
}

func (c *Core) bondConfirmed(dc *dexConnection, assetID uint32, coinID []byte, newTier int64) error {
	bondIDStr := coinIDString(assetID, coinID)

	// Update dc.acct.{bonds,pendingBonds,tier} under authMtx lock.
	var foundPending, foundConfirmed bool
	dc.acct.authMtx.Lock()
	delete(dc.acct.pendingBondsConfs, bondIDStr)
	for i, bond := range dc.acct.pendingBonds {
		if bond.AssetID == assetID && bytes.Equal(bond.CoinID, coinID) {
			// Delete the bond from pendingBonds and move it to (active) bonds.
			dc.acct.pendingBonds = cutBond(dc.acct.pendingBonds, i)
			dc.acct.bonds = append(dc.acct.bonds, bond)
			bond.Confirmed = true // not necessary, just for consistency with slice membership
			foundPending = true
			break
		}
	}
	if !foundPending {
		for _, bond := range dc.acct.bonds {
			if bond.AssetID == assetID && bytes.Equal(bond.CoinID, coinID) {
				foundConfirmed = true
				break
			}
		}
	}

	dc.acct.tier = newTier
	targetTier := dc.acct.targetTier
	isAuthed := dc.acct.isAuthed
	dc.acct.authMtx.Unlock()

	if foundPending {
		// Set bond confirmed in the DB.
		err := c.db.ConfirmBond(dc.acct.host, assetID, coinID)
		if err != nil {
			return fmt.Errorf("db.ConfirmBond failure: %w", err)
		}
		c.log.Infof("Bond %s (%s) confirmed.", bondIDStr, unbip(assetID))
		details := fmt.Sprintf("New tier = %d (target = %d).", newTier, targetTier) // TODO: format to subject,details
		c.notify(newBondPostNoteWithTier(TopicBondConfirmed, string(TopicBondConfirmed), details, db.Success, dc.acct.host, newTier))
	} else if !foundConfirmed {
		c.log.Errorf("bondConfirmed: Bond %s (%s) not found", bondIDStr, unbip(assetID))
		// just try to authenticate...
	} // else already found confirmed (no-op)

	// If we were not previously authenticated, we can infer that this was the
	// bond that created the account server-side, otherwise this was a top-up.
	if isAuthed {
		return nil // already logged in
	}

	if dc.acct.locked() {
		c.log.Info("Login to check current account tier with newly confirmed bond %v.", bondIDStr)
		return nil
	}

	err := c.authDEX(dc)
	if err != nil {
		details := fmt.Sprintf("Bond confirmed, but failed to authenticate connection: %v", err) // TODO: format to subject,details
		c.notify(newDEXAuthNote(TopicDexAuthError, string(TopicDexAuthError), dc.acct.host, false, details, db.ErrorLevel))
		return err
	}

	details := fmt.Sprintf("New tier = %d", newTier) // TODO: format to subject,details
	c.notify(newBondPostNoteWithTier(TopicAccountRegistered, string(TopicAccountRegistered),
		details, db.Success, dc.acct.host, newTier)) // possibly redundant with SubjectBondConfirmed

	return nil
}

func (c *Core) bondExpired(dc *dexConnection, assetID uint32, coinID []byte, newTier int64) error {
	// Update dc.acct.{bonds,tier} under authMtx lock.
	var found bool
	dc.acct.authMtx.Lock()
	for i, bond := range dc.acct.bonds {
		if bond.AssetID == assetID && bytes.Equal(bond.CoinID, coinID) {
			// Delete the bond from bonds and move it to expiredBonds.
			dc.acct.bonds = cutBond(dc.acct.bonds, i)
			if len(bond.RefundTx) > 0 || bond.KeyIndex != math.MaxUint32 {
				dc.acct.expiredBonds = append(dc.acct.expiredBonds, bond) // we'll wait for lockTime to pass to refund
			} else {
				c.log.Warnf("Dropping expired bond with no known keys or refund transaction. "+
					"This was a placeholder for an unknown bond reported to use by the server. "+
					"Bond ID: %x (%s)", coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
			}
			found = true
			break
		}
	}
	if !found { // rotateBonds may have gotten to it first
		for _, bond := range dc.acct.expiredBonds {
			if bond.AssetID == assetID && bytes.Equal(bond.CoinID, coinID) {
				found = true
				break
			}
		}
	}

	dc.acct.tier = newTier
	targetTier := dc.acct.targetTier
	dc.acct.authMtx.Unlock()

	bondIDStr := coinIDString(assetID, coinID)
	if !found {
		c.log.Warnf("bondExpired: Bond %s (%s) in bondexpired message not found locally (already refunded?).",
			bondIDStr, unbip(assetID))
	}

	if targetTier > uint64(newTier) {
		details := fmt.Sprintf("New tier = %d (target = %d).", newTier, targetTier)
		c.notify(newBondPostNoteWithTier(TopicBondExpired, string(TopicBondExpired),
			details, db.WarningLevel, dc.acct.host, newTier))
	}

	return nil
}
