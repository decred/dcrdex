// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"encoding/base64"
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

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/utils"
	"decred.org/dcrwallet/v4/chain"
	walleterrors "decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/p2p"
	walletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v4/spv"
	vspclient "decred.org/dcrwallet/v4/vsp"
	"decred.org/dcrwallet/v4/wallet"
	"decred.org/dcrwallet/v4/wallet/udb"
	"github.com/decred/dcrd/addrmgr/v2"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/connmgr/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

const (
	defaultAllowHighFees   = false
	defaultRelayFeePerKb   = 1e4
	defaultAccountGapLimit = 3
	defaultManualTickets   = false
	defaultMixSplitLimit   = 10

	defaultAcct        = 0
	defaultAccountName = "default"
	walletDbName       = "wallet.db"
	dbDriver           = "bdb"
	logDirName         = "spvlogs"
	logFileName        = "neutrino.log"
)

type dcrWallet interface {
	KnownAddress(ctx context.Context, a stdaddr.Address) (wallet.KnownAddress, error)
	AccountNumber(ctx context.Context, accountName string) (uint32, error)
	AddressAtIdx(ctx context.Context, account, branch, childIdx uint32) (stdaddr.Address, error)
	AccountBalance(ctx context.Context, account uint32, confirms int32) (wallet.Balances, error)
	LockedOutpoints(ctx context.Context, accountName string) ([]chainjson.TransactionInput, error)
	ListUnspent(ctx context.Context, minconf, maxconf int32, addresses map[string]struct{}, accountName string) ([]*walletjson.ListUnspentResult, error)
	LockOutpoint(txHash *chainhash.Hash, index uint32)
	ListTransactionDetails(ctx context.Context, txHash *chainhash.Hash) ([]walletjson.ListTransactionsResult, error)
	MixAccount(context.Context, uint32, uint32, uint32) error
	MainChainTip(ctx context.Context) (hash chainhash.Hash, height int32)
	NewExternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error)
	NewInternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error)
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
	StakeInfo(ctx context.Context) (*wallet.StakeInfoData, error)
	PurchaseTickets(ctx context.Context, n wallet.NetworkBackend, req *wallet.PurchaseTicketsRequest) (*wallet.PurchaseTicketsResponse, error)
	ForUnspentUnexpiredTickets(ctx context.Context, f func(hash *chainhash.Hash) error) error
	GetTickets(ctx context.Context, f func([]*wallet.TicketSummary, *wire.BlockHeader) (bool, error), startBlock, endBlock *wallet.BlockIdentifier) error
	TreasuryKeyPolicies() []wallet.TreasuryKeyPolicy
	GetAllTSpends(ctx context.Context) []*wire.MsgTx
	TSpendPolicy(tspendHash, ticketHash *chainhash.Hash) stake.TreasuryVoteT
	VSPHostForTicket(ctx context.Context, ticketHash *chainhash.Hash) (string, error)
	SetAgendaChoices(ctx context.Context, ticketHash *chainhash.Hash, choices map[string]string) (voteBits uint16, err error)
	SetTSpendPolicy(ctx context.Context, tspendHash *chainhash.Hash, policy stake.TreasuryVoteT, ticketHash *chainhash.Hash) error
	SetTreasuryKeyPolicy(ctx context.Context, pikey []byte, policy stake.TreasuryVoteT, ticketHash *chainhash.Hash) error
	SetRelayFee(relayFee dcrutil.Amount)
	GetTicketInfo(ctx context.Context, hash *chainhash.Hash) (*wallet.TicketSummary, *wire.BlockHeader, error)
	GetTransactions(ctx context.Context, f func(*wallet.Block) (bool, error), startBlock, endBlock *wallet.BlockIdentifier) error
	ListSinceBlock(ctx context.Context, start, end, syncHeight int32) ([]walletjson.ListTransactionsResult, error)
	UnlockOutpoint(txHash *chainhash.Hash, index uint32)
	SignTransaction(ctx context.Context, tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScripts map[wire.OutPoint][]byte,
		additionalKeysByAddress map[string]*dcrutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error)
	AgendaChoices(ctx context.Context, ticketHash *chainhash.Hash) (choices map[string]string, voteBits uint16, err error)
	NewVSPTicket(ctx context.Context, hash *chainhash.Hash) (*wallet.VSPTicket, error)
	RescanProgressFromHeight(ctx context.Context, n wallet.NetworkBackend, startHeight int32, p chan<- wallet.RescanProgress)
	RescanPoint(ctx context.Context) (*chainhash.Hash, error)
}

