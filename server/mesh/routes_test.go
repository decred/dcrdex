// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

const (
	routeLocalNodeID = "local-node"
	routePeerNodeID  = "peer-node"
)

type tRouteLink struct {
	id   uint64
	addr string

	sendErr     error
	requestFunc func(context.Context, string, any, any) error

	mtx            sync.Mutex
	sent           []*msgjson.Message
	authorizeCalls int
	disconnects    int
	ops            []string
	done           chan struct{}
	once           sync.Once
}

func newTRouteLink(id uint64) *tRouteLink {
	return &tRouteLink{
		id:   id,
		addr: "route-test-peer",
		done: make(chan struct{}),
	}
}

func (c *tRouteLink) ID() uint64            { return c.id }
func (c *tRouteLink) Addr() string          { return c.addr }
func (c *tRouteLink) Done() <-chan struct{} { return c.done }

func (c *tRouteLink) Request(ctx context.Context, route string, payload any, response any) error {
	if c.requestFunc != nil {
		return c.requestFunc(ctx, route, payload, response)
	}
	return nil
}

func (c *tRouteLink) Send(msg *msgjson.Message) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	// Model the real link: once disconnected, sends are refused, so a test
	// cannot certify delivery of an answer queued after a disconnect.
	if c.disconnects > 0 {
		return errors.New("peer disconnected")
	}
	c.sent = append(c.sent, msg)
	c.ops = append(c.ops, "send")
	return nil
}

func (c *tRouteLink) Authorize() {
	c.mtx.Lock()
	c.authorizeCalls++
	c.ops = append(c.ops, "authorize")
	c.mtx.Unlock()
}

func (c *tRouteLink) Disconnect() {
	c.mtx.Lock()
	c.disconnects++
	c.ops = append(c.ops, "disconnect")
	c.mtx.Unlock()
	c.once.Do(func() {
		close(c.done)
	})
}

func (c *tRouteLink) sentMessages() []*msgjson.Message {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return append([]*msgjson.Message(nil), c.sent...)
}

func (c *tRouteLink) authorizeCount() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.authorizeCalls
}

func (c *tRouteLink) disconnectCount() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.disconnects
}

func (c *tRouteLink) operations() []string {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return append([]string(nil), c.ops...)
}

func mustRequestMessage(t testing.TB, route string, payload any) *msgjson.Message {
	t.Helper()

	msg, err := msgjson.NewRequest(1, route, payload)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return msg
}

func decodeHelloResponse(t testing.TB, msg *msgjson.Message) *helloResponse {
	t.Helper()

	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %v", resp.Error)
	}

	var helloResp helloResponse
	if err := json.Unmarshal(resp.Result, &helloResp); err != nil {
		t.Fatalf("Unmarshal hello response error: %v", err)
	}
	return &helloResp
}

func decodeEmptyResponse(t testing.TB, msg *msgjson.Message) {
	t.Helper()

	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %v", resp.Error)
	}

	var ack map[string]json.RawMessage
	if err := json.Unmarshal(resp.Result, &ack); err != nil {
		t.Fatalf("Unmarshal ack error: %v", err)
	}
	if len(ack) != 0 {
		t.Fatalf("ack payload fields = %d, want 0", len(ack))
	}
}

func decodeResponseError(t testing.TB, msg *msgjson.Message) *msgjson.Error {
	t.Helper()

	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("expected response error")
	}
	return resp.Error
}

func requireNoRPCError(t testing.TB, rpcErr *msgjson.Error) {
	t.Helper()
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %+v", rpcErr)
	}
}

func requireRPCCode(t testing.TB, rpcErr *msgjson.Error, code int) {
	t.Helper()
	if rpcErr == nil || rpcErr.Code != code {
		t.Fatalf("rpc error = %+v, want code %d", rpcErr, code)
	}
}

func requireRPCOutcome(t testing.TB, rpcErr *msgjson.Error, code int, msg string) {
	t.Helper()
	if code == 0 {
		requireNoRPCError(t, rpcErr)
		return
	}
	requireRPCCode(t, rpcErr, code)
	if msg != "" && !strings.Contains(rpcErr.Message, msg) {
		t.Fatalf("rpc error message %q does not contain %q", rpcErr.Message, msg)
	}
}

func requireSent(t testing.TB, conn *tRouteLink, want int) []*msgjson.Message {
	t.Helper()
	sent := conn.sentMessages()
	if len(sent) != want {
		t.Fatalf("sent messages = %d, want %d", len(sent), want)
	}
	return sent
}

func requireOneAck(t testing.TB, conn *tRouteLink) {
	t.Helper()
	decodeEmptyResponse(t, requireSent(t, conn, 1)[0])
}

func requireNoSent(t testing.TB, conn *tRouteLink) {
	t.Helper()
	requireSent(t, conn, 0)
}

func requireResponseErrorCode(t testing.TB, conn *tRouteLink, code int) {
	t.Helper()
	msg := requireSent(t, conn, 1)[0]
	if msg.ID != 1 {
		t.Fatalf("response id = %d, want 1", msg.ID)
	}
	if got := decodeResponseError(t, msg); got.Code != code {
		t.Fatalf("response error = %+v, want code %d", got, code)
	}
}

func waitForCondition(t testing.TB, cond func() bool, desc string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met: %s", desc)
}

type testMeshApplication struct {
	commandForward  func(context.Context, string, CommandRequest) *msgjson.Error
	commandFailure  func(string, *msgjson.Error)
	commandResult   func(string, json.RawMessage)
	clientProxy     func(context.Context, *ClientProxyMessage) error
	clientConnected func([]account.AccountID) []account.AccountID
	event           func(context.Context, *eventEnvelope) error
}

func (a *testMeshApplication) answerClientConnected(users []account.AccountID) []account.AccountID {
	if a.clientConnected == nil {
		return nil
	}
	return a.clientConnected(users)
}

func (a *testMeshApplication) executeForwardedCommand(ctx context.Context, commandID string, req CommandRequest) *msgjson.Error {
	if a.commandForward == nil {
		return nil
	}
	return a.commandForward(ctx, commandID, req)
}

func (a *testMeshApplication) receiveCommandFailure(commandID string, msgErr *msgjson.Error) {
	if a.commandFailure != nil {
		a.commandFailure(commandID, msgErr)
	}
}

func (a *testMeshApplication) receiveCommandResult(commandID string, result json.RawMessage) {
	if a.commandResult != nil {
		a.commandResult(commandID, result)
	}
}

func (a *testMeshApplication) handleClientProxyMessage(ctx context.Context, msg *ClientProxyMessage) error {
	if a.clientProxy == nil {
		return nil
	}
	return a.clientProxy(ctx, msg)
}

