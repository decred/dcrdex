// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v3"
	"github.com/decred/dcrd/gcs/v3/blockcf2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/rpcclient/v7"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

var (
	requiredWalletVersion = dex.Semver{Major: 8, Minor: 8, Patch: 0}
	requiredNodeVersion   = dex.Semver{Major: 7, Minor: 0, Patch: 0}
)

// RawRequest RPC methods
const (
	methodGetCFilterV2       = "getcfilterv2"
	methodListUnspent        = "listunspent"
	methodListLockUnspent    = "listlockunspent"
	methodSignRawTransaction = "signrawtransaction"
	methodSyncStatus         = "syncstatus"
	methodGetPeerInfo        = "getpeerinfo"
)

// rpcWallet implements Wallet functionality using an rpc client to communicate
// with the json-rpc server of an external dcrwallet daemon.
type rpcWallet struct {
	chainParams *chaincfg.Params
	log         dex.Logger
	rpcCfg      *rpcclient.ConnConfig

	rpcMtx  sync.RWMutex
	spvMode bool
	// rpcConnector is a rpcclient.Client, does not need to be
	// set for testing.
	rpcConnector rpcConnector
	// rpcClient is a combined rpcclient.Client+dcrwallet.Client,
	// or a stub for testing.
	rpcClient rpcClient

	connectCount uint32 // atomic
}

// Ensure rpcWallet satisfies the Wallet interface.
var _ Wallet = (*rpcWallet)(nil)
var _ Mempooler = (*rpcWallet)(nil)
var _ FeeRateEstimator = (*rpcWallet)(nil)

type walletClient = dcrwallet.Client

type combinedClient struct {
	*rpcclient.Client
	*walletClient
}

func newCombinedClient(nodeRPCClient *rpcclient.Client, chainParams *chaincfg.Params) *combinedClient {
	return &combinedClient{
		nodeRPCClient,
		dcrwallet.NewClient(dcrwallet.RawRequestCaller(nodeRPCClient), chainParams),
	}
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
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	GetBlockHeaderVerbose(ctx context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error)
	GetBlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*wire.BlockHeader, error)
	GetRawMempool(ctx context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error)
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
func newRPCWallet(settings map[string]string, logger dex.Logger, net dex.Network) (*rpcWallet, error) {
	cfg, chainParams, err := loadRPCConfig(settings, net)
	if err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

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

	certs, err := os.ReadFile(cfg.RPCCert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
	}

	log.Infof("Setting up rpc client to communicate with dcrwallet at %s with TLS certificate %q.",
		cfg.RPCListen, cfg.RPCCert)
	rpcw.rpcCfg = &rpcclient.ConnConfig{
		Host:                cfg.RPCListen,
		Endpoint:            "ws",
		User:                cfg.RPCUser,
		Pass:                cfg.RPCPass,
		Certificates:        certs,
		DisableConnectOnNew: true, // don't start until Connect
	}
	// Validate the RPC client config, and create a placeholder (non-nil) RPC
	// connector and client that will be replaced on Connect. Any method calls
	// prior to Connect will be met with rpcclient.ErrClientNotConnected rather
	// than a panic.
	nodeRPCClient, err := rpcclient.New(rpcw.rpcCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("error setting up rpc client: %w", err)
	}
	rpcw.rpcConnector = nodeRPCClient
	rpcw.rpcClient = newCombinedClient(nodeRPCClient, chainParams)

	return rpcw, nil
}

