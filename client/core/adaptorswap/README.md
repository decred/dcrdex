# adaptorswap

Client-side orchestrator for BIP-340 adaptor-signature atomic swaps between
Bitcoin (Taproot tapscript 2-of-2) and Monero. The package implements the
state machine driven by setup-phase wire messages and chain observations,
delegating chain-specific work to `BTCAssetAdapter` and `XMRAssetAdapter`.

The mirror server-side coordinator lives in `server/swap/adaptor`. The
underlying cryptography lives in `internal/adaptorsigs` and
`internal/adaptorsigs/btc`. Wire types are in `dex/msgjson/adaptor.go`. A
standalone simnet demonstrator that bypasses the DEX server lives in
`internal/cmd/btcxmrswap`. The protocol spec is
`internal/cmd/xmrswap/PROTOCOL.md`. Operator runbook for a simnet swap
through the dcrdex server is `dex/testing/dcrdex/ADAPTOR_SIMNET.md`.

## What works today

- Cryptography (BIP-340 adaptor sigs, DLEQ, Taproot) - validated by unit
  tests + four BIP-340 official vectors.
- Standalone simnet CLI - all four protocol scenarios run green against
  bitcoind regtest 28.1 + the dex/testing/xmr harness.
- DEX server integration: client orchestrator + server coordinator, both
  bridges, market-config `swapType`/`scriptableAsset`/`lockBlocks` fields
  end-to-end, route registration on both sides, real wire transport (no
  more `NoopSender`), per-match `dcSender`/`dcrSender` plumbed.
- All three wallet-dependent `Config` fields are sourced at swap setup:
  `XmrNetTag`, `OwnXMRSweepDest`, `OwnBTCPayoutAddr` (the last rides on
  `AdaptorSetupPart` so the initiator gets the participant's BTC payout
  address before building the spendTx).
- Auto-advance through lock + xmr-confirm transitions so simnet doesn't
  need standalone chain watchers.
- Spend-observation goroutine on `PhaseSpendPresig` so the initiator
  recovers the participant's XMR scalar via `RecoverTweakBIP340`.
- Terminal-phase callback fires order-status update + manager teardown.
- Resume from snapshot is implemented on both sides
  (`NewOrchestratorFromState`, `NewCoordinatorFromState`) but not wired to
  any persister in production.
- Test coverage: ~50 test functions across the orchestrator, coordinator,
  bridges, and message decoders. Setup phase, refund/coop, refund/punish,
  recover-and-sweep, restart-resume, multi-swap isolation, validator
  rejection, server-mediated round-trip.

## What's left for production

In rough order of release-blocking severity. None of the items below
prevents a simnet swap with a trusted counterparty; each becomes
necessary for a production deployment.

### 1. Persistence

- Both `Orchestrator` and `Coordinator` currently use `NoopPersister`.
  Process restart drops every in-flight swap.
- `Orchestrator.Snapshot` / `RestoreState` / `NewOrchestratorFromState`
  exist and round-trip cleanly in tests; just nothing writes to disk.
- Need: a `bolt.DB`-backed `StatePersister` on the client (mirroring the
  HTLC swap persistence), and a `pgsql`-backed `adaptor.StatePersister`
  on the server.
- DB schema additions on the server (`server/db/driver/pg/`): a new
  `adaptor_swaps` table or an extension of the matches table. Existing
  HTLC schema doesn't fit because it stores HTLC-specific columns
  (contract, secret, etc.).
- On the server, `NewSwapper` needs a startup loop analogous to the HTLC
  match restore - load all non-terminal coordinator snapshots, rebuild
  via `NewCoordinatorFromState`, register with the pool, restart any
  watchers.

### 2. Server-side chain auditors

- `BTCAuditor` / `XMRAuditor` interfaces exist on
  `server/swap/adaptor.Config`. Production needs implementations against
  `server/asset/btc` and `server/asset/xmr`.
- Without these, the server trust-skips chain audits: it accepts the
  initiator's `AdaptorLocked` claim (and the participant's
  `AdaptorXmrLocked`) without independent verification. A malicious
  initiator can claim "lockTx broadcast" without actually broadcasting,
  and the participant will then send XMR with no recourse.
