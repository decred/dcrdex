#!/usr/bin/env bash

set -e

# For release, remove pre-release info, and set metadata to "release".
VER="0.6.0-pre" # pre, beta, rc1, etc.
META= # "release"

rm -rf bin
mkdir -p bin/dexc-windows-amd64-v${VER}
mkdir -p bin/dexc-linux-amd64-v${VER}
mkdir -p bin/dexc-linux-arm64-v${VER}
mkdir -p bin/dexc-darwin-amd64-v${VER}
mkdir -p bin/dexc-darwin-arm64-v${VER}

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

LDFLAGS="-s -w -X main.Version=${VER}${META:++${META}}"

pushd client/cmd/dexc
GOOS=linux GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-linux-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=linux GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-linux-arm64-v${VER} -ldflags "$LDFLAGS"
GOOS=windows GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-windows-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-darwin-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-darwin-arm64-v${VER} -ldflags "$LDFLAGS"
popd

LDFLAGS="-s -w -X main.Version=${VER}${META:++${META}}"

pushd client/cmd/dexcctl
GOOS=linux GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-linux-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=linux GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-linux-arm64-v${VER} -ldflags "$LDFLAGS"
GOOS=windows GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-windows-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-darwin-amd64-v${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-darwin-arm64-v${VER} -ldflags "$LDFLAGS"
popd

echo "Files embedded in the Go webserver package:"
go list -f '{{ .EmbedFiles }}' decred.org/dcrdex/client/webserver
# NOTE: before embedding, we needed to grab: dist, src/font, src/html, src/img.

pushd bin
zip -9 -r -q dexc-windows-amd64-v${VER}.zip dexc-windows-amd64-v${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-linux-amd64-v${VER}.tar.gz dexc-linux-amd64-v${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-linux-arm64-v${VER}.tar.gz dexc-linux-arm64-v${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-darwin-amd64-v${VER}.tar.gz dexc-darwin-amd64-v${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-darwin-arm64-v${VER}.tar.gz dexc-darwin-arm64-v${VER}
sha256sum *.gz *.zip > dexc-v${VER}-manifest.txt
popd
