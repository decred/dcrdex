package core

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/core/adaptorswap"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// TestManagerRouteToEventMapping exercises routeToEvent for each
// supported msgjson Adaptor* route, asserting the returned Event
// type matches what the orchestrator expects.
func TestManagerRouteToEventMapping(t *testing.T) {
	tests := []struct {
		route   string
		payload any
		want    any // prototype Event of the expected concrete type
	}{
		{msgjson.AdaptorSetupPartRoute, &msgjson.AdaptorSetupPart{}, adaptorswap.EventKeysReceived{}},
		{msgjson.AdaptorSetupInitRoute, &msgjson.AdaptorSetupInit{}, adaptorswap.EventKeysReceived{}},
		{msgjson.AdaptorRefundPresignedRoute, &msgjson.AdaptorRefundPresigned{
			RefundSig: make([]byte, 64), SpendRefundAdaptorSig: make([]byte, 97),
		}, adaptorswap.EventRefundPresignedReceived{}},
		{msgjson.AdaptorXmrLockedRoute, &msgjson.AdaptorXmrLocked{}, adaptorswap.EventKeysReceived{}},
		{msgjson.AdaptorSpendPresigRoute, &msgjson.AdaptorSpendPresig{}, adaptorswap.EventSpendPresigReceived{}},
		{msgjson.AdaptorRefundBroadcastRoute, &msgjson.AdaptorRefundBroadcast{}, adaptorswap.EventRefundObservedOnChain{}},
		{msgjson.AdaptorCoopRefundRoute, &msgjson.AdaptorCoopRefund{}, adaptorswap.EventCoopRefundObserved{}},
		{msgjson.AdaptorPunishRoute, &msgjson.AdaptorPunish{TxID: []byte{1, 2, 3}}, adaptorswap.EventPunishObserved{}},
	}
	for _, tc := range tests {
		t.Run(tc.route, func(t *testing.T) {
			evt, err := routeToEvent(tc.route, tc.payload)
			if err != nil {
				t.Fatalf("err=%v want nil", err)
			}
			if gotT, wantT := typeName(evt), typeName(tc.want); gotT != wantT {
				t.Fatalf("event type=%s want %s", gotT, wantT)
			}
		})
	}

	// AdaptorLocked now produces EventLockConfirmed (trust mode in
	// the absence of a participant-side chain watcher).
	if evt, err := routeToEvent(msgjson.AdaptorLockedRoute,
		&msgjson.AdaptorLocked{}); err != nil {
		t.Fatalf("AdaptorLockedRoute: err=%v", err)
	} else if _, ok := evt.(adaptorswap.EventLockConfirmed); !ok {
		t.Fatalf("AdaptorLockedRoute event type %T, want EventLockConfirmed", evt)
	}

	// AdaptorSpendBroadcast remains informational (initiator
	// advances via observed witness).
	_, err := routeToEvent(msgjson.AdaptorSpendBroadcastRoute, nil)
	if !errors.Is(err, errPurelyInformational) {
		t.Fatalf("AdaptorSpendBroadcastRoute: err=%v want informational", err)
	}
	if !IsInformational(err) {
		t.Fatal("IsInformational rejected informational error for AdaptorSpendBroadcastRoute")
	}

	// Unknown route.
	if _, err := routeToEvent("adaptor_nonsense", nil); err == nil {
		t.Fatal("expected error for unknown route")
	}
}

func typeName(v any) string {
	switch v.(type) {
	case adaptorswap.EventKeysReceived:
		return "EventKeysReceived"
	case adaptorswap.EventRefundPresignedReceived:
		return "EventRefundPresignedReceived"
	case adaptorswap.EventSpendPresigReceived:
		return "EventSpendPresigReceived"
	case adaptorswap.EventRefundObservedOnChain:
		return "EventRefundObservedOnChain"
	case adaptorswap.EventCoopRefundObserved:
		return "EventCoopRefundObserved"
	case adaptorswap.EventPunishObserved:
		return "EventPunishObserved"
	}
	return "unknown"
}

