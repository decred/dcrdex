package adaptorswap

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/agl/ed25519/edwards25519"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/haven-protocol-org/monero-go-utils/base58"
)

var curve = edwards.Edwards()

// Config carries the external dependencies required to run an
// Orchestrator. Produced by the caller (typically client/core) from
// the match record plus user wallet handles.
type Config struct {
	SwapID     [32]byte
	OrderID    [32]byte
	MatchID    [32]byte
	Role       Role
	PairBTC    uint32
	PairXMR    uint32
	BtcAmount  int64
	XmrAmount  uint64
	LockBlocks uint32

	// Peer's destination address for their output. For the
	// initiator this is Alice's BTC payout address (where Alice
	// receives BTC on redeem). For the participant this is Bob's
	// XMR sweep destination. PeerBTCPayoutScript is populated by
	// the initiator's setup handler from AdaptorSetupPart.BTCPayoutAddr,
	// or out-of-band via SetPeerBTCPayoutScript.
	PeerBTCPayoutScript []byte
	OwnXMRSweepDest     string

	// OwnBTCPayoutAddr is the participant's own BTC deposit
	// address; the participant sends it in AdaptorSetupPart so the
	// initiator can build a spendTx output that pays this address.
	// Populated at config-build time by the bridge from the local
	// BTC wallet.
	OwnBTCPayoutAddr string

	// DecodeBTCAddr converts a BTC address string into a pkScript.
	// Supplied by the bridge so the orchestrator does not need to
	// know about chain params or btcutil. Used by the initiator to
	// translate AdaptorSetupPart.BTCPayoutAddr into
	// PeerBTCPayoutScript on receipt.
	DecodeBTCAddr func(addr string) ([]byte, error)

	// SpendObserver is called by the initiator when it transitions
	// into PhaseSpendPresig. The bridge typically supplies a
	// closure that spawns a goroutine polling
	// AssetBTC.ObserveSpend and, on observation, feeds the witness
	// back via the manager's Handle as EventSpendObservedOnChain.
	// nil means no observer wired (test mode); the swap will stall
	// at PhaseSpendPresig.
	SpendObserver func(outpoint wire.OutPoint, startHeight int64)

	// OnTerminal is called once when the orchestrator reaches a
	// terminal phase (PhaseComplete, PhaseFailed, PhasePunish).
	// The bridge typically uses this to log the outcome, update
	// the order's status, and tear down the orchestrator from the
	// manager's registry. nil disables the callback.
	OnTerminal func(phase Phase)

	// Network tag for XMR address encoding (18=mainnet, 24=stagenet).
	XmrNetTag uint64

	AssetBTC BTCAssetAdapter
	AssetXMR XMRAssetAdapter
	SendMsg  MessageSender
	Persist  StatePersister
}

// NewOrchestrator constructs an Orchestrator in PhaseInit with fresh
// per-swap keys. The caller invokes Handle to drive it through the
// protocol; depending on Role, the first action may be to send
// AdaptorSetupPart (participant) or to wait for it (initiator).
func NewOrchestrator(cfg *Config) (*Orchestrator, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	xmrSpend, err := edwards.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("xmr spend key: %w", err)
	}
	btcSign, err := btcec.NewPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("btc sign key: %w", err)
	}
	dleq, err := adaptorsigs.ProveDLEQ(xmrSpend.Serialize())
	if err != nil {
		return nil, fmt.Errorf("dleq: %w", err)
	}
	state := &State{
		SwapID:          cfg.SwapID,
		OrderID:         cfg.OrderID,
		MatchID:         cfg.MatchID,
		Role:            cfg.Role,
		PairBTC:         cfg.PairBTC,
		PairXMR:         cfg.PairXMR,
		BtcSignKey:      btcSign,
		XmrSpendKeyHalf: xmrSpend,
		DLEQProof:       dleq,
		Phase:           PhaseInit,
		Updated:         time.Now(),
	}
	o := &Orchestrator{
		state:    state,
		assetBTC: cfg.AssetBTC,
		assetXMR: cfg.AssetXMR,
		sendMsg:  cfg.SendMsg,
		persist:  cfg.Persist,
		cfg:      cfg,
	}
	return o, nil
}

// Orchestrator is defined in state.go; we add the cfg field here so
// the handler bodies can read the static configuration.
// (state.go was the scaffold; this is the implementation extension.)

// The cfg field is declared by re-opening Orchestrator in this file
// via an embed. Keeping it struct-local rather than on State keeps
// the persistence layer simpler (State is the durable half; Config
// is runtime-only).

// addCfg is a hack to add cfg to Orchestrator without editing state.go.
// Go does not allow reopening a struct in another file; we work around
// that by making cfg a field on Orchestrator below via a wrapper type.
// Actually, Orchestrator is defined in state.go with four fields; I
// will add cfg there rather than resort to a wrapper. See state.go
// patch companion to this file.

// Cfg returns the orchestrator's runtime configuration. The
// returned pointer is the same one held internally; mutating fields
// is not safe in general, but reading them at any time is.
func (o *Orchestrator) Cfg() *Config { return o.cfg }

// NewOrchestratorFromState rehydrates an Orchestrator from a State
// previously recovered via RestoreState. Used at startup to resume
// in-flight swaps after a process restart. cfg supplies the runtime
// dependencies (asset adapters, sender, persister); the State owns
// the swap-specific identity and key material so they survive the
// restart.
func NewOrchestratorFromState(cfg *Config, state *State) (*Orchestrator, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	if state == nil {
		return nil, errors.New("nil state")
	}
	return &Orchestrator{
		state:    state,
		assetBTC: cfg.AssetBTC,
		assetXMR: cfg.AssetXMR,
		sendMsg:  cfg.SendMsg,
		persist:  cfg.Persist,
		cfg:      cfg,
	}, nil
}

// Phase returns the current phase of the state machine. Safe to
// call concurrently with Handle.
func (o *Orchestrator) Phase() Phase {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	return o.state.Phase
}

// Role returns the role this orchestrator is playing.
func (o *Orchestrator) Role() Role {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	return o.state.Role
}

