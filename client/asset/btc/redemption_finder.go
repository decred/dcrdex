package btc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// FindRedemptionReq represents a request to find a contract's redemption,
// which is submitted to the RedemptionFinder.
type FindRedemptionReq struct {
	outPt        OutPoint
	blockHash    *chainhash.Hash
	blockHeight  int32
	resultChan   chan *FindRedemptionResult
	pkScript     []byte
	contractHash []byte
}

func (req *FindRedemptionReq) fail(s string, a ...any) {
	req.sendResult(&FindRedemptionResult{err: fmt.Errorf(s, a...)})
}

func (req *FindRedemptionReq) success(res *FindRedemptionResult) {
	req.sendResult(res)
}

func (req *FindRedemptionReq) sendResult(res *FindRedemptionResult) {
	select {
	case req.resultChan <- res:
	default:
		// In-case two separate threads find a result.
	}
}

func (req *FindRedemptionReq) PkScript() []byte {
	return req.pkScript
}

// FindRedemptionResult models the result of a find redemption attempt.
type FindRedemptionResult struct {
	redemptionCoinID dex.Bytes
	secret           dex.Bytes
	err              error
}

// RedemptionFinder searches on-chain for the redemption of a swap transactions.
type RedemptionFinder struct {
	mtx         sync.RWMutex
	log         dex.Logger
	redemptions map[OutPoint]*FindRedemptionReq

	getWalletTransaction      func(txHash *chainhash.Hash) (*GetTransactionResult, error)
	getBlockHeight            func(*chainhash.Hash) (int32, error)
	getBlock                  func(h chainhash.Hash) (*wire.MsgBlock, error)
	getBlockHeader            func(blockHash *chainhash.Hash) (hdr *BlockHeader, mainchain bool, err error)
	hashTx                    func(*wire.MsgTx) *chainhash.Hash
	deserializeTx             func([]byte) (*wire.MsgTx, error)
	getBestBlockHeight        func() (int32, error)
	searchBlockForRedemptions func(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq, blockHash chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult)
	getBlockHash              func(blockHeight int64) (*chainhash.Hash, error)
	findRedemptionsInMempool  func(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq) (discovered map[OutPoint]*FindRedemptionResult)
}

func NewRedemptionFinder(
	log dex.Logger,
	getWalletTransaction func(txHash *chainhash.Hash) (*GetTransactionResult, error),
	getBlockHeight func(*chainhash.Hash) (int32, error),
	getBlock func(h chainhash.Hash) (*wire.MsgBlock, error),
	getBlockHeader func(blockHash *chainhash.Hash) (hdr *BlockHeader, mainchain bool, err error),
	hashTx func(*wire.MsgTx) *chainhash.Hash,
	deserializeTx func([]byte) (*wire.MsgTx, error),
	getBestBlockHeight func() (int32, error),
	searchBlockForRedemptions func(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq, blockHash chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult),
	getBlockHash func(blockHeight int64) (*chainhash.Hash, error),
	findRedemptionsInMempool func(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq) (discovered map[OutPoint]*FindRedemptionResult),
) *RedemptionFinder {
	return &RedemptionFinder{
		log:                       log,
		getWalletTransaction:      getWalletTransaction,
		getBlockHeight:            getBlockHeight,
		getBlock:                  getBlock,
		getBlockHeader:            getBlockHeader,
		hashTx:                    hashTx,
		deserializeTx:             deserializeTx,
		getBestBlockHeight:        getBestBlockHeight,
		searchBlockForRedemptions: searchBlockForRedemptions,
		getBlockHash:              getBlockHash,
		findRedemptionsInMempool:  findRedemptionsInMempool,
		redemptions:               make(map[OutPoint]*FindRedemptionReq),
	}
}

