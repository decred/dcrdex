# Dockerfile for building libwallet2_api_c for Linux, Windows, and macOS
#
# Build:
#   docker build -t monero_c_builder .
#
# Extract libraries:
#   # Linux x86_64
#   docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-linux-gnu_libwallet2_api_c.so.xz | xz -d > libwallet2_api_c.so
#
#   # Windows x86_64
#   docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-w64-mingw32_libwallet2_api_c.dll.xz | xz -d > libwallet2_api_c.dll
#
#   # macOS x86_64 (remove .x86_64 from name before using)
#   docker run --rm monero_c_builder cat /monero_c/release/monero/x86_64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > libwallet2_api_c.x86_64.dylib
#
#   # macOS ARM64 (remove .arm64 from name before using)
#   docker run --rm monero_c_builder cat /monero_c/release/monero/aarch64-apple-darwin11_libwallet2_api_c.dylib.xz | xz -d > libwallet2_api_c.arm64.dylib
#
# Or run interactively:
#   docker run --rm -it monero_c_builder bash
#
# Or extract all at once:
#   docker run --rm monero_c_builder tar -cf - -C /monero_c/release/monero . | tar -xf -

FROM debian:bookworm@sha256:23f8d499dcf5559a92ef814c309d747268420cf5e5e60f067f495d509f0aeb93

ENV DEBIAN_FRONTEND=noninteractive

