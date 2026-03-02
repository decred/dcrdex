#!/usr/bin/env bash

set -e

cd "$(dirname "$0")"

# For release, remove pre-release info, and set metadata to "release".
VER="1.1.0-pre" # pre, beta, rc1, etc.
META= # "release"
BISONW_EXE="bisonw"

# Parse flags.
while [[ "$#" -gt 0 ]]; do
  case $1 in
    --xmr) TAGS_BISONW="${TAGS_BISONW:+${TAGS_BISONW} }xmr"; export CGO_ENABLED=1 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
  shift
done

export CGO_ENABLED=${CGO_ENABLED:-0}
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

build_targets (){
  for TARGET in ${TARGETS}; do
    OS=${TARGET%%/*}
    ARCH=${TARGET##*/}
    echo "Building for ${OS}-${ARCH}"

    pushd ..
    mkdir -p "resources/mac"
    popd

    pushd ../../../../client/cmd/bisonw
    GOOS=${OS} GOARCH=${ARCH} go build -trimpath ${TAGS_BISONW:+-tags ${TAGS_BISONW}} -o  "../bisonw-desktop/resources/mac/${BISONW_EXE}" -ldflags "${LDFLAGS_BISONW:-${LDFLAGS_BASE}}"
    popd

    pushd ../src
    cp bisonw-16.png ../resources/mac/bisonw-16.png
    popd

    # Bundle XMR shared library if building with XMR support.
    if [[ " ${TAGS_BISONW} " == *" xmr "* || "${TAGS_BISONW}" == "xmr" ]]; then
      cp "../../../../client/asset/xmr/lib/${OS}-${ARCH}/libwallet2_api_c.dylib" "../resources/mac/"
      # macOS requires ad-hoc signing when linking against an unsigned dynamic
      # library, and downloaded files may have quarantine attributes.
      if [[ "$(uname)" == "Darwin" ]]; then
        xattr -dr com.apple.quarantine "../resources/mac/libwallet2_api_c.dylib" 2>/dev/null || true
        codesign -f -s - "../resources/mac/libwallet2_api_c.dylib"
        codesign -f -s - "../resources/mac/${BISONW_EXE}"
      fi
    fi

    npm run make --platform=${OS} --arch=${ARCH}

    cp -R "./out/make/BisonWallet.dmg" "../installers/bisonw-desktop-${OS}-${ARCH}-v${VER}.dmg"

    rm -rf "./out"

  done
}

TARGETS="darwin/amd64 darwin/arm64"
build_targets

rm -rf "../resources"
