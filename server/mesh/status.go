// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/hex"
	"time"
)

// Status reports mesh status for the admin API.
type Status struct {
	Mode                     string    `json:"mode"`
	Ready                    bool      `json:"ready"`
	NodeID                   string    `json:"nodeID,omitempty"`
	PeerNodeID               string    `json:"peerNodeID,omitempty"`
	PeerConnected            bool      `json:"peerConnected"`
	HaltErr                  string    `json:"haltErr,omitempty"`
	LastTransition           time.Time `json:"lastTransition,omitzero"`
	FrontierSeq              uint64    `json:"frontierSeq,omitempty"`
	FrontierHash             string    `json:"frontierHash,omitempty"`
	PeerDisconnectedAt       time.Time `json:"peerDisconnectedAt,omitzero"`
	PromoteAt                time.Time `json:"promoteAt,omitzero"`
	DialAttempts             uint64    `json:"dialAttempts"`
	LastDialError            string    `json:"lastDialError,omitempty"`
	LastDialAt               time.Time `json:"lastDialAt,omitzero"`
	StreamActive             bool      `json:"streamActive,omitempty"`
	StreamTip                uint64    `json:"streamTip,omitempty"`
	StreamCursor             uint64    `json:"streamCursor,omitempty"`
	StreamLag                uint64    `json:"streamLag,omitempty"`
	PendingStreamResults     int       `json:"pendingStreamResults,omitempty"`
	PendingForwardedCommands int       `json:"pendingForwardedCommands"`
	StateLoaded              bool      `json:"stateLoaded"`
	Seeding                  bool      `json:"seeding,omitempty"`
}

// Status reports the mesh status for the admin API.
func (s *Service) Status() Status {
	st := Status{
		PendingForwardedCommands: s.commands.pendingCount(),
	}
	if err, ok := s.ready.result(); ok {
		st.Ready = err == nil
	}
	if err, ok := s.loaded.result(); ok {
		st.StateLoaded = err == nil
	}
	s.transport.fillStatus(&st)
	return st
}

func (t *singleServerTransport) fillStatus(st *Status) {
	st.Mode = "single_server"
}

func (n *node) fillStatus(st *Status) {
	state := n.control.currentState()
	st.Mode = state.mode.String()
	st.NodeID = n.nodeID
	if state.activeConn != nil {
		st.PeerConnected = true
		st.PeerNodeID = state.activeConn.peerNodeID
	}
	if state.haltErr != nil {
		st.HaltErr = state.haltErr.Error()
	}
	st.LastTransition = n.control.lastTransitionTime()
	st.DialAttempts = n.dialer.attemptCount()
	st.LastDialError, st.LastDialAt = n.dialer.lastDialError()
	st.Seeding = n.seedInProgress()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	if frontier, err := n.eventLogReader.EventLogFrontier(ctx); err == nil && frontier != nil {
		st.FrontierSeq = frontier.Seq
		st.FrontierHash = hex.EncodeToString(frontier.TipHash)
	}
	cancel()

	if state.mode == modeSlaveNoMaster && !state.peerDisconnected.IsZero() {
		st.PeerDisconnectedAt = state.peerDisconnected
		st.PromoteAt = state.peerDisconnected.Add(n.control.slavePromotionDelay)
	}

	if state.mode.canStreamEvents() {
		tip, cursor, active := n.stream.progress()
		st.StreamActive = active
		st.StreamTip = tip
		st.StreamCursor = cursor
		if tip > cursor {
			st.StreamLag = tip - cursor
		}
		st.PendingStreamResults = n.stream.pendingResults()
	}
}
