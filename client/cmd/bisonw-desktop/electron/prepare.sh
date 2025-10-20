#!/usr/bin/env bash

set -e

# For release, remove pre-release info, and set metadata to "release".
VER="1.0.4-pre" # pre, beta, rc1, etc.
META= # "release"

export CGO_ENABLED=0
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

PLATFORM=$(./platform.sh)

pushd ..
rm -rf resources
mkdir -p "resources/${PLATFORM}"
popd

pushd ../../../../client/cmd/bisonw
go build -trimpath ${TAGS_BISONW:+-tags ${TAGS_BISONW}} -o  "../bisonw-desktop/resources/${PLATFORM}/${BISONW_EXE}" -ldflags "${LDFLAGS_BISONW:-${LDFLAGS_BASE}}"
popd

pushd ../src
cp bisonw-16.png ../resources/${PLATFORM}/bisonw-16.png
popd

echo "Preparation complete. If you're running this for the first time, proceed to run npm ci before npm run start or npm run make."