func (a *testMeshApplication) applyReceivedEvent(ctx context.Context, entry *eventEnvelope) error {
	if a.event == nil {
		return nil
	}
	return a.event(ctx, entry)
}

func newRouteTestNode(mode nodeMode, active link, app meshApplication) *node {
	if app == nil {
		app = &testMeshApplication{}
	}
	state := nodeState{
		mode: mode,
	}
	if active != nil {
		state.activeConn = newNodeConn(active, "peer-node", "peer-node")
	}
	return newTestNodeWithState(state, app)
}

func handleRoutePayload(t testing.TB, n *node, routeName string, conn link, payload any) *msgjson.Error {
	t.Helper()
	return handleRouteMessage(t, context.Background(), n, conn, mustRequestMessage(t, routeName, payload))
}

func handleRouteMessage(t testing.TB, ctx context.Context, n *node, conn link, msg *msgjson.Message) *msgjson.Error {
	t.Helper()
	route := n.routes()[msg.Route]
	if route.handler == nil {
		t.Fatalf("missing test route %q", msg.Route)
	}
	return route.handler(ctx, conn, msg)
}

func validCommandForward(t *testing.T, commandID string) *commandForward {
	t.Helper()
	req := testCommandRequest(t)
	return &commandForward{
		CommandID: commandID,
		Kind:      req.Kind,
		User:      req.User,
		Msg:       req.Msg,
	}
}

func validClientProxyMessage(t testing.TB) *ClientProxyMessage {
	t.Helper()
	req, err := msgjson.NewRequest(77, msgjson.PreimageRoute, map[string]string{"ok": "true"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return &ClientProxyMessage{
		User: account.AccountID{0x22},
		Msg:  req,
	}
}

type capturedHandshakeResolution struct {
	calls        int
	conn         *nodeConn
	role         helloRole
	progress     progressState
	peerFrontier *db.EventLogPosition
	clientHost   string
	clientCert   []byte
}

type handshakeCaptureExpectation struct {
	calls        int
	role         helloRole
	progress     progressState
	peerFrontier *db.EventLogPosition
	peerNodeID   string
	initiatorID  string
}

func startHandshakeResolver(n *node, applyErr error) (*capturedHandshakeResolution, func()) {
	captured := new(capturedHandshakeResolution)
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		select {
		case queued := <-n.control.testQueue():
			ev, ok := queued.signal.(handshakeResolvedSignal)
			if !ok {
				if queued.reply != nil {
					queued.reply <- signalResult{err: errors.New("unexpected mesh signal")}
				}
				return
			}
			captured.calls++
			captured.conn = ev.conn
			captured.role = ev.peerRole
			captured.progress = ev.progress
			captured.peerFrontier = ev.peerFrontier
			captured.clientHost = ev.clientHost
			captured.clientCert = append([]byte(nil), ev.clientCert...)
			if queued.reply != nil {
				res := signalResult{
					handled: applyErr == nil,
					err:     applyErr,
					state: nodeState{
						mode:       modeEstablishedMaster,
						activeConn: ev.conn,
					},
				}
				switch {
				case applyErr != nil:
				case ev.progress == progressDiverged:
					// A fork halts this node. The state machine reports the
					// halt as the outcome, and the apply returns the halt
					// error.
					res.outcome = handshakeHalted
					res.state = nodeState{mode: modeHalted, haltErr: errors.New("diverged")}
				default:
					res.outcome = handshakeAdopted
				}
				queued.reply <- res
			}
		case <-done:
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
	return captured, stop
}

func newRouteHandshakeNode(svc *handshakeService) *node {
	n := newTestNodeWithState(nodeState{mode: modePending}, &testMeshApplication{})
	n.handshakeSvc = svc
	n.handshakes = newHandshakeSessions(n.log, svc, n)
	return n
}

func installRoutePendingDecision(t testing.TB, handshakes *handshakeSessions, conn link, state handshakeServiceState) {
	t.Helper()

	for _, pending := range state.pending {
		if pending.connID != conn.ID() {
			t.Fatalf("test pending connID = %d, want route link ID %d", pending.connID, conn.ID())
		}
		handshakes.addPending(conn, &handshakeResult{
			peerHello:  buildSignedHello(t, pending.peerHello),
			progress:   progressPeerAhead,
			clientHost: pending.peerHello.clientHost,
			clientCert: append([]byte(nil), pending.peerHello.clientCert...),
		})
	}
}

func requireHandshakeCapture(t testing.TB, captured *capturedHandshakeResolution, conn link, want handshakeCaptureExpectation) {
	t.Helper()
	if captured.calls != want.calls {
		t.Fatalf("callback calls = %d, want %d", captured.calls, want.calls)
	}
	if want.calls == 0 {
		return
	}
	if captured.conn == nil {
		t.Fatalf("nil callback conn")
	}
	if captured.role != want.role {
		t.Fatalf("callback role = %v, want %v", captured.role, want.role)
	}
	if captured.progress != want.progress {
		t.Fatalf("callback progress = %v, want %v", captured.progress, want.progress)
	}
	if want.peerFrontier != nil {
		if captured.peerFrontier == nil {
			t.Fatalf("nil callback peer frontier")
		}
		if captured.peerFrontier.Seq != want.peerFrontier.Seq || !bytes.Equal(captured.peerFrontier.TipHash, want.peerFrontier.TipHash) {
			t.Fatalf("callback peer frontier = %+v, want %+v", captured.peerFrontier, want.peerFrontier)
		}
	}
	if captured.conn.peerNodeID != want.peerNodeID {
		t.Fatalf("conn peerNodeID = %q, want %q", captured.conn.peerNodeID, want.peerNodeID)
	}
	if captured.conn.initiatorNodeID != want.initiatorID {
		t.Fatalf("conn initiatorNodeID = %q, want %q", captured.conn.initiatorNodeID, want.initiatorID)
	}
	if captured.conn.link != conn {
		t.Fatalf("callback conn did not wrap the route link")
	}
}

type routeRPCExpectation struct {
	code int
	msg  string
}

type routeHandshakeFixture struct {
	compat            *CompatSnapshot
	localFrontier     *db.EventLogPosition
	peerEqualFrontier *db.EventLogPosition
	peerAheadFrontier *db.EventLogPosition
	decisionFrontier  *db.EventLogPosition
}

func newRouteHandshakeFixture(t testing.TB) routeHandshakeFixture {
	t.Helper()
	return routeHandshakeFixture{
		compat:            testCompatSnapshot(t),
		localFrontier:     &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)},
		peerEqualFrontier: &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)},
		peerAheadFrontier: &db.EventLogPosition{Seq: 11, TipHash: testTipHash(11)},
		decisionFrontier:  &db.EventLogPosition{Seq: 15, TipHash: testTipHash(15)},
	}
}

