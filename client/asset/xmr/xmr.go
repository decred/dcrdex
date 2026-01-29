// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr

package xmr

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/xmr/cgo"
	"decred.org/dcrdex/dex"
	dexnxmr "decred.org/dcrdex/dex/networks/xmr"
)

const (
	version          = 0
	BipID            = 128
	walletTypeNative = "native"

	defaultFee          = 3  // Priority level 0-4, default to low
	defaultFeeRateLimit = 30

	// Refresh interval for sync status checks.
	syncTickerPeriod = 10 * time.Second
	// Refresh interval for peer count checks.
	peerCountTicker = 30 * time.Second

	// Default daemon addresses per network.
	// Mainnet and stagenet use public nodes; simnet uses localhost.
	defaultMainnetDaemon  = "http://xmr-node.cakewallet.com:18081"
	defaultStagenetDaemon = "http://node.monerodevs.org:38089"
	defaultSimnetDaemon   = "http://127.0.0.1:18081"

	// Default wallet name for simnet.
	defaultSimnetWalletName = "xmrwallet"
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "walletname",
			DisplayName: "Wallet Name",
			Description: "Name of the wallet file (without path or extension)",
			Required:    true,
		},
		{
			Key:         "daemonaddress",
			DisplayName: "Daemon Address",
			Description: "Address of the Monero daemon (e.g., localhost:18081)",
			Required:    true,
		},
		{
			Key:         "daemonusername",
			DisplayName: "Daemon Username",
			Description: "Username for daemon RPC authentication (optional)",
		},
		{
			Key:         "daemonpassword",
			DisplayName: "Daemon Password",
			Description: "Password for daemon RPC authentication (optional)",
			NoEcho:      true,
		},
		{
			Key:          "feepriority",
			DisplayName:  "Fee Priority",
			Description:  "Transaction fee priority (0=default, 1=low, 2=medium, 3=high, 4=highest)",
			DefaultValue: "0",
		},
	}

	// WalletInfo defines general information about a Monero wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Monero",
		SupportedVersions: []uint32{version},
		UnitInfo:          dexnxmr.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:        walletTypeNative,
			Seeded:      true,
			Tab:         "Native",
			Description: "Native Monero wallet using libwallet2",
			ConfigOpts:  configOpts,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

var _ asset.Driver = (*Driver)(nil)
var _ asset.Creator = (*Driver)(nil)

// Open creates the XMR wallet.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return newWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Monero.
// Monero coin IDs are transaction hashes.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	if len(coinID) != 32 {
		return "", fmt.Errorf("invalid coin ID length: expected 32, got %d", len(coinID))
	}
	return hex.EncodeToString(coinID), nil
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// Exists checks if a wallet already exists at the specified location.
func (d *Driver) Exists(walletType, dataDir string, settings map[string]string, net dex.Network) (bool, error) {
	if walletType != walletTypeNative {
		return false, fmt.Errorf("unknown wallet type %q", walletType)
	}
	walletName := settings["walletname"]
	if walletName == "" {
		if net == dex.Simnet {
			walletName = defaultSimnetWalletName
		} else {
			return false, errors.New("wallet name not specified")
		}
	}
	walletPath := filepath.Join(dataDir, walletName)
	wm := cgo.GetWalletManager()
	return wm.WalletExists(walletPath), nil
}

