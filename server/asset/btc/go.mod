module github.com/decred/dcrdex/server/asset/btc

go 1.13

require (
	github.com/btcsuite/btcd v0.0.0-20190824003749-130ea5bddde3
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/decred/dcrdex/server/asset v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/jessevdk/go-flags v1.4.0
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8 // indirect
)

replace github.com/decred/dcrdex/server/asset => ../
