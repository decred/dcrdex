// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// The cachedBlock structure should hold a minimal amount of information about a
// block needed to verify UTXO validity.
type cachedBlock struct {
	hash     chainhash.Hash
	height   uint32
	orphaned bool
}

// The blockCache caches block information to prevent repeated calls to
// rpcclient.GetblockVerbose.
type blockCache struct {
	mtx       sync.RWMutex
	blocks    map[chainhash.Hash]*cachedBlock
	mainchain map[uint32]*cachedBlock
	best      cachedBlock
}

// Constructor for a blockCache.
func newBlockCache() *blockCache {
	return &blockCache{
		blocks:    make(map[chainhash.Hash]*cachedBlock),
		mainchain: make(map[uint32]*cachedBlock),
	}
}

// Getter for a block by it's hash.
func (cache *blockCache) block(h *chainhash.Hash) (*cachedBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.blocks[*h]
	return blk, found
}

// Getter for a mainchain block by its height. This method does not attempt
// to load the block from the blockchain if it is not found. If that is required
// use (*Backend).getMainchaincachedBlock.
func (cache *blockCache) atHeight(height uint32) (*cachedBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	blk, found := cache.mainchain[height]
	return blk, found
}

// Add a block to the blockCache. This method will translate the RPC result
// to a cachedBlock, returning the cachedBlock. If the block is not orphaned, it
// will be added to the mainchain.
func (cache *blockCache) add(block *btcjson.GetBlockVerboseResult) (*cachedBlock, error) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	hash, err := chainhash.NewHashFromStr(block.Hash)
	if err != nil {
		return nil, fmt.Errorf("error decoding block hash %s: %w", block.Hash, err)
	}
	blk := &cachedBlock{
		hash:     *hash,
		height:   uint32(block.Height),
		orphaned: block.Confirmations == -1,
	}
	cache.blocks[*hash] = blk

	// Orphaned blocks will have -1 confirmations. Don't add them to mainchain.
	if !blk.orphaned {
		cache.mainchain[uint32(block.Height)] = blk
		if block.Height >= int64(cache.best.height) {
			cache.best.height = uint32(block.Height)
			cache.best.hash = *hash
			cache.best.orphaned = false // should not be needed, but be safe
		}
	}
	return blk, nil
}

// Get the best known block height in the blockCache.
func (cache *blockCache) tipHeight() uint32 {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best.height
}

// Get the best known block hash in the blockCache.
func (cache *blockCache) tipHash() chainhash.Hash {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best.hash
}

// Get the best known block height in the blockCache.
func (cache *blockCache) tip() cachedBlock {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.best
}

// Trigger a reorg, setting any blocks at or above the provided height as
// orphaned and removing them from mainchain, but not the blocks map. reorg
// clears the best block, so should always be followed with the addition of a
// new mainchain block.
func (cache *blockCache) reorg(from int64) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	if from < 0 {
		return
	}
	for height := uint32(from); height <= cache.best.height; height++ {
		block, found := cache.mainchain[height]
		if !found {
			// OK if not found.
			continue
		}
		// Delete the block from mainchain.
		delete(cache.mainchain, block.height)
		// Store an orphaned block in the blocks cache.
		cache.blocks[block.hash] = &cachedBlock{
			hash:     block.hash,
			height:   block.height,
			orphaned: true,
		}
	}
	// Set this to a dummy block with just the height. Caller will add mainchain
	// block after call to reorg.
	cache.best = cachedBlock{height: uint32(from)}
}
