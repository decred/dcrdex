package coinlock

import (
	"bytes"
	crand "crypto/rand"
	"testing"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/order/test"
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	crand.Read(bytes)
	return bytes
}

func randCoinID() CoinID {
	return CoinID(randomBytes(72))
}

func randomOrderID() order.OrderID {
	pk := randomBytes(order.OrderIDSize)
	var id order.OrderID
	copy(id[:], pk)
	return id
}

func randcomCoinID() order.CoinID {
	return randomBytes(36)
}

func Test_swapLocker_LockOrderCoins(t *testing.T) {
	w := &test.Writer{
		Addr: "asdf",
		Acct: test.NextAccount(),
		Sell: true,
		Market: &test.Market{
			Base:    2,
			Quote:   0,
			LotSize: 100,
		},
	}

	lo0, _ := test.WriteLimitOrder(w, 1000, 1, order.StandingTiF, 0)
	lo0.Coins = []order.CoinID{randcomCoinID(), randcomCoinID()}
	lo1, _ := test.WriteLimitOrder(w, 1000, 2, order.StandingTiF, 0)
	lo1.Coins = []order.CoinID{randcomCoinID(), randcomCoinID(), randcomCoinID()}

	orders := []order.Order{lo0, lo1}
	oid0, oid1 := lo0.ID(), lo1.ID()

	masterLock := NewMasterCoinLocker()
	swapLock := masterLock.Swap()
	bookLock := masterLock.Book()

	emptyCoins := masterLock.OrderCoinsLocked(oid0)
	if len(emptyCoins) != 0 {
		t.Fatalf("found coins that were not yet locked")
	}

	if failed := swapLock.LockOrdersCoins(orders); len(failed) != 0 {
		t.Fatalf("initial locks failed: %v", failed)
	}

	lo0Coins := masterLock.OrderCoinsLocked(oid0)
	if len(lo0Coins) != len(lo0.Coins) {
		t.Fatalf("Expected %d coins for order %v, got %d", len(lo0.Coins), lo0, len(lo0Coins))
	}

	lo1Coins := masterLock.OrderCoinsLocked(oid1)
	if len(lo1Coins) != len(lo1.Coins) {
		t.Fatalf("Expected %d coins for order %v, got %d", len(lo1.Coins), lo0, len(lo1Coins))
	}

	for _, coin := range lo1Coins {
		if !masterLock.CoinLocked(coin) {
			t.Errorf("masterLock said coin %v wasn't locked", coin)
		}
		if !swapLock.CoinLocked(coin) {
			t.Errorf("swapLock said coin %v wasn't locked", coin)
		}
		if !bookLock.CoinLocked(coin) {
			t.Errorf("bookLocker said coin %v wasn't locked", coin)
		}
	}

	// Locking the same orders again succeeds.
	failed := swapLock.LockOrdersCoins(orders)
	if len(failed) != 0 {
		t.Fatalf("same-order relock should succeed, got %d failed", len(failed))
	}

	// A different order contending for an already-locked coin still fails.
	lo2, _ := test.WriteLimitOrder(w, 1000, 3, order.StandingTiF, 0)
	newCoin := randcomCoinID()
	lo2.Coins = []order.CoinID{newCoin, lo0.Coins[0]}
	failed = swapLock.LockOrdersCoins([]order.Order{lo2})
	if len(failed) != 1 || failed[0] != lo2 {
		t.Fatalf("failed orders = %v, want only the contender", failed)
	}
	if swapLock.CoinLocked(newCoin) || len(swapLock.OrderCoinsLocked(lo2.ID())) != 0 {
		t.Fatal("failed request left coins locked")
	}

	// Now lock some in the book lock.
	bookLock.LockOrdersCoins([]order.Order{lo0})
	// unlock them in swap lock
	swapLock.UnlockOrderCoins(oid0)
	// verify they are still locked
	for _, coin := range lo0Coins {
		if !masterLock.CoinLocked(coin) {
			t.Errorf("masterLock said coin %v wasn't locked", coin)
		}
		if !swapLock.CoinLocked(coin) {
			t.Errorf("swapLock said coin %v wasn't locked", coin)
		}
		if !bookLock.CoinLocked(coin) {
			t.Errorf("bookLocker said coin %v wasn't locked", coin)
		}
	}
	// now unlock them in book lock too
	bookLock.UnlockOrderCoins(oid0)
	// verify they are now unlocked
	for _, coin := range lo0Coins {
		if masterLock.CoinLocked(coin) {
			t.Errorf("masterLock said coin %v was locked", coin)
		}
		if swapLock.CoinLocked(coin) {
			t.Errorf("swapLock said coin %v was locked", coin)
		}
		if bookLock.CoinLocked(coin) {
			t.Errorf("bookLocker said coin %v was locked", coin)
		}
	}
	if coins := swapLock.OrderCoinsLocked(oid0); len(coins) != 0 {
		t.Fatalf("unlocked order still lists coins: %v", coins)
	}

	// A repeated unlock must not release coins acquired by a new owner.
	if failed := swapLock.LockOrdersCoins([]order.Order{lo2}); len(failed) != 0 {
		t.Fatalf("new owner could not lock released coins: %v", failed)
	}
	swapLock.UnlockOrderCoins(oid0)
	for _, coin := range lo2.Coins {
		if !swapLock.CoinLocked(coin) {
			t.Fatal("old owner unlocked the new owner's coin")
		}
	}
	swapLock.UnlockOrderCoins(lo2.ID())
	if coins := swapLock.OrderCoinsLocked(lo2.ID()); len(coins) != 0 {
		t.Fatalf("unlocked contender still lists coins: %v", coins)
	}
	for _, coin := range lo2.Coins {
		if swapLock.CoinLocked(coin) {
			t.Fatal("contender's coin remains locked after unlock")
		}
	}
}

