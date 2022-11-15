package btc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wallet/txauthor"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
)

const (
	dbTimeout = 20 * time.Second
)

// btcSPVWallet implements BTCWallet for Bitcoin.
type btcSPVWallet struct {
	*wallet.Wallet
	chainParams *chaincfg.Params
	log         dex.Logger
	dir         string
	birthdayV   atomic.Value // time.Time
	// if allowAutomaticRescan is true, if when connect is called,
	// spvWallet.birthday is earlier than the birthday stored in the btcwallet
	// database, the transaction history will be wiped and a rescan will start.
	allowAutomaticRescan bool

	// Below fields are populated in Start.
	loader      *wallet.Loader
	chainClient *chain.NeutrinoClient
	cl          *neutrino.ChainService
	neutrinoDB  walletdb.DB

	// rescanStarting is set while reloading the wallet and dropping
	// transactions from the wallet db.
	rescanStarting uint32 // atomic

	peerManager *SPVPeerManager
}

var _ BTCWallet = (*btcSPVWallet)(nil)

// createSPVWallet creates a new SPV wallet.
func createSPVWallet(privPass []byte, seed []byte, bday time.Time, dataDir string, log dex.Logger, extIdx, intIdx uint32, net *chaincfg.Params) error {
	dir := filepath.Join(dataDir, net.Name, "spv")
	if err := logNeutrino(dir); err != nil {
		return fmt.Errorf("error initializing btcwallet+neutrino logging: %w", err)
	}

	logDir := filepath.Join(dataDir, net.Name, logDirName)
	err := os.MkdirAll(logDir, 0744)
	if err != nil {
		return fmt.Errorf("error creating wallet directories: %w", err)
	}

	loader := wallet.NewLoader(net, dir, true, dbTimeout, 250)

	pubPass := []byte(wallet.InsecurePubPassphrase)

	btcw, err := loader.CreateNewWallet(pubPass, privPass, seed, bday)
	if err != nil {
		return fmt.Errorf("CreateNewWallet error: %w", err)
	}

	bailOnWallet := func() {
		if err := loader.UnloadWallet(); err != nil {
			log.Errorf("Error unloading wallet after createSPVWallet error: %v", err)
		}
	}

	if extIdx > 0 || intIdx > 0 {
		err = extendAddresses(extIdx, intIdx, btcw)
		if err != nil {
			bailOnWallet()
			return fmt.Errorf("failed to set starting address indexes: %w", err)
		}
	}

	// The chain service DB
	neutrinoDBPath := filepath.Join(dir, neutrinoDBName)
	db, err := walletdb.Create("bdb", neutrinoDBPath, true, dbTimeout)
	if err != nil {
		bailOnWallet()
		return fmt.Errorf("unable to create neutrino db at %q: %w", neutrinoDBPath, err)
	}
	if err = db.Close(); err != nil {
		bailOnWallet()
		return fmt.Errorf("error closing newly created wallet database: %w", err)
	}

	if err := loader.UnloadWallet(); err != nil {
		return fmt.Errorf("error unloading wallet: %w", err)
	}

	return nil
}

// openSPVWallet is the BTCWalletConstructor for Bitcoin.
func openSPVWallet(dir string, cfg *WalletConfig,
	chainParams *chaincfg.Params, log dex.Logger) BTCWallet {

	w := &btcSPVWallet{
		dir:                  dir,
		chainParams:          chainParams,
		log:                  log,
		allowAutomaticRescan: !cfg.ActivelyUsed,
	}
	w.birthdayV.Store(cfg.AdjustedBirthday())
	return w
}

func (w *btcSPVWallet) Birthday() time.Time {
	return w.birthdayV.Load().(time.Time)
}

func (w *btcSPVWallet) updateDBBirthday(bday time.Time) error {
	btcw, isLoaded := w.loader.LoadedWallet()
	if !isLoaded {
		return fmt.Errorf("wallet not loaded")
	}
	return walletdb.Update(btcw.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(wAddrMgrBkt)
		return btcw.Manager.SetBirthday(ns, bday)
	})
}

