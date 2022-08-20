# DCRDEX v0.2.0 (beta)

May 7, 2021

For a high level introduction to DCRDEX, please read the [0.1.0 release notes](release-notes-0.1.0.md).

## Highlights

This release includes a large number of improvements to the UI, the
communications protocol, and software design.

The most notable new features are:

- Numerous UI and usability enhancements including responsive design and depth
  chart interactivity
- Support client control by the Decrediton GUI wallet and use of its accounts
- Experimental Bitcoin Cash (BCH) support
- Initial changes to support SPV (light) wallets in the next release
- Account import/export

The latest 1.6 release of dcrd and dcrwallet is required for this release of
DCRDEX. At the time of release, this corresponds to the v1.6.2 releases.
Bitcoin Core 0.20 and 0.21 are both supported.

## Important Notices

Although DCRDEX looks and feels like a regular exchange, the "decentralized"
aspect brings an expanded role to the client. Please take the time to read and
understand the following:

- Ensure your nodes (and wallets) are fully **synchronized with the blockchain
  network before placing orders**.
- **Never shutdown your wallets with dexc running**. When shutting down, always
  stop dexc before stopping your wallets.
- If you have to restart dexc with active orders or swaps, you must
  **immediately login again with your app password when dexc starts up**.
- There is an "inaction timeout" when it becomes your client's turn to broadcast
  a transaction, so be sure not to stop dexc or lose connectivity for longer
  than that or you risk having your active orders and swaps/matches revoked. If
  you do have to restart dexc, remember to login as soon as you start it up
  again.
- Only one dexc (client) process should be running for a given user account at
  any time. For example, if you have identical dexc configurations on two
  computers and you run dexc and login on both, neither dexc instance will be
  adequately connected to successfully negotiate swaps. Also note that order
  history is not synchronized between different installations.
- Your DEX server accounts exist inside the dexc.db file, the location of which
  depends on operating system, but is typically in ~/.dexc/mainnet/dexc.db or
  %HOMEPATH%\Local\Dexc\mainnet\dexc.db. Do not delete this file.
- If you use a non-default bitcoin wallet name, don't forget to set it in
  bitcoin.conf with a `wallet=wallet_name_here` line so that bitcoind
  will load it each time it starts. Otherwise, dexc will give you a "wallet not
  found" error on startup and login.
- bitcoind's "smart" fee estimation needs plenty of time to warm up or it is not
  so smart. When possible, keep your bitcoind running for at least 6 blocks,
  especially if had not been running for more than an hour, or ensure that the
  value returned from a bitcoin-cli call to `estimatesmartfee 1` returns a
  `"feerate"` close to what <https://mempool.space/> reports as "High priority".

## Client (dexc)

### Features and Improvements

