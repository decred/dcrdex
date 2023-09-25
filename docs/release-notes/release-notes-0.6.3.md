# DCRDEX v0.6.3

This release contains several important fixes and improvements.  All users are advised to update.

Please read the [v0.6.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.6.0) for a full list of changes since v0.5.

## Features and Improvements

- Trade limits scale with bond level.
- Add **extension mode**. When in extension mode, 1 or more wallets are managed by a higher level application, and cannot be reconfigured through the dexc GUI.

## Fixes

- Fix bug where order was being retired before server accepted the `redeem` message.
- Updates btcwallet dependency to incorporate recent upstream change (<https://github.com/btcsuite/btcwallet/pull/870>). Resolves a common error that arose during wallet rescans.
- Resolves a potential server deadlock that can occur if processing of matched cancel orders result in an account cancel violation that would result in a suspension.
- Removes "bonus tiers", which were an accounting curiosity on the server that didn't do anything for the client but did introduce a bug where client's bonds would fully expire before being replaced.

## Code Summary

25 files changed, 281 insertions(+), 126 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.6.2...v0.6.3>

2 contributors

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
