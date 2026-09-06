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

type streamTestReader struct {
	mtx      sync.Mutex
	frontier *db.EventLogPosition
	entries  []*db.EventLogEntry
}

func (r *streamTestReader) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return r.frontier, nil
}

func (r *streamTestReader) EventLogEntriesAfter(ctx context.Context, after uint64, limit int) ([]*db.EventLogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()
	var entries []*db.EventLogEntry
	for _, entry := range r.entries {
		if entry.Seq > after {
			entries = append(entries, entry)
			if len(entries) == limit {
				break
			}
		}
	}
	return entries, nil
}

type testSequenceStreamRequester struct {
	errs  []error
	calls int
}

func (r *testSequenceStreamRequester) Request(_ context.Context, route string, payload any, response any) error {
	r.calls++
	if route != eventEnvelopeRoute {
		return fmt.Errorf("route = %q, want %q", route, eventEnvelopeRoute)
	}
	if _, ok := payload.(*eventBatch); !ok {
		return fmt.Errorf("payload = %T, want *eventBatch", payload)
	}
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		if err != nil {
			return err
		}
	}
	ack, ok := response.(*eventAck)
	if !ok {
		return fmt.Errorf("response = %T, want *eventAck", response)
	}
	*ack = eventAck{}
	return nil
}

type sentEventEnvelope struct {
	connID   uint64
	envelope *eventEnvelope
}

type eventStreamManagerHarness struct {
	manager *eventStreamManager
	cancel  context.CancelFunc
	done    chan error
	sent    chan sentEventEnvelope
}

type testStreamNode struct {
	send      func(context.Context, uint64, *eventBatch) error
	sendChunk func(context.Context, uint64, *snapshotChunk) error
	fail      func(uint64, error)
}

func (p *testStreamNode) sendEventBatch(ctx context.Context, connID uint64, batch *eventBatch) error {
	if p.send == nil {
		return nil
	}
	return p.send(ctx, connID, batch)
}

func (p *testStreamNode) sendSnapshotChunk(ctx context.Context, connID uint64, chunk *snapshotChunk) error {
	if p.sendChunk == nil {
		return nil
	}
	return p.sendChunk(ctx, connID, chunk)
}

func (p *testStreamNode) handleStreamError(connID uint64, err error) {
	if p.fail != nil {
		p.fail(connID, err)
	}
}

func newTestEventStreamManager(eventLogReader db.EventLogReader, sendEvent func(context.Context, uint64, *eventBatch) error, onError func(uint64, error), initialFrontier *db.EventLogPosition) *eventStreamManager {
	if initialFrontier == nil {
		initialFrontier = &db.EventLogPosition{}
	}
	return newEventStreamManager(&eventStreamManagerConfig{
		log:                dex.Disabled,
		eventLogReader:     eventLogReader,
		node:               &testStreamNode{send: sendEvent, fail: onError},
		initialFrontierSeq: initialFrontier.Seq,
	})
}

