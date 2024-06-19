package dcr

import (
	"context"

	"decred.org/dcrwallet/v4/wallet/udb"
)

const (
	smalletCSPPSplitPoint = 1 << 18 // 262144
	mixedAccountName      = "mixed"
	mixedAccountBranch    = udb.InternalBranch
	tradingAccountName    = "dextrading"
)

func (w *spvWallet) mix(ctx context.Context, cfg *mixingConfig) {
	mixedAccount, err := w.AccountNumber(ctx, mixedAccountName)
	if err != nil {
		w.log.Errorf("unable to look up mixed account: %v", err)
		return
	}

	// unmixed account is the default account
	unmixedAccount := uint32(defaultAcct)

	w.log.Debugf("Starting cspp funds mixer with %s", cfg.server)

	// Don't perform any actions while transactions are not synced
	// through the tip block.
	synced, _ := w.spv.Synced(ctx)
	if !synced {
		w.log.Tracef("Skipping account mixing: transactions are not synced")
		return
	}

	if err = w.MixAccount(ctx, unmixedAccount, mixedAccount, mixedAccountBranch); err != nil {
		w.log.Errorf("Error mixing account: %v", err)
	}
}
