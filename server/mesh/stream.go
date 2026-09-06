// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
)

const (
	// eventStreamBatchLimit is the maximum number of events in a batch.
	eventStreamBatchLimit = 100
	// eventStreamBatchBytes is the target total envelope size in a batch.
	eventStreamBatchBytes = 1 << 20 // 1 MiB
)

// streamNode is the node surface required by eventStreamManager.
type streamNode interface {
	sendEventBatch(ctx context.Context, connID uint64, batch *eventBatch) error
	handleStreamError(connID uint64, err error)
}

type eventStreamManagerConfig struct {
	log                dex.Logger
	eventLogReader     db.EventLogReader
	node               streamNode
	initialFrontierSeq uint64
}

// eventStreamManager keeps a slave up to date with the master's event log.
//
// The slave subscribes with its committed log position. The manager reads
// later events from the database and sends them in order. It waits for the
// slave to acknowledge each batch before sending the next one. Once the
// slave catches up, the stream waits for new events (calls to eventCommitted)
// and then sends those as well.
//
// Only one subscription is served at a time. A new subscription cancels the
// previous stream and starts from the position supplied by the slave.
// Results of forwarded commands travel with their events. These results are
// held in memory until acknowledged and are discarded if the stream ends.
type eventStreamManager struct {
	log            dex.Logger
	eventLogReader db.EventLogReader
	node           streamNode
	wake           chan struct{}
	ctx            context.Context

	mtx          sync.Mutex
	stopped      bool
	availableTip uint64
	activeStream *eventStream
}

func newEventStreamManager(cfg *eventStreamManagerConfig) *eventStreamManager {
	return &eventStreamManager{
		log:            cfg.log,
		eventLogReader: cfg.eventLogReader,
		node:           cfg.node,
		wake:           make(chan struct{}, 1),
		availableTip:   cfg.initialFrontierSeq,
	}
}

func (s *eventStreamManager) run(ctx context.Context, ready chan<- struct{}) error {
	defer func() {
		s.mtx.Lock()
		s.stopped = true
		s.mtx.Unlock()
	}()

	s.ctx = ctx
	close(ready)

	for {
		stream, tip := s.waitForWork(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err := stream.sendThrough(tip, s.eventLogReader, s.node.sendEventBatch); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.failStream(stream, err)
		}
	}
}

// waitForWork blocks until there is an active stream whose cursor is behind
// the available tip, or ctx ends.
func (s *eventStreamManager) waitForWork(ctx context.Context) (*eventStream, uint64) {
	for {
		s.mtx.Lock()
		stream := s.activeStream
		tip := s.availableTip
		s.mtx.Unlock()

		if stream != nil && stream.cursorSeq() < tip {
			return stream, tip
		}

		select {
		case <-ctx.Done():
			return nil, 0
		case <-s.wake:
		}
	}
}

// wakeUp wakes up the waitForWork loop.
func (s *eventStreamManager) wakeUp() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// startStreamOnConn is called after the slave sent a subscription request.
// It replaces the existing stream.
func (s *eventStreamManager) startStreamOnConn(connID uint64, slaveFrontier *db.EventLogPosition) {
	// Refresh the frontier in case it has advanced.
	frontier, err := s.eventLogReader.EventLogFrontier(s.ctx)
	if err != nil {
		// Keep the old tip. Event notifications can advance it.
		s.log.Errorf("Could not refresh the available tip from the event log: %v", err)
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.stopped {
		s.log.Errorf("event stream stopped")
		return
	}

	if frontier != nil && frontier.Seq > s.availableTip {
		s.availableTip = frontier.Seq
	}

	if s.activeStream != nil {
		s.activeStream.kill()
	}

	s.activeStream = newEventStream(s.ctx, connID, slaveFrontier)

	s.wakeUp()
}

// stopConn cancels and removes the active stream if it belongs to connID.
func (s *eventStreamManager) stopConn(connID uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.activeStream == nil || s.activeStream.connID != connID {
		return
	}

	s.activeStream.kill()
	s.activeStream = nil
}

// failStream removes and cancels stream if it is still active.
// It reports errors other than context.Canceled to the node.
func (s *eventStreamManager) failStream(stream *eventStream, err error) {
	s.mtx.Lock()
	if s.activeStream != stream {
		s.mtx.Unlock()
		return
	}
	s.activeStream = nil
	s.mtx.Unlock()

	stream.kill()

	if errors.Is(err, context.Canceled) {
		return
	}

	s.node.handleStreamError(stream.connID, err)
}

// eventCommitted updates the available log tip, records any command
// result for the active stream, and wakes the sender.
func (s *eventStreamManager) eventCommitted(seq uint64, originCommandID string, commandResult json.RawMessage) {
	s.mtx.Lock()
	if seq > s.availableTip {
		s.availableTip = seq
	}
	if s.activeStream != nil && originCommandID != "" {
		s.activeStream.addMeta(seq, originCommandID, commandResult)
	}
	s.mtx.Unlock()

	s.wakeUp()
}

// refreshAvailableTip raises the available tip to the durable log frontier.
func (s *eventStreamManager) refreshAvailableTip(ctx context.Context) error {
	frontier, err := s.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return fmt.Errorf("could not refresh the available tip from the event log: %w", err)
	}
	if frontier == nil {
		return nil
	}

	s.mtx.Lock()
	raised := frontier.Seq > s.availableTip
	if raised {
		s.availableTip = frontier.Seq
	}
	s.mtx.Unlock()

	if raised {
		s.wakeUp()
	}

	return nil
}

