//go:build bchlive

// go test -v -tags bchlive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags bchlive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags bchlive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct
//
// go test -v -tags bchlive -run TestMedianFeeRates
// ------------------------------------------
// Test that a median fee rate can be calculated.

package bch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcutil"
)

var (
	bch *BCHBackend
	ctx context.Context
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("BCHTEST", dex.LevelTrace)

		// Since Bitcoin Cash's data dir is the same as Bitcoin, for dev, we'll
		// just need to all agree on using ~/.bch instead of ~/.bitcoin.
		homeDir := btcutil.AppDataDir("bch", false)
		configPath := filepath.Join(homeDir, "bitcoin.conf")

		dexAsset, err := NewBackend(configPath, logger, dex.Mainnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		bch, ok = dexAsset.(*BCHBackend)
		if !ok {
			fmt.Printf("Could not cast asset.Backend to *BCHBackend")
			return 1
		}

		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())

		wg, err := dexAsset.Connect(ctx)
		if err != nil {
			cancel()
			fmt.Printf("Connect failed: %v", err)
			return 1
		}
		defer func() {
			cancel()
			wg.Wait()
		}()

		return m.Run()
	}

	os.Exit(doIt())
}

func TestUTXOStats(t *testing.T) {
	btc.LiveUTXOStats(bch.Backend, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(bch.Backend, t, 100)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(bch.Backend, t, map[string]uint64{
		"bcf7ae875b585e00a61055372c1e99046b20f5fbfcd8659959afb6f428326bfa": 1,
		"056762c6df7ae2d80d6ebfcf7d9e3764dc4ca915fc983798ab4999fb3a30538f": 5,
		"dc3962fc4d2d7d99646cacc16a23cc49143ea9cfc43128ec986b61e9132b2726": 444,
		"0de586d0c74780605c36c0f51dcd850d1772f41a92c549e3aa36f9e78e905284": 2604,
	})
}

func TestMedianFeeRates(t *testing.T) {
	btc.TestMedianFeesTheHardWay(bch.Backend, t)
}