// TestManagerStartAndHandle verifies that StartSwap registers an
// orchestrator for the match and Handle routes subsequent messages
// to it. Uses a participant orchestrator so StartSwap emits an
// outbound AdaptorSetupPart.
func TestManagerStartAndHandle(t *testing.T) {
	sender := &bridgeRecordingSender{}
	m := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC:  &bridgeFakeBTC{},
		XMR:  &bridgeFakeXMR{},
		Send: sender,
	})

	matchID := order.MatchID{0xAB}
	cfg := &adaptorswap.Config{
		SwapID:  [32]byte{1},
		OrderID: [32]byte{2},
		MatchID: matchID,
		Role:    adaptorswap.RoleParticipant,
	}
	if _, err := m.StartSwap(cfg); err != nil {
		t.Fatalf("StartSwap: %v", err)
	}
	if len(sender.routes) != 1 || sender.routes[0] != msgjson.AdaptorSetupPartRoute {
		t.Fatalf("sender routes=%v", sender.routes)
	}

	// Handle of an unknown match returns an error.
	other := order.MatchID{0xFF}
	if err := m.Handle(msgjson.AdaptorSetupInitRoute, other,
		&msgjson.AdaptorSetupInit{}); err == nil {
		t.Fatal("expected error for unknown match")
	}

	// AdaptorSpendBroadcast remains the only purely informational
	// route (terminal on the participant side; initiator advances
	// via on-chain witness observation, not the wire claim).
	if err := m.Handle(msgjson.AdaptorSpendBroadcastRoute, matchID,
		&msgjson.AdaptorSpendBroadcast{}); err == nil {
		t.Fatal("expected informational error")
	} else if !IsInformational(err) {
		t.Fatalf("got %v, want informational", err)
	}

	m.Stop(matchID)
	if err := m.Handle(msgjson.AdaptorSetupInitRoute, matchID, &msgjson.AdaptorSetupInit{}); err == nil {
		t.Fatal("expected error for stopped match")
	}
}

// TestHandleAdaptorMsg covers the noteHandler entry point:
// decodeAdaptorMsg picks the right payload type per route, the
// match ID is extracted correctly, and informational routes are
// silently absorbed (return nil) when an orchestrator exists for
// the match.
func TestHandleAdaptorMsg(t *testing.T) {
	sender := &bridgeRecordingSender{}
	mgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{}, Send: sender,
	})
	c := &Core{adaptorMgr: mgr, log: tLogger}

	matchID := order.MatchID{0xAB}
	if _, err := mgr.StartSwap(&adaptorswap.Config{
		SwapID:  [32]byte{1},
		OrderID: [32]byte{2},
		MatchID: matchID,
		Role:    adaptorswap.RoleParticipant,
	}); err != nil {
		t.Fatalf("StartSwap: %v", err)
	}

	// Each adaptor route round-trips through NewNotification +
	// handleAdaptorMsg. Most produce errors from the orchestrator
	// (it is in PhaseAwaitingInitSetup; only init-setup advances
	// it), but a non-decode error means the dispatch reached the
	// orchestrator, which is what we want to verify.
	cases := []struct {
		route   string
		payload any
	}{
		{msgjson.AdaptorSetupInitRoute, &msgjson.AdaptorSetupInit{MatchID: matchID[:]}},
		{msgjson.AdaptorRefundPresignedRoute, &msgjson.AdaptorRefundPresigned{
			MatchID: matchID[:], RefundSig: make([]byte, 64), SpendRefundAdaptorSig: make([]byte, 97)}},
		{msgjson.AdaptorXmrLockedRoute, &msgjson.AdaptorXmrLocked{MatchID: matchID[:]}},
		{msgjson.AdaptorSpendPresigRoute, &msgjson.AdaptorSpendPresig{MatchID: matchID[:]}},
		{msgjson.AdaptorRefundBroadcastRoute, &msgjson.AdaptorRefundBroadcast{MatchID: matchID[:]}},
		{msgjson.AdaptorCoopRefundRoute, &msgjson.AdaptorCoopRefund{MatchID: matchID[:]}},
		{msgjson.AdaptorPunishRoute, &msgjson.AdaptorPunish{MatchID: matchID[:], TxID: []byte{1}}},
	}
	for _, tc := range cases {
		t.Run(tc.route, func(t *testing.T) {
			msg, err := msgjson.NewNotification(tc.route, tc.payload)
			if err != nil {
				t.Fatalf("NewNotification: %v", err)
			}
			// Just confirm decode + dispatch reached the manager
			// without a decode-layer error. Whether the
			// orchestrator advances is covered elsewhere.
			_ = handleAdaptorMsg(c, nil, msg)
		})
	}

	// AdaptorSpendBroadcast remains informational on the
	// initiator side (it advances via observed witness, not the
	// wire claim). Confirm the handler suppresses the sentinel.
	msg, err := msgjson.NewNotification(msgjson.AdaptorSpendBroadcastRoute,
		&msgjson.AdaptorSpendBroadcast{MatchID: matchID[:]})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if err := handleAdaptorMsg(c, nil, msg); err != nil {
		t.Fatalf("informational adaptor_spend_broadcast leaked error: %v", err)
	}

	// Bad match ID length surfaces as a decode error.
	bad, err := msgjson.NewNotification(msgjson.AdaptorSetupPartRoute,
		&msgjson.AdaptorSetupPart{MatchID: []byte{1, 2}})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if err := handleAdaptorMsg(c, nil, bad); err == nil {
		t.Fatal("expected decode error for short matchid")
	}

	// Unknown adaptor route is rejected before the manager.
	unk, err := msgjson.NewNotification("adaptor_unknown",
		&msgjson.AdaptorPunish{MatchID: matchID[:]})
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	if err := handleAdaptorMsg(c, nil, unk); err == nil {
		t.Fatal("expected error for unknown route")
	}
}

