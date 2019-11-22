# BTC Simnet Test Harness

You must have `bitcoind` and `bitcoin-cli` in `PATH` to use the harness.

The harness script will create three connected regnet nodes and wallets, and
then mine some blocks and send some BTC around. The result is a set of wallets
named **alpha**, **beta**, and **gamma**, each with slightly different
properties.

**Beta** is purely a mining node/wallet and has never sent a transaction. Beta
does have some mature coinbase transaction outputs to spend.

**Alpha** is also a mining node/wallet. Unlike beta, alpha has sent some BTC
so has some change outputs that are not coinbase and have varying number of
confirmations.

**Gamma** is another wallet on the alpha node. Gamma is encrypted, so requires
unlocking for sensitive operations. Gamma has no coinbase-spending outputs,
but has a number of UTXOs of varying size and confirmation count.
**The gamma wallet password is "abc"**.

## Harness control scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside of this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha`, `./beta`, and `./gamma` are just `bitcoin-cli` configured for their
respective wallets.
Try `./gamma getbalance`, for example.

`./reorg` will step through a script that causes the alpha node to undergo a
1-deep reorganization.

`./quit` shuts down the nodes and closes the tmux session.

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
