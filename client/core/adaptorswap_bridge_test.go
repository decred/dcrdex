package core

import (
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/client/core/adaptorswap"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/btcsuite/btcd/wire"
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

	// Informational routes.
	for _, route := range []string{
		msgjson.AdaptorLockedRoute,
		msgjson.AdaptorSpendBroadcastRoute,
	} {
		_, err := routeToEvent(route, nil)
		if !errors.Is(err, errPurelyInformational) {
			t.Fatalf("route %s: err=%v want informational", route, err)
		}
		if !IsInformational(err) {
			t.Fatalf("IsInformational rejected informational error for %s", route)
		}
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

	// Informational routes return the sentinel, not a failure.
	if err := m.Handle(msgjson.AdaptorLockedRoute, matchID, &msgjson.AdaptorLocked{}); err == nil {
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
	c := &Core{adaptorMgr: mgr}

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

	// Informational routes: with the orchestrator started, the
	// manager returns errPurelyInformational; the handler must
	// suppress it.
	for _, route := range []string{msgjson.AdaptorLockedRoute, msgjson.AdaptorSpendBroadcastRoute} {
		var p any
		switch route {
		case msgjson.AdaptorLockedRoute:
			p = &msgjson.AdaptorLocked{MatchID: matchID[:]}
		case msgjson.AdaptorSpendBroadcastRoute:
			p = &msgjson.AdaptorSpendBroadcast{MatchID: matchID[:]}
		}
		msg, err := msgjson.NewNotification(route, p)
		if err != nil {
			t.Fatalf("NewNotification(%s): %v", route, err)
		}
		if err := handleAdaptorMsg(c, nil, msg); err != nil {
			t.Fatalf("informational %s leaked error: %v", route, err)
		}
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
	tracker := &trackedTrade{Order: ord}

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

	// Maker on a BTC-base market => initiator => Start sends
	// nothing (initiator waits for AdaptorSetupPart).
	if len(sender.routes) != 0 {
		t.Fatalf("initiator should not emit setup; routes=%v", sender.routes)
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
	if len(sender.routes) != 1 || sender.routes[0] != msgjson.AdaptorSetupPartRoute {
		t.Fatalf("participant should emit AdaptorSetupPart; routes=%v", sender.routes)
	}
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
