# dcrdex - The Decred DEX

dcrdex is the repository for the specification and source code of the Decred
Distributed Exchange (DEX).

## Specification

The DEX [specification](spec/README.mediawiki) was drafted following stakeholder
approval of the [specification proposal](https://proposals.decred.org/proposals/a4f2a91c8589b2e5a955798d6c0f4f77f2eec13b62063c5f4102c21913dcaf32).

## Source

The source code for the DEX server and client are being developed according to
the specification. This undertaking was approved via a second DEX [development proposal](https://proposals.decred.org/proposals/417607aaedff2942ff3701cdb4eff76637eca4ed7f7ba816e5c0bd2e971602e1).

### Repository Organization

The proposed organization of the source code in the repository is as follows.

```
dcrdex
├── client                # client libraries
│   ├── ...               # user, ws/comms, orders, etc.
│   ├── cmd
│   │   ├── dexclient     # CLI app
│   │   └── dexwebclient  # e.g. http://127.0.0.1:7345
│   └── docs
├── README.md
├── server
│   ├── account        # account/user manager
│   │   └── pki        # possibly separate for sig. and verif. functions
│   ├── archivist      # persistent storage
│   │   ├── kv         # e.g. bbolt, badger
│   │   └── sql        # e.g. postgresql
│   ├── asset          # i.e. interface.go
│   │   ├── btc        # implement the interface
│   │   └── dcr
│   ├── book           # order book manager
│   ├── cmd
│   │   └── dcrdex     # main()
│   ├── comms          # communications hub, ideally abstract transport
│   │   ├── jsonrpc    # primarily the types and things the client uses too
│   │   └── ws         # websocket (perhaps other transports)
│   ├── dex
│   │   ├── admin      # administrative tools and portal (may need RPC server too)
│   │   └── controller # controller for multiple markets, users, api, comms, etc.
│   ├── docs
│   ├── htttpapi       # HTTP API
│   ├── market         # market manager
│   │   └── order      # the ubiquitous order type
│   ├── matcher        # order matching engine
│   └── swap           # the swap executor/coordinator
├── spec
└── testing
    └── harness.sh    # tmux testing harness 
```

Note that `dcrdex` is the name of both the repository and the DEX server process
built by the `{reporoot}/cmd/dcrdex` module.
