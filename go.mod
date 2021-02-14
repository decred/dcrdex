module decred.org/dcrdex

go 1.14

require (
	decred.org/dcrwallet/v2 v2.0.0-20210129212301-69cae76621d1
	github.com/btcsuite/btcd v0.20.1-beta.0.20200615134404-e4f59022a387
	github.com/btcsuite/btcutil v1.0.2
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210129220646-5834ce08e290
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.1
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.1.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210129220646-5834ce08e290
	github.com/decred/dcrd/gcs/v3 v3.0.0-20210129195202-a4265d63b619
	github.com/decred/dcrd/hdkeychain/v3 v3.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0-20210129220646-5834ce08e290
	github.com/decred/dcrd/rpcclient/v7 v7.0.0-20210129220646-5834ce08e290
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210129220646-5834ce08e290
	github.com/decred/dcrd/wire v1.4.0
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.1.0
	github.com/go-chi/chi v1.5.1
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.2.0
	github.com/smartystreets/goconvey v1.6.4 // indirect
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a
	golang.org/x/sync v0.0.0-20200625203802-6e8e738ad208
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	gopkg.in/ini.v1 v1.55.0
)

replace decred.org/dcrwallet/v2 => github.com/itswisdomagain/dcrwallet/v2 v2.0.0-20210130000200-ae1a2388d912
