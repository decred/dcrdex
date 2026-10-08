// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"

	"decred.org/dcrdex/server/meshevents"
)

// MaxUnprocessedClosedEpochs limits how many closed epochs may be waiting
// for processing after an epoch closes.
const MaxUnprocessedClosedEpochs = 2

// ProjectMarketSuspendScheduled returns the lifecycle after applying the event.
func ProjectMarketSuspendScheduled(prev *MarketLifecycle, update *MarketSuspendScheduledUpdate) (*MarketLifecycle, error) {
	if update == nil {
		return nil, fmt.Errorf("nil market_suspend_scheduled update")
	}
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", update.Market)
	}
	if prev.State != MarketStateRunning {
		return nil, fmt.Errorf("cannot schedule suspend for market %s in state %d",
			update.Market, prev.State)
	}
	next := *prev
	next.FinalEpochIdx = update.FinalEpochIdx
	next.FinalEpochDur = update.EpochDur
	next.PendingAction = MarketPendingSuspend
	next.PendingEpochIdx = update.FinalEpochIdx
	next.PendingEpochDur = update.EpochDur
	persist := update.PersistBook
	next.PersistBook = &persist
	return &next, nil
}

// ProjectMarketSuspended returns the lifecycle after applying a market_suspended
// event. The market must be draining and its final epoch must be processed.
func ProjectMarketSuspended(prev *MarketLifecycle, update *MarketSuspendedUpdate) (*MarketLifecycle, error) {
	if update == nil {
		return nil, fmt.Errorf("nil market_suspended update")
	}
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", update.Market)
	}
	if prev.State != MarketStateDraining {
		return nil, fmt.Errorf("cannot suspend market %s in state %d",
			update.Market, prev.State)
	}
	if !sameLifecycleEpoch(prev.FinalEpochIdx, prev.FinalEpochDur, update.FinalEpochIdx, update.EpochDur) {
		return nil, fmt.Errorf("suspend final epoch mismatch for market %s", update.Market)
	}
	if prev.ProcessedEpochIdx != prev.FinalEpochIdx {
		return nil, fmt.Errorf("cannot suspend market %s: final epoch %d is not processed (last processed epoch %d)",
			update.Market, prev.FinalEpochIdx, prev.ProcessedEpochIdx)
	}
	if prev.PersistBook == nil {
		return nil, fmt.Errorf("suspend missing persist_book")
	}
	next := *prev
	next.State = MarketStateSuspended
	next.PendingAction = MarketPendingNone
	next.PendingEpochIdx = 0
	next.PendingEpochDur = 0
	next.ActiveEpochIdx = 0
	return &next, nil
}

// ProjectMarketResumeScheduled returns the lifecycle after applying the event.
func ProjectMarketResumeScheduled(prev *MarketLifecycle, update *MarketResumeScheduledUpdate) (*MarketLifecycle, error) {
	if update == nil {
		return nil, fmt.Errorf("nil market_resume_scheduled update")
	}
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", update.Market)
	}
	if prev.State != MarketStateSuspended {
		return nil, fmt.Errorf("cannot schedule resume for market %s in state %d",
			update.Market, prev.State)
	}
	if prev.PersistBook == nil {
		return nil, fmt.Errorf("schedule_resume missing persisted persist_book")
	}
	next := *prev
	next.StartEpochIdx = update.StartEpochIdx
	next.StartEpochDur = update.EpochDur
	next.FinalEpochIdx = 0
	next.FinalEpochDur = 0
	next.PendingAction = MarketPendingResume
	next.PendingEpochIdx = update.StartEpochIdx
	next.PendingEpochDur = update.EpochDur
	return &next, nil
}

// ProjectMarketResumed returns the lifecycle after applying the event.
func ProjectMarketResumed(prev *MarketLifecycle, update *MarketResumedUpdate) (*MarketLifecycle, error) {
	if update == nil {
		return nil, fmt.Errorf("nil market_resumed update")
	}
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", update.Market)
	}
	if prev.State != MarketStateSuspended || prev.PendingAction != MarketPendingResume {
		return nil, fmt.Errorf("cannot resume market %s in lifecycle %d/%d",
			update.Market, prev.State, prev.PendingAction)
	}
	if !sameLifecycleEpoch(prev.PendingEpochIdx, prev.PendingEpochDur, update.StartEpochIdx, update.EpochDur) {
		return nil, fmt.Errorf("resume pending epoch mismatch for market %s", update.Market)
	}
	if err := update.RunParams.Validate(); err != nil {
		return nil, fmt.Errorf("resume for market %s: %w", update.Market, err)
	}
	// If resumption is late, start in the epoch containing the event timestamp.
	openEpochIdx := max(prev.PendingEpochIdx, update.Timestamp.UnixMilli()/prev.PendingEpochDur)
	return &MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateRunning,
		StartEpochIdx:     openEpochIdx,
		StartEpochDur:     update.EpochDur,
		PendingAction:     MarketPendingNone,
		ActiveEpochIdx:    openEpochIdx,
		ProcessedEpochIdx: openEpochIdx - 1,
		RunParams:         update.RunParams,
	}, nil
}

