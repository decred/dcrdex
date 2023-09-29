# Client Installation and Configuration

## Client Quick Start Installation

The DEX client can be installed in one of the following ways. Download the
application from *just one* of the following locations:

* Download [standalone DEX client for your operating system](https://dex.decred.org/#downloads).
* Download [the latest standalone DEX client release from GitHub](https://github.com/decred/dcrdex/releases)
  for your operating system. Extract the "dexc" executable from the archive and run it.
  Open any web browser to the link shown by the application. You may also put the **dexc**
  executable's folder on your `PATH`.
* [Use Decrediton](https://docs.decred.org/wallets/decrediton/decrediton-setup/),
  the official graphical Decred wallet, which integrates the DEX client, and go
  to the DEX tab.
* (Legacy users) Use the Decred command line application installer,
  [**dcrinstall**](https://docs.decred.org/wallets/cli/cli-installation/). This
  is no longer recommended.
* Build the standalone client [from source](https://github.com/decred/dcrdex/wiki/Client-Installation-and-Configuration#advanced-client-installation).

**WARNING**: If you decide to build from source, use the latest release branch,
not `master`.

### Important Note on External Wallets

If using external wallet software (e.g. **dcrd**+**dcrwallet**, **bitcoind**,
Electrum, etc.), they must remain running while the DEX client is running. Do
not shut down, lock, unlock, or otherwise modify your wallet settings while the
client is running. Also, only send funds from within the DEX application, not
directly from the external wallet's own controls. Finally, do not manually lock
or unlock any coins while the DEX client is running.

## Client Configuration

These instructions assume you've obtained the DEX client as described in the
[Client Quick Start Installation](#client-quick-start-installation) section.

### Prerequisites

If you use the standalone DEX client, you will need a web browser to open the
DEX client user interface as described in the next section.

Most users will use the native wallets that are already built into the DEX
client. Depending on the asset, you may be able to choose from: (1) a native
wallet, (2) an external full node wallet, or (3) an Electrum-based wallet.
Consult the following table for a summary of wallet support. If there is a
checkmark in the "native" column, no external software is required.

| Coin         | native | full node | Electrum | notes                        |
|--------------|--------|-----------|----------|------------------------------|
| Bitcoin      |    ✓   | [v0.21-v24](https://bitcoincore.org/en/download/) | [v4.2.x](https://electrum.org/) |                              |
| Decred       |    ✓   | [v1.7-1.8](https://github.com/decred/decred-release/releases) |     x    |                              |
| Ethereum     |    ✓   | geth IPC/http/ws |   N/A   |  see <https://github.com/decred/dcrdex/wiki/Ethereum>   |
| Litecoin     |    ✓   | [v0.21.2.2](https://litecoin.org/) | [v4.2.x](https://electrum-ltc.org/) | may require a [bootstrap peer](https://gist.github.com/chappjc/d0f26b12258f8531bb78b37f38d080a0) |
| Bitcoin Cash |    ✓   | [v24+](https://bitcoincashnode.org/) |     x    | use only Bitcoin Cash Node for full node |
| Dogecoin     |    x   |  [v1.14.5+](https://dogecoin.com/) |     x    |                              |
| Zcash        |    x   |   [v5.4.2](https://z.cash/download/)  |     x    |                              |

NOTE: The Electrum option is less mature and provides less privacy than the
other wallet types. Some manual configuration of the Electrum wallet's RPC
server is also necessary ([example](images/electrum-rpc-config.png)).

### Synchronizing Wallets

**If using the native wallets** that are integrated with the DEX client (see
above), you can skip this section.

If you choose to use an external wallet (full node or Electrum), you must start
and synchronize them with their networks *before* running the DEX client.

Note that Bitcoin Core and most "clones" support block pruning, which can keep
your blockchain storage down to a few GB, not the size of the full blockchain,
but a large size should be used to avoid full reindexing if used infrequently.
Also, for good network fee estimates, the full node should be running for
several blocks.

### Initial Setup

1. Start the client. For the standalone client (**dexc**), open a command prompt
   in the folder containing the dexc application and run it. e.g. `./dexc` on
   Mac and Linux, or `dexc.exe` on Windows. To avoid the command prompt on
   Windows, `dexc-tray.exe` may be run instead. If using Decrediton instead of
   **dexc**, just click the "DEX" tab and skip to step 3.

2. In your web browser, navigate to <http://localhost:5758>. Skip this step if
   using Decrediton.

   <img src="images/omnibar-client.png" width="320">

3. Set your new **client application password**. You will use this password to
   perform all future security-sensitive client operations.

   <img src="images/client-pw.png" width="320">

   NOTE: Checking the "Remember my password" box only applies to the current
   session. It is easiest for most users to have it checked.

4. Choose the DEX host that you would like to use. Either click one of the
   pre-defined hosts, or enter the address of a known host that you would like
   to use.

   <img src="images/add-dex-reg.png" width="320">

   NOTE: If you just want to view the markets without being able to trade, check
   the "No account" box. You will have an opportunity to create an identity
   later, but the remaining steps assume you are preparing to trade.

5. The DEX host will show all offered markets, and a choice of assets with which
   you can lock in a bond to enable trading. Select the asset you wish to use.

   <img src="images/choose-bond-asset.png" width="400">

   NOTE: A dedicate wiki page describing time-locked fidelity bonds will be
   created, but in short, fidelity bonds are funds redeemable only by you, but
   in the future. Having a potential trader lock some amount of funds before
   placing orders is an anti-spam mechanism to combat disruptive behavior like
   backing out on swaps.

6. Choose the type of wallet to use. In this screenshot, we choose a native BTC
   wallet and click "Create!". The wallet will begin to synchronize with the
   asset's network.

   <img src="images/create-btc.png" width="360">

   NOTE: This is your own **self-hosted** wallet. The wallet's address keys are
   derived from the DEX application's "seed", which you may backup from the
   Settings page at any time. Further, no central wallet backend service is
   involved, only the nodes on the coin's decentralized network.

7. The next form will show you synchronization progress, and give you the first
   deposit address for the wallet and the minimum amount you should deposit to
   be able to create your first bond in the next step, which is required to
   place orders. **This is your wallet**, so deposit as much as you like! After
   sending to your address, the transaction **must confirm** (i.e. be mined in a
   block) before the form will update your balance. This form will be skipped if
   the wallet is already funded and synchronized.

   <img src="images/sync-fund-btc.png" width="360">

   **IMPORTANT**: This is your own local wallet, and you can send as much as you
   like to it since *only* the amount required for the bond will be spent in the
   next step. The remaining amount, minus a small reserve for future bond
   transactions, will be in your available balance. For example, you can send
   yourself 5 BTC and only the required amount (0.0014 BTC in the case pictured
   above) will be spent to create the bond in the next step, with an equivalent
   amount plus fees in reserves. The remainder goes to your available balance,
   which can then be traded, sent, or simply held in the wallet.

   You may disable future bonds at any time by changing the "Target Tier" to 0
   in the "Update Bond Options" form accessible from DEX host settings form
   accessible from the Settings page. This will return any reserves to the
   available balance. Any active bonds will automatically be refunded when their
   lock time expires (currently 2 months after creation).

   NOTE: The native Litecoin and Bitcoin Cash wallets connect to full nodes on
   the blockchain network that have "compact block filters" enabled. It may take
   time for the wallet to crawl the network until it finds such nodes. Be
   patient; otherwise you can bootstrap the process using a known seed node such
   as the Litecoin nodes on [this list](https://gist.github.com/chappjc/d0f26b12258f8531bb78b37f38d080a0).

8. Once the wallet is synchronized and has at least enough to create your
   time-locked fidelity bond, the form will update, and you should click the
   button to create and broadcast your bond transaction.

   <img src="images/register-button.png" width="360">

   After proceeding, the available balance will be the amount you deposited
   in the previous step minus this bond amount and transaction fees.

9. You will then be taken to the **Markets** page, where you must wait for
   confirmations on your bond transaction:

   <img src="images/wait-for-confs.png" width="360">

   While waiting, you may create additional wallets either directly from the
   displayed market or on the Wallets page accessible from the navigation bar at
   the top. This is also a good time to retrieve your application "seed", as
   described in the next step.

   After the transaction is confirmed, the application will submit the bond for
   validation and you will be ready to trade:

   <img src="images/bond-accepted.png" width="360">

   It is recommended to export bond information whenever they are created since
   they are not automatically restored from just the application seed. Do this
   using the "Export Account" button of the DEX host settings accessible from
   the Settings page. If you restore from seed in the future: create the same
   wallets, add the same DEX host, and *then* import the bonds from this backup.

10. At any time you can go to the Settings page via the "gears" icon in the top
    navigation bar to retrieve the application seed that was generated when
    initializing the application in the first dialog. This seed is used to
    restore your DEX accounts and any native wallets, so keep it safe.

    <img src="images/view-seed.png" width="360">

11. That's it! Use the Buy/Sell form on the Markets page to begin placing
   orders. Go to the Wallets page to obtain addresses for your wallets so that
   you can send yourself funds to trade.

## Advanced Client Installation

### Dependencies

1. [Go 1.19 or 1.20](https://golang.org/doc/install)
2. (optional) [Node 18 or 20](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) is used to bundle resources for the browser interface. It's important to note that the DEX client has no external JavaScript dependencies. The client doesn't import any Node packages. We only use Node to lint and compile our own JavaScript and css resources. This build step is not required if building from a release branch such as `release-v0.6`.
3. At least 2 GB of available system memory.

### Build from Source

**Build the web assets** from *client/webserver/site/*.

**If building from the `master` branch,** bundle the CSS and JavaScript with Webpack:

```sh
npm clean-install && npm run build
```

**Build and run the client** from *client/cmd/dexc*.

```sh
go build
./dexc
```

Connect to the client from your browser at `localhost:5758`.

While `dexc` may be run from within the git workspace as described above, the
`dexc` binary executable generated with `go build` can be copied into a
different folder (e.g. `/opt/dcrdex/dexc`).

### Docker

#### Build the docker image

```sh
docker build -t user/dcrdex -f client/Dockerfile .
```

#### Create docker volume

```sh
docker volume create --name=dcrdex_data
```

#### Run image

```sh
docker run -d --rm -p 127.0.0.1:5758:5758 -v dcrdex_data:/root/.dexc user/dcrdex
```
