// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ltc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/gcs"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcwallet/waddrmgr"
	btcwallet "github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wtxmgr"
	neutrino "github.com/dcrlabs/neutrino-ltc"
	labschain "github.com/dcrlabs/neutrino-ltc/chain"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
	btcneutrino "github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
	ltcchaincfg "github.com/ltcsuite/ltcd/chaincfg"
	ltcchainhash "github.com/ltcsuite/ltcd/chaincfg/chainhash"
	"github.com/ltcsuite/ltcd/ltcutil"
	ltctxscript "github.com/ltcsuite/ltcd/txscript"
	ltcwire "github.com/ltcsuite/ltcd/wire"
	"github.com/ltcsuite/ltcwallet/chain"
	ltcwaddrmgr "github.com/ltcsuite/ltcwallet/waddrmgr"
	"github.com/ltcsuite/ltcwallet/wallet"
	"github.com/ltcsuite/ltcwallet/wallet/txauthor"
	"github.com/ltcsuite/ltcwallet/walletdb"
	_ "github.com/ltcsuite/ltcwallet/walletdb/bdb"
	ltcwtxmgr "github.com/ltcsuite/ltcwallet/wtxmgr"
)

const (
	DefaultM        uint64 = 784931 // From ltcutil. Used for gcs filters.
	logDirName             = "logs"
	neutrinoDBName         = "neutrino.db"
	defaultAcctNum         = 0
	defaultAcctName        = "default"
	dbTimeout              = 20 * time.Second
)

var (
	waddrmgrNamespace = []byte("waddrmgr")
	wtxmgrNamespace   = []byte("wtxmgr")

	testnet4Seeds = [][]byte{
		{0x12, 0xc0, 0x38, 0x95, 0x87, 0x4b},
		{0x3, 0x47, 0x1e, 0x2e, 0x87, 0x4b},
		{0x22, 0x59, 0x4e, 0x2d, 0x87, 0x4b},
		{0x22, 0x8c, 0xc5, 0x98, 0x87, 0x4b},
	}
)

// ltcSPVWallet is an implementation of btc.BTCWallet that runs a native
// Litecoin SPV Wallet. ltcSPVWallet mostly just translates types from the
// btcsuite types to ltcsuite and vice-versa. Startup and shutdown are notable
// exceptions, and have some critical code that needed to be duplicated (in
// order to avoid interface hell).
type ltcSPVWallet struct {
	// This section is populated in openSPVWallet.
	dir                  string
	chainParams          *ltcchaincfg.Params
	btcParams            *chaincfg.Params
	log                  dex.Logger
	birthdayV            atomic.Value // time.Time
	allowAutomaticRescan bool

	// This section is populated in Start.
	*wallet.Wallet
	chainClient *labschain.NeutrinoClient
	cl          *neutrino.ChainService
	loader      *wallet.Loader
	neutrinoDB  walletdb.DB

	peerManager *btc.SPVPeerManager
}

var _ btc.BTCWallet = (*ltcSPVWallet)(nil)

// openSPVWallet creates a ltcSPVWallet, but does not Start.
func openSPVWallet(dir string, cfg *btc.WalletConfig, btcParams *chaincfg.Params, log dex.Logger) btc.BTCWallet {
	var ltcParams *ltcchaincfg.Params
	switch btcParams.Name {
	case dexltc.MainNetParams.Name:
		ltcParams = &ltcchaincfg.MainNetParams
	case dexltc.TestNet4Params.Name:
		ltcParams = &ltcchaincfg.TestNet4Params
	case dexltc.RegressionNetParams.Name:
		ltcParams = &ltcchaincfg.RegressionNetParams
	}
	w := &ltcSPVWallet{
		dir:                  dir,
		chainParams:          ltcParams,
		btcParams:            btcParams,
		log:                  log,
		allowAutomaticRescan: !cfg.ActivelyUsed,
	}
	w.birthdayV.Store(cfg.AdjustedBirthday())
	return w
}

