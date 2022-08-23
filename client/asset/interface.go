// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
)

// WalletTrait is a bitset indicating various optional wallet features, such as
// the presence of auxiliary methods like Rescan, Withdraw and Sweep.
type WalletTrait uint64

const (
	WalletTraitRescanner    WalletTrait = 1 << iota // The Wallet is an asset.Rescanner.
	WalletTraitNewAddresser                         // The Wallet can generate new addresses on demand with NewAddress.
	WalletTraitLogFiler                             // The Wallet allows for downloading of a log file.
	WalletTraitFeeRater                             // Wallet can provide a fee rate for non-critical transactions
	WalletTraitAccelerator                          // This wallet can accelerate transactions using the CPFP technique
	WalletTraitRecoverer                            // The wallet is an asset.Recoverer.
	WalletTraitWithdrawer                           // The Wallet can withdraw a specific amount from an exchange wallet.
	WalletTraitSweeper                              // The Wallet can sweep all the funds, leaving no change.
	WalletTraitRestorer                             // The wallet is an asset.WalletRestorer
)

// IsRescanner tests if the WalletTrait has the WalletTraitRescanner bit set.
func (wt WalletTrait) IsRescanner() bool {
	return wt&WalletTraitRescanner != 0
}

// IsNewAddresser tests if the WalletTrait has the WalletTraitNewAddresser bit
// set, which indicates the presence of a NewAddress method that will generate a
// new address on each call. If this method does not exist, the Address method
// should be assumed to always return the same deposit address.
func (wt WalletTrait) IsNewAddresser() bool {
	return wt&WalletTraitNewAddresser != 0
}

// IsLogFiler tests if WalletTrait has the WalletTraitLogFiler bit set.
func (wt WalletTrait) IsLogFiler() bool {
	return wt&WalletTraitLogFiler != 0
}

// IsFeeRater tests if the WalletTrait has the WalletTraitFeeRater bit set,
// which indicates the presence of a FeeRate method.
func (wt WalletTrait) IsFeeRater() bool {
	return wt&WalletTraitFeeRater != 0
}

// IsAccelerator tests if the WalletTrait has the WalletTraitAccelerator bit set,
// which indicates the presence of an Accelerate method.
func (wt WalletTrait) IsAccelerator() bool {
	return wt&WalletTraitAccelerator != 0
}

// IsRecoverer tests if the WalletTrait has the WalletTraitRecoverer bit set,
// which indicates the wallet implements the Recoverer interface.
func (wt WalletTrait) IsRecoverer() bool {
	return wt&WalletTraitRecoverer != 0
}

// IsWithdrawer tests if the WalletTrait has the WalletTraitSender bit set,
// which indicates the presence of a Withdraw method.
func (wt WalletTrait) IsWithdrawer() bool {
	return wt&WalletTraitWithdrawer != 0
}

// IsSweeper test if the WalletTrait has the WalletTraitSweeper bit set, which
// indicates the presence of a Sweep method.
func (wt WalletTrait) IsSweeper() bool {
	return wt&WalletTraitSweeper != 0
}

// IsRestorer test if the WalletTrait has the WalletTraitRestorer bit set, which
// indicates the wallet implements the WalletRestorer interface.
func (wt WalletTrait) IsRestorer() bool {
	return wt&WalletTraitRestorer != 0
}

// DetermineWalletTraits returns the WalletTrait bitset for the provided Wallet.
func DetermineWalletTraits(w Wallet) (t WalletTrait) {
	if _, is := w.(Rescanner); is {
		t |= WalletTraitRescanner
	}
	if _, is := w.(NewAddresser); is {
		t |= WalletTraitNewAddresser
	}
	if _, is := w.(LogFiler); is {
		t |= WalletTraitLogFiler
	}
	if _, is := w.(FeeRater); is {
		t |= WalletTraitFeeRater
	}
	if _, is := w.(Accelerator); is {
		t |= WalletTraitAccelerator
	}
	if _, is := w.(Recoverer); is {
		t |= WalletTraitRecoverer
	}
	if _, is := w.(Withdrawer); is {
		t |= WalletTraitWithdrawer
	}
	if _, is := w.(Sweeper); is {
		t |= WalletTraitSweeper
	}
	if _, is := w.(WalletRestorer); is {
		t |= WalletTraitRestorer
	}
	return t
}

