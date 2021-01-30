#!/bin/sh
# Script for creating eth nodes.
set -e

# The following are required script arguments
TMUX_WIN_ID=$1
NAME=$2
NODE_PORT=$3
CHAIN_ADDRESS=$4
CHAIN_PASSWORD=$5
CHAIN_ADDRESS_JSON=$6
CHAIN_ADDRESS_JSON_FILE_NAME=$7
ADDRESS=$8
ADDRESS_PASSWORD=$9
ADDRESS_JSON=${10}
ADDRESS_JSON_FILE_NAME=${11}
NODE_KEY=${12}

WALLET_DIR="${NODES_ROOT}/${NAME}"
mkdir -p "${WALLET_DIR}"

# Write wallet ctl script.
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/bin/sh
geth --datadir="${WALLET_DIR}" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

# Write mine script.
cat > "${NODES_ROOT}/harness-ctl/mine-${NAME}" <<EOF
#!/bin/sh
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    ./${NAME} attach --preload "${MINE_JS}" --exec 'mine()'
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-${NAME}"


# Write password file to unlock accounts later.
cat > "${WALLET_DIR}/password" <<EOF
$CHAIN_PASSWORD
$ADDRESS_PASSWORD
EOF

# Create a tmux window.
tmux new-window -t "$TMUX_WIN_ID" -n "${NAME}"
tmux send-keys -t "$TMUX_WIN_ID" "set +o history" C-m
tmux send-keys -t "$TMUX_WIN_ID" "cd ${WALLET_DIR}" C-m

# Create and wait for a wallet initiated with a predefined genesis json.
echo "Creating simnet ${NAME} wallet"
tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} init "\
	"$GENESIS_JSON_FILE_LOCATION; tmux wait-for -S ${NAME}" C-m
tmux wait-for "${NAME}"

# Create two accounts. The first is used to mine blocks. The second contains
# funds.
echo "Creating account"
cat > "${WALLET_DIR}/keystore/$CHAIN_ADDRESS_JSON_FILE_NAME" <<EOF
$CHAIN_ADDRESS_JSON
EOF
cat > "${WALLET_DIR}/keystore/$ADDRESS_JSON_FILE_NAME" <<EOF
$ADDRESS_JSON
EOF

# The node key lets us control the enode address value.
echo "Setting node key"
cat > "${WALLET_DIR}/geth/nodekey" <<EOF
$NODE_KEY
EOF

# Start the eth node with both accounts unlocked, listening restricted to
# localhost, and syncmode set to full.
echo "Starting simnet ${NAME} wallet"
tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} --port " \
	"${NODE_PORT} --nodiscover --unlock ${CHAIN_ADDRESS},${ADDRESS} " \
	"--password ${WALLET_DIR}/password --miner.etherbase ${CHAIN_ADDRESS} " \
	"--syncmode full --netrestrict 127.0.0.1/32" C-m
