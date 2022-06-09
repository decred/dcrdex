#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
SESSION="dcr-harness"
export RPC_USER="user"
export RPC_PASS="pass"
export WALLET_PASS=abc

# --listen and --rpclisten ports for alpha and beta nodes.
# The ports are exported for use by create-wallet.sh.
export ALPHA_NODE_PORT="19560"
export ALPHA_NODE_RPC_PORT="19561"
export BETA_NODE_PORT="19570"
export BETA_NODE_RPC_PORT="19571"

ALPHA_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
ALPHA_MINING_ADDR="SsXciQNTo3HuV5tX3yy4hXndRWgLMRVC7Ah"
ALPHA_WALLET_RPC_PORT="19562"
ALPHA_WALLET_HTTPPROF_PORT="19563"

# The alpha wallet clone uses the same seed as the alpha wallet.
ALPHA_WALLET_CLONE_RPC_PORT="19564"

BETA_WALLET_SEED="3285a47d6a59f9c548b2a72c2c34a2de97967bede3844090102bbba76707fe9d"
BETA_MINING_ADDR="Ssge52jCzbixgFC736RSTrwAnvH3a4hcPRX"
BETA_WALLET_RPC_PORT="19572"
BETA_WALLET_HTTPPROF_PORT="19573"

TRADING_WALLET1_SEED="31cc0eeb220aa5b1f1ab3b6c47529d737976af1a556b156a665408f1711c962f"
TRADING_WALLET1_ADDRESS="SsjTp2QaT8qkPGWKKSeFi4DtaZsgkoPbgXt"
TRADING_WALLET1_PORT="19581"

TRADING_WALLET2_SEED="3db72efa55b9e6cce9c27dde9bea848c6199004f9b1ae2add3b04389495edb9c"
TRADING_WALLET2_ADDRESS="SsYW5LPmGCvvHuWok8U9FQu1kotv8LpvoEt"
TRADING_WALLET2_PORT="19582"

# WAIT can be used in a send-keys call along with a `wait-for donedcr` command to
# wait for process termination.
WAIT="; tmux wait-for -S donedcr"

NODES_ROOT=~/dextest/dcr
export NODES_ROOT

export SHELL=$(which bash)

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi
mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

MINE=1
# Bump sleep up to 3 if we have to mine a lot of blocks, because dcrwallet
# doesn't always keep up.
MINE_SLEEP=3
if [ -f ./harnesschain.tar.gz ]; then
  echo "Seeding blockchain from compressed file"
  MINE=0
  MINE_SLEEP=0.5
  mkdir -p "${NODES_ROOT}/alpha/data"
  mkdir -p "${NODES_ROOT}/beta/data"
  tar -xzf ./harnesschain.tar.gz -C ${NODES_ROOT}/alpha/data
  cp -r ${NODES_ROOT}/alpha/data/simnet ${NODES_ROOT}/beta/data/simnet
fi

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

# Add wallet script
HARNESS_DIR=$(dirname $0)
export HARNESS_DIR
cp "${HARNESS_DIR}/create-wallet.sh" "${NODES_ROOT}/harness-ctl/create-wallet"

# Script to send funds from alpha to address
cat > "${NODES_ROOT}/harness-ctl/fund" <<EOF
#!/usr/bin/env bash
./alpha sendtoaddress \$@
sleep 0.5
./mine-alpha 1
EOF
chmod +x "${NODES_ROOT}/harness-ctl/fund"

# Alpha mine script
cat > "${NODES_ROOT}/harness-ctl/mine-alpha" <<EOF
#!/usr/bin/env bash
NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ${NODES_ROOT}/alpha/alpha-ctl.conf regentemplate
    sleep 0.05
    dcrctl -C ${NODES_ROOT}/alpha/alpha-ctl.conf generate 1
    if [ $i != $NUM ]; then
      sleep ${MINE_SLEEP}
    fi
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-alpha"

# A node ctl config only exists for beta because some node functions are not
# accessible through the beta spv wallet.
cat > "${NODES_ROOT}/beta/beta_node-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert="${NODES_ROOT}/beta/rpc.cert"
rpcserver=127.0.0.1:${BETA_NODE_RPC_PORT}
EOF

# node ctl script
cat > "${NODES_ROOT}/harness-ctl/beta_node" <<EOF
#!/usr/bin/env bash
dcrctl -C "${NODES_ROOT}/beta/beta_node-ctl.conf" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/beta_node"

# Beta mine script
cat > "${NODES_ROOT}/harness-ctl/mine-beta" <<EOF
#!/usr/bin/env bash
NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ${NODES_ROOT}/beta/beta_node-ctl.conf regentemplate
    sleep 0.05
    dcrctl -C ${NODES_ROOT}/beta/beta_node-ctl.conf generate 1
    if [ $i != $NUM ]; then
      sleep ${MINE_SLEEP}
    fi
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-beta"