- Experimental support for Bitcoin Cash (BCH).
  ([542ed9b](https://github.com/decred/dcrdex/commit/542ed9ba9ef8db3d8f5d3e36b3078c9fa3d5888f))
- Show confirmations for swaps transactions on the Order page when a swap has
  not yet reached the required number of confirmations.
  ([ecbfebd](https://github.com/decred/dcrdex/commit/ecbfebdd6f3184b51d6f72011b488edd73e3c5cb))
- Open dialogs can be closed by hitting the "Escape" key.
  ([7c978cd](https://github.com/decred/dcrdex/commit/7c978cd8436c3fab9e90a80d1a17502e945e76f8))
- Allow changing the dex client "application password".
  ([8d7163c](https://github.com/decred/dcrdex/commit/8d7163cb9022f9f19ef706f33251a32d228d1c02))
- Responsive browser UI design.
  ([c91bde4](https://github.com/decred/dcrdex/commit/c91bde4446a5d3e6c3db20a805da380d4c6bf835))
- Differentiate between buy/sell orders in confirmation modal dialog.
  ([2bdf81f](https://github.com/decred/dcrdex/commit/2bdf81f94262c34a345c84daf4def921e41c7107))
- Clearer revocation notifications.
  ([c8c9729](https://github.com/decred/dcrdex/commit/c8c9729ba6adb5a1803a8a7e9de30e8aaa81c942))
- Raw transaction data is now transmitted to counterparties in the `'audit'` and
  `'match_status'` requests. This is a prerequisite for SPV clients.
  ([3704513](https://github.com/decred/dcrdex/commit/3704513bc63facc234edabf742ec1f87ef80b35d))
- More chart interactivity. (a) Indicators on the depth chart for the user's
  orders. When the mouse hovers near the indicator, the order is highlighted in
  the "Your Orders" table. Conversely, when the mouse hovers over a row in the
  "Your Orders" table, the indicator is highlighted on the chart. (b) Move the
  legend and hover info to the top. (c) When a rate is entered in the order form
  for a limit order, display a line indicator at that rate on the depth chart.
  (d) When the user hovers over an order in the buy/sell tables, display an
  indicator at that rate on the depth chart. (e) Last zoom level is saved across
  markets and reloads. (f) Display the total buy/sell volume.
  ([fb6f3ea](https://github.com/decred/dcrdex/commit/fb6f3eaa7dae7d043aae8ceb5771638554dd5d1c),
  [08ec4ac](https://github.com/decred/dcrdex/commit/08ec4ac99f06aa23d654cd3855c62c13945b2a2e))
- Multiple authorized browser sessions are now permitted. This refers to logging
  in to dexc from two different browsers that do not share a cookie store. This
  is now permitted, however, signing *out* of one session signs out of all
  sessions.
  ([030173b](https://github.com/decred/dcrdex/commit/030173b828fabb0cb4b2c9884bbeaa91b1ce7f11))
- A wallet's connection settings and private passphrase can be changed at the
  same time. Developers should see the `ReconfigureWallet` change.
  ([761e3e1](https://github.com/decred/dcrdex/commit/761e3e13981a9595623718036913fe54a2c2d764))
- Add account import/export functions.
  ([1a38c4d](https://github.com/decred/dcrdex/commit/1a38c4decea6c0c02370edf4ed4eee833afbe8c8))
- Add account disable function.
  ([f414a87](https://github.com/decred/dcrdex/commit/f414a87a3991d8abbdd8347dc46fe350c96d9981))
- Starting dexc when a configured DEX server is unreachable starts a reconnect
  attempt loop. Previously it was necessary to restart dexc later and hope the
  server was back.
  ([c782ffb](https://github.com/decred/dcrdex/commit/c782ffb0a8e4b8dd11360c8a6394a6b64c91cd3e))
- Account-based DCR wallet locking support. With dcrwallet 1.6, accounts may be
  individually encrypted, with a unique password, in contrast to whole-wallet
  locking. This allows working with such accounts by using the `accountunlocked`
  dcrwallet RPC to determine the locking scheme for an account, and the
  `unlockaccount`/`lockaccount` RPCs instead of `walletpassphrase`/`walletlock`.
  The "beta" simnet DCR harness now uses an individually-encrypted "default"
  account.
  ([ff4e76c](https://github.com/decred/dcrdex/commit/ff4e76cc509e0019f1239316db9e8c7639faf38f),
  [37cdc9e](https://github.com/decred/dcrdex/commit/37cdc9e640e032ec9b0f0f2da12be637e0f696ea))
- Handling of new server-provided fee rates. This will support SPV clients, and
  helps ensure that both redeem and order funding transactions are not created
  with low fee rates when the client's wallet/node is not providing good
  estimates.
  ([79a1cb0](https://github.com/decred/dcrdex/commit/79a1cb016f8d08354610767e890c78a9bd3470b4))
- Network-specific loopback IPs for the webserver and RPC server listen
  addresses. Now by default, dexc listens on 127.0.0.1 for mainnet, 127.0.0.2
  for testnet, and 127.0.0.3 for simnet. Users are still be able to specify
  custom addresses with `webaddr` and `rpcaddr`.
  ([08ec4ac](https://github.com/decred/dcrdex/commit/08ec4ac99f06aa23d654cd3855c62c13945b2a2e))
- Maximum order size estimates on order dialog. Get maximum order estimates
  based on wallet balances and, in the case of buy orders, the rate in the rate
  input field. The data is shown in the UI as a small message above the rate
  field. When you click on the label, the quantity fields are pre-populated with
  the max order.
  ([920d1ac](https://github.com/decred/dcrdex/commit/920d1ac50b748ea17c177fe5cad2259300361fef))
- dexcctl / RPC server: Add a matches list to the `myorders` response.
  ([3bef6ba](https://github.com/decred/dcrdex/commit/3bef6ba8ff8df2af7a8c37fd1c43e6119a1af32f))
- When orders are placed, the client remembers the expected maximum fee rate,
  and verifies that the rate provided by the server at match time does not
  exceed this amount.
  ([2123f10](https://github.com/decred/dcrdex/commit/2123f10a509bbf26ba3e74d3a7583b6524ba9191))
- Add a "fee rate limit" setting to each wallet that is checked against the max
  fee rate set by the server's config. Orders are blocked by the client if the
  server specifies a max fee rate that exceeds the client's limit.
  ([414ffcc](https://github.com/decred/dcrdex/commit/414ffccd83972718d9c3058526101ab745eb36d2))
- On shutdown, the active orders are logged, and inactive trades have their
  coins released to the wallet if they were not already.
  ([41749a8](https://github.com/decred/dcrdex/commit/41749a83afb9f234c3f5c5755367df3beb77d8f1))
- Add sample config files. (also server)
  ([792602b](https://github.com/decred/dcrdex/commit/792602bdab53f2b933f51142df79432fada378e8))
- The `'init'` and `'redeem'` requests are now run asynchronously so most other
  actions are not blocked while waiting for a response. This is generally an
  internal change, but it may improve the overall responsiveness of the dexc
  application.
  ([a0538bb](https://github.com/decred/dcrdex/commit/a0538bbef5460dd08342b8c99851bc6076faea73))
- Preimage request handling is reworked to prevent blocking for a long period
  given an incoming preimage request for an unknown order.
  ([1b66492](https://github.com/decred/dcrdex/commit/1b66492b1ef53670f32c19b484e7366fab977993))
- Add a custom webserver "site" directory argument.
  ([4506406](https://github.com/decred/dcrdex/commit/4506406d974aaa8463a2e924ccdf1147d8078c45))
- Favor confirmed UTXOs in BTC order funding. This is primarily an internal
  change, but it can defend against swaps that take too long to confirm.
  ([3f6e429](https://github.com/decred/dcrdex/commit/3f6e4299c81751558891bcc209cd50825e10183c))
- "Long" execution times (more than 250ms) for incoming message handling and
  track ticks are now logged.
  ([529cb0d](https://github.com/decred/dcrdex/commit/529cb0d17154f3a8a391848fa8eb2ff65453ed9c))

### Developer

- When `Core` is shut down, wallets are locked when the `Run` method returns.
  Previously, wallets were only locked if the consumer used `PromptShutdown`.
  ([8976d8d](https://github.com/decred/dcrdex/commit/8976d8d166706ba626cee437d8765fa3ca3794e4))
  This change was in 0.1.5, but it is reiterated here as it is an significant
  change in behavior that Go consumers should note.
- Updates to the `User` struct returned by the `User` and `Exchanges` methods of
  `client/core.Core`.  The `client/core.Market` has replaced the
  `{Base,Quote}{Order,Contract}Locked` fields with methods.
  ([167efd4](https://github.com/decred/dcrdex/commit/167efd491eb5eed4c1980995be6b48ea5a780f2a))
- When specifying TLS certificates, allow either a filename or the contents of
  the certificate file. This applies to the `Register`, `GetFee`, and
  `GetDEXConfig` methods of `client/core.Core`.
  ([44a3363](https://github.com/decred/dcrdex/commit/44a33633e40bee15e70b700b982c69833317692f))
- Notification subjects are now package-level constants.
  ([3aef72d](https://github.com/decred/dcrdex/commit/3aef72d00790b2d8b0354efaf844a8fd08f3bdc6))
- `ReconfigureWallet` has a new pass input (`nil` indicates no password change).
  ([761e3e1](https://github.com/decred/dcrdex/commit/761e3e13981a9595623718036913fe54a2c2d764))
- New order fee estimate API. See the new `(*Core).PreOrder` method and the new
  returned `OrderEstimate` type. Also see the `PreSwap` and `PreRedeem` methods
  of `client/asset.Wallet`, and the new types of the same name.
  ([5394cea](https://github.com/decred/dcrdex/commit/5394ceaa2a0e69518cabb2a3ae4fdb2164a6a08e))
- New `isinitialized` http API endpoint and `Core` method.
  ([b767a23](https://github.com/decred/dcrdex/commit/b767a23f0c89ec54a1f60e61c9bd21d05daba906))
- Add the `(*Core).GetDEXConfig` method and a corresponding http API endpoint
  `getdexinfo` the functions similar to `getfee` by making a temporary
  connection to a DEX with *no existing account*, except that it returns the
  server's entire config.
  ([d85f6bc](https://github.com/decred/dcrdex/commit/d85f6bc34d74773a56500089ff4d12e0e3d3380e))
- Check the server's API version and each asset's version that are now returned
  in the server's `'config'` response.
  ([e59b47f](https://github.com/decred/dcrdex/commit/e59b47fe777ba94741a38da62b06edd317101873),
  [205e802](https://github.com/decred/dcrdex/commit/205e8022adcaf8e7a5bda026e80d43edc3c07497),
  [1bc0cc9](https://github.com/decred/dcrdex/commit/1bc0cc9e9c1ffab8b6ed37dcad96cc136bbcf33f))
- Only active orders are listed by the `User` and `Exchanges` methods of `Core`.
  Completed orders that are pending retirement are excluded.
  ([6358d97](https://github.com/decred/dcrdex/commit/6358d97fe1a1977c363519a98c15273c0612a6b2))
- Add profiling switches to dcrdex. A CPU profile file may be specified with
  `--cpuprofile`. The http profiler may be started with `--httpprof`.
  ([c17baf9](https://github.com/decred/dcrdex/commit/c17baf93a76f6803af1d354bf0d9d7a51332e475))
- (internal) DCR asset backends now use `rpcclient/v6`, which provide cancelable requests.
  ([312397a](https://github.com/decred/dcrdex/commit/312397a56d307f71c12696bcaae126f4629c7aea),
  [9d65d55](https://github.com/decred/dcrdex/commit/9d65d55c8f3b0ca613fb3a7d401346706d65d7e7))
- (internal) BTC's asset backend now uses Decred's `rpcclient` package for
  cancellation capability. All request now use `RawRequest`.
  ([cefe6a5](https://github.com/decred/dcrdex/commit/cefe6a5ced3cce460311d3b50dffdfd4ce9aa22f))
- (internal) All incoming response and notification message handlers are wrapped
  for panic recovery.
  ([829a661](https://github.com/decred/dcrdex/commit/829a661a4c134ad575f9fdbac67b812c73a53589))
- (internal) Message unmarshalling is now more robust with respect to null
  payloads.
  ([9bf1a3e](https://github.com/decred/dcrdex/commit/9bf1a3eeacb86247e8b33067aabcf7fc0cf59f8a))
- Many third party Go dependency updates.
  ([go.mod diff](https://github.com/decred/dcrdex/compare/4517832...release-v0.2#diff-33ef32bf6c23acb95f5902d7097b7a1d5128ca061167ec0716715b0b9eeaa5f6))
- Update site build system to Webpack 5, and update most other deps.
  ([a8e76ea](https://github.com/decred/dcrdex/commit/a8e76eacdecf4e30d96248a885c13a27f81a867e))
- Add an ETH simnet harness for support of upcoming ETH support.
  ([ea10f5a](https://github.com/decred/dcrdex/commit/ea10f5a032ddc365f6982de2f0956e560a26dc2e))
- The simnet harnesses now listen on all interfaces.
  ([4e246cf](https://github.com/decred/dcrdex/commit/4e246cfbef547f41fee5bad2ab397ab80d89dc2a))
- The Decred wallet harnesses now start dcrwallet with http profiling enabled.
  ([b96f546](https://github.com/decred/dcrdex/commit/b96f546f3ff67c3fed3f013023c395daab115fc0))
- Rework the `db.MetaMatch` struct.
  ([db3df62](https://github.com/decred/dcrdex/commit/db3df625eb736442a464dcc3aea424e8a8491a78))

### Fixes

In addition to numerous fixes that were also in the 0.1.x releases, the
most notable fixes are:

- No longer show the Register dialog if the server for the only registered DEX
  happens to be down at the time.
  ([b6ea0ea](https://github.com/decred/dcrdex/commit/b6ea0eaccd92f0c1fe8058a943013217cbf38b0e))
- Correct handling of IPv6 listen addresses. (also on server)
  ([f0ef965](https://github.com/decred/dcrdex/commit/f0ef965103e035052c5034d0486b44881a0c1067))
- Update the browser UI when orders are placed via dexcctl.
  ([8cc1502](https://github.com/decred/dcrdex/commit/8cc1502b0bb511513c0cf70117cbc29b0952f8db))
- Better error reporting on the DEX registration dialog.
  ([2617d75](https://github.com/decred/dcrdex/commit/2617d750f5207d52dfb7afab82634253a99b514c))
- More robust recovery for orders that become unfunded (e.g. user spends coins
  that were reserved for an order).
  ([122277e](https://github.com/decred/dcrdex/commit/122277ec0eca5e5cb8151536aa2e2532d15d9af2))
- No longer prematurely broadcast Decred refund transactions.
  ([03cdf2d](https://github.com/decred/dcrdex/commit/03cdf2d473fc7546892112d78cfa3cfeb6a89605))
- Commitment checksum handling in the presence of missed preimage is now handled
  the same way as on the server by including the all epoch order commitments in
  the csum computation, not just the ones with revealed preimages.
  ([25e3679](https://github.com/decred/dcrdex/commit/25e3679e4e909adf5f0c34821ef14b34357fab42),
  [7d71ffd](https://github.com/decred/dcrdex/commit/7d71ffd5221e0965aefd532e05d6520b17302a54))
- Never show negative confirmations for swap transactions even before they have
  been checked.
  ([fb39b97](https://github.com/decred/dcrdex/commit/fb39b97f1d73e942f6dd79ef0cf0ae4e1a061fcc))
- The mouse wheel only zooms when hovering over the depth chart, no longer
  scrolling the page at the same time.
  ([736b005](https://github.com/decred/dcrdex/commit/736b005151cf3501f0724e36f070e4b74ac365e5))

## Server (dcrdex)

- `Swapper` resumes on startup from DB rather than a state file.
  ([a676e07](https://github.com/decred/dcrdex/commit/a676e074a6845a2b05b3597856d226a22f3c9234))
- Market data API endpoints. `/spots` is the spot price and booked volume of all
  markets. `/candles` is candlestick data, available in bin sizes of 24h, 1h,
  15m, and per-epoch sticks. e.g. `/candles/dcr/btc/15m`. `/orderbook` is
  already a WebSocket route, but is now also accessible by HTTP. An example URL
  is `/orderbook/dcr/btc`. `/config` is another WebSocket route that is also now
  available over HTTP too.
  ([08afde3](https://github.com/decred/dcrdex/commit/08afde3f7bb5d9df5579621ed0e7ae9850424744))
- Configurable trade limits with the new `--inittakerlotlimit` and
  `--abstakerlotlimit` dcrdex switches, and `userBookedLotLimit` set in
  markets.json.
  ([5771186](https://github.com/decred/dcrdex/commit/5771186321eb7b969685fc722ffdb65d42a64539))
- Provide API and asset versions in the `'config'` response.
  ([e59b47f](https://github.com/decred/dcrdex/commit/e59b47fe777ba94741a38da62b06edd317101873),
  [205e802](https://github.com/decred/dcrdex/commit/205e8022adcaf8e7a5bda026e80d43edc3c07497),
  [1bc0cc9](https://github.com/decred/dcrdex/commit/1bc0cc9e9c1ffab8b6ed37dcad96cc136bbcf33f))
- Begin sending `TxData` (raw tx) in audit and match_status requests to
  counterparty. This will support SPV clients.
  ([370451](https://github.com/decred/dcrdex/commit/3704513bc63facc234edabf742ec1f87ef80b35d))
- Experimental Bitcoin Cash (BCH) support.
  ([542ed9b](https://github.com/decred/dcrdex/commit/542ed9ba9ef8db3d8f5d3e36b3078c9fa3d5888f))
- Version the DB scheme and implement initial updates to populate historical
  market data in the `epoch_reports` table.
  ([d000f19](https://github.com/decred/dcrdex/commit/d000f196923497122a71790e8c8f0c89503cfaaf))
- The outgoing preimage request now includes the commitment for the preimage
  being requested.
  ([850e8a6](https://github.com/decred/dcrdex/commit/850e8a6779dd537ecb820974606a9f7294ca155a))
- Provide fee rate estimates to the clients in certain messages: `orderbook`,
  `epoch_report`, and the new `fee_rate` route. With this data provided to the
  clients, minimum fee rates of zero-conf funding coins are enforced.
  ([79a1cb0](https://github.com/decred/dcrdex/commit/79a1cb016f8d08354610767e890c78a9bd3470b4),
  [9885bf1](https://github.com/decred/dcrdex/commit/9885bf1ed697bd82ba300ed33c98647c2413df8f))
- Fix market suspension not purging the outgoing book router's orders list. The
  actual book was purged, but clients would still pull a book snapshot listing
  orders if they restarted after a purge.
  ([a25d14e](https://github.com/decred/dcrdex/commit/a25d14e3a397268bae8a8e899b836d53e0219d79))
- Order priority queue automatic reallocation and smaller initial capacity.
  ([3750cce](https://github.com/decred/dcrdex/commit/3750cce2abeed01879d26b54483b85a2f78f9187))
- New administrative endpoints: `orderbook`, `epochorders`, and `matches`.
  ([0ce3ec7](https://github.com/decred/dcrdex/commit/0ce3ec70024f48fc21b9e9e8aee925e31e2ceb02))
- Add order ID to cancel route error message.
  ([0a7157b](https://github.com/decred/dcrdex/commit/0a7157bb2c2511ac85842e88bb7ead0e127a85ae))
- Various test harness improvements.
  ([ca9882d](https://github.com/decred/dcrdex/commit/ca9882d36463c94375c200ca09cf1a44740f6d1a))
- Active order counts are logged when a user authenticates.
  ([945cb4a](https://github.com/decred/dcrdex/commit/945cb4aa618990c9ab66ee09954d0357e479bc6c))
- Drop the dependency on the deprecated golang.org/x/crypto/ssh/terminal
  repository.
  ([cae9f5a](https://github.com/decred/dcrdex/commit/cae9f5a12ef24c8f4f0d44a021768323b91aaa1f))
- The /api/market/{marketID}/matches endpoint now returns decoded swap/redeem
  coin IDs and an idiomatic JSON response.
  ([a1fbdc0](https://github.com/decred/dcrdex/commit/a1fbdc023c2dfd95d89ab66d91594be5d98ce011))

## Build requirements

- Go 1.15 or 1.16
- Node 14 is the minimum supported version for building the site assets.
- dcrd and dcrwallet must *still* be built from their `release-v1.6` branches.
- The minimum required dcrwallet RPC server version is 8.5.0, which corresponds
  to the v1.6.2 patch release of dcrwallet, but the latest `release-v1.6.x` tag
  should be used.

## Code Summary

166 commits, 287 files changed, 40,296 insertions(+), 18,072 deletions(-)

<https://github.com/decred/dcrdex/compare/4517832...release-v0.2>

9 contributors

- Amir Massarwa (@amassarwi)
- Brian Stafford (@buck54321)
- David Hill (@dajohi)
- Joe Gruffins (@JoeGruffins)
- Jonathan Chappelow (@chappjc)
- Kevin Wilde (@kevinstl)
- @peterzen
- Victor Oliveira (@vctt94)
- Wisdom Arerosuoghene (@itswisdomagain)
