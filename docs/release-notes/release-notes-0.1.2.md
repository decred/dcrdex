# DCRDEX v0.1.2

Nov 12, 2020

This patch release improves handling of slow contract audits, fixes a bug on
32-bit systems, automatically unlocks wallets to avoid swap failures if the
wallet unexpectedly became locked, and improves the display of canceled orders.
There are also a few usability improvements for developers.

Please read the [initial release (v0.1.0) notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0)
for important information and instructions.

## Client (dexc)

### Features and Improvements

#### User-facing

- When already logged in, automatically attempt to unlock wallets as needed for
  trades. This helps prevent users from breaking their swaps by accidentally
  locking their wallets.
  ([de40913](https://github.com/decred/dcrdex/commit/de409134c37270145dc7094e89d6ef9d8e2d1f74))
- Display cancel order matches differently from trade matches.
  ([b013581](https://github.com/decred/dcrdex/commit/b01358159eeb7cbe5024f58f035306e98bb0a2f8))

#### Developer

- Create a `Ready` method so consumer packages know when the client core is done
  starting up.
  ([c3d9e80](https://github.com/decred/dcrdex/commit/c3d9e80602e9cad8cc7ebc80e2d7e96a2257d3ab))
- Increase notification channel capacity to prevent dropped notifications when
  there are many simultaneous events.
  ([2de62a3](https://github.com/decred/dcrdex/commit/2de62a378d8b964c6ff2a485ca907b0b1c2b7ac4))
- Remove the obsolete (and incomplete) terminal UI.
  ([75ff8d0](https://github.com/decred/dcrdex/commit/75ff8d09f6f5f898dfd23ebbacbb7a3f1d2e473f))

### Fixes

- Workaround for 64-bit atomic variable access on 32-bit platforms.
  ([3abaf43](https://github.com/decred/dcrdex/commit/3abaf434a3da3603916969f7af4b0c487b76b149))
- Prevent contract auditing from blocking incoming messages.  Continue to search
  for counterparty contracts until it succeeds or the match is revoked, and log
  a warning if the audit is taking a long time.
  ([23f2f36](https://github.com/decred/dcrdex/commit/23f2f362486141419d4a321674229f3716fd4faf))

## Server (dcrdex)

There are no server changes.

## Code Summary

9 commits, 44 files changed, 621 insertions(+), and 2,652 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.1.1...v0.1.2>

3 contributors

- Brian Stafford (@buck54321)
- David Hill (@dajohi)
- Jonathan Chappelow (@chappjc)
