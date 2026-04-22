//go:build xmr

// Adaptor-swap helpers for the XMR asset backend.
//
// These are the three primitives identified in
// internal/cmd/xmrswap/XMR_WALLET_AUDIT.md as needed for the BTC/XMR
// adaptor swap. They are layered on top of the existing ExchangeWallet
// without modifying its HTLC-shaped asset.Wallet methods (which remain
// stubbed as ErrUnsupported).
//
// The orchestrator is responsible for ed25519 + secp256k1 key
// generation, DLEQ proofs, scalar arithmetic, and all protocol logic.
// This file only exposes the chain-interaction primitives that require
// cgo access to the Monero wallet2 library.

package xmr

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset/xmr/cxmr"
	"decred.org/dcrdex/dex"
)

// swapWallets tracks auxiliary per-swap wallets (watch-only and
// sweep) that are created and torn down during the lifecycle of an
// individual adaptor swap. Keyed by a caller-supplied swap ID.
type swapWalletSet struct {
	watch *cxmr.Wallet
	sweep *cxmr.Wallet
}

// ensureSwapWalletMap is a lazy initializer for the swap wallet map
// held on ExchangeWallet. Kept here rather than on the struct
// definition to minimize churn in xmr.go.
var (
	swapWalletMapMu sync.Mutex
	swapWalletMaps  = make(map[*ExchangeWallet]map[string]*swapWalletSet)
)

func swapMap(w *ExchangeWallet) map[string]*swapWalletSet {
	swapWalletMapMu.Lock()
	defer swapWalletMapMu.Unlock()
	m, ok := swapWalletMaps[w]
	if !ok {
		m = make(map[string]*swapWalletSet)
		swapWalletMaps[w] = m
	}
	return m
}

// XMRWatchHandle represents an open view-only wallet scanning a
// specific shared XMR address for an in-flight swap. Callers query
// it via HasFunds and close it via Close when done.
type XMRWatchHandle struct {
	wallet *cxmr.Wallet
	swapID string
	owner  *ExchangeWallet
	// Expected amount sent to the shared address. HasFunds compares
	// against this when reporting presence.
	expectedAmount uint64
}

// Synced reports whether the watch wallet has caught up to the
// daemon tip.
func (h *XMRWatchHandle) Synced() bool {
	if h == nil || h.wallet == nil {
		return false
	}
	return h.wallet.Synchronized()
}

// HasFunds returns true if the expected amount is visible as an
// unspent, unlocked output in the watch wallet's view of the shared
// address. minConfs is not separately enforced here since monero_c
// encodes confirm depth in the unlocked/locked balance split.
func (h *XMRWatchHandle) HasFunds() (present bool, unlocked bool, err error) {
	if h == nil || h.wallet == nil {
		return false, false, errors.New("watch wallet closed")
	}
	bal := h.wallet.Balance(0)
	unlockedBal := h.wallet.UnlockedBalance(0)
	return bal >= h.expectedAmount, unlockedBal >= h.expectedAmount, nil
}

// Close shuts down the watch wallet and removes it from the swap
// wallet map.
func (h *XMRWatchHandle) Close() error {
	if h == nil || h.wallet == nil {
		return nil
	}
	h.owner.wm.CloseWallet(h.wallet, false)
	h.wallet = nil

	m := swapMap(h.owner)
	swapWalletMapMu.Lock()
	defer swapWalletMapMu.Unlock()
	if set := m[h.swapID]; set != nil {
		set.watch = nil
	}
	return nil
}

