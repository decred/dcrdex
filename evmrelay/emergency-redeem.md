# Emergency Gasless Redeem

When the relay is down or unavailable and the client has no gas to redeem,
the signed redeem calldata can be built manually and submitted by a helper
who has gas funds.

## Prerequisites

- The afflicted client must be running with the RPC server enabled.
- The helper must have gas funds on the relevant chain (e.g. POL on Polygon).
- The helper must also be running a Bison Wallet with the RPC server enabled
  and a connected wallet for the same chain.

## Steps

### 1. Build calldata (afflicted client)

The afflicted client builds and signs the redeem calldata. The relayer
address must be the helper's wallet address (the one who will submit and
collect the relay fee).

```
bwctl -p <app_password> gaslessredeemcalldata <helper_wallet_address> '["<match_id_hex>"]'
```

Multiple match IDs from the same trade can be batched:

```
bwctl -p <app_password> gaslessredeemcalldata <helper_address> '["<match_id_1>","<match_id_2>"]'
```

This returns `assetID`, `contractAddress`, and `calldata`.

### 2. Validate (helper)

The helper validates the calldata to check gas cost and profitability.
The fee recipient in the calldata must match the helper's wallet address.

```
bwctl validategaslessredeem <assetID> <contractAddress> <calldata>
```

This returns the relay fee, gas estimate, estimated tx cost, and whether
submitting is profitable.

### 3. Submit (helper)

The helper submits the calldata on-chain, paying gas and collecting the
relay fee.

```
bwctl -p <app_password> submitgaslessredeem <assetID> <contractAddress> <calldata>
```

This returns the transaction hash.

## Notes

- The calldata is self-contained and can be transmitted out-of-band (e.g.
  via chat or email). It contains an EIP-712 signature that authorizes the
  redeem.
- The relay fee incentivizes the helper to submit the transaction. It is
  deducted from the redeemed swap value and sent to the helper's address.
- The calldata includes a deadline. If the deadline passes before
  submission, the calldata is no longer valid and must be rebuilt.
- The helper does not need to be running the relay service. They just need
  a Bison Wallet with gas funds on the same chain.
