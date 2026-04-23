package adaptor

import (
	"sync"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// ---- in-memory mocks ----

type recordingRouter struct {
	mu   sync.Mutex
	sent []routedMsg
}

type routedMsg struct {
	match   order.MatchID
	role    Role
	route   string
	payload any
}

func (r *recordingRouter) SendTo(m order.MatchID, role Role, route string, payload any) error {
	r.mu.Lock()
	r.sent = append(r.sent, routedMsg{m, role, route, payload})
	r.mu.Unlock()
	return nil
}

func (r *recordingRouter) last() routedMsg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent[len(r.sent)-1]
}

type outcomeRecord struct {
	match   order.MatchID
	role    Role
	outcome Outcome
}

type recordingReporter struct {
	mu       sync.Mutex
	outcomes []outcomeRecord
}

func (r *recordingReporter) Report(m order.MatchID, role Role, o Outcome) error {
	r.mu.Lock()
	r.outcomes = append(r.outcomes, outcomeRecord{m, role, o})
	r.mu.Unlock()
	return nil
}

type nopPersister struct{}

func (*nopPersister) Save(order.MatchID, *State) error   { return nil }
func (*nopPersister) Load(order.MatchID) (*State, error) { return nil, nil }

// ---- helpers ----

// buildPartSetup creates a well-formed AdaptorSetupPart with real
// key material that the coordinator's validation accepts.
func buildPartSetup(t *testing.T, matchID [32]byte) (*msgjson.AdaptorSetupPart, *btcec.PrivateKey) {
	t.Helper()
	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	return &msgjson.AdaptorSetupPart{
		OrderID:         matchID[:],
		MatchID:         matchID[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}, btcKey
}

// buildInitSetup creates a well-formed AdaptorSetupInit that uses
// the participant's btc signing pubkey from partSetup and matches
// the lock/refund leaf scripts our validator will rebuild.
func buildInitSetup(t *testing.T, matchID [32]byte, partBtcPub []byte, lockBlocks uint32) *msgjson.AdaptorSetupInit {
	t.Helper()
	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	kal := btcschnorr.SerializePubKey(btcKey.PubKey())
	coop, err := btcadaptor.LockLeafScript(kal, partBtcPub)
	if err != nil {
		t.Fatalf("coop: %v", err)
	}
	punish, err := btcadaptor.PunishLeafScript(partBtcPub, int64(lockBlocks))
	if err != nil {
		t.Fatalf("punish: %v", err)
	}
	// Minimal well-formed refundTx and spendRefundTx.
	refundTx := wire.NewMsgTx(2)
	refundTx.AddTxIn(&wire.TxIn{})
	refundTx.AddTxOut(&wire.TxOut{Value: 99_000, PkScript: []byte{0x51}})
	spendRefundTx := wire.NewMsgTx(2)
	spendRefundTx.AddTxIn(&wire.TxIn{Sequence: lockBlocks})
	spendRefundTx.AddTxOut(&wire.TxOut{Value: 98_000, PkScript: []byte{0x51}})
	var rbuf, srbuf bytesBuffer
	_ = refundTx.Serialize(&rbuf)
	_ = spendRefundTx.Serialize(&srbuf)
	return &msgjson.AdaptorSetupInit{
		OrderID:          matchID[:],
		MatchID:          matchID[:],
		PubSpendKey:      spend.PubKey().SerializeCompressed(),
		ViewKey:          view.Serialize(),
		PubSignKeyHalf:   kal,
		DLEQProof:        dleq,
		RefundTx:         rbuf.Bytes(),
		SpendRefundTx:    srbuf.Bytes(),
		LockLeafScript:   coop,
		RefundPkScript:   []byte{0x51},
		CoopLeafScript:   coop,
		PunishLeafScript: punish,
		LockBlocks:       lockBlocks,
	}
}

// bytesBuffer is a minimal io.Writer backed by an append slice, used
// to keep the test file free of extra imports.
type bytesBuffer struct{ b []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) { b.b = append(b.b, p...); return len(p), nil }
func (b *bytesBuffer) Bytes() []byte               { return b.b }

// ---- tests ----

// TestCoordinatorSetupForwards exercises the happy-path setup
// phase: PartSetup -> InitSetup -> Presigned, verifying each
// message is routed to the correct peer role and phases advance.
func TestCoordinatorSetupForwards(t *testing.T) {
	router := &recordingRouter{}
	reporter := &recordingReporter{}
	matchID := [32]byte{0xAB}
	orderID := [32]byte{0xCD}
	cfg := &Config{
		MatchID:         matchID,
		OrderID:         orderID,
		ScriptableAsset: 0,
		NonScriptAsset:  128,
		LockBlocks:      2,
		Router:          router,
		Report:          reporter,
		Persist:         &nopPersister{},
	}
	c, err := NewCoordinator(cfg)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	if c.Phase() != PhaseAwaitingPartSetup {
		t.Fatalf("initial phase=%s want PhaseAwaitingPartSetup", c.Phase())
	}

	// Part setup from participant.
	part, partBtc := buildPartSetup(t, matchID)
	if err := c.Handle(EventPartSetup{Msg: part}); err != nil {
		t.Fatalf("handle part: %v", err)
	}
	if c.Phase() != PhaseAwaitingInitSetup {
		t.Fatalf("after part phase=%s want PhaseAwaitingInitSetup", c.Phase())
	}
	if got := router.last().role; got != RoleInitiator {
		t.Fatalf("part forwarded to role %d, want RoleInitiator", got)
	}

	// Init setup from initiator.
	init := buildInitSetup(t, matchID, btcschnorr.SerializePubKey(partBtc.PubKey()), cfg.LockBlocks)
	if err := c.Handle(EventInitSetup{Msg: init}); err != nil {
		t.Fatalf("handle init: %v", err)
	}
	if c.Phase() != PhaseAwaitingPresigned {
		t.Fatalf("after init phase=%s want PhaseAwaitingPresigned", c.Phase())
	}
	if got := router.last().role; got != RoleParticipant {
		t.Fatalf("init forwarded to role %d, want RoleParticipant", got)
	}

	// Presigned from participant - use a real-but-arbitrary adaptor sig.
	priv, _ := btcec.NewPrivateKey()
	tweak, _ := btcec.NewPrivateKey()
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&tweak.Key, &T)
	var hash [32]byte
	fakeSig, _ := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(priv, hash[:], &T)
	pres := &msgjson.AdaptorRefundPresigned{
		OrderID:               matchID[:],
		MatchID:               matchID[:],
		RefundSig:             make([]byte, 64),
		SpendRefundAdaptorSig: fakeSig.Serialize(),
	}
	if err := c.Handle(EventPresigned{Msg: pres}); err != nil {
		t.Fatalf("handle presigned: %v", err)
	}
	if c.Phase() != PhaseAwaitingLocked {
		t.Fatalf("after presigned phase=%s want PhaseAwaitingLocked", c.Phase())
	}
	if got := router.last().role; got != RoleInitiator {
		t.Fatalf("presigned forwarded to role %d, want RoleInitiator", got)
	}
}

