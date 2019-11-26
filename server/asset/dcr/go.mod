module github.com/decred/dcrdex/server/asset/dcr

go 1.13

require (
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.1.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.0-20190905151318-0781162661f7
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0-20190905151318-0781162661f7
	github.com/decred/dcrd/dcrutil/v2 v2.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrd/txscript/v2 v2.0.0
	github.com/decred/dcrd/wire v1.2.0
	github.com/decred/dcrdex/server/asset v0.0.0-00010101000000-000000000000
	github.com/decred/slog v1.0.0
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/golang/protobuf v1.3.2 // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	github.com/jessevdk/go-flags v1.4.0
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

replace github.com/decred/dcrdex/server/asset => ../
