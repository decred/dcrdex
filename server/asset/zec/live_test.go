//go:build zeclive

// go test -v -tags zeclive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags zeclive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags zeclive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct

package zec

import (
	"context"
	"fmt"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	zec *ZECBackend
	ctx context.Context
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("ZECTEST", dex.LevelTrace)
		be, err := NewBackend("", logger, dex.Mainnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		zec, ok = be.(*ZECBackend)
		if !ok {
			fmt.Printf("Could not cast asset.Backend to *Backend")
			return 1
		}

		ctx, cancel := context.WithCancel(context.Background())
		wg, err := zec.Connect(ctx)
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
	btc.LiveUTXOStats(zec.Backend, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(zec.Backend, t, 50)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(zec.Backend, t, map[string]uint64{
		"920456117a0a9c55867e55cb02487d20a39feb4e4f6c9a69ec6f55fb243123e7": 5,
		"90c2fbb3636e5bd8d35fc17202bfe86935fad3e8244e736f9b267c8d0ad14f90": 4631,
	})
}
