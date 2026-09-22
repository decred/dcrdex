//go:build pgonline

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
)

func mustCleanTables(t *testing.T) {
	t.Helper()
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
}

// prepareSnapshotTest resets the database and cleans it again after the test.
func prepareSnapshotTest(t *testing.T) {
	t.Helper()
	mustCleanTables(t)
	t.Cleanup(func() {
		if err := cleanTables(archie.db); err != nil {
			t.Errorf("clean snapshot test tables: %v", err)
		}
	})
}

func mustStoreOrder(t *testing.T, ord order.Order, status order.OrderStatus) {
	t.Helper()
	epochDuration := int64(EpochDuration)
	epochIndex := ord.Time() / epochDuration
	if err := archie.StoreOrder(ord, epochIndex, epochDuration, status); err != nil {
		t.Fatalf("StoreOrder(%v): %v", ord.ID(), err)
	}
}

func checkStoredOrder(t *testing.T, ord order.Order, want order.OrderStatus) {
	t.Helper()
	stored, status, err := archie.Order(ord.ID(), ord.Base(), ord.Quote())
	if err != nil {
		t.Fatalf("Order(%v): %v", ord.ID(), err)
	}
	if got, want := stored.Serialize(), ord.Serialize(); !bytes.Equal(got, want) {
		t.Fatalf("Order(%v) data = %x, want %x", ord.ID(), got, want)
	}
	if status != want {
		t.Fatalf("Order(%v) status = %v, want %v", ord.ID(), status, want)
	}
	if trade := ord.Trade(); trade != nil && stored.Trade().Filled() != trade.Filled() {
		t.Fatalf("Order(%v) filled = %d, want %d", ord.ID(), stored.Trade().Filled(), trade.Filled())
	}
}

func mustOrderUnknown(t *testing.T, ord order.Order) {
	t.Helper()
	if _, _, err := archie.Order(ord.ID(), ord.Base(), ord.Quote()); !db.IsErrOrderUnknown(err) {
		t.Fatalf("Order(%v): err = %v, want unknown order", ord.ID(), err)
	}
}

// reloadFromSnapshot writes a snapshot, resets the database, and loads the
// snapshot back, checking that its frontier is preserved.
func reloadFromSnapshot(t *testing.T, ctx context.Context) *db.EventLogPosition {
	t.Helper()
	var buf bytes.Buffer
	writeFrontier, err := archie.WriteSnapshot(ctx, &buf)
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	mustCleanTables(t)
	loadFrontier, err := archie.LoadSnapshot(ctx, &buf)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loadFrontier.Seq != writeFrontier.Seq || !bytes.Equal(loadFrontier.TipHash, writeFrontier.TipHash) {
		t.Fatalf("loaded frontier = %s, want %s", loadFrontier, writeFrontier)
	}
	return loadFrontier
}

func insertSnapshotTestPoint(t *testing.T, ctx context.Context, account, link []byte) int64 {
	t.Helper()
	var id int64
	query := "INSERT INTO " + archie.tables.points +
		" (account, link, class, outcome) VALUES ($1, $2, 1, 1) RETURNING id"
	if err := archie.db.QueryRowContext(ctx, query, account, link).Scan(&id); err != nil {
		t.Fatalf("insert test point: %v", err)
	}
	return id
}

func appendSnapshotTestEvent(t *testing.T, ctx context.Context, payload []byte) *db.EventLogEntry {
	t.Helper()
	entry, err := archie.applyEventTx(ctx, &db.EventLogMeta{Event: payload}, "snap_test",
		payload, func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatalf("applyEventTx: %v", err)
	}
	return entry
}

