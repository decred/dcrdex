//go:build harness

package ltc

// Regnet tests expect the LTC test harness to be running. The harness miner
// must be OFF.
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
)

// const alphaAddress = "rltc1qjld4f85m96rr77035c5yuhkz8apxlkkla0ftmz"

var (
	tLotSize uint64 = 1e6
	tLTC            = &dex.Asset{
		ID:         2,
		Symbol:     "ltc",
		Version:    version,
		MaxFeeRate: 10,
		SwapConf:   1,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tLTC,
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
