module decred.org/dcrdex/client/cmd/dexcctl

go 1.13

replace (
	decred.org/dcrdex => ../../../
	github.com/ltcsuite/ltcutil => github.com/ltcsuite/ltcutil v0.0.0-20190507133322-23cdfa9fcc3d
)

require (
	decred.org/dcrdex v0.0.0-00010101000000-000000000000
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/go-socks v1.1.0
	github.com/jessevdk/go-flags v1.4.0
	golang.org/x/crypto v0.0.0-20191122220453-ac88ee75c92c
)
