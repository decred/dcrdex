// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"io"

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
