// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// spvWallet implements a Wallet backed by a built-in btcwallet + Neutrino.
//
// There are a few challenges presented in using an SPV wallet for DEX.
// 1. Finding non-wallet related blockchain data requires posession of the
//    pubkey script, not just transction hash and output index
// 2. Finding non-wallet related blockchain data can often entail extensive
//    scanning of compact filters. We can limit these scans with more
//    information, such as the match time, which would be the earliest a
//    transaction could be found on-chain.
// 3. We don't see a mempool. We're blind to new transactions until they are
//    mined. This requires special handling by the caller. We've been
//    anticipating this, so Core and Swapper are permissive of missing acks for
//    audit requests.

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/gcs"
	"github.com/btcsuite/btcutil/psbt"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wallet/txauthor"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	WalletTransactionNotFound = dex.ErrorKind("wallet transaction not found")
	SpentStatusUnknown        = dex.ErrorKind("spend status not known")

	// see btcd/blockchain/validate.go
	maxBlockTimeOffset = 2 * time.Hour
	neutrinoDBName     = "neutrino.db"
)

var (
	SimnetSeed    = []byte("simnet-seed-of-considerable-length")
	SimnetAddress = "bcrt1qq8t6vmptznycut4f3vxtguxgknmcnmpqlvd8wf"
)

// btcWallet is satisfied by *btcwallet.Wallet.
type btcWallet interface {
	PublishTransaction(tx *wire.MsgTx, label string) error
	CalculateAccountBalances(account uint32, confirms int32) (wallet.Balances, error)
	ListUnspent(minconf, maxconf int32, acctName string) ([]*btcjson.ListUnspentResult, error)
	FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, *psbt.Bip32Derivation, int64, error)
	ResetLockedOutpoints()
	LockOutpoint(op wire.OutPoint)
	UnlockOutpoint(op wire.OutPoint)
	LockedOutpoints() []btcjson.TransactionInput
	NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
	NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
	SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
		additionalKeysByAddress map[string]*btcutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error)
	PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error)
	Database() walletdb.DB
	Unlock(passphrase []byte, lock <-chan time.Time) error
	Lock()
	Locked() bool
	SendOutputs(outputs []*wire.TxOut, keyScope *waddrmgr.KeyScope, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error)
	HaveAddress(a btcutil.Address) (bool, error)
	Stop()
	WaitForShutdown()
	ChainSynced() bool
	SynchronizeRPC(chainClient chain.Interface)
	walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error)
	syncedTo() waddrmgr.BlockStamp
	signTransaction(*wire.MsgTx) error
}

// neutrinoService is satisfied by *neutrino.ChainService.
type neutrinoService interface {
	GetBlockHash(int64) (*chainhash.Hash, error)
	BestBlock() (*headerfs.BlockStamp, error)
	Peers() []*neutrino.ServerPeer
	GetBlockHeight(hash *chainhash.Hash) (int32, error)
	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
	GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error)
	GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error)
	Stop() error
}

// hashMap manages a mapping of chainhash.Hash -> chainhash.Hash. hashMap is
// satisfied by hashmap.Map, or a stub for testing purposes.
type hashMap interface {
	Get(chainhash.Hash) *chainhash.Hash
	Set(k, v chainhash.Hash)
	Run(context.Context)
	Close() error
}

// spvConfig is configuration for the built-in SPV wallet.
type spvConfig struct {
	dbDir        string
	chainParams  *chaincfg.Params
	connectPeers []string
}

// loadChainClient initializes the *btcwallet.Wallet and its supporting players.
// The returned wallet is not syncing.
func loadChainClient(cfg *spvConfig) (*wallet.Wallet, *wallet.Loader, *neutrino.ChainService, walletdb.DB, error) {
	netDir := filepath.Join(cfg.dbDir, cfg.chainParams.Name)

	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	loader := wallet.NewLoader(cfg.chainParams, netDir, true, 60*time.Second, 250)

	exists, err := loader.WalletExists()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return nil, nil, nil, nil, fmt.Errorf("wallet not found")
	}

	w, err := loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("couldn't load wallet")
	}

	neutrinoDBPath := filepath.Join(netDir, neutrinoDBName)
	neutrinoDB, err := walletdb.Create("bdb", neutrinoDBPath, true, 5*time.Second)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}

	// If we're on regtest and the peers haven't been explicitly set, add the
	// simnet harness alpha node as an additional peer so we don't have to type
	// it in.
	var addPeers []string
	if cfg.chainParams.Name == "regtest" && len(cfg.connectPeers) == 0 {
		addPeers = append(addPeers, "localhost:20575")
	}

	chainService, err := neutrino.NewChainService(neutrino.Config{
		DataDir:      netDir,
		Database:     neutrinoDB,
		ChainParams:  *cfg.chainParams,
		ConnectPeers: cfg.connectPeers,
		AddPeers:     addPeers,
	})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("couldn't create Neutrino ChainService: %v", err)
	}

	return w, loader, chainService, neutrinoDB, nil
}

