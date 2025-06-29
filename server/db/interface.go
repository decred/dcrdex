// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// EpochResults represents the outcome of epoch order processing, including
// preimage collection, and computation of commitment checksum and shuffle seed.
// MatchTime is the time at which order matching is executed.
type EpochResults struct {
	MktBase, MktQuote uint32
	Idx               int64
	Dur               int64
	MatchTime         int64
	CSum              []byte
	Seed              []byte
	OrdersRevealed    []order.OrderID
	OrdersMissed      []order.OrderID
	MatchVolume       uint64
	QuoteVolume       uint64
	BookBuys          uint64
	BookBuys5         uint64
	BookBuys25        uint64
	BookSells         uint64
	BookSells5        uint64
	BookSells25       uint64
	HighRate          uint64
	LowRate           uint64
	StartRate         uint64
	EndRate           uint64
}

// OrderStatus is the current status of an order.
type OrderStatus struct {
	ID     order.OrderID
	Status order.OrderStatus
}

// PreimageResult is the outcome of preimage collection for an order in an epoch
// that closed at a certain time.
type PreimageResult struct {
	Miss bool
	Time int64
	ID   order.OrderID
}

// KeyIndexer are the functions required to track an extended public key and
// derived children by index.
type KeyIndexer interface {
	KeyIndex(xpub string) (uint32, error)
	SetKeyIndex(idx uint32, xpub string) error
}

// DEXArchivist will be composed of several different interfaces. Starting with
// OrderArchiver.
type DEXArchivist interface {
	// LastErr should returns any fatal or unexpected error encountered by the
	// archivist backend. This may be used to check if the database had an
	// unrecoverable error (disconnect, etc.).
	LastErr() error

	// Fatal provides select semantics like Context.Done when there is a fatal
	// backend error. Use LastErr to get the error.
	Fatal() <-chan struct{}

	// Close should gracefully shutdown the backend, returning when complete.
	Close() error

	// InsertEpoch stores the results of a newly-processed epoch.
	InsertEpoch(ed *EpochResults) error

	// LastEpochRate gets the EndRate of the last EpochResults inserted for the
	// market. If the database is empty, no error and a rate of zero are
	// returned.
	LastEpochRate(b, q uint32) (uint64, error)

	// LoadEpochStats reads all market epoch history from the database.
	LoadEpochStats(uint32, uint32, []*candles.Cache) error
	LastCandleEndStamp(base, quote uint32, candleDur uint64) (uint64, error)
	InsertCandles(base, quote uint32, dur uint64, cs []*candles.Candle) error

	OrderArchiver
	AccountArchiver
	KeyIndexer
	MatchArchiver
	SwapArchiver
	ReputationArchiver
}

