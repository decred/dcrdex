// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"fmt"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
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

// handleCommandForward accepts a command that the slave forwarded. It checks
// the payload and runs the command in a new goroutine, which sends the
// response when the command is finished. A bad payload is refused at once.
func (n *node) handleCommandForward(_ context.Context, conn link, _ *nodeConn, msg *msgjson.Message) *msgjson.Error {
	cmd, msgErr := decodeRoutePayload(msg, "forwarded command", validateCommandForward)
	if msgErr != nil {
		return msgErr
	}
	// The executor can send requests to the slave on this same link, for
	// example command_result, command_failure or a relayed client message.
	// This connection's read loop reads their responses. The command must
	// run off the read loop, or the loop would wait on itself.
	go n.runForwardedCommand(conn, msg.ID, cmd.CommandID, CommandRequest{
		Kind: cmd.Kind,
		User: cmd.User,
		Msg:  cmd.Msg,
	})

	return nil
}

// runForwardedCommand runs a forwarded command and answers the forward
// request.
func (n *node) runForwardedCommand(conn link, reqID uint64, commandID string, req CommandRequest) {
	// The command runs under the node run context. The handler context is
	// the connection context, and a peer disconnect must not abort a command
	// in the middle of an apply.
	//
	// Errors in the executor are sent as an error response.
	if msgErr := n.app.executeForwardedCommand(n.runContext, commandID, req); msgErr != nil {
		if sendErr := sendRouteErrorResponse(conn, reqID, msgErr, "forwarded command"); sendErr != nil {
			n.log.Debugf("failed to send forwarded command error response: %v", sendErr)
		}
		return
	}

	// Just ack here. The actual response is sent in an event envelope, command_result or
	// command_failure.
	if err := sendRouteAck(conn, reqID, "forwarded command"); err != nil {
		n.log.Debugf("failed to send forwarded command ack: %v", err)
	}
}

// handleCommandFailure handles a command_failure sent from the master
// to the slave. This is an async response to a command_forward.
func (n *node) handleCommandFailure(_ context.Context, conn link, _ *nodeConn, msg *msgjson.Message) *msgjson.Error {
	fail, msgErr := decodeRoutePayload(msg, "command failure", validateCommandFailure)
	if msgErr != nil {
		return msgErr
	}

	n.app.receiveCommandFailure(fail.CommandID, fail.Error)

	return sendRouteAck(conn, msg.ID, "command failure")
}

// handleCommandResult handles a command_result sent from the master to the
// slave. This is an async response to a command_forward.
func (n *node) handleCommandResult(_ context.Context, conn link, _ *nodeConn, msg *msgjson.Message) *msgjson.Error {
	result, msgErr := decodeRoutePayload(msg, "command result", validateCommandResult)
	if msgErr != nil {
		return msgErr
	}

	n.app.receiveCommandResult(result.CommandID, result.Result)

	return sendRouteAck(conn, msg.ID, "command result")
}

// handleEventEnvelope applies a batch of streamed events from the master in
// order. It answers with an eventAck after every entry was applied, or with
// an error at the first entry that failed to apply.
func (n *node) handleEventEnvelope(ctx context.Context, conn link, peerConn *nodeConn, msg *msgjson.Message) *msgjson.Error {
	batch, msgErr := decodeRoutePayload(msg, "event batch", validateEventBatch)
	if msgErr != nil {
		return msgErr
	}

	if !n.eventsGateOpen() {
		return msgjson.NewError(msgjson.RPCInternal,
			"event envelope before this node was ready for events")
	}

	for _, entry := range batch.Entries {
		if err := n.applyInboundEventEnvelope(peerConn, entry); err != nil {
			return msgjson.NewError(msgjson.RPCInternal, "event apply failed: %v", err)
		}
	}

	return sendRouteResponse(conn, msg.ID, &eventAck{}, "event")
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

// handleStreamSubscribe answers a subscription to the event stream. It first
// checks the slave's frontier against the log. Then the state machine starts
// the stream, and the response carries this node's current tip.
func (n *node) handleStreamSubscribe(ctx context.Context, conn link, msg *msgjson.Message) *msgjson.Error {
	sub, msgErr := decodeRoutePayload(msg, "stream subscribe", validateStreamSubscribe)
	if msgErr != nil {
		return msgErr
	}

	frontier := fromFrontierMessage(sub.Frontier)

	if msgErr := n.validateSubscribeAgainstLog(ctx, frontier); msgErr != nil {
		if msgErr.Code == msgjson.SubscribeRejectedError {
			n.log.Warnf("Rejecting stream subscription from %s: %v", conn.Addr(), msgErr)
		}
		return msgErr
	}

	sig := streamSubscribeSignal{connID: conn.ID(), frontier: frontier}
	if msgErr := n.requestServe(sig, "stream subscribe"); msgErr != nil {
		return msgErr
	}

	// The stream is started. Tell the slave where this node's log ends.
	local, err := n.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "event log frontier: %v", err)
	}

	return sendRouteResponse(conn, msg.ID, &streamSubscribeResult{MasterTip: local.Seq}, "stream subscribe")
}

