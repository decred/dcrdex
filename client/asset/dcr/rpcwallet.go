package dcr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/rpcclient/v7"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// Implements Wallet functionality using rpc connections.
type rpcWallet struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	tipAtConnect int64

	chainParams *chaincfg.Params
	log         dex.Logger

	client *rpcclient.Client
	node   rpcClient
}

var _ Wallet = (*rpcWallet)(nil)

// rpcClient is an rpcclient.Client, or a stub for testing.
type rpcClient interface {
	EstimateSmartFee(ctx context.Context, confirmations int64, mode chainjson.EstimateSmartFeeMode) (*chainjson.EstimateSmartFeeResult, error)
	GetBlockChainInfo(ctx context.Context) (*chainjson.GetBlockChainInfoResult, error)
	SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error)
	GetBalanceMinConf(ctx context.Context, account string, minConfirms int) (*walletjson.GetBalanceResult, error)
	GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error)
	GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error)
	GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error
	GetRawChangeAddress(ctx context.Context, account string, net stdaddr.AddressParams) (stdaddr.Address, error)
	GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error)
	DumpPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error)
	GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) // Should return asset.CoinNotFoundError if tx is not found.
	WalletLock(ctx context.Context) error
	WalletPassphrase(ctx context.Context, passphrase string, timeoutSecs int64) error
	AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error)
	LockAccount(ctx context.Context, account string) error
	UnlockAccount(ctx context.Context, account, passphrase string) error
	Disconnected() bool
	RawRequest(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error)
	WalletInfo(ctx context.Context) (*walletjson.WalletInfoResult, error)
	ValidateAddress(ctx context.Context, address stdaddr.Address) (*walletjson.ValidateAddressWalletResult, error)
}

// newClient attempts to create a new websocket connection to a dcrwallet
// instance with the given credentials and notification handlers.
func newClient(host, user, pass, cert string, logger dex.Logger) (*rpcclient.Client, error) {
	certs, err := os.ReadFile(cert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
	}

	config := &rpcclient.ConnConfig{
		Host:                host,
		Endpoint:            "ws",
		User:                user,
		Pass:                pass,
		Certificates:        certs,
		DisableConnectOnNew: true,
	}

	ntfnHandlers := &rpcclient.NotificationHandlers{
		// Setup an on-connect handler for logging (re)connects.
		OnClientConnected: func() {
			logger.Infof("Connected to Decred wallet at %s", host)
		},
	}
	cl, err := rpcclient.New(config, ntfnHandlers)
	if err != nil {
		return nil, fmt.Errorf("Failed to start dcrwallet RPC client: %w", err)
	}

	return cl, nil
}

// Initialize prepares this rpcWallet for use by creating the rpc client
// connection.
// Part of the Wallet interface.
func (w *rpcWallet) Initialize(cfg *asset.WalletConfig, dcrCfg *Config, chainParams *chaincfg.Params, logger dex.Logger) error {
	w.chainParams = chainParams
	w.log = logger

	// Check rpc connection config values
	missing := ""
	if dcrCfg.RPCUser == "" {
		missing += " username"
	}
	if dcrCfg.RPCPass == "" {
		missing += " password"
	}
	if missing != "" {
		return fmt.Errorf("missing dcrwallet rpc credentials:%s", missing)
	}

	logger.Infof("Setting up new DCR wallet at %s with TLS certificate %q.",
		dcrCfg.RPCListen, dcrCfg.RPCCert)
	var err error
	w.client, err = newClient(dcrCfg.RPCListen, dcrCfg.RPCUser,
		dcrCfg.RPCPass, dcrCfg.RPCCert, logger)
	if err != nil {
		return fmt.Errorf("DCR ExchangeWallet.Run error: %w", err)
	}

	// Beyond this point, only node
	w.node = &combinedClient{w.client, dcrwallet.NewClient(dcrwallet.RawRequestCaller(w.client), chainParams)}

	return nil
}

