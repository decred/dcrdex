## Gas Estimator Utility

`getgas` is a program for getting gas estimates for DEX critical operations
on Ethereum. Use `getgas` to generate gas tables (`dexeth.Gases`) when adding a
new token or a new contract version.

If this is a new asset, you must populate either `dexeth.VersionedGases` or
`dexeth.Tokens` with generous estimates before running `getgas`.

### Use
- Create a credentials file. `getgas` will look in `~/ethtest/getgas-credentials.json`. You can override that location with the `--creds` CLI argument. The credentials file should have the JSON format in the example below. The seed can be anything.

**example credentials file**
```
{
    "seed": "<32-bytes hex>",
    "providers": {
        "simnet": "http://localhost:38556",
        "testnet": "https://goerli.infura.io/v3/<API key>",
        "mainnet": "https://mainnet.infura.io/v3/<API key>"
    }
}
```

- Decide the maximum number of swaps you want in the largest initiation transaction, `--n`. Minimum is 2. `getgas` will check initiations with from 1 up to `n` swaps. There is a balance between cost and precision. Using more than 2 generates an average over `n - 1` intiations to calculate the cost of additional swaps (`Gases.SwapAdd`) and redeems (`Gases.RedeemAdd`).

- A network **MUST** be specified with the `--mainnet`, `--testnet`, or `--simnet` flag.

- Use `--help` to see additional options.

**example usage**
```
go build -tags lgpl
./getgas --simnet --n 3 --token dextt.eth
```