// OrderArchiver is the interface required for storage and retrieval of all
// order data.
type OrderArchiver interface {
	// Order retrieves an order with the given OrderID, stored for the market
	// specified by the given base and quote assets.
	Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error)

	// BookOrders returns all book orders for a market.
	BookOrders(base, quote uint32) ([]*order.LimitOrder, error)

	// EpochOrders returns all epoch orders for a market.
	EpochOrders(base, quote uint32) ([]order.Order, error)

	// FlushBook revokes all booked orders for a market.
	FlushBook(base, quote uint32) (sellsRemoved, buysRemoved []order.OrderID, err error)

	// ActiveOrderCoins retrieves a CoinID slice for each active order.
	ActiveOrderCoins(base, quote uint32) (baseCoins, quoteCoins map[order.OrderID][]order.CoinID, err error)

	// UserOrders retrieves all orders for the given account in the market
	// specified by a base and quote asset.
	UserOrders(ctx context.Context, aid account.AccountID, base, quote uint32) ([]order.Order, []order.OrderStatus, error)

	// UserOrderStatuses retrieves the statuses and filled amounts of the orders
	// with the provided order IDs for the given account in the market specified
	// by a base and quote asset.
	// The number and ordering of the returned statuses is not necessarily the
	// same as the number and ordering of the provided order IDs. It is not an
	// error if any or all of the provided order IDs cannot be found for the
	// given account in the specified market.
	UserOrderStatuses(aid account.AccountID, base, quote uint32, oids []order.OrderID) ([]*OrderStatus, error)

	// ActiveUserOrderStatuses retrieves the statuses and filled amounts of all
	// active orders for a user across all markets.
	ActiveUserOrderStatuses(aid account.AccountID) ([]*OrderStatus, error)

	// CompletedUserOrders retrieves the N most recently completed orders for a
	// user across all markets.
	CompletedUserOrders(aid account.AccountID, N int) (oids []order.OrderID, compTimes []int64, err error)

	// PreimageStats retrieves the N most recent results of preimage requests
	// for the user across all markets.
	PreimageStats(user account.AccountID, lastN int) ([]*PreimageResult, error)

	// ExecutedCancelsForUser retrieves up to N executed cancel orders for a
	// given user. These may be user-initiated cancels, or cancels created by
	// the server (revokes). Executed cancel orders from all markets are
	// returned.
	ExecutedCancelsForUser(aid account.AccountID, N int) ([]*CancelRecord, error)

	// OrderWithCommit searches all markets' trade and cancel orders, both
	// active and archived, for an order with the given Commitment.
	OrderWithCommit(ctx context.Context, commit order.Commitment) (found bool, oid order.OrderID, err error)

	// OrderStatus gets the status, ID, and filled amount of the given order.
	OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error)

	// NewEpochOrder stores a new order with epoch status. Such orders are
	// pending execution or insertion on a book (standing limit orders with a
	// remaining unfilled amount). For trade orders, the epoch gap should be
	// db.EpochGapNA, while for cancel orders it is the number of epochs since
	// the targeted order was placed, as described in the docs for CancelRecord.
	NewEpochOrder(ord order.Order, epochIdx, epochDur int64, epochGap int32) error

	// StorePreimage stores the preimage associated with an existing order.
	StorePreimage(ord order.Order, pi order.Preimage) error

	// BookOrder books the given order. If the order was already stored (i.e.
	// NewEpochOrder), it's status and filled amount are updated, otherwise it
	// is inserted. See also UpdateOrderFilled.
	BookOrder(*order.LimitOrder) error

	// ExecuteOrder puts the order into the executed state, and sets the filled
	// amount for market and limit orders. For unmatched cancel orders, use
	// FailCancelOrder instead.
	ExecuteOrder(ord order.Order) error

	// CancelOrder puts a limit order into the canceled state. Market orders
	// must use ExecuteOrder since they may not be canceled. Similarly, cancel
	// orders must use ExecuteOrder or FailCancelOrder. Orders that are
	// terminated by the DEX rather than via a cancel order are considered
	// "revoked", and RevokeOrder should be used to set this status.
	CancelOrder(*order.LimitOrder) error

	// RevokeOrder puts an order into the revoked state, and generates a cancel
	// order to record the action. Orders should be revoked by the DEX according
	// to policy on failed orders. For canceling an order that was matched with
	// a cancel order, use CancelOrder.
	RevokeOrder(order.Order) (cancelID order.OrderID, t time.Time, err error)

	// RevokeOrderUncounted is like RevokeOrder except that the generated cancel
	// order will not be counted against the user. i.e. ExecutedCancelsForUser
	// should not return the cancel orders created this way.
	RevokeOrderUncounted(order.Order) (cancelID order.OrderID, t time.Time, err error)

	// NewArchivedCancel stores a cancel order directly in the executed state. This
	// is used for orders that are canceled when the market is suspended, and therefore
	// do not need to be matched.
	NewArchivedCancel(ord *order.CancelOrder, epochID, epochDur int64) error

	// FailCancelOrder puts an unmatched cancel order into the executed state.
	// For matched cancel orders, use ExecuteOrder.
	FailCancelOrder(*order.CancelOrder) error

	// UpdateOrderFilled updates the filled amount of the given order. This
	// function applies only to limit orders, not cancel or market orders. The
	// filled amount of a market order should be updated by ExecuteOrder.
	UpdateOrderFilled(*order.LimitOrder) error

	// UpdateOrderStatus updates the status and filled amount of the given
	// order.
	UpdateOrderStatus(order.Order, order.OrderStatus) error

	// SetOrderCompleteTime sets the successful completion time for an existing
	// order. This will follow the final step in swap negotiation, for an order
	// that is not on the book.
	SetOrderCompleteTime(ord order.Order, compTimeMs int64) error
}

