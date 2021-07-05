// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"bytes"
	"crypto/sha256"
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

	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
)

var (
	// The coin waiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the AuthManager will wait for a coin
	// waiter. This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute
)

const regFeeAssetID = 42 // DCR, TODO: make AuthManager field

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

// handlePostBond handles the 'postbond' request.
//
// The request payload includes the user's account public key and the serialized
// bond post transaction itself (not just the txid), the bond output's redeem
// script, and a signature of the account ID with the key to which the bond
// redeem script pays to demonstrate ownership of the bond by the account.
//
// The parseBondTx function is used to validate the transaction, and extract
// bond details (amount, locktime, pubkey, etc.) and the account ID to which it
// commits. This also checks that the account commitment corresponds to the
// user's public key provided in the payload. If these requirements are
// satisfied, the client will receive a PostBondResult in the response.
//
// Once the raw transaction is validated, the relevant asset node is used to
// locate the transaction on the network.
//
//   - If it is not found, a PostBondResult is sent immediately with
//     Confs=-1 set.  The bond is NOT stored in the DB. This was a courtesy
//     pre-validation. The user should then broadcast the transaction, wait for
//     the required number of confirmations, and send 'postbond' again.
//   - If the located transaction has the required number of confirmations,
//     the bond is stored in the DB as active, and a response is sent.
//   - Otherwise, a response is sent with the observed confirmation count, and a
//     coin waiter is started to watch for the txn to reach the required number
//     of confirmations. The bond is stored in DB as pending. When the
//     transaction reaches the required confirmations, it is flagged active in
//     the DB and a 'bondconfirmed' notification is sent.
//
// NOTE: This presently only supports Decred bonds.
func (auth *AuthManager) handlePostBond(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	postBond := new(msgjson.PostBond)
	err := msg.Unmarshal(&postBond)
	if err != nil || postBond == nil {
		return msgjson.NewError(msgjson.BondError, "error parsing postbond request")
	}

	// TODO: allow different assets for bond, switching parse functions, etc.
	assetID := postBond.AssetID
	if assetID != regFeeAssetID {
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

	// A bond's lockTime must be after bonExpiry from now.
	lockTimeThresh := time.Now().Add(auth.bondExpiry)

	// Decode raw tx, check fee output (0) and account commitment output (1).
	bondCoinID, amt, bondAddr, bondPK, lockTime, commitAcct, err := auth.parseBondTx(postBond.BondTx, postBond.BondScript) // i.e. dcr.ParseBondTx presently, but should switch on asset
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction: %v", err)
	}
	if amt < int64(auth.minBond) {
		return msgjson.NewError(msgjson.BondError, "insufficient bond amount %d, needed %d", amt, auth.minBond)
	}
	if lockTime < lockTimeThresh.Unix() {
		return msgjson.NewError(msgjson.BondError, "insufficient lock time %d, needed at least %d", lockTime, lockTimeThresh.Unix())
	}

	// Must be equal to account ID computed from pubkey in the PayFee message.
	if commitAcct != acctID {
		return msgjson.NewError(msgjson.BondError, "invalid bond transaction - account commitment does not match pubkey")
	}

	// Verify that the user has the private keys to the bond pubkey by verifying
	// the BondSig against the message (acct ID) and bond script's pubkey hash.
	msgHash := sha256.Sum256(acctID[:])
	sigPK, _, err := ecdsa.RecoverCompact(postBond.BondSig, msgHash[:])
	if err != nil {
		return msgjson.NewError(msgjson.BondError, "invalid bond signature: %v", err)
	}
	if !bytes.Equal(bondPK, sigPK.SerializeCompressed()) {
		return msgjson.NewError(msgjson.BondError, "bond signature does not match bond pubkey")
	}

	// All good. The client gets a PostBondResult (no error) unless the confirms
	// check has an unexpected error.
	expireTime := time.Unix(lockTime, 0).Add(-auth.bondExpiry)
	postBondRes := &msgjson.PostBondResult{
		AssetID: assetID,
		Amt:     uint64(amt),
		Expiry:  expireTime.Unix(),
		BondID:  bondCoinID,
		// Confs is set below.
	}
	auth.Sign(postBondRes)

	// See if the account exists, and get known unexpired bonds. Also see if the
	// account has previously paid a legacy registration fee.
	dbAcct, bonds, legacyFeePaid := auth.storage.Account(acctID, lockTimeThresh)

	// See if we already have this bond in DB. We still process the request
	// regardless, but this changes how we proceed.
	var dbBond *db.Bond
	for _, bond := range bonds {
		if bond.AssetID == assetID && bytes.Equal(bond.CoinID, bondCoinID) {
			dbBond = bond
			break
		}
	}
	// if dbBond != nil && !dbBond.Pending { /* respond now with dummy confs = reqConfs to save RPCs? */ }

	bondStr := coinIDString(assetID, bondCoinID)
	bondAssetSym := dex.BipIDSymbol(assetID)

	// Determine if it is pending (found, unconfirmed, fully-confirmed, etc.):
	// - check confirmations e.g. gettxout
	// - if tx not found, no error, just confs=-1, respond / break, no DB store (just pre-validate unsigned bond tw)
	// - store bond info in DB (updating pending flag if already stored)
	// - if confs >= required, mark bond active and adjust acct tier, no waiter
	// - if confs < required, start waiter
	chkAddr, chkAmt, confs, err := auth.checkFee(bondCoinID)
	if err != nil {
		// Not found is expected if this is a pre-broadcast validation request.
		if !errors.Is(err, asset.CoinNotFoundError) {
			return msgjson.NewError(msgjson.BondError, "unable to check bond with node: %v", err)
		}

		// OK. Respond with confs=-1. They may now broadcast, wait for the
		// required confirms, and postbond again.
		log.Debugf("Validated prospective bond txn output %s (%s) paying %d for user %v", bondStr, bondAssetSym, amt, acctID)
		postBondRes.Confs = -1
		resp, err := msgjson.NewResponse(msg.ID, postBondRes, nil)
		if err != nil { // shouldn't be possible
			return msgjson.NewError(msgjson.RPCInternalError, "internal encoding error")
		}
		err = conn.Send(resp)
		if err != nil {
			log.Warnf("error sending postbond result to link: %v", err)
		}
		return nil
	}

	if int64(chkAmt) != amt {
		return msgjson.NewError(msgjson.BondError, "node reports incorrect bond amount: %d != %d", chkAmt, amt)
	}

	if chkAddr != bondAddr {
		return msgjson.NewError(msgjson.BondError, "node reports incorrect bond address: %d != %d", chkAddr, bondAddr)
	}

	reqConfs := auth.feeConfs // will be from bondassets config (TODO)
	pending := confs < reqConfs

	// Only store if this is a new bond. If existing, just respond and possibly
	// start waiting for confirmations and/or flag as active in DB.
	if dbBond == nil {
		dbBond = &db.Bond{
			AssetID:  assetID,
			CoinID:   bondCoinID,
			Script:   postBond.BondScript,
			Amount:   amt,
			LockTime: lockTime,
			Pending:  pending,
		}

		// The user may be posting their initial bond (new account) or topping up.
		newAcct := dbAcct == nil
		if newAcct {
			log.Infof("Creating new user account %v from %v, posted first bond in %v (%s) / %v",
				acctID, conn.Addr(), bondStr, bondAssetSym, bondAddr)
			err = auth.storage.CreateAccountWithBond(acct, dbBond)
		} else {
			log.Infof("Adding bond for existing user account %v from %v, with bond in %v (%s) / %v",
				acctID, conn.Addr(), bondStr, bondAssetSym, bondAddr)
			err = auth.storage.AddBond(acct.ID, dbBond)
		}
		if err != nil {
			log.Errorf("Failure while storing bond for acct %v (new = %v): %v", acct, newAcct, err)
			return &msgjson.Error{
				Code:    msgjson.RPCInternalError,
				Message: "failed to store bond",
			}
		}
	} else if dbBond.Pending && !pending {
		// If the bond is fully confirmed, and it is not already in the DB as
		// confirmed, mark the bond active before sending response.
		log.Infof("Activating bond %s (%s) for acct %v", bondStr, bondAssetSym, acct)
		dbBond.Pending = false
		err = auth.storage.ActivateBond(acctID, assetID, bondCoinID)
		if err != nil {
			log.Errorf("Activating bond for acct %v failed: %v", acct, err)
			return &msgjson.Error{
				Code:    msgjson.RPCInternalError,
				Message: "failed to store bond",
			}
		}
	}

	if !pending {
		// Update client info if they are already authenticated.
		auth.addBond(acctID, dbBond) // idempotent

		if legacyFeePaid && postBond.LegacyFeeRefundAddr != "" {
			log.Warnf("Account %v posting bond requests a legacy fee refund to %v",
				acctID, postBond.LegacyFeeRefundAddr) // signature checked above
			// To actually do this, the operator would need to manually send funds
			// to the address, then use an admin endpoint to somehow delete the old
			// fee coin ID from the table.
		}
	}

	// Respond with the actual confirmation count and user tier.
	postBondRes.Confs = confs
	postBondRes.Tier, _ = auth.ComputeUserTier(acct.ID) // integrate score and active bond amounts
	resp, err := msgjson.NewResponse(msg.ID, postBondRes, nil)
	if err != nil { // shouldn't be possible
		return msgjson.NewError(msgjson.RPCInternalError, "internal encoding error")
	}
	err = conn.Send(resp)
	if err != nil {
		// Client should attempt postbond again.
		log.Warnf("Error sending postbond result to user %v: %v", acctID, err)
	}

	if !pending {
		// No need it start a waiter since the bond is already active.
		return nil
	}

	// Start a block waiter to activate the bond when it is fully-confirmed.
	bondIDKey := bondKey(assetID, bondCoinID)
	if !auth.registerBondWaiter(bondIDKey) {
		// Waiter already running, and this postbond response already sent.
		return nil
	}

	// TODO: Startup of AuthManager could recreate waiters for pending bonds,
	// otherwise clients will have to resubmit their postbond request.

	auth.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(time.Hour),
		TryFunc: func() bool {
			res := auth.waitBondConfs(conn, dbBond, acctID, reqConfs)
			if res == wait.DontTryAgain {
				auth.removeBondWaiter(bondIDKey)
			}
			return res
		},
		ExpireFunc: func() {
			auth.removeBondWaiter(bondIDKey)
			// User may retry postbond periodically or on reconnect.
		},
	})

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
func (auth *AuthManager) waitBondConfs(conn comms.Link, bond *db.Bond, acctID account.AccountID, reqConfs int64) bool {
	coinID := bond.CoinID
	addr, val, confs, err := auth.checkFee(coinID) // e.g. FeeCoin
	if err != nil {
		// This is unexpected because we we already validated everything, so
		// hopefully this is a transient failure such as RPC connectivity.
		log.Warnf("Unexpected error checking bond coin: %v", err)
		return wait.TryAgain
	}
	if confs < reqConfs {
		return wait.TryAgain
	}

	// Verify the bond amount as a spot check. This should be redundant with the
	// parseBondTx checks. If it disagrees, there is a bug in the fee asset
	// backend, and the operator will need to intervene.
	if int64(val) != bond.Amount {
		log.Errorf("checkFee: account %v fee coin %x pays %d; expected %d",
			acctID, coinID, val, bond.Amount)
		return wait.DontTryAgain
	}

	// Update bond status in storage.
	err = auth.storage.ActivateBond(acctID, bond.AssetID, coinID)
	if err != nil {
		log.Errorf("Failed to mark account %v as paid with coin %x", acctID, coinID)
		return wait.DontTryAgain
	}

	// Integrate active bonds and score to report tier.
	bondTotal, tier := auth.addBond(acctID, bond)
	if bondTotal == -1 { // user not authenticated, use DB
		tier, bondTotal = auth.ComputeUserTier(acctID)
	}

	log.Infof("Bond accepted: acct %v from %v locked %d in %v (%v). Bond total %d, tier %d",
		acctID, conn.Addr(), val, coinIDString(bond.AssetID, coinID), addr, bondTotal, tier)

	// PostBondResult was sent immediately after PostBond was handled. Now we
	// notify that the tranaction has reached reqConfs, but client can watch the
	// confirmation count theirself.
	bondConfNtfn := &msgjson.BondConfirmedNotification{
		AssetID:    bond.AssetID,
		BondCoinID: coinID,
		AccountID:  acctID[:],
		Tier:       tier,
	}
	auth.Sign(bondConfNtfn)
	resp, err := msgjson.NewNotification(msgjson.BondConfirmedRoute, bondConfNtfn)
	if err != nil {
		log.Error("BondConfirmedRoute encoding error: %v", err)
		return wait.DontTryAgain // they will have to discover their tier and active bonds on reconnect
	}

	// First attempt to send the bondconfirmed notification on the provided link. If
	// it is down, attempt to send to via the auth manager by account ID.
	err = conn.Send(resp)
	if err != nil {
		// The original link may be lost. See if they have an authenticated
		// connection.
		if err = auth.Send(acctID, resp); err != nil {
			log.Warnf("Error sending feepaid notification to account %v: %v", acctID, err)
			// The user will need to 'connect' to see confirmed status.
		}
	}

	return wait.DontTryAgain
}

