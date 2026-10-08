// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
)

// subLogReader is an event-log fake honoring after/limit semantics, unlike
// the handshake tests' reader.
type subLogReader struct {
	frontier *db.EventLogPosition
	entries  []*db.EventLogEntry
}

func (r *subLogReader) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	if r.frontier == nil {
		return &db.EventLogPosition{}, nil
	}
	return r.frontier, nil
}

func (r *subLogReader) EventLogEntriesAfter(_ context.Context, after uint64, limit int) ([]*db.EventLogEntry, error) {
	var out []*db.EventLogEntry
	for _, entry := range r.entries {
		if entry.Seq > after {
			out = append(out, entry)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// TestValidateSubscribeAgainstLog covers the handler-side, append-only-log
// validations: frontier bounds, hash agreement, and anchored-history
// rejection at subscribe time.
func TestValidateSubscribeAgainstLog(t *testing.T) {
	entry := func(seq uint64, kind string) *db.EventLogEntry {
		return &db.EventLogEntry{Seq: seq, Kind: kind, TipHash: testTipHash(seq)}
	}
	position := func(seq uint64) *db.EventLogPosition {
		return &db.EventLogPosition{Seq: seq, TipHash: testTipHash(seq)}
	}

	tests := []struct {
		name     string
		frontier *db.EventLogPosition
		reader   *subLogReader
		wantCode int // 0 means accepted
	}{
		{
			name:     "matching mid-log frontier accepted",
			frontier: position(2),
			reader: &subLogReader{frontier: position(3),
				entries: []*db.EventLogEntry{entry(1, "k"), entry(2, "k"), entry(3, "k")}},
		},
		{
			name:     "matching tip frontier accepted",
			frontier: position(3),
			reader:   &subLogReader{frontier: position(3)},
		},
		{
			name:     "beyond tip rejected permanently",
			frontier: position(9),
			reader:   &subLogReader{frontier: position(3)},
			wantCode: msgjson.SubscribeRejectedError,
		},
		{
			name:     "hash mismatch rejected permanently",
			frontier: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(99)},
			reader: &subLogReader{frontier: position(3),
				entries: []*db.EventLogEntry{entry(1, "k"), entry(2, "k"), entry(3, "k")}},
			wantCode: msgjson.SubscribeRejectedError,
		},
		{
			name:     "hash mismatch at the tip rejected permanently",
			frontier: &db.EventLogPosition{Seq: 3, TipHash: testTipHash(99)},
			reader: &subLogReader{frontier: position(3),
				entries: []*db.EventLogEntry{entry(1, "k"), entry(2, "k"), entry(3, "k")}},
			wantCode: msgjson.SubscribeRejectedError,
		},
		{
			name:     "full replay from empty log accepted",
			frontier: &db.EventLogPosition{},
			reader:   &subLogReader{frontier: &db.EventLogPosition{}},
		},
		{
			name:     "full replay across genesis anchor rejected permanently",
			frontier: &db.EventLogPosition{},
			reader: &subLogReader{frontier: position(1),
				entries: []*db.EventLogEntry{entry(1, db.MeshGenesisKind)}},
			wantCode: msgjson.SubscribeRejectedError,
		},
		{
			name:     "full replay across snapshot anchor rejected permanently",
			frontier: &db.EventLogPosition{},
			reader: &subLogReader{frontier: position(5),
				entries: []*db.EventLogEntry{entry(5, db.SnapshotAnchorKind)}},
			wantCode: msgjson.SubscribeRejectedError,
		},
		{
			name:     "full replay against pruned log head rejected permanently",
			frontier: &db.EventLogPosition{},
			reader: &subLogReader{frontier: position(5),
				entries: []*db.EventLogEntry{entry(3, "k"), entry(4, "k"), entry(5, "k")}},
			wantCode: msgjson.SubscribeRejectedError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &node{log: dex.Disabled, eventLogReader: tt.reader}
			msgErr := n.validateSubscribeAgainstLog(context.Background(), tt.frontier)
			if tt.wantCode == 0 {
				if msgErr != nil {
					t.Fatalf("unexpected rejection: %v", msgErr)
				}
				return
			}
			if msgErr == nil {
				t.Fatalf("accepted, want rejection code %d", tt.wantCode)
			}
			if msgErr.Code != tt.wantCode {
				t.Fatalf("rejection code = %d (%s), want %d", msgErr.Code, msgErr.Message, tt.wantCode)
			}
		})
	}
}

// TestSnapshotServeThenSubscribe checks that events are sent only after
// the peer subscribes following a snapshot transfer.
func TestSnapshotServeThenSubscribe(t *testing.T) {
	snapFrontier := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
	store := &fakeSnapshotStore{payload: []byte("snapshot-bytes"), frontier: snapFrontier}

	reader := &streamTestReader{entries: []*db.EventLogEntry{
		{Seq: 6, Kind: "k", Event: []byte(`"e6"`), TipHash: testTipHash(6)},
	}}

	var mtx sync.Mutex
	var chunks int
	var sawLast bool
	sentSeqs := make(chan uint64, 16)

	peer := &testStreamNode{
		sendChunk: func(_ context.Context, _ uint64, chunk *snapshotChunk) error {
			mtx.Lock()
			chunks++
			if chunk.Last {
				sawLast = true
			}
			mtx.Unlock()
			return nil
		},
		send: func(ctx context.Context, _ uint64, batch *eventBatch) error {
			for _, e := range batch.Entries {
				select {
				case sentSeqs <- e.Seq:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	}
	srv := newSnapshotServer(dex.Disabled, store, peer)
	mgr := newEventStreamManager(&eventStreamManagerConfig{
		log:            dex.Disabled,
		eventLogReader: reader,
		node:           peer,
	})

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = mgr.run(ctx, ready)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				t.Errorf("stream manager stopped with error: %v", runErr)
			}
		case <-time.After(2 * time.Second):
			t.Error("stream manager did not stop")
		}
	})
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("stream manager did not become ready")
	}

	// The seed request starts a send on the snapshot server.
	srv.startSendOnConn(ctx, 1)
	t.Cleanup(func() { srv.stopConn(1) })

	deadline := time.After(2 * time.Second)
	for {
		mtx.Lock()
		finished := sawLast
		mtx.Unlock()
		if finished && !srv.sendingTo(1) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("snapshot never completed")
		case <-time.After(2 * time.Millisecond):
		}
	}
	mtx.Lock()
	if chunks == 0 {
		t.Fatal("no snapshot chunks sent")
	}
	mtx.Unlock()

	if mgr.isStreamingTo(1) {
		t.Fatal("stream manager should have no part in a snapshot send")
	}

	// No events flow after the snapshot: no stream exists until the seeded
	// slave subscribes.
	mgr.eventCommitted(6, "", nil)
	select {
	case seq := <-sentSeqs:
		t.Fatalf("empty slot sent seq %d", seq)
	case <-time.After(50 * time.Millisecond):
	}

	// The post-load event subscribe fills the slot and the tail flows.
	mgr.startStreamOnConn(1, snapFrontier)
	mgr.eventCommitted(6, "", nil)
	select {
	case seq := <-sentSeqs:
		if seq != 6 {
			t.Fatalf("streamed seq %d, want 6 (snapshot covered <=5)", seq)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event 6 not streamed after the event subscribe")
	}
}

// TestSubscribeRejectionClasses pins the permanent classification the
// subscriber's retry loop depends on: only a SubscribeRejectedError aborts
// retries; every other failure retries on the normal cadence.
func TestSubscribeRejectionClasses(t *testing.T) {
	for _, tt := range []struct {
		name          string
		err           error
		wantPermanent bool
	}{
		{"unauthorized retries", &peerRPCError{Code: msgjson.UnauthorizedConnection, Message: "x"}, false},
		{"try-again retries", &peerRPCError{Code: msgjson.TryAgainLaterError, Message: "x"}, false},
		{"subscribe-rejected permanent", &peerRPCError{Code: msgjson.SubscribeRejectedError, Message: "x"}, true},
		{"internal retries", &peerRPCError{Code: msgjson.RPCInternal, Message: "x"}, false},
		{"plain error retries", errors.New("x"), false},
		{"wrapped permanent", fmt.Errorf("wrap: %w", &peerRPCError{Code: msgjson.SubscribeRejectedError, Message: "x"}), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if permanent := subscribeRejectionPermanent(tt.err); permanent != tt.wantPermanent {
				t.Fatalf("permanent = %t, want %t", permanent, tt.wantPermanent)
			}
		})
	}
}