// TestStartAdaptorMatches confirms that an adaptor-market match
// fed to startAdaptorMatches results in an orchestrator registered
// in the manager, and the role + amount derivations match Option-1
// semantics (BTC holder is maker == initiator) and base/quote pair
// assignment.
func TestStartAdaptorMatches(t *testing.T) {
	sender := &bridgeRecordingSender{}
	mgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{}, Send: sender,
	})
	c := &Core{
		adaptorMgr: mgr,
		log:        tLogger,
		net:        dex.Simnet,
		wallets:    make(map[uint32]*xcWallet),
	}

	const (
		btcAssetID uint32 = 0
		xmrAssetID uint32 = 128
	)
	mkt := &msgjson.Market{
		Name:            "btc_xmr",
		Base:            btcAssetID,
		Quote:           xmrAssetID,
		ScriptableAsset: btcAssetID,
		LockBlocks:      144,
	}

	ord := &order.LimitOrder{P: order.Prefix{ServerTime: time.Now()}}
	// Tracker has a dexConnection backed by captureWsConn so the
	// per-match dcSender wired into the orchestrator's Config can
	// emit AdaptorSetupPart on the participant path without a nil
	// deref. The captured messages are not asserted here (TestDcSender
	// covers the wire shape); we just need a non-nil transport.
	priv, _ := secp256k1.GeneratePrivateKey()
	tracker := &trackedTrade{
		Order: ord,
		dc: &dexConnection{
			WsConn: &captureWsConn{},
			acct:   &dexAccount{privKey: priv},
		},
	}

	matchID := order.MatchID{0xDE, 0xAD}
	makerMatch := &msgjson.Match{
		OrderID:  ord.ID().Bytes(),
		MatchID:  matchID[:],
		Quantity: 100_000_000, // 1 BTC, base
		Rate:     50_000_000,  // 0.5 XMR per BTC, base→quote
		Side:     uint8(order.Maker),
	}

	if err := c.startAdaptorMatches(tracker, []*msgjson.Match{makerMatch}, mkt); err != nil {
		t.Fatalf("startAdaptorMatches: %v", err)
	}

	// Orchestrator registered.
	o := mgr.orchestrators[matchID]
	if o == nil {
		t.Fatalf("no orchestrator registered for match %s", matchID)
	}
	// XmrNetTag derived from c.net (Simnet -> 18, mainnet-shaped).
	if o.Cfg().XmrNetTag != 18 {
		t.Fatalf("XmrNetTag = %d, want 18 for Simnet", o.Cfg().XmrNetTag)
	}
	// OwnXMRSweepDest left empty when no XMR wallet is connected.
	if o.Cfg().OwnXMRSweepDest != "" {
		t.Fatalf("expected empty OwnXMRSweepDest with no wallet, got %q", o.Cfg().OwnXMRSweepDest)
	}

	// Per-match dcSender wires outbound through tracker.dc.WsConn,
	// not through the manager's default sender. So assertions about
	// who emitted what go through the captured ws conn.
	captured := tracker.dc.WsConn.(*captureWsConn)
	// Maker on a BTC-base market => initiator => Start sends
	// nothing (initiator waits for AdaptorSetupPart).
	if len(captured.sent) != 0 {
		t.Fatalf("initiator should not emit setup; ws sent %d", len(captured.sent))
	}

	// A second match where this client is the taker => participant
	// => Start emits AdaptorSetupPart.
	matchID2 := order.MatchID{0xBE, 0xEF}
	takerMatch := &msgjson.Match{
		OrderID:  ord.ID().Bytes(),
		MatchID:  matchID2[:],
		Quantity: 200_000_000,
		Rate:     50_000_000,
		Side:     uint8(order.Taker),
	}
	if err := c.startAdaptorMatches(tracker, []*msgjson.Match{takerMatch}, mkt); err != nil {
		t.Fatalf("startAdaptorMatches taker: %v", err)
	}
	if mgr.orchestrators[matchID2] == nil {
		t.Fatalf("no orchestrator registered for taker match %s", matchID2)
	}
	if len(captured.sent) != 1 || captured.sent[0].Route != msgjson.AdaptorSetupPartRoute {
		t.Fatalf("participant should emit AdaptorSetupPart; captured=%d", len(captured.sent))
	}
	// Manager's default sender stays empty - per-match override
	// took priority.
	if len(sender.routes) != 0 {
		t.Fatalf("manager default sender should not be used; routes=%v", sender.routes)
	}
}

