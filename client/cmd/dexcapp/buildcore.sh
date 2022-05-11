#!/usr/bin/env bash

MACOSX_DEPLOYMENT_TARGET=10.11 go build -buildmode=c-shared -o ./lib/libcore/libcore.dylib ../../core_cgo
flutter pub run ffigen
