// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"crypto/sha256"
	"fmt"

	"github.com/btcsuite/btcd/btcec"
)

// checkSig checks that the message's signature was created with the
// private key for the provided public key.
func checkSig(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := btcec.ParsePubKey(pkBytes, btcec.S256())
	if err != nil {
		return fmt.Errorf("error decoding PublicKey from bytes: %w", err)
	}
	signature, err := btcec.ParseDERSignature(sigBytes, btcec.S256())
	if err != nil {
		return fmt.Errorf("error decoding Signature from bytes: %w", err)
	}
	hash := sha256.Sum256(msg)
	if !signature.Verify(hash[:], pubKey) {
		// This might be a legacy (buggy) client that signed the truncated
		// message itself. (V0PURGE!)
		if !signature.Verify(msg, pubKey) {
			return fmt.Errorf("signature verification failed")
		}
	}
	return nil
}
