// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
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
	txWaitExpiration = time.Minute
)

// handleRegister handles requests to the 'register' route.
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
			Time:         encode.UnixMilliU(unixMsNow()),
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
		if errors.As(err, &archiveErr) {
			// These cases should have been caught above.
			switch archiveErr.Code {
			case db.ErrAccountExists:
				return &msgjson.Error{
					Code:    msgjson.AccountExistsError,
					Message: archiveErr.Detail, // Fee coin
				}
			case db.ErrAccountSuspended:
				return &msgjson.Error{
					Code:    msgjson.AccountSuspendedError,
					Message: "account exists and is suspended",
				}
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
	if ai.BrokenRule != account.NoRule {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "account closed and cannot be reopened",
		}
	}

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
		TryFunc: func() bool {
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

// validateFee is a coin waiter that validates a client's notifyFee request and
// responds with an Acknowledgement.
func (auth *AuthManager) validateFee(conn comms.Link, msgID uint64, acctID account.AccountID,
	notifyFee *msgjson.NotifyFee, assetID uint32, regAddr string) bool {
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
