// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex/encode"
	"github.com/ethereum/go-ethereum/common"
)

// CoinIDFlag signifies the type of coin ID. Currenty an eth coin ID can be
// either a contract address and secret hash or a txid.
type CoinIDFlag uint16

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
	// SecretHashSize is the byte-length of the hash of the secret key used
	// in swaps.
	SecretHashSize = 32
)

// CoinID is an interface that objects which represent different types of ETH
// coin identifiers must implement.
type CoinID interface {
	String() string
	Encode() []byte
}

const (
	// coin type id (2) + tx id (32) = 34
	txCoinIDSize = 34
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
	TxID common.Hash
}

// String creates a human readable string.
func (c *TxCoinID) String() string {
	return fmt.Sprintf("tx: %x", c.TxID)
}

// Encode creates a byte slice that can be decoded with DecodeCoinID.
func (c *TxCoinID) Encode() []byte {
	b := make([]byte, txCoinIDSize)
	binary.BigEndian.PutUint16(b[:2], uint16(CIDTxID))
	copy(b[2:], c.TxID[:])
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

	return &TxCoinID{
		TxID: txID,
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

func CreateAmountCoinID(address common.Address, amount uint64) *AmountCoinID {
	var nonce [8]byte
	copy(nonce[:], encode.RandomBytes(8))
	return &AmountCoinID{
		Address: address,
		Amount:  amount,
		Nonce:   nonce,
	}
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

// ToGwei converts a *big.Int in wei (1e18 unit) to gwei (1e9 unit) as a uint64.
// Errors if the amount of gwei is too big to fit fully into a uint64.
func ToGwei(wei *big.Int) (uint64, error) {
	gweiFactorBig := big.NewInt(GweiFactor)
	gwei := big.NewInt(0).Div(wei, gweiFactorBig)
	if !gwei.IsUint64() {
		return 0, fmt.Errorf("suggest gas price %v gwei is too big for a uint64", wei)
	}
	return gwei.Uint64(), nil
}

// ToWei converts a uint64 in gwei (1e9 unit) to wei (1e18 unit) as a *big.Int.
func ToWei(gwei uint64) *big.Int {
	bigGwei := big.NewInt(0).SetUint64(gwei)
	gweiFactorBig := big.NewInt(GweiFactor)
	return new(big.Int).Mul(bigGwei, gweiFactorBig)
}