# Alpha wallet clone script.
# The alpha wallet clone connects to the beta node so it can vote on tickets
# purchased with the alpha wallet after the alpha node is disconnected. This
# alpha wallet clone does not need to purchase tickets or do anything else
# really other than just voting on tickets purchased with the alpha wallet.
cat > "${NODES_ROOT}/harness-ctl/clone-w-alpha" <<EOF
"${HARNESS_DIR}/create-wallet" "$SESSION:7" "alpha-clone" ${ALPHA_WALLET_SEED} \
${ALPHA_WALLET_CLONE_RPC_PORT} 0 1 # ENABLE_VOTING=1 enables voting but not ticket buyer
tmux select-window -t $SESSION:0 # return to the ctl window
EOF
chmod +x "${NODES_ROOT}/harness-ctl/clone-w-alpha"

# Reorg script
cat > "${NODES_ROOT}/harness-ctl/reorg" <<EOF
#!/usr/bin/env bash
REORG_NODE="alpha"
VALID_NODE="beta"
REORG_DEPTH=2

if [ "\$1" = "beta" ]; then
  REORG_NODE="beta"
  VALID_NODE="alpha"
fi

if [ "\$2" != "" ]; then
  REORG_DEPTH=\$2
fi

echo "Current alpha, beta best blocks"
./alpha getbestblock && ./beta_node getbestblock

echo "Disconnecting beta from alpha"
./beta_node addnode 127.0.0.1:${ALPHA_NODE_PORT} remove
sleep 1

# Start the alpha wallet clone to enable the beta node receive
# votes while disconnected from the alpha node.
"${NODES_ROOT}/harness-ctl/clone-w-alpha"

# Mine REORG_DEPTH blocks on REORG_NODE and REORG_DEPTH+1 blocks
# on VALID_NODE while disconnected.
echo "Mining \${REORG_DEPTH} blocks on \${REORG_NODE}"
./mine-\${REORG_NODE} \${REORG_DEPTH}
sleep 1
echo "Mining \$((REORG_DEPTH+1)) blocks on \${VALID_NODE}"
./mine-\${VALID_NODE} \$((REORG_DEPTH+1))
sleep 1

echo "Diverged alpha, beta best blocks" && ./alpha getbestblock && ./beta_node getbestblock

# Stop alpha wallet clone, no longer needed.
echo "Stopping alpha-clone wallet"
tmux send-keys -t $SESSION:7 C-c
tmux send-keys -t $SESSION:7 exit C-m

echo "Reconnecting beta to alpha"
./beta_node addnode 127.0.0.1:${ALPHA_NODE_PORT} add
sleep 1

echo "Reconnected alpha, beta best blocks" && ./alpha getbestblock && ./beta_node getbestblock

echo "${NODES_ROOT}/\${REORG_NODE}/logs/simnet/dcrd.log:"
grep REORG ${NODES_ROOT}/\${REORG_NODE}/logs/simnet/dcrd.log | tail -4
EOF
chmod +x "${NODES_ROOT}/harness-ctl/reorg"

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:3 C-c
tmux send-keys -t $SESSION:4 C-c
tmux send-keys -t $SESSION:5 C-c
tmux send-keys -t $SESSION:6 C-c
sleep 0.2
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux wait-for alphadcr
tmux wait-for betadcr
tmux kill-session
EOF
chmod +x "${NODES_ROOT}/harness-ctl/quit"

echo "Writing node config files"
################################################################################
# Configuration Files
################################################################################

# Alpha node config. Not used here, but added for use in dcrdex's markets.json
cat > "${NODES_ROOT}/alpha/dcrd.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/alpha/rpc.cert
rpclisten=127.0.0.1:${ALPHA_NODE_RPC_PORT}
EOF


################################################################################
# Start harness
################################################################################

echo "Starting harness"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${NODES_ROOT}/harness-ctl" C-m

################################################################################
# dcrd Nodes
################################################################################

tmux new-window -t $SESSION:1 -n 'alpha' $SHELL
tmux send-keys -t $SESSION:1 "set +o history" C-m
tmux send-keys -t $SESSION:1 "cd ${NODES_ROOT}/alpha" C-m

echo "Starting simnet alpha node (txindex for server)"
tmux send-keys -t $SESSION:1 "dcrd --appdata=${NODES_ROOT}/alpha \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${ALPHA_MINING_ADDR} --rpclisten=:${ALPHA_NODE_RPC_PORT} \
--txindex --listen=:${ALPHA_NODE_PORT} \
--debuglevel=debug \
--whitelist=127.0.0.0/8 --whitelist=::1 \
--simnet; tmux wait-for -S alphadcr" C-m

tmux new-window -t $SESSION:2 -n 'beta' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${NODES_ROOT}/beta" C-m

echo "Starting simnet beta node (no txindex for a typical client)"
tmux send-keys -t $SESSION:2 "dcrd --appdata=${NODES_ROOT}/beta \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--listen=:${BETA_NODE_PORT} --rpclisten=:${BETA_NODE_RPC_PORT} \
--miningaddr=${BETA_MINING_ADDR} \
--connect=127.0.0.1:${ALPHA_NODE_PORT} \
--debuglevel=debug \
--whitelist=127.0.0.0/8 --whitelist=::1 \
--simnet; tmux wait-for -S betadcr" C-m

