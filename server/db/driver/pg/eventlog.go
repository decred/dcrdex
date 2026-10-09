// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"math"

	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

func writeHashField(h hash.Hash, data []byte) {
	var lengthBytes [8]byte
	binary.BigEndian.PutUint64(lengthBytes[:], uint64(len(data)))
	h.Write(lengthBytes[:])
	h.Write(data)
}

type eventLogAppendRequest struct {
	entry           db.EventLogEntry
	expectedTipHash []byte
}

func newEventLogAppend(meta *db.EventLogMeta, kind string, txData []byte) (eventLogAppendRequest, error) {
	if meta == nil {
		return eventLogAppendRequest{}, fmt.Errorf("nil event log metadata")
	}
	if meta.Seq > math.MaxInt64 {
		return eventLogAppendRequest{}, fmt.Errorf("event log seq %d overflows int64", meta.Seq)
	}
	if meta.ExpectedTipHash != nil && meta.Seq == 0 {
		return eventLogAppendRequest{}, fmt.Errorf("expected event log tip hash requires an explicit sequence")
	}

	// Store empty byte strings rather than NULL for missing payloads.
	event := meta.Event
	if event == nil {
		event = []byte{}
	}
	if txData == nil {
		txData = []byte{}
	}

	return eventLogAppendRequest{
		entry: db.EventLogEntry{
			Seq:    meta.Seq,
			Kind:   kind,
			Event:  event,
			TxData: txData,
		},
		expectedTipHash: meta.ExpectedTipHash,
	}, nil
}

// eventLogHash hashes the previous tip, sequence, kind, event, and transaction
// data. Each variable length field is preceded by its length.
func eventLogHash(prev []byte, seq uint64, kind string, event, txData []byte) []byte {
	h := sha256.New()
	writeHashField(h, prev)
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], seq)
	h.Write(seqBytes[:])
	writeHashField(h, []byte(kind))
	writeHashField(h, event)
	writeHashField(h, txData)
	return h.Sum(nil)
}

