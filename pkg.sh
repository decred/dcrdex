#!/usr/bin/env bash

set -e

# For release, remove pre-release info, and set metadata to "release".
VER="0.6.0-beta" # pre, beta, rc1, etc.
META= # "release"

export CGO_ENABLED=0

# if META set, append "+${META}", otherwise nothing.
LDFLAGS="-s -w -X main.Version=${VER}${META:++${META}}"

# Build the webpack bundle prior to building the webserver package, which embeds
# the files. This is checked in for the release branches.
# pushd client/webserver/site
# go generate # just check, no write
# npm ci
# npm run build
# popd

rm -rf bin

build_targets (){
  for TARGET in ${TARGETS}; do
    OS=${TARGET%%/*}
    ARCH=${TARGET##*/}
    echo "Building for ${OS}-${ARCH} with FLAVOR=${FLAVOR}"

    mkdir -p "bin/dexc${FLAVOR}-${OS}-${ARCH}-v${VER}"

    pushd client/cmd/dexc
    GOOS=${OS} GOARCH=${ARCH} go build -trimpath -o "../../../bin/dexc${FLAVOR}-${OS}-${ARCH}-v${VER}" -ldflags "$LDFLAGS"
    popd

    pushd client/cmd/dexcctl
    GOOS=${OS} GOARCH=${ARCH} go build -trimpath -o "../../../bin/dexc${FLAVOR}-${OS}-${ARCH}-v${VER}" -ldflags "$LDFLAGS"
    popd

    pushd bin
    if [[ "$OS" == "windows" ]]; then
      zip -9 -r -q "dexc${FLAVOR}-${OS}-${ARCH}-v${VER}.zip" "dexc${FLAVOR}-${OS}-${ARCH}-v${VER}"
    else
      tar -I 'gzip -9' --owner=0 --group=0 -cf "dexc${FLAVOR}-${OS}-${ARCH}-v${VER}.tar.gz" "dexc${FLAVOR}-${OS}-${ARCH}-v${VER}"
    fi
    popd
  done
}

# Vanilla builds on all supported os/arch targets
TARGETS="linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64"
build_targets

# Only windows gets the systray build
TARGETS="windows/amd64"
FLAVOR="-tray"
build_targets

echo "Files embedded in the Go webserver package:"
go list -f '{{ .EmbedFiles }}' decred.org/dcrdex/client/webserver
# NOTE: before embedding, we needed to grab: dist, src/font, src/html, src/img.

pushd bin
sha256sum *.gz *.zip > dexc-v${VER}-manifest.txt
popd
