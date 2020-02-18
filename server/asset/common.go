// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import "decred.org/dcrdex/dex"

// The Backend interface is an interface for a blockchain backend.
type Backend interface {
	dex.Runner
	// Coin should return a Coin only for outputs that would be spendable on the
	// blockchain immediately. Pay-to-script-hash Coins require the redeem script
	// in order to calculate sigScript length and verify pubkeys.
	Coin(coinID []byte, redeemScript []byte) (Coin, error)
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
	// VerifyCoin decodes the coinID, and then attempts to retrieve details from
	// the blockchain. The redeemScript is verified to correspond with the
	// coinID. e.g. The coinID refers to a P2SH transaction output with a script
	// hash corresponding to the provided redeem script.
	VerifyCoin(coinID []byte, redeemScript []byte) error
	// ValidateContract ensures that the swap contract is constructed properly
	// for the asset.
	ValidateContract(contract []byte) error
}

// Coin provides data about an unspent transaction output.
type Coin interface {
	// Confirmations returns the number of confirmations for a Coin's transaction.
	// Because a Coin can become invalid after once being considered valid, this
	// condition should be checked for during confirmation counting and an error
	// returned if this Coin is no longer ready to spend. An unmined transaction
	// should have zero confirmations. A transaction in the current best block
	// should have one confirmation. A negative number can be returned if error
	// is not nil.
	Confirmations() (int64, error)
	// Auth checks that the owner of the provided pubkeys can spend the Coin.
	// The signatures (sigs) generated with the private keys corresponding
	// to pubkeys must validate against the pubkeys and signing message (msg).
	Auth(pubkeys, sigs [][]byte, msg []byte) error
	// AuditContract checks that coin is a swap contract and extracts the
	// receiving address and contract value on success.
	AuditContract() (string, uint64, error)
	// SpendsCoin checks if the coin spends a specified previous coin.
	// An error will be returned if the input is not parseable. If the Coin is not
	// spent by this coin, the boolean return value will be false, but no
	// error is returned.
	SpendsCoin(coinID []byte) (bool, error)
	// FeeRate returns the transaction fee rate, in atoms/byte equivalent.
	FeeRate() uint64
	// SpendSize returns the size of the serialized input that spends this Coin.
	SpendSize() uint32
	// ID is the coin ID.
	ID() []byte
	// TxID is a transaction identifier for the coin.
	TxID() string
	// Value is the output value.
	Value() uint64
}

// BackedAsset is a dex.Asset with a Backend.
type BackedAsset struct {
	dex.Asset
	Backend Backend
}
