# Adaptor-swap simnet runbook

This documents how to run a BIP-340 adaptor-signature BTC/XMR swap end-to-end on
simnet through the dcrdex server (not the standalone `internal/cmd/btcxmrswap`
CLI, which exercises the cryptography but bypasses the DEX).

## Status (2026-04-23) - end-to-end validated with sweep

A full adaptor swap completed on simnet through this runbook, including the
initiator's XMR sweep from the shared address:

```
CORE[xmr]: SweepSharedAddress: unlocked balance ready ... unlocked=100000000000
CORE[xmr]: SweepSharedAddress: SweepAll built ...; committing
CORE[xmr]: SweepSharedAddress: committed sweep tx ...
CORE: Adaptor swap complete for match ... on xmr_btc
```

Trade amount was 0.1 XMR (`qty=100_000_000_000`). See "Known gotchas"
below for the dust-threshold floor that governs the minimum workable
swap size.

What is wired, end-to-end:

- Operator config: `markets.json` accepts `swapType: "adaptor"`,
  `scriptableAsset`, `lockBlocks`.
- Server: `Swapper.NegotiateAdaptor` dispatches matched adaptor pairs to
  `AdaptorCoordinators` instead of HTLC `matchTracker`. All 10 `adaptor_*`
  routes are registered on `AuthManager` when adaptor coords are configured.
  `server/dex` builds and installs the coordinator pool on the Swapper
  config, with a PeerRouter that maps outbound `(matchID, role)` messages to
  the user account the match was started with.
- Client: `Core.adaptorMgr` instantiated at `New`, `adaptor_*` routes
  registered in `noteHandlers`, `negotiateMatches` diverts adaptor markets to
  `AdaptorSwapManager.StartSwap`. Outbound messages are signed with the
  account key before dispatch so the server-side `authUser` check passes.
- Wire path: `dcSender` pushes outbound notifications through
  `dexConnection.WsConn.Send`; inbound flows via `handleAdaptorMsg` →
  `manager.Handle` → orchestrator.
- All three wallet-dependent `Config` fields are sourced at swap setup:
  `XmrNetTag` from `dex.Network`, `OwnXMRSweepDest` from local XMR wallet,
  `OwnBTCPayoutAddr` from local BTC wallet (rides on `AdaptorSetupPart`).
- Auto-advance through lock + xmr-confirmation phases (no chain-watcher needed
  for a trust-mode simnet run).
- BTC spend observer: bridge spawns a goroutine on `PhaseSpendPresig` that
  polls `assetBTC.ObserveSpend` and feeds the witness back so the initiator
  can `RecoverTweakBIP340` and sweep XMR.
- Terminal-phase callback: logs swap completion, marks the order
  `OrderStatusExecuted`, unregisters the orchestrator.
- XMR wallet implements `asset.Authenticator` with no-op `Unlock`/`Lock` and
  `Locked() = !isOpen`, so xcWallet's unlock flow caches the decrypted
  password and `locallyUnlocked()` stops returning false.
- `server/asset/xmr.CheckSwapAddress` validates addresses against the
  configured network tags (18/42/19 for mainnet+simnet, 53/63/54 for
  testnet), so order intake no longer rejects XMR redemption addresses.

What is **not** wired and limits what you can do today:

- **Persistence**: both client and server use `NoopPersister`. State
  survives in-memory but a process restart drops every in-flight swap.
  Resume code paths exist (`NewOrchestratorFromState`,
  `NewCoordinatorFromState`) but nothing writes to disk by default.
  However, the client's HTLC match records *are* persisted to bolt.db, so
  after a restart the HTLC ticker will try to tick resurrected adaptor
  matches and spam "counterparty address never received" every 3 minutes
  until the match self-revokes. See "Known gotchas" below.
- **Server BTC/XMR auditors**: `BTCAuditor` / `XMRAuditor` interfaces exist
  but no production implementation. The server trust-skips chain audits
  (acceptable for simnet, NOT for production). Coordinator auto-advances on
  receipt of `EventLocked` / `EventXmrLocked` when no auditor is configured.
- **UI**: the web frontend has zero adaptor-swap awareness. You drive orders
  via `bwctl trade` (or the `Trade()` API directly).
