// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// spvWallet implements a Wallet backed by a built-in btcwallet + Neutrino.
//
// There are a few challenges presented in using an SPV wallet for DEX.
// 1. Finding non-wallet related blockchain data requires possession of the
//    pubkey script, not just transaction hash and output index
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
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/gcs"
	"github.com/btcsuite/btcutil/psbt"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/wallet/txauthor"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb" // bdb init() registers a driver
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/jrick/logrotate/rotator"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	WalletTransactionNotFound = dex.ErrorKind("wallet transaction not found")
	SpentStatusUnknown        = dex.ErrorKind("spend status not known")

	// defaultBroadcastWait is long enough for btcwallet's PublishTransaction
	// method to record the outgoing transaction and queue it for broadcasting.
	// This rough duration is necessary since with neutrino as the wallet's
	// chain service, its chainClient.SendRawTransaction call is blocking for up
	// to neutrino.Config.BroadcastTimeout while peers either respond to the inv
	// request with a getdata or time out. However, in virtually all cases, we
	// just need to know that btcwallet was able to create and store the
	// transaction record, and pass it to the chain service.
	defaultBroadcastWait = 2 * time.Second

	maxFutureBlockTime = 2 * time.Hour // see MaxTimeOffsetSeconds in btcd/blockchain/validate.go
	neutrinoDBName     = "neutrino.db"
	logDirName         = "logs"
	logFileName        = "neutrino.log"
	defaultAcctNum     = 0
	defaultAcctName    = "default"
)

// btcWallet is satisfied by *btcwallet.Wallet -> *walletExtender.
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
	ChainSynced() bool // currently unused
	SynchronizeRPC(chainClient chain.Interface)
	// walletExtender methods
	walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error)
	syncedTo() waddrmgr.BlockStamp
	signTransaction(*wire.MsgTx) error
	txNotifications() wallet.TransactionNotificationsClient
}

var _ btcWallet = (*walletExtender)(nil)

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

var _ neutrinoService = (*neutrino.ChainService)(nil)

