module github.com/decred/dcrdex/server/book

go 1.12

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/order => ../order
)

require (
	github.com/decred/dcrdex/server/account v0.0.0-20190907171020-c48f5b8f4bbc
	github.com/decred/dcrdex/server/order v0.0.0-20191015161642-e29ca1b594a7
	github.com/decred/slog v1.0.0
)
