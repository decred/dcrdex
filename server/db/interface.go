// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/meshevents"
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

// DEXArchivist is composed of lifecycle methods plus storage capability
// interfaces.
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
	MatchArchiver
	SwapArchiver
	ReputationArchiver
	EventLogReader
	SnapshotStore
	EventSourcedStateChecker
}

// OrderArchiver is the interface required for storage and retrieval of all
// order data.
type OrderArchiver interface {
	// Order retrieves an order with the given OrderID, stored for the market
	// specified by the given base and quote assets.
	Order(oid order.OrderID, base, quote uint32) (order.Order, order.OrderStatus, error)

	// OrdersWithCommit searches a market's trade and cancel orders for the
	// given commitment. Active orders are searched unconditionally; archived
	// orders only when accepted at or after archivedCutoff. Archived orders may
	// share a commitment, so several rows can match. An empty
	// result with a nil error means no matching row.
	OrdersWithCommit(ctx context.Context, base, quote uint32, commit order.Commitment, archivedCutoff time.Time) ([]OrderWithStatus, error)

	// BookOrders returns all book orders for a market.
	BookOrders(base, quote uint32) ([]*order.LimitOrder, error)

	// EpochOrders returns all epoch orders for a market.
	EpochOrders(base, quote uint32) ([]order.Order, error)

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

	// OrderStatus gets the status, ID, and filled amount of the given order.
	OrderStatus(order.Order) (order.OrderStatus, order.OrderType, int64, error)

	// ApplyOrderAcceptedEvent stores an accepted order with epoch status and
	// appends its event log entry in the same transaction.
	ApplyOrderAcceptedEvent(ctx context.Context, meta *EventLogMeta, update *OrderAcceptedUpdate) (*EventLogEntry, error)

	// ApplyMarketStartedEvent updates the market lifecycle, revokes the listed
	// booked orders and abandoned epoch trades, and marks abandoned cancels as
	// failed. It returns the updated lifecycle and the committed event log entry.
	ApplyMarketStartedEvent(ctx context.Context, meta *EventLogMeta, update *MarketStartedUpdate) (*MarketStartedApplyResult, error)

	// MarketLifecycle retrieves the durable lifecycle projection for a market.
	MarketLifecycle(market string) (*MarketLifecycle, error)

	// ApplyMarketSuspendScheduledEvent records a suspension schedule.
	ApplyMarketSuspendScheduledEvent(ctx context.Context, meta *EventLogMeta, update *MarketSuspendScheduledUpdate) (*MarketSuspendScheduledApplyResult, error)
	// ApplyMarketSuspendedEvent completes suspension, revoking booked orders when required.
	ApplyMarketSuspendedEvent(ctx context.Context, meta *EventLogMeta, update *MarketSuspendedUpdate) (*MarketSuspendedApplyResult, error)
	// ApplyMarketResumeScheduledEvent records a resumption schedule.
	ApplyMarketResumeScheduledEvent(ctx context.Context, meta *EventLogMeta, update *MarketResumeScheduledUpdate) (*MarketResumeScheduledApplyResult, error)
	// ApplyMarketResumedEvent revokes invalid booked orders and resumes trading.
	ApplyMarketResumedEvent(ctx context.Context, meta *EventLogMeta, update *MarketResumedUpdate) (*MarketResumedApplyResult, error)

	// ApplyAdvanceEpochEvent advances the market's active epoch, or enters
	// the draining state when its final epoch closes.
	ApplyAdvanceEpochEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.AdvanceEpochEvent) (*EventLogEntry, error)

	// ApplyEpochProcessedEvent records an epoch's preimage results, order
	// changes, matches, and reputation outcomes, and advances the last processed
	// epoch.
	ApplyEpochProcessedEvent(ctx context.Context, meta *EventLogMeta, policy *ReputationOutcomePolicy, update *EpochProcessedUpdate) (*EventLogEntry, error)

	// ApplySuspendedCancelEvent records the cancellation of a booked order
	// while its market is suspended.
	ApplySuspendedCancelEvent(ctx context.Context, meta *EventLogMeta, update *SuspendedCancelUpdate) (*SuspendedCancelApplyResult, error)

	// ApplyOrdersRevokedEvent revokes booked standing limit orders, records
	// their generated cancel orders, and records non-penalizing reputation
	// outcomes. It returns an error for targets that are not booked.
	ApplyOrdersRevokedEvent(ctx context.Context, meta *EventLogMeta, policy *ReputationOutcomePolicy, update *OrdersRevokedUpdate) (*EventLogEntry, error)
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
	// Account returns the account and bonds whose lock time is at least
	// lockTimeThresh. It returns a nil account and nil error if the account
	// does not exist.
	Account(ctx context.Context, acctID account.AccountID, lockTimeThresh time.Time) (acct *account.Account, activeBonds []*Bond, err error)

	FetchPrepaidBond(bondCoinID []byte) (strength uint32, lockTime int64, err error)

	// AccountInfo returns data for an account.
	AccountInfo(account.AccountID) (*Account, error)

	// ApplyBondPostedEvent stores the account and bond changes with the event
	// log entry in one transaction. It creates the account if needed and
	// consumes the token for a prepaid bond. An existing bond for the same
	// account is left unchanged, but the event is still recorded. A bond owned
	// by another account is an error.
	ApplyBondPostedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.BondPostedEvent, pimgSz, matchSz, orderSz int) (*BondPostedResult, error)

	// ApplyPrepaidBondsCreatedEvent stores the prepaid bond tokens and event
	// log entry in one transaction.
	ApplyPrepaidBondsCreatedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.PrepaidBondsCreatedEvent) (*EventLogEntry, error)
}

