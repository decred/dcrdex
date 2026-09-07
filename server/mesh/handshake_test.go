// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

func testTipHash(seq uint64) []byte {
	tipHash := make([]byte, eventLogTipHashSize)
	binary.BigEndian.PutUint64(tipHash[len(tipHash)-8:], seq)
	return tipHash
}

type testEventLogReader struct {
	mtx sync.Mutex

	frontier    *db.EventLogPosition
	entries     []*db.EventLogEntry
	frontierErr error
	entriesErr  error

	frontierCalls int
	entriesCalls  int
	frontierCtx   context.Context
	after         uint64
	limit         int
}

func (p *testEventLogReader) EventLogFrontier(ctx context.Context) (*db.EventLogPosition, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.frontierCalls++
	p.frontierCtx = ctx
	if p.frontierErr != nil {
		return nil, p.frontierErr
	}
	return p.frontier, nil
}

func (p *testEventLogReader) EventLogEntriesAfter(_ context.Context, after uint64, limit int) ([]*db.EventLogEntry, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	p.entriesCalls++
	p.after = after
	p.limit = limit
	if p.entriesErr != nil {
		return nil, p.entriesErr
	}
	return p.entries, nil
}

type tHandshakeTransport struct {
	resp *helloResponse

	helloErr    error
	decisionErr error

	helloReq      *helloMessage
	decisionReq   *decisionMessage
	helloCalls    int
	decisionCalls int
}

func (t *tHandshakeTransport) requestHello(_ context.Context, req *helloMessage) (*helloResponse, error) {
	t.helloCalls++
	t.helloReq = req
	return t.resp, t.helloErr
}

func (t *tHandshakeTransport) requestDecision(_ context.Context, req *decisionMessage) error {
	t.decisionCalls++
	t.decisionReq = req
	return t.decisionErr
}

type helloSpec struct {
	nodeID string
	role   helloRole

	frontier   *db.EventLogPosition
	compat     *CompatSnapshot
	clientHost string
	clientCert []byte

	omitCompatConfig bool
	mutate           func(*helloMessage)
}

type pendingDecisionState struct {
	connID    uint64
	peerHello helloSpec
}

type handshakeServiceState struct {
	nodeID string
	role   helloRole

	compat *CompatSnapshot

	localFrontier *db.EventLogPosition
	eventEntries  []*db.EventLogEntry
	frontierErr   error
	entriesErr    error

	clientHost string
	clientCert []byte
	pending    []pendingDecisionState
}

func testCompatSnapshot(t testing.TB) *CompatSnapshot {
	t.Helper()

	snapshot, err := NewCompatSnapshot(CompatConfig{
		Network:    "testnet",
		APIVersion: 4,
	})
	if err != nil {
		t.Fatalf("NewCompatSnapshot error: %v", err)
	}
	return snapshot
}