// TestSnapshotRoundTrip checks restored rows, the event log anchor, and later
// inserts that use the restored frontier and ID sequences.
func TestSnapshotRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prepareSnapshotTest(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	oldTime := now.Add(-time.Duration(snapshotHistoryDays+5) * 24 * time.Hour)

	bookedOrder := newLimitOrder(false, 4_800_000, 2, order.StandingTiF, 0)
	bookedOrder.AddFill(LotSize)
	mustStoreOrder(t, bookedOrder, order.OrderStatusBooked)

	recentArchived := newLimitOrder(true, 4_900_000, 1, order.StandingTiF, 0)
	recentArchived.SetTime(now)
	mustStoreOrder(t, recentArchived, order.OrderStatusExecuted)
	oldArchived := newLimitOrder(true, 5_000_000, 1, order.StandingTiF, 0)
	oldArchived.SetTime(oldTime)
	mustStoreOrder(t, oldArchived, order.OrderStatusExecuted)

	const hourMs = uint64(60 * 60 * 1000)
	wantCandles := []*candles.Candle{
		{StartStamp: 2 * hourMs, EndStamp: 3 * hourMs, MatchVolume: 1, QuoteVolume: 1, HighRate: 5, LowRate: 4, StartRate: 4, EndRate: 5},
		{StartStamp: 3 * hourMs, EndStamp: 4 * hourMs, MatchVolume: 2, QuoteVolume: 2, HighRate: 6, LowRate: 5, StartRate: 5, EndRate: 6},
	}
	if err := archie.InsertCandles(bookedOrder.Base(), bookedOrder.Quote(), hourMs, wantCandles); err != nil {
		t.Fatalf("InsertCandles: %v", err)
	}

	pointID := insertSnapshotTestPoint(t, ctx, []byte{0xaa}, []byte{0xbb})

	appendSnapshotTestEvent(t, ctx, []byte("e1"))
	wantFrontier := appendSnapshotTestEvent(t, ctx, []byte("e2"))

	loadFrontier := reloadFromSnapshot(t, ctx)

	t.Run("orders", func(t *testing.T) {
		checkStoredOrder(t, bookedOrder, order.OrderStatusBooked)
		checkStoredOrder(t, recentArchived, order.OrderStatusExecuted)
		mustOrderUnknown(t, oldArchived)
	})

	t.Run("candles", func(t *testing.T) {
		cache := candles.NewCache(10, hourMs)
		if err := archie.loadCandles(bookedOrder.Base(), bookedOrder.Quote(), cache, 10); err != nil {
			t.Fatalf("load restored candles: %v", err)
		}
		if len(cache.Candles) != len(wantCandles) {
			t.Fatalf("restored %d candles, want %d", len(cache.Candles), len(wantCandles))
		}
		for i, want := range wantCandles {
			if cache.Candles[i] != *want {
				t.Fatalf("candle %d = %+v, want %+v", i, cache.Candles[i], *want)
			}
		}
	})

	t.Run("point IDs", func(t *testing.T) {
		var restoredID int64
		var account, link []byte
		var class, outcome int
		if err := archie.db.QueryRowContext(ctx,
			"SELECT id, account, link, class, outcome FROM "+archie.tables.points).
			Scan(&restoredID, &account, &link, &class, &outcome); err != nil {
			t.Fatalf("read restored point: %v", err)
		}
		if restoredID != pointID || !bytes.Equal(account, []byte{0xaa}) ||
			!bytes.Equal(link, []byte{0xbb}) || class != 1 || outcome != 1 {
			t.Fatalf("restored point = (%d, %x, %x, %d, %d)", restoredID, account, link, class, outcome)
		}

		nextPointID := insertSnapshotTestPoint(t, ctx, []byte{0xcc}, []byte{0xdd})
		if nextPointID <= pointID {
			t.Fatalf("post-snapshot point id = %d, want > %d", nextPointID, pointID)
		}
	})

	t.Run("event log", func(t *testing.T) {
		if loadFrontier.Seq != wantFrontier.Seq || !bytes.Equal(loadFrontier.TipHash, wantFrontier.TipHash) {
			t.Fatalf("loaded frontier = %s, want seq=%d hash=%x",
				loadFrontier, wantFrontier.Seq, wantFrontier.TipHash)
		}

		entries, err := archie.EventLogEntriesAfter(ctx, 0, 10)
		if err != nil || len(entries) != 1 {
			t.Fatalf("event log after load = (%+v, %v), want one anchor", entries, err)
		}
		assertEventLogEntry(t, entries[0], &db.EventLogEntry{
			Seq: wantFrontier.Seq, Kind: db.SnapshotAnchorKind, TipHash: wantFrontier.TipHash,
		})

		next := appendSnapshotTestEvent(t, ctx, []byte("e3"))
		if next.Seq != wantFrontier.Seq+1 {
			t.Fatalf("next event seq = %d, want %d", next.Seq, wantFrontier.Seq+1)
		}

		wantNextTip := eventLogHash(wantFrontier.TipHash, next.Seq, next.Kind, next.Event, next.TxData)
		if !bytes.Equal(next.TipHash, wantNextTip) {
			t.Fatalf("next event tip = %x, want %x", next.TipHash, wantNextTip)
		}
	})
}

