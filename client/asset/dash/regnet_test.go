//go:build harness

package dash

// Regnet tests expect a Dash test harness to be running.
//
// Simnet harness info:
// ====================
//
// The harness has two nodes & four wallets. All four wallets have confirmed
// UTXOs.
// - Node alpha mines most of the coins into it's root wallet ("") then
//   distributes a wide range of those coins to the other three wallets:
//   gamma wallet, a named wallet on node alpha, beta root wallet ("") & delta
//   wallets.
//
// This harness is structured like the bitcoin harness with the root wallets
// unnamed ("")
//
//   ""
//   ├── gamma
//   │   └── wallet.dat
//   └── wallet.dat
//   ""
//   ├── delta
//   │   └── wallet.dat
//   └── wallet.dat

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/livetest"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

var (
	tLotSize uint64 = 1e6
	tDASH           = &dex.Asset{
		ID:           5,
		Symbol:       "dash",
		Version:      version,
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   20,
		SwapConf:     2,
	}
)

func TestWallet(t *testing.T) {
	livetest.Run(t, &livetest.Config{
		NewWallet: NewWallet,
		LotSize:   tLotSize,
		Asset:     tDASH,
		SplitTx:   true,
		FirstWallet: &livetest.WalletName{
			Node: "alpha",
			Name: "gamma",
		},
		SecondWallet: &livetest.WalletName{
			Node: "beta",
			Name: "delta",
		},
	})
}

// Tests mainnet externally and that testnet and simnet are not supported
func TestFetchExternalFee(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	_, err := fetchExternalFee(ctx, dex.Regtest)
	if err == nil {
		t.Fatal(errors.New("regtest should error"))
	}
	_, err = fetchExternalFee(ctx, dex.Testnet)
	if err == nil {
		t.Fatal(errors.New("testnet should error"))
	}
	var rate uint64
	rate, err = fetchExternalFee(ctx, dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("External fee rate fetched:: %d sat/B\n", rate)
}
