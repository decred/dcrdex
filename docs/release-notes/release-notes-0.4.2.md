# DCRDEX v0.4.2

Mar 17, 2022

For a high level introduction to DCRDEX, please read the [initial release's notes](https://github.com/decred/dcrdex/blob/master/docs/release-notes/release-notes-0.1.0.md).

This is a patch release.

## Highlights

### Features

- The client database is compacted on shutdown if there are sufficient free pages. (<https://github.com/decred/dcrdex/commit/0cb43ccab79459c50a69b6b627d239cee00d658b>)

### Fixes

- Decred: ensure that the Refund method signals correctly when the targeted contract is already spent.  This is necessary for the taker to begin locating the counterparty/maker redemption. (<https://github.com/decred/dcrdex/commit/b47907cc19ed62f41b9af86d1d9b991a360e65b5>)
- Begin a counterparty redemption search when a refund attempt fails in certain situations. (<https://github.com/decred/dcrdex/commit/d1ebfb193d70a25db29f8d1d6eabf9151b000ca5>)
- Various frontend fixes (<https://github.com/decred/dcrdex/commit/ccefcc975885bb0b0e50481ea8c61cfba630ed90>, <https://github.com/decred/dcrdex/commit/8d4746bae41be77c88f6064032e2c4c9e5d77849>)
- Avoid leaving Decred UTXOs locked in certain cases. (<https://github.com/decred/dcrdex/commit/d80b30e0022728280e491c754e9333d8263099f7>)
- Fix a panic when no market fee exists (<https://github.com/decred/dcrdex/commit/3d56fc8470a45cea638d7d6f054f19e91b851b15>)
- Recognize signatures of full-length message payloads (<https://github.com/decred/dcrdex/commit/2e93f761a9462caa937c97d23bf412bf4fa3617c>, <https://github.com/decred/dcrdex/commit/a60275a159f670757d78d305fe840b72ffc05419>)

### Developer

- Add a client database backup function, `(*Core).BackupDB`. (<https://github.com/decred/dcrdex/commit/5a34c2683bd41f1dcef152ee6210d18eb4f4fa38>)
- Document the `dex/msgjson.RegisterResult.ClientPubKey` intent (<https://github.com/decred/dcrdex/commit/a17e57798bf984689a85d4fb196dcf307a353495>)

## Important Notices

If upgrading from v0.2, read the [Upgrading section of the v0.4.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.4.0#upgrading) for important information.

## Code Summary

14 commits, 30 files changed, 339 insertions(+), 113 deletions(-)

[https://github.com/decred/dcrdex/compare/v0.4.1...v0.4.2](https://github.com/decred/dcrdex/compare/v0.4.1...v0.4.2)
