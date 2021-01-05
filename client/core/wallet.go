// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"fmt"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encrypt"
)

// xcWallet is a wallet.
type xcWallet struct {
	asset.Wallet
	connector    *dex.ConnectionMaster
	AssetID      uint32
	mtx          sync.RWMutex
	hookedUp     bool
	balance      *WalletBalance
	encPW        []byte
	pw           string
	address      string
	dbID         []byte
	synced       bool
	syncProgress float32
}

// Unlock unlocks the wallet.
func (w *xcWallet) Unlock(crypter encrypt.Crypter) error {
	if len(w.encPW) == 0 {
		if w.Locked() {
			return fmt.Errorf("wallet reporting as locked, but no password has been set")
		}
		return nil
	}
	pwB, err := crypter.Decrypt(w.encPW)
	if err != nil {
		return fmt.Errorf("unlockWallet decryption error: %w", err)
	}
	pw := string(pwB)
	err = w.Wallet.Unlock(pw)
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.pw = pw
	w.mtx.Unlock()
	return nil
}

// refreshUnlock checks that the wallet is unlocked, and if not, uses the cached
// password to attempt unlocking.
func (w *xcWallet) refreshUnlock() (unlockAttempted bool, err error) {
	// Check if the wallet is already unlocked.
	if !w.Locked() {
		return false, nil
	}
	if len(w.encPW) == 0 {
		return false, fmt.Errorf("%s wallet reporting as locked but no password has been set", unbip(w.AssetID))
	}
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	if len(w.pw) == 0 {
		return false, fmt.Errorf("cannot refresh unlock on a locked %s wallet", unbip(w.AssetID))
	}
	return true, w.Wallet.Unlock(w.pw)
}

// Lock the wallet.
func (w *xcWallet) Lock() error {
	if len(w.encPW) == 0 {
		return nil
	}
	w.mtx.Lock()
	w.pw = ""
	w.mtx.Unlock()
	return w.Wallet.Lock()
}

// unlocked will return true if the wallet is unlocked. The wallet is queried
// directly, likely involving an RPC call. Use locallyUnlocked if it's not
// critical.
func (w *xcWallet) unlocked() bool {
	return !w.Locked()
}

// locallyUnlocked checks whether we think the wallet is unlocked, but without
// asking the wallet itself. Use this to prevent spamming the RPC every time
// refreshUser is called.
func (w *xcWallet) locallyUnlocked() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return len(w.encPW) == 0 || len(w.pw) > 0
}

// state returns the current WalletState.
func (w *xcWallet) state() *WalletState {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	winfo := w.Info()
	return &WalletState{
		Symbol:       unbip(w.AssetID),
		AssetID:      w.AssetID,
		Open:         len(w.encPW) == 0 || len(w.pw) > 0,
		Running:      w.connector.On(),
		Balance:      w.balance,
		Address:      w.address,
		Units:        winfo.Units,
		Encrypted:    len(w.encPW) > 0,
		Synced:       w.synced,
		SyncProgress: w.syncProgress,
	}
}

// setBalance sets the wallet balance.
func (w *xcWallet) setBalance(bal *WalletBalance) {
	w.mtx.Lock()
	w.balance = bal
	w.mtx.Unlock()
}

func (w *xcWallet) currentDepositAddress() string {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.address
}

func (w *xcWallet) refreshDepositAddress() (string, error) {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	if !w.hookedUp {
		return "", fmt.Errorf("cannot get address from unconnected %s wallet",
			unbip(w.AssetID))
	}

	addr, err := w.Address()
	if err != nil {
		return "", fmt.Errorf("%s Wallet.Address error: %w", unbip(w.AssetID), err)
	}

	w.address = addr
	return addr, nil
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// Connect calls the dex.Connector's Connect method, sets the xcWallet.hookedUp
// flag to true, and validates the deposit address.
func (w *xcWallet) Connect(ctx context.Context) error {
	err := w.connector.Connect(ctx)
	if err != nil {
		return err
	}
	synced, progress, err := w.SyncStatus()
	if err != nil {
		return err
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()
	haveAddress := w.address != ""
	if haveAddress {
		haveAddress, err = w.OwnsAddress(w.address)
		if err != nil {
			return err
		}
	}
	if !haveAddress {
		w.address, err = w.Address()
		if err != nil {
			return fmt.Errorf("%s Wallet.Address error: %w", unbip(w.AssetID), err)
		}
	}
	w.hookedUp = true
	w.synced = synced
	w.syncProgress = progress

	return nil
}

// Disconnect calls the dex.Connector's Disconnect method and sets the
// xcWallet.hookedUp flag to false.
func (w *xcWallet) Disconnect() {
	w.connector.Disconnect()
	w.mtx.Lock()
	w.hookedUp = false
	w.mtx.Unlock()
}

// Confirmations calls (asset.Wallet).Confirmations with a timeout Context.
func (w *xcWallet) Confirmations(ctx context.Context, coinID []byte) (uint32, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, confCheckTimeout)
	defer cancel()
	return w.Wallet.Confirmations(ctx, coinID)
}
