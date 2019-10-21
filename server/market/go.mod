module github.com/decred/dcrdex/server/market

go 1.13

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/asset => ../asset
	github.com/decred/dcrdex/server/book => ../book
	github.com/decred/dcrdex/server/matcher => ../matcher
	github.com/decred/dcrdex/server/order => ../order
)

require (
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3 // indirect
	github.com/decred/dcrdex/server/account v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/asset v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/book v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/matcher v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/order v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/slog v1.0.0
)
