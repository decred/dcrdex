//go:build pgonline

package pg

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"decred.org/dcrdex/server/meshevents"
)

func seedMarketLifecycle(t *testing.T, lc *db.MarketLifecycle) {
	t.Helper()
	tx, err := archie.db.Begin()
	if err != nil {
		t.Fatalf("Begin error: %v", err)
	}
	defer tx.Rollback()
	if err := archie.upsertMarketLifecycleTx(tx, lc); err != nil {
		t.Fatalf("upsertMarketLifecycleTx error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit error: %v", err)
	}
}

func testMarketRunParams() meshevents.MarketRunParams {
	return meshevents.MarketRunParams{
		LotSize: LotSize, RateStep: RateStep, ParcelSize: 10,
		MaxUserCancelsPerEpoch: math.MaxUint32,
	}
}

func TestApplyMarketStartedEvent(t *testing.T) {
	ctx := context.Background()
	const epochDur = int64(EpochDuration)

	checkLifecycle := func(t *testing.T, want *db.MarketLifecycle) {
		t.Helper()
		got, err := archie.MarketLifecycle(want.Market)
		if err != nil {
			t.Fatalf("MarketLifecycle: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("lifecycle = %+v, want %+v", got, want)
		}
	}

	checkResult := func(t *testing.T, result *db.MarketStartedApplyResult) {
		t.Helper()
		if result == nil || result.Log == nil || result.Lifecycle == nil {
			t.Fatalf("incomplete market started result: %+v", result)
		}
		checkLifecycle(t, result.Lifecycle)
	}

	t.Run("first start", func(t *testing.T) {
		if err := cleanTables(archie.db); err != nil {
			t.Fatalf("cleanTables: %v", err)
		}
		const epochIdx = int64(12345)
		update := &db.MarketStartedUpdate{
			Market: "dcr_btc", Base: AssetDCR, Quote: AssetBTC,
			CurrentEpochIdx: epochIdx, EpochDur: epochDur,
			RunParams: testMarketRunParams(), RevocationTime: time.UnixMilli(epochIdx * epochDur).UTC(),
		}
		event := []byte("market-started-event")
		result, err := archie.ApplyMarketStartedEvent(ctx, &db.EventLogMeta{Event: event}, update)
		if err != nil {
			t.Fatalf("ApplyMarketStartedEvent: %v", err)
		}
		checkResult(t, result)
		tip := testEventApplyTip(t, nil, 1, meshevents.EventKindMarketStarted, event, update)
		requireEventApplyLog(t, result.Log, 1, meshevents.EventKindMarketStarted, event, tip, update)
		assertEventLogFrontier(t, ctx, 1, tip)
		checkLifecycle(t, &db.MarketLifecycle{
			Market: "dcr_btc", State: db.MarketStateRunning,
			StartEpochIdx: epochIdx, StartEpochDur: epochDur,
			ActiveEpochIdx: epochIdx, ProcessedEpochIdx: epochIdx - 1,
			RunParams: update.RunParams,
		})
	})

	t.Run("running restart", func(t *testing.T) {
		if err := cleanTables(archie.db); err != nil {
			t.Fatalf("cleanTables: %v", err)
		}
		const epochIdx = int64(20000)
		lifecycle := &db.MarketLifecycle{
			Market: "dcr_btc", State: db.MarketStateRunning,
			StartEpochIdx: epochIdx - 10, StartEpochDur: epochDur,
			ActiveEpochIdx: epochIdx - 5, ProcessedEpochIdx: epochIdx - 6,
			RunParams: testMarketRunParams(),
		}
		seedMarketLifecycle(t, lifecycle)

		booked := newLimitOrder(false, 4_900_000, 2, order.StandingTiF, 0)
		booked.SetTime(time.UnixMilli((epochIdx-8)*epochDur + 1))
		booked.AddFill(LotSize)
		target := newLimitOrder(false, 4_950_000, 1, order.StandingTiF, 0)
		target.SetTime(time.UnixMilli((epochIdx-8)*epochDur + 2))
		for _, ord := range []*order.LimitOrder{booked, target} {
			if err := storeOrderForTest(archie, ord, epochIdx-8, epochDur, order.OrderStatusBooked); err != nil {
				t.Fatalf("store booked order: %v", err)
			}
		}
		trade := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0)
		trade.SetTime(time.UnixMilli((epochIdx-5)*epochDur + 1))
		cancel := newCancelOrder(target.ID(), AssetDCR, AssetBTC, 0)
		cancel.AccountID = target.User()
		cancel.SetTime(time.UnixMilli((epochIdx-5)*epochDur + 2))
		for _, ord := range []order.Order{trade, cancel} {
			if err := storeOrderForTest(archie, ord, epochIdx-5, epochDur, order.OrderStatusEpoch); err != nil {
				t.Fatalf("store epoch order: %v", err)
			}
		}

		update := &db.MarketStartedUpdate{
			Market: "dcr_btc", Base: AssetDCR, Quote: AssetBTC,
			CurrentEpochIdx: epochIdx, EpochDur: epochDur,
			RunParams: lifecycle.RunParams, RevocationTime: time.UnixMilli(epochIdx * epochDur).UTC(),
			BookedRevokes: []*db.StartupOrderRevoke{{Order: booked, Reason: meshevents.StartupOrderRevokeReasonFundingCoinSpent}},
		}
		// Missing epoch revokes must roll back the lifecycle and booked revocation.
		if _, err := archie.ApplyMarketStartedEvent(ctx, &db.EventLogMeta{Event: []byte("missing-revokes")}, update); err == nil {
			t.Fatal("startup succeeded without revoking the active epoch orders")
		}
		checkLifecycle(t, lifecycle)
		if _, status, err := archie.Order(booked.ID(), AssetDCR, AssetBTC); err != nil || status != order.OrderStatusBooked {
			t.Fatalf("booked order after rollback: status %v, error %v", status, err)
		}
		if _, status, err := archie.Order(trade.ID(), AssetDCR, AssetBTC); err != nil || status != order.OrderStatusEpoch {
			t.Fatalf("epoch order after rollback: status %v, error %v", status, err)
		}
		assertEventLogFrontier(t, ctx, 0, nil)

		update.EpochRevokes = []*db.StartupOrderRevoke{
			{Order: trade, Reason: meshevents.StartupOrderRevokeReasonEpochAbandoned},
			{Order: cancel, Reason: meshevents.StartupOrderRevokeReasonEpochAbandoned},
		}
		result, err := archie.ApplyMarketStartedEvent(ctx, &db.EventLogMeta{Event: []byte("running-restart")}, update)
		if err != nil {
			t.Fatalf("ApplyMarketStartedEvent: %v", err)
		}
		checkResult(t, result)
		checkLifecycle(t, &db.MarketLifecycle{
			Market: "dcr_btc", State: db.MarketStateRunning,
			StartEpochIdx: epochIdx, StartEpochDur: epochDur,
			ActiveEpochIdx: epochIdx, ProcessedEpochIdx: epochIdx - 1,
			RunParams: update.RunParams,
		})
		for _, ord := range []*order.LimitOrder{booked, trade} {
			stored, status, err := archie.Order(ord.ID(), AssetDCR, AssetBTC)
			if err != nil || status != order.OrderStatusRevoked {
				t.Fatalf("revoked order %v: status %v, error %v", ord.ID(), status, err)
			}
			if stored.Trade().Filled() != ord.Filled() {
				t.Fatalf("revocation changed filled amount for %v", ord.ID())
			}
			// A server-generated cancel records each revocation, but is not counted.
			revocation := makePseudoCancel(ord.ID(), ord.User(), AssetDCR, AssetBTC, update.RevocationTime)
			if _, status, err := archie.Order(revocation.ID(), AssetDCR, AssetBTC); err != nil || status != order.OrderStatusRevoked {
				t.Fatalf("revocation cancel for %v: status %v, error %v", ord.ID(), status, err)
			}
			cancelTable := fullCancelOrderTableName(archie.dbName, mktInfo.Name, false)
			stmt := fmt.Sprintf(internal.SelectRevokeCancels, cancelTable)
			counted, err := revokeGeneratedCancelsForUser(ctx, archie.db, stmt, ord.User(), 10)
			if err != nil || len(counted) != 0 {
				t.Fatalf("counted cancels for %v: %v, error %v", ord.ID(), counted, err)
			}
		}
		if status, _, _, err := archie.orderStatusByID(archie.db, cancel.ID(), AssetDCR, AssetBTC); err != nil || status != orderStatusFailed {
			t.Fatalf("abandoned cancel: status %v, error %v", status, err)
		}
		if _, status, err := archie.Order(target.ID(), AssetDCR, AssetBTC); err != nil || status != order.OrderStatusBooked {
			t.Fatalf("cancel target: status %v, error %v", status, err)
		}
		for _, ord := range []order.Order{booked, trade, cancel} {
			_, _, outcomes, err := archie.GetUserReputationData(ctx, ord.User(), 10, 10, 10)
			if err != nil || len(outcomes) != 0 {
				t.Fatalf("reputation outcomes for %v: %v, error %v", ord.ID(), outcomes, err)
			}
		}
	})

	t.Run("restart after final epoch", func(t *testing.T) {
		if err := cleanTables(archie.db); err != nil {
			t.Fatalf("cleanTables: %v", err)
		}
		const finalEpochIdx = int64(30000)
		persistBook := true
		lifecycle := &db.MarketLifecycle{
			Market: "dcr_btc", State: db.MarketStateRunning,
			StartEpochIdx: finalEpochIdx - 5, StartEpochDur: epochDur,
			FinalEpochIdx: finalEpochIdx, FinalEpochDur: epochDur,
			PendingAction:   db.MarketPendingSuspend,
			PendingEpochIdx: finalEpochIdx, PendingEpochDur: epochDur,
			PersistBook: &persistBook, RunParams: testMarketRunParams(),
		}
		seedMarketLifecycle(t, lifecycle)
		ord := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0)
		ord.SetTime(time.UnixMilli(finalEpochIdx*epochDur + 1))
		if err := storeOrderForTest(archie, ord, finalEpochIdx, epochDur, order.OrderStatusEpoch); err != nil {
			t.Fatalf("store epoch order: %v", err)
		}
		update := &db.MarketStartedUpdate{
			Market: "dcr_btc", Base: AssetDCR, Quote: AssetBTC,
			CurrentEpochIdx: finalEpochIdx + 3, EpochDur: epochDur,
			RunParams: lifecycle.RunParams, RevocationTime: time.UnixMilli((finalEpochIdx + 3) * epochDur).UTC(),
			EpochRevokes: []*db.StartupOrderRevoke{{Order: ord, Reason: meshevents.StartupOrderRevokeReasonEpochAbandoned}},
		}
		result, err := archie.ApplyMarketStartedEvent(ctx, &db.EventLogMeta{Event: []byte("draining-restart")}, update)
		if err != nil {
			t.Fatalf("ApplyMarketStartedEvent: %v", err)
		}
		checkResult(t, result)
		if _, status, err := archie.Order(ord.ID(), AssetDCR, AssetBTC); err != nil || status != order.OrderStatusRevoked {
			t.Fatalf("epoch order: status %v, error %v", status, err)
		}
		checkLifecycle(t, &db.MarketLifecycle{
			Market: "dcr_btc", State: db.MarketStateDraining,
			StartEpochIdx: lifecycle.StartEpochIdx, StartEpochDur: epochDur,
			FinalEpochIdx: finalEpochIdx, FinalEpochDur: epochDur,
			ProcessedEpochIdx: finalEpochIdx,
			PersistBook:       &persistBook, RunParams: lifecycle.RunParams,
		})
	})
}

func TestApplyOrdersRevokedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
	ctx := context.Background()
	revokeTime := time.Now().Truncate(time.Millisecond).UTC()
	booked := newLimitOrder(false, 5_000_000, 1, order.StandingTiF, 0)
	booked.ClientTime = revokeTime.Add(-time.Minute)
	booked.SetTime(booked.ClientTime)
	epochIdx := booked.ServerTime.UnixMilli() / int64(EpochDuration)
	if err := storeOrderForTest(archie, booked, epochIdx, int64(EpochDuration), order.OrderStatusBooked); err != nil {
		t.Fatalf("store booked order: %v", err)
	}
	update := &db.OrdersRevokedUpdate{
		Reason:     meshevents.OrderRevokeReasonPenalty,
		RevokeTime: revokeTime,
		Orders:     []*order.LimitOrder{booked},
	}
	policy := &db.ReputationOutcomePolicy{OrderLimit: 10, FreeCancelThreshold: 2}
	event := []byte("orders-revoked-event")
	logEntry, err := archie.ApplyOrdersRevokedEvent(ctx, &db.EventLogMeta{Event: event}, policy, update)
	if err != nil {
		t.Fatalf("ApplyOrdersRevokedEvent: %v", err)
	}
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindOrdersRevoked, event, update)
	requireEventApplyLog(t, logEntry, 1, meshevents.EventKindOrdersRevoked, event, tip, update)
	assertEventLogFrontier(t, ctx, 1, tip)

	if _, status, err := archie.Order(booked.ID(), booked.Base(), booked.Quote()); err != nil || status != order.OrderStatusRevoked {
		t.Fatalf("revoked order: status %v, error %v", status, err)
	}
	// Even revocation for a penalty is not counted as a user cancellation.
	_, _, outcomes, err := archie.GetUserReputationData(ctx, booked.User(), 10, 10, 10)
	if err != nil {
		t.Fatalf("GetUserReputationData: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("got %d order outcomes, want 1", len(outcomes))
	}
	if outcomes[0].Canceled {
		t.Fatal("server revocation counted as a user cancellation")
	}
	stored, status, err := archie.Order(outcomes[0].OrderID, booked.Base(), booked.Quote())
	if err != nil {
		t.Fatalf("generated cancel: %v", err)
	}
	cancel, ok := stored.(*order.CancelOrder)
	if !ok {
		t.Fatalf("generated order type = %T, want *order.CancelOrder", stored)
	}
	if status != order.OrderStatusRevoked || cancel.TargetOrderID != booked.ID() || cancel.User() != booked.User() {
		t.Fatalf("unexpected generated cancel: status %v, order %+v", status, cancel)
	}
	if !cancel.ClientTime.Equal(revokeTime) || !cancel.ServerTime.Equal(revokeTime) {
		t.Fatalf("cancel times = %v/%v, want %v", cancel.ClientTime, cancel.ServerTime, revokeTime)
	}
}

func TestApplyEpochProcessedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	const epochIdx, epochDur int64 = 13245678, 6000
	seedMarketLifecycle(t, &db.MarketLifecycle{
		RunParams:     testMarketRunParams(),
		Market:        "dcr_btc",
		State:         db.MarketStateRunning,
		StartEpochIdx: epochIdx,
		StartEpochDur: epochDur,
		PendingAction: db.MarketPendingNone,
		// The epoch is closed but has not been processed.
		ActiveEpochIdx:    epochIdx + 1,
		ProcessedEpochIdx: epochIdx - 1,
	})

	storeEpochOrder := func(label string, ord order.Order, idx int64, status order.OrderStatus) {
		t.Helper()

		if err := storeOrderForTest(archie, ord, idx, epochDur, status); err != nil {
			t.Fatalf("StoreOrder %s %v error: %v", label, ord.ID(), err)
		}
	}

	storeEpochCancel := func(label string, cancel *order.CancelOrder, idx int64, gap int32) {
		t.Helper()

		if err := archie.storeOrder(archie.db, cancel, idx, epochDur, gap, orderStatusEpoch); err != nil {
			t.Fatalf("storeOrder (epoch) %s error: %v", label, err)
		}
	}

	booked := newLimitOrder(false, 5_000_000, 1, order.StandingTiF, 10)
	storeEpochOrder("booked", booked, epochIdx, order.OrderStatusEpoch)
	partial := newLimitOrder(false, 5_050_000, 3, order.StandingTiF, 13)
	storeEpochOrder("partial", partial, epochIdx, order.OrderStatusBooked)
	partial.Trade().SetFill(LotSize)
	cancelTarget := newLimitOrder(false, 5_025_000, 1, order.StandingTiF, 14)
	storeEpochOrder("cancel target", cancelTarget, epochIdx, order.OrderStatusBooked)
	fastCancel := newCancelOrder(booked.ID(), AssetDCR, AssetBTC, 11)
	storeEpochCancel("fast cancel", fastCancel, epochIdx, 1)
	freeCancel := newCancelOrder(booked.ID(), AssetDCR, AssetBTC, 12)
	storeEpochCancel("free cancel", freeCancel, epochIdx, 2)
	matchedCancel := newCancelOrder(cancelTarget.ID(), AssetDCR, AssetBTC, 15)
	storeEpochCancel("matched cancel", matchedCancel, epochIdx, 1)
	maker := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 20)
	taker := newLimitOrder(true, 4_800_000, 1, order.ImmediateTiF, 21)
	for _, ord := range []order.Order{maker, taker} {
		storeEpochOrder("match", ord, epochIdx, order.OrderStatusExecuted)
	}
	match := newMatch(maker, taker, taker.Quantity, order.EpochID{
		Idx: uint64(epochIdx),
		Dur: uint64(epochDur),
	})
	cancelMatch := newMatch(cancelTarget, matchedCancel, 0, order.EpochID{
		Idx: uint64(epochIdx),
		Dur: uint64(epochDur),
	})
	update := &db.EpochProcessedUpdate{
		Epoch: &db.EpochResults{
			MktBase:        AssetDCR,
			MktQuote:       AssetBTC,
			Idx:            epochIdx,
			Dur:            epochDur,
			MatchTime:      (epochIdx + 1) * epochDur,
			CSum:           []byte{0x01, 0x02},
			Seed:           []byte{0x03, 0x04},
			OrdersRevealed: []order.OrderID{booked.ID()},
			MatchVolume:    match.Quantity,
			QuoteVolume:    match.Quantity * match.Rate,
			HighRate:       5_000_000,
			LowRate:        4_800_000,
			StartRate:      4_900_000,
			EndRate:        4_950_000,
		},
		TradesBooked:    []*order.LimitOrder{booked},
		TradesPartial:   []*order.LimitOrder{partial},
		TradesCanceled:  []*order.LimitOrder{cancelTarget},
		CancelsExecuted: []*order.CancelOrder{fastCancel, freeCancel, matchedCancel},
		Matches:         []*order.Match{match, cancelMatch},
	}

	// Apply the order and match changes together with the event-log entry.
	event := []byte("epoch-processed-event")
	policy := &db.ReputationOutcomePolicy{OrderLimit: 10, FreeCancelThreshold: 2}
	logEntry, err := archie.ApplyEpochProcessedEvent(ctx, &db.EventLogMeta{Event: event}, policy, update)
	if err != nil {
		t.Fatalf("ApplyEpochProcessedEvent error: %v", err)
	}
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindEpochProcessed, event, update)
	requireEventApplyLog(t, logEntry, 1, meshevents.EventKindEpochProcessed, event, tip, update)
	assertEventLogFrontier(t, ctx, 1, tip)

	for _, want := range []struct {
		name   string
		ord    order.Order
		status order.OrderStatus
		filled int64
	}{
		{"booked", booked, order.OrderStatusBooked, 0},
		{"partial", partial, order.OrderStatusBooked, int64(LotSize)},
		{"cancel target", cancelTarget, order.OrderStatusCanceled, 0},
		{"fast cancel", fastCancel, order.OrderStatusExecuted, -1},
		{"free cancel", freeCancel, order.OrderStatusExecuted, -1},
		{"matched cancel", matchedCancel, order.OrderStatusExecuted, -1},
	} {
		status, _, filled, err := archie.OrderStatus(want.ord)
		if err != nil {
			t.Fatalf("%s OrderStatus: %v", want.name, err)
		}
		if status != want.status || filled != want.filled {
			t.Errorf("%s status/filled = %v/%d, want %v/%d", want.name, status, filled, want.status, want.filled)
		}
	}

	for _, want := range []struct {
		name   string
		match  *order.Match
		status order.MatchStatus
		active bool
	}{
		{"trade", match, order.NewlyMatched, true},
		{"cancel", cancelMatch, order.MatchComplete, false},
	} {
		stored, err := archie.MatchByID(want.match.ID(), AssetDCR, AssetBTC)
		if err != nil {
			t.Fatalf("%s MatchByID: %v", want.name, err)
		}
		if stored.Status != want.status || stored.Active != want.active {
			t.Errorf("%s match status/active = %v/%v, want %v/%v", want.name, stored.Status, stored.Active, want.status, want.active)
		}
	}

	for _, want := range []struct {
		name      string
		cancel    *order.CancelOrder
		penalized bool
	}{
		{"fast cancel", fastCancel, true},
		{"free cancel", freeCancel, false},
		{"matched cancel", matchedCancel, true},
	} {
		_, _, outcomes, err := archie.GetUserReputationData(ctx, want.cancel.User(), 10, 10, 10)
		if err != nil {
			t.Fatalf("%s GetUserReputationData: %v", want.name, err)
		}
		if len(outcomes) != 1 || outcomes[0].OrderID != want.cancel.ID() || outcomes[0].Canceled != want.penalized {
			t.Errorf("%s outcomes = %+v, want one outcome for %v with canceled=%t", want.name, outcomes, want.cancel.ID(), want.penalized)
		}
	}

	rate, err := archie.LastEpochRate(AssetDCR, AssetBTC)
	if err != nil {
		t.Fatalf("LastEpochRate: %v", err)
	}
	if rate != update.Epoch.EndRate {
		t.Errorf("last rate = %d, want %d", rate, update.Epoch.EndRate)
	}
	requireProcessedEpoch(t, epochIdx)
}