// CoinNotFoundError is returned when a coin cannot be found, either because it
// has been spent or it never existed. This error may be returned from
// AuditContract, Refund or Redeem as those methods expect the provided coin to
// exist and be unspent.
const (
	// ErrSwapNotInitiated most likely means that a swap using a contract has
	// not yet been mined. There is no guarantee that the swap will be mined
	// in the future.
	ErrSwapNotInitiated = dex.ErrorKind("swap not yet initiated")
	CoinNotFoundError   = dex.ErrorKind("coin not found")
	ErrRequestTimeout   = dex.ErrorKind("request timeout")
	ErrConnectionDown   = dex.ErrorKind("wallet not connected")
	ErrNotImplemented   = dex.ErrorKind("not implemented")
	ErrUnsupported      = dex.ErrorKind("unsupported")

	// InternalNodeLoggerName is the name for a logger that is used to fine
	// tune log levels for only loggers using this name.
	InternalNodeLoggerName = "INTL"
)

type WalletDefinition struct {
	// If seeded is true, the Create method will be called with a deterministic
	// seed that should be used to set the wallet key(s). This would be
	// true for built-in wallets.
	Seeded bool `json:"seeded"`
	// Type is a string identifying the wallet type. NOTE: There should be a
	// particular WalletTrait set for any given Type, but wallet construction is
	// presently required to discern traits.
	Type string `json:"type"`
	// Tab is a displayable string for the wallet type. One or two words. First
	// word capitalized. Displayed on a wallet selection tab.
	Tab string `json:"tab"`
	// Description is a short description of the wallet, suitable for a tooltip.
	Description string `json:"description"`
	// DefaultConfigPath is the default file path that the Wallet uses for its
	// configuration file. Probably only useful for unseeded / external wallets.
	DefaultConfigPath string `json:"configpath"`
	// ConfigOpts is a slice of expected Wallet config options, with the display
	// name, config key (for parsing the option from a config file/text) and
	// description for each option. This can be used to request config info from
	// users e.g. via dynamically generated GUI forms.
	ConfigOpts []*ConfigOption `json:"configopts"`
	// NoAuth can be set true to hide the wallet password field during wallet
	// creation.
	// TODO: Use an asset.Authenticator interface and WalletTraits to do this
	// instead.
	NoAuth bool `json:"noauth"`
}

// Token combines the generic dex.Token with a WalletDefinition.
type Token struct {
	*dex.Token
	Definition *WalletDefinition `json:"definition"`
}

// WalletInfo is auxiliary information about an ExchangeWallet.
type WalletInfo struct {
	// Name is the display name for the currency, e.g. "Decred"
	Name string `json:"name"`
	// Version is the Wallet's version number, which is used to signal when
	// major changes are made to internal details such as coin ID encoding and
	// contract structure that must be common to a server's.
	Version uint32 `json:"version"`
	// AvailableWallets is an ordered list of available WalletDefinition. The
	// first WalletDefinition is considered the default, and might, for instance
	// be the initial form offered to the user for configuration, with others
	// available to select.
	AvailableWallets []*WalletDefinition `json:"availablewallets"`
	// LegacyWalletIndex should be set for assets that existed before wallets
	// were typed. The index should point to the WalletDefinition that should
	// be assumed when the type is provided as an empty string.
	LegacyWalletIndex int `json:"emptyidx"`
	// UnitInfo is the information about unit names and conversion factors for
	// the asset.
	UnitInfo dex.UnitInfo `json:"unitinfo"`
}

// ConfigOption is a wallet configuration option.
type ConfigOption struct {
	Key          string      `json:"key"`
	DisplayName  string      `json:"displayname"`
	Description  string      `json:"description"`
	DefaultValue interface{} `json:"default"`
	// If MaxValue/MinValue are set to the string "now" for a date config, the
	// UI will display the current date.
	MaxValue          interface{} `json:"max"`
	MinValue          interface{} `json:"min"`
	NoEcho            bool        `json:"noecho"`
	IsBoolean         bool        `json:"isboolean"`
	IsDate            bool        `json:"isdate"`
	DisableWhenActive bool        `json:"disablewhenactive"`
	IsBirthdayConfig  bool        `json:"isBirthdayConfig"`
}