func differentCompatSnapshot(t testing.TB) *CompatSnapshot {
	t.Helper()

	snapshot, err := NewCompatSnapshot(CompatConfig{
		Network:    "testnet",
		APIVersion: 4,
		Markets: []CompatMarket{
			{Name: "dcr_btc", Base: 42, Quote: 0, LotSize: 1, ParcelSize: 1, RateStep: 1, EpochDuration: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewCompatSnapshot error: %v", err)
	}
	return snapshot
}

func testHandshakeSigner(t testing.TB) *secp256k1Signer {
	t.Helper()

	signer, err := newSecp256k1Signer(testPrivKey())
	if err != nil {
		t.Fatalf("newSecp256k1Signer error: %v", err)
	}
	return signer
}

func cloneCompatHash(hash [32]byte) dex.Bytes {
	return append(dex.Bytes(nil), hash[:]...)
}

func cloneCompatConfig(cfg *CompatConfig) *CompatConfig {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	return &cloned
}

func buildSignedHello(t testing.TB, spec helloSpec) *helloMessage {
	t.Helper()

	compat := spec.compat
	if compat == nil {
		compat = testCompatSnapshot(t)
	}
	frontier := spec.frontier
	if frontier == nil {
		frontier = &db.EventLogPosition{}
	}

	hello := &helloMessage{
		NodeID:     spec.nodeID,
		Role:       spec.role,
		Frontier:   toFrontierMessage(frontier),
		CompatHash: cloneCompatHash(compat.Hash),
		ClientHost: spec.clientHost,
		ClientCert: append(dex.Bytes(nil), spec.clientCert...),
	}
	if !spec.omitCompatConfig {
		hello.CompatConfig = cloneCompatConfig(compat.Config)
	}
	if spec.mutate != nil {
		spec.mutate(hello)
	}
	if err := hello.sign(testHandshakeSigner(t)); err != nil {
		t.Fatalf("hello.sign error: %v", err)
	}
	return hello
}

func newTestHandshakeService(t testing.TB, state handshakeServiceState) (*handshakeService, *testEventLogReader) {
	t.Helper()

	compat := state.compat
	if compat == nil {
		compat = testCompatSnapshot(t)
	}
	nodeID := state.nodeID
	if nodeID == "" {
		nodeID = "local-node"
	}
	localFrontier := state.localFrontier
	if localFrontier == nil {
		localFrontier = &db.EventLogPosition{}
	}

	eventLogReader := &testEventLogReader{
		frontier:    localFrontier,
		entries:     state.eventEntries,
		frontierErr: state.frontierErr,
		entriesErr:  state.entriesErr,
	}

	svc := newHandshakeService(
		nodeID,
		func() helloRole { return state.role },
		testHandshakeSigner(t),
		compat,
		eventLogReader,
		state.clientHost,
		state.clientCert,
		dex.Disabled,
	)

	return svc, eventLogReader
}

func requireClientEndpoint(t testing.TB, gotHost string, gotCert []byte, wantHost string, wantCert []byte) {
	t.Helper()
	if gotHost != wantHost || !reflect.DeepEqual(gotCert, wantCert) {
		t.Fatalf("client endpoint = %q/%x, want %q/%x", gotHost, gotCert, wantHost, wantCert)
	}
}

func TestCompareEventLogFrontier(t *testing.T) {
	providerErr := errors.New("provider error")

	tests := []struct {
		name string

		local       *db.EventLogPosition
		peer        *db.EventLogPosition
		entries     []*db.EventLogEntry
		frontierErr error
		entriesErr  error

		want            progressState
		wantErr         bool
		wantBelowAnchor bool
		wantAfter       uint64
		wantLimit       int
		wantCalls       int
	}{
		{
			name:  "empty equal",
			local: &db.EventLogPosition{},
			peer:  &db.EventLogPosition{},
			want:  progressEqual,
		},
		{
			name:  "same tip and hash equal",
			local: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			peer:  &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			want:  progressEqual,
		},
		{
			name:  "same tip different hash diverged",
			local: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			peer:  &db.EventLogPosition{Seq: 2, TipHash: testTipHash(3)},
			want:  progressDiverged,
		},
		{
			name:  "peer ahead is undecidable locally",
			local: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			peer:  &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			want:  progressPeerAhead,
		},
		{
			// The empty-joiner bootstrap: a Seq-0 peer is an ancestor by
			// definition and must resolve WITHOUT a history lookup — a
			// snapshot-seeded log has no entry at seq 0 to look up.
			name:      "empty peer against a non-empty log is local ahead without a lookup",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{},
			want:      progressLocalAhead,
			wantCalls: 0,
		},
		{
			name:  "empty local log behind a non-empty peer",
			local: &db.EventLogPosition{},
			peer:  &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			want:  progressPeerAhead,
		},
		{
			name:      "local ahead with peer prefix",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:   []*db.EventLogEntry{{Seq: 2, Kind: "test", TipHash: testTipHash(2)}},
			want:      progressLocalAhead,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			name:      "local ahead without peer prefix diverged",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(9)},
			entries:   []*db.EventLogEntry{{Seq: 2, Kind: "test", TipHash: testTipHash(2)}},
			want:      progressDiverged,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			name:       "historical lookup error",
			local:      &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:       &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entriesErr: providerErr,
			wantErr:    true,
			wantAfter:  1,
			wantLimit:  1,
			wantCalls:  1,
		},
		{
			name:      "historical entry missing",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:   nil,
			wantErr:   true,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			name:      "historical entry nil",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:   []*db.EventLogEntry{nil},
			wantErr:   true,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			// A row above the peer's seq that is not an anchor is a hole in
			// the log, not a snapshot anchor.
			name:      "historical entry wrong sequence",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:   []*db.EventLogEntry{{Seq: 3, Kind: "test", TipHash: testTipHash(2)}},
			wantErr:   true,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			name:            "peer below the snapshot anchor",
			local:           &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:            &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:         []*db.EventLogEntry{{Seq: 3, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(3)}},
			wantErr:         true,
			wantBelowAnchor: true,
			wantAfter:       1,
			wantLimit:       1,
			wantCalls:       1,
		},
		{
			// The peer sits on the anchor. The hashes decide, as for any row.
			name:      "peer on the snapshot anchor",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
			entries:   []*db.EventLogEntry{{Seq: 2, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(2)}},
			want:      progressLocalAhead,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			name:      "peer on the snapshot anchor with another hash",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 2, TipHash: testTipHash(9)},
			entries:   []*db.EventLogEntry{{Seq: 2, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(2)}},
			want:      progressDiverged,
			wantAfter: 1,
			wantLimit: 1,
			wantCalls: 1,
		},
		{
			// Genesis is stamped at seq 1, and a seq 0 peer never reaches
			// the lookup, so a genesis anchor cannot put a peer below it.
			name:    "genesis anchor against an empty peer",
			local:   &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:    &db.EventLogPosition{},
			entries: []*db.EventLogEntry{{Seq: 1, Kind: db.MeshGenesisKind, TipHash: testTipHash(1)}},
			want:    progressLocalAhead,
		},
		{
			name:      "genesis anchor against a foreign genesis",
			local:     &db.EventLogPosition{Seq: 3, TipHash: testTipHash(3)},
			peer:      &db.EventLogPosition{Seq: 1, TipHash: testTipHash(9)},
			entries:   []*db.EventLogEntry{{Seq: 1, Kind: db.MeshGenesisKind, TipHash: testTipHash(1)}},
			want:      progressDiverged,
			wantAfter: 0,
			wantLimit: 1,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &testEventLogReader{
				frontier:    tt.local,
				entries:     tt.entries,
				frontierErr: tt.frontierErr,
				entriesErr:  tt.entriesErr,
			}

			_, got, err := compareEventLogFrontier(context.Background(), reader, tt.peer)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if errors.Is(err, errPeerBelowAnchor) != tt.wantBelowAnchor {
					t.Fatalf("below anchor = %v, want %v (error %v)", !tt.wantBelowAnchor, tt.wantBelowAnchor, err)
				}
			} else {
				if err != nil {
					t.Fatalf("compareEventLogFrontier error: %v", err)
				}
				if got != tt.want {
					t.Fatalf("progress = %v, want %v", got, tt.want)
				}
			}
			if reader.entriesCalls != tt.wantCalls {
				t.Fatalf("entries calls = %d, want %d", reader.entriesCalls, tt.wantCalls)
			}
			if tt.wantCalls > 0 && reader.after != tt.wantAfter {
				t.Fatalf("entries after = %d, want %d", reader.after, tt.wantAfter)
			}
			if tt.wantCalls > 0 && reader.limit != tt.wantLimit {
				t.Fatalf("entries limit = %d, want %d", reader.limit, tt.wantLimit)
			}
		})
	}
}

