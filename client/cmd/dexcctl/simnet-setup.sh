#!/usr/bin/env bash
# Set up DCR and BTC wallets and register with the DEX.
# dcrdex, dexc, and the wallet simnet harnesses should all be running before
# calling this script.

set +e

~/dextest/ltc/harness-ctl/alpha getblockchaininfo > /dev/null
LTC_ON=$?

~/dextest/bch/harness-ctl/alpha getblockchaininfo > /dev/null
BCH_ON=$?

set -e

echo initializing
./dexcctl -p abc --simnet init

echo configuring Decred wallet
./dexcctl -p abc -p abc --simnet newwallet 42 ~/dextest/dcr/alpha/alpha.conf '{"account":"default"}'

echo configuring Bitcoin wallet
./dexcctl -p abc -p "" --simnet newwallet 0 ~/dextest/btc/alpha/alpha.conf '{"walletname":"gamma"}'

if [ $LTC_ON -eq 0 ]; then
	echo configuring Litecoin wallet
	./dexcctl -p abc -p "" --simnet newwallet 2 ~/dextest/ltc/alpha/alpha.conf '{"walletname":"gamma"}'
fi

if [ $BCH_ON -eq 0 ]; then
	echo configuring Bitcoin Cash wallet
	./dexcctl -p abc -p "" --simnet newwallet 145 ~/dextest/bch/alpha/alpha.conf '{"walletname":"gamma"}'
fi

echo registering with DEX
./dexcctl -p abc --simnet register 127.0.0.1:17273 100000000 ~/dextest/dcrdex/rpc.cert

echo mining fee confirmation blocks
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
sleep 2
tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
