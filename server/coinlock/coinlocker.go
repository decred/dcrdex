// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package coinlock

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex/order"
)

type CoinID = order.CoinID

// CoinLockChecker provides the ability to check if a coin or an order's backing
// coins are locked.
type CoinLockChecker interface {
	// CoinLocked indicates if a coin is locked.
	CoinLocked(coin CoinID) bool
	// OrderCoinsLocked returns all coins locked by an order.
	OrderCoinsLocked(oid order.OrderID) []CoinID
}

// CoinLocker provides the ability to lock, unlock and check lock status of
// coins.
type CoinLocker interface {
	CoinLockChecker
	// UnlockOrderCoins unlocks all locked coins associated with an order.
	UnlockOrderCoins(oid order.OrderID)
	// UnlockOrdersCoins is like UnlockOrderCoins for multiple orders.
	UnlockOrdersCoins(oids []order.OrderID)
	// LockOrdersCoins locks all of the coins associated with multiple orders.
	// Repeating a request for coins already held by the same order succeeds.
	LockOrdersCoins(orders []order.Order) (failed []order.Order)
	// LockCoins locks the coins listed for each order. Repeating a request
	// for coins already held by the same order succeeds.
	LockCoins(orderCoins map[order.OrderID][]CoinID) (failed map[order.OrderID][]CoinID)
}

// MasterCoinLocker coordinates a book and swap coin locker. The lock status of
// a coin may be checked, and the locker for the book and swapper may be
// obtained via the Book and Swap methods.
type MasterCoinLocker struct {
	bookLock *AssetCoinLocker
	swapLock *AssetCoinLocker
}

// NewMasterCoinLocker creates a new NewMasterCoinLocker.
func NewMasterCoinLocker() *MasterCoinLocker {
	return &MasterCoinLocker{
		bookLock: NewAssetCoinLocker(),
		swapLock: NewAssetCoinLocker(),
	}
}

// CoinLocked indicates if a coin is locked by either the swap or book lock.
func (cl *MasterCoinLocker) CoinLocked(coin CoinID) bool {
	lockedBySwap := cl.swapLock.CoinLocked(coin)
	if lockedBySwap {
		return true
	}

	return cl.bookLock.CoinLocked(coin)
}

// OrderCoinsLocked returns the order's swap-locked coins if any, otherwise its
// book-locked coins.
func (cl *MasterCoinLocker) OrderCoinsLocked(oid order.OrderID) []CoinID {
	coins := cl.swapLock.OrderCoinsLocked(oid)
	if len(coins) > 0 {
		return coins
	}

	// TODO: Figure out how we will handle the evolving coinIDs for an order
	// with partial fills and decide how to merge the results of both swap and
	// book locks.
	return cl.bookLock.OrderCoinsLocked(oid)
}

// Book provides the market-level CoinLocker.
func (cl *MasterCoinLocker) Book() CoinLocker {
	return &bookLocker{cl}
}

// Swap provides the swap-level CoinLocker.
func (cl *MasterCoinLocker) Swap() CoinLocker {
	return &swapLocker{cl}
}

type bookLocker struct {
	*MasterCoinLocker
}

// LockOrdersCoins locks all coins for the given orders.
func (bl *bookLocker) LockOrdersCoins(orders []order.Order) []order.Order {
	return bl.bookLock.LockOrdersCoins(orders)
}

// LockCoins locks coins associated with certain orders.
func (bl *bookLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) map[order.OrderID][]CoinID {
	return bl.bookLock.LockCoins(orderCoins)
}

// UnlockOrdersCoins unlocks all locked coins associated with an order.
func (bl *bookLocker) UnlockOrdersCoins(oids []order.OrderID) {
	bl.bookLock.UnlockOrdersCoins(oids)
}

// UnlockOrderCoins unlocks all locked coins associated with an order.
func (bl *bookLocker) UnlockOrderCoins(oid order.OrderID) {
	bl.bookLock.UnlockOrderCoins(oid)
}

var _ (CoinLocker) = (*bookLocker)(nil)

type swapLocker struct {
	*MasterCoinLocker
}

// LockOrdersCoins locks all coins for the given orders.
func (sl *swapLocker) LockOrdersCoins(orders []order.Order) []order.Order {
	return sl.swapLock.LockOrdersCoins(orders)
}

// LockCoins locks coins associated with certain orders.
func (sl *swapLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) map[order.OrderID][]CoinID {
	return sl.swapLock.LockCoins(orderCoins)
}

// UnlockOrderCoins unlocks all locked coins associated with an order.
func (sl *swapLocker) UnlockOrderCoins(oid order.OrderID) {
	sl.swapLock.UnlockOrderCoins(oid)
}

// UnlockOrdersCoins unlocks all locked coins associated with an order.
func (sl *swapLocker) UnlockOrdersCoins(oids []order.OrderID) {
	sl.swapLock.UnlockOrdersCoins(oids)
}

var _ (CoinLocker) = (*swapLocker)(nil)

type coinIDKey string

// AssetCoinLocker is a coin locker for a single asset. Do not use this for more
// than one asset.
type AssetCoinLocker struct {
	coinMtx sync.RWMutex
	// lockedCoins maps each locked coin to its owning order.
	lockedCoins        map[coinIDKey]order.OrderID
	lockedCoinsByOrder map[order.OrderID][]CoinID
}

// NewAssetCoinLocker constructs a new AssetCoinLocker.
func NewAssetCoinLocker() *AssetCoinLocker {
	return &AssetCoinLocker{
		lockedCoins:        make(map[coinIDKey]order.OrderID),
		lockedCoinsByOrder: make(map[order.OrderID][]CoinID),
	}
}

