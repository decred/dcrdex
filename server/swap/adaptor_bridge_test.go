package swap

import (
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/internal/adaptorsigs"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/swap/adaptor"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/decred/dcrd/dcrec/edwards/v2"
)

// bridgeRouter captures forwarded messages.
type bridgeRouter struct {
	sent []bridgeRouted
}

type bridgeRouted struct {
	m     order.MatchID
	role  adaptor.Role
	route string
}

func (r *bridgeRouter) SendTo(m order.MatchID, role adaptor.Role, route string, _ any) error {
	r.sent = append(r.sent, bridgeRouted{m, role, route})
	return nil
}

// TestAdaptorCoordinatorsLifecycle exercises the pool's
// Start/Handle/Stop contract: start creates an entry, Handle
// dispatches to it, Stop removes it, subsequent Handle errors.
func TestAdaptorCoordinatorsLifecycle(t *testing.T) {
	router := &bridgeRouter{}
	tmpl := adaptor.Config{
		Router:  router,
		Report:  NoopReporter{},
		Persist: NoopPersister{},
	}
	pool := NewAdaptorCoordinators(tmpl)

	var matchID order.MatchID
	copy(matchID[:], []byte{0xAB})
	var orderID order.MatchID
	copy(orderID[:], []byte{0xCD})

	c, err := pool.Start(matchID, orderID, 0, 128, 2)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if pool.Coordinator(matchID) != c {
		t.Fatal("pool.Coordinator returned wrong instance")
	}

	// Duplicate start errors.
	if _, err := pool.Start(matchID, orderID, 0, 128, 2); err == nil {
		t.Fatal("expected error for duplicate Start")
	}

	// Feed a setup message.
	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	part := &msgjson.AdaptorSetupPart{
		OrderID:         orderID[:],
		MatchID:         matchID[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}
	if err := pool.Handle(msgjson.AdaptorSetupPartRoute, matchID, part); err != nil {
		t.Fatalf("handle part: %v", err)
	}
	if c.Phase() != adaptor.PhaseAwaitingInitSetup {
		t.Fatalf("phase=%s want PhaseAwaitingInitSetup", c.Phase())
	}
	if len(router.sent) != 1 || router.sent[0].role != adaptor.RoleInitiator {
		t.Fatalf("router sent=%v", router.sent)
	}

	// Handle of unknown match errors.
	var other order.MatchID
	copy(other[:], []byte{0xFF})
	if err := pool.Handle(msgjson.AdaptorSetupPartRoute, other, part); err == nil {
		t.Fatal("expected error for unknown match")
	}

	// Stop and then Handle errors on the now-unknown match.
	pool.Stop(matchID)
	if err := pool.Handle(msgjson.AdaptorSetupPartRoute, matchID, part); err == nil {
		t.Fatal("expected error after Stop")
	}
}

// TestSwapperHandleAdaptorWithoutPool returns an error when
// AdaptorCoordinators is not configured. Verifies HTLC-only
// deployments aren't accidentally exposed to adaptor routing.
func TestSwapperHandleAdaptorWithoutPool(t *testing.T) {
	s := &Swapper{}
	var matchID order.MatchID
	if err := s.HandleAdaptor(msgjson.AdaptorSetupPartRoute, matchID, nil); err == nil {
		t.Fatal("expected error when adaptorCoords is nil")
	}
	if err := s.StartAdaptorMatch(matchID, matchID, 0, 128, 2); err == nil {
		t.Fatal("expected error from StartAdaptorMatch without pool")
	}
	// Stop is a no-op without a pool.
	s.StopAdaptorMatch(matchID)
}

// TestSwapperHandleAdaptorWithPool wires a pool through a Swapper
// and verifies the dispatch path reaches the coordinator.
func TestSwapperHandleAdaptorWithPool(t *testing.T) {
	router := &bridgeRouter{}
	pool := NewAdaptorCoordinators(adaptor.Config{
		Router: router, Report: NoopReporter{}, Persist: NoopPersister{},
	})
	s := &Swapper{adaptorCoords: pool}

	var matchID, orderID order.MatchID
	copy(matchID[:], []byte{0x01})
	copy(orderID[:], []byte{0x02})

	if err := s.StartAdaptorMatch(matchID, orderID, 0, 128, 2); err != nil {
		t.Fatalf("StartAdaptorMatch: %v", err)
	}

	// Real part-setup payload routes through.
	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	part := &msgjson.AdaptorSetupPart{
		OrderID:         orderID[:],
		MatchID:         matchID[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}
	if err := s.HandleAdaptor(msgjson.AdaptorSetupPartRoute, matchID, part); err != nil {
		t.Fatalf("HandleAdaptor: %v", err)
	}
	if len(router.sent) != 1 {
		t.Fatalf("router got %d msgs", len(router.sent))
	}

	s.StopAdaptorMatch(matchID)
	if err := s.HandleAdaptor(msgjson.AdaptorSetupPartRoute, matchID, part); err == nil {
		t.Fatal("expected error after Stop")
	}
}

// TestNegotiateAdaptor exercises the full match-creation path: a
// real Swapper rig is given an AdaptorCoordinators pool, then
// NegotiateAdaptor is invoked with a single match. Asserts that
// (a) a coordinator was created for the match, and (b) standard
// 'match' notifications were sent to both maker and taker so the
// existing client-side ack flow still kicks in.
func TestNegotiateAdaptor(t *testing.T) {
	set := tPerfectLimitLimit(uint64(1e8), uint64(1e8), true)
	matchInfo := set.matchInfos[0]
	rig, cleanup := tNewTestRig(matchInfo)
	defer cleanup()

	router := &bridgeRouter{}
	pool := NewAdaptorCoordinators(adaptor.Config{
		Router: router, Report: NoopReporter{}, Persist: NoopPersister{},
	})
	rig.swapper.adaptorCoords = pool

	rig.swapper.NegotiateAdaptor([]*order.MatchSet{set.matchSet}, ABCID, 144)

	if c := pool.Coordinator(matchInfo.matchID); c == nil {
		t.Fatalf("no coordinator created for match %s", matchInfo.matchID)
	}

	// HTLC tracking map must NOT contain the adaptor match.
	rig.swapper.matchMtx.RLock()
	_, htlcTracked := rig.swapper.matches[matchInfo.matchID]
	rig.swapper.matchMtx.RUnlock()
	if htlcTracked {
		t.Fatalf("adaptor match %s should not be in s.matches (HTLC tracking)", matchInfo.matchID)
	}

	// Both maker and taker should have received a 'match' request
	// from the standard notification fanout.
	if req := rig.auth.popReq(matchInfo.maker.acct); req == nil {
		t.Fatal("maker did not receive a match notification")
	}
	if req := rig.auth.popReq(matchInfo.taker.acct); req == nil {
		t.Fatal("taker did not receive a match notification")
	}
}

// TestAdaptorCoordinatorsMultipleMatchesIsolated verifies the
// server-side per-match registry behaves analogously to the client
// AdaptorSwapManager: distinct coordinators per match, Handle for
// one match leaves the others untouched, Stop on one removes only
// that entry.
func TestAdaptorCoordinatorsMultipleMatchesIsolated(t *testing.T) {
	router := &bridgeRouter{}
	pool := NewAdaptorCoordinators(adaptor.Config{
		Router: router, Report: NoopReporter{}, Persist: NoopPersister{},
	})

	type swap struct {
		matchID, orderID order.MatchID
	}
	swaps := []swap{
		{matchID: order.MatchID{0xA1}, orderID: order.MatchID{0xA0}},
		{matchID: order.MatchID{0xB1}, orderID: order.MatchID{0xB0}},
		{matchID: order.MatchID{0xC1}, orderID: order.MatchID{0xC0}},
	}
	coords := make(map[order.MatchID]*adaptor.Coordinator, len(swaps))
	for _, s := range swaps {
		c, err := pool.Start(s.matchID, s.orderID, 0, 128, 144)
		if err != nil {
			t.Fatalf("Start %x: %v", s.matchID[:1], err)
		}
		coords[s.matchID] = c
	}
	if got := len(pool.m); got != 3 {
		t.Fatalf("registry size = %d, want 3", got)
	}
	if coords[swaps[0].matchID] == coords[swaps[1].matchID] {
		t.Fatal("matches share a coordinator instance")
	}

	// Capture initial phases.
	initial := make(map[order.MatchID]adaptor.Phase, len(swaps))
	for _, s := range swaps {
		initial[s.matchID] = coords[s.matchID].Phase()
	}

	// Drive only swap 0 through a real PartSetup.
	target := swaps[0].matchID
	other := swaps[1].matchID
	third := swaps[2].matchID

	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	part := &msgjson.AdaptorSetupPart{
		OrderID:         swaps[0].orderID[:],
		MatchID:         target[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}
	if err := pool.Handle(msgjson.AdaptorSetupPartRoute, target, part); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	// Target advanced.
	if got := coords[target].Phase(); got == initial[target] {
		t.Fatalf("target match %x phase did not advance", target[:1])
	}
	// Others unchanged.
	if got := coords[other].Phase(); got != initial[other] {
		t.Errorf("match %x phase moved from %s to %s after handling match %x",
			other[:1], initial[other], got, target[:1])
	}
	if got := coords[third].Phase(); got != initial[third] {
		t.Errorf("match %x phase moved after handling match %x", third[:1], target[:1])
	}

	// Stop only the second match.
	pool.Stop(other)
	if _, present := pool.m[other]; present {
		t.Error("stopped match still in registry")
	}
	if _, present := pool.m[target]; !present {
		t.Error("untouched match removed from registry")
	}
	if _, present := pool.m[third]; !present {
		t.Error("untouched third match removed from registry")
	}
	// Handle on the stopped match errors.
	if err := pool.Handle(msgjson.AdaptorSetupPartRoute, other, part); err == nil {
		t.Error("Handle on stopped match should error")
	}
}

// TestHandleAdaptorMsg covers the AuthManager-route entry point:
// decode the route's payload, authUser passes (we mock the
// authMgr to accept), the matchID is extracted correctly, and the
// dispatch reaches the registered coordinator via HandleAdaptor.
// Also covers the nil-coords / unknown-route / bad-matchid /
// auth-fail error paths.
func TestHandleAdaptorMsg(t *testing.T) {
	// Without an AdaptorCoordinators pool the handler should
	// short-circuit to RPCInternalError.
	t.Run("no-pool", func(t *testing.T) {
		s := &Swapper{}
		msg, _ := msgjson.NewNotification(msgjson.AdaptorSetupPartRoute,
			&msgjson.AdaptorSetupPart{MatchID: make([]byte, 32)})
		err := s.handleAdaptorMsg(account.AccountID{}, msg)
		if err == nil || err.Code != msgjson.RPCInternalError {
			t.Fatalf("err = %v, want RPCInternalError", err)
		}
	})

	router := &bridgeRouter{}
	pool := NewAdaptorCoordinators(adaptor.Config{
		Router: router, Report: NoopReporter{}, Persist: NoopPersister{},
	})
	auth := &fakeAuthMgr{}
	s := &Swapper{adaptorCoords: pool, authMgr: auth}

	matchID := order.MatchID{0x42}
	orderID := order.MatchID{0x07}
	if _, err := pool.Start(matchID, orderID, 0, 128, 144); err != nil {
		t.Fatalf("pool.Start: %v", err)
	}

	// Build a real AdaptorSetupPart that the coordinator will
	// validate.
	spend, _ := edwards.GeneratePrivateKey()
	view, _ := edwards.GeneratePrivateKey()
	btcKey, _ := btcec.NewPrivateKey()
	dleq, err := adaptorsigs.ProveDLEQ(spend.Serialize())
	if err != nil {
		t.Fatalf("dleq: %v", err)
	}
	part := &msgjson.AdaptorSetupPart{
		Signature:       msgjson.Signature{Sig: []byte{0xAA}}, // accepted by fakeAuthMgr
		OrderID:         orderID[:],
		MatchID:         matchID[:],
		PubSpendKeyHalf: spend.PubKey().Serialize(),
		ViewKeyHalf:     view.Serialize(),
		PubSignKeyHalf:  btcschnorr.SerializePubKey(btcKey.PubKey()),
		DLEQProof:       dleq,
	}
	msg, err := msgjson.NewNotification(msgjson.AdaptorSetupPartRoute, part)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}

	t.Run("happy", func(t *testing.T) {
		if rpcErr := s.handleAdaptorMsg(account.AccountID{}, msg); rpcErr != nil {
			t.Fatalf("handleAdaptorMsg: %v", rpcErr)
		}
		if pool.Coordinator(matchID).Phase() != adaptor.PhaseAwaitingInitSetup {
			t.Fatalf("phase = %s, want PhaseAwaitingInitSetup", pool.Coordinator(matchID).Phase())
		}
		if len(router.sent) != 1 {
			t.Fatalf("router got %d msgs, want 1", len(router.sent))
		}
	})

	t.Run("unknown-route", func(t *testing.T) {
		bad, _ := msgjson.NewNotification("adaptor_unknown",
			&msgjson.AdaptorPunish{MatchID: matchID[:]})
		err := s.handleAdaptorMsg(account.AccountID{}, bad)
		if err == nil || err.Code != msgjson.RPCParseError {
			t.Fatalf("err = %v, want RPCParseError", err)
		}
	})

	t.Run("bad-matchid", func(t *testing.T) {
		bad, _ := msgjson.NewNotification(msgjson.AdaptorSetupPartRoute,
			&msgjson.AdaptorSetupPart{MatchID: []byte{1, 2, 3}}) // wrong length
		err := s.handleAdaptorMsg(account.AccountID{}, bad)
		if err == nil || err.Code != msgjson.RPCParseError {
			t.Fatalf("err = %v, want RPCParseError", err)
		}
	})

	t.Run("auth-fail", func(t *testing.T) {
		auth.authErr = errors.New("bad sig")
		defer func() { auth.authErr = nil }()
		if rpcErr := s.handleAdaptorMsg(account.AccountID{}, msg); rpcErr == nil ||
			rpcErr.Code != msgjson.SignatureError {
			t.Fatalf("err = %v, want SignatureError", rpcErr)
		}
	})
}

// fakeAuthMgr is a minimal AuthManager whose Auth() returns
// authErr (or nil). Other methods are no-op stubs needed only to
// satisfy the interface; this test exercises only the auth path.
type fakeAuthMgr struct {
	authErr error
}

func (f *fakeAuthMgr) Auth(account.AccountID, []byte, []byte) error { return f.authErr }
func (*fakeAuthMgr) Sign(...msgjson.Signable)                       {}
func (*fakeAuthMgr) Send(account.AccountID, *msgjson.Message) error { return nil }
func (*fakeAuthMgr) Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error {
	return nil
}
func (*fakeAuthMgr) RequestWithTimeout(account.AccountID, *msgjson.Message,
	func(comms.Link, *msgjson.Message), time.Duration, func()) error {
	return nil
}
func (*fakeAuthMgr) Route(string, func(account.AccountID, *msgjson.Message) *msgjson.Error) {}
func (*fakeAuthMgr) SwapSuccess(account.AccountID, db.MarketMatchID, uint64, time.Time) {
}
func (*fakeAuthMgr) Inaction(account.AccountID, db.Outcome, db.MarketMatchID, uint64,
	time.Time, order.OrderID) {
}

// TestAdaptorRouteToEventMapping confirms every supported
// server-inbound route produces the correct Event type, and
// unknown routes error.
func TestAdaptorRouteToEventMapping(t *testing.T) {
	tests := []struct {
		route   string
		payload any
		kind    string
	}{
		{msgjson.AdaptorSetupPartRoute, &msgjson.AdaptorSetupPart{}, "EventPartSetup"},
		{msgjson.AdaptorSetupInitRoute, &msgjson.AdaptorSetupInit{}, "EventInitSetup"},
		{msgjson.AdaptorRefundPresignedRoute, &msgjson.AdaptorRefundPresigned{}, "EventPresigned"},
		{msgjson.AdaptorLockedRoute, &msgjson.AdaptorLocked{}, "EventLocked"},
		{msgjson.AdaptorXmrLockedRoute, &msgjson.AdaptorXmrLocked{}, "EventXmrLocked"},
		{msgjson.AdaptorSpendPresigRoute, &msgjson.AdaptorSpendPresig{}, "EventSpendPresig"},
		{msgjson.AdaptorSpendBroadcastRoute, &msgjson.AdaptorSpendBroadcast{}, "EventSpendBroadcast"},
		{msgjson.AdaptorRefundBroadcastRoute, &msgjson.AdaptorRefundBroadcast{}, "EventRefundBroadcast"},
		{msgjson.AdaptorCoopRefundRoute, &msgjson.AdaptorCoopRefund{}, "EventCoopRefund"},
		{msgjson.AdaptorPunishRoute, &msgjson.AdaptorPunish{}, "EventPunish"},
	}
	for _, tc := range tests {
		t.Run(tc.route, func(t *testing.T) {
			evt, err := adaptorRouteToEvent(tc.route, tc.payload)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if got := evtKind(evt); got != tc.kind {
				t.Fatalf("evt kind=%s want %s", got, tc.kind)
			}
		})
	}
	if _, err := adaptorRouteToEvent("adaptor_nonsense", nil); err == nil {
		t.Fatal("expected error for unknown route")
	}
	// Type assertion failures.
	if _, err := adaptorRouteToEvent(msgjson.AdaptorSetupPartRoute, "not a part setup"); err == nil {
		t.Fatal("expected error for wrong payload type")
	}
}

func evtKind(e adaptor.Event) string {
	switch e.(type) {
	case adaptor.EventPartSetup:
		return "EventPartSetup"
	case adaptor.EventInitSetup:
		return "EventInitSetup"
	case adaptor.EventPresigned:
		return "EventPresigned"
	case adaptor.EventLocked:
		return "EventLocked"
	case adaptor.EventXmrLocked:
		return "EventXmrLocked"
	case adaptor.EventSpendPresig:
		return "EventSpendPresig"
	case adaptor.EventSpendBroadcast:
		return "EventSpendBroadcast"
	case adaptor.EventRefundBroadcast:
		return "EventRefundBroadcast"
	case adaptor.EventCoopRefund:
		return "EventCoopRefund"
	case adaptor.EventPunish:
		return "EventPunish"
	}
	return "unknown"
}
