package dcr

import (
	"context"

	"decred.org/dcrwallet/v5/wallet/udb"
	"github.com/decred/dcrd/dcrutil/v4"
)

// must be sorted large to small
var splitPoints = [...]dcrutil.Amount{
	1 << 36, // 687.19476736
	1 << 34, // 171.79869184
	1 << 32, // 042.94967296
	1 << 30, // 010.73741824
	1 << 28, // 002.68435456
	1 << 26, // 000.67108864
	1 << 24, // 000.16777216
	1 << 22, // 000.04194304
	1 << 20, // 000.01048576
	1 << 18, // 000.00262144
}

var splitPointMap = map[int64]struct{}{}

func init() {
	for _, amt := range splitPoints {
		splitPointMap[int64(amt)] = struct{}{}
	}
}

const (
	smalletCSPPSplitPoint = 1 << 18 // 262144
	mixedAccountName      = "mixed"
	mixedAccountBranch    = udb.InternalBranch
	tradingAccountName    = "dextrading"
)

func (w *spvWallet) mix(ctx context.Context) {
	mixedAccount, err := w.AccountNumber(ctx, mixedAccountName)
	if err != nil {
		w.log.Errorf("unable to look up mixed account: %v", err)
		return
	}

	// unmixed account is the default account
	unmixedAccount := uint32(defaultAcct)

	w.log.Debug("Starting new cspp peer-to-peer funds mixing cycle")

	// Don't perform any actions while transactions are not synced
	// through the tip block.
	w.spvMtx.RLock()
	synced, _ := w.spv.Synced(ctx)
	w.spvMtx.RUnlock()
	if !synced {
		w.log.Tracef("Skipping account mixing: transactions are not synced")
		return
	}

	if err = w.MixAccount(ctx, unmixedAccount, mixedAccount, mixedAccountBranch); err != nil {
		w.log.Errorf("Error mixing account: %v", err)
	}
}
