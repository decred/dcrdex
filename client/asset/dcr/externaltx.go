// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

type externalTx struct {
	hash *chainhash.Hash

	// blockMtx protects access to the fields below it, which
	// are set when the tx's block is found and cleared when
	// the previously found tx block is orphaned.
	blockMtx         sync.RWMutex
	lastScannedBlock *chainhash.Hash
	block            *block
	tree             int8
	outputSpenders   []*outputSpenderFinder
}

type outputSpenderFinder struct {
	*wire.TxOut
	op   outPoint
	tree int8

	spenderMtx       sync.RWMutex
	lastScannedBlock *chainhash.Hash
	spenderBlock     *block
}

// lookupTxOutWithBlockFilters returns confirmations and spend status of the
// requested output. If the block containing the output is not yet known, a
// a block filters scan is conducted to determine if the output is mined in a
// block between the current best block and the block just before the provided
// earliestTxTime. Returns asset.CoinNotFoundError if the block containing the
// output is not found.
func (dcr *ExchangeWallet) lookupTxOutWithBlockFilters(ctx context.Context, op outPoint, pkScript []byte, earliestTxTime time.Time) (uint32, bool, error) {
	if len(pkScript) == 0 {
		return 0, false, fmt.Errorf("cannot perform block filters lookup without a script")
	}

	output, outputBlock, err := dcr.externalTxOutput(ctx, op, pkScript, earliestTxTime)
	if err != nil {
		return 0, false, err
	}

	spent, err := dcr.isOutputSpent(ctx, output)
	if err != nil {
		return 0, false, fmt.Errorf("error checking if output %s is spent: %v", op, err)
	}

	// Get the current tip height to calculate confirmations.
	tip, err := dcr.getBestBlock(ctx)
	if err != nil {
		dcr.log.Errorf("getbestblock error %v", err)
		*tip = dcr.cachedBestBlock()
	}
	var confs uint32
	if tip.height >= outputBlock.height { // slight possibility that the cached tip height is behind the output's block height
		confs = uint32(tip.height + 1 - outputBlock.height)
	}
	return confs, spent, nil
}

// externalTxOutput attempts to locate the requested tx output in a mainchain
// block and if found, returns the output details along with the block details.
func (dcr *ExchangeWallet) externalTxOutput(ctx context.Context, op outPoint, pkScript []byte, earliestTxTime time.Time) (*outputSpenderFinder, *block, error) {
	dcr.externalTxMtx.Lock()
	tx := dcr.externalTxCache[op.txHash]
	if tx == nil {
		tx = &externalTx{hash: &op.txHash}
		dcr.externalTxCache[op.txHash] = tx
	}
	dcr.externalTxMtx.Unlock()

	// Hold the tx.blockMtx lock for 2 reasons:
	// 1) To read/write the tx.block, tx.tree and tx.outputSpenders fields.
	// 2) To prevent duplicate tx block scans if this tx block is not already
	//    known. Holding this lock now ensures that any ongoing scan completes
	//    before we try to access the tx.block field which may prevent
	//    unnecessary rescan.
	tx.blockMtx.Lock()
	defer tx.blockMtx.Unlock()

	// First check if the tx block is cached.
	txBlock, err := dcr.txBlockFromCache(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("error checking if tx %s is known to be mined: %v", tx.hash, err)
	}

	// Scan block filters to find the tx block if it is yet unknown.
	if txBlock == nil {
		txBlock, err = dcr.scanFiltersForTxBlock(ctx, tx, [][]byte{pkScript}, earliestTxTime)
		if err != nil {
			return nil, nil, fmt.Errorf("error checking if tx %s is mined: %v", tx.hash, err)
		}
		if txBlock == nil {
			return nil, nil, asset.CoinNotFoundError
		}
	}

	if len(tx.outputSpenders) <= int(op.vout) {
		return nil, nil, fmt.Errorf("tx %s does not have an output at index %d", tx.hash, op.vout)
	}
	return tx.outputSpenders[op.vout], txBlock, nil
}