// Reconfigure updates the wallet to user a new configuration.
func (w *rpcWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, net dex.Network, currentAddress, depositAccount string) (restart bool, err error) {
	if !(cfg.Type == walletTypeDcrwRPC || cfg.Type == walletTypeLegacy) {
		return true, nil
	}

	rpcCfg, chainParams, err := loadRPCConfig(cfg.Settings, net)
	if err != nil {
		return false, fmt.Errorf("error parsing config: %w", err)
	}

	walletCfg := new(walletConfig)
	_, err = loadConfig(cfg.Settings, net, walletCfg)
	if err != nil {
		return false, err
	}

	if chainParams.Net != w.chainParams.Net {
		return false, errors.New("cannot reconfigure to use different network")
	}
	certs, err := os.ReadFile(rpcCfg.RPCCert)
	if err != nil {
		return false, fmt.Errorf("TLS certificate read error: %w", err)
	}
	if rpcCfg.RPCUser == w.rpcCfg.User &&
		rpcCfg.RPCPass == w.rpcCfg.Pass &&
		bytes.Equal(certs, w.rpcCfg.Certificates) &&
		rpcCfg.RPCListen == w.rpcCfg.Host {
		return false, nil
	}

	newWallet, err := newRPCWallet(cfg.Settings, w.log, net)
	if err != nil {
		return false, err
	}

	err = newWallet.Connect(ctx)
	if err != nil {
		return false, fmt.Errorf("error connecting new wallet")
	}

	if walletCfg.ActivelyUsed {
		a, err := stdaddr.DecodeAddress(currentAddress, w.chainParams)
		if err != nil {
			return false, err
		}
		owns, err := newWallet.AccountOwnsAddress(ctx, a, depositAccount)
		if err != nil {
			return false, err
		}
		if !owns {
			return false, errors.New("cannot reconfigure to different wallet while there are active deals")
		}
	}

	w.rpcMtx.Lock()
	defer w.rpcMtx.Unlock()
	w.chainParams = newWallet.chainParams
	w.rpcCfg = newWallet.rpcCfg
	w.spvMode = newWallet.spvMode
	w.rpcConnector = newWallet.rpcConnector
	w.rpcClient = newWallet.rpcClient

	return false, nil
}

func (w *rpcWallet) handleRPCClientReconnection(ctx context.Context) {
	connectCount := atomic.AddUint32(&w.connectCount, 1)
	if connectCount == 1 {
		// first connection, below check will be performed
		// by *rpcWallet.Connect.
		return
	}

	w.log.Debugf("dcrwallet reconnected (%d)", connectCount-1)
	w.rpcMtx.RLock()
	defer w.rpcMtx.RUnlock()
	spv, err := checkRPCConnection(ctx, w.rpcConnector, w.rpcClient, w.log)
	if err != nil {
		w.log.Errorf("dcrwallet reconnect handler error: %v", err)
	}
	w.spvMode = spv
}

// checkRPCConnection verifies the dcrwallet connection with the walletinfo RPC
// and sets the spvMode flag accordingly. The spvMode flag is only set after a
// successful check. This method is not safe for concurrent access, and the
// rpcMtx must be at least read locked.
func checkRPCConnection(ctx context.Context, connector rpcConnector, client rpcClient, log dex.Logger) (bool, error) {
	// Check the required API versions.
	versions, err := connector.Version(ctx)
	if err != nil {
		return false, fmt.Errorf("dcrwallet version fetch error: %w", err)
	}

	ver, exists := versions["dcrwalletjsonrpcapi"]
	if !exists {
		return false, fmt.Errorf("dcrwallet.Version response missing 'dcrwalletjsonrpcapi'")
	}
	walletSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatible(requiredWalletVersion, walletSemver) {
		return false, fmt.Errorf("dcrwallet has an incompatible JSON-RPC version: got %s, expected %s",
			walletSemver, requiredWalletVersion)
	}

	ver, exists = versions["dcrdjsonrpcapi"]
	if exists {
		nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
		if !dex.SemverCompatible(requiredNodeVersion, nodeSemver) {
			return false, fmt.Errorf("dcrd has an incompatible JSON-RPC version: got %s, expected %s",
				nodeSemver, requiredNodeVersion)
		}
		log.Infof("Connected to dcrwallet (JSON-RPC API v%s) proxying dcrd (JSON-RPC API v%s)",
			walletSemver, nodeSemver)
		return false, nil
	}

	// SPV maybe?
	walletInfo, err := client.WalletInfo(ctx)
	if err != nil {
		return false, fmt.Errorf("walletinfo rpc error: %w", translateRPCCancelErr(err))
	}
	if !walletInfo.SPV {
		return false, fmt.Errorf("dcrwallet.Version response missing 'dcrdjsonrpcapi' for non-spv wallet")
	}
	log.Infof("Connected to dcrwallet (JSON-RPC API v%s) in SPV mode", walletSemver)
	return true, nil
}

