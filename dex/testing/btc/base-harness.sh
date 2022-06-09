#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
NODES_ROOT=~/dextest/${SYMBOL}
rm -rf "${NODES_ROOT}"

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

echo "Writing node config files"
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

export SHELL=$(which bash)

################################################################################
# Load prepared wallet if the files exist.
################################################################################

if [ -f ./harnesschain.tar.gz ]; then
  CHAIN_LOADED=1
  echo "Seeding alpha chain from compressed file"
  tar -xzf ./harnesschain.tar.gz -C ${NODES_ROOT}
else
  mkdir -p "${ALPHA_DIR}"
  mkdir -p "${BETA_DIR}"
fi

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
  ${EXTRA_ARGS}; tmux wait-for -S alpha${SYMBOL}" C-m
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
  -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001 ${EXTRA_ARGS}; \
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

cat > "./gamma" <<EOF
#!/usr/bin/env bash
${CLI} ${GAMMA_CLI_CFG} "\$@"
EOF
chmod +x "./gamma"

cat > "./delta" <<EOF
#!/usr/bin/env bash
${CLI} -rpcwallet=delta -rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass "\$@"
EOF
chmod +x "./delta"

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
${NEW_WALLET_CMD}
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
# Have to generate a block before calling sethdseed
################################################################################
if [ "$CHAIN_LOADED" ] ; then
  tmux send-keys -t $SESSION:2 "./alpha loadwallet gamma${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta loadwallet delta${DONE}" C-m\; ${WAIT}
  # Get the median block time up to date.
  tmux send-keys -t $SESSION:2 "./mine-alpha 12${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}

else
  tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
  # This timeout is apparently critical. Give the nodes time to sync.
  sleep 2

  echo "Generating the genesis block"
  tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 1 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}
  sleep 2

  if [ "$CREATE_DEFAULT_WALLET" ] ; then
    tmux send-keys -t $SESSION:2 "./alpha createwallet \"\" false true ${WALLET_PASSWORD} false false true${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./beta createwallet \"\" false true ${WALLET_PASSWORD} false false true${DONE}" C-m\; ${WAIT}
  else
    echo "Encrypting the alpha wallet (may take a minute)"
    tmux send-keys -t $SESSION:2 "./alpha encryptwallet ${WALLET_PASSWORD}${DONE}" C-m\; ${WAIT}

    echo "Encrypting the beta wallet"
    tmux send-keys -t $SESSION:2 "./beta encryptwallet ${WALLET_PASSWORD}${DONE}" C-m\; ${WAIT}
  fi


  #################################################################################
  # Optional restart for some assets
  ################################################################################

  if [ "$RESTART_AFTER_ENCRYPT" ] ; then
    echo "Restarting alpha/beta nodes"
    tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
      -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} \
      -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001 ${EXTRA_ARGS}; \
      tmux wait-for -S alpha${SYMBOL}" C-m

    tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
      -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -txindex=1 -regtest=1 \
      -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001 ${EXTRA_ARGS}; \
      tmux wait-for -S beta${SYMBOL}" C-m
    sleep 3
  fi

  tmux send-keys -t $SESSION:2 "./alpha walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}
  echo "Setting private key for alpha"
  tmux send-keys -t $SESSION:2 "./alpha sethdseed true ${ALPHA_WALLET_SEED}${DONE}" C-m\; ${WAIT}

  tmux send-keys -t $SESSION:2 "./beta walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}
  echo "Setting private key for beta"
  tmux send-keys -t $SESSION:2 "./beta sethdseed true ${BETA_WALLET_SEED}${DONE}" C-m\; ${WAIT}

  ################################################################################
  # Setup the gamma wallet
  ################################################################################
  echo "Creating the gamma wallet"
  # TODO: with Bitcoin Core v23 createwallet makes a descriptor wallet by default,
  # which we cannot use yet, so all the createwallet options will be needed.
  tmux send-keys -t $SESSION:2 "./alpha createwallet gamma${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./gamma sethdseed true ${GAMMA_WALLET_SEED}${DONE}" C-m\; ${WAIT}

  ################################################################################
  # Create the delta wallet
  ################################################################################
  echo "Creating the delta wallet"
  tmux send-keys -t $SESSION:2 "./beta createwallet delta${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./delta sethdseed true ${DELTA_WALLET_SEED}${DONE}" C-m\; ${WAIT}

  #################################################################################
  # Generate addresses
  ################################################################################

  tmux send-keys -t $SESSION:2 "./alpha getnewaddress${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./alpha getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}

  tmux send-keys -t $SESSION:2 "./beta getnewaddress${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}

  tmux send-keys -t $SESSION:2 "./gamma getnewaddress${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./gamma getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}

  tmux send-keys -t $SESSION:2 "./delta getnewaddress${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./delta getnewaddress \"\" \"legacy\"${DONE}" C-m\; ${WAIT}

  if [ "$NEEDS_MWEB_PEGIN_ACTIVATION" ] ; then
    ./alpha generatetoaddress 16 ${BETA_MINING_ADDR}
    HEIGHT=$(./alpha getblockcount)
    ./alpha generatetoaddress $((432 - $HEIGHT - 1)) ${ALPHA_MINING_ADDR}
    MWADDR=$(./alpha getnewaddress mweb-peg-in mweb)
    ./alpha sendtoaddress ${MWADDR} 1.234 && sleep 1
    ./alpha generatetoaddress 3 ${ALPHA_MINING_ADDR}
  else
    echo "Generating 400 blocks for alpha"
    tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 400 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}
  fi

  #################################################################################
  # Send gamma and delta some coin
  ################################################################################

  # Send the beta wallet some dough too.
  tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${BETA_MINING_ADDR} 1000${DONE}" C-m\; ${WAIT}

  #  Send some bitcoin to gamma and delta wallets, mining some blocks too.
  echo "Sending 84 BTC each to gamma and delta in 8 blocks"
  for i in 10 18 5 7 1 15 3 25
  do
    tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${GAMMA_ADDRESS} ${i}${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./alpha sendtoaddress ${DELTA_ADDRESS} ${i}${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./mine-alpha 1${DONE}" C-m\; ${WAIT}
  done

# End of new wallet setup
fi

# Have alpha share a little more wealth, esp. for trade_simnet_test.go
tmux send-keys -t $SESSION:2 "./alpha walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}
RECIPIENTS="{\"${BETA_MINING_ADDR}\":12,\"${GAMMA_ADDRESS}\":12,\"${DELTA_ADDRESS}\":12}"
for i in {1..30}; do
  tmux send-keys -t $SESSION:2 "./alpha sendmany \"\" '${RECIPIENTS}'${DONE}" C-m\; ${WAIT}
done

tmux send-keys -t $SESSION:2 "./mine-alpha 2${DONE}" C-m\; ${WAIT}


# Reenable history and attach to the control session.
tmux send-keys -t $SESSION:2 "set -o history" C-m
tmux attach-session -t $SESSION