// isStreamingTo reports whether the active stream belongs to connID.
func (s *eventStreamManager) isStreamingTo(connID uint64) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.activeStream != nil && s.activeStream.connID == connID
}

// progress reports the available tip and the active stream cursor.
// The cursor is zero when no stream is active.
func (s *eventStreamManager) progress() (tip, cursor uint64, active bool) {
	s.mtx.Lock()
	tip = s.availableTip
	stream := s.activeStream
	s.mtx.Unlock()
	if stream != nil {
		active = true
		cursor = stream.cursorSeq()
	}
	return tip, cursor, active
}

// pendingResults counts unacknowledged command results on the active stream.
// It returns zero when no stream is active.
func (s *eventStreamManager) pendingResults() int {
	s.mtx.Lock()
	stream := s.activeStream
	s.mtx.Unlock()
	if stream == nil {
		return 0
	}
	return stream.pendingMetaCount()
}

// streamCommandResult holds the results of a forwarded command.
type streamCommandResult struct {
	originCommandID string
	commandResult   json.RawMessage
}

// eventStreamSendFunc sends a batch and waits for the slave to acknowledge it.
type eventStreamSendFunc func(ctx context.Context, connID uint64, batch *eventBatch) error

// eventStream is a single event stream over a single connection.
type eventStream struct {
	connID uint64
	ctx    context.Context
	kill   context.CancelFunc

	mtx            sync.Mutex
	cursor         uint64
	pendingResults map[uint64]*streamCommandResult
}

func newEventStream(parent context.Context, connID uint64, slaveFrontier *db.EventLogPosition) *eventStream {
	ctx, cancel := context.WithCancel(parent)
	return &eventStream{
		connID:         connID,
		ctx:            ctx,
		kill:           cancel,
		cursor:         slaveFrontier.Seq,
		pendingResults: make(map[uint64]*streamCommandResult),
	}
}

// sendThrough reads and sends events after the cursor through the target
// sequence.
// It returns the first read or send error.
func (r *eventStream) sendThrough(through uint64, reader db.EventLogReader, send eventStreamSendFunc) error {
	cursor := r.cursorSeq()
	for cursor < through {
		entries, err := r.readEntries(reader, cursor, through)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("event stream gap after seq %d before target %d", cursor, through)
		}
		if cursor, err = r.batchAndSend(entries, cursor, through, send); err != nil {
			return err
		}
	}
	return nil
}

// readEntries retrieves a set of event log entries.
func (r *eventStream) readEntries(reader db.EventLogReader, after, through uint64) ([]*db.EventLogEntry, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	limit := eventStreamBatchLimit
	if remaining := through - after; remaining < uint64(limit) {
		limit = int(remaining)
	}
	return reader.EventLogEntriesAfter(r.ctx, after, limit)
}

// batchAndSend groups entries by encoded size and sends each batch.
func (r *eventStream) batchAndSend(entries []*db.EventLogEntry, cursor, through uint64, send eventStreamSendFunc) (uint64, error) {
	if err := r.ctx.Err(); err != nil {
		return cursor, err
	}
	batch := &eventBatch{Entries: make([]*eventEnvelope, 0, len(entries))}
	batchBytes := 0
	for _, entry := range entries {
		envelope := r.eventEnvelopeForEntry(entry, through)
		raw, err := json.Marshal(envelope)
		if err != nil {
			return cursor, err
		}
		if len(batch.Entries) > 0 && batchBytes+len(raw) > eventStreamBatchBytes {
			if cursor, err = r.deliverBatch(batch, send); err != nil {
				return cursor, err
			}
			batch = &eventBatch{Entries: make([]*eventEnvelope, 0, len(entries))}
			batchBytes = 0
		}
		batch.Entries = append(batch.Entries, envelope)
		batchBytes += len(raw)
	}
	return r.deliverBatch(batch, send)
}

