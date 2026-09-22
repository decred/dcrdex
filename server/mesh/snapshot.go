// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

const (
	snapshotChunkBytes = 1 << 20 // 1 MiB
	// meshReadLimit is the websocket read limit for mesh connections. It must
	// comfortably fit a JSON encoded snapshot chunk.
	meshReadLimit    = 4 * snapshotChunkBytes
	maxSnapshotBytes = 256 << 20 // 256 MiB
)

// snapshotChunk is one chunk of a snapshot.
// The final chunk has Last set and may be empty.
type snapshotChunk struct {
	Bytes dex.Bytes `json:"bytes"`
	Last  bool      `json:"last,omitempty"`
}

// snapshotChunkFunc sends one chunk and waits for the receiver to acknowledge it.
type snapshotChunkFunc func(ctx context.Context, chunk *snapshotChunk) error

// snapshotNode is the node surface required for snapshotServer.
type snapshotNode interface {
	// sendSnapshotChunk returns context.Canceled if connID is no longer the
	// active slave.
	sendSnapshotChunk(ctx context.Context, connID uint64, chunk *snapshotChunk) error
	handleStreamError(connID uint64, err error)
}

// snapshotSend is one snapshot transfer to a seeding slave.
type snapshotSend struct {
	connID uint64
	ctx    context.Context
	kill   context.CancelFunc
}

// snapshotServer tracks the active snapshot send to a seeding slave.
// A replaced send can still be running while it handles cancellation.
type snapshotServer struct {
	log   dex.Logger
	store db.SnapshotStore
	node  snapshotNode

	mtx    sync.Mutex
	active *snapshotSend
}

// newSnapshotServer creates a snapshot server with no active transfer.
func newSnapshotServer(log dex.Logger, store db.SnapshotStore, node snapshotNode) *snapshotServer {
	return &snapshotServer{log: log, store: store, node: node}
}

// startSendOnConn starts a snapshot send to connID.
// It cancels the previous send without waiting for it to stop.
func (s *snapshotServer) startSendOnConn(parent context.Context, connID uint64) {
	ctx, cancel := context.WithCancel(parent)
	send := &snapshotSend{connID: connID, ctx: ctx, kill: cancel}

	s.mtx.Lock()
	if s.active != nil {
		s.active.kill()
	}
	s.active = send
	s.mtx.Unlock()

	go s.runSend(send)
}

func (s *snapshotServer) runSend(send *snapshotSend) {
	defer send.kill()

	frontier, err := streamSnapshot(send.ctx, s.store, func(ctx context.Context, chunk *snapshotChunk) error {
		return s.node.sendSnapshotChunk(ctx, send.connID, chunk)
	})

	s.mtx.Lock()
	if s.active == send {
		s.active = nil
	}
	s.mtx.Unlock()

	if err != nil {
		if send.ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		s.node.handleStreamError(send.connID, err)
		return
	}

	s.log.Infof("Sent snapshot to conn %d at frontier %s. Events flow once the seeded slave subscribes.",
		send.connID, frontier)
}

// stopConn cancels and clears the active send if its connection ID matches.
// It does not wait for the send to finish.
func (s *snapshotServer) stopConn(connID uint64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.active == nil || s.active.connID != connID {
		return
	}
	s.active.kill()
	s.active = nil
}

// sendingTo reports whether the active snapshot send targets connID.
func (s *snapshotServer) sendingTo(connID uint64) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.active != nil && s.active.connID == connID
}

// streamSnapshot sends the producer's snapshot in chunks.
// It returns the snapshot frontier after the final chunk is acknowledged.
func streamSnapshot(ctx context.Context, producer db.SnapshotStore, send snapshotChunkFunc) (*db.EventLogPosition, error) {
	cw := &chunkWriter{ctx: ctx, send: send}
	frontier, err := producer.WriteSnapshot(ctx, cw)
	if err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}
	if err := cw.flush(); err != nil {
		return nil, fmt.Errorf("flush snapshot: %w", err)
	}
	return frontier, nil
}

