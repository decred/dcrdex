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
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/swap/adaptor"
)

// AdaptorCoordinators is a per-match pool of server-side adaptor
// swap coordinators. Safe for concurrent use.
type AdaptorCoordinators struct {
	mu    sync.Mutex
	m     map[order.MatchID]*adaptor.Coordinator
	users map[order.MatchID]map[adaptor.Role]account.AccountID

	cfgTmpl adaptor.Config // template; cfgTmpl.MatchID/OrderID are
	// overwritten per swap.
}

// NewAdaptorCoordinators returns an empty pool. cfgTmpl carries the
// operator-level defaults (timeouts, adapters, reporter, persister)
// that every per-match coordinator inherits.
func NewAdaptorCoordinators(cfgTmpl adaptor.Config) *AdaptorCoordinators {
	return &AdaptorCoordinators{
		m:       make(map[order.MatchID]*adaptor.Coordinator),
		users:   make(map[order.MatchID]map[adaptor.Role]account.AccountID),
		cfgTmpl: cfgTmpl,
	}
}

// Start creates and registers a coordinator for a newly matched
// order. Returns an error if a coordinator already exists for the
// match.
func (cc *AdaptorCoordinators) Start(matchID, orderID order.MatchID,
	scriptableAsset, nonScriptAsset uint32, lockBlocks uint32,
	initiator, participant account.AccountID) (*adaptor.Coordinator, error) {

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
	cc.users[matchID] = map[adaptor.Role]account.AccountID{
		adaptor.RoleInitiator:   initiator,
		adaptor.RoleParticipant: participant,
	}
	return c, nil
}

// UserFor returns the user account for the given match and role, or
// false if the match is unknown. Used by the PeerRouter to route
// outbound coordinator messages to the right counterparty.
func (cc *AdaptorCoordinators) UserFor(matchID order.MatchID, role adaptor.Role) (account.AccountID, bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	roles, ok := cc.users[matchID]
	if !ok {
		return account.AccountID{}, false
	}
	user, ok := roles[role]
	return user, ok
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
	delete(cc.users, matchID)
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

// HandleAdaptor routes an inbound msgjson Adaptor* message through
// the configured AdaptorCoordinators pool. Returns an error if
// adaptor coordination is not configured for this Swapper.
//
// Intended to be called from server/comms's adaptor-route handler:
//
//	dex.AuthMgr.Route(msgjson.AdaptorSetupPartRoute, func(...) {
//	    swapper.HandleAdaptor(msgjson.AdaptorSetupPartRoute, matchID, payload)
//	})
//
// HTLC routes continue to flow through the existing Swapper
// handlers; this method only fires for Adaptor* routes.
func (s *Swapper) HandleAdaptor(route string, matchID order.MatchID, payload any) error {
	if s.adaptorCoords == nil {
		return errors.New("adaptor swap coordination not configured")
	}
	return s.adaptorCoords.Handle(route, matchID, payload)
}

// StartAdaptorMatch creates an adaptor swap coordinator for a new
// match. Called from the match-creation path in Negotiate when the
// match's market has SwapType == dex.SwapTypeAdaptor.
//
// TODO: hook into Negotiate. The remaining call-site work is to
// have Negotiate consult its market config, dispatch
// adaptor-marked matches here, and skip the HTLC matchTracker
// setup for them.
func (s *Swapper) StartAdaptorMatch(matchID, orderID order.MatchID,
	scriptableAsset, nonScriptAsset uint32, lockBlocks uint32,
	initiator, participant account.AccountID) error {
	if s.adaptorCoords == nil {
		return errors.New("adaptor swap coordination not configured")
	}
	_, err := s.adaptorCoords.Start(matchID, orderID, scriptableAsset, nonScriptAsset, lockBlocks, initiator, participant)
	return err
}

// StopAdaptorMatch tears down the coordinator for a completed or
// cancelled adaptor swap match.
func (s *Swapper) StopAdaptorMatch(matchID order.MatchID) {
	if s.adaptorCoords == nil {
		return
	}
	s.adaptorCoords.Stop(matchID)
}

// handleAdaptorMsg is the AuthManager.Route handler registered for
// every adaptor_* route. It decodes the payload according to
// msg.Route, validates the user's signature against it, extracts
// the match ID, and dispatches to HandleAdaptor.
//
// All adaptor messages flow through this single function; the
// per-route decoding lives in adaptorMsgPayload to mirror the
// client-side decodeAdaptorMsg structure.
func (s *Swapper) handleAdaptorMsg(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	if s.adaptorCoords == nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: "adaptor swap coordination not configured",
		}
	}
	payload, matchID, err := adaptorMsgPayload(msg)
	if err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCParseError,
			Message: fmt.Sprintf("decode %s: %v", msg.Route, err),
		}
	}
	if rpcErr := s.authUser(user, payload); rpcErr != nil {
		return rpcErr
	}
	if err := s.HandleAdaptor(msg.Route, matchID, payload); err != nil {
		return &msgjson.Error{
			Code:    msgjson.RPCInternalError,
			Message: fmt.Sprintf("handle %s match %s: %v", msg.Route, matchID, err),
		}
	}
	return nil
}

