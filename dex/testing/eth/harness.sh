#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. It sets up four separate nodes.
# alpha and beta nodes are synced in snap mode. They emulate nodes used by the
# dcrdex server. Either has the authority to mine blocks. They start with
# pre-allocated funds. gamma and delta are synced in light mode and emulate
# nodes used by bisonw. They are sent some funds after being created. The
# harness waits for all nodes to sync before allowing tmux input.
set -ex

SESSION="eth-harness"

CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-38.123221057Z--9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS="9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS_JSON='{"address":"9ebba10a6136607688ca4f27fab70e23938cd027","crypto":{"cipher":"aes-128-ctr","ciphertext":"dcfbe17de6f315c732855111b782496d76b2d703169afddaaa69e1bc9e02ec51","cipherparams":{"iv":"907e5e050649d1c5c0be782ec7db5cf1"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"060f4e16d601069a6bccae0693a15cd72090baf1ab20e408c89883117d4f7c51"},"mac":"b9ca7dad75a04b77dc7751a814c051f32752603334e4bb4046caf927196a5579"},"id":"74805e39-6a2f-46eb-8125-70c41d12c6d9","version":3}'

ALPHA_ADDRESS="18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-28T08-47-02.993754951Z--18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON='{"address":"18d65fb8d60c1199bb1ad381be47aa692b482605","crypto":{"cipher":"aes-128-ctr","ciphertext":"927bc2432492fc4bbe9acfe0042f5cd2cef25aff251ac1fb2f420ee85e3b6ee4","cipherparams":{"iv":"89e7333535aed5284abd52f841d30c95"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6fe29ea59d166989be533da62d79802a6b0cef26a9766fa363c7a4bb4c263b5f"},"mac":"c7e2b6c4538c373b2c4e0be7b343db618d39cc68fa872909059357ff36743ca0"},"id":"0e2b9cef-d659-4a26-8739-879129ed0b63","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="30304"
ALPHA_AUTHRPC_PORT="8552"
ALPHA_HTTP_PORT="38556"
ALPHA_WS_PORT="38557"
ALPHA_WS_MODULES="eth"

BETA_ADDRESS="4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-58.179642501Z--4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON='{"address":"4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06","crypto":{"cipher":"aes-128-ctr","ciphertext":"c5672bb829df9e209ca8ce18dbdd1fed69c603d639e06ab09127b672a609c121","cipherparams":{"iv":"24460eb2934c8b61cee3ad0aa7b843c0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"1f85da881994ca7b4a23f0698da70500a4b79f97a4450b83b129ebf3b4c28f50"},"mac":"1ecea707f1bffa1f6f944cb47e83118d8179e8a5005b83c88610b7e8692a1197"},"id":"56633762-6fb1-4cbf-8396-3a2e4661f7d4","version":3}'
BETA_NODE_KEY="0f3f23a0f14202da009bd59a96457098acea901986629e54d5be1eea32fc404a"
BETA_ENODE="b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061"
BETA_NODE_PORT="30305"
BETA_AUTHRPC_PORT="8553"
BETA_HTTP_PORT="38558"
BETA_WS_PORT="38559"
BETA_WS_MODULES="eth,txpool"

# TODO: Light nodes broken as of geth 1.13.4-stable. Enable them when possible.
# TESTING_ADDRESS is used by the client's internal node.
TESTING_ADDRESS="946dfaB1AD7caCFeF77dE70ea68819a30acD4577"
SIMNET_TOKEN_ADDRESS="946dfaB1AD7caCFeF77dE70ea68819a30acD4577"

fileToHex () {
  echo $(xxd -p "$1" | tr -d '\n')
}
ETH_SWAP_V0=$(fileToHex "../../networks/eth/contracts/v0/contract.bin")
ERC20_SWAP_V0=$(fileToHex "../../networks/erc20/contracts/v0/swap_contract.bin")
TEST_TOKEN=$(fileToHex "../../networks/erc20/contracts/v0/token_contract.bin")
MULTIBALANCE_BIN=$(fileToHex "../../networks/eth/contracts/multibalance/contract.bin")
ETH_SWAP_V1=$(fileToHex "../../networks/eth/contracts/v1/contract.bin")
ERC20_SWAP_V1=$(fileToHex "../../networks/erc20/contracts/v1/swap_contract.bin")

