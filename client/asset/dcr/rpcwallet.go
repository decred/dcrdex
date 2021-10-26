// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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

var (
	requiredWalletVersion = dex.Semver{Major: 8, Minor: 7, Patch: 0} // TODO: Update to 8.8.0 for spv getcurrentnetwork support
	requiredNodeVersion   = dex.Semver{Major: 7, Minor: 0, Patch: 0}
)

// RawRequest RPC methods
const (
	methodGetCFilterV2       = "getcfilterv2"
	methodListUnspent        = "listunspent"
	methodListLockUnspent    = "listlockunspent"
	methodSignRawTransaction = "signrawtransaction"
	methodSyncStatus         = "syncstatus"
)

// rpcWallet implements Wallet functionality using an rpc client to communicate
// with the json-rpc server of an external dcrwallet daemon.
type rpcWallet struct {
	chainParams *chaincfg.Params
	log         dex.Logger
	spvMode     bool

	// rpcConnector is a rpcclient.Client, does not need to be
	// set for testing.
	rpcConnector
	// rpcClient is a combined rpcclient.Client+dcrwallet.Client,
	// or a stub for testing.
	rpcClient

	connectCount uint32
}

// Ensure rpcWallet satisfies the Wallet interface.
var _ Wallet = (*rpcWallet)(nil)

type walletClient = dcrwallet.Client

type combinedClient struct {
	*rpcclient.Client
	*walletClient
}

// Ensure combinedClient satisfies the rpcClient interface.
var _ rpcClient = (*combinedClient)(nil)

// ValidateAddress disambiguates the node and wallet methods of the same name.
func (cc *combinedClient) ValidateAddress(ctx context.Context, address stdaddr.Address) (*walletjson.ValidateAddressWalletResult, error) {
	return cc.walletClient.ValidateAddress(ctx, address)
}

// rpcConnector defines methods required by *rpcWallet for connecting and
// disconnecting the rpcClient to/from the json-rpc server.
type rpcConnector interface {
	Connect(ctx context.Context, retry bool) error
	Version(ctx context.Context) (map[string]chainjson.VersionResult, error)
	Disconnected() bool
	Shutdown()
	WaitForShutdown()
}

// rpcClient defines rpc request methods that are used by *rpcWallet.
// This is a *combinedClient or a stub for testing.
type rpcClient interface {
	GetCurrentNet(ctx context.Context) (wire.CurrencyNet, error)
	EstimateSmartFee(ctx context.Context, confirmations int64, mode chainjson.EstimateSmartFeeMode) (*chainjson.EstimateSmartFeeResult, error)
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
	RawRequest(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error)
	WalletInfo(ctx context.Context) (*walletjson.WalletInfoResult, error)
	ValidateAddress(ctx context.Context, address stdaddr.Address) (*walletjson.ValidateAddressWalletResult, error)
}

// newRPCWallet creates an rpcClient and uses it to construct a new instance
// of the rpcWallet. The rpcClient isn't connected to the server yet, use the
// Connect method of the returned *rpcWallet to connect the rpcClient to the
// server.
func newRPCWallet(cfg *Config, chainParams *chaincfg.Params, logger dex.Logger) (*rpcWallet, error) {
	// Check rpc connection config values
	missing := ""
	if cfg.RPCUser == "" {
		missing += " username"
	}
	if cfg.RPCPass == "" {
		missing += " password"
	}
	if missing != "" {
		return nil, fmt.Errorf("missing dcrwallet rpc credentials:%s", missing)
	}

	log := logger.SubLogger("RPC")
	rpcw := &rpcWallet{
		chainParams: chainParams,
		log:         log,
	}

	log.Infof("Setting up rpc client to communicate with dcrwallet at %s with TLS certificate %q.",
		cfg.RPCListen, cfg.RPCCert)
	err := rpcw.setupRPCClient(cfg.RPCListen, cfg.RPCUser, cfg.RPCPass, cfg.RPCCert)
	if err != nil {
		return nil, fmt.Errorf("error setting up rpc client: %w", err)
	}

	return rpcw, nil
}

// setupRPCClient attempts to create a new websocket connection to a dcrwallet
// instance with the given credentials and notification handlers.
func (w *rpcWallet) setupRPCClient(host, user, pass, cert string) error {
	certs, err := os.ReadFile(cert)
	if err != nil {
		return fmt.Errorf("TLS certificate read error: %w", err)
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
		OnClientConnected: w.handleRPCClientReconnection,
	}
	nodeRPCClient, err := rpcclient.New(config, ntfnHandlers)
	if err != nil {
		return fmt.Errorf("Failed to start dcrwallet RPC client: %w", err)
	}

	w.rpcConnector = nodeRPCClient
	w.rpcClient = &combinedClient{nodeRPCClient, dcrwallet.NewClient(dcrwallet.RawRequestCaller(nodeRPCClient), w.chainParams)}
	return nil
}

