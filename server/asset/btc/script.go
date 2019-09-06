// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
)

const (
	// SwapContractSize is the worst case scenario size for a swap contract,
	// which is the pk-script of the non-change output of an initialization
	// transaction as used in execution of an atomic swap.
	// See extractSwapAddresses for a breakdown of the bytes.
	SwapContractSize = 97
	// P2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	P2PKHSigScriptSize = 1 + 73 + 1 + 33
	// Overhead for a wire.TxIn. See wire.TxIn.SerializeSize.
	// hash 32 bytes + index 4 bytes + sequence 4 bytes + varint size for the
	// length of SignatureScript (varint assumed to be 1 byte here).
	txInOverhead = 32 + 4 + 4 + 1
	// derSigLength is the maximum length of a DER encoded signature.
	derSigLength = 73
	// initTxSize is the size of a standard serialized atomic swap initialization
	// transaction with one change output.
	// MsgTx overhead is 4 bytes version + 4 bytes locktime + 2 bytes of varints
	// for the number of transaction inputs and outputs (1 each here) + 1 P2PKH
	// input (25) + 2 outputs (8 + 1 bytes overhead each): 1 P2PKH script (25) +
	// 1 P2SH script (23).
	initTxSize = 4 + 4 + 1 + 1 + txInOverhead + P2PKHSigScriptSize + 8 + 1 + 25 + 8 + 1 + 23
)

type btcScriptType uint8

const (
	scriptP2PKH = 1 << iota
	scriptP2SH
	scriptTypeSegwit
	scriptMultiSig
	scriptUnsupported
)

const pubkeyLength = 33 // Length of a serialized compressed pubkey.

func (s btcScriptType) isP2SH() bool {
	return s&scriptP2SH != 0
}

func (s btcScriptType) isP2PKH() bool {
	return s&scriptP2PKH != 0
}

func (s btcScriptType) isSegwit() bool {
	return s&scriptTypeSegwit != 0
}

func (s btcScriptType) isMultiSig() bool {
	return s&scriptMultiSig != 0
}

// parseScriptType creates a dcrScriptType bitmap for the script type. A script
// type will be some combination of pay-to-pubkey-hash, pay-to-script-hash,
// and stake. If a script type is P2SH, it may or may not be mutli-sig.
func parseScriptType(pkScript, redeemScript []byte) btcScriptType {
	var scriptType btcScriptType
	class := txscript.GetScriptClass(pkScript)
	switch class {
	case txscript.PubKeyHashTy:
		scriptType |= scriptP2PKH
	case txscript.WitnessV0PubKeyHashTy:
		scriptType |= scriptP2PKH | scriptTypeSegwit
	case txscript.ScriptHashTy:
		scriptType |= scriptP2SH
	case txscript.WitnessV0ScriptHashTy:
		scriptType |= scriptP2SH | scriptTypeSegwit
	default:
		return scriptUnsupported
	}
	if scriptType.isP2SH() {
		scriptClass := txscript.GetScriptClass(redeemScript)
		if scriptClass == txscript.MultiSigTy {
			scriptType |= scriptMultiSig
		}
	}
	return scriptType
}

// extractPubKeyHash extracts the pubkey hash from the passed script if it is a
// /standard pay-to-pubkey-hash script.  It will return nil otherwise.
func extractPubKeyHash(script []byte) []byte {
	// A pay-to-pubkey-hash script is of the form:
	//  OP_DUP OP_HASH160 <20-byte hash> OP_EQUALVERIFY OP_CHECKSIG
	if len(script) == 25 &&
		script[0] == txscript.OP_DUP &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUALVERIFY &&
		script[24] == txscript.OP_CHECKSIG {

		return script[3:23]
	}

	return nil
}

// extractScriptHash extracts the script hash from the passed script if it is a
// standard pay-to-script-hash script.  It will return nil otherwise.
//
// NOTE: This function is only valid for version 0 opcodes.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func extractScriptHash(script []byte) []byte {
	// A pay-to-script-hash script is of the form:
	//  OP_HASH160 <20-byte scripthash> OP_EQUAL
	if len(script) == 23 &&
		script[0] == txscript.OP_HASH160 &&
		script[1] == txscript.OP_DATA_20 &&
		script[22] == txscript.OP_EQUAL {

		return script[2:22]
	}
	return nil
}

// extractWitnessScriptHash extracts the script hash from the passed script if
// it is a standard pay-to-script-hash script.  It will return nil otherwise.
//
// NOTE: This function is only valid for version 0 opcodes.  Since the function
// does not accept a script version, the results are undefined for other script
// versions.
func extractWitnessScriptHash(script []byte) []byte {
	// A pay-to-script-hash script is of the form:
	//  OP_0 OP_DATA_32 <32-byte scripthash> OP_EQUAL
	if len(script) == 34 &&
		script[0] == txscript.OP_0 &&
		script[1] == txscript.OP_DATA_32 {
		return script[2:34]
	}
	return nil
}

type btcScriptAddrs struct {
	pubkeys   []btcutil.Address
	numPK     int
	pkHashes  []btcutil.Address
	numPKH    int
	nRequired int
}