# PASSWORD is the password used to unlock all accounts/wallets/addresses.
PASSWORD="abc"

export NODES_ROOT=~/dextest/eth
export GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"

# Ensure we can create the session and that there's not a session already
# running before we nuke the data directory.
tmux new-session -d -s $SESSION "${SHELL}"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

# Write genesis json. ".*Block" fields represent block height where certain
# protocols take effect. "clique" is our proof of authority scheme. One block
# can be mined per second with a signature belonging to the address in
# "extradata". The addresses in the "alloc" field are allocated "balance".
# Values are in wei. 1*10^18 wei is equal to one eth. Addresses are allocated
# 11,000 eth. The addresses belong to alpha and beta nodes and two others are
# used in tests.
cat > "${NODES_ROOT}/genesis.json" <<EOF
{
  "config": {
    "chainId": 42,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "muirGlacierBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "arrowGlacierBlock": 0,
    "grayGlacierBlock": 0,
    "mergeNetSplitBlock": 0,
    "shanghaiBlock": 0,
    "cancunBlock": 0,
    "clique": {
      "period": 1,
      "epoch": 30000
    }
  },
  "difficulty": "1",
  "gasLimit": "30000000",
  "extradata": "0x00000000000000000000000000000000000000000000000000000000000000009ebba10a6136607688ca4f27fab70e23938cd0270000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
  "alloc": {
    "${ALPHA_ADDRESS}": {
        "balance": "100000000000000000000000000"
    }
  }
}
EOF

cat > "${NODES_ROOT}/harness-ctl/send.js" <<EOF
function send(from, to, value) {
  from = from.startsWith('0x') ? from : '0x' + from
  to = to.startsWith('0x') ? to : '0x' + to
  return personal.sendTransaction({ from, to, value, gasPrice: 82000000000 }, "${PASSWORD}")
}
EOF

cat > "${NODES_ROOT}/harness-ctl/sendtoaddress" <<EOF
#!/usr/bin/env bash
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"\$1\",\$2*1e18)"
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtoaddress"

cat > "${NODES_ROOT}/harness-ctl/deploy.js" <<EOF
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

cat > "${NODES_ROOT}/harness-ctl/contractAddress.js" <<EOF
function contractAddress(tx) {
  addr = eth.getTransactionReceipt(tx).contractAddress
  return addr;
}
EOF

# Add node script.
HARNESS_DIR=$(dirname "$0")
cp "${HARNESS_DIR}/create-node.sh" "${NODES_ROOT}/harness-ctl/create-node"

# Reorg script
cat > "${NODES_ROOT}/harness-ctl/reorg" <<EOF
#!/usr/bin/env bash
REORG_NODE="alpha"
VALID_NODE="beta"
REORG_DEPTH=2

if [ "\$1" = "beta" ]; then
  REORG_NODE="beta"
  VALID_NODE="alpha"
fi

if [ "\$2" != "" ]; then
  REORG_DEPTH=\$2
fi

echo "Before alpha, beta best blocks"
./alpha attach --exec 'eth.getBlock(eth.blockNumber)'
./beta attach --exec 'eth.getBlock(eth.blockNumber)'


NODES=('alpha' 'beta')
ENODES=($ALPHA_ENODE $BETA_ENODE)
PORTS=($ALPHA_NODE_PORT $BETA_NODE_PORT)

echo "Disconnecting nodes"
for node in "\${NODES[@]}"
do
for i in {0..1}
  do
    "./\$node" "attach --exec admin.removePeer('enode://\${ENODES[i]}@127.0.0.1:\${PORTS[i]}')"
  done
done

sleep 1

