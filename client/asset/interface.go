// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package asset

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// WalletTrait is a bitset indicating various optional wallet features, such as
// the presence of auxiliary methods like Rescan, Withdraw and Sweep.
type WalletTrait uint64

const (
	WalletTraitRescanner      WalletTrait = 1 << iota // The Wallet is an asset.Rescanner.
	WalletTraitNewAddresser                           // The Wallet can generate new addresses on demand with NewAddress.
	WalletTraitLogFiler                               // The Wallet allows for downloading of a log file.
	WalletTraitFeeRater                               // Wallet can provide a fee rate for non-critical transactions
	WalletTraitAccelerator                            // This wallet can accelerate transactions using the CPFP technique
	WalletTraitRecoverer                              // The wallet is an asset.Recoverer.
	WalletTraitWithdrawer                             // The Wallet can withdraw a specific amount from an exchange wallet.
	WalletTraitSweeper                                // The Wallet can sweep all the funds, leaving no change.
	WalletTraitRestorer                               // The wallet is an asset.WalletRestorer
	WalletTraitTxFeeEstimator                         // The wallet can estimate transaction fees.
	WalletTraitPeerManager                            // The wallet can manage its peers.
	WalletTraitAuthenticator                          // The wallet require authentication.
	WalletTraitShielded                               // The wallet is ShieldedWallet (e.g. ZCash)
	WalletTraitTokenApprover                          // The wallet is a TokenApprover
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

// IsSweeper tests if the WalletTrait has the WalletTraitSweeper bit set, which
// indicates the presence of a Sweep method.
func (wt WalletTrait) IsSweeper() bool {
	return wt&WalletTraitSweeper != 0
}

// IsRestorer tests if the WalletTrait has the WalletTraitRestorer bit set, which
// indicates the wallet implements the WalletRestorer interface.
func (wt WalletTrait) IsRestorer() bool {
	return wt&WalletTraitRestorer != 0
}

// IsTxFeeEstimator tests if the WalletTrait has the WalletTraitTxFeeEstimator
// bit set, which indicates the wallet implements the TxFeeEstimator interface.
func (wt WalletTrait) IsTxFeeEstimator() bool {
	return wt&WalletTraitTxFeeEstimator != 0
}

// IsPeerManager tests if the WalletTrait has the WalletTraitPeerManager bit
// set, which indicates the wallet implements the PeerManager interface.
func (wt WalletTrait) IsPeerManager() bool {
	return wt&WalletTraitPeerManager != 0
}

// IsAuthenticator tests if WalletTrait has WalletTraitAuthenticator bit set,
// which indicates authentication is required by wallet.
func (wt WalletTrait) IsAuthenticator() bool {
	return wt&WalletTraitAuthenticator != 0
}

// IsShielded tests if the WalletTrait has the WalletTraitShielded bit
// set, which indicates the wallet implements the ShieldedWallet interface.
func (wt WalletTrait) IsShielded() bool {
	return wt&WalletTraitShielded != 0
}

// IsTokenApprover tests if the WalletTrait has the WalletTraitTokenApprover bit
// set, which indicates the wallet implements the TokenApprover interface.
func (wt WalletTrait) IsTokenApprover() bool {
	return wt&WalletTraitTokenApprover != 0
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
	if _, is := w.(TxFeeEstimator); is {
		t |= WalletTraitTxFeeEstimator
	}
	if _, is := w.(PeerManager); is {
		t |= WalletTraitPeerManager
	}
	if _, is := w.(Authenticator); is {
		t |= WalletTraitAuthenticator
	}
	if _, is := w.(ShieldedWallet); is {
		t |= WalletTraitShielded
	}
	if _, is := w.(TokenApprover); is {
		t |= WalletTraitTokenApprover
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
	// ErrSwapRefunded is returned from ConfirmRedemption when the swap has
	// been refunded before the user could redeem.
	ErrSwapRefunded = dex.ErrorKind("swap refunded")
	// ErrNotEnoughConfirms is returned when a transaction is confirmed,
	// but does not have enough confirmations to be trusted.
	ErrNotEnoughConfirms = dex.ErrorKind("transaction does not have enough confirmations")
	// ErrWalletTypeDisabled indicates that a wallet type is no longer
	// available.
	ErrWalletTypeDisabled = dex.ErrorKind("wallet type has been disabled")
	// ErrInsufficientBalance is returned when there is insufficient available
	// balance for an operation, such as reserving funds for future bonds.
	ErrInsufficientBalance = dex.ErrorKind("insufficient available balance")
	// ErrIncorrectBondKey is returned when a provided private key is incorrect
	// for a bond output.
	ErrIncorrectBondKey = dex.ErrorKind("incorrect private key")
	// ErrUnapprovedToken is returned when trying to fund an order using a token
	// that has not been approved.
	ErrUnapprovedToken = dex.ErrorKind("token not approved")

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
	// NoAuth indicates that the wallet does not implement the Authenticator
	// interface. A better way to check is to use the wallet traits but wallet
	// construction is presently required to discern traits.
	NoAuth bool `json:"noauth"`
	// GuideLink is a link to wallet configuration docs that the user may follow
	// when creating a new wallet or updating its settings.
	GuideLink string `json:"guidelink"`
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
	// Version is the Wallet's primary asset version number, which is used to
	// signal when major changes are made to internal details such as coin ID
	// encoding and contract structure that must be common to a server's.
	Version uint32 `json:"version"` // Deprecated? Does frontend need .version?
	// SupportedVersions lists all supported asset versions. Several wallet
	// methods accept a version argument to indicate which contract to use,
	// however, the consumer (e.g. Core) is responsible for ensuring the
	// server's asset version is supported before using the Wallet.
	SupportedVersions []uint32 `json:"versions"`
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
	// MaxSwapsInTx is the max amount of swaps that this wallet can do in a
	// single transaction.
	MaxSwapsInTx uint64
	// MaxRedeemsInTx is the max amount of redemptions that this wallet can do
	// in a single transaction.
	MaxRedeemsInTx uint64
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
	Options           map[string]*ConfigOption
	NoEcho            bool `json:"noecho"`
	IsBoolean         bool `json:"isboolean"`
	IsDate            bool `json:"isdate"`
	DisableWhenActive bool `json:"disablewhenactive"`
	IsBirthdayConfig  bool `json:"isBirthdayConfig"`
	// Repeatable signals a text input that can be duplicated and submitted
	// multiple times, with the specified delimiter used to encode the data
	// in the settings map.
	Repeatable string `json:"repeatable"`
	// RepeatN signals how many times text input should be repeated, replicating
	// this option N times.
	RepeatN  int32 `json:"repeatN"`
	Required bool  `json:"required"`

	// ShowByDefault to show or not options on "hide advanced options".
	ShowByDefault bool `json:"showByDefault,omitempty"`
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

// ConfirmRedemptionStatus contains the coinID which redeemed a swap, the
// number of confirmations the transaction has, and the number of confirmations
// required for it to be considered confirmed.
type ConfirmRedemptionStatus struct {
	Confs  uint64
	Req    uint64
	CoinID dex.Bytes
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
	// canceled order. A nil Coins slice indicates to unlock all coins that the
	// wallet may have locked, a syntax that should always be followed by
	// FundingCoins for any active orders, and is thus only appropriate at time
	// of login. Unlocking all coins is likely only useful for external wallets
	// whose lifetime is longer than the asset.Wallet instance.
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
	// ContractLockTimeExpired returns true if the specified contract's locktime
	// has expired, making it possible to issue a Refund. The contract expiry
	// time is also returned, but reaching this time does not necessarily mean
	// the contract can be refunded since assets have different rules to satisfy
	// the lock. For example, in Bitcoin the median of the last 11 blocks must
	// be past the expiry time, not the current time.
	ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error)
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
	// LockTimeExpired returns true if the specified locktime has expired,
	// making it possible to redeem the locked coins.
	LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error)
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
	// ValidateAddress checks that the provided address is valid.
	ValidateAddress(address string) bool
	// ConfirmRedemption checks the status of a redemption. It returns the
	// number of confirmations the redemption has, the number of confirmations
	// that are required for it to be considered fully confirmed, and the
	// CoinID used to do the redemption. If it is determined that a transaction
	// will not be mined, this function will submit a new transaction to
	// replace the old one. The caller is notified of this by having a
	// different CoinID in the returned asset.ConfirmRedemptionStatus as was
	// used to call the function.
	ConfirmRedemption(coinID dex.Bytes, redemption *Redemption, feeSuggestion uint64) (*ConfirmRedemptionStatus, error)
	// SingleLotSwapFees returns the fees for a swap transaction for a single lot.
	SingleLotSwapFees(version uint32, feeRate uint64, options map[string]string) (uint64, error)
	// SingleLotRedeemFees returns the fees for a redeem transaction for a single lot.
	SingleLotRedeemFees(version uint32, feeRate uint64, options map[string]string) (uint64, error)
}

// Authenticator is a wallet implementation that require authentication.
type Authenticator interface {
	// Unlock unlocks the exchange wallet.
	Unlock(pw []byte) error
	// Lock locks the exchange wallet.
	Lock() error
	// Locked will be true if the wallet is currently locked.
	Locked() bool
}

// TxFeeEstimator is a wallet implementation with fee estimation functionality.
type TxFeeEstimator interface {
	// EstimateSendTxFee returns a tx fee estimate for sending or withdrawing
	// the provided amount using the provided feeRate. This uses actual utxos to
	// calculate the tx fee where possible and ensures the wallet has enough to
	// cover send value and minimum fees.
	EstimateSendTxFee(address string, value, feeRate uint64, subtract bool) (fee uint64, isValidAddress bool, err error)
}

// Broadcaster is a wallet that can send a raw transaction on the asset network.
type Broadcaster interface {
	// SendTransaction broadcasts a raw transaction, returning its coin ID.
	SendTransaction(rawTx []byte) ([]byte, error)
}

// Bonder is a wallet capable of creating and redeeming time-locked fidelity
// bond transaction outputs.
type Bonder interface {
	Broadcaster

	// BondsFeeBuffer suggests how much extra may be required for the
	// transaction fees part of bond reserves when bond rotation is enabled.
	// This should return an amount larger than the minimum required by the
	// asset's reserves system for fees, if non-zero, so that a reserves
	// "deficit" does not appear right after the first bond is posted.
	BondsFeeBuffer() uint64

	// RegisterUnspent informs the wallet of a certain amount already locked in
	// unspent bonds that will eventually be refunded with RefundBond. This
	// should be used prior to ReserveBondFunds. This alone does not enable
	// reserves enforcement, and it should be called on bring-up when existing
	// bonds that may refund to this wallet are known. Once ReserveBondFunds is
	// called, these live bond amounts will become enforced reserves when they
	// are refunded via RefundBond.
	RegisterUnspent(live uint64)
	// ReserveBondFunds (un)reserves funds for creation of future bonds.
	// MakeBondTx will create transactions that decrease these reserves, while
	// RefundBond will replenish the reserves. If the wallet's available balance
	// should be respected when adding reserves, the boolean argument may be set
	// to indicate this, in which case the return value indicates if it was able
	// to reserve the funds. In this manner, funds may be pre-reserved so that
	// when the wallet receives funds (from either external deposits or
	// refunding of live bonds), they will go directly into locked balance. When
	// the reserves are decremented to zero (by the amount that they were
	// incremented), all enforcement including any fee buffering is disabled.
	ReserveBondFunds(future int64, respectBalance bool) bool

	// MakeBondTx authors a DEX time-locked fidelity bond transaction for the
	// provided amount, lock time, and dex account ID. An explicit private key
	// type is used to guarantee it's not bytes from something else like a
	// public key. If there are insufficient bond reserves, the returned error
	// should be of kind asset.ErrInsufficientBalance. The returned function may
	// be used to abandon the bond iff it has not yet been broadcast. Generally
	// this means unlocking the funds that are used by the transaction and
	// restoring consumed reserves amounts.
	MakeBondTx(ver uint16, amt, feeRate uint64, lockTime time.Time, privKey *secp256k1.PrivateKey, acctID []byte) (*Bond, func(), error)
	// RefundBond will refund the bond given the full bond output details and
	// private key to spend it. The bond is broadcasted.
	RefundBond(ctx context.Context, ver uint16, coinID, script []byte, amt uint64, privKey *secp256k1.PrivateKey) (Coin, error)

	// A RefundBondByCoinID may be created in the future to attempt to refund a
	// bond by locating it on chain, i.e. without providing the amount or
	// script, while also verifying the bond output is unspent. However, it's
	// far more straightforward to generate the refund transaction using the
	// known values. Further, methods for (1) locking coins for future bonds,
	// and (2) renewing bonds by spending a bond directly into a new one, may be
	// required for efficient client bond management.
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

// DynamicSwapper defines methods that accept an initiation
// or redemption coinID and returns the fee spent on the transaction along with
// the secrets included in the tx. Returns asset.CoinNotFoundError for unmined
// txn. Returns asset.ErrNotEnoughConfirms for txn with too few confirmations.
// Will also error if the secret in the contractData is not found in the
// transaction secrets.
type DynamicSwapper interface {
	// DynamicSwapFeesPaid returns fees for initiation transactions.
	DynamicSwapFeesPaid(ctx context.Context, coinID, contractData dex.Bytes) (fee uint64, secretHashes [][]byte, err error)
	// DynamicRedemptionFeesPaid returns fees for redemption transactions.
	DynamicRedemptionFeesPaid(ctx context.Context, coinID, contractData dex.Bytes) (fee uint64, secretHashes [][]byte, err error)
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
	// FeesForRemainingSwaps returns the fees for a certain number of
	// chained/grouped swaps at a given feeRate. This should be used with an
	// Accelerator wallet to help compute the required amount for remaining
	// swaps for a given trade with a mix of future and active matches, which is
	// only known to the consumer. This is only accurate if each swap has a
	// single input or chained swaps all pay the same fees. Accurate estimates
	// for new orders without existing funding should use PreSwap or FundOrder.
	FeesForRemainingSwaps(n, feeRate uint64) uint64
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
	AccelerateOrder(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes,
		requiredForRemainingSwaps, newFeeRate uint64) (Coin, string, error)
	// AccelerationEstimate takes the same parameters as AccelerateOrder, but
	// instead of broadcasting the acceleration transaction, it just returns
	// the amount of funds that will need to be spent in order to increase the
	// average fee rate to the desired amount.
	AccelerationEstimate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes,
		requiredForRemainingSwaps, newFeeRate uint64) (uint64, error)
	// PreAccelerate returns the current average fee rate of the unmined swap
	// initiation and acceleration transactions, and also returns a suggested
	// range that the fee rate should be increased to in order to expedite mining.
	// The feeSuggestion argument is the current prevailing network rate. It is
	// used to help determine the suggestedRange, which is a range meant to give
	// the user a good amount of flexibility in determining the post acceleration
	// effective fee rate, but still not allowing them to pick something
	// outrageously high.
	PreAccelerate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes,
		requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *XYRange, *EarlyAcceleration, error)
}

