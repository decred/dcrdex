// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrwallet/v2/chain"
	walleterrors "decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/p2p"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/spv"
	"decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/udb"
	"github.com/decred/dcrd/addrmgr/v2"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/connmgr/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

const (
	defaultGapLimit        = uint32(100)
	defaultAllowHighFees   = false
	defaultRelayFeePerKb   = 1e4
	defaultAccountGapLimit = 10
	defaultManualTickets   = false

	defaultAcct     = 0
	defaultAcctName = "default"
	walletDbName    = "wallet.db"
	dbDriver        = "bdb"
	logDirName      = "logs"
	logFileName     = "neutrino.log"
)

type dcrWallet interface {
	KnownAddress(ctx context.Context, a stdaddr.Address) (wallet.KnownAddress, error)
	AccountBalance(ctx context.Context, account uint32, confirms int32) (wallet.Balances, error)
	LockedOutpoints(ctx context.Context, accountName string) ([]chainjson.TransactionInput, error)
	ListUnspent(ctx context.Context, minconf, maxconf int32, addresses map[string]struct{}, accountName string) ([]*walletjson.ListUnspentResult, error)
	UnlockOutpoint(txHash *chainhash.Hash, index uint32)
	LockOutpoint(txHash *chainhash.Hash, index uint32)
	ListTransactionDetails(ctx context.Context, txHash *chainhash.Hash) ([]walletjson.ListTransactionsResult, error)
	MainChainTip(ctx context.Context) (hash chainhash.Hash, height int32)
	NewExternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error)
	NewInternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error)
	SignTransaction(ctx context.Context, tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScripts map[wire.OutPoint][]byte,
		additionalKeysByAddress map[string]*dcrutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error)
	PublishTransaction(ctx context.Context, tx *wire.MsgTx, n wallet.NetworkBackend) (*chainhash.Hash, error)
	BlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*wire.BlockHeader, error)
	BlockInMainChain(ctx context.Context, hash *chainhash.Hash) (haveBlock, invalidated bool, err error)
	CFilterV2(ctx context.Context, blockHash *chainhash.Hash) ([gcs.KeySize]byte, *gcs.FilterV2, error)
	BlockInfo(ctx context.Context, blockID *wallet.BlockIdentifier) (*wallet.BlockInfo, error)
	AccountUnlocked(ctx context.Context, account uint32) (bool, error)
	LockAccount(ctx context.Context, account uint32) error
	UnlockAccount(ctx context.Context, account uint32, passphrase []byte) error
	LoadPrivateKey(ctx context.Context, addr stdaddr.Address) (key *secp256k1.PrivateKey, zero func(), err error)
	TxDetails(ctx context.Context, txHash *chainhash.Hash) (*udb.TxDetails, error)
	GetTransactionsByHashes(ctx context.Context, txHashes []*chainhash.Hash) (txs []*wire.MsgTx, notFound []*wire.InvVect, err error)
	// TODO: Rescan and DiscoverActiveAddresses can be used for a Rescanner.
}

// Interface for *spv.Syncer so that we can test with a stub.
type spvSyncer interface {
	wallet.NetworkBackend
	GetRemotePeers() map[string]*p2p.RemotePeer
}

// cachedBlock is a cached MsgBlock with a last-access time. The cleanBlockCache
// loop is started in Connect to periodically discard cachedBlocks that are too
// old.
type cachedBlock struct {
	*wire.MsgBlock
	lastAccess time.Time
}

type blockCache struct {
	sync.Mutex
	blocks map[chainhash.Hash]*cachedBlock // block hash -> block
}

// extendedWallet adds the TxDetails method to *wallet.Wallet.
type extendedWallet struct {
	*wallet.Wallet
}

// TxDetails exposes the (UnstableApi).TxDetails method.
func (w *extendedWallet) TxDetails(ctx context.Context, txHash *chainhash.Hash) (*udb.TxDetails, error) {
	return wallet.UnstableAPI(w.Wallet).TxDetails(ctx, txHash)
}

