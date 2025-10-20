#!/usr/bin/env bash

set -e

# For release, remove pre-release info, and set metadata to "release".
VER="1.0.4-pre" # pre, beta, rc1, etc.
META= # "release"

export CGO_ENABLED=0
export GOWORK=off

# if META set, append "+${META}", otherwise nothing.
LDFLAGS_BASE="-buildid= -s -w -X main.Version=${VER}${META:++${META}}"

# Build the webpack bundle prior to building the webserver package, which embeds
# the files.
pushd ../../../../client/webserver/site
go generate # just check, no write
npm ci
npm run build
popd

rm -rf out # This is the default output dir for electron-forge make

# Use folders outside of pwd to prevent electron-forge from adding them to build.
# The "ignore" packaging config seems to malfunction: https://github.com/electron/forge/issues/673#issuecomment-697180654
pushd ..
rm -rf resources
rm -rf installers
mkdir -p installers
popd

PLATFORM=$(./platform.sh)

build_targets (){
  for TARGET in ${TARGETS}; do
    OS=${TARGET%%/*}
    ARCH=${TARGET##*/}
    echo "Building for ${OS}-${ARCH}"

    pushd ..
    mkdir -p "resources/${PLATFORM}"
    popd

    pushd ../../../../client/cmd/bisonw
    GOOS=${OS} GOARCH=${ARCH} go build -trimpath ${TAGS_BISONW:+-tags ${TAGS_BISONW}} -o  "../bisonw-desktop/resources/${PLATFORM}/${BISONW_EXE}" -ldflags "${LDFLAGS_BISONW:-${LDFLAGS_BASE}}"
    popd

    pushd ../src
    cp bisonw-16.png ../resources/${PLATFORM}/bisonw-16.png
    popd

    npm run make --platform=${OS} --arch=${ARCH}

    cp -R "./out/make" "../installers/bisonw-${OS}-${ARCH}"

    rm -rf "./out"

  done
}

# Vanilla builds on all supported os/arch targets
TARGETS="linux/amd64 linux/arm64"
if [[ "$PLATFORM" == "win" ]]; then
  TARGETS="win32/amd64"
elif [[ "$PLATFORM" == "mac" ]]; then
  TARGETS="darwin/amd64" #darwin/arm64"
fi
build_targets

rm -rf "../resources"

# echo "Files embedded in the Go webserver package:"
# go list -f '{{ .EmbedFiles }}' decred.org/dcrdex/client/webserver
# # NOTE: before embedding, we needed to grab: dist, src/font, src/html, src/img.

# pushd bin
# shasum -a 256 *.gz *.zip > bisonw-v${VER}-manifest.txt
# popd
