# Kurtosis Ethereum Test Harness

The harness is a collection of scripts that can start an eth enviroment with two
execution and two concensus nodes.

The code being run is located at [ethpandaops](github.com/ethpandaops/ethereum-package).
Look there for settings and other info.

## Dependencies

The harness depends on [geth](https://github.com/ethereum/go-ethereum/tree/master/cmd/geth)
to run. geth v1.14.12+ is recommended.

It also requires tmux, bc, node, docker, and [Kurtosis](https://github.com/kurtosis-tech/kurtosis).

## Using

You must have `geth` in `PATH` to use the harness. You must run `npm install` to
bring in the js dependencies needed. Docker and Kurtosis must be running before
running harness.sh.

The harness script will create two geth and lighthouse nodes that will run in
the background.

## Harness control scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside of this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha` is just `geth` configured for its data directory.

Try `./alpha attach`, for example. This will put you in an interactive console
with the alpha node.

`./quit` shuts down the nodes and closes the tmux session.

If you encouter a problem, the harness can be killed from another terminal with
`tmux kill-session -t eth-harness`. The Kurtosis enclave can be stopped with
`kurtosis enclave rm -f eth-simnet`. This may need to be done if it was not
shut down cleanly.