// Connect establishes a connection to the previously created rpc client.
// Part of the Wallet interface.
func (w *rpcWallet) Connect(ctx context.Context) error {
	err := w.client.Connect(ctx, false)
	if err != nil {
		return fmt.Errorf("Decred Wallet connect error: %w", err)
	}

	// The websocket client is connected now, so if any of the following checks
	// fails and we return with a non-nil error, we must shutdown the rpc client
	// or subsequent reconnect attempts will be met with "websocket client has
	// already connected".
	var success bool
	defer func() {
		if !success {
			w.client.Shutdown()
			w.client.WaitForShutdown()
		}
	}()

	// Check the required API versions.
	versions, err := w.client.Version(ctx)
	if err != nil {
		return fmt.Errorf("DCR ExchangeWallet version fetch error: %w", err)
	}

	ver, exists := versions["dcrwalletjsonrpcapi"]
	if !exists {
		return fmt.Errorf("dcrwallet.Version response missing 'dcrwalletjsonrpcapi'")
	}
	walletSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatible(requiredWalletVersion, walletSemver) {
		return fmt.Errorf("dcrwallet has an incompatible JSON-RPC version: got %s, expected %s",
			walletSemver, requiredWalletVersion)
	}
	ver, exists = versions["dcrdjsonrpcapi"]
	if !exists {
		return fmt.Errorf("dcrwallet.Version response missing 'dcrdjsonrpcapi'")
	}
	nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatible(requiredNodeVersion, nodeSemver) {
		return fmt.Errorf("dcrd has an incompatible JSON-RPC version: got %s, expected %s",
			nodeSemver, requiredNodeVersion)
	}

	curnet, err := w.client.GetCurrentNet(ctx)
	if err != nil {
		return fmt.Errorf("getcurrentnet failure: %w", err)
	}

	// Set the tipAtConnect
	_, currentTip, err := w.GetBestBlock(ctx)
	if err != nil {
		return fmt.Errorf("error getting best block height: %w", translateRPCCancelErr(err))
	}
	atomic.StoreInt64(&w.tipAtConnect, currentTip)

	success = true
	w.log.Infof("Connected to dcrwallet (JSON-RPC API v%s) proxying dcrd (JSON-RPC API v%s) on %v",
		walletSemver, nodeSemver, curnet)
	return nil
}

// Disconnect shuts down access to the wallet by disconnecting the rpc client.
// Part of the Wallet interface.
func (w *rpcWallet) Disconnect() {
	w.client.Shutdown()
	w.client.WaitForShutdown()
}

// Disconnected returns true if the rpc client is not connected.
// Part of the Wallet interface.
func (w *rpcWallet) Disconnected() bool {
	return w.node.Disconnected()
}

// AccountOwnsAddress uses the validateaddress rpc to check if the provided
// address belongs to the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) AccountOwnsAddress(ctx context.Context, account, address string) (bool, error) {
	a, err := stdaddr.DecodeAddress(address, w.chainParams)
	if err != nil {
		return false, err
	}
	va, err := w.node.ValidateAddress(ctx, a)
	if err != nil {
		// Work around a bug with dcrwallet that prevents validateaddress for
		// locked accounts.
		if isAccountNotEncryptedErr(err) {
			return false, nil
		}
		return false, translateRPCCancelErr(err)
	}
	return va.IsMine && va.Account == account, nil
}

// Balance returns the total available funds in the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) Balance(ctx context.Context, account string, lockedAmount uint64) (*asset.Balance, error) {
	balances, err := w.node.GetBalanceMinConf(ctx, account, 0)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}

	var balance asset.Balance
	var acctFound bool
	for i := range balances.Balances {
		ab := &balances.Balances[i]
		if ab.AccountName == account {
			acctFound = true
			balance.Available = toAtoms(ab.Spendable) - lockedAmount
			balance.Immature = toAtoms(ab.ImmatureCoinbaseRewards) +
				toAtoms(ab.ImmatureStakeGeneration)
			balance.Locked = lockedAmount + toAtoms(ab.LockedByTickets)
			break
		}
	}

	if !acctFound {
		return nil, fmt.Errorf("account not found: %q", account)
	}

	return &balance, nil
}

