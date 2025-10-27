//go:build lbclive

// go test -v -tags lbclive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags lbclive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags lbclive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct
//
// go test -v -tags lbclive -run TestMedianFeeRates
// ------------------------------------------
// Test that a median fee rate can be calculated.

package lbc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	lbc *btc.Backend
	ctx context.Context
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		wg := new(sync.WaitGroup)
		defer func() {
			cancel()
			wg.Wait()
		}()

		logger := dex.StdOutLogger("LBCTEST", dex.LevelTrace)
		dexAsset, err := NewBackend(&asset.BackendConfig{
			AssetID: BipID,
			Logger:  logger,
			Net:     dex.Mainnet,
		})
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		lbc, ok = dexAsset.(*btc.Backend)
		if !ok {
			fmt.Printf("Could not cast asset.Backend to *Backend")
			return 1
		}

		wg, err = dexAsset.Connect(ctx)
		if err != nil {
			fmt.Printf("Connect failed: %v", err)
			return 1
		}

		return m.Run()
	}

	os.Exit(doIt())
}

func TestUTXOStats(t *testing.T) {
	btc.LiveUTXOStats(lbc, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(lbc, t, 1000)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(lbc, t, map[string]uint64{
		"d9ec52f3d2c8497638b7de47f77b1e00e91ca98f88bf844b1ae3a2a8ca44bf0b": 1000,
		"da68b225de1650d69fb57216d8403f91ea0a4b04f7be89150061748863480980": 703,
		"6e7bfce6aee69312629b1f60afe6dcef02f367207642f2dc380a554c21181eb2": 888,
		"9387d8b2097ee23cc3da36daf90262dda9a98eb25063ddddb630cf15513fa9b8": 1,
	})
}

func TestMedianFeeRates(t *testing.T) {
	btc.TestMedianFees(lbc, t)
}