// TestApplyEpochProcessedPreimageOutcomes checks preimage storage and reputation
// updates while a draining market finishes processing its closed epochs.
func TestApplyEpochProcessedPreimageOutcomes(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	const epochIdx, epochDur int64 = 13245678, 6000
	persist := true
	seedMarketLifecycle(t, &db.MarketLifecycle{
		RunParams:     testMarketRunParams(),
		Market:        "dcr_btc",
		State:         db.MarketStateDraining,
		StartEpochIdx: epochIdx,
		StartEpochDur: epochDur,
		FinalEpochIdx: epochIdx + 1,
		FinalEpochDur: epochDur,
		PendingAction: db.MarketPendingNone,
		PersistBook:   &persist,
		// Both closed epochs still need to be processed.
		ProcessedEpochIdx: epochIdx - 1,
	})
	revealed, pi := newLimitOrderRevealed(false, 4_900_000, 1, order.StandingTiF, 0)
	missed, _ := newLimitOrderRevealed(true, 4_800_000, 1, order.StandingTiF, 10)
	for _, ord := range []order.Order{revealed, missed} {
		if err := storeOrderForTest(archie, ord, epochIdx, epochDur, order.OrderStatusEpoch); err != nil {
			t.Fatalf("StoreOrder %v error: %v", ord.ID(), err)
		}
	}

	epochEnd := time.UnixMilli((epochIdx + 1) * epochDur).UTC()
	revokeTime := epochEnd.Add(500 * time.Millisecond)
	update := &db.EpochProcessedUpdate{
		Epoch: &db.EpochResults{
			MktBase:   AssetDCR,
			MktQuote:  AssetBTC,
			Idx:       epochIdx,
			Dur:       epochDur,
			MatchTime: epochEnd.UnixMilli(),
			CSum:      []byte{0x0c},
			Seed:      []byte{0x5e},
		},
		Misses: []*db.PreimageMissUpdate{{
			Order:      missed,
			RevokeTime: revokeTime,
		}},
		Reveals: []*db.PreimageRevealUpdate{{
			Order:    revealed,
			Preimage: pi,
		}},
		// Every processed order must leave epoch status.
		TradesBooked: []*order.LimitOrder{revealed},
	}

	// Apply one event and verify its durable DB effects.
	event := []byte("epoch-processed-event")
	policy := &db.ReputationOutcomePolicy{PreimageLimit: 1, OrderLimit: 1}
	logEntry, err := archie.ApplyEpochProcessedEvent(ctx, &db.EventLogMeta{Event: event}, policy, update)
	if err != nil {
		t.Fatalf("ApplyEpochProcessedEvent error: %v", err)
	}
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindEpochProcessed, event, update)
	requireEventApplyLog(t, logEntry, 1, meshevents.EventKindEpochProcessed, event, tip, update)
	assertEventLogFrontier(t, ctx, 1, tip)
	requireProcessedEpoch(t, epochIdx)

	// Reveals store preimages; misses revoke the order.
	gotPI, err := archie.OrderPreimage(revealed)
	if err != nil {
		t.Fatalf("OrderPreimage error: %v", err)
	}
	if gotPI != pi {
		t.Fatalf("stored preimage = %x, want %x", gotPI, pi)
	}
	if _, status, err := archie.Order(missed.ID(), missed.Base(), missed.Quote()); err != nil || status != order.OrderStatusRevoked {
		t.Fatalf("missed order status = %v, err = %v, want revoked", status, err)
	}

	// The event facts are translated into reputation outcomes by the DB layer.
	missedPimgs, _, missedOrds, err := archie.GetUserReputationData(ctx, missed.User(), 10, 10, 10)
	if err != nil {
		t.Fatalf("GetUserReputationData missed user error: %v", err)
	}
	if len(missedPimgs) != 1 || !missedPimgs[0].Miss || len(missedOrds) != 1 || missedOrds[0].Canceled {
		t.Fatalf("missed user reputation data pimgs=%+v ords=%+v, want miss and non-canceled revoke", missedPimgs, missedOrds)
	}
	revealPimgs, _, revealOrds, err := archie.GetUserReputationData(ctx, revealed.User(), 10, 10, 10)
	if err != nil {
		t.Fatalf("GetUserReputationData revealed user error: %v", err)
	}
	if len(revealPimgs) != 1 || revealPimgs[0].Miss || len(revealOrds) != 0 {
		t.Fatalf("revealed user reputation data pimgs=%+v ords=%+v, want one success preimage only", revealPimgs, revealOrds)
	}
}

