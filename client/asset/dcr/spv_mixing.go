package dcr

import (
	"context"
	"fmt"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/wallet/udb"
)

const (
	smalletCSPPSplitPoint = 1 << 18 // 262144
	mixedAccountName      = "mixed"
	mixedAccountBranch    = udb.InternalBranch
	tradingAccount        = "dextrading"
)

// checkMixingAccounts checks if the mixed, unmixed and trading accounts
// required to use this wallet for funds mixing are created and optionally
// creates any non-existing account. If the accounts exist or are created, the
// wallet is marked as having mixing accounts and the account numbers for the
// mixed and unmixed accounts are returned. If any of the accounts do not exist
// and is not created, an error is returned.
func (w *spvWallet) checkMixingAccounts(ctx context.Context, create bool) (uint32, uint32, error) {
	// returns an error if the account doesn't exist and is not created.
	checkAccount := func(acct string) (uint32, error) {
		acctNum, err := w.dcrWallet.AccountNumber(ctx, acct)
		if errors.Is(err, errors.NotExist) && create {
			acctNum, err = w.dcrWallet.NextAccount(ctx, acct)
		}
		return acctNum, err
	}

	unmixedAcctNum := defaultAcct // the default acct is the unmixed account

	mixedAcctNum, err := checkAccount(mixedAccountName)
	if err != nil {
		return 0, 0, err
	}

	_, err = checkAccount(tradingAccount) // don't need the account number, but should exist as well
	if err != nil {
		return 0, 0, err
	}

	// If we got here, all required accounts exist.
	w.hasMixedAccount.Store(true)
	return mixedAcctNum, uint32(unmixedAcctNum), nil
}

// IsMixing is true if the wallet is currently mixing funds.
func (w *spvWallet) IsMixing() bool {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()
	return w.cancelCSPPMixer != nil
}

// StartFundsMixer starts mixing funds. Creates the required accounts (including
// the dex trading account) if any does not already exist.
func (w *spvWallet) StartFundsMixer(ctx context.Context, passphrase []byte) error {
	w.csppMtx.Lock()
	if w.cancelCSPPMixer != nil {
		w.csppMtx.Unlock()
		return fmt.Errorf("funds mixer is already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	w.cancelCSPPMixer = cancel
	w.csppMtx.Unlock()

	if len(passphrase) > 0 {
		// Unlock wallet to create accounts if necessary and also ensures the
		// unmixed account is unlocked as it does not have an account-level
		// encryption.
		err := w.Unlock(ctx, passphrase, nil)
		if err != nil {
			return err
		}
	}

	mixedAccount, unmixedAccount, err := w.checkMixingAccounts(ctx, true)
	if err != nil {
		return err
	}

	cfg := w.cfgV.Load().(*spvConfig)
	w.log.Debugf("Starting cspp funds mixer with %s", cfg.CSPPServer)

	go func() {
		tipChangeCh, stopTipChangeNtfn := w.dcrWallet.MainTipChangedNotifications()
		defer func() {
			stopTipChangeNtfn()
			w.StopFundsMixer()
		}()

		for {
			select {
			case <-ctx.Done():
				return

			case n := <-tipChangeCh:
				if len(n.AttachedBlocks) == 0 {
					continue
				}

				// Don't perform any actions while transactions are not synced
				// through the tip block.
				if !w.spv.Synced() {
					w.log.Debugf("Skipping autobuyer actions: transactions are not synced")
					continue
				}

				dial, csppServer := cfg.dialCSPPServer, cfg.CSPPServer
				go func() {
					err := w.dcrWallet.MixAccount(ctx, dial, csppServer, unmixedAccount, mixedAccount, mixedAccountBranch)
					if err != nil {
						w.log.Error(err)
					}
				}()
			}
		}
	}()

	return nil
}

// StopFundsMixer stops the funds mixer. This will error if the mixer was not
// already running.
func (w *spvWallet) StopFundsMixer() error {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()
	if w.cancelCSPPMixer == nil {
		return fmt.Errorf("funds mixer isn't running")
	}
	w.cancelCSPPMixer()
	w.cancelCSPPMixer = nil
	return nil
}
