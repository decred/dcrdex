// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/server/meshevents"
)

func testRunParams() meshevents.MarketRunParams {
	return meshevents.MarketRunParams{
		LotSize:                100_000,
		RateStep:               1000,
		ParcelSize:             10,
		MaxUserCancelsPerEpoch: 2,
		MinimumRate:            500,
	}
}

func TestProjectMarketSuspendScheduled(t *testing.T) {
	const epochDur int64 = 10_000
	update := &MarketSuspendScheduledUpdate{
		Market:        "dcr_btc",
		FinalEpochIdx: 20,
		EpochDur:      epochDur,
		PersistBook:   true,
	}
	running := MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateRunning,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    15,
		ProcessedEpochIdx: 14,
		RunParams:         testRunParams(),
	}
	scheduled := running
	scheduled.FinalEpochIdx = 20
	scheduled.FinalEpochDur = epochDur
	scheduled.PendingAction = MarketPendingSuspend
	scheduled.PendingEpochIdx = 20
	scheduled.PendingEpochDur = epochDur
	scheduled.PersistBook = &update.PersistBook

	draining := running
	draining.State = MarketStateDraining
	for _, tc := range []struct {
		name    string
		prev    *MarketLifecycle
		update  *MarketSuspendScheduledUpdate
		want    *MarketLifecycle
		wantErr bool
	}{
		{
			name: "running market", prev: &running, update: update,
			want: &scheduled,
		},
		{
			name: "draining market", prev: &draining, update: update,
			wantErr: true,
		},
		{
			name: "missing lifecycle", update: update,
			wantErr: true,
		},
		{
			name: "missing update", prev: &running,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectMarketSuspendScheduled(tc.prev, tc.update)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lifecycle = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProjectMarketSuspended(t *testing.T) {
	const epochDur int64 = 10_000
	persistBook := true
	update := &MarketSuspendedUpdate{
		Market:        "dcr_btc",
		FinalEpochIdx: 20,
		EpochDur:      epochDur,
	}
	draining := MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateDraining,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		FinalEpochIdx:     20,
		FinalEpochDur:     epochDur,
		ProcessedEpochIdx: 20,
		PersistBook:       &persistBook,
		RunParams:         testRunParams(),
	}

	suspended := draining
	suspended.State = MarketStateSuspended

	unprocessedFinalEpoch := draining
	unprocessedFinalEpoch.ProcessedEpochIdx = 19
	running := MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateRunning,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    19,
		ProcessedEpochIdx: 18,
		FinalEpochIdx:     20,
		FinalEpochDur:     epochDur,
		PendingAction:     MarketPendingSuspend,
		PendingEpochIdx:   20,
		PendingEpochDur:   epochDur,
		PersistBook:       &persistBook,
		RunParams:         testRunParams(),
	}

	for _, tc := range []struct {
		name    string
		prev    *MarketLifecycle
		update  *MarketSuspendedUpdate
		want    *MarketLifecycle
		wantErr bool
	}{
		{
			name: "final epoch processed", prev: &draining, update: update,
			want: &suspended,
		},
		{
			name: "unprocessed final epoch", prev: &unprocessedFinalEpoch, update: update,
			wantErr: true,
		},
		{
			name: "running market", prev: &running, update: update,
			wantErr: true,
		},
		{
			name: "missing lifecycle", update: update,
			wantErr: true,
		},
		{
			name: "missing update", prev: &draining,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectMarketSuspended(tc.prev, tc.update)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lifecycle = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProjectMarketResumeScheduled(t *testing.T) {
	const epochDur int64 = 10_000
	persistBook := true
	update := &MarketResumeScheduledUpdate{
		Market:        "dcr_btc",
		StartEpochIdx: 30,
		EpochDur:      epochDur,
	}
	suspended := MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateSuspended,
		StartEpochIdx:     10,
		StartEpochDur:     epochDur,
		FinalEpochIdx:     20,
		FinalEpochDur:     epochDur,
		ProcessedEpochIdx: 20,
		PersistBook:       &persistBook,
		RunParams:         testRunParams(),
	}

	resumeScheduled := suspended
	resumeScheduled.StartEpochIdx = 30
	resumeScheduled.FinalEpochIdx = 0
	resumeScheduled.FinalEpochDur = 0
	resumeScheduled.PendingAction = MarketPendingResume
	resumeScheduled.PendingEpochIdx = 30
	resumeScheduled.PendingEpochDur = epochDur

	draining := suspended
	draining.State = MarketStateDraining

	for _, tc := range []struct {
		name    string
		prev    *MarketLifecycle
		update  *MarketResumeScheduledUpdate
		want    *MarketLifecycle
		wantErr bool
	}{
		{
			name: "schedule resume", prev: &suspended, update: update,
			want: &resumeScheduled,
		},
		{
			name: "market still draining", prev: &draining, update: update,
			wantErr: true,
		},
		{
			name: "missing lifecycle", update: update,
			wantErr: true,
		},
		{
			name: "missing update", prev: &suspended,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectMarketResumeScheduled(tc.prev, tc.update)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lifecycle = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProjectMarketResumed(t *testing.T) {
	const epochDur int64 = 10_000
	persistBook := true
	resumeScheduled := MarketLifecycle{
		Market:            "dcr_btc",
		State:             MarketStateSuspended,
		StartEpochIdx:     30,
		StartEpochDur:     epochDur,
		PendingAction:     MarketPendingResume,
		PendingEpochIdx:   30,
		PendingEpochDur:   epochDur,
		ProcessedEpochIdx: 20,
		PersistBook:       &persistBook,
		RunParams:         testRunParams(),
	}
	newRunParams := resumeScheduled.RunParams
	newRunParams.LotSize *= 2
	update := &MarketResumedUpdate{
		Market:        resumeScheduled.Market,
		StartEpochIdx: 30,
		EpochDur:      epochDur,
		Timestamp:     time.UnixMilli(30 * epochDur),
		RunParams:     newRunParams,
	}
	resumed := MarketLifecycle{
		Market:            resumeScheduled.Market,
		State:             MarketStateRunning,
		StartEpochIdx:     30,
		StartEpochDur:     epochDur,
		ActiveEpochIdx:    30,
		ProcessedEpochIdx: 29,
		RunParams:         newRunParams,
	}

	missingParams := *update
	missingParams.RunParams = meshevents.MarketRunParams{}

	// A late resumption opens the epoch containing the event timestamp.
	lateUpdate := *update
	lateUpdate.Timestamp = time.UnixMilli(32 * epochDur)
	resumedLate := resumed
	resumedLate.StartEpochIdx = 32
	resumedLate.ActiveEpochIdx = 32
	resumedLate.ProcessedEpochIdx = 31

	for _, tc := range []struct {
		name    string
		prev    *MarketLifecycle
		update  *MarketResumedUpdate
		want    *MarketLifecycle
		wantErr bool
	}{
		{
			name: "resume at scheduled epoch", prev: &resumeScheduled, update: update,
			want: &resumed,
		},
		{
			name: "resume after scheduled epoch", prev: &resumeScheduled, update: &lateUpdate,
			want: &resumedLate,
		},
		{
			name: "missing run parameters", prev: &resumeScheduled, update: &missingParams,
			wantErr: true,
		},
		{
			name: "missing lifecycle", update: update,
			wantErr: true,
		},
		{
			name: "missing update", prev: &resumeScheduled,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProjectMarketResumed(tc.prev, tc.update)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lifecycle = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProjectMarketStartedLifecycle(t *testing.T) {
	persist := true
	const market = "dcr_btc"
	const dur int64 = 10_000
	params := testRunParams()
	update := &MarketStartedUpdate{Market: market, CurrentEpochIdx: 25, EpochDur: dur, RunParams: params}
	running := MarketLifecycle{
		Market: market, State: MarketStateRunning, StartEpochIdx: 10, StartEpochDur: dur,
		ActiveEpochIdx: 19, ProcessedEpochIdx: 18, RunParams: params,
	}
	running.RunParams.LotSize *= 2 // Startup may change the trading parameters.
	pending := running
	pending.FinalEpochIdx, pending.FinalEpochDur = 25, dur
	pending.PendingAction = MarketPendingSuspend
	pending.PendingEpochIdx, pending.PendingEpochDur = 25, dur
	pending.PersistBook = &persist
	pastFinal := pending
	pastFinal.FinalEpochIdx, pastFinal.PendingEpochIdx = 20, 20
	pastFinal.RunParams = params
	draining := MarketLifecycle{
		Market: market, State: MarketStateDraining, StartEpochIdx: 10, StartEpochDur: dur,
		FinalEpochIdx: 20, FinalEpochDur: dur, PersistBook: &persist,
		ProcessedEpochIdx: 19, RunParams: params,
	}
	wantRunning := MarketLifecycle{
		Market: market, State: MarketStateRunning, StartEpochIdx: 25, StartEpochDur: dur,
		ActiveEpochIdx: 25, ProcessedEpochIdx: 24, RunParams: params,
	}
	wantPending := pending
	wantPending.ActiveEpochIdx, wantPending.ProcessedEpochIdx = 25, 24
	wantPending.RunParams = params
	wantDraining := draining
	wantDraining.ProcessedEpochIdx = 20

	for _, tt := range []struct {
		name    string
		prev    *MarketLifecycle
		want    MarketLifecycle
		changed bool
	}{
		{"first start", nil, wantRunning, true},
		{"running restart", &running, wantRunning, true},
		{"restart in final epoch keeps suspension", &pending, wantPending, true},
		{"restart after final epoch starts draining", &pastFinal, wantDraining, true},
		{"draining restart resolves abandoned epochs", &draining, wantDraining, true},
		{"draining restart with no abandoned epochs", &wantDraining, wantDraining, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next, changed, err := ProjectMarketStartedLifecycle(tt.prev, update)
			if err != nil {
				t.Fatal(err)
			}
			if next == nil || !reflect.DeepEqual(*next, tt.want) || changed != tt.changed {
				t.Fatalf("got lifecycle %+v, changed %v; want %+v, changed %v", next, changed, tt.want, tt.changed)
			}
		})
	}

	for _, tt := range []struct {
		name   string
		prev   MarketLifecycle
		mutate func(*MarketStartedUpdate)
	}{
		{"pending suspension rejects duration change", pending, func(u *MarketStartedUpdate) { u.EpochDur *= 2 }},
		{"draining rejects duration change", draining, func(u *MarketStartedUpdate) { u.EpochDur *= 2 }},
		{"draining rejects parameter change", wantDraining, func(u *MarketStartedUpdate) { u.RunParams.LotSize *= 2 }},
		{"passed suspension rejects parameter change", pastFinal, func(u *MarketStartedUpdate) { u.RunParams.LotSize *= 2 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u := *update
			tt.mutate(&u)
			if _, _, err := ProjectMarketStartedLifecycle(&tt.prev, &u); err == nil {
				t.Fatal("expected invalid startup parameters to be rejected")
			}
		})
	}
	t.Run("rejects suspended", func(t *testing.T) {
		suspended := wantDraining
		suspended.State = MarketStateSuspended
		if _, _, err := ProjectMarketStartedLifecycle(&suspended, update); err == nil {
			t.Fatal("expected suspended market startup to be rejected")
		}
	})
}

func TestProjectAdvanceEpochLifecycle(t *testing.T) {
	persist := true
	const market = "dcr_btc"
	const dur int64 = 10_000

	running := func(active int64) *MarketLifecycle {
		return &MarketLifecycle{
			Market:            market,
			State:             MarketStateRunning,
			StartEpochIdx:     10,
			StartEpochDur:     dur,
			PendingAction:     MarketPendingNone,
			ActiveEpochIdx:    active,
			ProcessedEpochIdx: active - 1,
		}
	}

	checkAdvance := func(t *testing.T, prev *MarketLifecycle, update *meshevents.AdvanceEpochEvent, want *MarketLifecycle) {
		t.Helper()
		before := *prev
		next, err := ProjectAdvanceEpochLifecycle(prev, update)
		if err != nil {
			t.Fatalf("ProjectAdvanceEpochLifecycle: %v", err)
		}
		if !reflect.DeepEqual(next, want) {
			t.Fatalf("lifecycle = %+v, want %+v", next, want)
		}
		if !reflect.DeepEqual(*prev, before) {
			t.Fatal("projection modified the previous lifecycle")
		}
	}

	for _, test := range []struct {
		name          string
		pendingFinal  int64
		active        int64
		opened        int64
		processingLag int64
	}{
		{"normal advance", 0, 15, 16, 1},
		{"pending suspension", 20, 15, 16, 1},
		{"final close", 20, 20, 0, 1},
		{"maximum processing lag", 0, 15, 16, MaxUnprocessedClosedEpochs},
	} {
		t.Run(test.name, func(t *testing.T) {
			prev := running(test.active)
			prev.ProcessedEpochIdx = test.active - test.processingLag
			if test.pendingFinal != 0 {
				prev.PendingAction = MarketPendingSuspend
				prev.PendingEpochIdx, prev.PendingEpochDur = test.pendingFinal, dur
				prev.FinalEpochIdx, prev.FinalEpochDur = test.pendingFinal, dur
				prev.PersistBook = &persist
			}
			want := *prev
			want.ActiveEpochIdx = test.opened
			if test.opened == 0 {
				want.State = MarketStateDraining
				want.PendingAction = MarketPendingNone
				want.PendingEpochIdx, want.PendingEpochDur = 0, 0
			}
			checkAdvance(t, prev, &meshevents.AdvanceEpochEvent{
				Market: market, ClosedEpochIdx: test.active, OpenedEpochIdx: test.opened, EpochDur: dur,
			}, &want)
		})
	}

	for _, test := range []struct {
		name   string
		change func(*MarketLifecycle, *meshevents.AdvanceEpochEvent)
	}{
		{"closed epoch mismatch", func(_ *MarketLifecycle, u *meshevents.AdvanceEpochEvent) {
			u.ClosedEpochIdx--
			u.OpenedEpochIdx--
		}},
		{"duration mismatch", func(_ *MarketLifecycle, u *meshevents.AdvanceEpochEvent) { u.EpochDur++ }},
		{"too many unprocessed epochs", func(lc *MarketLifecycle, _ *meshevents.AdvanceEpochEvent) {
			lc.ProcessedEpochIdx = lc.ActiveEpochIdx - MaxUnprocessedClosedEpochs - 1
		}},
		{"draining market", func(lc *MarketLifecycle, _ *meshevents.AdvanceEpochEvent) { lc.State = MarketStateDraining }},
		{"premature final close", func(_ *MarketLifecycle, u *meshevents.AdvanceEpochEvent) { u.OpenedEpochIdx = 0 }},
		{"advance past final epoch", func(lc *MarketLifecycle, _ *meshevents.AdvanceEpochEvent) {
			lc.PendingEpochIdx, lc.FinalEpochIdx = lc.ActiveEpochIdx, lc.ActiveEpochIdx
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			prev := running(15)
			prev.PendingAction = MarketPendingSuspend
			prev.PendingEpochIdx, prev.PendingEpochDur = 20, dur
			prev.FinalEpochIdx, prev.FinalEpochDur = 20, dur
			prev.PersistBook = &persist
			update := &meshevents.AdvanceEpochEvent{Market: market, ClosedEpochIdx: 15, OpenedEpochIdx: 16, EpochDur: dur}
			test.change(prev, update)
			if _, err := ProjectAdvanceEpochLifecycle(prev, update); err == nil {
				t.Fatal("expected advance to be rejected")
			}
		})
	}
}

func TestProjectEpochProcessedLifecycle(t *testing.T) {
	persist := true
	const market = "dcr_btc"
	const dur int64 = 10_000

	row := func(active, processed int64, pending MarketPendingAction) *MarketLifecycle {
		lc := &MarketLifecycle{
			Market:            market,
			State:             MarketStateRunning,
			StartEpochIdx:     10,
			StartEpochDur:     dur,
			PendingAction:     pending,
			ActiveEpochIdx:    active,
			ProcessedEpochIdx: processed,
		}
		if pending == MarketPendingSuspend {
			lc.FinalEpochIdx, lc.FinalEpochDur = 20, dur
			lc.PendingEpochIdx, lc.PendingEpochDur = 20, dur
			lc.PersistBook = &persist
		}
		return lc
	}

	draining := func(processed int64) *MarketLifecycle {
		lc := row(0, processed, MarketPendingNone)
		lc.State = MarketStateDraining
		lc.FinalEpochIdx, lc.FinalEpochDur = 20, dur
		lc.PersistBook = &persist
		return lc
	}

	tests := []struct {
		name          string
		prev          *MarketLifecycle
		epochIdx      int64
		epochDur      int64
		wantProcessed int64
		wantErr       bool
	}{
		{
			name: "processes the newest closed epoch",
			prev: row(16, 14, MarketPendingNone), epochIdx: 15, epochDur: dur,
			wantProcessed: 15,
		}, {
			name: "rejects skipping an epoch",
			prev: row(16, 13, MarketPendingNone), epochIdx: 15, epochDur: dur,
			wantErr: true,
		}, {
			name: "rejects the still-open epoch",
			prev: row(16, 15, MarketPendingNone), epochIdx: 16, epochDur: dur,
			wantErr: true,
		}, {
			name: "pending suspend allows processing before the final epoch",
			prev: row(19, 17, MarketPendingSuspend), epochIdx: 18, epochDur: dur,
			wantProcessed: 18,
		}, {
			name: "draining allows processing before the final epoch",
			prev: draining(18), epochIdx: 19, epochDur: dur,
			wantProcessed: 19,
		}, {
			name: "draining allows processing the final epoch",
			prev: draining(19), epochIdx: 20, epochDur: dur,
			wantProcessed: 20,
		}, {
			name: "draining rejects a duration mismatch",
			prev: draining(19), epochIdx: 20, epochDur: dur + 1,
			wantErr: true,
		}, {
			name: "drain rejects past the final epoch",
			prev: draining(20), epochIdx: 21, epochDur: dur,
			wantErr: true,
		}, {
			name: "rejects a suspended market",
			prev: func() *MarketLifecycle {
				lc := row(16, 15, MarketPendingNone)
				lc.State = MarketStateSuspended
				return lc
			}(), epochIdx: 16, epochDur: dur,
			wantErr: true,
		}, {
			name:    "rejects a missing row",
			prev:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, err := ProjectEpochProcessedLifecycle(tt.prev, market, tt.epochIdx, tt.epochDur)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected projection error, got %+v", next)
				}
				return
			}
			if err != nil {
				t.Fatalf("ProjectEpochProcessedLifecycle: %v", err)
			}
			if next.ProcessedEpochIdx != tt.wantProcessed {
				t.Fatalf("last processed epoch = %d, want %d", next.ProcessedEpochIdx, tt.wantProcessed)
			}
		})
	}
}
