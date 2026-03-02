#!/usr/bin/env bash

set -ex

SCRIPT_DIR=$(dirname "$0")

source $SCRIPT_DIR/common.sh

linux/prepare-snap.sh
snapcraft --verbose --output $BUILD_DIR/
