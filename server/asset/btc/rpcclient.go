// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const (
	methodGetBestBlockHash  = "getbestblockhash"
	methodGetBlockchainInfo = "getblockchaininfo"
	methodEstimateSmartFee  = "estimatesmartfee"
	methodEstimateFee       = "estimatefee"
	methodGetTxOut          = "gettxout"
	methodGetRawTransaction = "getrawtransaction"
	methodGetBlock          = "getblock"
	methodGetIndexInfo      = "getindexinfo"
	methodGetBlockHeader    = "getblockheader"
	methodGetBlockStats     = "getblockstats"

	errNoCompetition = dex.ErrorKind("no competition")
)

// RawRequester is for sending context-aware RPC requests, and has methods for
// shutting down the underlying connection. The returned error should be of type
// dcrjson.RPCError if non-nil.
type RawRequester interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
	Shutdown()
	WaitForShutdown()
}

// BlockFeeTransactions is a function that fetches a set of FeeTx, used to
// calculate median-fees manually.
type BlockFeeTransactions func(rc *RPCClient, blockHash *chainhash.Hash) (feeTxs []FeeTx, prevBlock chainhash.Hash, err error)

// RPCClient is a bitcoind wallet RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type RPCClient struct {
	ctx                  context.Context
	requester            RawRequester
	booleanGetBlockRPC   bool
	maxFeeBlocks         int
	arglessFeeEstimates  bool
	numericGetRawRPC     bool
	blockDeserializer    func([]byte) (*wire.MsgBlock, error)
	deserializeTx        func([]byte) (*wire.MsgTx, error)
	blockFeeTransactions BlockFeeTransactions
}

func (rc *RPCClient) callHashGetter(method string, args anylist) (*chainhash.Hash, error) {
	var txid string
	err := rc.call(method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// GetBestBlockHash returns the hash of the best block in the longest block
// chain.
func (rc *RPCClient) GetBestBlockHash() (*chainhash.Hash, error) {
	return rc.callHashGetter(methodGetBestBlockHash, nil)
}

// GetBlockchainInfoResult models the data returned from the getblockchaininfo
// command.
type GetBlockchainInfoResult struct {
	Blocks               int64  `json:"blocks"`
	Headers              int64  `json:"headers"`
	BestBlockHash        string `json:"bestblockhash"`
	InitialBlockDownload bool   `json:"initialblockdownload"`
}

// GetBlockChainInfo returns information related to the processing state of
// various chain-specific details.
func (rc *RPCClient) GetBlockChainInfo() (*GetBlockchainInfoResult, error) {
	chainInfo := new(GetBlockchainInfoResult)
	err := rc.call(methodGetBlockchainInfo, nil, chainInfo)
	if err != nil {
		return nil, err
	}
	return chainInfo, nil
}

// txIndexResult models the data returned from the getindexinfo command
// for txindex.
// txIndexResult.Txindex is nil if the returned data is an empty json object.
type txIndexResult struct {
	TxIndex *struct{} `json:"txindex"`
}

// checkTxIndex checks if bitcoind transaction index is enabled.
func (rc *RPCClient) checkTxIndex() (bool, error) {
	res := new(txIndexResult)
	err := rc.call(methodGetIndexInfo, anylist{"txindex"}, res)
	if err != nil {
		return false, err
	}
	// bitcoind returns an empty json object if txindex is not enabled.
	// It is safe to conclude txindex is enabled if res.Txindex is not nil.
	return res.TxIndex != nil, nil
}

// EstimateSmartFee requests the server to estimate a fee level.
func (rc *RPCClient) EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (uint64, error) {
	res := new(btcjson.EstimateSmartFeeResult)
	if err := rc.call(methodEstimateSmartFee, anylist{confTarget, mode}, res); err != nil {
		return 0, err
	}
	if res.FeeRate == nil || *res.FeeRate <= 0 {
		return 0, fmt.Errorf("fee rate couldn't be estimated")
	}
	return uint64(math.Round(*res.FeeRate * 1e5)), nil
}

// EstimateFee requests the server to estimate a fee level.
func (rc *RPCClient) EstimateFee(confTarget int64) (uint64, error) {
	var feeRate float64
	var args anylist
	if !rc.arglessFeeEstimates {
		args = anylist{confTarget}
	}
	if err := rc.call(methodEstimateFee, args, &feeRate); err != nil {
		return 0, err
	}
	if feeRate <= 0 {
		return 0, fmt.Errorf("fee could not be estimated")
	}
	return uint64(math.Round(feeRate * 1e5)), nil
}

// GetTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (rc *RPCClient) GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, rc.call(methodGetTxOut, anylist{txHash.String(), index, mempool},
		&res)
}

