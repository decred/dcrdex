package btc

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// spendingInput is added to a filterScanResult if a spending input is found.
type spendingInput struct {
	txHash      chainhash.Hash
	vin         uint32
	blockHash   chainhash.Hash
	blockHeight uint32
}

// filterScanResult is the result from a filter scan.
type filterScanResult struct {
	// blockHash is the block that the output was found in.
	blockHash *chainhash.Hash
	// blockHeight is the height of the block that the output was found in.
	blockHeight uint32
	// txOut is the output itself.
	txOut *wire.TxOut
	// spend will be set if a spending input is found.
	spend *spendingInput
	// checkpoint is used to track the last block scanned so that future scans
	// can skip scanned blocks.
	checkpoint chainhash.Hash
}

// hashEntry stores a chainhash.Hash with a last-access time that can be used
// for cache maintenance.
type hashEntry struct {
	hash       chainhash.Hash
	lastAccess time.Time
}

// scanCheckpoint is a cached, incomplete filterScanResult. When another scan
// is requested for an outpoint with a cached *scanCheckpoint, the scan can
// pick up where it left off.
type scanCheckpoint struct {
	res        *filterScanResult
	lastAccess time.Time
}

// blockInfoReader defines methods for retrieving block information.
type blockInfoReader interface {
	getBlockHash(blockHeight int64) (*chainhash.Hash, error)
	getBlockHeight(*chainhash.Hash) (int32, error)
	getBlockHeaderVerbose(blockHash *chainhash.Hash) (*wire.BlockHeader, error)
	getBlock(h chainhash.Hash) (*wire.MsgBlock, error)
	getChainHeight() (int32, error)
	matchPkScript(blockHash *chainhash.Hash, scripts [][]byte) (bool, error)
}

// BlockFiltersScanner is a utility tool for searching for an output and its
// spending input by scanning BIP158 compact filters. Used by SPV wallets to
// locate non-wallet transactions.
type BlockFiltersScanner struct {
	blockInfoReader
	log             dex.Logger
	cacheExpiration time.Duration

	checkpointMtx sync.Mutex
	checkpoints   map[OutPoint]*scanCheckpoint

	txBlocksMtx sync.Mutex
	txBlocks    map[chainhash.Hash]*hashEntry
}

// NewBlockFiltersScanner creates a BlockFiltersScanner.
func NewBlockFiltersScanner(blkInfoRdr blockInfoReader, log dex.Logger) *BlockFiltersScanner {
	return &BlockFiltersScanner{
		blockInfoReader: blkInfoRdr,
		log:             log,
		cacheExpiration: time.Hour * 2,
		txBlocks:        make(map[chainhash.Hash]*hashEntry),
		checkpoints:     make(map[OutPoint]*scanCheckpoint),
	}
}

// storeTxBlock stores the block hash for the tx in the cache.
func (s *BlockFiltersScanner) storeTxBlock(txHash, blockHash chainhash.Hash) {
	s.txBlocksMtx.Lock()
	defer s.txBlocksMtx.Unlock()
	s.txBlocks[txHash] = &hashEntry{
		hash:       blockHash,
		lastAccess: time.Now(),
	}
}

// txBlock attempts to retrieve the block hash for the tx from the cache.
func (s *BlockFiltersScanner) txBlock(txHash chainhash.Hash) (chainhash.Hash, bool) {
	s.txBlocksMtx.Lock()
	defer s.txBlocksMtx.Unlock()
	entry, found := s.txBlocks[txHash]
	if !found {
		return chainhash.Hash{}, false
	}
	entry.lastAccess = time.Now()
	return entry.hash, true
}

// cacheCheckpoint caches a *filterScanResult so that future scans can be
// skipped or shortened.
func (s *BlockFiltersScanner) cacheCheckpoint(txHash *chainhash.Hash, vout uint32, res *filterScanResult) {
	if res.spend != nil && res.blockHash == nil {
		// Probably set the start time too late. Don't cache anything
		return
	}
	s.checkpointMtx.Lock()
	defer s.checkpointMtx.Unlock()
	s.checkpoints[NewOutPoint(txHash, vout)] = &scanCheckpoint{
		res:        res,
		lastAccess: time.Now(),
	}
}