// createSPVWallet creates a new SPV wallet.
func createSPVWallet(privPass []byte, seed []byte, bday time.Time, walletDir string, log dex.Logger, extIdx, intIdx uint32, net *ltcchaincfg.Params) error {
	if err := logNeutrino(walletDir, log); err != nil {
		return fmt.Errorf("error initializing dcrwallet+neutrino logging: %w", err)
	}

	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	loader := wallet.NewLoader(net, walletDir, true, dbTimeout, 250)

	pubPass := []byte(wallet.InsecurePubPassphrase)

	btcw, err := loader.CreateNewWallet(pubPass, privPass, seed, bday)
	if err != nil {
		return fmt.Errorf("CreateNewWallet error: %w", err)
	}

	errCloser := dex.NewErrorCloser()
	defer errCloser.Done(log)
	errCloser.Add(loader.UnloadWallet)

	if extIdx > 0 || intIdx > 0 {
		err = extendAddresses(extIdx, intIdx, btcw)
		if err != nil {
			return fmt.Errorf("failed to set starting address indexes: %w", err)
		}
	}

	// The chain service DB
	neutrinoDBPath := filepath.Join(walletDir, neutrinoDBName)
	db, err := walletdb.Create("bdb", neutrinoDBPath, true, dbTimeout)
	if err != nil {
		return fmt.Errorf("unable to create neutrino db at %q: %w", neutrinoDBPath, err)
	}
	if err = db.Close(); err != nil {
		return fmt.Errorf("error closing newly created wallet database: %w", err)
	}

	if err := loader.UnloadWallet(); err != nil {
		return fmt.Errorf("error unloading wallet: %w", err)
	}

	errCloser.Success()
	return nil
}

// AccountInfo returns the account information of the wallet for use by the
// exchange wallet.
func (w *ltcSPVWallet) AccountInfo() btc.XCWalletAccount {
	return btc.XCWalletAccount{
		AccountName:   defaultAcctName,
		AccountNumber: defaultAcctNum,
	}
}

// walletParams works around a bug in ltcwallet that doesn't recognize
// wire.TestNet4 in (*ScopedKeyManager).cloneKeyWithVersion which is called from
// AccountProperties. Only do this for the *wallet.Wallet, not the
// *neutrino.ChainService.
func (w *ltcSPVWallet) walletParams() *ltcchaincfg.Params {
	if w.chainParams.Name != ltcchaincfg.TestNet4Params.Name {
		return w.chainParams
	}
	spoofParams := *w.chainParams
	spoofParams.Net = ltcwire.TestNet3
	return &spoofParams
}

// Start initializes the *ltcwallet.Wallet and its supporting players and starts
// syncing.
func (w *ltcSPVWallet) Start() (btc.SPVService, error) {
	if err := logNeutrino(w.dir, w.log); err != nil {
		return nil, fmt.Errorf("error initializing dcrwallet+neutrino logging: %v", err)
	}
	// recoverWindow arguments borrowed from ltcwallet directly.

	w.loader = wallet.NewLoader(w.walletParams(), w.dir, true, dbTimeout, 250)

	exists, err := w.loader.WalletExists()
	if err != nil {
		return nil, fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return nil, errors.New("wallet not found")
	}

	w.log.Debug("Starting native LTC wallet...")
	w.Wallet, err = w.loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
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
		// WARNING: PublishTransaction currently uses the entire duration
		// because if an external bug, but even if the resolved, a typical
		// inv/getdata round trip is ~4 seconds, so we set this so neutrino does
		// not cancel queries too readily.
		BroadcastTimeout: 6 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create Neutrino ChainService: %v", err)
	}
	errCloser.Add(w.cl.Stop)

	w.chainClient = labschain.NewNeutrinoClient(w.chainParams, w.cl, &logAdapter{w.log})

	oldBday := w.Manager.Birthday()
	wdb := w.Database()

	performRescan := w.Birthday().Before(oldBday)
	if performRescan && !w.allowAutomaticRescan {
		return nil, errors.New("cannot set earlier birthday while there are active deals")
	}

	if !oldBday.Equal(w.Birthday()) {
		err = walletdb.Update(wdb, func(dbtx walletdb.ReadWriteTx) error {
			ns := dbtx.ReadWriteBucket(waddrmgrNamespace)
			return w.Manager.SetBirthday(ns, w.Birthday())
		})
		if err != nil {
			w.log.Errorf("Failed to reset wallet manager birthday: %v", err)
			performRescan = false
		}
	}

	if performRescan {
		w.ForceRescan()
	}

	var defaultPeers []string
	switch w.chainParams.Net {
	case ltcwire.TestNet4:
		defaultPeers = []string{"127.0.0.1:19335"}
		for _, host := range testnet4Seeds {
			var addr netip.AddrPort
			addr.UnmarshalBinary(host)
			defaultPeers = append(defaultPeers, addr.String())
		}
	case ltcwire.TestNet, ltcwire.SimNet: // plain "wire.TestNet" is regnet!
		defaultPeers = []string{"127.0.0.1:20585"}
	}
	peerManager := btc.NewSPVPeerManager(&spvService{w.cl}, defaultPeers, w.dir, w.log, w.chainParams.DefaultPort)
	w.peerManager = peerManager

	if err = w.chainClient.Start(); err != nil { // lazily starts connmgr
		return nil, fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.log.Info("Synchronizing wallet with network...")
	w.SynchronizeRPC(w.chainClient)

	errCloser.Success()

	w.peerManager.ConnectToInitialWalletPeers()

	return &spvService{w.cl}, nil
}

