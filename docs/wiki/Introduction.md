# What is Bison Wallet?

Bison Wallet is a multi-coin wallet developed in concert with [DCRDEX](#dcrdex-protocol) 
and [Tatanka Mesh](#tatanka-mesh). Bison Wallet leverages state-of-the-art blockchain 
technology to bring more features and more privacy for your favorite assets. DCRDEX is 
built-in, as well as advanced trading features like market-making and arbitrage, directly
from your wallet.

Our goal is to find a balance of convenience and privacy that works for you,
while giving you access to advanced features most wallets ignore. For many
assets, we can cut out the middleman altogether and allow you to interact
directly with the blockchain network. This type of wallet is highly-resilient to
data collection and censorship.

We also focus on bringing advanced, asset-specific features for our wallets.
With Decred, you can use decentralized StakeShuffle mixing to further anonymize 
your funds, or stake your DCR and earn some block rewards. The Zcash wallet exposes 
unified addresses and shielded pools, and operates on a shielded-first principle 
that makes privacy effortless.

# DCRDEX Protocol

The [Decred Decentralized Exchange (DEX)](https://dex.decred.org/) is a system
that enables trustless exchange of different types of blockchain assets via a
familiar market-based API. DEX is a non-custodial solution for cross-chain
exchange based on [atomic swap technology](#atomic-swaps). DEX matches trading parties and
facilitates price discovery and the communication of swap details.


Some of the key features of the protocol are the following:

- **Fees** - no trading fees are collected and there is no superfluous token 
or blockchain that is used to monetize the project.
- **Fair** – orders are matched pseudorandomly within epochs to substantially 
reduce manipulative, abusive trading practices by high frequency trading that 
uses first-in-first-out matching.
- **Secure** – Server operators never take custody of client funds. Non-custodial 
exchange is accomplished using cross-chain atomic swaps.
- **Permissionless** – The simple client-server architecture makes it easy to set 
up new servers and clients and enhances censorship resistance.
- **No gatekeepers** – Projects can add support for their assets and run servers 
with the markets they require.
- **Verifiable volume** – volume and trade data can be externally verified against 
the corresponding blockchains and the atomic swaps that occur on-chain, preventing 
wash trading.
- **Private** – Know your customer (KYC) information is not required.
- **Transparent** – By performing exchanges on-chain and using cryptographic 
attestation, both clients and servers can be held accountable for malicious behavior.

# Tatanka Mesh

Tatanka Mesh (Tatanka, the mesh) is the evolution of DCRDEX. Where DCRDEX relies
on a central server for maintaining order books and policing trades, Tatanka is
a decentralized P2P protocol that enables a network of subscribers to
collectively perform these tasks. Here are the three critical services that
Tatanka Mesh provides.

- Enhance the ability for users to connect and to share data both publicly and privately
- Aggregate reputation data and monitor fidelity bonds. Tatanka can limit
access to users who earn a bad reputation
- Oracle services for fiat exchange rates and blockchain transaction fee rates

The mesh collects no fees for its services. Trades are performed using trustless
atomic swaps that exchange funds directly between wallets.

Going P2P empowers our users to trade directly, enhancing security,
censorship-resistance, privacy. and self-sovereignty.

# Atomic Swaps - review location

An Atomic Swap is a smart contract technology which makes possible to exchange coins from 
two different blockchains without having to trust any third party, for example a 
centralized exchange.

Atomic swaps involve each party paying into a contract transaction, one contract for each 
blockchain. The contracts contain an output that is spendable by either party, but the rules 
required for redemption are different for each party involved.

One party (called counterparty 1 or the initiator) generates a secret and pays the intended 
trade amount into a contract transaction. The contract output can be redeemed by the second 
party (called counterparty 2 or the participant) as long as the secret is known. 

If a period of time expires after the contract transaction has been 
mined but has not been redeemed by the participant, the contract output can be refunded 
back to the initiator’s wallet.

Check the
[dex specification](https://github.com/decred/dcrdex/blob/master/spec/atomic.mediawiki)
for more details about how atomic swaps work.

# Fees - review location


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


# Fidelity Bonds - review location

A fidelity bond are funds that are temporarily locked in an on-chain contract,
which is only redeemable by the user who posted the bond after a certain time. 
Fidelity bonds have replaced the original one-time server registration fee since
[DCRDEX v0.6.0](https://github.com/decred/dcrdex/releases/tag/v0.6.0) and act as an 
incentive for good conduct on the DEX.

Bonds can be revoked if an account engages in continued disruptive behavior, 
such as backing out on a swap. Revoked bonds can be re-activated with 
continued normal trading activity.

# History

Initially proposed by Jake Yocom-Piatt in the
[Decred blog](https://blog.decred.org/2018/06/05/A-New-Kind-of-DEX/) in 2018, 
DCRDEX development started in 2019 and has been fully funded by the Decred treasury.

**Project Timeline**

| Date    | Milestone                                                                                                               |
|---------|-------------------------------------------------------------------------------------------------------------------------|
| 2018-06 | [A New kind DEX](https://blog.decred.org/2018/06/05/A-New-Kind-of-DEX/) blog post published in the Decred blog.         |
| 2019-03 | [RFP: Decred Decentralized Exchange Infrastructure](https://proposals.decred.org/record/3360c14) proposal approved.     |
| 2019-06 | [Decentralized Exchange Specification Document](https://proposals.decred.org/record/94cc1ee) proposal approved.         |
| 2019-08 | [Decentralized Exchange Development](https://proposals.decred.org/record/ad972c3) proposal approved.                    |
| 2020-10 | [DCRDEX v0.1.0](https://github.com/decred/dcrdex/releases/tag/release-v0.1.0) MVP release.                              |
| 2021-01 | [Decred DEX Development Phase II](https://proposals.decred.org/record/cbd0f92) proposal approved.                       |
| 2021-05 | [DCRDEX v0.2.0](https://github.com/decred/dcrdex/releases/tag/v0.2.0) release.                                          |
| 2022-01 | [DCRDEX Phase 3 - Bonds, Decentralization, and Privacy](https://proposals.decred.org/record/3326c82) proposal approved. |
| 2022-02 | [DCRDEX v0.4.0](https://github.com/decred/dcrdex/releases/tag/v0.4.0) release.                                          |
| 2022-08 | [DCRDEX v0.5.0](https://github.com/decred/dcrdex/releases/tag/v0.5.0) release.                                          |
| 2023-01 | [DCRDEX integration on Umbrel](https://proposals.decred.org/record/8d83046) proposal approved.                          |
| 2023-03 | [Decred DEX - Desktop App and Packaging](https://proposals.decred.org/record/ae7c4fe) proposal approved.                |
| 2023-03 | [Decred DEX - Client Development](https://proposals.decred.org/record/ca6b749) proposal approved.                       |
| 2023-03 | [Decred DEX - Market Maker and Arbitrage Bot Development](https://dcrdata.org/proposals) proposal approved.             |
| 2023-04 | [DCRDEX v0.6.0](https://github.com/decred/dcrdex/releases/tag/v0.6.0) release.                                          |
| 2023-06 | [DCRDEX Mesh Beginnings and Bonds Evolution](https://proposals.decred.org/record/4d2324b) proposal approved.            |
| 2024-04 | [DCRDEX Monero Stage 1](https://proposals.decred.org/record/fa0ea64) proposal approved.                                 |
| 2024-09 | [DCRDEX v1.0.0](https://github.com/decred/dcrdex/releases/tag/v0.6.0) release.                                          |
| 2024-10 | [DCRDEX Development Phase 5.5](https://proposals.decred.org/record/0d23788) proposal approved.                          |
