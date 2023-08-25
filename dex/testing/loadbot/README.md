# LoadBot

LoadBot is a load testing client for dcrdex.

### Programs

When you run LoadBot, you must specify a **program**.
You specify the **program** with the `-p` flag.

`./loadbot -p pingpong`

A **program** is a unique trading profile.

#### pingpong / pingpong<n>

The **pingpong** program runs a single **trader** that places alternating buy
and sell orders at the current mid-gap price. The optional `n` parameter allows
running up to 4 **pingpong traders** simultaneously, each with their own
wallets and `core.Core`. For example `./loadbot -p pingpong2` will run two
pingpong **traders** in parallel.

#### sidestacker

The **sidestacker** program runs 2 **traders**, one seller and one buyer. Each
epoch, the **traders** will attempt to create order book depth on their side.
If the book is deep enough already, they will place taker orders targeting
the other side. Can be further altered by setting a linear increase
(trending market) or an automatic ascending/descending pattern (sideways market).

#### compound

The **compound** program will run 2 opposing **sidestacker traders**, 1
**pingpong** trader, and one **trader** called a **sniper**, which will
regularly pick off the best order from a random side.

#### heavy

The **heavy** program is like **compound** on steroids. Four
**sidestacker traders** with 6 orders per epoch, and a 5-order per epoch
**sniper**.

#### whale

The **whale** program runs multiple **traders** that randomly push the market in
one direction or the other. Can be run separately with other programs to create
a volatile market.

### Logging

Debug logging can be enabled with the `-debug` flag. Trace logging can be
enabled with the `-trace` flag.

### Network Simulator

LoadBot can use the [**toxiproxy** tool](https://github.com/Shopify/toxiproxy)
to simulate unfavorable network conditions.
To run network conditions, you must install and run the **toxiproxy** server
separately. Installation is easy and there is no configuration required.

The only network condition currently enabled is `latency500`, which adds 500
milliseconds of downstream latency to every wallet and server connection.

There are a number of conditions which can be imposed, and conditions can be
stacked.

|  condition |                                 description                                |
|:----------:|:--------------------------------------------------------------------------:|
| latency500 | add 500 ms of latency to downstream comms for wallets and server           |
| shaky500   | add 500 ms +/- 500ms of latency to downstream comms for wallets and server |
| slow100    | limit bandwidth on all server and wallet connections to 100 kB/s           |
| spotty20   | drop all connections every 20 seconds                                      |

`./loadbot -p sidestacker -latency500 -slow100`

### Development

A program is simply a wrapper for a series of `Trader`s. At the time of writing,
there are 3 types of `Trader`, the `pingPonger`, the `sideStacker`, and the
`sniper`. You can code up a new `Trader`, or create a new program from existing
`Trader`s.

The `Mantle` is a wrapper around the client `Core` that provides some
conveniences for implementing a `Trader`. For example, use the `Mantle`'s
`createWallet` method to create a wallet with balance monitoring and automatic
refill.

The `run` script is provided as a convenience to re-compile and run the LoadBot,
with output being both printed to the console and saved to a file named bot.log.

`./run sidestacker -debug`