// Account holds data returned by Accounts.
type Account struct {
	AccountID account.AccountID `json:"accountid"`
	Pubkey    dex.Bytes         `json:"pubkey"`
}

// Bond represents a time-locked fidelity bond posted by a user.
type Bond struct {
	Version  uint16
	AssetID  uint32
	CoinID   []byte
	Amount   int64
	Strength uint32 // Amount / <bond increment at time of acceptance>
	LockTime int64

	// Will we need to store asset-specific data, like the redeem script for
	// UTXO assets or a contract address or bond key for account assets? Or will
	// that info be conveyed by Version and CoinID?
	//
	// Data []byte
}

// AccountArchiver is the interface required for storage and retrieval of all
// account data.
type AccountArchiver interface {
	// Account retrieves the account information for the specified account ID. A
	// nil pointer will be returned for unknown or closed accounts. Bond and
	// registration fee payment status is returned as well. A bond is active if
	// its lockTime is after the lockTimeThresh Time, which should be
	// time.Now().Add(bondExpiry). The legacy bool return refers to the legacy
	// registration fee system, and legacyPaid indicates if the account has a
	// recorded fee coin (paid legacy fee).
	Account(acctID account.AccountID, lockTimeThresh time.Time) (acct *account.Account, activeBonds []*Bond)

	// CreateAccountWithBond creates a new account with the given bond. This is
	// used for the new postbond request protocol. The bond tx should be
	// fully-confirmed.
	CreateAccountWithBond(acct *account.Account, bond *Bond) error

	// AddBond stores a new Bond, which is uniquely identified by (asset ID,
	// coin ID), for an existing account.
	AddBond(acct account.AccountID, bond *Bond) error

	// DeleteBond deletes a bond which should generally be expired.
	DeleteBond(assetID uint32, coinID []byte) error

	FetchPrepaidBond(bondCoinID []byte) (strength uint32, lockTime int64, err error)
	DeletePrepaidBond(coinID []byte) error
	StorePrepaidBonds(coinIDs [][]byte, strength uint32, lockTime int64) error

	// AccountInfo returns data for an account.
	AccountInfo(account.AccountID) (*Account, error)
}

// MatchData represents an order pair match, but with just the order IDs instead
// of the full orders. The actual orders may be retrieved by ID.
type MatchData struct {
	ID        order.MatchID
	Taker     order.OrderID
	TakerAcct account.AccountID
	TakerAddr string
	TakerSell bool
	Maker     order.OrderID
	MakerAcct account.AccountID
	MakerAddr string
	Epoch     order.EpochID
	Quantity  uint64
	Rate      uint64
	BaseRate  uint64
	QuoteRate uint64
	Active    bool              // match negotiation in progress, not yet completed or failed
	Status    order.MatchStatus // note that failed swaps, where Active=false, can have any status
}

// MatchDataWithCoins pairs MatchData (embedded) with the encode swap and redeem
// coin IDs blobs for both maker and taker.
type MatchDataWithCoins struct {
	MatchData
	MakerSwapCoin   []byte
	MakerRedeemCoin []byte
	TakerSwapCoin   []byte
	TakerRedeemCoin []byte
}

// MatchStatus is the current status of a match, its known contracts and coin
// IDs, and its secret, if known.
type MatchStatus struct {
	ID            order.MatchID
	Status        order.MatchStatus
	MakerContract []byte
	TakerContract []byte
	MakerSwap     []byte
	TakerSwap     []byte
	MakerRedeem   []byte
	TakerRedeem   []byte
	Secret        []byte
	Active        bool
	TakerSell     bool
	IsTaker       bool
	IsMaker       bool
}