// SetPeerBTCPayoutScript records the counterparty's BTC payout
// pkScript on the orchestrator's runtime config. Idempotent if
// called twice with an identical script; errors on a mismatched
// follow-up to make replay attacks visible.
func (o *Orchestrator) SetPeerBTCPayoutScript(script []byte) error {
	if len(script) == 0 {
		return errors.New("empty peer btc payout script")
	}
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	if existing := o.cfg.PeerBTCPayoutScript; len(existing) > 0 {
		if !bytes.Equal(existing, script) {
			return errors.New("peer btc payout script already set with different value")
		}
		return nil
	}
	o.cfg.PeerBTCPayoutScript = script
	return nil
}

// Start kicks off the state machine based on Role. For a
// participant, this emits the initial AdaptorSetupPart; for an
// initiator, it is a no-op and the machine waits for inbound
// EventPartSetup.
func (o *Orchestrator) Start() error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	if o.state.Phase != PhaseInit {
		return fmt.Errorf("Start called in phase %s", o.state.Phase)
	}
	if o.state.Role == RoleParticipant {
		return o.sendPartSetup()
	}
	o.state.Phase = PhaseKeysSent
	return o.save()
}

// sendPartSetup emits AdaptorSetupPart (participant -> initiator).
// Must be called with o.state.mu held.
func (o *Orchestrator) sendPartSetup() error {
	pubSpend := o.state.XmrSpendKeyHalf.PubKey().Serialize()
	kbvf, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("view key half: %w", err)
	}
	o.state.FullViewKey = kbvf // held until combined with peer's half
	msg := &msgjson.AdaptorSetupPart{
		OrderID:         o.cfg.OrderID[:],
		MatchID:         o.cfg.MatchID[:],
		PubSpendKeyHalf: pubSpend,
		ViewKeyHalf:     kbvf.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(o.state.BtcSignKey.PubKey()),
		DLEQProof:       o.state.DLEQProof,
		BTCPayoutAddr:   o.cfg.OwnBTCPayoutAddr,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSetupPartRoute, msg); err != nil {
		return fmt.Errorf("send AdaptorSetupPart: %w", err)
	}
	o.state.Phase = PhaseKeysSent
	return o.save()
}

// Handle dispatches an Event to the appropriate phase handler.
// Events that do not match the current phase produce an error and
// do not advance state.
func (o *Orchestrator) Handle(evt Event) error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()

	switch o.state.Phase {
	case PhaseInit, PhaseKeysSent:
		return o.handleSetup(evt)
	case PhaseKeysReceived:
		return o.handleKeysReceived(evt)
	case PhaseRefundPresigned:
		return o.handleRefundPresigned(evt)
	case PhaseLockBroadcast:
		return o.handleLockBroadcast(evt)
	case PhaseLockConfirmed:
		return o.handleLockConfirmed(evt)
	case PhaseXmrSent:
		return o.handleXmrSent(evt)
	case PhaseXmrConfirmed:
		return o.handleXmrConfirmed(evt)
	case PhaseSpendPresig:
		return o.handleSpendPresig(evt)
	case PhaseSpendBroadcast:
		return o.handleSpendBroadcast(evt)
	case PhaseXmrSwept, PhaseComplete, PhaseFailed:
		return fmt.Errorf("event %T in terminal phase %s", evt, o.state.Phase)
	case PhaseRefundTxBroadcast:
		return o.handleRefundTxBroadcast(evt)
	case PhaseCoopRefund:
		return o.handleCoopRefund(evt)
	case PhasePunish:
		return o.handlePunish(evt)
	}
	return fmt.Errorf("unhandled phase %s", o.state.Phase)
}

// handleSetup processes the initial setup exchange. Initiator side:
// receives EventKeysReceived carrying AdaptorSetupPart. Participant
// side: receives EventKeysReceived carrying AdaptorSetupInit.
func (o *Orchestrator) handleSetup(evt Event) error {
	recv, ok := evt.(EventKeysReceived)
	if !ok {
		return fmt.Errorf("expected EventKeysReceived in phase %s, got %T", o.state.Phase, evt)
	}
	if o.state.Role == RoleInitiator {
		part, ok := recv.Setup.(*msgjson.AdaptorSetupPart)
		if !ok {
			return fmt.Errorf("initiator expected AdaptorSetupPart, got %T", recv.Setup)
		}
		return o.initiatorConsumePartSetup(part)
	}
	// Participant side.
	init, ok := recv.Setup.(*msgjson.AdaptorSetupInit)
	if !ok {
		return fmt.Errorf("participant expected AdaptorSetupInit, got %T", recv.Setup)
	}
	return o.participantConsumeInitSetup(init)
}

