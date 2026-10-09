//go:build pgonline

package pg

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/meshevents"
	"github.com/davecgh/go-spew/spew"
)

const cancelThreshWindow = 100 // spec

// storeOrderForTest stores an order with the specified epoch and status.
func storeOrderForTest(a *Archiver, ord order.Order, epochIdx, epochDur int64, status order.OrderStatus) error {
	return a.storeOrder(a.db, ord, epochIdx, epochDur, db.EpochGapNA, marketToPgStatus(status))
}

func TestStoreOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	orderBadLotSize := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadLotSize.Quantity /= 2

	orderBadMarket := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadMarket.BaseAsset = AssetDCR
	orderBadMarket.QuoteAsset = AssetDCR // same as base

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	limitA := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	marketSellA := newMarketSellOrder(2, 1)
	marketSellB := newMarketSellOrder(2, 0)
	cancelA := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)

	// Order with the same commitment as limitA, but different order id.
	limitAx := new(order.LimitOrder)
	*limitAx = *limitA
	limitAx.SetTime(time.Now())

	var epochIdx, epochDur int64 = 13245678, 6000

	type args struct {
		ord    order.Order
		status order.OrderStatus
	}
	tests := []struct {
		name        string
		args        args
		wantErr     bool
		wantErrType error
	}{
		{
			name: "ok limit booked (active)",
			args: args{
				ord:    newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
				status: order.OrderStatusBooked,
			},
			wantErr: false,
		},
		{
			name: "ok limit epoch (active)",
			args: args{
				ord:    newLimitOrder(false, 5000000, 1, order.StandingTiF, 0),
				status: order.OrderStatusEpoch,
			},
			wantErr: false,
		},
		{
			name: "ok limit canceled (archived)",
			args: args{
				ord:    newLimitOrder(false, 4700000, 1, order.StandingTiF, 0),
				status: order.OrderStatusCanceled,
			},
			wantErr: false,
		},
		{
			name: "ok limit executed (archived)",
			args: args{
				ord:    limitA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "limit duplicate",
			args: args{
				ord:    limitA,
				status: order.OrderStatusExecuted,
			},
			wantErr: true, // same OID already in orders_archived: primary key
		},
		{
			name: "limit duplicate by commit only",
			args: args{
				ord:    limitAx,
				status: order.OrderStatusExecuted,
			},
			wantErr: false, // new OID, commit only in archive
		},
		{
			name: "limit bad quantity (lot size)",
			args: args{
				ord:    orderBadLotSize,
				status: order.OrderStatusEpoch,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "limit bad trading pair",
			args: args{
				ord:    orderBadMarket,
				status: order.OrderStatusEpoch,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrUnsupportedMarket},
		},
		{
			name: "market sell - bad status (booked)",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusBooked,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "market sell - bad status (canceled)",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusCanceled,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrInvalidOrder},
		},
		{
			name: "market sell - active",
			args: args{
				ord:    marketSellB,
				status: order.OrderStatusEpoch,
			},
			wantErr: false,
		},
		{
			name: "market sell - archived",
			args: args{
				ord:    marketSellA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "market sell - already in other table",
			args: args{
				ord:    marketSellA,
				status: order.OrderStatusExecuted,
			},
			wantErr: true, // same OID already in orders_archived: primary key
		},
		{
			name: "market sell - duplicate archived order",
			args: args{
				ord:    marketSellB, // still live in orders_active from the epoch insert
				status: order.OrderStatusExecuted,
			},
			wantErr:     true,
			wantErrType: db.ArchiveError{Code: db.ErrReusedCommit},
		},
		{
			name: "cancel order",
			args: args{
				ord:    cancelA,
				status: order.OrderStatusExecuted,
			},
			wantErr: false,
		},
		{
			name: "cancel order - duplicate archived order",
			args: args{
				ord:    cancelA,
				status: order.OrderStatusExecuted,
			},
			wantErr: true, // same OID already in cancels_archived: primary key
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storeOrderForTest(archie, tt.args.ord, epochIdx, epochDur, tt.args.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("StoreOrder() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				t.Logf("%s: %v", tt.name, err)
				if tt.wantErrType != nil && !db.SameErrorTypes(err, tt.wantErrType) {
					t.Errorf("Wrong error. Got %v, expected %v", err, tt.wantErrType)
				}
			}
		})
	}
}

func TestApplyOrderAcceptedEvent(t *testing.T) {
	ctx := context.Background()
	const epochDur int64 = 6000
	limit, preimage := newLimitOrderRevealed(false, 4_900_000, 1, order.StandingTiF, 0)
	reuse := &order.LimitOrder{
		P: limit.P, T: *limit.T.Copy(), Rate: limit.Rate, Force: limit.Force,
	}
	reuse.SetTime(limit.ServerTime.Add(time.Millisecond))
	boundary := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0)
	finalEpoch := limit.Time() / epochDur
	boundary.SetTime(time.UnixMilli((finalEpoch + 1) * epochDur))
	beforeBoundary := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0)
	beforeBoundary.SetTime(boundary.ServerTime.Add(-time.Millisecond))
	cancel := newCancelOrder(limit.ID(), limit.Base(), limit.Quote(), 0)
	cancel.Commit = limit.Commit
	otherMarket := newLimitOrderWithAssets(false, 4_900_000, 1, order.StandingTiF, 0, AssetBTC, AssetLTC)
	otherMarket.Commit = limit.Commit

	tests := []struct {
		name           string
		ord            order.Order
		existing       order.Order
		existingStatus order.OrderStatus
		suspendEpoch   int64
		draining       bool
		wantErr        string
	}{
		{name: "limit order", ord: limit},
		{name: "draining market", ord: limit, suspendEpoch: finalEpoch, draining: true, wantErr: "non-running market"},
		{name: "market order", ord: newMarketSellOrder(1, 0)},
		{name: "cancel order", ord: newCancelOrder(limit.ID(), limit.Base(), limit.Quote(), 0)},
		{name: "existing order", ord: limit, existing: limit, existingStatus: order.OrderStatusEpoch},
		{name: "active commitment", ord: reuse, existing: limit, existingStatus: order.OrderStatusEpoch, wantErr: "reuses commit"},
		{name: "trade commitment reused by cancel", ord: cancel, existing: limit, existingStatus: order.OrderStatusEpoch, wantErr: "reuses commit"},
		{name: "cancel commitment reused by trade", ord: reuse, existing: cancel, existingStatus: order.OrderStatusEpoch, wantErr: "reuses commit"},
		{name: "commitment in another market", ord: reuse, existing: otherMarket, existingStatus: order.OrderStatusEpoch, wantErr: "reuses commit"},
		{name: "archived commitment", ord: reuse, existing: limit, existingStatus: order.OrderStatusExecuted},
		{name: "before suspension boundary", ord: beforeBoundary, suspendEpoch: finalEpoch},
		{name: "suspension boundary", ord: boundary, suspendEpoch: finalEpoch, wantErr: "at/after pending suspend boundary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}
			ord := tt.ord
			update := &db.OrderAcceptedUpdate{
				Order:    ord,
				EpochIdx: ord.Time() / epochDur,
				EpochDur: epochDur,
				EpochGap: db.EpochGapNA,
			}
			if ord.Type() == order.CancelOrderType {
				update.EpochGap = 2
			}
			lifecycle := &db.MarketLifecycle{
				RunParams:     testMarketRunParams(),
				Market:        "dcr_btc",
				State:         db.MarketStateRunning,
				StartEpochIdx: update.EpochIdx,
				StartEpochDur: epochDur,
			}
			if tt.suspendEpoch != 0 {
				// Keep the epoch within the suspension cutoff to isolate the timestamp check.
				update.EpochIdx = tt.suspendEpoch
				lifecycle.StartEpochIdx = update.EpochIdx
				lifecycle.FinalEpochIdx, lifecycle.FinalEpochDur = update.EpochIdx, epochDur
				lifecycle.PendingAction = db.MarketPendingSuspend
				lifecycle.PendingEpochIdx, lifecycle.PendingEpochDur = update.EpochIdx, epochDur
				persistBook := true
				lifecycle.PersistBook = &persistBook
			}
			if tt.draining {
				lifecycle.State = db.MarketStateDraining
				lifecycle.PendingAction = db.MarketPendingNone
				lifecycle.PendingEpochIdx, lifecycle.PendingEpochDur = 0, 0
				lifecycle.ProcessedEpochIdx = finalEpoch
			}
			seedMarketLifecycle(t, lifecycle)

			if tt.existing != nil {
				if tt.existingStatus == order.OrderStatusEpoch {
					if tt.existing.Base() != ord.Base() || tt.existing.Quote() != ord.Quote() {
						otherLifecycle := *lifecycle
						otherLifecycle.Market = mktInfo2.Name
						seedMarketLifecycle(t, &otherLifecycle)
					}
					seedUpdate := *update
					seedUpdate.Order = tt.existing
					seedUpdate.EpochIdx = tt.existing.Time() / epochDur
					if _, err := archie.ApplyOrderAcceptedEvent(ctx, &db.EventLogMeta{Event: []byte("original order")}, &seedUpdate); err != nil {
						t.Fatalf("seed active order: %v", err)
					}
				} else if err := storeOrderForTest(archie, tt.existing, tt.existing.Time()/epochDur, epochDur, tt.existingStatus); err != nil {
					t.Fatalf("seed archived order: %v", err)
				}
			}

			before, err := archie.EventLogFrontier(ctx)
			if err != nil {
				t.Fatalf("EventLogFrontier before apply: %v", err)
			}
			event := []byte(tt.name)
			seq := before.Seq + 1
			tip := testEventApplyTip(t, before.TipHash, seq, meshevents.EventKindOrderAccepted, event, update)
			entry, applyErr := archie.ApplyOrderAcceptedEvent(ctx, &db.EventLogMeta{
				Seq: seq, Event: event, ExpectedTipHash: tip,
			}, update)
			after, err := archie.EventLogFrontier(ctx)
			if err != nil {
				t.Fatalf("EventLogFrontier after apply: %v", err)
			}
			if tt.wantErr != "" {
				if applyErr == nil || !strings.Contains(applyErr.Error(), tt.wantErr) {
					t.Fatalf("apply error = %v, want %q", applyErr, tt.wantErr)
				}
				if tt.existing != nil && !db.IsErrReusedCommit(applyErr) {
					t.Fatalf("apply error = %v, want ErrReusedCommit", applyErr)
				}
				if _, status, err := archie.Order(ord.ID(), ord.Base(), ord.Quote()); err == nil || status != order.OrderStatusUnknown {
					t.Fatalf("rejected order status = %v, err = %v, want unknown order", status, err)
				}
				if after.Seq != before.Seq || !bytes.Equal(after.TipHash, before.TipHash) {
					t.Fatalf("rejected event changed frontier from %v to %v", before, after)
				}
				return
			}
			if applyErr != nil {
				t.Fatalf("ApplyOrderAcceptedEvent: %v", applyErr)
			}
			requireEventApplyLog(t, entry, seq, meshevents.EventKindOrderAccepted, event, tip, update)
			if after.Seq != seq || !bytes.Equal(after.TipHash, tip) {
				t.Fatalf("frontier = (%d, %x), want (%d, %x)", after.Seq, after.TipHash, seq, tip)
			}
			stored, status, err := archie.Order(ord.ID(), ord.Base(), ord.Quote())
			if err != nil {
				t.Fatalf("Order: %v", err)
			}
			if status != order.OrderStatusEpoch || !bytes.Equal(order.EncodeOrder(stored), order.EncodeOrder(ord)) {
				t.Fatalf("stored order differs from accepted epoch order (status %v)", status)
			}
			table := fullOrderTableName(archie.dbName, "dcr_btc", true)
			if ord.Type() == order.CancelOrderType {
				table = fullCancelOrderTableName(archie.dbName, "dcr_btc", true)
				var gap int32
				err := archie.db.QueryRow(fmt.Sprintf("SELECT epoch_gap FROM %s WHERE oid = $1", table), ord.ID()).Scan(&gap)
				if err != nil || gap != update.EpochGap {
					t.Fatalf("stored epoch gap = %d, err = %v, want %d", gap, err, update.EpochGap)
				}
			}
			var epochIdx, storedEpochDur int64
			err = archie.db.QueryRow(fmt.Sprintf("SELECT epoch_idx, epoch_dur FROM %s WHERE oid = $1", table), ord.ID()).Scan(&epochIdx, &storedEpochDur)
			if err != nil || epochIdx != update.EpochIdx || storedEpochDur != epochDur {
				t.Fatalf("stored epoch = (%d, %d), err = %v, want (%d, %d)", epochIdx, storedEpochDur, err, update.EpochIdx, epochDur)
			}

			if tt.existingStatus == order.OrderStatusExecuted {
				// Archiving the new order must preserve the reused commitment and preimage.
				archivedTable := fullOrderTableName(archie.dbName, "dcr_btc", false)
				if _, err := archie.db.Exec(fmt.Sprintf("UPDATE %s SET preimage = $1 WHERE oid = $2", archivedTable), preimage, tt.existing.ID()); err != nil {
					t.Fatalf("set archived preimage: %v", err)
				}
				if err := archie.StorePreimage(ord, preimage); err != nil {
					t.Fatalf("set active preimage: %v", err)
				}
				if err := archie.updateOrderStatus(archie.db, ord, orderStatusExecuted); err != nil {
					t.Fatalf("archive reused-commit order: %v", err)
				}
				stored, status, err := archie.Order(ord.ID(), ord.Base(), ord.Quote())
				if err != nil || status != order.OrderStatusExecuted || stored.Commitment() != ord.Commitment() {
					t.Fatalf("archived reused-commit order status = %v, err = %v", status, err)
				}
				storedPreimage, err := archie.OrderPreimage(ord)
				if err != nil || storedPreimage != preimage {
					t.Fatalf("archived preimage = %x, err = %v, want %x", storedPreimage, err, preimage)
				}
			}
		})
	}
}

