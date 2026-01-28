# Monero (XMR) Wallet

This package provides native Monero wallet support for dcrdex using cgo bindings
to [monero_c](https://github.com/MrCyjaneK/monero_c), a C wrapper around
Monero's wallet2 library.

## Prerequisites

Building with XMR support requires:

- Go 1.21+
- GCC/Clang (C/C++ compiler)
- CMake 3.5+
- The `monero_c` shared library

### System Dependencies

Install build dependencies for your platform:

**Debian/Ubuntu:**
```bash
sudo apt install build-essential cmake pkg-config libboost-all-dev \
    libssl-dev libzmq3-dev libsodium-dev libunwind-dev libpgm-dev gperf
```

**Fedora/RHEL:**
```bash
sudo dnf install gcc gcc-c++ cmake boost-devel openssl-devel \
    zeromq-devel libsodium-devel libunwind-devel gperf
```

**Arch Linux:**
```bash
sudo pacman -S base-devel cmake boost openssl zeromq libsodium libunwind gperf
```

**macOS:**
```bash
brew install cmake boost openssl zeromq libsodium gperf
```

**Windows (MSYS2):**
```bash
pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake \
    mingw-w64-x86_64-boost mingw-w64-x86_64-openssl \
    mingw-w64-x86_64-zeromq mingw-w64-x86_64-libsodium gperf
```

## Building monero_c

Clone and build the monero_c library:

```bash
# Clone with submodules
git clone --recursive https://github.com/MrCyjaneK/monero_c.git
cd monero_c

# Initialize submodules (pulls Monero source)
git submodule update --init --recursive --force

# Apply patches
./apply_patches.sh monero

# Build
mkdir -p build && cd build
cmake ..
make -j$(nproc)
```

This produces `libwallet2_api_c.so` (Linux), `libwallet2_api_c.dylib`
(macOS), or `wallet2_api_c.dll` (Windows).

### Windows Build Notes

On Windows, use MSYS2 MinGW 64-bit shell:

```bash
# In MSYS2 MinGW 64-bit shell
cd monero_c
./apply_patches.sh monero

mkdir -p build && cd build
cmake -G "MinGW Makefiles" ..
mingw32-make -j$(nproc)
```

### Docker Build (Linux)

A Dockerfile is provided for building the library in a container:

```bash
# From the client/asset/xmr directory
docker build -t monero_c_builder .

# Extract the library
docker run --rm monero_c_builder cat /monero_c/monero_libwallet2_api_c/build/x86_64-linux-gnu/libwallet2_api_c.so > libwallet2_api_c.so

# Install to system (optional)
sudo cp libwallet2_api_c.so /usr/local/lib/
sudo ldconfig
```

## Building dcrdex with XMR Support

Once monero_c is built, build dcrdex with the `xmr` build tag:

```bash
# Set library path (adjust path to your monero_c build directory)
export MONERO_C_DIR=/path/to/monero_c/build

# Linux
export CGO_LDFLAGS="-L${MONERO_C_DIR} -Wl,-rpath,${MONERO_C_DIR}"

# macOS
export CGO_LDFLAGS="-L${MONERO_C_DIR} -Wl,-rpath,${MONERO_C_DIR}"

# Windows (MSYS2 MinGW 64-bit shell)
export CGO_LDFLAGS="-L${MONERO_C_DIR}"
export PATH="${MONERO_C_DIR}:${PATH}"

# Build with xmr tag
go build -tags xmr ./client/cmd/bisonw
```

### System-wide Installation (Optional)

Instead of setting `CGO_LDFLAGS`, you can install the library system-wide:

```bash
# Linux
sudo cp libwallet2_api_c.so /usr/local/lib/
sudo ldconfig

# macOS
sudo cp libwallet2_api_c.dylib /usr/local/lib/

# Windows - copy DLL to system directory or keep in PATH
copy wallet2_api_c.dll C:\Windows\System32\
```

Then build without the `-L` flags:
```bash
go build -tags xmr ./client/cmd/bisonw
```

## Configuration

When creating an XMR wallet in dcrdex, you'll need to provide:

| Setting | Description |
|---------|-------------|
| `walletname` | Name of the wallet file (required, except simnet) |
| `daemonaddress` | Monero daemon address, e.g., `localhost:18081` (has defaults) |
| `daemonusername` | RPC username (optional) |
| `daemonpassword` | RPC password (optional) |
| `feepriority` | Fee priority 0-4 (default: 0) |

### Default Values

Default daemon addresses point to public nodes for mainnet/stagenet:

| Network | Default Daemon | Default Wallet Name |
|---------|----------------|---------------------|
| Mainnet | `http://xmr-node.cakewallet.com:18081` | (required) |
| Stagenet | `http://node.monerodevs.org:38089` | (required) |
| Simnet | `http://127.0.0.1:18081` | `xmrwallet` |

For simnet, all settings have sensible defaults so you can run without configuration.

### Daemon Ports

| Network | Default Port |
|---------|--------------|
| Mainnet | 18081 |
| Stagenet | 38081 |
| Testnet | 28081 |

## Wallet Seed Format

The dcrdex seed provided during wallet creation is a 32-byte value that is used
directly as the Monero **spend key**. This is the same approach used by the
previous RPC-based implementation.

- The seed is **not** a 25-word Monero mnemonic
- The 32-byte seed is hex-encoded and passed to Monero's
  `createDeterministicWalletFromSpendKey` function
- The wallet password is also hex-encoded for consistency with the previous
  implementation

This allows deterministic wallet recovery from the dcrdex application seed.

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
└── cgo/
    ├── wallet.go   # CGO bindings to monero_c
    └── doc.go      # Package documentation
```

The cgo package wraps approximately 30 functions from monero_c's wallet2_api_c.h,
providing Go-idiomatic access to:

- Wallet creation, opening, and recovery
- Balance queries
- Address generation (primary + subaddresses)
- Transaction creation and broadcasting
- Transaction history
- Sync status monitoring

## Troubleshooting

**"cannot find -lwallet2_api_c"**

The linker can't find the library. Either:
- Set `CGO_LDFLAGS` to point to the library location
- Install the library to a system path (`/usr/local/lib`)
- Ensure `LD_LIBRARY_PATH` (Linux) or `DYLD_LIBRARY_PATH` (macOS) includes the library path

**"undefined symbol" or "DLL not found" errors at runtime**

The library was found at build time but not runtime. Set the runtime library path:
```bash
# Linux
export LD_LIBRARY_PATH=/path/to/monero_c/build:$LD_LIBRARY_PATH

# macOS
export DYLD_LIBRARY_PATH=/path/to/monero_c/build:$DYLD_LIBRARY_PATH

# Windows - add to PATH or copy DLL to executable directory
set PATH=C:\path\to\monero_c\build;%PATH%
```

On Linux/macOS, you can also rebuild with `-Wl,-rpath` to embed the path in the binary.

**Wallet sync is slow**

Initial sync requires scanning the blockchain. Set a recent `birthday` (block height)
when recovering a wallet to skip scanning old blocks.
