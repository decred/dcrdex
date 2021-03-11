// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"encoding/hex"
	"fmt"
	"sync"

	"decred.org/dcrdex/client/asset"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs/v3"
	"github.com/decred/dcrd/gcs/v3/blockcf2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
)

type externalTx struct {
	hash *chainhash.Hash

	outputsMtx sync.RWMutex
	outputs    map[uint32]*externalTxOut

	scanMtx          sync.Mutex
	blockHash        *chainhash.Hash
	lastScannedBlock *chainhash.Hash
}

type externalTxOut struct {
	pkScript []byte

	scanMtx          sync.Mutex
	spenderBlockHash *chainhash.Hash
	lastScannedBlock *chainhash.Hash
}

// trackExternalTxOut records the script associated with a tx output to enable
// spv wallets easily locate the tx in a block when it's mined and to easily
// determine if the output is spent in a mined transaction.
func (dcr *ExchangeWallet) trackExternalTxOut(hash *chainhash.Hash, vout uint32, pkScript []byte) {
	if !dcr.spvMode {
		return
	}

	dcr.externalTxMtx.Lock()
	defer func() {
		dcr.externalTxMtx.Unlock()
		dcr.log.Debugf("Script cached for non-wallet output %s:%d.", hash, vout)
	}()

	if tx, exists := dcr.externalTxs[*hash]; exists {
		tx.outputsMtx.Lock()
		tx.outputs[vout] = &externalTxOut{
			pkScript: pkScript,
		}
		tx.outputsMtx.Unlock()
		return
	}

	dcr.externalTxs[*hash] = &externalTx{
		hash: hash,
		outputs: map[uint32]*externalTxOut{
			vout: {
				pkScript: pkScript,
			},
		},
	}
}

// externalTxOutConfirmations uses the script associated with a tx output to
// find the block in which the tx is mined and to determine if the output has
// been spent. The tx output's script must have been previously recorded using
// dcr.trackExternalTxOut, otherwise this will return asset.CoinNotFoundError.
func (dcr *ExchangeWallet) externalTxOutConfirmations(hash *chainhash.Hash, vout uint32) (uint32, bool, error) {
	dcr.externalTxMtx.RLock()
	tx, tracked := dcr.externalTxs[*hash]
	dcr.externalTxMtx.RUnlock()
	if !tracked {
		dcr.log.Errorf("Attempted to find txout confirmations without a cached script for %s:%d.", hash, vout)
		return 0, false, asset.CoinNotFoundError
	}

	tx.outputsMtx.RLock()
	output, tracked := tx.outputs[vout]
	tx.outputsMtx.RUnlock()
	if !tracked {
		return 0, false, asset.CoinNotFoundError
	}

	// If this tx output is not yet known to be spent, scan block filters
	// to try to locate a spender. If some other process got here first, a
	// block scan might be underway already. This process will be forced to
	// wait until the previous call completes.
	output.scanMtx.Lock()
	defer output.scanMtx.Unlock()

	confs, err := dcr.externalTxConfirmations(tx)
	if confs == 0 || err != nil {
		return confs, false, err
	}

	if output.spenderBlockHash != nil {
		// Output was previously spent in this block. Confirm that this block
		// is still a mainchain block.
		_, isMainchainBlock, err := dcr.isMainchainBlock(output.spenderBlockHash)
		if isMainchainBlock || err != nil {
			return confs, isMainchainBlock, err
		}
		// If !isMainchainBlock and err == nil, the block previously found to contain
		// this output's spender has been orphaned. Rescan again.
		output.spenderBlockHash = nil
	}

	checkSpentError := func(err error) (uint32, bool, error) {
		return confs, false, fmt.Errorf("unable to determine if output %s:%d is spent: %v", hash, vout, err)
	}

	// Start searching for mined output spender from the block the containing
	// tx was mined or from the block that was last scanned for this purpose.
	var startBlockHash *chainhash.Hash
	if output.lastScannedBlock == nil {
		startBlockHash = tx.blockHash
	} else {
		startBlockHash = output.lastScannedBlock
	}
	// Use mainChainAncestor to ensure that scanning starts from a mainchain
	// block in the event that either tx block or last scanned block have been
	// re-orged out of the mainchain.
	_, startBlock, err := dcr.mainChainAncestor(startBlockHash)
	if err != nil {
		return checkSpentError(err)
	}

	// Attempt to find a tx that spends this output in the blocks between the
	// last scanned block and the latest/best block.
	bestBlockHeight := dcr.cachedBestBlock().height
	for blockHeight := startBlock.Height; blockHeight <= bestBlockHeight; blockHeight++ {
		blockHash, err := dcr.node.GetBlockHash(dcr.ctx, blockHeight)
		if err != nil {
			return checkSpentError(translateRPCCancelErr(err))
		}
		blockFilter, err := dcr.getBlockFilterV2(blockHash)
		if err != nil {
			return checkSpentError(err)
		}
		if !blockFilter.Match(output.pkScript) {
			continue // check next block's filters (blockHeight++)
		}
		block, err := dcr.getBlock(blockHash, true)
		if err != nil {
			return checkSpentError(err)
		}
		for i := range block.RawTx {
			if txSpendsOutput(&block.RawTx[i], hash, vout) {
				return confs, true, nil
			}
		}
		for i := range block.RawSTx {
			if txSpendsOutput(&block.RawSTx[i], hash, vout) {
				return confs, true, nil
			}
		}
		output.lastScannedBlock = blockHash
	}

	return confs, false, nil // scanned up to best block, no spender found
}

