// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package broadcast

import (
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

// CacheExpiry is the duration after which a broadcast cache entry is
// considered stale and will be removed.
const CacheExpiry = 10 * time.Minute

// CoinIDsCacheKey builds a cache key from sorted coin IDs.
func CoinIDsCacheKey(coins []asset.Coin) string {
	ids := make([]string, len(coins))
	for i, c := range coins {
		ids[i] = hex.EncodeToString(c.ID())
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// RedeemCacheKey builds a cache key from sorted redemption input coin IDs.
func RedeemCacheKey(redemptions []*asset.Redemption) string {
	ids := make([]string, len(redemptions))
	for i, r := range redemptions {
		if r.Spends != nil && r.Spends.Coin != nil {
			ids[i] = hex.EncodeToString(r.Spends.Coin.ID())
		} else {
			ids[i] = "<nil>"
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// RefundCacheKey builds a cache key from a coin ID.
func RefundCacheKey(coinID []byte) string {
	return hex.EncodeToString(coinID)
}

// IsAlreadyBroadcastErr checks if the error indicates the transaction is
// already in the mempool or blockchain.
func IsAlreadyBroadcastErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, m := range []string{
		"txn-already-in-mempool",     // Bitcoin Core, btcd (mempool)
		"txn-already-known",          // Bitcoin Core (confirmed tx resubmission)
		"already in block chain",     // btcd
		"transaction already exists", // dcrd
		"already have transaction",   // zcashd
		"tx already exists",          // Bitcoin Core
		"already in the mempool",     // Electrum
	} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// CacheEntry is the interface that broadcast cache entry types must implement.
type CacheEntry interface {
	Stamp() time.Time
}

// Cache is a thread-safe cache for broadcast entries with automatic expiry.
type Cache[T CacheEntry] struct {
	mtx   sync.Mutex
	cache map[string]T
}

// NewCache creates a new broadcast cache.
func NewCache[T CacheEntry]() *Cache[T] {
	return &Cache[T]{
		cache: make(map[string]T),
	}
}

// Get returns the entry for the given key. If the entry is expired, it is
// deleted and the zero value is returned with false.
func (c *Cache[T]) Get(key string) (T, bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	entry, ok := c.cache[key]
	if ok && time.Since(entry.Stamp()) > CacheExpiry {
		delete(c.cache, key)
		var zero T
		return zero, false
	}
	return entry, ok
}

// Put stores an entry in the cache.
func (c *Cache[T]) Put(key string, entry T) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.cache[key] = entry
}

// Delete removes an entry from the cache.
func (c *Cache[T]) Delete(key string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	delete(c.cache, key)
}

// RecoverFromCache attempts to recover a previously built transaction from the
// cache. It first tries to rebroadcast the cached transaction. If that fails,
// it checks whether the transaction is already confirmed in the wallet. If
// either succeeds, the cached entry is returned and left in the cache to
// expire naturally. On any failure the cache entry is cleared and the caller
// should rebuild the transaction.
func RecoverFromCache[T CacheEntry](
	cache *Cache[T],
	key string,
	rebroadcast func(T) error,
	isConfirmed func(T) bool,
	log dex.Logger,
	opName string,
) (T, bool) {
	cached, ok := cache.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	err := rebroadcast(cached)
	if err == nil {
		log.Infof("%s broadcast recovered from cache", opName)
		return cached, true
	}
	if isConfirmed(cached) {
		log.Infof("%s transaction found in wallet, recovering", opName)
		return cached, true
	}
	cache.Delete(key)
	log.Warnf("Cached %s tx rebroadcast failed (%v), rebuilding", opName, err)
	var zero T
	return zero, false
}
