package coinlock

import (
	"math/rand"
	"testing"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/order/test"
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
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

// utxo implements order.Outpoint
type utxo struct {
	txHash []byte
	vout   uint32
}

func (u *utxo) TxHash() []byte { return u.txHash }
func (u *utxo) Vout() uint32   { return u.vout }

func randcomCoinID() order.CoinID {
	return randomBytes(36)
}

func verifyLocked(cl CoinLockChecker, coins []CoinID, wantLocked bool, t *testing.T) bool {
	for _, coin := range coins {
		locked := cl.CoinLocked(coin)
		if locked != wantLocked {
			t.Errorf("Coin %v locked=%v, wanted=%v.", coin, locked, wantLocked)
			return false
		}
	}
	return true
}

func Test_swapLocker_LockOrderCoins(t *testing.T) {
	rand.Seed(0)

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

	swapLock.LockOrdersCoins(orders)

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

	// Try and fail to relock coins.
	failed := swapLock.LockOrdersCoins(orders)
	if len(failed) != len(orders) {
		t.Fatalf("should have failed to lock %d coins, got %d failed", len(orders), len(failed))
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
}

func Test_bookLocker_LockCoins(t *testing.T) {
	masterLock := NewMasterCoinLocker()
	bookLock := masterLock.Book()

	rand.Seed(0)

	coinMap := make(map[order.OrderID][]CoinID)
	var allCoins []CoinID
	numOrders := 99
	allOrderIDs := make([]order.OrderID, numOrders)
	for i := 0; i < numOrders; i++ {
		coins := make([]CoinID, rand.Int63n(8)+1)
		for j := range coins {
			coins[j] = randCoinID()
		}
		oid := randomOrderID()
		coinMap[oid] = coins
		allCoins = append(allCoins, coins...)
		allOrderIDs[i] = oid
	}

	bookLock.LockCoins(coinMap)

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

	// Attempt relock of the already-locked coins.
	delete(coinMap, oid)
	failed := bookLock.LockCoins(coinMap)
	if len(failed) != len(coinMap) {
		t.Fatalf("should have failed to lock %d coins, got %d failed", len(coinMap), len(failed))
	}

	// Relock the coins for the removed order.
	bookLock.LockCoins(map[order.OrderID][]CoinID{
		oid: orderCoins,
	})

	// Make sure the BOOK locker say they are locked.
	if !verifyLocked(bookLock, allCoins, true) {
		t.Errorf("bookLock indicated coins were unlocked that should have been locked")
	}

	bookLock.UnlockAll()

	if !verifyLocked(bookLock, orderCoins, false) {
		t.Errorf("bookLock indicated coins were locked that should have been unlocked")
	}
}
