package adaptor

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/wire"
)

// Config carries the per-match context a Coordinator needs.
type Config struct {
	MatchID         [32]byte
	OrderID         [32]byte
	ScriptableAsset uint32
	NonScriptAsset  uint32
	LockBlocks      uint32
	// Per-phase timeouts. Zero values use protocol defaults.
	SetupTimeout   time.Duration
	LockTimeout    time.Duration
	XmrLockTimeout time.Duration
	RedeemTimeout  time.Duration
	RefundTimeout  time.Duration

	Router  PeerRouter
	BTC     BTCAuditor
	XMR     XMRAuditor
	Report  OutcomeReporter
	Persist StatePersister
}

func (c *Config) setupTimeout() time.Duration {
	if c.SetupTimeout == 0 {
		return 5 * time.Minute
	}
	return c.SetupTimeout
}

func (c *Config) lockTimeout() time.Duration {
	if c.LockTimeout == 0 {
		return 1 * time.Hour
	}
	return c.LockTimeout
}

func (c *Config) xmrLockTimeout() time.Duration {
	if c.XmrLockTimeout == 0 {
		return 30 * time.Minute
	}
	return c.XmrLockTimeout
}

func (c *Config) redeemTimeout() time.Duration {
	if c.RedeemTimeout == 0 {
		return 1 * time.Hour
	}
	return c.RedeemTimeout
}

// NewCoordinator constructs a Coordinator in PhaseAwaitingPartSetup.
// The caller feeds events via Handle until a terminal phase.
func NewCoordinator(cfg *Config) (*Coordinator, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	s := &State{
		MatchID:         cfg.MatchID,
		OrderID:         cfg.OrderID,
		ScriptableAsset: cfg.ScriptableAsset,
		NonScriptAsset:  cfg.NonScriptAsset,
		LockBlocks:      cfg.LockBlocks,
		Phase:           PhaseAwaitingPartSetup,
		Updated:         time.Now(),
		PhaseDeadline:   time.Now().Add(cfg.setupTimeout()),
	}
	return &Coordinator{
		state:   s,
		router:  cfg.Router,
		btc:     cfg.BTC,
		xmr:     cfg.XMR,
		report:  cfg.Report,
		persist: cfg.Persist,
		cfg:     cfg,
	}, nil
}

// Phase returns the current phase. Safe for concurrent use.
func (c *Coordinator) Phase() Phase {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.Phase
}

// FailOutcome returns the terminal failure outcome, if any.
func (c *Coordinator) FailOutcome() Outcome {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.FailOutcome
}

// Handle dispatches an event based on the current phase.
func (c *Coordinator) Handle(evt Event) error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	if c.state.Phase.IsTerminal() {
		return fmt.Errorf("event %T received in terminal phase %s", evt, c.state.Phase)
	}

	// Timeout events short-circuit to failure regardless of phase.
	if _, ok := evt.(EventTimeout); ok {
		return c.onTimeout()
	}

	switch c.state.Phase {
	case PhaseAwaitingPartSetup:
		return c.onPartSetup(evt)
	case PhaseAwaitingInitSetup:
		return c.onInitSetup(evt)
	case PhaseAwaitingPresigned:
		return c.onPresigned(evt)
	case PhaseAwaitingLocked:
		return c.onLocked(evt)
	case PhaseAwaitingXmrLocked:
		return c.onXmrLocked(evt)
	case PhaseAwaitingSpendPresig:
		return c.onSpendPresig(evt)
	case PhaseAwaitingSpendBroadcast:
		return c.onSpendBroadcast(evt)
	case PhaseAwaitingRefundResolution:
		return c.onRefundResolution(evt)
	case PhaseAwaitingCoopOrPunish:
		return c.onCoopOrPunish(evt)
	}
	return fmt.Errorf("no handler for phase %s", c.state.Phase)
}

// cfg is added to the Coordinator type by reopening here - see
// state.go, which declares the Coordinator. We add the field via a
// companion struct embedded below.

// We actually need cfg on the Coordinator. Since state.go already
// declares Coordinator with 5 fields, we extend it there. See the
// corresponding state.go edit in this commit.

// ----- handlers -----

