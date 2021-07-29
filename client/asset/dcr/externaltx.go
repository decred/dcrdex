// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"encoding/hex"
	"fmt"
	"sync"

	"decred.org/dcrdex/client/asset"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v3"
	"github.com/decred/dcrd/gcs/v3/blockcf2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/wire"
)

type externalTx struct {
	hash *chainhash.Hash

	mtx             sync.RWMutex
	relevantScripts [][]byte

	blockFiltersScanner

	// The folowing are protected by the blockFiltersScanner.scanMtx
	// because they are set when the tx's block is found and cleared
	// when the previously found tx block is orphaned. The scanMtx
	// lock must be held for read before accessing these fields.
	tree    int8
	outputs map[uint32]*externalTxOutput
}

type externalTxOutput struct {
	*wire.TxOut
	txHash  *chainhash.Hash
	vout    uint32
	spender blockFiltersScanner
}

func (output *externalTxOutput) String() string {
	return fmt.Sprintf("%s:%d", output.txHash, output.vout)
}

type blockFiltersScanner struct {
	scanMtx          sync.RWMutex
	lastScannedBlock *chainhash.Hash
	relevantBlock    *block
}

// relevantBlockHash returns the hash of the block relevant to this scanner, if
// the relevantBlok is set and is a mainchain block. Returns a nil hash and nil
// error if the relevantBlock is set but is no longer part of the mainchain.
// The scanner's scanMtx MUST be write-locked.
func (scanner *blockFiltersScanner) relevantBlockHash(nodeGetBlockHashFn func(int64) (*chainhash.Hash, error)) (*chainhash.Hash, error) {
	if scanner.relevantBlock == nil {
		return nil, nil
	}
	mainchainBlockHash, err := nodeGetBlockHashFn(scanner.relevantBlock.height)
	if err != nil {
		return nil, fmt.Errorf("cannot get hash for block %d: %v", scanner.relevantBlock.height, err)
	}
	if mainchainBlockHash.IsEqual(scanner.relevantBlock.hash) {
		return scanner.relevantBlock.hash, nil
	}
	scanner.relevantBlock = nil // clear so we don't keep checking if it is a mainchain block
	return nil, nil
}

// trackExternalTx records the script associated with a tx to enable spv
// wallets locate the tx in a block when it is mined. Once mined, the block
// containing the tx and the pkScripts of all of the tx outputs are recorded.
// The recorded output scripts can then be used to subsequently check if an
// output is spent in a mined transaction.
func (dcr *ExchangeWallet) trackExternalTx(hash *chainhash.Hash, script []byte) {
	defer func() {
		dcr.log.Debugf("Script %x cached for non-wallet tx %s.", script, hash)
	}()

	tx, tracked := dcr.externalTx(hash)
	if tracked {
		tx.mtx.Lock()
		tx.relevantScripts = append(tx.relevantScripts, script)
		tx.mtx.Unlock()
		return
	}

	dcr.externalTxMtx.Lock()
	dcr.externalTxs[*hash] = &externalTx{
		hash:            hash,
		relevantScripts: [][]byte{script},
	}
	dcr.externalTxMtx.Unlock()
}

func (dcr *ExchangeWallet) externalTx(hash *chainhash.Hash) (*externalTx, bool) {
	dcr.externalTxMtx.RLock()
	tx, tracked := dcr.externalTxs[*hash]
	dcr.externalTxMtx.RUnlock()
	return tx, tracked
}

func (dcr *ExchangeWallet) externalTxOut(hash *chainhash.Hash, vout uint32) (*wire.TxOut, int8, bool, error) {
	tx, tracked := dcr.externalTx(hash)
	if !tracked {
		return nil, 0, false, asset.CoinNotFoundError
	}

	// Lock the scanMtx to prevent attempted rescans from
	// mutating the tx block, outputs map or tree field.
	tx.scanMtx.RLock()
	txBlockHash, err := tx.relevantBlockHash(dcr.getBlockHash)
	if err != nil {
		tx.scanMtx.RUnlock()
		return nil, 0, false, err
	}
	if txBlockHash == nil {
		tx.scanMtx.RUnlock()
		return nil, 0, false, asset.CoinNotFoundError
	}
	output := tx.outputs[vout]
	txTree := tx.tree
	tx.scanMtx.RUnlock()

	if output == nil {
		return nil, 0, false, fmt.Errorf("tx %s has no output at index %d", hash, vout)
	}

	isSpent, err := dcr.findExternalTxOutputSpender(output, txBlockHash)
	if err != nil {
		return nil, 0, false, err
	}

	return output.TxOut, txTree, isSpent, nil
}

