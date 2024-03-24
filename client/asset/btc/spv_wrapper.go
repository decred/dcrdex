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
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/gcs"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb" // bdb init() registers a driver
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

const (
	WalletTransactionNotFound = dex.ErrorKind("wallet transaction not found")
	SpentStatusUnknown        = dex.ErrorKind("spend status not known")
	// NOTE: possibly unexport the two above error kinds.

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

var wAddrMgrBkt = []byte("waddrmgr")

// BTCWallet is roughly the (btcwallet/wallet.*Wallet) interface, with some
// additional required methods added.
type BTCWallet interface {
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
	PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error)
	Unlock(passphrase []byte, lock <-chan time.Time) error
	Lock()
	Locked() bool
	SendOutputs(outputs []*wire.TxOut, keyScope *waddrmgr.KeyScope, account uint32, minconf int32,
		satPerKb btcutil.Amount, coinSelectionStrategy wallet.CoinSelectionStrategy, label string) (*wire.MsgTx, error)
	HaveAddress(a btcutil.Address) (bool, error)
	WaitForShutdown()
	ChainSynced() bool // currently unused
	AccountProperties(scope waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error)
	// AccountInfo returns the account information of the wallet for use by the
	// exchange wallet.
	AccountInfo() XCWalletAccount
	// The below methods are not implemented by *wallet.Wallet, so must be
	// implemented by the BTCWallet implementation.
	WalletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error)
	SyncedTo() waddrmgr.BlockStamp
	SignTx(*wire.MsgTx) error
	BlockNotifications(context.Context) <-chan *BlockNotification
	RescanAsync() error
	ForceRescan()
	Start() (SPVService, error)
	Stop()
	Reconfigure(*asset.WalletConfig, string) (bool, error)
	Birthday() time.Time
	Peers() ([]*asset.WalletPeer, error)
	AddPeer(string) error
	RemovePeer(string) error
	ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error)
}

type XCWalletAccount struct {
	AccountName   string
	AccountNumber uint32
}

// BlockNotification is block hash and height delivered by a BTCWallet when it
// is finished processing a block.
type BlockNotification struct {
	Hash   chainhash.Hash
	Height int32
}

// SPVService is satisfied by *neutrino.ChainService, with the exception of the
// Peers method, which has a generic interface in place of neutrino.ServerPeer.
type SPVService interface {
	GetBlockHash(int64) (*chainhash.Hash, error)
	BestBlock() (*headerfs.BlockStamp, error)
	Peers() []SPVPeer
	AddPeer(addr string) error
	GetBlockHeight(hash *chainhash.Hash) (int32, error)
	GetBlockHeader(*chainhash.Hash) (*wire.BlockHeader, error)
	GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error)
	GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error)
	Stop() error
}

// SPVPeer is satisfied by *neutrino.ServerPeer, but is generalized to
// accommodate underlying implementations other than lightninglabs/neutrino.
type SPVPeer interface {
	StartingHeight() int32
	LastBlock() int32
	Addr() string
}

// btcChainService wraps *neutrino.ChainService in order to translate the
// neutrino.ServerPeer to the SPVPeer interface type.
type btcChainService struct {
	*neutrino.ChainService
}

func (s *btcChainService) Peers() []SPVPeer {
	rawPeers := s.ChainService.Peers()
	peers := make([]SPVPeer, 0, len(rawPeers))
	for _, p := range rawPeers {
		peers = append(peers, p)
	}
	return peers
}

func (s *btcChainService) AddPeer(addr string) error {
	return s.ChainService.ConnectNode(addr, true)
}

func (s *btcChainService) RemovePeer(addr string) error {
	return s.ChainService.RemoveNodeByAddr(addr)
}

var _ SPVService = (*btcChainService)(nil)

// BTCWalletConstructor is a function to construct a BTCWallet.
type BTCWalletConstructor func(dir string, cfg *WalletConfig, chainParams *chaincfg.Params, log dex.Logger) BTCWallet

