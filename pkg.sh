#!/bin/sh

set -e

VER="v0.2.5"

rm -rf bin
mkdir -p bin/dexc-windows-amd64-${VER}
mkdir -p bin/dexc-linux-amd64-${VER}
mkdir -p bin/dexc-linux-arm64-${VER}
mkdir -p bin/dexc-darwin-amd64-${VER}
mkdir -p bin/dexc-darwin-arm64-${VER}

export CGO_ENABLED=0

LDFLAGS="-s -w -X decred.org/dcrdex/client/cmd/dexc/version.appPreRelease= -X decred.org/dcrdex/client/cmd/dexc/version.appBuild=release"

pushd client/cmd/dexc
GOOS=linux GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-linux-amd64-${VER} -ldflags "$LDFLAGS"
GOOS=linux GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-linux-arm64-${VER} -ldflags "$LDFLAGS"
GOOS=windows GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-windows-amd64-${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-darwin-amd64-${VER} -ldflags "$LDFLAGS"
GOOS=darwin GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-darwin-arm64-${VER} -ldflags "$LDFLAGS"
popd

pushd client/cmd/dexcctl
GOOS=linux GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-linux-amd64-${VER}
GOOS=linux GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-linux-arm64-${VER}
GOOS=windows GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-windows-amd64-${VER}
GOOS=darwin GOARCH=amd64 go build -trimpath -o ../../../bin/dexc-darwin-amd64-${VER}
GOOS=darwin GOARCH=arm64 go build -trimpath -o ../../../bin/dexc-darwin-arm64-${VER}
popd

pushd client/webserver/site
npm ci
npm run build
popd

rm -rf bin/site
mkdir -p bin/site/src
pushd client/webserver/site
cp -R dist ../../../bin/site
cp -R src/font src/html src/img ../../../bin/site/src
popd

pushd bin
cp -R site dexc-windows-amd64-${VER}
cp -R site dexc-darwin-amd64-${VER}
cp -R site dexc-darwin-arm64-${VER}
cp -R site dexc-linux-amd64-${VER}
cp -R site dexc-linux-arm64-${VER}
zip -9 -r -q dexc-windows-amd64-${VER}.zip dexc-windows-amd64-${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-linux-amd64-${VER}.tar.gz dexc-linux-amd64-${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-linux-arm64-${VER}.tar.gz dexc-linux-arm64-${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-darwin-amd64-${VER}.tar.gz dexc-darwin-amd64-${VER}
tar -I 'gzip -9' --owner=0 --group=0 -cf dexc-darwin-arm64-${VER}.tar.gz dexc-darwin-arm64-${VER}
sha256sum *.gz *.zip > dexc-${VER}-manifest.txt
popd
