// +build dcrlive
//
// Since at least one live test runs for an hour, you should run live tests
// individually using the -run flag. All of these tests will only run with the
// 'dcrlive' build tag, specified with the -tags flag.
//
// go test -v -tags dcrlive -run LiveUTXO
// -----------------------------------
// This command will check the UTXO paths by iterating backwards through
// the transactions in the mainchain, starting with mempool, and requesting
// all found outputs through the Backend.utxo method. All utxos must be
// found or found to be spent.
//
// go test -v -tags dcrlive -run CacheAdvantage
// -----------------------------------------
// Check the difference between using the block cache and requesting via RPC.
//
// go test -v -tags dcrlive -run BlockMonitor -timeout 61m
// ------------------------------------------
// Monitor the block chain for a while and make sure that the block cache is
// updating appropriately.

package dcr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
)

var (
	dcr        *Backend
	ctx        context.Context
	testLogger dex.Logger
)

func TestMain(m *testing.M) {
	// Wrap everything for defers.
	doIt := func() int {
		logger := dex.StdOutLogger("DCRTEST", dex.LevelTrace)

		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		wg := new(sync.WaitGroup)
		defer func() {
			logger.Infof("Shutting down...")
			cancel()
			wg.Wait()
			logger.Infof("done.")
		}()

		var err error
		dcr, err = NewBackend("", logger, dex.Testnet)
		if err != nil {
			fmt.Printf("NewBackend error: %v\n", err)
			return 1
		}

		wg, err = dcr.Connect(ctx)
		if err != nil {
			fmt.Printf("Connect failed: %v", err)
			return 1
		}

		return m.Run()
	}

	os.Exit(doIt())
}

