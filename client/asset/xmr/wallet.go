package xmr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"decred.org/dcrdex/client/asset"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	WalletFileName    = "dex"
	WalletKeyfileName = "dex.keys"
	MainAccountIndex  = 0
)

var errRescanning = errors.New("currently rescanning wallet")

func (r *xmrRpc) keysFileMissing() bool {
	walletKeyFile := path.Join(r.dataDir, WalletKeyfileName)
	if _, err := os.Stat(walletKeyFile); errors.Is(err, os.ErrNotExist) {
		return true
	}
	return false
}

func (r *xmrRpc) openWallet() error {
	if r.isReScanning() {
		return errRescanning
	}
	openRq := rpc.OpenWalletRequest{
		Filename: WalletFileName,
		Password: "",
	}
	return r.wallet.OpenWallet(r.ctx, &openRq)
}

func (r *xmrRpc) closeWallet() error {
	if r.isReScanning() {
		return errRescanning
	}
	return r.wallet.CloseWallet(r.ctx)
}

func (r *xmrRpc) createWallet() error {
	if r.isReScanning() {
		return errRescanning
	}
	createRq := rpc.CreateWalletRequest{
		Filename: WalletFileName,
		Password: "",
		Language: "English", // mnemonic words language
	}
	return r.wallet.CreateWallet(r.ctx, &createRq)
}

func (r *xmrRpc) getWalletHeight() (uint64, error) {
	if r.isReScanning() {
		return 0, errRescanning
	}
	ghResp, err := r.wallet.GetHeight(r.ctx)
	if err != nil {
		return 0, err
	}
	return ghResp.Height, nil
}

func (r *xmrRpc) syncStatus() (*asset.SyncStatus, error) {
	var walletSynced = false
	if r.daemonState.synchronized && !r.daemonState.busySyncing {
		walletHeight, err := r.getWalletHeight()
		if err != nil {
			return nil, err
		}
		if walletHeight == r.daemonState.height {
			walletSynced = true
		}
	}

	ss := &asset.SyncStatus{
		Synced:         walletSynced && r.daemonState.numPeers > 0,
		TargetHeight:   r.daemonState.targetHeight,
		StartingBlocks: r.daemonState.connectHeight,
		Blocks:         r.daemonState.height,
	}
	return ss, nil
}

// refresh can be used as an immediate manual alternative to auto-refresh
func (r *xmrRpc) refresh() (uint64, bool, error) {
	if r.isReScanning() {
		return 0, false, errRescanning
	}
	refreshRq := rpc.RefreshRequest{}
	refreshResp, err := r.wallet.Refresh(r.ctx, &refreshRq)
	if err != nil {
		return 0, false, err
	}
	return refreshResp.BlocksFetched, refreshResp.ReceivedMoney, nil
}

// setAutoRefresh enables auto-refresh between monerod and wallet-rpc. It is on
// by default anyway but 20s interval
func (r *xmrRpc) setAutoRefresh() error {
	autoRq := rpc.AutoRefreshRequest{
		Enable: true,
		Period: AutoRefreshInterval,
	}
	return r.wallet.AutoRefresh(r.ctx, &autoRq)
}

func (r *xmrRpc) getBalance() (uint64, uint64, error) {
	if r.isReScanning() {
		return 0, 0, errRescanning
	}
	gbRq := rpc.GetBalanceRequest{
		AccountIndex: MainAccountIndex,
	}
	gbResp, err := r.wallet.GetBalance(r.ctx, &gbRq)
	if err != nil {
		return 0, 0, err
	}
	return gbResp.Balance, gbResp.UnlockedBalance, nil
}

func (r *xmrRpc) getAddressUsage(accountIdx uint64, address string) (bool, error) {
	if r.isReScanning() {
		return false, errRescanning
	}
	gaRq := rpc.GetAddressRequest{
		AccountIndex: accountIdx,
		// AddressIndex: empty => return all subaddresses
	}
	gaResp, err := r.wallet.GetAddress(r.ctx, &gaRq)
	if err != nil {
		return false, err
	}
	for _, a := range gaResp.Addresses {
		if a.Address == address {
			return a.Used, nil
		}
	}
	return false, fmt.Errorf("address not found %s", address)
}

func (r *xmrRpc) getNewAddress(accountIdx uint64) (string, error) {
	if r.isReScanning() {
		return "", errRescanning
	}
	caRq := rpc.CreateAddressRequest{
		AccountIndex: accountIdx,
		Count:        1,
	}
	caResp, err := r.wallet.CreateAddress(r.ctx, &caRq)
	if err != nil {
		return "", err
	}
	return caResp.Address, nil
}

func (r *xmrRpc) validateAddress(address string) bool {
	if r.isReScanning() {
		r.log.Debugf("rescanning: failed to validate address: %s", address)
		return false
	}
	valRq := rpc.ValidateAddressRequest{
		Address:        address,
		AnyNetType:     false, // belongs to the network on which the rpc-wallet's current daemon is running
		AllowOpenalias: false,
	}
	valResp, err := r.wallet.ValidateAddress(r.ctx, &valRq)
	if err != nil {
		r.log.Errorf("validateAddress - %v", err)
		return false
	}
	return valResp.Valid
}

