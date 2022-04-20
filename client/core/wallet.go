// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
)

// runWithTimeout runs the provided function, returning either the error from
// the function or errTimeout if the function fails to return within the
// timeout. This function is for wallet methods that may not have a context or
// timeout of their own, or we simply cannot rely on thirdparty packages to
// respect context cancellation or deadlines.
func runWithTimeout(f func() error, timeout time.Duration) error {
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		errChan <- f()
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(timeout):
		return errTimeout
	}
}

// xcWallet is a wallet. Use (*Core).loadWallet to construct a xcWallet.
type xcWallet struct {
	asset.Wallet
	connector  *dex.ConnectionMaster
	AssetID    uint32
	dbID       []byte
	walletType string
	traits     asset.WalletTrait

	mtx          sync.RWMutex
	encPass      []byte // empty means wallet not password protected
	balance      *WalletBalance
	pw           encode.PassBytes
	address      string
	peerCount    uint32
	monitored    uint32 // startWalletSyncMonitor goroutines monitoring sync status
	hookedUp     bool
	synced       bool
	syncProgress float32
}

// encPW returns xcWallet's encrypted password.
func (w *xcWallet) encPW() []byte {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.encPass
}

// setEncPW sets xcWallet's encrypted password.
func (w *xcWallet) setEncPW(encPW []byte) {
	w.mtx.Lock()
	w.encPass = encPW
	w.mtx.Unlock()
}

// Unlock unlocks the wallet backend and caches the decrypted wallet password so
// the wallet may be unlocked without user interaction using refreshUnlock.
func (w *xcWallet) Unlock(crypter encrypt.Crypter) error {
	if len(w.encPW()) == 0 {
		if w.Locked() {
			return fmt.Errorf("wallet reporting as locked, but no password has been set")
		}
		return nil
	}
	pw, err := crypter.Decrypt(w.encPW())
	if err != nil {
		return fmt.Errorf("unlockWallet decryption error: %w", err)
	}
	err = w.Wallet.Unlock(pw) // can be slow - no timeout and NOT in the critical section!
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
	w.mtx.RLock()
	pwUnset := len(w.encPass) == 0
	locked := len(w.pw) == 0
	w.mtx.RUnlock()
	if pwUnset {
		return false, fmt.Errorf("%s wallet reporting as locked but no password"+
			" has been set", unbip(w.AssetID))
	}
	if locked {
		return false, fmt.Errorf("cannot refresh unlock on a locked %s wallet",
			unbip(w.AssetID))
	}

	return true, w.Wallet.Unlock(w.pw)
}

// Lock the wallet. For encrypted wallets (encPW set), this clears the cached
// decrypted password and attempts to lock the wallet backend.
func (w *xcWallet) Lock(timeout time.Duration) error {
	w.mtx.Lock()
	if len(w.encPass) == 0 {
		w.mtx.Unlock()
		return nil
	}
	w.pw.Clear()
	w.pw = nil
	w.mtx.Unlock() // end critical section before actual wallet request

	return runWithTimeout(w.Wallet.Lock, timeout)
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
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	if len(w.encPass) == 0 {
		return true // unencrypted wallet
	}
	return len(w.pw) > 0 // cached password for encrypted wallet
}

// state returns the current WalletState.
func (w *xcWallet) state() *WalletState {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	winfo := w.Info()
	return &WalletState{
		Symbol:          unbip(w.AssetID),
		AssetID:         w.AssetID,
		Version:         winfo.Version,
		Open:            len(w.encPass) == 0 || len(w.pw) > 0,
		Running:         w.connector.On(),
		Balance:         w.balance,
		Address:         w.address,
		Units:           winfo.UnitInfo.AtomicUnit,
		FallbackFeeRate: w.FallbackFeeRate(),
		FeeRateUnits:    winfo.UnitInfo.FeeRateUnit,
		Encrypted:       len(w.encPass) > 0,
		PeerCount:       w.peerCount,
		Synced:          w.synced,
		SyncProgress:    w.syncProgress,
		WalletType:      w.walletType,
		Traits:          w.traits,
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
	if !w.connected() {
		return "", fmt.Errorf("cannot get address from unconnected %s wallet",
			unbip(w.AssetID))
	}

	na, is := w.Wallet.(asset.NewAddresser)
	if !is {
		return "", fmt.Errorf("wallet does not generate new addresses")
	}

	addr, err := na.NewAddress()
	if err != nil {
		return "", fmt.Errorf("%s Wallet.Address error: %w", unbip(w.AssetID), err)
	}

	w.mtx.Lock()
	w.address = addr
	w.mtx.Unlock()

	return addr, nil
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// Connect calls the dex.Connector's Connect method, sets the xcWallet.hookedUp
// flag to true, and validates the deposit address. Use Disconnect to cleanly
// shutdown the wallet.
func (w *xcWallet) Connect() error {
	// No parent context; use Disconnect instead.
	err := w.connector.Connect(context.Background())
	if err != nil {
		return err
	}
	// Now that we are connected, we must Disconnect if any calls fail below
	// since we are considering this wallet not "hookedUp".

	synced, progress, err := w.SyncStatus()
	if err != nil {
		w.connector.Disconnect()
		return err
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()
	haveAddress := w.address != ""
	if haveAddress {
		haveAddress, err = w.OwnsAddress(w.address)
		if err != nil {
			w.connector.Disconnect()
			return err
		}
	}
	if !haveAddress {
		w.address, err = w.Address()
		if err != nil {
			w.connector.Disconnect()
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

// Rescan will initiate a rescan of the wallet if the asset.Wallet
// implementation is a Rescanner.
func (w *xcWallet) Rescan(ctx context.Context) error {
	rescanner, ok := w.Wallet.(asset.Rescanner)
	if !ok {
		return errors.New("wallet does not support rescanning")
	}
	return rescanner.Rescan(ctx)
}

// LogFilePath returns the path of the wallet's log file if the
// asset.Wallet implementation is a LogFiler.
func (w *xcWallet) LogFilePath() (string, error) {
	logFiler, ok := w.Wallet.(asset.LogFiler)
	if !ok {
		return "", errors.New("wallet does not support getting log file")
	}
	return logFiler.LogFilePath(), nil
}

// SwapConfirmations calls (asset.Wallet).SwapConfirmations with a timeout
// Context. If the coin cannot be located, an asset.CoinNotFoundError is
// returned. If the coin is located, but recognized as spent, no error is
// returned.
func (w *xcWallet) SwapConfirmations(ctx context.Context, coinID []byte, contract []byte, matchTime uint64) (uint32, bool, error) {
	return w.Wallet.SwapConfirmations(ctx, coinID, contract, encode.UnixTimeMilli(int64(matchTime)))
}

// feeRater is identical to calling w.Wallet.(asset.FeeRater).
func (w *xcWallet) feeRater() (asset.FeeRater, bool) {
	rater, is := w.Wallet.(asset.FeeRater)
	return rater, is
}