func (r *RedemptionFinder) FindRedemption(ctx context.Context, coinID dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot decode contract coin id: %w", err)
	}

	outPt := NewOutPoint(txHash, vout)

	tx, err := r.getWalletTransaction(txHash)
	if err != nil {
		return nil, nil, fmt.Errorf("error finding wallet transaction: %v", err)
	}

	txOut, err := TxOutFromTxBytes(tx.Bytes, vout, r.deserializeTx, r.hashTx)
	if err != nil {
		return nil, nil, err
	}
	pkScript := txOut.PkScript

	var blockHash *chainhash.Hash
	if tx.BlockHash != "" {
		blockHash, err = chainhash.NewHashFromStr(tx.BlockHash)
		if err != nil {
			return nil, nil, fmt.Errorf("error decoding block hash from string %q: %w",
				tx.BlockHash, err)
		}
	}

	var blockHeight int32
	if blockHash != nil {
		r.log.Infof("FindRedemption - Checking block %v for swap %v", blockHash, outPt)
		blockHeight, err = r.checkRedemptionBlockDetails(outPt, blockHash, pkScript)
		if err != nil {
			return nil, nil, fmt.Errorf("checkRedemptionBlockDetails: op %v / block %q: %w",
				outPt, tx.BlockHash, err)
		}
	}

	req := &FindRedemptionReq{
		outPt:        outPt,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   make(chan *FindRedemptionResult, 1),
		pkScript:     pkScript,
		contractHash: dexbtc.ExtractScriptHash(pkScript),
	}

	if err := r.queueFindRedemptionRequest(req); err != nil {
		return nil, nil, fmt.Errorf("queueFindRedemptionRequest error for redemption %s: %w", outPt, err)
	}

	go r.tryRedemptionRequests(ctx, nil, []*FindRedemptionReq{req})

	var result *FindRedemptionResult
	select {
	case result = <-req.resultChan:
		if result == nil {
			err = fmt.Errorf("unexpected nil result for redemption search for %s", outPt)
		}
	case <-ctx.Done():
		err = fmt.Errorf("context cancelled during search for redemption for %s", outPt)
	}

	// If this contract is still tracked, remove from the queue to prevent
	// further redemption search attempts for this contract.
	r.mtx.Lock()
	delete(r.redemptions, outPt)
	r.mtx.Unlock()

	// result would be nil if ctx is canceled or the result channel is closed
	// without data, which would happen if the redemption search is aborted when
	// this ExchangeWallet is shut down.
	if result != nil {
		return result.redemptionCoinID, result.secret, result.err
	}
	return nil, nil, err
}

func (r *RedemptionFinder) checkRedemptionBlockDetails(outPt OutPoint, blockHash *chainhash.Hash, pkScript []byte) (int32, error) {
	blockHeight, err := r.getBlockHeight(blockHash)
	if err != nil {
		return 0, fmt.Errorf("GetBlockHeight for redemption block %s error: %w", blockHash, err)
	}
	blk, err := r.getBlock(*blockHash)
	if err != nil {
		return 0, fmt.Errorf("error retrieving redemption block %s: %w", blockHash, err)
	}

	var tx *wire.MsgTx
out:
	for _, iTx := range blk.Transactions {
		if *r.hashTx(iTx) == outPt.TxHash {
			tx = iTx
			break out
		}
	}
	if tx == nil {
		return 0, fmt.Errorf("transaction %s not found in block %s", outPt.TxHash, blockHash)
	}
	if uint32(len(tx.TxOut)) < outPt.Vout+1 {
		return 0, fmt.Errorf("no output %d in redemption transaction %s found in block %s", outPt.Vout, outPt.TxHash, blockHash)
	}
	if !bytes.Equal(tx.TxOut[outPt.Vout].PkScript, pkScript) {
		return 0, fmt.Errorf("pubkey script mismatch for redemption at %s", outPt)
	}

	return blockHeight, nil
}

func (r *RedemptionFinder) queueFindRedemptionRequest(req *FindRedemptionReq) error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	if _, exists := r.redemptions[req.outPt]; exists {
		return fmt.Errorf("duplicate find redemption request for %s", req.outPt)
	}
	r.redemptions[req.outPt] = req
	return nil
}

