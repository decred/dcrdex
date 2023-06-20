# DCRDEX v0.6.2

**IMPORTANT**: This release adds support for the Decred 1.8 consensus agendas.  The native SPV Decred wallet in the previous release (v0.6.1) will not follow the chain following any hard forks resulting from DCP0011 or DCP0012. **You you must update to v0.6.2 if you use the native Decred wallet.**

This release also contains several important fixes and improvements.  All users are advised to update.

Please read the [v0.6.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.6.0) for a full list of changes since v0.5.

## Features and Improvements

- Support the Decred 1.8 consensus agendas. All users of native SPV Decred wallets **must update** before the hard forks activate. ([8aad3fa](https://github.com/decred/dcrdex/commit/8aad3fa400fd9624d2f537ba6524d860ab2c2243), [9908805](https://github.com/decred/dcrdex/commit/990880501649ac4b8ee8ab6a86ca82080b3d6cfd), [e0c0c8e](https://github.com/decred/dcrdex/commit/e0c0c8ef0fb1f512b05ad7dbc0a3f89d9497d815))
- SPV wallet backends for BTC-based assets (BTC, LTC, BCH) are now capable of reporting a fee rate to the application, even if that fee rate is from an external source such as a block explorer or other fee rate oracle.  To ensure optimal fee rates are used in all scenarios, be sure the "External fee rate estimates" checkbox in the wallet settings form is checked.  It should be checked by default for BTC.  Do not change your wallet settings if it is already checked. ([89e761a](https://github.com/decred/dcrdex/commit/89e761a2edfcf0f71b51858f3f98697d0f5db9b4))
- The UI form where the bond asset is chosen now has improved wording about fidelity bonds.  Information presented to the user about bonds will be an area of continual improvement as we work towards v1.0. ([29faa9b](https://github.com/decred/dcrdex/commit/29faa9b643f2e7e14a21d92214b8ed1040108db3))
- The http file server for the browser interface is now more efficient when serving static resources like images/style/js.  ([3622872](https://github.com/decred/dcrdex/commit/362287214a6785b4da9ab3f6af787e45cedbf22e))

## Fixes

- On Windows, perform automatic recovery of corrupted Ethereum transaction databases. ([b9c9ece](https://github.com/decred/dcrdex/commit/b9c9ece032619d1492238922a533eaca0cfb9ac3))
- The suggested BTC deposit amount when preparing to register with bonds is now reasonable.  Previously it was using the "max" fee rate, which suggested to the user a very large amount to proceed with registration using BTC.  Now it uses a stabilized live estimate. ([300e975](https://github.com/decred/dcrdex/commit/300e975f1fd82778124d54c10d8f9a0189019410))
- When adding a DEX from the Settings page, the "wallet wait" form no longer proceeds when there is insufficient balance.  Previously it would allow the user to proceed (and possibly fail) when the available balance was just the amount of one bond, when the required amount to proceed is double that amount plus the "fee buffer". ([3957ff7](https://github.com/decred/dcrdex/commit/3957ff772bd0c746d37a258ec9993766befd82b9))
- When opened from the Settings page, the "wallet wait" form no longer closes itself when clicked. ([b1b75a2](https://github.com/decred/dcrdex/commit/b1b75a2cf49d01dc053478d40161fcdeba5cb63c))

## Server

Decred's Go module dependencies are updated.  There are no functional server changes.

## Code Summary

11 commits, 41 files changed, 445 insertions(+), and 401 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.6.1...v0.6.2>

3 contributors

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
- Philemon Ukane (@ukane-philemon)
