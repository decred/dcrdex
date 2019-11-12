module github.com/decred/dcrdex/client/comms

go 1.13

replace github.com/decred/dcrdex/server/comms/msgjson => ../../server/comms/msgjson

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrdex/server/comms/msgjson v0.0.0-20191017151723-32692e7cbf2a
	github.com/decred/slog v1.0.0
	github.com/gorilla/websocket v1.4.1
)