// startWallet starts neutrino and begins the wallet sync.
func startWallet(loader *wallet.Loader, chainClient *chain.NeutrinoClient) error {
	w, loaded := loader.LoadedWallet()
	if !loaded {
		return fmt.Errorf("wallet not loaded")
	}
	err := chainClient.Start()
	if err != nil {
		return fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.SynchronizeRPC(chainClient)
	return nil
}

// createSPVWallet creates a new SPV wallet.
func createSPVWallet(privPass []byte, seed []byte, dbDir string, net *chaincfg.Params) error {
	netDir := filepath.Join(dbDir, net.Name)
	err := os.MkdirAll(netDir, 0777)
	if err != nil {
		return fmt.Errorf("error creating wallet directories: %v", err)
	}

	loader := wallet.NewLoader(net, netDir, true, 60*time.Second, 250)
	defer loader.UnloadWallet()

	pubPass := []byte(wallet.InsecurePubPassphrase)

	_, err = loader.CreateNewWallet(pubPass, privPass, seed, time.Now())
	if err != nil {
		return fmt.Errorf("CreateNewWallet error: %w", err)
	}

	neutrinoDBPath := filepath.Join(netDir, neutrinoDBName)
	db, err := walletdb.Create("bdb", neutrinoDBPath, true, 5*time.Second)
	if err != nil {
		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("error closing newly created wallet database: %w", err)
	}

	return nil
}

// hashEntry stores a chainhash.Hash with a last-access time that can be used
// for cache maintenance.
type hashEntry struct {
	hash       chainhash.Hash
	lastAccess time.Time
}

// scanCheckpoint is a cached, incomplete filterScanResult. When another scan
// is requested for an outpoint with a cached *scanCheckpoint, the scan can
// pick up where it left off.
type scanCheckpoint struct {
	res        *filterScanResult
	lastAccess time.Time
}

// spvWallet is an in-process btcwallet.Wallet + neutrino light-filter-based
// Bitcoin wallet. spvWallet controls an instance of btcwallet.Wallet directly
// and does not run or connect to the RPC server.
type spvWallet struct {
	ctx         context.Context
	chainParams *chaincfg.Params
	wallet      btcWallet
	cl          neutrinoService
	chainClient *chain.NeutrinoClient
	acctNum     uint32
	neutrinoDB  walletdb.DB

	txBlocksMtx sync.Mutex
	txBlocks    map[chainhash.Hash]*hashEntry

	checkpointMtx sync.Mutex
	checkpoints   map[outPoint]*scanCheckpoint

	log    dex.Logger
	loader *wallet.Loader
}

var _ Wallet = (*spvWallet)(nil)

// loadSPVWallet loads an existing wallet.
func loadSPVWallet(dbDir string, logger dex.Logger, connectPeers []string, chainParams *chaincfg.Params) (*spvWallet, error) {
	w, loader, chainService, neutrinoDB, err := loadChainClient(&spvConfig{
		dbDir:        dbDir,
		chainParams:  chainParams,
		connectPeers: connectPeers,
	})
	if err != nil {
		return nil, err
	}

	return &spvWallet{
		chainParams: chainParams,
		cl:          chainService,
		chainClient: chain.NewNeutrinoClient(chainParams, chainService),
		acctNum:     0,
		wallet:      &walletExtender{w, chainParams},
		neutrinoDB:  neutrinoDB,
		txBlocks:    make(map[chainhash.Hash]*hashEntry),
		checkpoints: make(map[outPoint]*scanCheckpoint),
		log:         logger,
		loader:      loader,
	}, nil
}

// storeTxBlock stores the block hash for the tx in the cache.
func (w *spvWallet) storeTxBlock(txHash, blockHash chainhash.Hash) {
	w.txBlocksMtx.Lock()
	defer w.txBlocksMtx.Unlock()
	w.txBlocks[txHash] = &hashEntry{
		hash:       blockHash,
		lastAccess: time.Now(),
	}
}

// txBlock attempts to retrieve the block hash for the tx from the cache.
func (w *spvWallet) txBlock(txHash chainhash.Hash) (chainhash.Hash, bool) {
	w.txBlocksMtx.Lock()
	defer w.txBlocksMtx.Unlock()
	entry, found := w.txBlocks[txHash]
	if !found {
		return chainhash.Hash{}, false
	}
	entry.lastAccess = time.Now()
	return entry.hash, found
}

// cacheCheckpoint caches a *filterScanResult so that future scans can be
// skipped or shortened.
func (w *spvWallet) cacheCheckpoint(txHash *chainhash.Hash, vout uint32, res *filterScanResult) {
	if res.spend != nil && res.blockHash == nil {
		// Probably set the start time too late. Don't cache anything
		return
	}
	w.checkpointMtx.Lock()
	defer w.checkpointMtx.Unlock()
	w.checkpoints[newOutPoint(txHash, vout)] = &scanCheckpoint{
		res:        res,
		lastAccess: time.Now(),
	}
}

// checkpoint returns any cached *filterScanResult for the outpoint.
func (w *spvWallet) checkpoint(txHash *chainhash.Hash, vout uint32) *filterScanResult {
	w.checkpointMtx.Lock()
	defer w.checkpointMtx.Unlock()
	check, found := w.checkpoints[newOutPoint(txHash, vout)]
	if !found {
		return nil
	}
	check.lastAccess = time.Now()
	return check.res
}

// checkpointBlock returns a filterScanResult and the checkpoint block hash. If
// a result is found with an orphaned checkpoint block hash, it is cleared from
// the cache and not returned.
func (w *spvWallet) checkpointBlock(txHash *chainhash.Hash, vout uint32) (*filterScanResult, *chainhash.Hash) {
	res := w.checkpoint(txHash, vout)
	if res == nil {
		return nil, nil
	}
	blockHash := &res.checkpoint
	if !w.blockIsMainchain(blockHash, -1) {
		// reorg detected, abandon the checkpoint.
		w.checkpointMtx.Lock()
		delete(w.checkpoints, newOutPoint(txHash, vout))
		w.checkpointMtx.Unlock()
		return nil, nil
	}
	return res, blockHash
}

func (w *spvWallet) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	// Not needed for spv wallet.
	return nil, fmt.Errorf("RawRequest not available on spv")
}

func (w *spvWallet) estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	return nil, fmt.Errorf("EstimateSmartFee not available on spv")
}

func (w *spvWallet) ownsAddress(addr btcutil.Address) (bool, error) {
	return w.wallet.HaveAddress(addr)
}

func (w *spvWallet) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	// Fee sanity check?
	err := w.wallet.PublishTransaction(tx, "")
	if err != nil {
		return nil, err
	}
	txHash := tx.TxHash()

	// bitcoind would unlock these, but it seems that btcwallet doesn't, but it
	// seems like they're no longer returned from ListUnspent even if we unlock
	// the outpoint before the transaction is mined.
	for _, txIn := range tx.TxIn {
		w.wallet.UnlockOutpoint(txIn.PreviousOutPoint)
	}

	return &txHash, nil
}

func (w *spvWallet) getBlock(blockHash chainhash.Hash) (*wire.MsgBlock, error) {
	block, err := w.cl.GetBlock(blockHash)
	if err != nil {
		return nil, fmt.Errorf("neutrino GetBlock error: %v", err)
	}

	return block.MsgBlock(), nil
}

func (w *spvWallet) getBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return w.cl.GetBlockHash(blockHeight)
}

func (w *spvWallet) getBlockHeight(h *chainhash.Hash) (int32, error) {
	return w.cl.GetBlockHeight(h)
}

func (w *spvWallet) getBestBlockHash() (*chainhash.Hash, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}
	return &blk.Hash, err
}

func (w *spvWallet) getBestBlockHeight() (int32, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return -1, err
	}
	return blk.Height, err
}

// syncHeight is the best known sync height among peers.
func (w *spvWallet) syncHeight() int32 {
	var maxHeight int32
	for _, p := range w.cl.Peers() {
		tipHeight := p.StartingHeight()
		lastBlockHeight := p.LastBlock()
		if lastBlockHeight > tipHeight {
			tipHeight = lastBlockHeight
		}
		if tipHeight > maxHeight {
			maxHeight = tipHeight
		}
	}
	return maxHeight
}