- Server needs to feed `EventLockConfirmed` and `EventXmrOutputConfirmed`
  itself, and reject the swap if the wire claim doesn't match what shows
  up on-chain within the timeout window.
- The audit needs to verify both that the lockTx exists at the right
  outpoint AND that its output matches the script the coordinator
  recomputes from the setup-phase pubkeys.

### 3. Client-side chain watchers (strict mode)

- The client's auto-advance through `PhaseLockConfirmed` and
  `PhaseXmrConfirmed` is trust-mode: it accepts the wire claim
  (`AdaptorLocked` / `AdaptorXmrLocked`) without verifying on-chain.
- Production needs strict-mode where `AdaptorLocked` triggers a watcher
  that polls the BTC adapter for confirmation depth before firing
  `EventLockConfirmed`. Same for XMR audit on the participant side.
- Should be a `Config` flag (e.g. `TrustWireClaims bool`) so simnet/test
  runs can keep the cheap path.

### 4. Match outcome → DB / order history

- Today's `OnTerminal` callback only logs and updates
  `trackedTrade.metaData.Status` in memory. No `db.UpdateMatch`, no
  notification, no persistent record of "swap N completed at time T".
- Need: write a match record to the client's bolt DB on terminal
  transitions so the order history page (eventually) can show it. On
  the server, call `swapDone` so reputation/settlement accounting works.
- The DB record shape will overlap with #1 above; design together.

### 5. Order-intake completeness

- Option-1 enforcement (BTC holder must be the maker) is at
  `server/market/orderrouter.go:287` and only covers standing limit
  orders.
- Audit needed for: market orders, immediate TiF limits, cancel orders,
  reused-coin scenarios, partial-fill behavior on adaptor markets.
- The matcher itself doesn't know about adaptor semantics. If two
  market orders match on an adaptor pair with the wrong roles, the
  setup will fail at the orchestrator with a confusing error.

### 6. UI

- Web/desktop frontend has zero adaptor-swap awareness.
- Needs (roughly in order):
  - Wallet enablement prompts: "this market needs both BTC and XMR
    wallets connected, on both sides".
  - Order placement: enforce Option 1 visually (grey out illegal
    sell-non-scriptable + standing-limit combos).
  - Swap status display: render adaptor phases instead of HTLC-shaped
    "swap broadcast / audit pending / redeem broadcast".
  - Refund flow UI: explain coop refund vs. punish branch with the
    asymmetric XMR-forfeiture warning.
  - XMR wallet config wizard: daemon endpoint, restore-from-keys, the
    per-swap watch/sweep wallet model.
  - Translation strings for new error surfaces (DLEQ verify failed,
    leaf-script mismatch, lockTx reorged, etc.).

### 7. Wallet-rpc lifecycle robustness

- `client/asset/xmr/swap.go` opens per-swap watch and sweep wallets via
  `monero_c`. The audit doc flags concurrency concerns: monero_c hasn't
  been verified safe for multiple concurrent open wallets on one
  WalletManager. Need either: a small concurrency test, serialization
  through one goroutine, or out-of-process per-swap wallet-rpc.
- No graceful close of in-flight watch wallets on shutdown. Sweep
  wallets are deleted after a successful sweep but failure paths leave
  files in the data dir.

### 8. Reorg handling

- If lockTx reorgs out after the participant has sent XMR, the
  participant's funds are stuck at the shared address with the
  initiator's spend-key half undisclosed. The orchestrator has no
  reorg-detection logic; production needs it on both sides for the lock,
  refund, and spend phases.

### 9. Operator harness automation

- `dex/testing/dcrdex/harness.sh` doesn't pass `-tags xmr` through and
  doesn't generate an adaptor-aware `markets.json`.
- Need: a `genmarkets-adaptor.sh` sibling that emits the adaptor market
  config, and a `harness.sh` flag (or env var) that enables the xmr
  build tag and starts the XMR + BTC sub-harnesses automatically.