// validateSubscribeAgainstLog checks that this node's log can serve the event
// stream from peerFrontier. A SubscribeRejectedError will cause the slave to halt.
func (n *node) validateSubscribeAgainstLog(ctx context.Context, peerFrontier *db.EventLogPosition) *msgjson.Error {
	localFrontier, err := n.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "event log frontier: %v", err)
	}

	if peerFrontier.Seq > 0 {
		return n.validateResumePosition(ctx, peerFrontier, localFrontier)
	}

	return n.validateFullReplay(ctx, localFrontier)
}

// validateResumePosition checks that peerFrontier is a position in this
// node's log with the same tip hash.
func (n *node) validateResumePosition(ctx context.Context, peerFrontier, localFrontier *db.EventLogPosition) *msgjson.Error {
	if peerFrontier.Seq > localFrontier.Seq {
		return msgjson.NewError(msgjson.SubscribeRejectedError,
			"subscribe frontier %d beyond this node's tip %d", peerFrontier.Seq, localFrontier.Seq)
	}
	tipHash := localFrontier.TipHash
	if peerFrontier.Seq < localFrontier.Seq {
		entry, err := entryAt(ctx, n.eventLogReader, peerFrontier.Seq)
		if err != nil {
			return msgjson.NewError(msgjson.RPCInternal, "event log read: %v", err)
		}
		if entry == nil {
			return msgjson.NewError(msgjson.SubscribeRejectedError,
				"subscribe frontier %d is not in this node's replayable history", peerFrontier.Seq)
		}
		tipHash = entry.TipHash
	}
	if !bytes.Equal(tipHash, peerFrontier.TipHash) {
		return msgjson.NewError(msgjson.SubscribeRejectedError,
			"subscribe frontier %d tip hash does not match this node's log (diverged)", peerFrontier.Seq)
	}
	return nil
}

// validateFullReplay checks that this node's log can be replayed from the
// start. The log must be empty, or begin at sequence 1 with a real event. A
// log that begins at an anchor can only be joined from a snapshot.
func (n *node) validateFullReplay(ctx context.Context, localFrontier *db.EventLogPosition) *msgjson.Error {
	if localFrontier.Seq == 0 {
		return nil
	}
	entry, err := entryAt(ctx, n.eventLogReader, 1)
	if err != nil {
		return msgjson.NewError(msgjson.RPCInternal, "event log read: %v", err)
	}
	if entry == nil {
		return msgjson.NewError(msgjson.SubscribeRejectedError,
			"this node's event log does not begin at sequence 1 and its earlier state is "+
				"not replayable; the peer must seed from a snapshot instead")
	}
	if db.IsEventLogAnchorKind(entry.Kind) {
		return msgjson.NewError(msgjson.SubscribeRejectedError,
			"this node's event log begins at a %q anchor and its earlier state is not "+
				"replayable; the peer must seed from a snapshot instead", entry.Kind)
	}
	return nil
}

// entryAt returns the log entry at seq, or nil when the log has none. The
// entry is missing when it was never written or was pruned behind an anchor.
// Seq 0 is the empty log and has no entry.
func entryAt(ctx context.Context, reader db.EventLogReader, seq uint64) (*db.EventLogEntry, error) {
	if seq == 0 {
		return nil, nil
	}
	entries, err := reader.EventLogEntriesAfter(ctx, seq-1, 1)
	if err != nil {
		return nil, err
	}
	// entries[0].Seq != seq  means the entry was pruned behind an anchor.
	if len(entries) == 0 || entries[0] == nil || entries[0].Seq != seq {
		return nil, nil
	}
	return entries[0], nil
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
