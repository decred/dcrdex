// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
)

var (
	// The coin waiters will query for transaction data every recheckInterval.
	// TODO: increase this duration when notifyfee is removed and all clients do
	// payfee instead since only the former are expected to be at feeConfs
	// initially; with payfee they start unconfirmed.
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the AuthManager will wait for a coin
	// waiter. This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute
)

const regFeeAssetID = 42 // DCR, TODO: make AuthManager field

// handlePreRegister handles the 'preregister' request. A fee address is
// obtained from the pool, and returned along with the fee amount and the DEX
// pubkey in a PreRegisterResult. The user should then generate a fee payment
// transaction according to server/asset/dcr.ParseFeeTx and provide it via the
// 'payfee' request.
func (auth *AuthManager) handlePreRegister(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	preReg := new(msgjson.PreRegister)
	err := json.Unmarshal(msg.Payload, &preReg)
	if err != nil || preReg == nil {
		return msgjson.NewError(msgjson.FeeError, "error parsing preregister request")
	}

	if preReg.AssetID != regFeeAssetID {
		// TODO: handle multiple fee assets
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "unsupported registration fee asset",
		}
	}

	feeAddr := auth.feeAddrPool.Get()
	if feeAddr == "" {
		log.Errorf("Failed to acquire new fee address.")
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}

	log.Infof("Sending fee address %v (asset %d) and amount %v to client from %v",
		feeAddr, regFeeAssetID, auth.regFee, conn.Addr())

	regRes := &msgjson.PreRegisterResult{
		DEXPubKey: auth.signer.PubKey().SerializeCompressed(),
		AssetID:   regFeeAssetID,
		Address:   feeAddr,
		Fee:       auth.regFee,
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

func (auth *AuthManager) feeWaiterExists(acctID account.AccountID) bool {
	auth.feeWaiterMtx.Lock()
	defer auth.feeWaiterMtx.Unlock()
	_, found := auth.feeWaiterIdx[acctID]
	return found
}

func (auth *AuthManager) registerFeeWaiter(acctID account.AccountID) bool {
	auth.feeWaiterMtx.Lock()
	defer auth.feeWaiterMtx.Unlock()
	if _, found := auth.feeWaiterIdx[acctID]; found {
		return false
	}
	auth.feeWaiterIdx[acctID] = struct{}{}
	return true
}

func (auth *AuthManager) removeFeeWaiter(acctID account.AccountID) {
	auth.feeWaiterMtx.Lock()
	delete(auth.feeWaiterIdx, acctID)
	auth.feeWaiterMtx.Unlock()
}

// handlePayFee handles the 'payfee' request, which follows a 'preregister'
// request. The request payload includes the user's account public key and the
// serialized fee payment transaction itself (not just the txid). The
// AuthManager's parseFeeTx function (a field) is used to perform basic
// validation of the transaction, and extract the used fee address and account
// ID to which it commits. handlePayFee then checks that the fee address is
// owned by the feeAddrPool, and that the account to which the transaction
// commits corresponds to the user's public key provided in the PayFee payload.
// If these requirements are satisfied, the transaction is broadcasted with the
// AuthManager's sendFee function. If the transaction is accepted by the asset
// backend (should be a fully-validating node), the fee address is "returned" to
// the address pool so it will not be issued in future preregister request, and
// the user's account is created in the database. Finally, a fee waiter
// goroutine is launched to wait for the transaction to reach feeConfs. The fee
// waiter uses the same check function (the validateFee method) as used in the
// legacy notifyfee protocol.
func (auth *AuthManager) handlePayFee(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	payFee := new(msgjson.PayFee)
	err := json.Unmarshal(msg.Payload, &payFee)
	if err != nil || payFee == nil {
		return msgjson.NewError(msgjson.FeeError, "error parsing payfee request")
	}

	if payFee.AssetID != regFeeAssetID {
		// TODO: handle multiple fee assets
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "unsupported registration fee asset",
		}
	}

	// Create account.Account from pubkey.
	acct, err := account.NewAccountFromPubKey(payFee.PubKey)
	if err != nil {
		return msgjson.NewError(msgjson.FeeError, "error parsing account pubkey: "+err.Error())
	}
	acctID := acct.ID

	// Check signature first to ensure this message is from the account owner.
	sigMsg := payFee.Serialize()
	err = checkSigS256(sigMsg, payFee.SigBytes(), acct.PubKey)
	if err != nil {
		return msgjson.NewError(msgjson.FeeError, "signature error: "+err.Error())
	}

	// Stop if the account already exists or is already paid.
	if dbAcct, paid, _, _ := auth.storage.Account(acctID); dbAcct != nil {
		if paid {
			// if !confirmed && !auth.feeWaiterExists(acctID) { /* start over with new tx? */ }
			return &msgjson.Error{
				// Client should recognize this code and mark their account paid
				// (TODO client-side).
				Code:    msgjson.AccountExistsError,
				Message: "account already exists",
			}
			// Instead, server could issue positive response again if we could
			// verify the account was created with same data.
		}

		// The account would only exist if they already used payfee or the old
		// 'register' route, which should not be mixed with payfee.
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "existing unpaid account is invalid with payfee (want notify_fee?)",
		}
	}

	// decode raw tx, check fee output (0) and account commitment output (1)
	feeAddr, commitAcct, err := auth.parseFeeTx(payFee.RawFeeTx, int64(auth.regFee))
	if err != nil {
		return msgjson.NewError(msgjson.FeeError, "invalid fee transaction: %v", err)
	}

	// Must be equal to account ID computed from pubkey in the PayFee message.
	if commitAcct != acctID {
		return msgjson.NewError(msgjson.FeeError, "invalid fee transaction - account commitment does not match pubkey")
	}

	if !auth.feeAddrPool.Owns(feeAddr) {
		return msgjson.NewError(msgjson.FeeError, "invalid fee transaction - invalid fee address")
	}

	// Broadcast the txn.
	log.Infof("Broadcasting fee transaction %v for account %v", payFee.RawFeeTx, acctID)
	feeCoin, err := auth.sendFee(payFee.RawFeeTx)
	if err != nil {
		return msgjson.NewError(msgjson.FeeError, "invalid fee transaction: %v", err.Error())
	}

	auth.feeAddrPool.Used(feeAddr)

	log.Infof("Created new user account %v from %v with fee address %v, paid in %x",
		acctID, conn.Addr(), feeAddr, feeCoin)

	// Register account. When the tx is fully confirmed, storage.PayAccount
	// should be called to store the feeCoin (see waitFeeConfs).
	err = auth.storage.CreateAccountWithTx(acct, feeAddr, payFee.RawFeeTx)
	if err != nil {
		log.Debugf("CreateAccount(%v) failed: %v", acct, err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "failed to create new account (already registered?)",
		}
	}

	// Respond with a PayFeeResult.
	payFeeRes := &msgjson.PayFeeResult{FeeCoin: feeCoin}
	auth.Sign(payFee) // signature in our response is our signature of their request
	payFeeRes.SetSig(payFee.SigBytes())
	resp, err := msgjson.NewResponse(msg.ID, payFeeRes, nil)
	if err != nil { // shouldn't be possible
		return msgjson.NewError(msgjson.RPCInternalError, "internal encoding error")
	}

	err = conn.Send(resp)
	if err != nil {
		// If client attempts payfee again, they should recognize the
		// AccountExistsError error and mark their account paid.
		log.Warnf("error sending payfee result to link: %v", err)
	}

	// Start waiter, which marks the account paid by storing the fee coin ID.
	if !auth.registerFeeWaiter(acctID) {
		// This should be impossible since payfee prevents existing accounts
		// from getting here.
		return msgjson.NewError(msgjson.FeeWaiterRunningError,
			"already waiting for your fee transaction to reach the required confirmations")
	}

	// TODO: Startup of AuthManager should recreate waiters for accounts with a
	// raw tx recorded but not yet paid.

	auth.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(24 * time.Hour), // waiting for auth.feeConfs
		TryFunc: func() bool {
			res := auth.waitFeeConfs(conn, feeCoin, acctID, feeAddr)
			if res == wait.DontTryAgain {
				auth.removeFeeWaiter(acctID)
			}
			return res
		},
		ExpireFunc: func() {
			auth.removeFeeWaiter(acctID)
			auth.coinNotFound(acctID, msg.ID, feeCoin)
		},
	})
	return nil
}

