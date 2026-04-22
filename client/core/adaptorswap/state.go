// Package adaptorswap implements the client-side state machine for
// BIP-340 adaptor-signature atomic swaps. It consumes msgjson
// Adaptor* messages, drives the protocol through its phases, and
// calls back into the asset layer for chain interaction.
//
// Phase-2 step 3 of the master plan. The struct and Phase enum are
// the review target; handler bodies are stubs that document the
// expected transitions and will be filled in once simnet
// validation is available.
package adaptorswap

import (
	"sync"
	"time"

	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// Role distinguishes the two asymmetric sides of the swap. Bound at
// match time from the market configuration (BTC-holder is Initiator
// for a BTC/XMR market under Option 1).
type Role uint8

const (
	RoleInitiator   Role = iota // scriptable-chain holder (BTC)
	RoleParticipant             // non-scriptable holder (XMR)
)

func (r Role) String() string {
	switch r {
	case RoleInitiator:
		return "initiator"
	case RoleParticipant:
		return "participant"
	}
	return "unknown"
}

// Phase represents where a swap is in its state machine. Phases are
// strictly forward-progressing; errors or failures move to a
// terminal-failure phase rather than rewinding.
type Phase uint8

const (
	PhaseInit Phase = iota // freshly matched, nothing on-chain

	// Setup (off-chain).
	PhaseKeysSent     // participant has sent keys + DLEQ
	PhaseKeysReceived // initiator has sent back combined keys + refund-tx templates
	PhaseRefundPresigned

	// Lock.
	PhaseLockBroadcast
	PhaseLockConfirmed
	PhaseXmrSent
	PhaseXmrConfirmed

	// Redeem (happy path).
	PhaseSpendPresig
	PhaseSpendBroadcast
	PhaseXmrSwept

	// Refund / punish (branch).
	PhaseRefundTxBroadcast
	PhaseCoopRefund
	PhasePunish

	PhaseComplete // terminal success
	PhaseFailed   // terminal failure
)

func (p Phase) String() string {
	switch p {
	case PhaseInit:
		return "init"
	case PhaseKeysSent:
		return "keys-sent"
	case PhaseKeysReceived:
		return "keys-received"
	case PhaseRefundPresigned:
		return "refund-presigned"
	case PhaseLockBroadcast:
		return "lock-broadcast"
	case PhaseLockConfirmed:
		return "lock-confirmed"
	case PhaseXmrSent:
		return "xmr-sent"
	case PhaseXmrConfirmed:
		return "xmr-confirmed"
	case PhaseSpendPresig:
		return "spend-presig"
	case PhaseSpendBroadcast:
		return "spend-broadcast"
	case PhaseXmrSwept:
		return "xmr-swept"
	case PhaseRefundTxBroadcast:
		return "refund-broadcast"
	case PhaseCoopRefund:
		return "coop-refund"
	case PhasePunish:
		return "punish"
	case PhaseComplete:
		return "complete"
	case PhaseFailed:
		return "failed"
	}
	return "unknown"
}

// IsTerminal reports whether the phase is a final state.
func (p Phase) IsTerminal() bool {
	return p == PhaseComplete || p == PhaseFailed
}

// State is the complete per-swap state record. It persists through
// restarts via Snapshot / Restore.
type State struct {
	mu sync.Mutex

	// Identity.
	SwapID  [32]byte
	OrderID [32]byte
	MatchID [32]byte
	Role    Role
	PairBTC uint32 // asset ID of the scriptable side
	PairXMR uint32 // asset ID of the non-scriptable side

	// Per-swap fresh key material. Generated locally; never reused
	// across swaps.
	BtcSignKey      *btcec.PrivateKey   // this party's secp sign key (2-of-2)
	XmrSpendKeyHalf *edwards.PrivateKey // this party's ed25519 spend-key half
	DLEQProof       []byte              // proof tying XmrSpendKeyHalf to btcec pubkey

	// Counterparty material, populated over the setup phase.
	PeerBtcSignPub  []byte             // x-only BIP-340
	PeerXmrSpendPub *edwards.PublicKey // ed25519 pubkey
	PeerDLEQProof   []byte             // peer's DLEQ proof
	PeerScalarSecp  *btcec.PublicKey   // secp point extracted from peer DLEQ

	// XMR-side full key material (derivable once both parties
	// contributed). Both parties hold these.
	FullSpendPub *edwards.PublicKey // combined spend pubkey
	FullViewKey  *edwards.PrivateKey

	// BTC-side script + transaction artifacts.
	Lock          *btcadaptor.LockTxOutput
	Refund        *btcadaptor.RefundTxOutput
	LockTx        *wire.MsgTx
	LockVout      uint32
	LockHeight    int64
	RefundTx      *wire.MsgTx
	SpendRefundTx *wire.MsgTx
	SpendTx       *wire.MsgTx // filled once initiator builds it

	// Collected signatures.
	OwnRefundSig          []byte
	PeerRefundSig         []byte
	SpendRefundAdaptorSig *adaptorsigs.AdaptorSignature
	SpendAdaptorSig       *adaptorsigs.AdaptorSignature

	// XMR-side lock artifacts.
	XmrSendTxID      string
	XmrRestoreHeight uint64
	// Recovered XMR-key-half scalar from RecoverTweakBIP340. Set when
	// the counterparty's completed sig appears on-chain and the
	// recovery runs. Combined with XmrSpendKeyHalf to form the full
	// spend key.
	RecoveredPeerScalar *btcec.ModNScalar

	// State machine tracking.
	Phase     Phase
	Updated   time.Time
	LastError string
}

// Snapshot returns a serializable copy of the state for persistence.
// Keys, scalars, and *wire.MsgTx are encoded as bytes; see
// state_persist.go (not yet written) for the exact serialization.
func (s *State) Snapshot() *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &Snapshot{
		Phase:   s.Phase,
		Updated: s.Updated,
		// ... more fields elided in this stub
	}
}

