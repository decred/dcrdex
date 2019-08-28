// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"fmt"

	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
)

type dcrScriptType uint8

const (
	scriptP2PKH = 1 << iota
	scriptP2SH
	scriptStake
	scriptMultiSig
	scriptUnsupported
)

const pubkeyLength = 33 // Length of a serialized compressed pubkey.

func (s dcrScriptType) isP2SH() bool {
	return s&scriptP2SH != 0
}

func (s dcrScriptType) isP2PKH() bool {
	return s&scriptP2PKH != 0
}

func (s dcrScriptType) isStake() bool {
	return s&scriptStake != 0
}

// func (s dcrScriptType) isMultiSig() bool {
// 	return s&scriptMultiSig != 0
// }

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
	// All pubkey scripts are assumed to be version 0.
	currentScriptVersion = 0
	// Overhead for a wire.TxIn with a script length < 254.
	txInOverhead = 58
)

// parseScriptType creates a dcrScriptType bitmap for the script type. A script
// type will be some combination of pay-to-pubkey-hash, pay-to-script-hash,
// and stake. If a script type is P2SH, it may or may not be mutli-sig.
func parseScriptType(scriptVersion uint16, pkScript, redeemScript []byte) dcrScriptType {
	if scriptVersion != 0 {
		return scriptUnsupported
	}
	var scriptType dcrScriptType
	switch {
	case isPubKeyHashScript(pkScript):
		scriptType |= scriptP2PKH
	case isScriptHashScript(pkScript):
		scriptType |= scriptP2SH
	case isStakePubkeyHashScript(pkScript):
		scriptType |= scriptP2PKH | scriptStake
	case isStakeScriptHashScript(pkScript):
		scriptType |= scriptP2SH | scriptStake
	default:
		return scriptUnsupported
	}
	if scriptType.isP2SH() && txscript.IsMultisigScript(redeemScript) {
		scriptType |= scriptMultiSig
	}
	return scriptType
}

type dcrScriptAddrs struct {
	pubkeys   []dcrutil.Address
	numPK     int
	pkHashes  []dcrutil.Address
	numPKH    int
	nRequired int
}

// Extract the addresses from the pubkey script, or the redeem script if the
// pubkey script is P2SH. Addresses can be of several types, but the types
// suppported will be pubkey
func extractScriptAddrs(scriptType dcrScriptType, pkScript, redeemScript []byte) (*dcrScriptAddrs, error) {
	var err error
	var addrs []dcrutil.Address
	var numRequired int
	pubkeys := make([]dcrutil.Address, 0)
	pkHashes := make([]dcrutil.Address, 0)
	// For P2SH and non-P2SH multi-sig, pull the addresses from the pubkey script.
	if scriptType.isP2PKH() {
		_, addrs, numRequired, err = txscript.ExtractPkScriptAddrs(0, pkScript, chainParams)
	} else {
		_, addrs, numRequired, err = txscript.ExtractPkScriptAddrs(0, redeemScript, chainParams)
	}
	if err != nil {
		return nil, fmt.Errorf("extractScriptAddrs: %v", err)
	}
	for _, addr := range addrs {
		// If the address is an unhashed public key, is won't need a pubkey as part
		// of its sigScript, so count them separately.
		_, isPubkey := addr.(*dcrutil.AddressSecpPubKey)
		if isPubkey {
			pubkeys = append(pubkeys, addr)
		} else {
			pkHashes = append(pkHashes, addr)
		}
	}
	return &dcrScriptAddrs{
		pubkeys:   pubkeys,
		numPK:     len(pubkeys),
		pkHashes:  pkHashes,
		numPKH:    len(pkHashes),
		nRequired: numRequired,
	}, nil
}

// isPubKeyHashScript returns whether or not the passed script is a standard
// pay-to-pubkey-hash script.
func isPubKeyHashScript(script []byte) bool {
	return extractPubKeyHash(script) != nil
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

// Extract the sender and receiver addresses from a swap contract. If the
// provided script is not a swap contract, an error will be returned.
func extractSwapAddresses(pkScript []byte) (string, string, error) {
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
		return "", "", fmt.Errorf("incorrect swap contract length")
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

		receiverAddr, err := dcrutil.NewAddressPubKeyHash(pkScript[43:63], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from recipient's pubkey hash")
		}

		senderAddr, err := dcrutil.NewAddressPubKeyHash(pkScript[74:94], chainParams, dcrec.STEcdsaSecp256k1)
		if err != nil {
			return "", "", fmt.Errorf("error decoding address from sender's pubkey hash")
		}

		return senderAddr.String(), receiverAddr.String(), nil
	}
	return "", "", fmt.Errorf("invalid swap contract")
}

// isStakePubkeyHashScript returns whether or not the passed script is stake
// generation P2PKH script. Script is assumed to be version 0.
func isStakePubkeyHashScript(script []byte) bool {
	opcode := stakeOpcode(script)
	if opcode == 0 {
		return false
	}
	return extractStakePubKeyHash(script, opcode) != nil
}

// isStakePubkeyHashScript returns whether or not the passed script is stake
// generation P2SH script. Script is assumed to be version 0.
func isStakeScriptHashScript(script []byte) bool {
	opcode := stakeOpcode(script)
	if opcode == 0 {
		return false
	}
	return extractStakeScriptHash(script, opcode) != nil
}

// Check if the opcode is one of a small number of acceptable stake opcodes that
// are prepended to P2PKH scripts. Return the opcode if it is, else OP_0.
func stakeOpcode(script []byte) byte {
	if len(script) == 0 {
		return 0
	}
	opcode := script[0]
	if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
		return opcode
	}
	return 0
}

// extractStakePubKeyHash extracts a pubkey hash from the passed public key
// script if it is a standard pay-to-pubkey-hash script tagged with the provided
// stake opcode.  It will return nil otherwise.
func extractStakePubKeyHash(script []byte, stakeOpcode byte) []byte {
	if len(script) == 26 &&
		script[0] == stakeOpcode &&
		script[1] == txscript.OP_DUP &&
		script[2] == txscript.OP_HASH160 &&
		script[3] == txscript.OP_DATA_20 &&
		script[24] == txscript.OP_EQUALVERIFY &&
		script[25] == txscript.OP_CHECKSIG {

		return script[4:24]
	}

	return nil
}

// extractStakeScriptHash extracts a script hash from the passed public key
// script if it is a standard pay-to-script-hash script tagged with the provided
// stake opcode.  It will return nil otherwise.
func extractStakeScriptHash(script []byte, stakeOpcode byte) []byte {
	if len(script) == 24 &&
		script[0] == stakeOpcode &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		script[23] == txscript.OP_EQUAL {

		return script[3:23]
	}

	return nil
}

// isScriptHashScript returns whether or not the passed script is a standard
// pay-to-script-hash script.
func isScriptHashScript(script []byte) bool {
	return extractScriptHash(script) != nil
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

// Grab the script hash based on the dcrScriptType.
func extractScriptHashByType(scriptType dcrScriptType, pkScript []byte) ([]byte, error) {
	var redeemScript []byte
	// Stake related scripts will start with OP_SSGEN or OP_SSRTX.
	if scriptType.isStake() {
		opcode := stakeOpcode(pkScript)
		if opcode == 0 {
			return nil, fmt.Errorf("unsupported stake opcode")
		}
		redeemScript = extractStakeScriptHash(pkScript, opcode)
	} else {
		redeemScript = extractScriptHash(pkScript)
	}
	if redeemScript == nil {
		return nil, fmt.Errorf("failed to parse p2sh script")
	}
	return redeemScript, nil
}
