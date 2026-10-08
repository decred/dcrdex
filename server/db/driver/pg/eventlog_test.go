//go:build !pgonline

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"

	"decred.org/dcrdex/server/db"
)

func TestEventLogHash(t *testing.T) {
	// These fixed SHA256 vectors use big endian sequence numbers and lengths.
	// The last two cases have identical concatenated fields but different
	// boundaries, so their hashes must differ.
	const firstTip = "02ecbbe1dec48fbd80f2778e8cad0fee0e69136a31695d054926599487f3999b"
	for _, tt := range []struct {
		name, prevTip       string
		seq                 uint64
		kind, event, txData string
		want                string
	}{
		{
			name: "first entry", seq: 1,
			kind: "test_event_a", event: "one", txData: `{"n":1}`,
			want: firstTip,
		},
		{
			name: "chained entry", prevTip: firstTip, seq: 2,
			kind: "test_event_b", event: "two", txData: `{"n":2}`,
			want: "157ddd209fddff9885e3a6c267d56c68ba0fac7e2ca47df66644e97ba5248d15",
		},
		{
			name: "short kind", seq: 1, kind: "a", event: "bc",
			want: "7cd967cbe66c0a3c86a72cc53fc4f7548ca7ab2b92d6fd34a94c485e7a0f0817",
		},
		{
			name: "short event", seq: 1, kind: "ab", event: "c",
			want: "fd2929fd77085fc1f6a15d14e501933478beeb773eebb3c49679156702a824d1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prevTip, err := hex.DecodeString(tt.prevTip)
			if err != nil {
				t.Fatal(err)
			}
			got := eventLogHash(prevTip, tt.seq, tt.kind, []byte(tt.event), []byte(tt.txData))
			if hex.EncodeToString(got) != tt.want {
				t.Fatalf("eventLogHash = %x, want %s", got, tt.want)
			}
		})
	}
}

func TestCommitEventTxCanceled(t *testing.T) {
	testDB, err := sql.Open("stub", "")
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	tx, err := testDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	err = commitEventTx(ctx, tx)
	var unknown *db.EventCommitUnknownError
	if err == nil || errors.As(err, &unknown) {
		t.Fatalf("commit error = %T %[1]v, want known cancellation", err)
	}
	if eventTxInterrupted(ctx, errors.New("driver failure")) {
		t.Fatal("unrelated driver error classified as cancellation")
	}
}