// TestCoordinatorTimeoutMapsOutcome confirms that EventTimeout in
// an awaiting phase produces the correct Outcome for the blamed
// party.
func TestCoordinatorTimeoutMapsOutcome(t *testing.T) {
	tests := []struct {
		name  string
		phase Phase
		want  Outcome
	}{
		{"locked", PhaseAwaitingLocked, OutcomeInitiatorBailed},
		{"xmrLocked", PhaseAwaitingXmrLocked, OutcomeParticipantBailed},
		{"spendPresig", PhaseAwaitingSpendPresig, OutcomePunishedInitiator},
		{"spendBroadcast", PhaseAwaitingSpendBroadcast, OutcomePunishedInitiator},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := &recordingRouter{}
			reporter := &recordingReporter{}
			cfg := &Config{
				MatchID: [32]byte{1}, OrderID: [32]byte{2},
				Router: router, Report: reporter, Persist: &nopPersister{},
			}
			c, _ := NewCoordinator(cfg)
			c.state.Phase = tc.phase
			if err := c.Handle(EventTimeout{Reason: "test"}); err != nil {
				t.Fatalf("handle timeout: %v", err)
			}
			if c.Phase() != PhaseFailed {
				t.Fatalf("phase=%s want PhaseFailed", c.Phase())
			}
			if c.FailOutcome() != tc.want {
				t.Fatalf("outcome=%d want %d", c.FailOutcome(), tc.want)
			}
		})
	}
}