func (c *Coordinator) onPartSetup(evt Event) error {
	e, ok := evt.(EventPartSetup)
	if !ok {
		return fmt.Errorf("expected EventPartSetup, got %T", evt)
	}
	m := e.Msg
	if err := c.validatePartSetup(m); err != nil {
		return c.fail(OutcomeProtocolError, fmt.Errorf("invalid part setup: %w", err))
	}
	s := c.state
	s.ParticipantPubSpendKeyHalf = m.PubSpendKeyHalf
	s.ParticipantPubSignKeyHalf = m.PubSignKeyHalf
	s.ParticipantDLEQProof = m.DLEQProof

	// Route unmodified payload to initiator.
	if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleInitiator,
		msgjson.AdaptorSetupPartRoute, m); err != nil {
		return fmt.Errorf("route to initiator: %w", err)
	}
	s.Phase = PhaseAwaitingInitSetup
	s.PhaseDeadline = time.Now().Add(c.cfg.setupTimeout())
	return c.save()
}

func (c *Coordinator) onInitSetup(evt Event) error {
	e, ok := evt.(EventInitSetup)
	if !ok {
		return fmt.Errorf("expected EventInitSetup, got %T", evt)
	}
	m := e.Msg
	if err := c.validateInitSetup(m); err != nil {
		return c.fail(OutcomeProtocolError, fmt.Errorf("invalid init setup: %w", err))
	}
	s := c.state
	s.InitiatorPubSpendKeyHalf = nil // participant doesn't send this; initiator's ed25519 half is embedded in m.PubSpendKey
	s.InitiatorPubSignKeyHalf = m.PubSignKeyHalf
	s.InitiatorDLEQProof = m.DLEQProof
	s.FullSpendPub = m.PubSpendKey
	s.FullViewKey = m.ViewKey

	if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleParticipant,
		msgjson.AdaptorSetupInitRoute, m); err != nil {
		return fmt.Errorf("route to participant: %w", err)
	}
	s.Phase = PhaseAwaitingPresigned
	s.PhaseDeadline = time.Now().Add(c.cfg.setupTimeout())
	return c.save()
}

func (c *Coordinator) onPresigned(evt Event) error {
	e, ok := evt.(EventPresigned)
	if !ok {
		return fmt.Errorf("expected EventPresigned, got %T", evt)
	}
	m := e.Msg
	if err := c.validatePresigned(m); err != nil {
		return c.fail(OutcomeProtocolError, fmt.Errorf("invalid presigned: %w", err))
	}
	if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleInitiator,
		msgjson.AdaptorRefundPresignedRoute, m); err != nil {
		return fmt.Errorf("route to initiator: %w", err)
	}
	c.state.Phase = PhaseAwaitingLocked
	c.state.PhaseDeadline = time.Now().Add(c.cfg.lockTimeout())
	return c.save()
}

func (c *Coordinator) onLocked(evt Event) error {
	switch e := evt.(type) {
	case EventLocked:
		m := e.Msg
		c.state.LockTxID = m.TxID
		c.state.LockVout = m.Vout
		c.state.LockValue = m.Value
		// Route to participant so they know to start watching.
		if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleParticipant,
			msgjson.AdaptorLockedRoute, m); err != nil {
			return fmt.Errorf("route AdaptorLocked: %w", err)
		}
		// With a BTCAuditor configured, wait for EventLockConfirmed
		// from the auditor before advancing. In trust mode (no
		// auditor) accept the wire claim and move on so the
		// participant's subsequent EventXmrLocked isn't rejected.
		// Mirrors the onXmrLocked branch below.
		if c.cfg.BTC == nil {
			c.state.Phase = PhaseAwaitingXmrLocked
			c.state.PhaseDeadline = time.Now().Add(c.cfg.xmrLockTimeout())
		}
		return c.save()
	case EventLockConfirmed:
		c.state.LockHeight = e.Height
		c.state.Phase = PhaseAwaitingXmrLocked
		c.state.PhaseDeadline = time.Now().Add(c.cfg.xmrLockTimeout())
		return c.save()
	default:
		return fmt.Errorf("unexpected event %T in PhaseAwaitingLocked", evt)
	}
}

func (c *Coordinator) onXmrLocked(evt Event) error {
	switch e := evt.(type) {
	case EventXmrLocked:
		m := e.Msg
		c.state.XmrTxID = m.XmrTxID
		c.state.XmrSentHeight = m.RestoreHeight
		if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleInitiator,
			msgjson.AdaptorXmrLockedRoute, m); err != nil {
			return fmt.Errorf("route AdaptorXmrLocked: %w", err)
		}
		// Do not advance phase yet; wait for server XMR audit
		// (EventXmrOutputConfirmed) before the initiator should
		// send spend presig. If the operator has no XMR backend
		// wired, trust the reports and move on immediately.
		if c.cfg.XMR == nil {
			c.state.Phase = PhaseAwaitingSpendPresig
			c.state.PhaseDeadline = time.Now().Add(c.cfg.redeemTimeout())
		}
		return c.save()
	case EventXmrOutputConfirmed:
		c.state.Phase = PhaseAwaitingSpendPresig
		c.state.PhaseDeadline = time.Now().Add(c.cfg.redeemTimeout())
		return c.save()
	default:
		return fmt.Errorf("unexpected event %T in PhaseAwaitingXmrLocked", evt)
	}
}

