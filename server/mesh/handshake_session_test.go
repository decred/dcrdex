// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
)

func pendingHandshakeCount(h *handshakeSessions) int {
	h.pendingMtx.Lock()
	defer h.pendingMtx.Unlock()
	return len(h.pending)
}

func getPendingHandshake(h *handshakeSessions, connID uint64) (*pendingHandshakeSession, bool) {
	h.pendingMtx.Lock()
	defer h.pendingMtx.Unlock()
	session, found := h.pending[connID]
	return session, found
}

func newSessionTestNode(t testing.TB, state handshakeServiceState) *node {
	t.Helper()

	svc, _ := newTestHandshakeService(t, state)
	node := newRouteHandshakeNode(svc)
	node.handshakes.pendingTimeout = time.Hour
	return node
}

func newSessionPending(t testing.TB, spec helloSpec) *handshakeResult {
	t.Helper()

	return &handshakeResult{
		peerHello:  buildSignedHello(t, spec),
		progress:   progressPeerAhead,
		clientHost: spec.clientHost,
		clientCert: append([]byte(nil), spec.clientCert...),
	}
}

func expirePendingHandshakeForTest(t testing.TB, h *handshakeSessions, conn *tRouteLink, session *pendingHandshakeSession) bool {
	t.Helper()

	if h.clearPending(conn.ID(), session) {
		conn.Disconnect()
		return true
	}
	return false
}

func TestHandshakeSessionsHandleHelloResolvedAdoption(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	link := newTRouteLink(1201)
	node := newSessionTestNode(t, fix.state(roleMaster))
	captured, stopResolver := startHandshakeResolver(node, nil)
	defer stopResolver()

	hello := fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier)
	rpcErr := node.handshakes.handleHello(context.Background(), link, 1, hello)
	requireNoRPCError(t, rpcErr)

	decodeHelloResponse(t, requireSent(t, link, 1)[0])
	if got := link.authorizeCount(); got != 1 {
		t.Fatalf("authorize calls = %d, want 1", got)
	}
	if !reflect.DeepEqual(link.operations(), []string{"authorize", "send"}) {
		t.Fatalf("operations = %v, want authorize then send", link.operations())
	}
	requireHandshakeCapture(t, captured, link, peerHandshakeCapture(roleSlave, progressEqual, fix.peerEqualFrontier))
	if got := pendingHandshakeCount(node.handshakes); got != 0 {
		t.Fatalf("pending handshakes = %d, want 0", got)
	}
}

func TestHandshakeSessionsHandleHelloApplyFailure(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	forkFrontier := &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)}

	tests := []struct {
		name         string
		peerRole     helloRole
		entries      []*db.EventLogEntry
		frontier     *db.EventLogPosition
		wantSent     int
		wantAncestor bool
		wantProgress progressState
		wantOps      []string
	}{
		{
			name:         "equal: apply fails before send",
			peerRole:     roleSlave,
			frontier:     fix.peerEqualFrontier,
			wantSent:     0,
			wantProgress: progressEqual,
			wantOps:      []string{"authorize", "disconnect"},
		},
		{
			name:         "fork: send before apply",
			peerRole:     roleMaster,
			entries:      []*db.EventLogEntry{{Seq: 8, Kind: "test", TipHash: testTipHash(9)}},
			frontier:     forkFrontier,
			wantSent:     1,
			wantProgress: progressDiverged,
			wantOps:      []string{"authorize", "send", "disconnect"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := newTRouteLink(1202)
			state := fix.state(roleMaster)
			state.eventEntries = tt.entries
			node := newSessionTestNode(t, state)
			captured, stopResolver := startHandshakeResolver(node, errors.New("apply failed"))
			defer stopResolver()

			hello := fix.hello(t, routePeerNodeID, tt.peerRole, tt.frontier)
			rpcErr := node.handshakes.handleHello(context.Background(), link, 1, hello)
			requireNoRPCError(t, rpcErr)

			sent := requireSent(t, link, tt.wantSent)
			if tt.wantSent > 0 {
				resp := decodeHelloResponse(t, sent[0])
				if resp.Ancestor != tt.wantAncestor {
					t.Fatalf("response ancestor = %v, want %v", resp.Ancestor, tt.wantAncestor)
				}
			}
			if got := link.authorizeCount(); got != 1 {
				t.Fatalf("authorize calls = %d, want 1", got)
			}
			if !reflect.DeepEqual(link.operations(), tt.wantOps) {
				t.Fatalf("operations = %v, want %v", link.operations(), tt.wantOps)
			}
			requireHandshakeCapture(t, captured, link, peerHandshakeCapture(tt.peerRole, tt.wantProgress, tt.frontier))
			if got := pendingHandshakeCount(node.handshakes); got != 0 {
				t.Fatalf("pending handshakes = %d, want 0", got)
			}
		})
	}
}

