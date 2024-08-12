// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	walletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v4/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
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
// before attempting to create an ExchangeWallet instance of this type. It'll
// panic if callers try to register a wallet twice.
func RegisterCustomWallet(constructor WalletConstructor, def *asset.WalletDefinition) {
	for _, availableWallets := range WalletInfo.AvailableWallets {
		if def.Type == availableWallets.Type {
			panic(fmt.Sprintf("wallet type (%q) already registered", def.Type))
		}
	}
	customWalletConstructors[def.Type] = constructor
	WalletInfo.AvailableWallets = append(WalletInfo.AvailableWallets, def)
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

type XCWalletAccounts struct {
	PrimaryAccount string
	UnmixedAccount string
	TradingAccount string
}

// ListTransactionsResult is similar to the walletjson.ListTransactionsResult,
// but most fields omitted.
type ListTransactionsResult struct {
	TxID       string
	BlockIndex *int64
	BlockTime  int64
	// Send set to true means that the inputs of the transaction were
	// controlled by the wallet.
	Send   bool `json:"send"`
	Fee    *float64
	TxType *walletjson.ListTransactionsTxType
}

// Wallet defines methods that the ExchangeWallet uses for communicating with
// a Decred wallet and blockchain.
type Wallet interface {
	// Connect establishes a connection to the wallet.
	Connect(ctx context.Context) error
	// Disconnect shuts down access to the wallet.
	Disconnect()
	// SpvMode returns true if the wallet is connected to the Decred
	// network via SPV peers.
	SpvMode() bool
	// Accounts returns the names of the accounts for use by the exchange
	// wallet.
	Accounts() XCWalletAccounts
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
	// ListSinceBlock returns all wallet transactions confirmed since the specified
	// height.
	ListSinceBlock(ctx context.Context, start int32) ([]ListTransactionsResult, error)
	// MatchAnyScript looks for any of the provided scripts in the block specified.
	MatchAnyScript(ctx context.Context, blockHash *chainhash.Hash, scripts [][]byte) (bool, error)
	// AccountUnlocked returns true if the account is unlocked.
	AccountUnlocked(ctx context.Context, acctName string) (bool, error)
	// LockAccount locks the account.
	LockAccount(ctx context.Context, acctName string) error
	// UnlockAccount unlocks the account.
	UnlockAccount(ctx context.Context, passphrase []byte, acctName string) error
	// SyncStatus returns the wallet's sync status.
	SyncStatus(ctx context.Context) (*asset.SyncStatus, error)
	// PeerCount returns the number of network peers to which the wallet or its
	// backing node are connected.
	PeerCount(ctx context.Context) (uint32, error)
	// AddressPrivKey fetches the privkey for the specified address.
	AddressPrivKey(ctx context.Context, address stdaddr.Address) (*secp256k1.PrivateKey, error)
	// PurchaseTickets purchases n tickets. vspHost and vspPubKey only
	// needed for internal wallets. Peer-to-peer mixing can be enabled if required.
	PurchaseTickets(ctx context.Context, n int, vspHost, vspPubKey string, isMixing bool) ([]*asset.Ticket, error)
	// Tickets returns current active ticket hashes up until they are voted
	// or revoked. Includes unconfirmed tickets.
	Tickets(ctx context.Context) ([]*asset.Ticket, error)
	// VotingPreferences returns current voting preferences.
	VotingPreferences(ctx context.Context) ([]*walletjson.VoteChoice, []*asset.TBTreasurySpend, []*walletjson.TreasuryPolicyResult, error)
	// SetVotingPreferences sets preferences used when a ticket is chosen to
	// be voted on.
	SetVotingPreferences(ctx context.Context, choices, tspendPolicy, treasuryPolicy map[string]string) error
	SetTxFee(ctx context.Context, feePerKB dcrutil.Amount) error
	StakeInfo(ctx context.Context) (*wallet.StakeInfoData, error)
	Reconfigure(ctx context.Context, cfg *asset.WalletConfig, net dex.Network, currentAddress string) (restart bool, err error)
	WalletOwnsAddress(ctx context.Context, addr stdaddr.Address) (bool, error)
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
// TODO: Makes sense.
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

type ticketPager interface {
	TicketPage(ctx context.Context, scanStart int32, n, skipN int) ([]*asset.Ticket, error)
}

// TxOutput defines properties of a transaction output, including the
// details of the block containing the tx, if mined.
type TxOutput struct {
	*wire.TxOut
	Tree          int8
	Addresses     []string
	Confirmations uint32
}
