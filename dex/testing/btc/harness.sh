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
export ALPHA_WALLET_SEED="cMndqchcXSCUQDDZQSKU2cUHbPb5UfFL9afspxsBELeE6qx6ac9n"
export BETA_WALLET_SEED="cRHosJjgZ2UWsEAeHYYUFa8Z6viHYXm94GguGtpzMo6qwKBC1DSq"
export ALPHA_MINING_ADDR="bcrt1qy7agjj62epx0ydnqskgwlcfwu52xjtpj36hr0d"
export BETA_MINING_ADDR="bcrt1qsl5u7cvc7f96zewf5te6kzpunmd5plzuxgdlfq"
export WALLET_PASSWORD="abc"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
export GAMMA_ADDRESS="bcrt1qh6m8v7czylaz8tzeaxxjqjhqgs0ruu0lu44ksy"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cURsyTZ8icuTHwWxSfTC2Geu2F6dMRtnzt1gvSaxHdc9Zf6eviJN"
export DELTA_ADDRESS="bcrt1q4clywna5re22qh9mexqty8u8mqvhjh8cwhp5ms"
export EXTRA_ARGS="--blockfilterindex --peerblockfilters --rpcbind=0.0.0.0 --rpcallowip=0.0.0.0/0"
# Run the harness
./base-harness.sh
