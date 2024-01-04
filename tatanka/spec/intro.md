# Introduction to Tatanka Mesh

Tatanka Mesh (TM, or just "the mesh") is a multi-blockchain mesh network offering a suite
of services for clients. This initial specification version may reference the
"toy mesh", which is a proof-of-concept implementation of Tatanka Mesh. 

### Services

1) Reputation services. The mesh will handle client bonds and reputation in
a fashion similar to a dcrdex server. Bonds provide network access, increased
network usage limits, and can offset penalties for high-volume users. Clients
self-report successes and failures, which are used to build a reputation score.
If a clients reputation score drops too low, they can have their usage limited
or outright suspended.

1) Subscription channels, or "apps", perform a function similar to a chat room.
Clients subscribe to "topics", which can also have more narrow "subjects" to
which the client might subscribe. Subscription channels can be used to construct
various applications, such as a peer-to-peer decentralized exchange. The mesh
does not prescribe protocols for communications on a topic or subject. Such
protocols are defined by the client software.

1) Client message routing. The mesh can route messages from client to client,
even when those clients are connected to different mesh nodes. Client messaging
is end-to-end encrypted, so the mesh network itself cannot read the contents.
The encrypted messaging primitive is called a "tankagram". Clients connected
to the same subscription channel might send one another tankagrams to, for
instance, communicate details about an ongoing atomic swap.

1) Oracle services. The mesh will coordinate distribution of network transaction
fee rates that might be used as part of validation for app actions. The mesh
could potentially distribute fiat exchange rates as well, although such
functionality is not implemented in the toy mesh. See also the
[Outstanding Questions](#oustanding_questions) section.

### Other Important Features

- Although the toy mesh uses WebSockets for communications, there is no
prescribed transport protocol, and other communication backends are planned,
especially BisonRelay. If clients are using compatible protocols with the
requisite features (e.g. BisonRelay), client communications (tankagrams) can be
taken out-of-band.

- Version 0 of Tatanka Mesh will be whitelist-only. This means that the mesh
network itself is a network of trusted nodes. By using a whitelist, we can
initially forego implementation of what will inevitably be a complicated mesh
node reputation system.

- Tatanka Mesh does not maintain a global state. Mesh nodes need not agree on
client reputation, and the ability to synchronize reputation is extremely
limited. See the [Outstanding Questions](#oustanding_questions) section for more
discussion.

### Why?

The mesh architecture is designed in this way specifically to move all DEX
operations to the clients. In the litany of proposed U.S. legislation aimed at
regulating cryptocurrencies, even the most favorable bills indicate that
operating an order book would compel the server operator to collect
know-your-customer (KYC) and anti-money-laundering (AML) information from users.
This is going to be the case regardless of the (non-custodial, trustless) nature
of the underlying exchange protocol. While we believe that the trustless nature
of atomic swaps should preclude an order book operator from these regulations,
that is simply not going to be the case. As such, we're moving towards a fully
P2P model. Tatanka Mesh does not even know what a market is.

### Outstanding Questions<a name="oustanding_questions">

- Clients will self-report successes and counter-party failures, but who is in
charge of auditing these reports before inclusion into reputation? For example,
the mesh does not know what an atomic swap is, so how can a mesh node validate a
reported swap failure? Two possiblities are...
    1) The server could audit the reports, but do so in terms of blockchain
    primitives that exist outside of the context of particular applications. For
    example, the server could audit reports in terms of "HTLC pairs". The
    important thing is that the server does not even appear to participate in
    any way in the maintenance of an exchange market.
    1) The server could outsource auditing to clients, and accept the majority
    result. This might require the server to expose some blockchain functionality
    to clients who may not have e.g. txindex enabled on their UTXO-based
    blockchain. This also creates perhaps more questions than it answers. What
    if there aren't enough clients connected? How do we account for the report
    while we are still waiting for client audits?

- Since the mesh network does not maintain a global state, what happens if a
client is suspended on one node but not another? For the toy mesh, reputation
is only checked by the node(s) to which the client is directly connected. This
simple system might work for a whitelisted network, but is not a long-term
solution.

### More Info

[Introduction to Mesh DEX](meshdex.md)