const (
	// SpecialSettingActivelyUsed is a special setting that can be injected by
	// core that lets the wallet know that it is being actively used. A use
	// case is by the bitcoin SPV wallet to decide whether or not it is safe
	// to do a full rescan.
	SpecialSettingActivelyUsed = "special_activelyUsed"
)

// WalletConfig is the configuration settings for the wallet. WalletConfig
// is passed to the wallet constructor.
type WalletConfig struct {
	// Type is the type of wallet, corresponding to the Type field of an
	// available WalletDefinition.
	Type string
	// Settings is the key-value store of wallet connection parameters. The
	// Settings are supplied by the user according the the WalletInfo's
	// ConfigOpts.
	Settings map[string]string
	// TipChange is a function that will be called when the blockchain
	// monitoring loop detects a new block. If the error supplied is nil, the
	// client should check the confirmations on any negotiating swaps to see if
	// action is needed. If the error is non-nil, the wallet monitoring loop
	// encountered an error while retrieving tip information. This function
	// should not be blocking, and Wallet implementations should not rely on any
	// specific side effect of the function call.
	TipChange func(error)
	// PeersChange is a function that will be called when the number of
	// wallet/node peers changes, or the wallet fails to get the count. This
	// should not be called prior to Connect of the constructed wallet.
	PeersChange func(uint32, error)
	// DataDir is a filesystem directory the the wallet may use for persistent
	// storage.
	DataDir string
}

