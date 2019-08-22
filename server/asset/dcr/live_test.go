// +build dcrlive
//
// These tests can be run by using the -tags flag.
//
// go test -tags=dcrlive -run=LiveUTXO
// -----------------------------------
// This command will check the UTXO paths by iterating backwards through
// the transactions in the mainchain, starting with mempool, and requesting
// all found outputs through the dcrBackend.utxo method. All utxos must be
// found or found to be spent.
//
// go test -tags=dcrlive -run=CacheAdvantage
// -----------------------------------------
// Check the difference between using the block cache and requesting via RPC.

package dcr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"
)

var (
	dcrdConfigPath = filepath.Join(dcrdHomeDir, "dcrd.conf")
	dcr            *dcrBackend
)

type dcrdConfig struct {
	RPCUser string `short:"u" long:"rpcuser" description:"Username for RPC connections"`
	RPCPass string `short:"P" long:"rpcpass" default-mask:"-" description:"Password for RPC connections"`
}

func TestMain(m *testing.M) {
	UseLogger(slog.NewBackend(os.Stdout).Logger("DCRTEST"))
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	cfg := new(dcrdConfig)
	err := flags.NewIniParser(flags.NewParser(cfg, flags.Default|flags.IgnoreUnknown)).ParseFile(dcrdConfigPath)
	if err != nil {
		fmt.Printf("error reading dcrd config: %v\n", err)
		return
	}
	// fmt.Printf("dcrd user: %s\n", cfg.RPCUser)
	// fmt.Printf("dcrd pass: %s\n", cfg.RPCPass)
	dcr, err = NewDCR(&DCRConfig{
		Net:      mainnetName,
		DcrdUser: cfg.RPCUser,
		DcrdPass: cfg.RPCPass,
		DcrdServ: defaultMainnet,
		DcrdCert: defaultDaemonRPCCertFile,
		Context:  ctx,
	})
	if err != nil {
		fmt.Printf("NewDCR error: %v\n", err)
		return
	}
	os.Exit(m.Run())
}

// TestLiveUTXO will iterate the blockchain backwards, starting with mempool,
// checking that UTXOs are behaving as expected along the way.
func TestLiveUTXO(t *testing.T) {
	var bestHash *chainhash.Hash
	var mempool []*wire.MsgTx
	var txs []*wire.MsgTx
	maturity := uint32(chainParams.CoinbaseMaturity)

	// Check if the best hash is still bestHash.
	blockChanged := func() bool {
		h, _, _ := dcr.client.GetBestBlock()
		return *bestHash != *h
	}

	// Get the MsgTxs for the lsit of hashes.
	getMsgTxs := func(hashes []*chainhash.Hash) []*wire.MsgTx {
		outTxs := make([]*wire.MsgTx, 0)
		for _, h := range hashes {
			newTx, err := dcr.client.GetRawTransaction(h)
			if err != nil {
				t.Fatalf("error retreiving MsgTx: %v", err)
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
		mpHashes, err := dcr.client.GetRawMempool("all")
		if err != nil {
			t.Fatalf("getrawmempool error: %v", err)
		}
		mempool = getMsgTxs(mpHashes)
	}

	// checkTransactions checks that the UTXO for a all outputs is what is
	// expected.
	checkTransactions := func(expectedConfs uint32, txSet []*wire.MsgTx) error {
		txs = append(txs, txSet...)
		for i, msgTx := range txSet {
			if i == 0 && expectedConfs < maturity {
				continue
			}
			txHash := msgTx.CachedTxHash()
			for vout, out := range msgTx.TxOut {
				if out.Value == 0 {
					continue
				}
				// Check if its an acceptable script type.
				scriptTypeOK := isAcceptableScript(out.PkScript)
				// Now try to get the UXO with the dcrBackend
				utxo, err := dcr.utxo(txHash, uint32(vout))
				// There are 4 possibilities
				// 1. script is of acceptable type and utxo was found, in which case there
				//    should be zero confirmations (unless a new block snuck in).
				// 2. script is of acceptable type and utxo was not found. Error
				//    unless output is spent in mempool.
				// 3. script is of unacceptable type, and is found. Error.
				// 4. script is of unacceptable type, and is not found. Should receive an
				//    assets.UnsupportedScriptError.
				switch true {
				case scriptTypeOK && err == nil:
					// Just check for no error on Confirmations.
					confs, err := utxo.Confirmations()
					if err != nil {
						return fmt.Errorf("error getting confirmations for mempool tx output: %v", err)
					}
					if confs != int64(expectedConfs) {
						return fmt.Errorf("expected %d confirmations, found %d for %s:%d", expectedConfs, confs, txHash, vout)
					}
					break
				case scriptTypeOK && err != nil:
					// This is only okay if output is being spent by another transaction.
					// Since we are iterating backwards starting with mempool, we would
					// already know the spending transaction and have it stored.
					if !txOutIsSpent(*txHash, uint32(vout)) {
						return fmt.Errorf("unexpected UTXO error: %v", err)
					}
					break
				case !scriptTypeOK && err == nil:
					return fmt.Errorf("received UTXO for unacceptable script type")
				default: // !scriptTypeOK && err != nil
					// should receive an UnsupportedScriptError
					if err != asset.UnsupportedScriptError {
						return fmt.Errorf("expected unsupported script error for %s:%d, but received '%v'", txHash, vout, err)
					}
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
		block, err := dcr.client.GetBlock(prevHash)
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
		bestHash, _, err = dcr.client.GetBestBlock()
		prevHash = bestHash
		if err != nil {
			t.Fatalf("error retreiving best block: %v", err)
		}
		refreshMempool()
		if scanUtxos(0, mempool) {
			continue
		}

		// Scan 10 more blocks.
		confs = 1
		startOver := false
		for i := 0; i < 10; i++ {
			if checkNextBlock() {
				startOver = true
				break
			}
		}
		if !startOver {
			break
		}
	}
}

func TestCacheAdvantage(t *testing.T) {
	// Compare the speed of requesting blocks from the RPC vs. using the cache,
	// just to justify the added complexity.
	client := dcr.client
	nextHash, _, err := client.GetBestBlock()
	if err != nil {
		t.Fatalf("error retreiving best block info")
	}
	numBlocks := 10000
	blocks := make([]*chainjson.GetBlockVerboseResult, 0, numBlocks)
	start := time.Now()
	// Download the blocks over RPC, recording the duration.
	for i := 0; i < numBlocks; i++ {
		block, err := client.GetBlockVerbose(nextHash, false)
		if err != nil {
			t.Fatalf("error retreving %d-th block from the tip: %v", i, err)
		}
		blocks = append(blocks, block)
		nextHash, err = chainhash.NewHashFromStr(block.PreviousHash)
		if err != nil {
			t.Fatalf("error decoding block id %s: %v", block.PreviousHash, err)
		}
	}
	fmt.Printf("%d blocks fetched via RPC in %.3f ms\n", numBlocks, float64(time.Since(start).Nanoseconds())/1e6)
	// Now go back trough the blocks, summing the encoded size and building a
	// slice of hashes.
	cache := newBlockCache()
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
	fmt.Printf("%.1f MB of RPC bandwidth\n", float32(byteCount)/1e6)
	start = time.Now()
	// See how long it takes to retrieve the same block info from the cache.
	for _, hash := range hashes {
		b, found := cache.block(hash)
		if !found {
			t.Fatalf("blockCache test failed to find %s", hash)
		}
		_ = b
	}
	fmt.Printf("%d cached blocks retreived in %.3f ms\n", numBlocks, float64(time.Since(start).Nanoseconds())/1e6)
}
