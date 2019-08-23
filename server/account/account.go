package account

import (
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrdex/server/account/pki"
)

var HashFunc = blake256.Sum256

const (
	HashSize = blake256.Size
)

type AccountID [HashSize]byte

func New(pk [pki.PubKeySize]byte) AccountID {
	// hash the pubkey hash(hash(pubkey))
	h := HashFunc(pk[:])
	return HashFunc(h[:])
}
