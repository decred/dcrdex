#!/usr/bin/env bash
export SYMBOL="dgb"
export DAEMON="digibyted"
export CLI="digibyte-cli"
export RPC_USER="user"
export RPC_PASS="pass"
export ALPHA_LISTEN_PORT="20785"
export BETA_LISTEN_PORT="20786"
export ALPHA_RPC_PORT="20766"
export BETA_RPC_PORT="20767"
export ALPHA_WALLET_SEED="edGAHPVc4cRuwjrBbayutFB36X8hFLFSJxZXoQf2kd3LaUSFLBgH"
export BETA_WALLET_SEED="edvbnynKLpXwBvs8xNM1HiffweUS5FHSLYkTXYnUKShQSWJb2CV2" # dumpwallet and get hdseed
export ALPHA_MINING_ADDR="dgbrt1qk96nh49y26ls0czacapwa8p5s5wurkm0y7wd97"
export BETA_MINING_ADDR="dgbrt1q04mpmzm569rc3d4vs7uqjjj59jwkq7gv9drdsz"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="einJtvSQJxNGPQWKAwmRWEZ95ifc9C5uqJZZejAKoJvpanEg3azV"
export GAMMA_ADDRESS="dgbrt1q46scevzm9hu06y0pnl8v2fxmll9v8k3e3lnd43"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="ehA5EC6mbNvGCJ4HqtjNc822ojnU2bGRLoc3cZipoYFFYT7tYrq4"
export DELTA_ADDRESS="dgbrt1qdgzj8guegzuyfupcvy3vjlpfxpv8rgruqtkndf"
# Creates named wallets as legacy (BDB) format. DigiByte v8.26.0 defaults to
# descriptor wallets, but sethdseed only works with legacy wallets.
# $1 is the node to create with. $2 is the wallet name
export NEW_WALLET_CMD="./\$1 -named createwallet wallet_name=\$2 descriptors=false"
export BLOCKS_TO_MINE=120 # it chokes with high diff if you mine much more, but if you stop at 100, nothing much is mature it seems
# DigiByte v8.22+ (based on Bitcoin Core v0.21+) does not create default wallet automatically
export CREATE_DEFAULT_WALLET="1"
# -deprecatedrpc=create_bdb: Re-enable BDB wallet creation (deprecated in v8.26.0)
# Removed -disabledandelion=1 (invalid in v8.22+, correct syntax is -dandelion=0)
export EXTRA_ARGS="-deprecatedrpc=create_bdb -rpcbind=0.0.0.0 -rpcallowip=0.0.0.0/0 -nodiscover -nodnsseed"

# Background watch mining by default:
# Set 'export NOMINER="1"' to disable the miner window.
# Must use 'export' keyword to pass variable to base-harness.sh child process.
#
# TODO: Cannot presently continuously mine on simnet but when we can comment
# this out.
export NOMINER="1"

# Run the harness
../btc/base-harness.sh
