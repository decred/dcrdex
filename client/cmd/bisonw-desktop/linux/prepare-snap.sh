#!/usr/bin/env bash

set -ex

SCRIPT_DIR=$(dirname "$0")

source $SCRIPT_DIR/common.sh

SNAPCRAFT_YML_IN=snap/local/snapcraft.yaml.in
SNAPCRAFT_YML=snap/snapcraft.yaml
sed -e "s/\$VERSION/$VER/g" \
    -e "s/\$DEB_NAME/$DEB_NAME/g" "$SNAPCRAFT_YML_IN" > "$SNAPCRAFT_YML"