func TestApplySuspendedCancelEvent(t *testing.T) {
	ctx := context.Background()
	const epochDur = int64(EpochDuration)
	for _, tc := range []struct {
		name      string
		matchLots uint64
		wantErr   string
	}{
		{name: "cancel remaining quantity", matchLots: 2},
		{name: "reject original quantity", matchLots: 3, wantErr: "match ID mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}

			target := newLimitOrder(true, 4_900_000, 3, order.StandingTiF, 0)
			target.AddFill(LotSize)
			targetEpoch := target.Time() / epochDur
			persistBook := true
			seedMarketLifecycle(t, &db.MarketLifecycle{
				Market:            "dcr_btc",
				State:             db.MarketStateSuspended,
				StartEpochIdx:     targetEpoch,
				StartEpochDur:     epochDur,
				FinalEpochIdx:     targetEpoch + 1,
				FinalEpochDur:     epochDur,
				ProcessedEpochIdx: targetEpoch + 1,
				PersistBook:       &persistBook,
				RunParams:         testMarketRunParams(),
			})
			if err := storeOrderForTest(archie, target, targetEpoch, epochDur, order.OrderStatusBooked); err != nil {
				t.Fatalf("store target: %v", err)
			}
			cancel := newCancelOrder(target.ID(), AssetDCR, AssetBTC, 30)
			cancel.AccountID = target.AccountID
			epoch := order.EpochID{Idx: uint64(cancel.Time() / epochDur), Dur: uint64(epochDur)}
			match := newMatch(target, cancel, tc.matchLots*LotSize, epoch)
			match.Status = order.MatchComplete
			wantMatch := newMatch(target, cancel, target.Remaining(), epoch)
			wantMatch.Status = order.MatchComplete
			update := &db.SuspendedCancelUpdate{
				Market:          "dcr_btc",
				Base:            AssetDCR,
				Quote:           AssetBTC,
				Cancel:          cancel,
				TargetOrderID:   target.ID(),
				TargetAccount:   target.AccountID,
				TargetSell:      target.Sell,
				EpochIdx:        int64(epoch.Idx),
				EpochDur:        epochDur,
				FeeRateBase:     match.FeeRateBase,
				FeeRateQuote:    match.FeeRateQuote,
				MatchServerTime: cancel.ServerTime,
				Match:           match,
			}
			event := []byte(tc.name)
			tip := testEventApplyTip(t, nil, 1, meshevents.EventKindSuspendedCancel, event, update)
			result, applyErr := archie.ApplySuspendedCancelEvent(ctx, &db.EventLogMeta{
				Seq: 1, Event: event, ExpectedTipHash: tip,
			}, update)
			if tc.wantErr != "" {
				if applyErr == nil || !strings.Contains(applyErr.Error(), tc.wantErr) {
					t.Fatalf("apply error = %v, want %q", applyErr, tc.wantErr)
				}
				if result != nil {
					t.Fatalf("rejected cancel returned result %+v", result)
				}
			} else if applyErr != nil {
				t.Fatalf("ApplySuspendedCancelEvent: %v", applyErr)
			}

			wantTargetStatus := order.OrderStatusCanceled
			if tc.wantErr != "" {
				wantTargetStatus = order.OrderStatusBooked
			}
			storedTarget, status, err := archie.Order(target.ID(), AssetDCR, AssetBTC)
			if err != nil || status != wantTargetStatus {
				t.Fatalf("target status = %v, error = %v, want %v", status, err, wantTargetStatus)
			}
			if !bytes.Equal(order.EncodeOrder(storedTarget), order.EncodeOrder(target)) {
				t.Fatal("stored target data changed")
			}
			entries, err := archie.EventLogEntriesAfter(ctx, 0, 2)
			if err != nil {
				t.Fatalf("EventLogEntriesAfter: %v", err)
			}
			if tc.wantErr != "" {
				if _, _, err := archie.Order(cancel.ID(), AssetDCR, AssetBTC); !db.IsErrOrderUnknown(err) {
					t.Fatalf("cancel lookup error = %v, want unknown order", err)
				}
				for _, mid := range []order.MatchID{match.ID(), wantMatch.ID()} {
					if _, err := archie.MatchByID(mid, AssetDCR, AssetBTC); !db.IsErrMatchUnknown(err) {
						t.Fatalf("match %v lookup error = %v, want unknown match", mid, err)
					}
				}
				if len(entries) != 0 {
					t.Fatalf("event log has %d entries after rejection, want none", len(entries))
				}
				return
			}

			storedCancel, status, err := archie.Order(cancel.ID(), AssetDCR, AssetBTC)
			if err != nil || status != order.OrderStatusExecuted {
				t.Fatalf("cancel status = %v, error = %v, want executed", status, err)
			}
			if !bytes.Equal(order.EncodeOrder(storedCancel), order.EncodeOrder(cancel)) {
				t.Fatal("stored cancel differs from submitted cancel")
			}
			if result.Cancel == nil || !bytes.Equal(order.EncodeOrder(result.Cancel), order.EncodeOrder(cancel)) {
				t.Fatal("returned cancel differs from submitted cancel")
			}
			if result.TargetOrder == nil || !bytes.Equal(order.EncodeOrder(result.TargetOrder), order.EncodeOrder(target)) {
				t.Fatal("returned target differs from stored target")
			}
			gotMatch := result.Match
			if gotMatch == nil || gotMatch.Maker == nil || gotMatch.Taker == nil {
				t.Fatal("returned match is missing orders")
			}
			if gotMatch.ID() != wantMatch.ID() || gotMatch.Maker.ID() != target.ID() || gotMatch.Taker.ID() != cancel.ID() {
				t.Fatalf("returned match has wrong IDs: %+v", gotMatch)
			}
			if gotMatch.Quantity != target.Remaining() || gotMatch.Rate != target.Rate || gotMatch.Status != order.MatchComplete {
				t.Fatalf("returned match = %+v, want completed match for remaining quantity %d at rate %d", gotMatch, target.Remaining(), target.Rate)
			}
			if gotMatch.Epoch != epoch || gotMatch.FeeRateBase != match.FeeRateBase || gotMatch.FeeRateQuote != match.FeeRateQuote {
				t.Fatalf("returned match has wrong epoch or fee rates: %+v", gotMatch)
			}
			storedMatch, err := archie.MatchByID(wantMatch.ID(), AssetDCR, AssetBTC)
			if err != nil {
				t.Fatalf("MatchByID: %v", err)
			}
			if storedMatch.ID != wantMatch.ID() || storedMatch.Maker != target.ID() || storedMatch.Taker != cancel.ID() {
				t.Fatalf("stored match has wrong IDs: %+v", storedMatch)
			}
			if storedMatch.Quantity != target.Remaining() || storedMatch.Rate != target.Rate || storedMatch.Epoch != epoch {
				t.Fatalf("stored match has wrong quantity, rate, or epoch: %+v", storedMatch)
			}
			if storedMatch.Status != order.MatchComplete || storedMatch.Active {
				t.Fatalf("stored match = %+v, want complete and inactive", storedMatch)
			}
			if len(entries) != 1 {
				t.Fatalf("event log has %d entries, want 1", len(entries))
			}
			requireEventApplyLog(t, result.Log, 1, meshevents.EventKindSuspendedCancel, event, tip, update)
			requireEventApplyLog(t, entries[0], 1, meshevents.EventKindSuspendedCancel, event, tip, update)
		})
	}
}

