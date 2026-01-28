# Dockerfile for building libwallet2_api_c.so
#
# Build:
#   docker build -t monero_c_builder .
#
# Extract library:
#   docker run --rm monero_c_builder cat /monero_c/monero_libwallet2_api_c/build/x86_64-linux-gnu/libwallet2_api_c.so > libwallet2_api_c.so
#
# Or run interactively:
#   docker run --rm -it monero_c_builder bash

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install build dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    pkg-config \
    libboost-all-dev \
    libssl-dev \
    libzmq3-dev \
    libsodium-dev \
    libunwind-dev \
    libpgm-dev \
    gperf \
    git \
    && rm -rf /var/lib/apt/lists/*

# Clone monero_c with submodules
RUN git clone --recursive https://github.com/MrCyjaneK/monero_c.git /monero_c

WORKDIR /monero_c

# Apply patches
RUN ./apply_patches.sh monero

# Build
WORKDIR /monero_c/monero_libwallet2_api_c/build/x86_64-linux-gnu
RUN cmake ../.. && make -j$(nproc)

# Verify the library was built
RUN ls -la libwallet2_api_c.so

CMD ["bash"]