sleep 3

################################################################################
# dcrwallets
################################################################################

echo "Creating simnet alpha wallet"
USE_SPV="0"
ENABLE_VOTING="2" # 2 = enable voting and ticket buyer
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:3" "alpha" ${ALPHA_WALLET_SEED} \
${ALPHA_WALLET_RPC_PORT} ${USE_SPV} ${ENABLE_VOTING} ${ALPHA_WALLET_HTTPPROF_PORT}
# alpha uses walletpassphrase/walletlock.

# SPV wallets will declare peers stalled and disconnect with only ancient blocks
# from the archive, so we must mine a couple blocks first, but only now after the
# voting wallet (alpha) is running.
tmux send-keys -t $SESSION:0 "./mine-alpha 2${WAIT}" C-m\; wait-for donedcr

echo "Creating simnet beta wallet"
USE_SPV="1"
ENABLE_VOTING="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:4" "beta" ${BETA_WALLET_SEED} \
${BETA_WALLET_RPC_PORT} ${USE_SPV} ${ENABLE_VOTING} ${BETA_WALLET_HTTPPROF_PORT}

# The trading wallets need to be created from scratch every time.
echo "Creating simnet trading wallet 1"
USE_SPV="1"
ENABLE_VOTING="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:5" "trading1" ${TRADING_WALLET1_SEED} \
${TRADING_WALLET1_PORT} ${USE_SPV} ${ENABLE_VOTING}

echo "Creating simnet trading wallet 2"
USE_SPV="1"
ENABLE_VOTING="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:6" "trading2" ${TRADING_WALLET2_SEED} \
${TRADING_WALLET2_PORT} ${USE_SPV} ${ENABLE_VOTING}

sleep 15

# Give beta's "default" account a password, so it uses unlockaccount/lockaccount.
tmux send-keys -t $SESSION:0 "./beta setaccountpassphrase default ${WALLET_PASS}${WAIT}" C-m\; wait-for donedcr

# Lock the wallet so we know we can function with just account unlocking.
tmux send-keys -t $SESSION:0 "./beta walletlock${WAIT}" C-m\; wait-for donedcr

# Create fee account on alpha wallet for use by dcrdex simnet instances.
tmux send-keys -t $SESSION:0 "./alpha createnewaccount server_fees${WAIT}" C-m\; wait-for donedcr
tmux send-keys -t $SESSION:0 "./alpha getmasterpubkey server_fees${WAIT}" C-m\; wait-for donedcr

################################################################################
# Prepare the wallets
################################################################################

tmux select-window -t $SESSION:0
for WALLET in alpha beta trading1 trading2; do
  tmux send-keys -t $SESSION:0 "./${WALLET} getnewaddress${WAIT}" C-m\; wait-for donedcr
  tmux send-keys -t $SESSION:0 "./${WALLET} getnewaddress${WAIT}" C-m\; wait-for donedcr
done

if [ "$MINE" = "1" ]; then
  echo "Mining 600 blocks on alpha"
  echo "Mining blocks 0 through 99"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr

  # Send beta some dough while we're here.
  tmux send-keys -t $SESSION:0 "./alpha sendtoaddress ${BETA_MINING_ADDR} 1000${WAIT}" C-m\; wait-for donedcr

  echo "Mining blocks 100 through 199"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr
  echo "Mining blocks 200 through 299"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr
  echo "Mining blocks 300 through 399"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr
  echo "Mining blocks 400 through 499"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr
  # Don't stop here. There's a period of high ticket price where the avaialable
  # balance for alpha is really low. Go to 600 to get through it.
  echo "Mining blocks 500 through 599"
  tmux send-keys -t $SESSION:0 "./mine-alpha 100${WAIT}" C-m\; wait-for donedcr

  # Have alpha send some credits to the other wallets
  for i in 10 18 5 7 1 15 3 25
  do
    RECIPIENTS="{\"${BETA_MINING_ADDR}\":${i},\"${TRADING_WALLET1_ADDRESS}\":${i},\"${TRADING_WALLET2_ADDRESS}\":${i}}"
    tmux send-keys -t $SESSION:0 "./alpha sendmany default '${RECIPIENTS}'${WAIT}" C-m\; wait-for donedcr
  done
fi

# Have alpha share a little more wealth, esp. for trade_simnet_test.go
RECIPIENTS="{\"${TRADING_WALLET1_ADDRESS}\":24,\"${TRADING_WALLET2_ADDRESS}\":24,\"${BETA_MINING_ADDR}\":24}"
for i in {1..60}; do
  tmux send-keys -t $SESSION:0 "./alpha sendmany default '${RECIPIENTS}'${WAIT}" C-m\; wait-for donedcr
done

sleep 0.5
tmux send-keys -t $SESSION:0 "./mine-alpha 2${WAIT}" C-m\; wait-for donedcr

# Reenable history and attach to the control session.
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
