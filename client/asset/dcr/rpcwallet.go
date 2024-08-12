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
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrwallet/v4/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v4/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/gcs/v4/blockcf2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

var (
	compatibleWalletRPCVersions = []dex.Semver{
		{Major: 9, Minor: 0, Patch: 0}, // 1.8-pre
		{Major: 8, Minor: 8, Patch: 0}, // 1.7 release, min for getcurrentnet
	}
	compatibleNodeRPCVersions = []dex.Semver{
		{Major: 8, Minor: 0, Patch: 0}, // 1.8-pre, just dropped unused ticket RPCs
		{Major: 7, Minor: 0, Patch: 0}, // 1.7 release, new gettxout args
	}
	// From vspWithSPVWalletRPCVersion and later the wallet's current "vsp"
	// is included in the walletinfo response and the wallet will no longer
	// error on GetTickets with an spv wallet.
	vspWithSPVWalletRPCVersion = dex.Semver{Major: 9, Minor: 2, Patch: 0}
)

// RawRequest RPC methods
const (
	methodGetCFilterV2       = "getcfilterv2"
	methodListUnspent        = "listunspent"
	methodListLockUnspent    = "listlockunspent"
	methodSignRawTransaction = "signrawtransaction"
	methodSyncStatus         = "syncstatus"
	methodGetPeerInfo        = "getpeerinfo"
	methodWalletInfo         = "walletinfo"
)

// rpcWallet implements Wallet functionality using an rpc client to communicate
// with the json-rpc server of an external dcrwallet daemon.
type rpcWallet struct {
	chainParams *chaincfg.Params
	log         dex.Logger
	rpcCfg      *rpcclient.ConnConfig
	accountsV   atomic.Value // XCWalletAccounts

	hasSPVTicketFunctions bool

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
	GetStakeInfo(ctx context.Context) (*walletjson.GetStakeInfoResult, error)
	PurchaseTicket(ctx context.Context, fromAccount string, spendLimit dcrutil.Amount, minConf *int,
		ticketAddress stdaddr.Address, numTickets *int, poolAddress stdaddr.Address, poolFees *dcrutil.Amount,
		expiry *int, ticketChange *bool, ticketFee *dcrutil.Amount) ([]*chainhash.Hash, error)
	GetTickets(ctx context.Context, includeImmature bool) ([]*chainhash.Hash, error)
	GetVoteChoices(ctx context.Context) (*walletjson.GetVoteChoicesResult, error)
	SetVoteChoice(ctx context.Context, agendaID, choiceID string) error
	SetTxFee(ctx context.Context, fee dcrutil.Amount) error
	ListSinceBlock(ctx context.Context, hash *chainhash.Hash) (*walletjson.ListSinceBlockResult, error)
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

	rpcw.accountsV.Store(XCWalletAccounts{
		PrimaryAccount: cfg.PrimaryAccount,
		UnmixedAccount: cfg.UnmixedAccount,
		TradingAccount: cfg.TradingAccount,
	})

	return rpcw, nil
}

// Accounts returns the names of the accounts for use by the exchange wallet.
func (w *rpcWallet) Accounts() XCWalletAccounts {
	return w.accountsV.Load().(XCWalletAccounts)
}

