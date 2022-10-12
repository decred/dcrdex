# DCRDEX v0.5.4

Oct 10, 2022

**IMPORTANT:** This patch release includes a critical fix for all Bitcoin wallets.

Please read the [v0.5.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.5.0) for a full list of changes since v0.4.

For important information and an introduction to DCRDEX, read the [initial release (v0.1.0) notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0).

Either download the file for your distribution below, or wait for the signed distributions to be created at <https://github.com/decred/release/tags>.

## Fixes

- Blocks with taproot transactions with large witness items no longer fail to deserialize. This was caused by an upstream bug in the `btcsuite/btcd/wire` package.  See <https://github.com/btcsuite/btcd/pull/1896> (<https://github.com/decred/dcrdex/commit/ac83229523595d65cdd6461f1945ad776ee1fb8e>)

## Server

- Fix candles with a large negative rate. (<https://github.com/decred/dcrdex/commit/3d033f05fd2915c39d6c90d9dc0647da7aa6eee3>)

## Code Summary

3 commits, 13 files changed, 171 insertions(+), and 57 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.5.3...v0.5.4>

2 contributors

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