func decodeErrorResponse(t testing.TB, msg *msgjson.Message) *msgjson.Error {
	t.Helper()

	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected an error response")
	}
	return resp.Error
}

func TestHandshakeSessionsHandleHelloNotAdoptedAnswersBeforeDisconnect(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	link := newTRouteLink(1208)
	node := newSessionTestNode(t, fix.state(roleMaster))
	captured, stopResolver := startHandshakeResolver(node, fmt.Errorf("handshake: %w", errConnNotAdopted))
	defer stopResolver()

	hello := fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier)
	rpcErr := node.handshakes.handleHello(context.Background(), link, 1, hello)
	requireNoRPCError(t, rpcErr)

	sent := requireSent(t, link, 1)
	resp := decodeHelloResponse(t, sent[0])
	if !resp.NotAdopted {
		t.Fatal("hello response not marked NotAdopted")
	}
	if err := resp.Hello.verifySig(node.handshakes.svc.signer); err != nil {
		t.Fatalf("NotAdopted response hello signature: %v", err)
	}
	if !reflect.DeepEqual(link.operations(), []string{"authorize", "send", "disconnect"}) {
		t.Fatalf("operations = %v, want authorize, send, disconnect", link.operations())
	}
	requireHandshakeCapture(t, captured, link, peerHandshakeCapture(roleSlave, progressEqual, fix.peerEqualFrontier))
	if got := pendingHandshakeCount(node.handshakes); got != 0 {
		t.Fatalf("pending handshakes = %d, want 0", got)
	}
}

func TestHandshakeSessionsHandleHelloUnresolvedPendingLifecycle(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	link := newTRouteLink(1203)
	node := newSessionTestNode(t, fix.state(roleSlave))

	hello := fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier)
	rpcErr := node.handshakes.handleHello(context.Background(), link, 1, hello)
	requireNoRPCError(t, rpcErr)

	sent := requireSent(t, link, 1)
	if got := decodeHelloResponse(t, sent[0]).Ancestor; got {
		// A behind responder cannot resolve the fork check, so it must not
		// claim the initiator is a prefix; the initiator's decision completes
		// the handshake.
		t.Fatalf("hello response ancestor = true, want false")
	}
	if got := link.authorizeCount(); got != 1 {
		t.Fatalf("authorize calls = %d, want 1", got)
	}
	if !reflect.DeepEqual(link.operations(), []string{"authorize", "send"}) {
		t.Fatalf("operations = %v, want authorize then send", link.operations())
	}
	session, found := getPendingHandshake(node.handshakes, link.ID())
	if !found {
		t.Fatalf("missing pending handshake")
	}
	if session.pending.peerHello != hello {
		t.Fatalf("pending hello pointer changed")
	}

	link.Disconnect()
	waitForCondition(t, func() bool {
		return pendingHandshakeCount(node.handshakes) == 0
	}, "pending cleanup after disconnect")
}

