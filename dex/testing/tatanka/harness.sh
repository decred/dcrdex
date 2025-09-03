#!/usr/bin/env bash
# Tmux script that sets up a simnet harness.
set -ex
SESSION="tatanka-harness"
ROOT=~/dextest/tatanka
if [ -d "${ROOT}" ]; then
  rm -fR "${ROOT}"
fi
mkdir -p "${ROOT}"

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
    },
    { "symbol": "btc", "config": {} },
    { "symbol": "bch" , "config": {}},
    { "symbol": "firo", "config": {} },
    { "symbol": "ltc", "config": {} },
    { "symbol": "zec", "config": {} },
    { "symbol": "eth", "config": {} },
    { "symbol": "polygon" }
  ]
}
EOF

echo "Starting harness"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'tatanka'
tmux send-keys -t $SESSION:0 "cd ${ROOT}" C-m
tmux send-keys -t $SESSION:0 "tatanka --appdata=${ROOT}" C-m
tmux attach-session -t $SESSION
