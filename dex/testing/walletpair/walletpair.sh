#!/usr/bin/env bash

export SHELL=$(which bash)
SESSION="walletpair"


BW_ARGS=()
SIMNET=""
TESTNET=""
ONLY_ONE=""
ONLY_TWO=""
while [ "${1:-}" != "" ]; do
  case "$1" in
    --simnet)
      SIMNET="1"
      echo "Using simnet"
      BW_ARGS+=("$1")
      ;;
    --testnet)
      TESTNET="1"
      echo "Using testnet"
      BW_ARGS+=("$1")
      ;;
    -1)
      ONLY_ONE="1"
      echo "Only starting wallet # 1"
      ;;
    -2)
      ONLY_TWO="1"
      echo "Only starting wallet # 1"
      ;;
    *)
      BW_ARGS+=("$1")
      ;;
  esac
  shift
done

if [ "$SIMNET" ] ; then
  PAIR_ROOT=~/dextest/simnet-walletpair
  CLIENT_1_ADDR="127.0.0.6:5760"
  CLIENT_1_RPC_ADDR="127.0.0.6:5761"
  CLIENT_2_ADDR="127.0.0.7:5762"
  CLIENT_2_RPC_ADDR="127.0.0.7:5763"
else
  PAIR_ROOT=~/dextest/walletpair
  CLIENT_1_ADDR="127.0.0.4:5760"
  CLIENT_1_RPC_ADDR="127.0.0.4:5761"
  CLIENT_2_ADDR="127.0.0.5:5762"
  CLIENT_2_RPC_ADDR="127.0.0.5:5763"
fi

CLIENT_1_DIR="${PAIR_ROOT}/dexc1"
CLIENT_2_DIR="${PAIR_ROOT}/dexc2"
HARNESS_DIR="${PAIR_ROOT}/harness-ctl"
BW_DIR=$(realpath ../../../client/cmd/bisonw)
BISONW="${BW_DIR}/bisonw"

cd "${BW_DIR}"
go build
cd -

mkdir -p "${HARNESS_DIR}"
mkdir -p "${CLIENT_1_DIR}"
mkdir -p "${CLIENT_2_DIR}"

CLIENT_1_CONF="${CLIENT_1_DIR}/dexc.conf"
rm -f "${CLIENT_1_CONF}"
CLIENT_1_CTL_KEY="${CLIENT_1_DIR}/ctl.key"
rm -f "$CLIENT_1_CTL_KEY"
CLIENT_1_CTL_CERT="${CLIENT_1_DIR}/ctl.cert"
rm -f "$CLIENT_1_CTL_CERT"
cat > "${CLIENT_1_CONF}" <<EOF
webaddr=${CLIENT_1_ADDR}
rpc=1
rpckey=${CLIENT_1_CTL_KEY}
rpccert=${CLIENT_1_CTL_CERT}
rpcuser=user
rpcpass=pass
rpcaddr=${CLIENT_1_RPC_ADDR}
EOF

CLIENT_1_CTL_CONF="${CLIENT_1_DIR}/dexcctl.conf"
rm -f "$CLIENT_1_CTL_CONF"
cat > "$CLIENT_1_CTL_CONF" <<EOF
rpcuser=user
rpcpass=pass
rpccert=${CLIENT_1_CTL_CERT}
rpcaddr=${CLIENT_1_RPC_ADDR}
simnet=${SIMNET}
testnet=${TESTNET}
EOF

CLIENT_2_CONF="${CLIENT_2_DIR}/dexc.conf"
rm -f "$CLIENT_2_CONF"
CLIENT_2_CTL_KEY="${CLIENT_2_DIR}/ctl.key"
rm -f "$CLIENT_2_CTL_KEY"
CLIENT_2_CTL_CERT="${CLIENT_2_DIR}/ctlcert.conf"
rm -f "$CLIENT_2_CTL_CERT"
cat > "$CLIENT_2_CONF" <<EOF
webaddr=${CLIENT_2_ADDR}
rpc=1
rpckey=${CLIENT_2_CTL_KEY}
rpccert=${CLIENT_2_CTL_CERT}
rpcuser=user
rpcpass=pass
rpcaddr=${CLIENT_2_RPC_ADDR}
EOF

CLIENT_2_CTL_CONF="${CLIENT_2_DIR}/dexcctl.conf"
rm -f "$CLIENT_2_CTL_CONF"

cat > "$CLIENT_2_CTL_CONF" <<EOF
rpcuser=user
rpcpass=pass
rpccert=${CLIENT_2_CTL_CERT}
rpcaddr=${CLIENT_2_RPC_ADDR}
simnet=${SIMNET}
testnet=${TESTNET}
EOF

QUIT_FILE="${HARNESS_DIR}/quit"
rm -f "${QUIT_FILE}"
cat > "${QUIT_FILE}" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
sleep 1
tmux kill-session
EOF
chmod +x "${QUIT_FILE}"

echo "xdg-open http://${CLIENT_1_ADDR} > /dev/null 2>&1" > "${HARNESS_DIR}/bisonw1"
chmod +x "${HARNESS_DIR}/bisonw1"

echo "bwctl -C ${CLIENT_1_CTL_CONF} \$1" > "${HARNESS_DIR}/bw1ctl"
chmod +x "${HARNESS_DIR}/bw1ctl"

echo "xdg-open http://${CLIENT_2_ADDR} > /dev/null 2>&1" > "${HARNESS_DIR}/bisonw2"
chmod +x "${HARNESS_DIR}/bisonw2"

echo "bwctl -C ${CLIENT_2_CTL_CONF} \$1" > "${HARNESS_DIR}/bw2ctl"
chmod +x "${HARNESS_DIR}/bw2ctl"

tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'harness-ctl'

if [ -z "${ONLY_TWO}" ]; then
  tmux new-window -t $SESSION:1 -n 'bisonw1' $SHELL
  tmux send-keys -t $SESSION:1 "cd ${PAIR_ROOT}/dexc1" C-m
  tmux send-keys -t $SESSION:1 "${BISONW} --appdata=${CLIENT_1_DIR} ${BW_ARGS[@]}" C-m
fi

if [ -z "${ONLY_ONE}" ]; then
  tmux new-window -t $SESSION:2 -n 'bisonw2' $SHELL
  tmux send-keys -t $SESSION:2 "cd ${PAIR_ROOT}/dexc1" C-m
  tmux send-keys -t $SESSION:2 "${BISONW} --appdata=${CLIENT_2_DIR} ${BW_ARGS[@]}" C-m
fi

tmux select-window -t $SESSION:0
sleep 1
tmux send-keys -t $SESSION:0 "cd ${HARNESS_DIR}; tmux wait-for -S walletpair" C-m\; wait-for walletpair

if [ -z "${ONLY_TWO}" ]; then
  tmux send-keys -t $SESSION:0 "./bisonw1; tmux wait-for -S walletpair" C-m\; wait-for walletpair
fi

if [ -z "${ONLY_ONE}" ]; then
  tmux send-keys -t $SESSION:0 "./bisonw2" C-m
fi

tmux attach-session -t $SESSION
