//go:build harness

package firo

// Regnet tests expect the Firo test harness to be running.
//
// Sim harness info:
// The harness has four nodes: alpha, beta, gamma and delta with one wallet
// per node. All wallets have confirmed UTXOs. The alpha wallet has only
// coinbase outputs.

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

// Run harness with NOMINER="1"
func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tFIRO,
		SplitTx:   true,
		FirstWallet: &livetest.WalletName{
			WalletType: walletTypeRPC,
			Node:       "alpha",
		},
		SecondWallet: &livetest.WalletName{
			WalletType: walletTypeRPC,
			Node:       "gamma",
		},
	})
}

// Tests only mainnet, testnet is expected to return -1 normally
func TestFetchExternalFee(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	rate, err := externalFeeRate(ctx, dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("External fee rate fetched:: %d sat/B\n", rate)
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
	fmt.Printf("##### Fee rate fetched for %s! %d Sats/B \n", net, feeRate)
}