// Connect establishes a connection to the previously created rpc client. The
// wallet must not already be connected.
func (w *rpcWallet) Connect(ctx context.Context) error {
	w.rpcMtx.Lock()
	defer w.rpcMtx.Unlock()

	// NOTE: rpcclient.(*Client).Disconnected() returns false prior to connect,
	// so we cannot block incorrect Connect calls on that basis. However, it is
	// always safe to call Shutdown, so do it just in case.
	w.rpcConnector.Shutdown()

	// Prepare a fresh RPC client.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		// Setup an on-connect handler for logging (re)connects.
		OnClientConnected: func() { w.handleRPCClientReconnection(ctx) },
	}
	nodeRPCClient, err := rpcclient.New(w.rpcCfg, ntfnHandlers)
	if err != nil { // should never fail since we validated the config in newRPCWallet
		return fmt.Errorf("failed to create dcrwallet RPC client: %w", err)
	}

	atomic.StoreUint32(&w.connectCount, 0) // handleRPCClientReconnection should skip checkRPCConnection on first Connect

	w.rpcConnector = nodeRPCClient
	w.rpcClient = newCombinedClient(nodeRPCClient, w.chainParams)

	err = nodeRPCClient.Connect(ctx, false) // no retry
	if err != nil {
		return fmt.Errorf("dcrwallet connect error: %w", err)
	}

	net, err := w.rpcClient.GetCurrentNet(ctx)
	if err != nil {
		return translateRPCCancelErr(err)
	}
	if net != w.chainParams.Net {
		return fmt.Errorf("unexpected wallet network %s, expected %s", net, w.chainParams.Net)
	}

	// The websocket client is connected now, so if the following check
	// fails and we return with a non-nil error, we must shutdown the
	// rpc client otherwise subsequent reconnect attempts will be met
	// with "websocket client has already connected".
	spv, err := checkRPCConnection(ctx, w.rpcConnector, w.rpcClient, w.log)
	if err != nil {
		// The client should still be connected, but if not, do not try to
		// shutdown and wait as it could hang.
		if !errors.Is(err, rpcclient.ErrClientShutdown) {
			// Using w.Disconnect would deadlock with rpcMtx already locked.
			w.rpcConnector.Shutdown()
			w.rpcConnector.WaitForShutdown()
		}
		return err
	}

	w.spvMode = spv

	return nil
}

// Disconnect shuts down access to the wallet by disconnecting the rpc client.
// Part of the Wallet interface.
func (w *rpcWallet) Disconnect() {
	w.rpcMtx.Lock()
	defer w.rpcMtx.Unlock()

	w.rpcConnector.Shutdown() // rpcclient.(*Client).Disconnect is a no-op with auto-reconnect
	w.rpcConnector.WaitForShutdown()

	// NOTE: After rpcclient shutdown, the rpcConnector and rpcClient are dead
	// and cannot be started again. Connect must recreate them.
}

// client is a thread-safe accessor to the wallet's rpcClient.
func (w *rpcWallet) client() rpcClient {
	w.rpcMtx.RLock()
	defer w.rpcMtx.RUnlock()
	return w.rpcClient
}

// Network returns the network of the connected wallet.
// Part of the Wallet interface.
func (w *rpcWallet) Network(ctx context.Context) (wire.CurrencyNet, error) {
	net, err := w.client().GetCurrentNet(ctx)
	return net, translateRPCCancelErr(err)
}

