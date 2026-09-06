// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"fmt"
	"time"

	"decred.org/dcrdex/server/db"
)

// reduceResult is the result of applying one signal. It holds the next
// state, the effects to run, and whether the signal was handled.
type reduceResult struct {
	next    nodeState
	effects []effect
	handled bool          // false if the signal was ignored, so no transition effects are added
	outcome signalOutcome // optional, returned to the sender of the signal
}

// ignoredResult leaves the state unchanged and runs no effects.
func ignoredResult(cur nodeState) reduceResult {
	return reduceResult{next: cur}
}

// handledResult moves the node to next and runs effects.
func handledResult(next nodeState, effects []effect) reduceResult {
	return reduceResult{
		next:    next,
		effects: effects,
		handled: true,
	}
}

// withOutcome sets the outcome that the sender of the signal receives.
func (r reduceResult) withOutcome(o signalOutcome) reduceResult {
	r.outcome = o
	return r
}

// halt moves the node to modeHalted with err as the reason, and disconnects
// the active connection, if any.
func halt(cur nodeState, err error, at time.Time) reduceResult {
	next := cur
	next.mode = modeHalted
	next.haltErr = err

	var effects []effect
	if cur.activeConn != nil {
		effects = append(effects, effectDisconnect{Conn: cur.activeConn})
		next.activeConn = nil
		next.peerDisconnected = at
	}
	effects = append(effects, effectHalt{})
	return handledResult(next, effects)
}

// promote moves the node to preparing master. Entering a master mode makes
// withTransitionEffects add effectBecameMaster, which starts the workers.
func promote(cur nodeState) reduceResult {
	next := cur
	next.mode = modePreparingMaster
	next.peerDisconnected = time.Time{}
	return handledResult(next, nil)
}

// sameFrontier reports whether two log positions are identical.
func sameFrontier(a, b *db.EventLogPosition) bool {
	return a.Seq == b.Seq && bytes.Equal(a.TipHash, b.TipHash)
}

// supersedeAfter is the age at which the active connection can be replaced by
// a new handshake from the same peer. It equals the transport's pong read
// deadline. A younger connection is considered a parallel dial and is decided
// by node ID. An older one may be dead.
const supersedeAfter = 2 * meshPingPeriod

// peerAdoptionDecision reports whether to adopt the new connection. A node
// with no active connection adopts it. A connection from a different peer
// node ID is refused. A second connection from the same peer replaces the
// active one if its initiator node ID is lower, or if the active one is stale.
func peerAdoptionDecision(cur nodeState, peerNodeID, initiatorNodeID string, at time.Time) bool {
	active := cur.activeConn
	switch {
	case active == nil:
		return true // nothing to replace
	case active.peerNodeID != peerNodeID:
		return false // a different node, and the mesh has one peer
	case initiatorNodeID < active.initiatorNodeID:
		return true // parallel dial: the lower initiator wins
	case !cur.connAdopted.IsZero() && !at.IsZero() && at.Sub(cur.connAdopted) >= supersedeAfter:
		return true // the active connection is old enough to be dead
	default:
		return false
	}
}

// rejectsDivergedJoin reports whether a diverged handshake is refused without
// a mode change. A preparing or established master refuses a peer that does
// not report master, and keeps its role. Master against master is not
// refused here. It halts.
func rejectsDivergedJoin(localMode nodeMode, peerRole helloRole) bool {
	return localMode.isAuthoritativeMaster() && peerRole != roleMaster
}