func scanEventLogFrontier(row *sql.Row) (*db.EventLogPosition, error) {
	var seq int64
	var tipHash []byte
	err := row.Scan(&seq, &tipHash)
	if err == sql.ErrNoRows {
		return &db.EventLogPosition{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &db.EventLogPosition{
		Seq:     uint64(seq),
		TipHash: tipHash,
	}, nil
}

func (a *Archiver) eventLogFrontierTx(ctx context.Context, tx *sql.Tx) (*db.EventLogPosition, error) {
	stmt := fmt.Sprintf(internal.SelectEventLogFrontier, a.tables.eventLog)
	return scanEventLogFrontier(tx.QueryRowContext(ctx, stmt))
}

// appendEventLog locks the event log table and appends the next entry in tx.
// It computes the entry's sequence and tip hash, and makes sure the expected
// seq and tip hashes match (if they were supplied).
func (a *Archiver) appendEventLog(ctx context.Context, tx *sql.Tx, appendReq eventLogAppendRequest) (*db.EventLogEntry, error) {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(internal.LockEventLog, a.tables.eventLog)); err != nil {
		return nil, err
	}

	prev, err := a.eventLogFrontierTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	if prev.Seq != 0 && len(prev.TipHash) != db.EventLogTipHashSize {
		return nil, fmt.Errorf("event log previous tip hash length %d, want %d", len(prev.TipHash), db.EventLogTipHashSize)
	}
	if prev.Seq == math.MaxInt64 {
		return nil, fmt.Errorf("event log seq %d overflows int64", prev.Seq+1)
	}

	entry := &appendReq.entry
	nextSeq := prev.Seq + 1
	if entry.Seq != 0 && entry.Seq != nextSeq {
		return nil, fmt.Errorf("event log seq mismatch: got %d after %d", entry.Seq, prev.Seq)
	}
	entry.Seq = nextSeq
	entry.TipHash = eventLogHash(prev.TipHash, entry.Seq, entry.Kind, entry.Event, entry.TxData)

	if appendReq.expectedTipHash != nil && !bytes.Equal(appendReq.expectedTipHash, entry.TipHash) {
		return nil, &db.EventLogDivergenceError{
			Seq:             entry.Seq,
			ExpectedTipHash: bytes.Clone(appendReq.expectedTipHash),
			ActualTipHash:   entry.TipHash,
			Err:             fmt.Errorf("event log tip hash mismatch"),
		}
	}

	stmt := fmt.Sprintf(internal.InsertEventLog, a.tables.eventLog)
	if _, err := tx.ExecContext(ctx, stmt, int64(entry.Seq), entry.Kind, entry.Event, entry.TxData, entry.TipHash); err != nil {
		return nil, err
	}

	return entry, nil
}

func eventTxInterrupted(ctx context.Context, err error) bool {
	ctxErr := ctx.Err()
	return ctxErr != nil && (errors.Is(err, ctxErr) || errors.Is(err, sql.ErrTxDone))
}

// commitEventTx wraps commit errors whose outcome is unknown. Cancellation
// before Commit starts is known not to have landed.
func commitEventTx(ctx context.Context, tx *sql.Tx) error {
	err := tx.Commit()
	if err == nil {
		return nil
	}
	if eventTxInterrupted(ctx, err) {
		return fmt.Errorf("apply interrupted before commit: %w", err)
	}
	return &db.EventCommitUnknownError{Err: err}
}

// applyEventTx runs apply and appends its event log entry in a single transaction.
// It commits both changes together and returns the stored entry. If apply or the
// event log append fails, it rolls back the transaction.
func (a *Archiver) applyEventTx(ctx context.Context, meta *db.EventLogMeta, kind string, txData []byte, apply func(*sql.Tx) error) (logEntry *db.EventLogEntry, err error) {
	appendReq, err := newEventLogAppend(meta, kind, txData)
	if err != nil {
		return nil, err
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		if !eventTxInterrupted(ctx, err) {
			a.fatalBackendErr(err)
		}
		return nil, err
	}
	defer func() {
		if err == nil || errors.Is(err, sql.ErrTxDone) {
			return
		}
		tx.Rollback()
	}()

	if err = apply(tx); err != nil {
		return nil, err
	}

	logEntry, err = a.appendEventLog(ctx, tx, appendReq)
	if err != nil {
		return nil, err
	}

	if err = commitEventTx(ctx, tx); err != nil {
		if !eventTxInterrupted(ctx, err) {
			a.fatalBackendErr(err)
		}
		return nil, err
	}
	return logEntry, nil
}

// EventLogFrontier queries the database for the current event log frontier.
func (a *Archiver) EventLogFrontier(ctx context.Context) (*db.EventLogPosition, error) {
	stmt := fmt.Sprintf(internal.SelectEventLogFrontier, a.tables.eventLog)
	return scanEventLogFrontier(a.db.QueryRowContext(ctx, stmt))
}

// EventLogEntriesAfter queries the database for event log entries after a
// given sequence number.
func (a *Archiver) EventLogEntriesAfter(ctx context.Context, after uint64, limit int) ([]*db.EventLogEntry, error) {
	if after > math.MaxInt64 {
		return nil, fmt.Errorf("event log seq %d overflows int64", after)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("event log limit must be positive")
	}
	stmt := fmt.Sprintf(internal.SelectEventLogSince, a.tables.eventLog)
	rows, err := a.db.QueryContext(ctx, stmt, int64(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*db.EventLogEntry
	for rows.Next() {
		var seq int64
		entry := new(db.EventLogEntry)
		if err := rows.Scan(&seq, &entry.Kind, &entry.Event, &entry.TxData, &entry.TipHash); err != nil {
			return nil, err
		}
		entry.Seq = uint64(seq)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
