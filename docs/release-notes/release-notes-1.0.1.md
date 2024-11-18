# DCRDEX v1.0.1

This release contains several important fixes and improvements.  All users are advised to update.

Please read the [v1.0.0 release notes](https://github.com/decred/dcrdex/releases/tag/v1.0.0) for a full list of changes since v0.6.

## Fixes and Improvements

- Allow collapsing user-expanded reputation info on /markets
- Removed unused changepos field in btc fundrawtransaction that was causing a JSON parse error when firod returned a negative value
- Update minimum Firo version to 0.14.14.0
- Add support for enabling/disabling a server
- Fix a couple of issues with the Binance market feed subscriptions
- Fix a lock-contention problem where a long running "tick" function was causing the client to fail to report their preimage
- Fix high/low rate conversion on /markets
- Fix amounts displayed for token transactions in transaction history on /wallets
- Fix negative swap confirmations for Decred
- Fix bond confirmation messaging on /markets that was showing two different messages at once
- Fix filename flavor and enforce darwin cgo minimum version in bisonw-desktop builds
- Fix double accounting for locked balance when calculating available balance in market maker bot allocations
- Get rid of dead asset Zclassic
- Correct market maker bot level-spacing minimum value to match precision
- Add MATIC token on Ethereum network, for users who may have accidentally withdrawn on incorrect network
- Fixes an persistent error for orders that were active at startup, but for which the server was no longer reachable, or the market no longer existed
- Change native Polygon asset from MATIC to POL
- A bunch of little UI bugs (#2968)
- Remove too-restrictive validation of seeds (when recovering password) that didn't account for new mnemonic seeds
- Disable estimatesmartfee RPC as a source for Bitcoin transaction fee rates. It's busted
- Increase transaction history query buffer for Zcash to accommodate shorter block times
- Fix bug on markets view where user orders showed a ticker in the order type section instead of the order type
- Use border color to show if address is valid or invalid instead of the incorrectly-styled checkmark
- Update neutrino dependency for Bitcoin SPV wallet
- Add epoch reporting for market maker bots to better inform users of the bot's current state
- Fix bug where orders revoked by the server during disconnects were left in limbo

## Code Summary

71 files changed, 1283 insertions(+), 625 deletions(-)

<https://github.com/decred/dcrdex/compare/v1.0.0...v1.0.1>

5 contributors

- Brian Stafford (@buck54321)
- @dev-warrior777
- Joe Gruffins (@JoeGruffins)
- Marton (@martonp)
- Philemon Ukane (@ukane-philemon)
