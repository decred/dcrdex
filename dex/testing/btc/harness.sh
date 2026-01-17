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
# For a descriptor wallet, a [wallet_name]-desc.json file is used with
# importdescriptors to set the wallet keys.
# Note: Legacy wallet seeds removed for Bitcoin Core v30+ compatibility.
# Use gen-descriptors.sh to regenerate descriptor files if needed.
export ALPHA_MINING_ADDR="bcrt1qly8tvjf0ss0l596k296uanmqe9h2u38cjrju0n"
export BETA_MINING_ADDR="bcrt1qcf5c4rtdlkmavg7spmh0h23k5egyqdqe9fg2d2"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_ADDRESS="bcrt1q2zv5qg4j2khhmz2v0hqxduyw547a39w40jvg4h"
# Delta is a named wallet in the beta wallet directory.
export DELTA_ADDRESS="bcrt1qp04ul2s506fjcq5p2wf0lkq5qln85clryxks2l"
export EXTRA_ARGS="--blockfilterindex --peerblockfilters --rpcbind=0.0.0.0 --rpcallowip=0.0.0.0/0"
export CREATE_DEFAULT_WALLET="1"

# The new-wallet script creates a new wallet with keys (not blank), and no
# passphrase. $1 is the node to create the wallet on, $2 is the wallet
# passphrase, and $3 (optional) is true/false for a descriptor wallet.
# For reference, possible arguments to createwallet are:
# "wallet_name" ( disable_private_keys blank(boolean) "passphrase" avoid_reuse descriptors load_on_startup external_signer )
export NEW_WALLET_CMD="./\$1 -named createwallet wallet_name=\$2 descriptors=\${3:-true} load_on_startup=true"

# Run the harness
export HARNESS_VER="3" # descriptor wallets with correct checksums
./base-harness.sh
