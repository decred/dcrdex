#!/usr/bin/env bash
#
# Set up ElectrumX-Firo (electrumX) regtest server for testing.
# - Expects the firo regtest chain harness to be running and listening
#   for RPC's on harness.sh ALPHA_RPC_PORT="53768"
# - Exposes default RPC port at localhost:8000
# - Exposes wallet SSL connection service at localhost:50002
#
# Requires:
# - python3  - tested python3.10 and minimal testing python3.7
# - pip3     - for boostrap loading pip
# - gencerts - go install github.com/decred/dcrd/cmd/gencerts@release-v1.7
# - git
#
# See Also: README_ELECTRUM_HARNESSES.md and README_HARNESS.md

set -ex

# No external releases - just master
# May 11, 2023
# COMMIT=c0cdcc0dfcaa057058fd1ed281557dede924cd27
#  Jul 8, 2024
COMMIT=937e4bb3d8802317b64231844b698d8758029ca5

ELECTRUMX_DIR=~/dextest/electrum/firo/server
REPO_DIR=${ELECTRUMX_DIR}/electrumx-repo
DATA_DIR=${ELECTRUMX_DIR}/data
rm -rf ${DATA_DIR}
mkdir -p ${REPO_DIR} ${DATA_DIR}

cd ${REPO_DIR}

if [ ! -d "${REPO_DIR}/.git" ]; then
    git init
    git remote add origin https://github.com/firoorg/electrumx-firo.git
fi

git remote -v

git fetch --depth 1 origin ${COMMIT}
git reset --hard FETCH_HEAD

if [ ! -d "${ELECTRUMX_DIR}/venv" ]; then
    python3 -m venv ${ELECTRUMX_DIR}/venv
fi
source ${ELECTRUMX_DIR}/venv/bin/activate
python -m ensurepip --upgrade
pip install .

gencerts -L ${DATA_DIR}/ssl.cert ${DATA_DIR}/ssl.key

set +x

# Server Config
export COIN="Firo"
export NET="regtest"
export DB_ENGINE="leveldb"
export DB_DIRECTORY="${DATA_DIR}"
export DAEMON_URL="http://user:pass@127.0.0.1:53768"    # harness:alpha:rpc
export SERVICES="ssl://localhost:50002,tcp://localhost:50001,rpc://"
export SSL_CERTFILE="${DATA_DIR}/ssl.cert"
export SSL_KEYFILE="${DATA_DIR}/ssl.key"
export PEER_DISCOVERY="off"
export PEER_ANNOUNCE=
export LOG_LEVEL=DEBUG

./electrumx_server
