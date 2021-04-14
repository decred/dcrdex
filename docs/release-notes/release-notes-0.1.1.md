# DCRDEX v0.1.1

Nov 5, 2020

This patch release addresses two important match recovery bugs, and a number of
minor bugs. This release also includes several improvements to the client's user
interface. New client features include Tor proxy support and new deposit address
generation.

Please read the [initial release (v0.1.0)
notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0) for
important information and instructions.

## Client (dexc)

### Features and Improvements

- Add the mainnet ["client quick start" guide](https://github.com/decred/dcrdex#client-quick-start-installation).
  ([a383d5e](https://github.com/decred/dcrdex/commit/a383d5e76d2de90969f4eaf372084d290a051032))
- Tor support for connections with DEX servers.
  ([824f1c0](https://github.com/decred/dcrdex/commit/824f1c0da0b17afcab271c60665be6f8da3d6025))
  **WARNING**: This should be used with caution since Tor is slow and
  unreliable.
- On dexc start-up, display a link (URL) to the browser page, and if there are
  active orders, warn the user.
  ([a01e403](https://github.com/decred/dcrdex/commit/a01e403d09c491765d71ac34fc2d60b7898c3596))
- Add the ability to generate new deposit addresses.
  ([860af3e](https://github.com/decred/dcrdex/commit/860af3e19b49db9fb6b68016e894edd71361db3d))
- Various browser UI improvements, including order dialog wording and button
  formatting.
  ([dbf9d2c](https://github.com/decred/dcrdex/commit/dbf9d2c1f7c4644530b8a91f407818d4f435aa7b))
- Dialogs now have a close/cancel button.
  ([6716b58](https://github.com/decred/dcrdex/commit/6716b58c87933e71661f922d8cd2f479e6851a0d))
- Taker redemption transactions are more readily batched, potentially requiring
  fewer transactions for a taker order that matches with multiple maker orders.
  ([3ea75a9](https://github.com/decred/dcrdex/commit/3ea75a91d8e6935ad2cde128190042bde24f1e1d))
- When any node (e.g. bitcoind and dcrd) is still synchronizing with the
  network, new orders cannot be placed.
  ([2cac73a](https://github.com/decred/dcrdex/commit/2cac73a6655550d3cdd02ca2591844988e8126e7))

### Fixes

- Match recover is more robust, with fixes to revoked match handling on
  reconnect or restart.
  ([9790fb1](https://github.com/decred/dcrdex/commit/9790fb1cfb6e7ed7ffadc4624979ca57341d2ca0))
- Resolve a potential deadlock during match status resolution,
  ([c09017d](https://github.com/decred/dcrdex/commit/c09017d8170602bfae4fc2e34edd5ccfee34127e))
- Explicitly set js Content-Type in webserver to workaround misconfigured
  operating systems, such as Windows with misconfigured CLASSES_ROOT registry
  entries.
  ([f632893](https://github.com/decred/dcrdex/commit/f6328937f815f210daf4d910cb4722205ddf6e79))
- Delete obsolete notifications on frontend.
  ([8a69e99](https://github.com/decred/dcrdex/commit/8a69e991b518170eacc23d99eb8e1629ce7517d4))
- Avoid harmless but confusing warnings about returning zero coins when resumed
  trades are later completed.
  ([a01e403](https://github.com/decred/dcrdex/commit/a01e403d09c491765d71ac34fc2d60b7898c3596))
- Avoid redundant swap negotiation invocations on restart with unknown matches
  reported from a server.
  ([c0adb26](https://github.com/decred/dcrdex/commit/c0adb2659fe4ab341fc75b995c261b2cee029675))
- Orphaned cancel orders that could be created in certain circumstances are now
  retired during status resolution of the linked trade.
  ([867ba89](https://github.com/decred/dcrdex/commit/867ba894b6da4f6cda16ba4c371eb2436cc4d977))

## Server (dcrdex)

- Fix book purge heap orientation.
  ([eb6ccd4](https://github.com/decred/dcrdex/commit/eb6ccd464af474c4d7b506ee465a66e1f69f534f))
- Avoid orphaned epoch status orders when shutting down via SIGINT *without* a
  preceding suspend command.
  ([d463439](https://github.com/decred/dcrdex/commit/d4634395495f25b92cdc935fb7a72f311d23d118))
- When any node (e.g. bitcoind and dcrd) is still synchronizing with the
  network, relevant markets will not accept new orders.
  ([2cac73a](https://github.com/decred/dcrdex/commit/2cac73a6655550d3cdd02ca2591844988e8126e7))

## Code Summary

17 commits, 66 files changed, 2216 insertions(+), 566 deletions(-)

<https://github.com/decred/dcrdex/compare/release-v0.1.0...release-v0.1.1>

3 contributors

- Brian Stafford (@buck54321)
- David Hill (@dajohi)
- Jonathan Chappelow (@chappjc)