func (f routeHandshakeFixture) state(role helloRole) handshakeServiceState {
	return handshakeServiceState{
		nodeID:        routeLocalNodeID,
		role:          role,
		compat:        f.compat,
		localFrontier: f.localFrontier,
	}
}

func (f routeHandshakeFixture) hello(t testing.TB, nodeID string, role helloRole, frontier *db.EventLogPosition) *helloMessage {
	t.Helper()
	return buildSignedHello(t, helloSpec{
		nodeID:   nodeID,
		role:     role,
		frontier: frontier,
		compat:   f.compat,
	})
}

func (f routeHandshakeFixture) decisionState(role helloRole) handshakeServiceState {
	return handshakeServiceState{
		nodeID: routeLocalNodeID,
		compat: f.compat,
		pending: []pendingDecisionState{{
			connID: 202,
			peerHello: helloSpec{
				nodeID:   routePeerNodeID,
				role:     role,
				frontier: f.decisionFrontier,
				compat:   f.compat,
			},
		}},
	}
}

func peerHandshakeCapture(role helloRole, progress progressState, frontier *db.EventLogPosition) handshakeCaptureExpectation {
	return handshakeCaptureExpectation{
		calls:        1,
		role:         role,
		progress:     progress,
		peerFrontier: frontier,
		peerNodeID:   routePeerNodeID,
		initiatorID:  routePeerNodeID,
	}
}

func TestNodeRoutesHandleCommandResult(t *testing.T) {
	t.Run("delivers pending result and sends ack", func(t *testing.T) {
		active := newTRouteLink(301)
		svc := &Service{commands: newTestCommandCoordinator(nil, nil)}
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		svc.commands.registerPending("cmd-ok", req)
		defer svc.commands.removePending("cmd-ok")
		result := map[string]string{"status": "ok"}

		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			commandResult: svc.receiveCommandResult,
		})

		rpcErr := handleRoutePayload(t, handler, commandResultRoute, active, &commandResult{
			CommandID: "cmd-ok",
			Result:    mustMarshalJSON(t, result),
		})
		requireNoRPCError(t, rpcErr)

		requireOneAck(t, active)
		svc.commands.pendingMtx.Lock()
		pending := svc.commands.pending["cmd-ok"]
		svc.commands.pendingMtx.Unlock()
		if pending != nil {
			t.Fatalf("pending command was not removed")
		}
		responder.requireResult(t, result)
	})

	t.Run("rejects invalid payload", func(t *testing.T) {
		active := newTRouteLink(305)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			commandResult: func(string, json.RawMessage) {
				t.Fatalf("unexpected command result callback")
			},
		})

		result := map[string]string{"status": "ok"}
		rpcErr := handleRoutePayload(t, handler, commandResultRoute, active, &commandResult{
			Result: mustMarshalJSON(t, result),
		})
		requireRPCCode(t, rpcErr, msgjson.RPCParseError)
	})
}

func TestNodeRoutesHandleCommandFailure(t *testing.T) {
	t.Run("delivers failure and sends ack", func(t *testing.T) {
		active := newTRouteLink(351)
		wantErr := msgjson.NewError(msgjson.FundingError, "funding failed")
		var (
			gotCommandID string
			gotErr       *msgjson.Error
		)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			commandFailure: func(commandID string, msgErr *msgjson.Error) {
				gotCommandID = commandID
				gotErr = msgErr
			},
		})

		rpcErr := handleRoutePayload(t, handler, commandFailureRoute, active, &commandFailure{
			CommandID: "cmd-fail",
			Error:     wantErr,
		})
		requireNoRPCError(t, rpcErr)
		if gotErr == nil {
			t.Fatalf("command failure callback was not called")
		}
		if gotCommandID != "cmd-fail" || gotErr.Code != wantErr.Code {
			t.Fatalf("command failure = %s %+v, want command cmd-fail code %d", gotCommandID, gotErr, wantErr.Code)
		}
		requireOneAck(t, active)
	})

}

func TestNodeRoutesHandleCommandForward(t *testing.T) {
	t.Run("sends startup ack only after app completes", func(t *testing.T) {
		active := newTRouteLink(401)
		started := make(chan struct{})
		release := make(chan struct{})
		handler := newRouteTestNode(modeEstablishedMaster, active, &testMeshApplication{
			commandForward: func(context.Context, string, CommandRequest) *msgjson.Error {
				close(started)
				<-release
				return nil
			},
		})

		rpcErr := handleRoutePayload(t, handler, commandForwardRoute, active, validCommandForward(t, "cmd-async"))
		requireNoRPCError(t, rpcErr)
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("command app handler did not start")
		}
		requireNoSent(t, active)

		close(release)
		waitForCondition(t, func() bool { return len(active.sentMessages()) == 1 }, "forwarded command ack")
		requireOneAck(t, active)
	})

	t.Run("preserves msgjson app error", func(t *testing.T) {
		active := newTRouteLink(402)
		wantErr := msgjson.NewError(msgjson.FundingError, "funding failed")
		handler := newRouteTestNode(modeEstablishedMaster, active, &testMeshApplication{
			commandForward: func(context.Context, string, CommandRequest) *msgjson.Error {
				return wantErr
			},
		})

		rpcErr := handleRoutePayload(t, handler, commandForwardRoute, active, validCommandForward(t, "cmd-msgjson-err"))
		requireNoRPCError(t, rpcErr)
		waitForCondition(t, func() bool { return len(active.sentMessages()) == 1 }, "forwarded command error response")
		requireResponseErrorCode(t, active, wantErr.Code)
	})

	t.Run("runs under the node run context", func(t *testing.T) {
		active := newTRouteLink(405)
		gotCtx := make(chan context.Context, 1)
		handler := newRouteTestNode(modeEstablishedMaster, active, &testMeshApplication{
			commandForward: func(ctx context.Context, _ string, _ CommandRequest) *msgjson.Error {
				gotCtx <- ctx
				return nil
			},
		})
		type runKey struct{}
		runCtx := context.WithValue(context.Background(), runKey{}, "run")
		handler.runContext = runCtx
		routeCtx, cancel := context.WithCancel(context.Background())
		cancel()

		rpcErr := handleRouteMessage(t, routeCtx, handler, active,
			mustRequestMessage(t, commandForwardRoute, validCommandForward(t, "cmd-ctx")))
		requireNoRPCError(t, rpcErr)
		select {
		case got := <-gotCtx:
			if got != runCtx {
				t.Fatalf("command context = %v, want node run context", got)
			}
		case <-time.After(time.Second):
			t.Fatal("command was not run")
		}
		waitForCondition(t, func() bool { return len(active.sentMessages()) == 1 }, "forwarded command ack")
		requireOneAck(t, active)
	})

	t.Run("rejects invalid payload before app goroutine", func(t *testing.T) {
		active := newTRouteLink(404)
		var called atomic.Bool
		handler := newRouteTestNode(modeEstablishedMaster, active, &testMeshApplication{
			commandForward: func(context.Context, string, CommandRequest) *msgjson.Error {
				called.Store(true)
				return nil
			},
		})

		rpcErr := handleRoutePayload(t, handler, commandForwardRoute, active, &commandForward{})
		requireRPCCode(t, rpcErr, msgjson.RPCParseError)
		requireNoSent(t, active)
		if called.Load() {
			t.Fatalf("unexpected command app call")
		}
	})
}

