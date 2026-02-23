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

## v.initiator Is Not Validated Against msg.sender

In `initiate`, there is no check that `v.initiator == msg.sender`. The
caller pays for the swap, but the refund goes to `v.initiator`. If set
incorrectly, refunded funds go to the wrong address with no way to
recover them.

## Fee-on-Transfer and Rebasing Tokens

The contract is not compatible with fee-on-transfer or rebasing tokens.
No currently supported tokens (USDC, USDT, WETH, WBTC, POL) have this
property, but there is no on-chain guard preventing their use. A
post-transfer balance check in `initiate` would reject such tokens at
initiation time rather than allowing silent accounting mismatches that
surface later as failed redemptions for the last redeemer.
