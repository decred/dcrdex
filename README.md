# <img src="docs/images/logo_wide_v1.svg" alt="DCRDEX" width="250">

[![Build Status](https://github.com/decred/dcrdex/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrdex/actions)
[![ISC License](https://img.shields.io/badge/license-Blue_Oak-007788.svg)](https://blueoakcouncil.org/license/1.0.0)
[![GoDoc](https://img.shields.io/badge/go.dev-reference-blue.svg?logo=go&logoColor=lightblue)](https://pkg.go.dev/decred.org/dcrdex)

## What is DEX?

The Decred Decentralized Exchange (DEX) is a system that enables trustless
exchange of different types of blockchain assets via a familiar market-based
API. DEX is a non-custodial solution for cross-chain exchange based on
atomic swap technology. DEX matches trading parties and facilitates price
discovery and the communication of swap details.

Matching is performed through a familiar exchange interface, with market and
limit orders and an order book. Settlement occurs on-chain. DEX's epoch-based
matching algorithm and rules of community conduct ensure that the order book
action you see is real and not an army of bots.

Trades are performed directly between users through on-chain contracts with no
actual reliance on DEX, though swap details must be reported both as a
courtesy and to prove compliance with trading rules. Trades are settled with
pure 4-transaction atomic swaps and nothing else. Because DEX collects no
trading fees, there's no intermediary token and no fee transactions.

Although trading fees are not collected, DEX does require a one-time
registration fee to be paid on-chain. Atomic swap technology secures all trades,
but client software must still adhere to a set of policies to ensure orderly
settlement of matches. The maximum penalty imposable by DEX is loss of trading
privileges and forfeiture of registration fee.

## Contents

- [Client Quick Start Installation](#client-quick-start-installation)
- [Client Configuration](#client-configuration)
- [Important Stuff to Know](#important-stuff-to-know)
- [Fees](#fees)
- [Advanced Client Installation](#advanced-client-installation)
- [DEX Specification](#dex-specification)
- [Client Applications and the Core Package](#client-applications-and-the-core-package)
- [Server Installation](#server-installation)
- [Contribute](#contribute)

## Client Quick Start Installation

The DEX client can be installed in one of three ways:

1. [Install Decrediton](https://docs.decred.org/wallets/decrediton/decrediton-setup/) and go to the DEX tab.
2. Install the standalone DEX client using `dcrinstall` as described below.
3. Build the standalone client [from source](#advanced-client-installation).

If available for the latest release, the
[**dcrinstall**](https://docs.decred.org/wallets/cli/cli-installation/) tool
will install everything you need, and help set up your Decred wallet. Just run
dcrinstall from the command line with `--dcrdex` appended to the command shown
on the linked page for your operating system. Otherwise, see the latest DCRDEX
releases [on Github](https://github.com/decred/dcrdex/releases) for source code
or pre-compiled standalone packages.

**WARNING**: If you decide to build from source, use the `release-v0.5` branch,
not `master`.

### Sync Blockchains

Full nodes are NOT required to use DEX with BTC, DCR, or LTC. For Bitcoin, you
can choose to create a native (built into the DEX client) BTC wallet by choosing
the "Native" wallet type in the DEX client dialogs. A native DCR wallet is also
available, but you may use Decrediton or dcrwallet running in SPV (light) mode
for a more full-featured Decred wallet.

NOTE: The upcoming Ethereum wallet is also a native wallet that uses the standard
[light ethereum subprotocol (LES)](https://github.com/ethereum/devp2p/blob/master/caps/les.md).
However, ETH trading is not enabled in the current release and the wallet is not
available in the default build.

Both LTC and BTC support external Electrum wallets, but this option is less
mature and provides less privacy than the other wallet types. Be sure the wallet
is connected to and synchronized with the network first, only the
"default_wallet" is loaded, and its RPC server is configured
([example](./docs/images/electrum-rpc-config.png)).

If you choose to use full node wallets, you must fully synchronize them with
their networks *before* running the DEX client. This refers to **dcrd**,
**bitcoind**, **litecoind**, etc. Note that Bitcoin Core and most "clones"
support block pruning, which can keep your blockchain storage down to a few GB,
not the size of the full blockchain. Also, for good network fee estimates, the
node should be running for several blocks.

### Important Notes on Wallets

- **If you already have Decrediton installed**, upgrade Decrediton before
  running **dcrinstall**.

- If using external wallet software (e.g. Decrediton, **dcrd**+**dcrwallet**,
  **bitcoind**, Electrum, etc.), they must remain running while the DEX client
  is running. Do not shut down, lock, unlock, or otherwise modify your wallet
  settings while the client is running.

- For Electrum, the wallet must be the "default_wallet". Only one Electrum
  wallet should be opened to ensure the correct one is accessed.

## Client Configuration

These instructions assume you've used the
[Client Quick Start Installation](#client-quick-start-installation). If you've
used a [custom installation](#advanced-client-installation) for the client
and/or blockchain software, adapt as necessary.

### Prerequisites

External wallet software is not required for all assets. The native light
wallets are the simplest and best option for most users. But if using external
wallets, they should be running and synced before starting DEX. See the next
section for a list of supported wallets and assets.

Unless you use Decrediton to start DEX, you will need a web browser to open the
DEX client user interface as described in the next section.

### Optional External Software

Depending on which assets you wish to use, you have different choices for wallet
software. There are native/built-in light wallet options for Bitcoin and Decred,
an external light wallet option for Litecoin, and full-node support for all
other assets including: Bitcoin, Decred, Litecoin, ZCash, Dogecoin, Bitcoin
Cash. The following release will include Ethereum support with a native light
wallet.

1. **Bitcoin.** The native wallet has no prerequisites. To use a Bitcoin Core
   full node wallet (bitcoind or bitcoin-qt), the supported versions are
   [v0.21, v22, or v23](https://bitcoincore.org/en/download/) .
   Descriptor wallets are not supported in v0.21.
   An [Electrum v4.2.x](https://electrum.org/) wallet is also supported.
2. **Decred.** The native wallet has no prerequisites. Alternatively, Decrediton
   or the dcrwallet command line application may be used in either SPV or full
   node (RPC) mode. The latest Decrediton installer includes DEX. If using a
   standalone [dcrwallet](https://github.com/decred/dcrwallet), install from the
   [v1.7.x release binaries](https://github.com/decred/decred-release/releases),
   or build from the `release-v1.7` branches.
3. **Litecoin.** Either [Litecoin Core v0.21.x](https://litecoin.org/), or
   [Electrum-LTC v4.2.x](https://electrum-ltc.org/) are supported.
4. **Dogecoin.** [Dogecoin Core v1.14.5+](https://dogecoin.com/).
5. **ZCash.** [zcashd v5.1](https://z.cash/download/).
6. **Bitcoin Cash.** [Bitcoin Cash Node v24+](https://bitcoincashnode.org/en/)

### Connect Wallets and Register

1. Start the client. Either go to the "DEX" tab within Decrediton, or with the
   standalone client, open a command prompt in the folder containing the
   pre-compiled dexc client files and run `./dexc` (`dexc.exe` on Windows).
2. In your web browser, navigate to http://localhost:5758. Skip this step if
   using Decrediton.

[//]: # "TODO: update with a few screenshots for the new steps below"

1. Set your new **client application password**. You will use this password to
   perform all future security-sensitive client operations, including
   registering, signing in, and trading.

2. Choose the DEX server you would like to use. Either click one of the
   pre-defined hosts such as **dex.decred.org**, or enter the address of a known
   server that you would like to use.

3. The DEX server will offer a choice of assets with which to pay the one-time
   setup fee. (This is a nominal amount just to discourage abuse and maintain a
   good experience for all users. No further fees are collected on trades.)
   Select the asset you wish to use, and then configure the wallet. After the
   wallet is configured, the form will give you the first deposit address for
   the wallet and the minimum amount you should deposit to be able to pay the
   fee. NOTE: This is your own local wallet, and you can send as much as you
   like to the wallet since *only* the amount required for the fee will be spent
   when registering in the next step. The remaining balance will be available
   for trading or may be withdrawn.

4. Once the wallet is synchronized and has at least enough to pay the server's
   defined fee, click the button to submit the registration request.

5. You will then be taken to the **markets view**, where you must wait for
   confirmations on your registration fee transaction, at which time your client
   will automatically complete authentication with that server. While waiting,
   you may create additional wallets either directly from the displayed market
   or on the Wallets page accessible from the navigation bar at the top.

6. And that's it! The form to Buy/Sell will appear, and you can begin placing
   orders. Go to the Wallets page to obtain addresses for your wallets so that
   you can send yourself funds to trade.

## Important Stuff to Know

Trades settle on-chain and require block confirmations. Trades do not settle instantly.
In some cases, they may take hours to settle.
**The client software should not be shut down until you are absolutely certain that your trades have settled**.

**The client has to stay connected for the full duration of trade settlement**.
Losses of connectivity of a couple minutes are fine, but don't push it.
A loss of internet connectivity for more than 20 hours during trade settlement has the potential to result in lost funds.
Simply losing your connection to the DEX server does not put funds at risk.
You would have to lose connection to an entire blockchain network.

**There are initially limits on the amount of ordering you can do**.
We'll get these limits displayed somewhere soon, but in the meantime,
start with some smaller orders to build up your reputation. As you complete
orders, your limit will go up.

**If you fail to complete swaps** when your orders are matched, your account
will accumulate strikes that may be lead your account becoming automatically
suspended. These situations are not always intentional (e.g. prolonged loss of
internet access, crashed computer, etc.), so for technical assistance, please
reach out
[on Matrix](https://matrix.to/#/!mlRZqBtfWHrcmgdTWB:decred.org?via=decred.org&via=matrix.org).

## Fees

DEX does not collect any fees on the trades, but since all swap transactions
occur on-chain and are created directly by the users, they will pay network
transaction fees. Transaction fees vary based on how orders are matched. Fee
estimates are shown during order creation, and the realized fees are displayed
on the order details page.

To ensure that on-chain transaction fees do not eat a significant portion of the
order quantity, orders must be specified in increments of a minimum lot size.
To illustrate, if on-chain transaction fees worked out to $5, and a user was able
to place an order to trade $10, they would lose half of their trade to
transaction fees. For chains with single-match fees of $5, if the operator wanted
to limit possible fees to under 1% of the trade, the minimum lot size would need
to be set to about $500.

The scenario with the lowest fees is for an entire order to be consumed by a
single match. If this happens, the user pays the fees for two transactions: one
on the chain of the asset the user is selling and one on the chain of the asset
the user is buying. The worst case is for the order to be filled in multiple
matches each of one lot in amount, potentially requiring as many swaps as lots
in the order.
Check the
[dex specification](https://github.com/decred/dcrdex/blob/master/spec/atomic.mediawiki)
for more details about how atomic swaps work.

## Advanced Client Installation

### Dependencies

1. [Go 1.18 or 1.19](https://golang.org/doc/install)
2. [Node 16 or 18](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) is used to bundle resources for the browser interface. It's important to note that the DEX client has no external JavaScript dependencies. The client doesn't import any Node packages. We only use Node to lint and compile our own JavaScript and css resources.
3. At least 2 GB of available system memory.

### Build from Source

**Build the web assets** from *client/webserver/site/*.

Bundle the CSS and JavaScript with Webpack:

```
npm clean-install
npm run build
```

**Build and run the client** from *client/cmd/dexc*.

```
go build
./dexc
```

Connect to the client from your browser at `localhost:5758`.

While `dexc` may be run from within the git workspace as described above, the
`dexc` binary executable generated with `go build` and the entire `site` folder
may be copied into a different folder as long as `site` is in the same directory
as `dexc` (e.g. `/opt/dcrdex/dexc` and `/opt/dcrdex/site`).

### Docker

**Build the docker image**

```
docker build -t user/dcrdex -f client/Dockerfile .
```

**Create docker volume**

```
docker volume create --name=dcrdex_data
```

**Run image**

```
docker run -d --rm -p 127.0.0.1:5758:5758 -v dcrdex_data:/root/.dexc user/dcrdex
```

## DEX Specification

The [DEX specification](spec/README.mediawiki) details the messaging and trading
protocols required to use the Market API. Not only is the code in
in the **decred/dcrdex** repository open-source, but the entire protocol is
open-source. So anyone can, in principle, write their own client or server based
on the specification. Such an endeavor would be ill-advised in these early
stages, while the protocols are undergoing constant change.

## Server Installation

### Server Dependencies

1. Linux or MacOS
2. [Go >= 1.18](https://golang.org/doc/install)
3. [PostgreSQL 11+](https://www.postgresql.org/download/), [tuned](https://pgtune.leopard.in.ua/) and running.
4. Decred (dcrd) and Bitcoin (bitcoind) full nodes, and any other assets' full nodes, both with `txindex` enabled.

### Set up the database

In a PostgreSQL `psql` terminal, run

```sql
CREATE USER dcrdex WITH PASSWORD 'dexpass';
CREATE DATABASE dcrdex_testnet OWNER dcrdex;
```

### Generate a dcrwallet account public key

The master public key is used for collecting registration fees.
Using [dcrctl](https://docs.decred.org/wallets/cli/dcrctl-basics/)
and [dcrwallet](https://github.com/decred/dcrwallet),
create a new account.

`dcrctl --wallet --testnet createnewaccount fees`

Get the master public key for the account.

`dcrctl --wallet --testnet getmasterpubkey fees`

Master public keys are network-specific, so make sure to specify the network
to both `dcrwallet` and `dcrctl`, if not using mainnet.

Place the pubkey string into a new DEX configuration file.

**~/.dcrdex/dcrdex.conf**

```ini
# Testnet extended pubkey
regfeexpub=tpubVWHTkHRefqHptAnBdNcDJ...

# PostgreSQL Credentials
pgpass=dexpass
```

*~/.dcrdex/* is the default **app data directory** location used by the
DEX server, but can be customized with the `--appdata` command-line argument.

### Run your asset daemons

As of writing, only `dcrd`, `bitcoind`, and `litecoind` are supported. The
`txindex` configuration option must be set. Be sure to specify the correct
network if not using mainnet.

### Create the assets and market configuration file

A sample is given at
[*sample-markets.json*](server/cmd/dcrdex/sample-markets.json). See the
[**market json**](https://github.com/decred/dcrdex/wiki/Server-Admin#markets-json) section of the wiki for
more information on individual options.

### Build and run dcrdex

From a command prompt, navigate to **server/cmd/dcrdex**. Build the executable
by running `go build`. The generated executable will be named **dcrdex**. Run
`./dcrdex --help` to see configuration options that can be set either as a
command line argument or in the *dcrdex.conf* file. The
[**Exchange Variables**](spec/admin.mediawiki) section of the specification has
additional information on a few key options.

**Run the server.**

`./dcrdex --testnet`

from **server/cmd/dcrdex**.

## Contribute

**Looking to contribute? We need your help** to make DEX &#35;1.

Nearly all development is done in Go and JavaScript. Work is coordinated
through [the repo issues](https://github.com/decred/dcrdex/issues),
so that's the best place to start.
Before beginning work, chat with us in the
[DEX Development room](https://matrix.to/#/!EzTSRQITaqHuFBDFhM:decred.org?via=decred.org&via=matrix.org&via=zettaport.com).
The pace of development is pretty fast right now, so you'll be expected to keep
your pull requests moving through the review process.

Check out these wiki pages for more information.

- [Getting Started Contributing](../../wiki/Contribution-Guide)
- [Backend Development](../../wiki/Backend-Development)
- [Run **dcrdex** and **dexc** on simnet](../../wiki/Simnet-Testing). Recommended for development.
- [Run **dexc** on testnet](../../wiki/Testnet-Testing). Recommended for poking around.
- [Run the test app server](../../wiki/Test-App-Server). Useful for GUI development, or just to try everything out without needing to create wallets or connect to a **dcrdex** server.

## Source

The DEX [specification](spec/README.mediawiki) was drafted following stakeholder
approval of the
[specification proposal](https://proposals.decred.org/proposals/a4f2a91c8589b2e5a955798d6c0f4f77f2eec13b62063c5f4102c21913dcaf32).

The source code for the DEX server and client are being developed according to
the specification. This undertaking was approved via a second DEX
[development proposal](https://proposals.decred.org/proposals/417607aaedff2942ff3701cdb4eff76637eca4ed7f7ba816e5c0bd2e971602e1).