func TestOrdersWithCommit(t *testing.T) {
	ctx := context.Background()
	const epochDur int64 = 6000

	requireIDs := func(t *testing.T, got []db.OrderWithStatus, want ...db.OrderWithStatus) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("OrdersWithCommit returned %d rows, want %d", len(got), len(want))
		}
		byID := make(map[order.OrderID]order.OrderStatus, len(got))
		for _, row := range got {
			byID[row.Order.ID()] = row.Status
		}
		for _, w := range want {
			st, ok := byID[w.Order.ID()]
			if !ok || st != w.Status {
				t.Fatalf("missing or wrong status for %v: got %v, want %v", w.Order.ID(), st, w.Status)
			}
		}
	}

	for _, test := range []struct {
		name     string
		newOrder func(int64) order.Order
	}{
		{"limit", func(offset int64) order.Order {
			return newLimitOrder(false, 4_900_000, 1, order.StandingTiF, offset)
		}},
		{"cancel", func(offset int64) order.Order {
			return newCancelOrder(order.OrderID{1}, AssetDCR, AssetBTC, offset)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}
			older, newer := test.newOrder(0), test.newOrder(10)
			newer.Prefix().Commit = older.Commitment()
			lookup := func(cutoff time.Time, want ...db.OrderWithStatus) {
				t.Helper()
				got, err := archie.OrdersWithCommit(ctx, older.Base(), older.Quote(), older.Commitment(), cutoff)
				if err != nil {
					t.Fatalf("OrdersWithCommit: %v", err)
				}
				requireIDs(t, got, want...)
			}
			olderTime, newerTime := older.Prefix().ServerTime, newer.Prefix().ServerTime
			lookup(olderTime)

			if err := storeOrderForTest(archie, older, older.Time()/epochDur, epochDur, order.OrderStatusEpoch); err != nil {
				t.Fatalf("store older: %v", err)
			}
			// The cutoff applies only to archived orders.
			lookup(newerTime, db.OrderWithStatus{Order: older, Status: order.OrderStatusEpoch})

			if err := archie.updateOrderStatus(archie.db, older, orderStatusExecuted); err != nil {
				t.Fatalf("archive older: %v", err)
			}
			if err := storeOrderForTest(archie, newer, newer.Time()/epochDur, epochDur, order.OrderStatusEpoch); err != nil {
				t.Fatalf("store newer: %v", err)
			}
			lookup(olderTime,
				db.OrderWithStatus{Order: older, Status: order.OrderStatusExecuted},
				db.OrderWithStatus{Order: newer, Status: order.OrderStatusEpoch})

			if err := archie.updateOrderStatus(archie.db, newer, orderStatusExecuted); err != nil {
				t.Fatalf("archive newer: %v", err)
			}
			lookup(olderTime,
				db.OrderWithStatus{Order: older, Status: order.OrderStatusExecuted},
				db.OrderWithStatus{Order: newer, Status: order.OrderStatusExecuted})
			lookup(newerTime, db.OrderWithStatus{Order: newer, Status: order.OrderStatusExecuted})
		})
	}

	t.Run("cancellation", func(t *testing.T) {
		reader := &Archiver{
			db: archie.db, dbName: archie.dbName, markets: archie.markets,
			fatal: make(chan struct{}),
		}
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := reader.OrdersWithCommit(ctx, AssetDCR, AssetBTC, order.Commitment{}, time.Time{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lookup error = %v", err)
		}
		if err := reader.LastErr(); err != nil {
			t.Fatalf("canceled lookup marked backend failed: %v", err)
		}
	})
}

func TestApplyAdvanceEpochEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	update := &meshevents.AdvanceEpochEvent{
		Market:         "dcr_btc",
		ClosedEpochIdx: 42,
		OpenedEpochIdx: 43,
		EpochDur:       6000,
	}
	seedMarketLifecycle(t, &db.MarketLifecycle{
		RunParams:         testMarketRunParams(),
		Market:            update.Market,
		State:             db.MarketStateRunning,
		StartEpochIdx:     update.ClosedEpochIdx,
		StartEpochDur:     update.EpochDur,
		PendingAction:     db.MarketPendingNone,
		ActiveEpochIdx:    update.ClosedEpochIdx,
		ProcessedEpochIdx: update.ClosedEpochIdx - 1,
	})

	// Record the event and advance the active epoch.
	event := []byte("advance-epoch-event")
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindAdvanceEpoch, event, update)
	log, err := archie.ApplyAdvanceEpochEvent(ctx, &db.EventLogMeta{Event: event}, update)
	if err != nil {
		t.Fatalf("ApplyAdvanceEpochEvent error: %v", err)
	}
	requireEventApplyLog(t, log, 1, meshevents.EventKindAdvanceEpoch, event, tip, update)
	assertEventLogFrontier(t, ctx, 1, tip)
	lifecycle, err := archie.MarketLifecycle(update.Market)
	if err != nil {
		t.Fatalf("MarketLifecycle error: %v", err)
	}
	if lifecycle.ActiveEpochIdx != update.OpenedEpochIdx {
		t.Fatalf("active epoch cursor = %d, want %d", lifecycle.ActiveEpochIdx, update.OpenedEpochIdx)
	}

	// The closed epoch is no longer active, so the same update is rejected.
	if _, err := archie.ApplyAdvanceEpochEvent(ctx, &db.EventLogMeta{
		Seq:   2,
		Event: []byte("advance-epoch-stale"),
	}, update); err == nil {
		t.Fatalf("ApplyAdvanceEpochEvent accepted a closed epoch behind the cursor")
	}
	assertEventLogFrontier(t, ctx, 1, tip)

	// A wrong expected tip rolls back both the event and lifecycle update.
	forked := *update
	forked.ClosedEpochIdx, forked.OpenedEpochIdx = 43, 44
	_, err = archie.ApplyAdvanceEpochEvent(ctx, &db.EventLogMeta{
		Seq:             2,
		Event:           []byte("advance-epoch-bad-tip"),
		ExpectedTipHash: wrongEventTip(),
	}, &forked)
	var divergence *db.EventLogDivergenceError
	if !errors.As(err, &divergence) {
		t.Fatalf("ApplyAdvanceEpochEvent error = %T %[1]v, want EventLogDivergenceError", err)
	}
	assertEventLogFrontier(t, ctx, 1, tip)
	// The active epoch must not change after rollback.
	lifecycle, err = archie.MarketLifecycle(update.Market)
	if err != nil {
		t.Fatalf("MarketLifecycle error: %v", err)
	}
	if lifecycle.ActiveEpochIdx != update.OpenedEpochIdx {
		t.Fatalf("active epoch cursor after rollback = %d, want %d", lifecycle.ActiveEpochIdx, update.OpenedEpochIdx)
	}
}

func TestBookOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Store order (epoch) for new order
	// BookOrder for existing order

	var epochIdx, epochDur int64 = 13245678, 6000

	// Store standing limit order in epoch status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Book the same limit order.
	err = archie.updateOrderStatus(archie.db, lo, orderStatusBooked)
	if err != nil {
		t.Fatalf("book status update failed: %v", err)
	}
}

func TestExecuteOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Store order (executed) for new order
	// ExecuteOrder for existing order

	var epochIdx, epochDur int64 = 13245678, 6000

	// Store standing limit order in executed status.
	lo := newLimitOrder(true, 4200000, 1, order.StandingTiF, 0)
	err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Execute the same limit order.
	err = archie.updateOrderStatus(archie.db, lo, orderStatusExecuted)
	if err != nil {
		t.Fatalf("executed status update failed: %v", err)
	}
}

func TestCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Standing limit == OK
	var epochIdx, epochDur int64 = 13245678, 6000
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("BookOrder failed: %v", err)
	}

	// Cancel the same limit order.
	err = archie.updateOrderStatus(archie.db, lo, orderStatusCanceled)
	if err != nil {
		t.Fatalf("canceled status update failed: %v", err)
	}

	// Cancel an order not in the tables yet
	lo2 := newLimitOrder(true, 4600000, 1, order.StandingTiF, 0)
	err = archie.updateOrderStatus(archie.db, lo2, orderStatusCanceled)
	if !db.IsErrOrderUnknown(err) {
		t.Fatalf("canceled status update should have failed for unknown order.")
	}
}

func TestRevokeOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Standing limit == OK
	var epochIdx, epochDur int64 = 13245678, 6000
	lo := newLimitOrder(false, 4800000, 1, order.StandingTiF, 0)
	err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Revoke the same limit order.
	cancelID, timeStamp, err := archie.RevokeOrder(lo)
	if err != nil {
		t.Fatalf("RevokeOrder failed: %v", err)
	}

	// Check for the server-generated cancel order.
	co, coStatus, err := archie.Order(cancelID, lo.BaseAsset, lo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate cancel order: %v", err)
	}
	if co.ID() != cancelID {
		t.Errorf("incorrect cancel ID retrieved")
	}
	coT, ok := co.(*order.CancelOrder)
	if !ok {
		t.Fatalf("not a cancel order")
	}
	if coT.ClientTime != timeStamp {
		t.Errorf("got ClientTime %v, expected %v", coT.ClientTime, timeStamp)
	}
	if coT.ServerTime != timeStamp {
		t.Errorf("got ServerTime %v, expected %v", coT.ServerTime, timeStamp)
	}
	if coStatus != order.OrderStatusRevoked {
		t.Errorf("got order status %v, expected %v", coStatus, order.OrderStatusRevoked)
	}
	if !coT.Commit.IsZero() {
		t.Errorf("generated cancel order did not have NULL/zero-value commitment")
	}

	// Market orders may be revoked too, while swap is in progress.
	// NOTE: executed -> revoked status change may be odd.
	mo := newMarketSellOrder(1, 0)
	err = storeOrderForTest(archie, mo, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	cancelID, timeStamp, err = archie.RevokeOrderUncounted(mo)
	if err != nil {
		t.Fatalf("RevokeOrder failed: %v", err)
	}

	co, coStatus, err = archie.Order(cancelID, mo.BaseAsset, mo.QuoteAsset)
	if err != nil {
		t.Fatalf("Failed to locate cancel order: %v", err)
	}
	if co.ID() != cancelID {
		t.Errorf("incorrect cancel ID retrieved")
	}
	coT, ok = co.(*order.CancelOrder)
	if !ok {
		t.Fatalf("not a cancel order")
	}
	if coT.ClientTime != timeStamp {
		t.Errorf("got ClientTime %v, expected %v", coT.ClientTime, timeStamp)
	}
	if coT.ServerTime != timeStamp {
		t.Errorf("got ServerTime %v, expected %v", coT.ServerTime, timeStamp)
	}
	if coStatus != order.OrderStatusRevoked {
		t.Errorf("got order status %v, expected %v", coStatus, order.OrderStatusRevoked)
	}
	if !coT.Commit.IsZero() {
		t.Errorf("generated cancel order did not have NULL/zero-value commitment")
	}

	t.Run("rollback", func(t *testing.T) {
		lo := newLimitOrder(false, 4800000, 2, order.StandingTiF, 1)
		lo.FillAmt = LotSize
		if err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusBooked); err != nil {
			t.Fatal(err)
		}
		tx, err := archie.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		cancelID, err := archie.revokeBookedOrderByID(tx, lo.ID(), lo.User(), lo.Base(), lo.Quote(), false, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		for _, oid := range []order.OrderID{lo.ID(), cancelID} {
			status, _, filled, err := archie.orderStatusByID(tx, oid, lo.Base(), lo.Quote())
			if err != nil || status != orderStatusRevoked {
				t.Fatalf("order %v within transaction: status %v, error %v", oid, status, err)
			}
			if oid == lo.ID() && filled != int64(lo.Filled()) {
				t.Fatalf("revocation changed filled amount: got %v, want %v", filled, lo.Filled())
			}
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		status, _, filled, err := archie.OrderStatusByID(lo.ID(), lo.Base(), lo.Quote())
		if err != nil || status != order.OrderStatusBooked || filled != int64(lo.Filled()) {
			t.Fatalf("order after rollback: status %v, filled %v, error %v", status, filled, err)
		}
		if _, _, err := archie.Order(cancelID, lo.Base(), lo.Quote()); !db.IsErrOrderUnknown(err) {
			t.Fatalf("cancel order after rollback: expected unknown order, got %v", err)
		}
	})

	// Revoke an order not in the tables yet
	lo2 := newLimitOrder(true, 4600000, 1, order.StandingTiF, 0)
	_, _, err = archie.RevokeOrder(lo2)
	if !db.IsErrOrderUnknown(err) {
		t.Fatalf("RevokeOrder should have failed for unknown order.")
	}
}

func TestLoadOrderUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var oid order.OrderID
	copy(oid[:], orderID0)

	ordOut, statusOut, err := archie.Order(oid, mktInfo.Base, mktInfo.Quote)
	if err == nil || ordOut != nil {
		t.Errorf("Order should have failed to load non-existent order")
	}
	if statusOut != order.OrderStatusUnknown {
		t.Errorf("status of non-existent order should be OrderStatusUnknown, got %s", statusOut)
	}
}

func TestStoreLoadLimitOrderActive(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// Limit: buy, standing, booked
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := order.OrderStatusBooked

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadLimitOrderArchived(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// Limit: buy, standing, executed
	ordIn := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	statusIn := order.OrderStatusExecuted

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadMarketOrderActive(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// Market: sell, epoch (active)
	ordIn := newMarketSellOrder(1, 0)
	statusIn := order.OrderStatusEpoch

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestStoreLoadCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: epoch (active)
	ordIn := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)
	statusIn := order.OrderStatusEpoch

	// Do not use Stringers when dumping, and stop after 4 levels deep
	spew.Config.MaxDepth = 4
	spew.Config.DisableMethods = true

	oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	ordOut, statusOut, err := archie.Order(oid, base, quote)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if ordOut.ID() != oid {
		t.Errorf("Incorrect OrderId for retrieved order. Got %v, expected %v.",
			ordOut.ID(), oid)
		spew.Dump(ordIn)
		spew.Dump(ordOut)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}
}

func TestOrderStatusUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ord := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0) // not stored
	_, _, _, err := archie.OrderStatus(ord)
	if err == nil {
		t.Fatalf("OrderStatus succeeded to find nonexistent order!")
	}
	if !db.SameErrorTypes(err, db.ArchiveError{Code: db.ErrUnknownOrder}) {
		if errA, ok := err.(db.ArchiveError); ok {
			t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %d", errA.Code)
		}
		t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %v", err)
	}
}

