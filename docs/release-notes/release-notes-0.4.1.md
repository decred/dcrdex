# DCRDEX v0.4.1

Feb 11, 2022

For a high level introduction to DCRDEX, please read the [initial release's notes](https://github.com/decred/dcrdex/blob/master/docs/release-notes/release-notes-0.1.0.md).

This is a patch release.

## Highlights

### Features

- Add a link to a wallet's log file, if available (<https://github.com/decred/dcrdex/commit/7aa2b516e203507c74c93487d8a5fa842862f5f0>)
- Add a rescan function for the native BTC wallet (<https://github.com/decred/dcrdex/commit/d1757c0091dd31494d5878a28cec8f67233822ad>, <https://github.com/decred/dcrdex/commit/5b2887cc10b79208d8182a5f35c047a27fdc05a5>, <https://github.com/decred/dcrdex/commit/3c6e02590db6bafe6aac3d047f9067bc2288abca>)
- Add Polish language translations (<https://github.com/decred/dcrdex/commit/cf3dbf4bc863705a369a37750f9f5e0ffc770231>, <https://github.com/decred/dcrdex/commit/bc7cff8336122beee2ab758da69d4fc4d91f7c8c>, <https://github.com/decred/dcrdex/commit/069d6de91152d58fbf93f8bcb05170fabda3bc1e>, <https://github.com/decred/dcrdex/commit/b90fe93e4f078996db2f64416c1a210ca57d0362>)
- Monitor and report changes to the number of peers each wallet has (<https://github.com/decred/dcrdex/commit/8ffa2e39f473a13f63b4f15670c17e12bad2b80d>)
- In the main dexc app, catch _and log_ any runtime panics (<https://github.com/decred/dcrdex/commit/7b33071fa381ffd2af314c8875dda88fdd91b552>)

### Fixes

- Do not display the new address button for ETH, where you only have one account address. (<https://github.com/decred/dcrdex/commit/71fe6227cc9c2cee384f4da592bf8c5da8fdf6c9>, <https://github.com/decred/dcrdex/commit/3ea7d7c39196614a079f51e1dfa4d05fb816b0ca>)
- Smoother (monotonic) BTC wallet sync status (<https://github.com/decred/dcrdex/commit/21db63f56c66064caf032b13ccdc2d03de8fa105>)
- Fix server accounting of maker redeems in certain scenarios (<https://github.com/decred/dcrdex/commit/f260918cdfefe06d08d22ef5a13d5a44e9c18f21>)
- Various fronted fixes (<https://github.com/decred/dcrdex/commit/80fac5850260f604a5f81c43a5207fcd380daf83>, <https://github.com/decred/dcrdex/commit/6738bca369ead1e8f8f3560b1ea76df0a59fdfe3>)
- Avoid a deadlock when resuming trades on restart that require counterparty contract audit information (<https://github.com/decred/dcrdex/commit/d70547b5030ca0e7b2ffff9903c293abcd4421d3>)
- Fix a data race when reconfiguring a wallet (<https://github.com/decred/dcrdex/commit/486ed36033a5e251082fd9c6542a1d2a9a1e8f1b>)
- Allow the wallet Lock to timeout, in the event that the wallet backend is down or hung (<https://github.com/decred/dcrdex/commit/d67198f9cb554783801b94a1fcf86412b740ced5>)
- Allow a Decred RPC client to be restarted, such as if a reconfiguration attempt failed and the previous client needs to be reconnected (<https://github.com/decred/dcrdex/commit/74f4ec85a22d40825233141a6c7905cfd44b3ed2>)

### Developer

- Testing: add simnet node control scripts (<https://github.com/decred/dcrdex/commit/8613814a9e6f93339a1f9e0d35c1a70c19a71f72>)

## Important Notices

If upgrading from v0.2, read the [Upgrading section of the v0.4.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.4.0#upgrading) for important information.

## Code Summary

22 commits, 78 files changed, 3,589 insertions(+), 418 deletions(-)

[https://github.com/decred/dcrdex/compare/v0.4.0...v0.4.1](https://github.com/decred/dcrdex/compare/v0.4.0...v0.4.1)

**Full Changelog**: <https://github.com/decred/dcrdex/compare/v0.4.0...v0.4.1>