// syncStatus is information about the wallet's sync status.
func (w *spvWallet) syncStatus() (*syncStatus, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}

	return &syncStatus{
		Target:  w.syncHeight(),
		Height:  blk.Height,
		Syncing: !w.wallet.ChainSynced(),
	}, nil
}

// Balances retrieves a wallet's balance details.
func (w *spvWallet) balances() (*GetBalancesResult, error) {
	bals, err := w.wallet.CalculateAccountBalances(w.acctNum, 0 /* confs */)
	if err != nil {
		return nil, err
	}

	return &GetBalancesResult{
		Mine: Balances{
			Trusted:   bals.Spendable.ToBTC(),
			Untrusted: 0, // ? do we need to scan utxos instead ?
			Immature:  bals.ImmatureReward.ToBTC(),
		},
	}, nil
}

// ListUnspent retrieves list of the wallet's UTXOs.
func (w *spvWallet) listUnspent() ([]*ListUnspentResult, error) {
	unspents, err := w.wallet.ListUnspent(0, math.MaxInt32, "default")
	if err != nil {
		return nil, err
	}
	res := make([]*ListUnspentResult, 0, len(unspents))
	for _, utxo := range unspents {
		// If the utxo is unconfirmed, we should determine whether it's "safe"
		// by seeing if we control the inputs of it's transaction.
		var safe bool
		if utxo.Confirmations > 0 {
			safe = true
		} else {
			txHash, err := chainhash.NewHashFromStr(utxo.TxID)
			if err != nil {
				return nil, fmt.Errorf("error decoding txid %q: %v", utxo.TxID, err)
			}
			tx, _, _, _, err := w.wallet.FetchInputInfo(wire.NewOutPoint(txHash, utxo.Vout))
			if err != nil {
				return nil, fmt.Errorf("FetchInputInfo error: %v", err)
			}
			// To be "safe", we need to show that we own the inputs for the
			// utxo's transaction. We'll just try to find one.
			for _, txIn := range tx.TxIn {
				_, _, _, _, err := w.wallet.FetchInputInfo(&txIn.PreviousOutPoint)
				if err == nil {
					safe = true
					break
				}
			}
		}

		pkScript, err := hex.DecodeString(utxo.ScriptPubKey)
		if err != nil {
			return nil, err
		}

		redeemScript, err := hex.DecodeString(utxo.RedeemScript)
		if err != nil {
			return nil, err
		}

		res = append(res, &ListUnspentResult{
			TxID:    utxo.TxID,
			Vout:    utxo.Vout,
			Address: utxo.Address,
			// Label: ,
			ScriptPubKey:  pkScript,
			Amount:        utxo.Amount,
			Confirmations: uint32(utxo.Confirmations),
			RedeemScript:  redeemScript,
			Spendable:     utxo.Spendable,
			// Solvable: ,
			Safe: safe,
		})
	}
	return res, nil
}

// lockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (w *spvWallet) lockUnspent(unlock bool, ops []*output) error {
	switch {
	case unlock && len(ops) == 0:
		w.wallet.ResetLockedOutpoints()
	default:
		for _, op := range ops {
			op := wire.OutPoint{Hash: op.pt.txHash, Index: op.pt.vout}
			if unlock {
				w.wallet.UnlockOutpoint(op)
			} else {
				w.wallet.LockOutpoint(op)
			}
		}
	}
	return nil
}

// listLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.
func (w *spvWallet) listLockUnspent() ([]*RPCOutpoint, error) {
	outpoints := w.wallet.LockedOutpoints()
	pts := make([]*RPCOutpoint, 0, len(outpoints))
	for _, pt := range outpoints {
		pts = append(pts, &RPCOutpoint{
			TxID: pt.Txid,
			Vout: pt.Vout,
		})
	}
	return pts, nil
}

// changeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (w *spvWallet) changeAddress() (btcutil.Address, error) {
	return w.wallet.NewChangeAddress(w.acctNum, waddrmgr.KeyScopeBIP0084)
}

// AddressPKH gets a new base58-encoded (P2PKH) external address from the
// wallet.
func (w *spvWallet) addressPKH() (btcutil.Address, error) {
	return nil, fmt.Errorf("unimplemented")
}

// addressWPKH gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (w *spvWallet) addressWPKH() (btcutil.Address, error) {
	return w.wallet.NewAddress(w.acctNum, waddrmgr.KeyScopeBIP0084)
}

// signTx attempts to have the wallet sign the transaction inputs.
func (w *spvWallet) signTx(tx *wire.MsgTx) (*wire.MsgTx, error) {
	// Can't use btcwallet.Wallet.SignTransaction, because it doesn't work for
	// segwit transactions (for real?).
	return tx, w.wallet.signTransaction(tx)
}

// privKeyForAddress retrieves the private key associated with the specified
// address.
func (w *spvWallet) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	a, err := btcutil.DecodeAddress(addr, w.chainParams)
	if err != nil {
		return nil, err
	}
	return w.wallet.PrivKeyForAddress(a)
}

// Unlock unlocks the wallet.
func (w *spvWallet) Unlock(pass string) error {
	return w.wallet.Unlock([]byte(pass), time.After(time.Duration(math.MaxInt64)))
}

// Lock locks the wallet.
func (w *spvWallet) Lock() error {
	w.wallet.Lock()
	return nil
}

// sendToAddress sends the amount to the address. feeRate is in units of
// atoms/byte.
func (w *spvWallet) sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	addr, err := btcutil.DecodeAddress(address, w.chainParams)
	if err != nil {
		return nil, err
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}

	wireOP := wire.NewTxOut(int64(value), pkScript)

	feeRateAmt, err := btcutil.NewAmount(float64(feeRate) / 1e5)
	if err != nil {
		return nil, err
	}

	// Could try with minconf 1 first.
	tx, err := w.wallet.SendOutputs([]*wire.TxOut{wireOP}, nil, w.acctNum, 0, feeRateAmt, "")
	if err != nil {
		return nil, err
	}

	txHash := tx.TxHash()

	return &txHash, nil
}