func TestHandshakeServiceInitiateHandshake(t *testing.T) {
	snapshot := testCompatSnapshot(t)
	localFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerEqualFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerEqualForkFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(11)}
	peerAheadFrontier := &db.EventLogPosition{Seq: 12, TipHash: testTipHash(12)}
	peerBehindFrontier := &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)}
	localHost, localCert := "local.example:7232", []byte{1, 2}
	peerHost, peerCert := "Peer.EXAMPLE:7232", []byte{3, 4}

	// The behind-peer prefix entry: our log's hash at the peer's Seq 8.
	behindPrefixEntry := []*db.EventLogEntry{{Seq: 8, Kind: "test", TipHash: testTipHash(8)}}
	// The forked variant: our log's hash at Seq 8 differs from the peer's tip.
	behindForkEntry := []*db.EventLogEntry{{Seq: 8, Kind: "test", TipHash: testTipHash(9)}}

	tests := []struct {
		name  string
		state handshakeServiceState

		respHello    helloSpec
		respAncestor bool
		decisionErr  error

		wantProgress        progressState
		wantErr             string
		wantIncompatibility bool
		wantDecisionCalls   int
		// wantDecisionAncestor is asserted only when wantDecisionCalls > 0.
		wantDecisionAncestor bool
		wantClientHost       string
		wantClientCert       []byte
	}{
		{
			name: "equal frontiers resolve without a bit",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				clientHost:    localHost,
				clientCert:    localCert,
			},
			respHello: helloSpec{
				nodeID:     "peer-node",
				role:       roleSlave,
				frontier:   peerEqualFrontier,
				compat:     snapshot,
				clientHost: peerHost,
				clientCert: peerCert,
			},
			wantProgress:      progressEqual,
			wantDecisionCalls: 0,
			wantClientHost:    peerHost,
			wantClientCert:    peerCert,
		},
		{
			name: "equal-length fork resolves from the advertised hashes",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerEqualForkFrontier,
				compat:   snapshot,
			},
			wantProgress:      progressDiverged,
			wantDecisionCalls: 0,
		},
		{
			name: "ahead responder's true bit means we are cleanly behind",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleSlave,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerAheadFrontier,
				compat:   snapshot,
			},
			respAncestor:      true,
			wantProgress:      progressPeerAhead,
			wantDecisionCalls: 0,
		},
		{
			name: "ahead responder's false bit is a fork",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerAheadFrontier,
				compat:   snapshot,
			},
			wantProgress:      progressDiverged,
			wantDecisionCalls: 0,
		},
		{
			name: "missing bit from an ahead responder is a fork",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleSlave,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerAheadFrontier,
				compat:   snapshot,
			},
			wantProgress:      progressDiverged,
			wantDecisionCalls: 0,
		},
		{
			name: "bit from an equal-length responder is ignored",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   snapshot,
			},
			respAncestor:      true,
			wantProgress:      progressEqual,
			wantDecisionCalls: 0,
		},
		{
			name: "bit from a behind responder is ignored",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  behindPrefixEntry,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			respAncestor:         true,
			wantProgress:         progressLocalAhead,
			wantDecisionCalls:    1,
			wantDecisionAncestor: true,
		},
		{
			name: "behind responder with a clean prefix gets a true decision",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  behindPrefixEntry,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			wantProgress:         progressLocalAhead,
			wantDecisionCalls:    1,
			wantDecisionAncestor: true,
		},
		{
			name: "behind responder with a forked prefix gets a false decision",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  behindForkEntry,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			wantProgress:         progressDiverged,
			wantDecisionCalls:    1,
			wantDecisionAncestor: false,
		},
		{
			name: "computed fork survives decision send failure",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  behindForkEntry,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			// The responder may have disconnected on this very conclusion
			// before acking; the locally computed fork must still come back
			// so this node applies it to its own branch.
			decisionErr:          errors.New("mesh link disconnected during route mesh_hello_decision"),
			wantProgress:         progressDiverged,
			wantDecisionCalls:    1,
			wantDecisionAncestor: false,
		},
		{
			name: "clean-prefix decision send failure returns the error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  behindPrefixEntry,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			decisionErr:          errors.New("send failed"),
			wantErr:              "send failed",
			wantDecisionCalls:    1,
			wantDecisionAncestor: true,
		},
		{
			// This node's log begins at an anchor above the peer's tip. The
			// error stays local. Only the responder can tell the peer to
			// reset, and this node is the initiator.
			name: "behind responder below the anchor is an error, not a fork",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  []*db.EventLogEntry{{Seq: 10, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(10)}},
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			wantErr:           "below this node's snapshot anchor",
			wantDecisionCalls: 0,
		},
		{
			name: "peer compat mismatch is incompatible",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   differentCompatSnapshot(t),
			},
			wantErr:             "compatibility hash mismatch",
			wantIncompatibility: true,
			wantDecisionCalls:   0,
		},
		{
			name: "invalid peer metadata is incompatible",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleUnknown,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "local-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
			},
			wantErr:             "peer node ID matches local node ID",
			wantIncompatibility: true,
			wantDecisionCalls:   0,
		},
		{
			name: "invalid peer frontier is incompatible",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			respHello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerBehindFrontier,
				compat:   snapshot,
				mutate: func(hello *helloMessage) {
					hello.Frontier.TipHash = dex.Bytes{1}
				},
			},
			wantErr:             "frontier",
			wantIncompatibility: true,
			wantDecisionCalls:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestHandshakeService(t, tt.state)
			transport := &tHandshakeTransport{
				resp: &helloResponse{
					Hello:    buildSignedHello(t, tt.respHello),
					Ancestor: tt.respAncestor,
				},
				decisionErr: tt.decisionErr,
			}

			result, err := svc.initiateHandshake(context.Background(), transport)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				if tt.wantIncompatibility && !errors.Is(err, errPeerIncompatible) {
					t.Fatalf("expected incompatibility error, got %v", err)
				}
				if !tt.wantIncompatibility && errors.Is(err, errPeerIncompatible) {
					t.Fatalf("unexpected incompatibility classification: %v", err)
				}
				if result != nil {
					t.Fatalf("expected nil result, got %+v", result)
				}
			} else {
				if err != nil {
					t.Fatalf("initiateHandshake error: %v", err)
				}
				if result == nil {
					t.Fatalf("nil handshake result")
				}
				if result.progress != tt.wantProgress {
					t.Fatalf("progress = %v, want %v", result.progress, tt.wantProgress)
				}
				if result.peerHello == nil || result.peerHello.NodeID != tt.respHello.nodeID {
					t.Fatalf("peerHello = %+v, want nodeID %q", result.peerHello, tt.respHello.nodeID)
				}
			}

			if transport.helloCalls != 1 {
				t.Fatalf("requestHello calls = %d, want 1", transport.helloCalls)
			}
			if transport.helloReq == nil {
				t.Fatalf("nil hello request")
			}
			if err := transport.helloReq.verifySig(svc.signer); err != nil {
				t.Fatalf("hello request signature error: %v", err)
			}
			if transport.helloReq.NodeID != tt.state.nodeID {
				t.Fatalf("hello request nodeID = %q, want %q", transport.helloReq.NodeID, tt.state.nodeID)
			}
			if transport.helloReq.Role != tt.state.role {
				t.Fatalf("hello request role = %v, want %v", transport.helloReq.Role, tt.state.role)
			}
			if !reflect.DeepEqual(fromFrontierMessage(transport.helloReq.Frontier), tt.state.localFrontier) {
				t.Fatalf("hello request frontier = %+v, want %+v",
					fromFrontierMessage(transport.helloReq.Frontier), tt.state.localFrontier)
			}
			requireClientEndpoint(t, transport.helloReq.ClientHost, transport.helloReq.ClientCert, tt.state.clientHost, tt.state.clientCert)
			if !reflect.DeepEqual(transport.helloReq.CompatConfig, tt.state.compat.Config) {
				t.Fatalf("hello request compat config = %+v, want %+v",
					transport.helloReq.CompatConfig, tt.state.compat.Config)
			}
			if !reflect.DeepEqual(transport.helloReq.CompatHash, cloneCompatHash(tt.state.compat.Hash)) {
				t.Fatalf("hello request compat hash = %x, want %x",
					transport.helloReq.CompatHash, tt.state.compat.Hash)
			}

			if transport.decisionCalls != tt.wantDecisionCalls {
				t.Fatalf("requestDecision calls = %d, want %d", transport.decisionCalls, tt.wantDecisionCalls)
			}
			if tt.wantDecisionCalls > 0 {
				if transport.decisionReq == nil {
					t.Fatalf("nil decision request")
				}
				if transport.decisionReq.Ancestor != tt.wantDecisionAncestor {
					t.Fatalf("decision ancestor = %v, want %v", transport.decisionReq.Ancestor, tt.wantDecisionAncestor)
				}
			}

			if result != nil {
				requireClientEndpoint(t, result.clientHost, result.clientCert, tt.wantClientHost, tt.wantClientCert)
			}
		})
	}
}