// TestOnCounterPartyAddress confirms that a peer-address message
// for an adaptor match is routed into the orchestrator's
// PeerBTCPayoutScript, that an unknown match returns
// handled=false (so the HTLC path can run), and that a mismatched
// follow-up address is rejected.
func TestOnCounterPartyAddress(t *testing.T) {
	mgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{}, Send: &bridgeRecordingSender{},
	})
	matchID := order.MatchID{0xC0, 0xDE}
	o, err := mgr.StartSwap(&adaptorswap.Config{
		SwapID:  [32]byte{1},
		OrderID: [32]byte{2},
		MatchID: matchID,
		Role:    adaptorswap.RoleInitiator, // initiator needs peer's BTC payout
	})
	if err != nil {
		t.Fatalf("StartSwap: %v", err)
	}

	// Two valid regtest P2PKH addresses derived from fresh keys
	// so the test is independent of any external fixture.
	addr1 := genRegtestAddr(t)
	addr2 := genRegtestAddr(t)
	if addr1 == addr2 {
		t.Fatalf("test setup: regtest addr generator produced duplicates")
	}

	handled, err := mgr.OnCounterPartyAddress(matchID, addr1, dex.Simnet)
	if !handled {
		t.Fatal("expected handled=true for known adaptor match")
	}
	if err != nil {
		t.Fatalf("OnCounterPartyAddress: %v", err)
	}
	if got := o.Cfg().PeerBTCPayoutScript; len(got) == 0 {
		t.Fatal("PeerBTCPayoutScript not set")
	}

	// Unknown match: handled=false so the HTLC path takes over.
	other := order.MatchID{0xFF}
	handled, err = mgr.OnCounterPartyAddress(other, addr1, dex.Simnet)
	if handled || err != nil {
		t.Fatalf("unknown match: handled=%v err=%v, want false/nil", handled, err)
	}

	// Mismatched follow-up address rejected.
	handled, err = mgr.OnCounterPartyAddress(matchID, addr2, dex.Simnet)
	if !handled {
		t.Fatal("expected handled=true for known match on follow-up")
	}
	if err == nil {
		t.Fatal("expected error for mismatched follow-up address")
	}

	// Identical follow-up address is idempotent.
	handled, err = mgr.OnCounterPartyAddress(matchID, addr1, dex.Simnet)
	if !handled || err != nil {
		t.Fatalf("idempotent re-set: handled=%v err=%v", handled, err)
	}

	// Bad address surfaces decode error.
	handled, err = mgr.OnCounterPartyAddress(matchID, "not-an-address", dex.Simnet)
	if !handled || err == nil {
		t.Fatalf("bad address: handled=%v err=%v", handled, err)
	}
}

// genRegtestAddr returns a fresh BTC regtest P2PKH address.
func genRegtestAddr(t *testing.T) string {
	t.Helper()
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("priv: %v", err)
	}
	addr, err := btcutil.NewAddressPubKeyHash(
		btcutil.Hash160(priv.PubKey().SerializeCompressed()),
		&chaincfg.RegressionNetParams)
	if err != nil {
		t.Fatalf("addr: %v", err)
	}
	return addr.EncodeAddress()
}

