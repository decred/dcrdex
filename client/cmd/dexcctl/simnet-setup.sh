#!/usr/bin/env bash
# Set up DCR and BTC wallets and register with the DEX.
# dcrdex, dexc, and the wallet simnet harnesses should all be running before
# calling this script.
#
# dexc can be built with -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' -X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'"
# in order to set simnet locktimes.

set +e

SEED="9b6aff43a73ddfaee71414100801a4ca533a9ffc9696ae7f22a7e345b6e4f4fb00f7256c46a7457b70d1eb53fa660be31e04d46d09221dd44a7fc94d6c5c41b8"

~/dextest/ltc/harness-ctl/alpha getblockchaininfo > /dev/null
LTC_ON=$?

~/dextest/bch/harness-ctl/alpha getblockchaininfo > /dev/null
BCH_ON=$?

~/dextest/eth/harness-ctl/alpha attach --exec 'eth.blockNumber' > /dev/null
ETH_ON=$?

~/dextest/doge/harness-ctl/alpha getblockchaininfo > /dev/null
DOGE_ON=$?

~/dextest/firo/harness-ctl/alpha getblockchaininfo > /dev/null
FIRO_ON=$?

~/dextest/zec/harness-ctl/alpha getblockchaininfo > /dev/null
ZEC_ON=$?

~/dextest/dgb/harness-ctl/alpha getblockchaininfo > /dev/null
DGB_ON=$?

set -e

echo initializing
./dexcctl -p abc --simnet init $SEED

echo configuring Decred wallet
./dexcctl -p abc -p abc --simnet newwallet 42 dcrwalletRPC ~/dextest/dcr/alpha/alpha.conf '{"account":"default"}'

echo configuring Bitcoin wallet
./dexcctl -p abc -p "" --simnet newwallet 0 bitcoindRPC ~/dextest/btc/alpha/alpha.conf '{"walletname":"gamma"}'

if [ $LTC_ON -eq 0 ]; then
	echo configuring Litecoin wallet
	./dexcctl -p abc -p "" --simnet newwallet 2 litecoindRPC ~/dextest/ltc/alpha/alpha.conf '{"walletname":"gamma"}'
fi

if [ $BCH_ON -eq 0 ]; then
	echo configuring Bitcoin Cash wallet
	./dexcctl -p abc -p "" --simnet newwallet 145 bitcoindRPC ~/dextest/bch/alpha/alpha.conf '{"walletname":"gamma"}'
fi

if [ $ETH_ON -eq 0 ]; then
	echo configuring Eth and dextt.eth wallets
	# Connecting to the simnet beta node over WebSocket.
	./dexcctl -p abc -p "" --simnet newwallet 60 rpc "" "{\"providers\":\"ws://localhost:38559\"}"
	./dexcctl -p abc -p "" --simnet newwallet 60000 rpc
fi

if [ $DOGE_ON -eq 0 ]; then
	echo configuring doge wallet
	./dexcctl -p abc -p "" --simnet newwallet 3 dogecoindRPC ~/dextest/doge/alpha/alpha.conf
fi

if [ $FIRO_ON -eq 0 ]; then
	echo configuring firo wallet
	./dexcctl -p abc -p "" --simnet newwallet 136 firodRPC ~/dextest/firo/alpha/alpha.conf
fi

if [ $ZEC_ON -eq 0 ]; then
	echo configuring Zcash wallet
	./dexcctl -p abc -p "" --simnet newwallet 133 zcashdRPC ~/dextest/zec/alpha/alpha.conf
fi

if [ $DGB_ON -eq 0 ]; then
	echo configuring dgb wallet
	./dexcctl -p abc -p "" --simnet newwallet 20 digibytedRPC ~/dextest/dgb/alpha/alpha.conf
fi

echo checking if we have an account already
RESTORING=$(./dexcctl -p abc --simnet discoveracct 127.0.0.1:17273 ~/dextest/dcrdex/rpc.cert)

if [ $RESTORING == "true" ]; then

  echo account exists and is paid

else

  echo registering with DEX
  ./dexcctl -p abc --simnet login
  ./dexcctl -p abc --simnet postbond 127.0.0.1:17273 1000000000 42 true ~/dextest/dcrdex/rpc.cert

  echo mining fee confirmation blocks
  tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m
  sleep 2
  tmux send-keys -t dcr-harness:0 "./mine-alpha 1" C-m

fi
