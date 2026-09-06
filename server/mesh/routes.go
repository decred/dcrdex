// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"

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
