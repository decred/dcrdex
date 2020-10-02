#!/bin/sh
# Set up DCR and BTC wallets and register with the DEX.
# dcrdex, dexc, and the wallet simnet harnesses should all be running before
# calling this script.
set -e
echo initializing
./dexcctl -p abc init
echo configuring Decred wallet
./dexcctl -p abc -p abc newwallet 42 ~/dextest/dcr/alpha/alpha.conf '{"account":"default"}'
echo configuring Bitcoin wallet
./dexcctl -p abc -p "" newwallet 0 ~/dextest/btc/alpha/alpha.conf '{"walletname":"gamma"}'
echo configuring Litecoin wallet
./dexcctl -p abc -p "" newwallet 2 ~/dextest/ltc/alpha/alpha.conf '{"walletname":"gamma"}'
echo registering with DEX
./dexcctl -p abc register 127.0.0.1:17273 100000000 ~/dextest/dcrdex/rpc.cert
echo mining fee confirmation blocks
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
sleep 2
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m