func TestHandshakeSessionsHandleHelloResponseFailureCreatesNoPending(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	tests := []struct {
		name  string
		reqID uint64
		err   error
		code  int
		msg   string
	}{
		{
			name:  "send failure",
			reqID: 1,
			err:   errors.New("send failed"),
			code:  msgjson.RPCInternal,
			msg:   "failed to send mesh hello response",
		},
		{
			name:  "encode failure",
			reqID: 0,
			code:  msgjson.RPCInternal,
			msg:   "failed to encode mesh hello response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := newTRouteLink(1204)
			link.sendErr = tt.err
			node := newSessionTestNode(t, fix.state(roleSlave))
			node.handshakes.pendingTimeout = 10 * time.Millisecond
			hello := fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier)

			rpcErr := node.handshakes.handleHello(context.Background(), link, tt.reqID, hello)
			requireRPCOutcome(t, rpcErr, tt.code, tt.msg)
			if got := link.authorizeCount(); got != 1 {
				t.Fatalf("authorize calls = %d, want 1", got)
			}
			if got := pendingHandshakeCount(node.handshakes); got != 0 {
				t.Fatalf("pending handshakes = %d, want 0", got)
			}
			time.Sleep(25 * time.Millisecond)
			if got := link.disconnectCount(); got != 0 {
				t.Fatalf("disconnects = %d, want 0", got)
			}
		})
	}
}

func TestHandshakeSessionsHandleDecision(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	peerHost, peerCert := "peer.example:7232", []byte{9, 8, 7}

	tests := []struct {
		name         string
		decision     *decisionMessage
		applyErr     error
		sendErr      error
		wantRPC      routeRPCExpectation
		wantSent     int
		wantSentCode int
		wantDisc     int
		wantCalled   bool
	}{
		{
			name:       "success sends ack",
			decision:   &decisionMessage{Ancestor: true},
			wantSent:   1,
			wantCalled: true,
		},
		{
			name:       "diverged decision acks before the halting apply",
			decision:   &decisionMessage{Ancestor: false},
			wantSent:   1,
			wantDisc:   1,
			wantCalled: true,
		},
		{
			name:       "diverged apply failure still acks before disconnect",
			decision:   &decisionMessage{Ancestor: false},
			applyErr:   errors.New("apply failed"),
			wantSent:   1,
			wantDisc:   1,
			wantCalled: true,
		},
		{
			name:       "non-diverged apply failure disconnects and sends no ack",
			decision:   &decisionMessage{Ancestor: true},
			applyErr:   errors.New("apply failed"),
			wantDisc:   1,
			wantCalled: true,
		},
		{
			name:         "not-adopted decision answers already connected before disconnect",
			decision:     &decisionMessage{Ancestor: true},
			applyErr:     fmt.Errorf("handshake: %w", errConnNotAdopted),
			wantSent:     1,
			wantSentCode: msgjson.MeshAlreadyConnectedError,
			wantDisc:     1,
			wantCalled:   true,
		},
		{
			name:       "ack send failure returns internal error after adoption",
			decision:   &decisionMessage{Ancestor: true},
			sendErr:    errors.New("send failed"),
			wantRPC:    routeRPCExpectation{code: msgjson.RPCInternal, msg: "failed to send mesh decision response"},
			wantCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := newTRouteLink(1205)
			link.sendErr = tt.sendErr
			node := newSessionTestNode(t, fix.state(roleSlave))
			captured, stopResolver := startHandshakeResolver(node, tt.applyErr)
			defer stopResolver()
			node.handshakes.addPending(link, newSessionPending(t, helloSpec{
				nodeID:     routePeerNodeID,
				role:       roleMaster,
				frontier:   fix.decisionFrontier,
				compat:     fix.compat,
				clientHost: peerHost,
				clientCert: peerCert,
			}))

			rpcErr := node.handshakes.handleDecision(context.Background(), link, 1, tt.decision)
			requireRPCOutcome(t, rpcErr, tt.wantRPC.code, tt.wantRPC.msg)
			sent := requireSent(t, link, tt.wantSent)
			if tt.wantSentCode != 0 {
				if got := decodeErrorResponse(t, sent[0]).Code; got != tt.wantSentCode {
					t.Fatalf("sent response code = %d, want %d", got, tt.wantSentCode)
				}
			}
			if tt.wantSent > 0 && tt.wantDisc > 0 {
				// A verdict-bearing reply must be queued before the
				// disconnect tears the transport down.
				if !reflect.DeepEqual(link.operations(), []string{"send", "disconnect"}) {
					t.Fatalf("operations = %v, want send then disconnect", link.operations())
				}
			}
			if got := link.disconnectCount(); got != tt.wantDisc {
				t.Fatalf("disconnects = %d, want %d", got, tt.wantDisc)
			}
			if got := pendingHandshakeCount(node.handshakes); got != 0 {
				t.Fatalf("pending handshakes = %d, want 0", got)
			}
			wantCapture := peerHandshakeCapture(roleMaster, progressPeerAhead, fix.decisionFrontier)
			if !tt.decision.Ancestor {
				wantCapture.progress = progressDiverged
			}
			requireHandshakeCapture(t, captured, link, wantCapture)
			if tt.wantCalled && (captured.clientHost != peerHost || !reflect.DeepEqual(captured.clientCert, peerCert)) {
				t.Fatalf("captured endpoint = %q/%x, want %q/%x", captured.clientHost, captured.clientCert, peerHost, peerCert)
			}
		})
	}
}

