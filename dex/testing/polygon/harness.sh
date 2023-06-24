#!/usr/bin/env bash

SESSION="polygon-harness"

NODES_ROOT=~/dextest/polygon
GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"
HARNESS_DIR=${NODES_ROOT}/harness-ctl

mkdir -p "${NODES_ROOT}/${NAME}"
mkdir -p "${HARNESS_DIR}"

NAME="alpha"
PASSWORD="abc"

# Shutdown script
cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
# tmux send-keys -t $SESSION:2 C-c
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

GROUP_DIR="${NODES_ROOT}/${NAME}"
NODE_DIR="${GROUP_DIR}/bor"
mkdir -p "${NODE_DIR}"

CHAIN_ADDRESS="dbb75441459257a919c94426033af44286306739"
CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2023-06-19T10-15-10.217011000Z--dbb75441459257a919c94426033af44286306739"
CHAIN_ADDRESS_JSON='{"address":"dbb75441459257a919c94426033af44286306739","crypto":{"cipher":"aes-128-ctr","ciphertext":"342d44548474bab8654c09c1728ad22c38b1860437ea1083a87debc7233ad5e7","cipherparams":{"iv":"ccf56516614d4ea6bb1ea1cb4d1dcc9d"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"86e92faa7a88fce7fd4f60da6dd45e7b6754e5ff0bbda3afd27a4c54d6a64277"},"mac":"3ede3b539042c7936fea5c9d5da7f6276d565679a34d58014f8afe59be09e1f1"},"id":"49ddfe5e-0f43-40f7-9124-812a95806f2c","version":3}'

ALPHA_ADDRESS="bfd16c3465b1b130e332844b194222d7dd3fc761"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2023-06-19T10-16-28.635853000Z--bfd16c3465b1b130e332844b194222d7dd3fc761"
ALPHA_ADDRESS_JSON='{"address":"bfd16c3465b1b130e332844b194222d7dd3fc761","crypto":{"cipher":"aes-128-ctr","ciphertext":"f49548c8f1368aeefd8c9293bbc640b81c6c8472350c91681d4d5c04990669d9","cipherparams":{"iv":"8ee82b53f4e8e35b641e25236556b2e0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"3f7b263f31e7902aafbfd68d7626bda098108f89d8d3f3149a3b411c79c23f73"},"mac":"6c74b30a74e200f20c3c5928d95492d9972f44c436df8b7c6c0fc251b547af5a"},"id":"1f4379cd-8ab8-4b5e-ad4e-ef9d87553312","version":3}'
ALPHA_NODE_PORT="21547"
ALPHA_AUTHRPC_PORT="61829"
ALPHA_HTTP_PORT="48296"
ALPHA_WS_PORT="34983"
ALPHA_MODULES=["\"eth\""] # "eth,net,web3,debug,admin,personal,txpool,clique"

# Write genesis json. ".*Block" fields represent block height where certain
# protocols take effect. The addresses in the "alloc" field are allocated
# "balance". Values are in wei. 1*10^18 wei is equal to one MANTIC. Addresses
# are allocated 50,000 MANTIC. The first address belongs to alpha node
# Write genesis json file.
echo "Writing genesis json file".
cat > "${NODES_ROOT}/genesis.json" <<EOF
{
  "config": {
    "chainId": 90001,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip150Hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "bor": {
      "burntContract": {
        "0": "0x000000000000000000000000000000000000dead"
      }
    }
  },
  "gasLimit": "0x989680",
  "difficulty": "0x1",
  "alloc": {
    "bfd16c3465b1b130e332844b194222d7dd3fc761": {
      "balance": "50000000000000000000000"
    },
    "C26880A0AF2EA0c7E8130e6EC47Af756465452E8": {
      "balance": "0x3635c9adc5dea00000"
    },
    "be188D6641E8b680743A4815dFA0f6208038960F": {
      "balance": "0x3635c9adc5dea00000"
    },
    "c275DC8bE39f50D12F66B6a63629C39dA5BAe5bd": {
      "balance": "0x3635c9adc5dea00000"
    },
    "F903ba9E006193c1527BfBe65fe2123704EA3F99": {
      "balance": "0x3635c9adc5dea00000"
    },
    "928Ed6A3e94437bbd316cCAD78479f1d163A6A8C": {
      "balance": "0x3635c9adc5dea00000"
    }
  }
}
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
  BEFORE=\$("${HARNESS_DIR}/${NAME}" attach ${NODE_DIR}/bor.ipc --exec 'eth.blockNumber')
  "${HARNESS_DIR}/${NAME}" attach ${NODE_DIR}/bor.ipc --exec 'miner.start()' > /dev/null
  sleep \$(echo "\$NUM-1.8" | bc)
  "${HARNESS_DIR}/${NAME}" attach ${NODE_DIR}/bor.ipc --exec 'miner.stop()' > /dev/null
  sleep 4
  AFTER=\$("${HARNESS_DIR}/${NAME}" attach ${NODE_DIR}/bor.ipc --exec 'eth.blockNumber')
  DIFF=\$((AFTER-BEFORE))
  echo "Mined \$DIFF blocks on ${NAME}. Their hashes:"
  for i in \$(seq \$((BEFORE+1)) \$AFTER)
  do
    echo \$i
    "${HARNESS_DIR}/${NAME}" attach ${NODE_DIR}/bor.ipc --exec 'eth.getHeaderByNumber('\$i')'
  done
