# DCRDEX v0.6.1

This release contains several important fixes, and adds DigiByte support.

You should update to this release if you want to use Zcash.  It is strongly recommended to use v5.4.2 with this release of DCRDEX until v5.5.0 has been evaluated.

Please read the [v0.6.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.6.0) for a full list of changes since v0.5.

## Features

- Add DigiByte (DGB) support.  [DigiByte Core v7.17.3](https://github.com/DigiByte-Core/digibyte/releases/tag/v7.17.3) is now a supported external wallet.  The v8.22 release candidate has not been evaluated yet, but there are several RPC changes that will require an update. (<https://github.com/decred/dcrdex/pull/2323>)
- Recent matches are sent from the connected server on subscription to a book. ([295e862](https://github.com/decred/dcrdex/commit/295e862cc54494838e86c4f24360f89458ea2e87))
- The required zcashd version has been bumped to zcashd v5.4.2.  Version 5.5.0 has not been evaluated. ([d361aa5](https://github.com/decred/dcrdex/commit/d361aa5da7ab08476b632326d1775a31bda64b65))
- The rate of an order is now displayed in the header for the order in the Your Orders table. ([b5aac2e](https://github.com/decred/dcrdex/commit/b5aac2ee779ace30beec4cd530738e6a56ac3013))
- The last selected candle bin width is remembered.  The default for first use is 1hr. ([584f3e0](https://github.com/decred/dcrdex/commit/584f3e068fc0f8d2f05b26d431ad817eb335ea35))

## Fixes

There are a number of important fixes.

- If you use Litecoin, it is strongly recommended to update to this release. See [302e0e5](https://github.com/decred/dcrdex/commit/302e0e516a5400820aca233240c149402c76c50e).
- A number of wallet reconfiguration bugs are also fixed. See <https://github.com/decred/dcrdex/pull/2323>.
- Several important ZEC fixes. See [d892fde](https://github.com/decred/dcrdex/commit/d892fde4b2b4686d49bda422041c0b450688c6f0), [0eeb983](https://github.com/decred/dcrdex/commit/0eeb983a3c88aabf6a8ecc8f94056215bc8f3d08), [1abd06d](https://github.com/decred/dcrdex/commit/1abd06d84fda11b80c789eeba6139f3fa3b03be4), [f7fb3b9](https://github.com/decred/dcrdex/commit/f7fb3b9f5543a6932e183eae9d4ee9632c074bb7), [d361aa5](https://github.com/decred/dcrdex/commit/d361aa5da7ab08476b632326d1775a31bda64b65), [c8f7d91](https://github.com/decred/dcrdex/commit/c8f7d9152f12d39b95a2726c04dcd9b014bf7bdc), and [e3b333e](https://github.com/decred/dcrdex/commit/e3b333ee710a4519d26fda3dd3201aad167e1cb2).

See the [0.6.1 milestone](https://github.com/decred/dcrdex/milestone/30?closed=1) for the full list of fixes.

## Server

Startup order funding balance checks are fixed for when an account-based asset is the quote asset on the market. ([02abda2](https://github.com/decred/dcrdex/commit/02abda21c00599e15a5b6fba7c334123918f7c73))

## Code Summary

45 commits, 75 files changed, 1,769 insertions(+), and 364 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.6.0...v0.6.1>

7 contributors

- Brian Stafford (@buck54321)
- Joe Gruffins (@JoeGruffins)
- Jonathan Chappelow (@chappjc)
- @martonp
- @norwnd
- @peterzen
- Philemon Ukane (@ukane-philemon)