// CoinLocked indicates if a coin identifier (e.g. UTXO) is locked.
func (ac *AssetCoinLocker) CoinLocked(coin CoinID) bool {
	ac.coinMtx.RLock()
	_, locked := ac.lockedCoins[coinIDKey(coin)]
	ac.coinMtx.RUnlock()
	return locked
}

// OrderCoinsLocked lists the coin IDs (e.g. UTXOs) locked by an order.
func (ac *AssetCoinLocker) OrderCoinsLocked(oid order.OrderID) []CoinID {
	ac.coinMtx.RLock()
	defer ac.coinMtx.RUnlock()
	return ac.lockedCoinsByOrder[oid]
}

// unlockOrderCoins should be called with the coinMtx locked. Only locks the
// order itself holds are released.
func (ac *AssetCoinLocker) unlockOrderCoins(oid order.OrderID) {
	coins := ac.lockedCoinsByOrder[oid]
	for _, coin := range coins {
		key := coinIDKey(coin)
		if owner, locked := ac.lockedCoins[key]; locked && owner == oid {
			delete(ac.lockedCoins, key)
		}
	}
	delete(ac.lockedCoinsByOrder, oid)
}

// UnlockOrderCoins unlocks any coins backing the order.
func (ac *AssetCoinLocker) UnlockOrderCoins(oid order.OrderID) {
	ac.coinMtx.Lock()
	ac.unlockOrderCoins(oid)
	ac.coinMtx.Unlock()
}

// UnlockOrdersCoins unlocks any coins backing the orders.
func (ac *AssetCoinLocker) UnlockOrdersCoins(oids []order.OrderID) {
	ac.coinMtx.Lock()
	for _, oid := range oids {
		ac.unlockOrderCoins(oid)
	}
	ac.coinMtx.Unlock()
}

// ownsAllCoins reports whether the order already holds locks on all the
// given coins. coinMtx must be held.
func (ac *AssetCoinLocker) ownsAllCoins(oid order.OrderID, coinIDs []CoinID) bool {
	for _, coin := range coinIDs {
		if owner, locked := ac.lockedCoins[coinIDKey(coin)]; !locked || owner != oid {
			return false
		}
	}
	return true
}

// LockCoins locks all coins connected with certain orders. An order that
// already holds locks on all the given coins is a no-op, not a failure.
func (ac *AssetCoinLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) (failed map[order.OrderID][]CoinID) {
	failed = make(map[order.OrderID][]CoinID)
	ac.coinMtx.Lock()
	for oid, coins := range orderCoins {
		if ac.ownsAllCoins(oid, coins) {
			continue
		}
		var fail bool
		for _, coin := range coins {
			_, locked := ac.lockedCoins[coinIDKey(coin)]
			if locked {
				failed[oid] = append(failed[oid], coin)
				fail = true
			}
		}
		if fail {
			// Do not lock any of this order's coins.
			continue
		}

		ac.lockedCoinsByOrder[oid] = coins
		for _, coin := range coins {
			ac.lockedCoins[coinIDKey(coin)] = oid
		}
	}
	ac.coinMtx.Unlock()
	return
}

// LockOrdersCoins locks all coins associated with certain orders. An order
// that already holds locks on all of its coins is a no-op, not a failure.
func (ac *AssetCoinLocker) LockOrdersCoins(orders []order.Order) (failed []order.Order) {
	ac.coinMtx.Lock()
ordersLoop:
	for _, ord := range orders {
		coinIDs := ord.Trade().Coins
		if len(coinIDs) == 0 {
			continue
		}

		oid := ord.ID()
		if ac.ownsAllCoins(oid, coinIDs) {
			continue
		}

		for _, coin := range coinIDs {
			_, locked := ac.lockedCoins[coinIDKey(coin)]
			if locked {
				failed = append(failed, ord)
				continue ordersLoop
			}
		}

		ac.lockedCoinsByOrder[oid] = coinIDs
		for _, coin := range coinIDs {
			ac.lockedCoins[coinIDKey(coin)] = oid
		}
	}
	ac.coinMtx.Unlock()
	return
}

// DEXCoinLocker manages multiple MasterCoinLocker, one for each asset used by
// the DEX.
type DEXCoinLocker struct {
	masterLocks map[uint32]*MasterCoinLocker
}

// NewDEXCoinLocker creates a new DEXCoinLocker for the given assets.
func NewDEXCoinLocker(assets []uint32) *DEXCoinLocker {
	masterLocks := make(map[uint32]*MasterCoinLocker, len(assets))
	for _, asset := range assets {
		masterLocks[asset] = NewMasterCoinLocker()
	}

	return &DEXCoinLocker{masterLocks}
}

// AssetLocker retrieves the MasterCoinLocker for an asset.
func (c *DEXCoinLocker) AssetLocker(asset uint32) *MasterCoinLocker {
	return c.masterLocks[asset]
}

// CoinLocked checks if a coin belonging to an asset is locked.
func (c *DEXCoinLocker) CoinLocked(asset uint32, coin string) bool {
	locker := c.masterLocks[asset]
	if locker == nil {
		panic(fmt.Sprintf("unknown asset %d", asset))
	}

	return locker.CoinLocked(CoinID(coin))
}

// OrderCoinsLocked retrieves all locked coins for a given asset and user.
func (c *DEXCoinLocker) OrderCoinsLocked(asset uint32, oid order.OrderID) []CoinID {
	locker := c.masterLocks[asset]
	if locker == nil {
		panic(fmt.Sprintf("unknown asset %d", asset))
	}

	return locker.OrderCoinsLocked(oid)
}
