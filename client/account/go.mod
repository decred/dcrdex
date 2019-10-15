module github.com/decred/dcrdex/client/account

go 1.13

replace github.com/decred/dcrdex/server/account => ../../server/account

require (
	github.com/decred/dcrd/dcrec/secp256k1 v1.0.3
	github.com/decred/dcrdex/server/account v0.0.0-20191014151634-318621ff1b98
)