func TestHandshakeSessionsTimeoutDisconnects(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	link := newTRouteLink(1207)
	node := newSessionTestNode(t, fix.state(roleSlave))
	node.handshakes.pendingTimeout = 10 * time.Millisecond

	hello := fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier)
	rpcErr := node.handshakes.handleHello(context.Background(), link, 1, hello)
	requireNoRPCError(t, rpcErr)

	waitForCondition(t, func() bool {
		return pendingHandshakeCount(node.handshakes) == 0 && link.disconnectCount() == 1
	}, "pending timeout disconnect")

	rpcErr = node.handshakes.handleDecision(context.Background(), link, 2, &decisionMessage{Ancestor: true})
	requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "unexpected mesh decision")
}

func TestHandshakeSessionsTimeoutDisconnectRace(t *testing.T) {
	fix := newRouteHandshakeFixture(t)

	t.Run("disconnect cleanup wins", func(t *testing.T) {
		link := newTRouteLink(1210)
		node := newSessionTestNode(t, fix.state(roleSlave))
		session := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
			nodeID:   routePeerNodeID,
			role:     roleMaster,
			frontier: fix.decisionFrontier,
			compat:   fix.compat,
		}))

		link.Disconnect()
		waitForCondition(t, func() bool {
			return pendingHandshakeCount(node.handshakes) == 0
		}, "pending cleanup after disconnect")

		if expirePendingHandshakeForTest(t, node.handshakes, link, session) {
			t.Fatalf("stale timeout removed already-cleared pending session")
		}
		if got := link.disconnectCount(); got != 1 {
			t.Fatalf("disconnects = %d, want 1", got)
		}
	})

	t.Run("timeout cleanup wins", func(t *testing.T) {
		link := newTRouteLink(1211)
		node := newSessionTestNode(t, fix.state(roleSlave))
		session := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
			nodeID:   routePeerNodeID,
			role:     roleMaster,
			frontier: fix.decisionFrontier,
			compat:   fix.compat,
		}))

		if !expirePendingHandshakeForTest(t, node.handshakes, link, session) {
			t.Fatalf("timeout failed to clear pending session")
		}
		if got := pendingHandshakeCount(node.handshakes); got != 0 {
			t.Fatalf("pending handshakes = %d, want 0", got)
		}
		if got := link.disconnectCount(); got != 1 {
			t.Fatalf("disconnects = %d, want 1", got)
		}
		if node.handshakes.clearPending(link.ID(), session) {
			t.Fatalf("disconnect cleanup removed already-expired pending session")
		}
	})
}