// SwapData contains the data generated by the clients during swap negotiation.
type SwapData struct {
	SigMatchAckMaker []byte
	SigMatchAckTaker []byte
	ContractA        []byte // contains the secret hash used by both parties
	ContractACoinID  []byte
	ContractATime    int64
	ContractAAckSig  []byte // B's signature of contract A data
	ContractB        []byte
	ContractBCoinID  []byte
	ContractBTime    int64
	ContractBAckSig  []byte // A's signature of contract B data
	RedeemACoinID    []byte
	RedeemASecret    []byte // the secret revealed in A's redeem, also used in B's redeem
	RedeemATime      int64
	RedeemAAckSig    []byte // B's signature of redeem A data
	RedeemBCoinID    []byte
	RedeemBTime      int64
}

// SwapDataFull combines a MatchData, SwapData, and the Base/Quote asset IDs.
type SwapDataFull struct {
	Base, Quote uint32
	*MatchData
	*SwapData
}

// MarketMatchID designates a MatchID for a certain market by the market's
// base-quote asset IDs.
type MarketMatchID struct {
	order.MatchID
	Base, Quote uint32 // market
}

// MatchID constructs a MarketMatchID from an order.Match.
func MatchID(match *order.Match) MarketMatchID {
	return MarketMatchID{
		MatchID: match.ID(),
		Base:    match.Maker.BaseAsset, // same for taker's redeem as BaseAsset refers to the market
		Quote:   match.Maker.QuoteAsset,
	}
}

// MatchOutcome pairs an inactive match's status with a timestamp. In the case
// of a successful match for the user, this is when their redeem was received.
// In the case of an at-fault match failure for the user, this corresponds to
// the time of the previous match action. The previous action times are: match
// time, swap txn validated times, and initiator redeem validated time. Note
// that this does not directly correspond to match revocation times where
// inaction deadline references the time when the swap txns reach the required
// confirms. These times must match the reference times provided to the auth
// manager when registering new swap outcomes.
type MatchOutcome struct {
	Status      order.MatchStatus
	ID          order.MatchID
	Fail        bool // taker must reach MatchComplete, maker succeeds at MakerRedeemed
	Time        int64
	Value       uint64
	Base, Quote uint32 // the market
}

// MatchFail is a failed match and the effect on the user's score
type MatchFail struct {
	ID     order.MatchID
	Status order.MatchStatus
}

// MatchArchiver is the interface required for storage and retrieval of all
// match data.
type MatchArchiver interface {
	InsertMatch(match *order.Match) error
	MatchByID(mid order.MatchID, base, quote uint32) (*MatchData, error)
	UserMatches(aid account.AccountID, base, quote uint32) ([]*MatchData, error)
	CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*MatchOutcome, error)
	UserMatchFails(aid account.AccountID, lastN int) ([]*MatchFail, error)
	ForgiveMatchFail(mid order.MatchID) (bool, error)
	AllActiveUserMatches(aid account.AccountID) ([]*MatchData, error)
	MarketMatches(base, quote uint32) ([]*MatchDataWithCoins, error)
	MarketMatchesStreaming(base, quote uint32, includeInactive bool, N int64, f func(*MatchDataWithCoins) error) (int, error)
	MatchStatuses(aid account.AccountID, base, quote uint32, matchIDs []order.MatchID) ([]*MatchStatus, error)
}

