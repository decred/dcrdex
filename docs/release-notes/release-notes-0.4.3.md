# DCRDEX v0.4.3

Apr 29, 2022

For a high level introduction to DCRDEX, please read the [initial release's notes](https://github.com/decred/dcrdex/blob/master/docs/release-notes/release-notes-0.1.0.md).

This is a patch release.

## Highlights

### Features

- Dogecoin support via dogecoind, the reference full-node.  (<https://github.com/decred/dcrdex/commit/c0a9f5ef9e501e0c80620f9340f479ad82d3b460>, <https://github.com/decred/dcrdex/commit/30a561f3a57d50a659a75ec8a1826758ff613db7>, <https://github.com/decred/dcrdex/commit/0192b3b9080b767ef21907a935f03d7bc135af12>)
- Add a Tor listener to the server.  A ".onion" address may be specified with `--hiddenservice`. (<https://github.com/decred/dcrdex/commit/eb5f3cd976ab26ba88f6c6545c54c7d48af41f11>)
- Add an onion proxy option (`--onion`) to the client.  This proxy is used only for ".onion" server hosts, in contrast to the existing `--torproxy` option that is used for all hosts. (<https://github.com/decred/dcrdex/commit/5e3b7557a93f7f776e120859593f1bb8eca619e7>)
- Do not reject confirmed swap transactions on account of a low transaction fee (<https://github.com/decred/dcrdex/commit/57d3f0ec709da5fce13bfdb3d4902ad2088f314b>, <https://github.com/decred/dcrdex/commit/f7aa4612e8194dbe648830a1be96dc23c3b24b20>)
- Litecoin now requires segwit outputs, bumping its asset version to 1. (<https://github.com/decred/dcrdex/commit/a5b371b384ca4b17e0e1cedade34ca9af484aff5>)
- The taker now checks for a fresh swap fee rate before making their contract transaction.  This is helpful for the taker, who acts second, and thus potentially long after the match was made. (<https://github.com/decred/dcrdex/commit/b4c47feadbe7d703d1b6e3a54984e5a646da6aa2>, <https://github.com/decred/dcrdex/commit/4ffe9659436878d27ead0ec6070db6e7a5bb0293>, <https://github.com/decred/dcrdex/commit/666703d3e3d4db309f6e1b516c2a11650cd82113>)
- Additional server logging for init and redeem handling. (<https://github.com/decred/dcrdex/commit/9094faaff4c06837eefd92750d652c71c7bbba6a>)

### Fixes

- The server proactively revoke matches based on contract expiry. This includes where any known swap contracts are expired, or in the case of MakerSwapCast where the expected taker contract would be created with a lock time in the past. These scenarios are possible if a transaction takes an extremely long time to reach the required number of confirmations. (<https://github.com/decred/dcrdex/commit/b46796f7cc810b26b70c701ceb896451580dd85f>)
- The server rejects any provided contracts that are already expired. (<https://github.com/decred/dcrdex/commit/45f9b151c99edcfdbf4f8f7870139c63d7910c73>)
- The client double checks expiry of their and the counterparty's contracts before acting to prevent needlessly locking funds or redeeming very close to contract expiry, which is risky. (<https://github.com/decred/dcrdex/commit/79ccdd8c2fa5c1796b329cbc9f559b4f7b2e6958>)
- Various frontend fixes (<https://github.com/decred/dcrdex/commit/d0cd27c4f44cc7435d7b55e404a6558495ecde81>, <https://github.com/decred/dcrdex/commit/c1833e45e6216d262d672007a039d544edd52dfc>, <https://github.com/decred/dcrdex/commit/e5a9250e66f32c54062c7bc1c6b054da4aaf88bd>, <https://github.com/decred/dcrdex/commit/51509792e7232a2af5c5ffb3e9083b1b67155f6e>)
- The server will now continue to process init and redeem requests past the broadcast timeout, which is preferred since match revocation happens in a different goroutine. (<https://github.com/decred/dcrdex/commit/de839913ec32cf920757c38d3bf378e1c3752b5a>)
- The client begins signing an appropriate sighash in comms and asset packages rather than truncated data.  The server began recognizing such signatures in the last patch release.  (<https://github.com/decred/dcrdex/commit/6aad3107e4220b1786709132578ef9b059406a7a>, <https://github.com/decred/dcrdex/commit/f1f22fedb8bc98bc0c1225d11896989219ead629>)
- The html template builder now ignores extraneous files, only processing files with the .tmpl extension. (<https://github.com/decred/dcrdex/commit/6d9da296f3c5437d58f228e47744a13b29c30153>)
- Better error handling when checking counterparty swap transaction confirmations. (<https://github.com/decred/dcrdex/commit/4d0e726a4a5bc7315dbdb6c879dcfe23f708031c>)

### Developer

- Contract audits on Bitcion and Decred may be attempted with empty "txdata". (<https://github.com/decred/dcrdex/commit/8d1ab32208b8eff58bb00741cd73e3c0d48aa921>, <https://github.com/decred/dcrdex/commit/918e9bef83236ef90188b2c53549e8906bded3a3>)

## Important Notices

If upgrading from v0.2, read the [Upgrading section of the v0.4.0 release notes](https://github.com/decred/dcrdex/releases/tag/v0.4.0#upgrading) for important information.

## Code Summary

30 commits, 109 files changed, 2,725 insertions(+), 649 deletions(-)

[https://github.com/decred/dcrdex/compare/v0.4.2...v0.4.3](https://github.com/decred/dcrdex/compare/v0.4.2...v0.4.3)
