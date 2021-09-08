// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
)

// Addresser retrieves unique addresses.
type Addresser interface {
	NextAddress() string
}

// HDKeyIndexer retrieves and stores child indexes for an extended public key.
type HDKeyIndexer interface {
	KeyIndex(xpub string) (uint32, error)
	SetKeyIndex(idx uint32, xpub string) error
}

// CoinNotFoundError is to be returned from Contract, Redemption, and
// FundingCoin when the specified transaction cannot be found. Used by the
// server to handle network latency.
const (
	CoinNotFoundError = dex.ErrorKind("coin not found")
	ErrRequestTimeout = dex.ErrorKind("request timeout")
)

// Backend is a blockchain backend. TODO: Plumb every method with a cancellable
// request with a Context, which will prevent backend shutdown from cancelling
// requests, but is more idiomatic these days.
type Backend interface {
	// It is expected that Connect from dex.Connector is called and returns
	// before use of the asset, and that it is only called once for the life
	// of the asset.
	dex.Connector
	// Contract returns a Contract only for outputs that would be spendable on
	// the blockchain immediately. The redeem script is required in order to
	// calculate sigScript length and verify pubkeys.
	Contract(coinID []byte, redeemScript []byte) (*Contract, error)
	// TxData fetches the raw transaction data for the specified coin.
	TxData(coinID []byte) ([]byte, error)
	// ValidateSecret checks that the secret satisfies the contract.
	ValidateSecret(secret, contract []byte) bool
	// Redemption returns a Coin for redemptionID, a transaction input, that
	// spends contract ID, an output containing the swap contract.
	Redemption(redemptionID, contractID []byte) (Coin, error)
	// FundingCoin returns the unspent coin at the specified location. Coins
	// with non-standard pkScripts or scripts that require zero signatures to
	// redeem must return an error.
	FundingCoin(ctx context.Context, coinID []byte, redeemScript []byte) (FundingCoin, error)
	// BlockChannel creates and returns a new channel on which to receive updates
	// when new blocks are connected.
	BlockChannel(size int) <-chan *BlockUpdate
	// InitTxSize is the size of a serialized atomic swap initialization
	// transaction with 1 input spending a P2PKH utxo, 1 swap contract output and
	// 1 change output.
	InitTxSize() uint32
	// InitTxSizeBase is InitTxSize not including an input.
	InitTxSizeBase() uint32
	// CheckAddress checks that the given address is parseable.
	CheckAddress(string) bool
	// ValidateCoinID checks the coinID to ensure it can be decoded, returning a
	// human-readable string if it is valid.
	ValidateCoinID(coinID []byte) (string, error)
	// ValidateContract ensures that the swap contract is constructed properly
	// for the asset.
	ValidateContract(contract []byte) error
	// VerifyUnspentCoin attempts to verify a coin ID by decoding the coin ID
	// and retrieving the corresponding Coin. If the coin is not found or no
	// longer unspent, an asset.CoinNotFoundError is returned. Use FundingCoin
	// for more UTXO data.
	VerifyUnspentCoin(ctx context.Context, coinID []byte) error
	// FeeRate returns the current optimal fee rate in atoms / byte.
	FeeRate(context.Context) (uint64, error)
	// Synced should return true when the blockchain is synced and ready for
	// fee rate estimation.
	Synced() (bool, error)
}

// Coin represents a transaction input or output.
type Coin interface {
	// Confirmations returns the number of confirmations for a Coin's
	// transaction. Because a Coin can become invalid after once being
	// considered valid, this condition should be checked for during
	// confirmation counting and an error returned if this Coin is no longer
	// ready to spend. An unmined transaction should have zero confirmations. A
	// transaction in the current best block should have one confirmation. A
	// negative number can be returned if error is not nil.
	Confirmations(context.Context) (int64, error)
	// ID is the coin ID.
	ID() []byte
	// TxID is a transaction identifier for the coin.
	TxID() string
	// String is a human readable representation of the Coin.
	String() string
	// Value is the coin value.
	Value() uint64
	// FeeRate returns the transaction fee rate, in atoms/byte equivalent.
	FeeRate() uint64
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
}

// Contract is an atomic swap contract.
type Contract struct {
	Coin
	// SwapAddress is the receiving address of the swap contract.
	SwapAddress string
	// RedeemScript is the contract redeem script.
	RedeemScript []byte
	// LockTime is the refund locktime.
	LockTime time.Time
	// TxData is raw transaction data. This data is provided for some assets
	// to aid in SPV compatibility.
	TxData []byte
}

// BlockUpdate is sent over the update channel when a tip change is detected.
type BlockUpdate struct {
	Err   error
	Reorg bool
}

// ConnectionError error should be sent over the block update channel if a
// connection error is detected by the Backend.
type ConnectionError error

// NewConnectionError is a constructor for a ConnectionError.
func NewConnectionError(s string, a ...interface{}) ConnectionError {
	return ConnectionError(fmt.Errorf(s, a...))
}

// BackedAsset is a dex.Asset with a Backend.
type BackedAsset struct {
	dex.Asset
	Backend Backend
}
