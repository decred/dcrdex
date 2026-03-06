<a id="top"></a>

_Last updated for Bison Wallet v1.0.6._

## Contents

- [Overview](#overview)
- [Bot Types](#bot-types)
  - [Basic Market Maker](#basic-market-maker)
  - [Market Maker + Arbitrage](#market-maker--arbitrage)
  - [Simple Arbitrage](#simple-arbitrage)
- [Creating a Bot](#creating-a-bot)
  - [Selecting a Market](#selecting-a-market)
  - [Selecting a Bot Type](#selecting-a-bot-type)
  - [Configuration](#configuration)
  - [Balance Allocation](#balance-allocation)
- [Managing Bots](#managing-bots)
  - [Starting and Stopping](#starting-and-stopping)
  - [Run Logs and History](#run-logs-and-history)
- [CEX Integration](#cex-integration)
- [Key Concepts](#key-concepts)
  - [Gap Strategy](#gap-strategy)
  - [Oracles](#oracles)
  - [Placements](#placements)
  - [Profit Threshold](#profit-threshold)

This page is part of the [Using Bison Wallet](Using-Bison-Wallet) guide. It assumes you have already
set up a Bison Wallet using the [Getting Started Guide](Getting-Started).

# Overview

Bison Wallet includes built-in market making bots that can automatically maintain
orders on both sides of a market's order book. Market makers provide liquidity to
markets, limit slippage for other traders, and can potentially earn profits from
the spread between buy and sell prices.

The Market Making view can be accessed from the header navigation. From there you
can create new bots, configure them, and monitor running bots.

# Bot Types

Bison Wallet offers three types of market making bots, each with different strategies
and requirements.

## Basic Market Maker

The Basic Market Maker places buy and sell orders on a DCRDEX market using only your
DEX wallets. It does not require a centralized exchange (CEX) account. The bot
determines order prices based on a configurable [gap strategy](#gap-strategy) and
can optionally use [oracles](#oracles) to help determine rates.

This is the simplest bot type and is a good starting point for users who want to
provide liquidity on DCRDEX markets.

## Market Maker + Arbitrage

The Market Maker + Arbitrage bot combines market making on DCRDEX with arbitrage
against a centralized exchange. It places orders on the DEX order book and
simultaneously watches a linked CEX market. When a DEX order is filled, the bot
can execute a corresponding trade on the CEX to capture the price difference.

This bot type requires a [CEX account](#cex-integration) with API credentials.
It offers more sophisticated price discovery and can be more profitable in active
markets, but carries additional complexity and risk.

## Simple Arbitrage

The Simple Arbitrage bot focuses purely on arbitrage between DCRDEX and a centralized
exchange. Unlike the other bot types, it does not maintain standing orders on the
DEX order book. Instead, it monitors both markets and places trades only when a
profitable arbitrage opportunity is detected.

This bot type also requires a [CEX account](#cex-integration) with API credentials.
You configure a minimum [profit threshold](#profit-threshold), and the bot will only
execute trades that meet or exceed that threshold.

# Creating a Bot

## Selecting a Market

To create a new bot, navigate to the Market Making view and click the
`Add a Market Maker Bot` button. You will be prompted to select a DCRDEX server
and market pair for the bot to operate on.

You must have wallets created for both assets in the selected market.

## Selecting a Bot Type

After selecting a market, choose your bot type. The available options depend on
whether you have configured a CEX account:

- **Basic Market Maker** is always available.
- **Market Maker + Arbitrage** and **Simple Arbitrage** require a
  [configured CEX](#cex-integration).

## Configuration

Each bot type has its own configuration options. Common settings include:

- **Buy/Sell Placements** - Define the number of lots and the spread for each order
  placement level. You can configure multiple placement levels to create depth on
  the order book. See [Placements](#placements) for details.
- **Gap Strategy** - How the bot calculates the distance between buy and sell orders.
  See [Gap Strategy](#gap-strategy).
- **Oracles** - Whether to use rates from external exchanges to help determine
  order placement rates. See [Oracles](#oracles).
- **Drift Tolerance** - How far orders can drift from their ideal price before
  the bot replaces them.
- **Order Persistence** (arbitrage bots) - How long CEX orders that are not
  immediately filled are allowed to remain booked.
- **Profit Threshold** (arbitrage bots) - Minimum profit required before the bot
  will execute a trade. See [Profit Threshold](#profit-threshold).

## Balance Allocation

Before starting a bot, you must allocate funds from your wallets. The allocation
screen shows how much of your available balance will be dedicated to the bot for
placing orders. Funds allocated to a bot are reserved for its use and are not
available for manual trading while the bot is running.

For arbitrage bots, you will also allocate funds on the linked CEX.

# Managing Bots

## Starting and Stopping

From the Market Making view, each configured bot shows its current status. You can:

- **Start** a bot to begin placing orders.
- **Pause** a bot to temporarily stop its activity. Existing orders will be
  cancelled.
- **Retire** a bot to permanently remove it.

When a bot is running, it will automatically manage orders based on its configuration,
replacing them as market conditions change. The bot requires Bison Wallet to remain
running - if you shut down the application, all bots will stop.

You can adjust a running bot's configuration and balance allocation without
stopping it.

## Run Logs and History

Each bot run is recorded with detailed logs. You can view:

- **Run Logs** - Detailed logs for the current or previous bot runs, showing
  individual orders placed and filled.
- **Previous Market Making Runs** - A history of all past runs for a bot, including
  profit/loss summaries, order counts, and duration.

# CEX Integration

To use the arbitrage bot types, you must configure API credentials for a supported
centralized exchange. Currently supported exchanges are:

- **Binance**
- **Binance US**
- **Coinbase**
- **Bitget**
- **MEXC**

CEX credentials can be configured from the Market Making settings. You will need
to provide your API key and secret from the exchange. Bitget also requires an
API passphrase. Make sure your API key has trading permissions enabled.

> [!NOTE]
> Your API credentials are stored locally in your Bison Wallet data directory.
> They are never sent to the DCRDEX server.

**Rebalancing** - Arbitrage bots can optionally transfer funds between your
DEX wallet and CEX account automatically to maintain the required balances on
each side. You can configure this in the rebalance settings, choosing between:

- **External Transfers** - The bot will make actual deposits and withdrawals
  between the DEX wallet and the CEX.
- **Internal Only** - The bot will use any available funds in the wallet to
  simulate a transfer (adjusting the bot's internal balance tracking) without
  making actual transfers.

# Key Concepts

## Gap Strategy

The gap strategy determines how the bot calculates the spread between buy and sell
orders. The available strategies are:

- **Multiplier** - The gap is set to a specified multiple of the break-even spread
  (the minimum spread at which a buy-sell combo produces profit).
- **Percent** - The gap is set to a specified percentage of the mid-gap price.
- **Percent Plus** - The gap starts at the break-even spread and then adds a
  specified percentage of the mid-gap price.
- **Absolute** - The gap is set to a fixed rate difference between buy and sell
  orders.
- **Absolute Plus** - The gap starts at the break-even spread and then adds a
  fixed rate difference.

The break-even spread accounts for on-chain transaction fees so that matched
orders are always profitable before applying your configured gap.

## Oracles

Oracles are external exchange rates fetched from other exchanges. When enabled,
the bot uses these rates in combination with the DEX mid-gap rate to determine
order placement prices. This helps prevent manipulation on thin DEX markets where
a single large order could significantly shift the mid-gap rate.

You can configure:

- **Oracle Weight** - How much influence oracle rates have relative to the DEX
  mid-gap rate. Use a higher weight when DEX markets are thin.
- **Oracle Bias** - An adjustment applied to the oracle rates before using them.
- **Empty Market Rate** - A fallback rate to use if the DEX market has no orders
  and no oracles are available.

## Placements

Placements define the orders that the bot will maintain on the order book. Each
placement specifies:

- **Lots** - The number of lots for this order.
- **Gap Factor** - The distance from the basis price for this placement, interpreted
  according to the selected [gap strategy](#gap-strategy).

You can configure multiple placements on each side (buy and sell) to create
depth on the order book at different price levels. Placements are prioritized
in order - if the bot does not have enough allocated funds for all placements,
later placements may be skipped.

## Profit Threshold

For arbitrage bots, the profit threshold is the minimum profit percentage required
before the bot will execute a trade. This ensures that the bot only takes trades
where the price difference between the DEX and CEX is large enough to be worthwhile
after accounting for fees.

---

Next Section: [Managing your DCRDEX Accounts](Managing-your-DCRDEX-Accounts)

[⤴ Back to Top](#top)
