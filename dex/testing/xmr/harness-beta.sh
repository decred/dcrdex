#!/usr/bin/env bash
# Tmux script that sets up an XMR regtest harness with one node 'beta'.
#
# It is for XMR golang development usage alongside the simnet-walletpair tool.
# The design of the golang code is such that it needs only a monerod instance
# and creates and tears down a monero-wallet-rpc process, maybe several times.
# The main harness is overkill for this and could be could be confusing when
# specifically debugging processes as it creates 5 other monero-wallet-rpc's
# which would also show up using tools like 'top' or other process manager.

################################################################################
# Development
################################################################################

# export PATH=$PATH:~/monero-x86_64-linux-gnu-v0.18.4.2

export DUMMY=45GjcbHh1fvEyXEA6mDAKqNDMmy1Gon6CNHrdhp9hghfLXQNQj4J76TLtwYGoooKApWLM7kaZwdAxLycceHmuVcELCSFPHq

################################################################################
# Monero RPC functions
################################################################################

source monero_functions

################################################################################
# Start up
################################################################################

set -evx

# rpc listen port for beta node
export BETA_NODE_RPC_PORT="18081"

# data dir
NODES_ROOT=~/dextest/xmr-beta
HARNESS_CTL_DIR="${NODES_ROOT}/harness-ctl"
BETA_DATA_DIR="${NODES_ROOT}/beta"
BETA_REGTEST_CFG="${BETA_DATA_DIR}/beta.conf"

if [ -d "${NODES_ROOT}" ]; then
  rm -rf "${NODES_ROOT}"
fi
mkdir -p "${HARNESS_CTL_DIR}"
mkdir -p "${BETA_DATA_DIR}"
touch    "${BETA_REGTEST_CFG}"

# make available from the harness-ctl dir 
cp monero_functions ${HARNESS_CTL_DIR}

################################################################################
# Control Scripts
################################################################################

echo "Writing ctl scripts"

# Daemon info
cat > "${HARNESS_CTL_DIR}/beta_info" <<EOF
#!/usr/bin/env bash
source monero_functions
get_info ${BETA_NODE_RPC_PORT}
EOF
chmod +x "${HARNESS_CTL_DIR}/beta_info"
# -----------------------------------------------------------------------------

# Send a raw signed tx to monerod
# inputs:
# - tx as hex (from tx_blob returned by <wallet_name>_build_tx)
# - do_not_relay to other nodes - defaults to false
cat > "${HARNESS_CTL_DIR}/beta_sendrawtransaction" <<EOF
#!/usr/bin/env bash
source monero_functions
sendrawtransaction ${BETA_NODE_RPC_PORT} \$1 \$2 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/beta_sendrawtransaction"
# -----------------------------------------------------------------------------

# Get one or more transaction(s) details from monerod
# inputs:
# - txids as hex string - "hash1,hash2,hash3,..."
# - decode_as_json - defaults to false
cat > "${HARNESS_CTL_DIR}/beta_get_transactions" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transactions ${BETA_NODE_RPC_PORT} \$1 \$2 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/beta_get_transactions"
# -----------------------------------------------------------------------------

# Get one or more transaction(s) development details from monerod including tx lock time
# inputs:
# - txids as hex string - "hash1,hash2,hash3,..."
cat > "${HARNESS_CTL_DIR}/beta_get_transactions_details" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transactions ${BETA_NODE_RPC_PORT} \$1 true 2>/dev/null | jq '.txs[] | .as_json | fromjson'
EOF
chmod +x "${HARNESS_CTL_DIR}/beta_get_transactions_details"
# -----------------------------------------------------------------------------

# Mempool info
cat > "${HARNESS_CTL_DIR}/beta_transaction_pool" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transaction_pool ${BETA_NODE_RPC_PORT} 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/beta_transaction_pool"
# -----------------------------------------------------------------------------

# Mine (generate) to an address
# inputs:
# - mining address (must be valid primary address 4xx...)
# - number of blocks to mine
cat > "${HARNESS_CTL_DIR}/mine-to-address" <<EOF
#!/usr/bin/env bash
source monero_functions
generate \$1 ${BETA_NODE_RPC_PORT} \$2
sleep 1
EOF
chmod +x "${HARNESS_CTL_DIR}/mine-to-address"
# -----------------------------------------------------------------------------

# Start mining
# inputs:
# - mining address (must be valid primary address 4xx...)
# - period in seconds (mine 1 block every n seconds)
#   should be >= 15; real blocks are ~120 seconds
cat > "${HARNESS_CTL_DIR}/start-mining" <<EOF
#!/usr/bin/env bash
source monero_functions
address=\$1
period=\$2
if [ \${period} -lt 15 ]; then
  echo "watch mining period \${period} is too low - please use 15 seconds or greater"
  exit 1
fi
tmux send-keys -t $SESSION:2 "watch -n \${period} ./mine-to-address \${address} 1" C-m
EOF
chmod +x "${HARNESS_CTL_DIR}/start-mining"
# -----------------------------------------------------------------------------

# Stop mining
cat > "${HARNESS_CTL_DIR}/stop-mining" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:2 C-c
EOF
chmod +x "${HARNESS_CTL_DIR}/stop-mining"
# -----------------------------------------------------------------------------

# Shutdown
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:2 C-c
sleep 0.05
tmux send-keys -t $SESSION:1 C-c
sleep 0.05
tmux kill-session
sleep 0.05
EOF
chmod +x "${HARNESS_CTL_DIR}/quit"
# -----------------------------------------------------------------------------

################################################################################
# Daemon Configuration File
################################################################################

echo "Writing beta node config file ${BETA_REGTEST_CFG}"

cat > "${BETA_REGTEST_CFG}" <<EOF
regtest=1
offline=1
data-dir=${BETA_DATA_DIR}
rpc-bind-ip=127.0.0.1
rpc-bind-port=${BETA_NODE_RPC_PORT}
fixed-difficulty=1
log-level=1
EOF

################################################################################
# Start tmux harness
################################################################################

echo "Starting harness"

SESSION="xmr-harness"

tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 "harness-ctl"

# window 0
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${HARNESS_CTL_DIR}" C-m

################################################################################
# BETA Node
################################################################################

# start beta node - window 1
echo "starting beta node with config-file: ${BETA_REGTEST_CFG}"

tmux new-window -t $SESSION:1 -n 'beta' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${BETA_DATA_DIR}" C-m

tmux send-keys -t $SESSION:1 "monerod --config-file=${BETA_REGTEST_CFG}; tmux wait-for -S betaxmr" C-m

sleep 2

get_info ${BETA_NODE_RPC_PORT}

################################################################################
# Background Mining
################################################################################

# Watch miner - window 2
# use 'start-mining address period' to start
tmux new-window -t $SESSION:2 -n "miner" $SHELL
tmux send-keys -t $SESSION:2 "echo Mining address is currently limbo: ${DUMMY}" C-m
tmux send-keys -t $SESSION:2 "cd ${NODES_ROOT}/harness-ctl" C-m

################################################################################
# Run
################################################################################

# Re-enable history and attach to the control session.
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux select-window -t $SESSION:0
tmux attach-session -t $SESSION
