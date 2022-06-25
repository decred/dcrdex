package btc

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

type Wallet interface {
	RawRequester
	connect(ctx context.Context, wg *sync.WaitGroup) error
	estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error)
	sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error)
	getTxOut(txHash *chainhash.Hash, index uint32, pkScript []byte, startTime time.Time) (*wire.TxOut, uint32, error)
	getBlockHash(blockHeight int64) (*chainhash.Hash, error)
	getBlockHeight(*chainhash.Hash) (int32, error)
	getBestBlockHash() (*chainhash.Hash, error)
	getBestBlockHeight() (int32, error)
	medianTime() (time.Time, error)
	balances() (*GetBalancesResult, error)
	listUnspent() ([]*ListUnspentResult, error)
	lockUnspent(unlock bool, ops []*output) error
	listLockUnspent() ([]*RPCOutpoint, error)
	changeAddress() (btcutil.Address, error)
	addressPKH() (btcutil.Address, error)
	addressWPKH() (btcutil.Address, error)
	signTx(inTx *wire.MsgTx) (*wire.MsgTx, error)
	privKeyForAddress(addr string) (*btcec.PrivateKey, error)
	walletUnlock(pw []byte) error
	walletLock() error
	sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error)
	locked() bool
	syncStatus() (*syncStatus, error)
	peerCount() (uint32, error)
	swapConfirmations(txHash *chainhash.Hash, vout uint32, contract []byte, startTime time.Time) (confs uint32, spent bool, err error)
	getBlockHeader(blockHash *chainhash.Hash) (*blockHeader, error)
	ownsAddress(addr btcutil.Address) (bool, error)
	getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error)
	searchBlockForRedemptions(ctx context.Context, reqs map[outPoint]*findRedemptionReq, blockHash chainhash.Hash) (discovered map[outPoint]*findRedemptionResult)
	findRedemptionsInMempool(ctx context.Context, reqs map[outPoint]*findRedemptionReq) (discovered map[outPoint]*findRedemptionResult)
	getBlock(h chainhash.Hash) (*wire.MsgBlock, error)
	reconfigure(walletCfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error)
}

// tipNotifier can be implemented if the Wallet is able to provide a stream of
// blocks as they are finished being processed.
type tipNotifier interface {
	tipFeed() <-chan *block
}

// chainStamper is a source of the timestamp and the previous block hash for a
// specified block. A chainStamper is used to manually calculate the median time
// for a block.
type chainStamper interface {
	getChainStamp(*chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error)
}

const medianTimeBlocks = 11

// calcMedianTime calculates the median time of the previous 11 block headers.
// The median time is used for validating time-locked transactions. See notes in
// btcd/blockchain (*blockNode).CalcPastMedianTime() regarding incorrectly
// calculated median time for blocks 1, 3, 5, 7, and 9.
func calcMedianTime(stamper chainStamper, blockHash *chainhash.Hash) (time.Time, error) {
	timestamps := make([]int64, 0, medianTimeBlocks)

	zeroHash := chainhash.Hash{}

	h := blockHash
	for i := 0; i < medianTimeBlocks; i++ {
		stamp, prevHash, err := stamper.getChainStamp(h)
		if err != nil {
			return time.Time{}, fmt.Errorf("BlockHeader error for hash %q: %v", h, err)
		}
		timestamps = append(timestamps, stamp.Unix())

		if *prevHash == zeroHash {
			break
		}
		h = prevHash
	}

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	medianTimestamp := timestamps[len(timestamps)/2]
	return time.Unix(medianTimestamp, 0), nil
}
