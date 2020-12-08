// +build btclive
//
// Since at least one live test runs for an hour, you should run live tests
// individually using the -run flag. All of these tests will only run with the
// 'btclive' build tag, specified with the -tags flag.
//
// go test -v -tags btclive -run UTXOStats
// -----------------------------------
// Grab the most recent block and iterate it's outputs, taking account of
// how many UTXOs are found, how many are of an unknown type, etc.
//
// go test -v -tags btclive -run P2SHStats
// -----------------------------------------
// For each output in the last block, check it's previous outpoint to see if
// it's a P2SH or P2WSH. If so, takes statistics on the script types, including
// for the redeem script.
//
// go test -v -tags btclive -run BlockMonitor -timeout 61m
// ------------------------------------------
// Monitor the blockchain for a while and make sure that the block cache is
// updating appropriately.
//
// go test -v -tags btclive -run LiveFees
// ------------------------------------------
// Test that fees rates are parsed without error and that a few historical fee
// rates are correct.
//
// This last test does not pass. Leave as a code example for now.
// go test -v -tags btclive -run Plugin
// ------------------------------------------
// Import the constructor from a plugin and do some basic function checks.
// The plugin will need to be built first. To build, run
//    go build -buildmode=plugin
// from the package directory.

package btc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

var (
	btc *Backend
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

		logger := dex.StdOutLogger("BTCTEST", dex.LevelTrace)
		dexAsset, err := NewBackend("", logger, dex.Mainnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		var ok bool
		btc, ok = dexAsset.(*Backend)
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

// TestUTXOStats is routed through the exported testing utility LiveUTXOStats,
// enabling use by bitcoin clone backends during compatibility testing. See
// LiveUTXOStats in testing.go for an explanation of the test.
func TestUTXOStats(t *testing.T) {
	LiveUTXOStats(btc, t)
}

// TestP2SHStats is routed through the exported testing utility LiveP2SHStats,
// enabling use by bitcoin clone backends during compatibility testing. See
// LiveP2SHStats in testing.go for an explanation of the test.
func TestP2SHStats(t *testing.T) {
	LiveP2SHStats(btc, t)
}

// TestBlockMonitor is a live test that connects to bitcoind and listens for
// block updates, checking the state of the cache along the way. See TestReorg
// for additional testing of the block monitor loop.
func TestBlockMonitor(t *testing.T) {
	testDuration := 60 * time.Minute
	fmt.Printf("Starting BlockMonitor test. Test will last for %d minutes\n", int(testDuration.Minutes()))
	blockChan := btc.BlockChannel(5)
	expire := time.NewTimer(testDuration).C
	lastHeight := btc.blockCache.tipHeight()
out:
	for {
		select {
		case update := <-blockChan:
			if update.Err != nil {
				t.Fatalf("error encountered while monitoring blocks: %v", update.Err)
			}
			tipHeight := btc.blockCache.tipHeight()
			if update.Reorg {
				fmt.Printf("block received at height %d causes a %d block reorg\n", tipHeight, lastHeight-tipHeight+1)
			} else {
				fmt.Printf("block received for height %d\n", tipHeight)
			}
			lastHeight = tipHeight
			_, found := btc.blockCache.atHeight(tipHeight)
			if !found {
				t.Fatalf("did not find newly connected block at height %d", tipHeight)
			}
		case <-ctx.Done():
			break out
		case <-expire:
			break out
		}
	}
}

func TestLiveFees(t *testing.T) {
	LiveFeeRates(btc, t, map[string]uint64{
		"a32697f1796b7b87d953637ac827e11b84c6b0f9237cff793f329f877af50aea": 5848,
		"f3e3e209672fc057bd896c0f703f092a251fa4dca09d062a0223f760661b8187": 506,
		"a1075db55d416d3ca199f55b6084e2115b9345e16c5cf302fc80e9d5fbf5d48d": 4191,
	})
}

// This test does not pass yet.
// type backendConstructor func(context.Context, string, asset.Logger, asset.Network) (asset.Backend, error)
//
// // TestPlugin checks for a plugin file with the default name in the current
// // directory.
// // To build as a plugin: go build -buildmode=plugin
// func TestPlugin(t *testing.T) {
// 	dir, err := os.Getwd()
// 	if err != nil {
// 		t.Fatalf("error retrieving working directory: %v", err)
// 	}
// 	pluginPath := filepath.Join(dir, "btc.so")
// 	if _, err = os.Stat(pluginPath); os.IsNotExist(err) {
// 		t.Fatalf("no plugin found")
// 	}
// 	module, err := plugin.Open(pluginPath)
// 	if err != nil {
// 		t.Fatalf("error opening plugin from %s: %v", pluginPath, err)
// 	}
// 	newBackend, err := module.Lookup("NewBackend")
// 	if err != nil {
// 		t.Fatalf("error looking up symbol: %v", err)
// 	}
// 	constructor, ok := newBackend.(backendConstructor)
// 	if !ok {
// 		t.Fatalf("failed to cast imported symbol as backend constructor")
// 	}
// 	logger := slog.NewBackend(os.Stdout).Logger("PLUGIN")
// 	ctx, shutdown := context.WithCancel(context.Background())
// 	defer shutdown()
// 	dexAsset, err := constructor(ctx, SystemConfigPath("bitcoin"), logger, dex.Mainnet)
// 	if err != nil {
// 		t.Fatalf("error creating Backend from imported constructor: %v", err)
// 	}
// 	btc, ok := dexAsset.(*Backend)
// 	if !ok {
// 		t.Fatalf("failed to cast plugin Backend to *Backend")
// 	}
// 	_, err = btc.node.GetBestBlockHash()
// 	if err != nil {
// 		t.Fatalf("error retrieving best block hash: %v", err)
// 	}
// }