// resolvePeerState picks this node's mode after a successful handshake. A
// serving master rejects a fork from a peer that is not master before this
// function. Every other fork reaches this function and halts.
//
//	logs                 we              they            we become
//	prefix, we ahead     *               *               master
//	prefix, they ahead   not master      *               slave (catch up)
//	prefix, they ahead   master          *               halt (will not demote)
//	equal                neither master  neither master  lower node ID becomes master, else slave
//	equal                master          not master      stay master
//	equal                master          master          both halt
//	equal                not master      master          slave
//	fork                 master          master          halt
//	fork                 not master      *               halt
func resolvePeerState(localNodeID string, localMode nodeMode, peerNodeID string, peerRole helloRole, progress progressState) (nodeMode, error) {
	// Unequal frontier states determine the next mesh state directly.
	switch progress {
	case progressDiverged:
		return modeHalted, nil
	case progressLocalAhead:
		// An established master keeps its role. Anyone else becomes the
		// preparing master.
		if localMode == modeEstablishedMaster {
			return modeEstablishedMaster, nil
		}
		return modePreparingMaster, nil
	case progressPeerAhead:
		if localMode.isAuthoritativeMaster() {
			return modeHalted, nil
		}
		return modeEstablishedSlaveSyncing, nil
	case progressEqual:
		// Handled below.
	default:
		return modePending, fmt.Errorf("unknown progress state %d", progress)
	}

	// With equal frontiers, an established master keeps control unless the peer
	// also reports master, which halts. Otherwise a peer that reports master makes
	// the local node a slave, and all remaining equal cases fall back to the
	// node ID tie break.
	if localMode.isAuthoritativeMaster() {
		if peerRole == roleMaster {
			return modeHalted, nil
		}
		return localMode, nil
	}
	if peerRole == roleMaster {
		return modeEstablishedSlave, nil
	}
	if localNodeID < peerNodeID {
		return modePreparingMaster, nil
	}
	return modeEstablishedSlave, nil
}

// onHandshakeResolved handles a completed handshake. It decides whether to
// adopt the connection, then picks this node's mode from the peer's role and
// the event log comparison, and halts the node on a fork or a role clash.
func onHandshakeResolved(localNodeID string, cur nodeState, ev handshakeResolvedSignal) (reduceResult, error) {
	if cur.mode == modeHalted {
		return ignoredResult(cur).withOutcome(handshakeHalted), nil
	}
	if ev.conn == nil {
		return ignoredResult(cur), fmt.Errorf("nil peer connection")
	}

	// Two connections may have been opened in parallel, one from each side.
	// peerAdoptionDecision picks which one to keep.
	if !peerAdoptionDecision(cur, ev.conn.peerNodeID, ev.conn.initiatorNodeID, ev.at) {
		// No disconnect here. The handshake handler must first answer that the
		// connection was not adopted, and effects run before it gets this result.
		return handledResult(cur, nil).withOutcome(handshakeNotAdopted), nil
	}

	// A serving master rejects a diverged join from a peer that is not master,
	// and stays in mode.
	// The handshake handler answers before applying a diverged result, so
	// disconnecting here is safe.
	if ev.progress == progressDiverged && rejectsDivergedJoin(cur.mode, ev.peerRole) {
		return handledResult(cur, []effect{effectDisconnect{Conn: ev.conn}}).
			withOutcome(handshakeDivergedJoinRejected), nil
	}

	nextMode, err := resolvePeerState(localNodeID, cur.mode, ev.conn.peerNodeID, ev.peerRole, ev.progress)
	if err != nil {
		return ignoredResult(cur), err
	}
	if nextMode == modeHalted {
		return haltAfterHandshake(cur, ev), nil
	}

	next := cur
	next.mode = nextMode
	next.activeConn = ev.conn
	next.connAdopted = ev.at
	next.peerDisconnected = time.Time{}

	var effects []effect
	if !sameConn(cur.activeConn, ev.conn) {
		if cur.activeConn != nil {
			effects = append(effects, effectDisconnect{Conn: cur.activeConn})
		}
		effects = append(effects, effectWatchConn{Conn: ev.conn})
	}
	if next.mode.canReceiveEventStream() {
		effects = append(effects, effectStartSubscriber{Conn: ev.conn})
	}
	effects = append(effects, effectPeerClientEndpointChanged{Host: ev.clientHost, Cert: append([]byte(nil), ev.clientCert...)})
	return handledResult(next, effects).withOutcome(handshakeAdopted), nil
}

