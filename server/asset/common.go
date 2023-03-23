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
	NextAddress() (string, error)
}

// KeyIndexer retrieves and stores child indexes for an extended public key.
type KeyIndexer interface {
	KeyIndex(xpub string) (uint32, error)
	SetKeyIndex(idx uint32, xpub string) error
}

// BackendInfo provides auxiliary information about a backend.
type BackendInfo struct {
	SupportsDynamicTxFee bool
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
	// the blockchain immediately. Contract data (e.g. the redeem script for
	// UTXO assets) is required in order to calculate sigScript length and
	// verify pubkeys.
	Contract(coinID []byte, contractData []byte) (*Contract, error)
	// TxData fetches the raw transaction data for the specified coin.
	TxData(coinID []byte) ([]byte, error)
	// ValidateSecret checks that the secret satisfies the contract.
	ValidateSecret(secret, contractData []byte) bool
	// Redemption returns a Coin for redemptionID, a transaction input, that
	// spends contract ID, an output containing the swap contract.
	Redemption(redemptionID, contractID, contractData []byte) (Coin, error)
	// BlockChannel creates and returns a new channel on which to receive updates
	// when new blocks are connected.
	BlockChannel(size int) <-chan *BlockUpdate
	// InitTxSize is the size of a serialized atomic swap initialization
	// transaction with 1 input spending a P2PKH utxo, 1 swap contract output and
	// 1 change output.
	InitTxSize() uint32
	// InitTxSizeBase is InitTxSize not including an input.
	InitTxSizeBase() uint32
	// CheckSwapAddress checks that the given address is parseable, and suitable
	// as a redeem address in a swap contract script or initiation.
	CheckSwapAddress(string) bool
	// ValidateCoinID checks the coinID to ensure it can be decoded, returning a
	// human-readable string if it is valid.
	// Note: ValidateCoinID is NOT used for funding coin IDs for account-based
	// assets. This rule is only enforced by code patterns right now, but we may
	// consider adding separate methods in the future.
	ValidateCoinID(coinID []byte) (string, error)
	// ValidateContract ensures that the swap contract is constructed properly
	// for the asset.
	ValidateContract(contract []byte) error
	// FeeRate returns the current optimal fee rate in atoms / byte.
	FeeRate(context.Context) (uint64, error)
	// Synced should return true when the blockchain is synced and ready for
	// fee rate estimation.
	Synced() (bool, error)
	// Info provides auxiliary information about a backend.
	Info() *BackendInfo
	// ValidateFeeRate checks that the transaction fees used to initiate the
	// contract are sufficient.
	ValidateFeeRate(contract *Contract, reqFeeRate uint64) bool
}

// OutputTracker is implemented by backends for UTXO-based blockchains.
// OutputTracker tracks the value and spend-status of transaction outputs.
type OutputTracker interface {
	// VerifyUnspentCoin attempts to verify a coin ID by decoding the coin ID
	// and retrieving the corresponding Coin. If the coin is not found or no
	// longer unspent, an asset.CoinNotFoundError is returned. Use FundingCoin
	// for more UTXO data.
	VerifyUnspentCoin(ctx context.Context, coinID []byte) error
	// FundingCoin returns the unspent coin at the specified location. Coins
	// with non-standard pkScripts or scripts that require zero signatures to
	// redeem must return an error.
	FundingCoin(ctx context.Context, coinID []byte, redeemScript []byte) (FundingCoin, error)
}

// AccountBalancer is implemented by backends for account-based blockchains.
// An AccountBalancer reports the current balance for an account.
type AccountBalancer interface {
	// AccountBalance retrieves the current account balance.
	AccountBalance(addr string) (uint64, error)
	// ValidateSignature checks that the pubkey is correct for the address and
	// that the signature shows ownership of the associated private key.
	// IMPORTANT: As part of signature validation, the asset backend should
	// validate the address against a STRICT standard. Case, prefixes, suffixes,
	// etc. must be exactly the same order-to-order, since the address string
	// is used as a key for various accounting operations throughout DEX.
	ValidateSignature(addr string, pubkey, msg, sig []byte) error
	// RedeemSize is the gas used for a single redemption.
	RedeemSize() uint64
}

// TokenBacker is implemented by Backends that support degenerate tokens.
type TokenBacker interface {
	TokenBackend(assetID uint32, configPath string) (Backend, error)
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
	// ContractData is essential data about this swap. For example, the redeem
	// script for UTXO contracts, or a secret hash that keys swaps for account-
	// based contracts.
	ContractData []byte
	// SecretHash is the secret key hash used for this swap. This should be used
	// to validate a counterparty contract on another chain.
	SecretHash []byte
	// LockTime is the refund locktime.
	LockTime time.Time
	// TxData is raw transaction data. This data is provided for some assets
	// to aid in SPV compatibility.
	TxData []byte
}

// BlockUpdate is sent over the update channel when a tip change is detected.
type BlockUpdate struct {
	Err   error
	Reorg bool // TODO: Not used. Remove and update through btc and dcr. Don't really need the BlockUpdate struct at all.
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
