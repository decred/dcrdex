# DCRDEX v0.5.3

Sept 26, 2022

**IMPORTANT:** Use the [v0.5.4 patch release](https://github.com/decred/dcrdex/releases/tag/v0.5.4) (or newer) instead.  There is a critical bug fix related to Bitcoin block parsing.

Please read the [v0.5.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.5.0) for a full list of changes since v0.4.

For important information and an introduction to DCRDEX, read the [initial release (v0.1.0) notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0).

Either download the file for your distribution below, or wait for the signed distributions to be created at <https://github.com/decred/release/tags>.

## Features

- Add translations for de-DE. (<https://github.com/decred/dcrdex/commit/d8c6ce38a654c06be3e8f89c371779bd160dc62a>)
- Native BTC wallets now have an option to obtain a current network fee rate from an external source. (<https://github.com/decred/dcrdex/commit/1af28ea482f408601d2c3837d420eba3fd698c82>)
- Decred wallets now have the ability to change certain settings without requiring a full restart of the wallet. This allows the user to change certain minor settings such as the option to use the external fee rate source while trades are active. However, it is still recommended to keep wallet settings unchanged while trades are active. (<https://github.com/decred/dcrdex/commit/2ac43deb63bf153606ad28816a26c9e32ff5768c>)

## Fixes

- Concurrent login requests are handled in sequence now, preventing certain bugs when the user attempts to login again while a previous login request is being handled. (<https://github.com/decred/dcrdex/commit/5084446542f9142e4f08c444c704604d86b7e70d>)
- The password fields on the wallet configuration forms are generated with the `autocomplete = 'off'` attribute. This improves privacy by preventing the browser from remembering the values entered. (<https://github.com/decred/dcrdex/commit/3d365d73087fc615b816e96709d338d877bbb219>)
- The network fees paid for a swap redemption were incorrectly shown on the order details page. The correct fees are now shown. (<https://github.com/decred/dcrdex/commit/11ce1ffcd3cc405797cd7718f1ff254a796a547e>)
- Various frontend fixes. (<https://github.com/decred/dcrdex/commit/b01d81c01154b08306d040dbaca903d21f3f9acc>)
- More thorough handling of epoch status orders when network connectivity is interrupted and restored. This may resolve a variety of uncancellable orders, as well as perpetual locking of funds, which was previously only resolvable by restarting the DEX client. (<https://github.com/decred/dcrdex/commit/942e5946d43db4750f313fc9260093e663904210>)
- The default RPC port is removed from the Electrum wallet configuration form since there is no default. (<https://github.com/decred/dcrdex/commit/e9ca06d353c7aa1f66a120ac8b9b8a6cc021983e>)

## Server

- The server now sends a `revoke_order` notification after a preimage miss to assist with epoch order status resolution with clients that had lost connectivity at the time of the preimage request, but regained connectivity before the end of that epoch's match cycle. (<https://github.com/decred/dcrdex/commit/26fa5a1e4f4b9820a3681de35e6aab4482a676d3>)

## Code Summary

11 commits, 46 files changed, 3,670 insertions(+), and 323 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.5.2...v0.5.3>

5 contributors

- Jonathan Chappelow (@chappjc)
- @karamble
- @martonp
- Victor Oliveira (@vctt94)
- Philemon Ukane (@ukane-philemon)
