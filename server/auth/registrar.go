// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
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
		return msgjson.NewError(msgjson.BondError, "only DCR bonds supported presently")
	}

	// Create an account.Account from the provided pubkey.
	acct, err := account.NewAccountFromPubKey(preBond.AcctPubKey)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "error parsing account pubkey: "+err.Error())
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
	if !ok {
		return msgjson.NewError(msgjson.BondError, "only DCR bonds supported presently")
	}

	// Create an account.Account from the provided pubkey.
	acct, err := account.NewAccountFromPubKey(postBond.AcctPubKey)
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "error parsing account pubkey: "+err.Error())
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

	// All good. The client gets a PostBondResult (no error) unless the confirms
	// check has an unexpected error or times out.
	expireTime := time.Unix(lockTime, 0).Add(-auth.bondExpiry)
	postBondRes := &msgjson.PostBondResult{
		AccountID: acctID[:],
		AssetID:   assetID,
		Amount:    uint64(amt),
		Expiry:    uint64(expireTime.Unix()),
		BondID:    bondCoinID,
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
	dbAcct, bonds, _, _ := auth.storage.Account(acctID, lockTimeThresh)

	bondStr := coinIDString(assetID, bondCoinID)
	bondAssetSym := dex.BipIDSymbol(assetID)

	// See if we already have this bond in DB.
	for _, bond := range bonds {
		if bond.AssetID == assetID && bytes.Equal(bond.CoinID, bondCoinID) {
			log.Debugf("Found existing bond %s (%s) committing %d for user %v",
				bondStr, bondAssetSym, amt, acctID)
			_, postBondRes.Tier = auth.AcctStatus(acctID)
			return sendResp()
		}
	}

	dbBond := &db.Bond{
		Version:  postBond.Version,
		AssetID:  assetID,
		CoinID:   bondCoinID,
		Amount:   amt,
		Strength: uint32(uint64(amt) / bondAsset.Amt),
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
	bondTotal, tier := auth.addBond(acctID, bond)
	if bondTotal == -1 { // user not authenticated, use DB
		tier, bondTotal = auth.ComputeUserTier(acctID)
	}
	postBondRes.Tier = tier

	log.Infof("Bond accepted: acct %v from %v locked %d in %v. Bond total %d, tier %d",
		acctID, conn.Addr(), bond.Amount, coinIDString(bond.AssetID, coinID), bondTotal, tier)

	// Respond
	resp, err := msgjson.NewResponse(reqID, postBondRes, nil)
	if err != nil { // shouldn't be possible
		return
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("Error sending postbond result to user %v: %v", acctID, err)
		if err = auth.Send(acctID, resp); err != nil {
			log.Warnf("Error sending feepaid notification to account %v: %v", acctID, err)
			// The user will need to either 'connect' to see confirmed status,
			// or postbond again. If they reconnected before it was confirmed,
			// they must retry postbond until it confirms and is added to the DB
			// with their new account.
		}
	}
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

// handleRegister handles requests to the 'register' route.
//
// DEPRECATED (V0PURGE)
func (auth *AuthManager) handleRegister(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	register := new(msgjson.Register)
	err := msg.Unmarshal(&register)
	if err != nil || register == nil /* null payload */ {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing register request",
		}
	}

	// Create account.Account from pubkey.
	acct, err := account.NewAccountFromPubKey(register.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.PubKeyParseError,
			Message: "error parsing pubkey: " + err.Error(),
		}
	}

	// Check signature.
	sigMsg := register.Serialize()
	err = checkSigS256(sigMsg, register.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	sendRegRes := func(feeAsset uint32, feeAddr string, feeAmt uint64) *msgjson.Error {
		// Prepare, sign, and send response.
		regRes := &msgjson.RegisterResult{
			DEXPubKey:    auth.signer.PubKey().SerializeCompressed(),
			ClientPubKey: register.PubKey, // only for serialization and signature, not sent to client
			AssetID:      &feeAsset,
			Address:      feeAddr,
			Fee:          feeAmt,
			Time:         uint64(time.Now().UnixMilli()),
		}
		auth.Sign(regRes)

		resp, err := msgjson.NewResponse(msg.ID, regRes, nil)
		if err != nil {
			log.Errorf("error creating new response for registration result: %v", err)
			return &msgjson.Error{
				Code:    msgjson.RPCInternalError,
				Message: "internal error",
			}
		}

		err = conn.Send(resp)
		if err != nil {
			log.Warnf("Error sending register result to link: %v", err)
		}
		return nil
	}

	regAsset := uint32(42)
	if register.Asset != nil {
		regAsset = *register.Asset
	}

	// See if the account already exists. If it is paid, just respond with
	// AccountExistsError and the known fee coin. If it exists but is not paid,
	// resend the RegisterResult with the previously recorded fee address and
	// asset ID. Note that this presently does not permit a user to change from
	// the initially requested asset to another.
	ai, err := auth.storage.AccountInfo(acct.ID)
	if err == nil {
		if len(ai.FeeCoin) > 0 {
			if _, tier := auth.AcctStatus(acct.ID); tier < 1 {
				return &msgjson.Error{
					Code:    msgjson.AccountSuspendedError,
					Message: "account exists and is suspended", // "suspended" - they could upgrade and post bond instead
				}
			}
			return &msgjson.Error{
				Code:    msgjson.AccountExistsError,
				Message: ai.FeeCoin.String(), // Fee coin TODO: supplement with asset ID
			}
		}
		// Resend RegisterResult with the existing fee address.
		feeAsset := auth.feeAssets[ai.FeeAsset]
		if feeAsset == nil || feeAsset.Amt == 0 || ai.FeeAddress == "" ||
			regAsset != ai.FeeAsset /* sanity */ {
			// NOTE: Allowing register.Asset != ai.FeeAsset would require
			// clearing this account's previously recorded fee address.
			return &msgjson.Error{
				Code:    msgjson.RPCInternalError,
				Message: "asset not supported for registration",
			}
		}
		return sendRegRes(ai.FeeAsset, ai.FeeAddress, feeAsset.Amt)
	}
	if !db.IsErrAccountUnknown(err) {
		log.Errorf("AccountInfo error for ID %s: %v", acct.ID, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to create new account",
		}
	} // else account unknown, create it

	// Try to get a fee address and amount for the requested asset.
	feeAsset := auth.feeAssets[regAsset]
	feeAddr := auth.feeAddress(regAsset)
	if feeAsset == nil || feeAddr == "" {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "asset not supported for registration",
		}
	}

	// Register the new account with the fee address.
	if err = auth.storage.CreateAccount(acct, regAsset, feeAddr); err != nil {
		log.Debugf("CreateAccount(%s) failed: %v", acct.ID, err)
		var archiveErr db.ArchiveError // CreateAccount returns by value.
		if errors.As(err, &archiveErr) && archiveErr.Code == db.ErrAccountExists {
			// The account exists case would have been caught above.
			if _, tier := auth.AcctStatus(acct.ID); tier < 1 {
				return &msgjson.Error{
					Code:    msgjson.AccountSuspendedError,
					Message: "account exists and is suspended",
				}
			}
			return &msgjson.Error{
				Code:    msgjson.AccountExistsError,
				Message: archiveErr.Detail, // Fee coin, if it was paid that way
			}
		}
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to create new account",
		}
	}

	log.Infof("Created new user account %v from %v with fee address %v (%v)",
		acct.ID, conn.Addr(), feeAddr, dex.BipIDSymbol(regAsset))
	return sendRegRes(regAsset, feeAddr, feeAsset.Amt)
}

