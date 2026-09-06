// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"fmt"
	"time"

	"decred.org/dcrdex/server/db"
)

// meshSignal is an input to the node's state machine.
type meshSignal interface {
	isMeshSignal()
	String() string
}

// handshakeResolvedSignal reports a completed handshake with the peer.
type handshakeResolvedSignal struct {
	conn          *nodeConn
	peerRole      helloRole
	progress      progressState
	localFrontier *db.EventLogPosition
	peerFrontier  *db.EventLogPosition
	clientHost    string
	clientCert    []byte
	at            time.Time
}

func (handshakeResolvedSignal) isMeshSignal() {}

func (ev handshakeResolvedSignal) String() string {
	return fmt.Sprintf("handshakeResolved(conn=%s peerRole=%s progress=%s at=%s)",
		ev.conn, ev.peerRole, ev.progress, formatLogTime(ev.at))
}

// connectionDisconnectedSignal reports that the connection to the peer has been
// disconnected.
type connectionDisconnectedSignal struct {
	conn *nodeConn
	at   time.Time
}

func (connectionDisconnectedSignal) isMeshSignal() {}

func (ev connectionDisconnectedSignal) String() string {
	return fmt.Sprintf("connectionDisconnected(conn=%s at=%s)",
		ev.conn, formatLogTime(ev.at))
}

// terminalApplyFailureSignal reports that this node can no longer be trusted
// to match the event log, so it should halt.
type terminalApplyFailureSignal struct {
	err error
	at  time.Time
}

func (terminalApplyFailureSignal) isMeshSignal() {}

func (ev terminalApplyFailureSignal) String() string {
	return fmt.Sprintf("terminalEventApplyFailure(at=%s err=%v)",
		formatLogTime(ev.at), ev.err)
}

// streamCaughtUpSignal is only used on the slave, to report that the slave
// has caught up to the master's tip. target is the position reached. It is
// for logging only.
type streamCaughtUpSignal struct {
	conn   *nodeConn
	target *db.EventLogPosition
}

func (streamCaughtUpSignal) isMeshSignal() {}

func (ev streamCaughtUpSignal) String() string {
	return fmt.Sprintf("streamCaughtUp(conn=%s target=%s)", ev.conn, ev.target)
}

// streamFailedSignal is posted on the master to report that the event stream
// failed.
type streamFailedSignal struct {
	conn *nodeConn
	err  error
	at   time.Time
}

func (streamFailedSignal) isMeshSignal() {}

func (ev streamFailedSignal) String() string {
	return fmt.Sprintf("streamFailed(conn=%s at=%s err=%v)",
		ev.conn, formatLogTime(ev.at), ev.err)
}

// slavePromotionCheckSignal fires slavePromotionDelay after a slave lost its
// master. If the slave is still without a master and the full delay has
// passed since the disconnect, it promotes itself.
type slavePromotionCheckSignal struct {
	at time.Time
}

func (slavePromotionCheckSignal) isMeshSignal() {}

func (ev slavePromotionCheckSignal) String() string {
	return fmt.Sprintf("slavePromotionCheck(at=%s)",
		formatLogTime(ev.at))
}

// plannedHandoffSignal is posted by the slave after the master sends a
// masterHandoff message. If the master's tip is equal to the local tip,
// the slave will immediately promote itself, otherwise the node will halt.
type plannedHandoffSignal struct {
	conn   *nodeConn
	local  *db.EventLogPosition
	target *db.EventLogPosition
	at     time.Time
}

func (plannedHandoffSignal) isMeshSignal() {}

func (ev plannedHandoffSignal) String() string {
	return fmt.Sprintf("plannedHandoff(conn=%s local=%s target=%s at=%s)",
		ev.conn, ev.local, ev.target, formatLogTime(ev.at))
}

// masterEvidenceSignal is sent when we have proof that the master is alive.
// The slave must not promote.
type masterEvidenceSignal struct {
	at time.Time
}

func (masterEvidenceSignal) isMeshSignal() {}

func (ev masterEvidenceSignal) String() string {
	return fmt.Sprintf("masterEvidence(at=%s)", formatLogTime(ev.at))
}

// dialIncompatibleSignal reports that the peer refused our dial for a reason
// that retrying cannot fix. Only a pending node acts on it, by halting.
// A node that already has a role keeps running, because it means that a
// successful handshake happened in the past, and the incompatibility is due
// to the peer restarting.
type dialIncompatibleSignal struct {
	err error
	at  time.Time
}

func (dialIncompatibleSignal) isMeshSignal() {}

func (ev dialIncompatibleSignal) String() string {
	return fmt.Sprintf("dialIncompatible(at=%s err=%v)", formatLogTime(ev.at), ev.err)
}

// subscribeRejectedSignal reports that the master refused to stream events
// from this slave's position, and retrying cannot help. The slave's event
// log is ahead of the master's, has diverged from it, or is too old to
// replay. The slave halts.
type subscribeRejectedSignal struct {
	conn *nodeConn
	err  error
	at   time.Time
}

func (subscribeRejectedSignal) isMeshSignal() {}

func (ev subscribeRejectedSignal) String() string {
	return fmt.Sprintf("subscribeRejected(at=%s conn=%s err=%v)",
		formatLogTime(ev.at), ev.conn, ev.err)
}

// streamSubscribeSignal carries a validated stream_subscribe from the route
// handler into the reducer, which does the active-conn and mode checks
// atomically with state.
type streamSubscribeSignal struct {
	connID   uint64
	frontier *db.EventLogPosition
}

