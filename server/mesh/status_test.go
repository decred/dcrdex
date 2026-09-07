// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

func TestServiceStatusSingleServer(t *testing.T) {
	svc := newTestService(t, nil, nil)
	st := svc.Status()
	if st.Mode != "single_server" {
		t.Fatalf("mode = %q, want single_server", st.Mode)
	}
	if st.Ready {
		t.Fatalf("ready = true before startup resolved")
	}

	svc.ready.resolve(nil)
	if st = svc.Status(); !st.Ready {
		t.Fatalf("ready = false after successful startup")
	}
}

func TestServiceStatusMeshTransport(t *testing.T) {
	svc := newTestService(t, nil, &testTransport{master: true})
	st := svc.Status()
	if st.Mode != modeEstablishedMaster.String() {
		t.Fatalf("mode = %q, want %s", st.Mode, modeEstablishedMaster)
	}
}

func TestNodeFillStatus(t *testing.T) {
	haltErr := errors.New("terminal apply failure")
	peerConn := newNodeConn(newTRouteLink(9901), "peer-node", "node-a")
	n := newTestNodeWithState(nodeState{
		mode:       modeEstablishedMaster,
		activeConn: peerConn,
	}, nil)
	n.stream = newEventStreamManager(&eventStreamManagerConfig{
		log:                dex.Disabled,
		initialFrontierSeq: 7,
	})
	n.eventLogReader = &testEventLogReader{
		frontier: &db.EventLogPosition{Seq: 42, TipHash: []byte{0xab, 0xcd}},
	}
	dialer, err := newOutboundDialer("ws://mesh.example:17232", nil, dex.Disabled, nil, nil, &testDialerNode{})
	if err != nil {
		t.Fatalf("newOutboundDialer error: %v", err)
	}
	dialer.attempts.Add(3)
	n.dialer = dialer

	var st Status
	n.fillStatus(&st)
	if st.Mode != modeEstablishedMaster.String() {
		t.Fatalf("mode = %q, want %s", st.Mode, modeEstablishedMaster)
	}
	if st.NodeID != "node-a" {
		t.Fatalf("node id = %q, want node-a", st.NodeID)
	}
	if !st.PeerConnected || st.PeerNodeID != "peer-node" {
		t.Fatalf("peer = connected=%t id=%q, want connected peer-node", st.PeerConnected, st.PeerNodeID)
	}
	if st.HaltErr != "" {
		t.Fatalf("master halt err = %q, want none", st.HaltErr)
	}
	// The test constructor commits the initial state through setState.
	if st.LastTransition.IsZero() {
		t.Fatalf("last transition unset, want a timestamp")
	}
	if st.DialAttempts != 3 {
		t.Fatalf("dial attempts = %d, want 3", st.DialAttempts)
	}
	if st.FrontierSeq != 42 || st.FrontierHash != "abcd" {
		t.Fatalf("frontier = %d/%q, want 42/abcd", st.FrontierSeq, st.FrontierHash)
	}
	if st.StreamTip != 7 || st.StreamCursor != 0 {
		t.Fatalf("stream tip/cursor = %d/%d, want 7/0", st.StreamTip, st.StreamCursor)
	}
	if st.StreamLag != 7 || st.StreamActive {
		t.Fatalf("stream lag/active = %d/%t, want 7/false", st.StreamLag, st.StreamActive)
	}

	n.control.setState(halt(n.control.currentState(), haltErr, time.Now()).next)
	st = Status{}
	n.fillStatus(&st)
	if st.Mode != modeHalted.String() || st.HaltErr != haltErr.Error() {
		t.Fatalf("halted mode/error = %q/%q, want halted/%q", st.Mode, st.HaltErr, haltErr)
	}
	if st.PeerConnected || st.PeerNodeID != "" {
		t.Fatalf("halted peer = connected %t, ID %q", st.PeerConnected, st.PeerNodeID)
	}
	if st.StreamActive || st.StreamTip != 0 || st.StreamCursor != 0 || st.StreamLag != 0 || st.PendingStreamResults != 0 {
		t.Fatalf("halted status contains stream progress: %+v", st)
	}
}
