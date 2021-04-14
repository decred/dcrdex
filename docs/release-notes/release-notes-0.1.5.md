# DCRDEX v0.1.5

Feb 22, 2021

This patch release provides several important bug fixes.

Please read the [initial release (v0.1.0)
notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0) for
important information and instructions.

## Client (dexc)

### Features and Improvements

The user's account ID is now logged on connect and authentication with a DEX
server.
([8ce328c](https://github.com/decred/dcrdex/commit/8ce328cacc4d7b0d35973a39917eef48ac1d1d64))

### Fixes

- Fix a possible panic when reconfiguring a wallet that is not connected.
  ([dfe4cd1](https://github.com/decred/dcrdex/commit/dfe4cd12234d1d17d6114f3de8f062ff912c594b))
- When resuming trades on startup and login, counterparty contract audits now
  retry repeatedly, as is the case when an audit request is initially received.
  This prevents a match from being incorrectly revoked on startup if the
  wallet/node fails to locate the counterparty contract immediately.
  ([172dbb7](https://github.com/decred/dcrdex/commit/172dbb7085d70079e6f0ffb8f2e9bedceac280df))
- The client's database subsystem is always started first and stopped last. This
  is a prerequisite for the following wallet lock-on-shutdown change.
  ([b4ef3ff](https://github.com/decred/dcrdex/commit/b4ef3ff01f3a1567aecf762c5db75f83a9687d64))
- On shutdown of client `Core`, the wallets are now locked even if the
  `PromptShutdown` function is not used. This does not affect dexc users, only
  direct Go consumers of the `client/core.Core` type.
  ([70044e6](https://github.com/decred/dcrdex/commit/70044e68740faffc4888c6f4b4303806531a0255))
- Fix a possible interruption of the DEX reconnect loop if the config response
  timed out.
  ([4df683a](https://github.com/decred/dcrdex/commit/4df683a10d755d71f37c979655b6ceea6343db8d))
- Update the crypto/x/blake2 dependency to prevent silent memory corruption from
  the hash function's assembly code.
  ([c67af3f](https://github.com/decred/dcrdex/commit/c67af3f3b88750e69957e019d9eacc80d6aa7555))
- Handle orders that somehow lose their funding coins. Previously, such orders
  would forever be logged at startup but never retired, and any matches from
  such orders that required swap negotiation or other recovery would have been
  improperly abandoned.
  ([a7b5aa0](https://github.com/decred/dcrdex/commit/a7b5aa0a67dd2962c33d229cf101c59e85cb7b85))

## Server (dcrdex)

There are no substantive server changes, just a few logging improvements.

## Code Summary

11 commits, 13 files changed, 564 insertions(+), and 254 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.1.4...v0.1.5>

3 contributors

- Brian Stafford (@buck54321) (review)
- David Hill (@dajohi) (blake2 fix)
- Jonathan Chappelow (@chappjc)