// ProjectMarketStartedLifecycle returns the lifecycle state after startup
// recovery. It performs no storage I/O. changed is false when no update is needed.
func ProjectMarketStartedLifecycle(prev *MarketLifecycle, update *MarketStartedUpdate) (next *MarketLifecycle, changed bool, err error) {
	if update == nil {
		return nil, false, fmt.Errorf("nil market started update")
	}

	// A market's first start creates its lifecycle state.
	if prev == nil {
		return newRunningMarketLifecycle(update), true, nil
	}

	// Startup repair revokes leftover epoch orders, so a draining market can
	// finish suspension without processing those abandoned epochs.
	if prev.State == MarketStateDraining {
		// Trading remains stopped, so keep the existing trading parameters.
		if update.EpochDur != prev.StartEpochDur || update.RunParams != prev.RunParams {
			return nil, false, fmt.Errorf("market_started must carry the run's pinned parameters while market %s drains", update.Market)
		}
		if prev.ProcessedEpochIdx == prev.FinalEpochIdx {
			return prev, false, nil
		}
		next := *prev
		next.ProcessedEpochIdx = prev.FinalEpochIdx
		return &next, true, nil
	}

	// If the state is not running or draining, it means it is stopped. This
	// state requires a market resume rather than a market started event.
	if prev.State != MarketStateRunning {
		return nil, false, fmt.Errorf("market_started for market %s in state %d", update.Market, prev.State)
	}

	// Startup revokes leftover epoch orders. Set ProcessedEpochIdx to the epoch
	// before trading restarts, or to the final epoch if suspension is due.
	switch prev.PendingAction {
	case MarketPendingNone:
		return newRunningMarketLifecycle(update), true, nil
	case MarketPendingSuspend:
		// Keep the epoch duration used to schedule the suspension.
		if update.EpochDur != prev.PendingEpochDur {
			return nil, false, fmt.Errorf("market_started duration %d mismatches pending suspend duration %d for market %s; "+
				"the epoch duration cannot change while a suspend is pending",
				update.EpochDur, prev.PendingEpochDur, update.Market)
		}

		if update.CurrentEpochIdx > prev.PendingEpochIdx {
			// The final trading epoch passed while the server was down.
			// Enter draining without reopening trading or changing parameters.
			if update.RunParams != prev.RunParams {
				return nil, false, fmt.Errorf("market_started must carry the run's pinned parameters while market %s completes its suspend", update.Market)
			}
			next := *prev
			next.State = MarketStateDraining
			next.PendingAction = MarketPendingNone
			next.PendingEpochIdx = 0
			next.PendingEpochDur = 0
			next.ActiveEpochIdx = 0
			next.ProcessedEpochIdx = prev.FinalEpochIdx
			return &next, true, nil
		}

		// Restart trading at the current epoch and keep the scheduled suspension.
		next := *prev
		next.ActiveEpochIdx = update.CurrentEpochIdx
		next.ProcessedEpochIdx = update.CurrentEpochIdx - 1
		next.RunParams = update.RunParams
		return &next, true, nil
	default:
		// If the pending action is resume, it means the market was in stopped state,
		// and we would have errored above already.
		return nil, false, fmt.Errorf("market %s lifecycle row has invalid state/action %d/%d",
			update.Market, prev.State, prev.PendingAction)
	}
}