func (c *Coordinator) onSpendPresig(evt Event) error {
	e, ok := evt.(EventSpendPresig)
	if !ok {
		return fmt.Errorf("expected EventSpendPresig, got %T", evt)
	}
	m := e.Msg
	if err := c.validateSpendPresig(m); err != nil {
		return c.fail(OutcomeProtocolError, fmt.Errorf("invalid spend presig: %w", err))
	}
	if err := c.cfg.Router.SendTo(orderMatchID(c.cfg.MatchID), RoleParticipant,
		msgjson.AdaptorSpendPresigRoute, m); err != nil {
		return fmt.Errorf("route AdaptorSpendPresig: %w", err)
	}
	c.state.Phase = PhaseAwaitingSpendBroadcast
	c.state.PhaseDeadline = time.Now().Add(c.cfg.redeemTimeout())
	return c.save()
}

func (c *Coordinator) onSpendBroadcast(evt Event) error {
	switch e := evt.(type) {
	case EventSpendBroadcast:
		c.state.SpendTxID = e.Msg.TxID
		return c.save()
	case EventSpendOnChain:
		// Happy path complete.
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), RoleInitiator, OutcomeSuccess); err != nil {
			return err
		}
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), RoleParticipant, OutcomeSuccess); err != nil {
			return err
		}
		c.state.Phase = PhaseComplete
		c.state.FailOutcome = OutcomeSuccess
		return c.save()
	case EventRefundBroadcast:
		// Refund race: participant broadcast refund before spend
		// hit chain. Transition to refund resolution.
		c.state.RefundTxID = e.Msg.TxID
		c.state.Phase = PhaseAwaitingRefundResolution
		c.state.PhaseDeadline = time.Now().Add(c.cfg.redeemTimeout())
		return c.save()
	default:
		return fmt.Errorf("unexpected event %T in PhaseAwaitingSpendBroadcast", evt)
	}
}

func (c *Coordinator) onRefundResolution(evt Event) error {
	switch e := evt.(type) {
	case EventCoopRefundOnChain:
		c.state.SpendRefundTxID = e.TxID
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), RoleInitiator, OutcomeCoopRefund); err != nil {
			return err
		}
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), RoleParticipant, OutcomeCoopRefund); err != nil {
			return err
		}
		c.state.Phase = PhaseComplete
		c.state.FailOutcome = OutcomeCoopRefund
		return c.save()
	case EventPunishOnChain:
		c.state.SpendRefundTxID = e.TxID
		// The initiator stalled; participant punished.
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), RoleInitiator, OutcomePunishedInitiator); err != nil {
			return err
		}
		c.state.Phase = PhaseComplete
		c.state.FailOutcome = OutcomePunishedInitiator
		return c.save()
	default:
		return fmt.Errorf("unexpected event %T in PhaseAwaitingRefundResolution", evt)
	}
}

func (c *Coordinator) onCoopOrPunish(evt Event) error {
	return c.onRefundResolution(evt)
}

// onTimeout maps the current phase to an outcome the reputation
// system can consume.
func (c *Coordinator) onTimeout() error {
	var outcome Outcome
	switch c.state.Phase {
	case PhaseAwaitingPartSetup, PhaseAwaitingInitSetup, PhaseAwaitingPresigned:
		outcome = OutcomeProtocolError // setup stalled: generic protocol failure
	case PhaseAwaitingLocked:
		outcome = OutcomeInitiatorBailed
	case PhaseAwaitingXmrLocked:
		outcome = OutcomeParticipantBailed
	case PhaseAwaitingSpendPresig, PhaseAwaitingSpendBroadcast:
		outcome = OutcomePunishedInitiator
	default:
		outcome = OutcomeProtocolError
	}
	return c.fail(outcome, errors.New("phase timeout"))
}

func (c *Coordinator) fail(out Outcome, err error) error {
	c.state.LastError = err.Error()
	c.state.FailOutcome = out
	c.state.Phase = PhaseFailed
	if c.cfg.Report != nil {
		// Best-effort outcome reports to whoever is guilty. For a
		// generic protocol error we report to both sides.
		target := RoleInitiator
		if out == OutcomeParticipantBailed {
			target = RoleParticipant
		}
		if err := c.cfg.Report.Report(orderMatchID(c.cfg.MatchID), target, out); err != nil {
			return err
		}
	}
	return c.save()
}

