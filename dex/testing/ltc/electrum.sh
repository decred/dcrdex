#!/usr/bin/env bash

export SCRIPT_DIR=$(pwd)
export SYMBOL=ltc
export REPO=https://github.com/pooler/electrum-ltc.git
# 4.2.2.1 release
export COMMIT=c3571782191969b912e805056cf8510e0b41d277
export GENESIS=530827f38f93b43ed12af0b3ad25a288dc02ed74d6d7857862df51fc56c416f9
export RPCPORT=26789
export EX_PORT=55002
# https://github.com/spesmilo/electrum/issues/7833 (fixed in 4.3)
# possible alt workaround: python3 -m pip install --user "protobuf>=3.12,<4"
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python

../btc/electrum-base.sh
