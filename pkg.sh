#!/usr/bin/env bash

set -e

# For release, remove pre-release info, and set metadata to "release".
VER="0.6.0-pre" # pre, beta, rc1, etc.
META= # "release"

TARGETS="linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64"

export CGO_ENABLED=0

# if META set, append "+${META}", otherwise nothing.
LDFLAGS="-s -w -X main.Version=${VER}${META:++${META}}"

# Build the webpack bundle prior to building the webserver package, which embeds
# the files.
pushd client/webserver/site
go generate # just check, no write
npm ci
npm run build
popd

rm -rf bin
for TARGET in ${TARGETS}; do
  OS=${TARGET%%/*}
  ARCH=${TARGET##*/}

  mkdir -p "bin/dexc-${OS}-${ARCH}-v${VER}"

  pushd client/cmd/dexc
  GOOS=${OS} GOARCH=${ARCH} go build -trimpath -o "../../../bin/dexc-${OS}-${ARCH}-v${VER}" -ldflags "$LDFLAGS"
  popd

  pushd client/cmd/dexcctl
  GOOS=${OS} GOARCH=${ARCH} go build -trimpath -o "../../../bin/dexc-${OS}-${ARCH}-v${VER}" -ldflags "$LDFLAGS"
  popd

  pushd bin
  if [[ "$OS" == "windows" ]]; then
    zip -9 -r -q "dexc-${OS}-${ARCH}-v${VER}.zip" "dexc-${OS}-${ARCH}-v${VER}"
  else
    tar -I 'gzip -9' --owner=0 --group=0 -cf "dexc-${OS}-${ARCH}-v${VER}.tar.gz" "dexc-${OS}-${ARCH}-v${VER}"
  fi
  popd
done

echo "Files embedded in the Go webserver package:"
go list -f '{{ .EmbedFiles }}' decred.org/dcrdex/client/webserver
# NOTE: before embedding, we needed to grab: dist, src/font, src/html, src/img.

pushd bin
sha256sum *.gz *.zip > dexc-v${VER}-manifest.txt
popd
