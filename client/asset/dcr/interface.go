package dcr

import (
	"context"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

type Wallet interface {
	Setup(cfg *asset.WalletConfig, dcrCfg *Config, chainParams *chaincfg.Params, logger dex.Logger) error
	Connect(ctx context.Context) error
	Disconnect()
	Disconnected() bool
	AccountOwnsAddress(ctx context.Context, account, address string) (bool, error)
	Balance(ctx context.Context, account string, lockedAmount uint64) (*asset.Balance, error)
	LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error)
	EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error)
	Unspents(ctx context.Context, account string) ([]walletjson.ListUnspentResult, error)
	GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error)
	LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error
	GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error)
	GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error)
	SignRawTransaction(ctx context.Context, tx *wire.MsgTx) (*walletjson.SignRawTransactionResult, error)
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error)
	GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error)
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	BlockCFilter(ctx context.Context, blockHash string) (filter, key string, err error)
	WalletLock(ctx context.Context) error
	WalletPassphrase(ctx context.Context, passphrase string, timeoutSecs int64) error
	WalletUnlocked(ctx context.Context) bool
	AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error)
	LockAccount(ctx context.Context, account string) error
	UnlockAccount(ctx context.Context, account, passphrase string) error
	SyncStatus(ctx context.Context) (bool, float32, error)
	DumpPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error)
}