// unvalidatedCheckpoint returns any cached *filterScanResult for the outpoint.
func (s *BlockFiltersScanner) unvalidatedCheckpoint(txHash *chainhash.Hash, vout uint32) *filterScanResult {
	s.checkpointMtx.Lock()
	defer s.checkpointMtx.Unlock()
	check, found := s.checkpoints[NewOutPoint(txHash, vout)]
	if !found {
		return nil
	}
	check.lastAccess = time.Now()
	res := *check.res
	return &res
}

// checkpoint returns a filterScanResult and the checkpoint block hash. If a
// result is found with an orphaned checkpoint block hash, it is cleared from
// the cache and not returned.
func (s *BlockFiltersScanner) checkpoint(txHash *chainhash.Hash, vout uint32) *filterScanResult {
	res := s.unvalidatedCheckpoint(txHash, vout)
	if res == nil {
		return nil
	}
	if !s.blockIsMainchain(&res.checkpoint, -1) {
		// reorg detected, abandon the checkpoint.
		s.log.Debugf("abandoning checkpoint %s because checkpoint block %q is orphaned",
			NewOutPoint(txHash, vout), res.checkpoint)
		s.checkpointMtx.Lock()
		delete(s.checkpoints, NewOutPoint(txHash, vout))
		s.checkpointMtx.Unlock()
		return nil
	}
	return res
}

// CleanCaches removes cached tx and filter scan results that have not been
// accessed since the specified duration. Should be called periodically from a
// goroutine.
func (s *BlockFiltersScanner) CleanCaches(expiration time.Duration) {
	s.txBlocksMtx.Lock()
	for txHash, entry := range s.txBlocks {
		if time.Since(entry.lastAccess) > expiration {
			delete(s.txBlocks, txHash)
		}
	}
	s.txBlocksMtx.Unlock()

	s.checkpointMtx.Lock()
	for outPt, check := range s.checkpoints {
		if time.Since(check.lastAccess) > expiration {
			delete(s.checkpoints, outPt)
		}
	}
	s.checkpointMtx.Unlock()
}

// blockForStoredTx looks for a block hash in the txBlocks index.
func (s *BlockFiltersScanner) blockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32, error) {
	// Check if we know the block hash for the tx.
	blockHash, found := s.txBlock(*txHash)
	if !found {
		return nil, 0, nil
	}
	// Check that the block is still mainchain.
	blockHeight, err := s.getBlockHeight(&blockHash)
	if err != nil {
		s.log.Errorf("Error retrieving block height for hash %s: %v", blockHash, err)
		return nil, 0, err
	}
	return &blockHash, blockHeight, nil
}

// blockIsMainchain will be true if the blockHash is that of a mainchain block.
func (s *BlockFiltersScanner) blockIsMainchain(blockHash *chainhash.Hash, blockHeight int32) bool {
	if blockHeight < 0 {
		var err error
		blockHeight, err = s.getBlockHeight(blockHash)
		if err != nil {
			s.log.Errorf("Error getting block height for hash %s", blockHash)
			return false
		}
	}
	checkHash, err := s.getBlockHash(int64(blockHeight))
	if err != nil {
		s.log.Errorf("Error retrieving block hash for height %d", blockHeight)
		return false
	}

	return *checkHash == *blockHash
}

// mainchainBlockForStoredTx gets the block hash and height for the transaction
// IFF an entry has been stored in the txBlocks index.
func (s *BlockFiltersScanner) mainchainBlockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32) {
	// Check that the block is still mainchain.
	blockHash, blockHeight, err := s.blockForStoredTx(txHash)
	if err != nil {
		s.log.Errorf("Error retrieving mainchain block height for hash %s", blockHash)
		return nil, 0
	}
	if blockHash == nil {
		return nil, 0
	}
	if !s.blockIsMainchain(blockHash, blockHeight) {
		return nil, 0
	}
	return blockHash, blockHeight
}