// handleNotifyFee handles requests to the 'notifyfee' route.
//
// DEPRECATED (V0PURGE)
func (auth *AuthManager) handleNotifyFee(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	notifyFee := new(msgjson.NotifyFee)
	err := msg.Unmarshal(&notifyFee)
	if err != nil || notifyFee == nil /* null payload */ {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing notifyfee request",
		}
	}

	// Get account information.
	if len(notifyFee.AccountID) != account.HashSize {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "invalid account ID: " + notifyFee.AccountID.String(),
		}
	}

	var acctID account.AccountID
	copy(acctID[:], notifyFee.AccountID)

	ai, err := auth.storage.AccountInfo(acctID)
	if ai == nil || err != nil {
		if err != nil && !db.IsErrAccountUnknown(err) {
			log.Warnf("Unexpected AccountInfo(%v) failure: %v", acctID, err)
		}
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "no account found for ID " + notifyFee.AccountID.String(),
		}
	}
	// TODO: check tier and respond differently if <1?
	// if ai.BrokenRule != account.NoRule {
	// 	return &msgjson.Error{
	// 		Code:    msgjson.AuthenticationError,
	// 		Message: "account closed and cannot be reopened",
	// 	}
	// }

	// Check signature
	sigMsg := notifyFee.Serialize()
	acct, err := account.NewAccountFromPubKey(ai.Pubkey)
	if err != nil {
		// Shouldn't happen.
		log.Warnf("Pubkey decode failure for %v: %v", acctID, err)
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "pubkey decode failure",
		}
	}
	err = checkSigS256(sigMsg, notifyFee.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	if len(ai.FeeCoin) > 0 {
		// No need to search for txn, and storage.PayAccount would fail to store
		// the Coin again, so just respond if fee coin matches.
		if !notifyFee.CoinID.Equal(ai.FeeCoin) {
			return &msgjson.Error{
				Code:    msgjson.FeeError,
				Message: "invalid fee coin",
			}
		}
		// Account ID and fee coin are good, sign the request and respond.
		auth.Sign(notifyFee)
		notifyRes := new(msgjson.NotifyFeeResult)
		notifyRes.SetSig(notifyFee.SigBytes())
		resp, err := msgjson.NewResponse(msg.ID, notifyRes, nil)
		if err != nil {
			return &msgjson.Error{
				Code:    msgjson.RPCInternalError,
				Message: "internal encoding error",
			}
		}
		if err = conn.Send(resp); err != nil {
			log.Warnf("error sending notifyfee result to link: %v", err)
		}
		return nil
	}

	auth.feeWaiterMtx.Lock()
	if _, found := auth.feeWaiterIdx[acctID]; found {
		auth.feeWaiterMtx.Unlock() // cannot defer since first TryFunc is synchronous and may lock
		return &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "already looking for your fee coin, try again later",
		}
	}

	// Get the registration fee address assigned to the client's account.
	regAddr, assetID, err := auth.storage.AccountRegAddr(acctID)
	if err != nil {
		auth.feeWaiterMtx.Unlock()
		log.Infof("AccountRegAddr failed to load info for account %v: %v", acctID, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "error locating account info",
		}
	}

	auth.feeWaiterIdx[acctID] = struct{}{}
	auth.feeWaiterMtx.Unlock()

	removeWaiter := func() {
		auth.feeWaiterMtx.Lock()
		delete(auth.feeWaiterIdx, acctID)
		auth.feeWaiterMtx.Unlock()
	}

	auth.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(txWaitExpiration),
		TryFunc: func() wait.TryDirective {
			res := auth.validateFee(conn, msg.ID, acctID, notifyFee, assetID, regAddr)
			if res == wait.DontTryAgain {
				removeWaiter()
			}
			return res
		},
		ExpireFunc: func() {
			removeWaiter()
			auth.coinNotFound(acctID, msg.ID, notifyFee.CoinID)
		},
	})
	return nil
}

