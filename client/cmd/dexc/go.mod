module decred.org/dcrdex/client/cmd/dexc

go 1.13

replace (
	decred.org/dcrdex => ../../../
	github.com/decred/dcrd/rpcclient/v5 => ../../../../rpcclient-temp
	github.com/ltcsuite/ltcutil => github.com/ltcsuite/ltcutil v0.0.0-20190507133322-23cdfa9fcc3d
)

require (
	decred.org/dcrdex v0.0.0-00010101000000-000000000000
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/slog v1.0.0
	github.com/gdamore/tcell v1.3.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/rivo/tview v0.0.0-20191129065140-82b05c9fb329
)
