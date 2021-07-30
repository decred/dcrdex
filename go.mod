module decred.org/dcrdex

go 1.15

replace decred.org/dcrdex/internal/eth/reentryattack => ./internal/eth/reentryattack

require (
	decred.org/dcrwallet/v2 v2.0.0-20210714172147-8815838443cd
	github.com/btcsuite/btcd v0.20.1-beta.0.20200615134404-e4f59022a387
	github.com/btcsuite/btcutil v1.0.2
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210715032435-c9521b468f95
	github.com/decred/dcrd/certgen v1.1.2-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/dcrec v1.0.1-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.1.1-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210715032435-c9521b468f95
	github.com/decred/dcrd/hdkeychain/v3 v3.0.1-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0-20210715032435-c9521b468f95
	github.com/decred/dcrd/rpcclient/v7 v7.0.0-20210715032435-c9521b468f95
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210715032435-c9521b468f95
	github.com/decred/dcrd/wire v1.4.1-0.20210715032435-c9521b468f95
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.1.0
	github.com/ethereum/go-ethereum v1.10.6
	github.com/gcash/bchd v0.17.2-0.20201218180520-5708823e0e99
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000
	github.com/go-chi/chi/v5 v5.0.1
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.2.0
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d
	golang.org/x/time v0.0.0-20201208040808-7e3f01d25324
	gopkg.in/ini.v1 v1.55.0
)
