# Bison Wallet v1.1.0

This is a major feature release with 400+ commits from 60+ contributors. It
introduces Monero support, cross-chain bridging, private atomic swaps, gasless
EVM redemptions, Politeia governance integration, and significantly expanded
market making capabilities. All users must update. The protocol is incompatible
between versions.

## Important Upgrade Notes

### Go 1.24 Required

The minimum Go version for building from source is now Go 1.24.

### Protocol Incompatibility

This release introduces per-match swap addresses, which change the trade
negotiation protocol. Old clients cannot trade on a v1.1.0 server and new
clients cannot trade on a v1.0.x server.

### EVM Contract Upgrade (Server Operators)

v1.0.6 used EVM contract version 0. v1.1.0 defaults to contract version 1.
The server's RPC client binds to a single contract version, so after upgrading
the server cannot verify coins from in-flight v0 swaps. To handle this safely,
operators should either:

1. Wait for all active EVM swaps to complete before upgrading, OR
2. Place an `evm-protocol-overrides.json` file in the server's working
   directory to keep v0 until remaining swaps drain:

   ```json
   {"eth": 0, "usdc.eth": 0, "usdt.eth": 0, "matic.eth": 0}
   ```

   Once no v0 swaps remain, remove the file and restart to use v1.

## New Features

### Monero Wallet