# Uncomment to see the effect of reorgs on transactions.
# "./alpha" "attach --exec personal.sendTransaction({from:eth.accounts[0],to:eth.accounts[1],value:1,gasPrice:82000000000},\\"${PASSWORD}\\")"
# "./beta" "attach --exec personal.sendTransaction({from:eth.accounts[0],to:eth.accounts[1],value:1,gasPrice:82000000000},\\"${PASSWORD}\\")"

"./mine-\$VALID_NODE" \$((REORG_DEPTH + 2))
"./mine-\$REORG_NODE" \$REORG_DEPTH

sleep 1

echo "Connecting nodes"
for node in "\${NODES[@]}"
do
for i in {0..1}
  do
    "./\$node" "attach --exec admin.addPeer('enode://\${ENODES[i]}@127.0.0.1:\${PORTS[i]}')"
  done
done

sleep 1

echo "After alpha, beta best blocks"
./alpha attach --exec 'eth.getBlock(eth.blockNumber)'
./beta attach --exec 'eth.getBlock(eth.blockNumber)'
EOF
chmod +x "${NODES_ROOT}/harness-ctl/reorg"

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:5 C-c
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
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
"${HARNESS_DIR}/create-node.sh" "$SESSION:1" "alpha" "$ALPHA_NODE_PORT" \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON" \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$ALPHA_ADDRESS_JSON" "$ALPHA_ADDRESS_JSON_FILE_NAME" \
	"$ALPHA_NODE_KEY" "snap" "$ALPHA_AUTHRPC_PORT" "$ALPHA_HTTP_PORT" "$ALPHA_WS_PORT" \
	"$ALPHA_WS_MODULES"

echo "Starting simnet beta node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:2" "beta" "$BETA_NODE_PORT" \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON" \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$BETA_ADDRESS_JSON" "$BETA_ADDRESS_JSON_FILE_NAME" \
	"$BETA_NODE_KEY" "snap" "$BETA_AUTHRPC_PORT" "$BETA_HTTP_PORT" "$BETA_WS_PORT" \
	"$BETA_WS_MODULES"

# Miner
tmux new-window -t $SESSION:5 -n "miner" $SHELL
tmux send-keys -t $SESSION:5 "cd ${NODES_ROOT}/harness-ctl" C-m
tmux send-keys -t $SESSION:5 "watch -n 15 ./mine-alpha 1" C-m

sleep 1

# NOTE: Connecting a node will add for both. Also, light nodes take longer to
# set up. They will show 0 peers for some amount of time even after adding here.
echo "Connecting nodes"
"${NODES_ROOT}/harness-ctl/alpha" "attach --exec admin.addPeer('enode://${BETA_ENODE}@127.0.0.1:$BETA_NODE_PORT')"

echo "Mining some blocks"
# NOTE: These first couple of blocks will cause a reorg on one node or the
# other. The reason is unknown. It seems this initial mining and reorg is
# necessary for nodes to start communicating.
"${NODES_ROOT}/harness-ctl/mine-beta" "2"
"${NODES_ROOT}/harness-ctl/mine-alpha" "2"

SEND_AMT=5000000000000000000000
echo "Sending 5000 eth to beta and testing."
"${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"${BETA_ADDRESS}\",${SEND_AMT})"
TEST_TX_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/send.js --exec send(\"${ALPHA_ADDRESS}\",\"${TESTING_ADDRESS}\",${SEND_AMT})" | sed 's/"//g')
echo "ETH transaction to use in tests is ${TEST_TX_HASH}. Saving to ${NODES_ROOT}/test_tx_hash.txt"
cat > "${NODES_ROOT}/test_tx_hash.txt" <<EOF
${TEST_TX_HASH}
EOF

echo "Deploying ETHSwapV0 contract."
ETH_SWAP_CONTRACT_HASH_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${ETH_SWAP_V0}\")" | sed 's/"//g')

echo "Deploying ETHSwap1 contract."
ETH_SWAP_CONTRACT_HASH_V1=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${ETH_SWAP_V1}\")" | sed 's/"//g')

echo "Deploying USDC contract."
TEST_USDC_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20(\"${ALPHA_ADDRESS}\",\"${TEST_TOKEN}\",6)" | sed 's/"//g')

