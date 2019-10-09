module github.com/decred/dcrdex/server/market

go 1.13

replace (
	github.com/decred/dcrdex/server/account => ../account
	github.com/decred/dcrdex/server/book => ../book
	github.com/decred/dcrdex/server/matcher => ../matcher
	github.com/decred/dcrdex/server/order => ../order
)

require (
	github.com/decred/dcrdex/server/account v0.0.0-20190907171020-c48f5b8f4bbc
	github.com/decred/dcrdex/server/book v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/matcher v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/order v0.0.0-20191015161642-e29ca1b594a7
	github.com/decred/slog v1.0.0
)