// TestStateMarshalRoundtrip serializes a populated State, parses it
// back, and verifies field-by-field equality. Server-side state is
// public-only so plain JSON round-trip is sufficient.
func TestStateMarshalRoundtrip(t *testing.T) {
	s := &State{
		MatchID:                    [32]byte{0xAA},
		OrderID:                    [32]byte{0xBB},
		ScriptableAsset:            0,
		NonScriptAsset:             128,
		LockBlocks:                 144,
		ParticipantPubSpendKeyHalf: []byte{0x01, 0x02, 0x03},
		ParticipantPubSignKeyHalf:  []byte{0x04, 0x05},
		ParticipantDLEQProof:       []byte{0x06, 0x07, 0x08, 0x09},
		InitiatorPubSpendKeyHalf:   nil, // initiator's ed25519 half embedded in FullSpendPub
		InitiatorPubSignKeyHalf:    []byte{0x0A, 0x0B},
		InitiatorDLEQProof:         []byte{0x0C, 0x0D},
		FullSpendPub:               []byte{0x10, 0x11, 0x12, 0x13},
		FullViewKey:                []byte{0x14, 0x15, 0x16, 0x17},
		LockTxID:                   []byte{0x20, 0x21, 0x22, 0x23},
		LockVout:                   3,
		LockValue:                  1_000_000,
		LockHeight:                 815000,
		XmrTxID:                    []byte{0x30, 0x31, 0x32},
		XmrSentHeight:              3000000,
		Phase:                      PhaseAwaitingSpendPresig,
		FailOutcome:                OutcomeSuccess,
	}
	data, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.MatchID != s.MatchID || got.OrderID != s.OrderID {
		t.Error("identity mismatch")
	}
	if got.LockBlocks != s.LockBlocks || got.LockHeight != s.LockHeight ||
		got.LockValue != s.LockValue || got.LockVout != s.LockVout {
		t.Errorf("lock fields mismatch: blocks %d/%d height %d/%d value %d/%d vout %d/%d",
			got.LockBlocks, s.LockBlocks, got.LockHeight, s.LockHeight,
			got.LockValue, s.LockValue, got.LockVout, s.LockVout)
	}
	if got.Phase != s.Phase {
		t.Errorf("phase: got %s want %s", got.Phase, s.Phase)
	}
	if string(got.ParticipantDLEQProof) != string(s.ParticipantDLEQProof) ||
		string(got.InitiatorDLEQProof) != string(s.InitiatorDLEQProof) {
		t.Error("DLEQ proofs mismatch")
	}
	if string(got.FullSpendPub) != string(s.FullSpendPub) ||
		string(got.FullViewKey) != string(s.FullViewKey) {
		t.Error("XMR keys mismatch")
	}
}

// TestServerFilePersisterRoundtrip writes and reads a state via
// the file persister. Also exercises the missing-snapshot case.
func TestServerFilePersisterRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p, err := NewFilePersister(dir)
	if err != nil {
		t.Fatalf("NewFilePersister: %v", err)
	}
	var matchID order.MatchID
	copy(matchID[:], []byte{0xEE, 0xFF})
	s := &State{
		MatchID:    matchID,
		Phase:      PhaseAwaitingLocked,
		LockHeight: 1234,
		LockValue:  100_000,
	}
	if err := p.Save(matchID, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := p.Load(matchID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("nil state")
	}
	if got.Phase != s.Phase || got.LockHeight != s.LockHeight {
		t.Errorf("mismatch: phase %s/%s height %d/%d",
			got.Phase, s.Phase, got.LockHeight, s.LockHeight)
	}

	var missing order.MatchID
	copy(missing[:], []byte{0x99})
	if got, err := p.Load(missing); err != nil {
		t.Fatalf("Load missing: %v", err)
	} else if got != nil {
		t.Error("missing should return nil")
	}
}