// Interface for *spv.Syncer so that we can test with a stub.
type spvSyncer interface {
	wallet.NetworkBackend
	Synced(context.Context) (bool, int32)
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

// MainTipChangedNotifications returns a channel for receiving main tip change
// notifications, along with a function to close the channel when it is no
// longer needed.
func (w *extendedWallet) MainTipChangedNotifications() (chan *wallet.MainTipChangedNotification, func()) {
	ntfn := w.NtfnServer.MainTipChangedNotifications()
	return ntfn.C, ntfn.Done
}

// spvWallet is a Wallet built on dcrwallet's *wallet.Wallet running in SPV
// mode.
type spvWallet struct {
	dcrWallet         // *extendedWallet
	db                wallet.DB
	dir               string
	chainParams       *chaincfg.Params
	log               dex.Logger
	spv               spvSyncer // *spv.Syncer
	bestSpvPeerHeight int32     // atomic
	tipChan           chan *block
	gapLimit          uint32

	blockCache blockCache

	accts atomic.Value

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var _ Wallet = (*spvWallet)(nil)
var _ tipNotifier = (*spvWallet)(nil)

func createSPVWallet(pw, seed []byte, dataDir string, extIdx, intIdx, gapLimit uint32, chainParams *chaincfg.Params) error {
	netDir := filepath.Join(dataDir, chainParams.Name)
	walletDir := filepath.Join(netDir, "spv")

	if err := initLogging(netDir); err != nil {
		return fmt.Errorf("error initializing dcrwallet logging: %w", err)
	}

	if exists, err := walletExists(walletDir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("wallet at %q already exists", walletDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	dbPath := filepath.Join(walletDir, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return fmt.Errorf("error checking file existence for %q: %w", dbPath, err)
	}
	if exists {
		return fmt.Errorf("database file already exists at %q", dbPath)
	}

	// Ensure the data directory for the network exists.
	if err := checkCreateDir(walletDir); err != nil {
		return fmt.Errorf("checkCreateDir error: %w", err)
	}

	// At this point it is asserted that there is no existing database file, and
	// deleting anything won't destroy a wallet in use.  Defer a function that
	// attempts to remove any wallet remnants.
	defer func() {
		if err != nil {
			_ = os.Remove(walletDir)
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
	w, err := wallet.Open(ctx, newWalletConfig(db, chainParams, gapLimit))
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

	err = setupMixingAccounts(ctx, w, pw)
	if err != nil {
		return fmt.Errorf("error setting up mixing accounts: %v", err)
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

// If we're running on simnet, add some tspends and treasury keys.
func (w *spvWallet) initializeSimnetTspends(ctx context.Context) {
	if w.chainParams.Net != wire.SimNet {
		return
	}
	tspendWallet, is := w.dcrWallet.(interface {
		AddTSpend(tx wire.MsgTx) error
		GetAllTSpends(ctx context.Context) []*wire.MsgTx
		SetTreasuryKeyPolicy(ctx context.Context, pikey []byte, policy stake.TreasuryVoteT, ticketHash *chainhash.Hash) error
		TreasuryKeyPolicies() []wallet.TreasuryKeyPolicy
	})
	if !is {
		return
	}
	const numFakeTspends = 3
	if len(tspendWallet.GetAllTSpends(ctx)) >= numFakeTspends {
		return
	}
	expiryBase := uint32(time.Now().Add(time.Hour * 24 * 365).Unix())
	for i := uint32(0); i < numFakeTspends; i++ {
		var signatureScript [100]byte
		tx := &wire.MsgTx{
			Expiry: expiryBase + i,
			TxIn:   []*wire.TxIn{wire.NewTxIn(&wire.OutPoint{}, 0, signatureScript[:])},
			TxOut:  []*wire.TxOut{{Value: int64(i+1) * 1e8}},
		}
		if err := tspendWallet.AddTSpend(*tx); err != nil {
			w.log.Errorf("Error adding simnet tspend: %v", err)
		}
	}
	if len(tspendWallet.TreasuryKeyPolicies()) == 0 {
		priv, _ := secp256k1.GeneratePrivateKey()
		tspendWallet.SetTreasuryKeyPolicy(ctx, priv.PubKey().SerializeCompressed(), 0x01 /* yes */, nil)
	}
}

// setupMixingAccounts checks if the mixed, unmixed and trading accounts
// required to use this wallet for funds mixing exists and creates any of the
// accounts that does not yet exist. The wallet should be unlocked before
// calling this function.
func setupMixingAccounts(ctx context.Context, w *wallet.Wallet, pw []byte) error {
	requiredAccts := []string{mixedAccountName, tradingAccountName} // unmixed (default) acct already exists
	for _, acct := range requiredAccts {
		_, err := w.AccountNumber(ctx, acct)
		if err == nil {
			continue // account exist, check next account
		}

		if !errors.Is(err, walleterrors.NotExist) {
			return err
		}

		acctNum, err := w.NextAccount(ctx, acct)
		if err != nil {
			return err
		}
		if err = w.SetAccountPassphrase(ctx, acctNum, pw); err != nil {
			return err
		}
	}

	return nil
}

func (w *spvWallet) setAccounts(mixingEnabled bool) {
	if mixingEnabled {
		w.accts.Store(XCWalletAccounts{
			PrimaryAccount: mixedAccountName,
			UnmixedAccount: defaultAccountName,
			TradingAccount: tradingAccountName,
		})
		return
	}
	w.accts.Store(XCWalletAccounts{
		PrimaryAccount: defaultAccountName,
	})
}

// Accounts returns the names of the accounts for use by the exchange wallet.
func (w *spvWallet) Accounts() XCWalletAccounts {
	return w.accts.Load().(XCWalletAccounts)
}

func (w *spvWallet) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, net dex.Network, currentAddress string) (restart bool, err error) {
	return cfg.Type != walletTypeSPV, nil
}

// InitialAddress returns the branch 0, child 0 address of the default
// account.
func (w *spvWallet) InitialAddress(ctx context.Context) (string, error) {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, defaultAccountName)
	if err != nil {
		return "", err
	}

	addr, err := w.dcrWallet.AddressAtIdx(ctx, acctNum, 0, 0)
	if err != nil {
		return "", err
	}

	return addr.String(), nil
}

func (w *spvWallet) startWallet(ctx context.Context) error {
	netDir := filepath.Dir(w.dir)
	if err := initLogging(netDir); err != nil {
		return fmt.Errorf("error initializing dcrwallet logging: %w", err)
	}

	db, err := wallet.OpenDB(dbDriver, filepath.Join(w.dir, walletDbName))
	if err != nil {
		return fmt.Errorf("wallet.OpenDB error: %w", err)
	}

	dcrw, err := wallet.Open(ctx, newWalletConfig(db, w.chainParams, w.gapLimit))
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

	w.initializeSimnetTspends(ctx)

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
				if len(n.UnminedTransactions) > 0 {
					select {
					case w.tipChan <- nil:
					default:
						w.log.Warnf("tx report channel was blocking")
					}
				}
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

// WalletOwnsAddress returns whether any of the account controlled by this
// wallet owns the specified address.
func (w *spvWallet) WalletOwnsAddress(ctx context.Context, addr stdaddr.Address) (bool, error) {
	ka, err := w.KnownAddress(ctx, addr)
	if err != nil {
		if errors.Is(err, walleterrors.NotExist) {
			return false, nil
		}
		return false, fmt.Errorf("KnownAddress error: %w", err)
	}
	if kind := ka.AccountKind(); kind != wallet.AccountKindBIP0044 && kind != wallet.AccountKindImported {
		return false, nil
	}

	return true, nil
}

// AccountOwnsAddress checks if the provided address belongs to the specified
// account.
// Part of the Wallet interface.
func (w *spvWallet) AccountOwnsAddress(ctx context.Context, addr stdaddr.Address, account string) (bool, error) {
	ka, err := w.KnownAddress(ctx, addr)
	if err != nil {
		if errors.Is(err, walleterrors.NotExist) {
			return false, nil
		}
		return false, fmt.Errorf("KnownAddress error: %w", err)
	}
	if ka.AccountName() != account {
		return false, nil
	}
	if kind := ka.AccountKind(); kind != wallet.AccountKindBIP0044 && kind != wallet.AccountKindImported {
		return false, nil
	}
	return true, nil
}

// AccountBalance returns the balance breakdown for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) AccountBalance(ctx context.Context, confirms int32, accountName string) (*walletjson.GetAccountBalanceResult, error) {
	bal, err := w.accountBalance(ctx, confirms, accountName)
	if err != nil {
		return nil, err
	}

	return &walletjson.GetAccountBalanceResult{
		AccountName:             accountName,
		ImmatureCoinbaseRewards: bal.ImmatureCoinbaseRewards.ToCoin(),
		ImmatureStakeGeneration: bal.ImmatureStakeGeneration.ToCoin(),
		LockedByTickets:         bal.LockedByTickets.ToCoin(),
		Spendable:               bal.Spendable.ToCoin(),
		Total:                   bal.Total.ToCoin(),
		Unconfirmed:             bal.Unconfirmed.ToCoin(),
		VotingAuthority:         bal.VotingAuthority.ToCoin(),
	}, nil
}

func (w *spvWallet) accountBalance(ctx context.Context, confirms int32, accountName string) (wallet.Balances, error) {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return wallet.Balances{}, err
	}
	return w.dcrWallet.AccountBalance(ctx, acctNum, confirms)
}

// LockedOutputs fetches locked outputs for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) LockedOutputs(ctx context.Context, accountName string) ([]chainjson.TransactionInput, error) {
	return w.dcrWallet.LockedOutpoints(ctx, accountName)
}

// Unspents fetches unspent outputs for the specified account.
// Part of the Wallet interface.
func (w *spvWallet) Unspents(ctx context.Context, accountName string) ([]*walletjson.ListUnspentResult, error) {
	return w.dcrWallet.ListUnspent(ctx, 0, math.MaxInt32, nil, accountName)
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
func (w *spvWallet) ExternalAddress(ctx context.Context, accountName string) (stdaddr.Address, error) {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return nil, err
	}
	return w.NewExternalAddress(ctx, acctNum, wallet.WithGapPolicyWrap())
}

// InternalAddress returns an internal address using GapPolicyIgnore.
// Part of the Wallet interface.
func (w *spvWallet) InternalAddress(ctx context.Context, accountName string) (stdaddr.Address, error) {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return nil, err
	}
	return w.NewInternalAddress(ctx, acctNum, wallet.WithGapPolicyWrap())
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

// BlockTimestamp gets the timestamp of the block.
func (w *spvWallet) BlockTimestamp(ctx context.Context, blockHash *chainhash.Hash) (time.Time, error) {
	hdr, err := w.dcrWallet.BlockHeader(ctx, blockHash)
	if err != nil {
		return time.Time{}, err
	}
	return hdr.Timestamp, nil
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

	// Get next block hash unless there are none.
	var nextHash *chainhash.Hash
	confirmations := int64(-1)
	mainChainHasBlock, _, err := w.BlockInMainChain(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("error checking if block is in mainchain: %w", err)
	}
	if mainChainHasBlock {
		_, tipHeight := w.MainChainTip(ctx)
		if int32(hdr.Height) < tipHeight {
			nextHash, err = w.GetBlockHash(ctx, int64(hdr.Height)+1)
			if err != nil {
				return nil, fmt.Errorf("error getting next hash for block %q: %w", blockHash, err)
			}
		}
		if int32(hdr.Height) <= tipHeight {
			confirmations = int64(tipHeight) - int64(hdr.Height) + 1
		} else { // if tip is less, may be rolling back, so just mock dcrd/dcrwallet
			confirmations = 0
		}
	}

	return &BlockHeader{
		BlockHeader:   hdr,
		MedianTime:    medianTime,
		Confirmations: confirmations,
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
func (w *spvWallet) AccountUnlocked(ctx context.Context, accountName string) (bool, error) {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return false, err
	}
	return w.dcrWallet.AccountUnlocked(ctx, acctNum)
}

// LockAccount locks the specified account.
// Part of the Wallet interface.
func (w *spvWallet) LockAccount(ctx context.Context, accountName string) error {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return err
	}
	return w.dcrWallet.LockAccount(ctx, acctNum)
}

// UnlockAccount unlocks the specified account or the wallet if account is not
// encrypted. Part of the Wallet interface.
func (w *spvWallet) UnlockAccount(ctx context.Context, pw []byte, accountName string) error {
	acctNum, err := w.dcrWallet.AccountNumber(ctx, accountName)
	if err != nil {
		return err
	}
	return w.dcrWallet.UnlockAccount(ctx, acctNum, pw)
}

func (w *spvWallet) upgradeAccounts(ctx context.Context, pw []byte) error {
	ew, ok := w.dcrWallet.(*extendedWallet)
	if !ok {
		return nil // assume the accts exist, since we can't verify
	}

	if err := ew.Unlock(ctx, pw, nil); err != nil {
		return fmt.Errorf("cannot unlock wallet to check mixing accts: %v", err)
	}
	defer ew.Lock()

	if err := setupMixingAccounts(ctx, ew.Wallet, pw); err != nil {
		return err
	}
	return nil
}

// SyncStatus returns the wallet's sync status.
// Part of the Wallet interface.
func (w *spvWallet) SyncStatus(ctx context.Context) (*asset.SyncStatus, error) {
	ss := new(asset.SyncStatus)

	targetHeight := w.bestPeerInitialHeight()
	if targetHeight == 0 {
		return ss, nil
	}
	ss.TargetHeight = uint64(targetHeight)

	_, height := w.dcrWallet.MainChainTip(ctx)
	if height == 0 {
		return ss, nil
	}
	height = utils.Clamp(height, 0, targetHeight)
	ss.Blocks = uint64(height)

	ss.Synced, _ = w.spv.Synced(ctx)

	if rescanHash, err := w.dcrWallet.RescanPoint(ctx); err != nil {
		return nil, fmt.Errorf("error getting rescan point: %w", err)
	} else if rescanHash != nil {
		rescanHeader, err := w.dcrWallet.BlockHeader(ctx, rescanHash)
		if err != nil {
			return nil, fmt.Errorf("error getting rescan point header: %w", err)
		}
		h := uint64(utils.Clamp(rescanHeader.Height, 1, uint32(targetHeight)+1) - 1)
		ss.Transactions = &h
	}

	return ss, nil
}

// bestPeerInitialHeight is the highest InitialHeight recorded from connected
// spv peers. If no peers are connected, the last observed max peer height is
// returned.
func (w *spvWallet) bestPeerInitialHeight() int32 {
	peers := w.spv.GetRemotePeers()
	if len(peers) == 0 {
		return atomic.LoadInt32(&w.bestSpvPeerHeight)
	}

	var bestHeight int32
	for _, p := range peers {
		if h := p.InitialHeight(); h > bestHeight {
			bestHeight = h
		}
	}
	atomic.StoreInt32(&w.bestSpvPeerHeight, bestHeight)
	return bestHeight
}

// AddressPrivKey fetches the privkey for the specified address.
// Part of the Wallet interface.
func (w *spvWallet) AddressPrivKey(ctx context.Context, addr stdaddr.Address) (*secp256k1.PrivateKey, error) {
	privKey, _, err := w.dcrWallet.LoadPrivateKey(ctx, addr)
	return privKey, err
}

// StakeInfo returns the current stake info.
func (w *spvWallet) StakeInfo(ctx context.Context) (*wallet.StakeInfoData, error) {
	return w.dcrWallet.StakeInfo(ctx)
}

func (w *spvWallet) newVSPClient(vspHost, vspPubKey string, log dex.Logger) (*vspclient.Client, error) {
	return vspclient.New(vspclient.Config{
		URL:    vspHost,
		PubKey: vspPubKey,
		Dialer: new(net.Dialer).DialContext,
		Wallet: w.dcrWallet.(*extendedWallet).Wallet,
		Policy: &vspclient.Policy{
			MaxFee:     0.2e8,
			FeeAcct:    0,
			ChangeAcct: 0,
		},
		Params: w.chainParams,
	}, log)
}

// rescan performs a blocking rescan, sending updates on the channel.
func (w *spvWallet) rescan(ctx context.Context, fromHeight int32, c chan wallet.RescanProgress) {
	w.dcrWallet.RescanProgressFromHeight(ctx, w.spv, fromHeight, c)
}

// PurchaseTickets purchases n tickets, tells the provided vspd to monitor the
// ticket, and pays the vsp fee.
func (w *spvWallet) PurchaseTickets(ctx context.Context, n int, vspHost, vspPubKey string, mixing bool) ([]*asset.Ticket, error) {
	vspClient, err := w.newVSPClient(vspHost, vspPubKey, w.log.SubLogger("VSP"))
	if err != nil {
		return nil, err
	}

	req := &wallet.PurchaseTicketsRequest{
		Count:                n,
		VSPFeePaymentProcess: vspClient.Process,
		VSPFeePercent:        vspClient.FeePercentage,
		Mixing:               mixing,
	}

	if mixing {
		accts := w.Accounts()
		mixedAccountNum, err := w.AccountNumber(ctx, accts.PrimaryAccount)
		if err != nil {
			return nil, fmt.Errorf("error getting mixed account number: %w", err)
		}
		req.SourceAccount = mixedAccountNum
		// For simnet, we just change the source account. Others we need to
		// mix tickets through the cspp server.
		if w.chainParams.Net != wire.SimNet {
			req.MixedAccount = mixedAccountNum
			req.MixedAccountBranch = mixedAccountBranch
			req.MixedSplitAccount = req.MixedAccount
			req.ChangeAccount, err = w.AccountNumber(ctx, accts.UnmixedAccount)
			if err != nil {
				return nil, fmt.Errorf("error getting mixed change account number: %w", err)
			}
		}
	}

	res, err := w.dcrWallet.PurchaseTickets(ctx, w.spv, req)
	if err != nil {
		return nil, err
	}

	tickets := make([]*asset.Ticket, len(res.TicketHashes))
	for i, h := range res.TicketHashes {
		w.log.Debugf("Purchased ticket %s", h)
		ticketSummary, hdr, err := w.dcrWallet.GetTicketInfo(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("error fetching info for new ticket")
		}
		ticket := ticketSummaryToAssetTicket(ticketSummary, hdr, w.log)
		if ticket == nil {
			return nil, fmt.Errorf("invalid ticket summary for %s", h)
		}
		tickets[i] = ticket
	}
	return tickets, err
}

const (
	upperHeightMempool   = -1
	lowerHeightAutomatic = -1
	pageSizeUnlimited    = 0
)

// Tickets returns current active tickets.
func (w *spvWallet) Tickets(ctx context.Context) ([]*asset.Ticket, error) {
	return w.ticketsInRange(ctx, lowerHeightAutomatic, upperHeightMempool, pageSizeUnlimited, 0)
}

var _ ticketPager = (*spvWallet)(nil)

func (w *spvWallet) TicketPage(ctx context.Context, scanStart int32, n, skipN int) ([]*asset.Ticket, error) {
	if scanStart == -1 {
		_, scanStart = w.MainChainTip(ctx)
	}
	return w.ticketsInRange(ctx, 0, scanStart, n, skipN)
}

func (w *spvWallet) ticketsInRange(ctx context.Context, lowerHeight, upperHeight int32, maxN, skipN /* 0 = mempool */ int) ([]*asset.Ticket, error) {
	p := w.chainParams
	var startBlock, endBlock *wallet.BlockIdentifier // null endBlock goes through mempool
	// If mempool is included, there is no way to scan backwards.
	includeMempool := upperHeight == upperHeightMempool
	if includeMempool {
		_, upperHeight = w.MainChainTip(ctx)
	} else {
		endBlock = wallet.NewBlockIdentifierFromHeight(upperHeight)
	}
	if lowerHeight == lowerHeightAutomatic {
		bn := upperHeight - int32(p.TicketExpiry+uint32(p.TicketMaturity))
		startBlock = wallet.NewBlockIdentifierFromHeight(bn)
	} else {
		startBlock = wallet.NewBlockIdentifierFromHeight(lowerHeight)
	}

	// If not looking at mempool, we can reverse iteration order by swapping
	// start and end blocks.
	if endBlock != nil {
		startBlock, endBlock = endBlock, startBlock
	}

	tickets := make([]*asset.Ticket, 0)
	var skipped int
	processTicket := func(ticketSummaries []*wallet.TicketSummary, hdr *wire.BlockHeader) (bool, error) {
		for _, ticketSummary := range ticketSummaries {
			if skipped < skipN {
				skipped++
				continue
			}
			if ticket := ticketSummaryToAssetTicket(ticketSummary, hdr, w.log); ticket != nil {
				tickets = append(tickets, ticket)
			}

			if maxN > 0 && len(tickets) >= maxN {
				return true, nil
			}
		}

		return false, nil
	}

	if err := w.dcrWallet.GetTickets(ctx, processTicket, startBlock, endBlock); err != nil {
		return nil, err
	}

	// If this is a mempool scan, we cannot scan backwards, so reverse the
	// result order.
	if includeMempool {
		utils.ReverseSlice(tickets)
	}

	return tickets, nil
}

// VotingPreferences returns current voting preferences.
func (w *spvWallet) VotingPreferences(ctx context.Context) ([]*walletjson.VoteChoice, []*asset.TBTreasurySpend, []*walletjson.TreasuryPolicyResult, error) {
	_, agendas := wallet.CurrentAgendas(w.chainParams)

	choices, _, err := w.dcrWallet.AgendaChoices(ctx, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to get agenda choices: %v", err)
	}

	voteChoices := make([]*walletjson.VoteChoice, len(choices))

	i := 0
	for agendaID, choiceID := range choices {
		voteChoices[i] = &walletjson.VoteChoice{
			AgendaID: agendaID,
			ChoiceID: choiceID,
		}
		for _, agenda := range agendas {
			if agenda.Vote.Id != agendaID {
				continue
			}
			voteChoices[i].AgendaDescription = agenda.Vote.Description
			for _, choice := range agenda.Vote.Choices {
				if choiceID == choice.Id {
					voteChoices[i].ChoiceDescription = choice.Description
					break
				}
			}
		}
		i++
	}
	policyToStr := func(p stake.TreasuryVoteT) string {
		var policy string
		switch p {
		case stake.TreasuryVoteYes:
			policy = "yes"
		case stake.TreasuryVoteNo:
			policy = "no"
		}
		return policy
	}
	tspends := w.dcrWallet.GetAllTSpends(ctx)
	tSpendPolicy := make([]*asset.TBTreasurySpend, 0, len(tspends))
	for i := range tspends {
		msgTx := tspends[i]
		tspendHash := msgTx.TxHash()
		var val uint64
		for _, txOut := range msgTx.TxOut {
			val += uint64(txOut.Value)
		}
		p := w.dcrWallet.TSpendPolicy(&tspendHash, nil)
		tSpendPolicy = append(tSpendPolicy, &asset.TBTreasurySpend{
			Hash:          tspendHash.String(),
			CurrentPolicy: policyToStr(p),
			Value:         val,
		})
	}

	policies := w.dcrWallet.TreasuryKeyPolicies()
	treasuryPolicy := make([]*walletjson.TreasuryPolicyResult, 0, len(policies))
	for i := range policies {
		r := walletjson.TreasuryPolicyResult{
			Key:    hex.EncodeToString(policies[i].PiKey),
			Policy: policyToStr(policies[i].Policy),
		}
		if policies[i].Ticket != nil {
			r.Ticket = policies[i].Ticket.String()
		}
		treasuryPolicy = append(treasuryPolicy, &r)
	}

	return voteChoices, tSpendPolicy, treasuryPolicy, nil
}

// SetVotingPreferences sets voting preferences for the wallet and for vsps with
// active tickets.
func (w *spvWallet) SetVotingPreferences(ctx context.Context, choices, tspendPolicy,
	treasuryPolicy map[string]string) error {
	// Set the consensus vote choices for the wallet.
	if len(choices) > 0 {
		_, err := w.SetAgendaChoices(ctx, nil, choices)
		if err != nil {
			return err
		}
	}
	strToPolicy := func(s, t string) (stake.TreasuryVoteT, error) {
		var policy stake.TreasuryVoteT
		switch s {
		case "abstain", "invalid", "":
			policy = stake.TreasuryVoteInvalid
		case "yes":
			policy = stake.TreasuryVoteYes
		case "no":
			policy = stake.TreasuryVoteNo
		default:
			return 0, fmt.Errorf("unknown %s policy %q", t, s)
		}
		return policy, nil
	}
	// Set the tspend policy for the wallet.
	for k, v := range tspendPolicy {
		if len(k) != chainhash.MaxHashStringSize {
			return fmt.Errorf("invalid tspend hash length, expected %d got %d",
				chainhash.MaxHashStringSize, len(k))
		}
		hash, err := chainhash.NewHashFromStr(k)
		if err != nil {
			return fmt.Errorf("invalid hash %s: %v", k, err)
		}
		policy, err := strToPolicy(v, "tspend")
		if err != nil {
			return err
		}
		err = w.dcrWallet.SetTSpendPolicy(ctx, hash, policy, nil)
		if err != nil {
			return err
		}
	}
	// Set the treasury policy for the wallet.
	for k, v := range treasuryPolicy {
		pikey, err := hex.DecodeString(k)
		if err != nil {
			return fmt.Errorf("unable to decode pi key %s: %v", k, err)
		}
		if len(pikey) != secp256k1.PubKeyBytesLenCompressed {
			return fmt.Errorf("treasury key %s must be 33 bytes", k)
		}
		policy, err := strToPolicy(v, "treasury")
		if err != nil {
			return err
		}
		err = w.dcrWallet.SetTreasuryKeyPolicy(ctx, pikey, policy, nil)
		if err != nil {
			return err
		}
	}
	clientCache := make(map[string]*vspclient.Client)
	// Set voting preferences for VSPs. Continuing for all errors.
	// NOTE: Doing this in an unmetered loop like this is a privacy breaker.
	return w.dcrWallet.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		vspHost, err := w.dcrWallet.VSPHostForTicket(ctx, hash)
		if err != nil {
			if errors.Is(err, walleterrors.NotExist) {
				w.log.Warnf("ticket %s is not associated with a VSP", hash)
				return nil
			}
			w.log.Warnf("unable to get VSP associated with ticket %s: %v", hash, err)
			return nil
		}
		vspClient, have := clientCache[vspHost]
		if !have {
			info, err := vspInfo(ctx, vspHost)
			if err != nil {
				w.log.Warnf("unable to get info from vsp at %s for ticket %s: %v", vspHost, hash, err)
				return nil
			}
			vspPubKey := base64.StdEncoding.EncodeToString(info.PubKey)
			vspClient, err = w.newVSPClient(vspHost, vspPubKey, w.log.SubLogger("VSP"))
			if err != nil {
				w.log.Warnf("unable to load vsp at %s for ticket %s: %v", vspHost, hash, err)
				return nil
			}
		}
		// Never return errors here, so all tickets are tried.
		// The first error will be returned to the user.
		vspTicket, err := w.NewVSPTicket(ctx, hash)
		if err != nil {
			w.log.Warnf("unable to create vsp ticket for vsp at %s for ticket %s: %v", vspHost, hash, err)
		}
		err = vspClient.SetVoteChoice(ctx, vspTicket, choices, tspendPolicy, treasuryPolicy)
		if err != nil {
			w.log.Warnf("unable to set vote for vsp at %s for ticket %s: %v", vspHost, hash, err)
		}
		return nil
	})
}

func (w *spvWallet) ListSinceBlock(ctx context.Context, start int32) ([]ListTransactionsResult, error) {
	res := make([]ListTransactionsResult, 0)
	f := func(block *wallet.Block) (bool, error) {
		for _, tx := range block.Transactions {
			convertTxType := func(txType wallet.TransactionType) *walletjson.ListTransactionsTxType {
				switch txType {
				case wallet.TransactionTypeTicketPurchase:
					txType := walletjson.LTTTTicket
					return &txType
				case wallet.TransactionTypeVote:
					txType := walletjson.LTTTVote
					return &txType
				case wallet.TransactionTypeRevocation:
					txType := walletjson.LTTTRevocation
					return &txType
				case wallet.TransactionTypeCoinbase:
				case wallet.TransactionTypeRegular:
					txType := walletjson.LTTTRegular
					return &txType
				}
				w.log.Warnf("unknown transaction type %v", tx.Type)
				regularTxType := walletjson.LTTTRegular
				return &regularTxType
			}
			fee := tx.Fee.ToUnit(dcrutil.AmountCoin)
			var blockIndex, blockTime int64
			if block.Header != nil {
				blockIndex = int64(block.Header.Height)
				blockTime = block.Header.Timestamp.Unix()
			}
			res = append(res, ListTransactionsResult{
				TxID:       tx.Hash.String(),
				BlockIndex: &blockIndex,
				BlockTime:  blockTime,
				Send:       len(tx.MyInputs) > 0,
				TxType:     convertTxType(tx.Type),
				Fee:        &fee,
			})
		}
		return false, nil
	}

	startID := wallet.NewBlockIdentifierFromHeight(start)
	return res, w.dcrWallet.GetTransactions(ctx, f, startID, nil)
}

func (w *spvWallet) SetTxFee(_ context.Context, feePerKB dcrutil.Amount) error {
	w.dcrWallet.SetRelayFee(feePerKB)
	return nil
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

func newWalletConfig(db wallet.DB, chainParams *chaincfg.Params, gapLimit uint32) *wallet.Config {
	if gapLimit < wallet.DefaultGapLimit {
		gapLimit = wallet.DefaultGapLimit
	}
	return &wallet.Config{
		DB:              db,
		GapLimit:        gapLimit,
		AccountGapLimit: defaultAccountGapLimit,
		ManualTickets:   defaultManualTickets,
		AllowHighFees:   defaultAllowHighFees,
		RelayFee:        defaultRelayFeePerKb,
		Params:          chainParams,
		MixSplitLimit:   defaultMixSplitLimit,
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
// TODO: See if the below precaution is even necessary for dcrwallet. In theory,
// the the rotating file logger must be Close'd at some point, but there are
// concurrency issues with that since btcd and btcwallet have unsupervised
// goroutines still running after shutdown. So we leave the rotator running at
// the risk of losing some logs.
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

func ticketSummaryToAssetTicket(ticketSummary *wallet.TicketSummary, hdr *wire.BlockHeader, log dex.Logger) *asset.Ticket {
	spender := ""
	if ticketSummary.Spender != nil {
		spender = ticketSummary.Spender.Hash.String()
	}

	if ticketSummary.Ticket == nil || len(ticketSummary.Ticket.MyOutputs) < 1 {
		log.Errorf("No zeroth output")
		return nil
	}

	var blockHeight int64 = -1
	if hdr != nil {
		blockHeight = int64(hdr.Height)
	}

	return &asset.Ticket{
		Tx: asset.TicketTransaction{
			Hash:        ticketSummary.Ticket.Hash.String(),
			TicketPrice: uint64(ticketSummary.Ticket.MyOutputs[0].Amount),
			Fees:        uint64(ticketSummary.Ticket.Fee),
			Stamp:       uint64(ticketSummary.Ticket.Timestamp),
			BlockHeight: blockHeight,
		},
		Status:  asset.TicketStatus(ticketSummary.Status),
		Spender: spender,
	}
}
