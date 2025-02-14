# Trading Sequence

1. User initiates an order by specifying a desired quantity that they want to
buy or sell and an acceptable rate limit. This is the `DesiredTrade`.
2. The backend receives the `DesiredTrade` and checks whether there are any
orders on the order book to satisfy the trade using `MatchBook`. This generates
a set of potential matches (`[]*MatchProposal`), but there might be some
remaining quantity that we couldn't fulfill with the existing standing orders,
the `remain`.
3. Any `[]*MatchProposal` from `MatchBook` will generate match requests to
the owners of the matched standing orders.
4. If there is `remain`, we will generate our own standing order and broadcast
it to all market subscribers.