func (w *rpcWallet) handleRPCClientReconnection() {
	connectCount := atomic.AddUint32(&w.connectCount, 1)
	if connectCount == 1 {
		// first connection, below check will be performed
		// by *rpcWallet.Connect.
		return
	}

	w.log.Debugf("dcrwallet reconnected (%d)", connectCount-1)
	err := w.checkRPCConnection(context.TODO())
	if err != nil {
		w.log.Errorf("dcrwallet reconnect handler error: %v", err)
	}
}

// isSpvMode uses the walletinfo rpc to determine if this wallet is
// connected to the Decred network via SPV peers.
func (w *rpcWallet) checkRPCConnection(ctx context.Context) error {
	// Reset spvMode to false, until we're sure we're connected to
	// an SPV wallet below.
	w.spvMode = false

	// Check the required API versions.
	versions, err := w.rpcConnector.Version(ctx)
	if err != nil {
		return fmt.Errorf("dcrwallet version fetch error: %w", err)
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
	if exists {
		nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
		if !dex.SemverCompatible(requiredNodeVersion, nodeSemver) {
			return fmt.Errorf("dcrd has an incompatible JSON-RPC version: got %s, expected %s",
				nodeSemver, requiredNodeVersion)
		}
		w.log.Infof("Connected to dcrwallet (JSON-RPC API v%s) proxying dcrd (JSON-RPC API v%s) on %v",
			walletSemver, nodeSemver, w.chainParams.Name)
	} else {
		// SPV maybe?
		walletInfo, err := w.rpcClient.WalletInfo(ctx)
		if err != nil {
			return fmt.Errorf("walletinfo rpc error: %w", translateRPCCancelErr(err))
		}
		if !walletInfo.SPV {
			return fmt.Errorf("dcrwallet.Version response missing 'dcrdjsonrpcapi' for non-spv wallet")
		}
		w.spvMode = true
		w.log.Infof("Connected to dcrwallet (JSON-RPC API v%s) in SPV mode", walletSemver)
	}

	return nil
}

// Connect establishes a connection to the previously created rpc client.
// Part of the Wallet interface.
func (w *rpcWallet) Connect(ctx context.Context) error {
	err := w.rpcConnector.Connect(ctx, false)
	if err != nil {
		return fmt.Errorf("dcrwallet connect error: %w", err)
	}

	// The websocket client is connected now, so if the following check
	// fails and we return with a non-nil error, we must shutdown the
	// rpc client otherwise subsequent reconnect attempts will be met
	// with "websocket client has already connected".
	err = w.checkRPCConnection(ctx)
	if err != nil {
		w.rpcConnector.Shutdown()
		w.rpcConnector.WaitForShutdown()
		return err
	}

	return nil
}

// Disconnect shuts down access to the wallet by disconnecting the rpc client.
// Part of the Wallet interface.
func (w *rpcWallet) Disconnect() {
	w.rpcConnector.Shutdown()
	w.rpcConnector.WaitForShutdown()
}

// Disconnected returns true if the rpc client is not connected.
// Part of the Wallet interface.
func (w *rpcWallet) Disconnected() bool {
	return w.rpcConnector.Disconnected()
}

// Network returns the network of the connected wallet.
// Part of the Wallet interface.
func (w *rpcWallet) Network(ctx context.Context) (wire.CurrencyNet, error) {
	net, err := w.rpcClient.GetCurrentNet(ctx)
	return net, translateRPCCancelErr(err)
}

// SpvMode returns true if the wallet is connected to the Decred
// network via SPV peers.
// Part of the Wallet interface.
func (w *rpcWallet) SpvMode() bool {
	return w.spvMode
}

// NotifyOnTipChange registers a callback function that should be invoked when
// the wallet sees new mainchain blocks. The return value indicates if this
// notification can be provided.
// Part of the Wallet interface.
func (w *rpcWallet) NotifyOnTipChange(ctx context.Context, cb TipChangeCallback) bool {
	// TODO: Consider implementing tip change notifications using the rpcclient
	// websocket OnBlockConnected notification.
	return false
}

// AccountOwnsAddress uses the validateaddress rpc to check if the provided
// address belongs to the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) AccountOwnsAddress(ctx context.Context, account, address string) (bool, error) {
	a, err := stdaddr.DecodeAddress(address, w.chainParams)
	if err != nil {
		return false, err
	}
	va, err := w.rpcClient.ValidateAddress(ctx, a)
	if err != nil {
		return false, translateRPCCancelErr(err)
	}
	return va.IsMine && va.Account == account, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) AccountBalance(ctx context.Context, account string, confirms int32) (*walletjson.GetAccountBalanceResult, error) {
	balances, err := w.rpcClient.GetBalanceMinConf(ctx, account, int(confirms))
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}

	for i := range balances.Balances {
		ab := &balances.Balances[i]
		if ab.AccountName == account {
			return ab, nil
		}
	}

	return nil, fmt.Errorf("account not found: %q", account)
}

