#!/usr/bin/env bash

SESSION="polygon-harness"

NODES_ROOT=~/dextest/polygon
GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"
PASSWORD="abc"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

POLYGON_TEST_DIR=$(
  cd $(dirname "$0")
  pwd
)

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

ALPHA_ADDRESS="18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-28T08-47-02.993754951Z--18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON='{"address":"18d65fb8d60c1199bb1ad381be47aa692b482605","crypto":{"cipher":"aes-128-ctr","ciphertext":"927bc2432492fc4bbe9acfe0042f5cd2cef25aff251ac1fb2f420ee85e3b6ee4","cipherparams":{"iv":"89e7333535aed5284abd52f841d30c95"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6fe29ea59d166989be533da62d79802a6b0cef26a9766fa363c7a4bb4c263b5f"},"mac":"c7e2b6c4538c373b2c4e0be7b343db618d39cc68fa872909059357ff36743ca0"},"id":"0e2b9cef-d659-4a26-8739-879129ed0b63","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="21547"
ALPHA_AUTHRPC_PORT="61829"
ALPHA_HTTP_PORT="48296"
ALPHA_WS_PORT="34983"
ALPHA_GRPC_PORT="34984"

BETA_ADDRESS="4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-58.179642501Z--4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON='{"address":"4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06","crypto":{"cipher":"aes-128-ctr","ciphertext":"c5672bb829df9e209ca8ce18dbdd1fed69c603d639e06ab09127b672a609c121","cipherparams":{"iv":"24460eb2934c8b61cee3ad0aa7b843c0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"1f85da881994ca7b4a23f0698da70500a4b79f97a4450b83b129ebf3b4c28f50"},"mac":"1ecea707f1bffa1f6f944cb47e83118d8179e8a5005b83c88610b7e8692a1197"},"id":"56633762-6fb1-4cbf-8396-3a2e4661f7d4","version":3}'
BETA_NODE_KEY="0f3f23a0f14202da009bd59a96457098acea901986629e54d5be1eea32fc404a"
BETA_ENODE="b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061"
BETA_NODE_PORT="21548"
BETA_AUTHRPC_PORT="61830"
BETA_HTTP_PORT="48297"
BETA_WS_PORT="34985"
BETA_GRPC_PORT="34986"

BUNDLER_PRIV_KEY="dcfb54294baf3c746e15a85ca375dc7d5eb97fa7c87f838206daf93eaab2b7cc"
BUNDLER_ADDRESS="0x65797B6518F6694e86efceAdE581d2aC5a22b287"

fileToHex () {
  echo $(xxd -p "$1" | tr -d '\n')
}
ETH_SWAP_V0=$(fileToHex "../../networks/eth/contracts/v0/contract.bin")
ERC20_SWAP_V0=$(fileToHex "../../networks/erc20/contracts/v0/swap_contract.bin")
TEST_TOKEN=$(fileToHex "../../networks/erc20/contracts/v0/token_contract.bin")
MULTIBALANCE_BIN=$(fileToHex "../../networks/eth/contracts/multibalance/contract.bin")
ETH_SWAP_V1=$(fileToHex "../../networks/eth/contracts/v1/contract.bin")
ENTRYPOINT_V06=$(fileToHex "../../networks/eth/contracts/entrypoint/entrypoint.bin")

MODULES='["eth","txpool"]' # "eth,net,web3,debug,admin,personal,txpool,clique"

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
    "18d65fb8d60c1199bb1ad381be47aa692b482605": {
      "balance": "50000000000000000000000"
    },
    "${BUNDLER_ADDRESS}": {
      "balance": "100000000000000000000"
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
NUM=1
case \$1 in
    ''|*[!0-9]*)  ;;
    *) NUM=\$1 ;;
