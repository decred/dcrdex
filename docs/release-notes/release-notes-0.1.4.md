# DCRDEX v0.1.4

Jan 14, 2021

This patch release makes a number of small UI improvements, including showing
the user's account ID on the settings page, focusing password field, and
colorizing various buy/sell elements, and fixes several bugs.  The main bug
fixes deal with wallet settings changes, deposit address revalidation, Decred
withdrawals working for the full balance, and historical order and match
display.

Please read the [initial release (v0.1.0)
notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0) for
important information and instructions.

## Client (dexc)

### Features and Improvements

- The account ID for each configured DEX server is now displayed on the settings
  page. When not logged in, there is a placeholder that says to login to display
  the account ID.
  ([1a5c070](https://github.com/decred/dcrdex/commit/1a5c070196ab7899214b524a9c681168fbdcfd75))
- Focus password field in order dialogs.
  ([5eb9fb2](https://github.com/decred/dcrdex/commit/5eb9fb2de51722158eaf1d2122c11f30154bd9b3))
- Colorize the "Side" column in the orders table. ([83b07cd](https://github.com/decred/dcrdex/commit/83b07cd08a8a7ecced012335092c0f196d7fcfb0))
- The registration fee address is no longer logged if there is a funding error
  since there is nothing a user can do with the address other than shoot their
  self in the foot and send to it manually. Registration fee payment should only
  be done via the app.
  ([dc67cdb](https://github.com/decred/dcrdex/commit/dc67cdbb09fe6e296164da0b916ab8a1744912f6))
- Wallet balances are updated on all wallet settings changes.
  ([8ff4d94](https://github.com/decred/dcrdex/commit/8ff4d943d69182b9866faf6637e9e3c17e97db69))
- Wallet sync status is more consistently checked on wallet (re)connect events,
  and continually check sync status on RPC errors as it is common for
  node/wallet startup to initially error and then start reporting status (e.g.
  bitcoind's "verifying blocks..." error while starting up).
  ([1c9ca02](https://github.com/decred/dcrdex/commit/1c9ca02db974cbd76dceac5b29a825b5cc805c84),
  [53194c6](https://github.com/decred/dcrdex/commit/53194c615ee3179c2c6ec08278a41bbd9b234634))

### Fixes

- Fix changing wallet settings possibly interrupting active swaps.
  ([f0a304f](https://github.com/decred/dcrdex/commit/f0a304f7ea74af3ce75f3edc1cbb3f4f524f1c84))
- Fix a case where a wallet can become unlockable without restarting dexc if
  dexc were started with both active orders and an unlocked wallet.
  ([f8c47a1](https://github.com/decred/dcrdex/commit/f8c47a163387b8c63201f2f9ad1053a205e6203f))
- Fix duplicate notify_fee requests that resulted from multiple fee coin waiters
  being created for the same coin.
  ([ee1bd84](https://github.com/decred/dcrdex/commit/ee1bd84c8ef6136fcbbcf764782b610d20c3540c))
- Fix retrieving the full list of historical orders
  ([2846814](https://github.com/decred/dcrdex/commit/284681488b5812157dd8624151efc576764eb824))
- Fix incorrect year displayed for a match's date.
  ([a347b0f](https://github.com/decred/dcrdex/commit/a347b0f34d0fd143b566b59588cda4f86f1b218b))
- Wallet deposit addresses are validated and more often refreshed whenever the
  wallet is connected.
  ([c3990c7](https://github.com/decred/dcrdex/commit/c3990c765f7a7de2017da08c29fb9fae8853a522),
  [6a66a1c](https://github.com/decred/dcrdex/commit/6a66a1cb7701ed6d6e7187231a46ad1f2a74a782))
- Correctly handle chain sync status when in initial block download state, but
  blocks are up-to-date with headers. This is only possible in practice with
  simnet.
  ([3523de1](https://github.com/decred/dcrdex/commit/3523de11b270fed9162c0b2bd8aee2333fe2e8f6))
- Fix DCR withdraws in various cases.
  ([d0ba1e5](https://github.com/decred/dcrdex/commit/d0ba1e5dbcdc063c8fb4abf95725c67174868291)0
- Allow dexc to shutdown without hanging if a wallet was unexpectedly shutdown
  first.
  ([7321c36](https://github.com/decred/dcrdex/commit/7321c364297b8f5c0dd85cf798902b169bd3eebf))
- When loading active matches on login, correctly skip adding cancel order
  matches to the trades map.
  ([61697bb](https://github.com/decred/dcrdex/commit/61697bbc4364466d9eb55763aed8e7fb849e01e0))
- Prevent login while already logged in from re-creating the entries in the
  trades map.
  ([b6f81ad](https://github.com/decred/dcrdex/commit/b6f81adcc9a05f4c604420b3b138f1286b25c9c7))
- Resolve a data race on wallet reconfigure for DCR.
  ([bca1325](https://github.com/decred/dcrdex/commit/bca1325ab1ccdd21b3447571693a8212e5874e97))
- Avoid a possible deadlock on wallet reconfigure.
  ([4bed3e2](https://github.com/decred/dcrdex/commit/4bed3e2f55f97cac45ca30cf7ad4faac94d20604))

## Developer

- Simnet harnesses are quicker to start, being based on archives, and more well
  funded.
  ([0de8945](https://github.com/decred/dcrdex/commit/0de89456c129bc39a200e816fb660f216a7d41e2))
- Update simnet trade tests for current wallet unlocking system and more well
  funded harnesses.
  ([e198b1f](https://github.com/decred/dcrdex/commit/e198b1f095be8cad51c8e49604c873ed2ac4f02d))

## Server (dcrdex)

Create a fee rate scaling administrative endpoint. The endpoint is
`api/asset/{sym}/setfeescale/{scale}`, using a GET request instead of POST for
convenience.
([7a3f1831](https://github.com/decred/dcrdex/commit/7a3f18313a34a5945c064a06a1b85bfdc07b0dd4))

## Code Summary

27 commits, 52 files changed, 1582 insertions(+), and 890 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.1.3...v0.1.4>

6 contributors

- Amir Massarwa (@amassarwi)
- Brian Stafford (@buck54321)
- David Hill (@dajohi)
- Jonathan Chappelow (@chappjc)
- Kevin Wilde (@kevinstl)
- @peterzen
