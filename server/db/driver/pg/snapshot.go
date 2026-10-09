// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"encoding/gob"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/lib/pq"
)

func init() {
	gob.Register([]byte(nil))
	gob.Register(time.Time{})
}

// snapshotTable identifies a table to export and an optional row filter.
type snapshotTable struct {
	schema string
	table  string
	where  string
}

func (table snapshotTable) qualified() string {
	return qualifySchemaTable(table.schema, table.table)
}

func (table snapshotTable) key() string {
	return table.schema + "." + table.table
}

// snapshotHistoryDays sets how many days of order and match history snapshots
// include so reconnecting clients can check their order and match statuses.
const snapshotHistoryDays = 30

// snapshotTables returns the tables that should be included in the snapshot.
// It returns an error if a discovered table has no classification.
func (a *Archiver) snapshotTables(ctx context.Context, tx *sql.Tx) ([]snapshotTable, error) {
	schemas := append([]string{publicSchema}, slices.Sorted(maps.Keys(a.markets))...)
	var tables []snapshotTable
	for _, schema := range schemas {
		schemaTables, err := classifiedTables(ctx, tx, schema)
		if err != nil {
			return nil, err
		}
		for _, classified := range schemaTables {
			if classified.class != classSnapshot {
				continue
			}
			tables = append(tables, snapshotTable{schema, classified.table, a.snapshotFilter(schema, classified.table)})
		}
	}
	return tables, nil
}

// snapshotFilter is the dump WHERE clause, or "" for a full dump.
func (a *Archiver) snapshotFilter(schema, table string) string {
	switch table {
	case marketLifecycleTableName:
		return lifecycleForConfiguredMarkets(a.markets)
	case ordersArchivedTableName:
		return archivedOrdersInSnapshot(schema)
	case cancelsArchivedTableName:
		return serverTimeInSnapshotHistory()
	case matchesTableName:
		return activeOrRecentMatches()
	case epochReportsTableName:
		return recentEpochReports(schema)
	}
	if strings.HasPrefix(table, candlesTableName+"_") {
		return lastCacheSizeCandles(schema, table)
	}
	return ""
}

// lifecycleForConfiguredMarkets filters market_lifecycle by market name.
func lifecycleForConfiguredMarkets(markets map[string]*dex.MarketInfo) string {
	if len(markets) == 0 {
		return "FALSE"
	}
	names := make([]string, 0, len(markets))
	for _, mkt := range markets {
		names = append(names, mkt.Name)
	}
	slices.Sort(names)
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = pq.QuoteLiteral(name)
	}
	return "market IN (" + strings.Join(quoted, ", ") + ")"
}

func qualifySchemaTable(schema, table string) string {
	return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(table)
}

func serverTimeInSnapshotHistory() string {
	return fmt.Sprintf("server_time > now() - interval '%d days'", snapshotHistoryDays)
}

// matchEpochHorizonMs is now − snapshotHistoryDays in ms (epochIdx*epochDur).
func matchEpochHorizonMs() string {
	return fmt.Sprintf("(extract(epoch from now())*1000)::int8 - %d",
		int64(snapshotHistoryDays)*24*int64(time.Hour/time.Millisecond))
}

// archivedOrdersInSnapshot: snapshotted-match legs, cancel targets, or history window.
func archivedOrdersInSnapshot(schema string) string {
	return fmt.Sprintf("(%s) OR (%s) OR %s",
		oidsOfSnapshottedMatchOrders(schema),
		oidsOfArchivedCancelTargets(schema),
		serverTimeInSnapshotHistory())
}

// oidsOfSnapshottedMatchOrders pairs archived orders with the match dump set.
func oidsOfSnapshottedMatchOrders(schema string) string {
	matchesTable := qualifySchemaTable(schema, matchesTableName)
	matchSet := activeOrRecentMatches()
	return fmt.Sprintf(
		"oid IN (SELECT makerOrder FROM %s WHERE %s UNION SELECT takerOrder FROM %s WHERE %s)",
		matchesTable, matchSet, matchesTable, matchSet)
}

// oidsOfArchivedCancelTargets pairs targets with the cancels_archived window.
func oidsOfArchivedCancelTargets(schema string) string {
	return fmt.Sprintf(
		"oid IN (SELECT target_order FROM %s WHERE %s)",
		qualifySchemaTable(schema, cancelsArchivedTableName),
		serverTimeInSnapshotHistory())
}

func activeOrRecentMatches() string {
	return fmt.Sprintf("active OR epochIdx*epochDur > %s", matchEpochHorizonMs())
}

func recentEpochReports(schema string) string {
	return fmt.Sprintf("epoch_end > (SELECT coalesce(max(epoch_end),0) FROM %s) - %d",
		qualifySchemaTable(schema, epochReportsTableName), largestCandleBinMs())
}