// tryRedemptionRequests searches all mainchain blocks with height >= startBlock
// for redemptions.
func (r *RedemptionFinder) tryRedemptionRequests(ctx context.Context, startBlock *chainhash.Hash, reqs []*FindRedemptionReq) {
	undiscovered := make(map[OutPoint]*FindRedemptionReq, len(reqs))
	mempoolReqs := make(map[OutPoint]*FindRedemptionReq)
	for _, req := range reqs {
		// If there is no block hash yet, this request hasn't been mined, and a
		// spending tx cannot have been mined. Only check mempool.
		if req.blockHash == nil {
			mempoolReqs[req.outPt] = req
			continue
		}
		undiscovered[req.outPt] = req
	}

	epicFail := func(s string, a ...any) {
		for _, req := range reqs {
			req.fail(s, a...)
		}
	}

	// Only search up to the current tip. This does leave two unhandled
	// scenarios worth mentioning.
	//  1) A new block is mined during our search. In this case, we won't
	//     see the new block, but tryRedemptionRequests should be called again
	//     by the block monitoring loop.
	//  2) A reorg happens, and this tip becomes orphaned. In this case, the
	//     worst that can happen is that a shorter chain will replace a longer
	//     one (extremely rare). Even in that case, we'll just log the error and
	//     exit the block loop.
	tipHeight, err := r.getBestBlockHeight()
	if err != nil {
		epicFail("tryRedemptionRequests getBestBlockHeight error: %v", err)
		return
	}

	// If a startBlock is provided at a higher height, use that as the starting
	// point.
	var iHash *chainhash.Hash
	var iHeight int32
	if startBlock != nil {
		h, err := r.getBlockHeight(startBlock)
		if err != nil {
			epicFail("tryRedemptionRequests startBlock getBlockHeight error: %v", err)
			return
		}
		iHeight = h
		iHash = startBlock
	} else {
		iHeight = math.MaxInt32
		for _, req := range undiscovered {
			if req.blockHash != nil && req.blockHeight < iHeight {
				iHeight = req.blockHeight
				iHash = req.blockHash
			}
		}
	}

	// Helper function to check that the request hasn't been located in another
	// thread and removed from queue already.
	reqStillQueued := func(outPt OutPoint) bool {
		_, found := r.redemptions[outPt]
		return found
	}

	for iHeight <= tipHeight {
		validReqs := make(map[OutPoint]*FindRedemptionReq, len(undiscovered))
		r.mtx.RLock()
		for outPt, req := range undiscovered {
			if iHeight >= req.blockHeight && reqStillQueued(req.outPt) {
				validReqs[outPt] = req
			}
		}
		r.mtx.RUnlock()

		if len(validReqs) == 0 {
			iHeight++
			continue
		}

		r.log.Debugf("tryRedemptionRequests - Checking block %v for redemptions...", iHash)
		discovered := r.searchBlockForRedemptions(ctx, validReqs, *iHash)
		for outPt, res := range discovered {
			req, found := undiscovered[outPt]
			if !found {
				r.log.Critical("Request not found in undiscovered map. This shouldn't be possible.")
				continue
			}
			redeemTxID, redeemTxInput, _ := decodeCoinID(res.redemptionCoinID)
			r.log.Debugf("Found redemption %s:%d", redeemTxID, redeemTxInput)
			req.success(res)
			delete(undiscovered, outPt)
		}

		if len(undiscovered) == 0 {
			break
		}

		iHeight++
		if iHeight <= tipHeight {
			if iHash, err = r.getBlockHash(int64(iHeight)); err != nil {
				// This might be due to a reorg. Don't abandon yet, since
				// tryRedemptionRequests will be tried again by the block
				// monitor loop.
				r.log.Warn("error getting block hash for height %d: %v", iHeight, err)
				return
			}
		}
	}

	// Check mempool for any remaining undiscovered requests.
	for outPt, req := range undiscovered {
		mempoolReqs[outPt] = req
	}

	if len(mempoolReqs) == 0 {
		return
	}

	// Do we really want to do this? Mempool could be huge.
	searchDur := time.Minute * 5
	searchCtx, cancel := context.WithTimeout(ctx, searchDur)
	defer cancel()
	for outPt, res := range r.findRedemptionsInMempool(searchCtx, mempoolReqs) {
		req, ok := mempoolReqs[outPt]
		if !ok {
			r.log.Errorf("findRedemptionsInMempool discovered outpoint not found")
			continue
		}
		req.success(res)
	}
	if err := searchCtx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			r.log.Errorf("mempool search exceeded %s time limit", searchDur)
		} else {
			r.log.Error("mempool search was cancelled")
		}
	}
}