// Start initializes the *btcwallet.Wallet and its supporting players and
// starts syncing.
func (w *btcSPVWallet) Start() (SPVService, error) {
	if err := logNeutrino(w.dir); err != nil {
		return nil, fmt.Errorf("error initializing btcwallet+neutrino logging: %v", err)
	}
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	w.loader = wallet.NewLoader(w.chainParams, w.dir, true, dbTimeout, 250)

	exists, err := w.loader.WalletExists()
	if err != nil {
		return nil, fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return nil, errors.New("wallet not found")
	}

	w.log.Debug("Starting native BTC wallet...")
	btcw, err := w.loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
	if err != nil {
		return nil, fmt.Errorf("couldn't load wallet: %w", err)
	}

	errCloser := dex.NewErrorCloser()
	defer errCloser.Done(w.log)
	errCloser.Add(w.loader.UnloadWallet)

	neutrinoDBPath := filepath.Join(w.dir, neutrinoDBName)
	w.neutrinoDB, err = walletdb.Create("bdb", neutrinoDBPath, true, dbTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}
	errCloser.Add(w.neutrinoDB.Close)

	w.log.Debug("Starting neutrino chain service...")
	w.cl, err = neutrino.NewChainService(neutrino.Config{
		DataDir:       w.dir,
		Database:      w.neutrinoDB,
		ChainParams:   *w.chainParams,
		PersistToDisk: true, // keep cfilter headers on disk for efficient rescanning
		// AddPeers:      addPeers,
		// ConnectPeers:  connectPeers,
		// WARNING: PublishTransaction currently uses the entire duration
		// because if an external bug, but even if the resolved, a typical
		// inv/getdata round trip is ~4 seconds, so we set this so neutrino does
		// not cancel queries too readily.
		BroadcastTimeout: 6 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create Neutrino ChainService: %w", err)
	}
	errCloser.Add(w.cl.Stop)

	var defaultPeers []string
	switch w.chainParams.Net {
	case wire.MainNet:
		defaultPeers = []string{"cfilters.ssgen.io:8333"}
	case wire.TestNet3:
		defaultPeers = []string{"dex-test.ssgen.io:18333"}
	case wire.TestNet, wire.SimNet: // plain "wire.TestNet" is regnet!
		defaultPeers = []string{"127.0.0.1:20575"}
	}
	peerManager := NewSPVPeerManager(&btcChainService{w.cl}, defaultPeers, w.dir, w.log, w.chainParams.DefaultPort)
	w.peerManager = peerManager

	w.chainClient = chain.NewNeutrinoClient(w.chainParams, w.cl)
	w.Wallet = btcw

	oldBday := btcw.Manager.Birthday()

	performRescan := w.Birthday().Before(oldBday)
	if performRescan && !w.allowAutomaticRescan {
		return nil, errors.New("cannot set earlier birthday while there are active deals")
	}

	if !oldBday.Equal(w.Birthday()) {
		if err := w.updateDBBirthday(w.Birthday()); err != nil {
			w.log.Errorf("Failed to reset wallet manager birthday: %v", err)
			performRescan = false
		}
	}

	if performRescan {
		w.ForceRescan()
	}

	if err = w.chainClient.Start(); err != nil { // lazily starts connmgr
		return nil, fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.log.Info("Synchronizing wallet with network...")
	btcw.SynchronizeRPC(w.chainClient)

	errCloser.Success()

	w.peerManager.ConnectToInitialWalletPeers()

	return &btcChainService{w.cl}, nil
}

// Stop stops the wallet and database threads.
func (w *btcSPVWallet) Stop() {
	w.log.Info("Unloading wallet")
	if err := w.loader.UnloadWallet(); err != nil {
		w.log.Errorf("UnloadWallet error: %v", err)
	}
	if w.chainClient != nil {
		w.log.Trace("Stopping neutrino client chain interface")
		w.chainClient.Stop()
		w.chainClient.WaitForShutdown()
	}
	w.log.Trace("Stopping neutrino chain sync service")
	if err := w.cl.Stop(); err != nil {
		w.log.Errorf("error stopping neutrino chain service: %v", err)
	}
	w.log.Trace("Stopping neutrino DB.")
	if err := w.neutrinoDB.Close(); err != nil {
		w.log.Errorf("wallet db close error: %v", err)
	}

	// NOTE: Do we need w.Wallet.Stop()

	w.log.Info("SPV wallet closed")
}

func (w *btcSPVWallet) Reconfigure(cfg *asset.WalletConfig, _ /* currentAddress */ string) (restartRequired bool, err error) {
	parsedCfg := new(WalletConfig)
	if err = config.Unmapify(cfg.Settings, parsedCfg); err != nil {
		return
	}

	newBday := parsedCfg.AdjustedBirthday()
	if newBday.Equal(w.Birthday()) {
		// It's the only setting we care about.
		return
	}
	rescanRequired := newBday.Before(w.Birthday())
	if rescanRequired && parsedCfg.ActivelyUsed {
		return false, errors.New("cannot decrease the birthday with active orders")
	}
	if err := w.updateDBBirthday(newBday); err != nil {
		return false, fmt.Errorf("error storing new birthday: %w", err)
	}
	w.birthdayV.Store(newBday)
	if rescanRequired {
		if err = w.RescanAsync(); err != nil {
			return false, fmt.Errorf("error initiating rescan after birthday adjustment: %w", err)
		}
	}
	return
}

// RescanAsync initiates a full wallet recovery (used address discovery
// and transaction scanning) by stopping the btcwallet, dropping the transaction
// history from the wallet db, resetting the synced-to height of the wallet
// manager, restarting the wallet and its chain client, and finally commanding
// the wallet to resynchronize, which starts asynchronous wallet recovery.
// Progress of the rescan should be monitored with syncStatus. During the rescan
// wallet balances and known transactions may not be reported accurately or
// located. The SPVService is not stopped, so most spvWallet methods will
// continue to work without error, but methods using the btcWallet will likely
// return incorrect results or errors.
func (w *btcSPVWallet) RescanAsync() error {
	if !atomic.CompareAndSwapUint32(&w.rescanStarting, 0, 1) {
		w.log.Error("rescan already in progress")
	}
	defer atomic.StoreUint32(&w.rescanStarting, 0)
	w.log.Info("Stopping wallet and chain client...")
	w.Wallet.Stop() // stops Wallet and chainClient (not chainService)
	w.Wallet.WaitForShutdown()
	w.chainClient.WaitForShutdown()

	w.ForceRescan()

	w.log.Info("Starting wallet...")
	w.Wallet.Start()

	if err := w.chainClient.Start(); err != nil {
		return fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.log.Info("Synchronizing wallet with network...")
	w.Wallet.SynchronizeRPC(w.chainClient)
	return nil
}

// ForceRescan forces a full rescan with active address discovery on wallet
// restart by dropping the complete transaction history and setting the
// "synced to" field to nil. See the btcwallet/cmd/dropwtxmgr app for more
// information.
func (w *btcSPVWallet) ForceRescan() {
	wdb := w.Wallet.Database()

	w.log.Info("Dropping transaction history to perform full rescan...")
	err := wallet.DropTransactionHistory(wdb, false)
	if err != nil {
		w.log.Errorf("Failed to drop wallet transaction history: %v", err)
		// Continue to attempt restarting the wallet anyway.
	}

	err = walletdb.Update(wdb, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(wAddrMgrBkt)      // it'll be fine
		return w.Wallet.Manager.SetSyncedTo(ns, nil) // never synced, forcing recover from birthday
	})
	if err != nil {
		w.log.Errorf("Failed to reset wallet manager sync height: %v", err)
	}
}

// WalletTransaction pulls the transaction from the database.
func (w *btcSPVWallet) WalletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, WalletTransactionNotFound
	}

	return details, nil
}