// LockedOutputs fetches locked outputs for the specified account using rpc
// RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) LockedOutputs(ctx context.Context, account string) ([]chainjson.TransactionInput, error) {
	var locked []chainjson.TransactionInput
	err := w.rpcClientRawRequest(ctx, methodListLockUnspent, anylist{account}, &locked)
	return locked, translateRPCCancelErr(err)
}

// EstimateSmartFeeRate returns a smart feerate estimate using the
// estimatesmartfee rpc.
// Part of the Wallet interface.
func (w *rpcWallet) EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	estimateFeeResult, err := w.rpcClient.EstimateSmartFee(ctx, confTarget, mode)
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
	err := w.rpcClientRawRequest(ctx, methodListUnspent, params, &unspents)
	return unspents, err
}

// GetChangeAddress returns a change address from the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) GetChangeAddress(ctx context.Context, account string) (stdaddr.Address, error) {
	addr, err := w.rpcClient.GetRawChangeAddress(ctx, account, w.chainParams)
	return addr, translateRPCCancelErr(err)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the Wallet interface.
func (w *rpcWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	return translateRPCCancelErr(w.rpcClient.LockUnspent(ctx, unlock, ops))
}

// GetTxOut returns information about an unspent tx output, if found and
// is unspent. Use wire.TxTreeUnknown if the output tree is unknown, the
// correct tree will be returned if the unspent output is found.
// An asset.CoinNotFoundError is returned if the unspent output cannot be
// located. UnspentOutput is only guaranteed to return results for outputs
// that pay to the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, int8, error) {
	// Check for unspent output with gettxout rpc.
	var checkTrees []int8
	switch {
	case tree == wire.TxTreeUnknown:
		checkTrees = []int8{wire.TxTreeRegular, wire.TxTreeStake}
	case tree == wire.TxTreeRegular || tree == wire.TxTreeStake:
		checkTrees = []int8{tree}
	default:
		return nil, wire.TxTreeUnknown, fmt.Errorf("invalid tx tree %d", tree)
	}

	for _, tree := range checkTrees {
		txout, err := w.rpcClient.GetTxOut(ctx, txHash, index, tree, mempool)
		if err != nil {
			return nil, tree, translateRPCCancelErr(err)
		}
		if txout != nil {
			return txout, tree, nil
		}
	}

	// Return asset.CoinNotFoundError if no result was gotten from gettxout.
	return nil, wire.TxTreeUnknown, asset.CoinNotFoundError
}

// GetNewAddressGapPolicy returns an address from the specified account using
// the specified gap policy.
// Part of the Wallet interface.
func (w *rpcWallet) GetNewAddressGapPolicy(ctx context.Context, account string, gap dcrwallet.GapPolicy) (stdaddr.Address, error) {
	addr, err := w.rpcClient.GetNewAddressGapPolicy(ctx, account, gap)
	return addr, translateRPCCancelErr(err)
}

// SignRawTransaction signs the provided transaction using rpc RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) SignRawTransaction(ctx context.Context, txHex string) (*walletjson.SignRawTransactionResult, error) {
	var res walletjson.SignRawTransactionResult
	err := w.rpcClientRawRequest(ctx, methodSignRawTransaction, anylist{txHex}, &res)
	return &res, err
}

// SendRawTransaction broadcasts the provided transaction to the Decred network.
// Part of the Wallet interface.
func (w *rpcWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	hash, err := w.rpcClient.SendRawTransaction(ctx, tx, allowHighFees)
	return hash, translateRPCCancelErr(err)
}

// GetBlockHeaderVerbose returns block header info for the specified block hash.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	blockHeader, err := w.rpcClient.GetBlockHeaderVerbose(ctx, blockHash)
	return blockHeader, translateRPCCancelErr(err)
}