// prepareRedemptionRequestsForBlockCheck prepares a copy of the currently
// tracked redemptions, checking for missing block data along the way.
func (r *RedemptionFinder) prepareRedemptionRequestsForBlockCheck() []*FindRedemptionReq {
	// Search for contract redemption in new blocks if there
	// are contracts pending redemption.
	r.mtx.Lock()
	defer r.mtx.Unlock()
	reqs := make([]*FindRedemptionReq, 0, len(r.redemptions))
	for _, req := range r.redemptions {
		// If the request doesn't have a block hash yet, check if we can get one
		// now.
		if req.blockHash == nil {
			r.trySetRedemptionRequestBlock(req)
		}
		reqs = append(reqs, req)
	}
	return reqs
}

// ReportNewTip sets the currentTip. The tipChange callback function is invoked
// and a goroutine is started to check if any contracts in the
// findRedemptionQueue are redeemed in the new blocks.
func (r *RedemptionFinder) ReportNewTip(ctx context.Context, prevTip, newTip *BlockVector) {
	reqs := r.prepareRedemptionRequestsForBlockCheck()
	// Redemption search would be compromised if the starting point cannot
	// be determined, as searching just the new tip might result in blocks
	// being omitted from the search operation. If that happens, cancel all
	// find redemption requests in queue.
	notifyFatalFindRedemptionError := func(s string, a ...any) {
		for _, req := range reqs {
			req.fail("tipChange handler - "+s, a...)
		}
	}

	var startPoint *BlockVector
	// Check if the previous tip is still part of the mainchain (prevTip confs >= 0).
	// Redemption search would typically resume from prevTipHeight + 1 unless the
	// previous tip was re-orged out of the mainchain, in which case redemption
	// search will resume from the mainchain ancestor of the previous tip.
	prevTipHeader, isMainchain, err := r.getBlockHeader(&prevTip.Hash)
	switch {
	case err != nil:
		// Redemption search cannot continue reliably without knowing if there
		// was a reorg, cancel all find redemption requests in queue.
		notifyFatalFindRedemptionError("getBlockHeader error for prev tip hash %s: %w",
			prevTip.Hash, err)
		return

	case !isMainchain:
		// The previous tip is no longer part of the mainchain. Crawl blocks
		// backwards until finding a mainchain block. Start with the block
		// that is the immediate ancestor to the previous tip.
		ancestorBlockHash, err := chainhash.NewHashFromStr(prevTipHeader.PreviousBlockHash)
		if err != nil {
			notifyFatalFindRedemptionError("hash decode error for block %s: %w", prevTipHeader.PreviousBlockHash, err)
			return
		}
		for {
			aBlock, isMainchain, err := r.getBlockHeader(ancestorBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("getBlockHeader error for block %s: %w", ancestorBlockHash, err)
				return
			}
			if isMainchain {
				// Found the mainchain ancestor of previous tip.
				startPoint = &BlockVector{Height: aBlock.Height, Hash: *ancestorBlockHash}
				r.log.Debugf("reorg detected from height %d to %d", aBlock.Height, newTip.Height)
				break
			}
			if aBlock.Height == 0 {
				// Crawled back to genesis block without finding a mainchain ancestor
				// for the previous tip. Should never happen!
				notifyFatalFindRedemptionError("no mainchain ancestor for orphaned block %s", prevTipHeader.Hash)
				return
			}
			ancestorBlockHash, err = chainhash.NewHashFromStr(aBlock.PreviousBlockHash)
			if err != nil {
				notifyFatalFindRedemptionError("hash decode error for block %s: %w", prevTipHeader.PreviousBlockHash, err)
				return
			}
		}

	case newTip.Height-prevTipHeader.Height > 1:
		// 2 or more blocks mined since last tip, start at prevTip height + 1.
		afterPrivTip := prevTipHeader.Height + 1
		hashAfterPrevTip, err := r.getBlockHash(afterPrivTip)
		if err != nil {
			notifyFatalFindRedemptionError("getBlockHash error for height %d: %w", afterPrivTip, err)
			return
		}
		startPoint = &BlockVector{Hash: *hashAfterPrevTip, Height: afterPrivTip}

	default:
		// Just 1 new block since last tip report, search the lone block.
		startPoint = newTip
	}

	if len(reqs) > 0 {
		go r.tryRedemptionRequests(ctx, &startPoint.Hash, reqs)
	}
}