func TestApplyEpochProcessedRejectsUnprocessedOrders(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}
	ctx := context.Background()
	const epochIdx, epochDur int64 = 13245678, 6000
	seedMarketLifecycle(t, &db.MarketLifecycle{
		RunParams:         testMarketRunParams(),
		Market:            "dcr_btc",
		State:             db.MarketStateRunning,
		StartEpochIdx:     epochIdx,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    epochIdx + 1,
		ProcessedEpochIdx: epochIdx - 1,
	})
	included := newLimitOrder(false, 5_100_000, 1, order.StandingTiF, 30)
	omitted := newLimitOrder(true, 5_200_000, 1, order.StandingTiF, 40)
	for _, ord := range []order.Order{included, omitted} {
		if err := storeOrderForTest(archie, ord, epochIdx, epochDur, order.OrderStatusEpoch); err != nil {
			t.Fatalf("StoreOrder %v: %v", ord.ID(), err)
		}
	}
	// Booking only one order leaves the other in epoch status.
	update := &db.EpochProcessedUpdate{
		Epoch: &db.EpochResults{
			MktBase:   AssetDCR,
			MktQuote:  AssetBTC,
			Idx:       epochIdx,
			Dur:       epochDur,
			MatchTime: (epochIdx + 1) * epochDur,
			CSum:      []byte{0x0c},
			Seed:      []byte{0x5e},
		},
		TradesBooked: []*order.LimitOrder{included},
	}
	_, err := archie.ApplyEpochProcessedEvent(ctx, &db.EventLogMeta{Event: []byte("epoch-processed-incomplete")}, nil, update)
	if err == nil || !strings.Contains(err.Error(), "leaves 1 orders in epoch status") {
		t.Fatalf("incomplete processing error = %v, want epoch-status rejection", err)
	}
	if status, _, _, err := archie.OrderStatus(included); err != nil || status != order.OrderStatusEpoch {
		t.Fatalf("included order status = %v, err = %v, want unchanged epoch status", status, err)
	}
	assertEventLogFrontier(t, ctx, 0, nil)
	requireProcessedEpoch(t, epochIdx-1)
}

func requireProcessedEpoch(t *testing.T, want int64) {
	t.Helper()
	lifecycle, err := archie.MarketLifecycle("dcr_btc")
	if err != nil {
		t.Fatal(err)
	}
	if lifecycle == nil || lifecycle.ProcessedEpochIdx != want {
		t.Fatalf("lifecycle = %+v, want last processed epoch %d", lifecycle, want)
	}
}
