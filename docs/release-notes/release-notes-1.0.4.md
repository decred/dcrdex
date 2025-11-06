# DCRDEX v1.0.4

This release contains several important fixes and improvements.  All users are advised to update.

Please read the [v1.0.0 release notes](https://github.com/decred/dcrdex/releases/tag/v1.0.0) for a full list of changes since v0.6.

## Fixes and Improvements

- Updated dcrwallet dependency to 4.3.1 ([#3387](https://github.com/decred/dcrdex/pull/3387))
- Fixed a seg fault bug on window close on Darwin ([#3366](https://github.com/decred/dcrdex/pull/3366))
- Implemented snap package ([#2580](https://github.com/decred/dcrdex/pull/2580))
- Updated libwebkitgtk deps ([#3232](https://github.com/decred/dcrdex/pull/3232))
- Fixed a panic on nil dex connection config ([#3345](https://github.com/decred/dcrdex/pull/3345))
- Updates MM bot run end time after each event ([#3305](https://github.com/decred/dcrdex/pull/3305))
- Fixed an overflow bug VWAP calculations ([#3299](https://github.com/decred/dcrdex/pull/3299))
- Show upgrade banner if newer version is available on GitHub ([#3286](https://github.com/decred/dcrdex/pull/3286))
- Send notifications for changes in DEX account status ([#3280](https://github.com/decred/dcrdex/pull/3280))
- Deserialize Firo blocks from bytes ([#3263](https://github.com/decred/dcrdex/pull/3263))
- Fixed a bug where we were using a closed book feed channel ([#3254](https://github.com/decred/dcrdex/pull/3254))
- Fixed GetBlockHash for Firo compatibility ([#3251](https://github.com/decred/dcrdex/pull/3251))
- Added `ExchangeWalletCustom` to support external btc-clone wallets ([#2855](https://github.com/decred/dcrdex/pull/2855))
- Fixed electrum wallet transaction DB panic ([#3248](https://github.com/decred/dcrdex/pull/3248))
- Enabled mixing for v5 wallet. ([#3242](https://github.com/decred/dcrdex/pull/3242))
- Check for used addresses ([#2690](https://github.com/decred/dcrdex/pull/2690))
- Hide copy button on unsecure connection ([#3226](https://github.com/decred/dcrdex/pull/3226))
- Switched to custom bisoncraft version for webview dependency ([#3230](https://github.com/decred/dcrdex/pull/3230))
- Changed HTML titles to Bison Wallet ([#3223](https://github.com/decred/dcrdex/pull/3223))
- Updated btcwallet dependency to v0.16.10 ([#3206](https://github.com/decred/dcrdex/pull/3206))
- Created new syncer on Run error ([#3215](https://github.com/decred/dcrdex/pull/3215))
- Handle market parameters update ([#2846](https://github.com/decred/dcrdex/pull/2846))
- Use the app build version as Version in UI in hamburger menu

## Code Summary

153 files changed, 4252 insertions(+), 2103 deletions(-)

- JoeGruffins (@JoeGruffins): +683, -757, total: -74
- Marton (@martonp): +2018, -1076, total: 942
- @dev-warrior777: +637, -79, total: 558
- Peter Banik (@peterzen): +645, -45, total: 600
- Brian Stafford (@buck54321): +313, -90, total: 223
