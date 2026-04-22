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
	if o.state.Phase != PhaseLockBroadcast {
		t.Fatalf("after refund presigned phase=%s want PhaseLockBroadcast", o.state.Phase)
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
	if o.state.Phase != PhaseLockBroadcast {
		t.Fatalf("phase=%s want PhaseLockBroadcast", o.state.Phase)
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
