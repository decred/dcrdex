#!/bin/sh
# Tmux script that sets up a simnet harness.
set -ex
NODES_ROOT=~/dextest/${SYMBOL}
rm -rf "${NODES_ROOT}"

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

echo "Writing node config files"
mkdir -p "${ALPHA_DIR}"
mkdir -p "${BETA_DIR}"
mkdir -p "${HARNESS_DIR}"

WALLET_PASSWORD="abc"

ALPHA_CLI_CFG="-rpcwallet= -rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

BETA_CLI_CFG="-rpcwallet= -rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

GAMMA_CLI_CFG="-rpcwallet=gamma -rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

# DONE can be used in a send-keys call along with a `wait-for btc` command to
# wait for process termination.
DONE="; tmux wait-for -S ${SYMBOL}"
WAIT="wait-for ${SYMBOL}"

SESSION="${SYMBOL}-harness"

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Write config files.
################################################################################

# These config files aren't actually used here, but can be used by other
# programs. I would use them here, but bitcoind seems to have some issues
# reading from the file when using regtest.

cat > "${HARNESS_DIR}/alpha.conf" <<EOF
rpcuser=user
rpcpassword=pass
datadir=${ALPHA_DIR}
txindex=1
port=${ALPHA_LISTEN_PORT}
regtest=1
rpcport=${ALPHA_RPC_PORT}
EOF

cat > "${HARNESS_DIR}/beta.conf" <<EOF
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
tmux send-keys -t $SESSION:0 "cd ${ALPHA_DIR}" C-m
echo "Starting simnet alpha node"
tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001; tmux wait-for -S alpha${SYMBOL}" C-m
sleep 3

################################################################################
# Setup the beta node.
################################################################################

tmux new-window -t $SESSION:1 -n 'beta'
tmux send-keys -t $SESSION:1 "cd ${BETA_DIR}" C-m

echo "Starting simnet beta node"
tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -txindex=1 -regtest=1 \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001; tmux wait-for -S beta${SYMBOL}" C-m
sleep 3

################################################################################
# Setup the harness-ctl directory
################################################################################

tmux new-window -t $SESSION:2 -n 'harness-ctl'
tmux send-keys -t $SESSION:2 "cd ${HARNESS_DIR}" C-m
sleep 1

cd ${HARNESS_DIR}

cat > "./alpha" <<EOF
#!/bin/sh
${CLI} ${ALPHA_CLI_CFG} "\$@"
EOF
chmod +x "./alpha"

cat > "./mine-alpha" <<EOF
#!/bin/sh
${CLI} ${ALPHA_CLI_CFG} generatetoaddress \$1 ${ALPHA_MINING_ADDR}
EOF
chmod +x "./mine-alpha"

cat > "./gamma" <<EOF
#!/bin/sh
${CLI} ${GAMMA_CLI_CFG} "\$@"
EOF
chmod +x "./gamma"

cat > "./delta" <<EOF
#!/bin/sh
${CLI} -rpcwallet=delta -rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass "\$@"
EOF
chmod +x "./delta"

cat > "./beta" <<EOF
#!/bin/sh
${CLI} ${BETA_CLI_CFG} "\$@"
EOF
chmod +x "./beta"

cat > "./mine-beta" <<EOF
#!/bin/sh
${CLI} ${BETA_CLI_CFG} generatetoaddress \$1 ${BETA_MINING_ADDR}
EOF
chmod +x "./mine-beta"

cat > "./reorg" <<EOF
#!/bin/sh
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

cat > "${HARNESS_DIR}/quit" <<EOF
#!/bin/sh
tmux send-keys -t $SESSION:0 C-c
tmux send-keys -t $SESSION:1 C-c
tmux wait-for alpha${SYMBOL}
tmux wait-for beta${SYMBOL}
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
# This timeout is apparently critical. Give the nodes time to sync.
sleep 1

echo "Generating the genesis block"
tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 1 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}
sleep 1

#################################################################################
# Setup the alpha wallet
################################################################################

echo "Setting private key for alpha"
tmux send-keys -t $SESSION:2 "./alpha sethdseed true ${ALPHA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./alpha getnewaddress${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./alpha getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}
echo "Generating 30 blocks for alpha"
tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 30 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}

#################################################################################
# Setup the beta wallet
################################################################################

echo "Setting private key for beta"
tmux send-keys -t $SESSION:2 "./beta sethdseed true ${BETA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./beta getnewaddress${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./beta getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}
echo "Generating 110 blocks for beta"
tmux send-keys -t $SESSION:2 "./beta generatetoaddress 110 ${BETA_MINING_ADDR}${DONE}" C-m\; ${WAIT}

################################################################################
# Setup the gamma wallet
################################################################################

# Create and encrypt the gamma wallet
echo "Creating the gamma wallet"
tmux send-keys -t $SESSION:2 "./alpha createwallet gamma${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./gamma sethdseed true ${GAMMA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./gamma getnewaddress${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./gamma getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}
echo "Encrypting the gamma wallet (may take a minute)"
tmux send-keys -t $SESSION:2 "./gamma encryptwallet ${WALLET_PASSWORD}${DONE}" C-m\; ${WAIT}
if [ "$RESTART_AFTER_ENCRYPT" ] ; then
    echo "Restarting alpha/gamma wallets."
    tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
      -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} \
      -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001; tmux wait-for -S alphaltc" C-m
    sleep 3
    tmux send-keys -t $SESSION:2 "./alpha loadwallet gamma${DONE}" C-m\; ${WAIT}
fi

# Create and encrypt the delta wallet
echo "Creating the delta wallet"
tmux send-keys -t $SESSION:2 "./beta createwallet delta${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./delta sethdseed true ${DELTA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./delta getnewaddress${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:2 "./delta getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}
echo "Encrypting the delta wallet (may take a minute)"
tmux send-keys -t $SESSION:2 "./delta encryptwallet ${WALLET_PASSWORD}${DONE}" C-m\; ${WAIT}
if [ "$RESTART_AFTER_ENCRYPT" = 1 ] ; then
    echo "Restarting beta/delta wallets."
    tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
      -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -txindex=1 -regtest=1 \
      -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001; tmux wait-for -S beta${SYMBOL}" C-m
    sleep 3
    tmux send-keys -t $SESSION:2 "./beta loadwallet delta${DONE}" C-m\; ${WAIT}
fi

#  Send some bitcoin to gamma and delta wallets, mining some blocks too.
echo "Sending 84 BTC to gamma, delta in 8 blocks"
for i in 10 18 5 7 1 15 3 25
do
  tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${GAMMA_ADDRESS} ${i}${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${DELTA_ADDRESS} ${i}${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./mine-alpha 1${DONE}" C-m\; ${WAIT}
done

tmux attach-session -t $SESSION
