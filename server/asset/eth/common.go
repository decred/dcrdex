// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// SwapState is the state of a swap and corresponds to values in the Solidity
// swap contract.
type SwapState uint8

// CoinIDFlag signifies the type of coin ID. Currenty an eth coin ID can be
// either a contract address and secret hash or a txid.
type CoinIDFlag uint16

// Swap states represent the status of a swap. The default state of a swap is
// SSNone. A swap in status SSNone does not exist. SSInitiated indicates that a
// party has initiated the swap and funds have been sent to the contract.
// SSRedeemed indicates a successful swap where the participant was able to
// redeem with the secret hash. SSRefunded indicates a failed swap, where the
// initiating party refunded their coins after the locktime passed. A swap no
// longer changes states after reaching SSRedeemed or SSRefunded.
const (
	// SSNone indicates that the swap is not initiated. This is the default
	// state of a swap.
	SSNone SwapState = iota
	// SSInitiated indicates that the swap has been initiated.
	SSInitiated
	// SSRedeemed indicates that the swap was initiated and then redeemed.
	// This is one of two possible end states of a swap.
	SSRedeemed
	// SSRefunded indicates that the swap was initiated and then refunded.
	// This is one of two possible end states of a swap.
	SSRefunded
)

// CIDTxID and CIDSwap are used in CoinIDs to signify a coinID
// as either a transaction ID or a combination of a swap contract
// address and secret hash. One or the other must be set.
const (
	// CIDTxID indicates that this coin ID's hash is a txid. The address
	// portion is zeros and unused.
	CIDTxID CoinIDFlag = 1 << iota
	// CIDSwap indicates that this coin ID represents a swap with a
	// contract address and secret hash used to fetch data about a swap
	// from the live contract.
	CIDSwap
)

const (
	// coinIdSize = flags (2) + smart contract address where funds are
	// locked (20) + secret hash map key (32)
	coinIDSize = 54
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
	// GweiFactor is the amount of wei in one gwei. Eth balances are floored
	// as gwei, or 1e9 wei. This is used in factoring.
	GweiFactor = 1e9
	// InitGas is the amount of gas needed to initialize an ethereum swap.
	//
	// The price of a normal transaction is 21000 gas. However, when
	// contract methods are called it is difficult to descern where gas is
	// used. The formal declaration of what costs how much can be found at
	// https://ethereum.github.io/yellowpaper/paper.pdf in Appendix G.
	// The current value here is an appoximation based on tests.
	//
	// TODO: When the contract is solidified, break down evm functions
	// called and the gas used for each. (◍•﹏•)
	InitGas = 180000 // gas
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
	case SSNone:
		return "none"
	case SSInitiated:
		return "initiated"
	case SSRedeemed:
		return "redeemed"
	case SSRefunded:
		return "refunded"
	}
	return "unknown"
}

// DecodeCoinID decodes the coin ID into flags, a contract address, and hash
// that represents either a secret or txid depending on flags. The CIDTxID flag
// indicates that this coinID represents a simple txid. The address return is
// unused and bytes are a 32 byte txid. Bytes are in the same order as the txid.
// The CIDSwap flag indicates that this coinID represents a swap contract. The
// address is the swap contract's address and the bytes return is the 32 byte
// secret hash of a swap. Errors if the passed coinID is not the expected length.
func DecodeCoinID(coinID []byte) (CoinIDFlag, *common.Address, []byte, error) {
	if len(coinID) != coinIDSize {
		return 0, nil, nil, fmt.Errorf("coin ID wrong length. expected %d, got %d",
			coinIDSize, len(coinID))
	}
	hash, addr := make([]byte, 32), new(common.Address)
	copy(hash, coinID[22:])
	copy(addr[:], coinID[2:22])
	return CoinIDFlag(binary.BigEndian.Uint16(coinID[:2])),
		addr, hash, nil
}

// CoinIDToString converts coinID into a human readable string.
func CoinIDToString(coinID []byte) (string, error) {
	flags, addr, hash, err := DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x:%x:%x", flags, addr, hash), nil
}

// ToCoinID converts the address and secret hash, or txid to a coin ID. hash is
// expected to be 32 bytes long and represent either a txid or a secret hash. A
// hash longer than 32 bytes is truncated at 32. If a txid, bytes should be
// kept in the same order as the hex string representation.
func ToCoinID(flags CoinIDFlag, addr *common.Address, hash []byte) []byte {
	b := make([]byte, coinIDSize)
	binary.BigEndian.PutUint16(b[:2], uint16(flags))
	if IsCIDSwap(flags) {
		copy(b[2:], addr[:])
	}
	copy(b[22:], hash[:])
	return b
}

// IsCIDTxID returns whether the passed flags indicate the associated coin ID's
// hash portion represents a transaction hash.
func IsCIDTxID(flags CoinIDFlag) bool {
	return flags == CIDTxID
}

// IsCIDSwap returns whether the passed flags indicate the associated coin ID
// represents a swap with an address and secret hash.
func IsCIDSwap(flags CoinIDFlag) bool {
	return flags == CIDSwap
}
