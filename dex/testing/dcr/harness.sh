#!/bin/sh
# Tmux script that sets up a simnet harness.
set -e
set -x # for verbose output
SESSION="dcr-harness"
NODES_ROOT=~/dextest/dcr
RPC_USER="user"
RPC_PASS="pass"
ALPHA_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
BETA_WALLET_SEED="aabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbc"
WALLET_PASS=123
ALPHA_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
BETA_MINING_ADDR="SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y"
ALPHA_WALLET_PORT="19567"
BETA_WALLET_PORT="19568"
ALPHA_RPC_PORT="19570"
BETA_RPC_PORT="19569"
ALPHA_PORT="19571"

# WAIT can be used in a send-keys call along with a `wait-for donedcr` command to
# wait for process termination.
WAIT="; tmux wait-for -S donedcr"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

echo "Writing node config files"
mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

################################################################################
# Configuration Files
################################################################################

# Alpha ctl config
cat > "${NODES_ROOT}/alpha/alpha-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/alpha/rpc.cert
rpcserver=127.0.0.1:${ALPHA_WALLET_PORT}
EOF

# Beta ctl config
cat > "${NODES_ROOT}/beta/beta-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/beta/rpc.cert
rpcserver=127.0.0.1:${BETA_WALLET_PORT}
EOF

# Alpha wallet config
cat > "${NODES_ROOT}/alpha/w-alpha.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/alpha/rpc.cert
logdir=${NODES_ROOT}/alpha/log
appdata=${NODES_ROOT}/alpha
simnet=1
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=4
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:${ALPHA_RPC_PORT}
rpclisten=127.0.0.1:${ALPHA_WALLET_PORT}
nogrpc=1
EOF

# Beta wallet config
cat > "${NODES_ROOT}/beta/w-beta.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/beta/rpc.cert
logdir=${NODES_ROOT}/beta/log
appdata=${NODES_ROOT}/beta
simnet=1
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:${BETA_RPC_PORT}
rpclisten=127.0.0.1:${BETA_WALLET_PORT}
nogrpc=1
EOF

################################################################################
# Control Scripts
################################################################################

# Beta ctl
cat > "${NODES_ROOT}/harness-ctl/alpha" <<EOF
#!/bin/sh
dcrctl -C ${NODES_ROOT}/alpha/alpha-ctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/alpha"

# Alpha ctl
cat > "${NODES_ROOT}/harness-ctl/beta" <<EOF
#!/bin/sh
dcrctl -C ${NODES_ROOT}/beta/beta-ctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/beta"

# Alpha mine script
cat > "${NODES_ROOT}/harness-ctl/mine-alpha" <<EOF
#!/bin/sh
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ${NODES_ROOT}/alpha/alpha-ctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-alpha"

# Beta mine script
cat > "${NODES_ROOT}/harness-ctl/mine-beta" <<EOF
#!/bin/sh
NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C ${NODES_ROOT}/beta/beta-ctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/harness-ctl/mine-beta"

# Reorg script
cat > "${NODES_ROOT}/harness-ctl/reorg" <<EOF
#!/bin/sh
echo "Disconnecting beta from alpha"
sleep 1
./beta addnode 127.0.0.1:${ALPHA_PORT} remove
echo "Mining a block on alpha"
sleep 1
./mine-alpha 1
echo "Mining 3 blocks on beta"
./mine-beta 3
sleep 2
echo "Reconnecting beta to alpha"
./beta addnode 127.0.0.1:${ALPHA_PORT} add
sleep 2
grep REORG ${NODES_ROOT}/alpha/logs/simnet/dcrd.log
EOF
chmod +x "${NODES_ROOT}/harness-ctl/reorg"

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/bin/sh
tmux send-keys -t $SESSION:0 C-c
tmux send-keys -t $SESSION:1 C-c
tmux wait-for alphadcr
tmux wait-for betadcr
tmux kill-session
EOF
chmod +x "${NODES_ROOT}/harness-ctl/quit"

################################################################################
# dcrd Nodes
################################################################################

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

tmux rename-window -t $SESSION:0 'alpha'
tmux send-keys "cd ${NODES_ROOT}/alpha" C-m

echo "Starting simnet alpha node"
tmux send-keys "dcrd --appdata=${NODES_ROOT}/alpha \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${ALPHA_MINING_ADDR} --rpclisten=127.0.0.1:${ALPHA_RPC_PORT} \
--txindex --listen=127.0.0.1:${ALPHA_PORT} \
--debuglevel=debug \
--simnet; tmux wait-for -S alphadcr" C-m

tmux new-window -t $SESSION:1 -n 'beta'
tmux send-keys "cd ${NODES_ROOT}/beta" C-m

echo "Starting simnet beta node"
tmux send-keys "dcrd --appdata=${NODES_ROOT}/beta \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--listen=127.0.0.1:19559 --rpclisten=127.0.0.1:${BETA_RPC_PORT} \
--miningaddr=${BETA_MINING_ADDR} \
--txindex --connect=127.0.0.1:${ALPHA_PORT} \
--debuglevel=debug \
--simnet; tmux wait-for -S betadcr" C-m

sleep 3

################################################################################
# dcrwallets
################################################################################

tmux new-window -t $SESSION:2 -n 'w-alpha'
tmux send-keys "cd ${NODES_ROOT}/alpha" C-m
tmux send-keys "dcrwallet -C w-alpha.conf --create" C-m
echo "Creating simnet alpha wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${ALPHA_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C w-alpha.conf " C-m

tmux new-window -t $SESSION:3 -n 'w-beta'
tmux send-keys "cd ${NODES_ROOT}/beta" C-m
tmux send-keys "dcrwallet -C w-beta.conf --create" C-m
echo "Creating simnet beta wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${BETA_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C w-beta.conf --debuglevel=debug" C-m

sleep 30

################################################################################
# Prepare the wallets
################################################################################

tmux new-window -t $SESSION:4 -n 'harness-ctl'
tmux send-keys "cd ${NODES_ROOT}/harness-ctl" C-m

tmux send-keys "./alpha getnewaddress${WAIT}" C-m\; wait-for donedcr
tmux send-keys "./alpha getnewaddress${WAIT}" C-m\; wait-for donedcr
tmux send-keys "./beta getnewaddress${WAIT}" C-m\; wait-for donedcr
tmux send-keys "./beta getnewaddress${WAIT}" C-m\; wait-for donedcr

echo "Mining 160 blocks on alpha"
tmux send-keys "./mine-alpha 160${WAIT}" C-m\; wait-for donedcr

sleep 5

# Have beta send some credits to alpha
for i in 10 18 5 7 1 15 3 25
do
  tmux send-keys "./alpha sendtoaddress ${BETA_MINING_ADDR} ${i}${WAIT}" C-m\; wait-for donedcr
  tmux send-keys "./mine-alpha 1${WAIT}" C-m\; wait-for donedcr
done

tmux attach-session -t $SESSION
