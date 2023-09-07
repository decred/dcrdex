# Testing Firo ElectrumX Servers

## ElectrumX Test

This runs a test which accesses:

- Firo testnet electrumX server: __95.179.164.13:51002__
- Firo mainnet electrumX server: __electrumx.firo.org:50002__

and optionally:

- Firo simnet ElectrumX server running on a regtest chain server.

It tests the capabilities of the server.

## Dependencies

The regtest/simnet server test has a dependency on:

- The Firo simnet electrumx server: __electrumx.sh__
- The Firo simnet chain server: __harness.sh__

The simnet ElectrumX server (electrumx.sh) will connect over RPC to the
harness chain.

## Testing mainnet, testnet only

Run as a Go test from this directory:

__go test -v -count=1 ./...__

## Testing mainnet, testnet and regtest

Start __./harness.sh__ tmux script which starts 4 instances of the Firo regtest
harness chain.

Set environment variable __REGTEST="1"__

Run __./electrumx.sh__ which starts a simnet Firo ElectrumX server that accesses the
chain server harness over RPC.

Run as a Go test from this directory:

__go test -v -count=1 ./...__
