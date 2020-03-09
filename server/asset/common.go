// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import "decred.org/dcrdex/dex"

// CoinNotFoundError is to be returned from Contract, Redemption, and
// FundingCoin when the specified transaction cannot be found. Used by the
// server to handle network latency.
const CoinNotFoundError = dex.Error("coin not found")

// The Backend interface is an interface for a blockchain backend.
type Backend interface {
	dex.Runner
	// Contract should return a Contract only for outputs that would be spendable
	// on the blockchain immediately. The redeem script is required in order to
	// calculate sigScript length and verify pubkeys.
	Contract(coinID []byte, redeemScript []byte) (Contract, error)
	// ValidateSecret checks that the secret satisfies the contract.
	ValidateSecret(secret, contract []byte) bool
	// Redemption returns the redemption at the specified location.
	Redemption(redemptionID, contractID []byte) (Coin, error)
	// FundingCoin returns the unspent coin at the specified location.
	FundingCoin(coinID []byte, redeemScript []byte) (FundingCoin, error)
	// BlockChannel creates and returns a new channel on which to receive updates
	// when new blocks are connected.
	BlockChannel(size int) chan uint32
	// InitTxSize is the size of a serialized atomic swap initialization
	// transaction with 1 input spending a P2PKH utxo, 1 swap contract output and
	// 1 change output.
	InitTxSize() uint32
	// CheckAddress checks that the given address is parseable.
	CheckAddress(string) bool
	// ValidateCoinID checks the coinID to ensure it can be decoded.
	ValidateCoinID(coinID []byte) error
	// ValidateContract ensures that the swap contract is constructed properly
	// for the asset.
	ValidateContract(contract []byte) error
}

// Coin represents a transaction input or ouptut.
type Coin interface {
	// Confirmations returns the number of confirmations for a Coin's transaction.
	// Because a Coin can become invalid after once being considered valid, this
	// condition should be checked for during confirmation counting and an error
	// returned if this Coin is no longer ready to spend. An unmined transaction
	// should have zero confirmations. A transaction in the current best block
	// should have one confirmation. A negative number can be returned if error
	// is not nil.
	Confirmations() (int64, error)
	// ID is the coin ID.
	ID() []byte
	// TxID is a transaction identifier for the coin.
	TxID() string
}

// FundingCoin is some unspent value on the blockchain.
type FundingCoin interface {
	Coin
	// Auth checks that the owner of the provided pubkeys can spend the
	// FundingCoin. The signatures (sigs) generated with the private keys
	// corresponding to pubkeys must validate against the pubkeys and signing
	// message (msg).
	Auth(pubkeys, sigs [][]byte, msg []byte) error
	// SpendSize returns the size of the serialized input that spends this
	// FundingCoin.
	SpendSize() uint32
	// Value is the output value.
	Value() uint64
}

// Contract is an atomic swap contract.
type Contract interface {
	Coin
	// Value is the contract value.
	Value() uint64
	// Address retrieves the receiving address of the contract.
	Address() string
	// FeeRate returns the transaction fee rate, in atoms/byte equivalent.
	FeeRate() uint64
	// Script is the contract redeem script.
	Script() []byte
}

// BackedAsset is a dex.Asset with a Backend.
type BackedAsset struct {
	dex.Asset
	Backend Backend
}