func (r *xmrRpc) isOurAddress(accountIdx uint64, address string) (bool, error) {
	if r.isReScanning() {
		return false, errRescanning
	}
	gaRq := rpc.GetAddressRequest{
		AccountIndex: accountIdx,
		// AddressIndex: empty => return all subaddresses
	}
	gaResp, err := r.wallet.GetAddress(r.ctx, &gaRq)
	if err != nil {
		return false, err
	}
	for _, a := range gaResp.Addresses {
		if a.Address == address {
			return true, nil
		}
	}
	return false, nil
}

func (r *xmrRpc) estimateTxFeeAtoms(amount uint64, toAddress string, subtract bool, priority rpc.Priority) (uint64, error) {
	if r.isReScanning() {
		return 0, errRescanning
	}
	destinations := []rpc.Destination{
		{
			Amount:  amount,
			Address: toAddress,
		},
	}
	var subtractFeeFromOutputs []uint64 = nil // Send
	if subtract {
		subtractFeeFromOutputs = []uint64{0} // Withdraw
	}
	transferEstRq := rpc.TransferRequest{
		Destinations:           destinations,
		AccountIndex:           MainAccountIndex, // transfer from all subaddresses
		SubtractFeeFromOutputs: subtractFeeFromOutputs,
		Priority:               priority,
		RingSize:               16,    // only for stagenet, regtest
		DoNotRelay:             true,  // not default - do not broadcast
		GetTxHex:               false, // default - not needed
		GetTxMetadata:          false, // default - not needed as we will not send
	}
	transferEstResp, err := r.wallet.Transfer(r.ctx, &transferEstRq)
	if err != nil {
		r.log.Errorf("estimateTxFeeAtoms - %v", err)
		return 0, err
	}
	r.log.Debugf("estimateTxFeeAtoms - fee to send/withdraw: %d atoms to: %s with priority: %d is estimated at %s atoms",
		transferEstResp.Amount, toAddress, priority, transferEstResp.Fee)
	return transferEstResp.Fee, nil
}

func (r *xmrRpc) transferSimple(amount uint64, toAddress string, priority rpc.Priority) (string, error) {
	if r.isReScanning() {
		return "", errRescanning
	}
	// Send from account 0 from any subaddresses; to 1 destination. You do not get to pre-determine the fee.
	destinations := []rpc.Destination{
		{
			Amount:  amount,
			Address: toAddress,
		},
	}
	transferRq := rpc.TransferRequest{
		Destinations:  destinations,
		AccountIndex:  MainAccountIndex, // transfer from all subaddresses
		Priority:      priority,
		RingSize:      16,    // only for stagenet, regtest
		UnlockTime:    0,     // no spend lock
		GetTxHex:      true,  // not default and needed later
		GetTxMetadata: false, // default
	}
	transferResp, err := r.wallet.Transfer(r.ctx, &transferRq)
	if err != nil {
		r.log.Errorf("transferSimple - %v", err)
		return "", err
	}
	r.log.Debugf("transferSimple - sent: %d atoms to: %s fee: %d tx hash: %s",
		transferResp.Amount, toAddress, transferResp.Fee, transferResp.TxHash)
	return transferResp.TxHash, nil
}

func (r *xmrRpc) withdrawSimple(toAddress string, value uint64, priority rpc.Priority) (string, error) {
	if r.isReScanning() {
		return "", errRescanning
	}
	// Withdraw from account 0 from all subaddresses; to 1 destination charging the fee to the destination .
	_, unlocked, err := r.getBalance()
	if err != nil {
		return "", err
	}
	if unlocked == 0 {
		return "", fmt.Errorf("no unlocked balance")
	}
	transferRq := rpc.TransferRequest{
		Destinations: []rpc.Destination{
			{
				Address: toAddress,
				Amount:  value,
			},
		},
		AccountIndex:           MainAccountIndex, // transfer from all subaddresses
		SubtractFeeFromOutputs: []uint64{0},
		Priority:               priority,
		RingSize:               16, // only for stagenet, regtest
		UnlockTime:             0,  // no spend lock
		GetTxHex:               true,
	}
	transferResp, err := r.wallet.Transfer(r.ctx, &transferRq)
	if err != nil {
		r.log.Errorf("withdrawSimple - %v", err)
		return "", err
	}
	r.log.Debugf("withdrawSimple - sent: %d atoms as %d + fee: %d to %s.  tx hash: %s",
		value, transferResp.Amount, transferResp.Fee, toAddress, transferResp.TxHash)
	return transferResp.TxHash, nil
}

// rescanSpents checks wallet key images against daemon .. only trusted server
func (r *xmrRpc) rescanSpents(rescanCtx context.Context) error {
	r.rescanning.Store(true)
	defer r.rescanning.Store(false)
	r.daemonState.synchronized = false
	err := r.wallet.RescanSpent(rescanCtx) // synchronous
	if err != nil {
		r.log.Errorf("rescanSpents - %v", err)
		return err
	}
	return nil
}

func (r *xmrRpc) isReScanning() bool {
	return r.rescanning.Load()
}
