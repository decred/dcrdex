#!/usr/bin/env bash
# Script for creating eth nodes.
set -e

# The following are required script arguments.
TMUX_WIN_ID=$1
NAME=$2
AUTHRPC_PORT=${3}
HTTP_PORT=${4}
WS_PORT=${5}
WS_MODULES=${6}

GROUP_DIR="${NODES_ROOT}/${NAME}"
NODE_DIR="${GROUP_DIR}/node"
mkdir -p "${NODE_DIR}"
mkdir -p "${NODE_DIR}/geth"

# Write node ctl script.
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/usr/bin/env bash
geth --datadir="${NODE_DIR}" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

cat > "${NODE_DIR}/eth.conf" <<EOF
[Node]
DataDir = "${NODE_DIR}"
AuthPort = ${AUTHRPC_PORT}
EOF

# Create a tmux window.
tmux new-window -t "$TMUX_WIN_ID" -n "${NAME}" "${SHELL}"
tmux send-keys -t "$TMUX_WIN_ID" "set +o history" C-m
tmux send-keys -t "$TMUX_WIN_ID" "cd ${NODE_DIR}" C-m

echo "Starting simnet ${NAME} node"
# Start the eth node with our custom configuration file.
tmux send-keys -t "$TMUX_WIN_ID" "${NODES_ROOT}/harness-ctl/${NAME} " \
	"--config ${NODE_DIR}/eth.conf --verbosity 5 --vmdebug --http --http.port " \
	"${HTTP_PORT} --ws --ws.port ${WS_PORT} --ws.api ${WS_MODULES} " \
	"--dev --dev.period 10 2>&1 | tee ${NODE_DIR}/${NAME}.log" C-m
