#!/bin/sh
# Tmux script that configures and runs dcrdex.
set -e

# Get the absolute path for ~/dextest.
TEST_ROOT=$(cd ~/dextest; pwd)

# Setup test data dir for dcrdex.
DCRDEX_DATA_DIR=~/dextest/dcrdex
if [ -d "${DCRDEX_DATA_DIR}" ]; then
  rm -R "${DCRDEX_DATA_DIR}"
fi
mkdir -p "${DCRDEX_DATA_DIR}"
cd "${DCRDEX_DATA_DIR}"

echo "Writing markets.json and dcrdex.conf"

# Write markets.json.
# The dcr and btc harnesses should be running. The assets config paths
# used here are created by the respective harnesses.
cat > "./markets.json" <<EOF
{
    "markets": [
        {
            "base": "DCR_simnet",
            "quote": "BTC_simnet",
            "epochDuration": 6000,
            "marketBuyBuffer": 1.2
        }
    ],
    "assets": {
        "DCR_simnet": {
            "bip44symbol": "dcr",
            "network": "simnet",
            "lotSize": 100000000,
            "rateStep": 100000,
            "feeRate": 10,
            "swapConf": 1,
            "fundConf": 1,
            "configPath": "${TEST_ROOT}/dcr/alpha/dcrd.conf"
        },
        "BTC_simnet": {
            "bip44symbol": "btc",
            "network": "simnet",
            "lotSize": 100000,
            "rateStep": 100,
            "feeRate": 100,
            "swapConf": 1,
            "fundConf": 1,
            "configPath": "${TEST_ROOT}/btc/harness-ctl/alpha.conf"
        }
    }
}
EOF

# Write dcrdex.conf.
cat > "./dcrdex.conf" <<EOF
regfeexpub=spubVWHTkHRefqHptAnBdNcDJMnT9w7wBPGtyv5Ji6hHsHGGXyLhgq21SakpXmjEAAQFjcwm14bgXGa23ETaskUTxgm4cqi2qbKLkaY1YdCHmtz
pgdbname=dcrdex_simnet
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
regfeeconfirms=1
signingkeypass=keypass
EOF

echo "Starting dcrdex"
SESSION="dcrdex-harness"
tmux new-session -d -s $SESSION
tmux rename-window -t $SESSION:0 'dcrdex'
tmux send-keys -t $SESSION:0 "dcrdex --appdata=$(pwd) $@" C-m
tmux attach-session -t $SESSION
