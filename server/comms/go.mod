module github.com/decred/dcrdex/server/comms

go 1.12

replace (
	github.com/decred/dcrdex/server/comms/msgjson => ./msgjson
	github.com/decred/dcrdex/server/comms/rpc => ./rpc
)

require (
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrdex/server/comms/msgjson v0.0.0-00010101000000-000000000000
	github.com/decred/dcrdex/server/comms/rpc v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/go-chi/chi v4.0.2+incompatible
	github.com/gorilla/websocket v1.4.1
)
