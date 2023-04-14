#!/bin/sh
export SYMBOL="bch"
# It is expected that devs rename their Bitcoin Cash binaries, since the
# original name conflicts with Bitcoin.
export DAEMON="bitcoincashd"
export CLI="bitcoincash-cli"
export RPC_USER="user"
export RPC_PASS="pass"
export ALPHA_LISTEN_PORT="21575"
export BETA_LISTEN_PORT="21576"
export ALPHA_RPC_PORT="21556"
export BETA_RPC_PORT="21557"
export ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
export BETA_WALLET_SEED="cRHosJjgZ2UWsEAeHYYUFa8Z6viHYXm94GguGtpzMo6qwKBC1DSq"
export ALPHA_MINING_ADDR="bchreg:qqnm4z2tftyyeu3kvzzepmlp9mj3g6fvxgft570vll"
export BETA_MINING_ADDR="bchreg:qzr7nnmpnreyhgt9ex3082cg8j0dks8uts9khumg0m"
export WALLET_PASSWORD="abc"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
export GAMMA_ADDRESS="bchreg:qzltvanmqgnl5gavt85c6gz2upzpu0n3lu95f4p5mv"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cURsyTZ8icuTHwWxSfTC2Geu2F6dMRtnzt1gvSaxHdc9Zf6eviJN"
export DELTA_ADDRESS="bchreg:qzhru360ks09fgzuh0ycpvslslvpj72ulqlw5j6ksy"
# $1 is the node to create with. $2 is the wallet name
export NEW_WALLET_CMD="./\$1 createwallet \$2"
export GODAEMON="go run github.com/gcash/bchd@v0.19.0"
export GOCLIENT="go run github.com/gcash/bchd/cmd/bchctl@v0.19.0"
export OMEGA_LISTEN_PORT=21577
export OMEGA_RPC_PORT=21558
# Run the harness
../btc/base-harness.sh
