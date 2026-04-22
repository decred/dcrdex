// Package adaptor implements the server-side state machine for
// BIP-340 adaptor-signature atomic swaps. The server does not
// participate in the swap crypto (never holds private keys, never
// signs swap transactions); its role is:
//
//  1. Route msgjson Adaptor* messages between the two matched
//     clients without modifying payloads.
//  2. Validate that each message arrives in the expected phase
//     and carries well-formed, protocol-conforming data - e.g.
//     DLEQ proof verifies, adaptor sig has the right structure,
//     refund tx is built from the agreed scripts.
//  3. Observe on-chain events via the asset backends: lockTx
//     confirmation, XMR arrival at the shared address, spendTx
//     broadcast, refundTx broadcast, spendRefund coop/punish
//     spends.
//  4. Enforce protocol timeouts; when a party stalls in a phase
//     they are expected to act in, apply penalty outcomes the
//     reputation system can consume.
//
// This is the server mirror of client/core/adaptorswap/state.go.
// Many of the Phase values are the same, but the set of events
// the server reacts to is narrower - the server never receives
// "local wallet signed X" events because it has no wallet.
//
// Phase-3 scaffold. Handler bodies stubbed; wiring into
// server/swap, server/market, server/comms, and server/auth is the
// next stage and was deliberately deferred until the client CLI
// was validated on simnet (task #12 complete).
package adaptor

