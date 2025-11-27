module decred.org/dcrdex/dex/testing/loadbot

go 1.24.0

replace decred.org/dcrdex => ../../../

require (
	decred.org/dcrdex v0.0.0-20230206134810-8a482dd7caf1
	github.com/Shopify/toxiproxy/v2 v2.4.0
)

require (
	github.com/dcrlabs/ltcwallet v0.0.0-20240823165752-3e026e8da010 // indirect
	github.com/ltcsuite/lnd/tlv v0.0.0-20240222214433-454d35886119 // indirect
	github.com/ltcsuite/ltcd/chaincfg/chainhash v1.0.2 // indirect
)

require (
	decred.org/cspp/v2 v2.4.0 // indirect
	decred.org/dcrwallet/v5 v5.0.1 // indirect
	filippo.io/edwards25519 v1.0.0 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/VictoriaMetrics/fastcache v1.12.2 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/athanorlabs/go-dleq v0.1.0 // indirect
	github.com/bisoncraft/go-monero v0.1.1 // indirect
	github.com/bisoncraft/op-geth v0.0.0-20250729074358-3cfe4f15e91c // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/btcsuite/btcd v0.24.2-beta.rc1.0.20240625142744-cc26860b4026 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.4 // indirect
	github.com/btcsuite/btcd/btcutil v1.1.5 // indirect
	github.com/btcsuite/btcd/btcutil/psbt v1.1.8 // indirect
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0 // indirect
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f // indirect
	github.com/btcsuite/btcwallet v0.16.10 // indirect
	github.com/btcsuite/btcwallet/wallet/txauthor v1.3.5 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.2.2 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.2.5 // indirect
	github.com/btcsuite/btcwallet/walletdb v1.4.4 // indirect
	github.com/btcsuite/btcwallet/wtxmgr v1.5.4 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/golangcrypto v0.0.0-20150304025918-53f62d9b43e8 // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20220309191932-9e0f3af2f07a // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.3.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dchest/blake2b v1.0.0 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/dcrlabs/bchwallet v0.0.0-20240114124852-0e95005810be // indirect
	github.com/dcrlabs/neutrino-bch v0.0.0-20240114121828-d656bce11095 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/base58 v1.0.6 // indirect
	github.com/decred/dcrd/addrmgr/v3 v3.0.0 // indirect
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.2 // indirect
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.2 // indirect
	github.com/decred/dcrd/certgen v1.2.0 // indirect
	github.com/decred/dcrd/chaincfg/chainhash v1.0.5 // indirect
	github.com/decred/dcrd/chaincfg/v3 v3.3.0 // indirect
	github.com/decred/dcrd/connmgr/v3 v3.1.3 // indirect
	github.com/decred/dcrd/container/lru v1.0.0 // indirect
	github.com/decred/dcrd/crypto/blake256 v1.1.0 // indirect
	github.com/decred/dcrd/crypto/rand v1.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.3 // indirect
	github.com/decred/dcrd/dcrec v1.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.4 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/decred/dcrd/dcrjson/v4 v4.2.0 // indirect
	github.com/decred/dcrd/dcrutil/v4 v4.0.3 // indirect
	github.com/decred/dcrd/gcs/v4 v4.1.1 // indirect
	github.com/decred/dcrd/hdkeychain/v3 v3.1.3 // indirect
	github.com/decred/dcrd/lru v1.1.2 // indirect
	github.com/decred/dcrd/mixing v0.6.0 // indirect
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.4.0 // indirect
	github.com/decred/dcrd/rpcclient/v8 v8.1.0 // indirect
	github.com/decred/dcrd/txscript/v4 v4.1.2 // indirect
	github.com/decred/dcrd/wire v1.7.1 // indirect
	github.com/decred/go-socks v1.1.0 // indirect
	github.com/decred/slog v1.2.0 // indirect
	github.com/decred/vspd/client/v4 v4.0.1 // indirect
	github.com/decred/vspd/types/v2 v2.1.0 // indirect
	github.com/decred/vspd/types/v3 v3.0.0 // indirect
	github.com/dgraph-io/badger v1.6.2 // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.0 // indirect
	github.com/ethereum/go-ethereum v1.16.1 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/ferranbt/fastssz v0.1.2 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/gcash/bchd v0.19.0 // indirect
	github.com/gcash/bchlog v0.0.0-20180913005452-b4f036f92fa6 // indirect
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000 // indirect
	github.com/go-chi/chi/v5 v5.0.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.5-0.20220116011046-fa5810519dcb // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/holiman/billy v0.0.0-20240216141850-2abb0c79d3c4 // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/logrotate v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.8 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	github.com/lib/pq v1.10.4 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino v0.16.1-0.20240814152458-81d6cd2d2da5 // indirect
	github.com/lightninglabs/neutrino/cache v1.1.2 // indirect
	github.com/lightningnetwork/lnd/clock v1.0.1 // indirect
	github.com/lightningnetwork/lnd/queue v1.0.1 // indirect
	github.com/lightningnetwork/lnd/ticker v1.0.0 // indirect
	github.com/lightningnetwork/lnd/tlv v1.0.2 // indirect
	github.com/ltcsuite/lnd/clock v0.0.0-20200822020009-1a001cbb895a // indirect
	github.com/ltcsuite/lnd/queue v1.1.0 // indirect
	github.com/ltcsuite/lnd/ticker v1.0.1 // indirect
	github.com/ltcsuite/ltcd v0.23.6-0.20240131072528-64dfa402637a // indirect
	github.com/ltcsuite/ltcd/btcec/v2 v2.3.2 // indirect
	github.com/ltcsuite/ltcd/ltcutil v1.1.4-0.20240131072528-64dfa402637a // indirect
	github.com/ltcsuite/ltcd/ltcutil/psbt v1.1.1-0.20240131072528-64dfa402637a // indirect
	github.com/mattn/go-runewidth v0.0.13 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/naoina/toml v0.1.2-0.20170918210437-9fafd6967416 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/supranational/blst v0.3.14 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/tyler-smith/go-bip39 v1.1.0 // indirect
	github.com/zquestz/grab v0.0.0-20190224022517-abcee96e61b1 // indirect
	go.etcd.io/bbolt v1.3.12 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/term v0.37.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
)

replace github.com/btcsuite/btcd/btcec/v2 v2.3.4 => github.com/martonp/btcd/btcec/v2 v2.0.0-20250528172049-6b252bb1b6a1