// GetRawTransaction retrieves tx's information.
func (rc *RPCClient) GetRawTransaction(txHash *chainhash.Hash) ([]byte, error) {
	var txB dex.Bytes
	args := anylist{txHash.String(), false}
	if rc.numericGetRawRPC {
		args[1] = 0
	}
	err := rc.call(methodGetRawTransaction, args, &txB)
	if err != nil {
		return nil, err
	}
	return txB, nil
}

// GetRawTransactionVerbose retrieves the verbose tx information.
func (rc *RPCClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*VerboseTxExtended, error) {
	args := anylist{txHash.String(), true}
	if rc.numericGetRawRPC {
		args[1] = 1
	}
	res := new(VerboseTxExtended)
	return res, rc.call(methodGetRawTransaction, args, res)
}

// GetBlockVerboseResult is a subset of *btcjson.GetBlockVerboseResult.
type GetBlockVerboseResult struct {
	Hash          string   `json:"hash"`
	Confirmations int64    `json:"confirmations"`
	Height        int64    `json:"height"`
	Tx            []string `json:"tx,omitempty"`
	PreviousHash  string   `json:"previousblockhash"`
}

func (rc *RPCClient) GetRawBlock(blockHash *chainhash.Hash) ([]byte, error) {
	arg := interface{}(0)
	if rc.booleanGetBlockRPC {
		arg = false
	}
	var blockB dex.Bytes // UnmarshalJSON hex -> bytes
	err := rc.call(methodGetBlock, anylist{blockHash.String(), arg}, &blockB)
	if err != nil {
		return nil, err
	}
	return blockB, nil
}

func (rc *RPCClient) GetMsgBlock(blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	blockB, err := rc.GetRawBlock(blockHash)
	if err != nil {
		return nil, err
	}

	var msgBlock *wire.MsgBlock
	if rc.blockDeserializer == nil {
		msgBlock = &wire.MsgBlock{}
		if err := msgBlock.Deserialize(bytes.NewReader(blockB)); err != nil {
			return nil, err
		}
	} else {
		msgBlock, err = rc.blockDeserializer(blockB)
		if err != nil {
			return nil, err
		}
	}
	return msgBlock, nil
}

// getBlockWithVerboseHeader fetches raw block data, and the "verbose" block
// header, for the block with the given hash. The verbose block header return is
// separate because it contains other useful info like the height and median
// time that the wire type does not contain.
func (rc *RPCClient) getBlockWithVerboseHeader(blockHash *chainhash.Hash) (*wire.MsgBlock, *btcjson.GetBlockHeaderVerboseResult, error) {
	msgBlock, err := rc.GetMsgBlock(blockHash)
	if err != nil {
		return nil, nil, err
	}

	verboseHeader := new(btcjson.GetBlockHeaderVerboseResult)
	err = rc.call(methodGetBlockHeader, anylist{blockHash.String(), true}, verboseHeader)
	if err != nil {
		return nil, nil, err
	}

	return msgBlock, verboseHeader, nil
}

// GetBlockVerbose fetches verbose block data for the block with the given hash.
func (rc *RPCClient) GetBlockVerbose(blockHash *chainhash.Hash) (*GetBlockVerboseResult, error) {
	arg := interface{}(1)
	if rc.booleanGetBlockRPC {
		arg = true
	}
	res := new(GetBlockVerboseResult)
	return res, rc.call(methodGetBlock, anylist{blockHash.String(), arg}, res)
}

