// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	walletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v4/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	csppConfigFileName = "cspp_config.json"
)

var nativeAccounts = []string{defaultAccountName, mixedAccountName, tradingAccountName}

// mixingConfigFile is the structure for saving cspp server configuration to
// file.
type mixingConfigFile struct {
	LegacyOn string `json:"csppserver"`
	On       bool   `json:"on"`
}

// mixer is the settings and concurrency primitives for mixing operations.
type mixer struct {
	mtx    sync.RWMutex
	ctx    context.Context
	cancel func()
	wg     sync.WaitGroup
}

// turnOn should be called with the mtx locked.
func (m *mixer) turnOn(ctx context.Context) {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
}

// closeAndClear should be called with the mtx locked.
func (m *mixer) closeAndClear() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = nil, nil
}

// NativeWallet implements optional interfaces that are only provided by the
// built-in SPV wallet.
type NativeWallet struct {
	*ExchangeWallet
	csppConfigFilePath string
	spvw               *spvWallet

	mixer mixer
}

// NativeWallet must also satisfy the following interface(s).
var _ asset.FundsMixer = (*NativeWallet)(nil)
var _ asset.Rescanner = (*NativeWallet)(nil)

func initNativeWallet(ew *ExchangeWallet) (*NativeWallet, error) {
	spvWallet, ok := ew.wallet.(*spvWallet)
	if !ok {
		return nil, fmt.Errorf("spvwallet is required to init NativeWallet")
	}

	csppConfigFilePath := filepath.Join(spvWallet.dir, csppConfigFileName)
	cfgFileB, err := os.ReadFile(csppConfigFilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("unable to read cspp config file: %v", err)
	}

	if len(cfgFileB) > 0 {
		var cfg mixingConfigFile
		err = json.Unmarshal(cfgFileB, &cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal csppConfig: %v", err)
		}
		ew.mixing.Store(cfg.On || cfg.LegacyOn != "")
	}

	spvWallet.setAccounts(ew.mixing.Load())

	w := &NativeWallet{
		ExchangeWallet:     ew,
		spvw:               spvWallet,
		csppConfigFilePath: csppConfigFilePath,
	}
	ew.cycleMixer = func() {
		w.mixer.mtx.RLock()
		defer w.mixer.mtx.RUnlock()
		w.mixFunds()
	}

	return w, nil
}

func (w *NativeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg, err := w.ExchangeWallet.Connect(ctx)
	if err != nil {
		return nil, err
	}
	if w.mixing.Load() {
		w.startFundsMixer()
	} else {
		// Ensure any funds in the mixed account are transferred back to
		// primary.
		w.stopFundsMixer()
	}
	return wg, err
}

// ConfigureFundsMixer configures the wallet for funds mixing. Part of the
// asset.FundsMixer interface.
func (w *NativeWallet) ConfigureFundsMixer(enabled bool) (err error) {
	csppCfgBytes, err := json.Marshal(&mixingConfigFile{
		On: enabled,
	})
	if err != nil {
		return fmt.Errorf("error marshaling cspp config file: %w", err)
	}
	if err := os.WriteFile(w.csppConfigFilePath, csppCfgBytes, 0644); err != nil {
		return fmt.Errorf("error writing cspp config file: %w", err)
	}

	if !enabled {
		return w.stopFundsMixer()
	}
	w.startFundsMixer()
	w.emitBalance()
	return nil
}

// FundsMixingStats returns the current state of the wallet's funds mixer. Part
// of the asset.FundsMixer interface.
func (w *NativeWallet) FundsMixingStats() (*asset.FundsMixingStats, error) {
	return &asset.FundsMixingStats{
		Enabled:                 w.mixing.Load(),
		UnmixedBalanceThreshold: smalletCSPPSplitPoint,
	}, nil
}

// startFundsMixer starts the funds mixer.  This will error if the wallet does
// not allow starting or stopping the mixer or if the mixer was already
// started. Part of the asset.FundsMixer interface.
func (w *NativeWallet) startFundsMixer() {
	w.mixer.mtx.Lock()
	defer w.mixer.mtx.Unlock()
	w.mixer.turnOn(w.ctx)
	w.spvw.setAccounts(true)
	w.mixing.Store(true)
	w.mixFunds()
}

