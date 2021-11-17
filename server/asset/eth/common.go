// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

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
	// CIDAmount indicates that this coin ID represents a coin which has
	// not yet been submitted to the swap contract. It contains the address
	// which holds the ETH, an amount of ETH in gwei, and a random nonce to
	// avoid duplicate coin IDs.
	CIDAmount
)

// CoinID is an interface that objects which represent different types of ETH
// coin identifiers must implement.
type CoinID interface {
	String() string
	Encode() []byte
}

const (
	// coin type id (2) + tx id (32) + index (4) = 38
	txCoinIDSize = 38
	// coin type id (2) + address (20) + secret has (32) = 54
	swapCoinIDSize = 54
	// coin type id (2) + address (20) + amount (8) + nonce (8) = 38
	amountCoinIDSize = 38
)

// TxCoinID identifies a coin in the swap contract by the
// hash of the transaction which initiatied the swap and the
// index of the initiation in the argument to the initiate
// function. This type of ID is useful to identify coins that
// were sent in transactions that have not yet been mined.
type TxCoinID struct {
	TxID  common.Hash
	Index uint32
}

// String creates a human readable string.
func (c *TxCoinID) String() string {
	return fmt.Sprintf("tx: %x, index: %d", c.TxID, c.Index)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *TxCoinID) Encode() []byte {
	b := make([]byte, txCoinIDSize)
	binary.BigEndian.PutUint16(b[:2], uint16(CIDTxID))
	copy(b[2:], c.TxID[:])
	binary.BigEndian.PutUint32(b[34:], c.Index)
	return b
}

var _ CoinID = (*TxCoinID)(nil)

// decodeTxCoinID decodes a byte slice into an TxCoinID struct.
func decodeTxCoinID(coinID []byte) (*TxCoinID, error) {
	if len(coinID) != txCoinIDSize {
		return nil, fmt.Errorf("decodeTxCoinID: length expected %v, got %v",
			txCoinIDSize, len(coinID))
	}

	flag := binary.BigEndian.Uint16(coinID[:2])
	if CoinIDFlag(flag) != CIDTxID {
		return nil, fmt.Errorf("decodeTxCoinID: flag expected %v, got %v",
			flag, CIDTxID)
	}

	var txID [32]byte
	copy(txID[:], coinID[2:])

	index := binary.BigEndian.Uint32(coinID[34:])

	return &TxCoinID{
		TxID:  txID,
		Index: index,
	}, nil
}

// SwapCoinID identifies a coin in a swap contract.
type SwapCoinID struct {
	ContractAddress common.Address
	SecretHash      [32]byte
}

// String creates a human readable string.
func (c *SwapCoinID) String() string {
	return fmt.Sprintf("contract: %v, secret hash:%x",
		c.ContractAddress, c.SecretHash)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *SwapCoinID) Encode() []byte {
	b := make([]byte, swapCoinIDSize)
	binary.BigEndian.PutUint16(b[:2], uint16(CIDSwap))
	copy(b[2:22], c.ContractAddress[:])
	copy(b[22:], c.SecretHash[:])
	return b
}

var _ CoinID = (*SwapCoinID)(nil)

// decodeSwapCoinID decodes a byte slice into an SwapCoinID struct.
func decodeSwapCoinID(coinID []byte) (*SwapCoinID, error) {
	if len(coinID) != swapCoinIDSize {
		return nil, fmt.Errorf("decodeSwapCoinID: length expected %v, got %v",
			txCoinIDSize, len(coinID))
	}

	flag := binary.BigEndian.Uint16(coinID[:2])
	if CoinIDFlag(flag) != CIDSwap {
		return nil, fmt.Errorf("decodeSwapCoinID: flag expected %v, got %v",
			flag, CIDSwap)
	}

	var contractAddress [20]byte
	var secretHash [32]byte
	copy(contractAddress[:], coinID[2:22])
	copy(secretHash[:], coinID[22:])
	return &SwapCoinID{
		ContractAddress: contractAddress,
		SecretHash:      secretHash,
	}, nil
}

// AmountCoinID is an identifier for a coin which has not yet been sent to the
// swap contract.
type AmountCoinID struct {
	Address common.Address
	Amount  uint64
	Nonce   [8]byte
}

// String creates a human readable string.
func (c *AmountCoinID) String() string {
	return fmt.Sprintf("address: %v, amount:%x, nonce:%x",
		c.Address, c.Amount, c.Nonce)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *AmountCoinID) Encode() []byte {
	b := make([]byte, amountCoinIDSize)
	binary.BigEndian.PutUint16(b[:2], uint16(CIDAmount))
	copy(b[2:22], c.Address[:])
	binary.BigEndian.PutUint64(b[22:30], c.Amount)
	copy(b[30:], c.Nonce[:])
	return b
}

var _ CoinID = (*AmountCoinID)(nil)

// decodeAmountCoinID decodes a byte slice into an AmountCoinID struct.
func decodeAmountCoinID(coinID []byte) (*AmountCoinID, error) {
	if len(coinID) != amountCoinIDSize {
		return nil, fmt.Errorf("DecodeAmountCoinID: length expected %v, got %v",
			txCoinIDSize, len(coinID))
	}

	flag := binary.BigEndian.Uint16(coinID[:2])
	if CoinIDFlag(flag) != CIDAmount {
		return nil, fmt.Errorf("DecodeAmountCoinID: flag expected %v, got %v",
			flag, CIDAmount)
	}

	var address [20]byte
	var nonce [8]byte
	copy(address[:], coinID[2:22])
	copy(nonce[:], coinID[30:])
	return &AmountCoinID{
		Address: address,
		Amount:  binary.BigEndian.Uint64(coinID[22:30]),
		Nonce:   nonce,
	}, nil
}

// DecodeCoinID decodes the coin id byte slice into an object implementing the
// CoinID interface.
func DecodeCoinID(coinID []byte) (CoinID, error) {
	if len(coinID) < 2 {
		return nil,
			fmt.Errorf("DecodeCoinID: coinID length must be > 2, but got %v",
				len(coinID))
	}
	flag := CoinIDFlag(binary.BigEndian.Uint16(coinID[:2]))
	if flag == CIDTxID {
		return decodeTxCoinID(coinID)
	} else if flag == CIDSwap {
		return decodeSwapCoinID(coinID)
	} else if flag == CIDAmount {
		return decodeAmountCoinID(coinID)
	} else {
		return nil, fmt.Errorf("DecodeCoinID: invalid coin id flag: %v", flag)
	}
}

const (
	// MaxBlockInterval is the number of seconds since the last header came
	// in over which we consider the chain to be out of sync.
	MaxBlockInterval = 180
	// GweiFactor is the amount of wei in one gwei. Eth balances are floored
	// as gwei, or 1e9 wei. This is used in factoring.
	GweiFactor = 1e9
	// InitGas is the amount of gas needed to initialize a single
	// ethereum swap.
	InitGas = 135000
	// AdditionalInitGas is the amount of gas needed to initialize
	// additional swaps in the same transaction.
	AdditionalInitGas = 113000
	// RedeemGas is the amount of gas it costs to redeem a swap.
	RedeemGas = 60000
)

// ToGwei converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as a uint64.
// Errors if the amount of gwei is too big to fit fully into a uint64. The
// passed wei parameter value is changed and is no longer useable.
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
