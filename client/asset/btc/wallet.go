package btc

import (
	"context"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
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
}

type tipNotifier interface {
	tipFeed() <-chan *block
}