// LockedOutputs fetches locked outputs for the specified account using rpc
// RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error) {
	var locked []chainjson.TransactionInput
	err := w.nodeRawRequest(ctx, methodListLockUnspent, anylist{account}, &locked)
	return locked, translateRPCCancelErr(err)
}

// EstimateSmartFeeRate returns a smart feerate estimate using the
// estimatesmartfee rpc.
// Part of the Wallet interface.
func (w *rpcWallet) EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	estimateFeeResult, err := w.node.EstimateSmartFee(ctx, confTarget, mode)
	if err != nil {
		return 0, translateRPCCancelErr(err)
	}
	return estimateFeeResult.FeeRate, nil
}

// Unspents fetches unspent outputs for the specified account using rpc
// RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) Unspents(ctx context.Context, account string) ([]walletjson.ListUnspentResult, error) {
	var unspents []walletjson.ListUnspentResult
	// minconf, maxconf (rpcdefault=9999999), [address], account
	params := anylist{0, 9999999, nil, account}
	err := w.nodeRawRequest(ctx, methodListUnspent, params, &unspents)
	return unspents, err
}

// GetChangeAddress returns a change address from the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error) {
	addr, err := w.node.GetRawChangeAddress(ctx, account, w.chainParams)
	return addr, translateRPCCancelErr(err)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the Wallet interface.
func (w *rpcWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	return translateRPCCancelErr(w.node.LockUnspent(ctx, unlock, ops))
}

// GetTxOut returns information about an unspent tx output.
// Part of the Wallet interface.
func (w *rpcWallet) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error) {
	txOut, err := w.node.GetTxOut(ctx, txHash, index, tree, mempool)
	return txOut, translateRPCCancelErr(err)
}

// GetNewAddressGapPolicy returns an address from the specified account using
// the specified gap policy.
// Part of the Wallet interface.
func (w *rpcWallet) GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error) {
	addr, err := w.node.GetNewAddressGapPolicy(ctx, account, gap)
	return addr, translateRPCCancelErr(err)
}

// SignRawTransaction signs the provided transaction using rpc RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) SignRawTransaction(ctx context.Context, tx *wire.MsgTx) (*walletjson.SignRawTransactionResult, error) {
	txHex, err := msgTxToHex(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode MsgTx: %w", err)
	}
	var res walletjson.SignRawTransactionResult
	err = w.nodeRawRequest(ctx, methodSignRawTransaction, anylist{txHex}, &res)
	return &res, err
}

// SendRawTransaction broadcasts the provided transaction to the Decred network.
// Part of the Wallet interface.
func (w *rpcWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	hash, err := w.node.SendRawTransaction(ctx, tx, allowHighFees)
	return hash, translateRPCCancelErr(err)
}

// GetBlockHeaderVerbose returns block header info for the specified block hash.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	blockHeader, err := w.node.GetBlockHeaderVerbose(ctx, blockHash)
	return blockHeader, translateRPCCancelErr(err)
}

// GetBlockVerbose returns information about a block, optionally including verbose
// tx info.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	blk, err := w.node.GetBlockVerbose(ctx, blockHash, verboseTx)
	return blk, translateRPCCancelErr(err)
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	tx, err := w.node.GetTransaction(ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("error finding transaction %s in wallet: %w", txHash, translateRPCCancelErr(err))
	}
	return tx, nil
}

// GetRawTransactionVerbose returns details of the tx with the provided hash.
// Returns asset.CoinNotFoundError if the tx is not found.
// Part of the Wallet interface.
func (w *rpcWallet) GetRawTransactionVerbose(ctx context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	tx, err := w.node.GetRawTransactionVerbose(ctx, txHash)
	return tx, translateRPCCancelErr(err)
}

// GetRawMempool returns hashes for all txs of the specified type in the node's
// mempool.
// Part of the Wallet interface.
func (w *rpcWallet) GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	mempoolTxs, err := w.node.GetRawMempool(ctx, txType)
	return mempoolTxs, translateRPCCancelErr(err)
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the Wallet interface.
func (w *rpcWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	hash, height, err := w.node.GetBestBlock(ctx)
	return hash, height, translateRPCCancelErr(err)
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	bh, err := w.node.GetBlockHash(ctx, blockHeight)
	return bh, translateRPCCancelErr(err)
}

