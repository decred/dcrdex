#!/usr/bin/env bash

SYMBOL="zec"
DAEMON="zcashd"
CLI="zcash-cli"
RPC_USER="user"
RPC_PASS="pass"
ALPHA_LISTEN_PORT="33764"
BETA_LISTEN_PORT="33765"
DELTA_LISTEN_PORT="33766"
ALPHA_RPC_PORT="33768"
BETA_RPC_PORT="33769"
DELTA_RPC_PORT="33770"

SHELL=$(which bash)

set -ex
NODES_ROOT=~/dextest/${SYMBOL}
rm -rf "${NODES_ROOT}"
SOURCE_DIR=$(pwd)

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
DELTA_DIR="${NODES_ROOT}/delta"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

mkdir -p "${ALPHA_DIR}"
mkdir -p "${BETA_DIR}"
mkdir -p "${DELTA_DIR}"
mkdir -p "${HARNESS_DIR}"

WALLET_PASSWORD="abc"

ALPHA_CLI_CFG="-rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -rpcwallet=alphawallet -conf=${ALPHA_DIR}/alpha.conf"
BETA_CLI_CFG=" -rpcport=${BETA_RPC_PORT}  -regtest=1 -rpcuser=user -rpcpassword=pass -rpcwallet=betawallet  -conf=${BETA_DIR}/beta.conf"
DELTA_CLI_CFG="-rpcport=${DELTA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -rpcwallet=deltawallet -conf=${DELTA_DIR}/delta.conf"

# macro wait for process termination.
DONE="; tmux wait-for -S ${SYMBOL}"
WAIT="wait-for ${SYMBOL}"

SESSION="${SYMBOL}-harness"

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION $SHELL

###############################################################################
# Write config files.
###############################################################################

echo "Writing node config files"

# Activation logic updated for NU6.1.

# There is zcash logic around a low probability reorg at activation time for
# NU6 and NU6.1 which can reference <activation height -1> so we activate NU6
# at block 5 (2 blocks gap) and NU6.1 at block 7.

cat > "${ALPHA_DIR}/alpha.conf" <<EOF
rpcuser=user
rpcpassword=pass
# txindex=1
port=${ALPHA_LISTEN_PORT}
regtest=1
rpcport=${ALPHA_RPC_PORT}
# Required for 2026 operations
i-am-aware-zcashd-will-be-replaced-by-zebrad-and-zallet-in-2025=1
exportdir=${SOURCE_DIR}
# Upgrades 1-5 (Overwinter through NU5)
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
# NU6 (Block Reward/Lockbox)
nuparams=c8e71055:5
# NU6.1 (Finalized Dev Fund Disbursement)
nuparams=4dec4df0:7
EOF

cat > "${BETA_DIR}/beta.conf" <<EOF
rpcuser=user
rpcpassword=pass
# txindex=1
regtest=1
rpcport=${BETA_RPC_PORT}
i-am-aware-zcashd-will-be-replaced-by-zebrad-and-zallet-in-2025=1
exportdir=${SOURCE_DIR}
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
nuparams=c8e71055:5
nuparams=4dec4df0:7
EOF

cat > "${DELTA_DIR}/delta.conf" <<EOF
rpcuser=user
rpcpassword=pass
regtest=1
rpcport=${DELTA_RPC_PORT}
i-am-aware-zcashd-will-be-replaced-by-zebrad-and-zallet-in-2025=1
exportdir=${SOURCE_DIR}
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
nuparams=c8e71055:5
nuparams=4dec4df0:7
EOF

###############################################################################
# Start the alpha node.
###############################################################################

tmux rename-window -t $SESSION:0 'alpha'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${ALPHA_DIR}" C-m

start_alpha() {
  echo "Starting simnet alpha node"
  tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
    -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} -conf=alpha.conf \
    -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
    -whitelist=127.0.0.0/8 -whitelist=::1  \
    -wallet=alphawallet \
    -dbcache=5000 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001 \
    -allowdeprecated=getnewaddress  -printtoconsole"  C-m
  sleep 3
  tmux wait-for -S alpha${SYMBOL}
}
start_alpha

###############################################################################
# Setup the beta node.
###############################################################################

tmux new-window -t $SESSION:1 -n 'beta' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${BETA_DIR}" C-m

start_beta () {
  echo "Starting simnet beta node"
  tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
    -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -conf=beta.conf -dbcache=5000 -regtest=1 \
    -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
    -whitelist=127.0.0.0/8 -whitelist=::1  \
    -wallet=betawallet \
    -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001 -allowdeprecated=getnewaddress  -printtoconsole" C-m
  sleep 3
  tmux wait-for -S beta${SYMBOL}
}
start_beta

