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

const fundingCoinIDSize = 28      // address (20) + amount (8) = 28
const tokenFundingCoinIDSize = 36 // address (20) + amount (8) + amount (8) = 36

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

// tokenFundingCoinID is an ID
type tokenFundingCoinID struct {
	Address    common.Address
	TokenValue uint64
	Fees       uint64
}

// String creates a human readable string.
func (c *tokenFundingCoinID) String() string {
	return fmt.Sprintf("address: %s, amount:%v, fees:%v", c.Address, c.TokenValue, c.Fees)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *tokenFundingCoinID) Encode() []byte {
	b := make([]byte, tokenFundingCoinIDSize)
	copy(b[:20], c.Address[:])
	binary.BigEndian.PutUint64(b[20:28], c.TokenValue)
	binary.BigEndian.PutUint64(b[28:36], c.Fees)
	return b
}

// decodeTokenFundingCoinID decodes a byte slice into an tokenFundingCoinID.
func decodeTokenFundingCoinID(coinID []byte) (*tokenFundingCoinID, error) {
	if len(coinID) != tokenFundingCoinIDSize {
		return nil, fmt.Errorf("decodeTokenFundingCoinID: length expected %v, got %v",
			tokenFundingCoinIDSize, len(coinID))
	}

	var address [20]byte
	copy(address[:], coinID[:20])
	return &tokenFundingCoinID{
		Address:    address,
		TokenValue: binary.BigEndian.Uint64(coinID[20:28]),
		Fees:       binary.BigEndian.Uint64(coinID[28:36]),
	}, nil
}

// createFundingCoinID constructs a new fundingCoinID for the provided account
// address and amount in Gwei.
func createTokenFundingCoinID(address common.Address, tokenValue, fees uint64) *tokenFundingCoinID {
	return &tokenFundingCoinID{
		Address:    address,
		TokenValue: tokenValue,
		Fees:       fees,
	}
}