// Wallet is a common interface to be implemented by cryptocurrency wallet
// software.
type Wallet interface {
	// It should be assumed that once disconnected, subsequent Connect calls
	// will fail, requiring a new Wallet instance.
	dex.Connector
	// Info returns a set of basic information about the wallet driver.
	Info() *WalletInfo
	// Balance should return the balance of the wallet, categorized by
	// available, immature, and locked. Balance takes a list of minimum
	// confirmations for which to calculate maturity, and returns a list of
	// corresponding *Balance.
	Balance() (*Balance, error)
	// FundOrder selects coins for use in an order. The coins will be locked,
	// and will not be returned in subsequent calls to FundOrder or calculated
	// in calls to Available, unless they are unlocked with ReturnCoins. The
	// returned []dex.Bytes contains the redeem scripts for the selected coins.
	// Equal number of coins and redeemed scripts must be returned. A nil or
	// empty dex.Bytes should be appended to the redeem scripts collection for
	// coins with no redeem script.
	FundOrder(*Order) (coins Coins, redeemScripts []dex.Bytes, err error)
	// MaxOrder generates information about the maximum order size and
	// associated fees that the wallet can support for the specified DEX. The
	// fees are an estimate based on current network conditions, and will be <=
	// the fees associated with the Asset.MaxFeeRate. For quote assets, lotSize
	// will be an estimate based on current market conditions. lotSize should
	// not be zero.
	MaxOrder(*MaxOrderForm) (*SwapEstimate, error)
	// PreSwap gets a pre-swap estimate for the specified order size.
	PreSwap(*PreSwapForm) (*PreSwap, error)
	// PreRedeem gets a pre-redeem estimate for the specified order size.
	PreRedeem(*PreRedeemForm) (*PreRedeem, error)
	// ReturnCoins unlocks coins. This would be necessary in the case of a
	// canceled order.
	ReturnCoins(Coins) error
	// FundingCoins gets funding coins for the coin IDs. The coins are locked.
	// This method might be called to reinitialize an order from data stored
	// externally. This method will only return funding coins, e.g. unspent
	// transaction outputs.
	FundingCoins([]dex.Bytes) (Coins, error)
	// Swap sends the swaps in a single transaction. The Receipts returned can
	// be used to refund a failed transaction. The Input coins are unlocked
	// where necessary to ensure accurate balance reporting in cases where the
	// wallet includes spent coins as part of the locked balance just because
	// they were previously locked.
	Swap(*Swaps) (receipts []Receipt, changeCoin Coin, feesPaid uint64, err error)
	// Redeem sends the redemption transaction, which may contain more than one
	// redemption. The input coin IDs and the output Coin are returned.
	Redeem(redeems *RedeemForm) (ins []dex.Bytes, out Coin, feesPaid uint64, err error)
	// SignMessage signs the coin ID with the private key associated with the
	// specified Coin. A slice of pubkeys required to spend the Coin and a
	// signature for each pubkey are returned.
	SignMessage(Coin, dex.Bytes) (pubkeys, sigs []dex.Bytes, err error)
	// AuditContract retrieves information about a swap contract from the
	// provided txData and broadcasts the txData to ensure the contract is
	// propagated to the blockchain. The information returned would be used
	// to verify the counter-party's contract during a swap. It is not an
	// error if the provided txData cannot be broadcasted because it may
	// already be broadcasted. A successful audit response does not mean
	// the tx exists on the blockchain, use SwapConfirmations to ensure
	// the tx is mined.
	AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*AuditInfo, error)
	// LocktimeExpired returns true if the specified contract's locktime has
	// expired, making it possible to issue a Refund. The contract expiry time
	// is also returned, but reaching this time does not necessarily mean the
	// contract can be refunded since assets have different rules to satisfy the
	// lock. For example, in Bitcoin the median of the last 11 blocks must be
	// past the expiry time, not the current time.
	LocktimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error)
	// FindRedemption watches for the input that spends the specified
	// coin and contract, and returns the spending input and the
	// secret key when it finds a spender.
	//
	// For typical utxo-based blockchains, every input of every block tx
	// (starting at the contract block) will need to be scanned until a spending
	// input is found.
	//
	// FindRedemption is necessary to deal with the case of a maker redeeming
	// but not forwarding their redemption information. The DEX does not monitor
	// for this case. While it will result in the counter-party being penalized,
	// the input still needs to be found so the swap can be completed.
	//
	// NOTE: This could potentially be a long and expensive operation if
	// performed long after the swap is broadcast; might be better executed from
	// a goroutine.
	FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error)
	// Refund refunds a contract. This can only be used after the time lock has
	// expired AND if the contract has not been redeemed/refunded. This method
	// MUST return an asset.CoinNotFoundError error if the swap is already
	// spent, which is used to indicate if FindRedemption should be used and the
	// counterparty's swap redeemed. NOTE: The contract cannot be retrieved from
	// the unspent coin info as the wallet does not store it, even though it was
	// known when the init transaction was created. The client should store this
	// information for persistence across sessions.
	Refund(coinID, contract dex.Bytes, feeRate uint64) (dex.Bytes, error)
	// DepositAddress returns an address for depositing funds into the exchange
	// wallet.
	DepositAddress() (string, error)
	// OwnsDepositAddress indicates if the provided address can be used
	// to deposit funds into the wallet.
	OwnsDepositAddress(address string) (bool, error)
	// RedemptionAddress gets an address for use in redeeming the counterparty's
	// swap. This would be included in their swap initialization. This is
	// presently called when each trade order is *submitted*. If these are
	// unique addresses and the orders are canceled, the gap limit may become a
	// hurdle when restoring the wallet.
	RedemptionAddress() (string, error)
	// Unlock unlocks the exchange wallet.
	Unlock(pw []byte) error
	// Lock locks the exchange wallet.
	Lock() error
	// Locked will be true if the wallet is currently locked.
	Locked() bool
	// SwapConfirmations gets the number of confirmations and the spend status
	// for the specified swap. If the swap was not funded by this wallet, and
	// it is already spent, you may see CoinNotFoundError.
	// If the coin is located, but recognized as spent, no error is returned.
	// If the contract is already redeemed or refunded, the confs value may not
	// be accurate.
	// The contract and matchTime are provided so that wallets may search for
	// the coin using light filters.
	SwapConfirmations(ctx context.Context, coinID dex.Bytes, contract dex.Bytes, matchTime time.Time) (confs uint32, spent bool, err error)
	// ValidateSecret checks that the secret hashes to the secret hash.
	ValidateSecret(secret, secretHash []byte) bool
	// SyncStatus is information about the blockchain sync status. It should
	// only indicate synced when there are network peers and all blocks on the
	// network have been processed by the wallet.
	SyncStatus() (synced bool, progress float32, err error)
	// RegFeeConfirmations gets the confirmations for a registration fee
	// payment. This method need not be supported by all assets. Those assets
	// which do no support DEX registration fees will return an ErrUnsupported.
	RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error)
	// Send sends the exact value to the specified address. This is different
	// from Withdraw, which subtracts the tx fees from the amount sent.
	Send(address string, value, feeRate uint64) (Coin, error)
	// EstimateRegistrationTxFee returns an estimate for the tx fee needed to
	// pay the registration fee using the provided feeRate.
	EstimateRegistrationTxFee(feeRate uint64) uint64
}