###############################################################################
# Setup the delta node.
###############################################################################

tmux new-window -t $SESSION:2 -n 'delta' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${DELTA_DIR}" C-m

start_delta () {
  echo "Starting simnet delta node"
  tmux send-keys -t $SESSION:2 "${DAEMON} -rpcuser=user -rpcpassword=pass \
    -rpcport=${DELTA_RPC_PORT} -datadir=${DELTA_DIR} -conf=delta.conf -rpcthreads=16 -rpctimeout=300 -dbcache=4000 -regtest=1 \
    -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
    -whitelist=127.0.0.0/8 -whitelist=::1  \
    -wallet=deltawallet \
    -port=${DELTA_LISTEN_PORT} -fallbackfee=0.00001 -allowdeprecated=getnewaddress  -printtoconsole" C-m
  sleep 3
tmux wait-for -S delta${SYMBOL}
}
start_delta

###############################################################################
# Setup the harness-ctl directory
###############################################################################

tmux new-window -t $SESSION:3 -n 'harness-ctl' $SHELL
tmux send-keys -t $SESSION:3 "set +o history" C-m
tmux send-keys -t $SESSION:3 "cd ${HARNESS_DIR}" C-m

cd ${HARNESS_DIR}


# -----------------------------------------------------------------------------
# begin loadbot specific ->

# start-wallet, connect-alpha, and stop-wallet are used by loadbot to set up and
# run new wallets. As yet unchanged!
cat > "./start-wallet" <<EOF
#!/usr/bin/env bash

mkdir ${NODES_ROOT}/\$1

printf "rpcuser=user\nrpcpassword=pass\nregtest=1\nrpcport=\$2\nexportdir=${SOURCE_DIR}\nnuparams=5ba81b19:1\nnuparams=76b809bb:1\nnuparams=2bb40e60:1\nnuparams=f5b9230b:1\nnuparams=e9ff75a6:2\nnuparams=c2d6d0b4:3\n" > ${NODES_ROOT}/\$1/\$1.conf

${DAEMON} -rpcuser=user -rpcpassword=pass \
-rpcport=\$2 -datadir=${NODES_ROOT}/\$1 -regtest=1 -conf=\$1.conf \
-debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
-whitelist=127.0.0.0/8 -whitelist=::1  \
-wallet=alphawallet \
-port=\$3 -fallbackfee=0.00001  -allowdeprecated=getnewaddress  -printtoconsole
EOF
chmod +x "./start-wallet"

cat > "./connect-alpha" <<EOF
#!/usr/bin/env bash
${CLI} -conf=${NODES_ROOT}/\$2/\$2.conf -rpcport=\$1 -regtest=1 -rpcuser=user -rpcpassword=pass addnode 127.0.0.1:${ALPHA_LISTEN_PORT} onetry
EOF
chmod +x "./connect-alpha"

cat > "./stop-wallet" <<EOF
#!/usr/bin/env bash
${CLI} -conf=${NODES_ROOT}/\$2/\$2.conf -rpcport=\$1 -regtest=1 -rpcuser=user -rpcpassword=pass stop
EOF
chmod +x "./stop-wallet"
# <- end loadbot specific
# -----------------------------------------------------------------------------


cat > "./alpha" <<EOF
#!/usr/bin/env bash
${CLI} ${ALPHA_CLI_CFG} "\$@"
EOF
chmod +x "./alpha"

cat > "./beta" <<EOF
#!/usr/bin/env bash
${CLI} ${BETA_CLI_CFG} "\$@"
EOF
chmod +x "./beta"

cat > "./delta" <<EOF
#!/usr/bin/env bash
${CLI} ${DELTA_CLI_CFG} "\$@"
EOF
chmod +x "./delta"

# mine-alpha should mine delta not to break existing code
cat > "./mine-alpha" <<EOF
#!/usr/bin/env bash
${CLI} ${DELTA_CLI_CFG} generate \$1
# ${CLI} ${ALPHA_CLI_CFG} generate \$1
EOF
chmod +x "./mine-alpha"

# mine-beta should mine delta not to break existing code
cat > "./mine-beta" <<EOF 
#!/usr/bin/env bash
${CLI} ${DELTA_CLI_CFG} generate \$1
# ${CLI} ${BETA_CLI_CFG} generate \$1
EOF
chmod +x "./mine-beta"

