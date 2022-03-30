//go:build btclive

// To run tests against a node on another machine, you can create a config file
// and specify the path with the environmental variable DEX_BTC_CONFIGPATH.

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
// go test -v -tags btclive -run TestMedianFeeRates
// ------------------------------------------
// Test that the median-fee feeRate fallback calculation works.
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
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

var (
	btc *Backend
	ctx context.Context
)

func TestGetRawTransaction(t *testing.T) {
	expTxB, _ := hex.DecodeString("010000000001017d2cc6700c20cb64879eab415a0f3ba0b" +
		"d6c51bb2d95e3cc09a8f24c52d0335d0100000000ffffffff02ac2c15000000000017a" +
		"91431ab6a773ae3e4c857dea6db96ca513a96369f27879364f60100000000220020f52" +
		"e2cb266ee5919630b79207a3c946f655267eff4a1d3d829a17b5804320649050048304" +
		"502210088128c387df6642fe56275722dc2a700535eeab88ef1a5ca077815ff538b83e" +
		"4022010dc3cb58bac19613b69345fed923ff204b36d84d3c325df3ba63f5bab42198b0" +
		"147304402201bb828e28c5e91fa9af8c80451ee390006f10eded73156d4e7b71937d32" +
		"76ef102207e28c40aba395b5962d8fb1bfbcf4008888ac44b4c1b85827965ef0c69759" +
		"e4701483045022100f2c4a38556127f879fe205e7d325a445033e413dbac5fec99e858" +
		"133b1f37e2602201cd0cde48637beb036b507ee21b387103ff9d93751b6aac51ed4d02" +
		"77fff78ef018b53210288f5e79b55ab76d7c263628fad475bd4cc4cee466acced9b41f" +
		"d3a5cb71233a22102b976a06ed75f1931a303f587ccb2b9f18fe07c9d6aaceadb353ef" +
		"945579e93d52102d5c87f884d6b828e9786d49b4cee87c72e589b14e1668f4dcd40dd2" +
		"25be16a8621035eca61acb63c8738d5bccd57fb6544ca77dbc13593ce3e6754f8db563" +
		"05bf16b54ae00000000")

	txHash, err := chainhash.NewHashFromStr("79275472daabf4e79ef4dea9402841e61743e0080d4dd26a74819cfd64acadf9")
	if err != nil {
		t.Fatal(err)
	}
	txB, err := btc.node.GetRawTransaction(txHash)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(txB, expTxB) {
		t.Errorf("wrong tx bytes")
	}
}

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("BTCTEST", dex.LevelTrace)

		dexAsset, err := NewBackend(os.Getenv("DEX_BTC_CONFIGPATH"), logger, dex.Mainnet)
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

		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		wg, err := dexAsset.Connect(ctx)
		if err != nil {
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
	LiveP2SHStats(btc, t, 10000)
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

func TestMedianFeeRates(t *testing.T) {
	TestMedianFees(btc, t)
	TestMedianFeesTheHardWay(btc, t)
	// Just for comparison
	feeRate, err := btc.node.EstimateSmartFee(1, &btcjson.EstimateModeConservative)
	if err != nil {
		fmt.Printf("EstimateSmartFee unavailable: %v \n", err)
	} else {
		fmt.Printf("EstimateSmartFee: %d \n", feeRate)
	}
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
