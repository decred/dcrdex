#!/usr/bin/env bash
# Script for creating dcr wallets, dcr harness should be running before executing.
set -e

# The following are required script arguments
TMUX_WIN_ID=$1
NAME=$2
SEED=$3
RPC_PORT=$4
ENABLE_VOTING=$5
HTTPPROF_PORT=$6

WALLET_DIR="${NODES_ROOT}/${NAME}"
mkdir -p ${WALLET_DIR}

export SHELL=$(which bash)

# Connect to alpha or beta node
DCRD_RPC_PORT="${ALPHA_NODE_RPC_PORT}"
DCRD_RPC_CERT="${NODES_ROOT}/alpha/rpc.cert"
if [ "${NAME}" = "beta" ] || [ "${NAME}" = "alpha-clone" ]; then
  DCRD_RPC_PORT="${BETA_NODE_RPC_PORT}"
  DCRD_RPC_CERT="${NODES_ROOT}/beta/rpc.cert"
fi

# wallet config
cat > "${WALLET_DIR}/${NAME}.conf" <<EOF
simnet=1
nogrpc=1
appdata=${WALLET_DIR}
logdir=${WALLET_DIR}/log
debuglevel=debug
username=${RPC_USER}
password=${RPC_PASS}
rpclisten=127.0.0.1:${RPC_PORT}
rpccert=${WALLET_DIR}/rpc.cert
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:${DCRD_RPC_PORT}
cafile=${DCRD_RPC_CERT}
EOF

if [ "${ENABLE_VOTING}" = "1" ]; then
echo "enablevoting=1" >> "${WALLET_DIR}/${NAME}.conf"
fi

if [ "${ENABLE_VOTING}" = "2" ]; then
cat >> "${WALLET_DIR}/${NAME}.conf" <<EOF
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=6
ticketbuyer.balancetomaintainabsolute=1000
EOF
fi

if [ -n "${HTTPPROF_PORT}" ]; then
echo "profile=127.0.0.1:${HTTPPROF_PORT}" >> "${WALLET_DIR}/${NAME}.conf"
fi

# wallet ctl config
cat > "${WALLET_DIR}/${NAME}-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${WALLET_DIR}/rpc.cert
rpcserver=127.0.0.1:${RPC_PORT}
EOF

# wallet ctl script
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/usr/bin/env bash
dcrctl -C "${WALLET_DIR}/${NAME}-ctl.conf" --wallet \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

# wallet setup data
cat > "${WALLET_DIR}/wallet.answers" <<EOF
y
n
y
${SEED}
EOF

# create and unlock the wallet
tmux new-window -t $TMUX_WIN_ID -n w-"${NAME}" $SHELL
tmux send-keys -t $TMUX_WIN_ID "set +o history" C-m
tmux send-keys -t $TMUX_WIN_ID "cd ${WALLET_DIR}" C-m

echo "Creating simnet ${NAME} wallet"
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C ${NAME}.conf --create < wallet.answers; tmux wait-for -S ${NAME}" C-m
tmux wait-for ${NAME}

echo "Starting simnet ${NAME} wallet"
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C ${NAME}.conf" C-m