// TestLiveUTXO will iterate the blockchain backwards, starting with mempool,
// checking that UTXOs are behaving as expected along the way. Stats will be
// collected on the types of scripts found.
func TestLiveUTXO(t *testing.T) {
	var bestHash *chainhash.Hash
	var mempool []*wire.MsgTx
	var txs []*wire.MsgTx
	type testStats struct {
		p2pkh          int
		sp2pkh         int
		p2pkhSchnorr   int
		p2pkhEdwards   int
		p2sh           int
		sp2sh          int
		immatureBefore int
		immatureAfter  int
		utxoVal        uint64
		feeRates       []uint64
	}
	stats := new(testStats)
	var currentHeight, tipHeight int64
	maturity := uint32(chainParams.CoinbaseMaturity)
	numBlocks := 512

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Check if the best hash is still bestHash.
	blockChanged := func() bool {
		h, _, _ := dcr.client.GetBestBlock(ctx)
		return *bestHash != *h
	}

	// Get the MsgTxs for the list of hashes.
	getMsgTxs := func(hashes []*chainhash.Hash) []*wire.MsgTx {
		outTxs := make([]*wire.MsgTx, 0)
		for _, h := range hashes {
			newTx, err := dcr.client.GetRawTransaction(ctx, h)
			if err != nil {
				t.Fatalf("error retrieving MsgTx: %v", err)
				outTxs = append(outTxs, newTx.MsgTx())
			}
		}
		return outTxs
	}

	// A function to look through the current known transactions to check if an
	// output is spent.
	txOutInList := func(vout uint32, hash chainhash.Hash, txList []*wire.MsgTx) bool {
		for _, t := range txList {
			for _, in := range t.TxIn {
				if in.PreviousOutPoint.Hash == hash && in.PreviousOutPoint.Index == vout {
					return true
				}
			}
		}
		return false
	}

	// Check if the transaction is spent in a known block.
	txOutIsSpent := func(hash chainhash.Hash, vout uint32) bool {
		return txOutInList(vout, hash, txs) || txOutInList(vout, hash, mempool)
	}

	refreshMempool := func() {
		mpHashes, err := dcr.client.GetRawMempool(ctx, "all")
		if err != nil {
			t.Fatalf("getrawmempool error: %v", err)
		}
		mempool = getMsgTxs(mpHashes)
	}

	// checkTransactions checks that the UTXO for all outputs is what is expected.
	checkTransactions := func(expectedConfs uint32, txSet []*wire.MsgTx) error {
		txs = append(txs, txSet...)
		for _, msgTx := range txSet {
			txHash := msgTx.CachedTxHash()
			fee := false
			for vout, out := range msgTx.TxOut {
				if out.Value == 0 {
					continue
				}
				if out.Version != dexdcr.CurrentScriptVersion {
					continue
				}
				scriptType := dexdcr.ParseScriptType(dexdcr.CurrentScriptVersion, out.PkScript, nil)
				// We can't do P2SH during live testing, because we don't have the
				// scripts. Just count them for now.
				if scriptType.IsP2SH() {
					switch {
					case scriptType.IsStake():
						stats.sp2sh++
					default:
						stats.p2sh++
					}
					continue
				} else if scriptType&dexdcr.ScriptP2PKH != 0 {
					switch {
					case scriptType&dexdcr.ScriptSigEdwards != 0:
						stats.p2pkhEdwards++
					case scriptType&dexdcr.ScriptSigSchnorr != 0:
						stats.p2pkhSchnorr++
					case scriptType.IsStake():
						stats.sp2pkh++
					default:
						stats.p2pkh++
					}
				}
				// Check if its an acceptable script type.
				scriptTypeOK := scriptType != dexdcr.ScriptUnsupported
				// Now try to get the UTXO with the Backend
				utxo, err := dcr.utxo(ctx, txHash, uint32(vout), nil)
				// Can't do stakebase or coinbase.
				// ToDo: Use a custom error and check it.
				if err == immatureTransactionError {
					// just count these for now.
					confs := tipHeight - currentHeight + 1
					if confs < int64(maturity) {
						stats.immatureBefore++
					} else {
						stats.immatureAfter++
					}
					continue
				}
				// There are 4 possibilities
				// 1. script is of acceptable type and utxo was found, in which case there
				//    should be zero confirmations (unless a new block snuck in).
				// 2. script is of acceptable type and utxo was not found. Error
				//    unless output is spent in mempool.
				// 3. script is of unacceptable type, and is found. Error.
				// 4. script is of unacceptable type, and is not found. Should receive an
				//    dex.UnsupportedScriptError.
				switch true {
				case scriptTypeOK && err == nil:
					if !fee {
						fee = true
						stats.feeRates = append(stats.feeRates, utxo.FeeRate())
					}
					// Just check for no error on Confirmations.
					confs, err := utxo.Confirmations(ctx)
					if err != nil {
						return fmt.Errorf("error getting confirmations for mempool tx output: %w", err)
					}
					if confs != int64(expectedConfs) {
						return fmt.Errorf("expected %d confirmations, found %d for %s:%d", expectedConfs, confs, txHash, vout)
					}
					stats.utxoVal += utxo.Value()
					break
				case scriptTypeOK && err != nil:
					// This is only okay if output is being spent by another transaction.
					// Since we are iterating backwards starting with mempool, we would
					// already know the spending transaction and have it stored.
					if !txOutIsSpent(*txHash, uint32(vout)) {
						return fmt.Errorf("unexpected UTXO error: %w", err)
					}
					break
				case !scriptTypeOK && err == nil:
					return fmt.Errorf("received UTXO for unacceptable script type")
				default: // !scriptTypeOK && err != nil
					// this is normal. Do nothing.
				} // end switch
			} // end tx check
		} // end tx loop
		return nil
	}

	// Wraps checkTransactions with some additional checks.
	scanUtxos := func(confs uint32, txs []*wire.MsgTx) bool {
		if err := checkTransactions(confs, txs); err != nil {
			if blockChanged() {
				return true
			}
			// Try with fresh mempool before failing.
			refreshMempool()
			if err = checkTransactions(confs, txs); err != nil {
				t.Fatalf("failed transaction check for %d confs: %v", confs, err)
			}
		}
		return false
	}

	var confs uint32
	var prevHash *chainhash.Hash
	// Check the next (actually the previous) block. The returned bool indicates
	// that a block change was detected so the test should be run again.
	checkNextBlock := func() bool {
		block, err := dcr.client.GetBlock(ctx, prevHash)
		currentHeight = int64(block.Header.Height)
		if err != nil {
			t.Fatalf("error retrieving previous block (%s): %v", prevHash, err)
		}
		blockTxs := append([]*wire.MsgTx(nil), block.Transactions...)
		if scanUtxos(confs, append(blockTxs, block.STransactions...)) {
			return true
		}
		prevHash = &block.Header.PrevBlock
		confs++
		return false
	}

	for {
		// The loop can continue until a test completes without any block changes.
		var err error
		bestHash, currentHeight, err = dcr.client.GetBestBlock(ctx)
		prevHash = bestHash
		tipHeight = currentHeight
		if err != nil {
			t.Fatalf("error retrieving best block: %v", err)
		}
		refreshMempool()
		if scanUtxos(0, mempool) {
			continue
		}

		// Scan 10 more blocks.
		confs = 1
		startOver := false
		for i := 0; i < numBlocks; i++ {
			if checkNextBlock() {
				startOver = true
				break
			}
		}
		if !startOver {
			break
		}
	}
	t.Logf("%d P2PKH scripts", stats.p2pkh)
	t.Logf("%d stake P2PKH scripts", stats.sp2pkh)
	t.Logf("%d Schnorr P2PKH scripts", stats.p2pkhSchnorr)
	t.Logf("%d Edwards P2PKH scripts", stats.p2pkhEdwards)
	t.Logf("%d P2SH scripts", stats.p2sh)
	t.Logf("%d stake P2SH scripts", stats.sp2sh)
	t.Logf("%d immature transactions in the last %d blocks", stats.immatureBefore, maturity)
	t.Logf("%d immature transactions before %d blocks ago", stats.immatureAfter, maturity)
	t.Logf("total unspent value counted: %.2f DCR", float64(stats.utxoVal)/1e8)
	feeCount := len(stats.feeRates)
	if feeCount > 0 {
		var feeSum uint64
		for _, r := range stats.feeRates {
			feeSum += r
		}
		t.Logf("%d fees, avg rate %d", feeCount, feeSum/uint64(feeCount))
	}
}