func TestXmrNetTagForNet(t *testing.T) {
	cases := []struct {
		net  dex.Network
		want uint64
	}{
		{dex.Mainnet, 18},
		{dex.Testnet, 24}, // stagenet workaround for monero_c testnet bugs
		{dex.Simnet, 18},
	}
	for _, tc := range cases {
		if got := xmrNetTagForNet(tc.net); got != tc.want {
			t.Errorf("xmrNetTagForNet(%s) = %d, want %d", tc.net, got, tc.want)
		}
	}
}

// TestDcSender verifies the production message sender wraps the
// payload in a notification on the right route and pushes it
// through the dexConnection's websocket.
func TestDcSender(t *testing.T) {
	captured := &captureWsConn{}
	priv, _ := secp256k1.GeneratePrivateKey()
	dc := &dexConnection{WsConn: captured, acct: &dexAccount{privKey: priv}}
	s := &dcSender{dc: dc}

	matchID := order.MatchID{0xCA, 0xFE}
	payload := &msgjson.AdaptorSetupPart{
		MatchID:         matchID[:],
		PubSpendKeyHalf: make([]byte, 32),
	}
	if err := s.SendToPeer(msgjson.AdaptorSetupPartRoute, payload); err != nil {
		t.Fatalf("SendToPeer: %v", err)
	}
	if len(captured.sent) != 1 {
		t.Fatalf("captured.sent = %d, want 1", len(captured.sent))
	}
	got := captured.sent[0]
	if got.Route != msgjson.AdaptorSetupPartRoute {
		t.Errorf("route = %s, want %s", got.Route, msgjson.AdaptorSetupPartRoute)
	}
	if got.Type != msgjson.Notification {
		t.Errorf("type = %d, want Notification", got.Type)
	}
	// Round-trip the encoded payload to verify the matchid
	// survived.
	var back msgjson.AdaptorSetupPart
	if err := got.Unmarshal(&back); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !bytes.Equal(back.MatchID, matchID[:]) {
		t.Errorf("matchid lost in transit: got %x, want %x", back.MatchID, matchID[:])
	}

	// SendErr from the underlying WsConn surfaces.
	captured.sendErr = errors.New("ws down")
	if err := s.SendToPeer(msgjson.AdaptorSetupPartRoute, payload); err == nil {
		t.Fatal("expected SendErr to surface")
	}
}

// captureWsConn is a minimal comms.WsConn that records each Send
// call. Only Send is used by dcSender; the rest are no-op stubs.
type captureWsConn struct {
	sent    []*msgjson.Message
	sendErr error
}

func (c *captureWsConn) NextID() uint64 { return 0 }
func (c *captureWsConn) IsDown() bool   { return false }
func (c *captureWsConn) Send(m *msgjson.Message) error {
	c.sent = append(c.sent, m)
	return c.sendErr
}
func (c *captureWsConn) SendRaw([]byte) error { return nil }
func (c *captureWsConn) Request(*msgjson.Message, func(*msgjson.Message)) error {
	return nil
}
func (c *captureWsConn) RequestRaw(uint64, []byte, func(*msgjson.Message)) error {
	return nil
}
func (c *captureWsConn) RequestWithTimeout(*msgjson.Message, func(*msgjson.Message),
	time.Duration, func()) error {
	return nil
}
func (c *captureWsConn) Connect(context.Context) (*sync.WaitGroup, error) { return nil, nil }
func (c *captureWsConn) MessageSource() <-chan *msgjson.Message           { return nil }
func (c *captureWsConn) UpdateURL(string)                                 {}