// TestCoordinatorHappyPathTerminal exercises the final on-chain
// spend observation: PhaseAwaitingSpendBroadcast + EventSpendOnChain
// should report OutcomeSuccess for both roles.
// TestCoordinatorLockPhaseAdvances validates the chain-event side
// of the coordinator: EventLocked relays the BTC lock notice to the
// participant without advancing phase, EventLockConfirmed advances
// to PhaseAwaitingXmrLocked, EventXmrLocked relays the XMR notice
// to the initiator and (with no XMR auditor wired) auto-advances to
// PhaseAwaitingSpendPresig.
func TestCoordinatorLockPhaseAdvances(t *testing.T) {
	router := &recordingRouter{}
	cfg := &Config{
		MatchID: [32]byte{0x55}, OrderID: [32]byte{0x66},
		Router: router, Persist: &nopPersister{},
	}
	c, err := NewCoordinator(cfg)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	// Skip ahead to the start of the lock phase.
	c.state.Phase = PhaseAwaitingLocked

	// EventLocked: relays AdaptorLockedRoute to participant. With no
	// BTC auditor wired (cfg.BTC == nil), the coordinator trust-skips
	// the audit and auto-advances to PhaseAwaitingXmrLocked, mirroring
	// the no-XMR-auditor branch below.
	lockTxID := []byte{1, 2, 3, 4}
	if err := c.Handle(EventLocked{Msg: &msgjson.AdaptorLocked{
		MatchID: cfg.MatchID[:], TxID: lockTxID, Vout: 0, Value: 100_000_000,
	}}); err != nil {
		t.Fatalf("EventLocked: %v", err)
	}
	if got := router.last(); got.role != RoleParticipant || got.route != msgjson.AdaptorLockedRoute {
		t.Fatalf("EventLocked routed to role=%d route=%s, want RoleParticipant/AdaptorLockedRoute", got.role, got.route)
	}
	if got := c.Phase(); got != PhaseAwaitingXmrLocked {
		t.Fatalf("after EventLocked phase=%s, want PhaseAwaitingXmrLocked (trust-skip with no auditor)", got)
	}

	// EventXmrLocked: relays AdaptorXmrLockedRoute to initiator. No
	// XMR auditor is wired (cfg.XMR == nil), so the coordinator
	// trust-skips the audit and auto-advances.
	if err := c.Handle(EventXmrLocked{Msg: &msgjson.AdaptorXmrLocked{
		MatchID: cfg.MatchID[:], XmrTxID: []byte("xmrtxid"), RestoreHeight: 1234,
	}}); err != nil {
		t.Fatalf("EventXmrLocked: %v", err)
	}
	if got := router.last(); got.role != RoleInitiator || got.route != msgjson.AdaptorXmrLockedRoute {
		t.Fatalf("EventXmrLocked routed to role=%d route=%s, want RoleInitiator/AdaptorXmrLockedRoute", got.role, got.route)
	}
	if got := c.Phase(); got != PhaseAwaitingSpendPresig {
		t.Fatalf("after EventXmrLocked (no auditor) phase=%s, want PhaseAwaitingSpendPresig", got)
	}
}

// TestCoordinatorOutOfPhaseEventErrors confirms that delivering an
// event for a phase other than the current one is rejected without
// changing state, and a valid event for the current phase still
// works after the rejection (no permanent corruption).
func TestCoordinatorOutOfPhaseEventErrors(t *testing.T) {
	router := &recordingRouter{}
	cfg := &Config{
		MatchID: [32]byte{0x77}, OrderID: [32]byte{0x88},
		Router: router, Persist: &nopPersister{},
	}
	c, err := NewCoordinator(cfg)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	// Coordinator is in PhaseAwaitingPartSetup; deliver an
	// EventLocked instead. The handler should reject it.
	if err := c.Handle(EventLocked{Msg: &msgjson.AdaptorLocked{
		MatchID: cfg.MatchID[:], TxID: []byte{0xAA},
	}}); err == nil {
		t.Fatal("expected error for EventLocked in PhaseAwaitingPartSetup")
	}
	if got := c.Phase(); got != PhaseAwaitingPartSetup {
		t.Fatalf("phase changed after rejected event: got %s, want PhaseAwaitingPartSetup", got)
	}
	// A valid in-phase event then proceeds normally.
	part, _ := buildPartSetup(t, cfg.MatchID)
	if err := c.Handle(EventPartSetup{Msg: part}); err != nil {
		t.Fatalf("subsequent valid EventPartSetup: %v", err)
	}
	if got := c.Phase(); got != PhaseAwaitingInitSetup {
		t.Fatalf("phase=%s, want PhaseAwaitingInitSetup", got)
	}
}

