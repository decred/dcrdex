# Ethereum Test Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The harness depends on [geth](https://github.com/ethereum/go-ethereum/tree/master/cmd/geth)
to run.

## Using

You must have `geth` in `PATH` to use the harness.

The harness script will create four connected private nodes. Two, alpha and
beta, have mining abilities and pre-funded addresses with syncmode set to
"fast". They are meant to be used with server functions. Two more, gamma and
delta, are "light" nodes without mining abilites and with addresses that have
been sent funds. They are intenended to be used with client functions.

## Harness control scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside of this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha`, `./beta`, `./gamma`, and `./delta` are just `geth` configured for
their respective data directories.

Try `./alpha attach`, for example. This will put you in an interactive console
with the alpha node.

`./quit` shuts down the nodes and closes the tmux session.

`./mine-alpha n` and `./mine-beta n` will mine n blocks on the respective node.

## Dev Stuff

If things aren't looking right, you may need to look at the node windows to
see errors. In tmux, you can navigate between windows by typing `Ctrl+b` and
then the window number. The window numbers are listed at the bottom
of the tmux window. `Ctrl+b` followed by the number `1`, for example, will
change to the alpha node window. Examining the node output to look for errors
is usually a good first debugging step.

If you encouter a problem, the harness can be killed from another terminal with
`tmux kill-session -t eth-harness`.
