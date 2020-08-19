module decred.org/dcrdex/server/cmd/dcrdex

go 1.14

replace decred.org/dcrdex => ../../..

require (
	decred.org/dcrdex v0.0.0-00010101000000-000000000000
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
)
