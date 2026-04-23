# XMR Wallet Audit for Adaptor Swap Support

Scope: inventory `client/asset/xmr/` and its cgo layer `client/asset/xmr/cxmr/`
against the primitives a BTC/XMR adaptor swap needs. Identify what exists,
what is partially there, and what is missing.

## What the adaptor swap actually needs from the XMR wallet

Cross-referencing `internal/cmd/xmrswap/main.go` and the
`PROTOCOL.md` flow, the XMR wallet's job splits into four
primitives, of which only some are needed by each party:

| Primitive | Alice (XMR holder) | Bob (BTC holder) |
|---|---|---|
| Send XMR to arbitrary address | Yes (fund shared addr) | No |
| Watch arbitrary address for incoming output | No (she sent it, knows txid) | Yes (verify Alice locked XMR) |
| Restore a spendable wallet from known spend+view keys | Yes (sweep back on refund) | Yes (sweep forward on success) |
| Produce/consume a key-derivation scalar | Only at the protocol layer, not wallet | Same |

Bob only needs `watch` and `sweep` - he does not need to fund or hold any
XMR in his normal wallet. But he does need an XMR daemon connection. That
is an architectural consequence: BTC-holders trading on BTC/XMR markets
must have the XMR asset enabled in their client.

## Wired up (usable as-is)

These cgo primitives already exist and work:

### Wallet creation from raw keys
- `WalletManager.CreateWalletFromKeys(path, password, language, net, restoreHeight, address, viewKey, spendKey)` (cxmr/wallet.go:341). Creates a view-only wallet when `spendKey` is empty, or a full wallet when both keys are provided.
- `WalletManager.CreateDeterministicWalletFromSpendKey(...)` (cxmr/wallet.go:374). Already used for the primary wallet. Suitable for restoring a sweep wallet when both spend key halves are known and summed.

### Chain interaction
- `Wallet.CreateTransaction(destAddr, amount, priority, accountIndex)` (cxmr/wallet.go:709). Send to arbitrary address - used for Alice funding the shared address.
- `Wallet.SweepAll(destAddr, priority, accountIndex)` (cxmr/wallet.go:741). Sweep entire unlocked balance to one address - used for the sweep phase.
- `Wallet.Balance(accountIndex)`, `UnlockedBalance(accountIndex)` (cxmr/wallet.go:565, 570). Check that the shared address is funded.
- `Wallet.AllCoins()` (cxmr/wallet.go:786). Enumerate outputs; needed to confirm a specific amount landed at the shared address with enough confirmations.
- `Wallet.SetRecoveringFromSeed(bool)` and `SetRefreshFromBlockHeight(height)` (cxmr/wallet.go:560, 548). Fast-restore sweep wallets without rescanning the full chain - critical for swap latency.
- `Wallet.Synchronized()`, `BlockChainHeight()`, `DaemonBlockChainHeight()` (cxmr/wallet.go:528, 533, 538). Sync state.

### Validation
- `cxmr.AddressValid(address, net)` (cxmr/wallet.go:618). Base58 and network-byte validation on arbitrary addresses.
- `Wallet.WatchOnly()` (cxmr/wallet.go:699). Query whether a wallet is view-only.

### High-level (ExchangeWallet, `xmr.go`)
- `ExchangeWallet.Send(address, value, feeRate)` (xmr.go:800) - wraps CreateTransaction. Works for the XMR-holder's funding tx.
- `ExchangeWallet.Rescan` (xmr.go:1188) - supports manual re-sync. Present but not needed for swap flow.
- `ExchangeWallet.PrimaryAddress`, `NewAddress`, `GenerateSubaddresses`, etc. - standard deposit address helpers. Not directly used by adaptor swap.

## Wired but needs adaptation

### Single-wallet assumption in ExchangeWallet
`ExchangeWallet` holds one `*cxmr.Wallet` guarded by `walletMtx` (xmr.go:278-282). Adaptor swaps require, per live swap:

- The primary wallet (for funding, Alice only).
- A view-only "watch" wallet for the shared address (both parties, though in practice only Bob actively needs it - Alice verifies her own send via txid).
- A spendable "sweep" wallet created once the full spend key is known (for the final sweep out of shared address).

These extra wallets are needed concurrently and can outlive individual RPC calls. The struct needs a per-swap wallet map:

```go
swapWallets map[swapID]*swapWalletSet
// where swapWalletSet is { watch, sweep *cxmr.Wallet }
```

Monero_c's `WalletManager` is process-global (one `wm.ptr`) but each `Wallet` has its own `ptr`. Multiple open `Wallet` objects on one `WalletManager` is expected to work but needs verification on the monero_c side (TODO before relying on it).

### Chain height capture at send time
The reference (xmrswap main.go:1138) calls `alice.xmr.GetHeight(ctx)` immediately before `alice.initXmr`, so the sweep wallet can later be restored from that block. `ExchangeWallet` uses `BlockChainHeight()` internally but does not expose it. Trivial to add.

### AllCoins on a view-only wallet
To confirm "has N XMR arrived at shared address X with K confirms," Bob needs:
1. Open view-only wallet from (X, viewKey, recentHeight).
2. Sync to tip.
3. Check `AllCoins()` or `UnlockedBalance(0)` reports an unspent output of the expected amount.

The plumbing for (1)-(3) exists but not as a single high-level API. It is a straightforward composition.

## Missing (will need to be built)

### 1. Per-swap wallet lifecycle management
None. `ExchangeWallet` has no notion of a swap ID or auxiliary wallets. Need:

- A tempdir naming scheme for per-swap wallet files (keep them out of the primary wallet's directory).
- Create/open/close helpers that do NOT touch `w.wallet`.
- Cleanup on swap completion or failure.

### 2. Adaptor-swap-shaped primitives at the ExchangeWallet level
No API layer for the swap protocol to call. Rough sketch of what is needed (probably a new file `client/asset/xmr/swap.go`):

```go
// Fund: Alice sends XMR to the shared address.
// Returns the txid and the chain height at send time (for sweep-wallet restore).
func (w *ExchangeWallet) SendToSharedAddress(
    ctx context.Context, addr string, amount uint64,
) (txID string, sentAtHeight uint64, err error)

// Watch: open a view-only wallet for a shared address and return a handle.
// Concurrent: the handle can be queried while the main wallet keeps working.
func (w *ExchangeWallet) WatchSharedAddress(
    ctx context.Context, swapID string,
    addr, viewKey string, restoreHeight uint64,
) (*WatchHandle, error)

// WatchHandle: ask whether an expected amount has arrived with enough confs.
func (h *WatchHandle) HasUnspentOutput(
    amount uint64, minConfs uint32,
) (present bool, confirmed uint32, err error)
func (h *WatchHandle) Close() error

// Sweep: given the summed spend scalar and view key, open a spendable wallet
// at the shared address, sync, and sweep to dest. Returns the sweep txid.
func (w *ExchangeWallet) SweepSharedAddress(
    ctx context.Context, swapID string,
    spendKey, viewKey string, restoreHeight uint64, destAddr string,
) (txID string, err error)
```

None of these exist today. Underlying cgo primitives do.

### 3. Per-swap ed25519 key generation (belongs at protocol layer, not wallet)
Not a wallet gap, but a design note: the dcrdex seed drives the main spend
key. Adaptor swaps need FRESH per-swap ed25519 key material so that (a)
published adaptor tweaks don't leak wallet secrets, (b) repeated swaps
don't reuse keys. These should be generated at the swap state machine
level, not derived from the wallet. The XMR wallet only sees them when
creating per-swap watch/sweep wallets from raw keys.

### 4. Scalar sum helper
Given two ed25519 secret scalars, compute `(a + b) mod L` and hex-encode
for `CreateDeterministicWalletFromSpendKey`. The reference does this inline
(main.go:943-949 via `scalarAdd` and `edwards25519.FeAdd`). This should be
a package-level helper in `internal/adaptorsigs` or a new `internal/xmrkeys`
package, not in the wallet. Also not a gap here - just flagging it.

### 5. Shared-address derivation helper
Given two edwards.PublicKey (the two spend key halves summed) and an edwards
view pubkey, produce the base58-encoded Monero standard address for the
correct network. The reference does this inline (main.go:952-955 using
`base58.EncodeAddr(netTag, fullPubKey)` from the haven-protocol-org base58
library, which is already an indirect dep via the existing xmrswap tool).

Should live next to the scalar-sum helper. Not a gap in the wallet package.

### 6. Robust confirmation count on a non-owned output
`ConfirmTransaction` exists at xmr.go:1406 but is stubbed. For swap flow
we need per-output confirmation count on an output in a view-only wallet,
which is a different shape than the main-wallet tx confirmation check.
Needs `AllCoins()` filtered by subaddr + unspent + confs calculation.

## Risks and unknowns

### monero_c multi-wallet concurrency
Nothing in the cxmr package tests or uses more than one `*Wallet` at a
time. Opening two `Wallet` objects on the same `WalletManager` in parallel
and calling Refresh/Balance on each may have thread-safety issues at the
wallet2 layer. **Must verify with a small test** before committing to
concurrent per-swap wallets. If it does not work, options are:

- Serialize all wallet operations through a single goroutine.
- Use one `WalletManager` per auxiliary wallet (heavier but isolated).
- Use monero-wallet-rpc out-of-process per watch/sweep wallet (what the
  reference does - main.go uses multiple `bisoncraft/go-monero/rpc` clients
  over HTTP to separate monero-wallet-rpc processes).

The last option is what the xmrswap reference actually does. For dcrdex
integration, cgo is preferred to avoid the wallet-rpc dependency, so we
should confirm concurrency works.

### Daemon sharing
All auxiliary wallets should share `daemonAddr`. If the daemon is a public
node (the mainnet default is `xmr-node.cakewallet.com`), that node sees
every view-only wallet's scan queries, which is a privacy concern. Mitigate
by strongly recommending a private daemon in swap-enabled configurations.

### Stagenet vs testnet bug
`xmr.go:256-258` notes that monero_c has address validation bugs on
stagenet, so dcrdex uses testnet instead. For swap testing the chosen
network must be consistent across BTC and XMR sides.

### Dust subtraction
The ExchangeWallet caches a dust total and subtracts it from reported
balance (xmr.go:297-305) so users do not try to send unsendable amounts.
Not a problem for primary-wallet funding, but the sweep wallet from the
shared address may contain exactly one output - there is nothing to be
"dust" relative to. The sweep path should not go through the dust
subtraction logic.

## Gap count, summary

- **2 high-level ExchangeWallet APIs to design** (watch, sweep) plus one extension (send with height capture).
- **1 struct change** (multi-wallet lifecycle in ExchangeWallet).
- **3 small helpers** (scalar sum, shared-address derivation, confirm-by-output) that belong outside the wallet package.
- **1 verification item** (monero_c multi-wallet concurrency) before relying on in-process auxiliary wallets.
- **0 cgo primitives missing.** Every chain-interaction primitive needed is already exposed.

That last point is the significant one: we do not need to extend the cgo
bindings. The work is Go-level glue code + protocol-level crypto helpers.

## Recommended next steps (after BIP340 adaptor sigs work)

1. Small verification test: open two view-only wallets concurrently on the
   simnet harness, confirm both sync and report balances independently.
2. Write `client/asset/xmr/swap.go` with the three primitives sketched
   above (`SendToSharedAddress`, `WatchSharedAddress`, `SweepSharedAddress`)
   against the cgo layer. Include unit tests using two simnet daemons.
3. Protocol-layer helpers for scalar sum and address derivation go in a
   new `internal/xmrkeys/` or extend `internal/adaptorsigs/`.
4. Do NOT attempt to fill in the `Swap/AuditContract/Redeem/Refund` stubs
   in `xmr.go:1354-1414` using the existing HTLC-shaped interface. That
   interface does not fit adaptor swaps; the new swap type will route
   through the new primitives above, not through the HTLC methods.
