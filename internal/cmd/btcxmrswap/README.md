# btcxmrswap

BTC/XMR atomic swap demonstrator using BIP-340 Schnorr adaptor
signatures and Taproot tapscript 2-of-2. Ports `internal/cmd/xmrswap`
(the Decred reference) to the Bitcoin side.

All four scenarios from the reference are implemented:

- `success` - happy path; Bob (BTC holder) and Alice (XMR holder) both
  lock, Alice redeems BTC via a completed adaptor, Bob sweeps the XMR.
- `aliceBailsBeforeXmrInit` - Bob locks, Alice never sends XMR; Bob
  cooperatively refunds his BTC.
- `refund` - both parties lock, mutually unwind via coop refund; Bob's
  refund sig reveals his XMR scalar, letting Alice sweep.
- `bobBailsAfterXmrInit` - both parties lock, Bob disappears; Alice
  broadcasts refundTx and punish-spends alone after the CSV locktime.
  Her XMR is stranded - this is the asymmetric punishment.

## Status

- Crypto primitives (`internal/adaptorsigs`, `internal/adaptorsigs/btc`)
  are validated by unit tests, including four BIP-340 official vectors
  and full integration tests through btcd's script engine.
- This CLI compiles and was structurally verified via `go vet`.
- Live simnet validation is task #12 and has not been performed.

## Prerequisites for simnet

1. **bitcoind regtest + descriptor wallet** via `dex/testing/btc/harness.sh`.
   The harness listens on RPC ports 20556 (alpha, Bob) and 20557
   (beta, Alice) with RPC user `user` / password `pass`. Both wallets
   are descriptor-based so Taproot is available.

2. **monerod simnet + wallet-rpc** via `dex/testing/xmr/harness.sh`.
   Relevant wallet-rpc ports:
   - Alice (XMR sender, needs funds): 28284 (Charlie)
   - Bob (XMR receiver): 28184 (Bill)
   - Extra (used for sweep/watch wallets): 28484 (Own)

3. **Go 1.21+** to build the binary.

## Running

```bash
# Start both harnesses first (each creates their own tmux session).
cd dex/testing/btc && ./harness.sh &
cd dex/testing/xmr && ./harness.sh &

# Build and run the demo.
go run ./internal/cmd/btcxmrswap
```

On regtest the CLI auto-mines blocks to advance confirmations and
satisfy CSV timelocks. Pass `--no-mine` to disable (for testnet runs
where blocks come naturally). Pass `--testnet` for a testnet run; this
expects a `config.json` alongside the binary with custom RPC endpoints.

## Protocol summary

See `internal/cmd/xmrswap/PROTOCOL.md` for the full protocol spec.
Key differences on the BTC side:

- BTC 2-of-2 is a taproot tapscript leaf
  (`<kal> OP_CHECKSIGVERIFY <kaf> OP_CHECKSIG`) under an unspendable
  NUMS internal key.
- Refund output has a two-leaf tap tree: the cooperative 2-of-2 leaf,
  and a punish leaf `<csvBlocks> OP_CSV OP_DROP <kaf> OP_CHECKSIG`.
- Signatures are BIP-340 Schnorr (not ECDSA).
- Adaptor signatures use
  `internal/adaptorsigs.PublicKeyTweakedAdaptorSigBIP340`.

## Config for testnet

When `--testnet` is passed, the CLI reads `config.json` from the same
directory as the binary:

```json
{
  "alice": {
    "xmrhost": "http://127.0.0.1:28284/json_rpc",
    "btcrpc":  "127.0.0.1:18332",
    "btcuser": "user",
    "btcpass": "pass"
  },
  "bob": {
    "xmrhost": "http://127.0.0.1:28184/json_rpc",
    "btcrpc":  "127.0.0.1:18333",
    "btcuser": "user",
    "btcpass": "pass"
  },
  "extraxmrhost": "http://127.0.0.1:28484/json_rpc"
}
```

The XMR side uses stagenet when `--testnet` is set (`netTag=24`).