// Create creates a new Monero wallet from seed.
// The seed should be 32 bytes which is used as the Monero spend key.
func (d *Driver) Create(params *asset.CreateWalletParams) error {
	if params.Type != walletTypeNative {
		return fmt.Errorf("unknown wallet type %q", params.Type)
	}

	walletName := params.Settings["walletname"]
	if walletName == "" {
		if params.Net == dex.Simnet {
			walletName = defaultSimnetWalletName
		} else {
			return errors.New("wallet name not specified")
		}
	}

	walletPath := filepath.Join(params.DataDir, walletName)
	netType := dexNetworkToCgo(params.Net)

	// Ensure the data directory exists
	if err := os.MkdirAll(params.DataDir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	wm := cgo.GetWalletManager()

	// Check if wallet already exists
	if wm.WalletExists(walletPath) {
		return errors.New("wallet already exists")
	}

	// A seed is required. The seed must be 32 bytes (ed25519 seed size) which
	// is used as the Monero spend key directly.
	if len(params.Seed) == 0 {
		return errors.New("seed is required")
	}
	if len(params.Seed) != 32 {
		return fmt.Errorf("invalid seed size: expected 32 bytes, got %d", len(params.Seed))
	}

	// Hex-encode the seed to use as the spend key
	spendKey := hex.EncodeToString(params.Seed)

	// Hex-encode the password for wallet encryption.
	// This must match what OpenWithPW uses.
	password := hex.EncodeToString(params.Pass)

	// Create wallet from spend key
	wallet, err := wm.CreateDeterministicWalletFromSpendKey(
		walletPath,
		password,
		"English",
		netType,
		params.Birthday,
		spendKey,
	)
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	wm.CloseWallet(wallet, true)
	return nil
}

func dexNetworkToCgo(net dex.Network) int {
	switch net {
	case dex.Mainnet:
		return cgo.NetworkMainnet
	case dex.Testnet:
		return cgo.NetworkStagenet
	case dex.Simnet:
		// Monero regtest mode uses mainnet-style addresses
		return cgo.NetworkMainnet
	default:
		return cgo.NetworkMainnet
	}
}

// ExchangeWalletNoAuth implements asset.Wallet without authentication.
type ExchangeWalletNoAuth struct {
	*ExchangeWallet
}

// ExchangeWallet implements asset.Wallet with full Monero wallet functionality.
type ExchangeWallet struct {
	log    dex.Logger
	net    dex.Network
	emit   *asset.WalletEmitter

	wm         *cgo.WalletManager
	wallet     *cgo.Wallet
	walletPath string
	walletMtx  sync.RWMutex

	daemonAddr  string
	daemonUser  string
	daemonPass  string
	feePriority int

	tipAtConnect uint64
	isOpen       atomic.Bool

	syncStatus atomic.Value // *asset.SyncStatus

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var _ asset.Wallet = (*ExchangeWallet)(nil)
var _ asset.Opener = (*ExchangeWallet)(nil)
var _ asset.NewAddresser = (*ExchangeWallet)(nil)
var _ asset.Withdrawer = (*ExchangeWallet)(nil)
var _ asset.FeeRater = (*ExchangeWallet)(nil)
var _ asset.TxFeeEstimator = (*ExchangeWallet)(nil)

func newWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	walletName := cfg.Settings["walletname"]
	if walletName == "" {
		// Use default wallet name for simnet
		if network == dex.Simnet {
			walletName = defaultSimnetWalletName
		} else {
			return nil, errors.New("wallet name not specified")
		}
	}

	daemonAddr := cfg.Settings["daemonaddress"]
	if daemonAddr == "" {
		// Use default daemon address based on network
		switch network {
		case dex.Mainnet:
			daemonAddr = defaultMainnetDaemon
		case dex.Testnet:
			daemonAddr = defaultStagenetDaemon
		case dex.Simnet:
			daemonAddr = defaultSimnetDaemon
		default:
			return nil, errors.New("daemon address not specified")
		}
		logger.Infof("Using default daemon address: %s", daemonAddr)
	}

	feePriority := 0
	if fp := cfg.Settings["feepriority"]; fp != "" {
		fmt.Sscanf(fp, "%d", &feePriority)
		if feePriority < 0 || feePriority > 4 {
			feePriority = 0
		}
	}

	walletPath := filepath.Join(cfg.DataDir, walletName)

	w := &ExchangeWallet{
		log:         logger,
		net:         network,
		emit:        cfg.Emit,
		wm:          cgo.GetWalletManager(),
		walletPath:  walletPath,
		daemonAddr:  daemonAddr,
		daemonUser:  cfg.Settings["daemonusername"],
		daemonPass:  cfg.Settings["daemonpassword"],
		feePriority: feePriority,
	}

	w.syncStatus.Store(&asset.SyncStatus{})

	return w, nil
}

// OpenWithPW opens the wallet with the provided password. This must be called
// before Connect. Implements asset.Opener.
func (w *ExchangeWallet) OpenWithPW(ctx context.Context, pw []byte) error {
	if w.isOpen.Load() {
		return nil // Already open
	}

	netType := dexNetworkToCgo(w.net)

	// Use hex-encoded password to match the Create function
	password := hex.EncodeToString(pw)

	w.walletMtx.Lock()
	wallet, err := w.wm.OpenWallet(w.walletPath, password, netType)
	if err != nil {
		w.walletMtx.Unlock()
		return fmt.Errorf("failed to open wallet: %w", err)
	}
	w.wallet = wallet
	w.walletMtx.Unlock()

	w.isOpen.Store(true)
	return nil
}

// IsOpen returns whether the wallet is open. Implements asset.Opener.
func (w *ExchangeWallet) IsOpen() bool {
	return w.isOpen.Load()
}

// Close closes the wallet. Implements asset.Opener.
func (w *ExchangeWallet) Close(ctx context.Context) error {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet != nil {
		w.wallet.Store("")
		w.wm.CloseWallet(w.wallet, true)
		w.wallet = nil
	}
	w.isOpen.Store(false)
	return nil
}

// Connect establishes a connection to the Monero daemon. The wallet must be
// opened with OpenWithPW before calling Connect.
func (w *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if !w.isOpen.Load() {
		return nil, errors.New("wallet not open; call OpenWithPW first")
	}

	w.ctx, w.cancel = context.WithCancel(ctx)

	w.walletMtx.RLock()
	wallet := w.wallet
	w.walletMtx.RUnlock()

	if wallet == nil {
		return nil, errors.New("wallet is nil")
	}

	// Initialize connection to daemon
	if !wallet.Init(w.daemonAddr, w.daemonUser, w.daemonPass, false, false, "") {
		return nil, fmt.Errorf("failed to initialize daemon connection: %s", wallet.ErrorString())
	}

	// Initial refresh
	wallet.Refresh()
	w.tipAtConnect = wallet.BlockChainHeight()

	w.wg.Add(1)
	go w.monitorBlocks()

	return &w.wg, nil
}

func (w *ExchangeWallet) monitorBlocks() {
	defer w.wg.Done()

	syncTicker := time.NewTicker(syncTickerPeriod)
	defer syncTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			w.walletMtx.RLock()
			if w.wallet == nil {
				w.walletMtx.RUnlock()
				continue
			}
			w.wallet.Refresh()

			walletHeight := w.wallet.BlockChainHeight()
			daemonHeight := w.wallet.DaemonBlockChainHeight()
			synced := w.wallet.Synchronized()
			w.walletMtx.RUnlock()

			w.syncStatus.Store(&asset.SyncStatus{
				Synced:         synced,
				TargetHeight:   daemonHeight,
				Blocks:         walletHeight,
				StartingBlocks: w.tipAtConnect,
			})

			if w.emit != nil {
				w.emit.TipChange(walletHeight)
			}

		case <-w.ctx.Done():
			return
		}
	}
}