// Rescanner is a wallet implementation with rescan functionality.
type Rescanner interface {
	Rescan(ctx context.Context) error
}

// Recoverer is a wallet implementation with recover functionality.
type Recoverer interface {
	// GetRecoveryCfg returns information that will help the wallet get back to
	// its previous state after it is recreated.
	GetRecoveryCfg() (map[string]string, error)
	// Move will move all wallet files to a backup directory so the wallet can
	// be recreated.
	Move(backupdir string) error
}

// Withdrawer is a wallet that can withdraw a certain amount from the
// source wallet/account.
type Withdrawer interface {
	// Withdraw withdraws funds to the specified address. Fees are subtracted
	// from the value.
	Withdraw(address string, value, feeRate uint64) (Coin, error)
}

// Sweeper is a wallet that can clear the entire balance of the wallet/account
// to an address. Similar to Withdraw, but no input value is required.
type Sweeper interface {
	Sweep(address string, feeRate uint64) (Coin, error)
}

// NewAddresser is a wallet that can generate new deposit addresses.
type NewAddresser interface {
	NewAddress() (string, error)
}

// LogFiler is a wallet that allows for downloading of its log file.
type LogFiler interface {
	LogFilePath() string
}

// FeeRater is capable of retrieving a non-critical fee rate estimate for an
// asset. Some SPV wallets, for example, cannot provide a fee rate estimate, so
// shouldn't implement FeeRater. However, since the mode of external wallets may
// not be known on construction, only connect, a zero rate may be returned. The
// caller should always check for zero and have a fallback rate. The rates from
// FeeRate should be used for rates that are not validated by the server
// Withdraw and Send, and will/should not be used to generate a fee
// suggestion for swap operations.
type FeeRater interface {
	FeeRate() uint64
}

// WalletRestoration contains all the information needed for a user to restore
// their wallet in an external wallet.
type WalletRestoration struct {
	Target string `json:"target"`
	Seed   string `json:"seed"`
	// SeedName is the name of the seed used for this particular wallet, i.e
	// Private Key.
	SeedName     string `json:"seedName"`
	Instructions string `json:"instructions"`
}

// WalletRestorer is a wallet which gives information about how to restore
// itself in external wallet software.
type WalletRestorer interface {
	// RestorationInfo returns information about how to restore the wallet in
	// various external wallets.
	RestorationInfo(seed []byte) ([]*WalletRestoration, error)
}

// EarlyAcceleration is returned from the PreAccelerate function to inform the
// user that either their last acceleration or oldest swap transaction happened
// very recently, and that they should double check that they really want to do
// an acceleration.
type EarlyAcceleration struct {
	// TimePast is the amount of seconds that has past since either the previous
	// acceleration, or the oldest unmined swap transaction was submitted to
	// the blockchain.
	TimePast uint64 `json:"timePast"`
	// WasAccelerated is true if the action that took place TimePast seconds
	// ago was an acceleration. If false, the oldest unmined swap transaction
	// in the order was submitted TimePast seconds ago.
	WasAccelerated bool `json:"wasAccelerated"`
}

// Accelerator is implemented by wallets which support acceleration of the
// mining of swap transactions.
type Accelerator interface {
	// AccelerateOrder uses the Child-Pays-For-Parent technique to accelerate a
	// chain of swap transactions and previous accelerations. It broadcasts a new
	// transaction with a fee high enough so that the average fee of all the
	// unconfirmed transactions in the chain and the new transaction will have
	// an average fee rate of newFeeRate. The changeCoin argument is the latest
	// change in the order. It must be the input in the acceleration transaction
	// in order for the order to be accelerated. requiredForRemainingSwaps is the
	// amount of funds required to complete the rest of the swaps in the order.
	// The change output of the acceleration transaction will have at least
	// this amount.
	//
	// The returned change coin may be nil, and should be checked before use.
	AccelerateOrder(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (Coin, string, error)
	// AccelerationEstimate takes the same parameters as AccelerateOrder, but
	// instead of broadcasting the acceleration transaction, it just returns
	// the amount of funds that will need to be spent in order to increase the
	// average fee rate to the desired amount.
	AccelerationEstimate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (uint64, error)
	// PreAccelerate returns the current average fee rate of the unmined swap
	// initiation and acceleration transactions, and also returns a suggested
	// range that the fee rate should be increased to in order to expedite mining.
	// The feeSuggestion argument is the current prevailing network rate. It is
	// used to help determine the suggestedRange, which is a range meant to give
	// the user a good amount of flexibility in determining the post acceleration
	// effective fee rate, but still not allowing them to pick something
	// outrageously high.
	PreAccelerate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *XYRange, *EarlyAcceleration, error)
}

