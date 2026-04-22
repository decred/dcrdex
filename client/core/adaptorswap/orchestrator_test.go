package adaptorswap

import (
	"fmt"
	"sync"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/internal/adaptorsigs"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// ---- in-memory mocks ----

type recordingSender struct {
	mu   sync.Mutex
	sent []sentMsg
}

type sentMsg struct {
	route   string
	payload any
}

func (r *recordingSender) SendToPeer(route string, payload any) error {
	r.mu.Lock()
	r.sent = append(r.sent, sentMsg{route, payload})
	r.mu.Unlock()
	return nil
}

func (r *recordingSender) last() sentMsg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent[len(r.sent)-1]
}

type fakeBTC struct {
	value      int64
	fakeTx     *wire.MsgTx
	height     int64
	spendTx    *wire.MsgTx
	witness    [][]byte
	broadcasts []string
}

func (f *fakeBTC) FundBroadcastTaproot(pkScript []byte, value int64) (*wire.MsgTx, uint32, int64, error) {
	tx := wire.NewMsgTx(2)
	tx.AddTxIn(&wire.TxIn{})
	tx.AddTxOut(&wire.TxOut{Value: value, PkScript: pkScript})
	f.fakeTx = tx
	f.height = 100
	return tx, 0, 100, nil
}

func (f *fakeBTC) ObserveSpend(outpoint wire.OutPoint, startHeight int64) ([][]byte, error) {
	return f.witness, nil
}

func (f *fakeBTC) BroadcastTx(tx *wire.MsgTx) (string, error) {
	h := tx.TxHash()
	f.broadcasts = append(f.broadcasts, h.String())
	f.spendTx = tx
	return h.String(), nil
}

func (f *fakeBTC) CurrentHeight() (int64, error) {
	return f.height, nil
}

type fakeXMR struct {
	sends []xmrSend
	swept string
}

type xmrSend struct {
	addr   string
	amount uint64
}

func (f *fakeXMR) SendToSharedAddress(addr string, amount uint64) (string, uint64, error) {
	f.sends = append(f.sends, xmrSend{addr, amount})
	return "fake_xmr_txid", 500000, nil
}

func (f *fakeXMR) WatchSharedAddress(swapID, addr, viewKey string, rh, amt uint64) (XMRWatch, error) {
	return &stubWatch{}, nil
}

func (f *fakeXMR) SweepSharedAddress(swapID, addr, sk, vk string, rh uint64, dest string) (string, error) {
	f.swept = dest
	return "fake_sweep_txid", nil
}

type stubWatch struct{}

func (*stubWatch) Synced() bool                  { return true }
func (*stubWatch) HasFunds() (bool, bool, error) { return true, true, nil }
func (*stubWatch) Close() error                  { return nil }

type memPersister struct{}

func (*memPersister) Save([32]byte, *Snapshot) error   { return nil }
func (*memPersister) Load([32]byte) (*Snapshot, error) { return nil, nil }

// ---- helpers ----

// fakePeerSetup generates a participant's AdaptorSetupPart with real
// key material so it passes DLEQ extraction on the receiving side.
func fakePeerSetup(t *testing.T, orderID, matchID [32]byte) (*msgjson.AdaptorSetupPart, *edwards.PrivateKey, *btcec.PrivateKey, *edwards.PrivateKey) {
	t.Helper()
	spend, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("spend key: %v", err)
	}
	view, err := edwards.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("view key: %v", err)
	}
	btcKey, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("btc key: %v", err)
	}
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	m := &msgjson.AdaptorSetupPart{
		OrderID:         orderID[:],
		MatchID:         matchID[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}
	return m, spend, btcKey, view
}

// ---- tests ----