# Install build dependencies for all platforms
RUN apt-get update && apt-get install -y \
    build-essential \
    pkg-config \
    autoconf \
    libtool \
    ccache \
    make \
    cmake \
    gcc \
    g++ \
    git \
    curl \
    lbzip2 \
    libtinfo5 \
    gperf \
    xz-utils \
    python-is-python3 \
    # Windows cross-compilation (MinGW)
    gcc-mingw-w64-x86-64 \
    g++-mingw-w64-x86-64 \
    && rm -rf /var/lib/apt/lists/*

# Clone monero_c with submodules
RUN git clone --recursive https://github.com/MrCyjaneK/monero_c.git /monero_c

WORKDIR /monero_c

# Checkout specific version and apply patches
RUN git config --global user.name "bob" && \
    git config --global user.email "bob@nowhere.com" && \
    git checkout 411e8a1cdb3f4c2812d83f28c335d2a4eb18bd29 && \
    ./apply_patches.sh monero

# Apply our custom patches for allow_mismatched_daemon_version
COPY 0001-add-allow_mismatched_daemon_version.patch /tmp/
COPY 0002-add-wallet2_api_c-wrapper.patch /tmp/
RUN cd monero && git apply /tmp/0001-add-allow_mismatched_daemon_version.patch && \
    cd .. && git apply /tmp/0002-add-wallet2_api_c-wrapper.patch

# Add our new symbol to the macOS export list (required for symbol visibility on Darwin)
RUN sed -i '/_MONERO_Wallet_setTrustedDaemon/a _MONERO_Wallet_setAllowMismatchedDaemonVersion' \
    /monero_c/monero_libwallet2_api_c/monero_libwallet2_api_c.exp

# Build for Linux x86_64
RUN ./build_single.sh monero x86_64-linux-gnu -j$(nproc)

# Switch mingw-w64 to use POSIX threading model (required for std::mutex in Boost 1.85+)
RUN update-alternatives --set x86_64-w64-mingw32-gcc /usr/bin/x86_64-w64-mingw32-gcc-posix && \
    update-alternatives --set x86_64-w64-mingw32-g++ /usr/bin/x86_64-w64-mingw32-g++-posix

# Add libssp to linker flags for Windows only (wrapped in if(MINGW))
# The stack protector functions (__stack_chk_fail, etc.) are in libssp
RUN SSP_PATH=$(find /usr/lib/gcc/x86_64-w64-mingw32 -name "libssp.a" | grep posix | head -1) && \
    echo "Found libssp at: $SSP_PATH" && \
    echo "" >> /monero_c/monero_libwallet2_api_c/CMakeLists.txt && \
    echo "if(MINGW)" >> /monero_c/monero_libwallet2_api_c/CMakeLists.txt && \
    echo "    target_link_libraries(wallet2_api_c $SSP_PATH)" >> /monero_c/monero_libwallet2_api_c/CMakeLists.txt && \
    echo "endif()" >> /monero_c/monero_libwallet2_api_c/CMakeLists.txt && \
    tail -5 /monero_c/monero_libwallet2_api_c/CMakeLists.txt

# Build for Windows x86_64
RUN ./build_single.sh monero x86_64-w64-mingw32 -j$(nproc)

# Fix Boost build for macOS: add _DARWIN_C_SOURCE to expose BSD socket definitions
# (SO_NOSIGPIPE, SO_REUSEPORT, ESHUTDOWN, ipv6_mreq are hidden by _XOPEN_SOURCE)
RUN sed -i 's/cxxflags_darwin=-fPIC -std=c++11$/cxxflags_darwin=-fPIC -std=c++11 -D_DARWIN_C_SOURCE/' \
    /monero_c/contrib/depends/packages/boost.mk && \
    grep cxxflags_darwin /monero_c/contrib/depends/packages/boost.mk

# Fix polyseed build for macOS: add CMAKE_AR and CMAKE_RANLIB for cross-compilation
RUN sed -i 's|CC="$($(package)_cc)" cmake|CC="$($(package)_cc)" cmake -DCMAKE_AR=$($(package)_ar) -DCMAKE_RANLIB=$($(package)_ranlib)|' \
    /monero_c/contrib/depends/packages/polyseed.mk && \
    grep cmake /monero_c/contrib/depends/packages/polyseed.mk

# Fix cmake cross-compilation: insert CMAKE_TRY_COMPILE_TARGET_TYPE before project()
# This prevents cmake from trying to run compiled test programs (impossible in cross-compile)
RUN sed -i '/^cmake_minimum_required/a set(CMAKE_TRY_COMPILE_TARGET_TYPE STATIC_LIBRARY)' \
    /monero_c/monero_libwallet2_api_c/CMakeLists.txt && \
    head -5 /monero_c/monero_libwallet2_api_c/CMakeLists.txt

# Build macOS depends first (separate step to cache and debug)
RUN cd /monero_c/contrib/depends && make HOST=x86_64-apple-darwin11 -j$(nproc)

# Fix toolchain.cmake: CMAKE_C_COMPILER must be a path, not a command with args
# The generated toolchain has "SET(CMAKE_C_COMPILER clang -target ...)" but CMake
# expects just the path. Use CMAKE_SYSROOT and CMAKE_C_FLAGS for proper cross-compilation.
RUN TOOLCHAIN=/monero_c/contrib/depends/x86_64-apple-darwin11/share/toolchain.cmake && \
    CLANG_PATH=/monero_c/contrib/depends/x86_64-apple-darwin11/native/bin/clang && \
    SDK_PATH=/monero_c/contrib/depends/x86_64-apple-darwin11/native/SDK && \
    sed -i 's|SET(CMAKE_C_COMPILER clang -target.*-apple-darwin11-)|SET(CMAKE_C_COMPILER '"$CLANG_PATH"')|' "$TOOLCHAIN" && \
    sed -i 's|SET(CMAKE_CXX_COMPILER clang++ -target.*-apple-darwin11-)|SET(CMAKE_CXX_COMPILER '"$CLANG_PATH"'++)|' "$TOOLCHAIN" && \
    echo '' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_SYSROOT '"$SDK_PATH"')' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_OSX_SYSROOT '"$SDK_PATH"')' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -target x86_64-apple-darwin11 -mmacosx-version-min=10.14 -mlinker-version=609 -B/monero_c/contrib/depends/x86_64-apple-darwin11/native/bin/x86_64-apple-darwin11-")' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -target x86_64-apple-darwin11 -mmacosx-version-min=10.14 -mlinker-version=609 -stdlib=libc++ -B/monero_c/contrib/depends/x86_64-apple-darwin11/native/bin/x86_64-apple-darwin11- -D_LIBCPP_ENABLE_CXX17_REMOVED_UNARY_BINARY_FUNCTION")' >> "$TOOLCHAIN" && \
    echo "=== Fixed toolchain.cmake (tail) ===" && \
    tail -10 "$TOOLCHAIN"

# Skip forbid_undefined_symbols check for Darwin cross-compilation (test fails when cross-compiling)
RUN sed -i 's/CMAKE_SYSTEM_NAME MATCHES "kOpenBSD/CMAKE_SYSTEM_NAME MATCHES "Darwin|kOpenBSD/' \
    /monero_c/monero/CMakeLists.txt && \
    grep -A2 "forbid_undefined_symbols" /monero_c/monero/CMakeLists.txt | head -5

# Fix libc++ compatibility: std::__unary_function is internal, use std::unary_function
RUN sed -i 's/std::__unary_function/std::unary_function/g' \
    /monero_c/monero/src/cryptonote_basic/cryptonote_basic_impl.h && \
    grep -n "unary_function" /monero_c/monero/src/cryptonote_basic/cryptonote_basic_impl.h

# Build for macOS x86_64 (cross-compile)
RUN ./build_single.sh monero x86_64-apple-darwin11 -j$(nproc)

# Build macOS ARM64 depends
RUN cd /monero_c/contrib/depends && make HOST=aarch64-apple-darwin11 -j$(nproc)

# Fix toolchain.cmake for ARM64 (same issues as x86_64)
RUN TOOLCHAIN=/monero_c/contrib/depends/aarch64-apple-darwin11/share/toolchain.cmake && \
    CLANG_PATH=/monero_c/contrib/depends/aarch64-apple-darwin11/native/bin/clang && \
    SDK_PATH=/monero_c/contrib/depends/aarch64-apple-darwin11/native/SDK && \
    sed -i 's|SET(CMAKE_C_COMPILER clang -target.*-apple-darwin11-)|SET(CMAKE_C_COMPILER '"$CLANG_PATH"')|' "$TOOLCHAIN" && \
    sed -i 's|SET(CMAKE_CXX_COMPILER clang++ -target.*-apple-darwin11-)|SET(CMAKE_CXX_COMPILER '"$CLANG_PATH"'++)|' "$TOOLCHAIN" && \
    echo '' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_SYSROOT '"$SDK_PATH"')' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_OSX_SYSROOT '"$SDK_PATH"')' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -target arm64-apple-darwin11 -mmacosx-version-min=10.14 -mlinker-version=609 -B/monero_c/contrib/depends/aarch64-apple-darwin11/native/bin/aarch64-apple-darwin11-")' >> "$TOOLCHAIN" && \
    echo 'SET(CMAKE_CXX_FLAGS "${CMAKE_CXX_FLAGS} -target arm64-apple-darwin11 -mmacosx-version-min=10.14 -mlinker-version=609 -stdlib=libc++ -B/monero_c/contrib/depends/aarch64-apple-darwin11/native/bin/aarch64-apple-darwin11- -D_LIBCPP_ENABLE_CXX17_REMOVED_UNARY_BINARY_FUNCTION")' >> "$TOOLCHAIN" && \
    echo "=== Fixed ARM64 toolchain.cmake (tail) ===" && \
    tail -10 "$TOOLCHAIN"

# Build for macOS ARM64 (cross-compile)
RUN ./build_single.sh monero aarch64-apple-darwin11 -j$(nproc)

# Fix macOS dylib install_name so the library is found next to the executable.
# Using @executable_path avoids needing -Wl,-rpath flags (which Go's cgo rejects).
RUN for arch in x86_64 aarch64; do \
    xzfile="/monero_c/release/monero/${arch}-apple-darwin11_libwallet2_api_c.dylib.xz" && \
    dylib="${xzfile%.xz}" && \
    tool="/monero_c/contrib/depends/${arch}-apple-darwin11/native/bin/${arch}-apple-darwin11-install_name_tool" && \
    echo "Fixing install_name for ${arch} dylib..." && \
    xz -dk "$xzfile" && \
    "$tool" -id "@executable_path/libwallet2_api_c.dylib" "$dylib" && \
    rm "$xzfile" && \
    xz "$dylib" && \
    echo "Done: $(ls -la "$xzfile")"; \
    done

# Clean up darwin SDK files to reduce image size
RUN rm -rf /monero_c/contrib/depends/built/*/native_clang || true && \
    rm -rf /monero_c/contrib/depends/sources/*sdk*.tar.* || true && \
    rm -rf /monero_c/contrib/depends/sources/clang-llvm*.tar.* || true

# List built libraries
RUN echo "=== Built libraries ===" && \
    ls -la /monero_c/release/monero/

CMD ["bash"]
