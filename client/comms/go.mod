module github.com/decred/dcrdex/client/comms

go 1.13

replace github.com/decred/dcrdex/dex/msgjson => ../../dex/msgjson

require (
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrdex/dex/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/gorilla/websocket v1.4.1
)
