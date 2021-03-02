#!/usr/bin/env bash
# Script for creating eth nodes.
set -e

# The following are required script arguments.
TMUX_WIN_ID=$1
NAME=$2
NODE_PORT=$3
CHAIN_ADDRESS=$4
CHAIN_PASSWORD=$5
CHAIN_ADDRESS_JSON=$6
CHAIN_ADDRESS_JSON_FILE_NAME=$7
ADDRESS_JSON=$8
ADDRESS_JSON_FILE_NAME=$9
NODE_KEY=${10}
SYNC_MODE=${11}

GROUP_DIR="${NODES_ROOT}/${NAME}"
MINE_JS="${GROUP_DIR}/mine.js"
NODE_DIR="${GROUP_DIR}/node"
mkdir -p "${NODE_DIR}"

# Write node ctl script.
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/usr/bin/env bash
geth --datadir="${NODE_DIR}" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

# Write mine script if CHAIN_ADDRESS is present.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  cat > "${NODES_ROOT}/harness-ctl/mine-${NAME}" <<EOF
#!/usr/bin/env bash
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    "${NODES_ROOT}/harness-ctl/${NAME}" attach --preload "${MINE_JS}" --exec 'mine()'
  done
EOF
  chmod +x "${NODES_ROOT}/harness-ctl/mine-${NAME}"

  # Write mining javascript.
  # NOTE: This sometimes mines more than one block. It is a race. This returns
  # the number of blocks mined within the lifespan of the function, but one more
  # MAY be mined after returning.
  cat > "${MINE_JS}" <<EOF
function mine() {
  blkN = eth.blockNumber;
  miner.start();
  miner.stop();
  admin.sleep(1.1);
  return eth.blockNumber - blkN;
}
EOF

  # Write password file to unlock accounts later.
  cat > "${GROUP_DIR}/password" <<EOF
$CHAIN_PASSWORD
EOF

fi

cat > "${NODE_DIR}/eth.conf" <<EOF
[Eth]
NetworkId = 42
SyncMode = "${SYNC_MODE}"

[Eth.Ethash]
DatasetDir = "${NODE_DIR}/.ethash"

[Node]
DataDir = "${NODE_DIR}"

[Node.P2P]
NoDiscovery = true
BootstrapNodes = []
BootstrapNodesV5 = []
ListenAddr = ":${NODE_PORT}"
NetRestrict = [ "127.0.0.1/8", "::1/128" ]
EOF

# Add etherbase if mining.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  cat >> "${NODE_DIR}/eth.conf" <<EOF

[Eth.Miner]
Etherbase = "0x${CHAIN_ADDRESS}"
GasPrice = 81000000000
EOF
fi

# Create a tmux window.
tmux new-window -t "$TMUX_WIN_ID" -n "${NAME}" "${SHELL}"
tmux send-keys -t "$TMUX_WIN_ID" "set +o history" C-m
tmux send-keys -t "$TMUX_WIN_ID" "cd ${NODE_DIR}" C-m

# Create and wait for a node initiated with a predefined genesis json.
echo "Creating simnet ${NAME} node"
tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} init "\
	"$GENESIS_JSON_FILE_LOCATION; tmux wait-for -S ${NAME}" C-m
tmux wait-for "${NAME}"

# Create two accounts. The first is used to mine blocks. The second contains
# funds.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  echo "Creating account"
  cat > "${NODE_DIR}/keystore/$CHAIN_ADDRESS_JSON_FILE_NAME" <<EOF
$CHAIN_ADDRESS_JSON
EOF
fi

cat > "${NODE_DIR}/keystore/$ADDRESS_JSON_FILE_NAME" <<EOF
$ADDRESS_JSON
EOF

# The node key lets us control the enode address value.
echo "Setting node key"
cat > "${NODE_DIR}/geth/nodekey" <<EOF
$NODE_KEY
EOF

echo "Starting simnet ${NAME} node"
if [ "${SYNC_MODE}" = "fast" ]; then
  # Start the eth node with both accounts unlocked, listening restricted to
  # localhost, and syncmode set to fast.
  tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} --nodiscover " \
	  "--config ${NODE_DIR}/eth.conf --unlock ${CHAIN_ADDRESS} " \
	  "--password ${GROUP_DIR}/password --light.serve 25" C-m

else
  # Start the eth node listening restricted to localhost, and syncmode set to light.
  tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} --nodiscover " \
	  "--config ${NODE_DIR}/eth.conf" C-m
fi