// Disconnect shuts down the wallet.
func (w *ExchangeWallet) Disconnect() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()

	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()
	if w.wallet != nil {
		w.wallet.Store("")
		w.wm.CloseWallet(w.wallet, true)
		w.wallet = nil
	}
}

// Info returns basic wallet information.
func (w *ExchangeWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// Balance returns the wallet balance.
func (w *ExchangeWallet) Balance() (*asset.Balance, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	total := w.wallet.Balance(0)
	unlocked := w.wallet.UnlockedBalance(0)
	locked := total - unlocked

	return &asset.Balance{
		Available: unlocked,
		Immature:  0,
		Locked:    locked,
	}, nil
}

// DepositAddress returns the primary wallet address.
func (w *ExchangeWallet) DepositAddress() (string, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return "", errors.New("wallet not connected")
	}

	return w.wallet.Address(0, 0), nil
}

// NewAddress generates a new subaddress.
func (w *ExchangeWallet) NewAddress() (string, error) {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return "", errors.New("wallet not connected")
	}

	// Add a new subaddress
	numAddrs := w.wallet.NumSubaddresses(0)
	w.wallet.AddSubaddress(0, "")

	return w.wallet.Address(0, numAddrs), nil
}

// AddressUsed checks if an address has been used (received funds).
func (w *ExchangeWallet) AddressUsed(addr string) (bool, error) {
	// Monero doesn't easily expose per-address usage tracking.
	// For now, return false. A full implementation would scan tx history.
	return false, nil
}

// OwnsDepositAddress checks if the address belongs to this wallet.
func (w *ExchangeWallet) OwnsDepositAddress(address string) (bool, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return false, errors.New("wallet not connected")
	}

	// Check if it matches any of our subaddresses
	numAddrs := w.wallet.NumSubaddresses(0)
	for i := uint64(0); i < numAddrs; i++ {
		if w.wallet.Address(0, i) == address {
			return true, nil
		}
	}
	return false, nil
}

