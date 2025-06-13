#!/usr/bin/env bash
# Tmux script that sets up an XMR regtest harness with one node 'alpha' and 3
# wallets 'fred', 'bill' & 'charlie'. Charlie also has a View-Only sibling.
#
# There is now a new monero-wallet-rpc server with no attached wallet. This is
# for programmatically creating and using a new wallet. The wallet will be gen-
# erated in "own" directory but can be named whatever you need - maybe "alice",
# "Bob" or "carol"

################################################################################
# Development
################################################################################

# export PATH=$PATH:~/monero-x86_64-linux-gnu-v0.18.4.0

################################################################################
# Monero RPC functions
################################################################################

source monero_functions

################################################################################
# Start up
################################################################################

set -evx

RPC_USER="user"
RPC_PASS="pass"
WALLET_PASS=abc

LOCALHOST="127.0.0.1"

# p2p listen and rpc listen ports for alpha node
export ALPHA_NODE_PORT="18080"
export ALPHA_NODE_RPC_PORT="18081"

# for multinode - not used for singlenode
export ALPHA_NODE="${LOCALHOST}:${ALPHA_NODE_PORT}"

# wallet servers' listen rpc ports
export FRED_WALLET_RPC_PORT="28084"
export BILL_WALLET_RPC_PORT="28184"
export CHARLIE_WALLET_RPC_PORT="28284"
export CHARLIE_VIEW_WALLET_RPC_PORT="28384"
export OWN_WALLET_RPC_PORT="28484"

# wallet seeds, passwords & primary addresses
FRED_WALLET_SEED="vibrate fever timber cuffs hunter terminal dilute losing light because nabbing slower royal brunt gnaw vats fishing tipsy toxic vague oscar fudge mice nasty light"
export FRED_WALLET_NAME="fred"
export FRED_WALLET_PASS=""
export FRED_WALLET_PRIMARY_ADDRESS="494aSG3QY1C4PJf7YyDjFc9n2uAuARWSoGQ3hrgPWXtEjgGrYDn2iUw8WJP5Dzm4GuMkY332N9WfbaKfu5tWM3wk8ZeSEC5"

BILL_WALLET_SEED="zodiac playful artistic friendly ought myriad entrance inroads mural duets enraged furnished tsunami pimple ammo prying january swiftly pulp aunt beer ticket tubes unplugs ammo"
export BILL_WALLET_NAME="bill"
export BILL_WALLET_PASS=""
export BILL_WALLET_PRIMARY_ADDRESS="42xPx5nWhxegefWEzRNoJZWwK7d5ofKoWLG1Gmf8567nJMVR37P1EvqYxqWtfgtYUn8qgSbeAqoLcLKe3seFXV2k5ZSqvQw"

CHARLIE_WALLET_SEED="tilt equip bikini nylon ardent asylum eight vane gyrate venomous dove vortex aztec maul rash lair elope rover lodge neutral lemon eggs mocked mugged equip"
export CHARLIE_WALLET_NAME="charlie"
export CHARLIE_WALLET_PASS=""
export CHARLIE_WALLET_PRIMARY_ADDRESS="453w1dEoNE1HjKzKVpAU14Honzenqs5VKKQWHb7RuNHLa4ekXhXnGhR6RuttNpvjbtDjzy8pTgz5j4ZSsWQqyxSDBVQ4WCk"
export CHARLIE_WALLET_VIEWKEY="ff3bef320b8268cef410b78c91f34dfc995c72fcb1b498f7a732d76a42a9e207"
export CHARLIE_VIEW_WALLET_NAME="charlie_view"

# data dir
NODES_ROOT=~/dextest/xmr
FRED_WALLET_DIR="${NODES_ROOT}/wallets/fred"
BILL_WALLET_DIR="${NODES_ROOT}/wallets/bill"
CHARLIE_WALLET_DIR="${NODES_ROOT}/wallets/charlie"
CHARLIE_VIEW_WALLET_DIR="${NODES_ROOT}/wallets/charlie_view"
OWN_WALLET_DIR="${NODES_ROOT}/wallets/own"
HARNESS_CTL_DIR="${NODES_ROOT}/harness-ctl"
ALPHA_DATA_DIR="${NODES_ROOT}/alpha"
ALPHA_REGTEST_CFG="${ALPHA_DATA_DIR}/alpha.conf"

