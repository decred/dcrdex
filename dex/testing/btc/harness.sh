#!/bin/sh
# Tmux script that sets up a simnet harness.
set -e
#set -x # for verbose output
SESSION="btc-harness"
NODES_ROOT=~/dextest/btc
RPC_USER="user"
RPC_PASS="pass"
ALPHA_LISTEN_PORT="20575"
BETA_LISTEN_PORT="20576"
ALPHA_RPC_PORT="20556"
BETA_RPC_PORT="20557"
ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
BETA_WALLET_SEED="cRHosJjgZ2UWsEAeHYYUFa8Z6viHYXm94GguGtpzMo6qwKBC1DSq"
# Gamma is a named wallet in the alpha wallet directory.
GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
GAMMA_WALLET_PASSWORD="abc"
GAMMA_ADDRESS="2N9Lwbw6DKoNSyTB9xL4e9LXftuPP7XU214"
ALPHA_MINING_ADDR="2MzNGEV9CBZBptm25CZ4rm2TrKF8gfVU8XA"
BETA_MINING_ADDR="2NC2bYfZ9GX3gnDZB8CL7pYLytNKMfVxYDX"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

ALPHA_DIR="${NODES_ROOT}/alpha"
BETA_DIR="${NODES_ROOT}/beta"
HARNESS_DIR="${NODES_ROOT}/harness-ctl"

echo "Writing node config files"
mkdir -p "${ALPHA_DIR}"
mkdir -p "${BETA_DIR}"
mkdir -p "${HARNESS_DIR}"

# Get the absolute path.
NODES_ROOT=$(cd ~/dextest/btc; pwd)

ALPHA_CLI_CFG="-rpcwallet= -rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

BETA_CLI_CFG="-rpcwallet= -rpcport=${BETA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

GAMMA_CLI_CFG="-rpcwallet=gamma -rpcport=${ALPHA_RPC_PORT} -regtest=1 -rpcuser=user -rpcpassword=pass"

# WAIT can be used in a send-keys call along with a `wait-for done` command to
# wait for process termination.
WAIT="; tmux wait-for -S done"

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
tmux send-keys "cd ${ALPHA_DIR}" C-m
echo "Starting simnet alpha node"
tmux send-keys "bitcoind -rpcuser=user -rpcpassword=pass \
  -rpcport=${ALPHA_RPC_PORT} -datadir=${ALPHA_DIR} \
  -txindex=1 -regtest=1 -port=${ALPHA_LISTEN_PORT}; tmux wait-for -S alpha" C-m
sleep 3

################################################################################
# Setup the beta node.
################################################################################

tmux new-window -t $SESSION:1 -n 'beta'
tmux send-keys "cd ${BETA_DIR}" C-m

echo "Starting simnet beta node"
tmux send-keys "bitcoind -rpcuser=user -rpcpassword=pass \
  -rpcport=${BETA_RPC_PORT} -datadir=${BETA_DIR} -txindex=1 -regtest=1 \
  -port=${BETA_LISTEN_PORT}; tmux wait-for -S beta" C-m
sleep 3

################################################################################
# Setup the harness-ctl directory
################################################################################

tmux new-window -t $SESSION:2 -n 'harness-ctl'
tmux send-keys "cd ${HARNESS_DIR}" C-m
sleep 1

cd ${HARNESS_DIR}

cat > "./alpha" <<EOF
#!/bin/sh
bitcoin-cli ${ALPHA_CLI_CFG} "\$@"
EOF
chmod +x "./alpha"

cat > "./mine-alpha" <<EOF
#!/bin/sh
bitcoin-cli ${ALPHA_CLI_CFG} generatetoaddress \$1 ${ALPHA_MINING_ADDR}
EOF
chmod +x "./mine-alpha"

cat > "./gamma" <<EOF
#!/bin/sh
bitcoin-cli ${GAMMA_CLI_CFG} "\$@"
EOF
chmod +x "./gamma"

cat > "./beta" <<EOF
#!/bin/sh
bitcoin-cli ${BETA_CLI_CFG} "\$@"
EOF
chmod +x "./beta"

cat > "./mine-beta" <<EOF
#!/bin/sh
bitcoin-cli ${BETA_CLI_CFG} generatetoaddress \$1 ${BETA_MINING_ADDR}
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
tmux wait-for alpha
tmux wait-for beta
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

tmux send-keys "./beta addnode 127.0.0.1:${ALPHA_LISTEN_PORT} add${WAIT}" C-m\; wait-for done
# This timeout is apparently critical. Give the nodes time to sync.
sleep 1

echo "Generating the genesis block"
tmux send-keys "./alpha generatetoaddress 1 ${ALPHA_MINING_ADDR}${WAIT}" C-m\; wait-for done
sleep 1

#################################################################################
# Setup the alpha wallet
################################################################################

echo "Setting private key for alpha"
tmux send-keys "./alpha sethdseed true ${ALPHA_WALLET_SEED}${WAIT}" C-m\; wait-for done
tmux send-keys "./alpha getnewaddress${WAIT}" C-m\; wait-for done
tmux send-keys "./alpha getnewaddress \"\" \"legacy\"${WAIT}" C-m\; wait-for done
echo "Generating 30 blocks for alpha"
tmux send-keys "./alpha generatetoaddress 30 ${ALPHA_MINING_ADDR}${WAIT}" C-m\; wait-for done

#################################################################################
# Setup the beta wallet
################################################################################

echo "Setting private key for beta"
tmux send-keys "./beta sethdseed true ${BETA_WALLET_SEED}${WAIT}" C-m\; wait-for done
tmux send-keys "./beta getnewaddress${WAIT}" C-m\; wait-for done
tmux send-keys "./beta getnewaddress \"\" \"legacy\"${WAIT}" C-m\; wait-for done
echo "Generating 110 blocks for beta"
tmux send-keys "./beta generatetoaddress 110 ${BETA_MINING_ADDR}${WAIT}" C-m\; wait-for done

################################################################################
# Setup the gamma wallet
################################################################################

# Create  wallet, encrypt it, and send it some bitcoin, mining some blocks too.
echo "Creating the gamma wallet"
tmux send-keys "./alpha createwallet gamma${WAIT}" C-m\; wait-for done
tmux send-keys "./gamma sethdseed true ${GAMMA_WALLET_SEED}${WAIT}" C-m\; wait-for done
tmux send-keys "./gamma getnewaddress${WAIT}" C-m\; wait-for done
tmux send-keys "./gamma getnewaddress \"\" \"legacy\"${WAIT}" C-m\; wait-for done
echo "Encrypting the gamma wallet (may take a minute)"
tmux send-keys "./gamma encryptwallet ${GAMMA_WALLET_PASSWORD}${WAIT}" C-m\; wait-for done
echo "Sending 84 BTC to gamma in 8 blocks"
for i in 10 18 5 7 1 15 3 25
do
  tmux send-keys "./alpha sendtoaddress ${GAMMA_ADDRESS} ${i}${WAIT}" C-m\; wait-for done
  tmux send-keys "./mine-alpha 1${WAIT}" C-m\; wait-for done
done

tmux attach-session -t $SESSION
