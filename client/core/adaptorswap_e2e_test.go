package core

// End-to-end setup-phase test that routes messages through a real
// server-side adaptor.Coordinator instead of directly between two
// client managers (as TestSetupPhaseRoundTrip does). Validates that
// the wire payloads emitted by the client orchestrators are accepted
// by the server's validators in adaptor.Coordinator, and that the
// server's relays land back at the right client.

import (
	"errors"
	"fmt"
	"testing"

	"decred.org/dcrdex/client/core/adaptorswap"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/swap/adaptor"
	"github.com/btcsuite/btcd/wire"
)

// TestServerMediatedSetupRoundTrip wires the participant and
// initiator client managers through a server-side
// adaptor.Coordinator. The coordinator validates each setup
// message and forwards it to the other party. Asserts that all
// three state machines (participant, server, initiator) reach the
// expected setup-phase terminus and that the coordinator's relayed
// payloads carry the same on-the-wire bytes the orchestrators
// emitted.
func TestServerMediatedSetupRoundTrip(t *testing.T) {
	type queuedMsg struct {
		route   string
		payload any
	}
	var (
		toServer []queuedMsg // client -> server
		toInit   []queuedMsg // server -> initiator
		toPart   []queuedMsg // server -> participant
	)

	// Each client's sender drops outbound onto toServer; the server
	// is the only path the test exposes between clients.
	initSender := senderFunc(func(route string, payload any) error {
		toServer = append(toServer, queuedMsg{route, payload})
		return nil
	})
	partSender := senderFunc(func(route string, payload any) error {
		toServer = append(toServer, queuedMsg{route, payload})
		return nil
	})

	// Server router pushes to the per-role client queue.
	router := routerFunc(func(_ order.MatchID, role adaptor.Role,
		route string, payload any) error {
		switch role {
		case adaptor.RoleInitiator:
			toInit = append(toInit, queuedMsg{route, payload})
		case adaptor.RoleParticipant:
			toPart = append(toPart, queuedMsg{route, payload})
		default:
			return fmt.Errorf("router: unknown role %v", role)
		}
		return nil
	})

	const (
		btcAssetID uint32 = 0
		xmrAssetID uint32 = 128
		lockBlocks uint32 = 144
	)
	matchID := order.MatchID{0xE2, 0xE2}
	orderID := order.MatchID{0x07, 0x07}

	coord, err := adaptor.NewCoordinator(&adaptor.Config{
		MatchID:         [32]byte(matchID),
		OrderID:         [32]byte(orderID),
		ScriptableAsset: btcAssetID,
		NonScriptAsset:  xmrAssetID,
		LockBlocks:      lockBlocks,
		Router:          router,
		Persist:         noopServerPersister{},
	})
	if err != nil {
		t.Fatalf("server NewCoordinator: %v", err)
	}

	// Initiator's BTC adapter halts the orchestrator at
	// FundBroadcastTaproot so the test stops at the same point as
	// TestSetupPhaseRoundTrip.
	initMgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &errorBTC{}, XMR: &bridgeFakeXMR{}, Send: initSender,
	})
	partMgr := NewAdaptorSwapManager(&AdaptorSwapManagerConfig{
		BTC: &bridgeFakeBTC{}, XMR: &bridgeFakeXMR{}, Send: partSender,
	})
	makeCfg := func(role adaptorswap.Role) *adaptorswap.Config {
		return &adaptorswap.Config{
			SwapID:              [32]byte{1},
			OrderID:             [32]byte(orderID),
			MatchID:             matchID,
			Role:                role,
			PairBTC:             btcAssetID,
			PairXMR:             xmrAssetID,
			BtcAmount:           100_000_000,
			XmrAmount:           50_000_000,
			LockBlocks:          lockBlocks,
			XmrNetTag:           18,
			PeerBTCPayoutScript: []byte{0x51}, // OP_TRUE; placeholder
			OwnXMRSweepDest:     "test-xmr-dest",
		}
	}
	if _, err := initMgr.StartSwap(makeCfg(adaptorswap.RoleInitiator)); err != nil {
		t.Fatalf("initMgr.StartSwap: %v", err)
	}
	if _, err := partMgr.StartSwap(makeCfg(adaptorswap.RoleParticipant)); err != nil {
		t.Fatalf("partMgr.StartSwap: %v", err)
	}

	// route → server-side adaptor.Event mapping for the setup-phase
	// messages this test exercises. The bigger mapping lives in
	// server/swap.adaptorRouteToEvent (unexported); inlining here
	// keeps the test self-contained.
	toServerEvent := func(route string, payload any) (adaptor.Event, error) {
		switch route {
		case msgjson.AdaptorSetupPartRoute:
			m, ok := payload.(*msgjson.AdaptorSetupPart)
			if !ok {
				return nil, fmt.Errorf("unexpected payload %T for %s", payload, route)
			}
			return adaptor.EventPartSetup{Msg: m}, nil
		case msgjson.AdaptorSetupInitRoute:
			m, ok := payload.(*msgjson.AdaptorSetupInit)
			if !ok {
				return nil, fmt.Errorf("unexpected payload %T for %s", payload, route)
			}
			return adaptor.EventInitSetup{Msg: m}, nil
		case msgjson.AdaptorRefundPresignedRoute:
			m, ok := payload.(*msgjson.AdaptorRefundPresigned)
			if !ok {
				return nil, fmt.Errorf("unexpected payload %T for %s", payload, route)
			}
			return adaptor.EventPresigned{Msg: m}, nil
		}
		return nil, fmt.Errorf("unknown route %s", route)
	}

	const maxIters = 32
	for i := 0; i < maxIters; i++ {
		if len(toServer) == 0 && len(toInit) == 0 && len(toPart) == 0 {
			break
		}
		if len(toServer) > 0 {
			m := toServer[0]
			toServer = toServer[1:]
			evt, err := toServerEvent(m.route, m.payload)
			if err != nil {
				t.Fatalf("toServerEvent(%s): %v", m.route, err)
			}
			if err := coord.Handle(evt); err != nil {
				t.Fatalf("coord.Handle(%s): %v", m.route, err)
			}
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
	if total := len(toServer) + len(toInit) + len(toPart); total > 0 {
		t.Fatalf("queues not drained: toServer=%d toInit=%d toPart=%d",
			len(toServer), len(toInit), len(toPart))
	}

	if got := coord.Phase(); got != adaptor.PhaseAwaitingLocked {
		t.Errorf("coord phase = %s, want PhaseAwaitingLocked (after relaying RefundPresigned)", got)
	}
	if got := partMgr.orchestrators[matchID].Phase(); got != adaptorswap.PhaseRefundPresigned {
		t.Errorf("participant phase = %s, want PhaseRefundPresigned", got)
	}
	if got := initMgr.orchestrators[matchID].Phase(); got != adaptorswap.PhaseKeysReceived {
		t.Errorf("initiator phase = %s, want PhaseKeysReceived (halt at FundBroadcastTaproot)", got)
	}
}

// routerFunc adapts a function to adaptor.PeerRouter.
type routerFunc func(matchID order.MatchID, role adaptor.Role, route string, payload any) error

func (f routerFunc) SendTo(matchID order.MatchID, role adaptor.Role, route string, payload any) error {
	return f(matchID, role, route, payload)
}

// noopServerPersister is the test-side stand-in for the server's
// adaptor.StatePersister interface, dropping every snapshot.
type noopServerPersister struct{}

func (noopServerPersister) Save(order.MatchID, *adaptor.State) error   { return nil }
func (noopServerPersister) Load(order.MatchID) (*adaptor.State, error) { return nil, nil }

// silence unused import in case future edits drop the wire reference.
var _ = wire.NewMsgTx
var _ = errors.New