// spvWallet is a Wallet built on dcrwallet's *wallet.Wallet running in SPV
// mode.
type spvWallet struct {
	dcrWallet   // *wallet.Wallet
	db          wallet.DB
	acctNum     uint32
	acctName    string
	dir         string
	chainParams *chaincfg.Params
	log         dex.Logger
	spv         spvSyncer // *spv.Syncer
	tipChan     chan *block

	blockCache blockCache

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var _ Wallet = (*spvWallet)(nil)
var _ tipNotifier = (*spvWallet)(nil)

func createSPVWallet(pw, seed []byte, dataDir string, extIdx, intIdx uint32, chainParams *chaincfg.Params) error {
	dir := filepath.Join(dataDir, chainParams.Name, "spv")

	if err := initLogging(dir); err != nil {
		return fmt.Errorf("error initializing dcrwallet logging: %w", err)
	}

	if exists, err := walletExists(dir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("wallet at %q already exists", dir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	dbPath := filepath.Join(dir, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return fmt.Errorf("error checking file existence for %q: %w", dbPath, err)
	}
	if exists {
		return fmt.Errorf("database file already exists at %q", dbPath)
	}

	// Ensure the data directory for the network exists.
	if err := checkCreateDir(dir); err != nil {
		return fmt.Errorf("checkCreateDir error: %w", err)
	}

	// At this point it is asserted that there is no existing database file, and
	// deleting anything won't destroy a wallet in use.  Defer a function that
	// attempts to remove any wallet remnants.
	defer func() {
		if err != nil {
			_ = os.Remove(dir)
		}
	}()

	// Create the wallet database backed by bolt db.
	db, err := wallet.CreateDB(dbDriver, dbPath)
	if err != nil {
		return fmt.Errorf("CreateDB error: %w", err)
	}

	// Initialize the newly created database for the wallet before opening.
	err = wallet.Create(ctx, db, nil, pw, seed, chainParams)
	if err != nil {
		return fmt.Errorf("wallet.Create error: %w", err)
	}

	// Open the newly-created wallet.
	w, err := wallet.Open(ctx, newWalletConfig(db, chainParams))
	if err != nil {
		return fmt.Errorf("wallet.Open error: %w", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			fmt.Println("Error closing database:", err)
		}
	}()

	err = w.UpgradeToSLIP0044CoinType(ctx)
	if err != nil {
		return err
	}

	err = w.Unlock(ctx, pw, nil)
	if err != nil {
		return fmt.Errorf("error unlocking wallet: %w", err)
	}

	err = w.SetAccountPassphrase(ctx, defaultAcct, pw)
	if err != nil {
		return fmt.Errorf("error setting Decred account %d passphrase: %v", defaultAcct, err)
	}

	w.Lock()

	if extIdx > 0 || intIdx > 0 {
		err = extendAddresses(ctx, extIdx, intIdx, w)
		if err != nil {
			return fmt.Errorf("failed to set starting address indexes: %w", err)
		}
	}

	return nil
}

func (w *spvWallet) startWallet(ctx context.Context) error {
	if err := initLogging(w.dir); err != nil {
		return fmt.Errorf("error initializing dcrwallet logging: %w", err)
	}

	db, err := wallet.OpenDB(dbDriver, filepath.Join(w.dir, walletDbName))
	if err != nil {
		return fmt.Errorf("wallet.OpenDB error: %w", err)
	}

	dcrw, err := wallet.Open(ctx, newWalletConfig(db, w.chainParams))
	if err != nil {
		// If this function does not return to completion the database must be
		// closed.  Otherwise, because the database is locked on open, any
		// other attempts to open the wallet will hang, and there is no way to
		// recover since this db handle would be leaked.
		if err := db.Close(); err != nil {
			w.log.Errorf("Uh oh. Failed to close the database: %v", err)
		}
		return fmt.Errorf("wallet.Open error: %w", err)
	}
	w.dcrWallet = &extendedWallet{dcrw}
	w.db = db

	var connectPeers []string
	switch w.chainParams.Net {
	case wire.SimNet:
		connectPeers = []string{"localhost:19560"}
	}

	spv := newSpvSyncer(dcrw, w.dir, connectPeers)
	w.spv = spv

	w.wg.Add(2)
	go func() {
		defer w.wg.Done()
		w.spvLoop(ctx, spv)
	}()
	go func() {
		defer w.wg.Done()
		w.notesLoop(ctx, dcrw)
	}()

	return nil
}

// stop stops the wallet and database threads.
func (w *spvWallet) stop() {
	w.log.Info("Unloading wallet")
	if err := w.db.Close(); err != nil {
		w.log.Info("Error closing database: %v", err)
	}

	w.log.Info("SPV wallet closed")
}

func (w *spvWallet) spvLoop(ctx context.Context, syncer *spv.Syncer) {
	for {
		err := syncer.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		w.log.Errorf("SPV synchronization ended. trying again in 10 seconds: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second * 10):
		}
	}
}

func (w *spvWallet) notesLoop(ctx context.Context, dcrw *wallet.Wallet) {
	txNotes := dcrw.NtfnServer.TransactionNotifications()
	defer txNotes.Done()
	// removeTxNotes := dcrw.NtfnServer.RemovedTransactionNotifications()
	// defer removeTxNotes.Done()
	// acctNotes := dcrw.NtfnServer.AccountNotifications()
	// defer acctNotes.Done()
	// tipNotes := dcrw.NtfnServer.MainTipChangedNotifications()
	// defer tipNotes.Done()
	// confirmNotes := w.NtfnServer.ConfirmationNotifications(ctx)

	for {
		select {
		case n := <-txNotes.C:
			if len(n.AttachedBlocks) == 0 {
				continue
			}
			lastBlock := n.AttachedBlocks[len(n.AttachedBlocks)-1]
			h := lastBlock.Header.BlockHash()
			select {
			case w.tipChan <- &block{
				hash:   &h,
				height: int64(lastBlock.Header.Height),
			}:
			default:
				w.log.Warnf("tip report channel was blocking")
			}
		case <-ctx.Done():
			return
		}
	}
}

func (w *spvWallet) tipFeed() <-chan *block {
	return w.tipChan
}

// Connect starts the wallet and begins synchronization.
func (w *spvWallet) Connect(ctx context.Context) error {
	ctx, w.cancel = context.WithCancel(ctx)
	err := w.startWallet(ctx)
	if err != nil {
		return err
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer w.stop()

		ticker := time.NewTicker(time.Minute * 20)

		for {
			select {
			case <-ticker.C:
				w.cleanBlockCache()
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Disconnect shuts down the wallet and waits for monitored threads to exit.
// Part of the Wallet interface.
func (w *spvWallet) Disconnect() {
	w.cancel()
	w.wg.Wait()
}

// SpvMode is always true for spvWallet.
// Part of the Wallet interface.
func (w *spvWallet) SpvMode() bool {
	return true
}

// NotifyOnTipChange is not used, in favor of the tipNotifier pattern from btc.
func (w *spvWallet) NotifyOnTipChange(ctx context.Context, cb TipChangeCallback) bool {
	return false
}

// AddressInfo returns information for the provided address. It is an error if
// the address is not owned by the wallet.
func (w *spvWallet) AddressInfo(ctx context.Context, addrStr string) (*AddressInfo, error) {
	addr, err := stdaddr.DecodeAddress(addrStr, w.chainParams)
	if err != nil {
		return nil, err
	}
	ka, err := w.KnownAddress(ctx, addr)
	if err != nil {
		return nil, err
	}

	if ka, ok := ka.(wallet.BIP0044Address); ok {
		_, branch, _ := ka.Path()
		return &AddressInfo{Account: ka.AccountName(), Branch: branch}, nil
	}
	return nil, fmt.Errorf("unsupported address type %T", ka)
}

// AccountOwnsAddress checks if the provided address belongs to the specified
// account.
// Part of the Wallet interface.
func (w *spvWallet) AccountOwnsAddress(ctx context.Context, addr stdaddr.Address, _ string) (bool, error) {
	ka, err := w.KnownAddress(ctx, addr)
	if err != nil {
		return false, fmt.Errorf("KnownAddress error: %w", err)
	}
	if ka.AccountName() != w.acctName {
		return false, nil
	}
	if kind := ka.AccountKind(); kind != wallet.AccountKindBIP0044 && kind != wallet.AccountKindImported {
		return false, nil
	}
	return true, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) AccountBalance(ctx context.Context, confirms int32, _ string) (*walletjson.GetAccountBalanceResult, error) {
	bal, err := w.dcrWallet.AccountBalance(ctx, w.acctNum, confirms)
	if err != nil {
		return nil, err
	}

	return &walletjson.GetAccountBalanceResult{
		AccountName:             w.acctName,
		ImmatureCoinbaseRewards: bal.ImmatureCoinbaseRewards.ToCoin(),
		ImmatureStakeGeneration: bal.ImmatureStakeGeneration.ToCoin(),
		LockedByTickets:         bal.LockedByTickets.ToCoin(),
		Spendable:               bal.Spendable.ToCoin(),
		Total:                   bal.Total.ToCoin(),
		Unconfirmed:             bal.Unconfirmed.ToCoin(),
		VotingAuthority:         bal.VotingAuthority.ToCoin(),
	}, nil
}

// LockedOutputs fetches locked outputs for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) LockedOutputs(ctx context.Context, _ string) ([]chainjson.TransactionInput, error) {
	return w.dcrWallet.LockedOutpoints(ctx, w.acctName)
}

// Unspents fetches unspent outputs for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) Unspents(ctx context.Context, _ string) ([]*walletjson.ListUnspentResult, error) {
	return w.dcrWallet.ListUnspent(ctx, 0, math.MaxInt32, nil, w.acctName)
}

// LockUnspent locks or unlocks the specified outpoint.
// Part of the Wallet interface.
func (w *spvWallet) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	fun := w.LockOutpoint
	if unlock {
		fun = w.UnlockOutpoint
	}
	for _, op := range ops {
		fun(&op.Hash, op.Index)
	}
	return nil
}

// UnspentOutput returns information about an unspent tx output, if found
// and unspent.
// This method is only guaranteed to return results for outputs that pay to
// the wallet. Returns asset.CoinNotFoundError if the unspent output cannot
// be located.
// Part of the Wallet interface.
func (w *spvWallet) UnspentOutput(ctx context.Context, txHash *chainhash.Hash, index uint32, _ int8) (*TxOutput, error) {
	txd, err := w.dcrWallet.TxDetails(ctx, txHash)
	if errors.Is(err, walleterrors.NotExist) {
		return nil, asset.CoinNotFoundError
	} else if err != nil {
		return nil, err
	}

	details, err := w.ListTransactionDetails(ctx, txHash)
	if err != nil {
		return nil, err
	}

	var addrStr string
	for _, detail := range details {
		if detail.Vout == index {
			addrStr = detail.Address
		}
	}
	if addrStr == "" {
		return nil, fmt.Errorf("error locating address for output")
	}

	tree := wire.TxTreeRegular
	if txd.TxType != stake.TxTypeRegular {
		tree = wire.TxTreeStake
	}

	if len(txd.MsgTx.TxOut) <= int(index) {
		return nil, fmt.Errorf("not enough outputs")
	}

	_, tipHeight := w.MainChainTip(ctx)

	var ours bool
	for _, credit := range txd.Credits {
		if credit.Index == index {
			if credit.Spent {
				return nil, asset.CoinNotFoundError
			}
			ours = true
			break
		}
	}

	if !ours {
		return nil, asset.CoinNotFoundError
	}

	return &TxOutput{
		TxOut:         txd.MsgTx.TxOut[index],
		Tree:          tree,
		Addresses:     []string{addrStr},
		Confirmations: uint32(txd.Block.Height - tipHeight + 1),
	}, nil
}

// ExternalAddress returns an external address using GapPolicyIgnore.
// Part of the Wallet interface.
// Using GapPolicyWrap here, introducing a relatively small risk of address
// reuse, but improving wallet recoverability.
func (w *spvWallet) ExternalAddress(ctx context.Context, _ string) (stdaddr.Address, error) {
	return w.NewExternalAddress(ctx, w.acctNum, wallet.WithGapPolicyWrap())
}

// InternalAddress returns an internal address using GapPolicyIgnore.
// Part of the Wallet interface.
func (w *spvWallet) InternalAddress(ctx context.Context, _ string) (stdaddr.Address, error) {
	return w.NewInternalAddress(ctx, w.acctNum, wallet.WithGapPolicyIgnore())
}

// SignRawTransaction signs the provided transaction.
// Part of the Wallet interface.
func (w *spvWallet) SignRawTransaction(ctx context.Context, baseTx *wire.MsgTx) (*wire.MsgTx, error) {
	tx := baseTx.Copy()
	sigErrs, err := w.dcrWallet.SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if len(sigErrs) > 0 {
		for _, sigErr := range sigErrs {
			w.log.Errorf("signature error for index %d: %v", sigErr.InputIndex, sigErr.Error)
		}
		return nil, fmt.Errorf("%d signature errors", len(sigErrs))
	}
	return tx, nil
}

// SendRawTransaction broadcasts the provided transaction to the Decred network.
// Part of the Wallet interface.
func (w *spvWallet) SendRawTransaction(ctx context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	// TODO: Conditional high fee check?
	return w.PublishTransaction(ctx, tx, w.spv)
}

// GetBlockHeader generates a *BlockHeader for the specified block hash. The
// returned block header is a wire.BlockHeader with the addition of the block's
// median time and other auxiliary information.
func (w *spvWallet) GetBlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*BlockHeader, error) {
	hdr, err := w.dcrWallet.BlockHeader(ctx, blockHash)
	if err != nil {
		return nil, err
	}

	medianTime, err := w.medianTime(ctx, hdr)
	if err != nil {
		return nil, err
	}

	_, tipHeight := w.MainChainTip(ctx)
	if tipHeight < int32(hdr.Height) {
		return nil, errors.New("sumpin's wrong with our tip")
	}

	var nextHash *chainhash.Hash
	if tipHeight > int32(hdr.Height) {
		nextHash, err = w.GetBlockHash(ctx, int64(hdr.Height)+1)
		if err != nil {
			return nil, fmt.Errorf("error getting next hash for block %q: %w", blockHash, err)
		}
	}

	return &BlockHeader{
		BlockHeader:   hdr,
		MedianTime:    medianTime,
		Confirmations: int64(uint32(tipHeight)-hdr.Height) + 1,
		NextHash:      nextHash,
	}, nil
}

// medianTime calculates a blocks median time, which is the median of the
// timestamps of the previous 11 blocks.
func (w *spvWallet) medianTime(ctx context.Context, iBlkHeader *wire.BlockHeader) (int64, error) {
	// Calculate past median time. Look at the last 11 blocks, starting
	// with the requested block, which is consistent with dcrd.
	const numStamp = 11
	timestamps := make([]int64, 0, numStamp)
	for {
		timestamps = append(timestamps, iBlkHeader.Timestamp.Unix())
		if iBlkHeader.Height == 0 || len(timestamps) == numStamp {
			break
		}
		var err error
		iBlkHeader, err = w.dcrWallet.BlockHeader(ctx, &iBlkHeader.PrevBlock)
		if err != nil {
			return 0, fmt.Errorf("info not found for previous block: %v", err)
		}
	}
	sort.Slice(timestamps, func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	return timestamps[len(timestamps)/2], nil
}

// GetBlock returns the MsgBlock.
// Part of the Wallet interface.
func (w *spvWallet) GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	if block := w.cachedBlock(blockHash); block != nil {
		return block, nil
	}

	blocks, err := w.spv.Blocks(ctx, []*chainhash.Hash{blockHash})
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 { // Shouldn't actually be possible.
		return nil, fmt.Errorf("network returned 0 blocks")
	}

	block := blocks[0]
	w.cacheBlock(block)
	return block, nil
}

// GetTransaction returns the details of a wallet tx, if the wallet contains a
// tx with the provided hash. Returns asset.CoinNotFoundError if the tx is not
// found in the wallet.
// Part of the Wallet interface.
func (w *spvWallet) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*WalletTransaction, error) {
	// copy-pasted from dcrwallet/internal/rpc/jsonrpc/methods.go
	txd, err := w.dcrWallet.TxDetails(ctx, txHash)
	if errors.Is(err, walleterrors.NotExist) {
		return nil, asset.CoinNotFoundError
	} else if err != nil {
		return nil, err
	}

	_, tipHeight := w.MainChainTip(ctx)

	var b strings.Builder
	b.Grow(2 * txd.MsgTx.SerializeSize())
	err = txd.MsgTx.Serialize(hex.NewEncoder(&b))
	if err != nil {
		return nil, err
	}

	ret := WalletTransaction{
		Hex: b.String(),
	}

	if txd.Block.Height != -1 {
		ret.BlockHash = txd.Block.Hash.String()
		ret.Confirmations = int64(tipHeight - txd.Block.Height + 1)
	}

	details, err := w.ListTransactionDetails(ctx, txHash)
	if err != nil {
		return nil, err
	}
	ret.Details = make([]walletjson.GetTransactionDetailsResult, len(details))
	for i, d := range details {
		ret.Details[i] = walletjson.GetTransactionDetailsResult{
			Account:           d.Account,
			Address:           d.Address,
			Amount:            d.Amount,
			Category:          d.Category,
			InvolvesWatchOnly: d.InvolvesWatchOnly,
			Fee:               d.Fee,
			Vout:              d.Vout,
		}
	}

	return &ret, nil
}

// MatchAnyScript looks for any of the provided scripts in the block specified.
// Part of the Wallet interface.
func (w *spvWallet) MatchAnyScript(ctx context.Context, blockHash *chainhash.Hash, scripts [][]byte) (bool, error) {
	key, filter, err := w.dcrWallet.CFilterV2(ctx, blockHash)
	if err != nil {
		return false, err
	}
	return filter.MatchAny(key, scripts), nil

}

// GetRawTransaction returns details of the tx with the provided hash. Returns
// asset.CoinNotFoundError if the tx is not found.
// Part of the Wallet interface.
func (w *spvWallet) GetRawTransaction(ctx context.Context, txHash *chainhash.Hash) (*wire.MsgTx, error) {
	txs, _, err := w.dcrWallet.GetTransactionsByHashes(ctx, []*chainhash.Hash{txHash})
	if err != nil {
		return nil, err
	}
	if len(txs) != 1 {
		return nil, asset.CoinNotFoundError
	}
	return txs[0], nil
}

// GetBestBlock returns the hash and height of the wallet's best block.
// Part of the Wallet interface.
func (w *spvWallet) GetBestBlock(ctx context.Context) (*chainhash.Hash, int64, error) {
	blockHash, blockHeight := w.dcrWallet.MainChainTip(ctx)
	return &blockHash, int64(blockHeight), nil
}

// GetBlockHash returns the hash of the mainchain block at the specified height.
// Part of the Wallet interface.
func (w *spvWallet) GetBlockHash(ctx context.Context, blockHeight int64) (*chainhash.Hash, error) {
	info, err := w.dcrWallet.BlockInfo(ctx, wallet.NewBlockIdentifierFromHeight(int32(blockHeight)))
	if err != nil {
		return nil, err
	}
	return &info.Hash, nil
}

// AccountUnlocked returns true if the account is unlocked.
// Part of the Wallet interface.
func (w *spvWallet) AccountUnlocked(ctx context.Context, _ string) (bool, error) {
	return w.dcrWallet.AccountUnlocked(ctx, w.acctNum)
}

// LockAccount locks the specified account.
// Part of the Wallet interface.
func (w *spvWallet) LockAccount(ctx context.Context, _ string) error {
	return w.dcrWallet.LockAccount(ctx, w.acctNum)
}

// UnlockAccount unlocks the specified account or the wallet if account is not
// encrypted. Part of the Wallet interface.
func (w *spvWallet) UnlockAccount(ctx context.Context, pw []byte, _ string) error {
	return w.dcrWallet.UnlockAccount(ctx, w.acctNum, pw)
}

// SyncStatus returns the wallet's sync status.
// Part of the Wallet interface.
func (w *spvWallet) SyncStatus(ctx context.Context) (bool, float32, error) {
	bestHeight := w.bestPeerInitialHeight()
	_, h := w.dcrWallet.MainChainTip(ctx)
	height := h
	if h > bestHeight {
		bestHeight = h
	}

	if h == 0 || bestHeight == 0 {
		return false, 0, nil
	}

	return bestHeight == height, float32(height) / float32(bestHeight), nil
}

// bestPeerInitialHeight is the highest InitialHeight recorded from our peers.
func (w *spvWallet) bestPeerInitialHeight() int32 {
	var bestHeight int32
	for _, p := range w.spv.GetRemotePeers() {
		if h := p.InitialHeight(); h > bestHeight {
			bestHeight = h
		}
	}
	return bestHeight
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the Wallet interface.
func (w *spvWallet) AddressPrivKey(ctx context.Context, addr stdaddr.Address) (*secp256k1.PrivateKey, error) {
	privKey, _, err := w.dcrWallet.LoadPrivateKey(ctx, addr)
	return privKey, err
}

// cacheBlock caches a block for future use. The block has a lastAccess stamp
// added, and will be discarded if not accessed again within 2 hours.
func (w *spvWallet) cacheBlock(block *wire.MsgBlock) {
	blockHash := block.BlockHash()
	w.blockCache.Lock()
	defer w.blockCache.Unlock()
	cached := w.blockCache.blocks[blockHash]
	if cached == nil {
		cb := &cachedBlock{
			MsgBlock:   block,
			lastAccess: time.Now(),
		}
		w.blockCache.blocks[blockHash] = cb
	} else {
		cached.lastAccess = time.Now()
	}
}

// cachedBlock retrieves the MsgBlock from the cache, if it's been cached, else
// nil.
func (w *spvWallet) cachedBlock(blockHash *chainhash.Hash) *wire.MsgBlock {
	w.blockCache.Lock()
	defer w.blockCache.Unlock()
	cached := w.blockCache.blocks[*blockHash]
	if cached == nil {
		return nil
	}
	cached.lastAccess = time.Now()
	return cached.MsgBlock
}

// PeerCount returns the count of currently connected peers.
func (w *spvWallet) PeerCount(ctx context.Context) (uint32, error) {
	return uint32(len(w.spv.GetRemotePeers())), nil
}

// cleanBlockCache discards from the blockCache any blocks that have not been
// accessed for > 2 hours.
func (w *spvWallet) cleanBlockCache() {
	w.blockCache.Lock()
	defer w.blockCache.Unlock()
	for blockHash, cb := range w.blockCache.blocks {
		if time.Since(cb.lastAccess) > time.Hour*2 {
			delete(w.blockCache.blocks, blockHash)
		}
	}
}

func newSpvSyncer(w *wallet.Wallet, netDir string, connectPeers []string) *spv.Syncer {
	addr := &net.TCPAddr{IP: net.ParseIP("::1"), Port: 0}
	amgr := addrmgr.New(netDir, net.LookupIP)
	lp := p2p.NewLocalPeer(w.ChainParams(), addr, amgr)
	syncer := spv.NewSyncer(w, lp)
	if len(connectPeers) > 0 {
		syncer.SetPersistentPeers(connectPeers)
	}
	w.SetNetworkBackend(syncer)
	return syncer
}

// extendAddresses ensures that the internal and external branches have been
// extended to the specified indices. This can be used at wallet restoration to
// ensure that no duplicates are encountered with existing but unused addresses.
func extendAddresses(ctx context.Context, extIdx, intIdx uint32, dcrw *wallet.Wallet) error {
	if err := dcrw.SyncLastReturnedAddress(ctx, defaultAcct, udb.ExternalBranch, extIdx); err != nil {
		return fmt.Errorf("error syncing external branch index: %w", err)
	}

	if err := dcrw.SyncLastReturnedAddress(ctx, defaultAcct, udb.InternalBranch, intIdx); err != nil {
		return fmt.Errorf("error syncing internal branch index: %w", err)
	}

	return nil
}

func newWalletConfig(db wallet.DB, chainParams *chaincfg.Params) *wallet.Config {
	return &wallet.Config{
		DB:              db,
		GapLimit:        defaultGapLimit,
		AccountGapLimit: defaultAccountGapLimit,
		ManualTickets:   defaultManualTickets,
		AllowHighFees:   defaultAllowHighFees,
		RelayFee:        defaultRelayFeePerKb,
		Params:          chainParams,
	}
}

func checkCreateDir(path string) error {
	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(path, 0700); err != nil {
				return fmt.Errorf("cannot create directory: %s", err)
			}
		} else {
			return fmt.Errorf("error checking directory: %s", err)
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("path '%s' is not a directory", path)
	}

	return nil
}

// walletExists returns whether a file exists at the loader's database path.
// This may return an error for unexpected I/O failures.
func walletExists(dbDir string) (bool, error) {
	return fileExists(filepath.Join(dbDir, walletDbName))
}

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// logWriter implements an io.Writer that outputs to a rotating log file.
type logWriter struct {
	*rotator.Rotator
}

// Write writes the data in p to the log file.
func (w logWriter) Write(p []byte) (n int, err error) {
	return w.Rotator.Write(p)
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

// initLogging initializes logging in the dcrwallet packages. Logging only has
// to be initialized once, so an atomic flag is used internally to return early
// on subsequent invocations.
//
// TODO: See if the below precaution is even necessary for dcrwallet.
// // In theory, the the rotating file logger must be Close'd at some point, but
// // there are concurrency issues with that since btcd and btcwallet have
// // unsupervised goroutines still running after shutdown. So we leave the rotator
// // running at the risk of losing some logs.
func initLogging(netDir string) error {
	if !atomic.CompareAndSwapUint32(&loggingInited, 0, 1) {
		return nil
	}

	logSpinner, err := logRotator(netDir)
	if err != nil {
		return fmt.Errorf("error initializing log rotator: %w", err)
	}

	backendLog := slog.NewBackend(logWriter{logSpinner})

	logger := func(name string, lvl slog.Level) slog.Logger {
		l := backendLog.Logger(name)
		l.SetLevel(lvl)
		return l
	}
	wallet.UseLogger(logger("WLLT", slog.LevelInfo))
	udb.UseLogger(logger("UDB", slog.LevelInfo))
	chain.UseLogger(logger("CHAIN", slog.LevelInfo))
	spv.UseLogger(logger("SPV", slog.LevelDebug))
	p2p.UseLogger(logger("P2P", slog.LevelInfo))
	connmgr.UseLogger(logger("CONMGR", slog.LevelInfo))

	return nil
}
