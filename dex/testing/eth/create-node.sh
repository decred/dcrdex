#!/usr/bin/env bash
# Script for creating eth nodes.
set -ex

# The following are required script arguments.
TMUX_WIN_ID=$1
NAME=$2
AUTHRPC_PORT=${3}
HTTP_PORT=${4}
WS_PORT=${5}
WS_MODULES=${6}

GROUP_DIR="${NODES_ROOT}/${NAME}"
MINE_JS="${GROUP_DIR}/mine.js"
NODE_DIR="${GROUP_DIR}/node"
mkdir -p "${NODE_DIR}"
mkdir -p "${NODE_DIR}/geth"

# Write node ctl script.
cat > "${NODES_ROOT}/harness-ctl/${NAME}" <<EOF
#!/usr/bin/env bash
geth --datadir="${NODE_DIR}" \$*
EOF
chmod +x "${NODES_ROOT}/harness-ctl/${NAME}"

# The mining script may end up mining more or less blocks than specified. It
# also sends a transaction which must be done in --dev mode.
cat > "${NODES_ROOT}/harness-ctl/mine-${NAME}" <<EOF
#!/usr/bin/env bash
  NUM=1
  case \$1 in
      ''|*[!0-9]*|[0-1])  ;;
      *) NUM=\$1 ;;
  esac
  echo "Mining..."
  for i in \$(seq 1 \$NUM)
  do
    BEFORE=\$("${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.blockNumber')
    "${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.sendTransaction({from:eth.accounts[0],to:eth.accounts[0],value:1})' > /dev/null
    while true
    do
      AFTER=\$("${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.blockNumber')
      sleep 1
      DIFF=\$((AFTER-BEFORE))
      if [ \$DIFF -gt 0 ]; then
        break
      fi
    done
    echo \$AFTER
    "${NODES_ROOT}/harness-ctl/${NAME}" attach --exec 'eth.getHeaderByNumber('\$AFTER').hash'
  done
EOF
  chmod +x "${NODES_ROOT}/harness-ctl/mine-${NAME}"

cat > "${NODE_DIR}/eth.conf" <<EOF
[Eth]
SyncMode = "snap"

[Node]
DataDir = "${NODE_DIR}"
AuthPort = ${AUTHRPC_PORT}

[Eth.Miner]
GasCeil = 30000000
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
	"--dev 2>&1 | tee ${NODE_DIR}/${NAME}.log" C-m
