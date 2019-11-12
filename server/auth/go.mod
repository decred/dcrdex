module github.com/decred/server/auth

go 1.13

require (
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrdex/dex/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/dex/order v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/account v0.0.0-20191007225918-c21cff4ecc64
	github.com/decred/dcrdex/server/comms v0.0.0-20191017151723-32692e7cbf2a
	github.com/decred/slog v1.0.0
)

replace (
	github.com/decred/dcrdex/dex/msgjson => ../../dex/msgjson
	github.com/decred/dcrdex/dex/order => ../../dex/order
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/comms => ../comms
)