// haltAfterHandshake halts the node after a handshake that resolved to a
// fork or a role clash. The new connection and any old one are closed.
func haltAfterHandshake(cur nodeState, ev handshakeResolvedSignal) reduceResult {
	res := halt(cur, handshakeHaltError(cur, ev), ev.at)
	if !sameConn(cur.activeConn, ev.conn) {
		res.effects = append([]effect{effectDisconnect{Conn: ev.conn}}, res.effects...)
	}
	return res.withOutcome(handshakeHalted)
}

// handshakeHaltError builds the halt reason. A fork names both frontiers and,
// for a node joining a serving master, the --meshforkreset token.
func handshakeHaltError(cur nodeState, ev handshakeResolvedSignal) error {
	if ev.progress != progressDiverged {
		return fmt.Errorf("halting after handshake: local role %s, peer %s role %s, progress %s",
			cur.mode.helloRole(), ev.conn.peerNodeID, ev.peerRole, ev.progress)
	}
	hint := ""
	if !cur.mode.isAuthoritativeMaster() && ev.peerRole == roleMaster {
		if token := forkResetToken(ev.localFrontier); token != "" {
			hint = fmt.Sprintf("; this node holds committed events the serving master never received. "+
				"Back up the database for audit (pg_dump), then restart with --meshforkreset=%s "+
				"to wipe and resync from the master", token)
		}
	}
	return fmt.Errorf("MESH FORK DETECTED: halting after handshake, event logs diverged "+
		"(local role %s frontier %s, peer %s role %s frontier %s)%s",
		cur.mode.helloRole(), ev.localFrontier, ev.conn.peerNodeID, ev.peerRole, ev.peerFrontier, hint)
}

// onStreamCaughtUp moves a syncing slave to established slave once the event
// stream over the active connection has reached the tip the master reported.
// Any other mode, or a stale connection, ignores it.
func onStreamCaughtUp(cur nodeState, ev streamCaughtUpSignal) (reduceResult, error) {
	if cur.mode != modeEstablishedSlaveSyncing || !sameConn(cur.activeConn, ev.conn) {
		return ignoredResult(cur), nil
	}
	next := cur
	next.mode = modeEstablishedSlave
	return handledResult(next, nil), nil
}

// onSubscribeRejected halts the node after the peer refused, for good, to
// stream events from this node's frontier. A rejection on a replaced
// connection is ignored, and so is any rejection when this node is a master.
func onSubscribeRejected(cur nodeState, ev subscribeRejectedSignal) (reduceResult, error) {
	if !sameConn(cur.activeConn, ev.conn) || cur.mode.isAuthoritativeMaster() {
		return ignoredResult(cur), nil
	}
	return halt(cur, ev.err, ev.at), nil
}

// onConnectionDisconnected clears the closed active connection and sets the
// disconnect time. A slave enters modeSlaveNoMaster and starts the promotion
// timer, a syncing slave returns to pending, a master keeps its mode.
func onConnectionDisconnected(cur nodeState, ev connectionDisconnectedSignal) (reduceResult, error) {
	if !sameConn(cur.activeConn, ev.conn) {
		return ignoredResult(cur), nil
	}

	next := cur
	next.activeConn = nil
	next.peerDisconnected = ev.at

	effects := []effect{effectStopEventStream{Conn: ev.conn}}
	switch cur.mode {
	case modeEstablishedSlave:
		// The promotion delay counts from peerDisconnected, which was set
		// above. A zero value would promote on the first timer.
		next.mode = modeSlaveNoMaster
		effects = append(effects, effectScheduleSlavePromotionCheck{})
	case modeEstablishedSlaveSyncing:
		next.mode = modePending
	default:
		// A master keeps its mode and waits for the slave to reconnect.
	}
	return handledResult(next, effects), nil
}

// onStreamFailed handles a failed event stream or snapshot send to the slave.
// The master clears activeConn, records the disconnect time, and emits
// effectDisconnect to close the link so the slave reconnects and handshakes
// again.
func onStreamFailed(cur nodeState, ev streamFailedSignal) (reduceResult, error) {
	if cur.mode != modeEstablishedMaster || !sameConn(cur.activeConn, ev.conn) {
		return ignoredResult(cur), nil
	}
	next := cur
	next.activeConn = nil
	next.peerDisconnected = ev.at
	return handledResult(next, []effect{effectDisconnect{Conn: ev.conn}}), nil
}

