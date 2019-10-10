module github.com/decred/dcrdex/server/db

go 1.13

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/chaincfg v1.5.2
	github.com/decred/dcrdex/server/account v0.0.0-20191007225918-c21cff4ecc64
	github.com/decred/dcrdex/server/asset v0.0.0-20191007225918-c21cff4ecc64
	github.com/decred/dcrdex/server/market v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/order v0.0.0-20191007225918-c21cff4ecc64
	github.com/decred/slog v1.0.0
	github.com/lib/pq v1.2.0
)

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/asset => ../asset
	github.com/decred/dcrdex/server/market => ../market
	github.com/decred/dcrdex/server/order => ../order
)
