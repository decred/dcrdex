# Asset Interface Design for Adaptor Swaps

Design proposal for how the dcrdex client-side asset layer should
expose the primitives an adaptor swap needs. This is the Phase 2
exit criterion from the master plan.

## Constraints to satisfy

1. **Two asymmetric roles.** The scriptable-chain holder (BTC) and the
   non-scriptable holder (XMR) perform fundamentally different
   operations. The existing `asset.Wallet` HTLC-shaped methods
   (`Swap`, `AuditContract`, `Redeem`, `Refund`, `FindRedemption`) do
   not map onto adaptor swaps cleanly, and should not be contorted to
   fit.

2. **Coexistence with HTLC.** The HTLC swap type must keep working
   unchanged. All BTC-BTC-like pairs continue to use HTLC. Only pairs
   involving a non-scriptable asset (XMR in v1) use adaptor swaps.

3. **Multi-round protocol state.** Adaptor swaps accumulate state
   over multiple messages: partner keys, DLEQ proofs, pre-signed
   refund-chain transactions, adaptor signatures. Somewhere this
   state must be persistable so a restart does not abandon a swap.

4. **Per-swap fresh keys.** Each adaptor swap uses fresh ed25519 and
   secp256k1 scalars, not the main wallet's keys. Key generation and
   storage is per-swap, not per-wallet.

5. **Both parties sign the BTC side.** The BTC 2-of-2 tapscript
   requires signatures from both parties. The XMR-holding party
   must therefore hold a secp256k1 signing key and be able to produce
   tapscript sigs over a specific sighash. This is awkward if we
   think of it as "XMR wallet must do BTC signing."

## Two design directions

### Direction A: AdaptorSwapper interfaces on the asset backends

Extend the asset backends with adaptor-swap methods:

```go
type ScriptableAdaptorSwapper interface {
    BuildLockOutput(ctx, amount, kal, kaf) (LockMaterials, error)
    FundAndBroadcastLock(ctx, tx) (txid, vout, height, error)
    SignRefundTx(tx, leaf, key) ([]byte, error)
    BuildAdaptorSigOnSpend(...) (AdaptorSig, sigHash, error)
    VerifyAdaptorSigOnRefundSpend(...) error
    // ... a dozen more
}
type NonScriptableAdaptorSwapper interface {
    SendToSharedAddress(ctx, addr, amount) (txid, height, error)
    WatchSharedAddress(ctx, addr, viewKey, height) (WatchHandle, error)
    SweepSharedAddress(ctx, spendScalar, viewKey, height, dest) (txid, error)
}
```

Pros: backends own their chain logic, caller is thin.
Cons: the BTC backend grows a lot of adaptor-swap-specific knowledge
that is really protocol logic. The XMR backend is forced to know
about swap protocol even though most of its operations are general
"fund an address I do not own" primitives. The split between
"what the backend does" and "what the orchestrator does" gets muddy.

### Direction B: Orchestrator with thin asset primitives

Keep the asset backends mostly unchanged. Add only the truly
chain-specific primitives they are uniquely qualified to provide:

```go
// Additions to the BTC backend (or a tight interface extension):
type BTCTaprootPrimitives interface {
    // Fund, sign, and broadcast a tx with a caller-supplied taproot output.
    FundBroadcastTaproot(ctx, pkScript, amount) (tx *wire.MsgTx, vout uint32, height int64, err error)
    // Poll for block count - already exists as a basic primitive.
    BlockCount(ctx) (int64, error)
    // Mine blocks (regtest only; optional trait).
    GenerateBlocks(n int64) error
    // Observe a spending tx for a specific outpoint and return the
    // witness. Needed so the orchestrator can pull the completed sig
    // for RecoverTweak.
    ObserveSpend(ctx, outpoint) ([][]byte, error)
}

// Additions to the XMR backend (from XMR_WALLET_AUDIT.md):
type XMRSharedAddressPrimitives interface {
    SendToSharedAddress(ctx, addr, amount) (txid, sentHeight uint64, err error)
    WatchSharedAddress(ctx, swapID, addr, viewKeyHex, restoreHeight) (WatchHandle, error)
    SweepSharedAddress(ctx, swapID, spendKeyHex, viewKeyHex, restoreHeight, dest) (txid, error)
}
type WatchHandle interface {
    Synced(ctx) (bool, error)
    HasFunds(ctx, amount uint64, minConfs uint32) (bool, confs uint32, err error)
    Close() error
}
```