// externalTxOutConfirmations uses the script(s) associated with an externalTx
// to find the block in which the tx is mined and checks if the specified tx
// output is spent by a mined transaction. This tx must have been previously
// associated with one or more scripts using dcr.trackExternalTx, otherwise
// this will return asset.CoinNotFoundError.
func (dcr *ExchangeWallet) externalTxOutConfirmations(hash *chainhash.Hash, vout uint32) (uint32, bool, error) {
	tx, tracked := dcr.externalTx(hash)
	if !tracked {
		dcr.log.Errorf("Attempted to find confirmations for tx %s without an associated script.", hash)
		return 0, false, asset.CoinNotFoundError
	}

	// If this tx's block is not yet known, and some other process got here
	// first, a search for the tx's block might be underway already. Wait
	// until any previous search completes and releases this lock. Retain
	// the lock once acquired until we are done accessing tx.relevantBlock,
	// tx.tree and tx.outputSpendScanners.
	tx.scanMtx.Lock()

	// First try to determine if the tx has been mined before checking if
	// the specified output is spent. Outputs of unmined txs cannot be
	// spent in mined blocks.
	txBlockFound, err := dcr.findExternalTxBlock(tx)
	if !txBlockFound || err != nil {
		tx.scanMtx.Unlock()
		return 0, false, err
	}
	// Tx block is known. Read the tx block and desired output before
	// releasing the scanMtx lock.
	txBlock := tx.relevantBlock
	output := tx.outputs[vout]
	if output == nil {
		tx.scanMtx.Unlock()
		return 0, false, fmt.Errorf("tx %s has no output at index %d", hash, vout)
	}
	tx.scanMtx.Unlock()

	isSpent, err := dcr.findExternalTxOutputSpender(output, txBlock.hash)
	if err != nil {
		return 0, false, fmt.Errorf("unable to determine if output %s:%d is spent: %v", hash, vout, err)
	}

	bestBlockHeight := dcr.cachedBestBlock().height
	return uint32(bestBlockHeight - txBlock.height + 1), isSpent, nil
}