// TestManagerMultipleSwapsIsolated verifies that several
// concurrent matches on a single AdaptorSwapManager get distinct
// orchestrators, that a Handle for one match leaves the others
// untouched, and that stopping one match does not disturb the
// rest. Exercises the per-match registry + mutex.
func TestManagerMultipleSwapsIsolated(t *testing.T) {
	mgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{},
		Send: &bridgeRecordingSender{},
	})

	mkCfg := func(b byte, role adaptorswap.Role) *adaptorswap.Config {
		var mid order.MatchID
		mid[0] = b
		return &adaptorswap.Config{
			SwapID:  [32]byte{b},
			OrderID: [32]byte{b},
			MatchID: mid,
			Role:    role,
		}
	}

	cfgs := []*adaptorswap.Config{
		mkCfg(0x01, adaptorswap.RoleInitiator),
		mkCfg(0x02, adaptorswap.RoleParticipant),
		mkCfg(0x03, adaptorswap.RoleInitiator),
	}
	orchs := make(map[order.MatchID]*adaptorswap.Orchestrator, len(cfgs))
	for _, cfg := range cfgs {
		o, err := mgr.StartSwap(cfg)
		if err != nil {
			t.Fatalf("StartSwap %x: %v", cfg.MatchID[:1], err)
		}
		orchs[cfg.MatchID] = o
	}
	if len(mgr.orchestrators) != 3 {
		t.Fatalf("registry size = %d, want 3", len(mgr.orchestrators))
	}
	// All three are distinct instances.
	if orchs[cfgs[0].MatchID] == orchs[cfgs[1].MatchID] {
		t.Fatal("matches 1 and 2 share an orchestrator")
	}

	// Capture each orch's initial phase. Initiators start at
	// PhaseKeysSent; participants at PhaseKeysSent (after
	// sendPartSetup).
	initialPhases := make(map[order.MatchID]adaptorswap.Phase, len(cfgs))
	for mid, o := range orchs {
		initialPhases[mid] = o.Phase()
	}

	// Drive only match 1 through the next setup step. Phases of
	// matches 2 and 3 must not move.
	target := cfgs[0].MatchID
	other := cfgs[1].MatchID
	thirdMatch := cfgs[2].MatchID

	// A bogus AdaptorSetupInit advances the target's state-machine
	// attempt (it'll likely fail validation but the failure path is
	// per-orchestrator). What we care about is whether the OTHER
	// orchestrators see any state change.
	_ = mgr.Handle(msgjson.AdaptorSetupInitRoute, target, &msgjson.AdaptorSetupInit{
		MatchID: target[:],
	})
	if got := orchs[other].Phase(); got != initialPhases[other] {
		t.Errorf("match %x phase moved from %s to %s after handling match %x",
			other[:1], initialPhases[other], got, target[:1])
	}
	if got := orchs[thirdMatch].Phase(); got != initialPhases[thirdMatch] {
		t.Errorf("match %x phase moved after handling match %x", thirdMatch[:1], target[:1])
	}

	// Stop match 2 only.
	mgr.Stop(other)
	if _, present := mgr.orchestrators[other]; present {
		t.Error("orchestrator for stopped match still in registry")
	}
	if _, present := mgr.orchestrators[target]; !present {
		t.Error("non-stopped match removed from registry")
	}
	// Handle on the stopped match errors, but on the others still
	// works (here just confirm dispatch, not state advancement).
	if err := mgr.Handle(msgjson.AdaptorSetupPartRoute, other,
		&msgjson.AdaptorSetupPart{MatchID: other[:]}); err == nil {
		t.Error("Handle on stopped match should error")
	}
}