func (w *ltcSPVWallet) Birthday() time.Time {
	return w.birthdayV.Load().(time.Time)
}

func (w *ltcSPVWallet) Reconfigure(cfg *asset.WalletConfig, _ /* oldAddress */ string) (restart bool, err error) {
	parsedCfg := new(btc.WalletConfig)
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

func (w *ltcSPVWallet) updateDBBirthday(bday time.Time) error {
	btcw, isLoaded := w.loader.LoadedWallet()
	if !isLoaded {
		return fmt.Errorf("wallet not loaded")
	}
	return walletdb.Update(btcw.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(waddrmgrNamespace)
		return btcw.Manager.SetBirthday(ns, bday)
	})
}

func (w *ltcSPVWallet) txDetails(txHash *ltcchainhash.Hash) (*ltcwtxmgr.TxDetails, error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, btc.WalletTransactionNotFound
	}

	return details, nil
}

func (w *ltcSPVWallet) addrLTC2BTC(addr ltcutil.Address) (btcutil.Address, error) {
	return btcutil.DecodeAddress(addr.String(), w.btcParams)
}

func (w *ltcSPVWallet) addrBTC2LTC(addr btcutil.Address) (ltcutil.Address, error) {
	return ltcutil.DecodeAddress(addr.String(), w.chainParams)
}

func (w *ltcSPVWallet) PublishTransaction(btcTx *wire.MsgTx, label string) error {
	ltcTx, err := convertMsgTxToLTC(btcTx)
	if err != nil {
		return err
	}

	return w.Wallet.PublishTransaction(ltcTx, label)
}

func (w *ltcSPVWallet) CalculateAccountBalances(account uint32, confirms int32) (btcwallet.Balances, error) {
	bals, err := w.Wallet.CalculateAccountBalances(account, confirms)
	if err != nil {
		return btcwallet.Balances{}, err
	}
	return btcwallet.Balances{
		Total:          btcutil.Amount(bals.Total),
		Spendable:      btcutil.Amount(bals.Spendable),
		ImmatureReward: btcutil.Amount(bals.ImmatureReward),
	}, nil
}

func (w *ltcSPVWallet) ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error) {
	res, err := w.Wallet.ListSinceBlock(start, end, syncHeight)
	if err != nil {
		return nil, err
	}

	btcRes := make([]btcjson.ListTransactionsResult, len(res))
	for i, r := range res {
		btcRes[i] = btcjson.ListTransactionsResult{
			Abandoned:         r.Abandoned,
			Account:           r.Account,
			Address:           r.Address,
			Amount:            r.Amount,
			BIP125Replaceable: r.BIP125Replaceable,
			BlockHash:         r.BlockHash,
			BlockHeight:       r.BlockHeight,
			BlockIndex:        r.BlockIndex,
			BlockTime:         r.BlockTime,
			Category:          r.Category,
			Confirmations:     r.Confirmations,
			Fee:               r.Fee,
			Generated:         r.Generated,
			InvolvesWatchOnly: r.InvolvesWatchOnly,
			Label:             r.Label,
			Time:              r.Time,
			TimeReceived:      r.TimeReceived,
			Trusted:           r.Trusted,
			TxID:              r.TxID,
			Vout:              r.Vout,
			WalletConflicts:   r.WalletConflicts,
			Comment:           r.Comment,
			OtherAccount:      r.OtherAccount,
		}
	}

	return btcRes, nil
}