// TestCoordinatorRejectsMalformedSetup walks the validators that
// gate the setup phase and confirms each malformation drives the
// coordinator into PhaseFailed with OutcomeProtocolError. Exercises
// validatePartSetup (length checks, bad DLEQ), validateInitSetup
// (mismatched leaf scripts), and validatePresigned (wrong-length
// refund sig).
func TestCoordinatorRejectsMalformedSetup(t *testing.T) {
	matchID := [32]byte{0x99}

	// (1) PartSetup with wrong-length PubSpendKeyHalf.
	t.Run("part-bad-spend-pub-len", func(t *testing.T) {
		c, _ := NewCoordinator(&Config{
			MatchID: matchID, OrderID: matchID,
			Router: &recordingRouter{}, Report: &recordingReporter{}, Persist: &nopPersister{},
		})
		bad, _ := buildPartSetup(t, matchID)
		bad.PubSpendKeyHalf = []byte{1, 2, 3}  // wrong length
		_ = c.Handle(EventPartSetup{Msg: bad}) // fail() returns nil by design
		if got := c.Phase(); got != PhaseFailed {
			t.Fatalf("phase=%s, want PhaseFailed", got)
		}
		if got := c.FailOutcome(); got != OutcomeProtocolError {
			t.Fatalf("outcome=%d, want OutcomeProtocolError", got)
		}
	})

	// (2) PartSetup with empty DLEQ proof.
	t.Run("part-empty-dleq", func(t *testing.T) {
		c, _ := NewCoordinator(&Config{
			MatchID: matchID, OrderID: matchID,
			Router: &recordingRouter{}, Report: &recordingReporter{}, Persist: &nopPersister{},
		})
		bad, _ := buildPartSetup(t, matchID)
		bad.DLEQProof = nil
		_ = c.Handle(EventPartSetup{Msg: bad})
		if c.Phase() != PhaseFailed {
			t.Fatalf("phase=%s, want PhaseFailed", c.Phase())
		}
	})

	// (3) PartSetup with garbage DLEQ proof that fails extraction.
	t.Run("part-bad-dleq-bytes", func(t *testing.T) {
		c, _ := NewCoordinator(&Config{
			MatchID: matchID, OrderID: matchID,
			Router: &recordingRouter{}, Report: &recordingReporter{}, Persist: &nopPersister{},
		})
		bad, _ := buildPartSetup(t, matchID)
		bad.DLEQProof = []byte("not a real dleq proof")
		_ = c.Handle(EventPartSetup{Msg: bad})
		if c.Phase() != PhaseFailed {
			t.Fatalf("phase=%s, want PhaseFailed", c.Phase())
		}
	})

	// (4) InitSetup with a coop leaf script that doesn't match what
	// the coordinator rebuilds from the advertised pubkeys.
	t.Run("init-mismatched-coop-script", func(t *testing.T) {
		c, _ := NewCoordinator(&Config{
			MatchID: matchID, OrderID: matchID, LockBlocks: 144,
			Router: &recordingRouter{}, Report: &recordingReporter{}, Persist: &nopPersister{},
		})
		// Run the participant's part-setup so the coordinator stores
		// ParticipantPubSignKeyHalf, which the init-setup validator
		// uses to rebuild the leaf scripts.
		part, partBtc := buildPartSetup(t, matchID)
		if err := c.Handle(EventPartSetup{Msg: part}); err != nil {
			t.Fatalf("setup precondition: %v", err)
		}
		bad := buildInitSetup(t, matchID, btcschnorr.SerializePubKey(partBtc.PubKey()), 144)
		bad.CoopLeafScript = []byte{0xDE, 0xAD, 0xBE, 0xEF}
		_ = c.Handle(EventInitSetup{Msg: bad})
		if c.Phase() != PhaseFailed {
			t.Fatalf("phase=%s, want PhaseFailed", c.Phase())
		}
	})

	// (5) Presigned with wrong-length refund sig.
	t.Run("presigned-bad-refund-sig-len", func(t *testing.T) {
		c, _ := NewCoordinator(&Config{
			MatchID: matchID, OrderID: matchID,
			Router: &recordingRouter{}, Report: &recordingReporter{}, Persist: &nopPersister{},
		})
		c.state.Phase = PhaseAwaitingPresigned // skip ahead
		bad := &msgjson.AdaptorRefundPresigned{
			OrderID:               matchID[:],
			MatchID:               matchID[:],
			RefundSig:             []byte{1, 2, 3}, // not 64
			SpendRefundAdaptorSig: make([]byte, 97),
		}
		_ = c.Handle(EventPresigned{Msg: bad})
		if c.Phase() != PhaseFailed {
			t.Fatalf("phase=%s, want PhaseFailed", c.Phase())
		}
	})
}

