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

if [ -n "$HARNESS_VER" ]; then
  HARNESS_VER_FILE="${NODES_ROOT}/v${HARNESS_VER}"
fi

WALLET_PASSWORD="abc"

[ -z "${ALPHA_WALLET_SEED}" ] && ALPHA_DESCRIPTOR_WALLET="true" || ALPHA_DESCRIPTOR_WALLET="false"
[ -z "${BETA_WALLET_SEED}"  ] && BETA_DESCRIPTOR_WALLET="true" || BETA_DESCRIPTOR_WALLET="false"

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
  if [[ -n "$HARNESS_VER_FILE"  && ! -f "$HARNESS_VER_FILE" ]]; then
    echo "Harness archive outdated! Deleting it."
    rm -rf harnesschain.tar.gz "${NODES_ROOT}"
    mkdir -p "${HARNESS_DIR}"
    CHAIN_LOADED=
  fi
fi

if [ ! "$CHAIN_LOADED" ]; then
  mkdir -p "${ALPHA_DIR}"
  mkdir -p "${BETA_DIR}"
  if [ -n "$HARNESS_VER_FILE" ]; then
    touch "${HARNESS_VER_FILE}"
  fi
  FULLCHAINFILE=$(cd $(dirname "${BASH_SOURCE[0]:-$0}") && pwd)/harnesschain.tar.gz
fi

# If there are any descriptor files, copy them to the nodes root folder.
cp desc-*.json ${NODES_ROOT} 2> /dev/null || true

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
tmux send-keys -t $SESSION:3 C-c
tmux send-keys -t $SESSION:4 C-c
tmux wait-for alpha${SYMBOL}
tmux wait-for beta${SYMBOL}
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

if [ "$CHAIN_LOADED" ] ; then
  tmux send-keys -t $SESSION:2 "./alpha loadwallet gamma${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta loadwallet delta${DONE}" C-m\; ${WAIT}
  # Get the median block time up to date.
  tmux send-keys -t $SESSION:2 "./mine-alpha 12${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}

else
  sleep 1
  tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
  # This timeout is apparently critical. Give the nodes time to sync.
  sleep 2

  ################################################################################
  # Have to generate a block before calling sethdseed
  ################################################################################
  echo "Generating the genesis block"
  tmux send-keys -t $SESSION:2 "./alpha generatetoaddress 1 ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}
  sleep 2

  if [ "$CREATE_DEFAULT_WALLET" ] ; then # bitcoin core v0.21+ does not create default wallet automatically
    # Note that assets requiring CREATE_DEFAULT_WALLET also support named args.
    # Create these as "blank" wallets since sethdseed or importdescriptors
    # will follow.
    tmux send-keys -t $SESSION:2 "./alpha -named createwallet wallet_name= blank=true passphrase=\"${WALLET_PASSWORD}\" descriptors=${ALPHA_DESCRIPTOR_WALLET}${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./beta -named createwallet wallet_name= blank=true passphrase=\"${WALLET_PASSWORD}\" descriptors=${BETA_DESCRIPTOR_WALLET}${DONE}" C-m\; ${WAIT}
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
    sleep 2
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

    tmux send-keys -t $SESSION:2 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
  fi

  tmux send-keys -t $SESSION:2 "./alpha walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}
  tmux send-keys -t $SESSION:2 "./beta walletpassphrase ${WALLET_PASSWORD} 100000000${DONE}" C-m\; ${WAIT}

  echo "Setting private keys for alpha"
  if [ -z "$ALPHA_WALLET_SEED" ]; then
    tmux send-keys -t $SESSION:2 "./alpha importdescriptors \$(< ../desc-alpha.json)${DONE}" C-m\; ${WAIT}
  else
    tmux send-keys -t $SESSION:2 "./alpha sethdseed true ${ALPHA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
  fi

  echo "Setting private keys for beta"
  if [ -z "$BETA_WALLET_SEED" ]; then
    tmux send-keys -t $SESSION:2 "./beta importdescriptors \$(< ../desc-beta.json)${DONE}" C-m\; ${WAIT}
  else
    tmux send-keys -t $SESSION:2 "./beta sethdseed true ${BETA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
  fi

  ################################################################################
  # Setup the gamma wallet
  ################################################################################
  echo "Creating the gamma wallet"
  if [ -z "$GAMMA_WALLET_SEED"  ]; then
    tmux send-keys -t $SESSION:2 "./alpha -named createwallet wallet_name=gamma blank=true descriptors=true${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./gamma importdescriptors \$(< ../desc-gamma.json)${DONE}" C-m\; ${WAIT}
  else
    # Use new-wallet since BCH does not support named parameters (or
    # descriptors), while the descriptor enabled assets all support named.
    tmux send-keys -t $SESSION:2 "./new-wallet alpha gamma${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./gamma sethdseed true ${GAMMA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
  fi

  ################################################################################
  # Create the delta wallet
  ################################################################################
  echo "Creating the delta wallet"
  if [ -z "$DELTA_WALLET_SEED" ]; then
    tmux send-keys -t $SESSION:2 "./beta -named createwallet wallet_name=delta blank=true descriptors=true${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./delta importdescriptors \$(< ../desc-delta.json)${DONE}" C-m\; ${WAIT}
  else
    tmux send-keys -t $SESSION:2 "./new-wallet beta delta${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:2 "./delta sethdseed true ${DELTA_WALLET_SEED}${DONE}" C-m\; ${WAIT}
  fi

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
    BLOCKS_TO_MINE=${BLOCKS_TO_MINE=400}
    echo "Generating ${BLOCKS_TO_MINE} blocks for alpha"
    tmux send-keys -t $SESSION:2 "./alpha generatetoaddress ${BLOCKS_TO_MINE} ${ALPHA_MINING_ADDR}${DONE}" C-m\; ${WAIT}
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

