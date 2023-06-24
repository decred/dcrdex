#!/usr/bin/env bash

SESSION="polygon-harness"

SOURCE_DIR=$(pwd)
NODES_ROOT=~/dextest/polygon
GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"
HARNESS_DIR=${NODES_ROOT}/harness-ctl
PASSWORD="abc"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${HARNESS_DIR}"

# Shutdown script
cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

CHAIN_ADDRESS="dbb75441459257a919c94426033af44286306739"
CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2023-06-19T10-15-10.217011000Z--dbb75441459257a919c94426033af44286306739"
CHAIN_ADDRESS_JSON='{"address":"dbb75441459257a919c94426033af44286306739","crypto":{"cipher":"aes-128-ctr","ciphertext":"342d44548474bab8654c09c1728ad22c38b1860437ea1083a87debc7233ad5e7","cipherparams":{"iv":"ccf56516614d4ea6bb1ea1cb4d1dcc9d"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"86e92faa7a88fce7fd4f60da6dd45e7b6754e5ff0bbda3afd27a4c54d6a64277"},"mac":"3ede3b539042c7936fea5c9d5da7f6276d565679a34d58014f8afe59be09e1f1"},"id":"49ddfe5e-0f43-40f7-9124-812a95806f2c","version":3}'

ALPHA_ADDRESS="bfd16c3465b1b130e332844b194222d7dd3fc761"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2023-06-19T10-16-28.635853000Z--bfd16c3465b1b130e332844b194222d7dd3fc761"
ALPHA_ADDRESS_JSON='{"address":"bfd16c3465b1b130e332844b194222d7dd3fc761","crypto":{"cipher":"aes-128-ctr","ciphertext":"f49548c8f1368aeefd8c9293bbc640b81c6c8472350c91681d4d5c04990669d9","cipherparams":{"iv":"8ee82b53f4e8e35b641e25236556b2e0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"3f7b263f31e7902aafbfd68d7626bda098108f89d8d3f3149a3b411c79c23f73"},"mac":"6c74b30a74e200f20c3c5928d95492d9972f44c436df8b7c6c0fc251b547af5a"},"id":"1f4379cd-8ab8-4b5e-ad4e-ef9d87553312","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="21547"
ALPHA_AUTHRPC_PORT="61829"
ALPHA_HTTP_PORT="48296"
ALPHA_WS_PORT="34983"
ALPHA_GRPC_PORT="34984"

BETA_ADDRESS="0x43e14CE7116989113a0190554Bd4A26aEF39180F"
BETA_ADDRESS_JSON_FILE_NAME="UTC--2023-06-23T22-46-07.176018696Z--43e14ce7116989113a0190554bd4a26aef39180f"
BETA_ADDRESS_JSON='{"address":"43e14ce7116989113a0190554bd4a26aef39180f","crypto":{"cipher":"aes-128-ctr","ciphertext":"fa21dcc0aa09b463a737eca873a23c2c079ea2696fb5397553a4bee3f8ddee75","cipherparams":{"iv":"fc6f78ed9281b19e255daab0aa23fabe"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"e7d7ba97d207e40f837e2ab543df29e15072e919fb3456878a7dd5052e465b80"},"mac":"87afaa682e9136f38efee6a67e09c568f75c678850b0ca560473751b40a83e93"},"id":"f86f43be-9640-428b-966b-09b606556c66","version":3}'
BETA_NODE_KEY="0f3f23a0f14202da009bd59a96457098acea901986629e54d5be1eea32fc404a"
BETA_ENODE="b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061"
BETA_NODE_PORT="21548"
BETA_AUTHRPC_PORT="61830"
BETA_HTTP_PORT="48297"
BETA_WS_PORT="34985"
BETA_GRPC_PORT="34986"

MODULES=["\"eth\""] # "eth,net,web3,debug,admin,personal,txpool,clique"

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