// Test ActiveOrderCoins, BookOrders, and EpochOrders.
func TestActiveOrderCoins(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	multiCoinLO := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	multiCoinLO.Coins = append(multiCoinLO.Coins, order.CoinID{0x22, 0x23})

	epochLO := newLimitOrder(true, 1, 1, order.StandingTiF, 0)
	epochCO := newCancelOrder(multiCoinLO.ID(), AssetDCR, AssetBTC, 0)

	orderStatuses := []struct {
		ord         order.Order
		status      order.OrderStatus
		activeCoins int
	}{
		{
			multiCoinLO,
			order.OrderStatusBooked, // active, buy, booked
			-1,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived, buy
			0,
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active, sell, epoch
			1,
		},
		{
			epochLO,
			order.OrderStatusEpoch, // active, buy, epoch
			1,
		},
		{
			epochCO,
			order.OrderStatusEpoch, // cancel, epoch
			0,
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived, sell
			0,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active, buy
			-1,
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived, buy
			0,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}
	}

	baseCoins, quoteCoins, err := archie.ActiveOrderCoins(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("ActiveOrderCoins failed: %v", err)
	}

	for _, os := range orderStatuses {
		var coins, wantCoins []order.CoinID
		switch os.activeCoins {
		case 0: // no active
		case 1: // active base coins (sell order)
			coins = baseCoins[os.ord.ID()]
			wantCoins = os.ord.Trade().Coins
		case -1: // active quote coins (buy order)
			coins = quoteCoins[os.ord.ID()]
			wantCoins = os.ord.Trade().Coins
		}

		if len(coins) != len(wantCoins) {
			t.Errorf("Order %v has %d coins, expected %d", os.ord.ID(),
				len(coins), len(wantCoins))
			continue
		}
		for i := range coins {
			if !bytes.Equal(coins[i], wantCoins[i]) {
				t.Errorf("Order %v coin %d mismatch:\n\tgot %v\n\texpected %v",
					os.ord.ID(), i, coins[i], wantCoins[i])
			}
		}
	}

	bookOrders, err := archie.BookOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("BookOrders failed: %v", err)
	}

	if len(bookOrders) != 1 {
		t.Fatalf("got %d book orders, expected 1", len(bookOrders))
	}

	// Verify the order ID of the loaded order is correct. This ensures the
	// order is being loaded with all the fields to provide and identical
	// serialization.
	if multiCoinLO.ID() != bookOrders[0].ID() {
		t.Errorf("loaded book order has an incorrect order ID. Got %v, expected %v",
			bookOrders[0].ID(), multiCoinLO.ID())
	}

	los, mos, cos, err := archie.epochOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("epochOrders failed: %v", err)
	}

	if len(los) != 1 || len(mos) != 2 || len(cos) != 1 {
		t.Fatalf("got %d epoch limit orders, %d epoch market orders, and %d epoch cancel orders, expected 1, 2, and 1",
			len(los), len(mos), len(cos))
	}

	// Verify the order ID of the loaded order is correct. This ensures the
	// order is being loaded with all the fields to provide and identical
	// serialization.
	if epochLO.ID() != los[0].ID() {
		t.Errorf("epoch limit order has an incorrect order ID. Got %v, expected %v",
			los[0].ID(), epochLO.ID())
	}
	if epochCO.ID() != cos[0].ID() {
		t.Errorf("epoch cancel order has an incorrect order ID. Got %v, expected %v",
			cos[0].ID(), epochCO.ID())
	}

	// The exported version should return the same orders.
	orders, err := archie.EpochOrders(mktInfo.Base, mktInfo.Quote)
	if err != nil {
		t.Fatalf("EpochOrders failed: %v", err)
	}

	if len(orders) != 4 {
		t.Fatalf("got %d epoch orders, expected 4", len(orders))
	}
	for _, o := range orders {
		if o.ID() == los[0].ID() ||
			o.ID() == mos[0].ID() ||
			o.ID() == mos[1].ID() ||
			o.ID() == cos[0].ID() {
			continue
		}
		t.Fatalf("order %v in EpochOrders but not epochOrders", o.ID())
	}
}

func TestOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderStatuses := []struct {
		ord    order.Order
		status order.OrderStatus
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		trade := ordIn.Trade()
		statusIn := orderStatuses[i].status
		err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		statusOut, typeOut, filledOut, err := archie.OrderStatus(ordIn)
		if err != nil {
			t.Fatalf("OrderStatus(%d:%v) failed: %v", i, ordIn, err)
		}

		if statusOut != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				statusOut, statusIn)
		}

		if typeOut != ordIn.Type() {
			t.Errorf("Incorrect OrderType for retrieved order. Got %v, expected %v.",
				typeOut, ordIn.Type())
		}

		if filledOut != int64(trade.Filled()) {
			t.Errorf("Incorrect FillAmt for retrieved order. Got %v, expected %v.",
				filledOut, trade.Filled())
		}
	}
}

func TestCancelOrderStatus(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	// Cancel: executed (archived)
	ordIn := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 0)
	statusIn := order.OrderStatusExecuted

	//oid, base, quote := ordIn.ID(), ordIn.BaseAsset, ordIn.QuoteAsset

	err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	statusOut, typeOut, filledOut, err := archie.OrderStatus(ordIn)
	if err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	if statusOut != statusIn {
		t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
			statusOut, statusIn)
	}

	if typeOut != ordIn.Type() {
		t.Errorf("Incorrect OrderType for retrieved order. Got %v, expected %v.",
			typeOut, ordIn.Type())
	}

	if filledOut != -1 {
		t.Errorf("Incorrect FilledAmt for retrieved order. Got %v, expected %v.",
			filledOut, -1)
	}
}

func TestUpdateOrderUnknown(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ord := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0) // not stored

	err := archie.UpdateOrderStatus(ord, order.OrderStatusExecuted)
	if err == nil {
		t.Fatalf("UpdateOrder succeeded to update nonexistent order!")
	}
	if !db.SameErrorTypes(err, db.ArchiveError{Code: db.ErrUnknownOrder}) {
		if errA, ok := err.(db.ArchiveError); ok {
			t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %d", errA.Code)
		}
		t.Fatalf("Expected ArchiveError with code ErrUnknownOrder, got %v", err)
	}
}

func TestUpdateOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	orderStatuses := []struct {
		ord       order.Order
		status    order.OrderStatus
		newStatus order.OrderStatus
		newFilled uint64
		wantErr   bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusEpoch,  // active
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked,   // active
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
			order.OrderStatusBooked,   // active, should err
			0,
			true,
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch,  // active
			order.OrderStatusBooked, // active, invalid for market
			0,
			false,
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
			order.OrderStatusExecuted, // archived, no change
			0,
			false,
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch,    // active
			order.OrderStatusExecuted, // archived
			2000000000,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1),
			order.OrderStatusEpoch,    // active
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
		{
			newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 2),
			order.OrderStatusExecuted, // archived
			order.OrderStatusCanceled, // archived
			0,
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		switch ot := ordIn.(type) {
		case *order.LimitOrder:
			ot.FillAmt = orderStatuses[i].newFilled
		case *order.MarketOrder:
			ot.FillAmt = orderStatuses[i].newFilled
		}

		newStatus := orderStatuses[i].newStatus
		err = archie.UpdateOrderStatus(ordIn, newStatus)
		if (err != nil) != orderStatuses[i].wantErr {
			t.Fatalf("UpdateOrderStatus(%d:%v, %s) failed: %v", i, ordIn, newStatus, err)
		}
	}
}