// txBlockFromCache returns the block containing this tx if it's known and
// still part of the mainchain. It is not an error if the block is unknown
// or invalidated.
// The tx.blockMtx MUST be locked for writing.
func (dcr *ExchangeWallet) txBlockFromCache(ctx context.Context, tx *externalTx) (*block, error) {
	if tx.block == nil {
		return nil, nil
	}

	isMainchain, err := dcr.wallet.IsValidMainchain(ctx, tx.block.hash)
	if err != nil {
		return nil, err
	}

	if isMainchain {
		dcr.log.Debugf("Cached tx %s is mined in block %d (%s).", tx.hash, tx.block.height, tx.block.hash)
		return tx.block, nil
	}

	// Tx block was previously set but seems to have been invalidated.
	// Clear the tx tree, outputs and block info fields that must have
	// been previously set.
	dcr.log.Warnf("Block %s found to contain tx %s has been invalidated.", tx.block.hash, tx.hash)
	tx.block = nil
	tx.tree = -1
	tx.outputSpenders = nil
	return nil, nil
}

// scanFiltersForTxBlock attempts to find the block containing the provided tx
// by scanning block filters from the current best block down to the block just
// before earliestTxTime or the block that was last scanned, if there was a
// previous scan. If the tx block is found, the block hash, height and the tx
// outputs details are cached; and the block is returned.
// The tx.blockMtx MUST be locked for writing.
func (dcr *ExchangeWallet) scanFiltersForTxBlock(ctx context.Context, tx *externalTx, txScripts [][]byte, earliestTxTime time.Time) (*block, error) {
	// Scan block filters in reverse from the current best block to the last
	// scanned block. If the last scanned block has been re-orged out of the
	// mainchain, scan back to the mainchain ancestor of the lastScannedBlock.
	var lastScannedBlock *block
	if tx.lastScannedBlock != nil {
		stopBlockHash, stopBlockHeight, err := dcr.mainchainAncestor(ctx, tx.lastScannedBlock)
		if err != nil {
			return nil, fmt.Errorf("error looking up mainchain ancestor for block %s", err)
		}
		tx.lastScannedBlock = stopBlockHash
		lastScannedBlock = &block{hash: stopBlockHash, height: stopBlockHeight}
	}

	// Run cfilters scan in reverse from best block to lastScannedBlock or
	// to block just before earliestTxTime.
	currentTip := dcr.cachedBestBlock()
	if lastScannedBlock == nil {
		dcr.log.Debugf("Searching for tx %s in blocks between block %d (%s) to the block just before %s.",
			tx.hash, currentTip.height, currentTip.hash, earliestTxTime)
	} else if lastScannedBlock.height < currentTip.height {
		dcr.log.Debugf("Searching for tx %s in blocks %d (%s) to %d (%s).", tx.hash,
			currentTip.height, currentTip.hash, lastScannedBlock.height, lastScannedBlock.hash)
	} else {
		if lastScannedBlock.height > currentTip.height {
			dcr.log.Warnf("Previous cfilters look up for tx %s stopped at block %d but current tip is %d?",
				tx.hash, lastScannedBlock.height, currentTip.height)
		}
		return nil, nil // no new blocks to scan
	}

	iHash := currentTip.hash
	iHeight := currentTip.height

	// Set the current tip as the last scanned block so subsequent
	// scans cover the latest tip back to this current tip.
	scanCompletedWithoutResults := func() (*block, error) {
		tx.lastScannedBlock = currentTip.hash
		dcr.log.Debugf("Tx %s NOT found in blocks %d (%s) to %d (%s).", tx.hash,
			currentTip.height, currentTip.hash, iHeight, iHash)
		return nil, nil
	}

	earliestTxStamp := earliestTxTime.Unix()
	for {
		msgTx, outputSpenders, err := dcr.findTxInBlock(ctx, *tx.hash, txScripts, iHash)
		if err != nil {
			return nil, err
		}

		if msgTx != nil {
			tx.block = &block{hash: iHash, height: iHeight}
			tx.tree = determineTxTree(msgTx)
			tx.outputSpenders = outputSpenders
			return tx.block, nil
		}

		// Block does not include the tx, check the previous block.
		// Abort the search if we've scanned blocks from the tip back to the
		// block we scanned last or the block just before earliestTxTime.
		if iHeight == 0 {
			return scanCompletedWithoutResults()
		}
		if lastScannedBlock != nil && iHeight <= lastScannedBlock.height {
			return scanCompletedWithoutResults()
		}
		iBlock, err := dcr.wallet.GetBlockHeader(dcr.ctx, iHash)
		if err != nil {
			return nil, fmt.Errorf("getblockheader error for block %s: %w", iHash, translateRPCCancelErr(err))
		}
		if iBlock.Timestamp.Unix() <= earliestTxStamp {
			return scanCompletedWithoutResults()
		}

		iHeight--
		iHash = &iBlock.PrevBlock
		continue
	}
}

