## Gas Estimator Utility

`getgas` is a program for getting gas estimates for DEX-critical operations
on Ethereum. Use `getgas` to generate gas tables (`dexeth.Gases`) when adding a
new token or a new contract version.

If this is a new asset, you must populate either `dexeth.VersionedGases` or
`dexeth.Tokens` with generous estimates before running `getgas`.

### Use
- Create a credentials file. `getgas` will look in `~/ethtest/getgas-credentials.json` for Ethereum, and `~/ethtest/getgas-credentials_polygon.json` for Polygon. You can override that location with the `--creds` CLI argument. The credentials file should have the JSON format in the example below. The seed can be anything.

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
- Select the blockchain with `--chain`. The default is `--chain eth`, but `--chain polygon` can be selected as well.

- Use the `--readcreds` utility to check the validity of the credentials file and to print the address. e.g. `./getgas --readcreds --mainnet`. 

- Decide the maximum number of swaps you want in the largest initiation transaction, `--n`. Minimum is 2. `getgas` will check initiations with from 1 up to `n` swaps. There is a balance between cost and precision. Using more than 2 generates an average over `n - 1` intiations to calculate the cost of additional swaps (`Gases.SwapAdd`) and redeems (`Gases.RedeemAdd`).

- Use the `--fundingrequired` utility to see what funding is required for the estimate wallet. e.g. `./getgas --fundingrequired --n 3 --token usdc.eth --testnet`.

- If you run `getgas` with insufficient or zero ETH and/or token balance on the seed, no transactions will be sent and you'll get a message indicating the amount of funding needed to run.

- A network **MUST** be specified with the `--mainnet`, `--testnet`, or `--simnet` flag.

- You can return Ethereum from the estimate wallet to a specified address with the `--return` utility, e.g. `./getgas --return d12ab7cf72ccf1f3882ec99ddc53cd415635c3be --mainnet`

-- **DO NOT** send any more token balance that what is suggested by the `--fundingrequired`. Remaining token balance is not returned.

- Use `--help` to see additional options.

- **If running on mainnet, real funds will be used** for fees based on the current network base fee and tip rate. All swaps sent are just 1 gwei (atom), with fees additional. If testing a token, 1 atom will be transferred to a [random address](https://ethereum.stackexchange.com/a/3298/83118) and will be unrecoverable.

**example usage**
```
go build -tags lgpl
./getgas --simnet --n 3 --token dextt.eth
```