func (w *ltcSPVWallet) ListUnspent(minconf, maxconf int32, acctName string) ([]*btcjson.ListUnspentResult, error) {
	// ltcwallet's ListUnspent takes either a list of addresses, or else returns
	// all non-locked unspent outputs for all accounts. We need to iterate the
	// results anyway to convert type.
	uns, err := w.Wallet.ListUnspent(minconf, maxconf, acctName)
	if err != nil {
		return nil, err
	}

	outs := make([]*btcjson.ListUnspentResult, len(uns))
	for i, u := range uns {
		if u.Account != acctName {
			continue
		}
		outs[i] = &btcjson.ListUnspentResult{
			TxID:          u.TxID,
			Vout:          u.Vout,
			Address:       u.Address,
			Account:       u.Account,
			ScriptPubKey:  u.ScriptPubKey,
			RedeemScript:  u.RedeemScript,
			Amount:        u.Amount,
			Confirmations: u.Confirmations,
			Spendable:     u.Spendable,
		}
	}

	return outs, nil
}

// FetchInputInfo is not actually implemented in ltcwallet. This is based on the
// btcwallet implementation. As this is used by btc.spvWallet, we really only
// need the TxOut, and to show ownership.
func (w *ltcSPVWallet) FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, *psbt.Bip32Derivation, int64, error) {

	td, err := w.txDetails((*ltcchainhash.Hash)(&prevOut.Hash))
	if err != nil {
		return nil, nil, nil, 0, err
	}

	if prevOut.Index >= uint32(len(td.TxRecord.MsgTx.TxOut)) {
		return nil, nil, nil, 0, fmt.Errorf("not enough outputs")
	}

	ltcTxOut := td.TxRecord.MsgTx.TxOut[prevOut.Index]

	// Verify we own at least one parsed address.
	_, addrs, _, err := ltctxscript.ExtractPkScriptAddrs(ltcTxOut.PkScript, w.chainParams)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	notOurs := true
	for i := 0; notOurs && i < len(addrs); i++ {
		_, err := w.Wallet.AddressInfo(addrs[i])
		notOurs = err != nil
	}
	if notOurs {
		return nil, nil, nil, 0, btcwallet.ErrNotMine
	}

	btcTxOut := &wire.TxOut{
		Value:    ltcTxOut.Value,
		PkScript: ltcTxOut.PkScript,
	}

	return nil, btcTxOut, nil, 0, nil
}

func (w *ltcSPVWallet) LockOutpoint(op wire.OutPoint) {
	w.Wallet.LockOutpoint(ltcwire.OutPoint{
		Hash:  ltcchainhash.Hash(op.Hash),
		Index: op.Index,
	})
}

func (w *ltcSPVWallet) UnlockOutpoint(op wire.OutPoint) {
	w.Wallet.UnlockOutpoint(ltcwire.OutPoint{
		Hash:  ltcchainhash.Hash(op.Hash),
		Index: op.Index,
	})
}

func (w *ltcSPVWallet) LockedOutpoints() []btcjson.TransactionInput {
	locks := w.Wallet.LockedOutpoints()
	locked := make([]btcjson.TransactionInput, len(locks))
	for i, lock := range locks {
		locked[i] = btcjson.TransactionInput{
			Txid: lock.Txid,
			Vout: lock.Vout,
		}
	}
	return locked
}

func (w *ltcSPVWallet) NewChangeAddress(account uint32, _ waddrmgr.KeyScope) (btcutil.Address, error) {
	ltcAddr, err := w.Wallet.NewChangeAddress(account, ltcwaddrmgr.KeyScopeBIP0084)
	if err != nil {
		return nil, err
	}
	return w.addrLTC2BTC(ltcAddr)
}

func (w *ltcSPVWallet) NewAddress(account uint32, _ waddrmgr.KeyScope) (btcutil.Address, error) {
	ltcAddr, err := w.Wallet.NewAddress(account, ltcwaddrmgr.KeyScopeBIP0084)
	if err != nil {
		return nil, err
	}
	return w.addrLTC2BTC(ltcAddr)
}

func (w *ltcSPVWallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error) {
	ltcAddr, err := w.addrBTC2LTC(a)
	if err != nil {
		return nil, err
	}

	ltcKey, err := w.Wallet.PrivKeyForAddress(ltcAddr)
	if err != nil {
		return nil, err
	}

	priv, _ /* pub */ := btcec.PrivKeyFromBytes(ltcKey.Serialize())
	return priv, nil
}

