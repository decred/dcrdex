#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. There is only one node in
# --dev mode.
set -ex

SESSION="eth-harness"

ALPHA_AUTHRPC_PORT="8552"
ALPHA_HTTP_PORT="38556"
ALPHA_WS_PORT="38557"
ALPHA_WS_MODULES="eth"

BUNDLER_PRIV_KEY="dcfb54294baf3c746e15a85ca375dc7d5eb97fa7c87f838206daf93eaab2b7cc"
BUNDLER_ADDRESS="0x65797B6518F6694e86efceAdE581d2aC5a22b287"

# TESTING_ADDRESS is used by the client's internal node.
TESTING_ADDRESS="946dfaB1AD7caCFeF77dE70ea68819a30acD4577"

fileToHex () {
  echo $(xxd -p "$1" | tr -d '\n')
}
ETH_SWAP_V0=$(fileToHex "../../networks/eth/contracts/v0/contract.bin")
ERC20_SWAP_V0=$(fileToHex "../../networks/erc20/contracts/v0/swap_contract.bin")
TEST_TOKEN=$(fileToHex "../../networks/erc20/contracts/v0/token_contract.bin")
MULTIBALANCE_BIN=$(fileToHex "../../networks/eth/contracts/multibalance/contract.bin")
ETH_SWAP_V1=$(fileToHex "../../networks/eth/contracts/v1/contract.bin")
ENTRYPOINT_V06=$(fileToHex "../../networks/eth/contracts/entrypoint/entrypoint.bin")

# PASSWORD is the password used to unlock all accounts/wallets/addresses.
PASSWORD="abc"

export NODES_ROOT=~/dextest/eth

# Ensure we can create the session and that there's not a session already
# running before we nuke the data directory.
tmux new-session -d -s $SESSION "${SHELL}"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/harness-ctl"

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

cat > "${NODES_ROOT}/harness-ctl/send.js" <<EOF
function send(to, value) {
  to = to.startsWith('0x') ? to : '0x' + to
  return eth.sendTransaction({from:eth.accounts[0], to, value})
}
EOF

cat > "${NODES_ROOT}/harness-ctl/sendtoaddress" <<EOF
#!/usr/bin/env bash
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"\$1\",\$2*1e18)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtoaddress"

cat > "${NODES_ROOT}/harness-ctl/deploy.js" <<EOF
function deploy(contract) {
  tx = eth.sendTransaction({from:eth.accounts[0],data:"0x"+contract})
  return tx;
}

function deployERC20(contract, decimals) {
  const hexDecimals = decimals.toString(16);
  const data = "0x" + contract + hexDecimals.padStart(64, "0");
  tx = eth.sendTransaction({from:eth.accounts[0],data:data})
  return tx;
}

function deployERC20Swap(contract, tokenAddr) {
  if (tokenAddr.slice(0, 2) === "0x") {
    tokenAddr = tokenAddr.slice(2)
  }
  var paddedAddr = tokenAddr.padStart(64, "0")
  tx = eth.sendTransaction({from:eth.accounts[0],data:"0x"+contract+paddedAddr})
  return tx;
}
EOF

cat > "${NODES_ROOT}/harness-ctl/contractAddress.js" <<EOF
function contractAddress(tx) {
  addr = eth.getTransactionReceipt(tx).contractAddress
  return addr;
}
EOF

# Add node script.
HARNESS_DIR=$(
  cd $(dirname "$0")
  pwd
)
cp "${HARNESS_DIR}/create-node.sh" "${NODES_ROOT}/harness-ctl/create-node"

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:5 C-c
tmux send-keys -t $SESSION:1 C-c
tmux kill-session
EOF
chmod +x "${NODES_ROOT}/harness-ctl/quit"

################################################################################
# Start harness
################################################################################

tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${NODES_ROOT}/harness-ctl" C-m

################################################################################
# Eth nodes
################################################################################

echo "Starting simnet alpha node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:1" "alpha" \
	"$ALPHA_AUTHRPC_PORT" "$ALPHA_HTTP_PORT" "$ALPHA_WS_PORT" \
	"$ALPHA_WS_MODULES"

