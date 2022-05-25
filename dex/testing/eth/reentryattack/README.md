## Reentry Contract Creation

Have `solc` and `abigen` installed on your system and run from this directory:

```sh
solc --combined-json abi,bin --optimize --overwrite ReentryAttack.sol -o .
abigen --combined-json combined.json --pkg reentryattack --out ./contract.go
rm combined.json
```

## Reentry Contract Usage

In order to see the effects of a reentry attack on a vulnerable contract,
VulnerableToReentryAttack.sol can be used.

NOTE: The contract interface is no longer compatible with the
dex/networks/eth/contracts/v0 API, so the following substitution of the
ETHSwapV0 bytecode with the vulnerable contract's code will not work without
updating the vulnerable "ETHSwap" contract.

```sh
solc --combined-json abi,bin --optimize --overwrite VulnerableToReentryAttack.sol -o .
abigen --combined-json combined.json --pkg v0 --out ../../../dex/networks/eth/contracts/v0/contract.go
rm combined.json
```

Then, the contract's hex in the newly created contract.go file must be used in
the harness, which deploys the contract used for testing, by replacing the hex
there and restarting the harness.

Finally, the harness tests in client/asset/eth contains a test that should fail
and show that indeed funds can be siphoned from the vulnerable contract.
