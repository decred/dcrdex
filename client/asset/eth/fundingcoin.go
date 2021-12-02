// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"encoding/binary"
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"github.com/ethereum/go-ethereum/common"
)

const fundingCoinIDSize = 36 // address (20) + amount (8) + nonce (8) = 36

// fundingCoinID is an identifier for a coin which has not yet been sent to the
// swap contract.
type fundingCoinID struct {
	Address common.Address
	Amount  uint64
	Nonce   [8]byte
}

// String creates a human readable string.
func (c *fundingCoinID) String() string {
	return fmt.Sprintf("address: %v, amount:%x, nonce:%x",
		c.Address, c.Amount, c.Nonce)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *fundingCoinID) Encode() []byte {
	b := make([]byte, fundingCoinIDSize)
	copy(b[:20], c.Address[:])
	binary.BigEndian.PutUint64(b[20:28], c.Amount)
	copy(b[28:], c.Nonce[:])
	return b
}

// decodeFundingCoinID decodes a byte slice into an fundingCoinID struct.
func decodeFundingCoinID(coinID []byte) (*fundingCoinID, error) {
	if len(coinID) != fundingCoinIDSize {
		return nil, fmt.Errorf("decodeFundingCoinID: length expected %v, got %v",
			fundingCoinIDSize, len(coinID))
	}

	var address [20]byte
	var nonce [8]byte
	copy(address[:], coinID[:20])
	copy(nonce[:], coinID[28:])
	return &fundingCoinID{
		Address: address,
		Amount:  binary.BigEndian.Uint64(coinID[20:28]),
		Nonce:   nonce,
	}, nil
}

// createFundingCoinID constructs a new fundingCoinID for the provided account
// address and amount in Gwei. A random nonce is assigned.
func createFundingCoinID(address common.Address, amount uint64) *fundingCoinID {
	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))
	return &fundingCoinID{
		Address: address,
		Amount:  amount,
		Nonce:   nonce,
	}
}