func (w *ltcSPVWallet) SendOutputs(outputs []*wire.TxOut, _ *waddrmgr.KeyScope, account uint32, minconf int32,
	satPerKb btcutil.Amount, css btcwallet.CoinSelectionStrategy, label string) (*wire.MsgTx, error) {

	ltcOuts := make([]*ltcwire.TxOut, len(outputs))
	for i, op := range outputs {
		ltcOuts[i] = &ltcwire.TxOut{
			Value:    op.Value,
			PkScript: op.PkScript,
		}
	}

	ltcTx, err := w.Wallet.SendOutputs(ltcOuts, &ltcwaddrmgr.KeyScopeBIP0084, account,
		minconf, ltcutil.Amount(satPerKb), wallet.CoinSelectionStrategy(css), label)
	if err != nil {
		return nil, err
	}

	btcTx, err := convertMsgTxToBTC(ltcTx)
	if err != nil {
		return nil, err
	}

	return btcTx, nil
}

func (w *ltcSPVWallet) HaveAddress(a btcutil.Address) (bool, error) {
	ltcAddr, err := w.addrBTC2LTC(a)
	if err != nil {
		return false, err
	}

	return w.Wallet.HaveAddress(ltcAddr)
}

func (w *ltcSPVWallet) Stop() {
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

	w.log.Info("SPV wallet closed")
}

func (w *ltcSPVWallet) AccountProperties(_ waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error) {
	props, err := w.Wallet.AccountProperties(ltcwaddrmgr.KeyScopeBIP0084, acct)
	if err != nil {
		return nil, err
	}
	return &waddrmgr.AccountProperties{
		AccountNumber:        props.AccountNumber,
		AccountName:          props.AccountName,
		ExternalKeyCount:     props.ExternalKeyCount,
		InternalKeyCount:     props.InternalKeyCount,
		ImportedKeyCount:     props.ImportedKeyCount,
		MasterKeyFingerprint: props.MasterKeyFingerprint,
		KeyScope:             waddrmgr.KeyScopeBIP0084,
		IsWatchOnly:          props.IsWatchOnly,
		// The last two would need conversion but aren't currently used.
		// AccountPubKey:        props.AccountPubKey,
		// AddrSchema:           props.AddrSchema,
	}, nil
}

func (w *ltcSPVWallet) RescanAsync() error {
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
func (w *ltcSPVWallet) ForceRescan() {
	w.log.Info("Dropping transaction history to perform full rescan...")
	err := w.dropTransactionHistory()
	if err != nil {
		w.log.Errorf("Failed to drop wallet transaction history: %v", err)
		// Continue to attempt restarting the wallet anyway.
	}

	err = walletdb.Update(w.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(waddrmgrNamespace) // it'll be fine
		return w.Manager.SetSyncedTo(ns, nil)         // never synced, forcing recover from birthday
	})
	if err != nil {
		w.log.Errorf("Failed to reset wallet manager sync height: %v", err)
	}
}

// dropTransactionHistory drops the transaction history. It is based off of the
// dropwtxmgr utility in the ltcwallet repo.
func (w *ltcSPVWallet) dropTransactionHistory() error {
	w.log.Info("Dropping wallet transaction history")

	return walletdb.Update(w.Database(), func(tx walletdb.ReadWriteTx) error {
		err := tx.DeleteTopLevelBucket(wtxmgrNamespace)
		if err != nil && err != walletdb.ErrBucketNotFound {
			return err
		}
		ns, err := tx.CreateTopLevelBucket(wtxmgrNamespace)
		if err != nil {
			return err
		}
		err = ltcwtxmgr.Create(ns)
		if err != nil {
			return err
		}

		ns = tx.ReadWriteBucket(waddrmgrNamespace)
		birthdayBlock, err := ltcwaddrmgr.FetchBirthdayBlock(ns)
		if err != nil {
			fmt.Println("Wallet does not have a birthday block " +
				"set, falling back to rescan from genesis")

			startBlock, err := ltcwaddrmgr.FetchStartBlock(ns)
			if err != nil {
				return err
			}
			return ltcwaddrmgr.PutSyncedTo(ns, startBlock)
		}

		// We'll need to remove our birthday block first because it
		// serves as a barrier when updating our state to detect reorgs
		// due to the wallet not storing all block hashes of the chain.
		if err := ltcwaddrmgr.DeleteBirthdayBlock(ns); err != nil {
			return err
		}

		if err := ltcwaddrmgr.PutSyncedTo(ns, &birthdayBlock); err != nil {
			return err
		}
		return ltcwaddrmgr.PutBirthdayBlock(ns, birthdayBlock)
	})
}