- **Server-side restore on startup**: even with persistence, the server's
  `NewSwapper` doesn't iterate adaptor matches and rebuild coordinators yet.
- **Order-intake validation**: Option-1 enforcement covers standing limit
  orders only; market orders / IoC limits / cancels for adaptor markets need
  audit. The participant-side intake currently goes through a HACK path
  (synthetic stub coin + server-side bypass in `processTrade`) rather than
  a proper adaptor-aware funding protocol.
- **Client BTC wallet integration**: bisonw's native BTC wallets (SPV,
  Electrum, RPC) don't expose their `rpcclient.Client` to the adaptor
  bridge, and the SPV variant has no arbitrary-tx-funding primitive at
  all. The adaptor flow side-channels a separate bitcoind connection via
  env vars. See "Known gotchas" below.

## Prerequisites

Three harnesses must be running:

```
dex/testing/btc/harness.sh        # BTC simnet (alpha rpc 20556, beta 20557)
dex/testing/xmr/harness.sh         # monerod regtest + wallet-rpc on 28184/28284/28484
dex/testing/dcr/harness.sh         # DCR simnet (still needed for the
                                   # dcrdex harness's DEX bond asset)
```

Build dcrdex and bisonw with the `xmr` tag so the cgo monero_c bindings
compile. Both sides need the `-tags xmr` build:

```
go build -tags xmr -o dcrdex ./server/cmd/dcrdex
go build -tags xmr -o bisonw ./client/cmd/bisonw
go build -tags xmr -o bwctl  ./client/cmd/bwctl
```

## markets.json

The dcrdex harness's `genmarkets.sh` emits an HTLC-only config. Hand-edit the
generated file (or generate via a sibling `genmarkets-adaptor.sh`) to add the
btc_xmr adaptor market:

```json
{
  "markets": [
    {
      "base": "btc",
      "quote": "xmr",
      "lotSize": 100000,
      "rateStep": 100,
      "epochDuration": 20000,
      "parcelSize": 1,
      "marketBuyBuffer": 1.5,
      "swapType": "adaptor",
      "scriptableAsset": "btc",
      "lockBlocks": 2
    }
  ],
  "assets": {
    "btc": {
      "bip44symbol": "btc",
      "network": "simnet",
      "maxFeeRate": 100,
      "swapConf": 1,
      "configPath": "/path/to/btc/alpha/alpha.conf"
    },
    "xmr": {
      "bip44symbol": "xmr",
      "network": "simnet",
      "maxFeeRate": 100,
      "swapConf": 1,
      "configPath": "/path/to/xmr/alpha/alpha.conf"
    }
  }
}
```

`lockBlocks: 2` matches what the `internal/cmd/btcxmrswap` reference uses on
simnet.

## Running the server

`./harness.sh` doesn't currently pass `-tags xmr` through, so launch dcrdex
directly:

```
./dcrdex --simnet ...
```

The server reads `markets.json`, loads the XMR backend (needs the tag), and
instantiates `AdaptorCoordinators` with trust-mode defaults (nil BTC/XMR
auditors, NoopReporter, NoopPersister).

## Running the clients

Two clients, typically two `bisonw` instances pointing at separate data
dirs. Each must have BTC and XMR wallets configured + connected. The XMR
config points the wallet at the harness's `monero-wallet-rpc` (port 28184
for one client, 28284 for the other; the third 28484 is for sweep wallets).

**Before starting each bisonw**, export the BTC adapter env vars. The
adaptor flow builds a Taproot 2-of-2 lock transaction using bitcoind's
`FundRawTransaction` + `SignRawTransactionWithWallet`, which bisonw's
native BTC wallet can't do. The BTC side-channel talks to a separate
bitcoind RPC:

```bash
# initiator machine — point at alpha harness default wallet
export DCRDEX_ADAPTOR_BTC_RPC='127.0.0.1:20556/wallet/'
export DCRDEX_ADAPTOR_BTC_USER=user
export DCRDEX_ADAPTOR_BTC_PASS=pass
./bisonw --simnet

# participant machine — point at beta harness default wallet
export DCRDEX_ADAPTOR_BTC_RPC='127.0.0.1:20557/wallet/'
export DCRDEX_ADAPTOR_BTC_USER=user
export DCRDEX_ADAPTOR_BTC_PASS=pass
./bisonw --simnet
```

