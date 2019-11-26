module decred.org/dcrdex

go 1.13

require (
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types v1.0.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.0.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrd/rpcclient/v5 v5.0.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.3.0
	github.com/decred/dcrwallet/wallet/v3 v3.0.5
	github.com/decred/slog v1.0.0
	github.com/go-chi/chi v4.0.2+incompatible
	github.com/gorilla/websocket v1.4.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/lib/pq v1.2.0
	github.com/ltcsuite/ltcd v0.0.0-20190519120615-e27ee083f08f
	golang.org/x/crypto v0.0.0-20191122220453-ac88ee75c92c
)

replace github.com/ltcsuite/ltcutil => github.com/ltcsuite/ltcutil v0.0.0-20190507133322-23cdfa9fcc3d
