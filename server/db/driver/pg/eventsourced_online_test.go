//go:build pgonline

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
)

// TestWipeEventSourcedState checks that wiping clears current and historical
// state, preserves market and metadata rows, and restarts the event sequence.
func TestWipeEventSourcedState(t *testing.T) {
	ctx := context.Background()
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Seed a public table, an archived order, and the event log.
	if _, err := archie.db.Exec(fmt.Sprintf(
		"INSERT INTO %s (account, link, class, outcome) VALUES ($1, $2, 1, 1)",
		archie.tables.points), []byte{0xaa}, []byte{0xbb}); err != nil {
		t.Fatalf("insert point: %v", err)
	}
	archivedOrder := newLimitOrder(true, 4_900_000, 1, order.StandingTiF, 10)
	if err := storeOrderForTest(archie, archivedOrder, 12345, int64(EpochDuration), order.OrderStatusExecuted); err != nil {
		t.Fatalf("StoreOrder: %v", err)
	}
	if _, err := archie.applyEventTx(ctx, &db.EventLogMeta{Event: []byte("e1")}, "wipe_test",
		[]byte("e1"), func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("applyEventTx: %v", err)
	}

	countRows := func(table string) int {
		t.Helper()
		var rowCount int
		if err := archie.db.QueryRow("SELECT count(*) FROM " + table).Scan(&rowCount); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return rowCount
	}
	marketsBefore, metaBefore := countRows("markets"), countRows("meta")
	if marketsBefore == 0 || metaBefore == 0 {
		t.Fatalf("expected config rows: markets=%d meta=%d", marketsBefore, metaBefore)
	}

	if err := archie.WipeEventSourcedState(ctx); err != nil {
		t.Fatalf("WipeEventSourcedState: %v", err)
	}

	empty, err := archie.HasNoEventSourcedState(ctx)
	if err != nil {
		t.Fatalf("HasNoEventSourcedState: %v", err)
	}
	if !empty {
		t.Fatal("event-sourced state remains after wipe")
	}
	if rowCount := countRows("markets"); rowCount != marketsBefore {
		t.Fatalf("markets rows = %d after wipe, want %d", rowCount, marketsBefore)
	}
	if rowCount := countRows("meta"); rowCount != metaBefore {
		t.Fatalf("meta rows = %d after wipe, want %d", rowCount, metaBefore)
	}

	entry, err := archie.applyEventTx(ctx, &db.EventLogMeta{Event: []byte("fresh")}, "wipe_test",
		[]byte("fresh"), func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatalf("applyEventTx after wipe: %v", err)
	}
	if entry.Seq != 1 {
		t.Fatalf("first post-wipe event seq = %d, want 1", entry.Seq)
	}
}

// TestHasNoEventSourcedState checks that a prepared database has no event state
// and that a public, market, or event log row makes it nonempty.
func TestHasNoEventSourcedState(t *testing.T) {
	ctx := context.Background()
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	assertEmpty := func(want bool, stage string) {
		t.Helper()
		empty, err := archie.HasNoEventSourcedState(ctx)
		if err != nil {
			t.Fatalf("%s: HasNoEventSourcedState: %v", stage, err)
		}
		if empty != want {
			t.Fatalf("%s: HasNoEventSourcedState = %v, want %v", stage, empty, want)
		}
	}

	assertEmpty(true, "prepared but unused")

	// Seed a public table.
	points := archie.tables.points
	if _, err := archie.db.Exec(fmt.Sprintf(
		"INSERT INTO %s (account, link, class, outcome) VALUES ($1, $2, 1, 1)",
		points), []byte{0x01}, []byte{0x02}); err != nil {
		t.Fatalf("insert point: %v", err)
	}
	assertEmpty(false, "with a points row")
	if _, err := archie.db.Exec("TRUNCATE TABLE " + points + " RESTART IDENTITY"); err != nil {
		t.Fatalf("clear points: %v", err)
	}
	assertEmpty(true, "points cleared")

	// Seed a market table.
	bookedOrder := newLimitOrder(false, 4_500_000, 1, order.StandingTiF, 0)
	if err := storeOrderForTest(archie, bookedOrder, 12345, int64(EpochDuration), order.OrderStatusBooked); err != nil {
		t.Fatalf("StoreOrder: %v", err)
	}
	assertEmpty(false, "with a booked order")
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
	assertEmpty(true, "order cleared")

	// Seed the event log while all projection tables are empty.
	if _, err := archie.applyEventTx(ctx, &db.EventLogMeta{Event: []byte{0xbe}}, "es_test",
		[]byte{0xbe}, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatalf("applyEventTx: %v", err)
	}
	assertEmpty(false, "with an event-log row")

	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables (cleanup): %v", err)
	}
}
