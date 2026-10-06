// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

const defaultPendingHandshakeTimeout = 2 * time.Minute

// handshakeSessions handles inbound handshakes and stores the hello state
// while waiting for a decision from an initiator with a longer log.
type handshakeSessions struct {
	log  dex.Logger
	svc  *handshakeService
	node *node

	pendingTimeout time.Duration

	pendingMtx sync.Mutex
	pending    map[uint64]*pendingHandshakeSession
}

type pendingHandshakeSession struct {
	pending *handshakeResult
	timer   *time.Timer
}

func newHandshakeSessions(log dex.Logger, svc *handshakeService, node *node) *handshakeSessions {
	return &handshakeSessions{
		log:            log,
		svc:            svc,
		node:           node,
		pendingTimeout: defaultPendingHandshakeTimeout,
		pending:        make(map[uint64]*pendingHandshakeSession),
	}
}

func sendHelloResponse(conn link, reqID uint64, resp *helloResponse) *msgjson.Error {
	return sendRouteResponse(conn, reqID, resp, "mesh hello")
}

// handleHello handles the hello message from the peer.
func (h *handshakeSessions) handleHello(ctx context.Context, conn link, reqID uint64, hello *helloMessage) *msgjson.Error {
	if h.hasPending(conn.ID()) {
		return msgjson.NewError(msgjson.RPCInternal, "mesh connection already has pending decision")
	}
	if state := h.node.control.currentState(); state.activeConn != nil && state.activeConn.link != nil &&
		state.activeConn.link.ID() == conn.ID() {
		return msgjson.NewError(msgjson.RPCInternal, "mesh hello on the active connection")
	}

	if err := h.svc.validatePeerHello(hello); err != nil {
		h.log.Warnf("Rejecting mesh hello from %s: %v", conn.Addr(), err)
		return msgjson.NewError(msgjson.AuthenticationError, "mesh hello verification failed: %v", err)
	}

	respHello, result, err := h.svc.processHello(ctx, hello)
	if err != nil {
		h.log.Warnf("Rejecting mesh hello from %s: %v", conn.Addr(), err)
		if errors.Is(err, errPeerBelowAnchor) {
			return msgjson.NewError(msgjson.MeshIncompatibleLogError,
				"mesh hello rejected, the initiator's event log ends below the responder's "+
					"snapshot anchor, so the initiator cannot join from that position")
		}
		return msgjson.NewError(msgjson.RPCInternal, "mesh hello processing failed: %v", err)
	}

	conn.Authorize()

	switch result.progress {
	case progressPeerAhead:
		// The initiator decides, so we store the result until the decision
		// message arrives.
		session := h.addPending(conn, result)
		msgErr := sendHelloResponse(conn, reqID, respHello)
		if msgErr != nil {
			h.clearPending(conn.ID(), session)
		}
		return msgErr
	case progressDiverged:
		// The apply may halt this node, so we send the response first so the
		// initiator can see the result.
		msgErr := sendHelloResponse(conn, reqID, respHello)
		if err := h.node.applyHandshakeResult(ctx, conn, result, hello.NodeID); err != nil {
			conn.Disconnect()
		}
		return msgErr
	}

	// progress is equal or we are ahead, so the handshake can be resolved.

	err = h.node.applyHandshakeResult(ctx, conn, result, hello.NodeID)
	switch {
	case err == nil:
		msgErr := sendHelloResponse(conn, reqID, respHello)
		if msgErr == nil {
			h.log.Infof("Accepted mesh hello from peer %s", hello.NodeID)
		}
		return msgErr
	case errors.Is(err, errConnNotAdopted):
		// Answer before disconnect so the dialer can tell a live peer from
		// silence.
		respHello.NotAdopted = true
		if msgErr := sendHelloResponse(conn, reqID, respHello); msgErr != nil {
			h.log.Warnf("Failed to send mesh hello adoption rejection to %s: %v", conn.Addr(), msgErr)
		}
	}

	conn.Disconnect()
	return nil
}

// handleDecision handles the decision message from the peer. It completes
// the handshake for a hello that was stored because the peer was ahead.
func (h *handshakeSessions) handleDecision(ctx context.Context, conn link, reqID uint64, decision *decisionMessage) *msgjson.Error {
	session := h.takePending(conn.ID())
	if session == nil {
		// The stored hello expired, or there never was one.
		return msgjson.NewError(msgjson.RPCInternal, "mesh decision processing failed: unexpected mesh decision")
	}
	result := session.pending
	result.progress = progressFromAncestor(decision.Ancestor)
	peerID := result.peerHello.NodeID

	if result.progress == progressDiverged {
		// The apply may halt this node, so we send the response first so the
		// initiator can see the result.
		msgErr := sendRouteAck(conn, reqID, "mesh decision")
		if err := h.node.applyHandshakeResult(ctx, conn, result, peerID); err != nil {
			conn.Disconnect()
		}
		return msgErr
	}

	err := h.node.applyHandshakeResult(ctx, conn, result, peerID)
	switch {
	case err == nil:
		return sendRouteAck(conn, reqID, "mesh decision")
	case errors.Is(err, errConnNotAdopted):
		// Answer before disconnect so the dialer can tell a live peer from silence.
		msgErr := sendRouteErrorResponse(conn, reqID,
			msgjson.NewError(msgjson.MeshAlreadyConnectedError, "a session with this peer is already active"),
			"mesh decision")
		if msgErr != nil {
			h.log.Warnf("Failed to send mesh decision adoption rejection to %s: %v", conn.Addr(), msgErr)
		}
	}
	conn.Disconnect()
	return nil
}

func (h *handshakeSessions) hasPending(connID uint64) bool {
	h.pendingMtx.Lock()
	defer h.pendingMtx.Unlock()
	_, found := h.pending[connID]
	return found
}

func (h *handshakeSessions) addPending(conn link, pending *handshakeResult) *pendingHandshakeSession {
	session := &pendingHandshakeSession{pending: pending}

	h.pendingMtx.Lock()
	h.pending[conn.ID()] = session
	session.timer = time.AfterFunc(h.pendingTimeout, func() {
		if h.clearPending(conn.ID(), session) {
			h.log.Warnf("Mesh handshake with %s timed out waiting for decision", conn.Addr())
			conn.Disconnect()
		}
	})
	h.pendingMtx.Unlock()

	go func() {
		<-conn.Done()
		h.clearPending(conn.ID(), session)
	}()

	return session
}

func (h *handshakeSessions) takePending(connID uint64) *pendingHandshakeSession {
	h.pendingMtx.Lock()
	defer h.pendingMtx.Unlock()

	session := h.pending[connID]
	if session == nil {
		return nil
	}
	delete(h.pending, connID)
	session.timer.Stop()
	return session
}

func (h *handshakeSessions) clearPending(connID uint64, session *pendingHandshakeSession) bool {
	h.pendingMtx.Lock()
	defer h.pendingMtx.Unlock()

	if h.pending[connID] != session {
		return false
	}
	delete(h.pending, connID)
	session.timer.Stop()
	return true
}