// TestInitiatorHappyPathThroughLockBroadcast drives a fresh
// initiator orchestrator from PhaseInit to PhaseLockBroadcast,
// exercising the two most code-heavy handlers:
// initiatorConsumePartSetup (builds lockTx/refundTx chain) and
// handleKeysReceived (funds+broadcasts lockTx).
func TestInitiatorHappyPathThroughLockBroadcast(t *testing.T) {
	msgr := &recordingSender{}
	btc := &fakeBTC{}
	xmr := &fakeXMR{}
	persist := &memPersister{}

	orderID := [32]byte{1}
	matchID := [32]byte{2}
	swapID := [32]byte{3}

	cfg := &Config{
		SwapID:              swapID,
		OrderID:             orderID,
		MatchID:             matchID,
		Role:                RoleInitiator,
		PairBTC:             0,   // BTC
		PairXMR:             128, // XMR BIP-44 ID
		BtcAmount:           100_000,
		XmrAmount:           1000,
		LockBlocks:          2,
		PeerBTCPayoutScript: []byte{txscript.OP_TRUE},
		OwnXMRSweepDest:     "4tester",
		XmrNetTag:           18,
		AssetBTC:            btc,
		AssetXMR:            xmr,
		SendMsg:             msgr,
		Persist:             persist,
	}

	o, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	if err := o.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Initiator Start is no-op except transitioning to PhaseKeysSent.
	if o.state.Phase != PhaseKeysSent {
		t.Fatalf("after Start phase=%s want PhaseKeysSent", o.state.Phase)
	}

	partSetup, _, _, _ := fakePeerSetup(t, orderID, matchID)

	if err := o.Handle(EventKeysReceived{Setup: partSetup}); err != nil {
		t.Fatalf("handle part setup: %v", err)
	}
	if o.state.Phase != PhaseKeysReceived {
		t.Fatalf("after part setup phase=%s want PhaseKeysReceived", o.state.Phase)
	}
	// Outgoing message should be AdaptorSetupInit.
	if got := msgr.last().route; got != msgjson.AdaptorSetupInitRoute {
		t.Fatalf("outgoing route=%q want AdaptorSetupInitRoute", got)
	}
	if o.state.Lock == nil || o.state.Refund == nil {
		t.Fatal("lock/refund output not built")
	}

	// Fabricate a participant's AdaptorRefundPresigned. We don't need
	// the adaptor sig to be cryptographically valid - the orchestrator
	// parses and stores it but does not verify in this path. We do
	// need ParseAdaptorSignature to accept it though; use a real sig
	// generated from a throwaway key.
	otherPriv, _ := btcec.NewPrivateKey()
	otherTweak, _ := btcec.NewPrivateKey()
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&otherTweak.Key, &T)
	var hash [32]byte
	fakeSig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(otherPriv, hash[:], &T)
	if err != nil {
		t.Fatalf("fake adaptor: %v", err)
	}
	pres := EventRefundPresignedReceived{
		RefundSig:  make([]byte, 64),
		AdaptorSig: fakeSig.Serialize(),
	}
	if err := o.Handle(pres); err != nil {
		t.Fatalf("handle refund presigned: %v", err)
	}
	// Initiator now auto-advances through PhaseLockBroadcast on
	// the strength of FundBroadcastTaproot's blocking confirm and
	// lands in PhaseLockConfirmed waiting for AdaptorXmrLocked.
	if o.state.Phase != PhaseLockConfirmed {
		t.Fatalf("after refund presigned phase=%s want PhaseLockConfirmed", o.state.Phase)
	}
	if o.state.LockTx == nil || o.state.LockHeight != 100 {
		t.Fatalf("lockTx not recorded: tx=%v height=%d", o.state.LockTx != nil, o.state.LockHeight)
	}
	// Outgoing AdaptorLocked.
	last := msgr.last()
	if last.route != msgjson.AdaptorLockedRoute {
		t.Fatalf("final route=%q want AdaptorLockedRoute", last.route)
	}
	locked, ok := last.payload.(*msgjson.AdaptorLocked)
	if !ok {
		t.Fatalf("payload type=%T", last.payload)
	}
	if locked.Value != uint64(cfg.BtcAmount) {
		t.Fatalf("AdaptorLocked.Value=%d want %d", locked.Value, cfg.BtcAmount)
	}
}

// TestParticipantStartEmitsSetupPart checks that calling Start on a
// participant orchestrator emits AdaptorSetupPart and moves to
// PhaseKeysSent.
func TestParticipantStartEmitsSetupPart(t *testing.T) {
	msgr := &recordingSender{}
	cfg := &Config{
		SwapID:   [32]byte{1},
		OrderID:  [32]byte{2},
		MatchID:  [32]byte{3},
		Role:     RoleParticipant,
		AssetBTC: &fakeBTC{},
		AssetXMR: &fakeXMR{},
		SendMsg:  msgr,
		Persist:  &memPersister{},
	}
	o, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := o.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if o.state.Phase != PhaseKeysSent {
		t.Fatalf("phase=%s want PhaseKeysSent", o.state.Phase)
	}
	if len(msgr.sent) != 1 {
		t.Fatalf("sent %d msgs, want 1", len(msgr.sent))
	}
	if msgr.last().route != msgjson.AdaptorSetupPartRoute {
		t.Fatalf("route=%q want AdaptorSetupPartRoute", msgr.last().route)
	}
	out := msgr.last().payload.(*msgjson.AdaptorSetupPart)
	if len(out.DLEQProof) == 0 || len(out.PubSpendKeyHalf) == 0 {
		t.Fatal("setup part missing fields")
	}
}