func (w *NativeWallet) stopFundsMixer() error {
	w.mixer.mtx.Lock()
	defer w.mixer.mtx.Unlock()
	w.mixer.closeAndClear()
	w.mixer.wg.Wait()
	if err := w.transferAccount(w.ctx, defaultAccountName, mixedAccountName, tradingAccountName); err != nil {
		return fmt.Errorf("error transferring funds while disabling mixing: %w", err)
	}
	w.spvw.setAccounts(false)
	w.mixing.Store(false)
	return nil
}

// Lock locks all the native wallet accounts.
func (w *NativeWallet) Lock() (err error) {
	w.mixer.mtx.Lock()
	w.mixer.closeAndClear()
	w.mixer.mtx.Unlock()
	w.mixer.wg.Wait()
	for _, acct := range nativeAccounts {
		if err = w.wallet.LockAccount(w.ctx, acct); err != nil {
			return fmt.Errorf("error locking native wallet account %q: %w", acct, err)
		}
	}
	return nil
}

// mixFunds checks the status of mixing operations and starts a mix cycle.
// mixFunds must be called with the mixer.mtx >= RLock'd.
func (w *NativeWallet) mixFunds() {
	if on := w.mixer.ctx != nil; !on {
		return
	}
	if !w.mixing.Load() {
		return
	}
	ctx := w.mixer.ctx
	if w.network == dex.Simnet {
		w.mixer.wg.Add(1)
		go func() {
			defer w.mixer.wg.Done()
			w.runSimnetMixer(ctx)
		}()
		return
	}
	w.mixer.wg.Add(1)
	go func() {
		defer w.mixer.wg.Done()
		w.spvw.mix(ctx)
		w.emitBalance()
	}()
}

// runSimnetMixer just sends all funds from the mixed account to the default
// account, after a short delay.
func (w *NativeWallet) runSimnetMixer(ctx context.Context) {
	if err := w.transferAccount(ctx, mixedAccountName, defaultAccountName); err != nil {
		w.log.Errorf("error transferring funds while disabling mixing: %w", err)
	}
}

// transferAccount sends all funds from the fromAccts to the toAcct.
func (w *NativeWallet) transferAccount(ctx context.Context, toAcct string, fromAccts ...string) error {
	// Move funds from mixed and trading account to default account.
	var unspents []*walletjson.ListUnspentResult
	for _, acctName := range fromAccts {
		uns, err := w.spvw.Unspents(ctx, acctName)
		if err != nil {
			return fmt.Errorf("error listing unspent outputs for acct %q: %w", acctName, err)
		}
		unspents = append(unspents, uns...)
	}
	if len(unspents) == 0 {
		return nil
	}
	var coinsToTransfer asset.Coins
	for _, unspent := range unspents {
		txHash, err := chainhash.NewHashFromStr(unspent.TxID)
		if err != nil {
			return fmt.Errorf("error decoding txid: %w", err)
		}
		v := toAtoms(unspent.Amount)
		op := newOutput(txHash, unspent.Vout, v, unspent.Tree)
		coinsToTransfer = append(coinsToTransfer, op)
	}

	tx, totalSent, err := w.sendAll(coinsToTransfer, toAcct)
	if err != nil {
		return fmt.Errorf("unable to transfer all funds from %+v accounts: %v", fromAccts, err)
	} else {
		w.log.Infof("Transferred %s from %+v accounts to %s account in tx %s.",
			dcrutil.Amount(totalSent), fromAccts, toAcct, tx.TxHash())
	}
	return nil
}