// BlockCFilter fetches the block filter info for the specified block.
// Part of the Wallet interface.
func (w *rpcWallet) BlockCFilter(ctx context.Context, blockHash string) (filter, key string, err error) {
	var cfRes walletjson.GetCFilterV2Result
	err = w.nodeRawRequest(ctx, methodGetCFilterV2, anylist{blockHash}, &cfRes)
	if err != nil {
		return "", "", err
	}
	return cfRes.Filter, cfRes.Key, nil
}

// LockWallet locks the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) LockWallet(ctx context.Context) error {
	return translateRPCCancelErr(w.node.WalletLock(ctx))
}

// UnlockWallet unlocks the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) UnlockWallet(ctx context.Context, passphrase string, timeoutSecs int64) error {
	return translateRPCCancelErr(w.node.WalletPassphrase(ctx, passphrase, timeoutSecs))
}

// WalletUnlocked returns true if the wallet is unlocked.
// Part of the Wallet interface.
func (w *rpcWallet) WalletUnlocked(ctx context.Context) bool {
	walletInfo, err := w.node.WalletInfo(ctx)
	if err != nil {
		w.log.Errorf("walletinfo error: %v", err)
		return true // assume wallet is unlocked?
	}
	return walletInfo.Unlocked
}

// AccountUnlocked returns true if the specified account is unlocked.
// Part of the Wallet interface.
func (w *rpcWallet) AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error) {
	res, err := w.node.AccountUnlocked(ctx, account)
	return res, translateRPCCancelErr(err)
}

// LockAccount locks the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) LockAccount(ctx context.Context, account string) error {
	err := w.node.LockAccount(ctx, account)
	if isAccountLockedErr(err) {
		return nil // it's already locked
	}
	return translateRPCCancelErr(err)
}

// UnlockAccount unlocks the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) UnlockAccount(ctx context.Context, account, passphrase string) error {
	return translateRPCCancelErr(w.node.UnlockAccount(ctx, account, passphrase))
}

// SyncStatus returns the wallet's sync status.
// Part of the Wallet interface.
func (w *rpcWallet) SyncStatus(ctx context.Context) (bool, float32, error) {
	chainInfo, err := w.node.GetBlockChainInfo(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("getblockchaininfo error: %w", translateRPCCancelErr(err))
	}
	toGo := chainInfo.Headers - chainInfo.Blocks
	if chainInfo.InitialBlockDownload || toGo > 1 {
		ogTip := atomic.LoadInt64(&w.tipAtConnect)
		totalToSync := chainInfo.Headers - ogTip
		var progress float32 = 1
		if totalToSync > 0 {
			progress = 1 - (float32(toGo) / float32(totalToSync))
		}
		return false, progress, nil
	}
	return true, 1, nil
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the Wallet interface.
func (w *rpcWallet) AddressPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error) {
	wif, err := w.node.DumpPrivKey(ctx, address)
	return wif, translateRPCCancelErr(err)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via nodeRawRequest.
type anylist []interface{}

// nodeRawRequest is used to marshal parameters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (w *rpcWallet) nodeRawRequest(ctx context.Context, method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := w.node.RawRequest(ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", translateRPCCancelErr(err))
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

// The rpcclient package functions will return a rpcclient.ErrRequestCanceled
// error if the context is canceled. Translate these to asset.ErrRequestTimeout.
func translateRPCCancelErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, rpcclient.ErrRequestCanceled) {
		err = asset.ErrRequestTimeout
	}
	return err
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCNoTxInfo
}

func isAccountNotEncryptedErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCWallet &&
		strings.Contains(rpcErr.Message, "account is not encrypted") // ... with a unique passphrase
}

func isAccountLockedErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCWalletUnlockNeeded &&
		strings.Contains(rpcErr.Message, "account is already locked")
}
