# DCRDEX v0.1.3

Nov 24, 2020

This patch release includes a workaround for a bug in the Safari browser, an
important fix to a possible deadlock (client hang), and a minor fix to the
client's validation of the server's order matching.

NOTE: If you use the dexcctl program (an optional command line tool), you will
need to move any dexcctl.conf file from the ".dexc" folder to a new ".dexcctl"
folder.

Please read the [initial release (v0.1.0)
notes](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0) for
important information and instructions.

## Client (dexc)

### Fixes

- Eliminate a possible deadlock (hang) introduced in v0.1.2.
  ([65c9830](https://github.com/decred/dcrdex/commit/65c98309370779e747d676b2c29020610645284d))
- Fix the client's validation of the server's deterministic epoch matching
  result. This avoids an error message in the logs, but the bug was otherwise
  not a problem.
  ([10b4689](https://github.com/decred/dcrdex/commit/10b4689ae9a1118f94747951fd3ac444e490faab))

### Other

The location of the dexcctl.conf file is now in ~/.dexcctl instead of ~/.dexc
(or the corresponding "appdata" folders on Windows and macOS).
([16a0fb0](https://github.com/decred/dcrdex/commit/16a0fb003e2eca51fd2c29b938b0ec9bf681f7e5))

## Server (dcrdex)

There are no server changes.

## Code Summary

5 commits, 17 files changed, 188 insertions(+), and 174 deletions(-)

<https://github.com/decred/dcrdex/compare/v0.1.2...v0.1.3>

2 contributors

- Brian Stafford (@buck54321)
- Jonathan Chappelow (@chappjc)
