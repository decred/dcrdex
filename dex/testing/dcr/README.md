# Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The harness depends on [dcrd](https://github.com/decred/dcrd), [dcrwallet](https://github.com/decred/dcrwallet) and [dcrctl](https://github.com/decred/dcrd/tree/master/cmd/dcrctl) to run.

## Harness structure

The harness creates set of folders to properly organize harness 
scripts and isolate test data. As a result the harness setup can
be deleted without any disruption to other local setup.

```
harness
├── alpha                 # alpha decred node
├── beta                  # beta decred node
├── w-alpha               # alpha decred wallet
├── w-beta                # beta decred wallet
└── harness-ctl           # misc. scripts
```

To run the the harness:

```sh
cd testing
./harness.sh
```
