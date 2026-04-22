package core

import (
	"errors"
	"testing"

	"decred.org/dcrdex/client/core/adaptorswap"
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
