// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

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

	mtx       sync.RWMutex // protects access to pkScripts
	pkScripts [][]byte

	// blockMtx protects access to the fields below because
	// they are set when the tx's block is found and cleared
	// when the previously found tx block is orphaned.
	blockMtx         sync.RWMutex
	lastScannedBlock *chainhash.Hash
	block            *block
	tree             int8
	outputs          []*externalTxOutput
}

type externalTxOutput struct {
	outPoint
	value           float64
	pkScriptHex     string
	pkScriptVersion uint16

	// The spenderMtx protects access to the fields below
	// because they are set when the block containing the tx
	// that spends this output is found and cleared when the
	// previously found block is orphaned.
	spenderMtx       sync.RWMutex
	lastScannedBlock *chainhash.Hash
	spenderBlock     *block
}

// externalTx returns details for the provided hash, if cached. If the tx cache
// doesn't yet exist and addToCache is true, the provided script will be cached
// against the tx hash to enable SPV wallets locate the tx in a block when it is
// mined. Once mined, the block containing the tx and the tx outputs details are
// also cached, to enable subsequently checking if any of the tx's output is
// spent in a mined transaction.
//
// This method should only be used with transactions that are NOT indexed by the
// wallet such as counter-party swaps.
func (dcr *ExchangeWallet) externalTx(hash *chainhash.Hash, pkScript []byte, addToCache bool) *externalTx {
	dcr.externalTxMtx.Lock()
	defer dcr.externalTxMtx.Unlock()

	tx := dcr.externalTxCache[*hash]
	if tx == nil && addToCache && len(pkScript) > 0 {
		tx := &externalTx{
			hash:      hash,
			pkScripts: [][]byte{pkScript},
		}
		dcr.externalTxCache[*hash] = tx
		dcr.log.Debugf("Script %x cached for non-wallet tx %s.", pkScript, hash)
	}
	// TODO: Consider appending this pkScript to the tx if cached.

	return tx
}