esac
echo "Mining \${NUM} blocks..."
START_BLOCK=\$("${HARNESS_DIR}/${NAME}" --exec 'eth.blockNumber')
# echo "Start Block: \${START_BLOCK}"
CURRENT_BLOCK=\${START_BLOCK}
"${HARNESS_DIR}/${NAME}" --exec 'miner.start()' > /dev/null
DIFF=0
while [ "\${DIFF}" -lt "\${NUM}" ];do
  sleep 0.05
  CURRENT_BLOCK=\$("${HARNESS_DIR}/${NAME}" --exec 'eth.blockNumber')
  DIFF=\$((CURRENT_BLOCK-START_BLOCK))
  # echo "Current Block: \${CURRENT_BLOCK}"
done
"${HARNESS_DIR}/${NAME}" --exec 'miner.stop()' > /dev/null
echo "Blocks Mined: \${DIFF}"
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
         "${ALPHA_ADDRESS_JSON_FILE_NAME}" "${ALPHA_ADDRESS_JSON}" "${ALPHA_GRPC_PORT}" \
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

cat > "${HARNESS_DIR}/send.js" <<EOF
function sendLegacyTx(from, to, value) {
      from = from.startsWith('0x') ? from : '0x' + from
      to = to.startsWith('0x') ? to : '0x' + to
      return personal.sendTransaction({ from, to, value, gasPrice: 2000000000 }, "${PASSWORD}")
}
EOF

cat > "${HARNESS_DIR}/sendtoaddress" <<EOF
#!/usr/bin/env bash
"${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/send.js --exec sendLegacyTx(\"${ALPHA_ADDRESS}\",\"\$1\",\$2*1e18)"
EOF
chmod +x "${HARNESS_DIR}/sendtoaddress"

cat > "${HARNESS_DIR}/deploy.js" <<EOF
function deploy(from, contract) {
  tx = personal.sendTransaction({from:"0x"+from,data:"0x"+contract,gasPrice:82000000000}, "${PASSWORD}")
  return tx;
}

function deployERC20(from, contract, decimals) {
  const hexDecimals = decimals.toString(16);
  const data = "0x" + contract + hexDecimals.padStart(64, "0");
  tx = personal.sendTransaction({from:"0x"+from,data:data,gasPrice:82000000000}, "${PASSWORD}")
  return tx;
}

function deployERC20Swap(from, contract, tokenAddr) {
  if (tokenAddr.slice(0, 2) === "0x") {
    tokenAddr = tokenAddr.slice(2)
  }
  var paddedAddr = tokenAddr.padStart(64, "0")
  tx = personal.sendTransaction({from:"0x"+from,data:"0x"+contract+paddedAddr,gasPrice:82000000000}, "${PASSWORD}")
  return tx;
}
EOF

cat > "${HARNESS_DIR}/contractAddress.js" <<EOF
function contractAddress(tx) {
  addr = eth.getTransactionReceipt(tx).contractAddress
  return addr;
}
EOF

mine_pending_txs() {
  while true
  do
    TXSLEN=$("${HARNESS_DIR}/alpha" "--exec eth.pendingTransactions.length")
    if [ "$TXSLEN" -eq 0 ]; then
      break
    fi
    echo "Waiting for transactions to be mined."
    "${HARNESS_DIR}/mine-alpha" "5"
  done
}

echo "Mining a few more blocks"
${HARNESS_DIR}/mine-alpha 6

SEND_AMT=5000000000000000000000 # 5000 MATIC
TEST_TX_HASH=$(${HARNESS_DIR}/alpha --preload "${HARNESS_DIR}/send.js" --exec "sendLegacyTx(\"${ALPHA_ADDRESS}\",\"${BETA_ADDRESS}\",${SEND_AMT})" | sed 's/"//g')
echo "ETH transaction to use in tests is ${TEST_TX_HASH}. Saving to ${NODES_ROOT}/test_tx_hash.txt"
cat > "${NODES_ROOT}/test_tx_hash.txt" <<EOF
${TEST_TX_HASH}
EOF

TEST_BLOCK10_HASH=$(${HARNESS_DIR}/alpha --exec "eth.getHeaderByNumber(10).hash" | sed 's/"//g')
echo "ETH block 10 hash to use in tests is ${TEST_BLOCK10_HASH}. Saving to ${NODES_ROOT}/test_block10_hash.txt"
cat > "${NODES_ROOT}/test_block10_hash.txt" <<EOF
${TEST_BLOCK10_HASH}
EOF

