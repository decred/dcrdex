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
mkdir -p "${NODE_DIR}/keystore"
mkdir -p "${NODE_DIR}/geth"

# Write node ctl script.
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/usr/bin/env bash
geth --datadir="${NODE_DIR}" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

# Write mine script if CHAIN_ADDRESS is present.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  # The mining script may end up mining more or less blocks than specified.
  cat > "${NODES_ROOT}/harness-ctl/mine-${NAME}" <<EOF
#!/usr/bin/env bash
  NUM=2
  case \$1 in
      ''|*[!0-9]*|[0-1])  ;;
      *) NUM=\$1 ;;
  esac
  echo "Mining..."
  BEFORE=\$("${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.blockNumber')
  "${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'miner.start()' > /dev/null
  sleep \$(echo "\$NUM-1.8" | bc)
  "${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'miner.stop()' > /dev/null
  sleep 1
  AFTER=\$("${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.blockNumber')
  DIFF=\$((AFTER-BEFORE))
  echo "Mined \$DIFF blocks on ${NAME}. Their headers:"
  for i in \$(seq \$((BEFORE+1)) \$AFTER)
  do
    echo \$i
    "${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.getHeaderByNumber('\$i').hash'
  done
EOF
  chmod +x "${NODES_ROOT}/harness-ctl/mine-${NAME}"

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
if [ "${SYNC_MODE}" = "snap" ]; then
  # Start the eth node with the chain account unlocked, listening restricted to
  # localhost, and our custom configuration file.
  tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} --nodiscover " \
	  "--config ${NODE_DIR}/eth.conf --unlock ${CHAIN_ADDRESS} " \
	  "--password ${GROUP_DIR}/password --light.serve 25 --datadir.ancient " \
	  "${NODE_DIR}/geth-ancient --verbosity 5 --vmdebug 2>&1 | tee " \
	  "${NODE_DIR}/${NAME}.log" C-m

else
  # Start the eth node listening restricted to localhost and our custom
  # configuration file.
  tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} --nodiscover " \
	  "--config ${NODE_DIR}/eth.conf --verbosity 5 2>&1 | tee " \
	  "${NODE_DIR}/${NAME}.log" C-m
fi