// eventEnvelopeForEntry copies an event row into an envelope and adds any
// pending command result.
func (r *eventStream) eventEnvelopeForEntry(entry *db.EventLogEntry, masterTip uint64) *eventEnvelope {
	envelope := &eventEnvelope{
		Seq:       entry.Seq,
		TipHash:   append([]byte(nil), entry.TipHash...),
		MasterTip: masterTip,
		Kind:      entry.Kind,
		Payload:   append([]byte(nil), entry.Event...),
	}

	r.mtx.Lock()
	meta := r.pendingResults[entry.Seq]
	if meta != nil {
		envelope.OriginCommandID = meta.originCommandID
		envelope.CommandResult = append([]byte(nil), meta.commandResult...)
	}
	r.mtx.Unlock()

	return envelope
}

// eventBatchWireBytes returns the encoded size of a batch request.
func eventBatchWireBytes(batch *eventBatch) (int, error) {
	req, err := msgjson.NewRequest(math.MaxUint64, eventEnvelopeRoute, batch)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

// deliverBatch sends the batch to the slave and updates the cursor.
func (r *eventStream) deliverBatch(batch *eventBatch, send eventStreamSendFunc) (uint64, error) {
	// Do not send if the batch is larger than the mesh read limit.
	wireBytes, err := eventBatchWireBytes(batch)
	if err != nil {
		return 0, err
	}
	if wireBytes > meshReadLimit {
		return 0, fmt.Errorf("event stream batch ending at seq %d encoded size %d exceeds mesh read limit %d",
			batch.Entries[len(batch.Entries)-1].Seq, wireBytes, meshReadLimit)
	}

	if err := send(r.ctx, r.connID, batch); err != nil {
		return 0, err
	}

	// Update the cursor and remove pending command results.
	last := batch.Entries[len(batch.Entries)-1].Seq
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if last > r.cursor {
		r.cursor = last
	}
	for _, envelope := range batch.Entries {
		delete(r.pendingResults, envelope.Seq)
	}
	return last, nil
}

// cursorSeq returns the last acknowledged sequence.
func (r *eventStream) cursorSeq() uint64 {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return r.cursor
}

// addMeta copies a command result into the pending results for seq.
func (r *eventStream) addMeta(seq uint64, originCommandID string, commandResult json.RawMessage) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.pendingResults[seq] = &streamCommandResult{
		originCommandID: originCommandID,
		commandResult:   append([]byte(nil), commandResult...),
	}
}

// pendingMetaCount counts unacknowledged command results held by the stream.
func (r *eventStream) pendingMetaCount() int {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return len(r.pendingResults)
}

// startEventStream opens an event stream from the slave's subscribed frontier.
func (n *node) startEventStream(peerConn *nodeConn, slaveFrontier *db.EventLogPosition) {
	n.log.Infof("Mesh event stream opening with peer %s from subscribed frontier %s.",
		meshPeerLogID(peerConn), slaveFrontier)
	n.stream.startStreamOnConn(peerConn.ID(), slaveFrontier)
}

// stopStreamForConn stops event and snapshot sends for peerConn.
// It does nothing if peerConn is nil.
func (n *node) stopStreamForConn(peerConn *nodeConn) {
	if peerConn != nil {
		n.stream.stopConn(peerConn.ID())
		n.snapshots.stopConn(peerConn.ID())
	}
}

// notifyLocalEventCommitted makes a durable local event and its command result
// available to the stream.
func (n *node) notifyLocalEventCommitted(seq uint64, originCommandID string, commandResult json.RawMessage) {
	n.stream.eventCommitted(seq, originCommandID, commandResult)
}

// sendEventBatch sends a batch to the active slave identified by connID and
// waits for acknowledgement.
// It returns context.Canceled if the connection is unavailable or closes during
// a failed request.
func (n *node) sendEventBatch(ctx context.Context, connID uint64, batch *eventBatch) error {
	conn, err := n.activePeerForRequest(nodeMode.canStreamEvents)
	if err != nil || conn.ID() != connID {
		return context.Canceled
	}
	err = requestEventBatch(ctx, conn.link.Request, batch)
	if err != nil {
		select {
		case <-conn.link.Done():
			return context.Canceled
		default:
		}
	}
	return err
}

// requestEventBatch sends a batch to the slave and waits for its
// acknowledgement.
// It returns without sending if ctx has ended.
func requestEventBatch(ctx context.Context, request func(context.Context, string, any, any) error, batch *eventBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var ack eventAck
	return request(ctx, eventEnvelopeRoute, batch, &ack)
}

// handleStreamError reports a failed event or snapshot stream for the active
// slave connection.
func (n *node) handleStreamError(connID uint64, err error) {
	state := n.control.currentState()
	if !state.mode.canStreamEvents() || state.activeConn == nil || state.activeConn.ID() != connID {
		return
	}
	_ = n.control.post(streamFailedSignal{
		conn: state.activeConn,
		err:  err,
		at:   time.Now(),
	})
}