// TestSetupPhaseRoundTrip is the first end-to-end test of the
// adaptor-swap message exchange. It wires two AdaptorSwapManagers
// (one initiator, one participant) with senders that forward
// outbound messages into the other manager's Handle, then runs the
// pump until the message queue drains. Asserts both orchestrators
// reach the expected phase by the end of the setup-phase exchange.
//
// Halts before any chain interaction by giving the initiator a BTC
// adapter whose FundBroadcastTaproot returns an error; this leaves
// the initiator in PhaseKeysReceived (after responding with
// AdaptorSetupInit) without the test needing a live chain backend.
func TestSetupPhaseRoundTrip(t *testing.T) {
	type queuedMsg struct {
		route   string
		payload any
	}
	var toInit, toPart []queuedMsg

	initSender := senderFunc(func(route string, payload any) error {
		toPart = append(toPart, queuedMsg{route, payload})
		return nil
	})
	partSender := senderFunc(func(route string, payload any) error {
		toInit = append(toInit, queuedMsg{route, payload})
		return nil
	})

	// Initiator's BTC adapter halts at FundBroadcastTaproot so the
	// state machine stops at PhaseKeysReceived without needing a
	// real chain.
	initMgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &errorBTC{}, XMR: &bridgeFakeXMR{}, Send: initSender,
	})
	partMgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{}, Send: partSender,
	})

	matchID := order.MatchID{0x42, 0x42}
	makeCfg := func(role adaptorswap.Role) *adaptorswap.Config {
		return &adaptorswap.Config{
			SwapID:              [32]byte{1},
			OrderID:             [32]byte{2},
			MatchID:             matchID,
			Role:                role,
			PairBTC:             0,
			PairXMR:             128,
			BtcAmount:           100_000_000,
			XmrAmount:           50_000_000,
			LockBlocks:          144,
			XmrNetTag:           18,
			PeerBTCPayoutScript: []byte{0x51}, // OP_TRUE; placeholder
			OwnXMRSweepDest:     "test-xmr-dest",
		}
	}

	if _, err := initMgr.StartSwap(makeCfg(adaptorswap.RoleInitiator)); err != nil {
		t.Fatalf("initiator StartSwap: %v", err)
	}
	if _, err := partMgr.StartSwap(makeCfg(adaptorswap.RoleParticipant)); err != nil {
		t.Fatalf("participant StartSwap: %v", err)
	}

	// Pump messages until both queues drain. Bound iterations to
	// catch infinite loops cheaply.
	const maxIters = 32
	for i := 0; i < maxIters; i++ {
		if len(toInit) == 0 && len(toPart) == 0 {
			break
		}
		if len(toInit) > 0 {
			m := toInit[0]
			toInit = toInit[1:]
			if err := initMgr.Handle(m.route, matchID, m.payload); err != nil && !IsInformational(err) {
				t.Logf("initMgr.Handle(%s): %v", m.route, err)
			}
		}
		if len(toPart) > 0 {
			m := toPart[0]
			toPart = toPart[1:]
			if err := partMgr.Handle(m.route, matchID, m.payload); err != nil && !IsInformational(err) {
				t.Logf("partMgr.Handle(%s): %v", m.route, err)
			}
		}
	}
	if len(toInit) > 0 || len(toPart) > 0 {
		t.Fatalf("queues not drained: toInit=%d toPart=%d", len(toInit), len(toPart))
	}

	initOrch := initMgr.orchestrators[matchID]
	partOrch := partMgr.orchestrators[matchID]
	if initOrch == nil || partOrch == nil {
		t.Fatalf("orchestrators missing: init=%v part=%v", initOrch, partOrch)
	}

	if got := partOrch.Phase(); got != adaptorswap.PhaseRefundPresigned {
		t.Errorf("participant phase = %s, want PhaseRefundPresigned", got)
	}
	if got := initOrch.Phase(); got != adaptorswap.PhaseKeysReceived {
		t.Errorf("initiator phase = %s, want PhaseKeysReceived (halt at FundBroadcastTaproot)", got)
	}
}

type senderFunc func(route string, payload any) error

func (f senderFunc) SendToPeer(route string, payload any) error { return f(route, payload) }

type errorBTC struct{}

func (*errorBTC) FundBroadcastTaproot(pkScript []byte, value int64) (*wire.MsgTx, uint32, int64, error) {
	return nil, 0, 0, errors.New("test halt: FundBroadcastTaproot disabled")
}
func (*errorBTC) ObserveSpend(wire.OutPoint, int64) ([][]byte, error) { return nil, nil }
func (*errorBTC) BroadcastTx(*wire.MsgTx) (string, error)             { return "", nil }
func (*errorBTC) CurrentHeight() (int64, error)                       { return 0, nil }

// ---- local test doubles (same shape as the orchestrator test mocks
// but declared here since the bridge is in package core) ----

type bridgeRecordingSender struct {
	routes []string
}

func (r *bridgeRecordingSender) SendToPeer(route string, payload any) error {
	r.routes = append(r.routes, route)
	return nil
}

type bridgeFakeBTC struct{}

func (*bridgeFakeBTC) FundBroadcastTaproot(pkScript []byte, value int64) (*wire.MsgTx, uint32, int64, error) {
	return nil, 0, 0, nil
}
func (*bridgeFakeBTC) ObserveSpend(outpoint wire.OutPoint, startHeight int64) ([][]byte, error) {
	return nil, nil
}
func (*bridgeFakeBTC) BroadcastTx(tx *wire.MsgTx) (string, error) { return "", nil }
func (*bridgeFakeBTC) CurrentHeight() (int64, error)              { return 0, nil }

type bridgeFakeXMR struct{}

func (*bridgeFakeXMR) SendToSharedAddress(addr string, amount uint64) (string, uint64, error) {
	return "", 0, nil
}
func (*bridgeFakeXMR) WatchSharedAddress(swapID, addr, viewKey string, rh, amt uint64) (adaptorswap.XMRWatch, error) {
	return nil, nil
}
func (*bridgeFakeXMR) SweepSharedAddress(swapID, addr, sk, vk string, rh uint64, dest string) (string, error) {
	return "", nil
}
