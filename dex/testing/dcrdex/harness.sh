#!/bin/sh
# Tmux script that configures and runs dcrdex.
set -e

# Get the absolute path for ~/dextest.
TEST_ROOT=$(cd ~/dextest; pwd)

# Setup test data dir for dcrdex.
DCRDEX_DATA_DIR=${TEST_ROOT}/dcrdex
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
pgdbname=dcrdex_simnet_test
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
regfeeconfirms=1
signingkeypass=keypass
EOF

# Write rpc.cert and rpc.key.
cat > "./rpc.cert" <<EOF
-----BEGIN CERTIFICATE-----
MIICpTCCAgagAwIBAgIQZMfxMkSi24xMr4CClCODrzAKBggqhkjOPQQDBDBJMSIw
IAYDVQQKExlkY3JkZXggYXV0b2dlbmVyYXRlZCBjZXJ0MSMwIQYDVQQDExp1YnVu
dHUtcy0xdmNwdS0yZ2ItbG9uMS0wMTAeFw0yMDA2MDgxMjM4MjNaFw0zMDA2MDcx
MjM4MjNaMEkxIjAgBgNVBAoTGWRjcmRleCBhdXRvZ2VuZXJhdGVkIGNlcnQxIzAh
BgNVBAMTGnVidW50dS1zLTF2Y3B1LTJnYi1sb24xLTAxMIGbMBAGByqGSM49AgEG
BSuBBAAjA4GGAAQApXJpVD7si8yxoITESq+xaXWtEpsCWU7X+8isRDj1cFfH53K6
/XNvn3G+Yq0L22Q8pMozGukA7KuCQAAL0xnuo10AecWBN0Zo2BLHvpwKkmAs71C+
5BITJksqFxvjwyMKbo3L/5x8S/JmAWrZoepBLfQ7HcoPqLAcg0XoIgJjOyFZgc+j
gYwwgYkwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB/wQFMAMBAf8wZgYDVR0RBF8w
XYIadWJ1bnR1LXMtMXZjcHUtMmdiLWxvbjEtMDGCCWxvY2FsaG9zdIcEfwAAAYcQ
AAAAAAAAAAAAAAAAAAAAAYcEsj5QQYcEChAABYcQ/oAAAAAAAAAYPqf//vUPXDAK
BggqhkjOPQQDBAOBjAAwgYgCQgFMEhyTXnT8phDJAnzLbYRktg7rTAbTuQRDp1PE
jf6b2Df4DkSX7JPXvVi3NeBru+mnrOkHBUMqZd0m036aC4q/ZAJCASa+olu4Isx7
8JE3XB6kGr+s48eIFPtmq1D0gOvRr3yMHrhJe3XDNqvppcHihG0qNb0gyaiX18Cv
vF8Ti1x2vTkD
-----END CERTIFICATE-----
EOF
cat > "./rpc.key" <<EOF
-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIADTDRCsp8om9OhJa+m46FZ5IhgLAno1Rp6B0i2lqESL5x9vV/upiV
TbNzCeFqEY5/Ujra9f8ZovqMlrIQmNOaZFmgBwYFK4EEACOhgYkDgYYABAClcmlU
PuyLzLGghMRKr7Fpda0SmwJZTtf7yKxEOPVwV8fncrr9c2+fcb5irQvbZDykyjMa
6QDsq4JAAAvTGe6jXQB5xYE3RmjYEse+nAqSYCzvUL7kEhMmSyoXG+PDIwpujcv/
nHxL8mYBatmh6kEt9Dsdyg+osByDRegiAmM7IVmBzw==
-----END EC PRIVATE KEY-----
EOF

echo "Starting dcrdex"
SESSION="dcrdex-harness"
tmux new-session -d -s $SESSION
tmux rename-window -t $SESSION:0 'dcrdex'
tmux send-keys -t $SESSION:0 "dcrdex --appdata=$(pwd) $@" C-m
tmux attach-session -t $SESSION
