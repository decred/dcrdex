//go:build dogelive
// +build dogelive

// go test -v -tags dogelive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags dogelive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags dogelive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct

package doge

import (
	"context"
	"fmt"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	doge *btc.Backend
	ctx  context.Context
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("DOGETEST", dex.LevelTrace)
		dexAsset, err := NewBackend("", logger, dex.Mainnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		doge, ok = dexAsset.(*btc.Backend)
		if !ok {
			fmt.Printf("Could not cast asset.Backend to *Backend")
			return 1
		}

		ctx, cancel := context.WithCancel(context.Background())
		wg, err := dexAsset.Connect(ctx)
		if err != nil {
			fmt.Printf("Connect failed: %v", err)
			return 1
		}
		defer wg.Wait()
		defer cancel()

		return m.Run()
	}

	os.Exit(doIt())
}

func TestUTXOStats(t *testing.T) {
	btc.LiveUTXOStats(doge, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(doge, t, 50)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(doge, t, map[string]uint64{
		"f5ebcf31851ba99d633cf05b4ef3793b67aeb790ef7ea084abb6af503ca7c070": 1005,
		"58f8771cfa4fa966dc639066eb82ce643e4c0aabc48f242b6abe2e9b83c35117": 444_444,
		// "6e7bfce6aee69312629b1f60afe6dcef02f367207642f2dc380a554c21181eb2": 888,
		// "9387d8b2097ee23cc3da36daf90262dda9a98eb25063ddddb630cf15513fa9b8": 1,
	})
}
