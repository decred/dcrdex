// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/server/account"
)

func testAcctID(b byte) account.AccountID {
	var user account.AccountID
	user[0] = b
	return user
}

func fixedRepFetcher(score int32, calls *int32) func(context.Context, account.AccountID) (*repData, error) {
	return func(context.Context, account.AccountID) (*repData, error) {
		atomic.AddInt32(calls, 1)
		return &repData{exists: true, score: score}, nil
	}
}

// blockedGet is a c.get that blocks inside fetch until release is called.
type blockedGet struct {
	started <-chan struct{} // closed when fetch starts
	release func()
	done    <-chan struct{} // closed when get returns
	data    *repData
	err     error
}

// startBlockedGet runs c.get in a goroutine with a fetch that signals when it starts,
// then blocks until release. Optional calls is incremented when the fetch runs.
func startBlockedGet(t *testing.T, c *repCache, user account.AccountID, score int32, calls *int32) *blockedGet {
	t.Helper()
	fetching := make(chan struct{})
	proceed := make(chan struct{})
	finished := make(chan struct{})
	bg := &blockedGet{
		started: fetching,
		release: sync.OnceFunc(func() { close(proceed) }),
		done:    finished,
	}
	go func() {
		defer close(finished)
		bg.data, bg.err = c.get(context.Background(), user, func(context.Context, account.AccountID) (*repData, error) {
			if calls != nil {
				atomic.AddInt32(calls, 1)
			}
			close(fetching)
			<-proceed
			return &repData{exists: true, score: score}, nil
		})
	}()
	t.Cleanup(func() {
		bg.release()
		<-bg.done
	})
	return bg
}

// waitForCacheMisses waits until the lookups have joined or started a fetch.
func waitForCacheMisses(t *testing.T, c *repCache, want uint64) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		_, misses, _, _ := c.stats()
		if misses == want {
			return
		}
		if misses > want {
			t.Fatalf("cache misses = %d, want %d", misses, want)
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatalf("cache misses = %d, want %d", misses, want)
		}
	}
}

func TestRepCache(t *testing.T) {
	c := newRepCache(8, time.Hour)
	user := testAcctID(1)
	ctx := context.Background()
	var calls int
	var fetchErr error
	score := int32(1)
	fetch := func(context.Context, account.AccountID) (*repData, error) {
		calls++
		if fetchErr != nil {
			return nil, fetchErr
		}
		return &repData{exists: true, score: score}, nil
	}
	check := func(wantScore int32, wantCalls int) {
		t.Helper()
		data, err := c.get(ctx, user, fetch)
		if err != nil || data.score != wantScore || calls != wantCalls {
			t.Fatalf("get = %v, %v, fetches = %d; want score %d and %d fetches", data, err, calls, wantScore, wantCalls)
		}
	}

	check(1, 1) // First lookup fetches.
	score = 2
	check(1, 1) // A hit returns the cached score.
	c.invalidate(user)
	check(2, 2)

	score = 3
	c.mtx.Lock()
	c.entries[user].Value.(*repEntry).loadedAt = time.Now().Add(-2 * c.maxAge)
	c.mtx.Unlock()
	check(3, 3) // Expired data is fetched again.

	c.invalidate(user)
	fetchErr = errors.New("db down")
	if _, err := c.get(ctx, user, fetch); !errors.Is(err, fetchErr) {
		t.Fatalf("get error = %v, want %v", err, fetchErr)
	}
	fetchErr = nil
	check(3, 5) // A failed fetch is retried.
	check(3, 5) // The successful retry is cached.
}

func TestRepCacheLRUEviction(t *testing.T) {
	c := newRepCache(2, time.Hour)
	ctx := context.Background()
	var calls int32
	fetch := fixedRepFetcher(1, &calls)

	userA, userB, userC := testAcctID(1), testAcctID(2), testAcctID(3)
	c.get(ctx, userA, fetch)
	c.get(ctx, userB, fetch)
	c.get(ctx, userA, fetch) // A more recent than B
	c.get(ctx, userC, fetch) // evicts B

	if calls != 3 {
		t.Fatalf("expected 3 fetches, got %d", calls)
	}
	c.get(ctx, userA, fetch)
	if calls != 3 {
		t.Fatal("user A should not have been evicted")
	}
	c.get(ctx, userB, fetch)
	if calls != 4 {
		t.Fatal("user B should have been evicted")
	}
	if _, _, _, evictions := c.stats(); evictions != 2 {
		t.Fatalf("expected 2 evictions, got %d", evictions)
	}
}

