#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. There is only one node in
# --dev mode.
set -ex

export CHAIN="base"

export ALPHA_AUTHRPC_PORT="8652"
export ALPHA_HTTP_PORT="39556"
export ALPHA_WS_PORT="39557"

export NODES_ROOT=~/dextest/base

./../eth/baseharness.sh