makenode () {
NAME=$1
HTTP_PORT=$2
WS_PORT=$3
AUTHRPC_PORT=$4
NODE_PORT=$5
SESSION_ID=$6
ADDRESS_JSON_FILE_NAME=$7
ADDRESS_JSON=$8
GRPC_PORT=$9
NODE_KEY=${10}

echo "Creating simnet ${NAME} node"

NODE_DIR="${NODES_ROOT}/${NAME}"
BOR_DIR="${NODE_DIR}/bor"
mkdir -p "${BOR_DIR}/bor" # There is a bor/bor dir we need to write the nodekey to.

cat > "${HARNESS_DIR}/${NAME}" <<EOF
#!/usr/bin/env bash
bor attach "${BOR_DIR}/bor.ipc" \$*
EOF
chmod +x "${HARNESS_DIR}/${NAME}"

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
  echo "${HARNESS_DIR}/${NAME} --exec 'eth.blockNumber')"
  BEFORE=\$("${HARNESS_DIR}/${NAME}" --exec 'eth.blockNumber')
  "${HARNESS_DIR}/${NAME}" --exec 'miner.start()' > /dev/null
  sleep \$(echo "\$NUM-1.8" | bc)
  "${HARNESS_DIR}/${NAME}" --exec 'miner.stop()' > /dev/null
  sleep 4
  AFTER=\$("${HARNESS_DIR}/${NAME}" --exec 'eth.blockNumber')
  DIFF=\$((AFTER-BEFORE))
  echo "Mined \$DIFF blocks on ${NAME}. Their hashes:"
  for i in \$(seq \$((BEFORE+1)) \$AFTER)
  do
    echo \$i
    "${HARNESS_DIR}/${NAME}" --exec 'eth.getHeaderByNumber('\$i')'
  done
EOF
  chmod +x "${HARNESS_DIR}/mine-${NAME}"
fi

cat > "${NODE_DIR}/bor.toml" <<EOF
chain = "${GENESIS_JSON_FILE_LOCATION}"
identity = "${NAME}"
verbosity = 5
vmdebug = true
datadir = "${BOR_DIR}"
ancient = "${BOR_DIR}/geth-ancient"
keystore = "${BOR_DIR}/keystore"
ethstats = ""
devfakeauthor = true

[p2p]
  maxpeers = 50
  maxpendpeers = 50
  bind = "127.0.0.1"
  port = ${NODE_PORT}
  nodiscover = true
  nat = "none"
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
  ipcpath = "${BOR_DIR}/bor.ipc"
  gascap = 50000000
  evmtimeout = "5s"
  txfeecap = 5.0
  allow-unprotected-txs = false
  [jsonrpc.http]
    enabled = true
    port = ${HTTP_PORT}
    host = "localhost"
    api = ${MODULES}
    vhosts = ["localhost"]
    corsdomain = ["localhost"]
  [jsonrpc.ws]
    enabled = true
    port = ${WS_PORT}
    host = "localhost"
    api = ${MODULES}
    origins = ["localhost"]
  [jsonrpc.auth]
    jwtsecret = ""
    addr = "localhost"
    port = ${AUTHRPC_PORT}
    vhosts = ["localhost"]

[accounts]
  unlock = ["${CHAIN_ADDRESS}"]
  password = "${NODE_DIR}/password"
  allow-insecure-unlock = true
  lightkdf = false
  disable-bor-wallet = false

[grpc]
  addr = ":${GRPC_PORT}"

EOF

# The node key lets us control the enode address value.
echo "Setting node key"
cat > "${BOR_DIR}/bor/nodekey" <<EOF
$NODE_KEY
EOF

# Write password file to unlock accounts later.
cat > "${NODE_DIR}/password" <<EOF
$PASSWORD
EOF

tmux new-window -t "${SESSION_ID}" -n "${NAME}" "${SHELL}"
tmux send-keys -t "${SESSION_ID}" "set +o history" C-m
tmux send-keys -t "${SESSION_ID}" "cd ${NODE_DIR}" C-m

# Create two accounts. The first is used to mine blocks. The second contains
# funds.
mkdir -p "${BOR_DIR}/keystore"
if [ "${CHAIN_ADDRESS}" != "_" ]; then
  echo "Creating account"
  cat > "${BOR_DIR}/keystore/${CHAIN_ADDRESS_JSON_FILE_NAME}" <<EOF
$CHAIN_ADDRESS_JSON
EOF
fi

cat > "${BOR_DIR}/keystore/${ADDRESS_JSON_FILE_NAME}" <<EOF
${ADDRESS_JSON}
EOF

# Providing a config file overwrites everything provided via cli so we have to
# provided everything via the config file: See:
# https://vscode.dev/github/maticnetwork/bor/blob/develop/internal/cli/server/command.go#L77
tmux send-keys -t "${SESSION_ID}" "bor server --config ${NODE_DIR}/bor.toml 2>&1 | tee ${NODE_DIR}/${NAME}.log" C-m

}
# END makenode ()

