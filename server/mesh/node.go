// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
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
