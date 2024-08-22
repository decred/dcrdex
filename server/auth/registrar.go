// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
)

var (
	// The coin waiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the AuthManager will wait for a coin
	// waiter. This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = 2 * time.Minute
)

// bondKey creates a unique map key for a bond by its asset ID and coin ID.
func bondKey(assetID uint32, coinID []byte) string {
	return string(append(encode.Uint32Bytes(assetID), coinID...))
}

func (auth *AuthManager) registerBondWaiter(key string) bool {
	auth.bondWaiterMtx.Lock()
	defer auth.bondWaiterMtx.Unlock()
	if _, found := auth.bondWaiterIdx[key]; found {
		return false
	}
	auth.bondWaiterIdx[key] = struct{}{}
	return true
}

func (auth *AuthManager) removeBondWaiter(key string) {
	auth.bondWaiterMtx.Lock()
	delete(auth.bondWaiterIdx, key)
	auth.bondWaiterMtx.Unlock()
}

// handlePreValidateBond handles the 'prevalidatebond' request.
//
// The request payload includes the user's account public key and the serialized
// bond post transaction itself (not just the txid).
//
// The parseBondTx function is used to validate the transaction, and extract
// bond details (amount and lock time) and the account ID to which it commits.
// This also checks that the account commitment corresponds to the user's public
// key provided in the payload. If these requirements are satisfied, the client
// will receive a PreValidateBondResult in the response. The user should then
// proceed to broadcast the bond and use the 'postbond' route once it reaches
// the required number of confirmations.
func (auth *AuthManager) handlePreValidateBond(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	preBond := new(msgjson.PreValidateBond)
	err := msg.Unmarshal(&preBond)
	if err != nil || preBond == nil {
		return msgjson.NewError(msgjson.BondError, "error parsing prevalidatebond request")
	}

	assetID := preBond.AssetID
	bondAsset, ok := auth.bondAssets[assetID]
	if !ok {
		return msgjson.NewError(msgjson.BondError, "%s does not support bonds", dex.BipIDSymbol(assetID))
	}

	// Create an account.Account from the provided pubkey.
	acct, err := account.NewAccountFromPubKey(preBond.AcctPubKey)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "error parsing account pubkey: %v", err)
	}
	acctID := acct.ID

	// Authenticate the message for the supposed account.
	sigMsg := preBond.Serialize()
	err = checkSigS256(sigMsg, preBond.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	// A bond's lockTime must be after bondExpiry from now.
	lockTimeThresh := time.Now().Add(auth.bondExpiry)

	// Decode raw tx, check fee output (0) and account commitment output (1).
	bondCoinID, amt, lockTime, commitAcct, err :=
		auth.parseBondTx(assetID, preBond.Version, preBond.RawTx /*, postBond.Data*/)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction: %v", err)
	}
	if amt < int64(bondAsset.Amt) {
		return msgjson.NewError(msgjson.BondError, "insufficient bond amount %d, needed %d", amt, bondAsset.Amt)
	}
	if lockTime < lockTimeThresh.Unix() {
		return msgjson.NewError(msgjson.BondError, "insufficient lock time %d, needed at least %d", lockTime, lockTimeThresh.Unix())
	}

	// Must be equal to account ID computed from pubkey in the PayFee message.
	if commitAcct != acctID {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction - account commitment does not match pubkey")
	}

	bondStr := coinIDString(assetID, bondCoinID)
	bondAssetSym := dex.BipIDSymbol(assetID)
	log.Debugf("Validated prospective bond txn output %s (%s) paying %d for user %v",
		bondStr, bondAssetSym, amt, acctID)

	expireTime := time.Unix(lockTime, 0).Add(-auth.bondExpiry)
	preBondRes := &msgjson.PreValidateBondResult{
		AccountID: acctID[:],
		AssetID:   assetID,
		Amount:    uint64(amt),
		Expiry:    uint64(expireTime.Unix()),
	}
	preBondRes.SetSig(auth.SignMsg(append(preBondRes.Serialize(), preBond.RawTx...)))

	resp, err := msgjson.NewResponse(msg.ID, preBondRes, nil)
	if err != nil { // shouldn't be possible
		return msgjson.NewError(msgjson.RPCInternalError, "internal encoding error")
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("Error sending prevalidatebond result to user %v: %v", acctID, err)
		if err = auth.Send(acctID, resp); err != nil {
			log.Warnf("Error sending prevalidatebond result to account %v: %v", acctID, err)
		}
	}
	return nil
}

