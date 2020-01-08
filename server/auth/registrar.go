// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"encoding/json"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
)

// handleRegister handles requests to the 'register' route.
func (auth *AuthManager) handleRegister(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	register := new(msgjson.Register)
	err := json.Unmarshal(msg.Payload, &register)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing register: " + err.Error(),
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
	sigMsg, err := register.Serialize()
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error serializing register: " + err.Error(),
		}
	}
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
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "storage error: " + err.Error(),
		}
	}

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
		log.Errorf("error serializing register result: %v", err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}

	resp, err := msgjson.NewResponse(comms.NextID(), regRes, nil)
	if err != nil {
		log.Errorf("error creating new response for registration result: %v", err)
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal error",
		}
	}

	err = conn.Send(resp)
	if err != nil {
		log.Warnf("error sending register result to link: %v", err)
	}

	return nil
}

// handleNotifyFee handles requests to the 'notifyfee' route.
func (auth *AuthManager) handleNotifyFee(conn comms.Link, msg *msgjson.Message) *msgjson.Error {
	// Unmarshal.
	notifyFee := new(msgjson.NotifyFee)
	err := json.Unmarshal(msg.Payload, &notifyFee)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error parsing notifyfee: " + err.Error(),
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
	sigMsg, err := notifyFee.Serialize()
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: "error serializing notifyfee: " + err.Error(),
		}
	}
	err = checkSigS256(sigMsg, notifyFee.SigBytes(), acct.PubKey)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.SignatureError,
			Message: "signature error: " + err.Error(),
		}
	}

	// Get the registration fee address assigned to the client's account.
	regAddr, err := auth.storage.AccountRegAddr(acctID)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "error locating account info: " + err.Error(),
		}
	}

	// Validate fee.
	addr, val, confs, err := auth.checkFee(notifyFee.CoinID)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "error getting fee info: " + err.Error(),
		}
	}
	if val < auth.regFee {
		return &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "fee too low",
		}
	}
	if confs < auth.feeConfs {
		return &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "too few confirmations",
		}
	}
	if addr != regAddr {
		return &msgjson.Error{
			Code:    msgjson.FeeError,
			Message: "wrong fee address. wanted " + regAddr + " got " + addr,
		}
	}

	// Mark the account as paid
	err = auth.storage.PayAccount(acctID, notifyFee.CoinID)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "wrong fee address. wanted " + regAddr + " got " + addr,
		}
	}

	// Create, sign, and send the the response.
	err = auth.Sign(notifyFee)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal signature error",
		}
	}
	notifyRes := new(msgjson.NotifyFeeResult)
	notifyRes.SetSig(notifyFee.SigBytes())
	resp, err := msgjson.NewResponse(msg.ID, notifyRes, nil)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "internal encoding error",
		}
	}
	err = conn.Send(resp)
	if err != nil {
		log.Warnf("error sending notifyfee result to link: %v", err)
	}

	return nil
}