// onMasterEvidence handles proof that the lost master is still alive. A
// slave without a master moves peerDisconnected forward to the evidence
// time and schedules a new promotion check, so the promotion delay restarts.
func onMasterEvidence(cur nodeState, ev masterEvidenceSignal) (reduceResult, error) {
	if cur.mode != modeSlaveNoMaster || !ev.at.After(cur.peerDisconnected) {
		return ignoredResult(cur), nil
	}
	next := cur
	next.peerDisconnected = ev.at
	return handledResult(next, []effect{effectScheduleSlavePromotionCheck{}}), nil
}

// onSlavePromotionCheck promotes a slave that has had no master for the full
// delay to preparing master. A check that fires early or belongs to an older
// disconnect is ignored.
func onSlavePromotionCheck(cur nodeState, ev slavePromotionCheckSignal, delay time.Duration) (reduceResult, error) {
	if cur.mode != modeSlaveNoMaster || ev.at.Sub(cur.peerDisconnected) < delay {
		return ignoredResult(cur), nil
	}
	return promote(cur), nil
}

// onPlannedHandoff moves a connected slave straight to modePreparingMaster,
// skipping the promotion delay, when its log frontier equals the departing
// master's. Any other frontier means the logs disagree, so the node
// disconnects and halts.
func onPlannedHandoff(cur nodeState, ev plannedHandoffSignal) (reduceResult, error) {
	if !cur.mode.canAcceptMasterHandoff() || !sameConn(cur.activeConn, ev.conn) ||
		ev.local == nil || ev.target == nil {
		return ignoredResult(cur), nil
	}
	if !sameFrontier(ev.local, ev.target) {
		return halt(cur, fmt.Errorf("planned handoff frontier mismatch (local %s, master %s)",
			ev.local, ev.target), ev.at), nil
	}
	return promote(cur), nil
}

// onMasterReady moves a preparing master to established master once its
// workers run, which also resolves startup. An established master ignores
// the signal. Any other mode is an error, since only a preparing master
// starts workers.
func onMasterReady(cur nodeState) (reduceResult, error) {
	if cur.mode == modeEstablishedMaster {
		return ignoredResult(cur), nil
	}
	if cur.mode != modePreparingMaster {
		return ignoredResult(cur), fmt.Errorf("master ready in node mode %s", cur.mode)
	}
	next := cur
	next.mode = modeEstablishedMaster
	return handledResult(next, nil), nil
}

// onMasterPreparationFailed halts a preparing or established master. Any
// other mode ignores it, because only a master runs the workers.
func onMasterPreparationFailed(cur nodeState, ev masterPreparationFailedSignal) (reduceResult, error) {
	if !cur.mode.isAuthoritativeMaster() {
		return ignoredResult(cur), nil
	}
	return halt(cur, ev.err, ev.at), nil
}

// servesStreamRequests reports whether the node serves a stream subscribe or
// snapshot request that arrived on connID. Only the established master serves
// them, and only on its active connection.
func servesStreamRequests(cur nodeState, connID uint64) bool {
	return cur.mode == modeEstablishedMaster &&
		cur.activeConn != nil && cur.activeConn.ID() == connID
}

// onStreamSubscribe handles a slave's stream_subscribe. An established master
// emits effectStartEventStream, which starts (or replaces) the stream to its
// active connection from the slave's frontier. Any other mode or connection
// ignores it.
func onStreamSubscribe(cur nodeState, ev streamSubscribeSignal) (reduceResult, error) {
	if !servesStreamRequests(cur, ev.connID) {
		return ignoredResult(cur), nil
	}
	return handledResult(cur, []effect{effectStartEventStream{
		Conn:          cur.activeConn,
		SlaveFrontier: ev.frontier,
	}}), nil
}

