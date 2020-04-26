dcrdex
======

[![Build Status](https://github.com/decred/dcrdex/workflows/Build%20and%20Test/badge.svg)](https://github.com/decred/dcrdex/actions)
[![ISC License](https://img.shields.io/badge/license-ISC-blue.svg)](http://copyfree.org)
[![GoDoc](https://img.shields.io/badge/godoc-reference-blue.svg)](https://godoc.org/decred.org/dcrdex)

## The Decred DEX Overview

dcrdex is the repository for the specification and source code of the Decred
Distributed Exchange (DEX).

## Specification

The DEX [specification](spec/README.mediawiki) was drafted following stakeholder
approval of the [specification proposal](https://proposals.decred.org/proposals/a4f2a91c8589b2e5a955798d6c0f4f77f2eec13b62063c5f4102c21913dcaf32).

## Source

The source code for the DEX server and client are being developed according to
the specification. This undertaking was approved via a second DEX [development proposal](https://proposals.decred.org/proposals/417607aaedff2942ff3701cdb4eff76637eca4ed7f7ba816e5c0bd2e971602e1).

## Using the DEX Server

The Decred DEX is in early stages of development, and should not be used to
conduct trades on mainnet. Instructions below are tailored for running on
testnet.

### Dependencies

1. Linux
2. [Go >= 1.13](https://golang.org/doc/install)
3. [PostgreSQL 11+](https://www.postgresql.org/download/), [tuned](https://pgtune.leopard.in.ua/) and running.

**Create the database** in a PostgreSQL `psql` terminal.

```
CREATE USER dcrdex WITH PASSWORD 'dexpass';
CREATE DATABASE dcrdex_testnet OWNER dcrdex;
```

**Generate a master public key** for collecting registration fees. This can be
done with [dcrwallet](https://github.com/decred/dcrwallet) using the
`getmasterpubkey` RPC command. Place the pubkey string into a new DEX
configuration file.

**~/.dcrdex/dcrdex.conf**
```
# Mainnet extended pubkey
regfeexpub=tpubVWHTkHRefqHptAnBdNcDJ...

# PostgreSQL Credentials
pgdbname=dcrdex_testnet
pgpass=dexpass
```

The **app data directory** (*~/.dcrdex/*) is the default location used by the
DEX server, but can be customized with a command-line argument.

Master public keys are network-specific, so **dcrd** and **dcrwallet** will need
to be run on the same network on which the server will be run.

**Generate DEX signing keys** from *server/cmd/genkey*.

```
go build
./genkey
```

and move the *dexpubkey* and *dexprivkey* files to app data directory.

**Run your asset daemons**. As of writing, only `dcrd`, `bitcoind`, and
`litecoind` are supported. The `txindex` configuration option must be set.

**Create the asset and market configuration file**. A sample is given at
*dcrdex/cmd/dcrdex/sample-markets.json*. See the
[**Per-asset Variables**](spec/admin.mediawiki) section of the specification for
more information on individual options.

From a command prompt, navigate to **server/cmd/dcrdex**. Build the executable
by running `go build`. The generated executable will be named **dcrdex**. Run
`./dcrdex --help` to see configuration options that can be set either here or
in the *dcrdex.conf* file. The
[**Exchange Variables**](spec/admin.mediawiki) section of the specification has
additional information on a few key options.

**Run the server.**

`./dcrdex --testnet`

from **server/cmd/dcrdex**.

## Using the DEX Client.

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

This will open up a terminal-based interface for the client.

## Development

Before starting work, chat with us in the
[DEX Development room](https://matrix.to/#/!EzTSRQITaqHuFBDFhM:decred.org?via=decred.org&via=matrix.org&via=zettaport.com).
DEX development will be opening up more and more to public contributions as we
finish up initial development, so keep checking here for updates.

**Contributing code**
1. Fork the repo.
2. Create a branch for your work (`git checkout -b cool-stuff`).
3. Code something great.
4. Commit and push to your forked repo.
5. Create a [pull request](https://github.com/decred/dcrdex/compare).
