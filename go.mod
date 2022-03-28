module decred.org/dcrdex

go 1.17

require (
	decred.org/dcrwallet/v2 v2.0.0-20211206163037-9537363becbb
	github.com/btcsuite/btcd v0.22.0-beta.0.20211026140004-31791ba4dc6e
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.3-0.20210527170813-e2ba6805a890 // note: hoists btcd's own require of btcutil
	github.com/btcsuite/btcutil/psbt v1.0.3-0.20201208143702-a53e38424cce
	github.com/btcsuite/btcwallet v0.12.0
	github.com/btcsuite/btcwallet/wallet/txauthor v1.1.0
	github.com/btcsuite/btcwallet/walletdb v1.4.0
	github.com/btcsuite/btcwallet/wtxmgr v1.3.0
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/blockchain/stake/v4 v4.0.0
	github.com/decred/dcrd/blockchain/v4 v4.0.0
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.3
	github.com/decred/dcrd/chaincfg/v3 v3.1.1
	github.com/decred/dcrd/crypto/blake256 v1.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.2
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1
	github.com/decred/dcrd/dcrjson/v4 v4.0.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.0
	github.com/decred/dcrd/gcs/v3 v3.0.0
	github.com/decred/dcrd/hdkeychain/v3 v3.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v3 v3.0.0
	github.com/decred/dcrd/rpcclient/v7 v7.0.0
	github.com/decred/dcrd/txscript/v4 v4.0.0
	github.com/decred/dcrd/wire v1.5.0
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/ethereum/go-ethereum v1.10.16
	github.com/gcash/bchd v0.17.2-0.20201218180520-5708823e0e99
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000
	github.com/go-chi/chi/v5 v5.0.1
	github.com/gorilla/websocket v1.4.2
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.10.3
	github.com/lightninglabs/neutrino v0.13.1-0.20211214231330-53b628ce1756
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	go.etcd.io/bbolt v1.3.7-0.20220130032806-d5db64bdbfde
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/term v0.0.0-20210220032956-6a3ed077a48d
	golang.org/x/text v0.3.6
	golang.org/x/time v0.0.0-20210220033141-f8bda1e9f3ba
	gopkg.in/ini.v1 v1.55.0
)

require (
	decred.org/cspp/v2 v2.0.0-20211122173608-ee00e4952d5f // indirect
	github.com/StackExchange/wmi v0.0.0-20180116203802-5d049714c4a6 // indirect
	github.com/VictoriaMetrics/fastcache v1.6.0 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.0.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.1.0 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20200131011700-2b0d299dbd22 // indirect
	github.com/dchest/siphash v1.2.2 // indirect
	github.com/deckarep/golang-set v1.8.0 // indirect
	github.com/decred/base58 v1.0.3 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.1.0 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.1 // indirect
	github.com/decred/dcrd/database/v3 v3.0.0 // indirect
	github.com/decred/dcrd/lru v1.1.1 // indirect
	github.com/edsrzf/mmap-go v1.0.0 // indirect
	github.com/fjl/memsize v0.0.0-20190710130421-bcb5799ab5e5 // indirect
	github.com/gballet/go-libpcsclite v0.0.0-20190607065134-2772fd86a8ff // indirect
	github.com/gcash/bchlog v0.0.0-20180913005452-b4f036f92fa6 // indirect
	github.com/go-ole/go-ole v1.2.1 // indirect
	github.com/go-stack/stack v1.8.0 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/uuid v1.1.5 // indirect
	github.com/hashicorp/go-bexpr v0.1.10 // indirect
	github.com/hashicorp/golang-lru v0.5.5-0.20210104140557-80c98217689d // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/holiman/uint256 v1.2.0 // indirect
	github.com/huin/goupnp v1.0.2 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.4 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightningnetwork/lnd/clock v1.0.1 // indirect
	github.com/lightningnetwork/lnd/queue v1.0.1 // indirect
	github.com/lightningnetwork/lnd/ticker v1.0.0 // indirect
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mattn/go-runewidth v0.0.9 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/mitchellh/pointerstructure v1.2.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/tsdb v0.7.1 // indirect
	github.com/rjeczalik/notify v0.9.1 // indirect
	github.com/rs/cors v1.7.0 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/status-im/keycard-go v0.0.0-20190316090335-8537d3370df4 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	github.com/tklauser/go-sysconf v0.3.5 // indirect
	github.com/tklauser/numcpus v0.2.2 // indirect
	github.com/tyler-smith/go-bip39 v1.0.1-0.20181017060643-dbb3b84ba2ef // indirect
	golang.org/x/sys v0.0.0-20210816183151-1e6c022a8912 // indirect
	gopkg.in/natefinch/npipe.v2 v2.0.0-20160621034901-c1b8fa8bdcce // indirect
	gopkg.in/urfave/cli.v1 v1.20.0 // indirect
)
