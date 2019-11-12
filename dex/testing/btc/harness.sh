#!/bin/sh
# Tmux script that sets up a simnet harness.
set -e
SESSION="harness"
NODES_ROOT=~/harness
RPC_USER="user"
RPC_PASS="pass"
ALPHA_WALLET_SEED="b280922d2cffda44648346412c5ec97f429938105003730414f10b01e1402eac"
BETA_WALLET_SEED="aabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbcaabbc"
WALLET_PASS=123
ALPHA_MINING_ADDR="SspUvSyDGSzvPz2NfdZ5LW15uq6rmuGZyhL"
BETA_MINING_ADDR="SsiuwSRYvH7pqWmRxFJWR8Vmqc3AWsjmK2Y"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

echo "Writing node config files"
mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/w-alpha"
mkdir -p "${NODES_ROOT}/w-beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

cat > "${NODES_ROOT}/alpha/alpha-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/alpha/rpc.cert
rpcserver=127.0.0.1:19556
EOF

cat > "${NODES_ROOT}/beta/beta-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/beta/rpc.cert
rpcserver=127.0.0.1:19560
EOF

cat > "${NODES_ROOT}/w-alpha/w-alpha-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/w-alpha/rpc.cert
rpcserver=127.0.0.1:19557
EOF

cat > "${NODES_ROOT}/w-beta/w-beta-ctl.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/w-beta/rpc.cert
rpcserver=127.0.0.1:19562
EOF

cat > "${NODES_ROOT}/w-alpha/w-alpha.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/alpha/rpc.cert
logdir=${NODES_ROOT}/w-alpha/log
appdata=${NODES_ROOT}/w-alpha
simnet=1
pass=${WALLET_PASS}
EOF

cat > "${NODES_ROOT}/w-beta/w-beta.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
cafile=${NODES_ROOT}/beta/rpc.cert
logdir=${NODES_ROOT}/w-beta/log
appdata=${NODES_ROOT}/w-beta
simnet=1
enablevoting=1
enableticketbuyer=1
ticketbuyer.limit=4
pass=${WALLET_PASS}
rpcconnect=127.0.0.1:19560
grpclisten=127.0.0.1:19561
rpclisten=127.0.0.1:19562
EOF

cd ${NODES_ROOT} && tmux new-session -d -s $SESSION

################################################################################
# Setup the alpha node.
################################################################################
cat > "${NODES_ROOT}/alpha/ctl" <<EOF
#!/bin/sh
dcrctl -C alpha-ctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/alpha/ctl"

tmux rename-window -t $SESSION:0 'alpha'
tmux send-keys "cd ${NODES_ROOT}/alpha" C-m

echo "Starting simnet alpha node"
tmux send-keys "dcrd --appdata=${NODES_ROOT}/alpha \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--miningaddr=${ALPHA_MINING_ADDR} \
--txindex \
--debuglevel=debug \
--simnet" C-m

################################################################################
# Setup the alpha node's dcrctl (alpha-ctl).
################################################################################
cat > "${NODES_ROOT}/alpha/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C alpha-ctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/alpha/mine"

tmux new-window -t $SESSION:1 -n 'alpha-ctl'
tmux send-keys "cd ${NODES_ROOT}/alpha" C-m

sleep 3
# mine some blocks to start the chain.
echo "Mining 2 blocks"
tmux send-keys "./mine 2" C-m
sleep 1

################################################################################
# Setup the alpha wallet.
################################################################################
cat > "${NODES_ROOT}/w-alpha/ctl" <<EOF
#!/bin/sh
dcrctl -C w-alpha-ctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/w-alpha/ctl"

tmux new-window -t $SESSION:2 -n 'w-alpha'
tmux send-keys "cd ${NODES_ROOT}/w-alpha" C-m
tmux send-keys "dcrwallet -C w-alpha.conf --create" C-m
echo "Creating simnet alpha wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${ALPHA_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C w-alpha.conf " C-m

################################################################################
# Setup the alpha wallet's dcrctl (w-alpha-ctl).
################################################################################
sleep 5
tmux new-window -t $SESSION:3 -n 'w-alpha-ctl'
tmux send-keys "cd ${NODES_ROOT}/w-alpha" C-m
tmux send-keys "./ctl getbalance"