// SendToSharedAddress sends `amount` atomic units to the shared XMR
// address derived from the peer's public spend key and the shared
// view key. Returns the transmitted txid and the daemon height at
// send time, suitable as a restore-height for later sweep-wallet
// recovery.
func (w *ExchangeWallet) SendToSharedAddress(ctx context.Context,
	sharedAddr string, amount uint64) (txID string, sentHeight uint64, err error) {

	w.walletMtx.RLock()
	if w.wallet == nil {
		w.walletMtx.RUnlock()
		return "", 0, errors.New("wallet not connected")
	}
	primary := w.wallet
	w.walletMtx.RUnlock()

	if !cxmr.AddressValid(sharedAddr, dexNetworkToCgo(w.net)) {
		return "", 0, fmt.Errorf("shared address invalid for network")
	}

	// Capture the chain height before sending. The sweep wallet
	// restore-height uses this value so it can skip most of the
	// chain when it is eventually opened.
	sentHeight = primary.DaemonBlockChainHeight()

	tx, err := primary.CreateTransaction(sharedAddr, amount, w.feePriority, 0)
	if err != nil {
		return "", 0, fmt.Errorf("create tx: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", 0, fmt.Errorf("commit tx: %w", err)
	}
	txID = tx.TxID()
	return txID, sentHeight, nil
}

// WatchSharedAddress opens a view-only wallet at the shared XMR
// address so the caller can verify the peer's lock without seeing
// the full wallet state. viewKey is the hex-encoded shared view key
// (the sum of both parties' view-key halves). restoreHeight lets the
// wallet skip old blocks when syncing. expectedAmount is what
// HasFunds compares against.
//
// The returned handle is valid until Close() is called. Do not hold
// multiple watch handles on the same shared address for the same
// ExchangeWallet concurrently - the underlying monero_c wallet
// manager has not been verified as safe for that (see
// XMR_WALLET_AUDIT.md).
func (w *ExchangeWallet) WatchSharedAddress(ctx context.Context,
	swapID, sharedAddr, viewKeyHex string, restoreHeight, expectedAmount uint64) (*XMRWatchHandle, error) {

	if !cxmr.AddressValid(sharedAddr, dexNetworkToCgo(w.net)) {
		return nil, fmt.Errorf("shared address invalid for network")
	}

	password := viewKeyHex // per-swap wallets use the view key as password for simplicity
	walletFile := filepath.Join(w.dataDir, "swap_watch_"+swapID)

	// CreateWalletFromKeys with empty spendKey produces a view-only wallet.
	watch, err := w.wm.CreateWalletFromKeys(
		walletFile, password, "English",
		dexNetworkToCgo(w.net), restoreHeight,
		sharedAddr, viewKeyHex, "",
	)
	if err != nil {
		return nil, fmt.Errorf("create watch wallet: %w", err)
	}

	// Connect to the same daemon as the primary wallet.
	if !watch.Init(w.daemonAddr, w.daemonUser, w.daemonPass, false, false, "") {
		w.wm.CloseWallet(watch, false)
		return nil, fmt.Errorf("watch wallet init: %s", watch.ErrorString())
	}
	if !watch.ConnectToDaemon() {
		w.wm.CloseWallet(watch, false)
		return nil, fmt.Errorf("watch wallet connect: %s", watch.ErrorString())
	}
	// See SweepSharedAddress for why these are required on simnet.
	if w.net == dex.Simnet {
		watch.SetTrustedDaemon(true)
		watch.SetAllowMismatchedDaemonVersion(true)
	}
	watch.SetRecoveringFromSeed(true)
	watch.SetRefreshFromBlockHeight(restoreHeight)
	watch.SetAutoRefreshInterval(5000)
	watch.StartRefresh()

	m := swapMap(w)
	swapWalletMapMu.Lock()
	if existing := m[swapID]; existing != nil {
		m[swapID].watch = watch
	} else {
		m[swapID] = &swapWalletSet{watch: watch}
	}
	swapWalletMapMu.Unlock()

	return &XMRWatchHandle{
		wallet:         watch,
		swapID:         swapID,
		owner:          w,
		expectedAmount: expectedAmount,
	}, nil
}

// SweepSharedAddress opens a spendable wallet at the shared XMR
// address using the full spend key (sum of both parties' halves) and
// sweeps all funds to destAddr. Returns the sweep txid.
func (w *ExchangeWallet) SweepSharedAddress(ctx context.Context, swapID,
	sharedAddr, spendKeyHex, viewKeyHex string, restoreHeight uint64,
	destAddr string) (txID string, err error) {

	if !cxmr.AddressValid(sharedAddr, dexNetworkToCgo(w.net)) {
		return "", fmt.Errorf("shared address invalid")
	}
	if !cxmr.AddressValid(destAddr, dexNetworkToCgo(w.net)) {
		return "", fmt.Errorf("destination address invalid")
	}
	if _, err := hex.DecodeString(spendKeyHex); err != nil {
		return "", fmt.Errorf("bad spend key hex: %w", err)
	}
	if _, err := hex.DecodeString(viewKeyHex); err != nil {
		return "", fmt.Errorf("bad view key hex: %w", err)
	}

	password := viewKeyHex
	walletFile := filepath.Join(w.dataDir, "swap_sweep_"+swapID)

	w.log.Infof("SweepSharedAddress: creating sweep wallet for swap %s (restore height %d)",
		swapID, restoreHeight)
	sweep, err := w.wm.CreateWalletFromKeys(
		walletFile, password, "English",
		dexNetworkToCgo(w.net), restoreHeight,
		sharedAddr, viewKeyHex, spendKeyHex,
	)
	if err != nil {
		return "", fmt.Errorf("create sweep wallet: %w", err)
	}
	defer w.wm.CloseWallet(sweep, true)

	if !sweep.Init(w.daemonAddr, w.daemonUser, w.daemonPass, false, false, "") {
		return "", fmt.Errorf("sweep wallet init: %s", sweep.ErrorString())
	}
	if !sweep.ConnectToDaemon() {
		return "", fmt.Errorf("sweep wallet connect: %s", sweep.ErrorString())
	}
	// Simnet monerod starts at hard fork v16 from block 0 while the
	// wallet expects the mainnet hard-fork schedule. Without these two
	// calls wallet2's refresh loop exits silently and the wallet stays
	// stuck at the restore height. Same handling as the main wallet's
	// Connect (xmr.go).
	if w.net == dex.Simnet {
		sweep.SetTrustedDaemon(true)
		sweep.SetAllowMismatchedDaemonVersion(true)
	}
	sweep.SetRecoveringFromSeed(true)
	sweep.SetRefreshFromBlockHeight(restoreHeight)
	sweep.SetAutoRefreshInterval(5000)
	sweep.StartRefresh()

	// Block until the shared-address output shows up in the sweep
	// wallet as an unlocked (spendable) balance. Polling the balance
	// is more reliable than wallet2's Synchronized() flag, which does
	// not always flip true for freshly-created view+spend wallets
	// even after the refresh thread has caught up.
	w.log.Infof("SweepSharedAddress: waiting for unlocked balance (swap %s)", swapID)
	unlocked, err := waitForUnlockedBalance(ctx, sweep, w.log, swapID, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("sweep wallet sync: %w", err)
	}
	w.log.Infof("SweepSharedAddress: unlocked balance ready (swap %s); unlocked=%d", swapID, unlocked)

	// Force a synchronous refresh so wallet2's per-subaddress output
	// table is up to date before SweepAll's internal checks run.
	sweep.Refresh()
	tx, err := sweep.SweepAll(destAddr, w.feePriority, 0)
	if err != nil {
		// wallet2's ignore_fractional_outputs (default on, no C
		// binding to disable) filters outputs whose value is less
		// than the fee cost of spending them. Surfaces as
		// "No unlocked balance in the specified subaddress(es)"
		// even when UnlockedBalance is positive. Requires a larger
		// swap amount, not a retry.
		return "", fmt.Errorf("sweep: %w", err)
	}
	w.log.Infof("SweepSharedAddress: SweepAll built (swap %s); committing", swapID)
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("sweep commit: %w", err)
	}
	w.log.Infof("SweepSharedAddress: committed sweep tx %s (swap %s)", tx.TxID(), swapID)

	m := swapMap(w)
	swapWalletMapMu.Lock()
	if set := m[swapID]; set != nil {
		set.sweep = nil
	}
	delete(m, swapID)
	swapWalletMapMu.Unlock()

	return tx.TxID(), nil
}

