//go:build pgonline

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"decred.org/dcrdex/server/db"
)

func resetEventLog(t *testing.T) {
	t.Helper()
	if _, err := archie.db.Exec(fmt.Sprintf("TRUNCATE %s;", archie.tables.eventLog)); err != nil {
		t.Fatalf("truncate event log: %v", err)
	}
}

func assertEventLogFrontier(t *testing.T, ctx context.Context, wantSeq uint64, wantTip []byte) {
	t.Helper()
	frontier, err := archie.EventLogFrontier(ctx)
	if err != nil {
		t.Fatalf("EventLogFrontier error: %v", err)
	}
	if frontier.Seq != wantSeq || !bytes.Equal(frontier.TipHash, wantTip) {
		t.Fatalf("frontier = (%d, %x), want (%d, %x)",
			frontier.Seq, frontier.TipHash, wantSeq, wantTip)
	}
}

func assertEventLogEntry(t *testing.T, got, want *db.EventLogEntry) {
	t.Helper()
	if got == nil || got.Seq != want.Seq || got.Kind != want.Kind ||
		!bytes.Equal(got.Event, want.Event) || !bytes.Equal(got.TxData, want.TxData) ||
		!bytes.Equal(got.TipHash, want.TipHash) {
		t.Fatalf("entry = %+v, want %+v", got, want)
	}
}

func appendEventLogEntry(ctx context.Context, meta *db.EventLogMeta, kind string, txData []byte) (*db.EventLogEntry, error) {
	appendReq, err := newEventLogAppend(meta, kind, txData)
	if err != nil {
		return nil, err
	}
	tx, err := archie.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	entry, err := archie.appendEventLog(ctx, tx, appendReq)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entry, nil
}

func TestEventLogAppendAndRead(t *testing.T) {
	resetEventLog(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	assertEventLogFrontier(t, ctx, 0, nil)

	entries, err := archie.EventLogEntriesAfter(ctx, 0, 10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read empty log = (%+v, %v), want no entries and no error", entries, err)
	}

	wantEntries := []*db.EventLogEntry{
		{Seq: 1, Kind: "test_event_a", Event: []byte("one"), TxData: []byte(`{"n":1}`)},
		{Seq: 2, Kind: "test_event_b", Event: []byte("two"), TxData: []byte(`{"n":2}`)},
		{Seq: 3, Kind: "empty_payloads"},
	}
	var prevTip []byte
	for _, want := range wantEntries {
		want.TipHash = eventLogHash(prevTip, want.Seq, want.Kind, want.Event, want.TxData)
		entry, err := appendEventLogEntry(ctx, &db.EventLogMeta{Event: want.Event}, want.Kind, want.TxData)
		if err != nil {
			t.Fatalf("append seq %d: %v", want.Seq, err)
		}
		assertEventLogEntry(t, entry, want)
		prevTip = want.TipHash
	}
	assertEventLogFrontier(t, ctx, 3, prevTip)

	for _, tt := range []struct {
		name    string
		after   uint64
		limit   int
		want    []*db.EventLogEntry
		wantErr bool
	}{
		{name: "all entries", limit: 10, want: wantEntries},
		{name: "limit first page", limit: 1, want: wantEntries[:1]},
		{name: "limit next page", after: 1, limit: 1, want: wantEntries[1:2]},
		{name: "after sequence", after: 1, limit: 10, want: wantEntries[1:]},
		{name: "at frontier", after: 3, limit: 10},
		{name: "past frontier", after: 4, limit: 10},
		{name: "zero limit", limit: 0, wantErr: true},
		{name: "negative limit", limit: -1, wantErr: true},
		{name: "sequence overflow", after: uint64(math.MaxInt64) + 1, limit: 10, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := archie.EventLogEntriesAfter(ctx, tt.after, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EventLogEntriesAfter error = %v, want error %v", err, tt.wantErr)
			}
			if len(entries) != len(tt.want) {
				t.Fatalf("read %d entries, want %d", len(entries), len(tt.want))
			}
			for i, want := range tt.want {
				assertEventLogEntry(t, entries[i], want)
			}
		})
	}
}