# delta is the miner for this network and the only one with coinbases
cat > "./mine-delta" <<EOF
#!/usr/bin/env bash
${CLI} ${DELTA_CLI_CFG} generate \$1
EOF
chmod +x "./mine-delta"

cat > "./reorg" <<EOF
#!/usr/bin/env bash
set -x
echo "Disconnecting beta from delta"
sleep 1
./beta disconnectnode 127.0.0.1:${DELTA_LISTEN_PORT}
echo "Mining a block on delta"
sleep 1
./mine-delta 1
echo "Mining 3 blocks on beta"
./mine-beta 3
sleep 2
echo "Reconnecting beta to delta"
./beta addnode 127.0.0.1:${DELTA_LISTEN_PORT} onetry
echo -e "\nRestart harness to clean these new non-account beta coinbases mined above."
echo -e "Or shield them - Zcash requires all coinbases to be shielded before spending.\n"
sleep 2
EOF
chmod +x "./reorg"

cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:0 C-c
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux send-keys -t $SESSION:3 C-c
tmux send-keys -t $SESSION:5 C-c
tmux wait-for alpha${SYMBOL}
tmux wait-for beta${SYMBOL}
tmux wait-for delta${SYMBOL}
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

###############################################################################
# Make a network of 3 daemons
###############################################################################

