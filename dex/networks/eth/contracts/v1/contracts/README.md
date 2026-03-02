# ETHSwapV1 Known Limitations and Concerns

## ERC20 Blocklisting Can Permanently Lock Funds

USDC and USDT have admin blocklist functionality. If Circle or Tether
blacklists this contract or either party after a swap is initiated, both
`redeem` and `refund` will revert on `safeTransfer`. The swap is stuck in
the `Filled` state permanently with no recovery path.

Unlike the `redeemAA` path (which finalizes the swap even on transfer
failure to prevent drain attacks), the regular `redeem` and `refund`
functions revert entirely on transfer failure, rolling back the state
change.

## No Account Abstraction Path for ERC20 Redemption

`validateUserOp` and `redeemAA` hardcode `address(0)` (native ETH). The
only ERC20 redemption path is `redeem`, which enforces
`tx.origin == msg.sender`. Smart contract wallets that can only interact
via ERC-4337 cannot redeem ERC20 swaps and must wait for the refund
timeout.

## redeemAA Failed Transfers Are Permanently Lost

If the recipient cannot accept ETH in `redeemAA`, the swap is still marked
as redeemed and the secret is revealed on-chain. The unreceived payout
remains in the contract with no recovery mechanism. This is intentional to
prevent a drain attack where `validateUserOp` pays prefund but `redeemAA`
reverts, allowing repeated extraction of contract funds to the EntryPoint.

The participant's counterparty on the other chain can still see the secret
and redeem there, so the participant potentially loses both sides.

## pendingValidation Flag Is Permanent on Execution Failure

If `validateUserOp` succeeds but `redeemAA` reverts (e.g., out-of-gas),
`pendingValidation[key]` is never cleared. That swap cannot be redeemed
via account abstraction again. The fallback is the regular `redeem`
function, which requires `tx.origin == msg.sender`. A smart contract
wallet that can only interact via ERC-4337 would need to wait for the
refund timeout.

## Refund Is Callable by Any EOA

The `refund` function does not restrict the caller to `v.initiator`. Any
EOA can trigger a refund for any expired swap. The funds are always sent
to `v.initiator`, so there is no theft risk, but a third party can force
a refund that the initiator might have preferred to leave open — for
example if the counterparty is still attempting to redeem. In the DCRDEX
protocol this is not a practical concern because refund timestamps are
chosen with sufficient margin and the DEX server coordinates timing, but
direct users of the contract should be aware that expired swaps can be
refunded by anyone.

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

## \_payPrefund Silently Ignores Transfer Failure

`_payPrefund` does not check the return value of the ETH transfer to
the EntryPoint. This is per the ERC-4337 specification: the EntryPoint
handles insufficient prefund scenarios itself (reverting with AA21),
so a failed prefund transfer does not need to revert `validateUserOp`.

## Test Swap Has No Backing ETH

The constructor creates a permanent test swap with `value: 1 ether`
for bundler compatibility checks, but no ETH is actually deposited.
This is safe because `redeemAA` skips the test swap before adding to
`total`, and `redeem`/`refund` reject it outright via the
`testSwapKey` check.

## sha256 Precompile Cost

The contract uses `sha256` throughout (for `contractKey`,
`secretValidates`, etc.) rather than the native `keccak256`. This is
intentional for cross-chain compatibility with DCRDEX, which uses
SHA-256 for atomic swap secrets. The `sha256` precompile costs roughly
double what `keccak256` does (60 base + 12/word vs 30 + 6/word), but
this is negligible relative to the storage operations in each
transaction.

## No receive() Function — EntryPoint Gas Refunds Are Lost

The contract has no `receive()` or `fallback()` function. After
executing a user operation, the EntryPoint refunds unused gas back to
the sender (this contract) via a low-level call. Without `receive()`,
those refunds silently fail — the EntryPoint handles the failure
gracefully, but the unused gas ETH stays in the EntryPoint permanently.
The contract overpays for every AA redemption by the difference between
estimated and actual gas.

## pendingValidation Leak on Post-Loop Validation Failure

`validateUserOp` sets `pendingValidation[key] = true` inside the
redemption loop, before the post-loop checks (missingAccountFunds vs
total, callGasLimit minimum, ECDSA signature). If any post-loop check
returns `SIG_VALIDATION_FAILED`, the EntryPoint does not call
`redeemAA`, so `pendingValidation` flags are never cleared. Those swaps
are permanently locked out of the AA redemption path (regular `redeem`
still works).

This is a griefing vector: an attacker who observes a pending user
operation in the mempool can extract the secret and swap details, then
submit a modified version with a valid secret but an invalid signature.
The validation loop passes (setting `pendingValidation`), the signature
check fails, and the swap is now blocked from AA redemption. The
attacker gains nothing financially, but the participant is forced to
fall back to the regular `redeem` path.

## isRedeemable Does Not Check block.number

`isRedeemable` checks `blockNum != 0` but not `blockNum < block.number`.
A swap initiated in the current block returns `true` from `isRedeemable`
but would fail the `blockNum < block.number` require in `redeem`. This
is a view function so there is no on-chain impact, but off-chain code
relying on it to gate redemption attempts could be misled into
submitting transactions that revert.

## Test Swap Bundler Check Depends on 1 ETH Exceeding Gas Prefund

The bundler compatibility check in `testBundlerCompatibility` submits a
user operation that redeems the permanent test swap (value: 1 ether).
During `validateUserOp`, the contract checks
`missingAccountFunds > total` and returns `SIG_VALIDATION_FAILED` if
the gas prefund exceeds the total redemption value. The check passes
because 1 ETH exceeds any reasonable gas prefund. If gas costs ever
approach 1 ETH, the compatibility check would start failing even though
real trades with higher lot sizes would succeed.