- Add Monero wallet support with CGO-based wallet integration (#3295, #3494)
- Automatic download of required Monero client tools (#3441)
- Reworked wallet creation flow and Opener interface (#3412, #3400)
- Monero atomic swap protocol with testnet support (#2936, #2942)
- Monero development harness (#2786)

### Cross-Chain Bridging

- Add Across Protocol support for bridging between Ethereum and Polygon PoS (#3331, #3216)
- Bridge POL from Polygon PoS to Ethereum (#3333)
- Track Polygon bridge deposit completion (#3326)
- Bridge UI for user-initiated cross-chain transfers (#3484)
- Use bridging in market making for cross-chain arbitrage (#3344)

### Private Atomic Swaps

- Implement private atomic swaps for BTC and DCR using adaptor signatures (#3290)
- Bundled libsecp256k1 C library for adaptor signatures and DLEQ proofs (#2810)

### Gasless EVM Redemptions

- Implement gasless redemptions for ETH/POL using EIP-712 relayed transactions (#3175, #3555)
- Update EVM v1 swap contract with gasless redeem support (#2038, #3488)

### OP Stack L2 Support

- Add Base network support (#3323)
- Add L1 fee support for OP Stack L2s (#3520)

### Politeia Governance Integration

- Integrate Politeia proposal voting into the wallet (#3428, #3461)
- Move Politeia voting lifecycle from Core to wallet (#3536)
- Add default Pi keys from network params to voting preferences (#3495)

### Market Making Enhancements

- Add Bitget exchange adapter (#3430)
- Add MEXC exchange adapter (#3367)
- Add Coinbase exchange adapter (#2809)
- Multi-hop arbitrage support and improvements (#3267, #3314)
- Epoch reporting for bot performance analysis (#2808, #3050)
- Display profit for each run and total profit on archives page (#3291)
- Live config and balance updates for running bots (#3081)
- Internal transfers between DEX and CEX balances (#2891)
- Market making server snapshots (#3508)
- Volume minimums and sanity checks for oracle rates (#2937)

### Mobile and Companion App

- Mobile companion app with Tor implementation (#2715)
- Route all outgoing connections through Tor (#3542)
- Require auth for companion app QR endpoint (#3541)

### Desktop Application

- Move desktop app from WebView to Electron (#3388)
- Remove old Mac build and reorganize packaging (#3534)

### Other New Features

- Add user onboarding game (#2921)
- Add wallet timeouts for unresponsive wallets (#3514)
- Add active trade limit (#3511)
- Show upgrade banner when newer version is available on GitHub (#3286)
- Download log files from UI (#3192)
- Add fiat value to order preview in trade form (#3448)
- Add slippage warnings for market orders (#3538)
- Add support for enabling/disabling a DEX account (#2946)
- Show partially filled status for cancelled orders (#3420)
- Add MATIC token on Ethereum network (#2988)
- Change native Polygon asset from MATIC to POL (#2957)
- Allow Firo to send to EXX addresses (#3119)
- Add DigiByte v8.22+ descriptor wallet support (#3376)

## Fixes and Improvements

### Trade and Swap Reliability

- Fix infinite refund loop (#3522)
- Babysit refund transactions for reliable completion (#3082)
- Add broadcast recovery for stuck transactions (#3519)
- Add SPV rescan stall detection and recovery (#3533)
- Recover stale mempool redeem/refund transactions (#3535)
- Keep trying init with the same secret on failures (#3269)
- Fall back to normal redeem when gasless relay fails (#574a4234)
- Retry block tx lookup in FindRedemption for multi-node setups (#3562)

### Server

- Add secret hash deduplication to prevent replay (#3556)
- Fix market buy funding validation (#3546)
- Harden comms rate limiting (#3527)
- Stash old fee rate momentarily after rate increase to avoid races (#3397)
- Add dedicated table for tracking reputation effects (#3259)
- Fix nil market panic on startup revocation (#02668c27)
- Update BTC rate providers (#3543)

### Market Making Fixes

- Fix unclickable rebalance settings for saved CEX bots (#3537)
- Fix CEX surplus allocation bug (#3313)
- Fix deposit balance check deadlock (#3066)
- Fix multi trade funding error (#3099)
- Fix withdrawal amount race (#3301)
- Binance: MATIC to POL update, fix orderbook sync (#3311, #2908)
- Cancel existing orders when starting bot (#2894)

### Wallet Fixes

- Fix incorrect USD fee error when sending USDC (#3458)
- Fix stack overflow in WalletTransaction (#3460)
- Fix bricked app after interrupt during registration (#3473)
- Fix Zcash multisplit (#2931)
- Fix Electrum Firo market making issues (#3202)
- Fix Polygon wallet connected/disconnected notifications (#3163)
- Fix ETH user action resolution handling (#3178)
- Fix negative swap confs for DCR (#3009)
- Fix connecting via Tor (#2934)
- Expire prepaid bonds correctly (#3220)
- Use user-set gas limit for ETH (#3469)
- Treat "already known" ETH tx responses as success (#3498)
- Fix ZEC refund fee calculation for ZIP-317 compliance (#3562)
- Fix wallet unlock affecting all networks for same ticker (#3562)

### UI Fixes

- Fix slippage estimation overflow (#3544)
- Fix reconfigure from UI (#3553)
- Fix rate display on fee bump notification (#3405)
- Fix market page daily high/low rate (#3014)
- Hide "~USD" when no fiat rate is available (#3471)
- Language fallback to prevent crash on unsupported locales (#3414)
- Updated Chinese and German translations (#3071, #3124)
- Show disabled state for individually disabled token wallets (#3562)

### WebSocket and Networking

- Fix WebSocket read loop blocking (#3501)
- Reset read deadline after message handling (#3507)
- Double the default websocket request timeout (#3076)

## Developer / Infrastructure

### Lexi Database

- Add new Lexi database package as a replacement for BoltDB (#3033)
- Add transaction support, index upgrades, simplified upgrade API (#3328, #3294, #3317)
- Migrate ETH transaction DB to Lexi (#3213)

### Tatanka Mesh (Next-Gen DEX)

- Add orderbook with map structure and push data (#3194, #3244, #3221)
- Add order compatibility and matching functions (#3165)
- Switch to AES for end-to-end encryption (#3204)
- Add fee rate estimate oracle (#2769)
- Add simnet harness and connect to mesh in core (#3339)

### Build and CI

- Go 1.24 and updated GitHub actions (#3347)
- Docker base images updated (#3341)
- Add more platforms for binary builds (#3073)
- Implement snap package (#2580)
- Migrate BTC harness to descriptor wallets (#3480)

### Dependencies

- Update dcrwallet to v5 (#3234)
- Update secp256k1 to 4.4.0 (#3198)
- Update btcwallet to v0.16.10 (#3206)
- Bump Decred deps to latest release versions (#3426)
- Switch bchwallet and go-bip39 to bisoncraft (#3479)

### Code Quality

- RPC server refactor (#3503)
- Replace `interface{}` with `any` throughout (#3509, #3504, #3515)
- Modernize to use `slices`, `maps`, built-in `max`/`min` (#3334, #3337)
- DRY up v1 token gas values across EVM chains (#3562)

### Simnet Testing

- Add per-match address simnet trade tests and README (#3562)

## Code Summary

958 files changed, 169,616 insertions(+), 40,875 deletions(-)

| Contributor | Commits |
| --- | --- |
| JoeGruffins | 98 |
| Marton | 78 |
| buck54321 | 78 |
| Philemon Ukane | 28 |
| dev-warrior777 | 19 |
| Justin Do | 15 |
| Jamie Holdstock | 11 |
| peterzen | 11 |
| karamble | 4 |
| David Hill | 2 |
| bochinchero | 2 |
| norwnd | 2 |
| slightsharp | 2 |
| Dave Collins | 1 |
| Jared Tate | 1 |
| Other one-time contributors | ~45 |