// SpvMode returns true if the wallet is connected to the Decred
// network via SPV peers.
// Part of the Wallet interface.
func (w *rpcWallet) SpvMode() bool {
	w.rpcMtx.RLock()
	defer w.rpcMtx.RUnlock()
	return w.spvMode
}

// NotifyOnTipChange registers a callback function that should be invoked when
// the wallet sees new mainchain blocks. The return value indicates if this
// notification can be provided.
// Part of the Wallet interface.
func (w *rpcWallet) NotifyOnTipChange(ctx context.Context, _ TipChangeCallback) bool {
	return false
}

// AddressInfo returns information for the provided address. It is an error
// if the address is not owned by the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) AddressInfo(ctx context.Context, address string) (*AddressInfo, error) {
	a, err := stdaddr.DecodeAddress(address, w.chainParams)
	if err != nil {
		return nil, err
	}
	res, err := w.client().ValidateAddress(ctx, a)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}
	if !res.IsValid {
		return nil, fmt.Errorf("address is invalid")
	}
	if !res.IsMine {
		return nil, fmt.Errorf("address does not belong to this wallet")
	}
	if res.Branch == nil {
		return nil, fmt.Errorf("no account branch info for address")
	}
	return &AddressInfo{Account: res.Account, Branch: *res.Branch}, nil
}

// AccountOwnsAddress checks if the provided address belongs to the specified
// account.
// Part of the Wallet interface.
func (w *rpcWallet) AccountOwnsAddress(ctx context.Context, addr stdaddr.Address, acctName string) (bool, error) {
	va, err := w.rpcClient.ValidateAddress(ctx, addr)
	if err != nil {
		return false, translateRPCCancelErr(err)
	}
	return va.IsMine && va.Account == acctName, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) AccountBalance(ctx context.Context, confirms int32, acctName string) (*walletjson.GetAccountBalanceResult, error) {
	balances, err := w.rpcClient.GetBalanceMinConf(ctx, acctName, int(confirms))
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}

	for i := range balances.Balances {
		ab := &balances.Balances[i]
		if ab.AccountName == acctName {
			return ab, nil
		}
	}

	return nil, fmt.Errorf("account not found: %q", acctName)
}

// LockedOutputs fetches locked outputs for the specified account using rpc
// RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) LockedOutputs(ctx context.Context, acctName string) ([]chainjson.TransactionInput, error) {
	var locked []chainjson.TransactionInput
	err := w.rpcClientRawRequest(ctx, methodListLockUnspent, anylist{acctName}, &locked)
	return locked, translateRPCCancelErr(err)
}

// EstimateSmartFeeRate returns a smart feerate estimate using the
// estimatesmartfee rpc.
// Part of the Wallet interface.
func (w *rpcWallet) EstimateSmartFeeRate(ctx context.Context, confTarget int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	// estimatesmartfee 1 returns extremely high rates (e.g. 0.00817644).
	if confTarget < 2 {
		confTarget = 2
	}
	estimateFeeResult, err := w.client().EstimateSmartFee(ctx, confTarget, mode)
	if err != nil {
		return 0, translateRPCCancelErr(err)
	}
	return estimateFeeResult.FeeRate, nil
}

// Unspents fetches unspent outputs for the specified account using rpc
// RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) Unspents(ctx context.Context, acctName string) ([]*walletjson.ListUnspentResult, error) {
	var unspents []*walletjson.ListUnspentResult
	// minconf, maxconf (rpcdefault=9999999), [address], account
	params := anylist{0, 9999999, nil, acctName}
	err := w.rpcClientRawRequest(ctx, methodListUnspent, params, &unspents)
	return unspents, err
}

// InternalAddress returns a change address from the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) InternalAddress(ctx context.Context, acctName string) (stdaddr.Address, error) {
	addr, err := w.rpcClient.GetRawChangeAddress(ctx, acctName, w.chainParams)
	return addr, translateRPCCancelErr(err)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the Wallet interface.
