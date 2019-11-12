module github.com/decred/dcrdex/server/db

go 1.13

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/chaincfg v1.2.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain v1.1.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrdex/dex v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/dex/order v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/account v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/asset v0.0.0-20191112195536-93fbcfa8146e
	github.com/decred/dcrdex/server/market v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/slog v1.0.0
	github.com/lib/pq v1.2.0
)

replace (
	github.com/decred/dcrdex/dex => ../../dex
	github.com/decred/dcrdex/dex/msgjson => ../../dex/msgjson
	github.com/decred/dcrdex/dex/order => ../../dex/order
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/asset => ../asset
	github.com/decred/dcrdex/server/comms => ../comms
	github.com/decred/dcrdex/server/market => ../market
)
