# Testing Firo ElectrumX Servers

## ElectrumX Test

This runs a test which accesses:
- Firo testnet electrumX server: __95.179.164.13:51002__
- Firo mainnet electrumX server: __electrumx.firo.org:50002__

Optionally:
- Firo simnet ElectrumX server running a dex testing harness chain

It tests the capabilities of the server

## Dependencies

The regtest/simnet server test has a dependency on:
- The Firo harness chain: __harness.sh__
- The Firo simnet electrumx server: __electrumx.sh__

The simnet ElectrumX server (electrumx.sh) should connect over RPC to the 
harness chain.

## Testing mainnet, testnet only

Run as a Go test:

__go test -v -count=1 ./...__

## Testing mainnet, testnet and additionally with regtest

Start **./harness.sh** tmux script which starts 4 instances of the Firo regtest 
harness chain

Set environment REGTEST="1"

Run **./electrumx.sh** which starts a Firo ElectrumX server that accesses the 
harness chain over RPC

Run as a Go test:

__go test -v -count=1 ./...__



