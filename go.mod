module decred.org/dcrdex

go 1.24.9

replace github.com/bisoncraft/mesh => github.com/buck54321/mesh v0.0.0-20260310125835-501f40b7413d

require (
	decred.org/dcrwallet/v5 v5.0.3
	filippo.io/edwards25519 v1.0.0
	fyne.io/systray v1.10.1-0.20220621085403-9a2652634e93
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412
	github.com/athanorlabs/go-dleq v0.1.0
	github.com/bisoncraft/bchwallet v1.0.2
	github.com/bisoncraft/go-bip39 v1.0.1
	github.com/bisoncraft/go-monero v0.1.1
	github.com/bisoncraft/mesh v0.0.0-00010101000000-000000000000
	github.com/bisoncraft/neutrino-bch v1.0.1
	github.com/bisoncraft/op-geth v0.0.0-20250729074358-3cfe4f15e91c
	github.com/btcsuite/btcd v0.24.2-beta.rc1.0.20240625142744-cc26860b4026
	github.com/btcsuite/btcd/btcec/v2 v2.3.4
	github.com/btcsuite/btcd/btcutil v1.1.5
	github.com/btcsuite/btcd/btcutil/psbt v1.1.8
	github.com/btcsuite/btcd/chaincfg/chainhash v1.1.0
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcwallet v0.16.10
	github.com/btcsuite/btcwallet/wallet/txauthor v1.3.5
	github.com/btcsuite/btcwallet/walletdb v1.4.4
	github.com/btcsuite/btcwallet/wtxmgr v1.5.4
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/dchest/blake2b v1.0.0
	github.com/dcrlabs/ltcwallet v0.0.0-20240823165752-3e026e8da010
	github.com/decred/base58 v1.0.6
	github.com/decred/dcrd/addrmgr/v3 v3.0.0
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.2
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.2
	github.com/decred/dcrd/certgen v1.2.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.5
	github.com/decred/dcrd/chaincfg/v3 v3.3.0
	github.com/decred/dcrd/connmgr/v3 v3.1.3
	github.com/decred/dcrd/crypto/blake256 v1.1.0
	github.com/decred/dcrd/dcrec v1.0.1
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.4
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0
	github.com/decred/dcrd/dcrjson/v4 v4.2.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.3
	github.com/decred/dcrd/gcs/v4 v4.1.1
	github.com/decred/dcrd/hdkeychain/v3 v3.1.3
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.4.0
	github.com/decred/dcrd/rpcclient/v8 v8.1.0
	github.com/decred/dcrd/txscript/v4 v4.1.2
	github.com/decred/dcrd/wire v1.7.2
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/decred/vspd/types/v2 v2.1.0
	github.com/dgraph-io/badger v1.6.2
	github.com/dgraph-io/badger/v4 v4.9.0
	github.com/ethereum/go-ethereum v1.16.7
	github.com/fatih/color v1.16.0
	github.com/gcash/bchd v0.19.0
	github.com/gcash/bchlog v0.0.0-20180913005452-b4f036f92fa6
	github.com/gcash/bchutil v0.0.0-20210113190856-6ea28dff4000
	github.com/go-chi/chi/v5 v5.2.3
	github.com/gorilla/websocket v1.5.3
	github.com/haven-protocol-org/monero-go-utils v0.0.0-20211126154105-058b2666f217
	github.com/huandu/skiplist v1.2.0
	github.com/jessevdk/go-flags v1.6.1
	github.com/jrick/logrotate v1.1.2
	github.com/lib/pq v1.10.4
	github.com/libp2p/go-libp2p v0.44.0
	github.com/lightninglabs/neutrino v0.16.1-0.20240814152458-81d6cd2d2da5
	github.com/ltcsuite/ltcd v0.23.6-0.20240131072528-64dfa402637a
	github.com/ltcsuite/ltcd/chaincfg/chainhash v1.0.2
	github.com/ltcsuite/ltcd/ltcutil v1.1.4-0.20240131072528-64dfa402637a
	github.com/pkg/browser v0.0.0-20210911075715-681adbf594b8
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/yuin/goldmark v1.7.13
	go.etcd.io/bbolt v1.3.12
	golang.org/x/crypto v0.47.0
	golang.org/x/exp v0.0.0-20250606033433-dcc06ee1d476
	golang.org/x/sync v0.19.0
	golang.org/x/term v0.39.0
	golang.org/x/text v0.33.0
	golang.org/x/time v0.12.0
	google.golang.org/protobuf v1.36.10
	gopkg.in/ini.v1 v1.67.0
	gopkg.in/square/go-jose.v2 v2.6.0
	lukechampine.com/blake3 v1.4.1
)

