// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
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