// TestHandleRejectsWrongEvent confirms that mismatched events are
// rejected rather than silently advancing the machine.
func TestHandleRejectsWrongEvent(t *testing.T) {
	msgr := &recordingSender{}
	cfg := &Config{
		SwapID: [32]byte{1}, OrderID: [32]byte{2}, MatchID: [32]byte{3},
		Role:     RoleInitiator,
		AssetBTC: &fakeBTC{}, AssetXMR: &fakeXMR{}, SendMsg: msgr, Persist: &memPersister{},
	}
	o, _ := NewOrchestrator(cfg)
	_ = o.Start()
	// Initiator in PhaseKeysSent should reject anything that isn't
	// EventKeysReceived.
	err := o.Handle(EventLockConfirmed{Height: 1})
	if err == nil {
		t.Fatal("expected error for wrong event in PhaseKeysSent")
	}
	if got := o.state.Phase; got != PhaseKeysSent {
		t.Fatalf("phase advanced to %s on error; should stay PhaseKeysSent", got)
	}
}

// TestInitiatorRefundPath drives a fully-setup initiator from
// PhaseLockBroadcast through InitiateRefund -> RefundTxBroadcast ->
// EventRefundCSVMatured -> coop spendRefund broadcast (terminal).
func TestInitiatorRefundPath(t *testing.T) {
	msgr := &recordingSender{}
	btc := &fakeBTC{}
	cfg := &Config{
		SwapID: [32]byte{1}, OrderID: [32]byte{2}, MatchID: [32]byte{3},
		Role:                RoleInitiator,
		BtcAmount:           100_000,
		XmrAmount:           1000,
		LockBlocks:          2,
		PeerBTCPayoutScript: []byte{txscript.OP_TRUE},
		XmrNetTag:           18,
		AssetBTC:            btc,
		AssetXMR:            &fakeXMR{},
		SendMsg:             msgr,
		Persist:             &memPersister{},
	}
	o, _ := NewOrchestrator(cfg)
	_ = o.Start()

	partSetup, _, _, _ := fakePeerSetup(t, cfg.OrderID, cfg.MatchID)
	if err := o.Handle(EventKeysReceived{Setup: partSetup}); err != nil {
		t.Fatalf("part setup: %v", err)
	}

	// Fabricate a real adaptor sig for spendRefundTx using Bob's XMR
	// scalar as the tweak - this is what the initiator would later
	// decrypt with its own ed25519 key.
	//
	// Tweak point = Bob's XMR spend-key half secp pubkey. Here we
	// just reach into the orchestrator state to grab Bob's own
	// ed25519 scalar and use it to create T.
	bobScalar, _ := btcec.PrivKeyFromBytes(o.state.XmrSpendKeyHalf.Serialize())
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&bobScalar.Key, &T)

	// Alice's secp signing key is fresh for this test; we build an
	// adaptor sig on the orchestrator's spendRefundTx sighash.
	alicePriv, _ := btcec.NewPrivateKey()
	refundValue := o.state.RefundTx.TxOut[0].Value
	prev := txscript.NewCannedPrevOutputFetcher(o.state.Refund.PkScript, refundValue)
	sh := txscript.NewTxSigHashes(o.state.SpendRefundTx, prev)
	coopLeaf := txscript.NewBaseTapLeaf(o.state.Refund.CoopLeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(sh, txscript.SigHashDefault,
		o.state.SpendRefundTx, 0, prev, coopLeaf)
	if err != nil {
		t.Fatalf("sighash: %v", err)
	}
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(alicePriv, sigHash, &T)
	if err != nil {
		t.Fatalf("adaptor: %v", err)
	}

	pres := EventRefundPresignedReceived{
		RefundSig:  make([]byte, 64),
		AdaptorSig: esig.Serialize(),
	}
	if err := o.Handle(pres); err != nil {
		t.Fatalf("presigned: %v", err)
	}
	// Initiator auto-advances through PhaseLockBroadcast (lockTx
	// confirmed by FundBroadcastTaproot's blocking confirm) and
	// lands in PhaseLockConfirmed.
	if o.state.Phase != PhaseLockConfirmed {
		t.Fatalf("phase=%s want PhaseLockConfirmed", o.state.Phase)
	}

	// Now pivot to refund path. Initiator decides to bail.
	if err := o.InitiateRefund(); err != nil {
		t.Fatalf("InitiateRefund: %v", err)
	}
	if o.state.Phase != PhaseRefundTxBroadcast {
		t.Fatalf("phase=%s want PhaseRefundTxBroadcast", o.state.Phase)
	}
	if len(btc.broadcasts) != 1 {
		t.Fatalf("refundTx broadcasts: %d want 1", len(btc.broadcasts))
	}

	// CSV matures - initiator coop-refunds.
	if err := o.Handle(EventRefundCSVMatured{Height: 200}); err != nil {
		t.Fatalf("csv matured: %v", err)
	}
	if o.state.Phase != PhaseComplete {
		t.Fatalf("phase=%s want PhaseComplete", o.state.Phase)
	}
	if len(btc.broadcasts) != 2 {
		t.Fatalf("spendRefund broadcasts: %d want 2 total", len(btc.broadcasts))
	}
}