// TestSnapshotMatchOrders checks which matches keep their archived orders in
// the snapshot even when the orders are outside the history window.
func TestSnapshotMatchOrders(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	oldTime := now.Add(-time.Duration(snapshotHistoryDays+5) * 24 * time.Hour)

	for _, tt := range []struct {
		name       string
		matchTime  time.Time
		active     bool
		wantOrders bool
	}{
		{name: "old active match", matchTime: oldTime, active: true, wantOrders: true},
		{name: "recent inactive match", matchTime: now, wantOrders: true},
		{name: "old inactive match", matchTime: oldTime},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			prepareSnapshotTest(t)

			maker := newLimitOrder(false, 4_500_000, 1, order.StandingTiF, 0)
			maker.SetTime(oldTime)
			mustStoreOrder(t, maker, order.OrderStatusExecuted)
			taker := newLimitOrder(true, 4_490_000, 1, order.ImmediateTiF, 0)
			taker.SetTime(oldTime)
			mustStoreOrder(t, taker, order.OrderStatusExecuted)
			bystander := newLimitOrder(true, 4_700_000, 1, order.ImmediateTiF, 0)
			bystander.SetTime(oldTime)
			mustStoreOrder(t, bystander, order.OrderStatusExecuted)

			epoch := order.EpochID{
				Idx: uint64(tt.matchTime.UnixMilli() / int64(EpochDuration)),
				Dur: EpochDuration,
			}
			match := newMatch(maker, taker, taker.Quantity, epoch)
			if err := archie.InsertMatch(match); err != nil {
				t.Fatalf("InsertMatch: %v", err)
			}
			if !tt.active {
				if err := archie.SetMatchInactive(db.MatchID(match), false); err != nil {
					t.Fatalf("SetMatchInactive: %v", err)
				}
			}

			reloadFromSnapshot(t, ctx)

			if tt.wantOrders {
				checkStoredOrder(t, maker, order.OrderStatusExecuted)
				checkStoredOrder(t, taker, order.OrderStatusExecuted)
			} else {
				mustOrderUnknown(t, maker)
				mustOrderUnknown(t, taker)
			}
			mustOrderUnknown(t, bystander)

			swaps, err := archie.ActiveSwaps()
			if err != nil {
				t.Fatalf("ActiveSwaps after load: %v", err)
			}
			if tt.active {
				if len(swaps) != 1 || swaps[0].MatchData.ID != match.ID() {
					t.Fatalf("ActiveSwaps after load = %d rows, want the one active match %v", len(swaps), match.ID())
				}
			} else if len(swaps) != 0 {
				t.Fatalf("ActiveSwaps after load = %d rows, want none", len(swaps))
			}
		})
	}
}

// TestSnapshotCarriesArchivedCancelTargets: recent cancel pulls old target order.
func TestSnapshotCarriesArchivedCancelTargets(t *testing.T) {
	ctx, cancelCtx := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelCtx()
	prepareSnapshotTest(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	oldTime := now.Add(-time.Duration(snapshotHistoryDays+5) * 24 * time.Hour)

	// A recent cancel must keep its target even though the target is
	// outside the history window.
	target := newLimitOrder(false, 4_500_000, 1, order.StandingTiF, 0)
	target.SetTime(oldTime)
	mustStoreOrder(t, target, order.OrderStatusCanceled)
	cancel := newCancelOrder(target.ID(), AssetDCR, AssetBTC, 0)
	cancel.SetTime(now)
	cancel.AccountID = target.AccountID
	mustStoreOrder(t, cancel, order.OrderStatusExecuted)

	bystander := newLimitOrder(true, 4_700_000, 1, order.ImmediateTiF, 0)
	bystander.SetTime(oldTime)
	mustStoreOrder(t, bystander, order.OrderStatusExecuted)

	reloadFromSnapshot(t, ctx)

	checkStoredOrder(t, target, order.OrderStatusCanceled)
	got, status, err := archie.Order(cancel.ID(), cancel.Base(), cancel.Quote())
	if err != nil {
		t.Fatalf("Order(cancel): %v", err)
	}
	if status != order.OrderStatusExecuted {
		t.Fatalf("Order(cancel) status = %v, want executed", status)
	}
	co, ok := got.(*order.CancelOrder)
	if !ok || co.TargetOrderID != target.ID() {
		t.Fatalf("cancel target after load = %v, want %v", got, target.ID())
	}
	mustOrderUnknown(t, bystander)
}

func TestLoadSnapshotRefusesNonEmpty(t *testing.T) {
	for _, tt := range []struct {
		name      string
		addPoints bool
	}{
		{name: "points only", addPoints: true},
		{name: "event log only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			prepareSnapshotTest(t)

			var snapshot bytes.Buffer
			if _, err := archie.WriteSnapshot(ctx, &snapshot); err != nil {
				t.Fatal(err)
			}
			var wantPointRows int
			var wantFrontier db.EventLogPosition
			if tt.addPoints {
				insertSnapshotTestPoint(t, ctx, []byte{0xaa}, []byte{0xbb})
				wantPointRows = 1
			} else {
				entry := appendSnapshotTestEvent(t, ctx, []byte("existing event"))
				wantFrontier = db.EventLogPosition{Seq: entry.Seq, TipHash: entry.TipHash}
			}

			frontier, err := archie.LoadSnapshot(ctx, &snapshot)
			if err == nil || !strings.Contains(err.Error(), "existing state") || frontier != nil {
				t.Fatalf("LoadSnapshot = (%v, %v), want existing state refusal", frontier, err)
			}
			var pointRows int
			if err := archie.db.QueryRowContext(ctx, "SELECT count(*) FROM "+archie.tables.points).Scan(&pointRows); err != nil {
				t.Fatal(err)
			}
			if pointRows != wantPointRows {
				t.Fatalf("point rows after refusal = %d, want %d", pointRows, wantPointRows)
			}
			assertEventLogFrontier(t, ctx, wantFrontier.Seq, wantFrontier.TipHash)
		})
	}
}

func TestSnapshotEmptyRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prepareSnapshotTest(t)

	frontier := reloadFromSnapshot(t, ctx)
	if frontier.Seq != 0 || len(frontier.TipHash) != 0 {
		t.Fatalf("empty snapshot frontier = %v", frontier)
	}
	entries, err := archie.EventLogEntriesAfter(ctx, 0, 10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty snapshot event log = (%+v, %v)", entries, err)
	}
	empty, err := archie.HasNoEventSourcedState(ctx)
	if err != nil || !empty {
		t.Fatalf("state after empty snapshot = (empty %v, %v)", empty, err)
	}
	first := appendSnapshotTestEvent(t, ctx, []byte("first event"))
	wantTip := eventLogHash(nil, 1, first.Kind, first.Event, first.TxData)
	if first.Seq != 1 || !bytes.Equal(first.TipHash, wantTip) {
		t.Fatalf("first event after empty snapshot = %+v, want seq 1 and tip %x", first, wantTip)
	}
}

func TestLoadSnapshotRollsBackPartialLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prepareSnapshotTest(t)

	bookedOrder := newLimitOrder(false, 4_800_000, 1, order.StandingTiF, 0)
	mustStoreOrder(t, bookedOrder, order.OrderStatusBooked)
	insertSnapshotTestPoint(t, ctx, []byte{0xaa}, []byte{0xbb})
	appendSnapshotTestEvent(t, ctx, []byte("source event"))
	snapshot, err := archie.buildSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Keep an unmodified snapshot to verify a retry succeeds.
	validSnapshot := snapshotBytes(t, snapshot)

	// Restore the points row before reaching the table that will fail.
	pointIndex := -1
	for i, table := range snapshot.Tables {
		if table.Schema == publicSchema && table.Table == pointsTableName {
			pointIndex = i
			break
		}
	}
	if pointIndex < 0 || len(snapshot.Tables[pointIndex].Rows) != 1 {
		t.Fatal("snapshot fixture must contain one point")
	}
	snapshot.Tables[0], snapshot.Tables[pointIndex] = snapshot.Tables[pointIndex], snapshot.Tables[0]

	// A missing column makes COPY fail on the populated order table.
	var failedTable string
	for i := range snapshot.Tables {
		table := &snapshot.Tables[i]
		if table.Table == ordersActiveTableName && len(table.Rows) > 0 {
			table.Columns[0] = "missing_snapshot_column"
			failedTable = table.key()
			break
		}
	}
	if failedTable == "" {
		t.Fatal("snapshot fixture must contain an active order")
	}

	mustCleanTables(t)
	inUseBefore := archie.db.Stats().InUse
	frontier, err := archie.LoadSnapshot(ctx, bytes.NewReader(snapshotBytes(t, snapshot)))
	if frontier != nil || err == nil || !strings.Contains(err.Error(), "load "+failedTable) {
		t.Fatalf("LoadSnapshot = (%v, %v), want failure loading %s", frontier, err, failedTable)
	}
	if inUse := archie.db.Stats().InUse; inUse != inUseBefore {
		t.Fatalf("failed load left %d connections in use, want %d", inUse, inUseBefore)
	}

	// Check before the load context expires, so its automatic rollback cannot
	// hide a transaction left open by LoadSnapshot.
	checkCtx, cancelCheck := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCheck()
	var pointRows int
	if err := archie.db.QueryRowContext(checkCtx, "SELECT count(*) FROM "+archie.tables.points).Scan(&pointRows); err != nil {
		t.Fatal(err)
	}
	if pointRows != 0 {
		t.Fatalf("partial load left %d point rows", pointRows)
	}
	assertEventLogFrontier(t, checkCtx, 0, nil)

	// A failed restore must not prevent a subsequent valid restore.
	if _, err := archie.LoadSnapshot(checkCtx, bytes.NewReader(validSnapshot)); err != nil {
		t.Fatalf("valid load after rollback: %v", err)
	}
	checkStoredOrder(t, bookedOrder, order.OrderStatusBooked)
}