require (
	decred.org/dcrwallet v1.7.0 // indirect
	github.com/AndreasBriese/bbloom v0.0.0-20190825152654-46b345b51c96 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251001021608-1fe7b43fc4d6 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/charmbracelet/colorprofile v0.2.3-0.20250311203215-f60798e515dc // indirect
	github.com/charmbracelet/x/ansi v0.10.1 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.13-0.20250311204145-2c3ea96c31dd // indirect
	github.com/charmbracelet/x/term v0.2.1 // indirect
	github.com/davidlazar/go-crypto v0.0.0-20200604182044-b73af7476f6c // indirect
	github.com/decred/dcrd/blockchain/stake/v3 v3.0.0 // indirect
	github.com/decred/dcrd/database/v2 v2.0.2 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v3 v3.0.0 // indirect
	github.com/decred/dcrd/dcrutil/v3 v3.0.0 // indirect
	github.com/decred/dcrd/gcs/v2 v2.1.0 // indirect
	github.com/decred/dcrd/txscript/v3 v3.0.0 // indirect
	github.com/decred/dcrtime v0.0.0-20191018193024-8d8b4ef0458e // indirect
	github.com/dgraph-io/ristretto v0.0.2 // indirect
	github.com/dgraph-io/ristretto/v2 v2.2.0 // indirect
	github.com/emicklei/dot v1.6.2 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/ethereum/go-bigmodexpfix v0.0.0-20250911101455-f9e208c548ab // indirect
	github.com/flynn/noise v1.1.0 // indirect
	github.com/francoispqt/gojay v1.2.13 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/flatbuffers v25.2.10+incompatible // indirect
	github.com/google/trillian v1.4.1 // indirect
	github.com/gorilla/schema v1.1.0 // indirect
	github.com/h2non/go-is-svg v0.0.0-20160927212452-35e8c4b0612c // indirect
	github.com/huin/goupnp v1.3.0 // indirect
	github.com/ipfs/go-cid v0.5.0 // indirect
	github.com/jackpal/go-nat-pmp v1.0.2 // indirect
	github.com/jbenet/go-temp-err-catcher v0.1.0 // indirect
	github.com/koron/go-ssdp v0.0.6 // indirect
	github.com/libp2p/go-buffer-pool v0.1.0 // indirect
	github.com/libp2p/go-flow-metrics v0.2.0 // indirect
	github.com/libp2p/go-libp2p-asn-util v0.4.1 // indirect
	github.com/libp2p/go-msgio v0.3.0 // indirect
	github.com/libp2p/go-netroute v0.3.0 // indirect
	github.com/libp2p/go-reuseport v0.4.0 // indirect
	github.com/libp2p/go-yamux/v5 v5.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/marcopeereboom/sbox v1.1.0 // indirect
	github.com/marten-seemann/tcp v0.0.0-20210406111302-dfbc87cc63fd // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/miekg/dns v1.1.66 // indirect
	github.com/mikioh/tcpinfo v0.0.0-20190314235526-30a79bb1804b // indirect
	github.com/mikioh/tcpopt v0.0.0-20190314235656-172688c1accc // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multiaddr v0.16.0 // indirect
	github.com/multiformats/go-multiaddr-dns v0.4.1 // indirect
	github.com/multiformats/go-multiaddr-fmt v0.1.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multicodec v0.9.1 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-multistream v0.6.1 // indirect
	github.com/multiformats/go-varint v0.0.7 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pbnjay/memory v0.0.0-20210728143218-7b4eea64cf58 // indirect
	github.com/pion/datachannel v1.5.10 // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/dtls/v3 v3.0.6 // indirect
	github.com/pion/ice/v4 v4.0.10 // indirect
	github.com/pion/interceptor v0.1.40 // indirect
	github.com/pion/logging v0.2.3 // indirect
	github.com/pion/mdns/v2 v2.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.15 // indirect
	github.com/pion/rtp v1.8.19 // indirect
	github.com/pion/sctp v1.8.39 // indirect
	github.com/pion/sdp/v3 v3.0.13 // indirect
	github.com/pion/srtp/v3 v3.0.6 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/stun/v3 v3.0.0 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/transport/v3 v3.0.7 // indirect
	github.com/pion/turn/v4 v4.0.2 // indirect
	github.com/pion/webrtc/v4 v4.1.2 // indirect
	github.com/prometheus/client_golang v1.22.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/quic-go/qpack v0.5.1 // indirect
	github.com/quic-go/quic-go v0.55.0 // indirect
	github.com/quic-go/webtransport-go v0.9.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/wlynxg/anet v0.0.5 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel v1.37.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/trace v1.37.0 // indirect
	go.uber.org/dig v1.19.0 // indirect
	go.uber.org/fx v1.24.0 // indirect
	go.uber.org/mock v0.5.2 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/mod v0.31.0 // indirect
	golang.org/x/telemetry v0.0.0-20251203150158-8fff8a5912fc // indirect
	golang.org/x/tools v0.40.0 // indirect
)

