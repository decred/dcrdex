#!/usr/bin/env bash

# Devs should install gencerts
# go install github.com/decred/dcrd/cmd/gencerts@release-v1.7

# ./electrumx.sh (Bitcoin/Litecoin) (20556/20566) (54002/55002)

set -ex

export COIN="${1:-Bitcoin}"

case $COIN in
  Bitcoin)
    SYMBOL=btc
    ;;

  Litecoin)
    SYMBOL=ltc
    ;;

  *)
    echo -n "unknown coin"
    ;;
esac


ELECTRUMX_DIR=~/dextest/electrum/${SYMBOL}/server
REPO_DIR=${ELECTRUMX_DIR}/electrumx-repo
DATA_DIR=${ELECTRUMX_DIR}/data
rm -rf ${DATA_DIR}
mkdir -p ${REPO_DIR} ${DATA_DIR}

cd ${REPO_DIR}

if [ ! -d "${REPO_DIR}/.git" ]; then
    git init
    git remote add origin https://github.com/spesmilo/electrumx.git
fi

git fetch --depth 1 origin fb037fbd23b8ce418cd67d68bbf8d32e69ecef62
git reset --hard FETCH_HEAD

if [ ! -d "${ELECTRUMX_DIR}/venv" ]; then
    python -m venv ${ELECTRUMX_DIR}/venv
fi
source ${ELECTRUMX_DIR}/venv/bin/activate
python -m ensurepip --upgrade
pip install .

gencerts -L ${DATA_DIR}/ssl.cert ${DATA_DIR}/ssl.key

set +x

# Server Config
export NET="regtest"
export DB_ENGINE="leveldb"
export DB_DIRECTORY="${DATA_DIR}"
export DAEMON_URL="http://user:pass@127.0.0.1:${2:-20556}"
export SERVICES="ssl://localhost:${3:-54002},rpc://"
export SSL_CERTFILE="${DATA_DIR}/ssl.cert"
export SSL_KEYFILE="${DATA_DIR}/ssl.key"
export PEER_DISCOVERY="off"
export PEER_ANNOUNCE= 
./electrumx_server
