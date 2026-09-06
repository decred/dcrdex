// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"fmt"
	"time"
)

// helloRole is the role advertised during handshake.
type helloRole uint8

const (
	roleUnknown helloRole = iota
	roleMaster
	roleSlave
)

func (r helloRole) String() string {
	switch r {
	case roleUnknown:
		return "unknown"
	case roleMaster:
		return "master"
	case roleSlave:
		return "slave"
	default:
		return fmt.Sprintf("helloRole(%d)", uint8(r))
	}
}

func (r helloRole) valid() bool {
	switch r {
	case roleUnknown, roleMaster, roleSlave:
		return true
	default:
		return false
	}
}

// progressState is the result of comparing the local and peer frontiers.
type progressState uint8

const (
	progressEqual progressState = iota
	progressLocalAhead
	progressPeerAhead
	progressDiverged
)

func (p progressState) String() string {
	switch p {
	case progressEqual:
		return "equal"
	case progressLocalAhead:
		return "local_ahead"
	case progressPeerAhead:
		return "peer_ahead"
	case progressDiverged:
		return "diverged"
	default:
		return fmt.Sprintf("progressState(%d)", uint8(p))
	}
}

// nodeMode is the node's current operating mode in the mesh.
type nodeMode uint8

const (
	modePending nodeMode = iota
	modePreparingMaster
	modeEstablishedMaster
	modeEstablishedSlaveSyncing
	modeEstablishedSlave
	modeSlaveNoMaster
	modeHalted
)

func (m nodeMode) String() string {
	switch m {
	case modePending:
		return "pending"
	case modePreparingMaster:
		return "preparing_master"
	case modeEstablishedMaster:
		return "established_master"
	case modeEstablishedSlaveSyncing:
		return "established_slave_syncing"
	case modeEstablishedSlave:
		return "established_slave"
	case modeSlaveNoMaster:
		return "slave_no_master"
	case modeHalted:
		return "halted"
	default:
		return fmt.Sprintf("nodeMode(%d)", uint8(m))
	}
}

// helloRole maps the node mode to the role reported in a hello message.
func (m nodeMode) helloRole() helloRole {
	switch m {
	case modePreparingMaster, modeEstablishedMaster:
		return roleMaster
	case modeEstablishedSlaveSyncing, modeEstablishedSlave, modeSlaveNoMaster:
		return roleSlave
	default:
		return roleUnknown
	}
}

func (m nodeMode) startupResolved() bool {
	switch m {
	case modeEstablishedMaster, modeEstablishedSlave, modeSlaveNoMaster, modeHalted:
		return true
	default:
		return false
	}
}

func (m nodeMode) startupPending() bool {
	return !m.startupResolved()
}

func (m nodeMode) isAuthoritativeMaster() bool {
	return m == modePreparingMaster || m == modeEstablishedMaster
}

func (m nodeMode) canExecuteCommands() bool {
	return m == modeEstablishedMaster
}

func (m nodeMode) canForwardCommands() bool {
	return m == modeEstablishedSlave
}

func (m nodeMode) canDeliverCommandCompletions() bool {
	return m == modeEstablishedMaster
}

func (m nodeMode) canStreamEvents() bool {
	return m == modeEstablishedMaster
}

func (m nodeMode) canReceiveEventStream() bool {
	return m == modeEstablishedSlaveSyncing || m == modeEstablishedSlave
}

func (m nodeMode) canAcceptMasterHandoff() bool {
	return m == modeEstablishedSlaveSyncing || m == modeEstablishedSlave
}

func (m nodeMode) canRelayClientMessages() bool {
	return m == modeEstablishedMaster || m == modeEstablishedSlaveSyncing || m == modeEstablishedSlave
}

func (m nodeMode) canExchangeClientConnectivity() bool {
	switch m {
	case modeEstablishedMaster, modeEstablishedSlaveSyncing, modeEstablishedSlave:
		return true
	default:
		return false
	}
}

// nodeState is the current mesh state published by the control loop.
type nodeState struct {
	mode        nodeMode
	activeConn  *nodeConn
	connAdopted time.Time
	// peerDisconnected is the time when the peer disconnected.
	// It is used to determine if the slave has waited long enough
	// to be promoted.
	peerDisconnected time.Time
	haltErr          error
}

func (s nodeState) helloRole() helloRole {
	return s.mode.helloRole()
}

func (s nodeState) hasActiveConnection() bool {
	return s.activeConn != nil
}

func (s nodeState) String() string {
	return fmt.Sprintf("nodeState{mode=%s activeConn=%s peerDisconnected=%s haltErr=%v}",
		s.mode, s.activeConn, formatLogTime(s.peerDisconnected), s.haltErr)
}

func formatLogTime(t time.Time) string {
	if t.IsZero() {
		return "<zero>"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (c *controlLoop) setState(state nodeState) {
	c.stateMtx.Lock()
	c.state = state
	c.lastTransition = time.Now()
	c.stateMtx.Unlock()
}

func (c *controlLoop) currentState() nodeState {
	c.stateMtx.RLock()
	defer c.stateMtx.RUnlock()
	return c.state
}

func (c *controlLoop) currentRole() helloRole {
	return c.currentState().helloRole()
}

func (c *controlLoop) currentMode() nodeMode {
	return c.currentState().mode
}

// haltStatus reports whether the node has halted and returns its halt error.
func (c *controlLoop) haltStatus() (bool, error) {
	state := c.currentState()
	if state.mode != modeHalted {
		return false, nil
	}
	return true, state.haltErr
}

func (c *controlLoop) hasPeerConnection() bool {
	return c.currentState().hasActiveConnection()
}

// lastTransitionTime returns the time of the most recent node state update.
func (c *controlLoop) lastTransitionTime() time.Time {
	c.stateMtx.RLock()
	defer c.stateMtx.RUnlock()
	return c.lastTransition
}
