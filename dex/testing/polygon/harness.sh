#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. There is only one node in
# --dev mode.
set -ex

export CHAIN="polygon"

export ALPHA_AUTHRPC_PORT="61829"
export ALPHA_HTTP_PORT="48296"
export ALPHA_WS_PORT="34983"

export NODES_ROOT=~/dextest/polygon

./../eth/baseharness.sh
