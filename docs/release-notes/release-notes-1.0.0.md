# DCRDEX v1.0.0

This release introduces a major version upgrade. Backwards compatibility is
not strictly preserved.

There are many new features, improvements, and bug fixes since v0.6.

## Features and Improvements

- Add Decred staking
- Add Decred mixing
- Add Polygon and Polygon tokens USDC, USDT, WETH, WBTC
- Add Ethereum token USDT
- Implement multi-balance contracts for Ethereum and Polygon to reduce provider request rate
- Use free RPC providers by default for Ethereum and Polygon to enable one-click wallet creation
- Implement Zcash shielded pool features and refactor wallet to be shielded-by-default
- Add Digibyte, Firo, Zclassic wallets
- Expose wallet transaction history
- Add wallet notifications for new transactions, tip changes, and async ticket purchases
- Server: Add a Websockets-based reverse tunnel for remote node RPC APIs
- Server: Enable CA certificates for simplified registration
- Implement new market making and arbitrage bots and GUI
- Add Binance and Binance US arbitrage support
- Refactor reputation system to accommodate low-lot-size markets and smaller bond sizes
- Show reputation data on dexsettings and trading views
- Enable password recovery using seed to avoid full wallet restorations
- Server: Enable public BTC RPC providers
- Normalize translation system to enable report generation for translators
- Server: Add database table for finalized candles for faster startup
- Total GUI refactor. Differentiated sections. Normalized forms. Drop sans-light font
- Add desktop notifications
- Implement model Tatanka Mesh for next-gen DEX
- Use binary searches for optimal lot count in various places for snappier response on low-lot-size markets
- Make transaction IDs and addresses in notifications into links
- Enable locating bonds when restoring from seed
- Switch to a custom BIP-39 based mnemonic seed that encodes the wallet birthday
- Server: Add endpoint for simple health check for is.decred.online/
- Enforce minimum rates, minimum lot sizes, and minimum bond sizes based on dust limits
- Add system installers for Windows, Mac, Linux
- Rebrand wallet from DCRDEX client (dexc) to Bison Wallet
- Allow displaying values in custom units in some places
- Add display for asset fiat rates, current network fees, and anticipated on-chain fees for trading

## Fixes

- Fix rescan panic for native BTC and LTC wallets
- Multiple UI fixes for incorrect conversions or wrong units displayed
- Fix panic for nil bookie via core.Book
- Disable Bitcoin Cash SPV wallet for lack of filter support
- Tons of other minor fixes

## Code Summary

696 files changed, 149904 insertions(+), 37372 deletions(-)

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
- Joe Gruffins (@JoeGruffins)
- Marton (@martonp)
- Peter Banik (@peterzen)
- Philemon Ukane (@ukane-philemon)
- @dev-warrior777
- Wisdom Arerosuoghene (@itswisdomagain)
- Migwi Ndung'u (@dmigwi)
- David Hill (@dhill)
- @norwnd
- @omahs
- @rubyisrust
