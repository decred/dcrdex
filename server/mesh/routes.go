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
