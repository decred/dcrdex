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

- [Important Stuff to Know](#important-stuff-to-know)
- [Fees](#fees)
- [DEX Specification](#dex-specification)
- [Client Applications and the Core Package](#client-applications-and-the-core-package)
- [Server Installation](#server-installation)
- [Contribute](#contribute)

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

Only the **full node** software listed in the [client configuration](#optional-external-software)
section are supported for the server. The `txindex` configuration options must
be set. Be sure to specify the correct network if not using mainnet.

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
