// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
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
