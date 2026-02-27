#!/usr/bin/env bash

source $(dirname "$0")/common.sh

snapcraft login

snapcraft upload --release=stable $BUILD_DIR/${APP}_${VER}_${ARCH}.snap
