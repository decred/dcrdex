// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr

package xmr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/xmr/cxmr"
	"decred.org/dcrdex/dex"
	dexnxmr "decred.org/dcrdex/dex/networks/xmr"
)

const (
	version          = 0
	BipID            = 128
	walletTypeNative = "native"

	defaultFee          = 3 // Priority level 0-4, default to low
	defaultFeeRateLimit = 30

	// Refresh interval for sync status checks during normal operation.
	syncTickerPeriod = 10 * time.Second
	// Faster polling during initial sync for progress updates.
	initialSyncTickerPeriod = 2 * time.Second
	// Refresh interval for peer count checks.
	peerCountTicker = 30 * time.Second

	// Default daemon addresses per network.
	// Mainnet uses public node; testnet/simnet use localhost.
	// TODO: Find a working public testnet node.
	defaultMainnetDaemon = "http://xmr-node.cakewallet.com:18081"
	defaultTestnetDaemon = "http://127.0.0.1:28081"
	defaultSimnetDaemon  = "http://127.0.0.1:18081"

	// Default wallet name for simnet.
	defaultSimnetWalletName = "xmrwallet"

	walletName     = "xmr"
	walletMetaFile = "xmr_meta.json"
)

// walletMeta stores persistent wallet metadata that wallet2 doesn't preserve.
// This follows Cake Wallet's approach of storing recovery info separately.
type walletMeta struct {
	// RestoreHeight is the block height to start scanning from.
	RestoreHeight uint64 `json:"restoreHeight"`
	// Birthday is the original unix timestamp used to calculate RestoreHeight.
	Birthday uint64 `json:"birthday"`
	// IsRecovery indicates this wallet needs recovery scanning.
	IsRecovery bool `json:"isRecovery"`
}