// validateFee is a coin waiter that validates a client's notifyfee request and
// responds with an Acknowledgement when it reaches the required number of
// confirmations (feeConfs). The database's PayAccount method is used to store
// the coinID of the fully-confirmed fee transaction, which is the current
// indicator that the account is paid with a legacy registration fee.
//
// DEPRECATED (V0PURGE)
func (auth *AuthManager) validateFee(conn comms.Link, msgID uint64, acctID account.AccountID,
	notifyFee *msgjson.NotifyFee, assetID uint32, regAddr string) wait.TryDirective {
	// If there is a problem, respond with an error.
	var msgErr *msgjson.Error
	defer func() {
		if msgErr == nil {
			return
		}
		resp, err := msgjson.NewResponse(msgID, nil, msgErr)
		if err != nil {
			log.Errorf("error encoding notifyfee error response: %v", err)
			return
		}
		err = conn.Send(resp)
		if err != nil {
			log.Warnf("error sending notifyfee error response: %v", err)
		}
	}()

	// Required confirmations and amount.
	feeAsset := auth.feeAssets[assetID]
	if feeAsset == nil { // shouldn't be possible
		msgErr = &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "unsupported asset",
		}
		return wait.DontTryAgain
	}

	coinID := notifyFee.CoinID
	addr, val, confs, err := auth.checkFee(assetID, coinID)
	if err != nil {
		if !errors.Is(err, asset.CoinNotFoundError) {
			log.Warnf("Unexpected error checking fee coin confirmations: %v", err)
			// return wait.DontTryAgain // maybe
		}
		return wait.TryAgain
	}
	if confs < int64(feeAsset.Confs) {
		return wait.TryAgain
	}

	if val < feeAsset.Amt {
		msgErr = &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "fee too low",
		}
		return wait.DontTryAgain
	}
	if addr != regAddr {
		msgErr = &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "wrong fee address. wanted " + regAddr,
		}
		return wait.DontTryAgain
	}

	// Mark the account as paid
	err = auth.storage.PayAccount(acctID, coinID)
	if err != nil {
		log.Errorf("Failed to mark account %v as paid with coin %x", acctID, coinID)
		msgErr = &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
		return wait.DontTryAgain
	}

	log.Infof("New user registered: acct %v from %v paid %d to %v", acctID, conn.Addr(), val, addr)

	// Create, sign, and send the the response.
	auth.Sign(notifyFee)
	notifyRes := new(msgjson.NotifyFeeResult)
	notifyRes.SetSig(notifyFee.SigBytes())
	resp, err := msgjson.NewResponse(msgID, notifyRes, nil)
	if err != nil {
		msgErr = &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal encoding error",
		}
		return wait.DontTryAgain
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("error sending notifyfee result to link: %v", err)
	}
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
