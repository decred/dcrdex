# Simnet Trade Tests

End-to-end integration tests that exercise real trades on simnet using asset
harnesses. Two simulated DEX clients place orders, match, and settle (or fail
to settle) across a variety of scenarios.

## Prerequisites

Start the asset harnesses for the pair you want to test, then start the dcrdex
harness. For example, to test DCR-BTC:

```sh
cd ~/dextest
./dcr/harness.sh
./btc/harness.sh
./dcrdex/harness.sh
```

The asset harnesses run background miners (`watch -n 15 ./mine-alpha 1`) by
default. The trade tests mine their own blocks at specific points, so
background mining can interfere with confirmation counting. Disable it by
exporting `NOMINER=1` before starting each asset harness:

```sh
export NOMINER=1
./dcr/harness.sh
./btc/harness.sh
```

## Building

The tests require the `harness` build tag:

```sh
go build -tags harness ./client/cmd/simnet-trade-tests/
```

## Running

The easiest way is through the `run` script, which rebuilds with shortened swap
lock times (3m taker, 6m maker) and selects the right node flags for each pair:

```sh
./run dcrbtc
```

Run `./run help` to see all pre-configured pairs and available flags.

### Pre-configured pairs

| Command            | Market          | Notes                         |
| ------------------ | --------------- | ----------------------------- |
| `dcrbtc`           | DCR-BTC         | RPC wallets                   |
| `dcrspvbtc`        | DCR-BTC         | DCR SPV, BTC RPC              |
| `dcrbtcspv`        | DCR-BTC         | DCR RPC, BTC SPV              |
| `dcrbtcelectrum`   | DCR-BTC         | DCR RPC, BTC Electrum         |
| `bchdcr`           | BCH-DCR         | RPC wallets                   |
| `bchspvdcr`        | BCH-DCR         | BCH SPV, DCR RPC              |
| `ltcdcr`           | LTC-DCR         | RPC wallets                   |
| `ltcspvdcr`        | LTC-DCR         | LTC SPV, DCR RPC              |
| `ltcelectrumdcr`   | LTC-DCR         | LTC Electrum, DCR RPC         |
| `ltcelectrumeth`   | LTC-ETH         | LTC Electrum, ETH native      |
| `dcrdash`          | DCR-DASH        | RPC wallets                   |
| `dcrdoge`          | DCR-DOGE        | RPC wallets                   |
| `dcrdgb`           | DCR-DGB         | RPC wallets                   |
| `dcreth`           | DCR-ETH         | DCR RPC, ETH native           |
| `dcrfiro`          | DCR-FIRO        | RPC wallets                   |
| `dcrfiroelectrum`  | DCR-FIRO        | DCR RPC, FIRO Electrum        |
| `firoelectrumdoge` | FIRO-DOGE       | FIRO Electrum, DOGE RPC       |
| `zecbtc`           | ZEC-BTC         | RPC wallets                   |
| `dcrusdc`          | DCR-USDC.ETH    | ERC-20 token on Ethereum      |
| `polygondcr`       | POLYGON-DCR     | Polygon RPC, DCR RPC          |
| `dcrusdcpolygon`   | DCR-USDC.POLYGON| ERC-20 token on Polygon       |
| `zclbtc`           | ZCL-BTC         | RPC wallets                   |

### Selecting tests

By default only the `success` test runs. Use `-t` to pick specific tests, or
`--all` to run everything:

```sh
./run dcrbtc -t success -t nomakerswap
./run dcrbtc --all
./run dcrbtc --all --except=orderstatus
```

Available tests:

| Test             | Description                                              |
| ---------------- | -------------------------------------------------------- |
| `success`        | Complete trade with swaps and redeems on both sides      |
| `nomakerswap`    | Maker fails to send swap tx                              |
| `notakerswap`    | Taker fails to swap; refund after locktime               |
| `nomakerredeem`  | Maker fails to redeem; both sides refund                 |
| `makerghost`     | Maker disappears after taker redeems; auto-redeem tested |
| `orderstatus`    | Order status recovery after client disconnection         |
| `resendpending`  | Resending of pending trade requests                      |
| `notakeraddr`    | Taker omits per-match swap address; taker penalized      |
| `nomakeraddr`    | Maker omits per-match swap address; maker penalized      |
| `notakerredeem`  | Taker fails to redeem after maker redeems; taker penalty |
| `missedcpaddr`   | Counterparty address re-delivered after reconnect        |

Each test runs twice by default, swapping which client is maker and which is
taker. Pass `--runonce` to only run each test once.

### Other useful flags

```
--debug          Log at debug level
--trace          Log at trace level
--color <color>  Log color: green (default), white, blue, yellow, magenta, red
--regasset <sym> Asset for bond registration (default: base asset)
```

## Troubleshooting

- **"coin locked"** - The DEX hasn't revoked a previously failed match. Wait a
  few seconds and retry, or clear the dcrdex db and restart the dcrdex harness.
- **"not enough to cover requested funds"** - Use the asset harness to send
  more funds to the affected wallet.
- **Fee payment confirmation issues** - Restart the dcr harness then the dcrdex
  harness (stop dcrdex first).
