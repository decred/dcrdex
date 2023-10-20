//go:build zcllive

// go test -v -tags zcllive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags zcllive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags zcllive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct

package zcl

import (
	"context"
	"fmt"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	zcl *ZECBackend
	ctx context.Context
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("ZECTEST", dex.LevelTrace)
		be, err := NewBackend(&asset.BackendConfig{
			AssetID: BipID,
			Logger:  logger,
			Net:     dex.Mainnet,
		})
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		zcl, ok = be.(*ZECBackend)
		if !ok {
			fmt.Printf("Could not cast asset.Backend to *ZECBackend")
			return 1
		}

		ctx, cancel := context.WithCancel(context.Background())
		wg, err := zcl.Connect(ctx)
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
	btc.LiveUTXOStats(zcl.Backend, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(zcl.Backend, t, 50)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(zcl.Backend, t, map[string]uint64{
		"597c3975f2445da89a9097a2d9131e5d76c476d248d6a1c277d93b1299c63b68": 1,
		// "90c2fbb3636e5bd8d35fc17202bfe86935fad3e8244e736f9b267c8d0ad14f90": 4631,
	})
}