// handlePostBond handles the 'postbond' request.
//
// The checkBond function is used to locate the bond transaction on the network,
// and verify the amount, lockTime, and account to which it commits.
//
// A 'postbond' request should not be made until the bond transaction has been
// broadcasted and reaches the required number of confirmations.
func (auth *AuthManager) handlePostBond(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	postBond := new(msgjson.PostBond)

	err := msg.Unmarshal(&postBond)
	if err != nil || postBond == nil {
		return msgjson.NewError(msgjson.BondError, "error parsing postbond request")
	}

	assetID := postBond.AssetID
	bondAsset, ok := auth.bondAssets[assetID]
	if !ok && assetID != account.PrepaidBondID {
		return msgjson.NewError(msgjson.BondError, "%s does not support bonds", dex.BipIDSymbol(assetID))
	}

	// Create an account.Account from the provided pubkey.
	acct, err := account.NewAccountFromPubKey(postBond.AcctPubKey)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "error parsing account pubkey: %v", err)
	}
	acctID := acct.ID

	// Authenticate the message for the supposed account.
	sigMsg := postBond.Serialize()
	err = checkSigS256(sigMsg, postBond.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	if assetID == account.PrepaidBondID {
		return auth.processPrepaidBond(conn, msg, acct, postBond.CoinID)
	}

	// A bond's lockTime must be after bondExpiry from now.
	lockTimeThresh := time.Now().Add(auth.bondExpiry)

	bondVer, bondCoinID := postBond.Version, postBond.CoinID
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	amt, lockTime, confs, commitAcct, err := auth.checkBond(ctx, assetID, bondVer, bondCoinID)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction: %v", err)
	}
	if amt < int64(bondAsset.Amt) {
		return msgjson.NewError(msgjson.BondError, "insufficient bond amount %d, needed %d", amt, bondAsset.Amt)
	}
	if lockTime < lockTimeThresh.Unix() {
		return msgjson.NewError(msgjson.BondError, "insufficient lock time %d, needed at least %d", lockTime, lockTimeThresh.Unix())
	}

	// Must be equal to account ID computed from pubkey in the PayFee message.
	if commitAcct != acctID {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction - account commitment does not match pubkey")
	}

	strength := uint32(uint64(amt) / bondAsset.Amt)

	// All good. The client gets a PostBondResult (no error) unless the confirms
	// check has an unexpected error or times out.
	expireTime := time.Unix(lockTime, 0).Add(-auth.bondExpiry)
	postBondRes := &msgjson.PostBondResult{
		AccountID:  acctID[:],
		AssetID:    assetID,
		Amount:     uint64(amt),
		Expiry:     uint64(expireTime.Unix()),
		Strength:   strength,
		BondID:     bondCoinID,
		Reputation: auth.ComputeUserReputation(acctID),
	}
	auth.Sign(postBondRes)

	sendResp := func() *msgjson.Error {
		resp, err := msgjson.NewResponse(msg.ID, postBondRes, nil)
		if err != nil { // shouldn't be possible
			return msgjson.NewError(msgjson.RPCInternalError, "internal encoding error")
		}
		err = conn.Send(resp)
		if err != nil {
			log.Warnf("Error sending postbond result to user %v: %v", acctID, err)
			if err = auth.Send(acctID, resp); err != nil {
				log.Warnf("Error sending postbond result to account %v: %v", acctID, err)
				// The user will need to 'connect' to reconcile bond status.
			}
		}
		return nil
	}

	// See if the account exists, and get known unexpired bonds. Also see if the
	// account has previously paid a legacy registration fee.
	dbAcct, bonds := auth.storage.Account(acctID, lockTimeThresh)

	bondStr := coinIDString(assetID, bondCoinID)
	bondAssetSym := dex.BipIDSymbol(assetID)

	// See if we already have this bond in DB.
	for _, bond := range bonds {
		if bond.AssetID == assetID && bytes.Equal(bond.CoinID, bondCoinID) {
			log.Debugf("Found existing bond %s (%s) committing %d for user %v",
				bondStr, bondAssetSym, amt, acctID)
			return sendResp()
		}
	}

	dbBond := &db.Bond{
		Version:  postBond.Version,
		AssetID:  assetID,
		CoinID:   bondCoinID,
		Amount:   amt,
		Strength: strength,
		LockTime: lockTime,
	}

	// Either store the bond or start a block waiter to activate the bond and
	// respond with a PostBondResult when it is fully-confirmed.
	bondIDKey := bondKey(assetID, bondCoinID)
	if !auth.registerBondWaiter(bondIDKey) {
		// Waiter already running! They'll get a response to their first
		// request, or find out on connect if the bond was activated.
		return msgjson.NewError(msgjson.BondAlreadyConfirmingError, "bond already submitted")
	}

	newAcct := dbAcct == nil
	reqConfs := int64(bondAsset.Confs)

	if confs >= reqConfs {
		// No need to call checkFee again in a waiter.
		log.Debugf("Activating new bond %s (%s) committing %d for user %v", bondStr, bondAssetSym, amt, acctID)
		auth.storeBondAndRespond(conn, dbBond, acct, newAcct, msg.ID, postBondRes)
		auth.removeBondWaiter(bondIDKey) // after storing it
		return nil
	}

	// The user should have submitted only when the bond was confirmed, so we
	// only expect to wait for asset network latency.
	log.Debugf("Found new bond %s (%s) committing %d for user %v. Confirming...",
		bondStr, bondAssetSym, amt, acctID)
	ctxTry, cancelTry := context.WithTimeout(context.Background(), txWaitExpiration) // prevent checkBond RPC hangs
	auth.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(txWaitExpiration),
		TryFunc: func() wait.TryDirective {
			res := auth.waitBondConfs(ctxTry, conn, dbBond, acct, reqConfs, newAcct, msg.ID, postBondRes)
			if res == wait.DontTryAgain {
				auth.removeBondWaiter(bondIDKey)
				cancelTry()
			}
			return res
		},
		ExpireFunc: func() {
			auth.removeBondWaiter(bondIDKey)
			cancelTry()
			// User may retry postbond periodically or on reconnect.
		},
	})
	// NOTE: server restart cannot restart these waiters, so user must resubmit
	// their postbond after their request times out.

	return nil
}

