//go:build harness

package firo

// Regnet tests expect the Firo test harness to be running.
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
	"context"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

var (
	tLotSize uint64 = 1e6
	tFIRO           = &dex.Asset{
		ID:           136,
		Symbol:       "firo",
		Version:      version,
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   20,
		SwapConf:     1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tFIRO,
		FirstWallet: &livetest.WalletName{
			Node: "alpha",
			Name: "alpha",
		},
		SecondWallet: &livetest.WalletName{
			Node: "beta",
			Name: "beta",
		},
	})
}

// Tests only mainnet, testnet expected to return -1 normally
func TestFetchExternalFee(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	rate, err := fetchExternalFee(ctx, dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("External fee rate fetched:: %d sat/B\n", rate)
}
