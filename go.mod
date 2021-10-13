module decred.org/dcrdex

go 1.16

require (
	decred.org/dcrwallet/v2 v2.0.0-20210913145543-714c2f555f04
	github.com/btcsuite/btcd v0.22.0-beta.0.20210803133449-f5a1fb9965e4
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.3-0.20210527170813-e2ba6805a890 // note: hoists btcd's own require of btcutil
	github.com/btcsuite/btcutil/psbt v1.0.3-0.20201208143702-a53e38424cce
	github.com/btcsuite/btcwallet v0.12.0
	github.com/btcsuite/btcwallet/wallet/txauthor v1.1.0
	github.com/btcsuite/btcwallet/wallet/txsizes v1.1.0 // indirect
	github.com/btcsuite/btcwallet/walletdb v1.4.0
	github.com/btcsuite/btcwallet/wtxmgr v1.3.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/certgen v1.1.2-0.20210901152745-8830d9c9cdba
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.0.1-0.20210901152745-8830d9c9cdba
	github.com/decred/dcrd/crypto/blake256 v1.0.1-0.20210901152745-8830d9c9cdba
	github.com/decred/dcrd/database/v3 v3.0.0-20210914193033-2efb9bda71fe // indirect
	github.com/decred/dcrd/dcrec v1.0.1-0.20210901152745-8830d9c9cdba
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2-0.20210715032435-c9521b468f95
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.0
	github.com/decred/dcrd/dcrjson/v4 v4.0.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/gcs/v3 v3.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/hdkeychain/v3 v3.0.1-0.20210901152745-8830d9c9cdba
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/rpcclient/v7 v7.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/txscript/v4 v4.0.0-20210914193033-2efb9bda71fe
	github.com/decred/dcrd/wire v1.4.1-0.20210901152745-8830d9c9cdba
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/ethereum/go-ethereum v1.10.6
	github.com/gcash/bchd v0.17.2-0.20201218180520-5708823e0e99
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000
	github.com/go-chi/chi/v5 v5.0.1
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.10.3
	github.com/lightninglabs/neutrino v0.12.1
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d
	golang.org/x/text v0.3.6
	golang.org/x/time v0.0.0-20201208040808-7e3f01d25324
	gopkg.in/ini.v1 v1.55.0
)
