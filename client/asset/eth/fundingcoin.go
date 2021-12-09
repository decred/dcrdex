// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

const fundingCoinIDSize = 28 // address (20) + amount (8) = 28

// fundingCoinID is an identifier for a coin which has not yet been sent to the
// swap contract.
type fundingCoinID struct {
	Address common.Address
	Amount  uint64
}

// String creates a human readable string.
func (c *fundingCoinID) String() string {
	return fmt.Sprintf("address: %v, amount:%x", c.Address, c.Amount)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *fundingCoinID) Encode() []byte {
	b := make([]byte, fundingCoinIDSize)
	copy(b[:20], c.Address[:])
	binary.BigEndian.PutUint64(b[20:28], c.Amount)
	return b
}

// decodeFundingCoinID decodes a byte slice into an fundingCoinID struct.
func decodeFundingCoinID(coinID []byte) (*fundingCoinID, error) {
	if len(coinID) != fundingCoinIDSize {
		return nil, fmt.Errorf("decodeFundingCoinID: length expected %v, got %v",
			fundingCoinIDSize, len(coinID))
	}

	var address [20]byte
	copy(address[:], coinID[:20])
	return &fundingCoinID{
		Address: address,
		Amount:  binary.BigEndian.Uint64(coinID[20:28]),
	}, nil
}

// createFundingCoinID constructs a new fundingCoinID for the provided account
// address and amount in Gwei.
func createFundingCoinID(address common.Address, amount uint64) *fundingCoinID {
	return &fundingCoinID{
		Address: address,
		Amount:  amount,
	}
}