// TestCacheAdvantage compares the speed of requesting blocks from the RPC vs.
//  using the cache to provide justification the added complexity.
func TestCacheAdvantage(t *testing.T) {
	client := dcr.client
	nextHash, _, err := client.GetBestBlock(ctx)
	if err != nil {
		t.Fatalf("error retrieving best block info")
	}
	numBlocks := 10000
	blocks := make([]*chainjson.GetBlockVerboseResult, 0, numBlocks)
	start := time.Now()
	// Download the blocks over RPC, recording the duration.
	for i := 0; i < numBlocks; i++ {
		block, err := client.GetBlockVerbose(ctx, nextHash, false)
		if err != nil {
			t.Fatalf("error retreving %d-th block from the tip: %v", i, err)
		}
		blocks = append(blocks, block)
		nextHash, err = chainhash.NewHashFromStr(block.PreviousHash)
		if err != nil {
			t.Fatalf("error decoding block id %s: %v", block.PreviousHash, err)
		}
	}
	t.Logf("%d blocks fetched via RPC in %.3f ms", numBlocks, float64(time.Since(start).Nanoseconds())/1e6)
	// Now go back trough the blocks, summing the encoded size and building a
	// slice of hashes.
	cache := newBlockCache(testLogger)
	byteCount := 0
	hashes := make([]*chainhash.Hash, 0, numBlocks)
	for _, block := range blocks {
		jsonBlock, err := json.Marshal(block)
		if err != nil {
			t.Fatalf("json encode error for block %s", block.Hash)
		}
		byteCount += len(jsonBlock)
		cache.add(block)
		hash, err := chainhash.NewHashFromStr(block.Hash)
		if err != nil {
			t.Fatalf("error decoding hash from %s", block.Hash)
		}
		hashes = append(hashes, hash)
	}
	t.Logf("%.1f MB of RPC bandwidth", float32(byteCount)/1e6)
	start = time.Now()
	// See how long it takes to retrieve the same block info from the cache.
	for _, hash := range hashes {
		b, found := cache.block(hash)
		if !found {
			t.Fatalf("blockCache test failed to find %s", hash)
		}
		_ = b
	}
	t.Logf("%d cached blocks retrieved in %.3f ms", numBlocks, float64(time.Since(start).Nanoseconds())/1e6)
}

// TestBlockMonitor is a live test that connects to dcrd and listens for block
// updates, checking the state of the cache along the way.
func TestBlockMonitor(t *testing.T) {
	testDuration := 60 * time.Minute
	fmt.Printf("Starting BlockMonitor test. Test will last for %d minutes\n", int(testDuration.Minutes()))
	blockChan := dcr.BlockChannel(5)
	expire := time.NewTimer(testDuration).C
	lastHeight := dcr.blockCache.tipHeight()
out:
	for {
		select {
		case update := <-blockChan:
			if update.Err != nil {
				t.Fatalf("error encountered while monitoring blocks: %v", update.Err)
			}
			tipHeight := dcr.blockCache.tipHeight()
			if update.Reorg {
				fmt.Printf("block received at height %d causes a %d block reorg\n", tipHeight, lastHeight-tipHeight+1)
			} else {
				fmt.Printf("block received for height %d\n", tipHeight)
			}
			lastHeight = tipHeight
			_, err := dcr.getMainchainDcrBlock(ctx, tipHeight)
			if err != nil {
				t.Fatalf("error getting newly connected block at height %d", tipHeight)
			}
		case <-ctx.Done():
			break out
		case <-expire:
			break out
		}
	}
}
