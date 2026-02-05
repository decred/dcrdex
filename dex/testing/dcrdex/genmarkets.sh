#!/usr/bin/env bash

TEST_ROOT=~/dextest
FILEPATH=${TEST_ROOT}/dcrdex/markets.json

EPOCH_DURATION=${EPOCH:-15000}
if [ "${EPOCH_DURATION}" -lt 1000 ]; then
    echo "epoch duration cannot be < 1000 ms"
    exit 1
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

~/dextest/eth/harness-ctl/alpha attach --exec 'eth.blockNumber' &> /dev/null
ETH_ON=$?

~/dextest/polygon/harness-ctl/alpha attach --exec 'eth.blockNumber' &> /dev/null
POLYGON_ON=$?

~/dextest/base/harness-ctl/alpha attach --exec 'eth.blockNumber' &> /dev/null
BASE_ON=$?

m=$(pgrep -a monerod)
XMR_ON=$?

echo "Writing markets.json and dcrdex.conf"

# Write markets.json.
# The dcr and btc harnesses should be running. The assets config paths
# used here are created by the respective harnesses.
cat > "${FILEPATH}" <<EOF
{
    "markets": [
        {
            "base": "DCR_simnet",
            "quote": "BTC_simnet",
            "lotSize": 2000000000,
            "rateStep": 100,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF

if [ $XMR_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "XMR_simnet",
            "quote": "DCR_simnet",
            "lotSize": 100000000,
            "rateStep": 100,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 5
EOF
else echo "Monero is not running. Configuring dcrdex markets without Monero."
fi

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "LTC_simnet",
            "quote": "DCR_simnet",
            "lotSize": 1000000000,
            "rateStep": 100000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1000
EOF
else echo "Litecoin is not running. Configuring dcrdex markets without LTC."
fi

if [ $BCH_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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
            "base": "ETH_simnet",
            "quote": "BTC_simnet",
            "lotSize": 100000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "BTC_simnet",
            "quote": "USDC.ETH_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "DCR_simnet",
            "quote": "USDC.ETH_simnet",
            "lotSize": 100000000,
            "rateStep": 100000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "BTC_simnet",
            "quote": "USDT.ETH_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
        },
        {
            "base": "USDC.ETH_simnet",
            "quote": "USDT.ETH_simnet",
            "lotSize": 10000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
else echo "Ethereum is not running. Configuring dcrdex markets without ETH."
fi

if [ $POLYGON_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
            "base": "DCR_simnet",
            "quote": "USDC.POLYGON_simnet",
            "lotSize": 10000000,
            "rateStep": 100,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 500
        },
        {
            "base": "DCR_simnet",
            "quote": "USDT.POLYGON_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
else echo "Polygon is not running. Configuring dcrdex markets without Polygon."
fi

if [ $BASE_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "BASE_simnet",
            "quote": "DCR_simnet",
            "lotSize": 100000000,
            "rateStep": 1000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 2500
        },
        {
            "base": "DCR_simnet",
            "quote": "USDC.BASE_simnet",
            "lotSize": 10000000,
            "rateStep": 100,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 500
        },
        {
            "base": "DCR_simnet",
            "quote": "USDT.BASE_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
else echo "Base is not running. Configuring dcrdex markets without Base."
fi

if [ $ETH_ON -eq 0 ] && [ $POLYGON_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "USDC.ETH_simnet",
            "quote": "USDC.POLYGON_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
fi

if [ $ETH_ON -eq 0 ] && [ $BASE_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "USDC.ETH_simnet",
            "quote": "USDC.BASE_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
fi

if [ $POLYGON_ON -eq 0 ] && [ $BASE_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "USDC.POLYGON_simnet",
            "quote": "USDC.BASE_simnet",
            "lotSize": 1000000,
            "rateStep": 10000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 4
EOF
fi

if [ $DOGE_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "DCR_simnet",
            "quote": "DOGE_simnet",
            "lotSize": 1000000,
            "rateStep": 1000000000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 1500
EOF
else echo "Dogecoin is not running. Configuring dcrdex markets without DOGE."
fi

if [ $FIRO_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "ZEC_simnet",
            "quote": "BTC_simnet",
            "lotSize": 100000000,
            "rateStep": 10,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 5
EOF
else echo "Zcash is not running. Configuring dcrdex markets without ZEC."
fi

if [ $ZCL_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
        },
        {
            "base": "ZCL_simnet",
            "quote": "BTC_simnet",
            "lotSize": 5000000,
            "rateStep": 100000,
            "epochDuration": ${EPOCH_DURATION},
            "marketBuyBuffer": 1.2,
            "parcelSize": 8
EOF
else echo "Zclassic is not running. Configuring dcrdex markets without ZCL."
fi

if [ $DGB_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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

cat << EOF >> "${FILEPATH}"
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
            "bondAmt": 100000,
            "bondConfs": 1,
            "nodeRelayID": "${BTC_NODERELAY_ID}"
EOF

if [ $XMR_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
         },
        "XMR_simnet": {
            "bip44symbol": "xmr",
            "network": "simnet",
            "maxFeeRate": 20,
            "swapConf": 2,
            "configPath": "${TEST_ROOT}/xmr-beta/beta/beta.conf"
EOF
fi

if [ $LTC_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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

cat > $ETH_CONFIG_PATH <<EOF
ws://localhost:38557 , 2000
EOF

cat << EOF >> "${FILEPATH}"
         },
        "ETH_simnet": {
            "bip44symbol": "eth",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2,
            "configPath": "$ETH_CONFIG_PATH"
        },
        "USDC.ETH_simnet": {
            "bip44symbol": "usdc.eth",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
        },
        "USDT.ETH_simnet": {
            "bip44symbol": "usdt.eth",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
EOF
fi # end if ETH_ON

if [ $POLYGON_ON -eq 0 ]; then
POLYGON_CONFIG_PATH=${TEST_ROOT}/polygon.conf

cat > $POLYGON_CONFIG_PATH <<EOF
ws://localhost:34983 , 2000
EOF

cat << EOF >> "${FILEPATH}"
         },
        "POLYGON_simnet": {
            "bip44symbol": "polygon",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2,
            "configPath": "$POLYGON_CONFIG_PATH"
        },
        "USDC.POLYGON_simnet": {
            "bip44symbol": "usdc.polygon",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
        },
        "USDT.POLYGON_simnet": {
            "bip44symbol": "usdt.polygon",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
EOF
fi # end if POLYGON_ON

if [ $BASE_ON -eq 0 ]; then
BASE_CONFIG_PATH=${TEST_ROOT}/base.conf

cat > $BASE_CONFIG_PATH <<EOF
ws://localhost:39557 , 2000
EOF

cat << EOF >> "${FILEPATH}"
         },
        "BASE_simnet": {
            "bip44symbol": "base",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2,
            "configPath": "$BASE_CONFIG_PATH"
        },
        "USDC.BASE_simnet": {
            "bip44symbol": "usdc.base",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
        },
        "USDT.BASE_simnet": {
            "bip44symbol": "usdt.base",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 2
EOF
fi # end if BASE_ON

if [ $DOGE_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
         },
        "ZEC_simnet": {
            "bip44symbol": "zec",
            "network": "simnet",
            "maxFeeRate": 200,
            "swapConf": 1,
            "configPath": "${TEST_ROOT}/zec/alpha/alpha.conf"
EOF
fi

if [ $ZCL_ON -eq 0 ]; then
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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
    cat << EOF >> "${FILEPATH}"
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

cat << EOF >> "${FILEPATH}"
        }
    }
}
EOF
