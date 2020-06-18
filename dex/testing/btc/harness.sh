#!/bin/sh
export SYMBOL="btc"
export DAEMON="bitcoind"
export CLI="bitcoin-cli"
export RPC_USER="user"
export RPC_PASS="pass"
export ALPHA_LISTEN_PORT="20575"
export BETA_LISTEN_PORT="20576"
export ALPHA_RPC_PORT="20556"
export BETA_RPC_PORT="20557"
export ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
export BETA_WALLET_SEED="cRHosJjgZ2UWsEAeHYYUFa8Z6viHYXm94GguGtpzMo6qwKBC1DSq"
export ALPHA_MINING_ADDR="2MzNGEV9CBZBptm25CZ4rm2TrKF8gfVU8XA"
export BETA_MINING_ADDR="2NC2bYfZ9GX3gnDZB8CL7pYLytNKMfVxYDX"
export WALLET_PASSWORD="abc"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
export GAMMA_ADDRESS="2N9Lwbw6DKoNSyTB9xL4e9LXftuPP7XU214"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cURsyTZ8icuTHwWxSfTC2Geu2F6dMRtnzt1gvSaxHdc9Zf6eviJN"
export DELTA_ADDRESS="2NBeBjSW2W5yhRK23F7rZMtqKiDsLCKmifa"
# Run the harness
./base-harness.sh