// externalTxOut returns details for the specified transaction output, along
// with the confirmations, spend status and tx tree. If the tx details are not
// currently cached, a search will be conducted to attempt finding the tx in a
// mainchain block, unless tryFindTx is false. asset.CoinNotFoundError is
// returned if the output cannot be found or (for full node wallets), if the
// output is spent.
//
// This method should only be used for transactions that are NOT indexed by the
// wallet. For wallet transactions, use dcr.walletOutputConfirmations.
//
// NOTE: SPV wallets are unable to look up unmined transaction outputs. Also,
// the `tryFindTx`, `pkScript` and `earliestTxTime` parameters are irrelevant
// for full node wallets, but required for SPV wallets if the caller intends to
// perform a search for the tx in a mainchain block.
func (dcr *ExchangeWallet) externalTxOut(ctx context.Context, op outPoint, tryFindTx bool, pkScript []byte, earliestTxTime time.Time) (
	*wire.TxOut, uint32, bool, int8, error) {

	if !dcr.wallet.SpvMode() {
		// Use the gettxout rpc to look up the requested tx output. Unlike
		// SPV wallets, full node wallets are able to look up outputs for
		// all transactions whether or not they are indexed by the wallet,
		// including outputs in mempool.
		output, txTree, err := dcr.unspentTxOut(ctx, &op.txHash, op.vout, true)
		if err != nil {
			return nil, 0, false, 0, fmt.Errorf("error finding unspent output %s: %w", op, translateRPCCancelErr(err))
		}
		if output == nil {
			// Output does not exist or has been spent.
			// We can try to look up the tx to determine if the output exists
			// and return a 'output is spent' error, but the getrawtransaction
			// rpc requires txindex to be enabled which may not be enabled by
			// the client. We also can't use the gettransaction rpc because
			// this method is particularly designed to work with txs that are
			// NOT indexed by the wallet and gettransaction only returns data
			// for wallet txs.
			//
			// TODO: Attempt finding the tx using block filters. If the tx is
			// found, then we can assert that the output is spent instead of
			// returning asset.CoinNotFoundError.
			return nil, 0, false, 0, asset.CoinNotFoundError
		}
		amt, outputPkScript, err := parseAmountAndScript(output.Value, output.ScriptPubKey.Hex)
		if err != nil {
			return nil, 0, false, 0, fmt.Errorf("error parsing tx output %s: %v", op, err)
		}
		return newTxOut(amt, output.ScriptPubKey.Version, outputPkScript), uint32(output.Confirmations), false, txTree, nil
	}

	// This is an SPV wallet. First try to determine if the tx has been mined
	// before checking if the specified output is spent. This will require
	// scanning block filters to try to locate the tx, if the tx's block is
	// not already known or the previously found tx block is no longer part of
	// the mainchain. If tryFindTx is false however, do NOT scan scan block
	// filters; instead, return asset.CoinNotFoundError.

	tx := dcr.externalTx(&op.txHash, pkScript, tryFindTx) // the tx hash and script will be cached if not previously cached and if tryFindTx is true
	if tx == nil {
		return nil, 0, false, 0, asset.CoinNotFoundError
	}

	// Hold the tx.blockMtx lock for 2 reasons:
	// 1) To read the tx block, outputs map, tree and outputs fields.
	// 2) To prevent duplicate tx block scans if this tx block is not already
	//    known and tryFindTx is true.
	// The closure below helps to ensure that blockMtx lock is released
	// as soon as all those are done.
	txBlock, output, txTree, err := func() (*block, *externalTxOutput, int8, error) {
		tx.blockMtx.Lock()
		defer tx.blockMtx.Unlock()
		txBlock, err := dcr.externalTxBlock(ctx, tx, tryFindTx, earliestTxTime)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("error checking if tx %s is mined: %v", op.txHash, err)
		}
		if txBlock == nil {
			// SPV wallets cannot look up unmined txs.
			return nil, nil, 0, asset.CoinNotFoundError
		}
		if len(tx.outputs) <= int(op.vout) {
			return nil, nil, 0, fmt.Errorf("tx %s does not have an output at index %d", op.txHash, op.vout)
		}
		return txBlock, tx.outputs[op.vout], tx.tree, nil
	}()
	if err != nil {
		return nil, 0, false, 0, err
	}

	amt, outputPkScript, err := parseAmountAndScript(output.value, output.pkScriptHex)
	if err != nil {
		return nil, 0, false, 0, fmt.Errorf("error parsing tx output %s: %v", op, err)
	}
	txOut := newTxOut(amt, output.pkScriptVersion, outputPkScript)

	// We have the requested output, let's check if it is spent.
	// Hold the output.spenderMtx lock for 2 reasons:
	// 1) To read (and set) the spenderBlock field.
	// 2) To prevent duplicate spender block scans if the spenderBlock is not
	//    already known.
	// The closure below helps to ensure that spenderMtx lock is released
	// as soon as all those are done.
	isSpent, err := func() (bool, error) {
		output.spenderMtx.Lock()
		defer output.spenderMtx.Unlock()

		// Check if this output is known to be spent in a mainchain block.
		spenderFound, err := dcr.isMainchainBlock(ctx, output.spenderBlock)
		if err != nil {
			return false, err
		} else if spenderFound {
			return true, nil
		} else if output.spenderBlock != nil {
			// Output was previously found to have been spent but the block
			// containing the spending tx seems to have been invalidated.
			dcr.log.Warnf("Block %s found to contain spender for output %s has been invalidated.", tx.block.hash, op)
			output.spenderBlock = nil
		}

		// This tx output is not known to be spent as of last search (if any).
		// Scan blocks from the lastScannedBlock (if there was a previous scan)
		// or from the block containing the output to attempt finding the spender
		// of this output. Use mainChainAncestor to ensure that scanning starts
		// from a mainchain block in the event that either the output block or
		// the lastScannedBlock have been re-orged out of the mainchain.
		startBlock := new(block)
		if output.lastScannedBlock == nil {
			startBlock.hash, startBlock.height, err = dcr.mainChainAncestor(ctx, txBlock.hash)
		} else {
			startBlock.hash, startBlock.height, err = dcr.mainChainAncestor(ctx, output.lastScannedBlock)
		}
		if err != nil {
			return false, err
		}

		// Search for this output's spender in the blocks between startBlock and
		// the current best block.
		spenderTx, stopBlockHash, err := dcr.findTxOutSpender(ctx, op, outputPkScript, startBlock)
		if stopBlockHash != nil { // might be nil if the search never scanned a block
			output.lastScannedBlock = stopBlockHash
		}
		if err != nil {
			return false, err
		}
		spent := spenderTx != nil
		if spent {
			spenderBlockHash, err := chainhash.NewHashFromStr(spenderTx.BlockHash)
			if err != nil {
				return false, err
			}
			output.spenderBlock = &block{hash: spenderBlockHash, height: spenderTx.BlockHeight}
		}
		return spent, nil
	}()
	if err != nil {
		return nil, 0, false, 0, fmt.Errorf("unable to check if output %s is spent: %v", op, err)
	}

	bestBlockHeight := dcr.cachedBestBlock().height
	confs := uint32(bestBlockHeight - txBlock.height + 1)
	return txOut, confs, isSpent, txTree, nil
}

