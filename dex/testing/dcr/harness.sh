#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
SESSION="dcr-harness"
export RPC_USER="user"
export RPC_PASS="pass"
export WALLET_PASS=abc
ALPHA_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
ALPHA_MINING_ADDR="SsXciQNTo3HuV5tX3yy4hXndRWgLMRVC7Ah"
ALPHA_NODE_PORT="19571"
ALPHA_WALLET_PORT="19567"
ALPHA_RPC_PORT="19570"
BETA_WALLET_SEED="3285a47d6a59f9c548b2a72c2c34a2de97967bede3844090102bbba76707fe9d"
BETA_MINING_ADDR="Ssge52jCzbixgFC736RSTrwAnvH3a4hcPRX"
BETA_NODE_PORT="19559"
BETA_WALLET_PORT="19568"
BETA_RPC_PORT="19569"

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

# Beta mine script
cat > "${NODES_ROOT}/harness-ctl/mine-beta" <<EOF
#!/usr/bin/env bash
NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ${NODES_ROOT}/beta/beta-ctl.conf regentemplate
    sleep 0.05
    dcrctl -C ${NODES_ROOT}/beta/beta-ctl.conf generate 1
    if [ $i != $NUM ]; then
      sleep ${MINE_SLEEP}
    fi
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-beta"

# Reorg script
cat > "${NODES_ROOT}/harness-ctl/reorg" <<EOF
#!/usr/bin/env bash
echo "Disconnecting beta from alpha"
sleep 1
./beta addnode 127.0.0.1:${ALPHA_NODE_PORT} remove
echo "Mining a block on alpha"
sleep 1
./mine-alpha 1
echo "Mining 3 blocks on beta"
./mine-beta 3
sleep 2
echo "Reconnecting beta to alpha"
./beta addnode 127.0.0.1:${ALPHA_NODE_PORT} add
sleep 2
grep REORG ${NODES_ROOT}/alpha/logs/simnet/dcrd.log
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

cat > "${NODES_ROOT}/harness-ctl/new-wallet" <<EOF
#!/usr/bin/env bash
./\$1 createnewaccount \$2
EOF
chmod +x "${NODES_ROOT}/harness-ctl/new-wallet"

echo "Writing node config files"
################################################################################
# Configuration Files
################################################################################

# Alpha node config. Not used here, but added for use in dcrdex's markets.json
cat > "${NODES_ROOT}/alpha/dcrd.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/alpha/rpc.cert
rpclisten=127.0.0.1:${ALPHA_RPC_PORT}
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

echo "Starting simnet alpha node"
tmux send-keys -t $SESSION:1 "dcrd --appdata=${NODES_ROOT}/alpha \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${ALPHA_MINING_ADDR} --rpclisten=127.0.0.1:${ALPHA_RPC_PORT} \
--txindex --listen=127.0.0.1:${ALPHA_NODE_PORT} \
--debuglevel=debug \
--whitelist=127.0.0.0/8 --whitelist=::1 \
--simnet; tmux wait-for -S alphadcr" C-m

tmux new-window -t $SESSION:2 -n 'beta' $SHELL
tmux send-keys -t $SESSION:2 "set +o history" C-m
tmux send-keys -t $SESSION:2 "cd ${NODES_ROOT}/beta" C-m

echo "Starting simnet beta node"
tmux send-keys -t $SESSION:2 "dcrd --appdata=${NODES_ROOT}/beta \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--listen=127.0.0.1:${BETA_NODE_PORT} --rpclisten=127.0.0.1:${BETA_RPC_PORT} \
--miningaddr=${BETA_MINING_ADDR} \
--txindex --connect=127.0.0.1:${ALPHA_NODE_PORT} \
--debuglevel=debug \
--whitelist=127.0.0.0/8 --whitelist=::1 \
--simnet; tmux wait-for -S betadcr" C-m

sleep 3

################################################################################
# dcrwallets
################################################################################

# Re-using $MINE to signal whether the wallets need to be created
# from scratch, or if they were loaded from file.
echo "Creating simnet alpha wallet"
ENABLE_TICKET_BUYER="1"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:3" "alpha" ${ALPHA_WALLET_SEED} \
${ALPHA_WALLET_PORT} ${ENABLE_TICKET_BUYER}

echo "Creating simnet beta wallet"
ENABLE_TICKET_BUYER="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:4" "beta" ${BETA_WALLET_SEED} \
${BETA_WALLET_PORT} ${ENABLE_TICKET_BUYER}

# The trading wallets need to be created from scratch every time.
echo "Creating simnet trading wallet 1"
ENABLE_TICKET_BUYER="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:5" "trading1" ${TRADING_WALLET1_SEED} \
${TRADING_WALLET1_PORT} ${ENABLE_TICKET_BUYER}

echo "Creating simnet trading wallet 2"
ENABLE_TICKET_BUYER="0"
"${HARNESS_DIR}/create-wallet.sh" "$SESSION:6" "trading2" ${TRADING_WALLET2_SEED} \
${TRADING_WALLET2_PORT} ${ENABLE_TICKET_BUYER}

sleep 15

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