func (w *ltcSPVWallet) WalletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	txDetails, err := w.txDetails((*ltcchainhash.Hash)(txHash))
	if err != nil {
		return nil, err
	}

	btcTx, err := convertMsgTxToBTC(&txDetails.MsgTx)
	if err != nil {
		return nil, err
	}

	credits := make([]wtxmgr.CreditRecord, len(txDetails.Credits))
	for i, c := range txDetails.Credits {
		credits[i] = wtxmgr.CreditRecord{
			Amount: btcutil.Amount(c.Amount),
			Index:  c.Index,
			Spent:  c.Spent,
			Change: c.Change,
		}
	}

	debits := make([]wtxmgr.DebitRecord, len(txDetails.Debits))
	for i, d := range txDetails.Debits {
		debits[i] = wtxmgr.DebitRecord{
			Amount: btcutil.Amount(d.Amount),
			Index:  d.Index,
		}
	}

	return &wtxmgr.TxDetails{
		TxRecord: wtxmgr.TxRecord{
			MsgTx:        *btcTx,
			Hash:         chainhash.Hash(txDetails.TxRecord.Hash),
			Received:     txDetails.TxRecord.Received,
			SerializedTx: txDetails.TxRecord.SerializedTx,
		},
		Block: wtxmgr.BlockMeta{
			Block: wtxmgr.Block{
				Hash:   chainhash.Hash(txDetails.Block.Hash),
				Height: txDetails.Block.Height,
			},
			Time: txDetails.Block.Time,
		},
		Credits: credits,
		Debits:  debits,
	}, nil
}

func (w *ltcSPVWallet) SyncedTo() waddrmgr.BlockStamp {
	bs := w.Manager.SyncedTo()
	return waddrmgr.BlockStamp{
		Height:    bs.Height,
		Hash:      chainhash.Hash(bs.Hash),
		Timestamp: bs.Timestamp,
	}

}

func (w *ltcSPVWallet) SignTx(btcTx *wire.MsgTx) error {
	ltcTx, err := convertMsgTxToLTC(btcTx)
	if err != nil {
		return err
	}

	var prevPkScripts [][]byte
	var inputValues []ltcutil.Amount
	for _, txIn := range btcTx.TxIn {
		_, txOut, _, _, err := w.FetchInputInfo(&txIn.PreviousOutPoint)
		if err != nil {
			return err
		}
		inputValues = append(inputValues, ltcutil.Amount(txOut.Value))
		prevPkScripts = append(prevPkScripts, txOut.PkScript)
		// Zero the previous witness and signature script or else
		// AddAllInputScripts does some weird stuff.
		txIn.SignatureScript = nil
		txIn.Witness = nil
	}

	err = txauthor.AddAllInputScripts(ltcTx, prevPkScripts, inputValues, &secretSource{w.Wallet, w.chainParams})
	if err != nil {
		return err
	}
	if len(ltcTx.TxIn) != len(btcTx.TxIn) {
		return fmt.Errorf("txin count mismatch")
	}
	for i, txIn := range btcTx.TxIn {
		ltcIn := ltcTx.TxIn[i]
		txIn.SignatureScript = ltcIn.SignatureScript
		txIn.Witness = make(wire.TxWitness, len(ltcIn.Witness))
		copy(txIn.Witness, ltcIn.Witness)
	}
	return nil
}