func TestRepCacheSingleflight(t *testing.T) {
	c := newRepCache(8, time.Hour)
	user := testAcctID(1)
	var calls int32
	leader := startBlockedGet(t, c, user, 9, &calls)
	<-leader.started

	followerDone := make(chan struct{})
	go func() {
		defer close(followerDone)
		data, err := c.get(context.Background(), user, fixedRepFetcher(0, &calls))
		if err != nil || data.score != 9 {
			t.Errorf("follower result = %v, %v, want score 9", data, err)
		}
	}()
	waitForCacheMisses(t, c, 2)
	leader.release()
	<-leader.done
	<-followerDone
	if calls != 1 {
		t.Fatalf("fetches = %d, want one shared fetch", calls)
	}
}

func TestRepDataBondTier(t *testing.T) {
	const now = 1000
	data := &repData{
		exists: true,
		bonds: []cachedBond{
			{strength: 1, lockTime: now - 1},    // expired
			{strength: 2, lockTime: now},        // boundary inclusive
			{strength: 4, lockTime: now + 3600}, // active
		},
	}
	if tier := data.bondTier(now); tier != 6 {
		t.Fatalf("expected bond tier 6, got %d", tier)
	}
	if tier := data.bondTier(now + 7200); tier != 0 {
		t.Fatalf("expected all bonds expired, got tier %d", tier)
	}
	if tier := (&repData{}).bondTier(now); tier != 0 {
		t.Fatalf("expected zero tier for unknown account, got %d", tier)
	}
}

func TestRepCacheInvalidation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		freshFirst bool
	}{
		{name: "old fetch finishes first"},
		{name: "new fetch finishes first", freshFirst: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newRepCache(8, time.Hour)
			user := testAcctID(1)
			stale := startBlockedGet(t, c, user, -1, nil)
			<-stale.started
			c.invalidate(user)
			fresh := startBlockedGet(t, c, user, 2, nil)
			<-fresh.started

			if tt.freshFirst {
				fresh.release()
				<-fresh.done
			}
			stale.release()
			<-stale.done
			if stale.err != nil || stale.data.score != -1 {
				t.Fatalf("invalidated caller got %v, %v, want its original score -1", stale.data, stale.err)
			}

			var calls int32
			done := make(chan struct{})
			go func() {
				defer close(done)
				data, err := c.get(context.Background(), user, fixedRepFetcher(99, &calls))
				if err != nil || data.score != 2 {
					t.Errorf("lookup after stale fetch = %v, %v, want score 2", data, err)
				}
			}()
			if !tt.freshFirst {
				waitForCacheMisses(t, c, 3)
				fresh.release()
			}
			<-fresh.done
			<-done
			if calls != 0 {
				t.Fatalf("extra fetches = %d, want to reuse the new fetch or its cached result", calls)
			}
		})
	}
}

func TestRepCacheCancellation(t *testing.T) {
	c := newRepCache(8, time.Hour)
	user := testAcctID(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaderDone := make(chan error, 1)
	go func() {
		_, err := c.get(ctx, user, func(ctx context.Context, _ account.AccountID) (*repData, error) {
			<-ctx.Done()
			// Drivers need not return context.Canceled themselves.
			return nil, errors.New("query canceled by database")
		})
		leaderDone <- err
	}()
	waitForCacheMisses(t, c, 1)

	followerDone := make(chan struct{})
	var data *repData
	var err error
	var calls int32
	go func() {
		defer close(followerDone)
		data, err = c.get(context.Background(), user, fixedRepFetcher(7, &calls))
	}()
	waitForCacheMisses(t, c, 2)
	cancel()
	if err := <-leaderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want cancellation", err)
	}
	<-followerDone
	if err != nil || data.score != 7 || calls != 1 {
		t.Fatalf("follower result = %v, %v, fetches = %d, want score 7 and one fetch", data, err, calls)
	}
	if data, err := c.get(context.Background(), user, fixedRepFetcher(0, &calls)); err != nil || data.score != 7 || calls != 1 {
		t.Fatalf("retry was not cached: %v, %v, fetches = %d", data, err, calls)
	}
}
