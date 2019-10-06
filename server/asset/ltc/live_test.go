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
	"testing"

	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/dcrdex/server/asset/btc"
	"github.com/decred/slog"
)

var (
	ltc *btc.Backend
)

func TestMain(m *testing.M) {
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	var err error
	dexAsset, err := NewBackend(ctx, "", logger, asset.Mainnet)
	if err != nil {
		fmt.Printf("NewBackend error: %v\n", err)
		return
	}
	var ok bool
	ltc, ok = dexAsset.(*btc.Backend)
	if !ok {
		fmt.Printf("Could not cast DEXAsset to *Backend")
		return
	}
	os.Exit(m.Run())
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
	})
}