func newStreamHarness(t *testing.T, entries []*db.EventLogEntry, initialFrontier *db.EventLogPosition) *eventStreamManagerHarness {
	t.Helper()
	h := &eventStreamManagerHarness{
		sent: make(chan sentEventEnvelope, 32),
		done: make(chan error, 1),
	}
	reader := &streamTestReader{entries: entries}
	// Flatten batches into per-envelope sends so assertions can observe
	// individual envelopes.
	sendEvent := func(ctx context.Context, connID uint64, batch *eventBatch) error {
		for _, envelope := range batch.Entries {
			select {
			case h.sent <- sentEventEnvelope{connID: connID, envelope: cloneEventEnvelope(envelope)}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	h.manager = newTestEventStreamManager(reader, sendEvent, func(uint64, error) {}, initialFrontier)

	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	ready := make(chan struct{})
	go func() {
		h.done <- h.manager.run(ctx, ready)
	}()
	select {
	case <-ready:
	case err := <-h.done:
		t.Fatalf("event stream manager stopped before ready: %v", err)
	case <-time.After(time.Second):
		t.Fatalf("event stream manager did not become ready")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-h.done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("event stream manager stopped with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("event stream manager did not stop")
		}
	})
	return h
}

func (h *eventStreamManagerHarness) start(connID uint64, slaveFrontier *db.EventLogPosition) {
	h.manager.startStreamOnConn(connID, slaveFrontier)
}

func (h *eventStreamManagerHarness) stop(connID uint64) {
	h.manager.stopConn(connID)
}

func (h *eventStreamManagerHarness) commit(seq uint64) {
	h.commitWithResult(seq, "", nil)
}

func (h *eventStreamManagerHarness) commitWithResult(seq uint64, originCommandID string, commandResult []byte) {
	h.manager.eventCommitted(seq, originCommandID, commandResult)
}

func (h *eventStreamManagerHarness) wantSent(t *testing.T, connID, seq, masterTip uint64) *eventEnvelope {
	t.Helper()
	var got sentEventEnvelope
	select {
	case got = <-h.sent:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for sent event envelope")
	}
	if got.connID != connID || got.envelope.Seq != seq || got.envelope.MasterTip != masterTip {
		t.Fatalf("sent envelope = conn %d seq %d tip %d, want %d/%d/%d",
			got.connID, got.envelope.Seq, got.envelope.MasterTip, connID, seq, masterTip)
	}
	return got.envelope
}

func (h *eventStreamManagerHarness) wantNoSent(t *testing.T) {
	t.Helper()
	select {
	case sent := <-h.sent:
		t.Fatalf("unexpected sent envelope: conn=%d envelope=%+v", sent.connID, sent.envelope)
	case <-time.After(20 * time.Millisecond):
	}
}

func (h *eventStreamManagerHarness) wantActiveStream(t *testing.T, connID uint64) {
	t.Helper()
	waitActiveStreamConnID(t, h.manager, connID)
}

func (h *eventStreamManagerHarness) wantNoActiveStream(t *testing.T) {
	t.Helper()
	waitNoActiveStream(t, h.manager)
}

func entry(seq uint64) *db.EventLogEntry {
	return &db.EventLogEntry{
		Seq:     seq,
		Kind:    "test",
		Event:   []byte(fmt.Sprintf(`{"seq":%d}`, seq)),
		TipHash: testTipHash(seq),
	}
}

func entries(seqs ...uint64) []*db.EventLogEntry {
	entries := make([]*db.EventLogEntry, 0, len(seqs))
	for _, seq := range seqs {
		entries = append(entries, entry(seq))
	}
	return entries
}

func pos(seq uint64) *db.EventLogPosition {
	if seq == 0 {
		return &db.EventLogPosition{}
	}
	return &db.EventLogPosition{Seq: seq, TipHash: testTipHash(seq)}
}

func cloneEventEnvelope(envelope *eventEnvelope) *eventEnvelope {
	if envelope == nil {
		return nil
	}
	return &eventEnvelope{
		Seq:             envelope.Seq,
		TipHash:         append([]byte(nil), envelope.TipHash...),
		MasterTip:       envelope.MasterTip,
		Kind:            envelope.Kind,
		OriginCommandID: envelope.OriginCommandID,
		CommandResult:   append([]byte(nil), envelope.CommandResult...),
		Payload:         append([]byte(nil), envelope.Payload...),
	}
}

func activeStream(stream *eventStreamManager) *eventStream {
	stream.mtx.Lock()
	defer stream.mtx.Unlock()
	return stream.activeStream
}

func activeStreamConnID(manager *eventStreamManager) uint64 {
	stream := activeStream(manager)
	if stream == nil {
		return 0
	}
	return stream.connID
}

func waitActiveStreamConnID(t *testing.T, manager *eventStreamManager, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for activeStreamConnID(manager) != want {
		if time.Now().After(deadline) {
			t.Fatalf("active stream connection ID = %d, want %d", activeStreamConnID(manager), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitNoActiveStream(t *testing.T, manager *eventStreamManager) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for activeStream(manager) != nil {
		if time.Now().After(deadline) {
			t.Fatalf("active stream = %p, want nil", activeStream(manager))
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEventStreamManager(t *testing.T) {
	h := newStreamHarness(t, entries(1, 2, 3), pos(2))

	h.wantNoSent(t)
	h.start(10, pos(0))
	h.wantSent(t, 10, 1, 2)
	h.wantSent(t, 10, 2, 2)

	commandResult := []byte(`{"status":"ok"}`)
	h.commitWithResult(3, "cmd-3", commandResult)
	live := h.wantSent(t, 10, 3, 3)
	if live.OriginCommandID != "cmd-3" || string(live.CommandResult) != string(commandResult) {
		t.Fatalf("live command result = %q/%s, want cmd-3/%s",
			live.OriginCommandID, live.CommandResult, commandResult)
	}

	h.start(20, pos(1))
	h.wantActiveStream(t, 20)
	h.stop(10)
	h.wantActiveStream(t, 20)
	h.wantSent(t, 20, 2, 3)
	h.wantSent(t, 20, 3, 3)

	h.stop(20)
	h.wantNoActiveStream(t)
}

// TestStreamReplacementDropsPendingCommandResults checks that a replacement
// stream drops pending command results.
func TestStreamReplacementDropsPendingCommandResults(t *testing.T) {
	entry := &db.EventLogEntry{Seq: 1, Kind: "k", Event: []byte(`"e1"`), TipHash: testTipHash(1)}
	reader := &streamTestReader{entries: []*db.EventLogEntry{entry}}

	entered := make(chan struct{})
	sentSeqs := make(chan *eventEnvelope, 8)
	mgr := newEventStreamManager(&eventStreamManagerConfig{
		log:            dex.Disabled,
		eventLogReader: reader,
		node: &testStreamNode{
			send: func(ctx context.Context, connID uint64, batch *eventBatch) error {
				if connID == 1 {
					// Keep the result pending until
					// replacement cancels this send.
					close(entered)
					<-ctx.Done()
					return ctx.Err()
				}
				for _, e := range batch.Entries {
					sentSeqs <- e
				}
				return nil
			},
		},
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

	// Block the send to conn 1 so its command result stays pending.
	mgr.startStreamOnConn(1, &db.EventLogPosition{})
	mgr.eventCommitted(1, "cmd-1", []byte(`"result"`))

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original stream did not enter send")
	}

	// Replacement cancels the blocked send and starts from the same
	// position.
	mgr.startStreamOnConn(2, &db.EventLogPosition{})
	mgr.eventCommitted(1, "", nil)

	select {
	case envelope := <-sentSeqs:
		if envelope.Seq != 1 {
			t.Fatalf("streamed seq %d, want 1", envelope.Seq)
		}
		if envelope.OriginCommandID != "" || len(envelope.CommandResult) != 0 {
			t.Fatalf("replacement stream carried a pending result for command %q", envelope.OriginCommandID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("entry not streamed on the replacement stream")
	}
}

func TestRequestEventBatch(t *testing.T) {
	envelope := &eventEnvelope{
		Seq:       1,
		TipHash:   testTipHash(1),
		MasterTip: 1,
		Kind:      "test",
	}

	tests := []struct {
		name      string
		errs      []error
		cancel    bool
		wantErr   bool
		wantCalls int
	}{
		{
			name:      "unauthorized is not retried",
			errs:      []error{msgjson.NewError(msgjson.UnauthorizedConnection, "not active yet")},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name:      "internal error is not retried",
			errs:      []error{msgjson.NewError(msgjson.RPCInternal, "apply failed")},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name:      "plain error is not retried",
			errs:      []error{errors.New("connection failed")},
			wantErr:   true,
			wantCalls: 1,
		},
		{
			name:    "canceled context is not sent",
			cancel:  true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			requester := &testSequenceStreamRequester{errs: append([]error(nil), tt.errs...)}
			err := requestEventBatch(ctx, requester.Request, &eventBatch{Entries: []*eventEnvelope{envelope}})
			if (err != nil) != tt.wantErr {
				t.Fatalf("requestEventBatch error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("requestEventBatch error = %v, want context.Canceled", err)
			}
			if requester.calls != tt.wantCalls {
				t.Fatalf("request calls = %d, want %d", requester.calls, tt.wantCalls)
			}
		})
	}
}