// SwapArchiver is the interface required for storage and retrieval of swap
// counterparty data.
//
// In the swap process, the counterparties are:
// - Initiator or party A on chain X. This is the maker in the DEX.
// - Participant or party B on chain Y. This is the taker in the DEX.
//
// For each match, a successful swap will generate the following data that must
// be stored:
//   - 5 client signatures. Both parties sign the data to acknowledge (1) the
//     match ack, and (2) the counterparty's contract script and contract
//     transaction. Plus the taker acks the maker's redemption transaction.
//   - 2 swap contracts and the associated transaction outputs (more generally,
//     coinIDs), one on each party's blockchain.
//   - 2 redemption transaction outputs (coinIDs).
//
// The methods for saving this data are defined below in the order in which the
// data is expected from the parties.
type SwapArchiver interface {
	// ActiveSwaps loads the full details for all active swaps across all markets.
	ActiveSwaps() ([]*SwapDataFull, error)

	// SwapData retrieves the swap/match status and the current SwapData.
	SwapData(mid MarketMatchID) (order.MatchStatus, *SwapData, error)

	// Match acknowledgement message signatures.

	// SaveMatchAckSigA records the match data acknowledgement signature from
	// swap party A (the initiator), which is the maker in the DEX.
	SaveMatchAckSigA(mid MarketMatchID, sig []byte) error

	// SaveMatchAckSigB records the match data acknowledgement signature from
	// swap party B (the participant), which is the taker in the DEX.
	SaveMatchAckSigB(mid MarketMatchID, sig []byte) error

	// Swap contracts, and counterparty audit acknowledgement signatures.

	// SaveContractA records party A's swap contract script and the coinID (e.g.
	// transaction output) containing the contract on chain X. Note that this
	// contract contains the secret hash.
	SaveContractA(mid MarketMatchID, contract []byte, coinID []byte, timestamp int64) error

	// SaveAuditAckSigB records party B's signature acknowledging their audit of
	// A's swap contract.
	SaveAuditAckSigB(mid MarketMatchID, sig []byte) error

	// SaveContractB records party B's swap contract script and the coinID (e.g.
	// transaction output) containing the contract on chain Y.
	SaveContractB(mid MarketMatchID, contract []byte, coinID []byte, timestamp int64) error

	// SaveAuditAckSigA records party A's signature acknowledging their audit of
	// B's swap contract.
	SaveAuditAckSigA(mid MarketMatchID, sig []byte) error

	// Redemption transactions, and counterparty acknowledgement signatures.

	// SaveRedeemA records party A's redemption coinID (e.g. transaction
	// output), which spends party B's swap contract on chain Y. Note that this
	// transaction will contain the secret, which party B extracts.
	SaveRedeemA(mid MarketMatchID, coinID, secret []byte, timestamp int64) error

	// SaveRedeemAckSigB records party B's signature acknowledging party A's
	// redemption, which spent their swap contract on chain Y and revealed the
	// secret.
	SaveRedeemAckSigB(mid MarketMatchID, sig []byte) error

	// SaveRedeemB records party B's redemption coinID (e.g. transaction
	// output), which spends party A's swap contract on chain X. This should
	// also flag the match as inactive.
	SaveRedeemB(mid MarketMatchID, coinID []byte, timestamp int64) error

	// SetMatchInactive sets the swap as done/inactive. This can be because of a
	// failed or successfully completed swap, but in practice this will be used
	// for failed swaps since SaveRedeemB flags the swap as done/inactive. If
	// the match is being marked as inactive prior to MatchComplete (the match
	// was revoked) but the user is not at fault, the forgive bool may be set to
	// true so the outcome will not count against the user who would have the
	// next action in the swap.
	SetMatchInactive(mid MarketMatchID, forgive bool) error
}

// ValidateOrder ensures that the order with the given status for the specified
// market is sensible. This function is in the database package because the
// concept of a valid order-status-market state is dependent on the semantics of
// order archival. The ServerTime may not be set yet, so the OrderID cannot be
// computed.
func ValidateOrder(ord order.Order, status order.OrderStatus, mkt *dex.MarketInfo) bool {
	// Orders with status OrderStatusUnknown should never reach the database.
	if status == order.OrderStatusUnknown {
		return false
	}

	// Bad MarketInfo!
	if mkt.Base == mkt.Quote {
		panic("MarketInfo specifies market with same base and quote assets")
	}

	return order.ValidateOrder(ord, status, mkt.LotSize) == nil
}

// EpochGapNA is a specifier for an epoch gap (epochs between limit order and
// cancel order) when such a designation doesn't apply in-context. For instance,
// revocations are treated in many places like cancel orders, but there is no
// reason to consider the epoch gap.
const EpochGapNA int32 = -1

// CancelRecord is info about a cancel order and when it matched.
type CancelRecord struct {
	ID        order.OrderID
	TargetID  order.OrderID
	MatchTime int64
	// EpochGap is the number of epochs passed since the targeted trade order
	// was placed, where 0 means canceled in the same epoch, 1 means canceled in
	// the next epoch, etc.
	EpochGap int32
}

// Reputation

