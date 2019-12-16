// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
)

// The dcrBlock structure should hold a minimal amount of information about a
// block needed to verify UTXO validity.
type dcrBlock struct {
	hash     chainhash.Hash
	height   uint32
	orphaned bool
	vote     bool // stakeholder vote result for the previous block
}

// The blockCache caches block information to prevent repeated calls to
// rpcclient.GetblockVerbose.
type blockCache struct {
	mtx       sync.RWMutex
	blocks    map[chainhash.Hash]*dcrBlock
	mainchain map[uint32]*dcrBlock
	best      dcrBlock
	log       dex.Logger
}

// Constructor for a blockCache.
func newBlockCache(logger dex.Logger) *blockCache {
	return &blockCache{
		blocks:    make(map[chainhash.Hash]*dcrBlock),
		mainchain: make(map[uint32]*dcrBlock),
		log:       logger,
	}
}

// Getter for a block by it's hash.
func (cache *blockCache) block(h *chainhash.Hash) (*dcrBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.blocks[*h]
	return blk, found
}

// Getter for a mainchain block by its height. This method does not attempt
// to load the block from the blockchain if it is not found. If that is required
// use (*DCRBackend).getMainchainDcrBlock.
func (cache *blockCache) atHeight(height uint32) (*dcrBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.mainchain[height]
	return blk, found
}

// Add a block to the blockCache. This method will translate the RPC result
// to a dcrBlock, returning the dcrBlock. If the block is not orphaned, it will
// be added to the mainchain.
func (cache *blockCache) add(block *chainjson.GetBlockVerboseResult) (*dcrBlock, error) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	hash, err := chainhash.NewHashFromStr(block.Hash)
	if err != nil {
		return nil, fmt.Errorf("error decoding block hash %s: %v", block.Hash, err)
	}
	blk := &dcrBlock{
		hash:     *hash,
		height:   uint32(block.Height),
		orphaned: block.Confirmations == -1,
		vote:     block.VoteBits&1 != 0,
	}
	cache.blocks[*hash] = blk

	// Orphaned blocks will have -1 confirmations. Don't add them to mainchain.
	if block.Confirmations > -1 {
		cache.mainchain[uint32(block.Height)] = blk
		if block.Height > int64(cache.best.height) {
			cache.best.height = uint32(block.Height)
			cache.best.hash = *hash
		}
	}
	return blk, nil
}

// Get the best known block height for the blockCache.
func (cache *blockCache) tipHeight() uint32 {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	return cache.best.height
}

// Get the best known block hash in the blockCache.
func (cache *blockCache) tipHash() chainhash.Hash {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best.hash
}

// Trigger a reorg, setting any blocks at or above the provided height as
// orphaned and removing them from mainchain, but not the blocks map. reorg
// clears the best block, so should always be followed with the addition of a
// new mainchain block.
func (cache *blockCache) reorg(newTip *chainjson.GetBlockVerboseResult) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	if newTip.Height < 0 {
		return
	}
	for height := uint32(newTip.Height); height <= cache.best.height; height++ {
		block, found := cache.mainchain[height]
		if !found {
			cache.log.Errorf("reorg block not found on mainchain at height %d for a reorg from %d to %d", height, newTip, cache.best.height)
			continue
		}
		// Delete the block from mainchain.
		delete(cache.mainchain, block.height)
		// Store an orphaned block in the blocks cache.
		cache.blocks[block.hash] = &dcrBlock{
			hash:     block.hash,
			height:   block.height,
			orphaned: true,
			vote:     block.vote,
		}
	}
	// Set this to a zero block so that the new block will replace it even if
	// it is of the same height as the previous best block.
	cache.best = dcrBlock{}
}
