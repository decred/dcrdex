# ETHSwapV1 Known Limitations and Concerns

## ERC20 Blocklisting Can Permanently Lock Funds

USDC and USDT have admin blocklist functionality. If Circle or Tether
blacklists this contract or either party after a swap is initiated, both
`redeem` and `refund` will revert on `safeTransfer`. The swap is stuck in
the `Filled` state permanently with no recovery path.

## Signed Redeems Are ETH-Only

`redeemWithSignature` and `_executeETHRedemptions` hardcode `address(0)`
(native ETH) for the contract key lookup. Token swaps use a different
key (keyed by the token address), so they cannot be redeemed through the
signed redeem path. The only ERC20 redemption path is `redeem`, which
requires `tx.origin == msg.sender`. Smart contract wallets cannot redeem
ERC20 swaps and must wait for the refund timeout.

More broadly, the `senderIsOrigin` modifier on `initiate`, `redeem`, and
`refund` excludes all smart contract wallets (ERC-4337 account
abstraction, multisigs, etc.) from interacting with those functions
directly. Only `redeemWithSignature` omits this restriction, since it is
designed for relay submission. As smart contract wallet adoption grows,
this limits the pool of users who can participate in swaps.

## Refund Is Callable by Any EOA

The `refund` function does not restrict the caller to `v.initiator`. Any
EOA can trigger a refund for any expired swap. The funds are always sent
to `v.initiator`, so there is no theft risk, but a third party can force
a refund that the initiator might have preferred to leave open - for
example if the counterparty is still attempting to redeem. In the DCRDEX
protocol this is not a practical concern because refund timestamps are
chosen with sufficient margin and the DEX server coordinates timing, but
direct users of the contract should be aware that expired swaps can be
refunded by anyone. Additionally, at the exact expiry boundary both
`redeem` and `refund` are valid in the same block (`redeem` does not
check the timestamp, only block number). If both transactions land in
the same block, the block proposer's ordering determines which succeeds.
An initiator could have a bot ready to submit `refund` the instant the
timestamp passes, potentially front-running a legitimate `redeem` that
is pending in the mempool.

## Rebasing and Fee-on-Outbound-Transfer Tokens

The contract is not compatible with rebasing tokens. No currently
supported tokens (USDC, USDT, WETH, WBTC, POL) have this property.
Fee-on-transfer tokens are rejected at initiation time via a
post-transfer balance check, but rebasing tokens that change balances
outside of transfers have no on-chain guard.

Tokens that charge fees only on outbound transfers (not on the
`transferFrom` during initiation) would pass the balance check but
deliver less than `v.value` on `redeem` or `refund`. No currently
supported tokens have this property.

## sha256 Precompile Cost

The contract uses `sha256` throughout (for `contractKey`,
`secretValidates`, etc.) rather than the native `keccak256`. This is
intentional for cross-chain compatibility with DCRDEX, which uses
SHA-256 for atomic swap secrets. The `sha256` precompile costs roughly
double what `keccak256` does (60 base + 12/word vs 30 + 6/word), but
this is negligible relative to the storage operations in each
transaction.

## isRedeemable Does Not Check block.number

`isRedeemable` checks `blockNum != 0` but not `blockNum < block.number`.
A swap initiated in the current block returns `true` from `isRedeemable`
but would fail the `blockNum < block.number` require in `redeem`. This
is a view function so there is no on-chain impact, but off-chain code
relying on it to gate redemption attempts could be misled into
submitting transactions that revert.

## Sequential Nonces Block Subsequent Gasless Redeems

`redeemWithSignature` uses sequential per-participant nonces. If a signed
redeem with nonce N is submitted but the relay goes down before the
transaction is mined or cancelled, nonce N+1 cannot be used until N is
consumed. The relay server mitigates this with its cancellation flow
(submitting same-nonce cancel transactions), and sequential nonces are
standard practice (ERC-2612 uses the same pattern), but participants
should be aware that a stuck relay can temporarily block gasless redeems.
The regular `redeem` function is always available as a fallback.

A related concern is that a participant using gasless redeem likely has
no ETH at all. There is no `incrementNonce` function on the contract, so
a gasless participant cannot revoke a pending signed message on-chain.
Their only option is to wait for the signed message's `deadline` to
expire. Keeping deadlines short limits the exposure window.

## Relayer Fee Cap

The contract caps the relayer fee at 50% of the total redeemed value.
The client also enforces this limit independently. This protects against
compromised client software signing away excessive fees, but means
gasless redeems are not possible when the relay fee would exceed half the
swap value (e.g. very small swaps on high-fee chains).

## MEV Front-Running of Relayer Fee When feeRecipient Is Zero

When `feeRecipient` is `address(0)`, the signed redemption message does
not bind the relayer fee to a specific address. The contract assigns
`feeRecipient = msg.sender` at execution time, so any party that
observes the transaction in the mempool can copy the calldata, submit it
with a higher gas price, and collect the relayer fee. The original
relayer's transaction then reverts (nonce already consumed) and they lose
gas with no compensation. The participant is unaffected - they receive
their funds minus the fee regardless of which address submits the
transaction. Setting a non-zero `feeRecipient` eliminates this risk
because the contract enforces `feeRecipient == msg.sender`, and the
`feeRecipient` value is covered by the EIP-712 signature.
