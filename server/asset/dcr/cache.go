// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package dcr

import (
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrdex/server/asset"
)

// The dcrBlock structure should hold a minimal amount of information about a
// block needed to verify UTXO validity.
type dcrBlock struct {
	hash     chainhash.Hash
	height   int64
	orphaned bool
	vote     bool // stakeholder vote result for the previous block
}

// The blockCache caches block information to prevent repeated calls to
// rpcclient.GetblockVerbose.
type blockCache struct {
	mtx       sync.RWMutex
	blocks    map[chainhash.Hash]*dcrBlock
	mainchain map[uint32]*dcrBlock
	tip       uint32
	log       asset.Logger
}

// Constructor for a blockCache.
func newBlockCache(logger asset.Logger) *blockCache {
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
// use (*dcrBackend).getMainchainDcrBlock.
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
		height:   block.Height,
		orphaned: block.Confirmations == -1,
		vote:     block.VoteBits&1 != 0,
	}
	cache.blocks[*hash] = blk

	// Orphaned blocks will have -1 confirmations. Don't add them to mainchain.
	if block.Confirmations > -1 {
		cache.mainchain[uint32(block.Height)] = blk
		if uint32(block.Height) > cache.tip {
			cache.tip = uint32(block.Height)
		}
	}
	return blk, nil
}

// Get the best known block height for the blockCache.
func (cache *blockCache) tipHeight() uint32 {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	return cache.tip
}

// Trigger a reorg, setting any blocks at or above the provided height as
// orphaned and removing them from mainchain, but not the blocks map.
func (cache *blockCache) reorg(newTip int64) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	if newTip < 0 {
		return
	}
	for height := uint32(newTip); height <= cache.tip; height++ {
		block, found := cache.mainchain[height]
		if !found {
			cache.log.Errorf("reorg block not found on mainchain at height %d for a reorg from %d to %d", height, newTip, cache.tip)
			continue
		}
		// Delete the block from mainchain.
		delete(cache.mainchain, uint32(block.height))
		// Store an orphaned block in the blocks cache.
		cache.blocks[block.hash] = &dcrBlock{
			hash:     block.hash,
			height:   block.height,
			orphaned: true,
			vote:     block.vote,
		}
	}
	cache.tip = uint32(newTip)
}