func TestEventLogConcurrentAppend(t *testing.T) {
	resetEventLog(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const n = 8
	type result struct {
		entry *db.EventLogEntry
		err   error
	}
	results := make(chan result, n)
	start := make(chan struct{})
	for i := range n {
		go func(i int) {
			<-start
			entry, err := appendEventLogEntry(ctx, &db.EventLogMeta{
				Event: []byte(fmt.Sprintf("event-%d", i)),
			}, "test_event", nil)
			results <- result{entry, err}
		}(i)
	}
	close(start)

	var appended []*db.EventLogEntry
	for range n {
		result := <-results
		if result.err != nil {
			t.Errorf("concurrent append: %v", result.err)
			continue
		}
		appended = append(appended, result.entry)
	}
	if t.Failed() {
		return
	}
	sort.Slice(appended, func(i, j int) bool { return appended[i].Seq < appended[j].Seq })

	stored, err := archie.EventLogEntriesAfter(ctx, 0, n+1)
	if err != nil || len(stored) != n {
		t.Fatalf("read concurrent appends = (%+v, %v), want %d entries", stored, err, n)
	}
	var prevTip []byte
	for i, entry := range appended {
		wantSeq := uint64(i + 1)
		if entry.Seq != wantSeq {
			t.Fatalf("entry %d has seq %d, want %d", i, entry.Seq, wantSeq)
		}
		wantTip := eventLogHash(prevTip, wantSeq, entry.Kind, entry.Event, entry.TxData)
		if !bytes.Equal(entry.TipHash, wantTip) {
			t.Fatalf("seq %d tip = %x, want %x", wantSeq, entry.TipHash, wantTip)
		}
		assertEventLogEntry(t, stored[i], entry)
		prevTip = entry.TipHash
	}
	assertEventLogFrontier(t, ctx, n, prevTip)
}

func TestApplyEventTxRejectsInvalidMetadata(t *testing.T) {
	for _, tt := range []struct {
		name string
		meta *db.EventLogMeta
	}{
		{"missing metadata", nil},
		{"sequence overflow", &db.EventLogMeta{Seq: uint64(math.MaxInt64) + 1}},
		{"expected hash without sequence", &db.EventLogMeta{ExpectedTipHash: []byte{}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			applied := false
			entry, err := archie.applyEventTx(context.Background(), tt.meta, "test_event", nil, func(*sql.Tx) error {
				applied = true
				return nil
			})
			if err == nil || entry != nil {
				t.Fatalf("applyEventTx = (%+v, %v), want no entry and an error", entry, err)
			}
			if applied {
				t.Fatal("invalid metadata reached the database update")
			}
		})
	}
}

func TestApplyEventTx(t *testing.T) {
	firstTip := eventLogHash(nil, 1, "seed_event", []byte("one"), nil)
	event, txData := []byte("two"), []byte("tx-two")
	wantTip := eventLogHash(firstTip, 2, "test_event", event, txData)
	applyErr := errors.New("database update rejected")

	for _, tt := range []struct {
		name           string
		seq            uint64
		expectedTip    []byte
		applyErr       error
		wantErr        bool
		wantDivergence bool
	}{
		{name: "allocate sequence"},
		{name: "verify sequence and hash", seq: 2, expectedTip: wantTip},
		{name: "callback failure", applyErr: applyErr, wantErr: true},
		{name: "skipped sequence", seq: 3, wantErr: true},
		{name: "repeated sequence", seq: 1, wantErr: true},
		{name: "wrong hash", seq: 2, expectedTip: bytes.Repeat([]byte{0x01}, db.EventLogTipHashSize), wantErr: true, wantDivergence: true},
		{name: "empty hash", seq: 2, expectedTip: []byte{}, wantErr: true, wantDivergence: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := cleanTables(archie.db); err != nil {
					t.Error(err)
				}
			})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first, err := appendEventLogEntry(ctx, &db.EventLogMeta{Event: []byte("one")}, "seed_event", nil)
			if err != nil {
				t.Fatalf("seed event: %v", err)
			}
			var pointID int64
			applied := false
			entry, err := archie.applyEventTx(ctx, &db.EventLogMeta{
				Seq: tt.seq, Event: event, ExpectedTipHash: tt.expectedTip,
			}, "test_event", txData, func(tx *sql.Tx) error {
				if err := tx.QueryRowContext(ctx, fmt.Sprintf(
					"INSERT INTO %s (account, link, class, outcome) VALUES ($1, $2, 1, 1) RETURNING id",
					archie.tables.points), []byte{0x01}, []byte{0x02}).Scan(&pointID); err != nil {
					return err
				}
				applied = true
				return tt.applyErr
			})
			if !applied {
				t.Fatalf("database insert did not succeed: %v", err)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyEventTx error = %v, want error %v", err, tt.wantErr)
			}
			if tt.applyErr != nil && !errors.Is(err, tt.applyErr) {
				t.Fatalf("applyEventTx error = %v, want %v", err, tt.applyErr)
			}
			var divergence *db.EventLogDivergenceError
			if errors.As(err, &divergence) != tt.wantDivergence {
				t.Fatalf("applyEventTx error = %v, want divergence %v", err, tt.wantDivergence)
			}
			if tt.wantDivergence && (divergence.Seq != 2 ||
				!bytes.Equal(divergence.ExpectedTipHash, tt.expectedTip) ||
				!bytes.Equal(divergence.ActualTipHash, wantTip)) {
				t.Fatalf("divergence = %+v, want seq 2, expected %x, actual %x", divergence, tt.expectedTip, wantTip)
			}

			// A separate deadline keeps automatic transaction cancellation from
			// making a missing rollback look successful.
			checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelCheck()
			var pointRows int
			if err := archie.db.QueryRowContext(checkCtx, "SELECT count(*) FROM "+archie.tables.points).Scan(&pointRows); err != nil {
				t.Fatal(err)
			}
			if tt.wantErr {
				if entry != nil || pointRows != 0 {
					t.Fatalf("failed transaction returned entry %+v and left %d point rows", entry, pointRows)
				}
				assertEventLogFrontier(t, checkCtx, 1, first.TipHash)

				// Reusing the ID proves rollback released the transaction's lock.
				if _, err := archie.db.ExecContext(checkCtx, fmt.Sprintf(
					"INSERT INTO %s (id, account, link, class, outcome) VALUES ($1, $2, $3, 1, 1)",
					archie.tables.points), pointID, []byte{0x01}, []byte{0x02}); err != nil {
					t.Fatalf("reuse point ID after rollback: %v", err)
				}
				return
			}

			if pointRows != 1 {
				t.Fatalf("committed point rows = %d, want 1", pointRows)
			}
			want := &db.EventLogEntry{Seq: 2, Kind: "test_event", Event: event, TxData: txData, TipHash: wantTip}
			assertEventLogEntry(t, entry, want)
			stored, err := archie.EventLogEntriesAfter(checkCtx, 1, 10)
			if err != nil || len(stored) != 1 {
				t.Fatalf("read committed event = (%+v, %v), want one entry", stored, err)
			}
			assertEventLogEntry(t, stored[0], want)
			assertEventLogFrontier(t, checkCtx, 2, wantTip)
		})
	}
}