The `/wallet/<name>` URL suffix disambiguates Bitcoin Core's JSON-RPC when
multiple wallets are loaded (the harness loads the default unnamed wallet
plus `gamma` on alpha, `delta` on beta). Trailing `/wallet/` targets the
unnamed default wallet, which has the most funds; `/wallet/gamma` or
`/wallet/delta` works as a fallback.

Sanity-check the path before driving a swap:

```bash
curl -su user:pass --data-binary \
  '{"jsonrpc":"1.0","id":"x","method":"getbalance","params":[]}' \
  -H 'content-type: text/plain;' \
  http://127.0.0.1:20556/wallet/
```

A numeric `result` means the URL is correct.

## Driving an order match

There is no UI for adaptor markets. Drive the orders via `bwctl trade`
(positional args: `host isLimit sell base quote qty rate tifnow
[optionsJSON]`). With `xmr_btc` market (base=xmr, quote=btc,
scriptableAsset=btc), Option 1 requires the BTC holder to be the maker:

| Side | Role | `sell` | `tifnow` | Notes |
|---|---|---|---|---|
| BTC seller = XMR buyer | maker / initiator | `false` | `false` (standing) | scriptable-side, allowed to book |
| XMR seller = BTC buyer | taker / participant | `true` | `true` (immediate) | non-scriptable; Option 1 rejects standing |

```bash
# 1. Maker (BTC holder, initiator). Standing limit.
bwctl --rpcuser="" --rpcpass=abc --rpcaddr=127.0.0.1:5757 \
      --rpccert=~/.dexcsimnet1/rpc.cert \
      trade 127.0.0.1:17273 true false 128 0 <qty> <rate> false

# Wait one epoch (20s for epochDuration: 20000) so the maker books.

# 2. Taker (XMR holder, participant). Immediate limit (tifnow=true).
bwctl --rpcuser="" --rpcpass=abc --rpcaddr=127.0.0.2:5760 \
      --rpccert=~/.dexcsimnet2/rpc.cert \
      trade 127.0.0.1:17273 true true 128 0 <qty> <rate> true
```

`<qty>` is in base atoms (XMR piconero, 1 XMR = 1e12 piconero) and must be
a multiple of `lotSize`. `<rate>` must be a multiple of `rateStep` and must
cross on both sides (buyer's bid ≥ seller's ask). Keep `qty` and `rate`
identical on both sides for the simplest first run.

**XMR dust threshold**: pick `<qty>` large enough that the XMR output at
the shared address is sweepable. monerod's `ignore_fractional_outputs`
(default on, no `monero_c` binding to disable) rejects any sweep input
whose value is below the marginal fee cost of spending it. At default
fee priority that threshold is on the order of 10^7 piconero
(0.00001 XMR). Comfortable test amounts: `qty = 10_000_000_000`
(0.01 XMR) or higher. Going below ~10^9 piconero leaves the swap's XMR
stranded at the shared address - the participant's send succeeds but
the initiator's sweep errors with "No unlocked balance in the
specified subaddress(es)".

## Expected log progression

Server:

```
DBG: NegotiateAdaptor: sending 'match' ack request to user X for 1 matches
DBG: NegotiateAdaptor: sending 'match' ack request to user Y for 1 matches
... (coordinator state machine logs phase transitions)
```

Client A (initiator, BTC holder, XMR buyer):

```
INF: Adaptor swap started for match XX on xmr_btc (role=initiator, ...)
... (orchestrator transitions through PhaseKeysSent, PhaseKeysReceived,
     PhaseLockBroadcast (briefly), PhaseLockConfirmed, PhaseXmrConfirmed,
     PhaseSpendPresig, PhaseXmrSwept, PhaseComplete)
INF: Adaptor swap complete for match XX on xmr_btc
```

Client B (participant, XMR holder, BTC buyer):

```
INF: Adaptor swap started for match XX on xmr_btc (role=participant, ...)
... (PhaseKeysSent, PhaseRefundPresigned, PhaseLockConfirmed, PhaseXmrSent,
     PhaseSpendBroadcast, PhaseComplete)
INF: Adaptor swap complete for match XX on xmr_btc
```

## What "complete" means today

- Initiator's BTC was paid to the participant via `spendTx` (broadcast by
  participant; observable on the BTC harness via
  `bitcoin-cli -regtest gettxout <spendtxid> 0`).
