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

	// Consider: EndFunc that runs after: (1) TryFunc returns DontTryAgain, (2)
	// ExpireFunc is run, or (3) the queue shuts down.
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
	// Expire any waiters left on shutdown.
	defer func() {
		q.waiterMtx.Lock()
		for _, w := range q.waiters {
			w.ExpireFunc()
		}
		q.waiters = q.waiters[:0]
		q.waiterMtx.Unlock()
	}()
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

// tick speed is piecewise linear, constant at fastestInterval at or below
// fullSpeedTicks, linear from fastestInterval to slowestInterval between
// fullSpeedTicks and fullyTapered, and slowestInterval beyond that.
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
	fastestInterval time.Duration
	slowestInterval time.Duration
	queueWaiter     chan *taperingWaiter
}

// NewTaperingTickerQueue is a constructor for a TaperingTicketQueue. The
// arguments fasterInterval and slowestInterval define how the Waiter attempt
// speed is tapered. Initially, attempts will be tried every fastestInterval.
// After fullSpeedTicks, the delays will be increased until it reaches
// slowestInterval (at fullyTapered).
func NewTaperingTickerQueue(fastestInterval, slowestInterval time.Duration) *TaperingTickerQueue {
	return &TaperingTickerQueue{
		fastestInterval: fastestInterval,
		slowestInterval: slowestInterval,
		queueWaiter:     make(chan *taperingWaiter, 16),
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
	q.queueWaiter <- &taperingWaiter{Waiter: waiter, nextTick: time.Now()}
}

// Run runs the primary wait loop until the context is canceled.
func (q *TaperingTickerQueue) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer wg.Wait()

	runWaiter := func(w *taperingWaiter) {
		defer wg.Done()

		if w.TryFunc() == DontTryAgain {
			return
		}
		// If this waiter has expired, issue the timeout error to the client
		// and don't re-insert.
		if w.Expiration.Before(time.Now()) {
			w.ExpireFunc()
			return
		}

		w.tick++
		w.nextTick = nextTick(w.tick, q.slowestInterval, q.fastestInterval,
			time.Now(), w.Expiration)

		q.queueWaiter <- w // send it back to the queue
	}

	waiters := make([]*taperingWaiter, 0, 100) // only used in the loop
	var timer *time.Timer
	for {
		var tick <-chan time.Time
		if len(waiters) > 0 {
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(time.Until(waiters[0].nextTick))
			tick = timer.C
		}

		select {
		case <-tick:
			// Remove the next waiter from the slice. runWaiter will re-insert
			// with a new nextTick time if it sees TryAgain.
			w := waiters[0]
			waiters = waiters[1:]
			wg.Add(1)
			go runWaiter(w)

		case w := <-q.queueWaiter:
			// A little optimization if this waiter would fire immediately, but
			// it works to append regardless.
			if time.Until(w.nextTick) <= 0 {
				wg.Add(1)
				go runWaiter(w)
				continue
			}

			waiters = append(waiters, w)
			sort.Slice(waiters, func(i, j int) bool {
				return waiters[i].nextTick.Before(waiters[j].nextTick) // ascending, next tick first
			})

		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			for _, w := range waiters {
				w.ExpireFunc() // early, but still ending prior to DontTryAgain
			}
			return
		}
	}
}

func nextTick(ticksPassed int, slowestInterval, fastestInterval time.Duration,
	now, expiration time.Time) time.Time {
	var nextTickTime time.Time
	switch {
	case ticksPassed < fullSpeedTicks:
		nextTickTime = now.Add(fastestInterval)
	case ticksPassed < fullyTapered: // ramp up the interval
		prog := float64(ticksPassed+1-fullSpeedTicks) / (fullyTapered - fullSpeedTicks)
		taper := float64(slowestInterval - fastestInterval)
		interval := fastestInterval + time.Duration(math.Round(prog*taper))
		nextTickTime = now.Add(interval)
	default:
		nextTickTime = now.Add(slowestInterval)
	}

	if nextTickTime.After(expiration) {
		return expiration
	}
	return nextTickTime
}
