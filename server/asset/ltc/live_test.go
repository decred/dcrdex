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
	ltc *btc.BTCBackend
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
	ltc, ok = dexAsset.(*btc.BTCBackend)
	if !ok {
		fmt.Printf("Could not cast DEXAsset to *BTCBackend")
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
