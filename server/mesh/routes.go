// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"fmt"

	"decred.org/dcrdex/dex/msgjson"
)

// link is a connection to the peer, as the routes and the handshake use it.
// It is implemented by rpcConn, but we use an interface for easier testing.
type link interface {
	ID() uint64
	Addr() string
	Send(*msgjson.Message) error
	Request(context.Context, string, any, any) error
	Authorize()
	Done() <-chan struct{}
	Disconnect()
}

// meshHandler is the function signature for a route handler. A return value
// that is not nil is sent to the peer as the error response. A nil return
// means the handler answered itself, will answer later from another
// goroutine, or closed the connection without an answer.
type meshHandler func(context.Context, link, *msgjson.Message) *msgjson.Error

// meshRoute is how an incoming message is handled and whether it
// requires the connection to have completed a handshake.
type meshRoute struct {
	handler      meshHandler
	requiresAuth bool
}

const (
	helloRoute              = "mesh_hello"
	helloDecisionRoute      = "mesh_hello_decision"
	commandForwardRoute     = "command_forward"
	commandFailureRoute     = "command_failure"
	commandResultRoute      = "command_result"
	clientProxyMessageRoute = "client_proxy_message"
	clientConnectedRoute    = "client_connected"
	eventEnvelopeRoute      = "event"
	streamSubscribeRoute    = "stream_subscribe"
	masterHandoffRoute      = "master_handoff"
	snapshotRequestRoute    = "snapshot_request"
	snapshotChunkRoute      = "snapshot_chunk"
)

// routes returns the node's route table for incoming messages.
func (n *node) routes() map[string]meshRoute {
	if n.routeTable != nil {
		return n.routeTable
	}
	n.routeTable = map[string]meshRoute{
		helloRoute:              {handler: n.handleHello},
		helloDecisionRoute:      {handler: n.handleDecision, requiresAuth: true},
		commandForwardRoute:     n.activePeerRoute(n.handleCommandForward, nodeMode.canExecuteCommands),
		commandFailureRoute:     n.activePeerRoute(n.handleCommandFailure, nodeMode.canForwardCommands),
		commandResultRoute:      n.activePeerRoute(n.handleCommandResult, nodeMode.canForwardCommands),
		clientProxyMessageRoute: n.activePeerRoute(n.handleClientProxyMessage, nodeMode.canRelayClientMessages),
		clientConnectedRoute:    n.activePeerRoute(n.handleClientConnected, nodeMode.canExchangeClientConnectivity),
		eventEnvelopeRoute:      n.activePeerRoute(n.handleEventEnvelope, nodeMode.canReceiveEventStream),
		masterHandoffRoute:      n.activePeerRoute(n.handleMasterHandoff, nodeMode.canAcceptMasterHandoff),
		snapshotChunkRoute:      n.activePeerRoute(n.handleSnapshotChunk, nodeMode.canReceiveEventStream),

		// stream_subscribe and snapshot_request are gated by the state machine,
		// not by activePeerRoute. A request that races the master's adoption
		// of the handshake must get a retryable rejection, not an unauthorized
		// error.
		streamSubscribeRoute: {requiresAuth: true, handler: n.handleStreamSubscribe},
		snapshotRequestRoute: {requiresAuth: true, handler: n.handleSnapshotRequest},
	}
	return n.routeTable
}

// peerMeshHandler is the function signature for a route handler that only the
// active peer may use.
type peerMeshHandler func(context.Context, link, *nodeConn, *msgjson.Message) *msgjson.Error

// activePeerRoute wraps a handler in a route that only the active peer may
// use, and only while allowed reports true for the node's mode. Any other
// request is refused with UnauthorizedConnection.
func (n *node) activePeerRoute(handler peerMeshHandler, allowed func(nodeMode) bool) meshRoute {
	return meshRoute{
		requiresAuth: true,
		handler: func(ctx context.Context, conn link, msg *msgjson.Message) *msgjson.Error {
			state := n.control.currentState()
			if !allowed(state.mode) || state.activeConn == nil || state.activeConn.link == nil ||
				state.activeConn.link.ID() != conn.ID() {
				return msgjson.NewError(msgjson.UnauthorizedConnection,
					"mesh route requires active peer connection in an allowed local state")
			}
			return handler(ctx, conn, state.activeConn, msg)
		},
	}
}

// decodeRoutePayload unmarshals the request payload into a T and validates
// it, if validate is given. A failure is answered with RPCParseError, and
// payloadName identifies the payload in that error.
func decodeRoutePayload[T any](msg *msgjson.Message, payloadName string, validate func(*T) error) (*T, *msgjson.Error) {
	var payload T
	if err := msg.Unmarshal(&payload); err != nil {
		return nil, msgjson.NewError(msgjson.RPCParseError, "error parsing %s", payloadName)
	}
	if validate != nil {
		if err := validate(&payload); err != nil {
			return nil, msgjson.NewError(msgjson.RPCParseError, "invalid %s: %v", payloadName, err)
		}
	}
	return &payload, nil
}

