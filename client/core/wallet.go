// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// xcWallet is a wallet.
type xcWallet struct {
	asset.Wallet
	connector *dex.ConnectionMaster
	AssetID   uint32
	mtx       sync.RWMutex
	lockTime  time.Time
	hookedUp  bool
	balance   *db.Balance
	encPW     []byte
	address   string
	dbID      []byte
}

// Unlock unlocks the wallet.
func (w *xcWallet) Unlock(pw string, dur time.Duration) error {
	err := w.Wallet.Unlock(pw, dur)
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.lockTime = time.Now().Add(dur)
	w.mtx.Unlock()
	return nil
}

// Lock the wallet. The lockTime is zeroed so that unlocked will return false.
func (w *xcWallet) Lock() error {
	w.mtx.Lock()
	w.lockTime = time.Time{}
	w.mtx.Unlock()
	return w.Wallet.Lock()
}

// unlocked will return true if the lockTime has not passed.
func (w *xcWallet) unlocked() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.lockTime.After(time.Now())
}

// state returns the current WalletState.
func (w *xcWallet) state() *WalletState {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	winfo := w.Info()
	return &WalletState{
		Symbol:  unbip(w.AssetID),
		AssetID: w.AssetID,
		Open:    w.lockTime.After(time.Now()),
		Running: w.connector.On(),
		Balance: w.balance,
		Address: w.address,
		Units:   winfo.Units,
	}
}

// setBalance sets the wallet balance.
func (w *xcWallet) setBalance(bal *db.Balance) {
	w.mtx.Lock()
	w.balance = bal
	w.mtx.Unlock()
}

// setAddress sets the wallet's deposit address.
func (w *xcWallet) setAddress(addr string) {
	w.mtx.Lock()
	w.address = addr
	w.mtx.Unlock()
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// Connect calls the dex.Connector's Connect method and sets the
// xcWallet.hookedUp flag to true.
func (w *xcWallet) Connect(ctx context.Context) error {
	err := w.connector.Connect(ctx)
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.hookedUp = true
	w.mtx.Unlock()
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
