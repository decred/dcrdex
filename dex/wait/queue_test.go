package wait

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTaper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := NewTaperingTickerQueue(time.Millisecond, time.Millisecond*10)
	go q.Run(ctx)

	var last time.Time
	intervals := make([]time.Duration, 0, 10)
	var expiration sync.WaitGroup
	expiration.Add(1)

	q.Wait(&Waiter{
		Expiration: time.Now().Add(time.Millisecond * 30),
		TryFunc: func() TryDirective {
			if last.IsZero() {
				last = time.Now()
				return TryAgain
			}
			intervals = append(intervals, time.Since(last))
			last = time.Now()
			return TryAgain
		},
		ExpireFunc: func() {
			expiration.Done()
		},
	})

	expiration.Wait()

	if len(intervals) < 3 {
		t.Fatalf("only %d intervals", len(intervals))
	}

	// check that the first interval was close to the expected value.
	var sum time.Duration
	for _, i := range intervals[:fullSpeedTicks] {
		sum += i
	}
	avg := sum / fullSpeedTicks

	// It'd be nice to check this tighter than *10, but with the race detector,
	// there are some unexpectedly long times.
	if avg < time.Millisecond || avg > time.Millisecond*10 {
		t.Fatalf("first intervals are out of bound: %s", avg)
	}

	// Make sure it tapered. Can't use the actual last interval, since that
	// might be truncated. Use the second from the last.
	lastInterval := intervals[len(intervals)-2]
	if lastInterval < time.Millisecond*2 {
		t.Fatalf("last interval wasn't ~5 ms: %s", lastInterval)
	}
}

func TestTaperingQueue(t *testing.T) {
	q := NewTaperingTickerQueue(time.Millisecond, time.Millisecond*5)
	q.recalcTimer = make(chan struct{}, 5)

	var waiterNumber int
	var resultMtx sync.Mutex
	var resultOrder []int
	var wg sync.WaitGroup
	addWaiter := func(numTryAgains int) {
		var numTrys int
		num := waiterNumber
		waiterNumber++
		q.Wait(&Waiter{
			Expiration: time.Now().Add(time.Hour),
			TryFunc: func() TryDirective {
				numTrys++
				if numTrys > numTryAgains {
					resultMtx.Lock()
					resultOrder = append(resultOrder, num)
					resultMtx.Unlock()
					wg.Done()
					return DontTryAgain
				}
				return TryAgain
			},
			ExpireFunc: func() {},
		})
	}

	wg.Add(5)
	addWaiter(4)
	addWaiter(0)
	addWaiter(3)
	addWaiter(1)
	addWaiter(2)

	expOrder := []int{1, 3, 4, 2, 0}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	go q.Run(ctx)

	wg.Wait()

	if len(resultOrder) != len(expOrder) {
		t.Fatalf("only %d of %d results received", len(resultOrder), len(expOrder))
	}

	for i := range resultOrder {
		if resultOrder[i] != expOrder[i] {
			t.Fatalf("wrong result order expected %+x, got %+x", expOrder, resultOrder)
		}
	}

	q.waiterMtx.Lock()
	remaining := len(q.waiters)
	q.waiterMtx.Unlock()
	if remaining != 0 {
		t.Fatalf("%d remaining waiters", remaining)
	}
}