################################################################################
# Setup the beta node.
################################################################################
cat > "${NODES_ROOT}/beta/ctl" <<EOF
#!/bin/sh
dcrctl -C beta-ctl.conf \$*
EOF
chmod +x "${NODES_ROOT}/beta/ctl"

tmux new-window -t $SESSION:4 -n 'beta'
tmux send-keys "cd ${NODES_ROOT}/beta" C-m

echo "Starting simnet beta node"
tmux send-keys "dcrd --appdata=${NODES_ROOT}/beta \
--rpcuser=${RPC_USER} --rpcpass=${RPC_PASS} \
--listen=127.0.0.1:19559 --rpclisten=127.0.0.1:19560 \
--miningaddr=${BETA_MINING_ADDR} \
--txindex --connect=127.0.0.1:18555 \
--debuglevel=debug \
--simnet" C-m

# ###############################################################################
# Setup the beta node's dcrctl (beta-ctl).
################################################################################
sleep 3
cat > "${NODES_ROOT}/beta/mine" <<EOF
#!/bin/sh
  NUM=1
  case \$1 in
      ''|*[!0-9]*)  ;;
      *) NUM=\$1 ;;
  esac
  for i in \$(seq \$NUM) ; do
    dcrctl -C beta-ctl.conf generate 1
    sleep 0.5
  done
EOF
chmod +x "${NODES_ROOT}/beta/mine"

tmux new-window -t $SESSION:5 -n 'beta-ctl'
tmux send-keys "cd ${NODES_ROOT}/beta" C-m
echo "Mining 30 blocks"
tmux send-keys "./mine 30" C-m
sleep 10
echo "At Stake Enabled Height (SEH)"


################################################################################
# Setup the beta wallet (w-beta).
################################################################################
cat > "${NODES_ROOT}/w-beta/ctl" <<EOF
#!/bin/sh
dcrctl -C w-beta-ctl.conf --wallet \$*
EOF
chmod +x "${NODES_ROOT}/w-beta/ctl"

tmux new-window -t $SESSION:6 -n 'w-beta'
tmux send-keys "cd ${NODES_ROOT}/w-beta" C-m
tmux send-keys "dcrwallet -C w-beta.conf --create" C-m
echo "Creating simnet beta wallet"
sleep 1
tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
sleep 1
tmux send-keys "${BETA_WALLET_SEED}" C-m C-m
tmux send-keys "dcrwallet -C w-beta.conf --debuglevel=debug" C-m

################################################################################
# Setup the beta wallet's dcrctl (w-beta-ctl).
################################################################################
sleep 1
tmux new-window -t $SESSION:7 -n 'w-beta-ctl'
tmux send-keys "cd ${NODES_ROOT}/w-beta" C-m
tmux send-keys "./ctl getbalance"

################################################################################
# Setup the harness ctl (harness-ctl).
################################################################################
sleep 5
cat > "${NODES_ROOT}/harness-ctl/reorg" <<EOF
#!/bin/sh
echo "Disconnecting beta from alpha"
sleep 1
tmux send-keys -t $SESSION:5 "./ctl addnode 127.0.0.1:18555 remove" C-m
echo "Mining a block on alpha"
sleep 1
tmux send-keys -t $SESSION:1 "./mine 1" C-m
echo "Mining 3 blocks on beta"
tmux send-keys -t $SESSION:5 "./mine 3" C-m
sleep 2
echo "Reconnecting beta to alpha"
tmux send-keys -t $SESSION:5 "./ctl addnode 127.0.0.1:18555 add" C-m
sleep 2
grep REORG ~/$SESSION/alpha/logs/simnet/dcrd.log
EOF
chmod +x "${NODES_ROOT}/harness-ctl/reorg"

tmux new-window -t $SESSION:8 -n 'harness-ctl'
tmux send-keys "cd ${NODES_ROOT}/harness-ctl" C-m
tmux send-keys "./reorg"

echo "Mining 112 blocks"
tmux send-keys -t $SESSION:5 "./mine 112" C-m
sleep 70
echo "At Stake Validation Height (SVH)"
sleep 1
echo "Mine more blocks (TicketMaturity) for stakebase outputs"
tmux send-keys -t $SESSION:5 "./mine 16" C-m
sleep 10


tmux attach-session -t $SESSION