// findBlockForTime locates a good start block so that a search beginning at the
// returned block has a very low likelihood of missing any blocks that have time
// > matchTime. This is done by performing a binary search (sort.Search) to find
// a block with a block time maxFutureBlockTime before matchTime. To ensure
// we also accommodate the median-block time rule and aren't missing anything
// due to out of sequence block times we use an unsophisticated algorithm of
// choosing the first block in an 11 block window with no times >= matchTime.
func (s *BlockFiltersScanner) findBlockForTime(matchTime time.Time) (int32, error) {
	offsetTime := matchTime.Add(-maxFutureBlockTime)

	bestHeight, err := s.getChainHeight()
	if err != nil {
		return 0, fmt.Errorf("getChainHeight error: %v", err)
	}

	getBlockTimeForHeight := func(height int32) (time.Time, error) {
		hash, err := s.getBlockHash(int64(height))
		if err != nil {
			return time.Time{}, err
		}
		header, err := s.getBlockHeaderVerbose(hash)
		if err != nil {
			return time.Time{}, err
		}
		return header.Timestamp, nil
	}

	iHeight := sort.Search(int(bestHeight), func(h int) bool {
		var iTime time.Time
		iTime, err = getBlockTimeForHeight(int32(h))
		if err != nil {
			return true
		}
		return iTime.After(offsetTime)
	})
	if err != nil {
		return 0, fmt.Errorf("binary search error finding best block for time %q: %w", matchTime, err)
	}

	// We're actually breaking an assumption of sort.Search here because block
	// times aren't always monotonically increasing. This won't matter though as
	// long as there are not > medianTimeBlocks blocks with inverted time order.
	var count int
	var iTime time.Time
	for iHeight > 0 {
		iTime, err = getBlockTimeForHeight(int32(iHeight))
		if err != nil {
			return 0, fmt.Errorf("getBlockTimeForHeight error: %w", err)
		}
		if iTime.Before(offsetTime) {
			count++
			if count == medianTimeBlocks {
				return int32(iHeight), nil
			}
		} else {
			count = 0
		}
		iHeight--
	}
	return 0, nil
}

