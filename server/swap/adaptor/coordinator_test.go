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

// TestCoordinatorHappyPathTerminal exercises the final on-chain
// spend observation: PhaseAwaitingSpendBroadcast + EventSpendOnChain
// should report OutcomeSuccess for both roles.
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
