
# DCRDEX v1.0.2

This release contains several important fixes and improvements.  All users are advised to update.

Please read the [v1.0.0 release notes](https://github.com/decred/dcrdex/releases/tag/v1.0.0) for a full list of changes since v0.6.

## Fixes and Improvements

- Update dcrwallet dependency to include some bug fixes ([#3068](https://github.com/decred/dcrdex/pull/3068))
- Fix formatting of Bitcoin DB filepath names for Windows compatibility ([#3056](https://github.com/decred/dcrdex/pull/3056))
- Fix bug in form closing code on markets view ([#3053](https://github.com/decred/dcrdex/pull/3053))
- Fix bugs related to Binance connection and balance retrieval ([#3060](https://github.com/decred/dcrdex/pull/3060))
- Fix Binance balance check deadlock ([#3066](https://github.com/decred/dcrdex/pull/3066))
- Add Decred mix split transaction identification. ([#3061](https://github.com/decred/dcrdex/pull/3061))
- Improve performance when deleting inactive orders/matches ([#3059](https://github.com/decred/dcrdex/pull/3059))

## Code Summary

32 files changed, 516 insertions(+), 242 deletions(-)

- Marton (@martonp)
- Brian Stafford (@buck54321)
- Jamie Holdstock (@jholdstock)
- JoeGruffins (@JoeGruffins)
- @dev-warrior777