// ScanFilters enables searching for an output and its spending input by
// scanning BIP158 compact filters. Caller should supply either blockHash or
// startTime. blockHash takes precedence. If blockHash is supplied, the scan
// will start at that block and continue to the current blockchain tip, or until
// both the output and a spending transaction is found. if startTime is
// supplied, and the blockHash for the output is not known to the wallet, a
// candidate block will be selected with findBlockTime.
func (s *BlockFiltersScanner) ScanFilters(txHash *chainhash.Hash, vout uint32, pkScript []byte, walletTip int32, startTime time.Time, blockHash *chainhash.Hash) (*filterScanResult, error) {
	// TODO: Check that any blockHash supplied is not orphaned?

	// Check if we know the block hash for the tx.
	var limitHeight int32
	// See if we have a checkpoint to use.
	checkPt := s.checkpoint(txHash, vout)
	if checkPt != nil {
		if checkPt.blockHash != nil && checkPt.spend != nil {
			// We already have the output and the spending input, and
			// checkpointBlock already verified it's still mainchain.
			return checkPt, nil
		}
		height, err := s.getBlockHeight(&checkPt.checkpoint)
		if err != nil {
			return nil, fmt.Errorf("getBlockHeight error: %w", err)
		}
		limitHeight = height + 1
	} else if blockHash == nil {
		// No checkpoint and no block hash. Gotta guess based on time.
		blockHash, limitHeight = s.mainchainBlockForStoredTx(txHash)
		if blockHash == nil {
			var err error
			limitHeight, err = s.findBlockForTime(startTime)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// No checkpoint, but user supplied a block hash.
		var err error
		limitHeight, err = s.getBlockHeight(blockHash)
		if err != nil {
			return nil, fmt.Errorf("error getting height for supplied block hash %s", blockHash)
		}
	}

	s.log.Debugf("Performing cfilters scan for %v:%d from height %d", txHash, vout, limitHeight)

	// Do a filter scan.
	utxo, err := s.filterScanFromHeight(*txHash, vout, pkScript, walletTip, limitHeight, checkPt)
	if err != nil {
		return nil, fmt.Errorf("filterScanFromHeight error: %w", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}

	// If we found a block, let's store a reference in our local database so we
	// can maybe bypass a long search next time.
	if utxo.blockHash != nil {
		s.log.Debugf("cfilters scan SUCCEEDED for %v:%d. block hash: %v, spent: %v",
			txHash, vout, utxo.blockHash, utxo.spend != nil)
		s.storeTxBlock(*txHash, *utxo.blockHash)
	}

	s.cacheCheckpoint(txHash, vout, utxo)

	return utxo, nil
}

// filterScanFromHeight scans BIP158 filters beginning at the specified block
// height until the tip, or until a spending transaction is found.
func (s *BlockFiltersScanner) filterScanFromHeight(txHash chainhash.Hash, vout uint32, pkScript []byte, walletTip int32, startBlockHeight int32, checkPt *filterScanResult) (*filterScanResult, error) {
	res := checkPt
	if res == nil {
		res = new(filterScanResult)
	}

search:
	for height := startBlockHeight; height <= walletTip; height++ {
		if res.spend != nil && res.blockHash == nil {
			s.log.Warnf("A spending input (%s) was found during the scan but the output (%s) "+
				"itself wasn't found. Was the startBlockHeight early enough?",
				NewOutPoint(&res.spend.txHash, res.spend.vin),
				NewOutPoint(&txHash, vout),
			)
			return res, nil
		}
		blockHash, err := s.getBlockHash(int64(height))
		if err != nil {
			return nil, fmt.Errorf("error getting block hash for height %d: %w", height, err)
		}
		matched, err := s.matchPkScript(blockHash, [][]byte{pkScript})
		if err != nil {
			return nil, fmt.Errorf("matchPkScript error: %w", err)
		}

		res.checkpoint = *blockHash
		if !matched {
			continue search
		}
		// Pull the block.
		s.log.Tracef("Block %v matched pkScript for output %v:%d. Pulling the block...",
			blockHash, txHash, vout)
		msgBlock, err := s.getBlock(*blockHash)
		if err != nil {
			return nil, fmt.Errorf("GetBlock error: %v", err)
		}

		// Scan every transaction.
	nextTx:
		for _, tx := range msgBlock.Transactions {
			// Look for a spending input.
			if res.spend == nil {
				for vin, txIn := range tx.TxIn {
					prevOut := &txIn.PreviousOutPoint
					if prevOut.Hash == txHash && prevOut.Index == vout {
						res.spend = &spendingInput{
							txHash:      tx.TxHash(),
							vin:         uint32(vin),
							blockHash:   *blockHash,
							blockHeight: uint32(height),
						}
						s.log.Tracef("Found txn %v spending %v in block %v (%d)", res.spend.txHash,
							txHash, res.spend.blockHash, res.spend.blockHeight)
						if res.blockHash != nil {
							break search
						}
						// The output could still be in this block, just not
						// in this transaction.
						continue nextTx
					}
				}
			}
			// Only check for the output if this is the right transaction.
			if res.blockHash != nil || tx.TxHash() != txHash {
				continue nextTx
			}
			for _, txOut := range tx.TxOut {
				if bytes.Equal(txOut.PkScript, pkScript) {
					res.blockHash = blockHash
					res.blockHeight = uint32(height)
					res.txOut = txOut
					s.log.Tracef("Found txn %v in block %v (%d)", txHash, res.blockHash, height)
					if res.spend != nil {
						break search
					}
					// Keep looking for the spending transaction.
					continue nextTx
				}
			}
		}
	}
	return res, nil
}
