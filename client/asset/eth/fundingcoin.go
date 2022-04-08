// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"encoding/binary"
	"fmt"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
)

const fundingCoinIDSize = 28 // address (20) + amount (8) = 28

// fundingCoin is an identifier for a coin which has not yet been sent to the
// swap contract.
type fundingCoin struct {
	addr common.Address
	amt  uint64
}

// String creates a human readable string.
func (c *fundingCoin) String() string {
	return fmt.Sprintf("address: %v, amount:%x", c.addr, c.amt)
}

// ID utf-8 encodes the account address. This ID will be sent to the server as
// part of the an order.
func (c *fundingCoin) ID() dex.Bytes {
	return []byte(c.addr.String())
}

// Value returns the value reserved in the funding coin.
func (c *fundingCoin) Value() uint64 {
	return c.amt
}

// RecoveryID is a byte-encoded address and value of a funding coin. RecoveryID
// satisfies the asset.RecoveryCoin interface, so this ID will be used as input
// for (asset.Wallet).FundingCoins.
func (c *fundingCoin) RecoveryID() dex.Bytes {
	b := make([]byte, fundingCoinIDSize)
	copy(b[:20], c.addr[:])
	binary.BigEndian.PutUint64(b[20:28], c.amt)
	return b
}

// decodeFundingCoin decodes a byte slice into an fundingCoinID struct.
func decodeFundingCoin(coinID []byte) (*fundingCoin, error) {
	if len(coinID) != fundingCoinIDSize {
		return nil, fmt.Errorf("decodeFundingCoin: length expected %v, got %v",
			fundingCoinIDSize, len(coinID))
	}

	var address [20]byte
	copy(address[:], coinID[:20])
	return &fundingCoin{
		addr: address,
		amt:  binary.BigEndian.Uint64(coinID[20:28]),
	}, nil
}

// createFundingCoin constructs a new fundingCoinID for the provided account
// address and amount in Gwei.
func createFundingCoin(address common.Address, amount uint64) *fundingCoin {
	return &fundingCoin{
		addr: address,
		amt:  amount,
	}
}