func TestNodeRoutesHandleClientProxyMessage(t *testing.T) {
	t.Run("sends ack on success", func(t *testing.T) {
		active := newTRouteLink(501)
		var calls int
		handler := newRouteTestNode(modeEstablishedMaster, active, &testMeshApplication{
			clientProxy: func(context.Context, *ClientProxyMessage) error {
				calls++
				return nil
			},
		})

		rpcErr := handleRoutePayload(t, handler, clientProxyMessageRoute, active, validClientProxyMessage(t))
		requireNoRPCError(t, rpcErr)
		if calls != 1 {
			t.Fatalf("client proxy calls = %d, want 1", calls)
		}
		requireOneAck(t, active)
	})

	t.Run("allows a syncing slave", func(t *testing.T) {
		active := newTRouteLink(502)
		var calls int
		handler := newRouteTestNode(modeEstablishedSlaveSyncing, active, &testMeshApplication{
			clientProxy: func(context.Context, *ClientProxyMessage) error {
				calls++
				return nil
			},
		})

		rpcErr := handleRoutePayload(t, handler, clientProxyMessageRoute, active, validClientProxyMessage(t))
		requireNoRPCError(t, rpcErr)
		if calls != 1 {
			t.Fatalf("client proxy calls = %d, want 1", calls)
		}
		requireOneAck(t, active)
	})

	t.Run("maps client-not-connected app error", func(t *testing.T) {
		active := newTRouteLink(506)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			clientProxy: func(context.Context, *ClientProxyMessage) error {
				return fmt.Errorf("%w: user gone", ErrClientNotConnected)
			},
		})

		rpcErr := handleRoutePayload(t, handler, clientProxyMessageRoute, active, validClientProxyMessage(t))
		requireRPCCode(t, rpcErr, msgjson.UserNotConnectedError)
	})

	t.Run("client-not-connected verdict reaches requester as sentinel", func(t *testing.T) {
		active := newTRouteLink(507)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			clientProxy: func(context.Context, *ClientProxyMessage) error {
				return fmt.Errorf("%w: user gone", ErrClientNotConnected)
			},
		})
		active.requestFunc = func(ctx context.Context, route string, payload any, response any) error {
			msg, err := msgjson.NewRequest(99, route, payload)
			if err != nil {
				return err
			}
			rpcErr := handleRouteMessage(t, ctx, handler, active, msg)
			if rpcErr != nil {
				return &peerRPCError{Code: rpcErr.Code, Message: rpcErr.Message}
			}
			return nil
		}
		requester := &node{
			log: dex.Disabled,
			control: newTestControlLoop(nodeState{
				mode:       modeEstablishedMaster,
				activeConn: newNodeConn(active, "peer-node", "peer-node"),
			}),
		}

		err := requester.sendClientProxyMessage(context.Background(), validClientProxyMessage(t))
		if !errors.Is(err, ErrClientNotConnected) {
			t.Fatalf("requester error = %v, want ErrClientNotConnected", err)
		}
	})

	t.Run("wraps ordinary app error", func(t *testing.T) {
		active := newTRouteLink(503)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			clientProxy: func(context.Context, *ClientProxyMessage) error {
				return errors.New("boom")
			},
		})

		rpcErr := handleRoutePayload(t, handler, clientProxyMessageRoute, active, validClientProxyMessage(t))
		requireRPCCode(t, rpcErr, msgjson.RPCInternal)
	})
}

func TestNodeRoutesPolicyBeforeParse(t *testing.T) {
	active := newTRouteLink(601)
	inactive := newTRouteLink(602)

	tests := []struct {
		name  string
		mode  nodeMode
		conn  link
		route string
	}{
		{"command forward wrong mode", modeEstablishedSlave, active, commandForwardRoute},
		{"command forward preparing master", modePreparingMaster, active, commandForwardRoute},
		{"command forward inactive", modeEstablishedMaster, inactive, commandForwardRoute},
		{"command failure while syncing", modeEstablishedSlaveSyncing, active, commandFailureRoute},
		{"command failure wrong mode", modeEstablishedMaster, active, commandFailureRoute},
		{"command failure inactive", modeEstablishedSlave, inactive, commandFailureRoute},
		{"command result while syncing", modeEstablishedSlaveSyncing, active, commandResultRoute},
		{"command result wrong mode", modeEstablishedMaster, active, commandResultRoute},
		{"command result inactive", modeEstablishedSlave, inactive, commandResultRoute},
		{"client proxy wrong mode", modePending, active, clientProxyMessageRoute},
		{"client proxy preparing master", modePreparingMaster, active, clientProxyMessageRoute},
		{"client proxy inactive", modeEstablishedMaster, inactive, clientProxyMessageRoute},
		{"client connected wrong mode", modePending, active, clientConnectedRoute},
		{"client connected inactive", modeEstablishedSlave, inactive, clientConnectedRoute},
		{"event wrong mode", modeEstablishedMaster, active, eventEnvelopeRoute},
		{"event inactive", modeEstablishedSlave, inactive, eventEnvelopeRoute},
		{"master handoff wrong mode", modeEstablishedMaster, active, masterHandoffRoute},
		{"master handoff inactive", modeEstablishedSlave, inactive, masterHandoffRoute},
		{"snapshot chunk wrong mode", modeEstablishedMaster, active, snapshotChunkRoute},
		{"snapshot chunk inactive", modeEstablishedSlaveSyncing, inactive, snapshotChunkRoute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			handler := newRouteTestNode(tt.mode, active, &testMeshApplication{
				commandForward:  func(context.Context, string, CommandRequest) *msgjson.Error { calls++; return nil },
				commandFailure:  func(string, *msgjson.Error) { calls++ },
				commandResult:   func(string, json.RawMessage) { calls++ },
				clientProxy:     func(context.Context, *ClientProxyMessage) error { calls++; return nil },
				clientConnected: func([]account.AccountID) []account.AccountID { calls++; return nil },
				event:           func(context.Context, *eventEnvelope) error { calls++; return nil },
			})

			rpcErr := handleRoutePayload(t, handler, tt.route, tt.conn, "not a valid payload")
			requireRPCCode(t, rpcErr, msgjson.UnauthorizedConnection)
			if calls != 0 {
				t.Fatalf("app calls = %d, want 0", calls)
			}
		})
	}
}

