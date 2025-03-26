// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbch "decred.org/dcrdex/dex/networks/bch"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/gcs"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcwallet/waddrmgr"
	btcwallet "github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/dcrlabs/bchwallet/chain"
	bchwaddrmgr "github.com/dcrlabs/bchwallet/waddrmgr"
	"github.com/dcrlabs/bchwallet/wallet"
	"github.com/dcrlabs/bchwallet/wallet/txauthor"
	"github.com/dcrlabs/bchwallet/walletdb"
	_ "github.com/dcrlabs/bchwallet/walletdb/bdb"
	bchwtxmgr "github.com/dcrlabs/bchwallet/wtxmgr"
	neutrino "github.com/dcrlabs/neutrino-bch"
	labschain "github.com/dcrlabs/neutrino-bch/chain"
	"github.com/decred/slog"
	"github.com/gcash/bchd/bchec"
	bchchaincfg "github.com/gcash/bchd/chaincfg"
	bchchainhash "github.com/gcash/bchd/chaincfg/chainhash"
	bchtxscript "github.com/gcash/bchd/txscript"
	bchwire "github.com/gcash/bchd/wire"
	"github.com/gcash/bchlog"
	"github.com/gcash/bchutil"
	"github.com/jrick/logrotate/rotator"
	btcneutrino "github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	DefaultM       uint64 = 784931 // From bchutil. Used for gcs filters.
	logDirName            = "logs"
	neutrinoDBName        = "neutrino.db"
	defaultAcctNum        = 0
)

var (
	waddrmgrNamespace = []byte("waddrmgr")
	wtxmgrNamespace   = []byte("wtxmgr")
)

// bchSPVWallet is an implementation of btc.BTCWallet that runs a native Bitcoin
// Cash SPV Wallet. bchSPVWallet mostly just translates types from the btcsuite
// types to gcash and vice-versa. Startup and shutdown are notable exceptions,
// and have some critical code that needed to be duplicated (in order to avoid
// interface hell).
type bchSPVWallet struct {
	// This section is populated in openSPVWallet.
	dir         string
	chainParams *bchchaincfg.Params
	btcParams   *chaincfg.Params
	log         dex.Logger

	// This section is populated in Start.
	*wallet.Wallet
	chainClient *labschain.NeutrinoClient
	cl          *neutrino.ChainService
	loader      *wallet.Loader
	neutrinoDB  walletdb.DB

	peerManager *btc.SPVPeerManager
}

var _ btc.BTCWallet = (*bchSPVWallet)(nil)

// openSPVWallet creates a bchSPVWallet, but does not Start.
// Satisfies btc.BTCWalletConstructor.
func openSPVWallet(dir string, cfg *btc.WalletConfig, btcParams *chaincfg.Params, log dex.Logger) btc.BTCWallet {
	var bchParams *bchchaincfg.Params
	switch btcParams.Name {
	case dexbch.MainNetParams.Name:
		bchParams = &bchchaincfg.MainNetParams
	case dexbch.TestNet4Params.Name:
		bchParams = &bchchaincfg.TestNet4Params
	case dexbch.RegressionNetParams.Name:
		bchParams = &bchchaincfg.RegressionNetParams
	}

	w := &bchSPVWallet{
		dir:         dir,
		chainParams: bchParams,
		btcParams:   btcParams,
		log:         log,
	}
	return w
}

