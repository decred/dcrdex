#!/usr/bin/env bash
# tmux script that sets up an eth simnet harness. There is only one node in
# --dev mode.
set -ex

# Private key bf557536250f420fd4c4fc5c25925dcf616b121dd99d9b98294d8e89f10f8d32
# Raw pubkey 047220d5c928c8474a1ddf21b99abd431e45fd6570a4d50bb5a26fc7beca86e02832a4131073b51aab4bd70656bc5c862444e9a8ade16023c46757ba148acffdc6
RICH_PUB_KEY="0x7B0E74365D34C02B707B3CD6A9317E4BCF757638"

SESSION="eth-harness"

ALPHA_HTTP_PORT="38553"
# ALPHA_WS_PORT="38554" # Listening on http

# TESTING_ADDRESS is used by the client's internal node.
TESTING_ADDRESS="946dfaB1AD7caCFeF77dE70ea68819a30acD4577"

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

export NODES_ROOT=~/dextest/eth

START_DIR=$(pwd)
SIGNJS=${START_DIR}/sign.js

# Ensure we can create the session and that there's not a session already
# running before we nuke the data directory.
tmux new-session -d -s $SESSION "${SHELL}"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}/harness-ctl"
mkdir -p "${NODES_ROOT}/genesis"

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

# Write node ctl script.
cat > "${NODES_ROOT}/harness-ctl/alpha" <<EOF
#!/usr/bin/env bash
geth \$* http://:$ALPHA_HTTP_PORT
EOF
chmod +x "${NODES_ROOT}/harness-ctl/alpha"

cat > "${NODES_ROOT}/harness-ctl/sendtoaddress" <<EOF
#!/usr/bin/env bash

TO=\$1
VALUE=\$2

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)

RAWHEX=\$(node ${SIGNJS} "send" \$TO \$VALUE \$NONCE \$GASPRICE "")

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtoaddress"

cat > "${NODES_ROOT}/harness-ctl/deploy" <<EOF
#!/usr/bin/env bash

DATA=\$1

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)

RAWHEX=\$(node ${SIGNJS} "send" "" 0 \$NONCE \$GASPRICE \$DATA)

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/deploy"

cat > "${NODES_ROOT}/harness-ctl/deployERC20" <<EOF
#!/usr/bin/env bash

DATA=\$1
DECIMALS=\$2

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)


HEXDECIMALS=\$(printf %x \$DECIMALS)
PADDED=\$(printf "%064s" \$HEXDECIMALS | tr ' ' 0)
DATA=\$DATA\$PADDED

RAWHEX=\$(node ${SIGNJS} "send" "" 0 \$NONCE \$GASPRICE \$DATA)

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/deployERC20"

cat > "${NODES_ROOT}/harness-ctl/deployERC20Swap" <<EOF
#!/usr/bin/env bash

DATA=\$1
TOKENADDR=\$2

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)

if [[ \$TOKENADDR == 0x* ]]; then
	TOKENADDR=\${TOKENADDR:2}
fi
PADDED=\$(printf "%064s" \$TOKENADDR | tr ' ' 0)
DATA=\${DATA}\$PADDED

RAWHEX=\$(node ${SIGNJS} "send" "" 0 \$NONCE \$GASPRICE \$DATA)

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/deployERC20Swap"

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

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
kurtosis enclave rm -f eth-simnet
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

echo "Starting nodes"
kurtosis run --enclave eth-simnet github.com/ethpandaops/ethereum-package --args-file ${START_DIR}/simple_chain.yaml

sleep 60

SEND_AMT=5000
echo "Sending 5000 eth to testing."
TEST_TX_HASH=$("${NODES_ROOT}/harness-ctl/sendtoaddress ${TESTING_ADDRESS} ${SEND_AMT})" | sed 's/"//g')
echo "ETH transaction to use in tests is ${TEST_TX_HASH}. Saving to ${NODES_ROOT}/test_tx_hash.txt"
cat > "${NODES_ROOT}/test_tx_hash.txt" <<EOF
${TEST_TX_HASH}
EOF

echo "Sending 5000 eth to bundler"
${NODES_ROOT}/harness-ctl/sendtoaddress ${BUNDLER_ADDRESS} ${SEND_AMT}

echo "Deploying Entrypoint contract."
ENTRYPOINT_CONTRACT_HASH=$(${NODES_ROOT}/harness-ctl/deploy ${ENTRYPOINT_V06} | sed 's/"//g')

echo "Deploying ETHSwapV0 contract."
ETH_SWAP_CONTRACT_HASH_V0=$(${NODES_ROOT}/harness-ctl/deploy ${ETH_SWAP_V0} | sed 's/"//g')

echo "Deploying USDC contract."
TEST_USDC_CONTRACT_HASH=$(${NODES_ROOT}/harness-ctl/deployERC20 ${TEST_TOKEN} 6 | sed 's/"//g')

echo "Deploying USDT contract."
TEST_USDT_CONTRACT_HASH=$(${NODES_ROOT}/harness-ctl/deployERC20 ${TEST_TOKEN} 6 | sed 's/"//g')

echo "Deploying MultiBalance contract."
MULTIBALANCE_CONTRACT_HASH=$(${NODES_ROOT}/harness-ctl/deploy ${MULTIBALANCE_BIN} | sed 's/"//g')

sleep 15