sleep 20

SEND_AMT=5000000000000000000000
echo "Sending 5000 eth to testing"
TEST_TX_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${TESTING_ADDRESS}\",${SEND_AMT})" | sed 's/"//g')
echo "ETH transaction to use in tests is ${TEST_TX_HASH}. Saving to ${NODES_ROOT}/test_tx_hash.txt"
cat > "${NODES_ROOT}/test_tx_hash.txt" <<EOF
${TEST_TX_HASH}
EOF

echo "Sending 5000 eth to bundler"
TEST_TX_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${BUNDLER_ADDRESS}\",${SEND_AMT})" | sed 's/"//g')

echo "Deploying Entrypoint contract."
ENTRYPOINT_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ENTRYPOINT_V06}\")" | sed 's/"//g')

echo "Deploying ETHSwapV0 contract."
ETH_SWAP_CONTRACT_HASH_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ETH_SWAP_V0}\")" | sed 's/"//g')

echo "Deploying USDC contract."
TEST_USDC_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20(\"${TEST_TOKEN}\",6)" | sed 's/"//g')

echo "Deploying USDT contract."
TEST_USDT_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20(\"${TEST_TOKEN}\",6)" | sed 's/"//g')

echo "Deploying MultiBalance contract."
MULTIBALANCE_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${MULTIBALANCE_BIN}\")" | sed 's/"//g')

mine_pending_txs() {
  while true
  do
    TXSLEN=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.pendingTransactions.length")
    if [ "$TXSLEN" -eq 0 ]; then
      break
    fi
    echo "Waiting for transactions to be mined."
    sleep 2
  done
}

mine_pending_txs

ENTRYPOINT_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ENTRYPOINT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Entrypoint contract address is ${ENTRYPOINT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/entrypoint_contract_address.txt"
cat > "${NODES_ROOT}/entrypoint_contract_address.txt" <<EOF
${ENTRYPOINT_CONTRACT_ADDR}
EOF

echo "Deploying ETHSwap1 contract."
ETH_SWAP_CONTRACT_HASH_V1=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ETH_SWAP_V1}\",\"${ENTRYPOINT_CONTRACT_ADDR}\")" | sed 's/"//g')

mine_pending_txs

ETH_SWAP_CONTRACT_ADDR_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "ETH SWAP contract address is ${ETH_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/eth_swap_contract_address.txt"
cat > "${NODES_ROOT}/eth_swap_contract_address.txt" <<EOF
${ETH_SWAP_CONTRACT_ADDR_V0}
EOF

ETH_SWAP_CONTRACT_ADDR_V1=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V1}\")" | sed 's/"//g')
echo "ETH SWAP V1 contract address is ${ETH_SWAP_CONTRACT_ADDR_V1}. Saving to ${NODES_ROOT}/eth_swap_contract_address_v1.txt"
cat > "${NODES_ROOT}/eth_swap_contract_address_v1.txt" <<EOF
${ETH_SWAP_CONTRACT_ADDR_V1}
EOF

TEST_USDC_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${TEST_USDC_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDC contract address is ${TEST_USDC_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdc_contract_address.txt"
cat > "${NODES_ROOT}/test_usdc_contract_address.txt" <<EOF
${TEST_USDC_CONTRACT_ADDR}
EOF

echo "Deploying v0 ERC20SwapV0 contract for USDC."
USDC_SWAP_CONTRACT_HASH_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ERC20_SWAP_V0}\",\"${TEST_USDC_CONTRACT_ADDR}\")" | sed 's/"//g')

TEST_USDT_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${TEST_USDT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDT contract address is ${TEST_USDT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdt_contract_address.txt"
cat > "${NODES_ROOT}/test_usdt_contract_address.txt" <<EOF
${TEST_USDT_CONTRACT_ADDR}
EOF

echo "Deploying ERC20SwapV0 contract for USDT."
USDT_SWAP_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ERC20_SWAP_V0}\",\"${TEST_USDT_CONTRACT_ADDR}\")" | sed 's/"//g')

