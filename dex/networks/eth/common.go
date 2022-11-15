// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/node"
)

// DecodeCoinID decodes the coin ID into a common.Hash. For eth, there are no
// funding coin IDs, just an account address. Care should be taken not to use
// DecodeCoinID or (Driver).DecodeCoinID for account addresses.
func DecodeCoinID(coinID []byte) (common.Hash, error) {
	if len(coinID) != common.HashLength {
		return common.Hash{}, fmt.Errorf("wrong coin ID length. wanted %d, got %d",
			common.HashLength, len(coinID))
	}
	var h common.Hash
	h.SetBytes(coinID)
	return h, nil
}

// SecretHashSize is the byte-length of the hash of the secret key used in
// swaps.
const SecretHashSize = 32

// JWTHTTPAuthFn returns a function that creates a signed jwt token using the
// provided secret and inserts it into the passed header.
func JWTHTTPAuthFn(jwtStr string) (func(h http.Header) error, error) {
	s, err := hex.DecodeString(strings.TrimPrefix(jwtStr, "0x"))
	if err != nil {
		return nil, err
	}
	var secret [32]byte
	copy(secret[:], s)
	return node.NewJWTAuth(secret), nil
}
