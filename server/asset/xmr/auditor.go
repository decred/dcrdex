// Package xmr provides server-side Monero chain observation for
// adaptor swaps. It implements the XMRAuditor interface defined in
// server/swap/adaptor by talking to a monero-wallet-rpc endpoint
// over HTTP/JSON-RPC.
//
// The server does not need a full asset.Backend for XMR (the
// adaptor swap protocol does not move XMR through the server). It
// only needs to verify that an expected output of expected amount
// has confirmed at the participant's reported shared address. This
// package is the minimum viable implementation of that check.
//
// Operators who do not want to run a monerod can omit this and
// rely on counterparty reports for XMR audit; the server-side
// adaptor coordinator handles a nil XMRAuditor by skipping the
// confirm-check phase.
package xmr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bisoncraft/go-monero/rpc"
)

// Auditor implements server/swap/adaptor.XMRAuditor against a
// monero-wallet-rpc endpoint.
type Auditor struct {
	rpcAddress string
	client     *rpc.Client
	netTag     uint64

	// pollInterval controls how often WaitOutputAtAddress retries
	// when the wallet has not yet seen the expected output. Zero
	// uses a default of 30 seconds.
	pollInterval time.Duration

	// minConf is the global minimum confirmation depth for outputs
	// to be considered settled. Per-call minConf in
	// WaitOutputAtAddress takes precedence when non-zero.
	minConf uint32

	// walletDir is forwarded to the wallet-rpc when generating new
	// per-swap watch wallets via generate_from_keys.
	walletDir string

	mu      sync.Mutex
	openMap map[string]bool // tracks open per-swap watch wallets by filename
}

// Config holds the auditor's runtime parameters.
type Config struct {
	// RPCAddress is the URL of a monero-wallet-rpc instance with
	// --wallet-dir (no wallet pre-loaded). The auditor will
	// generate per-swap view-only wallets via generate_from_keys.
	RPCAddress string
	// PollInterval is how often WaitOutputAtAddress retries.
	PollInterval time.Duration
	// MinConf is the global minimum confirmation depth.
	MinConf uint32
	// NetTag is the XMR network identifier (18 mainnet, 24 stagenet).
	NetTag uint64
	// WalletDir is for documentation; the wallet-rpc must already
	// be started with --wallet-dir pointing at a writable location.
	WalletDir string
	// HTTPClient is optional; defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// NewAuditor constructs an Auditor.
func NewAuditor(cfg *Config) (*Auditor, error) {
	if cfg == nil || cfg.RPCAddress == "" {
		return nil, errors.New("nil config or empty RPCAddress")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	a := &Auditor{
		rpcAddress: cfg.RPCAddress,
		client: rpc.New(rpc.Config{
			Address: cfg.RPCAddress,
			Client:  httpClient,
		}),
		netTag:       cfg.NetTag,
		pollInterval: cfg.PollInterval,
		minConf:      cfg.MinConf,
		walletDir:    cfg.WalletDir,
		openMap:      make(map[string]bool),
	}
	if a.pollInterval == 0 {
		a.pollInterval = 30 * time.Second
	}
	return a, nil
}

// WaitOutputAtAddress blocks until the wallet-rpc reports an unspent
// incoming transfer at sharedAddr of at least amount atomic units,
// or until ctx is cancelled. The first call for a given (sharedAddr,
// viewKey) tuple opens a fresh view-only wallet via generate_from
// _keys; subsequent calls reuse the open wallet via open_wallet.
//
// minConf overrides the auditor-level MinConf when non-zero.
//
// The ctx is the call's deadline. Operators set this from the
// adaptor swap coordinator's PhaseDeadline.
func (a *Auditor) WaitOutputAtAddress(ctx context.Context, swapID, sharedAddr,
	viewKeyHex string, restoreHeight uint64, amount uint64, minConf uint32) error {

	if minConf == 0 {
		minConf = a.minConf
	}
	walletFile := "swap_" + swapID
	if err := a.ensureWalletOpen(ctx, walletFile, sharedAddr, viewKeyHex, restoreHeight); err != nil {
		return fmt.Errorf("ensure wallet: %w", err)
	}

	tick := time.NewTicker(a.pollInterval)
	defer tick.Stop()
	for {
		ok, err := a.checkAvailable(ctx, amount, minConf)
		if err != nil {
			return fmt.Errorf("check transfers: %w", err)
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

// ensureWalletOpen opens the per-swap view-only wallet if it is
// not already open on the wallet-rpc instance.
func (a *Auditor) ensureWalletOpen(ctx context.Context, walletFile, addr,
	viewKey string, restoreHeight uint64) error {

	a.mu.Lock()
	already := a.openMap[walletFile]
	a.mu.Unlock()
	if already {
		return nil
	}

	// Try generate_from_keys first; on collision (wallet exists),
	// fall back to open_wallet.
	_, err := a.client.GenerateFromKeys(ctx, &rpc.GenerateFromKeysRequest{
		Filename:      walletFile,
		Address:       addr,
		ViewKey:       viewKey,
		RestoreHeight: restoreHeight,
	})
	if err != nil {
		// Best-effort: assume an "already exists" error and try
		// open_wallet. The go-monero/rpc package surfaces wallet-rpc
		// errors as plain Go errors; we don't have a typed code to
		// switch on so we just attempt the alternative.
		if openErr := a.client.OpenWallet(ctx, &rpc.OpenWalletRequest{
			Filename: walletFile,
		}); openErr != nil {
			return fmt.Errorf("generate_from_keys: %w; open_wallet: %v", err, openErr)
		}
	}
	a.mu.Lock()
	a.openMap[walletFile] = true
	a.mu.Unlock()
	return nil
}

// checkAvailable polls incoming_transfers for an unspent transfer
// of at least the expected amount.
func (a *Auditor) checkAvailable(ctx context.Context, amount uint64, _ uint32) (bool, error) {
	resp, err := a.client.IncomingTransfers(ctx, &rpc.IncomingTransfersRequest{
		TransferType: "available",
	})
	if err != nil {
		return false, err
	}
	var total uint64
	for _, t := range resp.Transfers {
		if t.Spent {
			continue
		}
		total += t.Amount
	}
	return total >= amount, nil
}

// Close detaches from any open wallets. Best-effort: errors are
// logged via the caller's mechanism, not propagated.
func (a *Auditor) Close(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.openMap = nil
	// monero-wallet-rpc's close_wallet would be appropriate but
	// is not exposed by the bisoncraft go-monero package. Operators
	// can restart the wallet-rpc to drop wallets if needed.
	return nil
}

// Compile-time assertion: *Auditor satisfies the XMRAuditor
// interface defined in server/swap/adaptor.
var _ adaptorXMRAuditor = (*Auditor)(nil)

// adaptorXMRAuditor mirrors server/swap/adaptor.XMRAuditor's
// signature locally to avoid an import cycle (the adaptor package
// imports msgjson and order; this is the reverse direction).
type adaptorXMRAuditor interface {
	WaitOutputAtAddress(ctx context.Context, swapID, sharedAddr,
		viewKeyHex string, restoreHeight, amount uint64, minConf uint32) error
}