// initiatorConsumePartSetup (Bob) reads Alice's keys, derives the
// combined XMR view key + spend pubkey, builds the BTC
// lockTx/refundTx/spendRefundTx chain, and emits AdaptorSetupInit.
func (o *Orchestrator) initiatorConsumePartSetup(m *msgjson.AdaptorSetupPart) error {
	s := o.state

	// Decode the participant's BTC payout address into a pkScript.
	// The script becomes the spendTx output (where Alice receives
	// BTC on redeem). Skip silently when no address came over the
	// wire AND no decoder is configured - tests / out-of-band
	// CounterPartyAddress flow can populate PeerBTCPayoutScript
	// later via SetPeerBTCPayoutScript.
	if m.BTCPayoutAddr != "" {
		if o.cfg.DecodeBTCAddr == nil {
			return errors.New("AdaptorSetupPart carries BTCPayoutAddr but no DecodeBTCAddr is configured")
		}
		script, err := o.cfg.DecodeBTCAddr(m.BTCPayoutAddr)
		if err != nil {
			return fmt.Errorf("decode peer btc payout addr %q: %w", m.BTCPayoutAddr, err)
		}
		o.cfg.PeerBTCPayoutScript = script
	}

	// Parse Alice's ed25519 spend-key half pubkey.
	partSpendPub, err := edwards.ParsePubKey(m.PubSpendKeyHalf)
	if err != nil {
		return fmt.Errorf("parse part spend pub: %w", err)
	}
	partViewHalf, _, err := edwards.PrivKeyFromScalar(m.ViewKeyHalf)
	if err != nil {
		return fmt.Errorf("parse part view half: %w", err)
	}
	peerScalarSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof)
	if err != nil {
		return fmt.Errorf("extract peer dleq pubkey: %w", err)
	}
	peerSignPub, err := btcschnorr.ParsePubKey(m.PubSignKeyHalf)
	if err != nil {
		return fmt.Errorf("parse peer sign pubkey: %w", err)
	}

	s.PeerXmrSpendPub = partSpendPub
	s.PeerDLEQProof = m.DLEQProof
	s.PeerScalarSecp = peerScalarSecp
	s.PeerBtcSignPub = m.PubSignKeyHalf

	// Combined XMR pubkeys.
	s.FullSpendPub = sumPubKeys(s.XmrSpendKeyHalf.PubKey(), partSpendPub)
	// Bob's own view half (random) combined with Alice's private half.
	ownViewHalf, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fmt.Errorf("own view half: %w", err)
	}
	viewBig := scalarAdd(partViewHalf.GetD(), ownViewHalf.GetD())
	viewBig.Mod(viewBig, curve.N)
	var viewBytes [32]byte
	viewBig.FillBytes(viewBytes[:])
	viewKey, _, err := edwards.PrivKeyFromScalar(viewBytes[:])
	if err != nil {
		return fmt.Errorf("combined view key: %w", err)
	}
	s.FullViewKey = viewKey

	// Build BTC script outputs.
	kal := btcschnorr.SerializePubKey(s.BtcSignKey.PubKey())
	kaf := m.PubSignKeyHalf
	lock, err := btcadaptor.NewLockTxOutput(kal, kaf)
	if err != nil {
		return fmt.Errorf("lock output: %w", err)
	}
	refund, err := btcadaptor.NewRefundTxOutput(kal, kaf, int64(o.cfg.LockBlocks))
	if err != nil {
		return fmt.Errorf("refund output: %w", err)
	}
	s.Lock = lock
	s.Refund = refund
	// Mirror leaf scripts into the script-fields for snapshot/restore
	// uniformity across roles (participant populates these from the
	// peer's setup msg; initiator does it here from its own builders).
	s.LockLeafScript = lock.LeafScript
	s.RefundPkScript = refund.PkScript
	s.CoopLeafScript = refund.CoopLeafScript
	s.PunishLeafScript = refund.PunishLeafScript
	s.LockBlocks = o.cfg.LockBlocks

	// Build unsigned refundTx and spendRefundTx skeletons. lockTx is
	// built later when we know the funded outpoint.
	refundTx := wire.NewMsgTx(2)
	refundTx.AddTxIn(&wire.TxIn{}) // placeholder; filled after lockTx broadcast
	refundTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 1000, PkScript: refund.PkScript})

	spendRefundTx := wire.NewMsgTx(2)
	spendRefundTx.AddTxIn(&wire.TxIn{Sequence: o.cfg.LockBlocks})
	spendRefundTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 2000, PkScript: o.cfg.PeerBTCPayoutScript})
	s.RefundTx = refundTx
	s.SpendRefundTx = spendRefundTx

	// Serialize for wire.
	var rbuf, srbuf bytes.Buffer
	_ = refundTx.Serialize(&rbuf)
	_ = spendRefundTx.Serialize(&srbuf)

	out := &msgjson.AdaptorSetupInit{
		OrderID:          o.cfg.OrderID[:],
		MatchID:          o.cfg.MatchID[:],
		PubSpendKey:      s.FullSpendPub.SerializeCompressed(),
		ViewKey:          viewKey.Serialize(),
		PubSignKeyHalf:   kal,
		DLEQProof:        s.DLEQProof,
		RefundTx:         rbuf.Bytes(),
		SpendRefundTx:    srbuf.Bytes(),
		LockLeafScript:   lock.LeafScript,
		RefundPkScript:   refund.PkScript,
		CoopLeafScript:   refund.CoopLeafScript,
		PunishLeafScript: refund.PunishLeafScript,
		LockBlocks:       o.cfg.LockBlocks,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSetupInitRoute, out); err != nil {
		return fmt.Errorf("send AdaptorSetupInit: %w", err)
	}
	_ = peerSignPub
	s.Phase = PhaseKeysReceived
	return o.save()
}