func (auth *AuthManager) storeBondAndRespond(conn comms.Link, bond *db.Bond, acct *account.Account,
	newAcct bool, reqID uint64, postBondRes *msgjson.PostBondResult) {
	acctID := acct.ID
	assetID, coinID := bond.AssetID, bond.CoinID
	bondStr := coinIDString(assetID, coinID)
	bondAssetSym := dex.BipIDSymbol(assetID)
	var err error
	if newAcct {
		log.Infof("Creating new user account %v from %v, posted first bond in %v (%s)",
			acctID, conn.Addr(), bondStr, bondAssetSym)
		err = auth.storage.CreateAccountWithBond(acct, bond)
	} else {
		log.Infof("Adding bond for existing user account %v from %v, with bond in %v (%s)",
			acctID, conn.Addr(), bondStr, bondAssetSym)
		err = auth.storage.AddBond(acct.ID, bond)
	}
	if err != nil {
		log.Errorf("Failure while storing bond for acct %v (new = %v): %v", acct, newAcct, err)
		conn.SendError(reqID, &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to store bond",
		})
		return
	}

	// Integrate active bonds and score to report tier.
	rep := auth.addBond(acctID, bond)
	if rep == nil { // user not authenticated, use DB
		rep = auth.ComputeUserReputation(acctID)
	}
	postBondRes.Reputation = rep

	log.Infof("Bond accepted: acct %v from %v locked %d in %v. Bond total %d, tier %d",
		acctID, conn.Addr(), bond.Amount, coinIDString(bond.AssetID, coinID), rep.BondedTier, rep.EffectiveTier())

	// Respond
	resp, err := msgjson.NewResponse(reqID, postBondRes, nil)
	if err != nil { // shouldn't be possible
		return
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("Error sending prepaid bond result to user %v: %v", acctID, err)
		if err = auth.Send(acctID, resp); err != nil {
			log.Warnf("Error sending feepaid notification to account %v: %v", acctID, err)
			// The user will need to either 'connect' to see confirmed status,
			// or postbond again. If they reconnected before it was confirmed,
			// they must retry postbond until it confirms and is added to the DB
			// with their new account.
		}
	}
}

