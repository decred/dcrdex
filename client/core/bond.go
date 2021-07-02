package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

const (
	bondAssetSymbol = "dcr" // Hard-coded to Decred for bond, for now
	// lockTimeLimit is an upper limit on the allowable bond lockTime.
	lockTimeLimit = 120 * 24 * time.Hour
)

func cutBond(bonds []*db.Bond, i int) []*db.Bond { // input slice modified
	bonds[i] = bonds[len(bonds)-1]
	bonds[len(bonds)-1] = nil
	bonds = bonds[:len(bonds)-1]
	return bonds
}

func (c *Core) watchExpiredBonds(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.refundExpiredBonds(ctx)
		}
	}
}

func (c *Core) refundExpiredBonds(ctx context.Context) {
	// 1. Refund bonds with passed lockTime.
	// 2. Move bonds that are expired according to DEX bond expiry into
	//    expiredBonds (lockTime<lockTimeThresh).
	// 3. TODO: Add bonds to keep N bonds active, according to some optional config.
	now := time.Now().Unix()

	type bondID struct {
		assetID uint32
		coinID  []byte
	}

	for _, dc := range c.dexConnections() {

		dc.cfgMtx.RLock()
		lockTimeThresh := now + int64(dc.cfg.BondExpiry)
		dc.cfgMtx.RUnlock()

		filterExpiredBonds := func(bond []*db.Bond) (liveBonds []*db.Bond) {
			for _, bond := range bond {
				if int64(bond.LockTime) <= lockTimeThresh {
					// This is generally unexpected because auth, reconnect, or a
					// bondexpired notification should do this first.
					dc.acct.expiredBonds = append(dc.acct.expiredBonds, bond)
					c.log.Warnf("Expired bond found: %v", coinIDString(bond.AssetID, bond.CoinID))
				} else {
					liveBonds = append(liveBonds, bond)
				}
			}
			return liveBonds
		}

		dc.acct.authMtx.Lock()
		// Screen the unexpired bonds slices.
		dc.acct.bonds = filterExpiredBonds(dc.acct.bonds)
		dc.acct.pendingBonds = filterExpiredBonds(dc.acct.pendingBonds) // possible expired before confirmed
		// Extract the expired bonds.
		expiredBonds := make([]*db.Bond, len(dc.acct.expiredBonds))
		copy(expiredBonds, dc.acct.expiredBonds)
		dc.acct.authMtx.Unlock()

		if len(expiredBonds) == 0 {
			continue
		}

		spentBonds := make([]*bondID, 0, len(expiredBonds))
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
				c.log.Errorf("%v wallet %v not available to refund bond %v: %v",
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

			// Generate a refund tx paying to an address from the currently
			// connected wallet, using bond.BondPrivKey to create the signed
			// transaction. The RefundTx is really a backup.
			newRefundTx, err := wallet.RefundBond(bond.CoinID, bond.Script, bond.BondPrivKey)
			bondAlreadySpent := errors.Is(err, asset.CoinNotFoundError) // or never mined!
			if err != nil && !bondAlreadySpent {
				c.log.Errorf("Failed to generate bond refund tx: %v", err)
				continue
			}

			// If the user hasn't already manually refunded the bond, broadcast
			// the refund txn. Mark it refunded and stop tracking regardless.
			if bondAlreadySpent {
				c.log.Warnf("Bond output not found, possibly already spent or never mined! "+
					"Marking refunded. Backup refund transaction: %x", bond.RefundTx)
			} else {
				refundCoinID, err := wallet.SendTransaction(newRefundTx)
				if err != nil {
					c.log.Errorf("Failed to broadcast bond refund txn %v: %v", newRefundTx, err)
					continue
				}
				c.log.Infof("Bond %v refunded in %v (%s)", bondIDStr, coinIDString(assetID, refundCoinID), unbip(bond.AssetID))
			}

			err = c.db.BondRefunded(dc.acct.host, bond.AssetID, bond.CoinID)
			if err != nil {
				c.log.Errorf("Failed to mark bond as refunded: %v", err)
			}

			spentBonds = append(spentBonds, &bondID{assetID, bond.CoinID})
		}

		if len(spentBonds) == 0 {
			continue
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
		dc.acct.authMtx.Unlock()
	}
}

func (c *Core) postBond(dc *dexConnection, bond *asset.Bond) (*msgjson.PostBondResult, error) {
	if len(dc.acct.encKey) == 0 {
		return nil, fmt.Errorf("uninitialized account")
	}

	pkBytes := dc.acct.pubKey()
	if len(pkBytes) == 0 {
		return nil, fmt.Errorf("account keys not decrypted")
	}

	assetID := bond.AssetID
	bondCoin := bond.CoinID
	bondCoinStr := coinIDString(assetID, bondCoin)

	// Do a postbond request with the raw bytes of the unsigned tx, the bond
	// script, and our account pubkey.
	postBond := &msgjson.PostBond{
		AcctPubKey: pkBytes,
		AssetID:    assetID,
		BondTx:     bond.UnsignedTx,
		BondScript: bond.BondScript,
		BondSig:    bond.BondAcctSig,
	}
	if dc.acct.hasLegacyFee() {
		wallet, err := c.connectedWallet(assetID)
		if err != nil {
			return nil, fmt.Errorf("cannot connect to %s wallet to post bond: %w", bondAssetSymbol, err)
		}
		// addr, err := wallet.Address()
		postBond.LegacyFeeRefundAddr = wallet.currentDepositAddress() // not ideal, but we don't want to burn addrs
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

func (c *Core) postAndConfirmBond(dc *dexConnection, bond *asset.Bond, reqConfs uint32) {
	assetID := bond.AssetID
	coinID := bond.CoinID
	coinIDStr := coinIDString(assetID, coinID)
	host := dc.acct.host

	// Inform the server, which will attempt to locate the bond and check
	// confirmations. If the account is new and the bond is located on the
	// network, an account will be created. If server sees the required number
	// of confirmations, the bond will be active and we should confirm the bond
	// (in DB and dc.acct.{bond,pendingBonds}). If the server reports <reqConfs
	// (but still located), it will start waiting for it to reach reqConfs and
	// then send a bondconfirmed ntfn (see handleBondConfirmedMsg).
	pbr, err := c.postBond(dc, bond)
	if err != nil {
		details := fmt.Sprintf("postbond request error: %v", err)
		c.notify(newBondPostNote(TopicBondPostError, string(TopicBondPostError), details, db.ErrorLevel, host))
		return
	}

	if pbr.Confs >= int64(reqConfs) {
		c.log.Infof("Bond confirmed %v (%s) with expire time of %v", coinIDStr, unbip(assetID), time.Unix(pbr.Expiry, 0))
		err = c.bondConfirmed(dc, assetID, coinID, pbr.Tier)
		if err != nil {
			c.log.Errorf("Unable to confirm bond: %v", err)
		}
		return
	}

	if pbr.Confs == -1 {
		c.log.Warnf("Server still doesn't see the bond %s (%s)!", coinIDStr, unbip(assetID))
		// Maybe use a time.AfterFunc or have a Core supervisory goroutine
		// that occasionally ensures there are waiters for pendingBonds?
		// For now, restart to retry.
		return
	}

	// The waiter is still done, but the server will send a bondConfirmed
	// ntfn shortly when it sees reqConfs that will trigger ConfirmBond.
	c.log.Infof("DEX %v reports bond %s (%s) has %d of %d confirmations. Waiting for ntfn...",
		host, coinIDStr, unbip(assetID), pbr.Confs, reqConfs)
}

// monitorBondConfs launches a block waiter for the bond txns to reach the
// required amount of confirmations. Once the requirement is met the server is
// notified.
func (c *Core) monitorBondConfs(dc *dexConnection, bond *asset.Bond, reqConfs uint32) {
	assetID := bond.AssetID
	coinID := bond.CoinID
	coinIDStr := coinIDString(assetID, coinID)
	host := dc.acct.host

	wallet, _ := c.connectedWallet(assetID)
	if wallet == nil {
		c.log.Errorf("No wallet for asset %v", unbip(assetID))
		return
	}
	lastConfs, err := wallet.RegFeeConfirmations(c.ctx, coinID)
	coinNotFound := errors.Is(err, asset.CoinNotFoundError)
	if err != nil && !coinNotFound {
		c.log.Errorf("Error getting confirmations for %s: %w", coinIDStr, err)
		return
	}

	if lastConfs >= reqConfs { // don't bother waiting for a block
		c.postAndConfirmBond(dc, bond, reqConfs)
		return
	}

	if coinNotFound {
		// Broadcast the bond and start waiting for confs.
		c.log.Infof("Broadcasting bond %v (%s), script = %x.\n\n"+
			"BACKUP refund tx paying to current wallet: %x\n\n",
			coinIDStr, unbip(bond.AssetID), bond.BondScript, bond.RedeemTx)
		if _, err = wallet.SendTransaction(bond.SignedTx); err != nil {
			c.log.Warnf("Failed to broadcast bond txn: %v")
			// TODO: screen inputs if the tx is trying to spend spent outputs
			// (invalid bond transaction that should be abandoned).
		}
		c.updateAssetBalance(bond.AssetID)
	}

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
		}

		if confs < reqConfs {
			details := fmt.Sprintf("Fee payment confirmations %v/%v", confs, reqConfs)
			c.notify(newBondPostNoteWithConfirmations(TopicRegUpdate, string(TopicRegUpdate),
				details, db.Data, assetID, int32(confs), host))
		}

		return confs >= reqConfs, nil
	}

	c.wait(coinID, assetID, trigger, func(err error) {
		if err != nil {
			details := fmt.Sprintf("Error encountered while waiting for bond confirms for %s: %v", host, err)
			c.notify(newBondPostNote(TopicBondPostError, string(TopicBondPostError),
				details, db.ErrorLevel, host))
			return
		}

		time.Sleep(2 * time.Second) // reduce likelihood of needing the bondconfirmed ntfn after postbond
		c.log.Infof("DEX %v bond txn %s now has %d confirmations. Submitting postbond request...",
			host, coinIDStr, reqConfs)

		c.postAndConfirmBond(dc, bond, reqConfs)
	})
}

// PostBond posts a new bond for a new or existing DEX account.
func (c *Core) PostBond(form *PostBondForm) (*PostBondResult, error) {
	// Make sure the app has been initialized.
	if !c.IsInitialized() {
		return nil, fmt.Errorf("app not initialized")
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

	// Check the app password.
	crypter, err := c.encryptionKey(form.AppPass)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	if form.Addr == "" {
		return nil, newError(emptyHostErr, "no dex address specified")
	}
	host, err := addrHost(form.Addr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}

	var success bool

	c.connMtx.RLock()
	dc, acctExists := c.conns[host]
	c.connMtx.RUnlock()
	if acctExists {
		if dc.acct.locked() { // require authDEX first to reconcile any existing bond statuses
			return nil, newError(acctKeyErr, "acct locked %s (login first)", form.Addr)
		}
	} else {
		// New DEX connection.
		cert, err := parseCert(host, form.Cert, c.net)
		if err != nil {
			return nil, newError(fileReadErr, "failed to read certificate file from %s: %v", cert, err)
		}
		dc, err := c.connectDEX(&db.AccountInfo{
			Host: host,
			Cert: cert,
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

		paid, err := c.discoverAccount(dc, crypter)
		if err != nil {
			return nil, err
		}
		if paid {
			success = true
			// The listen goroutine is already running, now track the conn.
			c.connMtx.Lock()
			c.conns[dc.acct.host] = dc
			c.connMtx.Unlock()

			return &PostBondResult{ /* no new bond */ }, nil
		}
		// dc.acct is now configured with encKey, privKey, and id for a new
		// (unregistered) account.
	}

	// Ensure this DEX supports this asset for bond, and get the required
	// confirmations and bond amount.
	bondAsset, bondExpiry := dc.bondAsset(bondAssetID)
	if bondAsset == nil {
		return nil, newError(assetSupportErr, "dex server does not support fidelity bonds in asset %q", bondAssetSymbol)
	}
	bondValidity := time.Duration(bondExpiry) * time.Second // bond lifetime

	lockTime := time.Now().Add(2 * bondValidity).Truncate(time.Second) // default lockTime is double
	if form.LockTime > 0 {
		lockTime = time.Unix(int64(form.LockTime), 0)
	}
	expireTime := time.Now().Add(bondValidity) // when the server would expire the bond
	if lockTime.Before(expireTime) {
		return nil, newError(bondTimeErr, "lock time of %d has already passed the server's expiry time of %v (bond expiry %d)",
			form.LockTime, expireTime, bondExpiry)
	}
	if lockTime.Add(-time.Minute).Before(expireTime) {
		return nil, newError(bondTimeErr, "lock time of %d is less than a minute from the server's expiry time of %v (bond expiry %d)",
			form.LockTime, expireTime, bondExpiry)
	}
	if lockDur := time.Until(lockTime); lockDur > lockTimeLimit {
		return nil, newError(bondTimeErr, "excessive lock time (%v>%v)", lockDur, lockTimeLimit)
	}

	if bondAssetID != bondAsset.ID { // internal consistency of config.bondassets array
		return nil, newError(signatureErr, "config response lists asset %d, but expected %d: %v",
			bondAsset.ID, bondAssetID, err)
	}

	// Check that the bond amount is non-zero.
	if form.Bond == 0 {
		return nil, newError(bondAmtErr, "zero registration fees not allowed")
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
	strength := form.Bond / bondAsset.Amt

	// Get ready to generate the bond txn.
	if !wallet.unlocked() {
		err = wallet.Unlock(crypter)
		if err != nil {
			return nil, newError(walletAuthErr, "failed to unlock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}

	// Make a bond transaction for the account ID generated from our public key.
	acctID := dc.acct.ID()
	bond, err := wallet.MakeBondTx(form.Bond, lockTime, acctID[:])
	if err != nil {
		return nil, codedError(registerErr, err)
	}

	// Do postbond with the *unsigned* txn.
	pbr, err := c.postBond(dc, bond)
	if err != nil {
		return nil, err
	}

	reqConfs := bondAsset.Confs
	bondCoinStr := coinIDString(bond.AssetID, bond.CoinID)
	c.log.Infof("DEX %v has validated our bond %v (%s) with strength %d. %d confirmations required to trade.",
		host, bondCoinStr, unbip(bond.AssetID), strength, reqConfs)

	// Store the account and bond info.
	dbBond := &db.Bond{
		AssetID:     bond.AssetID,
		CoinID:      bond.CoinID,
		UnsignedTx:  bond.UnsignedTx,
		SignedTx:    bond.SignedTx,
		Script:      bond.BondScript,
		Amount:      form.Bond,
		LockTime:    uint64(lockTime.Unix()),
		BondPrivKey: bond.BondPrivKey,
		RefundTx:    bond.RedeemTx,
		// Confirmed and Refunded are false (new bond tx)
	}

	if acctExists {
		err = c.db.AddBond(host, dbBond)
		if err != nil {
			return nil, fmt.Errorf("failed to store bond %v (%s) for dex %v: %w",
				bondCoinStr, unbip(bond.AssetID), host, err)
		}
	} else {
		ai := &db.AccountInfo{
			Host:      host,
			Cert:      dc.acct.cert,
			DEXPubKey: dc.acct.dexPubKey,
			EncKeyV2:  dc.acct.encKey,
			Bonds:     []*db.Bond{dbBond},
		}
		err = c.db.CreateAccount(ai)
		if err != nil {
			return nil, fmt.Errorf("failed to store account %v for dex %v: %w",
				dc.acct.id, host, err)
		}
	}

	dc.acct.authMtx.Lock()
	dc.acct.pendingBonds = append(dc.acct.pendingBonds, dbBond)
	dc.acct.authMtx.Unlock()

	// Broadcast the bond and start waiting for confs.
	c.log.Infof("Broadcasting bond %v (%s) with lock time %v, script = %x.\n\n"+
		"BACKUP refund tx paying to current wallet: %x\n\n",
		bondCoinStr, unbip(bond.AssetID), lockTime, bond.BondScript, bond.RedeemTx)
	if bondCoinCast, err := wallet.SendTransaction(bond.SignedTx); err != nil {
		c.log.Warnf("Failed to broadcast bond txn: %v")
	} else if !bytes.Equal(bond.CoinID, bondCoinCast) {
		c.log.Warnf("Broadcasted bond %v; was expecting %v!",
			coinIDString(bond.AssetID, bondCoinCast), bondCoinStr)
	}
	// TODO: return/unlock spent coins?

	c.updateAssetBalance(bond.AssetID)

	if pbr.Confs < int64(reqConfs) { // pbr.Confs should be -1!
		// Start waiting for reqConfs.
		details := fmt.Sprintf("Waiting for %d confirmations to post bond %v (%s) to %s",
			reqConfs, bondCoinStr, unbip(bond.AssetID), dc.acct.host)
		c.notify(newBondPostNoteWithConfirmations(TopicBondConfirming, string(TopicBondConfirming),
			details, db.Success, bond.AssetID, 0, dc.acct.host))
		// Set up the coin waiter, which watches confirmations so the user knows
		// when to expect their account to be marked paid by the server.
		c.monitorBondConfs(dc, bond, reqConfs)
	} else {
		c.log.Warnf("DEX %v claims that out bond txn %v is already confirmed, but we just made it!",
			host, bondCoinStr)
		err = c.bondConfirmed(dc, bond.AssetID, bond.CoinID, pbr.Tier)
		if err != nil {
			c.log.Errorf("Unable to confirm bond: %v", err)
		}
	}

	success = true

	return &PostBondResult{BondID: bondCoinStr, ReqConfirms: uint16(reqConfs)}, err
}

func (c *Core) bondConfirmed(dc *dexConnection, assetID uint32, coinID []byte, newTier int64) error {
	// Update dc.acct.{bonds,pendingBonds,tier} under authMtx lock.
	var foundPending, foundConfirmed bool
	dc.acct.authMtx.Lock()
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
	isAuthed := dc.acct.isAuthed
	dc.acct.authMtx.Unlock()

	bondIDStr := coinIDString(assetID, coinID)
	if foundPending {
		// Set bond confirmed in the DB.
		err := c.db.ConfirmBond(dc.acct.host, assetID, coinID)
		if err != nil {
			return fmt.Errorf("db.ConfirmBond failure: %w", err)
		}
		c.log.Infof("Bond %s (%s) confirmed.", bondIDStr, unbip(assetID))
		details := fmt.Sprintf("New tier = %d.", newTier) // TODO: format to subject,details
		c.notify(newBondPostNoteWithTier(TopicBondConfirmed, string(TopicBondConfirmed), details, db.Success, dc.acct.host, newTier))
	} else if !foundConfirmed {
		c.log.Errorf("bondConfirmed: Bond %s (%s) not found", bondIDStr, unbip(assetID))
		// just try to authenticate...
	} // else already found confirmed (no-op)

	// If we were not previously authenticated, we can infer that this was
	// the bond that created the account server-side, otherwise top-up.
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
			dc.acct.expiredBonds = append(dc.acct.expiredBonds, bond) // we'll wait for lockTime to pass to refund
			found = true
			break
		}
	}
	if !found { // refundExpiredBonds may have gotten to it first
		for _, bond := range dc.acct.expiredBonds {
			if bond.AssetID == assetID && bytes.Equal(bond.CoinID, coinID) {
				found = true
				break
			}
		}
	}

	dc.acct.tier = newTier
	dc.acct.authMtx.Unlock()

	bondIDStr := coinIDString(assetID, coinID)
	if !found {
		c.log.Warnf("bondExpired: Bond %s (%s) in bondexpired message not found locally (already refunded?).",
			bondIDStr, unbip(assetID))
	}

	details := fmt.Sprintf("New tier = %d.", newTier)
	c.notify(newBondPostNoteWithTier(TopicBondExpired, string(TopicBondExpired),
		details, db.Success, dc.acct.host, newTier))

	return nil
}
