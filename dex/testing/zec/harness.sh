#!/usr/bin/env bash

# IMPORTANT NOTE: It can take the beta node a little bit to get caught up with
# alpha after the harness initializes.

SYMBOL="zec"
DAEMON="zcashd"
CLI="zcash-cli"
RPC_USER="user"
RPC_PASS="pass"
ALPHA_LISTEN_PORT="33764"
BETA_LISTEN_PORT="33765"
DELTA_LISTEN_PORT="33766"
GAMMA_LISTEN_PORT="33767"
ALPHA_RPC_PORT="33768"
BETA_RPC_PORT="33769"
DELTA_RPC_PORT="33770"
GAMMA_RPC_PORT="33771"

set -ex
NODES_ROOT=~/dextest/${SYMBOL}
rm -rf "${NODES_ROOT}"
SOURCE_DIR=$(pwd)

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
DELTA_DIR="${NODES_ROOT}/delta"
GAMMA_DIR="${NODES_ROOT}/gamma"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

echo "Writing node config files"
mkdir -p "${HARNESS_DIR}"

WALLET_PASSWORD="abc"

ALPHA_CLI_CFG="-rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -conf=${ALPHA_DIR}/alpha.conf"
BETA_CLI_CFG="-rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -conf=${BETA_DIR}/beta.conf"
DELTA_CLI_CFG="-rpcport=${DELTA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -conf=${DELTA_DIR}/delta.conf"
GAMMA_CLI_CFG="-rpcport=${GAMMA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass -conf=${GAMMA_DIR}/gamma.conf"

# DONE can be used in a send-keys call along with a `wait-for btc` command to
# wait for process termination.
DONE="; tmux wait-for -S ${SYMBOL}"
WAIT="wait-for ${SYMBOL}"

SESSION="${SYMBOL}-harness"

SHELL=$(which bash)

################################################################################
# Load prepared wallet if the files exist.
################################################################################

mkdir -p "${ALPHA_DIR}"
mkdir -p "${BETA_DIR}"
mkdir -p "${DELTA_DIR}"
mkdir -p "${GAMMA_DIR}"

# mkdir -p ${ALPHA_DIR}/regtest
# cp ${SOURCE_DIR}/alpha_wallet.dat ${ALPHA_DIR}/regtest/wallet.dat
# mkdir -p ${BETA_DIR}/regtest
# cp ${SOURCE_DIR}/beta_wallet.dat ${BETA_DIR}/regtest/wallet.dat

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
txindex=1
port=${ALPHA_LISTEN_PORT}
regtest=1
rpcport=${ALPHA_RPC_PORT}
exportdir=${SOURCE_DIR}
# Activate all the things.
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
EOF

cat > "${BETA_DIR}/beta.conf" <<EOF
rpcuser=user
rpcpassword=pass
txindex=1
regtest=1
rpcport=${BETA_RPC_PORT}
exportdir=${SOURCE_DIR}
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
EOF

cat > "${DELTA_DIR}/delta.conf" <<EOF
rpcuser=user
rpcpassword=pass
regtest=1
rpcport=${DELTA_RPC_PORT}
exportdir=${SOURCE_DIR}
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
EOF

cat > "${GAMMA_DIR}/gamma.conf" <<EOF
rpcuser=user
rpcpassword=pass
regtest=1
rpcport=${GAMMA_RPC_PORT}
exportdir=${SOURCE_DIR}
nuparams=5ba81b19:1
nuparams=76b809bb:1
nuparams=2bb40e60:1
nuparams=f5b9230b:1
nuparams=e9ff75a6:2
nuparams=c2d6d0b4:3
EOF

################################################################################
# Start the alpha node.
################################################################################

tmux rename-window -t $SESSION:0 'alpha'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${ALPHA_DIR}" C-m
echo "Starting simnet alpha node"
tmux send-keys -t $SESSION:0 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} -conf=alpha.conf \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT} -fallbackfee=0.00001 \
  -printtoconsole; tmux wait-for -S alpha${SYMBOL}" C-m

################################################################################
# Setup the beta node.
################################################################################

tmux new-window -t $SESSION:1 -n 'beta' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${BETA_DIR}" C-m

echo "Starting simnet beta node"
tmux send-keys -t $SESSION:1 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -conf=beta.conf -txindex=1 -regtest=1 \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -port=${BETA_LISTEN_PORT} -fallbackfee=0.00001 -printtoconsole; \
  tmux wait-for -S beta${SYMBOL}" C-m

################################################################################
# Setup the delta node.
################################################################################

tmux new-window -t $SESSION:2 -n 'delta' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${DELTA_DIR}" C-m

echo "Starting simnet delta node"
tmux send-keys -t $SESSION:2 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${DELTA_RPC_PORT} -datadir=${DELTA_DIR} -conf=delta.conf -regtest=1 \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -port=${DELTA_LISTEN_PORT} -fallbackfee=0.00001 -printtoconsole; \
  tmux wait-for -S delta${SYMBOL}" C-m

################################################################################
# Setup the gamma node.
################################################################################

tmux new-window -t $SESSION:3 -n 'gamma' $SHELL
tmux send-keys -t $SESSION:3 "set +o history" C-m
tmux send-keys -t $SESSION:3 "cd ${GAMMA_DIR}" C-m