func extendAddresses(extIdx, intIdx uint32, btcw *wallet.Wallet) error {
	scopedKeyManager, err := btcw.Manager.FetchScopedKeyManager(waddrmgr.KeyScopeBIP0084)
	if err != nil {
		return err
	}

	return walletdb.Update(btcw.Database(), func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadWriteBucket(wAddrMgrBkt)
		if extIdx > 0 {
			if err := scopedKeyManager.ExtendExternalAddresses(ns, defaultAcctNum, extIdx); err != nil {
				return err
			}
		}
		if intIdx > 0 {
			return scopedKeyManager.ExtendInternalAddresses(ns, defaultAcctNum, intIdx)
		}
		return nil
	})
}

// spvWallet is an in-process btcwallet.Wallet + neutrino light-filter-based
// Bitcoin wallet. spvWallet controls an instance of btcwallet.Wallet directly
// and does not run or connect to the RPC server.
type spvWallet struct {
	chainParams *chaincfg.Params
	cfg         *WalletConfig
	wallet      BTCWallet
	cl          SPVService
	acctNum     uint32
	dir         string
	decodeAddr  dexbtc.AddressDecoder

	log dex.Logger

	tipChan            chan *BlockVector
	syncTarget         int32
	lastPrenatalHeight int32

	*BlockFiltersScanner
}

var _ Wallet = (*spvWallet)(nil)
var _ tipNotifier = (*spvWallet)(nil)

// reconfigure attempts to reconfigure the rpcClient for the new settings. Live
// reconfiguration is only attempted if the new wallet type is walletTypeSPV. An
// error is generated if the birthday is reduced and the special_activelyUsed
// flag is set.
func (w *spvWallet) reconfigure(cfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error) {
	// If the wallet type is not SPV, then we can't reconfigure the wallet.
	if cfg.Type != walletTypeSPV {
		restartRequired = true
		return
	}

	// Check if the SPV wallet exists. If it doesn't, then we can't reconfigure it.
	exists, err := walletExists(w.dir, w.chainParams)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, errors.New("wallet not found")
	}

	return w.wallet.Reconfigure(cfg, currentAddress)
}

// tipFeed satisfies the tipNotifier interface, signaling that *spvWallet
// will take precedence in sending block notifications.
func (w *spvWallet) tipFeed() <-chan *BlockVector {
	return w.tipChan
}