// createSPVWallet creates a new SPV wallet.
func createSPVWallet(privPass []byte, seed []byte, bday time.Time, walletDir string, log dex.Logger, extIdx, intIdx uint32, net *bchchaincfg.Params) error {
	if err := logNeutrino(walletDir, log); err != nil {
		return fmt.Errorf("error initializing bchwallet+neutrino logging: %w", err)
	}

	loader := wallet.NewLoader(net, walletDir, true, 250)

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
	db, err := walletdb.Create("bdb", neutrinoDBPath, true)
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

// Start initializes the *bchwallet.Wallet and its supporting players and starts
// syncing.
func (w *bchSPVWallet) Start() (btc.SPVService, error) {
	if err := logNeutrino(w.dir, w.log); err != nil {
		return nil, fmt.Errorf("error initializing bchwallet+neutrino logging: %v", err)
	}
	// recoverWindow arguments borrowed from bchwallet directly.
	w.loader = wallet.NewLoader(w.chainParams, w.dir, true, 250)

	exists, err := w.loader.WalletExists()
	if err != nil {
		return nil, fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return nil, errors.New("wallet not found")
	}

	w.log.Debug("Starting native BCH wallet...")
	w.Wallet, err = w.loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
	if err != nil {
		return nil, fmt.Errorf("couldn't load wallet: %w", err)
	}

	errCloser := dex.NewErrorCloser()
	defer errCloser.Done(w.log)
	errCloser.Add(w.loader.UnloadWallet)

	neutrinoDBPath := filepath.Join(w.dir, neutrinoDBName)
	w.neutrinoDB, err = walletdb.Create("bdb", neutrinoDBPath, true)
	if err != nil {
		return nil, fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}
	errCloser.Add(w.neutrinoDB.Close)

	w.log.Debug("Starting neutrino chain service...")
	w.cl, err = neutrino.NewChainService(neutrino.Config{
		DataDir:     w.dir,
		Database:    w.neutrinoDB,
		ChainParams: *w.chainParams,
		// https://github.com/gcash/neutrino/pull/36
		PersistToDisk: true, // keep cfilter headers on disk for efficient rescanning
		// WARNING: PublishTransaction currently uses the entire duration
		// because if an external bug, but even if the bug is resolved, a
		// typical inv/getdata round trip is ~4 seconds, so we set this so
		// neutrino does not cancel queries too readily.
		BroadcastTimeout: 6 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create Neutrino ChainService: %v", err)
	}
	errCloser.Add(w.cl.Stop)

	w.chainClient = labschain.NewNeutrinoClient(w.chainParams, w.cl, &logAdapter{w.log})

	var defaultPeers []string
	switch w.chainParams.Net {
	// case bchwire.MainNet:
	// 	addPeers = []string{"cfilters.ssgen.io"}
	case bchwire.TestNet4:
		// Add the address for a local bchd testnet4 node.
		defaultPeers = []string{}
	case bchwire.TestNet, bchwire.SimNet: // plain "wire.TestNet" is regnet!
		defaultPeers = []string{"127.0.0.1:21577"}
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

func (w *bchSPVWallet) txDetails(txHash *bchchainhash.Hash) (*bchwtxmgr.TxDetails, error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, btc.WalletTransactionNotFound
	}

	return details, nil
}

var _ btc.BTCWallet = (*bchSPVWallet)(nil)

func (w *bchSPVWallet) PublishTransaction(btcTx *wire.MsgTx, label string) error {
	bchTx, err := convertMsgTxToBCH(btcTx)
	if err != nil {
		return err
	}
	return w.Wallet.PublishTransaction(bchTx)
}

func (w *bchSPVWallet) CalculateAccountBalances(account uint32, confirms int32) (btcwallet.Balances, error) {
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

func (w *bchSPVWallet) ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error) {
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
			BlockIndex:        r.BlockIndex,
			BlockTime:         r.BlockTime,
			Category:          r.Category,
			Confirmations:     r.Confirmations,
			Fee:               r.Fee,
			Generated:         r.Generated,
			InvolvesWatchOnly: r.InvolvesWatchOnly,
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

func (w *bchSPVWallet) GetTransactions(startBlock, endBlock int32, _ string, cancel <-chan struct{}) (*btcwallet.GetTransactionsResult, error) {
	startID := wallet.NewBlockIdentifierFromHeight(startBlock)
	endID := wallet.NewBlockIdentifierFromHeight(endBlock)
	bchGTR, err := w.Wallet.GetTransactions(startID, endID, cancel)
	if err != nil {
		return nil, err
	}

	convertTxs := func(txs []wallet.TransactionSummary) []btcwallet.TransactionSummary {
		transactions := make([]btcwallet.TransactionSummary, len(txs))
		for i, tx := range txs {
			txHash := chainhash.Hash(*tx.Hash)
			inputs := make([]btcwallet.TransactionSummaryInput, len(tx.MyInputs))
			for k, in := range tx.MyInputs {
				inputs[k] = btcwallet.TransactionSummaryInput{
					Index:           in.Index,
					PreviousAccount: in.PreviousAccount,
					PreviousAmount:  btcutil.Amount(in.PreviousAmount),
				}
			}
			outputs := make([]btcwallet.TransactionSummaryOutput, len(tx.MyOutputs))
			for k, out := range tx.MyOutputs {
				outputs[k] = btcwallet.TransactionSummaryOutput{
					Index:    out.Index,
					Account:  out.Account,
					Internal: out.Internal,
				}
			}
			transactions[i] = btcwallet.TransactionSummary{
				Hash:        &txHash,
				Transaction: tx.Transaction,
				MyInputs:    inputs,
				MyOutputs:   outputs,
				Fee:         btcutil.Amount(tx.Fee),
				Timestamp:   tx.Timestamp,
			}
		}
		return transactions
	}

	btcGTR := &btcwallet.GetTransactionsResult{
		MinedTransactions:   make([]btcwallet.Block, len(bchGTR.MinedTransactions)),
		UnminedTransactions: convertTxs(bchGTR.UnminedTransactions),
	}

	for i, block := range bchGTR.MinedTransactions {
		blockHash := chainhash.Hash(*block.Hash)
		btcGTR.MinedTransactions[i] = btcwallet.Block{
			Hash:         &blockHash,
			Height:       block.Height,
			Timestamp:    block.Timestamp,
			Transactions: convertTxs(block.Transactions),
		}
	}

	return btcGTR, nil
}

func (w *bchSPVWallet) ListUnspent(minconf, maxconf int32, acctName string) ([]*btcjson.ListUnspentResult, error) {
	// bchwallet's ListUnspent takes either a list of addresses, or else returns
	// all non-locked unspent outputs for all accounts. We need to iterate the
	// results anyway to convert type.
	uns, err := w.Wallet.ListUnspent(minconf, maxconf, nil)
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

// FetchInputInfo is not actually implemented in bchwallet. This is based on the
// btcwallet implementation. As this is used by btc.spvWallet, we really only
// need the TxOut, and to show ownership.
func (w *bchSPVWallet) FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, *psbt.Bip32Derivation, int64, error) {

	td, err := w.txDetails((*bchchainhash.Hash)(&prevOut.Hash))
	if err != nil {
		return nil, nil, nil, 0, err
	}

	if prevOut.Index >= uint32(len(td.TxRecord.MsgTx.TxOut)) {
		return nil, nil, nil, 0, fmt.Errorf("not enough outputs")
	}

	bchTxOut := td.TxRecord.MsgTx.TxOut[prevOut.Index]

	// Verify we own at least one parsed address.
	_, addrs, _, err := bchtxscript.ExtractPkScriptAddrs(bchTxOut.PkScript, w.chainParams)
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
		Value:    bchTxOut.Value,
		PkScript: bchTxOut.PkScript,
	}

	return nil, btcTxOut, nil, 0, nil
}

func (w *bchSPVWallet) LockOutpoint(op wire.OutPoint) {
	w.Wallet.LockOutpoint(bchwire.OutPoint{
		Hash:  bchchainhash.Hash(op.Hash),
		Index: op.Index,
	})
}

func (w *bchSPVWallet) UnlockOutpoint(op wire.OutPoint) {
	w.Wallet.UnlockOutpoint(bchwire.OutPoint{
		Hash:  bchchainhash.Hash(op.Hash),
		Index: op.Index,
	})
}

func (w *bchSPVWallet) LockedOutpoints() []btcjson.TransactionInput {
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

func (w *bchSPVWallet) NewChangeAddress(account uint32, _ waddrmgr.KeyScope) (btcutil.Address, error) {
	bchAddr, err := w.Wallet.NewChangeAddress(account, bchwaddrmgr.KeyScopeBIP0044)
	if err != nil {
		return nil, err
	}
	return dexbch.DecodeCashAddress(bchAddr.String(), w.btcParams)
}

func (w *bchSPVWallet) NewAddress(account uint32, _ waddrmgr.KeyScope) (btcutil.Address, error) {
	bchAddr, err := w.Wallet.NewAddress(account, bchwaddrmgr.KeyScopeBIP0044)
	if err != nil {
		return nil, err
	}
	return dexbch.DecodeCashAddress(bchAddr.String(), w.btcParams)
}

func (w *bchSPVWallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error) {
	bchAddr, err := dexbch.BTCAddrToBCHAddr(a, w.btcParams)
	if err != nil {
		return nil, err
	}
	bchKey, err := w.Wallet.PrivKeyForAddress(bchAddr)
	if err != nil {
		return nil, err
	}

	priv, _ /* pub */ := btcec.PrivKeyFromBytes(bchKey.Serialize())
	return priv, nil
}

func (w *bchSPVWallet) SendOutputs(outputs []*wire.TxOut, _ *waddrmgr.KeyScope, account uint32, minconf int32,
	satPerKb btcutil.Amount, _ btcwallet.CoinSelectionStrategy, label string) (*wire.MsgTx, error) {

	bchOuts := make([]*bchwire.TxOut, len(outputs))
	for i, op := range outputs {
		bchOuts[i] = &bchwire.TxOut{
			Value:    op.Value,
			PkScript: op.PkScript,
		}
	}

	bchTx, err := w.Wallet.SendOutputs(bchOuts, account, minconf, bchutil.Amount(satPerKb))
	if err != nil {
		return nil, err
	}

	btcTx, err := convertMsgTxToBTC(bchTx)
	if err != nil {
		return nil, err
	}

	return btcTx, nil
}

func (w *bchSPVWallet) HaveAddress(a btcutil.Address) (bool, error) {
	bchAddr, err := dexbch.BTCAddrToBCHAddr(a, w.btcParams)
	if err != nil {
		return false, err
	}

	return w.Wallet.HaveAddress(bchAddr)
}

func (w *bchSPVWallet) Stop() {
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

func (w *bchSPVWallet) AccountProperties(_ waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error) {
	props, err := w.Wallet.AccountProperties(bchwaddrmgr.KeyScopeBIP0044, acct)
	if err != nil {
		return nil, err
	}
	return &waddrmgr.AccountProperties{
		// bch only has a subset of the btc AccountProperties. We'll give
		// everything we have. The only two that are actually used are these
		// first to fields.
		ExternalKeyCount: props.ExternalKeyCount,
		InternalKeyCount: props.InternalKeyCount,
		AccountNumber:    props.AccountNumber,
		AccountName:      props.AccountName,
		ImportedKeyCount: props.ImportedKeyCount,
	}, nil
}

func (w *bchSPVWallet) RescanAsync() error {
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
func (w *bchSPVWallet) ForceRescan() {
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
// dropwtxmgr utility in the bchwallet repo.
func (w *bchSPVWallet) dropTransactionHistory() error {
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
		err = bchwtxmgr.Create(ns)
		if err != nil {
			return err
		}

		ns = tx.ReadWriteBucket(waddrmgrNamespace)
		birthdayBlock, err := bchwaddrmgr.FetchBirthdayBlock(ns)
		if err != nil {
			fmt.Println("Wallet does not have a birthday block " +
				"set, falling back to rescan from genesis")

			startBlock, err := bchwaddrmgr.FetchStartBlock(ns)
			if err != nil {
				return err
			}
			return bchwaddrmgr.PutSyncedTo(ns, startBlock)
		}

		// We'll need to remove our birthday block first because it
		// serves as a barrier when updating our state to detect reorgs
		// due to the wallet not storing all block hashes of the chain.
		if err := bchwaddrmgr.DeleteBirthdayBlock(ns); err != nil {
			return err
		}

		if err := bchwaddrmgr.PutSyncedTo(ns, &birthdayBlock); err != nil {
			return err
		}
		return bchwaddrmgr.PutBirthdayBlock(ns, birthdayBlock)
	})
}

func (w *bchSPVWallet) WalletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	txDetails, err := w.txDetails((*bchchainhash.Hash)(txHash))
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

func (w *bchSPVWallet) SyncedTo() waddrmgr.BlockStamp {
	bs := w.Manager.SyncedTo()
	return waddrmgr.BlockStamp{
		Height:    bs.Height,
		Hash:      chainhash.Hash(bs.Hash),
		Timestamp: bs.Timestamp,
	}

}

func (w *bchSPVWallet) SignTx(btcTx *wire.MsgTx) error {
	bchTx, err := convertMsgTxToBCH(btcTx)
	if err != nil {
		return err
	}

	var prevPkScripts [][]byte
	var inputValues []bchutil.Amount
	for _, txIn := range btcTx.TxIn {
		_, txOut, _, _, err := w.FetchInputInfo(&txIn.PreviousOutPoint)
		if err != nil {
			return err
		}
		inputValues = append(inputValues, bchutil.Amount(txOut.Value))
		prevPkScripts = append(prevPkScripts, txOut.PkScript)
		// Zero the previous witness and signature script or else
		// AddAllInputScripts does some weird stuff.
		txIn.SignatureScript = nil
		txIn.Witness = nil
	}

	err = txauthor.AddAllInputScripts(bchTx, prevPkScripts, inputValues, &secretSource{w.Wallet, w.chainParams})
	if err != nil {
		return err
	}

	if len(bchTx.TxIn) != len(btcTx.TxIn) {
		return fmt.Errorf("txin count mismatch")
	}
	for i, txIn := range btcTx.TxIn {
		txIn.SignatureScript = bchTx.TxIn[i].SignatureScript
	}
	return nil
}

// BlockNotifications returns a channel on which to receive notifications of
// newly processed blocks. The caller should only call BlockNotificaitons once.
func (w *bchSPVWallet) BlockNotifications(ctx context.Context) <-chan *btc.BlockNotification {
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

func (w *bchSPVWallet) Birthday() time.Time {
	return w.Manager.Birthday()
}

func (w *bchSPVWallet) updateDBBirthday(bday time.Time) error {
	btcw, isLoaded := w.loader.LoadedWallet()
	if !isLoaded {
		return fmt.Errorf("wallet not loaded")
	}
	return walletdb.Update(btcw.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(waddrmgrNamespace)
		return btcw.Manager.SetBirthday(ns, bday)
	})
}

func (w *bchSPVWallet) Peers() ([]*asset.WalletPeer, error) {
	return w.peerManager.Peers()
}

func (w *bchSPVWallet) AddPeer(addr string) error {
	return w.peerManager.AddPeer(addr)
}

func (w *bchSPVWallet) RemovePeer(addr string) error {
	return w.peerManager.RemovePeer(addr)
}

func (w *bchSPVWallet) TotalReceivedForAddr(btcAddr btcutil.Address, minConf int32) (btcutil.Amount, error) {
	bchAddr, err := dexbch.BTCAddrToBCHAddr(btcAddr, w.btcParams)
	if err != nil {
		return 0, err
	}
	amt, err := w.Wallet.TotalReceivedForAddr(bchAddr, 0)
	if err != nil {
		return 0, err
	}
	return btcutil.Amount(amt), nil
}

// secretSource is used to locate keys and redemption scripts while signing a
// transaction. secretSource satisfies the txauthor.SecretsSource interface.
type secretSource struct {
	w           *wallet.Wallet
	chainParams *bchchaincfg.Params
}

// ChainParams returns the chain parameters.
func (s *secretSource) ChainParams() *bchchaincfg.Params {
	return s.chainParams
}

// GetKey fetches a private key for the specified address.
func (s *secretSource) GetKey(addr bchutil.Address) (*bchec.PrivateKey, bool, error) {
	ma, err := s.w.AddressInfo(addr)
	if err != nil {
		return nil, false, err
	}

	mpka, ok := ma.(bchwaddrmgr.ManagedPubKeyAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedPubKeyAddress", addr, ma)
		return nil, false, e
	}

	privKey, err := mpka.PrivKey()
	if err != nil {
		return nil, false, err
	}

	k, _ /* pub */ := bchec.PrivKeyFromBytes(bchec.S256(), privKey.Serialize())

	return k, ma.Compressed(), nil
}

// GetScript fetches the redemption script for the specified p2sh/p2wsh address.
func (s *secretSource) GetScript(addr bchutil.Address) ([]byte, error) {
	ma, err := s.w.AddressInfo(addr)
	if err != nil {
		return nil, err
	}

	msa, ok := ma.(bchwaddrmgr.ManagedScriptAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedScriptAddress", addr, ma)
		return nil, e
	}
	return msa.Script()
}

// spvService embeds gcash neutrino.ChainService and translates types.
type spvService struct {
	*neutrino.ChainService
}

var _ btc.SPVService = (*spvService)(nil)

func (s *spvService) GetBlockHash(height int64) (*chainhash.Hash, error) {
	bchHash, err := s.ChainService.GetBlockHash(height)
	if err != nil {
		return nil, err
	}
	return (*chainhash.Hash)(bchHash), nil
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
	return s.ChainService.GetBlockHeight((*bchchainhash.Hash)(h))
}

func (s *spvService) GetBlockHeader(h *chainhash.Hash) (*wire.BlockHeader, error) {
	hdr, err := s.ChainService.GetBlockHeader((*bchchainhash.Hash)(h))
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
	f, err := s.ChainService.GetCFilter(bchchainhash.Hash(blockHash), bchwire.GCSFilterRegular)
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
	blk, err := s.ChainService.GetBlock(bchchainhash.Hash(blockHash))
	if err != nil {
		return nil, err
	}

	b, err := blk.Bytes()
	if err != nil {
		return nil, err
	}

	return btcutil.NewBlockFromBytes(b)
}

func convertMsgTxToBTC(tx *bchwire.MsgTx) (*wire.MsgTx, error) {
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

func convertMsgTxToBCH(tx *wire.MsgTx) (*bchwire.MsgTx, error) {
	buf := new(bytes.Buffer)
	if err := tx.Serialize(buf); err != nil {
		return nil, err
	}
	bchTx := new(bchwire.MsgTx)
	if err := bchTx.Deserialize(buf); err != nil {
		return nil, err
	}
	return bchTx, nil
}

func extendAddresses(extIdx, intIdx uint32, bchw *wallet.Wallet) error {
	scopedKeyManager, err := bchw.Manager.FetchScopedKeyManager(bchwaddrmgr.KeyScopeBIP0044)
	if err != nil {
		return err
	}

	return walletdb.Update(bchw.Database(), func(dbtx walletdb.ReadWriteTx) error {
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

	backendLog := bchlog.NewBackend(logSpinner)

	logger := func(name string, lvl bchlog.Level) bchlog.Logger {
		l := backendLog.Logger(name)
		l.SetLevel(lvl)
		return &fileLoggerPlus{Logger: l, log: errorLogger.SubLogger(name)}
	}

	neutrino.UseLogger(logger("NTRNO", bchlog.LevelDebug))
	wallet.UseLogger(logger("BTCW", bchlog.LevelInfo))
	bchwtxmgr.UseLogger(logger("TXMGR", bchlog.LevelInfo))
	chain.UseLogger(logger("CHAIN", bchlog.LevelInfo))

	return nil
}

// fileLoggerPlus logs everything to a file, and everything with level >= warn
// to both file and a specified dex.Logger.
type fileLoggerPlus struct {
	bchlog.Logger
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

var _ bchlog.Logger = (*logAdapter)(nil)

func (a *logAdapter) Level() bchlog.Level {
	return bchlog.Level(a.Logger.Level())
}

func (a *logAdapter) SetLevel(lvl bchlog.Level) {
	a.Logger.SetLevel(slog.Level(lvl))
}
