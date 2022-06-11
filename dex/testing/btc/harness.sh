#!/usr/bin/env bash
export SYMBOL="btc"
export DAEMON="bitcoind"
export CLI="bitcoin-cli"
export RPC_USER="user"
export RPC_PASS="pass"
export ALPHA_LISTEN_PORT="20575"
export BETA_LISTEN_PORT="20576"
export ALPHA_RPC_PORT="20556"
export BETA_RPC_PORT="20557"
export WALLET_PASSWORD="abc"
# For a descriptor wallet (no seed), a [wallet_name]-desc.json file is used with
# importdescriptors to set the wallet keys.
export ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
export ALPHA_MINING_ADDR="bcrt1qy7agjj62epx0ydnqskgwlcfwu52xjtpj36hr0d"
export BETA_MINING_ADDR="bcrt1q8hzxze4zrjtpm27lrlanc20uhtswteye8ad7gx"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_ADDRESS="bcrt1qt2xf2s5dkm4t34zm69w9my2dhwmnskwv4st3ly"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cURsyTZ8icuTHwWxSfTC2Geu2F6dMRtnzt1gvSaxHdc9Zf6eviJN"
export DELTA_ADDRESS="bcrt1q4clywna5re22qh9mexqty8u8mqvhjh8cwhp5ms"
export EXTRA_ARGS="--blockfilterindex --peerblockfilters --rpcbind=0.0.0.0 --rpcallowip=0.0.0.0/0"
export CREATE_DEFAULT_WALLET="1"

# The new-wallet script creates a new wallet with keys (not blank), and no
# passphrase. $1 is the node to create the wallet on, $2 is the wallet
# passphrase, and $3 (optional) is true/false for a descriptor wallet.
# For reference, possible arguments to createwallet are:
# "wallet_name" ( disable_private_keys blank(boolean) "passphrase" avoid_reuse descriptors load_on_startup external_signer )
export NEW_WALLET_CMD="./\$1 -named createwallet wallet_name=\$2 descriptors=\${3:-false} load_on_startup=true"

# Run the harness
export HARNESS_VER="1" # for outdated chain archive detection
./base-harness.sh
