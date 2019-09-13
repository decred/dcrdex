module github.com/decred/dcrdex/server/book

go 1.12

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/order => ../order
)

require (
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrdex/server/account v0.0.0-20190907171020-c48f5b8f4bbc
	github.com/decred/dcrdex/server/order v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
)
