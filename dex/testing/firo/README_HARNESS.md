# Firo Chain Server Test Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The harness depends on [firod] and [firo-cli] to run.
Go to <https://github.com/firoorg/firo/releases> for binaries or source.

## Using

You must have `firod` and `firo-cli` in `PATH` to use the harness.

The harness script will create four connected regtest nodes **alpha**, **beta**,
**gamma**, **delta**. Each node will have one empty but encrypted wallet. The script will mine some blocks and send some regular transactions.

This simnet harness sets up 4 Firo nodes and a set of harness controls
Each node has a prepared, encrypted, empty wallet

```text
alpha/
├── alpha.conf
└── regtest
    ├── wallet.dat

beta/
├── beta.conf
└── regtest
    ├── wallet.dat

gamma/
├── gamma.conf
└── regtest
    ├── wallet.dat

delta/
├── delta.conf
└── regtest
    ├── wallet.dat

└── harness-ctl
    ├── alpha
    ├── beta
    ├── connect-alpha
    ├── delta
    ├── gamma
    ├── mine-alpha
    ├── mine-beta
    ├── quit
    ├── reorg
    ├── start-wallet
    └── stop-wallet
```

**alpha** is purely a mining node/wallet, and will have mostly coinbase
UTXOs to spend.

**beta**, **gamma**, **delta** will all connect to **alpha** as peers.

## Harness Control Scripts

The `./harness.sh` script will drop you into a tmux window in a directory
called `harness-ctl`. Inside this directory are a number of scripts to
allow you to perform RPC calls against each wallet.

`./alpha` `./beta` `./gamma` `./delta` are just `firo-cli` configured for their
respective node-wallets.

`./alpha getbalance`, for example.

### Other Examples 

`./reorg` will step through a script that causes the alpha node to undergo a reorg.

`./quit` shuts down the nodes and closes the tmux session.

## Background Miner

By default the harness also sets up a background miner which mines on the alpha
node every 15s. This can upset some test logic so it can be disabled by
setting environment var NOMINER="1"

## Dev Stuff

If things aren't looking right, you may need to look at the node windows to
see errors. In tmux, you can navigate between windows by typing `Ctrl+b` and
then the window number. The window numbers are listed at the bottom
of the tmux window. `Ctrl+b` followed by the number `0`, for example, will
change to the alpha node window. Examining the node output to look for errors
is usually a good first debugging step.

An unfortunate issue that will pop up if you're fiddling with the script is
zombie firod processes preventing the harness nodes from binding to the
specified ports. You'll have to manually hunt down the zombie PIDs and `kill`
them if this happens.

### Zombie Killer

(debian)

```bash
$   killall -9 firod                    # kill running daemons
$   tmux kill-session -t firo-harness   # kill firo-harness session
```