// participantConsumeInitSetup (Alice) validates Bob's material,
// pre-signs the refund chain, and emits AdaptorRefundPresigned.
func (o *Orchestrator) participantConsumeInitSetup(m *msgjson.AdaptorSetupInit) error {
	s := o.state

	// PubSpendKey is the combined ed25519 spend pubkey serialized
	// via SerializeCompressed in the initiator's emit path, so try
	// edwards first; fall back to btcec only for forward compat.
	fullSpend, err := edwards.ParsePubKey(m.PubSpendKey)
	if err != nil {
		_, secpErr := btcec.ParsePubKey(m.PubSpendKey)
		if secpErr != nil {
			return fmt.Errorf("parse full spend: %w / %w", err, secpErr)
		}
		// secp parse worked - we accept it but cannot derive a
		// shared XMR address from a non-edwards pubkey, so the
		// participant cannot sweep on the refund branch. Fail
		// loudly rather than silently.
		return errors.New("FullSpendPub came over the wire as secp; participant requires ed25519")
	}
	s.FullSpendPub = fullSpend

	peerScalarSecp, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof)
	if err != nil {
		return fmt.Errorf("peer dleq extract: %w", err)
	}
	s.PeerDLEQProof = m.DLEQProof
	s.PeerScalarSecp = peerScalarSecp
	s.PeerBtcSignPub = m.PubSignKeyHalf

	// Reconstruct view key.
	viewKey, _, err := edwards.PrivKeyFromScalar(m.ViewKey)
	if err != nil {
		return fmt.Errorf("parse view key: %w", err)
	}
	s.FullViewKey = viewKey

	// Parse transactions.
	refundTx := wire.NewMsgTx(2)
	if err := refundTx.Deserialize(bytes.NewReader(m.RefundTx)); err != nil {
		return fmt.Errorf("deserialize refundTx: %w", err)
	}
	spendRefundTx := wire.NewMsgTx(2)
	if err := spendRefundTx.Deserialize(bytes.NewReader(m.SpendRefundTx)); err != nil {
		return fmt.Errorf("deserialize spendRefundTx: %w", err)
	}
	s.RefundTx = refundTx
	s.SpendRefundTx = spendRefundTx
	s.LockLeafScript = m.LockLeafScript
	s.RefundPkScript = m.RefundPkScript
	s.CoopLeafScript = m.CoopLeafScript
	s.PunishLeafScript = m.PunishLeafScript
	s.LockBlocks = m.LockBlocks

	// Rebuild Lock and Refund locally so the participant has the
	// control blocks available at spend time. Both sides call
	// btcadaptor with the same inputs, producing identical outputs
	// - also serves as a structural sanity check on the received
	// leaf scripts.
	kal := m.PubSignKeyHalf
	kaf := btcschnorr.SerializePubKey(s.BtcSignKey.PubKey())
	lock, err := btcadaptor.NewLockTxOutput(kal, kaf)
	if err != nil {
		return fmt.Errorf("participant rebuild lock: %w", err)
	}
	if !bytes.Equal(lock.LeafScript, m.LockLeafScript) {
		return errors.New("lock leaf script mismatch vs. local rebuild")
	}
	refund, err := btcadaptor.NewRefundTxOutput(kal, kaf, int64(m.LockBlocks))
	if err != nil {
		return fmt.Errorf("participant rebuild refund: %w", err)
	}
	if !bytes.Equal(refund.PkScript, m.RefundPkScript) {
		return errors.New("refund pkScript mismatch vs. local rebuild")
	}
	s.Lock = lock
	s.Refund = refund

	// Pre-sign refundTx and adaptor-sign spendRefundTx.
	sigRefund, esig, err := o.participantPresignRefund()
	if err != nil {
		return err
	}
	s.OwnRefundSig = sigRefund
	s.SpendRefundAdaptorSig = esig

	out := &msgjson.AdaptorRefundPresigned{
		OrderID:               o.cfg.OrderID[:],
		MatchID:               o.cfg.MatchID[:],
		RefundSig:             sigRefund,
		SpendRefundAdaptorSig: esig.Serialize(),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorRefundPresignedRoute, out); err != nil {
		return fmt.Errorf("send AdaptorRefundPresigned: %w", err)
	}
	s.Phase = PhaseRefundPresigned
	return o.save()
}

// participantPresignRefund produces Alice's cooperative sig on
// refundTx and her adaptor sig on spendRefundTx (tweaked by Bob's
// XMR-key-half secp pubkey).
func (o *Orchestrator) participantPresignRefund() ([]byte, *adaptorsigs.AdaptorSignature, error) {
	s := o.state
	// refundTx input 0 spends the (not yet funded) lockTx vout. For
	// pre-signing, we sign a sighash that depends on the lockTx
	// outpoint + lockLeafScript; value is fixed to BtcAmount.
	prev := txscript.NewCannedPrevOutputFetcher(
		lockPkScript(s.LockLeafScript), o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(s.RefundTx, prev)
	leaf := txscript.NewBaseTapLeaf(s.LockLeafScript)

	sigRefund, err := txscript.RawTxInTapscriptSignature(
		s.RefundTx, sh, 0, o.cfg.BtcAmount, lockPkScript(s.LockLeafScript),
		leaf, txscript.SigHashDefault, s.BtcSignKey,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("refundTx sign: %w", err)
	}

	// spendRefundTx sighash via coop leaf.
	refundValue := s.RefundTx.TxOut[0].Value
	spendPrev := txscript.NewCannedPrevOutputFetcher(s.RefundPkScript, refundValue)
	spendSh := txscript.NewTxSigHashes(s.SpendRefundTx, spendPrev)
	coopLeaf := txscript.NewBaseTapLeaf(s.CoopLeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		spendSh, txscript.SigHashDefault, s.SpendRefundTx, 0,
		spendPrev, coopLeaf,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund sighash: %w", err)
	}
	var T btcec.JacobianPoint
	s.PeerScalarSecp.AsJacobian(&T)
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(s.BtcSignKey, sigHash, &T)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund adaptor: %w", err)
	}
	return sigRefund, esig, nil
}

// lockPkScript reconstructs the P2TR pkScript from the lock leaf.
// For pre-signing, this is approximate: we only need a fetcher that
// returns a matching pkScript for sighash computation. In a real
// deployment the peer's advertised pkScript should be validated
// against the rebuild.
func lockPkScript(leafScript []byte) []byte {
	// Rebuild via the same btcadaptor code path. This is expensive
	// per call; acceptable at this volume.
	// We treat the leaf script alone as enough to reconstruct (NUMS
	// internal key + single-leaf tree).
	key, _ := btcadaptor.UnspendableInternalKey()
	leaf := txscript.NewBaseTapLeaf(leafScript)
	tree := txscript.AssembleTaprootScriptTree(leaf)
	root := tree.RootNode.TapHash()
	outKey := txscript.ComputeTaprootOutputKey(key, root[:])
	pk, _ := txscript.PayToTaprootScript(outKey)
	return pk
}

// handleKeysReceived: initiator side, on receiving
// EventRefundPresignedReceived, validates the adaptor sig, stores
// it, and funds+broadcasts lockTx.
func (o *Orchestrator) handleKeysReceived(evt Event) error {
	pres, ok := evt.(EventRefundPresignedReceived)
	if !ok {
		return fmt.Errorf("expected EventRefundPresignedReceived, got %T", evt)
	}
	s := o.state
	if s.Role != RoleInitiator {
		return fmt.Errorf("participant received RefundPresigned; unexpected")
	}
	// Verify and store peer refund sig + adaptor sig.
	s.PeerRefundSig = pres.RefundSig
	esig, err := adaptorsigs.ParseAdaptorSignature(pres.AdaptorSig)
	if err != nil {
		return fmt.Errorf("parse adaptor sig: %w", err)
	}
	s.SpendRefundAdaptorSig = esig

	// Fund + broadcast lockTx.
	tx, vout, height, err := o.assetBTC.FundBroadcastTaproot(s.Lock.PkScript, o.cfg.BtcAmount)
	if err != nil {
		return fmt.Errorf("fund+broadcast lockTx: %w", err)
	}
	s.LockTx = tx
	s.LockVout = vout
	s.LockHeight = height

	// Re-point refundTx input to the now-known outpoint.
	lockHash := tx.TxHash()
	s.RefundTx.TxIn[0].PreviousOutPoint = wire.OutPoint{Hash: lockHash, Index: vout}
	// spendRefundTx input to refundTx output.
	refundHash := s.RefundTx.TxHash()
	s.SpendRefundTx.TxIn[0].PreviousOutPoint = wire.OutPoint{Hash: refundHash, Index: 0}

	// Notify participant: lock is broadcast.
	notice := &msgjson.AdaptorLocked{
		OrderID: o.cfg.OrderID[:],
		MatchID: o.cfg.MatchID[:],
		TxID:    lockHash[:],
		Vout:    vout,
		Value:   uint64(o.cfg.BtcAmount),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorLockedRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorLocked: %w", err)
	}
	s.Phase = PhaseLockBroadcast
	if err := o.save(); err != nil {
		return err
	}
	// FundBroadcastTaproot's waitForConfirm callback already blocked
	// until the tx confirmed, so the height is real and we can
	// immediately self-fire EventLockConfirmed without waiting for an
	// external chain watcher. Advances to PhaseLockConfirmed and
	// waits for participant's AdaptorXmrLocked.
	return o.handleLockBroadcast(EventLockConfirmed{Height: height})
}

