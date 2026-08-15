//go:build harness

package lbc

// Regnet tests expect the LBC test harness to be running.
//
//	cd client/asset/lbc
//	go test -v -count=1 -tags=harness -run TestWallet

import (
	"testing"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
)

var (
	tLotSize uint64 = 1e6
	tLBC            = &dex.Asset{
		ID:         BipID,
		Symbol:     "lbc",
		Version:    version,
		MaxFeeRate: 100,
		SwapConf:   1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tLBC,
		FirstWallet: &livetest.WalletName{
			Node:       "alpha",
			WalletType: walletTypeRPC,
		},
		SecondWallet: &livetest.WalletName{
			Node:       "beta",
			WalletType: walletTypeRPC,
		},
	})
}
