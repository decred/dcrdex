package btc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

// WalletConstructor defines a function that can be invoked to create a custom
// implementation of the Wallet interface.
type WalletConstructor func(cfg *WalletConfig, chainParams *chaincfg.Params, logger dex.Logger) (Wallet, error)

// customWalletConstructors are functions for setting up custom implementations
// of the Wallet interface that may be used by the ExchangeWallet instead of the
// default rpcWallet implementation.
var customWalletConstructors = map[string]WalletConstructor{}

// RegisterCustomWallet registers a function that should be used in creating a
// Wallet implementation that the ExchangeWallet of the specified type will use
// in place of the default spv implementation. External consumers can use
// this function to provide alternative Wallet implementations.
func RegisterCustomWallet(constructor WalletConstructor, def *asset.WalletDefinition) error {
	for _, availableWallets := range WalletInfo.AvailableWallets {
		if def.Type == availableWallets.Type {
			return fmt.Errorf("already support %q wallets", def.Type)
		}
	}
	customWalletConstructors[def.Type] = constructor
	WalletInfo.AvailableWallets = append(WalletInfo.AvailableWallets, def)
	return nil
}

type Wallet interface {
	connect(ctx context.Context, wg *sync.WaitGroup) error
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
	internalAddress() (btcutil.Address, error)
	externalAddress() (btcutil.Address, error)
	signTx(inTx *wire.MsgTx) (*wire.MsgTx, error)
	privKeyForAddress(addr string) (*btcec.PrivateKey, error)
	walletUnlock(pw []byte) error
	walletLock() error
	sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error)
	locked() bool
	syncStatus() (*syncStatus, error)
	swapConfirmations(txHash *chainhash.Hash, vout uint32, contract []byte, startTime time.Time) (confs uint32, spent bool, err error)
	getBlockHeader(blockHash *chainhash.Hash) (*blockHeader, error)
	ownsAddress(addr btcutil.Address) (bool, error)
	getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error)
	searchBlockForRedemptions(ctx context.Context, reqs map[outPoint]*findRedemptionReq, blockHash chainhash.Hash) (discovered map[outPoint]*findRedemptionResult)
	getBlock(h chainhash.Hash) (*wire.MsgBlock, error)
}

type feeEstimator interface {
	estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error)
}

type mempoolFinder interface {
	findRedemptionsInMempool(ctx context.Context, reqs map[outPoint]*findRedemptionReq) (discovered map[outPoint]*findRedemptionResult)
}

type tipNotifier interface {
	tipFeed() <-chan *block
}