// BlockNotifications returns a channel on which to receive notifications of
// newly processed blocks. The caller should only call BlockNotificaitons once.
func (w *ltcSPVWallet) BlockNotifications(ctx context.Context) <-chan *btc.BlockNotification {
	cl := w.NtfnServer.TransactionNotifications()
	ch := make(chan *btc.BlockNotification, 1)
	go func() {
		defer cl.Done()
		for {
			select {
			case note := <-cl.C:
				if len(note.AttachedBlocks) > 0 {
					lastBlock := note.AttachedBlocks[len(note.AttachedBlocks)-1]
					select {
					case ch <- &btc.BlockNotification{
						Hash:   chainhash.Hash(*lastBlock.Hash),
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

func (w *ltcSPVWallet) Peers() ([]*asset.WalletPeer, error) {
	return w.peerManager.Peers()
}

func (w *ltcSPVWallet) AddPeer(addr string) error {
	return w.peerManager.AddPeer(addr)
}

func (w *ltcSPVWallet) RemovePeer(addr string) error {
	return w.peerManager.RemovePeer(addr)
}

// secretSource is used to locate keys and redemption scripts while signing a
// transaction. secretSource satisfies the txauthor.SecretsSource interface.
type secretSource struct {
	w           *wallet.Wallet
	chainParams *ltcchaincfg.Params
}

// ChainParams returns the chain parameters.
func (s *secretSource) ChainParams() *ltcchaincfg.Params {
	return s.chainParams
}

// GetKey fetches a private key for the specified address.
func (s *secretSource) GetKey(addr ltcutil.Address) (*btcec.PrivateKey, bool, error) {
	ma, err := s.w.AddressInfo(addr)
	if err != nil {
		return nil, false, err
	}

	mpka, ok := ma.(ltcwaddrmgr.ManagedPubKeyAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedPubKeyAddress", addr, ma)
		return nil, false, e
	}

	privKey, err := mpka.PrivKey()
	if err != nil {
		return nil, false, err
	}

	k, _ /* pub */ := btcec.PrivKeyFromBytes(privKey.Serialize())

	return k, ma.Compressed(), nil
}

// GetScript fetches the redemption script for the specified p2sh/p2wsh address.
func (s *secretSource) GetScript(addr ltcutil.Address) ([]byte, error) {
	ma, err := s.w.AddressInfo(addr)
	if err != nil {
		return nil, err
	}

	msa, ok := ma.(ltcwaddrmgr.ManagedScriptAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedScriptAddress", addr, ma)
		return nil, e
	}
	return msa.Script()
}

// spvService embeds ltcsuite neutrino.ChainService and translates types.
type spvService struct {
	*neutrino.ChainService
}

var _ btc.SPVService = (*spvService)(nil)

func (s *spvService) GetBlockHash(height int64) (*chainhash.Hash, error) {
	ltcHash, err := s.ChainService.GetBlockHash(height)
	if err != nil {
		return nil, err
	}
	return (*chainhash.Hash)(ltcHash), nil
}

func (s *spvService) BestBlock() (*headerfs.BlockStamp, error) {
	bs, err := s.ChainService.BestBlock()
	if err != nil {
		return nil, err
	}
	return &headerfs.BlockStamp{
		Height:    bs.Height,
		Hash:      chainhash.Hash(bs.Hash),
		Timestamp: bs.Timestamp,
	}, nil
}

func (s *spvService) Peers() []btc.SPVPeer {
	rawPeers := s.ChainService.Peers()
	peers := make([]btc.SPVPeer, len(rawPeers))
	for i, p := range rawPeers {
		peers[i] = p
	}
	return peers
}

func (s *spvService) AddPeer(addr string) error {
	return s.ChainService.ConnectNode(addr, true)
}

func (s *spvService) RemovePeer(addr string) error {
	return s.ChainService.RemoveNodeByAddr(addr)
}

func (s *spvService) GetBlockHeight(h *chainhash.Hash) (int32, error) {
	return s.ChainService.GetBlockHeight((*ltcchainhash.Hash)(h))
}

func (s *spvService) GetBlockHeader(h *chainhash.Hash) (*wire.BlockHeader, error) {
	hdr, err := s.ChainService.GetBlockHeader((*ltcchainhash.Hash)(h))
	if err != nil {
		return nil, err
	}
	return &wire.BlockHeader{
		Version:    hdr.Version,
		PrevBlock:  chainhash.Hash(hdr.PrevBlock),
		MerkleRoot: chainhash.Hash(hdr.MerkleRoot),
		Timestamp:  hdr.Timestamp,
		Bits:       hdr.Bits,
		Nonce:      hdr.Nonce,
	}, nil
}

func (s *spvService) GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, _ ...btcneutrino.QueryOption) (*gcs.Filter, error) {
	f, err := s.ChainService.GetCFilter(ltcchainhash.Hash(blockHash), ltcwire.GCSFilterRegular)
	if err != nil {
		return nil, err
	}

	b, err := f.Bytes()
	if err != nil {
		return nil, err
	}

	return gcs.FromBytes(f.N(), f.P(), DefaultM, b)
}

func (s *spvService) GetBlock(blockHash chainhash.Hash, _ ...btcneutrino.QueryOption) (*btcutil.Block, error) {
	blk, err := s.ChainService.GetBlock(ltcchainhash.Hash(blockHash))
	if err != nil {
		return nil, err
	}

	b, err := blk.Bytes()
	if err != nil {
		return nil, err
	}

	return btcutil.NewBlockFromBytes(b)
}

func convertMsgTxToBTC(tx *ltcwire.MsgTx) (*wire.MsgTx, error) {
	buf := new(bytes.Buffer)
	if err := tx.Serialize(buf); err != nil {
		return nil, err
	}

	btcTx := new(wire.MsgTx)
	if err := btcTx.Deserialize(buf); err != nil {
		return nil, err
	}
	return btcTx, nil
}

func convertMsgTxToLTC(tx *wire.MsgTx) (*ltcwire.MsgTx, error) {
	buf := new(bytes.Buffer)
	if err := tx.Serialize(buf); err != nil {
		return nil, err
	}
	ltcTx := new(ltcwire.MsgTx)
	if err := ltcTx.Deserialize(buf); err != nil {
		return nil, err
	}

	return ltcTx, nil
}

func extendAddresses(extIdx, intIdx uint32, ltcw *wallet.Wallet) error {
	scopedKeyManager, err := ltcw.Manager.FetchScopedKeyManager(ltcwaddrmgr.KeyScopeBIP0084)
	if err != nil {
		return err
	}

	return walletdb.Update(ltcw.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(waddrmgrNamespace)
		if extIdx > 0 {
			scopedKeyManager.ExtendExternalAddresses(ns, defaultAcctNum, extIdx)
		}
		if intIdx > 0 {
			scopedKeyManager.ExtendInternalAddresses(ns, defaultAcctNum, intIdx)
		}
		return nil
	})
}

var (
	loggingInited uint32
	logFileName   = "neutrino.log"
)

// logRotator initializes a rotating file logger.
func logRotator(walletDir string) (*rotator.Rotator, error) {
	const maxLogRolls = 8
	logDir := filepath.Join(walletDir, logDirName)
	if err := os.MkdirAll(logDir, 0744); err != nil {
		return nil, fmt.Errorf("error creating log directory: %w", err)
	}

	logFilename := filepath.Join(logDir, logFileName)
	return rotator.New(logFilename, 32*1024, false, maxLogRolls)
}

// logNeutrino initializes logging in the neutrino + wallet packages. Logging
// only has to be initialized once, so an atomic flag is used internally to
// return early on subsequent invocations.
//
// In theory, the rotating file logger must be Closed at some point, but
// there are concurrency issues with that since btcd and btcwallet have
// unsupervised goroutines still running after shutdown. So we leave the rotator
// running at the risk of losing some logs.
func logNeutrino(walletDir string, errorLogger dex.Logger) error {
	if !atomic.CompareAndSwapUint32(&loggingInited, 0, 1) {
		return nil
	}

	logSpinner, err := logRotator(walletDir)
	if err != nil {
		return fmt.Errorf("error initializing log rotator: %w", err)
	}

	backendLog := btclog.NewBackend(logSpinner)

	logger := func(name string, lvl btclog.Level) btclog.Logger {
		l := backendLog.Logger(name)
		l.SetLevel(lvl)
		return &fileLoggerPlus{Logger: l, log: errorLogger.SubLogger(name)}
	}

	neutrino.UseLogger(logger("NTRNO", btclog.LevelDebug))
	wallet.UseLogger(logger("LTCW", btclog.LevelInfo))
	ltcwtxmgr.UseLogger(logger("TXMGR", btclog.LevelInfo))
	chain.UseLogger(logger("CHAIN", btclog.LevelInfo))

	return nil
}

// fileLoggerPlus logs everything to a file, and everything with level >= warn
// to both file and a specified dex.Logger.
type fileLoggerPlus struct {
	btclog.Logger
	log dex.Logger
}

func (f *fileLoggerPlus) Warnf(format string, params ...any) {
	f.log.Warnf(format, params...)
	f.Logger.Warnf(format, params...)
}

func (f *fileLoggerPlus) Errorf(format string, params ...any) {
	f.log.Errorf(format, params...)
	f.Logger.Errorf(format, params...)
}

func (f *fileLoggerPlus) Criticalf(format string, params ...any) {
	f.log.Criticalf(format, params...)
	f.Logger.Criticalf(format, params...)
}

func (f *fileLoggerPlus) Warn(v ...any) {
	f.log.Warn(v...)
	f.Logger.Warn(v...)
}

func (f *fileLoggerPlus) Error(v ...any) {
	f.log.Error(v...)
	f.Logger.Error(v...)
}

func (f *fileLoggerPlus) Critical(v ...any) {
	f.log.Critical(v...)
	f.Logger.Critical(v...)
}

type logAdapter struct {
	dex.Logger
}

var _ btclog.Logger = (*logAdapter)(nil)

func (a *logAdapter) Level() btclog.Level {
	return btclog.Level(a.Logger.Level())
}

func (a *logAdapter) SetLevel(lvl btclog.Level) {
	a.Logger.SetLevel(slog.Level(lvl))
}
