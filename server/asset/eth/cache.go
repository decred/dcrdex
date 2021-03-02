// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"sync"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// The ethBlock structure should hold a minimal amount of information about a
// block.
type ethBlock struct {
	hash     common.Hash
	height   uint64
	orphaned bool
}

// The blockCache caches block information to prevent repeated calls to
// rpcclient.GetblockVerbose.
type blockCache struct {
	mtx       sync.RWMutex
	blocks    map[common.Hash]*ethBlock
	mainchain map[uint64]*ethBlock
	best      ethBlock
	log       dex.Logger
}

// Constructor for a blockCache.
func newBlockCache(logger dex.Logger) *blockCache {
	return &blockCache{
		blocks:    make(map[common.Hash]*ethBlock),
		mainchain: make(map[uint64]*ethBlock),
		log:       logger,
	}
}

// Getter for a block by it's hash.
func (cache *blockCache) block(h common.Hash) (*ethBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.blocks[h]
	return blk, found
}

// Getter for a mainchain block by its height. This method does not attempt
// to load the block from the blockchain if it is not found.
func (cache *blockCache) atHeight(height uint64) (*ethBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.mainchain[height]
	return blk, found
}

// Add a block to the blockCache. This method will translate the RPC result
// to a ethBlock, returning the ethBlock. If the block is not orphaned, it will
// be added to the mainchain.
func (cache *blockCache) add(block *types.Block) (*ethBlock, error) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	// TODO: Fix this.
	orphaned := false
	height, hash := block.NumberU64(), block.Hash()
	blk := &ethBlock{
		hash:     hash,
		height:   height,
		orphaned: orphaned,
	}
	cache.blocks[hash] = blk

	if !orphaned {
		cache.mainchain[height] = blk
		if height > cache.best.height {
			cache.best.height = height
			cache.best.hash = hash
		}
	}
	return blk, nil
}

// Get the best known block height for the blockCache.
func (cache *blockCache) tipHeight() uint64 {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	return cache.best.height
}

// Get the best known block hash in the blockCache.
func (cache *blockCache) tipHash() common.Hash {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best.hash
}

// Get the best known block height in the blockCache.
func (cache *blockCache) tip() ethBlock {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best
}

// Trigger a reorg, setting any blocks at or above the provided height as
// orphaned and removing them from mainchain, but not the blocks map. reorg
// clears the best block, so should always be followed with the addition of a
// new mainchain block.
func (cache *blockCache) reorg(from uint64) {
	if from < 0 {
		return
	}
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	for height := from; height <= cache.best.height; height++ {
		block, found := cache.mainchain[height]
		if !found {
			cache.log.Errorf("reorg block not found on mainchain at height %d for a reorg from %d to %d", height, from, cache.best.height)
			continue
		}
		// Delete the block from mainchain.
		delete(cache.mainchain, block.height)
		// Store an orphaned block in the blocks cache.
		cache.blocks[block.hash] = &ethBlock{
			hash:     block.hash,
			height:   block.height,
			orphaned: true,
		}
	}
	// Set this to a zero block so that the new block will replace it even if
	// it is of the same height as the previous best block.
	cache.best = ethBlock{}
}
