// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var errWalletNotConnected = errors.New("wallet not connected")

// runWithTimeout runs the provided function, returning either the error from
// the function or errTimeout if the function fails to return within the
// timeout. This function is for wallet methods that may not have a context or
// timeout of their own, or we simply cannot rely on third party packages to
// respect context cancellation or deadlines.
func runWithTimeout(f func() error, timeout time.Duration) error {
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		errChan <- f()
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(timeout):
		return errTimeout
	}
}

// xcWallet is a wallet. Use (*Core).loadWallet to construct a xcWallet.
type xcWallet struct {
	asset.Wallet
	connector         *dex.ConnectionMaster
	AssetID           uint32
	Symbol            string
	version           uint32
	supportedVersions []uint32
	dbID              []byte
	walletType        string
	traits            asset.WalletTrait
	parent            *xcWallet

	mtx          sync.RWMutex
	encPass      []byte // empty means wallet not password protected
	balance      *WalletBalance
	pw           encode.PassBytes
	address      string
	peerCount    int32  // -1 means no count yet
	monitored    uint32 // startWalletSyncMonitor goroutines monitoring sync status
	hookedUp     bool
	synced       bool
	syncProgress float32
	disabled     bool

	// When wallets are being reconfigured and especially when the wallet type
	// or host is being changed, we want to suppress "walletstate" notes to
	// prevent subscribers from prematurely adopting the new WalletState before
	// the new wallet is fully validated and added to the Core wallets map.
	// WalletState notes during reconfiguration can come from the sync loop or
	// from the PeersChange callback.
	broadcasting *uint32
}

// encPW returns xcWallet's encrypted password.
func (w *xcWallet) encPW() []byte {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.encPass
}

// setEncPW sets xcWallet's encrypted password.
func (w *xcWallet) setEncPW(encPW []byte) {
	w.mtx.Lock()
	w.encPass = encPW
	w.mtx.Unlock()
}

func (w *xcWallet) supportsVer(ver uint32) bool {
	for _, v := range w.supportedVersions {
		if v == ver {
			return true
		}
	}
	return false
}

