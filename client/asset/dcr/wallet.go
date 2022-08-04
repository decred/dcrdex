// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// WalletConstructor defines a function that can be invoked to create a custom
// implementation of the Wallet interface.
type WalletConstructor func(settings map[string]string, chainParams *chaincfg.Params, logger dex.Logger) (Wallet, error)

// customWalletConstructors are functions for setting up custom implementations
// of the Wallet interface that may be used by the ExchangeWallet instead of the
// default rpcWallet implementation.
var customWalletConstructors = map[string]WalletConstructor{}

// RegisterCustomWallet registers a function that should be used in creating a
// Wallet implementation that the ExchangeWallet of the specified type will use
// in place of the default rpcWallet implementation. External consumers can use
// this function to provide alternative Wallet implementations, and must do so
// before attempting to create an ExchangeWallet instance of this type.
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

type TipChangeCallback func(context.Context, *chainhash.Hash, int64, error)

// BlockHeader is a wire.BlockHeader with the addition of a MedianTime field.
// Implementations must fill in the MedianTime field when returning a
// BlockHeader.
type BlockHeader struct {
	*wire.BlockHeader
	MedianTime    int64
	Confirmations int64
	NextHash      *chainhash.Hash
}

// AddressInfo is the source account and branch info for an address.
type AddressInfo struct {
	Account string
	Branch  uint32
}

// Wallet defines methods that the ExchangeWallet uses for communicating with
// a Decred wallet and blockchain.
type Wallet interface {
	// Connect establishes a connection to the wallet.
	Connect(ctx context.Context) error
	//  Disconnect shuts down access to the wallet.
	Disconnect()
	// SpvMode returns true if the wallet is connected to the Decred
	// network via SPV peers.
	SpvMode() bool
	// NotifyOnTipChange registers a callback function that the should be
	// invoked when the wallet sees new mainchain blocks. The return value
	// indicates if this notification can be provided. Where this tip change
	// notification is unimplemented, monitorBlocks should be used to track
	// tip changes.
	NotifyOnTipChange(ctx context.Context, cb TipChangeCallback) bool
	// AddressInfo returns information for the provided address. It is an error
	// if the address is not owned by the wallet.
	AddressInfo(ctx context.Context, address string) (*AddressInfo, error)
	// AccountOwnsAddress checks if the provided address belongs to the
	// specified account.
	AccountOwnsAddress(ctx context.Context, addr stdaddr.Address, acctName string) (bool, error)
	// AccountBalance returns the balance breakdown for the specified account.
	AccountBalance(ctx context.Context, confirms int32, acctName string) (*walletjson.GetAccountBalanceResult, error)
	// LockedOutputs fetches locked outputs for the Wallet.
	LockedOutputs(ctx context.Context, acctName string) ([]chainjson.TransactionInput, error)
	// Unspents fetches unspent outputs for the Wallet.
	Unspents(ctx context.Context, acctName string) ([]*walletjson.ListUnspentResult, error)
	// LockUnspent locks or unlocks the specified outpoint.
	LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error
	// UnspentOutput returns information about an unspent tx output, if found
	// and unspent. Use wire.TxTreeUnknown if the output tree is unknown, the
	// correct tree will be returned if the unspent output is found.
	// This method is only guaranteed to return results for outputs that pay to
	// the wallet, although wallets connected to a full node may return results
	// for non-wallet outputs. Returns asset.CoinNotFoundError if the unspent
	// output cannot be located.
	UnspentOutput(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8) (*TxOutput, error)
	// ExternalAddress returns a new external address.
	ExternalAddress(ctx context.Context, acctName string) (stdaddr.Address, error)
	// InternalAddress returns a change address from the Wallet.
	InternalAddress(ctx context.Context, acctName string) (stdaddr.Address, error)
	// SignRawTransaction signs the provided transaction. SignRawTransaction
	// is not used for redemptions, so previous outpoints and scripts should
	// be known by the wallet.
	// SignRawTransaction should not mutate the input transaction.
	SignRawTransaction(context.Context, *wire.MsgTx) (*wire.MsgTx, error)
	// SendRawTransaction broadcasts the provided transaction to the Decred
	// network.
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	// GetBlockHeader generates a *BlockHeader for the specified block hash. The
	// returned block header is a wire.BlockHeader with the addition of the
	// block's median time.
	GetBlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*BlockHeader, error)
	// GetBlock returns the *wire.MsgBlock.
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	// GetTransaction returns the details of a wallet tx, if the wallet contains a
	// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
	// found in the wallet.
	GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*WalletTransaction, error)
	// GetRawTransaction returns details of the tx with the provided hash.
	// Returns asset.CoinNotFoundError if the tx is not found.
	GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*wire.MsgTx, error)
	// GetBestBlock returns the hash and height of the wallet's best block.
	GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error)
	// GetBlockHash returns the hash of the mainchain block at the specified height.
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	// MatchAnyScript looks for any of the provided scripts in the block specified.
	MatchAnyScript(ctx context.Context, blockHash *chainhash.Hash, scripts [][]byte) (bool, error)
	// AccountUnlocked returns true if the account is unlocked.
	AccountUnlocked(ctx context.Context, acctName string) (bool, error)
	// LockAccount locks the account.
	LockAccount(ctx context.Context, acctName string) error
	// UnlockAccount unlocks the account.
	UnlockAccount(ctx context.Context, passphrase []byte, acctName string) error
	// SyncStatus returns the wallet's sync status.
	SyncStatus(ctx context.Context) (bool, float32, error)
	// PeerCount returns the number of network peers to which the wallet or its
	// backing node are connected.
	PeerCount(ctx context.Context) (uint32, error)
	// AddressPrivKey fetches the privkey for the specified address.
	AddressPrivKey(ctx context.Context, address stdaddr.Address) (*secp256k1.PrivateKey, error)
}

// WalletTransaction is a pared down version of walletjson.GetTransactionResult.
type WalletTransaction struct {
	Confirmations int64
	BlockHash     string
	Details       []walletjson.GetTransactionDetailsResult
	Hex           string
}

// tipNotifier can be implemented if the Wallet is able to provide a stream of
// blocks as they are finished being processed.
// DRAFT NOTE: This is alternative to NotifyOnTipChange. I prefer this method,
// and would vote to export this interface and get rid of NotifyOnTipChange.
// @itswisdomagain might be using the current API though.
type tipNotifier interface {
	tipFeed() <-chan *block
}

// FeeRateEstimator is satisfied by a Wallet that can provide fee rate
// estimates.
type FeeRateEstimator interface {
	// EstimateSmartFeeRate returns a smart feerate estimate.
	EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error)
}

// Mempooler is satisfied by a Wallet that can provide mempool info.
type Mempooler interface {
	// GetRawMempool returns hashes for all txs in a node's mempool.
	GetRawMempool(ctx context.Context) ([]*chainhash.Hash, error)
}

// TxOutput defines properties of a transaction output, including the
// details of the block containing the tx, if mined.
type TxOutput struct {
	*wire.TxOut
	Tree          int8
	Addresses     []string
	Confirmations uint32
}
