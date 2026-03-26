#!/usr/bin/env bash

BASE_DIR=$(realpath $(dirname $0))
BUILD_DIR="${BASE_DIR}/build"
REPO_DIR="${BUILD_DIR}/torrepo"

COMMIT_HASH=d133d41f5d285c2f0a9c2fd75acde285d67afd3d

rm -r -f "${REPO_DIR}"
mkdir -p "${REPO_DIR}"
cd "${REPO_DIR}"

git init
git remote add origin https://gitlab.torproject.org/tpo/core/tor
git fetch --depth 1 origin ${COMMIT_HASH}
git checkout FETCH_HEAD

./autogen.sh
./configure --disable-asciidoc
make -j$(nproc)

cd "${BUILD_DIR}"
rm -f tor
cp "${REPO_DIR}/src/app/tor" .
rm -rf "${REPO_DIR}"