// MedianFeeRate returns the median rate from the specified block.
func (rc *RPCClient) medianFeeRate() (uint64, error) {
	blockHash, err := rc.GetBestBlockHash()
	if err != nil {
		return 0, err
	}

	res := struct {
		FeeRatePercentiles []uint64 `json:"feerate_percentiles"` // 10th, 25th, 50th, 75th, and 90th percentiles
		TxCount            int      `json:"txs"`
	}{}

	categories := []string{"feerate_percentiles", "txs"}

	// We need at least a few transactions, but there's nothing stopping a miner
	// from publishing empty blocks, regardless of the current mempool state,
	// and we would want to reduce the impact of a particularly choosy node as
	// well, I think. So we'll check > 100 transaction in up to 10 blocks,
	// taking a weighted average of the fees. Consider two cases.
	//
	// 1) When the first block has > 100 transactions, it probably indicates the
	// the miner is operating as expected and the blockchain is busy. In this
	// case, the most recent block is the best estimate, since fees from older
	// blocks could become quickly outdated on a busy chain.
	//
	// 2) If the first block has fewer transactions, it may be safe to say that
	// either a) the miner is not stuffing blocks as expected or b) the
	// blockchain does not have 100 txs/block worth of traffic. Because we have
	// no historical view of mempool, it's impossible to say which one it is,
	// though. In this case, taking a weighted average over a few recent blocks
	// would provide a better estimate.

	var blocksChecked, txCount int
	var weight uint64
	for txCount < 101 {
		if blocksChecked >= rc.maxFeeBlocks {
			return 0, errNoCompetition
		}

		if err := rc.call(methodGetBlockStats, anylist{blockHash.String(), categories}, &res); err != nil {
			return 0, err
		}
		if len(res.FeeRatePercentiles) != 5 {
			return 0, fmt.Errorf("unexpected feerate_percentiles response. %d entries", len(res.FeeRatePercentiles))
		}

		feeRate := res.FeeRatePercentiles[2]

		weight += uint64(res.TxCount) * feeRate

		txCount += res.TxCount
		blocksChecked++

		if txCount >= 101 {
			break
		}

		// Not enough transactions to count yet.
		verboseBlock, err := rc.GetBlockVerbose(blockHash)
		if err != nil {
			return 0, err
		}

		blockHash, err = chainhash.NewHashFromStr(verboseBlock.PreviousHash)
		if err != nil {
			return 0, err
		}
	}

	// rounded average
	return uint64(math.Round(float64(weight) / float64(txCount))), nil
}

// FeeTx is a representation of a transaction that 1) has zero or more previous
// outpoints to fetch, and 2) given the requested outpoints, can report its tx
// fee rate, in Sats/byte.
type FeeTx interface {
	PrevOuts() []wire.OutPoint
	FeeRate(map[chainhash.Hash]map[int]int64) (uint64, error)
}

// btcFeeTx is the FeeTx for a standard Bitcoin MsgTx.
type btcFeeTx struct {
	*wire.MsgTx
}

var _ FeeTx = (*btcFeeTx)(nil)

// PrevOuts returns a list of previous outpoints for this tx.
func (tx *btcFeeTx) PrevOuts() []wire.OutPoint {
	ops := make([]wire.OutPoint, len(tx.TxIn))
	for i, txIn := range tx.TxIn {
		ops[i] = txIn.PreviousOutPoint
	}
	return ops
}

// FeeRate calculates this tx's fee rate.
func (tx *btcFeeTx) FeeRate(prevOuts map[chainhash.Hash]map[int]int64) (uint64, error) {
	var in, out int64
	for i, vin := range tx.TxIn {
		prevOut := vin.PreviousOutPoint
		outs, found := prevOuts[prevOut.Hash]
		if !found {
			return 0, fmt.Errorf("no prevout tx %s for %s:%d", prevOut.Hash, tx.TxHash(), i)
		}
		v, found := outs[int(prevOut.Index)]
		if !found {
			return 0, fmt.Errorf("no prevout vout %s:%d for %s:%d", prevOut.Hash, prevOut.Index, tx.TxHash(), i)
		}
		in += v
	}
	for _, vout := range tx.TxOut {
		out += vout.Value
	}
	fees := in - out
	if fees < 0 {
		return 0, fmt.Errorf("fees < 0 for tx %s", tx.TxHash())
	}
	sz := tx.SerializeSize()
	if sz == 0 {
		return 0, fmt.Errorf("size 0 tx %s", tx.TxHash())
	}
	return uint64(math.Round(float64(fees) / float64(sz))), nil
}

