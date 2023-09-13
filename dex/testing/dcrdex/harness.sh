#!/usr/bin/env bash
# Tmux script that configures and runs dcrdex.

set -e

# Setup test data dir for dcrdex.
TEST_ROOT=~/dextest
DCRDEX_DATA_DIR=${TEST_ROOT}/dcrdex
rm -rf "${DCRDEX_DATA_DIR}"
mkdir -p "${DCRDEX_DATA_DIR}"

# Rebuild dcrdex with the required simnet locktime settings.
HARNESS_DIR=$(
  cd $(dirname "$0")
  pwd
)

# Build script
cat > "${DCRDEX_DATA_DIR}/build" <<EOF
#!/usr/bin/env bash
cd ${HARNESS_DIR}/../../../server/cmd/dcrdex/
go build -o ${DCRDEX_DATA_DIR}/dcrdex -ldflags \
    "-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' \
    -X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'"
EOF
chmod +x "${DCRDEX_DATA_DIR}/build"

cat > "${DCRDEX_DATA_DIR}/build-lock" <<EOF
#!/usr/bin/env bash
cd ${HARNESS_DIR}/../../../server/cmd/dcrdex/
go build -o ${DCRDEX_DATA_DIR}/dcrdex -ldflags \
    "-X 'decred.org/dcrdex/dex.testLockTimeTaker=\${1:-3m}' \
    -X 'decred.org/dcrdex/dex.testLockTimeMaker=\${2:-6m}'"
EOF
chmod +x "${DCRDEX_DATA_DIR}/build-lock"

"${DCRDEX_DATA_DIR}/build"

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

~/dextest/bch/harness-ctl/alpha getblockchaininfo &> /dev/null
BCH_ON=$?

~/dextest/ltc/harness-ctl/alpha getblockchaininfo &> /dev/null
LTC_ON=$?

~/dextest/doge/harness-ctl/alpha getblockchaininfo &> /dev/null
DOGE_ON=$?

~/dextest/firo/harness-ctl/alpha getblockchaininfo &> /dev/null
FIRO_ON=$?

~/dextest/zec/harness-ctl/alpha getblockchaininfo &> /dev/null
ZEC_ON=$?

~/dextest/zcl/harness-ctl/alpha getblockchaininfo &> /dev/null
ZCL_ON=$?

~/dextest/dgb/harness-ctl/alpha getblockchaininfo &> /dev/null
DGB_ON=$?

~/dextest/dash/harness-ctl/alpha getblockchaininfo &> /dev/null
DASH_ON=$?

~/dextest/eth/harness-ctl/alpha attach --exec 'eth.blockNumber' > /dev/null
ETH_ON=$?

~/dextest/polygon/harness-ctl/alpha --exec 'eth.blockNumber' > /dev/null
POLYGON_ON=$?

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
            "lotSize": 1000000000,
            "rateStep": 100,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "LTC_simnet",
            "quote": "DCR_simnet",
            "lotSize": 5000000,
            "rateStep": 100000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1000
EOF
else echo "Litecoin is not running. Configuring dcrdex markets without LTC."
fi