// Unlock unlocks the wallet backend and caches the decrypted wallet password so
// the wallet may be unlocked without user interaction using refreshUnlock.
func (w *xcWallet) Unlock(crypter encrypt.Crypter) error {
	if w.isDisabled() { // cannot unlock disabled wallet.
		return fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if w.parent != nil {
		return w.parent.Unlock(crypter)
	}
	if !w.connected() {
		return errWalletNotConnected
	}

	a, is := w.Wallet.(asset.Authenticator)
	if !is {
		return nil
	}

	if len(w.encPW()) == 0 {
		if a.Locked() {
			return fmt.Errorf("wallet reporting as locked, but no password has been set")
		}
		return nil
	}
	pw, err := crypter.Decrypt(w.encPW())
	if err != nil {
		return fmt.Errorf("%s unlockWallet decryption error: %w", unbip(w.AssetID), err)
	}
	err = a.Unlock(pw) // can be slow - no timeout and NOT in the critical section!
	if err != nil {
		return err
	}
	w.mtx.Lock()
	w.pw = pw
	w.mtx.Unlock()
	return nil
}

// refreshUnlock is used to ensure the wallet is unlocked. If the wallet backend
// reports as already unlocked, which includes a wallet with no password
// protection, no further action is taken and a nil error is returned. If the
// wallet is reporting as locked, and the wallet is not known to be password
// protected (no encPW set) or the decrypted password is not cached, a non-nil
// error is returned. If no encrypted password is set, the xcWallet is
// misconfigured and should be recreated. If the decrypted password is not
// stored, the Unlock method should be used to decrypt the password. Finally, a
// non-nil error will be returned if the cached password fails to unlock the
// wallet, in which case unlockAttempted will also be true.
func (w *xcWallet) refreshUnlock() (unlockAttempted bool, err error) {
	if w.isDisabled() { // disabled wallet cannot be unlocked.
		return false, fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if w.parent != nil {
		return w.parent.refreshUnlock()
	}
	if !w.connected() {
		return false, errWalletNotConnected
	}

	a, is := w.Wallet.(asset.Authenticator)
	if !is {
		return false, nil
	}

	// Check if the wallet backend is already unlocked.
	if !a.Locked() {
		return false, nil // unlocked
	}

	// Locked backend requires both encrypted and decrypted passwords.
	w.mtx.RLock()
	pwUnset := len(w.encPass) == 0
	locked := len(w.pw) == 0
	w.mtx.RUnlock()
	if pwUnset {
		return false, fmt.Errorf("%s wallet reporting as locked but no password"+
			" has been set", unbip(w.AssetID))
	}
	if locked {
		return false, fmt.Errorf("cannot refresh unlock on a locked %s wallet",
			unbip(w.AssetID))
	}

	return true, a.Unlock(w.pw)
}

// Lock the wallet. For encrypted wallets (encPW set), this clears the cached
// decrypted password and attempts to lock the wallet backend.
func (w *xcWallet) Lock(timeout time.Duration) error {
	a, is := w.Wallet.(asset.Authenticator)
	if w.isDisabled() || !is { // wallet is disabled and is locked or it's not an authenticator.
		return nil
	}
	if w.parent != nil {
		return w.parent.Lock(timeout)
	}
	w.mtx.Lock()
	if !w.hookedUp {
		w.mtx.Unlock()
		return errWalletNotConnected
	}
	if len(w.encPass) == 0 {
		w.mtx.Unlock()
		return nil
	}
	w.pw.Clear()
	w.pw = nil
	w.mtx.Unlock() // end critical section before actual wallet request

	return runWithTimeout(a.Lock, timeout)
}

// unlocked will only return true if both the wallet backend is unlocked and we
// have cached the decryped wallet password. The wallet backend may be queried
// directly, likely involving an RPC call. Use locallyUnlocked to determine if
// the wallet is automatically unlockable rather than actually unlocked.
func (w *xcWallet) unlocked() bool {
	if w.isDisabled() {
		return false
	}
	a, is := w.Wallet.(asset.Authenticator)
	if !is {
		return w.locallyUnlocked()
	}
	if w.parent != nil {
		return w.parent.unlocked()
	}
	if !w.connected() {
		return false
	}
	return w.locallyUnlocked() && !a.Locked()
}

// locallyUnlocked checks whether we think the wallet is unlocked, but without
// asking the wallet itself. More precisely, for encrypted wallets (encPW set)
// this is true only if the decrypted password is cached. Use this to determine
// if the wallet may be unlocked without user interaction (via refreshUnlock).
func (w *xcWallet) locallyUnlocked() bool {
	if w.isDisabled() {
		return false
	}
	if w.parent != nil {
		return w.parent.locallyUnlocked()
	}
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	if len(w.encPass) == 0 {
		return true // unencrypted wallet
	}
	return len(w.pw) > 0 // cached password for encrypted wallet
}

func (w *xcWallet) unitInfo() dex.UnitInfo {
	return w.Info().UnitInfo
}

func (w *xcWallet) amtString(amt uint64) string {
	ui := w.unitInfo()
	return fmt.Sprintf("%s %s", ui.ConventionalString(amt), ui.Conventional.Unit)
}

func (w *xcWallet) amtStringSigned(amt int64) string {
	if amt >= 0 {
		return w.amtString(uint64(amt))
	}
	return "-" + w.amtString(uint64(-amt))
}

// state returns the current WalletState.
func (w *xcWallet) state() *WalletState {
	winfo := w.Info()

	w.mtx.RLock()
	var peerCount uint32
	if w.peerCount > 0 { // initialized to -1 initially, means no count yet
		peerCount = uint32(w.peerCount)
		if mixer, ok := w.Wallet.(asset.FundsMixer); ok {
			stats, _ := mixer.FundsMixingStats(context.TODO())
			fmt.Println(stats)
		}
	}

	var tokenApprovals map[uint32]asset.ApprovalStatus
	if w.connector.On() {
		tokenApprovals = w.ApprovalStatus()
	}

	state := &WalletState{
		Symbol:       unbip(w.AssetID),
		AssetID:      w.AssetID,
		Version:      winfo.Version,
		Open:         len(w.encPass) == 0 || len(w.pw) > 0,
		Running:      w.connector.On(),
		Balance:      w.balance,
		Address:      w.address,
		Units:        winfo.UnitInfo.AtomicUnit,
		Encrypted:    len(w.encPass) > 0,
		PeerCount:    peerCount,
		Synced:       w.synced,
		SyncProgress: w.syncProgress,
		WalletType:   w.walletType,
		Traits:       w.traits,
		Disabled:     w.disabled,
		Approved:     tokenApprovals,
	}
	w.mtx.RUnlock()

	if w.parent != nil {
		w.parent.mtx.RLock()
		state.Open = len(w.parent.encPass) == 0 || len(w.parent.pw) > 0
		w.parent.mtx.RUnlock()
	}

	return state
}

// setBalance sets the wallet balance.
func (w *xcWallet) setBalance(bal *WalletBalance) {
	w.mtx.Lock()
	w.balance = bal
	w.mtx.Unlock()
}

// setDisabled sets the wallet disabled field.
func (w *xcWallet) setDisabled(status bool) {
	w.mtx.Lock()
	w.disabled = status
	w.mtx.Unlock()
}

func (w *xcWallet) isDisabled() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.disabled
}

func (w *xcWallet) currentDepositAddress() string {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.address
}

func (w *xcWallet) refreshDepositAddress() (string, error) {
	if !w.connected() {
		return "", fmt.Errorf("cannot get address from unconnected %s wallet",
			unbip(w.AssetID))
	}

	na, is := w.Wallet.(asset.NewAddresser)
	if !is {
		return "", fmt.Errorf("wallet does not generate new addresses")
	}

	addr, err := na.NewAddress()
	if err != nil {
		return "", fmt.Errorf("%s Wallet.Address error: %w", unbip(w.AssetID), err)
	}

	w.mtx.Lock()
	w.address = addr
	w.mtx.Unlock()

	return addr, nil
}

// connected is true if the wallet has already been connected.
func (w *xcWallet) connected() bool {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	return w.hookedUp
}

// checkPeersAndSyncStatus checks that the wallet is synced, and has peers
// otherwise we might double spend if the wallet keys were used elsewhere. This
// should be checked before attempting to send funds but does not replace any
// other checks that may be required.
func (w *xcWallet) checkPeersAndSyncStatus() error {
	w.mtx.RLock()
	defer w.mtx.RUnlock()
	if w.peerCount < 1 {
		return fmt.Errorf("%s wallet has no connected peers", unbip(w.AssetID))
	}
	if !w.synced {
		return fmt.Errorf("%s wallet is not synchronized", unbip(w.AssetID))
	}
	return nil
}

// Connect calls the dex.Connector's Connect method, sets the xcWallet.hookedUp
// flag to true, and validates the deposit address. Use Disconnect to cleanly
// shutdown the wallet.
func (w *xcWallet) Connect() error {
	// Disabled wallet cannot be connected to unless it is enabled.
	if w.isDisabled() {
		return fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}

	// No parent context; use Disconnect instead. Also note that there's no
	// reconnect loop for wallet like with the server Connectors, so we use
	// ConnectOnce so that the ConnectionMaster's On method will report false.
	err := w.connector.ConnectOnce(context.Background())
	if err != nil {
		return err
	}

	var ready bool
	defer func() {
		// Now that we are connected, we must Disconnect if any calls fail below
		// since we are considering this wallet not "hookedUp".
		if !ready {
			w.connector.Disconnect()
		}
	}()

	synced, progress, err := w.SyncStatus()
	if err != nil {
		return err
	}

	w.mtx.Lock()
	defer w.mtx.Unlock()
	haveAddress := w.address != ""
	if haveAddress {
		if haveAddress, err = w.OwnsDepositAddress(w.address); err != nil {
			return err
		}
	}
	if !haveAddress {
		if w.address, err = w.DepositAddress(); err != nil {
			return err
		}
	}
	w.hookedUp = true
	w.synced = synced
	w.syncProgress = progress // updated in walletCheckAndNotify
	ready = true

	return nil
}

// Disconnect calls the dex.Connector's Disconnect method and sets the
// xcWallet.hookedUp flag to false.
func (w *xcWallet) Disconnect() {
	// Disabled wallet is already disconnected.
	if w.isDisabled() {
		return
	}
	w.connector.Disconnect()
	w.mtx.Lock()
	w.hookedUp = false
	w.mtx.Unlock()
}

// rescan will initiate a rescan of the wallet if the asset.Wallet
// implementation is a Rescanner.
func (w *xcWallet) rescan(ctx context.Context) error {
	if !w.connected() {
		return errWalletNotConnected
	}
	rescanner, ok := w.Wallet.(asset.Rescanner)
	if !ok {
		return errors.New("wallet does not support rescanning")
	}
	return rescanner.Rescan(ctx)
}

// logFilePath returns the path of the wallet's log file if the
// asset.Wallet implementation is a LogFiler.
func (w *xcWallet) logFilePath() (string, error) {
	logFiler, ok := w.Wallet.(asset.LogFiler)
	if !ok {
		return "", errors.New("wallet does not support getting log file")
	}
	return logFiler.LogFilePath(), nil
}

// accelerateOrder uses the Child-Pays-For-Parent technique to accelerate an
// order if the wallet is an Accelerator.
func (w *xcWallet) accelerateOrder(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, newFeeRate uint64) (asset.Coin, string, error) {
	if w.isDisabled() { // cannot perform order acceleration with disabled wallet.
		return nil, "", fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if !w.connected() {
		return nil, "", errWalletNotConnected
	}
	accelerator, ok := w.Wallet.(asset.Accelerator)
	if !ok {
		return nil, "", errors.New("wallet does not support acceleration")
	}
	return accelerator.AccelerateOrder(swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
}

// accelerationEstimate estimates the cost to accelerate an order if the wallet
// is an Accelerator.
func (w *xcWallet) accelerationEstimate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, error) {
	if w.isDisabled() { // cannot perform acceleration estimate with disabled wallet.
		return 0, fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if !w.connected() {
		return 0, errWalletNotConnected
	}
	accelerator, ok := w.Wallet.(asset.Accelerator)
	if !ok {
		return 0, errors.New("wallet does not support acceleration")
	}

	return accelerator.AccelerationEstimate(swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, feeSuggestion)
}

// preAccelerate gives the user information about accelerating an order if the
// wallet is an Accelerator.
func (w *xcWallet) preAccelerate(swapCoins, accelerationCoins []dex.Bytes, changeCoin dex.Bytes, requiredForRemainingSwaps, feeSuggestion uint64) (uint64, *asset.XYRange, *asset.EarlyAcceleration, error) {
	if w.isDisabled() { // cannot perform operation with disabled wallet.
		return 0, &asset.XYRange{}, nil, fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if !w.connected() {
		return 0, &asset.XYRange{}, nil, errWalletNotConnected
	}
	accelerator, ok := w.Wallet.(asset.Accelerator)
	if !ok {
		return 0, &asset.XYRange{}, nil, errors.New("wallet does not support acceleration")
	}

	return accelerator.PreAccelerate(swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, feeSuggestion)
}

// swapConfirmations calls (asset.Wallet).SwapConfirmations with a timeout
// Context. If the coin cannot be located, an asset.CoinNotFoundError is
// returned. If the coin is located, but recognized as spent, no error is
// returned.
func (w *xcWallet) swapConfirmations(ctx context.Context, coinID []byte, contract []byte, matchTime uint64) (uint32, bool, error) {
	if w.isDisabled() { // cannot check swap confirmation with disabled wallet.
		return 0, false, fmt.Errorf(walletDisabledErrStr, strings.ToUpper(unbip(w.AssetID)))
	}
	if !w.connected() {
		return 0, false, errWalletNotConnected
	}
	return w.Wallet.SwapConfirmations(ctx, coinID, contract, time.UnixMilli(int64(matchTime)))
}

// TxHistory returns all the transactions a wallet has made. If refID
// is nil, then transactions starting from the most recent are returned
// (past is ignored). If past is true, the transactions prior to the
// refID are returned, otherwise the transactions after the refID are
// returned. n is the number of transactions to return. If n is <= 0,
// all the transactions will be returned.
func (w *xcWallet) TxHistory(n int, refID *dex.Bytes, past bool) ([]*asset.WalletTransaction, error) {
	if !w.connected() {
		return nil, errWalletNotConnected
	}

	historian, ok := w.Wallet.(asset.WalletHistorian)
	if !ok {
		return nil, fmt.Errorf("wallet does not support transaction history")
	}

	return historian.TxHistory(n, refID, past)
}

// MakeBondTx authors a DEX time-locked fidelity bond transaction if the
// asset.Wallet implementation is a Bonder.
func (w *xcWallet) MakeBondTx(ver uint16, amt, feeRate uint64, lockTime time.Time, priv *secp256k1.PrivateKey, acctID []byte) (*asset.Bond, func(), error) {
	bonder, ok := w.Wallet.(asset.Bonder)
	if !ok {
		return nil, nil, errors.New("wallet does not support making bond transactions")
	}
	return bonder.MakeBondTx(ver, amt, feeRate, lockTime, priv, acctID)
}

// BondsFeeBuffer gets the bonds fee buffer based on the provided fee rate.
func (w *xcWallet) BondsFeeBuffer(feeRate uint64) uint64 {
	bonder, ok := w.Wallet.(asset.Bonder)
	if !ok {
		return 0
	}
	return bonder.BondsFeeBuffer(feeRate)
}

// RefundBond will refund the bond if the asset.Wallet implementation is a
// Bonder. The lock time must be passed to spend the bond. LockTimeExpired
// should be used to check first.
func (w *xcWallet) RefundBond(ctx context.Context, ver uint16, coinID, script []byte, amt uint64, priv *secp256k1.PrivateKey) (asset.Coin, error) {
	bonder, ok := w.Wallet.(asset.Bonder)
	if !ok {
		return nil, errors.New("wallet does not support refunding bond transactions")
	}
	return bonder.RefundBond(ctx, ver, coinID, script, amt, priv)
}

// SendTransaction broadcasts a raw transaction if the wallet is a Broadcaster.
func (w *xcWallet) SendTransaction(tx []byte) ([]byte, error) {
	bonder, ok := w.Wallet.(asset.Broadcaster)
	if !ok {
		return nil, errors.New("wallet is not a Broadcaster")
	}
	return bonder.SendTransaction(tx)
}

// ApproveToken sends an approval transaction if the wallet is a TokenApprover.
func (w *xcWallet) ApproveToken(assetVersion uint32, onConfirm func()) (string, error) {
	approver, ok := w.Wallet.(asset.TokenApprover)
	if !ok {
		return "", fmt.Errorf("%s wallet is not a TokenApprover", unbip(w.AssetID))
	}
	return approver.ApproveToken(assetVersion, onConfirm)
}

// ApproveToken sends an approval transaction if the wallet is a TokenApprover.
func (w *xcWallet) UnapproveToken(assetVersion uint32, onConfirm func()) (string, error) {
	approver, ok := w.Wallet.(asset.TokenApprover)
	if !ok {
		return "", fmt.Errorf("%s wallet is not a TokenApprover", unbip(w.AssetID))
	}
	return approver.UnapproveToken(assetVersion, onConfirm)
}

// ApprovalFee returns the estimated fee to send an approval transaction if the
// wallet is a TokenApprover.
func (w *xcWallet) ApprovalFee(assetVersion uint32, approval bool) (uint64, error) {
	approver, ok := w.Wallet.(asset.TokenApprover)
	if !ok {
		return 0, fmt.Errorf("%s wallet is not a TokenApprover", unbip(w.AssetID))
	}
	return approver.ApprovalFee(assetVersion, approval)
}

// ApprovalStatus returns the approval status of each version of the asset if
// the wallet is a TokenApprover.
func (w *xcWallet) ApprovalStatus() map[uint32]asset.ApprovalStatus {
	approver, ok := w.Wallet.(asset.TokenApprover)
	if !ok {
		return nil
	}

	return approver.ApprovalStatus()
}

// feeRater is identical to calling w.Wallet.(asset.FeeRater).
func (w *xcWallet) feeRater() (asset.FeeRater, bool) {
	rater, is := w.Wallet.(asset.FeeRater)
	return rater, is
}
