// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mj

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func MessageDigest(msg *msgjson.Message) [32]byte {
	b := make([]byte, 0, 1+len(msg.Route)+8+len(msg.Payload))
	b = append(b, byte(msg.Type))
	b = append(b, []byte(msg.Route)...)
	var idB [8]byte
	binary.BigEndian.PutUint64(idB[:], msg.ID)
	b = append(b, idB[:]...)
	b = append(b, msg.Payload...)
	return sha256.Sum256(b)
}

func SignMessage(priv *secp256k1.PrivateKey, msg *msgjson.Message) {
	h := MessageDigest(msg)
	msg.Sig = ecdsa.Sign(priv, h[:]).Serialize()
}

// CheckSig checks that the message's signature was created with the private
// key for the provided secp256k1 public key on the sha256 hash of the message.
func CheckSig(msg *msgjson.Message, pubKey *secp256k1.PublicKey) error {
	signature, err := ecdsa.ParseDERSignature(msg.Sig)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	h := MessageDigest(msg)
	if !signature.Verify(h[:], pubKey) {
		return fmt.Errorf("secp256k1 signature verification failed")
	}
	return nil
}