func (w *rpcWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	return translateRPCCancelErr(w.client().LockUnspent(ctx, unlock, ops))
}

// UnspentOutput returns information about an unspent tx output, if found
// and unspent. Use wire.TxTreeUnknown if the output tree is unknown, the
// correct tree will be returned if the unspent output is found.
// This method is only guaranteed to return results for outputs that pay to
// the wallet. Returns asset.CoinNotFoundError if the unspent output cannot
// be located.
// Part of the Wallet interface.
func (w *rpcWallet) UnspentOutput(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8) (*TxOutput, error) {
	var checkTrees []int8
	switch {
	case tree == wire.TxTreeUnknown:
		checkTrees = []int8{wire.TxTreeRegular, wire.TxTreeStake}
	case tree == wire.TxTreeRegular || tree == wire.TxTreeStake:
		checkTrees = []int8{tree}
	default:
		return nil, fmt.Errorf("invalid tx tree %d", tree)
	}

	for _, tree := range checkTrees {
		txOut, err := w.client().GetTxOut(ctx, txHash, index, tree, true)
		if err != nil {
			return nil, translateRPCCancelErr(err)
		}
		if txOut == nil {
			continue
		}

		amount, err := dcrutil.NewAmount(txOut.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid amount %f: %v", txOut.Value, err)
		}
		pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
		if err != nil {
			return nil, fmt.Errorf("invalid ScriptPubKey %s: %v", txOut.ScriptPubKey.Hex, err)
		}
		output := &TxOutput{
			TxOut:         newTxOut(int64(amount), txOut.ScriptPubKey.Version, pkScript),
			Tree:          tree,
			Addresses:     txOut.ScriptPubKey.Addresses,
			Confirmations: uint32(txOut.Confirmations),
		}
		return output, nil
	}

	return nil, asset.CoinNotFoundError
}

// ExternalAddress returns an external address from the specified account using
// GapPolicyIgnore.
// Part of the Wallet interface.
func (w *rpcWallet) ExternalAddress(ctx context.Context, acctName string) (stdaddr.Address, error) {
	addr, err := w.rpcClient.GetNewAddressGapPolicy(ctx, acctName, dcrwallet.GapPolicyIgnore)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}
	return addr, nil
}

// SignRawTransaction signs the provided transaction using rpc RawRequest.
// Part of the Wallet interface.
func (w *rpcWallet) SignRawTransaction(ctx context.Context, inTx *wire.MsgTx) (*wire.MsgTx, error) {
	baseTx := inTx.Copy()
	txHex, err := msgTxToHex(baseTx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode MsgTx: %w", err)
	}
	var res walletjson.SignRawTransactionResult
	err = w.rpcClientRawRequest(ctx, methodSignRawTransaction, anylist{txHex}, &res)
	if err != nil {
		return nil, err
	}

	for i := range res.Errors {
		sigErr := &res.Errors[i]
		return nil, fmt.Errorf("signing %v:%d, seq = %d, sigScript = %v, failed: %v (is wallet locked?)",
			sigErr.TxID, sigErr.Vout, sigErr.Sequence, sigErr.ScriptSig, sigErr.Error)
		// Will be incomplete below, so log each SignRawTransactionError and move on.
	}

	if !res.Complete {
		baseTxB, _ := baseTx.Bytes()
		w.log.Errorf("Incomplete raw transaction signatures (input tx: %x / incomplete signed tx: %s)",
			baseTxB, res.Hex)
		return nil, fmt.Errorf("incomplete raw tx signatures (is wallet locked?)")
	}

	return msgTxFromHex(res.Hex)
}

// SendRawTransaction broadcasts the provided transaction to the Decred network.
// Part of the Wallet interface.
func (w *rpcWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	hash, err := w.client().SendRawTransaction(ctx, tx, allowHighFees)
	return hash, translateRPCCancelErr(err)
}

