# xmrswap Reference Protocol Spec

Source: `internal/cmd/xmrswap/main.go` (1475 lines, 4 end-to-end tests).
Scope: DCR/XMR adaptor-signature atomic swap. This spec captures the actual
protocol implemented; it is the basis for porting to BTC/XMR.

## Roles

Two parties, assigned by asset holding (not by market maker/taker role):

| Code name | Role | Holds initially | Wants | First on-chain action |
|---|---|---|---|---|
| Bob | `initClient` (initiator) | DCR (scriptable) | XMR | Locks DCR |
| Alice | `partClient` (participant) | XMR (non-scriptable) | DCR | Locks XMR, only after DCR lock confirms |

Constraint: the scriptable-chain holder must be the initiator. There is no
Alice-first variant. This is what maps to "Option 1" for BTC/XMR: the BTC
holder must be the maker on a BTC-XMR market.

## Cryptographic state

Each party generates three secrets and shares the corresponding pubkeys plus
a DLEQ proof tying the XMR-spend-key half to the secp256k1 curve.

### Alice (partClient)

- `partSpendKeyHalf` (ed25519) - half of the XMR spend key. Not shared.
- `partSpendKeyHalfDleag` - DLEQ proof that Alice's secp256k1 witness equals
  her XMR-key half. Shared.
- `kbvf` (ed25519) - half of the XMR view key. Private shared with Bob (so
  both sides can watch the shared XMR address).
- `partSignKeyHalf` (secp256k1) - Alice's half of the 2-of-2 for DCR. Pulled
  from the DCR wallet via `GetNewAddress` + `DumpPrivKey` (main.go:452-460).
  Pubkey shared.

### Bob (initClient)

- `initSpendKeyHalf` (ed25519) - the other half of the XMR spend key. Not
  shared.
- `initSpendKeyHalfDleag` - DLEQ proof. Shared.
- `kbvl` (ed25519) - the other half of the XMR view key. Kept; Alice can also
  derive the full view key.
- `initSignKeyHalf` (secp256k1) - Bob's half of the 2-of-2 for DCR. Generated
  fresh.

### Derived

- Full XMR view key `viewKey = kbvf + kbvl (mod n)` - both parties hold it.
- Full XMR spend pubkey `pubSpendKey = partSpendKeyHalf.pub + initSpendKeyHalf.pub`
  - both parties know it; neither knows the full spend private key until after
  the final reveal.

## Scripts

Two DCR P2SH scripts, both embedded as `dcradaptor.LockTxScript` /
`LockRefundTxScript`.

### `lockTxScript` (plain 2-of-2)

Spends require signatures from both `initSignKeyHalf.pub` and
`partSignKeyHalf.pub`. No timelock.

### `lockRefundTxScript` (2-branch)

- Cooperative-refund branch (selected with `OP_TRUE` in spend script): requires
  both sigs. Bob's sig here is constrained such that publishing it reveals his
  XMR spend key half via adaptor-sig recovery.
- Punish branch (selected with `OP_FALSE`): requires only Alice's sig, after a
  CSV timelock (`durationLocktime = lockBlocks` blocks, hard-coded 2 on simnet).

## Transactions

| Tx | Built by | Spends | Pays to | Broadcast by | When |
|---|---|---|---|---|---|
| `lockTx` | Bob (`generateLockTxn`, funded via `FundRawTransaction`) | Bob's wallet UTXOs | P2SH(`lockTxScript`) | Bob (`initDcr`) | Phase 1 |
| `spendTx` | Bob (`initDcr`) | `lockTx` | Alice's address (`partSignKeyHalf` P2PKH) | Alice (`redeemDcr`) on happy path | Phase 3 success |
| `refundTx` | Bob (`generateLockTxn`), both pre-sign | `lockTx` | P2SH(`lockRefundTxScript`) | Either party (`startRefund`) | On refund |
| `spendRefundTx` | Bob (`generateLockTxn`) | `refundTx` | varies (see below) | Bob or Alice depending on branch | After refund |

Pre-signing: `refundTx` is signed by both parties before `lockTx` is broadcast.
`spendRefundTx` is partially pre-signed: Alice generates an adaptor signature
on it tweaked by Bob's XMR-key half pubkey (`generateRefundSigs` ->
`PublicKeyTweakedAdaptorSig`).

## Sequence - happy path

```
ALICE                                                BOB
(partClient, XMR holder)                             (initClient, DCR holder)

[1] generateDleag()
    -> pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag
                                -------->

                                              [2] generateLockTxn()
                                                  builds lockTx (unbroadcast),
                                                  refundTx (unsigned),
                                                  spendRefundTx (unsigned),
                                                  lockTxScript, lockRefundTxScript,
                                                  bobDleag, full viewKey, pubSpendKey
                                <--------

[3] generateRefundSigs()
    signs refundTx, creates
    adaptor sig on spendRefundTx
    tweaked by Bob's XMR-key pub
                                -------->

                                              [4] initDcr(): BROADCAST lockTx
                                                  -- on DCR chain -->

[5] wait lockTx confirms (>=lockBlocks)

[6] initXmr(): SEND xmrAmt to shared
    address derived from pubSpendKey
    + viewKey
                                -- on XMR chain -->

                                              [7] wait XMR confirms
                                              [8] sendLockTxSig():
                                                  adaptor sig on spendTx,
                                                  tweaked by Alice's XMR-key pub
                                <--------

[9] redeemDcr(): completes Bob's
    adaptor sig using her own sign key,
    BROADCASTS spendTx -> Alice gets DCR.
    The completed sig is now public on DCR chain.
                                -- on DCR chain -->

                                              [10] redeemXmr(): reads the DCR spendTx,
                                                   RecoverTweak() extracts Alice's
                                                   XMR-key half scalar from the
                                                   completed sig. Bob now has both
                                                   halves of the XMR spend key,
                                                   creates wallet-from-keys, sweeps.
```