// Snapshot is the persisted form of State. Persisted before each
// phase transition so a process restart can resume in-flight swaps.
type Snapshot struct {
	Phase   Phase
	Updated time.Time
	// ... full field set TBD once serialization is implemented
}

// Event is an input to the state machine. Events come from three
// sources: inbound msgjson Adaptor* messages relayed by the server,
// local wallet callbacks (lockTx confirmed, xmr confirmed, spend
// observed on-chain), and timeouts.
type Event interface {
	eventTag()
}

type EventKeysReceived struct{ Setup any } // server routes a peer's setup message
type EventRefundPresignedReceived struct{ RefundSig, AdaptorSig []byte }
type EventLockConfirmed struct{ Height int64 }
type EventXmrConfirmed struct{ Height uint64 }
type EventSpendPresigReceived struct {
	SpendTx    []byte
	AdaptorSig []byte
}
type EventSpendObservedOnChain struct{ Witness [][]byte }
type EventRefundObservedOnChain struct{ Witness [][]byte }
type EventTimeout struct{ Reason string }

func (EventKeysReceived) eventTag()            {}
func (EventRefundPresignedReceived) eventTag() {}
func (EventLockConfirmed) eventTag()           {}
func (EventXmrConfirmed) eventTag()            {}
func (EventSpendPresigReceived) eventTag()     {}
func (EventSpendObservedOnChain) eventTag()    {}
func (EventRefundObservedOnChain) eventTag()   {}
func (EventTimeout) eventTag()                 {}

// Orchestrator drives a State through the protocol.
//
// The interface shape - asset callbacks, message sender, persistence
// hook - is deliberately minimal. Concrete implementations bind to
// client/core's Core once the server-side state machine spec is
// ratified and the asset primitives are live-tested.
type Orchestrator struct {
	state    *State
	assetBTC BTCAssetAdapter
	assetXMR XMRAssetAdapter
	sendMsg  MessageSender
	persist  StatePersister
}

