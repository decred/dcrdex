// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand"
	"testing"

	"decred.org/dcrdex/server/db"
)

// fakeSnapshotStore writes its payload and records loaded bytes.
type fakeSnapshotStore struct {
	payload  []byte
	frontier *db.EventLogPosition
	loaded   []byte
	loadErr  error
}

func (f *fakeSnapshotStore) WriteSnapshot(_ context.Context, w io.Writer) (*db.EventLogPosition, error) {
	if _, err := w.Write(f.payload); err != nil {
		return nil, err
	}
	return f.frontier, nil
}

func (f *fakeSnapshotStore) LoadSnapshot(_ context.Context, r io.Reader) (*db.EventLogPosition, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	f.loaded = b
	return f.frontier, nil
}

func TestSnapshotChunkRoundTrip(t *testing.T) {
	// The payload varies across chunk boundaries.
	payload := make([]byte, snapshotChunkBytes*3+500)
	rand.New(rand.NewSource(1)).Read(payload)
	frontier := &db.EventLogPosition{Seq: 42, TipHash: []byte("tip-hash")}

	var chunks []*snapshotChunk
	send := func(_ context.Context, c *snapshotChunk) error {
		if len(c.Bytes) > snapshotChunkBytes {
			t.Fatalf("chunk of %d bytes exceeds bound %d", len(c.Bytes), snapshotChunkBytes)
		}
		chunks = append(chunks, &snapshotChunk{Bytes: append([]byte(nil), c.Bytes...), Last: c.Last})
		return nil
	}

	producer := &fakeSnapshotStore{payload: payload, frontier: frontier}
	writeFrontier, err := streamSnapshot(context.Background(), producer, send)
	if err != nil {
		t.Fatalf("streamSnapshot: %v", err)
	}
	if writeFrontier.Seq != frontier.Seq {
		t.Fatalf("write frontier seq = %d, want %d", writeFrontier.Seq, frontier.Seq)
	}
	if len(chunks) == 0 || !chunks[len(chunks)-1].Last {
		t.Fatalf("final chunk not marked Last")
	}
	for i, c := range chunks[:len(chunks)-1] {
		if c.Last {
			t.Fatalf("non-final chunk %d marked Last", i)
		}
	}

	recv := &snapshotReceiver{}
	for i, c := range chunks {
		if recv.transferComplete() {
			t.Fatalf("transfer complete before chunk %d of %d", i, len(chunks))
		}
		last, err := recv.receiveChunk(c)
		if err != nil {
			t.Fatalf("receive chunk %d: %v", i, err)
		}
		if last != (i == len(chunks)-1) {
			t.Fatalf("chunk %d last = %t", i, last)
		}
	}
	if !recv.transferComplete() {
		t.Fatal("transfer not complete after the final chunk")
	}
	if recv.lastProgress().IsZero() {
		t.Fatal("no progress recorded")
	}

	consumer := &fakeSnapshotStore{frontier: frontier}
	loadFrontier, err := recv.load(context.Background(), consumer)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loadFrontier.Seq != frontier.Seq {
		t.Fatalf("load frontier seq = %d, want %d", loadFrontier.Seq, frontier.Seq)
	}
	if !bytes.Equal(consumer.loaded, payload) {
		t.Fatalf("reassembled payload mismatch: got %d bytes, want %d", len(consumer.loaded), len(payload))
	}
}

// TestSnapshotReceiverChunkAfterFinal checks that a completed receiver rejects chunks.
func TestSnapshotReceiverChunkAfterFinal(t *testing.T) {
	recv := &snapshotReceiver{}
	if _, err := recv.receiveChunk(&snapshotChunk{Bytes: []byte("abc"), Last: true}); err != nil {
		t.Fatalf("final chunk: %v", err)
	}
	progress := recv.lastProgress()
	if _, err := recv.receiveChunk(&snapshotChunk{Bytes: []byte("more")}); err == nil {
		t.Fatal("chunk after the final chunk accepted")
	}
	if recv.buf.String() != "abc" || !recv.transferComplete() || recv.lastProgress() != progress {
		t.Fatal("rejected chunk changed the receiver")
	}
}