// sendRouteReply sends the response to request reqID. The response carries
// result on success or respErr on failure. If it cannot be encoded or sent,
// the returned error names the route as routeName.
func sendRouteReply(conn link, reqID uint64, result any, respErr *msgjson.Error, routeName string) *msgjson.Error {
	resp, err := msgjson.NewResponse(reqID, result, respErr)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "failed to encode %s response", routeName)
	}
	if err := conn.Send(resp); err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "failed to send %s response", routeName)
	}
	return nil
}

// sendRouteResponse sends result as the response to request reqID.
func sendRouteResponse(conn link, reqID uint64, result any, routeName string) *msgjson.Error {
	return sendRouteReply(conn, reqID, result, nil, routeName)
}

// sendRouteErrorResponse sends respErr as the response to request reqID.
func sendRouteErrorResponse(conn link, reqID uint64, respErr *msgjson.Error, routeName string) *msgjson.Error {
	return sendRouteReply(conn, reqID, nil, respErr, routeName)
}

// sendRouteAck sends an empty response to request reqID.
func sendRouteAck(conn link, reqID uint64, routeName string) *msgjson.Error {
	return sendRouteResponse(conn, reqID, struct{}{}, routeName)
}

// handleHello decodes the peer's hello and passes it to the handshake
// sessions. A bad payload is answered with a parse error.
func (n *node) handleHello(ctx context.Context, conn link, msg *msgjson.Message) *msgjson.Error {
	hello, msgErr := decodeRoutePayload[helloMessage](msg, "mesh hello request", nil)
	if msgErr != nil {
		return msgErr
	}

	return n.handshakes.handleHello(ctx, conn, msg.ID, hello)
}

// handleDecision completes a handshake that waited for the initiator's
// decision. It hands the result to the state machine and answers with an
// ack, or with MeshAlreadyConnectedError when the connection is not adopted.
func (n *node) handleDecision(ctx context.Context, conn link, msg *msgjson.Message) *msgjson.Error {
	decision, msgErr := decodeRoutePayload[decisionMessage](msg, "mesh decision request", nil)
	if msgErr != nil {
		return msgErr
	}

	return n.handshakes.handleDecision(ctx, conn, msg.ID, decision)
}

// requestServe sends sig to the state machine and reports whether the
// signal has been handled. TryAgainLater is returned if the node is not
// in the correct mode to handle the signal.
func (n *node) requestServe(sig meshSignal, routeName string) *msgjson.Error {
	res, err := n.control.send(sig)
	if err == nil {
		err = res.err
	}
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "%s failed: %v", routeName, err)
	}
	if !res.handled {
		return msgjson.NewError(msgjson.TryAgainLaterError,
			"%s: this node is not the established master on this connection (mode %s)", routeName, res.state.mode)
	}
	return nil
}

// handleSnapshotRequest handles a slave's request for a snapshot of the
// node's state. If the node can handle the request, it is acked, and the
// transfer is initiated.
func (n *node) handleSnapshotRequest(_ context.Context, conn link, msg *msgjson.Message) *msgjson.Error {
	if _, msgErr := decodeRoutePayload[snapshotRequest](msg, "snapshot request", nil); msgErr != nil {
		return msgErr
	}

	// The state machine will kick off the snapshot transfer.
	if msgErr := n.requestServe(snapshotRequestSignal{connID: conn.ID()}, "snapshot request"); msgErr != nil {
		return msgErr
	}

	return sendRouteAck(conn, msg.ID, "snapshot request")
}

// handleSnapshotChunk appends one snapshot chunk to the seed in progress on
// this connection. It answers each accepted chunk with an ack. After the final
// chunk it loads the snapshot into the DB in a new goroutine.
func (n *node) handleSnapshotChunk(_ context.Context, conn link, peerConn *nodeConn, msg *msgjson.Message) *msgjson.Error {
	chunk, msgErr := decodeRoutePayload[snapshotChunk](msg, "snapshot chunk", nil)
	if msgErr != nil {
		return msgErr
	}

	// A chunk is only accepted while a seed attempt is running on this
	// connection.
	seed := peerConn.seed.Load()
	if seed == nil {
		return msgjson.NewError(msgjson.RPCInternal, "unsolicited snapshot chunk: no seed in progress on this connection")
	}
	if seed.rx.transferComplete() {
		return msgjson.NewError(msgjson.RPCInternal, "snapshot chunk after final chunk")
	}

	last, err := seed.rx.receiveChunk(chunk)
	if err != nil {
		seed.fail(fmt.Errorf("snapshot receive: %w", err))
		return msgjson.NewError(msgjson.RPCInternal, "snapshot receive failed: %v", err)
	}

	if last {
		go seed.runLoad()
	}

	msgErr = sendRouteResponse(conn, msg.ID, &eventAck{}, "snapshot chunk")
	return msgErr
}
