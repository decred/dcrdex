# DEX on Tatanka Mesh

Implementing a peer-to-peer decentralized exchange on Tatanka Mesh will require
substantial re-thinking of many existing DCRDEX protocol features. The primary
difference between DCRDEX and DEX on Tatanka Mesh is that Tatanka Mesh clients
take over the roll of maintaining order books.

### Protocol Basics

1) There are no pre-defined markets. A market is simply a broadcast channel,
which is a topic-subject pair, e.g. topic = "market", subject = "dcr_btc". Any
user can create a new market by simply subscribing to a non-existent channel.
This means we need to eliminate most market configuration.
    - Lot size will not be a constant. Instead, it will be chosen by the user
    based on their preferences for limiting fee exposure. For example, the user
    may set their fee exposure limit to 1%. This will hide any order (in their
    local view of the order book) which have a lot size that might result in fee
    losses > 1%. Only orders with compatible lot sizes can match. To ensure
    orders fill completely, I propose that lot sizes must be a power of 2,
    though other schemes are possibly conceivable.
    - Max fee rates are similarly set by the user as part of the order, and
    orders whose max fee rate falls below the fee rates provided by the mesh
    oracle feed will be ignored or unbooked.
    - If the mesh provides a fiat exchange rate feed, we can define the rate
    step and parcel size as a exchange-wide constant in terms of USD, and these
    values will be dynamically re-calculated for each market as rates change.
    - We could potentially do away with epoch-based matching, but we may want to
    keep it around, albeit in a different form. Our goal should be that clients
    are reacting appropriately by creating matches for valid offers. Clients
    can't just ignore offers based on their own arbitrary criteria. Ideally
    they would accept the first offers that are sent to them, though this would
    be very hard to enforce.
    - I've considered doing away with order funding validation. The only purpose
    of funding validation is to prevent empty wallets from spamming the market
    with orders they have no intention to fulfill. This would of course result
    in an account suspension, so such spamming cannot go on indefinitely. The
    bonding system also requires users to lock up funds before using the
    network, so there is still a barrier to such malicious behavior, though this
    is not as strong a filter as funding validation would provide. It's
    important to remember that the global worst-case scenario for
    atomic-swap-based exchange is a refund and fees, not lost funds, so a
    malicious actor's only incentive to spam would be to grief the network. On
    the other hand, the server could still provide funding validation services
    without participating in the market. Either way, we're going to do away with
    the necessity of split funding transactions. More on this later.


1) Markets are user-policed. Successes and counterparty failures are
self-reported.

1) Match proposals could be either public or private. Public match proposals
could be monitored by all market participants to potentially root out bad
actors, but private match proposals have obvious benefits as well. Either way
matches are self-reported as public broadcasts, so that clients can maintain
their order books.

1) Swap communications take place in encrypted tankagrams. Unless a swap failure
occurs which requires an audit, the server never knows any swap details.

1) Orders will have an expiration time that can be only so far in the future.
Clients can extend their expiration time as needed, but still only within a
relatively short window into the future. Expired orders are pruned.
