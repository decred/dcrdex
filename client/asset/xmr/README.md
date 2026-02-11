# Monero (XMR) Wallet

This package provides native Monero wallet support for dcrdex using cgo bindings
to [monero_c](https://github.com/MrCyjaneK/monero_c), a C wrapper around
Monero's wallet2 library.

## Building dcrdex with XMR Support

The `libwallet2_api_c` shared library must be built before compiling with XMR
support. See [Rebuilding the Libraries](#rebuilding-the-libraries) for
instructions on building the library using the provided Dockerfile.

Once the library is built and placed in `lib/<platform>/`, build with:

- Go 1.21+
- GCC/Clang (C/C++ compiler)

```bash
go build -tags xmr ./client/cmd/bisonw
```

### Runtime Setup

The shared library must also be available at runtime.

**Option 1: Copy next to executable (recommended for development)**

```bash
# Linux
cp client/asset/xmr/lib/linux-amd64/libwallet2_api_c.so ./

# macOS ARM64 (Apple Silicon)
cp client/asset/xmr/lib/darwin-arm64/libwallet2_api_c.dylib ./
codesign -s - bisonw

# macOS x86_64 (Intel)
cp client/asset/xmr/lib/darwin-amd64/libwallet2_api_c.dylib ./
codesign -s - bisonw

# Windows
copy client\asset\xmr\lib\windows-amd64\libwallet2_api_c.dll .\
```

**Option 2: Install to application library directory (for system packaging)**

```bash
# Linux - use /usr/lib/bisonw to avoid conflicts with other apps using monero_c
sudo mkdir -p /usr/lib/bisonw
sudo cp client/asset/xmr/lib/linux-amd64/libwallet2_api_c.so /usr/lib/bisonw/
```

The binary's rpath is configured to search both the executable's directory and
`/usr/lib/bisonw`, so either location works without setting `LD_LIBRARY_PATH`.

## Configuration

When creating an XMR wallet in dcrdex, you'll need to provide:

| Setting | Description |
|---------|-------------|
| `daemonaddress` | Monero daemon address, e.g., `localhost:18081` (has defaults) |
| `daemonusername` | RPC username (optional) |
| `daemonpassword` | RPC password (optional) |
| `feepriority` | Fee priority 0-4 (default: 0) |

### Default Values

Default daemon addresses point to public nodes for mainnet/testnet:

| Network | Default Daemon | Default Wallet Name |
|---------|----------------|---------------------|
| Mainnet | `http://xmr-node.cakewallet.com:18081` | (required) |
| Testnet | `http://127.0.0.1:28081` | (required) |
| Simnet | `http://127.0.0.1:18081` | `xmrwallet` |

For simnet, all settings have sensible defaults so you can run without configuration.

### Daemon Ports

| Network | Default Port |
|---------|--------------|
| Mainnet | 18081 |
| Testnet | 28081 |

## Wallet Seed Format

The dcrdex seed provided during wallet creation is a 32-byte value that is used
directly as the Monero **spend key**.

- The seed is **not** a 25-word Monero mnemonic
- The 32-byte seed is hex-encoded and passed to Monero's
  `createDeterministicWalletFromSpendKey` function

## Running a Monero Daemon

You'll need access to a Monero daemon (monerod). Options:

**Run your own:**
```bash
monerod --detach
```

**Use a public node (less private):**
```
node.moneroworld.com:18089
```

## Current Limitations

- **No trading support**: This wallet currently only supports deposits, withdrawals,
  and balance checking. Atomic swap functionality (Swap, Redeem, Refund) is not
  yet implemented.

- **Fee model**: Monero uses priority levels (0-4) instead of explicit fee rates.
  The `feepriority` setting controls transaction priority.

## Architecture

```
client/asset/xmr/
├── xmr.go      # Driver and Wallet implementation
├── stub.go     # Stub when xmr tag not set
├── cxmr/
│   ├── wallet.go   # CGO bindings to monero_c
│   └── doc.go      # Package documentation
└── lib/
    ├── linux-amd64/    # Linux x86_64 library
    ├── darwin-amd64/   # macOS x86_64 library
    ├── darwin-arm64/   # macOS ARM64 library
    ├── windows-amd64/  # Windows x86_64 library
    └── .gitkeep        # Directory placeholders
```

The cgo package wraps approximately 30 functions from monero_c's wallet2_api_c.h,
providing Go-idiomatic access to:

- Wallet creation, opening, and recovery
- Balance queries
- Address generation (primary + subaddresses)
- Transaction creation and broadcasting
- Transaction history
- Sync status monitoring

## CGO Wallet Cryptographic Safety

The CGO wallet delegates all cryptographic operations to Monero's wallet2 library
via monero_c, meaning key derivation, transaction signing, ring signatures, and
RingCT are handled by the same battle-tested C++ code used by the official Monero
CLI/GUI wallets.

### Key Derivation and Storage

The dcrdex 32-byte seed is used directly as the Monero **secret spend key**. It
is hex-encoded and passed to monero_c's `createDeterministicWalletFromSpendKey`,
which deterministically derives the full key set:

```
32-byte dcrdex seed
    → hex-encode
    → MONERO_WalletManager_createDeterministicWalletFromSpendKey()
    → secret spend key + secret view key + public keys + primary address
```

The resulting wallet file is encrypted with the user's password (also
hex-encoded for consistency). All sensitive C strings — passwords, seeds, and
keys — are allocated with `C.CString` and freed with `C.free` via deferred
calls, so they do not linger in Go's garbage-collected heap.

Key material is never logged or transmitted. The secret spend key and secret
view key can be retrieved through the CGO bindings (`SecretSpendKey`,
`SecretViewKey` in `cxmr/wallet.go`) but are only used internally.

### Transaction-Level Protections

Monero's core privacy features are inherited transparently through wallet2:

- **Ring signatures** — each input is hidden among decoy outputs from the
  blockchain, making it infeasible to determine the true sender.
- **Stealth addresses** — every transaction creates a one-time destination
  address, preventing address reuse linkage.
- **RingCT (Confidential Transactions)** — transaction amounts are encrypted
  using Pedersen commitments; only the sender and receiver can see the value.

Transaction creation flows through `MONERO_Wallet_createTransaction`, which
handles input selection, ring member selection, Bulletproofs+ range proofs, and
signing entirely within wallet2. The Go layer only supplies the destination
address, amount, and fee priority — it never touches raw cryptographic
primitives.

### Secret Hash Validation

For atomic swap compatibility, the wallet validates secrets against their hashes
using SHA-256:

```go
func (w *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
    h := sha256.Sum256(secret)
    return bytes.Equal(h[:], secretHash)
}
```

This uses Go's `crypto/sha256` and a constant-time-safe `bytes.Equal`
comparison (constant-length inputs).

### Fund Validation Mechanism

Fund validation operates at two levels: aggregate balance queries and
per-output coin enumeration.

**Balance verification** calls wallet2's `balance()` and `unlockedBalance()`
for a given account index. These return the total and spendable balances
respectively, computed from the wallet's scanned output set. The difference
(`total - unlocked`) represents locked/immature funds awaiting the standard
10-confirmation maturity window.

**Per-output validation** uses `AllCoins()`, which enumerates every output
known to the wallet and returns its state:

| Field | Meaning |
|-------|---------|
| `Amount` | Output value in atomic units |
| `Spent` | Whether the output has already been consumed |
| `Unlocked` | Whether the output has enough confirmations to spend |
| `Frozen` | Whether the output has been manually frozen |
| `SubaddrAcct` | The subaddress account that owns the output |

Only outputs that are **not spent**, **unlocked**, **not frozen**, and belong to
**account 0** are considered available for spending.

**Dust filtering** — outputs below 0.001 XMR (`1_000_000_000` atomic units) are
classified as dust and subtracted from the available balance. This prevents the
wallet from attempting to construct transactions with uneconomical inputs. The
dust total is cached for 30 seconds to avoid repeated full-output enumeration
through CGO.

**Address ownership** is verified by iterating all generated subaddresses for
account 0 and checking for an exact string match. Address format validity is
checked separately via monero_c's `MONERO_Wallet_addressValid`, which
validates the Base58-encoded structure and network byte.

**Sync integrity** — the wallet continuously monitors its block height against
the daemon's chain tip. Balances and coin states are only considered reliable
once `Synchronized()` returns true and no rescan is in progress. During initial
sync or recovery, the wallet sets `SetRecoveringFromSeed(true)` and scans from
the configured restore height, ensuring no outputs are missed.

## Troubleshooting

**"cannot find -lwallet2_api_c"**

The linker can't find the library. Ensure the pre-built libraries exist in the
`lib/` directory. If building from a custom location, set `CGO_LDFLAGS`:

```bash
export CGO_LDFLAGS="-L/path/to/library"
```

**"Library not loaded" or "DLL not found" errors at runtime**

The library was found at build time but not runtime. Either:

1. Copy the library next to the executable (see Runtime Setup above)
2. Install it system-wide
3. Set the runtime library path:

```bash
# Linux
export LD_LIBRARY_PATH=/path/to/library:$LD_LIBRARY_PATH

# macOS
export DYLD_LIBRARY_PATH=/path/to/library:$DYLD_LIBRARY_PATH

# Windows - add to PATH
set PATH=C:\path\to\library;%PATH%
```

**"Killed: 9" on macOS**

macOS requires the binary to be ad-hoc signed after building with a dynamic
library. Run `codesign -s - bisonw` after building. If the library was
downloaded rather than built locally, you may also need to remove the quarantine
attribute: `xattr -d com.apple.quarantine libwallet2_api_c.dylib`.

**Wallet sync is slow**

Initial sync requires scanning the blockchain. Set a recent `birthday` (block height)
when recovering a wallet to skip scanning old blocks.

## Rebuilding the Libraries

If you need to rebuild the libraries (e.g., after updating monero_c), use the
provided Dockerfile:

```bash
# From the client/asset/xmr directory
docker build -t monero_c_builder .

# Extract libraries to lib/ directories
docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-linux-gnu_libwallet2_api_c.so.xz | xz -d > lib/linux-amd64/libwallet2_api_c.so

docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-w64-mingw32_libwallet2_api_c.dll.xz | xz -d > lib/windows-amd64/libwallet2_api_c.dll

docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > lib/darwin-amd64/libwallet2_api_c.dylib

docker run --rm monero_c_builder cat /monero_c/release/monero/aarch64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > lib/darwin-arm64/libwallet2_api_c.dylib
```

**Supported targets:**
- Linux x86_64 (`x86_64-linux-gnu`)
- Windows x86_64 (`x86_64-w64-mingw32`)
- macOS x86_64 (`x86_64-apple-darwin11`)
- macOS ARM64 (`aarch64-apple-darwin11`)

**Build notes:**

- **Build time**: Building dependencies for all 4 targets from scratch takes
  1-2+ hours depending on your machine.

- **macOS SDK**: The darwin builds automatically download the macOS SDK (~1GB)
  as part of the depends system.

- **Disk space**: The full build can use 20-30GB+ with all toolchains and
  intermediate files.

- **Debugging**: If something fails, run interactively to investigate:
  ```bash
  docker run --rm -it monero_c_builder bash
  ```