// ReputationArchiver handles interactions with the points table as well as
// upgrading the reputation version in the accounts table.
type ReputationArchiver interface {
	GetUserReputationData(ctx context.Context, user account.AccountID, pimgSz, matchSz, orderSz int) ([]*PreimageOutcome, []*MatchResult, []*OrderOutcome, error)
	AddPreimageOutcome(ctx context.Context, user account.AccountID, oid order.OrderID, miss bool) (*PreimageOutcome, error)
	AddMatchOutcome(ctx context.Context, user account.AccountID, mid order.MatchID, outcome Outcome) (*MatchResult, error)
	AddOrderOutcome(ctx context.Context, user account.AccountID, oid order.OrderID, canceled bool) (*OrderOutcome, error)
	PruneOutcomes(ctx context.Context, user account.AccountID, outcomeClass OutcomeClass, fromDBID int64) error
	GetUserReputationVersion(ctx context.Context, user account.AccountID) (int16, error)
	UpgradeUserReputationV1(
		ctx context.Context, user account.AccountID, pimgOutcomes []*PreimageOutcome, matchOutcomes []*MatchResult, orderOutcomes []*OrderOutcome, /* Without DB IDs */
	) ([]*PreimageOutcome, []*MatchResult, []*OrderOutcome, error) /* With DB IDs */
	ForgiveUser(ctx context.Context, user account.AccountID) error
}

// OutcomeClass is the type of interaction for which the user's reputation
// score is affected.
type OutcomeClass int16

const (
	OutcomeClassInvalid OutcomeClass = iota
	OutcomeClassPreimage
	OutcomeClassOrder
	OutcomeClassMatch
)

type Outcome int16

const (
	OutcomeInvalid Outcome = iota
	OutcomeForgiven
	// Match Outcomes
	OutcomeSwapSuccess
	OutcomeNoSwapAsMaker
	OutcomeNoSwapAsTaker
	OutcomeNoRedeemAsMaker
	OutcomeNoRedeemAsTaker
	// Preimage
	OutcomePreimageSuccess
	OutcomePreimageMiss
	// Order cancel/complete
	OutcomeOrderComplete
	OutcomeOrderCanceled
)

func (o Outcome) String() string {
	switch o {
	case OutcomeForgiven:
		return "forgiveness"
	case OutcomePreimageMiss:
		return "preimage miss"
	case OutcomePreimageSuccess:
		return "preimage success"
	case OutcomeSwapSuccess:
		return "swap success"
	case OutcomeNoSwapAsMaker:
		return "no swap as maker"
	case OutcomeNoSwapAsTaker:
		return "no swap as taker"
	case OutcomeNoRedeemAsMaker:
		return "no redeem as maker"
	case OutcomeNoRedeemAsTaker:
		return "no redeem as taker"
	case OutcomeOrderCanceled:
		return "excessive cancels"
	case OutcomeOrderComplete:
		return "order complete"
	case OutcomeInvalid:
		return "invalid violation"
	default:
		return "unknown violation"
	}
}

type Outcomer interface {
	Outcome() Outcome
	ID() int64
}

// PreimageOutcome is the outcome of preimage collection for an order.
type PreimageOutcome struct {
	DBID    int64
	OrderID order.OrderID
	Miss    bool
}

func (p *PreimageOutcome) Outcome() Outcome {
	if p.Miss {
		return OutcomePreimageMiss
	}
	return OutcomePreimageSuccess
}

func (p *PreimageOutcome) ID() int64 {
	return p.DBID
}

// MatchResult is the outcome of a swap.
type MatchResult struct {
	DBID         int64
	MatchID      order.MatchID
	MatchOutcome Outcome
}

func (m *MatchResult) Outcome() Outcome {
	return m.MatchOutcome
}

func (m *MatchResult) ID() int64 {
	return m.DBID
}

// OrderOutcome is the outcome of an order, either canceled before the epoch gap
// or not.
type OrderOutcome struct {
	DBID     int64
	OrderID  order.OrderID
	Canceled bool
}

func (o *OrderOutcome) Outcome() Outcome {
	if o.Canceled {
		return OutcomeOrderCanceled
	}
	return OutcomeOrderComplete
}

func (o *OrderOutcome) ID() int64 {
	return o.DBID
}