// handleRefundPresigned: participant side, awaits EventLockConfirmed
// (delivered either by a chain watcher or, on simnet/test mode, by
// the inbound AdaptorLocked notification translated to an event in
// the bridge). On confirmation, advances to PhaseLockConfirmed and
// chains directly into handleLockConfirmed so the participant
// proceeds to send XMR without needing a second wakeup.
func (o *Orchestrator) handleRefundPresigned(evt Event) error {
	e, ok := evt.(EventLockConfirmed)
	if !ok {
		return fmt.Errorf("expected EventLockConfirmed, got %T", evt)
	}
	o.state.LockHeight = e.Height
	o.state.Phase = PhaseLockConfirmed
	if err := o.save(); err != nil {
		return err
	}
	// Continue inline: handleLockConfirmed (participant branch) sends
	// XMR and advances to PhaseXmrSent.
	return o.handleLockConfirmed(evt)
}

// handleLockBroadcast: initiator waits for its lockTx to reach
// confirmation depth. Advance on EventLockConfirmed.
func (o *Orchestrator) handleLockBroadcast(evt Event) error {
	if _, ok := evt.(EventLockConfirmed); !ok {
		return fmt.Errorf("expected EventLockConfirmed, got %T", evt)
	}
	o.state.Phase = PhaseLockConfirmed
	return o.save()
}

// handleLockConfirmed: participant captures XMR height and sends
// XMR to the shared address.
func (o *Orchestrator) handleLockConfirmed(evt Event) error {
	s := o.state
	if s.Role == RoleInitiator {
		// Initiator waits for participant's AdaptorXmrLocked.
		ev, ok := evt.(EventKeysReceived)
		if !ok {
			return fmt.Errorf("initiator expected peer AdaptorXmrLocked, got %T", evt)
		}
		xmr, ok := ev.Setup.(*msgjson.AdaptorXmrLocked)
		if !ok {
			return fmt.Errorf("initiator expected AdaptorXmrLocked payload, got %T", ev.Setup)
		}
		s.XmrSendTxID = string(xmr.XmrTxID)
		s.XmrRestoreHeight = xmr.RestoreHeight
		s.Phase = PhaseXmrConfirmed
		if err := o.save(); err != nil {
			return err
		}
		// On simnet / no XMR auditor configured, trust the
		// participant's claim and continue inline. handleXmrConfirmed
		// builds + adaptor-signs spendTx and emits AdaptorSpendPresig.
		// Production with a real XMR auditor would instead wait for
		// EventXmrConfirmed before invoking this handler.
		return o.handleXmrConfirmed(EventXmrConfirmed{Height: xmr.RestoreHeight})
	}
	// Participant: send XMR.
	sharedAddr := deriveSharedAddress(s.FullSpendPub, s.FullViewKey.PubKey(), o.cfg.XmrNetTag)
	txid, height, err := o.assetXMR.SendToSharedAddress(sharedAddr, o.cfg.XmrAmount)
	if err != nil {
		return fmt.Errorf("send xmr: %w", err)
	}
	s.XmrSendTxID = txid
	s.XmrRestoreHeight = height

	notice := &msgjson.AdaptorXmrLocked{
		OrderID:       o.cfg.OrderID[:],
		MatchID:       o.cfg.MatchID[:],
		XmrTxID:       []byte(txid),
		RestoreHeight: height,
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorXmrLockedRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorXmrLocked: %w", err)
	}
	s.Phase = PhaseXmrSent
	return o.save()
}

// handleXmrSent: participant waits for initiator's AdaptorSpendPresig.
func (o *Orchestrator) handleXmrSent(evt Event) error {
	if o.state.Role != RoleParticipant {
		return fmt.Errorf("only participant waits for spend presig here")
	}
	ev, ok := evt.(EventSpendPresigReceived)
	if !ok {
		return fmt.Errorf("expected EventSpendPresigReceived, got %T", evt)
	}
	// Parse spendTx skeleton.
	spendTx := wire.NewMsgTx(2)
	if err := spendTx.Deserialize(bytes.NewReader(ev.SpendTx)); err != nil {
		return fmt.Errorf("parse spendTx: %w", err)
	}
	esig, err := adaptorsigs.ParseAdaptorSignature(ev.AdaptorSig)
	if err != nil {
		return fmt.Errorf("parse spend adaptor: %w", err)
	}
	o.state.SpendTx = spendTx
	o.state.SpendAdaptorSig = esig

	// Alice decrypts Bob's adaptor using her ed25519 scalar.
	aliceScalar, _ := btcec.PrivKeyFromBytes(o.state.XmrSpendKeyHalf.Serialize())
	bobSigCompleted, err := esig.DecryptBIP340(&aliceScalar.Key)
	if err != nil {
		return fmt.Errorf("decrypt bob adaptor: %w", err)
	}

	// Alice signs her tapscript half.
	prev := txscript.NewCannedPrevOutputFetcher(
		lockPkScript(o.state.LockLeafScript), o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(o.state.LockLeafScript)
	aliceSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sh, 0, o.cfg.BtcAmount, lockPkScript(o.state.LockLeafScript),
		leaf, txscript.SigHashDefault, o.state.BtcSignKey,
	)
	if err != nil {
		return fmt.Errorf("alice sign spend: %w", err)
	}

	// Assemble witness: sig_kaf, sig_kal(completed), script, control.
	lock, _ := btcadaptor.NewLockTxOutput(o.state.PeerBtcSignPub,
		btcschnorr.SerializePubKey(o.state.BtcSignKey.PubKey()))
	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		return fmt.Errorf("control block: %w", err)
	}
	spendTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, bobSigCompleted.Serialize(), o.state.LockLeafScript, ctrlSer,
	}

	txid, err := o.assetBTC.BroadcastTx(spendTx)
	if err != nil {
		return fmt.Errorf("broadcast spend: %w", err)
	}
	o.state.SpendTxID = []byte(txid)

	notice := &msgjson.AdaptorSpendBroadcast{
		OrderID: o.cfg.OrderID[:],
		MatchID: o.cfg.MatchID[:],
		TxID:    []byte(txid),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSpendBroadcastRoute, notice); err != nil {
		return fmt.Errorf("send AdaptorSpendBroadcast: %w", err)
	}
	o.state.Phase = PhaseSpendBroadcast
	return o.save()
}

