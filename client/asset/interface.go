// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
)

// Wallet is a common interface to be implemented by cryptocurrency wallet
// software.
type Wallet interface {
	// Available should return the total available funds in the wallet.
	// Note that after calling Fund, the amount returned by Available may change
	// by more than the value funded.
	Available() (available, locked uint64, err error)
	// Fund selects coins for use in an order. The coins will be locked, and will
	// not be returned in subsequent calls to Fund or calculated in calls to
	// Available, unless they are unlocked with ReturnCoins.
	Fund(uint64) (Coins, error)
	// ReturnCoins unlocks coins. This would be necessary in the case of a
	// canceled or partially filled order.
	ReturnCoins(Coins) error
	// Swap sends the swap transaction. The Receipts returned can be used to
	// refund a failed transaction.
	Swap(*SwapTx) ([]Receipt, error)
	// Redeem sends the redemption transaction.
	Redeem(*Redemption) error
	// SignMessage signs the message with the private key associated with the
	// specified Coin. A slice of pubkeys required to spend the Coin and a
	// signature for each pubkey are returned.
	SignMessage(Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error)
	// AuditContract retrieves information about a swap contract on the
	// blockchain. This would be used to verify the counter-party's contract
	// during a swap.
	AuditContract(string, uint32, dex.Bytes) (AuditInfo, error)
	// FindRedemption should attempt to find the input that spends the specified
	// output, and returns the secret key if it does.
	//
	// DRAFT NOTE: FindRedemption is necessary to deal with the case of a maker
	// redeeming but not forwarding their redemption information. The DEX does not
	// monitor for this case. While it will result in the counter-party being
	// penalized, the input still needs to be found so the swap can be completed.
	// For BTC-like blockchains, every input of every block starting at the
	// contract block will need to be scanned until the spending input is found.
	// This could potentially be an expensive operation if performed long after
	// the swap is broadcast. Realistically, though, the taker should start
	// looking for the maker's redemption beginning at swapconf confirmations
	// (at the latest) regardless of whether the server sends the 'redemption'
	// message or not.
	FindRedemption(ctx context.Context, txid string, vout uint32) (dex.Bytes, error)
	// Refund refunds a contract. This can only be used after the time lock has
	// expired.
	Refund(Receipt) error
	// Address returns an address for the exchange wallet.
	Address() (string, error)
	// Unlock unlocks the exchange wallet.
	Unlock(pw string, dur time.Duration) error
	// Lock locks the exchange wallet.
	Lock() error
}

// Coin is some amount of spendable asset. Coin provides the information needed
// to locate the unspent value on the blockchain.
type Coin interface {
	// ID is a unique identifier for this output, likely a txid.
	ID() string
	// Index is a secondary identifier needed to locate this coin.
	Index() uint32
	// Value is the available quantity, in atoms/satoshi.
	Value() uint64
	// Confirmations is the number of confirmations on this Coin's block. If the
	// coin becomes spent, Confirmations should return an error.
	Confirmations() (uint32, error)
	// Redeem is any redeem script required to spend the coin.
	Redeem() dex.Bytes
}

// Coins a collection of coins as returned by Fund.
type Coins []Coin

// Receipt holds information about a sent swap contract.
type Receipt interface {
	// Expiration is the time lock expiration.
	Expiration() time.Time
	// Coin is the contract output.
	Coin() Coin
}

// AuditInfo is audit information about a swap contract needed to audit the
// contract.
type AuditInfo interface {
	// Recipient is the string-encoded recipient address.
	Recipient() string
	// Expiration is the unix timestamp of the contract time lock expiration.
	Expiration() time.Time
	// Coin is the output that contains the contract.
	Coin() Coin
}

// INPUT TYPES
// The types below will be used by the client as inputs for the methods exposed
// by the wallet.

// SwapTx is the details needed to broadcast some number of swap contracts.
type SwapTx struct {
	// Inputs are the Coins being spent.
	Inputs Coins
	// Contracts are the SwapContracts to include in this SwapTx.
	Contracts []*Contract
}

// Contract is a swap contract.
type Contract struct {
	// Address is the receiving address.
	Address string
	// Value is the amount being traded.
	Value uint64
	// SecretHash is the hash of the secret key.
	SecretHash dex.Bytes
	// LockTime is the contract lock time.
	LockTime uint64
}

// Redemption is a redemption transaction that spends a counter-party's swap
// contract.
type Redemption struct {
	// Spends is the AuditInfo for the swap output being spent.
	Spends AuditInfo
	// Secret is the secret key needed to satisfy the swap contract.
	Secret dex.Bytes
}
