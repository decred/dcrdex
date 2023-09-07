#!/usr/bin/env bash
#
# Set up Electrum-Firo (electrum) regtest client wallet for testing.
# - Expects the firo regtest chain server harness to be running. 
# - Expects an ElectrumX_Firo server runing on top of the chain server
# - Connects to the ElectrumX server over SSL at localhost:EX_PORT
# - Exposes electrum RPC at localhost:RPC_PORT - Dex connects here
#
# Requires:
# - python3  - tested python3.10 and minimal testing python3.7
# - pip3     - for boostrap loading pip
# - git
#
# See Also: README_ELECTRUM_HARNESSES.md and README_HARNESS.md

set -ex

# https://github.com/spesmilo/electrum/issues/7833 (fixed in 4.3)
# possible alt workaround: python3 -m pip install "protobuf>=3.12,<4"
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python

SCRIPT_DIR=$(pwd)

# Electrum-Firo Version 4.1.5.2
COMMIT=a3f64386efc9069cae83e23c241331de6f418b2f

GENESIS=a42b98f04cc2916e8adfb5d9db8a2227c4629bc205748ed2f33180b636ee885b # regtest
RPCPORT=8001
EX_PORT=50002

ASSET_DIR=~/dextest/electrum/firo
ELECTRUM_DIR=${ASSET_DIR}/client
REPO_DIR=${ELECTRUM_DIR}/electrum-repo
WALLET_DIR=${ELECTRUM_DIR}/wallet
NET_DIR=${WALLET_DIR}/regtest
HARNESS_DIR=~/dextest/firo/harness-ctl

# startup options
# CLI, DAEMON, GUI  (Default start as CLI)
# To change the mode, run with e.g.
#   STARTUP=DAEMON ./electrum.sh
ELECTRUM_REGTEST_ARGS="--regtest --dir=${WALLET_DIR}"
WALLET_PASSWORD="abc"

rm -rf ${NET_DIR}/blockchain_headers ${NET_DIR}/forks ${NET_DIR}/certs ${NET_DIR}/wallets/default_wallet
mkdir -p ${NET_DIR}/regtest
mkdir -p ${NET_DIR}/wallets
mkdir -p ${REPO_DIR}

cd ${REPO_DIR}

if [ ! -d "${REPO_DIR}/.git" ]; then
    git init
    git remote add origin https://github.com/firoorg/electrum-firo.git
fi

git remote -v

CURRENT_COMMIT=$(git rev-parse HEAD)
if [ ! "${CURRENT_COMMIT}" == "${COMMIT}" ]; then
    git fetch --depth 1 origin ${COMMIT}
    git reset --hard FETCH_HEAD
fi

if [ ! -d "${ELECTRUM_DIR}/venv" ]; then
    # The venv interpreter will be this python version, e.g. python3.10
    python3 -m venv ${ELECTRUM_DIR}/venv
fi
source ${ELECTRUM_DIR}/venv/bin/activate
python --version
python -m pip install --upgrade pip # can support more versions than ensurepip
pip install -e .
pip install requests cryptography pycryptodomex
if [ "${STARTUP}" == "GUI" ];
then
    pip install pyqt5
fi

cp "${SCRIPT_DIR}/electrum_regtest_wallet" "${NET_DIR}/wallets/default_wallet"

cat > "${NET_DIR}/config" <<EOF
{
    "auto_connect": false,
    "tor_auto_on": false,
    "detect_proxy": false,
    "blockchain_preferred_block": {
        "hash": "${GENESIS}",
        "height": 0
    },
    "check_updates": false,
    "config_version": 3,
    "decimal_point": 8,
    "dont_show_testnet_warning": true,
    "gui_last_wallet": "${NET_DIR}/wallets/default_wallet",
    "is_maximized": false,
    "oneserver": false,
    "recently_open": [
        "${NET_DIR}/wallets/default_wallet"
    ],
    "rpchost": "127.0.0.1",
    "rpcpassword": "pass",
    "rpcport": ${RPCPORT},
    "rpcuser": "user",
    "server": "127.0.0.1:${EX_PORT}:s",
    "show_addresses_tab": true,
    "show_console_tab": true,
    "show_utxo_tab": true,
    "use_rbf": false
}
EOF

cat > "${ASSET_DIR}/client-config.ini" <<EOF
rpcuser=user
rpcpassword=pass
rpcbind=127.0.0.1:${RPCPORT}
EOF

cat > "${HARNESS_DIR}/electrum-cli" <<EOF
source ${ELECTRUM_DIR}/venv/bin/activate
PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python ${REPO_DIR}/electrum-firo ${ELECTRUM_REGTEST_ARGS} "\$@" --password=${WALLET_PASSWORD}
deactivate
EOF
chmod +x "${HARNESS_DIR}/electrum-cli"

if [  "${STARTUP}" == "GUI" ];
then
    echo "Starting GUI wallet"
    ./electrum-firo ${ELECTRUM_REGTEST_ARGS}    
else
    if [  "${STARTUP}" == "DAEMON" ];
    then
        echo "Starting wallet as a DAEMON"
        ./electrum-firo ${ELECTRUM_REGTEST_ARGS} daemon --detach
        ./electrum-firo ${ELECTRUM_REGTEST_ARGS} load_wallet --password=${WALLET_PASSWORD}
        ./electrum-firo ${ELECTRUM_REGTEST_ARGS} list_wallets
        ./electrum-firo ${ELECTRUM_REGTEST_ARGS} getinfo
        echo "use 'stop-daemon' to stop the daemon"
    else
        echo "Starting CLI wallet"
        ./electrum-firo ${ELECTRUM_REGTEST_ARGS} -v daemon
        # Run
        #    ./electrum-cli load_wallet
        # from the firo harness-ctl directory.
    fi
fi