// MatchData represents an order pair match, but with just the order IDs instead
// of the full orders. The actual orders may be retrieved by ID.
type MatchData struct {
	ID        order.MatchID
	Taker     order.OrderID
	TakerAcct account.AccountID
	// Deprecated: TakerAddr is the order-level address. Use TakerSwapAddr
	// for contract recipients.
	TakerAddr string
	TakerSell bool
	Maker     order.OrderID
	MakerAcct account.AccountID
	// Deprecated: MakerAddr is the order-level address. Use MakerSwapAddr
	// for contract recipients.
	MakerAddr string
	Epoch     order.EpochID
	Quantity  uint64
	Rate      uint64
	BaseRate  uint64
	QuoteRate uint64
	Active    bool              // match negotiation in progress, not yet completed or failed
	Status    order.MatchStatus // note that failed swaps, where Active=false, can have any status
	// MakerSwapAddr and TakerSwapAddr are per-match swap addresses from
	// each party's match acknowledgement. These are the addresses used for
	// swap contracts.
	MakerSwapAddr string
	TakerSwapAddr string
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
	// MakerSwapAddr and TakerSwapAddr are per-match swap addresses provided
	// by each party in their match acknowledgement. These are used instead of
	// the order-level addresses to ensure each match has a unique contract.
	MakerSwapAddr   string
	TakerSwapAddr   string
	ContractA       []byte // contains the secret hash used by both parties
	ContractACoinID []byte
	ContractATime   int64
	ContractAAckSig []byte // B's signature of contract A data
	ContractB       []byte
	ContractBCoinID []byte
	ContractBTime   int64
	ContractBAckSig []byte // A's signature of contract B data
	RedeemACoinID   []byte
	RedeemASecret   []byte // the secret revealed in A's redeem, also used in B's redeem
	RedeemATime     int64
	RedeemAAckSig   []byte // B's signature of redeem A data
	RedeemBCoinID   []byte
	RedeemBTime     int64
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

// ReputationForgivenResult contains the result of applying a reputation_forgiven event.
type ReputationForgivenResult struct {
	// Forgiven reports whether user forgiveness deleted any outcomes or match
	// forgiveness updated an inactive match. An already-forgiven inactive
	// match also reports true.
	Forgiven bool
	Log      *EventLogEntry
}

const EventLogTipHashSize = sha256.Size

// EventLogMeta specifies how to record a database update in the event log.
type EventLogMeta struct {
	// Seq is the event's sequence number. If zero, storage assigns the next
	// sequence. Otherwise, it must match the next sequence.
	Seq uint64

	// Event is the encoded event payload.
	Event []byte

	// ExpectedTipHash is the expected log tip hash after this event.
	// A nil value skips hash verification.
	ExpectedTipHash []byte
}

// EventLogEntry is a row in the event log.
type EventLogEntry struct {
	// Seq is the entry's sequence number. Stored entries start at 1 and
	// increase monotonically.
	Seq uint64
	// Kind identifies the type of the event.
	Kind string
	// Event is the encoded canonical mesh event payload.
	Event []byte
	// TxData is the encoded transaction data.
	TxData []byte
	// TipHash is the hash of the log through this entry.
	TipHash []byte
}

// SnapshotAnchorKind is the kind of the first entry in the event log that
// was initialized using a snapshot.
const SnapshotAnchorKind = "snapshot_anchor"

// MeshGenesisKind is the kind of the first entry in the event log of a database
// that was upgraded from a pre-mesh database. The seq of mesh genesis entries
// is always 1.
const MeshGenesisKind = "mesh_genesis"

// IsEventLogAnchorKind reports whether kind is a non-replayable event-log
// anchor.
func IsEventLogAnchorKind(kind string) bool {
	return kind == MeshGenesisKind || kind == SnapshotAnchorKind
}

// EventLogPosition identifies a position in the event log by sequence number
// and tip hash. A zero seq represents an empty log.
type EventLogPosition struct {
	Seq     uint64
	TipHash []byte
}

func (p *EventLogPosition) String() string {
	if p == nil {
		return "Position{nil}"
	}
	if p.Seq == 0 {
		return "Position{seq=0}"
	}
	return fmt.Sprintf("Position{seq=%d hash=%x}", p.Seq, p.TipHash)
}

// EventCommitUnknownError means the database could not confirm whether an
// event transaction committed.
type EventCommitUnknownError struct {
	Err error
}

func (e *EventCommitUnknownError) Error() string {
	if e == nil || e.Err == nil {
		return "event commit outcome unknown"
	}
	return fmt.Sprintf("event commit outcome unknown: %v", e.Err)
}

func (e *EventCommitUnknownError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// EventLogDivergenceError means the tip hash at Seq differs from the expected
// hash.
type EventLogDivergenceError struct {
	Seq             uint64
	ExpectedTipHash []byte
	ActualTipHash   []byte
	Err             error
}

func (e *EventLogDivergenceError) Error() string {
	if e == nil {
		return "event log divergence"
	}
	if e.Err != nil {
		return fmt.Sprintf("event log divergence at seq %d: %v", e.Seq, e.Err)
	}
	return fmt.Sprintf("event log divergence at seq %d", e.Seq)
}

func (e *EventLogDivergenceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SnapshotStore imports and exports a snapshot of the database state, required
// to sync a fresh database with a mesh peer.
type SnapshotStore interface {
	WriteSnapshot(ctx context.Context, w io.Writer) (*EventLogPosition, error)
	LoadSnapshot(ctx context.Context, r io.Reader) (*EventLogPosition, error)
}

// EventSourcedStateChecker checks and clears the event log and event projections
// while preserving configuration.
type EventSourcedStateChecker interface {
	// HasNoEventSourcedState reports whether the event log and all event
	// projections are empty. Configuration tables are ignored.
	HasNoEventSourcedState(ctx context.Context) (bool, error)

	// WipeEventSourcedState clears the event log and all event projections,
	// including historical market data. Configuration is preserved.
	WipeEventSourcedState(ctx context.Context) error
}

// EventLogReader allows callers to read the event log.
type EventLogReader interface {
	// EventLogFrontier returns the latest entry's position, or a zero
	// position if the log is empty.
	EventLogFrontier(context.Context) (*EventLogPosition, error)

	// EventLogEntriesAfter returns up to limit entries with sequence numbers
	// greater than after, in increasing order.
	EventLogEntriesAfter(ctx context.Context, after uint64, limit int) ([]*EventLogEntry, error)
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
	CompletedAndAtFaultMatchStats(aid account.AccountID, lastN int) ([]*MatchOutcome, error)
	UserMatchFails(aid account.AccountID, lastN int) ([]*MatchFail, error)
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
// The event appliers that save this data are defined below in the order in
// which the data is expected from the parties.
type SwapArchiver interface {
	// ActiveSwaps loads the full details for all active swaps across all markets.
	ActiveSwaps() ([]*SwapDataFull, error)

	// SwapDataFullByID loads a match's row and swap data by match ID,
	// searching configured markets for active or inactive matches. It returns
	// ErrUnknownMatch when no match is found.
	SwapDataFullByID(mid order.MatchID) (*SwapDataFull, error)

	// ApplyMatchAcksRecordedEvent records match acknowledgement signatures
	// and swap addresses.
	ApplyMatchAcksRecordedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.MatchAcksRecordedEvent) (*EventLogEntry, error)

	// ApplySwapContractRecordedEvent records a swap contract and advances
	// the match status.
	ApplySwapContractRecordedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.SwapContractRecordedEvent) (*EventLogEntry, error)

	// ApplyAuditAckRecordedEvent records a contract audit acknowledgement.
	ApplyAuditAckRecordedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.AuditAckRecordedEvent) (*EventLogEntry, error)

	// ApplySwapRedemptionRecordedEvent records a swap redemption and the
	// resulting match and order reputation outcomes.
	ApplySwapRedemptionRecordedEvent(ctx context.Context, meta *EventLogMeta, policy *ReputationOutcomePolicy, event *meshevents.SwapRedemptionRecordedEvent) (*EventLogEntry, error)

	// ApplyRedemptionAckRecordedEvent records a redemption acknowledgement.
	// Maker redemption acknowledgements have no persistent state beyond the
	// event-log row.
	ApplyRedemptionAckRecordedEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.RedemptionAckRecordedEvent) (*EventLogEntry, error)

	// ApplyMatchFailedEvent marks a match inactive and updates the affected
	// orders and reputation outcomes.
	ApplyMatchFailedEvent(ctx context.Context, meta *EventLogMeta, policy *ReputationOutcomePolicy, event *meshevents.MatchFailedEvent) (*EventLogEntry, error)
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

// ReputationArchiver handles interactions with the reputation points table.
type ReputationArchiver interface {
	GetUserReputationData(ctx context.Context, user account.AccountID, pimgSz, matchSz, orderSz int) ([]*PreimageOutcome, []*MatchResult, []*OrderOutcome, error)
	ApplyReputationForgivenEvent(ctx context.Context, meta *EventLogMeta, event *meshevents.ReputationForgivenEvent) (*ReputationForgivenResult, error)
	SetReputationInputsListener(func(users ...account.AccountID))
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
	// OutcomeNoAddrAsTaker is recorded when a match times out at
	// NewlyMatched because the taker failed to provide their per-match
	// swap address in time, preventing the maker from broadcasting.
	OutcomeNoAddrAsTaker
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
	case OutcomeNoAddrAsTaker:
		return "no address as taker"
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

// BondPostedResult contains the result of applying a bond_posted event.
type BondPostedResult struct {
	// BondAdded is true if the bond was inserted, or false if the account
	// already had the bond.
	BondAdded bool
	Log       *EventLogEntry

	// Reputation inputs read after storing the bond, in the same transaction.
	Bonds     []*Bond
	Preimages []*PreimageOutcome
	Matches   []*MatchResult
	Orders    []*OrderOutcome
}

// OrderWithStatus contains an order and its stored status.
type OrderWithStatus struct {
	Order  order.Order
	Status order.OrderStatus
}

// OrderAcceptedUpdate contains an accepted order and the epoch information
// needed to store it. Unlike meshevents.OrderAcceptedEvent, it includes the
// epoch index, duration, and cancel epoch gap calculated by the market.
type OrderAcceptedUpdate struct {
	Order    order.Order
	EpochIdx int64
	EpochDur int64
	EpochGap int32
}

// MarketState is a market's stored lifecycle state.
type MarketState int16

const (
	MarketStateRunning MarketState = iota + 1
	MarketStateSuspended
	// MarketStateDraining means order intake has stopped while outstanding
	// epochs finish processing before suspension.
	MarketStateDraining
)

// MarketPendingAction is a scheduled market transition.
type MarketPendingAction int16

const (
	MarketPendingNone MarketPendingAction = iota
	MarketPendingSuspend
	MarketPendingResume
)

// MarketLifecycle stores a market's state, scheduled transition,
// epoch progress, and trading parameters. Epoch durations are in milliseconds.
type MarketLifecycle struct {
	Market string
	State  MarketState
	// StartEpochIdx is the first epoch of the current or scheduled market run.
	StartEpochIdx int64
	StartEpochDur int64
	// FinalEpochIdx is the last trading epoch of a scheduled or completed
	// suspension. It and FinalEpochDur are zero when no final epoch is recorded.
	FinalEpochIdx int64
	FinalEpochDur int64
	PendingAction MarketPendingAction
	// PendingEpochIdx and PendingEpochDur identify the epoch of the pending
	// suspension or resumption. Both are zero when PendingAction is MarketPendingNone.
	PendingEpochIdx int64
	PendingEpochDur int64
	// PersistBook records whether to retain booked orders on suspension.
	// It is nil during normal trading, before a suspension is scheduled.
	PersistBook *bool
	// ActiveEpochIdx is the current trading epoch, or zero while draining
	// or suspended.
	ActiveEpochIdx int64
	// ProcessedEpochIdx is the last processed epoch, or the last epoch skipped
	// during startup or resume. The next epoch to process is ProcessedEpochIdx + 1.
	ProcessedEpochIdx int64
	// RunParams are the trading parameters for the current market run.
	RunParams meshevents.MarketRunParams
}

// SuspendTime is the wall-clock end of the final trading epoch. It is only
// meaningful when suspension is scheduled, the market is draining, or the
// market is suspended with no resume scheduled.
func (lc *MarketLifecycle) SuspendTime() time.Time {
	return time.UnixMilli((lc.FinalEpochIdx + 1) * lc.FinalEpochDur).UTC()
}

// ResumeTime is the wall-clock start of the pending resume epoch. It is only
// meaningful while PendingAction is MarketPendingResume.
func (lc *MarketLifecycle) ResumeTime() time.Time {
	return time.UnixMilli(lc.PendingEpochIdx * lc.PendingEpochDur).UTC()
}

// MarketSuspendScheduledUpdate contains the event fields and resolved market asset IDs.
type MarketSuspendScheduledUpdate struct {
	Market        string
	Base, Quote   uint32
	FinalEpochIdx int64
	EpochDur      int64
	// PersistBook specifies whether suspension keeps booked orders.
	PersistBook bool
}

// MarketSuspendScheduledApplyResult contains the stored lifecycle and event log entry.
type MarketSuspendScheduledApplyResult struct {
	Log       *EventLogEntry
	Lifecycle *MarketLifecycle
}

// MarketSuspendedUpdate identifies the market and final trading epoch
// for completing a scheduled suspension.
type MarketSuspendedUpdate struct {
	Market        string
	Base, Quote   uint32
	FinalEpochIdx int64
	EpochDur      int64
	// Timestamp is the revocation time for orders removed when purging the book.
	Timestamp time.Time
}

// MarketSuspendedApplyResult contains the updated lifecycle, event log entry,
// and IDs of orders revoked when purging the book.
type MarketSuspendedApplyResult struct {
	Log         *EventLogEntry
	Lifecycle   *MarketLifecycle
	PurgeOrders []order.OrderID
}

// MarketResumeScheduledUpdate identifies the market and first trading epoch
// for scheduling a resumption.
type MarketResumeScheduledUpdate struct {
	Market        string
	Base, Quote   uint32
	StartEpochIdx int64
	EpochDur      int64
}

// MarketResumeScheduledApplyResult contains the updated lifecycle and event log
// entry for a scheduled resumption.
type MarketResumeScheduledApplyResult struct {
	Log       *EventLogEntry
	Lifecycle *MarketLifecycle
}

// MarketResumedUpdate contains the market, epoch, trading parameters, and order
// revocations for completing a scheduled resumption.
type MarketResumedUpdate struct {
	Market        string
	Base, Quote   uint32
	StartEpochIdx int64
	EpochDur      int64
	Timestamp     time.Time
	// ResumeRevokes lists orders to revoke before resuming trading. Orders that
	// are no longer booked are skipped.
	ResumeRevokes []*StartupOrderRevoke
	RunParams     meshevents.MarketRunParams
}

// MarketResumedApplyResult contains the updated lifecycle, event log entry,
// and orders revoked when resuming trading.
type MarketResumedApplyResult struct {
	Log           *EventLogEntry
	Lifecycle     *MarketLifecycle
	ResumeRevokes []*StartupOrderRevoke
}

// MarketStartedApplyResult is the stored outcome of a market_started event.
type MarketStartedApplyResult struct {
	Log       *EventLogEntry
	Lifecycle *MarketLifecycle
}

// MarketStartedUpdate contains the decoded orders and resolved market asset IDs
// for a market_started event. This type exists in addition to
// meshevents.MarketStartedEvent so the database and in-memory updates can share
// the same validated orders without decoding them again.
type MarketStartedUpdate struct {
	Market          string
	Base            uint32
	Quote           uint32
	CurrentEpochIdx int64
	EpochDur        int64
	RunParams       meshevents.MarketRunParams
	RevocationTime  time.Time
	BookedRevokes   []*StartupOrderRevoke
	// EpochRevokes lists the abandoned epoch orders. It must include every
	// active epoch order exactly once.
	EpochRevokes []*StartupOrderRevoke
}

// OrdersRevokedUpdate contains the booked orders selected for revocation.
// Unlike meshevents.OrdersRevokedEvent, it contains the resolved orders
// rather than an account or a list of order IDs.
type OrdersRevokedUpdate struct {
	Reason     meshevents.OrderRevokeReason
	RevokeTime time.Time
	Orders     []*order.LimitOrder
}

// ReputationOutcomePolicy sets how many outcomes of each class are retained
// per account and which successful cancellations count as penalties.
type ReputationOutcomePolicy struct {
	PreimageLimit int
	MatchLimit    int
	OrderLimit    int

	// Successful cancellations with a nonnegative epoch gap below this
	// threshold are penalized.
	FreeCancelThreshold int32
}

// PreimageMissUpdate identifies an order to revoke for a missing preimage and
// the time to record for its revocation.
type PreimageMissUpdate struct {
	Order      order.Order
	RevokeTime time.Time
}

// PreimageRevealUpdate contains an order and its revealed preimage.
type PreimageRevealUpdate struct {
	Order    order.Order
	Preimage order.Preimage
}

// EpochProcessedUpdate contains the database changes resulting from matching
// an epoch, including preimage results, order changes, and matches.
type EpochProcessedUpdate struct {
	Epoch           *EpochResults
	Misses          []*PreimageMissUpdate
	Reveals         []*PreimageRevealUpdate
	TradesBooked    []*order.LimitOrder
	TradesPartial   []*order.LimitOrder
	TradesCompleted []order.Order
	TradesCanceled  []*order.LimitOrder

	// TradesFailed contains orders that did not match and will not enter the
	// book, such as market orders and immediate limit orders. They are archived
	// with executed status.
	TradesFailed    []order.Order
	CancelsFailed   []*order.CancelOrder
	CancelsExecuted []*order.CancelOrder
	Matches         []*order.Match
}

// SuspendedCancelUpdate contains a cancel order, its target details, and the
// expected match for canceling an order while the market is suspended.
type SuspendedCancelUpdate struct {
	Market          string
	Base            uint32
	Quote           uint32
	Cancel          *order.CancelOrder
	TargetOrderID   order.OrderID
	TargetAccount   account.AccountID
	TargetSell      bool
	EpochIdx        int64
	EpochDur        int64
	FeeRateBase     uint64
	FeeRateQuote    uint64
	MatchServerTime time.Time
	Match           *order.Match
}

// SuspendedCancelApplyResult contains the event log entry, executed cancel,
// stored target order, and match derived from the stored target.
type SuspendedCancelApplyResult struct {
	Log         *EventLogEntry
	Cancel      *order.CancelOrder
	TargetOrder *order.LimitOrder
	Match       *order.Match
}

// StartupOrderRevoke contains an order and its startup revocation reason.
type StartupOrderRevoke struct {
	Order  order.Order
	Reason meshevents.StartupOrderRevokeReason
}