// TokenMaster is implemented by assets which support degenerate tokens.
type TokenMaster interface {
	// CreateTokenWallet creates a wallet for the specified token asset. The
	// settings correspond to the Token.Definition.ConfigOpts.
	CreateTokenWallet(assetID uint32, settings map[string]string) error
	// OpenTokenWallet opens a wallet for the specified token asset. The
	// settings correspond to the Token.Definition.ConfigOpts. The tipChange
	// function will be called after the parent's tipChange function.
	OpenTokenWallet(assetID uint32, settings map[string]string, tipChange func(error)) (Wallet, error)
}

// AccountLocker is a wallet in which redemptions and refunds require a wallet
// to have available balance to pay fees.
type AccountLocker interface {
	// ReserveNRedemption is used when preparing funding for an order that
	// redeems to an account-based asset. The wallet will set aside the
	// appropriate amount of funds so that we can redeem N swaps using the fee
	// and version configuration specified in the dex.Asset. It is an error to
	// request funds > spendable balance.
	ReserveNRedemptions(n uint64, dexRedeemCfg *dex.Asset) (uint64, error)
	// ReReserveRedemption is used when reconstructing existing orders on
	// startup. It is an error to request funds > spendable balance.
	ReReserveRedemption(amt uint64) error
	// UnlockRedemptionReserves is used to return funds reserved for redemption
	// when an order is canceled or otherwise completed unfilled.
	UnlockRedemptionReserves(uint64)
	// ReserveNRefunds is used when preparing funding for an order that refunds
	// to an account-based asset. The wallet will set aside the appropriate
	// amount of funds so that we can refund N swaps using the fee and version
	// configuration specified in the dex.Asset. It is an error to request funds
	// > spendable balance.
	ReserveNRefunds(n uint64, dexSwapCfg *dex.Asset) (uint64, error)
	// ReReserveRefund is used when reconstructing existing orders on
	// startup. It is an error to request funds > spendable balance.
	ReReserveRefund(uint64) error
	// UnlockRefundReserves is used to return funds reserved for refunds
	// when an order was cancelled or revoked before a swap was initiated,
	// completed successully, or after a refund was done.
	UnlockRefundReserves(uint64)
}

// LiveReconfigurer is a wallet that can possibly handle a reconfiguration
// without the need for re-initialization.
type LiveReconfigurer interface {
	// Reconfigure attempts to reconfigure the wallet. If reconfiguration
	// requires a restart, the Wallet should still validate as much
	// configuration as possible.
	Reconfigure(ctx context.Context, cfg *WalletConfig, currentAddress string) (restartRequired bool, err error)
}

// Balance is categorized information about a wallet's balance.
type Balance struct {
	// Available is the balance that is available for trading immediately.
	Available uint64 `json:"available"`
	// Immature is the balance that is not ready, but will be after some
	// confirmations.
	Immature uint64 `json:"immature"`
	// Locked is the total amount locked in the wallet which includes but
	// is not limited to funds locked for swap but not actually swapped yet.
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
}

type RecoveryCoin interface {
	// RecoveryID is an ID that can be used to re-establish funding state during
	// startup. If a Coin implements RecoveryCoin, the RecoveryID will be used
	// in the database record, and ultimately passed to the FundingCoins method.
	RecoveryID() dex.Bytes
}

// Coins a collection of coins as returned by Fund.
type Coins []Coin

