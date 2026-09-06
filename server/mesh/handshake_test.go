// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/binary"
	"sync"

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
