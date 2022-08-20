# Contribution Guide

We welcome your contributions to dcrdex.

Development is coordinated via [github issues](/../issues) and the
[Matrix](https://matrix.org/)
[DEX Development room](https://matrix.to/#/!EzTSRQITaqHuFBDFhM:decred.org?via=decred.org&via=matrix.org&via=zettaport.com).
You can access the room at [chat.decred.org](https://chat.decred.org),
[riot.im](https://riot.im), or with any other Matrix client.

## Contributing code

1. Fork the repo.
2. Create a branch for your work (`git checkout -b cool-stuff`).
3. Code something great.
4. Commit and push to your forked repo.
5. Create a [pull request](https://github.com/decred/dcrdex/compare).

## Code Style (Go)

- Wrap comments after 80 columns, treating tabs as being 4 spaces wide. Trailing code comments are exempt.
- Try to keep code wrapped below 100 columns, but this is more of a guideline than a rule.
- Document all exported symbols. Use full sentences and proper punctuation.
- Justify the creation of exported types/variables/methods/functions. If it can be exported, it probably should be.
- Do not use `make` to allocate 0-capacity slices (e.g. `make([]int, 0)`) unless you have a good reason. It is safe to append to a `nil` slice.
- When declaring a variable with its zero value, just use `var` and not `:=`. For example, `var x int64` instead of `x := int64(0)`.
- When defining `error` strings, avoid beginning with a capital letter unless it refers to an exported type or proper noun, and avoid ending with punctuation. There are exceptions, so this is not checked by a linter.

## Get paid

If you are already a Decred contractor, you can charge for your development work
on **dcrdex**. If you are not a Decred contractor but would like to be, you
should first
[familiarize yourself with the onboarding process](https://docs.decred.org/contributing/overview/).
**dcrdex** is a great place to get started, but you would be expected to take on
only smaller jobs until you are comfortable navigating the code and have shown
the ability to communicate and follow through.
