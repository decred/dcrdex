## Intro

xmrswap performs adaptor signature swaps between dcr and xmr. There are four
tests; success, Alice bails before xmr init, refund, and Bob bails after xmr init.

## Requirements

dcrd, dcrwallet, dcrctl, monerod, monero-wallet-rpc, and monero-wallet-cli

## Simnet

Simnet requires that the dcr and xmr harnesses in /dcrdex/dex/testing be running.

## Testnet

Testnet requires a synced monerod running on --stagenet. It also requires three
monero-wallet-rpc running. Two of these must have wallets loaded, and
unlocked (--wallet-file). Alice must be funded. The third only needs to be
pointed to a directory (--wallet-dir).

It also requires two dcrd running on --testnet with bob being funded and unlocked.

The file example-config.json must be copied to config.json and correct locations
for your system filled in.

Testnet tests can be run with the --testnet flag.

Testnet tests take a long time to finish as they wait on monero funds being
available and dcr confirmations.