echo "Deploying Entrypoint contract."
ENTRYPOINT_CONTRACT_HASH=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${ENTRYPOINT_V06}\")" | sed 's/"//g')
echo "Entrypoint contract hash is ${ENTRYPOINT_CONTRACT_HASH}."

echo "Deploying ETHSwapV0 contract."
ETH_SWAP_CONTRACT_HASH_V0=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${ETH_SWAP_V0}\")" | sed 's/"//g')

echo "Deploying USDC contract."
TEST_USDC_CONTRACT_HASH=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deployERC20(\"${ALPHA_ADDRESS}\",\"${TEST_TOKEN}\",6)" | sed 's/"//g')
echo "TEST USDC contract hash is ${TEST_USDC_CONTRACT_HASH}."

echo "Deploying USDT contract."
TEST_USDT_CONTRACT_HASH=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deployERC20(\"${ALPHA_ADDRESS}\",\"${TEST_TOKEN}\",6)" | sed 's/"//g')
echo "TEST USDT contract hash is ${TEST_USDT_CONTRACT_HASH}."

echo "Deploying MultiBalance contract."
MULTIBALANCE_CONTRACT_HASH=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${MULTIBALANCE_BIN}\")" | sed 's/"//g')

mine_pending_txs

ENTRYPOINT_CONTRACT_ADDR=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${ENTRYPOINT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Entrypoint contract address is ${ENTRYPOINT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/entrypoint_contract_address.txt"
cat > "${NODES_ROOT}/entrypoint_contract_address.txt" <<EOF
${ENTRYPOINT_CONTRACT_ADDR}
EOF

echo "Deploying ETHSwapV1 contract."
ETH_SWAP_CONTRACT_HASH_V1=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ETH_SWAP_V1}\",\"${ENTRYPOINT_CONTRACT_ADDR}\")" | sed 's/"//g')

mine_pending_txs

ETH_SWAP_CONTRACT_ADDR_V0=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "ETH SWAP v0 contract address is ${ETH_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/eth_swap_contract_address.txt"
cat > "${NODES_ROOT}/eth_swap_contract_address.txt" <<EOF
${ETH_SWAP_CONTRACT_ADDR_V0}
EOF

ETH_SWAP_CONTRACT_ADDR_V1=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V1}\")" | sed 's/"//g')
echo "ETH SWAP v1 contract address is ${ETH_SWAP_CONTRACT_ADDR_V1}. Saving to ${NODES_ROOT}/eth_swap_contract_address.txt"
cat > "${NODES_ROOT}/eth_swap_contract_address_v1.txt" <<EOF
${ETH_SWAP_CONTRACT_ADDR_V1}
EOF