func (dcr *ExchangeWallet) findTxInBlock(ctx context.Context, txHash chainhash.Hash, txScripts [][]byte, blockHash *chainhash.Hash) (*wire.MsgTx, []*outputSpenderFinder, error) {
	key, filter, err := dcr.wallet.BlockFilter(ctx, blockHash)
	if err != nil {
		return nil, nil, err
	}
	if !filter.MatchAny(key, txScripts) {
		return nil, nil, nil
	}

	blk, err := dcr.wallet.GetBlock(ctx, blockHash)
	if err != nil {
		return nil, nil, fmt.Errorf("error retrieving block %s: %w", blockHash, err)
	}

	var msgTx *wire.MsgTx
	for _, tx := range blk.Transactions {
		if tx.TxHash() == txHash {
			dcr.log.Debugf("Found mined tx %s in block %s.", txHash, blk.BlockHash())
			msgTx = tx
			break
		}
	}

	if msgTx == nil {
		dcr.log.Debugf("Block %s filters matched scripts for tx %s but does NOT contain the tx.", blk.BlockHash(), txHash)
		return nil, nil, nil
	}

	// We have the txs in this block, check if any them spends an output
	// from the original tx.
	outputSpenders := make([]*outputSpenderFinder, len(msgTx.TxOut))
	for i, txOut := range msgTx.TxOut {
		outputSpenders[i] = &outputSpenderFinder{
			TxOut:            txOut,
			op:               newOutPoint(&txHash, uint32(i)),
			tree:             determineTxTree(msgTx),
			lastScannedBlock: blockHash,
		}
	}
	for _, tx := range blk.Transactions {
		if tx.TxHash() == txHash {
			continue // oriignal tx, ignore
		}
		for _, txIn := range tx.TxIn {
			if txIn.PreviousOutPoint.Hash == txHash { // found a spender
				outputSpenders[txIn.PreviousOutPoint.Index].spenderBlock = &block{int64(blk.Header.Height), blockHash}
			}
		}
	}

	return msgTx, outputSpenders, nil
}

func (dcr *ExchangeWallet) isOutputSpent(ctx context.Context, output *outputSpenderFinder) (bool, error) {
	// Hold the output.spenderMtx lock for 2 reasons:
	// 1) To read (and set) the spenderBlock field.
	// 2) To prevent duplicate spender block scans if the spenderBlock is not
	//    already known. Holding this lock now ensures that any ongoing scan
	//    completes before we try to access the output.spenderBlock field
	//    which may prevent unnecessary rescan.
	output.spenderMtx.Lock()
	defer output.spenderMtx.Unlock()

	// Check if this output is known to be spent in a mainchain block.
	if output.spenderBlock != nil {
		isMainchain, err := dcr.wallet.IsValidMainchain(ctx, output.spenderBlock.hash)
		if err != nil {
			return false, err
		}
		if isMainchain {
			dcr.log.Debugf("Found cached information for the spender of %s.", output.op)
			return true, nil
		}
		// Output was previously found to have been spent but the block
		// containing the spending tx seems to have been invalidated.
		dcr.log.Warnf("Block %s found to contain spender of output %s has been invalidated.",
			output.spenderBlock.hash, output.op)
		output.spenderBlock = nil
	}

	// This tx output is not known to be spent as of last search (if any).
	// Scan block filters starting from the block after the tx block or the
	// lastScannedBlock (if there was a previous scan). Use mainchainAncestor
	// to ensure that scanning starts from a mainchain block in the event that
	// the lastScannedBlock have been re-orged out of the mainchain. We already
	// checked that the txBlock is not invalidated above.
	_, lastScannedHeight, err := dcr.mainchainAncestor(ctx, output.lastScannedBlock)
	if err != nil {
		return false, err
	}
	nextScanHeight := lastScannedHeight + 1

	bestBlock := dcr.cachedBestBlock()
	if nextScanHeight >= bestBlock.height {
		if nextScanHeight > bestBlock.height {
			dcr.log.Warnf("Attempted to look for output spender in block %d but current tip is %d!",
				nextScanHeight, bestBlock.height)
		}
		// No new blocks to scan, output isn't spent as of last scan.
		return false, nil
	}

	// Search for this output's spender in the blocks between startBlock and
	// the current best block.
	nextScanHash, err := dcr.wallet.GetBlockHash(ctx, nextScanHeight)
	if err != nil {
		return false, err
	}
	spenderTx, stopBlockHash, stopBlockHeight, err := dcr.findTxOutSpender(ctx, output.op, output.PkScript, &block{nextScanHeight, nextScanHash})
	if stopBlockHash != nil { // might be nil if the search never scanned a block
		output.lastScannedBlock = stopBlockHash
	}
	if err != nil {
		return false, err
	}

	// Cache relevant spender info if the spender is found.
	if spenderTx == nil {
		return false, nil
	}

	output.spenderBlock = &block{hash: stopBlockHash, height: stopBlockHeight}
	return true, nil
}