ENTRYPOINT_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ENTRYPOINT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Entrypoint contract address is ${ENTRYPOINT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/entrypoint_contract_address.txt"
cat > "${NODES_ROOT}/entrypoint_contract_address.txt" <<EOF
${ENTRYPOINT_CONTRACT_ADDR}
EOF

echo "Deploying ETHSwap1 contract."
ETH_SWAP_CONTRACT_HASH_V1=$(${NODES_ROOT}/harness-ctl/deploy ${ETH_SWAP_V1} | sed 's/"//g')

sleep 15

ETH_SWAP_CONTRACT_ADDR_V0=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${ETH_SWAP_CONTRACT_HASH_V0}\") " | sed 's/"//g')
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
USDC_SWAP_CONTRACT_HASH_V0=$(${NODES_ROOT}/harness-ctl/deployERC20Swap ${ERC20_SWAP_V0} $TEST_USDC_CONTRACT_ADDR | sed 's/"//g')

TEST_USDT_CONTRACT_ADDR=$("${NODES_ROOT}/harness-ctl/alpha" "attach --preload ${NODES_ROOT}/harness-ctl/contractAddress.js --exec contractAddress(\"${TEST_USDT_CONTRACT_HASH}\")" | sed 's/"//g')
echo "Test USDT contract address is ${TEST_USDT_CONTRACT_ADDR}. Saving to ${NODES_ROOT}/test_usdt_contract_address.txt"
cat > "${NODES_ROOT}/test_usdt_contract_address.txt" <<EOF
${TEST_USDT_CONTRACT_ADDR}
EOF

echo "Deploying ERC20SwapV0 contract for USDT."
USDT_SWAP_CONTRACT_HASH=$(${NODES_ROOT}/harness-ctl/deployERC20Swap ${ERC20_SWAP_V0} ${TEST_USDT_CONTRACT_ADDR} | sed 's/"//g')

cat > "${NODES_ROOT}/harness-ctl/sendtokens" <<EOF
#!/usr/bin/env bash

TOKENADDR=\$1
DECIMALS=\$2
TO=\$3
VALUE=\$4

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)

RAWHEX=\$(node ${SIGNJS} "sendtoken" \$TO \$VALUE \$NONCE \$GASPRICE \$TOKENADDR \$DECIMALS)

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendtokens"

cat > "${NODES_ROOT}/harness-ctl/airdroptokens" <<EOF
#!/usr/bin/env bash

TOKENADDR=\$1
DECIMALS=\$2
VALUE=\$3

NONCE=\$(geth attach --exec "eth.getTransactionCount(\"$RICH_PUB_KEY\", \"pending\")" http://:$ALPHA_HTTP_PORT)
GASPRICE=\$(geth attach --exec "eth.gasPrice" http://:$ALPHA_HTTP_PORT)

RAWHEX=\$(node ${SIGNJS} "airdroptoken" $RICH_PUB_KEY \$VALUE \$NONCE \$GASPRICE \$TOKENADDR \$DECIMALS)

RES=\$(geth attach --exec "eth.sendRawTransaction(\"\$RAWHEX\")" http://:$ALPHA_HTTP_PORT)
echo \$RES
EOF
chmod +x "${NODES_ROOT}/harness-ctl/airdroptokens"

cat > "${NODES_ROOT}/harness-ctl/contractAddress.js" <<EOF
function contractAddress(tx) {
  addr = eth.getTransactionReceipt(tx).contractAddress
  return addr;
}
EOF

cat > "${NODES_ROOT}/harness-ctl/sendUSDC" <<EOF
#!/usr/bin/env bash
./sendtokens ${TEST_USDC_CONTRACT_ADDR} 6 \$1 \$2
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendUSDC"

cat > "${NODES_ROOT}/harness-ctl/sendUSDT" <<EOF
#!/usr/bin/env bash
./sendtokens ${TEST_USDT_CONTRACT_ADDR} 6 \$1 \$2
EOF
chmod +x "${NODES_ROOT}/harness-ctl/sendUSDT"

sleep 15

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
${NODES_ROOT}/harness-ctl/airdroptokens ${TEST_USDC_CONTRACT_ADDR} 6 100000000
${NODES_ROOT}/harness-ctl/airdroptokens ${TEST_USDT_CONTRACT_ADDR} 6 100000000

cd "${NODES_ROOT}/harness-ctl"

TEST_BLOCK1_HASH=$("${NODES_ROOT}/harness-ctl/alpha" "attach --exec eth.getHeaderByNumber(1).hash" | sed 's/"//g')
echo "ETH block 1 hash to use in tests is ${TEST_BLOCK1_HASH}. Saving to ${NODES_ROOT}/test_block1_hash.txt"
cat > "${NODES_ROOT}/test_block1_hash.txt" <<EOF
${TEST_BLOCK1_HASH}
EOF

kurtosis files download eth-simnet el_cl_genesis_data ${NODES_ROOT}/genesis/

# Set up bundler. Assumes the bundler exists in ../eth/bundler.
echo "Setting up bundler"
cd ${HARNESS_DIR}/../eth/bundler
go build
tmux new-window -t $SESSION:1 -n "bundler" $SHELL
tmux send-keys -t $SESSION:1 "cd ${HARNESS_DIR}/bundler" C-m
tmux send-keys -t $SESSION:1 "./bundler --privkey ${BUNDLER_PRIV_KEY}" C-m

# Reenable history and attach to the control session.
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
