// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pki

import "github.com/decred/dcrd/dcrec/secp256k1/v3"

type PrivateKey = secp256k1.PrivateKey

const (
	PrivKeySize = secp256k1.PrivKeyBytesLen
	PubKeySize  = secp256k1.PubKeyBytesLenCompressed
)