// ProjectAdvanceEpochLifecycle returns the lifecycle state after closing an
// epoch. It opens the next epoch, or enters MarketStateDraining when the
// final epoch of a scheduled suspension closes.
func ProjectAdvanceEpochLifecycle(prev *MarketLifecycle, event *meshevents.AdvanceEpochEvent) (*MarketLifecycle, error) {
	if err := event.Validate(); err != nil {
		return nil, err
	}
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", event.Market)
	}
	if prev.State != MarketStateRunning {
		return nil, fmt.Errorf("advance_epoch for non-running market %s", event.Market)
	}
	if event.EpochDur != prev.StartEpochDur {
		return nil, fmt.Errorf("advance_epoch duration %d mismatches run duration %d for market %s",
			event.EpochDur, prev.StartEpochDur, event.Market)
	}
	if event.ClosedEpochIdx != prev.ActiveEpochIdx {
		return nil, fmt.Errorf("advance_epoch closed epoch %d does not match active epoch cursor %d for market %s",
			event.ClosedEpochIdx, prev.ActiveEpochIdx, event.Market)
	}
	if prev.ProcessedEpochIdx < event.ClosedEpochIdx-MaxUnprocessedClosedEpochs {
		return nil, fmt.Errorf("advance_epoch closing epoch %d would exceed %d unprocessed epochs (last processed epoch %d) for market %s",
			event.ClosedEpochIdx, MaxUnprocessedClosedEpochs, prev.ProcessedEpochIdx, event.Market)
	}
	switch prev.PendingAction {
	case MarketPendingNone:
		if event.OpenedEpochIdx <= 0 {
			return nil, fmt.Errorf("advance_epoch opened epoch must be positive for market %s", event.Market)
		}
	case MarketPendingSuspend:
		if event.OpenedEpochIdx == 0 {
			if !sameLifecycleEpoch(prev.PendingEpochIdx, prev.PendingEpochDur, event.ClosedEpochIdx, event.EpochDur) {
				return nil, fmt.Errorf("final-close epoch mismatch for market %s", event.Market)
			}
			next := *prev
			next.State = MarketStateDraining
			next.PendingAction = MarketPendingNone
			next.PendingEpochIdx = 0
			next.PendingEpochDur = 0
			next.ActiveEpochIdx = 0
			return &next, nil
		}
		if event.ClosedEpochIdx >= prev.PendingEpochIdx || event.OpenedEpochIdx > prev.PendingEpochIdx {
			return nil, fmt.Errorf("advance_epoch crosses pending suspend final epoch for market %s", event.Market)
		}
	default:
		return nil, fmt.Errorf("advance_epoch rejected for market %s lifecycle pending action %d",
			event.Market, prev.PendingAction)
	}
	next := *prev
	next.ActiveEpochIdx = event.OpenedEpochIdx
	return &next, nil
}

// ProjectEpochProcessedLifecycle returns the lifecycle state after processing
// an epoch. The epoch must immediately follow the last processed epoch and
// must already be closed.
func ProjectEpochProcessedLifecycle(prev *MarketLifecycle, market string, epochIdx, epochDur int64) (*MarketLifecycle, error) {
	if prev == nil {
		return nil, fmt.Errorf("missing lifecycle row for market %s", market)
	}
	var lastClosed int64
	switch prev.State {
	case MarketStateRunning:
		if prev.PendingAction != MarketPendingNone && prev.PendingAction != MarketPendingSuspend {
			return nil, fmt.Errorf("epoch_processed rejected for market %s lifecycle pending action %d",
				prev.Market, prev.PendingAction)
		}
		lastClosed = prev.ActiveEpochIdx - 1
	case MarketStateDraining:
		lastClosed = prev.FinalEpochIdx
	default:
		return nil, fmt.Errorf("epoch_processed for inactive market %s", market)
	}
	if epochDur != prev.StartEpochDur {
		return nil, fmt.Errorf("epoch_processed duration %d mismatches run duration %d for market %s",
			epochDur, prev.StartEpochDur, market)
	}
	if epochIdx != prev.ProcessedEpochIdx+1 {
		return nil, fmt.Errorf("epoch_processed epoch %d does not follow last processed epoch %d for market %s",
			epochIdx, prev.ProcessedEpochIdx, market)
	}
	if epochIdx > lastClosed {
		return nil, fmt.Errorf("epoch_processed epoch %d is not closed (last closed epoch %d) for market %s",
			epochIdx, lastClosed, market)
	}
	if prev.State == MarketStateDraining &&
		epochIdx == prev.FinalEpochIdx &&
		epochDur != prev.FinalEpochDur {
		return nil, fmt.Errorf("epoch_processed duration %d mismatches final epoch duration %d for market %s",
			epochDur, prev.FinalEpochDur, market)
	}
	next := *prev
	next.ProcessedEpochIdx = epochIdx
	return &next, nil
}

func newRunningMarketLifecycle(update *MarketStartedUpdate) *MarketLifecycle {
	return &MarketLifecycle{
		Market:            update.Market,
		State:             MarketStateRunning,
		StartEpochIdx:     update.CurrentEpochIdx,
		StartEpochDur:     update.EpochDur,
		PendingAction:     MarketPendingNone,
		ActiveEpochIdx:    update.CurrentEpochIdx,
		ProcessedEpochIdx: update.CurrentEpochIdx - 1,
		RunParams:         update.RunParams,
	}
}

func sameLifecycleEpoch(idxA, durA, idxB, durB int64) bool {
	return idxA == idxB && durA == durB
}