// BTCAssetAdapter is what the orchestrator needs from the BTC asset
// side. Matches the primitives already committed in
// internal/adaptorsigs/btc (FundBroadcastTaproot, ObserveSpend).
type BTCAssetAdapter interface {
	FundBroadcastTaproot(pkScript []byte, value int64) (tx *wire.MsgTx, vout uint32, height int64, err error)
	ObserveSpend(outpoint wire.OutPoint, startHeight int64) (witness [][]byte, err error)
	BroadcastTx(tx *wire.MsgTx) (txid string, err error)
	CurrentHeight() (int64, error)
}

// XMRAssetAdapter is what the orchestrator needs from the XMR asset
// side. Matches the primitives already committed in
// client/asset/xmr/swap.go (SendToSharedAddress, WatchSharedAddress,
// SweepSharedAddress).
type XMRAssetAdapter interface {
	SendToSharedAddress(addr string, amount uint64) (txid string, sentHeight uint64, err error)
	WatchSharedAddress(swapID, addr, viewKeyHex string, restoreHeight, expectedAmount uint64) (XMRWatch, error)
	SweepSharedAddress(swapID, addr, spendKeyHex, viewKeyHex string, restoreHeight uint64, dest string) (txid string, err error)
}

// XMRWatch mirrors the XMRWatchHandle type in client/asset/xmr.
type XMRWatch interface {
	Synced() bool
	HasFunds() (present, unlocked bool, err error)
	Close() error
}

// MessageSender sends a msgjson message over the server-routed peer
// channel. The underlying transport is client/comms' ws conn, but
// the orchestrator only needs to know "send this msg to the peer."
type MessageSender interface {
	SendToPeer(route string, payload any) error
}

// StatePersister saves a Snapshot before each state transition. An
// in-memory implementation is fine for tests; production hooks into
// the client DB.
type StatePersister interface {
	Save(swapID [32]byte, snap *Snapshot) error
	Load(swapID [32]byte) (*Snapshot, error)
}

// Handle is the top-level event dispatcher. It locks the state,
// validates the event against the current phase, performs the
// transition (including persistence), and returns any error that
// should be propagated up to Core.
//
// Unimplemented transitions are explicit: the orchestrator does
// not silently drop events; it returns an error and logs, making
// protocol bugs loud.
func (o *Orchestrator) Handle(evt Event) error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	// The body below is a skeleton. Each case will be filled in as
	// the orchestrator transitions from design doc to live code.
	switch o.state.Phase {
	case PhaseInit:
		// Participant: send AdaptorSetupPart. Transition to PhaseKeysSent.
		// Initiator: wait for EventKeysReceived.
	case PhaseKeysSent:
		// Participant: wait for EventKeysReceived.
	case PhaseKeysReceived:
		// Both: pre-sign refund chain, transition to PhaseRefundPresigned.
	case PhaseRefundPresigned:
		// Initiator: broadcast lockTx via FundBroadcastTaproot.
		// Participant: wait for EventLockConfirmed.
	case PhaseLockBroadcast:
		// Initiator: wait for its own wallet to confirm.
	case PhaseLockConfirmed:
		// Participant: send XMR to shared address, transition to PhaseXmrSent.
	case PhaseXmrSent:
		// Initiator: wait for EventXmrConfirmed.
	case PhaseXmrConfirmed:
		// Initiator: build + adaptor-sign spendTx, send AdaptorSpendPresig.
	case PhaseSpendPresig:
		// Participant: decrypt + broadcast spendTx, transition to PhaseSpendBroadcast.
	case PhaseSpendBroadcast:
		// Initiator: ObserveSpend, RecoverTweakBIP340, open sweep wallet.
	case PhaseXmrSwept:
		// Both: terminal success, set PhaseComplete.
	case PhaseRefundTxBroadcast:
		// Initiator: decide coop-refund vs. wait.
		// Participant: wait-then-punish after CSV.
	case PhaseCoopRefund:
		// Participant: recover initiator scalar from on-chain sig, sweep XMR.
	case PhasePunish:
		// Participant: sign punish leaf, broadcast. Accept XMR stranding.
	}
	return nil
}
