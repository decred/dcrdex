// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"golang.org/x/crypto/ripemd160"
)

const (
	secretSize = 32
)

// atomicSwapContract returns an output script that may be redeemed by one of
// two signature scripts:
//
//   <their sig> <their pubkey> <initiator secret> 1
//
//   <my sig> <my pubkey> 0
//
// The first signature script is the normal redemption path done by the other
// party and requires the initiator's secret.  The second signature script is
// the refund path performed by us, but the refund can only be performed after
// locktime.
func atomicSwapContract(pkhMe, pkhThem *[ripemd160.Size]byte, locktime int64, secretHash []byte) ([]byte, error) {
	b := txscript.NewScriptBuilder()

	b.AddOp(txscript.OP_IF) // Normal redeem path
	{
		// Require initiator's secret to be a known length that the redeeming
		// party can audit.  This is used to prevent fraud attacks between two
		// currencies that have different maximum data sizes.
		b.AddOp(txscript.OP_SIZE)
		b.AddInt64(secretSize)
		b.AddOp(txscript.OP_EQUALVERIFY)

		// Require initiator's secret to be known to redeem the output.
		b.AddOp(txscript.OP_SHA256)
		b.AddData(secretHash)
		b.AddOp(txscript.OP_EQUALVERIFY)

		// Verify their signature is being used to redeem the output.  This
		// would normally end with OP_EQUALVERIFY OP_CHECKSIG but this has been
		// moved outside of the branch to save a couple bytes.
		b.AddOp(txscript.OP_DUP)
		b.AddOp(txscript.OP_HASH160)
		b.AddData(pkhThem[:])
	}
	b.AddOp(txscript.OP_ELSE) // Refund path
	{
		// Verify locktime and drop it off the stack (which is not done by
		// CLTV).
		b.AddInt64(locktime)
		b.AddOp(txscript.OP_CHECKLOCKTIMEVERIFY)
		b.AddOp(txscript.OP_DROP)

		// Verify our signature is being used to redeem the output.  This would
		// normally end with OP_EQUALVERIFY OP_CHECKSIG but this has been moved
		// outside of the branch to save a couple bytes.
		b.AddOp(txscript.OP_DUP)
		b.AddOp(txscript.OP_HASH160)
		b.AddData(pkhMe[:])
	}
	b.AddOp(txscript.OP_ENDIF)

	// Complete the signature check.
	b.AddOp(txscript.OP_EQUALVERIFY)
	b.AddOp(txscript.OP_CHECKSIG)

	return b.Script()
}

// redeemP2SHContract returns the signature script to redeem a contract output
// using the redeemer's signature and the initiator's secret.  This function
// assumes P2SH and appends the contract as the final data push.
func redeemP2SHContract(contract, sig, pubkey, secret []byte) ([]byte, error) {
	b := txscript.NewScriptBuilder()
	b.AddData(sig)
	b.AddData(pubkey)
	b.AddData(secret)
	b.AddInt64(1)
	b.AddData(contract)
	return b.Script()
}

// refundP2SHContract returns the signature script to refund a contract output
// using the contract author's signature after the locktime has been reached.
// This function assumes P2SH and appends the contract as the final data push.
func refundP2SHContract(contract, sig, pubkey []byte) ([]byte, error) {
	b := txscript.NewScriptBuilder()
	b.AddData(sig)
	b.AddData(pubkey)
	b.AddInt64(0)
	b.AddData(contract)
	return b.Script()
}

// sha254Hash returns a sha256 hash of the provided bytes.
func sha256Hash(x []byte) []byte {
	h := sha256.Sum256(x)
	return h[:]
}

// newSecret generated a random 32-byte secret.
func newSecret() ([]byte, error) {
	var secret [secretSize]byte
	_, err := rand.Read(secret[:])
	if err != nil {
		return nil, err
	}

	return secret[:], nil
}

// generateContractP2SHPkScript creates a P2SHPk script of the provided contract.
func generateContractP2SHPkScript(contract []byte, params *chaincfg.Params) ([]byte, error) {
	contractP2SH, err := dcrutil.NewAddressScriptHash(contract, params)
	if err != nil {
		return nil, err
	}

	contractP2SHPkScript, err := txscript.PayToAddrScript(contractP2SH)
	if err != nil {
		return nil, err
	}

	return contractP2SHPkScript, nil
}

// Contract represents the swap contract details.
type Contract struct {
	Secret           []byte
	RedeemAddr       dcrutil.Address
	CounterPartyAddr dcrutil.Address
	ContractData     []byte
	LockTime         uint32
}

// newContract creates a swap contract from the provided address and lock time.
func newContract(redeemAddr string, counterpartyAddr string, lockTime uint32, params *chaincfg.Params) (*Contract, error) {
	if uint32(lockTime) > wire.MaxTxInSequenceNum {
		return nil, fmt.Errorf("lockTime out of range")
	}

	secret, err := newSecret()
	if err != nil {
		return nil, err
	}

	addrMe, err := dcrutil.DecodeAddress(redeemAddr, params)
	if err != nil {
		return nil, err
	}

	addrThem, err := dcrutil.DecodeAddress(counterpartyAddr, params)
	if err != nil {
		return nil, err
	}

	secretHash := sha256Hash(secret)

	contract, err := atomicSwapContract(addrMe.Hash160(), addrThem.Hash160(),
		int64(lockTime), secretHash)
	if err != nil {
		return nil, err
	}

	return &Contract{
		Secret:           secret,
		RedeemAddr:       addrMe,
		CounterPartyAddr: addrThem,
		ContractData:     contract,
		LockTime:         lockTime,
	}, nil
}

// generateContracts create contracts from the provided redeem and counterparty addresses.
func generateContracts(redeemAddrs []string, counterpartyAddrs []string, lockTime uint32, params *chaincfg.Params) ([]*Contract, error) {
	if len(counterpartyAddrs) == 0 {
		return nil, fmt.Errorf("no counterparty addresses provided")
	}

	if len(redeemAddrs) == 0 {
		return nil, fmt.Errorf("no redeem addresses provided")
	}

	if len(redeemAddrs) != len(counterpartyAddrs) {
		return nil, fmt.Errorf("counterparty addresses size should be equal" +
			" to the redeem addresses size")
	}

	contracts := make([]*Contract, len(redeemAddrs))
	for idx, addr := range redeemAddrs {
		ctpAddr := counterpartyAddrs[idx]
		contract, err := newContract(addr, ctpAddr, lockTime, params)
		if err != nil {
			return nil, fmt.Errorf("unable to create contract: %v", err)
		}

		contracts[idx] = contract
	}

	return contracts, nil
}