// Extract the addresses from the pubkey script, or the redeem script if the
// pubkey script is P2SH. Addresses can be of several types, but the types
// suppported will be pubkey
func extractScriptAddrs(script []byte, chainParams *chaincfg.Params) (*btcScriptAddrs, error) {
	pubkeys := make([]btcutil.Address, 0)
	pkHashes := make([]btcutil.Address, 0)
	// For P2SH and non-P2SH multi-sig, pull the addresses from the pubkey script.
	_, addrs, numRequired, err := txscript.ExtractPkScriptAddrs(script, chainParams)
	if err != nil {
		return nil, fmt.Errorf("extractScriptAddrs: %v", err)
	}
	for _, addr := range addrs {
		// If the address is an unhashed public key, is won't need a pubkey as part
		// of its sigScript, so count them separately.
		_, isPubkey := addr.(*btcutil.AddressPubKey)
		if isPubkey {
			pubkeys = append(pubkeys, addr)
		} else {
			pkHashes = append(pkHashes, addr)
		}
	}
	return &btcScriptAddrs{
		pubkeys:   pubkeys,
		numPK:     len(pubkeys),
		pkHashes:  pkHashes,
		numPKH:    len(pkHashes),
		nRequired: numRequired,
	}, nil
}

// Extract the sender and receiver addresses from a swap contract. If the
// provided script is not a swap contract, an error will be returned.
func extractSwapAddresses(pkScript []byte, chainParams *chaincfg.Params) (string, string, error) {
	// A swap redemption sigScript is <pubkey> <secret> and satisfies the
	// following swap contract.
	//
	// OP_IF
	//  OP_SIZE hashSize OP_EQUALVERIFY OP_SHA256 OP_DATA_32 secretHash OP_EQUALVERIFY OP_DUP OP_HASH160 OP_DATA20 pkHashReceiver
	//     1   +   2    +      1       +    1    +   1      +   32     +      1       +   1  +   1      +    1    +    20
	// OP_ELSE
	//  OP_DATA4 locktime OP_CHECKLOCKTIMEVERIFY OP_DROP OP_DUP OP_HASH160 OP_DATA_20 pkHashSender
	//     1    +    4   +           1          +   1   +  1   +    1     +   1      +    20
	// OP_ENDIF
	// OP_EQUALVERIFY
	// OP_CHECKSIG
	//
	// 5 bytes if-else-endif-equalverify-checksig
	// 1 + 2 + 1 + 1 + 1 + 32 + 1 + 1 + 1 + 1 + 20 = 62 bytes for redeem block
	// 1 + 4 + 1 + 1 + 1 + 1 + 1 + 20 = 30 bytes for refund block
	// 5 + 62 + 30 = 97 bytes
	if len(pkScript) != SwapContractSize {
		return "", "", fmt.Errorf("incorrect swap contract length. found %d, expected %d", SwapContractSize, len(pkScript))
	}

	if pkScript[0] == txscript.OP_IF &&
		pkScript[1] == txscript.OP_SIZE &&
		// secret key hash size (2 bytes)
		pkScript[4] == txscript.OP_EQUALVERIFY &&
		pkScript[5] == txscript.OP_SHA256 &&
		pkScript[6] == txscript.OP_DATA_32 &&
		// secretHash (32 bytes)
		pkScript[39] == txscript.OP_EQUALVERIFY &&
		pkScript[40] == txscript.OP_DUP &&
		pkScript[41] == txscript.OP_HASH160 &&
		pkScript[42] == txscript.OP_DATA_20 &&
		// receiver's pkh (20 bytes)
		pkScript[63] == txscript.OP_ELSE &&
		pkScript[64] == txscript.OP_DATA_4 &&
		// time (4 bytes)
		pkScript[69] == txscript.OP_CHECKLOCKTIMEVERIFY &&
		pkScript[70] == txscript.OP_DROP &&
		pkScript[71] == txscript.OP_DUP &&
		pkScript[72] == txscript.OP_HASH160 &&
		pkScript[73] == txscript.OP_DATA_20 &&
		// sender's pkh (20 bytes)
		pkScript[94] == txscript.OP_ENDIF &&
		pkScript[95] == txscript.OP_EQUALVERIFY &&
		pkScript[96] == txscript.OP_CHECKSIG {

		receiverAddr, err := btcutil.NewAddressPubKeyHash(pkScript[43:63], chainParams)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		senderAddr, err := btcutil.NewAddressPubKeyHash(pkScript[74:94], chainParams)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from sender's pubkey hash")
		}

		return senderAddr.String(), receiverAddr.String(), nil
	}
	return "", "", fmt.Errorf("invalid swap contract")
}

// checkSig checks that the message's signature was created with the
// private key for the provided public key.
func checkSig(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := btcec.ParsePubKey(pkBytes, btcec.S256())
	if err != nil {
		return fmt.Errorf("error decoding PublicKey from bytes: %v", err)
	}
	signature, err := btcec.ParseDERSignature(sigBytes, btcec.S256())
	if err != nil {
		return fmt.Errorf("error decoding Signature from bytes: %v", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}
