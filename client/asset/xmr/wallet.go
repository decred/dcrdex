package xmr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/bisoncraft/go-monero/rpc"
)

const (
	MainAccountIndex  = 0
	GetVersionTimeout = 3 * time.Second
)

const (
	CheckpointMainnet  = 3375700
	CheckpointStagenet = 550000
)

var errSyncing = errors.New("currently syncing wallet")

// walletFilesMissing checks if either or both wallet db files are missing
func walletFilesMissing(dataDir string) bool {
	walletFile := path.Join(dataDir, WalletFileName)
	walletKeysFile := path.Join(dataDir, WalletKeysFileName)
	exists := func(dbFile string) bool {
		_, err := os.Stat(dbFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false
			}
			// other stat error
			// Overwriting these files trashes existing wallet db. Consider backup
			return true
		}
		return true
	}
	if exists(walletFile) && exists(walletKeysFile) {
		return false
	}
	return true
}

func (r *xmrRpc) openWallet(ctx context.Context, pw string) error {
	openRq := rpc.OpenWalletRequest{
		Filename: WalletFileName,
		Password: pw,
	}
	return r.wallet.OpenWallet(ctx, &openRq)
}

func (r *xmrRpc) walletServerRunning() bool {
	verCtx, cancel := context.WithTimeout(r.ctx, GetVersionTimeout)
	defer cancel()
	_, err := r.wallet.GetVersion(verCtx)
	// just assume error means RPC or net error and the process is not running
	if err != nil {
		r.log.Debugf("get_version from the wallet server: %v", err)
		return false
	}
	return true
}

func (r *xmrRpc) walletSynced() (uint64, uint64, bool, error) {
	r.daemonState.RLock()
	daemonHeight := r.daemonState.height
	r.daemonState.RUnlock()
	walletHeight, err := r.getWalletHeight()
	if err != nil {
		return 0, 0, false, err
	}
	return daemonHeight, walletHeight, walletHeight >= daemonHeight, nil
}

func (r *xmrRpc) syncStatus() (*asset.SyncStatus, error) {
	checkpoint := getCheckpoint(r.net)

	r.daemonState.RLock()
	numPeers := r.daemonState.numPeers
	r.daemonState.RUnlock()

	daemonHeight, walletHeight, walletSynced, err := r.walletSynced()
	if err != nil {
		return nil, err
	}

	if walletSynced {
		r.syncing.Store(false)
	}

	r.synclog.Debugf("syncStatus: numPeers: %d daemonHeight: %d walletHeight: %d start: %d walletSynced %v",
		numPeers, daemonHeight, walletHeight, checkpoint, walletSynced)

	ss := &asset.SyncStatus{
		Synced:         walletSynced && (numPeers > 0),
		TargetHeight:   daemonHeight,
		StartingBlocks: checkpoint,
		Blocks:         walletHeight,
	}
	return ss, nil
}

func getCheckpoint(net dex.Network) uint64 {
	var cp = uint64(0)
	switch net {
	case dex.Mainnet:
		cp = CheckpointMainnet
	case dex.Testnet:
		cp = CheckpointStagenet
	}
	return cp
}

func (r *xmrRpc) getWalletHeight() (uint64, error) {
	ghResp, err := r.wallet.GetHeight(r.ctx)
	if err != nil {
		return 0, err
	}
	return ghResp.Height, nil
}

func (r *xmrRpc) getBalance() (uint64, uint64, error) {
	if r.isSyncing() {
		return 0, 0, nil
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

func (r *xmrRpc) getPrimaryAddress() (string, error) {
	gaRq := rpc.GetAddressRequest{
		AccountIndex: 0,
		AddressIndex: []uint64{0},
	}
	gaResp, err := r.wallet.GetAddress(r.ctx, &gaRq)
	if err != nil {
		return "", err
	}
	return gaResp.Address, err
}

func (r *xmrRpc) getAddressUsage(accountIdx uint64, address string) (bool, error) {
	if r.isSyncing() {
		return false, errSyncing
	}
	gaRq := rpc.GetAddressRequest{
		AccountIndex: accountIdx,
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
	if r.isSyncing() {
		r.walletInfo.Lock()
		primaryAddress := r.walletInfo.primaryAddress
		r.walletInfo.Unlock()
		return primaryAddress, nil
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
	if r.isSyncing() {
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
	if r.isSyncing() {
		return true, nil
	}
	gaRq := rpc.GetAddressRequest{
		AccountIndex: accountIdx,
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
	if r.isSyncing() {
		return 0, errSyncing
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

func (r *xmrRpc) transferSimple(amount uint64, toAddress string, unlock uint64, priority rpc.Priority) (string, error) {
	if r.isSyncing() {
		return "", errSyncing
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
		UnlockTime:    unlock, // 0 = no spend lock for n blocks; after the 10 required confirmations
		GetTxHex:      true,   // not default and needed later
		GetTxMetadata: false,  // default
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
	if r.isSyncing() {
		return "", errSyncing
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
		UnlockTime:             0, // no spend lock
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
