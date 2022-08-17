# Server Installation

## Server Dependencies

1. Linux or MacOS
2. [Go >= 1.18](https://golang.org/doc/install)
3. [PostgreSQL 11+](https://www.postgresql.org/download/), [tuned](https://pgtune.leopard.in.ua/) and running.
4. Decred (dcrd) and Bitcoin (bitcoind) full nodes, and any other assets' full nodes, both with `txindex` enabled.

## Set up the database

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

### ~/.dcrdex/dcrdex.conf

```ini
# Testnet extended pubkey
regfeexpub=tpubVWHTkHRefqHptAnBdNcDJ...

# PostgreSQL Credentials
pgpass=dexpass
```

*~/.dcrdex/* is the default **app data directory** location used by the
DEX server, but can be customized with the `--appdata` command-line argument.

## Run your asset daemons

Only the **full node** software listed in the [client configuration](https://github.com/decred/dcrdex/wiki/Client-Installation-and-Configuration#optional-external-software)
section are supported for the server. The `txindex` configuration options must
be set. Be sure to specify the correct network if not using mainnet.

## Create the assets and market configuration file

A sample is given at
[*sample-markets.json*](server/cmd/dcrdex/sample-markets.json). See the
[**market json**](https://github.com/decred/dcrdex/wiki/Server-Admin#markets-json) section of the wiki for
more information on individual options.

## Build and run dcrdex

From a command prompt, navigate to **server/cmd/dcrdex**. Build the executable
by running `go build`. The generated executable will be named **dcrdex**. Run
`./dcrdex --help` to see configuration options that can be set either as a
command line argument or in the *dcrdex.conf* file. The
[**Exchange Variables**](https://github.com/decred/dcrdex/blob/master/spec/admin.mediawiki) section of the specification has
additional information on a few key options.

**Run the server.**

`./dcrdex --testnet`

from **server/cmd/dcrdex**.