// GetBlockHeader generates a *BlockHeader for the specified block hash. The
// returned block header is a wire.BlockHeader with the addition of the block's
// median time.
func (w *rpcWallet) GetBlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*BlockHeader, error) {
	hdr, err := w.rpcClient.GetBlockHeader(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	verboseHdr, err := w.rpcClient.GetBlockHeaderVerbose(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	var nextHash *chainhash.Hash
	if verboseHdr.NextHash != "" {
		nextHash, err = chainhash.NewHashFromStr(verboseHdr.NextHash)
		if err != nil {
			return nil, fmt.Errorf("invalid next block hash %v: %w",
				verboseHdr.NextHash, err)
		}
	}
	return &BlockHeader{
		BlockHeader:   hdr,
		MedianTime:    verboseHdr.MedianTime,
		Confirmations: verboseHdr.Confirmations,
		NextHash:      nextHash,
	}, nil
}

// GetBlock returns the MsgBlock.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	blk, err := w.rpcClient.GetBlock(ctx, blockHash)
	return blk, translateRPCCancelErr(err)
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the Wallet interface.
func (w *rpcWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*WalletTransaction, error) {
	tx, err := w.client().GetTransaction(ctx, txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("error finding transaction %s in wallet: %w", txHash, translateRPCCancelErr(err))
	}
	return &WalletTransaction{
		Confirmations: tx.Confirmations,
		BlockHash:     tx.BlockHash,
		Details:       tx.Details,
		Hex:           tx.Hex,
	}, nil
}

// GetRawTransaction returns details of the tx with the provided hash. Returns
// asset.CoinNotFoundError if the tx is not found.
// Part of the Wallet interface.
func (w *rpcWallet) GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*wire.MsgTx, error) {
	utilTx, err := w.rpcClient.GetRawTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}
	return utilTx.MsgTx(), nil
}

