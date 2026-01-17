# BTC Simnet Test Harness
The harness script will create two connected regnet nodes with four wallets, and
then mine some blocks and send some BTC around. The result is a set of wallets
named **alpha**, **beta**, **gamma**, and **delta**, each with slightly different
properties.

**Beta** is purely a mining node/wallet and has never sent a transaction. Beta
does have some mature coinbase transaction outputs to spend.

**Alpha** is also a mining node/wallet. Unlike beta, alpha has sent some BTC
so has some change outputs that are not coinbase and have varying number of
confirmations.

**Gamma** is another wallet on the alpha node. Gamma has no coinbase-spending
outputs, but has a number of UTXOs of varying size and confirmation count.

**Delta** is another wallet on the beta node, similar to gamma.

**The wallet password is "abc"** for encrypted wallets.

## Dependencies

You must have [bitcoind and bitcoin-cli](https://github.com/bitcoin/bitcoin/releases)
in `PATH` to use the harness. Bitcoin Core v26.0.0+ is recommended. v30+ is fully supported.

It also requires tmux.

## Harness control scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside of this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha`, `./beta`, `./gamma`, and `./delta` are just `bitcoin-cli` configured
for their respective wallets.
Try `./gamma getbalance`, for example.

`./reorg` will step through a script that causes the alpha node to undergo a
1-deep reorganization.

`./quit` shuts down the nodes and closes the tmux session.

## Regenerating Descriptor Files

If you need to regenerate the `desc-*.json` files (e.g., after changing wallet
configuration), use the `gen-descriptors.sh` script:

```bash
# Start a Bitcoin Core node first, then run:
./gen-descriptors.sh [RPC_PORT]
```

This will create new descriptor files and output the corresponding addresses
to update in `harness.sh`.

## Dev Stuff

If things aren't looking right, you may need to look at the node windows to
see errors. In tmux, you can navigate between windows by typing `Ctrl+b` and
then the window number. The window numbers are listed at the bottom
of the tmux window. `Ctrl+b` followed by the number `0`, for example, will
change to the alpha node window. Examining the node output to look for errors
is usually a good first debugging step.

An unfortunate issue that will pop up if you're fiddling with the script is
zombie bitcoind processes preventing the harness nodes from binding to the
specified ports. You'll have to manually hunt down the zombie PIDs and `kill`
them if this happens.

Don't forget that there may be tests that rely on the existing script's
specifics to function correctly. Changes must be tested throughout dcrdex.
