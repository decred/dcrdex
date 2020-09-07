# DCR Test Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The harness depends on [dcrd](https://github.com/decred/dcrd), [dcrwallet](https://github.com/decred/dcrwallet) and [dcrctl](https://github.com/decred/dcrd/tree/master/cmd/dcrctl) to run.

## Using

You must have `dcrd` and `dcrctl` in `PATH` to use the harness.

The harness script will create two connected simnet nodes and wallets, and
then mine some blocks and send some regular transactions. The result is a set of
wallets named **alpha** and **beta**, each with slightly different properties.

**Beta** is purely a mining node/wallet, and will have mostly coinbase
UTXOs to spend.

**Alpha** is a wallet with a range of UTXO values.

## Harness control scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside of this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha` and `./beta` are just `dcrctl` configured for their respective
wallets.
Try `./alpha getbalance`, for example.

`./reorg` will step through a script that causes the alpha or beta node to
undergo a 1-deep reorganization. The script takes the following optional
arguments: `./reorg {node} {depth}`, where:
- `node` is the node that should undergo a reorg, either "alpha" or "beta" (default: alpha)
- `depth` is the number of blocks that should be reorged into `node` (default: 1 for alpha, 3 for beta).
Currently, only 1 block (instead of `depth` blocks) can be purged from either
nodes during reorg, but the reorged node will always see `depth` new blocks
after the reorg.

`./quit` shuts down the nodes and closes the tmux session.

## Dev Stuff

If things aren't looking right, you may need to look at the node windows to
see errors. In tmux, you can navigate between windows by typing `Ctrl+b` and
then the window number. The window numbers are listed at the bottom
of the tmux window. `Ctrl+b` followed by the number `0`, for example, will
change to the alpha node window. Examining the node output to look for errors
is usually a good first debugging step.

An unfortunate issue that will pop up if you're fiddling with the script is
zombie dcrd processes preventing the harness nodes from binding to the
specified ports. You'll have to manually hunt down the zombie PIDs and `kill`
them if this happens.

Don't forget that there may be tests that rely on the existing script's
specifics to function correctly. Changes must be tested throughout dcrdex.