func TestHandshakeServiceValidatePeerHello(t *testing.T) {
	snapshot := testCompatSnapshot(t)
	localFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerEqualFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}

	tests := []struct {
		name    string
		state   handshakeServiceState
		hello   helloSpec
		wantErr string
	}{
		{
			name: "valid hello passes",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   snapshot,
			},
		},
		{
			name: "zero frontier non-empty hash returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: &db.EventLogPosition{},
				compat:   snapshot,
				mutate: func(hello *helloMessage) {
					hello.Frontier.TipHash = dex.Bytes{1}
				},
			},
			wantErr: "invalid peer frontier",
		},
		{
			name: "non-zero frontier empty hash returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: &db.EventLogPosition{Seq: 8},
				compat:   snapshot,
			},
			wantErr: "invalid peer frontier",
		},
		{
			name: "non-zero frontier short hash returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   snapshot,
				mutate: func(hello *helloMessage) {
					hello.Frontier.TipHash = dex.Bytes{1}
				},
			},
			wantErr: "invalid peer frontier",
		},
		{
			name: "non-zero frontier long hash returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   snapshot,
				mutate: func(hello *helloMessage) {
					hello.Frontier.TipHash = make(dex.Bytes, eventLogTipHashSize+1)
				},
			},
			wantErr: "invalid peer frontier",
		},
		{
			name: "invalid peer metadata returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleUnknown,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "local-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   snapshot,
			},
			wantErr: "peer node ID matches local node ID",
		},
		{
			name: "peer compat mismatch returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: peerEqualFrontier,
				compat:   differentCompatSnapshot(t),
			},
			wantErr: "compatibility hash mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestHandshakeService(t, tt.state)
			err := svc.validatePeerHello(buildSignedHello(t, tt.hello))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validatePeerHello error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestHandshakeServiceProcessHello(t *testing.T) {
	snapshot := testCompatSnapshot(t)
	localFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerEqualFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerAheadFrontier := &db.EventLogPosition{Seq: 11, TipHash: testTipHash(11)}
	localHost, localCert := "local.example:7232", []byte{1, 2}
	peerHost, peerCert := "Peer.EXAMPLE:7232", []byte{3, 4}

	tests := []struct {
		name  string
		state handshakeServiceState

		hello helloSpec

		wantAncestor   bool
		wantProgress   progressState
		wantErr        string
		wantClientHost string
		wantClientCert []byte
	}{
		{
			name: "resolved hello returns result",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				clientHost:    localHost,
				clientCert:    localCert,
			},
			hello: helloSpec{
				nodeID:     "peer-node",
				role:       roleSlave,
				frontier:   peerEqualFrontier,
				compat:     snapshot,
				clientHost: peerHost,
				clientCert: peerCert,
			},
			wantProgress:   progressEqual,
			wantClientHost: peerHost,
			wantClientCert: peerCert,
		},
		{
			name: "equal-length fork resolves without a bit",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: &db.EventLogPosition{Seq: 10, TipHash: testTipHash(11)},
				compat:   snapshot,
			},
			wantProgress: progressDiverged,
		},
		{
			name: "ahead of a clean prefix sets a true bit",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  []*db.EventLogEntry{{Seq: 8, Kind: "test", TipHash: testTipHash(8)}},
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)},
				compat:   snapshot,
			},
			wantAncestor: true,
			wantProgress: progressLocalAhead,
		},
		{
			name: "ahead of a forked prefix sets a false bit",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  []*db.EventLogEntry{{Seq: 8, Kind: "test", TipHash: testTipHash(9)}},
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleMaster,
				frontier: &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)},
				compat:   snapshot,
			},
			wantProgress: progressDiverged,
		},
		{
			name: "peer below the snapshot anchor returns error",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleMaster,
				compat:        snapshot,
				localFrontier: localFrontier,
				eventEntries:  []*db.EventLogEntry{{Seq: 10, Kind: db.SnapshotAnchorKind, TipHash: testTipHash(10)}},
			},
			hello: helloSpec{
				nodeID:   "peer-node",
				role:     roleSlave,
				frontier: &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)},
				compat:   snapshot,
			},
			wantErr: "below this node's snapshot anchor",
		},
		{
			name: "peer ahead stores pending decision",
			state: handshakeServiceState{
				nodeID:        "local-node",
				role:          roleSlave,
				compat:        snapshot,
				localFrontier: localFrontier,
				clientHost:    localHost,
				clientCert:    localCert,
			},
			hello: helloSpec{
				nodeID:     "peer-node",
				role:       roleMaster,
				frontier:   peerAheadFrontier,
				compat:     snapshot,
				clientHost: peerHost,
				clientCert: peerCert,
			},
			wantProgress:   progressPeerAhead,
			wantClientHost: peerHost,
			wantClientCert: peerCert,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc, reader := newTestHandshakeService(t, tt.state)
			inboundHello := buildSignedHello(t, tt.hello)

			resp, result, err := svc.processHello(ctx, inboundHello)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				if resp != nil {
					t.Fatalf("expected nil response, got %+v", resp)
				}
				if result != nil {
					t.Fatalf("expected nil result, got %+v", result)
				}
			} else {
				if err != nil {
					t.Fatalf("processHello error: %v", err)
				}
				if resp == nil || resp.Hello == nil {
					t.Fatalf("nil hello response")
				}
				if resp.Ancestor != tt.wantAncestor {
					t.Fatalf("response ancestor = %v, want %v", resp.Ancestor, tt.wantAncestor)
				}
				if err := resp.Hello.verifySig(svc.signer); err != nil {
					t.Fatalf("response hello signature error: %v", err)
				}
				if resp.Hello.NodeID != tt.state.nodeID {
					t.Fatalf("response hello nodeID = %q, want %q", resp.Hello.NodeID, tt.state.nodeID)
				}
				if resp.Hello.Role != tt.state.role {
					t.Fatalf("response hello role = %v, want %v", resp.Hello.Role, tt.state.role)
				}
				if !reflect.DeepEqual(fromFrontierMessage(resp.Hello.Frontier), tt.state.localFrontier) {
					t.Fatalf("response hello frontier = %+v, want %+v",
						fromFrontierMessage(resp.Hello.Frontier), tt.state.localFrontier)
				}
				requireClientEndpoint(t, resp.Hello.ClientHost, resp.Hello.ClientCert, tt.state.clientHost, tt.state.clientCert)
				if !reflect.DeepEqual(resp.Hello.CompatConfig, tt.state.compat.Config) {
					t.Fatalf("response hello compat config = %+v, want %+v", resp.Hello.CompatConfig, tt.state.compat.Config)
				}
				if !reflect.DeepEqual(resp.Hello.CompatHash, cloneCompatHash(tt.state.compat.Hash)) {
					t.Fatalf("response hello compat hash = %x, want %x", resp.Hello.CompatHash, tt.state.compat.Hash)
				}

				if result == nil {
					t.Fatalf("expected handshake result")
				}
				if result.progress != tt.wantProgress {
					t.Fatalf("result progress = %v, want %v", result.progress, tt.wantProgress)
				}
				if result.peerHello != inboundHello {
					t.Fatalf("result peerHello was not the inbound hello")
				}
				requireClientEndpoint(t, result.clientHost, result.clientCert, tt.wantClientHost, tt.wantClientCert)
			}

			if reader.frontierCalls > 0 && reader.frontierCtx != ctx {
				t.Fatalf("frontier reader did not receive the processHello context")
			}
		})
	}
}