// driveSetupPhase runs both initiator and participant orchestrators
// through the setup phase by hand, exchanging emitted messages
// between them. After this call the participant is in
// PhaseRefundPresigned with a fully-populated refund chain ready
// to drive into refund/punish branches; the initiator has received
// the AdaptorSetupPart and emitted AdaptorSetupInit but has not yet
// processed the participant's refund-presigned (so it stays at
// PhaseKeysReceived).
//
// Returns both orchestrators along with their senders so callers
// can assert on the emitted messages. Used by the punish/recover
// branch tests, which need a valid participant state to start
// from but don't otherwise care about the initiator side.
func driveSetupPhase(t *testing.T, initBTC, partBTC *fakeBTC,
	initXMR, partXMR *fakeXMR, initSender, partSender *recordingSender) (
	init *Orchestrator, part *Orchestrator) {

	t.Helper()
	matchID := [32]byte{0x42}
	orderID := [32]byte{0x42}
	mkCfg := func(role Role, btc *fakeBTC, xmr *fakeXMR, s *recordingSender) *Config {
		return &Config{
			SwapID: matchID, OrderID: orderID, MatchID: matchID,
			Role:                role,
			BtcAmount:           100_000,
			XmrAmount:           1000,
			LockBlocks:          2,
			PeerBTCPayoutScript: []byte{txscript.OP_TRUE},
			OwnXMRSweepDest:     "4tester",
			XmrNetTag:           18,
			AssetBTC:            btc,
			AssetXMR:            xmr,
			SendMsg:             s,
			Persist:             &memPersister{},
		}
	}
	var err error
	init, err = NewOrchestrator(mkCfg(RoleInitiator, initBTC, initXMR, initSender))
	if err != nil {
		t.Fatalf("init NewOrchestrator: %v", err)
	}
	part, err = NewOrchestrator(mkCfg(RoleParticipant, partBTC, partXMR, partSender))
	if err != nil {
		t.Fatalf("part NewOrchestrator: %v", err)
	}
	if err := init.Start(); err != nil {
		t.Fatalf("init Start: %v", err)
	}
	if err := part.Start(); err != nil {
		t.Fatalf("part Start: %v", err)
	}
	// part emitted AdaptorSetupPart -> deliver to init.
	partLast := partSender.last()
	if partLast.route != msgjson.AdaptorSetupPartRoute {
		t.Fatalf("part emitted %s, want AdaptorSetupPartRoute", partLast.route)
	}
	setupPart, ok := partLast.payload.(*msgjson.AdaptorSetupPart)
	if !ok {
		t.Fatalf("part payload type %T", partLast.payload)
	}
	if err := init.Handle(EventKeysReceived{Setup: setupPart}); err != nil {
		t.Fatalf("init handle PartSetup: %v", err)
	}
	// init emitted AdaptorSetupInit -> deliver to part.
	initLast := initSender.last()
	if initLast.route != msgjson.AdaptorSetupInitRoute {
		t.Fatalf("init emitted %s, want AdaptorSetupInitRoute", initLast.route)
	}
	setupInit, ok := initLast.payload.(*msgjson.AdaptorSetupInit)
	if !ok {
		t.Fatalf("init payload type %T", initLast.payload)
	}
	if err := part.Handle(EventKeysReceived{Setup: setupInit}); err != nil {
		t.Fatalf("part handle InitSetup: %v", err)
	}
	if got := part.Phase(); got != PhaseRefundPresigned {
		t.Fatalf("part phase after setup = %s, want PhaseRefundPresigned", got)
	}
	return init, part
}

