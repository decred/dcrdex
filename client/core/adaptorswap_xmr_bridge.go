//go:build xmr

// XMR-side adapter for the adaptor-swap orchestrator. Wraps the
// build-tagged client/asset/xmr ExchangeWallet so it satisfies
// adaptorswap.XMRAssetAdapter without exposing cgo-specific types
// to the rest of client/core.
//
// Symmetric to BTCRPCAdapter in adaptorswap_bridge.go, but here the
// underlying primitives live on a Wallet object rather than an RPC
// client because the XMR side accesses the wallet2 library
// in-process via cgo.

package core

import (
	"context"

	"decred.org/dcrdex/client/asset/xmr"
	"decred.org/dcrdex/client/core/adaptorswap"
)

// XMRWalletAdapter implements adaptorswap.XMRAssetAdapter by
// delegating to an *xmr.ExchangeWallet's swap primitives. The ctx
// stored at construction is used for every wallet call; pass a
// long-lived context (typically Core's run context) so cancellation
// during shutdown propagates into in-flight wallet calls.
type XMRWalletAdapter struct {
	w   *xmr.ExchangeWallet
	ctx context.Context
}

// NewXMRWalletAdapter returns an adapter bound to w. ctx is the
// context used for every underlying ExchangeWallet swap call.
func NewXMRWalletAdapter(ctx context.Context, w *xmr.ExchangeWallet) *XMRWalletAdapter {
	return &XMRWalletAdapter{w: w, ctx: ctx}
}

var _ adaptorswap.XMRAssetAdapter = (*XMRWalletAdapter)(nil)

func (a *XMRWalletAdapter) SendToSharedAddress(addr string, amount uint64) (string, uint64, error) {
	return a.w.SendToSharedAddress(a.ctx, addr, amount)
}

func (a *XMRWalletAdapter) WatchSharedAddress(swapID, addr, viewKeyHex string,
	restoreHeight, expectedAmount uint64) (adaptorswap.XMRWatch, error) {

	h, err := a.w.WatchSharedAddress(a.ctx, swapID, addr, viewKeyHex, restoreHeight, expectedAmount)
	if err != nil {
		return nil, err
	}
	// *xmr.XMRWatchHandle already has the methods adaptorswap.XMRWatch
	// requires (Synced, HasFunds, Close), so it satisfies the interface
	// directly. No adapter struct needed.
	return h, nil
}

func (a *XMRWalletAdapter) SweepSharedAddress(swapID, addr, spendKeyHex, viewKeyHex string,
	restoreHeight uint64, dest string) (string, error) {

	return a.w.SweepSharedAddress(a.ctx, swapID, addr, spendKeyHex, viewKeyHex, restoreHeight, dest)
}
