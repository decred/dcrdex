// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

const (
	repCacheCapacity = 1 << 16
	repCacheMaxAge   = 10 * time.Minute
)

// cachedBond holds a bond's strength and lock time for tier calculation.
type cachedBond struct {
	strength uint32
	lockTime int64 // Unix seconds
}

// repData holds a user's cached score and bonds.
type repData struct {
	exists bool // false for an unknown account
	score  int32
	bonds  []cachedBond
}

func newRepData(exists bool, score int32, bonds []*db.Bond) *repData {
	data := &repData{
		exists: exists,
		score:  score,
		bonds:  make([]cachedBond, len(bonds)),
	}
	for i, bond := range bonds {
		data.bonds[i] = cachedBond{
			strength: bond.Strength,
			lockTime: bond.LockTime,
		}
	}
	return data
}

// bondTier sums strengths with lockTime >= expiryThresh.
func (d *repData) bondTier(expiryThresh int64) int64 {
	var tier int64
	for _, bond := range d.bonds {
		if bond.lockTime >= expiryThresh {
			tier += int64(bond.strength)
		}
	}
	return tier
}

// repLoad holds the result of a shared fetch. Invalidating a load prevents
// caching its result; callers already waiting still receive it.
type repLoad struct {
	done chan struct{}
	data *repData
	err  error
}

// repEntry is a cached repData with its load time.
type repEntry struct {
	user     account.AccountID
	data     *repData
	loadedAt time.Time
}

// repCache holds a bounded set of reputation data, evicts the least recently
// used entries, and shares concurrent fetches for the same user.
type repCache struct {
	mtx      sync.Mutex
	capacity int
	maxAge   time.Duration
	entries  map[account.AccountID]*list.Element
	lru      *list.List // *repEntry values, front is most recently used
	loads    map[account.AccountID]*repLoad

	hits, misses, invalidations, evictions uint64
}

func newRepCache(capacity int, maxAge time.Duration) *repCache {
	return &repCache{
		capacity: capacity,
		maxAge:   maxAge,
		entries:  make(map[account.AccountID]*list.Element),
		lru:      list.New(),
		loads:    make(map[account.AccountID]*repLoad),
	}
}

// get returns cached reputation data or loads via fetch. Concurrent misses
// share one fetch. The returned *repData is shared and must not be mutated.
func (c *repCache) get(ctx context.Context, user account.AccountID,
	fetch func(context.Context, account.AccountID) (*repData, error)) (*repData, error) {

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, load, leader := c.probe(user)
		if load == nil { // cache hit
			return data, nil
		}
		if !leader {
			select {
			case <-load.done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Another caller's canceled fetch must not cancel this lookup.
			if errors.Is(load.err, context.Canceled) || errors.Is(load.err, context.DeadlineExceeded) {
				continue
			}
			return load.data, load.err
		}

		data, err := fetch(ctx, user)
		if err != nil && ctx.Err() != nil {
			// Return the context error so waiting callers can recognize cancellation and retry.
			err = ctx.Err()
		}
		c.completeLoad(user, load, data, err)
		return data, err
	}
}

// probe reports a hit (load == nil), an in-flight load to wait on
// (leader false), or a newly registered load the caller must fetch (leader true).
func (c *repCache) probe(user account.AccountID) (data *repData, load *repLoad, leader bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if elem, found := c.entries[user]; found {
		entry := elem.Value.(*repEntry)
		if time.Since(entry.loadedAt) < c.maxAge {
			c.lru.MoveToFront(elem)
			c.hits++
			return entry.data, nil, false
		}
		c.removeLocked(elem) // aged out
	}
	c.misses++
	if inFlight, running := c.loads[user]; running {
		return nil, inFlight, false
	}
	load = &repLoad{done: make(chan struct{})}
	c.loads[user] = load
	return nil, load, true
}

// completeLoad caches a successful result if the load is still current and
// unblocks its waiters.
func (c *repCache) completeLoad(user account.AccountID, load *repLoad, data *repData, err error) {
	c.mtx.Lock()
	// Invalidation may have replaced this load with a newer fetch.
	if c.loads[user] == load {
		delete(c.loads, user)
		if err == nil {
			c.storeLocked(user, data)
		}
	}
	c.mtx.Unlock()

	load.data, load.err = data, err
	close(load.done)
}

// invalidate removes cached data and detaches pending fetches for the users.
func (c *repCache) invalidate(users ...account.AccountID) {
	c.mtx.Lock()
	for _, user := range users {
		if elem, found := c.entries[user]; found {
			c.removeLocked(elem)
		}
		delete(c.loads, user)
		c.invalidations++
	}
	c.mtx.Unlock()
}

// stats returns cache activity counters.
func (c *repCache) stats() (hits, misses, invalidations, evictions uint64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.hits, c.misses, c.invalidations, c.evictions
}

// storeLocked inserts data for a user with no cached entry.
// The caller must hold c.mtx.
func (c *repCache) storeLocked(user account.AccountID, data *repData) {
	c.entries[user] = c.lru.PushFront(&repEntry{
		user:     user,
		data:     data,
		loadedAt: time.Now(),
	})
	for len(c.entries) > c.capacity {
		c.removeLocked(c.lru.Back())
		c.evictions++
	}
}

func (c *repCache) removeLocked(elem *list.Element) {
	delete(c.entries, elem.Value.(*repEntry).user)
	c.lru.Remove(elem)
}
