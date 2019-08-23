package pki

import "github.com/decred/dcrd/dcrec/secp256k1"

type PrivateKey = secp256k1.PrivateKey

const (
	PrivKeySize = secp256k1.PrivKeyBytesLen
	PubKeySize  = secp256k1.PubKeyBytesLenCompressed
)
