//go:build pgonline

package pg

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/meshevents"
)

func TestApplyMarketSuspendScheduledEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	const epochDur int64 = 10000
	running := &db.MarketLifecycle{
		Market:            "dcr_btc",
		State:             db.MarketStateRunning,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    15,
		ProcessedEpochIdx: 14,
		RunParams:         testMarketRunParams(),
	}
	seedMarketLifecycle(t, running)

	update := &db.MarketSuspendScheduledUpdate{
		Market:        running.Market,
		Base:          AssetDCR,
		Quote:         AssetBTC,
		FinalEpochIdx: 20,
		EpochDur:      epochDur,
		PersistBook:   false,
	}
	result, err := archie.ApplyMarketSuspendScheduledEvent(context.Background(), &db.EventLogMeta{Event: []byte("schedule")}, update)
	if err != nil {
		t.Fatalf("ApplyMarketSuspendScheduledEvent: %v", err)
	}

	if result.Log.Kind != meshevents.EventKindMarketSuspendScheduled {
		t.Fatalf("event kind = %q, want %q", result.Log.Kind, meshevents.EventKindMarketSuspendScheduled)
	}
	want := *running
	want.PendingAction = db.MarketPendingSuspend
	want.PendingEpochIdx = 20
	want.PendingEpochDur = epochDur
	want.FinalEpochIdx = 20
	want.FinalEpochDur = epochDur
	want.PersistBook = &update.PersistBook
	if !reflect.DeepEqual(result.Lifecycle, &want) {
		t.Fatalf("returned lifecycle = %+v, want %+v", result.Lifecycle, want)
	}

	stored, err := archie.MarketLifecycle(running.Market)
	if err != nil {
		t.Fatalf("MarketLifecycle: %v", err)
	}
	if !reflect.DeepEqual(stored, &want) {
		t.Fatalf("stored lifecycle = %+v, want %+v", stored, want)
	}
}

func TestApplyMarketSuspendedEvent(t *testing.T) {
	for _, tc := range []struct {
		name        string
		persistBook bool
	}{
		{name: "purge book", persistBook: false},
		{name: "retain book", persistBook: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}

			const epochDur = int64(EpochDuration)
			const finalEpochIdx int64 = 30000
			draining := &db.MarketLifecycle{
				Market:        "dcr_btc",
				State:         db.MarketStateDraining,
				StartEpochIdx: finalEpochIdx - 10,
				StartEpochDur: epochDur,
				FinalEpochIdx: finalEpochIdx,
				FinalEpochDur: epochDur,
				PendingAction: db.MarketPendingNone,
				PersistBook:   &tc.persistBook,
				// The final epoch must be processed before suspension.
				ProcessedEpochIdx: finalEpochIdx,
				RunParams:         testMarketRunParams(),
			}
			seedMarketLifecycle(t, draining)

			bookedOrders := []*order.LimitOrder{
				newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0),
				newLimitOrder(true, 5_100_000, 1, order.StandingTiF, 10),
			}
			for _, lo := range bookedOrders {
				if err := storeOrderForTest(archie, lo, finalEpochIdx-2, epochDur, order.OrderStatusBooked); err != nil {
					t.Fatalf("store order %v: %v", lo.ID(), err)
				}
			}

			update := &db.MarketSuspendedUpdate{
				Market:        draining.Market,
				Base:          AssetDCR,
				Quote:         AssetBTC,
				FinalEpochIdx: finalEpochIdx,
				EpochDur:      epochDur,
				Timestamp:     time.UnixMilli((finalEpochIdx + 1) * epochDur).UTC(),
			}
			result, err := archie.ApplyMarketSuspendedEvent(context.Background(), &db.EventLogMeta{Event: []byte("ml-suspend")}, update)
			if err != nil {
				t.Fatalf("ApplyMarketSuspendedEvent: %v", err)
			}

			if result.Log.Kind != meshevents.EventKindMarketSuspended {
				t.Fatalf("event kind = %q, want %q", result.Log.Kind, meshevents.EventKindMarketSuspended)
			}
			wantLifecycle := *draining
			wantLifecycle.State = db.MarketStateSuspended
			if !reflect.DeepEqual(result.Lifecycle, &wantLifecycle) {
				t.Fatalf("returned lifecycle = %+v, want %+v", result.Lifecycle, wantLifecycle)
			}
			stored, err := archie.MarketLifecycle(update.Market)
			if err != nil {
				t.Fatalf("MarketLifecycle: %v", err)
			}
			if !reflect.DeepEqual(stored, &wantLifecycle) {
				t.Fatalf("stored lifecycle = %+v, want %+v", stored, wantLifecycle)
			}

			var wantPurge []order.OrderID
			wantStatus := order.OrderStatusBooked
			if !tc.persistBook {
				for _, lo := range bookedOrders {
					wantPurge = append(wantPurge, lo.ID())
				}
				sortOrderIDs(wantPurge)
				wantStatus = order.OrderStatusRevoked
			}
			if !slices.Equal(result.PurgeOrders, wantPurge) {
				t.Fatalf("purge orders = %v, want %v", result.PurgeOrders, wantPurge)
			}
			for _, lo := range bookedOrders {
				if _, status, err := archie.Order(lo.ID(), lo.Base(), lo.Quote()); err != nil || status != wantStatus {
					t.Fatalf("order %v status = %v (err %v), want %v", lo.ID(), status, err, wantStatus)
				}
			}

			if !tc.persistBook {
				for _, lo := range bookedOrders {
					cancel := makePseudoCancel(lo.ID(), lo.User(), lo.Base(), lo.Quote(), update.Timestamp)
					if _, status, err := archie.Order(cancel.ID(), lo.Base(), lo.Quote()); err != nil || status != order.OrderStatusRevoked {
						t.Fatalf("revocation cancel for %v: status %v, error %v", lo.ID(), status, err)
					}
				}
			}
		})
	}
}

func TestApplyMarketResumeScheduledEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	const epochDur int64 = 10_000
	persistBook := true
	suspended := &db.MarketLifecycle{
		Market:            "dcr_btc",
		State:             db.MarketStateSuspended,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		FinalEpochIdx:     15,
		FinalEpochDur:     epochDur,
		ProcessedEpochIdx: 15,
		PersistBook:       &persistBook,
		RunParams:         testMarketRunParams(),
	}
	seedMarketLifecycle(t, suspended)

	update := &db.MarketResumeScheduledUpdate{
		Market:        suspended.Market,
		Base:          AssetDCR,
		Quote:         AssetBTC,
		StartEpochIdx: 20,
		EpochDur:      epochDur,
	}
	result, err := archie.ApplyMarketResumeScheduledEvent(context.Background(), &db.EventLogMeta{Event: []byte("schedule")}, update)
	if err != nil {
		t.Fatalf("ApplyMarketResumeScheduledEvent: %v", err)
	}

	if result.Log.Kind != meshevents.EventKindMarketResumeScheduled {
		t.Fatalf("event kind = %q, want %q", result.Log.Kind, meshevents.EventKindMarketResumeScheduled)
	}
	want := *suspended
	want.PendingAction = db.MarketPendingResume
	want.PendingEpochIdx = 20
	want.PendingEpochDur = epochDur
	want.StartEpochIdx = 20
	want.FinalEpochIdx = 0
	want.FinalEpochDur = 0
	if !reflect.DeepEqual(result.Lifecycle, &want) {
		t.Fatalf("returned lifecycle = %+v, want %+v", result.Lifecycle, want)
	}

	stored, err := archie.MarketLifecycle(suspended.Market)
	if err != nil {
		t.Fatalf("MarketLifecycle: %v", err)
	}
	if !reflect.DeepEqual(stored, &want) {
		t.Fatalf("stored lifecycle = %+v, want %+v", stored, want)
	}
}

func TestApplyMarketResumedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	const epochDur = int64(EpochDuration)
	const resumeEpochIdx int64 = 40000
	persistBook := true
	resumeScheduled := &db.MarketLifecycle{
		RunParams:         testMarketRunParams(),
		Market:            "dcr_btc",
		State:             db.MarketStateSuspended,
		StartEpochIdx:     resumeEpochIdx,
		StartEpochDur:     epochDur,
		PendingAction:     db.MarketPendingResume,
		PendingEpochIdx:   resumeEpochIdx,
		PendingEpochDur:   epochDur,
		PersistBook:       &persistBook,
		ProcessedEpochIdx: resumeEpochIdx - 10,
	}
	seedMarketLifecycle(t, resumeScheduled)

	// The revoke list includes a booked order, an already-revoked order, and
	// an unknown order. A booked order outside the list must be retained.
	selectedBooked := newLimitOrder(false, 4_900_000, 1, order.StandingTiF, 0)
	alreadyRevoked := newLimitOrder(true, 5_100_000, 1, order.StandingTiF, 10)
	neverStored := newLimitOrder(false, 4_800_000, 1, order.StandingTiF, 20)
	unselectedBooked := newLimitOrder(true, 5_200_000, 1, order.StandingTiF, 30)
	for _, lo := range []*order.LimitOrder{selectedBooked, alreadyRevoked, unselectedBooked} {
		if err := storeOrderForTest(archie, lo, resumeEpochIdx-25, epochDur, order.OrderStatusBooked); err != nil {
			t.Fatalf("store order %v: %v", lo.ID(), err)
		}
	}
	if _, _, err := archie.RevokeOrderUncounted(alreadyRevoked); err != nil {
		t.Fatalf("revoke order %v: %v", alreadyRevoked.ID(), err)
	}

	newRunParams := resumeScheduled.RunParams
	newRunParams.ParcelSize *= 2
	timestamp := time.UnixMilli(resumeEpochIdx * epochDur).UTC()
	update := &db.MarketResumedUpdate{
		Market:        "dcr_btc",
		Base:          AssetDCR,
		Quote:         AssetBTC,
		StartEpochIdx: resumeEpochIdx,
		EpochDur:      epochDur,
		Timestamp:     timestamp,
		RunParams:     newRunParams,
		ResumeRevokes: []*db.StartupOrderRevoke{
			{Order: selectedBooked, Reason: meshevents.StartupOrderRevokeReasonFundingCoinSpent},
			{Order: alreadyRevoked, Reason: meshevents.StartupOrderRevokeReasonFundingCoinSpent},
			{Order: neverStored, Reason: meshevents.StartupOrderRevokeReasonFundingCoinSpent},
		},
	}
	result, err := archie.ApplyMarketResumedEvent(ctx, &db.EventLogMeta{Event: []byte("ml-resume")}, update)
	if err != nil {
		t.Fatalf("ApplyMarketResumedEvent error: %v", err)
	}

	if result.Log.Kind != meshevents.EventKindMarketResumed {
		t.Fatalf("event kind = %q, want %q", result.Log.Kind, meshevents.EventKindMarketResumed)
	}

	wantLifecycle := &db.MarketLifecycle{
		Market:            "dcr_btc",
		State:             db.MarketStateRunning,
		StartEpochIdx:     resumeEpochIdx,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    resumeEpochIdx,
		ProcessedEpochIdx: resumeEpochIdx - 1,
		RunParams:         newRunParams,
	}
	if !reflect.DeepEqual(result.Lifecycle, wantLifecycle) {
		t.Fatalf("returned lifecycle = %+v, want %+v", result.Lifecycle, wantLifecycle)
	}
	stored, err := archie.MarketLifecycle(update.Market)
	if err != nil {
		t.Fatalf("MarketLifecycle: %v", err)
	}
	if !reflect.DeepEqual(stored, wantLifecycle) {
		t.Fatalf("stored lifecycle = %+v, want %+v", stored, wantLifecycle)
	}

	if len(result.ResumeRevokes) != 1 || result.ResumeRevokes[0].Order.ID() != selectedBooked.ID() {
		t.Fatalf("applied resume revokes = %+v, want only %v", result.ResumeRevokes, selectedBooked.ID())
	}
	for _, tc := range []struct {
		name       string
		ord        *order.LimitOrder
		wantStatus order.OrderStatus
	}{
		{name: "selected", ord: selectedBooked, wantStatus: order.OrderStatusRevoked},
		{name: "already revoked", ord: alreadyRevoked, wantStatus: order.OrderStatusRevoked},
		{name: "unselected", ord: unselectedBooked, wantStatus: order.OrderStatusBooked},
	} {
		if _, status, err := archie.Order(tc.ord.ID(), AssetDCR, AssetBTC); err != nil || status != tc.wantStatus {
			t.Fatalf("%s order %v status = %v (err %v), want %v", tc.name, tc.ord.ID(), status, err, tc.wantStatus)
		}
	}
	if _, _, err := archie.Order(neverStored.ID(), AssetDCR, AssetBTC); !db.IsErrOrderUnknown(err) {
		t.Fatalf("order %v lookup error = %v, want ErrUnknownOrder", neverStored.ID(), err)
	}
}
