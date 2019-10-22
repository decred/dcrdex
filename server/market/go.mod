module github.com/decred/dcrdex/server/market

go 1.13

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/asset => ../asset
	github.com/decred/dcrdex/server/book => ../book
	github.com/decred/dcrdex/server/comms => ../comms
	github.com/decred/dcrdex/server/comms/msgjson => ../comms/msgjson
	github.com/decred/dcrdex/server/market/types => ../market/types
	github.com/decred/dcrdex/server/matcher => ../matcher
	github.com/decred/dcrdex/server/order => ../order
	github.com/decred/dcrdex/server/order/test => ../order/test
)

require (
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrdex/server/account v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/asset v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/book v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/comms v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/comms/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/matcher v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/dcrdex/server/order v0.0.0-20191021140456-dfb4ce4aeb06
	github.com/decred/slog v1.0.0
)