func btcBlockFeeTransactions(rc *RPCClient, blockHash *chainhash.Hash) (feeTxs []FeeTx, prevBlock chainhash.Hash, err error) {
	blk, err := rc.GetMsgBlock(blockHash)
	if err != nil {
		return nil, chainhash.Hash{}, err
	}

	if len(blk.Transactions) == 0 {
		return nil, chainhash.Hash{}, fmt.Errorf("no transactions?")
	}

	feeTxs = make([]FeeTx, len(blk.Transactions)-1)
	for i, msgTx := range blk.Transactions[1:] { // skip coinbase
		feeTxs[i] = &btcFeeTx{msgTx}
	}
	return feeTxs, blk.Header.PrevBlock, nil
}

// medianFeesTheHardWay calculates the median fees from the previous block(s).
// medianFeesTheHardWay is used for assets that don't have a getblockstats RPC,
// and is only useful for non-segwit assets.
func (rc *RPCClient) medianFeesTheHardWay(ctx context.Context) (uint64, error) {
	const numTxs = 101

	iHash, err := rc.GetBestBlockHash()
	if err != nil {
		return 0, err
	}

	txs := make([]FeeTx, 0, numTxs)

	// prev_out_tx_hash -> prev_out_index -> value
	prevOuts := make(map[chainhash.Hash]map[int]int64, numTxs)

	var blocksChecked int

out:
	for len(txs) < numTxs {
		if ctx.Err() != nil {
			return 0, context.Canceled
		}

		blocksChecked++
		if blocksChecked > rc.maxFeeBlocks {
			return 0, errNoCompetition
		}

		feeTxs, prevBlock, err := rc.blockFeeTransactions(rc, iHash)
		if err != nil {
			return 0, err
		}

		rand.Shuffle(len(feeTxs), func(i, j int) { feeTxs[i], feeTxs[j] = feeTxs[j], feeTxs[i] })

		for _, tx := range feeTxs {
			for _, prevOut := range tx.PrevOuts() {
				prevs := prevOuts[prevOut.Hash]
				if len(prevs) == 0 {
					prevs = make(map[int]int64, 1)
					prevOuts[prevOut.Hash] = prevs
				}
				// Create a placeholder. Value will be set after all previous
				// outpoints for the tx are recorded.
				prevs[int(prevOut.Index)] = 0
			}
			txs = append(txs, tx)

			if len(txs) >= numTxs {
				break out
			}
		}

		iHash = &prevBlock
	}

	// Fetch all the previous outpoints and log the values.
	for txHash, prevs := range prevOuts {
		if ctx.Err() != nil {
			return 0, context.Canceled
		}

		txB, err := rc.GetRawTransaction(&txHash)
		if err != nil {
			return 0, fmt.Errorf("GetRawTransaction error: %v", err)
		}

		tx, err := rc.deserializeTx(txB)
		if err != nil {
			return 0, fmt.Errorf("error deserializing tx: %v", err)
		}

		for vout := range prevs {
			if len(tx.TxOut) < vout+1 {
				return 0, fmt.Errorf("too few outputs")
			}
			prevs[vout] = tx.TxOut[vout].Value
		}
	}

	// Do math.
	rates := make([]uint64, numTxs)
	for i, tx := range txs {
		r, err := tx.FeeRate(prevOuts)
		if err != nil {
			return 0, err
		}
		rates[i] = r
	}

	sort.Slice(rates, func(i, j int) bool { return rates[i] < rates[j] })
	return rates[len(rates)/2], nil
}

// RawRequest is a wrapper func for callers that are not context-enabled.
func (rc *RPCClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	return rc.requester.RawRequest(rc.ctx, method, params)
}

// Call is used to marshal parmeters and send requests to the RPC server via
// (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result will be
// marshaled into `thing`.
func (rc *RPCClient) Call(method string, args []interface{}, thing interface{}) error {
	return rc.call(method, args, thing)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// call is used internally to marshal parmeters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (rc *RPCClient) call(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := rc.requester.RawRequest(rc.ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}

	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}
