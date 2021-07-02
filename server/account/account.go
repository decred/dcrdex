// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package account

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type PrivateKey = secp256k1.PrivateKey

const (
	PrivKeySize = secp256k1.PrivKeyBytesLen
	PubKeySize  = secp256k1.PubKeyBytesLenCompressed
	HashSize    = blake256.Size
)

var HashFunc = blake256.Sum256

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

// MarshalJSON satisfies the json.Marshaller interface, and will marshal the
// id to a hex string.
func (aid AccountID) MarshalJSON() ([]byte, error) {
	return json.Marshal(aid.String())
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
	if len(pk) != PubKeySize {
		return nil, fmt.Errorf("invalid pubkey length, "+
			"expected %d, got %d", PubKeySize, len(pk))
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
	NoRule Rule = iota
	// PreimageReveal means an account failed to respond with a valid preimage
	// for their order during epoch processing.
	PreimageReveal
	// FailureToAct means that an account has not followed through on one of their
	// swap negotiation steps.
	FailureToAct
	// CancellationRate means the account's cancellation rate  has dropped below
	// the acceptable level.
	CancellationRate
	// LowFees means an account made a transaction that didn't pay fees at the
	// requisite level.
	LowFees
	// MaxRule in not an actual rule. It is a placeholder that is used to
	// determine the total number of rules. It must always be the last
	// definition in this list.
	MaxRule
)

// details holds rule specific details.
type details struct {
	name, description string
}

// ruleDetails maps rules to rule details.
var ruleDetails = map[Rule]details{
	NoRule: {
		name:        "NoRule",
		description: "no rules have been broken",
	},
	PreimageReveal: {
		name:        "PreimageReveal",
		description: "failed to respond with a valid preimage for an order during epoch processing",
	},
	FailureToAct: {
		name:        "FailureToAct",
		description: "did not follow through on a swap negotiation step",
	},
	CancellationRate: {
		name:        "CancellationRate",
		description: "cancellation rate dropped below the acceptable level",
	},
	LowFees: {
		name:        "LowFees",
		description: "did not pay transaction mining fees at the requisite level",
	},
}

// String satisfies the Stringer interface.
func (r Rule) String() string {
	if d, ok := ruleDetails[r]; ok {
		return d.name
	}
	return "unknown rule"
}

// Description returns a description of the rule.
func (r Rule) Description() string {
	if d, ok := ruleDetails[r]; ok {
		return d.description
	}
	return "description not specified"
}

// Punishable returns whether breaking this rule incurs a penalty.
func (r Rule) Punishable() bool {
	return r > NoRule && r < MaxRule
}
