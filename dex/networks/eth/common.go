// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"fmt"
	"math/big"

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

// ToGwei converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as a uint64.
// Errors if the amount of gwei is too big to fit fully into a uint64.
func ToGwei(wei *big.Int) (uint64, error) {
	if wei.Cmp(new(big.Int)) == -1 {
		return 0, fmt.Errorf("wei must be non-negative")
	}
	gweiFactorBig := big.NewInt(GweiFactor)
	gwei := new(big.Int).Div(wei, gweiFactorBig)
	if !gwei.IsUint64() {
		return 0, fmt.Errorf("suggest gas price %v gwei is too big for a uint64", wei)
	}
	return gwei.Uint64(), nil
}

// ToWei converts a uint64 in gwei (1e9 unit) to wei (1e18 unit) as a *big.Int.
func ToWei(gwei uint64) *big.Int {
	bigGwei := new(big.Int).SetUint64(gwei)
	gweiFactorBig := big.NewInt(GweiFactor)
	return new(big.Int).Mul(bigGwei, gweiFactorBig)
}
