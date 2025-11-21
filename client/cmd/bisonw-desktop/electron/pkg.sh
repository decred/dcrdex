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
rm -rf bin
rm -rf installers
mkdir -p installers
popd

function getPlatform() {
  case "$(uname -s)" in
    Linux) platform="linux" ;;
    Darwin) platform="mac" ;;
    CYGWIN*|MSYS*|MINGW*) platform="win" ;;
    *) platform="linux" ;;
  esac
  echo "${platform}"
}

PLATFORM=$(getPlatform)

build_targets (){
  for TARGET in ${TARGETS}; do
    OS=${TARGET%%/*}
    ARCH=${TARGET##*/}
    echo "Building for ${OS}-${ARCH}"

    pushd ..
    mkdir -p "bin"
    popd

    pushd ../../../../client/cmd/bisonw
    GOOS=${OS} GOARCH=${ARCH} go build -trimpath ${TAGS_BISONW:+-tags ${TAGS_BISONW}} -o  "../bisonw-desktop/bin/${BISONW_EXE}" -ldflags "${LDFLAGS_BISONW:-${LDFLAGS_BASE}}"
    popd

    npm run make --platform=$([[ "$OS" == "windows" ]] && echo "win32" || echo "$OS") --arch=${ARCH}

    if [[ "$PLATFORM" == "win" ]]; then
      cp -R "./out/make/squirrel.windows/x64/BisonWallet.exe" "../installers/bisonw-desktop-${OS}-${ARCH}-v${VER}.exe"
    elif [[ "$PLATFORM" == "mac" ]]; then
      cp -R "./out/make/BisonWallet.dmg" "../installers/bisonw-desktop-${OS}-${ARCH}-v${VER}.dmg"
    else
      cp -R "./out/make" "../installers/bisonw-desktop-${OS}-${ARCH}-v${VER}"
    fi

    rm -rf "./out"
    rm -rf "../bin" # clear bin dir for next build

  done
}

# Vanilla builds on all supported os/arch targets
TARGETS="linux/amd64 linux/arm64"
if [[ "$PLATFORM" == "win" ]]; then
  TARGETS="windows/amd64"
elif [[ "$PLATFORM" == "mac" ]]; then
  TARGETS="darwin/amd64" #darwin/arm64"
fi
build_targets

# echo "Files embedded in the Go webserver package:"
# go list -f '{{ .EmbedFiles }}' decred.org/dcrdex/client/webserver
# # NOTE: before embedding, we needed to grab: dist, src/font, src/html, src/img.

# pushd bin
# shasum -a 256 *.gz *.zip > bisonw-v${VER}-manifest.txt
# popd
