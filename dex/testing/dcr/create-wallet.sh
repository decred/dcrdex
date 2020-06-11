#!/bin/sh
# Script for creating dcr wallets, dcr harness should be running before executing.
set -e

NODES_ROOT=~/dextest/dcr

# The following are required script arguments
TMUX_WIN_ID=$1
NAME=$2
SEED=$3
PASS=$4
RPC_USER=$5
RPC_PASS=$6
RPC_PORT=$7
ENABLE_VOTING=$8

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
cat > "${WALLET_DIR}/w-${NAME}.conf" <<EOF
simnet=1
nogrpc=1
appdata=${WALLET_DIR}
logdir=${WALLET_DIR}/log
debuglevel=debug
username=${RPC_USER}
password=${RPC_PASS}
rpclisten=127.0.0.1:${RPC_PORT}
rpccert=${WALLET_DIR}/rpc.cert
pass=${PASS}
rpcconnect=127.0.0.1:${DCRD_RPC_PORT}
cafile=${DCRD_RPC_CERT}
${VOTING_CFG}
EOF

if [ "${ENABLE_VOTING}" = "1" ]; then
  cat >> "${WALLET_DIR}/w-${NAME}.conf" <<EOF
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=5
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

# create and unlock the wallet
tmux new-window -t $TMUX_WIN_ID -n "w-${NAME}"
tmux send-keys -t $TMUX_WIN_ID "cd ${WALLET_DIR}" C-m
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C w-${NAME}.conf --create" C-m
echo "Creating simnet ${NAME} wallet"
sleep 1
tmux send-keys -t $TMUX_WIN_ID "${PASS}" C-m "${PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys -t $TMUX_WIN_ID "${SEED}" C-m C-m
tmux send-keys -t $TMUX_WIN_ID "dcrwallet -C w-${NAME}.conf" C-m
