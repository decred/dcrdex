// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package wait

import (
	"context"
	"sync"
	"time"
)

const (
	// These constants are used for a Waiter to inform the TickerQueue whether to
	// run again or not.
	TryAgain     = false
	DontTryAgain = true
)

// Waiter is a function to run every recheckInterval until completion or
// expiration. Completion is indicated when the TryFunc returns DontTryAgain.
// Expiration occurs when TryAgain is returned after Expiration time.
type Waiter struct {
	// Expiration time is checked after the function returns TryAgain. If the
	// current time > Expiration, ExpireFunc will be run and the waiter will be
	// un-queued.
	Expiration time.Time
	// TryFunc is the function to run every recheckInterval until DontTryAgain is
	// returned or Waiter expires.
	TryFunc func() bool
	// ExpireFunc is a function to run in the case that the Waiter expires.
	ExpireFunc func()
}

// TickerQueue is a Waiter manager that checks a function periodically until
// DontTryAgain is indicated.
type TickerQueue struct {
	waiterMtx       sync.RWMutex
	waiters         []*Waiter
	recheckInterval time.Duration
}

// NewTickerQueue is the constructor for a new TickerQueue.
func NewTickerQueue(recheckInterval time.Duration) *TickerQueue {
	return &TickerQueue{
		recheckInterval: recheckInterval,
		waiters:         make([]*Waiter, 0, 256),
	}
}

// Wait attempts to run the (*Waiter).TryFunc until either 1) the function
// returns the value DontTryAgain, or 2) the function's Expiration time has
// passed. In the case of 2, the (*Waiter).ExpireFunc will be run.
func (w *TickerQueue) Wait(settings *Waiter) {
	if time.Now().After(settings.Expiration) {
		log.Error("wait.TickerQueue: Waiter given expiration before present")
		return
	}
	// Check to see if it passes right away.
	if settings.TryFunc() {
		return
	}
	w.waiterMtx.Lock()
	w.waiters = append(w.waiters, settings)
	w.waiterMtx.Unlock()
}

// Run runs the primary wait loop until the context is canceled.
func (w *TickerQueue) Run(ctx context.Context) {
	// The latencyTicker triggers a check of all waitFunc functions.
	latencyTicker := time.NewTicker(w.recheckInterval)
	defer latencyTicker.Stop()

	runWaiters := func() {
		w.waiterMtx.Lock()
		defer w.waiterMtx.Unlock()
		agains := make([]*Waiter, 0)
		// Grab new waiters
		tNow := time.Now()
		for _, settings := range w.waiters {
			if ctx.Err() != nil {
				return
			}
			if !settings.TryFunc() {
				// If this waiter has expired, issue the timeout error to the client
				// and do not append to the agains slice.
				if settings.Expiration.Before(tNow) {
					settings.ExpireFunc()
					continue
				}
				agains = append(agains, settings)
			} // End if !mFunc.f(). Nothing to do if mFunc returned dontTryAgain=true.
		}
		w.waiters = agains
	}
out:
	for {
		select {
		case <-latencyTicker.C:
			runWaiters()
		case <-ctx.Done():
			break out
		}
	}
}