EOF
  chmod +x "${HARNESS_DIR}/mine-${NAME}"
fi

cat > "${NODE_DIR}/bor.toml" <<EOF
chain = "${GENESIS_JSON_FILE_LOCATION}"
identity = "${NAME}"
verbosity = 5
vmdebug = true
datadir = "${NODE_DIR}"
keystore = "${NODE_DIR}/keystore"
ethstats = ""
devfakeauthor = true

[p2p]
  maxpeers = 50
  maxpendpeers = 50
  bind = "0.0.0.0"
  port = 30303
  nodiscover = true
  nat = "any"
  netrestrict = "127.0.0.1/8,::1/128"
  txarrivalwait = "500ms"

[heimdall]
  "bor.without" = true
  "bor.runheimdall" = false
  "bor.useheimdallapp" = false

[miner]
  mine = false
  etherbase = "0x${CHAIN_ADDRESS}"
  extradata = ""
  gaslimit = 30000000
  gasprice = "1000000000"
  recommit = "2m5s"

[jsonrpc]
  ipcdisable = false
  ipcpath = "${NODE_DIR}/bor.ipc"
  gascap = 50000000
  evmtimeout = "5s"
  txfeecap = 5.0
  allow-unprotected-txs = false
  [jsonrpc.http]
    enabled = true
    port = ${ALPHA_HTTP_PORT}
    host = "localhost"
    api = ${ALPHA_MODULES}
    vhosts = ["localhost"]
    corsdomain = ["localhost"]
  [jsonrpc.ws]
    enabled = true
    port = ${ALPHA_WS_PORT}
    host = "localhost"
    api = ${ALPHA_MODULES}
    origins = ["localhost"]
  [jsonrpc.auth]
    jwtsecret = ""
    addr = "localhost"
    port = ${ALPHA_AUTHRPC_PORT}
    vhosts = ["localhost"]

[accounts]
  unlock = ["${CHAIN_ADDRESS}"]
  password = "${GROUP_DIR}/password"
  allow-insecure-unlock = true
  lightkdf = false
  disable-bor-wallet = false

[grpc]
  addr = ":${ALPHA_NODE_PORT}"

EOF

# Write password file to unlock accounts later.
cat > "${GROUP_DIR}/password" <<EOF
$PASSWORD
EOF

echo "Starting harness"
tmux new-session -d -s $SESSION "${SHELL}"
tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${HARNESS_DIR}" C-m

tmux new-window -t "$SESSION:1" -n "${NAME}" "${SHELL}"
tmux send-keys -t "$SESSION:1" "set +o history" C-m
tmux send-keys -t "$SESSION:1" "cd ${NODE_DIR}" C-m

# # Create and wait for a node initiated with a predefined genesis json.
echo "Creating simnet ${NAME} node"
tmux send-keys -t "$SESSION:1" "${HARNESS_DIR}/${NAME} init "\
	"$GENESIS_JSON_FILE_LOCATION; tmux wait-for -S ${NAME}" C-m
tmux wait-for "${NAME}"

# Create two accounts. The first is used to mine blocks. The second contains
# funds.
mkdir -p "${NODE_DIR}/keystore"
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  echo "Creating account"
  cat > "${NODE_DIR}/keystore/$CHAIN_ADDRESS_JSON_FILE_NAME" <<EOF
$CHAIN_ADDRESS_JSON
EOF
fi

cat > "${NODE_DIR}/keystore/$ALPHA_ADDRESS_JSON_FILE_NAME" <<EOF
$ALPHA_ADDRESS_JSON
EOF

cat > "${NODES_ROOT}/harness-ctl/send.js" <<EOF
function sendLegacyTx(from, to, value) {
      from = from.startsWith('0x') ? from : '0x' + from
      to = to.startsWith('0x') ? to : '0x' + to
      return personal.sendTransaction({ from, to, value, gasPrice: 2000000000 }, "${PASSWORD}")
}
EOF

cat > "${NODES_ROOT}/harness-ctl/sendtoaddress" <<EOF
#!/usr/bin/env bash
"${HARNESS_DIR}/${NAME}" "attach ${NODE_DIR}/bor.ipc --preload ${NODES_ROOT}/harness-ctl/send.js --exec sendLegacyTx(\"${ALPHA_ADDRESS}\",\"\$1\",\$2*1e18)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtoaddress"

# Providing a config file overwrites everything provided via cli so we have to
# provided everything via the config file: See:
# https://vscode.dev/github/maticnetwork/bor/blob/develop/internal/cli/server/command.go#L77
tmux send-keys -t "$SESSION:1" "bor server --config ${NODE_DIR}/bor.toml 2>&1 | tee ${NODE_DIR}/${NAME}.log" C-m
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
