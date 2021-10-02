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
	// Initialize prepares the wallet for use.
	Initialize(cfg *asset.WalletConfig, dcrCfg *Config, chainParams *chaincfg.Params, logger dex.Logger) error
	// Connect establishes a connection to the wallet.
	Connect(ctx context.Context) error
	//  Disconnect shuts down access to the wallet.
	Disconnect()
	// Disconnected returns true if the wallet is not connected.
	Disconnected() bool
	// AccountOwnsAddress checks if the provided address belongs to the
	// specified account.
	AccountOwnsAddress(ctx context.Context, account, address string) (bool, error)
	// Balance returns the total available funds in the wallet.
	Balance(ctx context.Context, account string, lockedAmount uint64) (*asset.Balance, error)
	// LockedOutputs fetches locked outputs for the specified account.
	LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error)
	// EstimateSmartFeeRate returns a smart feerate estimate.
	EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error)
	// Unspents fetches unspent outputs for the specified account.
	Unspents(ctx context.Context, account string) ([]walletjson.ListUnspentResult, error)
	// GetChangeAddress returns a change address from the specified account.
	GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error)
	// LockUnspent locks or unlocks the specified outpoint.
	LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error
	// GetTxOut returns information about an unspent tx output.
	GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error)
	// GetNewAddressGapPolicy returns an address from the specified account using
	// the specified gap policy.
	GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error)
	// SignRawTransaction signs the provided transaction.
	SignRawTransaction(ctx context.Context, tx *wire.MsgTx) (*walletjson.SignRawTransactionResult, error)
	// SendRawTransaction broadcasts the provided transaction to the Decred
	// network.
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	// GetBlockHeaderVerbose returns block header info for the specified block hash.
	GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	// GetBlockVerbose returns information about a block, optionally including verbose
	// tx info.
	GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	// GetTransaction returns the details of a wallet tx, if the wallet contains a
	// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
	// found in the wallet.
	GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error)
	// GetRawTransactionVerbose returns details of the tx with the provided hash.
	// Returns asset.CoinNotFoundError if the tx is not found.
	GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	// GetRawMempool returns hashes for all txs of the specified type in the node's
	// mempool.
	GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	// GetBestBlock returns the hash and height of the wallet's best block.
	GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error)
	// GetBlockHash returns the hash of the mainchain block at the specified height.
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	// BlockCFilter fetches the block filter info for the specified block.
	BlockCFilter(ctx context.Context, blockHash string) (filter, key string, err error)
	// Lock locks the wallet.
	Lock(ctx context.Context) error
	// Unlock unlocks the wallet.
	Unlock(ctx context.Context, passphrase string, timeoutSecs int64) error
	// WalletUnlocked returns true if the wallet is unlocked.
	WalletUnlocked(ctx context.Context) bool
	// AccountUnlocked returns true if the specified account is unlocked.
	AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error)
	// LockAccount locks the specified account.
	LockAccount(ctx context.Context, account string) error
	// UnlockAccount unlocks the specified account.
	UnlockAccount(ctx context.Context, account, passphrase string) error
	// SyncStatus returns the wallet's sync status.
	SyncStatus(ctx context.Context) (bool, float32, error)
	// AddressPrivKey fetches the privkey for the specified address.
	AddressPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error)
}
