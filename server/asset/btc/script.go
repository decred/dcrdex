// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"

	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
)

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

// extractSwapAddresses extacts the sender and receiver addresses from a swap
// contract. If the provided script is not a swap contract, an error will be
// returned.
func extractSwapAddresses(pkScript []byte, chainParams *chaincfg.Params) (string, string, error) {
	s, r, _, _, err := dexbtc.ExtractSwapDetails(pkScript, chainParams)
	if err != nil {
		return "", "", err
	}
	return s.String(), r.String(), err
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
