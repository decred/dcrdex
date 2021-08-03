## Reentry Contract Creation

Have `solc` and `abigen` installed on your system and run from this directory:

`abigen --sol ReentryAttack.sol --pkg reentryattack --out ./contract.go`


## Reentry Contract Usage

In order to see the effects of a reentry attack on a vulnerable contract,
VulnerableToReentryAttack.sol can be used.

First repace the current dex contract bindings with the vulnerable contract.

`abigen --sol VulnerableToReentryAttack.sol --pkg eth --out ../../../dex/networks/eth/contract.go`

Then, the contract's hex in the newly created contract.go file must be used in
the harness, which deploys the contract used for testing, by replacing the hex
there and restarting the harness.

Finally, the harness tests in client/asset/eth contains a test that should fail
and show that indeed funds can be siphoned from the vulnerable contract.
