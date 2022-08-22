# Simnet Testing

- [Simnet Harnesses](#simnet-harnesses)
  - [Start the DCR and BTC simnet harnesses](#start-the-dcr-and-btc-simnet-harnesses)
  - [Automatic mining](#automatic-mining)
- [Server (`dcrdex`) Setup](#server-dcrdex-setup)
  - [Configure dcrdex markets for simnet](#configure-dcrdex-markets-for-simnet)
  - [Prepare the PostgreSQL DB](#prepare-the-postgresql-db)
  - [Start dcrdex](#start-dcrdex)
- [Client (`dexc`) Setup](#client-dexc-setup)
  - [The wallet config files for dexc](#the-wallet-config-files-for-dexc)
  - [Start `dexc`](#start-dexc)
  - [Setup the client via the browser interface](#setup-the-client-via-the-browser-interface)
  - [Setup the client via dexcctl](#setup-the-client-via-dexcctl)

## Simnet Harnesses

### Start the DCR and BTC simnet harnesses

The harnesses are used by both server (dcrdex) and client (dexc).

From the source repository root, run each command in a separate terminal:

DCR harness terminal

```sh
./dex/testing/dcr/harness.sh
```

BTC harness terminal

```sh
./dex/testing/btc/harness.sh
```

When a harness is ready, it will be at a command prompt in a tmux session (green bar at the bottom).

![tmux harness](https://user-images.githubusercontent.com/9373513/79356467-9f740900-7f04-11ea-80d7-6473eae24411.png)

When done with a harness, use `./quit` to shutdown all the processes and tmux sessions started by the harness.

### Automatic mining

For convenience, the harnesses may continually mine blocks with the `watch` command. To generate a new Bitcoin block every 5 seconds:

```sh
~  dextest  btc  harness-ctl  $  watch -n 25 ./mine-alpha 1
```

Decred is similar, but under the `dcr` folder:

```sh
~  dextest  dcr  harness-ctl  $  watch -n 30 ./mine-alpha 1
```

**WARNING**: Decred's harness can't mine indefinitely so stop the watch when you have completed your current test.  The harness config has a ticket buyer, but not enough tickets are purchased on a long enough timeline because automatic buying is not allowed before a price change.  This needs tweaking.

## Server (`dcrdex`) Setup

### Harness option

There is a dcrdex harness that will set up everything for you, although with a very short contract lock time set of 1 min maker and 30 sec taker.

```sh
~  [repo root]  dex  testing  dcrdex  $  ./harness.sh
```

To setup the dcrdex server manually, and with the regular contract lock times, follow the steps in the following subsections.

### Configure dcrdex markets for simnet

NOTE: If using the harness option described in the previous section, this and other server setup is not required.

`dcrdex` will start running the markets configured in `~/.dcrdex/markets.json`:

```json
{
    "markets": [
        {
            "base": "DCR_simnet",
            "quote": "BTC_simnet",
            "epochDuration": 6000,
            "marketBuyBuffer": 1.2,
            "lotSize": 100000000,
            "rateStep": 100
        }
    ],
    "assets": {
        "DCR_simnet": {
            "bip44symbol": "dcr",
            "network": "simnet",
            "maxFeeRate": 26,
            "swapConf": 1,
            "configPath": "/home/dcrd/.dcrd/simnet.conf",
            "regConfs": 1,
            "regFee": 100000000,
            "regXPub": "spubVWKGn9TGzyo7M4b5xubB5UV4joZ5HBMNBmMyGvYEaoZMkSxVG4opckpmQ26E85iHg8KQxrSVTdex56biddqtXBerG9xMN8Dvb3eNQVFFwpE"
        },
        "BTC_simnet": {
            "bip44symbol": "btc",
            "network": "simnet",
            "maxFeeRate": 122,
            "swapConf": 1,
            "configPath": "/home/bitcoin/.bitcoin/simnet.conf",
            "regConfs": 2,
            "regFee": 20000000,
            "regXPub": "vpub5SLqN2bLY4WeZJ9SmNJHsyzqVKreTXD4ZnPC22MugDNcjhKX5xNX9QiQWcE4SSRzVWyHWUihpKRT7hckDGNzVc69wSX2JPcfGeNiT5c2XZy"
        }
    }
}
```

For the DCR config file, this could be `~/dextest/btc/alpha/alpha.conf` to use the Decred simnet harness' setup.

For the BTC config file, this could be `~/dextest/btc/alpha/alpha.conf` to use the Bitcoin simnet harness' setup.

### Prepare the PostgreSQL DB

If necessary, first create the `dcrdex` PostgreSQL user with `CREATE USER dcrdex;`.

Wipe any old PostgreSQL DB, and create a new simnet database:

```sql
DROP DATABASE dcrdex_simnet;
CREATE DATABASE dcrdex_simnet OWNER dcrdex;
```

### Start dcrdex

Start `dcrdex` for simnet, with trace-level logging, and the `dcrdex_simnet` PostgreSQL database created [above](#prepare-the-postgresql-db):

```sh
./dcrdex -d trace --simnet --pgdbname=dcrdex_simnet
```

Note that the registration fee xpub, amount, and required confirmations are set in markets.json now.

## Client (`dexc`) Setup

### The wallet config files for dexc

Bitcoin:

- Config file: `~/dextest/btc/alpha/alpha.conf` or `~/dextest/btc/beta/beta.conf` (also applies the gamma wallet process), but you could want to create for example `~/.dexc/btc-simnet-w.conf` file and select that during wallet setup below.
- Account/wallet: For the alpha node's wallet, "" (empty string) or "gamma". For the beta node's wallet, "" (empty string) or "delta".
- Wallet password: "abc"

Decred:

- Config file: `~/dextest/dcr/alpha/alpha.conf` or `~/dextest/dcr/beta/beta.conf`, but you might want to create `~/.dexc/dcr-simnet-w.conf`.
- Account: default
- Wallet password: "abc"

### Start `dexc`

Clear any existing client simnet files from previous setups:

```sh
rm -fr ~/.dexc/simnet/
```

Start `dexc` with the web UI and RPC server in simnet mode and trace level logging. IMPORTANT: On the `release-0.1` branch you should also specify different HTTP and RPC listen addresses (e.g. To change the RPC port to 6757, and the HTTP IP address to 127.0.0.3: `--rpcaddr=localhost:6757 --webaddr=127.0.0.3:5758`). The following command does not change these, so modify it as necessary:

```sh
./dexc --simnet --rpc --log=trace
```

The client can be configured [via the browser interface](#setup-the-client-via-the-browser-interface) or with dexcctl on the command line.

### Setup the client via the browser interface

In a browser, navigate to <http://127.0.0.3:5758/> (or whatever dexc was configured to use with `--webaddr`), and be sure JavaScript is
enabled. This should redirect to <http://127.0.0.3:5758/register.>

1. Setup the app pass.
2. Configure the DCR wallet (default, abc, /home/jon/dextest/dcr/alpha/w-alpha.conf, app pass from 1.).
3. In the next step, specify the server address `localhost:17273` (for the harness, or whatever you manually configured dcrdex to use). Upload the TLS certificate from `~/dextest/dcrdex/rpc.cert` (for the dcrdex harness) or if running the server manually `~/.dcrdex/rpc.cert`.
4. Pay fee (type in app pass to authorize payment from DCR wallet).
5. If auto-mining is not setup, mine a few blocks on Decred simnet. How many depends on the `regfeeconfirms` setting used with `dcrdex.

    ```none
    ~  dextest  dcr  harness-ctl  $  ./mine-alpha 1 # mine one block
    ```

6. Place a couple orders that match.
7. Mine a few blocks on each chain, alternating chains. How many depends on the `"swapConf"` values in markets.json.

### Setup the client via dexcctl

IMPORTANT: The example `dexcctl` commands in this section use the `--simnet` flag, which is not recognized on the `release-0.1` branch, to change the default dexc ports to which it will attempt to connect.  On the `release-0.1` branch where this is not supported, you will need to specify the `-a, --rpcaddr=` option to match dexc. For example, if dexc was started with `--rpcaddr=localhost:6757`, then `dexcctl` would need to use `--rpcaddr=localhost:6757` as well.

If this is the first time starting `dexc`, the application must be initialized with a new "app password".
For example, initializing with the password "asdf":

```none
$   ./dexcctl --simnet -u u -P p init
Set new app password:  <asdf>
app initialized
```

Setting up a Decred wallet (DCR coin ID is 42):

```none
$   ./dexcctl --simnet -u u -P p newwallet 42 ~/dextest/dcr/alpha/alpha.conf '{"account":"default"}'
App password:       <asdf>
Wallet password:    <abc>
dcr wallet created and unlocked
```

The `dexc` console should log something similar to the following:

```none
[INF] CORE: Created dcr wallet. Account "default" balance available = 5227016557638 / locked = 0, Deposit address = SsfWraUBDUtU2dKAhBAYcb8HVTEupAnj9ta
[INF] CORE: Connected to and unlocked dcr wallet. Account "default" balance available = 5227016557638 / locked = 0, Deposit address = SsfWraUBDUtU2dKAhBAYcb8HVTEupAnj9ta
```

Setting up a Bitcoin wallet (BT coin ID is 0):

```none
$   ./dexcctl --simnet -u u -P p newwallet 0 ~/dextest/btc/alpha/alpha.conf '{"walletname":"gamma"}'
App password:      <asdf>
Wallet password:   <abc>
btc wallet created and unlocked
```

Note that Bitcoin's coin ID is always 0, even on testnet and simnet. Do not use coin ID 1 for BTC testnet.

Now both Decred and Bitcoin wallets are available. Query their status with the `wallets` command:

```none
$   ./dexcctl --simnet -u u -P p wallets
[
  {
    "symbol": "dcr",
    "assetID": 42,
    "open": true,
    "running": true,
    "updated": 0,
    "balance": 5227016557638,
    "address": "SsfWraUBDUtU2dKAhBAYcb8HVTEupAnj9ta",
    "feerate": 10,
    "units": "atoms"
  },
  {
    "symbol": "btc",
    "assetID": 0,
    "open": true,
    "running": true,
    "updated": 0,
    "balance": 8400000000,
    "address": "mig9X7iDprqwnDMR6FyakQpdJo18u7PCLq",
    "feerate": 2,
    "units": "Satoshis"
  }
]
```

Now that a Decred wallet is configured, you can register with a DEX server.  First query for the registration fee with the `getfee` command:

```none
$   ./dexcctl --simnet -u u -P p getfee "http://127.0.0.1:7232" ~/.dcrdex/rpc.cert 
{
  "fee": 100000000
}
```

Note that the fee is in atoms, the smallest unit of value in Decred, where 1 DCR == 1e8 atoms.

If the fee is acceptable, use the `register` command to pay the fee:

```none
$   ./dexcctl --simnet -u u -P p register "http://127.0.0.1:7232" 100000000 ~/.dcrdex/rpc.cert 
App password:    <asdf>
the DEX fee of 100000000 has been paid
```

After the required number of confirmations, the newly created account will be able to trade on that DEX server.
During this process, `dexc` should log messages similar to the following:

```none
[INF] CORE: Connected to DEX http://127.0.0.1:17273 and listening for messages.
[INF] CORE: Attempting registration fee payment for SsfWraUBDUtU2dKAhBAYcb8HVTEupAnj9ta of 100000000 units of dcr
[DBG] CORE[dcr]: 1 signature cycles to converge on fees for tx 4992afa0eaa92bfe82f977466c28fa9cc944c32f43ddad8d7e8c8329e62daa2f
[INF] CORE: notify: |SUCCESS| (fee payment) Fee paid - Waiting for 1 confirmations before trading at http://127.0.0.1:7232
...
[TRC] CORE: processing tip change for dcr
[DBG] CORE: Registration fee txn 326661613264653632393833386337653864616464643433326663333434633939636661323836633436373766393832666532626139656161306166393234393030303030303030 now has 1 confirmations.
[INF] CORE: Notifying dex http://127.0.0.1:17273 of fee payment.
[DBG] CORE: authenticated connection to http://127.0.0.1:17273
[INF] CORE: notify: |SUCCESS| (fee payment) Account registered - You may now trade at http://127.0.0.1:17273
```

For rapid setup, there is a script at `[repo root]/client/cmd/dexcctl/simnet-setup.sh` that has a reasonable default. You may also use this script as a template.

### Acquire DCR and BTC Simnet Funds

You can generate a deposit address for your wallet through the wallets view in the
client app.

Take the generated address to it's respective harness terminal and send funds to it using:

`./alpha sendtoaddress {address} {amount}`

Note: If you're using an `SPV` wallet, run `./mine-alpha 1` after sending or the SPV wallets won't see it.
