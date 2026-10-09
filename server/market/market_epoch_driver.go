// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/mesh"
	"decred.org/dcrdex/server/meshevents"
)

// marketEpochDriver starts the market, advances epochs, and processes closed epochs.
type marketEpochDriver struct {
	m         *Market
	epochPump *epochPump
}

func newMarketEpochDriver(m *Market) *marketEpochDriver {
	return &marketEpochDriver{m: m}
}

// run reports the startup result, then drives epochs until cancellation or
// failure. Successful startup may leave the market suspended.
func (d *marketEpochDriver) run(parent context.Context, ready chan<- error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if err := d.prepareStartup(ctx); err != nil {
		log.Errorf("Market %q startup failed: %v", d.m.name, err)
		ready <- err
		return
	}

	d.epochPump = newEpochPump()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.epochPump.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		d.runEpochProcessing(ctx, cancel)
	}()

	ready <- nil
	if err := d.runEpochAdvancer(ctx); err != nil && !quietDriverStop(ctx, err) {
		log.Errorf("Market %s epoch advancer stopped: %v", d.m.name, err)
	}
	cancel()
	wg.Wait()
	d.markMarketStopped()
}

// prepareStartup applies startup changes and completes any overdue suspension.
// A market that is already suspended waits for its scheduled resume instead.
func (d *marketEpochDriver) prepareStartup(ctx context.Context) error {
	d.m.epochMtx.RLock()
	suspended := d.m.lifecycleState == db.MarketStateSuspended
	d.m.epochMtx.RUnlock()
	if suspended {
		log.Infof("Market %q is starting suspended; waiting for resume event.", d.m.name)
		return nil
	}
	if err := d.waitForChainSync(ctx); err != nil {
		return err
	}

	startupEpoch, err := d.m.submitMarketStarted(ctx)
	if err != nil {
		return err
	}
	if d.m.isDraining() {
		return d.m.submitMarketSuspend(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	log.Infof("Market %s now accepting orders, epoch %d:%d", d.m.name,
		startupEpoch, d.m.EpochDuration())
	return nil
}

// waitForChainSync checks both asset backends until they are synchronized,
// retrying at the next configured epoch boundary.
func (d *marketEpochDriver) waitForChainSync(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.checkStorageHealth(); err != nil {
			return err
		}

		synced, err := d.m.swapper.ChainsSynced(d.m.base, d.m.quote)
		if err == nil && synced {
			return nil
		}
		if err != nil {
			log.Errorf("Not starting %s market because of ChainsSynced error: %v", d.m.name, err)
		} else {
			log.Debugf("Delaying start of %s market because chains aren't synced", d.m.name)
		}
		epochDur := d.m.configuredParams.epochDur
		nextEpochStart := time.UnixMilli((time.Now().UnixMilli()/epochDur + 1) * epochDur)
		if err := d.waitUntil(ctx, nextEpochStart); err != nil {
			return err
		}
	}
}

// runEpochAdvancer opens and closes epochs at their scheduled boundaries,
// waiting for processing to catch up when needed. While suspended, it waits
// for and applies scheduled resumes.
func (d *marketEpochDriver) runEpochAdvancer(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.checkStorageHealth(); err != nil {
			return err
		}

		state := d.readEpochDriverState()
		// Suspended and draining markets have no next epoch. Wait for a
		// lifecycle change, or submit a scheduled resume when it is due.
		if state.nextEpoch == nil {
			var err error
			switch {
			case state.lifecycleState == db.MarketStateSuspended &&
				state.pendingAction == db.MarketPendingResume:
				err = d.runPendingResume(ctx, state.pendingEpochIdx, state.pendingEpochDur)

			case state.lifecycleState == db.MarketStateSuspended ||
				state.lifecycleState == db.MarketStateDraining:
				err = d.waitLifecycleWake(ctx)

			default:
				return fmt.Errorf("market %s has no next epoch while running", d.m.name)
			}
			if err != nil {
				return err
			}
			continue
		}

		if err := d.waitUntil(ctx, state.nextEpoch.Start); err != nil {
			return err
		}
		closingIdx := state.nextEpoch.Epoch - 1
		// Wait for processing to catch up before closing another epoch.
		if err := d.waitForEpochProcessing(ctx, closingIdx); err != nil {
			return err
		}
		if err := d.advanceEpoch(ctx); err != nil {
			return err
		}
	}
}

