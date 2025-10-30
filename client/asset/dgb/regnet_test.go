//go:build harness

package dgb

// Regnet tests expect the DGB test harness to be running. The harness miner
// must be OFF.
//
// NOTE: After mining blocks, DigiByte node must do 2 things so tests will not
// pass until the harness is done with:
//  1. Update Wallet DB (fast): Write transaction to wallet database
//  2. Update UTXO Index (slow): Rebuild index of all unspent outputs
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
)

var (
	tLotSize uint64 = 1e6
	tDGB            = &dex.Asset{
		ID:         20,
		Symbol:     "dgb",
		Version:    version,
		MaxFeeRate: 100,
		SwapConf:   1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tDGB,
	})
}