var (
	configOpts = []*asset.ConfigOption{
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
	walletPath := filepath.Join(dataDir, walletName)
	wm := cxmr.GetWalletManager()
	return wm.WalletExists(walletPath), nil
}

// Create creates a new Monero wallet from seed.
// The seed should be 32 bytes which is used as the Monero spend key.
func (d *Driver) Create(params *asset.CreateWalletParams) error {
	if params.Type != walletTypeNative {
		return fmt.Errorf("unknown wallet type %q", params.Type)
	}

	walletPath := filepath.Join(params.DataDir, walletName)
	netType := dexNetworkToCgo(params.Net)

	// Ensure the data directory exists
	if err := os.MkdirAll(params.DataDir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	wm := cxmr.GetWalletManager()

	// If wallet files already exist from a previous failed attempt, remove
	// them so we can try again. The wallet is deterministic from the seed, so
	// nothing is lost.
	if wm.WalletExists(walletPath) {
		for _, f := range []string{walletPath, walletPath + ".keys", filepath.Join(params.DataDir, walletMetaFile)} {
			os.Remove(f)
		}
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

	restoreHeight := birthdayToHeight(params.Birthday, params.Net)

	if params.Logger != nil {
		params.Logger.Infof("Creating XMR wallet at %s with restore height %d", walletPath, restoreHeight)
	}

	// Create wallet from spend key. The restoreHeight passed here sets the
	// wallet's creation height but we also need to explicitly set the recovery
	// state and refresh height for proper blockchain scanning.
	wallet, err := wm.CreateDeterministicWalletFromSpendKey(
		walletPath,
		password,
		"English",
		netType,
		restoreHeight,
		spendKey,
	)
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	// Mark the wallet as recovering from seed and set the refresh height.
	// This is critical for proper blockchain scanning - without this, the
	// wallet may not scan from the correct height. This follows Cake Wallet's
	// approach of explicitly setting these values after wallet creation.
	wallet.SetRecoveringFromSeed(true)
	wallet.SetRefreshFromBlockHeight(restoreHeight)

	// Store the wallet to persist the recovery state and refresh height.
	if !wallet.Store("") {
		if params.Logger != nil {
			params.Logger.Warnf("Failed to store wallet after setting recovery state: %s", wallet.ErrorString())
		}
	}

	// Log the wallet address for debugging
	if params.Logger != nil {
		addr := wallet.Address(0, 0)
		actualRestoreHeight := wallet.GetRefreshFromBlockHeight()
		params.Logger.Infof("Created XMR wallet with primary address: %s, restore height: %d",
			addr, actualRestoreHeight)
	}

	wm.CloseWallet(wallet, true)

	// Save wallet metadata separately. The wallet2 library doesn't reliably
	// persist the recovery state and refresh height, so we store them in our
	// own metadata file (like Cake Wallet does with walletInfo).
	meta := &walletMeta{
		RestoreHeight: restoreHeight,
		Birthday:      params.Birthday,
		IsRecovery:    true,
	}
	if err := saveWalletMeta(params.DataDir, meta); err != nil {
		return fmt.Errorf("failed to save wallet metadata: %w", err)
	}

	return nil
}

func dexNetworkToCgo(net dex.Network) cxmr.NetworkType {
	switch net {
	case dex.Mainnet:
		return cxmr.NetworkMainnet
	case dex.Testnet:
		// Use Monero testnet instead of stagenet - stagenet has address
		// validation bugs in monero_c.
		// TODO: stagenet is supposedly more stable and meant for consumers
		// like us so we should switch to that whenever the library can
		// handle it. When testing validate address and send, which accepts
		// an address both fail because of the address values.
		return cxmr.NetworkTestnet
	case dex.Simnet:
		// Monero regtest mode uses mainnet-style addresses (prefix 4/8).
		// We use allow_mismatched_daemon_version to handle hard fork checks.
		return cxmr.NetworkMainnet
	default:
		return cxmr.NetworkMainnet
	}
}

// ExchangeWallet implements asset.Wallet with full Monero wallet functionality.
type ExchangeWallet struct {
	log  dex.Logger
	net  dex.Network
	emit *asset.WalletEmitter

	wm         *cxmr.WalletManager
	wallet     *cxmr.Wallet
	walletPath string
	dataDir    string
	walletMtx  sync.RWMutex

	daemonAddr  string
	daemonUser  string
	daemonPass  string
	feePriority cxmr.Priority

	tipAtConnect  uint64
	lastPeerCount uint32
	peersChange   func(uint32, error)
	isOpen        atomic.Bool
	rescanning    atomic.Bool

	syncStatus atomic.Value // *asset.SyncStatus

	// Monero's sweep skips outputs below the marginal fee cost of spending
	// them (ignore_fractional_outputs), but these dust outputs are still
	// included in the wallet's unlocked balance. We cache their total so
	// we can subtract it when reporting balance, ensuring users never see
	// a balance they cannot actually send. The cache avoids expensive
	// per-output CGO enumeration on every balance poll.
	dustMtx        sync.Mutex
	cachedDust     uint64
	cachedDustTime time.Time

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
var _ asset.Rescanner = (*ExchangeWallet)(nil)

func newWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	daemonAddr := cfg.Settings["daemonaddress"]
	if daemonAddr == "" {
		// Use default daemon address based on network
		switch network {
		case dex.Mainnet:
			daemonAddr = defaultMainnetDaemon
		case dex.Testnet:
			daemonAddr = defaultTestnetDaemon
		case dex.Simnet:
			daemonAddr = defaultSimnetDaemon
		default:
			return nil, errors.New("daemon address not specified")
		}
		logger.Infof("Using default daemon address: %s", daemonAddr)
	}

	var feePriority cxmr.Priority
	if fp := cfg.Settings["feepriority"]; fp != "" {
		var p int
		fmt.Sscanf(fp, "%d", &p)
		if p >= 0 && p <= 4 {
			feePriority = cxmr.Priority(p)
		}
	}

	walletPath := filepath.Join(cfg.DataDir, walletName)

	w := &ExchangeWallet{
		log:         logger,
		net:         network,
		emit:        cfg.Emit,
		wm:          cxmr.GetWalletManager(),
		walletPath:  walletPath,
		dataDir:     cfg.DataDir,
		daemonAddr:  daemonAddr,
		daemonUser:  cfg.Settings["daemonusername"],
		daemonPass:  cfg.Settings["daemonpassword"],
		feePriority: feePriority,
		peersChange: cfg.PeersChange,
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
func (w *ExchangeWallet) Close() error {
	w.log.Info("Closing xmr wallet ...")
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet != nil {
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

	// For local/simnet daemons, mark as trusted and allow mismatched daemon
	// version (required for regtest where daemon starts at hard fork v16 from
	// block 0, but wallet expects mainnet hard fork schedule).
	// TODO: This is made possible with a patch on monero_c, but it looks
	// like it is in monero master so should be part of the library at some point.
	if w.net == dex.Simnet {
		wallet.SetTrustedDaemon(true)
		wallet.SetAllowMismatchedDaemonVersion(true)
	}

	// Verify connection status
	connStatus := wallet.Connected()
	switch connStatus {
	case cxmr.ConnectionDisconnected:
		return nil, errors.New("failed to connect to daemon: disconnected")
	case cxmr.ConnectionWrongVersion:
		if w.net == dex.Mainnet {
			return nil, errors.New("daemon version mismatch: please update your Monero daemon")
		}
		w.log.Warnf("Daemon version mismatch, continuing anyway")
	}

	// Get initial heights for sync status.
	walletHeight := wallet.BlockChainHeight()
	daemonHeight := wallet.DaemonBlockChainHeight()
	synced := wallet.Synchronized()
	w.tipAtConnect = walletHeight

	// Load wallet metadata and apply recovery settings if needed.
	// The wallet2 library doesn't reliably persist setRecoveringFromSeed and
	// setRefreshFromBlockHeight, so we store this info separately and re-apply
	// it on every connect (like Cake Wallet does).
	meta, err := loadWalletMeta(w.dataDir)
	if err != nil {
		w.log.Warnf("Failed to load wallet metadata: %v", err)
	}
	if meta != nil && meta.IsRecovery {
		// Apply recovery settings. Cake Wallet checks if getCurrentHeight() <= 1,
		// but we apply whenever IsRecovery is true and wallet isn't synced yet.
		w.log.Infof("Wallet is in recovery mode, setting refresh from block height %d", meta.RestoreHeight)
		wallet.SetRecoveringFromSeed(true)
		wallet.SetRefreshFromBlockHeight(meta.RestoreHeight)
		w.tipAtConnect = meta.RestoreHeight
	}

	refreshHeight := wallet.GetRefreshFromBlockHeight()
	w.log.Debugf("Wallet refresh from block height: %d", refreshHeight)

	// Set initial sync status before starting monitors.
	w.syncStatus.Store(&asset.SyncStatus{
		Synced:         synced,
		TargetHeight:   daemonHeight,
		Blocks:         walletHeight,
		StartingBlocks: w.tipAtConnect,
	})

	if !synced {
		blocksRemaining := daemonHeight - walletHeight
		w.log.Infof("XMR wallet syncing: %d blocks remaining (wallet: %d, daemon: %d)",
			blocksRemaining, walletHeight, daemonHeight)
	} else {
		w.log.Infof("XMR wallet synchronized at height %d", walletHeight)
	}

	// Start background refresh thread. This handles blockchain synchronization
	// automatically without blocking. We also trigger an immediate async refresh
	// to start syncing right away.
	wallet.StartRefresh()
	wallet.RefreshAsync()

	w.wg.Add(2)
	go w.monitorBlocks()
	go w.monitorPeers()

	return &w.wg, nil
}

func (w *ExchangeWallet) monitorBlocks() {
	defer w.wg.Done()

	// Use faster polling during initial sync for better progress reporting.
	// The actual refresh is handled by the background thread started via
	// StartRefresh() in Connect(). We just poll the status here.
	syncTicker := time.NewTicker(initialSyncTickerPeriod)
	defer syncTicker.Stop()

	var lastHeight uint64
	synced := false

	for {
		select {
		case <-syncTicker.C:
			w.walletMtx.RLock()
			if w.wallet == nil {
				w.walletMtx.RUnlock()
				continue
			}

			// Don't call Refresh() here - it blocks and prevents shutdown.
			// The background refresh thread (started via StartRefresh) handles
			// blockchain synchronization. We just poll the current status.
			walletHeight := w.wallet.BlockChainHeight()
			daemonHeight := w.wallet.DaemonBlockChainHeight()
			walletSynced := w.wallet.Synchronized()
			w.walletMtx.RUnlock()

			// If we're rescanning, don't trust the wallet's sync status.
			// We're only synced when not rescanning AND wallet reports synced.
			rescanning := w.rescanning.Load()
			nowSynced := walletSynced && !rescanning

			// If rescanning and wallet height has caught up, clear the flag.
			if rescanning && walletHeight >= daemonHeight && daemonHeight > 0 {
				w.rescanning.Store(false)
				rescanning = false
				nowSynced = walletSynced
				w.log.Infof("Rescan complete at height %d", walletHeight)
			}

			w.syncStatus.Store(&asset.SyncStatus{
				Synced:         nowSynced,
				TargetHeight:   daemonHeight,
				Blocks:         walletHeight,
				StartingBlocks: w.tipAtConnect,
			})

			// Once synced, switch to slower polling interval.
			if nowSynced && !synced {
				synced = true
				syncTicker.Reset(syncTickerPeriod)
				w.log.Infof("XMR wallet synchronized at height %d", walletHeight)
				// Save immediately when sync completes.
				w.saveWallet()
				// Mark recovery as complete so we don't re-apply recovery
				// settings on next connect.
				w.clearRecoveryState()
			} else if !nowSynced && synced {
				// If we were synced but now rescanning, reset to fast polling.
				synced = false
				syncTicker.Reset(initialSyncTickerPeriod)
			}

			// Emit tip change if height changed.
			if walletHeight != lastHeight {
				lastHeight = walletHeight
				if w.emit != nil {
					w.emit.TipChange(walletHeight)
				}
			}

		case <-w.ctx.Done():
			// Pause the background refresh thread FIRST to stop any ongoing
			// rescan/refresh operations. This must happen before saveWallet()
			// because Store() can block if a refresh is in progress.
			w.wallet.PauseRefresh()
			// Now save the wallet state.
			w.saveWallet()
			return
		}
	}
}

// saveWallet persists the wallet state to disk.
func (w *ExchangeWallet) saveWallet() {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()
	if w.wallet != nil {
		height := w.wallet.BlockChainHeight()
		if ok := w.wallet.Store(""); !ok {
			w.log.Errorf("Failed to save wallet at height %d: %s", height, w.wallet.ErrorString())
		} else {
			w.log.Tracef("Saved wallet at height %d", height)
		}
	}
}

// clearRecoveryState updates the metadata to mark recovery as complete.
func (w *ExchangeWallet) clearRecoveryState() {
	meta, err := loadWalletMeta(w.dataDir)
	if err != nil {
		w.log.Warnf("Failed to load wallet meta to clear recovery state: %v", err)
		return
	}
	if meta == nil || !meta.IsRecovery {
		return // Nothing to clear
	}
	meta.IsRecovery = false
	if err := saveWalletMeta(w.dataDir, meta); err != nil {
		w.log.Warnf("Failed to save wallet meta after clearing recovery state: %v", err)
	} else {
		w.log.Infof("Recovery complete, cleared recovery state")
	}
}

func (w *ExchangeWallet) monitorPeers() {
	defer w.wg.Done()

	ticker := time.NewTicker(peerCountTicker)
	defer ticker.Stop()

	for {
		w.checkPeers()

		select {
		case <-ticker.C:
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *ExchangeWallet) checkPeers() {
	if w.peersChange == nil {
		return
	}

	w.walletMtx.RLock()
	wallet := w.wallet
	w.walletMtx.RUnlock()

	if wallet == nil {
		w.peersChange(0, nil)
		return
	}

	// Report 1 peer if we're connected to the daemon, 0 otherwise.
	// The daemon's RPC doesn't reliably report its p2p peer count to us.
	var numPeers uint32
	if wallet.Connected() == cxmr.ConnectionConnected {
		numPeers = 1
	}

	prevPeer := atomic.SwapUint32(&w.lastPeerCount, numPeers)
	if prevPeer != numPeers {
		w.peersChange(numPeers, nil)
	}
}

// Info returns basic wallet information.
func (w *ExchangeWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// Balance returns the wallet balance. Outputs whose value is below the
// dust threshold are excluded from Available, since Monero's sweep
// (create_transactions_all) skips fractional outputs that cost more in
// fees to spend than they are worth.
func (w *ExchangeWallet) Balance() (*asset.Balance, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	total := w.wallet.Balance(0)
	unlocked := w.wallet.UnlockedBalance(0)
	dust := w.dustTotal()

	return &asset.Balance{
		Available: unlocked - dust,
		Immature:  total - unlocked,
	}, nil
}

// dustThreshold is the minimum output value considered spendable. Outputs
// below this are ignored by Monero's sweep (ignore_fractional_outputs)
// because spending them costs more in fees than they are worth. This
// value is conservative and covers all fee priority levels.
const dustThreshold = 1_000_000_000 // 0.001 XMR

// dustTotalFromCoins computes the sum of all unlocked, unspent outputs below
// the dust threshold for account 0. This is expensive since it enumerates all
// wallet outputs via CGO.
func (w *ExchangeWallet) dustTotalFromCoins() uint64 {
	allCoins := w.wallet.AllCoins()
	var dust uint64
	for _, c := range allCoins {
		if c.Spent || !c.Unlocked || c.Frozen || c.SubaddrAcct != 0 {
			continue
		}
		if c.Amount < dustThreshold {
			dust += c.Amount
		}
	}
	return dust
}

const dustCacheDuration = 30 * time.Second

// dustTotal returns the cached dust total, refreshing it if the cache is stale.
func (w *ExchangeWallet) dustTotal() uint64 {
	w.dustMtx.Lock()
	defer w.dustMtx.Unlock()
	if time.Since(w.cachedDustTime) < dustCacheDuration {
		return w.cachedDust
	}
	w.cachedDust = w.dustTotalFromCoins()
	w.cachedDustTime = time.Now()
	return w.cachedDust
}

// DepositAddress returns a new subaddress for receiving funds.
// Each call creates a new subaddress (starting with "8" on mainnet/simnet).
// This matches the behavior of the previous RPC-based implementation.
func (w *ExchangeWallet) DepositAddress() (string, error) {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return "", errors.New("wallet not connected")
	}

	// Create a new subaddress for this deposit.
	// Subaddresses start with "8" on mainnet (vs "4" for primary address).
	numAddrs := w.wallet.NumSubaddresses(0)
	w.wallet.AddSubaddress(0, "")

	addr := w.wallet.Address(0, numAddrs)
	w.log.Debugf("Created new deposit subaddress %d: %s", numAddrs, addr)

	return addr, nil
}

// NewAddress generates a new subaddress. Same as DepositAddress.
func (w *ExchangeWallet) NewAddress() (string, error) {
	return w.DepositAddress()
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
	return cxmr.AddressValid(address, netType)
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

	return w.commitTx(tx, value)
}

// Withdraw withdraws funds, subtracting fees from the amount.
func (w *ExchangeWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	// For Monero, Withdraw subtracts the fee from value, sending
	// (value - fee) to the recipient.
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return nil, errors.New("wallet not connected")
	}

	unlocked := w.wallet.UnlockedBalance(0)
	dust := w.dustTotalFromCoins() // Fresh computation for accuracy.
	spendable := unlocked - dust

	// Max withdraw detection. When value >= spendable, sweep the wallet
	// using createTransactionMultDest with amount_sweep_all. This
	// calls create_transactions_all which sends the spendable balance
	// minus fee. Monero's ignore_fractional_outputs setting (enabled
	// by default) skips outputs whose value is less than the marginal
	// fee cost of including them as inputs. These dust outputs are
	// excluded from the reported balance (see dustThreshold) so the
	// user never sees a balance they cannot send.
	if value >= spendable {
		w.log.Debugf("Max withdraw (sweep): unlocked=%d, dust=%d, spendable=%d, value=%d", unlocked, dust, spendable, value)

		tx, err := w.wallet.SweepAll(address, w.feePriority, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to create sweep transaction: %w", err)
		}

		fee := tx.Fee()
		amount := tx.Amount()
		txid := tx.TxID()
		w.log.Debugf("Max withdraw pre-commit: amount=%d, fee=%d, txid=%s, txCount=%d", amount, fee, txid, tx.TxCount())

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to broadcast sweep transaction: %w", err)
		}

		// Sweep transactions via createTransactionMultDest may not
		// populate Amount() or TxID() correctly.
		if txid == "" {
			txid = tx.TxID()
		}
		if txid == "" {
			// Get txid from the most recent outgoing transaction.
			h := w.wallet.History()
			h.Refresh()
			if n := h.Count(); n > 0 {
				if ti := h.Transaction(n - 1); ti != nil {
					txid = ti.Hash()
					w.log.Debugf("Max withdraw: got txid from history: %s", txid)
				}
			}
		}
		if amount == 0 && fee > 0 {
			amount = spendable - fee
		}

		w.log.Debugf("Max withdraw committed: amount=%d, fee=%d, txid=%s", amount, fee, txid)
		coinID, _ := hex.DecodeString(txid)
		return &Coin{
			txHash: txid,
			value:  amount,
			id:     coinID,
		}, nil
	}

	// For partial withdrawals, iteratively find the right send amount.
	// The fee depends on the transaction structure (number of
	// inputs/outputs), which can change when the send amount crosses a
	// threshold that requires more inputs or outputs.
	feeEstimate, err := w.estimateFee(address, value)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate fee: %w", err)
	}

	seenFees := make(map[uint64]bool)
	var lastActualFee uint64

	for i := 0; i < 8; i++ {
		if value <= feeEstimate {
			return nil, fmt.Errorf("withdrawal amount %d is less than fee %d", value, feeEstimate)
		}

		sendAmount := value - feeEstimate
		tx, err := w.wallet.CreateTransaction(address, sendAmount, w.feePriority, 0)
		if err != nil {
			if lastActualFee > 0 {
				feeEstimate = lastActualFee + 1
				lastActualFee = 0
			} else {
				feeEstimate *= 2
			}
			continue
		}

		actualFee := tx.Fee()
		lastActualFee = actualFee

		if actualFee == feeEstimate {
			return w.commitTx(tx, sendAmount)
		}

		if seenFees[actualFee] {
			return w.commitTx(tx, sendAmount)
		}

		seenFees[actualFee] = true
		feeEstimate = actualFee
	}

	return nil, fmt.Errorf("failed to create withdrawal transaction for %d after fee adjustments", value)
}

// estimateFee creates a test transaction to estimate the fee. If creating a
// test tx for the full value fails (e.g. because value + fee > balance), a
// reduced value is used. The fee may differ from the actual transaction's fee
// if a different number of inputs is selected; callers should verify and retry.
func (w *ExchangeWallet) estimateFee(address string, value uint64) (uint64, error) {
	testTx, err := w.wallet.CreateTransaction(address, value, w.feePriority, 0)
	if err == nil {
		return testTx.Fee(), nil
	}
	// The full value likely exceeds what the wallet can send (value + fee >
	// balance). Use 3/4 of the unlocked balance to get a fee estimate.
	unlocked := w.wallet.UnlockedBalance(0)
	testValue := unlocked * 3 / 4
	if testValue == 0 {
		return 0, fmt.Errorf("insufficient unlocked balance")
	}
	testTx, err = w.wallet.CreateTransaction(address, testValue, w.feePriority, 0)
	if err != nil {
		return 0, err
	}
	return testTx.Fee(), nil
}

// commitTx commits a pending transaction and returns the resulting Coin.
// sendAmount is the expected send amount, used as fallback if the library
// returns 0 for Amount().
func (w *ExchangeWallet) commitTx(tx *cxmr.PendingTransaction, sendAmount uint64) (*Coin, error) {
	// Capture values before commit — the library may clear them after.
	amount := tx.Amount()
	fee := tx.Fee()
	txid := tx.TxID()

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to broadcast transaction: %w", err)
	}

	// Try post-commit if pre-commit was empty.
	if txid == "" {
		txid = tx.TxID()
	}
	if txid == "" {
		h := w.wallet.History()
		h.Refresh()
		if n := h.Count(); n > 0 {
			if ti := h.Transaction(n - 1); ti != nil {
				txid = ti.Hash()
			}
		}
	}
	if amount == 0 {
		amount = sendAmount
	}

	w.log.Debugf("commitTx: amount=%d, fee=%d, txid=%s", amount, fee, txid)
	coinID, _ := hex.DecodeString(txid)
	return &Coin{
		txHash: txid,
		value:  amount,
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

	if !cxmr.AddressValid(address, dexNetworkToCgo(w.net)) {
		return 0, false, nil
	}

	sendValue := value
	if maxWithdraw {
		sendValue = w.wallet.UnlockedBalance(0)
	}

	// For max withdraw or subtract, use estimateFee which falls back to a
	// reduced test value if sendValue + fee > balance.
	if maxWithdraw || subtract {
		fee, err := w.estimateFee(address, sendValue)
		if err != nil {
			return 0, true, nil
		}
		return fee, true, nil
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

// ValidateSecret checks that the secret hashes to the secret hash.
func (w *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
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
		if ti.Direction() == cxmr.DirectionIn {
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
			if ti.Direction() == cxmr.DirectionIn {
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
		if ti == nil || ti.IsFailed() {
			continue
		}
		// Use Confirmations() == 0 rather than IsPending() because
		// wallet2's IsPending() does not return true for incoming pool
		// transactions.
		if ti.Confirmations() > 0 {
			continue
		}
		txType := asset.Send
		if ti.Direction() == cxmr.DirectionIn {
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

	return pending
}

// Rescan performs a wallet rescan from the specified birthday. The birthday
// parameter is a unix timestamp in seconds, which is converted to an approximate
// block height. Passing 0 will rescan from genesis.
// Implements asset.Rescanner.
func (w *ExchangeWallet) Rescan(_ context.Context, bday uint64) error {
	return w.rescanFromHeight(birthdayToHeight(bday, w.net), bday)
}

// birthdayToHeight converts a unix timestamp (seconds) to an approximate
// block height for the current network. Returns 0 if bday is 0 or before
// the network genesis.
func birthdayToHeight(bday uint64, net dex.Network) uint64 {
	if bday == 0 {
		return 0
	}

	// Monero has ~2 minute block times across all networks.
	const avgBlockTime uint64 = 120 // 2 minutes

	// Genesis timestamps for each network. These are approximate timestamps
	// of when each network started producing blocks.
	var genesisTime uint64
	switch net {
	case dex.Mainnet:
		// Mainnet genesis: April 18, 2014
		genesisTime = 1397818193
	case dex.Testnet:
		// Testnet launched alongside mainnet in April 2014.
		genesisTime = 1397818193
	case dex.Simnet:
		// Simnet/regtest: local network, always scan from genesis
		return 0
	default:
		// Unknown network, use mainnet genesis as fallback
		genesisTime = 1397818193
	}

	if bday < genesisTime {
		return 0
	}

	return (bday - genesisTime) / avgBlockTime
}

// saveWalletMeta saves wallet metadata to a JSON file.
func saveWalletMeta(dataDir string, meta *walletMeta) error {
	metaPath := filepath.Join(dataDir, walletMetaFile)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal wallet meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write wallet meta: %w", err)
	}
	return nil
}

// loadWalletMeta loads wallet metadata from a JSON file.
// Returns nil if the file doesn't exist.
func loadWalletMeta(dataDir string) (*walletMeta, error) {
	metaPath := filepath.Join(dataDir, walletMetaFile)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read wallet meta: %w", err)
	}
	var meta walletMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal wallet meta: %w", err)
	}
	return &meta, nil
}

// rescanFromHeight performs a wallet rescan from a specific block height.
func (w *ExchangeWallet) rescanFromHeight(height, bday uint64) error {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return errors.New("wallet not connected")
	}

	// Update our metadata file with the new restore height.
	meta := &walletMeta{
		RestoreHeight: height,
		Birthday:      bday,
		IsRecovery:    true,
	}
	if err := saveWalletMeta(w.dataDir, meta); err != nil {
		w.log.Warnf("Failed to save wallet metadata: %v", err)
	}

	// Mark as recovering and set the refresh height. This is the same flow
	// used by Cake Wallet when recovering/rescanning. SetRecoveringFromSeed
	// tells the wallet2 library to scan for transactions from the specified
	// height rather than just checking for new blocks.
	w.wallet.SetRecoveringFromSeed(true)
	w.wallet.SetRefreshFromBlockHeight(height)

	// Get daemon height before triggering rescan.
	daemonHeight := w.wallet.DaemonBlockChainHeight()

	// Mark as rescanning so monitorBlocks doesn't report synced status.
	w.rescanning.Store(true)

	// RescanBlockchainAsync sets m_refreshShouldRescan=true which causes
	// doRefresh to call wallet2::rescan_blockchain(), clearing cached
	// transfers, key images, and payments before rescanning.
	w.wallet.RescanBlockchainAsync()

	// Update sync status to reflect the rescan.
	w.tipAtConnect = height
	w.syncStatus.Store(&asset.SyncStatus{
		Synced:         false,
		TargetHeight:   daemonHeight,
		Blocks:         height,
		StartingBlocks: height,
	})

	w.log.Infof("Started rescan from block height %d (target: %d)", height, daemonHeight)
	return nil
}

// PrimaryAddress returns the wallet's primary address (starting with "4").
// Note: For receiving funds, use DepositAddress() which creates subaddresses.
func (w *ExchangeWallet) PrimaryAddress() (string, error) {
	w.walletMtx.RLock()
	defer w.walletMtx.RUnlock()

	if w.wallet == nil {
		return "", errors.New("wallet not connected")
	}

	return w.wallet.Address(0, 0), nil
}

// GenerateSubaddresses pre-generates a specified number of subaddresses.
func (w *ExchangeWallet) GenerateSubaddresses(count uint64) error {
	w.walletMtx.Lock()
	defer w.walletMtx.Unlock()

	if w.wallet == nil {
		return errors.New("wallet not connected")
	}

	for i := uint64(0); i < count; i++ {
		w.wallet.AddSubaddress(0, "")
	}

	w.wallet.Store("")
	return nil
}

// RecoverWithSubaddresses generates subaddresses and rescans from block 0.
func (w *ExchangeWallet) RecoverWithSubaddresses(subaddressCount uint64) error {
	if err := w.GenerateSubaddresses(subaddressCount); err != nil {
		return fmt.Errorf("failed to generate subaddresses: %w", err)
	}
	return w.rescanFromHeight(0, 0)
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