type epochDriverState struct {
	nextEpoch       *EpochQueue
	lifecycleState  db.MarketState
	pendingAction   db.MarketPendingAction
	pendingEpochIdx int64
	pendingEpochDur int64
}

func (d *marketEpochDriver) readEpochDriverState() epochDriverState {
	d.m.epochMtx.RLock()
	defer d.m.epochMtx.RUnlock()
	return epochDriverState{
		nextEpoch:       d.m.nextEpoch,
		lifecycleState:  d.m.lifecycleState,
		pendingAction:   d.m.pendingLifecycleAction,
		pendingEpochIdx: d.m.pendingLifecycleEpochIdx,
		pendingEpochDur: d.m.pendingLifecycleEpochDur,
	}
}

// runPendingResume waits for the scheduled start time and synchronized asset
// backends, then submits the resume. It returns early if a lifecycle change
// wakes the wait, allowing the caller to read the updated schedule.
func (d *marketEpochDriver) runPendingResume(ctx context.Context, pendingEpochIdx, pendingEpochDur int64) error {
	resumeTime := time.UnixMilli(pendingEpochIdx * pendingEpochDur)
	lifecycleChanged, err := d.waitUntilOrLifecycleWake(ctx, resumeTime)
	if err != nil {
		return err
	}
	if lifecycleChanged {
		return nil
	}
	if err := d.waitForChainSync(ctx); err != nil {
		return err
	}
	if err := d.m.submitMarketResume(ctx, pendingEpochIdx, pendingEpochDur); err != nil {
		if !d.m.hasPendingResume(pendingEpochIdx, pendingEpochDur) {
			log.Debugf("Market %s pending resume %d:%d changed before resume event applied.",
				d.m.name, pendingEpochIdx, pendingEpochDur)
			return nil
		}
		return fmt.Errorf("resume at epoch %d:%d failed: %w", pendingEpochIdx, pendingEpochDur, err)
	}
	return nil
}

// waitForEpochProcessing waits until closing the given epoch would leave no
// more than MaxUnprocessedClosedEpochs closed epochs awaiting processing.
func (d *marketEpochDriver) waitForEpochProcessing(ctx context.Context, closingIdx int64) error {
	requiredProcessedEpoch := closingIdx - db.MaxUnprocessedClosedEpochs
	if d.processedEpoch() >= requiredProcessedEpoch {
		return nil
	}
	log.Warnf("Market %s waiting for epoch %d to be processed before closing epoch %d (last processed %d)",
		d.m.name, requiredProcessedEpoch, closingIdx, d.processedEpoch())
	for {
		if d.processedEpoch() >= requiredProcessedEpoch {
			log.Infof("Market %s processed epoch %d; resuming epoch advancement.", d.m.name, d.processedEpoch())
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.m.closureWake:
		}
	}
}

func (d *marketEpochDriver) processedEpoch() int64 {
	d.m.epochMtx.RLock()
	defer d.m.epochMtx.RUnlock()
	return d.m.processedEpochIdx
}