// findExternalTxBlock returns true if the block containing the provided tx is
// known and is a mainchain block. If the tx block is not yet known or has been
// re-orged out of the mainchain, this method matches the script(s) associated
// with the tx against multiple block cfilters (starting from the current best
// block down till the genesis block) in an attempt to find the block containing
// the tx.
// If found, the block hash, height and the tx output scripts are recorded.
// The tx's scanMtx MUST be write-locked.
func (dcr *ExchangeWallet) findExternalTxBlock(tx *externalTx) (bool, error) {
	// Check if this tx's block was found in a previous search attempt.
	txBlockHash, err := tx.relevantBlockHash(dcr.getBlockHash)
	if err != nil {
		return false, err
	}
	if txBlockHash != nil {
		return true, nil
	}

	// This tx's block is yet unknown. Clear the output scripts if they
	// were previously set using data from a previously recorded block
	// that is now invalidated.
	tx.tree = -1
	tx.outputs = nil

	// Start a new search for this tx's block using the associated scripts.
	tx.mtx.RLock()
	txScripts := tx.relevantScripts
	tx.mtx.RUnlock()

	// Scan block filters in reverse from the current best block to the last
	// scanned block. If the last scanned block has been re-orged out of the
	// mainchain, scan back to the mainchain ancestor of the lastScannedBlock.
	var lastScannedBlock block
	if tx.lastScannedBlock != nil {
		stopBlockHash, stopBlock, err := dcr.mainChainAncestor(tx.lastScannedBlock)
		if err != nil {
			return false, fmt.Errorf("error looking up mainchain ancestor for block %s", err)
		}
		lastScannedBlock.height = stopBlock.Height
		lastScannedBlock.hash = stopBlockHash
		tx.lastScannedBlock = stopBlockHash
	} else {
		// TODO: Determine a stopHeight to use based on when this tx was first seen
		// or some constant min block height value.
	}

	// Run cfilters scan in reverse from best block to lastScannedBlock.
	currentTip := dcr.cachedBestBlock()
	dcr.log.Debugf("Searching for tx %s in blocks %d (%s) to %d (%s).", tx.hash,
		currentTip.height, currentTip.hash, lastScannedBlock.height, lastScannedBlock.hash)
	for blockHeight := currentTip.height; blockHeight > lastScannedBlock.height; blockHeight-- {
		blockHash, err := dcr.wallet.GetBlockHash(dcr.ctx, blockHeight)
		if err != nil {
			return false, translateRPCCancelErr(err)
		}
		blockFilter, err := dcr.getBlockFilterV2(blockHash)
		if err != nil {
			return false, err
		}
		if !blockFilter.MatchAny(txScripts) {
			continue // check previous block's filters (blockHeight--)
		}
		dcr.log.Debugf("Block %d (%s) likely contains tx %s. Confirming.", blockHeight, blockHash, tx.hash)
		blk, err := dcr.getBlock(blockHash, true)
		if err != nil {
			return false, err
		}
		blockTxs := append(blk.RawTx, blk.RawSTx...)
		for i := range blockTxs {
			blkTx := &blockTxs[i]
			if blkTx.Txid == tx.hash.String() {
				dcr.log.Debugf("Found mined tx %s in block %d (%s).", tx.hash, blockHeight, blockHash)

				msgTx, err := msgTxFromHex(blkTx.Hex)
				if err != nil {
					return false, fmt.Errorf("invalid hex for tx %s: %v", tx.hash, err)
				}

				tx.relevantBlock = &block{hash: blockHash, height: blockHeight}
				tx.tree = determineTxTree(msgTx)

				// Store the pkScripts for all of this tx's outputs so they
				// can be used to later to determine if an output is spent.
				tx.outputs = make(map[uint32]*externalTxOutput)
				for i := range blkTx.Vout {
					output := &blkTx.Vout[i]
					amt, err := dcrutil.NewAmount(output.Value)
					if err != nil {
						dcr.log.Errorf("tx output %s:%d has invalid amount: %v", tx.hash, i, err)
						continue
					}
					pkScript, err := hex.DecodeString(output.ScriptPubKey.Hex)
					if err != nil {
						dcr.log.Errorf("tx output %s:%d has invalid pkScript: %v", tx.hash, i, err)
						continue
					}
					tx.outputs[uint32(i)] = &externalTxOutput{
						// TODO: output.ScriptPubKey.Version with dcrd 1.7 *release*, not yet
						TxOut:  newTxOut(int64(amt), output.Version, pkScript),
						txHash: tx.hash,
						vout:   uint32(i),
					}
				}
				return true, nil
			}
		}
	}

	// Scan completed from current tip to last scanned block. Set the
	// current tip as the last scanned block so subsequent scans cover
	// the latest tip back to this current tip.
	tx.lastScannedBlock = currentTip.hash
	dcr.log.Debugf("Tx %s NOT found in blocks %d (%s) to %d (%s).", tx.hash,
		currentTip.height, currentTip.hash, lastScannedBlock.height, lastScannedBlock.hash)
	return false, nil
}