- A scripted `cmd/test-adaptor-swap/main.go` that authenticates two
  test clients and submits a paired pair of orders so the integration
  can be exercised in CI without a human in the loop.

### 10. External security review

- BIP-340 Schnorr adaptor signatures are well-studied but the specific
  combination here (DLEQ to ed25519 + Taproot tapscript 2-of-2 +
  punish-leaf CSV) deserves adversarial scrutiny before mainnet.
- Recommended scope: the cryptography in `internal/adaptorsigs` /
  `internal/adaptorsigs/btc`, the state-machine invariants in
  `client/core/adaptorswap` and `server/swap/adaptor` (especially the
  validators that gate the setup phase), the trust-mode shortcuts when
  auditors are nil, and the message authentication on the
  `adaptor_*` routes.
- Several historical bugs have already been caught by tests (e.g.
  `participantConsumeInitSetup` discarding `FullSpendPub`); the audit
  surface is meaningful.

### 11. Mainnet / testnet deployment plan

- Standalone btcxmrswap CLI runs on simnet only. There is no mainnet or
  testnet plan: no asset registration in
  `server/cmd/dcrdex/markets.json` for production, no decision on
  `lockBlocks` for production (~144 BTC blocks suggested in the
  protocol doc, but unconfirmed), no operator deployment guide.
- The Monero stagenet workaround (network tag 24 instead of 53 due to
  monero_c testnet bugs) limits "testnet" testing to stagenet only.

### 12. XMR dust-threshold floor on adaptor markets

- monerod's `ignore_fractional_outputs` (default on; `monero_c` does not
  expose a binding to disable it) rejects sweep inputs whose value is
  below the marginal fee cost of spending them. Threshold at default
  fee priority is ~10^7 piconero (~0.00001 XMR).
- Consequence for adaptor markets: any swap whose XMR leg lands below
  the threshold leaves XMR stranded at the shared address. The
  participant's send completes; the initiator's `SweepAll` errors with
  "No unlocked balance in the specified subaddress(es)".
- Operators configuring `markets.json` for an XMR-leg adaptor market
  must ensure the minimum trade's XMR-side amount clears the dust
  threshold. For an xmr_btc market (XMR = base) that means
  `lotSize >= ~10^9 piconero` (0.001 XMR), with 10^10 (0.01 XMR) a
  comfortable default. For a btc_xmr market (XMR = quote) the minimum
  is `lotSize_btc * rateStep / 1e8 >= ~10^9 piconero`.
- Order-intake should also validate this at market registration to
  prevent a mis-configured operator from publishing a market whose
  minimum swap strands funds.

## Recommended landing order

If picking this back up, the highest leverage path to a deployable
release:

1. Persistence (#1) - unblocks restart safety, which is a prerequisite
   for any non-toy operator deployment.
2. Server-side BTC + XMR auditors (#2) - the trust-skip is the biggest
   security gap.
3. Client-side strict-mode chain watchers (#3) - companion to #2.
4. Match outcome DB recording (#4) - small, depends on #1.
5. Order-intake hardening (#5) - small.
6. Operator harness automation (#9) - makes everything else testable.
7. UI (#6) - large parallel effort that can run alongside.
8. Reorg handling (#8) - probably wants to land with auditors (#2/#3).
9. External security review (#10) - schedule once #1-#5 are stable.
10. Mainnet deployment (#11) - last; depends on #10.
11. Dust-threshold floor on XMR-leg markets (#12) - operator-facing
    config rule, cheap to enforce alongside order-intake (#5).

Wallet-rpc lifecycle robustness (#7) is independent and can be done
anytime; it surfaces under real load and might come up earlier in
practice.

## Branch state at time of writing

`xmrswaps` branch off v1.0.6, ~53 commits at 2026-04-22. End-to-end
simnet swap is achievable through the runbook in
`dex/testing/dcrdex/ADAPTOR_SIMNET.md`. Test suites green:
`server/swap/...`, `server/swap/adaptor`, `server/dex`,
`client/core/...`, `client/core/adaptorswap`, `dex/msgjson`. HTLC code
paths are untouched throughout.