// adaptorMsgPayload decodes msg according to msg.Route into the
// concrete msgjson Adaptor* type and returns it along with the
// match ID. Mirrors client/core's decodeAdaptorMsg; kept locally
// so the server doesn't import client/core.
func adaptorMsgPayload(msg *msgjson.Message) (msgjson.Signable, order.MatchID, error) {
	var matchBytes []byte
	var payload msgjson.Signable
	switch msg.Route {
	case msgjson.AdaptorSetupPartRoute:
		p := new(msgjson.AdaptorSetupPart)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSetupInitRoute:
		p := new(msgjson.AdaptorSetupInit)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorRefundPresignedRoute:
		p := new(msgjson.AdaptorRefundPresigned)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorLockedRoute:
		p := new(msgjson.AdaptorLocked)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorXmrLockedRoute:
		p := new(msgjson.AdaptorXmrLocked)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSpendPresigRoute:
		p := new(msgjson.AdaptorSpendPresig)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorSpendBroadcastRoute:
		p := new(msgjson.AdaptorSpendBroadcast)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorRefundBroadcastRoute:
		p := new(msgjson.AdaptorRefundBroadcast)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorCoopRefundRoute:
		p := new(msgjson.AdaptorCoopRefund)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	case msgjson.AdaptorPunishRoute:
		p := new(msgjson.AdaptorPunish)
		if err := msg.Unmarshal(p); err != nil {
			return nil, order.MatchID{}, err
		}
		matchBytes, payload = p.MatchID, p
	default:
		return nil, order.MatchID{}, fmt.Errorf("unknown adaptor route %q", msg.Route)
	}
	if len(matchBytes) != order.MatchIDSize {
		return nil, order.MatchID{}, fmt.Errorf("matchid length %d, want %d",
			len(matchBytes), order.MatchIDSize)
	}
	var matchID order.MatchID
	copy(matchID[:], matchBytes)
	return payload, matchID, nil
}

// adaptorRoutes is the list of routes handleAdaptorMsg is
// registered for. Used by NewSwapper to wire them all in one place.
var adaptorRoutes = []string{
	msgjson.AdaptorSetupPartRoute,
	msgjson.AdaptorSetupInitRoute,
	msgjson.AdaptorRefundPresignedRoute,
	msgjson.AdaptorLockedRoute,
	msgjson.AdaptorXmrLockedRoute,
	msgjson.AdaptorSpendPresigRoute,
	msgjson.AdaptorSpendBroadcastRoute,
	msgjson.AdaptorRefundBroadcastRoute,
	msgjson.AdaptorCoopRefundRoute,
	msgjson.AdaptorPunishRoute,
}