func (w *btcSPVWallet) SyncedTo() waddrmgr.BlockStamp {
	return w.Wallet.Manager.SyncedTo()
}

// getWalletBirthdayBlock retrieves the wallet's birthday block.
//
// NOTE: The wallet birthday block hash is NOT SET until the chain service
// passes the birthday block and the wallet looks it up based on the birthday
// Time and the downloaded block headers.
// This is presently unused, but I have plans for it with a wallet rescan.
// func (w *btcSPVWallet) getWalletBirthdayBlock() (*waddrmgr.BlockStamp, error) {
// 	var birthdayBlock waddrmgr.BlockStamp
// 	err := walletdb.View(w.Database(), func(dbtx walletdb.ReadTx) error {
// 		ns := dbtx.ReadBucket([]byte("waddrmgr")) // it'll be fine
// 		var err error
// 		birthdayBlock, _, err = w.Manager.BirthdayBlock(ns)
// 		return err
// 	})
// 	if err != nil {
// 		return nil, err // sadly, waddrmgr.ErrBirthdayBlockNotSet is expected during most of the chain sync
// 	}
// 	return &birthdayBlock, nil
// }

// SignTx signs the transaction inputs.
func (w *btcSPVWallet) SignTx(tx *wire.MsgTx) error {
	var prevPkScripts [][]byte
	var inputValues []btcutil.Amount
	for _, txIn := range tx.TxIn {
		// NOTE: The BitcoinCash implementation of BTCWallet ONLY produces the
		// *wire.TxOut.
		_, txOut, _, _, err := w.Wallet.FetchInputInfo(&txIn.PreviousOutPoint)
		if err != nil {
			return err
		}
		inputValues = append(inputValues, btcutil.Amount(txOut.Value))
		prevPkScripts = append(prevPkScripts, txOut.PkScript)
		// Zero the previous witness and signature script or else
		// AddAllInputScripts does some weird stuff.
		txIn.SignatureScript = nil
		txIn.Witness = nil
	}
	return txauthor.AddAllInputScripts(tx, prevPkScripts, inputValues, &secretSource{w, w.chainParams})
}