echo "Starting simnet gamma node"
tmux send-keys -t $SESSION:3 "${DAEMON} -rpcuser=user -rpcpassword=pass \
  -rpcport=${GAMMA_RPC_PORT} -datadir=${GAMMA_DIR} -conf=gamma.conf -regtest=1 \
  -debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
  -whitelist=127.0.0.0/8 -whitelist=::1 \
  -port=${GAMMA_LISTEN_PORT} -fallbackfee=0.00001 -printtoconsole; \
  tmux wait-for -S gamma${SYMBOL}" C-m
sleep 30

################################################################################
# Setup the harness-ctl directory
################################################################################

tmux new-window -t $SESSION:4 -n 'harness-ctl' $SHELL
tmux send-keys -t $SESSION:4 "set +o history" C-m
tmux send-keys -t $SESSION:4 "cd ${HARNESS_DIR}" C-m
sleep 1

cd ${HARNESS_DIR}

# start-wallet, connect-alpha, and stop-wallet are used by loadbot to set up and
# run new wallets.
cat > "./start-wallet" <<EOF
#!/usr/bin/env bash

mkdir ${NODES_ROOT}/\$1

printf "rpcuser=user\nrpcpassword=pass\nregtest=1\nrpcport=\$2\nexportdir=${SOURCE_DIR}\nnuparams=5ba81b19:1\nnuparams=76b809bb:1\nnuparams=2bb40e60:1\nnuparams=f5b9230b:1\nnuparams=e9ff75a6:2\nnuparams=c2d6d0b4:3\n" > ${NODES_ROOT}/\$1/\$1.conf

${DAEMON} -rpcuser=user -rpcpassword=pass \
-rpcport=\$2 -datadir=${NODES_ROOT}/\$1 -regtest=1 -conf=\$1.conf \
-debug=rpc -debug=net -debug=mempool -debug=walletdb -debug=addrman -debug=mempoolrej \
-whitelist=127.0.0.0/8 -whitelist=::1 \
-port=\$3 -fallbackfee=0.00001 -printtoconsole
EOF
chmod +x "./start-wallet"

cat > "./connect-alpha" <<EOF
#!/usr/bin/env bash
${CLI} -rpcport=\$1 -regtest=1 -rpcuser=user -rpcpassword=pass addnode 127.0.0.1:${ALPHA_LISTEN_PORT} onetry
EOF
chmod +x "./connect-alpha"

cat > "./stop-wallet" <<EOF
#!/usr/bin/env bash
${CLI} -rpcport=\$1 -regtest=1 -rpcuser=user -rpcpassword=pass stop
EOF
chmod +x "./stop-wallet"

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

cat > "./gamma" <<EOF
#!/usr/bin/env bash
${CLI} ${GAMMA_CLI_CFG} "\$@"
EOF
chmod +x "./gamma"

cat > "./mine-alpha" <<EOF
#!/usr/bin/env bash
${CLI} ${ALPHA_CLI_CFG} generate \$1
EOF
chmod +x "./mine-alpha"

cat > "./mine-beta" <<EOF
#!/usr/bin/env bash
${CLI} ${BETA_CLI_CFG} generate \$1
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

cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:0 C-c
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux send-keys -t $SESSION:3 C-c
tmux wait-for alpha${SYMBOL}
tmux wait-for beta${SYMBOL}
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

sleep 10

tmux send-keys -t $SESSION:4 "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:4 "./delta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:4 "./gamma addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${DONE}" C-m\; ${WAIT}
# This timeout is apparently critical. Give the nodes time to sync.
sleep 3

echo "Generating the genesis block"
tmux send-keys -t $SESSION:4 "./alpha generate 1${DONE}" C-m\; ${WAIT}
sleep 1

tmux send-keys -t $SESSION:4 "./alpha z_importwallet ${SOURCE_DIR}/alphawallet ${DONE}" C-m\; ${WAIT}
tmux send-keys -t $SESSION:4 "./beta z_importwallet ${SOURCE_DIR}/betawallet ${DONE}" C-m\; ${WAIT}

echo "Generating 400 blocks for alpha"
tmux send-keys -t $SESSION:4 "./alpha generate 400${DONE}" C-m\; ${WAIT}

#################################################################################
# Send gamma and delta some coin
################################################################################

ALPHA_ADDR="tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD"
BETA_ADDR="tmSog4freWuq1aC13yf1996fy4qXPmv3GTB"
DELTA_ADDR=`./delta getnewaddress`
GAMMA_ADDR=`./gamma getnewaddress`

# Send the lazy wallets some dough.
echo "Sending 174 ZEC to beta in 8 blocks"
for i in 100 18 5 7 1 15 3 25
do
    tmux send-keys -t $SESSION:4 "./alpha sendtoaddress ${BETA_ADDR} ${i}${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:4 "./alpha sendtoaddress ${DELTA_ADDR} ${i}${DONE}" C-m\; ${WAIT}
    tmux send-keys -t $SESSION:4 "./alpha sendtoaddress ${GAMMA_ADDR} ${i}${DONE}" C-m\; ${WAIT}
done

tmux send-keys -t $SESSION:4 "./mine-alpha 2${DONE}" C-m\; ${WAIT}

# Reenable history and attach to the control session.
tmux send-keys -t $SESSION:4 "set -o history" C-m
tmux attach-session -t $SESSION