func TestStorePreimage(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	lo, pi := newLimitOrderRevealed(false, 4900000, 1, order.StandingTiF, 0)
	err := storeOrderForTest(archie, lo, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	err = archie.StorePreimage(lo, pi)
	if err != nil {
		t.Fatalf("StorePreimage failed: %v", err)
	}

	piOut, err := archie.OrderPreimage(lo)
	if err != nil {
		t.Fatalf("OrderPreimage failed: %v", err)
	}

	if pi != piOut {
		t.Errorf("got preimage %v, expected %v", piOut, pi)
	}

	// Now test OrderPreimage when preimage is NULL.
	lo2, _ := newLimitOrderRevealed(false, 4900000, 1, order.StandingTiF, 0)
	err = storeOrderForTest(archie, lo2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	piOut2, err := archie.OrderPreimage(lo2)
	if err != nil {
		t.Fatalf("OrderPreimage failed: %v", err)
	}
	if !piOut2.IsZero() {
		t.Errorf("Preimage should have been the zero value, got %v", piOut2)
	}
}

func TestFailCancelOrder(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	co := newCancelOrder(targetOrderID, mktInfo.Base, mktInfo.Quote, 1)
	err := storeOrderForTest(archie, co, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	err = archie.updateOrderStatus(archie.db, co, orderStatusFailed)
	if err != nil {
		t.Fatalf("failed status update failed: %v", err)
	}
	_, status, err := loadCancelOrder(context.Background(), archie.db, archie.dbName, mktInfo.Name, co.ID())
	if err != nil {
		t.Errorf("loadCancelOrder failed: %v", err)
	}

	if status != orderStatusFailed {
		t.Errorf("cancel order should have been %s, got %s", orderStatusFailed, status)
	}
}

func TestUpdateOrderFilled(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	orderStatuses := []struct {
		ord           *order.LimitOrder
		status        order.OrderStatus
		newFilled     uint64
		wantUpdateErr bool
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4100000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
			0,
			false,
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
			0,
			false,
		},
	}

	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}

		ordIn.FillAmt = orderStatuses[i].newFilled

		err = archie.UpdateOrderFilled(ordIn)
		if (err != nil) != orderStatuses[i].wantUpdateErr {
			t.Fatalf("UpdateOrderFilled(%d:%v) failed: %v", i, ordIn, err)
		}
	}
}
func TestUserOrderStatuses(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var epochIdx, epochDur int64 = 13245678, 6000

	orderStatuses := []struct {
		ord    order.Order
		status order.OrderStatus
	}{
		{
			newLimitOrder(false, 4900000, 1, order.StandingTiF, 0),
			order.OrderStatusBooked, // active
		},
		{
			newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketSellOrder(2, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketSellOrder(1, 0),
			order.OrderStatusExecuted, // archived
		},
		{
			newMarketBuyOrder(2000000000, 0),
			order.OrderStatusEpoch, // active
		},
		{
			newMarketBuyOrder(2100000000, 0),
			order.OrderStatusExecuted, // archived
		},
	}

	unsavedOrder1 := newMarketBuyOrder(3000000000, 0)
	unsavedOrder2 := newMarketSellOrder(4, 0)

	orders := make([]order.Order, 0, len(orderStatuses)+2)
	orderIDs := make([]order.OrderID, 0, len(orderStatuses)+2)

	// Add unsaved orders
	orders = append(orders, unsavedOrder1, unsavedOrder2)
	orderIDs = append(orderIDs, unsavedOrder1.ID(), unsavedOrder2.ID())

	accountID := randomAccountID()
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		statusIn := orderStatuses[i].status
		if lo, ok := ordIn.(*order.LimitOrder); ok {
			lo.BaseAsset, lo.QuoteAsset = AssetBTC, AssetLTC // swap the assets to test across different mkts
		}
		ordIn.Prefix().AccountID = accountID
		err := storeOrderForTest(archie, ordIn, epochIdx, epochDur, statusIn)
		if err != nil {
			t.Fatalf("StoreOrder failed: %v", err)
		}
		orders = append(orders, ordIn)
		orderIDs = append(orderIDs, ordIn.ID())
	}

	// All orders except the 2 limit orders are DCR-BTC.
	orderStatusesOut, err := archie.UserOrderStatuses(accountID, AssetDCR, AssetBTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != len(orderStatuses)-2 /*the 2 limits*/ {
		t.Fatalf("OrderStatuses returned %d orders instead of %d", len(orderStatusesOut), len(orderStatuses)-2)
	}
	outMap := make(map[order.OrderID]*db.OrderStatus, len(orderStatusesOut))
	for _, orderStatus := range orderStatusesOut {
		outMap[orderStatus.ID] = orderStatus
	}
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		orderOut, found := outMap[ordIn.ID()]
		if !found {
			continue
		}
		statusIn := orderStatuses[i].status
		if orderOut.Status != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				orderOut.Status, statusIn)
		}
	}

	// Check statuses for the 2 limit orders that are BTC-LTC.
	orderStatusesOut, err = archie.UserOrderStatuses(accountID, AssetBTC, AssetLTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != 2 /*the 2 limits*/ {
		t.Fatalf("OrderStatuses returned %d orders instead of %d", len(orderStatusesOut), 2)
	}
	outMap = make(map[order.OrderID]*db.OrderStatus, len(orderStatusesOut))
	for _, orderStatus := range orderStatusesOut {
		outMap[orderStatus.ID] = orderStatus
	}
	for i := range orderStatuses {
		ordIn := orderStatuses[i].ord
		orderOut, found := outMap[ordIn.ID()]
		if !found {
			continue
		}
		statusIn := orderStatuses[i].status
		if orderOut.Status != statusIn {
			t.Errorf("Incorrect OrderStatus for retrieved order. Got %v, expected %v.",
				orderOut.Status, statusIn)
		}
	}

	// Expect nothing for wrong user ID.
	orderStatusesOut, err = archie.UserOrderStatuses(randomAccountID(), AssetDCR, AssetBTC, orderIDs)
	if err != nil {
		t.Fatalf("OrderStatuses failed: %v", err)
	}
	if len(orderStatusesOut) != 0 {
		t.Fatalf("OrderStatuses returned %d orders for wrong account ID", len(orderStatusesOut))
	}
}
func TestActiveUserOrderStatuses(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Two orders, different accounts, DCR-BTC.
	maker := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	taker := newLimitOrder(true, 4490000, 1, order.StandingTiF, 10)

	var epochIdx, epochDur int64 = 13245678, 6000
	err := storeOrderForTest(archie, maker, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = storeOrderForTest(archie, taker, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Second order from the same maker account.
	maker2 := newLimitOrder(false, 4500000, 1, order.ImmediateTiF, 20)
	maker2.AccountID = maker.AccountID

	// Store it.
	err = storeOrderForTest(archie, maker2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Same taker account, different market (BTC-LTC).
	taker2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	taker2.BaseAsset = AssetBTC
	taker2.QuoteAsset = AssetLTC
	taker2.AccountID = taker.AccountID

	// Store it.
	err = storeOrderForTest(archie, taker2, epochIdx, epochDur, order.OrderStatusEpoch)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Store cancel order for taker account.
	taker2Incomplete := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	taker2Incomplete.AccountID = taker.AccountID
	err = storeOrderForTest(archie, taker2Incomplete, epochIdx, epochDur, order.OrderStatusCanceled)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Maker should have 2 active orders in 1 market.
	// Taker should have 2 active orders in 2 markets and 1 inactive (canceled) order.

	tests := []struct {
		name              string
		acctID            account.AccountID
		numExpected       int
		wantOrderIDs      []order.OrderID
		wantOrderStatuses []order.OrderStatus
		wantedErr         error
	}{
		{
			"ok maker",
			maker.User(),
			2,
			[]order.OrderID{maker.ID(), maker2.ID()},
			[]order.OrderStatus{order.OrderStatusBooked, order.OrderStatusEpoch},
			nil,
		},
		{
			"ok taker",
			taker.User(),
			2,
			[]order.OrderID{taker.ID(), taker2.ID()},
			[]order.OrderStatus{order.OrderStatusBooked, order.OrderStatusEpoch},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
			nil,
			nil,
		},
	}

	idInSlice := func(oid order.OrderID, oids []order.OrderID) int {
		for i := range oids {
			if oids[i] == oid {
				return i
			}
		}
		return -1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orderStatuses, err := archie.ActiveUserOrderStatuses(tt.acctID)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(orderStatuses) != tt.numExpected {
				t.Errorf("Retrieved %d active orders for user %v, expected %d.", len(orderStatuses), tt.acctID, tt.numExpected)
			}
			for _, ord := range orderStatuses {
				wantId := idInSlice(ord.ID, tt.wantOrderIDs)
				if wantId == -1 {
					t.Errorf("Unexpected order ID %v retrieved.", ord.ID)
					continue
				}
				if ord.Status != tt.wantOrderStatuses[wantId] {
					t.Errorf("Incorrect order status for order %v. Got %d, want %d.",
						ord.ID, ord.Status, tt.wantOrderStatuses[wantId])
				}
			}
		})
	}
}

func TestCompletedUserOrders(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	nowMs := func() int64 {
		return time.Now().UnixMilli()
	}

	// Two orders, different accounts, DCR-BTC.
	maker := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	taker := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	var epochIdx, epochDur int64 = 13245678, 6000
	err := storeOrderForTest(archie, maker, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	err = storeOrderForTest(archie, taker, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set the orders' swap completion times.
	tSwapDoneMaker := nowMs()
	if err = archie.SetOrderCompleteTime(maker, tSwapDoneMaker); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	tSwapDoneTaker := tSwapDoneMaker + 10
	if err = archie.SetOrderCompleteTime(taker, tSwapDoneTaker); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Second order from the same maker account.
	maker2 := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	maker2.AccountID = maker.AccountID

	// Store it.
	err = storeOrderForTest(archie, maker2, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	// Set swap complete time.
	tSwapDoneMaker2 := nowMs()
	if err = archie.SetOrderCompleteTime(maker2, tSwapDoneMaker2); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Same taker account, different market (BTC-LTC).
	taker2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	taker2.BaseAsset = AssetBTC
	taker2.QuoteAsset = AssetLTC
	taker2.AccountID = taker.AccountID

	// Store it.
	err = storeOrderForTest(archie, taker2, epochIdx, epochDur, order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set swap complete time.
	tSwapDoneTaker2 := nowMs()
	if err = archie.SetOrderCompleteTime(taker2, tSwapDoneTaker2); err != nil {
		t.Fatalf("SetOrderCompleteTime failed: %v", err)
	}

	// Order without completion time set.
	taker2Incomplete := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	taker2Incomplete.AccountID = taker.AccountID
	err = storeOrderForTest(archie, taker2Incomplete, epochIdx, epochDur, order.OrderStatusCanceled) // archived, but not complete
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}
	// NO SetOrderCompleteTime, BUT in an orders_archived table.

	// Try and fail to set completion time for an order not in executed status.
	taker3 := newLimitOrder(true, 4390000, 1, order.StandingTiF, 20)
	err = storeOrderForTest(archie, taker3, epochIdx, epochDur, order.OrderStatusBooked)
	if err != nil {
		t.Fatalf("StoreOrder failed: %v", err)
	}

	// Set swap complete time.
	tSwapDoneTaker3 := nowMs()
	if err = archie.SetOrderCompleteTime(taker3, tSwapDoneTaker3); !db.IsErrOrderNotExecuted(err) {
		t.Fatalf("SetOrderCompleteTime should have returned a ErrOrderNotExecuted error for booked (not executed) order")
	}

	// Maker should have 2 completed orders in 1 market.
	// Taker should have 2 completed orders in 2 markets.

	tests := []struct {
		name          string
		acctID        account.AccountID
		numExpected   int
		wantOrderIDs  []order.OrderID
		wantCompTimes []int64
		wantedErr     error
	}{
		{
			"ok maker",
			maker.User(),
			2,
			[]order.OrderID{maker.ID(), maker2.ID()},
			[]int64{tSwapDoneMaker, tSwapDoneMaker2},
			nil,
		},
		{
			"ok taker",
			taker.User(),
			2,
			[]order.OrderID{taker.ID(), taker2.ID()},
			[]int64{tSwapDoneTaker, tSwapDoneTaker2},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
			nil,
			nil,
		},
	}

	idInSlice := func(mid order.OrderID, mids []order.OrderID) int {
		for i := range mids {
			if mids[i] == mid {
				return i
			}
		}
		return -1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oids, compTimes, err := archie.CompletedUserOrders(tt.acctID, cancelThreshWindow)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(oids) != tt.numExpected {
				t.Errorf("Retrieved %d completed orders for user %v, expected %d.", len(oids), tt.acctID, tt.numExpected)
			}
			for i := range oids {
				loc := idInSlice(oids[i], tt.wantOrderIDs)
				if loc == -1 {
					t.Errorf("Unexpected order ID %v retrieved.", oids[i])
					continue
				}
				if compTimes[i] != tt.wantCompTimes[loc] {
					t.Errorf("Incorrect order completion time. Got %d, want %d.",
						compTimes[loc], tt.wantCompTimes[i])
				}
			}
		})
	}
}