func TestNodeRoutesHandleEventEnvelope(t *testing.T) {
	t.Run("allows syncing and established slave states", func(t *testing.T) {
		for _, mode := range []nodeMode{modeEstablishedSlaveSyncing, modeEstablishedSlave} {
			t.Run(mode.String(), func(t *testing.T) {
				active := newTRouteLink(701)
				handler := newRouteTestNode(mode, active, &testMeshApplication{
					event: func(context.Context, *eventEnvelope) error {
						return nil
					},
				})

				rpcErr := handleRoutePayload(t, handler, eventEnvelopeRoute, active, &eventBatch{Entries: []*eventEnvelope{{
					Seq:       1,
					TipHash:   testTipHash(1),
					MasterTip: 2,
					Kind:      "test",
				}}})
				requireNoRPCError(t, rpcErr)
				requireSent(t, active, 1)
			})
		}
	})

	t.Run("uses accepted connection snapshot for caught up event", func(t *testing.T) {
		active := newTRouteLink(702)
		replacement := newTRouteLink(703)
		handler := newRouteTestNode(modeEstablishedSlaveSyncing, active, nil)
		handler.app = &testMeshApplication{
			event: func(context.Context, *eventEnvelope) error {
				handler.control.setState(nodeState{
					mode:       modeEstablishedSlaveSyncing,
					activeConn: newNodeConn(replacement, "replacement-node", "replacement-node"),
				})
				return nil
			},
		}

		caughtUp := make(chan *nodeConn, 1)
		go func() {
			queued := <-handler.control.testQueue()
			ev, ok := queued.signal.(streamCaughtUpSignal)
			if ok {
				caughtUp <- ev.conn
			}
		}()

		rpcErr := handleRoutePayload(t, handler, eventEnvelopeRoute, active, &eventBatch{Entries: []*eventEnvelope{{
			Seq:       1,
			TipHash:   testTipHash(1),
			MasterTip: 1,
			Kind:      "test",
		}}})
		requireNoRPCError(t, rpcErr)

		select {
		case got := <-caughtUp:
			if got == nil || got.link != active {
				t.Fatalf("caught up conn = %v, want original active link", got)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing stream caught up event")
		}
	})

	t.Run("uses run context after route cancellation", func(t *testing.T) {
		active := newTRouteLink(704)
		appliedCtx := make(chan context.Context, 1)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			event: func(ctx context.Context, _ *eventEnvelope) error {
				appliedCtx <- ctx
				return ctx.Err()
			},
		})
		runCtx := context.Background()
		handler.runContext = runCtx
		routeCtx, cancel := context.WithCancel(context.Background())
		cancel()

		rpcErr := handleRouteMessage(t, routeCtx, handler, active,
			mustRequestMessage(t, eventEnvelopeRoute, &eventBatch{Entries: []*eventEnvelope{{
				Seq: 1, TipHash: testTipHash(1), MasterTip: 2, Kind: "test",
			}}}))
		requireNoRPCError(t, rpcErr)
		requireSent(t, active, 1)
		select {
		case got := <-appliedCtx:
			if got != runCtx {
				t.Fatalf("apply context = %v, want node run context", got)
			}
		case <-time.After(time.Second):
			t.Fatal("event was not applied")
		}
	})
}

func TestNodeRoutesHandleHello(t *testing.T) {
	fix := newRouteHandshakeFixture(t)

	type expectation struct {
		rpc            routeRPCExpectation
		capture        handshakeCaptureExpectation
		authorize      int
		sent           int
		disconnects    int
		ancestor       bool
		pending        int
		cleanupPending bool
	}
	// activeConnCase says which connection, if any, is the node's active
	// connection when the hello arrives.
	type activeConnCase int
	const (
		noActiveConn activeConnCase = iota
		activeConnIsHelloLink
		activeConnIsOtherLink
	)
	type routeCase struct {
		name          string
		state         handshakeServiceState
		msgPayload    any
		zeroRequestID bool
		sendErr       error
		onCompleteErr error
		activeConn    activeConnCase
		want          expectation
	}

	run := func(t *testing.T, tt routeCase) {
		t.Helper()
		svc, _ := newTestHandshakeService(t, tt.state)
		link := newTRouteLink(101)
		link.sendErr = tt.sendErr

		node := newRouteHandshakeNode(svc)
		switch tt.activeConn {
		case activeConnIsHelloLink:
			node.control.setState(nodeState{
				mode:       modeEstablishedMaster,
				activeConn: newNodeConn(link, routePeerNodeID, routePeerNodeID),
			})
		case activeConnIsOtherLink:
			node.control.setState(nodeState{
				mode:       modeEstablishedMaster,
				activeConn: newNodeConn(newTRouteLink(102), routePeerNodeID, routePeerNodeID),
			})
		}
		captured, stopResolver := startHandshakeResolver(node, tt.onCompleteErr)
		defer stopResolver()

		msg := mustRequestMessage(t, helloRoute, tt.msgPayload)
		if tt.zeroRequestID {
			msg.ID = 0
		}

		rpcErr := node.handleHello(context.Background(), link, msg)
		requireRPCOutcome(t, rpcErr, tt.want.rpc.code, tt.want.rpc.msg)
		if got := link.authorizeCount(); got != tt.want.authorize {
			t.Fatalf("authorize calls = %d, want %d", got, tt.want.authorize)
		}
		sent := requireSent(t, link, tt.want.sent)
		requireHandshakeCapture(t, captured, link, tt.want.capture)
		if got := link.disconnectCount(); got != tt.want.disconnects {
			t.Fatalf("disconnects = %d, want %d", got, tt.want.disconnects)
		}
		if tt.want.sent > 0 {
			helloResp := decodeHelloResponse(t, sent[0])
			if helloResp.Ancestor != tt.want.ancestor {
				t.Fatalf("hello response ancestor = %v, want %v", helloResp.Ancestor, tt.want.ancestor)
			}
		}
		if got := pendingHandshakeCount(node.handshakes); got != tt.want.pending {
			t.Fatalf("pending decisions = %d, want %d", got, tt.want.pending)
		}
		if tt.want.cleanupPending {
			if _, found := getPendingHandshake(node.handshakes, link.ID()); !found {
				t.Fatalf("missing pending decision for connection %d", link.ID())
			}
			link.Disconnect()
			waitForCondition(t, func() bool {
				_, found := getPendingHandshake(node.handshakes, link.ID())
				return !found
			}, "pending decision cleanup after disconnect")
		}
	}

	tests := []routeCase{
		{
			name:       "parse error",
			state:      handshakeServiceState{nodeID: routeLocalNodeID, compat: fix.compat},
			msgPayload: "not a hello",
			want: expectation{
				rpc: routeRPCExpectation{code: msgjson.RPCParseError},
			},
		},
		{
			name:       "handshake validation error",
			state:      fix.state(roleUnknown),
			msgPayload: fix.hello(t, routeLocalNodeID, roleSlave, fix.peerEqualFrontier),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.AuthenticationError,
					msg:  "mesh hello verification failed",
				},
			},
		},
		{
			name: "frontier read error is internal",
			state: func() handshakeServiceState {
				state := fix.state(roleMaster)
				state.frontierErr = errors.New("db down")
				return state
			}(),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "mesh hello processing failed",
				},
			},
		},
		{
			// This node's log begins at an anchor above the peer's tip.
			name: "hello below the snapshot anchor is incompatible",
			state: func() handshakeServiceState {
				state := fix.state(roleMaster)
				state.eventEntries = []*db.EventLogEntry{{Seq: 10, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(10)}}
				return state
			}(),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)}),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.MeshIncompatibleLogError,
					msg:  "mesh hello rejected",
				},
			},
		},
		{
			name:       "hello on the active connection is refused",
			state:      fix.state(roleMaster),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
			activeConn: activeConnIsHelloLink,
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "mesh hello on the active connection",
				},
			},
		},
		{
			name:       "hello on another connection proceeds",
			state:      fix.state(roleMaster),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
			activeConn: activeConnIsOtherLink,
			want: expectation{
				authorize: 1,
				sent:      1,
				capture:   peerHandshakeCapture(roleSlave, progressEqual, fix.peerEqualFrontier),
			},
		},
		{
			name:       "resolved handshake calls callback",
			state:      fix.state(roleMaster),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
			want: expectation{
				authorize: 1,
				sent:      1,
				capture:   peerHandshakeCapture(roleSlave, progressEqual, fix.peerEqualFrontier),
			},
		},
		{
			name:       "unresolved handshake waits for decision",
			state:      fix.state(roleSlave),
			msgPayload: fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier),
			want: expectation{
				authorize:      1,
				sent:           1,
				pending:        1,
				cleanupPending: true,
			},
		},
		{
			// The result is adopted before the response is written, so a send
			// failure still leaves the handshake applied; the broken link is
			// cleaned up through the disconnect watch.
			name:       "response send failure returns internal error",
			state:      fix.state(roleMaster),
			msgPayload: fix.hello(t, routePeerNodeID, roleSlave, fix.peerEqualFrontier),
			sendErr:    errors.New("send failed"),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "failed to send mesh hello response",
				},
				authorize: 1,
				capture:   peerHandshakeCapture(roleSlave, progressEqual, fix.peerEqualFrontier),
			},
		},
		{
			name:       "unresolved response send failure clears pending",
			state:      fix.state(roleSlave),
			msgPayload: fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier),
			sendErr:    errors.New("send failed"),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "failed to send mesh hello response",
				},
				authorize: 1,
				pending:   0,
			},
		},
		{
			name:          "unresolved response encode failure clears pending",
			state:         fix.state(roleSlave),
			msgPayload:    fix.hello(t, routePeerNodeID, roleMaster, fix.peerAheadFrontier),
			zeroRequestID: true,
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "failed to encode mesh hello response",
				},
				authorize: 1,
				pending:   0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run(t, tt)
		})
	}
}