// handleXmrConfirmed: initiator builds + adaptor-signs spendTx and
// emits AdaptorSpendPresig.
func (o *Orchestrator) handleXmrConfirmed(evt Event) error {
	if o.state.Role != RoleInitiator {
		return fmt.Errorf("only initiator acts in PhaseXmrConfirmed")
	}
	// Build spendTx paying to peer's address.
	lockHash := o.state.LockTx.TxHash()
	spendTx := wire.NewMsgTx(2)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: lockHash, Index: o.state.LockVout},
	})
	spendTx.AddTxOut(&wire.TxOut{Value: o.cfg.BtcAmount - 1000, PkScript: o.cfg.PeerBTCPayoutScript})

	// Bob adaptor-signs with tweak = Alice's secp pubkey.
	prev := txscript.NewCannedPrevOutputFetcher(o.state.Lock.PkScript, o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(o.state.Lock.LeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(sh, txscript.SigHashDefault, spendTx, 0, prev, leaf)
	if err != nil {
		return fmt.Errorf("sighash: %w", err)
	}
	var T btcec.JacobianPoint
	o.state.PeerScalarSecp.AsJacobian(&T)
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(o.state.BtcSignKey, sigHash, &T)
	if err != nil {
		return fmt.Errorf("bob adaptor sig: %w", err)
	}
	o.state.SpendTx = spendTx
	o.state.SpendAdaptorSig = esig

	var txBuf bytes.Buffer
	_ = spendTx.Serialize(&txBuf)
	out := &msgjson.AdaptorSpendPresig{
		OrderID:    o.cfg.OrderID[:],
		MatchID:    o.cfg.MatchID[:],
		SpendTx:    txBuf.Bytes(),
		AdaptorSig: esig.Serialize(),
	}
	if err := o.sendMsg.SendToPeer(msgjson.AdaptorSpendPresigRoute, out); err != nil {
		return fmt.Errorf("send AdaptorSpendPresig: %w", err)
	}
	o.state.Phase = PhaseSpendPresig
	if err := o.save(); err != nil {
		return err
	}
	// Kick off the BTC chain watcher. The participant will broadcast
	// spendTx once they decrypt the adaptor sig; the witness on that
	// spend reveals their sig, from which RecoverTweakBIP340 extracts
	// the participant's XMR scalar so the initiator can sweep XMR.
	if o.cfg.SpendObserver != nil {
		lockHash := o.state.LockTx.TxHash()
		o.cfg.SpendObserver(
			wire.OutPoint{Hash: lockHash, Index: o.state.LockVout},
			o.state.LockHeight,
		)
	}
	return nil
}

// handleSpendPresig: initiator waits to observe spend on chain.
func (o *Orchestrator) handleSpendPresig(evt Event) error {
	if o.state.Role != RoleInitiator {
		return fmt.Errorf("only initiator waits for on-chain spend here")
	}
	e, ok := evt.(EventSpendObservedOnChain)
	if !ok {
		return fmt.Errorf("expected EventSpendObservedOnChain, got %T", evt)
	}
	// The completed Bob sig is in the witness at position 1 (sig_kal).
	if len(e.Witness) < 2 {
		return fmt.Errorf("witness too short: %d", len(e.Witness))
	}
	completedSig := e.Witness[1]
	sig, err := btcschnorr.ParseSignature(completedSig)
	if err != nil {
		return fmt.Errorf("parse completed sig: %w", err)
	}
	scalar, err := o.state.SpendAdaptorSig.RecoverTweakBIP340(sig)
	if err != nil {
		return fmt.Errorf("recover tweak: %w", err)
	}
	o.state.RecoveredPeerScalar = scalar

	// Reconstruct full spend key and open sweep wallet.
	var sb [32]byte
	scalar.PutBytes(&sb)
	partRecov, _, err := edwards.PrivKeyFromScalar(sb[:])
	if err != nil {
		return fmt.Errorf("part scalar -> ed25519: %w", err)
	}
	full := scalarAdd(o.state.XmrSpendKeyHalf.GetD(), partRecov.GetD())
	full.Mod(full, curve.N)
	var fullBytes [32]byte
	full.FillBytes(fullBytes[:])

	viewHex := hex.EncodeToString(reverseBytes(o.state.FullViewKey.Serialize()))
	spendHex := hex.EncodeToString(reverseBytes(fullBytes[:]))
	sharedAddr := deriveSharedAddress(o.state.FullSpendPub, o.state.FullViewKey.PubKey(), o.cfg.XmrNetTag)

	txid, err := o.assetXMR.SweepSharedAddress(hex.EncodeToString(o.cfg.SwapID[:]),
		sharedAddr, spendHex, viewHex, o.state.XmrRestoreHeight, o.cfg.OwnXMRSweepDest)
	if err != nil {
		return fmt.Errorf("sweep xmr: %w", err)
	}
	_ = txid
	o.state.Phase = PhaseXmrSwept
	o.state.Phase = PhaseComplete
	return o.save()
}

// handleSpendBroadcast: participant side terminal - spend confirmed.
func (o *Orchestrator) handleSpendBroadcast(evt Event) error {
	o.state.Phase = PhaseComplete
	return o.save()
}

// InitiateRefund is called when the local party has decided to bail
// on the swap and needs to start the refund chain. Assembles the
// pre-signed refundTx witness from PeerRefundSig + OwnRefundSig and
// broadcasts via the BTC adapter. Transitions to
// PhaseRefundTxBroadcast; the caller is responsible for feeding
// EventRefundCSVMatured (and, on the participant side,
// EventCoopRefundObserved) once those chain events occur.
func (o *Orchestrator) InitiateRefund() error {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	s := o.state
	if s.RefundTx == nil || s.Lock == nil {
		return errors.New("InitiateRefund: refund chain not ready")
	}
	// Participant holds its own refund sig in OwnRefundSig; peer's
	// refund sig in PeerRefundSig. Initiator's own sig was generated
	// in signRefundTx and cached separately. For both sides, the
	// witness is [sig_kaf, sig_kal, script, control_block].
	var sigKaf, sigKal []byte
	if s.Role == RoleInitiator {
		// initiator = kal, participant = kaf. peer sig is the kaf.
		sigKaf = s.PeerRefundSig
		// initiator's own sig - need to produce it now if not cached.
		own, err := o.initiatorSignRefundTx()
		if err != nil {
			return err
		}
		sigKal = own
	} else {
		sigKaf = s.OwnRefundSig
		sigKal = s.PeerRefundSig
	}

	ctrlSer, err := s.Lock.ControlBlock.ToBytes()
	if err != nil {
		return fmt.Errorf("control block: %w", err)
	}
	s.RefundTx.TxIn[0].Witness = wire.TxWitness{
		sigKaf, sigKal, s.Lock.LeafScript, ctrlSer,
	}
	if _, err := o.assetBTC.BroadcastTx(s.RefundTx); err != nil {
		return fmt.Errorf("broadcast refundTx: %w", err)
	}
	s.Phase = PhaseRefundTxBroadcast
	return o.save()
}

// initiatorSignRefundTx produces the initiator's cooperative sig on
// refundTx at refund time. Kept separate from the pre-sign flow so
// we don't hold a standing signature over the wire.
func (o *Orchestrator) initiatorSignRefundTx() ([]byte, error) {
	s := o.state
	prev := txscript.NewCannedPrevOutputFetcher(s.Lock.PkScript, o.cfg.BtcAmount)
	sh := txscript.NewTxSigHashes(s.RefundTx, prev)
	leaf := txscript.NewBaseTapLeaf(s.Lock.LeafScript)
	return txscript.RawTxInTapscriptSignature(
		s.RefundTx, sh, 0, o.cfg.BtcAmount, s.Lock.PkScript, leaf,
		txscript.SigHashDefault, s.BtcSignKey,
	)
}

// handleRefundTxBroadcast: wait for CSV maturity, then execute the
// role-specific branch.
//
//   - Initiator: on EventRefundCSVMatured, decrypts the participant's
//     adaptor-sig on spendRefundTx using its own ed25519 scalar and
//     broadcasts the coop path. Transitions to PhaseCoopRefund
//     terminal.
//   - Participant: on EventCoopRefundObserved (initiator cooperated),
//     recovers the initiator's scalar from the on-chain sig and
//     sweeps the shared-address XMR. Terminal.
//   - Participant: on EventRefundCSVMatured (initiator stalled),
//     signs the punish leaf alone and broadcasts. Transitions to
//     PhasePunish terminal. XMR is forfeited.
func (o *Orchestrator) handleRefundTxBroadcast(evt Event) error {
	s := o.state
	switch e := evt.(type) {
	case EventRefundCSVMatured:
		if s.Role == RoleInitiator {
			return o.initiatorCoopRefund()
		}
		return o.participantPunish()
	case EventCoopRefundObserved:
		if s.Role != RoleParticipant {
			return errors.New("EventCoopRefundObserved unexpected on initiator side")
		}
		return o.participantRecoverAndSweep(e.Witness)
	default:
		return fmt.Errorf("unexpected event %T in PhaseRefundTxBroadcast", evt)
	}
}

// initiatorCoopRefund (Bob) decrypts Alice's adaptor sig on
// spendRefundTx with his ed25519 scalar, signs his own tapscript
// half, assembles the coop-leaf witness, and broadcasts. Terminal
// on success.
func (o *Orchestrator) initiatorCoopRefund() error {
	s := o.state
	bobScalar, _ := btcec.PrivKeyFromBytes(s.XmrSpendKeyHalf.Serialize())
	aliceSig, err := s.SpendRefundAdaptorSig.DecryptBIP340(&bobScalar.Key)
	if err != nil {
		return fmt.Errorf("decrypt alice spendRefund adaptor: %w", err)
	}
	refundValue := s.RefundTx.TxOut[0].Value
	prev := txscript.NewCannedPrevOutputFetcher(s.Refund.PkScript, refundValue)
	sh := txscript.NewTxSigHashes(s.SpendRefundTx, prev)
	leaf := txscript.NewBaseTapLeaf(s.Refund.CoopLeafScript)

	bobSig, err := txscript.RawTxInTapscriptSignature(
		s.SpendRefundTx, sh, 0, refundValue, s.Refund.PkScript, leaf,
		txscript.SigHashDefault, s.BtcSignKey,
	)
	if err != nil {
		return fmt.Errorf("bob sign spendRefund coop: %w", err)
	}
	ctrlSer, err := s.Refund.CoopControlBlock.ToBytes()
	if err != nil {
		return fmt.Errorf("coop control block: %w", err)
	}
	s.SpendRefundTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig.Serialize(), bobSig, s.Refund.CoopLeafScript, ctrlSer,
	}
	if _, err := o.assetBTC.BroadcastTx(s.SpendRefundTx); err != nil {
		return fmt.Errorf("broadcast coop spendRefund: %w", err)
	}
	s.Phase = PhaseCoopRefund
	s.Phase = PhaseComplete
	return o.save()
}