// onSnapshotRequest answers a peer's snapshot request. Only an established
// master emits effectStartSnapshotSend, and only for its active connection.
// The node state does not change.
func onSnapshotRequest(cur nodeState, ev snapshotRequestSignal) (reduceResult, error) {
	if !servesStreamRequests(cur, ev.connID) {
		return ignoredResult(cur), nil
	}
	return handledResult(cur, []effect{effectStartSnapshotSend{Conn: cur.activeConn}}), nil
}

// onDialIncompatible halts a pending node. The peer refused its dial for a
// reason no retry can fix, and a node with no role cannot serve alone. A node
// that already has a role ignores it and keeps running.
func onDialIncompatible(cur nodeState, ev dialIncompatibleSignal) (reduceResult, error) {
	if cur.mode != modePending {
		return ignoredResult(cur), nil
	}
	return halt(cur, ev.err, ev.at), nil
}

// onTerminalEventApplyFailure halts the node. A node that is halted already
// ignores it, so the first halt reason is kept.
func onTerminalEventApplyFailure(cur nodeState, ev terminalApplyFailureSignal) (reduceResult, error) {
	if cur.mode == modeHalted {
		return ignoredResult(cur), nil
	}
	return halt(cur, ev.err, ev.at), nil
}

// reduceSignal is the node's state machine. Given the current state and one
// signal, it returns the next state and the effects to run. It is pure. The
// effects describe work for the caller to do. The effects of the mode change
// itself, such as starting the master workers, are added by
// withTransitionEffects.
func reduceSignal(localNodeID string, slavePromotionDelay time.Duration, cur nodeState, ev meshSignal) (reduceResult, error) {
	var result reduceResult
	var err error
	switch ev := ev.(type) {
	case handshakeResolvedSignal:
		result, err = onHandshakeResolved(localNodeID, cur, ev)
	case streamCaughtUpSignal:
		result, err = onStreamCaughtUp(cur, ev)
	case subscribeRejectedSignal:
		result, err = onSubscribeRejected(cur, ev)
	case connectionDisconnectedSignal:
		result, err = onConnectionDisconnected(cur, ev)
	case streamFailedSignal:
		result, err = onStreamFailed(cur, ev)
	case masterEvidenceSignal:
		result, err = onMasterEvidence(cur, ev)
	case slavePromotionCheckSignal:
		result, err = onSlavePromotionCheck(cur, ev, slavePromotionDelay)
	case plannedHandoffSignal:
		result, err = onPlannedHandoff(cur, ev)
	case masterReadySignal:
		result, err = onMasterReady(cur)
	case masterPreparationFailedSignal:
		result, err = onMasterPreparationFailed(cur, ev)
	case streamSubscribeSignal:
		result, err = onStreamSubscribe(cur, ev)
	case snapshotRequestSignal:
		result, err = onSnapshotRequest(cur, ev)
	case dialIncompatibleSignal:
		result, err = onDialIncompatible(cur, ev)
	case terminalApplyFailureSignal:
		result, err = onTerminalEventApplyFailure(cur, ev)
	default:
		return ignoredResult(cur), fmt.Errorf("unhandled signal %T in mode %s", ev, cur.mode)
	}
	if err != nil || !result.handled {
		return result, err
	}
	result.effects = withTransitionEffects(cur, result.next, result.effects)
	return result, nil
}

// withTransitionEffects adds the effects of the mode change itself.
func withTransitionEffects(cur, next nodeState, effects []effect) []effect {
	// Start master workers and fail pending commands with unknown outcomes.
	if !cur.mode.isAuthoritativeMaster() && next.mode.isAuthoritativeMaster() {
		effects = append([]effect{effectBecameMaster{}}, effects...)
		effects = append(effects, effectFailPendingCommands{Reason: "node promoted to mesh master"})
	}

	// Fail pending commands if the node just lost its master.
	if cur.mode != modeSlaveNoMaster && next.mode == modeSlaveNoMaster {
		effects = append(effects, effectFailPendingCommands{Reason: "mesh slave lost its master connection"})
	}

	// Resolve startup if the node just became established.
	if cur.mode.startupPending() && next.mode.startupResolved() && next.mode != modeHalted {
		effects = append(effects, effectStartupResolved{})
	}

	return effects
}