// findExternalTxOutputSpender returns true if a block is found that contains
// a tx that spends the provided output AND the block is a mainchain block.
// If no block has been found to contain a tx that spends the provided output
// or the block that was found to contain the spender is re-orged out of the
// mainchain, this method matches the output's pkScript against multiple block
// cfilters (starting from the block containing the output up till the current
// best block) in an attempt to find the block containing the output spender.
// If found, the block hash and height is recorded.
func (dcr *ExchangeWallet) findExternalTxOutputSpender(output *externalTxOutput, txBlockHash *chainhash.Hash) (bool, error) {
	// If the spender of this tx output is not yet known, and some other
	// process got here first, a search for the tx output spender might
	// be underway already. Wait until any previous search completes and
	// releases this lock.
	output.spender.scanMtx.Lock()
	defer output.spender.scanMtx.Unlock()

	// Check if the spender of this tx output was found in a previous search
	// attempt.
	spenderBlockHash, err := output.spender.relevantBlockHash(dcr.getBlockHash)
	if err != nil {
		return false, err
	}
	if spenderBlockHash != nil {
		return true, nil
	}

	// This tx output is not known to be spent as of last search (if any).
	// Scan blocks from the lastScannedBlock (if there was a previous scan)
	// or from the tx block to attempt finding the spender of this output.
	// Use mainChainAncestor to ensure that scanning starts from a mainchain
	// block in the event that either tx block or lastScannedBlock have been
	// re-orged out of the mainchain.
	var startBlock *chainjson.GetBlockVerboseResult
	if output.spender.lastScannedBlock == nil {
		_, startBlock, err = dcr.mainChainAncestor(txBlockHash)
	} else {
		_, startBlock, err = dcr.mainChainAncestor(output.spender.lastScannedBlock)
	}
	if err != nil {
		return false, err
	}

	// Search for this output's spender in the blocks between startBlock and bestBlock.
	bestBlock := dcr.cachedBestBlock()
	dcr.log.Debugf("Searching for the tx that spends output %s in blocks %d (%s) to %d (%s).",
		output, startBlock.Height, startBlock.Hash, bestBlock.height, bestBlock.hash)
	for blockHeight := startBlock.Height; blockHeight <= bestBlock.height; blockHeight++ {
		blockHash, err := dcr.wallet.GetBlockHash(dcr.ctx, blockHeight)
		if err != nil {
			return false, translateRPCCancelErr(err)
		}
		blockFilter, err := dcr.getBlockFilterV2(blockHash)
		if err != nil {
			return false, err
		}
		if !blockFilter.Match(output.PkScript) {
			output.spender.lastScannedBlock = blockHash
			continue // check next block's filters (blockHeight++)
		}
		dcr.log.Debugf("Block %d (%s) likely contains a tx that spends %s. Confirming.",
			blockHeight, blockHash, output)
		blk, err := dcr.getBlock(blockHash, true)
		if err != nil {
			return false, err
		}
		for i := range blk.RawTx {
			blkTx := &blk.RawTx[i]
			if txSpendsOutput(blkTx, output) {
				dcr.log.Debugf("Found spender for output %s in block %d (%s), spender tx hash %s.",
					output, blockHeight, blockHash, blkTx.Txid)
				output.spender.relevantBlock = &block{hash: blockHash, height: blockHeight}
				return true, nil
			}
		}
		for i := range blk.RawSTx {
			blkTx := &blk.RawSTx[i]
			if txSpendsOutput(blkTx, output) {
				dcr.log.Debugf("Found spender for output %s in block %d (%s), spender tx hash %s.",
					output, blockHeight, blockHash, blkTx.Txid)
				output.spender.relevantBlock = &block{hash: blockHash, height: blockHeight}
				return true, nil
			}
		}
		output.spender.lastScannedBlock = blockHash
	}

	dcr.log.Debugf("No spender tx found for %s in blocks %d (%s) to %d (%s).",
		output, startBlock.Height, startBlock.Hash, bestBlock.height, bestBlock.hash)
	return false, nil // scanned up to best block, no spender found
}

// txSpendsOutput returns true if the passed tx has an input that spends the
// specified output.
func txSpendsOutput(tx *chainjson.TxRawResult, txOut *externalTxOutput) bool {
	for i := range tx.Vin {
		input := &tx.Vin[i]
		if input.Vout == txOut.vout && input.Txid == txOut.txHash.String() {
			return true // found spender
		}
	}
	return false
}

type blockFilter struct {
	v2cfilters *gcs.FilterV2
	key        [gcs.KeySize]byte
}

func (bf *blockFilter) Match(data []byte) bool {
	return bf.v2cfilters.Match(bf.key, data)
}

func (bf *blockFilter) MatchAny(data [][]byte) bool {
	return bf.v2cfilters.MatchAny(bf.key, data)
}

func (dcr *ExchangeWallet) getBlockFilterV2(blockHash *chainhash.Hash) (*blockFilter, error) {
	bf, key, err := dcr.wallet.BlockCFilter(dcr.ctx, blockHash)
	if err != nil {
		return nil, err
	}
	filterB, err := hex.DecodeString(bf)
	if err != nil {
		return nil, fmt.Errorf("error decoding block filter: %w", err)
	}
	keyB, err := hex.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("error decoding block filter key: %w", err)
	}
	filter, err := gcs.FromBytesV2(blockcf2.B, blockcf2.M, filterB)
	if err != nil {
		return nil, fmt.Errorf("error deserializing block filter: %w", err)
	}
	var bcf2Key [gcs.KeySize]byte
	copy(bcf2Key[:], keyB)

	return &blockFilter{
		v2cfilters: filter,
		key:        bcf2Key,
	}, nil
}
