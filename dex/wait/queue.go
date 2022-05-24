// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package wait

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

// TryDirective is a response that a Waiter's TryFunc can return to instruct
// the queue to continue trying or to quit.
type TryDirective bool

const (
	// TryAgain, when returned from the Waiter's TryFunc, instructs the ticker
	// queue to try again after the configured delay.
	TryAgain TryDirective = false
	// DontTryAgain, when returned from the Waiter's TryFunc, instructs the
	// ticker queue to quit trying and quit tracking the Waiter.
	DontTryAgain TryDirective = true
)

// Waiter is a function to run every recheckInterval until completion or
// expiration. Completion is indicated when the TryFunc returns DontTryAgain.
// Expiration occurs when TryAgain is returned after Expiration time.
type Waiter struct {
	// Expiration time is checked after the function returns TryAgain. If the
	// current time > Expiration, ExpireFunc will be run and the waiter will be
	// un-queued.
	Expiration time.Time
	// TryFunc is the function to run periodically until DontTryAgain is
	// returned or Waiter expires.
	TryFunc func() TryDirective
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
func (q *TickerQueue) Wait(w *Waiter) {
	if time.Now().After(w.Expiration) {
		log.Error("wait.TickerQueue: Waiter given expiration before present")
		return
	}
	// Check to see if it passes right away.
	if w.TryFunc() == DontTryAgain {
		return
	}
	q.waiterMtx.Lock()
	q.waiters = append(q.waiters, w)
	q.waiterMtx.Unlock()
}

// Run runs the primary wait loop until the context is canceled.
func (q *TickerQueue) Run(ctx context.Context) {
	// The latencyTicker triggers a check of all waitFunc functions.
	latencyTicker := time.NewTicker(q.recheckInterval)
	defer latencyTicker.Stop()

	runWaiters := func() {
		q.waiterMtx.Lock()
		defer q.waiterMtx.Unlock()
		agains := make([]*Waiter, 0)
		// Grab new waiters
		tNow := time.Now()
		for _, w := range q.waiters {
			if ctx.Err() != nil {
				return
			}
			if w.TryFunc() == DontTryAgain {
				continue
			}
			// If this waiter has expired, issue the timeout error to the client
			// and do not append to the agains slice.
			if w.Expiration.Before(tNow) {
				w.ExpireFunc()
				continue
			}
			agains = append(agains, w)
		}
		q.waiters = agains
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

const (
	// fullSpeedTicks is the number of attempts that will be made with the
	// configured fastestInterval delay. After fullSpeedTicks, the retry speed
	// will be tapered off.
	fullSpeedTicks = 3
	// Once the number of attempts has reached fullyTapered, the delay between
	// attempts will be set to slowestInterval.
	fullyTapered = 15
)

type taperingWaiter struct {
	*Waiter
	// tick tracks the number of attempts that have been made and is used to
	// calculate the tapered delay.
	tick int
	// nextTick is used to sort the waiters.
	nextTick time.Time
}

// TaperingTickerQueue is a queue that will run Waiters according to a tapering-
// delay schedule. The first attempts will be more frequent, but if they are
// not successful, the delay between attempts will grow longer and longer up
// to a configurable maximum.
type TaperingTickerQueue struct {
	waiterMtx       sync.Mutex
	waiters         []*taperingWaiter
	fastestInterval time.Duration
	slowestInterval time.Duration
	recalcTimer     chan struct{}
}

// NewTaperingTickerQueue is a constructor for a TaperingTicketQueue. The
// arguments fasterInterval and slowestInterval define how the Waiter attempt
// speed is tapered. Initially, attempts will be tried every fastestInterval.
// After fullSpeedTicks, the delays will be increased until it reaches
// slowestInterval (at fullyTapered).
func NewTaperingTickerQueue(fastestInterval, slowestInterval time.Duration) *TaperingTickerQueue {
	return &TaperingTickerQueue{
		waiters:         make([]*taperingWaiter, 0, 100),
		fastestInterval: fastestInterval,
		slowestInterval: slowestInterval,
		recalcTimer:     make(chan struct{}, 1),
	}
}

// Wait attempts to run the (*Waiter).TryFunc until either 1) the function
// returns the value DontTryAgain, or 2) the function's Expiration time has
// passed. In the case of 2, the (*Waiter).ExpireFunc will be run.
func (q *TaperingTickerQueue) Wait(waiter *Waiter) {
	if time.Now().After(waiter.Expiration) {
		log.Error("wait.TickerQueue: Waiter given expiration before present")
		return
	}
	// We don't want the caller to hang here, so we won't call TryFunc. Instead
	// set the nextTick as now and the run loop will call it in a goroutine
	// immediately.
	q.insert(&taperingWaiter{Waiter: waiter}, time.Now())
}

// insert inserts the waiter, setting its nextTick under waiterMtx lock, and
// sorts the waiters by time until nextTick.
func (q *TaperingTickerQueue) insert(w *taperingWaiter, nextTick time.Time) {
	q.waiterMtx.Lock()
	w.nextTick = nextTick
	q.waiters = append(q.waiters, w)
	sort.Slice(q.waiters, func(i, j int) bool { return q.waiters[i].nextTick.Before(q.waiters[j].nextTick) })
	q.waiterMtx.Unlock()
	q.recalcTimer <- struct{}{}
}

// Run runs the primary wait loop until the context is canceled.
func (q *TaperingTickerQueue) Run(ctx context.Context) {

	taper := float64(q.slowestInterval - q.fastestInterval)

	runWaiter := func(w *taperingWaiter, n int) {
		if w.TryFunc() == DontTryAgain {
			return
		}
		// If this waiter has expired, issue the timeout error to the client
		// and don't re-insert.
		if w.Expiration.Before(time.Now()) {
			w.ExpireFunc()
			return
		}

		var nextTick time.Time
		switch {
		case n <= fullSpeedTicks:
			nextTick = time.Now().Add(q.fastestInterval)
		case n < fullyTapered: // ramp up the interval
			prog := float64(n-fullSpeedTicks) / (fullyTapered - fullSpeedTicks)
			interval := q.fastestInterval + time.Duration(math.Round(prog*taper))
			nextTick = time.Now().Add(interval)
		default:
			nextTick = time.Now().Add(q.slowestInterval)
		}
		if nextTick.After(w.Expiration) {
			nextTick = w.Expiration
		}
		q.insert(w, nextTick)
	}

	for {
		q.waiterMtx.Lock()
		var nextTick <-chan time.Time
		if len(q.waiters) > 0 {
			nextTick = time.After(time.Until(q.waiters[0].nextTick))
		}
		q.waiterMtx.Unlock()

		select {
		case <-nextTick:
			q.waiterMtx.Lock()
			// No need to check length. This loop is the only place waiters can
			// be removed from the slice and it's sychronous.
			w := q.waiters[0]
			tick := w.tick
			w.tick++
			// Remove the waiter from the slice. runWaiter will re-insert if
			// it sees TryAgain.
			q.waiters = q.waiters[1:]
			q.waiterMtx.Unlock()
			go runWaiter(w, tick)
		case <-q.recalcTimer:
		case <-ctx.Done():
			return
		}
	}

}
