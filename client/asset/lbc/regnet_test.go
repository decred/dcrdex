//go:build harness

package lbc

// Regnet tests expect the LBC test harness to be running.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
)

var (
	tLotSize uint64 = 1e12
	tLBC            = &dex.Asset{
		ID:         BipID,
		Symbol:     "lbc",
		MaxFeeRate: 1e6,
		SwapConf:   1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tLBC,
		FirstWallet: &livetest.WalletName{
			Node: "alpha",
		},
		SecondWallet: &livetest.WalletName{
			Node: "beta",
		},
	})
}

func TestExternalFeeRate(t *testing.T) {
	fetchRateWithTimeout(t, dex.Mainnet)
	fetchRateWithTimeout(t, dex.Testnet)
}

func fetchRateWithTimeout(t *testing.T, net dex.Network) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	feeRate, err := externalFeeRate(ctx, net)
	if err != nil {
		t.Fatalf("error fetching %s fees: %v", net, err)
	}
	fmt.Printf("##### Fee rate fetched for %s! %d Sats/vB \n", net, feeRate)
}