func TestNodeRoutesHandleDecision(t *testing.T) {
	fix := newRouteHandshakeFixture(t)

	type expectation struct {
		rpc         routeRPCExpectation
		capture     handshakeCaptureExpectation
		sent        int
		disconnects int
		pending     int
	}
	type routeCase struct {
		name          string
		state         handshakeServiceState
		msgPayload    any
		sendErr       error
		onCompleteErr error
		want          expectation
	}

	run := func(t *testing.T, tt routeCase) {
		t.Helper()
		svc, _ := newTestHandshakeService(t, tt.state)
		link := newTRouteLink(202)
		link.sendErr = tt.sendErr

		node := newRouteHandshakeNode(svc)
		installRoutePendingDecision(t, node.handshakes, link, tt.state)
		captured, stopResolver := startHandshakeResolver(node, tt.onCompleteErr)
		defer stopResolver()

		rpcErr := node.handleDecision(context.Background(), link, mustRequestMessage(t, helloDecisionRoute, tt.msgPayload))
		requireRPCOutcome(t, rpcErr, tt.want.rpc.code, tt.want.rpc.msg)
		sent := requireSent(t, link, tt.want.sent)
		requireHandshakeCapture(t, captured, link, tt.want.capture)
		if got := link.disconnectCount(); got != tt.want.disconnects {
			t.Fatalf("disconnects = %d, want %d", got, tt.want.disconnects)
		}
		if got := pendingHandshakeCount(node.handshakes); got != tt.want.pending {
			t.Fatalf("pending decisions = %d, want %d", got, tt.want.pending)
		}
		if tt.want.sent > 0 {
			decodeEmptyResponse(t, sent[0])
		}
	}

	tests := []routeCase{
		{
			name:       "parse error",
			state:      handshakeServiceState{nodeID: routeLocalNodeID, compat: fix.compat},
			msgPayload: "not a decision",
			want: expectation{
				rpc: routeRPCExpectation{code: msgjson.RPCParseError},
			},
		},
		{
			// A missing ancestor bit is false, which the behind node treats
			// as a fork. Seq already said the initiator is ahead.
			name:       "missing decision ancestor is a fork",
			state:      fix.decisionState(roleMaster),
			msgPayload: map[string]any{},
			want: expectation{
				sent:        1,
				capture:     peerHandshakeCapture(roleMaster, progressDiverged, fix.decisionFrontier),
				disconnects: 1,
			},
		},
		{
			name:       "explicit false decision ancestor decodes on the wire",
			state:      fix.decisionState(roleSlave),
			msgPayload: map[string]any{"ancestor": false},
			want: expectation{
				sent:        1,
				capture:     peerHandshakeCapture(roleSlave, progressDiverged, fix.decisionFrontier),
				disconnects: 1,
			},
		},
		{
			name:       "decision without pending session is internal",
			state:      handshakeServiceState{nodeID: routeLocalNodeID, compat: fix.compat},
			msgPayload: &decisionMessage{Ancestor: true},
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "mesh decision processing failed",
				},
			},
		},
		{
			name:       "successful decision sends ack",
			state:      fix.decisionState(roleMaster),
			msgPayload: &decisionMessage{Ancestor: true},
			want: expectation{
				sent:    1,
				capture: peerHandshakeCapture(roleMaster, progressPeerAhead, fix.decisionFrontier),
			},
		},
		{
			name:          "callback error disconnects and suppresses rpc error",
			state:         fix.decisionState(roleSlave),
			msgPayload:    &decisionMessage{Ancestor: false},
			onCompleteErr: errors.New("apply failed"),
			want: expectation{
				// The diverged decision's ack goes out before the failing
				// apply disconnects the initiator.
				sent:        1,
				capture:     peerHandshakeCapture(roleSlave, progressDiverged, fix.decisionFrontier),
				disconnects: 1,
			},
		},
		{
			name:       "ack send failure returns internal error",
			state:      fix.decisionState(roleMaster),
			msgPayload: &decisionMessage{Ancestor: true},
			sendErr:    errors.New("send failed"),
			want: expectation{
				rpc: routeRPCExpectation{
					code: msgjson.RPCInternal,
					msg:  "failed to send mesh decision response",
				},
				capture: peerHandshakeCapture(roleMaster, progressPeerAhead, fix.decisionFrontier),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run(t, tt)
		})
	}
}