echo "Deploying USDT contract."
TEST_USDT_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20(\"${ALPHA_ADDRESS}\",\"${TEST_TOKEN}\",6)" | sed 's/"//g')

echo "Deploying MultiBalance contract."
MULTIBALANCE_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deploy(\"${ALPHA_ADDRESS}\",\"${MULTIBALANCE_BIN}\")" | sed 's/"//g')

mine_pending_txs() {
  while true
  do
    TXSLEN=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.pendingTransactions.length")
    if [ "$TXSLEN" -eq 0 ]; then
      break
    fi
    echo "Waiting for transactions to be mined."
    "${NODES_ROOT}/harness-ctl/mine-alpha" "5"
  done
}

mine_pending_txs

ETH_SWAP_CONTRACT_ADDR_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "ETH SWAP contract address is ${ETH_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/eth_swap_contract_address_v0.txt"
cat > "${NODES_ROOT}/eth_swap_contract_address_v0.txt" <<EOF
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
USDC_SWAP_CONTRACT_HASH_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ERC20_SWAP_V0}\",\"${TEST_USDC_CONTRACT_ADDR}\")" | sed 's/"//g')

echo "Deploying v1 ERC20SwapV0 contract for USDC."
USDC_SWAP_CONTRACT_HASH_V1=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ERC20_SWAP_V1}\",\"${TEST_USDC_CONTRACT_ADDR}\")" | sed 's/"//g')

TEST_USDT_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${TEST_USDT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDT contract address is ${TEST_USDT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdt_contract_address.txt"
cat > "${NODES_ROOT}/test_usdt_contract_address.txt" <<EOF
${TEST_USDT_CONTRACT_ADDR}
EOF

echo "Deploying ERC20SwapV0 contract for USDT."
USDT_SWAP_CONTRACT_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/deploy.js --exec deployERC20Swap(\"${ALPHA_ADDRESS}\",\"${ERC20_SWAP_V0}\",\"${TEST_USDT_CONTRACT_ADDR}\")" | sed 's/"//g')

mine_pending_txs

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
    web3.eth.defaultAccount = web3.eth.accounts.length > 1 ? web3.eth.accounts[1] : web3.eth.accounts[0]
    personal.unlockAccount(web3.eth.defaultAccount, '${PASSWORD}')

    function transfer (tokenAddr, decimals, addr, val) {
      addr = addr.startsWith('0x') ? addr : '0x'+addr
      var testToken = contract.at(tokenAddr)
      return testToken.transfer(addr, val*(10**decimals))
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

USDC_SWAP_CONTRACT_ADDR_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${USDC_SWAP_CONTRACT_HASH_V0}\")" | sed 's/"//g')
echo "USDC v0 swap contract address is ${USDC_SWAP_CONTRACT_ADDR_V0}. Saving to ${NODES_ROOT}/usdc_swap_contract_address_v0.txt"
cat > "${NODES_ROOT}/usdc_swap_contract_address_v0.txt" <<EOF
${USDC_SWAP_CONTRACT_ADDR_V0}
EOF

USDC_SWAP_CONTRACT_ADDR_V1=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${USDC_SWAP_CONTRACT_HASH_V1}\")" | sed 's/"//g')
echo "USDC v1 swap contract address is ${USDC_SWAP_CONTRACT_ADDR_V1}. Saving to ${NODES_ROOT}/usdc_swap_contract_address_v1.txt"
cat > "${NODES_ROOT}/usdc_swap_contract_address_v1.txt" <<EOF
${USDC_SWAP_CONTRACT_ADDR_V1}
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

cd "${NODES_ROOT}/harness-ctl"

mine_pending_txs

TEST_BLOCK10_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.getHeaderByNumber(10).hash" | sed 's/"//g')
echo "ETH block 10 hash to use in tests is ${TEST_BLOCK10_HASH}. Saving to ${NODES_ROOT}/test_block10_hash.txt"
cat > "${NODES_ROOT}/test_block10_hash.txt" <<EOF
${TEST_BLOCK10_HASH}
EOF

# Reenable history and attach to the control session.
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