// waitFeeConfs is a coin waiter that should be started after validating and
// broadcasting a fee payment transaction in the payfee request handler. This
// waits for the transaction output referenced by coinID to reach auth.feeConfs,
// and then re-validates the amount and address to which the coinID pays. If the
// checks pass, the account is marked as paid in storage by saving the coinID
// for the accountID. Finally, a FeePaidNotification is sent to the provided
// conn. In case the notification fails to send (e.g. connection no longer
// active), the user should check paid status on 'connect'.
func (auth *AuthManager) waitFeeConfs(conn comms.Link, coinID []byte, acctID account.AccountID, regAddr string) bool {
	addr, val, confs, err := auth.checkFee(coinID)
	if err != nil || confs < auth.feeConfs {
		return wait.TryAgain // TODO: consider other errors that should fail instead, e.g. errcoindecode
	}

	// Verify the fee amount and address. This should be redundant for payfee
	// requests, but it is possible that checkFee could disagree. If these
	// happen there is a bug in the fee asset backend, and the operator will
	// need to intervene.
	if val < auth.regFee {
		log.Errorf("checkFee: account %v fee coin %x pays %d; expected %d",
			acctID, coinID, val, auth.regFee)
		return wait.DontTryAgain
	}
	if addr != regAddr {
		log.Errorf("checkFee: account %v fee coin %x pays to %s; %s",
			acctID, coinID, addr, regAddr)
		return wait.DontTryAgain
	}

	// Mark the account as paid by storing the coin ID.
	err = auth.storage.PayAccount(acctID, coinID)
	if err != nil {
		log.Errorf("Failed to mark account %v as paid with coin %x", acctID, coinID)
		return wait.DontTryAgain
	}
	if client := auth.user(acctID); client != nil {
		client.confirm()
	}

	log.Infof("New user registered: acct %v from %v paid %d to %v", acctID, conn.Addr(), val, addr)

	// PayFeeResult was sent immediately after broadcast. Now we notify that the
	// tranaction has reached feeConfs, but client can watch the confirmation
	// count theirself.
	feePaidNtfn := &msgjson.FeePaidNotification{AccountID: acctID[:]}
	auth.Sign(feePaidNtfn)
	resp, err := msgjson.NewNotification(msgjson.FeePaidRoute, feePaidNtfn)
	if err != nil {
		log.Error("FeePaidRoute encoding error: %v", err)
		return wait.DontTryAgain
	}

	// First attempt to send the feepaid notification on the provided link. If
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
	err := json.Unmarshal(msg.Payload, &register)
	if err != nil || register == nil {
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
		log.Debugf("CreateAccount(%v) failed: %v", acct, err)
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
	err := json.Unmarshal(msg.Payload, &notifyFee)
	if err != nil || notifyFee == nil {
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

	acct, paid, confirmed, open := auth.storage.Account(acctID)
	if acct == nil {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "no account found for ID " + notifyFee.AccountID.String(),
		}
	}
	if !open {
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "account closed and cannot be reopen",
		}
	}
	if paid {
		if !confirmed {
			// they shouldn't be using notify_fee, but maybe we could stat waitFeeConfs
			return &msgjson.Error{Code: msgjson.FeeError, Message: "route not applicable"}
		}
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
// indicator that the account is paid. This is done for backward compatibility
// and may change a future DB scheme.
//
// DEPRECATED
func (auth *AuthManager) validateFee(conn comms.Link, acctID account.AccountID, clientReq msgjson.Signable, msgID uint64, coinID []byte, regAddr string) bool {
	addr, val, confs, err := auth.checkFee(coinID)
	if err != nil || confs < auth.feeConfs {
		return wait.TryAgain // TODO: consider other errors that should fail instead, e.g. errcoindecode
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

	// Verify the fee amount and address.
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

	// Mark the account as paid by storing the coin ID.
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
	auth.Sign(clientReq)
	notifyRes := new(msgjson.NotifyFeeResult)
	notifyRes.SetSig(clientReq.SigBytes())
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