func TestInitiateHandshakeNotAdoptedAfterVerifiedHello(t *testing.T) {
	snapshot := testCompatSnapshot(t)
	localFrontier := &db.EventLogPosition{Seq: 10, TipHash: testTipHash(10)}
	peerFrontier := &db.EventLogPosition{Seq: 8, TipHash: testTipHash(8)}

	tests := []struct {
		name         string
		peerFrontier *db.EventLogPosition
		respAncestor bool
	}{{
		name:         "verified hello permits adoption rejection",
		peerFrontier: peerFrontier,
	}, {
		// The combination a real already-connected ahead responder sends:
		// its resolved response carries Ancestor=true AND NotAdopted. The
		// rejection must win over the bit, or the dialer would adopt a
		// duplicate connection and skip the promotion-clock evidence.
		name:         "rejection wins over a present ancestor bit",
		peerFrontier: &db.EventLogPosition{Seq: 12, TipHash: testTipHash(12)},
		respAncestor: true,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestHandshakeService(t, handshakeServiceState{
				nodeID:        "local-node",
				role:          roleSlave,
				compat:        snapshot,
				localFrontier: localFrontier,
			})
			transport := &tHandshakeTransport{
				resp: &helloResponse{
					Hello: buildSignedHello(t, helloSpec{
						nodeID:   "peer-node",
						role:     roleMaster,
						frontier: tt.peerFrontier,
						compat:   snapshot,
					}),
					Ancestor:   tt.respAncestor,
					NotAdopted: true,
				},
			}

			result, err := svc.initiateHandshake(context.Background(), transport)
			if result != nil {
				t.Fatalf("expected nil result, got %+v", result)
			}
			if !errors.Is(err, errPeerAlreadyConnected) {
				t.Fatalf("errPeerAlreadyConnected = false, want true (err %v)", err)
			}
		})
	}
}
