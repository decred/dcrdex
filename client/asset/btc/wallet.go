package btc

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// Wallet is the interface that BTC wallet backends must implement. TODO: plumb
// all requests with a context.Context.
type Wallet interface {
	RawRequester // for localFeeRate/rpcFeeRate calls
	Connect(ctx context.Context, wg *sync.WaitGroup) error
	SendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, pkScript []byte, startTime time.Time) (*wire.TxOut, uint32, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash() (*chainhash.Hash, error)
	GetBestBlockHeight() (int32, error)
	MedianTime() (time.Time, error)
	Balances() (*GetBalancesResult, error)
	ListUnspent() ([]*ListUnspentResult, error) // must not return locked coins
	LockUnspent(unlock bool, ops []*Output) error
	ListLockUnspent() ([]*RPCOutpoint, error)
	ChangeAddress() (btcutil.Address, error) // warning: don't just use the Stringer if there's a "recode" function for a clone e.g. BCH
	ExternalAddress() (btcutil.Address, error)
	SignTx(inTx *wire.MsgTx) (*wire.MsgTx, error)
	PrivKeyForAddress(addr string) (*btcec.PrivateKey, error)
	WalletUnlock(pw []byte) error
	WalletLock() error
	Locked() bool
	SyncStatus() (*asset.SyncStatus, error)
	PeerCount() (uint32, error)
	SwapConfirmations(txHash *chainhash.Hash, vout uint32, contract []byte, startTime time.Time) (confs uint32, spent bool, err error)
	GetBestBlockHeader() (*BlockHeader, error)
	OwnsAddress(addr btcutil.Address) (bool, error) // this should probably just take a string
	GetWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error)
	Reconfigure(walletCfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error)
	Fingerprint() (string, error)
	ListTransactionsSinceBlock(blockHeight int32) ([]*ListTransactionsResult, error)
	AddressUsed(addr string) (bool, error)
}

type TipRedemptionWallet interface {
	Wallet
	GetBlockHeight(*chainhash.Hash) (int32, error)
	GetBlockHeader(blockHash *chainhash.Hash) (hdr *BlockHeader, mainchain bool, err error)
	GetBlock(h chainhash.Hash) (*wire.MsgBlock, error)
	SearchBlockForRedemptions(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq, blockHash chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult)
	FindRedemptionsInMempool(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq) (discovered map[OutPoint]*FindRedemptionResult)
}

type TxFeeEstimator interface {
	EstimateSendTxFee(tx *wire.MsgTx, feeRate uint64, subtract bool) (fee uint64, err error)
}

// walletTxChecker provide a fast wallet tx query when block info not needed.
// This should be treated as an optimization method, where getWalletTransaction
// may always be used in its place if it does not exists.
type walletTxChecker interface {
	checkWalletTx(txid string) ([]byte, uint32, error)
}

// tipNotifier can be implemented if the Wallet is able to provide a stream of
// blocks as they are finished being processed.
type tipNotifier interface {
	tipFeed() <-chan *BlockVector
}

const medianTimeBlocks = 11

// chainStamper is a source of the timestamp and the previous block hash for a
// specified block. A chainStamper is used to manually calculate the median time
// for a block.
type chainStamper func(*chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error)

// CalcMedianTime calculates the median time of the previous 11 block headers.
// The median time is used for validating time-locked transactions. See notes in
// btcd/blockchain (*blockNode).CalcPastMedianTime() regarding incorrectly
// calculated median time for blocks 1, 3, 5, 7, and 9.
func CalcMedianTime(stamper chainStamper, blockHash *chainhash.Hash) (time.Time, error) {
	timestamps := make([]int64, 0, medianTimeBlocks)

	zeroHash := chainhash.Hash{}

	h := blockHash
	for i := 0; i < medianTimeBlocks; i++ {
		stamp, prevHash, err := stamper(h)
		if err != nil {
			return time.Time{}, fmt.Errorf("BlockHeader error for hash %q: %v", h, err)
		}
		timestamps = append(timestamps, stamp.Unix())

		if *prevHash == zeroHash {
			break
		}
		h = prevHash
	}

	slices.Sort(timestamps)

	medianTimestamp := timestamps[len(timestamps)/2]
	return time.Unix(medianTimestamp, 0), nil
}