import (
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// Phase on the server mirrors the client state machine, with two
// differences: (a) the server never sits in phases that are purely
// local to a client (e.g. a party thinking about whether to sign a
// refund); (b) the server has its own "awaiting-audit" phases that
// reflect what it's watching for on-chain.
type Phase uint8

const (
	PhaseInit Phase = iota // match just created; nothing exchanged yet

	// Setup messages flow in order PartSetup -> InitSetup -> Presigned.
	PhaseAwaitingPartSetup // waiting for AdaptorSetupPart from participant
	PhaseAwaitingInitSetup // waiting for AdaptorSetupInit from initiator
	PhaseAwaitingPresigned // waiting for AdaptorRefundPresigned from participant

	// Lock phase: server audits on-chain events.
	PhaseAwaitingLocked    // waiting for AdaptorLocked + audit of lockTx
	PhaseAwaitingXmrLocked // waiting for AdaptorXmrLocked + XMR audit

	// Redeem phase.
	PhaseAwaitingSpendPresig    // waiting for AdaptorSpendPresig from initiator
	PhaseAwaitingSpendBroadcast // waiting for AdaptorSpendBroadcast + audit

	// Refund/punish branch.
	PhaseAwaitingRefundResolution // refundTx was broadcast; waiting for coop-or-punish
	PhaseAwaitingCoopOrPunish     // in-progress refund; waiting for the branching choice

	PhaseComplete // terminal success: happy path or acceptable refund
	PhaseFailed   // terminal failure: timeout, invalid data, consensus rejection
)

// String returns a human-readable phase name for logging.
func (p Phase) String() string {
	switch p {
	case PhaseInit:
		return "init"
	case PhaseAwaitingPartSetup:
		return "awaiting-part-setup"
	case PhaseAwaitingInitSetup:
		return "awaiting-init-setup"
	case PhaseAwaitingPresigned:
		return "awaiting-presigned"
	case PhaseAwaitingLocked:
		return "awaiting-locked"
	case PhaseAwaitingXmrLocked:
		return "awaiting-xmr-locked"
	case PhaseAwaitingSpendPresig:
		return "awaiting-spend-presig"
	case PhaseAwaitingSpendBroadcast:
		return "awaiting-spend-broadcast"
	case PhaseAwaitingRefundResolution:
		return "awaiting-refund-resolution"
	case PhaseAwaitingCoopOrPunish:
		return "awaiting-coop-or-punish"
	case PhaseComplete:
		return "complete"
	case PhaseFailed:
		return "failed"
	}
	return "unknown"
}

func (p Phase) IsTerminal() bool {
	return p == PhaseComplete || p == PhaseFailed
}

// Outcome reports how a swap ended, for the reputation/penalty
// layer. The server translates these to db.Outcome* values that
// server/auth consumes when adjusting trader scores.
type Outcome uint8

const (
	OutcomeSuccess           Outcome = iota // normal redeem
	OutcomeCoopRefund                       // both parties agreed to unwind
	OutcomePunishedInitiator                // initiator stalled after participant locked XMR
	OutcomeInitiatorBailed                  // initiator silent before participant locked XMR
	OutcomeParticipantBailed                // participant never locked XMR after BTC confirmed
	OutcomeProtocolError                    // malformed msg or invalid proof
)

// State is the per-match server-side record.
type State struct {
	mu sync.Mutex

	// Identity.
	MatchID order.MatchID
	OrderID order.OrderID

	// Pair metadata.
	ScriptableAsset uint32
	NonScriptAsset  uint32

	// Locktime parameters agreed in the setup phase.
	LockBlocks uint32

	// Peer material, accumulated as setup progresses. The server
	// only holds pubkeys / proofs / unsigned txs - never private
	// keys or adaptor secrets.
	ParticipantPubSpendKeyHalf []byte // ed25519
	ParticipantPubSignKeyHalf  []byte // secp256k1 x-only
	ParticipantDLEQProof       []byte
	InitiatorPubSpendKeyHalf   []byte
	InitiatorPubSignKeyHalf    []byte
	InitiatorDLEQProof         []byte

	// Combined XMR pubkey + view key, as the initiator derived
	// them. The server holds these so it could, if needed, spin up
	// a view-only XMR wallet to audit participant's send. Whether
	// the server actually does that depends on operator
	// configuration (Phase-5 question).
	FullSpendPub []byte
	FullViewKey  []byte

	// Lock audit.
	LockTxID   []byte
	LockVout   uint32
	LockValue  uint64
	LockHeight int64

	// XMR audit.
	XmrTxID       []byte
	XmrSentHeight uint64

	// Spend audit. The server records spendTx and later any
	// refundTx / spendRefundTx that reach the mempool or chain.
	SpendTxID       []byte
	RefundTxID      []byte
	SpendRefundTxID []byte

	// State machine tracking.
	Phase       Phase
	Updated     time.Time
	LastError   string
	FailOutcome Outcome

	// Timeout deadlines for the current phase. When the current
	// clock passes PhaseDeadline and the expected actor has not
	// advanced the phase, the coordinator transitions to
	// PhaseFailed with a stalling outcome.
	PhaseDeadline time.Time
}

// Event types consumed by the server state machine.
type Event interface {
	eventTag()
}

// Peer messages - these are the inbound Adaptor* msgjson messages
// relayed through server/comms.
type EventPartSetup struct{ Msg *msgjson.AdaptorSetupPart }
type EventInitSetup struct{ Msg *msgjson.AdaptorSetupInit }
type EventPresigned struct {
	Msg *msgjson.AdaptorRefundPresigned
}
type EventLocked struct{ Msg *msgjson.AdaptorLocked }
type EventXmrLocked struct{ Msg *msgjson.AdaptorXmrLocked }
type EventSpendPresig struct{ Msg *msgjson.AdaptorSpendPresig }
type EventSpendBroadcast struct {
	Msg *msgjson.AdaptorSpendBroadcast
}
type EventRefundBroadcast struct {
	Msg *msgjson.AdaptorRefundBroadcast
}
type EventCoopRefund struct{ Msg *msgjson.AdaptorCoopRefund }
type EventPunish struct{ Msg *msgjson.AdaptorPunish }

// Chain observations from the server's asset backends.
type EventLockConfirmed struct {
	Height int64
	TxID   []byte
	Vout   uint32
	Value  uint64
}
type EventXmrOutputConfirmed struct {
	SharedAddr string
	Amount     uint64
}
type EventSpendOnChain struct {
	TxID    []byte
	Witness [][]byte
}
type EventRefundOnChain struct{ TxID []byte }
type EventCoopRefundOnChain struct {
	TxID    []byte
	Witness [][]byte
}
type EventPunishOnChain struct{ TxID []byte }

// EventTimeout fires when the current phase exceeds its deadline.
type EventTimeout struct{ Reason string }

func (EventPartSetup) eventTag()          {}
func (EventInitSetup) eventTag()          {}
func (EventPresigned) eventTag()          {}
func (EventLocked) eventTag()             {}
func (EventXmrLocked) eventTag()          {}
func (EventSpendPresig) eventTag()        {}
func (EventSpendBroadcast) eventTag()     {}
func (EventRefundBroadcast) eventTag()    {}
func (EventCoopRefund) eventTag()         {}
func (EventPunish) eventTag()             {}
func (EventLockConfirmed) eventTag()      {}
func (EventXmrOutputConfirmed) eventTag() {}
func (EventSpendOnChain) eventTag()       {}
func (EventRefundOnChain) eventTag()      {}
func (EventCoopRefundOnChain) eventTag()  {}
func (EventPunishOnChain) eventTag()      {}
func (EventTimeout) eventTag()            {}

// Coordinator is the per-match state machine runner. It owns a
// State, consumes events, validates them against the current
// phase, and emits side effects: message routing to the peer,
// penalty outcomes, state persistence.
//
// The interface shape is kept narrow so it can be unit-tested
// against mocks, and bound into the larger server/swap coordinator
// once the handler logic is filled in.
type Coordinator struct {
	state   *State
	router  PeerRouter
	btc     BTCAuditor
	xmr     XMRAuditor
	report  OutcomeReporter
	persist StatePersister
	// cfg is runtime configuration; not persisted.
	cfg *Config
}

// PeerRouter relays a validated Adaptor* message to the other
// matched client. The server does not mutate payload bytes; it
// just forwards. Implementations live in server/comms.
type PeerRouter interface {
	SendTo(matchID order.MatchID, role Role, route string, payload any) error
}

// Role identifies which side of the match a message is coming from
// or going to. The adaptor protocol is asymmetric, so this matters.
type Role uint8

const (
	RoleInitiator Role = iota // scriptable-chain holder
	RoleParticipant
)

func (r Role) String() string {
	switch r {
	case RoleInitiator:
		return "initiator"
	case RoleParticipant:
		return "participant"
	}
	return fmt.Sprintf("role(%d)", uint8(r))
}

// BTCAuditor observes the scriptable-chain (BTC) events the
// coordinator needs to decide what happened. It does not hold
// keys; it only reads the chain. Maps onto server/asset/btc.
type BTCAuditor interface {
	// WaitLockConfirm blocks until the lockTx has confirmed with
	// the required depth, or ctx is cancelled.
	WaitLockConfirm(txid []byte, vout uint32, minConf uint32) (height int64, err error)
	// WaitSpend blocks until the outpoint is spent and returns
	// the spending tx's witness for the relevant input.
	WaitSpend(outpoint []byte, startHeight int64) (txid []byte, witness [][]byte, err error)
	// CurrentHeight is used for timeout decisions.
	CurrentHeight() (int64, error)
}

// XMRAuditor optionally observes the XMR side. Most dcrdex
// operators will want to run a monerod + view-only wallet so they
// can adjudicate refund/punish claims that reference XMR outputs.
// A permissive operator config may skip this and trust the
// counterparty reports.
type XMRAuditor interface {
	WaitOutputAtAddress(sharedAddr string, amount uint64, minConf uint32) error
}

// OutcomeReporter feeds the reputation system. Implementations
// live in server/auth; they translate adaptor-swap Outcome values
// into db.Outcome* entries that affect trader scores.
type OutcomeReporter interface {
	Report(matchID order.MatchID, role Role, outcome Outcome) error
}

// StatePersister saves State before each transition so a process
// restart can resume in-flight matches. Backed by server/db.
type StatePersister interface {
	Save(matchID order.MatchID, s *State) error
	Load(matchID order.MatchID) (*State, error)
}

// Handle lives in coordinator.go alongside the phase handlers.
