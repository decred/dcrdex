//go:build harness

package zec

// Regnet tests expect the ZEC test harness to be running.

import (
	"testing"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	dexzec "decred.org/dcrdex/dex/networks/zec"
)

var (
	tLotSize uint64 = 1e6
	tZEC            = &dex.Asset{
		ID:           BipID,
		Symbol:       "zec",
		SwapSize:     dexzec.InitTxSize,
		SwapSizeBase: dexzec.InitTxSizeBase,
		MaxFeeRate:   100,
		SwapConf:     1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tZEC,
		FirstWallet: &livetest.WalletName{
			Node:     "alpha",
			Filename: "zcash.conf",
		},
		SecondWallet: &livetest.WalletName{
			Node:     "beta",
			Filename: "zcash.conf",
		},
		Unencrypted: true,
	})
}
