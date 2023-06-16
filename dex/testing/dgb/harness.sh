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
# Signal that the node needs to restart after encrypting wallet
export RESTART_AFTER_ENCRYPT="1"
# $1 is the node to create with. $2 is the wallet name
export NEW_WALLET_CMD="./\$1 createwallet \$2"
export BLOCKS_TO_MINE=120 # it chokes with high diff if you mine much more, but if you stop at 100, nothing much is mature it seems
export EXTRA_ARGS="-disabledandelion=1 -rpcbind=0.0.0.0 -rpcallowip=0.0.0.0/0 -nodiscover -nodnsseed" # -onlynet=ipv4 -nodiscover

# Background watch mining by default:
# 'export NOMINER="1"' or uncomment this line to disable
#
# TODO: Cannot presently continuously mine on simnet but when we can comment
# this out.
NOMINER="1"

# Run the harness
../btc/base-harness.sh