Then introduce an orchestrator in `client/core/adaptorswap.go` that:

- Owns per-swap ed25519 and secp256k1 key material.
- Drives the protocol state machine.
- Uses `internal/adaptorsigs` for crypto and `internal/adaptorsigs/btc`
  for tapscript.
- Calls the backend primitives for chain interaction.
- Persists per-swap state to the dcrdex DB.

Pros: backends stay small and mostly unchanged. Protocol logic
centralized in one orchestrator, which maps well to the server-side
state machine. Fresh-key-per-swap handled at the orchestrator
level, which is the natural owner.
Cons: the orchestrator has to know chain-specific details (tapscript
construction, BIP-340 adaptor signing over specific sighashes). This
duplicates some existing backend logic, e.g. signing.

## Recommendation: Direction B

Two reasons:

1. **Mirrors the server's state machine.** The server swap coordinator
   (`server/swap/swap.go`) is a single place that knows the HTLC
   state machine. Adding an adaptor swap state machine at the server
   is cleaner if the client mirrors that structure. Direction B
   produces an analogous
   `client/core/adaptorswap.go` that co-evolves with the server spec.

2. **Smaller backend surface area.** The XMR backend audit
   (XMR_WALLET_AUDIT.md) already identified the exact three
   primitives XMR needs (send/watch/sweep). For BTC, the existing
   backend is close to having what is needed, plus a `FundBroadcastTaproot`
   wrapper and an `ObserveSpend` helper. These are narrow, testable
   additions.

Direction A bloats the backends with protocol-shaped methods that
will need to be updated every time the protocol evolves, and the
existing HTLC-shaped methods in the same interface cause naming
confusion.

## Sketch of the orchestrator

```go
// client/core/adaptorswap.go (new)

type AdaptorSwapState struct {
    SwapID        [32]byte
    Role          SwapRole // Initiator (BTC side) or Participant (XMR side)
    Peer          *Peer
    PairAssets    [2]uint32 // {btc, xmr} or similar

    // Per-swap keys, all freshly generated.
    ScriptableSignKey  *btcec.PrivateKey  // for BTC 2-of-2
    ScriptableSignPub  []byte             // x-only
    XMRSpendKeyHalf    *edwards.PrivateKey
    DLEQProof          []byte

    // Counterparty material, populated as messages arrive.
    PeerScriptablePub  []byte
    PeerXMRSpendPub    *edwards.PublicKey
    PeerDLEQProof      []byte
    PeerScalarPubSecp  *btcec.PublicKey // derived from peer DLEQ

    // Transaction artifacts.
    Lock           *btcadaptor.LockTxOutput
    Refund         *btcadaptor.RefundTxOutput
    LockTx         *wire.MsgTx  // funded and broadcast
    RefundTx       *wire.MsgTx  // pre-signed cooperatively
    SpendRefundTx  *wire.MsgTx  // pre-signed; adaptor sig held separately
    SpendTx        *wire.MsgTx  // built when redeeming

    // Signatures collected.
    RefundSigOwn   []byte
    RefundSigPeer  []byte
    SpendRefundEsig  *adaptorsigs.AdaptorSignature
    SpendEsig        *adaptorsigs.AdaptorSignature

    // XMR-side handle for watching and eventually sweeping.
    XMRWatch         WatchHandle
    XMRSentHeight    uint64

    // Phase of the state machine.
    Phase        AdaptorSwapPhase
    LastUpdated  time.Time
}

type AdaptorSwapPhase int
const (
    PhaseKeyExchange AdaptorSwapPhase = iota
    PhaseRefundPresigned
    PhaseLockBroadcast
    PhaseLockConfirmed
    PhaseXMRSent
    PhaseXMRConfirmed
    PhaseSpendAdaptorSent
    PhaseSpendBroadcast
    PhaseXMRSwept
    PhaseComplete
    // Refund branches
    PhaseRefundTxBroadcast
    PhaseCoopRefund
    PhasePunish
    PhaseFailed
)
```

