module github.com/decred/dcrdex/server/asset/ltc

go 1.13

replace (
	github.com/decred/dcrdex/server/asset => ../
	github.com/decred/dcrdex/server/asset/btc => ../btc
)

require (
	github.com/btcsuite/btcd v0.0.0-20190824003749-130ea5bddde3
	github.com/decred/dcrdex/server/asset v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/asset/btc v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/ltcsuite/ltcd v0.0.0-20190519120615-e27ee083f08f
)
