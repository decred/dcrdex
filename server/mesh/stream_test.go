// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
)

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