// externalTxBlock returns the mainchain block containing the provided tx, if
// it is known. If the tx block is yet unknown or has been re-orged out of the
// mainchain AND if tryFindTxBlock is true, this method attempts to find the
// block containing the provided tx by scanning block filters from the current
// best block down to the block just before earliestTxTime or the block that was
// last scanned, if there was a previous scan. If the tx block is found, the
// block hash, height and the tx outputs details are cached.
// Requires the tx.scanMtx to be locked for write.
func (dcr *ExchangeWallet) externalTxBlock(ctx context.Context, tx *externalTx, tryFindTxBlock bool, earliestTxTime time.Time) (*block, error) {
	txBlockFound, err := dcr.isMainchainBlock(ctx, tx.block)
	if err != nil {
		return nil, err
	} else if txBlockFound {
		return tx.block, nil
	} else if tx.block != nil {
		// Tx block was previously set but seems to have been invalidated.
		// Log a warning(?) and clear the tx tree, outputs and block info
		// fields that must have been previously set.
		dcr.log.Warnf("Block %s found to contain tx %s has been invalidated.", tx.block.hash, tx.hash)
		tx.block = nil
		tx.tree = -1
		tx.outputs = nil
	}

	// Tx block is currently unknown. Return if the caller does not want
	// to start a search for the block.
	if !tryFindTxBlock {
		return nil, nil
	}

	// Start a new search for this tx's block using the associated scripts.
	tx.mtx.RLock()
	txScripts := tx.pkScripts
	tx.mtx.RUnlock()

	// Scan block filters in reverse from the current best block to the last
	// scanned block. If the last scanned block has been re-orged out of the
	// mainchain, scan back to the mainchain ancestor of the lastScannedBlock.
	var lastScannedBlock *block
	if tx.lastScannedBlock != nil {
		stopBlockHash, stopBlockHeight, err := dcr.mainChainAncestor(ctx, tx.lastScannedBlock)
		if err != nil {
			return nil, fmt.Errorf("error looking up mainchain ancestor for block %s", err)
		}
		tx.lastScannedBlock = stopBlockHash
		lastScannedBlock = &block{hash: stopBlockHash, height: stopBlockHeight}
	}

	// Run cfilters scan in reverse from best block to lastScannedBlock or
	// to block just before earliestTxTime.
	currentTip := dcr.cachedBestBlock()
	if lastScannedBlock != nil {
		dcr.log.Debugf("Searching for tx %s in blocks %d (%s) to %d (%s).", tx.hash,
			currentTip.height, currentTip.hash, lastScannedBlock.height, lastScannedBlock.hash)
	} else {
		dcr.log.Debugf("Searching for tx %s in blocks between block %d (%s) to the block just before %s.",
			tx.hash, currentTip.height, currentTip.hash, earliestTxTime)
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

	for {
		// Abort the search if we've scanned blocks from the tip back to the
		// block we scanned last or the block just before earliestTxTime.
		if iHeight == 0 {
			return scanCompletedWithoutResults()
		}
		if lastScannedBlock != nil && iHeight <= lastScannedBlock.height {
			return scanCompletedWithoutResults()
		}
		iBlock, err := dcr.wallet.GetBlockHeaderVerbose(dcr.ctx, iHash)
		if err != nil {
			return nil, fmt.Errorf("getblockheader error for block %s: %w", iHash, translateRPCCancelErr(err))
		}
		if iBlock.Time <= earliestTxTime.Unix() {
			return scanCompletedWithoutResults()
		}

		// Check if this block has the tx we're looking for.
		blockFilter, err := dcr.getBlockFilterV2(ctx, iHash)
		if err != nil {
			return nil, err
		}
		if blockFilter.MatchAny(txScripts) {
			dcr.log.Debugf("Block %d (%s) likely contains tx %s. Confirming.", iHeight, iHash, tx.hash)
			blk, err := dcr.getBlock(ctx, iHash, true)
			if err != nil {
				return nil, err
			}
			blockTxs := append(blk.RawTx, blk.RawSTx...)
			for i := range blockTxs {
				blkTx := &blockTxs[i]
				if blkTx.Txid != tx.hash.String() {
					continue // check next block tx
				}

				dcr.log.Debugf("Found mined tx %s in block %d (%s).", tx.hash, iHeight, iHash)
				msgTx, err := msgTxFromHex(blkTx.Hex)
				if err != nil {
					return nil, fmt.Errorf("invalid hex for tx %s: %v", tx.hash, err)
				}
				tx.block = &block{hash: iHash, height: iHeight}
				tx.tree = determineTxTree(msgTx)
				tx.outputs = make([]*externalTxOutput, len(blkTx.Vout))
				for i := range blkTx.Vout {
					blkTxOut := &blkTx.Vout[i]
					tx.outputs[i] = &externalTxOutput{
						outPoint:        newOutPoint(tx.hash, blkTxOut.N),
						value:           blkTxOut.Value,
						pkScriptHex:     blkTxOut.ScriptPubKey.Hex,
						pkScriptVersion: blkTxOut.ScriptPubKey.Version,
					}
				}
				return tx.block, nil
			}
			dcr.log.Debugf("Block %d (%s) does NOT contain tx %s.", iHeight, iHash, tx.hash)
		}

		// Block does not include the tx, check the previous block.
		iHeight--
		iHash, err = chainhash.NewHashFromStr(iBlock.PreviousHash)
		if err != nil {
			return nil, fmt.Errorf("error decoding previous hash %s for block %s: %w",
				iBlock.PreviousHash, iHash.String(), err)
		}
		continue
	}
}

// findTxOutSpender attempts to find and return the tx that spends the provided
// output by matching the provided outputPkScript against the block filters of
// the mainchain blocks between the provided startBlock and the current best
// block.
// If no tx is found to spend the provided output, the hash of the block that
// was last checked is returned along with any error that may have occurred
// during the search.
func (dcr *ExchangeWallet) findTxOutSpender(ctx context.Context, op outPoint, outputPkScript []byte, startBlock *block) (*chainjson.TxRawResult, *chainhash.Hash, error) {
	bestBlock := dcr.cachedBestBlock()
	dcr.log.Debugf("Searching if output %s is spent in blocks %d (%s) to %d (%s) using pkScript %x.",
		op, startBlock.height, startBlock.hash, bestBlock.height, bestBlock.hash, outputPkScript)

	iHeight := startBlock.height
	iHash := startBlock.hash
	for {
		blockFilter, err := dcr.getBlockFilterV2(ctx, iHash)
		if err != nil {
			return nil, nil, err
		}

		if blockFilter.Match(outputPkScript) {
			dcr.log.Debugf("Output %s is likely spent in block %d (%s). Confirming.",
				op, iHeight, iHash)
			blk, err := dcr.getBlock(ctx, iHash, true)
			if err != nil {
				return nil, iHash, err
			}
			blockTxs := append(blk.RawTx, blk.RawSTx...)
			for i := range blockTxs {
				blkTx := &blockTxs[i]
				if txSpendsOutput(blkTx, op) {
					dcr.log.Debugf("Found spender for output %s in block %d (%s), spender tx hash %s.",
						op, iHeight, iHash, blkTx.Txid)
					return blkTx, iHash, nil
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
			return nil, iHash, translateRPCCancelErr(err)
		}
		iHash = nextHash
	}

	dcr.log.Debugf("Output %s is NOT spent in blocks %d (%s) to %d (%s).",
		op, startBlock.height, startBlock.hash, bestBlock.height, bestBlock.hash)
	return nil, bestBlock.hash, nil // scanned up to best block, no spender found
}

// txSpendsOutput returns true if the passed tx has an input that spends the
// specified output.
func txSpendsOutput(tx *chainjson.TxRawResult, op outPoint) bool {
	if tx.Txid == op.txHash.String() {
		return false // no need to check inputs if this tx is the same tx that pays to the specified op
	}
	for i := range tx.Vin {
		input := &tx.Vin[i]
		if input.Vout == op.vout && input.Txid == op.txHash.String() {
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

func (dcr *ExchangeWallet) getBlockFilterV2(ctx context.Context, blockHash *chainhash.Hash) (*blockFilter, error) {
	bf, key, err := dcr.wallet.BlockCFilter(ctx, blockHash)
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

func parseAmountAndScript(amount float64, pkScriptHex string) (int64, []byte, error) {
	amt, err := dcrutil.NewAmount(amount)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid amount: %v", err)
	}
	pkScript, err := hex.DecodeString(pkScriptHex)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid pkScript: %v", err)
	}
	return int64(amt), pkScript, nil
}
