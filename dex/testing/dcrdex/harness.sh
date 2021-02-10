#!/usr/bin/env bash
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

EPOCH_DURATION=${EPOCH:-15000} 
if [ "${EPOCH_DURATION}" -lt 1000 ]; then
    echo "epoch duration cannot be < 1000 ms"
    exit 1
fi

echo "Writing markets.json and dcrdex.conf"

set +e

~/dextest/bch/harness-ctl/alpha getblockchaininfo > /dev/null
BCH_ON=$?

~/dextest/ltc/harness-ctl/alpha getblockchaininfo > /dev/null
LTC_ON=$?

set -e

# Write markets.json.
# The dcr and btc harnesses should be running. The assets config paths
# used here are created by the respective harnesses.
cat > "./markets.json" <<EOF
{
    "markets": [
        {
            "base": "DCR_simnet",
            "quote": "BTC_simnet",
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2
EOF

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "LTC_simnet",
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2
EOF
fi

if [ $BCH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "BCH_simnet",
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2
EOF
fi

cat << EOF >> "./markets.json"
    }
    ],
    "assets": {
        "DCR_simnet": {
            "bip44symbol": "dcr",
            "network": "simnet",
            "lotSize": 1000000000,
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
            "configPath": "${TEST_ROOT}/btc/alpha/alpha.conf"
EOF

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "LTC_simnet": {
            "bip44symbol": "ltc",
            "network": "simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "maxFeeRate": 20,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/ltc/alpha/alpha.conf"
EOF
fi

if [ $BCH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "BCH_simnet": {
            "bip44symbol": "bch",
            "network": "simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "maxFeeRate": 20,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/bch/alpha/alpha.conf"
EOF
fi

cat << EOF >> "./markets.json"
        }
    }
}
EOF

# Write dcrdex.conf. The regfeexpub comes from the alpha>server_fees account.
cat << EOF >> ./dcrdex.conf
; alpha wallet server_fees account xpub
regfeexpub=spubVWKGn9TGzyo7M4b5xubB5UV4joZ5HBMNBmMyGvYEaoZMkSxVG4opckpmQ26E85iHg8KQxrSVTdex56biddqtXBerG9xMN8Dvb3eNQVFFwpE
; beta wallet server_fees account xpub
regfeexpubnew=spubVW3qcqEFFCudsQCCm2PDReXS7ZcQfV7vWYgcXk1vzVVELPFeixrhFW5HFfCZdoGEbih6nnjoYLCUCgm63eDcbKqkAtU4z655tHqy4r9YrFa
pgdbname=${TEST_DB}
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
loglocal=true
regfeeconfirms=1
signingkeypass=keypass
adminsrvon=1
adminsrvpass=adminpass
adminsrvaddr=127.0.0.1:16542
bcasttimeout=1m
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
#!/usr/bin/env bash
if [[ "\$#" -eq "2" ]]; then
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass --header "Content-Type: text/plain" --data-binary "\$2" https://127.0.0.1:16542/api/\$1
else
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass https://127.0.0.1:16542/api/\$1
fi
EOF
chmod +x "${DCRDEX_DATA_DIR}/dexadm"

SESSION="dcrdex-harness"

export SHELL=$(which bash)

# Shutdown script
cat > "${DCRDEX_DATA_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:0 C-c
tmux wait-for donedex
tmux kill-session -t $SESSION
EOF
chmod +x "${DCRDEX_DATA_DIR}/quit"

echo "Starting dcrdex"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'dcrdex'
tmux send-keys -t $SESSION:0 "dcrdex --appdata=$(pwd) $*; tmux wait-for -S donedex" C-m
tmux attach-session -t $SESSION
