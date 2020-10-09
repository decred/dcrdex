// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"context"
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
	recheckInterval = time.Second * 5
	// txWaitExpiration is the longest the AuthManager will wait for a coin
	// waiter. This could be thought of as the maximum allowable backend latency.
	txWaitExpiration = time.Minute
)

// handleRegister handles requests to the 'register' route.
func (auth *AuthManager) handleRegister(_ context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	register := new(msgjson.Register)
	err := json.Unmarshal(msg.Payload, &register)
	if err != nil {
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

	log.Infof("Created new user account %v with fee address %v", acct.ID, feeAddr)

	// Prepare, sign, and send response.
	regRes := &msgjson.RegisterResult{
		DEXPubKey:    auth.signer.PubKey().SerializeCompressed(),
		ClientPubKey: register.PubKey,
		Address:      feeAddr,
		Fee:          auth.regFee,
		Time:         encode.UnixMilliU((unixMsNow())),
	}

	err = auth.Sign(regRes)
	if err != nil {
		log.Errorf("error serializing register result: %v, data = %#v", err, regRes)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}

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
func (auth *AuthManager) handleNotifyFee(ctx context.Context, conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	notifyFee := new(msgjson.NotifyFee)
	err := json.Unmarshal(msg.Payload, &notifyFee)
	if err != nil {
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

	acct, paid, open := auth.storage.Account(acctID)
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
		return &msgjson.Error{
			Code:    msgjson.AuthenticationError,
			Message: "'notifyfee' send for paid account",
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
			res := auth.validateFee(ctx, conn, acctID, notifyFee, msg.ID, notifyFee.CoinID, regAddr)
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
func (auth *AuthManager) validateFee(ctx context.Context, conn comms.Link, acctID account.AccountID, notifyFee *msgjson.NotifyFee, msgID uint64, coinID []byte, regAddr string) bool {
	addr, val, confs, err := auth.checkFee(ctx, coinID)
	if err != nil || confs < auth.feeConfs {
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

	log.Infof("New user registered: acct %v, paid %d to %v", acctID, val, addr)

	// Create, sign, and send the the response.
	err = auth.Sign(notifyFee)
	if err != nil {
		msgErr = &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal signature error",
		}
		return wait.DontTryAgain
	}
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
