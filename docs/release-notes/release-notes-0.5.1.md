# DCRDEX v0.5.1

Aug 29, 2022

This patch release includes a required fix for Windows users.

Please read the [v0.5.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.5.0) for a full list of changes since v0.4.

For important information and an introduction to DCRDEX, read the [initial release (v0.1.0) notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0).

## Client (dexc)

### Fixes

- The embedded site resources for the user interface were not loading correctly on Windows systems.  This uses the correct path construction for embedded file systems. (<https://github.com/decred/dcrdex/commit/078b129de48d07c7aa40ccaa6d50fbd6a14b3c7b>)
- When using `--no-embed-site`, a developer setting, it no longer takes two page refreshes to see the effect of a changed on-disk html template. (<https://github.com/decred/dcrdex/commit/2e625e04307a38b98273ff88e52e0447344fae19>)
- Fix being unable to change a DEX host name if the DEX server is not connected. (<https://github.com/decred/dcrdex/commit/5eb68c5d488511acacd0aed08530fcecf08f7b74>)

## Server (dcrdex)

There are no server changes.

## Code Summary

3 commits, 3 files changed, 34 insertions(+), and 14 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.5.0...v0.5.1>

2 contributors

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