func (streamSubscribeSignal) isMeshSignal() {}

func (ev streamSubscribeSignal) String() string {
	return fmt.Sprintf("streamSubscribe(connID=%d frontier=%s)", ev.connID, ev.frontier)
}

// snapshotRequestSignal reports that the peer requested a snapshot of this
// node's state. If this node is the established master and the request
// came over the active connection, it starts sending the snapshot.
type snapshotRequestSignal struct {
	connID uint64
}

func (snapshotRequestSignal) isMeshSignal() {}

func (ev snapshotRequestSignal) String() string {
	return fmt.Sprintf("snapshotRequest(connID=%d)", ev.connID)
}

// masterReadySignal reports that the master workers have started and the
// node can serve as master. It moves the node from preparing master to
// established master.
type masterReadySignal struct{}

func (masterReadySignal) isMeshSignal() {}

func (masterReadySignal) String() string {
	return "masterReady"
}

// masterPreparationFailedSignal reports that master preparation failed. The
// state loaders or a master worker failed to start. The node cannot serve as
// master without them, so it halts.
type masterPreparationFailedSignal struct {
	err error
	at  time.Time
}

func (masterPreparationFailedSignal) isMeshSignal() {}

func (ev masterPreparationFailedSignal) String() string {
	return fmt.Sprintf("masterPreparationFailed(at=%s err=%v)",
		formatLogTime(ev.at), ev.err)
}

// signalOutcome is an optional result that says how a signal was resolved.
// Each kind of signal defines its own.
type signalOutcome interface {
	isSignalOutcome()
}

// handshakeOutcome is how onHandshakeResolved resolved a handshake.
type handshakeOutcome uint8

const (
	// handshakeAdopted means the connection is now the active peer connection.
	handshakeAdopted handshakeOutcome = iota
	// handshakeNotAdopted means the handshake was fine, but an existing
	// session with this peer kept precedence.
	handshakeNotAdopted
	// handshakeDivergedJoinRejected means a serving master rejected a diverged
	// join from a peer that is not master, and keeps serving unchanged.
	handshakeDivergedJoinRejected
	// handshakeHalted means the node is halted, either from before this signal
	// or because this handshake detected a fork. nodeState.haltErr has the
	// cause.
	handshakeHalted
)

func (handshakeOutcome) isSignalOutcome() {}

func (o handshakeOutcome) String() string {
	switch o {
	case handshakeAdopted:
		return "adopted"
	case handshakeNotAdopted:
		return "not_adopted"
	case handshakeDivergedJoinRejected:
		return "diverged_join_rejected"
	case handshakeHalted:
		return "halted"
	default:
		return fmt.Sprintf("handshakeOutcome(%d)", uint8(o))
	}
}

// effect is an action for the caller to perform after a state change.
type effect interface {
	isEffect()
}

// effectDisconnect closes the connection, and stops any event stream or
// snapshot send to it.
type effectDisconnect struct {
	Conn *nodeConn
}

func (effectDisconnect) isEffect() {}

// effectStopEventStream stops any event stream or snapshot send to the
// connection, without closing it.
type effectStopEventStream struct {
	Conn *nodeConn
}

func (effectStopEventStream) isEffect() {}

// effectWatchConn starts watching an adopted connection. A
// connectionDisconnectedSignal is posted when it closes, and the master
// disconnects it if the slave does not subscribe within subscribeTimeout.
type effectWatchConn struct {
	Conn *nodeConn
}

func (effectWatchConn) isEffect() {}

// effectStartSubscriber starts the slave's subscriber, which subscribes to
// the master's event stream over the connection.
type effectStartSubscriber struct {
	Conn *nodeConn
}

func (effectStartSubscriber) isEffect() {}

// effectStartEventStream starts streaming events to the slave, from the
// entry after SlaveFrontier.
type effectStartEventStream struct {
	Conn          *nodeConn
	SlaveFrontier *db.EventLogPosition
}

func (effectStartEventStream) isEffect() {}

// effectStartSnapshotSend starts sending a snapshot of this node's state to
// the slave.
type effectStartSnapshotSend struct {
	Conn *nodeConn
}

func (effectStartSnapshotSend) isEffect() {}

// effectScheduleSlavePromotionCheck starts the timer that posts a
// slavePromotionCheckSignal after slavePromotionDelay.
type effectScheduleSlavePromotionCheck struct{}

func (effectScheduleSlavePromotionCheck) isEffect() {}

// effectBecameMaster reports that the node became the preparing master. It
// starts the master workers. The node becomes the established master once
// they are ready.
type effectBecameMaster struct{}

func (effectBecameMaster) isEffect() {}

// effectStartupResolved reports that the node is established as master or
// slave, so node startup is complete.
type effectStartupResolved struct{}

func (effectStartupResolved) isEffect() {}

// effectHalt stops the node. nodeState.haltErr is the reason.
type effectHalt struct{}

func (effectHalt) isEffect() {}

// effectFailPendingCommands answers pending forwarded commands with
// ResultUnavailableError.
type effectFailPendingCommands struct {
	Reason string
}

func (effectFailPendingCommands) isEffect() {}

// effectPeerClientEndpointChanged reports the peer's client endpoint, as
// learned in the handshake, so it can be advertised to clients for failover.
type effectPeerClientEndpointChanged struct {
	Host string
	Cert []byte
}

func (effectPeerClientEndpointChanged) isEffect() {}
