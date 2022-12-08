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
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/keygen"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
)

const (
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
		lockTimeThresh := now // in case dex is down, expire (to refund) when lock time is passed
		dc.cfgMtx.RLock()
		if dc.cfg != nil {
			lockTimeThresh += int64(dc.cfg.BondExpiry)
		}
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
			// connected wallet, using bond.PrivKey to create the signed
			// transaction. The RefundTx is really a backup.
			priv := secp256k1.PrivKeyFromBytes(bond.PrivKey)
			newRefundTx, err := wallet.RefundBond(ctx, bond.Version, bond.CoinID, bond.Data, bond.Amount, priv)
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
		// Data:   bond.Data, // presently not part of payload; just client-side records
	}

	preBondRes := new(msgjson.PreValidateBondResult)
	err := dc.signAndRequest(preBond, msgjson.PreValidateBondRoute, preBondRes, DefaultResponseTimeout)
	if err != nil {
		return codedError(registerErr, err)
	}

	// Check the response signature.
	err = dc.acct.checkSig(preBondRes.Serialize(), preBondRes.Sig)
	if err != nil {
		c.log.Warnf("postbond: DEX signature validation error: %v", err)
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
		// Data:   bond.Data, // presently not part of payload; just client-side records
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

	// Certain responses may warrant retrying, such as if the server is already
	// waiting for this bond's confs, but we no longer have a response handler
	// for the initial request. Maybe a Core supervisory goroutine that
	// occasionally ensures there are waiters for pendingBonds? For now just
	// retry like this...
	schedRetry := func() {
		time.AfterFunc(20*time.Second, func() {
			c.postAndConfirmBond(dc, bond)
		}) // not ideal - supervisory goroutine TODO
		c.log.Warnf("Server already confirming bond %v (%s). Will retry.",
			coinIDStr, unbip(assetID))
	}

	// Inform the server, which will attempt to locate the bond and check
	// confirmations. If server sees the required number of confirmations, the
	// bond will be active (and account created if new) and we should confirm
	// the bond (in DB and dc.acct.{bond,pendingBonds}).
	pbr, err := c.postBond(dc, bond) // can be long while server searches
	if err != nil {
		var mErr *msgjson.Error
		if errors.As(err, &mErr) && mErr.Code == msgjson.BondAlreadyConfirmingError {
			// The server is already handling a postbond request for this bond.
			// If we still have a response handler for the initial request, we
			// will get the response. If not (restart or previous request
			// timeout), we can either reconnect or postbond again *after*
			// server has confirmed it.
			schedRetry()
			return
		}
		details := fmt.Sprintf("postbond request error: %v", err)
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
func (c *Core) monitorBondConfs(dc *dexConnection, bond *asset.Bond, reqConfs uint32) {
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

	if coinNotFound {
		// Broadcast the bond and start waiting for confs.
		c.log.Infof("Broadcasting bond %v (%s), data = %x.\n\n"+
			"BACKUP refund tx paying to current wallet: %x\n\n",
			coinIDStr, unbip(bond.AssetID), bond.Data, bond.RedeemTx)
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

		c.log.Infof("DEX %v bond txn %s now has %d confirmations. Submitting postbond request...",
			host, coinIDStr, reqConfs)

		c.postAndConfirmBond(dc, bond)
	})
}

func deriveBondKey(seed []byte, assetID, bondIndex uint32) (*secp256k1.PrivateKey, error) {
	if bondIndex >= hdkeychain.HardenedKeyStart {
		return nil, fmt.Errorf("maximum key generation reached, cannot generate %dth key", bondIndex)
	}

	kids := []uint32{
		hdKeyPurposeBonds,
		assetID + hdkeychain.HardenedKeyStart,
		bondIndex,
	}
	extKey, err := keygen.GenDeepChild(seed, kids)
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

func (c *Core) nextBondKey(crypter encrypt.Crypter, assetID uint32) (*secp256k1.PrivateKey, error) {
	creds := c.creds()
	if creds == nil {
		return nil, errors.New("app not initialized")
	}
	seed, err := crypter.Decrypt(creds.EncSeed)
	if err != nil {
		return nil, fmt.Errorf("seed decryption error: %w", err)
	}
	defer encode.ClearBytes(seed)

	nextBondKeyIndex, err := c.db.NextBondKeyIndex(assetID)
	if err != nil {
		return nil, fmt.Errorf("NextBondIndex: %v", err)
	}

	return deriveBondKey(seed, assetID, nextBondKeyIndex)
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
	defer crypter.Close()
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
		dc, err = c.connectDEX(&db.AccountInfo{
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
	priv, err := c.nextBondKey(crypter, bondAssetID)
	if err != nil {
		return nil, fmt.Errorf("bond key derivation failed: %v", err)
	}
	defer priv.Zero()
	acctID := dc.acct.ID()
	bond, err := wallet.MakeBondTx(bondAsset.Version, form.Bond, lockTime, priv, acctID[:])
	if err != nil {
		return nil, codedError(registerErr, err)
	}

	// Do prevalidatebond with the *unsigned* txn.
	if err = c.preValidateBond(dc, bond); err != nil {
		return nil, err
	}

	reqConfs := bondAsset.Confs
	bondCoinStr := coinIDString(bond.AssetID, bond.CoinID)
	c.log.Infof("DEX %v has validated our bond %v (%s) with strength %d. %d confirmations required to trade.",
		host, bondCoinStr, unbip(bond.AssetID), strength, reqConfs)

	// Store the account and bond info.
	dbBond := &db.Bond{
		Version:    bond.Version,
		AssetID:    bond.AssetID,
		CoinID:     bond.CoinID,
		UnsignedTx: bond.UnsignedTx,
		SignedTx:   bond.SignedTx,
		Data:       bond.Data,
		Amount:     form.Bond,
		LockTime:   uint64(lockTime.Unix()),
		PrivKey:    bond.BondPrivKey,
		RefundTx:   bond.RedeemTx,
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
		c.connMtx.Lock()
		c.conns[dc.acct.host] = dc
		c.connMtx.Unlock()
	}

	dc.acct.authMtx.Lock()
	dc.acct.pendingBonds = append(dc.acct.pendingBonds, dbBond)
	dc.acct.authMtx.Unlock()

	success = true // no errors after this

	// Broadcast the bond and start waiting for confs.
	c.log.Infof("Broadcasting bond %v (%s) with lock time %v, data = %x.\n\n"+
		"BACKUP refund tx paying to current wallet: %x\n\n",
		bondCoinStr, unbip(bond.AssetID), lockTime, bond.Data, bond.RedeemTx)
	if bondCoinCast, err := wallet.SendTransaction(bond.SignedTx); err != nil {
		c.log.Warnf("Failed to broadcast bond txn (%v). Tx bytes: %x", bond.SignedTx)
	} else if !bytes.Equal(bond.CoinID, bondCoinCast) {
		c.log.Warnf("Broadcasted bond %v; was expecting %v!",
			coinIDString(bond.AssetID, bondCoinCast), bondCoinStr)
	}

	c.updateAssetBalance(bond.AssetID)

	// Start waiting for reqConfs.
	details := fmt.Sprintf("Waiting for %d confirmations to post bond %v (%s) to %s",
		reqConfs, bondCoinStr, unbip(bond.AssetID), dc.acct.host)
	c.notify(newBondPostNoteWithConfirmations(TopicBondConfirming, string(TopicBondConfirming),
		details, db.Success, bond.AssetID, 0, dc.acct.host))
	// Set up the coin waiter, which watches confirmations so the user knows
	// when to expect their account to be marked paid by the server.
	c.monitorBondConfs(dc, bond, reqConfs)

	return &PostBondResult{BondID: bondCoinStr, ReqConfirms: uint16(reqConfs)}, nil
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
			if len(bond.RefundTx) > 0 || len(bond.PrivKey) > 0 {
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
