// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type SwapState uint8

const (
	// Swap states represent the status of a swap.
	None SwapState = iota
	Initiated
	Redeemed
	Refunded

	// coinIdSize = flags (2) + smart contract address where funds are
	// locked (20) + secret hash map key (32)
	coinIDSize = 54
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
	// GweiFactor is the amount of wei in one gwei. Eth balances are floored
	// as gwei, or 1e9 wei. This is used in factoring.
	GweiFactor = 1e9
)

// ToGwei converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as a uint64.
// Errors if the amount of gwei is too big to fit fully into a uint64.
func ToGwei(wei *big.Int) (uint64, error) {
	gweiFactorBig := big.NewInt(GweiFactor)
	wei.Div(wei, gweiFactorBig)
	if !wei.IsUint64() {
		return 0, fmt.Errorf("suggest gas price %v gwei is too big for a uint64", wei)
	}
	return wei.Uint64(), nil
}

// String satisfies the Stringer interface.
func (ss SwapState) String() string {
	switch ss {
	case None:
		return "none"
	case Initiated:
		return "initiated"
	case Redeemed:
		return "redeemed"
	case Refunded:
		return "refunded"
	}
	return "unknown"
}

// DecodeCoinID decodes the coin ID into flags, a contract address, and secret hash.
func DecodeCoinID(coinID []byte) (uint16, common.Address, []byte, error) {
	if len(coinID) != coinIDSize {
		return 0, common.Address{}, nil, fmt.Errorf("coin ID wrong length. expected %d, got %d",
			coinIDSize, len(coinID))
	}
	secretHash := make([]byte, 32)
	copy(secretHash, coinID[22:])
	return binary.BigEndian.Uint16(coinID[:2]), common.BytesToAddress(coinID[2:22]), secretHash, nil
}

// CoinIDToString converts coinID into a human readable string.
func CoinIDToString(coinID []byte) (string, error) {
	flags, addr, secretHash, err := DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x:%x", flags, addr, secretHash), nil
}

// ToCoinID converts the address and secret hash to a coin ID.
func ToCoinID(flags uint16, addr *common.Address, secretHash []byte) []byte {
	b := make([]byte, coinIDSize)
	b[0] = byte(flags)
	b[1] = byte(flags >> 8)
	copy(b[2:], addr[:])
	copy(b[22:], secretHash[:])
	return b
}
