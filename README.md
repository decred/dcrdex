# <img src="docs/images/logo_wide_v1.svg" width="250">

[![Build Status](https://github.com/decred/dcrdex/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrdex/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](https://godoc.org/decred.org/dcrdex)

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

- [Market API](#market-api)
- [Client Applications and the Core Package](#client-applications-and-the-core-package)
- [Using Decred DEX](#using-decred-dex)
- [Server Installation](#server-installation)
- [Client Installation](#client-installation)
- [Contribute](#contribute)

## Market API

DEX services are offered via the **Market API**. The
[DEX specification](spec/README.mediawiki) details the messaging and trading
protocols required to use the Market API.

In order to be accessible to the widest range of applications and languages, the
Market API is accessed using JSON messages over WebSockets.
A basic client application can be implemented in most popular programming
languages with little more than the standard library.

## Client Applications and the Core Package

The initial DEX release also includes the client **Core** module, written in Go.
**Core** offers an intuitive programmer interface, with methods for creating
wallets, registering DEX accounts, viewing markets, and performing trades.

**dcrdex** has two applications built with **Core**.

The **browser-based GUI** (a.k.a. "the app") offers a familiar exchange
experience in your browser. The app is really just a one-client web server that
you run and connect to on the same machine. The market view allows you to see
the market's order book in sorted lists or as a depth chart. You can place your
order and monitor it's status in the same market view. The GUI application is
managed by the **dexc** utility in *client/cmd/dexc*.

The **dexcctl** utility enables trading via CLI. Commands are parsed and
issued to **Core** for execution. **dexcctl** also requires **dexc**.

## Using Decred DEX

**The Decred DEX is in early stages of development, and should not be used to
conduct trades on mainnet.** For those who would like to contribute, or just
poke around and offer feedback, there are a number of ways to do that.

- [Run **dcrdex** and **dexc** on simnet](../../wiki/Simnet-Testing). Recommended for development.
- [Run **dexc** on testnet](../../wiki/Testnet-Testing). Recommended for poking around.
- [Run the test app server](../../wiki/Test-App-Server). Useful for GUI development, or just to try everything out without needing to create wallets or connect to a **dcrdex** server.

## Server Installation

### Dependencies

1. Linux or MacOS
2. [Go >= 1.13](https://golang.org/doc/install)
3. [PostgreSQL 11+](https://www.postgresql.org/download/), [tuned](https://pgtune.leopard.in.ua/) and running.

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

### Run your asset daemons.

As of writing, only `dcrd`, `bitcoind`, and `litecoind` are supported. The
`txindex` configuration option must be set. Be sure to specify the correct
network if not using mainnet.

### Create the assets and market configuration file

A sample is given at
[*sample-markets.json*](server/cmd/dcrdex/sample-markets.json). See the
[**Per-asset Variables**](spec/admin.mediawiki) section of the specification for
more information on individual options.

### Build and run assets backend plugin

From a command prompt, navigate to **server/asset/build** .
Build the plugin assets and copy to resource application folder by running:
```
go build -buildmode=plugin ./btc/
cp ./btc.so ~/.dcrdex/assets/btc.so
```
Similar with other assets. Or you can simply navigate to **server/cmd/dcrdex** and run the script __plugins_build.sh__. This script will build dcrdex and all the plugin in the asset folder and move it to app data folder at once time

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

## Client Installation

The client is in early development, and is not fully functional. Instructions
are listed for development purposes.

### Dependencies

1. [Go >= 1.13](https://golang.org/doc/install)
2. [Node 12+](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) is used to bundle resources for the browser interface.

**Build the web assets** from *client/webserver/site/*.

```
npm install
npm run build
```

**Build and run the client** from *client/cmd/dexc*.

```
go build
./dexc --testnet
```

Connect to the client from your browser at `localhost:5758`.

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
- [Front-end Development](../../wiki/Frontend-Development)

## Source

The DEX [specification](spec/README.mediawiki) was drafted following stakeholder
approval of the
[specification proposal](https://proposals.decred.org/proposals/a4f2a91c8589b2e5a955798d6c0f4f77f2eec13b62063c5f4102c21913dcaf32).

The source code for the DEX server and client are being developed according to
the specification. This undertaking was approved via a second DEX
[development proposal](https://proposals.decred.org/proposals/417607aaedff2942ff3701cdb4eff76637eca4ed7f7ba816e5c0bd2e971602e1).