// TestBTCPayoutAddrFlowsThroughSetupPart confirms that the
// participant's BTC payout address rides on AdaptorSetupPart and
// the initiator decodes it into PeerBTCPayoutScript on receipt
// (closes the simnet blocker where the address never reached the
// initiator). Decoder is a closure here so the test does not
// depend on chaincfg.
func TestBTCPayoutAddrFlowsThroughSetupPart(t *testing.T) {
	const aliceAddr = "alice-test-addr"
	wantScript := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	matchID := [32]byte{0x55}
	orderID := [32]byte{0x66}

	initSender := &recordingSender{}
	partSender := &recordingSender{}

	mkCfg := func(role Role, btc *fakeBTC, s *recordingSender) *Config {
		cfg := &Config{
			SwapID: matchID, OrderID: orderID, MatchID: matchID,
			Role:                role,
			BtcAmount:           100_000,
			XmrAmount:           1000,
			LockBlocks:          2,
			PeerBTCPayoutScript: nil, // initiator must populate from setup msg
			OwnXMRSweepDest:     "4tester",
			XmrNetTag:           18,
			AssetBTC:            btc,
			AssetXMR:            &fakeXMR{},
			SendMsg:             s,
			Persist:             &memPersister{},
			DecodeBTCAddr: func(addr string) ([]byte, error) {
				if addr != aliceAddr {
					t.Fatalf("decoder got addr %q, want %q", addr, aliceAddr)
				}
				return wantScript, nil
			},
		}
		if role == RoleParticipant {
			cfg.OwnBTCPayoutAddr = aliceAddr
		}
		return cfg
	}

	init, err := NewOrchestrator(mkCfg(RoleInitiator, &fakeBTC{}, initSender))
	if err != nil {
		t.Fatalf("init NewOrchestrator: %v", err)
	}
	part, err := NewOrchestrator(mkCfg(RoleParticipant, &fakeBTC{}, partSender))
	if err != nil {
		t.Fatalf("part NewOrchestrator: %v", err)
	}
	if err := init.Start(); err != nil {
		t.Fatalf("init Start: %v", err)
	}
	if err := part.Start(); err != nil {
		t.Fatalf("part Start: %v", err)
	}

	// Snoop the participant's emit and confirm the address rode.
	partLast := partSender.last()
	setupPart, ok := partLast.payload.(*msgjson.AdaptorSetupPart)
	if !ok {
		t.Fatalf("part payload type %T", partLast.payload)
	}
	if setupPart.BTCPayoutAddr != aliceAddr {
		t.Fatalf("AdaptorSetupPart.BTCPayoutAddr = %q, want %q",
			setupPart.BTCPayoutAddr, aliceAddr)
	}

	// Deliver to the initiator and verify the decoded script
	// landed in cfg.PeerBTCPayoutScript.
	if err := init.Handle(EventKeysReceived{Setup: setupPart}); err != nil {
		t.Fatalf("init handle PartSetup: %v", err)
	}
	if got := init.Cfg().PeerBTCPayoutScript; !bytesEqual(got, wantScript) {
		t.Fatalf("init PeerBTCPayoutScript = %x, want %x", got, wantScript)
	}
}

// TestParticipantPunishPath drives a participant from a setup-
// complete state through InitiateRefund + EventRefundCSVMatured
// and verifies it broadcasts the punish-leaf spendRefundTx and
// reaches PhasePunish (terminal). This is the asymmetric outcome
// where the initiator stalled after the participant locked XMR -
// the participant takes the BTC, but XMR is forfeited.
func TestParticipantPunishPath(t *testing.T) {
	initBTC, partBTC := &fakeBTC{}, &fakeBTC{}
	initXMR, partXMR := &fakeXMR{}, &fakeXMR{}
	initSender, partSender := &recordingSender{}, &recordingSender{}
	_, part := driveSetupPhase(t, initBTC, partBTC, initXMR, partXMR, initSender, partSender)

	if err := part.InitiateRefund(); err != nil {
		t.Fatalf("part InitiateRefund: %v", err)
	}
	if got := part.Phase(); got != PhaseRefundTxBroadcast {
		t.Fatalf("after InitiateRefund phase = %s, want PhaseRefundTxBroadcast", got)
	}
	if len(partBTC.broadcasts) != 1 {
		t.Fatalf("refundTx broadcasts: %d, want 1", len(partBTC.broadcasts))
	}

	// CSV matures with no coop refund from initiator -> punish.
	if err := part.Handle(EventRefundCSVMatured{Height: 200}); err != nil {
		t.Fatalf("EventRefundCSVMatured: %v", err)
	}
	if got := part.Phase(); got != PhasePunish {
		t.Fatalf("after CSV matured phase = %s, want PhasePunish", got)
	}
	if len(partBTC.broadcasts) != 2 {
		t.Fatalf("after punish total broadcasts = %d, want 2 (refundTx + punish spendRefund)",
			len(partBTC.broadcasts))
	}
	// XMR was never swept (it's forfeited in this branch).
	if partXMR.swept != "" {
		t.Errorf("XMR sweep happened on punish path; dest=%q", partXMR.swept)
	}
}