// Reconfigure updates the wallet to user a new configuration.
func (w *rpcWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, net dex.Network, currentAddress string) (restart bool, err error) {
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

	var allOk bool
	defer func() {
		if allOk { // update the account names as the last step
			w.accountsV.Store(XCWalletAccounts{
				PrimaryAccount: rpcCfg.PrimaryAccount,
				UnmixedAccount: rpcCfg.UnmixedAccount,
				TradingAccount: rpcCfg.TradingAccount,
			})
		}
	}()

	currentAccts := w.accountsV.Load().(XCWalletAccounts)

	if rpcCfg.RPCUser == w.rpcCfg.User &&
		rpcCfg.RPCPass == w.rpcCfg.Pass &&
		bytes.Equal(certs, w.rpcCfg.Certificates) &&
		rpcCfg.RPCListen == w.rpcCfg.Host &&
		rpcCfg.PrimaryAccount == currentAccts.PrimaryAccount &&
		rpcCfg.UnmixedAccount == currentAccts.UnmixedAccount &&
		rpcCfg.TradingAccount == currentAccts.TradingAccount {
		allOk = true
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

	defer func() {
		if !allOk {
			newWallet.Disconnect()
		}
	}()

	for _, acctName := range []string{rpcCfg.PrimaryAccount, rpcCfg.TradingAccount, rpcCfg.UnmixedAccount} {
		if acctName == "" {
			continue
		}
		if _, err := newWallet.AccountUnlocked(ctx, acctName); err != nil {
			return false, fmt.Errorf("error checking lock status on account %q: %v", acctName, err)
		}
	}

	a, err := stdaddr.DecodeAddress(currentAddress, w.chainParams)
	if err != nil {
		return false, err
	}
	var depositAccount string
	if rpcCfg.UnmixedAccount != "" {
		depositAccount = rpcCfg.UnmixedAccount
	} else {
		depositAccount = rpcCfg.PrimaryAccount
	}
	owns, err := newWallet.AccountOwnsAddress(ctx, a, depositAccount)
	if err != nil {
		return false, err
	}
	if !owns {
		if walletCfg.ActivelyUsed {
			return false, errors.New("cannot reconfigure to different wallet while there are active trades")
		}
		return true, nil
	}

	w.rpcMtx.Lock()
	defer w.rpcMtx.Unlock()
	w.chainParams = newWallet.chainParams
	w.rpcCfg = newWallet.rpcCfg
	w.spvMode = newWallet.spvMode
	w.rpcConnector = newWallet.rpcConnector
	w.rpcClient = newWallet.rpcClient

	allOk = true
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
	spv, hasSPVTicketFunctions, err := checkRPCConnection(ctx, w.rpcConnector, w.rpcClient, w.log)
	if err != nil {
		w.log.Errorf("dcrwallet reconnect handler error: %v", err)
	}
	w.spvMode = spv
	w.hasSPVTicketFunctions = hasSPVTicketFunctions
}

// checkRPCConnection verifies the dcrwallet connection with the walletinfo RPC
// and sets the spvMode flag accordingly. The spvMode flag is only set after a
// successful check. This method is not safe for concurrent access, and the
// rpcMtx must be at least read locked.
func checkRPCConnection(ctx context.Context, connector rpcConnector, client rpcClient, log dex.Logger) (bool, bool, error) {
	// Check the required API versions.
	versions, err := connector.Version(ctx)
	if err != nil {
		return false, false, fmt.Errorf("dcrwallet version fetch error: %w", err)
	}

	ver, exists := versions["dcrwalletjsonrpcapi"]
	if !exists {
		return false, false, fmt.Errorf("dcrwallet.Version response missing 'dcrwalletjsonrpcapi'")
	}
	walletSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatibleAny(compatibleWalletRPCVersions, walletSemver) {
		return false, false, fmt.Errorf("advertised dcrwallet JSON-RPC version %v incompatible with %v",
			walletSemver, compatibleWalletRPCVersions)
	}

	hasSPVTicketFunctions := walletSemver.Major >= vspWithSPVWalletRPCVersion.Major &&
		walletSemver.Minor >= vspWithSPVWalletRPCVersion.Minor

	ver, exists = versions["dcrdjsonrpcapi"]
	if exists {
		nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
		if !dex.SemverCompatibleAny(compatibleNodeRPCVersions, nodeSemver) {
			return false, false, fmt.Errorf("advertised dcrd JSON-RPC version %v incompatible with %v",
				nodeSemver, compatibleNodeRPCVersions)
		}
		log.Infof("Connected to dcrwallet (JSON-RPC API v%s) proxying dcrd (JSON-RPC API v%s)",
			walletSemver, nodeSemver)
		return false, false, nil
	}

	// SPV maybe?
	walletInfo, err := client.WalletInfo(ctx)
	if err != nil {
		return false, false, fmt.Errorf("walletinfo rpc error: %w", translateRPCCancelErr(err))
	}
	if !walletInfo.SPV {
		return false, false, fmt.Errorf("dcrwallet.Version response missing 'dcrdjsonrpcapi' for non-spv wallet")
	}
	log.Infof("Connected to dcrwallet (JSON-RPC API v%s) in SPV mode", walletSemver)
	return true, hasSPVTicketFunctions, nil
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
	spv, hasSPVTicketFunctions, err := checkRPCConnection(ctx, w.rpcConnector, w.rpcClient, w.log)
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
	w.hasSPVTicketFunctions = hasSPVTicketFunctions

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

// WalletOwnsAddress returns whether any of the account controlled by this
// wallet owns the specified address.
func (w *rpcWallet) WalletOwnsAddress(ctx context.Context, addr stdaddr.Address) (bool, error) {
	va, err := w.rpcClient.ValidateAddress(ctx, addr)
	if err != nil {
		return false, translateRPCCancelErr(err)
	}

	return va.IsMine, nil
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

// ExternalAddress returns an external address from the specified account using
// GapPolicyWrap. The dcrwallet user should set --gaplimit= as needed to prevent
// address reused depending on their needs. Part of the Wallet interface.
func (w *rpcWallet) ExternalAddress(ctx context.Context, acctName string) (stdaddr.Address, error) {
	addr, err := w.rpcClient.GetNewAddressGapPolicy(ctx, acctName, dcrwallet.GapPolicyWrap)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}
	return addr, nil
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
	if w.spvMode {
		gtr, err := w.rpcClient.GetTransaction(ctx, txHash)
		if err != nil {
			return nil, err
		}

		txB, err := hex.DecodeString(gtr.Hex)
		if err != nil {
			return nil, err
		}

		return msgTxFromBytes(txB)
	}

	utilTx, err := w.rpcClient.GetRawTransaction(ctx, txHash)
	if err != nil {
		return nil, err
	}

	return utilTx.MsgTx(), nil
}

func (w *rpcWallet) ListSinceBlock(ctx context.Context, start int32) ([]ListTransactionsResult, error) {
	hash, err := w.GetBlockHash(ctx, int64(start))
	if err != nil {
		return nil, err
	}

	res, err := w.client().ListSinceBlock(ctx, hash)
	if err != nil {
		return nil, err
	}

	toReturn := make([]ListTransactionsResult, 0, len(res.Transactions))
	for _, tx := range res.Transactions {
		toReturn = append(toReturn, ListTransactionsResult{
			TxID:       tx.TxID,
			BlockIndex: tx.BlockIndex,
			BlockTime:  tx.BlockTime,
			Send:       tx.Category == "send",
			TxType:     tx.TxType,
			Fee:        tx.Fee,
		})
	}

	return toReturn, nil
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
func (w *rpcWallet) SyncStatus(ctx context.Context) (*asset.SyncStatus, error) {
	syncStatus := new(walletjson.SyncStatusResult)
	if err := w.rpcClientRawRequest(ctx, methodSyncStatus, nil, syncStatus); err != nil {
		return nil, fmt.Errorf("rawrequest error: %w", err)
	}
	peers, err := w.PeerInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting peer info: %w", err)
	}
	if syncStatus.Synced {
		_, targetHeight, err := w.client().GetBestBlock(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting block count: %w", err)
		}
		return &asset.SyncStatus{
			Synced:       len(peers) > 0 && !syncStatus.InitialBlockDownload,
			TargetHeight: uint64(targetHeight),
			Blocks:       uint64(targetHeight),
		}, nil
	}
	var targetHeight int64
	for _, p := range peers {
		if p.StartingHeight > targetHeight {
			targetHeight = p.StartingHeight
		}
	}
	if targetHeight == 0 {
		return new(asset.SyncStatus), nil
	}
	return &asset.SyncStatus{
		Synced:       syncStatus.Synced && !syncStatus.InitialBlockDownload,
		TargetHeight: uint64(targetHeight),
		Blocks:       uint64(math.Round(float64(syncStatus.HeadersFetchProgress) * float64(targetHeight))),
	}, nil
}

func (w *rpcWallet) PeerInfo(ctx context.Context) (peerInfo []*walletjson.GetPeerInfoResult, _ error) {
	return peerInfo, w.rpcClientRawRequest(ctx, methodGetPeerInfo, nil, &peerInfo)
}

// PeerCount returns the number of network peers to which the wallet or its
// backing node are connected.
func (w *rpcWallet) PeerCount(ctx context.Context) (uint32, error) {
	peerInfo, err := w.PeerInfo(ctx)
	if err != nil {
		return 0, err
	}
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

// StakeInfo returns the current gestakeinfo results.
func (w *rpcWallet) StakeInfo(ctx context.Context) (*wallet.StakeInfoData, error) {
	res, err := w.rpcClient.GetStakeInfo(ctx)
	if err != nil {
		return nil, err
	}
	sdiff, err := dcrutil.NewAmount(res.Difficulty)
	if err != nil {
		return nil, err
	}
	totalSubsidy, err := dcrutil.NewAmount(res.TotalSubsidy)
	if err != nil {
		return nil, err
	}
	return &wallet.StakeInfoData{
		BlockHeight:    res.BlockHeight,
		TotalSubsidy:   totalSubsidy,
		Sdiff:          sdiff,
		OwnMempoolTix:  res.OwnMempoolTix,
		Unspent:        res.Unspent,
		Voted:          res.Voted,
		Revoked:        res.Revoked,
		UnspentExpired: res.UnspentExpired,
		PoolSize:       res.PoolSize,
		AllMempoolTix:  res.AllMempoolTix,
		Immature:       res.Immature,
		Live:           res.Live,
		Missed:         res.Missed,
		Expired:        res.Expired,
	}, nil
}

// PurchaseTickets purchases n amount of tickets. Returns the purchased ticket
// hashes if successful.
func (w *rpcWallet) PurchaseTickets(ctx context.Context, n int, _, _ string, _ bool) ([]*asset.Ticket, error) {
	hashes, err := w.rpcClient.PurchaseTicket(
		ctx,
		"default",
		0,   // spendLimit
		nil, // minConf
		nil, // ticketAddress
		&n,  // numTickets
		nil, // poolAddress
		nil, // poolFees
		nil, // expiry
		nil, // ticketChange
		nil, // ticketFee
	)
	if err != nil {
		return nil, err
	}

	now := uint64(time.Now().Unix())
	tickets := make([]*asset.Ticket, len(hashes))
	for i, h := range hashes {
		// Need to get the ticket price
		tx, err := w.rpcClient.GetTransaction(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("error getting transaction for new ticket %s: %w", h, err)
		}
		msgTx, err := msgTxFromHex(tx.Hex)
		if err != nil {
			return nil, fmt.Errorf("error decoding ticket %s tx hex: %v", h, err)
		}
		if len(msgTx.TxOut) == 0 {
			return nil, fmt.Errorf("malformed ticket transaction %s", h)
		}
		var fees uint64
		for _, txIn := range msgTx.TxIn {
			fees += uint64(txIn.ValueIn)
		}
		for _, txOut := range msgTx.TxOut {
			fees -= uint64(txOut.Value)
		}
		tickets[i] = &asset.Ticket{
			Tx: asset.TicketTransaction{
				Hash:        h.String(),
				TicketPrice: uint64(msgTx.TxOut[0].Value),
				Fees:        fees,
				Stamp:       now,
				BlockHeight: -1,
			},
			Status: asset.TicketStatusUnmined,
		}
	}
	return tickets, nil
}

var oldSPVWalletErr = errors.New("wallet is an older spv wallet")

// Tickets returns active tickets.
func (w *rpcWallet) Tickets(ctx context.Context) ([]*asset.Ticket, error) {
	return w.tickets(ctx, true)
}

func (w *rpcWallet) tickets(ctx context.Context, includeImmature bool) ([]*asset.Ticket, error) {
	// GetTickets only works for spv clients after version 9.2.0
	if w.spvMode && !w.hasSPVTicketFunctions {
		return nil, oldSPVWalletErr
	}
	hashes, err := w.rpcClient.GetTickets(ctx, includeImmature)
	if err != nil {
		return nil, err
	}
	tickets := make([]*asset.Ticket, 0, len(hashes))
	for _, h := range hashes {
		tx, err := w.client().GetTransaction(ctx, h)
		if err != nil {
			w.log.Errorf("GetTransaction error for ticket %s: %v", h, err)
			continue
		}
		blockHeight := int64(-1)
		// If the transaction is not yet mined we do not know the block hash.
		if tx.BlockHash != "" {
			blkHash, err := chainhash.NewHashFromStr(tx.BlockHash)
			if err != nil {
				w.log.Errorf("Invalid block hash %v for ticket %v: %w", tx.BlockHash, h, err)
				continue
			}
			// dcrwallet returns do not include the block height.
			hdr, err := w.client().GetBlockHeader(ctx, blkHash)
			if err != nil {
				w.log.Errorf("GetBlockHeader error for ticket %s: %v", h, err)
				continue
			}
			blockHeight = int64(hdr.Height)
		}
		msgTx, err := msgTxFromHex(tx.Hex)
		if err != nil {
			w.log.Errorf("Error decoding ticket %s tx hex: %v", h, err)
			continue
		}
		if len(msgTx.TxOut) < 1 {
			w.log.Errorf("No outputs for ticket %s", h)
			continue
		}
		// Fee is always negative.
		feeAmt, _ := dcrutil.NewAmount(-tx.Fee)

		tickets = append(tickets, &asset.Ticket{
			Tx: asset.TicketTransaction{
				Hash:        h.String(),
				TicketPrice: uint64(msgTx.TxOut[0].Value),
				Fees:        uint64(feeAmt),
				Stamp:       uint64(tx.Time),
				BlockHeight: blockHeight,
			},
			// The walletjson.GetTransactionResult returned from GetTransaction
			// actually has a TicketStatus string field, but it doesn't appear
			// to ever be populated by dcrwallet.
			// Status:  somehowConvertFromString(tx.TicketStatus),

			// Not sure how to get the spender through RPC.
			// Spender: ?,
		})
	}
	return tickets, nil
}

// VotingPreferences returns current wallet voting preferences.
func (w *rpcWallet) VotingPreferences(ctx context.Context) ([]*walletjson.VoteChoice, []*asset.TBTreasurySpend, []*walletjson.TreasuryPolicyResult, error) {
	// Get consensus vote choices.
	choices, err := w.rpcClient.GetVoteChoices(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get vote choices: %v", err)
	}
	voteChoices := make([]*walletjson.VoteChoice, len(choices.Choices))
	for i, v := range choices.Choices {
		vc := v
		voteChoices[i] = &vc
	}
	// Get tspend voting policy.
	const tSpendPolicyMethod = "tspendpolicy"
	var tSpendRes []walletjson.TSpendPolicyResult
	err = w.rpcClientRawRequest(ctx, tSpendPolicyMethod, nil, &tSpendRes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get treasury spend policy: %v", err)
	}
	tSpendPolicy := make([]*asset.TBTreasurySpend, len(tSpendRes))
	for i, tp := range tSpendRes {
		// TODO: Find a way to get the tspend total value? Probably only
		// possible with a full node and txindex.
		tSpendPolicy[i] = &asset.TBTreasurySpend{
			Hash:          tp.Hash,
			CurrentPolicy: tp.Policy,
		}
	}
	// Get treasury voting policy.
	const treasuryPolicyMethod = "treasurypolicy"
	var treasuryRes []walletjson.TreasuryPolicyResult
	err = w.rpcClientRawRequest(ctx, treasuryPolicyMethod, nil, &treasuryRes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get treasury policy: %v", err)
	}
	treasuryPolicy := make([]*walletjson.TreasuryPolicyResult, len(treasuryRes))
	for i, v := range treasuryRes {
		tp := v
		treasuryPolicy[i] = &tp
	}
	return voteChoices, tSpendPolicy, treasuryPolicy, nil
}

// SetVotingPreferences sets voting preferences.
//
// NOTE: Will fail for communication problems with VSPs unlike internal wallets.
func (w *rpcWallet) SetVotingPreferences(ctx context.Context, choices, tSpendPolicy,
	treasuryPolicy map[string]string) error {
	for k, v := range choices {
		if err := w.rpcClient.SetVoteChoice(ctx, k, v); err != nil {
			return fmt.Errorf("unable to set vote choice: %v", err)
		}
	}
	const setTSpendPolicyMethod = "settspendpolicy"
	for k, v := range tSpendPolicy {
		if err := w.rpcClientRawRequest(ctx, setTSpendPolicyMethod, anylist{k, v}, nil); err != nil {
			return fmt.Errorf("unable to set tspend policy: %v", err)
		}
	}
	const setTreasuryPolicyMethod = "settreasurypolicy"
	for k, v := range treasuryPolicy {
		if err := w.rpcClientRawRequest(ctx, setTreasuryPolicyMethod, anylist{k, v}, nil); err != nil {
			return fmt.Errorf("unable to set treasury policy: %v", err)
		}
	}
	return nil
}

func (w *rpcWallet) SetTxFee(ctx context.Context, feePerKB dcrutil.Amount) error {
	return w.rpcClient.SetTxFee(ctx, feePerKB)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via nodeRawRequest.
type anylist []any

// rpcClientRawRequest is used to marshal parameters and send requests to the
// RPC server via rpcClient.RawRequest. If `thing` is non-nil, the result will
// be marshaled into `thing`.
func (w *rpcWallet) rpcClientRawRequest(ctx context.Context, method string, args anylist, thing any) error {
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

func (w *rpcWallet) walletInfo(ctx context.Context) (*walletjson.WalletInfoResult, error) {
	var walletInfo walletjson.WalletInfoResult
	err := w.rpcClientRawRequest(ctx, methodWalletInfo, nil, &walletInfo)
	return &walletInfo, translateRPCCancelErr(err)
}

var _ ticketPager = (*rpcWallet)(nil)

func (w *rpcWallet) TicketPage(ctx context.Context, scanStart int32, n, skipN int) ([]*asset.Ticket, error) {
	return make([]*asset.Ticket, 0), nil
}
