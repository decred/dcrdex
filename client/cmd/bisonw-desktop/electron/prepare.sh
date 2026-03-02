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

#Build the webpack bundle prior to building the webserver package, which embeds
# the files.
pushd ../../../../client/webserver/site
go generate # just check, no write
npm ci
npm run build
popd

pushd ..
rm -rf resources
mkdir -p "resources/mac"
popd

pushd ../../../../client/cmd/bisonw
go build -trimpath ${TAGS_BISONW:+-tags ${TAGS_BISONW}} -o  "../bisonw-desktop/resources/mac/${BISONW_EXE}" -ldflags "${LDFLAGS_BISONW:-${LDFLAGS_BASE}}"
popd

pushd ../src
cp bisonw-16.png ../resources/mac/bisonw-16.png
popd

# Bundle XMR shared library if building with XMR support.
if [[ " ${TAGS_BISONW} " == *" xmr "* || "${TAGS_BISONW}" == "xmr" ]]; then
  XMRARCH=$(go env GOARCH)
  cp "../../../../client/asset/xmr/lib/darwin-${XMRARCH}/libwallet2_api_c.dylib" "../resources/mac/"
  # macOS requires ad-hoc signing when linking against an unsigned dynamic
  # library, and downloaded files may have quarantine attributes.
  if [[ "$(uname)" == "Darwin" ]]; then
    xattr -dr com.apple.quarantine "../resources/mac/libwallet2_api_c.dylib" 2>/dev/null || true
    codesign -f -s - "../resources/mac/libwallet2_api_c.dylib"
    codesign -f -s - "../resources/mac/${BISONW_EXE}"
  fi
fi

echo "Preparation complete. Proceed to run npm run start or npm run make."