// TestParticipantRecoverAndSweepPath drives a participant through
// the alternate refund branch: after both parties have decided to
// unwind and the initiator coop-refunded, the on-chain witness
// reveals the initiator's XMR scalar and the participant sweeps
// the shared-address XMR back to its own destination. Terminal:
// PhaseComplete (both parties whole minus chain fees).
func TestParticipantRecoverAndSweepPath(t *testing.T) {
	initBTC, partBTC := &fakeBTC{}, &fakeBTC{}
	initXMR, partXMR := &fakeXMR{}, &fakeXMR{}
	initSender, partSender := &recordingSender{}, &recordingSender{}
	init, part := driveSetupPhase(t, initBTC, partBTC, initXMR, partXMR, initSender, partSender)

	// To produce a valid coop-refund witness, run the initiator
	// through InitiateRefund + EventRefundCSVMatured (its coop
	// branch), capturing the on-chain spendRefundTx witness. Then
	// feed that witness to the participant's
	// EventCoopRefundObserved handler.
	//
	// The initiator needs a valid SpendRefundAdaptorSig, which it
	// acquires from the participant's AdaptorRefundPresigned
	// message. Snoop that from partSender.
	var refundPresig *msgjson.AdaptorRefundPresigned
	for _, m := range partSender.sent {
		if m.route == msgjson.AdaptorRefundPresignedRoute {
			refundPresig = m.payload.(*msgjson.AdaptorRefundPresigned)
			break
		}
	}
	if refundPresig == nil {
		t.Fatal("participant never emitted AdaptorRefundPresigned")
	}
	if err := init.Handle(EventRefundPresignedReceived{
		RefundSig:  refundPresig.RefundSig,
		AdaptorSig: refundPresig.SpendRefundAdaptorSig,
	}); err != nil {
		t.Fatalf("init handle Presigned: %v", err)
	}
	if err := init.InitiateRefund(); err != nil {
		t.Fatalf("init InitiateRefund: %v", err)
	}
	if err := init.Handle(EventRefundCSVMatured{Height: 200}); err != nil {
		t.Fatalf("init EventRefundCSVMatured: %v", err)
	}
	// init's coop refund broadcast the spendRefundTx; the witness
	// is in initBTC.spendTx.TxIn[0].Witness.
	if initBTC.spendTx == nil {
		t.Fatal("initiator never broadcast spendRefundTx")
	}
	witness := initBTC.spendTx.TxIn[0].Witness
	if len(witness) < 4 {
		t.Fatalf("coop witness length = %d, want >=4", len(witness))
	}

	// Participant initiates refund (so participant's state is in
	// PhaseRefundTxBroadcast when it observes the coop), then
	// observes the on-chain coop refund and sweeps.
	if err := part.InitiateRefund(); err != nil {
		t.Fatalf("part InitiateRefund: %v", err)
	}
	if err := part.Handle(EventCoopRefundObserved{Witness: witness}); err != nil {
		t.Fatalf("part EventCoopRefundObserved: %v", err)
	}
	if got := part.Phase(); got != PhaseComplete {
		t.Fatalf("after recover+sweep phase = %s, want PhaseComplete", got)
	}
	// Sweep was executed against participant's OwnXMRSweepDest.
	if partXMR.swept != "4tester" {
		t.Errorf("XMR sweep dest = %q, want %q", partXMR.swept, "4tester")
	}
}