if [ $BCH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "BCH_simnet",
            "quote": "DCR_simnet",
            "lotSize": 100000,
            "rateStep": 1000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1200
EOF
else echo "Bitcoin Cash is not running. Configuring dcrdex markets without BCH."
fi

if [ $ETH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "ETH_simnet",
            "lotSize": 1000000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "BTC_simnet",
            "quote": "ETH_simnet",
            "lotSize": 1000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "DCR_simnet",
            "quote": "DEXTT_simnet",
            "lotSize": 100000000,
            "rateStep": 100000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
else echo "Ethereum is not running. Configuring dcrdex markets without ETH."
fi

if [ $POLYGON_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "POLYGON_simnet",
            "quote": "DCR_simnet",
            "lotSize": 100000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 2500
        },
        {
            "base": "DEXTT_POLYGON_simnet",
            "quote": "DCR_simnet",
            "lotSize": 100000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 500
EOF
else echo "Polygon is not running. Configuring dcrdex markets without Polygon."
fi

if [ $DOGE_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "DOGE_simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1500
EOF
else echo "Dogecoin is not running. Configuring dcrdex markets without DOGE."
fi

if [ $FIRO_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "FIRO_simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1500
EOF
else echo "Firo is not running. Configuring dcrdex markets without FIRO."
fi

if [ $ZEC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "ZEC_simnet",
            "quote": "BTC_simnet",
            "lotSize": 1000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1000
EOF
else echo "Zcash is not running. Configuring dcrdex markets without ZEC."
fi

if [ $ZCL_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "ZCL_simnet",
            "quote": "BTC_simnet",
            "lotSize": 1000000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2
EOF
else echo "Zclassic is not running. Configuring dcrdex markets without ZCL."
fi

if [ $DGB_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "DGB_simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1500
EOF
else echo "Digibyte is not running. Configuring dcrdex markets without DGB."
fi

if [ $DASH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
        },
        {
            "base": "DCR_simnet",
            "quote": "DASH_simnet",
            "lotSize": 1000000,
            "rateStep": 1000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1500
EOF
else echo "Dash is not running. Configuring dcrdex markets without DASH."
fi

# run with NODERELAY=1 to use a node relay for the bitcoin node.
BTC_NODERELAY_ID=""
DCR_NODERELAY_ID=""
BTC_CONFIG_PATH="${TEST_ROOT}/btc/alpha/alpha.conf"
DCR_CONFIG_PATH="${TEST_ROOT}/dcr/alpha/dcrd.conf"
if [[ -n ${NODERELAY} ]]; then
    BTC_NODERELAY_ID="btc_a21afba3"
    DCR_NODERELAY_ID="dcr_a21afba3"
    RELAY_CONF_PATH="${TEST_ROOT}/btc/alpha/alpha_noderelay.conf"
    if [ ! -f "${RELAY_CONF_PATH}" ]; then
        cp "${BTC_CONFIG_PATH}" "${RELAY_CONF_PATH}"
        echo "rpcbind=noderelay:${BTC_NODERELAY_ID}" >> "${RELAY_CONF_PATH}"
    fi
    BTC_CONFIG_PATH="${RELAY_CONF_PATH}"

    RELAY_CONF_PATH="${TEST_ROOT}/dcr/alpha/dcrd_noderelay.conf"
    if [ ! -f "${RELAY_CONF_PATH}" ]; then
        cp "${DCR_CONFIG_PATH}" "${RELAY_CONF_PATH}"
        echo "rpclisten=noderelay:${DCR_NODERELAY_ID}" >> "${RELAY_CONF_PATH}"
    fi
    DCR_CONFIG_PATH="${RELAY_CONF_PATH}"
fi

cat << EOF >> "./markets.json"
    }
    ],
    "assets": {
        "DCR_simnet": {
            "bip44symbol": "dcr",
            "network": "simnet",
            "maxFeeRate": 10,
            "swapConf": 1,
            "configPath": "${DCR_CONFIG_PATH}",
            "regConfs": 1,
            "regFee": 100000000,
            "regXPub": "spubVWKGn9TGzyo7M4b5xubB5UV4joZ5HBMNBmMyGvYEaoZMkSxVG4opckpmQ26E85iHg8KQxrSVTdex56biddqtXBerG9xMN8Dvb3eNQVFFwpE",
            "bondAmt": 50000000,
            "bondConfs": 1,
            "nodeRelayID": "${DCR_NODERELAY_ID}"
        },
        "BTC_simnet": {
            "bip44symbol": "btc",
            "network": "simnet",
            "maxFeeRate": 100,
            "swapConf": 1,
            "configPath": "${BTC_CONFIG_PATH}",
            "regConfs": 2,
            "regFee": 20000000,
            "regXPub": "vpub5SLqN2bLY4WeZJ9SmNJHsyzqVKreTXD4ZnPC22MugDNcjhKX5xNX9QiQWcE4SSRzVWyHWUihpKRT7hckDGNzVc69wSX2JPcfGeNiT5c2XZy",
            "bondAmt": 10000,
            "bondConfs": 1,
            "nodeRelayID": "${BTC_NODERELAY_ID}"
EOF

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "LTC_simnet": {
            "bip44symbol": "ltc",
            "network": "simnet",
            "maxFeeRate": 20,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/ltc/alpha/alpha.conf",
            "bondAmt": 1000000,
            "bondConfs": 1
EOF
fi

if [ $BCH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "BCH_simnet": {
            "bip44symbol": "bch",
            "network": "simnet",
            "maxFeeRate": 20,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/bch/alpha/alpha.conf",
            "bondAmt": 1000000,
            "bondConfs": 1
EOF
fi

if [ $ETH_ON -eq 0 ]; then
ETH_CONFIG_PATH=${TEST_ROOT}/eth.conf
ETH_IPC_FILE=${TEST_ROOT}/eth/alpha/node/geth.ipc

cat > $ETH_CONFIG_PATH <<EOF
ws://localhost:38559 , 2000
# comments are respected
; http://localhost:38556
${ETH_IPC_FILE},2
EOF

cat << EOF >> "./markets.json"
         },
        "ETH_simnet": {
            "bip44symbol": "eth",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2,
            "configPath": "$ETH_CONFIG_PATH"
        },
        "DEXTT_simnet": {
            "bip44symbol": "dextt.eth",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
EOF
fi # end if ETH_ON

if [ $POLYGON_ON -eq 0 ]; then
POLYGON_CONFIG_PATH=${TEST_ROOT}/polygon.conf
POLYGON_IPC_FILE=${TEST_ROOT}/polygon/alpha/bor/bor.ipc

cat > $POLYGON_CONFIG_PATH <<EOF
ws://localhost:34985 , 2000
# comments are respected
; http://localhost:48297
${POLYGONf_IPC_FILE},2
EOF

cat << EOF >> "./markets.json"
         },
        "POLYGON_simnet": {
            "bip44symbol": "polygon",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2,
            "configPath": "$POLYGON_CONFIG_PATH"
        },
        "DEXTT_POLYGON_simnet": {
            "bip44symbol": "dextt.polygon",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
EOF
fi # end if POLYGON_ON

if [ $DOGE_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "DOGE_simnet": {
            "bip44symbol": "doge",
            "network": "simnet",
            "maxFeeRate": 40000,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/doge/alpha/alpha.conf",
            "bondAmt": 2000000000,
            "bondConfs": 1
EOF
fi

if [ $FIRO_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "FIRO_simnet": {
            "bip44symbol": "firo",
            "network": "simnet",
            "maxFeeRate": 10,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/firo/alpha/alpha.conf"
EOF
fi

if [ $ZEC_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "ZEC_simnet": {
            "bip44symbol": "zec",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/zec/alpha/alpha.conf",
            "bondAmt": 40000000,
            "bondConfs": 1
EOF
fi

if [ $ZCL_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "ZCL_simnet": {
            "bip44symbol": "zcl",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/zcl/alpha/alpha.conf",
            "bondAmt": 40000000,
            "bondConfs": 1
EOF
fi

if [ $DASH_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "DASH_simnet": {
            "bip44symbol": "dash",
            "network": "simnet",
            "maxFeeRate": 10,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/dash/alpha/alpha.conf",
            "bondAmt": 10000000,
            "bondConfs": 1
EOF
fi

if [ $DGB_ON -eq 0 ]; then
    cat << EOF >> "./markets.json"
         },
        "DGB_simnet": {
            "bip44symbol": "dgb",
            "network": "simnet",
            "maxFeeRate": 2000,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/dgb/alpha/alpha.conf",
            "bondAmt": 20000000000,
            "bondConfs": 1
EOF
fi

cat << EOF >> "./markets.json"
        }
    }
}
EOF

# Write dcrdex.conf. The regfeexpub comes from the alpha>server_fees account.
cat << EOF >> ./dcrdex.conf
pgdbname=${TEST_DB}
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
loglocal=true
signingkeypass=keypass
adminsrvon=1
adminsrvpass=adminpass
adminsrvaddr=127.0.0.1:16542
bcasttimeout=1m
# freecancels=1
maxepochcancels=128
httpprof=1
noderelayaddr=127.0.0.1:17539
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
if [ -n "${NODERELAY}" ] ; then
  tmux send-keys -t $SESSION:1 C-c
  tmux wait-for donenoderelay
fi
tmux wait-for donedex
tmux kill-session -t $SESSION
EOF
chmod +x "${DCRDEX_DATA_DIR}/quit"

# Shutdown script
cat > "${DCRDEX_DATA_DIR}/run" <<EOF
#!/usr/bin/env bash
${DCRDEX_DATA_DIR}/dcrdex --appdata=$(pwd) $*; tmux wait-for -S donedex
EOF
chmod +x "${DCRDEX_DATA_DIR}/run"

echo "Starting dcrdex"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'dcrdex'

if [ -n "${NODERELAY}" ]; then
    SOURCENODE_DIR=$(realpath "${HARNESS_DIR}/../../../server/noderelay/cmd/sourcenode/")
    cd ${SOURCENODE_DIR}
    go build -o ${DCRDEX_DATA_DIR}/sourcenode
    cd "${DCRDEX_DATA_DIR}"

    RPC_PORT="20556"
    RELAYFILE="${DCRDEX_DATA_DIR}/data/simnet/noderelay/relay-files/${BTC_NODERELAY_ID}.relayfile"

    tmux new-window -t $SESSION:1 -n 'sourcenode_btc' $SHELL
    # dcrdex needs to write the relayfiles.
    tmux send-keys -t $SESSION:1 "sleep 4" C-m
    tmux send-keys -t $SESSION:1 "./sourcenode --port ${RPC_PORT} --relayfile ${RELAYFILE}; tmux wait-for -S donenoderelay" C-m

    # Decred
    RPC_PORT="19561"
    RELAYFILE="${DCRDEX_DATA_DIR}/data/simnet/noderelay/relay-files/${DCR_NODERELAY_ID}.relayfile"
    DCRD_CERT="${TEST_ROOT}/dcr/alpha/rpc.cert"

    tmux new-window -t $SESSION:2 -n 'sourcenode_dcr' $SHELL
    tmux send-keys -t $SESSION:2 "sleep 4" C-m
    tmux send-keys -t $SESSION:2 "./sourcenode --port ${RPC_PORT} --relayfile ${RELAYFILE} --localcert ${DCRD_CERT}; tmux wait-for -S donenoderelay" C-m
fi

tmux send-keys -t $SESSION:0 "${DCRDEX_DATA_DIR}/run" C-m
tmux select-window -t $SESSION:0
tmux attach-session -t $SESSION