- Initiator swept XMR from the shared address to its `OwnXMRSweepDest`
  (observable via the third XMR wallet-rpc).
- Both orders' `metaData.Status` updated to `OrderStatusExecuted`.

## Known gotchas (simnet hacks)

These are places the current branch takes shortcuts. Each is tracked for
follow-up work; see the README TODO list.

### BTC side-channel env vars

The adaptor orchestrator funds the Taproot lockTx via a separate
bitcoind RPC, not via bisonw's configured BTC wallet (SPV / Electrum /
RPC). Every bisonw that starts an adaptor swap needs the three env vars
in its own process environment; a desktop-launcher shortcut or systemd
unit that inherits only a minimal env will not pick them up from your
interactive shell. Verify with `env | grep DCRDEX_ADAPTOR` before launch.

If any of the three vars is missing, a new adaptor match logs

```
adaptor match <id>: BTC adapter unavailable; set DCRDEX_ADAPTOR_BTC_{RPC,USER,PASS}
```

and the match is skipped (no panic).

### Multi-wallet bitcoind URL path

Bitcoin Core refuses `FundRawTransaction` with error `-19 Multiple
wallets are loaded` unless the URL includes `/wallet/<name>`. Append
the path segment to `DCRDEX_ADAPTOR_BTC_RPC`:

```
127.0.0.1:20556/wallet/        # default (unnamed) wallet on alpha
127.0.0.1:20556/wallet/gamma   # named secondary on alpha
```

`./alpha listwallets` and `./beta listwallets` in
`dex/testing/btc/harness-ctl/` show what's loaded on each node.

### bolt.db persistence carries stale matches across restarts

There's no adaptor-swap state persistence yet, but the client *does*
write an HTLC-shaped match record to bolt.db on every new match. After a
bisonw restart, that record gets restored into the HTLC tracker and the
tick loop fires "Match X: counterparty address never received after
3m0s, self-revoking" every 3 minutes until the match self-cancels.

For a clean run, stop both bisonws and wipe their simnet DBs:

```
rm ~/.dexc/simnet/dexc.db             # client A
rm ~/.dexcsimnet2/simnet/dexc.db      # client B
```

You'll have to re-register (post bond) on next startup. Do this between
runs until adaptor persistence lands (README TODO #1).

### Participant order-intake uses stub coins

`bisonw` places a participant order with a synthetic XMR "coin"
(`xmr-adaptor-participant---------` padded to 32 bytes) and zero-byte
signature. The server's `processTrade` detects adaptor-market
non-scriptable funding and skips coin validation entirely. This is
enough to get a swap through but it's labeled `HACK:` in the commits
and has no replay / anti-spam protection. Real adaptor-aware intake is
tracked in README TODO #5.

### XMR wallet "not unlocked" errors

Resolved in this branch by making `xmr.ExchangeWallet` satisfy
`asset.Authenticator` with no-op `Unlock`/`Lock` and `Locked() =
!isOpen()`. If you see "xmr wallet is not unlocked" in the log on a
fresh build, confirm the branch includes the HACK commit's folded
fixup that adds those three methods.

## Known fragility points to watch

- The participant's `SendToSharedAddress` blocks until the XMR tx is accepted.
  On a slow XMR daemon this can timeout; the orchestrator does not retry.
- The initiator's `SpendObserver` polls indefinitely. If the participant
  never broadcasts (e.g. died after `PhaseRefundPresigned`), the goroutine
  leaks until process exit. There is no per-swap context cancellation yet.
- On restart, all in-flight swaps are dropped (no persistence). For a
  multi-step manual test, complete each swap in one process lifetime.

## When this runbook will be obsolete

The runbook becomes mostly unnecessary once the following land:

- A `genmarkets-adaptor.sh` that emits the right `markets.json` automatically.
- The harness passes `-tags xmr` through so adaptor markets just work with
  `./harness.sh`.
- Persistence is wired so multi-process / restart scenarios are first-class.
- bisonw's BTC wallet gains a Taproot-funding entry point (or exposes
  its `rpcclient.Client`) so the env-var side-channel goes away.
- A frontend that exposes adaptor swap status.

Until then this is the authoritative path to a working simnet swap.
