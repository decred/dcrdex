package account

import (
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/account/pki"
)

// Account represents a dex account.
type Account struct {
	ID      account.AccountID
	PrivKey *secp256k1.PrivateKey
}

// New creates a dex account. The associated private key of the
// account is generated randomly.
func New() (*Account, error) {
	privKey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}

	pubKey := privKey.PubKey()
	return &Account{
		ID:      account.NewID(pubKey.SerializeCompressed()),
		PrivKey: privKey,
	}, nil
}

// NewAccountFromPrivKey creates a dex account from the provided private
// key bytes.
func NewAccountFromPrivKey(privK []byte) (*Account, error) {
	if len(privK) != pki.PrivKeySize {
		return nil, fmt.Errorf("invalid private key length, "+
			"expected %d, got %d", pki.PrivKeySize, len(privK))
	}

	privKey, pubKey := secp256k1.PrivKeyFromBytes(privK)
	return &Account{
		ID:      account.NewID(pubKey.SerializeCompressed()),
		PrivKey: privKey,
	}, nil
}

// Sign generates a signature for the provided message.
func (acc *Account) Sign(msg []byte) ([]byte, error) {
	if acc.PrivKey == nil {
		return nil, fmt.Errorf("account not initialized")
	}

	hash := account.HashFunc(msg)
	sig, err := acc.PrivKey.Sign(hash[:])
	if err != nil {
		return nil, err
	}

	return sig.Serialize(), nil
}