// NegotiateAdaptor is the adaptor-swap analogue of Negotiate. It is
// invoked by the Market when its market config has SwapType ==
// dex.SwapTypeAdaptor. The matchSets are all from the same adaptor
// market, so they share scriptableAsset and lockBlocks.
//
// Per match: locks the order coins, persists the match record, and
// either cancels (cancel-order matches) or starts an adaptor-swap
// coordinator. The standard 'match' notification is still emitted to
// both parties so the existing client-side ack machinery and order-
// status flow work unchanged; the adaptor-specific setup messages
// flow on the separate Adaptor* routes once the coordinator is
// running.
func (s *Swapper) NegotiateAdaptor(matchSets []*order.MatchSet,
	scriptableAsset, lockBlocks uint32) {

	s.handlerMtx.RLock()
	defer s.handlerMtx.RUnlock()
	if s.stop {
		log.Errorf("NegotiateAdaptor called on stopped swapper. Matches lost!")
		return
	}
	if s.adaptorCoords == nil {
		log.Errorf("NegotiateAdaptor called but no AdaptorCoordinators configured. Matches lost!")
		return
	}

	swapOrders := make([]order.Order, 0, 2*len(matchSets))
	for _, set := range matchSets {
		if set.Taker.Type() == order.CancelOrderType {
			continue
		}
		swapOrders = append(swapOrders, set.Taker)
		for _, maker := range set.Makers {
			swapOrders = append(swapOrders, maker)
		}
	}
	s.LockOrdersCoins(swapOrders)

	matches := readMatches(matchSets)

	for _, match := range matches {
		if err := s.storage.InsertMatch(match.Match); err != nil {
			log.Errorf("InsertMatch (match id=%v) failed: %v", match.ID(), err)
			return
		}
	}

	userMatches := make(map[account.AccountID][]*messageAcker)
	addUserMatch := func(acker *messageAcker) {
		s.authMgr.Sign(acker.params)
		userMatches[acker.user] = append(userMatches[acker.user], acker)
	}

	for _, match := range matches {
		if match.Taker.Type() == order.CancelOrderType {
			if err := s.storage.CancelOrder(match.Maker); err != nil {
				log.Errorf("Failed to cancel order %v", match.Maker)
				return
			}
		} else {
			// Pick the non-scriptable asset from the market base/quote.
			base, quote := match.Maker.BaseAsset, match.Maker.QuoteAsset
			nonScript := quote
			if scriptableAsset == quote {
				nonScript = base
			}
			matchID := match.ID()
			orderID := match.Maker.ID()
			var mid, oid order.MatchID
			copy(mid[:], matchID[:])
			copy(oid[:], orderID[:])
			// On adaptor markets the maker is the scriptable-side
			// holder (initiator), the taker is the non-scriptable
			// holder (participant). Order-router Option-1 enforces
			// this at intake.
			if _, err := s.adaptorCoords.Start(mid, oid, scriptableAsset, nonScript, lockBlocks,
				match.Maker.User(), match.Taker.User()); err != nil {
				log.Errorf("AdaptorCoordinators.Start (match %s): %v", matchID, err)
				continue
			}
		}

		makerMsg, takerMsg := matchNotifications(match)
		addUserMatch(&messageAcker{
			user:    match.Maker.User(),
			match:   match,
			params:  makerMsg,
			isMaker: true,
		})
		addUserMatch(&messageAcker{
			user:    match.Taker.User(),
			match:   match,
			params:  takerMsg,
			isMaker: false,
		})
	}

	for user, ms := range userMatches {
		msgs := make([]msgjson.Signable, 0, len(ms))
		for _, m := range ms {
			msgs = append(msgs, m.params)
		}
		req, err := msgjson.NewRequest(comms.NextID(), msgjson.MatchRoute, msgs)
		if err != nil {
			log.Errorf("error creating adaptor match notification request: %v", err)
			continue
		}
		u, m := user, ms
		log.Debugf("NegotiateAdaptor: sending 'match' ack request to user %v for %d matches",
			u, len(m))
		if err := s.authMgr.Request(u, req, func(_ comms.Link, resp *msgjson.Message) {
			s.processMatchAcks(u, resp, m)
		}); err != nil {
			log.Infof("Failed to send %v request to %v. The match will be returned in the connect response.",
				req.Route, u)
		}
	}
}
