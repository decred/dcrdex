// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"crypto/sha256"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// secp256k1Signer implements the signer interface using a secp256k1 private key.
type secp256k1Signer struct {
	privKey *secp256k1.PrivateKey
}

var _ signer = (*secp256k1Signer)(nil)

// newSecp256k1Signer creates a new signer using the provided private key.
func newSecp256k1Signer(privKey *secp256k1.PrivateKey) (*secp256k1Signer, error) {
	if privKey == nil {
		return nil, fmt.Errorf("nil private key")
	}
	return &secp256k1Signer{
		privKey: privKey,
	}, nil
}

func (s *secp256k1Signer) sign(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	return ecdsa.Sign(s.privKey, hash[:]).Serialize(), nil
}

func (s *secp256k1Signer) verify(sigBytes, data []byte) bool {
	sig, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return false
	}
	hash := sha256.Sum256(data)
	return sig.Verify(hash[:], s.privKey.PubKey())
}