replace github.com/btcsuite/btcd/btcec/v2 v2.3.4 => github.com/martonp/btcd/btcec/v2 v2.0.0-20250528172049-6b252bb1b6a1

require (
	decred.org/cspp/v2 v2.4.0 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/StackExchange/wmi v1.2.1 // indirect
	github.com/VictoriaMetrics/fastcache v1.13.0 // indirect
	github.com/aead/siphash v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.22.0 // indirect
	github.com/btcsuite/btcwallet/wallet/txrules v1.2.2 // indirect
	github.com/btcsuite/btcwallet/wallet/txsizes v1.2.5 // indirect
	github.com/btcsuite/go-socks v0.0.0-20170105172521-4720035b7bfd // indirect
	github.com/btcsuite/golangcrypto v0.0.0-20150304025918-53f62d9b43e8 // indirect
	github.com/btcsuite/websocket v0.0.0-20150119174127-31079b680792 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20220309191932-9e0f3af2f07a // indirect
	github.com/consensys/gnark-crypto v0.18.0 // indirect
	github.com/crate-crypto/go-eth-kzg v1.4.0 // indirect
	github.com/crate-crypto/go-ipa v0.0.0-20240724233137-53bbb0ceb27a // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/container/lru v1.0.0 // indirect
	github.com/decred/dcrd/crypto/rand v1.0.1 // indirect
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2 // indirect
	github.com/decred/dcrd/database/v3 v3.0.3 // indirect
	github.com/decred/dcrd/lru v1.1.2 // indirect
	github.com/decred/dcrd/mixing v0.6.1 // indirect
	github.com/decred/politeia v1.5.0
	github.com/decred/vspd/client/v4 v4.0.2 // indirect
	github.com/decred/vspd/types/v3 v3.0.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.5 // indirect
	github.com/ethereum/go-verkle v0.2.2 // indirect
	github.com/ferranbt/fastssz v0.1.4 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gofrs/flock v0.12.1 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/holiman/billy v0.0.0-20250707135307-f2f9b9aae7db // indirect
	github.com/holiman/bloomfilter/v2 v2.0.3 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/jrick/bitset v1.0.0 // indirect
	github.com/jrick/wsrpc/v2 v2.3.8 // indirect
	github.com/kkdai/bstream v1.0.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/lightninglabs/gozmq v0.0.0-20191113021534-d20a764486bf // indirect
	github.com/lightninglabs/neutrino/cache v1.1.2 // indirect
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
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/naoina/go-stringutil v0.1.0 // indirect
	github.com/naoina/toml v0.1.2-0.20170918210437-9fafd6967416 // indirect
	github.com/olekukonko/tablewriter v0.0.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/shirou/gopsutil v3.21.4-0.20210419000835-c7a38de76ee5+incompatible // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/supranational/blst v0.3.16-0.20250831170142-f48500c1fdbe // indirect
	github.com/tevino/abool v1.2.0 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/zquestz/grab v0.0.0-20190224022517-abcee96e61b1 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