// GetBlockVerbose returns information about a block, optionally including verbose
// tx info.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockVerbose(ctx context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	blk, err := w.rpcClient.GetBlockVerbose(ctx, blockHash, verboseTx)
	return blk, translateRPCCancelErr(err)
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	tx, err := w.rpcClient.GetTransaction(ctx, txHash)
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
	tx, err := w.rpcClient.GetRawTransactionVerbose(ctx, txHash)
	return tx, translateRPCCancelErr(err)
}

// GetRawMempool returns hashes for all txs of the specified type in the node's
// mempool.
// Part of the Wallet interface.
func (w *rpcWallet) GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	mempoolTxs, err := w.rpcClient.GetRawMempool(ctx, txType)
	return mempoolTxs, translateRPCCancelErr(err)
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the Wallet interface.
func (w *rpcWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	hash, height, err := w.rpcClient.GetBestBlock(ctx)
	return hash, height, translateRPCCancelErr(err)
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	bh, err := w.rpcClient.GetBlockHash(ctx, blockHeight)
	return bh, translateRPCCancelErr(err)
}

// BlockCFilter fetches the block filter info for the specified block.
// Part of the Wallet interface.
func (w *rpcWallet) BlockCFilter(ctx context.Context, blockHash *chainhash.Hash) (filter, key string, err error) {
	var cfRes walletjson.GetCFilterV2Result
	err = w.rpcClientRawRequest(ctx, methodGetCFilterV2, anylist{blockHash.String()}, &cfRes)
	if err != nil {
		return "", "", err
	}
	return cfRes.Filter, cfRes.Key, nil
}

// LockWallet locks the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) LockWallet(ctx context.Context) error {
	return translateRPCCancelErr(w.rpcClient.WalletLock(ctx))
}

// UnlockWallet unlocks the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) UnlockWallet(ctx context.Context, passphrase string, timeoutSecs int64) error {
	return translateRPCCancelErr(w.rpcClient.WalletPassphrase(ctx, passphrase, timeoutSecs))
}

// WalletUnlocked returns true if the wallet is unlocked.
// Part of the Wallet interface.
func (w *rpcWallet) WalletUnlocked(ctx context.Context) bool {
	walletInfo, err := w.rpcClient.WalletInfo(ctx)
	if err != nil {
		w.log.Errorf("walletinfo error: %v", err)
		return true // assume wallet is unlocked?
	}
	return walletInfo.Unlocked
}

// AccountUnlocked returns true if the specified account is unlocked.
// Part of the Wallet interface.
func (w *rpcWallet) AccountUnlocked(ctx context.Context, account string) (*walletjson.AccountUnlockedResult, error) {
	res, err := w.rpcClient.AccountUnlocked(ctx, account)
	return res, translateRPCCancelErr(err)
}

// LockAccount locks the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) LockAccount(ctx context.Context, account string) error {
	err := w.rpcClient.LockAccount(ctx, account)
	if isAccountLockedErr(err) {
		return nil // it's already locked
	}
	return translateRPCCancelErr(err)
}

// UnlockAccount unlocks the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) UnlockAccount(ctx context.Context, account, passphrase string) error {
	return translateRPCCancelErr(w.rpcClient.UnlockAccount(ctx, account, passphrase))
}

// SyncStatus returns the wallet's sync status.
// Part of the Wallet interface.
func (w *rpcWallet) SyncStatus(ctx context.Context) (bool, float32, error) {
	syncStatus := new(walletjson.SyncStatusResult)
	err := w.rpcClientRawRequest(ctx, methodSyncStatus, nil, syncStatus)
	if err != nil {
		return false, 0, fmt.Errorf("rawrequest error: %w", err)
	}
	ready := syncStatus.Synced && !syncStatus.InitialBlockDownload
	return ready, syncStatus.HeadersFetchProgress, nil
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the Wallet interface.
func (w *rpcWallet) AddressPrivKey(ctx context.Context, address stdaddr.Address) (*dcrutil.WIF, error) {
	wif, err := w.rpcClient.DumpPrivKey(ctx, address)
	return wif, translateRPCCancelErr(err)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via nodeRawRequest.
type anylist []interface{}

// rpcClientRawRequest is used to marshal parameters and send requests to the
// RPC server via rpcClient.RawRequest. If `thing` is non-nil, the result will
// be marshaled into `thing`.
func (w *rpcWallet) rpcClientRawRequest(ctx context.Context, method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := w.rpcClient.RawRequest(ctx, method, params)
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

func isAccountLockedErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCWalletUnlockNeeded &&
		strings.Contains(rpcErr.Message, "account is already locked")
}