func TestNodeRoutesHandshakeAuthPolicy(t *testing.T) {
	routes := newRouteTestNode(modePending, nil, nil).routes()
	if routes[helloRoute].requiresAuth {
		t.Fatalf("%s requires auth", helloRoute)
	}
	if !routes[helloDecisionRoute].requiresAuth {
		t.Fatalf("%s does not require auth", helloDecisionRoute)
	}
}

func decodeClientConnectedResult(t testing.TB, msg *msgjson.Message) []account.AccountID {
	t.Helper()
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %v", resp.Error)
	}
	var result clientConnectedResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal client connected result error: %v", err)
	}
	return result.Connected
}

func TestNodeRoutesHandleClientConnected(t *testing.T) {
	userA, userB := account.AccountID{0x01}, account.AccountID{0x02}
	validQuery := &clientConnectedQuery{Users: []account.AccountID{userA, userB}}

	t.Run("answers from app in every established mode", func(t *testing.T) {
		for _, mode := range []nodeMode{modeEstablishedMaster, modeEstablishedSlaveSyncing, modeEstablishedSlave} {
			active := newTRouteLink(521)
			var gotUsers []account.AccountID
			handler := newRouteTestNode(mode, active, &testMeshApplication{
				clientConnected: func(users []account.AccountID) []account.AccountID {
					gotUsers = users
					return []account.AccountID{userB}
				},
			})

			rpcErr := handleRoutePayload(t, handler, clientConnectedRoute, active, validQuery)
			requireNoRPCError(t, rpcErr)
			if len(gotUsers) != 2 || gotUsers[0] != userA || gotUsers[1] != userB {
				t.Fatalf("mode %s: queried users = %v, want [%v %v]", mode, gotUsers, userA, userB)
			}
			connected := decodeClientConnectedResult(t, requireSent(t, active, 1)[0])
			if len(connected) != 1 || connected[0] != userB {
				t.Fatalf("mode %s: connected = %v, want just %v", mode, connected, userB)
			}
		}
	})

	t.Run("rejects invalid payload before app", func(t *testing.T) {
		active := newTRouteLink(522)
		handler := newRouteTestNode(modeEstablishedSlave, active, &testMeshApplication{
			clientConnected: func([]account.AccountID) []account.AccountID {
				t.Fatalf("unexpected client connected app call")
				return nil
			},
		})

		rpcErr := handleRoutePayload(t, handler, clientConnectedRoute, active, &clientConnectedQuery{})
		requireRPCCode(t, rpcErr, msgjson.RPCParseError)
		requireNoSent(t, active)
	})
}