// swapConfirmations attempts to get the numbe of confirmations and the spend
// status for the specified tx output. For swap outputs that were not generated
// by this wallet, startTime must be supplied to limit the search. Use the match
// time assigned by the server.
func (w *spvWallet) swapConfirmations(txHash *chainhash.Hash, vout uint32, pkScript []byte,
	startTime time.Time) (confs uint32, spent bool, err error) {

	// First, check if it's a wallet transaction. We probably won't be able
	// to see the spend status, since the wallet doesn't track the swap contract
	// output, but we can get the block if it's been mined.
	blockHash, confs, spent, err := w.confirmations(txHash, vout)
	if err == nil {
		return confs, spent, nil
	}
	var assumedMempool bool
	switch err {
	case WalletTransactionNotFound:
	case SpentStatusUnknown:
		if blockHash == nil {
			// We generated this swap, but it probably hasn't been mined yet.
			// It's SpentStatusUnknown because the wallet doesn't track the
			// spend status of the swap contract output itself, since it's not
			// recognized as a wallet output. We'll still try to find the
			// confirmations with other means, but if we can't find it, we'll
			// report it as a zero-conf unspent output. This ignores the remote
			// possibility that the output could be both in mempool and spent.
			assumedMempool = true
		}
	default:
		return 0, false, err
	}

	// If we still don't have the block hash, we may have it stored. Check the
	// dex database first. This won't give us the confirmations and spent
	// status, but it will allow us to short circuit a longer scan if we already
	// know the output is spent.
	if blockHash == nil {
		blockHash, _ = w.mainchainBlockForStoredTx(txHash)
	}

	// Our last option is neutrino.
	utxo, err := w.scanFilters(txHash, vout, pkScript, startTime, blockHash)
	if err != nil {
		return 0, false, err
	}

	if utxo.spend == nil && utxo.blockHash == nil {
		if assumedMempool {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("output %s:%v not found with search parameters startTime = %s, pkScript = %x",
			txHash, vout, startTime, pkScript)
	}

	if utxo.blockHash != nil {
		bestHeight, err := w.getBestBlockHeight()
		if err != nil {
			return 0, false, fmt.Errorf("getBestBlockHeight error: %v", err)
		}
		confs = uint32(bestHeight) - utxo.blockHeight + 1
	}

	if utxo.spend != nil {
		// This is a sticky situation. Neutrino will only tell us about the
		// spend OR the utxo, not both. So if we get a spending transaction, it
		// would take additional work to get the confirmation on the original
		// output. But do we actually care at that point? I'm guessing that's
		// Neutrino's philosophy, and maybe it should be ours here too.
		return confs, true, nil
	}

	// unspent
	return confs, false, nil
}

func (w *spvWallet) locked() bool {
	return w.wallet.Locked()
}

func (w *spvWallet) walletLock() error {
	w.wallet.Lock()
	return nil
}

func (w *spvWallet) walletUnlock(pass string) error {
	return w.Unlock(pass)
}

func (w *spvWallet) getBlockHeader(hashStr string) (*blockHeader, error) {
	blockHash, err := chainhash.NewHashFromStr(hashStr)
	if err != nil {
		return nil, err
	}
	hdr, err := w.cl.GetBlockHeader(blockHash)
	if err != nil {
		return nil, err
	}

	medianTime, err := w.calcMedianTime(blockHash)
	if err != nil {
		return nil, err
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, fmt.Errorf("BestBlock error: %v", err)
	}

	blockHeight, err := w.cl.GetBlockHeight(blockHash)
	if err != nil {
		return nil, err
	}

	confs := uint32(tip.Height - blockHeight)

	return &blockHeader{
		Hash:          hdr.BlockHash().String(),
		Confirmations: int64(confs),
		Height:        int64(blockHeight),
		Time:          hdr.Timestamp.Unix(),
		MedianTime:    medianTime.Unix(),
	}, nil
}

const medianTimeBlocks = 11

// calcMedianTime calculates the median time of the previous 11 block headers.
// The median time is used for validating time-locked transactions. See notes in
// btcd/blockchain (*blockNode).CalcPastMedianTime() regarding incorrectly
// calculated median time for blocks 1, 3, 5, 7, and 9.
func (w *spvWallet) calcMedianTime(blockHash *chainhash.Hash) (time.Time, error) {
	timestamps := make([]int64, 0, medianTimeBlocks)

	zeroHash := chainhash.Hash{}

	h := blockHash
	for i := 0; i < medianTimeBlocks; i++ {
		hdr, err := w.cl.GetBlockHeader(h)
		if err != nil {
			return time.Time{}, fmt.Errorf("BlockHeader error for hash %q: %v", h, err)
		}
		timestamps = append(timestamps, hdr.Timestamp.Unix())

		if hdr.PrevBlock == zeroHash {
			break
		}
		h = &hdr.PrevBlock
	}

	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})

	medianTimestamp := timestamps[len(timestamps)/2]
	return time.Unix(medianTimestamp, 0), nil
}

