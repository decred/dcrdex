package broadcast

import (
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

func TestIsAlreadyBroadcastErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"mempool", errors.New("txn-already-in-mempool"), true},
		{"known", errors.New("txn-already-known"), true},
		{"blockchain", errors.New("already in block chain"), true},
		{"exists", errors.New("transaction already exists"), true},
		{"have", errors.New("already have transaction"), true},
		{"tx exists", errors.New("tx already exists"), true},
		{"in mempool", errors.New("Already in the mempool"), true},
		{"unrelated", errors.New("insufficient funds"), false},
		{"wrapped", errors.New("rpc error: txn-already-in-mempool"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAlreadyBroadcastErr(tt.err); got != tt.want {
				t.Errorf("IsAlreadyBroadcastErr() = %v, want %v", got, tt.want)
			}
		})
	}
}

type testCoin struct {
	id []byte
}

func (c *testCoin) ID() dex.Bytes  { return c.id }
func (c *testCoin) String() string { return dex.Bytes(c.id).String() }
func (c *testCoin) TxID() string   { return "" }
func (c *testCoin) Value() uint64  { return 0 }

func TestCoinIDsCacheKey(t *testing.T) {
	coins := []asset.Coin{
		&testCoin{id: []byte{0xbb}},
		&testCoin{id: []byte{0xaa}},
	}
	key := CoinIDsCacheKey(coins)
	// Should be sorted.
	if key != "aa,bb" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestRedeemCacheKey(t *testing.T) {
	redemptions := []*asset.Redemption{
		{Spends: &asset.AuditInfo{Coin: &testCoin{id: []byte{0xdd}}}},
		{Spends: &asset.AuditInfo{Coin: &testCoin{id: []byte{0xcc}}}},
	}
	key := RedeemCacheKey(redemptions)
	if key != "cc,dd" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestRedeemCacheKeyNilSpends(t *testing.T) {
	redemptions := []*asset.Redemption{
		{Spends: nil},
		{Spends: &asset.AuditInfo{Coin: &testCoin{id: []byte{0xaa}}}},
	}
	key := RedeemCacheKey(redemptions)
	if key != "<nil>,aa" {
		t.Fatalf("unexpected key: %s", key)
	}
}

func TestRefundCacheKey(t *testing.T) {
	key := RefundCacheKey([]byte{0xde, 0xad})
	if key != "dead" {
		t.Fatalf("unexpected key: %s", key)
	}
}

type testEntry struct {
	stamp time.Time
	val   int
}

func (e *testEntry) Stamp() time.Time { return e.stamp }

func TestCacheGetPutDelete(t *testing.T) {
	c := NewCache[*testEntry]()

	// Get from empty cache.
	_, ok := c.Get("key")
	if ok {
		t.Fatal("expected no entry")
	}

	// Put and Get.
	entry := &testEntry{stamp: time.Now(), val: 42}
	c.Put("key", entry)
	got, ok := c.Get("key")
	if !ok {
		t.Fatal("expected entry")
	}
	if got.val != 42 {
		t.Fatalf("expected val 42, got %d", got.val)
	}

	// Delete and verify gone.
	c.Delete("key")
	_, ok = c.Get("key")
	if ok {
		t.Fatal("expected no entry after delete")
	}
}

func TestCacheExpiry(t *testing.T) {
	c := NewCache[*testEntry]()

	// Insert an entry that is already expired.
	entry := &testEntry{stamp: time.Now().Add(-CacheExpiry - time.Second), val: 99}
	c.Put("expired", entry)

	// Get should return false and clean up.
	_, ok := c.Get("expired")
	if ok {
		t.Fatal("expected expired entry to be removed")
	}

	// Verify it's actually gone.
	c.mtx.Lock()
	_, exists := c.cache["expired"]
	c.mtx.Unlock()
	if exists {
		t.Fatal("expired entry should have been deleted from map")
	}
}

func TestRecoverFromCache(t *testing.T) {
	log := dex.StdOutLogger("TEST", dex.LevelWarn)

	t.Run("cache miss", func(t *testing.T) {
		c := NewCache[*testEntry]()
		_, ok := RecoverFromCache(c, "missing",
			func(e *testEntry) error { return nil },
			func(e *testEntry) bool { return false },
			log, "Test",
		)
		if ok {
			t.Fatal("expected no recovery on cache miss")
		}
	})

	t.Run("rebroadcast success", func(t *testing.T) {
		c := NewCache[*testEntry]()
		entry := &testEntry{stamp: time.Now(), val: 7}
		c.Put("key", entry)

		got, ok := RecoverFromCache(c, "key",
			func(e *testEntry) error { return nil },
			func(e *testEntry) bool { t.Fatal("should not check wallet"); return false },
			log, "Test",
		)
		if !ok {
			t.Fatal("expected recovery")
		}
		if got.val != 7 {
			t.Fatalf("expected val 7, got %d", got.val)
		}
		// Cache entry should still exist (expires naturally).
		if _, exists := c.Get("key"); !exists {
			t.Fatal("cache entry should still exist after successful recovery")
		}
	})

	t.Run("rebroadcast fail wallet success", func(t *testing.T) {
		c := NewCache[*testEntry]()
		entry := &testEntry{stamp: time.Now(), val: 8}
		c.Put("key", entry)

		got, ok := RecoverFromCache(c, "key",
			func(e *testEntry) error { return errors.New("broadcast failed") },
			func(e *testEntry) bool { return true },
			log, "Test",
		)
		if !ok {
			t.Fatal("expected recovery via wallet")
		}
		if got.val != 8 {
			t.Fatalf("expected val 8, got %d", got.val)
		}
		// Cache entry should still exist (expires naturally).
		if _, exists := c.Get("key"); !exists {
			t.Fatal("cache entry should still exist after successful recovery")
		}
	})

	t.Run("both fail", func(t *testing.T) {
		c := NewCache[*testEntry]()
		entry := &testEntry{stamp: time.Now(), val: 9}
		c.Put("key", entry)

		_, ok := RecoverFromCache(c, "key",
			func(e *testEntry) error { return errors.New("broadcast failed") },
			func(e *testEntry) bool { return false },
			log, "Test",
		)
		if ok {
			t.Fatal("expected no recovery when both fail")
		}
		if _, exists := c.Get("key"); exists {
			t.Fatal("cache entry should have been deleted")
		}
	})

	t.Run("expired entry", func(t *testing.T) {
		c := NewCache[*testEntry]()
		entry := &testEntry{stamp: time.Now().Add(-CacheExpiry - time.Second), val: 10}
		c.Put("key", entry)

		_, ok := RecoverFromCache(c, "key",
			func(e *testEntry) error { t.Fatal("should not attempt broadcast"); return nil },
			func(e *testEntry) bool { t.Fatal("should not check wallet"); return false },
			log, "Test",
		)
		if ok {
			t.Fatal("expected no recovery for expired entry")
		}
	})
}
