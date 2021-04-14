# DCRDEX v0.1.0 (beta)

Oct 20, 2020

## Important Notices

- Ensure your nodes (and wallets) are fully **synchronized with the blockchain
  network before placing orders**. The software will verify this for you in the
  next patch release.
- **Never shutdown your wallets with dexc running**. When shutting down, always
  stop dexc before stopping your wallets.
- If you have to restart dexc with active orders or swaps, you must
  **immediately login again with your app password when dexc starts up**. The
  next patch release wil inform you on startup if this is required.
- There is a ~10 minute "inaction timeout" when it becomes your client's turn to
  broadcast a transaction, so be sure not to stop dexc or lose connectivity for
  longer than that or you risk having your active orders and swaps/matches
  revoked. If you do have to restart dexc, remember to login as soon as you
  start it up again.
- Only one dexc process should be running for a given user account at any time.
  For example, if you have identical dexc configurations on two computers and
  you run dexc and login on both, neither dexc instance will be adequately
  connected to successfully negotiate swaps. Also note that order history is not
  synchronized between different installations at this time.
- Your DEX server accounts exist inside the dexc.db file, the location of which
  depends on operating system, but is typically in ~/.dexc/mainnet/dexc.db or
  %HOMEPATH%\Local\Dexc\mainnet\dexc.db.  Do not delete this account or you will
  need to register again at whatever server was configured in it.
- If you use a non-default bitcoin wallet name, don't forget to set it in
  bitcoin.conf with a `wallet={insert wallet name here}` line so that bitcoind
  will load it each time it starts.  Otherwise dexc will give you a "wallet not
  found" error on startup and login.
- bitcoind's "smart" fee estimation needs plenty of time to warm up or it is not
  so smart, potentially leading to you creating redeem transactions with low
  fees that can make the transaction take a very long time to mine (get
  confirmed).  Either keep your bitcoind running for a couple hours after
  starting it up after it had been stopped for a long time (any more than ~1hr)
  prior to starting it, or ensure that the value returned from a bitcoin-cli
  call to `estimatesmartfee 1` returns a `"feerate"` close to what
  <https://mempool.space/> reports as "High priority".

## Overview

This release of DCRDEX includes a client program called dexc and a server called
dcrdex. Users run their own wallets (e.g. dcrwallet or bitcoind), which dexc
works with to perform trades via atomic swaps. dcrdex facilitates price
discovery by maintaining an order book for one or more markets, and coordinates
atomic swaps directly between pairs of traders that are matched according to
their orders. The server is generally run on a remote system, but anyone may
operate a dcrdex server.

This release supports Decred, Bitcoin, and Litecoin.

### Client (dexc)

- Provides a browser-based interface, which is self-hosted by the dexc program,
  for configuring wallets, displaying market data such as order books, and
  placing and monitoring orders.
- Communicates with any user-specified dcrdex server.
- Funds orders and executes atomic swaps by controlling the external wallets
  (dcrwallet, etc.).

### Server (dcrdex)

- Accepts orders from clients who prove ownership of on-chain coins to fund the
  order.
- Books and matches orders with an epoch-based matching algorithm.
- Relays swap data between matched parties, allowing the clients to perform the
  transactions themselves directly on the assets' blockchains.
- Has a one-time nominal (e.g. 1 DCR) registration fee, which acts as an
  anti-spam measure and to incentivize completing swaps.
- Enforces the code of community conduct by suspending accounts that repeatedly
  violate the rules.

## Features

### Markets and Orders

The server maintains a familiar market of buy and sell orders, each with a
quantity and a rate. A market is defined by a pair of assets, where one asset is
referred to as the **base asset**. For example, in the "DCR-BTC" market, DCR is
the base asset and BTC is known as the **quote asset**. A market is also
specified by a **lot size**, which is a quantity of the base asset. Order
quantity must be a multiple of lot size, with the exception of market buy orders
that are necessarily specified in units of the quote asset that is offered in
the trade. The intent of a client to execute an atomic swap of one asset for
another is communicated by submitting orders to a specific market on a dcrdex
server.

The two types of trade orders are market orders, which have a quantity but no
rate, and limit orders, which also specify a rate. Limit orders also have a
**time-in-force** that specifies if the order should be allowed to become booked
or if it should only be allowed to match with other orders when it is initially
processed. The time-in-force options are referred to as "standing" or
"immediate", where standing indicates the order is allowed to become booked
while immediate restricts that order to being a taker order by only allowing a
match when it is initially processed.

The following image is an example order submission dialog from a testnet DCR-BTC
market with a 40 DCR lot size that demonstrates limit order buying 2 lots (80
DCR) at a rate of 0.001207 BTC/DCR using a standing time-in-force to allow the
order to become booked if it is not filled:

![submit order dialog](https://user-images.githubusercontent.com/9373513/97030709-c8fd9500-1524-11eb-8bd0-3eb4cf95e8c8.png)

Checking the "Match for one epoch only" box above specifies that the limit
order's time-in-force should be immediate, while unchecking it allows the order
to be booked if it does not match with another order at first. The concept of
epochs is described in the [Epoch](#epochs) section.

### Order Funding

Since orders must be funded by coins from the user's wallets, placing an order
"locks" an amount in the relevant wallet. For example, a buy order on the
DCR-BTC market marks a certain quantity of BTC as locked with the user's wallet.
(This involves no transactions or movement of funds.) This amount will be shown
in the "locked" row of the Balances table.

It is important to note that the amount that is locked by the order may be
**larger than the order quantity** since the "locked" amount is dependent on the
size of the UTXO (for UTXO-based assets like Bitcoin and Decred) that is
reserved for use as an input to the swap transaction, where the amount that does
not enter the contract goes in a change address. This is no different from when
you make a regular transaction, however because the input UTXOs are locked in
advance of broadcasting the actual transaction that spends them, you will see
the amount locked in the wallet until the swap actually takes place.

Depending on the asset, there may be a wallet setting on the Wallets page to
pre-size funding UTXOs to avoid this over-locking, but (1) it involves an extra
transaction that pays to yourself before placing the order, which has on-chain
transaction fees that may be undesirable on chains like BTC, and (2) it is only
applied for limit orders with standing time-in-force since the the UTXOs are
only locked until the swap transaction is broadcasted, which is relatively brief
for taker-only orders that are never booked.

### Epochs

An important concept with DCRDEX is that newly submitted orders are processed in
short windows of time called **epochs**, the length of which is part of the
server's market configuration, but is typically on the order of 10 seconds. When
a valid order is received by the server, it enters into the pool of epoch orders
before it is matched and/or booked. The motivation for this approach is
described in detail in the [DCRDEX specification](../../spec/README.mediawiki).
The Your Orders table will show the status of such orders as "epoch" until they
are matched at the end of the epoch, as described in the next section.

Order cancellation requests are also processed in the epoch with trade
(market/limit) orders since a cancellation is actually a type of order. However,
from the user's perspective, cancelling an order is simply a matter of clicking
the cancel icon for one of their booked orders.

### Matching

When the end of an epoch is reached, the orders it includes are then matched
with the orders that are already on the book. A key concept of DCRDEX order
matching is a deterministic algorithm for shuffling the epoch orders so that it
is difficult for a user to game the system. To perform the shuffling of the
closed epoch prior to matching, clients with orders in the epoch must provide to
the server a special value for each of their orders called a **preimage**, which
must correspond to another value that was provided when the order was initially
submitted called the **commitment**. This is done automatically by dexc,
requiring no action from the user.

If an order fails to match with another order, it will become either **booked**
or **executed** with no part of the order filled. The Your Orders table displays
the current status and remaining quantity of each of a user's orders. If an
order does match with another trade order, the order status will become
**settling**, and atomic swap negotiation begins. A cancel order may also fail
to match if another trade matches with the targeted order first, or it may match
after the targeted order is partially filled in the same epoch.

### Settlement

When maker orders (on the book) are matched with taker orders (from an epoch),
the atomic swap sequence begins. No action is required from either user during
the process.

In the current atomic swap protocol, the **maker initiates** by broadcasting a
transaction with a swap contract on the relevant asset network, and informing
the server of the transaction and the full contract. The server audits the
contract, and if it is successfully validated, the information is relayed to the
taker, who independently audits the contract to ensure it meets their
expectations. The transaction containing the maker's swap contract must then be
mined and reach the **swap confirmation requirement**, which is also a market
setting. For example, Bitcoin might require 3 confirmations while other chains
like Litecoin might be considerably more. When the required number of
confirmations is reached, the **taker participates** by broadcasting a
transaction with their swap contract and informing the server. Again, the server
and the counterparty audit the contract and wait for that asset's swap
confirmation requirement. When the required confirmations are reached, the
**maker redeems** the taker's contract and informs the server of the redemption
transaction. This is the end of the process for the maker, as the redemption
spends the taker's contract, paying to an address controlled by the maker. The
server relays the maker's redeem data to the taker, and the **taker redeems**
immediately, ending the swap.

The Order Details page shows each match associated with an order. For example, a
match where the user was the taker is shown below with links to block explorers
for each of the transactions described above. The maker will have their
redemption listed, but not the taker's.

![match details](https://user-images.githubusercontent.com/9373513/97028559-eda43d80-1521-11eb-9ab6-2e0b21df584d.png)

Orders may be partially filled in increments of the lot size. Hence a single
order may have more than one match (and thus swap) associated with it, each of
which will be shown on the Order Details page.

Wallet balances will change during swap negotiation. When the client broadcasts
their swap contract, the amount locked in that contract will go into the
"locked" row for the asset that funded the order. When the counterparty redeems
their contract, that amount will be reduced by the contract amount, and the user
will redeem the counterparty contract, thus adding to the balance of the other
asset. This is the essence of the atomic swap. Note that until the redemption
transactions are confirmed, the redeemed amount may remain in the wallet's
"immature" balance category, but this depends on the asset.

### Revoked Matches

While the atomic swap process requires no party to trust the other, a swap may
be forced into an alternate path ending in one or both users refunding
themselves by spending their own contract after the lock time expires. This
happens when one of the parties fails to act in the expected time frame, an
**inaction timeout**. When an inaction timeout occurs the following happens:

- The match is revoked, and both parties are notified.
- The at-fault user has their order revoked (if it was partially filled and
  still booked) and is notified.
- The at-fault user has their score adjusted according to type of match failure.
  See below for descriptions of each type and the associated user score
  adjustments.

The general categories of match failures are:

- `NoMakerSwap`: A match is made, but the maker does not initiate the swap. No
  transactions are created in this case.
- `NoTakerSwap`: The maker (initiator) broadcasts their swap contract
  transaction and informs the server, but the taker (participant) fails to
  broadcast their swap contract and inform the server. The maker will
  automatically refund their contract when it expires after 20 hrs.
- `NoMakerRedeem`: The taker broadcasts their swap and informs the server, but
  the maker does not redeem it. The taker will refund when their contract
  expires after 8 hrs. Note that the taker's client begins watching for an
  unannounced redeem of their contract by the maker, which reveals the secret
  and permits the taker to redeem as well, completing the swap although in a
  potentially extended time frame.
- `NoTakerRedeem`: The maker redeems the taker's contract and informs the
  server, but the taker fails to redeem the maker's contract even though they
  can do so at any time. This case is not disruptive to the counterparty, and is
  only detrimental to the takers, so it is of minimal concern.

NOTE: The *order* remaining amounts are still reduced at match time although
they did not settle that portion of the order.

### User Scoring

Users have an incentive to respond with their preimage for their submitted
orders and to complete swaps as the match negotiation protocol specifies, and if
they repeatedly fail to act as required, their account may be suspended. This
may require either communicating an excusable reason for the issue to the server
operator, or registering a new account. However, a reasonable scoring system is
established to balance the need to deter intentional disruptions with the
reality of unreliable consumer networks and other such technical issues.

In this release, there are two primary inaction violations that adjust a users
score: (1) failure to respond with a preimage for an order when the epoch for
that order is closed (preimage miss), and (2) swap negotiation resulting in
match revocation as described in the [previous section](#revoked_matches).

The score threshold at which an account becomes suspended (ban score) is an
operator set variable, but the default is 20.

The adjustment to the at-fault user's score depends on the match failure:

| Match Outcome   | Points | Notes                                              |
|-----------------|-------:|----------------------------------------------------|
| `NoMakerSwap`   |      4 | book spoof, taker needs new order, no locked funds |
| `NoTakerSwap`   |     11 | maker has contract stuck for 20 hrs                |
| `NoMakerRedeem` |      7 | taker has contract stuck for 8 hrs                 |
| `NoTakerRedeem` |      1 | counterparty not inconvenienced, only self         |
| `Success`       |     -1 | offsets violations                                 |

A preimage miss adds 2 points to the users score.

The above scoring system should be considered tentative while it is evaluated in
the wild.

### Order Size Limits

This release uses an experimental system to set the maximum order quantity based
on their swap history. It is likely to change, but it is described in [PR #750](https://github.com/decred/dcrdex/pull/750).

## Code summary

This release consists of 473 pull requests comprising 506 commits from 12
contributors.

Contributors (alphabetical order):

- Brian Stafford (@buck54321)
- David Hill (@dajohi)
- @degeri
- Donald Adu-Poku (@dnldd)
- Fernando Abolafio (@fernandoabolafio)
- Joe Gruffins (@JoeGruffins)
- Jonathan Chappelow (@chappjc)
- Kevin Wilde (@kevinstl)
- @song50119
- Victor Oliveira (@vctt94)
- Wisdom Arerosuoghene (@itswisdomagain)
- @zeoio

(there is no previous release to which a diff can be made)
