package account

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
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

// String returns a hexadecimal representation of the AccountID. String
// implements fmt.Stringer.
func (aid AccountID) String() string {
	return hex.EncodeToString(aid[:])
}

// Value implements the sql/driver.Valuer interface.
func (aid AccountID) Value() (driver.Value, error) {
	return aid[:], nil // []byte
}

// Scan implements the sql.Scanner interface.
func (aid *AccountID) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		copy(aid[:], src)
		return nil
		//case string:
		// case nil:
		// 	*oid = nil
		// 	return nil
	}

	return fmt.Errorf("cannot convert %T to AccountID", src)
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

// Rule represents a rule of community conduct.
type Rule uint8

const (
	// NoRule indicates that no rules have been broken. This may be an invalid
	// value in some contexts.
	NoRule = iota
	// FailureToAct means that an account has not followed through on one of their
	// swap negotiation steps.
	FailureToAct
	// CancellationRatio means the account's cancellation ratio has dropped below
	// the acceptable level.
	CancellationRatio
	// LowFees means an account made a transaction that didn't pay fees at the
	// requisite level.
	LowFees
)