// birthdayBlockHeight performs a binary search for the last block with a
// timestamp lower than the provided birthday.
func (w *NativeWallet) birthdayBlockHeight(ctx context.Context, bday uint64) int32 {
	w.tipMtx.RLock()
	tipHeight := w.currentTip.height
	w.tipMtx.RUnlock()
	var err error
	firstBlockAfterBday := sort.Search(int(tipHeight), func(blockHeightI int) bool {
		if err != nil { // if we see any errors, just give up.
			return false
		}
		var blockHash *chainhash.Hash
		if blockHash, err = w.spvw.GetBlockHash(ctx, int64(blockHeightI)); err != nil {
			w.log.Errorf("Error getting block hash for height %d: %v", blockHeightI, err)
			return false
		}
		stamp, err := w.spvw.BlockTimestamp(ctx, blockHash)
		if err != nil {
			w.log.Errorf("Error getting block header for hash %s: %v", blockHash, err)
			return false
		}
		return uint64(stamp.Unix()) >= bday
	})
	if err != nil {
		w.log.Errorf("Error encountered searching for birthday block: %v", err)
		firstBlockAfterBday = 1
	}
	if firstBlockAfterBday == int(tipHeight) {
		w.log.Errorf("Birthday %d is from the future", bday)
		return 0
	}

	if firstBlockAfterBday == 0 {
		return 0
	}
	return int32(firstBlockAfterBday - 1)
}

// Rescan initiates a rescan of the wallet from height 0. Rescan only blocks
// long enough for the first asynchronous update, either an error or after the
// first 2000 blocks are scanned.
func (w *NativeWallet) Rescan(ctx context.Context, bday uint64) (err error) {
	// Make sure we don't already have one running.
	w.rescan.Lock()
	rescanInProgress := w.rescan.progress != nil
	if !rescanInProgress {
		w.rescan.progress = &rescanProgress{}
	}
	w.rescan.Unlock()
	if rescanInProgress {
		return errors.New("rescan already in progress")
	}

	if bday == 0 {
		bday = defaultWalletBirthdayUnix
	}
	bdayHeight := w.birthdayBlockHeight(ctx, bday)
	// Add a little buffer.
	const blockBufferN = 100
	if bdayHeight >= blockBufferN {
		bdayHeight -= blockBufferN
	} else {
		bdayHeight = 0
	}

	setProgress := func(height int32) {
		w.rescan.Lock()
		w.rescan.progress = &rescanProgress{scannedThrough: int64(height)}
		w.rescan.Unlock()
	}

	c := make(chan wallet.RescanProgress)
	go w.spvw.rescan(ctx, bdayHeight, c) // RescanProgressWithHeight will defer close(c)

	// First update will either be an error or a report of the first 2000
	// blocks. We can block until we get one.
	errC := make(chan error, 1)
	sendErr := func(err error) {
		select {
		case errC <- err:
		default:
		}
		if err == nil {
			w.receiveTxLastQuery.Store(0)
		} else {
			w.log.Errorf("Error encountered in rescan: %v", err)
		}
	}

	w.wg.Add(1)
	go func() {
		defer func() {
			w.rescan.Lock()
			lastUpdate := w.rescan.progress
			w.rescan.progress = nil
			w.rescan.Unlock()
			if lastUpdate != nil && lastUpdate.scannedThrough > 0 {
				w.log.Infof("Completed rescan of %d blocks", lastUpdate.scannedThrough)
			}
			w.wg.Done()
		}()
		// Rescans are quick. Timeouts > a second are probably too high, but
		// we'll give ample buffer.
		timeout := time.After(time.Minute)
		for {
			select {
			case u, open := <-c:
				if !open { // channel was closed. rescan is finished.
					if timeout == nil {
						sendErr(nil)
					} else {
						// We never saw an update.
						sendErr(errors.New("rescan finished without a progress update"))
					}
					return
				}
				sendErr(u.Err) // Hopefully nil, causing Rescan to return nil.
				timeout = nil  // Any update cancels timeout.
				if u.Err == nil {
					setProgress(u.ScannedThrough)
				}
			case <-timeout:
				sendErr(errors.New("rescan never sent progress updates"))
				return
			case <-ctx.Done():
				sendErr(ctx.Err())
				return
			}
		}
	}()
	return <-errC
}