func (w *spvWallet) RawRequest(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	// Not needed for spv wallet.
	return nil, errors.New("RawRequest not available on spv")
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
		defer func() {
			w.log.Tracef("PublishTransaction(%v) completed in %v", tx.TxHash(), time.Since(tStart))
		}() // after outpoint unlocking and signalling
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

	// bitcoind would unlock these, btcwallet does not. Although it seems like
	// they are no longer returned from ListUnspent after publishing, it must
	// not be returned by LockedOutpoints (listlockunspent) for the lockedSats
	// computations to be correct.
	for _, txIn := range tx.TxIn {
		w.wallet.UnlockOutpoint(txIn.PreviousOutPoint)
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

// getBlockHeight gets the mainchain height for the specified block. Returns
// error for orphaned blocks.
func (w *spvWallet) getBlockHeight(h *chainhash.Hash) (int32, error) {
	return w.cl.GetBlockHeight(h)
}

func (w *spvWallet) getBestBlockHash() (*chainhash.Hash, error) {
	blk := w.wallet.SyncedTo()
	return &blk.Hash, nil
}

// getBestBlockHeight returns the height of the best block processed by the
// wallet, which indicates the height at which the compact filters have been
// retrieved and scanned for wallet addresses. This is may be less than
// getChainHeight, which indicates the height that the chain service has reached
// in its retrieval of block headers and compact filter headers.
func (w *spvWallet) getBestBlockHeight() (int32, error) {
	return w.wallet.SyncedTo().Height, nil
}

// getChainStamp satisfies chainStamper for manual median time calculations.
func (w *spvWallet) getChainStamp(blockHash *chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error) {
	hdr, err := w.cl.GetBlockHeader(blockHash)
	if err != nil {
		return
	}
	return hdr.Timestamp, &hdr.PrevBlock, nil
}

// medianTime is the median time for the current best block.
func (w *spvWallet) medianTime() (time.Time, error) {
	blk := w.wallet.SyncedTo()
	return CalcMedianTime(w.getChainStamp, &blk.Hash)
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

func (w *spvWallet) peers() ([]*asset.WalletPeer, error) {
	return w.wallet.Peers()
}

func (w *spvWallet) addPeer(addr string) error {
	return w.wallet.AddPeer(addr)
}

func (w *spvWallet) removePeer(addr string) error {
	return w.wallet.RemovePeer(addr)
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

// SyncStatus is information about the wallet's sync status.
//
// The neutrino wallet has a two stage sync:
//  1. chain service fetching block headers and filter headers
//  2. wallet address manager retrieving and scanning filters
//
// We only report a single sync height, so we are going to show some progress in
// the chain service sync stage that comes before the wallet has performed any
// address recovery/rescan, and switch to the wallet's sync height when it
// reports non-zero height.
func (w *spvWallet) syncStatus() (*SyncStatus, error) {
	// Chain service headers (block and filter) height.
	chainBlk, err := w.cl.BestBlock()
	if err != nil {
		return nil, err
	}

	currentHeight := chainBlk.Height

	var target int32
	if len(w.cl.Peers()) > 0 {
		target = w.syncHeight()
	} else { // use cached value if available
		target = atomic.LoadInt32(&w.syncTarget)
	}

	var synced bool
	var blk *BlockVector
	// Wallet address manager sync height.
	if chainBlk.Timestamp.After(w.wallet.Birthday()) {
		// After the wallet's birthday, the wallet address manager should begin
		// syncing. Although block time stamps are not necessarily monotonically
		// increasing, this is a reasonable condition at which the wallet's sync
		// height should be consulted instead of the chain service's height.
		walletBlock := w.wallet.SyncedTo()
		if walletBlock.Height == 0 {
			// The wallet is about to start its sync, so just return the last
			// chain service height prior to wallet birthday until it begins.
			return &SyncStatus{
				Target:  target,
				Height:  atomic.LoadInt32(&w.lastPrenatalHeight),
				Syncing: true,
			}, nil
		}
		blk = &BlockVector{
			Height: int64(walletBlock.Height),
			Hash:   walletBlock.Hash,
		}
		currentHeight = walletBlock.Height
		synced = currentHeight >= target // maybe && w.wallet.ChainSynced()
	} else {
		// Chain service still syncing.
		blk = &BlockVector{
			Height: int64(currentHeight),
			Hash:   chainBlk.Hash,
		}
		atomic.StoreInt32(&w.lastPrenatalHeight, currentHeight)
	}

	if target > 0 && atomic.SwapInt32(&w.syncTarget, target) == 0 {
		w.tipChan <- blk
	}

	return &SyncStatus{
		Target:  target,
		Height:  int32(blk.Height),
		Syncing: !synced,
	}, nil
}

// ownsInputs determines if we own the inputs of the tx.
func (w *spvWallet) ownsInputs(txid string) bool {
	txHash, err := chainhash.NewHashFromStr(txid)
	if err != nil {
		w.log.Warnf("Error decoding txid %q: %v", txid, err)
		return false
	}
	txDetails, err := w.wallet.WalletTransaction(txHash)
	if err != nil {
		w.log.Warnf("walletTransaction(%v) error: %v", txid, err)
		return false
	}

	for _, txIn := range txDetails.MsgTx.TxIn {
		_, _, _, _, err = w.wallet.FetchInputInfo(&txIn.PreviousOutPoint)
		if err != nil {
			if !errors.Is(err, wallet.ErrNotMine) {
				w.log.Warnf("FetchInputInfo error: %v", err)
			}
			return false
		}
	}
	return true
}

func (w *spvWallet) listTransactionsSinceBlock(blockHeight int32) ([]btcjson.ListTransactionsResult, error) {
	return w.wallet.ListSinceBlock(-1, blockHeight, 0)
}

// balances retrieves a wallet's balance details.
func (w *spvWallet) balances() (*GetBalancesResult, error) {
	// Determine trusted vs untrusted coins with listunspent.
	unspents, err := w.wallet.ListUnspent(0, math.MaxInt32, w.wallet.AccountInfo().AccountName)
	if err != nil {
		return nil, fmt.Errorf("error listing unspent outputs: %w", err)
	}
	var trusted, untrusted btcutil.Amount
	for _, txout := range unspents {
		if txout.Confirmations > 0 || w.ownsInputs(txout.TxID) {
			trusted += btcutil.Amount(toSatoshi(txout.Amount))
			continue
		}
		untrusted += btcutil.Amount(toSatoshi(txout.Amount))
	}

	// listunspent does not include immature coinbase outputs or locked outputs.
	bals, err := w.wallet.CalculateAccountBalances(w.acctNum, 0 /* confs */)
	if err != nil {
		return nil, err
	}
	w.log.Tracef("Bals: spendable = %v (%v trusted, %v untrusted, %v assumed locked), immature = %v",
		bals.Spendable, trusted, untrusted, bals.Spendable-trusted-untrusted, bals.ImmatureReward)
	// Locked outputs would be in wallet.Balances.Spendable. Assume they would
	// be considered trusted and add them back in.
	if all := trusted + untrusted; bals.Spendable > all {
		trusted += bals.Spendable - all
	}

	return &GetBalancesResult{
		Mine: Balances{
			Trusted:   trusted.ToBTC(),
			Untrusted: untrusted.ToBTC(),
			Immature:  bals.ImmatureReward.ToBTC(),
		},
	}, nil
}

// listUnspent retrieves list of the wallet's UTXOs.
func (w *spvWallet) listUnspent() ([]*ListUnspentResult, error) {
	unspents, err := w.wallet.ListUnspent(0, math.MaxInt32, w.wallet.AccountInfo().AccountName)
	if err != nil {
		return nil, err
	}
	res := make([]*ListUnspentResult, 0, len(unspents))
	for _, utxo := range unspents {
		// If the utxo is unconfirmed, we should determine whether it's "safe"
		// by seeing if we control the inputs of its transaction.
		safe := utxo.Confirmations > 0 || w.ownsInputs(utxo.TxID)

		// These hex decodings are unlikely to fail because they come directly
		// from the listunspent result. Regardless, they should not result in an
		// error for the caller as we can return the valid utxos.
		pkScript, err := hex.DecodeString(utxo.ScriptPubKey)
		if err != nil {
			w.log.Warnf("ScriptPubKey decode failure: %v", err)
			continue
		}

		redeemScript, err := hex.DecodeString(utxo.RedeemScript)
		if err != nil {
			w.log.Warnf("ScriptPubKey decode failure: %v", err)
			continue
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
			SafePtr: &safe,
		})
	}
	return res, nil
}

// lockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (w *spvWallet) lockUnspent(unlock bool, ops []*Output) error {
	switch {
	case unlock && len(ops) == 0:
		w.wallet.ResetLockedOutpoints()
	default:
		for _, op := range ops {
			op := wire.OutPoint{Hash: op.Pt.TxHash, Index: op.Pt.Vout}
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

// externalAddress gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (w *spvWallet) externalAddress() (btcutil.Address, error) {
	return w.wallet.NewAddress(w.acctNum, waddrmgr.KeyScopeBIP0084)
}

// signTx attempts to have the wallet sign the transaction inputs.
func (w *spvWallet) signTx(tx *wire.MsgTx) (*wire.MsgTx, error) {
	// Can't use btcwallet.Wallet.SignTransaction, because it doesn't work for
	// segwit transactions (for real?).
	return tx, w.wallet.SignTx(tx)
}

// privKeyForAddress retrieves the private key associated with the specified
// address.
func (w *spvWallet) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	a, err := w.decodeAddr(addr, w.chainParams)
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

// estimateSendTxFee callers should provide at least one output value.
func (w *spvWallet) estimateSendTxFee(tx *wire.MsgTx, feeRate uint64, subtract bool) (fee uint64, err error) {
	minTxSize := uint64(tx.SerializeSize())
	var sendAmount uint64
	for _, txOut := range tx.TxOut {
		sendAmount += uint64(txOut.Value)
	}

	unspents, err := w.listUnspent()
	if err != nil {
		return 0, fmt.Errorf("error listing unspent outputs: %w", err)
	}

	utxos, _, _, err := convertUnspent(0, unspents, w.chainParams)
	if err != nil {
		return 0, fmt.Errorf("error converting unspent outputs: %w", err)
	}

	enough := sendEnough(sendAmount, feeRate, subtract, minTxSize, true, false)
	sum, _, inputsSize, _, _, _, _, err := TryFund(utxos, enough)
	if err != nil {
		return 0, err
	}

	txSize := minTxSize + inputsSize
	estFee := feeRate * txSize
	remaining := sum - sendAmount

	// Check if there will be a change output if there is enough remaining.
	estFeeWithChange := (txSize + dexbtc.P2WPKHOutputSize) * feeRate
	var changeValue uint64
	if remaining > estFeeWithChange {
		changeValue = remaining - estFeeWithChange
	}

	if subtract {
		// fees are already included in sendAmount, anything else is change.
		changeValue = remaining
	}

	var finalFee uint64
	if dexbtc.IsDustVal(dexbtc.P2WPKHOutputSize, changeValue, feeRate, true) {
		// remaining cannot cover a non-dust change and the fee for the change.
		finalFee = estFee + remaining
	} else {
		// additional fee will be paid for non-dust change
		finalFee = estFeeWithChange
	}

	if subtract {
		sendAmount -= finalFee
	}
	if dexbtc.IsDustVal(minTxSize, sendAmount, feeRate, true) {
		return 0, errors.New("output value is dust")
	}

	return finalFee, nil
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
	walletBlock := w.wallet.SyncedTo() // where cfilters are received and processed
	walletTip := walletBlock.Height
	utxo, err := w.ScanFilters(txHash, vout, pkScript, walletTip, startTime, blockHash)
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

func (w *spvWallet) getBlockHeaderVerbose(blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	return w.cl.GetBlockHeader(blockHash)
}

// getBlockHeader gets the *blockHeader for the specified block hash. It also
// returns a bool value to indicate whether this block is a part of main chain.
// For orphaned blocks header.Confirmations is negative.
func (w *spvWallet) getBlockHeader(blockHash *chainhash.Hash) (header *BlockHeader, mainchain bool, err error) {
	hdr, err := w.cl.GetBlockHeader(blockHash)
	if err != nil {
		return nil, false, err
	}

	tip, err := w.cl.BestBlock()
	if err != nil {
		return nil, false, fmt.Errorf("BestBlock error: %v", err)
	}

	blockHeight, err := w.cl.GetBlockHeight(blockHash)
	if err != nil {
		return nil, false, err
	}

	confirmations := int64(-1)
	mainchain = w.blockIsMainchain(blockHash, blockHeight)
	if mainchain {
		confirmations = int64(confirms(blockHeight, tip.Height))
	}

	return &BlockHeader{
		Hash:              hdr.BlockHash().String(),
		Confirmations:     confirmations,
		Height:            int64(blockHeight),
		Time:              hdr.Timestamp.Unix(),
		PreviousBlockHash: hdr.PrevBlock.String(),
	}, mainchain, nil
}

func (w *spvWallet) getBestBlockHeader() (*BlockHeader, error) {
	hash, err := w.getBestBlockHash()
	if err != nil {
		return nil, err
	}
	hdr, _, err := w.getBlockHeader(hash)
	return hdr, err
}

func (w *spvWallet) logFilePath() string {
	return filepath.Join(w.dir, logDirName, logFileName)
}

// connect will start the wallet and begin syncing.
func (w *spvWallet) connect(ctx context.Context, wg *sync.WaitGroup) (err error) {
	w.cl, err = w.wallet.Start()
	if err != nil {
		return err
	}

	blockNotes := w.wallet.BlockNotifications(ctx)

	// Nanny for the caches checkpoints and txBlocks caches.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer w.wallet.Stop()

		ticker := time.NewTicker(time.Minute * 20)
		defer ticker.Stop()
		expiration := time.Hour * 2
		for {
			select {
			case <-ticker.C:
				w.BlockFiltersScanner.CleanCaches(expiration)

			case blk := <-blockNotes:
				syncTarget := atomic.LoadInt32(&w.syncTarget)
				if syncTarget == 0 || (blk.Height < syncTarget && blk.Height%10_000 != 0) {
					continue
				}

				select {
				case w.tipChan <- &BlockVector{
					Hash:   blk.Hash,
					Height: int64(blk.Height),
				}:
				default:
					w.log.Warnf("tip report channel was blocking")
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// moveWalletData will move all wallet files to a backup directory, but leaving
// the logs folder.
func (w *spvWallet) moveWalletData(backupDir string) error {
	timeString := time.Now().Format("2006-01-02T15:04:05")
	backupFolder := filepath.Join(backupDir, w.chainParams.Name, timeString)
	err := os.MkdirAll(backupFolder, 0744)
	if err != nil {
		return err
	}

	// Copy wallet logs folder since we do not move it.
	backupLogDir := filepath.Join(backupFolder, logDirName)
	walletLogDir := filepath.Join(w.dir, logDirName)
	if err := copyDir(walletLogDir, backupLogDir); err != nil {
		return err
	}

	// Move contents of the wallet dir, except the logs folder.
	return filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == w.dir { // top
			return nil
		}
		if d.IsDir() && d.Name() == logDirName {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(w.dir, path)
		if err != nil {
			return err
		}
		err = os.Rename(path, filepath.Join(backupFolder, rel))
		if err != nil {
			return err
		}
		if d.IsDir() { // we just moved a folder, including the contents
			return filepath.SkipDir
		}
		return nil
	})
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	_, err = io.Copy(out, in)
	return err
}

// copyDir recursively copies the directories and files in source directory to
// destination directory without preserving the original file permissions. The
// destination folder must not exist.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	fi, err := os.Stat(dst)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		err = os.MkdirAll(dst, 0744)
		if err != nil {
			return err
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("%q is not a directory", dst)
	}

	for _, fd := range entries {
		fName := fd.Name()
		srcFile := filepath.Join(src, fName)
		dstFile := filepath.Join(dst, fName)
		if fd.IsDir() {
			err = copyDir(srcFile, dstFile)
		} else if fd.Type().IsRegular() {
			err = copyFile(srcFile, dstFile)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// numDerivedAddresses returns the number of internal and external addresses
// that the wallet has derived.
func (w *spvWallet) numDerivedAddresses() (internal, external uint32, err error) {
	props, err := w.wallet.AccountProperties(waddrmgr.KeyScopeBIP0084, w.acctNum)
	if err != nil {
		return 0, 0, err
	}

	return props.InternalKeyCount, props.ExternalKeyCount, nil
}

// fingerprint returns an identifier for this wallet. It is the hash of the
// compressed serialization of the account pub key.
func (w *spvWallet) fingerprint() (string, error) {
	props, err := w.wallet.AccountProperties(waddrmgr.KeyScopeBIP0084, w.acctNum)
	if err != nil {
		return "", err
	}

	if props.AccountPubKey == nil {
		return "", fmt.Errorf("no account key available")
	}

	pk, err := props.AccountPubKey.ECPubKey()
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(btcutil.Hash160(pk.SerializeCompressed())), nil
}

// getTxOut finds an unspent transaction output and its number of confirmations.
// To match the behavior of the RPC method, even if an output is found, if it's
// known to be spent, no *wire.TxOut and no error will be returned.
func (w *spvWallet) getTxOut(txHash *chainhash.Hash, vout uint32, pkScript []byte, startTime time.Time) (*wire.TxOut, uint32, error) {
	// Check for a wallet transaction first
	txDetails, err := w.wallet.WalletTransaction(txHash)
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
	walletBlock := w.wallet.SyncedTo() // where cfilters are received and processed
	walletTip := walletBlock.Height
	utxo, err := w.ScanFilters(txHash, vout, pkScript, walletTip, startTime, blockHash)
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

// searchBlockForRedemptions attempts to find spending info for the specified
// contracts by searching every input of all txs in the provided block range.
func (w *spvWallet) searchBlockForRedemptions(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq,
	blockHash chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult) {

	// Just match all the scripts together.
	scripts := make([][]byte, 0, len(reqs))
	for _, req := range reqs {
		scripts = append(scripts, req.pkScript)
	}

	discovered = make(map[OutPoint]*FindRedemptionResult, len(reqs))

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
		newlyDiscovered := findRedemptionsInTxWithHasher(ctx, true, reqs, msgTx, w.chainParams, hashTx)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

// findRedemptionsInMempool is unsupported for SPV.
func (w *spvWallet) findRedemptionsInMempool(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq) (discovered map[OutPoint]*FindRedemptionResult) {
	return
}

// confirmations looks for the confirmation count and spend status on a
// transaction output that pays to this wallet.
func (w *spvWallet) confirmations(txHash *chainhash.Hash, vout uint32) (blockHash *chainhash.Hash, confs uint32, spent bool, err error) {
	details, err := w.wallet.WalletTransaction(txHash)
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

// getWalletTransaction checks the wallet database for the specified
// transaction. Only transactions with output scripts that pay to the wallet or
// transactions that spend wallet outputs are stored in the wallet database.
// This is pretty much copy-paste from btcwallet 'gettransaction' JSON-RPC
// handler.
func (w *spvWallet) getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
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
	details, err := w.wallet.WalletTransaction(txHash)
	if err != nil {
		if errors.Is(err, WalletTransactionNotFound) {
			return nil, asset.CoinNotFoundError // for the asset.Wallet interface
		}
		return nil, err
	}

	syncBlock := w.wallet.SyncedTo()

	// TODO: The serialized transaction is already in the DB, so reserializing
	// might be avoided here. According to btcwallet, details.SerializedTx is
	// "optional" (?), but we might check for it.
	txRaw, err := serializeMsgTx(&details.MsgTx)
	if err != nil {
		return nil, err
	}

	ret := &GetTransactionResult{
		TxID:         txHash.String(),
		Bytes:        txRaw, // 'Hex' field name is a lie, kinda
		Time:         uint64(details.Received.Unix()),
		TimeReceived: uint64(details.Received.Unix()),
	}

	if details.Block.Height >= 0 {
		ret.BlockHash = details.Block.Hash.String()
		ret.BlockTime = uint64(details.Block.Time.Unix())
		// ret.BlockHeight = uint64(details.Block.Height)
		ret.Confirmations = uint64(confirms(details.Block.Height, syncBlock.Height))
	}

	return ret, nil

	/*
		var debitTotal, creditTotal btcutil.Amount // credits excludes change
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
			ret.Fee = (debitTotal - outputTotal).ToBTC()
		}

		ret.Amount = creditTotal.ToBTC()
		return ret, nil
	*/
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
