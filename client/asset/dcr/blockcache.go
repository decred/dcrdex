// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
)

// dcrBlock defines basic information about a block.
type dcrBlock struct {
	Hash         *chainhash.Hash
	Height       int64
	PreviousHash string
}

// blockCache caches block information to prevent repeated calls to
// rpcclient.GetblockVerbose.
type blockCache struct {
	mtx       sync.RWMutex
	blocks    map[chainhash.Hash]*dcrBlock
	mainchain map[int64]*chainhash.Hash
	bestBlock struct {
		hash   *chainhash.Hash
		height int64
	}
	log dex.Logger
}

func newBlockCache(log dex.Logger) *blockCache {
	return &blockCache{
		blocks:    make(map[chainhash.Hash]*dcrBlock),
		mainchain: make(map[int64]*chainhash.Hash),
		log:       log,
	}
}

// add adds a block to the BlockCache. This method will translate the RPC result
// to a *Block, returning the *Block. If the block is not orphaned, it will be
// added to the mainchain.
func (cache *blockCache) add(verboseBlk *chainjson.GetBlockVerboseResult) (*dcrBlock, error) {
	cache.mtx.Lock()
	defer cache.mtx.Unlock()

	hash, err := chainhash.NewHashFromStr(verboseBlk.Hash)
	if err != nil {
		return nil, fmt.Errorf("error decoding block hash %s: %w", verboseBlk.Hash, err)
	}

	blk := &dcrBlock{
		Hash:         hash,
		Height:       verboseBlk.Height,
		PreviousHash: verboseBlk.PreviousHash,
	}
	cache.blocks[*hash] = blk

	// Orphaned blocks will have -1 confirmations. Don't add them to mainchain.
	if verboseBlk.Confirmations > -1 {
		cache.mainchain[verboseBlk.Height] = hash
		if verboseBlk.Height > cache.bestBlock.height {
			cache.bestBlock.height = verboseBlk.Height
			cache.bestBlock.hash = hash
		}
	}

	return blk, nil
}

// tip returns the best known block hash and height for the blockCache.
func (cache *blockCache) tip() (*chainhash.Hash, int64) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	return cache.bestBlock.hash, cache.bestBlock.height
}

// mainchainHash returns the hash for the mainchain block at the specified height.
// This method does not attempt to fetch the required hash from the blockchain
// if it is not cached.
func (cache *blockCache) mainchainHash(blockHeight int64) (*chainhash.Hash, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	hash, found := cache.mainchain[blockHeight]
	return hash, found
}

// blockAt returns basic information about the block with the specified height.
// If withTxs is true, the returned block object will contain all transactions
// in the block, otherwise only the transaction IDs will be returned with the block.
func (cache *blockCache) blockAt(height int64) (*dcrBlock, bool) {
	blockHash, found := cache.mainchainHash(height)
	if !found {
		return nil, false
	}
	return cache.block(blockHash)
}

// block returns basic information about the block with the specified hash. This
// method does not attempt to fetch the required hash from the blockchain if it
// is not cached.
func (cache *blockCache) block(hash *chainhash.Hash) (*dcrBlock, bool) {
	cache.mtx.RLock()
	defer cache.mtx.RUnlock()
	block, found := cache.blocks[*hash]
	return block, found
}

// purgeMainchainBlocks deletes all blocks at and above the specified height
// from the mainchain but not the blocks map. This should be done if a reorg
// occurs on the blockchain or it is no longer certain that the blocks in the
// specified range still belong in the mainchain.
// NOTE: purgeMainchainBlocks clears the best block, so should always be followed
// with the addition of a new mainchain block which would become the best block.
func (cache *blockCache) purgeMainchainBlocks(fromHeight int64) {
	if fromHeight < 0 {
		return
	}

	cache.mtx.Lock()
	defer cache.mtx.Unlock()
	for blockHeight := fromHeight; blockHeight <= cache.bestBlock.height; blockHeight++ {
		delete(cache.mainchain, blockHeight)
	}
	cache.bestBlock.hash = nil
	cache.bestBlock.height = 0
}