set +x
if [ ! "$CHAIN_LOADED" ]; then
  echo "**********************************************************************"
  echo "You may wish to recreate your harnesschain.tar.gz file AFTER stopping the nodes:"
  echo "  cd ${NODES_ROOT}"
  echo "  tar czf ${FULLCHAINFILE} alpha beta v${HARNESS_VER}"
  echo "**********************************************************************"
fi

# Reenable history
tmux send-keys -t $SESSION:2 "set -o history" C-m

# Miner
if [ -z "$NOMINER" ] ; then
  tmux new-window -t $SESSION:3 -n "miner" $SHELL
  tmux send-keys -t $SESSION:3 "cd ${HARNESS_DIR}" C-m
  tmux send-keys -t $SESSION:3 "watch -n 15 ./mine-alpha 1" C-m
fi

if [ ! -z "$GODAEMON" ]; then
  $GODAEMON --version &> /dev/null
  DAEMON_INSTALLED=$?

  if [ $DAEMON_INSTALLED -eq 0 ]; then
    echo "Go node found. Starting"

    tmux new-window -t $SESSION:4 -n "go-node" $SHELL
    tmux send-keys -t $SESSION:4 "set +o history" C-m

    $GOCLIENT --version &> /dev/null
    CLIENT_INSTALLED=$?
    if [ $DAEMON_INSTALLED -eq 0 ]; then
      echo "${GOCLIENT} installed"

      OMEGA_DIR="${NODES_ROOT}/gonode"
      mkdir -p "${OMEGA_DIR}"

      NODE_CONF="${OMEGA_DIR}/gonode.conf"
      CLIENT_CONF="${OMEGA_DIR}/goctl.conf"


cat > "${NODE_CONF}" <<EOF
addpeer=127.0.0.1:${ALPHA_LISTEN_PORT}
listen=:${OMEGA_LISTEN_PORT}
rpcuser=user
rpcpass=pass
rpclisten=0.0.0.0:${OMEGA_RPC_PORT}
regtest=1
rpccert=${OMEGA_DIR}/rpc.cert
rpckey=${OMEGA_DIR}/rpc.key
debuglevel=trace
EOF

cat > "${CLIENT_CONF}" <<EOF
rpcuser=user
rpcpass=pass
simnet=1
rpccert=${OMEGA_DIR}/rpc.cert
rpcserver=127.0.0.1:${OMEGA_RPC_PORT}
EOF

cat > "${HARNESS_DIR}/omega" <<EOF
#!/usr/bin/env bash
${GOCLIENT} --configfile=${CLIENT_CONF} "\$@"
EOF
chmod +x "${HARNESS_DIR}/omega"

      tmux send-keys -t $SESSION:4 "${GODAEMON} --datadir ${OMEGA_DIR} \
      --configfile ${NODE_CONF}" C-m

    else
      echo "Go CLI client not found"
    fi
  else
    echo "Go node not found"
  fi
fi

tmux select-window -t $SESSION:2
tmux attach-session -t $SESSION
