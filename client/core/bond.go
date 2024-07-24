package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

const (
	// lockTimeLimit is an upper limit on the allowable bond lockTime.
	lockTimeLimit = 120 * 24 * time.Hour

	defaultBondAsset = 42 // DCR

	maxBondedMult    = 4
	bondTickInterval = 20 * time.Second
)

func cutBond(bonds []*db.Bond, i int) []*db.Bond { // input slice modified
	bonds[i] = bonds[len(bonds)-1]
	bonds[len(bonds)-1] = nil
	bonds = bonds[:len(bonds)-1]
	return bonds
}

func (c *Core) triggerBondRotation() {
	select {
	case c.rotate <- struct{}{}:
	default:
	}
}

func (c *Core) watchBonds(ctx context.Context) {
	t := time.NewTicker(bondTickInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.rotateBonds(ctx)
		case <-c.rotate:
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

// sumBondStrengths calculates the total strength of a list of bonds.
func sumBondStrengths(bonds []*db.Bond, bondAssets map[uint32]*msgjson.BondAsset) (total int64) {
	for _, bond := range bonds {
		if bond.Strength > 0 { // Added with v2 reputation
			total += int64(bond.Strength)
			continue
		}
		// v1. Gotta hope the server didn't change the bond amount.
		if ba := bondAssets[bond.AssetID]; ba != nil && ba.Amt > 0 {
			strength := bond.Amount / ba.Amt
			total += int64(strength)
		}
	}
	return
}

type dexBondCfg struct {
	bondAssets     map[uint32]*msgjson.BondAsset
	replaceThresh  int64
	bondExpiry     int64
	lockTimeThresh int64
	haveConnected  bool
}

// updateBondReserves iterates existing accounts and calculates the amount that
// should be reserved for bonds for each asset. The wallets reserves are
// updated regardless of whether the wallet balance can support it. If
// exactly 1 balanceCheckID is provided, the balance will be checked for that
// asset, and any insufficiencies will be logged.
func (c *Core) updateBondReserves(balanceCheckID ...uint32) {
	reserves := make(map[uint32][]uint64)
	processDC := func(dc *dexConnection) {
		bondAssets, _ := dc.bondAssets()
		if bondAssets == nil { // reconnect loop may be running
			return
		}

		dc.acct.authMtx.RLock()
		defer dc.acct.authMtx.RUnlock()
		if dc.acct.targetTier == 0 {
			return
		}

		bondAsset := bondAssets[dc.acct.bondAsset]
		if bondAsset == nil {
			// Logged at login auth.
			return
		}
		future := c.minBondReserves(dc, bondAsset)
		reserves[bondAsset.ID] = append(reserves[bondAsset.ID], future)
	}

	for _, dc := range c.dexConnections() {
		processDC(dc)
	}

	for _, w := range c.xcWallets() {
		bonder, is := w.Wallet.(asset.Bonder)
		if !is {
			continue
		}
		bondValues, found := reserves[w.AssetID]
		if !found {
			// Not selected as a bond asset for any exchanges.
			bonder.SetBondReserves(0)
			return
		}
		var nominalReserves uint64
		for _, v := range bondValues {
			nominalReserves += v
		}
		// One bondFeeBuffer for each exchange using this asset as their bond
		// asset.
		n := uint64(len(bondValues))
		feeReserves := n * bonder.BondsFeeBuffer(c.feeSuggestionAny(w.AssetID))
		// Even if reserves are 0, we may still want to reserve fees for
		// renewing bonds.
		paddedReserves := nominalReserves + feeReserves
		if len(balanceCheckID) == 1 && w.AssetID == balanceCheckID[0] && w.connected() {
			// There are a few paths that request balance checks, and the
			// cached balance is expected to be up-to-date in them all, with the
			// only possible exception of ReconfigureWallet -> reReserveFunding,
			// where an error updating the balance in ReconfigureWallet is only
			// logged as a warning, for some reason. Either way, worst that
			// happens in that scenario is log an outdated message.
			w.mtx.RLock()
			bal := w.balance
			w.mtx.RUnlock()
			avail := bal.Available + bal.BondReserves
			if avail < paddedReserves {
				c.log.Warnf("Bond reserves of %d %s exceeds available balance of %d",
					paddedReserves, unbip(w.AssetID), avail)
			}
		}
		bonder.SetBondReserves(paddedReserves)
	}
}

// minBondReserves calculates the minimum number of tiers that we need to
// reserve funds for. minBondReserveTiers must be called with the authMtx
// RLocked.
func (c *Core) minBondReserves(dc *dexConnection, bondAsset *BondAsset) uint64 {
	acct, targetTier := dc.acct, dc.acct.targetTier
	if targetTier == 0 {
		return 0
	}
	// Keep a list of tuples of [weakTime, bondStrength]. Later, we'll check
	// these against expired bonds, to see how many tiers we can expect to have
	// refunded funds available for.
	activeTiers := make([][2]uint64, 0)
	dexCfg := dc.config()
	bondExpiry := dexCfg.BondExpiry
	pBuffer := uint64(pendingBuffer(c.net))
	var tierSum uint64
	for _, bond := range append(acct.pendingBonds, acct.bonds...) {
		weakTime := bond.LockTime - bondExpiry - pBuffer
		ba := dexCfg.BondAssets[dex.BipIDSymbol(bond.AssetID)]
		if ba == nil {
			// Bond asset no longer supported. Can't calculate strength.
			// Consider it strength one.
			activeTiers = append(activeTiers, [2]uint64{weakTime, 1})
			continue
		}

		tiers := bond.Amount / ba.Amt
		// We won't count any active bond strength > our tier target.
		if tiers > targetTier-tierSum {
			tiers = targetTier - tierSum
		}
		tierSum += tiers
		activeTiers = append(activeTiers, [2]uint64{weakTime, tiers})
		if tierSum == targetTier {
			break
		}
	}

	// If our active+pending bonds don't cover our target tier for some reason,
	// we need to add the missing bond strength. Double-count because we
	// needed to renew these bonds too.
	reserveTiers := (targetTier - tierSum) * 2
	sort.Slice(activeTiers, func(i, j int) bool {
		return activeTiers[i][0] < activeTiers[j][0]
	})
	sort.Slice(acct.expiredBonds, func(i, j int) bool { // probably already is sorted, but whatever
		return acct.expiredBonds[i].LockTime < acct.expiredBonds[j].LockTime
	})
	sBuffer := uint64(spendableDelay(c.net))
out:
	for _, bond := range acct.expiredBonds {
		if bond.AssetID != bondAsset.ID {
			continue
		}
		strength := bond.Amount / bondAsset.Amt
		refundableTime := bond.LockTime + sBuffer
		for i, pair := range activeTiers {
			weakTime, tiers := pair[0], pair[1]
			if tiers == 0 {
				continue
			}
			if refundableTime >= weakTime {
				// Everything is time-sorted. If this bond won't be refunded
				// in time, none of the others will either.
				break out
			}
			// Modify the activeTiers strengths in-place. Will cause some
			// extra iteration, but beats the complexity of trying to modify
			// the slice somehow.
			if tiers < strength {
				strength -= tiers
				activeTiers[i][1] = 0
			} else {
				activeTiers[i][1] = tiers - strength
				// strength = 0
				break
			}
		}
	}
	for _, pair := range activeTiers {
		reserveTiers += pair[1]
	}
	return reserveTiers * bondAsset.Amt
}

// dexBondConfig retrieves a dex's configuration related to bonds.
func (c *Core) dexBondConfig(dc *dexConnection, now int64) *dexBondCfg {
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
	return &dexBondCfg{
		bondAssets:     bondAssets,
		replaceThresh:  replaceThresh,
		bondExpiry:     bondExpiry,
		haveConnected:  haveConnected,
		lockTimeThresh: lockTimeThresh,
	}
}

type dexAcctBondState struct {
	ExchangeAuth
	repost   []*asset.Bond
	mustPost int64 // includes toComp
	toComp   int64
	inBonds  uint64
}

// bondStateOfDEX collects all the information needed to determine what
// bonds need to be refunded and how much new bonds should be posted.
func (c *Core) bondStateOfDEX(dc *dexConnection, bondCfg *dexBondCfg) *dexAcctBondState {
	dc.acct.authMtx.Lock()
	defer dc.acct.authMtx.Unlock()

	state := new(dexAcctBondState)
	weakBonds := make([]*db.Bond, 0, len(dc.acct.bonds))

	filterExpiredBonds := func(bonds []*db.Bond) (liveBonds []*db.Bond) {
		for _, bond := range bonds {
			if int64(bond.LockTime) <= bondCfg.lockTimeThresh {
				// Often auth, reconnect, or a bondexpired notification will
				// do this first, but we must also here for refunds when the
				// DEX host is down or gone.
				dc.acct.expiredBonds = append(dc.acct.expiredBonds, bond)
				c.log.Infof("Newly expired bond found: %v (%s)", coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
			} else {
				if int64(bond.LockTime) <= bondCfg.replaceThresh {
					weakBonds = append(weakBonds, bond) // but not yet expired (still live or pending)
					c.log.Debugf("Soon to expire bond found: %v (%s)",
						coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
				}
				liveBonds = append(liveBonds, bond)
			}
		}
		return liveBonds
	}

	state.Rep, state.TargetTier, state.EffectiveTier = dc.acct.rep, dc.acct.targetTier, dc.acct.rep.EffectiveTier()
	state.BondAssetID, state.MaxBondedAmt, state.PenaltyComps = dc.acct.bondAsset, dc.acct.maxBondedAmt, dc.acct.penaltyComps
	state.inBonds, _ = dc.bondTotalInternal(state.BondAssetID)
	// Screen the unexpired bonds slices.
	dc.acct.bonds = filterExpiredBonds(dc.acct.bonds)
	dc.acct.pendingBonds = filterExpiredBonds(dc.acct.pendingBonds) // possibly expired before confirmed
	state.PendingStrength = sumBondStrengths(dc.acct.pendingBonds, bondCfg.bondAssets)
	state.WeakStrength = sumBondStrengths(weakBonds, bondCfg.bondAssets)
	state.LiveStrength = sumBondStrengths(dc.acct.bonds, bondCfg.bondAssets) // for max bonded check
	state.PendingBonds = dc.pendingBonds()
	// Extract the expired bonds.
	state.ExpiredBonds = make([]*db.Bond, len(dc.acct.expiredBonds))
	copy(state.ExpiredBonds, dc.acct.expiredBonds)
	// Retry postbond for pending bonds that may have failed during
	// submission after their block waiters triggered.
	state.repost = make([]*asset.Bond, 0, len(dc.acct.pendingBonds))
	for _, bond := range dc.acct.pendingBonds {
		if bondCfg.haveConnected && !c.waiting(bond.CoinID, bond.AssetID) {
			c.log.Warnf("Found a pending bond that is not waiting for confirmations. Re-posting: %s (%s)",
				coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
			state.repost = append(state.repost, assetBond(bond))
		}
	}

	// Calculate number of bonds increments to post.
	bondedTier := state.LiveStrength + state.PendingStrength
	strongBondedTier := bondedTier - state.WeakStrength
	if uint64(strongBondedTier) < state.TargetTier {
		state.mustPost += int64(state.TargetTier) - strongBondedTier
	} else if uint64(strongBondedTier) > state.TargetTier {
		state.Compensation = strongBondedTier - int64(state.TargetTier)
	}
	// Look for penalties to replace.
	expectedServerTier := state.LiveStrength
	reportedServerTier := state.Rep.EffectiveTier()
	if reportedServerTier < expectedServerTier {
		state.toComp = expectedServerTier - reportedServerTier
		penaltyCompRemainder := int64(dc.acct.penaltyComps) - state.Compensation
		if penaltyCompRemainder <= 0 {
			penaltyCompRemainder = 0
		}
		if state.toComp > penaltyCompRemainder {
			state.toComp = penaltyCompRemainder
		}
	}
	state.mustPost += state.toComp
	return state
}

func (c *Core) exchangeAuth(dc *dexConnection) *ExchangeAuth {
	return &c.bondStateOfDEX(dc, c.dexBondConfig(dc, time.Now().Unix())).ExchangeAuth
}

type bondID struct {
	assetID uint32
	coinID  []byte
}

// refundExpiredBonds refunds expired bonds and returns the list of bonds that
// have been refunded and their assetIDs.
func (c *Core) refundExpiredBonds(ctx context.Context, acct *dexAccount, cfg *dexBondCfg, state *dexAcctBondState, now int64) (map[uint32]struct{}, int64, error) {
	spentBonds := make([]*bondID, 0, len(state.ExpiredBonds))
	assetIDs := make(map[uint32]struct{})

	for _, bond := range state.ExpiredBonds {
		bondIDStr := fmt.Sprintf("%v (%s)", coinIDString(bond.AssetID, bond.CoinID), unbip(bond.AssetID))
		if now < int64(bond.LockTime) {
			ttr := time.Duration(int64(bond.LockTime)-now) * time.Second
			if ttr < 15*time.Minute || ((ttr/time.Minute)%30 == 0 && (ttr%time.Minute <= bondTickInterval)) {
				c.log.Debugf("Expired bond %v refundable in about %v.", bondIDStr, ttr)
			}
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
			return nil, 0, fmt.Errorf("Wallet %v is not an asset.Bonder", unbip(bond.AssetID))
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
		if bond.KeyIndex == math.MaxUint32 { // invalid/unknown key index fallback (v0 db.Bond, which was never released, or unknown bond from server), also will skirt reserves :/
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
		} else { // expected case -- TODO: remove the math.MaxUint32 sometime after bonds V1
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
			subject, details := c.formatDetails(TopicBondRefunded, makeCoinIDToken(bond.CoinID.String(), bond.AssetID), acct.host,
				makeCoinIDToken(refundCoinStr, bond.AssetID), wallet.amtString(refundVal), wallet.amtString(bond.Amount))
			c.notify(newBondRefundNote(TopicBondRefunded, subject, details, db.Success))
		}

		err = c.db.BondRefunded(acct.host, assetID, bond.CoinID)
		if err != nil { // next DB load we'll retry, hit bondAlreadySpent, and store here again
			c.log.Errorf("Failed to mark bond as refunded: %v", err)
		}

		spentBonds = append(spentBonds, &bondID{assetID, bond.CoinID})
		assetIDs[assetID] = struct{}{}
	}

	// Remove spentbonds from the dexConnection's expiredBonds list.
	acct.authMtx.Lock()
	for _, spentBond := range spentBonds {
		for i, bond := range acct.expiredBonds {
			if bond.AssetID == spentBond.assetID && bytes.Equal(bond.CoinID, spentBond.coinID) {
				acct.expiredBonds = cutBond(acct.expiredBonds, i)
				break // next spentBond
			}
		}
	}
	expiredBondsStrength := sumBondStrengths(acct.expiredBonds, cfg.bondAssets)
	acct.authMtx.Unlock()

	return assetIDs, expiredBondsStrength, nil
}

// repostPendingBonds rebroadcasts all pending bond transactions for a
// dexConnection.
func (c *Core) repostPendingBonds(dc *dexConnection, cfg *dexBondCfg, state *dexAcctBondState, unlocked bool) {
	for _, bond := range state.repost {
		if !unlocked { // can't sign the postbond msg
			c.log.Warnf("Cannot post pending bond for %v until account is unlocked.", dc.acct.host)
			continue
		}
		// Not dependent on authed - this may be the first bond
		// (registering) where bondConfirmed does authDEX if needed.
		if bondAsset, ok := cfg.bondAssets[bond.AssetID]; ok {
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
}

// postRequiredBonds posts any required bond increments for a dexConnection.
func (c *Core) postRequiredBonds(
	dc *dexConnection,
	cfg *dexBondCfg,
	state *dexAcctBondState,
	bondAsset *msgjson.BondAsset,
	wallet *xcWallet,
	expiredStrength int64,
	unlocked bool,
) (newlyBonded uint64) {

	if state.TargetTier == 0 || state.mustPost <= 0 || cfg.bondExpiry <= 0 {
		return
	}

	c.log.Infof("Gotta post %d bond increments now. Target tier %d, current bonded tier %d (%d weak, %d pending), compensating %d penalties",
		state.mustPost, state.TargetTier, state.Rep.BondedTier, state.WeakStrength, state.PendingStrength, state.toComp)

	if !unlocked || dc.status() != comms.Connected {
		c.log.Warnf("Unable to post the required bond while disconnected or account is locked.")
		return
	}
	_, err := wallet.refreshUnlock()
	if err != nil {
		c.log.Errorf("failed to unlock bond asset wallet %v: %v", unbip(state.BondAssetID), err)
		return
	}
	err = wallet.checkPeersAndSyncStatus()
	if err != nil {
		c.log.Errorf("Cannot post new bonds yet. %v", err)
		return
	}

	// For the max bonded limit, we'll normalize all bonds to the
	// currently selected bond asset.
	toPost := state.mustPost
	amt := bondAsset.Amt * uint64(state.mustPost)
	currentlyBondedAmt := uint64(state.PendingStrength+state.LiveStrength+expiredStrength) * bondAsset.Amt
	for state.MaxBondedAmt > 0 && amt+currentlyBondedAmt > state.MaxBondedAmt && toPost > 0 {
		toPost-- // dumber, but reads easier
		amt = bondAsset.Amt * uint64(toPost)
	}
	if toPost == 0 {
		c.log.Warnf("Unable to post new bond with equivalent of %s currently bonded (limit of %s)",
			wallet.amtString(currentlyBondedAmt), wallet.amtString(state.MaxBondedAmt))
		return
	}
	if toPost < state.mustPost {
		c.log.Warnf("Only posting %d bond increments instead of %d because of current bonding limit of %s",
			toPost, state.mustPost, wallet.amtString(state.MaxBondedAmt))
	}

	lockTime, err := c.calculateMergingLockTime(dc)
	if err != nil {
		c.log.Errorf("Error calculating merging locktime: %v", err)
		return
	}

	_, err = c.makeAndPostBond(dc, true, wallet, amt, c.feeSuggestionAny(wallet.AssetID), lockTime, bondAsset)
	if err != nil {
		c.log.Errorf("Unable to post bond: %v", err)
		return
	}
	return amt
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

	for _, dc := range c.dexConnections() {
		initialized, unlocked := dc.acct.status()
		if !initialized {
			continue // view-only or temporary connection
		}
		// Account unlocked is generally implied by bondKeysReady, but we will
		// check per-account before post since accounts can be individually
		// locked. However, we must refund bonds regardless.

		bondCfg := c.dexBondConfig(dc, now)
		if len(bondCfg.bondAssets) == 0 {
			if !dc.IsDown() {
				dc.log.Meter("no-bond-assets", time.Minute*10).Warnf("Zero bond assets reported for apparently connected DCRDEX server")
			}
			continue
		}
		acctBondState := c.bondStateOfDEX(dc, bondCfg)

		c.repostPendingBonds(dc, bondCfg, acctBondState, unlocked)

		refundedAssets, expiredStrength, err := c.refundExpiredBonds(ctx, dc.acct, bondCfg, acctBondState, now)
		if err != nil {
			c.log.Errorf("Failed to refund expired bonds for %v: %v", dc.acct.host, err)
			continue
		}
		for assetID := range refundedAssets {
			c.updateAssetBalance(assetID)
		}

		bondAsset := bondCfg.bondAssets[acctBondState.BondAssetID]
		if bondAsset == nil {
			if acctBondState.TargetTier > 0 {
				c.log.Warnf("Bond asset %d not supported by DEX %v", acctBondState.BondAssetID, dc.acct.host)
			}
			continue
		}

		wallet, err := c.connectedWallet(acctBondState.BondAssetID)
		if err != nil {
			if acctBondState.TargetTier > 0 {
				c.log.Errorf("%v wallet not available for bonds: %v", unbip(acctBondState.BondAssetID), err)
			}
			continue
		}

		c.postRequiredBonds(dc, bondCfg, acctBondState, bondAsset, wallet, expiredStrength, unlocked)
	}

	c.updateBondReserves()
}

func (c *Core) preValidateBond(dc *dexConnection, bond *asset.Bond) error {
	if len(dc.acct.encKey) == 0 {
		return fmt.Errorf("uninitialized account")
	}

	pkBytes := dc.acct.pubKey()
	if len(pkBytes) == 0 {
		return fmt.Errorf("account keys not decrypted")
	}

	// Pre-validate with the raw bytes of the unsigned tx and our account
	// pubkey.
	preBond := &msgjson.PreValidateBond{
		AcctPubKey: pkBytes,
		AssetID:    bond.AssetID,
		Version:    bond.Version,
		RawTx:      bond.UnsignedTx,
	}

	preBondRes := new(msgjson.PreValidateBondResult)
	err := dc.signAndRequest(preBond, msgjson.PreValidateBondRoute, preBondRes, DefaultResponseTimeout)
	if err != nil {
		return codedError(registerErr, err)
	}
	// Check the response signature.
	err = dc.acct.checkSig(append(preBondRes.Serialize(), bond.UnsignedTx...), preBondRes.Sig)
	if err != nil {
		return newError(signatureErr, "preValidateBond: DEX signature validation error: %v", err)
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
		return nil, fmt.Errorf("server reported bond coin ID %v, expected %v", coinIDString(assetID, postBondRes.BondID),
			bondCoinStr)
	}

	dc.acct.authMtx.Lock()
	dc.updateReputation(postBondRes.Reputation)
	dc.acct.authMtx.Unlock()

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
		subject, details := c.formatDetails(TopicBondPostError, err, err)
		c.notify(newBondPostNote(TopicBondPostError, subject, details, db.ErrorLevel, dc.acct.host))
		return
	}

	c.log.Infof("Bond confirmed %v (%s) with expire time of %v", coinIDStr,
		unbip(assetID), time.Unix(int64(pbr.Expiry), 0))
	err = c.bondConfirmed(dc, assetID, coinID, pbr)
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
				details, db.Data, assetID, coinIDStr, int32(confs), host, c.exchangeAuth(dc)))
		}

		return confs >= reqConfs, nil
	}

	c.wait(coinID, assetID, trigger, func(err error) {
		if err != nil {
			subject, details := c.formatDetails(TopicBondPostErrorConfirm, host, err)
			c.notify(newBondPostNote(TopicBondPostError, subject, details, db.ErrorLevel, host))
			return
		}

		c.log.Infof("DEX %v bond txn %s now has %d confirmations. Submitting postbond request...",
			host, coinIDStr, reqConfs)

		c.postAndConfirmBond(dc, bond) // if it fails (e.g. timeout), retry in rotateBonds
	})
}

// RedeemPrepaidBond redeems a pre-paid bond for a dcrdex host server.
func (c *Core) RedeemPrepaidBond(appPW []byte, code []byte, host string, certI any) (tier uint64, err error) {
	// Make sure the app has been initialized.
	if !c.IsInitialized() {
		return 0, fmt.Errorf("app not initialized")
	}

	// Check the app password.
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return 0, codedError(passwordErr, err)
	}
	defer crypter.Close()

	var success, acctExists bool

	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if found {
		acctExists = !dc.acct.isViewOnly()
		if acctExists {
			if dc.acct.locked() { // require authDEX first to reconcile any existing bond statuses
				return 0, newError(acctKeyErr, "acct locked %s (login first)", host)
			}
		}
	} else {
		// New DEX connection.
		cert, err := parseCert(host, certI, c.net)
		if err != nil {
			return 0, newError(fileReadErr, "failed to read certificate file from %s: %v", cert, err)
		}
		dc, err = c.connectDEX(&db.AccountInfo{
			Host: host,
			Cert: cert,
			// bond maintenance options set below.
		})
		if err != nil {
			return 0, codedError(connectionErr, err)
		}

		// Close the connection to the dex server if the registration fails.
		defer func() {
			if !success {
				dc.connMaster.Disconnect()
			}
		}()
	}

	if !acctExists { // new dex connection or pre-existing view-only connection
		_, err := c.discoverAccount(dc, crypter)
		if err != nil {
			return 0, err
		}
	}

	pkBytes := dc.acct.pubKey()
	if len(pkBytes) == 0 {
		return 0, fmt.Errorf("account keys not decrypted")
	}

	// Do a postbond request with the raw bytes of the unsigned tx, the bond
	// script, and our account pubkey.
	postBond := &msgjson.PostBond{
		AcctPubKey: pkBytes,
		AssetID:    account.PrepaidBondID,
		// Version:    0,
		CoinID: code,
	}
	postBondRes := new(msgjson.PostBondResult)
	if err = dc.signAndRequest(postBond, msgjson.PostBondRoute, postBondRes, DefaultResponseTimeout); err != nil {
		return 0, codedError(registerErr, err)
	}

	// Check the response signature.
	err = dc.acct.checkSig(postBondRes.Serialize(), postBondRes.Sig)
	if err != nil {
		c.log.Warnf("postbond: DEX signature validation error: %v", err)
	}

	lockTime := postBondRes.Expiry + dc.config().BondExpiry

	dbBond := &db.Bond{
		// Version:    0,
		AssetID:   account.PrepaidBondID,
		CoinID:    code,
		LockTime:  lockTime,
		Strength:  postBondRes.Strength,
		Confirmed: true,
	}

	dc.acct.authMtx.Lock()
	dc.updateReputation(postBondRes.Reputation)
	dc.acct.bonds = append(dc.acct.bonds, dbBond)
	dc.acct.authMtx.Unlock()

	if !acctExists {
		dc.acct.keyMtx.RLock()
		ai := &db.AccountInfo{
			Host:      dc.acct.host,
			Cert:      dc.acct.cert,
			DEXPubKey: dc.acct.dexPubKey,
			EncKeyV2:  dc.acct.encKey,
			Bonds:     []*db.Bond{dbBond},
		}
		dc.acct.keyMtx.RUnlock()

		if err = c.dbCreateOrUpdateAccount(dc, ai); err != nil {
			return 0, fmt.Errorf("failed to store pre-paid account for dex %s: %w", host, err)
		}
		c.addDexConnection(dc)
	}

	success = true // Don't disconnect anymore.

	if err = c.db.AddBond(dc.acct.host, dbBond); err != nil {
		return 0, fmt.Errorf("failed to store pre-paid bond for dex %s: %w", host, err)
	}

	if err = c.bondConfirmed(dc, account.PrepaidBondID, code, postBondRes); err != nil {
		return 0, fmt.Errorf("bond redeemed, but failed to auth: %v", err)
	}

	c.updateBondReserves()

	c.notify(newBondAuthUpdate(dc.acct.host, c.exchangeAuth(dc)))

	return uint64(postBondRes.Strength), nil
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
	dc, _, err := c.dex(form.Host)
	if err != nil {
		return err
	}
	// TODO: exclude unregistered and/or watch-only
	dbAcct, err := c.db.Account(form.Host)
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
	var penaltyComps0 uint16
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

	var success bool
	dc.acct.authMtx.Lock()
	defer func() {
		dc.acct.authMtx.Unlock()
		if success {
			c.notify(newBondAuthUpdate(dc.acct.host, c.exchangeAuth(dc)))
		}
	}()

	if !dc.acct.isAuthed {
		return errors.New("login or register first")
	}

	// Revert to initial values if we encounter any error below.
	bondAssetID0 = dc.acct.bondAsset
	targetTier0, maxBondedAmt0, penaltyComps0 = dc.acct.targetTier, dc.acct.maxBondedAmt, dc.acct.penaltyComps
	defer func() { // still under authMtx lock on defer stack
		if !success {
			dc.acct.bondAsset = bondAssetID0
			dc.acct.maxBondedAmt = maxBondedAmt0
			dc.acct.penaltyComps = penaltyComps0
			if dc.acct.targetTier > 0 || assetChanged {
				dc.acct.targetTier = targetTier0
			} // else the user was trying to clear target tier and the wallet was gone too
		}
	}()

	// Verify the new bond asset wallet first.
	bondAssetID := bondAssetID0
	if form.BondAssetID != nil {
		bondAssetID = *form.BondAssetID
	}
	assetChanged = bondAssetID != bondAssetID0

	targetTier := targetTier0
	if form.TargetTier != nil {
		targetTier = *form.TargetTier
	}
	tierChanged = targetTier != targetTier0
	if tierChanged {
		dc.acct.targetTier = targetTier
		dbAcct.TargetTier = targetTier
	}

	var penaltyComps = penaltyComps0
	if form.PenaltyComps != nil {
		penaltyComps = *form.PenaltyComps
	}
	dc.acct.penaltyComps = penaltyComps
	dbAcct.PenaltyComps = penaltyComps

	var bondAssetAmt uint64 // because to disable we must proceed even with no config
	bondAsset := bondAssets[bondAssetID]
	if bondAsset == nil {
		if targetTier > 0 || assetChanged {
			return fmt.Errorf("dex %v is does not support %v as a bond asset (or we lack their config)",
				dbAcct.Host, unbip(bondAssetID))
		} // else disable, attempting to unreserve funds if wallet is available
	} else {
		bondAssetAmt = bondAsset.Amt
	}

	// If we're lowering our bond, we can't set the max bonded amount too low.
	tierForDefaultMaxBonded := targetTier
	if targetTier > 0 && targetTier0 > targetTier {
		tierForDefaultMaxBonded = targetTier0
	}

	maxBonded := maxBondedMult * bondAssetAmt * (tierForDefaultMaxBonded + uint64(penaltyComps)) // the min if none specified
	if form.MaxBondedAmt != nil {
		requested := *form.MaxBondedAmt
		if requested < maxBonded {
			return fmt.Errorf("requested bond maximum of %d is lower than minimum of %d", requested, maxBonded)
		}
		maxBonded = requested
	}

	var found bool
	wallet, found = c.wallet(bondAssetID)
	if !found || !wallet.connected() {
		return fmt.Errorf("bond asset wallet %v does not exist or is not connected", unbip(bondAssetID))
	}
	bonder, ok := wallet.Wallet.(asset.Bonder)
	if !ok {
		return fmt.Errorf("wallet %v is not an asset.Bonder", unbip(bondAssetID))
	}

	_, err = wallet.refreshUnlock()
	if err != nil {
		return fmt.Errorf("bond asset wallet %v is locked", unbip(bondAssetID))
	}

	if assetChanged || tierChanged {
		bal, err := wallet.Balance()
		if err != nil {
			return fmt.Errorf("failed to get balance for %s wallet: %w", unbip(bondAssetID), err)
		}
		avail := bal.Available + bal.BondReserves

		// We need to recalculate bond reserves, including all other exchanges.
		// We're under the dc.acct.authMtx lock, so we'll add our contribution
		// first and then iterate the others in a loop where we're okay to lock
		// their authMtx (via bondTotal).
		nominalReserves := c.minBondReserves(dc, bondAsset)
		var n uint64
		if targetTier > 0 {
			n = 1
		}
		var tiers uint64 = targetTier
		for _, otherDC := range c.dexConnections() {
			if otherDC.acct.host == dc.acct.host { // Only adding others
				continue
			}
			assetID, _, _ := otherDC.bondOpts()
			if assetID != bondAssetID {
				continue
			}
			bondAsset, _ := otherDC.bondAsset(assetID)
			if bondAsset == nil {
				continue
			}
			n++
			tiers += targetTier
			ba := BondAsset(*bondAsset)
			otherDC.acct.authMtx.RLock()
			nominalReserves += c.minBondReserves(dc, &ba)
			otherDC.acct.authMtx.RUnlock()
		}

		var feeReserves uint64
		if n > 0 {
			feeBuffer := bonder.BondsFeeBuffer(c.feeSuggestionAny(bondAssetID))
			feeReserves = n * feeBuffer
			req := nominalReserves + feeReserves
			c.log.Infof("%d DEX server(s) using %s for bonding a total of %d tiers. %d required includes %d in fee reserves. Current balance = %d",
				n, unbip(bondAssetID), tiers, req, feeReserves, avail)
			// If raising the tier or changing asset, enforce available funds.
			if (assetChanged || targetTier > targetTier0) && req > avail {
				return fmt.Errorf("insufficient funds. need %d, have %d", req, avail)
			}
		}

		bonder.SetBondReserves(nominalReserves + feeReserves)

		dc.acct.bondAsset = bondAssetID
		dbAcct.BondAsset = bondAssetID
	}

	if assetChanged || tierChanged || form.MaxBondedAmt != nil || maxBonded < dc.acct.maxBondedAmt {
		dc.acct.maxBondedAmt = maxBonded
		dbAcct.MaxBondedAmt = maxBonded
	}

	c.triggerBondRotation()

	c.log.Debugf("Bond options for %v: target tier %d, bond asset %d, maxBonded %v",
		dbAcct.Host, dc.acct.targetTier, dc.acct.bondAsset, dbAcct.MaxBondedAmt)

	if err = c.db.UpdateAccountInfo(dbAcct); err == nil {
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
	return bonder.BondsFeeBuffer(c.feeSuggestionAny(assetID)), nil
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
//
// Note that the FeeBuffer field of the form is optional, but it may be provided
// to ensure that the wallet reserves the amount reported by a preceding call to
// BondsFeeBuffer, such as during initial wallet funding.
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
	err = wallet.checkPeersAndSyncStatus()
	if err != nil {
		return nil, err
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
			return nil, newError(walletBalanceErr, "insufficient available balance")
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

	feeRate := c.feeSuggestionAny(bondAssetID, dc)

	// Ensure this DEX supports this asset for bond, and get the required
	// confirmations and bond amount.
	bondAsset, _ := dc.bondAsset(bondAssetID)
	if bondAsset == nil {
		return nil, newError(assetSupportErr, "dex host has not connected or does not support fidelity bonds in asset %q", bondAssetSymbol)
	}

	var lockTime time.Time
	if form.LockTime > 0 {
		lockTime = time.Unix(int64(form.LockTime), 0)
	} else {
		lockTime, err = c.calculateMergingLockTime(dc)
		if err != nil {
			return nil, err
		}
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
		maxBondedAmt := maxBondedMult * form.Bond // default
		if form.MaxBondedAmt != nil {
			maxBondedAmt = *form.MaxBondedAmt
		}
		dc.acct.authMtx.Lock()
		dc.acct.bondAsset = bondAssetID
		dc.acct.targetTier = form.Bond / bondAsset.Amt
		dc.acct.maxBondedAmt = maxBondedAmt
		dc.acct.authMtx.Unlock()
	}

	// Make a bond transaction for the account ID generated from our public key.
	bondCoin, err := c.makeAndPostBond(dc, acctExists, wallet, form.Bond, feeRate, lockTime, bondAsset)
	if err != nil {
		return nil, err
	}
	c.updateBondReserves() // Can probably reduce reserves because of the pending bond.
	success = true
	bondCoinStr := coinIDString(bondAssetID, bondCoin)
	return &PostBondResult{BondID: bondCoinStr, ReqConfirms: uint16(bondAsset.Confs)}, nil
}

// calculateMergingLockTime calculates a locktime for a new bond for the
// specified account, with consideration for merging parallel bond tracks.
// Tracks are merged by choosing the locktime of an existing bond if one exists
// and has a locktime value in an acceptable range. We will merge tracks even if
// it means reducing the live period associated with the bond by as much as
// ~75%.
func (c *Core) calculateMergingLockTime(dc *dexConnection) (time.Time, error) {
	bondExpiry := int64(dc.config().BondExpiry)
	lockDur := minBondLifetime(c.net, bondExpiry)
	lockTime := time.Now().Add(lockDur).Truncate(time.Second)
	expireTime := lockTime.Add(time.Second * time.Duration(-bondExpiry)) // when the server would expire the bond
	if time.Until(expireTime) < time.Minute {
		return time.Time{}, newError(bondTimeErr, "bond would expire in less than one minute")
	}
	if lockDur := time.Until(lockTime); lockDur > lockTimeLimit {
		return time.Time{}, newError(bondTimeErr, "excessive lock time (%v>%v)", lockDur, lockTimeLimit)
	}

	// If we have parallel bond tracks out of sync, we may use an earlier lock
	// time in order to get back in sync.
	mergeableLocktimeThresh := uint64(time.Now().Unix() + bondExpiry*5/4 + pendingBuffer(c.net))
	var bestMergeableLocktime uint64
	dc.acct.authMtx.RLock()
	for _, b := range dc.acct.bonds {
		if b.LockTime > mergeableLocktimeThresh && (bestMergeableLocktime == 0 || b.LockTime > bestMergeableLocktime) {
			bestMergeableLocktime = b.LockTime
		}
	}
	dc.acct.authMtx.RUnlock()
	if bestMergeableLocktime > 0 {
		newLockTime := time.Unix(int64(bestMergeableLocktime), 0)
		bondExpiryDur := time.Duration(bondExpiry) * time.Second
		c.log.Infof("Reducing bond expiration date from %s to %s to facilitate merge with parallel bond track",
			lockTime.Add(-bondExpiryDur), newLockTime.Add(-bondExpiryDur))
		lockTime = newLockTime
	}
	return lockTime, nil
}

func (c *Core) makeAndPostBond(dc *dexConnection, acctExists bool, wallet *xcWallet, amt, feeRate uint64,
	lockTime time.Time, bondAsset *msgjson.BondAsset) ([]byte, error) {

	bondKey, keyIndex, err := c.nextBondKey(bondAsset.ID)
	if err != nil {
		return nil, fmt.Errorf("bond key derivation failed: %v", err)
	}
	defer bondKey.Zero()

	acctID := dc.acct.ID()
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
		Strength:   uint32(amt / bondAsset.Amt),
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

	// Set up the coin waiter, which watches confirmations so the user knows
	// when to expect their account to be marked paid by the server.
	c.monitorBondConfs(dc, bond, reqConfs)

	c.updateAssetBalance(bond.AssetID)

	// Start waiting for reqConfs.
	subject, details := c.formatDetails(TopicBondConfirming, reqConfs, makeCoinIDToken(bondCoinStr, bond.AssetID), unbip(bond.AssetID), dc.acct.host)
	c.notify(newBondPostNoteWithConfirmations(TopicBondConfirming, subject,
		details, db.Success, bond.AssetID, bondCoinStr, 0, dc.acct.host, c.exchangeAuth(dc)))

	return bond.CoinID, nil
}

func (c *Core) updatePendingBondConfs(dc *dexConnection, assetID uint32, coinID []byte, confs uint32) {
	dc.acct.authMtx.Lock()
	defer dc.acct.authMtx.Unlock()
	bondIDStr := coinIDString(assetID, coinID)
	dc.acct.pendingBondsConfs[bondIDStr] = confs
}

func (c *Core) bondConfirmed(dc *dexConnection, assetID uint32, coinID []byte, pbr *msgjson.PostBondResult) error {
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

	dc.acct.rep = *pbr.Reputation
	effectiveTier := dc.acct.rep.EffectiveTier()
	bondedTier := dc.acct.rep.BondedTier
	targetTier := dc.acct.targetTier
	isAuthed := dc.acct.isAuthed
	dc.acct.authMtx.Unlock()

	if foundPending {
		// Set bond confirmed in the DB.
		err := c.db.ConfirmBond(dc.acct.host, assetID, coinID)
		if err != nil {
			return fmt.Errorf("db.ConfirmBond failure: %w", err)
		}
		subject, details := c.formatDetails(TopicBondConfirmed, effectiveTier, targetTier)
		c.notify(newBondPostNoteWithTier(TopicBondConfirmed, subject, details, db.Success, dc.acct.host, bondedTier, c.exchangeAuth(dc)))
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
		subject, details := c.formatDetails(TopicDexAuthErrorBond, err)
		c.notify(newDEXAuthNote(TopicDexAuthError, subject, dc.acct.host, false, details, db.ErrorLevel))
		return err
	}

	subject, details := c.formatDetails(TopicAccountRegTier, effectiveTier)
	c.notify(newBondPostNoteWithTier(TopicAccountRegistered, subject,
		details, db.Success, dc.acct.host, bondedTier, c.exchangeAuth(dc))) // possibly redundant with SubjectBondConfirmed

	return nil
}

func (c *Core) bondExpired(dc *dexConnection, assetID uint32, coinID []byte, note *msgjson.BondExpiredNotification) error {
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

	if note.Reputation != nil {
		dc.acct.rep = *note.Reputation
	} else {
		dc.acct.rep.BondedTier = note.Tier + int64(dc.acct.rep.Penalties)
	}
	targetTier := dc.acct.targetTier
	effectiveTier := dc.acct.rep.EffectiveTier()
	bondedTier := dc.acct.rep.BondedTier
	dc.acct.authMtx.Unlock()

	bondIDStr := coinIDString(assetID, coinID)
	if !found {
		c.log.Warnf("bondExpired: Bond %s (%s) in bondexpired message not found locally (already refunded?).",
			bondIDStr, unbip(assetID))
	}

	if int64(targetTier) > effectiveTier {
		subject, details := c.formatDetails(TopicBondExpired, effectiveTier, targetTier)
		c.notify(newBondPostNoteWithTier(TopicBondExpired, subject,
			details, db.WarningLevel, dc.acct.host, bondedTier, c.exchangeAuth(dc)))
	}

	return nil
}
