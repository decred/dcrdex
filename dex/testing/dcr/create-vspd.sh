#!/usr/bin/env bash
# Script for creating dcr vspd, dcr harness and wallets should be running before executing.
set -e

# The following are required script arguments
TMUX_WIN_ID=$1
PORT=$2
FEE_XPUB=$3

VSPD_DIR="${NODES_ROOT}/vspd"

git clone -b client/v4.0.1 --depth 1 https://github.com/decred/vspd ${VSPD_DIR}

cd ${VSPD_DIR}/cmd/vspadmin
go build
cd ${VSPD_DIR}/cmd/vspd
go build


DCRD_PORT="${ALPHA_NODE_RPC_PORT}"
DCRD_CERT="${NODES_ROOT}/alpha/rpc.cert"
USER="${RPC_USER}"
PASS="${RPC_PASS}"
WALLET_PORT="${VSPD_WALLET_RPC_PORT}"
DCRWALLET_RPC_PORT="${ALPHA_WALLET_RPC_PORT}"
VSPADMIN_BIN="${VSPD_DIR}/cmd/vspadmin/vspadmin"
VSPD_BIN="${VSPD_DIR}/cmd/vspd/vspd"

WALLET_CERT="${NODES_ROOT}/vspdwallet/rpc.cert"

# vspd config
cat > "${VSPD_DIR}/vspd.conf" <<EOF
listen=127.0.0.1:${PORT}
network=simnet
vspfee=2.0
dcrdhost=127.0.0.1:${DCRD_PORT}
dcrduser=${USER}
dcrdpass=${PASS}
dcrdcert=${DCRD_CERT}
wallethost=127.0.0.1:${WALLET_PORT}
walletuser=${USER}
walletpass=${PASS}
walletcert=${WALLET_CERT}
supportemail=www.support.com
adminpass=${PASS}
loglevel=trace
EOF

# start the vspd
tmux new-window -t $TMUX_WIN_ID -n vspd $SHELL
tmux send-keys -t $TMUX_WIN_ID "set +o history" C-m
tmux send-keys -t $TMUX_WIN_ID "cd ${VSPD_DIR}" C-m

echo "Creating simnet vspd database"
tmux send-keys -t $TMUX_WIN_ID "${VSPADMIN_BIN} createdatabase  ${FEE_XPUB} --network=simnet --homedir=${VSPD_DIR}; tmux wait-for -S vspadmin" C-m
tmux wait-for vspadmin

echo "Starting simnet vspd"
tmux send-keys -t $TMUX_WIN_ID "${VSPD_BIN} --homedir=${VSPD_DIR}; tmux wait-for -S vspd" C-m