func (auth *AuthManager) processPrepaidBond(conn comms.Link, msg *msgjson.Message, acct *account.Account, coinID []byte) *msgjson.Error {
	auth.prepaidBondMtx.Lock()
	defer auth.prepaidBondMtx.Unlock()
	strength, lockTimeI, err := auth.storage.FetchPrepaidBond(coinID)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "unknown or already spent pre-paid bond: %v", err)
	}

	lockTime := time.Unix(lockTimeI, 0)
	expireTime := lockTime.Add(-auth.bondExpiry)
	if time.Until(expireTime) < time.Hour*24 {
		return msgjson.NewError(msgjson.BondError, "pre-paid bond is too old")
	}

	postBondRes := &msgjson.PostBondResult{
		AccountID:  acct.ID[:],
		AssetID:    account.PrepaidBondID,
		Amount:     0,
		Strength:   strength,
		Expiry:     uint64(expireTime.Unix()),
		BondID:     coinID,
		Reputation: auth.ComputeUserReputation(acct.ID),
	}
	auth.Sign(postBondRes)

	lockTimeThresh := time.Now().Add(auth.bondExpiry)
	dbAcct, _ := auth.storage.Account(acct.ID, lockTimeThresh)

	dbBond := &db.Bond{
		AssetID:  account.PrepaidBondID,
		CoinID:   coinID,
		Strength: strength,
		LockTime: lockTimeI,
	}

	newAcct := dbAcct == nil
	if newAcct {
		log.Infof("Creating new user account %s from pre-paid bond. addr = %s", acct.ID, conn.Addr())
		err = auth.storage.CreateAccountWithBond(acct, dbBond)
	} else {
		log.Infof("Adding pre-bond for existing user account %v, addr = %s", acct.ID, conn.Addr())
		err = auth.storage.AddBond(acct.ID, dbBond)
	}
	if err != nil {
		log.Errorf("Failure while storing pre-paid bond for acct %v (new = %v): %v", acct.ID, newAcct, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to store pre-paid bond",
		}
	}

	if err := auth.storage.DeletePrepaidBond(coinID); err != nil {
		log.Errorf("Error deleting pre-paid bond id = %s from database: %v", dex.Bytes(coinID), err)
	}

	rep := auth.addBond(acct.ID, dbBond)
	if rep == nil { // user not authenticated, use DB
		rep = auth.ComputeUserReputation(acct.ID)
	}
	postBondRes.Reputation = rep

	log.Infof("Pre-paid bond accepted: acct %v from %v. Bonded tier %d, effective tier %d",
		acct.ID, conn.Addr(), rep.BondedTier, rep.EffectiveTier())

	resp, err := msgjson.NewResponse(msg.ID, postBondRes, nil)
	if err != nil { // shouldn't be possible
		return nil
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("Error sending pre-paid bond result to user %v: %v", acct.ID, err)
		if err = auth.Send(acct.ID, resp); err != nil {
			log.Warnf("Error sending pre-paid notification to account %v: %v", acct.ID, err)
		}
	}
	return nil
}

// waitBondConfs is a coin waiter that should be started after validating a bond
// transaction in the postbond request handler. This waits for the transaction
// output referenced by coinID to reach reqConfs, and then re-validates the
// amount and address to which the coinID pays. If the checks pass, the account
// is marked as paid in storage by saving the coinID for the accountID. Finally,
// a FeePaidNotification is sent to the provided conn. In case the notification
// fails to send (e.g. connection no longer active), the user should check paid
// status on 'connect'.
func (auth *AuthManager) waitBondConfs(ctx context.Context, conn comms.Link, bond *db.Bond, acct *account.Account,
	reqConfs int64, newAcct bool, reqID uint64, postBondRes *msgjson.PostBondResult) wait.TryDirective {
	assetID, coinID := bond.AssetID, bond.CoinID
	amt, _, confs, _, err := auth.checkBond(ctx, assetID, bond.Version, coinID)
	if err != nil {
		// This is unexpected because we already validated everything, so
		// hopefully this is a transient failure such as RPC connectivity.
		log.Warnf("Unexpected error checking bond coin: %v", err)
		return wait.TryAgain
	}
	if confs < reqConfs {
		return wait.TryAgain
	}
	acctID := acct.ID

	// Verify the bond amount as a spot check. This should be redundant with the
	// parseBondTx checks. If it disagrees, there is a bug in the fee asset
	// backend, and the operator will need to intervene.
	if amt != bond.Amount {
		log.Errorf("checkFee: account %v fee coin %x pays %d; expected %d",
			acctID, coinID, amt, bond.Amount)
		return wait.DontTryAgain
	}

	// Store and respond
	log.Debugf("Activating new bond %s (%s) committing %d for user %v",
		coinIDString(assetID, coinID), dex.BipIDSymbol(assetID), amt, acctID)
	auth.storeBondAndRespond(conn, bond, acct, newAcct, reqID, postBondRes)

	return wait.DontTryAgain
}

// coinNotFound sends an error response for a coin not found.
func (auth *AuthManager) coinNotFound(acctID account.AccountID, msgID uint64, coinID []byte) {
	resp, err := msgjson.NewResponse(msgID, nil, &msgjson.Error{
		Code:    msgjson.TransactionUndiscovered,
		Message: fmt.Sprintf("failed to find transaction %x", coinID),
	})
	if err != nil {
		log.Error("NewResponse error in (Swapper).loop: %v", err)
	}
	if err := auth.Send(acctID, resp); err != nil {
		log.Infof("Failed to send coin-not-found error to user %s: %v", acctID, err)
	}
}