add_nodes () {
  tmux send-keys -t $SESSION:3 "./alpha addnode 127.0.0.1:${DELTA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:3 "./beta  addnode 127.0.0.1:${DELTA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
  sleep 3 # per old script
}
add_nodes

#################################################################################
# Import clean, empty NU6.1 pre-exported wallets. Alpha has an imported t-privkey
#################################################################################

# import
tmux send-keys -t $SESSION:3 "./alpha z_importwallet ${SOURCE_DIR}/alphawallet ${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:3 "./beta  z_importwallet ${SOURCE_DIR}/betawallet ${DONE}"  C-m\; ${WAIT}
tmux send-keys -t $SESSION:3 "./delta z_importwallet ${SOURCE_DIR}/deltawallet ${DONE}" C-m\; ${WAIT}
# accounts
tmux send-keys -t $SESSION:3 "./alpha z_getnewaccount ${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:3 "./beta  z_getnewaccount ${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:3 "./delta z_getnewaccount ${DONE}" C-m\; ${WAIT}
sleep 1

###############################################################################
# Mine coins -> delta. Includes "mining" genesis. Wait for nodes/wallets ready
###############################################################################

# NOTE: THIS TAKES ~120..150s - original script had many long sleeps
# - New nodes on network are building commitment trees & maybe rescan.
# - Thus delta finishes fast but alpha & beta take a while (in parallel)
tmux send-keys -t $SESSION:3 "./delta generate 150${DONE}" C-m\; ${WAIT}

wait_for_wallet() {
  local NODE=$1
  echo "Waiting for ${NODE} to clear Code -28..."
  while ./${NODE} z_getbalanceforaccount 0 2>&1 | grep -q "disabled while reindexing"; do
    sleep 3
  done
  echo -e "\n${NODE} is now READY!\n"
}
wait_for_wallet delta
wait_for_wallet beta
wait_for_wallet alpha

###############################################################################
# Get account address - helper
###############################################################################

# get an address from the node's account pool. Not internal pool.
getAccountAddr () {
  cd "${HARNESS_DIR}"
  local NODE=$1
  local ACCT=$2
  # get the Unified Address
  # Silence into stderr
  local ADDR_JSON=$(./${NODE} z_getaddressforaccount ${ACCT} '["orchard", "p2pkh"]' 2>/dev/null)
    if [ -z "$ADDR_JSON" ]; then
    echo "ERROR: z_getaddressforaccount failed for $NODE account $ACCT" >&2
    return 1
  fi
  local UA=$(echo "${ADDR_JSON}" | jq -r '.address')
  # extract the specific Transparent Receiver (p2pkh) from the UA
  local TADDR=$(./${NODE} z_listunifiedreceivers "${UA}" | jq -r '.p2pkh')
  # just the t-addr
  echo "${TADDR}"
}

# used below for distribution
IMPORTED_ADDR=tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD
#
ALPHA_ADDR_ACC0=$(getAccountAddr alpha 0)
BETA_ADDR_ACC0=$(getAccountAddr beta 0)
DELTA_ADDR_ACC0=$(getAccountAddr delta 0)

###############################################################################
# Wait for async z_op - helper
###############################################################################

# some async zcash commands return an op-id that can be waited on and returns 
# success/error/executing status for each call.
wait_for_z_op() {
  local NODE=$1
  local OPID=$2
  echo "Waiting for operation $OPID on $NODE..."
  while true; do
    # handle both array and single-object responses
    local STATUS=$(./${NODE} z_getoperationstatus "[\"$OPID\"]" | jq -r 'if type=="array" then .[0].status else .status end')
    #   
    if [ "$STATUS" == "success" ]; then break; fi
    if [ "$STATUS" == "failed" ]; then 
       echo "ERROR: Op $OPID failed"
       ./${NODE} z_getoperationstatus "[\"$OPID\"]"
       exit 1
    fi
    sleep 1
  done
}

###############################################################################
# Shield all mined delta coinbases so that they can be spent.
###############################################################################

DELTA_UA=$(./delta z_getaddressforaccount 0 | jq -r '.address')
# z_shieldcoinbase "from" "to" (fee) (limit) (memo) (privacyPolicy)
# use 0 for limit to shield all available UTXOs, default is 50
# use ZIP 317 to calculate fee for zcash internal actions
# use "" for memo, faster
# allow privacy leak
# https://zcash.github.io/rpc/z_shieldcoinbase.html
SHIELD_OP=$(./delta z_shieldcoinbase "*" "$DELTA_UA" 0.05 0 "" "AllowLinkingAccountAddresses" | jq -r '.opid')
# wait z_op
wait_for_z_op delta "$SHIELD_OP"
# confirm results in harness-ctl window -  informational .. can be removed
tmux send-keys -t $SESSION:3 "./delta z_getoperationresult" C-m
# mine delta to confirm
./delta generate 1
# wait for block processing
sleep 2 
# show account balance
./delta z_getbalanceforaccount 0

###############################################################################
# De-shield all delta shielded coins -> transparent in account 0
###############################################################################

DELTA_TADDR=$(getAccountAddr delta 0)
SHIELDED_ZATS=$(./delta z_getbalanceforaccount 0 | jq -r '.pools.orchard.valueZat')
SHIELDED_ZEC=$(echo "scale=8; $SHIELDED_ZATS / 100000000" | bc -l)
# ZIP 317 Fee: 0.00005 ZEC per internal action. 
# 3 actions (Input + Output + Change), 0.00015 ZEC
# https://zcash.github.io/rpc/z_sendmany.html
ZIP317_FEE=0.00015000
SEND_AMT=$(echo "$SHIELDED_ZEC - $ZIP317_FEE" | bc -l)
echo "Shielded Balance: $SHIELDED_ZEC ZEC. Sending to Transparent: $SEND_AMT with Fee: $ZIP317_FEE"
# execute with explicit fee and even lighter privacy policy than above
DESHIELD_OP=$(./delta z_sendmany \
  "$DELTA_UA" \
  "[{\"address\": \"$DELTA_TADDR\", \"amount\": $SEND_AMT}]" \
  1 \
  $ZIP317_FEE \
  "AllowRevealedRecipients")
# wait z_op
wait_for_z_op delta "$DESHIELD_OP"
# confirm results in harness-ctl window - informational .. can be removed
tmux send-keys -t $SESSION:3 "./delta z_getoperationresult" C-m
# mine delta to confirm
./delta generate 1
# wait block processing
sleep 2
# show account balance
./delta z_getbalanceforaccount 0

###############################################################################
# Send alpha & beta some cleaned, spendable coins.
###############################################################################

for i in 1 2 3 4 5 10 20 30 20 10 5 4 3 2 1
do
  # ./delta sendtoaddress "${IMPORTED_ADDR}"   "5.0" "" "" true # sweep # tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD - non-account balance and golang makes test fail
  ./delta sendtoaddress "${ALPHA_ADDR_ACC0}" "5.0" "" "" true # sweep
  ./delta sendtoaddress "${BETA_ADDR_ACC0}"  "5.0" "" "" true # sweep
  sleep 1
done
./delta generate 1

################################################################################
# Set up mining - MINING env var enables - default Off. Window always available.
################################################################################

tmux new-window -t $SESSION:5 -n 'miner' $SHELL
tmux send-keys -t $SESSION:5 "cd ${HARNESS_DIR}" C-m
if [ "${MINING}" == "" ]; then
  echo "not mining"
else
  echo "mining ..."
  tmux send-keys -t $SESSION:5 "watch -n 15 ./mine-delta 1" C-m
fi

################################################################################
# Re-enable history. Attach to the control session
################################################################################

tmux select-window -t $SESSION:3
tmux send-keys -t $SESSION:3 "set -o history" C-m
tmux attach-session -t $SESSION
