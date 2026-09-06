// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

// meshApplication lets the node deliver peer commands, events, and results to
// Service, and relay client messages and connection queries.
type meshApplication interface {
	executeForwardedCommand(context.Context, string, CommandRequest) *msgjson.Error
	receiveCommandFailure(string, *msgjson.Error)
	receiveCommandResult(string, json.RawMessage)
	applyReceivedEvent(context.Context, *eventEnvelope) error
	handleClientProxyMessage(context.Context, *ClientProxyMessage) error
	answerClientConnected([]account.AccountID) []account.AccountID
}

// lifecycleHooks are lifecycle notifications sent from the node to its creator.
type lifecycleHooks struct {
	becameMaster              func()
	peerClientEndpointChanged func(string, []byte)
	failPendingCommands       func(reason string)
	halted                    func(error)
}

// node is the peered implementation of meshTransport (as opposed to
// singleServerTransport). It manages the communication with the peer,
// the lifecycle of the node, and calls back into the meshApplication as
// needed. It implements the meshTransport interface.
type node struct {
	log          dex.Logger
	nodeID       string
	handshakeSvc *handshakeService
	handshakes   *handshakeSessions
	dialer       *outboundDialer
	serverCfg    meshServerConfig
	app          meshApplication

	control       *controlLoop
	stream        *eventStreamManager
	snapshots     *snapshotServer
	lifecycle     lifecycleHooks
	applyFailures applyFailureStreak

	eventLogReader db.EventLogReader
	snapshotStore  db.SnapshotStore

	// eventsGate is used to block sending events to the application until
	// it is ready to receive them.
	eventsGate *readiness

	// seeding marks a first join in progress: set at construction for an
	// empty-log node, cleared at seed-orchestrator teardown.
	seeding atomic.Bool

	routeTable map[string]meshRoute
	cancelRun  context.CancelFunc
	runContext context.Context
}

var _ meshTransport = (*node)(nil)

// waitForStartup blocks until startup resolves or the run context ends. A
// context end is refined into the halt error when the control loop halted.
func (n *node) waitForStartup(runCtx context.Context, startup *readiness) error {
	if err := startup.wait(runCtx); err != nil {
		state := n.control.currentState()
		if state.mode == modeHalted {
			if state.haltErr != nil {
				return state.haltErr
			}
			return fmt.Errorf("mesh halted during startup")
		}
		return fmt.Errorf("mesh stopped during startup")
	}
	return nil
}

// controlHalted is a callback passed to the control loop to handle
// the situation where the node has been halted.
func (n *node) controlHalted(err error) {
	n.cancelRun()
	n.lifecycle.halted(err)
}

// haltStatus reports the control loop's terminal halt state.
func (n *node) haltStatus() (bool, error) {
	return n.control.haltStatus()
}

// handleEffect handles an effect from the control loop.
func (n *node) handleEffect(eff effect) {
	switch e := eff.(type) {
	case effectBecameMaster:
		n.lifecycle.becameMaster()
	case effectDisconnect:
		n.stopStreamForConn(e.Conn)
		e.Conn.link.Disconnect()
	case effectStopEventStream:
		n.stopStreamForConn(e.Conn)
	case effectWatchConn:
		n.watchPeerConn(e.Conn)
	case effectStartSubscriber:
		go n.runSubscriber(e.Conn)
	case effectStartEventStream:
		n.startEventStream(e.Conn, e.SlaveFrontier)
	case effectStartSnapshotSend:
		n.startSnapshotSend(e.Conn)
	case effectPeerClientEndpointChanged:
		n.lifecycle.peerClientEndpointChanged(e.Host, append([]byte(nil), e.Cert...))
	case effectFailPendingCommands:
		n.lifecycle.failPendingCommands(e.Reason)
	}
}

// errConnNotAdopted means this conn lost adoption to an existing session
// with the same peer.
var errConnNotAdopted = errors.New("connection not adopted")

// applyHandshakeResult hands a completed wire handshake to the control loop,
// which decides whether to adopt the connection and which mode this node
// moves to (possibly halting it).
//   - nil means the connection was adopted as the active connection
//   - Any error means the connection was not adopted and the caller must disconnect it.
//   - errConnNotAdopted specifically means the handshake itself was fine but an
//     existing session with this peer took precedence.
func (n *node) applyHandshakeResult(ctx context.Context, conn link, result *handshakeResult, initiatorNodeID string) error {
	peerConn := newNodeConn(conn, result.peerHello.NodeID, initiatorNodeID)
	peerRole := result.peerHello.Role
	progress := result.progress
	peerFrontier := fromFrontierMessage(result.peerHello.Frontier)

	localFrontier, err := n.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return fmt.Errorf("local event log frontier: %w", err)
	}

	res, err := n.control.send(handshakeResolvedSignal{
		conn:          peerConn,
		peerRole:      peerRole,
		progress:      progress,
		localFrontier: localFrontier,
		peerFrontier:  peerFrontier,
		clientHost:    result.clientHost,
		clientCert:    append([]byte(nil), result.clientCert...),
		at:            time.Now(),
	})
	if err != nil {
		return err
	}
	if res.err != nil {
		return res.err
	}

	state := res.state

	switch outcome := res.outcome.(type) {
	case handshakeOutcome:
		switch outcome {
		case handshakeAdopted:
			n.log.Infof("Mesh handshake with peer %s resolved: peer_role=%s progress=%s local_state=%s",
				meshPeerLogID(peerConn), peerRole, progress, state.mode)
			return nil
		case handshakeNotAdopted:
			n.log.Infof("Mesh handshake with peer %s resolved: peer_role=%s progress=%s connection_not_adopted local_state=%s",
				meshPeerLogID(peerConn), peerRole, progress, state.mode)
			return errConnNotAdopted
		case handshakeDivergedJoinRejected:
			n.log.Warnf("Mesh peer %s presented a diverged event log (peer_role=%s); "+
				"rejecting join, remaining serving master. local_state=%s",
				meshPeerLogID(peerConn), peerRole, state.mode)
			return fmt.Errorf("diverged peer join rejected; this node remains the serving master")
		case handshakeHalted:
			n.log.Warnf("Mesh handshake with peer %s resolved: peer_role=%s progress=%s local_state=%s err=%v",
				meshPeerLogID(peerConn), peerRole, progress, state.mode, state.haltErr)
			if state.haltErr != nil {
				return state.haltErr
			}
			return fmt.Errorf("mesh halted after handshake")
		default:
			return fmt.Errorf("unknown handshake outcome %s", outcome)
		}
	default:
		return fmt.Errorf("handshake resolution reported no outcome (local_state=%s)", state.mode)
	}
}

// notifyMasterReady sends a signal to the control loop to indicate
// the master is ready to serve clients.
func (n *node) notifyMasterReady() error {
	res, err := n.control.send(masterReadySignal{})
	if err != nil {
		return err
	}
	if res.err != nil {
		return res.err
	}
	if res.state.mode != modeEstablishedMaster {
		return fmt.Errorf("master readiness resolved to node mode %s", res.state.mode)
	}
	return nil
}

// notifyMasterPreparationFailed sends a signal to the control loop to indicate
// the master preparation failed.
func (n *node) notifyMasterPreparationFailed(err error) error {
	if err == nil {
		err = fmt.Errorf("master preparation failed")
	}
	res, sendErr := n.control.send(masterPreparationFailedSignal{
		err: err,
		at:  time.Now(),
	})
	if sendErr != nil {
		return sendErr
	}
	return res.err
}
