#!/usr/bin/env bash
export SYMBOL="ltc"
export DAEMON="litecoind"
export CLI="litecoin-cli"
export RPC_USER="user"
export RPC_PASS="pass"
export ALPHA_LISTEN_PORT="20585"
export BETA_LISTEN_PORT="20586"
export ALPHA_RPC_PORT="20566"
export BETA_RPC_PORT="20567"
export ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
export BETA_WALLET_SEED="cRHosJjgZ2UWsEAeHYYUFa8Z6viHYXm94GguGtpzMo6qwKBC1DSq"
export ALPHA_MINING_ADDR="QTj2MW1SCfEv2wn7pf6sqiuHTdaWXViqZJ"
export BETA_MINING_ADDR="QfPMfgRPHd6mvQKDkJN8uEnR2kmBYAUE1x"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
export GAMMA_ADDRESS="QchhiwxTLuRY7dwCaS6fE2y73HqD56MRCR"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cNueSN7jzE9DEQsP8SgyonVvMSWyqk2xjTK3RPh2HAdWrR6zb8Y9"
export DELTA_ADDRESS="QYUukoqupSC86DmWZLj3miArZFcy3eGC4i"
# Signal that the node needs to restart after encrypting wallet
export RESTART_AFTER_ENCRYPT="1"
export EXTRA_ARGS="-blockfilterindex=1 -peerblockfilters=1 -rpcserialversion=2"
export CREATE_DEFAULT_WALLET="1"
export NEEDS_MWEB_PEGIN_ACTIVATION="1"
# Run the harness
../btc/base-harness.sh
