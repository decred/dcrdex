// Bridge between server/swap and server/swap/adaptor. Owns per-match
// Coordinator lifecycle; maps inbound msgjson Adaptor* routes to
// server/swap/adaptor.Event values; supplies a PeerRouter
// implementation that dispatches outbound messages through the
// server's comms layer.
//
// Does not modify the existing HTLC coordinator in swap.go. The
// server-side Swapper picks up adaptor swaps by routing messages
// through (AdaptorCoordinators).Handle when the match's market has
// SwapType == dex.SwapTypeAdaptor.

package swap

import (
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/swap/adaptor"
)

// AdaptorCoordinators is a per-match pool of server-side adaptor
// swap coordinators. Safe for concurrent use.
type AdaptorCoordinators struct {
	mu sync.Mutex
	m  map[order.MatchID]*adaptor.Coordinator

	cfgTmpl adaptor.Config // template; cfgTmpl.MatchID/OrderID are
	// overwritten per swap.
}

// NewAdaptorCoordinators returns an empty pool. cfgTmpl carries the
// operator-level defaults (timeouts, adapters, reporter, persister)
// that every per-match coordinator inherits.
func NewAdaptorCoordinators(cfgTmpl adaptor.Config) *AdaptorCoordinators {
	return &AdaptorCoordinators{
		m:       make(map[order.MatchID]*adaptor.Coordinator),
		cfgTmpl: cfgTmpl,
	}
}

// Start creates and registers a coordinator for a newly matched
// order. Returns an error if a coordinator already exists for the
// match.
func (cc *AdaptorCoordinators) Start(matchID, orderID order.MatchID,
	scriptableAsset, nonScriptAsset uint32, lockBlocks uint32) (*adaptor.Coordinator, error) {

	cc.mu.Lock()
	defer cc.mu.Unlock()
	if _, ok := cc.m[matchID]; ok {
		return nil, fmt.Errorf("coordinator already exists for match %s", matchID)
	}
	cfg := cc.cfgTmpl
	copy(cfg.MatchID[:], matchID[:])
	copy(cfg.OrderID[:], orderID[:])
	cfg.ScriptableAsset = scriptableAsset
	cfg.NonScriptAsset = nonScriptAsset
	cfg.LockBlocks = lockBlocks

	c, err := adaptor.NewCoordinator(&cfg)
	if err != nil {
		return nil, err
	}
	cc.m[matchID] = c
	return c, nil
}

// Handle routes an inbound Adaptor* message payload to the
// coordinator for the given match. Returns an error if the match is
// unknown; informational routes (none on the server side; every
// route carries an event) never return the informational sentinel.
func (cc *AdaptorCoordinators) Handle(route string, matchID order.MatchID, payload any) error {
	cc.mu.Lock()
	c, ok := cc.m[matchID]
	cc.mu.Unlock()
	if !ok {
		return fmt.Errorf("no coordinator for match %s", matchID)
	}
	evt, err := adaptorRouteToEvent(route, payload)
	if err != nil {
		return err
	}
	return c.Handle(evt)
}

// Dispatch feeds a non-message event (chain observation, timeout)
// into the coordinator for the match.
func (cc *AdaptorCoordinators) Dispatch(matchID order.MatchID, evt adaptor.Event) error {
	cc.mu.Lock()
	c, ok := cc.m[matchID]
	cc.mu.Unlock()
	if !ok {
		return fmt.Errorf("no coordinator for match %s", matchID)
	}
	return c.Handle(evt)
}

// Stop removes and tears down a coordinator, typically on match
// completion or cancellation.
func (cc *AdaptorCoordinators) Stop(matchID order.MatchID) {
	cc.mu.Lock()
	delete(cc.m, matchID)
	cc.mu.Unlock()
}

// Coordinator exposes the raw coordinator for a match; used by
// tests and for admin endpoints.
func (cc *AdaptorCoordinators) Coordinator(matchID order.MatchID) *adaptor.Coordinator {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.m[matchID]
}

// adaptorRouteToEvent maps the server-inbound msgjson Adaptor*
// routes to coordinator events. The server only consumes peer-
// originated messages on these routes; AdaptorLocked / AdaptorXmr
// Locked / AdaptorSpendPresig / etc. are audit triggers that the
// coordinator routes onward to the other party.
func adaptorRouteToEvent(route string, payload any) (adaptor.Event, error) {
	switch route {
	case msgjson.AdaptorSetupPartRoute:
		m, ok := payload.(*msgjson.AdaptorSetupPart)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventPartSetup{Msg: m}, nil
	case msgjson.AdaptorSetupInitRoute:
		m, ok := payload.(*msgjson.AdaptorSetupInit)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventInitSetup{Msg: m}, nil
	case msgjson.AdaptorRefundPresignedRoute:
		m, ok := payload.(*msgjson.AdaptorRefundPresigned)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventPresigned{Msg: m}, nil
	case msgjson.AdaptorLockedRoute:
		m, ok := payload.(*msgjson.AdaptorLocked)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventLocked{Msg: m}, nil
	case msgjson.AdaptorXmrLockedRoute:
		m, ok := payload.(*msgjson.AdaptorXmrLocked)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventXmrLocked{Msg: m}, nil
	case msgjson.AdaptorSpendPresigRoute:
		m, ok := payload.(*msgjson.AdaptorSpendPresig)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventSpendPresig{Msg: m}, nil
	case msgjson.AdaptorSpendBroadcastRoute:
		m, ok := payload.(*msgjson.AdaptorSpendBroadcast)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventSpendBroadcast{Msg: m}, nil
	case msgjson.AdaptorRefundBroadcastRoute:
		m, ok := payload.(*msgjson.AdaptorRefundBroadcast)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventRefundBroadcast{Msg: m}, nil
	case msgjson.AdaptorCoopRefundRoute:
		m, ok := payload.(*msgjson.AdaptorCoopRefund)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventCoopRefund{Msg: m}, nil
	case msgjson.AdaptorPunishRoute:
		m, ok := payload.(*msgjson.AdaptorPunish)
		if !ok {
			return nil, fmt.Errorf("payload type %T for route %s", payload, route)
		}
		return adaptor.EventPunish{Msg: m}, nil
	}
	return nil, fmt.Errorf("unknown adaptor route %q", route)
}

// ----- PeerRouter interface implementations -----

// RouterFunc adapts a function to adaptor.PeerRouter. Useful for
// wiring the server's comms layer in.
type RouterFunc func(matchID order.MatchID, role adaptor.Role, route string, payload any) error

func (f RouterFunc) SendTo(matchID order.MatchID, role adaptor.Role, route string, payload any) error {
	return f(matchID, role, route, payload)
}

// NoopReporter is an OutcomeReporter that drops reports. Useful for
// tests and for operators running without a reputation backend.
type NoopReporter struct{}

func (NoopReporter) Report(order.MatchID, adaptor.Role, adaptor.Outcome) error { return nil }

// NoopPersister discards state snapshots.
type NoopPersister struct{}

func (NoopPersister) Save(order.MatchID, *adaptor.State) error   { return nil }
func (NoopPersister) Load(order.MatchID) (*adaptor.State, error) { return nil, nil }

// sanity: errors import silences "imported and not used" if no
// package-level error is declared.
var _ = errors.New
