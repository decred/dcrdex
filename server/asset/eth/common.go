// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// coinIdSize = flags (2) + smart contract address where funds are locked (20) + secret
// hash map key (32)
const (
	coinIDSize = 54
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
)

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