if [ -d "${NODES_ROOT}" ]; then
  rm -fR "${NODES_ROOT}"
fi
mkdir -p "${FRED_WALLET_DIR}"
mkdir -p "${BILL_WALLET_DIR}"
mkdir -p "${CHARLIE_WALLET_DIR}"
mkdir -p "${CHARLIE_VIEW_WALLET_DIR}"
mkdir -p "${OWN_WALLET_DIR}"
mkdir -p "${HARNESS_CTL_DIR}"
mkdir -p "${ALPHA_DATA_DIR}"
touch    "${ALPHA_REGTEST_CFG}"

# make available from the harness-ctl dir 
cp monero_functions ${HARNESS_CTL_DIR} 

# Background watch mining in window 7 by default:
# 'export NOMINER="1"' or uncomment this line to disable
#NOMINER="1"

################################################################################
# Control Scripts
################################################################################
echo "Writing ctl scripts"

# Daemon info
cat > "${HARNESS_CTL_DIR}/alpha_info" <<EOF
#!/usr/bin/env bash
source monero_functions
get_info ${ALPHA_NODE_RPC_PORT}
EOF
chmod +x "${HARNESS_CTL_DIR}/alpha_info"
# -----------------------------------------------------------------------------

# Send a raw signed tx to monerod
# inputs:
# - tx as hex (from tx_blob returned by <wallet_name>_build_tx)
# - do_not_relay to other nodes - defaults to false
cat > "${HARNESS_CTL_DIR}/alpha_sendrawtransaction" <<EOF
#!/usr/bin/env bash
source monero_functions
sendrawtransaction ${ALPHA_NODE_RPC_PORT} \$1 \$2 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/alpha_sendrawtransaction"
# -----------------------------------------------------------------------------

# Get one or more transaction details from monerod
# inputs:
# - txids as hex string - "hash1,hash2,hash3,..."
# - decode_as_json - defaults to false
cat > "${HARNESS_CTL_DIR}/alpha_get_transactions" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transactions ${ALPHA_NODE_RPC_PORT} \$1 \$2 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/alpha_get_transactions"
# -----------------------------------------------------------------------------

# Get one or more transaction development details from monerod including tx lock time
# inputs:
# - txids as hex string - "hash1,hash2,hash3,..."
cat > "${HARNESS_CTL_DIR}/alpha_get_transactions_details" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transactions ${ALPHA_NODE_RPC_PORT} \$1 true 2>/dev/null | jq '.txs[] | .as_json | fromjson'
EOF
chmod +x "${HARNESS_CTL_DIR}/alpha_get_transactions_details"
# -----------------------------------------------------------------------------

# Mempool info
cat > "${HARNESS_CTL_DIR}/alpha_transaction_pool" <<EOF
#!/usr/bin/env bash
source monero_functions
get_transaction_pool ${ALPHA_NODE_RPC_PORT} 2>/dev/null
EOF
chmod +x "${HARNESS_CTL_DIR}/alpha_transaction_pool"
# -----------------------------------------------------------------------------

# Mine to bill-the-miner
# inputs:
# - number of blocks to mine
cat > "${HARNESS_CTL_DIR}/mine-to-bill" <<EOF
#!/usr/bin/env bash
source monero_functions
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} \$1
sleep 1
EOF
chmod +x "${HARNESS_CTL_DIR}/mine-to-bill"
# -----------------------------------------------------------------------------

# Send funds from fred's primary account address to another address
# inputs:
# - money in atomic units 1e12
# - recipient monero address
# - lock time in blocks after which the money can be spent (defaults to 0; no locking)
cat > "${HARNESS_CTL_DIR}/fred_transfer_to" <<EOF
#!/usr/bin/env bash
source monero_functions
transfer_simple ${FRED_WALLET_RPC_PORT} \$1 \$2 \$3
sleep 0.5
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_transfer_to"
# -----------------------------------------------------------------------------

