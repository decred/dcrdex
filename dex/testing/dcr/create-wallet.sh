#!/bin/sh
# Script for creating dcr wallets, dcr harness should be running before executing.
set -e

# The following are required script arguments
TMUX_WIN_ID=$1
NAME=$2
SEED=$3
RPC_PORT=$4
ENABLE_VOTING=$5

WALLET_DIR="${NODES_ROOT}/${NAME}"
mkdir -p ${WALLET_DIR}

# Connect to alpha or beta node
DCRD_RPC_PORT="19570"
DCRD_RPC_CERT="${NODES_ROOT}/alpha/rpc.cert"
if [ "${NAME}" = "beta" ]; then
  DCRD_RPC_PORT="19569"
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
${VOTING_CFG}
EOF

if [ "${ENABLE_VOTING}" = "1" ]; then
  cat >> "${WALLET_DIR}/${NAME}.conf" <<EOF
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=6
ticketbuyer.balancetomaintainabsolute=1000
EOF
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
#!/bin/sh
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
tmux new-window -t $TMUX_WIN_ID -n "${NAME}"
tmux send-keys -t $TMUX_WIN_ID "set +o history" C-m
tmux send-keys -t $TMUX_WIN_ID "cd ${WALLET_DIR}" C-m

echo "Creating simnet ${NAME} wallet"
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C ${NAME}.conf --create < wallet.answers; tmux wait-for -S ${NAME}" C-m
tmux wait-for ${NAME}

echo "Starting simnet ${NAME} wallet"
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C ${NAME}.conf" C-m
