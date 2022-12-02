//go:build harness

package doge

// Regnet tests expect the DOGE test harness to be running.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

var (
	tLotSize uint64 = 1e12
	tDOGE           = &dex.Asset{
		ID:           BipID,
		Symbol:       "doge",
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   1e6,
		SwapConf:     1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tDOGE,
		FirstWallet: &livetest.WalletName{
			Node: "alpha",
		},
		SecondWallet: &livetest.WalletName{
			Node: "beta",
		},
	})
}

func TestFetchExternalFee(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	rate, err := fetchExternalFee(ctx, dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("#### External fee rate fetched: %d sat/B\n", rate)
}
