// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"decred.org/dcrdex/server/db"
)

const (
	// meshPingPeriod is the period at which a mesh node will ping its peer.
	meshPingPeriod = 20 * time.Second

	// defaultRequestTimeout is the default timeout for a request to the mesh peer.
	defaultRequestTimeout = 10 * time.Second
)

var (
	connIDCounter    uint64
	requestIDCounter uint64
)

func nextConnID() uint64 {
	return atomic.AddUint64(&connIDCounter, 1)
}

func nextRequestID() uint64 {
	return atomic.AddUint64(&requestIDCounter, 1)
}

// rpcConn wraps the websocket connection to the mesh peer. It also
// handles request/response bookkeeping, and dispatches inbound requests
// to the request handlers.
type rpcConn struct {
	*ws.WSLink

	wsConn ws.Connection

	id     uint64
	routes map[string]meshRoute
	authed atomic.Bool

	ctx           context.Context
	cancelContext context.CancelFunc

	reqMtx       sync.Mutex
	respHandlers map[uint64]chan<- *msgjson.Message
}

func newRPCConn(ctx context.Context, addr string, wsConn ws.Connection, routes map[string]meshRoute, log dex.Logger) *rpcConn {
	connCtx, cancel := context.WithCancel(ctx)
	conn := &rpcConn{
		wsConn:        wsConn,
		id:            nextConnID(),
		routes:        routes,
		ctx:           connCtx,
		cancelContext: cancel,
		respHandlers:  make(map[uint64]chan<- *msgjson.Message),
	}
	conn.WSLink = ws.NewWSLink(addr, wsConn, meshPingPeriod, conn.handleMessage, log)
	conn.WSLink.SetReadLimit(meshReadLimit)
	return conn
}

func (c *rpcConn) connect() (*sync.WaitGroup, error) {
	wg, err := c.WSLink.Connect(c.ctx)
	if err != nil {
		c.cancelContext()
		_ = c.wsConn.Close()
		return nil, err
	}
	c.watchDisconnect()
	return wg, nil
}

func (c *rpcConn) watchDisconnect() {
	go func() {
		select {
		case <-c.ctx.Done():
		case <-c.Done():
			c.cancelContext()
		}
	}()
}

func (c *rpcConn) ID() uint64 {
	return c.id
}

func (c *rpcConn) Authorize() {
	c.authed.Store(true)
}

func (c *rpcConn) Authed() bool {
	return c.authed.Load()
}

// peerRPCError is a peer's RPC failure, decoded from their response.
type peerRPCError struct {
	Code    int
	Message string
}

// Error implements the error interface.
func (e *peerRPCError) Error() string {
	return fmt.Sprintf("error code %d: %s", e.Code, e.Message)
}

// MsgError returns the peer error as *msgjson.Error so callers can relay it.
func (e *peerRPCError) MsgError() *msgjson.Error {
	return msgjson.NewError(e.Code, "%s", e.Message)
}

// Request sends a request on route and waits for the peer's response. The
// result is unmarshaled into response when it is non-nil; a peer-reported
// failure is returned as a *peerRPCError. If ctx carries no deadline,
// defaultRequestTimeout applies. The wait ends if the link drops.
func (c *rpcConn) Request(ctx context.Context, route string, payload any, response any) error {
	reqID := nextRequestID()
	req, err := msgjson.NewRequest(reqID, route, payload)
	if err != nil {
		return err
	}

	rawReq, err := json.Marshal(req)
	if err != nil {
		return err
	}

	respChan := make(chan *msgjson.Message, 1)
	c.reqMtx.Lock()
	c.respHandlers[reqID] = respChan
	c.reqMtx.Unlock()
	defer c.clearRespHandler(reqID)

	if err := c.SendRaw(rawReq); err != nil {
		return err
	}

	reqCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		reqCtx, cancel = context.WithTimeout(ctx, defaultRequestTimeout)
	}
	defer cancel()

	select {
	case respMsg := <-respChan:
		resp, err := respMsg.Response()
		if err != nil {
			return err
		}
		if resp.Error != nil {
			return &peerRPCError{Code: resp.Error.Code, Message: resp.Error.Message}
		}
		if response != nil {
			return json.Unmarshal(resp.Result, response)
		}
		return nil
	case <-reqCtx.Done():
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("timed out waiting for route %s response", route)
		}
		return reqCtx.Err()
	case <-c.Done():
		return fmt.Errorf("mesh link disconnected during route %s", route)
	}
}

