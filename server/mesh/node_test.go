// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

func newTestNodeWithState(state nodeState, app meshApplication) *node {
	if app == nil {
		app = &testMeshApplication{}
	}
	n := &node{
		log:        dex.Disabled,
		nodeID:     "node-a",
		control:    newQueuedTestControlLoop(state, 4),
		app:        app,
		runContext: context.Background(),
		stream: newEventStreamManager(&eventStreamManagerConfig{
			log:  dex.Disabled,
			node: &testStreamNode{},
		}),
		eventLogReader: &testEventLogReader{frontier: &db.EventLogPosition{}},
		snapshotStore:  &fakeSnapshotStore{},
	}
	n.snapshots = newSnapshotServer(n.log, n.snapshotStore, n)
	return n
}

func TestCheckEventPublishAvailable(t *testing.T) {
	peer := newNodeConn(newTRouteLink(1), "peer-node", "peer-node")

	startStream := func(n *node, connID uint64) {
		t.Helper()
		n.stream = newTestEventStreamManager(&streamTestReader{}, nil, nil, &db.EventLogPosition{})
		n.stream.ctx = context.Background()
		n.stream.startStreamOnConn(connID, &db.EventLogPosition{})
	}

	t.Run("forwarded conn no stream", func(t *testing.T) {
		n := newTestNodeWithState(nodeState{mode: modeEstablishedMaster, activeConn: peer}, nil)
		err := n.checkEventPublishAvailable(true)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("forwarded stream to other conn", func(t *testing.T) {
		n := newTestNodeWithState(nodeState{mode: modeEstablishedMaster, activeConn: peer}, nil)
		startStream(n, 2)
		err := n.checkEventPublishAvailable(true)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("forwarded stream to active conn", func(t *testing.T) {
		n := newTestNodeWithState(nodeState{mode: modeEstablishedMaster, activeConn: peer}, nil)
		startStream(n, 1)
		if err := n.checkEventPublishAvailable(true); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("local no stream", func(t *testing.T) {
		n := newTestNodeWithState(nodeState{mode: modeEstablishedMaster, activeConn: peer}, nil)
		if err := n.checkEventPublishAvailable(false); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})

	t.Run("preparation local vs forwarded", func(t *testing.T) {
		n := newTestNodeWithState(nodeState{mode: modePreparingMaster, activeConn: peer}, nil)
		if err := n.checkEventPublishAvailable(false); err != nil {
			t.Fatalf("local err = %v, want nil", err)
		}
		err := n.checkEventPublishAvailable(true)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("forwarded err = %v, want ErrUnavailable", err)
		}
	})
}

func TestApplyInboundEventEnvelopeTerminalFailure(t *testing.T) {
	const appliedSeq uint64 = 1
	applyErr := &db.EventLogDivergenceError{
		Seq:             appliedSeq + 1,
		ExpectedTipHash: testTipHash(appliedSeq + 1),
		ActualTipHash:   testTipHash(appliedSeq + 2),
		Err:             errors.New("frontier mismatch"),
	}
	n := newTestNodeWithState(nodeState{mode: modeEstablishedSlave}, &testMeshApplication{
		event: func(context.Context, *eventEnvelope) error {
			return applyErr
		},
	})
	queue := n.control.testQueue()

	err := n.applyInboundEventEnvelope(nil, &eventEnvelope{
		Seq:       appliedSeq + 1,
		TipHash:   testTipHash(appliedSeq + 1),
		MasterTip: appliedSeq + 1,
		Kind:      "test",
		Payload:   []byte("peer-payload"),
	})
	if !errors.Is(err, applyErr) {
		t.Fatalf("apply error = %v, want %v", err, applyErr)
	}

	select {
	case queued := <-queue:
		ev, ok := queued.signal.(terminalApplyFailureSignal)
		if !ok {
			t.Fatalf("posted event = %T, want terminalApplyFailureSignal", queued.signal)
		}
		if !errors.Is(ev.err, applyErr) {
			t.Fatalf("posted error = %v, want %v", ev.err, applyErr)
		}
	default:
		t.Fatalf("missing terminalApplyFailureSignal")
	}
}

func TestNodePostTerminalApplyFailureIfNeeded(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantPosted bool
	}{
		{
			name: "ignores non-terminal error",
			err:  errors.New("apply failed"),
		},
		{
			name: "posts terminal divergence error",
			err: &db.EventLogDivergenceError{
				Seq: 2,
				Err: errors.New("projection mismatch"),
			},
			wantPosted: true,
		},
		{
			name:       "posts unknown commit outcome",
			err:        &db.EventCommitUnknownError{Err: errors.New("commit outcome unknown")},
			wantPosted: true,
		},
		{
			name:       "posts replication wedge",
			err:        &replicationWedgedError{Seq: 7, Attempts: 3, Err: errors.New("apply failed")},
			wantPosted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestNodeWithState(nodeState{mode: modeEstablishedSlave}, nil)
			queue := p.control.testQueue()

			p.postTerminalApplyFailureIfNeeded(tt.err)

			select {
			case queued := <-queue:
				if !tt.wantPosted {
					t.Fatalf("posted event = %T, want none", queued.signal)
				}
				ev, ok := queued.signal.(terminalApplyFailureSignal)
				if !ok {
					t.Fatalf("posted event = %T, want terminalApplyFailureSignal", queued.signal)
				}
				if !errors.Is(ev.err, tt.err) {
					t.Fatalf("posted error = %v, want %v", ev.err, tt.err)
				}
			default:
				if tt.wantPosted {
					t.Fatalf("missing terminalApplyFailureSignal")
				}
			}
		})
	}
}

func TestNodeApplyHandshakeResult(t *testing.T) {
	haltErr := errors.New("halted after handshake")
	peerFrontier := &db.EventLogPosition{Seq: 4, TipHash: testTipHash(4)}
	clientHost := "peer.example:7232"
	clientCert := []byte{1, 2, 3}
	tests := []struct {
		name    string
		state   nodeState
		outcome handshakeOutcome
		wantErr string
	}{
		{
			name: "adopted connection succeeds",
			state: nodeState{
				mode: modeEstablishedMaster,
			},
			outcome: handshakeAdopted,
		},
		{
			name: "halted returns halt error",
			state: nodeState{
				mode:    modeHalted,
				haltErr: haltErr,
			},
			outcome: handshakeHalted,
			wantErr: haltErr.Error(),
		},
		{
			name: "non adopted connection returns error",
			state: nodeState{
				mode:       modeEstablishedMaster,
				activeConn: testRPCSession("node-c", "node-a"),
			},
			outcome: handshakeNotAdopted,
			wantErr: "connection not adopted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newTRouteLink(800)
			wantConn := newNodeConn(conn, "node-b", "node-a")
			state := tt.state
			if state.activeConn == nil && state.mode != modeHalted {
				state.activeConn = wantConn
			}
			p := newTestNodeWithState(nodeState{mode: modePending}, nil)
			result := &handshakeResult{
				peerHello: &helloMessage{
					NodeID:   "node-b",
					Role:     roleSlave,
					Frontier: toFrontierMessage(peerFrontier),
				},
				progress:   progressEqual,
				clientHost: clientHost,
				clientCert: clientCert,
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				queued := <-p.control.testQueue()
				ev, ok := queued.signal.(handshakeResolvedSignal)
				if !ok {
					t.Errorf("queued event = %T, want handshakeResolvedSignal", queued.signal)
					queued.reply <- signalResult{err: errors.New("unexpected event")}
					return
				}
				if ev.conn == nil || ev.conn.link != conn || ev.conn.peerNodeID != "node-b" || ev.conn.initiatorNodeID != "node-a" {
					t.Errorf("handshake event conn = %+v, want peer node-b initiator node-a", ev.conn)
				}
				if ev.peerRole != roleSlave || ev.progress != progressEqual {
					t.Errorf("handshake event = %+v, want roleSlave progressEqual", ev)
				}
				if ev.peerFrontier == nil || ev.peerFrontier.Seq != peerFrontier.Seq || !reflect.DeepEqual(ev.peerFrontier.TipHash, peerFrontier.TipHash) {
					t.Errorf("handshake event frontier = %+v, want %+v", ev.peerFrontier, peerFrontier)
				}
				if ev.clientHost != clientHost || !reflect.DeepEqual(ev.clientCert, clientCert) {
					t.Errorf("handshake event endpoint = %q/%x, want %q/%x", ev.clientHost, ev.clientCert, clientHost, clientCert)
				}
				queued.reply <- signalResult{
					handled: true,
					state:   state,
					outcome: tt.outcome,
				}
			}()

			err := p.applyHandshakeResult(context.Background(), conn, result, "node-a")
			<-done
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("applyHandshakeResult error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("applyHandshakeResult error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func testRPCSession(cpNode, localNode string) *nodeConn {
	return newNodeConn(newTRouteLink(801), cpNode, localNode)
}

func TestNodeSendEventEnvelopeTreatsClosedLinkAsCanceled(t *testing.T) {
	wsConn := &tWSConn{closed: make(chan struct{})}
	reqConn := newRPCConn(t.Context(), "test-peer", wsConn, nil, dex.Disabled)
	wg, err := reqConn.connect()
	if err != nil {
		t.Fatalf("connect error: %v", err)
	}
	reqConn.Disconnect()
	wg.Wait()

	conn := newNodeConn(reqConn, "node-b", "node-a")
	p := &node{
		control: newTestControlLoop(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: conn,
		}),
	}
	err = p.sendEventBatch(context.Background(), conn.ID(), &eventBatch{Entries: []*eventEnvelope{{
		Seq:       1,
		TipHash:   testTipHash(1),
		MasterTip: 1,
		Kind:      "test",
		Payload:   []byte(`{"payload":1}`),
	}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("send event envelope error = %v, want context canceled", err)
	}
}

func TestApplyFailureStreak(t *testing.T) {
	var e applyFailureStreak
	err := errors.New("apply failed")
	live := context.Background()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	steps := []struct {
		ctx  context.Context
		seq  uint64
		err  error
		want int
	}{
		{live, 5, err, 1},
		{live, 5, err, 2},
		{live, 5, err, 3},
		{live, 6, err, 1}, // different seq restarts
		{live, 6, err, 2},
		{canceled, 6, err, 0}, // dead ctx did not reset
		{canceled, 6, context.Canceled, 0},
		{live, 6, err, 3},
		{live, 6, context.Canceled, 4}, // live ctx: internal cancel still counts
		{live, 6, fmt.Errorf("apply: %w", context.DeadlineExceeded), 5},
		{live, 6, nil, 0}, // success resets
		{live, 6, err, 1},
	}
	for i, step := range steps {
		if got := e.observe(step.ctx, step.seq, step.err); got != step.want {
			t.Fatalf("step %d: observe(%d, %v) = %d, want %d",
				i, step.seq, step.err, got, step.want)
		}
	}
}

func TestApplyInboundEventEnvelopeWedgeHalt(t *testing.T) {
	applyErr := errors.New("event apply failed: swap contract recorded event for unknown match")
	failing := true
	n := newTestNodeWithState(nodeState{mode: modeEstablishedSlaveSyncing}, &testMeshApplication{
		event: func(context.Context, *eventEnvelope) error {
			if failing {
				return applyErr
			}
			return nil
		},
	})
	queue := n.control.testQueue()

	requireNoSignal := func(step string) {
		t.Helper()
		select {
		case queued := <-queue:
			t.Fatalf("%s: unexpected posted signal %T", step, queued.signal)
		default:
		}
	}

	// MasterTip != Seq so the success path does not post a caught-up signal.
	env := &eventEnvelope{Seq: 24, MasterTip: 30, Kind: "test", Payload: []byte("p")}
	otherEnv := &eventEnvelope{Seq: 25, MasterTip: 30, Kind: "test", Payload: []byte("p")}

	// Two failures, then a different seq, then a success: streak resets, no halt.
	for i := 0; i < applyWedgeStreakThreshold-1; i++ {
		if err := n.applyInboundEventEnvelope(nil, env); !errors.Is(err, applyErr) {
			t.Fatalf("apply %d error = %v, want %v", i, err, applyErr)
		}
	}
	if err := n.applyInboundEventEnvelope(nil, otherEnv); !errors.Is(err, applyErr) {
		t.Fatalf("other-seq apply error = %v, want %v", err, applyErr)
	}
	requireNoSignal("below threshold")
	failing = false
	if err := n.applyInboundEventEnvelope(nil, otherEnv); err != nil {
		t.Fatalf("successful apply error: %v", err)
	}
	if got := n.applyFailures.count; got != 0 {
		t.Fatalf("failure streak after success = %d, want 0", got)
	}

	// Three consecutive failures at one seq post the wedge halt.
	failing = true
	for i := 0; i < applyWedgeStreakThreshold; i++ {
		if err := n.applyInboundEventEnvelope(nil, env); !errors.Is(err, applyErr) {
			t.Fatalf("apply %d error = %v, want %v", i, err, applyErr)
		}
	}
	select {
	case queued := <-queue:
		sig, ok := queued.signal.(terminalApplyFailureSignal)
		if !ok {
			t.Fatalf("posted signal = %T, want terminalApplyFailureSignal", queued.signal)
		}
		var wedged *replicationWedgedError
		if !errors.As(sig.err, &wedged) {
			t.Fatalf("posted error = %v, want replicationWedgedError", sig.err)
		}
		if wedged.Seq != env.Seq || wedged.Attempts != applyWedgeStreakThreshold || !errors.Is(wedged, applyErr) {
			t.Fatalf("wedged error = %+v, want seq %d, attempts %d, cause %v",
				wedged, env.Seq, applyWedgeStreakThreshold, applyErr)
		}
	default:
		t.Fatalf("missing wedge halt signal after %d failures", applyWedgeStreakThreshold)
	}
}