// handleRegister handles requests to the 'register' route.
//
// DEPRECATED
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

	// Register account and get a fee payment address.
	feeAddr, err := auth.storage.CreateAccount(acct)
	if err != nil {
		log.Debugf("CreateAccount(%s) failed: %v", acct.ID, err)
		var archiveErr *db.ArchiveError
		if errors.As(err, &archiveErr) {
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
			Message: "failed to create new account (already registered?)",
		}
	}

	log.Infof("Created new user account %v from %v with fee address %v", acct.ID, conn.Addr(), feeAddr)

	// Prepare, sign, and send response.
	regRes := &msgjson.RegisterResult{
		DEXPubKey:    auth.signer.PubKey().SerializeCompressed(),
		ClientPubKey: register.PubKey,
		Address:      feeAddr,
		Fee:          auth.regFee,
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

// handleNotifyFee handles requests to the 'notifyfee' route.
//
// DEPRECATED
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

	acct, _, paid := auth.storage.Account(acctID, time.Now()) // don't need bonds
	if acct == nil {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "no account found for ID " + notifyFee.AccountID.String(),
		}
	}
	if paid {
		return &msgjson.Error{
			// Client should recognize this code and mark their account paid
			// (TODO client-side).
			Code:    msgjson.AccountExistsError,
			Message: "'notifyfee' sent for paid account",
		}
	}

	// Check signature
	sigMsg := notifyFee.Serialize()
	err = checkSigS256(sigMsg, notifyFee.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
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
	regAddr, err := auth.storage.AccountRegAddr(acctID)
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
			res := auth.validateFee(conn, acctID, notifyFee, msg.ID, notifyFee.CoinID, regAddr)
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
// DEPRECATED
func (auth *AuthManager) validateFee(conn comms.Link, acctID account.AccountID, notifyFee *msgjson.NotifyFee, msgID uint64, coinID []byte, regAddr string) bool {
	addr, val, confs, err := auth.checkFee(coinID)
	if err != nil || confs < auth.feeConfs {
		if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
			log.Warnf("Unexpected error checking fee coin confirmations: %v", err)
			// return wait.DontTryAgain // maybe
		}
		return wait.TryAgain
	}
	var msgErr *msgjson.Error
	defer func() {
		if msgErr != nil {
			resp, err := msgjson.NewResponse(msgID, nil, msgErr)
			if err != nil {
				log.Errorf("error encoding notifyfee error response: %v", err)
				return
			}
			err = conn.Send(resp)
			if err != nil {
				log.Warnf("error sending notifyfee result to link: %v", err)
			}
		}
	}()
	if val < auth.regFee {
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
