module github.com/decred/dcrdex/server/asset/btc

go 1.12

require (
	github.com/btcsuite/btcd v0.0.0-20190824003749-130ea5bddde3
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrdex/server/asset v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
)

replace github.com/decred/dcrdex/server/asset => ../