TEST_USDC_CONTRACT_ADDR=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${TEST_USDC_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDC contract address is ${TEST_USDC_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdc_contract_address.txt"
cat > "${NODES_ROOT}/test_usdc_contract_address.txt" <<EOF
${TEST_USDC_CONTRACT_ADDR}
EOF

TEST_USDT_CONTRACT_ADDR=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${TEST_USDT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDT contract address is ${TEST_USDT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdt_contract_address.txt"
cat > "${NODES_ROOT}/test_usdt_contract_address.txt" <<EOF
${TEST_USDT_CONTRACT_ADDR}
EOF

cat > "${NODES_ROOT}/harness-ctl/loadTestToken.js" <<EOF
    // This ABI comes from running 'solc --abi TestToken.sol'
    const testTokenABI = [{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"airdrop","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"user","type":"address"},{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"testApprove","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"sender","type":"address"},{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}]
    testTokenABI.forEach((el) => {
        if (el.stateMutability === "view") {
            // the constant field was deprecated and the output from solc no
            // longer contains it, but the geth console version of web3 still
            // seems to require it.
            el.constant = true;
        }
    })
    var contract = web3.eth.contract(testTokenABI)
    web3.eth.defaultAccount = "0x${ALPHA_ADDRESS}"
    personal.unlockAccount(web3.eth.defaultAccount, '${PASSWORD}')

    function transfer (tokenAddr, decimals, addr, val) {
      addr = addr.startsWith('0x') ? addr : '0x'+addr
      var testToken = contract.at(tokenAddr)
      console.log(web3.eth.defaultAccount);
      return testToken.transfer(addr, val*Math.pow(10, decimals))
    }
EOF

cat > "${HARNESS_DIR}/alphaWithToken.sh" <<EOF
  # The testToken variable will provide access to the deployed test token contract.
  ./alpha --preload loadTestToken.js
EOF
chmod +x "${HARNESS_DIR}/alphaWithToken.sh"

cat > "${HARNESS_DIR}/sendUSDC" <<EOF
#!/usr/bin/env bash
./alpha --preload loadTestToken.js --exec "transfer(\"${TEST_USDC_CONTRACT_ADDR}\",6,\"\$1\",\$2)"
EOF
chmod +x "${HARNESS_DIR}/sendUSDC"

echo "Deploying ERC20SwapV0 contract for USDC."
USDC_SWAP_CONTRACT_HASH_V0=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ERC20_SWAP_V0}\",\"${TEST_USDC_CONTRACT_ADDR}\")" | sed 's/"//g')

cat > "${HARNESS_DIR}/sendUSDT" <<EOF
#!/usr/bin/env bash
./alpha --preload loadTestToken.js --exec "transfer(\"${TEST_USDT_CONTRACT_ADDR}\",6,\"\$1\",\$2)"
EOF
chmod +x "${HARNESS_DIR}/sendUSDT"

echo "Deploying ERC20SwapV0 contract for USDT."
USDT_SWAP_CONTRACT_HASH=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ERC20_SWAP_V0}\",\"${TEST_USDT_CONTRACT_ADDR}\")" | sed 's/"//g')


mine_pending_txs

USDC_SWAP_CONTRACT_ADDR_V0=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${USDC_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "USDC SWAP v0 contract address is ${USDC_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/usdc_swap_contract_address.txt"
cat > "${NODES_ROOT}/usdc_swap_contract_address.txt" <<EOF
${USDC_SWAP_CONTRACT_ADDR_V0}
EOF

USDT_SWAP_CONTRACT_ADDR=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${USDT_SWAP_CONTRACT_HASH}\")" | sed 's/"//g')
echo "USDT SWAP contract address is ${USDT_SWAP_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/usdt_swap_contract_address.txt"
cat > "${NODES_ROOT}/usdt_swap_contract_address.txt" <<EOF
${USDT_SWAP_CONTRACT_ADDR}
EOF

MULTIBALANCE_CONTRACT_ADDR=$("${HARNESS_DIR}/alpha" "--preload ${HARNESS_DIR}/contractAddress.js --exec contractAddress(\"${MULTIBALANCE_CONTRACT_HASH}\")" | sed 's/"//g')
echo "MultiBalance contract address is ${MULTIBALANCE_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/multibalance_address.txt"
cat > "${NODES_ROOT}/multibalance_address.txt" <<EOF
${MULTIBALANCE_CONTRACT_ADDR}
EOF

# Miner
tmux new-window -t $SESSION:3 -n "miner" $SHELL
tmux send-keys -t $SESSION:3 "cd ${NODES_ROOT}/harness-ctl" C-m
tmux send-keys -t $SESSION:3 "watch -n 15 ./mine-alpha 1" C-m

# Set up bundler
echo "Setting up bundler"
cd ${POLYGON_TEST_DIR}/../eth/bundler
go build
tmux new-window -t $SESSION:6 -n "bundler" $SHELL
tmux send-keys -t $SESSION:6 "cd ${HARNESS_DIR}/bundler" C-m
tmux send-keys -t $SESSION:6 "./bundler --privkey ${BUNDLER_PRIV_KEY} --chain polygon" C-m

tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
