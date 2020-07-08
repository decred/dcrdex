// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
)

// CoinNotFoundError is returned when a coin cannot be found, either because it
// has been spent or it never existed. This error may be returned from
// AuditContract, Refund or Redeem as those methods expect the provided coin to
// exist and be unspent.
const CoinNotFoundError = dex.ErrorKind("coin not found")

// WalletInfo is auxiliary information about an ExchangeWallet.
type WalletInfo struct {
	// Name is the display name for the currency, e.g. "Decred"
	Name string `json:"name"`
	// Units is the unit used for the smallest (integer) denomination of the
	// currency, in plural form e.g. atoms, Satoshis.
	Units string `json:"units"`
	// DefaultConfigPath is the default file path that the Wallet uses for its
	// configuration file.
	DefaultConfigPath string `json:"configpath"`
	// ConfigOpts is a slice of expected Wallet config options, with the display
	// name, config key (for parsing the option from a config file/text) and
	// description for each option. This can be used to request config info from
	// users e.g. via dynamically generated GUI forms.
	ConfigOpts []*config.Option `json:"configopts"`
	// DefaultFeeRate is the default fee rate used for withdraws, refunds, and
	// DEX fee payment transactions when optimal fee estimation is unavailable.
	DefaultFeeRate uint64 `json:"defaultfeerate"` // TODO: remove
}

// WalletConfig is the configuration settings for the wallet. WalletConfig
// is passed to the wallet constructor.
type WalletConfig struct {
	// Account is the name of the wallet account. The account and wallet must
	// already exist.
	Account string
	// FallbackFeeRate is fee rate used for withdraws, refunds, and DEX fee
	// payment transactions when optimal fee estimation is unavailable.
	FallbackFeeRate uint64
	// Settings is the key-value store of wallet connection parameters.
	Settings map[string]string
	// TipChange is a function that will be called when the blockchain monitoring
	// loop detects a new block. If the error supplied is nil, the client should
	// check the confirmations on any negotiating swaps to see if action is
	// needed. If the error is non-nil, the wallet monitoring loop encountered an
	// error while retrieving tip information.
	TipChange func(error)
}

