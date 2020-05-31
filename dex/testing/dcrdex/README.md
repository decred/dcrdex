# DCRDEX Test Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The dcrdex harness depends on 2 other harnesses: [DCR Test Harness](dex/testing/dcr/README.md) and [BTC Simnet Test Harness](dex/testing/btc/README.md) to run.

## Using

The [DCR Test Harness](dex/testing/dcr/README.md) and [BTC Simnet Test Harness](dex/testing/btc/README.md) must be running to use the dcrdex harness.

The dcrdex harness script will create a markets.json file referencing dcr and btc
node config files created by the respective node harnesses; and start a dcrdex
instance listening at `127.0.0.1:17273`.

The rpc cert for the dcrdex instance will be created in `~/dextest/dcrdex/rpc.cert`.

## Harness control

To quit the harness, use ctrl+c to stop the running dcrdex instance and then 
`tmux kill-session` to exit the harness.

## Dev Stuff

If things aren't looking right, you may need to look at the harness tmux window
to see errors. Examining the dcrdex logs to look for errors is usually a good
first debugging step.

An unfortunate issue that will pop up if you're fiddling with the script is
zombie dcrdex processes preventing the harness nodes from binding to the
specified ports. You'll have to manually hunt down the zombie PIDs and `kill`
them if this happens.

Don't forget that there may be tests that rely on the existing script's
specifics to function correctly. Changes must be tested throughout dcrdex.