echo "Starting harness"
tmux new-session -d -s "${SESSION}" "${SHELL}"

echo "Creating nodes"
makenode "alpha" "${ALPHA_HTTP_PORT}" "${ALPHA_WS_PORT}" \
         "${ALPHA_AUTHRPC_PORT}" "${ALPHA_NODE_PORT}" "${SESSION}:1" \
         "${ALPHA_ADDRESS_JSON_FILE_NAME}" "${ALPHA_ADDRESS_JSON}" "${ALPHS_GRPC_PORT}" \
         "${ALPHA_NODE_KEY}"
makenode "beta" "${BETA_HTTP_PORT}" "${BETA_WS_PORT}" \
         "${BETA_AUTHRPC_PORT}" "${BETA_NODE_PORT}" "${SESSION}:2" \
         "${BETA_ADDRESS_JSON_FILE_NAME}" "${BETA_ADDRESS_JSON}" "${BETA_GRPC_PORT}" \
         "${BETA_NODE_KEY}"

sleep 3

echo "Connecting nodes"
${HARNESS_DIR}/alpha --exec "admin.addPeer('enode://${BETA_ENODE}@127.0.0.1:$BETA_NODE_PORT')"

echo "Mining some blocks"
"${HARNESS_DIR}/mine-beta" "2"
"${HARNESS_DIR}/mine-alpha" "2"

tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${HARNESS_DIR}" C-m

cat > "${NODES_ROOT}/harness-ctl/send.js" <<EOF
function sendLegacyTx(from, to, value) {
      from = from.startsWith('0x') ? from : '0x' + from
      to = to.startsWith('0x') ? to : '0x' + to
      return personal.sendTransaction({ from, to, value, gasPrice: 2000000000 }, "${PASSWORD}")
}
EOF

cat > "${NODES_ROOT}/harness-ctl/sendtoaddress" <<EOF
#!/usr/bin/env bash
"${HARNESS_DIR}/alpha" "--preload ${NODES_ROOT}/harness-ctl/send.js --exec sendLegacyTx(\"${ALPHA_ADDRESS}\",\"\$1\",\$2*1e18)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtoaddress"

echo "Mining a few more blocks"
${HARNESS_DIR}/mine-alpha 6

TESTING_ADDRESS="C26880A0AF2EA0c7E8130e6EC47Af756465452E8"
SEND_AMT=5000000000000000000000
TEST_TX_HASH=$(${HARNESS_DIR}/alpha --preload "${NODES_ROOT}/harness-ctl/send.js" --exec "sendLegacyTx(\"${ALPHA_ADDRESS}\",\"${TESTING_ADDRESS}\",${SEND_AMT})" | sed 's/"//g')
echo "ETH transaction to use in tests is ${TEST_TX_HASH}. Saving to ${NODES_ROOT}/test_tx_hash.txt"
cat > "${NODES_ROOT}/test_tx_hash.txt" <<EOF
${TEST_TX_HASH}
EOF

TEST_BLOCK10_HASH=$(${HARNESS_DIR}/alpha --exec "eth.getHeaderByNumber(10).hash" | sed 's/"//g')
echo "ETH block 10 hash to use in tests is ${TEST_BLOCK10_HASH}. Saving to ${NODES_ROOT}/test_block10_hash.txt"
cat > "${NODES_ROOT}/test_block10_hash.txt" <<EOF
${TEST_BLOCK10_HASH}
EOF

# TEST_TOKEN_CONTRACT_ADDR=$(${HARNESS_DIR}/alpha --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec "contractAddress(\"${TEST_TOKEN_CONTRACT_HASH}\")" | sed 's/"//g')
# echo "Test Token contract address is ${TEST_TOKEN_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_token_contract_address.txt"
# cat > "${NODES_ROOT}/test_token_contract_address.txt" <<EOF
# ${TEST_TOKEN_CONTRACT_ADDR}
# EOF

tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