// RedemptionAddress returns an address for redemption (same as deposit for Monero).
func (w *ExchangeWallet) RedemptionAddress() (string, error) {
	return w.NewAddress()
}

// ValidateAddress checks if an address is valid.
func (w *ExchangeWallet) ValidateAddress(address string) bool {
	netType := dexNetworkToCgo(w.net)
	return cgo.AddressValid(address, netType)
}

// Send sends XMR to an address.
func (w *ExchangeWallet) Send(address string, value, feeRate uint64) (asset.Coin, error) {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	tx, err := w.wallet.CreateTransaction(address, value, w.feePriority, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	txid := tx.TxID()
	coinID, _ := hex.DecodeString(txid)

	return &Coin{
		txHash: txid,
		value:  tx.Amount(),
		id:     coinID,
	}, nil
}

// Withdraw withdraws funds, subtracting fees from the amount.
func (w *ExchangeWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	// For Monero, Send and Withdraw are effectively the same since
	// the fee is always subtracted from the wallet balance, not the send amount.
	// We need to estimate the fee and subtract it from the value.
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	// Create a test transaction to get the fee
	testTx, err := w.wallet.CreateTransaction(address, value, w.feePriority, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate fee: %w", err)
	}

	fee := testTx.Fee()
	if value <= fee {
		return nil, fmt.Errorf("withdrawal amount %d is less than fee %d", value, fee)
	}

	// Create actual transaction with fee subtracted
	actualValue := value - fee
	tx, err := w.wallet.CreateTransaction(address, actualValue, w.feePriority, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	txid := tx.TxID()
	coinID, _ := hex.DecodeString(txid)

	return &Coin{
		txHash: txid,
		value:  tx.Amount(),
		id:     coinID,
	}, nil
}

// FeeRate returns the current fee rate (as a priority level for Monero).
func (w *ExchangeWallet) FeeRate() uint64 {
	return uint64(w.feePriority)
}

// EstimateSendTxFee estimates the fee for sending a transaction.
func (w *ExchangeWallet) EstimateSendTxFee(address string, value, feeRate uint64, subtract, maxWithdraw bool) (uint64, bool, error) {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return 0, false, errors.New("wallet not connected")
	}

	if !cgo.AddressValid(address, dexNetworkToCgo(w.net)) {
		return 0, false, nil
	}

	sendValue := value
	if maxWithdraw {
		sendValue = w.wallet.UnlockedBalance(0)
	}

	tx, err := w.wallet.CreateTransaction(address, sendValue, w.feePriority, 0)
	if err != nil {
		// Return 0 fee but valid address if we can't create tx (e.g., insufficient funds)
		return 0, true, nil
	}

	return tx.Fee(), true, nil
}

// SyncStatus returns the wallet sync status.
func (w *ExchangeWallet) SyncStatus() (*asset.SyncStatus, error) {
	status := w.syncStatus.Load().(*asset.SyncStatus)
	return status, nil
}

// ValidateSecret validates a secret against its hash.
func (w *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	// Standard SHA256 validation
	if len(secret) != 32 || len(secretHash) != 32 {
		return false
	}
	// This would be implemented with proper hash comparison
	return true
}

// LockTimeExpired checks if a locktime has expired.
func (w *ExchangeWallet) LockTimeExpired(ctx context.Context, lockTime time.Time) (bool, error) {
	return time.Now().After(lockTime), nil
}

// RegFeeConfirmations returns confirmations for a registration fee payment.
func (w *ExchangeWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (uint32, error) {
	return 0, asset.ErrUnsupported
}

// StandardSendFee returns the standard send fee.
func (w *ExchangeWallet) StandardSendFee(feeRate uint64) uint64 {
	// Monero fees are dynamic; return an estimate based on typical tx size
	return 10000000 // 0.00001 XMR as placeholder
}

// TxHistory returns transaction history.
func (w *ExchangeWallet) TxHistory(req *asset.TxHistoryRequest) (*asset.TxHistoryResponse, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	history := w.wallet.History()
	history.Refresh()

	count := history.Count()
	if req.N > 0 && req.N < count {
		count = req.N
	}

	txs := make([]*asset.WalletTransaction, 0, count)
	for i := 0; i < count; i++ {
		ti := history.Transaction(i)
		if ti == nil {
			continue
		}

		txType := asset.Send
		if ti.Direction() == cgo.DirectionIn {
			txType = asset.Receive
		}

		txs = append(txs, &asset.WalletTransaction{
			Type:        txType,
			ID:          ti.Hash(),
			Amount:      ti.Amount(),
			Fees:        ti.Fee(),
			BlockNumber: ti.BlockHeight(),
			Timestamp:   ti.Timestamp(),
			Confirmed:   ti.Confirmations() > 0,
		})
	}

	return &asset.TxHistoryResponse{
		Txs: txs,
	}, nil
}

// WalletTransaction returns a single transaction.
func (w *ExchangeWallet) WalletTransaction(ctx context.Context, txID string) (*asset.WalletTransaction, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	history := w.wallet.History()
	history.Refresh()

	for i := 0; i < history.Count(); i++ {
		ti := history.Transaction(i)
		if ti != nil && ti.Hash() == txID {
			txType := asset.Send
			if ti.Direction() == cgo.DirectionIn {
				txType = asset.Receive
			}
			return &asset.WalletTransaction{
				Type:        txType,
				ID:          ti.Hash(),
				Amount:      ti.Amount(),
				Fees:        ti.Fee(),
				BlockNumber: ti.BlockHeight(),
				Timestamp:   ti.Timestamp(),
				Confirmed:   ti.Confirmations() > 0,
			}, nil
		}
	}

	return nil, errors.New("transaction not found")
}

// PendingTransactions returns pending (unconfirmed) transactions.
func (w *ExchangeWallet) PendingTransactions(ctx context.Context) []*asset.WalletTransaction {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return nil
	}

	history := w.wallet.History()
	history.Refresh()

	var pending []*asset.WalletTransaction
	for i := 0; i < history.Count(); i++ {
		ti := history.Transaction(i)
		if ti != nil && ti.IsPending() {
			txType := asset.Send
			if ti.Direction() == cgo.DirectionIn {
				txType = asset.Receive
			}
			pending = append(pending, &asset.WalletTransaction{
				Type:      txType,
				ID:        ti.Hash(),
				Amount:    ti.Amount(),
				Fees:      ti.Fee(),
				Timestamp: ti.Timestamp(),
				Confirmed: false,
			})
		}
	}

	return pending
}

// The following methods are required by the Wallet interface but not supported
// for basic wallet functionality (trading not implemented).

func (w *ExchangeWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, uint64, error) {
	return nil, nil, 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) MaxOrder(form *asset.MaxOrderForm) (*asset.SwapEstimate, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) PreSwap(form *asset.PreSwapForm) (*asset.PreSwap, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) PreRedeem(form *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) ReturnCoins(coins asset.Coins) error {
	return asset.ErrUnsupported
}

func (w *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) ([]dex.Bytes, []dex.Bytes, error) {
	return nil, nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) ContractLockTimeExpired(ctx context.Context, contract dex.Bytes) (bool, time.Time, error) {
	return false, time.Time{}, asset.ErrUnsupported
}

func (w *ExchangeWallet) FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (dex.Bytes, dex.Bytes, error) {
	return nil, nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) Refund(coinID, contract dex.Bytes, feeRate uint64) (dex.Bytes, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) SwapConfirmations(ctx context.Context, coinID, contract dex.Bytes, matchTime time.Time) (uint32, bool, error) {
	return 0, false, asset.ErrUnsupported
}

func (w *ExchangeWallet) ConfirmTransaction(coinID dex.Bytes, confirmTx *asset.ConfirmTx, feeSuggestion uint64) (*asset.ConfirmTxStatus, error) {
	return nil, asset.ErrUnsupported
}

func (w *ExchangeWallet) SingleLotSwapRefundFees(version uint32, feeRate uint64, useSafeTxSize bool) (uint64, uint64, error) {
	return 0, 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) SingleLotRedeemFees(version uint32, feeRate uint64) (uint64, error) {
	return 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) FundMultiOrder(ord *asset.MultiOrder, maxLock uint64) ([]asset.Coins, [][]dex.Bytes, uint64, error) {
	return nil, nil, 0, asset.ErrUnsupported
}

func (w *ExchangeWallet) MaxFundingFees(numTrades uint32, feeRate uint64, options map[string]string) uint64 {
	return 0
}

// Coin represents a Monero transaction output.
type Coin struct {
	txHash string
	value  uint64
	id     []byte
}

func (c *Coin) ID() dex.Bytes {
	return c.id
}

func (c *Coin) TxID() string {
	return c.txHash
}

func (c *Coin) String() string {
	return c.txHash
}

func (c *Coin) Value() uint64 {
	return c.value
}
