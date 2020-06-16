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
export ALPHA_MINING_ADDR="2MzNGEV9CBZBptm25CZ4rm2TrKF8gfVU8XA"
export BETA_MINING_ADDR="2NC2bYfZ9GX3gnDZB8CL7pYLytNKMfVxYDX"
# Gamma is a named wallet in the alpha wallet directory.
export GAMMA_WALLET_SEED="cR6gasj1RtB9Qv9j2kVej2XzQmXPmZBcn8KzUmxSSCQoz3TqTNMg"
export GAMMA_ADDRESS="2N9Lwbw6DKoNSyTB9xL4e9LXftuPP7XU214"
# Delta is a named wallet in the beta wallet directory.
export DELTA_WALLET_SEED="cNueSN7jzE9DEQsP8SgyonVvMSWyqk2xjTK3RPh2HAdWrR6zb8Y9"
export DELTA_ADDRESS="2N589dnyfoL92x31TwEh2h1jRQsB9CaQTJQ"
# Signal that the node needs to restart after encrypting wallet
export RESTART_AFTER_ENCRYPT="1"
# Run the harness
../btc/base-harness.sh