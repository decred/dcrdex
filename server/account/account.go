package account

import (
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrdex/server/account/pki"
)

var HashFunc = blake256.Sum256

const (
	HashSize = blake256.Size
)

type AccountID [HashSize]byte

// NewID generates a unique account id with the provided public key bytes.
func NewID(pk []byte) AccountID {
	// Hash the pubkey hash.
	h := HashFunc(pk)
	return HashFunc(h[:])
}

// Account represents a dex client account.
type Account struct {
	ID     AccountID
	PubKey *secp256k1.PublicKey
}

// NewAccountFromPubKey creates a dex client account from the provided public
// key bytes.
func NewAccountFromPubKey(pk []byte) (*Account, error) {
	if len(pk) != pki.PubKeySize {
		return nil, fmt.Errorf("invalid pubkey length, "+
			"expected %d, got %d", pki.PubKeySize, len(pk))
	}

	pubKey, err := secp256k1.ParsePubKey(pk)
	if err != nil {
		return nil, err
	}

	return &Account{
		ID:     NewID(pk),
		PubKey: pubKey,
	}, nil
}