func (w *btcSPVWallet) BlockNotifications(ctx context.Context) <-chan *BlockNotification {
	cl := w.Wallet.NtfnServer.TransactionNotifications()
	ch := make(chan *BlockNotification, 1)
	go func() {
		defer cl.Done()
		for {
			select {
			case note := <-cl.C:
				if len(note.AttachedBlocks) > 0 {
					lastBlock := note.AttachedBlocks[len(note.AttachedBlocks)-1]
					select {
					case ch <- &BlockNotification{
						Hash:   *lastBlock.Hash,
						Height: lastBlock.Height,
					}:
					default:
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func (w *btcSPVWallet) AddPeer(addr string) error {
	return w.peerManager.AddPeer(addr)
}

func (w *btcSPVWallet) RemovePeer(addr string) error {
	return w.peerManager.RemovePeer(addr)
}

func (w *btcSPVWallet) Peers() ([]*asset.WalletPeer, error) {
	return w.peerManager.Peers()
}

// secretSource is used to locate keys and redemption scripts while signing a
// transaction. secretSource satisfies the txauthor.SecretsSource interface.
type secretSource struct {
	w           *btcSPVWallet
	chainParams *chaincfg.Params
}

// ChainParams returns the chain parameters.
func (s *secretSource) ChainParams() *chaincfg.Params {
	return s.chainParams
}

// GetKey fetches a private key for the specified address.
func (s *secretSource) GetKey(addr btcutil.Address) (*btcec.PrivateKey, bool, error) {
	ma, err := s.w.Wallet.AddressInfo(addr)
	if err != nil {
		return nil, false, err
	}

	mpka, ok := ma.(waddrmgr.ManagedPubKeyAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedPubKeyAddress", addr, ma)
		return nil, false, e
	}

	privKey, err := mpka.PrivKey()
	if err != nil {
		return nil, false, err
	}
	return privKey, ma.Compressed(), nil
}

// GetScript fetches the redemption script for the specified p2sh/p2wsh address.
func (s *secretSource) GetScript(addr btcutil.Address) ([]byte, error) {
	ma, err := s.w.Wallet.AddressInfo(addr)
	if err != nil {
		return nil, err
	}

	msa, ok := ma.(waddrmgr.ManagedScriptAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedScriptAddress", addr, ma)
		return nil, e
	}
	return msa.Script()
}

// logNeutrino initializes logging in the neutrino + wallet packages. Logging
// only has to be initialized once, so an atomic flag is used internally to
// return early on subsequent invocations.
//
// In theory, the rotating file logger must be Closed at some point, but
// there are concurrency issues with that since btcd and btcwallet have
// unsupervised goroutines still running after shutdown. So we leave the rotator
// running at the risk of losing some logs.
func logNeutrino(dir string) error {
	if !atomic.CompareAndSwapUint32(&loggingInited, 0, 1) {
		return nil
	}

	logSpinner, err := logRotator(dir)
	if err != nil {
		return fmt.Errorf("error initializing log rotator: %w", err)
	}

	backendLog := btclog.NewBackend(logWriter{logSpinner})

	logger := func(name string, lvl btclog.Level) btclog.Logger {
		l := backendLog.Logger(name)
		l.SetLevel(lvl)
		return l
	}

	neutrino.UseLogger(logger("NTRNO", btclog.LevelDebug))
	wallet.UseLogger(logger("BTCW", btclog.LevelInfo))
	wtxmgr.UseLogger(logger("TXMGR", btclog.LevelInfo))
	chain.UseLogger(logger("CHAIN", btclog.LevelInfo))

	return nil
}