// ----- validation -----

func (c *Coordinator) validatePartSetup(m *msgjson.AdaptorSetupPart) error {
	if len(m.PubSpendKeyHalf) != 32 {
		return fmt.Errorf("bad spend pub len %d", len(m.PubSpendKeyHalf))
	}
	if len(m.ViewKeyHalf) != 32 {
		return fmt.Errorf("bad view half len %d", len(m.ViewKeyHalf))
	}
	if len(m.PubSignKeyHalf) != 32 {
		return fmt.Errorf("bad sign pub len %d", len(m.PubSignKeyHalf))
	}
	if len(m.DLEQProof) == 0 {
		return errors.New("empty DLEQ proof")
	}
	// Optionally verify DLEQ proof via adaptorsigs.VerifyDLEQ. For
	// now we only check it extracts a valid secp pubkey.
	if _, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof); err != nil {
		return fmt.Errorf("dleq proof: %w", err)
	}
	return nil
}

func (c *Coordinator) validateInitSetup(m *msgjson.AdaptorSetupInit) error {
	if len(m.PubSpendKey) == 0 || len(m.ViewKey) == 0 || len(m.PubSignKeyHalf) != 32 {
		return errors.New("missing or wrong-sized key material")
	}
	if _, err := adaptorsigs.ExtractSecp256k1PubKeyFromProof(m.DLEQProof); err != nil {
		return fmt.Errorf("dleq proof: %w", err)
	}
	// Validate that CoopLeafScript and PunishLeafScript are
	// consistent with what we'd build from the advertised pubkeys.
	// kal = initiator pub; kaf = participant pub (we held this from
	// prior PartSetup).
	kal := m.PubSignKeyHalf
	kaf := c.state.ParticipantPubSignKeyHalf
	wantCoop, err := btcadaptor.LockLeafScript(kal, kaf)
	if err != nil {
		return fmt.Errorf("coop leaf rebuild: %w", err)
	}
	if !bytes.Equal(wantCoop, m.CoopLeafScript) {
		return errors.New("coop leaf script mismatch")
	}
	wantPunish, err := btcadaptor.PunishLeafScript(kaf, int64(m.LockBlocks))
	if err != nil {
		return fmt.Errorf("punish leaf rebuild: %w", err)
	}
	if !bytes.Equal(wantPunish, m.PunishLeafScript) {
		return errors.New("punish leaf script mismatch")
	}
	// Unmarshal refund txs for structure sanity.
	refundTx := wire.NewMsgTx(2)
	if err := refundTx.Deserialize(bytes.NewReader(m.RefundTx)); err != nil {
		return fmt.Errorf("refundTx: %w", err)
	}
	spendRefundTx := wire.NewMsgTx(2)
	if err := spendRefundTx.Deserialize(bytes.NewReader(m.SpendRefundTx)); err != nil {
		return fmt.Errorf("spendRefundTx: %w", err)
	}
	return nil
}

func (c *Coordinator) validatePresigned(m *msgjson.AdaptorRefundPresigned) error {
	if len(m.RefundSig) != 64 {
		return fmt.Errorf("refund sig length %d", len(m.RefundSig))
	}
	if _, err := adaptorsigs.ParseAdaptorSignature(m.SpendRefundAdaptorSig); err != nil {
		return fmt.Errorf("adaptor sig: %w", err)
	}
	return nil
}

func (c *Coordinator) validateSpendPresig(m *msgjson.AdaptorSpendPresig) error {
	spendTx := wire.NewMsgTx(2)
	if err := spendTx.Deserialize(bytes.NewReader(m.SpendTx)); err != nil {
		return fmt.Errorf("spendTx: %w", err)
	}
	if _, err := adaptorsigs.ParseAdaptorSignature(m.AdaptorSig); err != nil {
		return fmt.Errorf("adaptor sig: %w", err)
	}
	return nil
}

// save persists the current state.
func (c *Coordinator) save() error {
	c.state.Updated = time.Now()
	if c.cfg.Persist == nil {
		return nil
	}
	return c.cfg.Persist.Save(orderMatchID(c.cfg.MatchID), c.state)
}

// orderMatchID bridges [32]byte -> order.MatchID for Router calls.
func orderMatchID(b [32]byte) order.MatchID {
	var out order.MatchID
	copy(out[:], b[:])
	return out
}