// Wallet is a common interface to be implemented by cryptocurrency wallet
// software.
type Wallet interface {
	dex.Connector
	// Info returns a set of basic information about the wallet.
	Info() *WalletInfo
	// Balance should return the balance of the wallet, categorized by
	// available, immature, and locked. Balance takes a list of minimum
	// confirmations for which to calculate maturity, and returns a list of
	// corresponding *Balance.
	Balance() (*Balance, error)
	// Fund selects coins for use in an order. The coins will be locked, and will
	// not be returned in subsequent calls to Fund or calculated in calls to
	// Available, unless they are unlocked with ReturnCoins.
	FundOrder(uint64, *dex.Asset) (Coins, error)
	// ReturnCoins unlocks coins. This would be necessary in the case of a
	// canceled order.
	ReturnCoins(Coins) error
	// FundingCoins gets funding coins for the coin IDs. The coins are locked.
	// This method might be called to reinitialize an order from data stored
	// externally. This method will only return funding coins, e.g. unspent
	// transaction outputs.
	FundingCoins([]dex.Bytes) (Coins, error)
	// Swap sends the swaps in a single transaction. The Receipts returned can
	// be used to refund a failed transaction. The change coin is locked with
	// the wallet in case it is still required to fund chained swaps for an
	// order. The caller should unlock the change coin via ReturnCoins if it is
	// no longer required to fund additional swaps for the parent order.
	Swap(*Swaps) ([]Receipt, Coin, error)
	// Redeem sends the redemption transaction, which may contain more than one
	// redemption. The input coin IDs and the output Coin are returned.
	Redeem(redeems []*Redemption) ([]dex.Bytes, Coin, error)
	// SignMessage signs the coin ID with the private key associated with the
	// specified Coin. A slice of pubkeys required to spend the Coin and a
	// signature for each pubkey are returned.
	SignMessage(Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error)
	// AuditContract retrieves information about a swap contract on the
	// blockchain. This would be used to verify the counter-party's contract
	// during a swap. If the coin cannot be found for the coin ID, the
	// ExchangeWallet should return CoinNotFoundError. This enables the client
	// to properly handle network latency.
	AuditContract(coinID, contract dex.Bytes) (AuditInfo, error)
	// LocktimeExpired returns true if the specified contract's locktime has
	// expired, making it possible to issue a Refund.
	LocktimeExpired(contract dex.Bytes) (bool, error)
	// FindRedemption should attempt to find the input that spends the specified
	// coin, and return the secret key if it does.
	//
	// NOTE: FindRedemption is necessary to deal with the case of a maker
	// redeeming but not forwarding their redemption information. The DEX does not
	// monitor for this case. While it will result in the counter-party being
	// penalized, the input still needs to be found so the swap can be completed.
	// For typical blockchains, every input of every block starting at the
	// contract block will need to be scanned until the spending input is found.
	// This could potentially be an expensive operation if performed long after
	// the swap is broadcast. Realistically, though, the taker should start
	// looking for the maker's redemption beginning at swapconf confirmations
	// regardless of whether the server sends the 'redemption' message or not.
	FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error)
	// Refund refunds a contract. This can only be used after the time lock has
	// expired AND if the contract has not been redeemed/refunded.
	// NOTE: The contract cannot be retrieved from the unspent coin info as the
	// wallet does not store it, even though it was known when the init transaction
	// was created. The client should store this information for persistence across
	// sessions.
	Refund(coinID, contract dex.Bytes) (dex.Bytes, error)
	// Address returns an address for the exchange wallet.
	Address() (string, error)
	// Unlock unlocks the exchange wallet.
	Unlock(pw string, dur time.Duration) error
	// Lock locks the exchange wallet.
	Lock() error
	// PayFee sends the dex registration fee. Transaction fees are in addition to
	// the registration fee, and the fee rate is taken from the DEX configuration.
	PayFee(address string, feeAmt uint64) (Coin, error)
	// Confirmations gets the number of confirmations for the specified coin ID.
	// The ID need not represent an unspent coin, but coin IDs unknown to this
	// wallet may return an error.
	Confirmations(id dex.Bytes) (uint32, error)
	// Withdraw withdraws funds to the specified address. Fees are subtracted from
	// the value.
	Withdraw(address string, value uint64) (Coin, error)
	// ValidateSecret checks that the secret hashes to the secret hash.
	ValidateSecret(secret, secretHash []byte) bool
}

// Balance is categorized information about a wallet's balance.
type Balance struct {
	// Available is the balance that is available for trading immediately.
	Available uint64 `json:"available"`
	// Immature is the balance that is not ready, but will be after some
	// confirmations.
	Immature uint64 `json:"immature"`
	// Locked is the balance that that is currently locked for swap, but
	// not actually swapped yet.
	Locked uint64 `json:"locked"`
}

// Coin is some amount of spendable asset. Coin provides the information needed
// to locate the unspent value on the blockchain.
type Coin interface {
	// ID is a unique identifier for this coin.
	ID() dex.Bytes
	// String is a string representation of the coin.
	String() string
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
	// Coin is the contract's coin.
	Coin() Coin
	// String provides a human-readable representation of the contract's Coin.
	String() string
}

// AuditInfo is audit information about a swap contract needed to audit the
// contract.
type AuditInfo interface {
	// Recipient is the string-encoded recipient address.
	Recipient() string
	// Expiration is the unix timestamp of the contract time lock expiration.
	Expiration() time.Time
	// Coin is the coin that contains the contract.
	Coin() Coin
	// SecretHash is the contract's secret hash.
	SecretHash() dex.Bytes
}

// INPUT TYPES
// The types below will be used by the client as inputs for the methods exposed
// by the wallet.

// Swaps is the details needed to broadcast a swap contract(s).
type Swaps struct {
	// Inputs are the Coins being spent.
	Inputs Coins
	// Contract is the contract data.
	Contracts []*Contract
	// FeeRate is the required fee rate in atoms/byte.
	FeeRate uint64
	// LockChange can be set to true if the change should be locked for
	// subsequent matches.
	LockChange bool
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
