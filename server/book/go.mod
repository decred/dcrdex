module github.com/decred/dcrdex/server/book

go 1.13

replace (
	github.com/decred/dcrdex/dex/order => ../../dex/order
	github.com/decred/dcrdex/server/account => ../account
)

require (
	github.com/decred/dcrdex/dex/order v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/account v0.0.0-20190907171020-c48f5b8f4bbc
	github.com/decred/slog v1.0.0
)
