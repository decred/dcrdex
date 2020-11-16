// +build ltclive
//
// go test -v -tags ltclive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags ltclive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags ltclive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct

package ltc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset/btc"
)

var (
	ltc *btc.Backend
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

		logger := dex.StdOutLogger("LTCTEST", dex.LevelTrace)
		dexAsset, err := NewBackend("", logger, dex.Mainnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		ltc, ok = dexAsset.(*btc.Backend)
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
	btc.LiveUTXOStats(ltc, t)
}

func TestP2SHStats(t *testing.T) {
	btc.LiveP2SHStats(ltc, t)
}

func TestLiveFees(t *testing.T) {
	btc.LiveFeeRates(ltc, t, map[string]uint64{
		"d9ec52f3d2c8497638b7de47f77b1e00e91ca98f88bf844b1ae3a2a8ca44bf0b": 1000,
		"da68b225de1650d69fb57216d8403f91ea0a4b04f7be89150061748863480980": 703,
		"6e7bfce6aee69312629b1f60afe6dcef02f367207642f2dc380a554c21181eb2": 888,
		"9387d8b2097ee23cc3da36daf90262dda9a98eb25063ddddb630cf15513fa9b8": 1,
	})
}