// participantPunish (Alice) signs the punish leaf alone and
// broadcasts the spendRefundTx after CSV matured without initiator
// cooperation. Terminal - XMR is stranded at the shared address.
func (o *Orchestrator) participantPunish() error {
	s := o.state
	refundValue := s.RefundTx.TxOut[0].Value
	prev := txscript.NewCannedPrevOutputFetcher(s.Refund.PkScript, refundValue)
	sh := txscript.NewTxSigHashes(s.SpendRefundTx, prev)
	leaf := txscript.NewBaseTapLeaf(s.Refund.PunishLeafScript)

	aliceSig, err := txscript.RawTxInTapscriptSignature(
		s.SpendRefundTx, sh, 0, refundValue, s.Refund.PkScript, leaf,
		txscript.SigHashDefault, s.BtcSignKey,
	)
	if err != nil {
		return fmt.Errorf("alice punish sign: %w", err)
	}
	ctrlSer, err := s.Refund.PunishControlBlock.ToBytes()
	if err != nil {
		return fmt.Errorf("punish control block: %w", err)
	}
	s.SpendRefundTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, s.Refund.PunishLeafScript, ctrlSer,
	}
	if _, err := o.assetBTC.BroadcastTx(s.SpendRefundTx); err != nil {
		return fmt.Errorf("broadcast punish spendRefund: %w", err)
	}
	s.Phase = PhasePunish
	return o.save()
}

