#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
SESSION="tatanka-harness"
ROOT=~/dextest/tatanka
if [ -d "${ROOT}" ]; then
  rm -fR "${ROOT}"
fi
mkdir -p "${ROOT}"

HARNESS_DIR=$(
  cd $(dirname "$0")
  pwd
)

CMD_DIR=$(realpath "${HARNESS_DIR}/../../../tatanka/cmd/tatanka")

# Build script
cat > "${ROOT}/build" <<EOF
#!/usr/bin/env bash
cd ${CMD_DIR}
go build -o ${ROOT}/tatanka
EOF
chmod +x "${ROOT}/build"

"${ROOT}/build"

cat > "${HARNESS_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:1 C-c
tmux wait-for alpha_tatanka
# seppuku
tmux kill-session
EOF
chmod +x "${HARNESS_DIR}/quit"

cp "priv.key" "${ROOT}"

cat > "${ROOT}/tatanka.conf" <<EOF
simnet=1
notls=1
EOF

DCR_CERT=~/dextest/dcr/alpha/rpc.cert
cat > "${ROOT}/chains.json" <<EOF
{
  "chains": [
    {
      "symbol": "dcr",
      "config": {
        "rpcuser": "user",
        "rpcpass": "pass",
        "rpclisten": "127.0.0.1:19561",
        "rpccert": "${DCR_CERT}"
      }
    }
  ]
}
EOF

echo "Starting harness"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'harness'
tmux new-window -t $SESSION:1 -n 'alpha' $SHELL
tmux send-keys -t $SESSION:1 "cd ${ROOT}" C-m
tmux send-keys -t $SESSION:1 "./tatanka --appdata=${ROOT}; tmux wait-for -S alpha_tatanka" C-m
tmux select-window -t $SESSION:0
tmux attach-session -t $SESSION