// TestStateFullSnapshotRoundtrip drives an initiator through to
// PhaseLockBroadcast (the most state-heavy point in the happy path
// before redemption), serializes the resulting State to bytes, and
// restores it. The restored State must contain matching values for
// all fields that were populated, including the rebuilt Lock and
// Refund taproot output materials.
func TestStateFullSnapshotRoundtrip(t *testing.T) {
	msgr := &recordingSender{}
	btc := &fakeBTC{}
	cfg := &Config{
		SwapID: [32]byte{0xAA}, OrderID: [32]byte{0xBB}, MatchID: [32]byte{0xCC},
		Role:                RoleInitiator,
		BtcAmount:           100_000,
		XmrAmount:           1000,
		LockBlocks:          2,
		PeerBTCPayoutScript: []byte{txscript.OP_TRUE},
		XmrNetTag:           18,
		AssetBTC:            btc, AssetXMR: &fakeXMR{}, SendMsg: msgr, Persist: &memPersister{},
	}
	o, _ := NewOrchestrator(cfg)
	_ = o.Start()

	partSetup, _, _, _ := fakePeerSetup(t, cfg.OrderID, cfg.MatchID)
	if err := o.Handle(EventKeysReceived{Setup: partSetup}); err != nil {
		t.Fatalf("part setup: %v", err)
	}

	// Build a real adaptor sig so PhaseLockBroadcast is reached.
	bobScalar, _ := btcec.PrivKeyFromBytes(o.state.XmrSpendKeyHalf.Serialize())
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&bobScalar.Key, &T)
	alicePriv, _ := btcec.NewPrivateKey()
	refundValue := o.state.RefundTx.TxOut[0].Value
	prev := txscript.NewCannedPrevOutputFetcher(o.state.Refund.PkScript, refundValue)
	sh := txscript.NewTxSigHashes(o.state.SpendRefundTx, prev)
	coopLeaf := txscript.NewBaseTapLeaf(o.state.Refund.CoopLeafScript)
	sigHash, _ := txscript.CalcTapscriptSignaturehash(sh, txscript.SigHashDefault,
		o.state.SpendRefundTx, 0, prev, coopLeaf)
	esig, _ := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(alicePriv, sigHash, &T)
	pres := EventRefundPresignedReceived{
		RefundSig:  make([]byte, 64),
		AdaptorSig: esig.Serialize(),
	}
	if err := o.Handle(pres); err != nil {
		t.Fatalf("presigned: %v", err)
	}

	// Snapshot and restore.
	o.state.mu.Lock()
	data, err := o.state.FullSnapshot()
	o.state.mu.Unlock()
	if err != nil {
		t.Fatalf("FullSnapshot: %v", err)
	}
	if len(data) < 200 {
		t.Fatalf("snapshot too small: %d bytes", len(data))
	}

	got, err := RestoreState(data)
	if err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	// Spot-check key fields.
	if got.Phase != o.state.Phase {
		t.Errorf("phase=%s want %s", got.Phase, o.state.Phase)
	}
	if got.SwapID != o.state.SwapID {
		t.Error("swap ID mismatch")
	}
	if got.LockHeight != o.state.LockHeight {
		t.Errorf("lock height got %d want %d", got.LockHeight, o.state.LockHeight)
	}
	if got.LockTx == nil || got.RefundTx == nil || got.SpendRefundTx == nil {
		t.Error("transactions not restored")
	}
	if got.Lock == nil || got.Refund == nil {
		t.Error("Lock/Refund not rebuilt from leaf scripts")
	}
	if got.SpendRefundAdaptorSig == nil {
		t.Error("adaptor sig not restored")
	}
	if got.BtcSignKey == nil ||
		!got.BtcSignKey.Key.Equals(&o.state.BtcSignKey.Key) {
		t.Error("btc sign key mismatch")
	}
	if got.XmrSpendKeyHalf == nil ||
		!bytesEqual(got.XmrSpendKeyHalf.Serialize(), o.state.XmrSpendKeyHalf.Serialize()) {
		t.Error("xmr spend key mismatch")
	}
}

// TestFilePersisterRoundtrip writes a full snapshot to disk and
// reads it back through the FilePersister.
func TestFilePersisterRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p, err := NewFilePersister(dir)
	if err != nil {
		t.Fatalf("NewFilePersister: %v", err)
	}
	// Build a minimal state with enough populated fields to
	// exercise the JSON encoding.
	s := &State{
		SwapID:     [32]byte{0xEE},
		OrderID:    [32]byte{0xFF},
		MatchID:    [32]byte{0x01},
		Role:       RoleInitiator,
		Phase:      PhaseLockBroadcast,
		LockBlocks: 2,
		LockHeight: 100,
	}
	priv, _ := btcec.NewPrivateKey()
	s.BtcSignKey = priv
	xmrK, _ := edwards.GeneratePrivateKey()
	s.XmrSpendKeyHalf = xmrK

	swapID := s.SwapID
	if err := p.SaveFull(swapID, s); err != nil {
		t.Fatalf("SaveFull: %v", err)
	}
	got, err := p.LoadFull(swapID)
	if err != nil {
		t.Fatalf("LoadFull: %v", err)
	}
	if got == nil {
		t.Fatal("got nil state")
	}
	if got.Phase != s.Phase || got.LockHeight != s.LockHeight {
		t.Errorf("mismatch: phase %s/%s height %d/%d",
			got.Phase, s.Phase, got.LockHeight, s.LockHeight)
	}
	// Missing swap returns (nil, nil).
	missing, err := p.LoadFull([32]byte{0xFE})
	if err != nil {
		t.Fatalf("LoadFull(missing): %v", err)
	}
	if missing != nil {
		t.Error("missing snapshot should be nil")
	}
}

