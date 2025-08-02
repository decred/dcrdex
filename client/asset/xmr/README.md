# Monero XMR Wallet for Dex

## About

This is information for running a Monero XMR exchange wallet on Dex.

## Requirements

To RPC with an XMR wallet the `monero-wallet-rpc` tool from <https://github.com/monero-project/monero/releases> is required.

The latest CLI tools release (May 2025) is <https://github.com/monero-project/monero/releases/tag/v0.18.4.0>

Download, verify the binary hashes, and extract the `CLI tools` binaries. Enter the full path to the directory you put the CLI tools in the dialog shown on the "Create Monero Wallet" page.

### Local Monero chain

It is more secure to use a local copy of the blockchain. In this case it is also expected that the full chain has been downloaded to the local machine. It should also be running when you create or use the wallet.

### Remote chain

A remote `monerod` chain server may also be used although this is considered much less secure by the Monero community. Find an HTTP server at <https://monero.fail/> - this should be identified when creating the Monero wallet with any server password required (usually none)