// createSPVWallet creates a new SPV wallet.
func createSPVWallet(privPass []byte, seed []byte, dbDir string, log dex.Logger, net *chaincfg.Params) error {
	netDir := filepath.Join(dbDir, net.Name)

	if err := logNeutrino(netDir); err != nil {
		return fmt.Errorf("error initializing btcwallet+neutrino logging: %v", err)
	}

	logDir := filepath.Join(netDir, logDirName)
	err := os.MkdirAll(logDir, 0744)
	if err != nil {
		return fmt.Errorf("error creating wallet directories: %v", err)
	}

	loader := wallet.NewLoader(net, netDir, true, 60*time.Second, 250)

	pubPass := []byte(wallet.InsecurePubPassphrase)

	_, err = loader.CreateNewWallet(pubPass, privPass, seed, walletBirthday)
	if err != nil {
		return fmt.Errorf("CreateNewWallet error: %w", err)
	}

	bailOnWallet := func() {
		if err := loader.UnloadWallet(); err != nil {
			log.Errorf("Error unloading wallet after createSPVWallet error: %v", err)
		}
	}

	neutrinoDBPath := filepath.Join(netDir, neutrinoDBName)
	db, err := walletdb.Create("bdb", neutrinoDBPath, true, 5*time.Second)
	if err != nil {
		bailOnWallet()
		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
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

var (
	// loggingInited will be set when the log rotator has been initialized.
	loggingInited uint32
)

// logRotator initializes a rotating file logger.
func logRotator(netDir string) (*rotator.Rotator, error) {
	const maxLogRolls = 8
	logDir := filepath.Join(netDir, logDirName)
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
// In theory, the the rotating file logger must be Close'd at some point, but
// there are concurrency issues with that since btcd and btcwallet have
// unsupervised goroutines still running after shutdown. So we leave the rotator
// running at the risk of losing some logs.
func logNeutrino(netDir string) error {
	if !atomic.CompareAndSwapUint32(&loggingInited, 0, 1) {
		return nil
	}

	logSpinner, err := logRotator(netDir)
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

// logWriter implements an io.Writer that outputs to a rotating log file.
type logWriter struct {
	*rotator.Rotator
}

// Write writes the data in p to the log file.
func (w logWriter) Write(p []byte) (n int, err error) {
	return w.Rotator.Write(p)
}

// spvWallet is an in-process btcwallet.Wallet + neutrino light-filter-based
// Bitcoin wallet. spvWallet controls an instance of btcwallet.Wallet directly
// and does not run or connect to the RPC server.
type spvWallet struct {
	chainParams  *chaincfg.Params
	wallet       btcWallet
	cl           neutrinoService
	chainClient  *chain.NeutrinoClient
	birthday     time.Time
	acctNum      uint32
	acctName     string
	netDir       string
	neutrinoDB   walletdb.DB
	connectPeers []string

	txBlocksMtx sync.Mutex
	txBlocks    map[chainhash.Hash]*hashEntry

	checkpointMtx sync.Mutex
	checkpoints   map[outPoint]*scanCheckpoint

	log    dex.Logger
	loader *wallet.Loader

	tipChan            chan *block
	syncTarget         int32
	lastPrenatalHeight int32

	// rescanStarting is set while reloading the wallet and dropping
	// transactions from the wallet db.
	rescanStarting uint32 // atomic
}

var _ Wallet = (*spvWallet)(nil)
var _ tipNotifier = (*spvWallet)(nil)

// loadSPVWallet loads an existing wallet.
func loadSPVWallet(dbDir string, logger dex.Logger, connectPeers []string, chainParams *chaincfg.Params) *spvWallet {
	return &spvWallet{
		chainParams:  chainParams,
		acctNum:      defaultAcctNum,
		acctName:     defaultAcctName,
		netDir:       filepath.Join(dbDir, chainParams.Name),
		txBlocks:     make(map[chainhash.Hash]*hashEntry),
		checkpoints:  make(map[outPoint]*scanCheckpoint),
		log:          logger,
		connectPeers: connectPeers,
		tipChan:      make(chan *block, 8),
	}
}

// tipFeed satisfies the tipNotifier interface, signaling that *spvWallet
// will take precedence in sending block notifications.
func (w *spvWallet) tipFeed() <-chan *block {
	return w.tipChan
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
	return entry.hash, true
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

// unvalidatedCheckpoint returns any cached *filterScanResult for the outpoint.
func (w *spvWallet) unvalidatedCheckpoint(txHash *chainhash.Hash, vout uint32) *filterScanResult {
	w.checkpointMtx.Lock()
	defer w.checkpointMtx.Unlock()
	check, found := w.checkpoints[newOutPoint(txHash, vout)]
	if !found {
		return nil
	}
	check.lastAccess = time.Now()
	res := *check.res
	return &res
}

// checkpoint returns a filterScanResult and the checkpoint block hash. If a
// result is found with an orphaned checkpoint block hash, it is cleared from
// the cache and not returned.
func (w *spvWallet) checkpoint(txHash *chainhash.Hash, vout uint32) *filterScanResult {
	res := w.unvalidatedCheckpoint(txHash, vout)
	if res == nil {
		return nil
	}
	if !w.blockIsMainchain(&res.checkpoint, -1) {
		// reorg detected, abandon the checkpoint.
		w.log.Debugf("abandoning checkpoint %s because checkpoint block %q is orphaned",
			newOutPoint(txHash, vout), res.checkpoint)
		w.checkpointMtx.Lock()
		delete(w.checkpoints, newOutPoint(txHash, vout))
		w.checkpointMtx.Unlock()
		return nil
	}
	return res
}

func (w *spvWallet) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	// Not needed for spv wallet.
	return nil, errors.New("RawRequest not available on spv")
}

func (w *spvWallet) estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	return nil, errors.New("EstimateSmartFee not available on spv")
}

func (w *spvWallet) ownsAddress(addr btcutil.Address) (bool, error) {
	return w.wallet.HaveAddress(addr)
}

func (w *spvWallet) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	// Publish the transaction in a goroutine so the caller may wait for a given
	// period before it goes asynchronous and it is assumed that btcwallet at
	// least succeeded with its DB updates and queueing of the transaction for
	// rebroadcasting. In the future, a new btcwallet method should be added
	// that returns after performing its internal actions, but broadcasting
	// asynchronously and sending the outcome in a channel or promise.
	res := make(chan error, 1)
	go func() {
		tStart := time.Now()
		defer close(res)
		if err := w.wallet.PublishTransaction(tx, ""); err != nil {
			w.log.Errorf("PublishTransaction(%v) failure: %v", tx.TxHash(), err)
			res <- err
			return
		}
		defer w.log.Tracef("PublishTransaction(%v) completed in %v", tx.TxHash(),
			time.Since(tStart)) // after outpoint unlocking and signalling

		// bitcoind would unlock these, but it seems that btcwallet does not.
		// However, it seems like they are no longer returned from ListUnspent
		// even if we unlock the outpoint before the transaction is mined, so
		// this is just housekeeping for btcwallet's lockedOutpoints map.
		for _, txIn := range tx.TxIn {
			w.wallet.UnlockOutpoint(txIn.PreviousOutPoint)
		}
		res <- nil
	}()

	select {
	case err := <-res:
		if err != nil {
			return nil, err
		}
	case <-time.After(defaultBroadcastWait):
		w.log.Debugf("No error from PublishTransaction after %v for txn %v. "+
			"Assuming wallet accepted it.", defaultBroadcastWait, tx.TxHash())
	}

	txHash := tx.TxHash() // down here in case... the msgTx was mutated?
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
	blk := w.wallet.syncedTo()
	return &blk.Hash, nil
}

// getBestBlockHeight returns the height of the best block processed by the
// wallet, which indicates the height at which the compact filters have been
// retrieved and scanned for wallet addresses. This is may be less than
// getChainHeight, which indicates the height that the chain service has reached
// in its retrieval of block headers and compact filter headers.
func (w *spvWallet) getBestBlockHeight() (int32, error) {
	return w.wallet.syncedTo().Height, nil
}

// getChainHeight is only for confirmations since it does not reflect the wallet
// manager's sync height, just the chain service.
func (w *spvWallet) getChainHeight() (int32, error) {
	blk, err := w.cl.BestBlock()
	if err != nil {
		return -1, err
	}
	return blk.Height, err
}

func (w *spvWallet) peerCount() (uint32, error) {
	return uint32(len(w.cl.Peers())), nil
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
//
// The neutrino wallet has a two stage sync:
//  1. chain service fetching block headers and filter headers
//  2. wallet address manager retrieving and scanning filters
//
// We only report a single sync height, so we are going to show some progress in
// the chain service sync stage that comes before the wallet has performed any
// address recovery/rescan, and switch to the wallet's sync height when it
// reports non-zero height.
func (w *spvWallet) syncStatus() (*syncStatus, error) {
	// Chain service headers (block and filter) height.
	chainBlk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}
	target := w.syncHeight()
	currentHeight := chainBlk.Height

	var synced bool
	var blk *block
	// Wallet address manager sync height.
	if chainBlk.Timestamp.After(w.birthday) {
		// After the wallet's birthday, the wallet address manager should begin
		// syncing. Although block time stamps are not necessarily monotonically
		// increasing, this is a reasonable condition at which the wallet's sync
		// height should be consulted instead of the chain service's height.
		walletBlock := w.wallet.syncedTo()
		if walletBlock.Height == 0 {
			// The wallet is about to start its sync, so just return the last
			// chain service height prior to wallet birthday until it begins.
			return &syncStatus{
				Target:  target,
				Height:  atomic.LoadInt32(&w.lastPrenatalHeight),
				Syncing: true,
			}, nil
		}
		blk = &block{
			height: int64(walletBlock.Height),
			hash:   walletBlock.Hash,
		}
		currentHeight = walletBlock.Height
		synced = currentHeight >= target // maybe && w.wallet.ChainSynced()
	} else {
		// Chain service still syncing.
		blk = &block{
			height: int64(currentHeight),
			hash:   chainBlk.Hash,
		}
		atomic.StoreInt32(&w.lastPrenatalHeight, currentHeight)
	}

	if target > 0 && atomic.SwapInt32(&w.syncTarget, target) == 0 {
		w.tipChan <- blk
	}

	return &syncStatus{
		Target:  target,
		Height:  int32(blk.height),
		Syncing: !synced,
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
	unspents, err := w.wallet.ListUnspent(0, math.MaxInt32, w.acctName)
	if err != nil {
		return nil, err
	}
	res := make([]*ListUnspentResult, 0, len(unspents))
	for _, utxo := range unspents {
		// If the utxo is unconfirmed, we should determine whether it's "safe"
		// by seeing if we control the inputs of its transaction.
		var safe bool
		if utxo.Confirmations > 0 {
			safe = true
		} else {
			txHash, err := chainhash.NewHashFromStr(utxo.TxID)
			if err != nil {
				return nil, fmt.Errorf("error decoding txid %q: %v", utxo.TxID, err)
			}
			txDetails, err := w.wallet.walletTransaction(txHash)
			if err != nil {
				return nil, fmt.Errorf("walletTransaction error: %v", err)
			}
			// To be "safe", we need to show that we own the inputs for the
			// utxo's transaction. We'll just try to find one.
			safe = true
			// TODO: Keep a cache of our redemption outputs and allow those as
			// safe inputs.
			for _, txIn := range txDetails.MsgTx.TxIn {
				_, _, _, _, err := w.wallet.FetchInputInfo(&txIn.PreviousOutPoint)
				if err != nil {
					if !errors.Is(err, wallet.ErrNotMine) {
						w.log.Warnf("FetchInputInfo error: %v", err)
					}
					safe = false
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
	return nil, errors.New("unimplemented")
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
func (w *spvWallet) Unlock(pw []byte) error {
	return w.wallet.Unlock(pw, nil)
}

// Lock locks the wallet.
func (w *spvWallet) Lock() error {
	w.wallet.Lock()
	return nil
}

// sendToAddress sends the amount to the address. feeRate is in units of
// sats/byte.
func (w *spvWallet) sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	addr, err := btcutil.DecodeAddress(address, w.chainParams)
	if err != nil {
		return nil, err
	}

	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, err
	}

	if subtract {
		return w.sendWithSubtract(pkScript, value, feeRate)
	}

	wireOP := wire.NewTxOut(int64(value), pkScript)
	// converting sats/vB -> sats/kvB
	feeRateAmt := btcutil.Amount(feeRate * 1e3)
	tx, err := w.wallet.SendOutputs([]*wire.TxOut{wireOP}, nil, w.acctNum, 0, feeRateAmt, "")
	if err != nil {
		return nil, err
	}

	txHash := tx.TxHash()

	return &txHash, nil
}

func (w *spvWallet) sendWithSubtract(pkScript []byte, value, feeRate uint64) (*chainhash.Hash, error) {
	txOutSize := dexbtc.TxOutOverhead + uint64(len(pkScript)) // send-to address
	var unfundedTxSize uint64 = dexbtc.MinimumTxOverhead + dexbtc.P2WPKHOutputSize /* change */ + txOutSize

	unspents, err := w.listUnspent()
	if err != nil {
		return nil, fmt.Errorf("error listing unspent outputs: %w", err)
	}

	utxos, _, _, err := convertUnspent(0, unspents, w.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error converting unspent outputs: %w", err)
	}

	// With sendWithSubtract, fees are subtracted from the sent amount, so we
	// target an input sum, not an output value. Makes the math easy.
	enough := func(_, inputsVal uint64) bool {
		return inputsVal >= value
	}

	sum, inputsSize, _, fundingCoins, _, _, err := fund(utxos, enough)
	if err != nil {
		return nil, fmt.Errorf("error funding sendWithSubtract value of %s: %v", amount(value), err)
	}

	fees := (unfundedTxSize + uint64(inputsSize)) * feeRate
	send := value - fees
	extra := sum - send

	switch {
	case fees > sum:
		return nil, fmt.Errorf("fees > sum")
	case fees > value:
		return nil, fmt.Errorf("fees > value")
	case send > sum:
		return nil, fmt.Errorf("send > sum")
	}

	tx := wire.NewMsgTx(wire.TxVersion)
	for op := range fundingCoins {
		wireOP := wire.NewOutPoint(&op.txHash, op.vout)
		txIn := wire.NewTxIn(wireOP, []byte{}, nil)
		tx.AddTxIn(txIn)
	}

	change := extra - fees
	changeAddr, err := w.changeAddress()
	if err != nil {
		return nil, fmt.Errorf("error retrieving change address: %w", err)
	}

	changeScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return nil, fmt.Errorf("error generating pubkey script: %w", err)
	}

	changeOut := wire.NewTxOut(int64(change), changeScript)

	// One last check for dust.
	if dexbtc.IsDust(changeOut, feeRate) {
		// Re-calculate fees and change
		fees = (unfundedTxSize - dexbtc.P2WPKHOutputSize + uint64(inputsSize)) * feeRate
		send = sum - fees
	} else {
		tx.AddTxOut(changeOut)
	}

	wireOP := wire.NewTxOut(int64(send), pkScript)
	tx.AddTxOut(wireOP)

	if err := w.wallet.signTransaction(tx); err != nil {
		return nil, fmt.Errorf("signing error: %w", err)
	}

	return w.sendRawTransaction(tx)
}

// swapConfirmations attempts to get the number of confirmations and the spend
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
		w.log.Tracef("swapConfirmations - WalletTransactionNotFound: %v:%d", txHash, vout)
	case SpentStatusUnknown:
		w.log.Tracef("swapConfirmations - SpentStatusUnknown: %v:%d (block %v, confs %d)",
			txHash, vout, blockHash, confs)
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
	w.log.Tracef("swapConfirmations - scanFilters: %v:%d (block %v, start time %v)",
		txHash, vout, blockHash, startTime)
	utxo, err := w.scanFilters(txHash, vout, pkScript, startTime, blockHash)
	if err != nil {
		return 0, false, err
	}

	if utxo.spend == nil && utxo.blockHash == nil {
		if assumedMempool {
			w.log.Tracef("swapConfirmations - scanFilters did not find %v:%d, assuming in mempool.",
				txHash, vout)
			// NOT asset.CoinNotFoundError since this is normal for mempool
			// transactions with an SPV wallet.
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("output %s:%v not found with search parameters startTime = %s, pkScript = %x",
			txHash, vout, startTime, pkScript)
	}

	if utxo.blockHash != nil {
		bestHeight, err := w.getChainHeight()
		if err != nil {
			return 0, false, fmt.Errorf("getBestBlockHeight error: %v", err)
		}
		confs = uint32(bestHeight) - utxo.blockHeight + 1
	}

	if utxo.spend != nil {
		// In the off-chance that a spend was found but not the output itself,
		// confs will be incorrect here.
		// In situations where we're looking for the counter-party's swap, we
		// revoke if it's found to be spent, without inspecting the confs, so
		// accuracy of confs is not significant. When it's our output, we'll
		// know the block and won't end up here. (even if we did, we just end up
		// sending out some inaccurate Data-severity notifications to the UI
		// until the match progresses)
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

func (w *spvWallet) walletUnlock(pw []byte) error {
	return w.Unlock(pw)
}

func (w *spvWallet) getBlockHeader(blockHash *chainhash.Hash) (*blockHeader, error) {
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

	return &blockHeader{
		Hash:          hdr.BlockHash().String(),
		Confirmations: int64(confirms(blockHeight, tip.Height)),
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

func (w *spvWallet) logFilePath() string {
	return filepath.Join(w.netDir, logDirName, logFileName)
}

// connect will start the wallet and begin syncing.
func (w *spvWallet) connect(ctx context.Context, wg *sync.WaitGroup) error {
	if err := logNeutrino(w.netDir); err != nil {
		return fmt.Errorf("error initializing btcwallet+neutrino logging: %v", err)
	}

	err := w.startWallet()
	if err != nil {
		return err
	}

	txNotes := w.wallet.txNotifications()

	// Nanny for the caches checkpoints and txBlocks caches.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer w.stop()
		defer txNotes.Done()

		ticker := time.NewTicker(time.Minute * 20)
		defer ticker.Stop()
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

			case note := <-txNotes.C:
				if len(note.AttachedBlocks) > 0 {
					lastBlock := note.AttachedBlocks[len(note.AttachedBlocks)-1]
					syncTarget := atomic.LoadInt32(&w.syncTarget)

					for ib := range note.AttachedBlocks {
						for _, nt := range note.AttachedBlocks[ib].Transactions {
							w.log.Debugf("Block %d contains wallet transaction %v", note.AttachedBlocks[ib].Height, nt.Hash)
						}
					}

					if syncTarget == 0 || (lastBlock.Height < syncTarget && lastBlock.Height%10_000 != 0) {
						continue
					}

					select {
					case w.tipChan <- &block{
						hash:   *lastBlock.Hash,
						height: int64(lastBlock.Height),
					}:
					default:
						w.log.Warnf("tip report channel was blocking")
					}
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// startWallet initializes the *btcwallet.Wallet and its supporting players and
// starts syncing.
func (w *spvWallet) startWallet() error {
	// timeout and recoverWindow arguments borrowed from btcwallet directly.
	w.loader = wallet.NewLoader(w.chainParams, w.netDir, true, 60*time.Second, 250)

	exists, err := w.loader.WalletExists()
	if err != nil {
		return fmt.Errorf("error verifying wallet existence: %v", err)
	}
	if !exists {
		return errors.New("wallet not found")
	}

	w.log.Debug("Starting native BTC wallet...")
	btcw, err := w.loader.OpenExistingWallet([]byte(wallet.InsecurePubPassphrase), false)
	if err != nil {
		return fmt.Errorf("couldn't load wallet: %w", err)
	}

	bailOnWallet := func() {
		if err := w.loader.UnloadWallet(); err != nil {
			w.log.Errorf("Error unloading wallet: %v", err)
		}
	}

	neutrinoDBPath := filepath.Join(w.netDir, neutrinoDBName)
	w.neutrinoDB, err = walletdb.Create("bdb", neutrinoDBPath, true, wallet.DefaultDBTimeout)
	if err != nil {
		bailOnWallet()
		return fmt.Errorf("unable to create wallet db at %q: %v", neutrinoDBPath, err)
	}

	bailOnWalletAndDB := func() {
		if err := w.neutrinoDB.Close(); err != nil {
			w.log.Errorf("Error closing neutrino database: %v", err)
		}
		bailOnWallet()
	}

	// Depending on the network, we add some addpeers or a connect peer. On
	// regtest, if the peers haven't been explicitly set, add the simnet harness
	// alpha node as an additional peer so we don't have to type it in. On
	// mainet and testnet3, add a known reliable persistent peer to be used in
	// addition to normal DNS seed-based peer discovery.
	var addPeers []string
	switch w.chainParams.Net {
	case wire.MainNet:
		addPeers = []string{"cfilters.ssgen.io"}
	case wire.TestNet3:
		addPeers = []string{"dex-test.ssgen.io"}
	case wire.TestNet, wire.SimNet: // plain "wire.TestNet" is regnet!
		if len(w.connectPeers) == 0 {
			w.connectPeers = []string{"localhost:20575"}
		}
	}
	w.log.Debug("Starting neutrino chain service...")
	chainService, err := neutrino.NewChainService(neutrino.Config{
		DataDir:       w.netDir,
		Database:      w.neutrinoDB,
		ChainParams:   *w.chainParams,
		PersistToDisk: true, // keep cfilter headers on disk for efficient rescanning
		AddPeers:      addPeers,
		ConnectPeers:  w.connectPeers,
		// WARNING: PublishTransaction currently uses the entire duration
		// because if an external bug, but even if the resolved, a typical
		// inv/getdata round trip is ~4 seconds, so we set this so neutrino does
		// not cancel queries too readily.
		BroadcastTimeout: 6 * time.Second,
	})
	if err != nil {
		bailOnWalletAndDB()
		return fmt.Errorf("couldn't create Neutrino ChainService: %v", err)
	}

	bailOnEverything := func() {
		if err := chainService.Stop(); err != nil {
			w.log.Errorf("Error closing neutrino chain service: %v", err)
		}
		bailOnWalletAndDB()
	}

	w.cl = chainService
	w.chainClient = chain.NewNeutrinoClient(w.chainParams, chainService)
	w.wallet = &walletExtender{btcw, w.chainParams}
	w.birthday = btcw.Manager.Birthday()

	if err = w.chainClient.Start(); err != nil { // lazily starts connmgr
		bailOnEverything()
		return fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.log.Info("Synchronizing wallet with network...")
	btcw.SynchronizeRPC(w.chainClient)

	return nil
}

// rescanWalletAsync initiates a full wallet recovery (used address discovery
// and transaction scanning) by stopping the btcwallet, dropping the transaction
// history from the wallet db, resetting the synced-to height of the wallet
// manager, restarting the wallet and its chain client, and finally commanding
// the wallet to resynchronize, which starts asynchronous wallet recovery.
// Progress of the rescan should be monitored with syncStatus. During the rescan
// wallet balances and known transactions may not be reported accurately or
// located. The neutrinoService is not stopped, so most spvWallet methods will
// continue to work without error, but methods using the btcWallet will likely
// return incorrect results or errors.
func (w *spvWallet) rescanWalletAsync() error {
	if !atomic.CompareAndSwapUint32(&w.rescanStarting, 0, 1) {
		return errors.New("rescan already in progress")
	}
	defer atomic.StoreUint32(&w.rescanStarting, 0)

	// Stop the wallet, but do not use w.loader.UnloadWallet because it also
	// closes the database.
	btcw, ok := w.loader.LoadedWallet()
	if !ok {
		return errors.New("wallet not loaded")
	}
	wdb := btcw.Database()

	w.log.Info("Stopping wallet and chain client...")
	btcw.Stop() // stops Wallet and chainClient (not chainService)
	btcw.WaitForShutdown()
	w.chainClient.WaitForShutdown()

	// Force a full rescan with active address discovery on wallet restart by
	// dropping the complete transaction history. See the
	// btcwallet/cmd/dropwtxmgr app for more information.
	w.log.Info("Dropping transaction history to perform full rescan...")
	err := wallet.DropTransactionHistory(wdb, false)
	if err != nil {
		w.log.Errorf("Failed to drop wallet transaction history: %v", err)
		// Continue to attempt restarting the wallet anyway.
	}

	err = walletdb.Update(wdb, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket([]byte("waddrmgr")) // it'll be fine
		return btcw.Manager.SetSyncedTo(ns, nil)       // never synced, forcing recover from birthday
	})
	if err != nil {
		w.log.Errorf("Failed to reset wallet manager sync height: %v", err)
	}

	w.log.Info("Starting wallet...")
	btcw.Start()

	if err = w.chainClient.Start(); err != nil {
		return fmt.Errorf("couldn't start Neutrino client: %v", err)
	}

	w.log.Info("Synchronizing wallet with network...")
	btcw.SynchronizeRPC(w.chainClient)
	return nil
}

// stop stops the wallet and database threads.
func (w *spvWallet) stop() {
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
		w.log.Errorf("Error retrieving block hash for height %d", blockHeight)
		return false
	}

	return *checkHash == *blockHash
}

// mainchainBlockForStoredTx gets the block hash and height for the transaction
// IFF an entry has been stored in the txBlocks index.
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
// returned block has a very low likelihood of missing any blocks that have time
// > matchTime. This is done by performing a binary search (sort.Search) to find
// a block with a block time maxFutureBlockTime before matchTime. To ensure
// we also accommodate the median-block time rule and aren't missing anything
// due to out of sequence block times we use an unsophisticated algorithm of
// choosing the first block in an 11 block window with no times >= matchTime.
func (w *spvWallet) findBlockForTime(matchTime time.Time) (*chainhash.Hash, int32, error) {
	offsetTime := matchTime.Add(-maxFutureBlockTime)

	bestHeight, err := w.getChainHeight()
	if err != nil {
		return nil, 0, fmt.Errorf("getChainHeight error: %v", err)
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
	// times aren't always monotonically increasing. This won't matter though as
	// long as there are not > medianTimeBlocks blocks with inverted time order.
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
	// TODO: Check that any blockHash supplied is not orphaned?

	// Check if we know the block hash for the tx.
	var limitHeight int32
	// See if we have a checkpoint to use.
	checkPt := w.checkpoint(txHash, vout)
	if checkPt != nil {
		if checkPt.blockHash != nil && checkPt.spend != nil {
			// We already have the output and the spending input, and
			// checkpointBlock already verified it's still mainchain.
			return checkPt, nil
		}
		height, err := w.getBlockHeight(&checkPt.checkpoint)
		if err != nil {
			return nil, fmt.Errorf("getBlockHeight error: %w", err)
		}
		limitHeight = height + 1
	} else if blockHash == nil {
		// No checkpoint and no block hash. Gotta guess based on time.
		blockHash, limitHeight = w.mainchainBlockForStoredTx(txHash)
		if blockHash == nil {
			var err error
			_, limitHeight, err = w.findBlockForTime(startTime)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// No checkpoint, but user supplied a block hash.
		var err error
		limitHeight, err = w.getBlockHeight(blockHash)
		if err != nil {
			return nil, fmt.Errorf("error getting height for supplied block hash %s", blockHash)
		}
	}

	w.log.Debugf("Performing cfilters scan for %v:%d from height %d", txHash, vout, limitHeight)

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
		w.log.Debugf("cfilters scan SUCCEEDED for %v:%d. block hash: %v, spent: %v",
			txHash, vout, utxo.blockHash, utxo.spend != nil)
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
	var blockHash *chainhash.Hash
	if err != nil && !errors.Is(err, WalletTransactionNotFound) {
		return nil, 0, fmt.Errorf("walletTransaction error: %w", err)
	}

	if txDetails != nil {
		spent, found := outputSpendStatus(txDetails, vout)
		if found {
			if spent {
				return nil, 0, nil
			}
			if len(txDetails.MsgTx.TxOut) <= int(vout) {
				return nil, 0, fmt.Errorf("wallet transaction %s doesn't have enough outputs for vout %d", txHash, vout)
			}

			var confs uint32
			if txDetails.Block.Height > 0 {
				tip, err := w.cl.BestBlock()
				if err != nil {
					return nil, 0, fmt.Errorf("BestBlock error: %v", err)
				}
				confs = uint32(confirms(txDetails.Block.Height, tip.Height))
			}

			msgTx := &txDetails.MsgTx
			if len(msgTx.TxOut) <= int(vout) {
				return nil, 0, fmt.Errorf("wallet transaction %s found, but not enough outputs for vout %d", txHash, vout)
			}
			return msgTx.TxOut[vout], confs, nil

		}
		if txDetails.Block.Hash != (chainhash.Hash{}) {
			blockHash = &txDetails.Block.Hash
		}
	}

	// We don't really know if it's spent, so we'll need to scan.
	utxo, err := w.scanFilters(txHash, vout, pkScript, startTime, blockHash)
	if err != nil {
		return nil, 0, err
	}

	if utxo == nil || utxo.spend != nil || utxo.blockHash == nil {
		return nil, 0, nil
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, 0, fmt.Errorf("BestBlock error: %v", err)
	}

	confs := uint32(confirms(int32(utxo.blockHeight), tip.Height))

	return utxo.txOut, confs, nil
}

// filterScanFromHeight scans BIP158 filters beginning at the specified block
// height until the tip, or until a spending transaction is found.
func (w *spvWallet) filterScanFromHeight(txHash chainhash.Hash, vout uint32, pkScript []byte, startBlockHeight int32, checkPt *filterScanResult) (*filterScanResult, error) {
	walletBlock := w.wallet.syncedTo() // where cfilters are received and processed
	tip := walletBlock.Height

	res := checkPt
	if res == nil {
		res = new(filterScanResult)
	}

search:
	for height := startBlockHeight; height <= tip; height++ {
		if res.spend != nil && res.blockHash == nil {
			w.log.Warnf("A spending input (%s) was found during the scan but the output (%s) "+
				"itself wasn't found. Was the startBlockHeight early enough?",
				newOutPoint(&res.spend.txHash, res.spend.vin),
				newOutPoint(&txHash, vout),
			)
			return res, nil
		}
		blockHash, err := w.getBlockHash(int64(height))
		if err != nil {
			return nil, fmt.Errorf("error getting block hash for height %d: %w", height, err)
		}
		matched, err := w.matchPkScript(blockHash, [][]byte{pkScript})
		if err != nil {
			return nil, fmt.Errorf("matchPkScript error: %w", err)
		}

		res.checkpoint = *blockHash
		if !matched {
			continue search
		}
		// Pull the block.
		w.log.Tracef("Block %v matched pkScript for output %v:%d. Pulling the block...",
			blockHash, txHash, vout)
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
							blockHash:   *blockHash,
							blockHeight: uint32(height),
						}
						w.log.Tracef("Found txn %v spending %v in block %v (%d)", res.spend.txHash,
							txHash, res.spend.blockHash, res.spend.blockHeight)
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
			if res.blockHash != nil || tx.TxHash() != txHash {
				continue nextTx
			}
			for _, txOut := range tx.TxOut {
				if bytes.Equal(txOut.PkScript, pkScript) {
					res.blockHash = blockHash
					res.blockHeight = uint32(height)
					res.txOut = txOut
					w.log.Tracef("Found txn %v in block %v (%d)", txHash, res.blockHash, height)
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

	if filter.N() == 0 {
		return false, fmt.Errorf("unexpected empty filter for %s", blockHash)
	}

	var filterKey [gcs.KeySize]byte
	copy(filterKey[:], blockHash[:gcs.KeySize])

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
		w.log.Errorf("matchPkScript error: %v", err)
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

	if details.Block.Hash != (chainhash.Hash{}) {
		blockHash = &details.Block.Hash
		height, err := w.getChainHeight()
		if err != nil {
			return nil, 0, false, err
		}
		confs = uint32(confirms(details.Block.Height, height))
	}

	spent, found := outputSpendStatus(details, vout)
	if found {
		return blockHash, confs, spent, nil
	}

	return blockHash, confs, false, SpentStatusUnknown
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

// getWalletBirthdayBlock retrieves the wallet's birthday block.
//
// NOTE: The wallet birthday block hash is NOT SET until the chain service
// passes the birthday block and the wallet looks it up based on the birthday
// Time and the downloaded block headers.
// This is presently unused, but I have plans for it with a wallet rescan.
// func (w *walletExtender) getWalletBirthdayBlock() (*waddrmgr.BlockStamp, error) {
// 	var birthdayBlock waddrmgr.BlockStamp
// 	err := walletdb.View(w.Database(), func(dbtx walletdb.ReadTx) error {
// 		ns := dbtx.ReadBucket([]byte("waddrmgr")) // it'll be fine
// 		var err error
// 		birthdayBlock, _, err = w.Manager.BirthdayBlock(ns)
// 		return err
// 	})
// 	if err != nil {
// 		return nil, err // sadly, waddrmgr.ErrBirthdayBlockNotSet is expected during most of chain sync
// 	}
// 	return &birthdayBlock, nil
// }

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

// txNotifications gives access to the NotificationServer's tx notifications.
func (w *walletExtender) txNotifications() wallet.TransactionNotificationsClient {
	return w.NtfnServer.TransactionNotifications()
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

// outputSpendStatus will return the spend status of the output if it's found
// in the TxDetails.Credits.
func outputSpendStatus(details *wtxmgr.TxDetails, vout uint32) (spend, found bool) {
	for _, credit := range details.Credits {
		if credit.Index == vout {
			return credit.Spent, true
		}
	}
	return false, false
}
