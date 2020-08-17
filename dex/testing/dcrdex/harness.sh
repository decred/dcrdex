#!/bin/sh
# Tmux script that configures and runs dcrdex.

set -e

# Rebuild dcrdex with the required simnet locktime settings.
HARNESS_DIR=$(dirname "$0")
(cd "$HARNESS_DIR" && cd ../../../server/cmd/dcrdex && go install -ldflags \
"-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' \
-X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'")

# Setup test data dir for dcrdex.
TEST_ROOT=~/dextest
DCRDEX_DATA_DIR=${TEST_ROOT}/dcrdex
rm -rf "${DCRDEX_DATA_DIR}"
mkdir -p "${DCRDEX_DATA_DIR}"
cd "${DCRDEX_DATA_DIR}"

# Drop and re-create the test db.
TEST_DB=dcrdex_simnet_test
sudo -u postgres -H psql -c "DROP DATABASE IF EXISTS ${TEST_DB}" \
-c "CREATE DATABASE ${TEST_DB} OWNER dcrdex"

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
            "maxFeeRate": 10,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/dcr/alpha/dcrd.conf"
        },
        "BTC_simnet": {
            "bip44symbol": "btc",
            "network": "simnet",
            "lotSize": 100000,
            "rateStep": 100,
            "maxFeeRate": 100,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/btc/harness-ctl/alpha.conf"
        }
    }
}
EOF

# Write dcrdex.conf. The regfeexpub comes from the alpha>server_fees account.
cat > "./dcrdex.conf" <<EOF
regfeexpub=spubVWKGn9TGzyo7M4b5xubB5UV4joZ5HBMNBmMyGvYEaoZMkSxVG4opckpmQ26E85iHg8KQxrSVTdex56biddqtXBerG9xMN8Dvb3eNQVFFwpE
pgdbname=${TEST_DB}
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
regfeeconfirms=1
signingkeypass=keypass
adminsrvon=1
adminsrvpass=adminpass
adminsrvaddr=127.0.0.1:16542
EOF

# Set the postgres user pass if provided. 
if [ -n "${PG_PASS}" ]; then
echo pgpass="${PG_PASS}" >> ./dcrdex.conf
fi

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

# DEX admin script
cat > "${DCRDEX_DATA_DIR}/dexadm" <<EOF
#!/bin/sh
if [[ "\$#" -eq "2" ]]; then
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass --header "Content-Type: text/plain" --data-binary "\$2" https://127.0.0.1:16542/api/\$1
else
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass https://127.0.0.1:16542/api/\$1
fi
EOF
chmod +x "${DCRDEX_DATA_DIR}/dexadm"

SESSION="dcrdex-harness"

# Shutdown script
cat > "${DCRDEX_DATA_DIR}/quit" <<EOF
#!/bin/sh
tmux send-keys -t $SESSION:0 C-c
tmux wait-for donedex
tmux kill-session -t $SESSION
EOF
chmod +x "${DCRDEX_DATA_DIR}/quit"

echo "Starting dcrdex"
tmux new-session -d -s $SESSION
tmux rename-window -t $SESSION:0 'dcrdex'
tmux send-keys -t $SESSION:0 "dcrdex --appdata=$(pwd) $*; tmux wait-for -S donedex" C-m
tmux attach-session -t $SESSION