// connect will start the wallet and begin syncing.
func (w *spvWallet) connect(ctx context.Context) error {
	w.ctx = ctx

	err := startWallet(w.loader, w.chainClient)
	if err != nil {
		return err
	}

	// Possible to subscribe to block notifications here with a NewRescan ->
	// *Rescan supplied with a QuitChan-type RescanOption.
	// Actually, should use btcwallet.Wallet.NtfnServer ?

	// Nanny for the spendingTxs cache. We'll keep the cache entries for 2 hours
	// past their last access time.
	go func() {
		ticker := time.NewTicker(time.Minute * 20)
		expiration := time.Hour * 2
		for {
			select {
			case <-ticker.C:
				w.txBlocksMtx.Lock()
				for txHash, entry := range w.txBlocks {
					if time.Since(entry.lastAccess) > expiration {
						delete(w.txBlocks, txHash)
					}
				}
				w.txBlocksMtx.Unlock()

				w.checkpointMtx.Lock()
				for outPt, check := range w.checkpoints {
					if time.Since(check.lastAccess) > expiration {
						delete(w.checkpoints, outPt)
					}
				}
				w.checkpointMtx.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// stop stops the wallet and database threads.
func (w *spvWallet) stop() {
	if err := w.loader.UnloadWallet(); err != nil {
		w.log.Errorf("UnloadWallet error: %v", err)
	}
	if w.chainClient != nil {
		w.chainClient.Stop()
		w.chainClient.WaitForShutdown()
	}
	if err := w.cl.Stop(); err != nil {
		w.log.Errorf("error stopping neutrino chain service: %v", err)
	}
	if err := w.neutrinoDB.Close(); err != nil {
		w.log.Errorf("wallet db close error: %v", err)
	}

	w.log.Debugf("SPV wallet closed")
}

// blockForStoredTx looks for a block hash in the txBlocks index.
func (w *spvWallet) blockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32, error) {
	// Check if we know the block hash for the tx.
	blockHash, found := w.txBlock(*txHash)
	if !found {
		return nil, 0, nil
	}
	// Check that the block is still mainchain.
	blockHeight, err := w.cl.GetBlockHeight(&blockHash)
	if err != nil {
		w.log.Errorf("Error retrieving block height for hash %s: %v", blockHash, err)
		return nil, 0, err
	}
	return &blockHash, blockHeight, nil
}

// blockIsMainchain will be true if the blockHash is that of a mainchain block.
func (w *spvWallet) blockIsMainchain(blockHash *chainhash.Hash, blockHeight int32) bool {
	if blockHeight < 0 {
		var err error
		blockHeight, err = w.cl.GetBlockHeight(blockHash)
		if err != nil {
			w.log.Errorf("Error getting block height for hash %s", blockHash)
			return false
		}
	}
	checkHash, err := w.cl.GetBlockHash(int64(blockHeight))
	if err != nil {
		w.log.Errorf("Error retriving block hash for height %d", blockHeight)
		return false
	}

	return *checkHash == *blockHash
}

// mainchainBlockForStoredTx gets the block hash and height for the transaction
// IFF an entry has been stored in the blockTxs index.
func (w *spvWallet) mainchainBlockForStoredTx(txHash *chainhash.Hash) (*chainhash.Hash, int32) {
	// Check that the block is still mainchain.
	blockHash, blockHeight, err := w.blockForStoredTx(txHash)
	if err != nil {
		w.log.Errorf("Error retrieving mainchain block height for hash %s", blockHash)
		return nil, 0
	}
	if blockHash == nil {
		return nil, 0
	}
	if !w.blockIsMainchain(blockHash, blockHeight) {
		return nil, 0
	}
	return blockHash, blockHeight
}

// findBlockForTime locates a good start block so that a search beginning at the
// returned block has a very low likelyhood of missing any blocks that have time
// > matchTime. This is done by performing a binary search (sort.Search) to find
// a block with a block time MAX_FUTURE_BLOCK_TIME before matchTime. To ensure
// we also accommodate the median-block time rule and aren't missing anything
// due to out of sequence block times we use an unsophisticated algorithm of
// choosing the first block in an 11 block window with no times >= matchTime.
func (w *spvWallet) findBlockForTime(matchTime time.Time) (*chainhash.Hash, int32, error) {
	offsetTime := matchTime.Add(-maxBlockTimeOffset)

	bestHeight, err := w.getBestBlockHeight()
	if err != nil {
		return nil, 0, fmt.Errorf("getBestBlockHeight error: %v", err)
	}

	getBlockTimeForHeight := func(height int32) (*chainhash.Hash, time.Time, error) {
		hash, err := w.cl.GetBlockHash(int64(height))
		if err != nil {
			return nil, time.Time{}, err
		}
		header, err := w.cl.GetBlockHeader(hash)
		if err != nil {
			return nil, time.Time{}, err
		}
		return hash, header.Timestamp, nil
	}

	iHeight := sort.Search(int(bestHeight), func(h int) bool {
		var iTime time.Time
		_, iTime, err = getBlockTimeForHeight(int32(h))
		if err != nil {
			return true
		}
		return iTime.After(offsetTime)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("binary search error finding best block for time %q: %w", matchTime, err)
	}

	// We're actually breaking an assumption of sort.Search here because block
	// times aren't always monotonically increasing. This won't matter though
	// as long as there are not > blockTimeSearchBuffer blocks with inverted
	// time order.
	var count int
	var iHash *chainhash.Hash
	var iTime time.Time
	for iHeight > 0 {
		iHash, iTime, err = getBlockTimeForHeight(int32(iHeight))
		if err != nil {
			return nil, 0, fmt.Errorf("getBlockTimeForHeight error: %w", err)
		}
		if iTime.Before(offsetTime) {
			count++
			if count == medianTimeBlocks {
				return iHash, int32(iHeight), nil
			}
		} else {
			count = 0
		}
		iHeight--
	}
	return w.chainParams.GenesisHash, 0, nil

}

// scanFilters enables searching for an output and its spending input by
// scanning BIP158 compact filters. Caller should supply either blockHash or
// startTime. blockHash takes precedence. If blockHash is supplied, the scan
// will start at that block and continue to the current blockchain tip, or until
// both the output and a spending transaction is found. if startTime is
// supplied, and the blockHash for the output is not known to the wallet, a
// candidate block will be selected with findBlockTime.
func (w *spvWallet) scanFilters(txHash *chainhash.Hash, vout uint32, pkScript []byte, startTime time.Time, blockHash *chainhash.Hash) (*filterScanResult, error) {
	// Check if we know the block hash for the tx.
	var limitHeight int32
	// See if we have a checkpoint to use.
	checkPt, checkBlock := w.checkpointBlock(txHash, vout)
	if checkPt != nil {
		if checkPt.blockHash != nil && checkPt.spend != nil {
			// We already have the output and the spending input, and
			// checkpointBlock already verified it's still mainchain.
			return checkPt, nil
		}
		height, err := w.getBlockHeight(checkBlock)
		if err != nil {
			return nil, fmt.Errorf("getBlockHeight error: %w", err)
		}
		limitHeight = height + 1
		blockHash = checkBlock
	} else if blockHash == nil {
		blockHash, limitHeight = w.mainchainBlockForStoredTx(txHash)
		// blockHash nil if not found
	} else {
		var err error
		limitHeight, err = w.getBlockHeight(blockHash)
		if err != nil {
			return nil, fmt.Errorf("error getting height for supplied block hash %s", blockHash)
		}
	}

	// We only care about the limitHeight now, but we can tell if it's been set
	// by whether blockHash is nil, since that means it wasn't passed in, and
	// it wasn't found in the database.
	if blockHash == nil {
		var err error
		_, limitHeight, err = w.findBlockForTime(startTime)
		if err != nil {
			return nil, err
		}
	}

	// Do a filter scan.
	utxo, err := w.filterScanFromHeight(*txHash, vout, pkScript, limitHeight, checkPt)
	if err != nil {
		return nil, fmt.Errorf("filterScanFromHeight error: %w", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}

	// If we found a block, let's store a reference in our local database so we
	// can maybe bypass a long search next time.
	if utxo.blockHash != nil {
		w.storeTxBlock(*txHash, *utxo.blockHash)
	}

	w.cacheCheckpoint(txHash, vout, utxo)

	return utxo, nil
}

// getTxOut finds an unspent transaction output and its number of confirmations.
// To match the behavior of the RPC method, even if an output is found, if it's
// known to be spent, no *wire.TxOut and no error will be returned.
func (w *spvWallet) getTxOut(txHash *chainhash.Hash, vout uint32, pkScript []byte, startTime time.Time) (*wire.TxOut, uint32, error) {
	// Check for a wallet transaction first
	txDetails, err := w.wallet.walletTransaction(txHash)
	if err == nil {
		tip, err := w.cl.BestBlock()
		if err != nil {
			return nil, 0, fmt.Errorf("BestBlock error: %v", err)
		}

		var confs uint32
		if txDetails.Block.Height > 0 {
			confs = uint32(tip.Height - txDetails.Block.Height + 1)
		}
		msgTx := &txDetails.MsgTx
		if len(msgTx.TxOut) < int(vout+1) {
			return nil, 0, fmt.Errorf("wallet transaction %s found, but not enough outputs for vout %d", txHash, vout)
		}
		return msgTx.TxOut[vout], confs, nil
	}
	if !errors.Is(err, WalletTransactionNotFound) {
		return nil, 0, fmt.Errorf("walletTransaction error: %w", err)
	}

	var blockHash *chainhash.Hash
	if txDetails != nil && txDetails.Block.Hash != (chainhash.Hash{}) {
		blockHash = &txDetails.Block.Hash
	}

	// We don't really know if it's spent, so we'll need to scan.
	utxo, err := w.scanFilters(txHash, vout, pkScript, startTime, blockHash)
	if err != nil {
		return nil, 0, err
	}

	if utxo == nil || utxo.spend != nil {
		return nil, 0, nil
	}

	if utxo.blockHash == nil {
		return nil, 0, fmt.Errorf("output %s:%v not found", txHash, vout)
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, 0, fmt.Errorf("BestBlock error: %v", err)
	}

	confs := uint32(tip.Height) - utxo.blockHeight + 1

	return utxo.txOut, confs, nil
}

// filterScanFromHeight scans BIP158 filters beginning at the specified block
// height until the tip, or until a spending transaction is found.
func (w *spvWallet) filterScanFromHeight(txHash chainhash.Hash, vout uint32, pkScript []byte, startBlockHeight int32, checkPt *filterScanResult) (*filterScanResult, error) {
	tip, err := w.getBestBlockHeight()
	if err != nil {
		return nil, err
	}

	res := checkPt
	if res == nil {
		res = new(filterScanResult)
	}

search:
	for height := startBlockHeight; height <= tip; height++ {
		if res.spend != nil && res.blockHash == nil {
			w.log.Debugf("A spending input was found during the scan but the output " +
				"itself wasn't found. Was the startBlockHeight early enough?")
			return res, nil
		}
		blockHash, err := w.getBlockHash(int64(height))
		if err != nil {
			return nil, fmt.Errorf("error getting block hash for height %d: %w", height, err)
		}
		res.checkpoint = *blockHash
		matched, err := w.matchPkScript(blockHash, [][]byte{pkScript})
		if err != nil {
			return nil, fmt.Errorf("matchPkScript error: %w", err)
		}
		if !matched {
			continue search
		}
		// Pull the block.
		block, err := w.cl.GetBlock(*blockHash)
		if err != nil {
			return nil, fmt.Errorf("GetBlock error: %v", err)
		}
		msgBlock := block.MsgBlock()

		// Scan every transaction.
	nextTx:
		for _, tx := range msgBlock.Transactions {
			// Look for a spending input.
			if res.spend == nil {
				for vin, txIn := range tx.TxIn {
					prevOut := &txIn.PreviousOutPoint
					if prevOut.Hash == txHash && prevOut.Index == vout {
						res.spend = &spendingInput{
							txHash:      tx.TxHash(),
							vin:         uint32(vin),
							blockHash:   *block.Hash(),
							blockHeight: uint32(height),
						}
						if res.blockHash != nil {
							break search
						}
						// The output could still be in this block, just not
						// in this transaction.
						continue nextTx
					}
				}
			}
			// Only check for the output if this is the right transaction.
			if tx.TxHash() != txHash {
				continue nextTx
			}
			for _, txOut := range tx.TxOut {
				if bytes.Equal(txOut.PkScript, pkScript) {
					res.blockHash = block.Hash()
					res.blockHeight = uint32(height)
					res.txOut = txOut
					if res.spend != nil {
						break search
					}
					// Keep looking for the spending transaction.
					continue nextTx
				}
			}
		}
	}
	return res, nil
}

// matchPkScript pulls the filter for the block and attempts to match the
// supplied scripts.
func (w *spvWallet) matchPkScript(blockHash *chainhash.Hash, scripts [][]byte) (bool, error) {
	filter, err := w.cl.GetCFilter(*blockHash, wire.GCSFilterRegular)
	if err != nil {
		return false, fmt.Errorf("GetCFilter error: %w", err)
	}
	var filterKey [gcs.KeySize]byte
	copy(filterKey[:], blockHash[:])

	matchFound, err := filter.MatchAny(filterKey, scripts)
	if err != nil {
		return false, fmt.Errorf("MatchAny error: %w", err)
	}
	return matchFound, nil
}

// getWalletTransaction checks the wallet database for the specified
// transaction. Only transactions with output scripts that pay to the wallet or
// transactions that spend wallet outputs are stored in the wallet database.
func (w *spvWallet) getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	return w.getTransaction(txHash)
}

// searchBlockForRedemptions attempts to find spending info for the specified
// contracts by searching every input of all txs in the provided block range.
func (w *spvWallet) searchBlockForRedemptions(ctx context.Context, reqs map[outPoint]*findRedemptionReq,
	blockHash chainhash.Hash) (discovered map[outPoint]*findRedemptionResult) {

	// Just match all the scripts together.
	scripts := make([][]byte, 0, len(reqs))
	for _, req := range reqs {
		scripts = append(scripts, req.pkScript)
	}

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	matchFound, err := w.matchPkScript(&blockHash, scripts)
	if err != nil {
		w.log.Errorf("matchPkScript error: %w", err)
		return
	}

	if !matchFound {
		return
	}

	// There is at least one match. Pull the block.
	block, err := w.cl.GetBlock(blockHash)
	if err != nil {
		w.log.Errorf("neutrino GetBlock error: %v", err)
		return
	}

	for _, msgTx := range block.MsgBlock().Transactions {
		newlyDiscovered := findRedemptionsInTx(ctx, true, reqs, msgTx, w.chainParams)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

// findRedemptionsInMempool is unsupported for SPV.
func (w *spvWallet) findRedemptionsInMempool(ctx context.Context, reqs map[outPoint]*findRedemptionReq) (discovered map[outPoint]*findRedemptionResult) {
	return
}

// confirmations looks for the confirmation count and spend status on a
// transaction output that pays to this wallet.
func (w *spvWallet) confirmations(txHash *chainhash.Hash, vout uint32) (blockHash *chainhash.Hash, confs uint32, spent bool, err error) {
	details, err := w.wallet.walletTransaction(txHash)
	if err != nil {
		return nil, 0, false, err
	}
	if details == nil {
		return nil, 0, false, WalletTransactionNotFound
	}

	if details.Block.Hash != (chainhash.Hash{}) {
		blockHash = &details.Block.Hash
	}

	for _, credit := range details.Credits {
		if credit.Index == vout {
			syncBlock := w.wallet.syncedTo() // Better than chainClient.GetBestBlockHeight() ?
			return blockHash, uint32(confirms(details.Block.Height, syncBlock.Height)), credit.Spent, nil
		}
	}

	return blockHash, 0, false, SpentStatusUnknown
}

// getTransaction retrieves the specified wallet-related transaction.
// This is pretty much a copy-past from btcwallet 'gettransaction' JSON-RPC
// handler.
func (w *spvWallet) getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	// Option # 1 just copies from UnstableAPI.TxDetails. Duplicating the
	// unexported bucket key feels dirty.
	//
	// var details *wtxmgr.TxDetails
	// err := walletdb.View(w.Database(), func(dbtx walletdb.ReadTx) error {
	// 	txKey := []byte("wtxmgr")
	// 	txmgrNs := dbtx.ReadBucket(txKey)
	// 	var err error
	// 	details, err = w.TxStore.TxDetails(txmgrNs, txHash)
	// 	return err
	// })

	// Option #2
	// This is what the JSON-RPC does (and has since at least May 2018).
	details, err := w.wallet.walletTransaction(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, WalletTransactionNotFound
	}

	syncBlock := w.wallet.syncedTo()

	// TODO: The serialized transaction is already in the DB, so
	// reserializing can be avoided here.
	var txBuf bytes.Buffer
	txBuf.Grow(details.MsgTx.SerializeSize())
	err = details.MsgTx.Serialize(&txBuf)
	if err != nil {
		return nil, err
	}

	ret := &GetTransactionResult{
		TxID:         txHash.String(),
		Hex:          txBuf.Bytes(), // 'Hex' field name is a lie, kinda
		Time:         uint64(details.Received.Unix()),
		TimeReceived: uint64(details.Received.Unix()),
	}

	if details.Block.Height != -1 {
		ret.BlockHash = details.Block.Hash.String()
		ret.BlockTime = uint64(details.Block.Time.Unix())
		ret.Confirmations = uint64(confirms(details.Block.Height, syncBlock.Height))
	}

	var (
		debitTotal  btcutil.Amount
		creditTotal btcutil.Amount // Excludes change
		fee         btcutil.Amount
		feeF64      float64
	)
	for _, deb := range details.Debits {
		debitTotal += deb.Amount
	}
	for _, cred := range details.Credits {
		if !cred.Change {
			creditTotal += cred.Amount
		}
	}
	// Fee can only be determined if every input is a debit.
	if len(details.Debits) == len(details.MsgTx.TxIn) {
		var outputTotal btcutil.Amount
		for _, output := range details.MsgTx.TxOut {
			outputTotal += btcutil.Amount(output.Value)
		}
		fee = debitTotal - outputTotal
		feeF64 = fee.ToBTC()
	}

	if len(details.Debits) == 0 {
		// Credits must be set later, but since we know the full length
		// of the details slice, allocate it with the correct cap.
		ret.Details = make([]*WalletTxDetails, 0, len(details.Credits))
	} else {
		ret.Details = make([]*WalletTxDetails, 1, len(details.Credits)+1)

		ret.Details[0] = &WalletTxDetails{
			Category: "send",
			Amount:   (-debitTotal).ToBTC(), // negative since it is a send
			Fee:      feeF64,
		}
		ret.Fee = feeF64
	}

	credCat := wallet.RecvCategory(details, syncBlock.Height, w.chainParams).String()
	for _, cred := range details.Credits {
		// Change is ignored.
		if cred.Change {
			continue
		}

		var address string
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(
			details.MsgTx.TxOut[cred.Index].PkScript, w.chainParams)
		if err == nil && len(addrs) == 1 {
			addr := addrs[0]
			address = addr.EncodeAddress()
		}

		ret.Details = append(ret.Details, &WalletTxDetails{
			Address:  address,
			Category: WalletTxCategory(credCat),
			Amount:   cred.Amount.ToBTC(),
			Vout:     cred.Index,
		})
	}

	ret.Amount = creditTotal.ToBTC()
	return ret, nil
}

// walletExtender gives us access to a handful of fields or methods on
// *wallet.Wallet that don't make sense to stub out for testing.
type walletExtender struct {
	*wallet.Wallet
	chainParams *chaincfg.Params
}

// walletTransaction pulls the transaction from the database.
func (w *walletExtender) walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	details, err := wallet.UnstableAPI(w.Wallet).TxDetails(txHash)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, WalletTransactionNotFound
	}

	return details, nil
}

func (w *walletExtender) syncedTo() waddrmgr.BlockStamp {
	return w.Manager.SyncedTo()
}

// signTransaction signs the transaction inputs.
func (w *walletExtender) signTransaction(tx *wire.MsgTx) error {
	var prevPkScripts [][]byte
	var inputValues []btcutil.Amount
	for _, txIn := range tx.TxIn {
		_, txOut, _, _, err := w.FetchInputInfo(&txIn.PreviousOutPoint)
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
	return walletdb.View(w.Database(), func(dbtx walletdb.ReadTx) error {
		return txauthor.AddAllInputScripts(tx, prevPkScripts, inputValues, &secretSource{w, w.chainParams})
	})
}

// spendingInput is added to a filterScanResult if a spending input is found.
type spendingInput struct {
	txHash      chainhash.Hash
	vin         uint32
	blockHash   chainhash.Hash
	blockHeight uint32
}

// filterScanResult is the result from a filter scan.
type filterScanResult struct {
	// blockHash is the block that the output was found in.
	blockHash *chainhash.Hash
	// blockHeight is the height of the block that the output was found in.
	blockHeight uint32
	// txOut is the output itself.
	txOut *wire.TxOut
	// spend will be set if a spending input is found.
	spend *spendingInput
	// checkpoint is used to track the last block scanned so that future scans
	// can skip scanned blocks.
	checkpoint chainhash.Hash
}

// secretSource is used to locate keys and redemption scripts while signing a
// transaction. secretSource satisfies the txauthor.SecretsSource interface.
type secretSource struct {
	w           *walletExtender
	chainParams *chaincfg.Params
}

// ChainParams returns the chain parameters.
func (s *secretSource) ChainParams() *chaincfg.Params {
	return s.chainParams
}

// GetKey fetches a private key for the specified address.
func (s *secretSource) GetKey(addr btcutil.Address) (*btcec.PrivateKey, bool, error) {
	ma, err := s.w.AddressInfo(addr)
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
	ma, err := s.w.AddressInfo(addr)
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

func confirms(txHeight, curHeight int32) int32 {
	switch {
	case txHeight == -1, txHeight > curHeight:
		return 0
	default:
		return curHeight - txHeight + 1
	}
}

// chain.Interface
// type Interface interface {
// 	Start() error
// 	Stop()
// 	WaitForShutdown()
// 	GetBestBlock() (*chainhash.Hash, int32, error)
// 	GetBlock(*chainhash.Hash) (*wire.MsgBlock, error)
// 	GetBlockHash(int64) (*chainhash.Hash, error)
// 	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
// 	IsCurrent() bool
// 	FilterBlocks(*FilterBlocksRequest) (*FilterBlocksResponse, error)
// 	BlockStamp() (*waddrmgr.BlockStamp, error)
// 	SendRawTransaction(*wire.MsgTx, bool) (*chainhash.Hash, error)
// 	Rescan(*chainhash.Hash, []btcutil.Address, map[wire.OutPoint]btcutil.Address) error
// 	NotifyReceived([]btcutil.Address) error
// 	NotifyBlocks() error
// 	Notifications() <-chan interface{}
// 	BackEnd() string
// }

// func (w *Wallet) MakeMultiSigScript(addrs []btcutil.Address, nRequired int) ([]byte, error)
// func (w *Wallet) ImportP2SHRedeemScript(script []byte) (*btcutil.AddressScriptHash, error)
// func (w *Wallet) SubmitRescan(job *RescanJob) <-chan error
// func (w *Wallet) Rescan(addrs []btcutil.Address, unspent []wtxmgr.Credit) error
// func (w *Wallet) UnspentOutputs(policy OutputSelectionPolicy) ([]*TransactionOutput, error)
// func (w *Wallet) Start() // called by loader.OpenExistingWallet
// func (w *Wallet) SynchronizeRPC(chainClient chain.Interface)
// func (w *Wallet) ChainClient() chain.Interface
// func (w *Wallet) Stop()
// func (w *Wallet) ShuttingDown() bool
// func (w *Wallet) WaitForShutdown()
// func (w *Wallet) SynchronizingToNetwork() bool
// func (w *Wallet) ChainSynced() bool
// func (w *Wallet) SetChainSynced(synced bool)
// func (w *Wallet) CreateSimpleTx(account uint32, outputs []*wire.TxOut, minconf int32, satPerKb btcutil.Amount, dryRun bool)
// func (w *Wallet) Unlock(passphrase []byte, lock <-chan time.Time) error
// func (w *Wallet) Lock()
// func (w *Wallet) Locked() bool
// func (w *Wallet) ChangePrivatePassphrase(old, new []byte) error
// func (w *Wallet) ChangePublicPassphrase(old, new []byte) error
// func (w *Wallet) ChangePassphrases(publicOld, publicNew, privateOld, privateNew []byte) error
// func (w *Wallet) ChangePassphrases(publicOld, publicNew, privateOld, privateNew []byte) error
// func (w *Wallet) AccountAddresses(account uint32) (addrs []btcutil.Address, err error)
// func (w *Wallet) CalculateBalance(confirms int32) (btcutil.Amount, error)
// func (w *Wallet) CalculateAccountBalances(account uint32, confirms int32) (Balances, error)
// func (w *Wallet) CurrentAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) PubKeyForAddress(a btcutil.Address) (*btcec.PublicKey, error)
// func (w *Wallet) LabelTransaction(hash chainhash.Hash, label string, overwrite bool) error
// func (w *Wallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error)
// func (w *Wallet) HaveAddress(a btcutil.Address) (bool, error)
// func (w *Wallet) AccountOfAddress(a btcutil.Address) (uint32, error)
// func (w *Wallet) AddressInfo(a btcutil.Address) (waddrmgr.ManagedAddress, error)
// func (w *Wallet) AccountNumber(scope waddrmgr.KeyScope, accountName string) (uint32, error)
// func (w *Wallet) AccountName(scope waddrmgr.KeyScope, accountNumber uint32) (string, error)
// func (w *Wallet) AccountProperties(scope waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error)
// func (w *Wallet) RenameAccount(scope waddrmgr.KeyScope, account uint32, newName string) error
// func (w *Wallet) NextAccount(scope waddrmgr.KeyScope, name string) (uint32, error)
// func (w *Wallet) ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListTransactions(from, count int) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListAddressTransactions(pkHashes map[string]struct{}) ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) ListAllTransactions() ([]btcjson.ListTransactionsResult, error)
// func (w *Wallet) GetTransactions(startBlock, endBlock *BlockIdentifier, cancel <-chan struct{}) (*GetTransactionsResult, error)
// func (w *Wallet) Accounts(scope waddrmgr.KeyScope) (*AccountsResult, error)
// func (w *Wallet) AccountBalances(scope waddrmgr.KeyScope, requiredConfs int32) ([]AccountBalanceResult, error)
// func (w *Wallet) ListUnspent(minconf, maxconf int32, addresses map[string]struct{}) ([]*btcjson.ListUnspentResult, error)
// func (w *Wallet) DumpPrivKeys() ([]string, error)
// func (w *Wallet) DumpWIFPrivateKey(addr btcutil.Address) (string, error)
// func (w *Wallet) ImportPrivateKey(scope waddrmgr.KeyScope, wif *btcutil.WIF, bs *waddrmgr.BlockStamp, rescan bool) (string, error)
// func (w *Wallet) LockedOutpoint(op wire.OutPoint) bool
// func (w *Wallet) LockOutpoint(op wire.OutPoint)
// func (w *Wallet) UnlockOutpoint(op wire.OutPoint)
// func (w *Wallet) ResetLockedOutpoints()
// func (w *Wallet) LockedOutpoints() []btcjson.TransactionInput
// func (w *Wallet) LeaseOutput(id wtxmgr.LockID, op wire.OutPoint) (time.Time, error)
// func (w *Wallet) ReleaseOutput(id wtxmgr.LockID, op wire.OutPoint) error
// func (w *Wallet) SortedActivePaymentAddresses() ([]string, error)
// func (w *Wallet) NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error)
// func (w *Wallet) TotalReceivedForAccounts(scope waddrmgr.KeyScope, minConf int32) ([]AccountTotalReceivedResult, error)
// func (w *Wallet) TotalReceivedForAddr(addr btcutil.Address, minConf int32) (btcutil.Amount, error)
// func (w *Wallet) SendOutputs(outputs []*wire.TxOut, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error)
// func (w *Wallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType,
// 	                            additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
// 	                            additionalKeysByAddress map[string]*btcutil.WIF,
// 	                            p2shRedeemScriptsByAddress map[string][]byte) ([]SignatureError, error)
// func (w *Wallet) PublishTransaction(tx *wire.MsgTx, label string) error
// func (w *Wallet) ChainParams() *chaincfg.Params
// func (w *Wallet) Database() walletdb.DB
// func (w *Wallet) FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, int64, error)