func TestHandshakeSessionsTimeoutDecisionRace(t *testing.T) {
	fix := newRouteHandshakeFixture(t)

	t.Run("timeout cleanup wins", func(t *testing.T) {
		link := newTRouteLink(1212)
		node := newSessionTestNode(t, fix.state(roleSlave))
		session := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
			nodeID:   routePeerNodeID,
			role:     roleMaster,
			frontier: fix.decisionFrontier,
			compat:   fix.compat,
		}))

		if !expirePendingHandshakeForTest(t, node.handshakes, link, session) {
			t.Fatalf("timeout failed to clear pending session")
		}
		rpcErr := node.handshakes.handleDecision(context.Background(), link, 1, &decisionMessage{Ancestor: true})
		requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "unexpected mesh decision")
		requireSent(t, link, 0)
		if got := link.disconnectCount(); got != 1 {
			t.Fatalf("disconnects = %d, want 1", got)
		}
	})

	t.Run("decision cleanup wins", func(t *testing.T) {
		link := newTRouteLink(1213)
		node := newSessionTestNode(t, fix.state(roleSlave))
		captured, stopResolver := startHandshakeResolver(node, nil)
		defer stopResolver()
		session := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
			nodeID:   routePeerNodeID,
			role:     roleMaster,
			frontier: fix.decisionFrontier,
			compat:   fix.compat,
		}))

		rpcErr := node.handshakes.handleDecision(context.Background(), link, 1, &decisionMessage{Ancestor: true})
		requireNoRPCError(t, rpcErr)
		requireOneAck(t, link)
		requireHandshakeCapture(t, captured, link, peerHandshakeCapture(roleMaster, progressPeerAhead, fix.decisionFrontier))
		if expirePendingHandshakeForTest(t, node.handshakes, link, session) {
			t.Fatalf("stale timeout removed already-resolved pending session")
		}
		if got := link.disconnectCount(); got != 0 {
			t.Fatalf("disconnects = %d, want 0", got)
		}
		if got := pendingHandshakeCount(node.handshakes); got != 0 {
			t.Fatalf("pending handshakes = %d, want 0", got)
		}
	})
}

func TestHandshakeSessionsDuplicateHelloPreservesOriginalPending(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	tests := []struct {
		name        string
		secondHello *helloMessage
	}{
		{
			name:        "duplicate unresolved hello",
			secondHello: fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier),
		},
		{
			name:        "duplicate resolving hello",
			secondHello: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := newTRouteLink(1208)
			node := newSessionTestNode(t, fix.state(roleSlave))
			firstHello := fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier)
			rpcErr := node.handshakes.handleHello(context.Background(), link, 1, firstHello)
			requireNoRPCError(t, rpcErr)
			firstSession, found := getPendingHandshake(node.handshakes, link.ID())
			if !found {
				t.Fatalf("missing first pending session")
			}

			rpcErr = node.handshakes.handleHello(context.Background(), link, 2, tt.secondHello)
			requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "already has pending decision")
			requireSent(t, link, 1)
			if got := pendingHandshakeCount(node.handshakes); got != 1 {
				t.Fatalf("pending handshakes = %d, want 1", got)
			}
			gotSession, found := getPendingHandshake(node.handshakes, link.ID())
			if !found || gotSession != firstSession {
				t.Fatalf("pending session replaced: got %p found %v, want %p", gotSession, found, firstSession)
			}
			if gotSession.pending.peerHello != firstHello {
				t.Fatalf("pending hello replaced")
			}
		})
	}
}

func TestHandshakeSessionsStaleCleanupNoop(t *testing.T) {
	fix := newRouteHandshakeFixture(t)
	link := newTRouteLink(1209)
	node := newSessionTestNode(t, fix.state(roleSlave))

	first := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
		nodeID:   routePeerNodeID,
		role:     roleMaster,
		frontier: &db.EventLogPosition{Seq: 11, TipHash: testTipHash(11)},
		compat:   fix.compat,
	}))
	taken := node.handshakes.takePending(link.ID())
	if taken != first {
		t.Fatalf("taken session = %p, want %p", taken, first)
	}
	second := node.handshakes.addPending(link, newSessionPending(t, helloSpec{
		nodeID:   routePeerNodeID,
		role:     roleMaster,
		frontier: &db.EventLogPosition{Seq: 12, TipHash: testTipHash(12)},
		compat:   fix.compat,
	}))

	if node.handshakes.clearPending(link.ID(), first) {
		t.Fatalf("stale cleanup removed current pending session")
	}
	if got := pendingHandshakeCount(node.handshakes); got != 1 {
		t.Fatalf("pending handshakes = %d, want 1", got)
	}
	got, found := getPendingHandshake(node.handshakes, link.ID())
	if !found || got != second {
		t.Fatalf("pending session = %p found %v, want %p", got, found, second)
	}
	if got := link.disconnectCount(); got != 0 {
		t.Fatalf("disconnects = %d, want 0", got)
	}
}
