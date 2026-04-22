package swap

import (
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/internal/adaptorsigs"
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
