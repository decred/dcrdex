module github.com/decred/dcrdex/server/comms

go 1.13

replace github.com/decred/dcrdex/dex/msgjson => ../../dex/msgjson

require (
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrdex/dex/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/go-chi/chi v4.0.2+incompatible
	github.com/gorilla/websocket v1.4.1
	golang.org/x/net v0.0.0-20191125084936-ffdde1057850 // indirect
)