func Test_bookLocker_LockCoins(t *testing.T) {
	masterLock := NewMasterCoinLocker()
	bookLock := masterLock.Book()

	coinMap := make(map[order.OrderID][]CoinID)
	var allCoins []CoinID
	const numOrders = 2
	allOrderIDs := make([]order.OrderID, numOrders)
	for i := 0; i < numOrders; i++ {
		coins := make([]CoinID, i+2)
		for j := range coins {
			coins[j] = randCoinID()
		}
		oid := randomOrderID()
		coinMap[oid] = coins
		allCoins = append(allCoins, coins...)
		allOrderIDs[i] = oid
	}

	if failed := bookLock.LockCoins(coinMap); len(failed) != 0 {
		t.Fatalf("initial locks failed: %v", failed)
	}

	verifyLocked := func(cl CoinLockChecker, coins []CoinID, wantLocked bool) (ok bool) {
		for _, coin := range coins {
			locked := cl.CoinLocked(coin)
			if locked != wantLocked {
				t.Errorf("Coin %v locked=%v, wanted=%v.", coin, locked, wantLocked)
				return false
			}
		}
		return true
	}

	// Make sure the BOOK locker say they are locked.
	if !verifyLocked(bookLock, allCoins, true) {
		t.Errorf("bookLock indicated coins were unlocked that should have been locked")
	}

	// Make sure the MASTER locker say they are locked too.
	if !verifyLocked(masterLock, allCoins, true) {
		t.Errorf("masterLock indicated coins were unlocked that should have been locked")
	}

	// Make sure the SWAP lockers sways they are locked too.
	swapLock := masterLock.Swap()
	if !verifyLocked(swapLock, allCoins, true) {
		t.Errorf("swapLock indicated coins were unlocked that should have been locked")
	}

	// try and fail to unlock coins via the swap lock
	oid := allOrderIDs[0]
	swapLock.UnlockOrderCoins(oid)
	if !verifyLocked(swapLock, allCoins, true) {
		t.Errorf("swapLock indicated coins were unlocked that should have been locked")
	}

	// unlock properly via the book lock
	bookLock.UnlockOrderCoins(oid)
	orderCoins := coinMap[oid]
	if !verifyLocked(bookLock, orderCoins, false) {
		t.Errorf("bookLock indicated coins were locked that should have been unlocked")
	}
	if !verifyLocked(swapLock, orderCoins, false) {
		t.Errorf("swapLock indicated coins were locked that should have been unlocked")
	}

	if coins := bookLock.OrderCoinsLocked(oid); len(coins) != 0 {
		t.Fatalf("unlocked order still lists coins: %v", coins)
	}

	// Locking the remaining order again succeeds.
	delete(coinMap, oid)
	failed := bookLock.LockCoins(coinMap)
	if len(failed) != 0 {
		t.Fatalf("same-order relock should succeed, got %d failed", len(failed))
	}

	// A different order contending for an already-locked coin still fails.
	contender := randomOrderID()
	newCoin, lockedCoin := randCoinID(), coinMap[allOrderIDs[1]][0]
	failed = bookLock.LockCoins(map[order.OrderID][]CoinID{
		contender: {newCoin, lockedCoin},
	})
	if len(failed) != 1 || len(failed[contender]) != 1 || !bytes.Equal(failed[contender][0], lockedCoin) {
		t.Fatalf("failed coins = %v, want the contender's conflicting coin", failed)
	}
	if bookLock.CoinLocked(newCoin) || len(bookLock.OrderCoinsLocked(contender)) != 0 {
		t.Fatal("failed request left coins locked")
	}

	// Releasing the old order again must preserve the new owner's locks.
	if failed := bookLock.LockCoins(map[order.OrderID][]CoinID{contender: orderCoins}); len(failed) != 0 {
		t.Fatalf("new owner could not lock released coins: %v", failed)
	}
	bookLock.UnlockOrdersCoins([]order.OrderID{oid})
	if !verifyLocked(bookLock, orderCoins, true) {
		t.Fatal("old owner unlocked the new owner's coins")
	}
	bookLock.UnlockOrdersCoins([]order.OrderID{contender})
	if coins := bookLock.OrderCoinsLocked(contender); len(coins) != 0 {
		t.Fatalf("unlocked contender still lists coins: %v", coins)
	}

	// Relock the coins for the removed order.
	if failed := bookLock.LockCoins(map[order.OrderID][]CoinID{
		oid: orderCoins,
	}); len(failed) != 0 {
		t.Fatalf("relocking the original order failed: %v", failed)
	}

	// Make sure the BOOK locker say they are locked.
	if !verifyLocked(bookLock, allCoins, true) {
		t.Errorf("bookLock indicated coins were unlocked that should have been locked")
	}
}
