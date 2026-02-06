# Pre-built monero_c Libraries

This directory contains pre-built `libwallet2_api_c` libraries for each
supported platform. These are built using the Dockerfile in the parent
directory.

## Platforms

| Directory | Platform | Library |
|-----------|----------|---------|
| `linux-amd64/` | Linux x86_64 | `libwallet2_api_c.so` |
| `windows-amd64/` | Windows x86_64 | `libwallet2_api_c.dll` |
| `darwin-amd64/` | macOS x86_64 | `libwallet2_api_c.dylib` |
| `darwin-arm64/` | macOS ARM64 | `libwallet2_api_c.dylib` |

## Verification

Verify library integrity against the checksums:

```bash
cd client/asset/xmr/lib
sha256sum -c SHA256SUMS
```

## Building New Libraries

To rebuild the libraries (from `client/asset/xmr/`):

```bash
# Build the Docker image
docker build -t monero_c_builder .

# Extract libraries
docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-linux-gnu_libwallet2_api_c.so.xz | xz -d > lib/linux-amd64/libwallet2_api_c.so

docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-w64-mingw32_libwallet2_api_c.dll.xz | xz -d > lib/windows-amd64/libwallet2_api_c.dll

docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > lib/darwin-amd64/libwallet2_api_c.dylib

docker run --rm monero_c_builder cat /monero_c/release/monero/aarch64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > lib/darwin-arm64/libwallet2_api_c.dylib

# Generate checksums
cd lib
sha256sum */libwallet2_api_c.* > SHA256SUMS
```

## Runtime Requirements

After building bisonw with XMR support, the library must be available at runtime:

**Option 1: Copy library next to executable (recommended for development)**
```bash
# Linux
cp lib/linux-amd64/libwallet2_api_c.so ./

# macOS
cp lib/darwin-arm64/libwallet2_api_c.dylib ./  # or darwin-amd64

# Windows
copy lib\windows-amd64\libwallet2_api_c.dll .\
```

**Option 2: Install to application library directory (for system packaging)**
```bash
# Linux - use /usr/lib/bisonw to avoid conflicts with other apps using monero_c
sudo mkdir -p /usr/lib/bisonw
sudo cp lib/linux-amd64/libwallet2_api_c.so /usr/lib/bisonw/
```

The binary's rpath is configured to search both the executable's directory and
`/usr/lib/bisonw`, so either location works without setting `LD_LIBRARY_PATH`.
