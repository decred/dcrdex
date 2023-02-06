package wait

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

func TestTaperingQueueExpiration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := NewTaperingTickerQueue(time.Millisecond, time.Millisecond*10)
	go q.Run(ctx)

	var expirationTime time.Time
	var expiration sync.WaitGroup
	expiration.Add(1)

	wantExpirationTime := time.Now().Add(time.Millisecond * 10)
	q.Wait(&Waiter{
		Expiration: wantExpirationTime,
		TryFunc: func() TryDirective {
			return TryAgain
		},
		ExpireFunc: func() {
			expirationTime = time.Now()
			expiration.Done()
		},
	})

	expiration.Wait()

	if expirationTime.Before(wantExpirationTime) {
		t.Fatalf("expired at: %v - sooner than expected: %v", expirationTime, wantExpirationTime)
	}
}

func TestTaperingQueue(t *testing.T) {
	const fastestInterval, slowestInterval = 1 * time.Millisecond, 2 * time.Millisecond
	expiration := time.Now().Add(time.Minute)

	q := NewTaperingTickerQueue(fastestInterval, slowestInterval)

	// waiterTriesTimedMtx protects waiterTriesTimed from concurrent access.
	var waiterTriesTimedMtx sync.Mutex
	// waiterTriesTimed maps waiter to a list of tries, each try is represented
	// by timestamp (that reflects when waiter try starts executing).
	waiterTriesTimed := make(map[int][]time.Time, 5)
	var wgWaiters sync.WaitGroup
	addWaiter := func(waiterNumber, numTryAgains int) {
		var numTrys int
		q.Wait(&Waiter{
			Expiration: expiration,
			TryFunc: func() TryDirective {
				waiterTriesTimedMtx.Lock()
				// Record when try func was called to check it later.
				waiterTriesTimed[waiterNumber] = append(waiterTriesTimed[waiterNumber], time.Now())
				waiterTriesTimedMtx.Unlock()
				numTrys++
				if numTrys > numTryAgains {
					wgWaiters.Done()
					return DontTryAgain
				}
				return TryAgain
			},
			// We don't expect expire func being called in this test, leaving it
			// undefined so that we'll get a panic in case it gets called.
			//ExpireFunc: func() {},
		})
	}

	wgWaiters.Add(5)
	addWaiter(0, 20)
	addWaiter(1, 0)
	addWaiter(2, 10)
	addWaiter(3, 3)
	addWaiter(4, 1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	var wgQ sync.WaitGroup
	wgQ.Add(1)
	go func() {
		defer wgQ.Done()
		q.Run(ctx)
	}()

	// Wait for each waiter to get done, then stop the ticker queue itself and
	// wait for it.
	wgWaiters.Wait()
	cancel()
	wgQ.Wait()

	calcExpectedTicks := func(totalTicks int) []time.Time {
		return expTickSchedule(time.Now(), fastestInterval, slowestInterval)[:totalTicks]
	}
	// There is always at least one tick per waiter we expect (hence +1).
	expWaiterTriesTimes := map[int][]time.Time{
		0: calcExpectedTicks(20 + 1),
		1: calcExpectedTicks(0 + 1),
		2: calcExpectedTicks(10 + 1),
		3: calcExpectedTicks(3 + 1),
		4: calcExpectedTicks(1 + 1),
	}
	for waiterNumber, wantTriesTimes := range expWaiterTriesTimes {
		gotTriesTimed := waiterTriesTimed[waiterNumber]
		if len(gotTriesTimed) != len(wantTriesTimes) {
			t.Fatalf("expected waiter %d to execute %d tries, got %d instead",
				waiterNumber, len(wantTriesTimes), len(gotTriesTimed))
		}
		var (
			prevWantTryTime time.Time
			prevGotTryTimed time.Time
		)
		for i, wantTryTime := range wantTriesTimes {
			gotTryTimed := gotTriesTimed[i]
			if i == 0 {
				// Can't compare try difference for first try since there is nothing
				// to compare against.
				prevWantTryTime = wantTryTime
				prevGotTryTimed = gotTryTimed
				continue
			}
			// Check that waiter tapering works, in other words each waiter try attempt
			// doesn't execute sooner than we expect.
			// We compare the actual observed time difference between two adjacent waiter
			// try-attempts with synthetically calculated one (in wantTryDiff var), the
			// actual observed should be higher-or-equal because there is additional
			// code executing (scheduling/executing retry-attempts and such).
			wantTryDiff := wantTryTime.Sub(prevWantTryTime)
			gotTryDiff := gotTryTimed.Sub(prevGotTryTimed)
			if gotTryDiff < wantTryDiff {
				t.Fatalf("expected waiter %d to have time difference between tries %d-%d be > %v, "+
					"got time difference: %v", waiterNumber, i, i-1, wantTryDiff, gotTryDiff)
			}
			prevWantTryTime = wantTryTime
			prevGotTryTimed = gotTryTimed
		}
	}
}

func Test_nextTick(t *testing.T) {
	const fastestInterval, slowestInterval = 100 * time.Millisecond, 500 * time.Millisecond
	var gotTicks []time.Time
	now := time.Now()
	expiration := now.Add(time.Hour)

	// First tick happens right away.
	gotTicks = append(gotTicks, now)
	for tick := 1; tick <= 19; tick++ {
		gotTicks = append(gotTicks, nextTick(tick, slowestInterval, fastestInterval,
			gotTicks[tick-1], expiration))
	}
	wantTicks := expTickSchedule(now, fastestInterval, slowestInterval)

	// To check expiration on the last tick.
	gotTicks = append(gotTicks, nextTick(20, slowestInterval, fastestInterval,
		expiration, expiration))
	wantTicks[len(wantTicks)-1] = expiration

	for i, want := range wantTicks {
		got := gotTicks[i]
		if want != got {
			t.Fatalf("expected tick %d to be: %v, got: %v", i, want, got)
		}
	}
}

// expTickSchedule returns expected tick schedule with a certain startTime.
func expTickSchedule(startTime time.Time, fastestInterval, slowestInterval time.Duration) []time.Time {
	expectedTicks := [21]time.Time{} // 21 element should be enough for all our needs in these tests.
	expectedTicks[0] = startTime
	expectedTicks[1] = expectedTicks[0].Add(fastestInterval)
	expectedTicks[2] = expectedTicks[1].Add(fastestInterval)

	taper := func(i int) time.Duration {
		const linearCnt = fullyTapered - fullSpeedTicks
		ramp := float64(slowestInterval - fastestInterval)
		return time.Duration(math.Round(float64(i) / linearCnt * ramp))
	}
	for i := fullSpeedTicks; i < fullyTapered-1; i++ {
		expectedTicks[i] = expectedTicks[i-1].Add(fastestInterval + taper(i-2))
	}
	for i := fullyTapered - 1; i < len(expectedTicks); i++ {
		expectedTicks[i] = expectedTicks[i-1].Add(slowestInterval)
	}
	return expectedTicks[:]
}
