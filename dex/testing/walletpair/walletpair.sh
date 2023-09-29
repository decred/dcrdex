#!/usr/bin/env bash

export SHELL=$(which bash)
SESSION="walletpair"
PAIR_ROOT=~/dextest/walletpair
CLIENT_1_DIR="${PAIR_ROOT}/dexc1"
CLIENT_1_ADDR="127.0.0.4:5760"
CLIENT_2_DIR="${PAIR_ROOT}/dexc2"
CLIENT_2_ADDR="127.0.0.5:5761"
HARNESS_DIR="${PAIR_ROOT}/harness-ctl"
DEXC_DIR=$(realpath ../../../client/cmd/dexc)
DEXC="${DEXC_DIR}/dexc"

cd "${DEXC_DIR}"
go build
cd -

mkdir -p "${HARNESS_DIR}"
mkdir -p "${CLIENT_1_DIR}"
mkdir -p "${CLIENT_2_DIR}"

CLIENT_1_CONF="${CLIENT_1_DIR}/dexc.conf"
rm -f "${CLIENT_1_CONF}"
cat > "${CLIENT_1_CONF}" <<EOF
webaddr=${CLIENT_1_ADDR}
experimental=true
EOF

CLIENT_2_CONF="${CLIENT_2_DIR}/dexc.conf"
rm -f "$CLIENT_2_CONF"
cat > "$CLIENT_2_CONF" <<EOF
webaddr=${CLIENT_2_ADDR}
experimental=true
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

echo "xdg-open http://${CLIENT_1_ADDR} > /dev/null 2>&1" > "${HARNESS_DIR}/dexc1"
chmod +x "${HARNESS_DIR}/dexc1"

echo "xdg-open http://${CLIENT_2_ADDR} > /dev/null 2>&1" > "${HARNESS_DIR}/dexc2"
chmod +x "${HARNESS_DIR}/dexc2"

tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'harness-ctl'

tmux new-window -t $SESSION:1 -n 'dexc1' $SHELL
tmux send-keys -t $SESSION:1 "cd ${PAIR_ROOT}/dexc1" C-m
tmux send-keys -t $SESSION:1 "${DEXC} --appdata=${CLIENT_1_DIR} ${@}" C-m

tmux new-window -t $SESSION:2 -n 'dexc1' $SHELL
tmux send-keys -t $SESSION:2 "cd ${PAIR_ROOT}/dexc1" C-m
tmux send-keys -t $SESSION:2 "${DEXC} --appdata=${CLIENT_2_DIR} ${@}" C-m

tmux select-window -t $SESSION:0
sleep 1
tmux send-keys -t $SESSION:0 "cd ${HARNESS_DIR}; tmux wait-for -S walletpair" C-m\; wait-for walletpair
tmux send-keys -t $SESSION:0 "./dexc1; tmux wait-for -S walletpair" C-m\; wait-for walletpair
tmux send-keys -t $SESSION:0 "./dexc2" C-m
tmux attach-session -t $SESSION
