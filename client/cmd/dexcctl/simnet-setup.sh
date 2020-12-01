#!/bin/sh
# Set up DCR and BTC wallets and register with the DEX.
# dcrdex, dexc, and the wallet simnet harnesses should all be running before
# calling this script.

RPC_PASS="abc"

set -e
AUTH="./dexcctl -p abc -P ${RPC_PASS}"
echo initializing
${AUTH} init
echo configuring Decred wallet
${AUTH} -p abc newwallet 42 ~/dextest/dcr/alpha/alpha.conf '{"account":"default"}'
echo configuring Bitcoin wallet
${AUTH} -p "" newwallet 0 ~/dextest/btc/alpha/alpha.conf '{"walletname":"gamma"}'
echo configuring Litecoin wallet
${AUTH} -p "" newwallet 2 ~/dextest/ltc/alpha/alpha.conf '{"walletname":"gamma"}'
echo registering with DEX
${AUTH} register 127.0.0.1:17273 100000000 ~/dextest/dcrdex/rpc.cert
echo mining fee confirmation blocks
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
sleep 2
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