// TestResumeFromSnapshot drives an orchestrator to PhaseLockBroadcast,
// snapshots its state, restores it via RestoreState, hands the
// restored state to NewOrchestratorFromState, and verifies the new
// orchestrator (a) reports the saved phase and (b) has the same
// per-swap key material as the original. Closes the restart-resilience
// gap end-to-end (snapshot -> bytes -> RestoreState -> Orchestrator).
func TestResumeFromSnapshot(t *testing.T) {
	msgr := &recordingSender{}
	cfg := &Config{
		SwapID: [32]byte{0xAA}, OrderID: [32]byte{0xBB}, MatchID: [32]byte{0xCC},
		Role:                RoleInitiator,
		BtcAmount:           100_000,
		XmrAmount:           1000,
		LockBlocks:          2,
		PeerBTCPayoutScript: []byte{txscript.OP_TRUE},
		XmrNetTag:           18,
		AssetBTC:            &fakeBTC{}, AssetXMR: &fakeXMR{}, SendMsg: msgr, Persist: &memPersister{},
	}
	o, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	if err := o.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	partSetup, _, _, _ := fakePeerSetup(t, cfg.OrderID, cfg.MatchID)
	if err := o.Handle(EventKeysReceived{Setup: partSetup}); err != nil {
		t.Fatalf("part setup: %v", err)
	}

	// Drive to PhaseLockBroadcast with a real adaptor sig.
	bobScalar, _ := btcec.PrivKeyFromBytes(o.state.XmrSpendKeyHalf.Serialize())
	var T btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&bobScalar.Key, &T)
	alicePriv, _ := btcec.NewPrivateKey()
	refundValue := o.state.RefundTx.TxOut[0].Value
	prev := txscript.NewCannedPrevOutputFetcher(o.state.Refund.PkScript, refundValue)
	sh := txscript.NewTxSigHashes(o.state.SpendRefundTx, prev)
	coopLeaf := txscript.NewBaseTapLeaf(o.state.Refund.CoopLeafScript)
	sigHash, _ := txscript.CalcTapscriptSignaturehash(sh, txscript.SigHashDefault,
		o.state.SpendRefundTx, 0, prev, coopLeaf)
	esig, _ := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(alicePriv, sigHash, &T)
	if err := o.Handle(EventRefundPresignedReceived{
		RefundSig:  make([]byte, 64),
		AdaptorSig: esig.Serialize(),
	}); err != nil {
		t.Fatalf("presigned: %v", err)
	}
	savedPhase := o.Phase()
	if savedPhase != PhaseLockConfirmed {
		t.Fatalf("setup phase = %s, want PhaseLockConfirmed", savedPhase)
	}

	// Snapshot -> bytes.
	o.state.mu.Lock()
	data, err := o.state.FullSnapshot()
	o.state.mu.Unlock()
	if err != nil {
		t.Fatalf("FullSnapshot: %v", err)
	}

	// Simulate a process restart: bytes -> RestoreState -> rehydrated
	// orchestrator with a fresh Config (the runtime deps are not
	// persisted; only the state is).
	restored, err := RestoreState(data)
	if err != nil {
		t.Fatalf("RestoreState: %v", err)
	}
	resumeCfg := &Config{
		SwapID: cfg.SwapID, OrderID: cfg.OrderID, MatchID: cfg.MatchID,
		Role:                cfg.Role,
		BtcAmount:           cfg.BtcAmount,
		XmrAmount:           cfg.XmrAmount,
		LockBlocks:          cfg.LockBlocks,
		PeerBTCPayoutScript: cfg.PeerBTCPayoutScript,
		XmrNetTag:           cfg.XmrNetTag,
		AssetBTC:            &fakeBTC{}, AssetXMR: &fakeXMR{},
		SendMsg: &recordingSender{}, Persist: &memPersister{},
	}
	resumed, err := NewOrchestratorFromState(resumeCfg, restored)
	if err != nil {
		t.Fatalf("NewOrchestratorFromState: %v", err)
	}

	if got := resumed.Phase(); got != savedPhase {
		t.Errorf("resumed phase = %s, want %s", got, savedPhase)
	}
	// The original key material survived the round trip.
	if !resumed.state.BtcSignKey.Key.Equals(&o.state.BtcSignKey.Key) {
		t.Error("resumed BtcSignKey != original")
	}
	if !bytesEqual(resumed.state.XmrSpendKeyHalf.Serialize(), o.state.XmrSpendKeyHalf.Serialize()) {
		t.Error("resumed XmrSpendKeyHalf != original")
	}
	// Cfg() returns the resume Config, which is what the rehydrated
	// machine should use going forward (different sender, different
	// asset adapter instances).
	if resumed.Cfg() != resumeCfg {
		t.Error("Cfg() should be the resume Config, not the original")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Ensure unused imports compile in all code paths.
var _ = fmt.Errorf
