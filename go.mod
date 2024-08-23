module decred.org/dcrdex

go 1.21

require (
	decred.org/dcrwallet/v4 v4.1.1
	fyne.io/systray v1.10.1-0.20220621085403-9a2652634e93
	github.com/btcsuite/btcd v0.24.2-beta.rc1.0.20240625142744-cc26860b4026
	github.com/btcsuite/btcd/btcec/v2 v2.3.4
	github.com/btcsuite/btcd/btcutil v1.1.5
	github.com/btcsuite/btcd/btcutil/psbt v1.1.8
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcwallet v0.16.10-0.20240815225602-6ecae9c12fde
	github.com/btcsuite/btcwallet/wallet/txauthor v1.3.4
	github.com/btcsuite/btcwallet/walletdb v1.4.2
	github.com/btcsuite/btcwallet/wtxmgr v1.5.3
	github.com/davecgh/go-spew v1.1.1
	github.com/dchest/blake2b v1.0.0
	github.com/dcrlabs/bchwallet v0.0.0-20240114124852-0e95005810be
	github.com/dcrlabs/ltcwallet v0.0.0-20240823165752-3e026e8da010
	github.com/dcrlabs/neutrino-bch v0.0.0-20240114121828-d656bce11095
	github.com/decred/base58 v1.0.5
	github.com/decred/dcrd/addrmgr/v2 v2.0.4
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.1
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.1
	github.com/decred/dcrd/certgen v1.1.3
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4
	github.com/decred/dcrd/chaincfg/v3 v3.2.1
	github.com/decred/dcrd/connmgr/v3 v3.1.2
	github.com/decred/dcrd/crypto/blake256 v1.0.1
	github.com/decred/dcrd/dcrec v1.0.1
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.3.0
	github.com/decred/dcrd/dcrjson/v4 v4.1.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.2
	github.com/decred/dcrd/gcs/v4 v4.1.0
	github.com/decred/dcrd/hdkeychain/v3 v3.1.2
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.3.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.1
	github.com/decred/dcrd/txscript/v4 v4.1.1
	github.com/decred/dcrd/wire v1.7.0
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/decred/vspd/types/v2 v2.1.0
	github.com/dgraph-io/badger v1.6.2
	github.com/ethereum/go-ethereum v1.14.8
	github.com/fatih/color v1.16.0
	github.com/gcash/bchd v0.19.0
	github.com/gcash/bchlog v0.0.0-20180913005452-b4f036f92fa6
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000
	github.com/go-chi/chi/v5 v5.0.1
	github.com/gorilla/websocket v1.5.1
	github.com/huandu/skiplist v1.2.0
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.10.4
	github.com/lightninglabs/neutrino v0.16.0
	github.com/ltcsuite/ltcd v0.23.6-0.20240131072528-64dfa402637a
	github.com/ltcsuite/ltcd/chaincfg/chainhash v1.0.2
	github.com/ltcsuite/ltcd/ltcutil v1.1.4-0.20240131072528-64dfa402637a
	github.com/pkg/browser v0.0.0-20210911075715-681adbf594b8
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/tyler-smith/go-bip39 v1.1.0
	go.etcd.io/bbolt v1.3.9
	golang.org/x/crypto v0.23.0
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa
	golang.org/x/sync v0.7.0
	golang.org/x/term v0.20.0
	golang.org/x/text v0.15.0
	golang.org/x/time v0.5.0
	gopkg.in/ini.v1 v1.67.0
	lukechampine.com/blake3 v1.3.0
)

require (
	decred.org/cspp/v2 v2.2.0 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/DataDog/zstd v1.5.2 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/VictoriaMetrics/fastcache v1.12.2 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.10.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.2.1 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.2.4 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/golangcrypto v0.0.0-20150304025918-53f62d9b43e8 // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/fifo v0.0.0-20240606204812-0bbfbd93a7ce // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/pebble v1.1.1 // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20220309191932-9e0f3af2f07a // indirect
	github.com/consensys/bavard v0.1.13 // indirect
	github.com/consensys/gnark-crypto v0.12.1 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240223125850-b1e8a79f509c // indirect
	github.com/crate-crypto/go-kzg-4844 v1.0.0 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.2 // indirect
	github.com/decred/dcrd/lru v1.1.1 // indirect
	github.com/decred/dcrd/mixing v0.3.0 // indirect
	github.com/decred/vspd/client/v3 v3.0.0 // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ethereum/c-kzg-4844 v1.0.0 // indirect
	github.com/ethereum/go-verkle v0.1.1-0.20240306133620-7d920df305f0 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/gofrs/flock v0.8.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/holiman/billy v0.0.0-20240216141850-2abb0c79d3c4 // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/holiman/uint256 v1.3.1 // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.5 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/klauspost/compress v1.16.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino/cache v1.1.1 // indirect
	github.com/lightningnetwork/lnd/clock v1.0.1 // indirect
	github.com/lightningnetwork/lnd/queue v1.0.1 // indirect
	github.com/lightningnetwork/lnd/ticker v1.0.0 // indirect
	github.com/lightningnetwork/lnd/tlv v1.0.2 // indirect
	github.com/ltcsuite/lnd/clock v0.0.0-20200822020009-1a001cbb895a // indirect
	github.com/ltcsuite/lnd/queue v1.1.0 // indirect
	github.com/ltcsuite/lnd/ticker v1.0.1 // indirect
	github.com/ltcsuite/lnd/tlv v0.0.0-20240222214433-454d35886119 // indirect
	github.com/ltcsuite/ltcd/btcec/v2 v2.3.2 // indirect
	github.com/ltcsuite/ltcd/ltcutil/psbt v1.1.1-0.20240131072528-64dfa402637a // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mmcloughlin/addchain v0.4.0 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.14.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.39.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	github.com/rs/cors v1.8.2 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	github.com/supranational/blst v0.3.11 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	github.com/tevino/abool v1.2.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/zquestz/grab v0.0.0-20190224022517-abcee96e61b1 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	rsc.io/tmplfunc v0.0.3 // indirect
)
