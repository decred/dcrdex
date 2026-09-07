// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
)

// workerSupervisor starts master workers once in registration order.
type workerSupervisor struct {
	workers      []MasterWorker
	log          dex.Logger
	ensureLoaded func(context.Context) error
	onReadiness  func(context.Context, error)

	startCh chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc
}

// newWorkerSupervisor creates a supervisor that waits for a start request.
func newWorkerSupervisor(workers []MasterWorker, log dex.Logger,
	ensureLoaded func(context.Context) error, onReadiness func(context.Context, error)) *workerSupervisor {
	return &workerSupervisor{
		workers:      workers,
		log:          log,
		ensureLoaded: ensureLoaded,
		onReadiness:  onReadiness,
		startCh:      make(chan struct{}, 1),
		done:         make(chan struct{}),
	}
}

// startWorkers requests worker startup without waiting.
// Repeated requests do not restart workers.
func (ws *workerSupervisor) startWorkers() {
	select {
	case ws.startCh <- struct{}{}:
	default:
	}
}

// stopWorkers cancels workers and waits until all have exited.
func (ws *workerSupervisor) stopWorkers() {
	ws.cancel()
	<-ws.done
}

// startSupervisor starts a goroutine that waits for a worker startup request.
// It must be called exactly once, before stopWorkers.
func (ws *workerSupervisor) startSupervisor(lifeCtx context.Context, wg *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(lifeCtx)
	ws.cancel = cancel
	wg.Add(1)
	go func() {
		defer wg.Done()
		ws.run(ctx)
	}()
}

// run waits for a start request and loads state before starting workers.
// It waits for each startup result before starting the next worker.
// It reports the group result and waits for all started workers to exit.
func (ws *workerSupervisor) run(ctx context.Context) {
	defer close(ws.done)

	select {
	case <-ws.startCh:
	case <-ctx.Done():
		return
	}

	var workers sync.WaitGroup
	defer workers.Wait()

	if err := ws.ensureLoaded(ctx); err != nil {
		ws.onReadiness(ctx, err)
		return
	}

	for _, worker := range ws.workers {
		if err := ctx.Err(); err != nil {
			ws.onReadiness(ctx, err)
			return
		}
		workerReady := ws.startWorker(ctx, &workers, worker)
		if err := waitWorkerReady(ctx, worker.Name, workerReady); err != nil {
			ws.onReadiness(ctx, err)
			return
		}
	}

	ws.onReadiness(ctx, nil)
}

// startWorker starts a worker and returns its startup result.
// The first readiness report sets the result.
// A return before that report sets a failure or the context error.
func (ws *workerSupervisor) startWorker(ctx context.Context, workers *sync.WaitGroup, worker MasterWorker) *readiness {
	ready := newReadiness()
	workers.Add(1)
	go func() {
		defer workers.Done()
		ws.log.Infof("Starting mesh master worker %q.", worker.Name)
		worker.Run(ctx, ready.resolve)
		if err := ctx.Err(); err != nil {
			ready.resolve(err)
		} else {
			ready.resolve(fmt.Errorf("returned before reporting startup readiness"))
		}
		ws.log.Infof("Mesh master worker %q stopped.", worker.Name)
	}()
	return ready
}

// waitWorkerReady waits for startup readiness and adds the worker name to
// failures.
// If the wait fails after cancellation, it returns the context error.
func waitWorkerReady(ctx context.Context, workerName string, workerReady *readiness) error {
	if err := workerReady.wait(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("mesh master worker %q startup readiness failed: %w", workerName, err)
	}
	return nil
}