// findTxOutSpender attempts to find and return the tx that spends the provided
// output by matching the provided outputPkScript against the block filters of
// the mainchain blocks between the provided startBlock and the current best
// block.
// If no tx is found to spend the provided output, the hash of the block that
// was last checked is returned along with any error that may have occurred
// during the search.
func (dcr *ExchangeWallet) findTxOutSpender(ctx context.Context, op outPoint, outputPkScript []byte, startBlock *block) (*wire.MsgTx, *chainhash.Hash, int64, error) {
	var lastScannedHash *chainhash.Hash

	iHeight := startBlock.height
	iHash := startBlock.hash
	bestBlock := dcr.cachedBestBlock()
	for {
		key, filter, err := dcr.wallet.BlockFilter(ctx, iHash)
		if err != nil {
			return nil, lastScannedHash, 0, err
		}

		if filter.Match(key, outputPkScript) {
			dcr.log.Debugf("Output %s is likely spent in block %d (%s). Confirming.",
				op, iHeight, iHash)
			blk, err := dcr.wallet.GetBlock(ctx, iHash)
			if err != nil {
				return nil, lastScannedHash, 0, fmt.Errorf("error retrieving block %s: %w", iHash, err)
			}
			for _, tx := range blk.Transactions {
				if txSpendsOutput(tx, op) {
					dcr.log.Debugf("Found spender for output %s in block %d (%s), spender tx hash %s.",
						op, iHeight, iHash, tx.TxHash())
					return tx, iHash, iHeight, nil
				}
			}
			dcr.log.Debugf("Output %s is NOT spent in block %d (%s).", op, iHeight, iHash)
		}

		if iHeight >= bestBlock.height { // reached the tip, stop searching
			break
		}

		// Block does not include the output spender, check the next block.
		iHeight++
		nextHash, err := dcr.wallet.GetBlockHash(ctx, iHeight)
		if err != nil {
			return nil, iHash, 0, translateRPCCancelErr(err)
		}
		lastScannedHash = iHash
		iHash = nextHash
	}

	dcr.log.Debugf("Output %s is NOT spent in blocks %d (%s) to %d (%s).",
		op, startBlock.height, startBlock.hash, bestBlock.height, bestBlock.hash)
	return nil, bestBlock.hash, 0, nil // scanned up to best block, no spender found
}

// txSpendsOutput returns true if the passed tx has an input that spends the
// specified output.
func txSpendsOutput(tx *wire.MsgTx, op outPoint) bool {
	if tx.TxHash() == op.txHash {
		return false // no need to check inputs if this tx is the same tx that pays to the specified op
	}
	for _, txIn := range tx.TxIn {
		prevOut := &txIn.PreviousOutPoint
		if prevOut.Index == op.vout && prevOut.Hash == op.txHash {
			return true // found spender
		}
	}
	return false
}
