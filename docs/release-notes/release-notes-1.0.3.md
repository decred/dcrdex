# DCRDEX v1.0.3

This release contains several important fixes and improvements.  All users are advised to update.

Please read the [v1.0.0 release notes](https://github.com/decred/dcrdex/releases/tag/v1.0.0) for a full list of changes since v0.6.

## Fixes and Improvements

- Allow Firo to send to an EXX address ([#3119](https://github.com/decred/dcrdex/pull/3119))
- Fix Electrum Firo mm issues ([#3202](https://github.com/decred/dcrdex/pull/3202))
- Use server's fee rate for swap fee estimates ([#3200](https://github.com/decred/dcrdex/pull/3200))
- Add messaging for penalty compensation ([#3188](https://github.com/decred/dcrdex/pull/3188))
- Update dcrwallet to 4.3.0 ([#3197](https://github.com/decred/dcrdex/pull/3197))
- Update feature-policy header ([#3193](https://github.com/decred/dcrdex/pull/3193))
- Fix Polygon wallet connected/disconnected notification spam ([#3163](https://github.com/decred/dcrdex/pull/3163))
- Shorten systray tooltip ([#3183](https://github.com/decred/dcrdex/pull/3183))
- Ignore irrelevant connect errors ([#3182](https://github.com/decred/dcrdex/pull/3182))
- Handle Binance invalid listen key error ([#3139](https://github.com/decred/dcrdex/pull/3139))
- Fix Ethereum wallet user action resolution handling ([#3178](https://github.com/decred/dcrdex/pull/3178))
- Account for multi-split buffer when making MultiTrade ([#3156](https://github.com/decred/dcrdex/pull/3156))
- Fix market-maker balance handling for shared txs across matches ([#3138](https://github.com/decred/dcrdex/pull/3138))
- Fix Polygon polygon symbol conversion ([#3118](https://github.com/decred/dcrdex/pull/3118))
- Update static chinese translations ([#3071](https://github.com/decred/dcrdex/pull/3071))
- Translation German translations ([#3124](https://github.com/decred/dcrdex/pull/3124))
- Store correct Bitcoin tx history refund hash ([#3142](https://github.com/decred/dcrdex/pull/3142))
- Correct for Binance lot size and rate step filters ([#3093](https://github.com/decred/dcrdex/pull/3093))
- Populate the core.Exchange map fields even when not connected ([#3103](https://github.com/decred/dcrdex/pull/3103))
- Avoid error on RunStatsNote ([#3104](https://github.com/decred/dcrdex/pull/3104))
- Parse Binance error responses ([#3090](https://github.com/decred/dcrdex/pull/3090))

## Code Summary

530 files changed, 46490 insertions(+), 8867 deletions(-)

- Marton (@martonp)
- @dev-warrior777
- JoeGruffins (@JoeGruffins)
- Brian Stafford (@buck54321)
- @karamble
- @norwnd
- Peter Banik (@peterzen)