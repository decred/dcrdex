#!/usr/bin/env bash
SYMBOL="doge"
DAEMON="dogecoind"
CLI="dogecoin-cli"
RPC_USER="user"
RPC_PASS="pass"
ALPHA_LISTEN_PORT="23764"
BETA_LISTEN_PORT="23765"
ALPHA_RPC_PORT="23766"
BETA_RPC_PORT="23767"
ALPHA_MINING_ADDR="mjzB71SzfAi8BwNvobPZ983d8vCdxuQD2i"
BETA_MINING_ADDR="mwL7ypWCMEhBhGcsqWNwkvAasuXP3duXQk"

set -ex
NODES_ROOT=~/dextest/${SYMBOL}
rm -rf "${NODES_ROOT}"
SOURCE_DIR=$(pwd)

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

echo "Writing node config files"
mkdir -p "${HARNESS_DIR}"

WALLET_PASSWORD="abc"

ALPHA_CLI_CFG="-rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

BETA_CLI_CFG="-rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

# DONE can be used in a send-keys call along with a `wait-for btc` command to
# wait for process termination.
DONE="; tmux wait-for -S ${SYMBOL}"
WAIT="wait-for ${SYMBOL}"

SESSION="${SYMBOL}-harness"

SHELL=$(which bash)

################################################################################
# Load prepared wallet if the files exist.
################################################################################

mkdir -p ${ALPHA_DIR}/regtest
cp ${SOURCE_DIR}/alpha_wallet.dat ${ALPHA_DIR}/regtest/wallet.dat
mkdir -p ${BETA_DIR}/regtest
cp ${SOURCE_DIR}/beta_wallet.dat ${BETA_DIR}/regtest/wallet.dat

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION $SHELL

################################################################################
# Write config files.
################################################################################

# These config files aren't actually used here, but can be used by other
# programs. I would use them here, but bitcoind seems to have some issues
# reading from the file when using regtest.

cat > "${ALPHA_DIR}/alpha.conf" <<EOF
rpcuser=user
rpcpassword=pass
datadir=${ALPHA_DIR}
txindex=1
port=${ALPHA_LISTEN_PORT}
regtest=1
rpcport=${ALPHA_RPC_PORT}
EOF

cat > "${BETA_DIR}/beta.conf" <<EOF
rpcuser=user
rpcpassword=pass
datadir=${BETA_DIR}
txindex=1
regtest=1
rpcport=${BETA_RPC_PORT}
EOF

################################################################################
# Start the alpha node.
################################################################################

tmux rename-window -t $SESSION:0 'alpha'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${ALPHA_DIR}" C-m
echo "Starting simnet alpha node"
tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001 \
  -printtoconsole; tmux wait-for -S alpha${SYMBOL}" C-m
sleep 3

################################################################################
# Setup the beta node.
################################################################################

tmux new-window -t $SESSION:1 -n 'beta' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${BETA_DIR}" C-m

echo "Starting simnet beta node"
tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -txindex=1 -regtest=1 \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001 -printtoconsole; \
  tmux wait-for -S beta${SYMBOL}" C-m
sleep 3

################################################################################
# Setup the harness-ctl directory
################################################################################

tmux new-window -t $SESSION:2 -n 'harness-ctl' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${HARNESS_DIR}" C-m
sleep 1

cd ${HARNESS_DIR}

cat > "./alpha" <<EOF
#!/usr/bin/env bash
${CLI} ${ALPHA_CLI_CFG} "\$@"
EOF
chmod +x "./alpha"

cat > "./mine-alpha" <<EOF
#!/usr/bin/env bash
${CLI} ${ALPHA_CLI_CFG} generatetoaddress \$1 ${ALPHA_MINING_ADDR}
EOF
chmod +x "./mine-alpha"

cat > "./beta" <<EOF
#!/usr/bin/env bash
${CLI} ${BETA_CLI_CFG} "\$@"
EOF
chmod +x "./beta"

cat > "./mine-beta" <<EOF
#!/usr/bin/env bash
${CLI} ${BETA_CLI_CFG} generatetoaddress \$1 ${BETA_MINING_ADDR}
EOF
chmod +x "./mine-beta"

cat > "./reorg" <<EOF
#!/usr/bin/env bash
set -x
echo "Disconnecting beta from alpha"
sleep 1
./beta disconnectnode 127.0.0.1:${ALPHA_LISTEN_PORT}
echo "Mining a block on alpha"
sleep 1
./mine-alpha 1
echo "Mining 3 blocks on beta"
./mine-beta 3
sleep 2
echo "Reconnecting beta to alpha"
./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} onetry
sleep 2
EOF
chmod +x "./reorg"

cat > "./new-wallet" <<EOF
#!/usr/bin/env bash
./\$1 createwallet \$2
EOF
chmod +x "./new-wallet"

cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:0 C-c
tmux send-keys -t $SESSION:1 C-c
tmux wait-for alpha${SYMBOL}
tmux wait-for beta${SYMBOL}
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

################################################################################
# Generate the first block
################################################################################
tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
# This timeout is apparently critical. Give the nodes time to sync.
sleep 1

tmux send-keys -t $SESSION:2 "./alpha walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./beta walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}

echo "Generating 400 blocks for alpha"
tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 400 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}

#################################################################################
# Send beta some coin
################################################################################

# Send the beta wallet some dough.
echo "Sending 8,400,000 BTC to beta in 8 blocks"
for i in 1000000 1800000 500000 700000 100000 1500000 300000 2500000
do
    tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${BETA_MINING_ADDR} ${i}${DONE}" C-m\; ${WAIT}
done

tmux send-keys -t $SESSION:2 "./mine-alpha 2${DONE}" C-m\; ${WAIT}

# Reenable history and attach to the control session.
tmux send-keys -t $SESSION:2 "set -o history" C-m
tmux attach-session -t $SESSION
