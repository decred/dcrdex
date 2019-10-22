module github.com/decred/dcrdex/server/swap

go 1.13

require (
	github.com/decred/dcrdex/server/account v0.0.0-20191016143014-620c34d707f0
	github.com/decred/dcrdex/server/asset v0.0.0-20191016143014-620c34d707f0
	github.com/decred/dcrdex/server/comms v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/comms/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/matcher v0.0.0-20191016143014-620c34d707f0
	github.com/decred/dcrdex/server/order v0.0.0-20191015161642-e29ca1b594a7
	github.com/decred/slog v1.0.0
)

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/asset => ../asset
	github.com/decred/dcrdex/server/comms => ../comms
	github.com/decred/dcrdex/server/comms/msgjson => ../comms/msgjson
	github.com/decred/dcrdex/server/matcher => ../matcher
	github.com/decred/dcrdex/server/order => ../order
)