// chunkWriter splits snapshot bytes into chunks.
type chunkWriter struct {
	ctx  context.Context
	send snapshotChunkFunc
	buf  []byte
}

// Write buffers p and sends it in chunks.
func (cw *chunkWriter) Write(p []byte) (int, error) {
	cw.buf = append(cw.buf, p...)
	for len(cw.buf) >= snapshotChunkBytes {
		chunk := append([]byte(nil), cw.buf[:snapshotChunkBytes]...)
		if err := cw.send(cw.ctx, &snapshotChunk{Bytes: chunk}); err != nil {
			return len(p), err
		}
		cw.buf = cw.buf[snapshotChunkBytes:]
	}
	return len(p), nil
}

// flush sends the remaining bytes with Last set to true.
// It sends an empty final chunk when no bytes remain.
func (cw *chunkWriter) flush() error {
	return cw.send(cw.ctx, &snapshotChunk{Bytes: append([]byte(nil), cw.buf...), Last: true})
}

// snapshotReceiver collects the chunks of one snapshot.
// It holds the whole snapshot in memory for the load into the DB.
type snapshotReceiver struct {
	mtx      sync.Mutex
	buf      bytes.Buffer
	done     bool
	lastRecv time.Time
}

// receiveChunk appends a snapshot chunk and reports whether it is the final chunk.
// It rejects chunks after completion and chunks that would exceed the size limit.
// A rejected chunk leaves the receiver unchanged.
func (r *snapshotReceiver) receiveChunk(chunk *snapshotChunk) (last bool, err error) {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if r.done {
		return false, fmt.Errorf("snapshot chunk after final chunk")
	}
	if r.buf.Len()+len(chunk.Bytes) > maxSnapshotBytes {
		return false, fmt.Errorf("snapshot exceeds maximum size of %d bytes", maxSnapshotBytes)
	}
	r.buf.Write(chunk.Bytes)
	r.lastRecv = time.Now()
	if chunk.Last {
		r.done = true
	}
	return chunk.Last, nil
}

// load loads the snapshot into the DB.
// It refuses to load before the final chunk arrives.
func (r *snapshotReceiver) load(ctx context.Context, store db.SnapshotStore) (*db.EventLogPosition, error) {
	r.mtx.Lock()
	if !r.done {
		r.mtx.Unlock()
		return nil, fmt.Errorf("snapshot load before final chunk")
	}
	reader := bytes.NewReader(r.buf.Bytes())
	r.mtx.Unlock()

	frontier, err := store.LoadSnapshot(ctx, reader)
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	return frontier, nil
}

// transferComplete reports whether the final chunk has been received.
func (r *snapshotReceiver) transferComplete() bool {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return r.done
}

// lastProgress returns the time of the last accepted chunk.
func (r *snapshotReceiver) lastProgress() time.Time {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return r.lastRecv
}

// startSnapshotSend starts a snapshot send to the peer.
func (n *node) startSnapshotSend(peerConn *nodeConn) {
	n.log.Infof("Starting snapshot send to peer %s.", meshPeerLogID(peerConn))
	n.snapshots.startSendOnConn(n.runContext, peerConn.ID())
}

// sendSnapshotChunk sends one snapshot chunk to the active slave identified by
// connID and waits for its ack. It returns context.Canceled if the node cannot
// stream to connID or a request fails after the link closes.
func (n *node) sendSnapshotChunk(ctx context.Context, connID uint64, chunk *snapshotChunk) error {
	conn, err := n.activePeerForRequest(nodeMode.canStreamEvents)
	if err != nil || conn.ID() != connID {
		return context.Canceled
	}
	var ack eventAck
	err = conn.link.Request(ctx, snapshotChunkRoute, chunk, &ack)
	if err != nil {
		select {
		case <-conn.link.Done():
			return context.Canceled
		default:
		}
	}
	return err
}