// externalTxConfirmations uses the output script(s) associated with the
// specified tx to find the block in which the tx is mined.
func (dcr *ExchangeWallet) externalTxConfirmations(tx *externalTx) (uint32, error) {
	// If this tx's block is not yet known, scan block filters to try to
	// locate it. If some other process got here first, a block scan might
	// be underway already. This process will be forced to wait until the
	// previous call completes.
	tx.scanMtx.Lock()
	defer tx.scanMtx.Unlock()

	// Check if a previous scan already found this tx's block.
	if tx.blockHash != nil {
		confs, err := dcr.blockConfirmations(tx.blockHash)
		if confs > -1 || err != nil {
			return uint32(confs), err
		}
		// If confs == -1 and err == nil, the block previously found to contain
		// this tx has been orphaned. Rescan again.
		tx.blockHash = nil
	}

	// Start a new search for this tx's block using the output scripts.
	tx.outputsMtx.RLock()
	outputScripts := make([][]byte, 0, len(tx.outputs))
	for _, output := range tx.outputs {
		outputScripts = append(outputScripts, output.pkScript)
	}
	tx.outputsMtx.RUnlock()

	// Scan block filters in reverse from the current best block to the last
	// scanned block. If the last scanned block has been re-orged out of the
	// mainchain, scan back to the mainchain ancestor of the last scanned block.
	var stopHeight int64
	var stopHash *chainhash.Hash
	if tx.lastScannedBlock != nil {
		stopBlockHash, stopBlock, err := dcr.mainChainAncestor(tx.lastScannedBlock)
		if err != nil {
			return 0, err
		}
		stopHeight = stopBlock.Height
		stopHash = stopBlockHash
	} else {
		// TODO: Determine a stopHeight to use based on when this tx was first seen
		// or some constant min block height value.
	}

	// Run cfilters scan in reverse.
	currentTip := dcr.cachedBestBlock()
	dcr.log.Debugf("Searching for tx %s in blocks %d (%s) to %d (%s).", tx.hash,
		currentTip.height, currentTip.hash, stopHeight, stopHash)
	for blockHeight := currentTip.height; blockHeight > stopHeight; blockHeight-- {
		blockHash, err := dcr.node.GetBlockHash(dcr.ctx, blockHeight)
		if err != nil {
			return 0, translateRPCCancelErr(err)
		}
		blockFilter, err := dcr.getBlockFilterV2(blockHash)
		if err != nil {
			return 0, err
		}
		if !blockFilter.MatchAny(outputScripts) {
			continue // check previous block's filters (blockHeight--)
		}
		dcr.log.Debugf("Block %d (%s) likely contains tx %s. Confirming.", blockHeight, blockHash, tx.hash)
		block, err := dcr.getBlock(blockHash, false)
		if err != nil {
			return 0, err
		}
		blockTxIDs := append(block.Tx, block.STx...)
		for _, blkTxID := range blockTxIDs {
			if blkTxID == tx.hash.String() {
				dcr.log.Debugf("Found mined tx %s in block %d (%s).", tx.hash, blockHeight, blockHash)
				tx.blockHash = blockHash
				return uint32(currentTip.height - block.Height + 1), nil
			}
		}
	}

	// Scan completed from current tip to last scanned block. Set the
	// current tip as the last scanned block so subsequent scans cover
	// the latest tip back to this current tip (excluded).
	tx.lastScannedBlock = currentTip.hash
	dcr.log.Debugf("Tx %s NOT found in blocks %d (%s) to %d (%s).", tx.hash,
		currentTip.height, currentTip.hash, stopHeight, stopHash)
	return 0, nil
}

func (dcr *ExchangeWallet) blockConfirmations(blockHash *chainhash.Hash) (int32, error) {
	block, isMainchain, err := dcr.isMainchainBlock(blockHash)
	if !isMainchain || err != nil {
		return -1, err
	}
	bestBlockHeight := dcr.cachedBestBlock().height
	return int32(bestBlockHeight - block.Height + 1), nil
}

// spendingInputIndex returns the index of the input in the provided tx that
// spends the provided output, if such input exists in the tx.
func txSpendsOutput(tx *chainjson.TxRawResult, prevOut *chainhash.Hash, prevOutIndex uint32) bool {
	for i := range tx.Vin {
		input := &tx.Vin[i]
		if input.Vout == prevOutIndex && input.Txid == prevOut.String() {
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

// Get the block v2filter, checking the cache first.
func (dcr *ExchangeWallet) getBlockFilterV2(blockHash *chainhash.Hash) (*blockFilter, error) {
	var blockCf2 walletjson.GetCFilterV2Result
	err := dcr.nodeRawRequest(methodGetCFilterV2, anylist{blockHash.String()}, &blockCf2)
	if err != nil {
		return nil, err
	}
	filterB, err := hex.DecodeString(blockCf2.Filter)
	if err != nil {
		return nil, fmt.Errorf("error decoding block filter: %w", err)
	}
	keyB, err := hex.DecodeString(blockCf2.Key)
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
