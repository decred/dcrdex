// +build harness

package ltc

// Regnet tests expect the LTC test harness to be running.
//
// Sim harness info:
// The harness has three wallets, alpha, beta, and gamma.
// All three wallets have confirmed UTXOs.
// The beta wallet has only coinbase outputs.
// The alpha wallet has coinbase outputs too, but has sent some to the gamma
//   wallet, so also has some change outputs.
// The gamma wallet has regular transaction outputs of varying size and
// confirmation count. Value:Confirmations =
// 10:8, 18:7, 5:6, 7:5, 1:4, 15:3, 3:2, 25:1

import (
	"testing"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

const alphaAddress = "mt9hgfXXbM3x7hewgEAovBwqoMAAnctJ4V"

var tLTC = &dex.Asset{
	ID:           2,
	Symbol:       "ltc",
	SwapSize:     dexbtc.InitTxSize,
	SwapSizeBase: dexbtc.InitTxSizeBase,
	MaxFeeRate:   10,
	LotSize:      1e6,
	RateStep:     10,
	SwapConf:     1,
}

func TestWallet(t *testing.T) {
	livetest.Run(t, NewWallet, alphaAddress, tLTC, false)
}