// waitForUnlockedBalance polls account 0's unlocked balance until it
// is positive or the timeout elapses. Matches the approach the
// standalone btcxmrswap CLI uses (poll wallet-rpc GetBalance), which
// is more reliable than wallet2's Synchronized() flag for
// freshly-created view+spend wallets. Logs balance + chain-height
// progression every 10 seconds so the operator can tell whether the
// wallet is scanning (chain heights advancing), the output was seen
// but not yet mature (Balance > 0, UnlockedBalance == 0), or the
// refresh thread is stalled (heights / balance both stuck at 0).
func waitForUnlockedBalance(ctx context.Context, w *cxmr.Wallet, log dex.Logger,
	swapID string, timeout time.Duration) (uint64, error) {
	deadline := time.Now().Add(timeout)
	var lastProgressLog time.Time
	for {
		bal := w.Balance(0)
		unlocked := w.UnlockedBalance(0)
		if unlocked > 0 {
			return unlocked, nil
		}
		if time.Since(lastProgressLog) > 10*time.Second {
			log.Infof("SweepSharedAddress: swap %s progress: balance=%d unlocked=%d "+
				"walletHeight=%d daemonHeight=%d synced=%t",
				swapID, bal, unlocked,
				w.BlockChainHeight(), w.DaemonBlockChainHeight(), w.Synchronized())
			lastProgressLog = time.Now()
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("timed out (balance=%d unlocked=%d walletHeight=%d daemonHeight=%d)",
				bal, unlocked, w.BlockChainHeight(), w.DaemonBlockChainHeight())
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Ensure helper variables are not flagged unused during partial
// builds.
var _ = dex.Network(0)
