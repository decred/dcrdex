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

// xcWallet is a wallet. Use (*Core).loadWallet to construct a xcWallet.
type xcWallet struct {
	asset.Wallet
	connector *dex.ConnectionMaster
	AssetID   uint32
	dbID      []byte
	encPW     []byte // empty means wallet not password protected

	mtx          sync.RWMutex
	balance      *WalletBalance
	pw           string
	address      string
	hookedUp     bool
	synced       bool
	syncProgress float32
	feeRateLimit float32
}

// Unlock unlocks the wallet backend and caches the decrypted wallet password so
// the wallet may be unlocked without user interaction using refreshUnlock.
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

// refreshUnlock is used to ensure the wallet is unlocked. If the wallet backend
// reports as already unlocked, which includes a wallet with no password
// protection, no further action is taken and a nil error is returned. If the
// wallet is reporting as locked, and the wallet is not known to be password
// protected (no encPW set) or the decrypted password is not cached, a non-nil
// error is returned. If no encrypted password is set, the xcWallet is
// misconfigured and should be recreated. If the decrypted password is not
// stored, the Unlock method should be used to decrypt the password. Finally, a
// non-nil error will be returned if the cached password fails to unlock the
// wallet, in which case unlockAttempted will also be true.
func (w *xcWallet) refreshUnlock() (unlockAttempted bool, err error) {
	// Check if the wallet backend is already unlocked.
	if !w.Locked() {
		return false, nil // unlocked
	}
	// Locked backend requires both encrypted and decrypted passwords.
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

// Lock the wallet. For encrypted wallets (encPW set), this clears the cached
// decrypted password and attempts to lock the wallet backend.
func (w *xcWallet) Lock() error {
	if len(w.encPW) == 0 {
		return nil
	}
	w.mtx.Lock()
	w.pw = ""
	w.mtx.Unlock()
	return w.Wallet.Lock()
}

// unlocked will only return true if both the wallet backend is unlocked and we
// have cached the decryped wallet password. The wallet backend may be queried
// directly, likely involving an RPC call. Use locallyUnlocked to determine if
// the wallet is automatically unlockable rather than actually unlocked.
func (w *xcWallet) unlocked() bool {
	return w.locallyUnlocked() && !w.Locked()
}

// locallyUnlocked checks whether we think the wallet is unlocked, but without
// asking the wallet itself. More precisely, for encrypted wallets (encPW set)
// this is true only if the decrypted password is cached. Use this to determine
// if the wallet may be unlocked without user interaction (via refreshUnlock).
func (w *xcWallet) locallyUnlocked() bool {
	// return len(w.encPW) == 0 || len(w.pw) > 0
	if len(w.encPW) == 0 {
		return true // unencrypted wallet
	}
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return len(w.pw) > 0 // cached password for encrypted wallet
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