Key insight: the adaptor sig in step 8 "encrypts" Bob's 2-of-2 contribution to
`spendTx` under Alice's XMR-key-half pubkey. For Alice to publish a valid
spend of `lockTx`, she must complete (decrypt) it using her secret, which
unavoidably publishes a standard secp256k1 Schnorr signature on-chain. Bob
reads that signature, subtracts the adaptor tweak, and recovers Alice's
ed25519 XMR-key half (via the DLEQ-proven mapping).

## Sequence - refund (`refund` test)

Both parties have locked (`lockTx` confirmed, XMR sent to shared address), but
they mutually decide to unwind (in the test, this just means Bob calls
`startRefund` without waiting for anything).

```
[1]-[6] same as happy path.

[7]  startRefund(): Bob (or Alice) broadcasts refundTx
     -> funds move to P2SH(lockRefundTxScript).

[8]  waitDCR(): wait for lockBlocks CSV to mature on the new output.
     <- This is the "nothing-up-my-sleeve" delay; Alice could instead
        race Bob via takeDcr.

[9]  Bob: refundDcr(). Decrypts Alice's adaptor sig on spendRefundTx using his
     initSpendKeyHalf -> produces partSignKeyHalfSig that reveals Alice's XMR
     key half via the adaptor construction. Signs his half. Broadcasts
     spendRefundTx through the OP_TRUE (cooperative-refund) branch -> Bob gets
     his DCR back. The on-chain sig leaks what Alice needs.

[10] Alice: refundXmr(). Parses partSignKeyHalfSig, calls RecoverTweak() on
     the adaptor sig -> gets initSpendKeyHalf scalar. Now knows both halves of
     the XMR spend key. Constructs wallet-from-keys (different wallet than the
     one she sent from; this is a sweep wallet) and restores from the recorded
     XMR height. Sweeps the shared-address XMR.
```

Net result: both parties get their original assets back, minus chain fees (two
DCR transactions for Bob, XMR sweep fee for Alice).

## Sequence - Alice bails before XMR init (`aliceBailsBeforeXmrInit`)

```
[1]-[4] Bob locks DCR. Alice has generated keys and signed the refund, but
        never calls initXmr.

[5] Bob waits (in the test, he immediately calls startRefund; in production,
    he'd wait some timeout). Broadcasts refundTx.

[6] waitDCR(): CSV matures.

[7] Bob: refundDcr() through OP_TRUE branch. Same mechanic as the normal
    refund. Bob's sig reveals his XMR key half on-chain, but since Alice never
    locked XMR, this leakage is harmless. Bob gets DCR back minus fees.
```

Alice: no state changed on-chain, no funds lost.

## Sequence - Bob bails after XMR init (`bobBailsAfterXmrInit`)

The punish path. Bob has locked DCR. Alice has locked XMR. Bob should send the
esig (step 8 of happy path) so Alice can redeem, but goes silent.

```
[1]-[6] Both parties lock (Alice XMR, Bob DCR).

[7] Bob disappears. Alice (not Bob) calls startRefund: broadcasts refundTx.
    (refundTx is pre-signed by both, so Alice can broadcast without Bob.)

[8] waitDCR(): CSV matures.

[9] Alice: takeDcr(). Uses the OP_FALSE branch: only her sig is required
    (timelock enforces that Bob had his chance to refund first). Alice
    takes all the DCR to a new address.
```

Outcome: Alice has the DCR but her XMR is stranded at the shared address
forever (she doesn't have Bob's half of the spend key and Bob never reveals it
because he never initiated the cooperative refund). Bob loses his DCR and
also cannot sweep the XMR. Mutually-destructive equilibrium - the punishment
against Bob's stalling is that he loses his DCR to Alice.

## State machine

