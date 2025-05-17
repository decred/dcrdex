<a id="top"/>

<img src="./images/logo_wide_v2.svg" alt="DCRDEX" width="456">

---

This wiki provides documentation for Bison Wallet and DCRDEX users and developers.

For a step-by-step guide of the download, installation and configuration of Bison Wallet,
see the [Getting Started](Getting-Started) section.

If you're a developer and interested in contributing, jump to the
 [Development and Contributing](Development-and-Contributing) section.

# What is Bison Wallet?

Bison Wallet is a multi-coin wallet developed in concert with [DCRDEX](#what-is-dcrdex)
and [Tatanka Mesh](#tatanka-mesh). Bison Wallet leverages state-of-the-art blockchain
technology to bring more features and more privacy for your favorite assets. DCRDEX is
built-in and has advanced trading features like market-making and arbitrage with funds
directlyfrom your wallet.

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

# What is DCRDEX?

The [Decred Decentralized Exchange (DEX)](https://dex.decred.org/) is a system
that enables trustless exchange of different types of blockchain assets via a
familiar market-based API. DEX is a non-custodial solution for cross-chain
exchange based on atomic swap technology. DEX matches trading parties and
facilitates price discovery and the communication of swap details.

Some key features of the protocol are:

- **Fees** - No trading fees are collected and there is no superfluous token
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

# Supported Assets

Most users will use the native wallets that are already built into Bison Wallet.
Depending on the asset, you may be able to choose from: (1) a native
wallet, (2) an external full node wallet, or (3) an Electrum-based wallet.
Consult the following table for a summary of wallet support. If there is a
checkmark in the "native" column, no external software is required.

| Coin         | native | full node                                                   | Electrum                                                      | notes                                                                                             |
|--------------|--------|-------------------------------------------------------------|---------------------------------------------------------------|---------------------------------------------------------------------------------------------------|
| Bitcoin      | ✓      | [v27.0](https://bitcoincore.org/en/download/)               | [v4.5.5](https://electrum.org/)                               |                                                                                                   |
| Decred       | ✓      | [v2.0.3](https://github.com/decred/decred-release/releases) | x                                                             |                                                                                                   |
| Ethereum     | ✓      | geth IPC/http/ws                                            | N/A                                                           | see [RPC Providers for EVM-Compatible Networks](Wallet#rpc-providers-for-evm-compatible-networks) |
| Polygon      | ✓      | bor IPC/http/ws                                             | N/A                                                           | see [RPC Providers for EVM-Compatible Networks](Wallet#rpc-providers-for-evm-compatible-networks) |
| Litecoin     | ✓      | [v0.21.2.1](https://litecoin.org/)                          | [v4.2.2](https://electrum-ltc.org/)                           |                                                                                                   |
| Bitcoin Cash | ✓      | [v27.0.0](https://bitcoincashnode.org/)                     | x                                                             | use only Bitcoin Cash Node for full node                                                          |
| Dogecoin     | x      | [v1.14.7.0](https://dogecoin.com/)                          | x                                                             |                                                                                                   |
| Zcash        | x      | [v5.4.2](https://z.cash/download/)                          | x                                                             |                                                                                                   |
| Dash         | x      | [v20.1.1](https://github.com/dashpay/dash/releases)         | x                                                             |                                                                                                   |
| Firo         | x      | [v0.14.14.1](https://github.com/firoorg/firo/releases)      | [v4.1.5.5](https://github.com/firoorg/electrum-firo/releases) |                                                                                                   |

# Project History

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

---

[⤴ Back to Top](#top)
