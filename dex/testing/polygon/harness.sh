#!/usr/bin/env bash

SESSION="polygon-harness"

SOURCE_DIR=$(pwd)
NODES_ROOT=~/dextest/polygon
GENESIS_JSON_FILE_LOCATION="${SOURCE_DIR}/simnet-genesis.json"
HARNESS_DIR=${NODES_ROOT}/harness-ctl

mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${HARNESS_DIR}"

NAME="alpha"
PASSWORD="abc"

cat > "${HARNESS_DIR}/${NAME}" <<EOF
#!/usr/bin/env bash
bor --datadir="${NODE_DIR}" \$*
EOF
chmod +x "${HARNESS_DIR}/${NAME}"

# Shutdown script
cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
# tmux send-keys -t $SESSION:2 C-c
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

GROUP_DIR="${NODES_ROOT}/${NAME}"
NODE_DIR="${GROUP_DIR}/node"

ALPHA_ADDRESS="18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-28T08-47-02.993754951Z--18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON='{"address":"18d65fb8d60c1199bb1ad381be47aa692b482605","crypto":{"cipher":"aes-128-ctr","ciphertext":"927bc2432492fc4bbe9acfe0042f5cd2cef25aff251ac1fb2f420ee85e3b6ee4","cipherparams":{"iv":"89e7333535aed5284abd52f841d30c95"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6fe29ea59d166989be533da62d79802a6b0cef26a9766fa363c7a4bb4c263b5f"},"mac":"c7e2b6c4538c373b2c4e0be7b343db618d39cc68fa872909059357ff36743ca0"},"id":"0e2b9cef-d659-4a26-8739-879129ed0b63","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="10563"
ALPHA_AUTHRPC_PORT="24331"
ALPHA_HTTP_PORT="38556"
ALPHA_WS_PORT="38557"
ALPHA_WS_MODULES="eth"

CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-38.123221057Z--9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS="9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS_JSON='{"address":"9ebba10a6136607688ca4f27fab70e23938cd027","crypto":{"cipher":"aes-128-ctr","ciphertext":"dcfbe17de6f315c732855111b782496d76b2d703169afddaaa69e1bc9e02ec51","cipherparams":{"iv":"907e5e050649d1c5c0be782ec7db5cf1"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"060f4e16d601069a6bccae0693a15cd72090baf1ab20e408c89883117d4f7c51"},"mac":"b9ca7dad75a04b77dc7751a814c051f32752603334e4bb4046caf927196a5579"},"id":"74805e39-6a2f-46eb-8125-70c41d12c6d9","version":3}'

cat > "${NODE_DIR}/polygon.toml" <<EOF
[Eth]
NetworkId = 137000

[Eth.Ethash]
DatasetDir = "${NODE_DIR}/.ethash"

[Node]
DataDir = "${NODE_DIR}"
AuthPort = ${ALPHA_AUTHRPC_PORT}

[Node.P2P]
NoDiscovery = true
BootstrapNodes = []
BootstrapNodesV5 = []
ListenAddr = ":${ALPHA_NODE_PORT}"
NetRestrict = [ "127.0.0.1/8", "::1/128" ]

[Eth.Miner]
Etherbase = "0x${CHAIN_ADDRESS}"
GasFloor = 30000000
GasCeil = 30000000
EOF

# Write mine script if CHAIN_ADDRESS is present.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  # The mining script may end up mining more or less blocks than specified.
  cat > "${HARNESS_DIR}/mine-${NAME}" <<EOF
#!/usr/bin/env bash
  NUM=2
  case \$1 in
      ''|*[!0-9]*|[0-1])  ;;
      *) NUM=\$1 ;;
  esac
  echo "Mining..."
  BEFORE=\$("${HARNESS_DIR}/${NAME}" attach --exec 'eth.blockNumber')
  "${HARNESS_DIR}/${NAME}" attach --exec 'miner.start()' > /dev/null
  sleep \$(echo "\$NUM-1.8" | bc)
  "${HARNESS_DIR}/${NAME}" attach --exec 'miner.stop()' > /dev/null
  sleep 1
  AFTER=\$("${HARNESS_DIR}/${NAME}" attach --exec 'eth.blockNumber')
  DIFF=\$((AFTER-BEFORE))
  echo "Mined \$DIFF blocks on ${NAME}. Their hashes:"
  for i in \$(seq \$((BEFORE+1)) \$AFTER)
  do
    echo \$i
    "${HARNESS_DIR}/${NAME}" attach --exec 'eth.getHeaderByNumber('\$i').hash'
  done
EOF
  chmod +x "${HARNESS_DIR}/mine-${NAME}"

# Write password file to unlock accounts later.
cat > "${GROUP_DIR}/password" <<EOF
$PASSWORD
EOF

fi

echo "Starting harness"
tmux new-session -d -s $SESSION "${SHELL}"
tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${HARNESS_DIR}" C-m

tmux new-window -t "$SESSION:1" -n "${NAME}" "${SHELL}"
tmux send-keys -t "$SESSION:1" "set +o history" C-m
tmux send-keys -t "$SESSION:1" "cd ${NODE_DIR}" C-m

# # Create and wait for a node initiated with a predefined genesis json.
# echo "Creating simnet ${NAME} node"
# tmux send-keys -t "$SESSION:1" "${HARNESS_DIR}/${NAME} init "\
# 	"$GENESIS_JSON_FILE_LOCATION; tmux wait-for -S ${NAME}" C-m
# tmux wait-for "${NAME}"

# Create two accounts. The first is used to mine blocks. The second contains
# funds.
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  echo "Creating account"
  cat > "${NODE_DIR}/keystore/$CHAIN_ADDRESS_JSON_FILE_NAME" <<EOF
$CHAIN_ADDRESS_JSON
EOF
fi

cat > "${NODE_DIR}/keystore/$ALPHA_ADDRESS_JSON_FILE_NAME" <<EOF
$ALPHA_ADDRESS_JSON
EOF

# The node key lets us control the enode address value.
echo "Setting node key"
cat > "${NODE_DIR}/geth/nodekey" <<EOF
$ALPHA_NODE_KEY
EOF

# Start the eth node with the chain account unlocked, listening restricted to
  # localhost, and our custom configuration file.
tmux send-keys -t "$SESSION:1" "bor server --nodiscover " \
    "--chain ${GENESIS_JSON_FILE_LOCATION} --config ${NODE_DIR}/polygon.toml --unlock ${CHAIN_ADDRESS} " \
    "--password ${GROUP_DIR}/password --datadir="${NODE_DIR}" --datadir.ancient " \
    "${NODE_DIR}/geth-ancient --verbosity 5 --vmdebug --http --http.port ${ALPHA_HTTP_PORT} " \
    "--ws --ws.port ${ALPHA_WS_PORT} --ws.api ${ALPHA_WS_MODULES} " \
    "--bor.withoutheimdall --bor.devfakeauthor --dev --allow-insecure-unlock --disable-bor-wallet false " \
    "2>&1 | tee ${NODE_DIR}/${NAME}.log" C-m

tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
