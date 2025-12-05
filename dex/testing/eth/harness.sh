#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. There is only one node in
# --dev mode.
set -ex

export CHAIN="eth"

export ALPHA_AUTHRPC_PORT="8552"
export ALPHA_HTTP_PORT="38556"
export ALPHA_WS_PORT="38557"

export NODES_ROOT=~/dextest/eth

./baseharness.sh