// TestStreamSnapshotSendError checks that a send failure stops the transfer.
func TestStreamSnapshotSendError(t *testing.T) {
	producer := &fakeSnapshotStore{
		payload:  make([]byte, snapshotChunkBytes*3),
		frontier: &db.EventLogPosition{Seq: 7},
	}

	sendErr := errors.New("link down")
	var sends int
	send := func(_ context.Context, c *snapshotChunk) error {
		sends++
		if sends > 2 {
			t.Fatal("chunk sent after a send failure")
		}
		if sends == 2 {
			return sendErr
		}
		return nil
	}

	frontier, err := streamSnapshot(context.Background(), producer, send)
	if !errors.Is(err, sendErr) {
		t.Fatalf("streamSnapshot error = %v, want %v", err, sendErr)
	}
	if frontier != nil {
		t.Fatalf("frontier = %v for a failed transfer, want nil", frontier)
	}
	if sends != 2 {
		t.Fatalf("%d sends, want 2", sends)
	}
}

// TestSnapshotReceiverLoadError checks that a load failure returns no frontier.
func TestSnapshotReceiverLoadError(t *testing.T) {
	recv := &snapshotReceiver{}
	if _, err := recv.receiveChunk(&snapshotChunk{Bytes: []byte("junk"), Last: true}); err != nil {
		t.Fatalf("final chunk: %v", err)
	}

	loadErr := errors.New("bad gob")
	frontier, err := recv.load(context.Background(), &fakeSnapshotStore{loadErr: loadErr})
	if !errors.Is(err, loadErr) {
		t.Fatalf("load error = %v, want %v", err, loadErr)
	}
	if frontier != nil {
		t.Fatalf("frontier = %v for a failed load, want nil", frontier)
	}
}

// TestSnapshotChunkBoundaries checks the empty final chunk.
// Empty input and exact chunk multiples both need this marker.
func TestSnapshotChunkBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name       string
		payloadLen int
		wantChunks int
	}{
		{"exact multiple", snapshotChunkBytes * 2, 3},
		{"empty payload", 0, 1},
	} {
		payload := make([]byte, tt.payloadLen)
		rand.New(rand.NewSource(2)).Read(payload)
		producer := &fakeSnapshotStore{payload: payload, frontier: &db.EventLogPosition{Seq: 3}}

		var chunks []*snapshotChunk
		send := func(_ context.Context, c *snapshotChunk) error {
			chunks = append(chunks, &snapshotChunk{Bytes: append([]byte(nil), c.Bytes...), Last: c.Last})
			return nil
		}
		if _, err := streamSnapshot(context.Background(), producer, send); err != nil {
			t.Fatalf("%s: streamSnapshot: %v", tt.name, err)
		}
		if len(chunks) != tt.wantChunks {
			t.Fatalf("%s: %d chunks, want %d", tt.name, len(chunks), tt.wantChunks)
		}
		last := chunks[len(chunks)-1]
		if !last.Last || len(last.Bytes) != 0 {
			t.Fatalf("%s: final chunk Last=%t with %d bytes, want an empty Last chunk", tt.name, last.Last, len(last.Bytes))
		}

		recv := &snapshotReceiver{}
		for i, c := range chunks {
			if _, err := recv.receiveChunk(c); err != nil {
				t.Fatalf("%s: receive chunk %d: %v", tt.name, i, err)
			}
		}
		consumer := &fakeSnapshotStore{frontier: &db.EventLogPosition{Seq: 3}}
		if _, err := recv.load(context.Background(), consumer); err != nil {
			t.Fatalf("%s: load: %v", tt.name, err)
		}
		if !bytes.Equal(consumer.loaded, payload) {
			t.Fatalf("%s: reassembled %d bytes, want %d", tt.name, len(consumer.loaded), len(payload))
		}
	}
}
