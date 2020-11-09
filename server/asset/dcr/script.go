// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"

	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/schnorr"
)

// checkSig checks the signature against the pubkey and message.
func checkSig(msg, pkBytes, sigBytes []byte, sigType dcrec.SignatureType) error {
	switch sigType {
	case dcrec.STEcdsaSecp256k1:
		return checkSigS256(msg, pkBytes, sigBytes)
	case dcrec.STEd25519:
		return checkSigEdwards(msg, pkBytes, sigBytes)
	case dcrec.STSchnorrSecp256k1:
		return checkSigSchnorr(msg, pkBytes, sigBytes)
	}
	return fmt.Errorf("unsupported signature type")
}

// checkSigS256 checks that the message's signature was created with the
// private key for the provided secp256k1 public key.
func checkSigS256(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 PublicKey from bytes: %w", err)
	}
	signature, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}

// checkSigEdwards checks that the message's signature was created with the
// private key for the provided edwards public key.
func checkSigEdwards(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := edwards.ParsePubKey(pkBytes)
	if err != nil {
		return fmt.Errorf("error decoding edwards PublicKey from bytes: %w", err)
	}
	signature, err := edwards.ParseSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("error decoding edwards Signature from bytes: %w", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("edwards signature verification failed")
	}
	return nil
}

// checkSigSchnorr checks that the message's signature was created with the
// private key for the provided schnorr public key.
func checkSigSchnorr(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := schnorr.ParsePubKey(pkBytes)
	if err != nil {
		return fmt.Errorf("error decoding schnorr PublicKey from bytes: %w", err)
	}
	signature, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("error decoding schnorr Signature from bytes: %w", err)
	}
	if !signature.Verify(msg, pubKey) {
		return fmt.Errorf("schnorr signature verification failed")
	}
	return nil
}