# Send funds from bill's primary account address to another address
# inputs:
# - money in atomic units 1e12
# - recipient monero address
# - lock time in blocks after which the money can be spent (defaults to 0; no locking)
cat > "${HARNESS_CTL_DIR}/bill_transfer_to" <<EOF
#!/usr/bin/env bash
source monero_functions
transfer_simple ${BILL_WALLET_RPC_PORT} \$1 \$2 \$3
sleep 0.5
EOF
chmod +x "${HARNESS_CTL_DIR}/bill_transfer_to"
# -----------------------------------------------------------------------------

# Send funds from charlie's primary account address to another address
# inputs:
# - money in atomic units 1e12
# - recipient monero address
# - lock time in blocks after which the money can be spent (defaults to 0; no locking)
cat > "${HARNESS_CTL_DIR}/charlie_transfer_to" <<EOF
#!/usr/bin/env bash
source monero_functions
transfer_simple ${CHARLIE_WALLET_RPC_PORT} \$1 \$2 \$3
sleep 0.5
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_transfer_to"
# -----------------------------------------------------------------------------

# Get fred's balance from an account
# input
# - account number - defaults to account 0
cat > "${HARNESS_CTL_DIR}/fred_balance" <<EOF
#!/usr/bin/env bash
source monero_functions
get_balance ${FRED_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_balance"
# -----------------------------------------------------------------------------

# Get bill's balance from an account
# input
# - account number - defaults to account 0
cat > "${HARNESS_CTL_DIR}/bill_balance" <<EOF
#!/usr/bin/env bash
source monero_functions
get_balance ${BILL_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/bill_balance"
# -----------------------------------------------------------------------------

# Get charlie's balance from an account
# input
# - account number - defaults to account 0
cat > "${HARNESS_CTL_DIR}/charlie_balance" <<EOF
#!/usr/bin/env bash
source monero_functions
get_balance ${CHARLIE_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_balance"
# -----------------------------------------------------------------------------

# Update fred's wallet from the daemon latest info
cat > "${HARNESS_CTL_DIR}/fred_refresh_wallet" <<EOF
#!/usr/bin/env bash
source monero_functions
refresh_wallet ${FRED_WALLET_RPC_PORT}
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_refresh_wallet"
# -----------------------------------------------------------------------------

# Update bill's wallet from the daemon latest info
cat > "${HARNESS_CTL_DIR}/bill_refresh_wallet" <<EOF
#!/usr/bin/env bash
source monero_functions
refresh_wallet ${BILL_WALLET_RPC_PORT}
EOF
chmod +x "${HARNESS_CTL_DIR}/bill_refresh_wallet"
# -----------------------------------------------------------------------------

# Update charlie's wallet from the daemon latest info
cat > "${HARNESS_CTL_DIR}/charlie_refresh_wallet" <<EOF
#!/usr/bin/env bash
source monero_functions
refresh_wallet ${CHARLIE_WALLET_RPC_PORT}
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_refresh_wallet"
# -----------------------------------------------------------------------------

# Get incoming transfers to fred's wallet
# input
# - transfer_type - defualts to "all"
cat > "${HARNESS_CTL_DIR}/fred_incoming_transfers" <<EOF
#!/usr/bin/env bash
source monero_functions
incoming_transfers ${FRED_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_incoming_transfers"
# -----------------------------------------------------------------------------

# Get incoming transfers to charlie's wallet
# input
# - transfer_type - defaults to "all"
cat > "${HARNESS_CTL_DIR}/charlie_incoming_transfers" <<EOF
#!/usr/bin/env bash
source monero_functions
incoming_transfers ${CHARLIE_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_incoming_transfers"
# -----------------------------------------------------------------------------

# Export outputs from fred wallet - outputs data hex
# input:
# - all - defaults to true - otherwise only new outputs since the last call
cat > "${HARNESS_CTL_DIR}/fred_export_outputs" <<EOF
#!/usr/bin/env bash
source monero_functions
export_outputs ${FRED_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_export_outputs"
# -----------------------------------------------------------------------------

# Export outputs from charlie wallet - outputs data hex
# input:
# - all - defaults to true - otherwise only new outputs since the last call
cat > "${HARNESS_CTL_DIR}/charlie_export_outputs" <<EOF
#!/usr/bin/env bash
source monero_functions
export_outputs ${CHARLIE_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_export_outputs"
# -----------------------------------------------------------------------------

# Export outputs from charlie view wallet for offline signing - array of key images and ephemeral signatures
# input:
# - all - defaults to true - otherwise only new outputs since the last call .. not useful for view wallet
cat > "${HARNESS_CTL_DIR}/charlie_view_export_outputs" <<EOF
#!/usr/bin/env bash
source monero_functions
export_outputs ${CHARLIE_VIEW_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_view_export_outputs"
# -----------------------------------------------------------------------------

# Export signed key images from fred wallet - array of key images and ephemeral signatures
# input:
# - all - defaults to true - otherwise only new outputs since the last call
cat > "${HARNESS_CTL_DIR}/fred_export_key_images" <<EOF
#!/usr/bin/env bash
source monero_functions
export_key_images ${FRED_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_export_key_images"
# -----------------------------------------------------------------------------

# Export signed key images from charlie wallet - array of key images and ephemeral signatures
# input:
# - all - defaults to true - otherwise only new outputs since the last call
cat > "${HARNESS_CTL_DIR}/charlie_export_key_images" <<EOF
#!/usr/bin/env bash
source monero_functions
export_key_images ${CHARLIE_WALLET_RPC_PORT} \$1
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_export_key_images"
# -----------------------------------------------------------------------------

# Build a signed tx with fred wallet
# inputs
# - money in atomic units 1e12
# - recipient monero address
# - lock time in blocks after which the money can be spent (defaults to 0; no locking)
# outputs:
# - tx_blob
# - tx_hash
cat > "${HARNESS_CTL_DIR}/fred_build_tx" <<EOF
#!/usr/bin/env bash
source monero_functions
transfer_no_relay ${FRED_WALLET_RPC_PORT} \$1 \$2 \$3 | jq -nr '[inputs] | add | .result.tx_blob, .result.tx_hash'
EOF
chmod +x "${HARNESS_CTL_DIR}/fred_build_tx"
# -----------------------------------------------------------------------------

# Build a signed tx with charlie wallet
# inputs
# - money in atomic units 1e12
# - recipient monero address
# - lock time in blocks after which the money can be spent (defaults to 0; no locking)
# outputs:
# - tx_blob
# - tx_hash
cat > "${HARNESS_CTL_DIR}/charlie_build_tx" <<EOF
#!/usr/bin/env bash
source monero_functions
transfer_no_relay ${CHARLIE_WALLET_RPC_PORT} \$1 \$2 \$3 | jq -nr '[inputs] | add | .result.tx_blob, .result.tx_hash'
EOF
chmod +x "${HARNESS_CTL_DIR}/charlie_build_tx"
# -----------------------------------------------------------------------------

# Get basic info on all wallets from env which can be used in the scripts above
cat > "${HARNESS_CTL_DIR}/wallets" <<EOF
#!/usr/bin/env bash
env | grep FRED
env | grep BILL
env | grep CHARLIE
env | grep OWN
EOF
chmod +x "${HARNESS_CTL_DIR}/wallets"
# -----------------------------------------------------------------------------

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
if [ -z "$NOMINER" ]
then
   tmux send-keys -t $SESSION:7 C-c
fi
sleep 0.05
tmux send-keys -t $SESSION:6 C-c
sleep 0.05
tmux send-keys -t $SESSION:5 C-c
sleep 0.05
tmux send-keys -t $SESSION:4 C-c
sleep 0.05
tmux send-keys -t $SESSION:3 C-c
sleep 0.05
tmux send-keys -t $SESSION:2 C-c
sleep 0.05
tmux send-keys -t $SESSION:1 C-c
sleep 0.05
tmux kill-session
sleep 0.05
EOF
chmod +x "${HARNESS_CTL_DIR}/quit"
# -----------------------------------------------------------------------------

# Commands help
cat > "${NODES_ROOT}/harness-ctl/help" <<EOF
#!/usr/bin/env bash
source monero_functions
cmd_help | less
EOF
chmod +x "${HARNESS_CTL_DIR}/help"
# -----------------------------------------------------------------------------

################################################################################
# Configuration Files
################################################################################
echo "Writing alpha node config file ${ALPHA_REGTEST_CFG}"

cat > "${ALPHA_REGTEST_CFG}" <<EOF
regtest=1
offline=1
data-dir=${ALPHA_DATA_DIR}
rpc-bind-ip=127.0.0.1
rpc-bind-port=${ALPHA_NODE_RPC_PORT}
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
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${HARNESS_CTL_DIR}" C-m

################################################################################
# SINGLE NODE
################################################################################

# start alpha node - window 1
echo "starting singlenode alpha with config-file: ${ALPHA_REGTEST_CFG}"

tmux new-window -t $SESSION:1 -n 'alpha' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${ALPHA_DATA_DIR}" C-m

tmux send-keys -t $SESSION:1 "monerod --config-file=${ALPHA_REGTEST_CFG}; tmux wait-for -S alphaxmr" C-m

sleep 5

get_info ${ALPHA_NODE_RPC_PORT}

################################################################################
# WALLET SERVERS
################################################################################

# Start the first wallet server - window 2
echo "starting fred wallet server"

tmux new-window -t $SESSION:2 -n 'fred' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${FRED_WALLET_DIR}" C-m

tmux send-keys -t $SESSION:2 "monero-wallet-rpc \
   --rpc-bind-ip 127.0.0.1 \
   --rpc-bind-port ${FRED_WALLET_RPC_PORT} \
   --wallet-dir ${FRED_WALLET_DIR} \
   --disable-rpc-login \
   --allow-mismatched-daemon-version; tmux wait-for -S fredxmr" C-m

sleep 2

# Start the second wallet server - window 3
echo "starting bill wallet server"

tmux new-window -t $SESSION:3 -n 'bill' $SHELL
tmux send-keys -t $SESSION:3 "set +o history" C-m
tmux send-keys -t $SESSION:3 "cd ${BILL_WALLET_DIR}" C-m

tmux send-keys -t $SESSION:3 "monero-wallet-rpc \
   --rpc-bind-ip 127.0.0.1 \
   --rpc-bind-port ${BILL_WALLET_RPC_PORT} \
   --wallet-dir ${BILL_WALLET_DIR} \
   --disable-rpc-login \
   --allow-mismatched-daemon-version; tmux wait-for -S billxmr" C-m

sleep 2

# Start the third wallet server - window 4
echo "starting bill wallet server"

tmux new-window -t $SESSION:4 -n 'charlie' $SHELL
tmux send-keys -t $SESSION:4 "set +o history" C-m
tmux send-keys -t $SESSION:4 "cd ${CHARLIE_WALLET_DIR}" C-m

tmux send-keys -t $SESSION:4 "monero-wallet-rpc \
   --rpc-bind-ip 127.0.0.1 \
   --rpc-bind-port ${CHARLIE_WALLET_RPC_PORT} \
   --wallet-dir ${CHARLIE_WALLET_DIR} \
   --disable-rpc-login \
   --allow-mismatched-daemon-version; tmux wait-for -S charliexmr" C-m

sleep 2

# Start the fourth wallet server - window 5
echo "starting charlie_view wallet server"

tmux new-window -t $SESSION:5 -n 'charlie_view' $SHELL
tmux send-keys -t $SESSION:5 "set +o history" C-m
tmux send-keys -t $SESSION:5 "cd ${CHARLIE_VIEW_WALLET_DIR}" C-m

tmux send-keys -t $SESSION:5 "monero-wallet-rpc \
   --rpc-bind-ip 127.0.0.1 \
   --rpc-bind-port ${CHARLIE_VIEW_WALLET_RPC_PORT} \
   --wallet-dir ${CHARLIE_VIEW_WALLET_DIR} \
   --disable-rpc-login \
   --allow-mismatched-daemon-version; tmux wait-for -S charlieviewxmr" C-m

sleep 2

# Start the fifth wallet server - window 5
echo "starting Own wallet server"

tmux new-window -t $SESSION:6 -n 'own' $SHELL
tmux send-keys -t $SESSION:6 "set +o history" C-m
tmux send-keys -t $SESSION:6 "cd ${OWN_WALLET_DIR}" C-m

tmux send-keys -t $SESSION:6 "monero-wallet-rpc \
   --rpc-bind-ip 127.0.0.1 \
   --rpc-bind-port ${OWN_WALLET_RPC_PORT} \
   --wallet-dir ${OWN_WALLET_DIR} \
   --disable-rpc-login \
   --allow-mismatched-daemon-version; tmux wait-for -S ownxmr" C-m

sleep 2

################################################################################
# Create the wallets
################################################################################

# from here on we are working in the harness-ctl dir
tmux send-keys -t $SESSION:0 "cd ${HARNESS_CTL_DIR}" C-m

# recreate_fred wallet
restore_deterministic_wallet ${FRED_WALLET_RPC_PORT} "${FRED_WALLET_NAME}" "${FRED_WALLET_PASS}" "${FRED_WALLET_SEED}"
sleep 3

# recreate bill wallet
restore_deterministic_wallet ${BILL_WALLET_RPC_PORT} "${BILL_WALLET_NAME}" "${BILL_WALLET_PASS}" "${BILL_WALLET_SEED}"
sleep 3

# recreate charlie wallet
restore_deterministic_wallet ${CHARLIE_WALLET_RPC_PORT} "${CHARLIE_WALLET_NAME}" "${CHARLIE_WALLET_PASS}" "${CHARLIE_WALLET_SEED}"
sleep 3

# ################################################################################
# # Prepare the wallets
# ################################################################################

# mine 300 blocks to bill's wallet (60 confirmations needed for coinbase)
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 60
sleep 2
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 60
sleep 2
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 60
sleep 2
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 60
sleep 2
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 60

# let bill's wallet catch up - time sensitive: it is abnormal to mine 300 blocks so fast
refresh_wallet ${BILL_WALLET_RPC_PORT} | jq '.'
sleep 3

# Monero block reward 0.6 XMR on regtest

# bill starts with 180 XMR 144 spendable
get_balance ${BILL_WALLET_RPC_PORT}

# transfer some money from bill-the-miner to fred and charlie
for money in 10000000000000 18000000000000 5000000000000 7000000000000 1000000000000 15000000000000 3000000000000 25000000000000
do
    transfer_simple ${BILL_WALLET_RPC_PORT} ${money} ${FRED_WALLET_PRIMARY_ADDRESS}
	sleep 1
	transfer_simple ${BILL_WALLET_RPC_PORT} ${money} ${CHARLIE_WALLET_PRIMARY_ADDRESS}
	sleep 1
    generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 1
    sleep 1
done

# generate charlie view only wallet (no spend key)
generate_from_keys ${CHARLIE_VIEW_WALLET_RPC_PORT} \
   "${CHARLIE_VIEW_WALLET_NAME}" \
   "${CHARLIE_WALLET_PRIMARY_ADDRESS}" \
   "" \
   "${CHARLIE_WALLET_VIEWKEY}" \
   "${CHARLIE_WALLET_PASS}"
sleep 1

refresh_wallet ${FRED_WALLET_RPC_PORT} | jq '.'
sleep 1
refresh_wallet ${BILL_WALLET_RPC_PORT} | jq '.'
sleep 1
refresh_wallet ${CHARLIE_WALLET_RPC_PORT} | jq '.'
sleep 1
refresh_wallet ${CHARLIE_VIEW_WALLET_RPC_PORT} | jq '.'
sleep 1
# mine 10 more blocks to make all fred's and charlie's money spendable (normal tx needs 10 confirmations)
generate ${BILL_WALLET_PRIMARY_ADDRESS} ${ALPHA_NODE_RPC_PORT} 10
# let all the wallets catch up
sleep 3

# Watch miner
if [ -z "$NOMINER" ]
then
  tmux new-window -t $SESSION:7 -n "miner" $SHELL
  tmux send-keys -t $SESSION:7 "cd ${NODES_ROOT}/harness-ctl" C-m
  tmux send-keys -t $SESSION:7 "watch -n 15 ./mine-to-bill 1" C-m
fi

# Re-enable history and attach to the control session.
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux select-window -t $SESSION:0
tmux attach-session -t $SESSION