// Receipt holds information about a sent swap contract.
type Receipt interface {
	// Expiration is the time lock expiration.
	Expiration() time.Time
	// Coin is the swap initiation transaction's Coin.
	Coin() Coin
	// Contract is the unique swap contract data. This may be a redeem script
	// for UTXO assets, or other information that uniquely identifies the swap
	// for account-based assets e.g. a contract version and secret hash for ETH.
	Contract() dex.Bytes
	// String provides a human-readable representation of the swap that may
	// provide supplementary data to locate the swap.
	String() string
	// SignedRefund is a signed refund script that can be used to return
	// funds to the user in the case a contract expires.
	SignedRefund() dex.Bytes
}

// AuditInfo is audit information about a swap contract needed to audit the
// contract.
type AuditInfo struct {
	// Recipient is the string-encoded recipient address.
	Recipient string
	// Expiration is the unix timestamp of the contract time lock expiration.
	Expiration time.Time
	// Coin is the coin that contains the contract.
	Coin Coin
	// Contract is the unique swap contract data. This may be a redeem script
	// for UTXO assets, or other information that uniquely identifies the swap
	// for account-based assets e.g. a contract version and secret hash for ETH.
	Contract dex.Bytes
	// SecretHash is the contract's secret hash. This is likely to be encoded in
	// the Contract field, which is often the redeem script or an asset-specific
	// encoding of the unique swap data.
	SecretHash dex.Bytes
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
	// AssetConfig contains the asset version and fee configuration for the DEX.
	// NOTE: Only one Config field is supported, so only orders from the same
	// host can be batched. We could consider moving this field to the Contract
	// and Wallets could batch compatible swaps internally.
	AssetConfig *dex.Asset
	// Options are OrderOptions set or selected by the user at order time.
	Options map[string]string
}

// Contract is a swap contract.
type Contract struct {
	// Address is the receiving address.
	Address string
	// Value is the amount being traded.
	Value uint64
	// SecretHash is the hash of the secret key.
	SecretHash dex.Bytes
	// LockTime is the contract lock time in UNIX seconds.
	LockTime uint64
}

// Redemption is a redemption transaction that spends a counter-party's swap
// contract.
type Redemption struct {
	// Spends is the AuditInfo for the swap output being spent.
	Spends *AuditInfo
	// Secret is the secret key needed to satisfy the swap contract.
	Secret dex.Bytes
}

// RedeemForm is a group of Redemptions. The struct will be
// expanded in in-progress work to accommodate order-time options.
type RedeemForm struct {
	Redemptions []*Redemption
	// FeeSuggestion is a suggested fee rate. For redemptions, the suggestion is
	// just a fallback if an internal estimate using the wallet's redeem confirm
	// block target setting is not available. Since this is the redemption,
	// there is no obligation on the client to use the fee suggestion in any
	// way, but obviously fees that are too low may result in the redemption
	// getting stuck in mempool.
	FeeSuggestion uint64
	Options       map[string]string
}

// Order is order details needed for FundOrder.
type Order struct {
	// Value is the amount required to satisfy the order. The Value does not
	// include fees. Fees will be calculated internally based on the number of
	// possible swaps (MaxSwapCount) and the exchange's configuration
	// (DEXConfig).
	Value uint64
	// MaxSwapCount is the number of lots in the order, which is also the
	// maximum number of transaction that an order could potentially generate
	// in a worst-case scenario of all 1-lot matches. Note that if requesting
	// funding for the quote asset's wallet, the number of lots will not be
	// Value / DEXConfig.LotSize, because an order is quantified in the base
	// asset, so lots is always (order quantity) / (base asset lot size).
	MaxSwapCount uint64 // uint64 for compatibility with quantity and lot size.
	// DEXConfig holds values specific to and provided by a particular server.
	// Info about fee rates and swap transaction sizes is used internally to
	// calculate the funding required to cover fees.
	DEXConfig    *dex.Asset
	RedeemConfig *dex.Asset
	// Immediate should be set to true if this is for an order that is not a
	// standing order, likely a market order or a limit order with immediate
	// time-in-force.
	Immediate bool
	// FeeSuggestion is a suggested fee from the server. If a split transaction
	// is used, the fee rate used should be at least the suggested fee, else
	// zero-conf coins might be rejected.
	FeeSuggestion uint64
	// Options are options that corresponds to PreSwap.Options, as well as
	// their values.
	Options map[string]string
}