// GetRawMempool returns hashes for all txs of the specified type in the node's
// mempool.
// Part of the Wallet interface.
func (w *rpcWallet) GetRawMempool(ctx context.Context) ([]*chainhash.Hash, error) {
	mempoolTxs, err := w.rpcClient.GetRawMempool(ctx, chainjson.GRMAll)
	return mempoolTxs, translateRPCCancelErr(err)
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the Wallet interface.
func (w *rpcWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	hash, height, err := w.client().GetBestBlock(ctx)
	return hash, height, translateRPCCancelErr(err)
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the Wallet interface.
func (w *rpcWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	bh, err := w.client().GetBlockHash(ctx, blockHeight)
	return bh, translateRPCCancelErr(err)
}

// MatchAnyScript looks for any of the provided scripts in the block specified.
// Part of the Wallet interface.
func (w *rpcWallet) MatchAnyScript(ctx context.Context, blockHash *chainhash.Hash, scripts [][]byte) (bool, error) {
	var cfRes walletjson.GetCFilterV2Result
	err := w.rpcClientRawRequest(ctx, methodGetCFilterV2, anylist{blockHash.String()}, &cfRes)
	if err != nil {
		return false, err
	}

	bf, key := cfRes.Filter, cfRes.Key
	filterB, err := hex.DecodeString(bf)
	if err != nil {
		return false, fmt.Errorf("error decoding block filter: %w", err)
	}
	keyB, err := hex.DecodeString(key)
	if err != nil {
		return false, fmt.Errorf("error decoding block filter key: %w", err)
	}
	filter, err := gcs.FromBytesV2(blockcf2.B, blockcf2.M, filterB)
	if err != nil {
		return false, fmt.Errorf("error deserializing block filter: %w", err)
	}

	var bcf2Key [gcs.KeySize]byte
	copy(bcf2Key[:], keyB)

	return filter.MatchAny(bcf2Key, scripts), nil

}

// lockWallet locks the wallet.
func (w *rpcWallet) lockWallet(ctx context.Context) error {
	return translateRPCCancelErr(w.rpcClient.WalletLock(ctx))
}

// unlockWallet unlocks the wallet.
func (w *rpcWallet) unlockWallet(ctx context.Context, passphrase string, timeoutSecs int64) error {
	return translateRPCCancelErr(w.rpcClient.WalletPassphrase(ctx, passphrase, timeoutSecs))
}

// AccountUnlocked returns true if the account is unlocked.
// Part of the Wallet interface.
func (w *rpcWallet) AccountUnlocked(ctx context.Context, acctName string) (bool, error) {
	// First return locked status of the account, falling back to walletinfo if
	// the account is not individually password protected.
	var res *walletjson.AccountUnlockedResult
	res, err := w.rpcClient.AccountUnlocked(ctx, acctName)
	if err != nil {
		return false, err
	}
	if res.Encrypted {
		return *res.Unlocked, nil
	}
	// The account is not individually encrypted, so check wallet lock status.
	walletInfo, err := w.rpcClient.WalletInfo(ctx)
	if err != nil {
		return false, fmt.Errorf("walletinfo error: %w", err)
	}
	return walletInfo.Unlocked, nil
}

// LockAccount locks the specified account.
// Part of the Wallet interface.
func (w *rpcWallet) LockAccount(ctx context.Context, acctName string) error {
	if w.rpcConnector.Disconnected() {
		return asset.ErrConnectionDown
	}

	// Since hung calls to Lock() may block shutdown of the consumer and thus
	// cancellation of the ExchangeWallet subsystem's Context, dcr.ctx, give
	// this a timeout in case the connection goes down or the RPC hangs for
	// other reasons.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	res, err := w.rpcClient.AccountUnlocked(ctx, acctName)
	if err != nil {
		return err
	}
	if !res.Encrypted {
		return w.lockWallet(ctx)
	}
	if res.Unlocked != nil && !*res.Unlocked {
		return nil
	}

	err = w.rpcClient.LockAccount(ctx, acctName)
	if isAccountLockedErr(err) {
		return nil // it's already locked
	}
	return translateRPCCancelErr(err)
}

// UnlockAccount unlocks the specified account or the wallet if account is not
// encrypted. Part of the Wallet interface.
func (w *rpcWallet) UnlockAccount(ctx context.Context, pw []byte, acctName string) error {
	var res *walletjson.AccountUnlockedResult
	res, err := w.rpcClient.AccountUnlocked(ctx, acctName)
	if err != nil {
		return err
	}
	if res.Encrypted {
		return translateRPCCancelErr(w.rpcClient.UnlockAccount(ctx, acctName, string(pw)))
	}
	return w.unlockWallet(ctx, string(pw), 0)

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
	if !ready {
		return false, syncStatus.HeadersFetchProgress, nil
	}
	// It looks like we are ready based on syncstatus, but that may just be
	// comparing wallet height to known chain height. Now check peers.
	numPeers, err := w.PeerCount(ctx)
	if err != nil {
		return false, 0, err
	}
	return numPeers > 0, syncStatus.HeadersFetchProgress, nil
}

// PeerCount returns the number of network peers to which the wallet or its
// backing node are connected.
func (w *rpcWallet) PeerCount(ctx context.Context) (uint32, error) {
	var peerInfo []*walletjson.GetPeerInfoResult
	err := w.rpcClientRawRequest(ctx, methodGetPeerInfo, nil, &peerInfo)
	return uint32(len(peerInfo)), err
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the Wallet interface.
func (w *rpcWallet) AddressPrivKey(ctx context.Context, address stdaddr.Address) (*secp256k1.PrivateKey, error) {
	wif, err := w.rpcClient.DumpPrivKey(ctx, address)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}
	var priv secp256k1.PrivateKey
	if overflow := priv.Key.SetByteSlice(wif.PrivKey()); overflow || priv.Key.IsZero() {
		return nil, errors.New("invalid private key")
	}
	return &priv, nil
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
	b, err := w.client().RawRequest(ctx, method, params)
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