func TestValidateClientConnectedQuery(t *testing.T) {
	user := account.AccountID{0x01}
	overLimit := make([]account.AccountID, maxClientConnectedUsers+1)
	tests := []struct {
		name    string
		query   *clientConnectedQuery
		wantErr bool
	}{
		{"nil", nil, true},
		{"empty", &clientConnectedQuery{}, true},
		{"over limit", &clientConnectedQuery{Users: overLimit}, true},
		{"at limit ok", &clientConnectedQuery{Users: overLimit[:maxClientConnectedUsers]}, false},
		{"one user ok", &clientConnectedQuery{Users: []account.AccountID{user}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClientConnectedQuery(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateClientConnectedQuery error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateEventBatch(t *testing.T) {
	valid := func(seq uint64) *eventEnvelope {
		return &eventEnvelope{
			Seq:       seq,
			TipHash:   testTipHash(seq),
			MasterTip: eventStreamBatchLimit + 1,
			Kind:      "test",
		}
	}
	atLimit := make([]*eventEnvelope, eventStreamBatchLimit)
	for i := range atLimit {
		atLimit[i] = valid(uint64(i + 1))
	}
	overLimit := make([]*eventEnvelope, eventStreamBatchLimit+1)
	for i := range overLimit {
		overLimit[i] = valid(uint64(i + 1))
	}
	tests := []struct {
		name    string
		batch   *eventBatch
		wantErr bool
	}{
		{"nil", nil, true},
		{"empty", &eventBatch{}, true},
		{"nil entry", &eventBatch{Entries: []*eventEnvelope{nil}}, true},
		{"non-contiguous", &eventBatch{Entries: []*eventEnvelope{valid(1), valid(3)}}, true},
		{"over limit", &eventBatch{Entries: overLimit}, true},
		{"at limit ok", &eventBatch{Entries: atLimit}, false},
		{"one entry ok", &eventBatch{Entries: []*eventEnvelope{valid(1)}}, false},
		{"two contiguous ok", &eventBatch{Entries: []*eventEnvelope{valid(1), valid(2)}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEventBatch(tt.batch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateEventBatch error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// controlAnswer replies to the next signal on a node's control queue and
// records that signal.
type controlAnswer struct {
	got chan meshSignal
}

// answerControlSignal starts a goroutine that answers the next signal on the
// node's control queue with reply. The goroutine stops at test cleanup if no
// signal arrives.
func answerControlSignal(t testing.TB, n *node, reply signalResult) *controlAnswer {
	t.Helper()
	answer := &controlAnswer{got: make(chan meshSignal, 1)}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		select {
		case queued := <-n.control.testQueue():
			answer.got <- queued.signal
			if queued.reply != nil {
				queued.reply <- reply
			}
		case <-done:
		}
	}()
	return answer
}

// wait returns the answered signal.
func (a *controlAnswer) wait(t testing.TB) meshSignal {
	t.Helper()
	select {
	case sig := <-a.got:
		return sig
	case <-time.After(time.Second):
		t.Fatalf("no control signal received")
		return nil
	}
}

// none fails the test if a signal was answered.
func (a *controlAnswer) none(t testing.TB) {
	t.Helper()
	select {
	case sig := <-a.got:
		t.Fatalf("unexpected control signal %T", sig)
	default:
	}
}

func decodeResultInto(t testing.TB, msg *msgjson.Message, result any) {
	t.Helper()
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("Response error: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected response error: %v", resp.Error)
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
}

func TestNodeRoutesHandleStreamSubscribe(t *testing.T) {
	tip := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	subscribeAt := func(pos *db.EventLogPosition) *streamSubscribe {
		return &streamSubscribe{Frontier: toFrontierMessage(pos)}
	}

	t.Run("starts the stream and reports the master tip", func(t *testing.T) {
		active := newTRouteLink(801)
		handler := newRouteTestNode(modeEstablishedMaster, active, nil)
		handler.eventLogReader = &testEventLogReader{frontier: tip}
		answer := answerControlSignal(t, handler, signalResult{
			handled: true,
			state:   nodeState{mode: modeEstablishedMaster},
		})

		rpcErr := handleRoutePayload(t, handler, streamSubscribeRoute, active, subscribeAt(tip))
		requireNoRPCError(t, rpcErr)
		sig, ok := answer.wait(t).(streamSubscribeSignal)
		if !ok || sig.connID != active.ID() || sig.frontier.Seq != tip.Seq {
			t.Fatalf("control signal = %+v, want stream subscribe on conn %d at seq %d", sig, active.ID(), tip.Seq)
		}
		var result streamSubscribeResult
		decodeResultInto(t, requireSent(t, active, 1)[0], &result)
		if result.MasterTip != tip.Seq {
			t.Fatalf("master tip = %d, want %d", result.MasterTip, tip.Seq)
		}
	})

	t.Run("not the master gets try again later", func(t *testing.T) {
		active := newTRouteLink(802)
		handler := newRouteTestNode(modeEstablishedSlave, active, nil)
		handler.eventLogReader = &testEventLogReader{frontier: tip}
		answerControlSignal(t, handler, signalResult{state: nodeState{mode: modeEstablishedSlave}})

		rpcErr := handleRoutePayload(t, handler, streamSubscribeRoute, active, subscribeAt(tip))
		requireRPCOutcome(t, rpcErr, msgjson.TryAgainLaterError, "not the established master")
		requireNoSent(t, active)
	})

	t.Run("control error is internal", func(t *testing.T) {
		active := newTRouteLink(803)
		handler := newRouteTestNode(modeEstablishedMaster, active, nil)
		handler.eventLogReader = &testEventLogReader{frontier: tip}
		answerControlSignal(t, handler, signalResult{err: errors.New("boom")})

		rpcErr := handleRoutePayload(t, handler, streamSubscribeRoute, active, subscribeAt(tip))
		requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "stream subscribe failed")
		requireNoSent(t, active)
	})

	t.Run("rejects a frontier beyond the tip before the state machine", func(t *testing.T) {
		active := newTRouteLink(804)
		handler := newRouteTestNode(modeEstablishedMaster, active, nil)
		handler.eventLogReader = &testEventLogReader{frontier: tip}
		answer := answerControlSignal(t, handler, signalResult{
			handled: true,
			state:   nodeState{mode: modeEstablishedMaster},
		})

		beyond := &db.EventLogPosition{Seq: 11, TipHash: testTipHash(11)}
		rpcErr := handleRoutePayload(t, handler, streamSubscribeRoute, active, subscribeAt(beyond))
		requireRPCOutcome(t, rpcErr, msgjson.SubscribeRejectedError, "beyond this node's tip")
		requireNoSent(t, active)
		answer.none(t)
	})
}

func TestNodeRoutesHandleSnapshotRequest(t *testing.T) {
	t.Run("starts the snapshot and acks", func(t *testing.T) {
		active := newTRouteLink(811)
		handler := newRouteTestNode(modeEstablishedMaster, active, nil)
		answer := answerControlSignal(t, handler, signalResult{
			handled: true,
			state:   nodeState{mode: modeEstablishedMaster},
		})

		rpcErr := handleRoutePayload(t, handler, snapshotRequestRoute, active, &snapshotRequest{})
		requireNoRPCError(t, rpcErr)
		sig, ok := answer.wait(t).(snapshotRequestSignal)
		if !ok || sig.connID != active.ID() {
			t.Fatalf("control signal = %+v, want snapshot request on conn %d", sig, active.ID())
		}
		requireOneAck(t, active)
	})

	t.Run("not the master gets try again later", func(t *testing.T) {
		active := newTRouteLink(812)
		handler := newRouteTestNode(modePreparingMaster, active, nil)
		answerControlSignal(t, handler, signalResult{state: nodeState{mode: modePreparingMaster}})

		rpcErr := handleRoutePayload(t, handler, snapshotRequestRoute, active, &snapshotRequest{})
		requireRPCOutcome(t, rpcErr, msgjson.TryAgainLaterError, "not the established master")
		requireNoSent(t, active)
	})
}

func TestNodeRoutesHandleSnapshotChunk(t *testing.T) {
	seedFinished := func(seed *seedAttempt) bool {
		seed.mtx.Lock()
		defer seed.mtx.Unlock()
		return seed.finished
	}

	t.Run("no seed in progress is internal", func(t *testing.T) {
		active := newTRouteLink(831)
		handler := newRouteTestNode(modeEstablishedSlaveSyncing, active, nil)

		rpcErr := handleRoutePayload(t, handler, snapshotChunkRoute, active, &snapshotChunk{Bytes: []byte("abc")})
		requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "unsolicited snapshot chunk")
		requireNoSent(t, active)
	})

	t.Run("buffers the chunk and acks", func(t *testing.T) {
		active := newTRouteLink(832)
		handler := newRouteTestNode(modeEstablishedSlaveSyncing, active, nil)
		seed := newSeedAttempt(context.Background(), nil, dex.Disabled)
		handler.control.currentState().activeConn.seed.Store(seed)

		rpcErr := handleRoutePayload(t, handler, snapshotChunkRoute, active, &snapshotChunk{Bytes: []byte("abc")})
		requireNoRPCError(t, rpcErr)
		requireOneAck(t, active)
		if got := seed.rx.buf.String(); got != "abc" {
			t.Fatalf("buffered %q, want %q", got, "abc")
		}
		if seedFinished(seed) {
			t.Fatalf("seed finished after a chunk that was not the last")
		}
	})

	t.Run("stray chunk after the final chunk does not fail the seed", func(t *testing.T) {
		active := newTRouteLink(833)
		handler := newRouteTestNode(modeEstablishedSlaveSyncing, active, nil)
		seed := newSeedAttempt(context.Background(), nil, dex.Disabled)
		seed.rx.done = true
		handler.control.currentState().activeConn.seed.Store(seed)

		rpcErr := handleRoutePayload(t, handler, snapshotChunkRoute, active, &snapshotChunk{Bytes: []byte("abc")})
		requireRPCOutcome(t, rpcErr, msgjson.RPCInternal, "after final chunk")
		requireNoSent(t, active)
		if seedFinished(seed) {
			t.Fatalf("stray chunk failed the seed")
		}
	})
}