```
              +---------------------+
              |  Setup (off-chain): |
              |  [1] aliceDleag     |
              |  [2] bobDleag,      |
              |      build txs      |
              |  [3] refund sigs    |
              +----------+----------+
                         |
                         v
              +---------------------+
              |   Bob broadcasts    |
              |      lockTx         |
              |   (DCR locked)      |
              +----------+----------+
                         |
             +-----------+-----------+
             |                       |
        Alice sends XMR        Alice never sends
             |                       |
             v                       v
   +-------------------+    +---------------------+
   |   XMR locked at   |    | Bob: startRefund +  |
   |  shared address   |    | waitDCR + refundDcr |
   +--------+----------+    |   (OP_TRUE branch)  |
            |               |   Bob gets DCR back |
            |               +---------------------+
     +------+------+              [scenario 2]
     |             |
Bob sends      Bob bails
  esig          (silent)
     |             |
     v             v
+----------+  +----------------------+
|Alice     |  | Alice: startRefund + |
|redeemDcr |  | waitDCR + takeDcr    |
|          |  |  (OP_FALSE branch,   |
|Bob then  |  |   after timelock)    |
|redeemXmr |  | Alice gets DCR;      |
|          |  | XMR stranded         |
|[success] |  |      [scenario 4]    |
+----------+  +----------------------+

Alternative: both agree to refund cooperatively after locking ->
Bob: startRefund + refundDcr(OP_TRUE) -> reveals XMR half.
Alice: refundXmr -> sweeps back.              [scenario 3]
```

## Timelock structure

Only one CSV window, `lockBlocks = 2` (simnet). Enforced on `spendRefundTx`'s
input via `txIn.Sequence = durationLocktime` (main.go:628). This window
separates the "Bob can still cooperatively refund" phase from the "Alice can
punish" phase on the `refundTx` output.

There is no timelock on `lockTx` itself. The only way out of `lockTx` is for
both parties to sign (happy path) or for `refundTx` to be broadcast (either
party can do this anytime, since both pre-signed).

For production BTC/XMR, the `lockBlocks` value matters much more than on
simnet. It has to be (a) long enough for Bob to notice Alice hasn't completed
and initiate refundDcr, plus (b) long enough for the refundTx to confirm with
margin. For BTC, probably on the order of 144 blocks (~24h) for the CSV window,
vs. 2 blocks simnet.

## Gaps vs. production

From the file's own `TODO: Verification at all stages has not been implemented
yet.` (main.go:42), plus what shows up in the code:

1. No input verification. Alice never verifies Bob's DLEQ proof; Bob never
   verifies Alice's. Both must validate counterparty DLEQ proofs before
   proceeding, or a malicious counterparty can send garbage that lets them
   steal.
2. No value/script verification. Alice doesn't confirm `lockTx` actually has
   the right amount or script before she sends XMR. Mandatory.
3. No XMR confirmation check. Bob proceeds to `sendLockTxSig` after a
   `time.Sleep(5s)` (main.go:1148). Must check the XMR output has confirmed
   with enough depth, actually lands at the expected shared address, and has
   the expected amount.
4. No reorg handling. If `lockTx` reorgs out, Alice's XMR send is stuck.
5. Refund timing heuristic. In the real protocol, Bob needs to decide "how
   long do I wait for Alice to lock XMR before I bail?" The test immediately
   bails; production needs a timeout parameter and a decision procedure.
6. `spendRefundTx` fee is hard-coded (`dumbFee`). Needs dynamic fee estimation.
7. Wallet-from-keys recovery requires restoring from a recorded block height.
   If the height is wrong, the sweep wallet misses outputs. This is fragile
   and needs robust height capture.
8. `takeDcr` does not reveal Bob's XMR half. Alice only loses XMR in the
   punish scenario because `takeDcr` uses OP_FALSE (her sig alone) - it does
   not constrain Bob's sig. If you wanted Alice to also be able to recover
   XMR in this scenario, you'd need a different script structure.
9. No replay/double-spend protection if either party reuses keys across
   swaps. Each swap must generate fresh `initSignKeyHalf` and
   `partSignKeyHalf`.

## Translation targets for BTC/XMR

Items that must change when porting to BTC:

- `dcradaptor.LockTxScript` -> Taproot 2-of-MuSig2 key-path (or P2WSH 2-of-2
  if you keep ECDSA adaptor sigs). Recommended: P2TR with MuSig2.
- `dcradaptor.LockRefundTxScript` -> Taproot with two tap leaves:
  cooperative-refund leaf (2-of-2 key path or script path with adaptor-sig
  recovery) and punish leaf (1-of-1 Alice + `OP_CHECKSEQUENCEVERIFY`).
- `sign.RawTxInSignature(..., STSchnorrSecp256k1)` -> BIP340 signing using
  `btcec` or `dcrec/secp256k1/v4/schnorr` (same library, BIP340 mode). Note:
  the existing `adaptorsigs` package uses Decred EC-Schnorr-DCRv0, not BIP340.
  This is the single biggest porting task - BIP340 variants of
  `PublicKeyTweakedAdaptorSig`, `Decrypt`, `RecoverTweak`, and verification.
- `wire.MsgTx` / `txscript` -> `btcd/wire` / `btcd/txscript`.
- `FundRawTransaction` (dcrwallet) -> whatever the BTC asset backend exposes.
  `client/asset/btc` already has this plumbing via `bitcoind`/SPV wallets.
- DCR chain params -> BTC chain params; output types; CSV encoding (sequence
  field differs in subtle ways, but CSV is consensus-compatible).
- The `durationLocktime` hack (`int64(lockBlocks)` without
  `SequenceLockTimeIsSeconds`) carries over unchanged for block-based CSV.