cat > "${NODES_ROOT}/harness-ctl/loadTestToken.js" <<EOF
    // This ABI comes from running 'solc --abi TestToken.sol'
    const testTokenABI = [{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"airdrop","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"user","type":"address"},{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"testApprove","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"sender","type":"address"},{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}]
    var contract = web3.eth.contract(testTokenABI)
    web3.eth.defaultAccount = web3.eth.accounts[0]

    function transfer (tokenAddr, decimals, addr, val) {
      addr = addr.startsWith('0x') ? addr : '0x'+addr
      var testToken = contract.at(tokenAddr)
      return testToken.transfer(addr, val*(10**decimals))
    }

    function airdrop (tokenAddr, amt) {
      var testToken = contract.at(tokenAddr)
      return testToken.airdrop(web3.eth.accounts[0], amt)
    }
EOF


cat > "${NODES_ROOT}/harness-ctl/alphaWithToken.sh" <<EOF
  # The testToken variable will provide access to the deployed test token contract.
  ./alpha --preload loadTestToken.js
EOF
chmod +x "${NODES_ROOT}/harness-ctl/alphaWithToken.sh"

cat > "${NODES_ROOT}/harness-ctl/sendUSDC" <<EOF
#!/usr/bin/env bash
./alpha attach --preload loadTestToken.js --exec "transfer(\"${TEST_USDC_CONTRACT_ADDR}\",6,\"\$1\",\$2)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendUSDC"

cat > "${NODES_ROOT}/harness-ctl/sendUSDT" <<EOF
#!/usr/bin/env bash
./alpha attach --preload loadTestToken.js --exec "transfer(\"${TEST_USDT_CONTRACT_ADDR}\",6,\"\$1\",\$2)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendUSDT"

mine_pending_txs

USDC_SWAP_CONTRACT_ADDR_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${USDC_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "USDC v0 swap contract address is ${USDC_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/usdc_swap_contract_address.txt"
cat > "${NODES_ROOT}/usdc_swap_contract_address.txt" <<EOF
${USDC_SWAP_CONTRACT_ADDR_V0}
EOF

USDT_SWAP_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${USDT_SWAP_CONTRACT_HASH}\")" | sed 's/"//g')
echo "ERC20 SWAP contract address is ${USDT_SWAP_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/usdt_swap_contract_address.txt"
cat > "${NODES_ROOT}/usdt_swap_contract_address.txt" <<EOF
${USDT_SWAP_CONTRACT_ADDR}
EOF

MULTIBALANCE_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${MULTIBALANCE_CONTRACT_HASH}\")" | sed 's/"//g')
echo "MultiBalance contract address is ${MULTIBALANCE_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/multibalance_address.txt"
cat > "${NODES_ROOT}/multibalance_address.txt" <<EOF
${MULTIBALANCE_CONTRACT_ADDR}
EOF

# Add test tokens.
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/loadTestToken.js --exec airdrop(\"${TEST_USDC_CONTRACT_ADDR}\",4400000000000000000)"
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/loadTestToken.js --exec airdrop(\"${TEST_USDT_CONTRACT_ADDR}\",4400000000000000000)"

cd "${NODES_ROOT}/harness-ctl"

TEST_BLOCK1_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.getHeaderByNumber(1).hash" | sed 's/"//g')
echo "ETH block 1 hash to use in tests is ${TEST_BLOCK1_HASH}. Saving to ${NODES_ROOT}/test_block1_hash.txt"
cat > "${NODES_ROOT}/test_block1_hash.txt" <<EOF
${TEST_BLOCK1_HASH}
EOF

# Set up bundler
echo "Setting up bundler"
cd ${HARNESS_DIR}/bundler
go build
tmux new-window -t $SESSION:6 -n "bundler" $SHELL
tmux send-keys -t $SESSION:6 "cd ${HARNESS_DIR}/bundler" C-m
tmux send-keys -t $SESSION:6 "./bundler --privkey ${BUNDLER_PRIV_KEY}" C-m

# Reenable history and attach to the control session.
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