// advanceEpoch closes the current epoch and queues it for preimage collection
// and processing after the event has been applied.
func (d *marketEpochDriver) advanceEpoch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	d.m.epochMtx.RLock()
	currentEpoch, nextEpoch := d.m.currentEpoch, d.m.nextEpoch
	if currentEpoch == nil || nextEpoch == nil {
		d.m.epochMtx.RUnlock()
		return fmt.Errorf("market %s cannot advance epoch before market_started", d.m.name)
	}
	closedEpochIdx := currentEpoch.Epoch
	openedEpochIdx := nextEpoch.Epoch
	epochDur := nextEpoch.Duration
	if d.m.pendingLifecycleAction == db.MarketPendingSuspend && closedEpochIdx == d.m.pendingLifecycleEpochIdx {
		// Close the final trading epoch without opening another while the market drains.
		openedEpochIdx = 0
	}
	d.m.epochMtx.RUnlock()

	payload := meshevents.NewAdvanceEpochEvent(
		d.m.name, closedEpochIdx, openedEpochIdx, epochDur,
	)
	event, err := mesh.NewEvent(payload)
	if err != nil {
		return fmt.Errorf("failed to build advance epoch event for epoch %d: %w", closedEpochIdx, err)
	}
	if _, err := d.m.mesh.ApplyEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to apply advance epoch event for epoch %d: %w", closedEpochIdx, err)
	}

	if !d.m.enqueueEpoch(d.epochPump, currentEpoch) {
		return fmt.Errorf("market %s failed to enqueue closed epoch %d", d.m.name, currentEpoch.Epoch)
	}
	return nil
}

// runEpochProcessing processes closed epochs after preimage collection and
// completes suspension after the final epoch. On failure, it cancels the
// driver and discards queued epochs so the epoch pump can finish.
func (d *marketEpochDriver) runEpochProcessing(ctx context.Context, stopDriver context.CancelFunc) {
	for epoch := range d.epochPump.ready {
		if err := d.m.processReadyEpoch(ctx, epoch); err != nil {
			if !quietDriverStop(ctx, err) {
				log.Criticalf("Stopping market %s run: epoch %d close failed: %v",
					d.m.name, epoch.Epoch, err)
			}
			stopDriver()
			break
		}

		// Complete suspension only after the final epoch has been processed.
		if !d.m.drainingFinalEpoch(epoch.Epoch, epoch.Duration) {
			continue
		}
		err := d.m.submitMarketSuspend(ctx)
		if err == nil {
			continue
		}
		if !d.m.isDraining() {
			// The market has already left the draining state despite the submit error.
			log.Debugf("Market %s suspend applied despite a submit error: %v", d.m.name, err)
			continue
		}
		if !quietDriverStop(ctx, err) {
			log.Errorf("Failed to apply market suspend event for market %s after final epoch %d: %v",
				d.m.name, epoch.Epoch, err)
		}
		stopDriver()
		break
	}

	// Consume the remaining epochs so the pump can finish after cancellation.
	for skipped := range d.epochPump.ready {
		log.Warnf("Skipping market %s epoch %d after an earlier pipeline failure.",
			d.m.name, skipped.Epoch)
	}
	log.Debugf("epoch pump drained for market %s", d.m.name)
}

func (d *marketEpochDriver) markMarketStopped() {
	d.m.running.Store(false)

	d.m.epochMtx.Lock()
	d.m.activeEpochIdx = 0
	d.m.epochMtx.Unlock()

	d.m.tasks.Wait()

	// Leave unfinished epoch orders for the next market_started event to revoke.
	log.Infof("Market %q stopped.", d.m.name)
}

func (d *marketEpochDriver) checkStorageHealth() error {
	if err := d.m.storage.LastErr(); err != nil {
		return fmt.Errorf("market storage failed: %w", err)
	}
	return nil
}

// quietDriverStop reports cancellation or mesh unavailability, which do not
// need an additional error log.
func quietDriverStop(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, mesh.ErrUnavailable)
}

// waitLifecycleWake waits for a lifecycle change or context cancellation.
func (d *marketEpochDriver) waitLifecycleWake(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.m.lifecycleWake:
		return nil
	}
}

// waitUntilOrLifecycleWake waits until at or a lifecycle change.
// It returns true if a lifecycle change interrupted the wait.
func (d *marketEpochDriver) waitUntilOrLifecycleWake(ctx context.Context, at time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	wait := time.Until(at)
	if wait <= 0 {
		return false, nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-d.m.lifecycleWake:
		return true, nil
	case <-timer.C:
		return false, ctx.Err()
	}
}

// waitUntil waits until at or context cancellation.
func (d *marketEpochDriver) waitUntil(ctx context.Context, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wait := time.Until(at)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