// TokenConfig is required to OpenTokenWallet.
type TokenConfig struct {
	// AssetID of the token.
	AssetID uint32
	// Settings correspond to Token.Definition.ConfigOpts.
	Settings map[string]string
	// TipChange will be called after the parent's TipChange.
	TipChange func(error)
	// PeersChange will be called after the parent's PeersChange.
	PeersChange func(uint32, error)
}

// TokenMaster is implemented by assets which support degenerate tokens.
type TokenMaster interface {
	// CreateTokenWallet creates a wallet for the specified token asset. The
	// settings correspond to the Token.Definition.ConfigOpts.
	CreateTokenWallet(assetID uint32, settings map[string]string) error
	// OpenTokenWallet opens a wallet for the specified token asset.
	OpenTokenWallet(cfg *TokenConfig) (Wallet, error)
}

// AccountLocker is a wallet in which redemptions and refunds require a wallet
// to have available balance to pay fees.
type AccountLocker interface {
	// ReserveNRedemption is used when preparing funding for an order that
	// redeems to an account-based asset. The wallet will set aside the
	// appropriate amount of funds so that we can redeem N swaps using the
	// specified fee and asset version. It is an error to request funds >
	// spendable balance.
	ReserveNRedemptions(n uint64, ver uint32, maxFeeRate uint64) (uint64, error)
	// ReReserveRedemption is used when reconstructing existing orders on
	// startup. It is an error to request funds > spendable balance.
	ReReserveRedemption(amt uint64) error
	// UnlockRedemptionReserves is used to return funds reserved for redemption
	// when an order is canceled or otherwise completed unfilled.
	UnlockRedemptionReserves(uint64)
	// ReserveNRefunds is used when preparing funding for an order that refunds
	// to an account-based asset. The wallet will set aside the appropriate
	// amount of funds so that we can refund N swaps using the specified fee and
	// asset version. It is an error to request funds > spendable balance.
	ReserveNRefunds(n uint64, ver uint32, maxFeeRate uint64) (uint64, error)
	// ReReserveRefund is used when reconstructing existing orders on
	// startup. It is an error to request funds > spendable balance.
	ReReserveRefund(uint64) error
	// UnlockRefundReserves is used to return funds reserved for refunds
	// when an order was cancelled or revoked before a swap was initiated,
	// completed successfully, or after a refund was done.
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

// PeerSource specifies how a wallet knows about a peer. It may have been
// hardcoded into the wallet code, added manually by the user, or discovered
// by communicating with the default/user added peers.
type PeerSource uint16

const (
	WalletDefault PeerSource = iota
	UserAdded
	Discovered
)

// WalletPeer provides information about a wallet's peer.
type WalletPeer struct {
	Addr      string     `json:"addr"`
	Source    PeerSource `json:"source"`
	Connected bool       `json:"connected"`
}

// PeerManager is a wallet which provides allows the user to see the peers the
// wallet is connected to and add new peers.
type PeerManager interface {
	// Peers returns a list of peers that the wallet is connected to.
	Peers() ([]*WalletPeer, error)
	// AddPeer connects the wallet to a new peer. The peer's address will be
	// persisted and connected to each time the wallet is started up.
	AddPeer(addr string) error
	// RemovePeer will remove a peer that was added by AddPeer. This peer may
	// still be connected to by the wallet if it discovers it on its own.
	RemovePeer(addr string) error
}

type ApprovalStatus uint8

const (
	Approved ApprovalStatus = iota
	Pending
	NotApproved
)

// TokenApprover is implemented by wallets that require an approval before
// trading.
type TokenApprover interface {
	// ApproveToken sends an approval transaction for a specific version of
	// the token's swap contract. An error is returned if an approval has
	// already been done or is pending. The onConfirm callback is called
	// when the approval transaction is confirmed.
	ApproveToken(assetVer uint32, onConfirm func()) (string, error)
	// UnapproveToken removes the approval for a specific version of the
	// token's swap contract.
	UnapproveToken(assetVer uint32, onConfirm func()) (string, error)
	// ApprovalStatus returns the approval status for each version of the
	// token's swap contract.
	ApprovalStatus() map[uint32]ApprovalStatus
	// ApprovalFee returns the estimated fee for an approval transaction.
	ApprovalFee(assetVer uint32, approval bool) (uint64, error)
}

// Bond is the fidelity bond info generated for a certain account ID, amount,
// and lock time. These data are intended for the "post bond" request, in which
// the server pre-validates the unsigned transaction, the client then publishes
// the corresponding signed transaction, and a final request is made once the
// bond is fully confirmed. The caller should manage the private key.
type Bond struct {
	Version uint16
	AssetID uint32
	Amount  uint64
	CoinID  []byte
	Data    []byte // additional data to interpret the bond e.g. redeem script, bond contract, etc.
	// SignedTx and UnsignedTx are the opaque (raw bytes) signed and unsigned
	// bond creation transactions, in whatever encoding and funding scheme for
	// this asset and wallet. The unsigned one is used to pre-validate this bond
	// with the server prior to publishing it, thus locking funds for a long
	// period of time. Once the bond is pre-validated, the signed tx may then be
	// published by the wallet.
	SignedTx, UnsignedTx []byte
	// RedeemTx is a backup transaction that spends the bond output. Normally
	// the a key index will be used to derive the key when the bond expires.
	RedeemTx []byte
}

// ShieldedStatus is the balance and address associated with the shielded
// account.
type ShieldedStatus struct {
	LastAddress string
	Balance     uint64
}

// ShieldedWallet is implemented by ZCash, and enables working with value in
// shielded pools.
type ShieldedWallet interface {
	// ShieldedStatus list the last address and the balance in the shielded
	// account.
	ShieldedStatus() (*ShieldedStatus, error)
	// NewShieldedAddress creates a new shielded address. A shielded address can
	// be reused without sacrifice of privacy on-chain, but that doesn't stop
	// meat-space coordination to reduce privacy.
	NewShieldedAddress() (string, error)
	// ShieldFunds moves funds from the transparent account to the shielded
	// account.
	ShieldFunds(ctx context.Context, amt uint64) ([]byte, error)
	// UnshieldFunds moves funds from the shielded account to the transparent
	// account.
	UnshieldFunds(ctx context.Context, amt uint64) ([]byte, error)
	// SendShielded sends funds from the shielded account to the provided
	// shielded or transparent address.
	SendShielded(ctx context.Context, toAddr string, amt uint64) ([]byte, error)
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
	// Other is a place to list custom balance categories. It is recommended for
	// custom balance added here to have a translation and tooltip info in
	// client/webserver/site/src/js/wallet.js#customWalletBalanceCategory
	Other map[string]*CustomBalance `json:"other"`
}

type CustomBalance struct {
	Amount uint64 `json:"amt"`
	Locked bool   `json:"locked"`
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
	// Version is the asset version. Most backends only support one version.
	Version uint32
	// Inputs are the Coins being spent.
	Inputs Coins
	// Contract is the contract data.
	Contracts []*Contract
	// FeeRate is the required fee rate in atoms/byte.
	FeeRate uint64
	// LockChange can be set to true if the change should be locked for
	// subsequent matches.
	LockChange bool
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
	// Version is the asset version of the "from" asset with the init
	// transaction (this wallet). Most backends only support one version.
	Version uint32
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
	// MaxFeeRate is the largest possible fee rate for the init transaction (of
	// this "from" asset) specific to and provided by a particular server, and
	// is used to calculate the funding required to cover fees.
	MaxFeeRate uint64
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

	// The following fields are only used for some assets where the redeemed/to
	// asset may require funds in this "from" asset. For example, buying ERC20
	// tokens with ETH.

	// RedeemVersion is the asset version of the "to" asset with the redeem
	// transaction. Most backends only support one version.
	RedeemVersion uint32
	// RedeemAssetID is the asset ID of the "to" asset.
	RedeemAssetID uint32
}