// lastCacheSizeCandles keeps the newest candles.CacheSize rows.
func lastCacheSizeCandles(schema, table string) string {
	return fmt.Sprintf(
		"end_stamp >= (SELECT coalesce(min(end_stamp),0) FROM (SELECT end_stamp FROM %s ORDER BY end_stamp DESC LIMIT %d) latest)",
		qualifySchemaTable(schema, table), candles.CacheSize)
}

var largestCandleBinMs = sync.OnceValue(func() int64 {
	var largest time.Duration
	for _, bin := range candles.BinSizes {
		if dur, err := time.ParseDuration(bin); err == nil && dur > largest {
			largest = dur
		}
	}
	return largest.Milliseconds()
})

type snapshotTableDump struct {
	Schema  string
	Table   string
	Columns []string
	Rows    [][]any
}

func (dump snapshotTableDump) qualified() string {
	return qualifySchemaTable(dump.Schema, dump.Table)
}

func (dump snapshotTableDump) key() string {
	return dump.Schema + "." + dump.Table
}

// pgSnapshot is the gob payload exchanged by WriteSnapshot and LoadSnapshot.
type pgSnapshot struct {
	FrontierSeq     uint64
	FrontierTipHash []byte
	Tables          []snapshotTableDump
}

func (snapshot *pgSnapshot) frontier() *db.EventLogPosition {
	return &db.EventLogPosition{Seq: snapshot.FrontierSeq, TipHash: snapshot.FrontierTipHash}
}

// WriteSnapshot reads the selected tables and event log frontier from one
// consistent database snapshot. It encodes them to w and returns the frontier.
func (a *Archiver) WriteSnapshot(ctx context.Context, w io.Writer) (*db.EventLogPosition, error) {
	snapshot, err := a.buildSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err := gob.NewEncoder(w).Encode(snapshot); err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	return snapshot.frontier(), nil
}

func (a *Archiver) buildSnapshot(ctx context.Context) (*pgSnapshot, error) {
	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	frontier, err := a.eventLogFrontierTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("read frontier: %w", err)
	}

	tables, err := a.snapshotTables(ctx, tx)
	if err != nil {
		return nil, err
	}

	snapshot := &pgSnapshot{FrontierSeq: frontier.Seq, FrontierTipHash: frontier.TipHash}
	for _, table := range tables {
		dump, err := dumpSnapshotTable(ctx, tx, table)
		if err != nil {
			return nil, fmt.Errorf("dump %s.%s: %w", table.schema, table.table, err)
		}
		snapshot.Tables = append(snapshot.Tables, *dump)
	}

	return snapshot, nil
}

func dumpSnapshotTable(ctx context.Context, tx *sql.Tx, table snapshotTable) (*snapshotTableDump, error) {
	query := "SELECT * FROM " + table.qualified()
	if table.where != "" {
		query += " WHERE " + table.where
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	dump := &snapshotTableDump{Schema: table.schema, Table: table.table, Columns: columns}
	for rows.Next() {
		row, err := scanSnapshotRow(rows, len(columns))
		if err != nil {
			return nil, err
		}
		dump.Rows = append(dump.Rows, row)
	}
	return dump, rows.Err()
}

func scanSnapshotRow(rows *sql.Rows, columnCount int) ([]any, error) {
	values := make([]any, columnCount)
	destinations := make([]any, columnCount)
	for i := range values {
		destinations[i] = &values[i]
	}
	if err := rows.Scan(destinations...); err != nil {
		return nil, err
	}
	return values, nil
}

// LoadSnapshot restores a snapshot produced by WriteSnapshot and returns its
// event log frontier. It rejects databases with existing event state and
// snapshots whose tables do not match the receiving database's snapshot tables.
// The caller must prevent concurrent database writes during the restore.
func (a *Archiver) LoadSnapshot(ctx context.Context, r io.Reader) (*db.EventLogPosition, error) {
	snapshot, err := decodePGSnapshot(r)
	if err != nil {
		return nil, err
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	expected, err := a.snapshotTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := validateSnapshotTableSet(snapshot.Tables, expected); err != nil {
		return nil, err
	}

	empty, err := hasNoEventSourcedState(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("event-sourced state check: %w", err)
	}
	if !empty {
		return nil, fmt.Errorf("refusing to load a snapshot into a database with existing state")
	}

	if err := loadSnapshotTables(ctx, tx, snapshot.Tables); err != nil {
		return nil, err
	}
	frontier, err := a.seedEventLogAnchor(ctx, tx, snapshot.FrontierSeq, snapshot.FrontierTipHash)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit snapshot load: %w", err)
	}
	return frontier, nil
}

func decodePGSnapshot(r io.Reader) (*pgSnapshot, error) {
	var snapshot pgSnapshot
	if err := gob.NewDecoder(r).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if snapshot.FrontierSeq > math.MaxInt64 {
		return nil, fmt.Errorf("snapshot frontier sequence %d overflows int64", snapshot.FrontierSeq)
	}
	if snapshot.FrontierSeq == 0 {
		if len(snapshot.FrontierTipHash) != 0 {
			return nil, fmt.Errorf("snapshot frontier at sequence zero must have no tip hash")
		}
	} else if len(snapshot.FrontierTipHash) != db.EventLogTipHashSize {
		return nil, fmt.Errorf("snapshot frontier tip hash length %d, want %d", len(snapshot.FrontierTipHash), db.EventLogTipHashSize)
	}
	return &snapshot, nil
}

// validateSnapshotTableSet checks for an exact table match, allowing any order
// but rejecting missing, extra, or duplicate tables.
func validateSnapshotTableSet(got []snapshotTableDump, expected []snapshotTable) error {
	want := make([]string, len(expected))
	for i, table := range expected {
		want[i] = table.key()
	}
	have := make([]string, len(got))
	for i, dump := range got {
		have[i] = dump.key()
	}
	slices.Sort(want)
	slices.Sort(have)
	if !slices.Equal(want, have) {
		return fmt.Errorf("snapshot table set mismatch: got %v, want %v", have, want)
	}
	return nil
}

func loadSnapshotTables(ctx context.Context, tx *sql.Tx, tables []snapshotTableDump) error {
	for _, dump := range tables {
		if err := loadSnapshotTable(ctx, tx, dump); err != nil {
			return fmt.Errorf("load %s.%s: %w", dump.Schema, dump.Table, err)
		}
	}
	return nil
}

// seedEventLogAnchor clears the log and inserts an anchor for a nonzero frontier.
func (a *Archiver) seedEventLogAnchor(ctx context.Context, tx *sql.Tx, seq uint64, tipHash []byte) (*db.EventLogPosition, error) {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s", a.tables.eventLog)); err != nil {
		return nil, fmt.Errorf("clear event log: %w", err)
	}
	frontier := &db.EventLogPosition{Seq: seq, TipHash: tipHash}
	if seq > 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(internal.InsertEventLog, a.tables.eventLog),
			int64(seq), db.SnapshotAnchorKind, []byte{}, []byte{}, tipHash); err != nil {
			return nil, fmt.Errorf("seed frontier anchor: %w", err)
		}
	}
	return frontier, nil
}

