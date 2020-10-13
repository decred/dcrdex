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
	// UnlockAll releases all locked coins.
	UnlockAll()
	// UnlockOrderCoins unlocks all locked coins associated with an order.
	UnlockOrderCoins(oid order.OrderID)
	// UnlockOrdersCoins is like UnlockOrderCoins for multiple orders.
	UnlockOrdersCoins(oids []order.OrderID)
	// LockOrdersCoins locks all of the coins associated with multiple orders.
	LockOrdersCoins(orders []order.Order) (failed []order.Order)
	// LockCoins locks coins associated with certain orders. The input is
	// defined as a map of OrderIDs to a CoinID slice since it is likely easiest
	// for the caller to construct the input in this way.
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

// OrderCoinsLocked lists all coins locked by a given order are locked by either
// the swap or book lock.
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

// LockOrdersCoins locks coins associated with certain orders.
func (bl *bookLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) map[order.OrderID][]CoinID {
	return bl.bookLock.LockCoins(orderCoins)
}

// UnlockAll releases all locked coins.
func (bl *bookLocker) UnlockAll() {
	bl.bookLock.UnlockAll()
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

// LockOrdersCoins locks coins associated with certain orders.
func (sl *swapLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) map[order.OrderID][]CoinID {
	return sl.swapLock.LockCoins(orderCoins)
}

// UnlockAll releases all locked coins.
func (sl *swapLocker) UnlockAll() {
	sl.swapLock.UnlockAll()
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
	coinMtx            sync.RWMutex
	lockedCoins        map[coinIDKey]struct{}
	lockedCoinsByOrder map[order.OrderID][]CoinID
}

// NewAssetCoinLocker constructs a new AssetCoinLocker.
func NewAssetCoinLocker() *AssetCoinLocker {
	return &AssetCoinLocker{
		lockedCoins:        make(map[coinIDKey]struct{}),
		lockedCoinsByOrder: make(map[order.OrderID][]CoinID),
	}
}

// UnlockAll releases all locked coins.
func (ac *AssetCoinLocker) UnlockAll() {
	ac.coinMtx.Lock()
	ac.lockedCoins = make(map[coinIDKey]struct{})
	ac.lockedCoinsByOrder = make(map[order.OrderID][]CoinID)
	ac.coinMtx.Unlock()
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

// unlockOrderCoins should be called with the coinMtx locked.
func (ac *AssetCoinLocker) unlockOrderCoins(oid order.OrderID) {
	coins := ac.lockedCoinsByOrder[oid]
	for i := range coins {
		delete(ac.lockedCoins, coinIDKey(coins[i]))
	}
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
	for i := range oids {
		ac.unlockOrderCoins(oids[i])
	}
	ac.coinMtx.Unlock()
}

// LockCoins locks all coins (e.g. UTXOS) connected with certain orders.
func (ac *AssetCoinLocker) LockCoins(orderCoins map[order.OrderID][]CoinID) (failed map[order.OrderID][]CoinID) {
	failed = make(map[order.OrderID][]CoinID)
	ac.coinMtx.Lock()
	for oid, coins := range orderCoins {
		var fail bool
		for i := range coins {
			_, locked := ac.lockedCoins[coinIDKey(coins[i])]
			if locked {
				failed[oid] = append(failed[oid], coins[i])
				fail = true
			}
		}
		if fail {
			// Do not lock any of this order's coins.
			continue
		}

		ac.lockedCoinsByOrder[oid] = coins
		for i := range coins {
			ac.lockedCoins[coinIDKey(coins[i])] = struct{}{}
		}
	}
	ac.coinMtx.Unlock()
	return
}

// LockOrdersCoins locks all coins associated with certain orders.
func (ac *AssetCoinLocker) LockOrdersCoins(orders []order.Order) (failed []order.Order) {
	ac.coinMtx.Lock()
ordersLoop:
	for _, ord := range orders {
		coinIDs := ord.Trade().Coins
		if len(coinIDs) == 0 {
			continue // e.g. CancelOrder
		}

		for i := range coinIDs {
			_, locked := ac.lockedCoins[coinIDKey(coinIDs[i])]
			if locked {
				failed = append(failed, ord)
				continue ordersLoop
			}
		}

		ac.lockedCoinsByOrder[ord.ID()] = coinIDs
		for i := range coinIDs {
			ac.lockedCoins[coinIDKey(coinIDs[i])] = struct{}{}
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