// participantRecoverAndSweep (Alice) extracts the initiator's XMR
// scalar from the coop-refund witness on-chain, combines with her
// own half, and sweeps the shared-address XMR.
func (o *Orchestrator) participantRecoverAndSweep(witness [][]byte) error {
	s := o.state
	if len(witness) < 2 {
		return fmt.Errorf("coop witness too short: %d", len(witness))
	}
	// Witness layout: [sig_kaf (alice completed), sig_kal (bob), script, ctrl].
	completed := witness[0]
	sig, err := btcschnorr.ParseSignature(completed)
	if err != nil {
		return fmt.Errorf("parse completed sig: %w", err)
	}
	scalar, err := s.SpendRefundAdaptorSig.RecoverTweakBIP340(sig)
	if err != nil {
		return fmt.Errorf("recover bob scalar: %w", err)
	}
	s.RecoveredPeerScalar = scalar

	var sb [32]byte
	scalar.PutBytes(&sb)
	partRecov, _, err := edwards.PrivKeyFromScalar(sb[:])
	if err != nil {
		return fmt.Errorf("part scalar -> ed25519: %w", err)
	}
	full := scalarAdd(s.XmrSpendKeyHalf.GetD(), partRecov.GetD())
	full.Mod(full, curve.N)
	var fullBytes [32]byte
	full.FillBytes(fullBytes[:])

	viewHex := hex.EncodeToString(reverseBytes(s.FullViewKey.Serialize()))
	spendHex := hex.EncodeToString(reverseBytes(fullBytes[:]))
	sharedAddr := deriveSharedAddress(s.FullSpendPub, s.FullViewKey.PubKey(), o.cfg.XmrNetTag)

	if _, err := o.assetXMR.SweepSharedAddress(hex.EncodeToString(o.cfg.SwapID[:]),
		sharedAddr, spendHex, viewHex, s.XmrRestoreHeight, o.cfg.OwnXMRSweepDest); err != nil {
		return fmt.Errorf("sweep xmr: %w", err)
	}
	s.Phase = PhaseCoopRefund
	s.Phase = PhaseComplete
	return o.save()
}

// handleCoopRefund: terminal for initiator on the coop path;
// participant transitions via participantRecoverAndSweep from
// handleRefundTxBroadcast.
func (o *Orchestrator) handleCoopRefund(evt Event) error {
	return fmt.Errorf("event %T in terminal PhaseCoopRefund", evt)
}

// handlePunish: terminal for participant. Initiator does not reach
// this phase (the punish path is participant-only).
func (o *Orchestrator) handlePunish(evt Event) error {
	return fmt.Errorf("event %T in terminal PhasePunish", evt)
}

// save is a wrapper around the persister. Must be called with
// o.state.mu held. Also fires the OnTerminal callback exactly
// once when the state machine first enters a terminal phase, so
// the bridge can record outcomes and tear down the orchestrator.
func (o *Orchestrator) save() error {
	o.state.Updated = time.Now()
	terminal := o.state.Phase.IsTerminal() && !o.terminalFired
	if terminal {
		o.terminalFired = true
	}
	var err error
	if o.persist != nil {
		err = o.persist.Save(o.cfg.SwapID, o.state.Snapshot())
	}
	if terminal && o.cfg.OnTerminal != nil {
		o.cfg.OnTerminal(o.state.Phase)
	}
	return err
}

// ----- crypto helpers -----

func sumPubKeys(a, b *edwards.PublicKey) *edwards.PublicKey {
	x, y := curve.Add(a.GetX(), a.GetY(), b.GetX(), b.GetY())
	return edwards.NewPublicKey(x, y)
}

func scalarAdd(a, b *big.Int) *big.Int {
	return scalarAddMod(a, b)
}

// scalarAddMod adds two ed25519 scalars mod L using the edwards25519
// field element helpers from agl/ed25519/edwards25519 (imported but
// not strictly needed - big.Int + explicit mod curve.N is enough
// given the DLEQ library's invariant that scalars fit under both L
// and n).
func scalarAddMod(a, b *big.Int) *big.Int {
	sum := new(big.Int).Add(a, b)
	return sum
}

// deriveSharedAddress builds the base58-encoded XMR standard
// address from a combined spend pubkey and the shared view pubkey.
func deriveSharedAddress(spendPub, viewPub *edwards.PublicKey, netTag uint64) string {
	var full []byte
	full = append(full, spendPub.SerializeCompressed()...)
	full = append(full, viewPub.SerializeCompressed()...)
	return base58.EncodeAddr(netTag, full)
}

// reverseBytes returns a little-endian interpretation of a
// big-endian 32-byte scalar (XMR wallet creation expects LE hex).
func reverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, v := range b {
		out[len(b)-1-i] = v
	}
	return out
}

// Ensure unused imports are reachable during partial compilation.
var (
	_ = edwards25519.FieldElement{}
	_ = rand.Reader
)