// TestCoordinatorResumeFromSnapshot mirrors the client-side
// TestResumeFromSnapshot for the server coordinator: drive a
// coordinator through the setup phase, snapshot, restart with a
// fresh config, and verify the resumed coordinator continues the
// state machine from the saved phase.
func TestCoordinatorResumeFromSnapshot(t *testing.T) {
	router1 := &recordingRouter{}
	matchID := [32]byte{0xCA}
	cfg := &Config{
		MatchID: matchID, OrderID: matchID, LockBlocks: 144,
		Router: router1, Report: &recordingReporter{}, Persist: &nopPersister{},
	}
	c, err := NewCoordinator(cfg)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}

	// Drive through setup so the saved State has substantive
	// content (peer pubkeys, dleq proofs, leaf scripts).
	part, partBtc := buildPartSetup(t, matchID)
	if err := c.Handle(EventPartSetup{Msg: part}); err != nil {
		t.Fatalf("part: %v", err)
	}
	init := buildInitSetup(t, matchID, btcschnorr.SerializePubKey(partBtc.PubKey()), cfg.LockBlocks)
	if err := c.Handle(EventInitSetup{Msg: init}); err != nil {
		t.Fatalf("init: %v", err)
	}
	savedPhase := c.Phase()
	if savedPhase != PhaseAwaitingPresigned {
		t.Fatalf("setup phase = %s, want PhaseAwaitingPresigned", savedPhase)
	}

	// Snapshot -> bytes -> Unmarshal.
	c.state.mu.Lock()
	data, err := c.state.Marshal()
	c.state.mu.Unlock()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	restored, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Simulate a process restart: fresh router (a real restart
	// gets a new comms layer) and a new Coordinator over the
	// restored state.
	router2 := &recordingRouter{}
	resumeCfg := &Config{
		MatchID: cfg.MatchID, OrderID: cfg.OrderID, LockBlocks: cfg.LockBlocks,
		Router: router2, Report: &recordingReporter{}, Persist: &nopPersister{},
	}
	resumed, err := NewCoordinatorFromState(resumeCfg, restored)
	if err != nil {
		t.Fatalf("NewCoordinatorFromState: %v", err)
	}
	if got := resumed.Phase(); got != savedPhase {
		t.Fatalf("resumed phase = %s, want %s", got, savedPhase)
	}

	// Resume must continue the state machine. Feed the next valid
	// event (Presigned) and verify it advances + routes to the
	// initiator on the new router.
	priv, _ := btcec.NewPrivateKey()
	tweak, _ := btcec.NewPrivateKey()
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&tweak.Key, &T)
	var hash [32]byte
	fakeSig, _ := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(priv, hash[:], &T)
	pres := &msgjson.AdaptorRefundPresigned{
		OrderID: matchID[:], MatchID: matchID[:],
		RefundSig: make([]byte, 64), SpendRefundAdaptorSig: fakeSig.Serialize(),
	}
	if err := resumed.Handle(EventPresigned{Msg: pres}); err != nil {
		t.Fatalf("resumed Handle Presigned: %v", err)
	}
	if got := resumed.Phase(); got != PhaseAwaitingLocked {
		t.Errorf("after resumed Presigned phase = %s, want PhaseAwaitingLocked", got)
	}
	if got := router2.last(); got.role != RoleInitiator || got.route != msgjson.AdaptorRefundPresignedRoute {
		t.Errorf("resumed routing role=%d route=%s, want RoleInitiator/AdaptorRefundPresignedRoute",
			got.role, got.route)
	}
	// The original router must NOT have received the post-resume
	// message - confirms cfg.Router is the new one.
	router1.mu.Lock()
	last := router1.sent[len(router1.sent)-1]
	router1.mu.Unlock()
	if last.route == msgjson.AdaptorRefundPresignedRoute {
		t.Error("original router got the post-resume message; resume Cfg's Router not in use")
	}
}

func TestCoordinatorHappyPathTerminal(t *testing.T) {
	router := &recordingRouter{}
	reporter := &recordingReporter{}
	cfg := &Config{
		MatchID: [32]byte{1}, OrderID: [32]byte{2},
		Router: router, Report: reporter, Persist: &nopPersister{},
	}
	c, _ := NewCoordinator(cfg)
	c.state.Phase = PhaseAwaitingSpendBroadcast

	if err := c.Handle(EventSpendOnChain{TxID: []byte{1, 2, 3}}); err != nil {
		t.Fatalf("handle spend on chain: %v", err)
	}
	if c.Phase() != PhaseComplete {
		t.Fatalf("phase=%s want PhaseComplete", c.Phase())
	}
	if c.FailOutcome() != OutcomeSuccess {
		t.Fatalf("outcome=%d want OutcomeSuccess", c.FailOutcome())
	}
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if len(reporter.outcomes) != 2 {
		t.Fatalf("got %d outcome reports, want 2", len(reporter.outcomes))
	}
}
