#!/usr/bin/env bash

export SCRIPT_DIR=$(pwd)
export SYMBOL=btc
export REPO=https://github.com/spesmilo/electrum.git
# 4.3.0a somewhere on master
export COMMIT=0fca35fa4068ea7c9da60cd5330b85c2ca1d0398
export GENESIS=0f9188f13cb7b2c71f2a335e3a4fc328bf5beb436012afca590b1a11466e2206
export RPCPORT=16789
export EX_PORT=54002

./electrum-base.sh