func loadSnapshotTable(ctx context.Context, tx *sql.Tx, dump snapshotTableDump) error {
	if _, err := tx.ExecContext(ctx, "TRUNCATE TABLE "+dump.qualified()); err != nil {
		return err
	}
	if len(dump.Rows) == 0 {
		return nil
	}
	if err := copyInRows(ctx, tx, dump.Schema, dump.Table, dump.Columns, dump.Rows); err != nil {
		return err
	}
	return syncSerialSequences(ctx, tx, dump.Schema, dump.Table)
}

func copyInRows(ctx context.Context, tx *sql.Tx, schema, table string, columns []string, rows [][]any) error {
	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema(schema, table, columns...))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row...); err != nil {
			return err
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return err
	}
	return stmt.Close()
}

// syncSerialSequences advances sequences past the IDs restored by COPY, which
// does not advance them itself. This prevents later inserts from reusing an ID.
func syncSerialSequences(ctx context.Context, tx *sql.Tx, schema, table string) error {
	tableName := qualifySchemaTable(schema, table)
	columns, err := listSerialColumns(ctx, tx, tableName)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if err := advanceSerialSequence(ctx, tx, tableName, column); err != nil {
			return err
		}
	}
	return nil
}

// serialColumn identifies a column and the sequence that generates its values.
type serialColumn struct {
	name     string
	sequence string
}

// listSerialColumns finds columns with associated sequences in the qualified
// tableName. It returns nil if the table has none.
func listSerialColumns(ctx context.Context, tx *sql.Tx, tableName string) ([]serialColumn, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT a.attname, pg_get_serial_sequence($1, a.attname)
		FROM pg_attribute a
		WHERE 
			a.attrelid = $1::regclass AND 
			a.attnum > 0 AND 
			NOT a.attisdropped AND
			pg_get_serial_sequence($1, a.attname) IS NOT NULL`, tableName)
	if err != nil {
		return nil, fmt.Errorf("list serial sequences: %w", err)
	}
	defer rows.Close()

	var columns []serialColumn
	for rows.Next() {
		var column serialColumn
		if err := rows.Scan(&column.name, &column.sequence); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

// advanceSerialSequence sets the next sequence value to one past the highest
// stored ID, or one if the table is empty.
func advanceSerialSequence(ctx context.Context, tx *sql.Tx, tableName string, column serialColumn) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		"SELECT setval($1, COALESCE((SELECT MAX(%s) FROM %s), 0) + 1, false)",
		pq.QuoteIdentifier(column.name), tableName), column.sequence); err != nil {
		return fmt.Errorf("sync sequence %s: %w", column.sequence, err)
	}
	return nil
}