Driven by incoming messages on a channel and by a timer for
timeouts. Each phase has narrow responsibilities:

- `PhaseKeyExchange`: produce DLEQ proof, send keys to peer, receive
  peer keys, extract peer secp pub.
- `PhaseRefundPresigned`: build refund chain via `btcadaptor`, send
  pre-signed refund sig + adaptor sig on spendRefundTx.
- `PhaseLockBroadcast`: scriptable side funds and broadcasts lockTx;
  non-scriptable side waits and watches.
- `PhaseLockConfirmed`: non-scriptable side records XMR height and
  sends XMR to shared address.
- ... and so on.

## Backend additions required

### BTC (`client/asset/btc/`)

Two new methods on `btc.ExchangeWallet` or in a small extension
interface:

```go
type TaprootFunder interface {
    // FundBroadcastTaproot builds a tx paying `amount` to `pkScript`
    // (a P2TR output), funds it from the wallet, signs it, and
    // broadcasts. Returns the confirmed tx, the lock-output vout,
    // and the block height of confirmation.
    FundBroadcastTaproot(ctx context.Context, pkScript []byte, amount int64) (
        tx *wire.MsgTx, vout uint32, confirmedHeight int64, err error)

    // ObserveSpend blocks until the outpoint is spent, and returns
    // the witness stack of the spending tx.
    ObserveSpend(ctx context.Context, op wire.OutPoint) (witness [][]byte, err error)
}
```

`FundBroadcastTaproot` is mostly a glue of existing methods (the
existing backend already has FundRawTransaction, SignRawTransaction,
SendRawTransaction wrappers).

`ObserveSpend` is new but corresponds to a common pattern; probably
similar to `FindRedemption` but generalized.

### XMR (`client/asset/xmr/`)

Three new methods and a `WatchHandle` type, as sketched in the audit
doc:

```go
type XMRSharedAddressPrimitives interface {
    SendToSharedAddress(ctx, addr, amount) (txid, height uint64, err error)
    WatchSharedAddress(ctx, swapID, addr, viewKey, height) (*XMRWatch, error)
    SweepSharedAddress(ctx, swapID, spendKey, viewKey, height, dest) (txid, error)
}
```

Plus per-swap wallet lifecycle (extends `ExchangeWallet` with a
`map[swapID]*cxmr.Wallet` of watch/sweep wallets).

## Server-side mirror

The server needs a parallel adaptor swap state machine, which is the
critical-path remaining work. It will have the same phases and call
into `server/asset/btc/` and `server/asset/xmr/` for chain observation.
`server/asset/xmr/` does not yet exist and must be added.

## Incremental build order

1. `client/asset/xmr/swap.go`: the three XMR primitives, per-swap
   wallet lifecycle. Unit-testable with simnet.
2. `client/asset/btc/`: `FundBroadcastTaproot` + `ObserveSpend`.
3. `client/core/adaptorswap.go`: the orchestrator, initially driven
   by an in-memory state machine with a single test scenario.
4. `dex/msgjson`: adaptor swap messages.
5. `server/swap/adaptor_swap.go`: mirror server state machine.
6. Market-layer config for adaptor swap pairs + Option 1 enforcement
   (BTC maker only, XMR-sell limit orders rejected).
7. `server/asset/xmr/`: the server backend.

Each step is independently testable. Items 1-3 can be done without
any protocol work; items 4-7 depend on a protocol spec being ratified
but otherwise do not block 1-3.