// trySetRedemptionRequestBlock should be called with findRedemptionMtx Lock'ed.
func (r *RedemptionFinder) trySetRedemptionRequestBlock(req *FindRedemptionReq) {
	tx, err := r.getWalletTransaction(&req.outPt.TxHash)
	if err != nil {
		r.log.Errorf("getWalletTransaction error for FindRedemption transaction: %v", err)
		return
	}

	if tx.BlockHash == "" {
		return
	}
	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		r.log.Errorf("error decoding block hash %q: %v", tx.BlockHash, err)
		return
	}

	blockHeight, err := r.checkRedemptionBlockDetails(req.outPt, blockHash, req.pkScript)
	if err != nil {
		r.log.Error(err)
		return
	}
	// Don't update the FindRedemptionReq, since the findRedemptionMtx only
	// protects the map.
	req = &FindRedemptionReq{
		outPt:        req.outPt,
		blockHash:    blockHash,
		blockHeight:  blockHeight,
		resultChan:   req.resultChan,
		pkScript:     req.pkScript,
		contractHash: req.contractHash,
	}
	r.redemptions[req.outPt] = req
}

func (r *RedemptionFinder) CancelRedemptionSearches() {
	// Close all open channels for contract redemption searches
	// to prevent leakages and ensure goroutines that are started
	// to wait on these channels end gracefully.
	r.mtx.Lock()
	for contractOutpoint, req := range r.redemptions {
		req.fail("shutting down")
		delete(r.redemptions, contractOutpoint)
	}
	r.mtx.Unlock()
}

func FindRedemptionsInTxWithHasher(ctx context.Context, segwit bool, reqs map[OutPoint]*FindRedemptionReq, msgTx *wire.MsgTx,
	chainParams *chaincfg.Params, hashTx func(*wire.MsgTx) *chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult) {

	discovered = make(map[OutPoint]*FindRedemptionResult, len(reqs))

	for vin, txIn := range msgTx.TxIn {
		if ctx.Err() != nil {
			return discovered
		}
		poHash, poVout := txIn.PreviousOutPoint.Hash, txIn.PreviousOutPoint.Index
		for outPt, req := range reqs {
			if discovered[outPt] != nil {
				continue
			}
			if outPt.TxHash == poHash && outPt.Vout == poVout {
				// Match!
				txHash := hashTx(msgTx)
				secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript, req.contractHash[:], segwit, chainParams)
				if err != nil {
					req.fail("no secret extracted from redemption input %s:%d for swap output %s: %v",
						txHash, vin, outPt, err)
					continue
				}
				discovered[outPt] = &FindRedemptionResult{
					redemptionCoinID: ToCoinID(txHash, uint32(vin)),
					secret:           secret,
				}
			}
		}
	}
	return
}
