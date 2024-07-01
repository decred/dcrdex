#!/usr/bin/env bash

export SCRIPT_DIR=$(pwd)
export SYMBOL=btc
export REPO=https://github.com/spesmilo/electrum.git
export COMMIT=7263a49129d14db288a01b0b9d569422baddf5e1 # 4.5.5
export GENESIS=0f9188f13cb7b2c71f2a335e3a4fc328bf5beb436012afca590b1a11466e2206
export RPCPORT=16789
export EX_PORT=54002

./electrum-base.sh