// classifyHelloPeerError wraps a permanent hello refusal from the peer as
// errPeerIncompatible, if required.
// AuthenticationError means our hello did not verify
// MeshIncompatibleLogError means our event log is below the peer's snapshot anchor,
// so we cannot stream events from them.
func classifyHelloPeerError(err error, localFrontier *db.EventLogPosition) error {
	var peerErr *peerRPCError
	if !errors.As(err, &peerErr) {
		return err
	}
	switch peerErr.Code {
	case msgjson.AuthenticationError:
		return wrapPeerIncompatible(peerErr)
	case msgjson.MeshIncompatibleLogError:
		err := wrapPeerIncompatible(peerErr)
		if token := forkResetToken(localFrontier); token != "" {
			err = fmt.Errorf("%w. This node's event log ends at %s. Back up the database (pg_dump),"+
				"then restart with --meshforkreset=%s to wipe it and reseed from the peer",
				err, localFrontier, token)
		}
		return err
	}
	return err
}

func (c *rpcConn) requestHello(ctx context.Context, req *helloMessage) (*helloResponse, error) {
	var resp helloResponse
	if err := c.Request(ctx, helloRoute, req, &resp); err != nil {
		return nil, classifyHelloPeerError(err, fromFrontierMessage(req.Frontier))
	}
	return &resp, nil
}

// classifyDecisionPeerError wraps a MeshAlreadyConnectedError from the peer
// as errPeerAlreadyConnected. Other errors are returned unchanged.
func classifyDecisionPeerError(err error) error {
	var peerErr *peerRPCError
	if errors.As(err, &peerErr) && peerErr.Code == msgjson.MeshAlreadyConnectedError {
		return fmt.Errorf("%w: %v", errPeerAlreadyConnected, peerErr)
	}
	return err
}

func (c *rpcConn) requestDecision(ctx context.Context, req *decisionMessage) error {
	return classifyDecisionPeerError(c.Request(ctx, helloDecisionRoute, req, nil))
}

// handleMessage handles an incoming message from the mesh peer.
func (c *rpcConn) handleMessage(msg *msgjson.Message) *msgjson.Error {
	switch msg.Type {
	case msgjson.Response:
		if msg.ID == 0 {
			return msgjson.NewError(msgjson.RPCParseError, "response id cannot be 0")
		}
		cb := c.takeRespHandler(msg.ID)
		if cb == nil {
			// Response arrived after the requester gave up, so silently drop it.
			return nil
		}
		select {
		case cb <- msg:
		default:
		}
		return nil

	case msgjson.Request, msgjson.Notification:
		route := c.routes[msg.Route]
		if route.handler == nil {
			return msgjson.NewError(msgjson.RPCUnknownRoute, "unknown route")
		}
		if route.requiresAuth && !c.Authed() {
			return msgjson.NewError(msgjson.UnauthorizedConnection,
				"cannot use route '%s' on an unauthorized mesh connection", msg.Route)
		}
		return route.handler(c.ctx, c, msg)

	default:
		return msgjson.NewError(msgjson.UnknownMessageType, "unknown message type")
	}
}

// takeRespHandler removes and returns the response channel registered for id,
// or nil if no handler is registered.
func (c *rpcConn) takeRespHandler(id uint64) chan<- *msgjson.Message {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.respHandlers[id]
	if ok {
		delete(c.respHandlers, id)
	}
	return cb
}

func (c *rpcConn) clearRespHandler(id uint64) {
	c.reqMtx.Lock()
	delete(c.respHandlers, id)
	c.reqMtx.Unlock()
}

// nodeConn holds a peer connection, its node IDs, and any snapshot download.
type nodeConn struct {
	// link is the websocket link to the peer.
	link link

	// peerNodeID is the nodeID of the peer.
	peerNodeID string

	// initiatorNodeID identifies the node that opened the connection.
	// It breaks ties between parallel connections.
	initiatorNodeID string

	// seed is the in-progress snapshot download on this conn.
	// It is set on the slave when the slave starts a snapshot download,
	// and cleared after seeding is complete.
	seed atomic.Pointer[seedAttempt]
}

func newNodeConn(conn link, peerNodeID, initiatorNodeID string) *nodeConn {
	return &nodeConn{
		link:            conn,
		peerNodeID:      peerNodeID,
		initiatorNodeID: initiatorNodeID,
	}
}

func (c *nodeConn) ID() uint64 {
	if c == nil || c.link == nil {
		return 0
	}
	return c.link.ID()
}

func (c *nodeConn) String() string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("nodeConn{id=%d peer=%s initiator=%s}",
		c.ID(), c.peerNodeID, c.initiatorNodeID)
}

// sameConn compares two nodeConns for equality.
func sameConn(a, b *nodeConn) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	case a.link == nil || b.link == nil:
		return a == b
	default:
		return a.ID() == b.ID()
	}
}
